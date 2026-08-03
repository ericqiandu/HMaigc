package service

import (
	"testing"

	"infinite-canvas/backend/internal/model"
)

func TestMiniMaxSpeechEstimatedCostUsesExactCharacterCount(t *testing.T) {
	pricing := &model.ModelPricing{Tiers: []model.ModelPricingTier{{Specification: miniMaxSpeechCharacterSpecification, SupplierCostMicros: 3_500_000}}}
	cost, err := miniMaxAudioEstimatedCost(&model.ApiCallLog{Model: "speech-2.8-hd", InputCharacterCount: 2_500}, pricing)
	if err != nil {
		t.Fatal(err)
	}
	if cost != 875_000 {
		t.Fatalf("cost = %d, want 875000", cost)
	}
}

func TestMiniMaxVoiceCloneEstimatedCostIsPerVoice(t *testing.T) {
	pricing := &model.ModelPricing{Tiers: []model.ModelPricingTier{{Specification: miniMaxVoiceCloneSpecification, SupplierCostMicros: 9_900_000}}}
	cost, err := miniMaxAudioEstimatedCost(&model.ApiCallLog{Model: miniMaxVoiceCloningPricingModel}, pricing)
	if err != nil {
		t.Fatal(err)
	}
	if cost != 9_900_000 {
		t.Fatalf("cost = %d, want 9900000", cost)
	}
}
