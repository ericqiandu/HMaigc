package service

import (
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
)

func TestPaymentWebhookProviderTradeConflictReviewWriteFailureRollsBackFactAndRequestsRetry(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "pro-month")
	now := time.Now().UTC().Truncate(time.Second)
	transactions := make([]*model.PaymentTransaction, 0, 2)
	for index, suffix := range []string{"first", "second"} {
		user := &model.User{
			ID: "conflict-rollback-user-" + suffix, Username: "conflict-rollback-user-" + suffix,
			Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
		order, err := svc.CreateMembershipOrder(user, CreateMembershipOrderRequest{PlanID: plan.ID}, "conflict-rollback-order-"+suffix)
		if err != nil {
			t.Fatal(err)
		}
		createWebhookTestCheckout(t, db, order.ID, user.ID, "conflict-rollback-checkout-"+suffix, now)
		expiresAt := now.Add(15 * time.Minute)
		status := model.PaymentTransactionFailed
		failureCode := "provider_rejected"
		if index == 1 {
			status = model.PaymentTransactionCreated
			failureCode = ""
		}
		transaction := &model.PaymentTransaction{
			ID: "conflict-rollback-transaction-" + suffix, OrderType: model.PaymentOrderMembership,
			OrderID: order.ID, UserID: user.ID, Provider: model.PaymentProviderWechat,
			MerchantOrderNo: "MCONFLICTROLLBACK" + strings.ToUpper(suffix), AmountCents: order.TotalPriceCents,
			Currency: order.Currency, Status: status, FailureCode: failureCode, ExpiresAt: &expiresAt,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(transaction).Error; err != nil {
			t.Fatal(err)
		}
		transactions = append(transactions, transaction)
	}

	const providerTradeNo = "conflict-rollback-shared-trade"
	first := &model.PaymentWebhookEvent{
		ID: "conflict-rollback-first-event", Provider: model.PaymentProviderWechat,
		ProviderEventID: "conflict-rollback-first-provider-event", TransactionID: transactions[0].ID,
		MerchantOrderNo: transactions[0].MerchantOrderNo, ProviderTradeNo: providerTradeNo,
		AmountCents: transactions[0].AmountCents, Currency: transactions[0].Currency, PaidAt: &now,
		PayloadDigest: strings.Repeat("8", 64), Status: model.PaymentWebhookReceived,
		ReceivedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if _, created, err := svc.repo.RecordPaymentWebhookEvent(first); err != nil || !created {
		t.Fatalf("record first provider trade fact = created:%v err:%v", created, err)
	}
	if err := db.Exec(`
		CREATE TRIGGER reject_provider_trade_review
		BEFORE UPDATE OF status ON payment_transactions
		WHEN OLD.id = 'conflict-rollback-transaction-second' AND NEW.status = 'review_required'
		BEGIN
			SELECT RAISE(ABORT, 'injected provider-trade transaction review failure');
		END
	`).Error; err != nil {
		t.Fatal(err)
	}

	err := svc.fulfillVerifiedPayment(
		model.PaymentProviderWechat, "conflict-rollback-second-provider-event", transactions[1].MerchantOrderNo,
		providerTradeNo, transactions[1].AmountCents, transactions[1].Currency, now,
		[]byte(`{"id":"conflict-rollback-second-provider-event"}`),
	)
	if err == nil || ShouldAcknowledgePaymentWebhook(err) {
		t.Fatalf("provider-trade transaction review write failure = err:%v ack:%v, want retry", err, ShouldAcknowledgePaymentWebhook(err))
	}
	var secondEventCount int64
	if lookupErr := db.Model(&model.PaymentWebhookEvent{}).
		Where("provider_event_id = ?", "conflict-rollback-second-provider-event").Count(&secondEventCount).Error; lookupErr != nil {
		t.Fatal(lookupErr)
	}
	if secondEventCount != 0 {
		t.Fatalf("failed atomic provider-trade review left %d second facts", secondEventCount)
	}
	var secondTransaction model.PaymentTransaction
	if lookupErr := db.First(&secondTransaction, "id = ?", transactions[1].ID).Error; lookupErr != nil {
		t.Fatal(lookupErr)
	}
	if secondTransaction.Status != model.PaymentTransactionCreated || secondTransaction.FailureCode != "" {
		t.Fatalf("failed atomic provider-trade review changed transaction: %#v", secondTransaction)
	}
}
