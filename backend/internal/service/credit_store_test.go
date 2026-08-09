package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

func TestDefaultCreditTopupProductsFillTheStorefrontAndRemainIdempotent(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := db.AutoMigrate(&model.CreditTopupProduct{}); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureDefaultCreditTopupProducts(); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureDefaultCreditTopupProducts(); err != nil {
		t.Fatal(err)
	}
	var surpriseCount int64
	if err := db.Model(&model.CreditTopupProduct{}).Where("category = ?", model.CreditProductCategorySurprise).Count(&surpriseCount).Error; err != nil {
		t.Fatal(err)
	}
	var generalCount int64
	if err := db.Model(&model.CreditTopupProduct{}).Where("category = ?", model.CreditProductCategoryGeneral).Count(&generalCount).Error; err != nil {
		t.Fatal(err)
	}
	if surpriseCount != 3 || generalCount != 6 {
		t.Fatalf("default product counts surprise=%d general=%d, want 3 and 6", surpriseCount, generalCount)
	}
}

func TestCreditTopupOrderIsIdempotentAndPaymentCreditsExactlyOnce(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := db.AutoMigrate(&model.CreditTopupProduct{}, &model.CreditTopupOrder{}); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	admin := model.User{ID: "admin-credit-store", Username: "credit-admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	user := model.User{ID: "credit-buyer", Username: "credit-buyer", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	product, err := svc.SaveCreditTopupProduct(&admin, "", SaveCreditTopupProductRequest{
		Code: "test-general", Name: "测试通用积分", Category: model.CreditProductCategoryGeneral,
		BaseMicrocredits: 1_000_000, BonusMicrocredits: 500_000, PriceCents: 100, OriginalPriceCents: 200,
		RequiredMembershipTier: "origin", StockLimit: 10, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.CreateCreditTopupOrder(&user, CreateCreditTopupOrderRequest{ProductID: product.ID}, "credit-order-key")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := svc.CreateCreditTopupOrder(&user, CreateCreditTopupOrderRequest{ProductID: product.ID}, "credit-order-key")
	if err != nil {
		t.Fatal(err)
	}
	if replay.ID != first.ID {
		t.Fatalf("idempotent replay order = %s, want %s", replay.ID, first.ID)
	}
	otherProduct, err := svc.SaveCreditTopupProduct(&admin, "", SaveCreditTopupProductRequest{
		Code: "test-other", Name: "另一积分商品", Category: model.CreditProductCategoryGeneral,
		BaseMicrocredits: 2_000_000, PriceCents: 200, OriginalPriceCents: 200,
		RequiredMembershipTier: "origin", StockLimit: 10, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateCreditTopupOrder(&user, CreateCreditTopupOrderRequest{ProductID: otherProduct.ID}, "credit-order-key"); err == nil {
		t.Fatal("same idempotency key with a different product was accepted")
	} else {
		var authErr *AuthError
		if !errors.As(err, &authErr) || authErr.Status != 409 {
			t.Fatalf("idempotency conflict error = %v, want 409", err)
		}
	}
	now := time.Now()
	expiresAt := now.Add(15 * time.Minute)
	if err := db.Create(&model.PaymentCheckoutSession{
		ID: "credit-checkout", OrderType: model.PaymentOrderCreditTopup, OrderID: first.ID, UserID: user.ID,
		TokenHash: strings.Repeat("c", 64), TokenCipher: "enc:v1:test", Status: model.PaymentCheckoutActive,
		ExpiresAt: expiresAt, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	transaction := model.PaymentTransaction{
		ID: "credit-payment", OrderType: model.PaymentOrderCreditTopup, OrderID: first.ID, UserID: user.ID,
		Provider: model.PaymentProviderWechat, MerchantOrderNo: "credit-merchant-order", AmountCents: first.TotalPriceCents,
		Currency: first.Currency, Status: model.PaymentTransactionPending, ExpiresAt: &expiresAt, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&transaction).Error; err != nil {
		t.Fatal(err)
	}
	event := &model.PaymentWebhookEvent{
		ID: "credit-event", Provider: model.PaymentProviderWechat, ProviderEventID: "credit-provider-event",
		TransactionID: transaction.ID, MerchantOrderNo: transaction.MerchantOrderNo, ProviderTradeNo: "provider-trade",
		AmountCents: transaction.AmountCents, Currency: transaction.Currency, PaidAt: &now, PayloadDigest: strings.Repeat("d", 64),
		Status: model.PaymentWebhookReceived, ReceivedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if _, _, err := svc.repo.RecordPaymentWebhookEvent(event); err != nil {
		t.Fatal(err)
	}
	already, err := svc.repo.FulfillPaymentTransaction(repository.PaymentFulfillment{Event: event, TransactionID: transaction.ID, ProviderTradeNo: "provider-trade", PaidAt: now, PaymentConfirmed: true})
	if err != nil || already {
		t.Fatalf("first fulfillment already=%v err=%v", already, err)
	}
	account, err := svc.repo.CreditAccount(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if account.AvailableMicrocredits != first.TotalMicrocredits {
		t.Fatalf("balance = %d, want %d", account.AvailableMicrocredits, first.TotalMicrocredits)
	}
	var topupLedger model.CreditLedgerEntry
	if err := db.First(&topupLedger, "reference_key = ?", "credit-topup:"+first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if topupLedger.ActorUserID != model.SystemActorID {
		t.Fatalf("automatic topup ledger actor = %q, want system", topupLedger.ActorUserID)
	}
	replayEvent := *event
	replayEvent.ID = "credit-event-replay"
	storedEvent, _, err := svc.repo.RecordPaymentWebhookEvent(&replayEvent)
	if err != nil {
		t.Fatal(err)
	}
	already, err = svc.repo.FulfillPaymentTransaction(repository.PaymentFulfillment{Event: storedEvent, TransactionID: transaction.ID, ProviderTradeNo: "provider-trade", PaidAt: now, PaymentConfirmed: true})
	if err != nil || !already {
		t.Fatalf("replay fulfillment already=%v err=%v", already, err)
	}
	account, err = svc.repo.CreditAccount(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if account.AvailableMicrocredits != first.TotalMicrocredits {
		t.Fatalf("replay balance = %d, want %d", account.AvailableMicrocredits, first.TotalMicrocredits)
	}
}

func TestCreditTopupProductUpdateDoesNotCreateMissingProduct(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := db.AutoMigrate(&model.CreditTopupProduct{}, &model.CreditTopupOrder{}); err != nil {
		t.Fatal(err)
	}
	admin := model.User{ID: "admin-credit-update", Username: "credit-admin-update", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	_, err := svc.SaveCreditTopupProduct(&admin, "missing-credit-product", SaveCreditTopupProductRequest{
		Code: "missing", Name: "不存在商品", Category: model.CreditProductCategoryGeneral,
		BaseMicrocredits: 1_000_000, PriceCents: 100, OriginalPriceCents: 100,
		RequiredMembershipTier: "origin", StockLimit: -1, Enabled: true,
	})
	if err == nil {
		t.Fatal("updating a missing product created it")
	}
	var count int64
	if countErr := db.Model(&model.CreditTopupProduct{}).Count(&count).Error; countErr != nil {
		t.Fatal(countErr)
	}
	if count != 0 {
		t.Fatalf("missing update created %d products", count)
	}
}

func TestPaymentCreditTopupCancellationPreservesPayableTransaction(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := db.AutoMigrate(&model.CreditTopupProduct{}, &model.CreditTopupOrder{}); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	product := &model.CreditTopupProduct{
		ID: "credit-payable-product", Code: "credit-payable-product", Name: "Payable product",
		Category: model.CreditProductCategoryGeneral, BaseMicrocredits: 1000, PriceCents: 100,
		OriginalPriceCents: 100, Currency: "CNY", RequiredMembershipTier: "origin",
		StockLimit: -1, Enabled: true, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := db.Create(product).Error; err != nil {
		t.Fatal(err)
	}
	user := &model.User{ID: "credit-payable-user", Username: "credit-payable-user", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	order, err := svc.CreateCreditTopupOrder(user, CreateCreditTopupOrderRequest{ProductID: product.ID}, "credit-payable-order")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	transaction := &model.PaymentTransaction{
		ID: "credit-payable-transaction", OrderType: model.PaymentOrderCreditTopup, OrderID: order.ID, UserID: user.ID,
		Provider: model.PaymentProviderAlipay, MerchantOrderNo: "MCREDITPAYABLE", AmountCents: order.TotalPriceCents,
		Currency: order.Currency, Status: model.PaymentTransactionPending, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(transaction).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CancelCreditTopupOrder(user, order.ID); err == nil {
		t.Fatal("payable credit topup order was cancelled without provider reconciliation")
	}
	var persistedOrder model.CreditTopupOrder
	if err := db.First(&persistedOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persistedOrder.Status != model.CreditTopupOrderPending {
		t.Fatalf("credit topup order status = %s, want pending", persistedOrder.Status)
	}
	var persistedTransaction model.PaymentTransaction
	if err := db.First(&persistedTransaction, "id = ?", transaction.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persistedTransaction.Status != model.PaymentTransactionPending {
		t.Fatalf("credit topup transaction status = %s, want pending", persistedTransaction.Status)
	}
}
