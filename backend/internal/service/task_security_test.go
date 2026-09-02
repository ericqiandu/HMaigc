package service

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTaskSecurityRejectsMissingEnvelopeBeforeBillingOrExecution(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.BillingOrder{}, &model.TaskLog{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	task := model.Task{
		ID: "task-without-envelope", UserID: "user-1", ProjectID: "canvas-1", Type: "canvas_image",
		Audience: model.TaskAudienceCustomer, ExecutionKind: model.TaskExecutionProvider,
		Status: model.TaskStatusRunning, Stage: "运行中", BillingOrderID: "billing-1", InputJSON: `{}`,
		LeaseOwner: "worker-1", LeaseExpiresAt: ptr(now.Add(time.Minute)), LeaseGeneration: 1,
		LeaseToken: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		CreatedAt:  now, UpdatedAt: now,
	}
	order := model.BillingOrder{
		ID: task.BillingOrderID, UserID: task.UserID, TaskID: task.ID,
		Status: model.BillingStatusReserved, AmountMicrocredits: 100_000, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	svc := New(repository.New(db), t.TempDir())
	svc.workerID = task.LeaseOwner

	err = svc.processClaimedTask(&task)
	if err == nil || !strings.Contains(err.Error(), "任务执行信封") {
		t.Fatalf("processClaimedTask() error = %v, want explicit envelope error", err)
	}
	var storedTask model.Task
	if err := db.First(&storedTask, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != model.TaskStatusFailed || !strings.Contains(storedTask.Error, "任务执行信封") {
		t.Fatalf("stored task = status:%q error:%q", storedTask.Status, storedTask.Error)
	}
	var storedOrder model.BillingOrder
	if err := db.First(&storedOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedOrder.Status != model.BillingStatusReserved {
		t.Fatalf("billing status = %q, want reserved before any billing side effect", storedOrder.Status)
	}
}

func TestTaskSecurityPersistsSignedFactsAndRejectsChangedPayloadOnRetry(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.BillingOrder{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	task := model.Task{
		ID: "sealed-task", UserID: "user-1", ProjectID: "canvas-1", Type: "canvas_image",
		Audience: model.TaskAudienceCustomer, ExecutionKind: model.TaskExecutionProvider,
		Status: model.TaskStatusFailed, BillingOrderID: "billing-1",
		Prompt:    "original prompt",
		InputJSON: `{"mode":"image","apiKey":"encrypted-secret-value"}`,
		CreatedAt: now, UpdatedAt: now,
	}
	order := model.BillingOrder{
		ID: task.BillingOrderID, UserID: task.UserID, TeamID: "team-1", TaskID: task.ID,
		Status: model.BillingStatusRefunded, CreatedAt: now, UpdatedAt: now,
	}
	svc := New(repository.New(db), t.TempDir())
	if err := svc.sealTaskExecutionEnvelope(&task, &order, now); err != nil {
		t.Fatalf("sealTaskExecutionEnvelope(): %v", err)
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if task.ExecutionEnvelope == "" || task.ExecutionEnvelopeKeyID == "" || len(task.ExecutionPayloadDigest) != 64 {
		t.Fatalf("sealed execution facts are incomplete: %#v", task)
	}
	if strings.Contains(task.ExecutionEnvelope, "encrypted-secret-value") {
		t.Fatal("execution envelope leaked task input")
	}
	if err := svc.verifyTaskExecutionEnvelope(&task, now.Add(time.Minute)); err != nil {
		t.Fatalf("verifyTaskExecutionEnvelope(): %v", err)
	}
	changedPrompt := task
	changedPrompt.Prompt = "changed prompt"
	if err := svc.verifyTaskExecutionEnvelope(&changedPrompt, now.Add(time.Minute)); err == nil || !strings.Contains(err.Error(), "payload digest") {
		t.Fatalf("changed prompt verification error = %v, want payload digest mismatch", err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("id = ?", order.ID).Update("task_id", "different-task").Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.verifyTaskExecutionEnvelope(&task, now.Add(time.Minute)); err == nil || !strings.Contains(err.Error(), "计费订单与执行任务冲突") {
		t.Fatalf("changed billing task verification error = %v, want billing task mismatch", err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("id = ?", order.ID).Update("task_id", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Update("input_json", `{"mode":"image","apiKey":"changed"}`).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := svc.RetryTask(task.UserID, task.ID); err == nil || !strings.Contains(err.Error(), "payload digest") {
		t.Fatalf("RetryTask() error = %v, want payload digest mismatch", err)
	}
	var storedOrder model.BillingOrder
	if err := db.First(&storedOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedOrder.Status != model.BillingStatusRefunded {
		t.Fatalf("billing status changed after rejected retry: %q", storedOrder.Status)
	}
}

func TestStorageMigrationPreservesSignedTaskInput(t *testing.T) {
	dataDir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dataDir, "storage.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err := db.AutoMigrate(
		&model.Task{},
		&model.Asset{},
		&model.CanvasProject{},
		&model.TaskLog{},
		&model.Result{},
	); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	originalInput := `{"mode":"image","metadata":{"source":"signed-task"}}`
	task := model.Task{
		ID: "signed-storage-migration-task", UserID: "user-1", ProjectID: "canvas-1", Type: "canvas_image",
		Audience: model.TaskAudienceCustomer, ExecutionKind: model.TaskExecutionProvider,
		Status: model.TaskStatusSucceeded, InputJSON: originalInput, CreatedAt: now, UpdatedAt: now,
	}
	svc := New(repository.New(db), dataDir)
	if err := svc.sealTaskExecutionEnvelope(&task, nil, now); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := svc.MigrateLegacyStorage(); err != nil {
		t.Fatalf("MigrateLegacyStorage(): %v", err)
	}
	var stored model.Task
	if err := db.First(&stored, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.InputJSON != originalInput {
		t.Fatalf("signed task input changed during migration: got %q want %q", stored.InputJSON, originalInput)
	}
	if err := svc.verifyTaskExecutionEnvelope(&stored, now.Add(time.Minute)); err != nil {
		t.Fatalf("signed task failed verification after storage migration: %v", err)
	}
}

func TestTaskInputRejectsClientWatermarkFields(t *testing.T) {
	for _, input := range []map[string]any{
		{"watermark": true},
		{"config": map[string]any{"videoWatermark": "false"}},
		{"metadata": map[string]any{"watermark": "true"}},
	} {
		if err := validateTaskWatermarkInput(input); err == nil {
			t.Fatalf("validateTaskWatermarkInput(%#v) unexpectedly succeeded", input)
		}
	}
}

func TestNormalizeTaskInputMakesTypedProviderConfigBillable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.ModelChannel{},
		&model.ChannelModel{},
		&model.ChannelModelPriceTier{},
		&model.SystemSetting{},
		&model.MembershipPlan{},
		&model.MembershipSubscription{},
		&model.TeamMember{},
	); err != nil {
		t.Fatal(err)
	}
	channelModel := model.ChannelModel{
		ID: "model-1", ChannelID: "channel-1", ModelKey: "text-model", Capability: "text",
		BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 100_000, PriceConfigured: true, Enabled: true,
		AccessPolicy: model.ModelAccessAuthenticated,
	}
	if err := db.Create(&model.ModelChannel{ID: "channel-1", Scope: model.ChannelScopeSystem, Enabled: true, InterfaceType: model.ChannelInterfaceChatCompletion}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&channelModel).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SystemSetting{Key: agentDefaultModelSettingKey, ValueJSON: `{"channelModelId":"model-1"}`}).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db)}
	input, err := normalizeTaskInput(map[string]any{
		"mode":   "text",
		"config": providerConfig{ChannelID: "channel-1", Model: "text-model", APIKey: "system"},
	})
	if err != nil {
		t.Fatal(err)
	}
	order, err := svc.taskBillingOrder("user-1", &model.Task{ID: "task-1", Type: "agent_storyboard"}, input)
	if err != nil {
		t.Fatal(err)
	}
	if order == nil || order.ChannelID != "channel-1" || order.AmountMicrocredits != 100_000 {
		t.Fatalf("taskBillingOrder() = %#v", order)
	}
}

func TestTaskBillingOrderRejectsAgentModelOutsideAdministratorDefault(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ModelChannel{}, &model.ChannelModel{}, &model.ChannelModelPriceTier{}, &model.SystemSetting{}, &model.MembershipPlan{}, &model.MembershipSubscription{}, &model.TeamMember{}); err != nil {
		t.Fatal(err)
	}
	channel := model.ModelChannel{ID: "channel", Scope: model.ChannelScopeSystem, Enabled: true, InterfaceType: model.ChannelInterfaceChatCompletion}
	models := []model.ChannelModel{
		{ID: "default", ChannelID: channel.ID, ModelKey: "gpt", Capability: "text", BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 100, PriceConfigured: true, Enabled: true, AccessPolicy: model.ModelAccessAuthenticated},
		{ID: "other", ChannelID: channel.ID, ModelKey: "deepseek", Capability: "text", BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 100, PriceConfigured: true, Enabled: true, AccessPolicy: model.ModelAccessAuthenticated},
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SystemSetting{Key: agentDefaultModelSettingKey, ValueJSON: `{"channelModelId":"default"}`}).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db)}
	_, err = svc.taskBillingOrder("user", &model.Task{ID: "task", Type: "agent_storyboard"}, map[string]any{
		"mode": "text", "config": map[string]any{"channelId": channel.ID, "model": "deepseek"},
	})
	if err == nil || !strings.Contains(err.Error(), "全站默认模型") {
		t.Fatalf("non-default Agent model error = %v", err)
	}
}

func TestSystemProxyRejectsTextModelOutsideAdministratorDefault(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ModelChannel{}, &model.ChannelModel{}, &model.ChannelModelPriceTier{}, &model.SystemSetting{}); err != nil {
		t.Fatal(err)
	}
	channel := model.ModelChannel{ID: "legacy-text", Scope: model.ChannelScopeSystem, Enabled: true, InterfaceType: model.ChannelInterfaceChatCompletion, BaseURL: "https://example.com/v1", APIKey: "secret"}
	models := []model.ChannelModel{
		{ID: "default", ChannelID: channel.ID, ModelKey: "default-model", Capability: "text", BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 100, PriceConfigured: true, Enabled: true, AccessPolicy: model.ModelAccessAuthenticated},
		{ID: "other", ChannelID: channel.ID, ModelKey: "other-model", Capability: "text", BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 100, PriceConfigured: true, Enabled: true, AccessPolicy: model.ModelAccessAuthenticated},
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SystemSetting{Key: agentDefaultModelSettingKey, ValueJSON: `{"channelModelId":"default"}`}).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db)}
	if _, err := svc.ResolveSystemProxyRuntime(&channel, "other-model"); err == nil || !strings.Contains(err.Error(), "全站默认模型") {
		t.Fatalf("non-default system proxy error = %v", err)
	}
	runtime, err := svc.ResolveSystemProxyRuntime(&channel, "default-model")
	if err != nil || runtime.APIKey != channel.APIKey {
		t.Fatalf("default system proxy runtime = %#v, %v", runtime, err)
	}
}

func TestRetryTaskRejectsUncertainBillingBeforeAnotherProviderRequest(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.BillingOrder{}); err != nil {
		t.Fatal(err)
	}
	task := model.Task{ID: "task", UserID: "user", Type: "canvas_text", Status: model.TaskStatusFailed, BillingOrderID: "bill", InputJSON: `{}`}
	orders := []model.BillingOrder{
		{ID: "bill", UserID: "user", TaskID: task.ID, IdempotencyKey: "uncertain", Status: model.BillingStatusUncertain},
		{ID: "bill-newer", UserID: "user", TaskID: task.ID, IdempotencyKey: "settled", Status: model.BillingStatusSettled},
	}
	svc := New(repository.New(db), t.TempDir())
	if err := svc.sealTaskExecutionEnvelope(&task, &orders[0], time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&orders).Error; err != nil {
		t.Fatal(err)
	}
	_, err = svc.RetryTask("user", task.ID)
	if err == nil || !strings.Contains(err.Error(), "费用状态仍待核对") {
		t.Fatalf("RetryTask() error = %v", err)
	}
}

