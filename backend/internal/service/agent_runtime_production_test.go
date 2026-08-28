package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

func TestProductionPlanArgumentsRejectUnknownFields(t *testing.T) {
	tests := map[string]string{
		"top level": `{"planKey":"","baseVersion":0,"unexpected":true,"draft":{"title":"广告","targetDurationMs":1000,"script":"脚本","shots":[{"shotKey":"shot-1","order":1,"durationMs":1000,"scriptText":"镜头","imagePrompt":"画面","videoPrompt":"动作","dependencies":[]}]}}`,
		"draft":     `{"planKey":"","baseVersion":0,"draft":{"title":"广告","targetDurationMs":1000,"script":"脚本","unexpected":true,"shots":[{"shotKey":"shot-1","order":1,"durationMs":1000,"scriptText":"镜头","imagePrompt":"画面","videoPrompt":"动作","dependencies":[]}]}}`,
		"shot":      `{"planKey":"","baseVersion":0,"draft":{"title":"广告","targetDurationMs":1000,"script":"脚本","shots":[{"shotKey":"shot-1","order":1,"durationMs":1000,"scriptText":"镜头","imagePrompt":"画面","videoPrompt":"动作","dependencies":[],"unexpected":true}]}}`,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeAgentProductionPlanArguments(json.RawMessage(raw)); !errors.Is(err, errAgentRuntimeProductionPlanInput) {
				t.Fatalf("unknown field error = %v", err)
			}
		})
	}
}

func TestProductionPlanArgumentsRequireServerControlledInitialIdentity(t *testing.T) {
	initialWithClientKey := json.RawMessage(`{"planKey":"client-chosen","baseVersion":0,"draft":{"title":"广告","targetDurationMs":1000,"script":"脚本","shots":[{"shotKey":"shot-1","order":1,"durationMs":1000,"scriptText":"镜头","imagePrompt":"画面","videoPrompt":"动作","dependencies":[]}]}}`)
	if _, err := decodeAgentProductionPlanArguments(initialWithClientKey); !errors.Is(err, errAgentRuntimeProductionPlanInput) {
		t.Fatalf("client-controlled initial plan key error = %v", err)
	}
	updateWithoutKey := json.RawMessage(`{"planKey":"","baseVersion":1,"draft":{"title":"广告","targetDurationMs":1000,"script":"脚本","shots":[{"shotKey":"shot-1","order":1,"durationMs":1000,"scriptText":"镜头","imagePrompt":"画面","videoPrompt":"动作","dependencies":[]}]}}`)
	if _, err := decodeAgentProductionPlanArguments(updateWithoutKey); !errors.Is(err, errAgentRuntimeProductionPlanInput) {
		t.Fatalf("missing update plan key error = %v", err)
	}
}

