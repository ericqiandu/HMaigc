package agentruntime

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
)

const maxRuntimeSteps = 24

type RuntimeState struct {
	StateVersion         int                      `json:"stateVersion"`
	StepNumber           int                      `json:"stepNumber"`
	MaxSteps             int                      `json:"maxSteps"`
	Status               RunStatus                `json:"status"`
	ExpectedDelivery     *ExpectedDelivery        `json:"expectedDelivery,omitempty"`
	Verification         *DeliveryVerification    `json:"verification,omitempty"`
	PendingToolCall      *ToolCallDecision        `json:"pendingToolCall,omitempty"`
	PendingToolStarted   bool                     `json:"pendingToolStarted,omitempty"`
	PendingClarification *PendingClarification    `json:"pendingClarification,omitempty"`
	ClarificationHistory []CompletedClarification `json:"clarificationHistory"`
	LastToolResult       *ToolResult              `json:"lastToolResult,omitempty"`
	DecisionFeedback     *ModelDecisionFeedback   `json:"decisionFeedback,omitempty"`
	FinalMessage         string                   `json:"finalMessage,omitempty"`
	FailureCode          string                   `json:"failureCode,omitempty"`
	UserMessage          string                   `json:"userMessage"`
	Configuration        RunConfiguration         `json:"configuration"`
	LoadedSkillDirs      []string                 `json:"loadedSkillDirs,omitempty"`
	PendingSteers        []PendingSteer           `json:"pendingSteers,omitempty"`
}

type SteerRequest struct {
	ClientRequestID      string `json:"clientRequestId"`
	Message              string `json:"message"`
	ExpectedStateVersion int    `json:"expectedStateVersion"`
}

type PendingSteer struct {
	ClientRequestID string `json:"clientRequestId"`
	Message         string `json:"message"`
}

type GenerationModelSelection struct {
	ChannelID string `json:"channelId"`
	Model     string `json:"model"`
}

type GenerationModelSelections struct {
	Image  *GenerationModelSelection `json:"image,omitempty"`
	Video  *GenerationModelSelection `json:"video,omitempty"`
	Audio  *GenerationModelSelection `json:"audio,omitempty"`
	Vision *GenerationModelSelection `json:"vision,omitempty"`
}

type SkillSelection struct {
	Dir                string                  `json:"dir"`
	Name               string                  `json:"name"`
	Description        string                  `json:"description"`
	Instructions       string                  `json:"instructions"`
	Version            int                     `json:"version"`
	Checksum           string                  `json:"checksum"`
	CapabilityManifest SkillCapabilityManifest `json:"capabilityManifest"`
	SourceKind         string                  `json:"sourceKind,omitempty"`
	SourceURL          string                  `json:"sourceUrl,omitempty"`
	SourceRevision     string                  `json:"sourceRevision,omitempty"`
	SourceLicense      string                  `json:"sourceLicense,omitempty"`
	PublishedAt        string                  `json:"publishedAt,omitempty"`
}

type ExecutionMode string

const (
	ExecutionGuided    ExecutionMode = "guided"
	ExecutionAutomatic ExecutionMode = "automatic"
)

type ResourceAttachment struct {
	ResourceID string `json:"resourceId"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	MIMEType   string `json:"mimeType"`
	SizeBytes  int64  `json:"sizeBytes"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	DurationMS int64  `json:"durationMs,omitempty"`
}

// RunConfiguration is an immutable snapshot of the user's explicit composer choices.
// The server resolves and validates it before the first billed model task is created.
type RunConfiguration struct {
	GenerationModels GenerationModelSelections `json:"generationModels"`
	Skills           []SkillSelection          `json:"skills"`
	Attachments      []ResourceAttachment      `json:"attachments"`
	ExecutionMode    ExecutionMode             `json:"executionMode"`
}

type RuntimeInput struct {
	Decision ModelDecision
	Evidence DeliveryEvidence
}

type RuntimeTransition struct {
	State            RuntimeState
	EventKinds       []EventKind
	RejectedToolCall *ToolCallDecision
}

type ToolResolution struct {
	ToolCallID    string
	ActionVersion int
	Succeeded     bool
	Output        json.RawMessage
	ErrorCode     string
	FailureClass  ToolFailureClass
}

type ToolExecution struct {
	ToolCallID    string
	ActionVersion int
}

