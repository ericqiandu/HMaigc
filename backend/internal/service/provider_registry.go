package service

import (
	"fmt"
	"strings"
)

type ProviderAdapterDescriptor struct {
	ProviderKind string              `json:"providerKind"`
	Family       string              `json:"family"`
	Models       []ProviderModelSpec `json:"models"`
}

type ProviderModelSpec struct {
	ModelKey                string   `json:"modelKey"`
	DisplayName             string   `json:"displayName"`
	MarketingCopy           string   `json:"marketingCopy"`
	UpstreamMode            string   `json:"upstreamMode"`
	Capability              string   `json:"capability"`
	Resolutions             []string `json:"resolutions"`
	Ratios                  []string `json:"ratios"`
	DurationMin             int      `json:"durationMin"`
	DurationMax             int      `json:"durationMax"`
	SupportsSmartDuration   bool     `json:"supportsSmartDuration"`
	SupportsGeneratedAudio  bool     `json:"supportsGeneratedAudio"`
	SupportsWatermark       bool     `json:"supportsWatermark"`
	SupportsAudioOnly       bool     `json:"supportsAudioOnly"`
	RequiresAdaptiveFrames  bool     `json:"requiresAdaptiveFrames"`
	MaxImages               int      `json:"maxImages"`
	MaxVideos               int      `json:"maxVideos"`
	MaxAudios               int      `json:"maxAudios"`
	MaxVideoDurationSeconds int      `json:"maxVideoDurationSeconds"`
	MaxAudioDurationSeconds int      `json:"maxAudioDurationSeconds"`
	Tools                   []string `json:"tools"`
	Published               bool     `json:"published"`
	ChannelModelID          string   `json:"channelModelId"`
	Enabled                 bool     `json:"enabled"`
	PriceConfigured         bool     `json:"priceConfigured"`
}

type ProviderRegistry struct {
	descriptors []ProviderAdapterDescriptor
	byFamily    map[string]ProviderAdapterDescriptor
}

func NewProviderRegistry(descriptors []ProviderAdapterDescriptor) (*ProviderRegistry, error) {
	registry := &ProviderRegistry{
		descriptors: make([]ProviderAdapterDescriptor, 0, len(descriptors)),
		byFamily:    make(map[string]ProviderAdapterDescriptor, len(descriptors)),
	}
	modelKeys := make(map[string]struct{})
	for _, source := range descriptors {
		descriptor := cloneProviderAdapterDescriptor(source)
		descriptor.ProviderKind = strings.TrimSpace(descriptor.ProviderKind)
		descriptor.Family = strings.TrimSpace(descriptor.Family)
		if descriptor.ProviderKind == "" || descriptor.Family == "" {
			return nil, fmt.Errorf("provider adapter descriptor requires provider kind and family")
		}
		familyKey := providerFamilyRegistryKey(descriptor.ProviderKind, descriptor.Family)
		if _, exists := registry.byFamily[familyKey]; exists {
			return nil, fmt.Errorf("duplicate provider adapter family %s/%s", descriptor.ProviderKind, descriptor.Family)
		}
		for index := range descriptor.Models {
			model := &descriptor.Models[index]
			model.ModelKey = strings.TrimSpace(model.ModelKey)
			model.DisplayName = strings.TrimSpace(model.DisplayName)
			model.UpstreamMode = strings.TrimSpace(model.UpstreamMode)
			model.Capability = strings.TrimSpace(model.Capability)
			if model.ModelKey == "" || model.DisplayName == "" || model.UpstreamMode == "" || model.Capability == "" {
				return nil, fmt.Errorf("provider adapter %s/%s contains incomplete model identity", descriptor.ProviderKind, descriptor.Family)
			}
			if _, exists := modelKeys[model.ModelKey]; exists {
				return nil, fmt.Errorf("duplicate provider model key %s", model.ModelKey)
			}
			modelKeys[model.ModelKey] = struct{}{}
		}
		registry.descriptors = append(registry.descriptors, descriptor)
		registry.byFamily[familyKey] = descriptor
	}
	return registry, nil
}

func (r *ProviderRegistry) Descriptors() []ProviderAdapterDescriptor {
	result := make([]ProviderAdapterDescriptor, len(r.descriptors))
	for index, descriptor := range r.descriptors {
		result[index] = cloneProviderAdapterDescriptor(descriptor)
	}
	return result
}

func (r *ProviderRegistry) Descriptor(providerKind string, family string) (ProviderAdapterDescriptor, bool) {
	descriptor, ok := r.byFamily[providerFamilyRegistryKey(providerKind, family)]
	return cloneProviderAdapterDescriptor(descriptor), ok
}

func providerFamilyRegistryKey(providerKind string, family string) string {
	return strings.TrimSpace(providerKind) + "\n" + strings.TrimSpace(family)
}

