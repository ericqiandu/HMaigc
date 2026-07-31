package opscontroller

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"infinite-canvas/backend/internal/buildinfo"
	"infinite-canvas/backend/internal/opsprotocol"
)

type Config struct {
	StateFile string
	BackupDir string
}

type Controller struct {
	store         *Store
	executor      Executor
	releaseSource ReleaseSource
	config        Config
	wake          chan struct{}
	startMu       sync.Mutex
}

func New(store *Store, executor Executor, releaseSource ReleaseSource, config Config) (*Controller, error) {
	if store == nil {
		return nil, errors.New("运维控制器存储不能为空")
	}
	if executor == nil {
		return nil, errors.New("运维控制器执行器不能为空")
	}
	if releaseSource == nil {
		return nil, errors.New("运维控制器版本源不能为空")
	}
	if strings.TrimSpace(config.StateFile) == "" || strings.TrimSpace(config.BackupDir) == "" {
		return nil, errors.New("运维控制器状态路径未配置")
	}
	if err := store.FailInterruptedOperations(time.Now()); err != nil {
		return nil, err
	}
	return &Controller{
		store: store, executor: executor, releaseSource: releaseSource,
		config: config, wake: make(chan struct{}, 1),
	}, nil
}

func (c *Controller) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := c.executeNext(ctx); err != nil {
			// 任务级错误已持久化；下一轮继续检查队列。
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-c.wake:
		}
	}
}

func (c *Controller) StartOperation(input opsprotocol.StartOperationRequest) (*opsprotocol.Operation, error) {
	normalized, err := normalizeOperationRequest(input)
	if err != nil {
		return nil, err
	}
	requestHash, err := operationRequestHash(normalized)
	if err != nil {
		return nil, err
	}
	c.startMu.Lock()
	defer c.startMu.Unlock()
	active, err := c.store.ActiveOperation()
	if err != nil {
		return nil, err
	}
	if active != nil && active.IdempotencyKey != normalized.IdempotencyKey {
		return nil, ErrOperationActive
	}
	state, err := ReadReleaseState(c.config.StateFile)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	record := operationRecord{
		ID: newOperationID(), Action: string(normalized.Action), TargetVersion: normalized.TargetVersion,
		CurrentVersionAtStart: state.CurrentVersion, Status: string(opsprotocol.OperationQueued),
		Phase: "等待控制器调度", ActorUserID: normalized.ActorUserID,
		ActorDisplayName: normalized.ActorDisplayName, IdempotencyKey: normalized.IdempotencyKey,
		RequestHash: requestHash, CreatedAt: now, UpdatedAt: now,
	}
	operation, created, err := c.store.CreateOrGetOperation(record)
	if err != nil {
		return nil, err
	}
	if created {
		if err := c.store.AppendLog(operation.ID, "system", "管理员请求已验签并进入独立部署队列", now); err != nil {
			return nil, err
		}
		select {
		case c.wake <- struct{}{}:
		default:
		}
	}
	return operation, nil
}

func (c *Controller) Overview(ctx context.Context) (*opsprotocol.Overview, error) {
	state, err := ReadReleaseState(c.config.StateFile)
	if err != nil {
		return nil, err
	}
	active, err := c.store.ActiveOperation()
	if err != nil {
		return nil, err
	}
	latest, err := c.store.LatestOperation()
	if err != nil {
		return nil, err
	}
	backups, err := ScanBackups(c.config.BackupDir, 1)
	if err != nil {
		return nil, err
	}
	var latestBackup *opsprotocol.Backup
	if len(backups) > 0 {
		latestBackup = &backups[0]
	}
	rollbackReady, rollbackStatus := inspectRollbackReadiness(c.config.BackupDir, state)
	return &opsprotocol.Overview{
		Controller: opsprotocol.ControllerInfo{Status: "ok", Version: buildinfo.Version, Commit: buildinfo.Commit},
		Release:    c.releaseSource.Check(ctx, state.CurrentVersion), ActiveOperation: active,
		LatestOperation: latest, LatestBackup: latestBackup,
		RollbackReady: rollbackReady, RollbackStatus: rollbackStatus,
		PreviousVersion: state.PreviousVersion, UpdatedAt: time.Now(),
	}, nil
}

func (c *Controller) Operations(limit int) (*opsprotocol.OperationPage, error) {
	return c.store.Operations(normalizeLimit(limit, 20, 100))
}

func (c *Controller) Operation(id string) (*opsprotocol.Operation, error) {
	return c.store.Operation(strings.TrimSpace(id))
}

func (c *Controller) OperationLogs(id string, after uint64, limit int) (*opsprotocol.OperationLogPage, error) {
	if _, err := c.store.Operation(strings.TrimSpace(id)); err != nil {
		return nil, err
	}
	return c.store.OperationLogs(strings.TrimSpace(id), after, normalizeLimit(limit, 200, 500))
}

func (c *Controller) Backups(limit int) ([]opsprotocol.Backup, error) {
	return ScanBackups(c.config.BackupDir, normalizeLimit(limit, 20, 100))
}

