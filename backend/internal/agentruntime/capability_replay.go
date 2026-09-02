package agentruntime

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

type ToolReplay struct {
	Call                ToolCallDecision
	SourceRunID         string
	SourceToolCallID    string
	SourceActionVersion int
	Result              ToolResolution
}

func CapabilityIdempotencyKey(scope Scope, call ToolCallDecision) (string, error) {
	if err := scope.Validate(); err != nil {
		return "", err
	}
	if call.ToolName != ToolMediaGenerate && call.ToolName != ToolVisionAnalyze {
		return "", nil
	}
	decoded, err := DecodeCapabilityArguments(call.ToolName, call.Arguments)
	if err != nil {
		return "", err
	}
	if call.ActionVersion != 1 {
		return "", errors.New("commercial capability idempotency facts are invalid")
	}
	var facts string
	switch arguments := decoded.(type) {
	case MediaGenerateArguments:
		facts = strings.Join([]string{
			string(scope.TenantKind), scope.TenantID, scope.ActorUserID, scope.DomainProjectID,
			scope.CanvasID, scope.ThreadID, string(call.ToolName), strconv.Itoa(call.ActionVersion),
			arguments.TargetCanvasNodeID, arguments.ClientRequestID,
		}, "\x00")
	case VisionAnalyzeArguments:
		facts = strings.Join([]string{
			string(scope.TenantKind), scope.TenantID, scope.ActorUserID, scope.DomainProjectID,
			scope.CanvasID, scope.ThreadID, scope.RunID, string(call.ToolName), strconv.Itoa(call.ActionVersion),
			arguments.ModelRecordID, arguments.ModelKey, strings.Join(arguments.SourceResourceIDs, "\x1f"),
			arguments.Prompt, string(arguments.Detail), arguments.ClientRequestID,
		}, "\x00")
	default:
		return "", errors.New("commercial capability idempotency facts are invalid")
	}
	digest := sha256.Sum256([]byte(facts))
	return "cap-" + hex.EncodeToString(digest[:16]), nil
}

func ReplayResolvedTool(current RuntimeState, pending RuntimeTransition, replay ToolReplay) (RuntimeTransition, error) {
	if err := validateAdvancingState(current); err != nil {
		return RuntimeTransition{}, err
	}
	if pending.State.Status != RunWaitingApproval || pending.State.PendingToolCall == nil ||
		pending.State.PendingToolStarted || pending.State.StateVersion != current.StateVersion+1 ||
		pending.State.StepNumber != current.StepNumber+1 {
		return RuntimeTransition{}, errors.New("agent replay source transition is invalid")
	}
	call := pending.State.PendingToolCall
	if replay.Call.ToolCallID != call.ToolCallID || replay.Call.ActionVersion != call.ActionVersion ||
		replay.Call.ToolName != call.ToolName || !bytes.Equal(replay.Call.Arguments, call.Arguments) ||
		strings.TrimSpace(replay.SourceRunID) == "" || strings.TrimSpace(replay.SourceToolCallID) == "" ||
		replay.SourceActionVersion < 1 {
		return RuntimeTransition{}, errors.New("agent replay identity is invalid")
	}
	resolution := replay.Result
	resolution.ToolCallID = strings.TrimSpace(resolution.ToolCallID)
	resolution.ErrorCode = strings.TrimSpace(resolution.ErrorCode)
	output := bytes.TrimSpace(resolution.Output)
	if resolution.ToolCallID != call.ToolCallID || resolution.ActionVersion != call.ActionVersion ||
		len(output) == 0 || len(output) > agentToolResultLimit || output[0] != '{' || !json.Valid(output) {
		return RuntimeTransition{}, errors.New("agent replay result is invalid")
	}
	if resolution.Succeeded && (resolution.ErrorCode != "" || resolution.FailureClass != "") {
		return RuntimeTransition{}, errors.New("successful agent replay result contains failure facts")
	}
	if !resolution.Succeeded && (!validFailureCode(resolution.ErrorCode) ||
		(resolution.FailureClass != ToolFailureAgentRepairable && resolution.FailureClass != ToolFailureTerminal)) {
		return RuntimeTransition{}, errors.New("failed agent replay result is invalid")
	}
	next := pending.State
	next.Status = RunRunning
	next.PendingToolCall = nil
	next.PendingToolStarted = false
	next.LastToolResult = &ToolResult{
		ToolCallID: resolution.ToolCallID, ActionVersion: resolution.ActionVersion,
		Succeeded: resolution.Succeeded, Output: append(json.RawMessage(nil), output...), ErrorCode: resolution.ErrorCode,
	}
	if !resolution.Succeeded && resolution.FailureClass == ToolFailureTerminal {
		next.Status = RunFailed
		next.FailureCode = resolution.ErrorCode
	}
	events := []EventKind{EventToolCall, EventToolResult, EventRunStatusChanged}
	if next.Status == RunFailed {
		events = []EventKind{EventToolCall, EventToolResult, EventRunFailed}
	}
	replay.Result.Output = append(json.RawMessage(nil), output...)
	return RuntimeTransition{State: next, EventKinds: events, ToolReplay: &replay}, nil
}
