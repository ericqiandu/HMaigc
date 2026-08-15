package service

import (
	"math"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
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
