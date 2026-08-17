package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

func TestAgentRuntimeFreezesSelectedGenerationModelAndSkillInstructions(t *testing.T) {
	server, _ := newAgentRuntimeDecisionServer(t, `{"kind":"final","final":{"message":"ok","expectedDelivery":{"kind":"answer","requiredArtifacts":["text"],"completionCriteria":[{"fact":"final_message"}]}}}`)
	svc, db, fixture := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	createAgentRuntimeImageModel(t, db, fixture)
	if err := db.Create(&model.Resource{ID: "runtime-reference-image", UserID: agentRuntimeServiceScope().ActorUserID, Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png", Width: 1280, Height: 720}).Error; err != nil {
		t.Fatal(err)
	}
	svc.agentRuntimeSkillResolver = func(_ context.Context, userID string, dir string) (*UpdreamSkill, error) {
		if userID != agentRuntimeServiceScope().ActorUserID || dir != "storyboard-director" {
			t.Fatalf("unexpected skill lookup: user=%q dir=%q", userID, dir)
		}
		return &UpdreamSkill{Dir: dir, Name: "分镜导演", Description: "拆解镜头", DetailText: "先读取画布事实，再输出可执行分镜。", Version: 7}, nil
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
	if len(promptContext.CallableModels) != 1 || promptContext.CallableModels[0].Model != "kz_gpt_image2" {
		t.Fatalf("selected callable models = %#v", promptContext.CallableModels)
	}
	if len(promptContext.Configuration.Skills) != 1 || promptContext.Configuration.Skills[0].Instructions != "先读取画布事实，再输出可执行分镜。" {
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

type agentRuntimePromptContextForTest struct {
	Configuration  agentruntime.RunConfiguration `json:"configuration"`
	CallableModels []struct {
		ChannelID             string                      `json:"channelId"`
		Model                 string                      `json:"model"`
		Capability            string                      `json:"capability"`
		UnitPriceMicrocredits int64                       `json:"unitPriceMicrocredits"`
		ProviderCapabilities  *PublicProviderCapabilities `json:"providerCapabilities"`
	} `json:"callableModels"`
}

func decodeAgentRuntimePromptContextForTest(t *testing.T, prompt string) agentRuntimePromptContextForTest {
	t.Helper()
	const prefix = "以下 JSON 是本轮唯一可信的运行事实。请自主决定直接交付或调用一个可用工具，并严格按系统约定返回一个 JSON 对象：\n"
	if !strings.HasPrefix(prompt, prefix) {
		t.Fatalf("agent prompt prefix is invalid: %q", prompt)
	}
	var context agentRuntimePromptContextForTest
	if err := json.Unmarshal([]byte(strings.TrimPrefix(prompt, prefix)), &context); err != nil {
		t.Fatal(err)
	}
	return context
}
