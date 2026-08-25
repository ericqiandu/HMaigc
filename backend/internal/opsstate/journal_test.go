package opsstate

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"infinite-canvas/backend/internal/opsprotocol"
)

func TestJournalPersistsImmutableRequestAndEvents(t *testing.T) {
	t.Parallel()

	journal := newTestJournal(t)
	request := opsprotocol.OperationRequestFile{
		OperationID: "op-001",
		Request: opsprotocol.StartOperationRequest{
			Action: opsprotocol.ActionUpgrade, TargetVersion: "v1.0.58",
			ActorUserID: "admin", ActorDisplayName: "管理员", IdempotencyKey: "idem-001", Confirmation: "UPGRADE v1.0.58",
		},
		RequestHash: strings.Repeat("a", 64), ExpectedVersion: "v1.0.58",
		RunnerSource:             opsprotocol.RunnerSourceTarget,
		ControllerVersionAtStart: "v1.0.57", CreatedAt: time.Now().UTC(),
	}
	if err := journal.CreateRequest(request); err != nil {
		t.Fatal(err)
	}
	if err := journal.CreateRequest(request); err != nil {
		t.Fatalf("same immutable request must be idempotent: %v", err)
	}
	conflict := request
	conflict.RequestHash = strings.Repeat("b", 64)
	if err := journal.CreateRequest(conflict); !errors.Is(err, ErrImmutableFactExists) {
		t.Fatalf("expected immutable conflict, got %v", err)
	}

	event := opsprotocol.OperationEvent{
		OperationID: "op-001", Generation: 1, Sequence: 1, Kind: "fact",
		Stage: opsprotocol.StageOnlinePreflight, Stream: "system", Message: "预检完成", CreatedAt: time.Now().UTC(),
	}
	if err := journal.AppendEvent(event); err != nil {
		t.Fatal(err)
	}
	if err := journal.AppendEvent(event); !errors.Is(err, ErrImmutableFactExists) {
		t.Fatalf("expected duplicate event rejection, got %v", err)
	}
	events, err := journal.ReadEvents("op-001", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Sequence != 1 || events[0].Message != "预检完成" {
		t.Fatalf("events=%+v", events)
	}
}

func TestJournalRejectsRequestWithoutExpectedVersion(t *testing.T) {
	t.Parallel()

	journal := newTestJournal(t)
	request := opsprotocol.OperationRequestFile{
		OperationID: "op-missing-expected",
		Request: opsprotocol.StartOperationRequest{
			Action: opsprotocol.ActionVerify, ActorUserID: "admin", ActorDisplayName: "管理员",
			IdempotencyKey: "missing-expected-0001", Confirmation: "VERIFY",
		},
		RequestHash: strings.Repeat("a", 64), RunnerSource: opsprotocol.RunnerSourceCurrent,
		ControllerVersionAtStart: "v1.0.57", CreatedAt: time.Now().UTC(),
	}
	if err := journal.CreateRequest(request); err == nil || !strings.Contains(err.Error(), "expected version") {
		t.Fatalf("expected missing expected version rejection, got %v", err)
	}
}

func TestJournalRejectsDivergentExpectedVersionContract(t *testing.T) {
	t.Parallel()

	base := opsprotocol.OperationRequestFile{
		OperationID: "op-version-contract",
		Request: opsprotocol.StartOperationRequest{
			Action: opsprotocol.ActionUpgrade, TargetVersion: "v1.0.58",
			ActorUserID: "admin", ActorDisplayName: "管理员",
			IdempotencyKey: "version-contract-0001", Confirmation: "UPGRADE v1.0.58",
		},
		RequestHash: strings.Repeat("a", 64), CurrentVersion: "v1.0.57",
		ExpectedVersion: "v1.0.58", RunnerSource: opsprotocol.RunnerSourceTarget,
		ControllerVersionAtStart: "v1.0.57", CreatedAt: time.Now().UTC(),
	}
	tests := []struct {
		name   string
		mutate func(*opsprotocol.OperationRequestFile)
	}{
		{name: "upgrade expected differs from target", mutate: func(request *opsprotocol.OperationRequestFile) {
			request.ExpectedVersion = "v1.0.59"
		}},
		{name: "upgrade runner is not target", mutate: func(request *opsprotocol.OperationRequestFile) {
			request.RunnerSource = opsprotocol.RunnerSourceCurrent
		}},
		{name: "rollback expected differs from previous", mutate: func(request *opsprotocol.OperationRequestFile) {
			request.Request.Action = opsprotocol.ActionRollback
			request.Request.TargetVersion = ""
			request.Request.Confirmation = "ROLLBACK"
			request.PreviousVersion = "v1.0.56"
			request.RunnerSource = opsprotocol.RunnerSourceCurrent
		}},
		{name: "rollback recovery point is missing", mutate: func(request *opsprotocol.OperationRequestFile) {
			request.Request.Action = opsprotocol.ActionRollback
			request.Request.TargetVersion = ""
			request.Request.Confirmation = "ROLLBACK"
			request.ExpectedVersion = "v1.0.56"
			request.PreviousVersion = "v1.0.56"
			request.RunnerSource = opsprotocol.RunnerSourceCurrent
		}},
		{name: "upgrade carries a rollback recovery point", mutate: func(request *opsprotocol.OperationRequestFile) {
			request.RollbackBackup = "/var/lib/hmaigc-ops/backups/old"
		}},
		{name: "verify expected differs from current", mutate: func(request *opsprotocol.OperationRequestFile) {
			request.Request.Action = opsprotocol.ActionVerify
			request.Request.TargetVersion = ""
			request.Request.Confirmation = "VERIFY"
			request.RunnerSource = opsprotocol.RunnerSourceCurrent
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := base
			test.mutate(&request)
			journal := newTestJournal(t)
			if err := journal.CreateRequest(request); err == nil {
				t.Fatalf("divergent request was accepted: %+v", request)
			}
		})
	}
}

func TestJournalRejectsTraversalAndCorruptMutableFact(t *testing.T) {
	t.Parallel()

	journal := newTestJournal(t)
	if _, err := journal.Layout().OperationDir("../escape"); !errors.Is(err, ErrInvalidOperationID) {
		t.Fatalf("expected path rejection, got %v", err)
	}
	checkpointPath, err := journal.Layout().CheckpointPath("op-002")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(checkpointPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(checkpointPath, []byte(`{"stage":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.ReadCheckpoint("op-002"); !errors.Is(err, ErrCorruptFact) {
		t.Fatalf("expected corrupt fact error, got %v", err)
	}
}

func TestJournalVerifiesCommandEnvelopeBeforePayloadDecode(t *testing.T) {
	t.Parallel()

	journal := newTestJournal(t)
	secret := []byte(strings.Repeat("s", 32))
	command := opsprotocol.RunnerLaunchCommand{
		OperationID: "op-003", Generation: 7, FencingToken: "raw-secret-token",
		RunnerDigest: "example.invalid/runner@sha256:" + strings.Repeat("c", 64), IssuedAt: time.Now().UTC(),
	}
	if err := journal.WriteLaunchCommand(secret, command); err != nil {
		t.Fatal(err)
	}
	read, err := journal.ReadLaunchCommand(secret, "op-003")
	if err != nil {
		t.Fatal(err)
	}
	if read.Generation != 7 || read.FencingToken != "raw-secret-token" {
		t.Fatalf("command=%+v", read)
	}

	path, err := journal.Layout().LaunchCommandPath("op-003")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(encoded), "raw-secret-token", "tampered-token!", 1)
	if tampered == string(encoded) {
		t.Fatal("fixture did not alter signed payload")
	}
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.ReadLaunchCommand(secret, "op-003"); !errors.Is(err, ErrInvalidCommandSignature) {
		t.Fatalf("expected signature failure before trusted decode, got %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("launch command mode=%o", info.Mode().Perm())
		}
	}
}

func TestJournalRejectsLaunchReplayAndAllowsHigherGeneration(t *testing.T) {
	t.Parallel()

	journal := newTestJournal(t)
	secret := []byte(strings.Repeat("g", 32))
	issuedAt := time.Now().UTC()
	command := opsprotocol.RunnerLaunchCommand{
		OperationID: "op-005", Generation: 3, FencingToken: "generation-three",
		RunnerDigest: "example.invalid/runner@sha256:" + strings.Repeat("d", 64), IssuedAt: issuedAt,
	}
	if err := journal.WriteLaunchCommand(secret, command); err != nil {
		t.Fatal(err)
	}
	if err := journal.WriteLaunchCommand(secret, command); err != nil {
		t.Fatalf("same launch command must be idempotent: %v", err)
	}
	replayed := command
	replayed.FencingToken = "replayed-generation"
	if err := journal.WriteLaunchCommand(secret, replayed); !errors.Is(err, ErrImmutableFactExists) {
		t.Fatalf("expected generation replay rejection, got %v", err)
	}
	next := command
	next.Generation = 4
	next.FencingToken = "generation-four"
	if err := journal.WriteLaunchCommand(secret, next); err != nil {
		t.Fatalf("write next generation: %v", err)
	}
	read, err := journal.ReadLaunchCommand(secret, command.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if read.Generation != next.Generation || read.FencingToken != next.FencingToken {
		t.Fatalf("launch command=%+v", read)
	}
}

func TestJournalAtomicallyReplacesMutableFacts(t *testing.T) {
	t.Parallel()

	journal := newTestJournal(t)
	first := opsprotocol.OperationCheckpoint{
		OperationID: "op-006", Generation: 1, Sequence: 1,
		Stage: opsprotocol.StageOnlinePreflight, ServiceState: opsprotocol.ServiceCurrentOnline,
		UpdatedAt: time.Now().UTC(),
	}
	if err := journal.WriteCheckpoint(first.OperationID, first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.Sequence = 2
	second.Stage = opsprotocol.StagePublicVerifying
	if err := journal.WriteCheckpoint(second.OperationID, second); err != nil {
		t.Fatal(err)
	}
	read, err := journal.ReadCheckpoint(first.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if read.Sequence != 2 || read.Stage != opsprotocol.StagePublicVerifying {
		t.Fatalf("checkpoint=%+v", read)
	}
}

func TestLeaseFencingRejectsStaleOrConflictingOwner(t *testing.T) {
	t.Parallel()

	journal := newTestJournal(t)
	base := opsprotocol.RunnerLease{
		OperationID: "op-lease", Generation: 2, TokenHash: strings.Repeat("a", 64),
		RunnerDigest: "example.invalid/runner@sha256:" + strings.Repeat("b", 64),
		AcquiredAt:   time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Minute),
	}
	if err := journal.WriteLease(base.OperationID, base); err != nil {
		t.Fatal(err)
	}
	stale := base
	stale.Generation = 1
	if err := journal.WriteLease(base.OperationID, stale); !errors.Is(err, ErrImmutableFactExists) {
		t.Fatalf("stale lease replaced owner: %v", err)
	}
	conflict := base
	conflict.TokenHash = strings.Repeat("c", 64)
	if err := journal.WriteLease(base.OperationID, conflict); !errors.Is(err, ErrImmutableFactExists) {
		t.Fatalf("conflicting lease replaced owner: %v", err)
	}
	next := base
	next.Generation = 3
	next.TokenHash = strings.Repeat("d", 64)
	if err := journal.WriteLease(base.OperationID, next); err != nil {
		t.Fatalf("new fenced generation was rejected: %v", err)
	}
}

func TestJournalRejectsCheckpointRegressionAndResultOverwrite(t *testing.T) {
	t.Parallel()

	journal := newTestJournal(t)
	checkpoint := opsprotocol.OperationCheckpoint{
		OperationID: "op-009", Generation: 2, FencingTokenHash: strings.Repeat("a", 64),
		Sequence: 5, Stage: opsprotocol.StageBackingUp,
		ServiceState: opsprotocol.ServiceMaintenance, UpdatedAt: time.Now().UTC(),
	}
	if err := journal.WriteCheckpoint(checkpoint.OperationID, checkpoint); err != nil {
		t.Fatal(err)
	}
	regressed := checkpoint
	regressed.Sequence = 4
	regressed.Stage = opsprotocol.StageQuiescedAudit
	if err := journal.WriteCheckpoint(regressed.OperationID, regressed); !errors.Is(err, ErrImmutableFactExists) {
		t.Fatalf("expected checkpoint regression rejection, got %v", err)
	}
	readCheckpoint, err := journal.ReadCheckpoint(checkpoint.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if readCheckpoint.Sequence != checkpoint.Sequence || readCheckpoint.Stage != checkpoint.Stage {
		t.Fatalf("checkpoint was overwritten: %+v", readCheckpoint)
	}

	result := opsprotocol.OperationResult{
		OperationID: checkpoint.OperationID, Generation: checkpoint.Generation,
		Status: opsprotocol.OperationFailed, ResultVersion: "v1.0.57",
		ServiceState:      opsprotocol.ServiceCurrentRestored,
		ControllerHandoff: opsprotocol.ControllerHandoffUnchanged,
		CompletedAt:       time.Now().UTC(),
	}
	if err := journal.WriteResult(result.OperationID, result); err != nil {
		t.Fatal(err)
	}
	if err := journal.WriteResult(result.OperationID, result); err != nil {
		t.Fatalf("same result must be idempotent: %v", err)
	}
	overwrite := result
	overwrite.Status = opsprotocol.OperationSucceeded
	if err := journal.WriteResult(overwrite.OperationID, overwrite); !errors.Is(err, ErrImmutableFactExists) {
		t.Fatalf("expected final result overwrite rejection, got %v", err)
	}
	overwrite.Generation++
	if err := journal.WriteResult(overwrite.OperationID, overwrite); !errors.Is(err, ErrImmutableFactExists) {
		t.Fatalf("terminal operation must reject a later generation, got %v", err)
	}

	recoverable := result
	recoverable.OperationID = "op-010"
	recoverable.Status = opsprotocol.OperationRecoveryRequired
	if err := journal.WriteResult(recoverable.OperationID, recoverable); err != nil {
		t.Fatal(err)
	}
	recovered := recoverable
	recovered.Generation++
	recovered.Status = opsprotocol.OperationFailed
	recovered.ServiceState = opsprotocol.ServiceCurrentRestored
	if err := journal.WriteResult(recovered.OperationID, recovered); err != nil {
		t.Fatalf("higher recovery generation must replace recovery_required: %v", err)
	}
}

func TestJournalRejectsExpiredLaunchCommand(t *testing.T) {
	t.Parallel()

	journal := newTestJournal(t)
	secret := []byte(strings.Repeat("e", 32))
	command := opsprotocol.RunnerLaunchCommand{
		OperationID: "op-007", Generation: 1, FencingToken: "expired-token",
		RunnerDigest: "example.invalid/runner@sha256:" + strings.Repeat("f", 64),
		IssuedAt:     time.Now().UTC().Add(-31 * time.Minute),
	}
	if err := journal.WriteLaunchCommand(secret, command); !errors.Is(err, ErrCommandExpired) {
		t.Fatalf("expected expired command rejection, got %v", err)
	}
}

func TestJournalCanRotatePastExpiredSignedLaunchGeneration(t *testing.T) {
	t.Parallel()

	journal := newTestJournal(t)
	secret := []byte(strings.Repeat("r", 32))
	expired := opsprotocol.RunnerLaunchCommand{
		OperationID: "op-008", Generation: 1, FencingToken: "expired-generation",
		RunnerDigest: "example.invalid/runner@sha256:" + strings.Repeat("1", 64),
		IssuedAt:     time.Now().UTC().Add(-31 * time.Minute),
	}
	encoded, err := encodeSignedCommand(secret, expired)
	if err != nil {
		t.Fatal(err)
	}
	path, err := journal.Layout().LaunchCommandPath(expired.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeBytesAtomic(path, 0o600, encoded); err != nil {
		t.Fatal(err)
	}

	next := expired
	next.Generation = 2
	next.FencingToken = "fresh-generation"
	next.IssuedAt = time.Now().UTC()
	if err := journal.WriteLaunchCommand(secret, next); err != nil {
		t.Fatalf("rotate expired signed generation: %v", err)
	}
}

func TestJournalConcurrentEventCreateHasSingleWinner(t *testing.T) {
	t.Parallel()

	journal := newTestJournal(t)
	event := opsprotocol.OperationEvent{
		OperationID: "op-004", Generation: 2, Sequence: 9, Kind: "fact",
		Stage: opsprotocol.StageBackingUp, Stream: "system", Message: "备份完成", CreatedAt: time.Now().UTC(),
	}
	var successes atomic.Int32
	var conflicts atomic.Int32
	var unexpected atomic.Value
	var wait sync.WaitGroup
	for range 12 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			err := journal.AppendEvent(event)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrImmutableFactExists):
				conflicts.Add(1)
			default:
				unexpected.Store(err)
			}
		}()
	}
	wait.Wait()
	if value := unexpected.Load(); value != nil {
		t.Fatalf("unexpected append error: %v", value)
	}
	if successes.Load() != 1 || conflicts.Load() != 11 {
		t.Fatalf("successes=%d conflicts=%d", successes.Load(), conflicts.Load())
	}
}

func newTestJournal(t *testing.T) *Journal {
	t.Helper()
	journal, err := NewJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return journal
}
