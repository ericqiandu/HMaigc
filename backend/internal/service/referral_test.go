package service

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func TestReferralFirstPurchaseGrantsBothLedgersExactlyOnce(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	admin := &model.User{ID: "referral-admin", Username: "referral-admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	inviter := &model.User{ID: "referral-inviter", Username: "inviter", Role: model.UserRoleUser, Status: model.UserStatusActive}
	invitee := &model.User{ID: "referral-invitee", Username: "invitee", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create([]*model.User{admin, inviter, invitee}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	relationship := model.ReferralRelationship{
		ID: "relationship-first-purchase", InviterUserID: inviter.ID, InviteeUserID: invitee.ID,
		ReferralCode: "INVITE1234", Status: model.ReferralRelationshipEligible,
		BoundAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&relationship).Error; err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "pro-month")
	rule := model.ReferralRewardRule{
		ID: "referral-rule-pro-month", MembershipPlanID: plan.ID,
		InviterRewardMicrocredits: 200 * CreditScale, InviteeRewardMicrocredits: 1_000 * CreditScale,
		Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&rule).Error; err != nil {
		t.Fatal(err)
	}

	firstOrder, err := svc.CreateMembershipOrder(invitee, CreateMembershipOrderRequest{PlanID: plan.ID}, "referral-first-order")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AdminConfirmMembershipOrder(admin, firstOrder.ID, ConfirmMembershipOrderRequest{
		ProviderTradeNo: "referral-first-trade", Note: "首购邀请奖励测试",
	}); err != nil {
		t.Fatal(err)
	}
	assertCreditBalance(t, db, inviter.ID, rule.InviterRewardMicrocredits)
	assertCreditBalance(t, db, invitee.ID, plan.CreditsPerPeriod+rule.InviteeRewardMicrocredits)

	var rewards []model.ReferralReward
	if err := db.Find(&rewards).Error; err != nil {
		t.Fatal(err)
	}
	if len(rewards) != 1 || rewards[0].MembershipOrderID != firstOrder.ID {
		t.Fatalf("unexpected referral rewards: %#v", rewards)
	}
	var refreshedRelationship model.ReferralRelationship
	if err := db.First(&refreshedRelationship, "id = ?", relationship.ID).Error; err != nil {
		t.Fatal(err)
	}
	if refreshedRelationship.Status != model.ReferralRelationshipRewarded || refreshedRelationship.RewardedAt == nil {
		t.Fatalf("relationship not marked rewarded: %#v", refreshedRelationship)
	}

	_, duplicateErr := svc.AdminConfirmMembershipOrder(admin, firstOrder.ID, ConfirmMembershipOrderRequest{
		ProviderTradeNo: "referral-duplicate-trade", Note: "重复确认",
	})
	var authErr *AuthError
	if !errors.As(duplicateErr, &authErr) || authErr.Status != http.StatusConflict {
		t.Fatalf("duplicate confirmation error = %v, want conflict", duplicateErr)
	}

	secondOrder, err := svc.CreateMembershipOrder(invitee, CreateMembershipOrderRequest{PlanID: plan.ID}, "referral-renewal-order")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AdminConfirmMembershipOrder(admin, secondOrder.ID, ConfirmMembershipOrderRequest{
		ProviderTradeNo: "referral-renewal-trade", Note: "续费不重复发放邀请奖励",
	}); err != nil {
		t.Fatal(err)
	}
	assertCreditBalance(t, db, inviter.ID, rule.InviterRewardMicrocredits)
	// The renewal subscription starts after the active month and therefore its
	// membership credits are granted by lifecycle reconciliation, not at payment.
	assertCreditBalance(t, db, invitee.ID, plan.CreditsPerPeriod+rule.InviteeRewardMicrocredits)
	var rewardCount int64
	if err := db.Model(&model.ReferralReward{}).Count(&rewardCount).Error; err != nil {
		t.Fatal(err)
	}
	if rewardCount != 1 {
		t.Fatalf("reward count after renewal = %d, want 1", rewardCount)
	}
}

func TestReferralFirstPurchaseFailsExplicitlyWhenRuleMissing(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	admin := &model.User{ID: "referral-missing-admin", Username: "missing-admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	inviter := &model.User{ID: "referral-missing-inviter", Username: "missing-inviter", Role: model.UserRoleUser, Status: model.UserStatusActive}
	invitee := &model.User{ID: "referral-missing-invitee", Username: "missing-invitee", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create([]*model.User{admin, inviter, invitee}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := db.Create(&model.ReferralRelationship{
		ID: "relationship-missing-rule", InviterUserID: inviter.ID, InviteeUserID: invitee.ID,
		ReferralCode: "MISSING123", Status: model.ReferralRelationshipEligible,
		BoundAt: now, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "max-month")
	order, err := svc.CreateMembershipOrder(invitee, CreateMembershipOrderRequest{PlanID: plan.ID}, "referral-missing-rule-order")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.AdminConfirmMembershipOrder(admin, order.ID, ConfirmMembershipOrderRequest{
		ProviderTradeNo: "missing-rule-trade", Note: "缺失规则必须失败",
	})
	if err == nil || err.Error() != "该个人会员套餐缺少邀请奖励规则，订单暂不能履约" {
		t.Fatalf("confirmation error = %v", err)
	}
	var storedOrder model.MembershipOrder
	if err := db.First(&storedOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedOrder.Status != model.MembershipOrderPending {
		t.Fatalf("order status = %s, want pending", storedOrder.Status)
	}
}

func assertCreditBalance(t *testing.T, db *gorm.DB, userID string, expected int64) {
	t.Helper()
	var account model.CreditAccount
	if err := db.First(&account, "user_id = ?", userID).Error; err != nil {
		t.Fatal(err)
	}
	if account.AvailableMicrocredits != expected {
		t.Fatalf("user %s credit balance = %d, want %d", userID, account.AvailableMicrocredits, expected)
	}
}
