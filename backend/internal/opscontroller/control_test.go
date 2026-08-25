package opscontroller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"infinite-canvas/backend/internal/opsprotocol"
)

func TestControlHTTPStatusContract(t *testing.T) {
	fixture := newReconcileFixture(t)
	handler, err := NewHTTPHandler(fixture.controller, fixture.secret)
	if err != nil {
		t.Fatal(err)
	}
	operation := fixture.startQueuedUpgrade(t)

	unknown := signedControlRequest(t, fixture.secret, http.MethodPost, "/v1/operations/missing/cancel", validCancelRequest("missing", "cancel-http-missing-0001"), "nonce-missing")
	unknownResponse := httptest.NewRecorder()
	handler.ServeHTTP(unknownResponse, unknown)
	if unknownResponse.Code != http.StatusNotFound {
		t.Fatalf("unknown operation status=%d body=%s", unknownResponse.Code, unknownResponse.Body.String())
	}

	invalid := validCancelRequest(operation.ID, "cancel-http-invalid-0001")
	invalid.Confirmation = "STOP wrong"
	invalidRequest := signedControlRequest(t, fixture.secret, http.MethodPost, "/v1/operations/"+operation.ID+"/cancel", invalid, "nonce-invalid")
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalidRequest)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid confirmation status=%d body=%s", invalidResponse.Code, invalidResponse.Body.String())
	}

	recover := signedControlRequest(t, fixture.secret, http.MethodPost, "/v1/operations/"+operation.ID+"/recover", validRecoverRequest(operation.ID, "recover-http-conflict-0001"), "nonce-recover")
	recoverResponse := httptest.NewRecorder()
	handler.ServeHTTP(recoverResponse, recover)
	if recoverResponse.Code != http.StatusConflict {
		t.Fatalf("illegal recovery status=%d body=%s", recoverResponse.Code, recoverResponse.Body.String())
	}
}

