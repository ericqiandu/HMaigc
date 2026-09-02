package database

import (
	"encoding/json"
	"reflect"
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

func TestRetireIncompatiblePausedAgentRuns_PreservesHistoricalTerminalToolCalls(t *testing.T) {
	for _, terminalStatus := range []agentruntime.ToolCallStatus{agentruntime.ToolCallSucceeded, agentruntime.ToolCallFailed} {
		t.Run(string(terminalStatus), func(t *testing.T) {
			db := openAgentRuntimeSchemaSQLite(t)
			if err := MigrateBaseSchema(db); err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, time.August, 24, 11, 40, 0, 0, time.UTC)
			fixture := createIncompatiblePausedRunFixture(t, db, "run-paused-history-"+string(terminalStatus), agentruntime.RunWaitingApproval, now, true)
			historicalStartedAt := now.Add(-time.Minute)
			historicalErrorCode := ""
			if terminalStatus == agentruntime.ToolCallFailed {
				historicalErrorCode = "historical_failure"
			}
			historical := model.AgentToolCall{
				ID: "tool-record-history-" + fixture.runID, RunID: fixture.runID, ToolCallID: "tool-history-" + fixture.runID,
				ActionVersion: 1, ToolName: string(agentruntime.ToolSkillLoad), Status: terminalStatus,
				IdempotencyKey: fixture.runID + ":tool:history", InputJSON: `{}`, OutputJSON: `{"loaded":true}`,
				ErrorCode: historicalErrorCode, StartedAt: &historicalStartedAt, CreatedAt: historicalStartedAt, UpdatedAt: historicalStartedAt,
			}
			if err := db.Create(&historical).Error; err != nil {
				t.Fatal(err)
			}

			if err := retireIncompatiblePausedAgentRuns(db, now.Add(time.Minute)); err != nil {
				t.Fatalf("historical terminal tool call must not block paused-run retirement: %v", err)
			}

			var retiredRun model.AgentRun
			if err := db.First(&retiredRun, "id = ?", fixture.runID).Error; err != nil {
				t.Fatal(err)
			}
			if retiredRun.Status != agentruntime.RunCancelled {
				t.Fatalf("retired run status = %s", retiredRun.Status)
			}
			var preserved model.AgentToolCall
			if err := db.First(&preserved, "id = ?", historical.ID).Error; err != nil {
				t.Fatal(err)
			}
			if preserved.Status != terminalStatus || preserved.StartedAt == nil || !preserved.StartedAt.Equal(historicalStartedAt) || preserved.OutputJSON != historical.OutputJSON || preserved.ErrorCode != historicalErrorCode {
				t.Fatalf("historical terminal tool call was mutated = %#v", preserved)
			}
			var pending model.AgentToolCall
			if err := db.First(&pending, "id = ?", "tool-record-"+fixture.runID).Error; err != nil {
				t.Fatal(err)
			}
			if pending.Status != agentruntime.ToolCallFailed || pending.ErrorCode != retiredAgentRuntimeContractFailureCode {
				t.Fatalf("current pending tool call was not retired = %#v", pending)
			}
		})
	}
}

