package service

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

func TestExpectedDeliveryRequiresExactApprovedMediaRevisionAndReadyResource(t *testing.T) {
	legacy := agentruntime.ExpectedDelivery{
		Kind:              agentruntime.DeliveryGeneratedAsset,
		RequiredArtifacts: []agentruntime.ArtifactKind{agentruntime.ArtifactVideo},
		CompletionCriteria: []agentruntime.DeliveryCriterion{{
			Fact: agentruntime.DeliveryFactArtifact, Artifact: agentruntime.ArtifactVideo,
		}},
	}
	if expectedDeliveryRequiresMedia(legacy, "video") {
		t.Fatal("legacy URL-only delivery contract accepted for governed media generation")
	}

	exact := exactAgentMediaExpectedDelivery(agentruntime.ArtifactVideo)
	if !expectedDeliveryRequiresMedia(exact, "video") {
		t.Fatal("exact revision and ready resource delivery contract rejected")
	}
}

func exactAgentMediaExpectedDelivery(kind agentruntime.ArtifactKind) agentruntime.ExpectedDelivery {
	return agentruntime.ExpectedDelivery{
		Kind:              agentruntime.DeliveryGeneratedAsset,
		RequiredArtifacts: []agentruntime.ArtifactKind{kind},
		CompletionCriteria: []agentruntime.DeliveryCriterion{
			{Fact: agentruntime.DeliveryFactArtifactRevision, Artifact: kind},
			{Fact: agentruntime.DeliveryFactResource, Artifact: kind},
		},
	}
}

func TestFreezeAgentMediaGenerationBindsExactInputsModelAndQuote(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeImageModel(t, db, fixture)
	scope := agentRuntimeServiceScope()
	scope.DomainProjectID = "runtime-project"
	resource := seedAgentMediaInputResource(t, db, scope, "media-input-resource", "image", "inputs/reference.png", "etag-reference")
	revision := seedAgentMediaInputRevision(t, svc, scope, "media-input-artifact", "media-input", resource.ID)
	callable := agentRuntimeCallableModelFact{
		ChannelID: "runtime-image-channel", Model: "kz_gpt_image2", DisplayName: "GPT Image 2",
		Capability: "image", BillingMode: "fixed_request", PriceStrategy: "image_resolution",
		UnitPriceMicrocredits: 250,
		ProviderCapabilities:  agentRuntimeGPTImageCapabilitiesForTest(t),
	}
	proposal := MediaGenerateArguments{
		InputRevisions:    []agentruntime.ArtifactRevisionRef{{ArtifactID: revision.ArtifactID, RevisionID: revision.ID}},
		GenerationModel:   agentruntime.GenerationModelSelection{ChannelID: callable.ChannelID, Model: callable.Model},
		Capability:        "image",
		Parameters:        json.RawMessage(`{"prompt":"角色定妆照","aspectRatio":"1:1","resolution":"1K","quality":"medium","count":1}`),
		OutputArtifactKey: "character-portrait", ExpectedOutputSchema: agentMediaCandidateSchema,
		ExpectedDelivery: exactAgentMediaExpectedDelivery(agentruntime.ArtifactImage),
	}
	call := &agentruntime.ToolCallDecision{
		ToolCallID: "media-call-1", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
		ExpectedDelivery: proposal.ExpectedDelivery,
	}
	call.Arguments = mustMarshalAgentMediaTestJSON(t, proposal)

	frozenJSON, err := svc.freezeAgentMediaGenerationDecisionArguments(
		scope,
		agentruntime.RunConfiguration{
			ExecutionMode: agentruntime.ExecutionGuided,
			GenerationModels: agentruntime.GenerationModelSelections{
				Image: &proposal.GenerationModel,
			},
			Skills: []agentruntime.SkillSelection{},
		},
		[]agentRuntimeCallableModelFact{callable},
		call,
	)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := decodeFrozenAgentMediaGenerationArguments(frozenJSON)
	if err != nil {
		t.Fatal(err)
	}
	if frozen.GenerationModelRecordID != "runtime-image-model" || frozen.Commercial.ChannelModelID != "runtime-image-model" {
		t.Fatalf("frozen model records = %q / %q", frozen.GenerationModelRecordID, frozen.Commercial.ChannelModelID)
	}
	if len(frozen.InputResources) != 1 || frozen.InputResources[0].ResourceID != resource.ID ||
		frozen.InputResources[0].ObjectKey != resource.ObjectKey || frozen.InputResources[0].ETag != resource.ETag {
		t.Fatalf("frozen input resources = %#v", frozen.InputResources)
	}
	if frozen.Commercial.QuoteID == "" || frozen.Commercial.ApprovalFingerprint == "" || frozen.RequestIdentity == "" {
		t.Fatalf("frozen approval facts are incomplete: %#v", frozen)
	}

	if err := db.Model(&model.Resource{}).Where("id = ?", resource.ID).Update("object_key", "inputs/changed.png").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.currentAgentMediaGenerationCommand(scope, frozen); !errors.Is(err, errAgentMediaInputChanged) {
		t.Fatalf("changed resource error = %v, want %v", err, errAgentMediaInputChanged)
	}
}

