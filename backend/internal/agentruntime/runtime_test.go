package agentruntime_test

import (
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
)

func TestAdvanceRuntimeTransitionsFromFacts(t *testing.T) {
	answer := agentruntime.ExpectedDelivery{Kind: agentruntime.DeliveryAnswer, CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}}}
	base := agentruntime.RuntimeState{StateVersion: 1, StepNumber: 0, MaxSteps: 3, Status: agentruntime.RunQueued}

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
}

func TestAdvanceRuntimeFailsClosedAtBoundaries(t *testing.T) {
	answer := agentruntime.ExpectedDelivery{Kind: agentruntime.DeliveryAnswer, CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}}}
	final := agentruntime.ModelDecision{Kind: agentruntime.DecisionFinal, Final: &agentruntime.FinalDecision{Message: "未完成", ExpectedDelivery: answer}}

	exhausted := agentruntime.RuntimeState{StateVersion: 3, StepNumber: 2, MaxSteps: 3, Status: agentruntime.RunRunning}
	transition, err := agentruntime.Advance(exhausted, agentruntime.RuntimeInput{Decision: final})
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.Status != agentruntime.RunFailed || transition.State.FailureCode != "step_budget_exhausted" {
		t.Fatalf("exhausted transition = %#v", transition)
	}

	invalid := []agentruntime.RuntimeState{
		{StateVersion: 0, MaxSteps: 3, Status: agentruntime.RunQueued},
		{StateVersion: 2, StepNumber: 0, MaxSteps: 3, Status: agentruntime.RunQueued},
		{StateVersion: 1, MaxSteps: 0, Status: agentruntime.RunQueued},
		{StateVersion: 1, MaxSteps: 25, Status: agentruntime.RunQueued},
		{StateVersion: 1, MaxSteps: 3, Status: agentruntime.RunSucceeded},
		{StateVersion: 1, StepNumber: 4, MaxSteps: 3, Status: agentruntime.RunRunning},
	}
	for _, state := range invalid {
		if _, err := agentruntime.Advance(state, agentruntime.RuntimeInput{Decision: final}); err == nil {
			t.Fatalf("invalid state accepted: %#v", state)
		}
	}
}
