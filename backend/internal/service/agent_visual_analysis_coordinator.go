package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

func (s *Service) coordinatePendingAgentVisualAnalysis(
	scope agentruntime.Scope,
	state agentruntime.RuntimeState,
	call *agentruntime.ToolCallDecision,
	record *model.AgentToolCall,
) (*AgentRuntimeProgress, error) {
	arguments, err := decodeFrozenAgentVisualAnalysisArguments(call.Arguments)
	if err != nil {
		return s.resolvePendingAgentToolFailure(scope, state, call, "visual_analysis_invalid")
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
			return s.agentVisualAnalysisCompletedToolProgress(scope, state, call)
		}
		record, _, err = s.frozenAgentToolCall(scope, call, agentruntime.ToolCallRunning, state.Configuration.ExecutionMode)
		if err != nil {
			return nil, err
		}
	}

	task, _, err := s.ensureAgentVisualAnalysisTask(scope, record, arguments)
	if err != nil {
		return nil, err
	}
	switch task.Status {
	case model.TaskStatusQueued, model.TaskStatusRunning:
		return s.agentRuntimeProgressForCurrentState(scope, state)
	case model.TaskStatusFailed:
		return s.failAgentVisualAnalysis(scope, call, agentVisualAnalysisTaskFailureCode(*task), taskFailureOutput(*task))
	case model.TaskStatusCancelled:
		return s.failAgentVisualAnalysis(scope, call, "visual_analysis_cancelled", map[string]string{"taskId": task.ID})
	case model.TaskStatusSucceeded:
	default:
		return nil, errors.New("agent visual analysis task status is invalid")
	}

	result, err := decodeAgentVisualAnalysisTaskResult(task.ResultJSON)
	if err != nil {
		return s.failAgentVisualAnalysis(scope, call, "visual_analysis_result_invalid", map[string]string{
			"taskId": task.ID, "reason": err.Error(),
		})
	}
	if result.ArtifactID != arguments.OutputArtifactID || result.Schema != agentruntime.ArtifactSchemaVisualEvidenceV1 {
		return s.failAgentVisualAnalysis(scope, call, "visual_analysis_result_invalid", map[string]string{
			"taskId": task.ID, "reason": "visual analysis result facts conflict",
		})
	}
	revision, err := s.repo.ArtifactRevisionForArtifactInScope(scope, result.ArtifactID, result.ArtifactRevision)
	if err != nil {
		return nil, err
	}
	if revision.Revision != result.Revision {
		return s.failAgentVisualAnalysis(scope, call, "visual_analysis_result_invalid", map[string]string{
			"taskId": task.ID, "reason": "visual analysis revision facts conflict",
		})
	}
	if err := validateStoredAgentVisualEvidence(*revision, agentVisualAnalysisExecution(arguments)); err != nil {
		return s.failAgentVisualAnalysis(scope, call, "visual_analysis_result_invalid", map[string]string{
			"taskId": task.ID, "reason": err.Error(),
		})
	}
	output, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	latest, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	if !samePendingStartedTool(latest, call) {
		return s.agentVisualAnalysisCompletedToolProgress(scope, latest, call)
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
	return s.agentVisualAnalysisCompletedToolProgress(scope, progress.State, call)
}

func decodeAgentVisualAnalysisTaskResult(raw string) (agentVisualAnalysisTaskResult, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	var result agentVisualAnalysisTaskResult
	if err := decoder.Decode(&result); err != nil {
		return agentVisualAnalysisTaskResult{}, errAgentVisualArgumentsInvalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return agentVisualAnalysisTaskResult{}, errAgentVisualArgumentsInvalid
	}
	result.ArtifactID = strings.TrimSpace(result.ArtifactID)
	result.ArtifactRevision = strings.TrimSpace(result.ArtifactRevision)
	result.Schema = strings.TrimSpace(result.Schema)
	result.ProviderRequest = strings.TrimSpace(result.ProviderRequest)
	if result.ArtifactID == "" || result.ArtifactRevision == "" || result.Revision < 1 ||
		result.Schema != agentruntime.ArtifactSchemaVisualEvidenceV1 || len(result.ProviderRequest) > 512 {
		return agentVisualAnalysisTaskResult{}, errAgentVisualArgumentsInvalid
	}
	return result, nil
}

func taskFailureOutput(task model.Task) map[string]string {
	output := map[string]string{"taskId": task.ID}
	if reason := strings.TrimSpace(task.Error); reason != "" {
		output["reason"] = reason
	}
	return output
}

func agentVisualAnalysisTaskFailureCode(task model.Task) string {
	switch strings.TrimSpace(task.Error) {
	case errAgentVisualSourceRevisionStale.Error():
		return "visual_evidence_stale"
	case errAgentVisualInputUnavailable.Error():
		return "visual_analysis_input_unavailable"
	default:
		return "visual_analysis_failed"
	}
}

func (s *Service) failAgentVisualAnalysis(
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
		return s.agentVisualAnalysisCompletedToolProgress(scope, latest, call)
	}
	return s.resolvePendingAgentToolFailureWithOutput(scope, latest, call, failureCode, output)
}

func (s *Service) agentVisualAnalysisCompletedToolProgress(
	scope agentruntime.Scope,
	state agentruntime.RuntimeState,
	call *agentruntime.ToolCallDecision,
) (*AgentRuntimeProgress, error) {
	record, err := s.repo.AgentToolCallForScope(scope, call.ToolCallID, call.ActionVersion)
	if err != nil {
		return nil, err
	}
	if record.Status != agentruntime.ToolCallSucceeded && record.Status != agentruntime.ToolCallFailed {
		return nil, errors.New("agent visual analysis tool completion facts conflict")
	}
	return s.agentRuntimeProgressForCurrentState(scope, state)
}
