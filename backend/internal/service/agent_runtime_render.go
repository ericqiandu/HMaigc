package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

var errAgentRuntimeProductionRenderInput = errors.New("agent production render arguments are invalid")

type agentProductionRenderInputError struct {
	code   string
	reason string
	class  agentruntime.ToolFailureClass
}

func (err *agentProductionRenderInputError) Error() string {
	return errAgentRuntimeProductionRenderInput.Error() + ": " + err.reason
}

func (err *agentProductionRenderInputError) Unwrap() error {
	return errAgentRuntimeProductionRenderInput
}

func newAgentProductionRenderInputError(code string, reason string) error {
	return &agentProductionRenderInputError{code: code, reason: reason, class: agentruntime.ToolFailureAgentRepairable}
}

func newTerminalAgentProductionRenderInputError(code string, reason string) error {
	return &agentProductionRenderInputError{code: code, reason: reason, class: agentruntime.ToolFailureTerminal}
}

func agentProductionRenderFailureCode(err error) (string, bool) {
	code, _, ok := agentProductionRenderFailureDetails(err)
	return code, ok
}

func agentProductionRenderFailureDetails(err error) (string, agentruntime.ToolFailureClass, bool) {
	var inputErr *agentProductionRenderInputError
	if !errors.As(err, &inputErr) {
		return "", "", false
	}
	return inputErr.code, inputErr.class, true
}

type agentProductionRenderRequest struct {
	PlanKey         string                                `json:"planKey"`
	PlanVersion     int                                   `json:"planVersion"`
	ArtifactID      string                                `json:"artifactId"`
	GenerationModel agentruntime.GenerationModelSelection `json:"generationModel"`
	ImageConfig     *agentruntime.ImageRenderConfig       `json:"imageConfig,omitempty"`
	VideoConfig     *agentruntime.VideoRenderConfig       `json:"videoConfig,omitempty"`
}

func (s *Service) freezeAgentProductionRenderArguments(scope agentruntime.Scope, callableModels []agentRuntimeCallableModelFact, raw json.RawMessage) (json.RawMessage, error) {
	request, err := decodeAgentProductionRenderRequest(raw)
	if err != nil {
		return nil, err
	}
	plan, err := s.repo.AgentProductionPlanVersionForScope(scope, request.PlanKey, request.PlanVersion)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, newAgentProductionRenderInputError("production_plan_version_conflict", "plan version is unavailable")
		}
		return nil, err
	}
	if plan.Status != model.AgentProductionPlanActive {
		return nil, newAgentProductionRenderInputError("production_plan_version_conflict", "plan version is not active")
	}
	artifacts, err := s.repo.AgentProductionArtifactsForVersion(scope, request.PlanKey, request.PlanVersion)
	if err != nil {
		return nil, err
	}
	var artifact *model.AgentProductionArtifact
	for index := range artifacts {
		if artifacts[index].ID == request.ArtifactID {
			artifact = &artifacts[index]
			break
		}
	}
	if artifact == nil || (artifact.Status != model.AgentProductionArtifactPlanned && artifact.Status != model.AgentProductionArtifactFailed) {
		return nil, newAgentProductionRenderInputError("production_artifact_conflict", "artifact is unavailable in a renderable state")
	}
	if artifact.Status == model.AgentProductionArtifactFailed {
		if err := s.validatePreviousProductionAttemptForRetry(scope, *artifact); err != nil {
			return nil, err
		}
	}
	callable, err := productionRenderCallableModel(callableModels, request.GenerationModel, artifact.Kind)
	if err != nil {
		return nil, err
	}
	if err := validateProductionRenderCapabilities(request, *artifact, callable); err != nil {
		return nil, err
	}
	quoteRequest, err := productionRenderQuoteRequest(request, *artifact)
	if err != nil {
		return nil, err
	}
	quote, err := s.QuoteTaskBilling(scope.ActorUserID, quoteRequest)
	if err != nil {
		return nil, err
	}
	frozen := agentruntime.ProductionRenderArguments{
		PlanKey: request.PlanKey, PlanVersion: request.PlanVersion, ArtifactID: request.ArtifactID,
		Attempt: artifact.Attempt, GenerationModel: request.GenerationModel,
		ImageConfig: request.ImageConfig, VideoConfig: request.VideoConfig,
		FrozenRenderQuote: agentruntime.FrozenRenderQuote{
			AmountMicrocredits: quote.AmountMicrocredits, PerTaskAmountMicrocredits: quote.PerTaskAmountMicrocredits,
			PriceVersion: quote.PriceVersion, BillingMode: quote.BillingMode,
			PricingResolution: quote.PricingResolution, PricingInputVariant: quote.PricingInputVariant,
			Quantity: quote.Quantity, QuoteFingerprint: quote.QuoteFingerprint,
		},
	}
	return json.Marshal(frozen)
}

