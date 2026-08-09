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
		&model.UserIdentity{},
		&model.ReferralProfile{},
		&model.ReferralRelationship{},
		&model.ReferralRewardRule{},
		&model.ReferralReward{},
		&model.MembershipPlan{},
		&model.MembershipOrder{},
		&model.MembershipSubscription{},
		&model.Team{},
		&model.TeamMember{},
		&model.TeamInvitation{},
		&model.TeamAuditEvent{},
		&model.CanvasProject{},
		&model.CanvasCollaborator{},
		&model.CanvasChange{},
		&model.CanvasShare{},
		&model.Resource{},
		&model.CreditAccount{},
		&model.CreditLedgerEntry{},
		&model.TeamCreditAccount{},
		&model.TeamCreditLedgerEntry{},
		&model.BillingOrder{},
		&model.InvoiceRequest{},
		&model.Project{},
		&model.ProjectCollaborator{},
		&model.AdminAuditEvent{},
		&model.SystemSetting{},
		&model.UserOSSSetting{},
		&model.UserDailyUploadUsage{},
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
	expectedNames := map[string]string{
		"origin-free": "基础版",
		"pro-month":   "标准版", "pro-year": "标准版",
		"max-month": "高级版", "max-year": "高级版",
		"ultra-month": "至尊版", "ultra-year": "至尊版",
		"team-pro-year": "标准版", "team-max-year": "高级版", "team-ultra-year": "至尊版",
	}
	for _, plan := range plans {
		if plan.Name != expectedNames[plan.Code] {
			t.Fatalf("%s name = %q, want %q", plan.Code, plan.Name, expectedNames[plan.Code])
		}
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

func TestMembershipCatalogRevisionReplacesLegacyConfigurationOnce(t *testing.T) {
	svc, db := newMembershipTestService(t)
	legacyPlan := model.MembershipPlan{
		ID: "legacy-plan", Code: "legacy-vip", Name: "旧后台套餐", Tier: "legacy",
		Audience: model.MembershipAudiencePersonal, BillingCycle: model.MembershipBillingCycleMonth,
		Currency: "CNY", ImageConcurrency: 1, VideoConcurrency: 1, Enabled: true,
	}
	customizedStandard := model.MembershipPlan{
		ID: "stable-pro-year", Code: "pro-year", Name: "旧标准套餐", Tier: "pro",
		Audience: model.MembershipAudiencePersonal, BillingCycle: model.MembershipBillingCycleYear,
		PriceCents: 1, Currency: "CNY", ImageConcurrency: 1, VideoConcurrency: 1, Enabled: false,
	}
	if err := db.Create(&[]model.MembershipPlan{legacyPlan, customizedStandard}).Error; err != nil {
		t.Fatal(err)
	}

	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	replaced := membershipPlanByCode(t, db, "pro-year")
	if replaced.ID != customizedStandard.ID || replaced.Name != "标准版" || replaced.PriceCents != 94900 || !replaced.Enabled {
		t.Fatalf("canonical plan was not replaced in place: %#v", replaced)
	}
	archived := membershipPlanByCode(t, db, legacyPlan.Code)
	if archived.Enabled {
		t.Fatalf("legacy plan remained sellable: %#v", archived)
	}
	adminPlans, err := svc.MembershipPlans(&model.User{ID: "admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if len(adminPlans) != 10 {
		t.Fatalf("admin catalog count = %d, want 10", len(adminPlans))
	}

	if err := db.Model(&model.MembershipPlan{}).Where("code = ?", "pro-year").Update("name", "运营自定义标准版").Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	preserved := membershipPlanByCode(t, db, "pro-year")
	if preserved.Name != "运营自定义标准版" {
		t.Fatalf("completed catalog revision overwrote later admin configuration: %#v", preserved)
	}
}

func TestMembershipEntitlementMarksOriginUserAsNonMember(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	user := &model.User{ID: "user-origin", Username: "origin", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}

	entitlement, err := svc.MembershipEntitlement(user)
	if err != nil {
		t.Fatal(err)
	}
	if entitlement.IsActiveMember {
		t.Fatalf("Origin user was marked as an active member: %#v", entitlement)
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
	if !entitlement.IsActiveMember {
		t.Fatalf("paid subscription was not marked as an active member: %#v", entitlement)
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
	var auditCount int64
	if err := db.Model(&model.AdminAuditEvent{}).Where("action = ? AND target_id = ?", "membership_order.confirm", order.ID).Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("membership confirmation audit count = %d, want 1", auditCount)
	}
}

func TestMembershipOrderConfirmationRollsBackWhenAuditInsertFails(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	admin := &model.User{ID: "admin-audit-failure", Username: "admin-audit-failure", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	user := &model.User{ID: "user-audit-failure", Username: "user-audit-failure", Role: model.UserRoleUser, Status: model.UserStatusActive}
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
	if err := db.Exec(`
		CREATE TRIGGER reject_membership_confirmation_audit
		BEFORE INSERT ON admin_audit_events
		WHEN NEW.action = 'membership_order.confirm'
		BEGIN
			SELECT RAISE(ABORT, 'membership confirmation audit unavailable');
		END
	`).Error; err != nil {
		t.Fatal(err)
	}

	_, err = svc.AdminConfirmMembershipOrder(admin, order.ID, ConfirmMembershipOrderRequest{
		ProviderTradeNo: "audit-failure-trade",
		Note:            "审计写入失败时必须整体回滚",
	})
	if err == nil || !strings.Contains(err.Error(), "membership confirmation audit unavailable") {
		t.Fatalf("confirmation error = %v, want audit insertion failure", err)
	}

	var stored model.MembershipOrder
	if err := db.First(&stored, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.MembershipOrderPending || stored.PaidAt != nil {
		t.Fatalf("order changed despite audit failure: %#v", stored)
	}
	for name, table := range map[string]string{
		"subscriptions":   "membership_subscriptions",
		"credit ledgers":  "credit_ledger_entries",
		"credit accounts": "credit_accounts",
	} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d after rollback, want 0", name, count)
		}
	}
}

func TestMembershipOrderCloseRollsBackWhenAuditInsertFails(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	admin := &model.User{ID: "admin-close-audit-failure", Username: "admin-close-audit-failure", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	user := &model.User{ID: "user-close-audit-failure", Username: "user-close-audit-failure", Role: model.UserRoleUser, Status: model.UserStatusActive}
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
	if err := db.Exec(`
		CREATE TRIGGER reject_membership_close_audit
		BEFORE INSERT ON admin_audit_events
		WHEN NEW.action = 'membership_order.close'
		BEGIN
			SELECT RAISE(ABORT, 'membership close audit unavailable');
		END
	`).Error; err != nil {
		t.Fatal(err)
	}

	_, err = svc.AdminCloseMembershipOrder(admin, order.ID, CloseMembershipOrderRequest{Note: "审计写入失败时不得关闭订单"})
	if err == nil || !strings.Contains(err.Error(), "membership close audit unavailable") {
		t.Fatalf("close error = %v, want audit insertion failure", err)
	}

	var stored model.MembershipOrder
	if err := db.First(&stored, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.MembershipOrderPending || stored.ResolvedBy != "" || stored.ResolutionNote != "" {
		t.Fatalf("order changed despite close audit failure: %#v", stored)
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
	if entitlement.PlanName != "标准版" || entitlement.Tier != "pro" {
		t.Fatalf("entitlement identity = %s/%s, want purchased 标准版/pro snapshot", entitlement.PlanName, entitlement.Tier)
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

func TestMembershipPlanViewsDerivesTeamCommercialBenefitsFromStructuredEntitlements(t *testing.T) {
	plans := []model.MembershipPlan{{
		ID:                        "team-plan-structured",
		Code:                      "team-structured",
		Audience:                  model.MembershipAudienceTeam,
		BenefitsJSON:              `["旧的非结构化宣传文案"]`,
		UnlimitedTaskQueue:        true,
		TeamStorageBytes:          130 * (1 << 40),
		SharedAssetsEnabled:       true,
		ProjectPermissionsEnabled: true,
		InvoicingEnabled:          true,
		CommercialUseEnabled:      true,
	}}
	views, err := membershipPlanViews(plans)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(views[0].Benefits, "|")
	want := strings.Join([]string{
		"多人画布协作",
		"团队共享资产库",
		"团队任务不限排队（执行并发受模型渠道限制）",
		"团队席位管理",
		"积分用量管控",
		"项目权限管理",
		"发票申请与交付",
		"团队资产隔离",
		"云端存储空间 130 TB",
		"商业使用授权",
	}, "|")
	if got != want {
		t.Fatalf("team benefits = %q, want %q", got, want)
	}
}
