package database

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestWatermarkPolicySchemaCreatesTablesTaskFactsAndExactIndexes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []any{
		&model.PolicyPublicationHead{},
		&model.PolicyPublication{},
		&model.UserWatermarkPreference{},
		&model.UserPolicyConsent{},
		&model.UserWatermarkPreferenceEvent{},
	} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("missing watermark table for %T", table)
		}
	}
	for _, column := range []string{"watermark_capability", "watermark_directive", "watermark_parameter_applied", "watermark_parameter_value", "watermark_policy_publication_id", "watermark_policy_version"} {
		if !db.Migrator().HasColumn(&model.Task{}, column) {
			t.Fatalf("tasks.%s was not migrated", column)
		}
	}
	expectedIndexes := map[string]string{
		"idx_policy_publication_version":                    `CREATE UNIQUE INDEX idx_policy_publication_version ON policy_publications(kind, version)`,
		"idx_user_policy_consent":                           `CREATE UNIQUE INDEX idx_user_policy_consent ON user_policy_consents(user_id, policy_publication_id)`,
		"idx_user_watermark_preference_events_user_created": `CREATE INDEX idx_user_watermark_preference_events_user_created ON user_watermark_preference_events(user_id, created_at)`,
	}
	for name, expected := range expectedIndexes {
		var actual string
		if err := db.Raw("SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?", name).Scan(&actual).Error; err != nil {
			t.Fatal(err)
		}
		if canonicalWatermarkIndexSQL(actual) != canonicalWatermarkIndexSQL(expected) {
			t.Fatalf("index %s SQL = %q, want %q", name, actual, expected)
		}
	}
}

func TestWatermarkPolicyIntegrityRejectsWrongExistingIndexDefinition(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DROP INDEX idx_user_policy_consent`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX idx_user_policy_consent ON user_policy_consents(user_id)`).Error; err != nil {
		t.Fatal(err)
	}
	err = EnsureWatermarkPolicyIntegritySchema(db)
	if err == nil || !strings.Contains(err.Error(), "idx_user_policy_consent") {
		t.Fatalf("wrong watermark index error = %v", err)
	}
}