func TestRetireIncompatiblePausedAgentRuns_PreservesHistoricalTerminalArtifacts(t *testing.T) {
	tests := []struct {
		name          string
		kind          model.AgentProductionArtifactKind
		status        model.AgentProductionArtifactStatus
		resourceID    string
		canvasNodeID  string
		lastErrorCode string
	}{
		{name: "succeeded script", kind: model.AgentProductionArtifactScript, status: model.AgentProductionArtifactSucceeded},
		{name: "failed media", kind: model.AgentProductionArtifactVideoClip, status: model.AgentProductionArtifactFailed, lastErrorCode: "historical_failure"},
		{name: "committed media", kind: model.AgentProductionArtifactStoryboardImage, status: model.AgentProductionArtifactCommitted, resourceID: "resource-history", canvasNodeID: "node-history"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := openAgentRuntimeSchemaSQLite(t)
			if err := MigrateBaseSchema(db); err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, time.August, 24, 11, 50, 0, 0, time.UTC)
			fixture := createIncompatiblePausedRunFixture(t, db, "run-paused-artifact-"+string(test.status), agentruntime.RunWaitingApproval, now, true)
			historical := model.AgentProductionArtifact{
				ID: "artifact-history-" + fixture.runID, PlanKey: "plan-" + fixture.runID,
				PlanVersionID: fixture.planVersionID, PlanVersion: 1, Kind: test.kind, Status: test.status,
				ResourceID: test.resourceID, CanvasNodeID: test.canvasNodeID, LastErrorCode: test.lastErrorCode,
				CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute),
			}
			if err := db.Create(&historical).Error; err != nil {
				t.Fatal(err)
			}

			if err := retireIncompatiblePausedAgentRuns(db, now.Add(time.Minute)); err != nil {
				t.Fatalf("historical terminal artifact must not block paused-run retirement: %v", err)
			}

			var preserved model.AgentProductionArtifact
			if err := db.First(&preserved, "id = ?", historical.ID).Error; err != nil {
				t.Fatal(err)
			}
			if preserved.Status != historical.Status || preserved.ResourceID != historical.ResourceID || preserved.CanvasNodeID != historical.CanvasNodeID || preserved.LastErrorCode != historical.LastErrorCode {
				t.Fatalf("historical terminal artifact was mutated = %#v", preserved)
			}
		})
	}
}

