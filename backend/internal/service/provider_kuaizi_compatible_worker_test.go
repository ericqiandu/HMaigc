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
)

const providerWorkerLeaseToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestProviderAssetSlotMapsMixedReferencesToExactSeedanceFields(t *testing.T) {
	manifest, err := agentruntime.NewReferenceManifest([]agentruntime.ReferenceManifestEntry{
		providerReferenceManifestEntry("clip-b", agentruntime.ReferenceMediaVideo, "video-b", 1),
		providerReferenceManifestEntry("portrait-b", agentruntime.ReferenceMediaImage, "image-b", 2),
		providerReferenceManifestEntry("portrait-a", agentruntime.ReferenceMediaImage, "image-a", 3),
		providerReferenceManifestEntry("voice-a", agentruntime.ReferenceMediaAudio, "audio-a", 4),
	})
	if err != nil {
		t.Fatal(err)
	}
	input := canvasGenerationInput{
		Prompt: "镜头推进",
		ReferenceImages: []providerMedia{
			{Name: "portrait-a", ID: "image-a", Type: "image", URL: "/api/resources/image-a/file"},
			{Name: "portrait-b", ID: "image-b", Type: "image", URL: "/api/resources/image-b/file"},
		},
		ReferenceVideos: []providerMedia{{Name: "clip-b", ID: "video-b", Type: "video", URL: "/api/resources/video-b/file"}},
		ReferenceAudios: []providerMedia{{Name: "voice-a", ID: "audio-a", Type: "audio", URL: "/api/resources/audio-a/file"}},
		Metadata:        map[string]interface{}{referenceManifestMetadataKey: manifest},
	}

	slots, present, err := prepareProviderAssetSlots(&input, "seedance")
	if err != nil {
		t.Fatal(err)
	}
	if !present {
		t.Fatal("reference manifest was not detected")
	}
	if input.ReferenceImages[0].Name != "portrait-b" || input.ReferenceImages[1].Name != "portrait-a" {
		t.Fatalf("provider image order = %#v", input.ReferenceImages)
	}
	wantFields := map[string]string{
		"portrait-b": "content[1].image_url",
		"portrait-a": "content[2].image_url",
		"clip-b":     "content[3].video_url",
		"voice-a":    "content[4].audio_url",
	}
	wantSlots := map[string]string{"portrait-b": "image1", "portrait-a": "image2", "clip-b": "video1", "voice-a": "audio1"}
	for _, slot := range slots {
		if slot.RequestField != wantFields[slot.AssetKey] || slot.ProviderSlot != wantSlots[slot.AssetKey] {
			t.Fatalf("slot for %s = %#v", slot.AssetKey, slot)
		}
	}
}

func TestProviderAssetSlotMapsImageFamiliesToExactRequestFields(t *testing.T) {
	for _, test := range []struct {
		family string
		field  string
	}{
		{family: "seedream", field: "image[0]"},
		{family: "gpt-image2", field: "images[0]"},
		{family: "kling", field: "images[0].url"},
	} {
		t.Run(test.family, func(t *testing.T) {
			manifest, err := agentruntime.NewReferenceManifest([]agentruntime.ReferenceManifestEntry{
				providerReferenceManifestEntry("portrait", agentruntime.ReferenceMediaImage, "image-resource", 1),
			})
			if err != nil {
				t.Fatal(err)
			}
			input := canvasGenerationInput{
				Prompt:          "生成角色图",
				ReferenceImages: []providerMedia{{Name: "portrait", ID: "image-resource", Type: "image", URL: "/api/resources/image-resource/file"}},
				Metadata:        map[string]interface{}{referenceManifestMetadataKey: manifest},
			}
			slots, present, err := prepareProviderAssetSlots(&input, test.family)
			if err != nil {
				t.Fatal(err)
			}
			if !present || len(slots) != 1 || slots[0].RequestField != test.field || slots[0].ProviderSlot != "image1" {
				t.Fatalf("provider slots = %#v", slots)
			}
		})
	}
}

func TestProviderAssetSlotTraceSerializationOmitsURLsAndPrivatePayloads(t *testing.T) {
	slots := []providerAssetSlot{{
		AssetKey: "portrait", MediaType: agentruntime.ReferenceMediaImage,
		ArtifactID: "artifact", RevisionID: "revision", ResourceID: "resource",
		RequestField: "images[0]", ProviderSlot: "image1", Ordinal: 1,
	}}
	trace := providerAssetTrace("gpt-image2", "provider-task", slots)
	raw, err := json.Marshal(trace)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(raw)
	if !strings.Contains(serialized, `"providerTaskId":"provider-task"`) || !strings.Contains(serialized, `"assetKey":"portrait"`) {
		t.Fatalf("trace = %s", serialized)
	}
	for _, forbidden := range []string{"X-Amz-Signature", "https://media.example", "private prompt"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("trace leaked %q: %s", forbidden, serialized)
		}
	}
}

