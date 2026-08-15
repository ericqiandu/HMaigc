package service

import (
	"encoding/json"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/model"
)

func TestAgentRuntimeFreezesCallableMediaModelFactsWithoutProviderSecrets(t *testing.T) {
	server, _ := newAgentRuntimeDecisionServer(t, `{"kind":"final","final":{"message":"ok","expectedDelivery":{"kind":"answer","requiredArtifacts":["text"],"completionCriteria":[{"fact":"final_message"}]}}}`)
	svc, db, fixture := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	createAgentRuntimeImageModel(t, db, fixture)

	input := StartAgentRuntimeInput{
		Scope: agentRuntimeServiceScope(), ClientRequestID: "context-catalog-request",
		UserMessage: "生成一张图片", MaxSteps: 6,
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
