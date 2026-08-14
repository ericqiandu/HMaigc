package agentruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

const maxRuntimeSteps = 24

type RuntimeState struct {
	StateVersion     int                   `json:"stateVersion"`
	StepNumber       int                   `json:"stepNumber"`
	MaxSteps         int                   `json:"maxSteps"`
	Status           RunStatus             `json:"status"`
	ExpectedDelivery *ExpectedDelivery     `json:"expectedDelivery,omitempty"`
	Verification     *DeliveryVerification `json:"verification,omitempty"`
	PendingToolCall  *ToolCallDecision     `json:"pendingToolCall,omitempty"`
	LastToolResult   *ToolResult           `json:"lastToolResult,omitempty"`
	FinalMessage     string                `json:"finalMessage,omitempty"`
	FailureCode      string                `json:"failureCode,omitempty"`
	UserMessage      string                `json:"userMessage"`
}

type RuntimeInput struct {
	Decision ModelDecision
	Evidence DeliveryEvidence
}

type RuntimeTransition struct {
	State      RuntimeState
	EventKinds []EventKind
}

type ToolResolution struct {
	ToolCallID    string
	ActionVersion int
	Succeeded     bool
	Output        json.RawMessage
	ErrorCode     string
}

type ToolResult struct {
	ToolCallID    string          `json:"toolCallId"`
	ActionVersion int             `json:"actionVersion"`
	Succeeded     bool            `json:"succeeded"`
	Output        json.RawMessage `json:"output"`
	ErrorCode     string          `json:"errorCode,omitempty"`
}

type ToolApprovalDecision string

const (
	ToolApprovalApproved ToolApprovalDecision = "approved"
	ToolApprovalRejected ToolApprovalDecision = "rejected"
)

type ToolApproval struct {
	ToolCallID    string
	ActionVersion int
	Decision      ToolApprovalDecision
}

