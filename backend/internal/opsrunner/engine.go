package opsrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"infinite-canvas/backend/internal/opsprotocol"
	"infinite-canvas/backend/internal/opsstate"
)

type Engine struct {
	journal *opsstate.Journal
	runtime Runtime
	now     Clock
}

var ErrRunnerLeaseLost = errors.New("runner lease ownership lost")

func NewEngine(journal *opsstate.Journal, runtime Runtime, now Clock) *Engine {
	return &Engine{journal: journal, runtime: runtime, now: now}
}

func (e *Engine) Run(ctx context.Context, input RunInput) error {
	if err := validateRunInput(input); err != nil {
		return err
	}
	if e.journal == nil || e.runtime == nil || e.now == nil {
		return fmt.Errorf("runner engine dependencies are incomplete")
	}
	if err := e.validateOwnership(input); err != nil {
		return err
	}
	recoveryGeneration := false
	if result, err := e.journal.ReadResult(input.OperationID); err == nil {
		if opsprotocol.IsTerminalStatus(result.Status) {
			return nil
		}
		if result.Status != opsprotocol.OperationRecoveryRequired {
			return fmt.Errorf("persisted result is not a completed runner outcome: %s", result.Status)
		}
		if result.Generation >= input.Generation {
			return nil
		}
		recoveryGeneration = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	request, err := e.journal.ReadRequest(input.OperationID)
	if err != nil {
		return err
	}
	if request.ImportedTerminal {
		return fmt.Errorf("%w: imported terminal history cannot be executed", opsstate.ErrCommandMismatch)
	}
	stages, err := stagesFor(request.Request.Action)
	if err != nil {
		return err
	}
	checkpoint, err := e.loadOrCreateCheckpoint(input, request)
	if err != nil {
		return err
	}
	sequence, latestEvent, err := e.readEventState(input.OperationID, input.Generation)
	if err != nil {
		return err
	}
	if latestEvent != nil {
		checkpoint, applied, applyErr := ApplyUnknownStageIntent(checkpoint, *latestEvent, e.now().UTC())
		if applyErr != nil {
			return applyErr
		}
		if applied {
			if err := e.journal.WriteCheckpoint(input.OperationID, checkpoint); err != nil {
				return err
			}
			return e.writeTerminalResult(
				input,
				request,
				checkpoint,
				opsprotocol.OperationRecoveryRequired,
				opsprotocol.ErrorStateConflict,
				"destructive stage outcome is unknown; the persisted recovery action must run in a new generation",
			)
		}
	}
	if recoveryGeneration {
		if _, cancelErr := e.journal.ReadCancelCommand(input.CommandSecret, input.OperationID); cancelErr == nil {
			checkpoint.CancelRequested = true
		} else if !errors.Is(cancelErr, fs.ErrNotExist) {
			return cancelErr
		}
		return e.runPersistedRecovery(ctx, input, request, &checkpoint, &sequence)
	}
	startIndex, err := nextStageIndex(stages, checkpoint)
	if err != nil {
		return err
	}

	for index := startIndex; index < len(stages); index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := e.validateOwnership(input); err != nil {
			return err
		}
		cancelled, err := e.handleCancellation(ctx, input, request, &checkpoint, &sequence)
		if err != nil || cancelled {
			return err
		}
		stage := stages[index]
		output, stageErr := e.executeStage(ctx, input, request, checkpoint, stage, &sequence)
		if stageErr != nil {
			return e.handleStageFailure(ctx, input, request, &checkpoint, stage, output, stageErr, &sequence)
		}
		advanceCheckpoint(&checkpoint, stage, output, e.now().UTC(), sequence)
		if err := e.journal.WriteCheckpoint(input.OperationID, checkpoint); err != nil {
			return err
		}
	}

	return e.writeTerminalResult(input, request, checkpoint, opsprotocol.OperationSucceeded, "", "")
}

