package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newMembershipTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.User{},
		&model.MembershipPlan{},
		&model.MembershipOrder{},
		&model.MembershipSubscription{},
		&model.Team{},
		&model.TeamMember{},
		&model.TeamInvitation{},
		&model.TeamAuditEvent{},
		&model.CreditAccount{},
		&model.CreditLedgerEntry{},
		&model.AdminAuditEvent{},
		&model.SystemSetting{},
		&model.PaymentCheckoutSession{},
		&model.PaymentTransaction{},
		&model.PaymentWebhookEvent{},
		&model.Task{},
	); err != nil {
		t.Fatal(err)
	}
	return &Service{repo: repository.New(db), dataDir: t.TempDir()}, db
}

func membershipPlanByCode(t *testing.T, db *gorm.DB, code string) model.MembershipPlan {
	t.Helper()
	var plan model.MembershipPlan
	if err := db.First(&plan, "code = ?", code).Error; err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestDefaultMembershipPlansExposeExpectedCommercialTiers(t *testing.T) {
	svc, _ := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	plans, err := svc.MembershipPlans(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 10 {
		t.Fatalf("plan count = %d, want 10", len(plans))
	}
	expectedConcurrency := map[string][2]int{
		"origin-free":     {4, 2},
		"pro-month":       {6, 4},
		"max-month":       {8, 6},
		"ultra-month":     {12, 8},
		"team-pro-year":   {6, 4},
		"team-max-year":   {8, 6},
		"team-ultra-year": {12, 8},
	}
	for _, plan := range plans {
		expected, ok := expectedConcurrency[plan.Code]
		if !ok {
			continue
		}
		if plan.ImageConcurrency != expected[0] || plan.VideoConcurrency != expected[1] {
			t.Fatalf("%s concurrency = %d/%d, want %d/%d", plan.Code, plan.ImageConcurrency, plan.VideoConcurrency, expected[0], expected[1])
		}
		if len(plan.Benefits) == 0 {
			t.Fatalf("%s has no benefits", plan.Code)
		}
		delete(expectedConcurrency, plan.Code)
	}
	if len(expectedConcurrency) != 0 {
		t.Fatalf("missing expected plans: %#v", expectedConcurrency)
	}
}

func TestEnsureDefaultMembershipPlansIsIdempotent(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}

	var count int64
	if err := db.Model(&model.MembershipPlan{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 10 {
		t.Fatalf("plan count after repeated initialization = %d, want 10", count)
	}
}

func TestMembershipOrderConfirmationGrantsSubscriptionAndCreditsOnce(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	admin := &model.User{ID: "admin-1", Username: "admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	user := &model.User{ID: "user-1", Username: "alice", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(admin).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "pro-month")
	order, err := svc.CreateMembershipOrder(user, CreateMembershipOrderRequest{PlanID: plan.ID})
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := svc.AdminConfirmMembershipOrder(admin, order.ID, ConfirmMembershipOrderRequest{
		ProviderTradeNo: "test-trade-001",
		Note:            "自动化测试确认",
	})
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Status != model.MembershipOrderPaid {
		t.Fatalf("order status = %s, want paid", confirmed.Status)
	}
	entitlement, err := svc.MembershipEntitlement(user)
	if err != nil {
		t.Fatal(err)
	}
	if entitlement.Tier != "pro" || entitlement.ImageConcurrency != 6 || entitlement.VideoConcurrency != 4 {
		t.Fatalf("unexpected entitlement: %#v", entitlement)
	}
	var account model.CreditAccount
	if err := db.First(&account, "user_id = ?", user.ID).Error; err != nil {
		t.Fatal(err)
	}
	if account.AvailableMicrocredits != plan.CreditsPerPeriod {
		t.Fatalf("available credits = %d, want %d", account.AvailableMicrocredits, plan.CreditsPerPeriod)
	}
	var ledgerCountBeforeDuplicate int64
	if err := db.Model(&model.CreditLedgerEntry{}).Where("user_id = ?", user.ID).Count(&ledgerCountBeforeDuplicate).Error; err != nil {
		t.Fatal(err)
	}
	if ledgerCountBeforeDuplicate != 1 {
		t.Fatalf("credit ledger count before duplicate confirmation = %d, want 1", ledgerCountBeforeDuplicate)
	}
	_, err = svc.AdminConfirmMembershipOrder(admin, order.ID, ConfirmMembershipOrderRequest{
		ProviderTradeNo: "test-trade-002",
		Note:            "重复确认必须失败",
	})
	var authErr *AuthError
	if !errors.As(err, &authErr) || authErr.Status != http.StatusConflict {
		t.Fatalf("duplicate confirmation error = %#v, want HTTP 409 AuthError", err)
	}
	var subscriptionCount int64
	if err := db.Model(&model.MembershipSubscription{}).Where("order_id = ?", order.ID).Count(&subscriptionCount).Error; err != nil {
		t.Fatal(err)
	}
	if subscriptionCount != 1 {
		t.Fatalf("subscription count = %d, want 1", subscriptionCount)
	}
	var accountAfterDuplicate model.CreditAccount
	if err := db.First(&accountAfterDuplicate, "user_id = ?", user.ID).Error; err != nil {
		t.Fatal(err)
	}
	if accountAfterDuplicate.AvailableMicrocredits != plan.CreditsPerPeriod {
		t.Fatalf("available credits after duplicate confirmation = %d, want %d", accountAfterDuplicate.AvailableMicrocredits, plan.CreditsPerPeriod)
	}
	var ledgerCountAfterDuplicate int64
	if err := db.Model(&model.CreditLedgerEntry{}).Where("user_id = ?", user.ID).Count(&ledgerCountAfterDuplicate).Error; err != nil {
		t.Fatal(err)
	}
	if ledgerCountAfterDuplicate != ledgerCountBeforeDuplicate {
		t.Fatalf("credit ledger count after duplicate confirmation = %d, want %d", ledgerCountAfterDuplicate, ledgerCountBeforeDuplicate)
	}
}

func TestMembershipEntitlementUsesPurchasedPlanSnapshot(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	admin := &model.User{ID: "admin-1", Username: "admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	user := &model.User{ID: "user-1", Username: "alice", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(admin).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "pro-month")
	order, err := svc.CreateMembershipOrder(user, CreateMembershipOrderRequest{PlanID: plan.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AdminConfirmMembershipOrder(admin, order.ID, ConfirmMembershipOrderRequest{
		ProviderTradeNo: "snapshot-trade-001",
		Note:            "确认购买快照",
	}); err != nil {
		t.Fatal(err)
	}
	plan.Name = "已修改的新套餐名称"
	plan.Tier = "changed"
	plan.ImageConcurrency = 99
	plan.VideoConcurrency = 88
	plan.TopupDiscountBasisPoints = 5000
	if err := db.Save(&plan).Error; err != nil {
		t.Fatal(err)
	}
	entitlement, err := svc.MembershipEntitlement(user)
	if err != nil {
		t.Fatal(err)
	}
	if entitlement.PlanName != "Pro" || entitlement.Tier != "pro" {
		t.Fatalf("entitlement identity = %s/%s, want purchased Pro/pro snapshot", entitlement.PlanName, entitlement.Tier)
	}
	if entitlement.ImageConcurrency != 6 || entitlement.VideoConcurrency != 4 || entitlement.TopupDiscountBasis != 8000 {
		t.Fatalf("entitlement mutated with current plan: %#v", entitlement)
	}
}

func TestMembershipConfirmationUsesOrderSnapshotAfterPlanChanges(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	admin := &model.User{ID: "admin-snapshot", Username: "admin-snapshot", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	user := &model.User{ID: "user-snapshot", Username: "snapshot-buyer", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(admin).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "pro-month")
	order, err := svc.CreateMembershipOrder(user, CreateMembershipOrderRequest{PlanID: plan.ID})
	if err != nil {
		t.Fatal(err)
	}
	purchasedCredits := plan.CreditsPerPeriod
	plan.CreditsPerPeriod = purchasedCredits + 9000000
	plan.ImageConcurrency = 99
	plan.VideoConcurrency = 88
	if err := db.Save(&plan).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AdminConfirmMembershipOrder(admin, order.ID, ConfirmMembershipOrderRequest{
		ProviderTradeNo: "snapshot-before-confirm-001",
		Note:            "验证订单快照开通",
	}); err != nil {
		t.Fatal(err)
	}
	entitlement, err := svc.MembershipEntitlement(user)
	if err != nil {
		t.Fatal(err)
	}
	if entitlement.ImageConcurrency != 6 || entitlement.VideoConcurrency != 4 {
		t.Fatalf("confirmed entitlement used mutable plan: %#v", entitlement)
	}
	var account model.CreditAccount
	if err := db.First(&account, "user_id = ?", user.ID).Error; err != nil {
		t.Fatal(err)
	}
	if account.AvailableMicrocredits != purchasedCredits {
		t.Fatalf("granted credits = %d, want order snapshot credits %d", account.AvailableMicrocredits, purchasedCredits)
	}
}

func TestMembershipEntitlementRejectsInvalidSubscriptionSnapshot(t *testing.T) {
	svc, db := newMembershipTestService(t)
	user := &model.User{ID: "user-1", Username: "alice", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	subscription := &model.MembershipSubscription{
		ID:               "subscription-1",
		UserID:           user.ID,
		PlanID:           "plan-1",
		OrderID:          "order-1",
		Status:           model.MembershipSubscriptionActive,
		Seats:            1,
		PlanSnapshotJSON: "{broken",
		StartsAt:         now.Add(-time.Hour),
		EndsAt:           timePointer(now.Add(time.Hour)),
	}
	if err := db.Create(subscription).Error; err != nil {
		t.Fatal(err)
	}
	_, err := svc.MembershipEntitlement(user)
	if err == nil || !strings.Contains(err.Error(), "套餐快照损坏") {
		t.Fatalf("invalid snapshot error = %v, want explicit snapshot corruption error", err)
	}
}

func TestTeamMemberReceivesTeamSubscriptionSnapshotEntitlement(t *testing.T) {
	svc, db := newMembershipTestService(t)
	owner := &model.User{ID: "owner-1", Username: "owner", Role: model.UserRoleUser, Status: model.UserStatusActive}
	member := &model.User{ID: "member-1", Username: "member", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create([]*model.User{owner, member}).Error; err != nil {
		t.Fatal(err)
	}
	team, err := svc.CreateTeam(owner, CreateTeamRequest{Name: "商业创作团队"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	teamMember := &model.TeamMember{ID: "team-member-1", TeamID: team.ID, UserID: member.ID, Role: model.TeamMemberRoleMember, Status: model.TeamMemberStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(teamMember).Error; err != nil {
		t.Fatal(err)
	}
	snapshot := model.MembershipPlan{
		ID:                       "team-plan-1",
		Name:                     "团队 Max",
		Tier:                     "max",
		Audience:                 model.MembershipAudienceTeam,
		ImageConcurrency:         8,
		VideoConcurrency:         6,
		TopupDiscountBasisPoints: 8500,
	}
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	subscription := &model.MembershipSubscription{
		ID:               "team-subscription-1",
		UserID:           owner.ID,
		TeamID:           team.ID,
		PlanID:           snapshot.ID,
		OrderID:          "team-order-1",
		Status:           model.MembershipSubscriptionActive,
		Seats:            2,
		PlanSnapshotJSON: string(snapshotJSON),
		StartsAt:         now.Add(-time.Hour),
		EndsAt:           timePointer(now.Add(time.Hour)),
	}
	if err := db.Create(subscription).Error; err != nil {
		t.Fatal(err)
	}
	entitlement, err := svc.MembershipEntitlement(member)
	if err != nil {
		t.Fatal(err)
	}
	if entitlement.TeamID != team.ID || entitlement.PlanName != "团队 Max" || entitlement.ImageConcurrency != 8 || entitlement.VideoConcurrency != 6 {
		t.Fatalf("unexpected team entitlement: %#v", entitlement)
	}
}

func timePointer(value time.Time) *time.Time {
	return &value
}

func TestTeamMembershipRequiresOwnedTeamAndValidSeatRange(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	user := &model.User{ID: "user-1", Username: "owner", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "team-pro-year")
	if _, err := svc.CreateMembershipOrder(user, CreateMembershipOrderRequest{PlanID: plan.ID, Seats: 1}); err == nil {
		t.Fatal("team order with too few seats unexpectedly succeeded")
	}
	team, err := svc.CreateTeam(user, CreateTeamRequest{Name: "短剧制作团队"})
	if err != nil {
		t.Fatal(err)
	}
	order, err := svc.CreateMembershipOrder(user, CreateMembershipOrderRequest{PlanID: plan.ID, TeamID: team.ID, Seats: 3})
	if err != nil {
		t.Fatal(err)
	}
	if order.TeamID != team.ID || order.Seats != 3 || order.TotalPriceCents != plan.PriceCents*3 {
		t.Fatalf("unexpected team order: %#v", order)
	}
}