func TestPaymentIntegritySchemaUpgradesLegacyNullsAndCreatesExactIndexes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	legacyDDL := []string{
		`CREATE TABLE membership_orders (id text PRIMARY KEY, order_number text, user_id text, idempotency_key text NULL, request_hash text NULL, team_id text, plan_id text, seats integer, unit_price_cents integer, total_price_cents integer, currency text, status text, payment_provider text, provider_trade_no text, plan_snapshot_json text, resolved_by text, resolution_note text, paid_at datetime, created_at datetime, updated_at datetime)`,
		`CREATE TABLE payment_checkout_sessions (id text PRIMARY KEY, order_type text, order_id text, user_id text, token_hash text, token_cipher text NULL, status text, expires_at datetime, created_at datetime, updated_at datetime)`,
		`CREATE TABLE payment_transactions (id text PRIMARY KEY, order_type text, order_id text, user_id text, provider text, merchant_order_no text, provider_trade_no text NULL, amount_cents integer, currency text, status text, code_url text, failure_code text NULL, failure_reason text NULL, expires_at datetime, paid_at datetime, closed_at datetime, created_at datetime, updated_at datetime)`,
		`CREATE TABLE payment_webhook_events (id text PRIMARY KEY, provider text, provider_event_id text, transaction_id text, merchant_order_no text NULL, provider_trade_no text NULL, currency text NULL, failure_code text NULL, payload_digest text, status text, failure_reason text NULL, received_at datetime, processed_at datetime, created_at datetime, updated_at datetime)`,
	}
	for _, statement := range legacyDDL {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := db.Exec(`INSERT INTO membership_orders (id, order_number, user_id, plan_id, seats, unit_price_cents, total_price_cents, currency, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "legacy-order", "M-LEGACY", "legacy-user", "legacy-plan", 1, 100, 100, "CNY", model.MembershipOrderPaid, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO payment_checkout_sessions (id, order_type, order_id, user_id, token_hash, status, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, "legacy-session", model.PaymentOrderMembership, "legacy-order", "legacy-user", "hash", model.PaymentCheckoutConsumed, now, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO payment_transactions (id, order_type, order_id, user_id, provider, merchant_order_no, provider_trade_no, amount_cents, currency, status, failure_reason, paid_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?)`, "legacy-transaction", model.PaymentOrderMembership, "legacy-order", "legacy-user", model.PaymentProviderWechat, "merchant-legacy", "trade-legacy", 100, "CNY", model.PaymentTransactionPaid, now, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO payment_webhook_events (id, provider, provider_event_id, transaction_id, payload_digest, status, failure_reason, received_at, processed_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?)`, "legacy-event", model.PaymentProviderWechat, "event-legacy", "legacy-transaction", "digest", model.PaymentWebhookProcessed, now, now, now, now).Error; err != nil {
		t.Fatal(err)
	}

	if err := MigrateSchema(db); err != nil {
		t.Fatalf("runtime schema migration: %v", err)
	}
	if err := EnsurePaymentIntegritySchema(db); err != nil {
		t.Fatalf("idempotent payment integrity verification: %v", err)
	}

	for table, columns := range map[string][]string{
		"membership_orders":         {"idempotency_key", "request_hash"},
		"payment_checkout_sessions": {"token_cipher"},
		"payment_transactions":      {"failure_code"},
		"payment_webhook_events":    {"merchant_order_no", "provider_trade_no", "currency", "failure_code"},
	} {
		for _, column := range columns {
			assertSQLiteNotNullEmptyDefault(t, db, table, column)
		}
	}
	var event model.PaymentWebhookEvent
	if err := db.First(&event, "id = ?", "legacy-event").Error; err != nil {
		t.Fatal(err)
	}
	if event.MerchantOrderNo != "merchant-legacy" || event.ProviderTradeNo != "trade-legacy" || event.AmountCents != 100 || event.Currency != "CNY" || event.PaidAt == nil || !event.PaidAt.Equal(now) {
		t.Fatalf("processed webhook facts were not backfilled from the transaction: %#v", event)
	}

	expectedIndexes := map[string]string{
		"idx_membership_order_user_idempotency":   `CREATE UNIQUE INDEX idx_membership_order_user_idempotency ON membership_orders(user_id, idempotency_key) WHERE idempotency_key <> ''`,
		"idx_payment_transactions_payable_order":  `CREATE UNIQUE INDEX idx_payment_transactions_payable_order ON payment_transactions(order_type, order_id) WHERE status IN ('created', 'pending', 'review_required')`,
		"idx_payment_transactions_provider_trade": `CREATE UNIQUE INDEX idx_payment_transactions_provider_trade ON payment_transactions(provider, provider_trade_no) WHERE provider_trade_no <> ''`,
	}
	for name, expected := range expectedIndexes {
		var actual string
		if err := db.Raw("SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?", name).Scan(&actual).Error; err != nil {
			t.Fatal(err)
		}
		if compactSQL(actual) != compactSQL(expected) {
			t.Fatalf("index %s SQL = %q, want %q", name, actual, expected)
		}
	}
}

func TestPaymentIntegritySchemaRejectsWrongExistingPredicate(t *testing.T) {
	db := openPaymentIntegritySQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX idx_payment_transactions_payable_order ON payment_transactions(order_type, order_id) WHERE status IN ('created', 'pending')`).Error; err != nil {
		t.Fatal(err)
	}
	err := EnsurePaymentIntegritySchema(db)
	if err == nil || !strings.Contains(err.Error(), "idx_payment_transactions_payable_order") {
		t.Fatalf("wrong existing predicate error = %v", err)
	}
}

