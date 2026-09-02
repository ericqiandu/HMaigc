package database

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

func TestToolSchemaV8CutoverRetiresSafeV7RunsAndPreservesCurrentAndTerminalFacts(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.September, 2, 11, 0, 0, 0, time.UTC)
	queued := agentruntime.RuntimeState{
		StateVersion: 1, StepNumber: 0, MaxSteps: 8, Status: agentruntime.RunQueued,
		UserMessage: "分析图片", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided},
	}
	waiting := agentruntime.RuntimeState{
		StateVersion: 2, StepNumber: 1, MaxSteps: 8, Status: agentruntime.RunWaitingApproval,
		UserMessage: "生成图片", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided},
		PendingToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "media-v7", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
			Arguments: json.RawMessage(`{"mediaKind":"image","modelRecordId":"image-record","modelKey":"image-model","parameters":{"prompt":"红色方块"},"sourceResourceIds":[],"targetCanvasNodeId":"node-1","clientRequestId":"media-v7"}`),
			ExpectedDelivery: agentruntime.ExpectedDelivery{
				Kind: agentruntime.DeliveryGeneratedAsset, RequiredArtifacts: []agentruntime.ArtifactKind{agentruntime.ArtifactImage},
				CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactTaskBackedResource, Artifact: agentruntime.ArtifactImage}},
			},
		},
	}
	seedState := func(runID string, state agentruntime.RuntimeState, createdAt time.Time) {
		t.Helper()
		encoded, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		run := model.AgentRun{
			ID: runID, ThreadID: "thread-" + runID, ActorUserID: "user-v7", ClientRequestID: "request-" + runID,
			Status: state.Status, LastEventSequence: 1, StateVersion: state.StateVersion, StepNumber: state.StepNumber, MaxSteps: state.MaxSteps,
			ModelRecordID: "agent-record", ModelKey: "agent-model", ToolSchemaVersion: agentruntime.RetiredCloudToolSchemaVersion,
			RuntimeVersion: agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion,
			CreatedAt: createdAt, UpdatedAt: createdAt,
		}
		if err := db.Create(&run).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.AgentRunEvent{
			ID: "event-" + runID, RunID: runID, Sequence: 1, Kind: agentruntime.EventRunCreated,
			PayloadJSON: string(encoded), CreatedAt: createdAt,
		}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.AgentCheckpoint{
			ID: "checkpoint-" + runID, RunID: runID, Sequence: 1,
			StateVersion: state.StateVersion, StateJSON: string(encoded), CreatedAt: createdAt,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	seedState("v7-queued", queued, now)
	seedState("v7-waiting", waiting, now.Add(time.Second))

	running := model.AgentRun{
		ID: "v7-running", ThreadID: "thread-v7-running", ActorUserID: "user-v7", ClientRequestID: "request-v7-running",
		Status: agentruntime.RunRunning, ToolSchemaVersion: agentruntime.RetiredCloudToolSchemaVersion,
		RuntimeVersion: agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion,
		CreatedAt: now.Add(2 * time.Second), UpdatedAt: now.Add(2 * time.Second),
	}
	current := model.AgentRun{
		ID: "v8-current", ThreadID: "thread-v8-current", ActorUserID: "user-v8", ClientRequestID: "request-v8-current",
		Status: agentruntime.RunQueued, ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion: agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion,
		CreatedAt: now.Add(3 * time.Second), UpdatedAt: now.Add(3 * time.Second),
	}
	completedAt := now.Add(-time.Minute)
	terminal := model.AgentRun{
		ID: "v7-terminal", ThreadID: "thread-v7-terminal", ActorUserID: "user-v7", ClientRequestID: "request-v7-terminal",
		Status: agentruntime.RunSucceeded, ToolSchemaVersion: agentruntime.RetiredCloudToolSchemaVersion,
		RuntimeVersion: agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion,
		CreatedAt: now.Add(-time.Hour), UpdatedAt: completedAt, CompletedAt: &completedAt,
	}
	if err := db.Create(&[]model.AgentRun{running, current, terminal}).Error; err != nil {
		t.Fatal(err)
	}

	audit, err := AuditAgentRuntimeUpgrade(db)
	if err != nil {
		t.Fatal(err)
	}
	if audit.CandidateRuns != 3 || audit.RetirableRuns != 2 || !upgradeAuditHasBlocker(audit, running.ID, agentRuntimeUpgradeBlockerNonRetirableStatus) {
		t.Fatalf("v8 cutover audit = %#v", audit)
	}
	if err := db.Model(&model.AgentRun{}).Where("id = ?", running.ID).Update("status", agentruntime.RunCancelled).Error; err != nil {
		t.Fatal(err)
	}
	if err := EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatalf("retire safe v7 runs: %v", err)
	}
	for _, expected := range []struct {
		runID  string
		status agentruntime.RunStatus
	}{
		{runID: "v7-queued", status: agentruntime.RunFailed},
		{runID: "v7-waiting", status: agentruntime.RunCancelled},
	} {
		var run model.AgentRun
		if err := db.First(&run, "id = ?", expected.runID).Error; err != nil {
			t.Fatal(err)
		}
		if run.Status != expected.status || run.ToolSchemaVersion != agentruntime.RetiredCloudToolSchemaVersion {
			t.Fatalf("retired v7 run %s = %#v", expected.runID, run)
		}
		if expected.status == agentruntime.RunFailed {
			var checkpoint model.AgentCheckpoint
			if err := db.Order("sequence DESC").First(&checkpoint, "run_id = ?", expected.runID).Error; err != nil {
				t.Fatal(err)
			}
			var terminalState agentruntime.RuntimeState
			if err := json.Unmarshal([]byte(checkpoint.StateJSON), &terminalState); err != nil {
				t.Fatal(err)
			}
			if terminalState.FailureCode != agentruntime.FailureRuntimeSchemaRetired {
				t.Fatalf("retired v7 checkpoint %s = %#v", expected.runID, terminalState)
			}
		} else {
			var event model.AgentRunEvent
			if err := db.Order("sequence DESC").First(&event, "run_id = ?", expected.runID).Error; err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(event.PayloadJSON, `"reason":"`+agentruntime.FailureRuntimeSchemaRetired+`"`) {
				t.Fatalf("retired v7 waiting event %s = %s", expected.runID, event.PayloadJSON)
			}
		}
	}
	for _, expected := range []model.AgentRun{current, terminal} {
		var stored model.AgentRun
		if err := db.First(&stored, "id = ?", expected.ID).Error; err != nil {
			t.Fatal(err)
		}
		if stored.Status != expected.Status || stored.ToolSchemaVersion != expected.ToolSchemaVersion || (expected.CompletedAt != nil && stored.CompletedAt == nil) {
			t.Fatalf("preserved run %s changed: %#v", expected.ID, stored)
		}
	}
}

