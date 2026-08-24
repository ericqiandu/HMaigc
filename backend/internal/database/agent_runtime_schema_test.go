package database

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEnsureAgentRuntimeIntegritySchemaCreatesExactIndexes(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"idx_agent_runs_thread_client_request":                       `CREATE UNIQUE INDEX idx_agent_runs_thread_client_request ON agent_runs(thread_id, client_request_id)`,
		"idx_agent_run_events_run_sequence":                          `CREATE UNIQUE INDEX idx_agent_run_events_run_sequence ON agent_run_events(run_id, sequence)`,
		"idx_agent_checkpoints_run_sequence":                         `CREATE UNIQUE INDEX idx_agent_checkpoints_run_sequence ON agent_checkpoints(run_id, sequence)`,
		"idx_agent_tool_calls_action":                                `CREATE UNIQUE INDEX idx_agent_tool_calls_action ON agent_tool_calls(run_id, tool_call_id, action_version)`,
		"idx_agent_threads_scope":                                    `CREATE INDEX idx_agent_threads_scope ON agent_threads(tenant_kind, tenant_id, canvas_id, updated_at)`,
		"idx_agent_production_plan_versions_scope_key_version":       `CREATE UNIQUE INDEX idx_agent_production_plan_versions_scope_key_version ON agent_production_plan_versions(tenant_kind, tenant_id, domain_project_id, canvas_id, plan_key, version)`,
		"idx_agent_production_artifacts_version_reference_shot_kind": `CREATE UNIQUE INDEX idx_agent_production_artifacts_version_reference_shot_kind ON agent_production_artifacts(plan_version_id, reference_key, shot_key, kind)`,
		"idx_agent_production_artifacts_task":                        `CREATE UNIQUE INDEX idx_agent_production_artifacts_task ON agent_production_artifacts(task_id) WHERE task_id <> ''`,
		"idx_agent_production_artifacts_billing":                     `CREATE UNIQUE INDEX idx_agent_production_artifacts_billing ON agent_production_artifacts(billing_order_id) WHERE billing_order_id <> ''`,
		"idx_agent_production_artifacts_resource":                    `CREATE UNIQUE INDEX idx_agent_production_artifacts_resource ON agent_production_artifacts(resource_id) WHERE resource_id <> ''`,
	}
	for name, expected := range want {
		var actual string
		if err := db.Raw("SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?", name).Scan(&actual).Error; err != nil {
			t.Fatal(err)
		}
		if compactSQL(actual) != compactSQL(expected) {
			t.Fatalf("index %s SQL = %q, want %q", name, actual, expected)
		}
	}
}

func TestEnsureAgentRuntimeIntegritySchemaCreatesTimelineTableAndExactIndexes(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasTable(&model.AgentTimelineItem{}) {
		t.Fatal("agent timeline table was not created")
	}
	if err := EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"idx_agent_timeline_items_run_ordinal":  `CREATE UNIQUE INDEX idx_agent_timeline_items_run_ordinal ON agent_timeline_items(run_id, ordinal)`,
		"idx_agent_timeline_items_run_sequence": `CREATE UNIQUE INDEX idx_agent_timeline_items_run_sequence ON agent_timeline_items(run_id, source_event_sequence)`,
		"idx_agent_timeline_items_thread_query": `CREATE INDEX idx_agent_timeline_items_thread_query ON agent_timeline_items(thread_id, created_at, id)`,
	}
	for name, expected := range want {
		var actual string
		if err := db.Raw("SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?", name).Scan(&actual).Error; err != nil {
			t.Fatal(err)
		}
		if compactSQL(actual) != compactSQL(expected) {
			t.Fatalf("index %s SQL = %q, want %q", name, actual, expected)
		}
	}
	if err := EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatalf("repeated timeline migration must be idempotent: %v", err)
	}
}

func TestEnsureAgentRuntimeIntegritySchemaRejectsWrongNamedTimelineIndex(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	const wrong = `CREATE UNIQUE INDEX idx_agent_timeline_items_run_ordinal ON agent_timeline_items(run_id, source_event_sequence)`
	if err := db.Exec(wrong).Error; err != nil {
		t.Fatal(err)
	}
	err := EnsureAgentRuntimeIntegritySchema(db)
	if err == nil || !strings.Contains(err.Error(), "idx_agent_timeline_items_run_ordinal") {
		t.Fatalf("wrong timeline index error = %v", err)
	}
}

