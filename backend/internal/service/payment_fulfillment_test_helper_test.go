package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

type membershipPaymentTestFixture struct {
	Transaction     *model.PaymentTransaction
	ProviderEventID string
	ProviderTradeNo string
	PaidAt          time.Time
	Body            []byte
}

func payMembershipOrderForTest(t *testing.T, svc *Service, db *gorm.DB, order *model.MembershipOrder, providerTradeNo string) (*membershipPaymentTestFixture, error) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	expiresAt := now.Add(15 * time.Minute)
	tokenSeed := sha256.Sum256([]byte(order.ID + ":" + providerTradeNo))
	if err := db.Create(&model.PaymentCheckoutSession{
		ID: newID(), OrderType: model.PaymentOrderMembership, OrderID: order.ID, UserID: order.UserID,
		TokenHash: hex.EncodeToString(tokenSeed[:]), TokenCipher: "enc:v1:test", Status: model.PaymentCheckoutActive,
		ExpiresAt: expiresAt, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	transaction := &model.PaymentTransaction{
		ID: newID(), OrderType: model.PaymentOrderMembership, OrderID: order.ID, UserID: order.UserID,
		Provider: model.PaymentProviderWechat, MerchantOrderNo: "MTEST" + newID(), AmountCents: order.TotalPriceCents,
		Currency: order.Currency, Status: model.PaymentTransactionPending, ExpiresAt: &expiresAt, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(transaction).Error; err != nil {
		t.Fatal(err)
	}
	fixture := &membershipPaymentTestFixture{
		Transaction: transaction, ProviderEventID: "test-payment-event-" + newID(),
		ProviderTradeNo: providerTradeNo, PaidAt: now,
		Body: []byte(fmt.Sprintf(`{"order":%q,"trade":%q}`, order.ID, providerTradeNo)),
	}
	err := replayMembershipPaymentForTest(svc, fixture)
	return fixture, err
}

func replayMembershipPaymentForTest(svc *Service, fixture *membershipPaymentTestFixture) error {
	return svc.fulfillVerifiedPayment(
		fixture.Transaction.Provider, fixture.ProviderEventID, fixture.Transaction.MerchantOrderNo,
		fixture.ProviderTradeNo, fixture.Transaction.AmountCents, fixture.Transaction.Currency,
		fixture.PaidAt, fixture.Body,
	)
}
