package service

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

func TestCanonicalAlipayWebhookFormExcludesSignatureFields(t *testing.T) {
	values := url.Values{
		"trade_status": {"TRADE_SUCCESS"},
		"app_id":       {"app-1"},
		"sign_type":    {"RSA2"},
		"sign":         {"signature"},
	}
	got := canonicalAlipayWebhookForm(values)
	want := "app_id=app-1&trade_status=TRADE_SUCCESS"
	if got != want {
		t.Fatalf("canonical form = %q, want %q", got, want)
	}
}

func TestVerifyAlipayWebhookSignature(t *testing.T) {
	privateKey, publicKeyPEM := newWebhookTestRSAKey(t)
	values := url.Values{
		"app_id":       {"app-1"},
		"out_trade_no": {"merchant-order-1"},
		"sign_type":    {"RSA2"},
		"total_amount": {"19.90"},
		"trade_status": {"TRADE_SUCCESS"},
	}
	values.Set("sign", signWebhookTestMessage(t, privateKey, canonicalAlipayWebhookForm(values)))
	if err := verifyAlipayWebhookSignature(values, publicKeyPEM); err != nil {
		t.Fatal(err)
	}

	values.Set("total_amount", "29.90")
	if err := verifyAlipayWebhookSignature(values, publicKeyPEM); err == nil {
		t.Fatal("tampered notification unexpectedly passed signature verification")
	}
}

func TestParseAmountCentsIsExact(t *testing.T) {
	tests := []struct {
		value string
		want  int64
		valid bool
	}{
		{value: "0.01", want: 1, valid: true},
		{value: "19.90", want: 1990, valid: true},
		{value: "1000000.00", want: 100000000, valid: true},
		{value: "19.9", valid: false},
		{value: "-1.00", valid: false},
		{value: "NaN", valid: false},
	}
	for _, test := range tests {
		got, err := parseAmountCents(test.value)
		if test.valid && (err != nil || got != test.want) {
			t.Fatalf("parseAmountCents(%q) = %d, %v; want %d", test.value, got, err, test.want)
		}
		if !test.valid && err == nil {
			t.Fatalf("parseAmountCents(%q) unexpectedly succeeded", test.value)
		}
	}
}

