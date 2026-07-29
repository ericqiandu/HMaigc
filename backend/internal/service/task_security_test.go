package service

import (
	"strings"
	"testing"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNormalizeTaskInputMakesTypedProviderConfigBillable(t *testing.T) {
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
	); err != nil {
		t.Fatal(err)
	}
	channelModel := model.ChannelModel{
		ID: "model-1", ChannelID: "channel-1", ModelKey: "text-model", Capability: "text",
		BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 100_000, PriceConfigured: true, Enabled: true,
	}
	if err := db.Create(&channelModel).Error; err != nil {
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