func (e *Engine) validateOwnership(input RunInput) error {
	lease, err := e.journal.ReadLease(input.OperationID)
	if err != nil {
		return fmt.Errorf("%w: read durable lease: %v", ErrRunnerLeaseLost, err)
	}
	if lease.Generation != input.Generation ||
		lease.TokenHash != hashToken(input.FencingToken) ||
		lease.RunnerDigest != input.RunnerDigest ||
		!lease.ExpiresAt.After(e.now().UTC()) {
		return fmt.Errorf("%w: generation, token, digest, or expiry no longer matches", ErrRunnerLeaseLost)
	}
	return nil
}

func (e *Engine) loadOrCreateCheckpoint(input RunInput, request opsprotocol.OperationRequestFile) (opsprotocol.OperationCheckpoint, error) {
	checkpoint, err := e.journal.ReadCheckpoint(input.OperationID)
	if err == nil {
		if err := validateCheckpointRequestContract(checkpoint, request); err != nil {
			return opsprotocol.OperationCheckpoint{}, err
		}
		if checkpoint.Generation > input.Generation {
			return opsprotocol.OperationCheckpoint{}, fmt.Errorf("%w: checkpoint fencing mismatch", opsstate.ErrCommandMismatch)
		}
		if checkpoint.Generation == input.Generation {
			if checkpoint.FencingTokenHash != hashToken(input.FencingToken) || checkpoint.RunnerDigest != input.RunnerDigest {
				return opsprotocol.OperationCheckpoint{}, fmt.Errorf("%w: checkpoint fencing mismatch", opsstate.ErrCommandMismatch)
			}
			return checkpoint, nil
		}
		if !isRunnableRecoveryAction(checkpoint.NextSafeAction) {
			if !isRunnableRecoveryAction(input.RecoveryAction) {
				return opsprotocol.OperationCheckpoint{}, fmt.Errorf(
					"%w: checkpoint recovery action %s cannot start a new generation",
					opsstate.ErrCommandMismatch,
					checkpoint.NextSafeAction,
				)
			}
			checkpoint.NextSafeAction = input.RecoveryAction
		}
		checkpoint.Generation = input.Generation
		checkpoint.FencingTokenHash = hashToken(input.FencingToken)
		checkpoint.RunnerDigest = input.RunnerDigest
		checkpoint.Sequence = 0
		checkpoint.UpdatedAt = e.now().UTC()
		if err := e.journal.WriteCheckpoint(input.OperationID, checkpoint); err != nil {
			return opsprotocol.OperationCheckpoint{}, err
		}
		return checkpoint, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return opsprotocol.OperationCheckpoint{}, err
	}
	now := e.now().UTC()
	checkpoint = opsprotocol.OperationCheckpoint{
		OperationID: request.OperationID, Action: request.Request.Action,
		TargetVersion: operationTargetVersion(request), RunnerDigest: input.RunnerDigest,
		Generation: input.Generation, FencingTokenHash: hashToken(input.FencingToken),
		Stage: opsprotocol.StageAccepted, CurrentVersion: request.CurrentVersion,
		ExpectedVersion: request.ExpectedVersion, ServiceState: opsprotocol.ServiceCurrentOnline,
		CurrentControllerVersion: request.ControllerVersionAtStart,
		StageStartedAt:           &now, StageCompletedAt: &now, UpdatedAt: now,
	}
	if err := e.journal.WriteCheckpoint(input.OperationID, checkpoint); err != nil {
		return opsprotocol.OperationCheckpoint{}, err
	}
	return checkpoint, nil
}

func validateCheckpointRequestContract(checkpoint opsprotocol.OperationCheckpoint, request opsprotocol.OperationRequestFile) error {
	if checkpoint.OperationID != request.OperationID || checkpoint.Action != request.Request.Action ||
		checkpoint.ExpectedVersion != request.ExpectedVersion || checkpoint.TargetVersion != operationTargetVersion(request) {
		return fmt.Errorf("%w: checkpoint diverges from immutable operation request", opsstate.ErrCorruptFact)
	}
	return nil
}

func operationTargetVersion(request opsprotocol.OperationRequestFile) string {
	switch request.Request.Action {
	case opsprotocol.ActionInstall, opsprotocol.ActionUpgrade, opsprotocol.ActionRollback:
		return request.ExpectedVersion
	default:
		return ""
	}
}

