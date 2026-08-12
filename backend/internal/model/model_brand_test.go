package model

import "testing"

func TestInferModelBrandKey(t *testing.T) {
	tests := map[string]string{
		"gpt-image-2": "openai", "kz_gpt_image2": "openai", "gemini-2.5-flash": "google", "MiniMax-Hailuo-02": "minimax",
		"seedance-2.0-fast": "seedance", "kling-v3": "kling", "unknown-provider-model": "generic",
	}
	for modelKey, expected := range tests {
		if actual := InferModelBrandKey(modelKey); actual != expected {
			t.Fatalf("InferModelBrandKey(%q) = %q, want %q", modelKey, actual, expected)
		}
	}
}

func TestModelBrandKeyValidation(t *testing.T) {
	if !IsModelBrandKey("minimax") {
		t.Fatal("minimax should be a valid model brand")
	}
	if IsModelBrandKey("uploaded-logo") {
		t.Fatal("unknown model brand should be rejected")
	}
}