func TestPaymentWebhookFulfillVerifiedPaymentIsIdempotentAcrossEventIDs(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	user := &model.User{ID: "payment-user", Username: "payment-user", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "pro-month")
	order, err := svc.CreateMembershipOrder(user, CreateMembershipOrderRequest{PlanID: plan.ID}, "payment-webhook-order")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	expiresAt := now.Add(15 * time.Minute)
	if err := db.Create(&model.PaymentCheckoutSession{
		ID: "payment-webhook-checkout", OrderType: model.PaymentOrderMembership, OrderID: order.ID, UserID: user.ID,
		TokenHash: strings.Repeat("9", 64), TokenCipher: "enc:v1:test", Status: model.PaymentCheckoutActive,
		ExpiresAt: expiresAt, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	transaction := &model.PaymentTransaction{
		ID: "transaction-1", OrderType: model.PaymentOrderMembership, OrderID: order.ID, UserID: user.ID,
		Provider: model.PaymentProviderWechat, MerchantOrderNo: "merchant-order-1",
		AmountCents: order.TotalPriceCents, Currency: order.Currency,
		Status: model.PaymentTransactionCreated, ExpiresAt: &expiresAt, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(transaction).Error; err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"id":"event-1"}`)
	if err := svc.fulfillVerifiedPayment(
		model.PaymentProviderWechat, "event-1", transaction.MerchantOrderNo,
		"wechat-trade-1", transaction.AmountCents, transaction.Currency, now, body,
	); err != nil {
		t.Fatal(err)
	}
	if err := svc.fulfillVerifiedPayment(
		model.PaymentProviderWechat, "event-2", transaction.MerchantOrderNo,
		"wechat-trade-1", transaction.AmountCents, transaction.Currency, now, []byte(`{"id":"event-2"}`),
	); err != nil {
		t.Fatal(err)
	}
	if err := svc.fulfillVerifiedPayment(
		model.PaymentProviderWechat, "event-1", transaction.MerchantOrderNo,
		"wechat-trade-1", transaction.AmountCents, transaction.Currency, now, body,
	); err != nil {
		t.Fatal(err)
	}

	var paidOrder model.MembershipOrder
	if err := db.First(&paidOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if paidOrder.Status != model.MembershipOrderPaid {
		t.Fatalf("order status = %s, want paid", paidOrder.Status)
	}
	var subscriptions int64
	if err := db.Model(&model.MembershipSubscription{}).Where("order_id = ?", order.ID).Count(&subscriptions).Error; err != nil {
		t.Fatal(err)
	}
	if subscriptions != 1 {
		t.Fatalf("subscription count = %d, want 1", subscriptions)
	}
	var ledgers int64
	if err := db.Model(&model.CreditLedgerEntry{}).Where("reference_key = ?", "membership-order:"+order.ID).Count(&ledgers).Error; err != nil {
		t.Fatal(err)
	}
	if ledgers != 1 {
		t.Fatalf("credit ledger count = %d, want 1", ledgers)
	}
	var events int64
	if err := db.Model(&model.PaymentWebhookEvent{}).Where("provider_event_id = ?", "event-1").Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("original webhook event count = %d, want 1", events)
	}
	if err := db.Model(&model.PaymentWebhookEvent{}).Where("provider_trade_no = ?", "wechat-trade-1").Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	if events != 2 {
		t.Fatalf("same-trade event count = %d, want two processed event facts", events)
	}
	var persistedEvent model.PaymentWebhookEvent
	if err := db.First(&persistedEvent, "provider_event_id = ?", "event-1").Error; err != nil {
		t.Fatal(err)
	}
	if persistedEvent.MerchantOrderNo != transaction.MerchantOrderNo || persistedEvent.ProviderTradeNo != "wechat-trade-1" ||
		persistedEvent.AmountCents != transaction.AmountCents || persistedEvent.Currency != transaction.Currency || persistedEvent.PaidAt == nil {
		t.Fatalf("persisted webhook facts = %#v", persistedEvent)
	}
	if persistedEvent.Status != model.PaymentWebhookProcessed {
		t.Fatalf("webhook status = %s, want processed", persistedEvent.Status)
	}
	if paidOrder.ResolvedBy != model.SystemActorID {
		t.Fatalf("paid order resolved_by = %q, want system actor", paidOrder.ResolvedBy)
	}
	var ledger model.CreditLedgerEntry
	if err := db.First(&ledger, "reference_key = ?", "membership-order:"+order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if ledger.ActorUserID != model.SystemActorID {
		t.Fatalf("automatic membership ledger actor = %q, want system", ledger.ActorUserID)
	}
}

func TestPaymentWebhookRecordIsIdempotentAndProcessedCannotDowngrade(t *testing.T) {
	svc, db := newMembershipTestService(t)
	now := time.Now().UTC().Truncate(time.Second)
	event := &model.PaymentWebhookEvent{
		ID: "event-fact-1", Provider: model.PaymentProviderWechat, ProviderEventID: "provider-event-fact-1",
		MerchantOrderNo: "M-EVENT-FACT", ProviderTradeNo: "provider-trade-fact", AmountCents: 1990, Currency: "CNY",
		PaidAt: &now, PayloadDigest: strings.Repeat("a", 64), Status: model.PaymentWebhookReceived,
		ReceivedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	winner, created, err := svc.repo.RecordPaymentWebhookEvent(event)
	if err != nil || !created || winner.ID != event.ID {
		t.Fatalf("record first webhook fact = winner:%#v created:%v err:%v", winner, created, err)
	}
	replay := *event
	replay.ID = "event-fact-replay"
	winner, created, err = svc.repo.RecordPaymentWebhookEvent(&replay)
	if err != nil || created || winner.ID != event.ID {
		t.Fatalf("record same digest replay = winner:%#v created:%v err:%v", winner, created, err)
	}
	conflict := replay
	conflict.PayloadDigest = strings.Repeat("b", 64)
	if _, _, err := svc.repo.RecordPaymentWebhookEvent(&conflict); !errors.Is(err, repository.ErrPaymentWebhookConflict) {
		t.Fatalf("different digest event error = %v", err)
	}
	if err := svc.repo.MarkPaymentWebhookOutcome(event.ID, model.PaymentWebhookProcessed, "", "", now); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.MarkPaymentWebhookOutcome(event.ID, model.PaymentWebhookRejected, "late_payment", "must not win", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var persisted model.PaymentWebhookEvent
	if err := db.First(&persisted, "id = ?", event.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.Status != model.PaymentWebhookProcessed || persisted.FailureCode != "" {
		t.Fatalf("processed webhook was downgraded: %#v", persisted)
	}
}

func TestPaymentWebhookProviderTradeConflictPersistsReviewAndAcknowledges(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "pro-month")
	now := time.Now().UTC().Truncate(time.Second)
	transactions := make([]*model.PaymentTransaction, 0, 2)
	for index := 0; index < 2; index++ {
		user := &model.User{
			ID: fmt.Sprintf("provider-conflict-user-%d", index), Username: fmt.Sprintf("provider-conflict-user-%d", index),
			Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
		order, err := svc.CreateMembershipOrder(user, CreateMembershipOrderRequest{PlanID: plan.ID}, fmt.Sprintf("provider-conflict-order-key-%d", index))
		if err != nil {
			t.Fatal(err)
		}
		expiresAt := now.Add(15 * time.Minute)
		if err := db.Create(&model.PaymentCheckoutSession{
			ID: fmt.Sprintf("provider-conflict-checkout-%d", index), OrderType: model.PaymentOrderMembership,
			OrderID: order.ID, UserID: user.ID, TokenHash: fmt.Sprintf("%064d", index+81), TokenCipher: "enc:v1:test",
			Status: model.PaymentCheckoutActive, ExpiresAt: expiresAt, CreatedAt: now, UpdatedAt: now,
		}).Error; err != nil {
			t.Fatal(err)
		}
		transaction := &model.PaymentTransaction{
			ID: fmt.Sprintf("provider-conflict-transaction-%d", index), OrderType: model.PaymentOrderMembership,
			OrderID: order.ID, UserID: user.ID, Provider: model.PaymentProviderWechat,
			MerchantOrderNo: fmt.Sprintf("MPROVIDERCONFLICT%d", index), AmountCents: order.TotalPriceCents,
			Currency: order.Currency, Status: model.PaymentTransactionFailed, FailureCode: "provider_rejected",
			ExpiresAt: &expiresAt, CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(transaction).Error; err != nil {
			t.Fatal(err)
		}
		transactions = append(transactions, transaction)
	}
	first := &model.PaymentWebhookEvent{
		ID: "provider-conflict-first-event", Provider: model.PaymentProviderWechat, ProviderEventID: "provider-conflict-first-provider-event",
		TransactionID: transactions[0].ID, MerchantOrderNo: transactions[0].MerchantOrderNo, ProviderTradeNo: "provider-conflict-shared-trade",
		AmountCents: transactions[0].AmountCents, Currency: transactions[0].Currency, PaidAt: &now,
		PayloadDigest: strings.Repeat("7", 64), Status: model.PaymentWebhookReviewRequired, FailureCode: "non_payable_transaction",
		ReceivedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if _, created, err := svc.repo.RecordPaymentWebhookEvent(first); err != nil || !created {
		t.Fatalf("record first provider trade fact = created:%v err:%v", created, err)
	}

	err := svc.fulfillVerifiedPayment(
		model.PaymentProviderWechat, "provider-conflict-second-provider-event", transactions[1].MerchantOrderNo,
		first.ProviderTradeNo, transactions[1].AmountCents, transactions[1].Currency, now,
		[]byte(`{"id":"provider-conflict-second-provider-event"}`),
	)
	if !ShouldAcknowledgePaymentWebhook(err) {
		t.Fatalf("durable provider trade conflict ACK = false, err=%v", err)
	}
	var second model.PaymentWebhookEvent
	if lookupErr := db.First(&second, "provider = ? AND provider_event_id = ?", model.PaymentProviderWechat, "provider-conflict-second-provider-event").Error; lookupErr != nil {
		t.Fatal(lookupErr)
	}
	if second.Status != model.PaymentWebhookReviewRequired || second.FailureCode != "provider_trade_conflict" {
		t.Fatalf("second provider trade fact = %#v", second)
	}
	var secondTransaction model.PaymentTransaction
	if lookupErr := db.First(&secondTransaction, "id = ?", transactions[1].ID).Error; lookupErr != nil {
		t.Fatal(lookupErr)
	}
	if secondTransaction.Status != model.PaymentTransactionFailed {
		t.Fatalf("conflicting transaction status = %s, want failed", secondTransaction.Status)
	}
}

func TestPaymentReconciliationProviderTradeConflictNeverFulfillsSecondOrder(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	admin := &model.User{ID: "reconcile-conflict-admin", Username: "reconcile-conflict-admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	firstUser := &model.User{ID: "reconcile-conflict-user-1", Username: "reconcile-conflict-user-1", Role: model.UserRoleUser, Status: model.UserStatusActive}
	secondUser := &model.User{ID: "reconcile-conflict-user-2", Username: "reconcile-conflict-user-2", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create([]*model.User{admin, firstUser, secondUser}).Error; err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "pro-month")
	users := []*model.User{firstUser, secondUser}
	transactions := make([]*model.PaymentTransaction, 0, len(users))
	for index, user := range users {
		order, err := svc.CreateMembershipOrder(user, CreateMembershipOrderRequest{PlanID: plan.ID}, fmt.Sprintf("reconcile-conflict-order-%d", index))
		if err != nil {
			t.Fatal(err)
		}
		createWebhookTestCheckout(t, db, order.ID, user.ID, fmt.Sprintf("reconcile-conflict-checkout-%d", index), now)
		status := model.PaymentTransactionFailed
		failureCode := "provider_rejected"
		if index == 1 {
			status = model.PaymentTransactionPending
			failureCode = ""
		}
		expiresAt := now.Add(15 * time.Minute)
		transaction := &model.PaymentTransaction{
			ID: fmt.Sprintf("reconcile-conflict-tx-%d", index), OrderType: model.PaymentOrderMembership,
			OrderID: order.ID, UserID: user.ID, Provider: model.PaymentProviderAlipay,
			MerchantOrderNo: fmt.Sprintf("MRECONCILECONFLICT%d", index), AmountCents: order.TotalPriceCents,
			Currency: order.Currency, Status: status, FailureCode: failureCode, ExpiresAt: &expiresAt,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(transaction).Error; err != nil {
			t.Fatal(err)
		}
		transactions = append(transactions, transaction)
	}
	const providerTradeNo = "reconcile-shared-provider-trade"
	firstEvent := &model.PaymentWebhookEvent{
		ID: "reconcile-conflict-first-event", Provider: model.PaymentProviderAlipay,
		ProviderEventID: "reconcile-conflict-first-provider-event", TransactionID: transactions[0].ID,
		MerchantOrderNo: transactions[0].MerchantOrderNo, ProviderTradeNo: providerTradeNo,
		AmountCents: transactions[0].AmountCents, Currency: transactions[0].Currency, PaidAt: &now,
		PayloadDigest: strings.Repeat("6", 64), Status: model.PaymentWebhookReceived,
		ReceivedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if _, created, err := svc.repo.RecordPaymentWebhookEvent(firstEvent); err != nil || !created {
		t.Fatalf("record first reconciliation provider trade fact = created:%v err:%v", created, err)
	}

	result, err := svc.reconcileConfirmedPayment(admin, transactions[1], providerPaymentFact{
		State: providerPaymentPaid, ProviderTradeNo: providerTradeNo,
		AmountCents: transactions[1].AmountCents, Currency: transactions[1].Currency, PaidAt: now,
	})
	if err == nil || result != nil {
		t.Fatalf("provider-trade conflict reconciliation = result:%#v err:%v, want audited failure", result, err)
	}
	var secondEvent model.PaymentWebhookEvent
	if lookupErr := db.First(&secondEvent, "transaction_id = ?", transactions[1].ID).Error; lookupErr != nil {
		t.Fatal(lookupErr)
	}
	if secondEvent.Status != model.PaymentWebhookReviewRequired || secondEvent.FailureCode != "provider_trade_conflict" {
		t.Fatalf("reconciliation conflict fact = %#v", secondEvent)
	}
	var secondTransaction model.PaymentTransaction
	if lookupErr := db.First(&secondTransaction, "id = ?", transactions[1].ID).Error; lookupErr != nil {
		t.Fatal(lookupErr)
	}
	if secondTransaction.Status != model.PaymentTransactionReviewRequired || secondTransaction.FailureCode != "provider_trade_conflict" {
		t.Fatalf("reconciliation conflict transaction = %#v", secondTransaction)
	}
	var subscriptionCount int64
	if lookupErr := db.Model(&model.MembershipSubscription{}).Where("order_id = ?", transactions[1].OrderID).Count(&subscriptionCount).Error; lookupErr != nil {
		t.Fatal(lookupErr)
	}
	if subscriptionCount != 0 {
		t.Fatalf("provider-trade conflict granted %d subscriptions", subscriptionCount)
	}
}

func TestPaymentWebhookPersistsPermanentValidationFailureAndAcknowledges(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	user := &model.User{ID: "webhook-validation-user", Username: "webhook-validation-user", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "pro-month")
	order, err := svc.CreateMembershipOrder(user, CreateMembershipOrderRequest{PlanID: plan.ID}, "webhook-validation-order")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	createWebhookTestCheckout(t, db, order.ID, user.ID, "webhook-validation-checkout", now)
	transaction := &model.PaymentTransaction{
		ID: "webhook-validation-transaction", OrderType: model.PaymentOrderMembership, OrderID: order.ID, UserID: user.ID,
		Provider: model.PaymentProviderWechat, MerchantOrderNo: "M-WEBHOOK-VALIDATION", AmountCents: order.TotalPriceCents,
		Currency: order.Currency, Status: model.PaymentTransactionPending, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(transaction).Error; err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"provider":"verified","event":"amount-mismatch"}`)
	err = svc.fulfillVerifiedPayment(
		transaction.Provider, "amount-mismatch-event", transaction.MerchantOrderNo, "amount-mismatch-trade",
		transaction.AmountCents+1, transaction.Currency, now, body,
	)
	if err == nil || !ShouldAcknowledgePaymentWebhook(err) {
		t.Fatalf("amount mismatch disposition err=%v acknowledged=%v", err, ShouldAcknowledgePaymentWebhook(err))
	}
	var event model.PaymentWebhookEvent
	if err := db.First(&event, "provider_event_id = ?", "amount-mismatch-event").Error; err != nil {
		t.Fatal(err)
	}
	if event.Status != model.PaymentWebhookRejected || event.FailureCode != "amount_currency_mismatch" ||
		event.MerchantOrderNo != transaction.MerchantOrderNo || event.ProviderTradeNo != "amount-mismatch-trade" {
		t.Fatalf("durable mismatch event = %#v", event)
	}

	unknownBody := []byte(`{"provider":"verified","event":"unknown-merchant"}`)
	err = svc.fulfillVerifiedPayment(
		transaction.Provider, "unknown-merchant-event", "M-UNKNOWN-MERCHANT", "unknown-merchant-trade",
		transaction.AmountCents, transaction.Currency, now, unknownBody,
	)
	if err == nil || !ShouldAcknowledgePaymentWebhook(err) {
		t.Fatalf("unknown merchant disposition err=%v acknowledged=%v", err, ShouldAcknowledgePaymentWebhook(err))
	}
	event = model.PaymentWebhookEvent{}
	if err := db.First(&event, "provider_event_id = ?", "unknown-merchant-event").Error; err != nil {
		t.Fatal(err)
	}
	if event.Status != model.PaymentWebhookRejected || event.FailureCode != "unknown_merchant_order" {
		t.Fatalf("durable unknown merchant event = %#v", event)
	}
}

func TestPaymentWebhookLateClosedAndSecondPaymentsEnterDurableReview(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	user := &model.User{ID: "webhook-review-user", Username: "webhook-review-user", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "pro-month")
	now := time.Now().UTC().Truncate(time.Second)
	tests := []struct {
		name        string
		status      model.PaymentTransactionStatus
		expiresAt   *time.Time
		storedTrade string
		callback    string
		failureCode string
	}{
		{name: "late", status: model.PaymentTransactionPending, expiresAt: func() *time.Time { value := now.Add(-time.Minute); return &value }(), callback: "late-trade", failureCode: "late_payment"},
		{name: "closed", status: model.PaymentTransactionClosed, callback: "closed-trade", failureCode: "non_payable_transaction"},
		{name: "second", status: model.PaymentTransactionPaid, storedTrade: "first-trade", callback: "second-trade", failureCode: "second_payment"},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			order, err := svc.CreateMembershipOrder(user, CreateMembershipOrderRequest{PlanID: plan.ID}, fmt.Sprintf("webhook-review-order-%d", index))
			if err != nil {
				t.Fatal(err)
			}
			createWebhookTestCheckout(t, db, order.ID, user.ID, fmt.Sprintf("webhook-review-checkout-%d", index), now)
			transaction := &model.PaymentTransaction{
				ID: fmt.Sprintf("webhook-review-transaction-%d", index), OrderType: model.PaymentOrderMembership,
				OrderID: order.ID, UserID: user.ID, Provider: model.PaymentProviderWechat,
				MerchantOrderNo: fmt.Sprintf("MWEBHOOKREVIEW%02d", index), ProviderTradeNo: test.storedTrade,
				AmountCents: order.TotalPriceCents, Currency: order.Currency, Status: test.status,
				ExpiresAt: test.expiresAt, CreatedAt: now, UpdatedAt: now,
			}
			if test.status == model.PaymentTransactionPaid {
				transaction.PaidAt = &now
			}
			if err := db.Create(transaction).Error; err != nil {
				t.Fatal(err)
			}
			err = svc.fulfillVerifiedPayment(
				transaction.Provider, fmt.Sprintf("webhook-review-event-%d", index), transaction.MerchantOrderNo,
				test.callback, transaction.AmountCents, transaction.Currency, now, []byte(fmt.Sprintf(`{"case":%q}`, test.name)),
			)
			if err == nil || !ShouldAcknowledgePaymentWebhook(err) {
				t.Fatalf("review disposition err=%v acknowledged=%v", err, ShouldAcknowledgePaymentWebhook(err))
			}
			var event model.PaymentWebhookEvent
			if err := db.First(&event, "provider_event_id = ?", fmt.Sprintf("webhook-review-event-%d", index)).Error; err != nil {
				t.Fatal(err)
			}
			if event.Status != model.PaymentWebhookReviewRequired || event.FailureCode != test.failureCode {
				t.Fatalf("durable review event = %#v", event)
			}
		})
	}
}

