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

func TestProviderRegistryPublishesSeedance20And25CompatibleCapabilities(t *testing.T) {
	registry, err := NewProviderRegistry(kuaiziProviderAdapterDescriptors())
	if err != nil {
		t.Fatal(err)
	}
	descriptor, _ := registry.Descriptor("kuaizi", "seedance")
	if len(descriptor.Models) != 4 {
		t.Fatalf("seedance models = %#v", descriptor.Models)
	}
	model := descriptor.Models[3]
	if model.ModelKey != "doubao-seedance-2-5-260628" || model.DisplayName != "Seedance 2.5" || model.UpstreamMode != model.ModelKey || model.Capability != "video" {
		t.Fatalf("seedance 2.5 identity = %#v", model)
	}
	if model.DurationMin != 4 || model.DurationMax != 30 || !model.SupportsSmartDuration || !model.SupportsGeneratedAudio || !model.SupportsWatermark {
		t.Fatalf("seedance 2.5 duration/features = %#v", model)
	}
	if strings.Join(model.Resolutions, ",") != "480p,720p" || len(model.Ratios) != 7 || model.MaxImages != 30 || model.MaxVideos != 10 || model.MaxAudios != 10 {
		t.Fatalf("seedance 2.5 media constraints = %#v", model)
	}
	for _, index := range []int{0, 1, 2} {
		model := descriptor.Models[index]
		if model.DurationMax != 15 || model.MaxImages != 9 || model.MaxVideos != 3 || model.MaxAudios != 3 {
			t.Fatalf("seedance 2.0 constraints = %#v", model)
		}
	}
}

func TestKuaiziCompatibleInputEnforcesPerModelCapabilities(t *testing.T) {
	tests := []struct {
		name  string
		model string
		input canvasGenerationInput
		want  string
	}{
		{name: "2.0 rejects 30 seconds", model: "doubao-seedance-2-0-fast-260128", input: canvasGenerationInput{Config: providerConfig{Size: "16:9", VQuality: "720p", VideoSeconds: "30"}}, want: "4–15"},
		{name: "2.0 audio needs visual media", model: "doubao-seedance-2-0-260128", input: canvasGenerationInput{Config: providerConfig{Size: "16:9", VQuality: "720p", VideoSeconds: "5"}, ReferenceAudios: []providerMedia{{URL: "https://cdn.example.com/a.mp3"}}}, want: "必须同时连接"},
		{name: "2.5 audio needs text", model: "doubao-seedance-2-5-260628", input: canvasGenerationInput{Config: providerConfig{Size: "adaptive", VQuality: "720p", VideoSeconds: "5"}, ReferenceAudios: []providerMedia{{URL: "https://cdn.example.com/a.mp3"}}}, want: "必须同时提供提示词"},
		{name: "2.5 rejects 1080p", model: "doubao-seedance-2-5-260628", input: canvasGenerationInput{Config: providerConfig{Size: "16:9", VQuality: "1080p", VideoSeconds: "5"}}, want: "不支持分辨率"},
		{name: "2.5 frame requires adaptive", model: "doubao-seedance-2-5-260628", input: canvasGenerationInput{Config: providerConfig{Size: "16:9", VQuality: "720p", VideoSeconds: "5"}, ReferenceImages: []providerMedia{{ID: "first", URL: "https://cdn.example.com/a.png"}}, Metadata: map[string]interface{}{"videoStartFrameNodeId": "first"}}, want: "只支持自适应"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.input.Config.Model = test.model
			spec, _ := kuaiziSeedanceModelSpec(test.model)
			_, _, _, err := validateKuaiziCompatibleVideoInput(test.input, spec)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}

	input := canvasGenerationInput{Prompt: "让参考音频驱动画面", Config: providerConfig{Model: "doubao-seedance-2-5-260628", Size: "adaptive", VQuality: "720p", VideoSeconds: "30"}, ReferenceAudios: []providerMedia{{URL: "https://cdn.example.com/a.mp3"}}}
	spec, _ := kuaiziSeedanceModelSpec(input.Config.Model)
	if _, _, duration, err := validateKuaiziCompatibleVideoInput(input, spec); err != nil || duration != 30 {
		t.Fatalf("2.5 compatible input = duration %d, error %v", duration, err)
	}
}

func TestProviderRegistryRejectsDuplicateProviderFamilyAndModelKeys(t *testing.T) {
	base := ProviderAdapterDescriptor{
		ProviderKind: "kuaizi",
		Family:       "seedance",
		Models:       []ProviderModelSpec{{ModelKey: "doubao-seedance-2-5-260628", DisplayName: "Seedance 2.5", UpstreamMode: "doubao-seedance-2-5-260628", Capability: "video"}},
	}
	tests := []struct {
		name        string
		descriptors []ProviderAdapterDescriptor
		want        string
	}{
		{name: "provider family", descriptors: []ProviderAdapterDescriptor{base, base}, want: "kuaizi/seedance"},
		{name: "model key in another family", descriptors: []ProviderAdapterDescriptor{base, {
			ProviderKind: "kuaizi", Family: "other", Models: []ProviderModelSpec{{ModelKey: "doubao-seedance-2-5-260628", DisplayName: "Duplicate", UpstreamMode: "other", Capability: "video"}},
		}}, want: "doubao-seedance-2-5-260628"},
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
