package service

import (
	"encoding/json"
	"errors"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func (s *Service) coordinatePendingAgentMediaAssembly(
	scope agentruntime.Scope,
	state agentruntime.RuntimeState,
	call *agentruntime.ToolCallDecision,
	record *model.AgentToolCall,
) (*AgentRuntimeProgress, error) {
	arguments, err := decodeMediaAssembleArguments(call.Arguments)
	if err != nil {
		return s.resolvePendingAgentToolFailure(scope, state, call, "media_assembly_invalid")
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
			return s.agentMediaAssemblyCompletedToolProgress(scope, state, call)
		}
		record, _, err = s.frozenAgentToolCall(scope, call, agentruntime.ToolCallRunning, state.Configuration.ExecutionMode)
		if err != nil {
			return nil, err
		}
	}

	task, err := s.EnqueueAgentMediaAssembly(EnqueueAgentMediaAssemblyInput{
		Scope: scope, PlanRevision: arguments.PlanRevision,
		ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion,
	})
	if err != nil {
		return s.failAgentMediaAssemblyTool(scope, call, "media_assembly_invalid", map[string]string{"reason": err.Error()})
	}
	content, err := s.mediaAssemblyTimelineContent(scope, call, arguments, *task)
	if err != nil {
		return nil, err
	}
	if task.Status == model.TaskStatusQueued || task.Status == model.TaskStatusRunning {
		if _, err := s.repo.AppendAgentMediaAssemblyTimeline(scope, content); err != nil {
			return nil, err
		}
		return s.agentRuntimeProgressForCurrentState(scope, state)
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	if task.Status == model.TaskStatusFailed || task.Status == model.TaskStatusCancelled {
		return s.failAgentMediaAssemblyToolWithJSON(scope, call, "media_assembly_"+string(content.TaskStatus), encoded)
	}
	if task.Status != model.TaskStatusSucceeded {
		return nil, errors.New("agent media assembly task status is invalid")
	}

	latest, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	if !samePendingStartedTool(latest, call) {
		return s.agentMediaAssemblyCompletedToolProgress(scope, latest, call)
	}
	resolved, err := agentruntime.ResolveTool(latest, agentruntime.ToolResolution{
		ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion, Succeeded: true, Output: encoded,
	})
	if err != nil {
		return nil, err
	}
	progress, err := s.commitAgentRuntimeState(scope, latest, resolved)
	if err != nil {
		return nil, err
	}
	return s.agentMediaAssemblyCompletedToolProgress(scope, progress.State, call)
}

func (s *Service) failAgentMediaAssemblyTool(
	scope agentruntime.Scope,
	call *agentruntime.ToolCallDecision,
	failureCode string,
	output map[string]string,
) (*AgentRuntimeProgress, error) {
	encoded, err := json.Marshal(output)
	if err != nil {
		return nil, err
	}
	return s.failAgentMediaAssemblyToolWithJSON(scope, call, failureCode, encoded)
}

func (s *Service) failAgentMediaAssemblyToolWithJSON(
	scope agentruntime.Scope,
	call *agentruntime.ToolCallDecision,
	failureCode string,
	output json.RawMessage,
) (*AgentRuntimeProgress, error) {
	latest, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	if !samePendingStartedTool(latest, call) {
		return s.agentMediaAssemblyCompletedToolProgress(scope, latest, call)
	}
	return s.resolvePendingAgentToolFailureWithJSONOutput(scope, latest, call, failureCode, output)
}

func (s *Service) agentMediaAssemblyCompletedToolProgress(
	scope agentruntime.Scope,
	state agentruntime.RuntimeState,
	call *agentruntime.ToolCallDecision,
) (*AgentRuntimeProgress, error) {
	record, err := s.repo.AgentToolCallForScope(scope, call.ToolCallID, call.ActionVersion)
	if err != nil {
		return nil, err
	}
	if record.Status != agentruntime.ToolCallSucceeded && record.Status != agentruntime.ToolCallFailed {
		return nil, errors.New("agent media assembly tool completion facts conflict")
	}
	return s.agentRuntimeProgressForCurrentState(scope, state)
}

func (s *Service) mediaAssemblyTimelineContent(
	scope agentruntime.Scope,
	call *agentruntime.ToolCallDecision,
	arguments MediaAssembleArguments,
	task model.Task,
) (agentruntime.MediaAssemblyTimelineContent, error) {
	input, err := decodeMediaAssemblyTaskInput(task.InputJSON)
	if err != nil || input.Scope != scope || input.PlanRevision != arguments.PlanRevision ||
		input.ToolCallID != call.ToolCallID || input.ActionVersion != call.ActionVersion {
		return agentruntime.MediaAssemblyTimelineContent{}, errors.New("agent media assembly frozen facts conflict")
	}
	revision, err := s.repo.ArtifactRevisionForArtifactInScope(scope, arguments.PlanRevision.ArtifactID, arguments.PlanRevision.RevisionID)
	if err != nil {
		return agentruntime.MediaAssemblyTimelineContent{}, err
	}
	plan, err := agentruntime.DecodeAssemblyPlanV2([]byte(revision.PayloadJSON))
	if err != nil {
		return agentruntime.MediaAssemblyTimelineContent{}, err
	}
	content := agentruntime.MediaAssemblyTimelineContent{
		ContentType: agentruntime.MediaAssemblyContentType, ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion,
		TaskID: task.ID, Stage: task.Stage, ClipCount: len(plan.Clips), AudioMode: plan.AudioMode,
		Output: plan.Output, PlanRevision: arguments.PlanRevision,
	}
	switch task.Status {
	case model.TaskStatusQueued:
		content.TaskStatus = agentruntime.MediaAssemblyTaskQueued
	case model.TaskStatusRunning:
		content.TaskStatus = agentruntime.MediaAssemblyTaskRunning
	case model.TaskStatusFailed:
		content.TaskStatus = agentruntime.MediaAssemblyTaskFailed
		content.ErrorCode = "media_assembly_failed"
	case model.TaskStatusCancelled:
		content.TaskStatus = agentruntime.MediaAssemblyTaskCancelled
		content.ErrorCode = "media_assembly_cancelled"
		if task.ResultJSON != "" {
			result, decodeErr := decodeMediaAssemblyTaskResult(task.ResultJSON)
			if decodeErr != nil || result.PlanDigest != input.PlanDigest {
				return agentruntime.MediaAssemblyTimelineContent{}, errors.New("agent media assembly cancelled result facts conflict")
			}
			content.Final = &agentruntime.MediaAssemblyFinal{
				ArtifactRevision: result.ArtifactRevision,
				ResourceID:       result.ResourceID,
				Adopted:          false,
			}
		}
	case model.TaskStatusSucceeded:
		content.TaskStatus = agentruntime.MediaAssemblyTaskSucceeded
		result, decodeErr := decodeMediaAssemblyTaskResult(task.ResultJSON)
		if decodeErr != nil || result.PlanDigest != input.PlanDigest {
			return agentruntime.MediaAssemblyTimelineContent{}, errors.New("agent media assembly result facts conflict")
		}
		adopted := false
		head, headErr := s.repo.ArtifactHeadRevisionForScope(scope, result.ArtifactRevision.ArtifactID)
		if headErr == nil {
			adopted = head.ID == result.ArtifactRevision.RevisionID
		} else if !errors.Is(headErr, gorm.ErrRecordNotFound) {
			return agentruntime.MediaAssemblyTimelineContent{}, headErr
		}
		content.Final = &agentruntime.MediaAssemblyFinal{
			ArtifactRevision: result.ArtifactRevision, ResourceID: result.ResourceID, Adopted: adopted,
		}
	default:
		return agentruntime.MediaAssemblyTimelineContent{}, errors.New("agent media assembly task status is invalid")
	}
	if err := content.Validate(); err != nil {
		return agentruntime.MediaAssemblyTimelineContent{}, err
	}
	return content, nil
}
