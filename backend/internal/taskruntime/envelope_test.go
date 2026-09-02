package taskruntime

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTaskEnvelopeCanonicalEncodingIsStable(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"prompt":"生成一段镜头","config":{"model":"seedance"}}`)
	claims := NewClaims(testBinding(), payload, time.Unix(1_800_000_000, 0).UTC())
	key := bytes.Repeat([]byte{0x42}, 32)

	first, err := Sign(claims, "task-envelope-2026-08", key)
	if err != nil {
		t.Fatalf("sign first envelope: %v", err)
	}
	second, err := Sign(claims, "task-envelope-2026-08", key)
	if err != nil {
		t.Fatalf("sign second envelope: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("canonical envelope changed between identical signs:\n%s\n%s", first, second)
	}

	wantPrefix := `{"keyId":"task-envelope-2026-08","claims":{"version":1,"tenantKind":"team","tenantId":"team-1","projectId":"project-1","canvasId":"canvas-1","runId":"run-1","artifactRevisionId":"revision-1","taskId":"task-1","taskType":"video_image_to_video","requesterId":"user-1","payloadDigest":"`
	if !strings.HasPrefix(string(first), wantPrefix) {
		t.Fatalf("canonical field order changed: %s", first)
	}
	if strings.Contains(string(first), "\n") || strings.Contains(string(first), " ") {
		t.Fatalf("canonical envelope must not contain insignificant whitespace: %s", first)
	}

	verified, err := Verify(first, VerificationKeys{"task-envelope-2026-08": key}, testBinding(), payload, time.Unix(1_700_000_000, 0).UTC())
	if err != nil {
		t.Fatalf("verify canonical envelope: %v", err)
	}
	if verified.PayloadDigest != PayloadDigest(payload) {
		t.Fatalf("payload digest = %q, want %q", verified.PayloadDigest, PayloadDigest(payload))
	}
}

func TestTaskEnvelopeRejectsEveryBoundFactMismatch(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"mode":"text","prompt":"hello"}`)
	key := bytes.Repeat([]byte{0x31}, 32)
	expiresAt := time.Unix(1_800_000_000, 0).UTC()
	encoded, err := Sign(NewClaims(testBinding(), payload, expiresAt), "current", key)
	if err != nil {
		t.Fatalf("sign envelope: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Binding)
	}{
		{name: "tenant kind", mutate: func(binding *Binding) { binding.TenantKind = "personal" }},
		{name: "tenant id", mutate: func(binding *Binding) { binding.TenantID = "team-2" }},
		{name: "project", mutate: func(binding *Binding) { binding.ProjectID = "project-2" }},
		{name: "canvas", mutate: func(binding *Binding) { binding.CanvasID = "canvas-2" }},
		{name: "run", mutate: func(binding *Binding) { binding.RunID = "run-2" }},
		{name: "artifact revision", mutate: func(binding *Binding) { binding.ArtifactRevisionID = "revision-2" }},
		{name: "task", mutate: func(binding *Binding) { binding.TaskID = "task-2" }},
		{name: "task type", mutate: func(binding *Binding) { binding.TaskType = "image_generation" }},
		{name: "requester", mutate: func(binding *Binding) { binding.RequesterID = "user-2" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expected := testBinding()
			tc.mutate(&expected)
			if _, err := Verify(encoded, VerificationKeys{"current": key}, expected, payload, time.Unix(1_700_000_000, 0).UTC()); !errors.Is(err, ErrClaimMismatch) {
				t.Fatalf("Verify() error = %v, want ErrClaimMismatch", err)
			}
		})
	}
}

func TestTaskEnvelopeRejectsPayloadMutationExpiryAndReplay(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"prompt":"original"}`)
	key := bytes.Repeat([]byte{0x23}, 32)
	expiresAt := time.Unix(1_800_000_000, 0).UTC()
	encoded, err := Sign(NewClaims(testBinding(), payload, expiresAt), "current", key)
	if err != nil {
		t.Fatalf("sign envelope: %v", err)
	}

	if _, err := Verify(encoded, VerificationKeys{"current": key}, testBinding(), []byte(`{"prompt":"changed"}`), time.Unix(1_700_000_000, 0).UTC()); !errors.Is(err, ErrPayloadDigestMismatch) {
		t.Fatalf("mutated payload error = %v, want ErrPayloadDigestMismatch", err)
	}
	if _, err := Verify(encoded, VerificationKeys{"current": key}, testBinding(), payload, expiresAt); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired envelope error = %v, want ErrExpired", err)
	}

	replayed := testBinding()
	replayed.TaskID = "task-replayed"
	if _, err := Verify(encoded, VerificationKeys{"current": key}, replayed, payload, time.Unix(1_700_000_000, 0).UTC()); !errors.Is(err, ErrClaimMismatch) {
		t.Fatalf("replayed envelope error = %v, want ErrClaimMismatch", err)
	}
}

