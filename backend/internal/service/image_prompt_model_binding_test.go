package service

import (
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

type imagePromptModelFixture struct {
	svc        *Service
	db         *gorm.DB
	model      model.ChannelModel
	channel    model.ModelChannel
	account    model.ProviderAccount
	endpoint   model.ProviderEndpointVersion
	credential model.ProviderCredential
	version    model.ProviderCredentialVersion
}

func TestFreezeImagePromptModelIncludesProviderVersions(t *testing.T) {
	fixture := newImagePromptModelFixture(t)

	binding, err := fixture.svc.freezeImagePromptModelBinding()
	if err != nil {
		t.Fatalf("freeze image prompt model: %v", err)
	}
	if binding.CatalogRecordID != fixture.model.ID || binding.ModelKey != fixture.model.ModelKey || binding.ConfigurationRevision != fixture.model.UpdatedAt.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("frozen catalog binding = %#v", binding)
	}
	if binding.ProviderEndpointVersionID != fixture.endpoint.ID || binding.ProviderCredentialVersionID != fixture.version.ID {
		t.Fatalf("frozen provider binding = %#v", binding)
	}
}

func TestFreezeImagePromptModelIsIndependentFromAgentDefaultModel(t *testing.T) {
	fixture := newImagePromptModelFixture(t)
	now := fixture.model.UpdatedAt
	directChannel := model.ModelChannel{
		ID: "direct-agent-channel", Scope: model.ChannelScopeSystem, Enabled: true,
		Name: "Direct Agent", BaseURL: "https://api.deepseek.com", APIKey: "direct-key",
		APIFormat: "openai", InterfaceType: model.ChannelInterfaceOpenAIResponse,
		CreatedAt: now, UpdatedAt: now,
	}
	directModel := model.ChannelModel{
		ID: "direct-agent-model", ChannelID: directChannel.ID, ModelKey: "deepseek-v4-flash",
		DisplayName: "Direct DeepSeek", AccessPolicy: model.ModelAccessAuthenticated,
		Capability: "text", BillingMode: "fixed_request", PriceStrategy: "flat",
		UnitPriceMicrocredits: 100, PriceConfigured: true, Enabled: true,
		CreatedAt: now, UpdatedAt: now,
	}
	for _, row := range []interface{}{&directChannel, &directModel} {
		if err := fixture.db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fixture.svc.UpdateAgentDefaultModelSetting(providerAdmin(), UpdateAgentDefaultModelRequest{ChannelModelID: directModel.ID}); err != nil {
		t.Fatal(err)
	}

	binding, err := fixture.svc.freezeImagePromptModelBinding()
	if err != nil {
		t.Fatalf("freeze dedicated image prompt model: %v", err)
	}
	if binding.CatalogRecordID != fixture.model.ID || binding.ProviderEndpointVersionID != fixture.endpoint.ID || binding.ProviderCredentialVersionID != fixture.version.ID {
		t.Fatalf("image prompt binding followed Agent default model = %#v", binding)
	}
}

func TestFreezeImagePromptModelRejectsUnversionedOrPartialRuntime(t *testing.T) {
	t.Run("direct channel", func(t *testing.T) {
		fixture := newImagePromptModelFixture(t)
		direct := model.ModelChannel{
			ID: "direct-channel", Scope: model.ChannelScopeSystem, Enabled: true,
			Name: "Direct", BaseURL: "https://direct.example/v1", APIKey: "direct-key",
			APIFormat: "openai", InterfaceType: model.ChannelInterfaceChatCompletion,
			CreatedAt: fixture.model.CreatedAt, UpdatedAt: fixture.model.UpdatedAt,
		}
		if err := fixture.db.Create(&direct).Error; err != nil {
			t.Fatal(err)
		}
		if err := fixture.db.Model(&fixture.model).Updates(map[string]interface{}{
			"channel_id": direct.ID, "provider_credential_id": "",
		}).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.svc.freezeImagePromptModelBinding(); err == nil {
			t.Fatal("unversioned direct channel was accepted")
		}
	})

	t.Run("missing active endpoint", func(t *testing.T) {
		fixture := newImagePromptModelFixture(t)
		if err := fixture.db.Model(&fixture.endpoint).Update("status", "pending").Error; err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.svc.freezeImagePromptModelBinding(); err == nil {
			t.Fatal("partial provider runtime was accepted")
		}
	})
}

func TestVerifyFrozenImagePromptModelAcceptsRetiredProviderVersions(t *testing.T) {
	fixture := newImagePromptModelFixture(t)
	binding, err := fixture.svc.freezeImagePromptModelBinding()
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&fixture.endpoint).Update("status", "retired").Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&fixture.version).Update("status", "retired").Error; err != nil {
		t.Fatal(err)
	}

	if err := fixture.svc.verifyFrozenImagePromptModel(binding); err != nil {
		t.Fatalf("retired frozen versions rejected: %v", err)
	}
}

