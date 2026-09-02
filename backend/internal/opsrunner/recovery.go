package opsrunner

import (
	"fmt"
	"time"

	"infinite-canvas/backend/internal/opsprotocol"
)

type ObservedState struct {
	ActualVersion       string
	CurrentHealthy      bool
	TargetHealthy       bool
	DataRewriteStarted  bool
	VerifiedBackupPath  string
	ReleaseCommitted    bool
	RunnerStillOwnsLock bool
}

func ApplyUnknownStageIntent(
	checkpoint opsprotocol.OperationCheckpoint,
	event opsprotocol.OperationEvent,
	now time.Time,
) (opsprotocol.OperationCheckpoint, bool, error) {
	if !isStageOutcomeEvent(event.Kind) || event.Generation != checkpoint.Generation ||
		event.Sequence <= checkpoint.Sequence || !isDestructiveStage(event.Stage) {
		return checkpoint, false, nil
	}
	checkpoint.Stage = event.Stage
	checkpoint.StageCompletedAt = nil
	checkpoint.Sequence = event.Sequence
	checkpoint.FailureCode = opsprotocol.ErrorStateConflict
	checkpoint.FailureMessage = "destructive stage event was persisted without a matching checkpoint"
	checkpoint.NextSafeAction = opsprotocol.RecoveryRequireOperator
	if stageMayRewriteData(event.Stage) {
		checkpoint.DataMigrationStarted = true
		decision, err := DecideRecovery(checkpoint, observedFrom(checkpoint, StageOutput{}))
		if err != nil {
			return checkpoint, false, err
		}
		checkpoint.NextSafeAction = decision.Action
	}
	checkpoint.ServiceState = opsprotocol.ServiceUnknown
	checkpoint.UpdatedAt = now.UTC()
	return checkpoint, true, nil
}

func isStageOutcomeEvent(kind string) bool {
	return kind == EventKindIntent || kind == EventKindFailure || kind == EventKindFact
}

type RecoveryDecision struct {
	Action               opsprotocol.RecoveryAction
	Stage                opsprotocol.OperationStage
	ExpectedServiceState opsprotocol.ServiceState
	Reason               string
}

func DecideRecovery(checkpoint opsprotocol.OperationCheckpoint, observed ObservedState) (RecoveryDecision, error) {
	if checkpoint.ExpectedVersion == "" && (checkpoint.ReleaseCommitted || observed.ReleaseCommitted) {
		return RecoveryDecision{}, fmt.Errorf("committed release is missing its expected version")
	}
	if !observed.RunnerStillOwnsLock {
		return requireOperator("runner ownership cannot be proven"), nil
	}
	if checkpoint.ReleaseCommitted || observed.ReleaseCommitted {
		if observed.TargetHealthy && observed.ActualVersion == checkpoint.ExpectedVersion {
			return RecoveryDecision{
				Action:               opsprotocol.RecoveryContinueControllerHandoff,
				Stage:                opsprotocol.StageControllerHandoff,
				ExpectedServiceState: opsprotocol.ServiceTargetOnline,
				Reason:               "committed target is healthy; only controller handoff remains",
			}, nil
		}
		return requireOperator("release commit evidence conflicts with the observed target"), nil
	}
	if checkpoint.DataMigrationStarted || observed.DataRewriteStarted {
		if checkpoint.VerifiedRecoveryPoint &&
			checkpoint.BackupChecksumStatus == "verified" &&
			checkpoint.BackupPath != "" &&
			checkpoint.BackupPath == observed.VerifiedBackupPath {
			return RecoveryDecision{
				Action:               opsprotocol.RecoveryRestoreBackup,
				Stage:                opsprotocol.StageRestoringBackup,
				ExpectedServiceState: opsprotocol.ServiceCurrentRestored,
				Reason:               "data rewrite started after a verified recovery point",
			}, nil
		}
		return requireOperator("data may have changed but no matching verified recovery point exists"), nil
	}
	if checkpoint.WritesQuiesced || checkpoint.ServiceState == opsprotocol.ServiceMaintenance {
		return RecoveryDecision{
			Action:               opsprotocol.RecoveryRestoreCurrent,
			Stage:                opsprotocol.StageRestoringCurrent,
			ExpectedServiceState: opsprotocol.ServiceCurrentRestored,
			Reason:               "writes were quiesced before any data rewrite",
		}, nil
	}
	if observed.CurrentHealthy || checkpoint.ServiceState == opsprotocol.ServiceCurrentOnline {
		return RecoveryDecision{
			Action:               opsprotocol.RecoveryNone,
			ExpectedServiceState: opsprotocol.ServiceCurrentOnline,
			Reason:               "current release remains online",
		}, nil
	}
	return requireOperator("service state cannot be proven"), nil
}

func requireOperator(reason string) RecoveryDecision {
	return RecoveryDecision{
		Action:               opsprotocol.RecoveryRequireOperator,
		ExpectedServiceState: opsprotocol.ServiceUnknown,
		Reason:               reason,
	}
}