func createWebhookTestCheckout(t *testing.T, db *gorm.DB, orderID string, userID string, seed string, now time.Time) {
	t.Helper()
	tokenHash := sha256.Sum256([]byte(seed))
	if err := db.Create(&model.PaymentCheckoutSession{
		ID: seed, OrderType: model.PaymentOrderMembership, OrderID: orderID, UserID: userID,
		TokenHash: hex.EncodeToString(tokenHash[:]), TokenCipher: "enc:v1:test", Status: model.PaymentCheckoutActive,
		ExpiresAt: now.Add(15 * time.Minute), CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestPaymentWebhookDifferentDigestConflictsWithoutOverwrite(t *testing.T) {
	svc, db := newMembershipTestService(t)
	now := time.Now().UTC()
	first := &model.PaymentWebhookEvent{
		ID: "digest-original", Provider: model.PaymentProviderAlipay, ProviderEventID: "digest-provider-event",
		MerchantOrderNo: "M-DIGEST", ProviderTradeNo: "digest-trade", AmountCents: 100, Currency: "CNY", PaidAt: &now,
		PayloadDigest: strings.Repeat("1", 64), Status: model.PaymentWebhookReceived, ReceivedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if _, _, err := svc.repo.RecordPaymentWebhookEvent(first); err != nil {
		t.Fatal(err)
	}
	changed := *first
	changed.ID = "digest-changed"
	changed.PayloadDigest = strings.Repeat("2", 64)
	_, _, err := svc.repo.RecordPaymentWebhookEvent(&changed)
	if !errors.Is(err, repository.ErrPaymentWebhookConflict) || ShouldAcknowledgePaymentWebhook(err) {
		t.Fatalf("digest conflict err=%v acknowledged=%v", err, ShouldAcknowledgePaymentWebhook(err))
	}
	var persisted model.PaymentWebhookEvent
	if err := db.First(&persisted, "provider_event_id = ?", first.ProviderEventID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.ID != first.ID || persisted.PayloadDigest != first.PayloadDigest {
		t.Fatalf("digest conflict overwrote first fact: %#v", persisted)
	}
}

func TestPaymentWebhookSignatureOrFactCommitFailureRequestsProviderRetry(t *testing.T) {
	t.Run("signature failure", func(t *testing.T) {
		svc, db := newMembershipTestService(t)
		merchantPrivate, platformPublicPEM := newWebhookTestRSAKey(t)
		admin := &model.User{ID: "signature-admin", Username: "signature-admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
		if err := db.Create(admin).Error; err != nil {
			t.Fatal(err)
		}
		setting := readyWechatPaymentSetting()
		setting.Wechat.MerchantPrivateKey = rsaPrivateKeyPEM(t, merchantPrivate)
		setting.Wechat.PlatformPublicKey = platformPublicPEM
		setting.Wechat.APIv3Key = strings.Repeat("k", 32)
		if _, err := svc.UpdatePaymentSetting(admin, setting); err != nil {
			t.Fatal(err)
		}
		err := svc.HandleWechatPaymentWebhook(WechatPaymentWebhookHeaders{
			Timestamp: strconv.FormatInt(time.Now().Unix(), 10), Nonce: "invalid-signature-nonce",
			Signature: base64.StdEncoding.EncodeToString([]byte("invalid-signature")),
		}, []byte(`{"id":"must-not-persist"}`))
		if err == nil || ShouldAcknowledgePaymentWebhook(err) {
			t.Fatalf("signature failure err=%v acknowledged=%v", err, ShouldAcknowledgePaymentWebhook(err))
		}
		var count int64
		if err := db.Model(&model.PaymentWebhookEvent{}).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("signature failure event count = %d, err=%v", count, err)
		}
	})

	t.Run("verified fact commit failure", func(t *testing.T) {
		svc, db := newMembershipTestService(t)
		if err := svc.EnsureDefaultMembershipPlans(); err != nil {
			t.Fatal(err)
		}
		user := &model.User{ID: "fact-commit-user", Username: "fact-commit-user", Role: model.UserRoleUser, Status: model.UserStatusActive}
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
		plan := membershipPlanByCode(t, db, "pro-month")
		order, err := svc.CreateMembershipOrder(user, CreateMembershipOrderRequest{PlanID: plan.ID}, "fact-commit-order")
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		transaction := &model.PaymentTransaction{
			ID: "fact-commit-transaction", OrderType: model.PaymentOrderMembership, OrderID: order.ID, UserID: user.ID,
			Provider: model.PaymentProviderWechat, MerchantOrderNo: "MFACTCOMMIT", AmountCents: order.TotalPriceCents,
			Currency: order.Currency, Status: model.PaymentTransactionPending, CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(transaction).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(`CREATE TRIGGER reject_verified_payment_fact BEFORE INSERT ON payment_webhook_events BEGIN SELECT RAISE(FAIL, 'injected verified fact commit failure'); END`).Error; err != nil {
			t.Fatal(err)
		}
		err = svc.fulfillVerifiedPayment(
			transaction.Provider, "fact-commit-event", transaction.MerchantOrderNo, "fact-commit-trade",
			transaction.AmountCents, transaction.Currency, now, []byte(`{"id":"fact-commit-event"}`),
		)
		if err == nil || ShouldAcknowledgePaymentWebhook(err) {
			t.Fatalf("fact commit failure err=%v acknowledged=%v", err, ShouldAcknowledgePaymentWebhook(err))
		}
		var count int64
		if err := db.Model(&model.PaymentWebhookEvent{}).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("fact commit failure event count = %d, err=%v", count, err)
		}
	})
}

func TestPaymentReconciliationAlipayQueryNormalizesSignedProviderStatesAndCloseFailure(t *testing.T) {
	merchantPrivate, _ := newWebhookTestRSAKey(t)
	platformPrivate, platformPublicPEM := newWebhookTestRSAKey(t)
	channel := paymentChannelSettingValue{
		AppID: "alipay-app", MerchantID: "alipay-merchant", MerchantPrivateKey: rsaPrivateKeyPEM(t, merchantPrivate),
		PlatformPublicKey: platformPublicPEM, GatewayURL: "https://openapi.alipay.com/gateway.do",
	}
	transaction := &model.PaymentTransaction{
		Provider: model.PaymentProviderAlipay, MerchantOrderNo: "MALIPAYQUERY", AmountCents: 1990, Currency: "CNY",
	}
	tests := []struct {
		name  string
		inner string
		state providerPaymentState
	}{
		{
			name:  "paid",
			inner: `{"code":"10000","msg":"Success","out_trade_no":"MALIPAYQUERY","trade_no":"202608090001","trade_status":"TRADE_SUCCESS","total_amount":"19.90","send_pay_date":"2026-08-09 13:14:15"}`,
			state: providerPaymentPaid,
		},
		{
			name:  "unpaid",
			inner: `{"code":"10000","msg":"Success","out_trade_no":"MALIPAYQUERY","trade_status":"WAIT_BUYER_PAY","total_amount":"19.90"}`,
			state: providerPaymentUnpaid,
		},
		{
			name:  "not found",
			inner: `{"code":"40004","msg":"Business Failed","sub_code":"ACQ.TRADE_NOT_EXIST","sub_msg":"trade not exist"}`,
			state: providerPaymentNotFound,
		},
		{
			name:  "unknown",
			inner: `{"code":"20000","msg":"Service Currently Unavailable"}`,
			state: providerPaymentUnknown,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withPaymentHTTPTransport(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Fatal(err)
				}
				values, err := url.ParseQuery(string(body))
				if err != nil {
					t.Fatal(err)
				}
				if values.Get("method") != "alipay.trade.query" || !strings.Contains(values.Get("biz_content"), transaction.MerchantOrderNo) {
					t.Fatalf("unexpected Alipay query form: %s", body)
				}
				signature := signWebhookTestMessage(t, platformPrivate, test.inner)
				responseBody := fmt.Sprintf(`{"alipay_trade_query_response":%s,"sign":%q}`, test.inner, signature)
				return paymentTestResponse(request, http.StatusOK, responseBody, nil), nil
			}))
			fact, err := queryProviderPayment(transaction, channel)
			if err != nil {
				t.Fatal(err)
			}
			if fact.State != test.state {
				t.Fatalf("query state = %s, want %s", fact.State, test.state)
			}
			if test.state == providerPaymentPaid && (fact.ProviderTradeNo != "202608090001" || fact.AmountCents != 1990 || fact.Currency != "CNY" || fact.PaidAt.IsZero()) {
				t.Fatalf("paid Alipay fact = %#v", fact)
			}
		})
	}

	withPaymentHTTPTransport(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatal(err)
		}
		if values.Get("method") != "alipay.trade.close" {
			t.Fatalf("Alipay close method = %q", values.Get("method"))
		}
		inner := `{"code":"40004","msg":"Business Failed","sub_code":"ACQ.TRADE_HAS_SUCCESS","sub_msg":"trade paid"}`
		signature := signWebhookTestMessage(t, platformPrivate, inner)
		responseBody := fmt.Sprintf(`{"alipay_trade_close_response":%s,"sign":%q}`, inner, signature)
		return paymentTestResponse(request, http.StatusOK, responseBody, nil), nil
	}))
	if err := closeProviderPayment(transaction, channel); err == nil {
		t.Fatal("provider close failure unexpectedly succeeded")
	}
}

