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

func TestInitializeAgentRunFreezesModelAndCreatesCheckpointOnce(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLiteFile(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC()
	input := InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-record-1", ModelKey: "gpt-5.5",
		MaxSteps: 4, ToolSchemaVersion: 1, UserMessage: "请根据当前画布继续完成任务", Now: now,
	}
	initialized, err := repo.InitializeAgentRun(input)
	if err != nil {
		t.Fatal(err)
	}
	if !initialized.Created || initialized.Run.StateVersion != 1 || initialized.Run.ModelRecordID != input.ModelRecordID || initialized.Run.ModelKey != input.ModelKey || initialized.Run.MaxSteps != input.MaxSteps {
		t.Fatalf("initialized run = %#v", initialized)
	}
	loaded, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.StateVersion != 1 || loaded.StepNumber != 0 || loaded.Status != agentruntime.RunQueued || loaded.UserMessage != input.UserMessage {
		t.Fatalf("initial checkpoint = %#v", loaded)
	}
	replayed, err := repo.InitializeAgentRun(input)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Created {
		t.Fatalf("initialization replay created facts = %#v", replayed)
	}
	var eventCount, checkpointCount int64
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ?", scope.RunID).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentCheckpoint{}).Where("run_id = ?", scope.RunID).Count(&checkpointCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 || checkpointCount != 1 {
		t.Fatalf("initial facts duplicated: events=%d checkpoints=%d", eventCount, checkpointCount)
	}

	conflict := input
	conflict.ModelKey = "deepseek-v4-pro"
	if _, err := repo.InitializeAgentRun(conflict); !errors.Is(err, ErrAgentRuntimeInitializationConflict) {
		t.Fatalf("different model replay error = %v", err)
	}
	conflict = input
	conflict.UserMessage = "另一个用户请求"
	if _, err := repo.InitializeAgentRun(conflict); !errors.Is(err, ErrAgentRuntimeInitializationConflict) {
		t.Fatalf("different user message replay error = %v", err)
	}
}

