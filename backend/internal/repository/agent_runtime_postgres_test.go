package repository

import (
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
		ToolSchemaVersion: 1, UserMessage: "读取当前画布",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	current, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	requested, err := agentruntime.Advance(current, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "call-read-state", ToolName: agentruntime.ToolCanvasReadState,
			ActionVersion: 1, Arguments: []byte(`{}`),
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
		ToolSchemaVersion: 1, UserMessage: "修改当前画布",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: time.Now().UTC(),
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
			ToolCallID: "call-pg-apply", ToolName: agentruntime.ToolCanvasApplyOps, ActionVersion: 1,
			Arguments: []byte(`{"baseRevision":7,"patch":{"upsertNodes":[{"id":"node-pg"}]}}`),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := firstRepo.CommitAgentRuntimeTransition(scope, current, requested, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	approved, err := agentruntime.ReviewToolApproval(requested.State, agentruntime.ToolApproval{
		ToolCallID: "call-pg-apply", ActionVersion: 1, Decision: agentruntime.ToolApprovalApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := firstRepo.CommitAgentRuntimeTransition(scope, requested.State, approved, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	started, err := agentruntime.BeginToolExecution(approved.State, agentruntime.ToolExecution{
		ToolCallID: "call-pg-apply", ActionVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := firstRepo.CommitAgentRuntimeTransition(scope, approved.State, started, time.Now().UTC()); err != nil {
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
		MaxSteps: 6, ToolSchemaVersion: 1, UserMessage: "读取画布并给出下一步",
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
	if eventCount != 1 || checkpointCount != 1 {
		t.Fatalf("PostgreSQL initialization facts: events=%d checkpoints=%d", eventCount, checkpointCount)
	}
	input.ModelKey = "different-model"
	if _, err := New(secondDB).InitializeAgentRun(input); !errors.Is(err, ErrAgentRuntimeInitializationConflict) {
		t.Fatalf("PostgreSQL conflicting replay = %v", err)
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