func TestPaymentReconciliationWechatQueryAndCloseUseSignedMerchantFacts(t *testing.T) {
	merchantPrivate, _ := newWebhookTestRSAKey(t)
	platformPrivate, platformPublicPEM := newWebhookTestRSAKey(t)
	channel := paymentChannelSettingValue{
		AppID: "wechat-app", MerchantID: "wechat-merchant", MerchantSerialNo: "merchant-serial",
		MerchantPrivateKey: rsaPrivateKeyPEM(t, merchantPrivate), PlatformPublicKey: platformPublicPEM,
		GatewayURL: "https://api.mch.weixin.qq.com",
	}
	transaction := &model.PaymentTransaction{
		Provider: model.PaymentProviderWechat, MerchantOrderNo: "MWECHATQUERY", AmountCents: 1990, Currency: "CNY",
	}
	withPaymentHTTPTransport(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if !strings.Contains(request.Header.Get("Authorization"), `mchid="wechat-merchant"`) {
			t.Fatalf("missing signed WeChat authorization: %q", request.Header.Get("Authorization"))
		}
		if request.Method == http.MethodGet {
			if request.URL.EscapedPath() != "/v3/pay/transactions/out-trade-no/MWECHATQUERY" || request.URL.Query().Get("mchid") != channel.MerchantID {
				t.Fatalf("unexpected WeChat query URL: %s", request.URL.String())
			}
			body := `{"appid":"wechat-app","mchid":"wechat-merchant","out_trade_no":"MWECHATQUERY","transaction_id":"wechat-trade-1","trade_state":"SUCCESS","success_time":"2026-08-09T13:14:15+08:00","amount":{"total":1990,"currency":"CNY"}}`
			return signedWechatTestResponse(t, request, http.StatusOK, body, platformPrivate), nil
		}
		if request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/close") {
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(body), `"mchid":"wechat-merchant"`) {
				t.Fatalf("unexpected WeChat close body: %s", body)
			}
			return signedWechatTestResponse(t, request, http.StatusNoContent, "", platformPrivate), nil
		}
		t.Fatalf("unexpected WeChat request: %s %s", request.Method, request.URL)
		return nil, nil
	}))
	fact, err := queryProviderPayment(transaction, channel)
	if err != nil {
		t.Fatal(err)
	}
	if fact.State != providerPaymentPaid || fact.ProviderTradeNo != "wechat-trade-1" || fact.AmountCents != 1990 || fact.Currency != "CNY" || fact.PaidAt.IsZero() {
		t.Fatalf("paid WeChat fact = %#v", fact)
	}
	if err := closeProviderPayment(transaction, channel); err != nil {
		t.Fatal(err)
	}
}

