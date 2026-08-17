package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

const agentGenerationPromptLimit = 64 * 1024

type agentGenerationSubmitArguments struct {
	Type   string                 `json:"type"`
	Prompt string                 `json:"prompt"`
	Input  map[string]interface{} `json:"input"`
}

type agentGenerationSubmitResult struct {
	TaskID             string              `json:"taskId"`
	BillingOrderID     string              `json:"billingOrderId"`
	Status             model.TaskStatus    `json:"status"`
	Capability         string              `json:"capability"`
	Model              string              `json:"model"`
	AmountMicrocredits int64               `json:"amountMicrocredits"`
	BillingStatus      model.BillingStatus `json:"billingStatus"`
}

type agentGenerationWaitArguments struct {
	TaskID string `json:"taskId"`
}

type agentGenerationWaitResult struct {
	TaskID        string                          `json:"taskId"`
	Status        model.TaskStatus                `json:"status"`
	Stage         string                          `json:"stage"`
	Progress      int                             `json:"progress"`
	BillingStatus model.BillingStatus             `json:"billingStatus,omitempty"`
	Artifacts     []agentruntime.DeliveryArtifact `json:"artifacts"`
}

func decodeAgentGenerationSubmitArguments(raw json.RawMessage) (agentGenerationSubmitArguments, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var arguments agentGenerationSubmitArguments
	if err := decoder.Decode(&arguments); err != nil {
		return agentGenerationSubmitArguments{}, errors.New("agent generation submit arguments are invalid")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return agentGenerationSubmitArguments{}, errors.New("agent generation submit arguments are invalid")
	}
	arguments.Type = strings.TrimSpace(arguments.Type)
	arguments.Prompt = strings.TrimSpace(arguments.Prompt)
	capability := ""
	switch arguments.Type {
	case "canvas_image":
		capability = "image"
	case "canvas_video":
		capability = "video"
	case "canvas_audio":
		capability = "audio"
	default:
		return agentGenerationSubmitArguments{}, errors.New("agent generation task type is invalid")
	}
	if arguments.Prompt == "" || len([]rune(arguments.Prompt)) > agentGenerationPromptLimit || arguments.Input == nil || strings.TrimSpace(stringField(arguments.Input, "mode")) != capability {
		return agentGenerationSubmitArguments{}, errors.New("agent generation submit arguments are invalid")
	}
	return arguments, nil
}

func decodeAgentGenerationWaitArguments(raw json.RawMessage) (agentGenerationWaitArguments, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var arguments agentGenerationWaitArguments
	if err := decoder.Decode(&arguments); err != nil {
		return agentGenerationWaitArguments{}, errors.New("agent generation wait arguments are invalid")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return agentGenerationWaitArguments{}, errors.New("agent generation wait arguments are invalid")
	}
	arguments.TaskID = strings.TrimSpace(arguments.TaskID)
	if arguments.TaskID == "" || len(arguments.TaskID) > 80 {
		return agentGenerationWaitArguments{}, errors.New("agent generation wait arguments are invalid")
	}
	return arguments, nil
}

func agentGenerationIdentity(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:16])
}

func agentGenerationOperation(runID string) string {
	return "agent_runtime:" + strings.TrimSpace(runID)
}

func agentGenerationRunID(operation string) (string, bool) {
	const prefix = "agent_runtime:"
	operation = strings.TrimSpace(operation)
	if !strings.HasPrefix(operation, prefix) || len(operation) > 64 {
		return "", false
	}
	runID := strings.TrimSpace(strings.TrimPrefix(operation, prefix))
	return runID, runID != ""
}

