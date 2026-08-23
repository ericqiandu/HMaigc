package service

import (
	"testing"

	"infinite-canvas/backend/internal/model"
)

func TestNormalizeVideoPricingResolution(t *testing.T) {
	tests := []struct {
		name  string
		usage BillingUsage
		want  string
	}{
		{name: "numeric 480p", usage: BillingUsage{Resolution: "480"}, want: "480P"},
		{name: "numeric 720p", usage: BillingUsage{Resolution: "720"}, want: "720P"},
		{name: "MiniMax H3 768p", usage: BillingUsage{Resolution: "768p"}, want: "768P"},
		{name: "1080p suffix", usage: BillingUsage{Resolution: "1080p"}, want: "1080P"},
		{name: "base 2k", usage: BillingUsage{Resolution: "2K"}, want: "2K"},
		{name: "base 4k", usage: BillingUsage{Resolution: "4k"}, want: "4K"},
		{name: "Kling standard mode", usage: BillingUsage{Resolution: "std"}, want: "STD"},
		{name: "Kling professional mode", usage: BillingUsage{Resolution: "pro"}, want: "PRO"},
		{name: "super resolution does not replace base tier", usage: BillingUsage{Resolution: "720", SuperResolutionEnabled: true, SuperResolutionResolution: "4K"}, want: "720P"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeVideoPricingResolution(test.usage); got != test.want {
				t.Fatalf("normalizeVideoPricingResolution() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestBuildChannelModelPriceTiersAcceptsOnlyVideoGenerationResolutions(t *testing.T) {
	item := &model.ChannelModel{
		ID:              "model-1",
		PriceStrategy:   "video_resolution",
		PriceConfigured: true,
		PriceVersion:    3,
	}
	requests := []ChannelModelPriceTierRequest{
		{Resolution: "480P", InputVariant: "standard", UnitPriceMicrocredits: 1_000_000},
		{Resolution: "768P", InputVariant: "standard", UnitPriceMicrocredits: 1_500_000},
		{Resolution: "1080P", InputVariant: "standard", UnitPriceMicrocredits: 2_000_000},
	}

	tiers, err := buildChannelModelPriceTiers(item, requests)
	if err != nil {
		t.Fatal(err)
	}
	if len(tiers) != len(requests) {
		t.Fatalf("tier count = %d, want %d", len(tiers), len(requests))
	}
	for index, tier := range tiers {
		if tier.Resolution != requests[index].Resolution {
			t.Fatalf("tier %d resolution = %q, want %q", index, tier.Resolution, requests[index].Resolution)
		}
		if tier.UnitPriceMicrocredits != requests[index].UnitPriceMicrocredits {
			t.Fatalf("tier %d price = %d, want %d", index, tier.UnitPriceMicrocredits, requests[index].UnitPriceMicrocredits)
		}
		if tier.InputVariant != requests[index].InputVariant {
			t.Fatalf("tier %d input variant = %q, want %q", index, tier.InputVariant, requests[index].InputVariant)
		}
	}
}

func TestBuildSeedancePriceTiersRequiresResolutionAndReferenceVariant(t *testing.T) {
	item := &model.ChannelModel{
		ID: "seedance-25", ModelKey: "doubao-seedance-2-5-260628",
		PriceStrategy: "video_resolution", PriceConfigured: true, PriceVersion: 2,
	}
	requests := []ChannelModelPriceTierRequest{
		{Resolution: "480P", InputVariant: "standard", UnitPriceMicrocredits: 670_000},
		{Resolution: "480P", InputVariant: "reference_video", UnitPriceMicrocredits: 720_000},
		{Resolution: "720P", InputVariant: "standard", UnitPriceMicrocredits: 1_510_000},
		{Resolution: "720P", InputVariant: "reference_video", UnitPriceMicrocredits: 1_630_000},
	}
	if _, err := buildChannelModelPriceTiers(item, requests); err != nil {
		t.Fatal(err)
	}
	if _, err := buildChannelModelPriceTiers(item, requests[:3]); err == nil {
		t.Fatal("incomplete Seedance four-tier price was accepted")
	}
}

func TestBuildChannelModelPriceTiersRejectsLegacySuperResolutionTier(t *testing.T) {
	item := &model.ChannelModel{ID: "model-1", PriceStrategy: "video_resolution", PriceConfigured: true}
	_, err := buildChannelModelPriceTiers(item, []ChannelModelPriceTierRequest{{Resolution: "SR_2K", UnitPriceMicrocredits: 4_000_000}})
	if err == nil {
		t.Fatal("expected legacy super-resolution model tier to fail")
	}
}

func TestBuildChannelModelPriceTiersRejectsUnsupportedVideoResolution(t *testing.T) {
	item := &model.ChannelModel{
		ID:              "model-1",
		PriceStrategy:   "video_resolution",
		PriceConfigured: true,
	}
	_, err := buildChannelModelPriceTiers(item, []ChannelModelPriceTierRequest{
		{Resolution: "8K", UnitPriceMicrocredits: 1_000_000},
	})
	if err == nil {
		t.Fatal("expected unsupported video resolution to fail")
	}
}