func (e *Engine) runPersistedRecovery(
	ctx context.Context,
	input RunInput,
	request opsprotocol.OperationRequestFile,
	checkpoint *opsprotocol.OperationCheckpoint,
	sequence *uint64,
) error {
	if err := e.validateOwnership(input); err != nil {
		return err
	}
	stage, completionStatus, err := recoveryExecution(checkpoint.NextSafeAction, checkpoint.CancelRequested)
	if err != nil {
		return err
	}
	originalErrorCode := checkpoint.FailureCode
	originalError := checkpoint.FailureMessage
	output, recoveryErr := e.executeStage(ctx, input, request, *checkpoint, stage, sequence)
	if recoveryErr != nil {
		checkpoint.Stage = stage
		checkpoint.StageCompletedAt = nil
		checkpoint.Sequence = *sequence
		checkpoint.ServiceState = opsprotocol.ServiceUnknown
		checkpoint.NextSafeAction = opsprotocol.RecoveryRequireOperator
		checkpoint.FailureCode = opsprotocol.ErrorRestoreFailed
		checkpoint.FailureMessage = recoveryErr.Error()
		checkpoint.UpdatedAt = e.now().UTC()
		if err := e.journal.WriteCheckpoint(input.OperationID, *checkpoint); err != nil {
			return err
		}
		return e.writeTerminalResult(
			input,
			request,
			*checkpoint,
			opsprotocol.OperationRecoveryRequired,
			opsprotocol.ErrorRestoreFailed,
			recoveryErr.Error(),
		)
	}
	advanceCheckpoint(checkpoint, stage, output, e.now().UTC(), *sequence)
	checkpoint.NextSafeAction = opsprotocol.RecoveryNone
	if err := e.journal.WriteCheckpoint(input.OperationID, *checkpoint); err != nil {
		return err
	}
	return e.writeTerminalResult(input, request, *checkpoint, completionStatus, originalErrorCode, originalError)
}

func (e *Engine) readEventState(operationID string, generation uint64) (uint64, *opsprotocol.OperationEvent, error) {
	events, err := e.journal.ReadEvents(operationID, generation, 0)
	if err != nil {
		return 0, nil, err
	}
	if len(events) == 0 {
		return 0, nil, nil
	}
	latest := events[len(events)-1]
	return latest.Sequence, &latest, nil
}

func (e *Engine) executeStage(
	ctx context.Context,
	input RunInput,
	request opsprotocol.OperationRequestFile,
	checkpoint opsprotocol.OperationCheckpoint,
	stage opsprotocol.OperationStage,
	sequence *uint64,
) (StageOutput, error) {
	if err := e.appendEvent(request.OperationID, input.Generation, sequence, EventKindIntent, stage, "stage intent persisted"); err != nil {
		return StageOutput{}, err
	}
	output, err := e.runtime.Execute(ctx, StageInput{
		Request: request, Checkpoint: checkpoint, Stage: stage, FencingToken: input.FencingToken,
	})
	if err != nil {
		if appendErr := e.appendEvent(request.OperationID, input.Generation, sequence, EventKindFailure, stage, err.Error()); appendErr != nil {
			return output, errors.Join(err, appendErr)
		}
		return output, err
	}
	if err := validateStageOutput(request, stage, output); err != nil {
		if appendErr := e.appendEvent(request.OperationID, input.Generation, sequence, EventKindFailure, stage, err.Error()); appendErr != nil {
			return output, errors.Join(err, appendErr)
		}
		return output, err
	}
	if err := e.appendEvent(request.OperationID, input.Generation, sequence, EventKindFact, stage, "stage fact persisted"); err != nil {
		return StageOutput{}, err
	}
	return output, nil
}

func validateStageOutput(request opsprotocol.OperationRequestFile, stage opsprotocol.OperationStage, output StageOutput) error {
	if output.ResultVersion == "" || stage == opsprotocol.StageRestoringCurrent || stage == opsprotocol.StageRestoringBackup {
		return nil
	}
	if output.ResultVersion != request.ExpectedVersion {
		return fmt.Errorf("stage %s reported result version %s, expected %s", stage, output.ResultVersion, request.ExpectedVersion)
	}
	return nil
}

