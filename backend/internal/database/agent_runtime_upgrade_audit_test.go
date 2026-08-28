package database

import (
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

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
