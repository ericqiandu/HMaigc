package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
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
		&model.TaskLog{},
	); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsurePaymentIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureTeamIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	return &Service{repo: repository.New(db), dataDir: t.TempDir()}, db
}

func TestMembershipOrderIdempotencyRequiresBoundedKey(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	user := &model.User{ID: "idempotency-key-user", Username: "idempotency-key-user", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "pro-month")
	for name, key := range map[string]string{"missing": "", "oversize": strings.Repeat("k", 121)} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.CreateMembershipOrder(user, CreateMembershipOrderRequest{PlanID: plan.ID}, key)
			var authErr *AuthError
			if !errors.As(err, &authErr) || authErr.Status != http.StatusBadRequest {
				t.Fatalf("CreateMembershipOrder key error = %#v, want HTTP 400", err)
			}
		})
	}
}

func TestMembershipOrderIdempotencyReplaysWinnerAndRejectsChangedPurchase(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	firstUser := &model.User{ID: "idempotency-user-a", Username: "idempotency-user-a", Role: model.UserRoleUser, Status: model.UserStatusActive}
	secondUser := &model.User{ID: "idempotency-user-b", Username: "idempotency-user-b", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(&[]model.User{*firstUser, *secondUser}).Error; err != nil {
		t.Fatal(err)
	}
	pro := membershipPlanByCode(t, db, "pro-month")
	max := membershipPlanByCode(t, db, "max-month")

	first, err := svc.CreateMembershipOrder(firstUser, CreateMembershipOrderRequest{PlanID: pro.ID}, "same-request")
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := svc.CreateMembershipOrder(firstUser, CreateMembershipOrderRequest{PlanID: pro.ID}, "same-request")
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != first.ID || replayed.RequestHash != first.RequestHash || len(first.RequestHash) != 64 {
		t.Fatalf("same request did not replay the winning order: first=%#v replay=%#v", first, replayed)
	}
	var sameRequestCount int64
	if err := db.Model(&model.MembershipOrder{}).Where("user_id = ? AND idempotency_key = ?", firstUser.ID, "same-request").Count(&sameRequestCount).Error; err != nil || sameRequestCount != 1 {
		t.Fatalf("same request row count = %d, err=%v", sameRequestCount, err)
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "same-request") || strings.Contains(string(encoded), first.RequestHash) || strings.Contains(string(encoded), "idempotencyKey") || strings.Contains(string(encoded), "requestHash") {
		t.Fatalf("write-only idempotency facts leaked through JSON: %s", encoded)
	}

	_, err = svc.CreateMembershipOrder(firstUser, CreateMembershipOrderRequest{PlanID: max.ID}, "same-request")
	assertMembershipOrderConflict(t, err)

	otherUserOrder, err := svc.CreateMembershipOrder(secondUser, CreateMembershipOrderRequest{PlanID: pro.ID}, "same-request")
	if err != nil {
		t.Fatal(err)
	}
	if otherUserOrder.ID == first.ID {
		t.Fatal("cross-user idempotency key reuse returned another user's order")
	}

	teamPlan := membershipPlanByCode(t, db, "team-pro-year")
	teamA := model.Team{ID: "idempotency-team-a", OwnerUserID: firstUser.ID, Name: "A", Status: model.TeamStatusActive}
	teamB := model.Team{ID: "idempotency-team-b", OwnerUserID: firstUser.ID, Name: "B", Status: model.TeamStatusActive}
	if err := db.Create(&[]model.Team{teamA, teamB}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateMembershipOrder(firstUser, CreateMembershipOrderRequest{PlanID: teamPlan.ID, TeamID: teamA.ID, Seats: 2}, "team-change"); err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateMembershipOrder(firstUser, CreateMembershipOrderRequest{PlanID: teamPlan.ID, TeamID: teamB.ID, Seats: 2}, "team-change")
	assertMembershipOrderConflict(t, err)
	if _, err := svc.CreateMembershipOrder(firstUser, CreateMembershipOrderRequest{PlanID: teamPlan.ID, TeamID: teamA.ID, Seats: 2}, "seat-change"); err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateMembershipOrder(firstUser, CreateMembershipOrderRequest{PlanID: teamPlan.ID, TeamID: teamA.ID, Seats: 3}, "seat-change")
	assertMembershipOrderConflict(t, err)
}

func TestMembershipOrderIdempotencyReplaysAfterPlanUpdateAndArchive(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	user := &model.User{ID: "idempotency-archive-user", Username: "idempotency-archive-user", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "pro-year")
	request := CreateMembershipOrderRequest{PlanID: plan.ID}
	created, err := svc.CreateMembershipOrder(user, request, "archive-replay")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.MembershipPlan{}).Where("id = ?", plan.ID).
		Select("price_cents", "enabled").Updates(model.MembershipPlan{PriceCents: plan.PriceCents + 100, Enabled: false}).Error; err != nil {
		t.Fatal(err)
	}
	replayed, err := svc.CreateMembershipOrder(user, request, "archive-replay")
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != created.ID || replayed.UnitPriceCents != plan.PriceCents || replayed.PlanSnapshotJSON != created.PlanSnapshotJSON {
		t.Fatalf("archived plan replay changed immutable order facts: created=%#v replayed=%#v", created, replayed)
	}
}

func TestMembershipOrderIdempotencyRejectsNonPositivePaidPlanPrices(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	admin := &model.User{ID: "price-admin", Username: "price-admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	user := &model.User{ID: "price-user", Username: "price-user", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(&[]model.User{*admin, *user}).Error; err != nil {
		t.Fatal(err)
	}
	for _, planCode := range []string{"pro-month", "pro-year"} {
		plan := membershipPlanByCode(t, db, planCode)
		request := UpdateMembershipPlanRequest{
			Name: plan.Name, OriginalPriceCents: plan.OriginalPriceCents, CreditsPerPeriod: plan.CreditsPerPeriod,
			ImageConcurrency: plan.ImageConcurrency, VideoConcurrency: plan.VideoConcurrency,
			TopupDiscountBasisPoints: plan.TopupDiscountBasisPoints, Benefits: []string{"测试权益"}, Enabled: true, SortOrder: plan.SortOrder,
		}
		for _, price := range []int64{0, -1} {
			request.PriceCents = price
			if _, err := svc.AdminUpdateMembershipPlan(admin, plan.ID, request); err == nil {
				t.Fatalf("admin update accepted %s plan price %d", plan.BillingCycle, price)
			}
			if err := db.Model(&model.MembershipPlan{}).Where("id = ?", plan.ID).Update("price_cents", price).Error; err != nil {
				t.Fatal(err)
			}
			key := fmt.Sprintf("invalid-%s-price-%d", plan.BillingCycle, price)
			if _, err := svc.CreateMembershipOrder(user, CreateMembershipOrderRequest{PlanID: plan.ID}, key); err == nil {
				t.Fatalf("order creation accepted %s plan price %d", plan.BillingCycle, price)
			}
			if err := svc.EnsureDefaultMembershipPlans(); err == nil {
				t.Fatalf("startup catalog validation accepted %s plan price %d", plan.BillingCycle, price)
			}
			if err := db.Model(&model.MembershipPlan{}).Where("id = ?", plan.ID).Update("price_cents", plan.PriceCents).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestMembershipCatalogStartupRejectsInvalidExistingPlanBeforeStaleRevisionWrite(t *testing.T) {
	svc, db := newMembershipTestService(t)
	invalidPlan := model.MembershipPlan{
		ID: "stale-revision-plan", Code: "pro-year", Name: "非法旧年付套餐", Tier: "pro",
		Audience: model.MembershipAudiencePersonal, BillingCycle: model.MembershipBillingCycleYear,
		PriceCents: 0, Currency: "CNY", ImageConcurrency: 1, VideoConcurrency: 1, Enabled: true,
	}
	staleRevision := model.SystemSetting{Key: membershipCatalogRevisionSettingKey, ValueJSON: `"stale-catalog-revision"`}
	if err := db.Create(&invalidPlan).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&staleRevision).Error; err != nil {
		t.Fatal(err)
	}

	err := svc.EnsureDefaultMembershipPlans()
	if err == nil || !strings.Contains(err.Error(), invalidPlan.Code) {
		t.Fatalf("startup stale-revision price validation error = %v", err)
	}
	stored := membershipPlanByCode(t, db, invalidPlan.Code)
	if stored.PriceCents != 0 || stored.Name != invalidPlan.Name {
		t.Fatalf("catalog write overwrote invalid existing plan before validation: %#v", stored)
	}
	var revision model.SystemSetting
	if err := db.First(&revision, "key = ?", membershipCatalogRevisionSettingKey).Error; err != nil {
		t.Fatal(err)
	}
	if revision.ValueJSON != staleRevision.ValueJSON {
		t.Fatalf("catalog revision advanced before validation: %q", revision.ValueJSON)
	}
}

func TestMembershipCatalogStartupRejectsNonCatalogPaidPlans(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		billingCycle model.MembershipBillingCycle
		priceCents   int64
	}{
		{name: "month-zero", billingCycle: model.MembershipBillingCycleMonth, priceCents: 0},
		{name: "year-negative", billingCycle: model.MembershipBillingCycleYear, priceCents: -1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			svc, db := newMembershipTestService(t)
			if err := svc.EnsureDefaultMembershipPlans(); err != nil {
				t.Fatal(err)
			}
			plan := model.MembershipPlan{
				ID: "non-catalog-" + testCase.name, Code: "non-catalog-" + testCase.name, Name: "非目录付费套餐", Tier: "legacy",
				Audience: model.MembershipAudiencePersonal, BillingCycle: testCase.billingCycle,
				PriceCents: testCase.priceCents, Currency: "CNY", ImageConcurrency: 1, VideoConcurrency: 1, Enabled: false,
			}
			if err := db.Create(&plan).Error; err != nil {
				t.Fatal(err)
			}

			err := svc.EnsureDefaultMembershipPlans()
			if err == nil || !strings.Contains(err.Error(), plan.Code) {
				t.Fatalf("startup non-catalog price validation error = %v", err)
			}
		})
	}
}

func TestMembershipCatalogStartupValidatesFinalNonCatalogPlansAfterRevisionWrite(t *testing.T) {
	svc, db := newMembershipTestService(t)
	plan := model.MembershipPlan{
		ID: "final-non-catalog-year", Code: "final-non-catalog-year", Name: "待归档年付套餐", Tier: "legacy",
		Audience: model.MembershipAudiencePersonal, BillingCycle: model.MembershipBillingCycleYear,
		PriceCents: 1, Currency: "CNY", ImageConcurrency: 1, VideoConcurrency: 1, Enabled: true,
	}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`
		CREATE TRIGGER invalidate_archived_non_catalog_price
		AFTER UPDATE OF enabled ON membership_plans
		WHEN NEW.code = 'final-non-catalog-year'
		BEGIN
			UPDATE membership_plans SET price_cents = 0 WHERE id = NEW.id;
		END`).Error; err != nil {
		t.Fatal(err)
	}

	err := svc.EnsureDefaultMembershipPlans()
	if err == nil || !strings.Contains(err.Error(), plan.Code) {
		t.Fatalf("startup final catalog price validation error = %v", err)
	}
}