func (e *Engine) appendEvent(
	operationID string,
	generation uint64,
	sequence *uint64,
	kind string,
	stage opsprotocol.OperationStage,
	message string,
) error {
	nextSequence := *sequence + 1
	err := e.journal.AppendEvent(opsprotocol.OperationEvent{
		OperationID: operationID, Generation: generation, Sequence: nextSequence,
		Kind: kind, Stage: stage, Stream: "system", Message: message, CreatedAt: e.now().UTC(),
	})
	if err != nil {
		return err
	}
	*sequence = nextSequence
	return nil
}

func (e *Engine) handleCancellation(
	ctx context.Context,
	input RunInput,
	request opsprotocol.OperationRequestFile,
	checkpoint *opsprotocol.OperationCheckpoint,
	sequence *uint64,
) (bool, error) {
	_, err := e.journal.ReadCancelCommand(input.CommandSecret, input.OperationID)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	checkpoint.CancelRequested = true
	checkpoint.NextSafeAction = opsprotocol.RecoveryNone
	checkpoint.UpdatedAt = e.now().UTC()
	if checkpoint.ServiceState == opsprotocol.ServiceCurrentOnline && !checkpoint.WritesQuiesced {
		if err := e.writeTerminalResult(input, request, *checkpoint, opsprotocol.OperationCancelled, opsprotocol.ErrorCancelledAtSafePoint, "cancelled before the maintenance window"); err != nil {
			return false, err
		}
		return true, nil
	}
	if checkpoint.ReleaseCommitted {
		return false, nil
	}
	stage := opsprotocol.StageRestoringCurrent
	if checkpoint.DataMigrationStarted && checkpoint.VerifiedRecoveryPoint {
		stage = opsprotocol.StageRestoringBackup
	}
	output, restoreErr := e.executeStage(ctx, input, request, *checkpoint, stage, sequence)
	if restoreErr != nil {
		checkpoint.FailureCode = opsprotocol.ErrorRestoreFailed
		checkpoint.FailureMessage = restoreErr.Error()
		checkpoint.NextSafeAction = opsprotocol.RecoveryRequireOperator
		checkpoint.ServiceState = opsprotocol.ServiceUnknown
		checkpoint.Sequence = *sequence
		checkpoint.UpdatedAt = e.now().UTC()
		if err := e.journal.WriteCheckpoint(input.OperationID, *checkpoint); err != nil {
			return false, err
		}
		if err := e.writeTerminalResult(input, request, *checkpoint, opsprotocol.OperationRecoveryRequired, opsprotocol.ErrorRestoreFailed, restoreErr.Error()); err != nil {
			return false, err
		}
		return true, nil
	}
	advanceCheckpoint(checkpoint, stage, output, e.now().UTC(), *sequence)
	checkpoint.CancelRequested = true
	if err := e.journal.WriteCheckpoint(input.OperationID, *checkpoint); err != nil {
		return false, err
	}
	if err := e.writeTerminalResult(input, request, *checkpoint, opsprotocol.OperationCancelled, opsprotocol.ErrorCancelledAtSafePoint, "cancelled at a safe point"); err != nil {
		return false, err
	}
	return true, nil
}

