package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func membershipCheckoutOrder(t *testing.T, plan model.MembershipPlan, order model.MembershipOrder) *model.MembershipOrder {
	t.Helper()
	snapshot, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	order.PlanSnapshotJSON = string(snapshot)
	return &order
}

func TestMembershipCheckoutViewProjectsFrozenPersonalAndTeamFacts(t *testing.T) {
	tests := []struct {
		name  string
		plan  model.MembershipPlan
		order model.MembershipOrder
		want  MembershipCheckoutSummary
	}{
		{
			name: "personal preserves zero original price",
			plan: model.MembershipPlan{
				ID: "personal-plan", Code: "pro-month", Name: "专业版", Tier: "pro",
				Audience: model.MembershipAudiencePersonal, BillingCycle: model.MembershipBillingCycleMonth,
				PriceCents: 1200, OriginalPriceCents: 0, Currency: "CNY", CreditsPerPeriod: 300,
				ImageConcurrency: 1, VideoConcurrency: 1,
			},
			order: model.MembershipOrder{
				ID: "personal-order", PlanID: "personal-plan", Seats: 1,
				UnitPriceCents: 1200, TotalPriceCents: 1200, Currency: "CNY",
			},
			want: MembershipCheckoutSummary{
				Audience: model.MembershipAudiencePersonal, Code: "pro-month", Name: "专业版", Tier: "pro",
				BillingCycle: model.MembershipBillingCycleMonth, Seats: 1,
				ActualPriceCents: 1200, OriginalPriceCents: 0, CreditsPerPeriod: 300, TotalCreditsPerPeriod: 300,
			},
		},
		{
			name: "team multiplies immutable unit facts by seats",
			plan: model.MembershipPlan{
				ID: "team-plan", Code: "team-year", Name: "团队版", Tier: "team",
				Audience: model.MembershipAudienceTeam, BillingCycle: model.MembershipBillingCycleYear,
				PriceCents: 2500, OriginalPriceCents: 3500, Currency: "CNY", CreditsPerPeriod: 500,
				ImageConcurrency: 2, VideoConcurrency: 2, MinSeats: 2, MaxSeats: 20,
			},
			order: model.MembershipOrder{
				ID: "team-order", TeamID: "team-1", PlanID: "team-plan", Seats: 3,
				UnitPriceCents: 2500, TotalPriceCents: 7500, Currency: "CNY",
			},
			want: MembershipCheckoutSummary{
				Audience: model.MembershipAudienceTeam, Code: "team-year", Name: "团队版", Tier: "team",
				BillingCycle: model.MembershipBillingCycleYear, Seats: 3,
				ActualPriceCents: 7500, OriginalPriceCents: 10500, CreditsPerPeriod: 500, TotalCreditsPerPeriod: 1500,
			},
		},
		{
			name: "original price equal to actual remains unchanged",
			plan: model.MembershipPlan{
				ID: "equal-plan", Code: "equal", Name: "同价套餐", Tier: "pro",
				Audience: model.MembershipAudiencePersonal, BillingCycle: model.MembershipBillingCycleMonth,
				PriceCents: 800, OriginalPriceCents: 800, Currency: "CNY", CreditsPerPeriod: 80,
				ImageConcurrency: 1, VideoConcurrency: 1,
			},
			order: model.MembershipOrder{ID: "equal-order", PlanID: "equal-plan", Seats: 1, UnitPriceCents: 800, TotalPriceCents: 800, Currency: "CNY"},
			want: MembershipCheckoutSummary{
				Audience: model.MembershipAudiencePersonal, Code: "equal", Name: "同价套餐", Tier: "pro",
				BillingCycle: model.MembershipBillingCycleMonth, Seats: 1,
				ActualPriceCents: 800, OriginalPriceCents: 800, CreditsPerPeriod: 80, TotalCreditsPerPeriod: 80,
			},
		},
		{
			name: "original price below actual remains unchanged",
			plan: model.MembershipPlan{
				ID: "below-plan", Code: "below", Name: "历史原价套餐", Tier: "pro",
				Audience: model.MembershipAudiencePersonal, BillingCycle: model.MembershipBillingCycleMonth,
				PriceCents: 900, OriginalPriceCents: 700, Currency: "CNY", CreditsPerPeriod: 90,
				ImageConcurrency: 1, VideoConcurrency: 1,
			},
			order: model.MembershipOrder{ID: "below-order", PlanID: "below-plan", Seats: 1, UnitPriceCents: 900, TotalPriceCents: 900, Currency: "CNY"},
			want: MembershipCheckoutSummary{
				Audience: model.MembershipAudiencePersonal, Code: "below", Name: "历史原价套餐", Tier: "pro",
				BillingCycle: model.MembershipBillingCycleMonth, Seats: 1,
				ActualPriceCents: 900, OriginalPriceCents: 700, CreditsPerPeriod: 90, TotalCreditsPerPeriod: 90,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := membershipCheckoutSummaryFromOrder(membershipCheckoutOrder(t, test.plan, test.order))
			if err != nil {
				t.Fatal(err)
			}
			if *got != test.want {
				t.Fatalf("summary = %#v, want %#v", *got, test.want)
			}
		})
	}
}