func TestCommitAgentRuntimeTransitionRegistersAndCompletesToolAtomically(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-1", ModelKey: "gpt-5.5", MaxSteps: 4,
		ToolSchemaVersion: 1, UserMessage: "读取当前画布", Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	current, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	decision := agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{
		ToolCallID: "call-read-state", ToolName: agentruntime.ToolCanvasReadState,
		ActionVersion: 1, Arguments: []byte(`{"expectedRevision":7}`),
	}}
	requested, err := agentruntime.Advance(current, agentruntime.RuntimeInput{Decision: decision})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, current, requested, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	var call model.AgentToolCall
	if err := db.First(&call, "run_id = ? AND tool_call_id = ? AND action_version = ?", scope.RunID, "call-read-state", 1).Error; err != nil {
		t.Fatal(err)
	}
	if call.Status != agentruntime.ToolCallPending || call.RiskLevel != agentruntime.ToolRiskRead || call.RequiredAccess != agentruntime.AccessViewer || call.ApprovalRequired || call.IdempotencyKey != scope.RunID+":call-read-state:1" || call.InputJSON != `{"expectedRevision":7}` {
		t.Fatalf("registered call = %#v", call)
	}
	resolved, err := agentruntime.ResolveTool(requested.State, agentruntime.ToolResolution{
		ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion, Succeeded: true,
		Output: []byte(`{"canvasId":"canvas-1","revision":7}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, requested.State, resolved, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&call, "id = ?", call.ID).Error; err != nil {
		t.Fatal(err)
	}
	if call.Status != agentruntime.ToolCallSucceeded || call.OutputJSON != `{"canvasId":"canvas-1","revision":7}` {
		t.Fatalf("completed call = %#v", call)
	}
	loaded, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.StateVersion != 3 || loaded.StepNumber != 1 || loaded.Status != agentruntime.RunRunning || loaded.LastToolResult == nil {
		t.Fatalf("resolved checkpoint = %#v", loaded)
	}
}

func TestCommitAgentRuntimeTransitionPersistsToolExecutionStartAtomically(t *testing.T) {
	repo, _ := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-1", ModelKey: "gpt-5.5", MaxSteps: 4,
		ToolSchemaVersion: 1, UserMessage: "修改当前画布", Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	current, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	requested, err := agentruntime.Advance(current, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall,
		ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "call-apply", ToolName: agentruntime.ToolCanvasApplyOps, ActionVersion: 1,
			Arguments: []byte(`{"baseRevision":7,"patch":{"deleteNodeIds":["obsolete"]}}`),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, current, requested, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	approved, err := agentruntime.ReviewToolApproval(requested.State, agentruntime.ToolApproval{
		ToolCallID: "call-apply", ActionVersion: 1, Decision: agentruntime.ToolApprovalApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, requested.State, approved, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	started, err := agentruntime.BeginToolExecution(approved.State, agentruntime.ToolExecution{
		ToolCallID: "call-apply", ActionVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, approved.State, started, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	record, err := repo.AgentToolCallForScope(scope, "call-apply", 1)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != agentruntime.ToolCallRunning || record.StartedAt == nil {
		t.Fatalf("started tool record = %#v", record)
	}
	loaded, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.PendingToolStarted || loaded.StateVersion != started.State.StateVersion || loaded.StepNumber != approved.State.StepNumber {
		t.Fatalf("started checkpoint = %#v", loaded)
	}
}

func TestCommitAgentRuntimeTransitionPersistsApprovalDecisionAtomically(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-1", ModelKey: "gpt-5.5", MaxSteps: 4,
		ToolSchemaVersion: 1, UserMessage: "生成一张图片", Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	current, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	requested, err := agentruntime.Advance(current, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "call-generate", ToolName: agentruntime.ToolGenerationSubmit,
			ActionVersion: 1, Arguments: []byte(`{"prompt":"test"}`),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, current, requested, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	approved, err := agentruntime.ReviewToolApproval(requested.State, agentruntime.ToolApproval{
		ToolCallID: "call-generate", ActionVersion: 1, Decision: agentruntime.ToolApprovalApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, requested.State, approved, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	var call model.AgentToolCall
	if err := db.First(&call, "run_id = ? AND tool_call_id = ?", scope.RunID, "call-generate").Error; err != nil {
		t.Fatal(err)
	}
	if call.Status != agentruntime.ToolCallPending || call.ApprovalDecision != agentruntime.ToolApprovalApproved || call.ApprovalByUserID != scope.ActorUserID || call.ApprovalDecidedAt == nil {
		t.Fatalf("approved call = %#v", call)
	}
}

func TestCommitAgentRuntimeTransitionPersistsRunEventsAndCheckpointAtomically(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).Updates(model.AgentRun{MaxSteps: 3, ModelRecordID: "model-1", ModelKey: "gpt", ToolSchemaVersion: 1}).Error; err != nil {
		t.Fatal(err)
	}
	previous := agentruntime.RuntimeState{StateVersion: 1, StepNumber: 0, MaxSteps: 3, Status: agentruntime.RunQueued, UserMessage: "test"}
	state := agentruntime.RuntimeState{StateVersion: 2, StepNumber: 1, MaxSteps: 3, Status: agentruntime.RunRunning, UserMessage: "test"}
	transition := agentruntime.RuntimeTransition{State: state, EventKinds: []agentruntime.EventKind{agentruntime.EventRunStatusChanged, agentruntime.EventCheckpointSaved}}
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).Update("state_version", 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, previous, transition, time.Now().UTC()); err != nil {
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
	previous := agentruntime.RuntimeState{StateVersion: 1, StepNumber: 0, MaxSteps: 3, Status: agentruntime.RunQueued, UserMessage: "test"}
	transition := agentruntime.RuntimeTransition{State: agentruntime.RuntimeState{StateVersion: 2, StepNumber: 1, MaxSteps: 3, Status: agentruntime.RunRunning, UserMessage: "test"}, EventKinds: []agentruntime.EventKind{agentruntime.EventRunStatusChanged}}
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).Update("state_version", 1).Error; err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errs <- repo.CommitAgentRuntimeTransition(scope, previous, transition, time.Now().UTC())
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
	terminalPrevious := agentruntime.RuntimeState{StateVersion: 3, StepNumber: 2, MaxSteps: 3, Status: agentruntime.RunRunning, UserMessage: "test"}
	terminalTransition := agentruntime.RuntimeTransition{State: agentruntime.RuntimeState{StateVersion: 4, StepNumber: 3, MaxSteps: 3, Status: agentruntime.RunFailed, UserMessage: "test"}, EventKinds: []agentruntime.EventKind{agentruntime.EventRunFailed}}
	if err := repo.CommitAgentRuntimeTransition(scope, terminalPrevious, terminalTransition, time.Now().UTC()); !errors.Is(err, ErrAgentRuntimeStepConflict) {
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
	previous := agentruntime.RuntimeState{StateVersion: 1, StepNumber: 0, MaxSteps: 3, Status: agentruntime.RunQueued, UserMessage: "test"}
	transition := agentruntime.RuntimeTransition{State: agentruntime.RuntimeState{StateVersion: 2, StepNumber: 1, MaxSteps: 3, Status: agentruntime.RunRunning, UserMessage: "test"}, EventKinds: []agentruntime.EventKind{agentruntime.EventRunStatusChanged}}
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).Update("state_version", 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, previous, transition, time.Now().UTC()); err != nil {
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
	previous := agentruntime.RuntimeState{StateVersion: 1, StepNumber: 0, MaxSteps: 3, Status: agentruntime.RunQueued, UserMessage: "test"}
	transition := agentruntime.RuntimeTransition{State: agentruntime.RuntimeState{StateVersion: 2, StepNumber: 1, MaxSteps: 3, Status: agentruntime.RunRunning, UserMessage: "test"}, EventKinds: []agentruntime.EventKind{agentruntime.EventRunStatusChanged}}
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).Update("state_version", 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, previous, transition, time.Now().UTC()); err == nil || err.Error() != "transition checkpoint failed" {
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
