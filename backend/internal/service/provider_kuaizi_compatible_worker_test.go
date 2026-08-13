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
			task := model.Task{ID: "task", UserID: "user", Type: "canvas_video", Model: modelKey, Status: model.TaskStatusRunning, LeaseOwner: "worker-1", LeaseExpiresAt: ptr(time.Now().Add(time.Minute)), InputJSON: string(inputJSON), ProviderAccountID: "account", ProviderEndpointVersionID: "endpoint-v1", ProviderCredentialVersionID: "key-v1", WatermarkCapability: model.WatermarkCapabilityControlled, WatermarkDirective: model.WatermarkDirectiveWithWatermark, WatermarkParameterApplied: true, WatermarkParameterValue: boolPointer(true)}
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
	task := model.Task{ID: "crashed-task", UserID: "user", Type: "canvas_video", Model: "doubao-seedance-2-5-260628", Status: model.TaskStatusRunning, LeaseOwner: "worker-2", LeaseExpiresAt: ptr(time.Now().Add(time.Minute)), InputJSON: string(inputJSON), ProviderAccountID: "account", ProviderEndpointVersionID: "endpoint-v1", ProviderCredentialVersionID: "key-v1", WatermarkCapability: model.WatermarkCapabilityControlled, WatermarkDirective: model.WatermarkDirectiveWithWatermark, WatermarkParameterApplied: true, WatermarkParameterValue: boolPointer(true)}
	if err := repo.Create(&task); err != nil {
		t.Fatal(err)
	}
	if err := repo.BeginProviderCreate(task.ID, task.LeaseOwner); err != nil {
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
