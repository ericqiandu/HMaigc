package repository

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func TestCommitRejectedAgentToolDecisionPersistsOneFailedCallAndCheckpoint(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLiteFile(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-1", ModelKey: "gpt-5.5", MaxSteps: 4,
		ToolSchemaVersion: 1, RuntimeVersion: 1, PolicyVersion: 1, UserMessage: "生成视频",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic}, Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	current, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	call := agentruntime.ToolCallDecision{
		ToolCallID: "render-invalid", ToolName: agentruntime.ToolProductionRender, ActionVersion: 1,
		Arguments: json.RawMessage(`{"artifactId":"artifact-video"}`), ExpectedDelivery: repositoryTestImageDelivery(),
	}
	transition, err := agentruntime.RejectToolDecision(current, agentruntime.ToolDecisionFailure{
		Call: call, Class: agentruntime.ToolFailureAgentRepairable,
		ErrorCode: "generation_parameter_unsupported", Output: json.RawMessage(`{"reason":"duration is unsupported"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, current, transition, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	record, err := repo.AgentToolCallForScope(scope, call.ToolCallID, call.ActionVersion)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != agentruntime.ToolCallFailed || record.ToolName != string(call.ToolName) ||
		record.InputJSON != string(call.Arguments) || record.OutputJSON != `{"reason":"duration is unsupported"}` ||
		record.ErrorCode != "generation_parameter_unsupported" || !record.ApprovalRequired || record.ApprovalDecision != "" {
		t.Fatalf("rejected tool record = %#v", record)
	}
	checkpoint, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.LastToolResult == nil || checkpoint.LastToolResult.ToolCallID != call.ToolCallID ||
		checkpoint.LastToolResult.ErrorCode != record.ErrorCode || string(checkpoint.LastToolResult.Output) != record.OutputJSON {
		t.Fatalf("rejected tool checkpoint = %#v", checkpoint)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, current, transition, time.Now().UTC()); !errors.Is(err, ErrAgentRuntimeStepConflict) {
		t.Fatalf("replayed rejected transition error = %v", err)
	}
	var count int64
	if err := db.Model(&model.AgentToolCall{}).Where("run_id = ? AND tool_call_id = ?", scope.RunID, call.ToolCallID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("rejected tool call count = %d, want 1", count)
	}
}

func TestInitializeAgentRunFreezesModelAndCreatesCheckpointOnce(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLiteFile(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC()
	input := InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-record-1", ModelKey: "gpt-5.5",
		MaxSteps: 4, ToolSchemaVersion: 1, RuntimeVersion: 1, PolicyVersion: 1, UserMessage: "请根据当前画布继续完成任务",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: now,
	}
	initialized, err := repo.InitializeAgentRun(input)
	if err != nil {
		t.Fatal(err)
	}
	if !initialized.Created || initialized.Run.StateVersion != 1 || initialized.Run.ModelRecordID != input.ModelRecordID || initialized.Run.ModelKey != input.ModelKey || initialized.Run.MaxSteps != input.MaxSteps || initialized.Run.RuntimeVersion != 1 || initialized.Run.PolicyVersion != 1 {
		t.Fatalf("initialized run = %#v", initialized)
	}
	loaded, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.StateVersion != 1 || loaded.StepNumber != 0 || loaded.Status != agentruntime.RunQueued || loaded.UserMessage != input.UserMessage {
		t.Fatalf("initial checkpoint = %#v", loaded)
	}
	if loaded.ClarificationHistory == nil {
		t.Fatal("clarification history must serialize as an explicit empty array")
	}
	history, err := repo.AgentThreadHistory(scope, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Run == nil || history[0].Run.RuntimeVersion != input.RuntimeVersion || history[0].Run.PolicyVersion != input.PolicyVersion {
		t.Fatalf("thread history did not preserve runtime policy versions: %#v", history)
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
		ToolSchemaVersion: 1, RuntimeVersion: 1, PolicyVersion: 1, UserMessage: "读取当前画布",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic}, Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	current, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	decision := agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{
		ToolCallID: "call-read-state", ToolName: agentruntime.ToolCanvasCommit,
		ActionVersion: 1, Arguments: []byte(`{"expectedRevision":7}`), ExpectedDelivery: repositoryTestAnswerDelivery(),
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
	if call.Status != agentruntime.ToolCallPending || call.RiskLevel != agentruntime.ToolRiskWrite || call.RequiredAccess != agentruntime.AccessEditor || call.ApprovalRequired || call.IdempotencyKey != scope.RunID+":call-read-state:1" || call.InputJSON != `{"expectedRevision":7}` {
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
		ToolSchemaVersion: 1, RuntimeVersion: 1, PolicyVersion: 1, UserMessage: "修改当前画布",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic}, Now: time.Now().UTC(),
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
			ToolCallID: "call-apply", ToolName: agentruntime.ToolCanvasCommit, ActionVersion: 1,
			Arguments: []byte(`{"baseRevision":7,"patch":{"deleteNodeIds":["obsolete"]}}`), ExpectedDelivery: repositoryTestCanvasDelivery(),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, current, requested, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	started, err := agentruntime.BeginToolExecution(requested.State, agentruntime.ToolExecution{
		ToolCallID: "call-apply", ActionVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, requested.State, started, time.Now().UTC()); err != nil {
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
	if !loaded.PendingToolStarted || loaded.StateVersion != started.State.StateVersion || loaded.StepNumber != requested.State.StepNumber {
		t.Fatalf("started checkpoint = %#v", loaded)
	}
}

func TestCommitAgentRuntimeTransitionPersistsApprovalDecisionAtomically(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-1", ModelKey: "gpt-5.5", MaxSteps: 4,
		ToolSchemaVersion: 1, RuntimeVersion: 1, PolicyVersion: 1, UserMessage: "生成一张图片",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	current, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "approval-plan", BaseVersion: 0,
		Draft: agentruntime.ProductionPlanDraft{
			Title: "审批计划", TargetDurationMS: 1_000, Script: "镜头",
			Shots: []agentruntime.ShotPlanDraft{{ShotKey: "shot-1", Order: 1, DurationMS: 1_000, ScriptText: "镜头", ImagePrompt: "画面", VideoPrompt: "动作", Dependencies: []string{}}},
		},
		Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact := firstProductionArtifact(t, plan.Artifacts, "shot-1", model.AgentProductionArtifactStoryboardImage)
	rawArguments, err := json.Marshal(agentruntime.ProductionRenderArguments{
		PlanKey: plan.Plan.PlanKey, PlanVersion: plan.Plan.Version, ArtifactID: artifact.ID, Attempt: artifact.Attempt,
		GenerationModel: agentruntime.GenerationModelSelection{ChannelID: "image-channel", Model: "image-model"},
		ImageConfig:     &agentruntime.ImageRenderConfig{Size: "1:1", Quality: "medium", Count: 1},
		FrozenRenderQuote: agentruntime.FrozenRenderQuote{
			AmountMicrocredits: 100, PerTaskAmountMicrocredits: 100, PriceVersion: 1,
			BillingMode: "fixed_request", Quantity: 1, QuoteFingerprint: "approval-quote",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	requested, err := agentruntime.Advance(current, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "call-generate", ToolName: agentruntime.ToolProductionRender,
			ActionVersion: 1, Arguments: rawArguments, ExpectedDelivery: repositoryTestImageDelivery(),
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

func TestCommitProductionRetryApprovalClearsRefundedAttemptBindingsAtomically(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	if err := db.AutoMigrate(&model.Task{}, &model.BillingOrder{}); err != nil {
		t.Fatal(err)
	}
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC()
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-1", ModelKey: "gpt-5.5", MaxSteps: 4,
		ToolSchemaVersion: 1, RuntimeVersion: 1, PolicyVersion: 1, UserMessage: "重试失败镜头",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	current, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "refunded-retry-plan", BaseVersion: 0,
		Draft: agentruntime.ProductionPlanDraft{
			Title: "退款后重试", TargetDurationMS: 1_000, Script: "镜头",
			Shots: []agentruntime.ShotPlanDraft{{ShotKey: "shot-1", Order: 1, DurationMS: 1_000, ScriptText: "镜头", ImagePrompt: "画面", VideoPrompt: "动作", Dependencies: []string{}}},
		},
		Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact := firstProductionArtifact(t, plan.Artifacts, "shot-1", model.AgentProductionArtifactStoryboardImage)
	task := model.Task{
		ID: "refunded-retry-task", UserID: scope.ActorUserID, ProjectID: scope.CanvasID,
		Type: "canvas_image", Capability: "image", Status: model.TaskStatusFailed,
		BillingOrderID: "refunded-retry-order", CreatedAt: now, UpdatedAt: now,
	}
	order := model.BillingOrder{
		ID: task.BillingOrderID, UserID: scope.ActorUserID, TaskID: task.ID,
		IdempotencyKey: "refunded-retry-order-key", Status: model.BillingStatusRefunded,
		AmountMicrocredits: 100, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentProductionArtifact{}).Where("id = ?", artifact.ID).
		Updates(map[string]any{
			"status": model.AgentProductionArtifactFailed, "attempt": 1,
			"task_id": task.ID, "billing_order_id": order.ID, "last_error_code": "production_generation_failed",
		}).Error; err != nil {
		t.Fatal(err)
	}
	rawArguments, err := json.Marshal(agentruntime.ProductionRenderArguments{
		PlanKey: plan.Plan.PlanKey, PlanVersion: plan.Plan.Version, ArtifactID: artifact.ID, Attempt: 1,
		GenerationModel: agentruntime.GenerationModelSelection{ChannelID: "image-channel", Model: "image-model"},
		ImageConfig:     &agentruntime.ImageRenderConfig{Size: "1:1", Quality: "medium", Count: 1},
		FrozenRenderQuote: agentruntime.FrozenRenderQuote{
			AmountMicrocredits: 100, PerTaskAmountMicrocredits: 100, PriceVersion: 1,
			BillingMode: "fixed_request", Quantity: 1, QuoteFingerprint: "refunded-retry-quote",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	requested, err := agentruntime.Advance(current, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "call-refunded-retry", ToolName: agentruntime.ToolProductionRender,
			ActionVersion: 1, Arguments: rawArguments, ExpectedDelivery: repositoryTestImageDelivery(),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, current, requested, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var stored model.AgentProductionArtifact
	if err := db.First(&stored, "id = ?", artifact.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.AgentProductionArtifactAwaitingApproval || stored.Attempt != 1 || stored.TaskID != "" || stored.BillingOrderID != "" || stored.ResourceID != "" {
		t.Fatalf("prepared retry artifact = %#v", stored)
	}
	var oldTask model.Task
	var oldOrder model.BillingOrder
	if err := db.First(&oldTask, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&oldOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if oldTask.Status != model.TaskStatusFailed || oldOrder.Status != model.BillingStatusRefunded {
		t.Fatalf("historical commercial facts changed: task=%s billing=%s", oldTask.Status, oldOrder.Status)
	}
}

func repositoryTestAnswerDelivery() agentruntime.ExpectedDelivery {
	return agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryAnswer,
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}},
	}
}

func repositoryTestCanvasDelivery() agentruntime.ExpectedDelivery {
	return agentruntime.ExpectedDelivery{
		Kind: agentruntime.DeliveryCanvasChange, TargetCanvasID: "agent-canvas-1",
		RequiredArtifacts:  []agentruntime.ArtifactKind{agentruntime.ArtifactCanvasRevision},
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactCanvasRevision}},
	}
}

func repositoryTestImageDelivery() agentruntime.ExpectedDelivery {
	return agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryGeneratedAsset,
		RequiredArtifacts:  []agentruntime.ArtifactKind{agentruntime.ArtifactImage},
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactArtifact, Artifact: agentruntime.ArtifactImage}},
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