func TestAgentRuntimeSkillLoadExposesFrozenInstructionsOnNextStep(t *testing.T) {
	decision := `{"kind":"tool_call","toolCall":{"toolCallId":"load-storyboard","toolName":"skill.load","actionVersion":1,"arguments":{"dir":"storyboard-director"}}}`
	server, _ := newAgentRuntimeDecisionServer(t, decision, agentRuntimeTestAnswerDelivery())
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	svc.agentRuntimeSkillResolver = func(_ context.Context, userID string, dir string) (*Skill, error) {
		instructions := "冻结的 Skill 执行说明。"
		return &Skill{Dir: dir, Name: "分镜导演", Description: "拆解镜头", DetailText: instructions, Version: 9, Checksum: agentRuntimeTestSkillChecksum(instructions)}, nil
	}
	scope := agentRuntimeServiceScope()
	input := StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "skill-load-run", UserMessage: "调用已选分镜 Skill", MaxSteps: 6,
		Configuration: AgentRuntimeConfigurationInput{SkillDirs: []string{"storyboard-director"}, ExecutionMode: agentruntime.ExecutionAutomatic},
	}
	started, err := svc.StartAgentRuntime(input)
	if err != nil {
		t.Fatal(err)
	}
	initialTask, err := svc.repo.TaskForUser(scope.ActorUserID, started.ModelTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(initialTask.Prompt, "冻结的 Skill 执行说明。") {
		t.Fatal("initial prompt exposed unloaded skill instructions")
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	progress, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if progress.ModelTask == nil || len(progress.State.LoadedSkillDirs) != 1 || progress.State.LoadedSkillDirs[0] != "storyboard-director" {
		t.Fatalf("skill load progress = %#v", progress)
	}
	nextTask, err := svc.repo.TaskForUser(scope.ActorUserID, progress.ModelTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	nextPrompt := decodeAgentRuntimePromptContextForTest(t, nextTask.Prompt)
	if len(nextPrompt.LoadedSkillDirs) != 1 || nextPrompt.LoadedSkillDirs[0] != "storyboard-director" || len(nextPrompt.Configuration.Skills) != 1 || nextPrompt.Configuration.Skills[0].Instructions != "冻结的 Skill 执行说明。" {
		t.Fatalf("loaded skill prompt = %#v", nextPrompt)
	}
}

func TestLegacyV3ProductionPlanToolPersistsPlanAndArtifactsWithoutMediaBilling(t *testing.T) {
	t.Skip("tool schema v3 is terminal-history-only; v4 production graph and specialist persistence have dedicated coverage")
	decision := `{"kind":"tool_call","toolCall":{"toolCallId":"plan-orange-ad","toolName":"production.plan","actionVersion":1,"arguments":{"planKey":"","baseVersion":0,"draft":{"title":"10秒橙子广告","targetDurationMs":10000,"script":"鲜橙唤醒清晨。","shots":[{"shotKey":"shot-1","order":1,"durationMs":5000,"scriptText":"鲜橙落水","deliverables":["storyboard_image","video_clip"],"imagePrompt":"鲜橙产品特写","videoPrompt":"慢镜头水花","dependencies":[]},{"shotKey":"shot-2","order":2,"durationMs":5000,"scriptText":"果汁收尾","deliverables":["storyboard_image","video_clip"],"imagePrompt":"果汁英雄镜头","videoPrompt":"镜头推进","dependencies":["shot-1"]}]}}}}`
	server, _ := newAgentRuntimeDecisionServer(t, decision, agentRuntimeTestAnswerDelivery())
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	scope := agentRuntimeServiceScope()
	input := StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "production-plan-run", UserMessage: "规划一个10秒橙子广告", MaxSteps: 6,
		Configuration: AgentRuntimeConfigurationInput{ExecutionMode: agentruntime.ExecutionAutomatic},
	}
	if _, err := svc.StartAgentRuntime(input); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	progress, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Status != agentruntime.RunRunning || progress.State.PendingToolCall != nil ||
		progress.State.LastToolResult == nil || !progress.State.LastToolResult.Succeeded || progress.ModelTask == nil {
		t.Fatalf("production plan progress = %#v", progress)
	}
	var output struct {
		PlanKey     string `json:"planKey"`
		PlanVersion int    `json:"planVersion"`
		Artifacts   []struct {
			ArtifactID string                              `json:"artifactId"`
			Kind       model.AgentProductionArtifactKind   `json:"kind"`
			ShotKey    string                              `json:"shotKey"`
			Status     model.AgentProductionArtifactStatus `json:"status"`
		} `json:"artifacts"`
		TargetDurationMS int `json:"targetDurationMs"`
	}
	if err := json.Unmarshal(progress.State.LastToolResult.Output, &output); err != nil {
		t.Fatal(err)
	}
	if output.PlanKey == "" || output.PlanVersion != 1 || len(output.Artifacts) != 5 || output.TargetDurationMS != 10_000 {
		t.Fatalf("production plan output = %#v", output)
	}
	if output.Artifacts[0].ArtifactID == "" || output.Artifacts[0].Kind != model.AgentProductionArtifactScript ||
		output.Artifacts[0].ShotKey != "" || output.Artifacts[0].Status != model.AgentProductionArtifactSucceeded ||
		output.Artifacts[1].ArtifactID == "" || output.Artifacts[1].Kind != model.AgentProductionArtifactStoryboardImage ||
		output.Artifacts[1].ShotKey != "shot-1" || output.Artifacts[1].Status != model.AgentProductionArtifactPlanned ||
		output.Artifacts[2].ArtifactID == "" || output.Artifacts[2].Kind != model.AgentProductionArtifactVideoClip ||
		output.Artifacts[2].ShotKey != "shot-1" || output.Artifacts[2].Status != model.AgentProductionArtifactPlanned {
		t.Fatalf("production plan artifact descriptors = %#v", output.Artifacts)
	}
	var plans int64
	var artifacts int64
	var mediaTasks int64
	if err := db.Model(&model.AgentProductionPlanVersion{}).Where("plan_key = ?", output.PlanKey).Count(&plans).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentProductionArtifact{}).Where("plan_key = ?", output.PlanKey).Count(&artifacts).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("user_id = ? AND type IN ?", scope.ActorUserID, []string{"canvas_image", "canvas_video", "canvas_audio"}).Count(&mediaTasks).Error; err != nil {
		t.Fatal(err)
	}
	if plans != 1 || artifacts != 5 || mediaTasks != 0 {
		t.Fatalf("production plan facts: plans=%d artifacts=%d mediaTasks=%d", plans, artifacts, mediaTasks)
	}
	nextTask, err := svc.repo.TaskForUser(scope.ActorUserID, progress.ModelTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	nextContext := decodeAgentRuntimePromptContextForTest(t, nextTask.Prompt)
	if nextContext.ProductionPlan == nil || nextContext.ProductionPlan.PlanKey != output.PlanKey ||
		nextContext.ProductionPlan.PlanVersion != 1 || len(nextContext.ProductionPlan.Shots) != 2 ||
		len(nextContext.ProductionPlan.Artifacts) != 5 ||
		nextContext.ProductionPlan.Artifacts[1].Kind != model.AgentProductionArtifactStoryboardImage {
		t.Fatalf("next model production plan facts = %#v", nextContext.ProductionPlan)
	}
}

func TestProductionRenderCapabilitiesRejectUnsupportedQualityBeforeApproval(t *testing.T) {
	artifact := model.AgentProductionArtifact{Kind: model.AgentProductionArtifactStoryboardImage}
	callable := agentRuntimeCallableModelFact{
		ChannelID: "apimart-image", Model: "gpt-image-2", Capability: "image",
		ProviderCapabilities: &PublicProviderCapabilities{
			ModelKey: "gpt-image-2", Capability: "image", Ratios: []string{"16:9"},
			Resolutions: []string{"1K", "2K", "4K"}, Qualities: []string{}, OutputCounts: []int{1},
		},
	}
	request := agentProductionRenderRequest{
		GenerationModel: agentruntime.GenerationModelSelection{ChannelID: callable.ChannelID, Model: callable.Model},
		ImageConfig:     &agentruntime.ImageRenderConfig{Size: "16:9", Resolution: "1K", Quality: "high", Count: 1},
	}
	if err := validateProductionRenderCapabilities(request, artifact, callable); err == nil || !strings.Contains(err.Error(), "quality") {
		t.Fatalf("unsupported quality error = %v", err)
	}
	request.ImageConfig.Quality = ""
	if err := validateProductionRenderCapabilities(request, artifact, callable); err != nil {
		t.Fatalf("capability-valid image config rejected: %v", err)
	}
}

func TestProductionRenderCapabilitiesRequirePublishedImageResolutionBeforeApproval(t *testing.T) {
	artifact := model.AgentProductionArtifact{Kind: model.AgentProductionArtifactStoryboardImage}
	callable := agentRuntimeCallableModelFact{
		ChannelID: "image-channel", Model: kuaiziGPTImage2Model, Capability: "image",
		ProviderCapabilities: &PublicProviderCapabilities{
			ModelKey: kuaiziGPTImage2Model, Capability: "image", Ratios: []string{"16:9"},
			Resolutions: []string{"1K", "2K", "4K"}, Qualities: []string{"low", "medium", "high"}, OutputCounts: []int{1},
		},
	}
	request := agentProductionRenderRequest{
		GenerationModel: agentruntime.GenerationModelSelection{ChannelID: callable.ChannelID, Model: callable.Model},
		ImageConfig:     &agentruntime.ImageRenderConfig{Size: "16:9", Quality: "high", Count: 1},
	}
	if err := validateProductionRenderCapabilities(request, artifact, callable); err == nil || !strings.Contains(err.Error(), "resolution") {
		t.Fatalf("missing resolution error = %v", err)
	}
	request.ImageConfig.Resolution = "4K"
	if err := validateProductionRenderCapabilities(request, artifact, callable); err != nil {
		t.Fatalf("capability-valid Image 2 config rejected: %v", err)
	}
}

func TestProductionRenderCapabilitiesRejectUnsupportedVideoAspectRatioBeforeApproval(t *testing.T) {
	artifact := model.AgentProductionArtifact{Kind: model.AgentProductionArtifactVideoClip}
	callable := agentRuntimeCallableModelFact{
		ChannelID: "video-channel", Model: "video-model", Capability: "video",
		ProviderCapabilities: &PublicProviderCapabilities{
			ModelKey: "video-model", Capability: "video", Ratios: []string{"16:9", "9:16"},
			Resolutions: []string{"720p"}, DurationMin: 4, DurationMax: 15, SupportsGeneratedAudio: false,
		},
	}
	request := agentProductionRenderRequest{
		GenerationModel: agentruntime.GenerationModelSelection{ChannelID: callable.ChannelID, Model: callable.Model},
		VideoConfig:     &agentruntime.VideoRenderConfig{DurationSeconds: 5, AspectRatio: "1:1", Quality: "720p"},
	}
	if err := validateProductionRenderCapabilities(request, artifact, callable); err == nil || !strings.Contains(err.Error(), "aspect ratio") {
		t.Fatalf("unsupported aspect ratio error = %v", err)
	}
	request.VideoConfig.AspectRatio = "9:16"
	if err := validateProductionRenderCapabilities(request, artifact, callable); err != nil {
		t.Fatalf("capability-valid video config rejected: %v", err)
	}
}

func TestProductionRenderCapabilitiesRejectGeneratedAudioOutsidePublishedResolution(t *testing.T) {
	artifact := model.AgentProductionArtifact{Kind: model.AgentProductionArtifactVideoClip}
	callable := agentRuntimeCallableModelFact{
		ChannelID: "kuaizi-kling", Model: kuaiziKlingModel, Capability: "video",
		ProviderCapabilities: &PublicProviderCapabilities{
			ModelKey: kuaiziKlingModel, Capability: "video", Ratios: []string{"16:9"},
			Resolutions: []string{"std", "pro", "4k"}, DurationMin: 3, DurationMax: 15,
			SupportsGeneratedAudio: true, GeneratedAudioResolutions: []string{"std", "pro"},
		},
	}
	request := agentProductionRenderRequest{
		GenerationModel: agentruntime.GenerationModelSelection{ChannelID: callable.ChannelID, Model: callable.Model},
		VideoConfig:     &agentruntime.VideoRenderConfig{DurationSeconds: 5, AspectRatio: "16:9", Quality: "4k", GenerateAudio: true},
	}
	if err := validateProductionRenderCapabilities(request, artifact, callable); err == nil || !strings.Contains(err.Error(), "generated audio") {
		t.Fatalf("unsupported generated-audio resolution error = %v", err)
	}
	request.VideoConfig.Quality = "pro"
	if err := validateProductionRenderCapabilities(request, artifact, callable); err != nil {
		t.Fatalf("published generated-audio resolution rejected: %v", err)
	}
}

func TestProductionRenderRejectsLegacyPlanWithoutDeliverables(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	scope := agentRuntimeServiceScope()
	now := time.Now().UTC()
	createAgentRuntimeScopedRunFacts(t, db, scope, now)
	plan := model.AgentProductionPlanVersion{
		ID: "legacy-plan-version", PlanKey: "legacy-plan",
		TenantKind: scope.TenantKind, TenantID: scope.TenantID, DomainProjectID: scope.DomainProjectID,
		CanvasID: scope.CanvasID, CreatedByRunID: scope.RunID, Version: 1,
		Status: model.AgentProductionPlanActive, Title: "旧纯视频计划", TargetDurationMS: 5_000,
		Script: "抽象光影。", ReferencesJSON: `[]`,
		ShotsJSON:            `[{"shotKey":"shot-1","order":1,"durationMs":5000,"scriptText":"光带聚合","imagePrompt":"旧分镜提示词","videoPrompt":"镜头推进","dependencies":[]}]`,
		ExpectedDeliveryJSON: `{"scripts":1,"referenceImages":0,"storyboardImages":1,"videoClips":1}`,
		CreatedAt:            now, UpdatedAt: now,
	}
	artifact := model.AgentProductionArtifact{
		ID: "legacy-video-artifact", PlanKey: plan.PlanKey, PlanVersionID: plan.ID, PlanVersion: plan.Version,
		ShotKey: "shot-1", Kind: model.AgentProductionArtifactVideoClip, Status: model.AgentProductionArtifactPlanned,
		CreatedAt: now, UpdatedAt: now,
	}
	for _, value := range []any{&plan, &artifact} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}

	_, err := svc.freezeAgentProductionRenderArguments(scope, nil, json.RawMessage(`{
		"planKey":"legacy-plan",
		"planVersion":1,
		"artifactId":"legacy-video-artifact",
		"generationModel":{"channelId":"video-channel","model":"video-model"},
		"videoConfig":{"durationSeconds":5,"aspectRatio":"16:9","quality":"720p","generateAudio":false}
	}`))
	code, _, ok := agentProductionRenderFailureDetails(err)
	if !ok || code != "production_plan_invalid" || !strings.Contains(err.Error(), "deliverables") {
		t.Fatalf("legacy production plan freeze error = %v code=%q", err, code)
	}
}

