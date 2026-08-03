package service

import (
	"testing"

	"infinite-canvas/backend/internal/model"
)

func TestMiniMaxH3EstimatedCostIncludesGeneratedAndInputMaterials(t *testing.T) {
	pricing := &model.ModelPricing{Tiers: []model.ModelPricingTier{
		{Specification: "768P", SupplierCostMicros: 500_000},
		{Specification: miniMaxH3InputImageOverageSpecification, SupplierCostMicros: 200_000},
		{Specification: miniMaxH3InputVideo768PSpecification, SupplierCostMicros: 500_000},
	}}
	log := &model.ApiCallLog{
		PricingSpecification: "768p", VideoSeconds: 6, InputImageCount: 7,
		InputVideoCount: 1, InputVideoDurationMs: 2_500, InputVideoDurationComplete: true,
	}

	cost, err := miniMaxH3EstimatedCost(log, pricing)
	if err != nil {
		t.Fatal(err)
	}
	// 6s generated + 2 images above the free quota + 2.5s input video.
	if cost != 4_650_000 {
		t.Fatalf("cost = %d, want 4650000", cost)
	}
}

func TestMiniMaxH3EstimatedCostRejectsIncompleteInputVideoDuration(t *testing.T) {
	pricing := &model.ModelPricing{Tiers: []model.ModelPricingTier{{Specification: "2K", SupplierCostMicros: 800_000}}}
	log := &model.ApiCallLog{PricingSpecification: "2K", VideoSeconds: 8, InputVideoCount: 1, InputVideoDurationComplete: false}
	if _, err := miniMaxH3EstimatedCost(log, pricing); err == nil {
		t.Fatal("expected incomplete input video duration to fail cost calculation")
	}
}
