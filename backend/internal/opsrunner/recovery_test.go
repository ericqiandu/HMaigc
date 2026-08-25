package opsrunner

import (
	"testing"
	"time"

	"infinite-canvas/backend/internal/opsprotocol"
)

func TestUncheckpointedDestructiveEventsSelectVerifiedBackupRecovery(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{EventKindIntent, EventKindFailure, EventKindFact} {
		kind := kind
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			checkpoint := opsprotocol.OperationCheckpoint{
				Generation: 1, Sequence: 4, ExpectedVersion: "v1.0.58",
				VerifiedRecoveryPoint: true, BackupPath: "/verified/current",
				BackupChecksumStatus: "verified", ServiceState: opsprotocol.ServiceMaintenance,
			}
			updated, applied, err := ApplyUnknownStageIntent(checkpoint, opsprotocol.OperationEvent{
				Generation: 1, Sequence: 5, Kind: kind, Stage: opsprotocol.StageStartingTarget,
			}, time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			if !applied || !updated.DataMigrationStarted || updated.NextSafeAction != opsprotocol.RecoveryRestoreBackup {
				t.Fatalf("kind=%s updated=%+v applied=%v", kind, updated, applied)
			}
		})
	}
}

func TestRecoveryAfterCommittedReleaseKeepsHealthyTarget(t *testing.T) {
	t.Parallel()

	decision, err := DecideRecovery(
		opsprotocol.OperationCheckpoint{
			ExpectedVersion: "v1.0.58", ReleaseCommitted: true,
			ServiceState: opsprotocol.ServiceTargetOnline,
		},
		ObservedState{
			ActualVersion: "v1.0.58", TargetHealthy: true,
			ReleaseCommitted: true, RunnerStillOwnsLock: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != opsprotocol.RecoveryContinueControllerHandoff ||
		decision.Stage != opsprotocol.StageControllerHandoff ||
		decision.ExpectedServiceState != opsprotocol.ServiceTargetOnline {
		t.Fatalf("decision=%+v", decision)
	}
}

func TestRecoveryUsesCurrentBeforeDataRewriteAndBackupAfter(t *testing.T) {
	t.Parallel()

	current, err := DecideRecovery(
		opsprotocol.OperationCheckpoint{
			WritesQuiesced: true, ServiceState: opsprotocol.ServiceMaintenance,
		},
		ObservedState{RunnerStillOwnsLock: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if current.Action != opsprotocol.RecoveryRestoreCurrent || current.Stage != opsprotocol.StageRestoringCurrent {
		t.Fatalf("current decision=%+v", current)
	}

	backup, err := DecideRecovery(
		opsprotocol.OperationCheckpoint{
			WritesQuiesced: true, VerifiedRecoveryPoint: true,
			BackupPath: "/verified/backup", BackupChecksumStatus: "verified",
			DataMigrationStarted: true, ServiceState: opsprotocol.ServiceMaintenance,
		},
		ObservedState{
			DataRewriteStarted: true, VerifiedBackupPath: "/verified/backup",
			RunnerStillOwnsLock: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if backup.Action != opsprotocol.RecoveryRestoreBackup || backup.Stage != opsprotocol.StageRestoringBackup {
		t.Fatalf("backup decision=%+v", backup)
	}
}

func TestRecoveryRequiresOperatorWithoutRunnerOwnership(t *testing.T) {
	t.Parallel()

	decision, err := DecideRecovery(
		opsprotocol.OperationCheckpoint{WritesQuiesced: true, ServiceState: opsprotocol.ServiceMaintenance},
		ObservedState{RunnerStillOwnsLock: false},
	)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != opsprotocol.RecoveryRequireOperator || decision.ExpectedServiceState != opsprotocol.ServiceUnknown {
		t.Fatalf("decision=%+v", decision)
	}
}
