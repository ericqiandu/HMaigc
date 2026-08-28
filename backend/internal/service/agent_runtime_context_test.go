package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/skillcatalog"

	"gorm.io/gorm"
)

func TestAgentRuntimeFreezesSeededFirstPartySkillVersion(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	configuration, err := svc.resolveAgentRuntimeConfiguration(context.Background(), agentRuntimeServiceScope().ActorUserID, AgentRuntimeConfigurationInput{
		SkillDirs: []string{"short-drama-director"}, ExecutionMode: agentruntime.ExecutionAutomatic,
	})
	if err != nil {
		t.Fatal(err)
	}
	builtins, err := skillcatalog.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	var expected skillcatalog.BuiltinSkill
	for _, builtin := range builtins {
		if builtin.Dir == "short-drama-director" {
			expected = builtin
			break
		}
	}
	if len(configuration.Skills) != 1 {
		t.Fatalf("frozen seeded skills = %#v", configuration.Skills)
	}
	frozen := configuration.Skills[0]
	frozenManifestJSON, err := json.Marshal(frozen.CapabilityManifest)
	if err != nil {
		t.Fatal(err)
	}
	if frozen.Dir != expected.Dir || frozen.Version != expected.Version || frozen.Instructions != expected.Instructions || frozen.Checksum != expected.Checksum ||
		string(frozenManifestJSON) != expected.CapabilityManifestJSON || frozen.SourceLicense != expected.SourceLicense {
		t.Fatalf("frozen seeded skill = %#v; expected version=%d checksum=%s", frozen, expected.Version, expected.Checksum)
	}
}

func TestResolveAgentRuntimeSkillSelectionsForSpecialistRejectsCapabilityMismatch(t *testing.T) {
	svc, _, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	svc.agentRuntimeSkillResolver = func(_ context.Context, _ string, dir string) (*Skill, error) {
		instructions := "只基于真实视觉输入形成可追溯证据。"
		return &Skill{
			Dir: dir, Name: "视觉证据分析", Description: "分析视觉资产", DetailText: instructions,
			Version: 1, Checksum: agentRuntimeTestSkillChecksum(instructions), SourceKind: "original",
			SourceRevision: "test-v1", SourceLicense: "HMaigc-Proprietary", PublishedAt: "2026-08-27T00:00:00Z",
			CapabilityManifest: agentruntime.SkillCapabilityManifest{
				Specialists:     []agentruntime.SpecialistKey{agentruntime.SpecialistVisual},
				Tools:           []agentruntime.AgentToolName{agentruntime.ToolVisionAnalyze},
				ArtifactSchemas: []string{"visual_evidence.v1"},
			},
		}, nil
	}

	_, err := svc.resolveAgentRuntimeSkillSelectionsForSpecialist(context.Background(), "user-1", []string{"visual-evidence-analysis"}, agentruntime.SpecialistNarrative)
	if err == nil || !strings.Contains(err.Error(), "Skill Capability Manifest") {
		t.Fatalf("capability mismatch error = %v", err)
	}
}

