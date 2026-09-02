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

	"infinite-canvas/backend/internal/model"
)

func TestKuaiziGPTImage2TreatsBusinessServerErrorAsCreateUncertain(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":500,"message":"temporary upstream failure"}`))
	}))
	defer server.Close()

	_, err := runKuaiziGPTImage2Task(context.Background(), canvasGenerationInput{
		Mode: "image", Prompt: "生成海报", Config: providerConfig{Model: kuaiziGPTImage2Model, BaseURL: server.URL, APIKey: "image-key", Size: "1024x1024"},
	})
	var uncertain *KuaiziCompatibleCreateError
	if !errors.As(err, &uncertain) {
		t.Fatalf("error = %T %v, want KuaiziCompatibleCreateError", err, err)
	}
}

func TestEnrichAPICallLogRedactsKuaiziGPTImage2FailureMessage(t *testing.T) {
	log := &model.ApiCallLog{Model: kuaiziGPTImage2Model, Status: model.ApiCallStatusFailed}
	(&Service{}).EnrichAPICallLog(log, []byte(`{"code":500,"message":"leaked-key leaked-prompt"}`))
	if log.ErrorCode != "500" || strings.Contains(log.Error, "leaked-key") || strings.Contains(log.Error, "leaked-prompt") {
		t.Fatalf("log = %#v", log)
	}
}