func TestPaymentReconciliationStrongAuditsPaidClosedAndFailedOutcomes(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	merchantPrivate, _ := newWebhookTestRSAKey(t)
	platformPrivate, platformPublicPEM := newWebhookTestRSAKey(t)
	admin := &model.User{ID: "reconcile-admin", Username: "reconcile-admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	user := &model.User{ID: "reconcile-user", Username: "reconcile-user", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create([]*model.User{admin, user}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdatePaymentSetting(admin, PaymentSettingRequest{
		Alipay: PaymentChannelSettingRequest{
			Enabled: true, AppID: "reconcile-app", MerchantID: "reconcile-merchant",
			MerchantPrivateKey: rsaPrivateKeyPEM(t, merchantPrivate), PlatformPublicKey: platformPublicPEM,
			NotifyURL: "https://merchant.example.com/alipay", GatewayURL: "https://openapi.alipay.com/gateway.do",
		},
	}); err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "pro-month")
	tests := []struct {
		name          string
		queryResponse func(transaction *model.PaymentTransaction) string
		closeResponse string
		wantStatus    model.PaymentTransactionStatus
		wantError     bool
	}{
		{
			name: "paid",
			queryResponse: func(transaction *model.PaymentTransaction) string {
				return fmt.Sprintf(`{"code":"10000","msg":"Success","out_trade_no":%q,"trade_no":"reconcile-paid-trade","trade_status":"TRADE_SUCCESS","total_amount":%q,"send_pay_date":"2026-08-09 13:14:15"}`, transaction.MerchantOrderNo, formatAmountCents(transaction.AmountCents))
			},
			wantStatus: model.PaymentTransactionPaid,
		},
		{
			name: "unpaid closed",
			queryResponse: func(transaction *model.PaymentTransaction) string {
				return fmt.Sprintf(`{"code":"10000","msg":"Success","out_trade_no":%q,"trade_status":"WAIT_BUYER_PAY","total_amount":%q}`, transaction.MerchantOrderNo, formatAmountCents(transaction.AmountCents))
			},
			closeResponse: `{"code":"10000","msg":"Success"}`,
			wantStatus:    model.PaymentTransactionClosed,
		},
		{
			name: "unknown",
			queryResponse: func(*model.PaymentTransaction) string {
				return `{"code":"20000","msg":"Service Currently Unavailable"}`
			},
			wantStatus: model.PaymentTransactionReviewRequired,
			wantError:  true,
		},
		{
			name: "close failed",
			queryResponse: func(transaction *model.PaymentTransaction) string {
				return fmt.Sprintf(`{"code":"10000","msg":"Success","out_trade_no":%q,"trade_status":"WAIT_BUYER_PAY","total_amount":%q}`, transaction.MerchantOrderNo, formatAmountCents(transaction.AmountCents))
			},
			closeResponse: `{"code":"40004","msg":"Business Failed","sub_code":"ACQ.TRADE_HAS_SUCCESS","sub_msg":"trade paid"}`,
			wantStatus:    model.PaymentTransactionReviewRequired,
			wantError:     true,
		},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			order, err := svc.CreateMembershipOrder(user, CreateMembershipOrderRequest{PlanID: plan.ID}, fmt.Sprintf("reconcile-order-%d", index))
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			expiresAt := now.Add(15 * time.Minute)
			if err := db.Create(&model.PaymentCheckoutSession{
				ID: fmt.Sprintf("reconcile-checkout-%d", index), OrderType: model.PaymentOrderMembership,
				OrderID: order.ID, UserID: user.ID, TokenHash: fmt.Sprintf("%064d", index+70), TokenCipher: "enc:v1:test",
				Status: model.PaymentCheckoutActive, ExpiresAt: expiresAt, CreatedAt: now, UpdatedAt: now,
			}).Error; err != nil {
				t.Fatal(err)
			}
			transaction := &model.PaymentTransaction{
				ID: fmt.Sprintf("reconcile-transaction-%d", index), OrderType: model.PaymentOrderMembership,
				OrderID: order.ID, UserID: user.ID, Provider: model.PaymentProviderAlipay,
				MerchantOrderNo: fmt.Sprintf("MRECONCILE%02d", index), AmountCents: order.TotalPriceCents,
				Currency: order.Currency, Status: model.PaymentTransactionPending, ExpiresAt: &expiresAt,
				CreatedAt: now, UpdatedAt: now,
			}
			if err := db.Create(transaction).Error; err != nil {
				t.Fatal(err)
			}
			withPaymentHTTPTransport(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Fatal(err)
				}
				values, err := url.ParseQuery(string(body))
				if err != nil {
					t.Fatal(err)
				}
				inner := test.queryResponse(transaction)
				field := "alipay_trade_query_response"
				if values.Get("method") == "alipay.trade.close" {
					inner = test.closeResponse
					field = "alipay_trade_close_response"
				}
				signature := signWebhookTestMessage(t, platformPrivate, inner)
				responseBody := fmt.Sprintf(`{%q:%s,"sign":%q}`, field, inner, signature)
				return paymentTestResponse(request, http.StatusOK, responseBody, nil), nil
			}))
			result, reconcileErr := svc.AdminReconcilePaymentTransaction(admin, transaction.ID)
			if test.wantError && reconcileErr == nil || !test.wantError && reconcileErr != nil {
				t.Fatalf("reconcile result=%#v err=%v, wantError=%v", result, reconcileErr, test.wantError)
			}
			var stored model.PaymentTransaction
			if err := db.First(&stored, "id = ?", transaction.ID).Error; err != nil {
				t.Fatal(err)
			}
			if stored.Status != test.wantStatus {
				t.Fatalf("transaction status = %s, want %s", stored.Status, test.wantStatus)
			}
			var audits []model.AdminAuditEvent
			if err := db.Where("target_type = ? AND target_id = ?", "payment_transaction", transaction.ID).Order("created_at asc").Find(&audits).Error; err != nil {
				t.Fatal(err)
			}
			if len(audits) != 2 || audits[0].Action != "payment_transaction.reconcile_attempt" {
				t.Fatalf("strong reconcile audits = %#v", audits)
			}
			if stored.Status == model.PaymentTransactionPaid {
				var paidOrder model.MembershipOrder
				if err := db.First(&paidOrder, "id = ?", order.ID).Error; err != nil {
					t.Fatal(err)
				}
				if paidOrder.ResolvedBy != model.SystemActorID {
					t.Fatalf("reconciled paid order actor = %q, want system", paidOrder.ResolvedBy)
				}
			}
		})
	}
}

