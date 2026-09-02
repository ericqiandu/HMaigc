package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func TestAgentDefaultModelSettingValidatesCatalogAndInvalidatesWithoutFallback(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	admin := providerAdmin()
	now := time.Now().UTC()
	channel := model.ModelChannel{ID: "agent-channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "Agent", InterfaceType: model.ChannelInterfaceOpenAIResponse, CreatedAt: now, UpdatedAt: now}
	textModel := model.ChannelModel{ID: "agent-text", ChannelID: channel.ID, ModelKey: "custom-agent", DisplayName: "Custom Agent", AccessPolicy: model.ModelAccessAuthenticated, Capability: "text", BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 100, PriceConfigured: true, Enabled: true, CreatedAt: now, UpdatedAt: now}
	imageModel := model.ChannelModel{ID: "agent-image", ChannelID: channel.ID, ModelKey: "image-2.0", DisplayName: "Image 2.0", AccessPolicy: model.ModelAccessAuthenticated, Capability: "image", BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 100, PriceConfigured: true, Enabled: true, CreatedAt: now, UpdatedAt: now}
	for _, row := range []any{&channel, &textModel, &imageModel} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.UpdateAgentDefaultModelSetting(&model.User{ID: "user", Role: model.UserRoleUser}, UpdateAgentDefaultModelRequest{ChannelModelID: textModel.ID}); err == nil {
		t.Fatal("non-admin updated Agent default model")
	}
	if _, err := svc.UpdateAgentDefaultModelSetting(admin, UpdateAgentDefaultModelRequest{ChannelModelID: imageModel.ID}); err == nil {
		t.Fatal("image model accepted as Agent default")
	}
	setting, err := svc.UpdateAgentDefaultModelSetting(admin, UpdateAgentDefaultModelRequest{ChannelModelID: textModel.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !setting.Configured || setting.ChannelModelID != textModel.ID || setting.ChannelID != channel.ID || setting.ModelKey != textModel.ModelKey {
		t.Fatalf("saved Agent model = %#v", setting)
	}
	public, err := svc.PublicAgentDefaultModel()
	if err != nil {
		t.Fatal(err)
	}
	if public == nil || public.ChannelModelID != textModel.ID || public.ChannelID != channel.ID || public.ModelKey != textModel.ModelKey {
		t.Fatalf("public Agent model = %#v", public)
	}
	if err := db.Model(&model.ChannelModel{}).Where("id = ?", textModel.ID).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	public, err = svc.PublicAgentDefaultModel()
	if err != nil {
		t.Fatal(err)
	}
	if public != nil {
		t.Fatalf("disabled Agent model leaked as fallback = %#v", public)
	}
	adminView, err := svc.AdminAgentDefaultModelSetting(admin)
	if err != nil {
		t.Fatal(err)
	}
	if adminView.Configured {
		t.Fatalf("disabled Agent model remained configured = %#v", adminView)
	}
}

func TestAgentDefaultModelSettingAcceptsFixedRequestChannelSharingManagedDeepSeekModelKey(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	admin := providerAdmin()
	now := time.Now().UTC()
	channel := model.ModelChannel{
		ID:            "official-deepseek-channel",
		Scope:         model.ChannelScopeSystem,
		Enabled:       true,
		Name:          "DeepSeek",
		BaseURL:       "https://api.deepseek.com",
		APIFormat:     "openai",
		InterfaceType: model.ChannelInterfaceOpenAIResponse,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	item := model.ChannelModel{
		ID:                    "official-deepseek-flash",
		ChannelID:             channel.ID,
		ModelKey:              "deepseek-v4-flash",
		DisplayName:           "DeepSeek V4 Flash",
		AccessPolicy:          model.ModelAccessAuthenticated,
		Capability:            "text",
		BillingMode:           "fixed_request",
		PriceStrategy:         "flat",
		UnitPriceMicrocredits: 1_000_000,
		PriceConfigured:       true,
		Enabled:               true,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}

	setting, err := svc.UpdateAgentDefaultModelSetting(admin, UpdateAgentDefaultModelRequest{ChannelModelID: item.ID})
	if err != nil {
		t.Fatalf("fixed-request DeepSeek channel rejected because another provider uses the same model key: %v", err)
	}
	if !setting.Configured || setting.ChannelModelID != item.ID || setting.ChannelID != channel.ID || setting.ModelKey != item.ModelKey {
		t.Fatalf("saved Agent model = %#v", setting)
	}
	public, err := svc.PublicAgentDefaultModel()
	if err != nil {
		t.Fatal(err)
	}
	if public == nil || public.ChannelModelID != item.ID || public.ChannelID != channel.ID || public.ModelKey != item.ModelKey {
		t.Fatalf("direct DeepSeek Agent model was filtered after it was saved = %#v", public)
	}
}

func TestAgentDefaultModelSettingRejectsTextModelWithUnsupportedDecisionInterface(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	admin := providerAdmin()
	now := time.Now().UTC()
	channel := model.ModelChannel{
		ID: "unsupported-agent-channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "Unsupported",
		InterfaceType: model.ChannelInterfaceOpenAIImage, CreatedAt: now, UpdatedAt: now,
	}
	item := model.ChannelModel{
		ID: "unsupported-agent-model", ChannelID: channel.ID, ModelKey: "text-over-image-protocol",
		AccessPolicy: model.ModelAccessAuthenticated, Capability: "text", BillingMode: "fixed_request", PriceStrategy: "flat",
		UnitPriceMicrocredits: 100, PriceConfigured: true, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := svc.UpdateAgentDefaultModelSetting(admin, UpdateAgentDefaultModelRequest{ChannelModelID: item.ID}); err == nil {
		t.Fatal("text model using an unsupported Agent decision interface was accepted")
	}
}

func TestAgentDefaultModelSettingAndAuditRollbackTogether(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	admin := providerAdmin()
	now := time.Now().UTC()
	channel := model.ModelChannel{ID: "agent-channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "Agent", InterfaceType: model.ChannelInterfaceOpenAIResponse, CreatedAt: now, UpdatedAt: now}
	models := []model.ChannelModel{
		{ID: "agent-text-a", ChannelID: channel.ID, ModelKey: "gpt-5.5", AccessPolicy: model.ModelAccessAuthenticated, Capability: "text", BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 100, PriceConfigured: true, Enabled: true, CreatedAt: now, UpdatedAt: now},
		{ID: "agent-text-b", ChannelID: channel.ID, ModelKey: "deepseek-v4-pro", AccessPolicy: model.ModelAccessAuthenticated, Capability: "text", BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 80, PriceConfigured: true, Enabled: true, CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateAgentDefaultModelSetting(admin, UpdateAgentDefaultModelRequest{ChannelModelID: models[0].ID}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_agent_setting_audit BEFORE INSERT ON admin_audit_events WHEN NEW.target_id = 'agent_default_model' BEGIN SELECT RAISE(ABORT, 'reject audit'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateAgentDefaultModelSetting(admin, UpdateAgentDefaultModelRequest{ChannelModelID: models[1].ID}); err == nil {
		t.Fatal("audit failure accepted")
	}
	stored, err := svc.repo.SystemSetting(agentDefaultModelSettingKey)
	if err != nil {
		t.Fatal(err)
	}
	value, err := parseAgentDefaultModelSetting(stored)
	if err != nil {
		t.Fatal(err)
	}
	if value.ChannelModelID != models[0].ID {
		t.Fatalf("setting changed after audit rollback = %#v", value)
	}
}

func TestAgentDefaultModelSettingRejectsCorruptStoredValue(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	if err := db.Create(&model.SystemSetting{Key: agentDefaultModelSettingKey, ValueJSON: `{`}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublicAgentDefaultModel(); err == nil {
		t.Fatal("corrupt Agent setting was silently ignored")
	}
	if _, err := svc.repo.SystemSetting("missing"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing setting error = %v", err)
	}
}

func TestPublicAgentDefaultModelRejectsManagedCredentialThatBecameUnhealthy(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	admin := providerAdmin()
	now := time.Now().UTC()
	account := model.ProviderAccount{ID: "agent-account", ProviderKind: kuaiziProviderKind, Name: "筷子", Enabled: true, CreatedAt: now, UpdatedAt: now}
	endpoint := model.ProviderEndpointVersion{ID: "agent-endpoint", ProviderAccountID: account.ID, BaseURL: "https://aiopenapi.kuaizi.cn", Status: "active", Version: 1, CreatedAt: now}
	credential := model.ProviderCredential{ID: "agent-credential", ProviderAccountID: account.ID, Family: kuaiziAccountCredentialFamily, Enabled: true, HealthStatus: "healthy", CreatedAt: now, UpdatedAt: now}
	version := model.ProviderCredentialVersion{ID: "agent-version", ProviderCredentialID: credential.ID, KeyCipher: "encrypted", Status: "active", Version: 1, CreatedAt: now}
	channel := model.ModelChannel{ID: deterministicKuaiziChatChannelID("gpt"), Scope: model.ChannelScopeSystem, Enabled: true, Name: "GPT", InterfaceType: model.ChannelInterfaceChatCompletion, CreatedAt: now, UpdatedAt: now}
	item := model.ChannelModel{ID: "agent-managed", ChannelID: channel.ID, ModelKey: "gpt-5.5", ProviderCredentialID: credential.ID, Capability: "text", BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 100, PriceConfigured: true, Enabled: true, AccessPolicy: model.ModelAccessAuthenticated, CreatedAt: now, UpdatedAt: now}
	for _, row := range []any{&account, &endpoint, &credential, &version, &channel, &item} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.UpdateAgentDefaultModelSetting(admin, UpdateAgentDefaultModelRequest{ChannelModelID: item.ID}); err != nil {
		t.Fatal(err)
	}
	if selected, err := svc.PublicAgentDefaultModel(); err != nil || selected == nil {
		t.Fatalf("healthy Agent default = %#v, %v", selected, err)
	}
	if err := db.Model(&model.ProviderCredential{}).Where("id = ?", credential.ID).Update("health_status", "unavailable").Error; err != nil {
		t.Fatal(err)
	}
	if selected, err := svc.PublicAgentDefaultModel(); err != nil || selected != nil {
		t.Fatalf("unhealthy Agent default leaked = %#v, %v", selected, err)
	}
}

func TestAgentDefaultVisionModelSettingFreezesOnlyEligibleVisionModel(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	now := time.Now().UTC()
	channel := model.ModelChannel{
		ID: "vision-direct-channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "DeepSeek Vision",
		BaseURL: "https://api.deepseek.com", APIKey: "vision-secret", APIFormat: "openai",
		InterfaceType: model.ChannelInterfaceOpenAIResponse, CreatedAt: now, UpdatedAt: now,
	}
	item := model.ChannelModel{
		ID: "vision-model", ChannelID: channel.ID, ModelKey: "deepseek-v4-flash-vision-exp", DisplayName: "DeepSeek Vision",
		AccessPolicy: model.ModelAccessAuthenticated, Capability: "vision", BillingMode: "token_usage", PriceStrategy: "token",
		PriceConfigured: true, Enabled: true, PriceVersion: 7, CreatedAt: now, UpdatedAt: now,
	}
	pricing := model.ModelPricing{
		ID: "vision-pricing", ChannelID: channel.ID, Model: item.ModelKey, Capability: "vision", Currency: "CNY",
		InputPerMillionMicros: 1_000_000, CachedPerMillionMicros: 20_000,
		OutputPerMillionMicros: 2_000_000, MaxOutputTokens: 8_192, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&pricing).Error; err != nil {
		t.Fatal(err)
	}
	admin := providerAdmin()
	setting, err := svc.UpdateAgentDefaultVisionModelSetting(admin, UpdateAgentDefaultVisionModelRequest{ChannelModelID: item.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !setting.Configured || setting.ChannelModelID != item.ID || setting.ChannelID != channel.ID || setting.ModelKey != item.ModelKey {
		t.Fatalf("vision setting = %#v", setting)
	}
	stored, err := svc.AdminAgentDefaultVisionModelSetting(admin)
	if err != nil || !stored.Configured || stored.ChannelModelID != item.ID {
		t.Fatalf("stored vision setting = %#v, error = %v", stored, err)
	}
	var audit model.AdminAuditEvent
	if err := db.Where("action = ? AND target_id = ?", "agent_default_vision_model.update", agentDefaultVisionModelSettingKey).Take(&audit).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ChannelModel{}).Where("id = ?", item.ID).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	invalidated, err := svc.AdminAgentDefaultVisionModelSetting(admin)
	if err != nil || invalidated.Configured {
		t.Fatalf("disabled vision setting = %#v, error = %v", invalidated, err)
	}
}

func TestAgentDefaultVisionModelSettingRejectsIneligibleCommercialFacts(t *testing.T) {
	tests := []struct {
		name          string
		interfaceType model.ChannelInterfaceType
		capability    string
		billingMode   string
		priceStrategy string
		apiKey        string
		withPricing   bool
	}{
		{name: "text capability", interfaceType: model.ChannelInterfaceChatCompletion, capability: "text", billingMode: "token_usage", priceStrategy: "token", apiKey: "secret", withPricing: true},
		{name: "unsupported interface", interfaceType: model.ChannelInterfaceOpenAIImage, capability: "vision", billingMode: "token_usage", priceStrategy: "token", apiKey: "secret", withPricing: true},
		{name: "fixed billing", interfaceType: model.ChannelInterfaceChatCompletion, capability: "vision", billingMode: "fixed_request", priceStrategy: "flat", apiKey: "secret", withPricing: true},
		{name: "missing price", interfaceType: model.ChannelInterfaceChatCompletion, capability: "vision", billingMode: "token_usage", priceStrategy: "token", apiKey: "secret"},
		{name: "missing direct credential", interfaceType: model.ChannelInterfaceChatCompletion, capability: "vision", billingMode: "token_usage", priceStrategy: "token", withPricing: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			svc, db := openProviderCredentialService(t)
			now := time.Now().UTC()
			channel := model.ModelChannel{ID: "vision-ineligible-channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "Vision", BaseURL: "https://api.deepseek.com", APIKey: testCase.apiKey, InterfaceType: testCase.interfaceType, CreatedAt: now, UpdatedAt: now}
			item := model.ChannelModel{ID: "vision-ineligible-model", ChannelID: channel.ID, ModelKey: "deepseek-v4-flash-vision-exp", AccessPolicy: model.ModelAccessAuthenticated, Capability: testCase.capability, BillingMode: testCase.billingMode, PriceStrategy: testCase.priceStrategy, PriceConfigured: true, Enabled: true, PriceVersion: 1, CreatedAt: now, UpdatedAt: now}
			if err := db.Create(&channel).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&item).Error; err != nil {
				t.Fatal(err)
			}
			if testCase.withPricing {
				pricing := model.ModelPricing{ID: "vision-ineligible-pricing", ChannelID: channel.ID, Model: item.ModelKey, Capability: "vision", Currency: "CNY", InputPerMillionMicros: 1_000_000, OutputPerMillionMicros: 2_000_000, MaxOutputTokens: 8_192, CreatedAt: now, UpdatedAt: now}
				if err := db.Create(&pricing).Error; err != nil {
					t.Fatal(err)
				}
			}
			if _, err := svc.UpdateAgentDefaultVisionModelSetting(providerAdmin(), UpdateAgentDefaultVisionModelRequest{ChannelModelID: item.ID}); err == nil {
				t.Fatal("ineligible vision model was accepted")
			}
		})
	}
}

func TestAgentDefaultVisionModelSettingInvalidatesManagedCredentialThatBecameUnhealthy(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	if err := db.Model(&model.ProviderCredential{}).Where("id = ?", fixture.credential.ID).Update("health_status", "unavailable").Error; err != nil {
		t.Fatal(err)
	}
	setting, err := svc.AdminAgentDefaultVisionModelSetting(providerAdmin())
	if err != nil {
		t.Fatal(err)
	}
	if setting.Configured {
		t.Fatalf("unhealthy managed vision model remained configured = %#v", setting)
	}
	if _, _, err := svc.agentRuntimeDefaultVisionModel(); err == nil || !strings.Contains(err.Error(), "不可执行") {
		t.Fatalf("unhealthy managed vision model runtime error = %v", err)
	}
}
