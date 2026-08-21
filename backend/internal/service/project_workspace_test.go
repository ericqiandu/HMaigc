package service

import (
	"net/http"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
)

func TestCreateProjectUsesAuthorizedTeamWorkspace(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := db.AutoMigrate(&model.WorkflowTemplateVersion{}, &model.WorkflowInstance{}, &model.WorkflowStepInstance{}); err != nil {
		t.Fatal(err)
	}
	owner := createTeamTestUser(t, db, "project-workspace-owner", "project-workspace-owner@example.com")
	team, err := svc.CreateTeam(owner, teamCreateRequest("项目工作区"))
	if err != nil {
		t.Fatal(err)
	}
	activateTeamTestSubscription(t, db, team, owner, 4)

	project, err := svc.CreateProject(owner.ID, CreateProjectRequest{Name: "团队短剧", TeamID: team.ID})
	if err != nil {
		t.Fatal(err)
	}
	if project.TeamID != team.ID {
		t.Fatalf("project team id = %q, want %q", project.TeamID, team.ID)
	}
}

func TestCreateProjectRejectsTeamMemberWithoutProjectManagementCapability(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := db.AutoMigrate(&model.WorkflowTemplateVersion{}, &model.WorkflowInstance{}, &model.WorkflowStepInstance{}); err != nil {
		t.Fatal(err)
	}
	owner := createTeamTestUser(t, db, "project-member-owner", "project-member-owner@example.com")
	member := createTeamTestUser(t, db, "project-member-user", "project-member-user@example.com")
	team, err := svc.CreateTeam(owner, teamCreateRequest("成员权限团队"))
	if err != nil {
		t.Fatal(err)
	}
	activateTeamTestSubscription(t, db, team, owner, 4)
	now := time.Now()
	if err := db.Create(&model.TeamMember{
		ID: newID(), TeamID: team.ID, UserID: member.ID, Role: model.TeamMemberRoleMember,
		Status: model.TeamMemberStatusActive, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	_, err = svc.CreateProject(member.ID, CreateProjectRequest{Name: "越权团队项目", TeamID: team.ID})
	requireAuthStatus(t, err, http.StatusForbidden)
	var projectCount int64
	if err := db.Model(&model.Project{}).Where("team_id = ?", team.ID).Count(&projectCount).Error; err != nil {
		t.Fatal(err)
	}
	if projectCount != 0 {
		t.Fatalf("unauthorized team project count = %d, want 0", projectCount)
	}
}
