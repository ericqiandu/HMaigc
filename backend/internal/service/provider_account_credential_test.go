package service

import (
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func TestMigrateKuaiziAccountCredentialConvergesEqualKeysAndPreservesFrozenLegacyRuntime(t *testing.T) {
	server := newSwitchableKuaiziBalanceServer(t)
	defer server.Close()
	svc, db := openProviderCredentialService(t)
	createLegacyKuaiziAccount(t, db, server.URL)
	createActiveLegacyCredential(t, svc, db, "seedance", "shared-kuaizi-key")
	createActiveLegacyCredential(t, svc, db, "gpt", "shared-kuaizi-key")

	var legacyCredentials []model.ProviderCredential
	if err := db.Order("family ASC").Find(&legacyCredentials).Error; err != nil {
		t.Fatal(err)
	}
	if len(legacyCredentials) != 2 {
		t.Fatalf("legacy credential count = %d", len(legacyCredentials))
	}
	var seedanceVersion model.ProviderCredentialVersion
	if err := db.First(&seedanceVersion, "provider_credential_id = ? AND status = ?", legacyCredentials[1].ID, "active").Error; err != nil {
		t.Fatal(err)
	}
	var endpoint model.ProviderEndpointVersion
	if err := db.First(&endpoint, "status = ?", "active").Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	models := []model.ChannelModel{
		{ID: "model-gpt", ChannelID: "channel-gpt", ProviderCredentialID: legacyCredentials[0].ID, ModelKey: "gpt-5.5", Capability: "text", Enabled: true, PriceConfigured: true, CreatedAt: now, UpdatedAt: now},
		{ID: "model-seedance", ChannelID: "channel-video", ProviderCredentialID: legacyCredentials[1].ID, ModelKey: "doubao-seedance-2-5-260628", Capability: "video", Enabled: true, PriceConfigured: true, CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&models).Error; err != nil {
		t.Fatal(err)
	}

	if err := svc.MigrateKuaiziAccountCredential(); err != nil {
		t.Fatal(err)
	}

	var shared model.ProviderCredential
	if err := db.First(&shared, "provider_account_id = ? AND family = ?", kuaiziAccountID, kuaiziAccountCredentialFamily).Error; err != nil {
		t.Fatal(err)
	}
	if !shared.Enabled || shared.HealthStatus != "healthy" {
		t.Fatalf("shared credential = %#v", shared)
	}
	var rebound int64
	if err := db.Model(&model.ChannelModel{}).Where("provider_credential_id = ?", shared.ID).Count(&rebound).Error; err != nil {
		t.Fatal(err)
	}
	if rebound != 2 {
		t.Fatalf("rebound models = %d", rebound)
	}
	for _, legacy := range legacyCredentials {
		var stored model.ProviderCredential
		if err := db.First(&stored, "id = ?", legacy.ID).Error; err != nil {
			t.Fatal(err)
		}
		if stored.Enabled {
			t.Fatalf("legacy credential %s remains enabled", stored.Family)
		}
	}

	frozen, err := svc.repo.FrozenProviderRuntime(model.Task{ProviderAccountID: kuaiziAccountID, ProviderEndpointVersionID: endpoint.ID, ProviderCredentialVersionID: seedanceVersion.ID})
	if err != nil {
		t.Fatal(err)
	}
	legacyPlaintext, err := NewProviderSecretCipher(svc.dataDir).Decrypt(frozen.ProviderAccountID, frozen.ProviderCredentialID, frozen.CredentialVersion, frozen.KeyCipher)
	if err != nil || legacyPlaintext != "shared-kuaizi-key" {
		t.Fatalf("legacy frozen runtime plaintext=%q err=%v", legacyPlaintext, err)
	}
}

func TestMigrateKuaiziAccountCredentialRejectsDifferentKeysWithoutMutation(t *testing.T) {
	server := newSwitchableKuaiziBalanceServer(t)
	defer server.Close()
	svc, db := openProviderCredentialService(t)
	createLegacyKuaiziAccount(t, db, server.URL)
	createActiveLegacyCredential(t, svc, db, "seedance", "seedance-key")
	createActiveLegacyCredential(t, svc, db, "gpt", "different-gpt-key")

	err := svc.MigrateKuaiziAccountCredential()
	if err == nil || !strings.Contains(err.Error(), "不一致") {
		t.Fatalf("migration error = %v", err)
	}
	var sharedCount int64
	if err := db.Model(&model.ProviderCredential{}).Where("family = ?", kuaiziAccountCredentialFamily).Count(&sharedCount).Error; err != nil {
		t.Fatal(err)
	}
	if sharedCount != 0 {
		t.Fatalf("shared credential count = %d", sharedCount)
	}
	var enabledLegacy int64
	if err := db.Model(&model.ProviderCredential{}).Where("family IN ? AND enabled = ?", []string{"seedance", "gpt"}, true).Count(&enabledLegacy).Error; err != nil {
		t.Fatal(err)
	}
	if enabledLegacy != 2 {
		t.Fatalf("enabled legacy credentials = %d", enabledLegacy)
	}
}

func createLegacyKuaiziAccount(t *testing.T, db *gorm.DB, baseURL string) {
	t.Helper()
	now := time.Now().UTC()
	account := model.ProviderAccount{ID: kuaiziAccountID, ProviderKind: kuaiziProviderKind, Name: "筷子科技", Enabled: true, CreatedAt: now, UpdatedAt: now}
	endpoint := model.ProviderEndpointVersion{ID: newID(), ProviderAccountID: kuaiziAccountID, BaseURL: baseURL, Status: "active", Version: 1, CreatedBy: "test", CreatedAt: now}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
}

func createActiveLegacyCredential(t *testing.T, svc *Service, db *gorm.DB, family string, key string) {
	t.Helper()
	now := time.Now().UTC()
	credential := model.ProviderCredential{ID: deterministicProviderCredentialID(family), ProviderAccountID: kuaiziAccountID, Family: family, HealthStatus: "healthy", HealthCode: "verified", Enabled: true, ConcurrencyLimit: 1, HealthCheckedAt: &now, CreatedAt: now, UpdatedAt: now}
	ciphertext, err := svc.EncryptProviderSecret(kuaiziAccountID, credential.ID, 1, key)
	if err != nil {
		t.Fatal(err)
	}
	version := model.ProviderCredentialVersion{ID: newID(), ProviderCredentialID: credential.ID, KeyCipher: ciphertext, KeyFingerprint: providerKeyFingerprint(key), Status: "active", Version: 1, VerifiedAt: &now, ActivatedAt: &now, LastVerificationCode: "verified", LastBalanceSubunits: "100", CreatedBy: "test", CreatedAt: now}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&version).Error; err != nil {
		t.Fatal(err)
	}
}