func TestProductionRenderRetryRejectsUnresolvedPreviousBillingBeforeApproval(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	scope := agentRuntimeServiceScope()
	now := time.Now().UTC()
	createAgentRuntimeScopedRunFacts(t, db, scope, now)
	plan := model.AgentProductionPlanVersion{
		ID: "retry-unresolved-plan-version", PlanKey: "retry-unresolved-plan",
		TenantKind: scope.TenantKind, TenantID: scope.TenantID, DomainProjectID: scope.DomainProjectID,
		CanvasID: scope.CanvasID, CreatedByRunID: scope.RunID, Version: 1,
		Status: model.AgentProductionPlanActive, Title: "待核账重试", TargetDurationMS: 5_000,
		Script: "鲜橙入水。", ShotsJSON: `[{"shotKey":"shot-1","order":1,"durationMs":5000,"scriptText":"鲜橙入水","deliverables":["storyboard_image","video_clip"],"imagePrompt":"鲜橙特写","videoPrompt":"水花慢镜头","dependencies":[]}]`,
		ExpectedDeliveryJSON: `{}`, CreatedAt: now, UpdatedAt: now,
	}
	task := model.Task{
		ID: "retry-unresolved-task", UserID: scope.ActorUserID, ProjectID: scope.CanvasID,
		Type: "canvas_image", Capability: "image", Status: model.TaskStatusFailed,
		BillingOrderID: "retry-unresolved-order", CreatedAt: now, UpdatedAt: now,
	}
	order := model.BillingOrder{
		ID: task.BillingOrderID, UserID: scope.ActorUserID, TaskID: task.ID,
		IdempotencyKey: "retry-unresolved-order-key", Status: model.BillingStatusUncertain,
		AmountMicrocredits: 10_000_000, CreatedAt: now, UpdatedAt: now,
	}
	artifact := model.AgentProductionArtifact{
		ID: "retry-unresolved-artifact", PlanKey: plan.PlanKey, PlanVersionID: plan.ID,
		PlanVersion: plan.Version, ShotKey: "shot-1", Kind: model.AgentProductionArtifactStoryboardImage,
		Status: model.AgentProductionArtifactFailed, Attempt: 1,
		TaskID: task.ID, BillingOrderID: order.ID, LastErrorCode: "production_generation_failed",
		CreatedAt: now, UpdatedAt: now,
	}
	for _, value := range []any{&plan, &task, &order, &artifact} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}

	_, err := svc.freezeAgentProductionRenderArguments(scope, nil, json.RawMessage(`{
		"planKey":"retry-unresolved-plan",
		"planVersion":1,
		"artifactId":"retry-unresolved-artifact",
		"generationModel":{"channelId":"image-channel","model":"image-model"},
		"imageConfig":{"size":"1:1","resolution":"1K","quality":"medium","count":1}
	}`))
	code, class, ok := agentProductionRenderFailureDetails(err)
	if !ok || code != "production_previous_billing_unresolved" {
		t.Fatalf("retry freeze error = %v code=%q, want production_previous_billing_unresolved", err, code)
	}
	if class != agentruntime.ToolFailureTerminal {
		t.Fatalf("retry freeze failure class = %q, want terminal", class)
	}
}

func TestProductionRenderLegacyApprovedRetryDoesNotReuseUnresolvedTask(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	scope := agentRuntimeServiceScope()
	now := time.Now().UTC()
	task := model.Task{
		ID: "legacy-retry-task", UserID: scope.ActorUserID, ProjectID: scope.CanvasID,
		Type: "canvas_video", Capability: "video", Status: model.TaskStatusFailed,
		BillingOrderID: "legacy-retry-order", CreatedAt: now, UpdatedAt: now,
	}
	order := model.BillingOrder{
		ID: task.BillingOrderID, UserID: scope.ActorUserID, TaskID: task.ID,
		IdempotencyKey: "legacy-retry-order-key", Status: model.BillingStatusUncertain,
		AmountMicrocredits: 10_000_000, CreatedAt: now, UpdatedAt: now,
	}
	for _, value := range []any{&task, &order} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	artifact := model.AgentProductionArtifact{
		ID: "legacy-retry-artifact", Status: model.AgentProductionArtifactAwaitingApproval,
		Kind: model.AgentProductionArtifactVideoClip, Attempt: 1,
		TaskID: task.ID, BillingOrderID: order.ID,
	}
	call := model.AgentToolCall{IdempotencyKey: "legacy-retry-call"}
	arguments := agentruntime.ProductionRenderArguments{
		ArtifactID: artifact.ID, Attempt: 1,
		GenerationModel: agentruntime.GenerationModelSelection{ChannelID: "video-channel", Model: "video-model"},
		VideoConfig:     &agentruntime.VideoRenderConfig{DurationSeconds: 10, AspectRatio: "16:9", Quality: "720p"},
	}

	_, _, err := svc.ensureProductionArtifactTask(scope, &call, arguments, artifact)
	code, ok := agentProductionRenderFailureCode(err)
	if !ok || code != "production_previous_billing_unresolved" {
		t.Fatalf("legacy retry task error = %v code=%q, want production_previous_billing_unresolved", err, code)
	}
	var taskCount int64
	if err := db.Model(&model.Task{}).Where("operation = ?", "production_render:"+scope.RunID).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 {
		t.Fatalf("legacy retry created %d new production tasks, want 0", taskCount)
	}
}