func TestEnsureAgentRuntimeIntegritySchemaReplacesLegacyGlobalProductionIndexes(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE UNIQUE INDEX idx_agent_production_plan_versions_key_version ON agent_production_plan_versions(plan_key, version)`,
		`CREATE UNIQUE INDEX idx_agent_production_artifacts_plan_shot_kind ON agent_production_artifacts(plan_key, plan_version, shot_key, kind)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	for _, legacyIndex := range legacyAgentProductionIndexes {
		var count int64
		if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", legacyIndex).Scan(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("legacy index %s still exists", legacyIndex)
		}
	}
}

func TestEnsureAgentRuntimeIntegritySchemaRejectsWrongNamedIndex(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	row := model.AgentRunEvent{
		ID: "event-1", RunID: "run-1", Sequence: 1,
		Kind: agentruntime.EventRunCreated, PayloadJSON: `{}`, CreatedAt: now,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	const wrong = `CREATE UNIQUE INDEX idx_agent_run_events_run_sequence ON agent_run_events(run_id, kind)`
	if err := db.Exec(wrong).Error; err != nil {
		t.Fatal(err)
	}
	err := EnsureAgentRuntimeIntegritySchema(db)
	if err == nil || !strings.Contains(err.Error(), "idx_agent_run_events_run_sequence") {
		t.Fatalf("wrong index error = %v", err)
	}
	var stored model.AgentRunEvent
	if err := db.First(&stored, "id = ?", row.ID).Error; err != nil {
		t.Fatalf("existing event changed: %v", err)
	}
	var definition string
	if err := db.Raw("SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?", "idx_agent_run_events_run_sequence").Scan(&definition).Error; err != nil {
		t.Fatal(err)
	}
	if compactSQL(definition) != compactSQL(wrong) {
		t.Fatalf("wrong index was silently rewritten: %q", definition)
	}
}

func TestEnsureAgentRuntimeIntegritySchemaRejectsLegacyDuplicateFacts(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	rows := []model.AgentRunEvent{
		{ID: "event-a", RunID: "run-duplicate", Sequence: 1, Kind: agentruntime.EventRunCreated, PayloadJSON: `{}`, CreatedAt: now},
		{ID: "event-b", RunID: "run-duplicate", Sequence: 1, Kind: agentruntime.EventRunStatusChanged, PayloadJSON: `{}`, CreatedAt: now},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	err := EnsureAgentRuntimeIntegritySchema(db)
	if err == nil || !strings.Contains(err.Error(), "run-duplicate") {
		t.Fatalf("duplicate event error = %v", err)
	}
	var count int64
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ?", "run-duplicate").Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("duplicate historical rows changed: count=%d err=%v", count, err)
	}
}

func TestEnsureAgentRuntimeIntegritySchemaRejectsTimelineDuplicateFacts(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		second     model.AgentTimelineItem
		wantDetail string
	}{
		{
			name: "ordinal",
			second: model.AgentTimelineItem{
				ID: "timeline-b", Ordinal: 1, SourceEventSequence: 2,
			},
			wantDetail: "agent timeline ordinal",
		},
		{
			name: "source event sequence",
			second: model.AgentTimelineItem{
				ID: "timeline-b", Ordinal: 2, SourceEventSequence: 1,
			},
			wantDetail: "agent timeline source event",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			db := openAgentRuntimeSchemaSQLite(t)
			if err := MigrateBaseSchema(db); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			first := model.AgentTimelineItem{
				ID: "timeline-a", TenantKind: "user", TenantID: "user-timeline", ThreadID: "thread-timeline", RunID: "run-timeline",
				Kind: model.AgentTimelineItemStatusKind, Status: model.AgentTimelineItemCompleted,
				Ordinal: 1, SourceEventSequence: 1, ContentJSON: `{}`, CreatedAt: now, UpdatedAt: now,
			}
			second := testCase.second
			second.TenantKind = first.TenantKind
			second.TenantID = first.TenantID
			second.ThreadID = first.ThreadID
			second.RunID = first.RunID
			second.Kind = model.AgentTimelineItemStatusKind
			second.Status = model.AgentTimelineItemCompleted
			second.ContentJSON = `{}`
			second.CreatedAt = now
			second.UpdatedAt = now
			if err := db.Create(&[]model.AgentTimelineItem{first, second}).Error; err != nil {
				t.Fatal(err)
			}
			err := EnsureAgentRuntimeIntegritySchema(db)
			if err == nil || !strings.Contains(err.Error(), testCase.wantDetail) || !strings.Contains(err.Error(), first.RunID) {
				t.Fatalf("timeline conflict error = %v", err)
			}
			var count int64
			if err := db.Model(&model.AgentTimelineItem{}).Where("run_id = ?", first.RunID).Count(&count).Error; err != nil || count != 2 {
				t.Fatalf("duplicate timeline rows changed: count=%d err=%v", count, err)
			}
		})
	}
}

