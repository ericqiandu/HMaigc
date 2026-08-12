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

func TestProcessTaskUsesFrozenKuaiziRuntimeAndResumesWithoutSecondCreate(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	createCalls := 0
	statusCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case kuaiziSeedance25CreatePath:
			createCalls++
			if request.Header.Get("ApiKey") != "frozen-key" {
				t.Errorf("ApiKey = %q", request.Header.Get("ApiKey"))
			}
			_, _ = response.Write([]byte(`{"code":0,"data":{"task_id":"upstream-task"},"trace_id":"create-trace"}`))
		case kuaiziSeedance25StatusPath:
			statusCalls++
			_, _ = response.Write([]byte(`{"code":0,"data":{"task_id":"upstream-task","status":"succeeded","video_url":"` + serverURL(request) + `/result.mp4","duration":5,"usage":{"total_tokens":100}}}`))
		case "/result.mp4":
			response.Header().Set("Content-Type", "video/mp4")
			_, _ = response.Write([]byte("video-bytes"))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	svc, repo := openProviderSecretSQLite(t, t.TempDir())
	seedFrozenKuaiziTaskRuntime(t, svc, repo, server.URL, "frozen-key")
	inputJSON, err := json.Marshal(canvasGenerationInput{
		Mode: "video", Prompt: "生成视频",
		Config: providerConfig{Model: "kuaizi-seedance-2.5", VQuality: "720p", Size: "16:9", VideoSeconds: "5"},
	})
	if err != nil {
		t.Fatal(err)
	}
	task := model.Task{
		ID: "task", UserID: "user", Type: "canvas_video", Model: "kuaizi-seedance-2.5", Status: model.TaskStatusRunning, InputJSON: string(inputJSON),
		ProviderAccountID: "account", ProviderEndpointVersionID: "endpoint-v1", ProviderCredentialVersionID: "key-v1",
	}
	if err := repo.Create(&task); err != nil {
		t.Fatal(err)
	}

	result, _, err := svc.processTask(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	video, _ := result["video"].(map[string]interface{})
	if video["taskId"] != "upstream-task" || !strings.HasPrefix(video["dataUrl"].(string), "data:video/mp4;base64,") {
		t.Fatalf("video = %#v", video)
	}
	stored, err := repo.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ProviderRequestID != "upstream-task" {
		t.Fatalf("ProviderRequestID = %q", stored.ProviderRequestID)
	}

	if _, _, err := svc.processTask(context.Background(), *stored); err != nil {
		t.Fatal(err)
	}
	if createCalls != 1 || statusCalls != 2 {
		t.Fatalf("calls = create:%d status:%d", createCalls, statusCalls)
	}
	logs, err := svc.APICallLogs(&model.User{ID: "admin", Role: model.UserRoleAdmin}, 10)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, log := range logs {
		kinds[log.RequestKind]++
	}
	if len(logs) != 5 || kinds["create"] != 1 || kinds["poll"] != 2 || kinds["download"] != 2 {
		t.Fatalf("api call logs = %#v", logs)
	}
}

func TestProcessTaskDoesNotRouteOtherFrozenProviderModelsToSeedance25(t *testing.T) {
	svc, _ := openProviderSecretSQLite(t, t.TempDir())
	inputJSON, err := json.Marshal(canvasGenerationInput{Mode: "video", Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.processTask(context.Background(), model.Task{
		ID: "task-other", UserID: "user", Type: "canvas_video", Model: "future-model", InputJSON: string(inputJSON),
		ProviderCredentialVersionID: "future-key",
	})
	if err == nil || strings.Contains(err.Error(), "筷子 Seedance 2.5") {
		t.Fatalf("other model routed to Seedance 2.5: %v", err)
	}
}

func TestKuaiziSeedance25CreateFailureRequiresBillingReview(t *testing.T) {
	svc, repo := openProviderSecretSQLite(t, t.TempDir())
	order := model.BillingOrder{ID: "order", UserID: "user", TaskID: "task", Status: model.BillingStatusRunning}
	if err := repo.Create(&order); err != nil {
		t.Fatal(err)
	}
	if svc.BillingFailureRequiresReview(order.ID, "task", &KuaiziSeedance25CreateError{err: errors.New("请求失败")}) != true {
		t.Fatal("ambiguous create failure was treated as refundable")
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

func serverURL(request *http.Request) string {
	return "http://" + request.Host
}
