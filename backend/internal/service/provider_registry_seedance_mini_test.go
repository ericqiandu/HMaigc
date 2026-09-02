package service

import (
	"strings"
	"testing"
)

func TestSeedanceMiniPublishesOnlySupplierSupportedResolutions(t *testing.T) {
	spec, ok := kuaiziProviderModelSpec("doubao-seedance-2-0-mini-260615")
	if !ok {
		t.Fatal("Seedance 2.0 Mini provider specification is missing")
	}
	if got := strings.Join(spec.Resolutions, ","); got != "480p,720p" {
		t.Fatalf("Mini resolutions = %q, want 480p,720p", got)
	}
	if got := strings.Join(spec.ReferenceVideoResolutions, ","); got != "480p,720p" {
		t.Fatalf("Mini reference-video resolutions = %q, want 480p,720p", got)
	}
	if got := strings.Join(providerPricingInputVariants(spec), ","); got != "standard,standard_audio,reference_video" {
		t.Fatalf("Mini pricing input variants = %q, want standard,standard_audio,reference_video", got)
	}
}
