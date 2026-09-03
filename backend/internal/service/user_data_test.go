package service

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestValidateSyncedPayloadAllowsDataURLMentionInErrorMessage(t *testing.T) {
	raw, err := json.Marshal(map[string]interface{}{
		"nodes": []interface{}{
			map[string]interface{}{
				"metadata": map[string]interface{}{
					"errorDetails": "Expected a base64 image such as data:image/png;base64,aW1n, but received application/octet-stream",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSyncedPayload(raw, "画布"); err != nil {
		t.Fatalf("validateSyncedPayload() error = %v", err)
	}
}

func TestValidateSyncedPayloadRejectsNestedInlineMedia(t *testing.T) {
	for _, content := range []string{
		"data:image/png;base64,aW1n",
		"  DATA:VIDEO/mp4;base64,dmlkZW8=",
		"data:audio/mpeg;base64,YXVkaW8=",
	} {
		raw, err := json.Marshal(map[string]interface{}{
			"nodes": []interface{}{
				map[string]interface{}{"metadata": map[string]interface{}{"content": content}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := validateSyncedPayload(raw, "画布"); err == nil {
			t.Fatalf("validateSyncedPayload(%q) error = nil", content)
		}
	}
}

func TestCreateUserCanvasProjectRejectsLegacyOverwriteAfterRevisionedMutation(t *testing.T) {
	svc, _, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	owner := &model.User{ID: "runtime-user"}
	original := json.RawMessage(`{
		"id":"canvas-create-only",
		"title":"Agent 生产画布",
		"createdAt":"2026-08-18T00:00:00Z",
		"updatedAt":"2026-08-18T00:00:00Z",
		"nodes":[],"connections":[],"chatSessions":[],"activeChatId":null,
		"backgroundMode":"lines","showImageInfo":false,
		"viewport":{"x":0,"y":0,"k":1},"directorScenes":[]
	}`)
	if _, err := svc.CreateUserCanvasProject(owner.ID, original); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CommitCanvasMutation(owner, "canvas-create-only", CanvasMutationRequest{
		BaseRevision: 0, ClientMutationID: "agent-commit",
		Patch: CanvasMutationPatch{UpsertNodes: []json.RawMessage{json.RawMessage(`{"id":"agent-video","type":"video","title":"成片","position":{"x":0,"y":0},"width":420,"height":236}`)}},
	}); err != nil {
		t.Fatal(err)
	}

	legacyEmpty := json.RawMessage(`{
		"id":"canvas-create-only",
		"title":"Agent 生产画布",
		"createdAt":"2026-08-18T00:00:00Z",
		"updatedAt":"2026-08-18T00:01:00Z",
		"nodes":[],"connections":[],"chatSessions":[],"activeChatId":null,
		"backgroundMode":"lines","showImageInfo":false,
		"viewport":{"x":0,"y":0,"k":1},"directorScenes":[]
	}`)
	_, err := svc.CreateUserCanvasProject(owner.ID, legacyEmpty)
	var authErr *AuthError
	if !errors.As(err, &authErr) || authErr.Status != http.StatusConflict {
		t.Fatalf("legacy overwrite error = %v, want HTTP 409", err)
	}
	stored, err := svc.repo.CanvasProject("canvas-create-only")
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Nodes []json.RawMessage `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(stored.PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if stored.Revision != 1 || len(payload.Nodes) != 1 {
		t.Fatalf("stored canvas revision=%d nodes=%d, want revision=1 nodes=1", stored.Revision, len(payload.Nodes))
	}
}

func TestCreateUserCanvasProjectReturnsConflictWhenConcurrentCreateWins(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	const canvasID = "canvas-create-race"
	callbackName := "test:insert-concurrent-canvas"
	if err := db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Table != "canvas_projects" {
			return
		}
		project, ok := tx.Statement.Dest.(*model.CanvasProject)
		if !ok || project.ID != canvasID {
			return
		}
		result := tx.Session(&gorm.Session{NewDB: true}).Exec(
			`INSERT INTO canvas_projects (id, user_id, title, payload_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			project.ID, project.UserID, "concurrent winner", project.PayloadJSON, project.CreatedAt, project.UpdatedAt,
		)
		if result.Error != nil {
			tx.AddError(result.Error)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callbackName) })

	raw := json.RawMessage(`{
		"id":"canvas-create-race",
		"title":"并发创建画布",
		"createdAt":"2026-08-18T00:00:00Z",
		"updatedAt":"2026-08-18T00:00:00Z",
		"nodes":[],"connections":[],"chatSessions":[],"activeChatId":null,
		"backgroundMode":"lines","showImageInfo":false,
		"viewport":{"x":0,"y":0,"k":1},"directorScenes":[]
	}`)
	_, err := svc.CreateUserCanvasProject("runtime-user", raw)
	var authErr *AuthError
	if !errors.As(err, &authErr) || authErr.Status != http.StatusConflict {
		t.Fatalf("concurrent create error = %v, want HTTP 409", err)
	}
}

func TestDeleteUserCanvasProjectRollsBackShareWhenProjectDeleteFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.CanvasProject{}, &model.CanvasProjectDeletion{}, &model.CanvasShare{}, &model.CanvasChange{}, &model.CanvasCollaborator{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	project := &model.CanvasProject{ID: "canvas-delete-rollback", UserID: "user-1", Title: "待删除画布", PayloadJSON: `{"id":"canvas-delete-rollback","title":"待删除画布"}`, CreatedAt: now, UpdatedAt: now}
	share := &model.CanvasShare{ID: "share-delete-rollback", UserID: "user-1", ProjectID: project.ID, TokenHash: "share-delete-rollback-hash", Enabled: true, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(project).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(share).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_canvas_delete BEFORE DELETE ON canvas_projects BEGIN SELECT RAISE(ABORT, 'forced canvas delete failure'); END`).Error; err != nil {
		t.Fatal(err)
	}

	svc := New(repository.New(db), t.TempDir())
	if err := svc.DeleteUserCanvasProject("user-1", project.ID); err == nil {
		t.Fatal("DeleteUserCanvasProject() error = nil, want forced transaction failure")
	}
	if err := db.First(&model.CanvasProject{}, "id = ?", project.ID).Error; err != nil {
		t.Fatalf("canvas project must remain after rollback: %v", err)
	}
	if err := db.First(&model.CanvasShare{}, "id = ?", share.ID).Error; err != nil {
		t.Fatalf("canvas share must remain after rollback: %v", err)
	}
	var deletionCount int64
	if err := db.Model(&model.CanvasProjectDeletion{}).Where("canvas_id = ?", project.ID).Count(&deletionCount).Error; err != nil {
		t.Fatal(err)
	}
	if deletionCount != 0 {
		t.Fatalf("canvas deletion tombstones after rollback = %d, want 0", deletionCount)
	}
}

func TestDeleteUserCanvasProjectRecordsDeletionTombstone(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	now := time.Now().UTC().Add(-time.Minute)
	project := &model.CanvasProject{
		ID: "canvas-delete-tombstone", UserID: "runtime-user", Title: "待删除画布",
		PayloadJSON: `{"id":"canvas-delete-tombstone","title":"待删除画布","nodes":[]}`,
		CreatedAt:   now, UpdatedAt: now,
	}
	if err := db.Create(project).Error; err != nil {
		t.Fatal(err)
	}

	if err := svc.DeleteUserCanvasProject("runtime-user", project.ID); err != nil {
		t.Fatalf("DeleteUserCanvasProject() error = %v", err)
	}
	deletion, err := svc.repo.CanvasProjectDeletion(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deletion.CanvasID != project.ID || deletion.UserID != "runtime-user" || deletion.TeamID != "" || deletion.DeletedByUserID != "runtime-user" {
		t.Fatalf("canvas deletion = %#v", deletion)
	}
	if !deletion.DeletedAt.After(now) {
		t.Fatalf("deletedAt = %s, want after %s", deletion.DeletedAt, now)
	}
	if _, err := svc.repo.CanvasProject(project.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("deleted canvas lookup error = %v, want record not found", err)
	}
}

func TestCanvasProjectDeletionsForActorRespectsPersonalAndTeamScope(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	now := time.Now().UTC()
	team := &model.Team{ID: "team-deletion-scope", OwnerUserID: "team-owner", Name: "删除测试团队", Status: model.TeamStatusActive, CreatedAt: now, UpdatedAt: now}
	members := []model.TeamMember{
		{ID: "team-deletion-owner", TeamID: team.ID, UserID: "team-owner", Role: model.TeamMemberRoleOwner, Status: model.TeamMemberStatusActive, CreatedAt: now, UpdatedAt: now},
		{ID: "team-deletion-member", TeamID: team.ID, UserID: "team-member", Role: model.TeamMemberRoleMember, Status: model.TeamMemberStatusActive, CreatedAt: now, UpdatedAt: now},
		{ID: "team-deletion-removed", TeamID: team.ID, UserID: "removed-member", Role: model.TeamMemberRoleMember, Status: model.TeamMemberStatusRemoved, CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(team).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&members).Error; err != nil {
		t.Fatal(err)
	}
	deletions := []model.CanvasProjectDeletion{
		{CanvasID: "personal-deletion", UserID: "runtime-user", DeletedByUserID: "runtime-user", DeletedAt: now.Add(-time.Minute)},
		{CanvasID: "team-deletion", UserID: "team-owner", TeamID: team.ID, DeletedByUserID: "team-owner", DeletedAt: now},
	}
	if err := db.Create(&deletions).Error; err != nil {
		t.Fatal(err)
	}

	personal, err := svc.repo.CanvasProjectDeletionsForActor("runtime-user")
	if err != nil {
		t.Fatal(err)
	}
	if len(personal) != 1 || personal[0].CanvasID != "personal-deletion" {
		t.Fatalf("personal deletions = %#v", personal)
	}
	teamMember, err := svc.repo.CanvasProjectDeletionsForActor("team-member")
	if err != nil {
		t.Fatal(err)
	}
	if len(teamMember) != 1 || teamMember[0].CanvasID != "team-deletion" {
		t.Fatalf("team member deletions = %#v", teamMember)
	}
	for _, actorID := range []string{"unrelated-user", "removed-member"} {
		visible, err := svc.repo.CanvasProjectDeletionsForActor(actorID)
		if err != nil {
			t.Fatal(err)
		}
		if len(visible) != 0 {
			t.Fatalf("deletions visible to %s = %#v, want none", actorID, visible)
		}
	}
}

func TestCreateUserCanvasProjectReturnsGoneAfterDeletion(t *testing.T) {
	svc, _, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	raw := canvasSyncTestProjectJSON(t, "canvas-deleted-create", "不能复活的画布")
	if _, err := svc.CreateUserCanvasProject("runtime-user", raw); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteUserCanvasProject("runtime-user", "canvas-deleted-create"); err != nil {
		t.Fatal(err)
	}

	_, err := svc.CreateUserCanvasProject("runtime-user", raw)
	var authErr *AuthError
	if !errors.As(err, &authErr) || authErr.Status != http.StatusGone {
		t.Fatalf("create deleted canvas error = %v, want HTTP 410", err)
	}
	if _, err := svc.repo.CanvasProject("canvas-deleted-create"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("deleted canvas was recreated: %v", err)
	}
}

func TestDeleteUserCanvasProjectIsIdempotentForDeletionScope(t *testing.T) {
	svc, _, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	raw := canvasSyncTestProjectJSON(t, "canvas-delete-idempotent", "幂等删除画布")
	if _, err := svc.CreateUserCanvasProject("runtime-user", raw); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteUserCanvasProject("runtime-user", "canvas-delete-idempotent"); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteUserCanvasProject("runtime-user", "canvas-delete-idempotent"); err != nil {
		t.Fatalf("repeated owner delete error = %v", err)
	}

	err := svc.DeleteUserCanvasProject("unrelated-user", "canvas-delete-idempotent")
	var authErr *AuthError
	if !errors.As(err, &authErr) || authErr.Status != http.StatusNotFound {
		t.Fatalf("unrelated repeated delete error = %v, want HTTP 404", err)
	}
}

func TestDeleteCanvasProjectWithCollaborationAcceptsStaleConcurrentDelete(t *testing.T) {
	svc, _, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	raw := canvasSyncTestProjectJSON(t, "canvas-delete-concurrent", "并发幂等删除画布")
	if _, err := svc.CreateUserCanvasProject("runtime-user", raw); err != nil {
		t.Fatal(err)
	}
	project, err := svc.repo.CanvasProject("canvas-delete-concurrent")
	if err != nil {
		t.Fatal(err)
	}
	deletedAt := time.Now().UTC()
	if err := svc.repo.DeleteCanvasProjectWithCollaboration(project, "runtime-user", deletedAt); err != nil {
		t.Fatal(err)
	}

	if err := svc.repo.DeleteCanvasProjectWithCollaboration(project, "runtime-user", deletedAt.Add(time.Second)); err != nil {
		t.Fatalf("stale concurrent delete error = %v, want idempotent success", err)
	}
}

func TestUserCanvasProjectDeletionSummariesReturnsServerFacts(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	deletedAt := time.Date(2026, time.August, 25, 3, 4, 5, 0, time.UTC)
	deletion := model.CanvasProjectDeletion{
		CanvasID: "canvas-deletion-summary", UserID: "runtime-user",
		DeletedByUserID: "runtime-user", DeletedAt: deletedAt,
	}
	if err := db.Create(&deletion).Error; err != nil {
		t.Fatal(err)
	}

	summaries, err := svc.UserCanvasProjectDeletions("runtime-user")
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].ID != deletion.CanvasID || !summaries[0].DeletedAt.Equal(deletedAt) {
		t.Fatalf("deletion summaries = %#v", summaries)
	}
}

func canvasSyncTestProjectJSON(t *testing.T, id string, title string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"id": id, "title": title,
		"createdAt": "2026-08-25T00:00:00Z", "updatedAt": "2026-08-25T00:00:00Z",
		"nodes": []any{}, "connections": []any{}, "chatSessions": []any{}, "activeChatId": nil,
		"backgroundMode": "lines", "showImageInfo": false,
		"viewport": map[string]any{"x": 0, "y": 0, "k": 1}, "directorScenes": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestDeleteUserAssetDeletesExclusiveOSSObject(t *testing.T) {
	deleteRequests := make(chan string, 1)
	server := useAssetDeletionOSSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodDelete {
			t.Fatalf("OSS request method = %s, want DELETE", request.Method)
		}
		deleteRequests <- request.URL.Path
		writer.WriteHeader(http.StatusNoContent)
	}))

	svc := newAssetDeletionTestService(t)
	saveAssetDeletionFixture(t, svc, server.URL, "asset-1", "resource-exclusive")

	if err := svc.DeleteUserAsset("user-1", "asset-1"); err != nil {
		t.Fatalf("DeleteUserAsset() error = %v", err)
	}
	select {
	case path := <-deleteRequests:
		if path != "/users/user-1/image/exclusive.png" {
			t.Fatalf("OSS DELETE path = %q", path)
		}
	default:
		t.Fatal("DeleteUserAsset() did not delete the OSS object")
	}
	if _, err := svc.repo.AssetForUser("user-1", "asset-1"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("deleted asset lookup error = %v", err)
	}
	resource, err := svc.repo.Resource("resource-exclusive")
	if err != nil {
		t.Fatal(err)
	}
	if resource.Status != model.ResourceStatusDeleted {
		t.Fatalf("resource status = %s, want deleted", resource.Status)
	}
}

func TestDeleteUserAssetOSSFailureKeepsAssetAndResource(t *testing.T) {
	server := useAssetDeletionOSSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = writer.Write([]byte("temporary failure"))
	}))
	svc := newAssetDeletionTestService(t)
	saveAssetDeletionFixture(t, svc, server.URL, "asset-failure", "resource-failure")

	err := svc.DeleteUserAsset("user-1", "asset-failure")
	if err == nil || !strings.Contains(err.Error(), "OSS 对象删除失败") {
		t.Fatalf("DeleteUserAsset() error = %v", err)
	}
	if _, err := svc.repo.AssetForUser("user-1", "asset-failure"); err != nil {
		t.Fatalf("asset must remain after OSS failure: %v", err)
	}
	resource, err := svc.repo.Resource("resource-failure")
	if err != nil {
		t.Fatal(err)
	}
	if resource.Status != model.ResourceStatusReady {
		t.Fatalf("resource status = %s, want ready", resource.Status)
	}
}

func TestDeleteUserAssetAcceptsAlreadyMissingOSSObject(t *testing.T) {
	server := useAssetDeletionOSSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
	}))
	svc := newAssetDeletionTestService(t)
	saveAssetDeletionFixture(t, svc, server.URL, "asset-missing-object", "resource-missing-object")

	if err := svc.DeleteUserAsset("user-1", "asset-missing-object"); err != nil {
		t.Fatalf("DeleteUserAsset() error = %v", err)
	}
	resource, err := svc.repo.Resource("resource-missing-object")
	if err != nil {
		t.Fatal(err)
	}
	if resource.Status != model.ResourceStatusDeleted {
		t.Fatalf("resource status = %s, want deleted", resource.Status)
	}
}

func TestDeleteUserAssetRetainsResourceReferencedByCanvas(t *testing.T) {
	deleteRequests := 0
	server := useAssetDeletionOSSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		deleteRequests++
		writer.WriteHeader(http.StatusNoContent)
	}))
	svc := newAssetDeletionTestService(t)
	saveAssetDeletionFixture(t, svc, server.URL, "asset-shared", "resource-shared")
	canvasPayload, err := json.Marshal(struct {
		Nodes []assetDeletionCanvasNode `json:"nodes"`
	}{
		Nodes: []assetDeletionCanvasNode{{
			Metadata: assetDeletionResourceData{StorageKey: "resource:resource-shared"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.Save(&model.CanvasProject{
		ID: "canvas-1", UserID: "user-1", PayloadJSON: string(canvasPayload),
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	if err := svc.DeleteUserAsset("user-1", "asset-shared"); err != nil {
		t.Fatalf("DeleteUserAsset() error = %v", err)
	}
	if deleteRequests != 0 {
		t.Fatalf("OSS DELETE requests = %d, want 0 for a referenced resource", deleteRequests)
	}
	resource, err := svc.repo.Resource("resource-shared")
	if err != nil {
		t.Fatal(err)
	}
	if resource.Status != model.ResourceStatusReady {
		t.Fatalf("referenced resource status = %s, want ready", resource.Status)
	}
}

func TestDeleteUserAssetsSharingResourceDeletesObjectAfterLastAsset(t *testing.T) {
	deleteRequests := 0
	server := useAssetDeletionOSSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		deleteRequests++
		writer.WriteHeader(http.StatusNoContent)
	}))
	svc := newAssetDeletionTestService(t)
	saveAssetDeletionFixture(t, svc, server.URL, "asset-shared-first", "resource-shared-assets")
	saveAssetDeletionRecord(t, svc, "asset-shared-second", "resource-shared-assets", time.Now())

	if err := svc.DeleteUserAsset("user-1", "asset-shared-first"); err != nil {
		t.Fatalf("DeleteUserAsset(first) error = %v", err)
	}
	if deleteRequests != 0 {
		t.Fatalf("OSS DELETE requests after first asset = %d, want 0", deleteRequests)
	}
	if err := svc.DeleteUserAsset("user-1", "asset-shared-second"); err != nil {
		t.Fatalf("DeleteUserAsset(second) error = %v", err)
	}
	if deleteRequests != 1 {
		t.Fatalf("OSS DELETE requests after last asset = %d, want 1", deleteRequests)
	}
	resource, err := svc.repo.Resource("resource-shared-assets")
	if err != nil {
		t.Fatal(err)
	}
	if resource.Status != model.ResourceStatusDeleted {
		t.Fatalf("resource status = %s, want deleted", resource.Status)
	}
}

func TestDeleteUserAssetRejectsResourceInActiveMigration(t *testing.T) {
	deleteRequests := 0
	server := useAssetDeletionOSSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		deleteRequests++
		writer.WriteHeader(http.StatusNoContent)
	}))
	svc := newAssetDeletionTestService(t)
	saveAssetDeletionFixture(t, svc, server.URL, "asset-migrating", "resource-migrating")
	if err := svc.repo.Save(&model.StorageMigrationItem{
		ID: "migration-item-1", JobID: "migration-job-1", ResourceID: "resource-migrating",
		Status: model.StorageMigrationItemPending, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	err := svc.DeleteUserAsset("user-1", "asset-migrating")
	if err == nil || !strings.Contains(err.Error(), "正在迁移") {
		t.Fatalf("DeleteUserAsset() error = %v", err)
	}
	if deleteRequests != 0 {
		t.Fatalf("OSS DELETE requests = %d, want 0 during migration", deleteRequests)
	}
	if _, err := svc.repo.AssetForUser("user-1", "asset-migrating"); err != nil {
		t.Fatalf("asset must remain during migration: %v", err)
	}
}

func TestDeleteUserAssetDeletesExclusiveLocalFile(t *testing.T) {
	svc := newAssetDeletionTestService(t)
	now := time.Now()
	objectKey := "users/user-1/image/local.png"
	filePath := filepath.Join(svc.dataDir, "resources", filepath.FromSlash(objectKey))
	if err := os.MkdirAll(filepath.Dir(filePath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte("img"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CreateResource(&model.Resource{
		ID: "resource-local-delete", UserID: "user-1", Kind: "image",
		Status: model.ResourceStatusReady, Provider: "local", ObjectKey: objectKey,
		MimeType: "image/png", Size: 3, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	saveAssetDeletionRecord(t, svc, "asset-local-delete", "resource-local-delete", now)

	if err := svc.DeleteUserAsset("user-1", "asset-local-delete"); err != nil {
		t.Fatalf("DeleteUserAsset() error = %v", err)
	}
	if _, err := os.Stat(filePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("local resource file still exists or stat failed: %v", err)
	}
}

func TestDeleteUserAssetDeletesOrphanedAssetWithMissingResource(t *testing.T) {
	svc := newAssetDeletionTestService(t)
	saveAssetDeletionRecord(t, svc, "asset-orphaned", "resource-missing", time.Now())

	if err := svc.DeleteUserAsset("user-1", "asset-orphaned"); err != nil {
		t.Fatalf("DeleteUserAsset() error = %v", err)
	}
	if _, err := svc.repo.AssetForUser("user-1", "asset-orphaned"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("deleted orphaned asset lookup error = %v", err)
	}
}

func TestDeleteUserAssetRejectsResourceOwnedByAnotherUser(t *testing.T) {
	svc := newAssetDeletionTestService(t)
	now := time.Now()
	if err := svc.repo.CreateResource(&model.Resource{
		ID: "resource-foreign", UserID: "user-2", Kind: "image",
		Status: model.ResourceStatusReady, Provider: "local",
		ObjectKey: "users/user-2/image/foreign.png", MimeType: "image/png",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	saveAssetDeletionRecord(t, svc, "asset-foreign-reference", "resource-foreign", now)

	err := svc.DeleteUserAsset("user-1", "asset-foreign-reference")
	if err == nil || !strings.Contains(err.Error(), "不属于当前账号") {
		t.Fatalf("DeleteUserAsset() error = %v", err)
	}
	if _, err := svc.repo.AssetForUser("user-1", "asset-foreign-reference"); err != nil {
		t.Fatalf("asset must remain after ownership rejection: %v", err)
	}
	if _, err := svc.repo.ResourceForUser("user-2", "resource-foreign"); err != nil {
		t.Fatalf("foreign resource must remain after ownership rejection: %v", err)
	}
}

func newAssetDeletionTestService(t *testing.T) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.SystemSetting{},
		&model.UserOSSSetting{},
		&model.Resource{},
		&model.Asset{},
		&model.AssetVersion{},
		&model.AssetRepresentation{},
		&model.CharacterVoiceBinding{},
		&model.ProjectAssetLink{},
		&model.Shot{},
		&model.ShotAssetReference{},
		&model.VoiceProfile{},
		&model.StorageMigrationItem{},
		&model.CanvasProject{},
		&model.Session{},
	); err != nil {
		t.Fatal(err)
	}
	return New(repository.New(db), t.TempDir())
}

type assetDeletionResourceData struct {
	StorageKey string `json:"storageKey"`
}

type assetDeletionPayload struct {
	ID    string                    `json:"id"`
	Kind  string                    `json:"kind"`
	Title string                    `json:"title"`
	Data  assetDeletionResourceData `json:"data"`
}

type assetDeletionCanvasNode struct {
	Metadata assetDeletionResourceData `json:"metadata"`
}

func useAssetDeletionOSSServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	serverAddress := server.Listener.Addr().String()
	originalTransport := outboundTransport
	outboundTransport = &http.Transport{
		DialContext: func(ctx context.Context, network string, _ string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: time.Second}
			return dialer.DialContext(ctx, network, serverAddress)
		},
	}
	t.Cleanup(func() {
		outboundTransport.CloseIdleConnections()
		outboundTransport = originalTransport
	})
	return server
}

func saveAssetDeletionFixture(t *testing.T, svc *Service, endpoint string, assetID string, resourceID string) {
	t.Helper()
	settingJSON, err := json.Marshal(ossSettingValue{
		Enabled: true, Provider: "aliyun", Endpoint: endpoint, Bucket: "media",
		AccessKeyID: "access-id", AccessKeySecret: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.SaveSystemSetting(&model.SystemSetting{Key: ossSettingKey, ValueJSON: string(settingJSON)}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := svc.repo.CreateResource(&model.Resource{
		ID: resourceID, UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady,
		Provider: "aliyun", Endpoint: endpoint, Bucket: "media",
		ObjectKey: "users/user-1/image/exclusive.png", MimeType: "image/png", Size: 3,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	saveAssetDeletionRecord(t, svc, assetID, resourceID, now)
}

func saveAssetDeletionRecord(t *testing.T, svc *Service, assetID string, resourceID string, now time.Time) {
	t.Helper()
	payload, err := json.Marshal(assetDeletionPayload{
		ID: assetID, Kind: "image", Title: "待删除素材",
		Data: assetDeletionResourceData{StorageKey: "resource:" + resourceID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.UpsertAsset(&model.Asset{
		ID: assetID, UserID: "user-1", Kind: "image", Title: "待删除素材",
		PayloadJSON: string(payload), CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
}
