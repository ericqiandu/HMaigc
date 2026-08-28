package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

type agentProductionRenderResult struct {
	ArtifactID     string                              `json:"artifactId"`
	ArtifactKind   model.AgentProductionArtifactKind   `json:"artifactKind"`
	ArtifactStatus model.AgentProductionArtifactStatus `json:"artifactStatus"`
	Attempt        int                                 `json:"attempt"`
	TaskID         string                              `json:"taskId"`
	BillingOrderID string                              `json:"billingOrderId"`
	ResourceID     string                              `json:"resourceId"`
}

func (s *Service) coordinatePendingAgentProductionRender(
	scope agentruntime.Scope,
	state agentruntime.RuntimeState,
	call *agentruntime.ToolCallDecision,
	record *model.AgentToolCall,
) (*AgentRuntimeProgress, error) {
	arguments, err := decodeFrozenProductionRenderArguments(call.Arguments)
	if err != nil {
		return s.resolvePendingAgentToolFailure(scope, state, call, "production_render_invalid")
	}
	artifact, err := s.productionArtifactForRender(scope, arguments)
	if err != nil {
		return nil, err
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
			return s.productionRenderCompletedToolProgress(scope, state, call)
		}
		record, _, err = s.frozenAgentToolCall(scope, call, agentruntime.ToolCallRunning, state.Configuration.ExecutionMode)
		if err != nil {
			return nil, err
		}
	}

	task, order, err := s.ensureProductionArtifactTask(scope, record, arguments, *artifact)
	if err != nil {
		if errors.Is(err, errProductionPrerequisiteAssetMissing) {
			return s.failProductionRender(scope, state, call, *artifact, "production_prerequisite_missing", map[string]string{"reason": err.Error()})
		}
		if failureCode, ok := agentProductionRenderFailureCode(err); ok {
			return s.failProductionRender(scope, state, call, *artifact, failureCode, map[string]string{"reason": err.Error()})
		}
		return nil, err
	}
	artifact, err = s.bindProductionArtifactTask(scope, arguments, *artifact, *task, *order)
	if err != nil {
		return nil, err
	}

	switch task.Status {
	case model.TaskStatusQueued:
		return s.agentRuntimeProgressForCurrentState(scope, state)
	case model.TaskStatusRunning:
		if artifact.Status == model.AgentProductionArtifactQueued {
			artifact, err = s.repo.TransitionAgentProductionArtifact(scope, repository.ArtifactTransition{
				ArtifactID: artifact.ID, ExpectedStatus: artifact.Status, NextStatus: model.AgentProductionArtifactRunning,
				ExpectedAttempt: artifact.Attempt, NextAttempt: artifact.Attempt, Now: time.Now().UTC(),
			})
			if err != nil {
				return nil, err
			}
		}
		return s.agentRuntimeProgressForCurrentState(scope, state)
	case model.TaskStatusFailed:
		return s.failProductionRender(scope, state, call, *artifact, "production_generation_failed", productionGenerationFailureOutput(*task))
	case model.TaskStatusCancelled:
		return s.failProductionRender(scope, state, call, *artifact, "production_generation_cancelled", map[string]string{"taskId": task.ID})
	case model.TaskStatusSucceeded:
	default:
		return nil, errors.New("production render task status is invalid")
	}

	resourceID, err := taskResultResourceID(task.ResultJSON, artifact.Kind)
	if err != nil {
		return s.failProductionRender(scope, state, call, *artifact, "production_result_invalid", map[string]string{"taskId": task.ID, "reason": err.Error()})
	}
	resource, err := s.productionResourceForScope(scope, resourceID)
	if err != nil {
		return nil, err
	}
	expectedKind := "image"
	if artifact.Kind == model.AgentProductionArtifactVideoClip {
		expectedKind = "video"
	}
	if resource.Status != model.ResourceStatusReady || resource.Kind != expectedKind {
		return s.failProductionRender(scope, state, call, *artifact, "production_result_invalid", map[string]string{"taskId": task.ID, "reason": "production resource is not ready or has the wrong kind"})
	}
	if artifact.Status != model.AgentProductionArtifactSucceeded {
		artifact, err = s.repo.TransitionAgentProductionArtifact(scope, repository.ArtifactTransition{
			ArtifactID: artifact.ID, ExpectedStatus: artifact.Status, NextStatus: model.AgentProductionArtifactSucceeded,
			ExpectedAttempt: artifact.Attempt, NextAttempt: artifact.Attempt,
			TaskID: task.ID, BillingOrderID: order.ID, ResourceID: resource.ID, Now: time.Now().UTC(),
		})
		if err != nil {
			return nil, err
		}
	}
	output, err := json.Marshal(agentProductionRenderResult{
		ArtifactID: artifact.ID, ArtifactKind: artifact.Kind, ArtifactStatus: artifact.Status, Attempt: artifact.Attempt,
		TaskID: task.ID, BillingOrderID: order.ID, ResourceID: resource.ID,
	})
	if err != nil {
		return nil, err
	}
	latest, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	if !samePendingStartedTool(latest, call) {
		return s.productionRenderCompletedToolProgress(scope, latest, call)
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
	return s.productionRenderCompletedToolProgress(scope, progress.State, call)
}

