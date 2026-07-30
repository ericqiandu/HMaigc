package service

import (
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
)

func TestDefaultMiniMaxSystemVoicesAreCompleteAndClassified(t *testing.T) {
	voices, err := defaultMiniMaxSystemVoices("channel-1", time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(voices) != defaultMiniMaxSystemVoiceCount {
		t.Fatalf("default voice count = %d, want %d", len(voices), defaultMiniMaxSystemVoiceCount)
	}
	seen := make(map[string]bool, len(voices))
	chineseCount := 0
	for _, voice := range voices {
		if voice.ChannelID != "channel-1" || voice.VoiceKey == "" || voice.DisplayName == "" || voice.Language == "" {
			t.Fatalf("invalid default voice = %#v", voice)
		}
		if seen[voice.VoiceKey] {
			t.Fatalf("duplicate default voice key %q", voice.VoiceKey)
		}
		seen[voice.VoiceKey] = true
		if voice.Language == "Chinese" || voice.Language == "Chinese,Yue" {
			chineseCount++
		}
	}
	if chineseCount == 0 {
		t.Fatal("default catalog has no Chinese voices")
	}
}

func TestEnsureDefaultChannelVoicesIsRepeatableAndPreservesOperations(t *testing.T) {
	svc, db := newChannelVoiceTestService(t)
	miniMaxChannel := model.ModelChannel{
		ID: "minimax-channel", UserID: "admin", Scope: model.ChannelScopeSystem, Enabled: true,
		Name: "MiniMax", BaseURL: "https://api.minimaxi.com/v1", APIKey: "test-key",
		InterfaceType: model.ChannelInterfaceMiniMaxSpeech,
	}
	otherChannel := model.ModelChannel{
		ID: "other-channel", UserID: "admin", Scope: model.ChannelScopeSystem, Enabled: true,
		Name: "Other", BaseURL: "https://example.com/v1", APIKey: "test-key",
		InterfaceType: model.ChannelInterfaceChatCompletion,
	}
	for _, channel := range []*model.ModelChannel{&miniMaxChannel, &otherChannel} {
		if err := db.Create(channel).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := svc.EnsureDefaultChannelVoices(); err != nil {
		t.Fatal(err)
	}
	var seeded []model.ChannelVoice
	if err := db.Where("channel_id = ?", miniMaxChannel.ID).Find(&seeded).Error; err != nil {
		t.Fatal(err)
	}
	if len(seeded) != defaultMiniMaxSystemVoiceCount {
		t.Fatalf("seeded voice count = %d, want %d", len(seeded), defaultMiniMaxSystemVoiceCount)
	}
	var otherCount int64
	if err := db.Model(&model.ChannelVoice{}).Where("channel_id = ?", otherChannel.ID).Count(&otherCount).Error; err != nil {
		t.Fatal(err)
	}
	if otherCount != 0 {
		t.Fatalf("non-MiniMax voice count = %d, want 0", otherCount)
	}

	operated := seeded[0]
	if err := db.Model(&model.ChannelVoice{}).Where("id = ?", operated.ID).Update("display_name", "运营自定义名称").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ChannelVoice{}).Where("id = ?", operated.ID).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ChannelVoice{}).Where("id = ?", operated.ID).Update("description", "").Error; err != nil {
		t.Fatal(err)
	}
	removed := seeded[1]
	if err := db.Delete(&removed).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureDefaultChannelVoices(); err != nil {
		t.Fatal(err)
	}
	var after model.ChannelVoice
	if err := db.First(&after, "id = ?", operated.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.DisplayName != "运营自定义名称" || after.Description != "" || after.Enabled {
		t.Fatalf("operated voice was overwritten: %#v", after)
	}
	var repeatedCount int64
	if err := db.Model(&model.ChannelVoice{}).Where("channel_id = ?", miniMaxChannel.ID).Count(&repeatedCount).Error; err != nil {
		t.Fatal(err)
	}
	if repeatedCount != int64(defaultMiniMaxSystemVoiceCount-1) {
		t.Fatalf("repeated active seed count = %d, want %d", repeatedCount, defaultMiniMaxSystemVoiceCount-1)
	}
	var totalWithDeleted int64
	if err := db.Unscoped().Model(&model.ChannelVoice{}).Where("channel_id = ?", miniMaxChannel.ID).Count(&totalWithDeleted).Error; err != nil {
		t.Fatal(err)
	}
	if totalWithDeleted != int64(defaultMiniMaxSystemVoiceCount) {
		t.Fatalf("repeated total seed count = %d, want %d", totalWithDeleted, defaultMiniMaxSystemVoiceCount)
	}
}

func TestCreateMiniMaxSystemChannelSeedsDefaultVoicesImmediately(t *testing.T) {
	svc, _ := newChannelVoiceTestService(t)
	admin := &model.User{ID: "admin-1", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	enabled := true
	created, err := svc.CreateSystemChannel(admin, ChannelRequest{
		Name: "MiniMax", BaseURL: "https://api.minimaxi.com/v1", APIKey: "test-key",
		InterfaceType: string(model.ChannelInterfaceMiniMaxSpeech),
		Models:        []string{"speech-2.8-hd", "speech-2.8-turbo"},
		Enabled:       &enabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Voices) != defaultMiniMaxSystemVoiceCount {
		t.Fatalf("created channel voice count = %d, want %d", len(created.Voices), defaultMiniMaxSystemVoiceCount)
	}
	if created.Voices[0].Language != "Chinese" {
		t.Fatalf("first created channel voice = %#v", created.Voices[0])
	}
}

func TestPublicChannelVoicesPutChineseLanguagesFirst(t *testing.T) {
	voices := []model.ChannelVoice{
		{ID: "english", VoiceKey: "English_Narrator", DisplayName: "English", Language: "English", Kind: "system", AccessPolicy: model.ModelAccessAuthenticated, CompatibleModelsJSON: "[]", ProviderStatus: "active", Enabled: true},
		{ID: "mandarin", VoiceKey: "Chinese_Narrator", DisplayName: "普通话", Language: "Chinese", Kind: "system", AccessPolicy: model.ModelAccessAuthenticated, CompatibleModelsJSON: "[]", ProviderStatus: "active", Enabled: true},
		{ID: "cantonese", VoiceKey: "Cantonese_Narrator", DisplayName: "粤语", Language: "Chinese,Yue", Kind: "system", AccessPolicy: model.ModelAccessAuthenticated, CompatibleModelsJSON: "[]", ProviderStatus: "active", Enabled: true},
	}
	public, err := publicChannelVoicesForUser(voices, true, false, "user-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(public) != 3 || public[0].Language != "Chinese" || public[1].Language != "Chinese,Yue" || public[2].Language != "English" {
		t.Fatalf("public voice order = %#v", public)
	}
}
