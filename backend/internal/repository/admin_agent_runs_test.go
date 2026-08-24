package repository

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAdminAgentRunsUsesDatabasePaginationAndActivityFilters(t *testing.T) {
	repo, db := openAdminAgentRunRepositorySQLite(t)
	now := time.Date(2026, time.August, 24, 14, 0, 0, 0, time.UTC)
	createAdminAgentRunUser(t, db, "user-a", "a@example.com", "阿青")
	createAdminAgentRunUser(t, db, "user-b", "b@example.com", "白露")
	createAdminAgentRunRecord(t, db, adminAgentRunFixture{
		runID: "run-waiting-input", userID: "user-a", projectID: "project-one", canvasID: "canvas-one",
		status: agentruntime.RunWaitingInput, updatedAt: now.Add(-20 * time.Minute),
	})
	createAdminAgentRunRecord(t, db, adminAgentRunFixture{
		runID: "run-running", userID: "user-a", projectID: "project-two", canvasID: "canvas-two",
		status: agentruntime.RunRunning, updatedAt: now.Add(-15 * time.Minute),
	})
	createAdminAgentRunRecord(t, db, adminAgentRunFixture{
		runID: "run-waiting-approval", userID: "user-b", projectID: "project-three", canvasID: "canvas-three",
		status: agentruntime.RunWaitingApproval, updatedAt: now.Add(-5 * time.Minute),
	})
	createAdminAgentRunRecord(t, db, adminAgentRunFixture{
		runID: "run-queued", userID: "user-b", projectID: "project-four", canvasID: "canvas-four",
		status: agentruntime.RunQueued, updatedAt: now.Add(-time.Minute),
	})
	for index := 0; index < 18; index++ {
		createAdminAgentRunRecord(t, db, adminAgentRunFixture{
			runID: "run-filler-" + time.Duration(index).String(), userID: "user-b",
			projectID: "project-filler", canvasID: "canvas-filler",
			status: agentruntime.RunQueued, updatedAt: now.Add(time.Duration(index) * time.Second),
		})
	}
	createAdminAgentRunRecord(t, db, adminAgentRunFixture{
		runID: "run-succeeded", userID: "user-a", projectID: "project-five", canvasID: "canvas-five",
		status: agentruntime.RunSucceeded, updatedAt: now.Add(-30 * time.Minute),
	})

	page, err := repo.AdminAgentRuns(AdminAgentRunQuery{Page: 1, PageSize: 20}, now)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 22 || page.Page != 1 || page.PageSize != 20 || len(page.Items) != 20 {
		t.Fatalf("default page = %#v", page)
	}
	if page.Items[0].RunID != "run-waiting-input" || page.Items[1].RunID != "run-running" {
		t.Fatalf("default ordering = %#v", page.Items)
	}
	if page.Items[0].ActivityClassification != AdminAgentRunAwaitingUser || page.Items[0].InactiveSeconds != 1200 {
		t.Fatalf("waiting input classification = %#v", page.Items[0])
	}
	if page.Items[1].ActivityClassification != AdminAgentRunPossiblyStalled || page.Items[1].InactiveSeconds != 900 {
		t.Fatalf("running classification = %#v", page.Items[1])
	}
	secondPage, err := repo.AdminAgentRuns(AdminAgentRunQuery{Page: 2, PageSize: 20}, now)
	if err != nil {
		t.Fatal(err)
	}
	if secondPage.Total != 22 || len(secondPage.Items) != 2 {
		t.Fatalf("second page = %#v", secondPage)
	}

	awaitingUser, err := repo.AdminAgentRuns(AdminAgentRunQuery{Activity: AdminAgentRunAwaitingUser, Page: 1, PageSize: 20}, now)
	if err != nil {
		t.Fatal(err)
	}
	if awaitingUser.Total != 2 || len(awaitingUser.Items) != 2 || awaitingUser.Items[0].RunID != "run-waiting-input" || awaitingUser.Items[1].RunID != "run-waiting-approval" {
		t.Fatalf("awaiting user page = %#v", awaitingUser)
	}

	stalled, err := repo.AdminAgentRuns(AdminAgentRunQuery{Activity: AdminAgentRunPossiblyStalled, Page: 1, PageSize: 20}, now)
	if err != nil {
		t.Fatal(err)
	}
	if stalled.Total != 1 || len(stalled.Items) != 1 || stalled.Items[0].RunID != "run-running" {
		t.Fatalf("possibly stalled page = %#v", stalled)
	}

	byUser, err := repo.AdminAgentRuns(AdminAgentRunQuery{User: "a@example.com", Page: 1, PageSize: 20}, now)
	if err != nil {
		t.Fatal(err)
	}
	if byUser.Total != 2 || len(byUser.Items) != 2 {
		t.Fatalf("user-filtered page = %#v", byUser)
	}

	byScope, err := repo.AdminAgentRuns(AdminAgentRunQuery{Scope: "canvas-three", Page: 1, PageSize: 20}, now)
	if err != nil {
		t.Fatal(err)
	}
	if byScope.Total != 1 || byScope.Items[0].RunID != "run-waiting-approval" {
		t.Fatalf("scope-filtered page = %#v", byScope)
	}

	updatedBefore := now.Add(-10 * time.Minute)
	oldRuns, err := repo.AdminAgentRuns(AdminAgentRunQuery{UpdatedBefore: &updatedBefore, Page: 1, PageSize: 20}, now)
	if err != nil {
		t.Fatal(err)
	}
	if oldRuns.Total != 2 || oldRuns.Items[0].RunID != "run-waiting-input" || oldRuns.Items[1].RunID != "run-running" {
		t.Fatalf("updated-before page = %#v", oldRuns)
	}
}