func TestEnsureAgentRuntimeIntegritySchemaRejectsIncompatibleActiveRunWithoutMutation(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	run := model.AgentRun{
		ID: "run-old-active", ThreadID: "thread-old", ActorUserID: "user-old", ClientRequestID: "request-old",
		Status: agentruntime.RunWaitingInput, ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion - 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}

	err := EnsureAgentRuntimeIntegritySchema(db)
	if err == nil || !strings.Contains(err.Error(), "run_id=run-old-active") ||
		!strings.Contains(err.Error(), "required="+strconv.Itoa(agentruntime.CurrentToolSchemaVersion)) {
		t.Fatalf("incompatible active run error = %v", err)
	}
	var stored model.AgentRun
	if err := db.First(&stored, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != run.Status || stored.ToolSchemaVersion != run.ToolSchemaVersion {
		t.Fatalf("incompatible run changed during fail-close validation: %#v", stored)
	}

	if err := db.Model(&model.AgentRun{}).Where("id = ?", run.ID).Update("status", agentruntime.RunCancelled).Error; err != nil {
		t.Fatal(err)
	}
	if err := EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatalf("terminal historical run should not block the current runtime: %v", err)
	}
}

func TestEnsureAgentRuntimeIntegritySchemaRejectsRuntimeAndPolicyVersionMismatch(t *testing.T) {
	for _, testCase := range []struct {
		name           string
		runtimeVersion int
		policyVersion  int
		wantFact       string
	}{
		{name: "runtime", runtimeVersion: agentruntime.CurrentRuntimeVersion - 1, policyVersion: agentruntime.CurrentPolicyVersion, wantFact: "runtime_version="},
		{name: "policy", runtimeVersion: agentruntime.CurrentRuntimeVersion, policyVersion: agentruntime.CurrentPolicyVersion - 1, wantFact: "policy_version="},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			db := openAgentRuntimeSchemaSQLite(t)
			if err := MigrateBaseSchema(db); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			run := model.AgentRun{
				ID: "run-version-" + testCase.name, ThreadID: "thread-version", ActorUserID: "user-version", ClientRequestID: "request-version-" + testCase.name,
				Status: agentruntime.RunRunning, ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
				RuntimeVersion: testCase.runtimeVersion, PolicyVersion: testCase.policyVersion,
				CreatedAt: now, UpdatedAt: now,
			}
			if err := db.Create(&run).Error; err != nil {
				t.Fatal(err)
			}
			err := EnsureAgentRuntimeIntegritySchema(db)
			if err == nil || !strings.Contains(err.Error(), "run_id="+run.ID) || !strings.Contains(err.Error(), testCase.wantFact) {
				t.Fatalf("version mismatch error = %v", err)
			}
		})
	}
}