func TestTaskBillingOrderRejectsMissingSystemModel(t *testing.T) {
	svc := &Service{}
	_, err := svc.taskBillingOrder("user-1", &model.Task{ID: "task-1", Type: "canvas_image"}, map[string]any{"mode": "image"})
	if err == nil || !strings.Contains(err.Error(), "后台配置的系统模型") {
		t.Fatalf("taskBillingOrder() error = %v", err)
	}
}

func TestTaskBillingOrderRejectsCapabilityMismatchAndZeroPrice(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ChannelModel{}, &model.ChannelModelPriceTier{}, &model.SystemSetting{}, &model.MembershipPlan{}, &model.MembershipSubscription{}, &model.TeamMember{}); err != nil {
		t.Fatal(err)
	}
	items := []model.ChannelModel{
		{
			ID: "text-model", ChannelID: "channel-1", ModelKey: "text-model", Capability: "text",
			BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 100_000, PriceConfigured: true, Enabled: true,
		},
		{
			ID: "free-image-model", ChannelID: "channel-1", ModelKey: "free-image-model", Capability: "image",
			BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 0, PriceConfigured: true, Enabled: true,
		},
	}
	if err := db.Create(&items).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db)}
	task := &model.Task{ID: "task-1", Type: "canvas_image"}

	_, err = svc.taskBillingOrder("user-1", task, map[string]any{
		"mode":   "image",
		"config": map[string]any{"channelId": "channel-1", "model": "text-model"},
	})
	if err == nil || !strings.Contains(err.Error(), "能力") {
		t.Fatalf("capability mismatch error = %v", err)
	}

	_, err = svc.taskBillingOrder("user-1", task, map[string]any{
		"mode":   "image",
		"config": map[string]any{"channelId": "channel-1", "model": "free-image-model"},
	})
	if err == nil || !strings.Contains(err.Error(), "积分价格") {
		t.Fatalf("zero price error = %v", err)
	}
}

