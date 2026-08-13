package service

import (
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTaskWatermarkCapabilityUsesClosedBackendFacts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ModelChannel{}, &model.ChannelModel{}, &model.ChannelModelPriceTier{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	channels := []model.ModelChannel{
		{ID: "controlled-video", Scope: model.ChannelScopeSystem, Enabled: true, InterfaceType: model.ChannelInterfaceMiniMaxVideo, CreatedAt: now, UpdatedAt: now},
		{ID: "unsupported-image", Scope: model.ChannelScopeSystem, Enabled: true, InterfaceType: model.ChannelInterfaceOpenAIImage, CreatedAt: now, UpdatedAt: now},
		{ID: "unknown-video", Scope: model.ChannelScopeSystem, Enabled: true, InterfaceType: model.ChannelInterfaceType("unknown-video"), CreatedAt: now, UpdatedAt: now},
	}
	for _, channel := range channels {
		if err := db.Create(&channel).Error; err != nil {
			t.Fatal(err)
		}
	}
	models := []model.ChannelModel{
		{ID: "controlled-model", ChannelID: "controlled-video", ModelKey: "video-controlled", Capability: "video", Enabled: true, CreatedAt: now, UpdatedAt: now},
		{ID: "unsupported-model", ChannelID: "unsupported-image", ModelKey: "image-unsupported", Capability: "image", Enabled: true, CreatedAt: now, UpdatedAt: now},
		{ID: "unknown-model", ChannelID: "unknown-video", ModelKey: "video-unknown", Capability: "video", Enabled: true, CreatedAt: now, UpdatedAt: now},
	}
	for _, item := range models {
		if err := db.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := &Service{repo: repository.New(db)}

	controlled, err := svc.taskWatermarkCapability("video", &model.BillingOrder{ChannelModelID: "controlled-model"})
	if err != nil || controlled != model.WatermarkCapabilityControlled {
		t.Fatalf("controlled capability = %q, err=%v", controlled, err)
	}
	unsupported, err := svc.taskWatermarkCapability("image", &model.BillingOrder{ChannelModelID: "unsupported-model"})
	if err != nil || unsupported != model.WatermarkCapabilityUnsupported {
		t.Fatalf("unsupported capability = %q, err=%v", unsupported, err)
	}
	if _, err := svc.taskWatermarkCapability("video", &model.BillingOrder{ChannelModelID: "unknown-model"}); err == nil || !strings.Contains(err.Error(), "缺少明确的水印能力契约") {
		t.Fatalf("unknown media capability error = %v", err)
	}
	if got, err := svc.taskWatermarkCapability("text", nil); err != nil || got != model.WatermarkCapabilityNotApplicable {
		t.Fatalf("text capability = %q, err=%v", got, err)
	}
}

func TestTaskWatermarkCapabilityUsesManagedRegistryIdentity(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ModelChannel{}, &model.ChannelModel{}, &model.ChannelModelPriceTier{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	channel := model.ModelChannel{ID: "kuaizi", Scope: model.ChannelScopeSystem, Enabled: true, InterfaceType: model.ChannelInterfaceNewAPIVideo, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	items := []model.ChannelModel{
		{ID: "seedance", ChannelID: channel.ID, ProviderCredentialID: "credential", ModelKey: "doubao-seedance-2-5-260628", Capability: "video", Enabled: true, CreatedAt: now, UpdatedAt: now},
		{ID: "spoofed", ChannelID: channel.ID, ProviderCredentialID: "credential", ModelKey: "not-registered", Capability: "video", Enabled: true, CreatedAt: now, UpdatedAt: now},
	}
	for _, item := range items {
		if err := db.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := &Service{repo: repository.New(db)}
	got, err := svc.taskWatermarkCapability("video", &model.BillingOrder{ChannelModelID: "seedance"})
	if err != nil || got != model.WatermarkCapabilityControlled {
		t.Fatalf("managed capability = %q, err=%v", got, err)
	}
	if _, err := svc.taskWatermarkCapability("video", &model.BillingOrder{ChannelModelID: "spoofed"}); err == nil {
		t.Fatal("unregistered managed model was accepted")
	}
}
