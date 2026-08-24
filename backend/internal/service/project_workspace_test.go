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

func TestRegisterTaskOutputRejectsInternalAgentTask(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := db.AutoMigrate(&model.WorkflowTemplateVersion{}, &model.WorkflowInstance{}, &model.WorkflowStepInstance{}, &model.WorkflowStepTask{}); err != nil {
		t.Fatal(err)
	}
	user := createTeamTestUser(t, db, "workflow-internal-task-user", "workflow-internal-task@example.com")
	project, err := svc.CreateProject(user.ID, CreateProjectRequest{Name: "内部任务隔离项目"})
	if err != nil {
		t.Fatal(err)
	}
	workflows, err := svc.ProjectWorkflows(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(workflows) != 1 || len(workflows[0].Steps) == 0 {
		t.Fatalf("project workflows = %#v", workflows)
	}
	step := workflows[0].Steps[0]
	now := time.Now().UTC()
	internalTask := model.Task{
		ID: "internal-agent-workflow-task", UserID: user.ID, Audience: model.TaskAudienceInternal,
		ProjectID: project.ID, Type: agentRuntimeModelTaskType, Status: model.TaskStatusSucceeded,
		ResultJSON: `{"message":"internal model result"}`, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&internalTask).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := svc.RegisterTaskOutput(user.ID, project.ID, step.ID, RegisterTaskOutputRequest{TaskID: internalTask.ID}); err == nil {
		t.Fatal("internal Agent task was accepted as a customer workflow output")
	}
	var storedStep model.WorkflowStepInstance
	if err := db.First(&storedStep, "id = ?", step.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedStep.Status != model.WorkflowStepStatusReady || storedStep.OutputJSON != "{}" {
		t.Fatalf("rejected internal task changed workflow step = %#v", storedStep)
	}
	var linkCount int64
	if err := db.Model(&model.WorkflowStepTask{}).Where("task_id = ?", internalTask.ID).Count(&linkCount).Error; err != nil {
		t.Fatal(err)
	}
	if linkCount != 0 {
		t.Fatalf("rejected internal task created %d workflow links", linkCount)
	}
}
