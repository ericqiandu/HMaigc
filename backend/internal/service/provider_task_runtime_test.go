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
	"path/filepath"
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
	if err := db.Create(&model.CreditAccount{UserID: runtimeFixture.task.UserID, AvailableMicrocredits: 100, ReservedMicrocredits: runtimeFixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	if claimed, err := svc.repo.ClaimProviderTaskExecution(runtimeFixture.task.ID, runtimeFixture.task.LeaseOwner, runtimeFixture.task.LeaseToken); err != nil || !claimed {
		t.Fatalf("ClaimProviderTaskExecution() = %t, %v", claimed, err)
	}
	if err := svc.repo.MarkProviderTaskCreateStartedForLease(runtimeFixture.task.ID, repository.ProviderTaskLease{Owner: runtimeFixture.task.LeaseOwner, Token: runtimeFixture.task.LeaseToken}); err != nil {
		t.Fatal(err)
	}
	if err := svc.finalizeProviderExecutionFailure(runtimeFixture.task, &KuaiziSeedance25CreateUncertainError{Cause: errors.New("response lost")}); err != nil {
		t.Fatalf("finalizeProviderExecutionFailure() error = %v", err)
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
	if err := db.Create(&model.CreditAccount{UserID: runtimeFixture.task.UserID, AvailableMicrocredits: 100, ReservedMicrocredits: runtimeFixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	if claimed, err := svc.repo.ClaimProviderTaskExecution(runtimeFixture.task.ID, runtimeFixture.task.LeaseOwner, runtimeFixture.task.LeaseToken); err != nil || !claimed {
		t.Fatalf("ClaimProviderTaskExecution() = %t, %v", claimed, err)
	}
	if err := svc.repo.MarkProviderTaskCreateStartedForLease(runtimeFixture.task.ID, repository.ProviderTaskLease{Owner: runtimeFixture.task.LeaseOwner, Token: runtimeFixture.task.LeaseToken}); err != nil {
		t.Fatal(err)
	}
	if err := svc.finalizeProviderExecutionFailure(runtimeFixture.task, &KuaiziSeedance25CreateUncertainError{Cause: errors.New("response lost")}); err != nil {
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
	if err := db.Create(&model.CreditAccount{UserID: runtimeFixture.task.UserID, AvailableMicrocredits: 100, ReservedMicrocredits: runtimeFixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	if claimed, err := svc.repo.ClaimProviderTaskExecution(runtimeFixture.task.ID, runtimeFixture.task.LeaseOwner, runtimeFixture.task.LeaseToken); err != nil || !claimed {
		t.Fatalf("provider execution claim = %t, %v", claimed, err)
	}
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
	_, executionErr := svc.executeKuaiziSeedance25Task(context.Background(), runtime, seedance25TestInput(), client, time.Millisecond)
	if executionErr == nil {
		t.Fatal("executeKuaiziSeedance25Task() error = nil")
	}
	if err := svc.finalizeProviderExecutionFailure(runtimeFixture.task, executionErr); err != nil {
		t.Fatal(err)
	}
	var fact model.ProviderTaskFact
	if err := db.First(&fact, "task_id = ?", runtimeFixture.task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if fact.ProviderStatus != "create_failed" || fact.CreateTraceID != "" || fact.ReconciliationStatus != "resolved" {
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
			if err := db.Create(&model.CreditAccount{UserID: runtimeFixture.task.UserID, AvailableMicrocredits: 100, ReservedMicrocredits: runtimeFixture.order.AmountMicrocredits}).Error; err != nil {
				t.Fatal(err)
			}
			if claimed, err := svc.repo.ClaimProviderTaskExecution(runtimeFixture.task.ID, runtimeFixture.task.LeaseOwner, runtimeFixture.task.LeaseToken); err != nil || !claimed {
				t.Fatalf("provider execution claim = %t, %v", claimed, err)
			}
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
				if attempt == 0 {
					if finalizeErr := svc.finalizeProviderExecutionFailure(runtimeFixture.task, err); finalizeErr != nil {
						t.Fatal(finalizeErr)
					}
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
	if err := db.Create(&model.CreditAccount{UserID: runtimeFixture.task.UserID, AvailableMicrocredits: 100, ReservedMicrocredits: runtimeFixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	if claimed, err := svc.repo.ClaimProviderTaskExecution(runtimeFixture.task.ID, runtimeFixture.task.LeaseOwner, runtimeFixture.task.LeaseToken); err != nil || !claimed {
		t.Fatalf("provider execution claim = %t, %v", claimed, err)
	}
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
	_, executionErr := svc.executeKuaiziSeedance25Task(context.Background(), runtime, seedance25TestInput(), client, time.Millisecond)
	if executionErr == nil {
		t.Fatal("executeKuaiziSeedance25Task() error = nil")
	}
	if err := svc.finalizeProviderExecutionFailure(runtimeFixture.task, executionErr); err != nil {
		t.Fatal(err)
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

func TestProviderTerminalFailureUsesAtomicUncertainFinalizer(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 100, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	if claimed, err := svc.repo.ClaimProviderTaskExecution(fixture.task.ID, fixture.task.LeaseOwner, fixture.task.LeaseToken); err != nil || !claimed {
		t.Fatalf("provider execution claim = %t, %v", claimed, err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == kuaiziSeedance25CreatePath {
			_, _ = io.WriteString(writer, `{"code":0,"message":"","data":{"task_id":"provider-terminal-failure"},"trace_id":"trace-create"}`)
			return
		}
		_, _ = io.WriteString(writer, `{"code":0,"message":"","data":{"task_id":"provider-terminal-failure","status":"failed","error":"controlled failure"},"trace_id":"trace-failed"}`)
	}))
	defer server.Close()
	runtime, err := svc.resolveProviderTaskRuntime(fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	runtime.EndpointVersion.BaseURL = server.URL
	client := NewKuaiziSeedance25Client(server.Client(), NewProviderSecretCipher(svc.dataDir))
	_, executionErr := svc.executeKuaiziSeedance25Task(context.Background(), runtime, seedance25TestInput(), client, time.Millisecond)
	if executionErr == nil {
		t.Fatal("terminal provider failure returned nil")
	}
	if err := svc.finalizeProviderExecutionFailure(fixture.task, executionErr); err != nil {
		t.Fatal(err)
	}
	storedTask, _ := svc.repo.Task(fixture.task.ID)
	storedFact, _ := svc.repo.ProviderTaskFact(fixture.task.ID)
	storedOrder, _ := svc.repo.BillingOrder(fixture.order.ID)
	if storedTask.Status != model.TaskStatusFailed || storedFact.ProviderStatus != "failed" || storedFact.LastPollTraceID != "trace-failed" || storedFact.ReconciliationStatus != "manual_review" || storedOrder.Status != model.BillingStatusUncertain {
		t.Fatalf("terminal provider finalizer facts: task=%s fact=%#v billing=%s", storedTask.Status, storedFact, storedOrder.Status)
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
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
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
	processErr := svc.processClaimedTask(task)
	if processErr == nil {
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
		{status: "succeeded", providerTaskID: "provider-recovery", successFacts: true, billingStatus: model.BillingStatusRunning},
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
	originalOutboundTransport := externalBinaryTransport
	testOutboundTransport := externalBinaryTransport.Clone()
	testOutboundTransport.TLSClientConfig = source.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	testOutboundTransport.DialContext = func(ctx context.Context, network string, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, source.Listener.Addr().String())
	}
	externalBinaryTransport = testOutboundTransport
	t.Cleanup(func() { externalBinaryTransport = originalOutboundTransport })
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
	if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Updates(map[string]any{
		"reconciliation_status": "pending", "execution_lease_token": "pending-resume-claim",
	}).Error; err != nil {
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

func TestProviderTerminalCreateUncertainReleasesCapacityForNextTask(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Updates(map[string]any{"provider_status": "create_uncertain", "reconciliation_status": "manual_review"}).Error; err != nil {
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
	if !claimed {
		t.Fatal("terminal create_uncertain fact retained provider capacity")
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
	staleLease := repository.ProviderTaskLease{Owner: fixture.task.LeaseOwner, Token: fixture.task.LeaseToken}
	if err := svc.repo.UpdateProviderTaskPollForLease(fixture.task.ID, staleLease, "running", "stale-trace"); err == nil {
		t.Fatal("stale poll overwrote a resolved provider fact")
	}
	if err := svc.repo.SaveProviderTaskSuccessForLease(fixture.task.ID, staleLease, "stale-trace", "https://cdn.example/result.mp4", "https://cdn.example/last.png", 8, "42"); err == nil {
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
	if storedTask.Status != model.TaskStatusQueued || storedTask.Stage != providerResolvedPostprocessStage || storedTask.Error != providerResolvedTaskError || storedTask.LeaseOwner != "" || storedTask.LeaseToken != "" || storedTask.LeaseExpiresAt != nil || order.Status != model.BillingStatusRefunded {
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

func TestProviderTransitionsFenceStaleLeaseAndRejectBareTerminalSuccess(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Updates(map[string]any{
		"provider_status": "running", "provider_task_id": "provider-generation", "execution_lease_token": fixture.task.LeaseToken,
	}).Error; err != nil {
		t.Fatal(err)
	}
	oldLease := repository.ProviderTaskLease{Owner: fixture.task.LeaseOwner, Token: fixture.task.LeaseToken}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("lease_expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	newTask, err := svc.repo.ClaimNextTask(fixture.task.LeaseOwner, time.Minute)
	if err != nil || newTask == nil {
		t.Fatalf("reclaim task = %#v, error = %v", newTask, err)
	}
	if claimed, err := svc.repo.ClaimProviderTaskExecution(newTask.ID, newTask.LeaseOwner, newTask.LeaseToken); err != nil || !claimed {
		t.Fatalf("reclaimed provider execution = %t, %v", claimed, err)
	}
	newLease := repository.ProviderTaskLease{Owner: newTask.LeaseOwner, Token: newTask.LeaseToken}
	if err := svc.repo.SaveProviderTaskSuccessForLease(fixture.task.ID, newLease, "trace-success", "https://cdn.example/result.mp4", "https://cdn.example/last.png", 8, "42"); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.UpdateProviderTaskPollForLease(fixture.task.ID, oldLease, "running", "trace-late-running"); err == nil {
		t.Fatal("stale generation regressed a succeeded provider fact")
	}
	if err := svc.repo.UpdateProviderTaskPollForLease(fixture.task.ID, newLease, "succeeded", "trace-bare-success"); err == nil {
		t.Fatal("poll observer persisted terminal success without its payload")
	}
	stored, err := svc.repo.ProviderTaskFact(fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ProviderStatus != "succeeded" || stored.LastPollTraceID != "trace-success" || stored.AssetSourceURL == "" || stored.TotalTokens != "42" {
		t.Fatalf("provider success fact regressed: %#v", stored)
	}
}

func TestProviderFailureFinalizerRollsBackAllCommercialFactsAndReleasesCapacity(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 100, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	claimedExecution, err := svc.repo.ClaimProviderTaskExecution(fixture.task.ID, fixture.task.LeaseOwner, fixture.task.LeaseToken)
	if err != nil || !claimedExecution {
		t.Fatalf("ClaimProviderTaskExecution() = %t, %v", claimedExecution, err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_atomic_provider_terminal BEFORE UPDATE ON tasks WHEN OLD.id = 'task-runtime' BEGIN SELECT RAISE(ABORT, 'provider terminal task write failed'); END`).Error; err != nil {
		t.Fatal(err)
	}
	lease := repository.ProviderTaskLease{Owner: fixture.task.LeaseOwner, Token: fixture.task.LeaseToken}
	resolution := repository.ProviderTaskFailureResolution{
		ExpectedStatuses: []string{"execution_claimed"}, ProviderStatus: "execution_failed", ReconciliationStatus: "resolved",
		TaskStage: "任务失败", TaskError: "上游请求前本地失败",
	}
	if err := svc.repo.FinalizeProviderTaskRefund(fixture.task.ID, lease, resolution); err == nil || !strings.Contains(err.Error(), "provider terminal task write failed") {
		t.Fatalf("fault-injected finalizer error = %v", err)
	}
	assertProviderFailureFacts := func(wantFact string, wantReconciliation string, wantBilling model.BillingStatus, wantTask model.TaskStatus, wantAvailable int64, wantReserved int64, wantLedgers int64) {
		t.Helper()
		fact, err := svc.repo.ProviderTaskFact(fixture.task.ID)
		if err != nil {
			t.Fatal(err)
		}
		order, err := svc.repo.BillingOrder(fixture.order.ID)
		if err != nil {
			t.Fatal(err)
		}
		task, err := svc.repo.Task(fixture.task.ID)
		if err != nil {
			t.Fatal(err)
		}
		var account model.CreditAccount
		if err := db.First(&account, "user_id = ?", fixture.task.UserID).Error; err != nil {
			t.Fatal(err)
		}
		var ledgers int64
		if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ?", fixture.order.ID).Count(&ledgers).Error; err != nil {
			t.Fatal(err)
		}
		if fact.ProviderStatus != wantFact || fact.ReconciliationStatus != wantReconciliation || order.Status != wantBilling || task.Status != wantTask || account.AvailableMicrocredits != wantAvailable || account.ReservedMicrocredits != wantReserved || ledgers != wantLedgers {
			t.Fatalf("facts fact=%s/%s billing=%s task=%s account=%d/%d ledgers=%d", fact.ProviderStatus, fact.ReconciliationStatus, order.Status, task.Status, account.AvailableMicrocredits, account.ReservedMicrocredits, ledgers)
		}
	}
	assertProviderFailureFacts("execution_claimed", "pending", model.BillingStatusRunning, model.TaskStatusRunning, 100, fixture.order.AmountMicrocredits, 0)
	if err := db.Exec(`DROP TRIGGER fail_atomic_provider_terminal`).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.FinalizeProviderTaskRefund(fixture.task.ID, lease, resolution); err != nil {
		t.Fatal(err)
	}
	assertProviderFailureFacts("execution_failed", "resolved", model.BillingStatusRefunded, model.TaskStatusFailed, 100+fixture.order.AmountMicrocredits, 0, 1)

	secondTask := providerRuntimeTask("task-after-local-provider-failure", "user-after-local-provider-failure", "worker-after-local-provider-failure")
	secondTask.BillingOrderID = ""
	secondFact := providerRuntimeFact(secondTask.ID, "", "reserved")
	if err := db.Create(&secondTask).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&secondFact).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err := svc.repo.ClaimProviderTaskExecution(secondTask.ID, secondTask.LeaseOwner, secondTask.LeaseToken)
	if err != nil || !claimed {
		t.Fatalf("capacity after atomic local failure claimed=%t error=%v", claimed, err)
	}
}

func TestProviderAdminResolutionLetsInflightResourceWriterRecordReadyWithoutChangingCommercialResolution(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 100, ReservedMicrocredits: fixture.order.AmountMicrocredits, CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	fact, err := svc.repo.ProviderTaskFact(fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	fact.ProviderStatus = "succeeded"
	fact.ReconciliationStatus = "manual_review"
	fact.ProviderTaskID = "provider-ready-during-resolution"
	if err := svc.repo.Save(fact); err != nil {
		t.Fatal(err)
	}
	resource := model.Resource{
		ID: "resource-ready-during-resolution", UserID: fixture.task.UserID, SourceTaskID: fixture.task.ID,
		Kind: "video", Status: model.ResourceStatusPending, Provider: "local", ObjectKey: "pending/result.mp4",
		MimeType: "video/mp4", Size: 4, QuotaDay: "2026-08-12", QuotaReserved: true,
		WriteToken: "write-resolution", WriteTaskLeaseToken: fixture.task.LeaseToken,
		WriteLeaseExpiresAt: ptr(time.Now().Add(time.Minute)), CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := svc.repo.Create(&resource); err != nil {
		t.Fatal(err)
	}
	resolved, err := svc.repo.RefundProviderTaskBilling(fixture.order.ID, repository.ProviderTaskBillingResolution{
		ExpectedProviderStatus: "succeeded", ExpectedReconciliationStatus: "manual_review", ExpectedBillingStatus: model.BillingStatusRunning,
		ResolvedProviderStatus: "succeeded", ActorUserID: "admin", Note: "verified refund",
		TaskStatus: model.TaskStatusFailed, TaskStage: "resolved", TaskError: "resolved",
	})
	if err != nil || !resolved {
		t.Fatalf("RefundProviderTaskBilling() = %t, %v", resolved, err)
	}
	resource.Status = model.ResourceStatusReady
	resource.ETag = "etag-ready"
	if err := svc.repo.CompleteSourceTaskResourceWrite(&resource, fixture.task.LeaseOwner, fixture.task.LeaseToken, "write-resolution"); err != nil {
		t.Fatalf("CompleteSourceTaskResourceWrite() after resolution error = %v", err)
	}
	storedResource, err := svc.repo.Resource(resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	storedTask, _ := svc.repo.Task(fixture.task.ID)
	storedOrder, _ := svc.repo.BillingOrder(fixture.order.ID)
	storedFact, _ := svc.repo.ProviderTaskFact(fixture.task.ID)
	if storedResource.Status != model.ResourceStatusReady || storedResource.ETag != "etag-ready" {
		t.Fatalf("resource after resolution = %#v", storedResource)
	}
	if storedTask.Status != model.TaskStatusQueued || storedOrder.Status != model.BillingStatusRefunded || storedFact.ReconciliationStatus != "resolved" {
		t.Fatalf("commercial facts changed after writer completion: task=%s billing=%s fact=%s", storedTask.Status, storedOrder.Status, storedFact.ReconciliationStatus)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("next_poll_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	reclaimed, err := svc.repo.ClaimNextTask(svc.workerID, time.Minute)
	if err != nil || reclaimed == nil {
		t.Fatalf("claim ready resolved postprocess = %#v, %v", reclaimed, err)
	}
	if claimedExecution, err := svc.repo.ClaimProviderTaskExecution(reclaimed.ID, reclaimed.LeaseOwner, reclaimed.LeaseToken); err != nil || !claimedExecution {
		t.Fatalf("claim ready resolved execution = %t, %v", claimedExecution, err)
	}
	if err := svc.repo.CompleteResolvedProviderPostprocess(fixture.task.ID, repository.ProviderTaskLease{Owner: reclaimed.LeaseOwner, Token: reclaimed.LeaseToken}, model.TaskStatusFailed, "resolved", "resolved", reclaimed.InputJSON, `{"resourceId":"`+resource.ID+`"}`); err != nil {
		t.Fatal(err)
	}
	storedTask, _ = svc.repo.Task(fixture.task.ID)
	if storedTask.Status != model.TaskStatusFailed {
		t.Fatalf("ready resolved postprocess did not reach terminal projection: %#v", storedTask)
	}
}

func TestProviderAdminResolutionLeavesPendingResourcePostprocessClaimableUntilAssetReady(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	admin := model.User{ID: "pending-postprocess-admin", Username: "pending-postprocess-admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	owner := model.User{ID: fixture.task.UserID, Username: "pending-postprocess-owner", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&[]model.User{admin, owner}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 96, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	payload := []byte("pending-postprocess-video")
	source := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/result.mp4" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "video/mp4")
		_, _ = writer.Write(payload)
	}))
	defer source.Close()
	originalOutboundTransport := externalBinaryTransport
	testOutboundTransport := externalBinaryTransport.Clone()
	testOutboundTransport.TLSClientConfig = source.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	testOutboundTransport.DialContext = func(ctx context.Context, network string, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, source.Listener.Addr().String())
	}
	externalBinaryTransport = testOutboundTransport
	t.Cleanup(func() { externalBinaryTransport = originalOutboundTransport })
	sourceURL := strings.Replace(source.URL, "127.0.0.1", "example.com", 1) + "/result.mp4"
	if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Updates(map[string]any{
		"provider_status": "succeeded", "provider_task_id": "provider-pending-postprocess", "execution_lease_token": fixture.task.LeaseToken,
		"asset_source_url": sourceURL, "last_frame_url": "https://cdn.example/last.png", "actual_duration_seconds": 8, "total_tokens": "42",
	}).Error; err != nil {
		t.Fatal(err)
	}
	resource, err := svc.prepareSourceTaskResource(fixture.task.UserID, fixture.task.ID, "video", "generated.mp4", "video/mp4", int64(len(payload)), 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if resource.WriteToken != "" || resource.QuotaReserved {
		t.Fatalf("new pending resource unexpectedly claimed: %#v", resource)
	}

	if _, err := svc.ResolveBillingOrder(&admin, fixture.order.ID, ResolveBillingRequest{Action: "refund", Note: "preserve pending success asset"}); err != nil {
		t.Fatal(err)
	}
	handoffTask, _ := svc.repo.Task(fixture.task.ID)
	handoffResource, _ := svc.repo.ResourceForSourceTask(fixture.task.UserID, fixture.task.ID)
	if handoffTask.Status != model.TaskStatusQueued || handoffResource.Status != model.ResourceStatusPending || handoffResource.WriteResolution != "resolving" {
		t.Fatalf("pending postprocess handoff: task=%#v resource=%#v", handoffTask, handoffResource)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("next_poll_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	reclaimed, err := svc.repo.ClaimNextTask(svc.workerID, time.Minute)
	if err != nil || reclaimed == nil {
		t.Fatalf("claim pending postprocess = %#v, %v", reclaimed, err)
	}
	if err := svc.processClaimedTask(reclaimed); err != nil {
		t.Fatalf("process pending postprocess = %v", err)
	}

	finalTask, _ := svc.repo.Task(fixture.task.ID)
	finalFact, _ := svc.repo.ProviderTaskFact(fixture.task.ID)
	finalOrder, _ := svc.repo.BillingOrder(fixture.order.ID)
	finalResource, _ := svc.repo.ResourceForSourceTask(fixture.task.UserID, fixture.task.ID)
	var usage model.UserDailyUploadUsage
	if err := db.First(&usage, "user_id = ?", fixture.task.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if finalTask.Status != model.TaskStatusFailed || finalFact.ReconciliationStatus != "resolved" || finalOrder.Status != model.BillingStatusRefunded || finalResource.Status != model.ResourceStatusReady || usage.Bytes != int64(len(payload)) {
		t.Fatalf("pending postprocess completion: task=%#v fact=%#v order=%#v resource=%#v usage=%#v", finalTask, finalFact, finalOrder, finalResource, usage)
	}
}

func TestProviderAdminResolutionBeforeResourceCreationLeavesSucceededPostprocessClaimable(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	admin := model.User{ID: "pre-resource-admin", Username: "pre-resource-admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	owner := model.User{ID: fixture.task.UserID, Username: "pre-resource-owner", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&[]model.User{admin, owner}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 96, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	payload := []byte("postprocess-created-after-resolution")
	source := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "video/mp4")
		_, _ = writer.Write(payload)
	}))
	defer source.Close()
	originalOutboundTransport := externalBinaryTransport
	testOutboundTransport := externalBinaryTransport.Clone()
	testOutboundTransport.TLSClientConfig = source.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	testOutboundTransport.DialContext = func(ctx context.Context, network string, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, source.Listener.Addr().String())
	}
	externalBinaryTransport = testOutboundTransport
	t.Cleanup(func() { externalBinaryTransport = originalOutboundTransport })
	sourceURL := strings.Replace(source.URL, "127.0.0.1", "example.com", 1) + "/result.mp4"
	if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Updates(map[string]any{
		"provider_status": "succeeded", "provider_task_id": "provider-before-resource", "execution_lease_token": fixture.task.LeaseToken,
		"asset_source_url": sourceURL, "last_frame_url": "https://cdn.example/last.png", "actual_duration_seconds": 8, "total_tokens": "42",
	}).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := svc.ResolveBillingOrder(&admin, fixture.order.ID, ResolveBillingRequest{Action: "refund", Note: "resolve before resource creation"}); err != nil {
		t.Fatal(err)
	}
	storedTask, _ := svc.repo.Task(fixture.task.ID)
	storedFact, _ := svc.repo.ProviderTaskFact(fixture.task.ID)
	storedOrder, _ := svc.repo.BillingOrder(fixture.order.ID)
	if storedTask.Status != model.TaskStatusQueued || storedTask.LeaseToken != "" || storedFact.ReconciliationStatus != "resolved" || storedOrder.Status != model.BillingStatusRefunded {
		t.Fatalf("pre-resource resolution did not preserve postprocess entry: task=%#v fact=%#v order=%#v", storedTask, storedFact, storedOrder)
	}
	if _, err := svc.repo.ResourceForSourceTask(fixture.task.UserID, fixture.task.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("resource unexpectedly exists before postprocess recovery: %v", err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("next_poll_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	reclaimed, err := svc.repo.ClaimNextTask(svc.workerID, time.Minute)
	if err != nil || reclaimed == nil {
		t.Fatalf("claim postprocess without prior resource = %#v, %v", reclaimed, err)
	}
	if err := svc.processClaimedTask(reclaimed); err != nil {
		t.Fatalf("process postprocess without prior resource = %v", err)
	}
	finalTask, _ := svc.repo.Task(fixture.task.ID)
	finalResource, _ := svc.repo.ResourceForSourceTask(fixture.task.UserID, fixture.task.ID)
	var usage model.UserDailyUploadUsage
	if err := db.First(&usage, "user_id = ?", fixture.task.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if finalTask.Status != model.TaskStatusFailed || finalResource.Status != model.ResourceStatusReady || usage.Bytes != int64(len(payload)) {
		t.Fatalf("postprocess created after resolution did not converge: task=%#v resource=%#v usage=%#v", finalTask, finalResource, usage)
	}
}

func TestProviderAdminResolutionKeepsInflightWriteFailureRetryable(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	admin := model.User{ID: "failed-write-admin", Username: "failed-write-admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	owner := model.User{ID: fixture.task.UserID, Username: "failed-write-owner", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&[]model.User{admin, owner}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 96, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Updates(map[string]any{
		"provider_status": "succeeded", "provider_task_id": "provider-failed-write", "execution_lease_token": fixture.task.LeaseToken,
	}).Error; err != nil {
		t.Fatal(err)
	}
	resource, err := svc.prepareSourceTaskResource(fixture.task.UserID, fixture.task.ID, "video", "generated.mp4", "video/mp4", 24, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := svc.claimPreparedGeneratedResourceWrite(fixture.task.UserID, fixture.task, resource, "failed-write-token", "pending/failed-write.mp4", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolveBillingOrder(&admin, fixture.order.ID, ResolveBillingRequest{Action: "refund", Note: "resolve during failed write"}); err != nil {
		t.Fatal(err)
	}
	claimed.Status = model.ResourceStatusFailed
	claimed.Error = "recoverable object write failure"
	if err := svc.repo.CompleteSourceTaskResourceWrite(claimed, fixture.task.LeaseOwner, fixture.task.LeaseToken, "failed-write-token"); err != nil {
		t.Fatal(err)
	}
	queuedTask, _ := svc.repo.Task(fixture.task.ID)
	failedResource, _ := svc.repo.ResourceForSourceTask(fixture.task.UserID, fixture.task.ID)
	if queuedTask.Status != model.TaskStatusQueued || failedResource.Status != model.ResourceStatusFailed || failedResource.QuotaReserved {
		t.Fatalf("failed resolved writer lost retry entry: task=%#v resource=%#v", queuedTask, failedResource)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("next_poll_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	reclaimed, err := svc.repo.ClaimNextTask(svc.workerID, time.Minute)
	if err != nil || reclaimed == nil {
		t.Fatalf("claim failed resolved postprocess = %#v, %v", reclaimed, err)
	}
	if claimedExecution, err := svc.repo.ClaimProviderTaskExecution(reclaimed.ID, reclaimed.LeaseOwner, reclaimed.LeaseToken); err != nil || !claimedExecution {
		t.Fatalf("claim failed resolved execution = %t, %v", claimedExecution, err)
	}
	retry, err := svc.repo.ClaimSourceTaskResourceWriteWithQuota(fixture.task.UserID, fixture.task.ID, reclaimed.LeaseOwner, reclaimed.LeaseToken, "retry-write-token", "pending/retry-write.mp4", time.Minute, "2026-08-12", 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	retry.Status = model.ResourceStatusReady
	retry.ETag = "retry-ready"
	if err := svc.repo.CompleteSourceTaskResourceWrite(retry, reclaimed.LeaseOwner, reclaimed.LeaseToken, "retry-write-token"); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CompleteResolvedProviderPostprocess(fixture.task.ID, repository.ProviderTaskLease{Owner: reclaimed.LeaseOwner, Token: reclaimed.LeaseToken}, model.TaskStatusFailed, "resolved", "resolved", reclaimed.InputJSON, `{"resourceId":"`+retry.ID+`"}`); err != nil {
		t.Fatal(err)
	}
	finalTask, _ := svc.repo.Task(fixture.task.ID)
	finalResource, _ := svc.repo.ResourceForSourceTask(fixture.task.UserID, fixture.task.ID)
	finalOrder, _ := svc.repo.BillingOrder(fixture.order.ID)
	if finalTask.Status != model.TaskStatusFailed || finalResource.Status != model.ResourceStatusReady || finalOrder.Status != model.BillingStatusRefunded {
		t.Fatalf("failed writer retry did not converge: task=%#v resource=%#v order=%#v", finalTask, finalResource, finalOrder)
	}
}

func TestProviderQuotaAndWriteClaimAreAtomicAgainstAdminResolution(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	admin := model.User{ID: "atomic-resource-admin", Username: "atomic-resource-admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	owner := model.User{ID: fixture.task.UserID, Username: "atomic-resource-owner", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&[]model.User{admin, owner}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 96, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Updates(map[string]any{
		"provider_status": "succeeded", "provider_task_id": "provider-atomic-resource", "execution_lease_token": fixture.task.LeaseToken,
		"asset_source_url": "https://cdn.example/result.mp4", "last_frame_url": "https://cdn.example/last.png", "actual_duration_seconds": 8, "total_tokens": "42",
	}).Error; err != nil {
		t.Fatal(err)
	}
	resource, err := svc.prepareSourceTaskResource(fixture.task.UserID, fixture.task.ID, "video", "generated.mp4", "video/mp4", 24, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	claimPaused := make(chan struct{})
	releaseClaim := make(chan struct{})
	const callbackName = "test:provider-quota-write-claim"
	if err := db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "resources" {
			return
		}
		updates, ok := tx.Statement.Dest.(map[string]any)
		if !ok || updates["write_token"] != "atomic-write-token" {
			return
		}
		close(claimPaused)
		<-releaseClaim
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callbackName) })

	claimDone := make(chan struct {
		resource *model.Resource
		err      error
	}, 1)
	go func() {
		claimed, claimErr := svc.claimPreparedGeneratedResourceWrite(fixture.task.UserID, fixture.task, resource, "atomic-write-token", "pending/atomic-result.mp4", time.Minute)
		claimDone <- struct {
			resource *model.Resource
			err      error
		}{claimed, claimErr}
	}()
	select {
	case <-claimPaused:
	case <-time.After(5 * time.Second):
		close(releaseClaim)
		t.Fatal("combined quota/write claim did not reach atomic resource update")
	}
	visibleResource, err := svc.repo.ResourceForSourceTask(fixture.task.UserID, fixture.task.ID)
	if err != nil {
		close(releaseClaim)
		t.Fatal(err)
	}
	if visibleResource.QuotaReserved || visibleResource.WriteToken != "" {
		close(releaseClaim)
		t.Fatalf("quota or write claim became separately visible: %#v", visibleResource)
	}
	close(releaseClaim)
	claimedResult := <-claimDone
	if claimedResult.err != nil {
		t.Fatal(claimedResult.err)
	}
	if _, err := svc.ResolveBillingOrder(&admin, fixture.order.ID, ResolveBillingRequest{Action: "refund", Note: "admin raced atomic resource claim"}); err != nil {
		t.Fatal(err)
	}
	claimedResult.resource.Status = model.ResourceStatusReady
	claimedResult.resource.ETag = "atomic-ready"
	if err := svc.repo.CompleteSourceTaskResourceWrite(claimedResult.resource, fixture.task.LeaseOwner, fixture.task.LeaseToken, "atomic-write-token"); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("next_poll_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	reclaimed, err := svc.repo.ClaimNextTask(svc.workerID, time.Minute)
	if err != nil || reclaimed == nil {
		t.Fatalf("claim atomic resolved postprocess = %#v, %v", reclaimed, err)
	}
	if claimedExecution, err := svc.repo.ClaimProviderTaskExecution(reclaimed.ID, reclaimed.LeaseOwner, reclaimed.LeaseToken); err != nil || !claimedExecution {
		t.Fatalf("claim atomic resolved execution = %t, %v", claimedExecution, err)
	}
	if err := svc.repo.CompleteResolvedProviderPostprocess(fixture.task.ID, repository.ProviderTaskLease{Owner: reclaimed.LeaseOwner, Token: reclaimed.LeaseToken}, model.TaskStatusFailed, "resolved", "resolved", reclaimed.InputJSON, `{"resourceId":"`+claimedResult.resource.ID+`"}`); err != nil {
		t.Fatal(err)
	}

	storedTask, _ := svc.repo.Task(fixture.task.ID)
	storedFact, _ := svc.repo.ProviderTaskFact(fixture.task.ID)
	storedOrder, _ := svc.repo.BillingOrder(fixture.order.ID)
	storedResource, _ := svc.repo.ResourceForSourceTask(fixture.task.UserID, fixture.task.ID)
	var usage model.UserDailyUploadUsage
	if err := db.First(&usage, "user_id = ?", fixture.task.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != model.TaskStatusFailed || storedFact.ReconciliationStatus != "resolved" || storedOrder.Status != model.BillingStatusRefunded || storedResource.Status != model.ResourceStatusReady || usage.Bytes != resource.Size {
		t.Fatalf("atomic quota/write claim convergence: task=%#v fact=%#v order=%#v resource=%#v usage=%#v", storedTask, storedFact, storedOrder, storedResource, usage)
	}
}

func TestProviderSucceededPostprocessFailureRequeuesAndNextGenerationCompletesSameResource(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 100, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Updates(map[string]any{
		"provider_status": "succeeded", "provider_task_id": "provider-postprocess", "execution_lease_token": fixture.task.LeaseToken,
	}).Error; err != nil {
		t.Fatal(err)
	}
	quotaDay := "2026-08-12"
	resource := model.Resource{
		ID: "resource-postprocess-retry", UserID: fixture.task.UserID, SourceTaskID: fixture.task.ID,
		Kind: "video", Status: model.ResourceStatusPending, Provider: "local", ObjectKey: "pending/old.mp4",
		MimeType: "video/mp4", Size: 4, QuotaDay: quotaDay, QuotaReserved: true,
		WriteToken: "old-put", WriteTaskLeaseToken: fixture.task.LeaseToken, WriteLeaseExpiresAt: ptr(time.Now().Add(time.Minute)),
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.UserDailyUploadUsage{ID: fixture.task.UserID + ":" + quotaDay, UserID: fixture.task.UserID, Day: quotaDay, Bytes: resource.Size}).Error; err != nil {
		t.Fatal(err)
	}
	oldLease := repository.ProviderTaskLease{Owner: fixture.task.LeaseOwner, Token: fixture.task.LeaseToken}
	if err := svc.repo.RequeueProviderTaskPostprocess(fixture.task.ID, oldLease, "recoverable put failure"); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("next_poll_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	newTask, err := svc.repo.ClaimNextTask(fixture.task.LeaseOwner, time.Minute)
	if err != nil || newTask == nil || newTask.LeaseToken == oldLease.Token {
		t.Fatalf("postprocess reclaim = %#v, %v", newTask, err)
	}
	claimed, err := svc.repo.ClaimProviderTaskExecution(newTask.ID, newTask.LeaseOwner, newTask.LeaseToken)
	if err != nil || !claimed {
		t.Fatalf("postprocess execution claim = %t, %v", claimed, err)
	}
	current, err := svc.repo.ClaimSourceTaskResourceWriteWithQuota(fixture.task.UserID, fixture.task.ID, newTask.LeaseOwner, newTask.LeaseToken, "new-put", "pending/new.mp4", time.Minute, "2026-08-12", 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	current.Status = model.ResourceStatusReady
	current.ETag = "etag-new"
	if err := svc.repo.CompleteSourceTaskResourceWrite(current, newTask.LeaseOwner, newTask.LeaseToken, "new-put"); err != nil {
		t.Fatal(err)
	}
	completed := *newTask
	completed.Status = model.TaskStatusSucceeded
	completed.Stage = "任务完成"
	completed.Progress = 100
	completed.ResultJSON = `{"resourceId":"resource-postprocess-retry"}`
	completed.CompletedAt = ptr(time.Now())
	completed.LeaseOwner, completed.LeaseToken, completed.LeaseExpiresAt = "", "", nil
	if err := svc.repo.SaveProviderTaskCompletion(&completed, newTask.LeaseOwner, newTask.LeaseToken, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	var resourceCount int64
	var usage model.UserDailyUploadUsage
	storedOrder, _ := svc.repo.BillingOrder(fixture.order.ID)
	storedTask, _ := svc.repo.Task(fixture.task.ID)
	if err := db.Model(&model.Resource{}).Where("source_task_id = ?", fixture.task.ID).Count(&resourceCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&usage, "id = ?", fixture.task.UserID+":"+quotaDay).Error; err != nil {
		t.Fatal(err)
	}
	if resourceCount != 1 || usage.Bytes != resource.Size || storedTask.Status != model.TaskStatusSucceeded || storedOrder.Status != model.BillingStatusSettled {
		t.Fatalf("postprocess retry did not converge: resources=%d quota=%d task=%s billing=%s", resourceCount, usage.Bytes, storedTask.Status, storedOrder.Status)
	}
}

func TestProviderAdminResolutionCASRejectsAfterAutomaticCompletion(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 100, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Updates(map[string]any{
		"provider_status": "succeeded", "provider_task_id": "provider-auto-wins", "execution_lease_token": fixture.task.LeaseToken,
	}).Error; err != nil {
		t.Fatal(err)
	}
	completed := fixture.task
	completed.Status = model.TaskStatusSucceeded
	completed.Stage = "任务完成"
	completed.CompletedAt = ptr(time.Now())
	completed.LeaseOwner, completed.LeaseToken, completed.LeaseExpiresAt = "", "", nil
	if err := svc.repo.SaveProviderTaskCompletion(&completed, fixture.task.LeaseOwner, fixture.task.LeaseToken, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	resolved, err := svc.repo.RefundProviderTaskBilling(fixture.order.ID, repository.ProviderTaskBillingResolution{
		ExpectedProviderStatus: "succeeded", ExpectedReconciliationStatus: "pending", ExpectedBillingStatus: model.BillingStatusRunning,
		ResolvedProviderStatus: "succeeded_resolved", ActorUserID: "late-admin", Note: "late resolution",
		TaskStatus: model.TaskStatusFailed, TaskStage: "resolved", TaskError: "resolved",
	})
	if err == nil || resolved {
		t.Fatalf("late admin resolution = %t, %v", resolved, err)
	}
	storedOrder, _ := svc.repo.BillingOrder(fixture.order.ID)
	storedTask, _ := svc.repo.Task(fixture.task.ID)
	storedFact, _ := svc.repo.ProviderTaskFact(fixture.task.ID)
	if storedOrder.Status != model.BillingStatusSettled || storedOrder.ResolvedBy != "" || storedTask.Status != model.TaskStatusSucceeded || storedFact.ReconciliationStatus != "resolved" {
		t.Fatalf("late admin resolution mutated automatic completion: order=%#v task=%s fact=%s", storedOrder, storedTask.Status, storedFact.ReconciliationStatus)
	}
}

func TestActiveTaskCancellationIsGenerationScopedAndCancelsAllActiveGenerations(t *testing.T) {
	svc, _ := openProviderCredentialService(t)
	firstContext, firstCancel := context.WithCancel(context.Background())
	secondContext, secondCancel := context.WithCancel(context.Background())
	svc.registerActiveTask("task-cancel-generations", "lease-one", firstCancel)
	svc.registerActiveTask("task-cancel-generations", "lease-two", secondCancel)
	svc.cancelActiveTask("task-cancel-generations")
	if firstContext.Err() != context.Canceled || secondContext.Err() != context.Canceled {
		t.Fatalf("cancel all generations: first=%v second=%v", firstContext.Err(), secondContext.Err())
	}
	thirdContext, thirdCancel := context.WithCancel(context.Background())
	svc.registerActiveTask("task-cancel-generations", "lease-three", thirdCancel)
	svc.unregisterActiveTask("task-cancel-generations", "lease-one")
	svc.cancelActiveTask("task-cancel-generations")
	if thirdContext.Err() != context.Canceled {
		t.Fatalf("stale unregister deleted newer generation: %v", thirdContext.Err())
	}
}

func TestProviderCancelTaskBeforeOutboundAtomicallyRefundsAndReleasesCapacity(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 96, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	if claimed, err := svc.repo.ClaimProviderTaskExecution(fixture.task.ID, fixture.task.LeaseOwner, fixture.task.LeaseToken); err != nil || !claimed {
		t.Fatalf("provider execution claim = %t, %v", claimed, err)
	}

	cancelled, err := svc.CancelTask(fixture.task.UserID, fixture.task.ID)
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
	account, err := svc.repo.CreditAccount(fixture.task.UserID)
	if err != nil {
		t.Fatal(err)
	}
	var refunds int64
	if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ? AND type = ?", fixture.order.ID, model.CreditLedgerRefund).Count(&refunds).Error; err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != model.TaskStatusCancelled || order.Status != model.BillingStatusRefunded || fact.ProviderStatus != "cancelled_before_create" || fact.ReconciliationStatus != "resolved" {
		t.Fatalf("provider cancel facts: task=%s billing=%s provider=%s reconciliation=%s", cancelled.Status, order.Status, fact.ProviderStatus, fact.ReconciliationStatus)
	}
	if account.AvailableMicrocredits != 100 || account.ReservedMicrocredits != 0 || refunds != 1 {
		t.Fatalf("provider cancel credits: account=%#v refunds=%d", account, refunds)
	}

	secondTask := providerRuntimeTask("task-after-provider-cancel", "user-after-provider-cancel", "worker-after-provider-cancel")
	secondTask.BillingOrderID = ""
	if err := db.Create(&secondTask).Error; err != nil {
		t.Fatal(err)
	}
	secondFact := providerRuntimeFact(secondTask.ID, "", "reserved")
	if err := db.Create(&secondFact).Error; err != nil {
		t.Fatal(err)
	}
	if claimed, err := svc.repo.ClaimProviderTaskExecution(secondTask.ID, secondTask.LeaseOwner, secondTask.LeaseToken); err != nil || !claimed {
		t.Fatalf("provider capacity after terminal cancel = %t, %v", claimed, err)
	}
}

func TestProviderCancelTaskTakesOverExpiredRunningLeaseWithoutWaitingForReclaim(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 96, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	if claimed, err := svc.repo.ClaimProviderTaskExecution(fixture.task.ID, fixture.task.LeaseOwner, fixture.task.LeaseToken); err != nil || !claimed {
		t.Fatalf("provider execution claim = %t, %v", claimed, err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("lease_expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}

	cancelled, err := svc.CancelTask(fixture.task.UserID, fixture.task.ID)
	if err != nil {
		t.Fatalf("CancelTask() expired generation = %v", err)
	}
	fact, _ := svc.repo.ProviderTaskFact(fixture.task.ID)
	order, _ := svc.repo.BillingOrder(fixture.order.ID)
	account, _ := svc.repo.CreditAccount(fixture.task.UserID)
	if cancelled.Status != model.TaskStatusCancelled || cancelled.LeaseToken != "" || fact.ProviderStatus != "cancelled_before_create" || fact.ReconciliationStatus != "resolved" || order.Status != model.BillingStatusRefunded || account.AvailableMicrocredits != 100 || account.ReservedMicrocredits != 0 {
		t.Fatalf("expired cancel did not converge: task=%#v fact=%#v order=%#v account=%#v", cancelled, fact, order, account)
	}
}

func TestProviderCancelTaskAfterCreateBoundaryStaysClaimableUntilAdminResolution(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	admin := model.User{ID: "provider-cancel-admin", Username: "provider-cancel-admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	owner := model.User{ID: fixture.task.UserID, Username: "provider-cancel-owner", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&[]model.User{admin, owner}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 96, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	lease := repository.ProviderTaskLease{Owner: fixture.task.LeaseOwner, Token: fixture.task.LeaseToken}
	if claimed, err := svc.repo.ClaimProviderTaskExecution(fixture.task.ID, lease.Owner, lease.Token); err != nil || !claimed {
		t.Fatalf("provider execution claim = %t, %v", claimed, err)
	}
	if err := svc.repo.MarkProviderTaskCreateStartedForLease(fixture.task.ID, lease); err != nil {
		t.Fatal(err)
	}

	requested, err := svc.CancelTask(fixture.task.UserID, fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	fact, _ := svc.repo.ProviderTaskFact(fixture.task.ID)
	order, _ := svc.repo.BillingOrder(fixture.order.ID)
	if requested.Status != model.TaskStatusQueued || requested.LeaseToken != "" || fact.ProviderStatus != "creating" || fact.ReconciliationStatus != "cancel_requested" || order.Status != model.BillingStatusUncertain {
		t.Fatalf("outbound cancel request: task=%#v fact=%#v order=%#v", requested, fact, order)
	}

	secondTask := providerRuntimeTask("task-blocked-by-provider-cancel", "user-blocked-by-provider-cancel", "worker-blocked-by-provider-cancel")
	secondTask.BillingOrderID = ""
	if err := db.Create(&secondTask).Error; err != nil {
		t.Fatal(err)
	}
	secondFact := providerRuntimeFact(secondTask.ID, "", "reserved")
	if err := db.Create(&secondFact).Error; err != nil {
		t.Fatal(err)
	}
	if claimed, err := svc.repo.ClaimProviderTaskExecution(secondTask.ID, secondTask.LeaseOwner, secondTask.LeaseToken); err != nil || claimed {
		t.Fatalf("cancel review capacity claim = %t, %v, want occupied", claimed, err)
	}

	if _, err := svc.ResolveBillingOrder(&admin, fixture.order.ID, ResolveBillingRequest{Action: "refund", Note: "cancel review resolved"}); err != nil {
		t.Fatal(err)
	}
	resolvedTask, _ := svc.repo.Task(fixture.task.ID)
	resolvedFact, _ := svc.repo.ProviderTaskFact(fixture.task.ID)
	resolvedOrder, _ := svc.repo.BillingOrder(fixture.order.ID)
	if resolvedTask.Status != model.TaskStatusFailed || resolvedFact.ReconciliationStatus != "resolved" || resolvedOrder.Status != model.BillingStatusRefunded {
		t.Fatalf("resolved cancel request: task=%#v fact=%#v order=%#v", resolvedTask, resolvedFact, resolvedOrder)
	}
	if claimed, err := svc.repo.ClaimProviderTaskExecution(secondTask.ID, secondTask.LeaseOwner, secondTask.LeaseToken); err != nil || !claimed {
		t.Fatalf("provider capacity after cancel resolution = %t, %v", claimed, err)
	}
}

func TestProviderCancelTaskDuringPollRequeuesSameProviderTask(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 96, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Updates(map[string]any{
		"provider_status": "running", "provider_task_id": "provider-cancel-poll", "execution_lease_token": fixture.task.LeaseToken,
	}).Error; err != nil {
		t.Fatal(err)
	}

	requested, err := svc.CancelTask(fixture.task.UserID, fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if requested.Status != model.TaskStatusQueued {
		t.Fatalf("poll cancellation task status = %s", requested.Status)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("next_poll_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	reclaimed, err := svc.repo.ClaimNextTask(fixture.task.LeaseOwner, time.Minute)
	if err != nil || reclaimed == nil {
		t.Fatalf("claim cancellation review task = %#v, %v", reclaimed, err)
	}
	if claimed, err := svc.repo.ClaimProviderTaskExecution(reclaimed.ID, reclaimed.LeaseOwner, reclaimed.LeaseToken); err != nil || !claimed {
		t.Fatalf("claim cancellation review provider execution = %t, %v", claimed, err)
	}
	fact, err := svc.repo.ProviderTaskFact(fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fact.ProviderTaskID != "provider-cancel-poll" || fact.ProviderStatus != "running" || fact.ReconciliationStatus != "cancel_requested" || fact.ExecutionLeaseToken != reclaimed.LeaseToken {
		t.Fatalf("reclaimed cancellation review fact = %#v", fact)
	}
	if err := svc.repo.MarkProviderTaskCreateStartedForLease(fixture.task.ID, repository.ProviderTaskLease{Owner: reclaimed.LeaseOwner, Token: reclaimed.LeaseToken}); err == nil {
		t.Fatal("poll cancellation review allowed a second create")
	}
}

func TestProviderCancelTaskDuringResourcePutPreservesRetryableSuccessAsset(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 96, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	payload := []byte("cancel-during-put-video")
	source := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "video/mp4")
		_, _ = writer.Write(payload)
	}))
	defer source.Close()
	originalOutboundTransport := externalBinaryTransport
	testOutboundTransport := externalBinaryTransport.Clone()
	testOutboundTransport.TLSClientConfig = source.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	testOutboundTransport.DialContext = func(ctx context.Context, network string, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, source.Listener.Addr().String())
	}
	externalBinaryTransport = testOutboundTransport
	t.Cleanup(func() { externalBinaryTransport = originalOutboundTransport })
	sourceURL := strings.Replace(source.URL, "127.0.0.1", "example.com", 1) + "/result.mp4"
	if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Updates(map[string]any{
		"provider_status": "succeeded", "provider_task_id": "provider-cancel-put", "execution_lease_token": fixture.task.LeaseToken,
		"asset_source_url": sourceURL, "last_frame_url": "https://cdn.example/last.png", "actual_duration_seconds": 8, "total_tokens": "42",
	}).Error; err != nil {
		t.Fatal(err)
	}
	resource, err := svc.prepareSourceTaskResource(fixture.task.UserID, fixture.task.ID, "video", "generated.mp4", "video/mp4", int64(len(payload)), 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := svc.claimPreparedGeneratedResourceWrite(fixture.task.UserID, fixture.task, resource, "cancel-put-old", "pending/cancel-put-old.mp4", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	reader := &gatedProviderResourceReader{payload: payload, started: make(chan struct{}), release: make(chan struct{})}
	putDone := make(chan error, 1)
	go func() {
		_, putErr := svc.writeClaimedSourceTaskResource(fixture.task.UserID, claimed, fixture.task.LeaseOwner, fixture.task.LeaseToken, "cancel-put-old", reader)
		putDone <- putErr
	}()
	select {
	case <-reader.started:
	case <-time.After(5 * time.Second):
		close(reader.release)
		t.Fatal("resource put did not block")
	}
	requested, err := svc.CancelTask(fixture.task.UserID, fixture.task.ID)
	if err != nil {
		close(reader.release)
		t.Fatal(err)
	}
	if requested.Status != model.TaskStatusQueued {
		close(reader.release)
		t.Fatalf("resource put cancellation status = %s", requested.Status)
	}
	close(reader.release)
	if putErr := <-putDone; putErr != nil {
		t.Fatalf("cancel handoff rejected the original successful writer: %v", putErr)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("next_poll_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	reclaimed, err := svc.repo.ClaimNextTask(svc.workerID, time.Minute)
	if err != nil || reclaimed == nil {
		t.Fatalf("claim cancelled resource postprocess = %#v, %v", reclaimed, err)
	}
	if err := svc.processClaimedTask(reclaimed); err != nil {
		t.Fatal(err)
	}
	finalTask, _ := svc.repo.Task(fixture.task.ID)
	finalFact, _ := svc.repo.ProviderTaskFact(fixture.task.ID)
	finalOrder, _ := svc.repo.BillingOrder(fixture.order.ID)
	finalResource, _ := svc.repo.ResourceForSourceTask(fixture.task.UserID, fixture.task.ID)
	var usage model.UserDailyUploadUsage
	if err := db.First(&usage, "user_id = ?", fixture.task.UserID).Error; err != nil {
		t.Fatal(err)
	}
	var storedFiles int
	if err := filepath.WalkDir(filepath.Join(svc.dataDir, "resources"), func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr == nil && !entry.IsDir() {
			storedFiles++
		}
		return walkErr
	}); err != nil {
		t.Fatal(err)
	}
	if finalTask.Status != model.TaskStatusSucceeded || finalFact.ReconciliationStatus != "resolved" || finalOrder.Status != model.BillingStatusSettled || finalResource.Status != model.ResourceStatusReady || finalResource.ObjectKey != "pending/cancel-put-old.mp4" || usage.Bytes != int64(len(payload)) || storedFiles != 1 {
		t.Fatalf("cancelled resource put recovery: task=%#v fact=%#v order=%#v resource=%#v usage=%#v", finalTask, finalFact, finalOrder, finalResource, usage)
	}
}

func TestLateProviderCreateResponseHandsOffToCurrentGenerationWithoutSecondCreate(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	if claimed, err := svc.repo.ClaimProviderTaskExecution(fixture.task.ID, fixture.task.LeaseOwner, fixture.task.LeaseToken); err != nil || !claimed {
		t.Fatalf("initial provider execution = %t, %v", claimed, err)
	}
	oldLease := repository.ProviderTaskLease{Owner: fixture.task.LeaseOwner, Token: fixture.task.LeaseToken}
	if err := svc.repo.MarkProviderTaskCreateStartedForLease(fixture.task.ID, oldLease); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("lease_expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	newTask, err := svc.repo.ClaimNextTask(fixture.task.LeaseOwner, time.Minute)
	if err != nil || newTask == nil {
		t.Fatalf("reclaim task = %#v, error = %v", newTask, err)
	}
	if claimed, err := svc.repo.ClaimProviderTaskExecution(newTask.ID, newTask.LeaseOwner, newTask.LeaseToken); err != nil || !claimed {
		t.Fatalf("reclaimed provider execution = %t, %v", claimed, err)
	}
	creation, err := svc.repo.SaveProviderTaskCreationForLease(fixture.task.ID, oldLease, "provider-late-create", "trace-late-create")
	if err != nil {
		t.Fatal(err)
	}
	if !creation.HandedOff {
		t.Fatal("late create response was not marked as a generation handoff")
	}
	newLease := repository.ProviderTaskLease{Owner: newTask.LeaseOwner, Token: newTask.LeaseToken}
	if err := svc.repo.MarkProviderTaskCreateStartedForLease(fixture.task.ID, newLease); err == nil {
		t.Fatal("new generation attempted a second create after handoff")
	}
	if err := svc.repo.UpdateProviderTaskPollForLease(fixture.task.ID, newLease, "running", "trace-current-running"); err != nil {
		t.Fatal(err)
	}
	stored, err := svc.repo.ProviderTaskFact(fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ProviderTaskID != "provider-late-create" || stored.ProviderStatus != "running" || stored.CreateLeaseToken != oldLease.Token || stored.ExecutionLeaseToken != newLease.Token {
		t.Fatalf("late create handoff facts = %#v", stored)
	}
	decision, err := svc.repo.DecideProviderCreateUncertain(fixture.task.ID, newLease, "", "handoff observed after execution snapshot")
	if err != nil || decision != repository.ProviderCreateUncertainRequeued {
		t.Fatalf("create handoff decision = %q, %v", decision, err)
	}
	requeued, err := svc.repo.Task(fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if requeued.Status != model.TaskStatusQueued || requeued.LeaseToken != "" || requeued.LeaseOwner != "" {
		t.Fatalf("handoff requeue task = %#v", requeued)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("next_poll_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	polledTask, err := svc.repo.ClaimNextTask(fixture.task.LeaseOwner, time.Minute)
	if err != nil || polledTask == nil {
		t.Fatalf("claim handed-off provider task = %#v, %v", polledTask, err)
	}
	if claimed, err := svc.repo.ClaimProviderTaskExecution(polledTask.ID, polledTask.LeaseOwner, polledTask.LeaseToken); err != nil || !claimed {
		t.Fatalf("claim handed-off provider execution = %t, %v", claimed, err)
	}
	finalFact, err := svc.repo.ProviderTaskFact(fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finalFact.ProviderTaskID != "provider-late-create" || finalFact.ProviderStatus != "running" || finalFact.ExecutionLeaseToken != polledTask.LeaseToken {
		t.Fatalf("handed-off provider task was not preserved for polling = %#v", finalFact)
	}
	if err := svc.repo.MarkProviderTaskCreateStartedForLease(fixture.task.ID, repository.ProviderTaskLease{Owner: polledTask.LeaseOwner, Token: polledTask.LeaseToken}); err == nil {
		t.Fatal("handed-off provider task allowed a second create after requeue")
	}
}

func TestProcessClaimedTaskAtomicallyRequeuesLateCreateHandoffWithoutManualReview(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 96, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	oldLease := repository.ProviderTaskLease{Owner: fixture.task.LeaseOwner, Token: fixture.task.LeaseToken}
	if claimed, err := svc.repo.ClaimProviderTaskExecution(fixture.task.ID, oldLease.Owner, oldLease.Token); err != nil || !claimed {
		t.Fatalf("old provider execution claim = %t, %v", claimed, err)
	}
	if err := svc.repo.MarkProviderTaskCreateStartedForLease(fixture.task.ID, oldLease); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("lease_expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	current, err := svc.repo.ClaimNextTask(svc.workerID, time.Minute)
	if err != nil || current == nil {
		t.Fatalf("new task generation = %#v, %v", current, err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("late create recovery sent a second upstream request")
	}))
	defer server.Close()
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	if err := db.Model(&model.ProviderEndpointVersion{}).Where("id = ?", "endpoint-v1").Update("base_url", server.URL).Error; err != nil {
		t.Fatal(err)
	}

	firstServiceDecisionRead := make(chan struct{})
	releaseServiceDecision := make(chan struct{})
	var factReads atomic.Int64
	const callbackName = "test:late-create-after-service-decision-read"
	if err := db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*model.ProviderTaskFact); !ok {
			return
		}
		if factReads.Add(1) != 6 {
			return
		}
		close(firstServiceDecisionRead)
		<-releaseServiceDecision
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	processDone := make(chan error, 1)
	go func() { processDone <- svc.processClaimedTask(current) }()
	select {
	case <-firstServiceDecisionRead:
	case <-time.After(5 * time.Second):
		close(releaseServiceDecision)
		t.Fatalf("processClaimedTask did not reach create-uncertain decision read; fact reads=%d", factReads.Load())
	}
	write, err := svc.repo.SaveProviderTaskCreationForLease(fixture.task.ID, oldLease, "provider-late-process", "trace-late-process")
	if err != nil || !write.HandedOff {
		close(releaseServiceDecision)
		t.Fatalf("late create handoff = %#v, %v", write, err)
	}
	close(releaseServiceDecision)
	select {
	case <-processDone:
	case <-time.After(5 * time.Second):
		t.Fatal("processClaimedTask did not finish after late create handoff")
	}

	storedTask, _ := svc.repo.Task(fixture.task.ID)
	storedFact, _ := svc.repo.ProviderTaskFact(fixture.task.ID)
	storedOrder, _ := svc.repo.BillingOrder(fixture.order.ID)
	if storedTask.Status != model.TaskStatusQueued || storedFact.ProviderTaskID != "provider-late-process" || storedFact.ProviderStatus != "submitted" || storedFact.ReconciliationStatus != "pending" || storedOrder.Status != model.BillingStatusRunning {
		t.Fatalf("late create service decision: task=%#v fact=%#v order=%#v", storedTask, storedFact, storedOrder)
	}
}

func TestLateProviderCreateResponseCreatesResolvedObservationAfterAdministrativeResolution(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 100, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	oldLease := repository.ProviderTaskLease{Owner: fixture.task.LeaseOwner, Token: fixture.task.LeaseToken}
	if claimed, err := svc.repo.ClaimProviderTaskExecution(fixture.task.ID, oldLease.Owner, oldLease.Token); err != nil || !claimed {
		t.Fatalf("provider execution claim = %t, %v", claimed, err)
	}
	if err := svc.repo.MarkProviderTaskCreateStartedForLease(fixture.task.ID, oldLease); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.FinalizeProviderTaskUncertain(fixture.task.ID, oldLease, repository.ProviderTaskFailureResolution{
		ExpectedStatuses: []string{"creating"}, ProviderStatus: "create_uncertain", ReconciliationStatus: "manual_review", TaskStage: "任务失败", TaskError: "create uncertain",
	}); err != nil {
		t.Fatal(err)
	}
	resolved, err := svc.repo.RefundProviderTaskBilling(fixture.order.ID, repository.ProviderTaskBillingResolution{
		ExpectedProviderStatus: "create_uncertain", ExpectedReconciliationStatus: "manual_review", ExpectedBillingStatus: model.BillingStatusUncertain,
		ResolvedProviderStatus: "create_uncertain_resolved", ActorUserID: "admin", Note: "resolved before late response",
		TaskStatus: model.TaskStatusFailed, TaskStage: "resolved", TaskError: "resolved",
	})
	if err != nil || !resolved {
		t.Fatalf("administrative resolution = %t, %v", resolved, err)
	}
	write, err := svc.repo.SaveProviderTaskCreationForLease(fixture.task.ID, oldLease, "provider-observed-after-resolution", "trace-observed")
	if err != nil || !write.HandedOff {
		t.Fatalf("late resolved observation write = %#v, %v", write, err)
	}
	storedFact, _ := svc.repo.ProviderTaskFact(fixture.task.ID)
	storedTask, _ := svc.repo.Task(fixture.task.ID)
	storedOrder, _ := svc.repo.BillingOrder(fixture.order.ID)
	if storedFact.ProviderTaskID != "provider-observed-after-resolution" || storedFact.ProviderStatus != "resolved_observation_submitted" || storedFact.ReconciliationStatus != "resolved" || storedTask.Status != model.TaskStatusQueued || storedTask.LeaseToken != "" || storedOrder.Status != model.BillingStatusRefunded || storedOrder.ResolvedBy != "admin" || storedOrder.ResolutionNote != "resolved before late response" {
		t.Fatalf("late create did not establish durable resolved observation: fact=%#v task=%#v billing=%#v", storedFact, storedTask, storedOrder)
	}
	payload := []byte("late-resolved-observation-video")
	assetServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "video/mp4")
		_, _ = writer.Write(payload)
	}))
	defer assetServer.Close()
	originalOutboundTransport := externalBinaryTransport
	assetTransport := externalBinaryTransport.Clone()
	assetTransport.TLSClientConfig = assetServer.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	assetTransport.DialContext = func(ctx context.Context, network string, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, assetServer.Listener.Addr().String())
	}
	externalBinaryTransport = assetTransport
	t.Cleanup(func() { externalBinaryTransport = originalOutboundTransport })
	assetURL := strings.Replace(assetServer.URL, "127.0.0.1", "example.com", 1) + "/result.mp4"
	var creates atomic.Int64
	var polls atomic.Int64
	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == kuaiziSeedance25CreatePath {
			creates.Add(1)
			http.Error(writer, "second create forbidden", http.StatusInternalServerError)
			return
		}
		if request.URL.Path != kuaiziSeedance25StatusPath {
			http.NotFound(writer, request)
			return
		}
		pollNumber := polls.Add(1)
		if pollNumber == 1 {
			http.Error(writer, "transient observation timeout", http.StatusGatewayTimeout)
			return
		}
		if pollNumber == 2 {
			_, _ = io.WriteString(writer, `{"code":0,"message":"","data":{"task_id":"provider-observed-after-resolution","status":"pending"},"trace_id":"trace-observation-pending"}`)
			return
		}
		_, _ = io.WriteString(writer, `{"code":0,"message":"","data":{"task_id":"provider-observed-after-resolution","status":"succeeded","video_url":"`+assetURL+`","last_frame_url":"https://cdn.example/last.png","duration":8,"total_tokens":"42"},"trace_id":"trace-observation-success"}`)
	}))
	defer providerServer.Close()
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	if err := db.Model(&model.ProviderEndpointVersion{}).Where("id = ?", "endpoint-v1").Update("base_url", providerServer.URL).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("input_json", `{"broken resolved observation input"`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("next_poll_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	observedTask, err := svc.repo.ClaimNextTask(svc.workerID, time.Minute)
	if err != nil || observedTask == nil {
		t.Fatalf("claim resolved observation = %#v, %v", observedTask, err)
	}
	if err := svc.processClaimedTask(observedTask); err == nil {
		t.Fatal("transient resolved observation poll unexpectedly succeeded")
	}
	transientFact, _ := svc.repo.ProviderTaskFact(fixture.task.ID)
	transientTask, _ := svc.repo.Task(fixture.task.ID)
	if transientFact.ProviderStatus != "resolved_observation_poll_uncertain" || transientTask.Status != model.TaskStatusQueued {
		t.Fatalf("transient observation did not requeue: fact=%#v task=%#v", transientFact, transientTask)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("next_poll_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	observedTask, err = svc.repo.ClaimNextTask(svc.workerID, time.Minute)
	if err != nil || observedTask == nil {
		t.Fatalf("reclaim resolved observation = %#v, %v", observedTask, err)
	}
	if err := svc.processClaimedTask(observedTask); err != nil {
		t.Fatalf("process recovered resolved observation = %v", err)
	}
	finalFact, _ := svc.repo.ProviderTaskFact(fixture.task.ID)
	finalTask, _ := svc.repo.Task(fixture.task.ID)
	finalOrder, _ := svc.repo.BillingOrder(fixture.order.ID)
	finalResource, _ := svc.repo.ResourceForSourceTask(fixture.task.UserID, fixture.task.ID)
	if creates.Load() != 0 || polls.Load() != 3 || finalFact.ProviderStatus != "resolved_observation_succeeded" || finalFact.ReconciliationStatus != "resolved" || finalTask.Status != model.TaskStatusFailed || finalResource.Status != model.ResourceStatusReady || finalOrder.Status != model.BillingStatusRefunded || finalOrder.ResolvedBy != "admin" || finalOrder.ResolutionNote != "resolved before late response" {
		t.Fatalf("resolved observation did not converge: creates=%d polls=%d fact=%#v task=%#v resource=%#v order=%#v", creates.Load(), polls.Load(), finalFact, finalTask, finalResource, finalOrder)
	}
}

func TestResolvedProviderObservationFailureRestoresAdministrativeTerminalWithoutChangingBilling(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 100, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	oldLease := repository.ProviderTaskLease{Owner: fixture.task.LeaseOwner, Token: fixture.task.LeaseToken}
	if claimed, err := svc.repo.ClaimProviderTaskExecution(fixture.task.ID, oldLease.Owner, oldLease.Token); err != nil || !claimed {
		t.Fatalf("provider execution claim = %t, %v", claimed, err)
	}
	if err := svc.repo.MarkProviderTaskCreateStartedForLease(fixture.task.ID, oldLease); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.FinalizeProviderTaskUncertain(fixture.task.ID, oldLease, repository.ProviderTaskFailureResolution{
		ExpectedStatuses: []string{"creating"}, ProviderStatus: "create_uncertain", ReconciliationStatus: "manual_review", TaskStage: "任务失败", TaskError: "create uncertain",
	}); err != nil {
		t.Fatal(err)
	}
	resolved, err := svc.repo.RefundProviderTaskBilling(fixture.order.ID, repository.ProviderTaskBillingResolution{
		ExpectedProviderStatus: "create_uncertain", ExpectedReconciliationStatus: "manual_review", ExpectedBillingStatus: model.BillingStatusUncertain,
		ResolvedProviderStatus: "create_uncertain_resolved", ActorUserID: "admin", Note: "keep administrative refund",
		TaskStatus: model.TaskStatusFailed, TaskStage: "resolved", TaskError: "resolved",
	})
	if err != nil || !resolved {
		t.Fatalf("administrative resolution = %t, %v", resolved, err)
	}
	if _, err := svc.repo.SaveProviderTaskCreationForLease(fixture.task.ID, oldLease, "provider-observation-failed", "trace-create-observation-failed"); err != nil {
		t.Fatal(err)
	}
	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == kuaiziSeedance25CreatePath {
			t.Fatal("resolved observation attempted a second create")
		}
		_, _ = io.WriteString(writer, `{"code":0,"message":"","data":{"task_id":"provider-observation-failed","status":"failed","error":"controlled observation failure"},"trace_id":"trace-observation-failed"}`)
	}))
	defer providerServer.Close()
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	if err := db.Model(&model.ProviderEndpointVersion{}).Where("id = ?", "endpoint-v1").Update("base_url", providerServer.URL).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("next_poll_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	observedTask, err := svc.repo.ClaimNextTask(svc.workerID, time.Minute)
	if err != nil || observedTask == nil {
		t.Fatalf("claim resolved failure observation = %#v, %v", observedTask, err)
	}
	if err := svc.processClaimedTask(observedTask); err != nil {
		t.Fatalf("process resolved failure observation = %v", err)
	}
	finalFact, _ := svc.repo.ProviderTaskFact(fixture.task.ID)
	finalTask, _ := svc.repo.Task(fixture.task.ID)
	finalOrder, _ := svc.repo.BillingOrder(fixture.order.ID)
	if finalFact.ProviderStatus != "resolved_observation_failed" || finalFact.LastPollTraceID != "trace-observation-failed" || finalFact.ReconciliationStatus != "resolved" || finalTask.Status != model.TaskStatusFailed || finalOrder.Status != model.BillingStatusRefunded || finalOrder.ResolvedBy != "admin" || finalOrder.ResolutionNote != "keep administrative refund" {
		t.Fatalf("resolved failure observation did not preserve commercial decision: fact=%#v task=%#v order=%#v", finalFact, finalTask, finalOrder)
	}
}

func TestResolvedProviderObservationPollUncertainReturnsToNonterminalProviderStatus(t *testing.T) {
	for _, providerStatus := range []string{"submitted", "pending"} {
		t.Run(providerStatus, func(t *testing.T) {
			svc, db := openProviderCredentialService(t)
			fixture := saveProviderRuntimeFixture(t, svc, db)
			if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 100, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
				t.Fatal(err)
			}
			createLease := repository.ProviderTaskLease{Owner: fixture.task.LeaseOwner, Token: fixture.task.LeaseToken}
			if claimed, err := svc.repo.ClaimProviderTaskExecution(fixture.task.ID, createLease.Owner, createLease.Token); err != nil || !claimed {
				t.Fatalf("provider execution claim = %t, %v", claimed, err)
			}
			if err := svc.repo.MarkProviderTaskCreateStartedForLease(fixture.task.ID, createLease); err != nil {
				t.Fatal(err)
			}
			if err := svc.repo.FinalizeProviderTaskUncertain(fixture.task.ID, createLease, repository.ProviderTaskFailureResolution{
				ExpectedStatuses: []string{"creating"}, ProviderStatus: "create_uncertain", ReconciliationStatus: "manual_review", TaskStage: "任务失败", TaskError: "create uncertain",
			}); err != nil {
				t.Fatal(err)
			}
			resolved, err := svc.repo.RefundProviderTaskBilling(fixture.order.ID, repository.ProviderTaskBillingResolution{
				ExpectedProviderStatus: "create_uncertain", ExpectedReconciliationStatus: "manual_review", ExpectedBillingStatus: model.BillingStatusUncertain,
				ResolvedProviderStatus: "create_uncertain_resolved", ActorUserID: "admin", Note: "observe after transient poll",
				TaskStatus: model.TaskStatusFailed, TaskStage: "resolved", TaskError: "resolved",
			})
			if err != nil || !resolved {
				t.Fatalf("administrative resolution = %t, %v", resolved, err)
			}
			if _, err := svc.repo.SaveProviderTaskCreationForLease(fixture.task.ID, createLease, "provider-observation-retry", "trace-create"); err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("next_poll_at", time.Now().Add(-time.Second)).Error; err != nil {
				t.Fatal(err)
			}
			firstPoll, err := svc.repo.ClaimNextTask(svc.workerID, time.Minute)
			if err != nil || firstPoll == nil {
				t.Fatalf("first observation poll claim = %#v, %v", firstPoll, err)
			}
			if claimed, err := svc.repo.ClaimProviderTaskExecution(firstPoll.ID, firstPoll.LeaseOwner, firstPoll.LeaseToken); err != nil || !claimed {
				t.Fatalf("first observation execution claim = %t, %v", claimed, err)
			}
			if err := svc.repo.RequeueResolvedProviderObservation(firstPoll.ID, repository.ProviderTaskLease{Owner: firstPoll.LeaseOwner, Token: firstPoll.LeaseToken}, "trace-timeout", "transient timeout"); err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("next_poll_at", time.Now().Add(-time.Second)).Error; err != nil {
				t.Fatal(err)
			}
			secondPoll, err := svc.repo.ClaimNextTask(svc.workerID, time.Minute)
			if err != nil || secondPoll == nil {
				t.Fatalf("second observation poll claim = %#v, %v", secondPoll, err)
			}
			if claimed, err := svc.repo.ClaimProviderTaskExecution(secondPoll.ID, secondPoll.LeaseOwner, secondPoll.LeaseToken); err != nil || !claimed {
				t.Fatalf("second observation execution claim = %t, %v", claimed, err)
			}
			secondLease := repository.ProviderTaskLease{Owner: secondPoll.LeaseOwner, Token: secondPoll.LeaseToken}
			if err := svc.repo.UpdateProviderTaskPollForLease(secondPoll.ID, secondLease, providerStatus, "trace-recovered"); err != nil {
				t.Fatalf("poll_uncertain -> %s = %v", providerStatus, err)
			}
			storedFact, err := svc.repo.ProviderTaskFact(fixture.task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if storedFact.ProviderStatus != "resolved_observation_"+providerStatus || storedFact.ReconciliationStatus != "resolved" || storedFact.ExecutionLeaseToken != secondPoll.LeaseToken {
				t.Fatalf("recovered observation fact = %#v", storedFact)
			}
		})
	}
}

func TestAdministrativeResolutionAdoptsLateCreateRecordedDuringManualReview(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 100, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	createLease := repository.ProviderTaskLease{Owner: fixture.task.LeaseOwner, Token: fixture.task.LeaseToken}
	if claimed, err := svc.repo.ClaimProviderTaskExecution(fixture.task.ID, createLease.Owner, createLease.Token); err != nil || !claimed {
		t.Fatalf("provider execution claim = %t, %v", claimed, err)
	}
	if err := svc.repo.MarkProviderTaskCreateStartedForLease(fixture.task.ID, createLease); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.FinalizeProviderTaskUncertain(fixture.task.ID, createLease, repository.ProviderTaskFailureResolution{
		ExpectedStatuses: []string{"creating"}, ProviderStatus: "create_uncertain", ReconciliationStatus: "manual_review", TaskStage: "任务失败", TaskError: "create uncertain",
	}); err != nil {
		t.Fatal(err)
	}
	write, err := svc.repo.SaveProviderTaskCreationForLease(fixture.task.ID, createLease, "provider-before-admin-resolution", "trace-before-admin")
	if err != nil || !write.HandedOff || write.ResolvedObservation {
		t.Fatalf("manual-review late create write = %#v, %v", write, err)
	}
	resolved, err := svc.repo.RefundProviderTaskBilling(fixture.order.ID, repository.ProviderTaskBillingResolution{
		ExpectedProviderStatus: "create_uncertain", ExpectedReconciliationStatus: "manual_review", ExpectedBillingStatus: model.BillingStatusUncertain,
		ResolvedProviderStatus: "create_uncertain_resolved", ActorUserID: "admin", Note: "adopt late create after manual review",
		TaskStatus: model.TaskStatusFailed, TaskStage: "resolved", TaskError: "resolved",
	})
	if err != nil || !resolved {
		t.Fatalf("administrative resolution = %t, %v", resolved, err)
	}
	storedFact, _ := svc.repo.ProviderTaskFact(fixture.task.ID)
	storedTask, _ := svc.repo.Task(fixture.task.ID)
	storedOrder, _ := svc.repo.BillingOrder(fixture.order.ID)
	if storedFact.ProviderTaskID != "provider-before-admin-resolution" || storedFact.ProviderStatus != "resolved_observation_submitted" || storedFact.ReconciliationStatus != "resolved" || storedTask.Status != model.TaskStatusQueued || storedTask.LeaseToken != "" || storedOrder.Status != model.BillingStatusRefunded || storedOrder.ResolvedBy != "admin" {
		t.Fatalf("administrative resolution orphaned late create: fact=%#v task=%#v billing=%#v", storedFact, storedTask, storedOrder)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("next_poll_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	observedTask, err := svc.repo.ClaimNextTask(svc.workerID, time.Minute)
	if err != nil || observedTask == nil {
		t.Fatalf("claim adopted resolved observation = %#v, %v", observedTask, err)
	}
	if claimed, err := svc.repo.ClaimProviderTaskExecution(observedTask.ID, observedTask.LeaseOwner, observedTask.LeaseToken); err != nil || !claimed {
		t.Fatalf("claim adopted observation execution = %t, %v", claimed, err)
	}
	if err := svc.repo.MarkProviderTaskCreateStartedForLease(observedTask.ID, repository.ProviderTaskLease{Owner: observedTask.LeaseOwner, Token: observedTask.LeaseToken}); err == nil {
		t.Fatal("adopted late create allowed a second provider create")
	}
}

func TestSourceTaskResourceWriteUsesFencedClaim(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	fixture := saveProviderRuntimeFixture(t, svc, db)
	if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Update("provider_status", "succeeded").Error; err != nil {
		t.Fatal(err)
	}
	if claimed, err := svc.repo.ClaimProviderTaskExecution(fixture.task.ID, fixture.task.LeaseOwner, fixture.task.LeaseToken); err != nil || !claimed {
		t.Fatalf("initial resource execution claim = %t, %v", claimed, err)
	}
	resource := model.Resource{
		ID: "resource-fenced", UserID: fixture.task.UserID, SourceTaskID: fixture.task.ID,
		Kind: "video", Status: model.ResourceStatusPending, Provider: "local", ObjectKey: "pending/result.mp4",
		MimeType: "video/mp4", Size: 4, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	first, err := svc.repo.ClaimSourceTaskResourceWriteWithQuota(fixture.task.UserID, fixture.task.ID, fixture.task.LeaseOwner, fixture.task.LeaseToken, "write-one", "pending/result-write-one.mp4", time.Minute, "2026-08-12", 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repo.ClaimSourceTaskResourceWriteWithQuota(fixture.task.UserID, fixture.task.ID, fixture.task.LeaseOwner, fixture.task.LeaseToken, "write-two", "pending/result-write-two.mp4", time.Minute, "2026-08-12", 1<<30); err == nil {
		t.Fatal("second writer claimed an unexpired resource write")
	}
	newTaskLeaseToken := "new-task-generation"
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("lease_token", newTaskLeaseToken).Error; err != nil {
		t.Fatal(err)
	}
	if claimed, err := svc.repo.ClaimProviderTaskExecution(fixture.task.ID, fixture.task.LeaseOwner, newTaskLeaseToken); err != nil || !claimed {
		t.Fatalf("replacement resource execution claim = %t, %v", claimed, err)
	}
	second, err := svc.repo.ClaimSourceTaskResourceWriteWithQuota(fixture.task.UserID, fixture.task.ID, fixture.task.LeaseOwner, newTaskLeaseToken, "write-two", "pending/result-write-two.mp4", time.Minute, "2026-08-12", 1<<30)
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
	if claimed, err := svc.repo.ClaimProviderTaskExecution(fixture.task.ID, fixture.task.LeaseOwner, fixture.task.LeaseToken); err != nil || !claimed {
		t.Fatalf("provider execution claim = %t, %v", claimed, err)
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
	if claimed, err := svc.repo.ClaimProviderTaskExecution(runtimeFixture.task.ID, runtimeFixture.task.LeaseOwner, runtimeFixture.task.LeaseToken); err != nil || !claimed {
		t.Fatalf("provider execution claim = %t, %v", claimed, err)
	}
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
	if claimed, err := svc.repo.ClaimProviderTaskExecution(fixture.task.ID, fixture.task.LeaseOwner, fixture.task.LeaseToken); err != nil || !claimed {
		t.Fatalf("provider execution claim = %t, %v", claimed, err)
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
		errorsByPath <- recoveryRepo.FinalizeProviderTaskRefund(fixture.task.ID, repository.ProviderTaskLease{Owner: fixture.task.LeaseOwner, Token: fixture.task.LeaseToken}, repository.ProviderTaskFailureResolution{
			ExpectedStatuses: []string{"execution_claimed"}, ProviderStatus: "execution_failed", ReconciliationStatus: "resolved", TaskStage: "任务失败", TaskError: "request not sent",
		})
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
			if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Updates(map[string]any{
				"provider_status": "create_uncertain", "reconciliation_status": "pending", "execution_lease_token": fixture.task.LeaseToken,
			}).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.BillingOrder{}).Where("id = ?", fixture.order.ID).Update("status", model.BillingStatusUncertain).Error; err != nil {
				t.Fatal(err)
			}
			recover := func() error {
				err := repository.New(db.Session(&gorm.Session{NewDB: true})).FinalizeProviderTaskUncertain(fixture.task.ID, repository.ProviderTaskLease{Owner: fixture.task.LeaseOwner, Token: fixture.task.LeaseToken}, repository.ProviderTaskFailureResolution{
					ExpectedStatuses: []string{"create_uncertain"}, ProviderStatus: "create_uncertain", ReconciliationStatus: "manual_review", TaskStage: "任务失败", TaskError: "controlled uncertainty",
				})
				if err != nil {
					fact, fetchErr := svc.repo.ProviderTaskFact(fixture.task.ID)
					if fetchErr == nil && fact.ReconciliationStatus == "resolved" {
						return nil
					}
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
	if claimed, err := second.ClaimProviderTaskExecution(reclaimed.ID, reclaimed.LeaseOwner, reclaimed.LeaseToken); err != nil || !claimed {
		t.Fatalf("first reclaimed execution = %t, %v", claimed, err)
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
	claimedFirst, err := first.ClaimSourceTaskResourceWriteWithQuota(resource.UserID, fixture.task.ID, reclaimed.LeaseOwner, reclaimed.LeaseToken, "pg-write-one", "pending/result-one.mp4", time.Minute, "2026-08-12", 1<<30)
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
	if claimed, err := second.ClaimProviderTaskExecution(secondGeneration.ID, secondGeneration.LeaseOwner, secondGeneration.LeaseToken); err != nil || !claimed {
		t.Fatalf("second reclaimed execution = %t, %v", claimed, err)
	}
	claimedSecond, err := second.ClaimSourceTaskResourceWriteWithQuota(resource.UserID, fixture.task.ID, secondGeneration.LeaseOwner, secondGeneration.LeaseToken, "pg-write-two", "pending/result-two.mp4", time.Minute, "2026-08-12", 1<<30)
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

func TestPostgresProviderLatePollCannotRegressNewGenerationSuccess(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	svc := New(repository.New(db.Session(&gorm.Session{NewDB: true})), t.TempDir())
	fixture := saveProviderRuntimeFixture(t, svc, db)
	oldLease := repository.ProviderTaskLease{Owner: fixture.task.LeaseOwner, Token: fixture.task.LeaseToken}
	if err := db.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Updates(map[string]any{
		"provider_status": "running", "provider_task_id": "provider-pg-generation", "execution_lease_token": oldLease.Token,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("lease_expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	secondRepo := repository.New(db.Session(&gorm.Session{NewDB: true}))
	newTask, err := secondRepo.ClaimNextTask(oldLease.Owner, time.Minute)
	if err != nil || newTask == nil || newTask.LeaseToken == oldLease.Token {
		t.Fatalf("new PostgreSQL generation = %#v, %v", newTask, err)
	}
	if claimed, err := secondRepo.ClaimProviderTaskExecution(newTask.ID, newTask.LeaseOwner, newTask.LeaseToken); err != nil || !claimed {
		t.Fatalf("new PostgreSQL execution claim = %t, %v", claimed, err)
	}
	firstRepo := repository.New(db.Session(&gorm.Session{NewDB: true}))
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		results <- firstRepo.UpdateProviderTaskPollForLease(fixture.task.ID, oldLease, "running", "trace-late-pg")
	}()
	go func() {
		<-start
		results <- secondRepo.SaveProviderTaskSuccessForLease(fixture.task.ID, repository.ProviderTaskLease{Owner: newTask.LeaseOwner, Token: newTask.LeaseToken}, "trace-success-pg", "https://cdn.example/result.mp4", "https://cdn.example/last.png", 8, "42")
	}()
	close(start)
	var failures int
	for range 2 {
		if err := <-results; err != nil {
			failures++
		}
	}
	if failures != 1 {
		t.Fatalf("PostgreSQL late-poll race failures = %d, want stale generation only", failures)
	}
	stored, err := secondRepo.ProviderTaskFact(fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ProviderStatus != "succeeded" || stored.LastPollTraceID != "trace-success-pg" || stored.AssetSourceURL == "" {
		t.Fatalf("PostgreSQL late poll regressed success: %#v", stored)
	}
}

func TestPostgresProviderLateCreateHandsOffAfterReclaimWithoutSecondCreate(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	svc := New(repository.New(db.Session(&gorm.Session{NewDB: true})), t.TempDir())
	fixture := saveProviderRuntimeFixture(t, svc, db)
	oldLease := repository.ProviderTaskLease{Owner: fixture.task.LeaseOwner, Token: fixture.task.LeaseToken}
	if claimed, err := svc.repo.ClaimProviderTaskExecution(fixture.task.ID, oldLease.Owner, oldLease.Token); err != nil || !claimed {
		t.Fatalf("old PostgreSQL execution claim = %t, %v", claimed, err)
	}
	if err := svc.repo.MarkProviderTaskCreateStartedForLease(fixture.task.ID, oldLease); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("lease_expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	secondRepo := repository.New(db.Session(&gorm.Session{NewDB: true}))
	newTask, err := secondRepo.ClaimNextTask(oldLease.Owner, time.Minute)
	if err != nil || newTask == nil || newTask.LeaseToken == oldLease.Token {
		t.Fatalf("new PostgreSQL create generation = %#v, %v", newTask, err)
	}
	if claimed, err := secondRepo.ClaimProviderTaskExecution(newTask.ID, newTask.LeaseOwner, newTask.LeaseToken); err != nil || !claimed {
		t.Fatalf("new PostgreSQL create execution claim = %t, %v", claimed, err)
	}
	firstRepo := repository.New(db.Session(&gorm.Session{NewDB: true}))
	start := make(chan struct{})
	type createResult struct {
		write repository.ProviderTaskCreationWrite
		err   error
	}
	creation := make(chan createResult, 1)
	secondStart := make(chan error, 1)
	go func() {
		<-start
		write, err := firstRepo.SaveProviderTaskCreationForLease(fixture.task.ID, oldLease, "provider-pg-late-create", "trace-pg-late-create")
		creation <- createResult{write: write, err: err}
	}()
	go func() {
		<-start
		secondStart <- secondRepo.MarkProviderTaskCreateStartedForLease(fixture.task.ID, repository.ProviderTaskLease{Owner: newTask.LeaseOwner, Token: newTask.LeaseToken})
	}()
	close(start)
	created := <-creation
	if created.err != nil || !created.write.HandedOff {
		t.Fatalf("PostgreSQL late create handoff = %#v, %v", created.write, created.err)
	}
	if err := <-secondStart; err == nil {
		t.Fatal("PostgreSQL reclaimed generation obtained a second create transition")
	}
	stored, err := secondRepo.ProviderTaskFact(fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ProviderTaskID != "provider-pg-late-create" || stored.CreateLeaseToken != oldLease.Token || stored.ExecutionLeaseToken != newTask.LeaseToken || stored.ProviderStatus != "submitted" {
		t.Fatalf("PostgreSQL late create handoff facts = %#v", stored)
	}
}

func TestPostgresProviderCancelBeforeOutboundAtomicallyReleasesBillingAndCapacityAcrossConnections(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	first := db.Session(&gorm.Session{NewDB: true})
	second := db.Session(&gorm.Session{NewDB: true})
	svc := New(repository.New(first), t.TempDir())
	fixture := saveProviderRuntimeFixture(t, svc, first)
	if err := first.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 96, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	if claimed, err := svc.repo.ClaimProviderTaskExecution(fixture.task.ID, fixture.task.LeaseOwner, fixture.task.LeaseToken); err != nil || !claimed {
		t.Fatalf("provider execution claim = %t, %v", claimed, err)
	}
	if _, err := svc.CancelTask(fixture.task.UserID, fixture.task.ID); err != nil {
		t.Fatal(err)
	}

	storedTask, _ := repository.New(second).Task(fixture.task.ID)
	storedFact, _ := repository.New(second).ProviderTaskFact(fixture.task.ID)
	storedOrder, _ := repository.New(second).BillingOrder(fixture.order.ID)
	var account model.CreditAccount
	if err := second.First(&account, "user_id = ?", fixture.task.UserID).Error; err != nil {
		t.Fatal(err)
	}
	var refunds int64
	if err := second.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ? AND type = ?", fixture.order.ID, model.CreditLedgerRefund).Count(&refunds).Error; err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != model.TaskStatusCancelled || storedFact.ProviderStatus != "cancelled_before_create" || storedFact.ReconciliationStatus != "resolved" || storedOrder.Status != model.BillingStatusRefunded || account.AvailableMicrocredits != 100 || account.ReservedMicrocredits != 0 || refunds != 1 {
		t.Fatalf("PostgreSQL provider cancel: task=%#v fact=%#v order=%#v account=%#v refunds=%d", storedTask, storedFact, storedOrder, account, refunds)
	}
	secondTask := providerRuntimeTask("pg-task-after-provider-cancel", "pg-user-after-provider-cancel", "pg-worker-after-provider-cancel")
	secondTask.BillingOrderID = ""
	secondFact := providerRuntimeFact(secondTask.ID, "", "reserved")
	if err := second.Create(&secondTask).Error; err != nil {
		t.Fatal(err)
	}
	if err := second.Create(&secondFact).Error; err != nil {
		t.Fatal(err)
	}
	if claimed, err := repository.New(second).ClaimProviderTaskExecution(secondTask.ID, secondTask.LeaseOwner, secondTask.LeaseToken); err != nil || !claimed {
		t.Fatalf("PostgreSQL capacity after provider cancel = %t, %v", claimed, err)
	}
}

func TestPostgresProviderExpiredCancelConvergesBeforeAndAfterConcurrentReclaim(t *testing.T) {
	for _, test := range []struct {
		name       string
		claimFirst bool
	}{{name: "cancel fences expired generation first"}, {name: "claim changes generation before cancel CAS", claimFirst: true}} {
		t.Run(test.name, func(t *testing.T) {
			db := testsupport.OpenPaymentIntegrationPostgres(t)
			if err := database.MigrateSchema(db); err != nil {
				t.Fatal(err)
			}
			cancelDB := db.Session(&gorm.Session{NewDB: true})
			claimDB := db.Session(&gorm.Session{NewDB: true})
			cancelService := New(repository.New(cancelDB), t.TempDir())
			fixture := saveProviderRuntimeFixture(t, cancelService, cancelDB)
			if err := cancelDB.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 96, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
				t.Fatal(err)
			}
			if claimed, err := repository.New(cancelDB).ClaimProviderTaskExecution(fixture.task.ID, fixture.task.LeaseOwner, fixture.task.LeaseToken); err != nil || !claimed {
				t.Fatalf("provider execution claim = %t, %v", claimed, err)
			}
			if err := cancelDB.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("lease_expires_at", time.Now().Add(-time.Second)).Error; err != nil {
				t.Fatal(err)
			}

			var cancelled *model.Task
			var cancelErr error
			if !test.claimFirst {
				cancelled, cancelErr = cancelService.CancelTask(fixture.task.UserID, fixture.task.ID)
				if cancelErr != nil {
					t.Fatal(cancelErr)
				}
				if reclaimed, err := repository.New(claimDB).ClaimNextTask("pg-claim-after-cancel", time.Minute); err != nil || reclaimed != nil {
					t.Fatalf("claim after expired cancellation = %#v, %v", reclaimed, err)
				}
			} else {
				staleRead := make(chan struct{})
				releaseRead := make(chan struct{})
				var blockOnce sync.Once
				const callbackName = "test:pg-expired-cancel-stale-read"
				if err := cancelDB.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
					if _, ok := tx.Statement.Dest.(*model.Task); !ok {
						return
					}
					matchesTask := false
					matchesUser := false
					for _, value := range tx.Statement.Vars {
						text, ok := value.(string)
						if !ok {
							continue
						}
						matchesTask = matchesTask || text == fixture.task.ID
						matchesUser = matchesUser || text == fixture.task.UserID
					}
					if !matchesTask || !matchesUser {
						return
					}
					blockOnce.Do(func() {
						close(staleRead)
						<-releaseRead
					})
				}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = cancelDB.Callback().Query().Remove(callbackName) })
				cancelDone := make(chan struct {
					task *model.Task
					err  error
				}, 1)
				go func() {
					task, err := cancelService.CancelTask(fixture.task.UserID, fixture.task.ID)
					cancelDone <- struct {
						task *model.Task
						err  error
					}{task, err}
				}()
				select {
				case <-staleRead:
				case <-time.After(5 * time.Second):
					close(releaseRead)
					t.Fatal("cancel service did not pause after stale task read")
				}
				reclaimed, err := repository.New(claimDB).ClaimNextTask("pg-claim-winner", time.Minute)
				if err != nil || reclaimed == nil || reclaimed.LeaseToken == fixture.task.LeaseToken {
					close(releaseRead)
					t.Fatalf("concurrent reclaim = %#v, %v", reclaimed, err)
				}
				close(releaseRead)
				result := <-cancelDone
				cancelled, cancelErr = result.task, result.err
				if cancelErr != nil {
					t.Fatalf("cancel did not retry current generation: %v", cancelErr)
				}
				if _, err := repository.New(claimDB).ClaimProviderTaskExecution(reclaimed.ID, reclaimed.LeaseOwner, reclaimed.LeaseToken); err == nil {
					t.Fatal("reclaimed generation executed after cancellation fenced it")
				}
			}

			storedTask, _ := repository.New(claimDB).Task(fixture.task.ID)
			storedFact, _ := repository.New(claimDB).ProviderTaskFact(fixture.task.ID)
			storedOrder, _ := repository.New(claimDB).BillingOrder(fixture.order.ID)
			var account model.CreditAccount
			if err := claimDB.First(&account, "user_id = ?", fixture.task.UserID).Error; err != nil {
				t.Fatal(err)
			}
			var refunds int64
			if err := claimDB.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ? AND type = ?", fixture.order.ID, model.CreditLedgerRefund).Count(&refunds).Error; err != nil {
				t.Fatal(err)
			}
			if cancelled == nil || cancelled.Status != model.TaskStatusCancelled || storedTask.Status != model.TaskStatusCancelled || storedFact.ProviderStatus != "cancelled_before_create" || storedFact.ReconciliationStatus != "resolved" || storedOrder.Status != model.BillingStatusRefunded || account.AvailableMicrocredits != 100 || account.ReservedMicrocredits != 0 || refunds != 1 {
				t.Fatalf("expired cancellation convergence: returned=%#v task=%#v fact=%#v order=%#v account=%#v refunds=%d", cancelled, storedTask, storedFact, storedOrder, account, refunds)
			}
		})
	}
}

func TestPostgresProviderResolvedObservationSurvivesLateAdministrativeOrderAndTransientPoll(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	createDB := db.Session(&gorm.Session{NewDB: true})
	adminDB := db.Session(&gorm.Session{NewDB: true})
	createService := New(repository.New(createDB), t.TempDir())
	adminService := New(repository.New(adminDB), t.TempDir())
	fixture := saveProviderRuntimeFixture(t, createService, createDB)
	admin := model.User{ID: "pg-observation-order-admin", Username: "pg-observation-order-admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	owner := model.User{ID: fixture.task.UserID, Username: "pg-observation-order-owner", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := createDB.Create(&[]model.User{admin, owner}).Error; err != nil {
		t.Fatal(err)
	}
	if err := createDB.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 96, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	createLease := repository.ProviderTaskLease{Owner: fixture.task.LeaseOwner, Token: fixture.task.LeaseToken}
	if claimed, err := createService.repo.ClaimProviderTaskExecution(fixture.task.ID, createLease.Owner, createLease.Token); err != nil || !claimed {
		t.Fatalf("provider execution claim = %t, %v", claimed, err)
	}
	if err := createService.repo.MarkProviderTaskCreateStartedForLease(fixture.task.ID, createLease); err != nil {
		t.Fatal(err)
	}
	if err := createService.repo.FinalizeProviderTaskUncertain(fixture.task.ID, createLease, repository.ProviderTaskFailureResolution{
		ExpectedStatuses: []string{"creating"}, ProviderStatus: "create_uncertain", ReconciliationStatus: "manual_review", TaskStage: "任务失败", TaskError: "create uncertain",
	}); err != nil {
		t.Fatal(err)
	}
	write, err := createService.repo.SaveProviderTaskCreationForLease(fixture.task.ID, createLease, "provider-pg-observation-order", "trace-pg-late-create-before-admin")
	if err != nil || !write.HandedOff || write.ResolvedObservation {
		t.Fatalf("late create before admin resolution = %#v, %v", write, err)
	}
	if _, err := adminService.ResolveBillingOrder(&admin, fixture.order.ID, ResolveBillingRequest{Action: "refund", Note: "adopt late create before admin resolution"}); err != nil {
		t.Fatal(err)
	}
	if err := adminDB.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("next_poll_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	firstPoll, err := adminService.repo.ClaimNextTask("pg-observation-first-poll", time.Minute)
	if err != nil || firstPoll == nil {
		t.Fatalf("first observation claim = %#v, %v", firstPoll, err)
	}
	if claimed, err := adminService.repo.ClaimProviderTaskExecution(firstPoll.ID, firstPoll.LeaseOwner, firstPoll.LeaseToken); err != nil || !claimed {
		t.Fatalf("first observation execution = %t, %v", claimed, err)
	}
	if err := adminService.repo.RequeueResolvedProviderObservation(firstPoll.ID, repository.ProviderTaskLease{Owner: firstPoll.LeaseOwner, Token: firstPoll.LeaseToken}, "trace-pg-transient-timeout", "transient timeout"); err != nil {
		t.Fatal(err)
	}
	if err := createDB.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("next_poll_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	secondPoll, err := createService.repo.ClaimNextTask("pg-observation-second-poll", time.Minute)
	if err != nil || secondPoll == nil {
		t.Fatalf("second observation claim = %#v, %v", secondPoll, err)
	}
	if claimed, err := createService.repo.ClaimProviderTaskExecution(secondPoll.ID, secondPoll.LeaseOwner, secondPoll.LeaseToken); err != nil || !claimed {
		t.Fatalf("second observation execution = %t, %v", claimed, err)
	}
	secondLease := repository.ProviderTaskLease{Owner: secondPoll.LeaseOwner, Token: secondPoll.LeaseToken}
	if err := createService.repo.UpdateProviderTaskPollForLease(secondPoll.ID, secondLease, "pending", "trace-pg-provider-pending"); err != nil {
		t.Fatal(err)
	}
	storedTask, _ := adminService.repo.Task(fixture.task.ID)
	storedFact, _ := adminService.repo.ProviderTaskFact(fixture.task.ID)
	storedOrder, _ := adminService.repo.BillingOrder(fixture.order.ID)
	if storedTask.Status != model.TaskStatusRunning || storedFact.ProviderTaskID != "provider-pg-observation-order" || storedFact.ProviderStatus != "resolved_observation_pending" || storedFact.ReconciliationStatus != "resolved" || storedFact.ExecutionLeaseToken != secondPoll.LeaseToken || storedOrder.Status != model.BillingStatusRefunded || storedOrder.ResolvedBy != admin.ID {
		t.Fatalf("PostgreSQL resolved observation convergence: task=%#v fact=%#v billing=%#v", storedTask, storedFact, storedOrder)
	}
	capacityTask := providerRuntimeTask("task-pg-observation-capacity", "user-pg-observation-capacity", "worker-pg-observation-capacity")
	capacityTask.BillingOrderID = ""
	capacityFact := providerRuntimeFact(capacityTask.ID, "", "reserved")
	if err := adminDB.Create(&capacityTask).Error; err != nil {
		t.Fatal(err)
	}
	if err := adminDB.Create(&capacityFact).Error; err != nil {
		t.Fatal(err)
	}
	if claimed, err := adminService.repo.ClaimProviderTaskExecution(capacityTask.ID, capacityTask.LeaseOwner, capacityTask.LeaseToken); err != nil || claimed {
		t.Fatalf("active resolved observation capacity claim = %t, %v", claimed, err)
	}
}

func TestPostgresProviderResolvedLateCreateObservationPreservesAssetAndAdministrativeDecision(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	processDB := db.Session(&gorm.Session{NewDB: true})
	adminDB := db.Session(&gorm.Session{NewDB: true})
	processService := New(repository.New(processDB), t.TempDir())
	adminService := New(repository.New(adminDB), t.TempDir())
	fixture := saveProviderRuntimeFixture(t, processService, processDB)
	admin := model.User{ID: "pg-late-observation-admin", Username: "pg-late-observation-admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	owner := model.User{ID: fixture.task.UserID, Username: "pg-late-observation-owner", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := processDB.Create(&[]model.User{admin, owner}).Error; err != nil {
		t.Fatal(err)
	}
	if err := processDB.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 96, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	if err := processDB.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("lease_owner", processService.workerID).Error; err != nil {
		t.Fatal(err)
	}
	current, err := repository.New(processDB).Task(fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}

	payload := []byte("pg-resolved-late-create-video")
	assetServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "video/mp4")
		_, _ = writer.Write(payload)
	}))
	defer assetServer.Close()
	originalOutboundTransport := externalBinaryTransport
	assetTransport := externalBinaryTransport.Clone()
	assetTransport.TLSClientConfig = assetServer.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	assetTransport.DialContext = func(ctx context.Context, network string, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, assetServer.Listener.Addr().String())
	}
	externalBinaryTransport = assetTransport
	t.Cleanup(func() { externalBinaryTransport = originalOutboundTransport })
	assetURL := strings.Replace(assetServer.URL, "127.0.0.1", "example.com", 1) + "/result.mp4"
	createStarted := make(chan struct{})
	releaseCreate := make(chan struct{})
	var creates atomic.Int64
	var polls atomic.Int64
	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case kuaiziSeedance25CreatePath:
			creates.Add(1)
			close(createStarted)
			<-releaseCreate
			_, _ = io.WriteString(writer, `{"code":0,"message":"","data":{"task_id":"provider-pg-resolved-observation"},"trace_id":"trace-pg-late-create"}`)
		case kuaiziSeedance25StatusPath:
			polls.Add(1)
			_, _ = io.WriteString(writer, `{"code":0,"message":"","data":{"task_id":"provider-pg-resolved-observation","status":"succeeded","video_url":"`+assetURL+`","last_frame_url":"https://cdn.example/last.png","duration":8,"total_tokens":"42"},"trace_id":"trace-pg-observation-success"}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer providerServer.Close()
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	if err := processDB.Model(&model.ProviderEndpointVersion{}).Where("id = ?", "endpoint-v1").Update("base_url", providerServer.URL).Error; err != nil {
		t.Fatal(err)
	}

	processDone := make(chan error, 1)
	go func() { processDone <- processService.processClaimedTask(current) }()
	select {
	case <-createStarted:
	case <-time.After(5 * time.Second):
		close(releaseCreate)
		t.Fatal("provider create did not reach paused response")
	}
	if _, err := adminService.CancelTask(fixture.task.UserID, fixture.task.ID); err != nil {
		close(releaseCreate)
		t.Fatal(err)
	}
	if _, err := adminService.ResolveBillingOrder(&admin, fixture.order.ID, ResolveBillingRequest{Action: "refund", Note: "PG resolved before late create response"}); err != nil {
		close(releaseCreate)
		t.Fatal(err)
	}
	close(releaseCreate)
	select {
	case err := <-processDone:
		if err != nil {
			t.Fatalf("late create handoff process = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("late create handoff did not finish")
	}
	if err := adminDB.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("next_poll_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	observationTask, err := repository.New(processDB).ClaimNextTask(processService.workerID, time.Minute)
	if err != nil || observationTask == nil {
		t.Fatalf("claim late resolved observation = %#v, %v", observationTask, err)
	}
	if err := processService.processClaimedTask(observationTask); err != nil {
		t.Fatalf("process late resolved observation = %v", err)
	}

	storedTask, _ := repository.New(adminDB).Task(fixture.task.ID)
	storedFact, _ := repository.New(adminDB).ProviderTaskFact(fixture.task.ID)
	storedOrder, _ := repository.New(adminDB).BillingOrder(fixture.order.ID)
	storedResource, _ := repository.New(adminDB).ResourceForSourceTask(fixture.task.UserID, fixture.task.ID)
	var observationLogs int64
	if err := adminDB.Model(&model.TaskLog{}).Where("task_id = ? AND message LIKE ?", fixture.task.ID, "%持续观察%").Count(&observationLogs).Error; err != nil {
		t.Fatal(err)
	}
	if creates.Load() != 1 || polls.Load() != 1 || storedTask.Status != model.TaskStatusFailed || storedFact.ProviderTaskID != "provider-pg-resolved-observation" || storedFact.ProviderStatus != "resolved_observation_succeeded" || storedFact.ReconciliationStatus != "resolved" || storedOrder.Status != model.BillingStatusRefunded || storedOrder.ResolvedBy != admin.ID || storedOrder.ResolutionNote != "PG resolved before late create response" || storedResource.Status != model.ResourceStatusReady || observationLogs == 0 {
		t.Fatalf("PostgreSQL late resolved observation: creates=%d polls=%d task=%#v fact=%#v order=%#v resource=%#v logs=%d", creates.Load(), polls.Load(), storedTask, storedFact, storedOrder, storedResource, observationLogs)
	}
	secondTask := providerRuntimeTask("pg-task-after-resolved-observation", "pg-user-after-resolved-observation", "pg-worker-after-resolved-observation")
	secondTask.BillingOrderID = ""
	secondFact := providerRuntimeFact(secondTask.ID, "", "reserved")
	if err := adminDB.Create(&secondTask).Error; err != nil {
		t.Fatal(err)
	}
	if err := adminDB.Create(&secondFact).Error; err != nil {
		t.Fatal(err)
	}
	if claimed, err := repository.New(adminDB).ClaimProviderTaskExecution(secondTask.ID, secondTask.LeaseOwner, secondTask.LeaseToken); err != nil || !claimed {
		t.Fatalf("capacity after resolved observation = %t, %v", claimed, err)
	}
}

func TestPostgresProviderQuotaWriteClaimSerializesAdminResolutionAcrossConnections(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	claimDB := db.Session(&gorm.Session{NewDB: true})
	adminDB := db.Session(&gorm.Session{NewDB: true})
	claimService := New(repository.New(claimDB), t.TempDir())
	adminService := New(repository.New(adminDB), t.TempDir())
	fixture := saveProviderRuntimeFixture(t, claimService, claimDB)
	admin := model.User{ID: "pg-resource-admin", Username: "pg-resource-admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	owner := model.User{ID: fixture.task.UserID, Username: "pg-resource-owner", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := claimDB.Create(&[]model.User{admin, owner}).Error; err != nil {
		t.Fatal(err)
	}
	if err := claimDB.Create(&model.CreditAccount{UserID: fixture.task.UserID, AvailableMicrocredits: 96, ReservedMicrocredits: fixture.order.AmountMicrocredits}).Error; err != nil {
		t.Fatal(err)
	}
	if err := claimDB.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fixture.task.ID).Updates(map[string]any{
		"provider_status": "succeeded", "provider_task_id": "provider-pg-atomic-resource", "execution_lease_token": fixture.task.LeaseToken,
		"asset_source_url": "https://cdn.example/result.mp4", "last_frame_url": "https://cdn.example/last.png", "actual_duration_seconds": 8, "total_tokens": "42",
	}).Error; err != nil {
		t.Fatal(err)
	}
	resource, err := claimService.prepareSourceTaskResource(fixture.task.UserID, fixture.task.ID, "video", "generated.mp4", "video/mp4", 24, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	claimPaused := make(chan struct{})
	releaseClaim := make(chan struct{})
	const callbackName = "test:pg-provider-quota-write-claim"
	if err := claimDB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "resources" {
			return
		}
		updates, ok := tx.Statement.Dest.(map[string]any)
		if !ok || updates["write_token"] != "pg-atomic-write" {
			return
		}
		close(claimPaused)
		<-releaseClaim
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = claimDB.Callback().Update().Remove(callbackName) })
	claimDone := make(chan struct {
		resource *model.Resource
		err      error
	}, 1)
	go func() {
		claimed, claimErr := claimService.claimPreparedGeneratedResourceWrite(fixture.task.UserID, fixture.task, resource, "pg-atomic-write", "pending/pg-atomic.mp4", time.Minute)
		claimDone <- struct {
			resource *model.Resource
			err      error
		}{claimed, claimErr}
	}()
	select {
	case <-claimPaused:
	case <-time.After(5 * time.Second):
		close(releaseClaim)
		t.Fatal("PostgreSQL quota/write claim did not pause")
	}
	adminDone := make(chan error, 1)
	go func() {
		_, resolveErr := adminService.ResolveBillingOrder(&admin, fixture.order.ID, ResolveBillingRequest{Action: "refund", Note: "PG admin raced atomic resource claim"})
		adminDone <- resolveErr
	}()
	select {
	case resolveErr := <-adminDone:
		close(releaseClaim)
		t.Fatalf("PostgreSQL admin crossed atomic quota/write claim: %v", resolveErr)
	case <-time.After(150 * time.Millisecond):
	}
	close(releaseClaim)
	claimedResult := <-claimDone
	if claimedResult.err != nil {
		t.Fatal(claimedResult.err)
	}
	if err := <-adminDone; err != nil {
		t.Fatal(err)
	}
	claimedResult.resource.Status = model.ResourceStatusReady
	claimedResult.resource.ETag = "pg-atomic-ready"
	if err := repository.New(claimDB).CompleteSourceTaskResourceWrite(claimedResult.resource, fixture.task.LeaseOwner, fixture.task.LeaseToken, "pg-atomic-write"); err != nil {
		t.Fatal(err)
	}
	if err := adminDB.Model(&model.Task{}).Where("id = ?", fixture.task.ID).Update("next_poll_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	reclaimed, err := repository.New(adminDB).ClaimNextTask(adminService.workerID, time.Minute)
	if err != nil || reclaimed == nil {
		t.Fatalf("PostgreSQL claim resolved resource completion = %#v, %v", reclaimed, err)
	}
	if claimedExecution, err := repository.New(adminDB).ClaimProviderTaskExecution(reclaimed.ID, reclaimed.LeaseOwner, reclaimed.LeaseToken); err != nil || !claimedExecution {
		t.Fatalf("PostgreSQL claim resolved provider completion = %t, %v", claimedExecution, err)
	}
	if err := repository.New(adminDB).CompleteResolvedProviderPostprocess(fixture.task.ID, repository.ProviderTaskLease{Owner: reclaimed.LeaseOwner, Token: reclaimed.LeaseToken}, model.TaskStatusFailed, "resolved", "resolved", reclaimed.InputJSON, `{"resourceId":"`+claimedResult.resource.ID+`"}`); err != nil {
		t.Fatal(err)
	}
	storedTask, _ := repository.New(adminDB).Task(fixture.task.ID)
	storedFact, _ := repository.New(adminDB).ProviderTaskFact(fixture.task.ID)
	storedOrder, _ := repository.New(adminDB).BillingOrder(fixture.order.ID)
	storedResource, _ := repository.New(adminDB).ResourceForSourceTask(fixture.task.UserID, fixture.task.ID)
	var usage model.UserDailyUploadUsage
	if err := adminDB.First(&usage, "user_id = ?", fixture.task.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != model.TaskStatusFailed || storedFact.ReconciliationStatus != "resolved" || storedOrder.Status != model.BillingStatusRefunded || storedResource.Status != model.ResourceStatusReady || usage.Bytes != resource.Size {
		t.Fatalf("PostgreSQL atomic resource convergence: task=%#v fact=%#v order=%#v resource=%#v usage=%#v", storedTask, storedFact, storedOrder, storedResource, usage)
	}
}

type providerRuntimeFixture struct {
	task         model.Task
	order        model.BillingOrder
	channel      model.ModelChannel
	channelModel model.ChannelModel
	endpointV1   string
}

type gatedProviderResourceReader struct {
	payload []byte
	started chan struct{}
	release chan struct{}
	once    sync.Once
	done    bool
}

func (reader *gatedProviderResourceReader) Read(buffer []byte) (int, error) {
	if reader.done {
		return 0, io.EOF
	}
	reader.once.Do(func() { close(reader.started) })
	<-reader.release
	reader.done = true
	return copy(buffer, reader.payload), nil
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