func TestAuditAgentRuntimeUpgrade_ReportsEveryActiveRunAndIsReadOnly(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 24, 18, 0, 0, 0, time.UTC)
	seedIncompatibleQueuedAgentRun(t, db, "run-queued-safe", now)
	createIncompatiblePausedRunFixture(t, db, "run-paused-safe", agentruntime.RunWaitingInput, now.Add(time.Second), false)
	seedIncompatibleQueuedAgentRun(t, db, "run-queued-advanced", now.Add(2*time.Second))
	type advancedQueuedRun struct {
		StateVersion int `gorm:"column:state_version"`
		StepNumber   int `gorm:"column:step_number"`
	}
	if err := db.Model(&model.AgentRun{}).Where("id = ?", "run-queued-advanced").
		Select("state_version", "step_number").
		Updates(advancedQueuedRun{StateVersion: 2, StepNumber: 1}).Error; err != nil {
		t.Fatal(err)
	}
	seedIncompatibleQueuedAgentRun(t, db, "run-queued-future", now.Add(3*time.Second))
	if err := db.Model(&model.AgentRun{}).Where("id = ?", "run-queued-future").Update(
		"tool_schema_version", agentruntime.CurrentToolSchemaVersion+1,
	).Error; err != nil {
		t.Fatal(err)
	}
	for index, status := range []agentruntime.RunStatus{agentruntime.RunRunning, agentruntime.RunWaitingTool} {
		run := model.AgentRun{
			ID: "run-active-" + string(status), ThreadID: "thread-active-" + string(status),
			ActorUserID: "user-active", ClientRequestID: "request-active-" + string(status),
			Status: status, ToolSchemaVersion: agentruntime.LegacyToolSchemaVersion,
			RuntimeVersion: agentruntime.LegacyRuntimeVersion, PolicyVersion: agentruntime.LegacyPolicyVersion,
			CreatedAt: now.Add(time.Duration(index+4) * time.Second), UpdatedAt: now,
		}
		if err := db.Create(&run).Error; err != nil {
			t.Fatal(err)
		}
	}
	seedIncompatibleQueuedAgentRun(t, db, "run-queued-timeline", now.Add(6*time.Second))
	if err := db.Create(&model.AgentTimelineItem{
		ID: "timeline-run-queued-timeline", TenantKind: agentruntime.TenantPersonal, TenantID: "user-old",
		ThreadID: "thread-run-queued-timeline", RunID: "run-queued-timeline", Kind: model.AgentTimelineItemStatusKind,
		Status: model.AgentTimelineItemInProgress, Ordinal: 1, SourceEventSequence: 1,
		ContentJSON: `{}`, StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Task{
		ID: "task-run-active-running", UserID: "user-active", ProjectID: "project-active",
		Type: "agent_runtime_model", Capability: "text", Status: model.TaskStatusRunning,
		Operation: "agent_model:run-active-running", Provider: "system", Model: "model-active",
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	before := pausedRetirementFactsSnapshot(t, db)
	audit, err := AuditAgentRuntimeUpgrade(db)
	if err != nil {
		t.Fatal(err)
	}
	after := pausedRetirementFactsSnapshot(t, db)
	if after != before {
		t.Fatal("upgrade audit mutated runtime facts")
	}
	if audit.CandidateRuns != 7 || audit.RetirableRuns != 2 {
		t.Fatalf("audit summary = %#v", audit)
	}
	want := map[string][]string{
		"run-active-running":      {agentRuntimeUpgradeBlockerNonRetirableStatus, agentRuntimeBlockerActiveProviderTask},
		"run-active-waiting_tool": {agentRuntimeUpgradeBlockerNonRetirableStatus},
		"run-queued-advanced":     {queuedRunBlockerNotPristine, queuedRunBlockerCheckpointMismatch},
		"run-queued-future":       {queuedRunBlockerFutureContract},
		"run-queued-timeline":     {queuedRunBlockerExternalTimeline},
	}
	for runID, categories := range want {
		for _, category := range categories {
			if !upgradeAuditHasBlocker(audit, runID, category) {
				t.Fatalf("missing blocker run=%s category=%s audit=%#v", runID, category, audit)
			}
		}
	}
	orderedRuns := make([]string, 0, len(want))
	for _, blocker := range audit.Blockers {
		if len(orderedRuns) == 0 || orderedRuns[len(orderedRuns)-1] != blocker.RunID {
			orderedRuns = append(orderedRuns, blocker.RunID)
		}
	}
	if got := strings.Join(orderedRuns, ","); got != "run-queued-advanced,run-queued-future,run-active-running,run-active-waiting_tool,run-queued-timeline" {
		t.Fatalf("blocker order = %s", got)
	}
}

func TestEnsureAgentRuntimeIntegritySchemaReportsFullAuditBeforeWriting(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 24, 18, 30, 0, 0, time.UTC)
	seedIncompatibleQueuedAgentRun(t, db, "run-queued-retirable", now)
	paused := createIncompatiblePausedRunFixture(t, db, "run-paused-retirable", agentruntime.RunWaitingInput, now.Add(time.Second), false)
	active := model.AgentRun{
		ID: "run-running-blocker", ThreadID: "thread-running-blocker", ActorUserID: "user-running",
		ClientRequestID: "request-running-blocker", Status: agentruntime.RunRunning,
		ToolSchemaVersion: agentruntime.LegacyToolSchemaVersion,
		RuntimeVersion:    agentruntime.LegacyRuntimeVersion, PolicyVersion: agentruntime.LegacyPolicyVersion,
		CreatedAt: now.Add(2 * time.Second), UpdatedAt: now,
	}
	if err := db.Create(&active).Error; err != nil {
		t.Fatal(err)
	}

	before := pausedRetirementFactsSnapshot(t, db)
	err := EnsureAgentRuntimeIntegritySchema(db)
	if err == nil || !strings.Contains(err.Error(), agentRuntimeRetirementInvalidCode) ||
		!strings.Contains(err.Error(), "run-running-blocker") ||
		!strings.Contains(err.Error(), agentRuntimeUpgradeBlockerNonRetirableStatus) {
		t.Fatalf("full upgrade audit error = %v", err)
	}
	if after := pausedRetirementFactsSnapshot(t, db); after != before {
		t.Fatal("schema migration wrote facts before full audit passed")
	}
	assertPausedRunUnchanged(t, db, paused)
}

func TestValidateAgentRuntimeUpgradeCoverageRejectsMissingAndOverlappingDispositions(t *testing.T) {
	tests := []struct {
		name         string
		candidateIDs []string
		queuedPlans  []queuedRunRetirementPlan
		pausedPlans  []pausedRunRetirementPlan
		blockers     []AgentRuntimeUpgradeBlocker
	}{
		{
			name:         "missing candidate disposition",
			candidateIDs: []string{"run-queued", "run-missing"},
			queuedPlans:  []queuedRunRetirementPlan{{Run: model.AgentRun{ID: "run-queued"}}},
		},
		{
			name:         "plan and blocker overlap",
			candidateIDs: []string{"run-overlap"},
			queuedPlans:  []queuedRunRetirementPlan{{Run: model.AgentRun{ID: "run-overlap"}}},
			blockers:     []AgentRuntimeUpgradeBlocker{{RunID: "run-overlap"}},
		},
		{
			name:         "unknown disposition",
			candidateIDs: []string{"run-known"},
			pausedPlans:  []pausedRunRetirementPlan{{Run: model.AgentRun{ID: "run-known"}}},
			blockers:     []AgentRuntimeUpgradeBlocker{{RunID: "run-unknown"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateAgentRuntimeUpgradeCoverage(test.candidateIDs, test.queuedPlans, test.pausedPlans, test.blockers)
			if err == nil || !strings.Contains(err.Error(), "agent runtime upgrade audit coverage mismatch") {
				t.Fatalf("coverage validation error = %v", err)
			}
		})
	}

	if err := validateAgentRuntimeUpgradeCoverage(
		[]string{"run-queued", "run-paused", "run-blocked"},
		[]queuedRunRetirementPlan{{Run: model.AgentRun{ID: "run-queued"}}},
		[]pausedRunRetirementPlan{{Run: model.AgentRun{ID: "run-paused"}}},
		[]AgentRuntimeUpgradeBlocker{
			{RunID: "run-blocked", Category: "first"},
			{RunID: "run-blocked", Category: "second"},
		},
	); err != nil {
		t.Fatalf("valid coverage rejected: %v", err)
	}
}

func upgradeAuditHasBlocker(audit AgentRuntimeUpgradeAudit, runID string, category string) bool {
	for _, blocker := range audit.Blockers {
		if blocker.RunID == runID && blocker.Category == category {
			return true
		}
	}
	return false
}
