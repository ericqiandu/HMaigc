package service

import (
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
	inviter := &model.User{ID: "referral-inviter", Username: "inviter", Role: model.UserRoleUser, Status: model.UserStatusActive}
	invitee := &model.User{ID: "referral-invitee", Username: "invitee", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create([]*model.User{inviter, invitee}).Error; err != nil {
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
	firstPayment, err := payMembershipOrderForTest(t, svc, db, firstOrder, "referral-first-trade")
	if err != nil {
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

	if err := replayMembershipPaymentForTest(svc, firstPayment); err != nil {
		t.Fatalf("same verified first-purchase payment replay: %v", err)
	}

	secondOrder, err := svc.CreateMembershipOrder(invitee, CreateMembershipOrderRequest{PlanID: plan.ID}, "referral-renewal-order")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := payMembershipOrderForTest(t, svc, db, secondOrder, "referral-renewal-trade"); err != nil {
		t.Fatal(err)
	}
	assertCreditBalance(t, db, inviter.ID, rule.InviterRewardMicrocredits)
	// Each paid membership order grants its frozen periodic credits immediately;
	// the invitation reward remains a first-purchase fact and is not duplicated.
	assertCreditBalance(t, db, invitee.ID, plan.CreditsPerPeriod*2+rule.InviteeRewardMicrocredits)
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
	inviter := &model.User{ID: "referral-missing-inviter", Username: "missing-inviter", Role: model.UserRoleUser, Status: model.UserStatusActive}
	invitee := &model.User{ID: "referral-missing-invitee", Username: "missing-invitee", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create([]*model.User{inviter, invitee}).Error; err != nil {
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
	_, err = payMembershipOrderForTest(t, svc, db, order, "missing-rule-trade")
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
