package service

import (
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

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
	transaction := model.PaymentTransaction{
		ID: "credit-payment", OrderType: model.PaymentOrderCreditTopup, OrderID: first.ID, UserID: user.ID,
		Provider: model.PaymentProviderWechat, MerchantOrderNo: "credit-merchant-order", AmountCents: first.TotalPriceCents,
		Currency: first.Currency, Status: model.PaymentTransactionPending, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&transaction).Error; err != nil {
		t.Fatal(err)
	}
	event := &model.PaymentWebhookEvent{ID: "credit-event", Provider: model.PaymentProviderWechat, ProviderEventID: "credit-provider-event", PayloadDigest: "digest", ReceivedAt: now, CreatedAt: now, UpdatedAt: now}
	already, err := svc.repo.FulfillPaymentTransaction(repository.PaymentFulfillment{Event: event, TransactionID: transaction.ID, ProviderTradeNo: "provider-trade", PaidAt: now})
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
	replayEvent := &model.PaymentWebhookEvent{ID: "credit-event-replay", Provider: model.PaymentProviderWechat, ProviderEventID: "credit-provider-event", PayloadDigest: "digest", ReceivedAt: now, CreatedAt: now, UpdatedAt: now}
	already, err = svc.repo.FulfillPaymentTransaction(repository.PaymentFulfillment{Event: replayEvent, TransactionID: transaction.ID, ProviderTradeNo: "provider-trade", PaidAt: now})
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
