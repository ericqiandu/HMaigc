package opscontroller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"infinite-canvas/backend/internal/opsprotocol"
	"infinite-canvas/backend/internal/opsrunner"
)

func (c *Controller) CancelOperation(operationID string, input opsprotocol.CancelOperationRequest) (*opsprotocol.Operation, error) {
	operationID = strings.TrimSpace(operationID)
	normalized, err := normalizeCancelRequest(operationID, input)
	if err != nil {
		return nil, err
	}
	requestHash, err := controlRequestHash(normalized)
	if err != nil {
		return nil, err
	}

	c.startMu.Lock()
	defer c.startMu.Unlock()
	operation, err := c.store.Operation(operationID)
	if err != nil {
		return nil, err
	}
	existing, readErr := c.journal.ReadCancelCommand(c.commandSecret, operationID)
	if readErr == nil {
		if existing.RequestHash != requestHash {
			return nil, ErrIdempotencyConflict
		}
		return operation, nil
	}
	if !errors.Is(readErr, fs.ErrNotExist) {
		return nil, readErr
	}
	if operation.Status != opsprotocol.OperationQueued && operation.Status != opsprotocol.OperationRunning {
		return nil, ErrCancellationNotAllowed
	}
	now := time.Now().UTC()
	nonce, err := newFencingToken()
	if err != nil {
		return nil, err
	}
	command := opsprotocol.CancelOperationCommand{
		OperationID: operationID, RequestHash: requestHash,
		ActorUserID: normalized.ActorUserID, ActorDisplayName: normalized.ActorDisplayName,
		RequestedAt: now, Nonce: nonce,
	}
	if err := c.journal.WriteCancelCommand(c.commandSecret, command); err != nil {
		return nil, err
	}
	if err := c.store.MarkCancelling(operationID, operation.Status, now); err != nil {
		return nil, err
	}
	if err := c.store.AppendLog(operationID, "system", "管理员已请求安全停止任务", now); err != nil {
		return nil, err
	}
	if operation.Status == opsprotocol.OperationQueued {
		if err := c.journal.WriteResult(operationID, opsprotocol.OperationResult{
			OperationID: operationID, Status: opsprotocol.OperationCancelled,
			ResultVersion: operation.CurrentVersionAtStart, ServiceState: operation.ServiceState,
			ControllerVersion: operation.ControllerVersionAtStart,
			ControllerHandoff: opsprotocol.ControllerHandoffUnchanged,
			ErrorCode:         opsprotocol.ErrorCancelledAtSafePoint, Error: "cancelled before Runner launch",
			CompletedAt: now,
		}); err != nil {
			return nil, err
		}
		if err := c.projector.Project(operationID); err != nil {
			return nil, err
		}
	}
	return c.store.Operation(operationID)
}

