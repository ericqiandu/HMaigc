package service

import (
	"encoding/json"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/model"
)

func TestNormalizeChannelModelPriceTierCollectionsEmitsEmptyArray(t *testing.T) {
	input := []model.ChannelModel{{ID: "model-1"}}

	normalized := normalizeChannelModelPriceTierCollections(input)

	if normalized[0].PriceTiers == nil {
		t.Fatal("PriceTiers is nil, want an explicit empty collection")
	}
	if input[0].PriceTiers != nil {
		t.Fatal("normalization mutated the repository result")
	}
	encoded, err := json.Marshal(normalized[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"priceTiers":[]`) {
		t.Fatalf("JSON = %s, want priceTiers as an empty array", encoded)
	}
}

func TestBuildChannelModelPriceTiersAcceptsInputImageUsageAdjustment(t *testing.T) {
	item := &model.ChannelModel{
		ID:            "image-model",
		Capability:    "image",
		BillingMode:   "fixed_request",
		PriceStrategy: "flat",
		PriceVersion:  3,
	}

	tiers, err := buildChannelModelPriceTiers(item, []ChannelModelPriceTierRequest{{
		UsageMetric:           inputImageUsageMetric,
		IncludedQuantity:      1,
		UnitPriceMicrocredits: 4_000,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tiers) != 1 {
		t.Fatalf("len(tiers) = %d, want 1", len(tiers))
	}
	if tiers[0].UsageMetric != inputImageUsageMetric || tiers[0].IncludedQuantity != 1 || tiers[0].UnitPriceMicrocredits != 4_000 {
		t.Fatalf("tier = %#v, want an input-image adjustment with one included image", tiers[0])
	}
}

func TestBuildChannelModelPriceTiersRejectsInvalidUsageAdjustment(t *testing.T) {
	item := &model.ChannelModel{
		ID:            "image-model",
		Capability:    "image",
		BillingMode:   "fixed_request",
		PriceStrategy: "flat",
	}
	tests := []struct {
		name  string
		tiers []ChannelModelPriceTierRequest
		want  string
	}{
		{
			name: "negative included quantity",
			tiers: []ChannelModelPriceTierRequest{{
				UsageMetric: inputImageUsageMetric, IncludedQuantity: -1, UnitPriceMicrocredits: 4_000,
			}},
			want: "免费数量不能小于 0",
		},
		{
			name: "non-positive unit price",
			tiers: []ChannelModelPriceTierRequest{{
				UsageMetric: inputImageUsageMetric, IncludedQuantity: 1, UnitPriceMicrocredits: 0,
			}},
			want: "超额积分价格必须大于 0",
		},
		{
			name: "duplicate metric",
			tiers: []ChannelModelPriceTierRequest{
				{UsageMetric: inputImageUsageMetric, IncludedQuantity: 1, UnitPriceMicrocredits: 4_000},
				{UsageMetric: inputImageUsageMetric, IncludedQuantity: 2, UnitPriceMicrocredits: 5_000},
			},
			want: "用量价格不能重复",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := buildChannelModelPriceTiers(item, test.tiers)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want message containing %q", err, test.want)
			}
		})
	}
}

func TestChannelModelPricingSnapshotIncludesUsageAdjustmentFacts(t *testing.T) {
	item := &model.ChannelModel{
		BillingMode:           "fixed_request",
		PriceStrategy:         "flat",
		UnitPriceMicrocredits: 120_000,
		PriceConfigured:       true,
		PriceTiers: []model.ChannelModelPriceTier{{
			UsageMetric:           inputImageUsageMetric,
			IncludedQuantity:      1,
			UnitPriceMicrocredits: 4_000,
		}},
	}
	snapshot := snapshotChannelModelPricing(item)

	changed := []model.ChannelModelPriceTier{{
		UsageMetric:           inputImageUsageMetric,
		IncludedQuantity:      2,
		UnitPriceMicrocredits: 4_000,
	}}
	if !snapshot.differsFrom(item, changed) {
		t.Fatal("changing the included quantity must increment the price version")
	}
}

func TestBuildChannelModelPriceTiersRequiresEveryKlingExecutablePricingVariant(t *testing.T) {
	item := &model.ChannelModel{
		ID: "kling", ModelKey: kuaiziKlingModel, Capability: "video", BillingMode: "per_second",
		PriceStrategy: "video_resolution", PriceConfigured: true, PriceVersion: 2,
	}
	requests := []ChannelModelPriceTierRequest{
		{Resolution: "std", InputVariant: "standard", UnitPriceMicrocredits: 60_000_000},
		{Resolution: "std", InputVariant: "standard_audio", UnitPriceMicrocredits: 80_000_000},
		{Resolution: "std", InputVariant: "reference_video", UnitPriceMicrocredits: 90_000_000},
		{Resolution: "pro", InputVariant: "standard", UnitPriceMicrocredits: 80_000_000},
		{Resolution: "pro", InputVariant: "standard_audio", UnitPriceMicrocredits: 100_000_000},
		{Resolution: "pro", InputVariant: "reference_video", UnitPriceMicrocredits: 120_000_000},
		{Resolution: "4k", InputVariant: "standard", UnitPriceMicrocredits: 300_000_000},
	}

	tiers, err := buildChannelModelPriceTiers(item, requests)
	if err != nil {
		t.Fatal(err)
	}
	if len(tiers) != 7 {
		t.Fatalf("len(tiers) = %d, want 7 executable Kling variants", len(tiers))
	}
	if _, err := buildChannelModelPriceTiers(item, requests[:6]); err == nil || !strings.Contains(err.Error(), "4K / standard") {
		t.Fatalf("missing 4K standard error = %v", err)
	}
}