type ToolResult struct {
	ToolCallID    string          `json:"toolCallId"`
	ActionVersion int             `json:"actionVersion"`
	Succeeded     bool            `json:"succeeded"`
	Output        json.RawMessage `json:"output"`
	ErrorCode     string          `json:"errorCode,omitempty"`
}

type ModelDecisionFeedback struct {
	Code   string `json:"code"`
	Reason string `json:"reason"`
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

func AppendSteer(current RuntimeState, request SteerRequest) (RuntimeTransition, bool, error) {
	if err := validateRuntimeState(current); err != nil {
		return RuntimeTransition{}, false, err
	}
	request.ClientRequestID = strings.TrimSpace(request.ClientRequestID)
	request.Message = strings.TrimSpace(request.Message)
	if request.ClientRequestID == "" || len(request.ClientRequestID) > 120 || request.Message == "" || len(request.Message) > 64*1024 || request.ExpectedStateVersion < 1 {
		return RuntimeTransition{}, false, ErrSteerConflict
	}
	for _, pending := range current.PendingSteers {
		if pending.ClientRequestID != request.ClientRequestID {
			continue
		}
		if pending.Message != request.Message {
			return RuntimeTransition{}, false, ErrSteerConflict
		}
		return RuntimeTransition{State: current}, true, nil
	}
	if runtimeStatusTerminal(current.Status) || current.StateVersion != request.ExpectedStateVersion {
		return RuntimeTransition{}, false, ErrSteerConflict
	}
	next := current
	next.StateVersion++
	next.PendingSteers = append(append([]PendingSteer(nil), current.PendingSteers...), PendingSteer{
		ClientRequestID: request.ClientRequestID,
		Message:         request.Message,
	})
	if err := validateRuntimeState(next); err != nil {
		return RuntimeTransition{}, false, err
	}
	return RuntimeTransition{State: next, EventKinds: []EventKind{EventRunSteered}}, false, nil
}

func ConsumePendingSteersAtSafeBoundary(current RuntimeState) (RuntimeState, []PendingSteer, error) {
	if err := validateRuntimeState(current); err != nil {
		return RuntimeState{}, nil, err
	}
	if current.Status != RunQueued && current.Status != RunRunning {
		return RuntimeState{}, nil, ErrSteerConflict
	}
	if current.PendingToolCall != nil || current.PendingToolStarted || current.PendingClarification != nil {
		return RuntimeState{}, nil, ErrSteerConflict
	}
	if len(current.PendingSteers) == 0 {
		return current, nil, nil
	}
	consumed := append([]PendingSteer(nil), current.PendingSteers...)
	next := current
	next.PendingSteers = nil
	if err := validateRuntimeState(next); err != nil {
		return RuntimeState{}, nil, err
	}
	return next, consumed, nil
}

func Interrupt(current RuntimeState, expectedStateVersion int) (RuntimeTransition, error) {
	if err := validateRuntimeState(current); err != nil {
		return RuntimeTransition{}, err
	}
	if expectedStateVersion < 1 || current.StateVersion != expectedStateVersion || runtimeStatusTerminal(current.Status) {
		return RuntimeTransition{}, ErrInterruptConflict
	}
	next := current
	next.StateVersion++
	next.Status = RunCancelled
	next.PendingSteers = nil
	next.PendingClarification = nil
	next.DecisionFeedback = nil
	next.Verification = nil
	next.FailureCode = ""
	if !current.PendingToolStarted {
		next.PendingToolCall = nil
		next.PendingToolStarted = false
	}
	if err := validateRuntimeState(next); err != nil {
		return RuntimeTransition{}, err
	}
	return RuntimeTransition{State: next, EventKinds: []EventKind{EventRunInterrupted}}, nil
}

func runtimeStatusTerminal(status RunStatus) bool {
	return status == RunSucceeded || status == RunFailed || status == RunCancelled
}

// BeginModelRequest 在首个供应商请求发出前持久化 queued -> running；
// 后续模型步骤已经处于 running，不重复制造状态事件。
func BeginModelRequest(current RuntimeState) (RuntimeTransition, error) {
	if err := validateRuntimeState(current); err != nil {
		return RuntimeTransition{}, err
	}
	if current.Status != RunQueued {
		return RuntimeTransition{}, errors.New("agent runtime is not queued for a model request")
	}
	next := current
	next.StateVersion++
	next.Status = RunRunning
	if err := validateRuntimeState(next); err != nil {
		return RuntimeTransition{}, err
	}
	return RuntimeTransition{State: next, EventKinds: []EventKind{EventRunStatusChanged}}, nil
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
	next.PendingToolStarted = false
	next.Verification = nil
	next.FailureCode = failureCode
	return RuntimeTransition{State: next, EventKinds: []EventKind{EventRunFailed}}, nil
}

// Terminate records an external, non-model failure for any active runtime state.
func Terminate(current RuntimeState, failureCode string) (RuntimeTransition, error) {
	if err := validateRuntimeState(current); err != nil {
		return RuntimeTransition{}, err
	}
	if current.Status == RunSucceeded || current.Status == RunFailed || current.Status == RunCancelled {
		return RuntimeTransition{}, errors.New("agent runtime is already terminal")
	}
	failureCode = strings.TrimSpace(failureCode)
	if !validFailureCode(failureCode) {
		return RuntimeTransition{}, errors.New("agent runtime failure code is invalid")
	}
	next := current
	next.StateVersion++
	next.Status = RunFailed
	next.Verification = nil
	next.FailureCode = failureCode
	if current.PendingToolCall != nil {
		next.LastToolResult = &ToolResult{
			ToolCallID: current.PendingToolCall.ToolCallID, ActionVersion: current.PendingToolCall.ActionVersion,
			Succeeded: false, Output: json.RawMessage(`{}`), ErrorCode: failureCode,
		}
	}
	next.PendingToolCall = nil
	next.PendingToolStarted = false
	next.PendingClarification = nil
	eventKinds := []EventKind{EventRunFailed}
	if current.PendingToolCall != nil {
		eventKinds = []EventKind{EventToolResult, EventRunFailed}
	}
	return RuntimeTransition{State: next, EventKinds: eventKinds}, nil
}

func Advance(current RuntimeState, input RuntimeInput) (RuntimeTransition, error) {
	return AdvanceForToolSchema(current, input, CurrentToolSchemaVersion)
}

func AdvanceForToolSchema(current RuntimeState, input RuntimeInput, toolSchemaVersion int) (RuntimeTransition, error) {
	if err := validateAdvancingState(current); err != nil {
		return RuntimeTransition{}, err
	}
	if err := input.Decision.ValidateForToolSchema(toolSchemaVersion); err != nil {
		return RuntimeTransition{}, err
	}
	var decisionExpected ExpectedDelivery
	if input.Decision.ToolCall != nil {
		decisionExpected = input.Decision.ToolCall.ExpectedDelivery
	} else if input.Decision.Clarification != nil {
		decisionExpected = input.Decision.Clarification.ExpectedDelivery
	} else {
		decisionExpected = input.Decision.Final.ExpectedDelivery
	}
	next := current
	next.StateVersion++
	next.StepNumber++
	if current.ExpectedDelivery == nil {
		frozen := decisionExpected
		next.ExpectedDelivery = &frozen
	} else if !current.ExpectedDelivery.Equal(decisionExpected) {
		next.Status = RunRunning
		next.PendingToolCall = nil
		next.PendingToolStarted = false
		next.Verification = nil
		next.DecisionFeedback = &ModelDecisionFeedback{
			Code: "delivery_contract_changed", Reason: "expectedDelivery must exactly match the contract frozen by the first model decision",
		}
		next.FinalMessage = ""
		next.FailureCode = ""
		if next.StepNumber >= next.MaxSteps {
			next.Status = RunFailed
			next.FailureCode = "step_budget_exhausted"
			return RuntimeTransition{State: next, EventKinds: []EventKind{EventModelRejected, EventRunFailed}}, nil
		}
		return RuntimeTransition{State: next, EventKinds: []EventKind{EventModelRejected, EventRunStatusChanged}}, nil
	}
	next.PendingToolCall = nil
	next.PendingToolStarted = false
	next.LastToolResult = nil
	next.DecisionFeedback = nil
	next.Verification = nil
	next.FailureCode = ""
	next.PendingClarification = nil

	if input.Decision.Kind == DecisionClarificationRequest {
		if next.StepNumber >= next.MaxSteps {
			next.Status = RunFailed
			next.FailureCode = "step_budget_exhausted"
			return RuntimeTransition{State: next, EventKinds: []EventKind{EventRunFailed}}, nil
		}
		if _, reused := completedClarificationByRequestID(next.ClarificationHistory, input.Decision.Clarification.RequestID); reused {
			next.Status = RunRunning
			next.DecisionFeedback = &ModelDecisionFeedback{
				Code: "clarification_identity_reused", Reason: "clarification requestId was already completed and must not be reused",
			}
			return RuntimeTransition{State: next, EventKinds: []EventKind{EventModelRejected, EventRunStatusChanged}}, nil
		}
		next.Status = RunWaitingInput
		next.PendingClarification = &PendingClarification{
			Request: cloneClarificationDecision(*input.Decision.Clarification),
			Answers: []ClarificationAnswer{},
		}
		return RuntimeTransition{State: next, EventKinds: []EventKind{EventClarificationRequested, EventRunStatusChanged}}, nil
	}

	if input.Decision.Kind == DecisionToolCall {
		if next.StepNumber >= next.MaxSteps {
			next.Status = RunFailed
			next.FailureCode = "step_budget_exhausted"
			return RuntimeTransition{State: next, EventKinds: []EventKind{EventRunFailed}}, nil
		}
		policy, ok := ToolPolicyForSchema(input.Decision.ToolCall.ToolName, toolSchemaVersion)
		if !ok {
			return RuntimeTransition{}, errors.New("agent tool policy is unavailable")
		}
		next.Status = RunWaitingTool
		next.PendingToolCall = input.Decision.ToolCall
		if ApprovalRequiredFor(policy, current.Configuration.ExecutionMode) {
			next.Status = RunWaitingApproval
			return RuntimeTransition{State: next, EventKinds: []EventKind{EventToolCall, EventApprovalRequired, EventRunStatusChanged}}, nil
		}
		return RuntimeTransition{State: next, EventKinds: []EventKind{EventToolCall, EventRunStatusChanged}}, nil
	}

	final := input.Decision.Final
	if missing := missingRequiredSkillDirs(next.Configuration.Skills, next.LoadedSkillDirs); len(missing) > 0 {
		next.FinalMessage = ""
		next.DecisionFeedback = &ModelDecisionFeedback{
			Code: "required_skill_not_loaded", Reason: "load the missing selected skills before final delivery: " + strings.Join(missing, ", "),
		}
		if next.StepNumber >= next.MaxSteps {
			next.Status = RunFailed
			next.FailureCode = "step_budget_exhausted"
			return RuntimeTransition{State: next, EventKinds: []EventKind{EventModelRejected, EventRunFailed}}, nil
		}
		next.Status = RunRunning
		return RuntimeTransition{State: next, EventKinds: []EventKind{EventModelRejected, EventRunStatusChanged}}, nil
	}
	verification := VerifyDelivery(final.ExpectedDelivery, input.Evidence)
	next.Verification = &verification
	next.FinalMessage = final.Message
	switch verification.Status {
	case VerificationSatisfied:
		next.Status = RunSucceeded
		return RuntimeTransition{State: next, EventKinds: []EventKind{EventAgentMessageCompleted, EventRunCompleted}}, nil
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

// RejectModelDecision feeds a structurally invalid model decision back into
// the same bounded run so the Agent can correct itself without hiding the error.
func RejectModelDecision(current RuntimeState, feedback ModelDecisionFeedback) (RuntimeTransition, error) {
	if err := validateAdvancingState(current); err != nil {
		return RuntimeTransition{}, err
	}
	feedback.Code = strings.TrimSpace(feedback.Code)
	feedback.Reason = strings.TrimSpace(feedback.Reason)
	if !validModelDecisionFeedbackCode(feedback.Code) || feedback.Reason == "" || len(feedback.Reason) > 240 {
		return RuntimeTransition{}, errors.New("agent model decision feedback is invalid")
	}
	next := current
	next.StateVersion++
	next.StepNumber++
	next.Status = RunRunning
	next.PendingToolCall = nil
	next.PendingToolStarted = false
	next.DecisionFeedback = &feedback
	next.Verification = nil
	next.FinalMessage = ""
	next.FailureCode = ""
	if next.StepNumber >= next.MaxSteps {
		next.Status = RunFailed
		next.FailureCode = "step_budget_exhausted"
		return RuntimeTransition{State: next, EventKinds: []EventKind{EventModelRejected, EventRunFailed}}, nil
	}
	return RuntimeTransition{State: next, EventKinds: []EventKind{EventModelRejected, EventRunStatusChanged}}, nil
}

// RejectReusedToolIdentity turns a model's reused tool identity into an
// explicit repair fact without attempting to persist a duplicate tool call.
func RejectReusedToolIdentity(current RuntimeState, call ToolCallDecision) (RuntimeTransition, error) {
	if err := validateAdvancingState(current); err != nil {
		return RuntimeTransition{}, err
	}
	decision := ModelDecision{Kind: DecisionToolCall, ToolCall: &call}
	if err := decision.Validate(); err != nil {
		return RuntimeTransition{}, err
	}
	next := current
	next.StateVersion++
	next.StepNumber++
	next.PendingToolCall = nil
	next.PendingToolStarted = false
	next.Verification = nil
	next.FinalMessage = ""
	next.FailureCode = ""
	next.DecisionFeedback = nil
	next.LastToolResult = &ToolResult{
		ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion,
		Succeeded: false, Output: json.RawMessage(`{"reason":"toolCallId and actionVersion were already used"}`), ErrorCode: "tool_identity_reused",
	}
	if next.StepNumber >= next.MaxSteps {
		next.Status = RunFailed
		next.FailureCode = "step_budget_exhausted"
		return RuntimeTransition{State: next, EventKinds: []EventKind{EventToolResult, EventRunFailed}}, nil
	}
	next.Status = RunRunning
	return RuntimeTransition{State: next, EventKinds: []EventKind{EventToolResult, EventRunStatusChanged}}, nil
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
	if resolution.Succeeded && resolution.FailureClass != "" {
		return RuntimeTransition{}, errors.New("successful agent tool result cannot have a failure class")
	}
	if !resolution.Succeeded && !validFailureCode(resolution.ErrorCode) {
		return RuntimeTransition{}, errors.New("failed agent tool result requires an error code")
	}
	if !resolution.Succeeded && resolution.FailureClass != ToolFailureAgentRepairable && resolution.FailureClass != ToolFailureTerminal {
		return RuntimeTransition{}, errors.New("failed agent tool result requires a failure class")
	}
	next := current
	next.StateVersion++
	next.Status = RunRunning
	next.PendingToolCall = nil
	next.PendingToolStarted = false
	next.LastToolResult = &ToolResult{
		ToolCallID: resolution.ToolCallID, ActionVersion: resolution.ActionVersion,
		Succeeded: resolution.Succeeded, Output: append(json.RawMessage(nil), output...), ErrorCode: resolution.ErrorCode,
	}
	if resolution.Succeeded && current.PendingToolCall.ToolName == ToolSkillsLoad {
		loaded, err := resolvedSkillDir(current.Configuration.Skills, resolution.Output)
		if err != nil {
			return RuntimeTransition{}, err
		}
		next.LoadedSkillDirs = appendLoadedSkillDir(next.LoadedSkillDirs, loaded)
	}
	if !resolution.Succeeded && resolution.FailureClass == ToolFailureTerminal {
		next.Status = RunFailed
		next.FailureCode = resolution.ErrorCode
		return RuntimeTransition{State: next, EventKinds: []EventKind{EventToolResult, EventRunFailed}}, nil
	}
	if next.StepNumber >= next.MaxSteps {
		next.Status = RunFailed
		next.FailureCode = "step_budget_exhausted"
		return RuntimeTransition{State: next, EventKinds: []EventKind{EventToolResult, EventRunFailed}}, nil
	}
	return RuntimeTransition{State: next, EventKinds: []EventKind{EventToolResult, EventRunStatusChanged}}, nil
}

func BeginToolExecution(current RuntimeState, execution ToolExecution) (RuntimeTransition, error) {
	if err := validateRuntimeState(current); err != nil {
		return RuntimeTransition{}, err
	}
	if current.Status != RunWaitingTool || current.PendingToolCall == nil || current.PendingToolStarted {
		return RuntimeTransition{}, errors.New("agent runtime tool is not pending execution")
	}
	execution.ToolCallID = strings.TrimSpace(execution.ToolCallID)
	if execution.ToolCallID != current.PendingToolCall.ToolCallID || execution.ActionVersion != current.PendingToolCall.ActionVersion {
		return RuntimeTransition{}, errors.New("agent tool execution identity is invalid")
	}
	next := current
	next.StateVersion++
	next.PendingToolStarted = true
	return RuntimeTransition{State: next, EventKinds: []EventKind{EventToolStarted}}, nil
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
		if next.StepNumber >= next.MaxSteps {
			next.Status = RunFailed
			next.PendingToolCall = nil
			next.PendingToolStarted = false
			next.FailureCode = "step_budget_exhausted"
			next.LastToolResult = &ToolResult{
				ToolCallID: approval.ToolCallID, ActionVersion: approval.ActionVersion,
				Succeeded: false, Output: json.RawMessage(`{}`), ErrorCode: "step_budget_exhausted",
			}
			return RuntimeTransition{State: next, EventKinds: []EventKind{EventApprovalDecided, EventToolResult, EventRunFailed}}, nil
		}
		next.Status = RunWaitingTool
		return RuntimeTransition{State: next, EventKinds: []EventKind{EventApprovalDecided, EventRunStatusChanged}}, nil
	case ToolApprovalRejected:
		next.Status = RunCancelled
		next.PendingToolCall = nil
		next.PendingToolStarted = false
		next.LastToolResult = &ToolResult{
			ToolCallID: approval.ToolCallID, ActionVersion: approval.ActionVersion,
			Succeeded: false, Output: json.RawMessage(`{}`), ErrorCode: "tool_approval_rejected",
		}
		next.FailureCode = ""
		return RuntimeTransition{State: next, EventKinds: []EventKind{EventApprovalDecided, EventToolResult, EventRunInterrupted}}, nil
	default:
		return RuntimeTransition{}, errors.New("agent tool approval decision is invalid")
	}
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
	if !state.Status.Valid() {
		return errors.New("agent runtime status is invalid")
	}
	if state.PendingToolStarted && ((state.Status != RunWaitingTool && state.Status != RunCancelled) || state.PendingToolCall == nil) {
		return errors.New("agent runtime tool execution state is invalid")
	}
	if err := validatePendingSteers(state.PendingSteers); err != nil {
		return err
	}
	if err := validateClarificationState(state); err != nil {
		return err
	}
	if strings.TrimSpace(state.UserMessage) == "" || len(state.UserMessage) > 64*1024 {
		return errors.New("agent runtime user message is invalid")
	}
	if err := ValidateRunConfiguration(state.Configuration); err != nil {
		return err
	}
	if state.DecisionFeedback != nil {
		feedback := *state.DecisionFeedback
		feedback.Code = strings.TrimSpace(feedback.Code)
		feedback.Reason = strings.TrimSpace(feedback.Reason)
		if !validModelDecisionFeedbackCode(feedback.Code) || feedback.Reason == "" || len(feedback.Reason) > 240 {
			return errors.New("agent model decision feedback is invalid")
		}
	}
	if state.ExpectedDelivery != nil {
		if err := state.ExpectedDelivery.Validate(); err != nil {
			return err
		}
	}
	if err := validateLoadedSkillDirs(state.Configuration.Skills, state.LoadedSkillDirs); err != nil {
		return err
	}
	return nil
}

// ValidateRuntimeState verifies a persisted checkpoint before recovery resumes
// provider or tool execution. It performs no mutation and accepts no fallback.
func ValidateRuntimeState(state RuntimeState) error {
	return validateRuntimeState(state)
}

func validatePendingSteers(pendingSteers []PendingSteer) error {
	identities := make(map[string]struct{}, len(pendingSteers))
	for _, pending := range pendingSteers {
		if strings.TrimSpace(pending.ClientRequestID) != pending.ClientRequestID || pending.ClientRequestID == "" || len(pending.ClientRequestID) > 120 ||
			strings.TrimSpace(pending.Message) != pending.Message || pending.Message == "" || len(pending.Message) > 64*1024 {
			return errors.New("agent runtime pending steer is invalid")
		}
		if _, duplicated := identities[pending.ClientRequestID]; duplicated {
			return errors.New("agent runtime pending steer is duplicated")
		}
		identities[pending.ClientRequestID] = struct{}{}
	}
	return nil
}

func validModelDecisionFeedbackCode(code string) bool {
	return code == "model_decision_invalid" || code == "delivery_contract_changed" || code == "required_skill_not_loaded" || code == "clarification_identity_reused"
}

func validateClarificationState(state RuntimeState) error {
	if state.Status == RunWaitingInput {
		if state.PendingClarification == nil || state.PendingToolCall != nil || state.PendingToolStarted {
			return errors.New("agent runtime clarification state is invalid")
		}
	} else if state.PendingClarification != nil {
		return errors.New("agent runtime clarification state is invalid")
	}
	requestIDs := make(map[string]struct{}, len(state.ClarificationHistory))
	for _, completed := range state.ClarificationHistory {
		if err := validateCompletedClarification(completed); err != nil {
			return err
		}
		if _, duplicated := requestIDs[completed.Request.RequestID]; duplicated {
			return errors.New("agent runtime clarification history is duplicated")
		}
		requestIDs[completed.Request.RequestID] = struct{}{}
		if state.ExpectedDelivery == nil || !state.ExpectedDelivery.Equal(completed.Request.ExpectedDelivery) {
			return errors.New("agent runtime clarification delivery contract is invalid")
		}
	}
	if state.PendingClarification != nil {
		pending := state.PendingClarification
		if err := validateClarificationRecord(pending.Request, pending.Answers, false); err != nil {
			return err
		}
		if _, reused := requestIDs[pending.Request.RequestID]; reused {
			return errors.New("agent runtime clarification identity is reused")
		}
		if state.ExpectedDelivery == nil || !state.ExpectedDelivery.Equal(pending.Request.ExpectedDelivery) {
			return errors.New("agent runtime clarification delivery contract is invalid")
		}
	}
	return nil
}

type resolvedSkillLoad struct {
	Dir          string `json:"dir"`
	Name         string `json:"name"`
	Version      int    `json:"version"`
	Instructions string `json:"instructions"`
}

func resolvedSkillDir(selected []SkillSelection, output json.RawMessage) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.DisallowUnknownFields()
	var result resolvedSkillLoad
	if err := decoder.Decode(&result); err != nil {
		return "", errors.New("agent skill load result is invalid")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return "", errors.New("agent skill load result is invalid")
	}
	result.Dir = strings.TrimSpace(result.Dir)
	result.Name = strings.TrimSpace(result.Name)
	result.Instructions = strings.TrimSpace(result.Instructions)
	for _, skill := range selected {
		if skill.Dir == result.Dir && skill.Name == result.Name && skill.Version == result.Version && skill.Instructions == result.Instructions {
			return result.Dir, nil
		}
	}
	return "", errors.New("agent skill load result conflicts with frozen selection")
}

func appendLoadedSkillDir(current []string, dir string) []string {
	next := append([]string(nil), current...)
	for _, loaded := range next {
		if loaded == dir {
			return next
		}
	}
	next = append(next, dir)
	sort.Strings(next)
	return next
}

func missingRequiredSkillDirs(selected []SkillSelection, loaded []string) []string {
	loadedSet := make(map[string]struct{}, len(loaded))
	for _, dir := range loaded {
		loadedSet[dir] = struct{}{}
	}
	missing := make([]string, 0)
	for _, skill := range selected {
		if _, ok := loadedSet[skill.Dir]; !ok {
			missing = append(missing, skill.Dir)
		}
	}
	return missing
}

func validateLoadedSkillDirs(selected []SkillSelection, loaded []string) error {
	selectedSet := make(map[string]struct{}, len(selected))
	for _, skill := range selected {
		selectedSet[skill.Dir] = struct{}{}
	}
	previous := ""
	for _, dir := range loaded {
		if strings.TrimSpace(dir) != dir || dir == "" || (previous != "" && dir <= previous) {
			return errors.New("agent runtime loaded skill facts are invalid")
		}
		if _, ok := selectedSet[dir]; !ok {
			return errors.New("agent runtime loaded skill facts conflict with configuration")
		}
		previous = dir
	}
	return nil
}

func ValidateRunConfiguration(configuration RunConfiguration) error {
	if configuration.ExecutionMode != ExecutionGuided && configuration.ExecutionMode != ExecutionAutomatic {
		return errors.New("agent runtime execution mode is invalid")
	}
	for _, selection := range []*GenerationModelSelection{configuration.GenerationModels.Image, configuration.GenerationModels.Video, configuration.GenerationModels.Audio, configuration.GenerationModels.Vision} {
		if selection == nil {
			continue
		}
		selection.ChannelID = strings.TrimSpace(selection.ChannelID)
		selection.Model = strings.TrimSpace(selection.Model)
		if selection.ChannelID == "" || len(selection.ChannelID) > 80 || selection.Model == "" || len(selection.Model) > 120 {
			return errors.New("agent runtime generation model selection is invalid")
		}
	}
	if len(configuration.Skills) > 8 {
		return errors.New("agent runtime skill selection is invalid")
	}
	previousDir := ""
	totalInstructions := 0
	for _, skill := range configuration.Skills {
		skill.Dir = strings.TrimSpace(skill.Dir)
		skill.Name = strings.TrimSpace(skill.Name)
		skill.Description = strings.TrimSpace(skill.Description)
		skill.Instructions = strings.TrimSpace(skill.Instructions)
		skill.Checksum = strings.TrimSpace(skill.Checksum)
		checksum, checksumError := hex.DecodeString(skill.Checksum)
		expectedChecksum := sha256.Sum256([]byte(skill.Instructions))
		if skill.Dir == "" || len(skill.Dir) > 120 || skill.Name == "" || len(skill.Name) > 160 ||
			len(skill.Description) > 4*1024 || skill.Instructions == "" || len(skill.Instructions) > 32*1024 ||
			skill.Version <= 0 || checksumError != nil || len(checksum) != sha256.Size || skill.Checksum != strings.ToLower(skill.Checksum) ||
			!bytes.Equal(checksum, expectedChecksum[:]) ||
			(previousDir != "" && skill.Dir <= previousDir) {
			return errors.New("agent runtime skill selection is invalid")
		}
		previousDir = skill.Dir
		totalInstructions += len(skill.Instructions)
		if len(skill.CapabilityManifest.Specialists) > 0 || len(skill.CapabilityManifest.Tools) > 0 || len(skill.CapabilityManifest.ArtifactSchemas) > 0 {
			if err := ValidateSkillCapabilityManifest(skill.CapabilityManifest); err != nil {
				return errors.New("agent runtime skill capability manifest is invalid")
			}
			if strings.TrimSpace(skill.SourceKind) != skill.SourceKind || strings.TrimSpace(skill.SourceLicense) != skill.SourceLicense ||
				skill.SourceKind == "" || len(skill.SourceKind) > 32 || skill.SourceLicense == "" || len(skill.SourceLicense) > 80 ||
				strings.TrimSpace(skill.SourceURL) != skill.SourceURL || len(skill.SourceURL) > 1000 ||
				strings.TrimSpace(skill.SourceRevision) != skill.SourceRevision || len(skill.SourceRevision) > 160 ||
				strings.TrimSpace(skill.PublishedAt) != skill.PublishedAt || skill.PublishedAt == "" || len(skill.PublishedAt) > 64 {
				return errors.New("agent runtime frozen skill source facts are invalid")
			}
		}
	}
	if totalInstructions > 64*1024 {
		return errors.New("agent runtime skill selection is invalid")
	}
	if len(configuration.Attachments) > 12 {
		return errors.New("agent runtime attachment selection is invalid")
	}
	previousResourceID := ""
	var totalAttachmentBytes int64
	for _, attachment := range configuration.Attachments {
		attachment.ResourceID = strings.TrimSpace(attachment.ResourceID)
		attachment.Name = strings.TrimSpace(attachment.Name)
		attachment.Kind = strings.TrimSpace(attachment.Kind)
		attachment.MIMEType = strings.TrimSpace(attachment.MIMEType)
		if attachment.ResourceID == "" || len(attachment.ResourceID) > 80 || attachment.Name == "" || len(attachment.Name) > 240 ||
			!validFrozenMediaMIME(attachment.Kind, attachment.MIMEType) || len(attachment.MIMEType) > 120 || attachment.SizeBytes <= 0 ||
			attachment.SizeBytes > 4<<30 || !validFrozenMediaDimensions(attachment) ||
			(previousResourceID != "" && attachment.ResourceID <= previousResourceID) {
			return errors.New("agent runtime attachment selection is invalid")
		}
		if totalAttachmentBytes > (4<<30)-attachment.SizeBytes {
			return errors.New("agent runtime attachment selection is invalid")
		}
		totalAttachmentBytes += attachment.SizeBytes
		previousResourceID = attachment.ResourceID
	}
	return nil
}

func validFrozenMediaMIME(kind string, mimeType string) bool {
	if mimeType == "" || strings.ToLower(mimeType) != mimeType || strings.ContainsAny(mimeType, "; ") {
		return false
	}
	prefix := kind + "/"
	if kind != "image" && kind != "audio" && kind != "video" || !strings.HasPrefix(mimeType, prefix) || len(mimeType) <= len(prefix) {
		return false
	}
	for _, character := range strings.TrimPrefix(mimeType, prefix) {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '.' || character == '+' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func validFrozenMediaDimensions(attachment ResourceAttachment) bool {
	switch attachment.Kind {
	case "image":
		return attachment.Width > 0 && attachment.Height > 0 && attachment.DurationMS >= 0
	case "audio":
		return attachment.Width == 0 && attachment.Height == 0 && attachment.DurationMS > 0
	case "video":
		return attachment.Width > 0 && attachment.Height > 0 && attachment.DurationMS > 0
	default:
		return false
	}
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
