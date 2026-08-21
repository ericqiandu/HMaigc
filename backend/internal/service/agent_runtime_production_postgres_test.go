package service

import (
	"strings"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/testsupport"

	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresProductionRenderApprovalReplayCreatesOneCommercialFactAcrossConnections(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	dataDirectory := t.TempDir()
	primary := New(repository.New(db), dataDirectory)
	if err := primary.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	seedPostgresProductionRenderFixture(t, primary, db)
	secondDB := openSecondProductionPostgresConnection(t, db)
	secondary := New(repository.New(secondDB), dataDirectory)

	scope := agentruntime.Scope{
		TenantKind: agentruntime.TenantPersonal, TenantID: "runtime-user", ActorUserID: "runtime-user",
		RunID: "postgres-production-run", ThreadID: "postgres-production-thread", CanvasID: "runtime-canvas",
		Access: agentruntime.AccessGrant{Level: agentruntime.AccessEditor},
	}
	now := time.Now().UTC()
	plan := model.AgentProductionPlanVersion{
		ID: "postgres-production-version", PlanKey: "postgres-render-plan",
		TenantKind: scope.TenantKind, TenantID: scope.TenantID, DomainProjectID: scope.DomainProjectID, CanvasID: scope.CanvasID,
		CreatedByRunID: scope.RunID, Version: 1, Status: model.AgentProductionPlanActive,
		Title: "跨连接分镜", TargetDurationMS: 5_000, Script: "鲜橙落水。",
		ShotsJSON:            `[{"shotKey":"shot-1","order":1,"durationMs":5000,"scriptText":"鲜橙落水","imagePrompt":"鲜橙产品特写","videoPrompt":"慢镜头水花","dependencies":[]}]`,
		ExpectedDeliveryJSON: `{"scripts":1,"storyboardImages":1,"videoClips":1}`, CreatedAt: now, UpdatedAt: now,
	}
	artifact := model.AgentProductionArtifact{
		ID: "postgres-storyboard-artifact", PlanKey: plan.PlanKey, PlanVersionID: plan.ID, PlanVersion: plan.Version,
		ShotKey: "shot-1", Kind: model.AgentProductionArtifactStoryboardImage,
		Status: model.AgentProductionArtifactAwaitingApproval, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&artifact).Error; err != nil {
		t.Fatal(err)
	}
	quote, err := primary.QuoteTaskBilling(scope.ActorUserID, TaskBillingQuoteRequest{
		ProjectID: scope.CanvasID, Type: "canvas_image", Operation: "production_render", BatchCount: 1,
		Input: TaskBillingQuoteInput{Mode: "image", Config: TaskBillingQuoteConfig{
			ChannelID: "postgres-image-channel", Model: "kz_gpt_image2", Size: "1:1", Quality: "medium",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	arguments := agentruntime.ProductionRenderArguments{
		PlanKey: plan.PlanKey, PlanVersion: plan.Version, ArtifactID: artifact.ID, Attempt: 0,
		GenerationModel: agentruntime.GenerationModelSelection{ChannelID: "postgres-image-channel", Model: "kz_gpt_image2"},
		ImageConfig:     &agentruntime.ImageRenderConfig{Size: "1:1", Quality: "medium", Count: 1},
		FrozenRenderQuote: agentruntime.FrozenRenderQuote{
			AmountMicrocredits: quote.AmountMicrocredits, PerTaskAmountMicrocredits: quote.PerTaskAmountMicrocredits,
			PriceVersion: quote.PriceVersion, BillingMode: quote.BillingMode, PricingResolution: quote.PricingResolution,
			PricingInputVariant: quote.PricingInputVariant, Quantity: quote.Quantity, QuoteFingerprint: quote.QuoteFingerprint,
		},
	}
	call := model.AgentToolCall{IdempotencyKey: "postgres-production-render:1"}
	services := []*Service{primary, secondary}
	errs := make([]error, len(services))
	results := make([]*model.AgentProductionArtifact, len(services))
	start := make(chan struct{})
	var workers sync.WaitGroup
	for index, svc := range services {
		workers.Add(1)
		go func(worker int, contender *Service) {
			defer workers.Done()
			<-start
			current, loadErr := contender.productionArtifactForRender(scope, arguments)
			if loadErr != nil {
				errs[worker] = loadErr
				return
			}
			task, order, createErr := contender.ensureProductionArtifactTask(scope, &call, arguments, *current)
			if createErr != nil {
				errs[worker] = createErr
				return
			}
			results[worker], errs[worker] = contender.bindProductionArtifactTask(scope, arguments, *current, *task, *order)
		}(index, svc)
	}
	close(start)
	workers.Wait()
	for index, err := range errs {
		if err != nil {
			t.Fatalf("production render worker %d: %v", index, err)
		}
		if results[index] == nil || results[index].Status != model.AgentProductionArtifactQueued || results[index].Attempt != 1 {
			t.Fatalf("production render worker %d result = %#v", index, results[index])
		}
	}

	identity := productionRenderTaskIdentity(call.IdempotencyKey, 1)
	var taskCount, orderCount, reserveCount int64
	if err := db.Model(&model.Task{}).Where("id = ?", identity).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("user_id = ? AND idempotency_key = ?", scope.ActorUserID, "agent-production:"+identity).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).Where("user_id = ? AND type = ?", scope.ActorUserID, model.CreditLedgerReserve).Count(&reserveCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 1 || orderCount != 1 || reserveCount != 1 || results[0].TaskID != results[1].TaskID || results[0].BillingOrderID != results[1].BillingOrderID {
		t.Fatalf("production commercial facts tasks=%d orders=%d reserves=%d first=%#v second=%#v", taskCount, orderCount, reserveCount, results[0], results[1])
	}
}

func seedPostgresProductionRenderFixture(t *testing.T, svc *Service, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC()
	account := model.ProviderAccount{ID: "postgres-runtime-account", ProviderKind: kuaiziProviderKind, Name: "筷子", Enabled: true, CreatedAt: now, UpdatedAt: now}
	endpoint := model.ProviderEndpointVersion{ID: "postgres-runtime-endpoint", ProviderAccountID: account.ID, BaseURL: "https://example.com", Status: "active", Version: 1, CreatedAt: now}
	credential := model.ProviderCredential{ID: "postgres-runtime-credential", ProviderAccountID: account.ID, Family: kuaiziAccountCredentialFamily, Enabled: true, HealthStatus: "healthy", ConcurrencyLimit: 2, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatal(err)
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
	if err := db.Create(&version).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&channelModel).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&canvas).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&credits).Error; err != nil {
		t.Fatal(err)
	}
}

func openSecondProductionPostgresConnection(t *testing.T, db *gorm.DB) *gorm.DB {
	t.Helper()
	dialector, ok := db.Dialector.(*postgresdriver.Dialector)
	if !ok || dialector.Config == nil || strings.TrimSpace(dialector.Config.DSN) == "" {
		t.Fatal("PostgreSQL integration DSN is unavailable")
	}
	second, err := database.Open(database.Config{Driver: "postgres", DSN: dialector.Config.DSN})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := second.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return second
}
