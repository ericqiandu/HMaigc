package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

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
	if _, err := decodeStoredAgentProductionPlan(*plan); err != nil {
		return nil, newAgentProductionRenderInputError("production_plan_invalid", err.Error())
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
	if request.ImageConfig != nil {
		request.ImageConfig.Size, err = productionImageRenderSize(request.ImageConfig.Size, request.ImageConfig.Resolution, callable.ProviderCapabilities)
		if err != nil {
			return nil, newAgentProductionRenderInputError("generation_parameter_unsupported", err.Error())
		}
	}
	videoInputMode, videoInputResourceID, err := s.freezeProductionVideoInputResource(scope, request, *artifact, callable)
	if err != nil {
		return nil, err
	}
	frozen := agentruntime.ProductionRenderArguments{
		PlanKey: request.PlanKey, PlanVersion: request.PlanVersion, ArtifactID: request.ArtifactID,
		Attempt: artifact.Attempt, GenerationModel: request.GenerationModel,
		VideoInputMode:       videoInputMode,
		VideoInputResourceID: videoInputResourceID,
		ImageConfig:          request.ImageConfig, VideoConfig: request.VideoConfig,
	}
	prompt, err := productionArtifactPrompt(*plan, *artifact)
	if err != nil {
		return nil, err
	}
	command, err := s.productionMediaGenerationCommand(scope, frozen, *artifact, prompt, callable.ProviderCapabilities)
	if err != nil {
		return nil, err
	}
	attempt, err := s.FreezeMediaQuote(scope, command, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	frozen.FrozenRenderQuote = frozenRenderQuoteFromMediaAttempt(*attempt)
	return json.Marshal(frozen)
}

func (s *Service) freezeProductionVideoInputResource(
	scope agentruntime.Scope,
	request agentProductionRenderRequest,
	artifact model.AgentProductionArtifact,
	callable agentRuntimeCallableModelFact,
) (agentruntime.ProductionVideoInputMode, string, error) {
	if artifact.Kind != model.AgentProductionArtifactVideoClip {
		return "", "", nil
	}
	resource, err := s.productionStoryboardResource(scope, agentruntime.ProductionRenderArguments{
		PlanKey: request.PlanKey, PlanVersion: request.PlanVersion,
	}, artifact)
	if err == nil {
		return agentruntime.ProductionVideoInputStoryboard, resource.ID, nil
	}
	if !errors.Is(err, errProductionPrerequisiteAssetMissing) {
		return "", "", err
	}
	if callable.ProviderCapabilities != nil && callable.ProviderCapabilities.SupportsTextToVideo {
		return agentruntime.ProductionVideoInputTextToVideo, "", nil
	}
	return "", "", newAgentProductionRenderInputError(
		"production_prerequisite_missing",
		"the selected video model requires a ready storyboard image for the same shot",
	)
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
	} else if kind != model.AgentProductionArtifactStoryboardImage && kind != model.AgentProductionArtifactReferenceImage {
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
	case model.AgentProductionArtifactReferenceImage, model.AgentProductionArtifactStoryboardImage:
		if request.ImageConfig == nil || callable.Capability != "image" {
			return newAgentProductionRenderInputError("generation_parameter_unsupported", "image capability is unavailable")
		}
		size := strings.TrimSpace(request.ImageConfig.Size)
		if size != "auto" && !containsString(capabilities.Ratios, size) {
			return newAgentProductionRenderInputError("generation_parameter_unsupported", "image size is not published by provider capabilities")
		}
		quality := strings.TrimSpace(request.ImageConfig.Quality)
		if (len(capabilities.Qualities) == 0 && quality != "") || (len(capabilities.Qualities) > 0 && !containsString(capabilities.Qualities, quality)) {
			return newAgentProductionRenderInputError("generation_parameter_unsupported", "image quality is not published by provider capabilities")
		}
		if !containsString(capabilities.Resolutions, request.ImageConfig.Resolution) {
			return newAgentProductionRenderInputError("generation_parameter_unsupported", "image resolution is not published by provider capabilities")
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
		if !containsString(capabilities.Ratios, request.VideoConfig.AspectRatio) {
			return newAgentProductionRenderInputError("generation_parameter_unsupported", "video aspect ratio is not published by provider capabilities")
		}
		if (capabilities.DurationMin > 0 && request.VideoConfig.DurationSeconds < capabilities.DurationMin) ||
			(capabilities.DurationMax > 0 && request.VideoConfig.DurationSeconds > capabilities.DurationMax) {
			return newAgentProductionRenderInputError("generation_parameter_unsupported", "video duration is outside provider capabilities")
		}
		if request.VideoConfig.GenerateAudio && !providerGeneratedAudioSupported(capabilities.SupportsGeneratedAudio, capabilities.GeneratedAudioResolutions, request.VideoConfig.Quality) {
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
		request.ImageConfig.Resolution = strings.TrimSpace(request.ImageConfig.Resolution)
		request.ImageConfig.Quality = strings.TrimSpace(request.ImageConfig.Quality)
		if request.ImageConfig.Size == "" || request.ImageConfig.Resolution == "" || request.ImageConfig.Count != 1 {
			return agentProductionRenderRequest{}, newAgentProductionRenderInputError("generation_parameter_unsupported", "image config is invalid")
		}
	}
	if request.VideoConfig != nil {
		request.VideoConfig.AspectRatio = strings.TrimSpace(request.VideoConfig.AspectRatio)
		request.VideoConfig.Quality = strings.TrimSpace(request.VideoConfig.Quality)
		if request.VideoConfig.DurationSeconds < 1 || request.VideoConfig.DurationSeconds > 30 || request.VideoConfig.AspectRatio == "" || request.VideoConfig.Quality == "" {
			return agentProductionRenderRequest{}, newAgentProductionRenderInputError("generation_parameter_unsupported", "video config is invalid")
		}
	}
	return request, nil
}

func productionImageRenderSize(ratio string, resolution string, capabilities *PublicProviderCapabilities) (string, error) {
	if capabilities == nil {
		return "", errors.New("image provider capabilities are unavailable")
	}
	parts := strings.Split(strings.TrimSpace(ratio), ":")
	if len(parts) != 2 {
		return "", errors.New("image aspect ratio is invalid")
	}
	ratioWidth, widthErr := strconv.ParseFloat(parts[0], 64)
	ratioHeight, heightErr := strconv.ParseFloat(parts[1], 64)
	if widthErr != nil || heightErr != nil || ratioWidth <= 0 || ratioHeight <= 0 {
		return "", errors.New("image aspect ratio is invalid")
	}
	normalizedResolution := strings.ToUpper(strings.TrimSpace(resolution))
	targetPixels := capabilities.ResolutionPixels[normalizedResolution]
	var width, height float64
	if targetPixels > 0 {
		aspectRatio := ratioWidth / ratioHeight
		width = math.Sqrt(float64(targetPixels) * aspectRatio)
		height = math.Sqrt(float64(targetPixels) / aspectRatio)
	} else {
		longestEdge := map[string]float64{"1K": 1824, "2K": 2048, "4K": 3840}[normalizedResolution]
		if longestEdge == 0 {
			return "", errors.New("image resolution is invalid")
		}
		if normalizedResolution == "1K" && ratioWidth == ratioHeight {
			longestEdge = 1024
		}
		shortestEdge := longestEdge * math.Min(ratioWidth, ratioHeight) / math.Max(ratioWidth, ratioHeight)
		if ratioWidth >= ratioHeight {
			width, height = longestEdge, shortestEdge
		} else {
			width, height = shortestEdge, longestEdge
		}
	}
	width, height = alignProductionImageDimension(width), alignProductionImageDimension(height)
	const maxPixels = 8_294_400
	if width*height > maxPixels {
		scale := math.Sqrt(maxPixels / (width * height))
		width = floorProductionImageDimension(width * scale)
		height = floorProductionImageDimension(height * scale)
	}
	return strconv.Itoa(int(width)) + "x" + strconv.Itoa(int(height)), nil
}

func alignProductionImageDimension(value float64) float64 {
	return math.Max(64, math.Round(value/16)*16)
}

func floorProductionImageDimension(value float64) float64 {
	return math.Max(64, math.Floor(value/16)*16)
}
