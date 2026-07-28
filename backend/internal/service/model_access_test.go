package service

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newModelAccessTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.ChannelModel{},
		&model.ChannelModelPriceTier{},
		&model.MembershipOrder{},
		&model.MembershipSubscription{},
		&model.TeamMember{},
		&model.SystemSetting{},
	); err != nil {
		t.Fatal(err)
	}
	item := model.ChannelModel{
		ID: "member-image", ChannelID: "channel-1", ModelKey: "member-image",
		DisplayName: "会员图片模型", AccessPolicy: model.ModelAccessMember, Capability: "image",
		BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 100_000,
		PriceConfigured: true, Enabled: true, PriceVersion: 1,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	return &Service{repo: repository.New(db), dataDir: t.TempDir()}, db
}

func TestMemberModelRejectsUserWithoutActiveSubscription(t *testing.T) {
	svc, _ := newModelAccessTestService(t)

	_, err := svc.newBillingOrder("user-1", "", "request-1", "channel-1", "member-image", "image", "canvas_image", BillingUsage{Quantity: 1})

	if err == nil || !strings.Contains(err.Error(), "仅限有效会员") {
		t.Fatalf("newBillingOrder() error = %v, want membership rejection", err)
	}
}

func TestMemberModelAllowsActivePersonalSubscription(t *testing.T) {
	svc, db := newModelAccessTestService(t)
	now := time.Now()
	endsAt := now.Add(24 * time.Hour)
	subscription := model.MembershipSubscription{
		ID: "subscription-1", UserID: "user-1", PlanID: "pro", OrderID: "order-1",
		Status: model.MembershipSubscriptionActive, StartsAt: now.Add(-time.Hour), EndsAt: &endsAt,
	}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatal(err)
	}

	order, err := svc.newBillingOrder("user-1", "", "request-1", "channel-1", "member-image", "image", "canvas_image", BillingUsage{Quantity: 1})

	if err != nil {
		t.Fatal(err)
	}
	if order.ChannelModelID != "member-image" || order.AmountMicrocredits != 100_000 {
		t.Fatalf("newBillingOrder() = %#v", order)
	}
}

func TestMemberModelAllowsActiveTeamSeat(t *testing.T) {
	svc, db := newModelAccessTestService(t)
	now := time.Now()
	endsAt := now.Add(24 * time.Hour)
	subscription := model.MembershipSubscription{
		ID: "subscription-team", TeamID: "team-1", PlanID: "team-pro", OrderID: "order-team",
		Status: model.MembershipSubscriptionActive, StartsAt: now.Add(-time.Hour), EndsAt: &endsAt,
	}
	member := model.TeamMember{ID: "member-1", TeamID: "team-1", UserID: "user-1", Role: model.TeamMemberRoleMember, Status: model.TeamMemberStatusActive}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&member).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := svc.newBillingOrder("user-1", "", "request-1", "channel-1", "member-image", "image", "canvas_image", BillingUsage{Quantity: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestPublicChannelMarksMemberModelAccessibility(t *testing.T) {
	channel := model.ModelChannel{ID: "channel-1", Scope: model.ChannelScopeSystem, Enabled: true}
	item := model.ChannelModel{
		ID: "member-image", ChannelID: channel.ID, ModelKey: "member-image", DisplayName: "会员图片模型",
		AccessPolicy: model.ModelAccessMember, Capability: "image", BillingMode: "fixed_request",
		PriceStrategy: "flat", UnitPriceMicrocredits: 100_000, PriceConfigured: true, Enabled: true,
	}

	guestCatalog := publicChannel(channel, false, []model.ChannelModel{item}, false)
	memberCatalog := publicChannel(channel, false, []model.ChannelModel{item}, true)

	if len(guestCatalog.ModelCosts) != 1 || guestCatalog.ModelCosts[0].Accessible {
		t.Fatalf("guest catalog = %#v", guestCatalog.ModelCosts)
	}
	if guestCatalog.ModelCosts[0].AccessPolicy != model.ModelAccessMember || !memberCatalog.ModelCosts[0].Accessible {
		t.Fatalf("member catalog = %#v", memberCatalog.ModelCosts)
	}
}

func TestAdminChannelModelIconUploadAndRemoval(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ChannelModel{}, &model.ChannelModelPriceTier{}, &model.AdminAuditEvent{}); err != nil {
		t.Fatal(err)
	}
	item := model.ChannelModel{
		ID: "image-model", ChannelID: "channel-1", ModelKey: "image-model",
		AccessPolicy: model.ModelAccessAuthenticated, Capability: "image", Enabled: true,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db), dataDir: t.TempDir()}
	admin := &model.User{ID: "admin-1", Role: model.UserRoleAdmin}

	updated, err := svc.UpdateAdminChannelModelIcon(admin, item.ChannelID, item.ID, pngFileHeader(t))
	if err != nil {
		t.Fatal(err)
	}
	if updated.IconURL == "" {
		t.Fatal("uploaded model icon URL should not be empty")
	}
	filePath, mimeType, _, err := svc.ChannelModelIconFile(admin, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if mimeType != "image/png" {
		t.Fatalf("mime type = %q, want image/png", mimeType)
	}
	if _, err := os.Stat(filePath); err != nil {
		t.Fatal(err)
	}

	removed, err := svc.RemoveAdminChannelModelIcon(admin, item.ChannelID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if removed.IconURL != "" {
		t.Fatalf("removed icon URL = %q, want empty", removed.IconURL)
	}
	if _, _, _, err := svc.ChannelModelIconFile(admin, item.ID); !errors.Is(err, ErrChannelModelIconNotConfigured) {
		t.Fatalf("icon after removal error = %v", err)
	}
	var auditCount int64
	if err := db.Model(&model.AdminAuditEvent{}).Where("target_type = ? AND target_id = ?", "channel_model", item.ID).Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if auditCount != 2 {
		t.Fatalf("audit count = %d, want 2", auditCount)
	}
}

func TestSaveAdminChannelModelPersistsAccessPolicyAndAuditAtomically(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ModelChannel{}, &model.ChannelModel{}, &model.ChannelModelPriceTier{}, &model.AdminAuditEvent{}); err != nil {
		t.Fatal(err)
	}
	channel := model.ModelChannel{
		ID: "channel-1", UserID: "admin-1", Scope: model.ChannelScopeSystem, Enabled: true,
		Name: "系统图片", InterfaceType: model.ChannelInterfaceOpenAIImage, ModelsJSON: "[]",
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db), dataDir: t.TempDir()}
	admin := &model.User{ID: "admin-1", Role: model.UserRoleAdmin}
	enabled := true

	saved, err := svc.SaveAdminChannelModel(admin, channel.ID, "", ChannelModelRequest{
		ModelKey: "member-image", DisplayName: "会员图片", AccessPolicy: model.ModelAccessMember,
		Capability: "image", BillingMode: "fixed_request", PriceStrategy: "flat",
		UnitPriceMicrocredits: 100_000, PriceConfigured: true, Enabled: &enabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.AccessPolicy != model.ModelAccessMember {
		t.Fatalf("access policy = %q, want member", saved.AccessPolicy)
	}
	var storedChannel model.ModelChannel
	if err := db.First(&storedChannel, "id = ?", channel.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedChannel.ModelsJSON != `["member-image"]` {
		t.Fatalf("models json = %s", storedChannel.ModelsJSON)
	}
	var auditCount int64
	if err := db.Model(&model.AdminAuditEvent{}).Where("action = ? AND target_id = ?", "channel_model.save", saved.ID).Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("audit count = %d, want 1", auditCount)
	}
}
