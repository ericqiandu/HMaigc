package service

import (
	"math"
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
