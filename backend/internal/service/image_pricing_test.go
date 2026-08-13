package service

import "testing"

func TestBillingUsageUsesImageDimensionsInsteadOfQualityForResolutionTier(t *testing.T) {
	tests := []struct {
		size string
		want string
	}{
		{size: "1024x1024", want: "1K"},
		{size: "2048x1152", want: "2K"},
		{size: "2864x2864", want: "4K"},
	}
	for _, test := range tests {
		usage := billingUsage("image", "image-model", map[string]any{"count": 1, "size": test.size, "quality": "low"})
		if usage.Resolution != test.want {
			t.Fatalf("size %s resolution = %q, want %q", test.size, usage.Resolution, test.want)
		}
	}
}
