package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/testsupport"
)

func TestPostgresKuaiziReconciliationUsesFrozenCredentialForTokenAndMedia(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	var oldCalls int
	oldServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		oldCalls++
		if request.Header.Get("ApiKey") != "old-frozen-key" {
			t.Fatalf("old ApiKey = %q", request.Header.Get("ApiKey"))
		}
		var payload struct {
			TaskID string `json:"task_id"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		amount := 6
		if payload.TaskID == "pg-media-task" {
			amount = 2
		}
		_, _ = fmt.Fprintf(writer, `{"code":0,"data":{"items":[{"order_id":"pg-provider-order-%s","amount":%d,"status":"succeeded","task_id":"%s","task_status":"succeeded","task_duration":1,"total_tokens":10,"created_at":"2026-08-15T10:00:00Z"}]}}`, payload.TaskID, amount, payload.TaskID)
	}))
	defer oldServer.Close()
	newServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("reconciliation used rotated endpoint")
	}))
	defer newServer.Close()

	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureProviderIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	svc := New(repository.New(db), filepath.Join(t.TempDir(), "data"))
	now := time.Now().UTC()
	account := model.ProviderAccount{ID: "pg-provider-account", ProviderKind: kuaiziProviderKind, Name: "筷子", Enabled: true, CreatedAt: now, UpdatedAt: now}
	credential := model.ProviderCredential{ID: "pg-provider-credential", ProviderAccountID: account.ID, Family: kuaiziAccountCredentialFamily, Enabled: true, HealthStatus: "healthy", CreatedAt: now, UpdatedAt: now}
	oldCipher, err := svc.EncryptProviderSecret(account.ID, credential.ID, 1, "old-frozen-key")
	if err != nil {
		t.Fatal(err)
	}
	newCipher, err := svc.EncryptProviderSecret(account.ID, credential.ID, 2, "new-key")
	if err != nil {
		t.Fatal(err)
	}
	rows := []any{
		&account,
		&model.ProviderEndpointVersion{ID: "pg-endpoint-v1", ProviderAccountID: account.ID, BaseURL: oldServer.URL, Status: "retired", Version: 1, CreatedAt: now},
		&model.ProviderEndpointVersion{ID: "pg-endpoint-v2", ProviderAccountID: account.ID, BaseURL: newServer.URL, Status: "active", Version: 2, CreatedAt: now},
		&credential,
		&model.ProviderCredentialVersion{ID: "pg-key-v1", ProviderCredentialID: credential.ID, KeyCipher: oldCipher, Status: "retired", Version: 1, CreatedAt: now},
		&model.ProviderCredentialVersion{ID: "pg-key-v2", ProviderCredentialID: credential.ID, KeyCipher: newCipher, Status: "active", Version: 2, CreatedAt: now},
		&model.CreditAccount{UserID: "pg-agent-user", AvailableMicrocredits: 100_000_000, CreatedAt: now, UpdatedAt: now},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	order := &model.BillingOrder{
		ID: "pg-reconcile-order", UserID: "pg-agent-user", IdempotencyKey: "pg-reconcile-order", ChannelID: "pg-channel", ChannelModelID: "pg-model",
		Model: "deepseek-v4-flash", Capability: "text", Scene: "agent", BillingMode: "token_usage", Status: model.BillingStatusReserved,
		AmountMicrocredits: 30_000_000, ReservedAmountMicrocredits: 30_000_000, ProviderEndpointVersionID: "pg-endpoint-v1", ProviderCredentialVersionID: "pg-key-v1",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := svc.repo.ReserveBillingOrder(order); err != nil {
		t.Fatal(err)
	}
	if err := svc.BeginTokenBillingRequest(order.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.ScheduleTokenBillingReconciliation(order.ID, "chatcmpl-pg-provider-task", "pending", TokenUsageFact{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("id = ?", order.ID).Update("next_reconcile_at", now.Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	mediaOrder := &model.BillingOrder{
		ID: "pg-media-reconcile-order", UserID: "pg-agent-user", IdempotencyKey: "pg-media-reconcile-order",
		Model: "doubao-seedance-2-0-260128", Capability: "video", Scene: "image_to_video", BillingMode: "per_second",
		Status: model.BillingStatusReserved, AmountMicrocredits: 10_000_000, ProviderRequestID: "pg-media-task",
		ProviderEndpointVersionID: "pg-endpoint-v1", ProviderCredentialVersionID: "pg-key-v1", CreatedAt: now, UpdatedAt: now,
	}
	if err := svc.repo.ReserveBillingOrder(mediaOrder); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("id = ?", mediaOrder.ID).Updates(map[string]any{
		"status": model.BillingStatusUncertain, "next_reconcile_at": now.Add(-time.Second),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RunKuaiziBillingReconciliationBatch(context.Background(), now, 10); err != nil {
		t.Fatal(err)
	}
	if oldCalls != 2 {
		t.Fatalf("old endpoint calls = %d", oldCalls)
	}
	var stored model.BillingOrder
	if err := db.First(&stored, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.BillingStatusSettled || stored.ProviderBillingAmount != 6 {
		t.Fatalf("reconciled order = %#v", stored)
	}
	var storedMedia model.BillingOrder
	if err := db.First(&storedMedia, "id = ?", mediaOrder.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedMedia.Status != model.BillingStatusSettled || storedMedia.ProviderBillingAmount != 2 {
		t.Fatalf("reconciled media order = %#v", storedMedia)
	}
}
