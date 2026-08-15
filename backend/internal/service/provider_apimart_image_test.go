package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"infinite-canvas/backend/internal/model"
)

func TestRunAPIMartImageTaskSubmitsAndPolls(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	var received apimartImageRequest
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("Authorization = %q", request.Header.Get("Authorization"))
		}
		switch request.URL.Path {
		case "/v1/images/generations":
			if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"code":200,"data":[{"status":"submitted","task_id":"image-task-1"}]}`))
		case "/v1/tasks/image-task-1":
			if request.URL.Query().Get("language") != "zh" {
				t.Fatalf("language = %q", request.URL.Query().Get("language"))
			}
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"code":200,"data":{"status":"completed","result":{"images":[{"url":["https://cdn.example.com/result.png"]}]}}}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	result, err := runAPIMartImageTask(context.Background(), canvasGenerationInput{
		Prompt: "一只猫",
		Config: providerConfig{
			BaseURL:       server.URL + "/v1",
			APIKey:        "test-key",
			Model:         "gemini-3.1-flash-image-preview",
			InterfaceType: "apimart-image",
			Size:          "1920x1080",
			Quality:       "high",
		},
		ReferenceImages: []providerMedia{{ID: "reference-1", DataURL: testReferenceImageDataURL}},
	})
	if err != nil {
		t.Fatalf("runAPIMartImageTask() error = %v", err)
	}
	if received.Size != "16:9" || received.Resolution != "4K" || received.Count != 1 {
		t.Fatalf("request output settings = %#v", received)
	}
	if len(received.ImageURLs) != 1 || received.ImageURLs[0] != testReferenceImageDataURL {
		t.Fatalf("request image_urls = %#v", received.ImageURLs)
	}
	images, ok := result["images"].([]map[string]string)
	if !ok || len(images) != 1 || images[0]["dataUrl"] != "https://cdn.example.com/result.png" {
		t.Fatalf("result images = %#v", result["images"])
	}
}

func TestRunAPIMartImageTaskReturnsProviderFailure(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodPost {
			_, _ = response.Write([]byte(`{"code":200,"data":[{"status":"submitted","task_id":"failed-task"}]}`))
			return
		}
		_, _ = response.Write([]byte(`{"code":200,"data":{"status":"failed","error":{"message":"内容审核未通过"}}}`))
	}))
	defer server.Close()

	_, err := runAPIMartImageTask(context.Background(), canvasGenerationInput{
		Prompt: "test",
		Config: providerConfig{BaseURL: server.URL + "/v1", APIKey: "test-key", Model: "nano-banana-2-ext"},
	})
	if err == nil || err.Error() != "APIMart 图片任务失败：内容审核未通过" {
		t.Fatalf("error = %v", err)
	}
}

func TestNormalizeAPIMartImageOutputRejectsUnsupportedRatio(t *testing.T) {
	profile, profileErr := apimartImageProfile("gemini-3.1-flash-image-preview")
	if profileErr != nil {
		t.Fatalf("profile error = %v", profileErr)
	}
	_, _, _, err := normalizeAPIMartImageOutput("1000x700", "auto", profile)
	if err == nil {
		t.Fatal("expected unsupported ratio error")
	}
}

func TestAPIMartGPTImageOneRequestUsesQualityAndTransparency(t *testing.T) {
	profile, err := apimartImageProfile("gpt-image-1.5-official")
	if err != nil {
		t.Fatalf("profile error = %v", err)
	}
	request, err := apimartImageRequestFromInput(canvasGenerationInput{
		Prompt: "透明背景产品图",
		Config: providerConfig{
			Model:                 "gpt-image-1.5-official",
			Size:                  "1536x1024",
			Quality:               "high",
			TransparentBackground: "true",
		},
		ReferenceImages: []providerMedia{{ID: "reference-1", URL: "https://cdn.example.com/reference.png"}},
	}, profile)
	if err != nil {
		t.Fatalf("request error = %v", err)
	}
	if request.Size != "3:2" || request.Quality != "high" || request.Resolution != "" {
		t.Fatalf("request output settings = %#v", request)
	}
	if request.Background != "transparent" || request.OutputFormat != "png" {
		t.Fatalf("request transparency settings = %#v", request)
	}
}

func TestAPIMartGPTImageTwoRequestUsesLowercaseResolution(t *testing.T) {
	profile, err := apimartImageProfile("gpt-image-2")
	if err != nil {
		t.Fatalf("profile error = %v", err)
	}
	request, err := apimartImageRequestFromInput(canvasGenerationInput{
		Prompt: "电影海报",
		Config: providerConfig{
			Model:   "gpt-image-2",
			Size:    "3840x2160",
			Quality: "high",
		},
	}, profile)
	if err != nil {
		t.Fatalf("request error = %v", err)
	}
	if request.Size != "16:9" || request.Resolution != "4k" || request.Quality != "" {
		t.Fatalf("request output settings = %#v", request)
	}
}

func TestAPIMartGPT4oRejectsUnsupportedAspectRatio(t *testing.T) {
	profile, err := apimartImageProfile("gpt-4o-image")
	if err != nil {
		t.Fatalf("profile error = %v", err)
	}
	_, err = apimartImageRequestFromInput(canvasGenerationInput{
		Prompt: "test",
		Config: providerConfig{Model: "gpt-4o-image", Size: "16:9"},
	}, profile)
	if err == nil {
		t.Fatal("expected unsupported ratio error")
	}
}

func TestAPIMartImageProfileRejectsUnknownModel(t *testing.T) {
	_, err := apimartImageProfile("unknown-image-model")
	if err == nil {
		t.Fatal("expected unknown model error")
	}
}

func TestEnrichAPICallLogReadsAPIMartTaskID(t *testing.T) {
	log := &model.ApiCallLog{Capability: "image"}
	(&Service{}).EnrichAPICallLog(log, []byte(`{"code":200,"data":[{"status":"submitted","task_id":"image-task-1"}]}`))

	if log.ProviderRequestID != "image-task-1" {
		t.Fatalf("ProviderRequestID = %q", log.ProviderRequestID)
	}
}

func TestEnrichAPICallLogReadsStreamingChatCompletionFacts(t *testing.T) {
	log := &model.ApiCallLog{Status: model.ApiCallStatusSucceeded, Capability: "text"}
	body := []byte("data: {\"id\":\"chatcmpl-billing-1\",\"choices\":[{\"delta\":{\"content\":\"验收\"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl-billing-1\",\"choices\":[],\"usage\":{\"prompt_tokens\":120,\"completion_tokens\":8,\"prompt_tokens_details\":{\"cached_tokens\":20}}}\n\n" +
		"data: [DONE]\n\n")

	(&Service{}).EnrichAPICallLog(log, body)

	if log.ProviderRequestID != "chatcmpl-billing-1" || !log.UsageAvailable || log.InputTokens != 120 || log.CachedTokens != 20 || log.OutputTokens != 8 {
		t.Fatalf("streaming billing facts = %#v", log)
	}
}