func TestPaymentIntegritySchemaDoesNotTrustWrongExistingIndexName(t *testing.T) {
	db := openPaymentIntegritySQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX idx_wrong_payable_order ON payment_transactions(order_type, order_id) WHERE status IN ('created', 'pending', 'review_required')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := EnsurePaymentIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	var definition string
	if err := db.Raw("SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?", "idx_payment_transactions_payable_order").Scan(&definition).Error; err != nil {
		t.Fatal(err)
	}
	if compactSQL(definition) != compactSQL(`CREATE UNIQUE INDEX idx_payment_transactions_payable_order ON payment_transactions(order_type, order_id) WHERE status IN ('created', 'pending', 'review_required')`) {
		t.Fatalf("expected named payable index was not created exactly: %q", definition)
	}
}

func TestPaymentIntegritySchemaRejectsDuplicatePayableFactsWithoutDeletingRows(t *testing.T) {
	db := openPaymentIntegritySQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	transactions := []model.PaymentTransaction{
		{ID: "payable-a", OrderType: model.PaymentOrderMembership, OrderID: "order-duplicate", UserID: "user-a", Provider: model.PaymentProviderWechat, MerchantOrderNo: "merchant-a", AmountCents: 100, Currency: "CNY", Status: model.PaymentTransactionCreated, CreatedAt: now, UpdatedAt: now},
		{ID: "payable-b", OrderType: model.PaymentOrderMembership, OrderID: "order-duplicate", UserID: "user-a", Provider: model.PaymentProviderAlipay, MerchantOrderNo: "merchant-b", AmountCents: 100, Currency: "CNY", Status: model.PaymentTransactionReviewRequired, CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&transactions).Error; err != nil {
		t.Fatal(err)
	}
	err := EnsurePaymentIntegritySchema(db)
	if err == nil || !strings.Contains(err.Error(), "membership/order-duplicate") || !strings.Contains(err.Error(), "merchant-a") || !strings.Contains(err.Error(), "merchant-b") {
		t.Fatalf("duplicate payable facts error = %v", err)
	}
	var count int64
	if err := db.Model(&model.PaymentTransaction{}).Where("order_id = ?", "order-duplicate").Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("duplicate payable rows changed: count=%d err=%v", count, err)
	}
}

func TestPaymentIntegritySchemaRejectsDuplicateProviderTradeFactsWithoutDeletingRows(t *testing.T) {
	db := openPaymentIntegritySQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	transactions := []model.PaymentTransaction{
		{ID: "trade-a", OrderType: model.PaymentOrderMembership, OrderID: "order-a", UserID: "user-a", Provider: model.PaymentProviderWechat, MerchantOrderNo: "merchant-a", ProviderTradeNo: "provider-duplicate", AmountCents: 100, Currency: "CNY", Status: model.PaymentTransactionPaid, CreatedAt: now, UpdatedAt: now},
		{ID: "trade-b", OrderType: model.PaymentOrderCreditTopup, OrderID: "order-b", UserID: "user-b", Provider: model.PaymentProviderWechat, MerchantOrderNo: "merchant-b", ProviderTradeNo: "provider-duplicate", AmountCents: 200, Currency: "CNY", Status: model.PaymentTransactionPaid, CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&transactions).Error; err != nil {
		t.Fatal(err)
	}
	err := EnsurePaymentIntegritySchema(db)
	if err == nil || !strings.Contains(err.Error(), "membership/order-a") || !strings.Contains(err.Error(), "credit_topup/order-b") || !strings.Contains(err.Error(), "merchant-a") || !strings.Contains(err.Error(), "merchant-b") {
		t.Fatalf("duplicate provider trade facts error = %v", err)
	}
	var count int64
	if err := db.Model(&model.PaymentTransaction{}).Where("provider_trade_no = ?", "provider-duplicate").Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("duplicate provider trade rows changed: count=%d err=%v", count, err)
	}
}

