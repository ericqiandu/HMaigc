package opscontroller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/opsprotocol"
)

func TestControllerPersistsOneExpectedVersionFactForEveryAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		request         opsprotocol.StartOperationRequest
		expectedVersion string
		displayedTarget string
	}{
		{
			name: "upgrade", expectedVersion: "v1.0.57", displayedTarget: "v1.0.57",
			request: operationRequest(opsprotocol.ActionUpgrade, "v1.0.57", "UPGRADE v1.0.57", "expected-upgrade-0001"),
		},
		{
			name: "rollback", expectedVersion: "v1.0.54", displayedTarget: "v1.0.54",
			request: operationRequest(opsprotocol.ActionRollback, "", "ROLLBACK", "expected-rollback-0001"),
		},
		{
			name: "backup", expectedVersion: "v1.0.55",
			request: operationRequest(opsprotocol.ActionBackup, "", "BACKUP", "expected-backup-0001"),
		},
		{
			name: "verify", expectedVersion: "v1.0.55",
			request: operationRequest(opsprotocol.ActionVerify, "", "VERIFY", "expected-verify-0001"),
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newReconcileFixture(t)
			if test.request.Action == opsprotocol.ActionRollback {
				backupPath := filepath.Join(fixture.backupDir, "rollback--v1.0.54")
				createVerifiedBackup(t, backupPath, "v1.0.54")
				state := "CURRENT_VERSION=v1.0.55\nPREVIOUS_VERSION=v1.0.54\nROLLBACK_BACKUP=" + backupPath + "\n"
				if err := os.WriteFile(fixture.stateFile, []byte(state), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			operation, err := fixture.controller.StartOperation(test.request)
			if err != nil {
				t.Fatal(err)
			}
			persisted, err := fixture.journal.ReadRequest(operation.ID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.ExpectedVersion != test.expectedVersion {
				t.Fatalf("expectedVersion=%q want=%q", persisted.ExpectedVersion, test.expectedVersion)
			}
			if test.request.Action == opsprotocol.ActionRollback && persisted.RollbackBackup == "" {
				t.Fatalf("rollback request did not pin its verified recovery point: %+v", persisted)
			}
			if operation.TargetVersion != test.displayedTarget {
				t.Fatalf("displayed target=%q want=%q", operation.TargetVersion, test.displayedTarget)
			}
		})
	}
}

func TestControllerRejectsInstallBecauseFirstDeploymentUsesBootstrap(t *testing.T) {
	t.Parallel()

	fixture := newReconcileFixture(t)
	_, err := fixture.controller.StartOperation(operationRequest(
		opsprotocol.ActionInstall, "v1.0.57", "INSTALL v1.0.57", "install-is-bootstrap-0001",
	))
	if err == nil || !strings.Contains(err.Error(), "bootstrap") {
		t.Fatalf("expected explicit bootstrap-only install rejection, got %v", err)
	}
}

func TestControllerRejectsRollbackWithoutVerifiedRecoveryPoint(t *testing.T) {
	t.Parallel()

	fixture := newReconcileFixture(t)
	_, err := fixture.controller.StartOperation(operationRequest(
		opsprotocol.ActionRollback, "", "ROLLBACK", "rollback-no-backup-0001",
	))
	if err == nil || !strings.Contains(err.Error(), "恢复点") {
		t.Fatalf("expected explicit rollback readiness error, got %v", err)
	}
}

func TestControllerIdempotencyConflictUsesDeterministicOperationID(t *testing.T) {
	fixture := newReconcileFixture(t)
	request := validVerifyRequest("idempotency-key-0001")

	first, err := fixture.controller.StartOperation(request)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := fixture.controller.StartOperation(request)
	if err != nil {
		t.Fatal(err)
	}
	if retry.ID != first.ID || first.ID != operationIDForIdempotency(request.IdempotencyKey) {
		t.Fatalf("unexpected deterministic id: first=%s retry=%s", first.ID, retry.ID)
	}

	conflicting := request
	conflicting.ActorDisplayName = "Different Administrator"
	if _, err := fixture.controller.StartOperation(conflicting); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
}

func TestControllerRecoversIdempotentRequestWhenJournalCommitPrecedesProjection(t *testing.T) {
	t.Parallel()

	fixture := newReconcileFixture(t)
	input := validVerifyRequest("journal-first-idempotency-0001")
	normalized, err := normalizeOperationRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	requestHash, err := operationRequestHash(normalized)
	if err != nil {
		t.Fatal(err)
	}
	persisted := opsprotocol.OperationRequestFile{
		OperationID: operationIDForIdempotency(input.IdempotencyKey), Request: normalized,
		RequestHash: requestHash, CurrentVersion: "v1.0.55", PreviousVersion: "v1.0.54",
		ExpectedVersion: "v1.0.55", RunnerSource: opsprotocol.RunnerSourceCurrent,
		ControllerVersionAtStart: "v1.0.56", CreatedAt: time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC),
	}
	if err := fixture.journal.CreateRequest(persisted); err != nil {
		t.Fatal(err)
	}

	operation, err := fixture.controller.StartOperation(input)
	if err != nil {
		t.Fatal(err)
	}
	if operation.ID != persisted.OperationID || operation.IdempotencyKey != input.IdempotencyKey {
		t.Fatalf("journal-first request was not projected idempotently: %+v", operation)
	}
}

