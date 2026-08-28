package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

var errProductionPrerequisiteAssetMissing = errors.New("production prerequisite asset is missing")

const productionResultMaterializing = "production_result_materializing"

func (s *Service) productionArtifactForRender(scope agentruntime.Scope, arguments agentruntime.ProductionRenderArguments) (*model.AgentProductionArtifact, error) {
	artifacts, err := s.repo.AgentProductionArtifactsForVersion(scope, arguments.PlanKey, arguments.PlanVersion)
	if err != nil {
		return nil, err
	}
	for index := range artifacts {
		artifact := &artifacts[index]
		if artifact.ID != arguments.ArtifactID {
			continue
		}
		if artifact.Attempt < arguments.Attempt || artifact.Attempt > arguments.Attempt+1 {
			return nil, repository.ErrAgentProductionArtifactConflict
		}
		if artifact.Kind != model.AgentProductionArtifactReferenceImage && artifact.Kind != model.AgentProductionArtifactStoryboardImage && artifact.Kind != model.AgentProductionArtifactVideoClip {
			return nil, errAgentRuntimeProductionRenderInput
		}
		return artifact, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (s *Service) ensureProductionArtifactTask(
	scope agentruntime.Scope,
	record *model.AgentToolCall,
	arguments agentruntime.ProductionRenderArguments,
	artifact model.AgentProductionArtifact,
) (*model.Task, *model.BillingOrder, error) {
	if artifact.TaskID != "" && artifact.Attempt == arguments.Attempt {
		if err := s.validatePreviousProductionAttemptForRetry(scope, artifact); err != nil {
			return nil, nil, err
		}
		return nil, nil, newAgentProductionRenderInputError("production_artifact_conflict", "retry approval did not detach the previous production attempt")
	}
	if record == nil || record.ApprovalDecision != agentruntime.ToolApprovalApproved || record.ApprovalDecidedAt == nil {
		return nil, nil, newTerminalAgentProductionRenderInputError("production_approval_invalid", "production media cost approval is missing")
	}
	plan, err := s.repo.AgentProductionPlanVersionForScope(scope, arguments.PlanKey, arguments.PlanVersion)
	if err != nil {
		return nil, nil, err
	}
	prompt, err := productionArtifactPrompt(*plan, artifact)
	if err != nil {
		return nil, nil, err
	}
	capabilities, err := s.currentProductionRenderCapabilities(arguments.GenerationModel)
	if err != nil {
		return nil, nil, err
	}
	command, err := s.productionMediaGenerationCommand(scope, arguments, artifact, prompt, capabilities)
	if err != nil {
		return nil, nil, err
	}
	approved, err := s.ApproveMediaAttempt(scope, mediaAttemptFromFrozenRender(arguments), command, record.ApprovalDecidedAt.UTC())
	if err != nil {
		if errors.Is(err, ErrCostApprovalQuoteMismatch) {
			return nil, nil, newTerminalAgentProductionRenderInputError("production_quote_mismatch", err.Error())
		}
		return nil, nil, err
	}
	task, order, err := s.EnsureMediaTask(context.Background(), scope, *approved)
	if err != nil {
		if errors.Is(err, ErrCostApprovalQuoteMismatch) {
			return nil, nil, newTerminalAgentProductionRenderInputError("production_quote_mismatch", err.Error())
		}
		return nil, nil, err
	}
	return task, order, nil
}

func (s *Service) productionMediaGenerationCommand(
	scope agentruntime.Scope,
	arguments agentruntime.ProductionRenderArguments,
	artifact model.AgentProductionArtifact,
	prompt string,
	capabilities *PublicProviderCapabilities,
) (MediaGenerationCommand, error) {
	input, taskType, err := s.productionRenderTaskInput(scope, arguments, artifact, prompt)
	if err != nil {
		return MediaGenerationCommand{}, err
	}
	parameters, err := json.Marshal(input)
	if err != nil {
		return MediaGenerationCommand{}, err
	}
	capability := capabilityFromTaskType(taskType)
	quantity := int64(1)
	if capability == "video" {
		quantity = int64(arguments.VideoConfig.DurationSeconds)
	}
	return MediaGenerationCommand{
		ArtifactRevisionID:   artifact.ID,
		Attempt:              arguments.Attempt + 1,
		TaskType:             taskType,
		Operation:            "production_render:" + scope.RunID,
		Prompt:               prompt,
		Capability:           capability,
		ChannelID:            arguments.GenerationModel.ChannelID,
		ModelKey:             arguments.GenerationModel.Model,
		ParametersJSON:       string(parameters),
		Quantity:             quantity,
		ProviderCapabilities: capabilities,
	}, nil
}

func (s *Service) currentProductionRenderCapabilities(selection agentruntime.GenerationModelSelection) (*PublicProviderCapabilities, error) {
	channel, err := s.repo.SystemChannel(selection.ChannelID)
	if err != nil {
		return nil, err
	}
	capabilities := publicProviderModelCapabilities(channel.InterfaceType, selection.Model)
	if capabilities == nil {
		return nil, newAgentProductionRenderInputError("generation_parameter_unsupported", "provider capabilities are unavailable for the selected model")
	}
	return capabilities, nil
}

func frozenRenderQuoteFromMediaAttempt(attempt MediaGenerationAttempt) agentruntime.FrozenRenderQuote {
	return agentruntime.FrozenRenderQuote{
		AmountMicrocredits: attempt.AmountMicrocredits, PerTaskAmountMicrocredits: attempt.PerTaskAmountMicrocredits,
		PriceVersion: attempt.PriceVersion, BillingMode: attempt.BillingMode,
		PricingResolution: attempt.PricingResolution, PricingInputVariant: attempt.PricingInputVariant,
		Quantity: attempt.Quantity, QuoteFingerprint: attempt.BillingQuoteFingerprint,
		QuoteID: attempt.QuoteID, ApprovalFingerprint: attempt.ApprovalFingerprint,
		TaskID: attempt.TaskID, BillingIdempotencyKey: attempt.BillingIdempotencyKey,
		ChannelModelID: attempt.ChannelModelID, Capability: attempt.Capability,
		TaskType: attempt.TaskType, Operation: attempt.Operation, Prompt: attempt.Prompt,
		ParametersJSON: attempt.ParametersJSON, ProviderCapabilitiesJSON: attempt.ProviderCapabilitiesJSON,
		ExpiresAt: attempt.ExpiresAt,
	}
}

func mediaAttemptFromFrozenRender(arguments agentruntime.ProductionRenderArguments) MediaGenerationAttempt {
	return MediaGenerationAttempt{
		ArtifactRevisionID: arguments.ArtifactID, Attempt: arguments.Attempt + 1,
		TaskID: arguments.TaskID, BillingIdempotencyKey: arguments.BillingIdempotencyKey,
		TaskType: arguments.TaskType, Operation: arguments.Operation, Prompt: arguments.Prompt,
		Capability: arguments.Capability, ChannelID: arguments.GenerationModel.ChannelID,
		ChannelModelID: arguments.ChannelModelID, ModelKey: arguments.GenerationModel.Model,
		ParametersJSON: arguments.ParametersJSON, ProviderCapabilitiesJSON: arguments.ProviderCapabilitiesJSON,
		Quantity: arguments.Quantity, AmountMicrocredits: arguments.AmountMicrocredits,
		PerTaskAmountMicrocredits: arguments.PerTaskAmountMicrocredits, PriceVersion: arguments.PriceVersion,
		BillingMode: arguments.BillingMode, PricingResolution: arguments.PricingResolution,
		PricingInputVariant:     arguments.PricingInputVariant,
		BillingQuoteFingerprint: arguments.QuoteFingerprint,
		QuoteID:                 arguments.QuoteID, ApprovalFingerprint: arguments.ApprovalFingerprint,
		ExpiresAt: arguments.ExpiresAt,
	}
}

func productionArtifactPrompt(plan model.AgentProductionPlanVersion, artifact model.AgentProductionArtifact) (string, error) {
	if artifact.Kind == model.AgentProductionArtifactReferenceImage {
		var references []agentruntime.ReferenceAssetDraft
		if err := json.Unmarshal([]byte(plan.ReferencesJSON), &references); err != nil {
			return "", err
		}
		for _, reference := range references {
			if reference.ReferenceKey == artifact.ReferenceKey {
				return strings.TrimSpace(reference.ImagePrompt), nil
			}
		}
		return "", errors.New("production reference artifact is missing")
	}
	var shots []agentruntime.ShotPlanDraft
	if err := json.Unmarshal([]byte(plan.ShotsJSON), &shots); err != nil {
		return "", err
	}
	for _, shot := range shots {
		if shot.ShotKey != artifact.ShotKey {
			continue
		}
		if artifact.Kind == model.AgentProductionArtifactStoryboardImage {
			return strings.TrimSpace(shot.ImagePrompt), nil
		}
		return strings.TrimSpace(shot.VideoPrompt), nil
	}
	return "", errors.New("production artifact shot is missing")
}

func (s *Service) productionRenderTaskInput(
	scope agentruntime.Scope,
	arguments agentruntime.ProductionRenderArguments,
	artifact model.AgentProductionArtifact,
	prompt string,
) (canvasGenerationInput, string, error) {
	config := providerConfig{ChannelID: arguments.GenerationModel.ChannelID, Model: arguments.GenerationModel.Model}
	input := canvasGenerationInput{Prompt: prompt, Config: config}
	if artifact.Kind == model.AgentProductionArtifactReferenceImage || artifact.Kind == model.AgentProductionArtifactStoryboardImage {
		if arguments.ImageConfig == nil {
			return canvasGenerationInput{}, "", errAgentRuntimeProductionRenderInput
		}
		input.Mode = "image"
		input.Config.Size = arguments.ImageConfig.Size
		input.Config.Quality = arguments.ImageConfig.Quality
		input.Config.Count = strconv.Itoa(arguments.ImageConfig.Count)
		input.Config.TransparentBackground = strconv.FormatBool(arguments.ImageConfig.TransparentBackground)
		if artifact.Kind == model.AgentProductionArtifactStoryboardImage {
			references, err := s.productionShotReferenceResources(scope, arguments, artifact)
			if err != nil {
				return canvasGenerationInput{}, "", err
			}
			input.ReferenceImages = references
		}
		return input, "canvas_image", nil
	}
	if arguments.VideoConfig == nil {
		return canvasGenerationInput{}, "", errAgentRuntimeProductionRenderInput
	}
	input.Mode = "video"
	input.Config.VideoSeconds = strconv.Itoa(arguments.VideoConfig.DurationSeconds)
	input.Config.Size = arguments.VideoConfig.AspectRatio
	input.Config.VQuality = arguments.VideoConfig.Quality
	input.Config.VideoGenerateAudio = strconv.FormatBool(arguments.VideoConfig.GenerateAudio)
	switch arguments.VideoInputMode {
	case agentruntime.ProductionVideoInputTextToVideo:
		if arguments.VideoInputResourceID != "" {
			return canvasGenerationInput{}, "", errAgentRuntimeProductionRenderInput
		}
	case agentruntime.ProductionVideoInputStoryboard:
		if arguments.VideoInputResourceID == "" {
			return canvasGenerationInput{}, "", errAgentRuntimeProductionRenderInput
		}
		resource, err := s.productionResourceForScope(scope, arguments.VideoInputResourceID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return canvasGenerationInput{}, "", errProductionPrerequisiteAssetMissing
			}
			return canvasGenerationInput{}, "", err
		}
		if resource.Status != model.ResourceStatusReady || resource.Kind != "image" {
			return canvasGenerationInput{}, "", errProductionPrerequisiteAssetMissing
		}
		input.ReferenceImages = []providerMedia{{
			ID: resource.ID, Name: resource.ID, Type: "image", URL: "/api/resources/" + resource.ID + "/file",
			StorageKey: "resource:" + resource.ID, MimeType: resource.MimeType,
		}}
	default:
		return canvasGenerationInput{}, "", errAgentRuntimeProductionRenderInput
	}
	return input, "canvas_video", nil
}

func (s *Service) productionShotReferenceResources(
	scope agentruntime.Scope,
	arguments agentruntime.ProductionRenderArguments,
	storyboardArtifact model.AgentProductionArtifact,
) ([]providerMedia, error) {
	plan, err := s.repo.AgentProductionPlanVersionForScope(scope, arguments.PlanKey, arguments.PlanVersion)
	if err != nil {
		return nil, err
	}
	var shots []agentruntime.ShotPlanDraft
	if err := json.Unmarshal([]byte(plan.ShotsJSON), &shots); err != nil {
		return nil, err
	}
	var referenceKeys []string
	for _, shot := range shots {
		if shot.ShotKey == storyboardArtifact.ShotKey {
			referenceKeys = shot.ReferenceKeys
			break
		}
	}
	if len(referenceKeys) == 0 {
		return nil, nil
	}
	artifacts, err := s.repo.AgentProductionArtifactsForVersion(scope, arguments.PlanKey, arguments.PlanVersion)
	if err != nil {
		return nil, err
	}
	referenceArtifacts := make(map[string]model.AgentProductionArtifact, len(referenceKeys))
	for _, artifact := range artifacts {
		if artifact.Kind == model.AgentProductionArtifactReferenceImage {
			referenceArtifacts[artifact.ReferenceKey] = artifact
		}
	}
	media := make([]providerMedia, 0, len(referenceKeys))
	for _, referenceKey := range referenceKeys {
		artifact, exists := referenceArtifacts[referenceKey]
		if !exists || (artifact.Status != model.AgentProductionArtifactSucceeded && artifact.Status != model.AgentProductionArtifactCommitted) || artifact.ResourceID == "" {
			return nil, errProductionPrerequisiteAssetMissing
		}
		resource, err := s.productionResourceForScope(scope, artifact.ResourceID)
		if err != nil {
			return nil, err
		}
		if resource.Status != model.ResourceStatusReady || resource.Kind != "image" {
			return nil, errProductionPrerequisiteAssetMissing
		}
		media = append(media, providerMedia{
			ID: resource.ID, Name: referenceKey, Type: "image", URL: "/api/resources/" + resource.ID + "/file",
			StorageKey: "resource:" + resource.ID, MimeType: resource.MimeType,
		})
	}
	return media, nil
}

func (s *Service) productionStoryboardResource(scope agentruntime.Scope, arguments agentruntime.ProductionRenderArguments, videoArtifact model.AgentProductionArtifact) (*model.Resource, error) {
	artifacts, err := s.repo.AgentProductionArtifactsForVersion(scope, arguments.PlanKey, arguments.PlanVersion)
	if err != nil {
		return nil, err
	}
	for _, artifact := range artifacts {
		if artifact.ShotKey != videoArtifact.ShotKey || artifact.Kind != model.AgentProductionArtifactStoryboardImage ||
			(artifact.Status != model.AgentProductionArtifactSucceeded && artifact.Status != model.AgentProductionArtifactCommitted) ||
			artifact.ResourceID == "" {
			continue
		}
		resource, err := s.productionResourceForScope(scope, artifact.ResourceID)
		if err != nil {
			return nil, err
		}
		if resource.Status != model.ResourceStatusReady || resource.Kind != "image" {
			return nil, errProductionPrerequisiteAssetMissing
		}
		return resource, nil
	}
	return nil, errProductionPrerequisiteAssetMissing
}

func (s *Service) productionResourceForScope(scope agentruntime.Scope, resourceID string) (*model.Resource, error) {
	if scope.TenantKind == agentruntime.TenantTeam {
		return s.repo.ResourceForTeam(scope.TenantID, resourceID)
	}
	return s.repo.ResourceForUser(scope.ActorUserID, resourceID)
}

func (s *Service) bindProductionArtifactTask(
	scope agentruntime.Scope,
	arguments agentruntime.ProductionRenderArguments,
	artifact model.AgentProductionArtifact,
	task model.Task,
	order model.BillingOrder,
) (*model.AgentProductionArtifact, error) {
	if artifact.TaskID != "" {
		if artifact.TaskID != task.ID || artifact.BillingOrderID != order.ID || artifact.Attempt != arguments.Attempt+1 {
			return nil, repository.ErrAgentProductionArtifactConflict
		}
		return &artifact, nil
	}
	transitioned, err := s.repo.TransitionAgentProductionArtifact(scope, repository.ArtifactTransition{
		ArtifactID: artifact.ID, ExpectedStatus: model.AgentProductionArtifactAwaitingApproval, NextStatus: model.AgentProductionArtifactQueued,
		ExpectedAttempt: arguments.Attempt, NextAttempt: arguments.Attempt + 1,
		TaskID: task.ID, BillingOrderID: order.ID, Now: time.Now().UTC(),
	})
	if err == nil {
		return transitioned, nil
	}
	if !errors.Is(err, repository.ErrAgentProductionArtifactConflict) {
		return nil, err
	}
	latest, loadErr := s.productionArtifactForRender(scope, arguments)
	if loadErr != nil {
		return nil, errors.Join(err, loadErr)
	}
	if latest.Status != model.AgentProductionArtifactQueued || latest.Attempt != arguments.Attempt+1 ||
		latest.TaskID != task.ID || latest.BillingOrderID != order.ID {
		return nil, err
	}
	return latest, nil
}

func productionRenderTaskIdentity(idempotencyKey string, attempt int) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(idempotencyKey) + ":" + strconv.Itoa(attempt)))
	return hex.EncodeToString(digest[:16])
}

