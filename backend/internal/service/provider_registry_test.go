package service

import (
	"strings"
	"testing"
)

func TestProviderRegistryContainsOnlyImplementedFamilies(t *testing.T) {
	registry, err := NewProviderRegistry(kuaiziProviderAdapterDescriptors())
	if err != nil {
		t.Fatal(err)
	}
	descriptors := registry.Descriptors()
	if len(descriptors) != 1 {
		t.Fatalf("descriptor count = %d, want 1", len(descriptors))
	}
	seedance, ok := registry.Descriptor("kuaizi", "seedance")
	if !ok {
		t.Fatal("kuaizi/seedance descriptor is missing")
	}
	if seedance.ProviderKind != "kuaizi" || seedance.Family != "seedance" {
		t.Fatalf("seedance descriptor = %#v", seedance)
	}
}

func TestProviderRegistryPublishesSeedance25StructuralCapabilities(t *testing.T) {
	registry, err := NewProviderRegistry(kuaiziProviderAdapterDescriptors())
	if err != nil {
		t.Fatal(err)
	}
	descriptor, _ := registry.Descriptor("kuaizi", "seedance")
	if len(descriptor.Models) != 1 {
		t.Fatalf("seedance models = %#v", descriptor.Models)
	}
	model := descriptor.Models[0]
	if model.ModelKey != "kuaizi-seedance-2.5" || model.DisplayName != "Seedance 2.5" || model.UpstreamMode != "seedance2.5" || model.Capability != "video" {
		t.Fatalf("seedance 2.5 identity = %#v", model)
	}
	if model.DurationMin != 4 || model.DurationMax != 30 || !model.SupportsSmartDuration || !model.SupportsGeneratedAudio || !model.SupportsWatermark {
		t.Fatalf("seedance 2.5 duration/features = %#v", model)
	}
	if strings.Join(model.Resolutions, ",") != "480p,720p" || len(model.Ratios) != 7 || model.MaxImages != 30 || model.MaxVideos != 10 || model.MaxAudios != 10 {
		t.Fatalf("seedance 2.5 media constraints = %#v", model)
	}
}

func TestProviderRegistryRejectsDuplicateProviderFamilyAndModelKeys(t *testing.T) {
	base := ProviderAdapterDescriptor{
		ProviderKind: "kuaizi",
		Family:       "seedance",
		Models:       []ProviderModelSpec{{ModelKey: "kuaizi-seedance-2.5", DisplayName: "Seedance 2.5", UpstreamMode: "seedance-2.5", Capability: "video"}},
	}
	tests := []struct {
		name        string
		descriptors []ProviderAdapterDescriptor
		want        string
	}{
		{name: "provider family", descriptors: []ProviderAdapterDescriptor{base, base}, want: "kuaizi/seedance"},
		{name: "model key in another family", descriptors: []ProviderAdapterDescriptor{base, {
			ProviderKind: "kuaizi", Family: "other", Models: []ProviderModelSpec{{ModelKey: "kuaizi-seedance-2.5", DisplayName: "Duplicate", UpstreamMode: "other", Capability: "video"}},
		}}, want: "kuaizi-seedance-2.5"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewProviderRegistry(test.descriptors); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("duplicate registry error = %v, want mention of %q", err, test.want)
			}
		})
	}
}

func TestProviderRegistryRuntimeGateRejectsDuplicateRegistration(t *testing.T) {
	descriptor := kuaiziProviderAdapterDescriptors()[0]
	if err := validateProviderRegistryRuntime([]ProviderAdapterDescriptor{descriptor, descriptor}); err == nil || !strings.Contains(err.Error(), "kuaizi/seedance") {
		t.Fatalf("runtime registry duplicate error = %v", err)
	}
}