func TestControllerDoesNotImportJournalFirstRequestWhileAnotherOperationIsActive(t *testing.T) {
	fixture := newReconcileFixture(t)
	orphanInput := validVerifyRequest("journal-first-blocked-0001")
	normalized, err := normalizeOperationRequest(orphanInput)
	if err != nil {
		t.Fatal(err)
	}
	requestHash, err := operationRequestHash(normalized)
	if err != nil {
		t.Fatal(err)
	}
	persisted := opsprotocol.OperationRequestFile{
		OperationID: operationIDForIdempotency(orphanInput.IdempotencyKey), Request: normalized,
		RequestHash: requestHash, CurrentVersion: "v1.0.55", PreviousVersion: "v1.0.54",
		ExpectedVersion: "v1.0.55", RunnerSource: opsprotocol.RunnerSourceCurrent,
		ControllerVersionAtStart: "v1.0.56", CreatedAt: time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC),
	}
	if err := fixture.journal.CreateRequest(persisted); err != nil {
		t.Fatal(err)
	}
	activeRequest := persistJournalFirstVerify(
		t, fixture, validVerifyRequest("active-operation-0001"), persisted.CreatedAt.Add(-time.Second),
	)
	if _, _, err := fixture.store.ImportRequest(activeRequest); err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.controller.StartOperation(orphanInput); !errors.Is(err, ErrOperationActive) {
		t.Fatalf("expected active operation to block orphan journal import, got %v", err)
	}
}

func TestControllerRejectsNewRequestWhileUnprojectedJournalRequestIsActive(t *testing.T) {
	fixture := newReconcileFixture(t)
	orphanInput := validVerifyRequest("journal-first-active-0001")
	persistJournalFirstVerify(t, fixture, orphanInput, time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC))

	if _, err := fixture.controller.StartOperation(validVerifyRequest("new-request-blocked-0001")); !errors.Is(err, ErrOperationActive) {
		t.Fatalf("expected unprojected journal request to block a new operation, got %v", err)
	}
}

