package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/testsupport"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestProviderTaskFactCreationIsAtomicWithTaskAndReservation(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	if err := db.Create(&model.CreditAccount{UserID: "user-atomic", AvailableMicrocredits: 10_000_000}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_provider_fact BEFORE INSERT ON provider_task_facts BEGIN SELECT RAISE(ABORT, 'provider fact insert failed'); END`).Error; err != nil {
		t.Fatal(err)
	}
	task := providerRuntimeTask("task-atomic", "user-atomic", "owner")
	order := providerRuntimeBillingOrder("billing-atomic", task.ID, task.UserID)
	fact := providerRuntimeFact(task.ID, order.ID, "reserved")
	err := svc.repo.CreateTaskWithCreditReservation(&task, &order, &fact, repository.ActiveTaskPolicy{Unlimited: true})
	if err == nil || !strings.Contains(err.Error(), "provider fact insert failed") {
		t.Fatalf("CreateTaskWithCreditReservation() error = %v", err)
	}
	for _, check := range []struct{ table, column, id string }{
		{table: "tasks", column: "id", id: task.ID},
		{table: "billing_orders", column: "id", id: order.ID},
		{table: "provider_task_facts", column: "task_id", id: task.ID},
	} {
		var count int64
		if err := db.Table(check.table).Where(check.column+" = ?", check.id).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s rows = %d, want rollback", check.table, count)
		}
	}
	var account model.CreditAccount
	if err := db.First(&account, "user_id = ?", task.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if account.AvailableMicrocredits != 10_000_000 || account.ReservedMicrocredits != 0 {
		t.Fatalf("credit account after rollback = %#v", account)
	}
}

func TestProviderTaskRuntimeUsesFrozenEndpointAndCredentialVersions(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	runtimeFixture := saveProviderRuntimeFixture(t, svc, db)

	runtime, err := svc.resolveProviderTaskRuntime(runtimeFixture.task.ID)
	if err != nil {
		t.Fatalf("resolveProviderTaskRuntime() error = %v", err)
	}
	if runtime.EndpointVersion.ID != "endpoint-v1" || runtime.EndpointVersion.BaseURL != runtimeFixture.endpointV1 || runtime.CredentialVersion.ID != "credential-v1" {
		t.Fatalf("runtime versions = endpoint:%#v credential:%#v", runtime.EndpointVersion, runtime.CredentialVersion)
	}
	plaintext, err := NewProviderSecretCipher(svc.dataDir).Decrypt(runtime.Account.ID, runtime.Credential.ID, runtime.CredentialVersion.ID, runtime.CredentialVersion.KeyCipher)
	if err != nil || plaintext != "key-v1" {
		t.Fatalf("frozen secret = %q, %v", plaintext, err)
	}
	encoded, err := json.Marshal(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "key-v1") || strings.Contains(string(encoded), "enc:provider:") {
		t.Fatalf("runtime JSON leaked secret: %s", encoded)
	}
}

func TestProviderTaskCommercialFactsFreezeStructuralInputsWithoutTaskSecrets(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	runtimeFixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Create(&model.CreditAccount{UserID: "user-create", AvailableMicrocredits: 10_000_000}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	resources := []model.Resource{
		{ID: "image", UserID: "user-create", Status: model.ResourceStatusReady, Provider: "aliyun", ObjectKey: "image.png", CreatedAt: now, UpdatedAt: now},
		{ID: "video", UserID: "user-create", Status: model.ResourceStatusReady, Provider: "aliyun", ObjectKey: "video.mp4", CreatedAt: now, UpdatedAt: now},
		{ID: "audio", UserID: "user-create", Status: model.ResourceStatusReady, Provider: "aliyun", ObjectKey: "audio.mp3", CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&resources).Error; err != nil {
		t.Fatal(err)
	}
	task := model.Task{ID: "task-create", UserID: "user-create", Type: "canvas_video", Capability: "video", Operation: "video_generate"}
	input := map[string]any{
		"mode": "video",
		"config": map[string]any{
			"channelId": runtimeFixture.channel.ID, "model": runtimeFixture.channelModel.ModelKey,
			"baseUrl": "/api/ai/system/" + runtimeFixture.channel.ID, "apiKey": "system",
			"videoSeconds": "-1", "vquality": "720p", "size": "adaptive",
		},
		"referenceImages": []any{map[string]any{"id": "image", "storageKey": "resource:image", "role": "reference_image"}},
		"referenceVideos": []any{map[string]any{"id": "video", "storageKey": "resource:video", "role": "reference_video"}},
		"referenceAudios": []any{map[string]any{"id": "audio", "storageKey": "resource:audio", "role": "reference_audio"}},
		"metadata":        map[string]any{"nested": map[string]any{"apiKey": "must-not-persist", "baseUrl": "https://must-not-persist.example"}},
	}
	facts, sanitized, err := svc.buildTaskCommercialFacts(task.UserID, &task, input)
	if err != nil {
		t.Fatalf("buildTaskCommercialFacts() error = %v", err)
	}
	if facts.ProviderFact == nil || facts.ProviderFact.ProviderEndpointVersionID != "endpoint-v2" || facts.ProviderFact.ProviderCredentialVersionID != "credential-v2" {
		t.Fatalf("provider fact = %#v", facts.ProviderFact)
	}
	if facts.ProviderFact.RequestedDurationSeconds != -1 || facts.ProviderFact.Resolution != "720p" || facts.ProviderFact.InputVariant != "reference_video" || facts.ProviderFact.InputImageCount != 1 || facts.ProviderFact.InputVideoCount != 1 || facts.ProviderFact.InputAudioCount != 1 {
		t.Fatalf("provider fact structural snapshot = %#v", facts.ProviderFact)
	}
	if facts.BillingOrder.PricingInputVariant != "reference_video" || facts.BillingOrder.UnitPriceMicrocredits != 4 || facts.BillingOrder.Quantity != 30 {
		t.Fatalf("billing input variant snapshot = %#v", facts.BillingOrder)
	}
	encoded, err := json.Marshal(sanitized)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "apiKey") || strings.Contains(string(encoded), "baseUrl") || strings.Contains(string(encoded), "must-not-persist") {
		t.Fatalf("sanitized task input retained provider secrets/endpoint: %s", encoded)
	}
	standardTask := model.Task{ID: "task-create-standard", UserID: task.UserID, Type: task.Type, Capability: task.Capability, Operation: task.Operation}
	standardInput := map[string]any{
		"mode": "video", "prompt": "animate",
		"config": map[string]any{
			"channelId": runtimeFixture.channel.ID, "model": runtimeFixture.channelModel.ModelKey,
			"videoSeconds": "4", "vquality": "720p", "size": "adaptive",
		},
	}
	standardFacts, _, err := svc.buildTaskCommercialFacts(task.UserID, &standardTask, standardInput)
	if err != nil {
		t.Fatal(err)
	}
	if standardFacts.BillingOrder.PricingInputVariant != "standard" || standardFacts.BillingOrder.UnitPriceMicrocredits != 3 {
		t.Fatalf("standard billing tier = %#v", standardFacts.BillingOrder)
	}
}

func TestProviderTaskCreateUncertainPersistsFactsAndBlocksSecondCreate(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	runtimeFixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", runtimeFixture.task.ID).Update("provider_status", "reserved").Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.MarkProviderTaskCreateUncertain(runtimeFixture.task.ID, "response_lost"); err != nil {
		t.Fatalf("MarkProviderTaskCreateUncertain() error = %v", err)
	}
	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	runtime, err := svc.resolveProviderTaskRuntime(runtimeFixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	runtime.EndpointVersion.BaseURL = server.URL
	client := NewKuaiziSeedance25Client(server.Client(), NewProviderSecretCipher(svc.dataDir))
	_, err = svc.executeKuaiziSeedance25Task(context.Background(), runtime, seedance25TestInput(), client, time.Millisecond)
	var uncertain *KuaiziSeedance25CreateUncertainError
	if !errors.As(err, &uncertain) {
		t.Fatalf("executeKuaiziSeedance25Task() error = %T %v", err, err)
	}
	if requests.Load() != 0 {
		t.Fatalf("create requests = %d, want 0", requests.Load())
	}
	var fact model.ProviderTaskFact
	if err := db.First(&fact, "task_id = ?", runtimeFixture.task.ID).Error; err != nil {
		t.Fatal(err)
	}
	var order model.BillingOrder
	if err := db.First(&order, "id = ?", runtimeFixture.order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if fact.ProviderStatus != "create_uncertain" || order.Status != model.BillingStatusUncertain {
		t.Fatalf("uncertain facts = fact:%#v order:%#v", fact, order)
	}
}

func TestProviderTaskRecoveredCreatingStateNeverSendsSecondCreate(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	runtimeFixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", runtimeFixture.task.ID).Update("provider_status", "creating").Error; err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	runtime, err := svc.resolveProviderTaskRuntime(runtimeFixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	runtime.EndpointVersion.BaseURL = server.URL
	client := NewKuaiziSeedance25Client(server.Client(), NewProviderSecretCipher(svc.dataDir))
	_, err = svc.executeKuaiziSeedance25Task(context.Background(), runtime, seedance25TestInput(), client, time.Millisecond)
	var uncertain *KuaiziSeedance25CreateUncertainError
	if !errors.As(err, &uncertain) {
		t.Fatalf("executeKuaiziSeedance25Task() error = %T %v", err, err)
	}
	if requests.Load() != 0 {
		t.Fatalf("recovered create requests = %d, want 0", requests.Load())
	}
}

func TestProviderTaskCreateUncertainCannotUseManualRetryPath(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	runtimeFixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Model(&model.Task{}).Where("id = ?", runtimeFixture.task.ID).Updates(map[string]any{
		"status": model.TaskStatusFailed, "error": "create uncertain",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.MarkProviderTaskCreateUncertain(runtimeFixture.task.ID, "response_lost"); err != nil {
		t.Fatal(err)
	}
	_, err := svc.RetryTask(runtimeFixture.task.UserID, runtimeFixture.task.ID)
	if err == nil || !strings.Contains(err.Error(), "上游创建结果不确定") {
		t.Fatalf("RetryTask() error = %v", err)
	}
}

func TestProviderTaskDefinitiveCreateRejectionReleasesExecutionFact(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	runtimeFixture := saveProviderRuntimeFixture(t, svc, db)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(writer, `{"code":4001,"message":"rejected","data":null,"trace_id":"trace-rejected"}`)
	}))
	defer server.Close()
	runtime, err := svc.resolveProviderTaskRuntime(runtimeFixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	runtime.EndpointVersion.BaseURL = server.URL
	client := NewKuaiziSeedance25Client(server.Client(), NewProviderSecretCipher(svc.dataDir))
	if _, err := svc.executeKuaiziSeedance25Task(context.Background(), runtime, seedance25TestInput(), client, time.Millisecond); err == nil {
		t.Fatal("executeKuaiziSeedance25Task() error = nil")
	}
	var fact model.ProviderTaskFact
	if err := db.First(&fact, "task_id = ?", runtimeFixture.task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if fact.ProviderStatus != "create_failed" || fact.CreateTraceID != "" {
		t.Fatalf("definitive rejection fact = %#v", fact)
	}
}

func TestProviderTaskCreateHTTP5xxPersistsUncertainAndNeverCreatesTwice(t *testing.T) {
	for _, response := range []struct {
		statusCode int
		body       string
	}{{http.StatusInternalServerError, `<html>failed</html>`}, {http.StatusBadGateway, ""}, {524, strings.Repeat("x", 4<<20+1)}} {
		t.Run(http.StatusText(response.statusCode), func(t *testing.T) {
			svc, db := openProviderCredentialService(t)
			runtimeFixture := saveProviderRuntimeFixture(t, svc, db)
			var creates atomic.Int64
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				creates.Add(1)
				writer.WriteHeader(response.statusCode)
				_, _ = io.WriteString(writer, response.body)
			}))
			defer server.Close()
			runtime, err := svc.resolveProviderTaskRuntime(runtimeFixture.task.ID)
			if err != nil {
				t.Fatal(err)
			}
			runtime.EndpointVersion.BaseURL = server.URL
			client := NewKuaiziSeedance25Client(server.Client(), NewProviderSecretCipher(svc.dataDir))
			for attempt := 0; attempt < 2; attempt++ {
				_, err = svc.executeKuaiziSeedance25Task(context.Background(), runtime, seedance25TestInput(), client, time.Millisecond)
				var uncertain *KuaiziSeedance25CreateUncertainError
				if !errors.As(err, &uncertain) {
					t.Fatalf("attempt %d error = %T %v", attempt, err, err)
				}
				runtime, err = svc.resolveProviderTaskRuntime(runtimeFixture.task.ID)
				if err != nil {
					t.Fatal(err)
				}
				runtime.EndpointVersion.BaseURL = server.URL
			}
			var fact model.ProviderTaskFact
			var order model.BillingOrder
			if err := db.First(&fact, "task_id = ?", runtimeFixture.task.ID).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.First(&order, "id = ?", runtimeFixture.order.ID).Error; err != nil {
				t.Fatal(err)
			}
			if creates.Load() != 1 || fact.ProviderStatus != "create_uncertain" || order.Status != model.BillingStatusUncertain {
				t.Fatalf("creates=%d fact=%#v order=%#v", creates.Load(), fact, order)
			}
		})
	}
}