func (e *Engine) handleStageFailure(
	ctx context.Context,
	input RunInput,
	request opsprotocol.OperationRequestFile,
	checkpoint *opsprotocol.OperationCheckpoint,
	stage opsprotocol.OperationStage,
	output StageOutput,
	stageErr error,
	sequence *uint64,
) error {
	if stageMayRewriteData(stage) {
		checkpoint.DataMigrationStarted = true
	}
	applyStageOutput(checkpoint, stage, output)
	checkpoint.Stage = stage
	checkpoint.StageCompletedAt = nil
	checkpoint.Sequence = *sequence
	checkpoint.FailureCode = errorCodeForStage(stage)
	checkpoint.FailureMessage = stageErr.Error()
	checkpoint.UpdatedAt = e.now().UTC()
	decision, err := DecideRecovery(*checkpoint, observedFrom(*checkpoint, output))
	if err != nil {
		return err
	}
	checkpoint.NextSafeAction = decision.Action
	if err := e.journal.WriteCheckpoint(input.OperationID, *checkpoint); err != nil {
		return err
	}
	if decision.Action == opsprotocol.RecoveryNone {
		return e.writeTerminalResult(input, request, *checkpoint, opsprotocol.OperationFailed, checkpoint.FailureCode, stageErr.Error())
	}
	if decision.Action == opsprotocol.RecoveryRequireOperator {
		checkpoint.ServiceState = opsprotocol.ServiceUnknown
		return e.writeTerminalResult(input, request, *checkpoint, opsprotocol.OperationRecoveryRequired, checkpoint.FailureCode, decision.Reason)
	}
	if decision.Action == opsprotocol.RecoveryContinueControllerHandoff {
		return e.writeTerminalResult(input, request, *checkpoint, opsprotocol.OperationRecoveryRequired, opsprotocol.ErrorControllerHandoffFailed, decision.Reason)
	}

	recoveryOutput, recoveryErr := e.executeStage(ctx, input, request, *checkpoint, decision.Stage, sequence)
	if recoveryErr != nil {
		checkpoint.ServiceState = opsprotocol.ServiceUnknown
		checkpoint.NextSafeAction = opsprotocol.RecoveryRequireOperator
		checkpoint.FailureCode = opsprotocol.ErrorRestoreFailed
		checkpoint.FailureMessage = recoveryErr.Error()
		checkpoint.Sequence = *sequence
		checkpoint.UpdatedAt = e.now().UTC()
		if err := e.journal.WriteCheckpoint(input.OperationID, *checkpoint); err != nil {
			return err
		}
		return e.writeTerminalResult(input, request, *checkpoint, opsprotocol.OperationRecoveryRequired, opsprotocol.ErrorRestoreFailed, recoveryErr.Error())
	}
	advanceCheckpoint(checkpoint, decision.Stage, recoveryOutput, e.now().UTC(), *sequence)
	checkpoint.NextSafeAction = opsprotocol.RecoveryNone
	if err := e.journal.WriteCheckpoint(input.OperationID, *checkpoint); err != nil {
		return err
	}
	return e.writeTerminalResult(input, request, *checkpoint, opsprotocol.OperationFailed, errorCodeForStage(stage), stageErr.Error())
}

func (e *Engine) writeTerminalResult(
	input RunInput,
	request opsprotocol.OperationRequestFile,
	checkpoint opsprotocol.OperationCheckpoint,
	status opsprotocol.OperationStatus,
	errorCode opsprotocol.OperationErrorCode,
	errorMessage string,
) error {
	now := e.now().UTC()
	handoff := checkpointControllerHandoff(checkpoint)
	resultVersion := checkpoint.CurrentVersion
	if status == opsprotocol.OperationSucceeded {
		resultVersion = checkpoint.ExpectedVersion
	}
	return e.journal.WriteResult(input.OperationID, opsprotocol.OperationResult{
		OperationID: input.OperationID, Generation: input.Generation,
		Status: status, ResultVersion: resultVersion,
		ControllerVersion: checkpoint.CurrentControllerVersion,
		ServiceState:      checkpoint.ServiceState, ControllerHandoff: handoff,
		Warnings:  append([]opsprotocol.OperationWarning(nil), checkpoint.Warnings...),
		ErrorCode: errorCode, Error: errorMessage, CompletedAt: now,
	})
}

func advanceCheckpoint(
	checkpoint *opsprotocol.OperationCheckpoint,
	stage opsprotocol.OperationStage,
	output StageOutput,
	completedAt time.Time,
	sequence uint64,
) {
	previous := checkpoint.Stage
	applyStageOutput(checkpoint, stage, output)
	checkpoint.PreviousCompletedStage = previous
	checkpoint.Stage = stage
	checkpoint.StageStartedAt = &completedAt
	checkpoint.StageCompletedAt = &completedAt
	checkpoint.Sequence = sequence
	checkpoint.FailureCode = ""
	checkpoint.FailureMessage = ""
	checkpoint.UpdatedAt = completedAt
}