func TestKuaiziGPTImage2SubmitsDocumentedPayloadAndDownloadsResult(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	var createBody map[string]any
	var statusBody map[string]any
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/ai-open-platform-api/") && (request.Header.Get("ApiKey") != "image-key" || request.Header.Get("Authorization") != "") {
			t.Errorf("authentication headers = ApiKey %q Authorization %q", request.Header.Get("ApiKey"), request.Header.Get("Authorization"))
		}
		switch request.URL.Path {
		case "/ai-open-platform-api/v1/chatgpt/image/task/create":
			if err := json.NewDecoder(request.Body).Decode(&createBody); err != nil {
				t.Fatal(err)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"code":0,"message":"","data":{"task_id":"kz-cgt-image-1"}}`))
		case "/ai-open-platform-api/v1/chatgpt/image/task/status":
			if err := json.NewDecoder(request.Body).Decode(&statusBody); err != nil {
				t.Fatal(err)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"code":0,"message":"","data":{"task_id":"kz-cgt-image-1","status":"succeeded","image_url":"` + server.URL + `/result.png"}}`))
		case "/result.png":
			writer.Header().Set("Content-Type", "image/png")
			_, _ = writer.Write([]byte("png-bytes"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	result, err := runKuaiziGPTImage2Task(context.Background(), canvasGenerationInput{
		Prompt:          "生成电影海报",
		Config:          providerConfig{BaseURL: server.URL, APIKey: "image-key", Model: "kz_gpt_image2", Size: "1920x1088", Quality: "high"},
		ReferenceImages: []providerMedia{{URL: "https://assets.example.com/reference.png"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if createBody["model"] != "kz_gpt_image2" || createBody["prompt"] != "生成电影海报" || createBody["size"] != "1920x1088" || createBody["model_version"] != "image2_high" {
		t.Fatalf("create payload = %#v", createBody)
	}
	images, ok := createBody["images"].([]any)
	if !ok || len(images) != 1 || images[0] != "https://assets.example.com/reference.png" {
		t.Fatalf("reference images = %#v", createBody["images"])
	}
	if _, exists := createBody["seed"]; exists {
		t.Fatal("seed must not be sent in the first integration slice")
	}
	if _, exists := createBody["negative_prompt"]; exists {
		t.Fatal("negative_prompt must not be sent in the first integration slice")
	}
	if _, exists := createBody["watermark"]; exists {
		t.Fatalf("unsupported GPT Image 2 request contains watermark: %#v", createBody)
	}
	if _, exists := createBody["aigc_watermark"]; exists {
		t.Fatalf("unsupported GPT Image 2 request contains aigc_watermark: %#v", createBody)
	}
	if statusBody["task_id"] != "kz-cgt-image-1" {
		t.Fatalf("status payload = %#v", statusBody)
	}
	output, ok := result["images"].([]map[string]string)
	if !ok || len(output) != 1 || !strings.HasPrefix(output[0]["dataUrl"], "data:image/png;base64,") {
		t.Fatalf("result = %#v", result)
	}
}

func TestKuaiziGPTImage2RetriesTransientPollServerErrorWithoutCreatingAgain(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	createCalls := 0
	pollCalls := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case kuaiziGPTImage2CreatePath:
			createCalls++
			_, _ = writer.Write([]byte(`{"code":0,"message":"","data":{"task_id":"kz-cgt-transient-poll"}}`))
		case kuaiziGPTImage2StatusPath:
			pollCalls++
			if pollCalls == 1 {
				writer.WriteHeader(http.StatusInternalServerError)
				_, _ = writer.Write([]byte(`{"code":500,"message":"temporary status failure"}`))
				return
			}
			_, _ = writer.Write([]byte(`{"code":0,"message":"","data":{"task_id":"kz-cgt-transient-poll","status":"succeeded","image_url":"` + server.URL + `/result.png"}}`))
		case "/result.png":
			writer.Header().Set("Content-Type", "image/png")
			_, _ = writer.Write([]byte("png"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	result, err := runKuaiziGPTImage2TaskWithPollInterval(context.Background(), canvasGenerationInput{
		Prompt: "生成海报",
		Config: providerConfig{BaseURL: server.URL, APIKey: "image-key", Model: kuaiziGPTImage2Model, Size: "1024x1024"},
	}, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if createCalls != 1 || pollCalls != 2 {
		t.Fatalf("create calls = %d, poll calls = %d", createCalls, pollCalls)
	}
	if images, ok := result["images"].([]map[string]string); !ok || len(images) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestKuaiziGPTImage2DoesNotForwardApiKeyThroughRedirect(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	sinkCalls := 0
	sink := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		sinkCalls++
		if request.Header.Get("ApiKey") != "" {
			t.Error("redirect leaked ApiKey")
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer sink.Close()
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, sink.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	_, err := runKuaiziGPTImage2Task(context.Background(), canvasGenerationInput{
		Prompt: "生成海报",
		Config: providerConfig{BaseURL: source.URL, APIKey: "redirect-secret", Model: "kz_gpt_image2", Size: "1024x1024", Quality: "medium"},
	})
	if err == nil || !strings.Contains(err.Error(), "不允许重定向") {
		t.Fatalf("redirect error = %v", err)
	}
	if sinkCalls != 0 {
		t.Fatalf("redirect sink calls = %d, want 0", sinkCalls)
	}
}

func TestKuaiziGPTImage2ValidatesSizeQualityAndUnsupportedEdits(t *testing.T) {
	tests := []struct {
		name  string
		input canvasGenerationInput
		want  string
	}{
		{name: "dimension multiple", input: canvasGenerationInput{Prompt: "x", Config: providerConfig{Model: "kz_gpt_image2", Size: "1024x1000", Quality: "medium"}}, want: "16 的倍数"},
		{name: "pixel minimum", input: canvasGenerationInput{Prompt: "x", Config: providerConfig{Model: "kz_gpt_image2", Size: "256x256", Quality: "medium"}}, want: "总像素"},
		{name: "longest edge", input: canvasGenerationInput{Prompt: "x", Config: providerConfig{Model: "kz_gpt_image2", Size: "4096x2048", Quality: "medium"}}, want: "最长边"},
		{name: "quality", input: canvasGenerationInput{Prompt: "x", Config: providerConfig{Model: "kz_gpt_image2", Size: "1024x1024", Quality: "ultra"}}, want: "画质"},
		{name: "transparent", input: canvasGenerationInput{Prompt: "x", Config: providerConfig{Model: "kz_gpt_image2", Size: "1024x1024", Quality: "medium", TransparentBackground: "true"}}, want: "透明背景"},
		{name: "mask", input: canvasGenerationInput{Prompt: "x", Config: providerConfig{Model: "kz_gpt_image2", Size: "1024x1024", Quality: "medium"}, Mask: &providerMedia{DataURL: "data:image/png;base64,YQ=="}}, want: "蒙版"},
		{name: "batch", input: canvasGenerationInput{Prompt: "x", Config: providerConfig{Model: "kz_gpt_image2", Size: "1024x1024", Quality: "medium", Count: "2"}}, want: "只支持生成 1 张"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := kuaiziGPTImage2CreatePayload(test.input); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}

	payload, err := kuaiziGPTImage2CreatePayload(canvasGenerationInput{Prompt: "x", Config: providerConfig{Model: "kz_gpt_image2", Size: "16:9", Quality: "auto"}})
	if err != nil {
		t.Fatal(err)
	}
	if payload["aspect_ratio"] != "16:9" || payload["model_version"] != "image2_medium" {
		t.Fatalf("ratio/default quality payload = %#v", payload)
	}
}

func TestProcessTaskUsesFrozenKuaiziGPTImage2Credential(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/ai-open-platform-api/") && request.Header.Get("ApiKey") != "frozen-image-key" {
			t.Fatalf("ApiKey = %q", request.Header.Get("ApiKey"))
		}
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/ai-open-platform-api/v1/chatgpt/image/task/create":
			_, _ = writer.Write([]byte(`{"code":0,"message":"","data":{"task_id":"kz-cgt-frozen-image"}}`))
		case "/ai-open-platform-api/v1/chatgpt/image/task/status":
			_, _ = writer.Write([]byte(`{"code":0,"message":"","data":{"task_id":"kz-cgt-frozen-image","status":"succeeded","image_url":"` + server.URL + `/result.png"}}`))
		case "/result.png":
			writer.Header().Set("Content-Type", "image/png")
			_, _ = writer.Write([]byte("image"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	svc, repo := openProviderSecretSQLite(t, t.TempDir())
	seedFrozenKuaiziImageRuntime(t, svc, repo, server.URL, "frozen-image-key")
	inputJSON, err := json.Marshal(canvasGenerationInput{Mode: "image", Prompt: "生成海报", Config: providerConfig{Model: "kz_gpt_image2", Size: "1024x1024", Quality: "medium"}})
	if err != nil {
		t.Fatal(err)
	}
	task := model.Task{ID: "image-task", UserID: "user", Type: "canvas_image", Model: "kz_gpt_image2", Status: model.TaskStatusRunning, LeaseOwner: "image-worker", LeaseGeneration: 1, LeaseToken: providerWorkerLeaseToken, InputJSON: string(inputJSON), ProviderAccountID: "image-account", ProviderEndpointVersionID: "image-endpoint-v1", ProviderCredentialVersionID: "image-key-v1", WatermarkCapability: model.WatermarkCapabilityUnsupported, WatermarkDirective: model.WatermarkDirectiveProviderDefault}
	if err := repo.Create(&task); err != nil {
		t.Fatal(err)
	}
	result, _, err := svc.processTask(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	images, ok := result["images"].([]map[string]string)
	if !ok || len(images) != 1 {
		t.Fatalf("result = %#v", result)
	}
	stored, err := svc.persistGeneratedMediaResult("user", result)
	if err != nil {
		t.Fatal(err)
	}
	storedImages, ok := stored["images"].([]interface{})
	if !ok || len(storedImages) != 1 {
		t.Fatalf("stored images = %#v", stored)
	}
	storedImage, ok := storedImages[0].(map[string]interface{})
	if !ok || !strings.HasPrefix(stringField(storedImage, "storageKey"), "resource:") || !strings.HasPrefix(stringField(storedImage, "url"), "/api/resources/") {
		t.Fatalf("stored image = %#v", storedImage)
	}
	storedTask, err := repo.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTask.ProviderRequestID != "kz-cgt-frozen-image" {
		t.Fatalf("provider request id = %q", storedTask.ProviderRequestID)
	}
	logs, err := repo.ApiCallLogs("user", false, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 3 {
		t.Fatalf("request logs = %#v", logs)
	}
	for _, log := range logs {
		if log.Billable != (log.RequestKind == "create") {
			t.Fatalf("request %s billable = %v", log.RequestKind, log.Billable)
		}
	}
}

func seedFrozenKuaiziImageRuntime(t *testing.T, svc *Service, repo interface{ Create(any) error }, baseURL string, key string) {
	t.Helper()
	now := time.Now().UTC()
	ciphertext, err := svc.EncryptProviderSecret("image-account", "image-credential", 1, key)
	if err != nil {
		t.Fatal(err)
	}
	rows := []any{
		&model.ProviderAccount{ID: "image-account", ProviderKind: "kuaizi", Name: "筷子科技", Enabled: true, CreatedAt: now, UpdatedAt: now},
		&model.ProviderEndpointVersion{ID: "image-endpoint-v1", ProviderAccountID: "image-account", BaseURL: baseURL, Status: "retired", Version: 1, CreatedAt: now},
		&model.ProviderCredential{ID: "image-credential", ProviderAccountID: "image-account", Family: "gpt-image2", Enabled: true, HealthStatus: "healthy", CreatedAt: now, UpdatedAt: now},
		&model.ProviderCredentialVersion{ID: "image-key-v1", ProviderCredentialID: "image-credential", KeyCipher: ciphertext, KeyFingerprint: "fingerprint", Status: "retired", Version: 1, CreatedAt: now},
	}
	for _, row := range rows {
		if err := repo.Create(row); err != nil {
			t.Fatal(err)
		}
	}
}
