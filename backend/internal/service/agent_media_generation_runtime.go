package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

const (
	agentMediaCandidateSchema     = "media_candidate.v1"
	agentMediaGenerationOperation = "media_generation"
)

var (
	errAgentMediaArgumentsInvalid       = errors.New("agent media generation arguments are invalid")
	errAgentMediaInputChanged           = errors.New("agent media generation input changed after approval proposal")
	errAgentMediaModelUnavailable       = errors.New("agent media generation model is unavailable")
	errAgentMediaCapabilitiesChanged    = errors.New("agent media generation provider capabilities changed after approval proposal")
	errAgentNativeAudioUnavailable      = errors.New("agent video model does not support the requested native audio facts")
	errAgentMediaResultInvalid          = errors.New("agent media generation result is invalid")
	errDirectedVideoRegenerationInvalid = errors.New("directed video regeneration facts are invalid")
)

type agentMediaInputResource struct {
	Revision   agentruntime.ArtifactRevisionRef `json:"revision"`
	ResourceID string                           `json:"resourceId"`
	Kind       string                           `json:"kind"`
	URL        string                           `json:"url"`
	ObjectKey  string                           `json:"objectKey"`
	ETag       string                           `json:"etag"`
	MimeType   string                           `json:"mimeType"`
	DurationMS int64                            `json:"durationMs,omitempty"`
}

type agentMediaGenerationArguments struct {
	InputRevisions          []agentruntime.ArtifactRevisionRef    `json:"inputRevisions"`
	InputResources          []agentMediaInputResource             `json:"inputResources"`
	GenerationModel         agentruntime.GenerationModelSelection `json:"generationModel"`
	GenerationModelRecordID string                                `json:"generationModelRecordId"`
	Capability              string                                `json:"capability"`
	Parameters              json.RawMessage                       `json:"parameters"`
	OutputArtifactID        string                                `json:"outputArtifactId"`
	OutputArtifactKey       string                                `json:"outputArtifactKey"`
	ExpectedOutputSchema    string                                `json:"expectedOutputSchema"`
	ExpectedDelivery        agentruntime.ExpectedDelivery         `json:"expectedDelivery"`
	RequestIdentity         string                                `json:"requestIdentity"`
	SkillVersions           []agentruntime.SkillSelection         `json:"skillVersions"`
	Commercial              MediaGenerationAttempt                `json:"commercial"`
	DirectedRegeneration    *DirectedVideoRegenerationArguments   `json:"directedRegeneration,omitempty"`
}

type agentImageGenerationParameters struct {
	Prompt                string `json:"prompt"`
	AspectRatio           string `json:"aspectRatio"`
	Resolution            string `json:"resolution"`
	Quality               string `json:"quality,omitempty"`
	Count                 int    `json:"count"`
	TransparentBackground bool   `json:"transparentBackground,omitempty"`
}

type agentVideoGenerationParameters struct {
	Prompt          string `json:"prompt"`
	AspectRatio     string `json:"aspectRatio"`
	Resolution      string `json:"resolution"`
	DurationSeconds int    `json:"durationSeconds"`
	GenerateAudio   bool   `json:"generateAudio"`
}

type agentAudioGenerationParameters struct {
	Prompt        string `json:"prompt"`
	Voice         string `json:"voice"`
	Format        string `json:"format,omitempty"`
	Speed         string `json:"speed,omitempty"`
	Volume        string `json:"volume,omitempty"`
	Pitch         string `json:"pitch,omitempty"`
	Emotion       string `json:"emotion,omitempty"`
	LanguageBoost string `json:"languageBoost,omitempty"`
	SampleRate    string `json:"sampleRate,omitempty"`
	Bitrate       string `json:"bitrate,omitempty"`
	Channel       string `json:"channel,omitempty"`
	Instructions  string `json:"instructions,omitempty"`
}

func artifactRevisionContains(raw string, expected agentruntime.ArtifactRevisionRef) bool {
	var refs []agentruntime.ArtifactRevisionRef
	if err := json.Unmarshal([]byte(raw), &refs); err != nil {
		return false
	}
	for _, ref := range refs {
		if ref.Validate() != nil {
			return false
		}
		if ref == expected {
			return true
		}
	}
	return false
}

func revisionRefsContain(refs []agentruntime.ArtifactRevisionRef, expected agentruntime.ArtifactRevisionRef) bool {
	for _, ref := range refs {
		if ref == expected {
			return true
		}
	}
	return false
}

