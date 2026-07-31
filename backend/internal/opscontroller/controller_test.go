package opscontroller

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/opsprotocol"
)

type recordingExecutor struct {
	mu      sync.Mutex
	actions []opsprotocol.Action
	err     error
}

func (e *recordingExecutor) Execute(_ context.Context, action opsprotocol.Action, _ string, appendLog func(string, string)) (ExecutionResult, error) {
	e.mu.Lock()
	e.actions = append(e.actions, action)
	e.mu.Unlock()
	appendLog("stdout", "executor output")
	if e.err != nil {
		return ExecutionResult{ExitCode: 17}, e.err
	}
	return ExecutionResult{ExitCode: 0}, nil
}

func TestControllerIdempotencyConflictAndPersistedLogs(t *testing.T) {
	t.Parallel()

	controller, store, executor := newTestController(t)
	request := validVerifyRequest("idempotency-key-0001")

	first, err := controller.StartOperation(request)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := controller.StartOperation(request)
	if err != nil {
		t.Fatal(err)
	}
	if retry.ID != first.ID {
		t.Fatalf("expected idempotent retry to return %s, got %s", first.ID, retry.ID)
	}

	conflicting := request
	conflicting.ActorDisplayName = "Different Administrator"
	if _, err := controller.StartOperation(conflicting); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}

	if err := controller.executeNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	completed, err := store.Operation(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != opsprotocol.OperationSucceeded {
		t.Fatalf("expected succeeded status, got %s", completed.Status)
	}
	logs, err := store.OperationLogs(first.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs.Items) < 4 {
		t.Fatalf("expected audit and execution logs, got %d", len(logs.Items))
	}
	executor.mu.Lock()
	defer executor.mu.Unlock()
	if len(executor.actions) != 1 || executor.actions[0] != opsprotocol.ActionVerify {
		t.Fatalf("unexpected executor actions: %v", executor.actions)
	}
}

func TestControllerRejectsConcurrentOperations(t *testing.T) {
	t.Parallel()

	controller, _, _ := newTestController(t)
	if _, err := controller.StartOperation(validVerifyRequest("idempotency-key-0002")); err != nil {
		t.Fatal(err)
	}
	second := validVerifyRequest("idempotency-key-0003")
	second.Action = opsprotocol.ActionBackup
	second.Confirmation = "BACKUP"
	if _, err := controller.StartOperation(second); !errors.Is(err, ErrOperationActive) {
		t.Fatalf("expected active operation conflict, got %v", err)
	}
}

func TestControllerMarksExecutionFailure(t *testing.T) {
	t.Parallel()

	controller, store, executor := newTestController(t)
	executor.err = errors.New("synthetic execution failure")
	operation, err := controller.StartOperation(validVerifyRequest("idempotency-key-0004"))
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.executeNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	failed, err := store.Operation(operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != opsprotocol.OperationFailed || failed.ExitCode == nil || *failed.ExitCode != 17 {
		t.Fatalf("expected explicit failed operation with exit code 17: %+v", failed)
	}
	if failed.Error == "" {
		t.Fatal("expected persisted failure reason")
	}
}

func newTestController(t *testing.T) (*Controller, *Store, *recordingExecutor) {
	t.Helper()
	root := t.TempDir()
	stateDir := filepath.Join(root, "release")
	backupDir := filepath.Join(root, "backups")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	stateFile := filepath.Join(stateDir, "release.env")
	state := "CURRENT_VERSION=v1.0.11\nPREVIOUS_VERSION=\nROLLBACK_BACKUP=\nUPDATED_AT=" + time.Now().UTC().Format(time.RFC3339) + "\n"
	if err := os.WriteFile(stateFile, []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(filepath.Join(root, "controller.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	executor := &recordingExecutor{}
	controller, err := New(
		store,
		executor,
		StaticReleaseSource{Result: opsprotocol.ReleaseCheck{Status: "ok", LatestVersion: "v1.0.11"}},
		Config{StateFile: stateFile, BackupDir: backupDir},
	)
	if err != nil {
		t.Fatal(err)
	}
	return controller, store, executor
}

func validVerifyRequest(idempotencyKey string) opsprotocol.StartOperationRequest {
	return opsprotocol.StartOperationRequest{
		Action:           opsprotocol.ActionVerify,
		ActorUserID:      "admin-user",
		ActorDisplayName: "Administrator",
		IdempotencyKey:   idempotencyKey,
		Confirmation:     "VERIFY",
	}
}
