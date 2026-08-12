package repository

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/testsupport"

	"gorm.io/gorm"
)

func TestPostgresProviderAccountConcurrentEndpointActivationHasOneWinner(t *testing.T) {
	db := openPostgresProviderSchema(t)
	repo := New(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	account := &model.ProviderAccount{ID: "account", ProviderKind: "kuaizi", Name: "筷子科技", Enabled: true, CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateProviderAccount(account); err != nil {
		t.Fatal(err)
	}
	versions := []*model.ProviderEndpointVersion{
		{ID: "endpoint-old", ProviderAccountID: account.ID, BaseURL: "https://old.example.com", Status: "pending", Version: 1, CreatedAt: now},
		{ID: "endpoint-a", ProviderAccountID: account.ID, BaseURL: "https://a.example.com", Status: "pending", Version: 2, CreatedAt: now},
		{ID: "endpoint-b", ProviderAccountID: account.ID, BaseURL: "https://b.example.com", Status: "pending", Version: 3, CreatedAt: now},
	}
	for _, version := range versions {
		if err := repo.CreateProviderEndpointVersion(version); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.ActivateProviderEndpointVersion(account.ID, versions[0].ID, "", now); err != nil {
		t.Fatal(err)
	}
	errorsByCandidate := runConcurrentActivations([]string{versions[1].ID, versions[2].ID}, func(id string) error {
		return repo.ActivateProviderEndpointVersion(account.ID, id, versions[0].ID, now.Add(time.Second))
	})
	assertOneProviderActivationWinner(t, errorsByCandidate)
	var count int64
	if err := db.Model(&model.ProviderEndpointVersion{}).Where("provider_account_id = ? AND status = ?", account.ID, "active").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("active endpoint count = %d, err=%v", count, err)
	}
}

func TestPostgresProviderCredentialConcurrentVersionActivationHasOneWinner(t *testing.T) {
	db := openPostgresProviderSchema(t)
	repo := New(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	account := &model.ProviderAccount{ID: "account", ProviderKind: "kuaizi", Name: "筷子科技", Enabled: true, CreatedAt: now, UpdatedAt: now}
	credential := &model.ProviderCredential{ID: "credential", ProviderAccountID: account.ID, Family: "seedance", Enabled: true, ConcurrencyLimit: 2, CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateProviderAccount(account); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateProviderCredential(credential); err != nil {
		t.Fatal(err)
	}
	versions := []*model.ProviderCredentialVersion{
		{ID: "key-old", ProviderCredentialID: credential.ID, KeyCipher: "enc:provider:v1:old", KeyFingerprint: "old", Status: "pending", Version: 1, CreatedAt: now},
		{ID: "key-a", ProviderCredentialID: credential.ID, KeyCipher: "enc:provider:v1:a", KeyFingerprint: "a", Status: "pending", Version: 2, CreatedAt: now},
		{ID: "key-b", ProviderCredentialID: credential.ID, KeyCipher: "enc:provider:v1:b", KeyFingerprint: "b", Status: "pending", Version: 3, CreatedAt: now},
	}
	for _, version := range versions {
		if err := repo.CreateProviderCredentialVersion(version); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.ActivateProviderCredentialVersion(credential.ID, versions[0].ID, "", now); err != nil {
		t.Fatal(err)
	}
	errorsByCandidate := runConcurrentActivations([]string{versions[1].ID, versions[2].ID}, func(id string) error {
		return repo.ActivateProviderCredentialVersion(credential.ID, id, versions[0].ID, now.Add(time.Second))
	})
	assertOneProviderActivationWinner(t, errorsByCandidate)
	var count int64
	if err := db.Model(&model.ProviderCredentialVersion{}).Where("provider_credential_id = ? AND status = ?", credential.ID, "active").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("active credential version count = %d, err=%v", count, err)
	}
}

func TestPostgresKuaiziAccountCredentialMigrationCommitsOneSharedFact(t *testing.T) {
	db := openPostgresProviderSchema(t)
	repo := New(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	account := model.ProviderAccount{ID: "kuaizi-account", ProviderKind: "kuaizi", Name: "筷子科技", Enabled: true, CreatedAt: now, UpdatedAt: now}
	channel := model.ModelChannel{ID: "kuaizi-channel", Scope: model.ChannelScopeSystem, Name: "筷子科技", Enabled: true, CreatedAt: now, UpdatedAt: now}
	legacy := []model.ProviderCredential{
		{ID: "credential-gpt", ProviderAccountID: account.ID, Family: "gpt", Enabled: true, CreatedAt: now, UpdatedAt: now},
		{ID: "credential-seedance", ProviderAccountID: account.ID, Family: "seedance", Enabled: true, CreatedAt: now, UpdatedAt: now},
	}
	versions := []model.ProviderCredentialVersion{
		{ID: "version-gpt", ProviderCredentialID: legacy[0].ID, KeyCipher: "frozen-gpt", Status: "active", Version: 1, CreatedAt: now},
		{ID: "version-seedance", ProviderCredentialID: legacy[1].ID, KeyCipher: "frozen-seedance", Status: "active", Version: 1, CreatedAt: now},
	}
	for _, row := range []any{&account, &channel, &legacy[0], &legacy[1], &versions[0], &versions[1]} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	models := []model.ChannelModel{
		{ID: "model-gpt", ChannelID: channel.ID, ProviderCredentialID: legacy[0].ID, ModelKey: "gpt-5.5", Capability: "text", CreatedAt: now, UpdatedAt: now},
		{ID: "model-seedance", ChannelID: channel.ID, ProviderCredentialID: legacy[1].ID, ModelKey: "doubao-seedance-2-5-260628", Capability: "video", CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&models).Error; err != nil {
		t.Fatal(err)
	}
	activatedAt := now
	shared := model.ProviderCredential{ID: "credential-account", ProviderAccountID: account.ID, Family: "account", Enabled: true, CreatedAt: now, UpdatedAt: now}
	sharedVersion := model.ProviderCredentialVersion{ID: "version-account", ProviderCredentialID: shared.ID, KeyCipher: "shared", Status: "active", Version: 1, ActivatedAt: &activatedAt, CreatedAt: now}
	audit := model.AdminAuditEvent{ID: "audit-account-migration", ActorUserID: "system-migration", Action: "provider.credential.migrate_account", TargetType: "provider_account", TargetID: account.ID, Summary: "账号凭据迁移", MetadataJSON: `{}`, CreatedAt: now}
	if err := repo.MigrateKuaiziAccountCredential(KuaiziAccountCredentialMigration{
		AccountID: account.ID, SharedCredential: shared, SharedVersion: sharedVersion,
		LegacyCredentialVersionIDs: map[string]string{legacy[0].ID: versions[0].ID, legacy[1].ID: versions[1].ID}, Audit: audit,
	}); err != nil {
		t.Fatal(err)
	}
	var rebound int64
	if err := db.Model(&model.ChannelModel{}).Where("provider_credential_id = ?", shared.ID).Count(&rebound).Error; err != nil || rebound != 2 {
		t.Fatalf("rebound models = %d, err=%v", rebound, err)
	}
	var enabledLegacy int64
	if err := db.Model(&model.ProviderCredential{}).Where("id IN ? AND enabled = ?", []string{legacy[0].ID, legacy[1].ID}, true).Count(&enabledLegacy).Error; err != nil || enabledLegacy != 0 {
		t.Fatalf("enabled legacy = %d, err=%v", enabledLegacy, err)
	}
	var frozenVersions int64
	if err := db.Model(&model.ProviderCredentialVersion{}).Where("id IN ?", []string{versions[0].ID, versions[1].ID}).Count(&frozenVersions).Error; err != nil || frozenVersions != 2 {
		t.Fatalf("frozen legacy versions = %d, err=%v", frozenVersions, err)
	}
	var audits int64
	if err := db.Model(&model.AdminAuditEvent{}).Where("id = ?", audit.ID).Count(&audits).Error; err != nil || audits != 1 {
		t.Fatalf("migration audit count = %d, err=%v", audits, err)
	}
}

func TestPostgresProviderAccountIntegrityRejectsWrongIndexWithoutDeletion(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	row := &model.ProviderEndpointVersion{ID: "endpoint", ProviderAccountID: "account", BaseURL: "https://api.example.com", Status: "pending", Version: 1, CreatedAt: now}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX idx_provider_endpoint_active ON provider_endpoint_versions(provider_account_id, status) WHERE status = 'active'`).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureProviderIntegritySchema(db); err == nil || !strings.Contains(err.Error(), "idx_provider_endpoint_active") {
		t.Fatalf("wrong provider index error = %v", err)
	}
	var count int64
	if err := db.Model(&model.ProviderEndpointVersion{}).Where("id = ?", row.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("provider row changed after failed verification: count=%d err=%v", count, err)
	}
}

func TestPostgresProviderAccountIntegrityRejectsMissingPredicateWithoutDeletion(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	row := &model.ProviderEndpointVersion{ID: "endpoint-predicate", ProviderAccountID: "account", BaseURL: "https://predicate.example.com", Status: "pending", Version: 1, CreatedAt: now}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX idx_provider_endpoint_active ON provider_endpoint_versions(provider_account_id)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureProviderIntegritySchema(db); err == nil || !strings.Contains(err.Error(), "idx_provider_endpoint_active") {
		t.Fatalf("missing PostgreSQL provider predicate error = %v", err)
	}
	var stored model.ProviderEndpointVersion
	if err := db.First(&stored, "id = ?", row.ID).Error; err != nil {
		t.Fatalf("PostgreSQL provider row was removed: %v", err)
	}
	if stored.BaseURL != row.BaseURL || stored.Status != row.Status {
		t.Fatalf("PostgreSQL provider row was overwritten: %#v", stored)
	}
}

func TestPostgresChannelModelVariantSupportsFourTiers(t *testing.T) {
	db := openPostgresProviderSchema(t)
	now := time.Now().UTC()
	channel := &model.ModelChannel{ID: "channel", Scope: model.ChannelScopeSystem, Name: "Kuaizi", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(channel).Error; err != nil {
		t.Fatal(err)
	}
	channelModel := &model.ChannelModel{ID: "model", ChannelID: channel.ID, ModelKey: "seedance-2.5", DisplayName: "Seedance 2.5", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(channelModel).Error; err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"standard", "first_frame", "last_frame", "reference"} {
		tier := &model.ChannelModelPriceTier{ID: "variant-" + variant, ChannelModelID: "model", Resolution: "1080p", InputVariant: variant, CreatedAt: now, UpdatedAt: now}
		if err := db.Create(tier).Error; err != nil {
			t.Fatalf("create variant %s: %v", variant, err)
		}
	}
	duplicate := &model.ChannelModelPriceTier{ID: "duplicate", ChannelModelID: "model", Resolution: "1080p", InputVariant: "standard", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(duplicate).Error; err == nil {
		t.Fatal("duplicate PostgreSQL channel model variant was accepted")
	}
}

func openPostgresProviderSchema(t *testing.T) *gorm.DB {
	t.Helper()
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureProviderIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func runConcurrentActivations(ids []string, activate func(string) error) []error {
	start := make(chan struct{})
	results := make([]error, len(ids))
	var wait sync.WaitGroup
	for index, id := range ids {
		wait.Add(1)
		go func(index int, id string) {
			defer wait.Done()
			<-start
			results[index] = activate(id)
		}(index, id)
	}
	close(start)
	wait.Wait()
	return results
}

func assertOneProviderActivationWinner(t *testing.T, results []error) {
	t.Helper()
	winners := 0
	conflicts := 0
	for _, err := range results {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, ErrProviderActivationConflict):
			conflicts++
		default:
			t.Fatalf("activation returned unexpected error: %v", err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("activation results = %#v, want one winner and one conflict", results)
	}
}