func TestResolveAgentRuntimeConfigurationFreezesReadyAudioAndVideoAttachments(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	userID := agentRuntimeServiceScope().ActorUserID
	resources := []model.Resource{
		{ID: "runtime-reference-audio", UserID: userID, Kind: "audio", Status: model.ResourceStatusReady, MimeType: "audio/wav", Size: 1_024, DurationMs: 12_000},
		{ID: "runtime-reference-video", UserID: userID, Kind: "video", Status: model.ResourceStatusReady, MimeType: "video/mp4", Size: 4_096, Width: 1280, Height: 720, DurationMs: 15_000},
	}
	if err := db.Create(&resources).Error; err != nil {
		t.Fatal(err)
	}

	configuration, err := svc.resolveAgentRuntimeConfiguration(context.Background(), userID, AgentRuntimeConfigurationInput{
		Attachments: []AgentRuntimeResourceInput{
			{ResourceID: "runtime-reference-video", Name: "样片.mp4"},
			{ResourceID: "runtime-reference-audio", Name: "对白.wav"},
		},
		ExecutionMode: agentruntime.ExecutionGuided,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(configuration.Attachments) != 2 || configuration.Attachments[0].Kind != "audio" || configuration.Attachments[0].SizeBytes != 1_024 ||
		configuration.Attachments[1].Kind != "video" || configuration.Attachments[1].DurationMS != 15_000 {
		t.Fatalf("frozen media attachments = %#v", configuration.Attachments)
	}
}

func TestAgentRuntimeFreezesSelectedGenerationModelAndSkillInstructions(t *testing.T) {
	server, _ := newAgentRuntimeDecisionServer(t, `{"kind":"final","final":{"message":"ok","expectedDelivery":{"kind":"answer","requiredArtifacts":["text"],"completionCriteria":[{"fact":"final_message"}]}}}`)
	svc, db, fixture := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	createAgentRuntimeImageModel(t, db, fixture)
	if err := db.Create(&model.Resource{ID: "runtime-reference-image", UserID: agentRuntimeServiceScope().ActorUserID, Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png", Size: 2_048, Width: 1280, Height: 720}).Error; err != nil {
		t.Fatal(err)
	}
	svc.agentRuntimeSkillResolver = func(_ context.Context, userID string, dir string) (*Skill, error) {
		if userID != agentRuntimeServiceScope().ActorUserID || dir != "storyboard-director" {
			t.Fatalf("unexpected skill lookup: user=%q dir=%q", userID, dir)
		}
		instructions := "先读取画布事实，再输出可执行分镜。"
		return &Skill{Dir: dir, Name: "分镜导演", Description: "拆解镜头", DetailText: instructions, Version: 7, Checksum: agentRuntimeTestSkillChecksum(instructions)}, nil
	}
	input := StartAgentRuntimeInput{
		Scope: agentRuntimeServiceScope(), ClientRequestID: "context-selection-request",
		UserMessage: "生成一张分镜图", MaxSteps: 6,
		Configuration: AgentRuntimeConfigurationInput{
			GenerationModels: agentruntime.GenerationModelSelections{
				Image: &agentruntime.GenerationModelSelection{ChannelID: "runtime-image-channel", Model: "kz_gpt_image2"},
			},
			SkillDirs:     []string{"storyboard-director"},
			Attachments:   []AgentRuntimeResourceInput{{ResourceID: "runtime-reference-image", Name: "人物参考.png"}},
			ExecutionMode: agentruntime.ExecutionAutomatic,
		},
	}
	progress, err := svc.StartAgentRuntime(input)
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Configuration.GenerationModels.Image == nil || progress.State.Configuration.GenerationModels.Image.Model != "kz_gpt_image2" {
		t.Fatalf("frozen generation model = %#v", progress.State.Configuration.GenerationModels)
	}
	if len(progress.State.Configuration.Skills) != 1 || progress.State.Configuration.Skills[0].Dir != "storyboard-director" || progress.State.Configuration.Skills[0].Version != 7 {
		t.Fatalf("frozen skills = %#v", progress.State.Configuration.Skills)
	}
	if progress.State.Configuration.ExecutionMode != agentruntime.ExecutionAutomatic || len(progress.State.Configuration.Attachments) != 1 {
		t.Fatalf("frozen composer facts = %#v", progress.State.Configuration)
	}
	attachment := progress.State.Configuration.Attachments[0]
	if attachment.ResourceID != "runtime-reference-image" || attachment.MIMEType != "image/png" || attachment.Width != 1280 || attachment.Height != 720 {
		t.Fatalf("frozen attachment = %#v", attachment)
	}
	stored, err := svc.repo.TaskForUser(input.Scope.ActorUserID, progress.ModelTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	promptContext := decodeAgentRuntimePromptContextForTest(t, stored.Prompt)
	if promptContext.CanvasRevision != 7 {
		t.Fatalf("canvas revision = %d, want 7", promptContext.CanvasRevision)
	}
	if len(promptContext.CallableModels) != 1 || promptContext.CallableModels[0].Model != "kz_gpt_image2" {
		t.Fatalf("selected callable models = %#v", promptContext.CallableModels)
	}
	if len(promptContext.Configuration.Skills) != 1 || promptContext.Configuration.Skills[0].Instructions != "" || strings.Contains(stored.Prompt, "先读取画布事实，再输出可执行分镜。") {
		t.Fatalf("selected skill prompt facts = %#v", promptContext.Configuration.Skills)
	}
}

func TestAgentRuntimeRejectsForeignAttachmentBeforeBilling(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	if err := db.Create(&model.Resource{
		ID: "foreign-agent-reference", UserID: "another-user", Kind: "image",
		Status: model.ResourceStatusReady, MimeType: "image/png", Width: 640, Height: 640,
	}).Error; err != nil {
		t.Fatal(err)
	}
	input := StartAgentRuntimeInput{
		Scope: agentRuntimeServiceScope(), ClientRequestID: "foreign-attachment-request",
		UserMessage: "参考这张图片", MaxSteps: 4,
		Configuration: AgentRuntimeConfigurationInput{
			Attachments:   []AgentRuntimeResourceInput{{ResourceID: "foreign-agent-reference", Name: "他人图片.png"}},
			ExecutionMode: agentruntime.ExecutionGuided,
		},
	}
	if _, err := svc.StartAgentRuntime(input); err == nil || !strings.Contains(err.Error(), "不属于当前用户") {
		t.Fatalf("foreign attachment error = %v", err)
	}
	var taskCount, billingCount int64
	if err := db.Model(&model.Task{}).Where("user_id = ?", input.Scope.ActorUserID).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("user_id = ?", input.Scope.ActorUserID).Count(&billingCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 || billingCount != 0 {
		t.Fatalf("foreign attachment created commercial facts: tasks=%d billing=%d", taskCount, billingCount)
	}
}

func TestAgentRuntimeFreezesCallableMediaModelFactsWithoutProviderSecrets(t *testing.T) {
	server, _ := newAgentRuntimeDecisionServer(t, `{"kind":"final","final":{"message":"ok","expectedDelivery":{"kind":"answer","requiredArtifacts":["text"],"completionCriteria":[{"fact":"final_message"}]}}}`)
	svc, db, fixture := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	createAgentRuntimeImageModel(t, db, fixture)

	input := StartAgentRuntimeInput{
		Scope: agentRuntimeServiceScope(), ClientRequestID: "context-catalog-request",
		UserMessage: "生成一张图片", MaxSteps: 6,
		Configuration: guidedAgentRuntimeConfigurationInput(),
	}
	progress, err := svc.StartAgentRuntime(input)
	if err != nil {
		t.Fatal(err)
	}
	if progress.ModelTask == nil {
		t.Fatal("agent model task is missing")
	}
	stored, err := svc.repo.TaskForUser(input.Scope.ActorUserID, progress.ModelTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	context := decodeAgentRuntimePromptContextForTest(t, stored.Prompt)
	if len(context.CallableModels) != 1 {
		t.Fatalf("callable models = %#v", context.CallableModels)
	}
	callable := context.CallableModels[0]
	if callable.ChannelID != "runtime-image-channel" || callable.Model != "kz_gpt_image2" || callable.Capability != "image" || callable.UnitPriceMicrocredits != 250 {
		t.Fatalf("callable model = %#v", callable)
	}
	if callable.ProviderCapabilities == nil || callable.ProviderCapabilities.ModelKey != "kz_gpt_image2" {
		t.Fatalf("provider capabilities = %#v", callable.ProviderCapabilities)
	}
	for _, secret := range []string{"runtime-secret-key", fixture.endpoint.BaseURL, fixture.version.KeyCipher} {
		if secret != "" && strings.Contains(stored.Prompt, secret) {
			t.Fatalf("agent prompt leaked provider secret %q", secret)
		}
	}

	if err := db.Model(&model.ChannelModel{}).Where("id = ?", "runtime-image-model").Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	replayed, err := svc.StartAgentRuntime(input)
	if err != nil {
		t.Fatalf("frozen model facts did not survive catalog change: %v", err)
	}
	if replayed.ModelTask == nil || replayed.ModelTask.ID != progress.ModelTask.ID {
		t.Fatalf("replayed model task = %#v", replayed.ModelTask)
	}
	unchanged, err := svc.repo.TaskForUser(input.Scope.ActorUserID, progress.ModelTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Prompt != stored.Prompt {
		t.Fatal("frozen callable model facts changed during replay")
	}
}

func TestAgentRuntimeFreezesNativeAudioVideoCapabilities(t *testing.T) {
	server, _ := newAgentRuntimeDecisionServer(t, `{"kind":"final","final":{"message":"ok","expectedDelivery":{"kind":"answer","requiredArtifacts":["text"],"completionCriteria":[{"fact":"final_message"}]}}}`)
	svc, db, fixture := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	createAgentRuntimeVideoModel(t, db, fixture)

	input := StartAgentRuntimeInput{
		Scope: agentRuntimeServiceScope(), ClientRequestID: "context-video-capability-request",
		UserMessage: "生成带原生对白的视频", MaxSteps: 6,
		Configuration: guidedAgentRuntimeConfigurationInput(),
	}
	progress, err := svc.StartAgentRuntime(input)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := svc.repo.TaskForUser(input.Scope.ActorUserID, progress.ModelTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	context := decodeAgentRuntimePromptContextForTest(t, stored.Prompt)
	if len(context.CallableModels) != 1 || context.CallableModels[0].ProviderCapabilities == nil {
		t.Fatalf("callable models = %#v", context.CallableModels)
	}
	capabilities := context.CallableModels[0].ProviderCapabilities
	if len(capabilities.Durations) != 27 || capabilities.Durations[0] != 4 || capabilities.Durations[len(capabilities.Durations)-1] != 30 {
		t.Fatalf("frozen durations = %#v", capabilities.Durations)
	}
	if !capabilities.SupportsImageToVideo || !capabilities.SupportsReferenceVideo || !capabilities.SupportsNativeAudio ||
		!capabilities.SupportsDialogue || !capabilities.SupportsVoiceReference || capabilities.SupportsLipSync || capabilities.SupportsIndependentAudio {
		t.Fatalf("frozen media delivery capabilities = %#v", capabilities)
	}
	if strings.Join(capabilities.AdaptiveRatioModes, ",") != "text,image,first_last_frame,image_reference,omni_reference" || strings.Join(capabilities.RequiredAdaptiveRatioModes, ",") != "first_last_frame" {
		t.Fatalf("frozen adaptive-ratio modes = allowed %#v required %#v", capabilities.AdaptiveRatioModes, capabilities.RequiredAdaptiveRatioModes)
	}
	if strings.Join(capabilities.GenerationModes, ",") != "text,image,first_last_frame,image_reference,omni_reference" || capabilities.MaxImagesWithVideo != 30 {
		t.Fatalf("frozen generation modes = %#v; max images with video = %d", capabilities.GenerationModes, capabilities.MaxImagesWithVideo)
	}
}

func createAgentRuntimeVideoModel(t *testing.T, db *gorm.DB, fixture agentRuntimeServiceFixture) {
	t.Helper()
	now := time.Now().UTC()
	channel := model.ModelChannel{
		ID: "runtime-video-channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "Agent Video",
		APIFormat: "openai", InterfaceType: model.ChannelInterfaceAIOpenVideoVolcengine,
		ModelsJSON: `["doubao-seedance-2-5-260628"]`, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	tiers := make([]model.ChannelModelPriceTier, 0, 9)
	for _, resolution := range []string{"480P", "720P", "1080P"} {
		for _, variant := range []string{"standard", "standard_audio", "reference_video"} {
			tiers = append(tiers, model.ChannelModelPriceTier{
				ID:             "runtime-video-" + strings.ToLower(resolution) + "-" + variant,
				ChannelModelID: "runtime-video-model", Resolution: resolution, InputVariant: variant,
				UnitPriceMicrocredits: 100, PriceVersion: 1,
			})
		}
	}
	channelModel := model.ChannelModel{
		ID: "runtime-video-model", ChannelID: channel.ID, ModelKey: "doubao-seedance-2-5-260628", DisplayName: "Seedance 2.5",
		ProviderCredentialID: fixture.credential.ID, AccessPolicy: model.ModelAccessAuthenticated, Capability: "video",
		BillingMode: "per_second", PriceStrategy: "video_resolution", PriceConfigured: true, Enabled: true,
		PriceTiers: tiers, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&channelModel).Error; err != nil {
		t.Fatal(err)
	}
}

func TestValidateAgentRuntimeCallableModelsAcceptsInputImageUsagePricing(t *testing.T) {
	models := []agentRuntimeCallableModelFact{{
		ChannelID: "image-channel", Model: "seedream5.0pro", DisplayName: "Seedream 5.0 Pro", Capability: "image",
		BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 120_000_000,
		PriceTiers: []PublicChannelModelPriceTier{{
			UsageMetric: inputImageUsageMetric, IncludedQuantity: 1, UnitPriceMicrocredits: 4_000_000,
		}},
	}}

	if err := validateAgentRuntimeCallableModels(models); err != nil {
		t.Fatalf("valid input-image usage pricing rejected: %v", err)
	}
}

func TestStartAgentRuntimeRejectsInvalidCallablePricingWithoutPersistingPartialRun(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeCanvas(t, db)
	createAgentRuntimeImageModel(t, db, fixture)
	now := time.Now().UTC()
	if err := db.Create(&model.ChannelModelPriceTier{
		ID: "invalid-runtime-usage-tier", ChannelModelID: "runtime-image-model",
		UsageMetric: "unsupported_usage", IncludedQuantity: 1, UnitPriceMicrocredits: 10,
		PriceVersion: 1, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	input := StartAgentRuntimeInput{
		Scope: agentRuntimeServiceScope(), ClientRequestID: "invalid-callable-pricing-request",
		UserMessage: "读取当前画布", MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput(),
	}
	if _, err := svc.StartAgentRuntime(input); err == nil || err.Error() != "agent callable model pricing facts are invalid" {
		t.Fatalf("invalid callable pricing error = %v", err)
	}
	var runCount int64
	if err := db.Model(&model.AgentRun{}).Where("client_request_id = ?", input.ClientRequestID).Count(&runCount).Error; err != nil {
		t.Fatal(err)
	}
	if runCount != 0 {
		t.Fatalf("invalid callable pricing persisted %d partial runs", runCount)
	}
}

func TestValidateAgentRuntimeCallableModelsRejectsInvalidInputImageUsagePricing(t *testing.T) {
	tests := []struct {
		name  string
		tiers []PublicChannelModelPriceTier
	}{
		{name: "unsupported metric", tiers: []PublicChannelModelPriceTier{{UsageMetric: "output_image", UnitPriceMicrocredits: 4_000_000}}},
		{name: "mixed dimensions", tiers: []PublicChannelModelPriceTier{{UsageMetric: inputImageUsageMetric, Resolution: "1K", UnitPriceMicrocredits: 4_000_000}}},
		{name: "negative included quantity", tiers: []PublicChannelModelPriceTier{{UsageMetric: inputImageUsageMetric, IncludedQuantity: -1, UnitPriceMicrocredits: 4_000_000}}},
		{name: "duplicate metric", tiers: []PublicChannelModelPriceTier{
			{UsageMetric: inputImageUsageMetric, IncludedQuantity: 1, UnitPriceMicrocredits: 4_000_000},
			{UsageMetric: inputImageUsageMetric, IncludedQuantity: 2, UnitPriceMicrocredits: 5_000_000},
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			models := []agentRuntimeCallableModelFact{{
				ChannelID: "image-channel", Model: "seedream5.0pro", DisplayName: "Seedream 5.0 Pro", Capability: "image",
				BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 120_000_000, PriceTiers: test.tiers,
			}}
			if err := validateAgentRuntimeCallableModels(models); err == nil || err.Error() != "agent callable model pricing facts are invalid" {
				t.Fatalf("invalid usage pricing error = %v", err)
			}
		})
	}
}

func TestAgentRuntimePromptIncludesCompletedClarificationFacts(t *testing.T) {
	delivery := agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryAnswer,
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}},
	}
	state := agentruntime.RuntimeState{
		StateVersion: 4, StepNumber: 1, MaxSteps: 6, Status: agentruntime.RunRunning,
		UserMessage: "生成汽车广告剧本", ExpectedDelivery: &delivery,
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic},
		ClarificationHistory: []agentruntime.CompletedClarification{{
			Request: agentruntime.ClarificationDecision{
				RequestID: "clarify-car-ad",
				Questions: []agentruntime.ClarificationQuestion{{
					ID: "duration", Prompt: "广告时长是多少？", Type: agentruntime.ClarificationSingleChoice,
					Options: []agentruntime.ClarificationOption{{ID: "15s", Label: "15 秒"}, {ID: "30s", Label: "30 秒"}},
				}},
				ExpectedDelivery: delivery,
			},
			Answers:              []agentruntime.ClarificationAnswer{{QuestionID: "duration", SelectedOptionIDs: []string{"30s"}}},
			CompletionQuestionID: "duration", CompletionExpectedStateVersion: 3,
		}},
	}
	prompt, err := encodeAgentRuntimeModelPrompt(agentRuntimeServiceScope(), state, 11, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	context := decodeAgentRuntimePromptContextForTest(t, prompt)
	if len(context.ClarificationHistory) != 1 || context.ClarificationHistory[0].Request.RequestID != "clarify-car-ad" {
		t.Fatalf("clarification history = %#v", context.ClarificationHistory)
	}
	if len(context.ClarificationHistory[0].Answers) != 1 || context.ClarificationHistory[0].Answers[0].SelectedOptionIDs[0] != "30s" {
		t.Fatalf("clarification answers = %#v", context.ClarificationHistory[0].Answers)
	}
}

func TestEncodeProductionAgentRuntimePromptUsesOnlyGraphAndGenericTools(t *testing.T) {
	t.Parallel()

	delivery := agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryAnswer,
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}},
	}
	state := agentruntime.RuntimeState{
		StateVersion: 4, StepNumber: 2, MaxSteps: 24, Status: agentruntime.RunRunning,
		UserMessage: "创作一部短片", ExpectedDelivery: &delivery,
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic},
	}
	stage := agentRuntimeProductionStageFact{
		ID: "stage-script", StageKey: "script", SpecialistKey: agentruntime.SpecialistNarrative,
		Status: agentruntime.StageAwaitingReview, Version: 3, ReviewRevisionID: "revision-script-1",
		ExpectedDelivery: delivery, ReviewPolicy: agentruntime.ReviewRequired, CostPolicy: agentruntime.CostNone,
	}
	production := &agentRuntimeProductionContextFact{
		Graph: &agentRuntimeProductionGraphFact{
			ID: "graph-version-1", GraphKey: "short-film", Version: 1,
			SchemaVersion: agentruntime.CurrentProductionSchemaVersion, Stages: []agentRuntimeProductionStageFact{stage},
		},
		CurrentStage: &stage,
		Artifacts: []agentRuntimeArtifactSummaryFact{{
			ArtifactID: "artifact-script", ArtifactKey: "script", Kind: "script_bundle.v1", HeadRevision: 1,
			RevisionID: "revision-script-1", SchemaVersion: 1, LifecycleStatus: "awaiting_review",
		}},
	}
	prompt, err := encodeAgentRuntimeModelPromptForToolSchema(
		agentRuntimeServiceScope(), state, 11, agentruntime.ProductionToolSchemaVersion,
		nil, nil, production, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimPrefix(prompt, agentRuntimeModelPromptPrefix)), &raw); err != nil {
		t.Fatal(err)
	}
	if _, found := raw["productionPlan"]; found {
		t.Fatal("production prompt exposed historical productionPlan")
	}
	if _, found := raw["productionGraph"]; !found {
		t.Fatal("production prompt omitted immutable productionGraph")
	}
	context := decodeAgentRuntimePromptContextForTest(t, prompt)
	if context.ToolSchemaVersion != agentruntime.ProductionToolSchemaVersion {
		t.Fatalf("tool schema version = %d", context.ToolSchemaVersion)
	}
	wantTools := []agentruntime.ToolName{
		agentruntime.ToolSkillLoad,
		agentruntime.ToolSpecialistDelegate,
		agentruntime.ToolVisionAnalyze,
		agentruntime.ToolMediaGenerate,
		agentruntime.ToolCanvasProject,
	}
	if len(context.CallableTools) != len(wantTools) {
		t.Fatalf("callable tools = %#v", context.CallableTools)
	}
	for index, want := range wantTools {
		if context.CallableTools[index].Name != want {
			t.Fatalf("callable tool[%d] = %q, want %q", index, context.CallableTools[index].Name, want)
		}
	}
	if context.ProductionGraph == nil || context.ProductionGraph.ID != "graph-version-1" ||
		context.CurrentStage == nil || context.CurrentStage.ReviewRevisionID != "revision-script-1" ||
		len(context.Artifacts) != 1 || context.Artifacts[0].RevisionID != "revision-script-1" {
		t.Fatalf("production context = graph %#v, stage %#v, artifacts %#v", context.ProductionGraph, context.CurrentStage, context.Artifacts)
	}
	if _, err := frozenAgentRuntimeModelContext(agentRuntimeServiceScope(), state, prompt); err != nil {
		t.Fatalf("frozen production prompt rejected: %v", err)
	}
}

func TestLoadAgentRuntimeProductionContextFactUsesExactHeadFactsWithoutPayload(t *testing.T) {
	svc, _, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	scope := agentRuntimeServiceScope()
	scope.DomainProjectID = "runtime-project"
	delivery := agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryAnswer,
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}},
	}
	graph, err := svc.repo.AppendProductionGraphVersion(scope, 0, agentruntime.ProductionGraphDraft{
		GraphKey: "short-film",
		Stages: []agentruntime.ProductionStageDraft{{
			StageKey: "script", SpecialistKey: agentruntime.SpecialistNarrative,
			DependsOnStageKeys: []string{}, InputRevisions: []agentruntime.ArtifactRevisionRef{},
			ExpectedDelivery: delivery, ReviewPolicy: agentruntime.ReviewRequired, CostPolicy: agentruntime.CostNone,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.AdvanceProductionStageCAS(scope, graph.Stages[0].ID, 1, agentruntime.StageRunning); err != nil {
		t.Fatal(err)
	}
	revision, err := svc.repo.AppendArtifactRevision(scope, "script-artifact", 0, agentruntime.ArtifactDraft{
		ArtifactKey: "script", Kind: "script_bundle.v1", SchemaVersion: 1,
		Payload: json.RawMessage(`{"secretScript":"不得进入模型上下文"}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	fact, err := svc.loadAgentRuntimeProductionContextFact(scope)
	if err != nil {
		t.Fatal(err)
	}
	if fact.Graph == nil || fact.Graph.ID != graph.Graph.ID || fact.CurrentStage == nil ||
		fact.CurrentStage.ID != graph.Stages[0].ID || fact.CurrentStage.Status != agentruntime.StageRunning {
		t.Fatalf("production graph fact = %#v, current stage = %#v", fact.Graph, fact.CurrentStage)
	}
	if len(fact.Artifacts) != 1 || fact.Artifacts[0].RevisionID != revision.ID || fact.Artifacts[0].HeadRevision != 1 {
		t.Fatalf("artifact facts = %#v", fact.Artifacts)
	}
	encoded, err := json.Marshal(fact)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "不得进入模型上下文") {
		t.Fatalf("production context exposed artifact payload: %s", encoded)
	}
}

func TestAgentRuntimeModelPromptUsesStoredProductionToolSchema(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeCanvas(t, db)
	scope := agentRuntimeServiceScope()
	scope.DomainProjectID = "runtime-project"
	if _, err := svc.repo.CreateAgentRun(repository.CreateAgentRunInput{
		Scope: scope, ClientRequestID: "production-context-request", Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).
		Update("tool_schema_version", agentruntime.ProductionToolSchemaVersion).Error; err != nil {
		t.Fatal(err)
	}
	delivery := agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryAnswer,
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}},
	}
	if _, err := svc.repo.AppendProductionGraphVersion(scope, 0, agentruntime.ProductionGraphDraft{
		GraphKey: "short-film",
		Stages: []agentruntime.ProductionStageDraft{{
			StageKey: "script", SpecialistKey: agentruntime.SpecialistNarrative,
			DependsOnStageKeys: []string{}, InputRevisions: []agentruntime.ArtifactRevisionRef{},
			ExpectedDelivery: delivery, ReviewPolicy: agentruntime.ReviewRequired, CostPolicy: agentruntime.CostNone,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	state := agentruntime.RuntimeState{
		StateVersion: 1, StepNumber: 0, MaxSteps: 24, Status: agentruntime.RunRunning,
		UserMessage: "创作短片", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic},
	}
	prompt, err := svc.agentRuntimeModelPrompt(scope, state)
	if err != nil {
		t.Fatal(err)
	}
	context := decodeAgentRuntimePromptContextForTest(t, prompt)
	if context.ToolSchemaVersion != agentruntime.ProductionToolSchemaVersion || context.ProductionGraph == nil || context.ProductionPlan != nil {
		t.Fatalf("production model context = %#v", context)
	}
}

func TestProductionAgentRuntimeSystemPromptExposesOnlyGenericTools(t *testing.T) {
	prompt, err := agentRuntimeSystemPromptForToolSchema(agentruntime.ProductionToolSchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"skill.load", "specialist.delegate", "vision.analyze", "media.generate", "canvas.project"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("production system prompt is missing %q", required)
		}
	}
	for _, legacy := range []string{"production.plan", "production.render", "canvas.commit"} {
		if strings.Contains(prompt, legacy) {
			t.Fatalf("production system prompt exposed legacy tool %q", legacy)
		}
	}
}

func TestAgentRuntimeSystemPromptDeclaresStructuredClarificationDecision(t *testing.T) {
	for _, required := range []string{
		`"kind":"clarification_request"`,
		`"requestId":"..."`,
		`"type":"single_choice|multi_choice|free_text"`,
		"仅当完成用户目标所需事实确实缺失时才允许追问",
	} {
		if !strings.Contains(agentRuntimeSystemPrompt, required) {
			t.Fatalf("agent runtime system prompt is missing %q", required)
		}
	}
}

func TestAgentRuntimeSystemPromptRequiresCompletionFromAccumulatedEvidence(t *testing.T) {
	for _, required := range []string{
		"deliveryEvidence 与 deliveryVerification",
		"已经满足的 criterion 禁止重复执行",
		"missingCriteria 只剩 final_message 时必须直接返回 final",
	} {
		if !strings.Contains(agentRuntimeSystemPrompt, required) {
			t.Fatalf("agent runtime system prompt is missing %q", required)
		}
	}
}

type agentRuntimePromptContextForTest struct {
	ToolSchemaVersion    int                                   `json:"toolSchemaVersion"`
	CanvasRevision       int64                                 `json:"canvasRevision"`
	DeliveryEvidence     *agentruntime.DeliveryEvidence        `json:"deliveryEvidence"`
	DeliveryVerification *agentruntime.DeliveryVerification    `json:"deliveryVerification"`
	Configuration        agentruntime.RunConfiguration         `json:"configuration"`
	LoadedSkillDirs      []string                              `json:"loadedSkillDirs"`
	ClarificationHistory []agentruntime.CompletedClarification `json:"clarificationHistory"`
	ProductionPlan       *agentRuntimeProductionPlanFact       `json:"productionPlan"`
	ProductionGraph      *agentRuntimeProductionGraphFact      `json:"productionGraph"`
	CurrentStage         *agentRuntimeProductionStageFact      `json:"currentStage"`
	Artifacts            []agentRuntimeArtifactSummaryFact     `json:"artifacts"`
	CallableTools        []agentRuntimeCallableToolFact        `json:"callableTools"`
	CallableModels       []struct {
		ChannelID             string                      `json:"channelId"`
		Model                 string                      `json:"model"`
		Capability            string                      `json:"capability"`
		UnitPriceMicrocredits int64                       `json:"unitPriceMicrocredits"`
		ProviderCapabilities  *PublicProviderCapabilities `json:"providerCapabilities"`
	} `json:"callableModels"`
}

func decodeAgentRuntimePromptContextForTest(t *testing.T, prompt string) agentRuntimePromptContextForTest {
	t.Helper()
	prefix := agentRuntimeModelPromptPrefix
	if !strings.HasPrefix(prompt, prefix) {
		t.Fatalf("agent prompt prefix is invalid: %q", prompt)
	}
	var context agentRuntimePromptContextForTest
	if err := json.Unmarshal([]byte(strings.TrimPrefix(prompt, prefix)), &context); err != nil {
		t.Fatal(err)
	}
	return context
}