func TestAuditIncompatiblePausedAgentRuns_ReportsAllBlockers(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 24, 18, 0, 0, 0, time.UTC)
	first := createIncompatiblePausedRunFixture(t, db, "run-audit-first", agentruntime.RunWaitingApproval, now, true)
	second := createIncompatiblePausedRunFixture(t, db, "run-audit-second", agentruntime.RunWaitingApproval, now.Add(time.Second), true)

	var firstCheckpoint model.AgentCheckpoint
	if err := db.Where("run_id = ?", first.runID).Take(&firstCheckpoint).Error; err != nil {
		t.Fatal(err)
	}
	var firstState agentruntime.RuntimeState
	if err := json.Unmarshal([]byte(firstCheckpoint.StateJSON), &firstState); err != nil {
		t.Fatal(err)
	}
	firstState.PendingToolStarted = true
	firstState.UserMessage = "audit-user-content-must-not-leak"
	firstStateJSON, err := json.Marshal(firstState)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentCheckpoint{}).Where("id = ?", firstCheckpoint.ID).Update("state_json", string(firstStateJSON)).Error; err != nil {
		t.Fatal(err)
	}
	startedAt := now.Add(2 * time.Second)
	if err := db.Model(&model.AgentToolCall{}).Where("run_id = ?", first.runID).Updates(struct {
		Status    agentruntime.ToolCallStatus `gorm:"column:status"`
		StartedAt time.Time                   `gorm:"column:started_at"`
		InputJSON string                      `gorm:"column:input_json"`
	}{
		Status: agentruntime.ToolCallRunning, StartedAt: startedAt,
		InputJSON: `{"secret":"audit-tool-input-must-not-leak"}`,
	}).Error; err != nil {
		t.Fatal(err)
	}
	firstRun := mustPausedRun(t, db, first.runID)
	if err := db.Create(&model.Task{
		ID: "task-audit-first", UserID: firstRun.ActorUserID, Audience: model.TaskAudienceInternal,
		Type: "agent_runtime_model", Capability: "text", Status: model.TaskStatusRunning,
		Operation: legacyAgentModelTaskOperationPrefix + first.runID, ProviderRequestID: "provider-secret-must-not-leak",
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.BillingOrder{
		ID: "billing-audit-first", UserID: firstRun.ActorUserID,
		IdempotencyKey: "agent-runtime:" + first.runID + ":2", Status: model.BillingStatusReserved,
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentProductionArtifact{}).Where("plan_version_id = ?", first.planVersionID).
		Update("status", model.AgentProductionArtifactQueued).Error; err != nil {
		t.Fatal(err)
	}

	if err := db.Model(&model.AgentRun{}).Where("id = ?", second.runID).Updates(struct {
		RuntimeVersion int `gorm:"column:runtime_version"`
		PolicyVersion  int `gorm:"column:policy_version"`
	}{
		RuntimeVersion: agentruntime.CurrentRuntimeVersion + 1,
		PolicyVersion:  agentruntime.CurrentPolicyVersion + 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentCheckpoint{}).Where("run_id = ?", second.runID).
		Update("state_json", `{"userMessage":"checkpoint-secret-must-not-leak"`).Error; err != nil {
		t.Fatal(err)
	}
	secondRun := mustPausedRun(t, db, second.runID)
	if err := db.Create(&model.BillingOrder{
		ID: "billing-audit-second", UserID: secondRun.ActorUserID,
		IdempotencyKey: "proxy-token:agent-runtime:" + second.runID + ":2", Status: model.BillingStatusUncertain,
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentProductionArtifact{}).Where("plan_version_id = ?", second.planVersionID).
		Update("status", model.AgentProductionArtifactRunning).Error; err != nil {
		t.Fatal(err)
	}

	audit, err := auditIncompatiblePausedAgentRuns(db)
	if err != nil {
		t.Fatal(err)
	}
	if audit.CandidateRuns != 2 || audit.RetirableRuns != 0 {
		t.Fatalf("audit counts = %#v", audit)
	}
	got := make([]string, 0, len(audit.Blockers))
	for _, blocker := range audit.Blockers {
		got = append(got, blocker.RunID+"/"+blocker.Category+"/"+blocker.FactStatus)
	}
	want := []string{
		"run-audit-first/active_or_unknown_artifact/queued",
		"run-audit-first/active_provider_task/running",
		"run-audit-first/pending_tool_started/",
		"run-audit-first/started_tool_call/running",
		"run-audit-first/unresolved_billing/reserved",
		"run-audit-second/active_or_unknown_artifact/running",
		"run-audit-second/checkpoint_decode_invalid/",
		"run-audit-second/future_contract/",
		"run-audit-second/unresolved_billing/uncertain",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("audit blockers = %#v, want %#v", got, want)
	}
	encoded, err := json.Marshal(audit)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"audit-user-content-must-not-leak", "audit-tool-input-must-not-leak", "provider-secret-must-not-leak", "checkpoint-secret-must-not-leak"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("audit leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestAuditIncompatiblePausedAgentRuns_IsReadOnly(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 24, 18, 30, 0, 0, time.UTC)
	createIncompatiblePausedRunFixture(t, db, "run-audit-read-only", agentruntime.RunWaitingApproval, now, true)
	before := pausedRetirementFactsSnapshot(t, db)

	audit, err := auditIncompatiblePausedAgentRuns(db)
	if err != nil {
		t.Fatal(err)
	}
	if audit.CandidateRuns != 1 || audit.RetirableRuns != 1 || len(audit.Blockers) != 0 {
		t.Fatalf("read-only audit = %#v", audit)
	}
	after := pausedRetirementFactsSnapshot(t, db)
	if before != after {
		t.Fatalf("audit mutated facts\nbefore=%s\nafter=%s", before, after)
	}
}

func TestRetireIncompatiblePausedAgentRuns_DoesNotWriteWhenAuditHasBlockers(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 24, 19, 0, 0, 0, time.UTC)
	createIncompatiblePausedRunFixture(t, db, "run-audit-safe", agentruntime.RunWaitingInput, now, false)
	risky := createIncompatiblePausedRunFixture(t, db, "run-audit-risky", agentruntime.RunWaitingApproval, now.Add(time.Second), true)
	if err := db.Model(&model.AgentProductionArtifact{}).Where("plan_version_id = ?", risky.planVersionID).
		Update("status", model.AgentProductionArtifactRunning).Error; err != nil {
		t.Fatal(err)
	}
	before := pausedRetirementFactsSnapshot(t, db)

	err := retireIncompatiblePausedAgentRuns(db, now.Add(time.Minute))
	if err == nil || !strings.Contains(err.Error(), "run-audit-risky") || !strings.Contains(err.Error(), "active_or_unknown_artifact") {
		t.Fatalf("retirement audit rejection = %v", err)
	}
	after := pausedRetirementFactsSnapshot(t, db)
	if before != after {
		t.Fatalf("blocked retirement mutated facts\nbefore=%s\nafter=%s", before, after)
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
			name: "pending tool already terminal",
			mutate: func(t *testing.T, db *gorm.DB, fixture pausedRunFixture, _ time.Time) {
				t.Helper()
				if err := db.Model(&model.AgentToolCall{}).Where("run_id = ?", fixture.runID).
					Update("status", agentruntime.ToolCallSucceeded).Error; err != nil {
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
			name: "unknown production artifact status",
			mutate: func(t *testing.T, db *gorm.DB, fixture pausedRunFixture, _ time.Time) {
				t.Helper()
				if err := db.Model(&model.AgentProductionArtifact{}).Where("plan_version_id = ?", fixture.planVersionID).
					Update("status", "future_supplier_state").Error; err != nil {
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

func mustPausedRun(t *testing.T, db *gorm.DB, runID string) model.AgentRun {
	t.Helper()
	var run model.AgentRun
	if err := db.First(&run, "id = ?", runID).Error; err != nil {
		t.Fatal(err)
	}
	return run
}

func pausedRetirementFactsSnapshot(t *testing.T, db *gorm.DB) string {
	t.Helper()
	snapshot := struct {
		Runs        []model.AgentRun                   `json:"runs"`
		Events      []model.AgentRunEvent              `json:"events"`
		Checkpoints []model.AgentCheckpoint            `json:"checkpoints"`
		Timeline    []model.AgentTimelineItem          `json:"timeline"`
		ToolCalls   []model.AgentToolCall              `json:"toolCalls"`
		Plans       []model.AgentProductionPlanVersion `json:"plans"`
		Artifacts   []model.AgentProductionArtifact    `json:"artifacts"`
		Tasks       []model.Task                       `json:"tasks"`
		Billing     []model.BillingOrder               `json:"billing"`
	}{}
	for _, query := range []struct {
		name string
		run  func() error
	}{
		{name: "runs", run: func() error { return db.Order("id").Find(&snapshot.Runs).Error }},
		{name: "events", run: func() error { return db.Order("id").Find(&snapshot.Events).Error }},
		{name: "checkpoints", run: func() error { return db.Order("id").Find(&snapshot.Checkpoints).Error }},
		{name: "timeline", run: func() error { return db.Order("id").Find(&snapshot.Timeline).Error }},
		{name: "tool calls", run: func() error { return db.Order("id").Find(&snapshot.ToolCalls).Error }},
		{name: "plans", run: func() error { return db.Order("id").Find(&snapshot.Plans).Error }},
		{name: "artifacts", run: func() error { return db.Order("id").Find(&snapshot.Artifacts).Error }},
		{name: "tasks", run: func() error { return db.Order("id").Find(&snapshot.Tasks).Error }},
		{name: "billing", run: func() error { return db.Order("id").Find(&snapshot.Billing).Error }},
	} {
		if err := query.run(); err != nil {
			t.Fatalf("snapshot %s: %v", query.name, err)
		}
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
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
		ModelRecordID: "model-record", ModelKey: "agent-model", ToolSchemaVersion: agentruntime.LegacyToolSchemaVersion,
		RuntimeVersion: agentruntime.LegacyRuntimeVersion, PolicyVersion: agentruntime.LegacyPolicyVersion,
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