func agentProductionRenderRunID(operation string) (string, bool) {
	const prefix = "production_render:"
	operation = strings.TrimSpace(operation)
	if !strings.HasPrefix(operation, prefix) || len(operation) > 96 {
		return "", false
	}
	runID := strings.TrimSpace(strings.TrimPrefix(operation, prefix))
	return runID, runID != ""
}

type storedProductionMedia struct {
	ResourceID string `json:"resourceId"`
}

type storedProductionMediaEnvelope struct {
	Images []storedProductionMedia `json:"images"`
	Image  *storedProductionMedia  `json:"image"`
	Videos []storedProductionMedia `json:"videos"`
	Video  *storedProductionMedia  `json:"video"`
}

func taskResultResourceID(resultJSON string, artifactKind model.AgentProductionArtifactKind) (string, error) {
	var result storedProductionMediaEnvelope
	if err := json.Unmarshal([]byte(resultJSON), &result); err != nil {
		return "", err
	}
	media := result.Images
	if artifactKind == model.AgentProductionArtifactVideoClip {
		media = result.Videos
		if len(media) == 0 && result.Video != nil {
			media = []storedProductionMedia{*result.Video}
		}
	} else if len(media) == 0 && result.Image != nil {
		media = []storedProductionMedia{*result.Image}
	}
	if len(media) != 1 || strings.TrimSpace(media[0].ResourceID) == "" {
		return "", errors.New("production task result resource is invalid")
	}
	return strings.TrimSpace(media[0].ResourceID), nil
}