func (s *Service) validatePreviousProductionAttemptForRetry(scope agentruntime.Scope, artifact model.AgentProductionArtifact) error {
	if artifact.TaskID == "" && artifact.BillingOrderID == "" && artifact.ResourceID == "" {
		return nil
	}
	if artifact.TaskID == "" || artifact.BillingOrderID == "" || artifact.ResourceID != "" {
		return newAgentProductionRenderInputError("production_artifact_conflict", "previous production attempt facts are incomplete or already contain a resource")
	}
	task, err := s.repo.TaskForUser(scope.ActorUserID, artifact.TaskID)
	if err != nil {
		return err
	}
	order, err := s.repo.BillingOrder(artifact.BillingOrderID)
	if err != nil {
		return err
	}
	if task.BillingOrderID != order.ID || order.UserID != scope.ActorUserID || order.TaskID != task.ID {
		return newAgentProductionRenderInputError("production_artifact_conflict", "previous production attempt commercial facts conflict")
	}
	if order.Status == model.BillingStatusReserved || order.Status == model.BillingStatusRunning || order.Status == model.BillingStatusUncertain {
		return newTerminalAgentProductionRenderInputError("production_previous_billing_unresolved", "previous production attempt billing is not resolved")
	}
	if order.Status != model.BillingStatusRefunded || (task.Status != model.TaskStatusFailed && task.Status != model.TaskStatusCancelled) {
		return newAgentProductionRenderInputError("production_artifact_conflict", "previous production attempt is not retryable")
	}
	return nil
}

func productionRenderCallableModel(models []agentRuntimeCallableModelFact, selection agentruntime.GenerationModelSelection, kind model.AgentProductionArtifactKind) (agentRuntimeCallableModelFact, error) {
	capability := "image"
	if kind == model.AgentProductionArtifactVideoClip {
		capability = "video"
	} else if kind != model.AgentProductionArtifactStoryboardImage {
		return agentRuntimeCallableModelFact{}, newAgentProductionRenderInputError("production_render_invalid", "artifact kind is unsupported")
	}
	for _, item := range models {
		if item.ChannelID == selection.ChannelID && item.Model == selection.Model && item.Capability == capability {
			return item, nil
		}
	}
	return agentRuntimeCallableModelFact{}, newAgentProductionRenderInputError("generation_model_not_callable", "selected generation model is not in the frozen callable model set")
}

func validateProductionRenderCapabilities(request agentProductionRenderRequest, artifact model.AgentProductionArtifact, callable agentRuntimeCallableModelFact) error {
	capabilities := callable.ProviderCapabilities
	if capabilities == nil || capabilities.ModelKey != request.GenerationModel.Model || capabilities.Capability != callable.Capability {
		return newAgentProductionRenderInputError("generation_parameter_unsupported", "provider capabilities are unavailable for the selected model")
	}
	containsString := func(values []string, value string) bool {
		for _, candidate := range values {
			if strings.EqualFold(strings.TrimSpace(candidate), strings.TrimSpace(value)) {
				return true
			}
		}
		return false
	}
	containsInt := func(values []int, value int) bool {
		for _, candidate := range values {
			if candidate == value {
				return true
			}
		}
		return false
	}
	switch artifact.Kind {
	case model.AgentProductionArtifactStoryboardImage:
		if request.ImageConfig == nil || callable.Capability != "image" {
			return newAgentProductionRenderInputError("generation_parameter_unsupported", "image capability is unavailable")
		}
		size := strings.TrimSpace(request.ImageConfig.Size)
		if size != "auto" && !containsString(capabilities.Ratios, size) {
			return newAgentProductionRenderInputError("generation_parameter_unsupported", "image size is not published by provider capabilities")
		}
		quality := strings.TrimSpace(request.ImageConfig.Quality)
		if quality != "" && !strings.EqualFold(quality, "auto") && !containsString(capabilities.Qualities, quality) {
			return newAgentProductionRenderInputError("generation_parameter_unsupported", "image quality is not published by provider capabilities")
		}
		if !containsInt(capabilities.OutputCounts, request.ImageConfig.Count) {
			return newAgentProductionRenderInputError("generation_parameter_unsupported", "image count is not published by provider capabilities")
		}
	case model.AgentProductionArtifactVideoClip:
		if request.VideoConfig == nil || callable.Capability != "video" {
			return newAgentProductionRenderInputError("generation_parameter_unsupported", "video capability is unavailable")
		}
		if !containsString(capabilities.Resolutions, request.VideoConfig.Quality) {
			return newAgentProductionRenderInputError("generation_parameter_unsupported", "video quality is not published by provider capabilities")
		}
		if (capabilities.DurationMin > 0 && request.VideoConfig.DurationSeconds < capabilities.DurationMin) ||
			(capabilities.DurationMax > 0 && request.VideoConfig.DurationSeconds > capabilities.DurationMax) {
			return newAgentProductionRenderInputError("generation_parameter_unsupported", "video duration is outside provider capabilities")
		}
		if request.VideoConfig.GenerateAudio && !capabilities.SupportsGeneratedAudio {
			return newAgentProductionRenderInputError("generation_parameter_unsupported", "generated audio is not supported by provider capabilities")
		}
	default:
		return newAgentProductionRenderInputError("production_render_invalid", "artifact kind is unsupported")
	}
	return nil
}

