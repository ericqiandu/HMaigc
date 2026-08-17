package agentruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
)

const maxRuntimeSteps = 24

type RuntimeState struct {
	StateVersion       int                    `json:"stateVersion"`
	StepNumber         int                    `json:"stepNumber"`
	MaxSteps           int                    `json:"maxSteps"`
	Status             RunStatus              `json:"status"`
	ExpectedDelivery   *ExpectedDelivery      `json:"expectedDelivery,omitempty"`
	Verification       *DeliveryVerification  `json:"verification,omitempty"`
	PendingToolCall    *ToolCallDecision      `json:"pendingToolCall,omitempty"`
	PendingToolStarted bool                   `json:"pendingToolStarted,omitempty"`
	LastToolResult     *ToolResult            `json:"lastToolResult,omitempty"`
	DecisionFeedback   *ModelDecisionFeedback `json:"decisionFeedback,omitempty"`
	FinalMessage       string                 `json:"finalMessage,omitempty"`
	FailureCode        string                 `json:"failureCode,omitempty"`
	UserMessage        string                 `json:"userMessage"`
	Configuration      RunConfiguration       `json:"configuration"`
	LoadedSkillDirs    []string               `json:"loadedSkillDirs,omitempty"`
}

type GenerationModelSelection struct {
	ChannelID string `json:"channelId"`
	Model     string `json:"model"`
}

type GenerationModelSelections struct {
	Image *GenerationModelSelection `json:"image,omitempty"`
	Video *GenerationModelSelection `json:"video,omitempty"`
}

type SkillSelection struct {
	Dir          string `json:"dir"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
	Version      int    `json:"version"`
}

type ExecutionMode string

const (
	ExecutionGuided    ExecutionMode = "guided"
	ExecutionAutomatic ExecutionMode = "automatic"
)

type ResourceAttachment struct {
	ResourceID string `json:"resourceId"`
	Name       string `json:"name"`
	MIMEType   string `json:"mimeType"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
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
	return RuntimeTransition{State: next, EventKinds: []EventKind{EventRunFailed}}, nil
}

func Advance(current RuntimeState, input RuntimeInput) (RuntimeTransition, error) {
	if err := validateAdvancingState(current); err != nil {
		return RuntimeTransition{}, err
	}
	if err := input.Decision.Validate(); err != nil {
		return RuntimeTransition{}, err
	}
	var decisionExpected ExpectedDelivery
	if input.Decision.ToolCall != nil {
		decisionExpected = input.Decision.ToolCall.ExpectedDelivery
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

	if input.Decision.Kind == DecisionToolCall {
		if next.StepNumber >= next.MaxSteps {
			next.Status = RunFailed
			next.FailureCode = "step_budget_exhausted"
			return RuntimeTransition{State: next, EventKinds: []EventKind{EventRunFailed}}, nil
		}
		policy, ok := ToolPolicyFor(input.Decision.ToolCall.ToolName)
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
			Code: "required_skill_not_loaded", Reason: "load every explicitly selected skill before final delivery",
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
	if !resolution.Succeeded && !validFailureCode(resolution.ErrorCode) {
		return RuntimeTransition{}, errors.New("failed agent tool result requires an error code")
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
	if resolution.Succeeded && current.PendingToolCall.ToolName == ToolSkillLoad {
		loaded, err := resolvedSkillDir(current.Configuration.Skills, resolution.Output)
		if err != nil {
			return RuntimeTransition{}, err
		}
		next.LoadedSkillDirs = appendLoadedSkillDir(next.LoadedSkillDirs, loaded)
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
		next.Status = RunRunning
		next.PendingToolCall = nil
		next.PendingToolStarted = false
		next.LastToolResult = &ToolResult{
			ToolCallID: approval.ToolCallID, ActionVersion: approval.ActionVersion,
			Succeeded: false, Output: json.RawMessage(`{}`), ErrorCode: "tool_approval_rejected",
		}
		if next.StepNumber >= next.MaxSteps {
			next.Status = RunFailed
			next.FailureCode = "step_budget_exhausted"
			return RuntimeTransition{State: next, EventKinds: []EventKind{EventApprovalDecided, EventToolResult, EventRunFailed}}, nil
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
	if state.PendingToolStarted && (state.Status != RunWaitingTool || state.PendingToolCall == nil) {
		return errors.New("agent runtime tool execution state is invalid")
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

func validModelDecisionFeedbackCode(code string) bool {
	return code == "model_decision_invalid" || code == "delivery_contract_changed" || code == "required_skill_not_loaded"
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
	for _, selection := range []*GenerationModelSelection{configuration.GenerationModels.Image, configuration.GenerationModels.Video} {
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
		if skill.Dir == "" || len(skill.Dir) > 120 || skill.Name == "" || len(skill.Name) > 160 ||
			len(skill.Description) > 4*1024 || skill.Instructions == "" || len(skill.Instructions) > 32*1024 ||
			skill.Version < 0 || (previousDir != "" && skill.Dir <= previousDir) {
			return errors.New("agent runtime skill selection is invalid")
		}
		previousDir = skill.Dir
		totalInstructions += len(skill.Instructions)
	}
	if totalInstructions > 64*1024 {
		return errors.New("agent runtime skill selection is invalid")
	}
	if len(configuration.Attachments) > 4 {
		return errors.New("agent runtime attachment selection is invalid")
	}
	previousResourceID := ""
	for _, attachment := range configuration.Attachments {
		attachment.ResourceID = strings.TrimSpace(attachment.ResourceID)
		attachment.Name = strings.TrimSpace(attachment.Name)
		attachment.MIMEType = strings.TrimSpace(attachment.MIMEType)
		if attachment.ResourceID == "" || len(attachment.ResourceID) > 80 || attachment.Name == "" || len(attachment.Name) > 240 ||
			!strings.HasPrefix(attachment.MIMEType, "image/") || len(attachment.MIMEType) > 120 || attachment.Width < 0 || attachment.Height < 0 ||
			(previousResourceID != "" && attachment.ResourceID <= previousResourceID) {
			return errors.New("agent runtime attachment selection is invalid")
		}
		previousResourceID = attachment.ResourceID
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