func TestRepeatedDeterministicToolFailureBecomesTerminal(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "repeat-failure-loop-guard", UserMessage: "执行计划", MaxSteps: 8,
		Configuration: guidedAgentRuntimeConfigurationInput(),
	})
	if err != nil {
		t.Fatal(err)
	}
	arguments := json.RawMessage(`{"planKey":"plan-1","baseVersion":0}`)
	previous := model.AgentToolCall{
		ID: "previous-repeat-failure", RunID: started.Run.ID, ToolCallID: "plan-call-1", ActionVersion: 1,
		ToolName: string(agentruntime.ToolProductionPlan), Status: agentruntime.ToolCallFailed,
		InputJSON: string(arguments), OutputJSON: `{"reason":"version conflict"}`,
		ErrorCode: "production_plan_version_conflict", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := db.Create(&previous).Error; err != nil {
		t.Fatal(err)
	}
	state := started.State
	state.LastToolResult = &agentruntime.ToolResult{
		ToolCallID: previous.ToolCallID, ActionVersion: previous.ActionVersion,
		Succeeded: false, ErrorCode: previous.ErrorCode, Output: json.RawMessage(previous.OutputJSON),
	}
	next := &agentruntime.ToolCallDecision{
		ToolCallID: "plan-call-2", ToolName: agentruntime.ToolProductionPlan, ActionVersion: 1,
		Arguments: arguments, ExpectedDelivery: agentRuntimeTestCanvasDelivery(),
	}

	class, err := svc.rejectedToolFailureClass(
		scope,
		state,
		next,
		previous.ErrorCode,
		json.RawMessage(previous.OutputJSON),
		agentruntime.ToolFailureAgentRepairable,
	)
	if err != nil {
		t.Fatal(err)
	}
	if class != agentruntime.ToolFailureTerminal {
		t.Fatalf("repeated failure class = %q, want terminal", class)
	}
}

func TestRepeatedToolFailureWithDifferentEvidenceRemainsRepairable(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "different-failure-evidence", UserMessage: "执行计划", MaxSteps: 8,
		Configuration: guidedAgentRuntimeConfigurationInput(),
	})
	if err != nil {
		t.Fatal(err)
	}
	previous := model.AgentToolCall{
		ID: "previous-different-evidence", RunID: started.Run.ID, ToolCallID: "plan-call-1", ActionVersion: 1,
		ToolName: string(agentruntime.ToolProductionPlan), Status: agentruntime.ToolCallFailed,
		InputJSON:  `{"baseVersion":0,"planKey":"plan-1"}`,
		OutputJSON: `{"reason":"version conflict at revision 1"}`,
		ErrorCode:  "production_plan_version_conflict", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := db.Create(&previous).Error; err != nil {
		t.Fatal(err)
	}
	state := started.State
	state.LastToolResult = &agentruntime.ToolResult{
		ToolCallID: previous.ToolCallID, ActionVersion: previous.ActionVersion,
		Succeeded: false, ErrorCode: previous.ErrorCode, Output: json.RawMessage(previous.OutputJSON),
	}
	next := &agentruntime.ToolCallDecision{
		ToolCallID: "plan-call-2", ToolName: agentruntime.ToolProductionPlan, ActionVersion: 1,
		Arguments:        json.RawMessage(`{"planKey":"plan-1","baseVersion":0}`),
		ExpectedDelivery: agentRuntimeTestCanvasDelivery(),
	}

	class, err := svc.rejectedToolFailureClass(
		scope,
		state,
		next,
		previous.ErrorCode,
		json.RawMessage(`{"reason":"version conflict at revision 2"}`),
		agentruntime.ToolFailureAgentRepairable,
	)
	if err != nil {
		t.Fatal(err)
	}
	if class != agentruntime.ToolFailureAgentRepairable {
		t.Fatalf("different failure evidence class = %q, want agent_repairable", class)
	}
}

func TestAgentToolArgumentsCompareJSONSemantically(t *testing.T) {
	if !equalAgentToolArguments(
		`{"baseVersion":0,"plan":{"title":"sample","shots":[1,2]}}`,
		json.RawMessage(`{"plan":{"shots":[1,2],"title":"sample"},"baseVersion":0}`),
	) {
		t.Fatal("semantically equal tool arguments were treated as different")
	}
}

func TestProductionStoryboardResourceAcceptsCommittedReadyArtifact(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	scope := agentRuntimeServiceScope()
	now := time.Now().UTC()
	createAgentRuntimeScopedRunFacts(t, db, scope, now)
	plan := model.AgentProductionPlanVersion{
		ID: "committed-storyboard-plan-version", PlanKey: "committed-storyboard-plan",
		TenantKind: scope.TenantKind, TenantID: scope.TenantID, DomainProjectID: scope.DomainProjectID,
		CanvasID: scope.CanvasID, CreatedByRunID: scope.RunID, Version: 1,
		Status: model.AgentProductionPlanActive, Title: "已提交分镜仍可生成视频", TargetDurationMS: 10_000,
		Script: "分镜已落入画布。", ShotsJSON: `[]`, ExpectedDeliveryJSON: `{}`,
		CreatedAt: now, UpdatedAt: now,
	}
	resource := model.Resource{
		ID: "committed-storyboard-resource", UserID: scope.ActorUserID, Kind: "image",
		Status: model.ResourceStatusReady, MimeType: "image/png", ObjectKey: "production/shot-1.png",
		CreatedAt: now, UpdatedAt: now,
	}
	storyboard := model.AgentProductionArtifact{
		ID: "committed-storyboard-artifact", PlanKey: plan.PlanKey, PlanVersionID: plan.ID,
		PlanVersion: plan.Version, ShotKey: "shot-1", Kind: model.AgentProductionArtifactStoryboardImage,
		Status: model.AgentProductionArtifactCommitted, Attempt: 1, CanvasNodeID: "storyboard-node",
		ResourceID: resource.ID, CreatedAt: now, UpdatedAt: now,
	}
	video := model.AgentProductionArtifact{
		ID: "awaiting-video-artifact", PlanKey: plan.PlanKey, PlanVersionID: plan.ID,
		PlanVersion: plan.Version, ShotKey: storyboard.ShotKey, Kind: model.AgentProductionArtifactVideoClip,
		Status: model.AgentProductionArtifactAwaitingApproval, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&video).Error; err != nil {
		t.Fatal(err)
	}

	resolved, err := svc.productionStoryboardResource(scope, agentruntime.ProductionRenderArguments{
		PlanKey: plan.PlanKey, PlanVersion: plan.Version,
	}, video)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ID != resource.ID {
		t.Fatalf("resolved storyboard resource = %s", resolved.ID)
	}
	inputMode, frozenID, err := svc.freezeProductionVideoInputResource(scope, agentProductionRenderRequest{
		PlanKey: plan.PlanKey, PlanVersion: plan.Version,
	}, video, agentRuntimeCallableModelFact{ProviderCapabilities: &PublicProviderCapabilities{SupportsTextToVideo: true}})
	if err != nil {
		t.Fatal(err)
	}
	if inputMode != "storyboard" || frozenID != resource.ID {
		t.Fatalf("frozen video input = mode %q resource %q, want storyboard / %q", inputMode, frozenID, resource.ID)
	}
	input, taskType, err := svc.productionRenderTaskInput(scope, agentruntime.ProductionRenderArguments{
		PlanKey: plan.PlanKey, PlanVersion: plan.Version,
		VideoInputMode: inputMode, VideoInputResourceID: frozenID,
		VideoConfig: &agentruntime.VideoRenderConfig{DurationSeconds: 5, AspectRatio: "16:9", Quality: "720p"},
	}, video, "镜头缓慢推进")
	if err != nil {
		t.Fatal(err)
	}
	if taskType != "canvas_video" || len(input.ReferenceImages) != 1 || input.ReferenceImages[0].ID != resource.ID {
		t.Fatalf("storyboard video task input = %#v, taskType=%s", input, taskType)
	}
}

