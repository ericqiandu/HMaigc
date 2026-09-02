package service

import (
	"context"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRuntimeConcurrencySettingPersistsAndChannelOverrideWins(t *testing.T) {
	t.Setenv("REDIS_URL", "")
	t.Setenv("CANVAS_WORKER_CONCURRENCY", "3")
	t.Setenv("CANVAS_CHANNEL_CONCURRENCY", "3")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(&model.SystemSetting{}, &model.AdminAuditEvent{}, &model.ModelChannel{}, &model.ChannelModel{}, &model.ProviderCredential{}); err != nil {
		t.Fatal(err)
	}
	svc := New(repository.New(db), t.TempDir())
	actor := &model.User{ID: "admin-1", Role: model.UserRoleAdmin}
	policy := defaultRuntimePolicy()
	policy.Task.WorkerConcurrency = 8
	policy.Task.ChannelConcurrency = 6
	setting, err := svc.UpdateRuntimePolicySetting(actor, policy)
	if err != nil {
		t.Fatal(err)
	}
	if setting.Task.WorkerConcurrency != 8 || setting.Task.ChannelConcurrency != 6 {
		t.Fatalf("setting = %#v", setting)
	}

	channel := model.ModelChannel{ID: "channel-1", Scope: model.ChannelScopeSystem, Enabled: true, ConcurrencyLimit: 0}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	release, limit, err := svc.AcquireChannelSlot(context.Background(), channel.ID, "", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if limit != 6 {
		t.Fatalf("global channel limit = %d, want 6", limit)
	}

	if err := db.Model(&channel).Update("concurrency_limit", 9).Error; err != nil {
		t.Fatal(err)
	}
	release, limit, err = svc.AcquireChannelSlot(context.Background(), channel.ID, "", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if limit != 9 {
		t.Fatalf("overridden channel limit = %d, want 9", limit)
	}
}

func TestSharedProviderCredentialUsesOneConcurrencyScopeAcrossChannels(t *testing.T) {
	t.Setenv("REDIS_URL", "")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.SystemSetting{}, &model.ModelChannel{}, &model.ChannelModel{}, &model.ChannelModelPriceTier{}, &model.ProviderCredential{}); err != nil {
		t.Fatal(err)
	}
	credential := model.ProviderCredential{ID: "shared", ProviderAccountID: kuaiziAccountID, Family: "account", Enabled: true, ConcurrencyLimit: 1}
	channels := []model.ModelChannel{
		{ID: "video", Scope: model.ChannelScopeSystem, Enabled: true, ConcurrencyLimit: 9},
		{ID: "agent", Scope: model.ChannelScopeSystem, Enabled: true, ConcurrencyLimit: 9},
	}
	models := []model.ChannelModel{
		{ID: "video-model", ChannelID: channels[0].ID, ProviderCredentialID: credential.ID, ModelKey: "seedance", Enabled: true},
		{ID: "agent-model", ChannelID: channels[1].ID, ProviderCredentialID: credential.ID, ModelKey: "gpt", Enabled: true},
	}
	for _, row := range []any{&credential, &channels, &models} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := New(repository.New(db), t.TempDir())
	release, limit, err := svc.AcquireChannelSlot(context.Background(), channels[0].ID, models[0].ModelKey, "", time.Minute)
	if err != nil || limit != 1 {
		t.Fatalf("first shared slot = limit %d, error %v", limit, err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, _, err := svc.AcquireChannelSlot(ctx, channels[1].ID, models[1].ModelKey, "", time.Minute); err == nil {
		t.Fatal("second channel bypassed shared credential concurrency")
	}
}

func TestVisionTaskUsesOtherConcurrencyClass(t *testing.T) {
	class, err := taskConcurrencyCapability(agentVisionTaskType)
	if err != nil {
		t.Fatal(err)
	}
	if class != taskCapabilityOther {
		t.Fatalf("vision concurrency class = %q, want %q", class, taskCapabilityOther)
	}
	found := false
	for _, capability := range concurrencyClassCapabilities(class) {
		if capability == "vision" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("other concurrency capabilities omit vision: %#v", concurrencyClassCapabilities(class))
	}
}