func TestVerifyFrozenImagePromptModelRejectsChangedOrMismatchedFacts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*imagePromptModelFixture, *imagePromptSpecialistModelBindingV1)
	}{
		{name: "changed model revision", mutate: func(_ *imagePromptModelFixture, binding *imagePromptSpecialistModelBindingV1) {
			binding.ConfigurationRevision = "other-revision"
		}},
		{name: "mismatched endpoint", mutate: func(_ *imagePromptModelFixture, binding *imagePromptSpecialistModelBindingV1) {
			binding.ProviderEndpointVersionID = "other-endpoint"
		}},
		{name: "mismatched credential", mutate: func(_ *imagePromptModelFixture, binding *imagePromptSpecialistModelBindingV1) {
			binding.ProviderCredentialVersionID = "other-credential"
		}},
		{name: "disabled model", mutate: func(fixture *imagePromptModelFixture, _ *imagePromptSpecialistModelBindingV1) {
			if err := fixture.db.Model(&fixture.model).Update("enabled", false).Error; err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			fixture := newImagePromptModelFixture(t)
			binding, err := fixture.svc.freezeImagePromptModelBinding()
			if err != nil {
				t.Fatal(err)
			}
			item.mutate(fixture, &binding)
			err = fixture.svc.verifyFrozenImagePromptModel(binding)
			var failure *imagePromptSpecialistFailure
			if !errors.As(err, &failure) || failure.Code != imagePromptErrorModelBindingInvalid {
				t.Fatalf("verify error = %#v", err)
			}
		})
	}
}

func newImagePromptModelFixture(t *testing.T) *imagePromptModelFixture {
	t.Helper()
	svc, db := openProviderCredentialService(t)
	now := time.Date(2026, time.September, 7, 12, 0, 0, 0, time.UTC)
	fixture := &imagePromptModelFixture{
		svc: svc,
		db:  db,
		account: model.ProviderAccount{
			ID: "prompt-account", ProviderKind: kuaiziProviderKind, Name: "Prompt Provider",
			Enabled: true, CreatedAt: now, UpdatedAt: now,
		},
		endpoint: model.ProviderEndpointVersion{
			ID: "prompt-endpoint-v1", ProviderAccountID: "prompt-account",
			BaseURL: "https://aiopenapi.kuaizi.cn", Status: "active", Version: 1, CreatedAt: now,
		},
		credential: model.ProviderCredential{
			ID: "prompt-credential", ProviderAccountID: "prompt-account",
			Family: kuaiziAccountCredentialFamily, Enabled: true, HealthStatus: "healthy",
			CreatedAt: now, UpdatedAt: now,
		},
	}
	fixture.channel = model.ModelChannel{
		ID: deterministicKuaiziChatChannelID("gpt"), Scope: model.ChannelScopeSystem,
		Enabled: true, Name: "Prompt GPT", APIFormat: "openai",
		InterfaceType: model.ChannelInterfaceChatCompletion, CreatedAt: now, UpdatedAt: now,
	}
	fixture.model = model.ChannelModel{
		ID: "prompt-model-v1", ChannelID: fixture.channel.ID, ProviderCredentialID: fixture.credential.ID,
		ModelKey: "gpt-5.5", DisplayName: "Prompt GPT", AccessPolicy: model.ModelAccessAuthenticated,
		Capability: "text", BillingMode: "fixed_request", PriceStrategy: "flat",
		UnitPriceMicrocredits: 100, PriceConfigured: true, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	for _, row := range []interface{}{&fixture.account, &fixture.endpoint, &fixture.credential, &fixture.channel, &fixture.model} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	ciphertext, err := svc.EncryptProviderSecret(fixture.account.ID, fixture.credential.ID, 1, "prompt-api-key")
	if err != nil {
		t.Fatal(err)
	}
	fixture.version = model.ProviderCredentialVersion{
		ID: "prompt-credential-v1", ProviderCredentialID: fixture.credential.ID,
		KeyCipher: ciphertext, KeyFingerprint: "fingerprint", Status: "active", Version: 1, CreatedAt: now,
	}
	if err := db.Create(&fixture.version).Error; err != nil {
		t.Fatal(err)
	}
	setting := model.SystemSetting{
		Key: imagePromptModelSettingKey, ValueJSON: `{"channelModelId":"prompt-model-v1"}`,
		UpdatedBy: "test", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&setting).Error; err != nil {
		t.Fatal(err)
	}
	return fixture
}
