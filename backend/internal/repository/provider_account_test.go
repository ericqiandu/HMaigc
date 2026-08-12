package repository

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProviderCredentialSecretDoesNotMarshalKeyCipher(t *testing.T) {
	encoded, err := json.Marshal(ProviderCredentialSecret{
		ProviderAccountID:    "account",
		ProviderCredentialID: "credential",
		CredentialVersionID:  "version",
		Version:              1,
		KeyCipher:            "sentinel-cipher",
	})
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(encoded)
	if strings.Contains(serialized, "KeyCipher") || strings.Contains(serialized, "sentinel-cipher") {
		t.Fatalf("provider credential secret leaked key cipher into JSON: %s", serialized)
	}
}

func TestProviderAccountEndpointActivationRequiresCurrentVersion(t *testing.T) {
	db := openProviderRepositorySQLite(t)
	repo := New(db)
	now := time.Now().UTC()
	account := &model.ProviderAccount{ID: "account", ProviderKind: "kuaizi", Name: "筷子科技", Enabled: true, CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateProviderAccount(account); err != nil {
		t.Fatal(err)
	}
	first := &model.ProviderEndpointVersion{ID: "endpoint-1", ProviderAccountID: account.ID, BaseURL: "https://api-1.example.com", Status: "pending", Version: 1, CreatedBy: "admin", CreatedAt: now}
	second := &model.ProviderEndpointVersion{ID: "endpoint-2", ProviderAccountID: account.ID, BaseURL: "https://api-2.example.com", Status: "pending", Version: 2, CreatedBy: "admin", CreatedAt: now}
	stale := &model.ProviderEndpointVersion{ID: "endpoint-stale", ProviderAccountID: account.ID, BaseURL: "https://api-stale.example.com", Status: "pending", Version: 3, CreatedBy: "admin", CreatedAt: now}
	for _, version := range []*model.ProviderEndpointVersion{first, second, stale} {
		if err := repo.CreateProviderEndpointVersion(version); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.ActivateProviderEndpointVersion(account.ID, first.ID, "", now); err != nil {
		t.Fatal(err)
	}
	if err := repo.ActivateProviderEndpointVersion(account.ID, second.ID, first.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := repo.ActivateProviderEndpointVersion(account.ID, stale.ID, first.ID, now.Add(2*time.Second)); !errors.Is(err, ErrProviderActivationConflict) {
		t.Fatalf("stale activation error = %v", err)
	}
	assertSingleActiveEndpoint(t, db, account.ID, second.ID)
}

func TestProviderCredentialFamilyAndActiveVersionAreUnique(t *testing.T) {
	db := openProviderRepositorySQLite(t)
	repo := New(db)
	now := time.Now().UTC()
	account := &model.ProviderAccount{ID: "account", ProviderKind: "kuaizi", Name: "筷子科技", Enabled: true, CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateProviderAccount(account); err != nil {
		t.Fatal(err)
	}
	credential := &model.ProviderCredential{ID: "credential", ProviderAccountID: account.ID, Family: "seedance", Enabled: true, ConcurrencyLimit: 2, HealthStatus: "unknown", CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateProviderCredential(credential); err != nil {
		t.Fatal(err)
	}
	duplicateFamily := *credential
	duplicateFamily.ID = "credential-duplicate"
	if err := repo.CreateProviderCredential(&duplicateFamily); err == nil {
		t.Fatal("duplicate provider credential family was accepted")
	}
	first := &model.ProviderCredentialVersion{ID: "key-1", ProviderCredentialID: credential.ID, KeyCipher: "enc:provider:v1:first", KeyFingerprint: "fingerprint-1", Status: "pending", Version: 1, CreatedBy: "admin", CreatedAt: now}
	second := &model.ProviderCredentialVersion{ID: "key-2", ProviderCredentialID: credential.ID, KeyCipher: "enc:provider:v1:second", KeyFingerprint: "fingerprint-2", Status: "pending", Version: 2, CreatedBy: "admin", CreatedAt: now}
	for _, version := range []*model.ProviderCredentialVersion{first, second} {
		if err := repo.CreateProviderCredentialVersion(version); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.ActivateProviderCredentialVersion(credential.ID, first.ID, "", now); err != nil {
		t.Fatal(err)
	}
	if err := repo.ActivateProviderCredentialVersion(credential.ID, second.ID, first.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var active []model.ProviderCredentialVersion
	if err := db.Where("provider_credential_id = ? AND status = ?", credential.ID, "active").Find(&active).Error; err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != second.ID {
		t.Fatalf("active credential versions = %#v", active)
	}
}

func TestProviderEndpointActivationRejectsCredentialRotationAfterValidation(t *testing.T) {
	db := openProviderRepositorySQLite(t)
	repo := New(db)
	now := time.Now().UTC()
	account := &model.ProviderAccount{ID: "account-race", ProviderKind: "kuaizi", Name: "筷子科技", Enabled: true, CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateProviderAccount(account); err != nil {
		t.Fatal(err)
	}
	endpoint1 := &model.ProviderEndpointVersion{ID: "endpoint-race-1", ProviderAccountID: account.ID, BaseURL: "https://api-1.example.com", Status: "pending", Version: 1, CreatedAt: now}
	endpoint2 := &model.ProviderEndpointVersion{ID: "endpoint-race-2", ProviderAccountID: account.ID, BaseURL: "https://api-2.example.com", Status: "pending", Version: 2, CreatedAt: now}
	if err := repo.CreateProviderEndpointVersion(endpoint1); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateProviderEndpointVersion(endpoint2); err != nil {
		t.Fatal(err)
	}
	if err := repo.ActivateProviderEndpointVersion(account.ID, endpoint1.ID, "", now); err != nil {
		t.Fatal(err)
	}
	credential := &model.ProviderCredential{ID: "credential-race", ProviderAccountID: account.ID, Family: "seedance", HealthStatus: "healthy", Enabled: true, CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateProviderCredential(credential); err != nil {
		t.Fatal(err)
	}
	key1 := &model.ProviderCredentialVersion{ID: "key-race-1", ProviderCredentialID: credential.ID, KeyCipher: "cipher-1", KeyFingerprint: "fingerprint-1", Status: "pending", Version: 1, CreatedAt: now}
	key2 := &model.ProviderCredentialVersion{ID: "key-race-2", ProviderCredentialID: credential.ID, KeyCipher: "cipher-2", KeyFingerprint: "fingerprint-2", Status: "pending", Version: 2, CreatedAt: now}
	if err := repo.CreateProviderCredentialVersion(key1); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateProviderCredentialVersion(key2); err != nil {
		t.Fatal(err)
	}
	if err := repo.ActivateProviderCredentialVersion(credential.ID, key1.ID, "", now); err != nil {
		t.Fatal(err)
	}
	if err := repo.ActivateProviderCredentialVersion(credential.ID, key2.ID, key1.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	record := ProviderCredentialVerification{CredentialID: credential.ID, VersionID: key1.ID, HealthStatus: "healthy", HealthCode: "verified", Balance: "10", CheckedAt: now, Verified: true}
	audit := &model.AdminAuditEvent{ID: "audit-race", ActorUserID: "admin", Action: "provider.endpoint.activate", TargetType: "provider_account", TargetID: account.ID, CreatedAt: now}
	err := repo.ActivateProviderEndpointWithCredentialVerifications(account.ID, endpoint2.ID, endpoint1.ID, []ProviderCredentialVerification{record}, now.Add(2*time.Second), audit)
	if !errors.Is(err, ErrProviderActivationConflict) {
		t.Fatalf("credential rotation activation error = %v", err)
	}
	assertSingleActiveEndpoint(t, db, account.ID, endpoint1.ID)
}

func TestProviderTaskFactRejectsDuplicateTaskAndNonemptyProviderTask(t *testing.T) {
	db := openProviderRepositorySQLite(t)
	repo := New(db)
	now := time.Now().UTC()
	first := providerTaskFactFixture("task-1", "provider-task", now)
	if err := repo.CreateProviderTaskFact(first); err != nil {
		t.Fatal(err)
	}
	duplicateTask := providerTaskFactFixture(first.TaskID, "other-provider-task", now)
	if err := repo.CreateProviderTaskFact(duplicateTask); err == nil {
		t.Fatal("duplicate task fact was accepted")
	}
	duplicateProviderTask := providerTaskFactFixture("task-2", first.ProviderTaskID, now)
	if err := repo.CreateProviderTaskFact(duplicateProviderTask); err == nil {
		t.Fatal("duplicate nonempty provider task was accepted")
	}
	for _, taskID := range []string{"empty-provider-task-1", "empty-provider-task-2"} {
		fact := providerTaskFactFixture(taskID, "", now)
		if err := repo.CreateProviderTaskFact(fact); err != nil {
			t.Fatalf("empty provider task id %s: %v", taskID, err)
		}
	}
}

func TestProviderBillingFactIsIdempotentOnlyForMatchingDigest(t *testing.T) {
	db := openProviderRepositorySQLite(t)
	repo := New(db)
	now := time.Now().UTC()
	fact := &model.ProviderBillingFact{
		ID: "billing-1", ProviderTaskFactID: "task-1", ProviderCredentialVersionID: "key-1",
		UpstreamOrderID: "upstream-1", ProviderTaskID: "provider-task-1", AmountSubunits: "1250000",
		BillingStatus: "billed", ProviderTaskStatus: "succeeded", TaskDurationSeconds: 10,
		TotalTokens: "9007199254740993", Description: "seedance 2.5", QueryTraceID: "trace-1",
		PayloadDigest: strings.Repeat("a", 64), BilledAt: now, ObservedAt: now,
	}
	stored, created, err := repo.RecordProviderBillingFact(fact)
	if err != nil || !created || stored.ID != fact.ID {
		t.Fatalf("create billing fact = stored:%#v created:%v err:%v", stored, created, err)
	}
	replay := *fact
	replay.ID = "billing-replay"
	stored, created, err = repo.RecordProviderBillingFact(&replay)
	if err != nil || created || stored.ID != fact.ID {
		t.Fatalf("replay billing fact = stored:%#v created:%v err:%v", stored, created, err)
	}
	conflict := replay
	conflict.ID = "billing-conflict"
	conflict.PayloadDigest = strings.Repeat("b", 64)
	if _, _, err := repo.RecordProviderBillingFact(&conflict); !errors.Is(err, ErrProviderBillingFactConflict) {
		t.Fatalf("conflicting billing fact error = %v", err)
	}
	var facts []model.ProviderBillingFact
	if err := db.Find(&facts).Error; err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || facts[0].PayloadDigest != fact.PayloadDigest {
		t.Fatalf("billing facts changed after conflict: %#v", facts)
	}
}

func openProviderRepositorySQLite(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared&_busy_timeout=5000"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureProviderIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func providerTaskFactFixture(taskID string, providerTaskID string, now time.Time) *model.ProviderTaskFact {
	return &model.ProviderTaskFact{
		TaskID: taskID, BillingOrderID: "order-" + taskID, ProviderAccountID: "account",
		ProviderEndpointVersionID: "endpoint-1", ProviderCredentialID: "credential",
		ProviderCredentialVersionID: "key-1", ChannelModelID: "channel-model", ProviderTaskID: providerTaskID,
		CreateTraceID: "trace-create", RequestedDurationSeconds: 10, ActualDurationSeconds: 10,
		Resolution: "1080p", InputVariant: "standard", ProviderStatus: "succeeded",
		InputImageCount: 1, TotalTokens: "9007199254740993", ReconciliationStatus: "pending",
		CreatedAt: now, UpdatedAt: now,
	}
}

func assertSingleActiveEndpoint(t *testing.T, db *gorm.DB, accountID string, expectedID string) {
	t.Helper()
	var active []model.ProviderEndpointVersion
	if err := db.Where("provider_account_id = ? AND status = ?", accountID, "active").Find(&active).Error; err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != expectedID {
		t.Fatalf("active endpoints = %#v, want %s", active, expectedID)
	}
}
