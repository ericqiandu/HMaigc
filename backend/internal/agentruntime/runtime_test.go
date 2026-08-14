package agentruntime_test

import (
	"encoding/json"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
)

func TestAdvanceRuntimeTransitionsFromFacts(t *testing.T) {
	answer := agentruntime.ExpectedDelivery{Kind: agentruntime.DeliveryAnswer, CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}}}
	base := agentruntime.RuntimeState{StateVersion: 1, StepNumber: 0, MaxSteps: 3, Status: agentruntime.RunQueued, UserMessage: "请读取画布"}

	final := agentruntime.ModelDecision{Kind: agentruntime.DecisionFinal, Final: &agentruntime.FinalDecision{Message: "完成", ExpectedDelivery: answer}}
	transition, err := agentruntime.Advance(base, agentruntime.RuntimeInput{Decision: final, Evidence: agentruntime.DeliveryEvidence{FinalMessage: "完成"}})
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.Status != agentruntime.RunSucceeded || transition.State.StepNumber != 1 || transition.State.StateVersion != 2 {
		t.Fatalf("satisfied transition = %#v", transition)
	}

	repairable, err := agentruntime.Advance(base, agentruntime.RuntimeInput{Decision: final})
	if err != nil {
		t.Fatal(err)
	}
	if repairable.State.Status != agentruntime.RunRunning || repairable.State.Verification == nil || repairable.State.Verification.Status != agentruntime.VerificationRepairable {
		t.Fatalf("repairable transition = %#v", repairable)
	}

	tool := agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{ToolCallID: "call-1", ToolName: agentruntime.ToolCanvasReadState, ActionVersion: 1, Arguments: []byte(`{}`)}}
	waiting, err := agentruntime.Advance(base, agentruntime.RuntimeInput{Decision: tool})
	if err != nil {
		t.Fatal(err)
	}
	if waiting.State.Status != agentruntime.RunWaitingTool || waiting.State.PendingToolCall == nil {
		t.Fatalf("tool transition = %#v", waiting)
	}

	writeTool := agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{ToolCallID: "call-2", ToolName: agentruntime.ToolCanvasApplyOps, ActionVersion: 1, Arguments: []byte(`{"baseRevision":3,"ops":[]}`)}}
	waitingApproval, err := agentruntime.Advance(base, agentruntime.RuntimeInput{Decision: writeTool})
	if err != nil {
		t.Fatal(err)
	}
	if waitingApproval.State.Status != agentruntime.RunWaitingApproval || waitingApproval.State.PendingToolCall == nil {
		t.Fatalf("approval transition = %#v", waitingApproval)
	}
}