func (c *Controller) RecoverOperation(ctx context.Context, operationID string, input opsprotocol.RecoverOperationRequest) (*opsprotocol.Operation, error) {
	operationID = strings.TrimSpace(operationID)
	normalized, err := normalizeRecoverRequest(operationID, input)
	if err != nil {
		return nil, err
	}
	requestHash, err := controlRequestHash(normalized)
	if err != nil {
		return nil, err
	}

	c.startMu.Lock()
	defer c.startMu.Unlock()
	operation, err := c.store.Operation(operationID)
	if err != nil {
		return nil, err
	}
	existingRecovery, readErr := c.journal.ReadRecoverCommand(c.commandSecret, operationID)
	if readErr == nil {
		if existingRecovery.RequestHash != requestHash {
			return nil, ErrIdempotencyConflict
		}
		if operation.Status != opsprotocol.OperationRecoveryRequired {
			return operation, nil
		}
	} else if !errors.Is(readErr, fs.ErrNotExist) {
		return nil, readErr
	}
	if operation.Status != opsprotocol.OperationRecoveryRequired {
		return nil, ErrRecoveryNotAllowed
	}
	instances, err := c.manager.ListByOperation(ctx, operationID)
	if err != nil {
		return nil, err
	}
	for _, instance := range instances {
		if instance.Running {
			return nil, fmt.Errorf("%w: 旧 Runner %s 仍在运行", ErrRecoveryNotAllowed, instance.ContainerID)
		}
	}
	checkpoint, err := c.journal.ReadCheckpoint(operationID)
	if err != nil {
		return nil, fmt.Errorf("%w: 恢复检查点不存在或已损坏: %v", ErrRecoveryNotAllowed, err)
	}
	action, err := recoveryAction(checkpoint)
	if err != nil {
		return nil, err
	}
	if action == opsprotocol.RecoveryNone {
		return c.completeSafeNoopRecovery(operation, checkpoint)
	}
	if action == opsprotocol.RecoveryRequireOperator {
		return nil, fmt.Errorf("%w: 持久事实无法确定安全恢复动作", ErrRecoveryNotAllowed)
	}
	if existingRecovery == nil {
		now := time.Now().UTC()
		nonce, nonceErr := newFencingToken()
		if nonceErr != nil {
			return nil, nonceErr
		}
		existingRecovery = &opsprotocol.RecoverOperationCommand{
			OperationID: operationID, RequestHash: requestHash,
			ActorUserID: normalized.ActorUserID, ActorDisplayName: normalized.ActorDisplayName,
			RecoveryAction: action, RequestedAt: now, Nonce: nonce,
		}
		if err := c.journal.WriteRecoverCommand(c.commandSecret, *existingRecovery); err != nil {
			return nil, err
		}
	}
	return c.launchRecovery(ctx, operation, checkpoint, *existingRecovery)
}

func (c *Controller) launchRecovery(
	ctx context.Context,
	operation *opsprotocol.Operation,
	checkpoint opsprotocol.OperationCheckpoint,
	recovery opsprotocol.RecoverOperationCommand,
) (*opsprotocol.Operation, error) {
	request, err := c.journal.ReadRequest(operation.ID)
	if err != nil {
		return nil, err
	}
	generation := checkpoint.Generation + 1
	launch, err := c.journal.ReadLaunchCommandForReconciliation(c.commandSecret, operation.ID)
	if err == nil && launch.Generation >= generation {
		if launch.RecoveryAction != recovery.RecoveryAction {
			return nil, ErrIdempotencyConflict
		}
		generation = launch.Generation
	} else {
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		token, tokenErr := newFencingToken()
		if tokenErr != nil {
			return nil, tokenErr
		}
		launch = opsprotocol.RunnerLaunchCommand{
			OperationID: operation.ID, Generation: generation, FencingToken: token,
			RunnerDigest: checkpoint.RunnerDigest, RecoveryAction: recovery.RecoveryAction,
			IssuedAt: time.Now().UTC(),
		}
		if err := c.journal.WriteLaunchCommand(c.commandSecret, launch); err != nil {
			return nil, err
		}
	}
	runner := ResolvedRunner{Version: runnerVersionForRequest(request), Digest: launch.RunnerDigest}
	now := time.Now().UTC()
	if err := c.store.MarkRecovering(operation.ID, checkpoint.Stage, runner, generation, now); err != nil {
		return nil, err
	}
	if err := c.manager.Start(ctx, RunnerLaunch{
		OperationID: operation.ID, Generation: generation,
		ImageDigest: launch.RunnerDigest, StateVolume: c.config.StateVolume,
	}); err != nil {
		message := "启动恢复 Runner 失败: " + err.Error()
		if persistErr := c.store.MarkRecoveryLaunchFailed(
			operation.ID, generation, recovery.RecoveryAction, message, time.Now().UTC(),
		); persistErr != nil {
			return nil, errors.Join(err, persistErr)
		}
		return nil, err
	}
	if err := c.store.AppendLog(operation.ID, "system", "已证明旧 Runner 停止，启动新 generation 执行安全恢复", now); err != nil {
		return nil, err
	}
	return c.store.Operation(operation.ID)
}