func TestTaskEnvelopeSupportsKeyRotationWithoutImplicitFallback(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"prompt":"rotate"}`)
	oldKey := bytes.Repeat([]byte{0x11}, 32)
	newKey := bytes.Repeat([]byte{0x22}, 32)
	encoded, err := Sign(NewClaims(testBinding(), payload, time.Unix(1_800_000_000, 0).UTC()), "old", oldKey)
	if err != nil {
		t.Fatalf("sign old envelope: %v", err)
	}

	if _, err := Verify(encoded, VerificationKeys{"old": oldKey, "new": newKey}, testBinding(), payload, time.Unix(1_700_000_000, 0).UTC()); err != nil {
		t.Fatalf("verify old envelope during rotation: %v", err)
	}
	if _, err := Verify(encoded, VerificationKeys{"new": newKey}, testBinding(), payload, time.Unix(1_700_000_000, 0).UTC()); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("missing old key error = %v, want ErrUnknownKey", err)
	}
	if _, err := Verify(encoded, VerificationKeys{"old": newKey}, testBinding(), payload, time.Unix(1_700_000_000, 0).UTC()); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("wrong key error = %v, want ErrInvalidSignature", err)
	}
}

func TestTaskEnvelopeRejectsNonCanonicalAndUnknownJSON(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"prompt":"canonical"}`)
	key := bytes.Repeat([]byte{0x51}, 32)
	encoded, err := Sign(NewClaims(testBinding(), payload, time.Unix(1_800_000_000, 0).UTC()), "current", key)
	if err != nil {
		t.Fatal(err)
	}

	withWhitespace := append([]byte(" "), encoded...)
	if _, err := Authenticate(withWhitespace, VerificationKeys{"current": key}, payload, time.Unix(1_700_000_000, 0).UTC()); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("non-canonical whitespace error = %v, want ErrInvalidEnvelope", err)
	}
	withUnknown := append(encoded[:len(encoded)-1], []byte(`,"unexpected":true}`)...)
	if _, err := Authenticate(withUnknown, VerificationKeys{"current": key}, payload, time.Unix(1_700_000_000, 0).UTC()); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("unknown field error = %v, want ErrInvalidEnvelope", err)
	}
	invalidTenant := NewClaims(testBinding(), payload, time.Unix(1_800_000_000, 0).UTC())
	invalidTenant.TenantKind = "workspace"
	if _, err := Sign(invalidTenant, "current", key); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("invalid tenant kind error = %v, want ErrInvalidEnvelope", err)
	}
}

func testBinding() Binding {
	return Binding{
		TenantKind: "team", TenantID: "team-1", ProjectID: "project-1", CanvasID: "canvas-1",
		RunID: "run-1", ArtifactRevisionID: "revision-1", TaskID: "task-1",
		TaskType: "video_image_to_video", RequesterID: "user-1",
	}
}
