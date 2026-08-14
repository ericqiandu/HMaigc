package agentruntime

import "errors"

const maxRuntimeSteps = 24

type RuntimeState struct {
	StateVersion     int                   `json:"stateVersion"`
	StepNumber       int                   `json:"stepNumber"`
	MaxSteps         int                   `json:"maxSteps"`
	Status           RunStatus             `json:"status"`
	ExpectedDelivery *ExpectedDelivery     `json:"expectedDelivery,omitempty"`
	Verification     *DeliveryVerification `json:"verification,omitempty"`
	PendingToolCall  *ToolCallDecision     `json:"pendingToolCall,omitempty"`
	FinalMessage     string                `json:"finalMessage,omitempty"`
	FailureCode      string                `json:"failureCode,omitempty"`
}

type RuntimeInput struct {
	Decision ModelDecision
	Evidence DeliveryEvidence
}

type RuntimeTransition struct {
	State      RuntimeState
	EventKinds []EventKind
}

func Advance(current RuntimeState, input RuntimeInput) (RuntimeTransition, error) {
	if err := validateAdvancingState(current); err != nil {
		return RuntimeTransition{}, err
	}
	if err := input.Decision.Validate(); err != nil {
		return RuntimeTransition{}, err
	}
	next := current
	next.StateVersion++
	next.StepNumber++
	next.PendingToolCall = nil
	next.Verification = nil
	next.FailureCode = ""

	if input.Decision.Kind == DecisionToolCall {
		next.Status = RunWaitingTool
		next.PendingToolCall = input.Decision.ToolCall
		return RuntimeTransition{State: next, EventKinds: []EventKind{EventToolCall, EventRunStatusChanged}}, nil
	}

	final := input.Decision.Final
	verification := VerifyDelivery(final.ExpectedDelivery, input.Evidence)
	next.ExpectedDelivery = &final.ExpectedDelivery
	next.Verification = &verification
	next.FinalMessage = final.Message
	switch verification.Status {
	case VerificationSatisfied:
		next.Status = RunSucceeded
		return RuntimeTransition{State: next, EventKinds: []EventKind{EventRunCompleted}}, nil
	case VerificationFailed:
		next.Status = RunFailed
		next.FailureCode = "delivery_contract_invalid"
		return RuntimeTransition{State: next, EventKinds: []EventKind{EventRunFailed}}, nil
	case VerificationRepairable:
		if next.StepNumber >= next.MaxSteps {
			next.Status = RunFailed
			next.FailureCode = "step_budget_exhausted"
			return RuntimeTransition{State: next, EventKinds: []EventKind{EventRunFailed}}, nil
		}
		next.Status = RunRunning
		return RuntimeTransition{State: next, EventKinds: []EventKind{EventRunStatusChanged}}, nil
	default:
		return RuntimeTransition{}, errors.New("delivery verification status is invalid")
	}
}

func validateAdvancingState(state RuntimeState) error {
	if state.StateVersion != state.StepNumber+1 || state.StepNumber < 0 || state.MaxSteps < 1 || state.MaxSteps > maxRuntimeSteps || state.StepNumber >= state.MaxSteps {
		return errors.New("agent runtime state boundary is invalid")
	}
	if state.Status != RunQueued && state.Status != RunRunning {
		return errors.New("agent runtime state is not advanceable")
	}
	return nil
}