func decodeFrozenProductionRenderArguments(raw json.RawMessage) (agentruntime.ProductionRenderArguments, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var arguments agentruntime.ProductionRenderArguments
	if err := decoder.Decode(&arguments); err != nil {
		return agentruntime.ProductionRenderArguments{}, errAgentRuntimeProductionRenderInput
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return agentruntime.ProductionRenderArguments{}, errAgentRuntimeProductionRenderInput
	}
	arguments.PlanKey = strings.TrimSpace(arguments.PlanKey)
	arguments.ArtifactID = strings.TrimSpace(arguments.ArtifactID)
	arguments.GenerationModel.ChannelID = strings.TrimSpace(arguments.GenerationModel.ChannelID)
	arguments.GenerationModel.Model = strings.TrimSpace(arguments.GenerationModel.Model)
	arguments.VideoInputMode = agentruntime.ProductionVideoInputMode(strings.TrimSpace(string(arguments.VideoInputMode)))
	arguments.VideoInputResourceID = strings.TrimSpace(arguments.VideoInputResourceID)
	arguments.BillingMode = strings.TrimSpace(arguments.BillingMode)
	arguments.QuoteFingerprint = strings.TrimSpace(arguments.QuoteFingerprint)
	arguments.QuoteID = strings.TrimSpace(arguments.QuoteID)
	arguments.ApprovalFingerprint = strings.TrimSpace(arguments.ApprovalFingerprint)
	arguments.TaskID = strings.TrimSpace(arguments.TaskID)
	arguments.BillingIdempotencyKey = strings.TrimSpace(arguments.BillingIdempotencyKey)
	arguments.ChannelModelID = strings.TrimSpace(arguments.ChannelModelID)
	arguments.Capability = normalizeCapability(arguments.Capability)
	arguments.TaskType = strings.TrimSpace(arguments.TaskType)
	arguments.Operation = strings.TrimSpace(arguments.Operation)
	arguments.Prompt = strings.TrimSpace(arguments.Prompt)
	arguments.ParametersJSON = strings.TrimSpace(arguments.ParametersJSON)
	arguments.ProviderCapabilitiesJSON = strings.TrimSpace(arguments.ProviderCapabilitiesJSON)
	if arguments.PlanKey == "" || arguments.PlanVersion < 1 || arguments.ArtifactID == "" || arguments.Attempt < 0 ||
		arguments.GenerationModel.ChannelID == "" || arguments.GenerationModel.Model == "" ||
		len(arguments.VideoInputResourceID) > 80 ||
		(arguments.ImageConfig == nil) == (arguments.VideoConfig == nil) || arguments.AmountMicrocredits < 0 ||
		arguments.PerTaskAmountMicrocredits < 0 || arguments.PriceVersion < 0 || arguments.Quantity < 1 ||
		arguments.BillingMode == "" || arguments.QuoteFingerprint == "" || arguments.QuoteID == "" ||
		arguments.ApprovalFingerprint == "" || arguments.TaskID == "" || arguments.BillingIdempotencyKey == "" ||
		arguments.ChannelModelID == "" || arguments.Capability == "" || arguments.TaskType == "" ||
		arguments.Operation == "" || arguments.Prompt == "" || arguments.ParametersJSON == "" ||
		arguments.ProviderCapabilitiesJSON == "" || arguments.ExpiresAt.IsZero() {
		return agentruntime.ProductionRenderArguments{}, errAgentRuntimeProductionRenderInput
	}
	if _, err := canonicalAgentJSON([]byte(arguments.ParametersJSON)); err != nil {
		return agentruntime.ProductionRenderArguments{}, errAgentRuntimeProductionRenderInput
	}
	if _, err := canonicalAgentJSON([]byte(arguments.ProviderCapabilitiesJSON)); err != nil {
		return agentruntime.ProductionRenderArguments{}, errAgentRuntimeProductionRenderInput
	}
	if arguments.VideoConfig != nil {
		arguments.VideoConfig.AspectRatio = strings.TrimSpace(arguments.VideoConfig.AspectRatio)
		if arguments.VideoConfig.AspectRatio == "" || !arguments.VideoInputMode.Valid() ||
			(arguments.VideoInputMode == agentruntime.ProductionVideoInputTextToVideo && arguments.VideoInputResourceID != "") ||
			(arguments.VideoInputMode == agentruntime.ProductionVideoInputStoryboard && arguments.VideoInputResourceID == "") {
			return agentruntime.ProductionRenderArguments{}, errAgentRuntimeProductionRenderInput
		}
	} else if arguments.VideoInputMode != "" || arguments.VideoInputResourceID != "" {
		return agentruntime.ProductionRenderArguments{}, errAgentRuntimeProductionRenderInput
	}
	return arguments, nil
}