func TestMembershipCheckoutViewRejectsInvalidFrozenFactsAndOverflow(t *testing.T) {
	validPlan := model.MembershipPlan{
		ID: "plan-1", Code: "pro", Name: "专业版", Tier: "pro",
		Audience: model.MembershipAudiencePersonal, BillingCycle: model.MembershipBillingCycleMonth,
		PriceCents: 1200, OriginalPriceCents: 1800, Currency: "CNY", CreditsPerPeriod: 300,
		ImageConcurrency: 1, VideoConcurrency: 1,
	}
	validOrder := model.MembershipOrder{
		ID: "order-1", PlanID: validPlan.ID, Seats: 1,
		UnitPriceCents: 1200, TotalPriceCents: 1200, Currency: "CNY",
	}
	valid := membershipCheckoutOrder(t, validPlan, validOrder)

	tests := []struct {
		name   string
		mutate func(*model.MembershipOrder)
	}{
		{name: "missing snapshot", mutate: func(order *model.MembershipOrder) { order.PlanSnapshotJSON = "" }},
		{name: "corrupted snapshot", mutate: func(order *model.MembershipOrder) { order.PlanSnapshotJSON = "{" }},
		{name: "plan id mismatch", mutate: func(order *model.MembershipOrder) { order.PlanID = "other-plan" }},
		{name: "audience mismatch", mutate: func(order *model.MembershipOrder) { order.TeamID = "team-1" }},
		{name: "currency mismatch", mutate: func(order *model.MembershipOrder) { order.Currency = "USD" }},
		{name: "unit price mismatch", mutate: func(order *model.MembershipOrder) { order.UnitPriceCents++ }},
		{name: "total price mismatch", mutate: func(order *model.MembershipOrder) { order.TotalPriceCents++ }},
		{name: "actual price multiplication overflow", mutate: func(order *model.MembershipOrder) {
			order.Seats = 2
			order.TeamID = "team-1"
			plan := validPlan
			plan.Audience = model.MembershipAudienceTeam
			plan.PriceCents = math.MaxInt64
			plan.OriginalPriceCents = 1
			order.UnitPriceCents = math.MaxInt64
			order.TotalPriceCents = math.MaxInt64
			order.PlanSnapshotJSON = membershipCheckoutOrder(t, plan, *order).PlanSnapshotJSON
		}},
		{name: "original price multiplication overflow", mutate: func(order *model.MembershipOrder) {
			order.Seats = 2
			order.TeamID = "team-1"
			order.TotalPriceCents = 2400
			plan := validPlan
			plan.Audience = model.MembershipAudienceTeam
			plan.OriginalPriceCents = math.MaxInt64
			order.PlanSnapshotJSON = membershipCheckoutOrder(t, plan, *order).PlanSnapshotJSON
		}},
		{name: "credit multiplication overflow", mutate: func(order *model.MembershipOrder) {
			order.Seats = 2
			order.TeamID = "team-1"
			order.TotalPriceCents = 2400
			plan := validPlan
			plan.Audience = model.MembershipAudienceTeam
			plan.CreditsPerPeriod = math.MaxInt64
			order.PlanSnapshotJSON = membershipCheckoutOrder(t, plan, *order).PlanSnapshotJSON
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := *valid
			test.mutate(&candidate)
			if _, err := membershipCheckoutSummaryFromOrder(&candidate); err == nil {
				t.Fatal("invalid frozen membership facts unexpectedly produced a checkout summary")
			}
		})
	}
}

func newPaymentCheckoutFixture(t *testing.T, idempotencyKey string) (*Service, *gorm.DB, *model.User, *model.MembershipOrder) {
	t.Helper()
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	admin, owner, _ := createCommercialTestUsers(t, db)
	if _, err := svc.UpdatePaymentSetting(admin, readyWechatPaymentSetting()); err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "pro-month")
	order, err := svc.CreateMembershipOrder(owner, CreateMembershipOrderRequest{PlanID: plan.ID}, idempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	return svc, db, owner, order
}

