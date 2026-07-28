package service

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/url"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
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

func TestFulfillVerifiedPaymentIsIdempotent(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	user := &model.User{ID: "payment-user", Username: "payment-user", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "pro-month")
	order, err := svc.CreateMembershipOrder(user, CreateMembershipOrderRequest{PlanID: plan.ID})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	transaction := &model.PaymentTransaction{
		ID: "transaction-1", OrderID: order.ID, UserID: user.ID,
		Provider: model.PaymentProviderWechat, MerchantOrderNo: "merchant-order-1",
		AmountCents: order.TotalPriceCents, Currency: order.Currency,
		Status: model.PaymentTransactionPending, CreatedAt: now, UpdatedAt: now,
	}
	if err := svc.repo.CreatePaymentTransaction(transaction); err != nil {
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
		t.Fatalf("webhook event count = %d, want 1", events)
	}
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