func (s *Service) directedVideoRegenerationAttempt(
	scope agentruntime.Scope,
	inputRevisions []agentruntime.ArtifactRevisionRef,
	facts DirectedVideoRegenerationArguments,
) (int, error) {
	if scope.Validate() != nil || facts.SourceShotRevision.Validate() != nil || facts.ApprovedCandidateRevision.Validate() != nil {
		return 0, errDirectedVideoRegenerationInvalid
	}
	sourceShot, err := s.repo.ArtifactRevisionForArtifactInScope(
		scope, facts.SourceShotRevision.ArtifactID, facts.SourceShotRevision.RevisionID,
	)
	if err != nil || sourceShot.Kind != "shot_revision" || sourceShot.LifecycleStatus != model.AgentArtifactRevisionStale {
		return 0, errors.Join(errDirectedVideoRegenerationInvalid, err)
	}
	currentShot, err := s.repo.ArtifactHeadRevisionForScope(scope, facts.SourceShotRevision.ArtifactID)
	if err != nil || currentShot.ID == sourceShot.ID || currentShot.Kind != "shot_revision" ||
		currentShot.ArtifactKey != sourceShot.ArtifactKey || currentShot.LifecycleStatus == model.AgentArtifactRevisionStale {
		return 0, errors.Join(errDirectedVideoRegenerationInvalid, err)
	}
	currentShotRef := agentruntime.ArtifactRevisionRef{ArtifactID: currentShot.ArtifactID, RevisionID: currentShot.ID}
	if !revisionRefsContain(inputRevisions, currentShotRef) {
		return 0, errDirectedVideoRegenerationInvalid
	}

	approvedRevisionIDs, err := s.repo.ApprovedArtifactRevisionIDsForScope(scope)
	if err != nil {
		return 0, errors.Join(errDirectedVideoRegenerationInvalid, err)
	}
	if _, approved := approvedRevisionIDs[facts.ApprovedCandidateRevision.RevisionID]; !approved {
		return 0, errDirectedVideoRegenerationInvalid
	}
	candidate, err := s.repo.ArtifactRevisionForArtifactInScope(
		scope, facts.ApprovedCandidateRevision.ArtifactID, facts.ApprovedCandidateRevision.RevisionID,
	)
	if err != nil || candidate.Kind != "media_candidate" || candidate.LifecycleStatus != model.AgentArtifactRevisionStale ||
		!artifactRevisionContains(candidate.UpstreamRevisionsJSON, facts.SourceShotRevision) {
		return 0, errors.Join(errDirectedVideoRegenerationInvalid, err)
	}
	candidateContent, err := agentruntime.DecodeMediaCandidateContent([]byte(candidate.PayloadJSON))
	if err != nil || candidateContent.MediaKind != agentruntime.ArtifactVideo || candidateContent.ResourceID != candidate.ResourceID ||
		strings.TrimSpace(candidateContent.SourceTaskID) == "" {
		return 0, errors.Join(errDirectedVideoRegenerationInvalid, err)
	}

	calls, err := s.repo.AgentToolCallsForScope(scope)
	if err != nil {
		return 0, errors.Join(errDirectedVideoRegenerationInvalid, err)
	}
	var previous *agentMediaGenerationArguments
	for _, call := range calls {
		if call.ToolName != string(agentruntime.ToolMediaGenerate) || call.Status != agentruntime.ToolCallSucceeded ||
			frozenAgentMediaTaskID(call.InputJSON) != candidateContent.SourceTaskID {
			continue
		}
		decoded, decodeErr := decodeFrozenAgentMediaGenerationArguments(json.RawMessage(call.InputJSON))
		if decodeErr != nil || previous != nil {
			return 0, errors.Join(errDirectedVideoRegenerationInvalid, decodeErr)
		}
		previous = &decoded
	}
	if previous == nil || previous.Capability != "video" || previous.Commercial.TaskID != candidateContent.SourceTaskID ||
		!strings.HasPrefix(candidate.ArtifactID, previous.OutputArtifactID+"-") ||
		!revisionRefsContain(previous.InputRevisions, facts.SourceShotRevision) ||
		previous.Commercial.Attempt < 1 || previous.Commercial.Attempt >= 999_999 {
		return 0, errDirectedVideoRegenerationInvalid
	}
	task, err := s.repo.TaskForUser(scope.ActorUserID, previous.Commercial.TaskID)
	if err != nil {
		return 0, errors.Join(errDirectedVideoRegenerationInvalid, err)
	}
	validatedTask, order, err := s.validateMediaTaskFacts(scope, previous.Commercial, task)
	if err != nil || validatedTask.Status != model.TaskStatusSucceeded {
		return 0, errors.Join(errDirectedVideoRegenerationInvalid, err)
	}
	switch order.Status {
	case model.BillingStatusReserved, model.BillingStatusRunning, model.BillingStatusSettled, model.BillingStatusUncertain:
	default:
		return 0, errDirectedVideoRegenerationInvalid
	}
	return previous.Commercial.Attempt + 1, nil
}

func frozenAgentMediaTaskID(raw string) string {
	var envelope struct {
		Commercial struct {
			TaskID string `json:"taskId"`
		} `json:"commercial"`
	}
	if json.Unmarshal([]byte(raw), &envelope) != nil {
		return ""
	}
	return strings.TrimSpace(envelope.Commercial.TaskID)
}

func agentMediaGenerationOperationForRun(runID string) string {
	return agentMediaGenerationOperation + ":" + strings.TrimSpace(runID)
}

func agentMediaGenerationRunID(operation string) (string, bool) {
	prefix := agentMediaGenerationOperation + ":"
	operation = strings.TrimSpace(operation)
	if !strings.HasPrefix(operation, prefix) || len(operation) > 96 {
		return "", false
	}
	runID := strings.TrimSpace(strings.TrimPrefix(operation, prefix))
	return runID, runID != ""
}

func agentMediaGenerationFailureDetails(err error) (string, agentruntime.ToolFailureClass, bool) {
	switch {
	case errors.Is(err, errAgentMediaArgumentsInvalid):
		return "media_generation_invalid", agentruntime.ToolFailureAgentRepairable, true
	case errors.Is(err, errAgentMediaInputChanged):
		return "media_generation_input_changed", agentruntime.ToolFailureAgentRepairable, true
	case errors.Is(err, errAgentMediaModelUnavailable):
		return "media_generation_model_unavailable", agentruntime.ToolFailureAgentRepairable, true
	case errors.Is(err, errAgentMediaCapabilitiesChanged):
		return "media_generation_capabilities_changed", agentruntime.ToolFailureAgentRepairable, true
	case errors.Is(err, errAgentNativeAudioUnavailable):
		return "native_audio_capability_unavailable", agentruntime.ToolFailureAgentRepairable, true
	case errors.Is(err, errDirectedVideoRegenerationInvalid):
		return "directed_video_regeneration_invalid", agentruntime.ToolFailureAgentRepairable, true
	default:
		return "", "", false
	}
}