func TestProviderTaskUnknownPollStateBecomesReconciliationUncertain(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	runtimeFixture := saveProviderRuntimeFixture(t, svc, db)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == kuaiziSeedance25CreatePath {
			_, _ = io.WriteString(writer, `{"code":0,"message":"","data":{"task_id":"provider-unknown"},"trace_id":"trace-create"}`)
			return
		}
		_, _ = io.WriteString(writer, `{"code":0,"message":"","data":{"task_id":"provider-unknown","status":"mystery"},"trace_id":"trace-unknown"}`)
	}))
	defer server.Close()
	runtime, err := svc.resolveProviderTaskRuntime(runtimeFixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	runtime.EndpointVersion.BaseURL = server.URL
	client := NewKuaiziSeedance25Client(server.Client(), NewProviderSecretCipher(svc.dataDir))
	if _, err := svc.executeKuaiziSeedance25Task(context.Background(), runtime, seedance25TestInput(), client, time.Millisecond); err == nil {
		t.Fatal("executeKuaiziSeedance25Task() error = nil")
	}
	var fact model.ProviderTaskFact
	if err := db.First(&fact, "task_id = ?", runtimeFixture.task.ID).Error; err != nil {
		t.Fatal(err)
	}
	var order model.BillingOrder
	if err := db.First(&order, "id = ?", runtimeFixture.order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if fact.ProviderStatus != "poll_uncertain" || fact.LastPollTraceID != "trace-unknown" || order.Status != model.BillingStatusUncertain {
		t.Fatalf("unknown poll facts = fact:%#v order:%#v", fact, order)
	}
}

func TestProviderFailureSentinelsNeverReachPersistedOrHTTPFacts(t *testing.T) {
	const secret = "sentinel-runtime-key"
	const prompt = "sentinel-runtime-prompt bob@example.test"
	svc, db := openProviderCredentialService(t)
	runtimeFixture := saveProviderRuntimeFixture(t, svc, db)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == kuaiziSeedance25CreatePath {
			_, _ = io.WriteString(writer, `{"code":0,"message":"","data":{"task_id":"provider-sensitive"},"trace_id":"trace-create"}`)
			return
		}
		_, _ = io.WriteString(writer, `{"code":0,"message":"","data":{"task_id":"provider-sensitive","status":"failed","error":"sentinel-runtime-key sentinel-runtime-prompt bob@example.test"},"trace_id":"trace-poll"}`)
	}))
	defer server.Close()
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	ciphertext, err := NewProviderSecretCipher(svc.dataDir).Encrypt("account", "credential", "credential-v1", secret)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ProviderCredentialVersion{}).Where("id = ?", "credential-v1").Update("key_cipher", ciphertext).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ProviderEndpointVersion{}).Where("id = ?", "endpoint-v1").Update("base_url", server.URL).Error; err != nil {
		t.Fatal(err)
	}
	input := seedance25TestInput()
	input.Prompt = prompt
	encodedInput, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", runtimeFixture.task.ID).Updates(map[string]any{
		"prompt": prompt, "input_json": string(encodedInput), "lease_owner": svc.workerID,
	}).Error; err != nil {
		t.Fatal(err)
	}
	task, err := svc.repo.Task(runtimeFixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.processClaimedTask(task); err == nil {
		t.Fatal("processClaimedTask() error = nil")
	}
	storedTask, err := svc.repo.Task(runtimeFixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	logs, err := svc.repo.TaskLogs(runtimeFixture.task.UserID, runtimeFixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	var apiLogs []model.ApiCallLog
	if err := db.Where("task_id = ?", runtimeFixture.task.ID).Find(&apiLogs).Error; err != nil {
		t.Fatal(err)
	}
	dto, err := json.Marshal(struct {
		Error string `json:"error"`
	}{Error: storedTask.Error})
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := json.Marshal(struct {
		TaskError string
		Logs      []model.TaskLog
		Calls     []model.ApiCallLog
		DTO       json.RawMessage
	}{storedTask.Error, logs, apiLogs, dto})
	if err != nil {
		t.Fatal(err)
	}
	for _, sentinel := range []string{secret, "sentinel-runtime-prompt", "bob@example.test"} {
		if strings.Contains(string(persisted), sentinel) {
			t.Fatalf("persisted/HTTP facts leaked %q: %s", sentinel, persisted)
		}
	}
}

func TestProviderSuccessfulFieldSentinelsNeverReachPersistentOrHTTPFacts(t *testing.T) {
	const secret = "sentinel-success-runtime-key"
	const prompt = "sentinel success runtime prompt bob@example.test"
	for _, sensitiveCreateID := range []bool{true, false} {
		name := "sensitive_success_urls"
		if sensitiveCreateID {
			name = "sensitive_create_id"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("CANVAS_ENVIRONMENT", "development")
			svc, db := openProviderCredentialService(t)
			fixture := saveProviderRuntimeFixture(t, svc, db)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				if request.URL.Path == kuaiziSeedance25CreatePath {
					taskID := "provider-safe-id"
					if sensitiveCreateID {
						taskID = secret
					}
					_, _ = io.WriteString(writer, `{"code":0,"message":"","data":{"task_id":"`+taskID+`"},"trace_id":"trace-create"}`)
					return
				}
				_, _ = io.WriteString(writer, `{"code":0,"message":"","data":{"task_id":"provider-safe-id","status":"succeeded","video_url":"https://cdn.example/result.mp4?token=sentinel-success-runtime-key","last_frame_url":"https://cdn.example/last.png?prompt=sentinel%20success%20runtime%20prompt%20bob%40example.test","duration":8,"total_tokens":"42"},"trace_id":"trace-poll"}`)
			}))
			defer server.Close()
			ciphertext, err := NewProviderSecretCipher(svc.dataDir).Encrypt("account", "credential", "credential-v1", secret)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.ProviderCredentialVersion{}).Where("id = ?", "credential-v1").Update("key_cipher", ciphertext).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.ProviderEndpointVersion{}).Where("id = ?", "endpoint-v1").Update("base_url", server.URL).Error; err != nil {
				t.Fatal(err)
			}
			input := seedance25TestInput()
			input.Prompt = prompt
			encodedInput, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Updates(map[string]any{"prompt": prompt, "input_json": string(encodedInput), "lease_owner": svc.workerID}).Error; err != nil {
				t.Fatal(err)
			}
			task, err := svc.repo.Task(fixture.task.ID)
			if err != nil {
				t.Fatal(err)
			}
			_ = svc.processClaimedTask(task)
			storedTask, err := svc.repo.Task(fixture.task.ID)
			if err != nil {
				t.Fatal(err)
			}
			fact, err := svc.repo.ProviderTaskFact(fixture.task.ID)
			if err != nil {
				t.Fatal(err)
			}
			order, err := svc.repo.BillingOrder(fixture.order.ID)
			if err != nil {
				t.Fatal(err)
			}
			logs, err := svc.repo.TaskLogs(fixture.task.UserID, fixture.task.ID)
			if err != nil {
				t.Fatal(err)
			}
			var apiLogs []model.ApiCallLog
			if err := db.Where("task_id = ?", fixture.task.ID).Find(&apiLogs).Error; err != nil {
				t.Fatal(err)
			}
			surfaces, err := json.Marshal(struct {
				TaskError       string
				TaskResult      string
				ProviderRequest string
				FactTaskID      string
				FactSource      string
				FactLastFrame   string
				OrderRequest    string
				TaskLogs        []model.TaskLog
				APILogs         []model.ApiCallLog
			}{storedTask.Error, storedTask.ResultJSON, storedTask.ProviderRequestID, fact.ProviderTaskID, fact.AssetSourceURL, fact.LastFrameURL, order.ProviderRequestID, logs, apiLogs})
			if err != nil {
				t.Fatal(err)
			}
			for _, sentinel := range []string{secret, "bob@example.test", "sentinel success runtime prompt"} {
				if strings.Contains(string(surfaces), sentinel) {
					t.Fatalf("persistent/HTTP surfaces leaked %q: %s", sentinel, surfaces)
				}
			}
			if storedTask.Status != model.TaskStatusFailed || order.Status != model.BillingStatusUncertain {
				t.Fatalf("unsafe successful fields task=%s billing=%s fact=%s task_error=%q", storedTask.Status, order.Status, fact.ProviderStatus, storedTask.Error)
			}
			if sensitiveCreateID {
				if fact.ProviderStatus != "create_uncertain" {
					t.Fatalf("sensitive create ID fact status = %s", fact.ProviderStatus)
				}
			} else if fact.ProviderStatus != "poll_uncertain" {
				t.Fatalf("sensitive success URL fact status = %s", fact.ProviderStatus)
			}
		})
	}
}

