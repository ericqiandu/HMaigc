package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func createCollaborationTestCanvas(t *testing.T, db *gorm.DB, owner *model.User, id string) {
	t.Helper()
	raw := json.RawMessage(`{
		"id":"` + id + `",
		"title":"团队分镜画布",
		"createdAt":"2026-07-29T00:00:00Z",
		"updatedAt":"2026-07-29T00:00:00Z",
		"nodes":[],
		"connections":[],
		"chatSessions":[],
		"activeChatId":null,
		"backgroundMode":"lines",
		"showImageInfo":false,
		"viewport":{"x":0,"y":0,"k":1},
		"directorScenes":[]
	}`)
	if err := db.Create(&model.CanvasProject{
		ID: id, UserID: owner.ID, Title: "团队分镜画布", PayloadJSON: string(raw),
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestPersonalCanvasOwnerUsesRevisionedMutationProtocol(t *testing.T) {
	svc, db := newMembershipTestService(t)
	owner := createTeamTestUser(t, db, "personal-canvas-owner", "personal-canvas-owner@example.com")
	other := createTeamTestUser(t, db, "personal-canvas-other", "personal-canvas-other@example.com")
	createCollaborationTestCanvas(t, db, owner, "personal-revisioned-canvas")
	title := "个人画布新标题"
	request := CanvasMutationRequest{
		BaseRevision: 0, ClientMutationID: "personal-owner-change",
		Patch: CanvasMutationPatch{Document: &CanvasDocumentPatch{Title: &title}},
	}
	committed, err := svc.CommitCanvasMutation(owner, "personal-revisioned-canvas", request)
	if err != nil {
		t.Fatal(err)
	}
	if committed.Revision != 1 || committed.ActorUserID != owner.ID || committed.ClientMutationID != request.ClientMutationID {
		t.Fatalf("personal canvas mutation = %#v", committed)
	}
	replayed, err := svc.CommitCanvasMutation(owner, "personal-revisioned-canvas", request)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Revision != 1 {
		t.Fatalf("personal canvas replay = %#v", replayed)
	}
	if _, err := svc.CommitCanvasMutation(other, "personal-revisioned-canvas", CanvasMutationRequest{
		BaseRevision: 1, ClientMutationID: "personal-intruder-change", Patch: request.Patch,
	}); err == nil {
		t.Fatal("another user mutated a personal canvas")
	}
}

func TestCanvasCollaborationPermissionAndMutationLifecycle(t *testing.T) {
	svc, db := newMembershipTestService(t)
	owner := createTeamTestUser(t, db, "canvas-owner", "canvas-owner@example.com")
	member := createTeamTestUser(t, db, "canvas-member", "canvas-member@example.com")
	team, err := svc.CreateTeam(owner, CreateTeamRequest{Name: "协作制作组"})
	if err != nil {
		t.Fatal(err)
	}
	activateTeamTestSubscription(t, db, team, owner, 5)
	now := time.Now()
	if err := db.Create(&model.TeamMember{
		ID: "canvas-member-record", TeamID: team.ID, UserID: member.ID,
		Role: model.TeamMemberRoleMember, Status: model.TeamMemberStatusActive,
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	createCollaborationTestCanvas(t, db, owner, "canvas-collaboration-a")

	state, err := svc.ConfigureCanvasCollaboration(owner, "canvas-collaboration-a", ConfigureCanvasCollaborationRequest{
		TeamID: team.ID, DefaultAccess: model.CanvasAccessViewer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if state.Access.Level != model.CanvasAccessManager || !state.Access.CanManage || !state.Access.CanEdit {
		t.Fatalf("owner access = %#v", state.Access)
	}

	memberState, err := svc.CanvasCollaboration(member, "canvas-collaboration-a")
	if err != nil {
		t.Fatal(err)
	}
	if memberState.Access.Level != model.CanvasAccessViewer || memberState.Access.CanEdit {
		t.Fatalf("member default access = %#v", memberState.Access)
	}
	summaries, err := svc.UserCanvasProjectSummaries(member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].ID != "canvas-collaboration-a" || summaries[0].AccessLevel != model.CanvasAccessViewer {
		t.Fatalf("member canvas summaries = %#v", summaries)
	}
	requireAuthStatus(t, svc.ensureTaskProjectActive(member.ID, "canvas-collaboration-a"), http.StatusForbidden)
	_, err = svc.CommitCanvasMutation(member, "canvas-collaboration-a", CanvasMutationRequest{
		BaseRevision: 0, ClientMutationID: "viewer-write",
		Patch: CanvasMutationPatch{UpsertNodes: []json.RawMessage{json.RawMessage(`{"id":"node-1","type":"text","title":"节点","position":{"x":0,"y":0},"width":200,"height":100}`)}},
	})
	requireAuthStatus(t, err, http.StatusForbidden)

	if _, err := svc.UpdateCanvasCollaborator(owner, "canvas-collaboration-a", UpdateCanvasCollaboratorRequest{
		UserID: member.ID, Access: model.CanvasAccessEditor,
	}); err != nil {
		t.Fatal(err)
	}
	memberState, err = svc.CanvasCollaboration(member, "canvas-collaboration-a")
	if err != nil {
		t.Fatal(err)
	}
	if memberState.Access.Level != model.CanvasAccessEditor || !memberState.Access.CanEdit || memberState.Access.CanManage {
		t.Fatalf("member override access = %#v", memberState.Access)
	}
	if err := svc.ensureTaskProjectActive(member.ID, "canvas-collaboration-a"); err != nil {
		t.Fatalf("editor task access failed: %v", err)
	}

	request := CanvasMutationRequest{
		BaseRevision: 0, ClientMutationID: "member-node-create",
		Patch: CanvasMutationPatch{UpsertNodes: []json.RawMessage{json.RawMessage(`{"id":"node-1","type":"text","title":"节点","position":{"x":0,"y":0},"width":200,"height":100}`)}},
	}
	result, err := svc.CommitCanvasMutation(member, "canvas-collaboration-a", request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Revision != 1 || result.ActorUserID != member.ID {
		t.Fatalf("mutation result = %#v", result)
	}
	duplicate, err := svc.CommitCanvasMutation(member, "canvas-collaboration-a", request)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.Revision != result.Revision {
		t.Fatalf("duplicate revision = %d, want %d", duplicate.Revision, result.Revision)
	}
	_, err = svc.CommitCanvasMutation(member, "canvas-collaboration-a", CanvasMutationRequest{
		BaseRevision: 0, ClientMutationID: request.ClientMutationID,
		Patch: CanvasMutationPatch{DeleteNodeIDs: []string{"node-1"}},
	})
	requireAuthStatus(t, err, http.StatusConflict)
	_, err = svc.CommitCanvasMutation(owner, "canvas-collaboration-a", CanvasMutationRequest{
		BaseRevision: 0, ClientMutationID: "stale-owner-write",
		Patch: CanvasMutationPatch{DeleteNodeIDs: []string{"node-1"}},
	})
	requireAuthStatus(t, err, http.StatusConflict)

	var stored model.CanvasProject
	if err := db.First(&stored, "id = ?", "canvas-collaboration-a").Error; err != nil {
		t.Fatal(err)
	}
	if stored.Revision != 1 || stored.UpdatedByUserID != member.ID {
		t.Fatalf("stored canvas = %#v", stored)
	}
	var changes int64
	if err := db.Model(&model.CanvasChange{}).Where("canvas_id = ?", stored.ID).Count(&changes).Error; err != nil {
		t.Fatal(err)
	}
	if changes != 1 {
		t.Fatalf("change count = %d, want 1", changes)
	}
}

func TestCanvasCollaborationResourceAccessIsLimitedToReferencedResources(t *testing.T) {
	svc, db := newMembershipTestService(t)
	owner := createTeamTestUser(t, db, "canvas-resource-owner", "canvas-resource-owner@example.com")
	member := createTeamTestUser(t, db, "canvas-resource-member", "canvas-resource-member@example.com")
	team, err := svc.CreateTeam(owner, CreateTeamRequest{Name: "素材协作组"})
	if err != nil {
		t.Fatal(err)
	}
	activateTeamTestSubscription(t, db, team, owner, 3)
	now := time.Now()
	if err := db.Create(&model.TeamMember{
		ID: "canvas-resource-member-record", TeamID: team.ID, UserID: member.ID,
		Role: model.TeamMemberRoleMember, Status: model.TeamMemberStatusActive,
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	createCollaborationTestCanvas(t, db, owner, "canvas-resource-access")
	payload := `{
		"id":"canvas-resource-access",
		"title":"素材协作画布",
		"createdAt":"2026-07-29T00:00:00Z",
		"updatedAt":"2026-07-29T00:00:00Z",
		"nodes":[{"id":"image-1","type":"image","title":"参考图","position":{"x":0,"y":0},"width":320,"height":320,"metadata":{"storageKey":"resource:resource-allowed","content":"/api/resources/resource-allowed/file","prompt":"不要授权普通文本里的 /api/resources/resource-unrelated/file"}}],
		"connections":[],
		"chatSessions":[],
		"activeChatId":null,
		"backgroundMode":"lines",
		"showImageInfo":false,
		"viewport":{"x":0,"y":0,"k":1},
		"directorScenes":[]
	}`
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", "canvas-resource-access").Update("payload_json", payload).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"resource-allowed", "resource-unrelated"} {
		resourceOwnerID := owner.ID
		if id == "resource-allowed" {
			resourceOwnerID = member.ID
		}
		if err := db.Create(&model.Resource{
			ID: id, UserID: resourceOwnerID, Kind: "image", Status: model.ResourceStatusReady,
			Provider: "local", ObjectKey: resourceOwnerID + "/" + id + ".png", MimeType: "image/png",
			CreatedAt: now, UpdatedAt: now,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.ConfigureCanvasCollaboration(owner, "canvas-resource-access", ConfigureCanvasCollaborationRequest{
		TeamID: team.ID, DefaultAccess: model.CanvasAccessViewer,
	}); err != nil {
		t.Fatal(err)
	}
	state, err := svc.CanvasCollaboration(member, "canvas-resource-access")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(state.Project), "/api/canvas-projects/canvas-resource-access/resources/resource-allowed/file") {
		t.Fatalf("team resource URL not rewritten: %s", state.Project)
	}
	resource, err := svc.CanvasResource(member.ID, "canvas-resource-access", "resource-allowed")
	if err != nil {
		t.Fatal(err)
	}
	if resource.ID != "resource-allowed" {
		t.Fatalf("resource id = %s", resource.ID)
	}
	if resource.UserID != member.ID {
		t.Fatalf("resource owner = %s, want collaborator %s", resource.UserID, member.ID)
	}
	if _, err := svc.CanvasResource(member.ID, "canvas-resource-access", "resource-unrelated"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("unrelated resource error = %v, want record not found", err)
	}
	mutation, err := svc.CommitCanvasMutation(owner, "canvas-resource-access", CanvasMutationRequest{
		BaseRevision: 0, ClientMutationID: "move-resource-node",
		Patch: CanvasMutationPatch{UpsertNodes: []json.RawMessage{json.RawMessage(`{
			"id":"image-1","type":"image","title":"参考图","position":{"x":20,"y":30},"width":320,"height":320,
			"metadata":{"storageKey":"resource:resource-allowed","content":"/api/canvas-projects/canvas-resource-access/resources/resource-allowed/file"}
		}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mutation.Patch.UpsertNodes[0]), "/api/canvas-projects/canvas-resource-access/resources/resource-allowed/file") {
		t.Fatalf("broadcast mutation resource URL not rewritten: %s", mutation.Patch.UpsertNodes[0])
	}
	var stored model.CanvasProject
	if err := db.First(&stored, "id = ?", "canvas-resource-access").Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stored.PayloadJSON, "/api/resources/resource-allowed/file") ||
		strings.Contains(stored.PayloadJSON, "/api/canvas-projects/canvas-resource-access/resources/resource-allowed/file") {
		t.Fatalf("stored resource URL not canonical: %s", stored.PayloadJSON)
	}

	if _, err := svc.UpdateCanvasCollaborator(owner, "canvas-resource-access", UpdateCanvasCollaboratorRequest{
		UserID: member.ID, Access: model.CanvasAccessEditor,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Resource{
		ID: "resource-member-new", UserID: member.ID, Kind: "image", Status: model.ResourceStatusReady,
		Provider: "local", ObjectKey: member.ID + "/resource-member-new.png", MimeType: "image/png",
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	memberMutation, err := svc.CommitCanvasMutation(member, "canvas-resource-access", CanvasMutationRequest{
		BaseRevision: 1, ClientMutationID: "member-adds-own-resource",
		Patch: CanvasMutationPatch{UpsertNodes: []json.RawMessage{json.RawMessage(`{
			"id":"image-2","type":"image","title":"成员素材","position":{"x":380,"y":30},"width":320,"height":320,
			"metadata":{"storageKey":"resource:resource-member-new","content":"/api/resources/resource-member-new/file"}
		}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if memberMutation.Revision != 2 {
		t.Fatalf("member mutation revision = %d, want 2", memberMutation.Revision)
	}
	_, err = svc.CommitCanvasMutation(member, "canvas-resource-access", CanvasMutationRequest{
		BaseRevision: 2, ClientMutationID: "member-adds-foreign-resource",
		Patch: CanvasMutationPatch{UpsertNodes: []json.RawMessage{json.RawMessage(`{
			"id":"image-3","type":"image","title":"越权素材","position":{"x":740,"y":30},"width":320,"height":320,
			"metadata":{"storageKey":"resource:resource-unrelated","content":"/api/resources/resource-unrelated/file"}
		}`)}},
	})
	var authErr *AuthError
	if !errors.As(err, &authErr) || authErr.Status != http.StatusForbidden {
		t.Fatalf("foreign resource mutation error = %v, want forbidden", err)
	}
}

func TestCanvasCollaborationRejectsDanglingConnectionAndExpiredSubscriptionWrites(t *testing.T) {
	svc, db := newMembershipTestService(t)
	owner := createTeamTestUser(t, db, "canvas-owner-b", "canvas-owner-b@example.com")
	team, err := svc.CreateTeam(owner, CreateTeamRequest{Name: "协作边界组"})
	if err != nil {
		t.Fatal(err)
	}
	activateTeamTestSubscription(t, db, team, owner, 3)
	createCollaborationTestCanvas(t, db, owner, "canvas-collaboration-b")
	if _, err := svc.ConfigureCanvasCollaboration(owner, "canvas-collaboration-b", ConfigureCanvasCollaborationRequest{
		TeamID: team.ID, DefaultAccess: model.CanvasAccessEditor,
	}); err != nil {
		t.Fatal(err)
	}

	_, err = svc.CommitCanvasMutation(owner, "canvas-collaboration-b", CanvasMutationRequest{
		BaseRevision: 0, ClientMutationID: "dangling-connection",
		Patch: CanvasMutationPatch{UpsertConnections: []json.RawMessage{json.RawMessage(`{"id":"edge-1","fromNodeId":"missing-a","toNodeId":"missing-b"}`)}},
	})
	requireAuthStatus(t, err, http.StatusBadRequest)

	past := time.Now().Add(-time.Minute)
	if err := db.Model(&model.MembershipSubscription{}).Where("team_id = ?", team.ID).Update("ends_at", past).Error; err != nil {
		t.Fatal(err)
	}
	state, err := svc.CanvasCollaboration(owner, "canvas-collaboration-b")
	if err != nil {
		t.Fatal(err)
	}
	if state.Access.TeamSubscriptionActive || state.Access.CanEdit || !state.Access.CanManage {
		t.Fatalf("expired subscription access = %#v", state.Access)
	}
	_, err = svc.CommitCanvasMutation(owner, "canvas-collaboration-b", CanvasMutationRequest{
		BaseRevision: 0, ClientMutationID: "expired-write",
		Patch: CanvasMutationPatch{UpsertNodes: []json.RawMessage{json.RawMessage(`{"id":"node-expired","type":"text","title":"节点","position":{"x":0,"y":0},"width":200,"height":100}`)}},
	})
	requireAuthStatus(t, err, http.StatusPaymentRequired)
}