func TestPaymentIntegritySchemaRejectsProcessedWebhookWithoutTransactionFacts(t *testing.T) {
	db := openPaymentIntegritySQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	event := model.PaymentWebhookEvent{ID: "orphan-event", Provider: model.PaymentProviderWechat, ProviderEventID: "orphan-provider-event", TransactionID: "missing-transaction", PayloadDigest: "digest", Status: model.PaymentWebhookProcessed, ReceivedAt: now, ProcessedAt: &now, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	err := EnsurePaymentIntegritySchema(db)
	if err == nil || !strings.Contains(err.Error(), event.ID) || !strings.Contains(err.Error(), event.TransactionID) {
		t.Fatalf("processed webhook missing facts error = %v", err)
	}
}

func openPaymentIntegritySQLite(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func assertSQLiteNotNullEmptyDefault(t *testing.T, db *gorm.DB, table string, column string) {
	t.Helper()
	type columnInfo struct {
		Name       string  `gorm:"column:name"`
		NotNull    int     `gorm:"column:notnull"`
		DefaultSQL *string `gorm:"column:dflt_value"`
	}
	var columns []columnInfo
	if err := db.Raw("PRAGMA table_info(" + table + ")").Scan(&columns).Error; err != nil {
		t.Fatal(err)
	}
	for _, info := range columns {
		if info.Name != column {
			continue
		}
		if info.NotNull != 1 || info.DefaultSQL == nil || (strings.TrimSpace(*info.DefaultSQL) != "''" && strings.TrimSpace(*info.DefaultSQL) != `""`) {
			defaultSQL := "<nil>"
			if info.DefaultSQL != nil {
				defaultSQL = *info.DefaultSQL
			}
			t.Fatalf("%s.%s constraint = notnull:%d default:%q, want NOT NULL DEFAULT ''", table, column, info.NotNull, defaultSQL)
		}
		return
	}
	t.Fatalf("missing column %s.%s", table, column)
}

func compactSQL(value string) string {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\"", ""), "`", "")
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func TestMigrateSchemaBackfillsLegacyEmptyPriceStrategy(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatalf("initial schema migration: %v", err)
	}
	if !db.Migrator().HasColumn(&model.ChannelModel{}, "marketing_copy") {
		t.Fatal("channel_models.marketing_copy was not migrated")
	}
	if !db.Migrator().HasColumn(&model.ChannelModel{}, "promotion_badge") {
		t.Fatal("channel_models.promotion_badge was not migrated")
	}
	if !db.Migrator().HasColumn(&model.ChannelModel{}, "estimated_duration_seconds") {
		t.Fatal("channel_models.estimated_duration_seconds was not migrated")
	}
	if !db.Migrator().HasTable(&model.ChannelVoice{}) ||
		!db.Migrator().HasColumn(&model.ChannelVoice{}, "consent_confirmed_at") ||
		!db.Migrator().HasColumn(&model.ChannelVoice{}, "idempotency_key") ||
		!db.Migrator().HasColumn(&model.ChannelVoice{}, "owner_user_id") {
		t.Fatal("channel_voices commercial audit schema was not migrated")
	}
	if !db.Migrator().HasTable(&model.UserVoiceFavorite{}) {
		t.Fatal("user_voice_favorites schema was not migrated")
	}
	if !db.Migrator().HasTable(&model.ChannelVoicePreview{}) ||
		!db.Migrator().HasColumn(&model.ChannelVoicePreview{}, "provider_trace_id") {
		t.Fatal("channel_voice_previews cache schema was not migrated")
	}
	for _, table := range []interface{}{
		&model.ReferralProfile{},
		&model.ReferralRelationship{},
		&model.ReferralRewardRule{},
		&model.ReferralReward{},
	} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("referral table for %T was not migrated", table)
		}
	}
	if !db.Migrator().HasColumn(&model.OAuthState{}, "referral_code") ||
		!db.Migrator().HasColumn(&model.OAuthState{}, "registration_ip") {
		t.Fatal("OAuth referral context columns were not migrated")
	}
	var miniMaxPricing model.ModelPricing
	if err := db.Preload("Tiers").First(&miniMaxPricing, "channel_id = ? AND model = ? AND capability = ?", "", "MiniMax-H3", "video").Error; err != nil {
		t.Fatalf("load MiniMax H3 default supplier pricing: %v", err)
	}
	if len(miniMaxPricing.Tiers) != 8 {
		t.Fatalf("MiniMax H3 supplier tier count = %d, want 8", len(miniMaxPricing.Tiers))
	}
	tierCosts := make(map[string]int64, len(miniMaxPricing.Tiers))
	for _, tier := range miniMaxPricing.Tiers {
		if len(tier.ID) > 36 {
			t.Fatalf("MiniMax H3 pricing tier ID %q exceeds PostgreSQL varchar(36)", tier.ID)
		}
		tierCosts[tier.Specification] = tier.SupplierCostMicros
	}
	for specification, expected := range map[string]int64{
		"REGENERATE_768P_TO_2K":          300_000,
		"REGENERATE_INPUT_IMAGE_OVERAGE": 150_000,
		"REGENERATE_INPUT_VIDEO_768P":    300_000,
	} {
		if tierCosts[specification] != expected {
			t.Fatalf("MiniMax H3 tier %s cost = %d, want %d", specification, tierCosts[specification], expected)
		}
	}
	for modelName, expected := range map[string]int64{
		"speech-2.8-hd":         3_500_000,
		"speech-2.8-turbo":      2_000_000,
		"MiniMax-Voice-Cloning": 9_900_000,
	} {
		var audioPricing model.ModelPricing
		if err := db.Preload("Tiers").First(&audioPricing, "channel_id = ? AND model = ? AND capability = ?", "", modelName, "audio").Error; err != nil {
			t.Fatalf("load MiniMax audio pricing %s: %v", modelName, err)
		}
		if len(audioPricing.Tiers) != 1 || audioPricing.Tiers[0].SupplierCostMicros != expected {
			t.Fatalf("MiniMax audio pricing %s = %#v, want %d", modelName, audioPricing.Tiers, expected)
		}
		if len(audioPricing.ID) > 36 || len(audioPricing.Tiers[0].ID) > 36 {
			t.Fatalf("MiniMax audio pricing identifiers exceed PostgreSQL varchar(36): pricing=%q tier=%q", audioPricing.ID, audioPricing.Tiers[0].ID)
		}
	}

	channel := model.ModelChannel{
		ID:            "channel-image",
		Scope:         model.ChannelScopeSystem,
		Enabled:       true,
		Name:          "图片渠道",
		BaseURL:       "https://example.com/v1",
		APIKey:        "secret",
		APIFormat:     "openai",
		InterfaceType: model.ChannelInterfaceAPIMartImage,
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatalf("create channel: %v", err)
	}
	for _, voice := range []model.ChannelVoice{
		{ID: "voice-a", ChannelID: channel.ID, VoiceKey: "voice-a", DisplayName: "Voice A", Kind: "system", AccessPolicy: model.ModelAccessAuthenticated, CompatibleModelsJSON: "[]", ProviderStatus: "active", Enabled: true},
		{ID: "voice-b", ChannelID: channel.ID, VoiceKey: "voice-b", DisplayName: "Voice B", Kind: "system", AccessPolicy: model.ModelAccessAuthenticated, CompatibleModelsJSON: "[]", ProviderStatus: "active", Enabled: true},
	} {
		if err := db.Create(&voice).Error; err != nil {
			t.Fatalf("create channel voice with empty idempotency key: %v", err)
		}
	}
	legacyModel := model.ChannelModel{
		ID:                    "model-image",
		ChannelID:             channel.ID,
		ModelKey:              "image-model",
		DisplayName:           "Image Model",
		Capability:            "image",
		BillingMode:           "fixed_request",
		UnitPriceMicrocredits: 100_000,
		PriceConfigured:       true,
		Enabled:               true,
		PriceVersion:          1,
	}
	if err := db.Create(&legacyModel).Error; err != nil {
		t.Fatalf("create legacy model: %v", err)
	}
	if err := db.Exec("UPDATE channel_models SET price_strategy = NULL WHERE id = ?", legacyModel.ID).Error; err != nil {
		t.Fatalf("set legacy NULL strategy: %v", err)
	}

	if err := MigrateSchema(db); err != nil {
		t.Fatalf("backfill schema migration: %v", err)
	}

	var migrated model.ChannelModel
	if err := db.First(&migrated, "id = ?", legacyModel.ID).Error; err != nil {
		t.Fatalf("load migrated model: %v", err)
	}
	if migrated.PriceStrategy != "flat" {
		t.Fatalf("price strategy = %q, want flat", migrated.PriceStrategy)
	}
}