func TestPaymentReconciliationRejectedAndUnknownTransactionAttemptsAreAuditedWithoutResourceFacts(t *testing.T) {
	svc, db := newMembershipTestService(t)
	admin := &model.User{ID: "reconcile-access-admin", Username: "reconcile-access-admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	nonAdmin := &model.User{ID: "reconcile-access-user", Username: "reconcile-access-user", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create([]*model.User{admin, nonAdmin}).Error; err != nil {
		t.Fatal(err)
	}

	const requestedID = "opaque-transaction-id"
	if _, err := svc.AdminReconcilePaymentTransaction(nonAdmin, requestedID); err == nil {
		t.Fatal("non-admin reconciliation unexpectedly succeeded")
	} else {
		requireAuthStatus(t, err, http.StatusForbidden)
	}
	assertReconciliationAccessAudit(t, db, nonAdmin.ID, "payment_transaction.reconcile_rejected", requestedID, `{"outcome":"rejected","failureCode":"admin_required"}`)

	if _, err := svc.AdminReconcilePaymentTransaction(admin, requestedID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("unknown transaction reconciliation error = %v, want gorm.ErrRecordNotFound", err)
	}
	assertReconciliationAccessAudit(t, db, admin.ID, "payment_transaction.reconcile_failed", requestedID, `{"outcome":"failed","failureCode":"transaction_not_found"}`)

	var beforeAnonymous int64
	if err := db.Model(&model.AdminAuditEvent{}).Count(&beforeAnonymous).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AdminReconcilePaymentTransaction(nil, requestedID); err == nil {
		t.Fatal("anonymous reconciliation unexpectedly succeeded")
	} else {
		requireAuthStatus(t, err, http.StatusUnauthorized)
	}
	var afterAnonymous int64
	if err := db.Model(&model.AdminAuditEvent{}).Count(&afterAnonymous).Error; err != nil {
		t.Fatal(err)
	}
	if afterAnonymous != beforeAnonymous {
		t.Fatalf("anonymous attempt created domain audit: before=%d after=%d", beforeAnonymous, afterAnonymous)
	}
}