func (c *Controller) executeNext(ctx context.Context) error {
	operation, err := c.store.ClaimNextOperation(time.Now())
	if err != nil || operation == nil {
		return err
	}
	phase := operationPhase(operation.Action)
	now := time.Now()
	if err := c.store.UpdatePhase(operation.ID, phase, now); err != nil {
		return c.failClaimedOperation(operation.ID, nil, "控制器无法更新任务阶段: "+err.Error())
	}
	_ = c.store.AppendLog(operation.ID, "system", phase, now)
	var logMu sync.Mutex
	var logErrors []error
	result, executionErr := c.executor.Execute(ctx, operation.Action, operation.TargetVersion, func(stream string, message string) {
		message = strings.TrimSpace(message)
		if message == "" {
			return
		}
		if err := c.store.AppendLog(operation.ID, stream, message, time.Now()); err != nil {
			// 日志持久化失败必须让最终状态显式失败，不能静默继续。
			logMu.Lock()
			logErrors = append(logErrors, fmt.Errorf("持久化实时日志失败: %w", err))
			logMu.Unlock()
		}
	})
	if len(logErrors) > 0 {
		executionErr = errors.Join(append([]error{executionErr}, logErrors...)...)
	}
	exitCode := result.ExitCode
	state, stateErr := ReadReleaseState(c.config.StateFile)
	if stateErr != nil {
		executionErr = errors.Join(executionErr, fmt.Errorf("读取执行后发布状态失败: %w", stateErr))
	}
	if executionErr != nil {
		message := executionErr.Error()
		_ = c.store.AppendLog(operation.ID, "system", "任务失败："+message, time.Now())
		return c.store.CompleteOperation(operation.ID, opsprotocol.OperationFailed, "执行失败", state.CurrentVersion, &exitCode, message, time.Now())
	}
	if err := c.store.AppendLog(operation.ID, "system", "任务已完成并通过部署脚本验收", time.Now()); err != nil {
		return c.failClaimedOperation(operation.ID, &exitCode, "任务完成但审计日志持久化失败: "+err.Error())
	}
	return c.store.CompleteOperation(operation.ID, opsprotocol.OperationSucceeded, "已完成", state.CurrentVersion, &exitCode, "", time.Now())
}

func (c *Controller) failClaimedOperation(id string, exitCode *int, message string) error {
	_ = c.store.AppendLog(id, "system", message, time.Now())
	return c.store.CompleteOperation(id, opsprotocol.OperationFailed, "控制器失败", "", exitCode, message, time.Now())
}

func normalizeOperationRequest(input opsprotocol.StartOperationRequest) (opsprotocol.StartOperationRequest, error) {
	input.TargetVersion = strings.TrimSpace(input.TargetVersion)
	input.ActorUserID = strings.TrimSpace(input.ActorUserID)
	input.ActorDisplayName = strings.TrimSpace(input.ActorDisplayName)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.Confirmation = strings.TrimSpace(input.Confirmation)
	if input.ActorUserID == "" || len(input.ActorUserID) > 64 {
		return input, invalidRequest("管理员标识无效")
	}
	if input.ActorDisplayName == "" || len([]rune(input.ActorDisplayName)) > 80 {
		return input, invalidRequest("管理员名称无效")
	}
	if !validIdempotencyKey(input.IdempotencyKey) {
		return input, invalidRequest("幂等键必须是 16-128 位字母、数字、下划线或连字符")
	}
	expectedConfirmation := ""
	switch input.Action {
	case opsprotocol.ActionInstall:
		expectedConfirmation = "INSTALL " + input.TargetVersion
	case opsprotocol.ActionUpgrade:
		expectedConfirmation = "UPGRADE " + input.TargetVersion
	case opsprotocol.ActionRollback:
		expectedConfirmation = "ROLLBACK"
	case opsprotocol.ActionBackup:
		expectedConfirmation = "BACKUP"
	case opsprotocol.ActionVerify:
		expectedConfirmation = "VERIFY"
	default:
		return input, invalidRequest("不支持的运维动作")
	}
	if input.Action == opsprotocol.ActionInstall || input.Action == opsprotocol.ActionUpgrade {
		if err := opsprotocol.ValidateReleaseVersion(input.TargetVersion); err != nil {
			return input, invalidRequest(err.Error())
		}
	} else if input.TargetVersion != "" {
		return input, invalidRequest("当前运维动作不能指定目标版本")
	}
	if input.Confirmation != expectedConfirmation {
		return input, invalidRequest(fmt.Sprintf("确认短语不匹配，应为 %s", expectedConfirmation))
	}
	return input, nil
}

func operationRequestHash(input opsprotocol.StartOperationRequest) (string, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

func validIdempotencyKey(value string) bool {
	if len(value) < 16 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' ||
			char >= '0' && char <= '9' || char == '-' || char == '_' {
			continue
		}
		return false
	}
	return true
}

func operationPhase(action opsprotocol.Action) string {
	switch action {
	case opsprotocol.ActionInstall:
		return "执行首次安装与验活"
	case opsprotocol.ActionUpgrade:
		return "备份当前版本并执行升级"
	case opsprotocol.ActionRollback:
		return "创建安全备份并执行回滚"
	case opsprotocol.ActionBackup:
		return "停止写入并创建一致性备份"
	case opsprotocol.ActionVerify:
		return "校验版本、依赖与公网入口"
	default:
		return "执行运维任务"
	}
}

func normalizeLimit(value int, fallback int, maximum int) int {
	if value <= 0 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func newOperationID() string {
	var buffer [16]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buffer[:])
}
