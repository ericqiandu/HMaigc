package agentruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

type ToolFailureClass string

const (
	ToolFailureAgentRepairable ToolFailureClass = "agent_repairable"
	ToolFailureTerminal        ToolFailureClass = "terminal"
)

type ToolDecisionFailure struct {
	Call      ToolCallDecision
	Class     ToolFailureClass
	ErrorCode string
	Output    json.RawMessage
}

// RejectToolDecision records a model-selected tool and its deterministic
// preparation failure as one auditable transition. Transient infrastructure
// failures are deliberately excluded because their task owner controls retry.
func RejectToolDecision(current RuntimeState, failure ToolDecisionFailure) (RuntimeTransition, error) {
	if err := validateAdvancingState(current); err != nil {
		return RuntimeTransition{}, err
	}
	decision := ModelDecision{Kind: DecisionToolCall, ToolCall: &failure.Call}
	if err := decision.Validate(); err != nil || !failure.Call.ToolName.Valid() {
		return RuntimeTransition{}, errors.New("rejected agent tool decision is invalid")
	}
	failure.ErrorCode = strings.TrimSpace(failure.ErrorCode)
	output := bytes.TrimSpace(failure.Output)
	if !validFailureCode(failure.ErrorCode) || len(output) == 0 || len(output) > agentToolResultLimit || output[0] != '{' || !json.Valid(output) {
		return RuntimeTransition{}, errors.New("rejected agent tool failure is invalid")
	}
	if failure.Class != ToolFailureAgentRepairable && failure.Class != ToolFailureTerminal {
		return RuntimeTransition{}, errors.New("rejected agent tool failure class is invalid")
	}
	if current.ExpectedDelivery != nil && !current.ExpectedDelivery.Equal(failure.Call.ExpectedDelivery) {
		return RuntimeTransition{}, errors.New("rejected agent tool delivery contract conflicts with frozen facts")
	}

	call := failure.Call
	call.Arguments = append(json.RawMessage(nil), failure.Call.Arguments...)
	next := current
	next.StateVersion++
	next.StepNumber++
	next.Status = RunRunning
	next.PendingToolCall = nil
	next.PendingToolStarted = false
	next.DecisionFeedback = nil
	next.Verification = nil
	next.FinalMessage = ""
	next.FailureCode = ""
	if next.ExpectedDelivery == nil {
		frozen := call.ExpectedDelivery
		next.ExpectedDelivery = &frozen
	}
	next.LastToolResult = &ToolResult{
		ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion,
		Succeeded: false, Output: append(json.RawMessage(nil), output...), ErrorCode: failure.ErrorCode,
	}
	transition := RuntimeTransition{
		State: next, RejectedToolCall: &call,
		EventKinds: []EventKind{EventToolResult, EventRunStatusChanged},
	}
	if failure.Class == ToolFailureTerminal {
		transition.State.Status = RunFailed
		transition.State.FailureCode = failure.ErrorCode
		transition.EventKinds[1] = EventRunFailed
		return transition, nil
	}
	if transition.State.StepNumber >= transition.State.MaxSteps {
		transition.State.Status = RunFailed
		transition.State.FailureCode = "step_budget_exhausted"
		transition.EventKinds[1] = EventRunFailed
	}
	return transition, nil
}