func TestEnsureAgentRuntimeIntegritySchemaRetiresIncompatibleQueuedRunWithTerminalFacts(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	state := legacyAgentRuntimeStateV1{
		StateVersion: 1, StepNumber: 0, MaxSteps: 6, Status: agentruntime.RunQueued,
		UserMessage: "生成短剧样片",
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	run := model.AgentRun{
		ID: "run-old-queued", ThreadID: "thread-old-queued", ActorUserID: "user-old", ClientRequestID: "request-old-queued",
		Status: agentruntime.RunQueued, LastEventSequence: 1, StateVersion: 1, MaxSteps: state.MaxSteps,
		ModelRecordID: "model-record-old", ModelKey: "model-old", ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion - 1,
		RuntimeVersion: 1, PolicyVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AgentRunEvent{
		ID: "event-old-queued-1", RunID: run.ID, Sequence: 1, Kind: agentruntime.EventRunCreated,
		PayloadJSON: string(stateJSON), CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AgentCheckpoint{
		ID: "checkpoint-old-queued-1", RunID: run.ID, Sequence: 1,
		StateVersion: state.StateVersion, StateJSON: string(stateJSON), CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	if err := EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatalf("queued legacy run should be retired during the hard cutover: %v", err)
	}

	var stored model.AgentRun
	if err := db.First(&stored, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != agentruntime.RunFailed || stored.StateVersion != 2 || stored.StepNumber != 0 || stored.LastEventSequence != 2 || stored.CompletedAt == nil {
		t.Fatalf("retired run facts = %#v", stored)
	}
	if stored.ToolSchemaVersion != run.ToolSchemaVersion {
		t.Fatalf("historical tool schema changed: got %d want %d", stored.ToolSchemaVersion, run.ToolSchemaVersion)
	}

	var event model.AgentRunEvent
	if err := db.First(&event, "run_id = ? AND sequence = ?", run.ID, 2).Error; err != nil {
		t.Fatal(err)
	}
	if event.Kind != agentruntime.EventRunFailed {
		t.Fatalf("retirement event kind = %s", event.Kind)
	}
	if strings.Contains(event.PayloadJSON, `"configuration"`) {
		t.Fatalf("retirement rewrote the historical v1 state shape: %s", event.PayloadJSON)
	}
	var terminal legacyAgentRuntimeStateV1
	if err := json.Unmarshal([]byte(event.PayloadJSON), &terminal); err != nil {
		t.Fatal(err)
	}
	if terminal.Status != agentruntime.RunFailed || terminal.FailureCode != "tool_schema_retired" || terminal.StateVersion != 2 {
		t.Fatalf("terminal state = %#v", terminal)
	}
	var checkpoint model.AgentCheckpoint
	if err := db.First(&checkpoint, "run_id = ? AND sequence = ?", run.ID, 2).Error; err != nil {
		t.Fatal(err)
	}
	if checkpoint.StateVersion != terminal.StateVersion || checkpoint.StateJSON != event.PayloadJSON {
		t.Fatalf("terminal checkpoint does not match event: %#v", checkpoint)
	}
}

func TestEnsureAgentRuntimeIntegritySchemaRetiresPristineQueuedRunWithOlderRuntimeContract(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	state := agentruntime.RuntimeState{
		StateVersion: 1, StepNumber: 0, MaxSteps: 6, Status: agentruntime.RunQueued,
		UserMessage:   "生成短剧样片",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided},
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	run := model.AgentRun{
		ID: "run-old-runtime-queued", ThreadID: "thread-old-runtime", ActorUserID: "user-old", ClientRequestID: "request-old-runtime",
		Status: agentruntime.RunQueued, LastEventSequence: 1, StateVersion: 1, MaxSteps: state.MaxSteps,
		ModelRecordID: "model-record-old", ModelKey: "model-old", ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion: agentruntime.CurrentRuntimeVersion - 1, PolicyVersion: agentruntime.CurrentPolicyVersion - 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AgentRunEvent{
		ID: "event-old-runtime-1", RunID: run.ID, Sequence: 1, Kind: agentruntime.EventRunCreated,
		PayloadJSON: string(stateJSON), CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AgentCheckpoint{
		ID: "checkpoint-old-runtime-1", RunID: run.ID, Sequence: 1,
		StateVersion: state.StateVersion, StateJSON: string(stateJSON), CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	if err := EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatalf("pristine queued runtime should be retired during the hard cutover: %v", err)
	}
	var stored model.AgentRun
	if err := db.First(&stored, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != agentruntime.RunFailed || stored.StateVersion != 2 || stored.LastEventSequence != 2 || stored.CompletedAt == nil {
		t.Fatalf("retired runtime facts = %#v", stored)
	}
	if stored.RuntimeVersion != run.RuntimeVersion || stored.PolicyVersion != run.PolicyVersion {
		t.Fatalf("historical runtime contract changed: %#v", stored)
	}
	var checkpoint model.AgentCheckpoint
	if err := db.First(&checkpoint, "run_id = ? AND sequence = ?", run.ID, 2).Error; err != nil {
		t.Fatal(err)
	}
	var terminal agentruntime.RuntimeState
	if err := json.Unmarshal([]byte(checkpoint.StateJSON), &terminal); err != nil {
		t.Fatal(err)
	}
	if terminal.Status != agentruntime.RunFailed || terminal.FailureCode != "runtime_contract_retired" || terminal.StateVersion != 2 {
		t.Fatalf("terminal runtime state = %#v", terminal)
	}
}

func TestEnsureAgentRuntimeIntegritySchemaDoesNotRetireQueuedRunFromNewerToolSchema(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	run := model.AgentRun{
		ID: "run-newer-queued", ThreadID: "thread-newer", ActorUserID: "user-newer", ClientRequestID: "request-newer",
		Status: agentruntime.RunQueued, ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion + 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}

	err := EnsureAgentRuntimeIntegritySchema(db)
	if err == nil || !strings.Contains(err.Error(), "run_id=run-newer-queued") ||
		!strings.Contains(err.Error(), "tool_schema_version="+strconv.Itoa(agentruntime.CurrentToolSchemaVersion+1)) {
		t.Fatalf("newer tool schema rejection = %v", err)
	}
	var stored model.AgentRun
	if err := db.First(&stored, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != agentruntime.RunQueued || stored.CompletedAt != nil || stored.ToolSchemaVersion != run.ToolSchemaVersion {
		t.Fatalf("newer queued run was mutated: %#v", stored)
	}
}

func TestEnsureAgentRuntimeIntegritySchemaReportsEveryRiskyIncompatibleRun(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	runs := []model.AgentRun{
		{
			ID: "run-old-running", ThreadID: "thread-old-running", ActorUserID: "user-old", ClientRequestID: "request-old-running",
			Status: agentruntime.RunRunning, ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion - 1,
			CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "run-old-waiting-tool", ThreadID: "thread-old-waiting", ActorUserID: "user-old", ClientRequestID: "request-old-waiting",
			Status: agentruntime.RunWaitingTool, ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion - 1,
			CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second),
		},
	}
	if err := db.Create(&runs).Error; err != nil {
		t.Fatal(err)
	}

	err := EnsureAgentRuntimeIntegritySchema(db)
	if err == nil || !strings.Contains(err.Error(), "incompatible_active_runs=2") ||
		!strings.Contains(err.Error(), "run-old-running") || !strings.Contains(err.Error(), "run-old-waiting-tool") {
		t.Fatalf("incompatible run summary = %v", err)
	}
}

func TestEnsureAgentRuntimeIntegritySchemaRetirementIsAtomicAndIdempotent(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	for _, runID := range []string{"run-old-queued-a", "run-old-queued-b"} {
		seedIncompatibleQueuedAgentRun(t, db, runID, now)
	}

	if err := EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	if err := EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatalf("repeated retirement must be idempotent: %v", err)
	}
	for _, runID := range []string{"run-old-queued-a", "run-old-queued-b"} {
		var run model.AgentRun
		if err := db.First(&run, "id = ?", runID).Error; err != nil {
			t.Fatal(err)
		}
		if run.Status != agentruntime.RunFailed || run.LastEventSequence != 2 || run.StateVersion != 2 {
			t.Fatalf("retired run %s = %#v", runID, run)
		}
		var eventCount, checkpointCount int64
		if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ?", runID).Count(&eventCount).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&model.AgentCheckpoint{}).Where("run_id = ?", runID).Count(&checkpointCount).Error; err != nil {
			t.Fatal(err)
		}
		if eventCount != 2 || checkpointCount != 2 {
			t.Fatalf("retired run %s facts: events=%d checkpoints=%d", runID, eventCount, checkpointCount)
		}
	}
}

func TestEnsureAgentRuntimeIntegritySchemaRollsBackEveryRetirementWhenOneRunIsInvalid(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	seedIncompatibleQueuedAgentRun(t, db, "run-old-valid", now)
	seedIncompatibleQueuedAgentRun(t, db, "run-old-invalid", now.Add(time.Second))
	if err := db.Model(&model.AgentCheckpoint{}).
		Where("run_id = ? AND sequence = ?", "run-old-invalid", 1).
		Update("state_json", `{not-json}`).Error; err != nil {
		t.Fatal(err)
	}

	err := EnsureAgentRuntimeIntegritySchema(db)
	if err == nil || !strings.Contains(err.Error(), "run_id=run-old-invalid") {
		t.Fatalf("invalid legacy run error = %v", err)
	}
	for _, runID := range []string{"run-old-valid", "run-old-invalid"} {
		var run model.AgentRun
		if err := db.First(&run, "id = ?", runID).Error; err != nil {
			t.Fatal(err)
		}
		if run.Status != agentruntime.RunQueued || run.LastEventSequence != 1 || run.StateVersion != 1 || run.CompletedAt != nil {
			t.Fatalf("run %s mutated despite transaction rollback: %#v", runID, run)
		}
		var terminalEventCount int64
		if err := db.Model(&model.AgentRunEvent{}).
			Where("run_id = ? AND sequence > ?", runID, 1).
			Count(&terminalEventCount).Error; err != nil {
			t.Fatal(err)
		}
		if terminalEventCount != 0 {
			t.Fatalf("run %s retained terminal events after rollback: %d", runID, terminalEventCount)
		}
	}
}

func TestEnsureAgentRuntimeIntegritySchemaDoesNotRetireQueuedRunThatAlreadyAdvanced(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	const runID = "run-old-queued-advanced"
	seedIncompatibleQueuedAgentRun(t, db, runID, now)
	state := legacyAgentRuntimeStateV1{
		StateVersion: 2, StepNumber: 1, MaxSteps: 6, Status: agentruntime.RunQueued,
		UserMessage: "生成短剧样片",
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	type advancedRunFacts struct {
		StateVersion int `gorm:"column:state_version"`
		StepNumber   int `gorm:"column:step_number"`
	}
	if err := db.Model(&model.AgentRun{}).Where("id = ?", runID).
		Select("state_version", "step_number").Updates(advancedRunFacts{StateVersion: 2, StepNumber: 1}).Error; err != nil {
		t.Fatal(err)
	}
	type advancedCheckpointFacts struct {
		StateVersion int    `gorm:"column:state_version"`
		StateJSON    string `gorm:"column:state_json"`
	}
	if err := db.Model(&model.AgentCheckpoint{}).Where("run_id = ?", runID).
		Select("state_version", "state_json").Updates(advancedCheckpointFacts{StateVersion: 2, StateJSON: string(stateJSON)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ?", runID).
		Update("payload_json", string(stateJSON)).Error; err != nil {
		t.Fatal(err)
	}

	err = EnsureAgentRuntimeIntegritySchema(db)
	if err == nil || !strings.Contains(err.Error(), "run_id="+runID) {
		t.Fatalf("advanced queued run error = %v", err)
	}
	var stored model.AgentRun
	if err := db.First(&stored, "id = ?", runID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != agentruntime.RunQueued || stored.StateVersion != 2 || stored.StepNumber != 1 || stored.CompletedAt != nil {
		t.Fatalf("advanced queued run was retired: %#v", stored)
	}
}

func TestEnsureAgentRuntimeIntegritySchemaDoesNotRetireLegacyRunWithActiveModelBilling(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	const runID = "run-old-with-active-model-task"
	seedIncompatibleQueuedAgentRun(t, db, runID, now)
	task := model.Task{
		ID: legacyAgentModelTaskID(runID, 0), UserID: "user-old", ProjectID: "legacy-canvas",
		Type: "agent_runtime_model", Capability: "text", Status: model.TaskStatusQueued, Operation: "agent_model:" + runID,
		Provider: "system", Model: "model-old", BillingOrderID: "legacy-active-order",
		CreatedAt: now, UpdatedAt: now,
	}
	order := model.BillingOrder{
		ID: task.BillingOrderID, UserID: task.UserID, TaskID: task.ID,
		IdempotencyKey: "agent-runtime:" + runID + ":0", Scene: "agent_runtime_model",
		ChannelModelID: "model-record-old", Model: "model-old", Capability: "text",
		Quantity: 1, Status: model.BillingStatusReserved, AmountMicrocredits: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}

	err := EnsureAgentRuntimeIntegritySchema(db)
	if err == nil || !strings.Contains(err.Error(), "run_id="+runID) || !strings.Contains(err.Error(), "external facts") {
		t.Fatalf("active legacy model task error = %v", err)
	}
	var storedRun model.AgentRun
	if err := db.First(&storedRun, "id = ?", runID).Error; err != nil {
		t.Fatal(err)
	}
	var storedTask model.Task
	if err := db.First(&storedTask, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	var storedOrder model.BillingOrder
	if err := db.First(&storedOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedRun.Status != agentruntime.RunQueued || storedTask.Status != model.TaskStatusQueued || storedOrder.Status != model.BillingStatusReserved {
		t.Fatalf("active commercial facts changed: run=%#v task=%#v order=%#v", storedRun, storedTask, storedOrder)
	}
}

func TestEnsureAgentRuntimeIntegritySchemaDoesNotRetireLegacyRunAfterModelBillingIsTerminal(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	const runID = "run-old-with-terminal-model-task"
	seedIncompatibleQueuedAgentRun(t, db, runID, now)
	task := model.Task{
		ID: legacyAgentModelTaskID(runID, 0), UserID: "user-old", ProjectID: "legacy-canvas",
		Type: "agent_runtime_model", Capability: "text", Status: model.TaskStatusSucceeded, Operation: "agent_model:" + runID,
		Provider: "system", Model: "model-old", BillingOrderID: "legacy-terminal-order",
		CreatedAt: now, UpdatedAt: now, CompletedAt: &now,
	}
	order := model.BillingOrder{
		ID: task.BillingOrderID, UserID: task.UserID, TaskID: task.ID,
		IdempotencyKey: "agent-runtime:" + runID + ":0", Scene: "agent_runtime_model",
		ChannelModelID: "model-record-old", Model: "model-old", Capability: "text",
		Quantity: 1, Status: model.BillingStatusSettled, AmountMicrocredits: 1,
		CreatedAt: now, UpdatedAt: now, SettledAt: &now,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}

	if err := EnsureAgentRuntimeIntegritySchema(db); err == nil || !strings.Contains(err.Error(), "external facts") {
		t.Fatalf("terminal legacy model facts must block automatic retirement: %v", err)
	}
	var storedRun model.AgentRun
	if err := db.First(&storedRun, "id = ?", runID).Error; err != nil {
		t.Fatal(err)
	}
	if storedRun.Status != agentruntime.RunQueued || storedRun.CompletedAt != nil {
		t.Fatalf("legacy run changed despite external commercial facts: %#v", storedRun)
	}
	var storedTask model.Task
	if err := db.First(&storedTask, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	var storedOrder model.BillingOrder
	if err := db.First(&storedOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != task.Status || storedOrder.Status != order.Status || storedOrder.AmountMicrocredits != order.AmountMicrocredits {
		t.Fatalf("terminal commercial facts changed: task=%#v order=%#v", storedTask, storedOrder)
	}
}

func TestEnsureAgentRuntimeIntegritySchemaDoesNotRetireLegacyRunAfterTokenBillingRefund(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	const runID = "run-old-with-refunded-token-task"
	seedIncompatibleQueuedAgentRun(t, db, runID, now)
	task := model.Task{
		ID: legacyAgentModelTaskID(runID, 0), UserID: "user-old", ProjectID: "legacy-canvas",
		Type: "agent_runtime_model", Capability: "text", Status: model.TaskStatusFailed, Operation: "agent_model:" + runID,
		Provider: "system", Model: "model-old", BillingOrderID: "legacy-refunded-order",
		CreatedAt: now, UpdatedAt: now, CompletedAt: &now,
	}
	order := model.BillingOrder{
		ID: task.BillingOrderID, UserID: task.UserID, TaskID: task.ID,
		IdempotencyKey: "proxy-token:agent-runtime:" + runID + ":0", Scene: "agent_runtime_model",
		ChannelModelID: "model-record-old", Model: "model-old", Capability: "text",
		Quantity: 1, Status: model.BillingStatusRefunded, AmountMicrocredits: 1,
		CreatedAt: now, UpdatedAt: now, RefundedAt: &now,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}

	if err := EnsureAgentRuntimeIntegritySchema(db); err == nil || !strings.Contains(err.Error(), "external facts") {
		t.Fatalf("refunded legacy token billing must block automatic retirement: %v", err)
	}
	var storedRun model.AgentRun
	if err := db.First(&storedRun, "id = ?", runID).Error; err != nil {
		t.Fatal(err)
	}
	if storedRun.Status != agentruntime.RunQueued || storedRun.CompletedAt != nil {
		t.Fatalf("legacy run changed despite refunded billing facts: %#v", storedRun)
	}
	var storedTask model.Task
	if err := db.First(&storedTask, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	var storedOrder model.BillingOrder
	if err := db.First(&storedOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != task.Status || storedOrder.Status != order.Status || storedOrder.AmountMicrocredits != order.AmountMicrocredits {
		t.Fatalf("refunded commercial facts changed: task=%#v order=%#v", storedTask, storedOrder)
	}
}

func TestMigrateSchemaAddsAgentRuntimeWithoutChangingCanvasFacts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/agent-additive.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.User{}, &model.CanvasProject{}, &model.CanvasChange{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	user := model.User{ID: "user-existing", Username: "existing", DisplayName: "Existing", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}
	canvas := model.CanvasProject{ID: "canvas-existing", UserID: user.ID, ProjectID: "project-existing", Title: "Existing Canvas", PayloadJSON: `{"nodes":[]}`, Revision: 7, CreatedAt: now, UpdatedAt: now}
	change := model.CanvasChange{ID: "change-existing", CanvasID: canvas.ID, Revision: 7, ActorUserID: user.ID, ClientMutationID: "mutation-existing", PayloadJSON: `{"type":"replace"}`, CreatedAt: now}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&canvas).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&change).Error; err != nil {
		t.Fatal(err)
	}

	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"agent_threads", "agent_runs", "agent_run_events", "agent_checkpoints", "agent_tool_calls"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("missing additive agent runtime table %s", table)
		}
	}
	for _, table := range []string{"agent_production_plan_versions", "agent_production_artifacts"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("missing additive agent production table %s", table)
		}
	}
	for _, column := range []string{"state_version", "runtime_version", "policy_version"} {
		if !db.Migrator().HasColumn(&model.AgentRun{}, column) {
			t.Fatalf("missing additive agent_runs column %s", column)
		}
	}
	if !db.Migrator().HasColumn(&model.AgentProductionPlanVersion{}, "domain_project_id") {
		t.Fatal("missing additive agent production plan domain_project_id column")
	}
	for _, column := range []string{"risk_level", "required_access", "approval_required", "approval_decision", "approval_by_user_id", "approval_decided_at", "idempotency_key", "started_at"} {
		if !db.Migrator().HasColumn(&model.AgentToolCall{}, column) {
			t.Fatalf("missing additive agent_tool_calls column %s", column)
		}
	}
	var storedCanvas model.CanvasProject
	if err := db.First(&storedCanvas, "id = ?", canvas.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedCanvas.Revision != canvas.Revision || storedCanvas.PayloadJSON != canvas.PayloadJSON || storedCanvas.Title != canvas.Title {
		t.Fatalf("canvas changed during additive migration: %#v", storedCanvas)
	}
	var storedChange model.CanvasChange
	if err := db.First(&storedChange, "id = ?", change.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedChange.Revision != change.Revision || storedChange.PayloadJSON != change.PayloadJSON || storedChange.ClientMutationID != change.ClientMutationID {
		t.Fatalf("canvas change altered during additive migration: %#v", storedChange)
	}
	var indexCount int64
	if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", "idx_agent_run_events_run_sequence").Scan(&indexCount).Error; err != nil {
		t.Fatal(err)
	}
	if indexCount != 1 {
		t.Fatal("MigrateSchema did not install agent runtime integrity indexes")
	}
}

func openAgentRuntimeSchemaSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func seedIncompatibleQueuedAgentRun(t *testing.T, db *gorm.DB, runID string, now time.Time) {
	t.Helper()
	state := legacyAgentRuntimeStateV1{
		StateVersion: 1, StepNumber: 0, MaxSteps: 6, Status: agentruntime.RunQueued,
		UserMessage: "生成短剧样片",
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	run := model.AgentRun{
		ID: runID, ThreadID: "thread-" + runID, ActorUserID: "user-old", ClientRequestID: "request-" + runID,
		Status: agentruntime.RunQueued, LastEventSequence: 1, StateVersion: 1, MaxSteps: state.MaxSteps,
		ModelRecordID: "model-record-old", ModelKey: "model-old", ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion - 1,
		RuntimeVersion: 1, PolicyVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AgentRunEvent{
		ID: "event-" + runID, RunID: runID, Sequence: 1, Kind: agentruntime.EventRunCreated,
		PayloadJSON: string(stateJSON), CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AgentCheckpoint{
		ID: "checkpoint-" + runID, RunID: runID, Sequence: 1,
		StateVersion: state.StateVersion, StateJSON: string(stateJSON), CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
}
