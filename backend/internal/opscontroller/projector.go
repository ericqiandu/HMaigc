package opscontroller

import (
	"errors"
	"io/fs"
	"time"

	"infinite-canvas/backend/internal/opsprotocol"
	"infinite-canvas/backend/internal/opsstate"
)

type Projector struct {
	store         *Store
	journal       *opsstate.Journal
	commandSecret []byte
}

func NewProjector(store *Store, journal *opsstate.Journal, commandSecret []byte) *Projector {
	return &Projector{store: store, journal: journal, commandSecret: append([]byte(nil), commandSecret...)}
}

func (p *Projector) Project(operationID string) error {
	request, err := p.journal.ReadRequest(operationID)
	if err != nil {
		return err
	}
	if _, _, err := p.store.ImportRequest(request); err != nil {
		return err
	}
	current, err := p.store.Operation(operationID)
	if err != nil {
		return err
	}

	projection := operationProjection{
		Status: current.Status, Stage: current.Stage, Phase: current.Phase,
		RunnerVersion: current.RunnerVersion, RunnerDigest: current.RunnerDigest,
		RunnerGeneration: current.RunnerGeneration, StartedAt: current.StartedAt,
		HeartbeatAt: current.HeartbeatAt, ServiceState: current.ServiceState,
		CheckpointSequence: current.CheckpointSequence, RecoveryAction: current.RecoveryAction,
		CancelRequestedAt:       current.CancelRequestedAt,
		ResultVersion:           current.ResultVersion,
		ResultControllerVersion: current.ResultControllerVersion,
		ControllerHandoff:       current.ControllerHandoff,
		Warnings:                append([]opsprotocol.OperationWarning(nil), current.Warnings...),
		ErrorCode:               current.ErrorCode, Error: current.Error, ExitCode: current.ExitCode,
		CompletedAt: current.CompletedAt, UpdatedAt: current.UpdatedAt,
	}
	generation := current.RunnerGeneration

	lease, err := p.journal.ReadLease(operationID)
	if err == nil && lease.Generation >= generation {
		generation = lease.Generation
		projection.RunnerVersion = runnerVersionForRequest(request)
		projection.RunnerGeneration = lease.Generation
		projection.RunnerDigest = lease.RunnerDigest
		if projection.StartedAt == nil {
			startedAt := lease.AcquiredAt
			projection.StartedAt = &startedAt
		}
		if projection.Status == opsprotocol.OperationQueued {
			projection.Status = opsprotocol.OperationRunning
			projection.Stage = opsprotocol.StageRunnerPreparing
			projection.Phase = phaseForStage(projection.Stage)
		}
		projection.UpdatedAt = laterTime(projection.UpdatedAt, lease.AcquiredAt)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	heartbeat, err := p.journal.ReadHeartbeat(operationID)
	if err == nil && heartbeat.Generation >= generation {
		generation = heartbeat.Generation
		projection.RunnerGeneration = heartbeat.Generation
		projection.HeartbeatAt = &heartbeat.ObservedAt
		projection.Stage = heartbeat.Stage
		projection.ServiceState = heartbeat.ServiceState
		projection.CheckpointSequence = heartbeat.Sequence
		if !preserveControlStatus(projection.Status) {
			projection.Status = opsprotocol.OperationRunning
		}
		projection.Phase = phaseForStage(heartbeat.Stage)
		projection.UpdatedAt = laterTime(projection.UpdatedAt, heartbeat.ObservedAt)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	checkpoint, err := p.journal.ReadCheckpoint(operationID)
	if err == nil && checkpoint.Generation >= generation {
		generation = checkpoint.Generation
		projection.RunnerVersion = runnerVersionForRequest(request)
		projection.RunnerGeneration = checkpoint.Generation
		projection.RunnerDigest = checkpoint.RunnerDigest
		projection.Stage = checkpoint.Stage
		projection.ServiceState = checkpoint.ServiceState
		projection.CheckpointSequence = checkpoint.Sequence
		projection.RecoveryAction = checkpoint.NextSafeAction
		projection.Warnings = append([]opsprotocol.OperationWarning(nil), checkpoint.Warnings...)
		projection.ErrorCode = checkpoint.FailureCode
		projection.Error = checkpoint.FailureMessage
		if projection.StartedAt == nil && checkpoint.StageStartedAt != nil {
			startedAt := checkpoint.StageStartedAt.UTC()
			projection.StartedAt = &startedAt
		}
		if !preserveControlStatus(projection.Status) {
			projection.Status = opsprotocol.OperationRunning
		}
		projection.Phase = phaseForStage(checkpoint.Stage)
		projection.UpdatedAt = laterTime(projection.UpdatedAt, checkpoint.UpdatedAt)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	result, err := p.journal.ReadResult(operationID)
	if err == nil && result.Generation >= generation {
		generation = result.Generation
		projection.RunnerGeneration = result.Generation
		projection.Status = result.Status
		projection.Stage = opsprotocol.StageCompleted
		projection.Phase = phaseForResult(result.Status, projection.RecoveryAction)
		projection.ResultVersion = result.ResultVersion
		projection.ResultControllerVersion = result.ControllerVersion
		projection.ServiceState = result.ServiceState
		projection.ControllerHandoff = result.ControllerHandoff
		projection.Warnings = append([]opsprotocol.OperationWarning(nil), result.Warnings...)
		projection.ErrorCode = result.ErrorCode
		projection.Error = result.Error
		projection.ExitCode = result.ExitCode
		projection.CompletedAt = &result.CompletedAt
		projection.UpdatedAt = laterTime(projection.UpdatedAt, result.CompletedAt)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	cancel, err := p.journal.ReadCancelCommand(p.commandSecret, operationID)
	if err == nil && !opsprotocol.IsTerminalStatus(projection.Status) && projection.Status != opsprotocol.OperationRecoveryRequired {
		projection.Status = opsprotocol.OperationCancelling
		projection.Phase = "已收到停止请求，等待安全点"
		requestedAt := cancel.RequestedAt.UTC()
		projection.CancelRequestedAt = &requestedAt
		projection.UpdatedAt = laterTime(projection.UpdatedAt, requestedAt)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	if generation > 0 {
		for eventGeneration := uint64(1); eventGeneration <= generation; eventGeneration++ {
			events, err := p.journal.ReadEvents(operationID, eventGeneration, 0)
			if err != nil {
				return err
			}
			for _, event := range events {
				if err := p.store.AppendProjectedEvent(event); err != nil {
					return err
				}
			}
		}
	}
	return p.store.ProjectOperation(operationID, current.Status, current.Stage, projection)
}

func preserveControlStatus(status opsprotocol.OperationStatus) bool {
	switch status {
	case opsprotocol.OperationCancelling, opsprotocol.OperationRecovering, opsprotocol.OperationRecoveryRequired:
		return true
	default:
		return false
	}
}

func phaseForStage(stage opsprotocol.OperationStage) string {
	switch stage {
	case opsprotocol.StageAccepted:
		return "等待控制器调度"
	case opsprotocol.StageRunnerPreparing:
		return "准备独立 Runner"
	case opsprotocol.StageOnlinePreflight:
		return "在线预检"
	case opsprotocol.StagePublicVerifying:
		return "校验公网静态资源"
	case opsprotocol.StageQuiescing:
		return "停止新写入"
	case opsprotocol.StageQuiescedAudit:
		return "停写后审计"
	case opsprotocol.StageBackingUp:
		return "创建一致性备份"
	case opsprotocol.StageStartingTarget:
		return "启动目标版本"
	case opsprotocol.StageVerifyingTarget:
		return "验证目标版本"
	case opsprotocol.StageRestoringCurrent:
		return "恢复当前版本"
	case opsprotocol.StageRestoringBackup:
		return "恢复已验证备份"
	case opsprotocol.StageRestoringRollbackBackup:
		return "恢复上一版本数据"
	case opsprotocol.StageCommittingRelease:
		return "提交发布事实"
	case opsprotocol.StageControllerHandoff:
		return "切换运维控制器"
	case opsprotocol.StageCompleted:
		return "已完成"
	default:
		return "未知运维阶段"
	}
}

func phaseForResult(status opsprotocol.OperationStatus, recoveryAction opsprotocol.RecoveryAction) string {
	switch status {
	case opsprotocol.OperationSucceeded:
		return "已完成"
	case opsprotocol.OperationCancelled:
		return "已安全停止"
	case opsprotocol.OperationRecoveryRequired:
		return phaseForRecoveryAction(recoveryAction)
	default:
		return "执行失败"
	}
}

func phaseForRecoveryAction(action opsprotocol.RecoveryAction) string {
	switch action {
	case opsprotocol.RecoveryRestoreCurrent:
		return "等待恢复当前版本"
	case opsprotocol.RecoveryRestoreBackup:
		return "等待恢复已校验备份"
	case opsprotocol.RecoveryCommitTarget:
		return "等待提交目标版本"
	case opsprotocol.RecoveryContinueControllerHandoff:
		return "等待继续控制器交接"
	default:
		return "等待人工确认恢复动作"
	}
}

func laterTime(current time.Time, candidate time.Time) time.Time {
	if candidate.After(current) {
		return candidate
	}
	return current
}