func TestProductionRenderBuildsTextToVideoTaskWithoutStoryboardResource(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	scope := agentRuntimeServiceScope()
	now := time.Now().UTC()
	createAgentRuntimeScopedRunFacts(t, db, scope, now)
	record, err := svc.repo.AppendAgentProductionPlanVersion(repository.AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "text-to-video-plan", BaseVersion: 0,
		Draft: agentruntime.ProductionPlanDraft{
			Title: "五秒文生视频", TargetDurationMS: 5_000, Script: "城市天台镜头。",
			Shots: []agentruntime.ShotPlanDraft{{
				ShotKey: "shot-1", Order: 1, DurationMS: 5_000, ScriptText: "人物站在城市天台",
				Deliverables: agentRuntimeDualProductionDeliverables(),
				ImagePrompt:  "城市天台人物定帧", VideoPrompt: "微风吹动衣角，镜头缓慢推进", Dependencies: []string{},
			}},
		},
		Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	var video model.AgentProductionArtifact
	for _, artifact := range record.Artifacts {
		if artifact.Kind == model.AgentProductionArtifactVideoClip {
			video = artifact
			break
		}
	}
	if video.ID == "" {
		t.Fatal("video artifact was not created")
	}
	request := agentProductionRenderRequest{PlanKey: record.Plan.PlanKey, PlanVersion: record.Plan.Version}
	inputMode, frozenID, err := svc.freezeProductionVideoInputResource(scope, request, video, agentRuntimeCallableModelFact{
		ProviderCapabilities: &PublicProviderCapabilities{SupportsTextToVideo: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if inputMode != "text_to_video" || frozenID != "" {
		t.Fatalf("text-to-video frozen input = mode %q resource %q", inputMode, frozenID)
	}
	_, _, err = svc.freezeProductionVideoInputResource(scope, request, video, agentRuntimeCallableModelFact{
		ProviderCapabilities: &PublicProviderCapabilities{SupportsTextToVideo: false},
	})
	if code, ok := agentProductionRenderFailureCode(err); !ok || code != "production_prerequisite_missing" {
		t.Fatalf("missing non-text-to-video prerequisite error = %v, code=%q", err, code)
	}

	input, taskType, err := svc.productionRenderTaskInput(scope, agentruntime.ProductionRenderArguments{
		PlanKey: record.Plan.PlanKey, PlanVersion: record.Plan.Version,
		GenerationModel:      agentruntime.GenerationModelSelection{ChannelID: "video-channel", Model: "doubao-seedance-2-0-260128"},
		VideoInputMode:       agentruntime.ProductionVideoInputTextToVideo,
		VideoInputResourceID: frozenID,
		VideoConfig:          &agentruntime.VideoRenderConfig{DurationSeconds: 5, AspectRatio: "9:16", Quality: "720p", GenerateAudio: false},
	}, video, "微风吹动衣角，镜头缓慢推进")
	if err != nil {
		t.Fatal(err)
	}
	if taskType != "canvas_video" || input.Mode != "video" || input.Config.Size != "9:16" || len(input.ReferenceImages) != 0 || input.Prompt != "微风吹动衣角，镜头缓慢推进" {
		t.Fatalf("text-to-video task input = %#v, taskType=%s", input, taskType)
	}
	legacyFrozen, err := json.Marshal(agentruntime.ProductionRenderArguments{
		PlanKey: record.Plan.PlanKey, PlanVersion: record.Plan.Version, ArtifactID: video.ID,
		GenerationModel: agentruntime.GenerationModelSelection{ChannelID: "video-channel", Model: "doubao-seedance-2-0-260128"},
		VideoConfig:     &agentruntime.VideoRenderConfig{DurationSeconds: 5, Quality: "720p"},
		FrozenRenderQuote: agentruntime.FrozenRenderQuote{
			BillingMode: "fixed_request", Quantity: 1, QuoteFingerprint: "legacy-video-quote",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeFrozenProductionRenderArguments(legacyFrozen); !errors.Is(err, errAgentRuntimeProductionRenderInput) {
		t.Fatalf("legacy frozen video arguments error = %v, want explicit invalid input", err)
	}
}

func createAgentRuntimeScopedRunFacts(t *testing.T, db *gorm.DB, scope agentruntime.Scope, now time.Time) {
	t.Helper()
	thread := model.AgentThread{
		ID: scope.ThreadID, TenantKind: scope.TenantKind, TenantID: scope.TenantID,
		CreatedByUserID: scope.ActorUserID, DomainProjectID: scope.DomainProjectID,
		CanvasID: scope.CanvasID, Status: agentruntime.ThreadActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&thread).Error; err != nil {
		t.Fatal(err)
	}
	run := model.AgentRun{
		ID: scope.RunID, ThreadID: scope.ThreadID, ActorUserID: scope.ActorUserID,
		ClientRequestID: "scoped-production-fixture", Status: agentruntime.RunRunning,
		LastEventSequence: 2, StateVersion: 1, MaxSteps: 6,
		ModelRecordID: "runtime-agent-model", ModelKey: "gpt-5.5",
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion:    agentruntime.CurrentRuntimeVersion,
		PolicyVersion:     agentruntime.CurrentPolicyVersion,
		CreatedAt:         now, UpdatedAt: now,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
}

func TestProductionGenerationFailureOutputIncludesTaskReason(t *testing.T) {
	output := productionGenerationFailureOutput(model.Task{ID: "task-image", Error: "APIMart GPT Image 2 不支持图片质量 high"})
	if output["taskId"] != "task-image" || output["reason"] != "APIMart GPT Image 2 不支持图片质量 high" {
		t.Fatalf("failure output = %#v", output)
	}
}

func TestReconcileSucceededProductionArtifactMaterializesRemoteResultExactlyOnce(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	downloads := 0
	assetServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/paid-result.png" {
			http.NotFound(writer, request)
			return
		}
		downloads++
		writer.Header().Set("Content-Type", "image/png")
		_, _ = writer.Write([]byte("paid-provider-image"))
	}))
	defer assetServer.Close()

	decisionServer, _ := newAgentRuntimeDecisionServer(t, `{"kind":"final","final":{"message":"ok","expectedDelivery":{"kind":"answer","requiredArtifacts":["text"],"completionCriteria":[{"fact":"final_message"}]}}}`)
	defer decisionServer.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, decisionServer.URL)
	createAgentRuntimeCanvas(t, db)
	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "recover-paid-media", UserMessage: "生成分镜图", MaxSteps: 6,
		Configuration: AgentRuntimeConfigurationInput{ExecutionMode: agentruntime.ExecutionAutomatic},
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := svc.repo.AppendAgentProductionPlanVersion(repository.AppendAgentProductionPlanInput{
		Scope: scope, RunID: started.Run.ID, PlanKey: "recover-paid-media-plan", BaseVersion: 0,
		Draft: agentruntime.ProductionPlanDraft{
			Title: "恢复付费产物", TargetDurationMS: 1_000, Script: "画面。",
			Shots: []agentruntime.ShotPlanDraft{{
				ShotKey: "shot-1", Order: 1, DurationMS: 1_000, ScriptText: "画面",
				Deliverables: agentRuntimeDualProductionDeliverables(),
				ImagePrompt:  "画面", VideoPrompt: "动作", Dependencies: []string{},
			}},
		},
		Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var artifact model.AgentProductionArtifact
	for _, candidate := range record.Artifacts {
		if candidate.Kind == model.AgentProductionArtifactStoryboardImage {
			artifact = candidate
			break
		}
	}
	if artifact.ID == "" {
		t.Fatal("storyboard artifact missing")
	}
	now := time.Now().UTC()
	task := model.Task{
		ID: "paid-success-task", UserID: scope.ActorUserID, ProjectID: scope.CanvasID,
		Type: "canvas_image", Capability: "image", Status: model.TaskStatusSucceeded,
		Stage: "已完成", Progress: 100, BillingOrderID: "paid-success-order",
		ResultJSON:  `{"mode":"image","images":[{"dataUrl":"` + assetServer.URL + `/paid-result.png"}]}`,
		CompletedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	order := model.BillingOrder{
		ID: task.BillingOrderID, UserID: scope.ActorUserID, IdempotencyKey: "paid-success-order-key",
		TaskID: task.ID, Capability: "image", Status: model.BillingStatusSettled,
		AmountMicrocredits: 1_000_000, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentProductionArtifact{}).Where("id = ?", artifact.ID).
		Select("status", "attempt", "task_id", "billing_order_id", "last_error_code").
		Updates(model.AgentProductionArtifact{
			Status: model.AgentProductionArtifactFailed, Attempt: 1,
			TaskID: task.ID, BillingOrderID: order.ID, LastErrorCode: "production_result_invalid",
		}).Error; err != nil {
		t.Fatal(err)
	}

	if err := svc.reconcileSucceededProductionArtifacts(scope); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.materializeSucceededProductionTaskResult(scope, task, artifact.Kind); err != nil {
		t.Fatal(err)
	}
	if err := svc.reconcileSucceededProductionArtifacts(scope); err != nil {
		t.Fatal(err)
	}
	latestTask, err := svc.repo.TaskForUser(scope.ActorUserID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	resourceID, err := taskResultResourceID(latestTask.ResultJSON, artifact.Kind)
	if err != nil {
		t.Fatal(err)
	}
	resource, err := svc.repo.ResourceForUser(scope.ActorUserID, resourceID)
	if err != nil {
		t.Fatal(err)
	}
	if resource.Status != model.ResourceStatusReady || resource.Kind != "image" {
		t.Fatalf("recovered resource = %#v", resource)
	}
	artifacts, err := svc.repo.AgentProductionArtifactsForVersion(scope, record.Plan.PlanKey, record.Plan.Version)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range artifacts {
		if candidate.ID == artifact.ID && (candidate.Status != model.AgentProductionArtifactSucceeded || candidate.ResourceID != resource.ID || candidate.LastErrorCode != "") {
			t.Fatalf("recovered artifact = %#v", candidate)
		}
	}
	var resourceCount, taskCount, orderCount, ledgerCount int64
	if err := db.Model(&model.Resource{}).Where("id = ?", resource.ID).Count(&resourceCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("id = ?", order.ID).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ?", order.ID).Count(&ledgerCount).Error; err != nil {
		t.Fatal(err)
	}
	if resourceCount != 1 || taskCount != 1 || orderCount != 1 || ledgerCount != 0 {
		t.Fatalf("recovery facts resources=%d tasks=%d orders=%d ledgers=%d", resourceCount, taskCount, orderCount, ledgerCount)
	}
	if downloads != 1 {
		t.Fatalf("provider result downloads = %d, want 1", downloads)
	}
}

func TestLegacyV3ProductionRenderWithoutUserPinUsesFrozenCallableModelSetAndFreezesQuoteBeforeApproval(t *testing.T) {
	t.Skip("tool schema v3 is terminal-history-only; v4 media.generate freezes model and quote facts in its dedicated suite")
	var decision string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if decision == "" {
			t.Error("model decision was requested before the production artifact existed")
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeAgentRuntimeChatStream(t, writer, "chatcmpl-production", decision, 0, 0, 0)
	}))
	defer server.Close()
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")

	svc, db, fixture := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	createAgentRuntimeImageModel(t, db, fixture)
	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "production-render-quote", UserMessage: "生成第一镜分镜图", MaxSteps: 6,
		Configuration: AgentRuntimeConfigurationInput{ExecutionMode: agentruntime.ExecutionAutomatic},
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := svc.repo.AppendAgentProductionPlanVersion(repository.AppendAgentProductionPlanInput{
		Scope: scope, RunID: started.Run.ID, PlanKey: "plan-render-quote", BaseVersion: 0,
		Draft: agentruntime.ProductionPlanDraft{
			Title: "橙子广告", TargetDurationMS: 5_000, Script: "鲜橙落水。",
			Shots: []agentruntime.ShotPlanDraft{{
				ShotKey: "shot-1", Order: 1, DurationMS: 5_000, ScriptText: "鲜橙落水",
				Deliverables: agentRuntimeDualProductionDeliverables(),
				ImagePrompt:  "鲜橙产品特写", VideoPrompt: "慢镜头水花", Dependencies: []string{},
			}},
		},
		Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var imageArtifact model.AgentProductionArtifact
	for _, artifact := range record.Artifacts {
		if artifact.Kind == model.AgentProductionArtifactStoryboardImage {
			imageArtifact = artifact
			break
		}
	}
	if imageArtifact.ID == "" {
		t.Fatal("storyboard image artifact was not created")
	}
	decision = `{"kind":"tool_call","toolCall":{"toolCallId":"render-shot-1","toolName":"production.render","actionVersion":1,"arguments":{"planKey":"` + record.Plan.PlanKey + `","planVersion":1,"artifactId":"` + imageArtifact.ID + `","generationModel":{"channelId":"runtime-image-channel","model":"kz_gpt_image2"},"imageConfig":{"size":"1:1","resolution":"1K","quality":"medium","count":1}}}}`
	decision = agentRuntimeToolDecisionWithDelivery(t, decision, agentRuntimeTestImageDelivery())

	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	waiting, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.State.Status != agentruntime.RunWaitingApproval || waiting.State.PendingToolCall == nil || waiting.State.PendingToolCall.ToolName != agentruntime.ToolProductionRender {
		t.Fatalf("production render approval state = %#v", waiting.State)
	}
	artifacts, err := svc.repo.AgentProductionArtifactsForVersion(scope, record.Plan.PlanKey, record.Plan.Version)
	if err != nil {
		t.Fatal(err)
	}
	for _, artifact := range artifacts {
		if artifact.ID == imageArtifact.ID && artifact.Status != model.AgentProductionArtifactAwaitingApproval {
			t.Fatalf("render artifact status = %s", artifact.Status)
		}
	}
	toolCall, err := svc.repo.AgentToolCallForScope(scope, "render-shot-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	var frozen struct {
		AmountMicrocredits       int64     `json:"amountMicrocredits"`
		QuoteFingerprint         string    `json:"quoteFingerprint"`
		QuoteID                  string    `json:"quoteId"`
		ApprovalFingerprint      string    `json:"approvalFingerprint"`
		TaskID                   string    `json:"taskId"`
		BillingIdempotencyKey    string    `json:"billingIdempotencyKey"`
		ChannelModelID           string    `json:"channelModelId"`
		Capability               string    `json:"capability"`
		TaskType                 string    `json:"taskType"`
		ParametersJSON           string    `json:"parametersJson"`
		ProviderCapabilitiesJSON string    `json:"providerCapabilitiesJson"`
		ExpiresAt                time.Time `json:"expiresAt"`
	}
	if err := json.Unmarshal([]byte(toolCall.InputJSON), &frozen); err != nil {
		t.Fatal(err)
	}
	if frozen.AmountMicrocredits != 250 || frozen.QuoteFingerprint == "" || frozen.QuoteID == "" ||
		frozen.ApprovalFingerprint == "" || frozen.TaskID == "" || frozen.BillingIdempotencyKey == "" ||
		frozen.ChannelModelID != "runtime-image-model" || frozen.Capability != "image" || frozen.TaskType != "canvas_image" ||
		frozen.ParametersJSON == "" || frozen.ProviderCapabilitiesJSON == "" || frozen.ExpiresAt.IsZero() {
		t.Fatalf("frozen production quote = %#v input=%s", frozen, toolCall.InputJSON)
	}
	var mediaTasks, mediaOrders int64
	if err := db.Model(&model.Task{}).Where("user_id = ? AND capability IN ?", scope.ActorUserID, []string{"image", "video"}).Count(&mediaTasks).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("user_id = ? AND capability IN ?", scope.ActorUserID, []string{"image", "video"}).Count(&mediaOrders).Error; err != nil {
		t.Fatal(err)
	}
	if mediaTasks != 0 || mediaOrders != 0 {
		t.Fatalf("approval created commercial facts: tasks=%d orders=%d", mediaTasks, mediaOrders)
	}
	rejected, err := svc.SubmitAgentToolApproval(scope, AgentToolApprovalSubmission{
		ToolCallID: "render-shot-1", ActionVersion: 1, Decision: agentruntime.ToolApprovalRejected,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rejected.State.Status != agentruntime.RunCancelled || rejected.State.LastToolResult == nil || rejected.State.LastToolResult.ErrorCode != "tool_approval_rejected" {
		t.Fatalf("rejected production render state = %#v", rejected.State)
	}
	artifacts, err = svc.repo.AgentProductionArtifactsForVersion(scope, record.Plan.PlanKey, record.Plan.Version)
	if err != nil {
		t.Fatal(err)
	}
	for _, artifact := range artifacts {
		if artifact.ID == imageArtifact.ID && (artifact.Status != model.AgentProductionArtifactFailed || artifact.LastErrorCode != "tool_approval_rejected" || artifact.TaskID != "" || artifact.BillingOrderID != "") {
			t.Fatalf("rejected render artifact facts = %#v", artifact)
		}
	}
	if err := db.Model(&model.Task{}).Where("user_id = ? AND capability IN ?", scope.ActorUserID, []string{"image", "video"}).Count(&mediaTasks).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("user_id = ? AND capability IN ?", scope.ActorUserID, []string{"image", "video"}).Count(&mediaOrders).Error; err != nil {
		t.Fatal(err)
	}
	if mediaTasks != 0 || mediaOrders != 0 {
		t.Fatalf("rejected approval created commercial facts: tasks=%d orders=%d", mediaTasks, mediaOrders)
	}
	var modelTasks int64
	if err := db.Model(&model.Task{}).Where("user_id = ? AND type = ?", scope.ActorUserID, agentRuntimeModelTaskType).Count(&modelTasks).Error; err != nil {
		t.Fatal(err)
	}
	if modelTasks != 1 {
		t.Fatalf("rejected cost approval created another model task: %d", modelTasks)
	}
}

func TestLegacyV3AgentRenderPrepareFailureReturnsToolResultToOneNextModelStep(t *testing.T) {
	t.Skip("tool schema v3 is terminal-history-only; v4 media.generate exposes preparation failures explicitly")
	var decision string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeAgentRuntimeChatStream(t, writer, "chatcmpl-production", decision, 0, 0, 0)
	}))
	defer server.Close()
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")

	svc, db, fixture := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	createAgentRuntimeImageModel(t, db, fixture)
	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "production-render-repair", UserMessage: "生成第一镜分镜图", MaxSteps: 6,
		Configuration: AgentRuntimeConfigurationInput{ExecutionMode: agentruntime.ExecutionAutomatic},
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := svc.repo.AppendAgentProductionPlanVersion(repository.AppendAgentProductionPlanInput{
		Scope: scope, RunID: started.Run.ID, PlanKey: "plan-render-repair", BaseVersion: 0,
		Draft: agentruntime.ProductionPlanDraft{
			Title: "橙子广告", TargetDurationMS: 5_000, Script: "鲜橙落水。",
			Shots: []agentruntime.ShotPlanDraft{{
				ShotKey: "shot-1", Order: 1, DurationMS: 5_000, ScriptText: "鲜橙落水",
				Deliverables: agentRuntimeDualProductionDeliverables(),
				ImagePrompt:  "鲜橙产品特写", VideoPrompt: "慢镜头水花", Dependencies: []string{},
			}},
		},
		Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var imageArtifact model.AgentProductionArtifact
	for _, artifact := range record.Artifacts {
		if artifact.Kind == model.AgentProductionArtifactStoryboardImage {
			imageArtifact = artifact
			break
		}
	}
	decision = agentRuntimeToolDecisionWithDelivery(t,
		`{"kind":"tool_call","toolCall":{"toolCallId":"render-repair","toolName":"production.render","actionVersion":1,"arguments":{"planKey":"`+record.Plan.PlanKey+`","planVersion":1,"artifactId":"`+imageArtifact.ID+`","generationModel":{"channelId":"runtime-image-channel","model":"kz_gpt_image2"},"imageConfig":{"size":"1:1","resolution":"1K","quality":"ultra","count":1}}}}`,
		agentRuntimeTestImageDelivery(),
	)
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	state, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != agentruntime.RunRunning || state.LastToolResult == nil || state.LastToolResult.Succeeded ||
		state.LastToolResult.ErrorCode != "generation_parameter_unsupported" {
		t.Fatalf("repairable render checkpoint = %#v", state)
	}
	call, err := svc.repo.AgentToolCallForScope(scope, "render-repair", 1)
	if err != nil {
		t.Fatal(err)
	}
	if call.Status != agentruntime.ToolCallFailed || call.ErrorCode != state.LastToolResult.ErrorCode {
		t.Fatalf("repairable render tool call = %#v", call)
	}
	var modelTaskCount, toolCallCount, mediaTaskCount, mediaOrderCount int64
	if err := db.Model(&model.Task{}).Where("type = ? AND operation = ?", agentRuntimeModelTaskType, agentRuntimeModelOperation(scope.RunID)).Count(&modelTaskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentToolCall{}).Where("run_id = ?", scope.RunID).Count(&toolCallCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("user_id = ? AND capability IN ?", scope.ActorUserID, []string{"image", "video"}).Count(&mediaTaskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("user_id = ? AND capability IN ?", scope.ActorUserID, []string{"image", "video"}).Count(&mediaOrderCount).Error; err != nil {
		t.Fatal(err)
	}
	if modelTaskCount != 2 || toolCallCount != 1 || mediaTaskCount != 0 || mediaOrderCount != 0 {
		t.Fatalf("repair facts modelTasks=%d toolCalls=%d mediaTasks=%d mediaOrders=%d", modelTaskCount, toolCallCount, mediaTaskCount, mediaOrderCount)
	}
	progress, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if progress.ModelTask == nil || progress.ModelTask.ID != agentRuntimeModelTaskID(scope.RunID, state.StepNumber) {
		t.Fatalf("reused corrective model task = %#v", progress.ModelTask)
	}
	if err := db.Model(&model.Task{}).Where("type = ? AND operation = ?", agentRuntimeModelTaskType, agentRuntimeModelOperation(scope.RunID)).Count(&modelTaskCount).Error; err != nil {
		t.Fatal(err)
	}
	if modelTaskCount != 2 {
		t.Fatalf("corrective model task replay count = %d, want 2", modelTaskCount)
	}

	decision = agentRuntimeToolDecisionWithDelivery(t,
		`{"kind":"tool_call","toolCall":{"toolCallId":"render-repair-2","toolName":"production.render","actionVersion":1,"arguments":{"planKey":"`+record.Plan.PlanKey+`","planVersion":1,"artifactId":"`+imageArtifact.ID+`","generationModel":{"channelId":"runtime-image-channel","model":"kz_gpt_image2"},"imageConfig":{"size":"1:1","resolution":"1K","quality":"ultra","count":1}}}}`,
		agentRuntimeTestImageDelivery(),
	)
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	terminal, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if terminal.Status != agentruntime.RunFailed || terminal.LastToolResult == nil ||
		terminal.LastToolResult.ErrorCode != "generation_parameter_unsupported" {
		t.Fatalf("repeated render preparation failure = %#v", terminal)
	}
	if err := db.Model(&model.AgentToolCall{}).Where("run_id = ?", scope.RunID).Count(&toolCallCount).Error; err != nil {
		t.Fatal(err)
	}
	if toolCallCount != 2 {
		t.Fatalf("repeated render tool call count = %d, want 2", toolCallCount)
	}
}

func TestLegacyV3ProductionRenderApprovalCreatesOneRecoverableTaskAndAdoptsReadyResource(t *testing.T) {
	t.Skip("tool schema v3 is terminal-history-only; v4 media generation recovery and resource adoption have dedicated coverage")
	var decision string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeAgentRuntimeChatStream(t, writer, "chatcmpl-production", decision, 0, 0, 0)
	}))
	defer server.Close()
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")

	svc, db, fixture := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	createAgentRuntimeImageModel(t, db, fixture)
	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "production-render-recovery", UserMessage: "生成第一镜分镜图", MaxSteps: 6,
		Configuration: AgentRuntimeConfigurationInput{
			ExecutionMode: agentruntime.ExecutionAutomatic,
			GenerationModels: agentruntime.GenerationModelSelections{Image: &agentruntime.GenerationModelSelection{
				ChannelID: "runtime-image-channel", Model: "kz_gpt_image2",
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := svc.repo.AppendAgentProductionPlanVersion(repository.AppendAgentProductionPlanInput{
		Scope: scope, RunID: started.Run.ID, PlanKey: "plan-render-recovery", BaseVersion: 0,
		Draft: agentruntime.ProductionPlanDraft{
			Title: "橙子广告", TargetDurationMS: 5_000, Script: "鲜橙落水。",
			Shots: []agentruntime.ShotPlanDraft{{
				ShotKey: "shot-1", Order: 1, DurationMS: 5_000, ScriptText: "鲜橙落水",
				Deliverables: agentRuntimeDualProductionDeliverables(),
				ImagePrompt:  "鲜橙产品特写", VideoPrompt: "慢镜头水花", Dependencies: []string{},
			}},
		},
		Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var imageArtifact model.AgentProductionArtifact
	for _, artifact := range record.Artifacts {
		if artifact.Kind == model.AgentProductionArtifactStoryboardImage {
			imageArtifact = artifact
			break
		}
	}
	decision = agentRuntimeToolDecisionWithDelivery(t,
		`{"kind":"tool_call","toolCall":{"toolCallId":"render-recover","toolName":"production.render","actionVersion":1,"arguments":{"planKey":"`+record.Plan.PlanKey+`","planVersion":1,"artifactId":"`+imageArtifact.ID+`","generationModel":{"channelId":"runtime-image-channel","model":"kz_gpt_image2"},"imageConfig":{"size":"1:1","resolution":"1K","quality":"medium","count":1}}}}`,
		agentRuntimeTestImageDelivery(),
	)
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	waiting, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.State.Status != agentruntime.RunWaitingApproval {
		t.Fatalf("render approval state = %s", waiting.State.Status)
	}
	approved, err := svc.SubmitAgentToolApproval(scope, AgentToolApprovalSubmission{
		ToolCallID: "render-recover", ActionVersion: 1, Decision: agentruntime.ToolApprovalApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if approved.State.Status != agentruntime.RunWaitingTool {
		t.Fatalf("approved render state = %s", approved.State.Status)
	}

	coordinated, err := svc.CoordinatePendingAgentTool(scope, CoordinateAgentToolInput{ToolCallID: "render-recover", ActionVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if coordinated.State.Status != agentruntime.RunWaitingTool || !coordinated.State.PendingToolStarted {
		t.Fatalf("coordinated render state = %#v result=%#v", coordinated.State, coordinated.State.LastToolResult)
	}
	if _, err := svc.CoordinatePendingAgentTool(scope, CoordinateAgentToolInput{ToolCallID: "render-recover", ActionVersion: 1}); err != nil {
		t.Fatal(err)
	}
	artifacts, err := svc.repo.AgentProductionArtifactsForVersion(scope, record.Plan.PlanKey, record.Plan.Version)
	if err != nil {
		t.Fatal(err)
	}
	var bound model.AgentProductionArtifact
	for _, artifact := range artifacts {
		if artifact.ID == imageArtifact.ID {
			bound = artifact
		}
	}
	if bound.Status != model.AgentProductionArtifactQueued || bound.Attempt != 1 || bound.TaskID == "" || bound.BillingOrderID == "" {
		t.Fatalf("bound production artifact = %#v", bound)
	}
	var mediaTasks, mediaOrders, reserveEntries int64
	if err := db.Model(&model.Task{}).Where("id = ?", bound.TaskID).Count(&mediaTasks).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("id = ?", bound.BillingOrderID).Count(&mediaOrders).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ? AND type = ?", bound.BillingOrderID, model.CreditLedgerReserve).Count(&reserveEntries).Error; err != nil {
		t.Fatal(err)
	}
	if mediaTasks != 1 || mediaOrders != 1 || reserveEntries != 1 {
		t.Fatalf("commercial facts tasks=%d orders=%d reserves=%d", mediaTasks, mediaOrders, reserveEntries)
	}
	storedTask, err := svc.repo.TaskForUser(scope.ActorUserID, bound.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTask.Audience != model.TaskAudienceInternal {
		t.Fatalf("production media task audience = %q, want internal", storedTask.Audience)
	}

	now := time.Now().UTC()
	resource := model.Resource{ID: "production-ready-image", UserID: scope.ActorUserID, Kind: "image", Status: model.ResourceStatusReady, Provider: "oss", ObjectKey: "production/ready.png", MimeType: "image/png", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	resultJSON := `{"images":[{"resourceId":"` + resource.ID + `","url":"/api/resources/` + resource.ID + `/file"}]}`
	if err := db.Exec("UPDATE tasks SET status = ?, stage = ?, progress = ?, result_json = ?, completed_at = ?, updated_at = ? WHERE id = ?", model.TaskStatusSucceeded, "已完成", 100, resultJSON, now, now, bound.TaskID).Error; err != nil {
		t.Fatal(err)
	}
	completed, err := svc.CoordinatePendingAgentTool(scope, CoordinateAgentToolInput{ToolCallID: "render-recover", ActionVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if completed.State.LastToolResult == nil || !completed.State.LastToolResult.Succeeded {
		t.Fatalf("completed production render = %#v", completed.State)
	}
	artifacts, err = svc.repo.AgentProductionArtifactsForVersion(scope, record.Plan.PlanKey, record.Plan.Version)
	if err != nil {
		t.Fatal(err)
	}
	for _, artifact := range artifacts {
		if artifact.ID == imageArtifact.ID && (artifact.Status != model.AgentProductionArtifactSucceeded || artifact.ResourceID != resource.ID || artifact.Attempt != 1) {
			t.Fatalf("completed production artifact = %#v", artifact)
		}
	}
}

func agentRuntimeDualProductionDeliverables() []agentruntime.ProductionShotDeliverable {
	return []agentruntime.ProductionShotDeliverable{
		agentruntime.ProductionShotDeliverableStoryboardImage,
		agentruntime.ProductionShotDeliverableVideoClip,
	}
}