func assertMembershipOrderConflict(t *testing.T, err error) {
	t.Helper()
	var authErr *AuthError
	if !errors.As(err, &authErr) || authErr.Status != http.StatusConflict {
		t.Fatalf("idempotency conflict error = %#v, want HTTP 409", err)
	}
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
		PriceCents: 1, Currency: "CNY", ImageConcurrency: 1, VideoConcurrency: 1, Enabled: true,
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

func TestMembershipOrderPaymentGrantsSubscriptionAndCreditsOnce(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	user := &model.User{ID: "user-1", Username: "alice", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "pro-month")
	order, err := svc.CreateMembershipOrder(user, CreateMembershipOrderRequest{PlanID: plan.ID}, "confirmation-grant-order")
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := payMembershipOrderForTest(t, svc, db, order, "test-trade-001")
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := svc.repo.MembershipOrder(order.ID)
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
	if err := replayMembershipPaymentForTest(svc, fixture); err != nil {
		t.Fatalf("same verified payment replay: %v", err)
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
	var processedEventCount int64
	if err := db.Model(&model.PaymentWebhookEvent{}).Where("transaction_id = ? AND status = ?", fixture.Transaction.ID, model.PaymentWebhookProcessed).Count(&processedEventCount).Error; err != nil {
		t.Fatal(err)
	}
	if processedEventCount != 1 {
		t.Fatalf("processed payment event count = %d, want 1", processedEventCount)
	}
}

func TestPaymentMembershipCancellationAndLifecyclePreservePayableTransaction(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	user := &model.User{ID: "payable-cancel-user", Username: "payable-cancel-user", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "pro-month")
	order, err := svc.CreateMembershipOrder(user, CreateMembershipOrderRequest{PlanID: plan.ID}, "payable-cancel-order")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	transaction := &model.PaymentTransaction{
		ID: "payable-cancel-transaction", OrderType: model.PaymentOrderMembership, OrderID: order.ID, UserID: user.ID,
		Provider: model.PaymentProviderWechat, MerchantOrderNo: "MPAYABLECANCEL", AmountCents: order.TotalPriceCents,
		Currency: order.Currency, Status: model.PaymentTransactionReviewRequired, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(transaction).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CancelMembershipOrder(user, order.ID); err == nil {
		t.Fatal("payable membership order was cancelled without provider reconciliation")
	}
	if err := db.Model(&model.MembershipOrder{}).Where("id = ?", order.ID).Update("created_at", now.Add(-48*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.reconcileMembershipLifecycle(now); err != nil {
		t.Fatal(err)
	}
	var persistedOrder model.MembershipOrder
	if err := db.First(&persistedOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persistedOrder.Status != model.MembershipOrderPending {
		t.Fatalf("payable membership order status = %s, want pending", persistedOrder.Status)
	}
	var persistedTransaction model.PaymentTransaction
	if err := db.First(&persistedTransaction, "id = ?", transaction.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persistedTransaction.Status != model.PaymentTransactionReviewRequired {
		t.Fatalf("payable transaction status = %s, want review_required", persistedTransaction.Status)
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
	order, err := svc.CreateMembershipOrder(user, CreateMembershipOrderRequest{PlanID: plan.ID}, "snapshot-immutability-order")
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
	user := &model.User{ID: "user-1", Username: "alice", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "pro-month")
	order, err := svc.CreateMembershipOrder(user, CreateMembershipOrderRequest{PlanID: plan.ID}, "snapshot-corruption-order")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := payMembershipOrderForTest(t, svc, db, order, "snapshot-trade-001"); err != nil {
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

func TestMembershipPaymentUsesOrderSnapshotAfterPlanChanges(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	user := &model.User{ID: "user-snapshot", Username: "snapshot-buyer", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "pro-month")
	order, err := svc.CreateMembershipOrder(user, CreateMembershipOrderRequest{PlanID: plan.ID}, "invoice-membership-order")
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
	if _, err := payMembershipOrderForTest(t, svc, db, order, "snapshot-before-confirm-001"); err != nil {
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
	team, err := svc.CreateTeam(owner, teamCreateRequest("商业创作团队"))
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
	if _, err := svc.CreateMembershipOrder(user, CreateMembershipOrderRequest{PlanID: plan.ID, Seats: 1}, "team-missing-id-order"); err == nil {
		t.Fatal("team order with too few seats unexpectedly succeeded")
	}
	team, err := svc.CreateTeam(user, teamCreateRequest("短剧制作团队"))
	if err != nil {
		t.Fatal(err)
	}
	order, err := svc.CreateMembershipOrder(user, CreateMembershipOrderRequest{PlanID: plan.ID, TeamID: team.ID, Seats: 3}, "team-seat-bounds-order")
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
