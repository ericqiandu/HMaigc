package service

import (
	"encoding/json"
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
	snapshot, err := json.Marshal(model.MembershipPlan{ID: "pro", Name: "Pro", Tier: "pro", Audience: model.MembershipAudiencePersonal, ImageConcurrency: 6, VideoConcurrency: 4})
	if err != nil {
		t.Fatal(err)
	}
	subscription := model.MembershipSubscription{
		ID: "subscription-1", UserID: "user-1", PlanID: "pro", OrderID: "order-1",
		Status: model.MembershipSubscriptionActive, StartsAt: now.Add(-time.Hour), EndsAt: &endsAt, PlanSnapshotJSON: string(snapshot),
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
	snapshot, err := json.Marshal(model.MembershipPlan{ID: "team-pro", Name: "团队 Pro", Tier: "pro", Audience: model.MembershipAudienceTeam, ImageConcurrency: 6, VideoConcurrency: 4})
	if err != nil {
		t.Fatal(err)
	}
	subscription := model.MembershipSubscription{
		ID: "subscription-team", TeamID: "team-1", PlanID: "team-pro", OrderID: "order-team",
		Status: model.MembershipSubscriptionActive, StartsAt: now.Add(-time.Hour), EndsAt: &endsAt, PlanSnapshotJSON: string(snapshot),
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
		MarketingCopy: "高质量会员图片模型", PromotionBadge: "限时4折",
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
	if guestCatalog.ModelCosts[0].MarketingCopy != item.MarketingCopy || guestCatalog.ModelCosts[0].PromotionBadge != item.PromotionBadge {
		t.Fatalf("model presentation = %#v", guestCatalog.ModelCosts[0])
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
		ModelKey: "member-image", DisplayName: "会员图片", MarketingCopy: "面向会员的高质量图片生成", PromotionBadge: "限时4折",
		BrandKey:     "openai",
		AccessPolicy: model.ModelAccessMember,
		Capability:   "image", BillingMode: "fixed_request", PriceStrategy: "flat",
		UnitPriceMicrocredits: 100_000, PriceConfigured: true, Enabled: &enabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.AccessPolicy != model.ModelAccessMember {
		t.Fatalf("access policy = %q, want member", saved.AccessPolicy)
	}
	if saved.MarketingCopy != "面向会员的高质量图片生成" || saved.PromotionBadge != "限时4折" {
		t.Fatalf("model presentation = %#v", saved)
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

	presentationUpdated, err := svc.SaveAdminChannelModel(admin, channel.ID, saved.ID, ChannelModelRequest{
		ModelKey: saved.ModelKey, DisplayName: saved.DisplayName,
		MarketingCopy: "更新后的会员模型介绍", PromotionBadge: "会员专享",
		BrandKey:     saved.BrandKey,
		AccessPolicy: saved.AccessPolicy, Capability: saved.Capability,
		BillingMode: saved.BillingMode, PriceStrategy: saved.PriceStrategy,
		UnitPriceMicrocredits: saved.UnitPriceMicrocredits, PriceConfigured: saved.PriceConfigured, Enabled: &enabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if presentationUpdated.PriceVersion != saved.PriceVersion {
		t.Fatalf("presentation-only update price version = %d, want %d", presentationUpdated.PriceVersion, saved.PriceVersion)
	}

	priceUpdated, err := svc.SaveAdminChannelModel(admin, channel.ID, saved.ID, ChannelModelRequest{
		ModelKey: presentationUpdated.ModelKey, DisplayName: presentationUpdated.DisplayName,
		MarketingCopy: presentationUpdated.MarketingCopy, PromotionBadge: presentationUpdated.PromotionBadge,
		BrandKey:     presentationUpdated.BrandKey,
		AccessPolicy: presentationUpdated.AccessPolicy, Capability: presentationUpdated.Capability,
		BillingMode: presentationUpdated.BillingMode, PriceStrategy: presentationUpdated.PriceStrategy,
		UnitPriceMicrocredits: 120_000, PriceConfigured: presentationUpdated.PriceConfigured, Enabled: &enabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if priceUpdated.PriceVersion != presentationUpdated.PriceVersion+1 {
		t.Fatalf("pricing update price version = %d, want %d", priceUpdated.PriceVersion, presentationUpdated.PriceVersion+1)
	}

	invalidPresentationCases := []struct {
		name           string
		marketingCopy  string
		promotionBadge string
		wantMessage    string
	}{
		{name: "marketing copy", marketingCopy: strings.Repeat("文", 121), promotionBadge: "限时4折", wantMessage: "推广文案不能超过"},
		{name: "promotion badge", marketingCopy: "正常文案", promotionBadge: strings.Repeat("促", 13), wantMessage: "促销角标不能超过"},
		{name: "marketing copy control character", marketingCopy: "第一行\n第二行", promotionBadge: "限时4折", wantMessage: "推广文案不能包含"},
		{name: "promotion badge control character", marketingCopy: "正常文案", promotionBadge: "限时\n4折", wantMessage: "促销角标不能包含"},
	}
	for _, testCase := range invalidPresentationCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, saveErr := svc.SaveAdminChannelModel(admin, channel.ID, saved.ID, ChannelModelRequest{
				ModelKey: priceUpdated.ModelKey, DisplayName: priceUpdated.DisplayName,
				MarketingCopy: testCase.marketingCopy, PromotionBadge: testCase.promotionBadge,
				BrandKey:     priceUpdated.BrandKey,
				AccessPolicy: priceUpdated.AccessPolicy, Capability: priceUpdated.Capability,
				BillingMode: priceUpdated.BillingMode, PriceStrategy: priceUpdated.PriceStrategy,
				UnitPriceMicrocredits: priceUpdated.UnitPriceMicrocredits, PriceConfigured: priceUpdated.PriceConfigured, Enabled: &enabled,
			})
			if saveErr == nil || !strings.Contains(saveErr.Error(), testCase.wantMessage) {
				t.Fatalf("SaveAdminChannelModel() error = %v, want %q", saveErr, testCase.wantMessage)
			}
		})
	}
	storedModel, err := svc.repo.ChannelModelByID(channel.ID, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedModel.MarketingCopy != priceUpdated.MarketingCopy || storedModel.PromotionBadge != priceUpdated.PromotionBadge {
		t.Fatalf("invalid presentation request mutated model: %#v", storedModel)
	}
}