func samePendingStartedTool(state agentruntime.RuntimeState, call *agentruntime.ToolCallDecision) bool {
	return state.Status == agentruntime.RunWaitingTool && state.PendingToolStarted && state.PendingToolCall != nil &&
		state.PendingToolCall.ToolCallID == call.ToolCallID && state.PendingToolCall.ActionVersion == call.ActionVersion
}

func (s *Service) failProductionRender(
	scope agentruntime.Scope,
	state agentruntime.RuntimeState,
	call *agentruntime.ToolCallDecision,
	artifact model.AgentProductionArtifact,
	failureCode string,
	failureOutput map[string]string,
) (*AgentRuntimeProgress, error) {
	if artifact.Status != model.AgentProductionArtifactFailed {
		if _, err := s.repo.TransitionAgentProductionArtifact(scope, repository.ArtifactTransition{
			ArtifactID: artifact.ID, ExpectedStatus: artifact.Status, NextStatus: model.AgentProductionArtifactFailed,
			ExpectedAttempt: artifact.Attempt, NextAttempt: artifact.Attempt, LastErrorCode: failureCode, Now: time.Now().UTC(),
		}); err != nil {
			return nil, err
		}
	}
	latest, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	if !samePendingStartedTool(latest, call) {
		return s.productionRenderCompletedToolProgress(scope, latest, call)
	}
	return s.resolvePendingAgentToolFailureWithOutput(scope, latest, call, failureCode, failureOutput)
}

func productionGenerationFailureOutput(task model.Task) map[string]string {
	output := map[string]string{"taskId": task.ID}
	if reason := strings.TrimSpace(task.Error); reason != "" {
		output["reason"] = reason
	}
	return output
}

func (s *Service) productionRenderCompletedToolProgress(scope agentruntime.Scope, state agentruntime.RuntimeState, call *agentruntime.ToolCallDecision) (*AgentRuntimeProgress, error) {
	record, err := s.repo.AgentToolCallForScope(scope, call.ToolCallID, call.ActionVersion)
	if err != nil {
		return nil, err
	}
	if record.Status != agentruntime.ToolCallSucceeded && record.Status != agentruntime.ToolCallFailed {
		return nil, errors.New("production render tool completion facts conflict")
	}
	return s.agentRuntimeProgressForCurrentState(scope, state)
}
