package repository

import (
	"errors"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func TestCommitAgentRuntimeTransitionPersistsRunEventsAndCheckpointAtomically(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).Updates(model.AgentRun{MaxSteps: 3, ModelRecordID: "model-1", ModelKey: "gpt", ToolSchemaVersion: 1}).Error; err != nil {
		t.Fatal(err)
	}
	state := agentruntime.RuntimeState{StateVersion: 2, StepNumber: 1, MaxSteps: 3, Status: agentruntime.RunRunning}
	transition := agentruntime.RuntimeTransition{State: state, EventKinds: []agentruntime.EventKind{agentruntime.EventRunStatusChanged, agentruntime.EventCheckpointSaved}}
	if err := repo.CommitAgentRuntimeTransition(scope, 0, transition, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.StepNumber != 1 || loaded.StateVersion != 2 || loaded.Status != agentruntime.RunRunning {
		t.Fatalf("loaded state = %#v", loaded)
	}
	var run model.AgentRun
	if err := db.First(&run, "id = ?", scope.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if run.StepNumber != 1 || run.LastEventSequence != 2 || run.Status != agentruntime.RunRunning {
		t.Fatalf("run = %#v", run)
	}
	var eventCount, checkpointCount int64
	db.Model(&model.AgentRunEvent{}).Where("run_id = ?", scope.RunID).Count(&eventCount)
	db.Model(&model.AgentCheckpoint{}).Where("run_id = ?", scope.RunID).Count(&checkpointCount)
	if eventCount != 2 || checkpointCount != 1 {
		t.Fatalf("facts: events=%d checkpoints=%d", eventCount, checkpointCount)
	}
}

func TestCommitAgentRuntimeTransitionFencesConcurrentStepAndRollsBack(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLiteFile(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).Update("max_steps", 3).Error; err != nil {
		t.Fatal(err)
	}
	transition := agentruntime.RuntimeTransition{State: agentruntime.RuntimeState{StateVersion: 2, StepNumber: 1, MaxSteps: 3, Status: agentruntime.RunRunning}, EventKinds: []agentruntime.EventKind{agentruntime.EventRunStatusChanged}}
	var wait sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errs <- repo.CommitAgentRuntimeTransition(scope, 0, transition, time.Now().UTC())
		}()
	}
	wait.Wait()
	close(errs)
	success, conflict := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else if errors.Is(err, ErrAgentRuntimeStepConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).Updates(model.AgentRun{Status: agentruntime.RunSucceeded, StepNumber: 2}).Error; err != nil {
		t.Fatal(err)
	}
	terminalTransition := agentruntime.RuntimeTransition{State: agentruntime.RuntimeState{StateVersion: 4, StepNumber: 3, MaxSteps: 3, Status: agentruntime.RunFailed}, EventKinds: []agentruntime.EventKind{agentruntime.EventRunFailed}}
	if err := repo.CommitAgentRuntimeTransition(scope, 2, terminalTransition, time.Now().UTC()); !errors.Is(err, ErrAgentRuntimeStepConflict) {
		t.Fatalf("terminal overwrite error = %v", err)
	}

	other := scope
	other.TenantID, other.ActorUserID = "other", "other"
	if _, err := repo.LoadAgentCheckpoint(other); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-scope load error = %v", err)
	}
}

func TestLoadAgentCheckpointRejectsSnapshotDrift(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).Update("max_steps", 3).Error; err != nil {
		t.Fatal(err)
	}
	transition := agentruntime.RuntimeTransition{State: agentruntime.RuntimeState{StateVersion: 2, StepNumber: 1, MaxSteps: 3, Status: agentruntime.RunRunning}, EventKinds: []agentruntime.EventKind{agentruntime.EventRunStatusChanged}}
	if err := repo.CommitAgentRuntimeTransition(scope, 0, transition, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).Update("status", agentruntime.RunFailed).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.LoadAgentCheckpoint(scope); err == nil {
		t.Fatal("drifted run/checkpoint snapshot was accepted")
	}
}

func TestCommitAgentRuntimeTransitionRollsBackRunAndEventsWhenCheckpointFails(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).Update("max_steps", 3).Error; err != nil {
		t.Fatal(err)
	}
	const callback = "agent_runtime_transition_checkpoint_failure"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*model.AgentCheckpoint); ok {
			tx.AddError(errors.New("transition checkpoint failed"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
	transition := agentruntime.RuntimeTransition{State: agentruntime.RuntimeState{StateVersion: 2, StepNumber: 1, MaxSteps: 3, Status: agentruntime.RunRunning}, EventKinds: []agentruntime.EventKind{agentruntime.EventRunStatusChanged}}
	if err := repo.CommitAgentRuntimeTransition(scope, 0, transition, time.Now().UTC()); err == nil || err.Error() != "transition checkpoint failed" {
		t.Fatalf("checkpoint failure = %v", err)
	}
	var run model.AgentRun
	if err := db.First(&run, "id = ?", scope.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if run.StepNumber != 0 || run.LastEventSequence != 0 || run.Status != agentruntime.RunQueued {
		t.Fatalf("rolled back run = %#v", run)
	}
	var eventCount int64
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ?", scope.RunID).Count(&eventCount).Error; err != nil || eventCount != 0 {
		t.Fatalf("rolled back events = %d, %v", eventCount, err)
	}
}