func applyStageOutput(checkpoint *opsprotocol.OperationCheckpoint, stage opsprotocol.OperationStage, output StageOutput) {
	if output.ServiceState != "" {
		checkpoint.ServiceState = output.ServiceState
	}
	if output.CurrentVersion != "" {
		checkpoint.CurrentVersion = output.CurrentVersion
	}
	if output.BackupPath != "" {
		checkpoint.BackupPath = output.BackupPath
	}
	if output.BackupChecksumStatus != "" {
		checkpoint.BackupChecksumStatus = output.BackupChecksumStatus
	}
	if output.BackendImage != "" {
		checkpoint.BackendImage = output.BackendImage
	}
	if output.WebImage != "" {
		checkpoint.WebImage = output.WebImage
	}
	if output.BackupHelperImage != "" {
		checkpoint.BackupHelperImage = output.BackupHelperImage
	}
	if output.ControllerImage != "" {
		checkpoint.CandidateControllerImage = output.ControllerImage
	}
	if output.ControllerVersion != "" {
		checkpoint.CurrentControllerVersion = output.ControllerVersion
	}
	if output.ControllerHandoff != "" {
		checkpoint.ControllerHandoff = output.ControllerHandoff
	}
	if len(output.Warnings) > 0 {
		checkpoint.Warnings = append(checkpoint.Warnings, output.Warnings...)
	}
	checkpoint.VerifiedRecoveryPoint = checkpoint.VerifiedRecoveryPoint || output.VerifiedRecoveryPoint
	checkpoint.DataMigrationStarted = checkpoint.DataMigrationStarted || output.DataMigrationStarted
	checkpoint.TargetBackendHealthy = checkpoint.TargetBackendHealthy || output.TargetBackendHealthy
	checkpoint.TargetWebHealthy = checkpoint.TargetWebHealthy || output.TargetWebHealthy
	checkpoint.ReleaseCommitted = checkpoint.ReleaseCommitted || output.ReleaseCommitted
	checkpoint.WritesQuiesced = output.WritesQuiesced
	if stage == opsprotocol.StageRestoringCurrent || stage == opsprotocol.StageRestoringBackup ||
		output.ServiceState == opsprotocol.ServiceTargetOnline {
		checkpoint.WritesQuiesced = false
	}
}

func nextStageIndex(stages []opsprotocol.OperationStage, checkpoint opsprotocol.OperationCheckpoint) (int, error) {
	if checkpoint.Stage == opsprotocol.StageAccepted {
		return 0, nil
	}
	if checkpoint.StageCompletedAt == nil {
		return 0, fmt.Errorf("%w: stage %s has no completion fact", opsstate.ErrCorruptFact, checkpoint.Stage)
	}
	for index, stage := range stages {
		if stage == checkpoint.Stage {
			return index + 1, nil
		}
	}
	return 0, fmt.Errorf("%w: checkpoint stage %s is outside the action plan", opsstate.ErrCorruptFact, checkpoint.Stage)
}

func observedFrom(checkpoint opsprotocol.OperationCheckpoint, output StageOutput) ObservedState {
	actualVersion := output.ResultVersion
	if actualVersion == "" {
		actualVersion = checkpoint.ExpectedVersion
	}
	return ObservedState{
		ActualVersion:       actualVersion,
		CurrentHealthy:      checkpoint.ServiceState == opsprotocol.ServiceCurrentOnline || checkpoint.ServiceState == opsprotocol.ServiceCurrentRestored,
		TargetHealthy:       checkpoint.TargetBackendHealthy && checkpoint.TargetWebHealthy,
		DataRewriteStarted:  checkpoint.DataMigrationStarted,
		VerifiedBackupPath:  checkpoint.BackupPath,
		ReleaseCommitted:    checkpoint.ReleaseCommitted,
		RunnerStillOwnsLock: true,
	}
}

