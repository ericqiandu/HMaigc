package database

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func TestRetireIncompatiblePausedAgentRuns(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 24, 11, 30, 0, 0, time.UTC)
	waitingInput := createIncompatiblePausedRunFixture(t, db, "run-paused-input", agentruntime.RunWaitingInput, now, false)
	waitingApproval := createIncompatiblePausedRunFixture(t, db, "run-paused-approval", agentruntime.RunWaitingApproval, now.Add(time.Second), true)

	if err := retireIncompatiblePausedAgentRuns(db, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	for _, fixture := range []pausedRunFixture{waitingInput, waitingApproval} {
		var run model.AgentRun
		if err := db.First(&run, "id = ?", fixture.runID).Error; err != nil {
			t.Fatal(err)
		}
		if run.Status != agentruntime.RunCancelled || run.StateVersion != fixture.stateVersion+1 || run.LastEventSequence != fixture.sequence+1 || run.CompletedAt == nil {
			t.Fatalf("retired run %s = %#v", fixture.runID, run)
		}
		var events []model.AgentRunEvent
		if err := db.Where("run_id = ?", fixture.runID).Order("sequence").Find(&events).Error; err != nil {
			t.Fatal(err)
		}
		if len(events) != 2 || events[1].Kind != agentruntime.EventRunInterrupted {
			t.Fatalf("retirement events for %s = %#v", fixture.runID, events)
		}
		var checkpoints []model.AgentCheckpoint
		if err := db.Where("run_id = ?", fixture.runID).Order("sequence").Find(&checkpoints).Error; err != nil {
			t.Fatal(err)
		}
		if len(checkpoints) != 2 || checkpoints[1].StateVersion != fixture.stateVersion+1 {
			t.Fatalf("retirement checkpoints for %s = %#v", fixture.runID, checkpoints)
		}
		var timeline model.AgentTimelineItem
		if err := db.First(&timeline, "run_id = ?", fixture.runID).Error; err != nil {
			t.Fatal(err)
		}
		if timeline.Status != model.AgentTimelineItemInterrupted || timeline.CompletedAt == nil {
			t.Fatalf("retirement timeline for %s = %#v", fixture.runID, timeline)
		}
	}

	var toolCall model.AgentToolCall
	if err := db.First(&toolCall, "run_id = ?", waitingApproval.runID).Error; err != nil {
		t.Fatal(err)
	}
	if toolCall.Status != agentruntime.ToolCallFailed || toolCall.ErrorCode != retiredAgentRuntimeContractFailureCode {
		t.Fatalf("retirement tool call = %#v", toolCall)
	}
	var artifact model.AgentProductionArtifact
	if err := db.First(&artifact, "plan_version_id = ?", waitingApproval.planVersionID).Error; err != nil {
		t.Fatal(err)
	}
	if artifact.Status != model.AgentProductionArtifactFailed || artifact.LastErrorCode != retiredAgentRuntimeContractFailureCode {
		t.Fatalf("retirement artifact = %#v", artifact)
	}
}

func TestEnsureAgentRuntimeIntegritySchemaRetiresIncompatiblePausedRun(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 24, 11, 45, 0, 0, time.UTC)
	fixture := createIncompatiblePausedRunFixture(t, db, "run-schema-paused", agentruntime.RunWaitingApproval, now, true)

	if err := EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatalf("schema upgrade should retire safe paused run: %v", err)
	}
	var run model.AgentRun
	if err := db.First(&run, "id = ?", fixture.runID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != agentruntime.RunCancelled || run.CompletedAt == nil {
		t.Fatalf("schema-retired run = %#v", run)
	}
}

