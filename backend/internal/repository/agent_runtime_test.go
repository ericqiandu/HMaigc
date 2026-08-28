package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func repositoryAgentScope() agentruntime.Scope {
	return agentruntime.Scope{
		TenantKind:  agentruntime.TenantPersonal,
		TenantID:    "agent-user-1",
		ActorUserID: "agent-user-1",
		CanvasID:    "agent-canvas-1",
		ThreadID:    "agent-thread-1",
		RunID:       "agent-run-1",
		Access: agentruntime.AccessGrant{
			Level:              agentruntime.AccessManager,
			SubscriptionActive: true,
		},
	}
}

func TestCreateAgentRunIsIdempotentWithinThread(t *testing.T) {
	repo, _ := openAgentRuntimeRepositorySQLite(t)
	now := time.Now().UTC()
	input := CreateAgentRunInput{Scope: repositoryAgentScope(), ClientRequestID: "request-1", Now: now}

	first, err := repo.CreateAgentRun(input)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Created || first.Thread.ID != input.Scope.ThreadID || first.Run.ID != input.Scope.RunID {
		t.Fatalf("first run record = %#v", first)
	}
	replayed, err := repo.CreateAgentRun(input)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Created || replayed.Run.ID != first.Run.ID || replayed.Thread.ID != first.Thread.ID {
		t.Fatalf("idempotent replay = %#v, want run %s", replayed, first.Run.ID)
	}

	secondThread := input
	secondThread.Scope.ThreadID = "agent-thread-2"
	secondThread.Scope.RunID = "agent-run-2"
	second, err := repo.CreateAgentRun(secondThread)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Created || second.Run.ID == first.Run.ID {
		t.Fatalf("same request id in another thread must create an independent run: %#v", second)
	}
}

func TestRetireLegacyAgentRunsLeavesTerminalHistoryUntouched(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	if err := database.EnsureAgentProductionRuntimeSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	terminalScope := legacyRetirementScope("terminal")
	waitingScope := legacyRetirementScope("waiting")
	initializeLegacyRetirementRun(t, repo, terminalScope, now)
	initializeLegacyRetirementRun(t, repo, waitingScope, now.Add(time.Second))

	waitingState, err := repo.LoadAgentCheckpoint(waitingScope)
	if err != nil {
		t.Fatal(err)
	}
	waiting, err := agentruntime.Advance(waitingState, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall,
		ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "legacy-approval", ToolName: agentruntime.ToolProductionRender,
			ActionVersion: 1, Arguments: json.RawMessage(`{"artifactId":"legacy-video"}`),
			ExpectedDelivery: repositoryTestImageDelivery(),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	stageLegacyApprovalBoundary(t, db, waitingScope.RunID, waiting.State, now.Add(2*time.Second))
	completedAt := now.Add(3 * time.Second)
	if err := db.Model(&model.AgentRun{}).Where("id = ?", terminalScope.RunID).
		Updates(agentRunTerminalTestUpdates{Status: agentruntime.RunSucceeded, CompletedAt: completedAt, UpdatedAt: completedAt}).Error; err != nil {
		t.Fatal(err)
	}
	var terminalEventCount int64
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ?", terminalScope.RunID).Count(&terminalEventCount).Error; err != nil {
		t.Fatal(err)
	}

	audit, err := repo.AuditRetirableLegacyAgentRuns(2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 || audit[0].RuntimeVersion != 2 || audit[0].Status != agentruntime.RunWaitingApproval ||
		audit[0].Count != 1 || !audit[0].HasPendingApproval || !audit[0].HasPendingTool || audit[0].HasCheckpointIssue ||
		audit[0].HasPendingTask || audit[0].HasActiveArtifact || audit[0].HasUnresolvedBilling {
		t.Fatalf("legacy retirement audit = %#v", audit)
	}

	retired, err := repo.RetireLegacyAgentRunsAtSafeBoundary(2, 3, agentruntime.FailureRuntimeSchemaRetired)
	if err != nil {
		t.Fatal(err)
	}
	if retired != 1 {
		t.Fatalf("retired runs = %d, want 1", retired)
	}
	assertLegacyRetirementRunStatus(t, db, terminalScope.RunID, agentruntime.RunSucceeded)
	assertLegacyRetirementRunStatus(t, db, waitingScope.RunID, agentruntime.RunFailed)
	var unchangedTerminalEventCount int64
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ?", terminalScope.RunID).Count(&unchangedTerminalEventCount).Error; err != nil {
		t.Fatal(err)
	}
	if unchangedTerminalEventCount != terminalEventCount {
		t.Fatalf("terminal event count = %d, want unchanged %d", unchangedTerminalEventCount, terminalEventCount)
	}
	var terminalEvent model.AgentRunEvent
	if err := db.Where("run_id = ?", waitingScope.RunID).Order("sequence DESC").Take(&terminalEvent).Error; err != nil {
		t.Fatal(err)
	}
	if terminalEvent.Kind != agentruntime.EventRunFailed || !strings.Contains(terminalEvent.PayloadJSON, agentruntime.FailureRuntimeSchemaRetired) {
		t.Fatalf("retirement event = %#v", terminalEvent)
	}
	var errorItem model.AgentTimelineItem
	if err := db.Where("run_id = ? AND kind = ?", waitingScope.RunID, model.AgentTimelineItemError).Take(&errorItem).Error; err != nil {
		t.Fatal(err)
	}
	if errorItem.Status != model.AgentTimelineItemFailed || errorItem.SourceEventSequence != terminalEvent.Sequence ||
		!strings.Contains(errorItem.ContentJSON, agentruntime.FailureRuntimeSchemaRetired) {
		t.Fatalf("retirement timeline item = %#v", errorItem)
	}
	replayed, err := repo.RetireLegacyAgentRunsAtSafeBoundary(2, 3, agentruntime.FailureRuntimeSchemaRetired)
	if err != nil || replayed != 0 {
		t.Fatalf("retirement replay = %d, %v", replayed, err)
	}
}

