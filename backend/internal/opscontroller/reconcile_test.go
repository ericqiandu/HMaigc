package opscontroller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"infinite-canvas/backend/internal/opsprotocol"
	"infinite-canvas/backend/internal/opsstate"
)

func TestControllerRestartReattachesMatchingRunner(t *testing.T) {
	fixture := newReconcileFixture(t)
	operation := fixture.startQueuedUpgrade(t)
	fixture.manager.instances[operation.ID] = RunnerInstance{
		ContainerID: "runner-1", OperationID: operation.ID, Generation: 1,
		Digest: fixture.target.Digest, Running: true,
	}
	fixture.writeRunningFacts(t, operation.ID, time.Now().UTC())

	restarted := fixture.newController(t)
	if err := restarted.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fixture.manager.startCount != 0 {
		t.Fatal("restart must not start a second matching Runner")
	}
	projected, err := fixture.store.Operation(operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if projected.Status != opsprotocol.OperationRunning {
		t.Fatalf("expected running projection, got %s", projected.Status)
	}
}

func TestControllerRestartRequiresRecoveryWhenPreparedRunnerCannotBeConfirmed(t *testing.T) {
	fixture := newReconcileFixture(t)
	operation := fixture.startQueuedUpgrade(t)
	if err := fixture.controller.dispatchQueued(context.Background()); err != nil {
		t.Fatal(err)
	}
	delete(fixture.manager.instances, operation.ID)
	fixture.manager.startErr = errors.New("docker daemon unavailable")

	if err := fixture.controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("ownership uncertainty must be persisted instead of retried forever: %v", err)
	}
	projected, err := fixture.store.Operation(operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if projected.Status != opsprotocol.OperationRecoveryRequired || projected.RecoveryAction != opsprotocol.RecoveryRequireOperator {
		t.Fatalf("prepared Runner without provable ownership must require operator recovery: %+v", projected)
	}
}

func TestControllerProjectsResultExactlyOnce(t *testing.T) {
	fixture := newReconcileFixture(t)
	operation := fixture.startQueuedUpgrade(t)
	fixture.writeRunningFacts(t, operation.ID, time.Now().UTC())
	for sequence := uint64(1); sequence <= 3; sequence++ {
		if err := fixture.journal.AppendEvent(opsprotocol.OperationEvent{
			OperationID: operation.ID, Generation: 1, Sequence: sequence,
			Kind: "fact", Stage: opsprotocol.StageOnlinePreflight,
			Stream: "system", Message: "event", CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := fixture.journal.WriteResult(operation.ID, opsprotocol.OperationResult{
		OperationID: operation.ID, Generation: 1, Status: opsprotocol.OperationSucceeded,
		ResultVersion: "v1.0.57", ServiceState: opsprotocol.ServiceTargetOnline,
		ControllerHandoff: opsprotocol.ControllerHandoffUnchanged, CompletedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	projector := NewProjector(fixture.store, fixture.journal, fixture.secret)
	if err := projector.Project(operation.ID); err != nil {
		t.Fatal(err)
	}
	if err := projector.Project(operation.ID); err != nil {
		t.Fatal(err)
	}
	logs, err := fixture.store.OperationLogs(operation.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs.Items) != 3 {
		t.Fatalf("expected three projected event logs exactly once, got %d", len(logs.Items))
	}
}

func TestRecoveryRequiredPhaseReflectsPersistedAction(t *testing.T) {
	if got := phaseForResult(opsprotocol.OperationRecoveryRequired, opsprotocol.RecoveryRestoreBackup); got != "等待恢复已校验备份" {
		t.Fatalf("recovery phase=%q", got)
	}
	if got := phaseForResult(opsprotocol.OperationRecoveryRequired, opsprotocol.RecoveryRequireOperator); got != "等待人工确认恢复动作" {
		t.Fatalf("operator recovery phase=%q", got)
	}
}

func TestControllerEntersRecoveryRequiredWhenRunnerOwnershipUnknown(t *testing.T) {
	fixture := newReconcileFixture(t)
	operation := fixture.startQueuedUpgrade(t)
	fixture.manager.instances[operation.ID] = RunnerInstance{
		ContainerID: "runner-1", OperationID: operation.ID, Generation: 1,
		Digest: fixture.target.Digest, Running: true,
	}
	fixture.writeRunningFacts(t, operation.ID, time.Now().UTC().Add(-2*time.Minute))

	if err := fixture.controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	projected, err := fixture.store.Operation(operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if projected.Status != opsprotocol.OperationRecoveryRequired || projected.ErrorCode != opsprotocol.ErrorStateConflict {
		t.Fatalf("expected explicit recovery_required ownership conflict, got %+v", projected)
	}
}

func TestControllerPersistsSafeBackupRecoveryAfterRunnerDiesDuringDataRewrite(t *testing.T) {
	fixture := newReconcileFixture(t)
	operation := fixture.startQueuedUpgrade(t)
	observedAt := time.Now().UTC().Add(-2 * time.Minute)
	fixture.writeRunningFacts(t, operation.ID, observedAt)
	checkpoint, err := fixture.journal.ReadCheckpoint(operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint.Stage = opsprotocol.StageBackingUp
	checkpoint.StageCompletedAt = &observedAt
	checkpoint.ExpectedVersion = "v1.0.57"
	checkpoint.ServiceState = opsprotocol.ServiceMaintenance
	checkpoint.WritesQuiesced = true
	checkpoint.VerifiedRecoveryPoint = true
	checkpoint.BackupPath = "/var/lib/hmaigc-ops/backups/current-v1.0.55"
	checkpoint.BackupChecksumStatus = "verified"
	checkpoint.Sequence = 1
	if err := fixture.journal.WriteCheckpoint(operation.ID, checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := fixture.journal.AppendEvent(opsprotocol.OperationEvent{
		OperationID: operation.ID, Generation: 1, Sequence: 2,
		Kind: "intent", Stage: opsprotocol.StageStartingTarget,
		Stream: "system", Message: "stage intent persisted", CreatedAt: observedAt,
	}); err != nil {
		t.Fatal(err)
	}

	if err := fixture.controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	recovered, err := fixture.journal.ReadCheckpoint(operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !recovered.DataMigrationStarted || recovered.NextSafeAction != opsprotocol.RecoveryRestoreBackup {
		t.Fatalf("unknown destructive outcome did not preserve safe recovery: %+v", recovered)
	}
	projected, err := fixture.store.Operation(operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if projected.RecoveryAction != opsprotocol.RecoveryRestoreBackup {
		t.Fatalf("recovery projection lost the deterministic restore action: %+v", projected)
	}
}

type reconcileFixture struct {
	root       string
	store      *Store
	journal    *opsstate.Journal
	manager    *recordingRunnerManager
	controller *Controller
	target     ResolvedRunner
	secret     []byte
	stateFile  string
	backupDir  string
}

func newReconcileFixture(t *testing.T) *reconcileFixture {
	t.Helper()
	root := t.TempDir()
	store, err := OpenStore(filepath.Join(root, "controller.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	journal, err := opsstate.NewJournal(root)
	if err != nil {
		t.Fatal(err)
	}
	stateFile := filepath.Join(root, "release", "release.env")
	backupDir := filepath.Join(root, "backups")
	if err := os.MkdirAll(filepath.Dir(stateFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateFile, []byte("CURRENT_VERSION=v1.0.55\nPREVIOUS_VERSION=v1.0.54\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := ResolvedRunner{Version: "v1.0.57", Digest: immutableTestDigest("target")}
	fixture := &reconcileFixture{
		root: root, store: store, journal: journal, target: target,
		manager: &recordingRunnerManager{resolved: target, instances: make(map[string]RunnerInstance)},
		secret:  []byte("0123456789abcdef0123456789abcdef"), stateFile: stateFile, backupDir: backupDir,
	}
	fixture.controller = fixture.newController(t)
	return fixture
}

func (f *reconcileFixture) newController(t *testing.T) *Controller {
	t.Helper()
	controller, err := New(
		f.store, f.journal, f.manager, f.secret,
		StaticReleaseSource{Result: opsprotocol.ReleaseCheck{Status: "ok", LatestVersion: "v1.0.57"}},
		Config{
			StateFile: f.stateFile, BackupDir: f.backupDir, StateVolume: "hmaigc-ops-state",
			ControllerVersion: "v1.0.56", ControllerDigest: immutableTestDigest("controller"),
			HeartbeatTTL: 30 * time.Second, ReportError: func(error) {},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return controller
}

func (f *reconcileFixture) startQueuedUpgrade(t *testing.T) *opsprotocol.Operation {
	t.Helper()
	operation, err := f.controller.StartOperation(opsprotocol.StartOperationRequest{
		Action: opsprotocol.ActionUpgrade, TargetVersion: "v1.0.57",
		ActorUserID: "admin-user", ActorDisplayName: "Administrator",
		IdempotencyKey: "idempotency-upgrade-0001", Confirmation: "UPGRADE v1.0.57",
	})
	if err != nil {
		t.Fatal(err)
	}
	return operation
}

func (f *reconcileFixture) writeRunningFacts(t *testing.T, operationID string, observedAt time.Time) {
	t.Helper()
	token := "fencing-token"
	if err := f.journal.WriteLaunchCommand(f.secret, opsprotocol.RunnerLaunchCommand{
		OperationID: operationID, Generation: 1, FencingToken: token,
		RunnerDigest: f.target.Digest, IssuedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(token))
	if err := f.journal.WriteLease(operationID, opsprotocol.RunnerLease{
		OperationID: operationID, Generation: 1, TokenHash: hex.EncodeToString(hash[:]),
		RunnerDigest: f.target.Digest, AcquiredAt: observedAt, ExpiresAt: observedAt.Add(20 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	checkpoint := opsprotocol.OperationCheckpoint{
		OperationID: operationID, Action: opsprotocol.ActionUpgrade, TargetVersion: "v1.0.57",
		RunnerDigest: f.target.Digest, Generation: 1, FencingTokenHash: hex.EncodeToString(hash[:]),
		Stage: opsprotocol.StageOnlinePreflight, ServiceState: opsprotocol.ServiceCurrentOnline,
		Sequence: 0, UpdatedAt: observedAt,
	}
	if err := f.journal.WriteCheckpoint(operationID, checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := f.journal.WriteHeartbeat(operationID, opsprotocol.RunnerHeartbeat{
		OperationID: operationID, Generation: 1, Stage: checkpoint.Stage,
		ServiceState: checkpoint.ServiceState, ObservedAt: observedAt,
	}); err != nil {
		t.Fatal(err)
	}
}

type recordingRunnerManager struct {
	resolved   ResolvedRunner
	instances  map[string]RunnerInstance
	startCount int
	startErr   error
}

func (m *recordingRunnerManager) Resolve(context.Context, opsprotocol.OperationRequestFile) (ResolvedRunner, error) {
	return m.resolved, nil
}

func (m *recordingRunnerManager) Start(_ context.Context, launch RunnerLaunch) error {
	m.startCount++
	if m.startErr != nil {
		return m.startErr
	}
	m.instances[launch.OperationID] = RunnerInstance{
		ContainerID: "started", OperationID: launch.OperationID, Generation: launch.Generation,
		Digest: launch.ImageDigest, Running: true,
	}
	return nil
}

func (m *recordingRunnerManager) Inspect(_ context.Context, operationID string) (RunnerInstance, error) {
	return m.instances[operationID], nil
}

func (m *recordingRunnerManager) ListByOperation(_ context.Context, operationID string) ([]RunnerInstance, error) {
	instance, exists := m.instances[operationID]
	if !exists {
		return []RunnerInstance{}, nil
	}
	return []RunnerInstance{instance}, nil
}

func immutableTestDigest(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return "ghcr.io/example/hmaigc-ops-controller@sha256:" + hex.EncodeToString(sum[:])
}
