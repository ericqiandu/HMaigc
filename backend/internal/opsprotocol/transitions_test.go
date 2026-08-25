package opsprotocol

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestValidateStatusTransitionAllowsOnlyDeclaredEdges(t *testing.T) {
	t.Parallel()

	allowed := [][2]OperationStatus{
		{OperationQueued, OperationRunning},
		{OperationQueued, OperationCancelling},
		{OperationRunning, OperationCancelling},
		{OperationRunning, OperationRecovering},
		{OperationRunning, OperationSucceeded},
		{OperationRunning, OperationRecoveryRequired},
		{OperationCancelling, OperationCancelled},
		{OperationCancelling, OperationRecovering},
		{OperationRecovering, OperationSucceeded},
		{OperationRecovering, OperationRecoveryRequired},
		{OperationRecoveryRequired, OperationRecovering},
	}
	for _, pair := range allowed {
		if err := ValidateStatusTransition(pair[0], pair[1]); err != nil {
			t.Fatalf("expected %s -> %s: %v", pair[0], pair[1], err)
		}
	}

	denied := [][2]OperationStatus{
		{OperationQueued, OperationSucceeded},
		{OperationCancelling, OperationRunning},
		{OperationSucceeded, OperationRunning},
		{OperationFailed, OperationRecovering},
		{OperationCancelled, OperationRunning},
		{OperationRecoveryRequired, OperationSucceeded},
	}
	for _, pair := range denied {
		if err := ValidateStatusTransition(pair[0], pair[1]); !errors.Is(err, ErrInvalidStatusTransition) {
			t.Fatalf("expected %s -> %s to be rejected, got %v", pair[0], pair[1], err)
		}
	}
}

func TestIsTerminalStatusDistinguishesRecoverableOperations(t *testing.T) {
	t.Parallel()

	for _, status := range []OperationStatus{OperationSucceeded, OperationFailed, OperationCancelled} {
		if !IsTerminalStatus(status) {
			t.Fatalf("expected %s to be terminal", status)
		}
	}
	for _, status := range []OperationStatus{OperationQueued, OperationRunning, OperationCancelling, OperationRecovering, OperationRecoveryRequired} {
		if IsTerminalStatus(status) {
			t.Fatalf("expected %s to remain non-terminal", status)
		}
	}
}

func TestOperationResultJSONPreservesRecoveryFacts(t *testing.T) {
	t.Parallel()

	completedAt := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	exitCode := 0
	input := OperationResult{
		OperationID:       "op-001",
		Generation:        4,
		Status:            OperationSucceeded,
		ResultVersion:     "v1.0.58",
		ControllerVersion: "v1.0.57",
		ServiceState:      ServiceTargetOnline,
		ControllerHandoff: ControllerHandoffRestoredPrevious,
		Warnings:          []OperationWarning{{Code: ErrorControllerHandoffFailed, Message: "旧控制器已恢复"}},
		ErrorCode:         "",
		Error:             "",
		ExitCode:          &exitCode,
		CompletedAt:       completedAt,
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var output OperationResult
	if err := json.Unmarshal(encoded, &output); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input, output) {
		t.Fatalf("input=%+v output=%+v", input, output)
	}
}

func TestOverviewJSONKeepsPublicVerificationIndependent(t *testing.T) {
	t.Parallel()

	checkedAt := time.Date(2026, time.August, 25, 12, 1, 0, 0, time.UTC)
	overview := Overview{
		Release: ReleaseCheck{Status: "ok", CurrentVersion: "v1.0.58", CheckedAt: checkedAt},
		LatestOperation: &Operation{
			ID: "op-upgrade", Action: ActionUpgrade, Status: OperationSucceeded,
			ResultVersion: "v1.0.58", CreatedAt: checkedAt, UpdatedAt: checkedAt,
		},
		PublicVerification: PublicVerification{
			Status: PublicVerificationFailed, OperationID: "op-verify", CheckedAt: &checkedAt,
			ErrorCode: ErrorPublicVerifyFailed, Error: "CDN 连接超时",
		},
		UpdatedAt: checkedAt,
	}
	encoded, err := json.Marshal(overview)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Overview
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.LatestOperation == nil || decoded.LatestOperation.Status != OperationSucceeded {
		t.Fatalf("business result changed: %+v", decoded.LatestOperation)
	}
	if decoded.PublicVerification.Status != PublicVerificationFailed || decoded.PublicVerification.OperationID != "op-verify" {
		t.Fatalf("public verification lost: %+v", decoded.PublicVerification)
	}
}

func TestPublicVerificationJSONHasStableShapeWhenNotRun(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(PublicVerification{Status: PublicVerificationNotRun})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"status", "operationId", "checkedAt", "errorCode", "error"} {
		if _, ok := decoded[field]; !ok {
			t.Fatalf("public verification JSON omitted required field %q: %s", field, encoded)
		}
	}
	if string(decoded["checkedAt"]) != "null" {
		t.Fatalf("expected checkedAt=null, got %s", decoded["checkedAt"])
	}
}
