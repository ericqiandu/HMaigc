package service

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
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

func TestLinkCanvasUnitReturnsCommittedProjectRevision(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := db.AutoMigrate(&model.ProjectUnit{}, &model.CanvasUnitLink{}); err != nil {
		t.Fatal(err)
	}
	user := createTeamTestUser(t, db, "project-canvas-link-user", "project-canvas-link@example.com")
	now := time.Now().UTC()
	project := model.Project{
		ID: "project-canvas-link", UserID: user.ID, Name: "画布关联项目", Type: "short-drama", AspectRatio: "9:16",
		SourceType: "blank", Status: model.ProjectStatusActive, Revision: 7, CreatedAt: now, UpdatedAt: now,
	}
	unit := model.ProjectUnit{
		ID: "project-canvas-link-unit", ProjectID: project.ID, Kind: model.ProjectUnitKindChapter,
		Title: "第一章", Status: model.ProjectUnitStatusDraft, CreatedAt: now, UpdatedAt: now,
	}
	canvas := model.CanvasProject{
		ID: "project-canvas-link-canvas", UserID: user.ID, Title: "章节 Agent", PayloadJSON: `{}`, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&unit).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&canvas).Error; err != nil {
		t.Fatal(err)
	}

	link, revision, err := svc.LinkCanvasUnit(user.ID, project.ID, LinkCanvasUnitRequest{CanvasID: canvas.ID, UnitID: unit.ID, Role: "storyboard"})
	if err != nil {
		t.Fatal(err)
	}
	if link.ProjectID != project.ID || link.CanvasID != canvas.ID || link.UnitID != unit.ID {
		t.Fatalf("link = %#v", link)
	}
	if revision != 8 {
		t.Fatalf("committed project revision = %d, want 8", revision)
	}
	var storedProject model.Project
	if err := db.First(&storedProject, "id = ?", project.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedProject.Revision != revision {
		t.Fatalf("stored project revision = %d, returned = %d", storedProject.Revision, revision)
	}
}

func TestLinkCanvasUnitRepositoryRejectsCanvasOwnerWithoutProjectEditAccess(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := db.AutoMigrate(&model.ProjectUnit{}, &model.CanvasUnitLink{}); err != nil {
		t.Fatal(err)
	}
	owner := createTeamTestUser(t, db, "project-link-owner", "project-link-owner@example.com")
	canvasOwner := createTeamTestUser(t, db, "project-link-canvas-owner", "project-link-canvas-owner@example.com")
	now := time.Now().UTC()
	project := model.Project{
		ID: "project-link-private", UserID: owner.ID, Name: "私有项目", Type: "short-drama", AspectRatio: "9:16",
		SourceType: "blank", Status: model.ProjectStatusActive, Revision: 4, CreatedAt: now, UpdatedAt: now,
	}
	unit := model.ProjectUnit{
		ID: "project-link-private-unit", ProjectID: project.ID, Kind: model.ProjectUnitKindChapter,
		Title: "第一章", Status: model.ProjectUnitStatusDraft, CreatedAt: now, UpdatedAt: now,
	}
	canvas := model.CanvasProject{
		ID: "project-link-attacker-canvas", UserID: canvasOwner.ID, Title: "无权限画布", PayloadJSON: `{}`, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&unit).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&canvas).Error; err != nil {
		t.Fatal(err)
	}

	link := model.CanvasUnitLink{
		ID: "project-link-unauthorized", ProjectID: project.ID, CanvasID: canvas.ID, UnitID: unit.ID,
		Role: "storyboard", CreatedAt: now,
	}
	if _, err := svc.repo.LinkCanvasUnit(canvasOwner.ID, &link, now); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("unauthorized repository link error = %v, want record not found", err)
	}
	var storedCanvas model.CanvasProject
	if err := db.First(&storedCanvas, "id = ?", canvas.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedCanvas.ProjectID != "" {
		t.Fatalf("unauthorized link changed canvas project_id = %q", storedCanvas.ProjectID)
	}
	var linkCount int64
	if err := db.Model(&model.CanvasUnitLink{}).Where("id = ?", link.ID).Count(&linkCount).Error; err != nil {
		t.Fatal(err)
	}
	if linkCount != 0 {
		t.Fatalf("unauthorized link persisted %d records", linkCount)
	}
	var storedProject model.Project
	if err := db.First(&storedProject, "id = ?", project.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedProject.Revision != project.Revision {
		t.Fatalf("unauthorized link changed project revision = %d", storedProject.Revision)
	}
}

func TestLinkCanvasUnitRepositoryRejectsCanvasFromAnotherTenant(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := db.AutoMigrate(&model.ProjectUnit{}, &model.CanvasUnitLink{}); err != nil {
		t.Fatal(err)
	}
	user := createTeamTestUser(t, db, "project-link-cross-tenant", "project-link-cross-tenant@example.com")
	now := time.Now().UTC()
	project := model.Project{
		ID: "project-link-personal", UserID: user.ID, Name: "个人项目", Type: "short-drama", AspectRatio: "9:16",
		SourceType: "blank", Status: model.ProjectStatusActive, Revision: 2, CreatedAt: now, UpdatedAt: now,
	}
	unit := model.ProjectUnit{
		ID: "project-link-personal-unit", ProjectID: project.ID, Kind: model.ProjectUnitKindChapter,
		Title: "第一章", Status: model.ProjectUnitStatusDraft, CreatedAt: now, UpdatedAt: now,
	}
	canvas := model.CanvasProject{
		ID: "project-link-team-canvas", UserID: user.ID, TeamID: "another-tenant", Title: "其他租户画布",
		PayloadJSON: `{}`, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&unit).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&canvas).Error; err != nil {
		t.Fatal(err)
	}

	link := model.CanvasUnitLink{
		ID: "project-link-cross-tenant-link", ProjectID: project.ID, CanvasID: canvas.ID, UnitID: unit.ID,
		Role: "storyboard", CreatedAt: now,
	}
	if _, err := svc.repo.LinkCanvasUnit(user.ID, &link, now); !errors.Is(err, repository.ErrCanvasProjectScopeConflict) {
		t.Fatalf("cross-tenant repository link error = %v, want canvas scope conflict", err)
	}
	var storedCanvas model.CanvasProject
	if err := db.First(&storedCanvas, "id = ?", canvas.ID).Error; err != nil {
		t.Fatal(err)
	}
	var storedProject model.Project
	if err := db.First(&storedProject, "id = ?", project.ID).Error; err != nil {
		t.Fatal(err)
	}
	var linkCount int64
	if err := db.Model(&model.CanvasUnitLink{}).Where("id = ?", link.ID).Count(&linkCount).Error; err != nil {
		t.Fatal(err)
	}
	if storedCanvas.ProjectID != "" || storedProject.Revision != project.Revision || linkCount != 0 {
		t.Fatalf("cross-tenant link leaked writes: canvas=%#v project=%#v links=%d", storedCanvas, storedProject, linkCount)
	}
}