func TestControllerDoesNotDispatchSecondQueuedOperationWhileFirstIsRunning(t *testing.T) {
	fixture := newReconcileFixture(t)
	first, err := fixture.controller.StartOperation(validVerifyRequest("dispatch-first-0001"))
	if err != nil {
		t.Fatal(err)
	}
	secondInput := validVerifyRequest("dispatch-second-0001")
	secondRequest := persistJournalFirstVerify(t, fixture, secondInput, time.Now().UTC().Add(time.Second))
	if _, _, err := fixture.store.ImportRequest(secondRequest); err != nil {
		t.Fatal(err)
	}

	if err := fixture.controller.dispatchQueued(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := fixture.controller.dispatchQueued(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fixture.manager.startCount != 1 {
		t.Fatalf("expected only the first queued operation to start, got %d Runner starts", fixture.manager.startCount)
	}
	projectedFirst, err := fixture.store.Operation(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if projectedFirst.Status != opsprotocol.OperationRunning {
		t.Fatalf("first operation should remain active, got %+v", projectedFirst)
	}
	projectedSecond, err := fixture.store.Operation(secondRequest.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if projectedSecond.Status != opsprotocol.OperationQueued {
		t.Fatalf("second operation must remain queued while first is active, got %+v", projectedSecond)
	}
}

func TestControllerRejectsConcurrentOperations(t *testing.T) {
	fixture := newReconcileFixture(t)
	if _, err := fixture.controller.StartOperation(validVerifyRequest("idempotency-key-0002")); err != nil {
		t.Fatal(err)
	}
	second := validVerifyRequest("idempotency-key-0003")
	second.Action = opsprotocol.ActionBackup
	second.Confirmation = "BACKUP"
	if _, err := fixture.controller.StartOperation(second); !errors.Is(err, ErrOperationActive) {
		t.Fatalf("expected active operation conflict, got %v", err)
	}
}

func TestControllerDispatchesDigestPinnedRunnerWithoutSecretMetadata(t *testing.T) {
	fixture := newReconcileFixture(t)
	operation := fixture.startQueuedUpgrade(t)
	if err := fixture.controller.dispatchQueued(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fixture.manager.startCount != 1 {
		t.Fatalf("expected one Runner start, got %d", fixture.manager.startCount)
	}
	instance := fixture.manager.instances[operation.ID]
	if instance.Digest != fixture.target.Digest || instance.Generation != 1 {
		t.Fatalf("unexpected Runner ownership: %+v", instance)
	}
	command, err := fixture.journal.ReadLaunchCommandForReconciliation(fixture.secret, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if command.FencingToken == "" || command.RunnerDigest != fixture.target.Digest {
		t.Fatalf("expected signed launch ownership, got %+v", command)
	}
}

func TestControllerPersistsRunnerStartFailureAsDurableTerminalFact(t *testing.T) {
	fixture := newReconcileFixture(t)
	fixture.manager.startErr = errors.New("docker daemon unavailable")
	operation := fixture.startQueuedUpgrade(t)

	if err := fixture.controller.dispatchQueued(context.Background()); err == nil {
		t.Fatal("expected Runner start failure")
	}
	result, err := fixture.journal.ReadResult(operation.ID)
	if err != nil {
		t.Fatalf("Runner start failure must be durable in the journal: %v", err)
	}
	if result.Status != opsprotocol.OperationFailed || result.ErrorCode != opsprotocol.ErrorRunnerStartFailed {
		t.Fatalf("unexpected durable Runner start failure: %+v", result)
	}
	fixture.manager.startErr = nil
	if _, err := fixture.controller.StartOperation(validVerifyRequest("after-runner-start-failure-0001")); err != nil {
		t.Fatalf("terminal Runner start failure must not block the next operation: %v", err)
	}
}

func TestControllerKeepsAmbiguousRunnerStartOutcomeInRecoveryRequired(t *testing.T) {
	fixture := newReconcileFixture(t)
	fixture.manager.startErr = fmt.Errorf("%w: docker inspection unavailable", ErrRunnerStartOutcomeUnknown)
	operation := fixture.startQueuedUpgrade(t)

	if err := fixture.controller.dispatchQueued(context.Background()); !errors.Is(err, ErrRunnerStartOutcomeUnknown) {
		t.Fatalf("expected explicit ambiguous start outcome, got %v", err)
	}
	result, err := fixture.journal.ReadResult(operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != opsprotocol.OperationRecoveryRequired {
		t.Fatalf("ambiguous Runner start must not be projected as terminal failure: %+v", result)
	}
	projected, err := fixture.store.Operation(operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if projected.Status != opsprotocol.OperationRecoveryRequired || projected.RecoveryAction != opsprotocol.RecoveryRequireOperator {
		t.Fatalf("ambiguous Runner ownership must require operator evidence: %+v", projected)
	}
}

func TestOverviewReportsPublicVerificationNotRunBeforeAnyVerifyResult(t *testing.T) {
	fixture := newReconcileFixture(t)
	overview, err := fixture.controller.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if overview.PublicVerification.Status != opsprotocol.PublicVerificationNotRun {
		t.Fatalf("expected not_run, got %+v", overview.PublicVerification)
	}
}

func TestOverviewReportsLatestCompletedVerifyIndependentlyFromLatestBusinessOperation(t *testing.T) {
	fixture := newReconcileFixture(t)
	verifiedAt := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	businessAt := verifiedAt.Add(time.Minute)
	failedExit := 1
	for _, record := range []operationRecord{
		{
			ID: "verify-failed", Action: string(opsprotocol.ActionVerify), Status: string(opsprotocol.OperationFailed),
			Stage: string(opsprotocol.StageCompleted), Phase: "执行失败", ServiceState: string(opsprotocol.ServiceCurrentOnline),
			ErrorCode: string(opsprotocol.ErrorPublicVerifyFailed), Error: "CDN asset timed out", ExitCode: &failedExit,
			ActorUserID: "admin-user", ActorDisplayName: "Administrator", IdempotencyKey: "verify-failed-idempotency",
			RequestHash: "verify-request-hash", CreatedAt: verifiedAt, CompletedAt: &verifiedAt, UpdatedAt: verifiedAt,
		},
		{
			ID: "backup-succeeded", Action: string(opsprotocol.ActionBackup), Status: string(opsprotocol.OperationSucceeded),
			Stage: string(opsprotocol.StageCompleted), Phase: "已完成", ServiceState: string(opsprotocol.ServiceCurrentOnline),
			ActorUserID: "admin-user", ActorDisplayName: "Administrator", IdempotencyKey: "backup-succeeded-idempotency",
			RequestHash: "backup-request-hash", CreatedAt: businessAt, CompletedAt: &businessAt, UpdatedAt: businessAt,
		},
	} {
		if _, _, err := fixture.store.CreateOrGetOperation(record); err != nil {
			t.Fatal(err)
		}
	}

	overview, err := fixture.controller.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if overview.LatestOperation == nil || overview.LatestOperation.ID != "backup-succeeded" {
		t.Fatalf("latest business operation changed: %+v", overview.LatestOperation)
	}
	verification := overview.PublicVerification
	if verification.Status != opsprotocol.PublicVerificationFailed || verification.OperationID != "verify-failed" ||
		verification.CheckedAt == nil || !verification.CheckedAt.Equal(verifiedAt) || verification.Error != "CDN asset timed out" {
		t.Fatalf("latest completed verification was not projected independently: %+v", verification)
	}
}

func validVerifyRequest(idempotencyKey string) opsprotocol.StartOperationRequest {
	return operationRequest(opsprotocol.ActionVerify, "", "VERIFY", idempotencyKey)
}

func operationRequest(action opsprotocol.Action, targetVersion, confirmation, idempotencyKey string) opsprotocol.StartOperationRequest {
	return opsprotocol.StartOperationRequest{
		Action: action, TargetVersion: targetVersion, ActorUserID: "admin-user",
		ActorDisplayName: "Administrator", IdempotencyKey: idempotencyKey,
		Confirmation: confirmation,
	}
}

func persistJournalFirstVerify(
	t *testing.T,
	fixture *reconcileFixture,
	input opsprotocol.StartOperationRequest,
	createdAt time.Time,
) opsprotocol.OperationRequestFile {
	t.Helper()
	normalized, err := normalizeOperationRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	requestHash, err := operationRequestHash(normalized)
	if err != nil {
		t.Fatal(err)
	}
	persisted := opsprotocol.OperationRequestFile{
		OperationID: operationIDForIdempotency(input.IdempotencyKey), Request: normalized,
		RequestHash: requestHash, CurrentVersion: "v1.0.55", PreviousVersion: "v1.0.54",
		ExpectedVersion: "v1.0.55", RunnerSource: opsprotocol.RunnerSourceCurrent,
		ControllerVersionAtStart: "v1.0.56", CreatedAt: createdAt,
	}
	if err := fixture.journal.CreateRequest(persisted); err != nil {
		t.Fatal(err)
	}
	return persisted
}