func cloneProviderAdapterDescriptor(source ProviderAdapterDescriptor) ProviderAdapterDescriptor {
	result := source
	result.Models = make([]ProviderModelSpec, len(source.Models))
	for index, model := range source.Models {
		result.Models[index] = model
		result.Models[index].Resolutions = append([]string(nil), model.Resolutions...)
		result.Models[index].Ratios = append([]string(nil), model.Ratios...)
		result.Models[index].Tools = append([]string{}, model.Tools...)
	}
	return result
}

func kuaiziProviderAdapterDescriptors() []ProviderAdapterDescriptor {
	return []ProviderAdapterDescriptor{
		{
			ProviderKind: "kuaizi",
			Family:       "seedance",
			Models: []ProviderModelSpec{
				seedanceProviderModel("doubao-seedance-2-0-fast-260128", "Seedance 2.0 Fast", []string{"480p", "720p", "1080p"}, 15, 9, 3, 3, false, []string{"web_search"}),
				seedanceProviderModel("doubao-seedance-2-0-260128", "Seedance 2.0 Pro", []string{"480p", "720p", "1080p", "4k"}, 15, 9, 3, 3, false, []string{"web_search"}),
				seedanceProviderModel("doubao-seedance-2-0-mini-260615", "Seedance 2.0 Mini", []string{"480p", "720p", "1080p"}, 15, 9, 3, 3, false, []string{"web_search"}),
				seedanceProviderModel("doubao-seedance-2-5-260628", "Seedance 2.5", []string{"480p", "720p"}, 30, 30, 10, 10, true, nil),
			},
		},
		{
			ProviderKind: "kuaizi",
			Family:       "gpt-image2",
			Models: []ProviderModelSpec{{
				ModelKey: "kz_gpt_image2", DisplayName: "GPT Image 2", UpstreamMode: "kz_gpt_image2", Capability: "image",
				Resolutions: []string{"1K", "2K", "4K"},
				Ratios:      []string{"1:1", "1:2", "2:1", "9:16", "16:9", "3:4", "4:3", "3:2", "2:3", "5:4", "4:5", "21:9", "9:21"},
			}},
		},
		{
			ProviderKind: "kuaizi",
			Family:       "gpt",
			Models: []ProviderModelSpec{{
				ModelKey: "gpt-5.5", DisplayName: "GPT 5.5", MarketingCopy: "支持图片理解与 Agent 工具调用",
				UpstreamMode: "gpt-5.5", Capability: "text",
			}},
		},
		{
			ProviderKind: "kuaizi",
			Family:       "deepseek",
			Models: []ProviderModelSpec{{
				ModelKey: "deepseek-v4-pro", DisplayName: "DeepSeek V4 Pro", MarketingCopy: "纯文本 Agent 模型，不支持图片输入",
				UpstreamMode: "deepseek-v4-pro", Capability: "text",
			}},
		},
	}
}

func seedanceProviderModel(modelKey string, displayName string, resolutions []string, durationMax int, maxImages int, maxVideos int, maxAudios int, supportsAudioOnly bool, tools []string) ProviderModelSpec {
	return ProviderModelSpec{
		ModelKey: modelKey, DisplayName: displayName, UpstreamMode: modelKey, Capability: "video",
		Resolutions: resolutions, Ratios: []string{"adaptive", "16:9", "4:3", "1:1", "3:4", "9:16", "21:9"},
		DurationMin: 4, DurationMax: durationMax, SupportsSmartDuration: true,
		SupportsGeneratedAudio: true, SupportsWatermark: true,
		SupportsAudioOnly: supportsAudioOnly, RequiresAdaptiveFrames: supportsAudioOnly,
		MaxImages: maxImages, MaxVideos: maxVideos, MaxAudios: maxAudios,
		MaxVideoDurationSeconds: durationMax, MaxAudioDurationSeconds: durationMax,
		Tools: append([]string{}, tools...),
	}
}

func kuaiziSeedanceModelSpec(modelKey string) (ProviderModelSpec, bool) {
	candidate, ok := kuaiziProviderModelSpec(modelKey)
	return candidate, ok && candidate.Capability == "video"
}

func kuaiziProviderModelSpec(modelKey string) (ProviderModelSpec, bool) {
	for _, descriptor := range kuaiziProviderAdapterDescriptors() {
		for _, candidate := range descriptor.Models {
			if candidate.ModelKey == strings.TrimSpace(modelKey) {
				return candidate, true
			}
		}
	}
	return ProviderModelSpec{}, false
}

func kuaiziProviderFamilyForModel(modelKey string) (string, ProviderModelSpec, bool) {
	for _, descriptor := range kuaiziProviderAdapterDescriptors() {
		for _, candidate := range descriptor.Models {
			if candidate.ModelKey == strings.TrimSpace(modelKey) {
				return descriptor.Family, candidate, true
			}
		}
	}
	return "", ProviderModelSpec{}, false
}

func validateProviderRegistryRuntime(descriptors []ProviderAdapterDescriptor) error {
	if _, err := NewProviderRegistry(descriptors); err != nil {
		return fmt.Errorf("validate provider adapter registry: %w", err)
	}
	return nil
}