func TestAdminAgentRunAggregatesControlFactsWithoutSensitiveBodies(t *testing.T) {
	repo, db := openAdminAgentRunRepositorySQLite(t)
	now := time.Date(2026, time.August, 24, 15, 0, 0, 0, time.UTC)
	createAdminAgentRunUser(t, db, "control-user", "control@example.com", "控制台用户")
	for _, fixture := range []adminAgentRunFixture{
		{runID: "run-interruptible", userID: "control-user", projectID: "project-a", canvasID: "canvas-a", status: agentruntime.RunWaitingInput, updatedAt: now},
		{runID: "run-provider", userID: "control-user", projectID: "project-b", canvasID: "canvas-b", status: agentruntime.RunRunning, updatedAt: now},
		{runID: "run-billing", userID: "control-user", projectID: "project-c", canvasID: "canvas-c", status: agentruntime.RunRunning, updatedAt: now},
		{runID: "run-terminal", userID: "control-user", projectID: "project-d", canvasID: "canvas-d", status: agentruntime.RunSucceeded, updatedAt: now},
		{runID: "run-approval", userID: "control-user", projectID: "project-e", canvasID: "canvas-e", status: agentruntime.RunWaitingApproval, updatedAt: now},
		{runID: "run-media", userID: "control-user", projectID: "project-f", canvasID: "canvas-f", status: agentruntime.RunWaitingTool, updatedAt: now},
	} {
		createAdminAgentRunRecord(t, db, fixture)
	}
	providerTask := model.Task{
		ID: "task-provider", UserID: "control-user", Audience: model.TaskAudienceInternal,
		Type: "agent_runtime_model", Capability: "text", Status: model.TaskStatusRunning,
		Operation: "agent_model:run-provider", ProviderRequestID: "provider-secret-request",
		Prompt: "sensitive user prompt", ResultJSON: `{"reasoning":"sensitive model body"}`,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&providerTask).Error; err != nil {
		t.Fatal(err)
	}
	billingOrder := model.BillingOrder{
		ID: "billing-unresolved", UserID: "control-user", IdempotencyKey: "agent-runtime:run-billing:1",
		Status: model.BillingStatusUncertain, Error: "sensitive provider billing body", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&billingOrder).Error; err != nil {
		t.Fatal(err)
	}
	approvalCall := model.AgentToolCall{
		ID: "tool-approval", RunID: "run-approval", ToolCallID: "tool-call-approval", ActionVersion: 1,
		ToolName: string(agentruntime.ToolProductionRender), Status: agentruntime.ToolCallWaitingApproval,
		ApprovalRequired: true, IdempotencyKey: "run-approval:tool:1",
		InputJSON: `{"prompt":"sensitive render prompt"}`, OutputJSON: `{}`, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&approvalCall).Error; err != nil {
		t.Fatal(err)
	}
	mediaTask := model.Task{
		ID: "task-media", UserID: "control-user", Audience: model.TaskAudienceInternal,
		Type: "canvas_video", Capability: "video", Status: model.TaskStatusRunning,
		ProviderRequestID: "provider-secret-media-request", Prompt: "sensitive media prompt",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&mediaTask).Error; err != nil {
		t.Fatal(err)
	}
	mediaPlan := model.AgentProductionPlanVersion{
		ID: "plan-media", PlanKey: "plan-key-media", TenantKind: agentruntime.TenantPersonal,
		TenantID: "control-user", DomainProjectID: "project-f", CanvasID: "canvas-f",
		CreatedByRunID: "run-media", Version: 1, Status: model.AgentProductionPlanActive,
		Title: "sensitive media title", Script: "sensitive media script", ReferencesJSON: `[]`,
		ShotsJSON: `[]`, ExpectedDeliveryJSON: `{}`, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&mediaPlan).Error; err != nil {
		t.Fatal(err)
	}
	mediaArtifact := model.AgentProductionArtifact{
		ID: "artifact-media", PlanKey: mediaPlan.PlanKey, PlanVersionID: mediaPlan.ID, PlanVersion: 1,
		Kind: model.AgentProductionArtifactVideoClip, Status: model.AgentProductionArtifactRunning,
		TaskID: mediaTask.ID, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&mediaArtifact).Error; err != nil {
		t.Fatal(err)
	}

	interruptible, err := repo.AdminAgentRun("run-interruptible", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if interruptible.ControlDisposition != AdminAgentRunInterruptibleNow || interruptible.ProviderRequestState != "none" {
		t.Fatalf("interruptible facts = %#v", interruptible)
	}

	provider, err := repo.AdminAgentRun("run-provider", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if provider.LinkedModelTaskStatus != string(model.TaskStatusRunning) || provider.ProviderRequestState != "submitted" || provider.ControlDisposition != AdminAgentRunCancelRequestRequired {
		t.Fatalf("provider facts = %#v", provider)
	}

	billing, err := repo.AdminAgentRun("run-billing", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if billing.BillingState != string(model.BillingStatusUncertain) || billing.ControlDisposition != AdminAgentRunBlockedByUnresolvedBilling || billing.ControlBlockedReason != "billing_unresolved" {
		t.Fatalf("billing facts = %#v", billing)
	}

	terminal, err := repo.AdminAgentRun("run-terminal", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if terminal.ControlDisposition != AdminAgentRunAlreadyTerminal {
		t.Fatalf("terminal facts = %#v", terminal)
	}

	approval, err := repo.AdminAgentRun("run-approval", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if approval.PendingKind != "approval" || approval.PendingToolName != string(agentruntime.ToolProductionRender) || approval.ConfirmationPhrase != "STOP run-appr" {
		t.Fatalf("approval facts = %#v", approval)
	}
	media, err := repo.AdminAgentRun("run-media", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if media.LinkedMediaTaskStatus != string(model.TaskStatusRunning) || media.ProviderRequestState != "submitted" || media.ControlDisposition != AdminAgentRunCancelRequestRequired {
		t.Fatalf("media facts = %#v", media)
	}
	encoded, err := json.Marshal([]*AdminAgentRunRecord{provider, billing, approval, media})
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(encoded)
	for _, secret := range []string{"sensitive user prompt", "sensitive model body", "sensitive provider billing body", "sensitive render prompt", "provider-secret-request", "sensitive media prompt", "sensitive media title", "sensitive media script", "provider-secret-media-request"} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("admin run summary leaked %q: %s", secret, serialized)
		}
	}
}

func TestAdminAgentRunBlocksActiveTaskWithMissingBillingOrder(t *testing.T) {
	repo, db := openAdminAgentRunRepositorySQLite(t)
	now := time.Date(2026, time.August, 24, 15, 30, 0, 0, time.UTC)
	createAdminAgentRunUser(t, db, "billing-link-user", "billing-link@example.com", "账务关联用户")
	createAdminAgentRunRecord(t, db, adminAgentRunFixture{
		runID: "run-missing-linked-billing", userID: "billing-link-user",
		projectID: "project-missing-linked-billing", canvasID: "canvas-missing-linked-billing",
		status: agentruntime.RunQueued, updatedAt: now,
	})
	task := model.Task{
		ID: "task-missing-linked-billing", UserID: "billing-link-user", Audience: model.TaskAudienceInternal,
		Type: "agent_runtime_model", Capability: "text", Status: model.TaskStatusQueued,
		Operation: "agent_model:run-missing-linked-billing", BillingOrderID: "missing-billing-order",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	record, err := repo.AdminAgentRun("run-missing-linked-billing", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if record.ControlDisposition != AdminAgentRunBlockedByUnresolvedBilling || record.ControlBlockedReason != "billing_unresolved" {
		t.Fatalf("missing linked billing facts = %#v", record)
	}
}

type adminAgentRunFixture struct {
	runID     string
	userID    string
	projectID string
	canvasID  string
	status    agentruntime.RunStatus
	updatedAt time.Time
}

func openAdminAgentRunRepositorySQLite(t *testing.T) (*Repository, *gorm.DB) {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared&_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.Task{}, &model.BillingOrder{},
		&model.AgentThread{}, &model.AgentRun{}, &model.AgentTimelineItem{},
		&model.AgentRunEvent{}, &model.AgentCheckpoint{}, &model.AgentToolCall{},
		&model.AgentProductionPlanVersion{}, &model.AgentProductionArtifact{},
	); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	return New(db), db
}

func createAdminAgentRunUser(t *testing.T, db *gorm.DB, id string, email string, displayName string) {
	t.Helper()
	user := model.User{ID: id, Username: id, Email: email, DisplayName: displayName, Role: model.UserRoleUser}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
}

func createAdminAgentRunRecord(t *testing.T, db *gorm.DB, fixture adminAgentRunFixture) {
	t.Helper()
	threadID := "thread-" + fixture.runID
	thread := model.AgentThread{
		ID: threadID, TenantKind: agentruntime.TenantPersonal, TenantID: fixture.userID,
		CreatedByUserID: fixture.userID, DomainProjectID: fixture.projectID, CanvasID: fixture.canvasID,
		Status: agentruntime.ThreadActive, CreatedAt: fixture.updatedAt.Add(-time.Minute), UpdatedAt: fixture.updatedAt,
	}
	if err := db.Create(&thread).Error; err != nil {
		t.Fatal(err)
	}
	completedAt := (*time.Time)(nil)
	if fixture.status == agentruntime.RunSucceeded || fixture.status == agentruntime.RunFailed || fixture.status == agentruntime.RunCancelled {
		value := fixture.updatedAt
		completedAt = &value
	}
	run := model.AgentRun{
		ID: fixture.runID, ThreadID: threadID, ActorUserID: fixture.userID, ClientRequestID: "request-" + fixture.runID,
		Status: fixture.status, LastEventSequence: 3, StateVersion: 3, StepNumber: 1, MaxSteps: 24,
		ModelRecordID: "agent-model-record", ModelKey: "agent-model",
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion, RuntimeVersion: agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion,
		CreatedAt: fixture.updatedAt.Add(-time.Minute), UpdatedAt: fixture.updatedAt, CompletedAt: completedAt,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
}
