package service

import (
	"strings"
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

func TestSaveModelPricingPersistsInputImageUsageCost(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ModelPricing{}, &model.ModelPricingTier{}); err != nil {
		t.Fatal(err)
	}
	svc := New(repository.New(db), t.TempDir())
	actor := &model.User{ID: "admin", Role: model.UserRoleAdmin}

	pricing, err := svc.SaveModelPricing(actor, "", ModelPricingRequest{
		ChannelID: "channel", Model: "image", Capability: "image", Currency: "CNY", PerRequestMicros: 600_000,
		Tiers: []ModelPricingTierRequest{{
			Specification: "INPUT_IMAGE", UsageMetric: inputImageUsageMetric,
			IncludedQuantity: 1, SupplierCostMicros: 20_000,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pricing.Tiers) != 1 || pricing.Tiers[0].UsageMetric != inputImageUsageMetric ||
		pricing.Tiers[0].IncludedQuantity != 1 || pricing.Tiers[0].SupplierCostMicros != 20_000 {
		t.Fatalf("pricing tiers = %#v", pricing.Tiers)
	}
}

func TestSaveModelPricingRejectsInvalidInputImageUsageCost(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ModelPricing{}, &model.ModelPricingTier{}); err != nil {
		t.Fatal(err)
	}
	svc := New(repository.New(db), t.TempDir())
	actor := &model.User{ID: "admin", Role: model.UserRoleAdmin}
	tests := []struct {
		name  string
		tiers []ModelPricingTierRequest
		want  string
	}{
		{
			name:  "negative included quantity",
			tiers: []ModelPricingTierRequest{{Specification: "INPUT_IMAGE", UsageMetric: inputImageUsageMetric, IncludedQuantity: -1, SupplierCostMicros: 20_000}},
			want:  "免费数量",
		},
		{
			name:  "zero unit cost",
			tiers: []ModelPricingTierRequest{{Specification: "INPUT_IMAGE", UsageMetric: inputImageUsageMetric, IncludedQuantity: 1, SupplierCostMicros: 0}},
			want:  "超额成本",
		},
		{
			name: "duplicate metric",
			tiers: []ModelPricingTierRequest{
				{Specification: "INPUT_IMAGE_A", UsageMetric: inputImageUsageMetric, IncludedQuantity: 1, SupplierCostMicros: 20_000},
				{Specification: "INPUT_IMAGE_B", UsageMetric: inputImageUsageMetric, IncludedQuantity: 1, SupplierCostMicros: 30_000},
			},
			want: "用量成本不能重复",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := svc.SaveModelPricing(actor, "", ModelPricingRequest{
				ChannelID: "channel", Model: "image", Capability: "image", Currency: "CNY", Tiers: test.tiers,
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want message containing %q", err, test.want)
			}
		})
	}
}

func TestEstimateCallCostAddsInputImageUsageCost(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ModelPricing{}, &model.ModelPricingTier{}); err != nil {
		t.Fatal(err)
	}
	pricing := model.ModelPricing{
		ID: "pricing", ChannelID: "channel", Model: "image", Capability: "image", Currency: "CNY", PerRequestMicros: 600_000,
		Tiers: []model.ModelPricingTier{{
			ID: "input-overage", Specification: "INPUT_IMAGE", UsageMetric: inputImageUsageMetric,
			IncludedQuantity: 1, SupplierCostMicros: 20_000,
		}},
	}
	if err := db.Create(&pricing).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db)}
	log := model.ApiCallLog{
		ChannelID: "channel", Model: "image", Capability: "image", Status: model.ApiCallStatusSucceeded,
		Billable: true, InputImageCount: 3,
	}
	svc.estimateCallCost(&log)
	if !log.CostAvailable || log.EstimatedCostMicros != 640_000 || log.CostCalculationError != "" {
		t.Fatalf("log cost = %#v", log)
	}
}
