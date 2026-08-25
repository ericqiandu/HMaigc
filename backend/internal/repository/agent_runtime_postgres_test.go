package repository

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/testsupport"

	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresAgentRuntimeUpgradeRetiresPausedRunWithTerminalHistory(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 24, 15, 0, 0, 0, time.UTC)
	thread := model.AgentThread{
		ID: "postgres-paused-history-thread", TenantKind: agentruntime.TenantPersonal, TenantID: "postgres-paused-history-user",
		CreatedByUserID: "postgres-paused-history-user", DomainProjectID: "postgres-paused-history-project",
		CanvasID: "postgres-paused-history-canvas", Status: agentruntime.ThreadActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&thread).Error; err != nil {
		t.Fatal(err)
	}
	delivery := agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryAnswer,
		RequiredArtifacts:  []agentruntime.ArtifactKind{agentruntime.ArtifactText},
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}},
	}
	state := agentruntime.RuntimeState{
		StateVersion: 4, StepNumber: 2, MaxSteps: 24, Status: agentruntime.RunWaitingApproval,
		UserMessage: "创建 5 秒测试视频", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic},
		ExpectedDelivery: &delivery,
		PendingToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "postgres-paused-current-tool", ToolName: agentruntime.ToolProductionRender, ActionVersion: 1,
			Arguments: json.RawMessage(`{"planKey":"postgres-paused-plan","baseVersion":1}`), ExpectedDelivery: delivery,
		},
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	run := model.AgentRun{
		ID: "postgres-paused-history-run", ThreadID: thread.ID, ActorUserID: thread.CreatedByUserID,
		ClientRequestID: "postgres-paused-history-request", Status: state.Status, LastEventSequence: 7,
		StateVersion: state.StateVersion, StepNumber: state.StepNumber, MaxSteps: state.MaxSteps,
		ModelRecordID: "postgres-paused-model-record", ModelKey: "postgres-paused-model",
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion, RuntimeVersion: agentruntime.CurrentRuntimeVersion - 1,
		PolicyVersion: agentruntime.CurrentPolicyVersion - 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AgentRunEvent{ID: "postgres-paused-event", RunID: run.ID, Sequence: 7, Kind: agentruntime.EventRunStatusChanged, PayloadJSON: string(stateJSON), CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AgentCheckpoint{ID: "postgres-paused-checkpoint", RunID: run.ID, Sequence: 7, StateVersion: state.StateVersion, StateJSON: string(stateJSON), CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AgentTimelineItem{
		ID: "postgres-paused-timeline", TenantKind: thread.TenantKind, TenantID: thread.TenantID, ThreadID: thread.ID,
		RunID: run.ID, Kind: model.AgentTimelineItemStatusKind, Status: model.AgentTimelineItemInProgress,
		Ordinal: 1, SourceEventSequence: 7, ContentJSON: `{"label":"准备中"}`, StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	current := model.AgentToolCall{
		ID: "postgres-paused-current-record", RunID: run.ID, ToolCallID: state.PendingToolCall.ToolCallID,
		ActionVersion: 1, ToolName: string(state.PendingToolCall.ToolName), Status: agentruntime.ToolCallWaitingApproval,
		ApprovalRequired: true, IdempotencyKey: run.ID + ":tool:current", InputJSON: string(state.PendingToolCall.Arguments),
		OutputJSON: `{}`, CreatedAt: now, UpdatedAt: now,
	}
	historicalStartedAt := now.Add(-time.Minute)
	historical := model.AgentToolCall{
		ID: "postgres-paused-history-record", RunID: run.ID, ToolCallID: "postgres-paused-history-tool",
		ActionVersion: 1, ToolName: string(agentruntime.ToolSkillLoad), Status: agentruntime.ToolCallSucceeded,
		IdempotencyKey: run.ID + ":tool:history", InputJSON: `{}`, OutputJSON: `{"loaded":true}`,
		StartedAt: &historicalStartedAt, CreatedAt: historicalStartedAt, UpdatedAt: historicalStartedAt,
	}
	if err := db.Create(&[]model.AgentToolCall{current, historical}).Error; err != nil {
		t.Fatal(err)
	}
	plan := model.AgentProductionPlanVersion{
		ID: "postgres-paused-plan-version", PlanKey: "postgres-paused-plan", TenantKind: thread.TenantKind,
		TenantID: thread.TenantID, DomainProjectID: thread.DomainProjectID, CanvasID: thread.CanvasID,
		CreatedByRunID: run.ID, Version: 1, Status: model.AgentProductionPlanActive, Title: "测试计划",
		TargetDurationMS: 5000, Script: "测试脚本", ReferencesJSON: `[]`, ShotsJSON: `[]`, ExpectedDeliveryJSON: `{}`,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	artifacts := []model.AgentProductionArtifact{
		{
			ID: "postgres-paused-script", PlanKey: plan.PlanKey, PlanVersionID: plan.ID, PlanVersion: plan.Version,
			Kind: model.AgentProductionArtifactScript, Status: model.AgentProductionArtifactSucceeded,
			CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute),
		},
		{
			ID: "postgres-paused-committed-image", PlanKey: plan.PlanKey, PlanVersionID: plan.ID, PlanVersion: plan.Version,
			Kind: model.AgentProductionArtifactStoryboardImage, Status: model.AgentProductionArtifactCommitted,
			ResourceID: "postgres-paused-resource", CanvasNodeID: "postgres-paused-node",
			CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute),
		},
		{
			ID: "postgres-paused-artifact", PlanKey: plan.PlanKey, PlanVersionID: plan.ID, PlanVersion: plan.Version,
			Kind: model.AgentProductionArtifactVideoClip, Status: model.AgentProductionArtifactAwaitingApproval,
			CreatedAt: now, UpdatedAt: now,
		},
	}
	if err := db.Create(&artifacts).Error; err != nil {
		t.Fatal(err)
	}
	audit, err := database.AuditAgentRuntimeUpgrade(db)
	if err != nil {
		t.Fatal(err)
	}
	if audit.CandidateRuns != 1 || audit.RetirableRuns != 1 || len(audit.Blockers) != 0 {
		t.Fatalf("PostgreSQL paused retirement audit = %#v", audit)
	}
	var auditRun model.AgentRun
	if err := db.First(&auditRun, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if auditRun.Status != agentruntime.RunWaitingApproval || auditRun.StateVersion != run.StateVersion || auditRun.LastEventSequence != run.LastEventSequence {
		t.Fatalf("PostgreSQL audit mutated run = %#v", auditRun)
	}
	var auditPendingArtifact model.AgentProductionArtifact
	if err := db.First(&auditPendingArtifact, "id = ?", "postgres-paused-artifact").Error; err != nil {
		t.Fatal(err)
	}
	if auditPendingArtifact.Status != model.AgentProductionArtifactAwaitingApproval || auditPendingArtifact.LastErrorCode != "" {
		t.Fatalf("PostgreSQL audit mutated pending artifact = %#v", auditPendingArtifact)
	}

	if err := database.EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatalf("PostgreSQL upgrade rejected terminal tool history: %v", err)
	}
	var retired model.AgentRun
	if err := db.First(&retired, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if retired.Status != agentruntime.RunCancelled {
		t.Fatalf("PostgreSQL paused run status = %s", retired.Status)
	}
	var preserved model.AgentToolCall
	if err := db.First(&preserved, "id = ?", historical.ID).Error; err != nil {
		t.Fatal(err)
	}
	if preserved.Status != historical.Status || preserved.StartedAt == nil || !preserved.StartedAt.Equal(historicalStartedAt) || preserved.OutputJSON != historical.OutputJSON {
		t.Fatalf("PostgreSQL historical tool call was mutated = %#v", preserved)
	}
	var failedCurrent model.AgentToolCall
	if err := db.First(&failedCurrent, "id = ?", current.ID).Error; err != nil {
		t.Fatal(err)
	}
	if failedCurrent.Status != agentruntime.ToolCallFailed || failedCurrent.ErrorCode != "runtime_contract_retired" {
		t.Fatalf("PostgreSQL current tool call was not retired = %#v", failedCurrent)
	}
	var failedPendingArtifact model.AgentProductionArtifact
	if err := db.First(&failedPendingArtifact, "id = ?", "postgres-paused-artifact").Error; err != nil {
		t.Fatal(err)
	}
	if failedPendingArtifact.Status != model.AgentProductionArtifactFailed || failedPendingArtifact.LastErrorCode != "runtime_contract_retired" {
		t.Fatalf("PostgreSQL current pending artifact was not retired = %#v", failedPendingArtifact)
	}
	for artifactID, wantStatus := range map[string]model.AgentProductionArtifactStatus{
		"postgres-paused-script":          model.AgentProductionArtifactSucceeded,
		"postgres-paused-committed-image": model.AgentProductionArtifactCommitted,
	} {
		var preserved model.AgentProductionArtifact
		if err := db.First(&preserved, "id = ?", artifactID).Error; err != nil {
			t.Fatal(err)
		}
		if preserved.Status != wantStatus {
			t.Fatalf("PostgreSQL terminal artifact was mutated = %#v", preserved)
		}
	}
}

func TestPostgresAgentRuntimeUpgradeRejectsLegacyQueuedRunWithExternalFacts(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	const stateJSON = `{"stateVersion":1,"stepNumber":0,"maxSteps":6,"status":"queued","userMessage":"生成短剧样片"}`
	run := model.AgentRun{
		ID: "postgres-legacy-queued-run", ThreadID: "postgres-legacy-thread", ActorUserID: "postgres-legacy-user",
		ClientRequestID: "postgres-legacy-request", Status: agentruntime.RunQueued, LastEventSequence: 1,
		StateVersion: 1, MaxSteps: 6, ModelRecordID: "postgres-legacy-model-record",
		ModelKey: "postgres-legacy-model", ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion - 1,
		RuntimeVersion: 1, PolicyVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AgentRunEvent{
		ID: "postgres-legacy-event-1", RunID: run.ID, Sequence: 1,
		Kind: agentruntime.EventRunCreated, PayloadJSON: stateJSON, CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AgentCheckpoint{
		ID: "postgres-legacy-checkpoint-1", RunID: run.ID, Sequence: 1,
		StateVersion: 1, StateJSON: stateJSON, CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	legacyTask := model.Task{
		ID: legacyAgentRuntimeModelTaskID(run.ID, 0), UserID: run.ActorUserID, ProjectID: "postgres-legacy-canvas",
		Type: "agent_runtime_model", Capability: "text", Status: model.TaskStatusSucceeded, Operation: "agent_model:" + run.ID,
		Provider: "system", Model: run.ModelKey, BillingOrderID: "postgres-legacy-model-order",
		CreatedAt: now, UpdatedAt: now, CompletedAt: &now,
	}
	legacyOrder := model.BillingOrder{
		ID: legacyTask.BillingOrderID, UserID: run.ActorUserID, TaskID: legacyTask.ID,
		IdempotencyKey: "agent-runtime:" + run.ID + ":0", Scene: "agent_runtime_model",
		ChannelModelID: run.ModelRecordID, Model: run.ModelKey, Capability: "text",
		Quantity: 1, Status: model.BillingStatusSettled, AmountMicrocredits: 1,
		CreatedAt: now, UpdatedAt: now, SettledAt: &now,
	}
	if err := db.Create(&legacyTask).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&legacyOrder).Error; err != nil {
		t.Fatal(err)
	}
	active := model.AgentRun{
		ID: "postgres-running-blocker", ThreadID: "postgres-running-thread", ActorUserID: "postgres-running-user",
		ClientRequestID: "postgres-running-request", Status: agentruntime.RunRunning,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion - 1, RuntimeVersion: 1, PolicyVersion: 1,
		CreatedAt: now.Add(time.Second), UpdatedAt: now,
	}
	if err := db.Create(&active).Error; err != nil {
		t.Fatal(err)
	}
	audit, err := database.AuditAgentRuntimeUpgrade(db)
	if err != nil {
		t.Fatal(err)
	}
	if audit.CandidateRuns != 2 || audit.RetirableRuns != 0 || len(audit.Blockers) != 3 {
		t.Fatalf("PostgreSQL full upgrade audit = %#v", audit)
	}
	for _, expected := range []struct {
		runID    string
		category string
	}{
		{runID: run.ID, category: "external_billing"},
		{runID: run.ID, category: "external_model_task"},
		{runID: active.ID, category: "non_retirable_active_status"},
	} {
		found := false
		for _, blocker := range audit.Blockers {
			if blocker.RunID == expected.runID && blocker.Category == expected.category {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("PostgreSQL audit missing run=%s category=%s: %#v", expected.runID, expected.category, audit)
		}
	}

	if err := database.EnsureAgentRuntimeIntegritySchema(db); err == nil ||
		!strings.Contains(err.Error(), `"runId":"`+run.ID+`"`) ||
		!strings.Contains(err.Error(), `"runId":"`+active.ID+`"`) ||
		!strings.Contains(err.Error(), `"category":"external_billing"`) ||
		!strings.Contains(err.Error(), `"category":"external_model_task"`) ||
		!strings.Contains(err.Error(), `"category":"non_retirable_active_status"`) {
		t.Fatalf("PostgreSQL hard cutover accepted a run with commercial facts: %v", err)
	}
	var stored model.AgentRun
	if err := db.First(&stored, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != agentruntime.RunQueued || stored.StateVersion != run.StateVersion || stored.LastEventSequence != run.LastEventSequence || stored.CompletedAt != nil {
		t.Fatalf("PostgreSQL hard cutover changed a run with commercial facts: %#v", stored)
	}
	var storedActive model.AgentRun
	if err := db.First(&storedActive, "id = ?", active.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedActive.Status != active.Status || storedActive.CompletedAt != nil {
		t.Fatalf("PostgreSQL hard cutover changed a non-retirable active run: %#v", storedActive)
	}
	var storedTask model.Task
	if err := db.First(&storedTask, "id = ?", legacyTask.ID).Error; err != nil {
		t.Fatal(err)
	}
	var storedOrder model.BillingOrder
	if err := db.First(&storedOrder, "id = ?", legacyOrder.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != legacyTask.Status || storedOrder.Status != legacyOrder.Status || storedOrder.AmountMicrocredits != legacyOrder.AmountMicrocredits {
		t.Fatalf("PostgreSQL rejected migration changed commercial facts: task=%#v order=%#v", storedTask, storedOrder)
	}
}

func legacyAgentRuntimeModelTaskID(runID string, step int) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("agent-runtime-model\x00%s\x00%d", runID, step)))
	return fmt.Sprintf("agt_%x", digest[:16])
}

func TestPostgresAgentRuntimeIntegrityAndScopeIsolation(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	expected := map[string]struct {
		unique  bool
		table   string
		columns string
	}{
		"idx_agent_runs_thread_client_request": {unique: true, table: "agent_runs", columns: "thread_id,client_request_id"},
		"idx_agent_run_events_run_sequence":    {unique: true, table: "agent_run_events", columns: "run_id,sequence"},
		"idx_agent_checkpoints_run_sequence":   {unique: true, table: "agent_checkpoints", columns: "run_id,sequence"},
		"idx_agent_tool_calls_action":          {unique: true, table: "agent_tool_calls", columns: "run_id,tool_call_id,action_version"},
		"idx_agent_threads_scope":              {unique: false, table: "agent_threads", columns: "tenant_kind,tenant_id,canvas_id,updated_at"},
	}
	for name, want := range expected {
		var actual struct {
			Unique  bool   `gorm:"column:is_unique"`
			Table   string `gorm:"column:table_name"`
			Columns string `gorm:"column:columns"`
		}
		result := db.Raw(`
			SELECT indexes.indisunique AS is_unique, tables.relname AS table_name,
			       string_agg(attributes.attname, ',' ORDER BY keys.ordinality) AS columns
			  FROM pg_class names
			  JOIN pg_namespace namespaces ON namespaces.oid = names.relnamespace
			  JOIN pg_index indexes ON indexes.indexrelid = names.oid
			  JOIN pg_class tables ON tables.oid = indexes.indrelid
			  JOIN LATERAL unnest(indexes.indkey) WITH ORDINALITY AS keys(attnum, ordinality) ON keys.attnum > 0
			  JOIN pg_attribute attributes ON attributes.attrelid = tables.oid AND attributes.attnum = keys.attnum
			 WHERE namespaces.nspname = current_schema() AND names.relname = ?
			 GROUP BY indexes.indisunique, tables.relname`, name).Scan(&actual)
		if result.Error != nil || result.RowsAffected != 1 {
			t.Fatalf("read index %s: rows=%d err=%v", name, result.RowsAffected, result.Error)
		}
		if actual.Unique != want.unique || actual.Table != want.table || actual.Columns != want.columns {
			t.Fatalf("index %s = %#v, want %#v", name, actual, want)
		}
	}

	repo := New(db)
	now := time.Now().UTC()
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	other := scope
	other.TenantID = "agent-user-other"
	other.ActorUserID = "agent-user-other"
	if _, err := repo.AgentRunForScope(other); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("PostgreSQL cross-tenant read error = %v", err)
	}

	concurrentScope := repositoryAgentScope()
	concurrentScope.ThreadID = "postgres-concurrent-thread"
	const contenders = 8
	const queryBarrier = "agent_runtime_concurrent_thread_create_barrier"
	arrived := make(chan struct{}, contenders)
	release := make(chan struct{})
	if err := db.Callback().Create().Before("gorm:create").Register(queryBarrier, func(tx *gorm.DB) {
		if tx.Statement.Table != "agent_threads" {
			return
		}
		select {
		case arrived <- struct{}{}:
		case <-release:
			return
		}
		<-release
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(queryBarrier) })
	results := make(chan *AgentRunRecord, contenders)
	errs := make(chan error, contenders)
	var wait sync.WaitGroup
	for index := 0; index < contenders; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			candidate := concurrentScope
			candidate.RunID = fmt.Sprintf("postgres-concurrent-run-%d", index)
			record, err := repo.CreateAgentRun(CreateAgentRunInput{Scope: candidate, ClientRequestID: "postgres-one-request", Now: now.Add(time.Duration(index) * time.Millisecond)})
			if err != nil {
				errs <- err
				return
			}
			results <- record
		}(index)
	}
	for index := 0; index < contenders; index++ {
		select {
		case <-arrived:
		case <-time.After(10 * time.Second):
			t.Fatal("concurrent thread lookup barrier was not reached")
		}
	}
	close(release)
	wait.Wait()
	close(errs)
	close(results)
	for err := range errs {
		t.Fatalf("concurrent idempotent run creation failed: %v", err)
	}
	winningRunID := ""
	createdCount := 0
	for record := range results {
		if winningRunID == "" {
			winningRunID = record.Run.ID
		}
		if record.Run.ID != winningRunID {
			t.Fatalf("concurrent request returned different runs: %s and %s", winningRunID, record.Run.ID)
		}
		if record.Created {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Fatalf("concurrent request created %d runs, want 1", createdCount)
	}

	wrongDB := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(wrongDB); err != nil {
		t.Fatal(err)
	}
	const wrongIndex = `CREATE UNIQUE INDEX idx_agent_run_events_run_sequence ON agent_run_events(run_id, kind)`
	if err := wrongDB.Exec(wrongIndex).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureAgentRuntimeIntegritySchema(wrongDB); err == nil || !strings.Contains(err.Error(), "idx_agent_run_events_run_sequence") {
		t.Fatalf("wrong PostgreSQL agent index error = %v", err)
	}

	duplicateDB := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(duplicateDB); err != nil {
		t.Fatal(err)
	}
	duplicates := []model.AgentRunEvent{
		{ID: "postgres-event-a", RunID: "postgres-duplicate-run", Sequence: 1, Kind: agentruntime.EventRunCreated, PayloadJSON: `{}`, CreatedAt: now},
		{ID: "postgres-event-b", RunID: "postgres-duplicate-run", Sequence: 1, Kind: agentruntime.EventRunStatusChanged, PayloadJSON: `{}`, CreatedAt: now},
	}
	if err := duplicateDB.Create(&duplicates).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureAgentRuntimeIntegritySchema(duplicateDB); err == nil || !strings.Contains(err.Error(), "postgres-duplicate-run") {
		t.Fatalf("duplicate PostgreSQL event facts error = %v", err)
	}
}

func TestPostgresAgentEventSequenceAcrossConnections(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	secondDB := openSecondAgentPostgresConnection(t, db)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, New(db), scope)

	const workers = 32
	sequences := make(chan int64, workers)
	errs := make(chan error, workers)
	var wait sync.WaitGroup
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			repo := New(db)
			if index%2 == 1 {
				repo = New(secondDB)
			}
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
		t.Fatalf("PostgreSQL concurrent append failed: %v", err)
	}
	actual := make([]int, 0, workers)
	for sequence := range sequences {
		actual = append(actual, int(sequence))
	}
	sort.Ints(actual)
	for index, sequence := range actual {
		if sequence != index+1 {
			t.Fatalf("PostgreSQL event sequences = %v", actual)
		}
	}

	const callback = "postgres_agent_checkpoint_failure"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*model.AgentCheckpoint); ok {
			tx.AddError(errors.New("postgres checkpoint insert failed"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
	_, err := New(db).AppendAgentEvent(AppendAgentEventInput{
		Scope: scope, Kind: agentruntime.EventCheckpointSaved, PayloadJSON: `{}`,
		Checkpoint: &AgentCheckpointInput{StateVersion: 1, StateJSON: `{}`}, Now: time.Now().UTC(),
	})
	if err == nil || err.Error() != "postgres checkpoint insert failed" {
		t.Fatalf("PostgreSQL checkpoint failure = %v", err)
	}
	var run model.AgentRun
	if err := db.First(&run, "id = ?", scope.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if run.LastEventSequence != workers {
		t.Fatalf("failed PostgreSQL checkpoint advanced sequence to %d", run.LastEventSequence)
	}
}

func TestPostgresAgentRuntimeTransitionCASAcrossConnections(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	secondDB := openSecondAgentPostgresConnection(t, db)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, New(db), scope)
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).Updates(struct {
		MaxSteps     int `gorm:"column:max_steps"`
		StateVersion int `gorm:"column:state_version"`
	}{MaxSteps: 3, StateVersion: 1}).Error; err != nil {
		t.Fatal(err)
	}
	transition := agentruntime.RuntimeTransition{State: agentruntime.RuntimeState{StateVersion: 2, StepNumber: 1, MaxSteps: 3, Status: agentruntime.RunRunning, UserMessage: "test"}, EventKinds: []agentruntime.EventKind{agentruntime.EventRunStatusChanged}}
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, candidate := range []*Repository{New(db), New(secondDB)} {
		go func(repo *Repository) {
			<-start
			errs <- repo.CommitAgentRuntimeTransition(scope, agentruntime.RuntimeState{StateVersion: 1, StepNumber: 0, MaxSteps: 3, Status: agentruntime.RunQueued, UserMessage: "test"}, transition, time.Now().UTC())
		}(candidate)
	}
	close(start)
	success, conflict := 0, 0
	for range 2 {
		err := <-errs
		if err == nil {
			success++
		} else if errors.Is(err, ErrAgentRuntimeStepConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("PostgreSQL transition CAS: success=%d conflict=%d", success, conflict)
	}
	var run model.AgentRun
	if err := db.First(&run, "id = ?", scope.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if run.StepNumber != 1 || run.LastEventSequence != 1 {
		t.Fatalf("PostgreSQL transition facts = %#v", run)
	}
}

func TestPostgresAgentRuntimeToolCompletionCASAcrossConnections(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	secondDB := openSecondAgentPostgresConnection(t, db)
	scope := repositoryAgentScope()
	repo := New(db)
	createAgentRunForTest(t, repo, scope)
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-1", ModelKey: "gpt-5.5", MaxSteps: 4,
		ToolSchemaVersion: 1, RuntimeVersion: 1, PolicyVersion: 1, UserMessage: "读取当前画布",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic}, Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	current, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	requested, err := agentruntime.Advance(current, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "call-read-state", ToolName: agentruntime.ToolProductionPlan,
			ActionVersion: 1, Arguments: []byte(`{"planKey":"plan-pg"}`), ExpectedDelivery: repositoryTestAnswerDelivery(),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, current, requested, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	resolved, err := agentruntime.ResolveTool(requested.State, agentruntime.ToolResolution{
		ToolCallID: "call-read-state", ActionVersion: 1, Succeeded: true,
		Output: []byte(`{"canvasId":"canvas-1","revision":7}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, candidate := range []*Repository{repo, New(secondDB)} {
		go func(currentRepo *Repository) {
			<-start
			errs <- currentRepo.CommitAgentRuntimeTransition(scope, requested.State, resolved, time.Now().UTC())
		}(candidate)
	}
	close(start)
	var succeeded, conflicted int
	for range 2 {
		switch err := <-errs; {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrAgentRuntimeStepConflict):
			conflicted++
		default:
			t.Fatalf("tool completion error = %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("tool completion CAS succeeded=%d conflicted=%d", succeeded, conflicted)
	}
	loaded, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.StateVersion != 3 || loaded.StepNumber != 1 || loaded.Status != agentruntime.RunRunning || loaded.LastToolResult == nil {
		t.Fatalf("tool completion checkpoint = %#v", loaded)
	}
	var call model.AgentToolCall
	if err := db.First(&call, "run_id = ? AND tool_call_id = ?", scope.RunID, "call-read-state").Error; err != nil {
		t.Fatal(err)
	}
	if call.Status != agentruntime.ToolCallSucceeded || call.OutputJSON != `{"canvasId":"canvas-1","revision":7}` {
		t.Fatalf("tool completion call = %#v", call)
	}
}

func TestPostgresAgentCanvasMutationRecoveryAcrossConnections(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	secondDB := openSecondAgentPostgresConnection(t, db)
	firstRepo, secondRepo := New(db), New(secondDB)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, firstRepo, scope)
	if _, err := firstRepo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-1", ModelKey: "gpt-5.5", MaxSteps: 4,
		ToolSchemaVersion: 1, RuntimeVersion: 1, PolicyVersion: 1, UserMessage: "修改当前画布",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic}, Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	current, err := firstRepo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	requested, err := agentruntime.Advance(current, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall,
		ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "call-pg-apply", ToolName: agentruntime.ToolCanvasCommit, ActionVersion: 1,
			Arguments:        []byte(`{"planKey":"plan-pg","planVersion":1,"baseRevision":7,"artifactIds":["artifact-pg"]}`),
			ExpectedDelivery: repositoryTestCanvasDelivery(),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := firstRepo.CommitAgentRuntimeTransition(scope, current, requested, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	started, err := agentruntime.BeginToolExecution(requested.State, agentruntime.ToolExecution{
		ToolCallID: "call-pg-apply", ActionVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := firstRepo.CommitAgentRuntimeTransition(scope, requested.State, started, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.Create(&model.CanvasProject{
		ID: scope.CanvasID, UserID: scope.ActorUserID, Title: "before", Revision: 7,
		PayloadJSON: `{"nodes":[],"connections":[]}`, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	clientMutationID := "agent-pg-recovery"
	changePayload := `{"upsertNodes":[{"id":"node-pg"}]}`
	firstCommit, err := firstRepo.CommitCanvasChange(
		scope.CanvasID, "change-first", scope.ActorUserID, 7, clientMutationID, changePayload, time.Now().UTC(),
		func(current *model.CanvasProject) (string, string, error) {
			return `{"nodes":[{"id":"node-pg"}],"connections":[]}`, current.Title, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if firstCommit.Change.Revision != 8 {
		t.Fatalf("first canvas commit = %#v", firstCommit)
	}
	replayed, err := secondRepo.CommitCanvasChange(
		scope.CanvasID, "change-retry", scope.ActorUserID, 7, clientMutationID, changePayload, time.Now().UTC(),
		func(current *model.CanvasProject) (string, string, error) {
			t.Fatal("idempotent canvas replay called apply callback")
			return "", "", nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Change.ID != firstCommit.Change.ID || replayed.Change.Revision != 8 {
		t.Fatalf("replayed canvas commit = %#v", replayed)
	}
	resolved, err := agentruntime.ResolveTool(started.State, agentruntime.ToolResolution{
		ToolCallID: "call-pg-apply", ActionVersion: 1, Succeeded: true,
		Output: []byte(`{"canvasId":"agent-canvas-1","baseRevision":7,"committedRevision":8,"clientMutationId":"agent-pg-recovery"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := secondRepo.CommitAgentRuntimeTransition(scope, started.State, resolved, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	loaded, err := firstRepo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != agentruntime.RunRunning || loaded.LastToolResult == nil || !loaded.LastToolResult.Succeeded {
		t.Fatalf("recovered runtime = %#v", loaded)
	}
	var changes int64
	if err := db.Model(&model.CanvasChange{}).Where("canvas_id = ?", scope.CanvasID).Count(&changes).Error; err != nil {
		t.Fatal(err)
	}
	if changes != 1 {
		t.Fatalf("canvas changes after PostgreSQL recovery = %d", changes)
	}
}

func TestPostgresAgentRuntimeInitializationCASAcrossConnections(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	secondDB := openSecondAgentPostgresConnection(t, db)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, New(db), scope)
	input := InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "agent-model-record", ModelKey: "gpt-5.5",
		MaxSteps: 6, ToolSchemaVersion: 1, RuntimeVersion: 1, PolicyVersion: 1, UserMessage: "读取画布并给出下一步",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: time.Now().UTC(),
	}
	start := make(chan struct{})
	results := make(chan *InitializedAgentRun, 2)
	errs := make(chan error, 2)
	for _, candidate := range []*Repository{New(db), New(secondDB)} {
		go func(repo *Repository) {
			<-start
			result, err := repo.InitializeAgentRun(input)
			results <- result
			errs <- err
		}(candidate)
	}
	close(start)
	created := 0
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		result := <-results
		if result == nil || result.Run.ModelRecordID != input.ModelRecordID || result.Run.ModelKey != input.ModelKey || result.Run.MaxSteps != input.MaxSteps {
			t.Fatalf("PostgreSQL initialization result = %#v", result)
		}
		if result.Created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("PostgreSQL initialization created=%d", created)
	}
	var eventCount int64
	var checkpointCount int64
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ?", scope.RunID).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentCheckpoint{}).Where("run_id = ?", scope.RunID).Count(&checkpointCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 2 || checkpointCount != 1 {
		t.Fatalf("PostgreSQL initialization facts: events=%d checkpoints=%d", eventCount, checkpointCount)
	}
	input.ModelKey = "different-model"
	if _, err := New(secondDB).InitializeAgentRun(input); !errors.Is(err, ErrAgentRuntimeInitializationConflict) {
		t.Fatalf("PostgreSQL conflicting replay = %v", err)
	}
}

func TestPostgresAgentProductionPlanVersionCASAcrossConnections(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	secondDB := openSecondAgentPostgresConnection(t, db)
	firstRepo, secondRepo := New(db), New(secondDB)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, firstRepo, scope)
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := firstRepo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "postgres-production-plan", BaseVersion: 0,
		Draft: twoShotProductionPlanDraft("基础版本"), Now: now,
	}); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wait sync.WaitGroup
	for index, candidate := range []*Repository{firstRepo, secondRepo} {
		index, candidate := index, candidate
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := candidate.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
				Scope: scope, RunID: scope.RunID, PlanKey: "postgres-production-plan", BaseVersion: 1,
				Draft: twoShotProductionPlanDraft(fmt.Sprintf("并发版本 %d", index+1)), Now: now.Add(time.Second),
			})
			errs <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errs)

	succeeded, conflicted := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrAgentProductionPlanVersionConflict):
			conflicted++
		default:
			t.Fatalf("PostgreSQL production plan append error = %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("PostgreSQL production plan CAS: succeeded=%d conflicted=%d", succeeded, conflicted)
	}
	var plans int64
	var artifacts int64
	if err := db.Model(&model.AgentProductionPlanVersion{}).Where("plan_key = ?", "postgres-production-plan").Count(&plans).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentProductionArtifact{}).Where("plan_key = ?", "postgres-production-plan").Count(&artifacts).Error; err != nil {
		t.Fatal(err)
	}
	if plans != 2 || artifacts != 10 {
		t.Fatalf("PostgreSQL production plan facts: plans=%d artifacts=%d", plans, artifacts)
	}
}

func openSecondAgentPostgresConnection(t *testing.T, db *gorm.DB) *gorm.DB {
	t.Helper()
	dialector, ok := db.Dialector.(*postgresdriver.Dialector)
	if !ok || dialector.Config == nil || strings.TrimSpace(dialector.Config.DSN) == "" {
		t.Fatal("PostgreSQL integration DSN is unavailable")
	}
	second, err := database.Open(database.Config{Driver: "postgres", DSN: dialector.Config.DSN})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := second.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return second
}
