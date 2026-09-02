package agentruntime_test

import (
	"encoding/json"
	"errors"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
)

func externalRuntimeState() agentruntime.RuntimeState {
	return agentruntime.RuntimeState{
		StateVersion: 1,
		StepNumber:   0,
		MaxSteps:     4,
		Status:       agentruntime.RunRunning,
		UserMessage:  "读取当前画布",
		Configuration: agentruntime.RunConfiguration{
			ExecutionMode: agentruntime.ExecutionGuided,
		},
	}
}

func answerDelivery() agentruntime.ExpectedDelivery {
	return agentruntime.ExpectedDelivery{
		Kind: agentruntime.DeliveryAnswer,
		CompletionCriteria: []agentruntime.DeliveryCriterion{
			{Fact: agentruntime.DeliveryFactFinalMessage},
		},
	}
}

func canvasReadDecision() agentruntime.ModelDecision {
	return agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall,
		ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID:       "call-read-1",
			ToolName:         agentruntime.ToolCanvasRead,
			ActionVersion:    1,
			Arguments:        json.RawMessage(`{"canvasId":"canvas-1","selectedNodeIds":[],"includeViewport":true}`),
			ExpectedDelivery: answerDelivery(),
		},
	}
}

func TestReasoningHostAcceptsOnlyManagedAndLocalCodex(t *testing.T) {
	for _, host := range []agentruntime.ReasoningHost{
		agentruntime.ReasoningHostManaged,
		agentruntime.ReasoningHostLocalCodex,
	} {
		if !host.Valid() {
			t.Fatalf("expected reasoning host %q to be valid", host)
		}
	}
	for _, host := range []agentruntime.ReasoningHost{"", "local", "managed_fallback"} {
		if host.Valid() {
			t.Fatalf("expected reasoning host %q to be rejected", host)
		}
	}
}

func TestAdvanceExternalDecisionUsesCurrentToolSchema(t *testing.T) {
	transition, err := agentruntime.AdvanceExternalDecision(externalRuntimeState(), agentruntime.ExternalDecisionInput{
		ExpectedStateVersion: 1,
		Decision:             canvasReadDecision(),
	})
	if err != nil {
		t.Fatalf("advance current external decision: %v", err)
	}
	if transition.State.Status != agentruntime.RunWaitingTool || transition.State.PendingToolCall == nil {
		t.Fatalf("current tool decision did not enter waiting_tool: %#v", transition.State)
	}

	retired := canvasReadDecision()
	retired.ToolCall.ToolName = agentruntime.ToolCanvasProject
	if _, err := agentruntime.AdvanceExternalDecision(externalRuntimeState(), agentruntime.ExternalDecisionInput{
		ExpectedStateVersion: 1,
		Decision:             retired,
	}); err == nil {
		t.Fatal("retired tool schema decision was accepted")
	}
}

func TestAdvanceExternalDecisionRejectsWrongStateVersionAndNonRunningStatus(t *testing.T) {
	if _, err := agentruntime.AdvanceExternalDecision(externalRuntimeState(), agentruntime.ExternalDecisionInput{
		ExpectedStateVersion: 2,
		Decision:             canvasReadDecision(),
	}); !errors.Is(err, agentruntime.ErrExternalDecisionConflict) {
		t.Fatalf("wrong state version error = %v", err)
	}

	queued := externalRuntimeState()
	queued.Status = agentruntime.RunQueued
	if _, err := agentruntime.AdvanceExternalDecision(queued, agentruntime.ExternalDecisionInput{
		ExpectedStateVersion: 1,
		Decision:             canvasReadDecision(),
	}); !errors.Is(err, agentruntime.ErrExternalDecisionConflict) {
		t.Fatalf("non-running external state error = %v", err)
	}
}

func TestAdvanceExternalFinalRequiresDeliveryVerification(t *testing.T) {
	final := agentruntime.ModelDecision{
		Kind: agentruntime.DecisionFinal,
		Final: &agentruntime.FinalDecision{
			Message:          "读取完成",
			ExpectedDelivery: answerDelivery(),
		},
	}

	repair, err := agentruntime.AdvanceExternalDecision(externalRuntimeState(), agentruntime.ExternalDecisionInput{
		ExpectedStateVersion: 1,
		Decision:             final,
	})
	if err != nil {
		t.Fatalf("advance external final without evidence: %v", err)
	}
	if repair.State.Status != agentruntime.RunRunning || repair.State.Verification == nil || repair.State.Verification.Status != agentruntime.VerificationRepairable {
		t.Fatalf("external final bypassed delivery verification: %#v", repair.State)
	}

	completed, err := agentruntime.AdvanceExternalDecision(externalRuntimeState(), agentruntime.ExternalDecisionInput{
		ExpectedStateVersion: 1,
		Decision:             final,
		Evidence:             agentruntime.DeliveryEvidence{FinalMessage: "读取完成"},
	})
	if err != nil {
		t.Fatalf("advance external final with evidence: %v", err)
	}
	if completed.State.Status != agentruntime.RunSucceeded || completed.State.Verification == nil || completed.State.Verification.Status != agentruntime.VerificationSatisfied {
		t.Fatalf("verified external final was not completed: %#v", completed.State)
	}
}
