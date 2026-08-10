package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/testsupport"
)

func TestPostgresPaymentCheckoutSessionConcurrentClaimReturnsFrozenWinner(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatalf("migrate base schema: %v", err)
	}
	if err := database.EnsurePaymentIntegritySchema(db); err != nil {
		t.Fatalf("ensure payment integrity schema: %v", err)
	}
	admin := &model.User{ID: "checkout-pg-admin", Username: "checkout-pg-admin", Email: "checkout-pg-admin@example.com", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	owner := &model.User{ID: "checkout-pg-owner", Username: "checkout-pg-owner", Email: "checkout-pg-owner@example.com", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create([]*model.User{admin, owner}).Error; err != nil {
		t.Fatal(err)
	}
	plan := model.MembershipPlan{
		ID: "checkout-pg-plan", Code: "checkout-pg-plan", Name: "PG 并发套餐", Tier: "pro",
		Audience: model.MembershipAudiencePersonal, BillingCycle: model.MembershipBillingCycleMonth,
		PriceCents: 1200, OriginalPriceCents: 1800, Currency: "CNY", CreditsPerPeriod: 300,
		ImageConcurrency: 1, VideoConcurrency: 1,
	}
	snapshot, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	order := &model.MembershipOrder{
		ID: "checkout-pg-order", OrderNumber: "M-PG-CHECKOUT", UserID: owner.ID,
		IdempotencyKey: "checkout-pg-request", RequestHash: strings.Repeat("a", 64),
		PlanID: plan.ID, Seats: 1, UnitPriceCents: 1200, TotalPriceCents: 1200, Currency: "CNY",
		Status: model.MembershipOrderPending, PlanSnapshotJSON: string(snapshot), CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(order).Error; err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	svc := New(repository.New(db), dataDir)
	if _, err := svc.UpdatePaymentSetting(admin, readyWechatPaymentSetting()); err != nil {
		t.Fatal(err)
	}

	const workers = 24
	type claimResult struct {
		checkout *CreatePaymentCheckoutResult
		err      error
	}
	ready := sync.WaitGroup{}
	ready.Add(workers)
	start := make(chan struct{})
	results := make(chan claimResult, workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			workerService := New(repository.New(db), dataDir)
			ready.Done()
			<-start
			checkout, claimErr := workerService.CreatePaymentCheckout(owner, order.ID)
			results <- claimResult{checkout: checkout, err: claimErr}
		}()
	}
	ready.Wait()
	close(start)

	var first *CreatePaymentCheckoutResult
	for worker := 0; worker < workers; worker++ {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent checkout claim %d: %v", worker, result.err)
		}
		if result.checkout == nil {
			t.Fatalf("concurrent checkout claim %d returned nil", worker)
		}
		if first == nil {
			first = result.checkout
			continue
		}
		if result.checkout.CheckoutURL != first.CheckoutURL || !result.checkout.ExpiresAt.Equal(first.ExpiresAt) {
			t.Fatalf("claim %d observed a different winner: first=%#v current=%#v", worker, first, result.checkout)
		}
	}
	if first == nil {
		t.Fatal("concurrent claims produced no result")
	}

	var count int64
	if err := db.Model(&model.PaymentCheckoutSession{}).Where("order_id = ?", order.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("checkout session count = %d, want 1", count)
	}
	var stored model.PaymentCheckoutSession
	if err := db.First(&stored, "order_id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !stored.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatalf("stored expiry = %s, winner expiry = %s", stored.ExpiresAt, first.ExpiresAt)
	}
	token := checkoutToken(t, first.CheckoutURL)
	digest := sha256.Sum256([]byte(token))
	if stored.TokenHash != hex.EncodeToString(digest[:]) {
		t.Fatalf("stored token hash does not match winning URL token")
	}
	if !strings.HasPrefix(stored.TokenCipher, paymentCheckoutCipherPrefix) || strings.Contains(stored.TokenCipher, token) {
		t.Fatalf("winning token cipher is not a protected v1 envelope: %q", stored.TokenCipher)
	}
	payload, err := svc.decryptPaymentCheckoutToken(&stored)
	if err != nil {
		t.Fatal(err)
	}
	if payload.Token != token || payload.CheckoutURL != first.CheckoutURL {
		t.Fatalf("winning cipher payload = %#v, want token and URL returned to every caller", payload)
	}

	loser := &model.PaymentCheckoutSession{
		ID: "checkout-pg-loser", OrderType: model.PaymentOrderMembership, OrderID: order.ID, UserID: owner.ID,
		TokenHash: strings.Repeat("b", 64), TokenCipher: "enc:v1:loser-must-not-overwrite",
		Status: model.PaymentCheckoutExpired, ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute),
	}
	winner, err := repository.New(db).CreateOrGetPaymentCheckoutSession(loser)
	if err != nil {
		t.Fatal(err)
	}
	if winner.ID != stored.ID || winner.TokenHash != stored.TokenHash || winner.TokenCipher != stored.TokenCipher || winner.Status != stored.Status || !winner.ExpiresAt.Equal(stored.ExpiresAt) {
		t.Fatalf("loser overwrote or spliced winning checkout facts: stored=%#v returned=%#v", stored, winner)
	}

	changed := readyWechatPaymentSetting()
	changed.CheckoutBaseURL = "https://changed-checkout.example.com"
	changed.Wechat.Enabled = false
	if _, err := svc.UpdatePaymentSetting(admin, changed); err != nil {
		t.Fatal(err)
	}
	recovered, err := svc.CreatePaymentCheckout(owner, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.CheckoutURL != first.CheckoutURL || !recovered.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatalf("owner recovery used mutable payment config: first=%#v recovered=%#v", first, recovered)
	}
	var afterRecovery model.PaymentCheckoutSession
	if err := db.First(&afterRecovery, "order_id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if afterRecovery.TokenHash != stored.TokenHash || afterRecovery.TokenCipher != stored.TokenCipher || !afterRecovery.ExpiresAt.Equal(stored.ExpiresAt) {
		t.Fatalf("owner recovery mutated the PostgreSQL winner: before=%#v after=%#v", stored, afterRecovery)
	}

	expiredAt := now.Add(-time.Minute)
	if err := db.Model(&model.PaymentCheckoutSession{}).Where("id = ?", stored.ID).
		Select("status", "expires_at", "updated_at").Updates(&model.PaymentCheckoutSession{
		Status: model.PaymentCheckoutExpired, ExpiresAt: expiredAt, UpdatedAt: expiredAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	conflictResult, conflictErr := svc.createOrRecoverPaymentCheckout(
		model.PaymentOrderMembership, order.ID, owner.ID, readyWechatPaymentSetting().CheckoutBaseURL, now,
	)
	if conflictResult != nil {
		t.Fatalf("unique-conflict path returned terminal checkout winner: %#v", conflictResult)
	}
	var authErr *AuthError
	if !errors.As(conflictErr, &authErr) || authErr.Status != http.StatusConflict {
		t.Fatalf("unique-conflict terminal winner error = %v, want HTTP 409", conflictErr)
	}
}