func TestRetireLegacyAgentRunsBlocksPendingTask(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	if err := database.EnsureAgentProductionRuntimeSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	scope := legacyRetirementScope("task-blocked")
	initializeLegacyRetirementRun(t, repo, scope, now)
	task := model.Task{
		ID: "legacy-task", UserID: scope.ActorUserID, Audience: model.TaskAudienceInternal,
		Type: "agent_runtime_model", Capability: "text", Status: model.TaskStatusRunning,
		Operation: "agent_model:" + scope.RunID, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	audit, err := repo.AuditRetirableLegacyAgentRuns(2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 || !audit[0].HasPendingTask || audit[0].HasActiveArtifact {
		t.Fatalf("blocked legacy retirement audit = %#v", audit)
	}
	retired, err := repo.RetireLegacyAgentRunsAtSafeBoundary(2, 3, agentruntime.FailureRuntimeSchemaRetired)
	if !errors.Is(err, ErrLegacyRunRetirementBlocked) || retired != 0 {
		t.Fatalf("blocked retirement = %d, %v", retired, err)
	}
	assertLegacyRetirementRunStatus(t, db, scope.RunID, agentruntime.RunQueued)
}

func TestRetireLegacyAgentRunsBlocksActiveArtifactTask(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	if err := database.EnsureAgentProductionRuntimeSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	scope := legacyRetirementScope("artifact-task-blocked")
	initializeLegacyRetirementRun(t, repo, scope, now)

	plan := model.AgentProductionPlanVersion{
		ID: "legacy-plan-artifact-task", PlanKey: "legacy-plan", TenantKind: scope.TenantKind,
		TenantID: scope.TenantID, DomainProjectID: scope.DomainProjectID, CanvasID: scope.CanvasID,
		CreatedByRunID: scope.RunID, Version: 1, Status: model.AgentProductionPlanActive,
		Title: "旧计划", TargetDurationMS: 5000, Script: "旧脚本", ReferencesJSON: `[]`, ShotsJSON: `[]`,
		ExpectedDeliveryJSON: `{}`, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	task := model.Task{
		ID: "legacy-artifact-media-task", UserID: scope.ActorUserID, Audience: model.TaskAudienceCustomer,
		Type: "video_generation", Capability: "video", Status: model.TaskStatusRunning,
		Operation: "video.generate", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	artifact := model.AgentProductionArtifact{
		ID: "legacy-running-video", PlanKey: plan.PlanKey, PlanVersionID: plan.ID, PlanVersion: plan.Version,
		ShotKey: "shot-1", Kind: model.AgentProductionArtifactVideoClip, Status: model.AgentProductionArtifactRunning,
		TaskID: task.ID, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&artifact).Error; err != nil {
		t.Fatal(err)
	}

	audit, err := repo.AuditRetirableLegacyAgentRuns(2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 || !audit[0].HasPendingTask || !audit[0].HasActiveArtifact {
		t.Fatalf("active artifact task audit = %#v", audit)
	}
	retired, err := repo.RetireLegacyAgentRunsAtSafeBoundary(2, 3, agentruntime.FailureRuntimeSchemaRetired)
	if !errors.Is(err, ErrLegacyRunRetirementBlocked) || retired != 0 {
		t.Fatalf("active artifact task retirement = %d, %v", retired, err)
	}
	assertLegacyRetirementRunStatus(t, db, scope.RunID, agentruntime.RunQueued)
}

func TestRetireLegacyAgentRunsBlocksLinkedUnresolvedBilling(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	if err := database.EnsureAgentProductionRuntimeSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	scope := legacyRetirementScope("artifact-billing-blocked")
	initializeLegacyRetirementRun(t, repo, scope, now)

	plan := model.AgentProductionPlanVersion{
		ID: "legacy-plan-artifact-billing", PlanKey: "legacy-plan", TenantKind: scope.TenantKind,
		TenantID: scope.TenantID, DomainProjectID: scope.DomainProjectID, CanvasID: scope.CanvasID,
		CreatedByRunID: scope.RunID, Version: 1, Status: model.AgentProductionPlanActive,
		Title: "旧计划", TargetDurationMS: 5000, Script: "旧脚本", ReferencesJSON: `[]`, ShotsJSON: `[]`,
		ExpectedDeliveryJSON: `{}`, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	task := model.Task{
		ID: "legacy-artifact-settled-task", UserID: scope.ActorUserID, Audience: model.TaskAudienceCustomer,
		Type: "video_generation", Capability: "video", Status: model.TaskStatusSucceeded,
		Operation: "video.generate", BillingOrderID: "legacy-artifact-unresolved-order",
		CompletedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	order := model.BillingOrder{
		ID: task.BillingOrderID, UserID: scope.ActorUserID, TaskID: task.ID,
		IdempotencyKey: "media-order-without-runtime-prefix", Status: model.BillingStatusReserved,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	artifact := model.AgentProductionArtifact{
		ID: "legacy-succeeded-video", PlanKey: plan.PlanKey, PlanVersionID: plan.ID, PlanVersion: plan.Version,
		ShotKey: "shot-1", Kind: model.AgentProductionArtifactVideoClip, Status: model.AgentProductionArtifactSucceeded,
		TaskID: task.ID, BillingOrderID: order.ID, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&artifact).Error; err != nil {
		t.Fatal(err)
	}

	audit, err := repo.AuditRetirableLegacyAgentRuns(2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 || audit[0].HasPendingTask || audit[0].HasActiveArtifact || !audit[0].HasUnresolvedBilling {
		t.Fatalf("linked unresolved billing audit = %#v", audit)
	}
	retired, err := repo.RetireLegacyAgentRunsAtSafeBoundary(2, 3, agentruntime.FailureRuntimeSchemaRetired)
	if !errors.Is(err, ErrLegacyRunRetirementBlocked) || retired != 0 {
		t.Fatalf("linked unresolved billing retirement = %d, %v", retired, err)
	}
	assertLegacyRetirementRunStatus(t, db, scope.RunID, agentruntime.RunQueued)
}

func TestRetireLegacyAgentRunsReportsInvalidCheckpointAsBlocker(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	if err := database.EnsureAgentProductionRuntimeSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	scope := legacyRetirementScope("checkpoint-blocked")
	initializeLegacyRetirementRun(t, repo, scope, now)
	if err := db.Model(&model.AgentCheckpoint{}).Where("run_id = ?", scope.RunID).
		Update("state_json", `{"stateVersion":1}`).Error; err != nil {
		t.Fatal(err)
	}
	task := model.Task{
		ID: "legacy-invalid-checkpoint-task", UserID: scope.ActorUserID, Audience: model.TaskAudienceInternal,
		Type: "agent_runtime_model", Capability: "text", Status: model.TaskStatusRunning,
		Operation: "agent_model:" + scope.RunID, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	audit, err := repo.AuditRetirableLegacyAgentRuns(2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 || !audit[0].HasCheckpointIssue || !audit[0].HasPendingTask {
		t.Fatalf("invalid checkpoint audit = %#v", audit)
	}
	retired, err := repo.RetireLegacyAgentRunsAtSafeBoundary(2, 3, agentruntime.FailureRuntimeSchemaRetired)
	if !errors.Is(err, ErrLegacyRunRetirementBlocked) || retired != 0 {
		t.Fatalf("invalid checkpoint retirement = %d, %v", retired, err)
	}
	assertLegacyRetirementRunStatus(t, db, scope.RunID, agentruntime.RunQueued)
}

type agentRunTerminalTestUpdates struct {
	Status      agentruntime.RunStatus `gorm:"column:status"`
	CompletedAt time.Time              `gorm:"column:completed_at"`
	UpdatedAt   time.Time              `gorm:"column:updated_at"`
}

type agentRunApprovalBoundaryTestUpdates struct {
	Status       agentruntime.RunStatus `gorm:"column:status"`
	StateVersion int                    `gorm:"column:state_version"`
	StepNumber   int                    `gorm:"column:step_number"`
	UpdatedAt    time.Time              `gorm:"column:updated_at"`
}

type agentCheckpointStateTestUpdates struct {
	StateVersion int    `gorm:"column:state_version"`
	StateJSON    string `gorm:"column:state_json"`
}

func legacyRetirementScope(suffix string) agentruntime.Scope {
	scope := repositoryAgentScope()
	scope.ThreadID = "legacy-thread-" + suffix
	scope.RunID = "legacy-run-" + suffix
	return scope
}

func initializeLegacyRetirementRun(t *testing.T, repo *Repository, scope agentruntime.Scope, now time.Time) {
	t.Helper()
	createAgentRunForTest(t, repo, scope)
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "legacy-model-record", ModelKey: "deepseek-v4",
		MaxSteps: 6, ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion: agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion,
		UserMessage: "创建一个短片", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
}

// stageLegacyApprovalBoundary persists the pure runtime transition without
// invoking the production-render repository adapter. Retirement tests exercise
// runtime-version handling, not the separate frozen render contract.
func stageLegacyApprovalBoundary(t *testing.T, db *gorm.DB, runID string, state agentruntime.RuntimeState, now time.Time) {
	t.Helper()
	stateJSON, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.AgentRun{}).Where("id = ?", runID).
			Select("status", "state_version", "step_number", "updated_at").
			Updates(agentRunApprovalBoundaryTestUpdates{
				Status: state.Status, StateVersion: state.StateVersion, StepNumber: state.StepNumber, UpdatedAt: now,
			}).Error; err != nil {
			return err
		}
		return tx.Model(&model.AgentCheckpoint{}).Where("run_id = ? AND sequence = 2", runID).
			Select("state_version", "state_json").
			Updates(agentCheckpointStateTestUpdates{StateVersion: state.StateVersion, StateJSON: string(stateJSON)}).Error
	}); err != nil {
		t.Fatal(err)
	}
}

func assertLegacyRetirementRunStatus(t *testing.T, db *gorm.DB, runID string, want agentruntime.RunStatus) {
	t.Helper()
	var run model.AgentRun
	if err := db.First(&run, "id = ?", runID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != want {
		t.Fatalf("run %s status = %s, want %s", runID, run.Status, want)
	}
}

func TestStaleAgentRunsAfterSelectsOnlyOldActiveRunsWithPrimaryKeyCursor(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	now := time.Now().UTC()
	for index, runID := range []string{"active-run-a", "active-run-b", "active-run-c"} {
		scope := repositoryAgentScope()
		scope.ThreadID = fmt.Sprintf("active-thread-%d", index)
		scope.RunID = runID
		if _, err := repo.CreateAgentRun(CreateAgentRunInput{Scope: scope, ClientRequestID: fmt.Sprintf("active-request-%d", index), Now: now.Add(time.Duration(index) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&model.AgentRun{}).Where("id = ?", "active-run-b").Update("status", agentruntime.RunWaitingApproval).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentRun{}).Where("id IN ?", []string{"active-run-a", "active-run-c"}).Update("updated_at", now.Add(-2*time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	first, err := repo.StaleAgentRunsAfter("", now.Add(-time.Minute), 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.StaleAgentRunsAfter(first[0].RunID, now.Add(-time.Minute), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].RunID != "active-run-a" || len(second) != 1 || second[0].RunID != "active-run-c" {
		t.Fatalf("active run cursor pages = %#v then %#v", first, second)
	}
}

func TestCreateAgentRunRejectsThreadScopeConflict(t *testing.T) {
	repo, _ := openAgentRuntimeRepositorySQLite(t)
	now := time.Now().UTC()
	input := CreateAgentRunInput{Scope: repositoryAgentScope(), ClientRequestID: "request-conflict", Now: now}
	if _, err := repo.CreateAgentRun(input); err != nil {
		t.Fatal(err)
	}

	conflict := input
	conflict.Scope.RunID = "agent-run-conflict"
	conflict.Scope.CanvasID = "other-canvas"
	conflict.ClientRequestID = "request-conflict-2"
	if _, err := repo.CreateAgentRun(conflict); !errors.Is(err, ErrAgentScopeConflict) {
		t.Fatalf("thread scope conflict error = %v", err)
	}
}

func TestAgentRunScopeIsolationIsEnforcedByQuery(t *testing.T) {
	repo, _ := openAgentRuntimeRepositorySQLite(t)
	input := CreateAgentRunInput{Scope: repositoryAgentScope(), ClientRequestID: "request-isolation", Now: time.Now().UTC()}
	if _, err := repo.CreateAgentRun(input); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AgentRunForScope(input.Scope); err != nil {
		t.Fatal(err)
	}

	otherActor := input.Scope
	otherActor.TenantID = "agent-user-2"
	otherActor.ActorUserID = "agent-user-2"
	if _, err := repo.AgentRunForScope(otherActor); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-user read error = %v", err)
	}

	otherCanvas := input.Scope
	otherCanvas.CanvasID = "other-canvas"
	if _, err := repo.AgentRunForScope(otherCanvas); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-canvas read error = %v", err)
	}
}

func TestAppendAgentEventPersistsMatchingCheckpointAndReadsAfterSequence(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC()
	event, err := repo.AppendAgentEvent(AppendAgentEventInput{
		Scope: scope, Kind: agentruntime.EventRunCreated, PayloadJSON: `{"status":"queued"}`,
		Checkpoint: &AgentCheckpointInput{StateVersion: 1, StateJSON: `{"step":0}`}, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if event.Sequence != 1 || event.RunID != scope.RunID || event.Kind != agentruntime.EventRunCreated {
		t.Fatalf("event = %#v", event)
	}
	var checkpoint model.AgentCheckpoint
	if err := db.First(&checkpoint, "run_id = ? AND sequence = ?", scope.RunID, 1).Error; err != nil {
		t.Fatal(err)
	}
	if checkpoint.StateVersion != 1 || checkpoint.StateJSON != `{"step":0}` {
		t.Fatalf("checkpoint = %#v", checkpoint)
	}

	if _, err := repo.AppendAgentEvent(AppendAgentEventInput{
		Scope: scope, Kind: agentruntime.EventRunStatusChanged, PayloadJSON: `{"status":"running"}`, Now: now.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	events, err := repo.AgentRunEventsAfter(scope, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Sequence != 2 || events[0].Kind != agentruntime.EventRunStatusChanged {
		t.Fatalf("events after sequence 1 = %#v", events)
	}

	otherActor := scope
	otherActor.TenantID = "agent-user-other"
	otherActor.ActorUserID = "agent-user-other"
	if _, err := repo.AgentRunEventsAfter(otherActor, 0, 10); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-tenant event read error = %v", err)
	}
}

func TestAppendAgentEventRejectsInvalidBoundsWithoutAdvancingRun(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC()
	invalid := []AppendAgentEventInput{
		{Scope: scope, Kind: agentruntime.EventKind("storyboard.route"), PayloadJSON: `{}`, Now: now},
		{Scope: scope, Kind: agentruntime.EventRunCreated, PayloadJSON: `{broken`, Now: now},
		{Scope: scope, Kind: agentruntime.EventRunCreated, PayloadJSON: `{"data":"` + strings.Repeat("x", agentEventPayloadLimit) + `"}`, Now: now},
		{Scope: scope, Kind: agentruntime.EventRunCreated, PayloadJSON: `{}`, Checkpoint: &AgentCheckpointInput{StateVersion: 0, StateJSON: `{}`}, Now: now},
	}
	for index, input := range invalid {
		if _, err := repo.AppendAgentEvent(input); err == nil {
			t.Fatalf("invalid event %d accepted", index)
		}
	}
	for _, limit := range []int{0, 501} {
		if _, err := repo.AgentRunEventsAfter(scope, 0, limit); err == nil {
			t.Fatalf("invalid event read limit %d accepted", limit)
		}
	}
	var run model.AgentRun
	if err := db.First(&run, "id = ?", scope.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if run.LastEventSequence != 0 {
		t.Fatalf("invalid event advanced run sequence to %d", run.LastEventSequence)
	}
}

func TestAppendAgentEventRollsBackSequenceAndEventWhenCheckpointFails(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	const callback = "agent_checkpoint_failure"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*model.AgentCheckpoint); ok {
			tx.AddError(errors.New("checkpoint insert failed"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })

	_, err := repo.AppendAgentEvent(AppendAgentEventInput{
		Scope: scope, Kind: agentruntime.EventRunCreated, PayloadJSON: `{}`,
		Checkpoint: &AgentCheckpointInput{StateVersion: 1, StateJSON: `{}`}, Now: time.Now().UTC(),
	})
	if err == nil || err.Error() != "checkpoint insert failed" {
		t.Fatalf("checkpoint failure error = %v", err)
	}
	var run model.AgentRun
	if err := db.First(&run, "id = ?", scope.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if run.LastEventSequence != 0 {
		t.Fatalf("failed checkpoint advanced run sequence to %d", run.LastEventSequence)
	}
	var eventCount int64
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ?", scope.RunID).Count(&eventCount).Error; err != nil || eventCount != 0 {
		t.Fatalf("event transaction leaked: count=%d err=%v", eventCount, err)
	}
}

func TestAppendAgentEventAllocatesContinuousSequencesConcurrently(t *testing.T) {
	repo, _ := openAgentRuntimeRepositorySQLiteFile(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	const workers = 16
	sequences := make(chan int64, workers)
	errs := make(chan error, workers)
	var wait sync.WaitGroup
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			event, err := repo.AppendAgentEvent(AppendAgentEventInput{
				Scope: scope, Kind: agentruntime.EventModelDelta,
				PayloadJSON: fmt.Sprintf(`{"index":%d}`, index), Now: time.Now().UTC(),
			})
			if err != nil {
				errs <- err
				return
			}
			sequences <- event.Sequence
		}(index)
	}
	wait.Wait()
	close(errs)
	close(sequences)
	for err := range errs {
		t.Fatalf("concurrent append failed: %v", err)
	}
	actual := make([]int, 0, workers)
	for sequence := range sequences {
		actual = append(actual, int(sequence))
	}
	sort.Ints(actual)
	if len(actual) != workers {
		t.Fatalf("sequence count = %d, want %d", len(actual), workers)
	}
	for index, sequence := range actual {
		if sequence != index+1 {
			t.Fatalf("sequences = %v", actual)
		}
	}
}

func createAgentRunForTest(t *testing.T, repo *Repository, scope agentruntime.Scope) {
	t.Helper()
	if _, err := repo.CreateAgentRun(CreateAgentRunInput{Scope: scope, ClientRequestID: "request-for-" + scope.RunID, Now: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
}

func openAgentRuntimeRepositorySQLite(t *testing.T) (*Repository, *gorm.DB) {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared&_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.AgentThread{},
		&model.AgentRun{},
		&model.AgentTimelineItem{},
		&model.AgentRunEvent{},
		&model.AgentCheckpoint{},
		&model.AgentToolCall{},
		&model.AgentProductionPlanVersion{},
		&model.AgentProductionArtifact{},
		&model.Task{},
		&model.BillingOrder{},
	); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	return New(db), db
}

func openAgentRuntimeRepositorySQLiteFile(t *testing.T) (*Repository, *gorm.DB) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "agent-runtime.db") + "?_busy_timeout=10000&_journal_mode=WAL"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(16)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.AgentThread{}, &model.AgentRun{}, &model.AgentTimelineItem{}, &model.AgentRunEvent{}, &model.AgentCheckpoint{}, &model.AgentToolCall{}, &model.AgentProductionPlanVersion{}, &model.AgentProductionArtifact{}, &model.Task{}, &model.BillingOrder{}); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	return New(db), db
}