func recoveryAction(checkpoint opsprotocol.OperationCheckpoint) (opsprotocol.RecoveryAction, error) {
	if executableRecoveryAction(checkpoint.NextSafeAction) {
		return checkpoint.NextSafeAction, nil
	}
	decision, err := opsrunner.DecideRecovery(checkpoint, opsrunner.ObservedState{
		ActualVersion: checkpoint.CurrentVersion,
		CurrentHealthy: checkpoint.ServiceState == opsprotocol.ServiceCurrentOnline ||
			checkpoint.ServiceState == opsprotocol.ServiceCurrentRestored,
		TargetHealthy:       checkpoint.TargetBackendHealthy && checkpoint.TargetWebHealthy,
		DataRewriteStarted:  checkpoint.DataMigrationStarted,
		VerifiedBackupPath:  checkpoint.BackupPath,
		ReleaseCommitted:    checkpoint.ReleaseCommitted,
		RunnerStillOwnsLock: true,
	})
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrRecoveryNotAllowed, err)
	}
	return decision.Action, nil
}

func (c *Controller) completeSafeNoopRecovery(operation *opsprotocol.Operation, checkpoint opsprotocol.OperationCheckpoint) (*opsprotocol.Operation, error) {
	now := time.Now().UTC()
	if err := c.journal.WriteResult(operation.ID, opsprotocol.OperationResult{
		OperationID: operation.ID, Generation: checkpoint.Generation + 1,
		Status: opsprotocol.OperationFailed, ResultVersion: checkpoint.CurrentVersion,
		ControllerVersion: checkpoint.CurrentControllerVersion, ServiceState: checkpoint.ServiceState,
		ControllerHandoff: checkpoint.ControllerHandoff, Warnings: checkpoint.Warnings,
		ErrorCode: checkpoint.FailureCode, Error: checkpoint.FailureMessage, CompletedAt: now,
	}); err != nil {
		return nil, err
	}
	if err := c.projector.Project(operation.ID); err != nil {
		return nil, err
	}
	return c.store.Operation(operation.ID)
}

func normalizeCancelRequest(operationID string, input opsprotocol.CancelOperationRequest) (opsprotocol.CancelOperationRequest, error) {
	input.ActorUserID = strings.TrimSpace(input.ActorUserID)
	input.ActorDisplayName = strings.TrimSpace(input.ActorDisplayName)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.Confirmation = strings.TrimSpace(input.Confirmation)
	if err := validateControlActor(input.ActorUserID, input.ActorDisplayName, input.IdempotencyKey); err != nil {
		return input, err
	}
	if input.Confirmation != "STOP "+operationID {
		return input, invalidRequest("确认短语不匹配，应为 STOP " + operationID)
	}
	return input, nil
}

func normalizeRecoverRequest(operationID string, input opsprotocol.RecoverOperationRequest) (opsprotocol.RecoverOperationRequest, error) {
	input.ActorUserID = strings.TrimSpace(input.ActorUserID)
	input.ActorDisplayName = strings.TrimSpace(input.ActorDisplayName)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.Confirmation = strings.TrimSpace(input.Confirmation)
	if err := validateControlActor(input.ActorUserID, input.ActorDisplayName, input.IdempotencyKey); err != nil {
		return input, err
	}
	if input.Confirmation != "RECOVER "+operationID {
		return input, invalidRequest("确认短语不匹配，应为 RECOVER " + operationID)
	}
	return input, nil
}

func validateControlActor(actorUserID string, actorDisplayName string, idempotencyKey string) error {
	if actorUserID == "" || len(actorUserID) > 64 {
		return invalidRequest("管理员标识无效")
	}
	if actorDisplayName == "" || len([]rune(actorDisplayName)) > 80 {
		return invalidRequest("管理员名称无效")
	}
	if !validIdempotencyKey(idempotencyKey) {
		return invalidRequest("幂等键必须是 16-128 位字母、数字、下划线或连字符")
	}
	return nil
}

func controlRequestHash(input interface{}) (string, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func executableRecoveryAction(action opsprotocol.RecoveryAction) bool {
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
