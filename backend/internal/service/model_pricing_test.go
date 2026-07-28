package service

import (
	"testing"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSaveModelPricingIsIdempotentForScope(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, dbErr := db.DB(); dbErr == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(&model.ModelPricing{}, &model.ModelPricingTier{}); err != nil {
		t.Fatal(err)
	}

	svc := New(repository.New(db), t.TempDir())
	actor := &model.User{ID: "admin-1", Role: model.UserRoleAdmin}
	first, err := svc.SaveModelPricing(actor, "", ModelPricingRequest{
		ChannelID:      "channel-1",
		Model:          "video-model",
		Capability:     "video",
		Currency:       "CNY",
		PerMediaMicros: 1_000_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.SaveModelPricing(actor, "", ModelPricingRequest{
		ChannelID:            " channel-1 ",
		Model:                " video-model ",
		Capability:           "VIDEO",
		Currency:             "cny",
		PerMediaMicros:       2_000_000,
		PerVideoSecondMicros: 300_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("second save created pricing %q, want existing %q", second.ID, first.ID)
	}

	var count int64
	if err := db.Model(&model.ModelPricing{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("pricing count = %d, want 1", count)
	}
	if second.PerMediaMicros != 2_000_000 || second.PerVideoSecondMicros != 300_000 {
		t.Fatalf("second pricing was not updated: %#v", second)
	}
}