func TestCancelOperationWritesSignedCommandAndMovesToCancelling(t *testing.T) {
	fixture := newReconcileFixture(t)
	operation := fixture.startQueuedUpgrade(t)
	if err := fixture.controller.dispatchQueued(context.Background()); err != nil {
		t.Fatal(err)
	}
	request := validCancelRequest(operation.ID, "cancel-command-0001")

	result, err := fixture.controller.CancelOperation(operation.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != opsprotocol.OperationCancelling || result.CancelRequestedAt == nil {
		t.Fatalf("expected cancelling with durable request time, got %+v", result)
	}
	command, err := fixture.journal.ReadCancelCommand(fixture.secret, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if command.OperationID != operation.ID || command.ActorUserID != request.ActorUserID || command.RequestHash == "" {
		t.Fatalf("unexpected signed cancel command: %+v", command)
	}

	retry, err := fixture.controller.CancelOperation(operation.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	if retry.Status != opsprotocol.OperationCancelling {
		t.Fatalf("duplicate cancel must remain idempotent, got %+v", retry)
	}
	if err := fixture.journal.WriteHeartbeat(operation.ID, opsprotocol.RunnerHeartbeat{
		OperationID: operation.ID, Generation: 1, Stage: opsprotocol.StageOnlinePreflight,
		ServiceState: opsprotocol.ServiceCurrentOnline, ObservedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.controller.projector.Project(operation.ID); err != nil {
		t.Fatal(err)
	}
	projected, err := fixture.store.Operation(operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if projected.Status != opsprotocol.OperationCancelling {
		t.Fatalf("heartbeat projection must preserve cancelling, got %+v", projected)
	}
	conflict := request
	conflict.IdempotencyKey = "cancel-command-0002"
	if _, err := fixture.controller.CancelOperation(operation.ID, conflict); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected cancel idempotency conflict, got %v", err)
	}
}

func TestCancelQueuedOperationCompletesWithoutStartingRunner(t *testing.T) {
	fixture := newReconcileFixture(t)
	operation := fixture.startQueuedUpgrade(t)

	result, err := fixture.controller.CancelOperation(operation.ID, validCancelRequest(operation.ID, "cancel-command-queued-0001"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != opsprotocol.OperationCancelled || fixture.manager.startCount != 0 {
		t.Fatalf("queued cancel must be terminal without Runner: operation=%+v starts=%d", result, fixture.manager.startCount)
	}
}

func TestRecoverRequiresRecoveryRequiredStatus(t *testing.T) {
	fixture := newReconcileFixture(t)
	operation := fixture.startQueuedUpgrade(t)

	_, err := fixture.controller.RecoverOperation(context.Background(), operation.ID, validRecoverRequest(operation.ID, "recover-command-0001"))
	if !errors.Is(err, ErrRecoveryNotAllowed) {
		t.Fatalf("expected recovery state conflict, got %v", err)
	}
}

func TestRecoverStartsNewGenerationOnlyAfterPriorRunnerStopped(t *testing.T) {
	fixture := newReconcileFixture(t)
	operation := fixture.startQueuedUpgrade(t)
	if err := fixture.controller.dispatchQueued(context.Background()); err != nil {
		t.Fatal(err)
	}
	command, err := fixture.journal.ReadLaunchCommandForReconciliation(fixture.secret, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	tokenHash := sha256.Sum256([]byte(command.FencingToken))
	now := time.Now().UTC()
	if err := fixture.journal.WriteLease(operation.ID, opsprotocol.RunnerLease{
		OperationID: operation.ID, Generation: 1, TokenHash: hex.EncodeToString(tokenHash[:]),
		RunnerDigest: command.RunnerDigest, AcquiredAt: now.Add(-time.Minute), ExpiresAt: now.Add(-30 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.journal.WriteCheckpoint(operation.ID, opsprotocol.OperationCheckpoint{
		OperationID: operation.ID, Action: opsprotocol.ActionUpgrade, TargetVersion: "v1.0.57",
		RunnerDigest: command.RunnerDigest, Generation: 1, FencingTokenHash: hex.EncodeToString(tokenHash[:]),
		Stage: opsprotocol.StageQuiescedAudit, ServiceState: opsprotocol.ServiceMaintenance,
		WritesQuiesced: true, NextSafeAction: opsprotocol.RecoveryRestoreCurrent, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.journal.WriteResult(operation.ID, opsprotocol.OperationResult{
		OperationID: operation.ID, Generation: 1, Status: opsprotocol.OperationRecoveryRequired,
		ServiceState: opsprotocol.ServiceMaintenance, ControllerHandoff: opsprotocol.ControllerHandoffUnchanged,
		ErrorCode: opsprotocol.ErrorStateConflict, Error: "Runner interrupted", CompletedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.controller.projector.Project(operation.ID); err != nil {
		t.Fatal(err)
	}
	fixture.manager.instances[operation.ID] = RunnerInstance{
		ContainerID: "old-runner", OperationID: operation.ID, Generation: 1,
		Digest: command.RunnerDigest, Running: false,
	}

	result, err := fixture.controller.RecoverOperation(context.Background(), operation.ID, validRecoverRequest(operation.ID, "recover-command-0002"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != opsprotocol.OperationRecovering || result.RunnerGeneration != 2 {
		t.Fatalf("expected generation 2 recovery, got %+v", result)
	}
	if fixture.manager.startCount != 2 {
		t.Fatalf("expected one initial and one recovery Runner start, got %d", fixture.manager.startCount)
	}
	recoveryCommand, err := fixture.journal.ReadLaunchCommandForReconciliation(fixture.secret, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recoveryCommand.Generation != 2 || recoveryCommand.FencingToken == command.FencingToken {
		t.Fatalf("expected fresh recovery fencing generation, got %+v", recoveryCommand)
	}
	if err := fixture.controller.projector.Project(operation.ID); err != nil {
		t.Fatal(err)
	}
	projected, err := fixture.store.Operation(operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if projected.Status != opsprotocol.OperationRecovering || projected.RunnerGeneration != 2 {
		t.Fatalf("stale generation facts must not overwrite recovery ownership, got %+v", projected)
	}
}

func TestRecoveryRunnerStartFailureKeepsExecutableRecoveryActionForRetry(t *testing.T) {
	fixture := newReconcileFixture(t)
	operation := prepareRecoverableUpgrade(t, fixture)
	fixture.manager.startErr = errors.New("docker daemon unavailable")
	request := validRecoverRequest(operation.ID, "recover-retryable-start-0001")

	if _, err := fixture.controller.RecoverOperation(context.Background(), operation.ID, request); err == nil {
		t.Fatal("expected recovery Runner start failure")
	}
	failed, err := fixture.store.Operation(operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != opsprotocol.OperationRecoveryRequired || failed.RecoveryAction != opsprotocol.RecoveryRestoreCurrent {
		t.Fatalf("recovery start failure must preserve the executable action: %+v", failed)
	}

	fixture.manager.startErr = nil
	retried, err := fixture.controller.RecoverOperation(context.Background(), operation.ID, request)
	if err != nil {
		t.Fatalf("same signed recovery request should be retryable after infrastructure repair: %v", err)
	}
	if retried.Status != opsprotocol.OperationRecovering || retried.RunnerGeneration != 2 {
		t.Fatalf("unexpected recovery retry projection: %+v", retried)
	}
}

func TestRecoverRejectsWhenPriorRunnerCanStillMutateProduction(t *testing.T) {
	fixture := newReconcileFixture(t)
	operation := fixture.startQueuedUpgrade(t)
	if err := fixture.store.MarkRecoveryRequired(
		operation.ID, operation.Status, operation.Stage, opsprotocol.RecoveryRequireOperator,
		"ownership unknown", time.Now().UTC(),
	); err != nil {
		t.Fatal(err)
	}
	fixture.manager.instances[operation.ID] = RunnerInstance{
		ContainerID: "still-running", OperationID: operation.ID, Generation: 1,
		Digest: fixture.target.Digest, Running: true,
	}

	_, err := fixture.controller.RecoverOperation(context.Background(), operation.ID, validRecoverRequest(operation.ID, "recover-command-0003"))
	if !errors.Is(err, ErrRecoveryNotAllowed) {
		t.Fatalf("expected ownership conflict, got %v", err)
	}
	if fixture.manager.startCount != 0 {
		t.Fatalf("recovery must not start while prior Runner is alive, got %d starts", fixture.manager.startCount)
	}
}

func validCancelRequest(operationID string, idempotencyKey string) opsprotocol.CancelOperationRequest {
	return opsprotocol.CancelOperationRequest{
		ActorUserID: "admin-user", ActorDisplayName: "Administrator",
		IdempotencyKey: idempotencyKey, Confirmation: "STOP " + operationID,
	}
}

func prepareRecoverableUpgrade(t *testing.T, fixture *reconcileFixture) *opsprotocol.Operation {
	t.Helper()
	operation := fixture.startQueuedUpgrade(t)
	if err := fixture.controller.dispatchQueued(context.Background()); err != nil {
		t.Fatal(err)
	}
	command, err := fixture.journal.ReadLaunchCommandForReconciliation(fixture.secret, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	tokenHash := sha256.Sum256([]byte(command.FencingToken))
	now := time.Now().UTC()
	if err := fixture.journal.WriteLease(operation.ID, opsprotocol.RunnerLease{
		OperationID: operation.ID, Generation: 1, TokenHash: hex.EncodeToString(tokenHash[:]),
		RunnerDigest: command.RunnerDigest, AcquiredAt: now.Add(-time.Minute), ExpiresAt: now.Add(-30 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.journal.WriteCheckpoint(operation.ID, opsprotocol.OperationCheckpoint{
		OperationID: operation.ID, Action: opsprotocol.ActionUpgrade, TargetVersion: "v1.0.57",
		RunnerDigest: command.RunnerDigest, Generation: 1, FencingTokenHash: hex.EncodeToString(tokenHash[:]),
		Stage: opsprotocol.StageQuiescedAudit, ServiceState: opsprotocol.ServiceMaintenance,
		WritesQuiesced: true, NextSafeAction: opsprotocol.RecoveryRestoreCurrent, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.journal.WriteResult(operation.ID, opsprotocol.OperationResult{
		OperationID: operation.ID, Generation: 1, Status: opsprotocol.OperationRecoveryRequired,
		ServiceState: opsprotocol.ServiceMaintenance, ControllerHandoff: opsprotocol.ControllerHandoffUnchanged,
		ErrorCode: opsprotocol.ErrorStateConflict, Error: "Runner interrupted", CompletedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.controller.projector.Project(operation.ID); err != nil {
		t.Fatal(err)
	}
	fixture.manager.instances[operation.ID] = RunnerInstance{
		ContainerID: "old-runner", OperationID: operation.ID, Generation: 1,
		Digest: command.RunnerDigest, Running: false,
	}
	return operation
}

func TestCancelRejectsMismatchedConfirmation(t *testing.T) {
	fixture := newReconcileFixture(t)
	operation := fixture.startQueuedUpgrade(t)
	request := validCancelRequest(operation.ID, "cancel-command-invalid-0001")
	request.Confirmation = "STOP another-operation"

	_, err := fixture.controller.CancelOperation(operation.ID, request)
	var requestError *RequestError
	if !errors.As(err, &requestError) {
		t.Fatalf("expected explicit invalid confirmation error, got %v", err)
	}
}

func validRecoverRequest(operationID string, idempotencyKey string) opsprotocol.RecoverOperationRequest {
	return opsprotocol.RecoverOperationRequest{
		ActorUserID: "admin-user", ActorDisplayName: "Administrator",
		IdempotencyKey: idempotencyKey, Confirmation: "RECOVER " + operationID,
	}
}

func signedControlRequest(t *testing.T, secret []byte, method string, path string, input interface{}, nonce string) *http.Request {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set(opsprotocol.HeaderTimestamp, timestamp)
	request.Header.Set(opsprotocol.HeaderNonce, nonce)
	request.Header.Set(opsprotocol.HeaderSignature, opsprotocol.Signature(secret, method, path, timestamp, nonce, body))
	return request
}