func Fail(current RuntimeState, failureCode string) (RuntimeTransition, error) {
	if err := validateAdvancingState(current); err != nil {
		return RuntimeTransition{}, err
	}
	failureCode = strings.TrimSpace(failureCode)
	if !validFailureCode(failureCode) {
		return RuntimeTransition{}, errors.New("agent runtime failure code is invalid")
	}
	next := current
	next.StateVersion++
	next.StepNumber++
	next.Status = RunFailed
	next.PendingToolCall = nil
	next.Verification = nil
	next.FailureCode = failureCode
	return RuntimeTransition{State: next, EventKinds: []EventKind{EventRunFailed}}, nil
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
	next.LastToolResult = nil
	next.Verification = nil
	next.FailureCode = ""

	if input.Decision.Kind == DecisionToolCall {
		policy, ok := ToolPolicyFor(input.Decision.ToolCall.ToolName)
		if !ok {
			return RuntimeTransition{}, errors.New("agent tool policy is unavailable")
		}
		next.Status = RunWaitingTool
		next.PendingToolCall = input.Decision.ToolCall
		if policy.ApprovalRequired {
			next.Status = RunWaitingApproval
			return RuntimeTransition{State: next, EventKinds: []EventKind{EventToolCall, EventApprovalRequired, EventRunStatusChanged}}, nil
		}
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

func ResolveTool(current RuntimeState, resolution ToolResolution) (RuntimeTransition, error) {
	if err := validateRuntimeState(current); err != nil {
		return RuntimeTransition{}, err
	}
	if current.Status != RunWaitingTool || current.PendingToolCall == nil {
		return RuntimeTransition{}, errors.New("agent runtime is not waiting for a tool result")
	}
	resolution.ToolCallID = strings.TrimSpace(resolution.ToolCallID)
	resolution.ErrorCode = strings.TrimSpace(resolution.ErrorCode)
	if resolution.ToolCallID != current.PendingToolCall.ToolCallID || resolution.ActionVersion != current.PendingToolCall.ActionVersion {
		return RuntimeTransition{}, errors.New("agent tool result identity is invalid")
	}
	output := bytes.TrimSpace(resolution.Output)
	if len(output) == 0 || len(output) > agentToolResultLimit || output[0] != '{' || !json.Valid(output) {
		return RuntimeTransition{}, errors.New("agent tool result output is invalid")
	}
	if resolution.Succeeded && resolution.ErrorCode != "" {
		return RuntimeTransition{}, errors.New("successful agent tool result cannot have an error code")
	}
	if !resolution.Succeeded && !validFailureCode(resolution.ErrorCode) {
		return RuntimeTransition{}, errors.New("failed agent tool result requires an error code")
	}
	next := current
	next.StateVersion++
	next.Status = RunRunning
	next.PendingToolCall = nil
	next.LastToolResult = &ToolResult{
		ToolCallID: resolution.ToolCallID, ActionVersion: resolution.ActionVersion,
		Succeeded: resolution.Succeeded, Output: append(json.RawMessage(nil), output...), ErrorCode: resolution.ErrorCode,
	}
	return RuntimeTransition{State: next, EventKinds: []EventKind{EventToolResult, EventRunStatusChanged}}, nil
}

func ReviewToolApproval(current RuntimeState, approval ToolApproval) (RuntimeTransition, error) {
	if err := validateRuntimeState(current); err != nil {
		return RuntimeTransition{}, err
	}
	if current.Status != RunWaitingApproval || current.PendingToolCall == nil {
		return RuntimeTransition{}, errors.New("agent runtime is not waiting for tool approval")
	}
	approval.ToolCallID = strings.TrimSpace(approval.ToolCallID)
	if approval.ToolCallID != current.PendingToolCall.ToolCallID || approval.ActionVersion != current.PendingToolCall.ActionVersion {
		return RuntimeTransition{}, errors.New("agent tool approval identity is invalid")
	}
	next := current
	next.StateVersion++
	switch approval.Decision {
	case ToolApprovalApproved:
		next.Status = RunWaitingTool
		return RuntimeTransition{State: next, EventKinds: []EventKind{EventApprovalDecided, EventRunStatusChanged}}, nil
	case ToolApprovalRejected:
		next.Status = RunRunning
		next.PendingToolCall = nil
		next.LastToolResult = &ToolResult{
			ToolCallID: approval.ToolCallID, ActionVersion: approval.ActionVersion,
			Succeeded: false, Output: json.RawMessage(`{}`), ErrorCode: "tool_approval_rejected",
		}
	default:
		return RuntimeTransition{}, errors.New("agent tool approval decision is invalid")
	}
	return RuntimeTransition{State: next, EventKinds: []EventKind{EventApprovalDecided, EventToolResult, EventRunStatusChanged}}, nil
}

func validateAdvancingState(state RuntimeState) error {
	if err := validateRuntimeState(state); err != nil {
		return err
	}
	if state.StepNumber >= state.MaxSteps {
		return errors.New("agent runtime state boundary is invalid")
	}
	if strings.TrimSpace(state.UserMessage) == "" || len(state.UserMessage) > 64*1024 {
		return errors.New("agent runtime user message is invalid")
	}
	if state.Status != RunQueued && state.Status != RunRunning {
		return errors.New("agent runtime state is not advanceable")
	}
	return nil
}

func validateRuntimeState(state RuntimeState) error {
	if state.StateVersion < 1 || state.StepNumber < 0 || state.MaxSteps < 1 || state.MaxSteps > maxRuntimeSteps || state.StepNumber > state.MaxSteps {
		return errors.New("agent runtime state boundary is invalid")
	}
	if strings.TrimSpace(state.UserMessage) == "" || len(state.UserMessage) > 64*1024 {
		return errors.New("agent runtime user message is invalid")
	}
	return nil
}

func validFailureCode(value string) bool {
	if value == "" || len(value) > 80 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}