func TestChannelModelVariantMigrationBackfillsLegacyTierAndHardCutsUniqueKey(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE channel_model_price_tiers (
		id text PRIMARY KEY,
		channel_model_id text NOT NULL,
		resolution text NOT NULL,
		unit_price_microcredits integer NOT NULL,
		price_version integer NOT NULL,
		created_at datetime,
		updated_at datetime
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX idx_channel_model_resolution ON channel_model_price_tiers(channel_model_id, resolution)`).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.Exec(`INSERT INTO channel_model_price_tiers (id, channel_model_id, resolution, unit_price_microcredits, price_version, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "legacy", "model", "1080p", 10, 1, now, now).Error; err != nil {
		t.Fatal(err)
	}

	if err := MigrateSchema(db); err != nil {
		t.Fatalf("migrate legacy channel model variant: %v", err)
	}
	var legacy model.ChannelModelPriceTier
	if err := db.First(&legacy, "id = ?", "legacy").Error; err != nil {
		t.Fatal(err)
	}
	if legacy.InputVariant != "standard" || legacy.SupplierReferenceCurrency != "" || legacy.SupplierReferenceCostMinMicros != 0 || legacy.SupplierReferenceCostMaxMicros != 0 {
		t.Fatalf("legacy channel model tier = %#v", legacy)
	}
	for index, variant := range []string{"first_frame", "last_frame", "reference"} {
		tier := model.ChannelModelPriceTier{ID: "variant-" + variant, ChannelModelID: "model", Resolution: "1080p", InputVariant: variant, UnitPriceMicrocredits: int64(index + 20), PriceVersion: 2, CreatedAt: now, UpdatedAt: now}
		if err := db.Create(&tier).Error; err != nil {
			t.Fatalf("create variant %s: %v", variant, err)
		}
	}
	duplicate := model.ChannelModelPriceTier{ID: "duplicate-standard", ChannelModelID: "model", Resolution: "1080p", InputVariant: "standard", UnitPriceMicrocredits: 99, PriceVersion: 2, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate channel model variant was accepted")
	}
}

func TestLegacyChannelModelResolutionIndexAcceptsSQLiteQuotedEquivalent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE channel_model_price_tiers (
		id text PRIMARY KEY,
		channel_model_id text NOT NULL,
		resolution text NOT NULL
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE UNIQUE INDEX `idx_channel_model_resolution` ON `channel_model_price_tiers`(`channel_model_id`,`resolution`)").Error; err != nil {
		t.Fatal(err)
	}
	exists, exact, definition, err := legacyChannelModelResolutionIndex(db, "idx_channel_model_resolution")
	if err != nil {
		t.Fatal(err)
	}
	if !exists || !exact {
		t.Fatalf("quoted equivalent index rejected: exists=%t exact=%t definition=%s", exists, exact, definition)
	}
}