func TestReserveProxyBillingDeductsCreditsAndRejectsInsufficientBalance(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.ChannelModel{},
		&model.ChannelModelPriceTier{},
		&model.SystemSetting{},
		&model.MembershipPlan{},
		&model.MembershipSubscription{},
		&model.TeamMember{},
		&model.CreditAccount{},
		&model.CreditLedgerEntry{},
		&model.BillingOrder{},
	); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ChannelModel{
		ID: "image-model", ChannelID: "channel-1", ModelKey: "image-model", Capability: "image",
		BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 100_000, PriceConfigured: true, Enabled: true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CreditAccount{UserID: "user-1", AvailableMicrocredits: 150_000}).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db)}
	order, err := svc.ReserveProxyBilling("user-1", "channel-1", "image-model", "image", "canvas_image", "request-1", BillingUsage{Quantity: 1})
	if err != nil {
		t.Fatal(err)
	}
	if order.AmountMicrocredits != 100_000 {
		t.Fatalf("order amount = %d", order.AmountMicrocredits)
	}
	var account model.CreditAccount
	if err := db.First(&account, "user_id = ?", "user-1").Error; err != nil {
		t.Fatal(err)
	}
	if account.AvailableMicrocredits != 50_000 || account.ReservedMicrocredits != 100_000 {
		t.Fatalf("account = available:%d reserved:%d", account.AvailableMicrocredits, account.ReservedMicrocredits)
	}
	var ledgerCount int64
	if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ?", order.ID).Count(&ledgerCount).Error; err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 1 {
		t.Fatalf("reserve ledger count = %d", ledgerCount)
	}
	if _, err := svc.ReserveProxyBilling("user-1", "channel-1", "image-model", "image", "canvas_image", "request-2", BillingUsage{Quantity: 1}); err == nil || !strings.Contains(err.Error(), "积分不足") {
		t.Fatalf("insufficient balance error = %v", err)
	}
}

