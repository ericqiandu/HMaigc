package opscontroller

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"infinite-canvas/backend/internal/opsprotocol"
	"infinite-canvas/backend/internal/opsrunner"
)

func (c *Controller) Reconcile(ctx context.Context) error {
	requests, err := c.journal.ListRequests()
	if err != nil {
		return err
	}
	for _, request := range requests {
		if _, _, err := c.store.ImportRequest(request); err != nil {
			return err
		}
		if err := c.projector.Project(request.OperationID); err != nil {
			return err
		}
	}
	active, err := c.store.ActiveOperation()
	if err != nil || active == nil || active.Status == opsprotocol.OperationQueued {
		return err
	}
	if active.Status == opsprotocol.OperationRecoveryRequired || opsprotocol.IsTerminalStatus(active.Status) {
		return nil
	}
	instances, err := c.manager.ListByOperation(ctx, active.ID)
	if err != nil {
		return err
	}
	runningInstances := make([]RunnerInstance, 0, len(instances))
	for _, instance := range instances {
		if instance.Running {
			runningInstances = append(runningInstances, instance)
		}
	}
	if len(runningInstances) > 1 {
		return c.requireRecovery(active, "发现多个 Runner 实例，无法证明唯一所有权")
	}
	if len(runningInstances) == 0 {
		if active.Stage == opsprotocol.StageRunnerPreparing {
			return c.restartPreparedRunner(ctx, active)
		}
		return c.requireRecovery(active, "Runner 已退出或不存在，且没有终态事实，执行结果未知")
	}
	instance := runningInstances[0]
	if instance.OperationID != active.ID || instance.Generation != active.RunnerGeneration || instance.Digest != active.RunnerDigest {
		return c.requireRecovery(active, "Runner 实例与持久化 generation 或 digest 不一致")
	}
	if !instance.Running {
		return c.requireRecovery(active, "Runner 已退出但没有终态事实")
	}
	lease, err := c.journal.ReadLease(active.ID)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && active.Stage == opsprotocol.StageRunnerPreparing {
			return nil
		}
		return c.requireRecovery(active, "无法读取 Runner 持久租约: "+err.Error())
	}
	if lease.Generation != active.RunnerGeneration || lease.RunnerDigest != active.RunnerDigest {
		return c.requireRecovery(active, "Runner 租约与投影所有权不一致")
	}
	now := time.Now().UTC()
	if !lease.ExpiresAt.After(now) {
		return c.requireRecovery(active, "Runner 租约已过期，不能证明当前所有权")
	}
	if active.HeartbeatAt == nil {
		if now.Sub(lease.AcquiredAt) > c.config.HeartbeatTTL {
			return c.requireRecovery(active, "Runner 在宽限期内未写入心跳")
		}
		return nil
	}
	if now.Sub(active.HeartbeatAt.UTC()) > c.config.HeartbeatTTL {
		return c.requireRecovery(active, "Runner 心跳已过期，执行状态未知")
	}
	return nil
}

func (c *Controller) restartPreparedRunner(ctx context.Context, operation *opsprotocol.Operation) error {
	command, err := c.journal.ReadLaunchCommandForReconciliation(c.commandSecret, operation.ID)
	if err != nil {
		return c.requireRecovery(operation, "Runner 启动命令缺失或验签失败: "+err.Error())
	}
	if command.Generation != operation.RunnerGeneration || command.RunnerDigest != operation.RunnerDigest {
		return c.requireRecovery(operation, "Runner 启动命令与投影所有权不一致")
	}
	if err := c.manager.Start(ctx, RunnerLaunch{
		OperationID: operation.ID, Generation: command.Generation,
		ImageDigest: command.RunnerDigest, StateVolume: c.config.StateVolume,
	}); err != nil {
		return c.requireRecovery(operation, "控制器重启后无法确认 Runner 启动结果: "+err.Error())
	}
	return nil
}

func (c *Controller) requireRecovery(operation *opsprotocol.Operation, message string) error {
	now := time.Now().UTC()
	serviceState := operation.ServiceState
	controllerVersion := operation.ControllerVersionAtStart
	handoff := operation.ControllerHandoff
	recoveryAction := opsprotocol.RecoveryRequireOperator
	if checkpoint, err := c.journal.ReadCheckpoint(operation.ID); err == nil {
		events, eventsErr := c.journal.ReadEvents(operation.ID, checkpoint.Generation, 0)
		if eventsErr != nil {
			return eventsErr
		}
		if len(events) > 0 {
			updated, applied, applyErr := opsrunner.ApplyUnknownStageIntent(
				checkpoint, events[len(events)-1], now,
			)
			if applyErr != nil {
				return applyErr
			}
			if applied {
				updated.FailureMessage = message
				if err := c.journal.WriteCheckpoint(operation.ID, updated); err != nil {
					return err
				}
				checkpoint = updated
			}
		}
		serviceState = checkpoint.ServiceState
		controllerVersion = checkpoint.CurrentControllerVersion
		handoff = checkpoint.ControllerHandoff
		if checkpoint.NextSafeAction != "" {
			recoveryAction = checkpoint.NextSafeAction
		}
	}
	if handoff == "" {
		handoff = opsprotocol.ControllerHandoffUnchanged
	}
	if err := c.journal.WriteResult(operation.ID, opsprotocol.OperationResult{
		OperationID: operation.ID, Generation: operation.RunnerGeneration,
		Status: opsprotocol.OperationRecoveryRequired, ResultVersion: operation.ResultVersion,
		ControllerVersion: controllerVersion, ServiceState: serviceState,
		ControllerHandoff: handoff, ErrorCode: opsprotocol.ErrorStateConflict,
		Error: message, CompletedAt: now,
	}); err != nil {
		return err
	}
	if err := c.store.MarkRecoveryRequired(
		operation.ID, operation.Status, operation.Stage, recoveryAction, message, now,
	); err != nil {
		return err
	}
	if err := c.store.AppendLog(operation.ID, "system", "任务进入恢复处理："+message, now); err != nil {
		return fmt.Errorf("恢复状态已落盘，但审计日志持久化失败: %w", err)
	}
	return nil
}
