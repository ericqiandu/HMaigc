package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

type capturedVisionImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail"`
}

type capturedChatVisionContent struct {
	Type     string                 `json:"type"`
	Text     string                 `json:"text"`
	ImageURL capturedVisionImageURL `json:"image_url"`
}

type capturedResponsesVisionContent struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	ImageURL string `json:"image_url"`
	Detail   string `json:"detail"`
}

func TestProviderVisionChatCompletionRequestContract(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" || request.Header.Get("Authorization") != "Bearer frozen-secret" || request.Header.Get("ApiKey") != "" {
			t.Errorf("request contract path=%q authorization=%q apiKey=%q", request.URL.Path, request.Header.Get("Authorization"), request.Header.Get("ApiKey"))
		}
		var body struct {
			Model         string `json:"model"`
			Stream        bool   `json:"stream"`
			StreamOptions struct {
				IncludeUsage bool `json:"include_usage"`
			} `json:"stream_options"`
			Messages []struct {
				Role    string                      `json:"role"`
				Content []capturedChatVisionContent `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "deepseek-v4-flash-vision-exp" || !body.Stream || !body.StreamOptions.IncludeUsage || len(body.Messages) != 1 || body.Messages[0].Role != "user" {
			t.Fatalf("vision chat body = %#v", body)
		}
		content := body.Messages[0].Content
		if len(content) != 2 || content[0].Type != "text" || content[0].Text != "分析主体与构图" || content[1].Type != "image_url" || content[1].ImageURL.URL != testVisionImageDataURL || content[1].ImageURL.Detail != "low" {
			t.Fatalf("vision chat content = %#v", content)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"id\":\"chatcmpl-vision\",\"choices\":[{\"delta\":{\"content\":\"主体居中\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":400,\"completion_tokens\":8,\"prompt_tokens_details\":{\"cached_tokens\":12}}}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	beforeSendCalls := 0
	result, err := runVisionAnalysis(context.Background(), visionTestInput(server.URL, model.ChannelInterfaceChatCompletion), func() error {
		beforeSendCalls++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if beforeSendCalls != 1 || !result.RequestSent || result.Analysis != "主体居中" || result.ProviderRequestID != "chatcmpl-vision" || result.FinishReason != "stop" {
		t.Fatalf("vision chat result = %#v, beforeSendCalls=%d", result, beforeSendCalls)
	}
	if !result.Usage.Available || result.Usage.InputTokens != 400 || result.Usage.CachedTokens != 12 || result.Usage.OutputTokens != 8 {
		t.Fatalf("vision chat usage = %#v", result.Usage)
	}
}

func TestProviderVisionResponsesRequestContract(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" || request.Header.Get("Authorization") != "Bearer frozen-secret" {
			t.Errorf("request contract path=%q authorization=%q", request.URL.Path, request.Header.Get("Authorization"))
		}
		var body struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
			Input  []struct {
				Role    string                           `json:"role"`
				Content []capturedResponsesVisionContent `json:"content"`
			} `json:"input"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "deepseek-v4-flash-vision-exp" || !body.Stream || len(body.Input) != 1 || body.Input[0].Role != "user" {
			t.Fatalf("vision Responses body = %#v", body)
		}
		content := body.Input[0].Content
		if len(content) != 2 || content[0].Type != "input_text" || content[0].Text != "分析主体与构图" || content[1].Type != "input_image" || content[1].ImageURL != testVisionImageDataURL || content[1].Detail != "original" {
			t.Fatalf("vision Responses content = %#v", content)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"主体清晰\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-vision\",\"status\":\"completed\",\"usage\":{\"input_tokens\":401,\"output_tokens\":9,\"input_tokens_details\":{\"cached_tokens\":13}}}}\n\n"))
	}))
	defer server.Close()

	input := visionTestInput(server.URL, model.ChannelInterfaceOpenAIResponse)
	input.ImageDetail = agentruntime.VisionDetailOriginal
	result, err := runVisionAnalysis(context.Background(), input, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if !result.RequestSent || result.Analysis != "主体清晰" || result.ProviderRequestID != "resp-vision" || result.FinishReason != "completed" || result.Usage.InputTokens != 401 || result.Usage.CachedTokens != 13 || result.Usage.OutputTokens != 9 {
		t.Fatalf("vision Responses result = %#v", result)
	}
}

