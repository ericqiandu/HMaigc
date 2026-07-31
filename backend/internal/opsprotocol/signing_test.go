package opsprotocol

import (
	"net/http"
	"testing"
	"time"
)

func TestVerifySignatureRejectsTamperingAndExpiredRequests(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	now := time.Unix(1_800_000_000, 0)
	timestamp := "1800000000"
	nonce := "nonce-0123456789"
	body := []byte(`{"action":"verify"}`)
	signature := Signature(secret, http.MethodPost, "/v1/operations", timestamp, nonce, body)

	if err := VerifySignature(secret, http.MethodPost, "/v1/operations", timestamp, nonce, body, signature, now, time.Minute); err != nil {
		t.Fatalf("expected valid signature: %v", err)
	}
	if err := VerifySignature(secret, http.MethodPost, "/v1/operations", timestamp, nonce, []byte(`{"action":"backup"}`), signature, now, time.Minute); err == nil {
		t.Fatal("expected tampered body to be rejected")
	}
	if err := VerifySignature(secret, http.MethodPost, "/v1/operations", timestamp, nonce, body, signature, now.Add(2*time.Minute), time.Minute); err == nil {
		t.Fatal("expected expired request to be rejected")
	}
}
