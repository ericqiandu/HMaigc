package service

import "testing"

func TestNormalizeSuperResolutionPricingResolution(t *testing.T) {
	tests := map[string]string{
		"720": "720P", "1080p": "1080P", "2K": "2K", "4k": "4K", "8K": "8K", "480p": "",
	}
	for input, expected := range tests {
		if actual := normalizeSuperResolutionPricingResolution(input); actual != expected {
			t.Fatalf("normalizeSuperResolutionPricingResolution(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestValidSuperResolutionFPSBand(t *testing.T) {
	valid := [][2]int{{0, 30}, {30, 60}, {60, 120}}
	for _, band := range valid {
		if !validSuperResolutionFPSBand(band[0], band[1]) {
			t.Fatalf("expected band (%d, %d] to be valid", band[0], band[1])
		}
	}
	invalid := [][2]int{{0, 29}, {29, 60}, {60, 121}, {30, 30}}
	for _, band := range invalid {
		if validSuperResolutionFPSBand(band[0], band[1]) {
			t.Fatalf("expected band (%d, %d] to be invalid", band[0], band[1])
		}
	}
}
