package service

import (
	"encoding/json"
	"errors"
	"time"

	"infinite-canvas/backend/internal/agentruntime"

	"gorm.io/gorm"
)

func (s *Service) prepareAgentRuntimeApproval(
	scope agentruntime.Scope,
	current agentruntime.RuntimeState,
	transition agentruntime.RuntimeTransition,
	now time.Time,
) (agentruntime.RuntimeTransition, error) {
	if transition.State.Status != agentruntime.RunWaitingApproval || transition.State.PendingToolCall == nil {
		return transition, nil
	}
	if transition.State.PendingToolCall.ToolName == agentruntime.ToolVisionAnalyze {
		quote, err := s.freezeAgentVisionCapabilityQuote(scope, *transition.State.PendingToolCall, now)
		if err != nil {
			return agentruntime.RuntimeTransition{}, errors.Join(errors.New("vision.analyze approval quote could not be frozen"), err)
		}
		transition.ApprovalCostQuote = quote
		return transition, nil
	}
	if transition.State.PendingToolCall.ToolName != agentruntime.ToolMediaGenerate {
		return transition, nil
	}
	replayed, found, err := s.replayCompletedAgentMediaCapability(scope, current, transition)
	if err != nil {
		return agentruntime.RuntimeTransition{}, err
	}
	if found {
		return replayed, nil
	}
	quote, err := s.freezeAgentMediaCapabilityQuote(scope, *transition.State.PendingToolCall, now)
	if err == nil {
		transition.ApprovalCostQuote = quote
		return transition, nil
	}
	feedback, recoverable := agentRuntimeMediaDecisionFeedback(err)
	if !recoverable {
		return agentruntime.RuntimeTransition{}, errors.Join(errors.New("media.generate approval quote could not be frozen"), err)
	}
	return agentruntime.RejectModelDecision(current, feedback)
}

func (s *Service) replayCompletedAgentMediaCapability(
	scope agentruntime.Scope,
	current agentruntime.RuntimeState,
	transition agentruntime.RuntimeTransition,
) (agentruntime.RuntimeTransition, bool, error) {
	if transition.State.PendingToolCall == nil {
		return agentruntime.RuntimeTransition{}, false, errors.New("media.generate replay call is missing")
	}
	call := *transition.State.PendingToolCall
	capabilityKey, err := agentruntime.CapabilityIdempotencyKey(scope, call)
	if err != nil {
		return agentruntime.RuntimeTransition{}, false, err
	}
	record, err := s.repo.CompletedCapabilityCallForThread(scope, call.ToolName, capabilityKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return s.rejectInFlightAgentMediaCapability(scope, current, transition, capabilityKey)
	}
	if err != nil {
		return agentruntime.RuntimeTransition{}, false, err
	}
	if !equalAgentToolArguments(record.InputJSON, call.Arguments) {
		return agentruntime.RuntimeTransition{}, false, failAgentCapability(
			"media_request_conflict",
			"media.generate clientRequestId was already used with different arguments",
		)
	}
	sourceScope := scope
	sourceScope.RunID = record.RunID
	if _, err := s.currentMediaCapabilityDeliveryArtifacts(sourceScope, *record); err != nil {
		return agentruntime.RuntimeTransition{}, false, errors.Join(errors.New("media.generate replay receipt is not authoritative"), err)
	}
	result, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolMediaGenerate, json.RawMessage(record.OutputJSON))
	if err != nil {
		return agentruntime.RuntimeTransition{}, false, errors.Join(errors.New("media.generate replay receipt is invalid"), err)
	}
	encoded, err := agentruntime.NewToolExecutionResult(agentruntime.ToolMediaGenerate, result)
	if err != nil {
		return agentruntime.RuntimeTransition{}, false, err
	}
	sourceRunID := record.RunID
	sourceToolCallID := record.ToolCallID
	sourceActionVersion := record.ActionVersion
	if record.ReplaySourceRunID != "" {
		sourceRunID = record.ReplaySourceRunID
		sourceToolCallID = record.ReplaySourceToolCallID
		sourceActionVersion = record.ReplaySourceActionVersion
	}
	replayed, err := agentruntime.ReplayResolvedTool(current, transition, agentruntime.ToolReplay{
		Call: call, SourceRunID: sourceRunID, SourceToolCallID: sourceToolCallID,
		SourceActionVersion: sourceActionVersion,
		Result: agentruntime.ToolResolution{
			ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion,
			Succeeded: true, Output: encoded.Output,
		},
	})
	if err != nil {
		return agentruntime.RuntimeTransition{}, false, err
	}
	return replayed, true, nil
}

func (s *Service) rejectInFlightAgentMediaCapability(
	scope agentruntime.Scope,
	current agentruntime.RuntimeState,
	transition agentruntime.RuntimeTransition,
	capabilityKey string,
) (agentruntime.RuntimeTransition, bool, error) {
	if transition.State.PendingToolCall == nil {
		return agentruntime.RuntimeTransition{}, false, errors.New("media.generate in-flight call is missing")
	}
	call := *transition.State.PendingToolCall
	record, err := s.repo.InFlightCapabilityCallForThread(scope, call.ToolName, capabilityKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return agentruntime.RuntimeTransition{}, false, nil
	}
	if err != nil {
		return agentruntime.RuntimeTransition{}, false, err
	}
	if !equalAgentToolArguments(record.InputJSON, call.Arguments) {
		return agentruntime.RuntimeTransition{}, false, failAgentCapability(
			"media_request_conflict",
			"media.generate clientRequestId was already used with different arguments",
		)
	}
	rejected, err := agentruntime.RejectModelDecision(current, agentruntime.ModelDecisionFeedback{
		Code:   "model_decision_invalid",
		Reason: "media.generate request is already paid and still running; do not request another approval or create another generation",
	})
	if err != nil {
		return agentruntime.RuntimeTransition{}, false, err
	}
	return rejected, true, nil
}

func agentRuntimeMediaDecisionFeedback(err error) (agentruntime.ModelDecisionFeedback, bool) {
	reason := ""
	switch {
	case errors.Is(err, errAgentMediaArgumentsInvalid):
		reason = "media.generate arguments do not match the selected callable model capabilities"
	case errors.Is(err, errAgentMediaModelUnavailable):
		reason = "media.generate selected model identity is not callable in the current runtime context"
	case errors.Is(err, errAgentNativeAudioUnavailable):
		reason = "media.generate requested native audio is unavailable for the selected model facts"
	default:
		return agentruntime.ModelDecisionFeedback{}, false
	}
	return agentruntime.ModelDecisionFeedback{Code: "model_decision_invalid", Reason: reason}, true
}
