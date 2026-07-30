package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newChannelVoiceTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.ModelChannel{}, &model.ChannelModel{}, &model.ChannelVoice{}, &model.ChannelVoicePreview{},
		&model.AdminAuditEvent{}, &model.MembershipSubscription{}, &model.TeamMember{},
	); err != nil {
		t.Fatal(err)
	}
	return &Service{repo: repository.New(db), dataDir: t.TempDir()}, db
}

func TestRunMiniMaxAudioTaskMapsRequestAndDecodesHexAudio(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/t2a_v2" {
			t.Errorf("path = %q, want /v1/t2a_v2", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
		}
		var body map[string]interface{}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != "speech-2.8-hd" || body["text"] != "欢迎使用弘梦" || body["output_format"] != "hex" {
			t.Errorf("request body = %#v", body)
		}
		voiceSetting, _ := body["voice_setting"].(map[string]interface{})
		if voiceSetting["voice_id"] != "HMVoice001" || voiceSetting["speed"] != 1.1 || voiceSetting["vol"] != 1.4 || voiceSetting["pitch"] != float64(-2) || voiceSetting["emotion"] != "calm" {
			t.Errorf("voice_setting = %#v", voiceSetting)
		}
		audioSetting, _ := body["audio_setting"].(map[string]interface{})
		if audioSetting["format"] != "mp3" || audioSetting["sample_rate"] != float64(44100) || audioSetting["bitrate"] != float64(256000) || audioSetting["channel"] != float64(2) {
			t.Errorf("audio_setting = %#v", audioSetting)
		}
		if body["language_boost"] != "Chinese" {
			t.Errorf("language_boost = %#v", body["language_boost"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"audio":"494433","status":2},"trace_id":"trace-audio-1","base_resp":{"status_code":0,"status_msg":""}}`)
	}))
	defer server.Close()

	result, err := runMiniMaxAudioTask(context.Background(), canvasGenerationInput{
		Prompt: "欢迎使用弘梦",
		Config: providerConfig{
			InterfaceType: string(model.ChannelInterfaceMiniMaxSpeech),
			BaseURL:       server.URL + "/v1", APIKey: "test-key", Model: "speech-2.8-hd",
			AudioVoice: "HMVoice001", AudioFormat: "mp3", AudioSpeed: "1.1",
			AudioVolume: "1.4", AudioPitch: "-2", AudioEmotion: "calm",
			AudioLanguageBoost: "Chinese", AudioSampleRate: "44100", AudioBitrate: "256000", AudioChannel: "2",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	audio, _ := result["audio"].(map[string]interface{})
	if audio["mimeType"] != "audio/mpeg" || !strings.HasPrefix(audio["dataUrl"].(string), "data:audio/mpeg;base64,") || audio["traceId"] != "trace-audio-1" {
		t.Fatalf("audio result = %#v", audio)
	}
}

func TestMiniMaxBusinessErrorIsExplicit(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"base_resp":{"status_code":1004,"status_msg":"voice id not found"},"trace_id":"trace-error"}`)
	}))
	defer server.Close()

	_, err := runMiniMaxAudioTask(context.Background(), canvasGenerationInput{
		Prompt: "test",
		Config: providerConfig{
			InterfaceType: string(model.ChannelInterfaceMiniMaxSpeech),
			BaseURL:       server.URL, APIKey: "test-key", Model: "speech-2.8-hd",
			AudioVoice: "HMVoice001", AudioFormat: "mp3",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "1004") || !strings.Contains(err.Error(), "voice id not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunMiniMaxAudioTaskRejectsOutOfRangeSpeedBeforeRequest(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer server.Close()

	_, err := runMiniMaxAudioTask(context.Background(), canvasGenerationInput{
		Prompt: "test",
		Config: providerConfig{
			InterfaceType: string(model.ChannelInterfaceMiniMaxSpeech),
			BaseURL:       server.URL, APIKey: "test-key", Model: "speech-2.8-hd",
			AudioVoice: "HMVoice001", AudioFormat: "mp3", AudioSpeed: "4",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "[0.5, 2]") {
		t.Fatalf("error = %v", err)
	}
	if called {
		t.Fatal("invalid speed reached MiniMax upstream")
	}
}

func TestMiniMaxAudioSettingsRejectUnsupportedWhisperAndBitrate(t *testing.T) {
	_, err := parseMiniMaxAudioSettings(providerConfig{
		Model: "speech-2.8-hd", AudioFormat: "mp3", AudioEmotion: "whisper",
	})
	if err == nil || !strings.Contains(err.Error(), "speech-2.8") {
		t.Fatalf("whisper error = %v", err)
	}
	_, err = parseMiniMaxAudioSettings(providerConfig{
		Model: "speech-2.8-hd", AudioFormat: "mp3", AudioBitrate: "96000",
	})
	if err == nil || !strings.Contains(err.Error(), "比特率不支持") {
		t.Fatalf("bitrate error = %v", err)
	}
}

func TestMiniMaxVoiceLanguageUsesProviderValueAndExplicitPrefix(t *testing.T) {
	tests := []struct {
		name             string
		voiceKey         string
		providerLanguage string
		want             string
	}{
		{name: "provider", voiceKey: "custom", providerLanguage: "Japanese", want: "Japanese"},
		{name: "mandarin", voiceKey: "Chinese (Mandarin)_Reliable_Executive", want: "Chinese"},
		{name: "cantonese", voiceKey: "Cantonese_Professional_Female", want: "Chinese,Yue"},
		{name: "prefixed", voiceKey: "Korean_AirheadedGirl", want: "Korean"},
		{name: "unknown", voiceKey: "male-qn-qingse", want: ""},
	}
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			if got := miniMaxVoiceLanguage(item.voiceKey, item.providerLanguage); got != item.want {
				t.Fatalf("miniMaxVoiceLanguage(%q, %q) = %q, want %q", item.voiceKey, item.providerLanguage, got, item.want)
			}
		})
	}
}

func TestMiniMaxChannelVoicePreviewGeneratesOnceAndUsesVerifiedCache(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requestCount++
		if request.URL.Path != "/v1/t2a_v2" {
			t.Errorf("path = %q, want /v1/t2a_v2", request.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"audio":"494433","status":2},"trace_id":"trace-preview","base_resp":{"status_code":0,"status_msg":""}}`)
	}))
	defer server.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database.Models()...); err != nil {
		t.Fatal(err)
	}
	svc := New(repository.New(db), t.TempDir())
	user := model.User{ID: "preview-user", Username: "preview-user", Role: model.UserRoleUser, Status: model.UserStatusActive}
	channel := model.ModelChannel{
		ID: "preview-channel", UserID: "admin", Scope: model.ChannelScopeSystem, Enabled: true,
		Name: "MiniMax", BaseURL: server.URL + "/v1", APIKey: "test-key", APIFormat: "openai",
		InterfaceType: model.ChannelInterfaceMiniMaxSpeech, ModelsJSON: `["speech-2.8-hd"]`,
	}
	channelModel := model.ChannelModel{
		ID: "preview-model", ChannelID: channel.ID, ModelKey: "speech-2.8-hd", DisplayName: "MiniMax Speech",
		AccessPolicy: model.ModelAccessAuthenticated, Capability: "audio", Enabled: true,
	}
	voice := model.ChannelVoice{
		ID: "preview-voice", ChannelID: channel.ID, VoiceKey: "Korean_AirheadedGirl", DisplayName: "Airheaded Girl",
		Language: "Korean", Kind: "system", AccessPolicy: model.ModelAccessAuthenticated,
		CompatibleModelsJSON: `["speech-2.8-hd"]`, ProviderStatus: "active", Enabled: true,
	}
	for _, item := range []interface{}{&user, &channel, &channelModel, &voice} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}

	first, err := svc.MiniMaxChannelVoicePreview(context.Background(), &user, channel.ID, voice.ID, ChannelVoicePreviewRequest{Model: channelModel.ModelKey})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.MiniMaxChannelVoicePreview(context.Background(), &user, channel.ID, voice.ID, ChannelVoicePreviewRequest{Model: channelModel.ModelKey})
	if err != nil {
		t.Fatal(err)
	}
	if requestCount != 1 {
		t.Fatalf("upstream request count = %d, want 1", requestCount)
	}
	if first.Cached || !second.Cached {
		t.Fatalf("cache states: first=%v second=%v", first.Cached, second.Cached)
	}
	if first.AudioDataURL != second.AudioDataURL || !strings.HasPrefix(first.AudioDataURL, "data:audio/mpeg;base64,") {
		t.Fatalf("preview urls: first=%q second=%q", first.AudioDataURL, second.AudioDataURL)
	}
	if first.SHA256 == "" || first.TraceID != "trace-preview" {
		t.Fatalf("preview metadata = %#v", first)
	}
	var callLogs int64
	if err := db.Model(&model.ApiCallLog{}).Where("source = ? AND operation = ?", "voice-preview", "voice_preview").Count(&callLogs).Error; err != nil {
		t.Fatal(err)
	}
	if callLogs != 1 {
		t.Fatalf("voice preview audit logs = %d, want 1", callLogs)
	}
}

func TestValidateAudioTaskVoiceEnforcesMembershipAndModelCompatibility(t *testing.T) {
	svc, db := newChannelVoiceTestService(t)
	voice := model.ChannelVoice{
		ID: "voice-1", ChannelID: "channel-1", VoiceKey: "HMVoice001", DisplayName: "会员音色",
		Kind: "system", AccessPolicy: model.ModelAccessMember, CompatibleModelsJSON: `["speech-2.8-hd"]`,
		ProviderStatus: "active", Enabled: true,
	}
	if err := db.Create(&voice).Error; err != nil {
		t.Fatal(err)
	}
	config := providerConfig{ChannelID: "channel-1", InterfaceType: string(model.ChannelInterfaceMiniMaxSpeech), Model: "speech-2.8-hd", AudioVoice: "HMVoice001"}
	if err := svc.validateAudioTaskVoice("user-1", config); err == nil || !strings.Contains(err.Error(), "会员") {
		t.Fatalf("non-member error = %v", err)
	}
	now := time.Now()
	endsAt := now.Add(time.Hour)
	snapshot, _ := json.Marshal(model.MembershipPlan{ID: "pro", Audience: model.MembershipAudiencePersonal})
	if err := db.Create(&model.MembershipSubscription{
		ID: "subscription-1", UserID: "user-1", PlanID: "pro", OrderID: "order-1",
		Status: model.MembershipSubscriptionActive, StartsAt: now.Add(-time.Hour), EndsAt: &endsAt, PlanSnapshotJSON: string(snapshot),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.validateAudioTaskVoice("user-1", config); err != nil {
		t.Fatalf("member validation error = %v", err)
	}
	config.Model = "speech-2.8-turbo"
	if err := svc.validateAudioTaskVoice("user-1", config); err == nil || !strings.Contains(err.Error(), "不支持当前音频模型") {
		t.Fatalf("incompatible model error = %v", err)
	}
}

func TestSyncMiniMaxChannelVoicesIsRepeatable(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/get_voice" {
			t.Errorf("path = %q", request.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"system_voice":[{"voice_id":"male-qn-qingse","voice_name":"青涩青年","description":["清晰自然","适合旁白"]}],"voice_cloning":[],"voice_generation":[],"base_resp":{"status_code":0,"status_msg":""}}`)
	}))
	defer server.Close()
	svc, db := newChannelVoiceTestService(t)
	admin := &model.User{ID: "admin-1", Role: model.UserRoleAdmin}
	channel := model.ModelChannel{
		ID: "channel-1", UserID: admin.ID, Scope: model.ChannelScopeSystem, Enabled: true,
		Name: "MiniMax", BaseURL: server.URL + "/v1", APIKey: "test-key", InterfaceType: model.ChannelInterfaceMiniMaxSpeech,
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		voices, err := svc.SyncMiniMaxChannelVoices(context.Background(), admin, channel.ID)
		if err != nil {
			t.Fatalf("attempt %d: %v", attempt+1, err)
		}
		if len(voices) != 1 || voices[0].VoiceKey != "male-qn-qingse" {
			t.Fatalf("attempt %d voices = %#v", attempt+1, voices)
		}
		if voices[0].Description != "清晰自然；适合旁白" {
			t.Fatalf("attempt %d description = %q", attempt+1, voices[0].Description)
		}
	}
	var count int64
	if err := db.Model(&model.ChannelVoice{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("voice count = %d, want 1", count)
	}
}

