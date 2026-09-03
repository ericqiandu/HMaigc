package service

import (
	"testing"

	"infinite-canvas/backend/internal/model"
)

func TestAdminReferencesIncludesChannelInterfaceType(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	channel := model.ModelChannel{
		ID:            "admin-reference-channel",
		Scope:         model.ChannelScopeSystem,
		Name:          "视觉理解渠道",
		BaseURL:       "https://api.example.com",
		InterfaceType: model.ChannelInterfaceOpenAIResponse,
		Enabled:       true,
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}

	references, err := svc.AdminReferences(providerAdmin())
	if err != nil {
		t.Fatal(err)
	}
	if len(references.Channels) != 1 {
		t.Fatalf("channels = %d, want 1", len(references.Channels))
	}
	if references.Channels[0].InterfaceType != model.ChannelInterfaceOpenAIResponse {
		t.Fatalf("interfaceType = %q, want %q", references.Channels[0].InterfaceType, model.ChannelInterfaceOpenAIResponse)
	}
}
