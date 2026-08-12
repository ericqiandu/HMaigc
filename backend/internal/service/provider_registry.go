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
	ModelKey               string   `json:"modelKey"`
	DisplayName            string   `json:"displayName"`
	UpstreamMode           string   `json:"upstreamMode"`
	Capability             string   `json:"capability"`
	Resolutions            []string `json:"resolutions"`
	Ratios                 []string `json:"ratios"`
	DurationMin            int      `json:"durationMin"`
	DurationMax            int      `json:"durationMax"`
	SupportsSmartDuration  bool     `json:"supportsSmartDuration"`
	SupportsGeneratedAudio bool     `json:"supportsGeneratedAudio"`
	SupportsWatermark      bool     `json:"supportsWatermark"`
	MaxImages              int      `json:"maxImages"`
	MaxVideos              int      `json:"maxVideos"`
	MaxAudios              int      `json:"maxAudios"`
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
	}
	return result
}

func kuaiziProviderAdapterDescriptors() []ProviderAdapterDescriptor {
	return []ProviderAdapterDescriptor{{
		ProviderKind: "kuaizi",
		Family:       "seedance",
		Models: []ProviderModelSpec{{
			ModelKey:               "kuaizi-seedance-2.5",
			DisplayName:            "Seedance 2.5",
			UpstreamMode:           "seedance2.5",
			Capability:             "video",
			Resolutions:            []string{"480p", "720p"},
			Ratios:                 []string{"adaptive", "16:9", "4:3", "1:1", "3:4", "9:16", "21:9"},
			DurationMin:            4,
			DurationMax:            30,
			SupportsSmartDuration:  true,
			SupportsGeneratedAudio: true,
			SupportsWatermark:      true,
			MaxImages:              30,
			MaxVideos:              10,
			MaxAudios:              10,
		}},
	}}
}

func validateProviderRegistryRuntime(descriptors []ProviderAdapterDescriptor) error {
	if _, err := NewProviderRegistry(descriptors); err != nil {
		return fmt.Errorf("validate provider adapter registry: %w", err)
	}
	return nil
}