func TestProviderVisionManagedChatUsesApiKeyAuthentication(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("ApiKey") != "frozen-secret" || request.Header.Get("Authorization") != "" {
			t.Errorf("managed authentication ApiKey=%q Authorization=%q", request.Header.Get("ApiKey"), request.Header.Get("Authorization"))
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"id\":\"managed-vision\",\"choices\":[{\"delta\":{\"content\":\"分析完成\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()
	input := visionTestInput(server.URL, model.ChannelInterfaceChatCompletion)
	input.Config.ManagedProviderRuntime = true
	result, err := runVisionAnalysis(context.Background(), input, func() error { return nil })
	if err != nil || !result.RequestSent || result.ProviderRequestID != "managed-vision" {
		t.Fatalf("managed vision result=%#v error=%v", result, err)
	}
}

func TestProviderVisionRejectsInvalidRequestsBeforeDispatch(t *testing.T) {
	var networkCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		networkCalls.Add(1)
	}))
	defer server.Close()
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")

	tests := []struct {
		name   string
		mutate func(*canvasGenerationInput)
	}{
		{name: "unsupported interface", mutate: func(input *canvasGenerationInput) {
			input.Config.InterfaceType = string(model.ChannelInterfaceOpenAIImage)
		}},
		{name: "invalid detail", mutate: func(input *canvasGenerationInput) { input.ImageDetail = agentruntime.VisionDetail("high") }},
		{name: "missing image", mutate: func(input *canvasGenerationInput) { input.ReferenceImages = nil }},
		{name: "missing prompt", mutate: func(input *canvasGenerationInput) { input.Prompt = "" }},
		{name: "missing model", mutate: func(input *canvasGenerationInput) { input.Config.Model = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := visionTestInput(server.URL, model.ChannelInterfaceChatCompletion)
			test.mutate(&input)
			beforeSendCalls := 0
			result, err := runVisionAnalysis(context.Background(), input, func() error {
				beforeSendCalls++
				return nil
			})
			if err == nil || result.RequestSent || beforeSendCalls != 0 {
				t.Fatalf("result=%#v error=%v beforeSendCalls=%d", result, err, beforeSendCalls)
			}
		})
	}
	if networkCalls.Load() != 0 {
		t.Fatalf("network calls = %d", networkCalls.Load())
	}
}

func TestProviderVisionRejectsOversizedBodyAndCallbackFailureBeforeNetwork(t *testing.T) {
	var networkCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		networkCalls.Add(1)
	}))
	defer server.Close()
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")

	input := visionTestInput(server.URL, model.ChannelInterfaceChatCompletion)
	input.ReferenceImages = []providerMedia{
		{DataURL: "data:image/png;base64," + strings.Repeat("a", maxVisionRequestBytes/2)},
		{DataURL: "data:image/png;base64," + strings.Repeat("b", maxVisionRequestBytes/2)},
	}
	beforeSendCalls := 0
	result, err := runVisionAnalysis(context.Background(), input, func() error {
		beforeSendCalls++
		return nil
	})
	if err == nil || result.RequestSent || beforeSendCalls != 0 || networkCalls.Load() != 0 {
		t.Fatalf("oversize result=%#v error=%v beforeSendCalls=%d networkCalls=%d", result, err, beforeSendCalls, networkCalls.Load())
	}

	input = visionTestInput(server.URL, model.ChannelInterfaceChatCompletion)
	marker := errors.New("dispatch boundary unavailable")
	result, err = runVisionAnalysis(context.Background(), input, func() error { return marker })
	if !errors.Is(err, marker) || result.RequestSent || networkCalls.Load() != 0 {
		t.Fatalf("callback result=%#v error=%v networkCalls=%d", result, err, networkCalls.Load())
	}
}

func TestProviderVisionPreservesSentBoundaryAndRejectsIncompleteResults(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	tests := []struct {
		name string
		body string
	}{
		{name: "missing analysis", body: "data: {\"id\":\"req-1\",\"choices\":[{\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n"},
		{name: "missing request id", body: "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n"},
		{name: "missing usage", body: "data: {\"id\":\"req-1\",\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"},
		{name: "invalid usage", body: "data: {\"id\":\"req-1\",\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":1,\"prompt_tokens_details\":{\"cached_tokens\":11}}}\n\ndata: [DONE]\n\n"},
		{name: "truncated", body: "data: {\"id\":\"req-1\",\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			result, err := runVisionAnalysis(context.Background(), visionTestInput(server.URL, model.ChannelInterfaceChatCompletion), func() error { return nil })
			if err == nil || !result.RequestSent {
				t.Fatalf("result=%#v error=%v", result, err)
			}
		})
	}
}