func TestResolveToolPreservesModelStepAndAdvancesStateVersion(t *testing.T) {
	current := agentruntime.RuntimeState{
		StateVersion: 2, StepNumber: 1, MaxSteps: 4, Status: agentruntime.RunWaitingTool,
		UserMessage: "读取当前画布",
		PendingToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "call-1", ToolName: agentruntime.ToolCanvasReadState,
			ActionVersion: 1, Arguments: []byte(`{}`),
		},
	}
	transition, err := agentruntime.ResolveTool(current, agentruntime.ToolResolution{
		ToolCallID: "call-1", ActionVersion: 1, Succeeded: true,
		Output: []byte(`{"canvasId":"canvas-1","revision":7}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.Status != agentruntime.RunRunning || transition.State.StepNumber != 1 || transition.State.StateVersion != 3 {
		t.Fatalf("resolved state = %#v", transition.State)
	}
	if transition.State.PendingToolCall != nil || transition.State.LastToolResult == nil || !transition.State.LastToolResult.Succeeded {
		t.Fatalf("resolved tool facts = %#v", transition.State)
	}
}

func TestBeginToolExecutionPersistsRunningInterruptionWithoutConsumingModelStep(t *testing.T) {
	current := agentruntime.RuntimeState{
		StateVersion: 3, StepNumber: 1, MaxSteps: 4, Status: agentruntime.RunWaitingTool,
		UserMessage: "在画布中增加标题节点",
		PendingToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "call-apply", ToolName: agentruntime.ToolCanvasApplyOps,
			ActionVersion: 2, Arguments: json.RawMessage(`{"baseRevision":7,"patch":{"upsertNodes":[{"id":"title-node"}]}}`),
		},
	}

	transition, err := agentruntime.BeginToolExecution(current, agentruntime.ToolExecution{
		ToolCallID: "call-apply", ActionVersion: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.StateVersion != 4 || transition.State.StepNumber != 1 ||
		transition.State.Status != agentruntime.RunWaitingTool || !transition.State.PendingToolStarted {
		t.Fatalf("started state = %#v", transition.State)
	}
	if len(transition.EventKinds) != 1 || transition.EventKinds[0] != agentruntime.EventToolStarted {
		t.Fatalf("started events = %#v", transition.EventKinds)
	}
	if _, err := agentruntime.BeginToolExecution(transition.State, agentruntime.ToolExecution{
		ToolCallID: "call-apply", ActionVersion: 2,
	}); err == nil {
		t.Fatal("already started tool execution was accepted as a new transition")
	}
}

func TestReviewToolApprovalMatchesFrozenAction(t *testing.T) {
	current := agentruntime.RuntimeState{
		StateVersion: 2, StepNumber: 1, MaxSteps: 4, Status: agentruntime.RunWaitingApproval,
		UserMessage: "生成一张图片",
		PendingToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "call-generate", ToolName: agentruntime.ToolGenerationSubmit,
			ActionVersion: 2, Arguments: []byte(`{"model":"image"}`),
		},
	}
	approved, err := agentruntime.ReviewToolApproval(current, agentruntime.ToolApproval{
		ToolCallID: "call-generate", ActionVersion: 2, Decision: agentruntime.ToolApprovalApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if approved.State.Status != agentruntime.RunWaitingTool || approved.State.StateVersion != 3 || approved.State.StepNumber != 1 || approved.State.PendingToolCall == nil {
		t.Fatalf("approved transition = %#v", approved)
	}
	rejected, err := agentruntime.ReviewToolApproval(current, agentruntime.ToolApproval{
		ToolCallID: "call-generate", ActionVersion: 2, Decision: agentruntime.ToolApprovalRejected,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rejected.State.Status != agentruntime.RunRunning || rejected.State.PendingToolCall != nil || rejected.State.LastToolResult == nil || rejected.State.LastToolResult.ErrorCode != "tool_approval_rejected" {
		t.Fatalf("rejected transition = %#v", rejected)
	}
	if _, err := agentruntime.ReviewToolApproval(current, agentruntime.ToolApproval{
		ToolCallID: "another-call", ActionVersion: 2, Decision: agentruntime.ToolApprovalApproved,
	}); err == nil {
		t.Fatal("mismatched approval identity was accepted")
	}
}

func TestAdvanceRuntimeFailsClosedAtBoundaries(t *testing.T) {
	answer := agentruntime.ExpectedDelivery{Kind: agentruntime.DeliveryAnswer, CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}}}
	final := agentruntime.ModelDecision{Kind: agentruntime.DecisionFinal, Final: &agentruntime.FinalDecision{Message: "未完成", ExpectedDelivery: answer}}

	exhausted := agentruntime.RuntimeState{StateVersion: 3, StepNumber: 2, MaxSteps: 3, Status: agentruntime.RunRunning, UserMessage: "请读取画布"}
	transition, err := agentruntime.Advance(exhausted, agentruntime.RuntimeInput{Decision: final})
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.Status != agentruntime.RunFailed || transition.State.FailureCode != "step_budget_exhausted" {
		t.Fatalf("exhausted transition = %#v", transition)
	}

	invalid := []agentruntime.RuntimeState{
		{StateVersion: 0, MaxSteps: 3, Status: agentruntime.RunQueued},
		{StateVersion: 1, MaxSteps: 0, Status: agentruntime.RunQueued},
		{StateVersion: 1, MaxSteps: 25, Status: agentruntime.RunQueued},
		{StateVersion: 1, MaxSteps: 3, Status: agentruntime.RunSucceeded, UserMessage: "请读取画布"},
		{StateVersion: 1, StepNumber: 4, MaxSteps: 3, Status: agentruntime.RunRunning, UserMessage: "请读取画布"},
	}
	for _, state := range invalid {
		if _, err := agentruntime.Advance(state, agentruntime.RuntimeInput{Decision: final}); err == nil {
			t.Fatalf("invalid state accepted: %#v", state)
		}
	}
}

func TestFailRuntimeConsumesCurrentStepAndTerminates(t *testing.T) {
	base := agentruntime.RuntimeState{StateVersion: 1, StepNumber: 0, MaxSteps: 3, Status: agentruntime.RunQueued, UserMessage: "请读取画布"}
	transition, err := agentruntime.Fail(base, "model_task_failed")
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.Status != agentruntime.RunFailed || transition.State.StepNumber != 1 || transition.State.StateVersion != 2 || transition.State.FailureCode != "model_task_failed" {
		t.Fatalf("failure transition = %#v", transition)
	}
	if len(transition.EventKinds) != 1 || transition.EventKinds[0] != agentruntime.EventRunFailed {
		t.Fatalf("failure events = %#v", transition.EventKinds)
	}
	if _, err := agentruntime.Fail(base, "invalid code"); err == nil {
		t.Fatal("invalid failure code was accepted")
	}
}
