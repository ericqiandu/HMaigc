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
