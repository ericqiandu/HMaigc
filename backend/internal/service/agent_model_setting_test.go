package service

import (
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func TestAgentDefaultModelSettingValidatesCatalogAndInvalidatesWithoutFallback(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	admin := providerAdmin()
	now := time.Now().UTC()
	channel := model.ModelChannel{ID: "agent-channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "Agent", CreatedAt: now, UpdatedAt: now}
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

func TestAgentDefaultModelSettingAndAuditRollbackTogether(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	admin := providerAdmin()
	now := time.Now().UTC()
	channel := model.ModelChannel{ID: "agent-channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "Agent", CreatedAt: now, UpdatedAt: now}
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
