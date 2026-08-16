package service

import (
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/testsupport"

	"gorm.io/gorm"
)

func TestPostgresAgentGenerationSubmitConcurrentReplayCreatesOneCommercialFact(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	dataDirectory := t.TempDir()
	primary := New(repository.New(db), dataDirectory)
	if err := primary.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	seedPostgresAgentGenerationFixture(t, primary, db)
	secondary := New(repository.New(db), dataDirectory)

	request := CreateTaskRequest{
		ProjectID: "runtime-canvas",
		Type:      "canvas_image",
		Operation: agentGenerationOperation("postgres-generation-run"),
		Prompt:    "生成一张用于并发幂等验收的图片",
		Input: map[string]interface{}{
			"mode": "image",
			"config": map[string]interface{}{
				"channelId": "postgres-image-channel", "model": "kz_gpt_image2",
				"quality": "medium", "size": "1:1", "resolution": "1K", "count": 1,
			},
		},
	}
	identity := taskCreationIdentity{
		TaskID:                 agentGenerationIdentity("postgres-generation-submit"),
		BillingIdempotencyKey:  "agent-generation:" + agentGenerationIdentity("postgres-generation-submit"),
		UseCurrentBillingQuote: true,
	}
	services := []*Service{primary, secondary, primary, secondary, primary, secondary}
	start := make(chan struct{})
	results := make([]*model.Task, len(services))
	errorsByWorker := make([]error, len(services))
	var workers sync.WaitGroup
	for index, svc := range services {
		workers.Add(1)
		go func(worker int, contender *Service) {
			defer workers.Done()
			<-start
			results[worker], errorsByWorker[worker] = contender.createTaskWithIdentity("runtime-user", request, identity)
		}(index, svc)
	}
	close(start)
	workers.Wait()
	for index, err := range errorsByWorker {
		if err != nil {
			t.Fatalf("submit worker %d: %v", index, err)
		}
		if results[index] == nil || results[index].ID != identity.TaskID || results[index].BillingOrderID == "" {
			t.Fatalf("submit worker %d result = %#v", index, results[index])
		}
	}

	var taskCount, orderCount, reserveLedgerCount int64
	if err := db.Model(&model.Task{}).Where("id = ?", identity.TaskID).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("user_id = ? AND idempotency_key = ?", "runtime-user", identity.BillingIdempotencyKey).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).Where("user_id = ? AND type = ?", "runtime-user", model.CreditLedgerReserve).Count(&reserveLedgerCount).Error; err != nil {
		t.Fatal(err)
	}
	var credits model.CreditAccount
	if err := db.First(&credits, "user_id = ?", "runtime-user").Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 1 || orderCount != 1 || reserveLedgerCount != 1 || credits.AvailableMicrocredits != 750 || credits.ReservedMicrocredits != 250 {
		t.Fatalf("commercial facts: tasks=%d orders=%d ledgers=%d credits=%#v", taskCount, orderCount, reserveLedgerCount, credits)
	}
}

func seedPostgresAgentGenerationFixture(t *testing.T, svc *Service, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC()
	account := model.ProviderAccount{ID: "postgres-runtime-account", ProviderKind: kuaiziProviderKind, Name: "筷子", Enabled: true, CreatedAt: now, UpdatedAt: now}
	endpoint := model.ProviderEndpointVersion{ID: "postgres-runtime-endpoint", ProviderAccountID: account.ID, BaseURL: "https://example.com", Status: "active", Version: 1, CreatedAt: now}
	credential := model.ProviderCredential{ID: "postgres-runtime-credential", ProviderAccountID: account.ID, Family: kuaiziAccountCredentialFamily, Enabled: true, HealthStatus: "healthy", ConcurrencyLimit: 2, CreatedAt: now, UpdatedAt: now}
	for _, value := range []interface{}{&account, &endpoint, &credential} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	ciphertext, err := svc.EncryptProviderSecret(account.ID, credential.ID, 1, "postgres-runtime-secret")
	if err != nil {
		t.Fatal(err)
	}
	version := model.ProviderCredentialVersion{ID: "postgres-runtime-key", ProviderCredentialID: credential.ID, KeyCipher: ciphertext, Status: "active", Version: 1, CreatedAt: now}
	channel := model.ModelChannel{ID: "postgres-image-channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "Agent Image", APIFormat: "openai", InterfaceType: model.ChannelInterfaceOpenAIImage, ModelsJSON: `["kz_gpt_image2"]`, CreatedAt: now, UpdatedAt: now}
	channelModel := model.ChannelModel{
		ID: "postgres-image-model", ChannelID: channel.ID, ModelKey: "kz_gpt_image2", DisplayName: "GPT Image 2",
		ProviderCredentialID: credential.ID, AccessPolicy: model.ModelAccessAuthenticated, Capability: "image",
		BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 250, PriceConfigured: true, Enabled: true,
		CreatedAt: now, UpdatedAt: now,
	}
	canvas := model.CanvasProject{ID: "runtime-canvas", UserID: "runtime-user", Title: "Agent Canvas", Revision: 1, PayloadJSON: `{"nodes":[],"connections":[]}`, CreatedAt: now, UpdatedAt: now}
	credits := model.CreditAccount{UserID: "runtime-user", AvailableMicrocredits: 1_000, CreatedAt: now, UpdatedAt: now}
	for _, value := range []interface{}{&version, &channel, &channelModel, &canvas, &credits} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
}