func errorCodeForStage(stage opsprotocol.OperationStage) opsprotocol.OperationErrorCode {
	switch stage {
	case opsprotocol.StageOnlinePreflight:
		return opsprotocol.ErrorPreflightFailed
	case opsprotocol.StagePublicVerifying:
		return opsprotocol.ErrorPublicVerifyFailed
	case opsprotocol.StageQuiescing, opsprotocol.StageQuiescedAudit:
		return opsprotocol.ErrorQuiesceFailed
	case opsprotocol.StageBackingUp:
		return opsprotocol.ErrorBackupFailed
	case opsprotocol.StageStartingTarget:
		return opsprotocol.ErrorMigrationFailed
	case opsprotocol.StageVerifyingTarget:
		return opsprotocol.ErrorTargetHealthFailed
	case opsprotocol.StageRestoringCurrent, opsprotocol.StageRestoringBackup, opsprotocol.StageRestoringRollbackBackup:
		return opsprotocol.ErrorRestoreFailed
	case opsprotocol.StageControllerHandoff:
		return opsprotocol.ErrorControllerHandoffFailed
	default:
		return opsprotocol.ErrorStateConflict
	}
}

func checkpointControllerHandoff(checkpoint opsprotocol.OperationCheckpoint) opsprotocol.ControllerHandoff {
	if checkpoint.ControllerHandoff != "" {
		return checkpoint.ControllerHandoff
	}
	return opsprotocol.ControllerHandoffUnchanged
}

func isDestructiveStage(stage opsprotocol.OperationStage) bool {
	switch stage {
	case opsprotocol.StageQuiescing,
		opsprotocol.StageQuiescedAudit,
		opsprotocol.StageBackingUp,
		opsprotocol.StageStartingTarget,
		opsprotocol.StageVerifyingTarget,
		opsprotocol.StageRestoringCurrent,
		opsprotocol.StageRestoringBackup,
		opsprotocol.StageRestoringRollbackBackup,
		opsprotocol.StageCommittingRelease,
		opsprotocol.StageControllerHandoff:
		return true
	default:
		return false
	}
}

func stageMayRewriteData(stage opsprotocol.OperationStage) bool {
	return stage == opsprotocol.StageStartingTarget || stage == opsprotocol.StageRestoringRollbackBackup
}

func isRunnableRecoveryAction(action opsprotocol.RecoveryAction) bool {
	switch action {
	case opsprotocol.RecoveryRestoreCurrent,
		opsprotocol.RecoveryRestoreBackup,
		opsprotocol.RecoveryCommitTarget,
		opsprotocol.RecoveryContinueControllerHandoff:
		return true
	default:
		return false
	}
}

func recoveryExecution(action opsprotocol.RecoveryAction, cancelRequested bool) (opsprotocol.OperationStage, opsprotocol.OperationStatus, error) {
	switch action {
	case opsprotocol.RecoveryRestoreCurrent:
		status := opsprotocol.OperationFailed
		if cancelRequested {
			status = opsprotocol.OperationCancelled
		}
		return opsprotocol.StageRestoringCurrent, status, nil
	case opsprotocol.RecoveryRestoreBackup:
		status := opsprotocol.OperationFailed
		if cancelRequested {
			status = opsprotocol.OperationCancelled
		}
		return opsprotocol.StageRestoringBackup, status, nil
	case opsprotocol.RecoveryCommitTarget:
		return opsprotocol.StageCommittingRelease, opsprotocol.OperationSucceeded, nil
	case opsprotocol.RecoveryContinueControllerHandoff:
		return opsprotocol.StageControllerHandoff, opsprotocol.OperationSucceeded, nil
	default:
		return "", "", fmt.Errorf("recovery action %s is not executable", action)
	}
}

func validateRunInput(input RunInput) error {
	if input.OperationID == "" || input.Generation == 0 || input.FencingToken == "" || input.RunnerDigest == "" {
		return fmt.Errorf("runner operation id, generation, fencing token, and digest are required")
	}
	if len(input.CommandSecret) < 32 {
		return fmt.Errorf("runner command secret must contain at least 32 bytes")
	}
	return nil
}

func hashToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}