func TestNewBillingOrderAddsSuperResolutionPriceAndFreezesSnapshot(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.ChannelModel{},
		&model.ChannelModelPriceTier{},
		&model.SuperResolutionPricingRule{},
		&model.SystemSetting{},
		&model.MembershipPlan{},
		&model.MembershipSubscription{},
		&model.TeamMember{},
	); err != nil {
		t.Fatal(err)
	}
	videoModel := model.ChannelModel{
		ID: "video-model", ChannelID: "channel-1", ModelKey: "video-model", Capability: "video",
		BillingMode: "per_second", PriceStrategy: "video_resolution", PriceConfigured: true, Enabled: true, PriceVersion: 3,
		PriceTiers: []model.ChannelModelPriceTier{{ID: "video-tier-720p", Resolution: "720P", UnitPriceMicrocredits: 500_000}},
	}
	rule := model.SuperResolutionPricingRule{
		ID: "sr-professional-4k-60", Edition: "professional", Resolution: "4K",
		FPSMinExclusive: 30, FPSMaxInclusive: 60, Currency: "CNY",
		SupplierCostMinMicros: 2_000_000, SupplierCostMaxMicros: 2_000_000,
		UnitPriceMicrocredits: 800_000, PriceConfigured: true, Enabled: true, PriceVersion: 4,
	}
	if err := db.Create(&videoModel).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&rule).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db)}

	order, err := svc.newBillingOrder("user-1", personalBillingAccountScope(), "task-1", "request-1", "channel-1", "video-model", "video", "canvas_video", BillingUsage{
		Quantity: 6, Resolution: "720P", SuperResolutionEnabled: true,
		SuperResolutionResolution: "4K", SuperResolutionVersion: "professional", SuperResolutionFPS: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if order.AmountMicrocredits != 7_800_000 {
		t.Fatalf("total amount = %d, want 7800000", order.AmountMicrocredits)
	}
	if order.EnhancementAmountMicrocredits != 4_800_000 || order.EnhancementPricingRuleID != rule.ID {
		t.Fatalf("enhancement snapshot = %#v", order)
	}
	if order.EnhancementSupplierCostMinMicros != rule.SupplierCostMinMicros ||
		order.EnhancementSupplierCostMaxMicros != rule.SupplierCostMaxMicros ||
		!strings.Contains(order.EnhancementPricingSnapshotJSON, rule.ID) {
		t.Fatalf("enhancement cost snapshot = %#v", order)
	}
}

func TestTaskBillingOrderSelectsReferenceVideoPriceAndFreezesVariant(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ChannelModel{}, &model.ChannelModelPriceTier{}, &model.SystemSetting{}, &model.MembershipPlan{}, &model.MembershipSubscription{}, &model.TeamMember{}); err != nil {
		t.Fatal(err)
	}
	item := model.ChannelModel{
		ID: "seedance", ChannelID: "channel", ModelKey: "doubao-seedance-2-5-260628", Capability: "video",
		BillingMode: "per_second", PriceStrategy: "video_resolution", PriceConfigured: true, Enabled: true,
		PriceTiers: []model.ChannelModelPriceTier{
			{ID: "standard", Resolution: "720P", InputVariant: "standard", UnitPriceMicrocredits: 1_510_000},
			{ID: "reference", Resolution: "720P", InputVariant: "reference_video", UnitPriceMicrocredits: 1_630_000},
		},
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db)}
	order, err := svc.taskBillingOrder("user", &model.Task{ID: "task", Type: "canvas_video"}, map[string]any{
		"mode":            "video",
		"config":          map[string]any{"channelId": "channel", "model": item.ModelKey, "videoSeconds": "5", "vquality": "720p"},
		"referenceVideos": []any{map[string]any{"storageKey": "resource:video"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if order.PriceTierID != "reference" || order.PricingInputVariant != "reference_video" || order.AmountMicrocredits != 8_150_000 {
		t.Fatalf("reference video billing order = %#v", order)
	}
}

func TestTaskBillingOrderSelectsKlingGeneratedAudioPrice(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ChannelModel{}, &model.ChannelModelPriceTier{}, &model.SystemSetting{}, &model.MembershipPlan{}, &model.MembershipSubscription{}, &model.TeamMember{}); err != nil {
		t.Fatal(err)
	}
	item := model.ChannelModel{
		ID: "kling", ChannelID: "channel", ModelKey: kuaiziKlingModel, Capability: "video",
		BillingMode: "per_second", PriceStrategy: "video_resolution", PriceConfigured: true, Enabled: true,
		PriceTiers: []model.ChannelModelPriceTier{
			{ID: "silent", Resolution: "STD", InputVariant: "standard", UnitPriceMicrocredits: 60_000_000},
			{ID: "audio", Resolution: "STD", InputVariant: "standard_audio", UnitPriceMicrocredits: 80_000_000},
		},
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db)}
	order, err := svc.taskBillingOrder("user", &model.Task{ID: "task", Type: "canvas_video"}, map[string]any{
		"mode": "video", "config": map[string]any{"channelId": "channel", "model": item.ModelKey, "videoSeconds": "3", "vquality": "std", "videoGenerateAudio": "true"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if order.PriceTierID != "audio" || order.PricingInputVariant != "standard_audio" || order.AmountMicrocredits != 240_000_000 {
		t.Fatalf("generated-audio billing order = %#v", order)
	}
}

func TestTaskBillingOrderDoesNotApplySeedanceReferenceTierToOtherVideoModels(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ChannelModel{}, &model.ChannelModelPriceTier{}, &model.SystemSetting{}, &model.MembershipPlan{}, &model.MembershipSubscription{}, &model.TeamMember{}); err != nil {
		t.Fatal(err)
	}
	item := model.ChannelModel{ID: "minimax", ChannelID: "channel", ModelKey: "MiniMax-Hailuo-2.3", Capability: "video", BillingMode: "per_second", PriceStrategy: "video_resolution", PriceConfigured: true, Enabled: true,
		PriceTiers: []model.ChannelModelPriceTier{
			{ID: "standard", Resolution: "720P", InputVariant: "standard", UnitPriceMicrocredits: 1_000_000},
			{ID: "reference", Resolution: "720P", InputVariant: "reference_video", UnitPriceMicrocredits: 9_000_000},
		},
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db)}
	order, err := svc.taskBillingOrder("user", &model.Task{ID: "task", Type: "canvas_video"}, map[string]any{
		"mode": "video", "config": map[string]any{"channelId": "channel", "model": item.ModelKey, "videoSeconds": "5", "vquality": "720p"},
		"referenceVideos": []any{map[string]any{"storageKey": "resource:video"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if order.PriceTierID != "standard" || order.PricingInputVariant != "standard" || order.AmountMicrocredits != 5_000_000 {
		t.Fatalf("non-Seedance billing order = %#v", order)
	}
}

func TestTaskBillingOrderDerivesInputImageUsageFromActualReferences(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.ChannelModel{}, &model.ChannelModelPriceTier{}, &model.ModelPricing{}, &model.ModelPricingTier{},
		&model.SystemSetting{}, &model.MembershipPlan{}, &model.MembershipSubscription{}, &model.TeamMember{},
	); err != nil {
		t.Fatal(err)
	}
	item := model.ChannelModel{
		ID: "image", ChannelID: "channel", ModelKey: "image", Capability: "image",
		BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 100_000,
		PriceConfigured: true, Enabled: true,
		PriceTiers: []model.ChannelModelPriceTier{{
			ID: "user-input-overage", UsageMetric: inputImageUsageMetric, IncludedQuantity: 1, UnitPriceMicrocredits: 5_000,
		}},
	}
	pricing := model.ModelPricing{
		ID: "supplier", ChannelID: item.ChannelID, Model: item.ModelKey, Capability: item.Capability, Currency: "CNY",
		Tiers: []model.ModelPricingTier{{
			ID: "supplier-input-overage", Specification: "INPUT_IMAGE", UsageMetric: inputImageUsageMetric,
			IncludedQuantity: 1, SupplierCostMicros: 20_000,
		}},
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&pricing).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db)}
	order, err := svc.taskBillingOrder("user", &model.Task{ID: "task", Type: "canvas_image"}, map[string]any{
		"mode":   "image",
		"config": map[string]any{"channelId": item.ChannelID, "model": item.ModelKey, "count": "1"},
		"referenceImages": []any{
			map[string]any{"storageKey": "resource:one"},
			map[string]any{"storageKey": "resource:two"},
			map[string]any{"storageKey": "resource:three"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if order.UsageAdjustmentActualQuantity != 3 || order.UsageAdjustmentBillableQuantity != 2 ||
		order.UsageAdjustmentUserAmountMicrocredits != 10_000 || order.AmountMicrocredits != 110_000 {
		t.Fatalf("order = %#v", order)
	}
}

func TestValidateSystemProviderInputRejectsCustomCredentials(t *testing.T) {
	input, err := normalizeTaskInput(map[string]any{
		"config": providerConfig{BaseURL: "https://example.com", APIKey: "private-key", Model: "text-model"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSystemProviderInput(input); err == nil {
		t.Fatal("validateSystemProviderInput() error = nil")
	}
}

func TestValidateSystemProviderInputAcceptsSystemChannel(t *testing.T) {
	input, err := normalizeTaskInput(map[string]any{
		"config": providerConfig{
			ChannelID: "channel-1",
			BaseURL:   "/api/ai/system/channel-1",
			APIKey:    "system",
			Model:     "text-model",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSystemProviderInput(input); err != nil {
		t.Fatalf("validateSystemProviderInput() error = %v", err)
	}
}

func TestTaskInputRejectsInlineMedia(t *testing.T) {
	input, err := normalizeTaskInput(map[string]any{
		"referenceImages": []providerMedia{{DataURL: testReferenceImageDataURL}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsInlineMediaDataURL(input) {
		t.Fatal("containsInlineMediaDataURL() = false")
	}
}

func TestCreateSessionRemovesDraftWhenTaskCreationFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.SystemSetting{}, &model.Asset{}, &model.CanvasProject{}, &model.Session{}, &model.Message{}, &model.Task{}, &model.TaskLog{}, &model.Result{}, &model.ApiCallLog{}); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if err := db.Create(&model.Task{ID: newID(), UserID: "user-1", Status: model.TaskStatusQueued, Prompt: "queued"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := &Service{repo: repository.New(db), dataDir: t.TempDir()}
	if _, err := svc.CreateSession("user-1", CreateSessionRequest{Prompt: "new session"}); err == nil {
		t.Fatal("CreateSession() error = nil")
	}
	var sessionCount int64
	var messageCount int64
	if err := db.Model(&model.Session{}).Where("user_id = ?", "user-1").Count(&sessionCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Message{}).Where("user_id = ?", "user-1").Count(&messageCount).Error; err != nil {
		t.Fatal(err)
	}
	if sessionCount != 0 || messageCount != 0 {
		t.Fatalf("draft counts = sessions:%d messages:%d", sessionCount, messageCount)
	}
}

func TestCreateSessionReturnsExistingSessionForSameClientRequestID(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Session{}, &model.Message{}, &model.Task{}, &model.Result{}); err != nil {
		t.Fatal(err)
	}
	existing := model.Session{
		ID:        "launch_request_123",
		UserID:    "user-1",
		ProjectID: "project-1",
		Status:    model.SessionStatusActive,
		Prompt:    "创建三镜头分镜",
	}
	if err := db.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db)}

	detail, err := svc.CreateSession("user-1", CreateSessionRequest{
		ClientRequestID: existing.ID,
		ProjectID:       existing.ProjectID,
		Prompt:          existing.Prompt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Session.ID != existing.ID {
		t.Fatalf("session id = %q, want %q", detail.Session.ID, existing.ID)
	}
	var sessionCount int64
	if err := db.Model(&model.Session{}).Count(&sessionCount).Error; err != nil {
		t.Fatal(err)
	}
	if sessionCount != 1 {
		t.Fatalf("session count = %d, want 1", sessionCount)
	}
}

func TestCreateSessionRejectsReusedClientRequestIDWithDifferentPayload(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Session{}); err != nil {
		t.Fatal(err)
	}
	existing := model.Session{
		ID:        "launch_request_456",
		UserID:    "user-1",
		ProjectID: "project-1",
		Status:    model.SessionStatusActive,
		Prompt:    "原始请求",
	}
	if err := db.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db)}

	_, err = svc.CreateSession("user-1", CreateSessionRequest{
		ClientRequestID: existing.ID,
		ProjectID:       existing.ProjectID,
		Prompt:          "不同请求",
	})
	if err == nil || !strings.Contains(err.Error(), "已用于其他 Agent 请求") {
		t.Fatalf("CreateSession() error = %v", err)
	}
}

func TestValidClientRequestID(t *testing.T) {
	if !validClientRequestID("launch_Request-123") {
		t.Fatal("validClientRequestID() rejected a valid request id")
	}
	for _, value := range []string{"short", "contains space", "含中文", strings.Repeat("a", 37)} {
		if validClientRequestID(value) {
			t.Fatalf("validClientRequestID(%q) = true", value)
		}
	}
}
