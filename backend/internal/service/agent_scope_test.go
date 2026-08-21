package service

import (
	"net/http"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

func TestAuthorizeAgentScopeForPersonalCanvas(t *testing.T) {
	svc, db := newMembershipTestService(t)
	owner := createTeamTestUser(t, db, "agent-personal-owner", "agent-personal-owner@example.com")
	other := createTeamTestUser(t, db, "agent-personal-other", "agent-personal-other@example.com")
	createCollaborationTestCanvas(t, db, owner, "agent-personal-canvas")
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", "agent-personal-canvas").Update("project_id", "domain-project-1").Error; err != nil {
		t.Fatal(err)
	}

	scope, err := svc.AuthorizeAgentScope(owner.ID, "agent-personal-canvas", "thread-personal", "run-personal")
	if err != nil {
		t.Fatal(err)
	}
	if scope.TenantKind != agentruntime.TenantPersonal || scope.TenantID != owner.ID || scope.ActorUserID != owner.ID {
		t.Fatalf("personal tenant facts = %#v", scope)
	}
	if scope.DomainProjectID != "domain-project-1" || scope.CanvasID != "agent-personal-canvas" || scope.ThreadID != "thread-personal" || scope.RunID != "run-personal" {
		t.Fatalf("personal project/run facts = %#v", scope)
	}
	if scope.Access.Level != agentruntime.AccessManager || !scope.CanMutateCanvas() {
		t.Fatalf("personal access = %#v", scope.Access)
	}

	_, err = svc.AuthorizeAgentScope(other.ID, "agent-personal-canvas", "thread-other", "run-other")
	requireAuthStatus(t, err, http.StatusNotFound)
	if _, err := svc.AuthorizeAgentScope(owner.ID, "agent-personal-canvas", "", "run-personal"); err == nil {
		t.Fatal("missing thread id accepted")
	}
	if _, err := svc.AuthorizeAgentScope(owner.ID, "agent-personal-canvas", "thread-personal", ""); err == nil {
		t.Fatal("missing run id accepted")
	}
}

func TestAuthorizeAgentScopeUsesTeamAccessAndSubscriptionFacts(t *testing.T) {
	svc, db := newMembershipTestService(t)
	owner := createTeamTestUser(t, db, "agent-team-owner", "agent-team-owner@example.com")
	member := createTeamTestUser(t, db, "agent-team-member", "agent-team-member@example.com")
	outsider := createTeamTestUser(t, db, "agent-team-outsider", "agent-team-outsider@example.com")
	team, err := svc.CreateTeam(owner, teamCreateRequest("Agent Team"))
	if err != nil {
		t.Fatal(err)
	}
	activateTeamTestSubscription(t, db, team, owner, 3)
	now := time.Now().UTC()
	if err := db.Create(&model.TeamMember{
		ID: "agent-team-member-record", TeamID: team.ID, UserID: member.ID,
		Role: model.TeamMemberRoleMember, Status: model.TeamMemberStatusActive,
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CanvasProject{
		ID: "agent-team-canvas", UserID: owner.ID, TeamID: team.ID, ProjectID: "team-domain-project",
		Title: "Agent Team Canvas", PayloadJSON: `{}`, DefaultTeamAccess: model.CanvasAccessViewer,
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	ownerScope, err := svc.AuthorizeAgentScope(owner.ID, "agent-team-canvas", "thread-owner", "run-owner")
	if err != nil {
		t.Fatal(err)
	}
	if ownerScope.TenantKind != agentruntime.TenantTeam || ownerScope.TenantID != team.ID || ownerScope.Access.Level != agentruntime.AccessManager || !ownerScope.CanMutateCanvas() {
		t.Fatalf("team owner scope = %#v", ownerScope)
	}

	viewerScope, err := svc.AuthorizeAgentScope(member.ID, "agent-team-canvas", "thread-viewer", "run-viewer")
	if err != nil {
		t.Fatal(err)
	}
	if viewerScope.Access.Level != agentruntime.AccessViewer || viewerScope.CanMutateCanvas() {
		t.Fatalf("team viewer scope = %#v", viewerScope)
	}

	if err := db.Create(&model.CanvasCollaborator{
		ID: "agent-team-editor", CanvasID: "agent-team-canvas", TeamID: team.ID,
		UserID: member.ID, Access: model.CanvasAccessEditor, CreatedBy: owner.ID,
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	editorScope, err := svc.AuthorizeAgentScope(member.ID, "agent-team-canvas", "thread-editor", "run-editor")
	if err != nil {
		t.Fatal(err)
	}
	if editorScope.Access.Level != agentruntime.AccessEditor || !editorScope.CanMutateCanvas() || !editorScope.Access.SubscriptionActive {
		t.Fatalf("team editor scope = %#v", editorScope)
	}

	past := now.Add(-time.Hour)
	if err := db.Model(&model.MembershipSubscription{}).Where("team_id = ?", team.ID).Update("ends_at", past).Error; err != nil {
		t.Fatal(err)
	}
	expiredScope, err := svc.AuthorizeAgentScope(member.ID, "agent-team-canvas", "thread-expired", "run-expired")
	if err != nil {
		t.Fatal(err)
	}
	if expiredScope.Access.Level != agentruntime.AccessEditor || expiredScope.Access.SubscriptionActive || expiredScope.CanMutateCanvas() {
		t.Fatalf("expired team scope must preserve role but be read-only: %#v", expiredScope)
	}

	_, err = svc.AuthorizeAgentScope(outsider.ID, "agent-team-canvas", "thread-outsider", "run-outsider")
	requireAuthStatus(t, err, http.StatusNotFound)
}