func (s *Service) freezeAgentMediaGenerationDecisionArguments(
	scope agentruntime.Scope,
	configuration agentruntime.RunConfiguration,
	callableModels []agentRuntimeCallableModelFact,
	call *agentruntime.ToolCallDecision,
) ([]byte, error) {
	if call == nil || call.ToolName != agentruntime.ToolMediaGenerate || call.ActionVersion < 1 {
		return nil, errAgentMediaArgumentsInvalid
	}
	proposal, err := decodeMediaGenerateArguments(call.Arguments)
	if err != nil || !proposal.ExpectedDelivery.Equal(call.ExpectedDelivery) ||
		!expectedDeliveryRequiresMedia(proposal.ExpectedDelivery, proposal.Capability) {
		return nil, errAgentMediaArgumentsInvalid
	}
	selected := generationModelForCapability(configuration.GenerationModels, proposal.Capability)
	if selected == nil || *selected != proposal.GenerationModel {
		return nil, errAgentMediaModelUnavailable
	}
	callable, found := exactCallableMediaModel(callableModels, proposal.GenerationModel, proposal.Capability)
	if !found || callable.ProviderCapabilities == nil {
		return nil, errAgentMediaModelUnavailable
	}
	configured, err := s.repo.ChannelModelByKey(proposal.GenerationModel.ChannelID, proposal.GenerationModel.Model)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errAgentMediaModelUnavailable
		}
		return nil, err
	}
	if !matchesFrozenMediaModel(*configured, callable, proposal.Capability) {
		return nil, errAgentMediaModelUnavailable
	}
	parameters, err := canonicalAgentJSON(proposal.Parameters)
	if err != nil {
		return nil, errors.Join(errAgentMediaArgumentsInvalid, err)
	}
	inputResources, err := s.freezeAgentMediaInputResources(scope, proposal.InputRevisions)
	if err != nil {
		return nil, err
	}
	attempt := 1
	if proposal.DirectedRegeneration != nil {
		attempt, err = s.directedVideoRegenerationAttempt(scope, proposal.InputRevisions, *proposal.DirectedRegeneration)
		if err != nil {
			return nil, err
		}
	}
	digest := agentMediaGenerationIdentity(scope, proposal, configured.ID, call.ToolCallID, call.ActionVersion, parameters)
	frozen := agentMediaGenerationArguments{
		InputRevisions: cloneAgentMediaRevisionRefs(proposal.InputRevisions),
		InputResources: inputResources, GenerationModel: proposal.GenerationModel,
		GenerationModelRecordID: configured.ID, Capability: proposal.Capability,
		Parameters: parameters, OutputArtifactID: "generated-media-" + digest[:32],
		OutputArtifactKey: proposal.OutputArtifactKey, ExpectedOutputSchema: proposal.ExpectedOutputSchema,
		ExpectedDelivery: proposal.ExpectedDelivery, RequestIdentity: "media-generation:" + digest,
		SkillVersions:        cloneAgentRuntimeSkillSelections(configuration.Skills),
		Commercial:           MediaGenerationAttempt{Attempt: attempt},
		DirectedRegeneration: cloneDirectedVideoRegenerationArguments(proposal.DirectedRegeneration),
	}
	command, err := agentMediaGenerationCommand(scope, frozen, callable.ProviderCapabilities)
	if err != nil {
		return nil, err
	}
	commercial, err := s.FreezeMediaQuote(scope, command, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	frozen.Commercial = *commercial
	encoded, err := json.Marshal(frozen)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func generationModelForCapability(selections agentruntime.GenerationModelSelections, capability string) *agentruntime.GenerationModelSelection {
	switch capability {
	case "image":
		return selections.Image
	case "video":
		return selections.Video
	case "audio":
		return selections.Audio
	default:
		return nil
	}
}

func exactCallableMediaModel(models []agentRuntimeCallableModelFact, selection agentruntime.GenerationModelSelection, capability string) (agentRuntimeCallableModelFact, bool) {
	for _, candidate := range models {
		if candidate.ChannelID == selection.ChannelID && candidate.Model == selection.Model && candidate.Capability == capability {
			return candidate, true
		}
	}
	return agentRuntimeCallableModelFact{}, false
}

func matchesFrozenMediaModel(configured model.ChannelModel, callable agentRuntimeCallableModelFact, capability string) bool {
	return configured.ID != "" && configured.Enabled && configured.PriceConfigured && configured.Capability == capability &&
		configured.ChannelID == callable.ChannelID && configured.ModelKey == callable.Model && configured.DisplayName == callable.DisplayName &&
		configured.BillingMode == callable.BillingMode && configured.PriceStrategy == callable.PriceStrategy &&
		configured.UnitPriceMicrocredits == callable.UnitPriceMicrocredits
}

func expectedDeliveryRequiresMedia(delivery agentruntime.ExpectedDelivery, capability string) bool {
	want := agentruntime.ArtifactKind(capability)
	hasRequiredArtifact := false
	for _, kind := range delivery.RequiredArtifacts {
		if kind == want {
			hasRequiredArtifact = true
			break
		}
	}
	hasExactRevision := false
	hasReadyResource := false
	for _, criterion := range delivery.CompletionCriteria {
		if criterion.Artifact != want {
			continue
		}
		switch criterion.Fact {
		case agentruntime.DeliveryFactArtifactRevision:
			hasExactRevision = true
		case agentruntime.DeliveryFactResource:
			hasReadyResource = true
		}
	}
	return hasRequiredArtifact && hasExactRevision && hasReadyResource
}

func (s *Service) freezeAgentMediaInputResources(scope agentruntime.Scope, refs []agentruntime.ArtifactRevisionRef) ([]agentMediaInputResource, error) {
	if refs == nil || len(refs) > 16 {
		return nil, errAgentMediaArgumentsInvalid
	}
	resources := make([]agentMediaInputResource, 0, len(refs))
	for _, ref := range refs {
		revision, err := s.repo.ArtifactRevisionForArtifactInScope(scope, ref.ArtifactID, ref.RevisionID)
		if err != nil {
			return nil, errors.Join(errAgentMediaInputChanged, err)
		}
		head, err := s.repo.ArtifactHeadRevisionForScope(scope, ref.ArtifactID)
		if err != nil || head.ID != revision.ID || revision.LifecycleStatus == model.AgentArtifactRevisionStale {
			return nil, errors.Join(errAgentMediaInputChanged, err)
		}
		if revision.ResourceID == "" {
			continue
		}
		resource, err := s.productionResourceForScope(scope, revision.ResourceID)
		if err != nil || !agentMediaResourceReady(resource) {
			return nil, errors.Join(errAgentMediaInputChanged, err)
		}
		resources = append(resources, agentMediaInputResource{
			Revision: ref, ResourceID: resource.ID, Kind: resource.Kind,
			URL: "/api/resources/" + resource.ID + "/file", ObjectKey: resource.ObjectKey,
			ETag: resource.ETag, MimeType: resource.MimeType, DurationMS: resource.DurationMs,
		})
	}
	return resources, nil
}

func agentMediaResourceReady(resource *model.Resource) bool {
	if resource == nil || resource.Status != model.ResourceStatusReady ||
		(resource.Kind != "image" && resource.Kind != "video" && resource.Kind != "audio") {
		return false
	}
	return strings.TrimSpace(resource.Provider) != "" && strings.TrimSpace(resource.ObjectKey) != "" &&
		strings.TrimSpace(resource.ETag) != ""
}

func agentMediaGenerationIdentity(
	scope agentruntime.Scope,
	proposal MediaGenerateArguments,
	modelRecordID string,
	toolCallID string,
	actionVersion int,
	parameters []byte,
) string {
	facts := []string{
		string(scope.TenantKind), scope.TenantID, scope.ActorUserID, scope.DomainProjectID, scope.CanvasID,
		scope.ThreadID, scope.RunID, proposal.Capability, proposal.GenerationModel.ChannelID,
		proposal.GenerationModel.Model, modelRecordID, proposal.OutputArtifactKey, proposal.ExpectedOutputSchema,
		strings.TrimSpace(toolCallID), strconv.Itoa(actionVersion), string(parameters),
	}
	for _, ref := range proposal.InputRevisions {
		facts = append(facts, ref.ArtifactID, ref.RevisionID)
	}
	if proposal.DirectedRegeneration != nil {
		facts = append(facts,
			proposal.DirectedRegeneration.SourceShotRevision.ArtifactID,
			proposal.DirectedRegeneration.SourceShotRevision.RevisionID,
			proposal.DirectedRegeneration.ApprovedCandidateRevision.ArtifactID,
			proposal.DirectedRegeneration.ApprovedCandidateRevision.RevisionID,
		)
	}
	digest := sha256.Sum256([]byte(strings.Join(facts, "\x00")))
	return hex.EncodeToString(digest[:])
}

func agentMediaGenerationCommand(
	scope agentruntime.Scope,
	arguments agentMediaGenerationArguments,
	capabilities *PublicProviderCapabilities,
) (MediaGenerationCommand, error) {
	input, prompt, quantity, err := buildAgentMediaGenerationTaskInput(arguments, capabilities)
	if err != nil {
		return MediaGenerationCommand{}, err
	}
	parameters, err := json.Marshal(input)
	if err != nil {
		return MediaGenerationCommand{}, err
	}
	return MediaGenerationCommand{
		ArtifactRevisionID: arguments.OutputArtifactID, Attempt: arguments.Commercial.Attempt,
		TaskType: "canvas_" + arguments.Capability, Operation: agentMediaGenerationOperationForRun(scope.RunID),
		Prompt: prompt, Capability: arguments.Capability, ChannelID: arguments.GenerationModel.ChannelID,
		ModelKey: arguments.GenerationModel.Model, ParametersJSON: string(parameters), Quantity: quantity,
		ProviderCapabilities: capabilities,
	}, nil
}

func buildAgentMediaGenerationTaskInput(arguments agentMediaGenerationArguments, capabilities *PublicProviderCapabilities) (canvasGenerationInput, string, int64, error) {
	if capabilities == nil || capabilities.Capability != arguments.Capability || capabilities.ModelKey != arguments.GenerationModel.Model {
		return canvasGenerationInput{}, "", 0, errAgentMediaModelUnavailable
	}
	input := canvasGenerationInput{
		Mode:            arguments.Capability,
		Config:          providerConfig{ChannelID: arguments.GenerationModel.ChannelID, Model: arguments.GenerationModel.Model},
		ReferenceImages: []providerMedia{}, ReferenceVideos: []providerMedia{}, ReferenceAudios: []providerMedia{},
		Metadata: map[string]interface{}{
			"agentRequestIdentity": arguments.RequestIdentity,
			"outputArtifactId":     arguments.OutputArtifactID,
			"expectedOutputSchema": arguments.ExpectedOutputSchema,
		},
	}
	for _, resource := range arguments.InputResources {
		media := providerMedia{
			ID: resource.ResourceID, Name: resource.Revision.ArtifactID, Type: resource.Kind,
			URL: resource.URL, StorageKey: "resource:" + resource.ResourceID,
			MimeType: resource.MimeType, DurationMs: resource.DurationMS,
		}
		switch resource.Kind {
		case "image":
			input.ReferenceImages = append(input.ReferenceImages, media)
		case "video":
			input.ReferenceVideos = append(input.ReferenceVideos, media)
		case "audio":
			input.ReferenceAudios = append(input.ReferenceAudios, media)
		default:
			return canvasGenerationInput{}, "", 0, errAgentMediaArgumentsInvalid
		}
	}
	switch arguments.Capability {
	case "image":
		parameters, err := decodeAgentImageGenerationParameters(arguments.Parameters)
		if err != nil || !containsString(capabilities.Ratios, parameters.AspectRatio) ||
			!containsString(capabilities.Resolutions, parameters.Resolution) ||
			!containsOptionalString(capabilities.Qualities, parameters.Quality) ||
			!containsIntValue(capabilities.OutputCounts, parameters.Count) || len(input.ReferenceVideos) > 0 || len(input.ReferenceAudios) > 0 ||
			(capabilities.MaxImages > 0 && len(input.ReferenceImages) > capabilities.MaxImages) {
			return canvasGenerationInput{}, "", 0, errAgentMediaArgumentsInvalid
		}
		size, err := productionImageRenderSize(parameters.AspectRatio, parameters.Resolution, capabilities)
		if err != nil {
			return canvasGenerationInput{}, "", 0, errors.Join(errAgentMediaArgumentsInvalid, err)
		}
		input.Prompt = parameters.Prompt
		input.Config.Size = size
		input.Config.Quality = parameters.Quality
		input.Config.Count = strconv.Itoa(parameters.Count)
		input.Config.TransparentBackground = strconv.FormatBool(parameters.TransparentBackground)
		return input, parameters.Prompt, int64(parameters.Count), nil
	case "video":
		parameters, err := decodeAgentVideoGenerationParameters(arguments.Parameters)
		if err != nil {
			return canvasGenerationInput{}, "", 0, errAgentMediaArgumentsInvalid
		}
		if parameters.GenerateAudio && !providerGeneratedAudioSupported(capabilities.SupportsGeneratedAudio, capabilities.GeneratedAudioResolutions, parameters.Resolution) {
			return canvasGenerationInput{}, "", 0, errAgentNativeAudioUnavailable
		}
		if !containsString(capabilities.Ratios, parameters.AspectRatio) ||
			!containsString(capabilities.Resolutions, parameters.Resolution) ||
			(capabilities.DurationMin > 0 && parameters.DurationSeconds < capabilities.DurationMin) ||
			(capabilities.DurationMax > 0 && parameters.DurationSeconds > capabilities.DurationMax) ||
			(capabilities.MaxImages > 0 && len(input.ReferenceImages) > capabilities.MaxImages) ||
			(capabilities.MaxVideos > 0 && len(input.ReferenceVideos) > capabilities.MaxVideos) ||
			(capabilities.MaxAudios > 0 && len(input.ReferenceAudios) > capabilities.MaxAudios) {
			return canvasGenerationInput{}, "", 0, errAgentMediaArgumentsInvalid
		}
		input.Prompt = parameters.Prompt
		input.Config.Size = parameters.AspectRatio
		input.Config.VQuality = parameters.Resolution
		input.Config.VideoSeconds = strconv.Itoa(parameters.DurationSeconds)
		input.Config.VideoGenerateAudio = strconv.FormatBool(parameters.GenerateAudio)
		return input, parameters.Prompt, int64(parameters.DurationSeconds), nil
	case "audio":
		parameters, err := decodeAgentAudioGenerationParameters(arguments.Parameters)
		if err != nil || len(input.ReferenceImages) > 0 || len(input.ReferenceVideos) > 0 ||
			(capabilities.MaxAudios > 0 && len(input.ReferenceAudios) > capabilities.MaxAudios) {
			return canvasGenerationInput{}, "", 0, errAgentMediaArgumentsInvalid
		}
		input.Prompt = parameters.Prompt
		input.Config.AudioVoice = parameters.Voice
		input.Config.AudioFormat = parameters.Format
		input.Config.AudioSpeed = parameters.Speed
		input.Config.AudioVolume = parameters.Volume
		input.Config.AudioPitch = parameters.Pitch
		input.Config.AudioEmotion = parameters.Emotion
		input.Config.AudioLanguageBoost = parameters.LanguageBoost
		input.Config.AudioSampleRate = parameters.SampleRate
		input.Config.AudioBitrate = parameters.Bitrate
		input.Config.AudioChannel = parameters.Channel
		input.Config.AudioInstructions = parameters.Instructions
		return input, parameters.Prompt, 1, nil
	default:
		return canvasGenerationInput{}, "", 0, errAgentMediaArgumentsInvalid
	}
}

func agentMediaAudioModeForArguments(arguments agentMediaGenerationArguments) (agentruntime.MediaAudioMode, error) {
	switch arguments.Capability {
	case "image":
		return agentruntime.MediaAudioNone, nil
	case "audio":
		if _, err := decodeAgentAudioGenerationParameters(arguments.Parameters); err != nil {
			return "", errAgentMediaArgumentsInvalid
		}
		return agentruntime.MediaAudioIndependent, nil
	case "video":
		parameters, err := decodeAgentVideoGenerationParameters(arguments.Parameters)
		if err != nil {
			return "", errAgentMediaArgumentsInvalid
		}
		if parameters.GenerateAudio {
			return agentruntime.MediaAudioNative, nil
		}
		return agentruntime.MediaAudioNone, nil
	default:
		return "", errAgentMediaArgumentsInvalid
	}
}

func decodeAgentImageGenerationParameters(raw json.RawMessage) (agentImageGenerationParameters, error) {
	var parameters agentImageGenerationParameters
	if err := decodeStrictAgentMediaParameters(raw, &parameters); err != nil {
		return parameters, err
	}
	parameters.Prompt = strings.TrimSpace(parameters.Prompt)
	parameters.AspectRatio = strings.TrimSpace(parameters.AspectRatio)
	parameters.Resolution = strings.ToUpper(strings.TrimSpace(parameters.Resolution))
	parameters.Quality = strings.TrimSpace(parameters.Quality)
	if parameters.Prompt == "" || parameters.AspectRatio == "" || parameters.Resolution == "" || parameters.Count < 1 {
		return parameters, errAgentMediaArgumentsInvalid
	}
	return parameters, nil
}

func decodeAgentVideoGenerationParameters(raw json.RawMessage) (agentVideoGenerationParameters, error) {
	var parameters agentVideoGenerationParameters
	if err := decodeStrictAgentMediaParameters(raw, &parameters); err != nil {
		return parameters, err
	}
	parameters.Prompt = strings.TrimSpace(parameters.Prompt)
	parameters.AspectRatio = strings.TrimSpace(parameters.AspectRatio)
	parameters.Resolution = strings.TrimSpace(parameters.Resolution)
	if parameters.Prompt == "" || parameters.AspectRatio == "" || parameters.Resolution == "" || parameters.DurationSeconds < 1 {
		return parameters, errAgentMediaArgumentsInvalid
	}
	return parameters, nil
}

func decodeAgentAudioGenerationParameters(raw json.RawMessage) (agentAudioGenerationParameters, error) {
	var parameters agentAudioGenerationParameters
	if err := decodeStrictAgentMediaParameters(raw, &parameters); err != nil {
		return parameters, err
	}
	parameters.Prompt = strings.TrimSpace(parameters.Prompt)
	parameters.Voice = strings.TrimSpace(parameters.Voice)
	if parameters.Prompt == "" || parameters.Voice == "" {
		return parameters, errAgentMediaArgumentsInvalid
	}
	return parameters, nil
}

type agentMediaParameterSet interface {
	agentImageGenerationParameters | agentVideoGenerationParameters | agentAudioGenerationParameters
}

func decodeStrictAgentMediaParameters[T agentMediaParameterSet](raw json.RawMessage, target *T) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errAgentMediaArgumentsInvalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errAgentMediaArgumentsInvalid
	}
	return nil
}

