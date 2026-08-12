package database

import (
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProviderAccountSchemaCreatesExactIntegrityIndexes(t *testing.T) {
	db := openProviderSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := EnsureProviderIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	for table, columns := range map[string][]string{
		"channel_models":            {"provider_credential_id"},
		"channel_model_price_tiers": {"supplier_reference_currency"},
		"billing_orders":            {"pricing_input_variant"},
		"provider_accounts":         {"provider_kind", "name"},
		"provider_credential_versions": {
			"key_cipher", "key_fingerprint", "last_verification_code", "last_verification_trace_id", "last_balance_subunits", "created_by",
		},
	} {
		for _, column := range columns {
			assertSQLiteNotNullEmptyDefault(t, db, table, column)
		}
	}
	want := map[string]string{
		"idx_provider_endpoint_active":           `CREATE UNIQUE INDEX idx_provider_endpoint_active ON provider_endpoint_versions(provider_account_id) WHERE status = 'active'`,
		"idx_provider_credential_account_family": `CREATE UNIQUE INDEX idx_provider_credential_account_family ON provider_credentials(provider_account_id, family)`,
		"idx_provider_credential_version_active": `CREATE UNIQUE INDEX idx_provider_credential_version_active ON provider_credential_versions(provider_credential_id) WHERE status = 'active'`,
		"idx_channel_model_resolution_variant":   `CREATE UNIQUE INDEX idx_channel_model_resolution_variant ON channel_model_price_tiers(channel_model_id, resolution, input_variant)`,
	}
	for name, expected := range want {
		var actual string
		if err := db.Raw("SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?", name).Scan(&actual).Error; err != nil {
			t.Fatal(err)
		}
		if compactSQL(actual) != compactSQL(expected) {
			t.Fatalf("index %s SQL = %q, want %q", name, actual, expected)
		}
	}
}

func TestProviderAccountSchemaRejectsWrongNamedIndexWithoutChangingRows(t *testing.T) {
	db := openProviderSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	row := model.ProviderEndpointVersion{ID: "endpoint", ProviderAccountID: "account", BaseURL: "https://api.example.com", Status: "pending", Version: 1, CreatedAt: now}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX idx_provider_endpoint_active ON provider_endpoint_versions(provider_account_id, status) WHERE status = 'active'`).Error; err != nil {
		t.Fatal(err)
	}
	err := EnsureProviderIntegritySchema(db)
	if err == nil || !strings.Contains(err.Error(), "idx_provider_endpoint_active") {
		t.Fatalf("wrong provider index error = %v", err)
	}
	var stored model.ProviderEndpointVersion
	if err := db.First(&stored, "id = ?", row.ID).Error; err != nil {
		t.Fatalf("existing provider row was removed: %v", err)
	}
	if stored.BaseURL != row.BaseURL || stored.Status != row.Status {
		t.Fatalf("existing provider row was overwritten: %#v", stored)
	}
}

func TestProviderAccountSchemaRejectsMissingPredicateWithoutChangingRows(t *testing.T) {
	db := openProviderSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	row := model.ProviderEndpointVersion{ID: "endpoint-predicate", ProviderAccountID: "account", BaseURL: "https://predicate.example.com", Status: "pending", Version: 1, CreatedAt: now}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX idx_provider_endpoint_active ON provider_endpoint_versions(provider_account_id)`).Error; err != nil {
		t.Fatal(err)
	}
	err := EnsureProviderIntegritySchema(db)
	if err == nil || !strings.Contains(err.Error(), "idx_provider_endpoint_active") {
		t.Fatalf("missing provider predicate error = %v", err)
	}
	var stored model.ProviderEndpointVersion
	if err := db.First(&stored, "id = ?", row.ID).Error; err != nil {
		t.Fatalf("provider row was removed after predicate failure: %v", err)
	}
	if stored.BaseURL != row.BaseURL || stored.Status != row.Status {
		t.Fatalf("provider row was overwritten after predicate failure: %#v", stored)
	}
}

func TestProviderAccountSchemaRejectsConflictingHistoricalRowsWithoutDeletion(t *testing.T) {
	db := openProviderSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	rows := []model.ProviderEndpointVersion{
		{ID: "endpoint-a", ProviderAccountID: "account", BaseURL: "https://a.example.com", Status: "active", Version: 1, CreatedAt: now},
		{ID: "endpoint-b", ProviderAccountID: "account", BaseURL: "https://b.example.com", Status: "active", Version: 2, CreatedAt: now},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	err := EnsureProviderIntegritySchema(db)
	if err == nil || !strings.Contains(err.Error(), "account") {
		t.Fatalf("duplicate active endpoint error = %v", err)
	}
	var count int64
	if err := db.Model(&model.ProviderEndpointVersion{}).Where("provider_account_id = ?", "account").Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("conflicting endpoint rows changed: count=%d err=%v", count, err)
	}
}

func openProviderSchemaSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return db
}
