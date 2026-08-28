package service

import (
	"encoding/json"
	"errors"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

func (s *Service) coordinatePendingAgentMediaGeneration(
	scope agentruntime.Scope,
	state agentruntime.RuntimeState,
	call *agentruntime.ToolCallDecision,
	record *model.AgentToolCall,
) (*AgentRuntimeProgress, error) {
	arguments, err := decodeFrozenAgentMediaGenerationArguments(call.Arguments)
	if err != nil {
		return s.resolvePendingAgentToolFailure(scope, state, call, "media_generation_invalid")
	}
	if !state.PendingToolStarted {
		started, beginErr := agentruntime.BeginToolExecution(state, agentruntime.ToolExecution{
			ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion,
		})
		if beginErr != nil {
			return nil, beginErr
		}
		progress, commitErr := s.commitAgentRuntimeState(scope, state, started)
		if commitErr != nil {
			return nil, commitErr
		}
		state = progress.State
		if !samePendingStartedTool(state, call) {
			return s.agentMediaGenerationCompletedToolProgress(scope, state, call)
		}
		record, _, err = s.frozenAgentToolCall(scope, call, agentruntime.ToolCallRunning, state.Configuration.ExecutionMode)
		if err != nil {
			return nil, err
		}
	}

	task, order, err := s.ensureAgentMediaGenerationTask(scope, record, arguments)
	if err != nil {
		if errors.Is(err, ErrCostApprovalQuoteMismatch) {
			return s.failAgentMediaGeneration(scope, call, "media_generation_quote_changed", map[string]string{"reason": err.Error()})
		}
		if failureCode, _, classified := agentMediaGenerationFailureDetails(err); classified {
			return s.failAgentMediaGeneration(scope, call, failureCode, map[string]string{"reason": err.Error()})
		}
		return nil, err
	}
	switch task.Status {
	case model.TaskStatusQueued, model.TaskStatusRunning:
		return s.agentRuntimeProgressForCurrentState(scope, state)
	case model.TaskStatusFailed:
		return s.failAgentMediaGeneration(scope, call, "media_generation_failed", taskFailureOutput(*task))
	case model.TaskStatusCancelled:
		return s.failAgentMediaGeneration(scope, call, "media_generation_cancelled", map[string]string{"taskId": task.ID})
	case model.TaskStatusSucceeded:
	default:
		return nil, errors.New("agent media generation task status is invalid")
	}

	candidates, disposition, err := s.materializeAgentMediaCandidates(scope, *task, arguments, call)
	if err != nil {
		if errors.Is(err, errAgentMediaResultInvalid) || errors.Is(err, ErrVisualCandidateReviewInvalid) {
			return s.failAgentMediaGeneration(scope, call, "media_generation_result_invalid", map[string]string{
				"taskId": task.ID, "reason": err.Error(),
			})
		}
		return nil, err
	}
	if disposition == repository.MediaAttemptWriteUnadopted {
		latest, loadErr := s.repo.LoadAgentCheckpoint(scope)
		if loadErr != nil {
			return nil, loadErr
		}
		return s.agentRuntimeProgressForCurrentState(scope, latest)
	}
	refs := make([]agentruntime.ArtifactRevisionRef, 0, len(candidates))
	for _, candidate := range candidates {
		refs = append(refs, agentruntime.ArtifactRevisionRef{ArtifactID: candidate.ArtifactID, RevisionID: candidate.ID})
	}
	audioMode, err := agentMediaAudioModeForArguments(arguments)
	if err != nil {
		return s.failAgentMediaGeneration(scope, call, "media_generation_result_invalid", map[string]string{
			"taskId": task.ID, "reason": err.Error(),
		})
	}
	output, err := json.Marshal(agentruntime.MediaGenerationToolResult{
		TaskID: task.ID, BillingOrderID: order.ID, AudioMode: audioMode, Candidates: refs,
	})
	if err != nil {
		return nil, err
	}
	latest, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	if !samePendingStartedTool(latest, call) {
		return s.agentMediaGenerationCompletedToolProgress(scope, latest, call)
	}
	resolved, err := agentruntime.ResolveTool(latest, agentruntime.ToolResolution{
		ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion, Succeeded: true, Output: output,
	})
	if err != nil {
		return nil, err
	}
	progress, err := s.commitAgentRuntimeState(scope, latest, resolved)
	if err != nil {
		return nil, err
	}
	return s.agentMediaGenerationCompletedToolProgress(scope, progress.State, call)
}

func (s *Service) failAgentMediaGeneration(
	scope agentruntime.Scope,
	call *agentruntime.ToolCallDecision,
	failureCode string,
	output map[string]string,
) (*AgentRuntimeProgress, error) {
	latest, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	if !samePendingStartedTool(latest, call) {
		return s.agentMediaGenerationCompletedToolProgress(scope, latest, call)
	}
	return s.resolvePendingAgentToolFailureWithOutput(scope, latest, call, failureCode, output)
}

func (s *Service) agentMediaGenerationCompletedToolProgress(
	scope agentruntime.Scope,
	state agentruntime.RuntimeState,
	call *agentruntime.ToolCallDecision,
) (*AgentRuntimeProgress, error) {
	record, err := s.repo.AgentToolCallForScope(scope, call.ToolCallID, call.ActionVersion)
	if err != nil {
		return nil, err
	}
	if record.Status != agentruntime.ToolCallSucceeded && record.Status != agentruntime.ToolCallFailed {
		return nil, errors.New("agent media generation tool completion facts conflict")
	}
	return s.agentRuntimeProgressForCurrentState(scope, state)
}
