package service

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func TestKuaiziTextModelAllowsTokenUsageBilling(t *testing.T) {
	for _, modelKey := range []string{"deepseek-v4-flash", "deepseek-v4-pro"} {
		t.Run(modelKey, func(t *testing.T) {
			item := model.ChannelModel{
				ModelKey:        modelKey,
				Capability:      "text",
				BillingMode:     "token_usage",
				PriceStrategy:   "token",
				PriceConfigured: true,
				Enabled:         true,
			}
			pricing := &model.ModelPricing{
				Currency:               "CNY",
				InputPerMillionMicros:  1_000_000,
				CachedPerMillionMicros: 20_000,
				OutputPerMillionMicros: 2_000_000,
				ExpectedOutputTokens:   8_192,
			}

			snapshot, err := validateTokenUsageModelBilling(item, pricing)
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.InputPerMillionMicros != 1_000_000 || snapshot.CachedPerMillionMicros != 20_000 || snapshot.OutputPerMillionMicros != 2_000_000 || snapshot.MaxOutputTokens != 8_192 {
				t.Fatalf("pricing snapshot = %#v", snapshot)
			}
		})
	}
}

func TestKuaiziTokenBilledTextModelPublicationRequiresSupplierPrice(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	now := time.Now().UTC()
	channel := model.ModelChannel{ID: "token-agent-channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "Token Agent", CreatedAt: now, UpdatedAt: now}
	item := model.ChannelModel{
		ID: "token-agent-model", ChannelID: channel.ID, ModelKey: "deepseek-v4-flash", DisplayName: "DeepSeek V4 Flash",
		AccessPolicy: model.ModelAccessAuthenticated, Capability: "text", BillingMode: "token_usage", PriceStrategy: "token",
		PriceConfigured: true, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	pricing := model.ModelPricing{
		ID: "token-agent-price", ChannelID: channel.ID, Model: item.ModelKey, Capability: "text", Currency: "CNY",
		InputPerMillionMicros: 1_000_000, CachedPerMillionMicros: 20_000, OutputPerMillionMicros: 2_000_000,
		ExpectedOutputTokens: 8_192, CreatedAt: now, UpdatedAt: now,
	}
	for _, row := range []any{&channel, &item, &pricing} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}

	setting, err := svc.UpdateAgentDefaultModelSetting(providerAdmin(), UpdateAgentDefaultModelRequest{ChannelModelID: item.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !setting.Configured || setting.ChannelModelID != item.ID {
		t.Fatalf("Agent setting = %#v", setting)
	}

	if err := db.Delete(&pricing).Error; err != nil {
		t.Fatal(err)
	}
	setting, err = svc.AdminAgentDefaultModelSetting(providerAdmin())
	if err != nil {
		t.Fatal(err)
	}
	if setting.Configured {
		t.Fatalf("model without supplier price remained published = %#v", setting)
	}
}

func TestTokenUsageBillingRejectsNonTextAndMissingPrice(t *testing.T) {
	validItem := model.ChannelModel{
		ModelKey:        "deepseek-v4-pro",
		Capability:      "text",
		BillingMode:     "token_usage",
		PriceStrategy:   "token",
		PriceConfigured: true,
		Enabled:         true,
	}
	validPricing := &model.ModelPricing{
		Currency:               "CNY",
		InputPerMillionMicros:  3_000_000,
		CachedPerMillionMicros: 25_000,
		OutputPerMillionMicros: 6_000_000,
		ExpectedOutputTokens:   16_384,
	}

	tests := []struct {
		name    string
		item    model.ChannelModel
		pricing *model.ModelPricing
	}{
		{name: "missing supplier price", item: validItem, pricing: nil},
		{name: "non text capability", item: func() model.ChannelModel { value := validItem; value.Capability = "image"; return value }(), pricing: validPricing},
		{name: "unmanaged model", item: func() model.ChannelModel { value := validItem; value.ModelKey = "custom-text"; return value }(), pricing: validPricing},
		{name: "fixed unit price", item: func() model.ChannelModel { value := validItem; value.UnitPriceMicrocredits = 1; return value }(), pricing: validPricing},
		{name: "flat price strategy", item: func() model.ChannelModel { value := validItem; value.PriceStrategy = "flat"; return value }(), pricing: validPricing},
		{name: "wrong currency", item: validItem, pricing: func() *model.ModelPricing { value := *validPricing; value.Currency = "USD"; return &value }()},
		{name: "zero input price", item: validItem, pricing: func() *model.ModelPricing { value := *validPricing; value.InputPerMillionMicros = 0; return &value }()},
		{name: "zero output price", item: validItem, pricing: func() *model.ModelPricing { value := *validPricing; value.OutputPerMillionMicros = 0; return &value }()},
		{name: "negative cached price", item: validItem, pricing: func() *model.ModelPricing { value := *validPricing; value.CachedPerMillionMicros = -1; return &value }()},
		{name: "missing output ceiling", item: validItem, pricing: func() *model.ModelPricing { value := *validPricing; value.ExpectedOutputTokens = 0; return &value }()},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := validateTokenUsageModelBilling(testCase.item, testCase.pricing); err == nil {
				t.Fatal("invalid token billing configuration accepted")
			}
		})
	}
}

func TestTokenChargeMicrocreditsUsesIntegerCeiling(t *testing.T) {
	pricing := TokenPricingSnapshot{
		InputPerMillionMicros:  1_000_000,
		CachedPerMillionMicros: 20_000,
		OutputPerMillionMicros: 2_000_000,
		MaxOutputTokens:        8_192,
	}

	amount, err := tokenChargeMicrocredits(pricing, TokenUsageFact{InputTokens: 20_000, OutputTokens: 5_000}, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	if amount != 3_000_000 {
		t.Fatalf("charge = %d, want 3000000", amount)
	}

	oneToken, err := tokenChargeMicrocredits(pricing, TokenUsageFact{InputTokens: 1}, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	if oneToken != 100 {
		t.Fatalf("one-token ceiling = %d, want 100", oneToken)
	}
}

func TestTokenChargeMicrocreditsRejectsInvalidUsageAndOverflow(t *testing.T) {
	valid := TokenPricingSnapshot{InputPerMillionMicros: 1_000_000, CachedPerMillionMicros: 20_000, OutputPerMillionMicros: 2_000_000, MaxOutputTokens: 8_192}
	tests := []struct {
		name       string
		pricing    TokenPricingSnapshot
		usage      TokenUsageFact
		multiplier int64
	}{
		{name: "negative input", pricing: valid, usage: TokenUsageFact{InputTokens: -1}, multiplier: 10_000},
		{name: "cached exceeds input", pricing: valid, usage: TokenUsageFact{InputTokens: 1, CachedTokens: 2}, multiplier: 10_000},
		{name: "negative output", pricing: valid, usage: TokenUsageFact{OutputTokens: -1}, multiplier: 10_000},
		{name: "zero input price", pricing: func() TokenPricingSnapshot { value := valid; value.InputPerMillionMicros = 0; return value }(), usage: TokenUsageFact{InputTokens: 1}, multiplier: 10_000},
		{name: "zero output price", pricing: func() TokenPricingSnapshot { value := valid; value.OutputPerMillionMicros = 0; return value }(), usage: TokenUsageFact{OutputTokens: 1}, multiplier: 10_000},
		{name: "negative cached price", pricing: func() TokenPricingSnapshot { value := valid; value.CachedPerMillionMicros = -1; return value }(), usage: TokenUsageFact{InputTokens: 1}, multiplier: 10_000},
		{name: "zero multiplier", pricing: valid, usage: TokenUsageFact{InputTokens: 1}, multiplier: 0},
		{name: "overflow", pricing: func() TokenPricingSnapshot { value := valid; value.InputPerMillionMicros = math.MaxInt64; return value }(), usage: TokenUsageFact{InputTokens: math.MaxInt64}, multiplier: math.MaxInt64},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := tokenChargeMicrocredits(testCase.pricing, testCase.usage, testCase.multiplier); err == nil {
				t.Fatal("invalid token charge accepted")
			}
		})
	}
}

func TestReserveProxyTokenBillingAtomicallyFreezesMaximumCost(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	now := time.Now().UTC()
	channel := model.ModelChannel{ID: "reserve-token-channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "Token Agent", CreatedAt: now, UpdatedAt: now}
	item := model.ChannelModel{
		ID: "reserve-token-model", ChannelID: channel.ID, ModelKey: "deepseek-v4-flash", DisplayName: "DeepSeek V4 Flash",
		AccessPolicy: model.ModelAccessAuthenticated, Capability: "text", BillingMode: "token_usage", PriceStrategy: "token",
		PriceConfigured: true, Enabled: true, PriceVersion: 3, CreatedAt: now, UpdatedAt: now,
	}
	pricing := model.ModelPricing{
		ID: "reserve-token-price", ChannelID: channel.ID, Model: item.ModelKey, Capability: "text", Currency: "CNY",
		InputPerMillionMicros: 1_000_000, CachedPerMillionMicros: 20_000, OutputPerMillionMicros: 2_000_000,
		ExpectedOutputTokens: 50_000, CreatedAt: now, UpdatedAt: now,
	}
	account := model.CreditAccount{UserID: "reserve-token-user", AvailableMicrocredits: 100_000_000, CreatedAt: now, UpdatedAt: now}
	for _, row := range []any{&channel, &item, &pricing, &account} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	reservation := TokenBillingReservation{
		EstimatedInputTokens: 200_000,
		MaxOutputTokens:      50_000,
		Pricing: TokenPricingSnapshot{
			InputPerMillionMicros: 1_000_000, CachedPerMillionMicros: 20_000, OutputPerMillionMicros: 2_000_000, MaxOutputTokens: 50_000,
		},
		EndpointVersionID:   "endpoint-v1",
		CredentialVersionID: "credential-v1",
	}

	order, err := svc.ReserveProxyTokenBilling(account.UserID, channel.ID, item.ModelKey, "agent", "reserve-token-request", reservation)
	if err != nil {
		t.Fatal(err)
	}
	if order.AmountMicrocredits != 30_000_000 || order.ReservedAmountMicrocredits != 30_000_000 || order.EstimatedInputTokens != 200_000 || order.MaxOutputTokens != 50_000 || order.ProviderEndpointVersionID != "endpoint-v1" || order.ProviderCredentialVersionID != "credential-v1" || order.TokenPricingSnapshotJSON == "" {
		t.Fatalf("reserved order = %#v", order)
	}
	var storedAccount model.CreditAccount
	if err := db.First(&storedAccount, "user_id = ?", account.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if storedAccount.AvailableMicrocredits != 70_000_000 || storedAccount.ReservedMicrocredits != 30_000_000 {
		t.Fatalf("reserved account = %#v", storedAccount)
	}
}

func TestReserveProxyTokenBillingIdempotencyDoesNotReserveTwice(t *testing.T) {
	svc, db, account, channel, item, reservation := tokenReservationServiceFixture(t)
	first, err := svc.ReserveProxyTokenBilling(account.UserID, channel.ID, item.ModelKey, "agent", "same-request", reservation)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.ReserveProxyTokenBilling(account.UserID, channel.ID, item.ModelKey, "agent", "same-request", reservation)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("idempotent order IDs = %q and %q", first.ID, second.ID)
	}
	var storedAccount model.CreditAccount
	if err := db.First(&storedAccount, "user_id = ?", account.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if storedAccount.AvailableMicrocredits != 70_000_000 || storedAccount.ReservedMicrocredits != 30_000_000 {
		t.Fatalf("idempotent account = %#v", storedAccount)
	}
	var reserveCount int64
	if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ? AND type = ?", first.ID, model.CreditLedgerReserve).Count(&reserveCount).Error; err != nil {
		t.Fatal(err)
	}
	if reserveCount != 1 {
		t.Fatalf("reserve ledger count = %d", reserveCount)
	}
}

func TestReserveProxyTokenBillingRejectsNonUnitMultiplier(t *testing.T) {
	svc, _, account, channel, item, reservation := tokenReservationServiceFixture(t)
	policyJSON := `{"signupBonusMicrocredits":0,"checkinBonusMicrocredits":0,"defaultMultiplierBasisPoints":12000,"modelMultiplierBasisPoints":{}}`
	if err := svc.repo.SaveSystemSetting(&model.SystemSetting{Key: creditPolicySettingKey, ValueJSON: policyJSON}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReserveProxyTokenBilling(account.UserID, channel.ID, item.ModelKey, "agent", "non-unit-multiplier", reservation); err == nil {
		t.Fatal("non-1.0 token multiplier was accepted")
	}
}

func TestKuaiziAgentProxyMissingUsageStillSettlesByTaskID(t *testing.T) {
	var billingCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != kuaiziBillingPath || request.Header.Get("ApiKey") != "frozen-token-key" {
			t.Fatalf("billing request = %s, ApiKey=%q", request.URL.Path, request.Header.Get("ApiKey"))
		}
		billingCalls.Add(1)
		_, _ = io.WriteString(writer, "{\"code\":0,\"data\":{\"items\":[{\"order_id\":\"provider-order\",\"amount\":6,\"status\":\"succeeded\",\"task_id\":\"provider-task\",\"task_status\":\"succeeded\",\"task_duration\":1,\"total_tokens\":42,\"created_at\":\"2026-08-15T10:00:00Z\"}]}}")
	}))
	defer server.Close()
	svc, db, account, _, _, reservation := tokenReservationServiceFixture(t)
	installFrozenTokenRuntime(t, svc, db, server.URL, reservation, "frozen-token-key")
	order, err := svc.ReserveProxyTokenBilling(account.UserID, "idempotent-token-channel", "deepseek-v4-flash", "agent", "settle-by-task", reservation)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.MarkBillingRunning(order.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReconcileTokenBillingNow(context.Background(), order.ID, "provider-task", TokenUsageFact{}); err != nil {
		t.Fatal(err)
	}
	var stored model.BillingOrder
	if err := db.First(&stored, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.BillingStatusSettled || stored.ProviderBillingAmount != 6 || stored.ProviderBillingOrderID != "provider-order" || stored.ProviderBillingTotalTokens != 42 || stored.ProviderTaskStatus != "succeeded" || stored.ProviderBillingUnit != "fen" || stored.TokenUsageStatus != "missing" || billingCalls.Load() != 1 {
		t.Fatalf("settled order = %#v, billing calls=%d", stored, billingCalls.Load())
	}
}

func TestKuaiziAgentProxyPendingBillIsReconciledWithoutSecondModelCall(t *testing.T) {
	var billingCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		call := billingCalls.Add(1)
		status := "pending"
		if call > 1 {
			status = "succeeded"
		}
		_, _ = io.WriteString(writer, `{"code":0,"data":{"items":[{"order_id":"provider-order","amount":6,"status":"`+status+`","task_id":"provider-task","task_status":"succeeded","task_duration":1,"total_tokens":42,"created_at":"2026-08-15T10:00:00Z"}]}}`)
	}))
	defer server.Close()
	svc, db, account, _, _, reservation := tokenReservationServiceFixture(t)
	installFrozenTokenRuntime(t, svc, db, server.URL, reservation, "frozen-token-key")
	order, err := svc.ReserveProxyTokenBilling(account.UserID, "idempotent-token-channel", "deepseek-v4-flash", "agent", "pending-bill", reservation)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.MarkBillingRunning(order.ID); err != nil {
		t.Fatal(err)
	}
	usage := TokenUsageFact{InputTokens: 20, CachedTokens: 2, OutputTokens: 5, Available: true}
	if err := svc.ReconcileTokenBillingNow(context.Background(), order.ID, "provider-task", usage); err != nil {
		t.Fatal(err)
	}
	var pending model.BillingOrder
	if err := db.First(&pending, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if pending.TokenUsageStatus != "reported" || pending.InputTokens != 20 || pending.CachedTokens != 2 || pending.OutputTokens != 5 || pending.ProviderBillingOrderID != "provider-order" || pending.ProviderBillingStatus != "pending" || pending.ProviderBillingTotalTokens != 42 {
		t.Fatalf("pending billing facts = %#v", pending)
	}
	if err := db.Model(&model.BillingOrder{}).Where("id = ?", order.ID).Update("next_reconcile_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RunTokenBillingReconciliationBatch(context.Background(), time.Now(), 10); err != nil {
		t.Fatal(err)
	}
	var stored model.BillingOrder
	if err := db.First(&stored, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.BillingStatusSettled || stored.InputTokens != 20 || stored.CachedTokens != 2 || stored.OutputTokens != 5 || stored.TokenUsageStatus != "reported" || billingCalls.Load() != 2 {
		t.Fatalf("reconciled order = %#v, billing calls=%d", stored, billingCalls.Load())
	}
}

func TestKuaiziAgentProxyInvalidUsageDoesNotBlockSupplierSettlement(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"code":0,"data":{"items":[{"order_id":"provider-order","amount":6,"status":"succeeded","task_id":"provider-task","task_status":"succeeded","task_duration":1,"total_tokens":42,"created_at":"2026-08-15T10:00:00Z"}]}}`)
	}))
	defer server.Close()
	svc, db, account, _, _, reservation := tokenReservationServiceFixture(t)
	installFrozenTokenRuntime(t, svc, db, server.URL, reservation, "frozen-token-key")
	order, err := svc.ReserveProxyTokenBilling(account.UserID, "idempotent-token-channel", "deepseek-v4-flash", "agent", "invalid-usage", reservation)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.MarkBillingRunning(order.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReconcileTokenBillingNow(context.Background(), order.ID, "provider-task", TokenUsageFact{InputTokens: 1, CachedTokens: 2, Available: true}); err != nil {
		t.Fatal(err)
	}
	var stored model.BillingOrder
	if err := db.First(&stored, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.BillingStatusSettled || stored.TokenUsageStatus != "invalid" || stored.InputTokens != 0 || stored.ProviderBillingAmount != 6 {
		t.Fatalf("invalid usage settlement = %#v", stored)
	}
}

func TestKuaiziAgentProxyFailedBillWithAmountPersistsObservation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, "{\"code\":0,\"data\":{\"items\":[{\"order_id\":\"provider-failed-order\",\"amount\":1,\"status\":\"failed\",\"task_id\":\"provider-task\",\"task_status\":\"failed\",\"task_duration\":1,\"total_tokens\":9,\"created_at\":\"2026-08-15T10:00:00Z\"}]}}")
	}))
	defer server.Close()
	svc, db, account, _, _, reservation := tokenReservationServiceFixture(t)
	installFrozenTokenRuntime(t, svc, db, server.URL, reservation, "frozen-token-key")
	order, err := svc.ReserveProxyTokenBilling(account.UserID, "idempotent-token-channel", "deepseek-v4-flash", "agent", "failed-with-amount", reservation)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.MarkBillingRunning(order.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReconcileTokenBillingNow(context.Background(), order.ID, "provider-task", TokenUsageFact{}); err != nil {
		t.Fatal(err)
	}
	var stored model.BillingOrder
	if err := db.First(&stored, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.BillingStatusUncertain || stored.ProviderBillingOrderID != "provider-failed-order" || stored.ProviderBillingAmount != 1 || stored.ProviderBillingStatus != "failed" || stored.ProviderBillingTotalTokens != 9 || stored.ProviderTaskStatus != "failed" {
		t.Fatalf("failed bill observation = %#v", stored)
	}
}

func installFrozenTokenRuntime(t *testing.T, svc *Service, db *gorm.DB, baseURL string, reservation TokenBillingReservation, key string) {
	t.Helper()
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	now := time.Now().UTC()
	account := model.ProviderAccount{ID: "token-provider-account", ProviderKind: kuaiziProviderKind, Name: "筷子科技", Enabled: true, CreatedAt: now, UpdatedAt: now}
	endpoint := model.ProviderEndpointVersion{ID: reservation.EndpointVersionID, ProviderAccountID: account.ID, BaseURL: baseURL, Status: "retired", Version: 1, CreatedAt: now}
	credential := model.ProviderCredential{ID: "token-provider-credential", ProviderAccountID: account.ID, Family: kuaiziAccountCredentialFamily, Enabled: true, HealthStatus: "healthy", CreatedAt: now, UpdatedAt: now}
	ciphertext, err := svc.EncryptProviderSecret(account.ID, credential.ID, 1, key)
	if err != nil {
		t.Fatal(err)
	}
	version := model.ProviderCredentialVersion{ID: reservation.CredentialVersionID, ProviderCredentialID: credential.ID, KeyCipher: ciphertext, Status: "retired", Version: 1, CreatedAt: now}
	for _, row := range []any{&account, &endpoint, &credential, &version} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func tokenReservationServiceFixture(t *testing.T) (*Service, *gorm.DB, model.CreditAccount, model.ModelChannel, model.ChannelModel, TokenBillingReservation) {
	t.Helper()
	svc, db := openProviderCredentialService(t)
	now := time.Now().UTC()
	channel := model.ModelChannel{ID: "idempotent-token-channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "Token Agent", CreatedAt: now, UpdatedAt: now}
	item := model.ChannelModel{
		ID: "idempotent-token-model", ChannelID: channel.ID, ModelKey: "deepseek-v4-flash", DisplayName: "DeepSeek V4 Flash",
		AccessPolicy: model.ModelAccessAuthenticated, Capability: "text", BillingMode: "token_usage", PriceStrategy: "token",
		PriceConfigured: true, Enabled: true, PriceVersion: 3, CreatedAt: now, UpdatedAt: now,
	}
	pricing := model.ModelPricing{
		ID: "idempotent-token-price", ChannelID: channel.ID, Model: item.ModelKey, Capability: "text", Currency: "CNY",
		InputPerMillionMicros: 1_000_000, CachedPerMillionMicros: 20_000, OutputPerMillionMicros: 2_000_000,
		ExpectedOutputTokens: 50_000, CreatedAt: now, UpdatedAt: now,
	}
	account := model.CreditAccount{UserID: "idempotent-token-user", AvailableMicrocredits: 100_000_000, CreatedAt: now, UpdatedAt: now}
	for _, row := range []any{&channel, &item, &pricing, &account} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	reservation := TokenBillingReservation{
		EstimatedInputTokens: 200_000, MaxOutputTokens: 50_000,
		Pricing:           TokenPricingSnapshot{InputPerMillionMicros: 1_000_000, CachedPerMillionMicros: 20_000, OutputPerMillionMicros: 2_000_000, MaxOutputTokens: 50_000},
		EndpointVersionID: "endpoint-v1", CredentialVersionID: "credential-v1",
	}
	return svc, db, account, channel, item, reservation
}
