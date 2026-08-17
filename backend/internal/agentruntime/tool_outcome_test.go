package agentruntime_test

import (
	"encoding/json"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
)

func TestRejectToolDecisionReturnsRepairableFailureToSameRun(t *testing.T) {
	current := repairableToolDecisionRuntimeState()
	call := agentruntime.ToolCallDecision{
		ToolCallID: "render-unsupported", ToolName: agentruntime.ToolProductionRender, ActionVersion: 1,
		Arguments: json.RawMessage(`{"artifactId":"artifact-video"}`),
		ExpectedDelivery: agentruntime.ExpectedDelivery{
			Kind:               agentruntime.DeliveryAnswer,
			CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}},
		},
	}
	transition, err := agentruntime.RejectToolDecision(current, agentruntime.ToolDecisionFailure{
		Call: call, Class: agentruntime.ToolFailureAgentRepairable,
		ErrorCode: "generation_parameter_unsupported",
		Output:    json.RawMessage(`{"reason":"720p is unavailable"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.Status != agentruntime.RunRunning || transition.State.StateVersion != current.StateVersion+1 || transition.State.StepNumber != current.StepNumber+1 {
		t.Fatalf("repairable transition state = %#v", transition.State)
	}
	if transition.State.LastToolResult == nil || transition.State.LastToolResult.Succeeded ||
		transition.State.LastToolResult.ErrorCode != "generation_parameter_unsupported" ||
		string(transition.State.LastToolResult.Output) != `{"reason":"720p is unavailable"}` {
		t.Fatalf("repairable tool result = %#v", transition.State.LastToolResult)
	}
	if transition.RejectedToolCall == nil || transition.RejectedToolCall.ToolCallID != call.ToolCallID || transition.RejectedToolCall.ToolName != call.ToolName {
		t.Fatalf("rejected tool call = %#v", transition.RejectedToolCall)
	}
	if len(transition.EventKinds) != 2 || transition.EventKinds[0] != agentruntime.EventToolResult || transition.EventKinds[1] != agentruntime.EventRunStatusChanged {
		t.Fatalf("repairable event kinds = %#v", transition.EventKinds)
	}
}

func TestRejectToolDecisionTerminatesExplicitInvariantFailure(t *testing.T) {
	current := repairableToolDecisionRuntimeState()
	call := agentruntime.ToolCallDecision{
		ToolCallID: "render-conflict", ToolName: agentruntime.ToolProductionRender, ActionVersion: 1,
		Arguments: json.RawMessage(`{"artifactId":"artifact-video"}`),
		ExpectedDelivery: agentruntime.ExpectedDelivery{
			Kind:               agentruntime.DeliveryAnswer,
			CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}},
		},
	}
	transition, err := agentruntime.RejectToolDecision(current, agentruntime.ToolDecisionFailure{
		Call: call, Class: agentruntime.ToolFailureTerminal,
		ErrorCode: "frozen_tool_facts_conflict", Output: json.RawMessage(`{"reason":"frozen facts conflict"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.Status != agentruntime.RunFailed || transition.State.FailureCode != "frozen_tool_facts_conflict" {
		t.Fatalf("terminal transition state = %#v", transition.State)
	}
	if len(transition.EventKinds) != 2 || transition.EventKinds[0] != agentruntime.EventToolResult || transition.EventKinds[1] != agentruntime.EventRunFailed {
		t.Fatalf("terminal event kinds = %#v", transition.EventKinds)
	}
}

func repairableToolDecisionRuntimeState() agentruntime.RuntimeState {
	return agentruntime.RuntimeState{
		StateVersion: 3, StepNumber: 1, MaxSteps: 8, Status: agentruntime.RunRunning,
		UserMessage: "生成视频", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic},
	}
}