func TestMigrateChannelModelBrandsRemovesLegacyIconColumns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE channel_models (
		id text PRIMARY KEY,
		model_key text NOT NULL,
		brand_key text NOT NULL DEFAULT 'generic',
		icon_file text,
		icon_mime_type text
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO channel_models (id, model_key, icon_file, icon_mime_type) VALUES (?, ?, ?, ?)", "model-1", "MiniMax-Hailuo-2.3", "legacy.png", "image/png").Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateChannelModelBrands(db, true); err != nil {
		t.Fatal(err)
	}
	var brandKey string
	if err := db.Raw("SELECT brand_key FROM channel_models WHERE id = ?", "model-1").Scan(&brandKey).Error; err != nil {
		t.Fatal(err)
	}
	if brandKey != "minimax" {
		t.Fatalf("brand_key = %q, want minimax", brandKey)
	}
	if db.Migrator().HasColumn("channel_models", "icon_file") || db.Migrator().HasColumn("channel_models", "icon_mime_type") {
		t.Fatal("legacy model icon columns were not removed")
	}
}

func TestMigrateSchemaBackfillsLegacyTeamCommercialSnapshots(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatalf("initial schema migration: %v", err)
	}
	plan := model.MembershipPlan{
		ID: "legacy-team-plan", Code: "legacy-team", Name: "历史团队版", Tier: "pro",
		Audience: model.MembershipAudienceTeam, BillingCycle: model.MembershipBillingCycleYear,
		PriceCents: 100_000, Currency: "CNY", ImageConcurrency: 6, VideoConcurrency: 4,
		MinSeats: 2, MaxSeats: 20, Enabled: true,
	}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	snapshot, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	subscription := model.MembershipSubscription{
		ID: "legacy-subscription", UserID: "legacy-owner", TeamID: "legacy-team-id",
		PlanID: plan.ID, Status: model.MembershipSubscriptionActive, Seats: 5,
		PlanSnapshotJSON: string(snapshot), StartsAt: now, CreatedAt: now, UpdatedAt: now,
	}
	order := model.MembershipOrder{
		ID: "legacy-order", OrderNumber: "M-LEGACY", UserID: "legacy-owner", TeamID: "legacy-team-id",
		PlanID: plan.ID, Seats: 5, UnitPriceCents: plan.PriceCents, TotalPriceCents: plan.PriceCents * 5,
		Currency: plan.Currency, Status: model.MembershipOrderPaid, PlanSnapshotJSON: string(snapshot),
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	// SQLite legacy columns added without a default can remain NULL even though
	// scanning into an int64 presents them as zero to application code.
	if err := db.Exec("UPDATE membership_plans SET team_storage_bytes = NULL WHERE id = ?", plan.ID).Error; err != nil {
		t.Fatalf("set legacy NULL team storage: %v", err)
	}

	if err := MigrateSchema(db); err != nil {
		t.Fatalf("commercial entitlement migration: %v", err)
	}
	var migratedPlan model.MembershipPlan
	if err := db.First(&migratedPlan, "id = ?", plan.ID).Error; err != nil {
		t.Fatal(err)
	}
	if migratedPlan.TeamStorageBytes != 130*(1<<40) || !migratedPlan.SharedAssetsEnabled || !migratedPlan.UnlimitedTaskQueue {
		t.Fatalf("legacy NULL team plan entitlements not backfilled: %#v", migratedPlan)
	}
	var migratedSubscription model.MembershipSubscription
	if err := db.First(&migratedSubscription, "id = ?", subscription.ID).Error; err != nil {
		t.Fatal(err)
	}
	var migratedSnapshot model.MembershipPlan
	if err := json.Unmarshal([]byte(migratedSubscription.PlanSnapshotJSON), &migratedSnapshot); err != nil {
		t.Fatal(err)
	}
	if !migratedSnapshot.SharedAssetsEnabled || !migratedSnapshot.ProjectPermissionsEnabled ||
		!migratedSnapshot.InvoicingEnabled || !migratedSnapshot.CommercialUseEnabled ||
		!migratedSnapshot.UnlimitedTaskQueue || migratedSnapshot.TeamStorageBytes != 130*(1<<40) {
		t.Fatalf("subscription snapshot commercial entitlements not backfilled: %#v", migratedSnapshot)
	}
	var migratedOrder model.MembershipOrder
	if err := db.First(&migratedOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(migratedOrder.PlanSnapshotJSON), &migratedSnapshot); err != nil {
		t.Fatal(err)
	}
	if !migratedSnapshot.InvoicingEnabled {
		t.Fatal("historical paid team order did not receive invoicing entitlement")
	}
}
