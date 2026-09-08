package service

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestImagePromptModelRelayAuthEnvironmentRequiresIndependentSecretAndRedis(t *testing.T) {
	forwardSecret := "forward-0123456789abcdef0123456789abcdef"
	relaySecret := "relay-0123456789abcdef0123456789abcdef"
	t.Setenv("CANVAS_IMAGE_PROMPT_HMAC_SECRET", forwardSecret)
	t.Setenv("CANVAS_IMAGE_PROMPT_RELAY_ALLOWED_SERVICE_ID", "hono-image-prompt")
	t.Setenv("CANVAS_IMAGE_PROMPT_RELAY_HMAC_SECRET", relaySecret)

	if _, err := imagePromptModelRelayAuthFromEnvironment(true, nil); err == nil {
		t.Fatal("relay auth accepted a missing Redis coordinator")
	}
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { _ = client.Close() })
	config, err := imagePromptModelRelayAuthFromEnvironment(true, &runtimeCoordinator{redis: client})
	if err != nil || config == nil || string(config.secret) != relaySecret {
		t.Fatalf("relay auth config = %#v, err = %v", config, err)
	}
	t.Setenv("CANVAS_IMAGE_PROMPT_RELAY_HMAC_SECRET", forwardSecret)
	if _, err := imagePromptModelRelayAuthFromEnvironment(true, &runtimeCoordinator{redis: client}); err == nil {
		t.Fatal("relay auth accepted a reused forward HMAC secret")
	}
}

func TestImagePromptModelRelayAuthVerifiesSignatureAndRejectsReplay(t *testing.T) {
	now := time.Date(2026, time.September, 7, 15, 0, 0, 0, time.UTC)
	config := imagePromptModelRelayAuthConfig{allowedServiceID: "hono-image-prompt", secret: []byte("0123456789abcdef0123456789abcdef"), now: func() time.Time { return now }}
	svc := &Service{imagePromptModelRelayAuth: &config}
	claimed := false
	svc.imagePromptModelRelayNonceClaimer = func(_ context.Context, serviceID string, nonce string, ttl time.Duration) error {
		if serviceID != config.allowedServiceID || nonce != "nonce-1" || ttl != imagePromptModelRelayNonceTTL {
			t.Fatalf("nonce claim = %q %q %s", serviceID, nonce, ttl)
		}
		if claimed {
			return errors.New("replayed")
		}
		claimed = true
		return nil
	}
	body := []byte(`{"schemaVersion":"image-prompt-model-relay-request/v1"}`)
	digest, signature := imagePromptModelRelaySignature(config, now.Unix(), "nonce-1", body, "task-1", "task-1")
	input := ImagePromptModelRelayAuthInput{
		ServiceID: config.allowedServiceID, Timestamp: "1788783600", Nonce: "nonce-1", BodySHA256: digest,
		GenerationTaskID: "task-1", CorrelationID: "task-1", Signature: signature, Body: body,
	}
	input.Timestamp = strconv.FormatInt(now.Unix(), 10)
	if err := svc.VerifyImagePromptModelRelayAuth(context.Background(), input); err != nil {
		t.Fatalf("verify signed relay: %v", err)
	}
	err := svc.VerifyImagePromptModelRelayAuth(context.Background(), input)
	var failure *ImagePromptModelRelayError
	if !errors.As(err, &failure) || failure.Code != imagePromptErrorReplayRejected {
		t.Fatalf("replay error = %#v", err)
	}
}

func TestImagePromptModelRelayAuthRejectsExpiredAndTamperedRequestsBeforeNonceClaim(t *testing.T) {
	now := time.Date(2026, time.September, 7, 15, 0, 0, 0, time.UTC)
	config := imagePromptModelRelayAuthConfig{allowedServiceID: "hono-image-prompt", secret: []byte("0123456789abcdef0123456789abcdef"), now: func() time.Time { return now }}
	claims := 0
	svc := &Service{imagePromptModelRelayAuth: &config, imagePromptModelRelayNonceClaimer: func(context.Context, string, string, time.Duration) error { claims++; return nil }}
	body := []byte(`{"ok":true}`)
	digest, signature := imagePromptModelRelaySignature(config, now.Add(-61*time.Second).Unix(), "nonce-expired", body, "task-1", "task-1")
	expired := ImagePromptModelRelayAuthInput{ServiceID: config.allowedServiceID, Timestamp: strconv.FormatInt(now.Add(-61*time.Second).Unix(), 10), Nonce: "nonce-expired", BodySHA256: digest, GenerationTaskID: "task-1", CorrelationID: "task-1", Signature: signature, Body: body}
	if err := svc.VerifyImagePromptModelRelayAuth(context.Background(), expired); err == nil {
		t.Fatal("expired request was accepted")
	}
	validDigest, validSignature := imagePromptModelRelaySignature(config, now.Unix(), "nonce-tampered", body, "task-1", "task-1")
	tampered := ImagePromptModelRelayAuthInput{ServiceID: config.allowedServiceID, Timestamp: strconv.FormatInt(now.Unix(), 10), Nonce: "nonce-tampered", BodySHA256: validDigest, GenerationTaskID: "task-1", CorrelationID: "task-1", Signature: validSignature, Body: append(body, ' ')}
	if err := svc.VerifyImagePromptModelRelayAuth(context.Background(), tampered); err == nil {
		t.Fatal("tampered request was accepted")
	}
	if claims != 0 {
		t.Fatalf("invalid requests claimed %d nonce(s)", claims)
	}
}