func TestProviderAssetSlotRejectsManifestMediaMismatch(t *testing.T) {
	manifest, err := agentruntime.NewReferenceManifest([]agentruntime.ReferenceManifestEntry{
		providerReferenceManifestEntry("portrait", agentruntime.ReferenceMediaImage, "image-resource", 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	input := canvasGenerationInput{
		ReferenceImages: []providerMedia{{Name: "different-key", ID: "image-resource", Type: "image", URL: "https://media.example/image.png"}},
		Metadata:        map[string]interface{}{referenceManifestMetadataKey: manifest},
	}
	if _, _, err := prepareProviderAssetSlots(&input, "gpt-image2"); err == nil {
		t.Fatal("prepareProviderAssetSlots() accepted mismatched asset key")
	}
}

func TestProviderAssetSlotRejectsManifestResourceURLMismatch(t *testing.T) {
	manifest, err := agentruntime.NewReferenceManifest([]agentruntime.ReferenceManifestEntry{
		providerReferenceManifestEntry("portrait", agentruntime.ReferenceMediaImage, "image-resource", 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	input := canvasGenerationInput{
		ReferenceImages: []providerMedia{{Name: "portrait", ID: "image-resource", Type: "image", URL: "https://media.example/wrong-image.png"}},
		Metadata:        map[string]interface{}{referenceManifestMetadataKey: manifest},
	}
	if _, _, err := prepareProviderAssetSlots(&input, "gpt-image2"); err == nil {
		t.Fatal("prepareProviderAssetSlots() accepted mismatched resource URL")
	}
}

func TestProviderAssetSlotRejectsCustomerTaskManifest(t *testing.T) {
	manifest, err := agentruntime.NewReferenceManifest([]agentruntime.ReferenceManifestEntry{
		providerReferenceManifestEntry("portrait", agentruntime.ReferenceMediaImage, "image-resource", 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	inputJSON, err := json.Marshal(canvasGenerationInput{
		ReferenceImages: []providerMedia{{Name: "portrait", ID: "image-resource", Type: "image", URL: "/api/resources/image-resource/file"}},
		Metadata:        map[string]interface{}{referenceManifestMetadataKey: manifest},
	})
	if err != nil {
		t.Fatal(err)
	}
	task := model.Task{
		Audience:            model.TaskAudienceCustomer,
		Model:               "kz_gpt_image2",
		InputJSON:           string(inputJSON),
		WatermarkCapability: model.WatermarkCapabilityUnsupported,
		WatermarkDirective:  model.WatermarkDirectiveProviderDefault,
	}
	if _, err := (&Service{}).processKuaiziCompatibleTask(context.Background(), task); err == nil || !strings.Contains(err.Error(), "只允许用于内部") {
		t.Fatalf("processKuaiziCompatibleTask() error = %v, want internal-only rejection", err)
	}
}

func TestProviderAssetSlotTraceIsAttachedToInternalTaskResult(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	const modelKey = "doubao-seedance-2-0-fast-260128"
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case aiOpenPlatformVolcengineCreatePath:
			var body struct {
				Content []map[string]interface{} `json:"content"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.Content) != 2 || body.Content[1]["type"] != "image_url" {
				t.Errorf("provider content = %#v", body.Content)
			}
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"id":"kz-cgt-provider-trace-task","status":"queued"}`))
		case aiOpenPlatformVolcenginePollPath + "kz-cgt-provider-trace-task":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"id":"kz-cgt-provider-trace-task","status":"succeeded","content":{"video_url":"` + server.URL + `/result.mp4"}}`))
		case "/result.mp4":
			response.Header().Set("Content-Type", "video/mp4")
			_, _ = response.Write([]byte("video-bytes"))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	manifest, err := agentruntime.NewReferenceManifest([]agentruntime.ReferenceManifestEntry{
		{
			AssetKey: "portrait", MediaType: agentruntime.ReferenceMediaImage, SemanticRole: "character",
			ArtifactID: "artifact-portrait", RevisionID: "revision-portrait", ResourceID: "resource-portrait",
			ResourceURL: server.URL + "/private-reference.png?X-Amz-Signature=secret", SourceRevision: "revision-portrait", Ordinal: 1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	inputJSON, err := json.Marshal(canvasGenerationInput{
		Mode: "video", Prompt: "private prompt", Config: providerConfig{Model: modelKey, VQuality: "720p", Size: "16:9", VideoSeconds: "5"},
		ReferenceImages: []providerMedia{{
			Name: "portrait", ID: "resource-portrait", Type: "image",
			URL: server.URL + "/private-reference.png?X-Amz-Signature=secret",
		}},
		Metadata: map[string]interface{}{referenceManifestMetadataKey: manifest},
	})
	if err != nil {
		t.Fatal(err)
	}
	svc, repo := openProviderSecretSQLite(t, t.TempDir())
	seedFrozenKuaiziTaskRuntime(t, svc, repo, server.URL, "frozen-key")
	task := model.Task{
		ID: "provider-trace-internal-task", UserID: "user", Audience: model.TaskAudienceInternal,
		Type: "canvas_video", Model: modelKey, Status: model.TaskStatusRunning,
		LeaseOwner: "worker-trace", LeaseExpiresAt: ptr(time.Now().Add(time.Minute)), InputJSON: string(inputJSON),
		ProviderRequestID: "kz-cgt-provider-trace-task",
		ProviderAccountID: "account", ProviderEndpointVersionID: "endpoint-v1", ProviderCredentialVersionID: "key-v1",
		WatermarkCapability: model.WatermarkCapabilityControlled, WatermarkDirective: model.WatermarkDirectiveWithWatermark,
		WatermarkParameterApplied: true, WatermarkParameterValue: boolPointer(true),
	}
	if err := repo.Create(&task); err != nil {
		t.Fatal(err)
	}
	result, _, err := svc.processTask(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	trace, ok := result["providerAssetTrace"].(providerAssetTraceRecord)
	if !ok || trace.ProviderTaskID != "kz-cgt-provider-trace-task" || len(trace.Assets) != 1 || trace.Assets[0].AssetKey != "portrait" {
		t.Fatalf("provider asset trace = %#v", result["providerAssetTrace"])
	}
	rawResult, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"X-Amz-Signature", "private-reference.png", "private prompt"} {
		if strings.Contains(string(rawResult), forbidden) {
			t.Fatalf("provider result trace leaked %q: %s", forbidden, rawResult)
		}
	}
}

func providerReferenceManifestEntry(assetKey string, mediaType agentruntime.ReferenceMediaType, resourceID string, ordinal int) agentruntime.ReferenceManifestEntry {
	return agentruntime.ReferenceManifestEntry{
		AssetKey: assetKey, MediaType: mediaType, SemanticRole: "reference",
		ArtifactID: "artifact-" + assetKey, RevisionID: "revision-" + assetKey,
		ResourceID: resourceID, ResourceURL: "/api/resources/" + resourceID + "/file",
		SourceRevision: "revision-" + assetKey, Ordinal: ordinal,
	}
}

func TestProcessTaskUsesFrozenKuaiziCompatibleRuntimeForSeedance20And25(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	for _, modelKey := range []string{"doubao-seedance-2-0-fast-260128", "doubao-seedance-2-5-260628"} {
		t.Run(modelKey, func(t *testing.T) {
			createCalls := 0
			statusCalls := 0
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				switch {
				case request.URL.Path == aiOpenPlatformVolcengineCreatePath:
					createCalls++
					if request.Header.Get("Authorization") != "Bearer frozen-key" {
						t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
					}
					var body map[string]interface{}
					if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					if body["model"] != modelKey {
						t.Errorf("model = %#v", body["model"])
					}
					response.Header().Set("Content-Type", "application/json")
					_, _ = response.Write([]byte(`{"id":"kz-cgt-task","status":"queued"}`))
				case request.Method == http.MethodGet && request.URL.Path == aiOpenPlatformVolcenginePollPath+"kz-cgt-task":
					statusCalls++
					response.Header().Set("Content-Type", "application/json")
					_, _ = response.Write([]byte(`{"id":"kz-cgt-task","status":"succeeded","content":{"kz_video_url":"` + server.URL + `/result.mp4"},"duration":5,"usage":{"total_tokens":100}}`))
				case request.URL.Path == "/result.mp4":
					response.Header().Set("Content-Type", "video/mp4")
					_, _ = response.Write([]byte("video-bytes"))
				default:
					http.NotFound(response, request)
				}
			}))
			defer server.Close()

			svc, repo := openProviderSecretSQLite(t, t.TempDir())
			seedFrozenKuaiziTaskRuntime(t, svc, repo, server.URL, "frozen-key")
			inputJSON, err := json.Marshal(canvasGenerationInput{Mode: "video", Prompt: "生成视频", Config: providerConfig{Model: modelKey, VQuality: "720p", Size: "16:9", VideoSeconds: "5"}})
			if err != nil {
				t.Fatal(err)
			}
			task := model.Task{ID: "task", UserID: "user", Type: "canvas_video", Model: modelKey, Status: model.TaskStatusRunning, LeaseOwner: "worker-1", LeaseExpiresAt: ptr(time.Now().Add(time.Minute)), LeaseGeneration: 1, LeaseToken: providerWorkerLeaseToken, InputJSON: string(inputJSON), ProviderAccountID: "account", ProviderEndpointVersionID: "endpoint-v1", ProviderCredentialVersionID: "key-v1", WatermarkCapability: model.WatermarkCapabilityControlled, WatermarkDirective: model.WatermarkDirectiveWithWatermark, WatermarkParameterApplied: true, WatermarkParameterValue: boolPointer(true)}
			if err := repo.Create(&task); err != nil {
				t.Fatal(err)
			}
			result, _, err := svc.processTask(context.Background(), task)
			if err != nil {
				t.Fatal(err)
			}
			video, _ := result["video"].(map[string]interface{})
			if video["taskId"] != "kz-cgt-task" || !strings.HasPrefix(video["dataUrl"].(string), "data:video/mp4;base64,") {
				t.Fatalf("video = %#v", video)
			}
			stored, err := repo.Task(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.ProviderRequestID != "kz-cgt-task" {
				t.Fatalf("ProviderRequestID = %q", stored.ProviderRequestID)
			}
			if _, _, err := svc.processTask(context.Background(), *stored); err != nil {
				t.Fatal(err)
			}
			if createCalls != 1 || statusCalls != 2 {
				t.Fatalf("calls = create:%d status:%d", createCalls, statusCalls)
			}
		})
	}
}

func TestProcessKuaiziCompatibleTaskDispatchesSeedreamFamily(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	createCalls := 0
	statusCalls := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case kuaiziSeedreamCreatePath:
			createCalls++
			if request.Header.Get("ApiKey") != "frozen-key" {
				t.Errorf("ApiKey = %q", request.Header.Get("ApiKey"))
			}
			_, _ = response.Write([]byte(`{"code":0,"message":"","data":{"task_id":"kz-cgt-seedream-dispatch"},"trace_id":"trace-create"}`))
		case kuaiziSeedreamStatusPath:
			statusCalls++
			_, _ = response.Write([]byte(`{"code":0,"message":"","data":{"task_id":"kz-cgt-seedream-dispatch","status":"succeeded","image_urls":["` + server.URL + `/result.jpg"]},"trace_id":"trace-status"}`))
		case "/result.jpg":
			response.Header().Set("Content-Type", "image/jpeg")
			_, _ = response.Write([]byte("image-bytes"))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	svc, repo := openProviderSecretSQLite(t, t.TempDir())
	seedFrozenKuaiziTaskRuntime(t, svc, repo, server.URL, "frozen-key")
	inputJSON, err := json.Marshal(canvasGenerationInput{Mode: "image", Prompt: "生成橙子广告", Config: providerConfig{Model: kuaiziSeedreamLiteModel, Size: "2048x2048", Count: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	task := model.Task{ID: "seedream-task", UserID: "user", Type: "canvas_image", Model: kuaiziSeedreamLiteModel, Status: model.TaskStatusRunning, LeaseOwner: "worker-seedream", LeaseExpiresAt: ptr(time.Now().Add(time.Minute)), LeaseGeneration: 1, LeaseToken: providerWorkerLeaseToken, InputJSON: string(inputJSON), ProviderAccountID: "account", ProviderEndpointVersionID: "endpoint-v1", ProviderCredentialVersionID: "key-v1", WatermarkCapability: model.WatermarkCapabilityControlled, WatermarkDirective: model.WatermarkDirectiveWithWatermark, WatermarkParameterApplied: true, WatermarkParameterValue: boolPointer(true)}
	if err := repo.Create(&task); err != nil {
		t.Fatal(err)
	}
	result, _, err := svc.processTask(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	images, ok := result["images"].([]map[string]string)
	if !ok || len(images) != 1 || images[0]["taskId"] != "kz-cgt-seedream-dispatch" || images[0]["traceId"] != "trace-status" {
		t.Fatalf("images = %#v", result["images"])
	}
	stored, err := repo.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ProviderRequestID != "kz-cgt-seedream-dispatch" {
		t.Fatalf("ProviderRequestID = %q", stored.ProviderRequestID)
	}
	if createCalls != 1 || statusCalls != 1 {
		t.Fatalf("calls = create:%d status:%d", createCalls, statusCalls)
	}
}

func TestKuaiziAsyncCreateFencePreventsSecondPostAfterWorkerCrash(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	createCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == aiOpenPlatformVolcengineCreatePath {
			createCalls++
		}
		http.Error(response, "unexpected upstream call", http.StatusInternalServerError)
	}))
	defer server.Close()

	svc, repo := openProviderSecretSQLite(t, t.TempDir())
	seedFrozenKuaiziTaskRuntime(t, svc, repo, server.URL, "frozen-key")
	inputJSON, err := json.Marshal(canvasGenerationInput{Mode: "video", Prompt: "生成视频", Config: providerConfig{Model: "doubao-seedance-2-5-260628", VQuality: "720p", Size: "16:9", VideoSeconds: "5"}})
	if err != nil {
		t.Fatal(err)
	}
	task := model.Task{ID: "crashed-task", UserID: "user", Type: "canvas_video", Model: "doubao-seedance-2-5-260628", Status: model.TaskStatusRunning, LeaseOwner: "worker-2", LeaseExpiresAt: ptr(time.Now().Add(time.Minute)), LeaseGeneration: 1, LeaseToken: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", InputJSON: string(inputJSON), ProviderAccountID: "account", ProviderEndpointVersionID: "endpoint-v1", ProviderCredentialVersionID: "key-v1", WatermarkCapability: model.WatermarkCapabilityControlled, WatermarkDirective: model.WatermarkDirectiveWithWatermark, WatermarkParameterApplied: true, WatermarkParameterValue: boolPointer(true)}
	if err := repo.Create(&task); err != nil {
		t.Fatal(err)
	}
	if err := repo.BeginProviderCreate(task.ID, task.LeaseOwner, task.LeaseGeneration, task.LeaseToken); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.processTask(context.Background(), *stored)
	var uncertain *KuaiziCompatibleCreateError
	if !errors.As(err, &uncertain) {
		t.Fatalf("processTask() error = %T %v, want KuaiziCompatibleCreateError", err, err)
	}
	if createCalls != 0 {
		t.Fatalf("upstream create calls = %d, want 0", createCalls)
	}
}

func TestKuaiziCompatibleCreateFailureRequiresBillingReview(t *testing.T) {
	svc, repo := openProviderSecretSQLite(t, t.TempDir())
	order := model.BillingOrder{ID: "order", UserID: "user", TaskID: "task", Status: model.BillingStatusRunning}
	if err := repo.Create(&order); err != nil {
		t.Fatal(err)
	}
	if !svc.BillingFailureRequiresReview(order.ID, "task", &KuaiziCompatibleCreateError{err: context.DeadlineExceeded}) {
		t.Fatal("ambiguous compatible create failure was treated as refundable")
	}
}

func seedFrozenKuaiziTaskRuntime(t *testing.T, svc *Service, repo interface{ Create(any) error }, baseURL string, key string) {
	t.Helper()
	now := time.Now().UTC()
	ciphertext, err := svc.EncryptProviderSecret("account", "credential", 1, key)
	if err != nil {
		t.Fatal(err)
	}
	rows := []any{
		&model.ProviderAccount{ID: "account", ProviderKind: "kuaizi", Name: "筷子科技", Enabled: true, CreatedAt: now, UpdatedAt: now},
		&model.ProviderEndpointVersion{ID: "endpoint-v1", ProviderAccountID: "account", BaseURL: baseURL, Status: "retired", Version: 1, CreatedAt: now},
		&model.ProviderCredential{ID: "credential", ProviderAccountID: "account", Family: "seedance", Enabled: true, HealthStatus: "healthy", CreatedAt: now, UpdatedAt: now},
		&model.ProviderCredentialVersion{ID: "key-v1", ProviderCredentialID: "credential", KeyCipher: ciphertext, KeyFingerprint: "fingerprint", Status: "retired", Version: 1, CreatedAt: now},
	}
	for _, row := range rows {
		if err := repo.Create(row); err != nil {
			t.Fatal(err)
		}
	}
}