func TestRetireIncompatiblePausedAgentRuns_RejectsRiskFacts(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(t *testing.T, db *gorm.DB, fixture pausedRunFixture, now time.Time)
	}{
		{
			name: "started tool call",
			mutate: func(t *testing.T, db *gorm.DB, fixture pausedRunFixture, now time.Time) {
				t.Helper()
				if err := db.Model(&model.AgentToolCall{}).Where("run_id = ?", fixture.runID).
					Updates(struct {
						Status    agentruntime.ToolCallStatus `gorm:"column:status"`
						StartedAt time.Time                   `gorm:"column:started_at"`
					}{Status: agentruntime.ToolCallRunning, StartedAt: now}).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "submitted provider request",
			mutate: func(t *testing.T, db *gorm.DB, fixture pausedRunFixture, now time.Time) {
				t.Helper()
				var run model.AgentRun
				if err := db.First(&run, "id = ?", fixture.runID).Error; err != nil {
					t.Fatal(err)
				}
				task := model.Task{
					ID: "task-provider-" + fixture.runID, UserID: run.ActorUserID, Audience: model.TaskAudienceInternal,
					Type: "agent_runtime_model", Capability: "text", Status: model.TaskStatusRunning,
					Operation: "agent_model:" + fixture.runID, ProviderRequestID: "provider-request-1",
					CreatedAt: now, UpdatedAt: now,
				}
				if err := db.Create(&task).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "unresolved billing order",
			mutate: func(t *testing.T, db *gorm.DB, fixture pausedRunFixture, now time.Time) {
				t.Helper()
				var run model.AgentRun
				if err := db.First(&run, "id = ?", fixture.runID).Error; err != nil {
					t.Fatal(err)
				}
				order := model.BillingOrder{
					ID: "billing-" + fixture.runID, UserID: run.ActorUserID,
					IdempotencyKey: "agent-runtime:" + fixture.runID + ":2", Status: model.BillingStatusReserved,
					CreatedAt: now, UpdatedAt: now,
				}
				if err := db.Create(&order).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "active production artifact",
			mutate: func(t *testing.T, db *gorm.DB, fixture pausedRunFixture, _ time.Time) {
				t.Helper()
				if err := db.Model(&model.AgentProductionArtifact{}).Where("plan_version_id = ?", fixture.planVersionID).
					Update("status", model.AgentProductionArtifactQueued).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "checkpoint mismatch",
			mutate: func(t *testing.T, db *gorm.DB, fixture pausedRunFixture, _ time.Time) {
				t.Helper()
				if err := db.Model(&model.AgentCheckpoint{}).Where("run_id = ?", fixture.runID).
					Update("state_version", fixture.stateVersion-1).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			db := openAgentRuntimeSchemaSQLite(t)
			if err := MigrateBaseSchema(db); err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
			fixture := createIncompatiblePausedRunFixture(t, db, "run-risk", agentruntime.RunWaitingApproval, now, true)
			mutation.mutate(t, db, fixture, now.Add(time.Second))

			err := retireIncompatiblePausedAgentRuns(db, now.Add(time.Minute))
			if err == nil || !strings.Contains(err.Error(), agentRuntimeRetirementInvalidCode) || !strings.Contains(err.Error(), fixture.runID) {
				t.Fatalf("risk rejection = %v", err)
			}
			assertPausedRunUnchanged(t, db, fixture)
		})
	}
}

func TestRetireIncompatiblePausedAgentRuns_RollsBackBatch(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 24, 12, 30, 0, 0, time.UTC)
	safe := createIncompatiblePausedRunFixture(t, db, "run-batch-safe", agentruntime.RunWaitingInput, now, false)
	risky := createIncompatiblePausedRunFixture(t, db, "run-batch-risk", agentruntime.RunWaitingApproval, now.Add(time.Second), true)
	if err := db.Model(&model.AgentToolCall{}).Where("run_id = ?", risky.runID).Update("started_at", now.Add(time.Second)).Error; err != nil {
		t.Fatal(err)
	}

	if err := retireIncompatiblePausedAgentRuns(db, now.Add(time.Minute)); err == nil {
		t.Fatal("mixed retirement batch unexpectedly succeeded")
	}
	assertPausedRunUnchanged(t, db, safe)
	assertPausedRunUnchanged(t, db, risky)
}

func TestRetireIncompatiblePausedAgentRuns_IsIdempotent(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 24, 13, 0, 0, 0, time.UTC)
	fixture := createIncompatiblePausedRunFixture(t, db, "run-idempotent", agentruntime.RunWaitingInput, now, false)
	if err := retireIncompatiblePausedAgentRuns(db, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := retireIncompatiblePausedAgentRuns(db, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var eventCount int64
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ?", fixture.runID).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	var checkpointCount int64
	if err := db.Model(&model.AgentCheckpoint{}).Where("run_id = ?", fixture.runID).Count(&checkpointCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 2 || checkpointCount != 2 {
		t.Fatalf("idempotent fact counts = events:%d checkpoints:%d", eventCount, checkpointCount)
	}
}

func TestRetireIncompatiblePausedAgentRuns_RejectsFutureContract(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 24, 13, 30, 0, 0, time.UTC)
	fixture := createIncompatiblePausedRunFixture(t, db, "run-future", agentruntime.RunWaitingInput, now, false)
	if err := db.Model(&model.AgentRun{}).Where("id = ?", fixture.runID).
		Update("runtime_version", agentruntime.CurrentRuntimeVersion+1).Error; err != nil {
		t.Fatal(err)
	}

	err := retireIncompatiblePausedAgentRuns(db, now.Add(time.Minute))
	if err == nil || !strings.Contains(err.Error(), agentRuntimeRetirementInvalidCode) || !strings.Contains(err.Error(), fixture.runID) {
		t.Fatalf("future contract rejection = %v", err)
	}
	assertPausedRunUnchanged(t, db, fixture)
}

func assertPausedRunUnchanged(t *testing.T, db *gorm.DB, fixture pausedRunFixture) {
	t.Helper()
	var run model.AgentRun
	if err := db.First(&run, "id = ?", fixture.runID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status == agentruntime.RunCancelled || run.StateVersion != fixture.stateVersion || run.LastEventSequence != fixture.sequence || run.CompletedAt != nil {
		t.Fatalf("paused run mutated after rejection = %#v", run)
	}
	var eventCount int64
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ?", fixture.runID).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("paused run event count = %d", eventCount)
	}
}

type pausedRunFixture struct {
	runID         string
	planVersionID string
	stateVersion  int
	sequence      int64
}

func createIncompatiblePausedRunFixture(
	t *testing.T,
	db *gorm.DB,
	runID string,
	status agentruntime.RunStatus,
	now time.Time,
	withApprovalFacts bool,
) pausedRunFixture {
	t.Helper()
	const stateVersion = 4
	const sequence = int64(7)
	threadID := "thread-" + runID
	actorUserID := "user-" + runID
	thread := model.AgentThread{
		ID: threadID, TenantKind: agentruntime.TenantPersonal, TenantID: actorUserID,
		CreatedByUserID: actorUserID, DomainProjectID: "project-" + runID, CanvasID: "canvas-" + runID,
		Status: agentruntime.ThreadActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&thread).Error; err != nil {
		t.Fatal(err)
	}
	state := agentruntime.RuntimeState{
		StateVersion: stateVersion, StepNumber: 2, MaxSteps: 24, Status: status,
		UserMessage: "创建 5 秒测试视频", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic},
	}
	delivery := agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryAnswer,
		RequiredArtifacts:  []agentruntime.ArtifactKind{agentruntime.ArtifactText},
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}},
	}
	state.ExpectedDelivery = &delivery
	if status == agentruntime.RunWaitingInput {
		state.PendingClarification = &agentruntime.PendingClarification{
			Request: agentruntime.ClarificationDecision{
				RequestID: "clarification-" + runID,
				Questions: []agentruntime.ClarificationQuestion{{
					ID: "question-1", Prompt: "请确认画面风格", Type: agentruntime.ClarificationFreeText,
				}},
				ExpectedDelivery: delivery,
			},
			Answers: []agentruntime.ClarificationAnswer{},
		}
	}
	if withApprovalFacts {
		state.PendingToolCall = &agentruntime.ToolCallDecision{
			ToolCallID: "tool-" + runID, ToolName: agentruntime.ToolProductionRender, ActionVersion: 1,
			Arguments:        json.RawMessage(`{"planKey":"plan-test","baseVersion":1}`),
			ExpectedDelivery: delivery,
		}
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	run := model.AgentRun{
		ID: runID, ThreadID: threadID, ActorUserID: thread.CreatedByUserID, ClientRequestID: "request-" + runID,
		Status: status, LastEventSequence: sequence, StateVersion: stateVersion, StepNumber: state.StepNumber, MaxSteps: state.MaxSteps,
		ModelRecordID: "model-record", ModelKey: "agent-model", ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion: agentruntime.CurrentRuntimeVersion - 1, PolicyVersion: agentruntime.CurrentPolicyVersion - 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AgentRunEvent{
		ID: "event-" + runID, RunID: runID, Sequence: sequence,
		Kind: agentruntime.EventRunStatusChanged, PayloadJSON: string(stateJSON), CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AgentCheckpoint{
		ID: "checkpoint-" + runID, RunID: runID, Sequence: sequence,
		StateVersion: stateVersion, StateJSON: string(stateJSON), CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AgentTimelineItem{
		ID: "timeline-" + runID, TenantKind: thread.TenantKind, TenantID: thread.TenantID,
		ThreadID: threadID, RunID: runID, Kind: model.AgentTimelineItemStatusKind,
		Status: model.AgentTimelineItemInProgress, Ordinal: 1, SourceEventSequence: sequence,
		ContentJSON: `{"label":"准备中"}`, StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	fixture := pausedRunFixture{runID: runID, stateVersion: stateVersion, sequence: sequence}
	if !withApprovalFacts {
		return fixture
	}
	toolCall := model.AgentToolCall{
		ID: "tool-record-" + runID, RunID: runID, ToolCallID: state.PendingToolCall.ToolCallID,
		ActionVersion: 1, ToolName: string(state.PendingToolCall.ToolName), Status: agentruntime.ToolCallWaitingApproval,
		ApprovalRequired: true, IdempotencyKey: runID + ":tool:1", InputJSON: string(state.PendingToolCall.Arguments),
		OutputJSON: `{}`, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&toolCall).Error; err != nil {
		t.Fatal(err)
	}
	planVersionID := "plan-version-" + runID
	plan := model.AgentProductionPlanVersion{
		ID: planVersionID, PlanKey: "plan-" + runID, TenantKind: thread.TenantKind, TenantID: thread.TenantID,
		DomainProjectID: thread.DomainProjectID, CanvasID: thread.CanvasID, CreatedByRunID: runID,
		Version: 1, Status: model.AgentProductionPlanActive, Title: "测试计划", TargetDurationMS: 5000,
		Script: "测试脚本", ReferencesJSON: `[]`, ShotsJSON: `[]`, ExpectedDeliveryJSON: `{}`, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	artifact := model.AgentProductionArtifact{
		ID: "artifact-" + runID, PlanKey: plan.PlanKey, PlanVersionID: plan.ID, PlanVersion: 1,
		Kind: model.AgentProductionArtifactVideoClip, Status: model.AgentProductionArtifactAwaitingApproval,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&artifact).Error; err != nil {
		t.Fatal(err)
	}
	fixture.planVersionID = planVersionID
	return fixture
}