func TestPaymentCheckoutViewUsesDiscriminatedPublicFactsWithoutInternalFields(t *testing.T) {
	svc, db, owner, order := newPaymentCheckoutFixture(t, "typed-checkout-view")
	created, err := svc.CreatePaymentCheckout(owner, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	token := checkoutToken(t, created.CheckoutURL)
	now := time.Now()
	transaction := model.PaymentTransaction{
		ID: "transaction-internal-id", OrderType: model.PaymentOrderMembership, OrderID: order.ID, UserID: owner.ID,
		Provider: model.PaymentProviderWechat, MerchantOrderNo: "merchant-internal-number", AmountCents: order.TotalPriceCents,
		Currency: order.Currency, Status: model.PaymentTransactionPending, CodeURL: "weixin://wxpay/example",
		FailureCode: "internal-code", FailureReason: "internal-reason", ExpiresAt: &created.ExpiresAt, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&transaction).Error; err != nil {
		t.Fatal(err)
	}

	view, err := svc.PaymentCheckout(token)
	if err != nil {
		t.Fatal(err)
	}
	if view.OrderType != model.PaymentOrderMembership || view.OrderStatus != string(model.MembershipOrderPending) || view.CheckoutStatus != model.PaymentCheckoutActive {
		t.Fatalf("unexpected checkout statuses: %#v", view)
	}
	if view.MembershipSummary == nil || view.CreditTopupSummary != nil || view.ActiveTransaction == nil {
		t.Fatalf("checkout discriminator or active transaction missing: %#v", view)
	}
	if view.ActiveTransaction.Provider != model.PaymentProviderWechat || view.ActiveTransaction.CodeURL != transaction.CodeURL {
		t.Fatalf("active transaction = %#v", view.ActiveTransaction)
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		`"orderId"`, `"userId"`, `"teamId"`, `"planId"`, `"planSnapshotJson"`, `"productSnapshotJson"`,
		`"tokenHash"`, `"tokenCipher"`, `"merchantOrderNo"`, `"failureCode"`, `"failureReason"`,
		"transaction-internal-id", owner.ID, order.ID, "merchant-internal-number", "internal-reason",
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("bearer checkout JSON leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestPaymentCheckoutViewRejectsSessionOwnerMismatch(t *testing.T) {
	svc, db, owner, order := newPaymentCheckoutFixture(t, "checkout-session-owner-mismatch")
	created, err := svc.CreatePaymentCheckout(owner, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.PaymentCheckoutSession{}).
		Where("order_id = ?", order.ID).
		Update("user_id", "different-user").Error; err != nil {
		t.Fatal(err)
	}

	if _, err := svc.PaymentCheckout(checkoutToken(t, created.CheckoutURL)); err == nil {
		t.Fatal("checkout session with a mismatched owner unexpectedly exposed order facts")
	}
}

func TestPaymentCheckoutViewSanitizesBearerTransactionResponses(t *testing.T) {
	expiresAt := time.Now().Add(time.Minute)
	transaction := &model.PaymentTransaction{
		ID: "transaction-internal-id", OrderType: model.PaymentOrderMembership, OrderID: "order-internal-id", UserID: "user-internal-id",
		Provider: model.PaymentProviderWechat, MerchantOrderNo: "merchant-internal-number", ProviderTradeNo: "provider-trade-internal",
		AmountCents: 1200, Currency: "CNY", Status: model.PaymentTransactionPending, CodeURL: "weixin://wxpay/example",
		FailureCode: "internal-code", FailureReason: "internal-error", ExpiresAt: &expiresAt,
	}
	view, err := paymentCheckoutTransactionView(transaction)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"provider":"wechat","status":"pending","codeUrl":"weixin://wxpay/example","expiresAt":"`+expiresAt.Format(time.RFC3339Nano)+`"}` {
		t.Fatalf("public bearer transaction JSON = %s", encoded)
	}
}

func TestPaymentCheckoutViewPreservesCreditTopupAsSeparateSummary(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := db.AutoMigrate(&model.CreditTopupProduct{}, &model.CreditTopupOrder{}); err != nil {
		t.Fatal(err)
	}
	admin, owner, _ := createCommercialTestUsers(t, db)
	if _, err := svc.UpdatePaymentSetting(admin, readyWechatPaymentSetting()); err != nil {
		t.Fatal(err)
	}
	order := model.CreditTopupOrder{
		ID: "topup-order-internal-id", OrderNumber: "C202608090001", UserID: owner.ID, ProductID: "product-internal-id",
		BaseMicrocredits: 1000, BonusMicrocredits: 250, TotalMicrocredits: 1250,
		TotalPriceCents: 990, Currency: "CNY", Status: model.CreditTopupOrderPending,
		ProductSnapshotJSON: `{"id":"product-internal-id","name":"积分包"}`,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	token := "credit-topup-bearer-token"
	digest := sha256.Sum256([]byte(token))
	now := time.Now()
	if err := db.Create(&model.PaymentCheckoutSession{
		ID: "topup-session", OrderType: model.PaymentOrderCreditTopup, OrderID: order.ID, UserID: owner.ID,
		TokenHash: hex.EncodeToString(digest[:]), Status: model.PaymentCheckoutActive,
		ExpiresAt: now.Add(10 * time.Minute), CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	view, err := svc.PaymentCheckout(token)
	if err != nil {
		t.Fatal(err)
	}
	if view.OrderType != model.PaymentOrderCreditTopup || view.MembershipSummary != nil || view.CreditTopupSummary == nil {
		t.Fatalf("unexpected credit topup discriminator: %#v", view)
	}
	if *view.CreditTopupSummary != (CreditTopupCheckoutSummary{ActualPriceCents: 990, TotalMicrocredits: 1250}) {
		t.Fatalf("credit topup summary = %#v", view.CreditTopupSummary)
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{order.ID, owner.ID, order.ProductID, "productSnapshotJson"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("credit topup checkout leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestPaymentCheckoutSessionSequentialCreationReturnsSameEncryptedToken(t *testing.T) {
	svc, db, owner, order := newPaymentCheckoutFixture(t, "sequential-checkout-session")
	first, err := svc.CreatePaymentCheckout(owner, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreatePaymentCheckout(owner, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.CheckoutURL != second.CheckoutURL || !first.ExpiresAt.Equal(second.ExpiresAt) {
		t.Fatalf("sequential checkout recovery changed public facts: first=%#v second=%#v", first, second)
	}
	token := checkoutToken(t, first.CheckoutURL)
	var stored model.PaymentCheckoutSession
	if err := db.First(&stored, "order_id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored.TokenCipher, "enc:v1:") || strings.Contains(stored.TokenCipher, token) {
		t.Fatalf("stored token cipher is not a protected enc:v1 envelope: %q", stored.TokenCipher)
	}
	var count int64
	if err := db.Model(&model.PaymentCheckoutSession{}).Where("order_id = ?", order.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("checkout session count = %d, want 1", count)
	}
}

func TestPaymentCheckoutSessionConcurrentCreationReturnsOneURLAndExpiry(t *testing.T) {
	svc, db, owner, order := newPaymentCheckoutFixture(t, "concurrent-checkout-session")
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	const callers = 8
	results := make([]*CreatePaymentCheckoutResult, callers)
	errorsByCaller := make([]error, callers)
	var wait sync.WaitGroup
	wait.Add(callers)
	for index := 0; index < callers; index++ {
		go func(position int) {
			defer wait.Done()
			results[position], errorsByCaller[position] = svc.CreatePaymentCheckout(owner, order.ID)
		}(index)
	}
	wait.Wait()
	for index, callErr := range errorsByCaller {
		if callErr != nil {
			t.Fatalf("caller %d failed: %v", index, callErr)
		}
		if results[index].CheckoutURL != results[0].CheckoutURL || !results[index].ExpiresAt.Equal(results[0].ExpiresAt) {
			t.Fatalf("caller %d received a different checkout: %#v vs %#v", index, results[index], results[0])
		}
	}
}

func TestPaymentCheckoutSessionRecoveryRejectsBrokenEncryptedFacts(t *testing.T) {
	tests := []struct {
		name      string
		wantError string
		mutate    func(*testing.T, *Service, *gorm.DB, *model.PaymentCheckoutSession)
	}{
		{name: "invalid cipher", wantError: "密文格式无效", mutate: func(t *testing.T, _ *Service, db *gorm.DB, session *model.PaymentCheckoutSession) {
			if err := db.Model(session).Update("token_cipher", "enc:v1:not-base64").Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "hash mismatch", wantError: "密文与哈希不一致", mutate: func(t *testing.T, _ *Service, db *gorm.DB, session *model.PaymentCheckoutSession) {
			if err := db.Model(session).Update("token_hash", strings.Repeat("0", 64)).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "missing key", wantError: "加密密钥缺失", mutate: func(t *testing.T, svc *Service, _ *gorm.DB, _ *model.PaymentCheckoutSession) {
			if err := os.Remove(svc.dataDir + string(os.PathSeparator) + ".settings-key"); err != nil {
				t.Fatal(err)
			}
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svc, db, owner, order := newPaymentCheckoutFixture(t, "broken-checkout-"+strings.ReplaceAll(test.name, " ", "-"))
			created, err := svc.CreatePaymentCheckout(owner, order.ID)
			if err != nil {
				t.Fatal(err)
			}
			var session model.PaymentCheckoutSession
			if err := db.First(&session, "order_id = ?", order.ID).Error; err != nil {
				t.Fatal(err)
			}
			test.mutate(t, svc, db, &session)
			if _, err := svc.CreatePaymentCheckout(owner, order.ID); err == nil {
				t.Fatal("owner-side checkout URL recovery unexpectedly accepted broken encrypted facts")
			} else if !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("recovery error = %q, want it to contain %q", err, test.wantError)
			}
			if created.CheckoutURL == "" {
				t.Fatal("initial checkout URL unexpectedly empty")
			}
		})
	}
}

func TestPaymentCheckoutSessionCipherAADRejectsAlteredBoundFacts(t *testing.T) {
	svc, db, owner, order := newPaymentCheckoutFixture(t, "checkout-cipher-aad")
	if _, err := svc.CreatePaymentCheckout(owner, order.ID); err != nil {
		t.Fatal(err)
	}
	var stored model.PaymentCheckoutSession
	if err := db.First(&stored, "order_id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*model.PaymentCheckoutSession){
		"order type": func(session *model.PaymentCheckoutSession) { session.OrderType = model.PaymentOrderCreditTopup },
		"order id":   func(session *model.PaymentCheckoutSession) { session.OrderID = "different-order" },
		"user id":    func(session *model.PaymentCheckoutSession) { session.UserID = "different-user" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := stored
			mutate(&candidate)
			if _, err := svc.decryptPaymentCheckoutToken(&candidate); err == nil || !strings.Contains(err.Error(), "绑定订单事实") {
				t.Fatalf("AAD mutation error = %v, want bound-facts decryption failure", err)
			}
		})
	}
}

func TestPaymentCheckoutSessionHashOnlyRowIsReadableButCannotBeRecoveredByOwner(t *testing.T) {
	svc, db, owner, order := newPaymentCheckoutFixture(t, "legacy-hash-only-checkout")
	created, err := svc.CreatePaymentCheckout(owner, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	token := checkoutToken(t, created.CheckoutURL)
	var before model.PaymentCheckoutSession
	if err := db.First(&before, "order_id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&before).Update("token_cipher", "").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreatePaymentCheckout(owner, order.ID); err == nil {
		t.Fatal("hash-only checkout unexpectedly recovered an owner URL")
	}
	var after model.PaymentCheckoutSession
	if err := db.First(&after, "order_id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.TokenHash != before.TokenHash || after.ExpiresAt != before.ExpiresAt {
		t.Fatalf("hash-only checkout was rotated: before=%#v after=%#v", before, after)
	}
	if _, err := svc.PaymentCheckout(token); err != nil {
		t.Fatalf("valid direct bearer read failed for hash-only checkout: %v", err)
	}
}

func TestMembershipFulfillmentRejectsCreditMultiplicationOverflow(t *testing.T) {
	svc, _, _, _ := newPaymentCheckoutFixture(t, "fulfillment-overflow-checkout")
	plan := model.MembershipPlan{
		ID: "overflow-plan", Code: "overflow", Name: "溢出套餐", Tier: "team",
		Audience: model.MembershipAudienceTeam, BillingCycle: model.MembershipBillingCycleMonth,
		PriceCents: 1, OriginalPriceCents: 1, Currency: "CNY", CreditsPerPeriod: math.MaxInt64,
		ImageConcurrency: 1, VideoConcurrency: 1, MinSeats: 2, MaxSeats: 2,
	}
	order := membershipCheckoutOrder(t, plan, model.MembershipOrder{
		ID: "overflow-order", UserID: "owner", TeamID: "team-1", PlanID: plan.ID,
		Seats: 2, UnitPriceCents: 1, TotalPriceCents: 2, Currency: "CNY",
	})
	if _, err := svc.membershipFulfillmentForOrder(order, "actor", time.Now()); err == nil {
		t.Fatal("membership fulfillment unexpectedly overflowed credits into a ledger value")
	}
}