func decodeAgentProductionRenderRequest(raw json.RawMessage) (agentProductionRenderRequest, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var request agentProductionRenderRequest
	if err := decoder.Decode(&request); err != nil {
		return agentProductionRenderRequest{}, newAgentProductionRenderInputError("production_render_invalid", "arguments json is invalid")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return agentProductionRenderRequest{}, newAgentProductionRenderInputError("production_render_invalid", "arguments contain trailing json")
	}
	request.PlanKey = strings.TrimSpace(request.PlanKey)
	request.ArtifactID = strings.TrimSpace(request.ArtifactID)
	request.GenerationModel.ChannelID = strings.TrimSpace(request.GenerationModel.ChannelID)
	request.GenerationModel.Model = strings.TrimSpace(request.GenerationModel.Model)
	if request.PlanKey == "" || len(request.PlanKey) > 120 || request.PlanVersion < 1 || request.ArtifactID == "" || len(request.ArtifactID) > 80 ||
		request.GenerationModel.ChannelID == "" || request.GenerationModel.Model == "" || (request.ImageConfig == nil) == (request.VideoConfig == nil) {
		return agentProductionRenderRequest{}, newAgentProductionRenderInputError("production_render_invalid", "identity or generation config is invalid")
	}
	if request.ImageConfig != nil {
		request.ImageConfig.Size = strings.TrimSpace(request.ImageConfig.Size)
		request.ImageConfig.Quality = strings.TrimSpace(request.ImageConfig.Quality)
		if request.ImageConfig.Size == "" || request.ImageConfig.Count != 1 {
			return agentProductionRenderRequest{}, newAgentProductionRenderInputError("generation_parameter_unsupported", "image config is invalid")
		}
	}
	if request.VideoConfig != nil {
		request.VideoConfig.Quality = strings.TrimSpace(request.VideoConfig.Quality)
		if request.VideoConfig.DurationSeconds < 1 || request.VideoConfig.DurationSeconds > 30 || request.VideoConfig.Quality == "" {
			return agentProductionRenderRequest{}, newAgentProductionRenderInputError("generation_parameter_unsupported", "video config is invalid")
		}
	}
	return request, nil
}

func productionRenderQuoteRequest(request agentProductionRenderRequest, artifact model.AgentProductionArtifact) (TaskBillingQuoteRequest, error) {
	taskType := "canvas_image"
	mode := "image"
	config := TaskBillingQuoteConfig{ChannelID: request.GenerationModel.ChannelID, Model: request.GenerationModel.Model}
	switch artifact.Kind {
	case model.AgentProductionArtifactStoryboardImage:
		if request.ImageConfig == nil || request.VideoConfig != nil {
			return TaskBillingQuoteRequest{}, newAgentProductionRenderInputError("production_render_invalid", "storyboard config is invalid")
		}
		config.Size = request.ImageConfig.Size
		config.Quality = request.ImageConfig.Quality
	case model.AgentProductionArtifactVideoClip:
		taskType = "canvas_video"
		mode = "video"
		if request.VideoConfig == nil || request.ImageConfig != nil {
			return TaskBillingQuoteRequest{}, newAgentProductionRenderInputError("production_render_invalid", "video config is invalid")
		}
		config.VideoSeconds = strconv.Itoa(request.VideoConfig.DurationSeconds)
		config.VideoQuality = request.VideoConfig.Quality
	default:
		return TaskBillingQuoteRequest{}, newAgentProductionRenderInputError("production_render_invalid", "artifact kind is unsupported")
	}
	return TaskBillingQuoteRequest{
		Type: taskType, Operation: "production_render", BatchCount: 1,
		Input: TaskBillingQuoteInput{Mode: mode, Config: config},
	}, nil
}
