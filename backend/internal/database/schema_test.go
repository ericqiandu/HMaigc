package database

import (
	"testing"

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
