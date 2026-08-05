package database

import (
	"encoding/json"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

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
