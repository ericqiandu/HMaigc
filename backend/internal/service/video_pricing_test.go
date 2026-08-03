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
		{
			name: "super resolution 720p",
			usage: BillingUsage{
				Resolution:                "480",
				SuperResolutionEnabled:    true,
				SuperResolutionResolution: "720p",
			},
			want: "SR_720P",
		},
		{
			name: "super resolution 1080p numeric",
			usage: BillingUsage{
				Resolution:                "720",
				SuperResolutionEnabled:    true,
				SuperResolutionResolution: "1080",
			},
			want: "SR_1080P",
		},
		{
			name: "super resolution 2k",
			usage: BillingUsage{
				Resolution:                "1080",
				SuperResolutionEnabled:    true,
				SuperResolutionResolution: "2k",
			},
			want: "SR_2K",
		},
		{
			name: "super resolution 4k",
			usage: BillingUsage{
				Resolution:                "2k",
				SuperResolutionEnabled:    true,
				SuperResolutionResolution: "4K",
			},
			want: "SR_4K",
		},
		{
			name: "invalid super resolution 480p",
			usage: BillingUsage{
				Resolution:                "480",
				SuperResolutionEnabled:    true,
				SuperResolutionResolution: "480p",
			},
			want: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeVideoPricingResolution(test.usage); got != test.want {
				t.Fatalf("normalizeVideoPricingResolution() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestBuildChannelModelPriceTiersAcceptsVideoBaseAndSuperResolution(t *testing.T) {
	item := &model.ChannelModel{
		ID:              "model-1",
		PriceStrategy:   "video_resolution",
		PriceConfigured: true,
		PriceVersion:    3,
	}
	requests := []ChannelModelPriceTierRequest{
		{Resolution: "480P", UnitPriceMicrocredits: 1_000_000},
		{Resolution: "768P", UnitPriceMicrocredits: 1_500_000},
		{Resolution: "1080P", UnitPriceMicrocredits: 2_000_000},
		{Resolution: "SR_2K", UnitPriceMicrocredits: 4_000_000},
		{Resolution: "SR_4K", UnitPriceMicrocredits: 8_000_000},
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