func (s *Service) coordinatePendingAgentGenerationSubmit(
	scope agentruntime.Scope,
	state agentruntime.RuntimeState,
	call *agentruntime.ToolCallDecision,
	record *model.AgentToolCall,
) (*AgentRuntimeProgress, error) {
	arguments, err := decodeAgentGenerationSubmitArguments(call.Arguments)
	if err != nil {
		return s.resolvePendingAgentToolFailure(scope, state, call, "generation_request_invalid")
	}
	if err := validateAgentGenerationSubmitContract(state.Configuration, arguments); err != nil {
		return s.resolvePendingAgentToolFailureWithOutput(scope, state, call, "generation_request_invalid", map[string]string{"reason": err.Error()})
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
		if state.Status != agentruntime.RunWaitingTool || state.PendingToolCall == nil ||
			state.PendingToolCall.ToolCallID != call.ToolCallID || state.PendingToolCall.ActionVersion != call.ActionVersion || !state.PendingToolStarted {
			return s.agentGenerationCompletedToolProgress(scope, state, call)
		}
		record, _, err = s.frozenAgentToolCall(scope, call, agentruntime.ToolCallRunning, state.Configuration.ExecutionMode)
		if err != nil {
			return nil, err
		}
	}
	task, err := s.createTaskWithIdentity(scope.ActorUserID, CreateTaskRequest{
		ProjectID: scope.CanvasID,
		Type:      arguments.Type,
		Operation: agentGenerationOperation(scope.RunID),
		Prompt:    arguments.Prompt,
		Input:     arguments.Input,
	}, taskCreationIdentity{
		TaskID:                 agentGenerationIdentity(record.IdempotencyKey),
		BillingIdempotencyKey:  "agent-generation:" + agentGenerationIdentity(record.IdempotencyKey),
		UseCurrentBillingQuote: true,
	})
	if err != nil {
		var authErr *AuthError
		if errors.As(err, &authErr) {
			failureCode := "generation_submit_rejected"
			if authErr.Status == http.StatusServiceUnavailable {
				failureCode = "generation_unavailable"
			}
			reason := strings.TrimSpace(authErr.Message)
			if len([]rune(reason)) > 240 {
				reason = string([]rune(reason)[:240])
			}
			return s.resolvePendingAgentToolFailureWithOutput(scope, state, call, failureCode, map[string]string{"reason": reason})
		}
		return nil, err
	}
	order, err := s.repo.BillingOrder(task.BillingOrderID)
	if err != nil {
		return nil, err
	}
	output, err := json.Marshal(agentGenerationSubmitResult{
		TaskID: task.ID, BillingOrderID: task.BillingOrderID, Status: task.Status,
		Capability: task.Capability, Model: task.Model, AmountMicrocredits: order.AmountMicrocredits, BillingStatus: order.Status,
	})
	if err != nil {
		return nil, err
	}
	latest, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	if latest.Status != agentruntime.RunWaitingTool || latest.PendingToolCall == nil ||
		latest.PendingToolCall.ToolCallID != call.ToolCallID || latest.PendingToolCall.ActionVersion != call.ActionVersion || !latest.PendingToolStarted {
		return s.agentGenerationCompletedToolProgress(scope, latest, call)
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
	return s.agentGenerationCompletedToolProgress(scope, progress.State, call)
}

func validateAgentGenerationSubmitContract(configuration agentruntime.RunConfiguration, arguments agentGenerationSubmitArguments) error {
	config, ok := arguments.Input["config"].(map[string]interface{})
	if !ok {
		return errors.New("generation config must be an object")
	}
	channelID, channelOK := config["channelId"].(string)
	modelKey, modelOK := config["model"].(string)
	channelID = strings.TrimSpace(channelID)
	modelKey = strings.TrimSpace(modelKey)
	if !channelOK || !modelOK || channelID == "" || modelKey == "" {
		return errors.New("generation config requires channelId and model")
	}
	switch arguments.Type {
	case "canvas_image":
		selection := configuration.GenerationModels.Image
		if selection == nil || channelID != selection.ChannelID || modelKey != selection.Model {
			return errors.New("image generation must use the frozen selected model")
		}
		allowed := map[string]struct{}{
			"channelId": {}, "model": {}, "size": {}, "quality": {}, "count": {}, "transparentBackground": {},
		}
		for field := range config {
			if _, exists := allowed[field]; !exists {
				return errors.New("image generation config contains an unsupported field")
			}
		}
		for _, field := range []string{"size", "count"} {
			value, valid := config[field].(string)
			if !valid || strings.TrimSpace(value) == "" {
				return errors.New("image generation config requires size and count strings")
			}
		}
		if value, exists := config["quality"]; exists {
			quality, valid := value.(string)
			if !valid || strings.TrimSpace(quality) == "" {
				return errors.New("image generation config quality must be a non-empty string when provided")
			}
		}
	case "canvas_video":
		selection := configuration.GenerationModels.Video
		if selection == nil || channelID != selection.ChannelID || modelKey != selection.Model {
			return errors.New("video generation must use the frozen selected model")
		}
	case "canvas_audio":
		// Audio has no composer-level frozen selection yet; its formal task
		// contract remains the authoritative validator.
	default:
		return errors.New("generation task type is invalid")
	}
	return nil
}

func (s *Service) agentGenerationCompletedToolProgress(scope agentruntime.Scope, state agentruntime.RuntimeState, call *agentruntime.ToolCallDecision) (*AgentRuntimeProgress, error) {
	completed, err := s.repo.AgentToolCallForScope(scope, call.ToolCallID, call.ActionVersion)
	if err != nil {
		return nil, err
	}
	if completed.Status != agentruntime.ToolCallSucceeded && completed.Status != agentruntime.ToolCallFailed {
		return nil, errors.New("agent generation tool completion facts conflict")
	}
	return s.agentRuntimeProgressForCurrentState(scope, state)
}

func (s *Service) coordinatePendingAgentGenerationWait(
	scope agentruntime.Scope,
	state agentruntime.RuntimeState,
	call *agentruntime.ToolCallDecision,
) (*AgentRuntimeProgress, error) {
	arguments, err := decodeAgentGenerationWaitArguments(call.Arguments)
	if err != nil {
		return s.resolvePendingAgentToolFailure(scope, state, call, "generation_wait_invalid")
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
		if state.Status != agentruntime.RunWaitingTool || state.PendingToolCall == nil ||
			state.PendingToolCall.ToolCallID != call.ToolCallID || state.PendingToolCall.ActionVersion != call.ActionVersion || !state.PendingToolStarted {
			return s.agentGenerationCompletedToolProgress(scope, state, call)
		}
	}
	task, err := s.repo.TaskForUser(scope.ActorUserID, arguments.TaskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return s.resolvePendingAgentToolFailure(scope, state, call, "generation_task_not_found")
		}
		return nil, err
	}
	if task.ProjectID != scope.CanvasID || task.Operation != agentGenerationOperation(scope.RunID) {
		return s.resolvePendingAgentToolFailure(scope, state, call, "generation_task_scope_conflict")
	}
	switch task.Status {
	case model.TaskStatusQueued, model.TaskStatusRunning:
		return s.agentRuntimeProgressForCurrentState(scope, state)
	case model.TaskStatusFailed:
		return s.resolvePendingAgentToolFailure(scope, state, call, "generation_failed")
	case model.TaskStatusCancelled:
		return s.resolvePendingAgentToolFailure(scope, state, call, "generation_cancelled")
	case model.TaskStatusSucceeded:
	default:
		return nil, errors.New("agent generation task status is invalid")
	}
	artifactKind := agentruntime.ArtifactKind(task.Capability)
	if !artifactKind.Valid() || artifactKind == agentruntime.ArtifactText || artifactKind == agentruntime.ArtifactCanvasRevision {
		return s.resolvePendingAgentToolFailure(scope, state, call, "generation_result_invalid")
	}
	artifactURL, _ := taskMediaPreview(task.ResultJSON, task.Type)
	if strings.TrimSpace(artifactURL) == "" {
		return s.resolvePendingAgentToolFailure(scope, state, call, "generation_result_invalid")
	}
	billingStatus := model.BillingStatus("")
	if task.BillingOrderID != "" {
		order, orderErr := s.repo.BillingOrder(task.BillingOrderID)
		if orderErr != nil {
			return nil, orderErr
		}
		if order.UserID != task.UserID || order.TaskID != task.ID {
			return nil, errors.New("agent generation billing facts conflict")
		}
		billingStatus = order.Status
	}
	output, err := json.Marshal(agentGenerationWaitResult{
		TaskID: task.ID, Status: task.Status, Stage: task.Stage, Progress: task.Progress, BillingStatus: billingStatus,
		Artifacts: []agentruntime.DeliveryArtifact{{Kind: artifactKind, URL: artifactURL}},
	})
	if err != nil {
		return nil, err
	}
	latest, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	if latest.Status != agentruntime.RunWaitingTool || latest.PendingToolCall == nil ||
		latest.PendingToolCall.ToolCallID != call.ToolCallID || latest.PendingToolCall.ActionVersion != call.ActionVersion || !latest.PendingToolStarted {
		return s.agentGenerationCompletedToolProgress(scope, latest, call)
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
	return s.agentGenerationCompletedToolProgress(scope, progress.State, call)
}