func (s *Service) reconcileSucceededProductionArtifacts(scope agentruntime.Scope) error {
	record, err := s.repo.ActiveAgentProductionPlanForThread(scope)
	if err != nil || record == nil {
		return err
	}
	for _, current := range record.Artifacts {
		if current.Kind != model.AgentProductionArtifactReferenceImage && current.Kind != model.AgentProductionArtifactStoryboardImage && current.Kind != model.AgentProductionArtifactVideoClip {
			continue
		}
		artifact := current
		if !((artifact.Status == model.AgentProductionArtifactFailed && artifact.LastErrorCode == "production_result_invalid") ||
			(artifact.Status == model.AgentProductionArtifactQueued && artifact.LastErrorCode == productionResultMaterializing)) {
			continue
		}
		task, err := s.repo.TaskForUser(scope.ActorUserID, artifact.TaskID)
		if err != nil {
			return err
		}
		if task.Status != model.TaskStatusSucceeded {
			return errors.New("production result recovery requires a succeeded task")
		}
		order, err := s.repo.BillingOrder(artifact.BillingOrderID)
		if err != nil {
			return err
		}
		if order.UserID != scope.ActorUserID || order.TaskID != task.ID || order.Status != model.BillingStatusSettled {
			return errors.New("production result recovery billing facts conflict")
		}
		if artifact.Status == model.AgentProductionArtifactFailed && artifact.LastErrorCode == "production_result_invalid" {
			artifactPointer, transitionErr := s.repo.TransitionAgentProductionArtifact(scope, repository.ArtifactTransition{
				ArtifactID: artifact.ID, ExpectedStatus: artifact.Status, NextStatus: model.AgentProductionArtifactQueued,
				ExpectedAttempt: artifact.Attempt, NextAttempt: artifact.Attempt,
				TaskID: artifact.TaskID, BillingOrderID: artifact.BillingOrderID,
				LastErrorCode: productionResultMaterializing, Now: time.Now().UTC(),
			})
			if transitionErr != nil {
				return transitionErr
			}
			artifact = *artifactPointer
		}
		resourceID, err := s.materializeSucceededProductionTaskResult(scope, *task, artifact.Kind)
		if err != nil {
			return err
		}
		if _, err := s.repo.TransitionAgentProductionArtifact(scope, repository.ArtifactTransition{
			ArtifactID: artifact.ID, ExpectedStatus: artifact.Status, NextStatus: model.AgentProductionArtifactSucceeded,
			ExpectedAttempt: artifact.Attempt, NextAttempt: artifact.Attempt,
			TaskID: task.ID, BillingOrderID: order.ID, ResourceID: resourceID, Now: time.Now().UTC(),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) materializeSucceededProductionTaskResult(scope agentruntime.Scope, task model.Task, artifactKind model.AgentProductionArtifactKind) (string, error) {
	if resourceID, err := taskResultResourceID(task.ResultJSON, artifactKind); err == nil {
		resource, loadErr := s.productionResourceForScope(scope, resourceID)
		if loadErr != nil {
			return "", loadErr
		}
		if resource.Status != model.ResourceStatusReady {
			return "", errors.New("production task resource is not ready")
		}
		return resource.ID, nil
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal([]byte(task.ResultJSON), &result); err != nil {
		return "", err
	}
	item, location, err := productionTaskRemoteMediaItem(result, artifactKind)
	if err != nil {
		return "", err
	}
	rawURL := strings.TrimSpace(firstNonEmpty(rawJSONString(item["dataUrl"]), rawJSONString(item["url"])))
	if rawURL == "" {
		return "", errors.New("production task result has no recoverable remote media URL")
	}
	expectedKind := "image"
	if artifactKind == model.AgentProductionArtifactVideoClip {
		expectedKind = "video"
	}
	teamID := ""
	if scope.TenantKind == agentruntime.TenantTeam {
		teamID = scope.TenantID
	}
	resourceIdentity := productionRenderTaskIdentity("resource:"+task.ID+":"+string(artifactKind), 0)
	resource, loadErr := s.repo.Resource(resourceIdentity)
	needsMaterialization := false
	if loadErr == nil {
		if resource.UserID != task.UserID || resource.TeamID != teamID || resource.Kind != expectedKind {
			return "", errors.New("deterministic production resource facts conflict")
		}
		switch resource.Status {
		case model.ResourceStatusReady:
		case model.ResourceStatusPending, model.ResourceStatusFailed:
			needsMaterialization = true
		default:
			return "", errors.New("deterministic production resource status is not recoverable")
		}
	} else {
		if !errors.Is(loadErr, gorm.ErrRecordNotFound) {
			return "", loadErr
		}
		needsMaterialization = true
	}
	if needsMaterialization {
		policy, err := s.RuntimePolicy()
		if err != nil {
			return "", err
		}
		payload, err := downloadRemoteResource(rawURL, megabytes(policy.Resource.GeneratedFileMB))
		if err != nil {
			return "", fmt.Errorf("恢复已付费生成资源失败：%w", err)
		}
		if normalizeResourceKind("", payload.mimeType) != expectedKind {
			return "", errors.New("production task remote media kind conflicts with artifact")
		}
		width, height := 0, 0
		if expectedKind == "image" {
			width, height = imageDimensions(payload.data)
		}
		resource, err = s.storeScopedResourceWithIdentity(
			resourceIdentity, task.UserID, teamID, expectedKind, payload.fileName, payload.mimeType,
			int64(len(payload.data)), width, height, bytes.NewReader(payload.data),
		)
		if err != nil {
			return "", err
		}
	}
	resourceURL := "/api/resources/" + resource.ID + "/file"
	item["dataUrl"] = rawJSONStringValue(resourceURL)
	item["url"] = rawJSONStringValue(resourceURL)
	item["storageKey"] = rawJSONStringValue("resource:" + resource.ID)
	item["resourceId"] = rawJSONStringValue(resource.ID)
	item["bytes"] = json.RawMessage(strconv.FormatInt(resource.Size, 10))
	item["mimeType"] = rawJSONStringValue(resource.MimeType)
	item["width"] = json.RawMessage(strconv.Itoa(resource.Width))
	item["height"] = json.RawMessage(strconv.Itoa(resource.Height))
	if err := replaceProductionTaskRemoteMediaItem(result, location, item); err != nil {
		return "", err
	}
	nextResult, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	if _, err := s.repo.AttachSucceededTaskResource(task.UserID, task.ID, task.ResultJSON, string(nextResult)); err != nil {
		latest, loadErr := s.repo.TaskForUser(task.UserID, task.ID)
		if loadErr != nil {
			return "", errors.Join(err, loadErr)
		}
		latestResourceID, resultErr := taskResultResourceID(latest.ResultJSON, artifactKind)
		if resultErr != nil || latestResourceID != resource.ID {
			return "", err
		}
	}
	return resource.ID, nil
}

type productionTaskRemoteMediaLocation struct {
	key    string
	plural bool
}

func productionTaskRemoteMediaItem(result map[string]json.RawMessage, artifactKind model.AgentProductionArtifactKind) (map[string]json.RawMessage, productionTaskRemoteMediaLocation, error) {
	pluralKey, singularKey := "images", "image"
	if artifactKind == model.AgentProductionArtifactVideoClip {
		pluralKey, singularKey = "videos", "video"
	}
	if rawItems, ok := result[pluralKey]; ok {
		var items []json.RawMessage
		if err := json.Unmarshal(rawItems, &items); err != nil {
			return nil, productionTaskRemoteMediaLocation{}, errors.New("production task result media list is invalid")
		}
		if len(items) != 1 {
			return nil, productionTaskRemoteMediaLocation{}, errors.New("production task result media count is invalid")
		}
		var item map[string]json.RawMessage
		if err := json.Unmarshal(items[0], &item); err != nil || item == nil {
			return nil, productionTaskRemoteMediaLocation{}, errors.New("production task result media is invalid")
		}
		return item, productionTaskRemoteMediaLocation{key: pluralKey, plural: true}, nil
	}
	if rawItem, ok := result[singularKey]; ok {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(rawItem, &item); err != nil || item == nil {
			return nil, productionTaskRemoteMediaLocation{}, errors.New("production task result media is invalid")
		}
		return item, productionTaskRemoteMediaLocation{key: singularKey}, nil
	}
	return nil, productionTaskRemoteMediaLocation{}, errors.New("production task result media is missing")
}

func replaceProductionTaskRemoteMediaItem(result map[string]json.RawMessage, location productionTaskRemoteMediaLocation, item map[string]json.RawMessage) error {
	encoded, err := json.Marshal(item)
	if err != nil {
		return err
	}
	if location.plural {
		encoded, err = json.Marshal([]json.RawMessage{encoded})
		if err != nil {
			return err
		}
	}
	result[location.key] = encoded
	return nil
}

func rawJSONString(value json.RawMessage) string {
	var decoded string
	if len(value) == 0 || json.Unmarshal(value, &decoded) != nil {
		return ""
	}
	return decoded
}

func rawJSONStringValue(value string) json.RawMessage {
	return json.RawMessage(strconv.AppendQuote(nil, value))
}