func containsOptionalString(values []string, value string) bool {
	if len(values) == 0 {
		return value == ""
	}
	return containsString(values, value)
}

func containsIntValue(values []int, value int) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func cloneAgentRuntimeSkillSelections(values []agentruntime.SkillSelection) []agentruntime.SkillSelection {
	if values == nil {
		return []agentruntime.SkillSelection{}
	}
	cloned := make([]agentruntime.SkillSelection, len(values))
	copy(cloned, values)
	return cloned
}

func cloneDirectedVideoRegenerationArguments(value *DirectedVideoRegenerationArguments) *DirectedVideoRegenerationArguments {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func decodeFrozenAgentMediaGenerationArguments(raw json.RawMessage) (agentMediaGenerationArguments, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var arguments agentMediaGenerationArguments
	if err := decoder.Decode(&arguments); err != nil {
		return agentMediaGenerationArguments{}, errAgentMediaArgumentsInvalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || validateFrozenAgentMediaGenerationArguments(arguments) != nil {
		return agentMediaGenerationArguments{}, errAgentMediaArgumentsInvalid
	}
	return arguments, nil
}

func validateFrozenAgentMediaGenerationArguments(arguments agentMediaGenerationArguments) error {
	if arguments.InputRevisions == nil || arguments.InputResources == nil ||
		validateAgentRuntimeRevisionRefs(arguments.InputRevisions) != nil ||
		strings.TrimSpace(arguments.GenerationModel.ChannelID) == "" || strings.TrimSpace(arguments.GenerationModel.Model) == "" ||
		strings.TrimSpace(arguments.GenerationModelRecordID) == "" ||
		(arguments.Capability != "image" && arguments.Capability != "video" && arguments.Capability != "audio") ||
		!validStoredText(arguments.OutputArtifactID, 80) || !validStoredText(arguments.OutputArtifactKey, 120) ||
		arguments.ExpectedOutputSchema != agentMediaCandidateSchema || arguments.ExpectedDelivery.Validate() != nil ||
		!expectedDeliveryRequiresMedia(arguments.ExpectedDelivery, arguments.Capability) ||
		!validStoredText(arguments.RequestIdentity, 180) || arguments.SkillVersions == nil ||
		arguments.Commercial.Attempt < 1 || arguments.Commercial.Attempt >= 1_000_000 ||
		arguments.Commercial.ArtifactRevisionID != arguments.OutputArtifactID || arguments.Commercial.Capability != arguments.Capability ||
		arguments.Commercial.ChannelID != arguments.GenerationModel.ChannelID || arguments.Commercial.ModelKey != arguments.GenerationModel.Model ||
		arguments.Commercial.ChannelModelID != arguments.GenerationModelRecordID || arguments.Commercial.QuoteID == "" ||
		arguments.Commercial.ApprovalFingerprint == "" || arguments.Commercial.ExpiresAt.IsZero() {
		return errAgentMediaArgumentsInvalid
	}
	if arguments.DirectedRegeneration != nil &&
		(arguments.Capability != "video" || arguments.DirectedRegeneration.SourceShotRevision.Validate() != nil ||
			arguments.DirectedRegeneration.ApprovedCandidateRevision.Validate() != nil) {
		return errAgentMediaArgumentsInvalid
	}
	resourceIndex := 0
	for _, revision := range arguments.InputRevisions {
		if resourceIndex >= len(arguments.InputResources) || arguments.InputResources[resourceIndex].Revision != revision {
			continue
		}
		resource := arguments.InputResources[resourceIndex]
		if resource.Revision.Validate() != nil ||
			!validStoredText(resource.ResourceID, 80) || !validStoredText(resource.ObjectKey, 2048) ||
			!validStoredText(resource.ETag, 160) || (resource.Kind != "image" && resource.Kind != "video" && resource.Kind != "audio") {
			return errAgentMediaArgumentsInvalid
		}
		resourceIndex++
	}
	if resourceIndex != len(arguments.InputResources) {
		return errAgentMediaArgumentsInvalid
	}
	return nil
}

func (s *Service) currentAgentMediaGenerationCommand(scope agentruntime.Scope, arguments agentMediaGenerationArguments) (MediaGenerationCommand, error) {
	if validateFrozenAgentMediaGenerationArguments(arguments) != nil {
		return MediaGenerationCommand{}, errAgentMediaArgumentsInvalid
	}
	configured, err := s.requireAccessibleChannelModel(scope.ActorUserID, arguments.GenerationModel.ChannelID, arguments.GenerationModel.Model)
	if err != nil || configured.ID != arguments.GenerationModelRecordID || configured.Capability != arguments.Capability {
		return MediaGenerationCommand{}, errors.Join(errAgentMediaModelUnavailable, err)
	}
	currentRevisions := make(map[agentruntime.ArtifactRevisionRef]model.AgentArtifactRevision, len(arguments.InputRevisions))
	for _, reference := range arguments.InputRevisions {
		revision, err := s.repo.ArtifactRevisionForArtifactInScope(scope, reference.ArtifactID, reference.RevisionID)
		if err != nil {
			return MediaGenerationCommand{}, errors.Join(errAgentMediaInputChanged, err)
		}
		head, err := s.repo.ArtifactHeadRevisionForScope(scope, reference.ArtifactID)
		if err != nil || head.ID != revision.ID || revision.LifecycleStatus == model.AgentArtifactRevisionStale {
			return MediaGenerationCommand{}, errors.Join(errAgentMediaInputChanged, err)
		}
		currentRevisions[reference] = *revision
	}
	for _, frozen := range arguments.InputResources {
		revision, found := currentRevisions[frozen.Revision]
		if !found || revision.ResourceID != frozen.ResourceID {
			return MediaGenerationCommand{}, errAgentMediaInputChanged
		}
		resource, err := s.productionResourceForScope(scope, frozen.ResourceID)
		if err != nil || !agentMediaResourceReady(resource) || resource.Kind != frozen.Kind || resource.ObjectKey != frozen.ObjectKey ||
			resource.ETag != frozen.ETag || resource.MimeType != frozen.MimeType || resource.DurationMs != frozen.DurationMS {
			return MediaGenerationCommand{}, errors.Join(errAgentMediaInputChanged, err)
		}
	}
	if arguments.DirectedRegeneration != nil {
		attempt, attemptErr := s.directedVideoRegenerationAttempt(scope, arguments.InputRevisions, *arguments.DirectedRegeneration)
		if attemptErr != nil || attempt != arguments.Commercial.Attempt {
			return MediaGenerationCommand{}, errors.Join(errDirectedVideoRegenerationInvalid, attemptErr)
		}
	}
	capabilities, err := decodeFrozenMediaCapabilities(arguments.Commercial.ProviderCapabilitiesJSON)
	if err != nil {
		return MediaGenerationCommand{}, errAgentMediaArgumentsInvalid
	}
	currentCapabilities, err := s.currentAgentMediaProviderCapabilities(arguments.GenerationModel)
	if err != nil {
		return MediaGenerationCommand{}, errors.Join(errAgentMediaModelUnavailable, err)
	}
	frozenCapabilitiesJSON, err := canonicalMediaCapabilities(capabilities)
	if err != nil {
		return MediaGenerationCommand{}, errors.Join(errAgentMediaArgumentsInvalid, err)
	}
	currentCapabilitiesJSON, err := canonicalMediaCapabilities(currentCapabilities)
	if err != nil {
		return MediaGenerationCommand{}, errors.Join(errAgentMediaModelUnavailable, err)
	}
	if !bytes.Equal(frozenCapabilitiesJSON, currentCapabilitiesJSON) {
		return MediaGenerationCommand{}, errAgentMediaCapabilitiesChanged
	}
	return agentMediaGenerationCommand(scope, arguments, capabilities)
}

func (s *Service) currentAgentMediaProviderCapabilities(selection agentruntime.GenerationModelSelection) (*PublicProviderCapabilities, error) {
	channel, err := s.repo.SystemChannel(selection.ChannelID)
	if err != nil {
		return nil, err
	}
	capabilities := publicProviderModelCapabilities(channel.InterfaceType, selection.Model)
	if capabilities == nil {
		return nil, errAgentMediaModelUnavailable
	}
	return capabilities, nil
}

func decodeFrozenMediaCapabilities(raw string) (*PublicProviderCapabilities, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var capabilities PublicProviderCapabilities
	if err := decoder.Decode(&capabilities); err != nil {
		return nil, err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errAgentMediaArgumentsInvalid
	}
	return &capabilities, nil
}

func (s *Service) ensureAgentMediaGenerationTask(
	scope agentruntime.Scope,
	record *model.AgentToolCall,
	arguments agentMediaGenerationArguments,
) (*model.Task, *model.BillingOrder, error) {
	if record == nil || record.Status != agentruntime.ToolCallRunning || strings.TrimSpace(record.IdempotencyKey) == "" ||
		record.ApprovalDecision != agentruntime.ToolApprovalApproved || record.ApprovalDecidedAt == nil {
		return nil, nil, errAgentMediaArgumentsInvalid
	}
	command, err := s.currentAgentMediaGenerationCommand(scope, arguments)
	if err != nil {
		return nil, nil, err
	}
	approved, err := s.ApproveMediaAttempt(scope, arguments.Commercial, command, record.ApprovalDecidedAt.UTC())
	if err != nil {
		return nil, nil, err
	}
	_, order, err := s.EnsureMediaTask(context.Background(), scope, *approved)
	if err != nil {
		return nil, nil, err
	}
	task, err := s.repo.TaskForUser(scope.ActorUserID, approved.TaskID)
	if err != nil {
		return nil, nil, err
	}
	return taskForOutput(*task), order, nil
}

func (s *Service) materializeAgentMediaCandidates(
	scope agentruntime.Scope,
	task model.Task,
	arguments agentMediaGenerationArguments,
	call *agentruntime.ToolCallDecision,
) ([]model.AgentArtifactRevision, repository.MediaAttemptWriteDisposition, error) {
	if scope.Validate() != nil || call == nil || call.ToolName != agentruntime.ToolMediaGenerate ||
		task.Status != model.TaskStatusSucceeded || task.UserID != scope.ActorUserID || task.ProjectID != scope.CanvasID ||
		task.ID != arguments.Commercial.TaskID {
		return nil, "", errAgentMediaResultInvalid
	}
	resources, err := agentruntime.DecodeMediaTaskResultResources([]byte(task.ResultJSON))
	if err != nil {
		return nil, "", errAgentMediaResultInvalid
	}
	inputs := make([]repository.MediaCandidateAttemptInput, 0, len(resources))
	for index, result := range resources {
		resource, loadErr := s.productionResourceForScope(scope, result.ResourceID)
		if loadErr != nil || !agentMediaResourceReady(resource) || resource.Kind != string(result.Kind) {
			return nil, "", errors.Join(errAgentMediaResultInvalid, loadErr)
		}
		artifactID := fmt.Sprintf("%s-%02d", arguments.OutputArtifactID, index+1)
		artifactKey := fmt.Sprintf("%s-candidate-%02d", arguments.OutputArtifactKey, index+1)
		draft, draftErr := mediaCandidateDraft(GeneratedMediaCandidate{
			ArtifactID: artifactID, ArtifactKey: artifactKey, MediaKind: string(result.Kind),
			ResourceID: result.ResourceID, SourceTaskID: task.ID,
			ProviderRequestIdentity: fmt.Sprintf("%s:%02d", arguments.RequestIdentity, index+1),
			UpstreamRevisions:       cloneAgentMediaRevisionRefs(arguments.InputRevisions),
			SkillVersions:           cloneAgentRuntimeSkillSelections(arguments.SkillVersions),
		})
		if draftErr != nil {
			return nil, "", draftErr
		}
		inputs = append(inputs, repository.MediaCandidateAttemptInput{ArtifactID: artifactID, Draft: draft})
	}
	results, err := s.repo.AppendMediaCandidateRevisionsForAttempt(scope, inputs, repository.MediaAttemptCompletionFence{
		ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion,
		ExpectedTaskID: task.ID, ExpectedAttempt: arguments.Commercial.Attempt,
		ExpectedArtifactRevisionID: arguments.Commercial.ArtifactRevisionID,
		ApprovalFingerprint:        arguments.Commercial.ApprovalFingerprint,
	}, time.Now().UTC())
	if err != nil {
		return nil, "", err
	}
	revisions := make([]model.AgentArtifactRevision, 0, len(results))
	disposition := results[0].Disposition
	for _, result := range results {
		if result.Disposition != disposition {
			return nil, "", repository.ErrMediaAttemptFenceConflict
		}
		revisions = append(revisions, result.Revision)
	}
	return revisions, disposition, nil
}

func cloneAgentMediaRevisionRefs(values []agentruntime.ArtifactRevisionRef) []agentruntime.ArtifactRevisionRef {
	if values == nil {
		return []agentruntime.ArtifactRevisionRef{}
	}
	cloned := make([]agentruntime.ArtifactRevisionRef, len(values))
	copy(cloned, values)
	return cloned
}
