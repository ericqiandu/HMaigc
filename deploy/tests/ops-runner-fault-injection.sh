#!/usr/bin/env bash

set -Eeuo pipefail

readonly REPOSITORY_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

run_fault_group() {
    local label="$1"
    local package="$2"
    local pattern="$3"

    printf 'fault gate: %s\n' "$label"
    (
        cd "$REPOSITORY_ROOT/backend"
        go test "$package" -run "$pattern" -count=1
    )
}

run_fault_group \
    'immutable facts, command tampering, generation fencing, and corrupt journals' \
    './internal/opsstate' \
    'TestJournal(PersistsImmutableRequestAndEvents|RejectsTraversalAndCorruptMutableFact|VerifiesCommandEnvelopeBeforePayloadDecode|RejectsLaunchReplayAndAllowsHigherGeneration|AtomicallyReplacesMutableFacts|RejectsCheckpointRegressionAndResultOverwrite|ConcurrentEventCreateHasSingleWinner)|TestLeaseFencingRejectsStaleOrConflictingOwner'

run_fault_group \
    'duplicate requests, controller restart, ownership loss, and recovery authorization' \
    './internal/opscontroller' \
    'TestController(IdempotencyConflictUsesDeterministicOperationID|RejectsConcurrentOperations|RestartReattachesMatchingRunner|ProjectsResultExactlyOnce|EntersRecoveryRequiredWhenRunnerOwnershipUnknown)|TestRecover(StartsNewGenerationOnlyAfterPriorRunnerStopped|RejectsWhenPriorRunnerCanStillMutateProduction)|TestCancel(OperationWritesSignedCommandAndMovesToCancelling|QueuedOperationCompletesWithoutStartingRunner)|TestRollbackReadinessRequiresVerifiedBackupInsideRoot'

run_fault_group \
    'checkpoint restart, safe cancellation, target failure, repeated recovery, and controller handoff' \
    './internal/opsrunner' \
    'Test(UpgradePersistsFactBeforeNextStage|RunnerRestartResumesAfterEveryCompletedCheckpoint|CancellationAfterQuiesceRestoresCurrentBeforeCancelled|CancellationBeforeMaintenanceDoesNotExecuteAnyStage|FailureAfterDataRewriteRestoresVerifiedBackup|RestoreFailureRequiresOperatorAndPreservesEvidence|ControllerHandoffRestoredPreviousRemainsSuccessfulWarning|RunnerRefusesProductionStageAfterLeaseExpires|RestartAfterDestructiveIntentDoesNotBlindlyRepeatStage|HigherGenerationContinuesOnlyPersistedRecoveryAction|RecoveryAfterCommittedReleaseKeepsHealthyTarget|RecoveryUsesCurrentBeforeDataRewriteAndBackupAfter|RecoveryRequiresOperatorWithoutRunnerOwnership|DeploymentLockRejectsConcurrentRunnerWithoutWaiting|ShellRuntimeEnforcesPerStageTimeout)'

run_fault_group \
    'one-time bootstrap active-operation refusal and repeat import integrity' \
    './internal/opsbootstrap' \
    'TestBootstrap(RefusesActiveHistoricalOperation|ImportsTerminalHistoryWithoutChangingIDs|IsIdempotentForSameSourceAndRejectsDifferentFacts)'

# This deterministic stage harness injects a bounded public-CDN hang and candidate
# controller health failure. Both are deliberately isolated from the committed
# business release outcome.
bash "$REPOSITORY_ROOT/deploy/tests/hmaigc-stage-smoke.sh"

printf 'durable operations fault-injection gate passed\n'