func TestProviderVisionSanitizesHTTPFailure(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	secretBody := "upstream leaked frozen-secret " + testVisionImageDataURL + " 分析主体与构图"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(secretBody))
	}))
	defer server.Close()

	result, err := runVisionAnalysis(context.Background(), visionTestInput(server.URL, model.ChannelInterfaceChatCompletion), func() error { return nil })
	if err == nil || !result.RequestSent {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	for _, secret := range []string{"frozen-secret", testVisionImageDataURL, "分析主体与构图"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("vision error leaked %q: %v", secret, err)
		}
	}
}

func TestProviderVisionMediaUsesBoundedLocalDataAndShortLivedOSSURL(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	imageBytes := encodeVisionTestImage(t, "png", 4, 3)
	local := createLocalVisionTestResource(t, svc, db, model.Resource{
		ID: "vision-provider-local", UserID: "runtime-user", Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png",
	}, imageBytes)
	settingJSON, err := json.Marshal(ossSettingValue{
		Enabled: true, Provider: "aliyun", Endpoint: "https://oss-cn-test.aliyuncs.com", Bucket: "private-bucket",
		AccessKeyID: "access-id", AccessKeySecret: "secret-value",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.SaveSystemSetting(&model.SystemSetting{Key: ossSettingKey, ValueJSON: string(settingJSON)}); err != nil {
		t.Fatal(err)
	}
	remote := model.Resource{
		ID: "vision-provider-oss", UserID: "runtime-user", Kind: "image", Status: model.ResourceStatusReady,
		Provider: "aliyun", Endpoint: "https://oss-cn-test.aliyuncs.com", Bucket: "private-bucket",
		ObjectKey: "users/runtime-user/image/reference.png", MimeType: "image/png", Size: int64(len(imageBytes)),
	}
	resources := []agentVisionResource{
		{Fact: repository.AgentCapabilityResourceFact{ResourceID: local.ID, Name: "本地图", MimeType: local.MimeType, SizeBytes: local.Size}, Resource: local, Width: 4, Height: 3},
		{Fact: repository.AgentCapabilityResourceFact{ResourceID: remote.ID, Name: "对象图", MimeType: remote.MimeType, SizeBytes: remote.Size}, Resource: remote, Width: 4, Height: 3},
	}
	media, err := svc.agentVisionProviderMedia(agentRuntimeServiceScope(), resources)
	if err != nil {
		t.Fatal(err)
	}
	if len(media) != 2 || media[0].ID != local.ID || media[0].Name != "本地图" || media[0].DataURL != dataURL("image/png", imageBytes) || media[0].URL != "" {
		t.Fatalf("local provider media = %#v", media)
	}
	parsed, err := url.Parse(media[1].URL)
	if err != nil {
		t.Fatal(err)
	}
	if media[1].ID != remote.ID || media[1].Name != "对象图" || media[1].DataURL != "" || parsed.Host != "private-bucket.oss-cn-test.aliyuncs.com" || parsed.Query().Get("Signature") == "" || parsed.Query().Get("OSSAccessKeyId") != "access-id" {
		t.Fatalf("OSS provider media = %#v", media[1])
	}
	if strings.Contains(media[1].URL, "secret-value") {
		t.Fatalf("signed URL leaked OSS secret: %q", media[1].URL)
	}
}

const testVisionImageDataURL = "data:image/png;base64,iVBORw0KGgo="

func visionTestInput(baseURL string, interfaceType model.ChannelInterfaceType) canvasGenerationInput {
	return canvasGenerationInput{
		Mode:            "text",
		Prompt:          "分析主体与构图",
		ImageDetail:     agentruntime.VisionDetailLow,
		ReferenceImages: []providerMedia{{ID: "resource-1", MimeType: "image/png", DataURL: testVisionImageDataURL}},
		Config: providerConfig{
			BaseURL:         baseURL,
			APIKey:          "frozen-secret",
			Model:           "deepseek-v4-flash-vision-exp",
			InterfaceType:   string(interfaceType),
			MaxOutputTokens: 256,
		},
	}
}
