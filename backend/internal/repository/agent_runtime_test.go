package repository

import (
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
		&model.AgentRunEvent{},
		&model.AgentCheckpoint{},
		&model.AgentToolCall{},
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
	if err := db.AutoMigrate(&model.AgentThread{}, &model.AgentRun{}, &model.AgentRunEvent{}, &model.AgentCheckpoint{}, &model.AgentToolCall{}); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	return New(db), db
}
