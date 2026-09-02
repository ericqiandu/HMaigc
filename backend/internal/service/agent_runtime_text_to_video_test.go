package service

import (
	"testing"

	"infinite-canvas/backend/internal/model"
)

func TestVideoProvidersPublishTextToVideoCapability(t *testing.T) {
	registry, err := NewProviderRegistry(kuaiziProviderAdapterDescriptors())
	if err != nil {
		t.Fatal(err)
	}
	seedance, ok := registry.Descriptor(kuaiziProviderKind, "seedance")
	if !ok || len(seedance.Models) != 4 {
		t.Fatalf("Seedance descriptor = %#v, exists=%v", seedance, ok)
	}
	for _, item := range seedance.Models {
		if !item.SupportsTextToVideo {
			t.Fatalf("Seedance model %q did not publish text-to-video capability", item.ModelKey)
		}
	}
	kling, ok := registry.Descriptor(kuaiziProviderKind, "kling")
	if !ok || len(kling.Models) != 1 || !kling.Models[0].SupportsTextToVideo {
		t.Fatalf("Kling descriptor = %#v, exists=%v", kling, ok)
	}
	public := publicProviderModelCapabilities(model.ChannelInterfaceAIOpenVideoVolcengine, kuaiziKlingModel, "video")
	if public == nil || !public.SupportsTextToVideo {
		t.Fatalf("Kling public capabilities = %#v", public)
	}
}