func assertReconciliationAccessAudit(t *testing.T, db *gorm.DB, actorID string, action string, targetID string, wantMetadata string) {
	t.Helper()
	var audit model.AdminAuditEvent
	if err := db.Where("actor_user_id = ? AND action = ? AND target_type = ? AND target_id = ?", actorID, action, "payment_transaction", targetID).First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	if audit.MetadataJSON != wantMetadata {
		t.Fatalf("access audit metadata = %s, want %s", audit.MetadataJSON, wantMetadata)
	}
	if strings.Contains(audit.MetadataJSON, "provider") || strings.Contains(audit.MetadataJSON, "merchantOrderNo") {
		t.Fatalf("access audit leaked resource facts: %s", audit.MetadataJSON)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func withPaymentHTTPTransport(t *testing.T, transport http.RoundTripper) {
	t.Helper()
	previous := paymentHTTPClient
	paymentHTTPClient = &http.Client{Transport: transport, Timeout: time.Second}
	t.Cleanup(func() { paymentHTTPClient = previous })
}

func paymentTestResponse(request *http.Request, status int, body string, header http.Header) *http.Response {
	if header == nil {
		header = make(http.Header)
	}
	return &http.Response{
		StatusCode: status, Status: fmt.Sprintf("%d test", status), Header: header,
		Body: io.NopCloser(strings.NewReader(body)), Request: request,
	}
}

func signedWechatTestResponse(t *testing.T, request *http.Request, status int, body string, privateKey *rsa.PrivateKey) *http.Response {
	t.Helper()
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := "wechat-response-nonce"
	signature := signWebhookTestMessage(t, privateKey, timestamp+"\n"+nonce+"\n"+body+"\n")
	header := make(http.Header)
	header.Set("Wechatpay-Timestamp", timestamp)
	header.Set("Wechatpay-Nonce", nonce)
	header.Set("Wechatpay-Signature", signature)
	header.Set("Wechatpay-Serial", "platform-key-id")
	return paymentTestResponse(request, status, body, header)
}

func rsaPrivateKeyPEM(t *testing.T, privateKey *rsa.PrivateKey) string {
	t.Helper()
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}))
}

func newWebhookTestRSAKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	publicKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: encoded})
	return privateKey, string(publicKeyPEM)
}

func signWebhookTestMessage(t *testing.T, privateKey *rsa.PrivateKey, message string) string {
	t.Helper()
	digest := sha256.Sum256([]byte(message))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(signature)
}