func TestKuaiziModelVisibilityRequiresCompleteRuntimeHealthAndPricing(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	runtimeFixture := saveProviderRuntimeFixture(t, svc, db)
	user := &model.User{ID: "viewer", Status: model.UserStatusActive, Role: model.UserRoleUser}

	assertVisible := func(want bool) {
		t.Helper()
		channels, err := svc.PublicSystemChannels(user)
		if err != nil {
			t.Fatal(err)
		}
		visible := false
		for _, channel := range channels {
			for _, modelKey := range channel.Models {
				if modelKey == runtimeFixture.channelModel.ModelKey {
					visible = true
				}
			}
		}
		if visible != want {
			t.Fatalf("Seedance 2.5 visible = %v, want %v", visible, want)
		}
	}

	assertVisible(true)
	if err := db.Model(&model.ProviderCredential{}).Where("id = ?", "credential").Update("health_status", "unhealthy").Error; err != nil {
		t.Fatal(err)
	}
	assertVisible(false)
	if _, err := svc.requireAccessibleChannelModel(user.ID, runtimeFixture.channel.ID, runtimeFixture.channelModel.ModelKey); err == nil {
		t.Fatal("requireAccessibleChannelModel() accepted a hidden unhealthy provider model")
	}
}

func TestKuaiziBoundModelCannotUseLegacyChannelSecretPathOrDefaultModel(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	runtimeFixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Model(&model.ModelChannel{}).Where("id = ?", runtimeFixture.channel.ID).Updates(map[string]any{
		"base_url": "https://legacy.example", "api_key": "legacy-secret",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.resolveProviderConfig(providerConfig{ChannelID: runtimeFixture.channel.ID}); err == nil || !strings.Contains(err.Error(), "显式指定模型") {
		t.Fatalf("default model resolution error = %v", err)
	}
	if _, err := svc.resolveProviderConfig(providerConfig{ChannelID: runtimeFixture.channel.ID, Model: runtimeFixture.channelModel.ModelKey}); err == nil || !strings.Contains(err.Error(), "冻结任务") {
		t.Fatalf("legacy provider secret path error = %v", err)
	}
}

func TestProviderConcurrencyClaimIsWiredBeforeBillingRuns(t *testing.T) {
	source, err := os.ReadFile("service.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	claim := strings.Index(body, "ClaimProviderTaskExecution(task.ID")
	billing := strings.Index(body, "MarkBillingRunning(task.BillingOrderID)")
	if claim < 0 || billing < 0 || claim > billing {
		t.Fatalf("provider execution claim index=%d billing index=%d", claim, billing)
	}
}

func TestKuaiziConcurrencyCapacityRequeueDoesNotBusyClaim(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	runtimeFixture := saveProviderRuntimeFixture(t, svc, db)
	if err := svc.repo.RequeueTaskWaitingForProviderCapacity(runtimeFixture.task.ID, "owner", runtimeFixture.task.LeaseToken); err != nil {
		t.Fatal(err)
	}
	claimed, err := svc.repo.ClaimNextTask("next-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claimed != nil {
		t.Fatalf("ClaimNextTask() immediately reclaimed capacity-waiting task: %#v", claimed)
	}
	var stored model.Task
	if err := db.First(&stored, "id = ?", runtimeFixture.task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.NextPollAt == nil || !stored.NextPollAt.After(time.Now()) {
		t.Fatalf("capacity retry time = %v", stored.NextPollAt)
	}
}

func TestProviderTaskRecoveryLeavesRunningForEveryPersistedFactState(t *testing.T) {
	tests := []struct {
		status         string
		providerTaskID string
		successFacts   bool
		billingStatus  model.BillingStatus
	}{
		{status: "poll_uncertain", providerTaskID: "provider-recovery", billingStatus: model.BillingStatusUncertain},
		{status: "succeeded", providerTaskID: "provider-recovery", successFacts: true, billingStatus: model.BillingStatusUncertain},
		{status: "failed", providerTaskID: "provider-recovery", billingStatus: model.BillingStatusUncertain},
		{status: "create_failed", billingStatus: model.BillingStatusRefunded},
		{status: "create_uncertain", billingStatus: model.BillingStatusUncertain},
	}
	for _, test := range tests {
		t.Run(test.status, func(t *testing.T) {
			svc, db := openProviderCredentialService(t)
			fixture := saveProviderRuntimeFixture(t, svc, db)
			if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 100, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
				t.Fatal(err)
			}
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path != kuaiziSeedance25StatusPath {
					t.Errorf("unexpected recovery request path %s", request.URL.Path)
				}
				writer.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(writer, `{"code":0,"message":"","data":{"task_id":"provider-recovery","status":"failed","error":"controlled rejection"},"trace_id":"trace-recovery"}`)
			}))
			defer server.Close()
			t.Setenv("CANVAS_ENVIRONMENT", "development")
			updates := map[string]any{"provider_status": test.status, "provider_task_id": test.providerTaskID}
			if test.successFacts {
				updates["asset_source_url"] = "https://127.0.0.1/result.mp4"
				updates["last_frame_url"] = "https://127.0.0.1/last.png"
				updates["actual_duration_seconds"] = 8
				updates["total_tokens"] = "42"
			}
			if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Updates(updates).Error; err != nil {
				t.Fatal(err)
			}
			baseURL := server.URL
			if test.successFacts {
				baseURL = "not-a-runtime-endpoint"
			}
			if err := db.Model(&model.ProviderEndpointVersion{}).Where("id = ?", "endpoint-v1").Update("base_url", baseURL).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("lease_owner", svc.workerID).Error; err != nil {
				t.Fatal(err)
			}
			task, err := svc.repo.Task(fixture.task.ID)
			if err != nil {
				t.Fatal(err)
			}
			_ = svc.processClaimedTask(task)
			stored, err := svc.repo.Task(fixture.task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status == model.TaskStatusRunning {
				t.Fatalf("task remains running after %s recovery: %#v", test.status, stored)
			}
			if test.successFacts && strings.Contains(stored.Error, "服务地址") {
				t.Fatalf("succeeded recovery incorrectly revalidated retired endpoint: %s", stored.Error)
			}
			var order model.BillingOrder
			if err := db.First(&order, "id = ?", fixture.order.ID).Error; err != nil {
				t.Fatal(err)
			}
			if order.Status != test.billingStatus {
				t.Fatalf("billing after %s recovery = %s, want %s", test.status, order.Status, test.billingStatus)
			}
		})
	}
}

func TestProviderSucceededRecoveryUsesMinimalFactsAndCompletesAssetAndBilling(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	var downloads atomic.Int64
	source := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		downloads.Add(1)
		if request.URL.Path != "/result.mp4" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "video/mp4")
		_, _ = writer.Write([]byte("safe-video-bytes"))
	}))
	defer source.Close()
	originalOutboundTransport := outboundTransport
	testOutboundTransport := outboundTransport.Clone()
	testOutboundTransport.TLSClientConfig = source.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	testOutboundTransport.DialContext = func(ctx context.Context, network string, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, source.Listener.Addr().String())
	}
	outboundTransport = testOutboundTransport
	t.Cleanup(func() { outboundTransport = originalOutboundTransport })
	sourceURL := strings.Replace(source.URL, "127.0.0.1", "example.com", 1)
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 100, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	brokenInput := `{"mode":"video","prompt":"old prompt","config":{"channelId":"channel","model":"kuaizi-seedance-2.5","videoSeconds":"4","vquality":"720p","size":"16:9"},"referenceImages":[{"storageKey":"resource:deleted-resource","role":"reference_image"}]}`
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Updates(map[string]any{"input_json": brokenInput, "lease_owner": svc.workerID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ProviderEndpointVersion{}).Where("id = ?", "endpoint-v1").Updates(map[string]any{"base_url": "not-a-valid-endpoint", "status": "retired"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ProviderCredentialVersion{}).Where("id = ?", "credential-v1").Updates(map[string]any{"key_cipher": "unusable-cipher", "status": "retired"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Updates(map[string]any{
		"provider_status": "succeeded", "provider_task_id": "provider-success-recovery", "asset_source_url": sourceURL + "/result.mp4",
		"last_frame_url": "https://cdn.example/last.png", "actual_duration_seconds": 8, "total_tokens": "42",
	}).Error; err != nil {
		t.Fatal(err)
	}
	task, err := svc.repo.Task(fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.processClaimedTask(task); err != nil {
		t.Fatalf("processClaimedTask() error = %v", err)
	}
	storedTask, err := svc.repo.Task(fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	var order model.BillingOrder
	if err := db.First(&order, "id = ?", fixture.order.ID).Error; err != nil {
		t.Fatal(err)
	}
	var generatedResources []model.Resource
	if err := db.Where("user_id = ? AND status = ?", fixture.task.UserID, model.ResourceStatusReady).Find(&generatedResources).Error; err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != model.TaskStatusSucceeded || order.Status != model.BillingStatusSettled || len(generatedResources) != 1 || downloads.Load() != 1 {
		t.Fatalf("succeeded recovery task=%s order=%s resources=%d downloads=%d error=%s", storedTask.Status, order.Status, len(generatedResources), downloads.Load(), storedTask.Error)
	}
	if generatedResources[0].SourceTaskID != fixture.task.ID || !generatedResources[0].QuotaReserved || generatedResources[0].QuotaDay == "" {
		t.Fatalf("provider result resource idempotency facts source_task_id=%q quota=%t/%q", generatedResources[0].SourceTaskID, generatedResources[0].QuotaReserved, generatedResources[0].QuotaDay)
	}
	storedFact, err := svc.repo.ProviderTaskFact(fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	recoveredResult, err := svc.processSucceededProviderTask(context.Background(), *storedTask, *storedFact)
	if err != nil {
		t.Fatalf("repeat succeeded projection error = %v", err)
	}
	if _, err := svc.persistKuaiziSeedance25Resource(*storedTask, recoveredResult); err != nil {
		t.Fatalf("repeat succeeded persistence error = %v", err)
	}
	var repeatedResources int64
	if err := db.Model(&model.Resource{}).Where("source_task_id = ?", fixture.task.ID).Count(&repeatedResources).Error; err != nil {
		t.Fatal(err)
	}
	if repeatedResources != 1 || downloads.Load() != 1 {
		t.Fatalf("repeat recovery resources=%d downloads=%d, want one idempotent asset/download", repeatedResources, downloads.Load())
	}
	quotaBefore, err := svc.repo.DailyUploadBytes(fixture.task.UserID, generatedResources[0].QuotaDay)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Resource{}).Where("id = ?", generatedResources[0].ID).Update("status", model.ResourceStatusPending).Error; err != nil {
		t.Fatal(err)
	}
	pendingLease := time.Now().Add(time.Minute)
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Updates(map[string]any{
		"status": model.TaskStatusRunning, "lease_owner": svc.workerID, "lease_token": "pending-resume-claim", "lease_expires_at": &pendingLease,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Update("reconciliation_status", "pending").Error; err != nil {
		t.Fatal(err)
	}
	pendingTask, err := svc.repo.Task(fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	pendingFact, err := svc.repo.ProviderTaskFact(fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	pendingResult, err := svc.processSucceededProviderTask(context.Background(), *pendingTask, *pendingFact)
	if err != nil {
		t.Fatalf("pending resource recovery projection error = %v", err)
	}
	if _, err := svc.persistKuaiziSeedance25Resource(*pendingTask, pendingResult); err != nil {
		t.Fatalf("pending resource recovery persistence error = %v", err)
	}
	quotaAfter, err := svc.repo.DailyUploadBytes(fixture.task.UserID, generatedResources[0].QuotaDay)
	if err != nil {
		t.Fatal(err)
	}
	if quotaAfter != quotaBefore || downloads.Load() != 2 {
		t.Fatalf("pending recovery quota=%d→%d downloads=%d, want idempotent quota and one resume download", quotaBefore, quotaAfter, downloads.Load())
	}
	if strings.Contains(storedTask.ResultJSON, sourceURL) || strings.Contains(storedTask.ResultJSON, "sourceUrl") || !strings.Contains(storedTask.ResultJSON, "resource:") {
		t.Fatalf("result JSON leaked source or missed persisted resource: %s", storedTask.ResultJSON)
	}
}

func TestValidateStoredKuaiziSeedance25SuccessPreservesOutputSecurityContract(t *testing.T) {
	valid := model.ProviderTaskFact{
		ProviderTaskID: "provider-success", AssetSourceURL: "https://cdn.example/result.mp4",
		LastFrameURL: "https://cdn.example/last.png", ActualDurationSeconds: 8, TotalTokens: "42",
	}
	for _, mutate := range []func(*model.ProviderTaskFact){
		func(fact *model.ProviderTaskFact) { fact.AssetSourceURL = "http://cdn.example/result.mp4" },
		func(fact *model.ProviderTaskFact) { fact.LastFrameURL = "https://user@cdn.example/last.png" },
		func(fact *model.ProviderTaskFact) { fact.TotalTokens = "-1" },
		func(fact *model.ProviderTaskFact) { fact.TotalTokens = "not-a-number" },
		func(fact *model.ProviderTaskFact) { fact.TotalTokens = strings.Repeat("9", 81) },
	} {
		fact := valid
		mutate(&fact)
		if err := validateStoredKuaiziSeedance25Success(fact); err == nil {
			t.Fatalf("validateStoredKuaiziSeedance25Success(%+v) error = nil", fact)
		}
	}
}

func TestProviderCreateUncertainOccupiesCapacityAfterTaskTerminalization(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Update("provider_status", "create_uncertain").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Updates(map[string]any{"status": model.TaskStatusFailed, "lease_owner": ""}).Error; err != nil {
		t.Fatal(err)
	}
	secondTask := providerRuntimeTask("task-capacity-after-uncertain", "user-capacity", "worker-capacity")
	secondTask.BillingOrderID = ""
	secondFact := providerRuntimeFact(secondTask.ID, "", "reserved")
	if err := db.Create(&secondTask).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&secondFact).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err := svc.repo.ClaimProviderTaskExecution(secondTask.ID, secondTask.LeaseOwner, secondTask.LeaseToken)
	if err != nil {
		t.Fatal(err)
	}
	if claimed {
		t.Fatal("reserved task exceeded capacity occupied by create_uncertain fact")
	}
}

func TestResolvedProviderFactCannotOccupyCapacityOrAcceptStalePoll(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Updates(map[string]any{
		"provider_status": "running", "provider_task_id": "provider-running", "reconciliation_status": "resolved",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Updates(map[string]any{"status": model.TaskStatusFailed, "lease_owner": ""}).Error; err != nil {
		t.Fatal(err)
	}
	secondTask := providerRuntimeTask("task-after-resolved-running", "user-after-resolved-running", "worker-after-resolved-running")
	secondTask.BillingOrderID = ""
	if err := db.Create(&secondTask).Error; err != nil {
		t.Fatal(err)
	}
	secondFact := providerRuntimeFact(secondTask.ID, "", "reserved")
	if err := db.Create(&secondFact).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err := svc.repo.ClaimProviderTaskExecution(secondTask.ID, secondTask.LeaseOwner, secondTask.LeaseToken)
	if err != nil {
		t.Fatal(err)
	}
	if !claimed {
		t.Fatal("resolved running fact still occupied provider capacity")
	}
	if err := svc.repo.UpdateProviderTaskPoll(fixture.task.ID, "succeeded", "stale-trace"); err == nil {
		t.Fatal("stale poll overwrote a resolved provider fact")
	}
	if err := svc.repo.SaveProviderTaskSuccess(fixture.task.ID, "succeeded", "stale-trace", "https://cdn.example/result.mp4", "https://cdn.example/last.png", 8, "42"); err == nil {
		t.Fatal("stale success overwrote a resolved provider fact")
	}
}

func TestResolvedProviderFactRejectsStaleTaskCompletion(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	admin := model.User{ID: "stale-completion-admin", Username: "stale-completion-admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	owner := model.User{ID: fixture.task.UserID, Username: "stale-completion-owner", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&[]model.User{admin, owner}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 100, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("id = ?", fixture.order.ID).Update("status", model.BillingStatusRunning).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Updates(map[string]any{
		"provider_status": "succeeded", "provider_task_id": "provider-success", "asset_source_url": "https://cdn.example/result.mp4",
		"last_frame_url": "https://cdn.example/last.png", "actual_duration_seconds": 8, "total_tokens": "42",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolveBillingOrder(&admin, fixture.order.ID, ResolveBillingRequest{Action: "refund", Note: "manual review won"}); err != nil {
		t.Fatal(err)
	}
	completed := fixture.task
	completed.Status = model.TaskStatusSucceeded
	if err := svc.repo.SaveProviderTaskCompletion(&completed, fixture.task.LeaseOwner, fixture.task.LeaseToken, nil, nil, nil); err == nil {
		t.Fatal("stale provider completion overwrote admin reconciliation")
	}
	storedTask, err := svc.repo.Task(fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	order, err := svc.repo.BillingOrder(fixture.order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != model.TaskStatusFailed || storedTask.Stage != "任务已人工核对终结" || storedTask.Error != "上游任务已完成人工核对" || storedTask.LeaseOwner != "" || storedTask.LeaseToken != "" || storedTask.LeaseExpiresAt != nil || order.Status != model.BillingStatusRefunded {
		t.Fatalf("stale completion task=%s billing=%s", storedTask.Status, order.Status)
	}
}

func TestProviderCompletionRequiresCurrentUnexpiredLeaseGeneration(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 100, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Updates(map[string]any{
		"provider_status": "succeeded", "provider_task_id": "provider-success", "asset_source_url": "https://cdn.example/result.mp4",
		"last_frame_url": "https://cdn.example/last.png", "actual_duration_seconds": 8, "total_tokens": "42",
	}).Error; err != nil {
		t.Fatal(err)
	}
	completed := fixture.task
	completed.Status = model.TaskStatusSucceeded
	if err := svc.repo.SaveProviderTaskCompletion(&completed, fixture.task.LeaseOwner, "stale-generation", nil, nil, nil); err == nil {
		t.Fatal("provider completion accepted a stale lease generation")
	}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("lease_expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.SaveProviderTaskCompletion(&completed, fixture.task.LeaseOwner, fixture.task.LeaseToken, nil, nil, nil); err == nil {
		t.Fatal("provider completion accepted an expired lease")
	}
	var stored model.Task
	if err := db.First(&stored, "id = ?", fixture.task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.TaskStatusRunning {
		t.Fatalf("stale completions changed task status to %s", stored.Status)
	}
}

func TestSourceTaskResourceWriteUsesFencedClaim(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Update("provider_status", "succeeded").Error; err != nil {
		t.Fatal(err)
	}
	resource := model.Resource{
		ID: "resource-fenced", UserID: fixture.task.UserID, SourceTaskID: fixture.task.ID,
		Kind: "video", Status: model.ResourceStatusPending, Provider: "local", ObjectKey: "pending/result.mp4",
		MimeType: "video/mp4", Size: 4, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	first, err := svc.repo.ClaimSourceTaskResourceWrite(fixture.task.UserID, fixture.task.ID, fixture.task.LeaseOwner, fixture.task.LeaseToken, "write-one", "pending/result-write-one.mp4", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repo.ClaimSourceTaskResourceWrite(fixture.task.UserID, fixture.task.ID, fixture.task.LeaseOwner, fixture.task.LeaseToken, "write-two", "pending/result-write-two.mp4", time.Minute); err == nil {
		t.Fatal("second writer claimed an unexpired resource write")
	}
	newTaskLeaseToken := "new-task-generation"
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("lease_token", newTaskLeaseToken).Error; err != nil {
		t.Fatal(err)
	}
	second, err := svc.repo.ClaimSourceTaskResourceWrite(fixture.task.UserID, fixture.task.ID, fixture.task.LeaseOwner, newTaskLeaseToken, "write-two", "pending/result-write-two.mp4", time.Minute)
	if err != nil {
		t.Fatalf("new task generation could not resume an unexpired stale resource claim: %v", err)
	}
	first.Status = model.ResourceStatusReady
	if err := svc.repo.CompleteSourceTaskResourceWrite(first, fixture.task.LeaseOwner, fixture.task.LeaseToken, "write-one"); err == nil {
		t.Fatal("stale resource writer completed after its fencing token was replaced")
	}
	second.Status = model.ResourceStatusReady
	if err := svc.repo.CompleteSourceTaskResourceWrite(second, fixture.task.LeaseOwner, newTaskLeaseToken, "write-two"); err != nil {
		t.Fatal(err)
	}
	var stored model.Resource
	if err := db.First(&stored, "id = ?", resource.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.ResourceStatusReady || stored.ObjectKey != "pending/result-write-two.mp4" || stored.WriteToken != "" || stored.WriteTaskLeaseToken != "" || stored.WriteLeaseExpiresAt != nil {
		t.Fatalf("resource claim did not converge: %+v", stored)
	}
}

func TestProviderRecoveryTaskUpdateFailureRollsBackBillingAccountAndLedger(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 100, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Update("provider_status", "create_failed").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("lease_owner", svc.workerID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_recovery_task_update BEFORE UPDATE ON tasks WHEN OLD.id = 'task-runtime' BEGIN SELECT RAISE(ABORT, 'recovery task update failed'); END`).Error; err != nil {
		t.Fatal(err)
	}
	task, err := svc.repo.Task(fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.processClaimedTask(task); err == nil || !strings.Contains(err.Error(), "recovery task update failed") {
		t.Fatalf("processClaimedTask() error = %v", err)
	}
	var order model.BillingOrder
	var account model.CreditAccount
	var ledgerCount int64
	if err := db.First(&order, "id = ?", fixture.order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&account, "user_id = ?", fixture.task.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ? AND type = ?", fixture.order.ID, model.CreditLedgerRefund).Count(&ledgerCount).Error; err != nil {
		t.Fatal(err)
	}
	if order.Status != model.BillingStatusRunning || account.AvailableMicrocredits != 100 || account.ReservedMicrocredits != fixture.order.AmountMicrocredits || ledgerCount != 0 {
		t.Fatalf("non-atomic recovery order=%s account=%#v refund_ledgers=%d", order.Status, account, ledgerCount)
	}
}

func TestProviderTaskSuccessFactsPersistBeforeAssetDownload(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	runtimeFixture := saveProviderRuntimeFixture(t, svc, db)
	var statusCalls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case kuaiziSeedance25CreatePath:
			_, _ = io.WriteString(writer, `{"code":0,"message":"","data":{"task_id":"provider-success"},"trace_id":"trace-create"}`)
		case kuaiziSeedance25StatusPath:
			statusCalls.Add(1)
			_, _ = io.WriteString(writer, `{"code":0,"message":"","data":{"task_id":"provider-success","status":"succeeded","video_url":"https://cdn.example.com/result.mp4","last_frame_url":"https://cdn.example.com/last.png","duration":8,"total_tokens":"42"},"trace_id":"trace-poll"}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	if err := db.Model(&model.ProviderEndpointVersion{}).Where("id = ?", "endpoint-v1").Update("base_url", server.URL).Error; err != nil {
		t.Fatal(err)
	}
	runtime, err := svc.resolveProviderTaskRuntime(runtimeFixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	client := NewKuaiziSeedance25Client(server.Client(), NewProviderSecretCipher(svc.dataDir))
	result, err := svc.executeKuaiziSeedance25Task(context.Background(), runtime, seedance25TestInput(), client, time.Millisecond)
	if err != nil {
		t.Fatalf("executeKuaiziSeedance25Task() error = %v", err)
	}
	if statusCalls.Load() != 1 || result["sourceUrl"] != "https://cdn.example.com/result.mp4" {
		t.Fatalf("result/status calls = %#v / %d", result, statusCalls.Load())
	}
	var fact model.ProviderTaskFact
	if err := db.First(&fact, "task_id = ?", runtimeFixture.task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if fact.ProviderTaskID != "provider-success" || fact.CreateTraceID != "trace-create" || fact.ProviderStatus != "succeeded" || fact.LastPollTraceID != "trace-poll" || fact.AssetSourceURL == "" || fact.LastFrameURL == "" || fact.ActualDurationSeconds != 8 || fact.TotalTokens != "42" {
		t.Fatalf("persisted success fact = %#v", fact)
	}
}

func TestPostgresKuaiziConcurrencyClaimsOneExecutionAcrossRepositories(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	account := model.ProviderAccount{ID: "account", ProviderKind: "kuaizi", Enabled: true, CreatedAt: now, UpdatedAt: now}
	credential := model.ProviderCredential{ID: "credential", ProviderAccountID: account.ID, Family: "seedance", Enabled: true, ConcurrencyLimit: 1, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatal(err)
	}
	for index, taskID := range []string{"task-a", "task-b"} {
		owner := "owner-" + taskID
		task := providerRuntimeTask(taskID, "user-"+taskID, owner)
		fact := providerRuntimeFact(taskID, "", "reserved")
		fact.ProviderCredentialID = credential.ID
		fact.ProviderCredentialVersionID = "credential-v1"
		fact.CreatedAt = now.Add(time.Duration(index) * time.Millisecond)
		fact.UpdatedAt = fact.CreatedAt
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&fact).Error; err != nil {
			t.Fatal(err)
		}
	}
	repositories := []*repository.Repository{repository.New(db.Session(&gorm.Session{NewDB: true})), repository.New(db.Session(&gorm.Session{NewDB: true}))}
	results := make([]bool, 2)
	errorsByWorker := make([]error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(2)
	for index := range repositories {
		go func(index int) {
			defer workers.Done()
			ready.Done()
			<-start
			results[index], errorsByWorker[index] = repositories[index].ClaimProviderTaskExecution("task-"+string(rune('a'+index)), "owner-task-"+string(rune('a'+index)), "owner-task-"+string(rune('a'+index))+"-claim")
		}(index)
	}
	ready.Wait()
	close(start)
	workers.Wait()
	claimed := 0
	for index := range results {
		if errorsByWorker[index] != nil {
			t.Fatalf("worker %d error = %v", index, errorsByWorker[index])
		}
		if results[index] {
			claimed++
		}
	}
	if claimed != 1 {
		t.Fatalf("claimed = %d, results=%v", claimed, results)
	}
	var claimedFact model.ProviderTaskFact
	if err := db.First(&claimedFact, "provider_status = ?", "execution_claimed").Error; err != nil {
		t.Fatal(err)
	}
	if claimedFact.ProviderCredentialVersionID != "credential-v1" {
		t.Fatalf("claimed credential version = %s", claimedFact.ProviderCredentialVersionID)
	}
}

func TestPostgresKuaiziCredentialFreezeRecoversV1AfterV2ActivationAcrossConnections(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	firstConnection := db.Session(&gorm.Session{NewDB: true})
	secondConnection := db.Session(&gorm.Session{NewDB: true})
	svc := New(repository.New(firstConnection), t.TempDir())
	var v1Calls atomic.Int64
	var v2Calls atomic.Int64
	var receivedKey string
	v1 := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		v1Calls.Add(1)
		receivedKey = request.Header.Get("ApiKey")
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"code":0,"message":"","data":{"task_id":"provider-frozen","status":"failed","error":"controlled rejection"},"trace_id":"trace-v1"}`)
	}))
	defer v1.Close()
	v2 := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { v2Calls.Add(1) }))
	defer v2.Close()
	fixture := saveProviderRuntimeFixture(t, svc, firstConnection)
	if err := firstConnection.Model(&model.ProviderEndpointVersion{}).Where("id = ?", "endpoint-v2").Updates(map[string]any{"base_url": v2.URL, "status": "pending"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := firstConnection.Model(&model.ProviderEndpointVersion{}).Where("id = ?", "endpoint-v1").Updates(map[string]any{"base_url": v1.URL, "status": "active"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := firstConnection.Model(&model.ProviderCredentialVersion{}).Where("id = ?", "credential-v2").Update("status", "pending").Error; err != nil {
		t.Fatal(err)
	}
	if err := firstConnection.Model(&model.ProviderCredentialVersion{}).Where("id = ?", "credential-v1").Update("status", "active").Error; err != nil {
		t.Fatal(err)
	}
	if err := firstConnection.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Updates(map[string]any{"provider_status": "poll_uncertain", "provider_task_id": "provider-frozen"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := secondConnection.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.ProviderEndpointVersion{}).Where("id = ?", "endpoint-v1").Update("status", "retired").Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ProviderEndpointVersion{}).Where("id = ?", "endpoint-v2").Update("status", "active").Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ProviderCredentialVersion{}).Where("id = ?", "credential-v1").Update("status", "retired").Error; err != nil {
			return err
		}
		return tx.Model(&model.ProviderCredentialVersion{}).Where("id = ?", "credential-v2").Update("status", "active").Error
	}); err != nil {
		t.Fatal(err)
	}
	runtime, err := svc.resolveProviderTaskRuntime(fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	client := NewKuaiziSeedance25Client(v1.Client(), NewProviderSecretCipher(svc.dataDir))
	_, err = svc.executeKuaiziSeedance25Task(context.Background(), runtime, seedance25TestInput(), client, time.Millisecond)
	if err == nil {
		t.Fatal("recovered failed provider task returned nil error")
	}
	if runtime.EndpointVersion.ID != "endpoint-v1" || runtime.CredentialVersion.ID != "credential-v1" || receivedKey != "key-v1" || v1Calls.Load() != 1 || v2Calls.Load() != 0 {
		t.Fatalf("frozen recovery endpoint=%s credential=%s key=%q v1=%d v2=%d", runtime.EndpointVersion.ID, runtime.CredentialVersion.ID, receivedKey, v1Calls.Load(), v2Calls.Load())
	}
}

func TestPostgresProviderTaskFactInsertFailureRollsBackCommercialFactsAcrossConnections(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	setupConnection := db.Session(&gorm.Session{NewDB: true})
	executionConnection := db.Session(&gorm.Session{NewDB: true})
	const userID = "pg-provider-rollback-user"
	if err := setupConnection.Create(&model.CreditAccount{UserID: userID, AvailableMicrocredits: 1_000}).Error; err != nil {
		t.Fatal(err)
	}
	if err := setupConnection.Exec(`CREATE FUNCTION fail_provider_fact_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'provider fact insert failed'; END $$`).Error; err != nil {
		t.Fatal(err)
	}
	if err := setupConnection.Exec(`CREATE TRIGGER fail_provider_fact BEFORE INSERT ON provider_task_facts FOR EACH ROW EXECUTE FUNCTION fail_provider_fact_insert()`).Error; err != nil {
		t.Fatal(err)
	}
	task := providerRuntimeTask("pg-task-rollback", userID, "")
	task.Status = model.TaskStatusQueued
	order := providerRuntimeBillingOrder("pg-billing-rollback", task.ID, userID)
	order.Status = model.BillingStatusReserved
	fact := providerRuntimeFact(task.ID, order.ID, "reserved")
	err := repository.New(executionConnection).CreateTaskWithCreditReservation(&task, &order, &fact, repository.ActiveTaskPolicy{Unlimited: true})
	if err == nil || !strings.Contains(err.Error(), "provider fact insert failed") {
		t.Fatalf("CreateTaskWithCreditReservation() error = %v", err)
	}
	for _, target := range []struct {
		model any
		where string
		value string
	}{{&model.Task{}, "id = ?", task.ID}, {&model.BillingOrder{}, "id = ?", order.ID}, {&model.ProviderTaskFact{}, "task_id = ?", task.ID}, {&model.CreditLedgerEntry{}, "billing_order_id = ?", order.ID}} {
		var count int64
		if err := setupConnection.Model(target.model).Where(target.where, target.value).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%T rows = %d, want rollback", target.model, count)
		}
	}
	var account model.CreditAccount
	if err := setupConnection.First(&account, "user_id = ?", userID).Error; err != nil {
		t.Fatal(err)
	}
	if account.AvailableMicrocredits != 1_000 || account.ReservedMicrocredits != 0 {
		t.Fatalf("credit account after rollback = %#v", account)
	}
}

func TestPostgresProviderBillingRefundRaceIsIdempotentAcrossConnections(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	svc := New(repository.New(db.Session(&gorm.Session{NewDB: true})), t.TempDir())
	fixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 100, ReservedMicrocredits: 8}).Error; err != nil {
		t.Fatal(err)
	}
	otherOrder := providerRuntimeBillingOrder("billing-other-reservation", "task-other-reservation", fixture.task.UserID)
	if err := db.Create(&otherOrder).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Update("provider_status", "create_failed").Error; err != nil {
		t.Fatal(err)
	}
	blocker := db.Begin()
	if blocker.Error != nil {
		t.Fatal(blocker.Error)
	}
	var lockedAccount model.CreditAccount
	if err := blocker.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedAccount, "user_id = ?", fixture.task.UserID).Error; err != nil {
		_ = blocker.Rollback()
		t.Fatal(err)
	}
	recoveryRepo := repository.New(db.Session(&gorm.Session{NewDB: true}))
	ordinaryRepo := repository.New(db.Session(&gorm.Session{NewDB: true}))
	start := make(chan struct{})
	errorsByPath := make(chan error, 2)
	go func() {
		<-start
		_, err := recoveryRepo.FinalizeProviderTaskRecovery(fixture.task.ID, fixture.task.LeaseOwner, fixture.task.LeaseToken)
		errorsByPath <- err
	}()
	go func() {
		<-start
		errorsByPath <- ordinaryRepo.RefundBillingOrder(fixture.order.ID, "ordinary refund")
	}()
	close(start)
	time.Sleep(150 * time.Millisecond)
	if err := blocker.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := <-errorsByPath; err != nil {
			t.Fatalf("concurrent refund error = %v", err)
		}
	}
	var account model.CreditAccount
	if err := db.First(&account, "user_id = ?", fixture.task.UserID).Error; err != nil {
		t.Fatal(err)
	}
	var refundLedgers int64
	if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ? AND type = ?", fixture.order.ID, model.CreditLedgerRefund).Count(&refundLedgers).Error; err != nil {
		t.Fatal(err)
	}
	if account.AvailableMicrocredits != 104 || account.ReservedMicrocredits != 4 || refundLedgers != 1 {
		t.Fatalf("refund race account=%#v refund_ledgers=%d, want one delta/ledger", account, refundLedgers)
	}
}

func TestPostgresProviderReconciliationConvergesAcrossRecoveryAndAdminOrder(t *testing.T) {
	for _, recoveryFirst := range []bool{true, false} {
		name := "admin_then_recovery"
		if recoveryFirst {
			name = "recovery_then_admin"
		}
		t.Run(name, func(t *testing.T) {
			db := testsupport.OpenPaymentIntegrationPostgres(t)
			if err := database.MigrateSchema(db); err != nil {
				t.Fatal(err)
			}
			svc := New(repository.New(db.Session(&gorm.Session{NewDB: true})), t.TempDir())
			fixture := saveProviderRuntimeFixture(t, svc, db)
			admin := model.User{ID: "reconciliation-admin", Username: "reconciliation-admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
			owner := model.User{ID: fixture.task.UserID, Username: "reconciliation-owner", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
			if err := db.Create(&[]model.User{admin, owner}).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 100, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
				t.Fatal(err)
			}
			if err := svc.repo.MarkProviderTaskCreateUncertain(fixture.task.ID, "controlled uncertainty"); err != nil {
				t.Fatal(err)
			}
			recover := func() error {
				finalized, err := repository.New(db.Session(&gorm.Session{NewDB: true})).FinalizeProviderTaskRecovery(fixture.task.ID, fixture.task.LeaseOwner, fixture.task.LeaseToken)
				if err == nil && !finalized {
					return errors.New("provider recovery did not report terminal convergence")
				}
				return err
			}
			resolve := func() error {
				_, err := svc.ResolveBillingOrder(&admin, fixture.order.ID, ResolveBillingRequest{Action: "refund", Note: "provider reconciliation"})
				return err
			}
			if recoveryFirst {
				if err := recover(); err != nil {
					t.Fatal(err)
				}
				if err := resolve(); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := resolve(); err != nil {
					t.Fatal(err)
				}
				if err := recover(); err != nil {
					t.Fatal(err)
				}
			}
			var storedTask model.Task
			var storedFact model.ProviderTaskFact
			var storedOrder model.BillingOrder
			if err := db.First(&storedTask, "id = ?", fixture.task.ID).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.First(&storedFact, "task_id = ?", fixture.task.ID).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.First(&storedOrder, "id = ?", fixture.order.ID).Error; err != nil {
				t.Fatal(err)
			}
			var ledgers int64
			if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ? AND type = ?", fixture.order.ID, model.CreditLedgerRefund).Count(&ledgers).Error; err != nil {
				t.Fatal(err)
			}
			if storedTask.Status == model.TaskStatusRunning || storedFact.ReconciliationStatus != "resolved" || storedFact.ProviderStatus == "create_uncertain" || storedOrder.Status != model.BillingStatusRefunded || ledgers != 1 {
				t.Fatalf("reconciliation task=%s fact=%s/%s order=%s ledgers=%d", storedTask.Status, storedFact.ProviderStatus, storedFact.ReconciliationStatus, storedOrder.Status, ledgers)
			}
			secondTask := providerRuntimeTask("task-after-reconciliation", "user-after-reconciliation", "worker-after-reconciliation")
			secondTask.BillingOrderID = ""
			secondFact := providerRuntimeFact(secondTask.ID, "", "reserved")
			if err := db.Create(&secondTask).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&secondFact).Error; err != nil {
				t.Fatal(err)
			}
			claimed, err := repository.New(db.Session(&gorm.Session{NewDB: true})).ClaimProviderTaskExecution(secondTask.ID, secondTask.LeaseOwner, secondTask.LeaseToken)
			if err != nil {
				t.Fatal(err)
			}
			if !claimed {
				t.Fatal("resolved create uncertainty still occupies provider capacity")
			}
		})
	}
}

func TestPostgresProviderLeaseAndResourceFencingAcrossConnections(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	first := repository.New(db.Session(&gorm.Session{NewDB: true}))
	second := repository.New(db.Session(&gorm.Session{NewDB: true}))
	svc := New(first, t.TempDir())
	fixture := saveProviderRuntimeFixture(t, svc, db)
	oldToken := fixture.task.LeaseToken
	if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 100, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Update("provider_status", "succeeded").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("lease_expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	reclaimed, err := second.ClaimNextTask(fixture.task.LeaseOwner, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed == nil || reclaimed.ID != fixture.task.ID || reclaimed.LeaseToken == "" || reclaimed.LeaseToken == oldToken {
		t.Fatalf("reclaim did not rotate lease generation: %#v", reclaimed)
	}
	staleCompletion := fixture.task
	staleCompletion.Status = model.TaskStatusSucceeded
	if err := first.SaveProviderTaskCompletion(&staleCompletion, fixture.task.LeaseOwner, oldToken, nil, nil, nil); err == nil {
		t.Fatal("expired provider worker completed after a same-owner reclaim")
	}
	staleOrder, err := first.BillingOrder(fixture.order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if staleOrder.Status != model.BillingStatusRunning {
		t.Fatalf("stale completion changed billing to %s", staleOrder.Status)
	}
	resource := model.Resource{
		ID: "pg-resource-fenced", UserID: fixture.task.UserID, SourceTaskID: fixture.task.ID,
		Kind: "video", Status: model.ResourceStatusPending, Provider: "local", ObjectKey: "pending/result.mp4",
		MimeType: "video/mp4", Size: 4, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	claimedFirst, err := first.ClaimSourceTaskResourceWrite(resource.UserID, fixture.task.ID, reclaimed.LeaseOwner, reclaimed.LeaseToken, "pg-write-one", "pending/result-one.mp4", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("lease_expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	secondGeneration, err := second.ClaimNextTask(reclaimed.LeaseOwner, time.Minute)
	if err != nil || secondGeneration == nil || secondGeneration.LeaseToken == reclaimed.LeaseToken {
		t.Fatalf("second task generation = %#v, error = %v", secondGeneration, err)
	}
	claimedSecond, err := second.ClaimSourceTaskResourceWrite(resource.UserID, fixture.task.ID, secondGeneration.LeaseOwner, secondGeneration.LeaseToken, "pg-write-two", "pending/result-two.mp4", time.Minute)
	if err != nil {
		t.Fatalf("new task generation could not fence an unexpired resource claim: %v", err)
	}
	claimedFirst.Status = model.ResourceStatusReady
	if err := first.CompleteSourceTaskResourceWrite(claimedFirst, reclaimed.LeaseOwner, reclaimed.LeaseToken, "pg-write-one"); err == nil {
		t.Fatal("expired PostgreSQL resource writer completed after a new claim")
	}
	claimedSecond.Status = model.ResourceStatusReady
	if err := second.CompleteSourceTaskResourceWrite(claimedSecond, secondGeneration.LeaseOwner, secondGeneration.LeaseToken, "pg-write-two"); err != nil {
		t.Fatal(err)
	}
	storedTask, err := first.Task(fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	storedResource, err := first.Resource(resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != model.TaskStatusRunning || storedTask.LeaseToken != secondGeneration.LeaseToken || storedResource.Status != model.ResourceStatusReady || storedResource.ObjectKey != "pending/result-two.mp4" {
		t.Fatalf("fencing did not converge task=%#v resource=%#v", storedTask, storedResource)
	}
}

type providerRuntimeFixture struct {
	task         model.Task
	order        model.BillingOrder
	channel      model.ModelChannel
	channelModel model.ChannelModel
	endpointV1   string
}

func saveProviderRuntimeFixture(t *testing.T, svc *Service, db *gorm.DB) providerRuntimeFixture {
	t.Helper()
	now := time.Now()
	account := model.ProviderAccount{ID: "account", ProviderKind: "kuaizi", Name: "Kuaizi", Enabled: true, CreatedAt: now, UpdatedAt: now}
	endpointV1 := "https://endpoint-v1.example.net"
	endpoints := []model.ProviderEndpointVersion{
		{ID: "endpoint-v1", ProviderAccountID: account.ID, BaseURL: endpointV1, Status: "retired", Version: 1, CreatedAt: now},
		{ID: "endpoint-v2", ProviderAccountID: account.ID, BaseURL: "https://endpoint-v2.example.net", Status: "active", Version: 2, CreatedAt: now.Add(time.Second)},
	}
	credential := model.ProviderCredential{ID: "credential", ProviderAccountID: account.ID, Family: "seedance", HealthStatus: "healthy", Enabled: true, ConcurrencyLimit: 1, CreatedAt: now, UpdatedAt: now}
	keyV1, err := svc.EncryptProviderSecret(account.ID, credential.ID, "credential-v1", "key-v1")
	if err != nil {
		t.Fatal(err)
	}
	keyV2, err := NewProviderSecretCipher(svc.dataDir).Encrypt(account.ID, credential.ID, "credential-v2", "key-v2")
	if err != nil {
		t.Fatal(err)
	}
	versions := []model.ProviderCredentialVersion{
		{ID: "credential-v1", ProviderCredentialID: credential.ID, KeyCipher: keyV1, KeyFingerprint: "fingerprint-v1", Status: "retired", Version: 1, CreatedAt: now},
		{ID: "credential-v2", ProviderCredentialID: credential.ID, KeyCipher: keyV2, KeyFingerprint: "fingerprint-v2", Status: "active", Version: 2, CreatedAt: now.Add(time.Second)},
	}
	channel := model.ModelChannel{ID: "channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "Kuaizi", ModelsJSON: `["kuaizi-seedance-2.5"]`, CreatedAt: now, UpdatedAt: now}
	channelModel := model.ChannelModel{
		ID: "channel-model", ChannelID: channel.ID, ProviderCredentialID: credential.ID, ModelKey: "kuaizi-seedance-2.5", DisplayName: "Seedance 2.5", Capability: "video",
		BillingMode: "per_second", PriceStrategy: "video_resolution", PriceConfigured: true, Enabled: true, PriceVersion: 1, CreatedAt: now, UpdatedAt: now,
		PriceTiers: []model.ChannelModelPriceTier{
			{ID: "480-standard", Resolution: "480P", InputVariant: "standard", UnitPriceMicrocredits: 1, PriceVersion: 1},
			{ID: "480-reference", Resolution: "480P", InputVariant: "reference_video", UnitPriceMicrocredits: 2, PriceVersion: 1},
			{ID: "720-standard", Resolution: "720P", InputVariant: "standard", UnitPriceMicrocredits: 3, PriceVersion: 1},
			{ID: "720-reference", Resolution: "720P", InputVariant: "reference_video", UnitPriceMicrocredits: 4, PriceVersion: 1},
		},
	}
	task := providerRuntimeTask("task-runtime", "user-runtime", "owner")
	order := providerRuntimeBillingOrder("billing-runtime", task.ID, task.UserID)
	fact := providerRuntimeFact(task.ID, order.ID, "reserved")
	for _, value := range []any{&account, &endpoints, &credential, &versions, &channel, &channelModel, &task, &order, &fact} {
		if err := db.Create(value).Error; err != nil {
			t.Fatalf("create %T: %v", value, err)
		}
	}
	return providerRuntimeFixture{task: task, order: order, channel: channel, channelModel: channelModel, endpointV1: endpointV1}
}

func providerRuntimeTask(id string, userID string, owner string) model.Task {
	now := time.Now()
	lease := now.Add(time.Minute)
	return model.Task{ID: id, UserID: userID, Type: "canvas_video", Capability: "video", Status: model.TaskStatusRunning, LeaseOwner: owner, LeaseToken: owner + "-claim", LeaseExpiresAt: &lease, BillingOrderID: "billing-runtime", Provider: "system", Model: "kuaizi-seedance-2.5", InputJSON: `{"mode":"video","prompt":"animate","config":{"channelId":"channel","model":"kuaizi-seedance-2.5","videoSeconds":"4","vquality":"720p","size":"16:9"}}`, CreatedAt: now, UpdatedAt: now}
}

func providerRuntimeBillingOrder(id string, taskID string, userID string) model.BillingOrder {
	now := time.Now()
	return model.BillingOrder{ID: id, UserID: userID, IdempotencyKey: id, TaskID: taskID, ChannelID: "channel", ChannelModelID: "channel-model", Model: "kuaizi-seedance-2.5", Capability: "video", BillingMode: "per_second", UnitPriceMicrocredits: 1, Quantity: 4, AmountMicrocredits: 4, Status: model.BillingStatusRunning, CreatedAt: now, UpdatedAt: now}
}

func providerRuntimeFact(taskID string, billingOrderID string, status string) model.ProviderTaskFact {
	now := time.Now()
	return model.ProviderTaskFact{
		TaskID: taskID, BillingOrderID: billingOrderID, ProviderAccountID: "account", ProviderEndpointVersionID: "endpoint-v1",
		ProviderCredentialID: "credential", ProviderCredentialVersionID: "credential-v1", ChannelModelID: "channel-model",
		RequestedDurationSeconds: 4, Resolution: "720p", InputVariant: "standard", ProviderStatus: status,
		ReconciliationStatus: "pending", CreatedAt: now, UpdatedAt: now,
	}
}