func TestAgentMediaGenerationMaterializesEveryCandidateExactlyOnce(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	scope := agentRuntimeServiceScope()
	scope.DomainProjectID = "runtime-project"
	image := seedAgentMediaInputResource(t, db, scope, "generated-image", "image", "outputs/image.png", "etag-image")
	video := seedAgentMediaInputResource(t, db, scope, "generated-video", "video", "outputs/video.mp4", "etag-video")
	audio := seedAgentMediaInputResource(t, db, scope, "generated-audio", "audio", "outputs/audio.mp3", "etag-audio")
	arguments := agentMediaGenerationArguments{
		InputRevisions: []agentruntime.ArtifactRevisionRef{}, InputResources: []agentMediaInputResource{},
		GenerationModel:         agentruntime.GenerationModelSelection{ChannelID: "channel", Model: "model"},
		GenerationModelRecordID: "model-record", Capability: "image",
		Parameters:       json.RawMessage(`{"prompt":"候选资产","aspectRatio":"1:1","resolution":"1K","quality":"medium","count":1}`),
		OutputArtifactID: "media-output-root", OutputArtifactKey: "media-output",
		ExpectedOutputSchema: agentMediaCandidateSchema,
		ExpectedDelivery:     exactAgentMediaExpectedDelivery(agentruntime.ArtifactImage),
		RequestIdentity:      "media-generation:request-1", SkillVersions: []agentruntime.SkillSelection{},
	}
	resultJSON := `{"images":[{"resourceId":"generated-image"}],"videos":[{"resourceId":"generated-video"}],"audio":{"resourceId":"generated-audio"}}`
	task := model.Task{ID: "media-task", UserID: scope.ActorUserID, ProjectID: scope.CanvasID, Status: model.TaskStatusSucceeded, ResultJSON: resultJSON}

	first, err := svc.materializeAgentMediaCandidates(scope, task, arguments)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.materializeAgentMediaCandidates(scope, task, arguments)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 3 || len(second) != 3 {
		t.Fatalf("candidate counts first=%d second=%d", len(first), len(second))
	}
	for index := range first {
		if first[index].ID != second[index].ID {
			t.Fatalf("candidate replay %d = %q, want %q", index, second[index].ID, first[index].ID)
		}
	}
	stored, err := svc.repo.MediaCandidateRevisionsInScope(scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 3 {
		t.Fatalf("stored candidate count = %d, want 3", len(stored))
	}
	wantResources := map[string]string{"image": image.ID, "video": video.ID, "audio": audio.ID}
	for index := range stored {
		var payload agentruntime.MediaCandidateContent
		if err := json.Unmarshal([]byte(stored[index].PayloadJSON), &payload); err != nil {
			t.Fatal(err)
		}
		if wantResources[string(payload.MediaKind)] != payload.ResourceID {
			t.Fatalf("candidate %d payload = %#v", index, payload)
		}
	}
}

func TestAgentMediaGenerationOperationWakesOwningRun(t *testing.T) {
	want := "runtime-run"
	operation := agentMediaGenerationOperationForRun(want)
	if got, ok := agentMediaGenerationRunID(operation); !ok || got != want {
		t.Fatalf("operation parser = %q/%v, want %q/true", got, ok, want)
	}
	for _, invalid := range []string{"", "media_generation:", "other:" + want} {
		if got, ok := agentMediaGenerationRunID(invalid); ok || got != "" {
			t.Fatalf("invalid operation %q parsed as %q/%v", invalid, got, ok)
		}
	}
}

func TestNativeAudioVideoUsesOneVideoTaskWithoutIndependentAudioArtifact(t *testing.T) {
	arguments := agentMediaGenerationArguments{
		Capability: "video", GenerationModel: agentruntime.GenerationModelSelection{ChannelID: "video-channel", Model: "video-model"},
		Parameters: json.RawMessage(`{"prompt":"角色说出对白","aspectRatio":"16:9","resolution":"720p","durationSeconds":5,"generateAudio":true}`),
	}
	capabilities := &PublicProviderCapabilities{
		Capability: "video", ModelKey: "video-model", Ratios: []string{"16:9"}, Resolutions: []string{"720p"},
		DurationMin: 1, DurationMax: 15, SupportsGeneratedAudio: true, GeneratedAudioResolutions: []string{"720p"},
	}
	input, _, _, err := buildAgentMediaGenerationTaskInput(arguments, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	mode, err := agentMediaAudioModeForArguments(arguments)
	if err != nil {
		t.Fatal(err)
	}
	if mode != agentruntime.MediaAudioNative || input.Mode != "video" || input.Config.VideoGenerateAudio != "true" {
		t.Fatalf("native audio task = mode %q input %#v", mode, input)
	}
	if len(input.ReferenceAudios) != 0 {
		t.Fatalf("native audio task created independent audio inputs: %#v", input.ReferenceAudios)
	}
}

func TestNativeAudioVideoRejectsModelWithoutFrozenCapability(t *testing.T) {
	arguments := agentMediaGenerationArguments{
		Capability: "video", GenerationModel: agentruntime.GenerationModelSelection{ChannelID: "video-channel", Model: "video-model"},
		Parameters: json.RawMessage(`{"prompt":"角色说出对白","aspectRatio":"16:9","resolution":"720p","durationSeconds":5,"generateAudio":true}`),
	}
	capabilities := &PublicProviderCapabilities{
		Capability: "video", ModelKey: "video-model", Ratios: []string{"16:9"}, Resolutions: []string{"720p"},
		DurationMin: 1, DurationMax: 15,
	}
	if _, _, _, err := buildAgentMediaGenerationTaskInput(arguments, capabilities); !errors.Is(err, errAgentNativeAudioUnavailable) {
		t.Fatalf("native audio capability error = %v, want %v", err, errAgentNativeAudioUnavailable)
	}
	code, class, ok := agentMediaGenerationFailureDetails(errAgentNativeAudioUnavailable)
	if !ok || code != "native_audio_capability_unavailable" || class != agentruntime.ToolFailureAgentRepairable {
		t.Fatalf("native audio failure classification = %q/%q/%v", code, class, ok)
	}
}

func TestIndependentAudioUsesSeparateAudioCapabilityAndQuoteIdentity(t *testing.T) {
	arguments := agentMediaGenerationArguments{
		Capability: "audio", GenerationModel: agentruntime.GenerationModelSelection{ChannelID: "audio-channel", Model: "speech-model"},
		Parameters: json.RawMessage(`{"prompt":"别回头。","voice":"hero-voice"}`),
		Commercial: MediaGenerationAttempt{QuoteID: "audio-quote"},
	}
	capabilities := &PublicProviderCapabilities{Capability: "audio", ModelKey: "speech-model", SupportsAudioOnly: true}
	input, _, quantity, err := buildAgentMediaGenerationTaskInput(arguments, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	mode, err := agentMediaAudioModeForArguments(arguments)
	if err != nil {
		t.Fatal(err)
	}
	if mode != agentruntime.MediaAudioIndependent || input.Mode != "audio" || quantity != 1 || arguments.Commercial.QuoteID == "" {
		t.Fatalf("independent audio task = mode %q quantity %d input %#v quote %q", mode, quantity, input, arguments.Commercial.QuoteID)
	}
}

func TestCoordinatePendingAgentMediaGenerationCreatesInternalTaskAndResolvesCandidates(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeCanvas(t, db)
	createAgentRuntimeImageModel(t, db, fixture)
	scope := agentRuntimeServiceScope()
	scope.DomainProjectID = "runtime-project"
	now := time.Now().UTC()
	if err := db.Create(&model.Project{
		ID: scope.DomainProjectID, UserID: scope.ActorUserID, Name: "Media Runtime Project",
		Type: "short-drama", Status: model.ProjectStatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", scope.CanvasID).
		Update("project_id", scope.DomainProjectID).Error; err != nil {
		t.Fatal(err)
	}
	selection := agentruntime.GenerationModelSelection{ChannelID: "runtime-image-channel", Model: "kz_gpt_image2"}
	configuration := agentruntime.RunConfiguration{
		ExecutionMode:    agentruntime.ExecutionGuided,
		GenerationModels: agentruntime.GenerationModelSelections{Image: &selection},
		Skills:           []agentruntime.SkillSelection{},
	}
	if _, err := svc.repo.CreateInitializedAgentRun(repository.CreateInitializedAgentRunInput{
		Create: repository.CreateAgentRunInput{Scope: scope, ClientRequestID: "media-coordinator", Now: now},
		Initialize: repository.InitializeAgentRunInput{
			Scope: scope, ModelRecordID: fixture.channelModel.ID, ModelKey: fixture.channelModel.ModelKey,
			MaxSteps: 8, ToolSchemaVersion: agentruntime.ProductionToolSchemaVersion,
			RuntimeVersion: agentruntime.ProductionRuntimeVersion, PolicyVersion: agentruntime.ProductionPolicyVersion,
			UserMessage: "生成角色图", Configuration: configuration, Now: now,
		},
	}); err != nil {
		t.Fatal(err)
	}
	queued, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	running, err := agentruntime.BeginModelRequest(queued)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CommitAgentRuntimeTransition(scope, queued, running, now); err != nil {
		t.Fatal(err)
	}
	delivery := exactAgentMediaExpectedDelivery(agentruntime.ArtifactImage)
	proposal := MediaGenerateArguments{
		InputRevisions: []agentruntime.ArtifactRevisionRef{}, GenerationModel: selection, Capability: "image",
		Parameters:        json.RawMessage(`{"prompt":"角色定妆照","aspectRatio":"1:1","resolution":"1K","quality":"medium","count":1}`),
		OutputArtifactKey: "character-portrait", ExpectedOutputSchema: agentMediaCandidateSchema, ExpectedDelivery: delivery,
	}
	callable := agentRuntimeCallableModelFact{
		ChannelID: selection.ChannelID, Model: selection.Model, DisplayName: "GPT Image 2", Capability: "image",
		BillingMode: "fixed_request", PriceStrategy: "image_resolution", UnitPriceMicrocredits: 250,
		ProviderCapabilities: agentRuntimeGPTImageCapabilitiesForTest(t),
	}
	toolDecision := agentruntime.ToolCallDecision{
		ToolCallID: "media-coordinator", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
		ExpectedDelivery: delivery,
	}
	toolDecision.Arguments = mustMarshalAgentMediaTestJSON(t, proposal)
	frozen, err := svc.freezeAgentMediaGenerationDecisionArguments(scope, configuration, []agentRuntimeCallableModelFact{callable}, &toolDecision)
	if err != nil {
		t.Fatal(err)
	}
	toolDecision.Arguments = frozen
	waitingApproval, err := agentruntime.AdvanceForToolSchema(running.State, agentruntime.RuntimeInput{
		Decision: agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &toolDecision},
	}, agentruntime.ProductionToolSchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CommitAgentRuntimeTransition(scope, running.State, waitingApproval, now); err != nil {
		t.Fatal(err)
	}
	approved, err := agentruntime.ReviewToolApproval(waitingApproval.State, agentruntime.ToolApproval{
		ToolCallID: toolDecision.ToolCallID, ActionVersion: toolDecision.ActionVersion, Decision: agentruntime.ToolApprovalApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CommitAgentRuntimeTransition(scope, waitingApproval.State, approved, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	progress, err := svc.coordinatePendingAgentTool(scope, CoordinateAgentToolInput{
		ToolCallID: toolDecision.ToolCallID, ActionVersion: toolDecision.ActionVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Status != agentruntime.RunWaitingTool || !progress.State.PendingToolStarted {
		t.Fatalf("media coordinator start state = %#v result=%#v", progress.State, progress.State.LastToolResult)
	}
	var task model.Task
	if err := db.Where("operation = ?", agentMediaGenerationOperationForRun(scope.RunID)).First(&task).Error; err != nil {
		t.Fatal(err)
	}
	if task.Audience != model.TaskAudienceInternal {
		t.Fatalf("media task audience = %q", task.Audience)
	}
	resource := seedAgentMediaInputResource(t, db, scope, "media-coordinator-result", "image", "outputs/result.png", "etag-result")
	resultJSON := `{"images":[{"resourceId":"` + resource.ID + `"}]}`
	if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Updates(map[string]interface{}{
		"status": model.TaskStatusSucceeded, "result_json": resultJSON,
	}).Error; err != nil {
		t.Fatal(err)
	}
	progress, err = svc.coordinatePendingAgentTool(scope, CoordinateAgentToolInput{
		ToolCallID: toolDecision.ToolCallID, ActionVersion: toolDecision.ActionVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := svc.repo.AgentToolCallForScope(scope, toolDecision.ToolCallID, toolDecision.ActionVersion)
	if err != nil {
		t.Fatal(err)
	}
	if recorded.Status != agentruntime.ToolCallSucceeded || progress.State.Status != agentruntime.RunRunning {
		t.Fatalf("media coordinator completion = status %q state %q", recorded.Status, progress.State.Status)
	}
	stored, err := svc.repo.MediaCandidateRevisionsInScope(scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].ResourceID != resource.ID {
		t.Fatalf("media candidates = %#v", stored)
	}
}

func TestCurrentAgentMediaGenerationCommandRejectsChangedProviderCapabilities(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeImageModel(t, db, fixture)
	scope := agentRuntimeServiceScope()
	selection := agentruntime.GenerationModelSelection{ChannelID: "runtime-image-channel", Model: "kz_gpt_image2"}
	delivery := exactAgentMediaExpectedDelivery(agentruntime.ArtifactImage)
	proposal := MediaGenerateArguments{
		InputRevisions: []agentruntime.ArtifactRevisionRef{}, GenerationModel: selection, Capability: "image",
		Parameters:        json.RawMessage(`{"prompt":"角色定妆照","aspectRatio":"1:1","resolution":"1K","quality":"medium","count":1}`),
		OutputArtifactKey: "character-portrait", ExpectedOutputSchema: agentMediaCandidateSchema, ExpectedDelivery: delivery,
	}
	call := &agentruntime.ToolCallDecision{
		ToolCallID: "media-capability-drift", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
		ExpectedDelivery: delivery,
	}
	call.Arguments = mustMarshalAgentMediaTestJSON(t, proposal)
	callable := agentRuntimeCallableModelFact{
		ChannelID: selection.ChannelID, Model: selection.Model, DisplayName: "GPT Image 2", Capability: "image",
		BillingMode: "fixed_request", PriceStrategy: "image_resolution", UnitPriceMicrocredits: 250,
		ProviderCapabilities: agentRuntimeGPTImageCapabilitiesForTest(t),
	}
	frozenJSON, err := svc.freezeAgentMediaGenerationDecisionArguments(
		scope,
		agentruntime.RunConfiguration{
			ExecutionMode:    agentruntime.ExecutionGuided,
			GenerationModels: agentruntime.GenerationModelSelections{Image: &selection},
			Skills:           []agentruntime.SkillSelection{},
		},
		[]agentRuntimeCallableModelFact{callable},
		call,
	)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := decodeFrozenAgentMediaGenerationArguments(frozenJSON)
	if err != nil {
		t.Fatal(err)
	}
	quotedCapabilities, err := decodeFrozenMediaCapabilities(frozen.Commercial.ProviderCapabilitiesJSON)
	if err != nil {
		t.Fatal(err)
	}
	quotedCapabilities.MaxImages++
	changedCapabilities, err := canonicalMediaCapabilities(quotedCapabilities)
	if err != nil {
		t.Fatal(err)
	}
	frozen.Commercial.ProviderCapabilitiesJSON = string(changedCapabilities)
	frozen.Commercial.ApprovalFingerprint, err = mediaApprovalFingerprint(scope, frozen.Commercial)
	if err != nil {
		t.Fatal(err)
	}
	frozen.Commercial.QuoteID = mediaQuoteID(frozen.Commercial.ApprovalFingerprint)

	if _, err := svc.currentAgentMediaGenerationCommand(scope, frozen); err == nil {
		t.Fatal("currentAgentMediaGenerationCommand accepted provider capability drift")
	}
}

func agentRuntimeGPTImageCapabilitiesForTest(t *testing.T) *PublicProviderCapabilities {
	t.Helper()
	capabilities := publicProviderModelCapabilities(model.ChannelInterfaceOpenAIImage, "kz_gpt_image2")
	if capabilities == nil {
		t.Fatal("GPT Image 2 provider capabilities are unavailable")
	}
	return capabilities
}

func seedAgentMediaInputResource(
	t *testing.T,
	db *gorm.DB,
	scope agentruntime.Scope,
	id string,
	kind string,
	objectKey string,
	etag string,
) model.Resource {
	t.Helper()
	now := time.Now().UTC()
	resource := model.Resource{
		ID: id, UserID: scope.ActorUserID, Kind: kind, Status: model.ResourceStatusReady,
		Provider: "oss", Endpoint: "oss.example.com", Bucket: "agent-media", ObjectKey: objectKey,
		MimeType: "application/octet-stream", ETag: etag, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	return resource
}

func seedAgentMediaInputRevision(
	t *testing.T,
	svc *Service,
	scope agentruntime.Scope,
	artifactID string,
	artifactKey string,
	resourceID string,
) model.AgentArtifactRevision {
	t.Helper()
	revision, err := svc.repo.AppendArtifactRevisionOnce(scope, artifactID, agentruntime.ArtifactDraft{
		ArtifactKey: artifactKey, Kind: "uploaded_media", SchemaVersion: 1,
		Payload: json.RawMessage(`{"source":"user"}`), ResourceID: resourceID,
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{}, SkillVersions: []agentruntime.SkillSelection{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return *revision
}

func mustMarshalAgentMediaTestJSON(t *testing.T, value interface{}) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