func TestSyncMiniMaxChannelVoicesDisablesMissingActiveVoice(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"system_voice":[{"voice_id":"current-voice","voice_name":"当前音色","description":[]}],"voice_cloning":[],"voice_generation":[],"base_resp":{"status_code":0,"status_msg":""}}`)
	}))
	defer server.Close()
	svc, db := newChannelVoiceTestService(t)
	admin := &model.User{ID: "admin-1", Role: model.UserRoleAdmin}
	channel := model.ModelChannel{
		ID: "channel-1", UserID: admin.ID, Scope: model.ChannelScopeSystem, Enabled: true,
		Name: "MiniMax", BaseURL: server.URL + "/v1", APIKey: "test-key", InterfaceType: model.ChannelInterfaceMiniMaxSpeech,
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	stale := model.ChannelVoice{
		ID: "voice-stale", ChannelID: channel.ID, VoiceKey: "stale-voice", DisplayName: "已下架音色",
		Kind: "system", AccessPolicy: model.ModelAccessAuthenticated, CompatibleModelsJSON: "[]",
		ProviderStatus: "active", Enabled: true,
	}
	if err := db.Create(&stale).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SyncMiniMaxChannelVoices(context.Background(), admin, channel.ID); err != nil {
		t.Fatal(err)
	}
	var saved model.ChannelVoice
	if err := db.First(&saved, "id = ?", stale.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Enabled || saved.ProviderStatus != "missing" || !strings.Contains(saved.LastError, "供应商音色目录") {
		t.Fatalf("stale voice = %#v", saved)
	}
}
