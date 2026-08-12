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
