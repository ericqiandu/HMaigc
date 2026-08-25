package opscontroller

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"infinite-canvas/backend/internal/buildinfo"
	"infinite-canvas/backend/internal/opsprotocol"
	"infinite-canvas/backend/internal/opsstate"
)

type Config struct {
	StateFile         string
	BackupDir         string
	StateVolume       string
	ControllerVersion string
	ControllerDigest  string
	HeartbeatTTL      time.Duration
	ReportError       func(error)
}

type Controller struct {
	store         *Store
	journal       *opsstate.Journal
	manager       RunnerManager
	projector     *Projector
	commandSecret []byte
	releaseSource ReleaseSource
	config        Config
	wake          chan struct{}
	startMu       sync.Mutex
}

func New(store *Store, journal *opsstate.Journal, manager RunnerManager, commandSecret []byte, releaseSource ReleaseSource, config Config) (*Controller, error) {
	if store == nil {
		return nil, errors.New("运维控制器存储不能为空")
	}
	if journal == nil || manager == nil {
		return nil, errors.New("运维 Journal 或 Runner 管理器不能为空")
	}
	if len(commandSecret) < 32 {
		return nil, errors.New("运维命令签名密钥长度不足")
	}
	if releaseSource == nil {
		return nil, errors.New("运维控制器版本源不能为空")
	}
	if strings.TrimSpace(config.StateFile) == "" || strings.TrimSpace(config.BackupDir) == "" ||
		strings.TrimSpace(config.StateVolume) == "" || strings.TrimSpace(config.ControllerVersion) == "" ||
		!isImmutableImageDigest(config.ControllerDigest) {
		return nil, errors.New("运维控制器状态、卷、版本或不可变镜像摘要未完整配置")
	}
	if config.ReportError == nil {
		return nil, errors.New("运维控制器错误报告器未配置")
	}
	if config.HeartbeatTTL <= 0 {
		config.HeartbeatTTL = 30 * time.Second
	}
	return &Controller{
		store: store, journal: journal, manager: manager,
		projector: NewProjector(store, journal, commandSecret), commandSecret: append([]byte(nil), commandSecret...),
		releaseSource: releaseSource,
		config:        config, wake: make(chan struct{}, 1),
	}, nil
}

func (c *Controller) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := c.Reconcile(ctx); err != nil {
			c.config.ReportError(fmt.Errorf("运维事实对账失败: %w", err))
		}
		if err := c.dispatchQueued(ctx); err != nil {
			c.config.ReportError(fmt.Errorf("运维 Runner 调度失败: %w", err))
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
	existing, err := c.store.OperationByIdempotencyKey(normalized.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	operationID := operationIDForIdempotency(normalized.IdempotencyKey)
	if existing != nil {
		request, readErr := c.journal.ReadRequest(operationID)
		if readErr != nil {
			return nil, readErr
		}
		if request.RequestHash != requestHash {
			return nil, ErrIdempotencyConflict
		}
		return existing, nil
	}
	persisted, readErr := c.journal.ReadRequest(operationID)
	if readErr == nil {
		if persisted.RequestHash != requestHash {
			return nil, ErrIdempotencyConflict
		}
		active, activeErr := c.store.ActiveOperation()
		if activeErr != nil {
			return nil, activeErr
		}
		if active != nil && active.IdempotencyKey != normalized.IdempotencyKey {
			return nil, ErrOperationActive
		}
		operation, _, importErr := c.store.ImportRequest(persisted)
		return operation, importErr
	}
	if !errors.Is(readErr, os.ErrNotExist) {
		return nil, readErr
	}
	journalActive, err := c.hasOtherActiveJournalRequest(operationID)
	if err != nil {
		return nil, err
	}
	if journalActive {
		return nil, ErrOperationActive
	}
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
	expectedVersion, rollbackBackup, err := c.expectedVersionFor(normalized, state)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	runnerSource := opsprotocol.RunnerSourceCurrent
	if normalized.Action == opsprotocol.ActionInstall || normalized.Action == opsprotocol.ActionUpgrade {
		runnerSource = opsprotocol.RunnerSourceTarget
	}
	request := opsprotocol.OperationRequestFile{
		OperationID: operationID, Request: normalized, RequestHash: requestHash,
		CurrentVersion: state.CurrentVersion, PreviousVersion: state.PreviousVersion,
		ExpectedVersion: expectedVersion, RollbackBackup: rollbackBackup,
		RunnerSource: runnerSource, ControllerVersionAtStart: c.config.ControllerVersion,
		CreatedAt: now,
	}
	if err := c.journal.CreateRequest(request); err != nil {
		return nil, err
	}
	operation, created, err := c.store.ImportRequest(request)
	if err != nil {
		return nil, err
	}
	if created {
		select {
		case c.wake <- struct{}{}:
		default:
		}
	}
	return operation, nil
}

func (c *Controller) expectedVersionFor(request opsprotocol.StartOperationRequest, state ReleaseState) (string, string, error) {
	var expected string
	var rollbackBackup string
	switch request.Action {
	case opsprotocol.ActionUpgrade:
		expected = request.TargetVersion
	case opsprotocol.ActionRollback:
		ready, status := inspectRollbackReadiness(c.config.BackupDir, state)
		if !ready {
			return "", "", invalidRequest("无法回滚：" + status)
		}
		expected = state.PreviousVersion
		rollbackBackup = state.RollbackBackup
	case opsprotocol.ActionBackup, opsprotocol.ActionVerify:
		expected = state.CurrentVersion
	default:
		return "", "", invalidRequest("不支持的运维动作")
	}
	if err := opsprotocol.ValidateReleaseVersion(expected); err != nil {
		return "", "", invalidRequest("无法确定运维期望版本：" + err.Error())
	}
	return expected, rollbackBackup, nil
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
	publicVerification, err := c.store.LatestPublicVerification()
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
		PreviousVersion:    state.PreviousVersion,
		PublicVerification: publicVerification,
		UpdatedAt:          time.Now(),
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

func (c *Controller) dispatchQueued(ctx context.Context) error {
	c.startMu.Lock()
	defer c.startMu.Unlock()
	active, err := c.store.ActiveOperation()
	if err != nil || active == nil {
		return err
	}
	if active.Status != opsprotocol.OperationQueued {
		return nil
	}
	operation := *active
	request, err := c.journal.ReadRequest(operation.ID)
	if err != nil {
		return err
	}
	resolved, command, err := c.resolveLaunch(ctx, request)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := c.store.MarkRunnerStarted(operation.ID, resolved, command.Generation, now); err != nil {
		return err
	}
	launch := RunnerLaunch{
		OperationID: operation.ID, Generation: command.Generation,
		ImageDigest: command.RunnerDigest, StateVolume: c.config.StateVolume,
	}
	if err := c.manager.Start(ctx, launch); err != nil {
		message := "启动独立 Runner 失败: " + err.Error()
		if errors.Is(err, ErrRunnerStartOutcomeUnknown) {
			startedOperation, readErr := c.store.Operation(operation.ID)
			if readErr != nil {
				return errors.Join(err, readErr)
			}
			if recoveryErr := c.requireRecovery(startedOperation, message); recoveryErr != nil {
				return errors.Join(err, recoveryErr)
			}
			return err
		}
		completedAt := time.Now().UTC()
		serviceState := opsprotocol.ServiceUnknown
		if request.CurrentVersion != "" {
			serviceState = opsprotocol.ServiceCurrentOnline
		}
		if persistErr := c.journal.WriteResult(operation.ID, opsprotocol.OperationResult{
			OperationID: operation.ID, Generation: command.Generation,
			Status: opsprotocol.OperationFailed, ResultVersion: request.CurrentVersion,
			ControllerVersion: request.ControllerVersionAtStart, ServiceState: serviceState,
			ControllerHandoff: opsprotocol.ControllerHandoffUnchanged,
			ErrorCode:         opsprotocol.ErrorRunnerStartFailed, Error: message, CompletedAt: completedAt,
		}); persistErr != nil {
			return errors.Join(err, persistErr)
		}
		if persistErr := c.store.MarkRunnerStartFailed(operation.ID, message, completedAt); persistErr != nil {
			return errors.Join(err, persistErr)
		}
		return err
	}
	return nil
}

func (c *Controller) hasOtherActiveJournalRequest(operationID string) (bool, error) {
	requests, err := c.journal.ListRequests()
	if err != nil {
		return false, err
	}
	for _, request := range requests {
		if request.OperationID == operationID {
			continue
		}
		result, readErr := c.journal.ReadResult(request.OperationID)
		if readErr == nil {
			if opsprotocol.IsTerminalStatus(result.Status) {
				continue
			}
			return true, nil
		}
		if errors.Is(readErr, os.ErrNotExist) {
			return true, nil
		}
		return false, readErr
	}
	return false, nil
}

func (c *Controller) resolveLaunch(ctx context.Context, request opsprotocol.OperationRequestFile) (ResolvedRunner, opsprotocol.RunnerLaunchCommand, error) {
	existing, err := c.journal.ReadLaunchCommand(c.commandSecret, request.OperationID)
	if err == nil {
		return ResolvedRunner{Version: runnerVersionForRequest(request), Digest: existing.RunnerDigest}, existing, nil
	}
	generation := uint64(1)
	if errors.Is(err, opsstate.ErrCommandExpired) {
		stale, readErr := c.journal.ReadLaunchCommandForReconciliation(c.commandSecret, request.OperationID)
		if readErr != nil {
			return ResolvedRunner{}, opsprotocol.RunnerLaunchCommand{}, readErr
		}
		generation = stale.Generation + 1
	} else if !errors.Is(err, os.ErrNotExist) {
		return ResolvedRunner{}, opsprotocol.RunnerLaunchCommand{}, err
	}
	resolved, err := c.manager.Resolve(ctx, request)
	if err != nil {
		return ResolvedRunner{}, opsprotocol.RunnerLaunchCommand{}, err
	}
	token, err := newFencingToken()
	if err != nil {
		return ResolvedRunner{}, opsprotocol.RunnerLaunchCommand{}, err
	}
	command := opsprotocol.RunnerLaunchCommand{
		OperationID: request.OperationID, Generation: generation, FencingToken: token,
		RunnerDigest: resolved.Digest, IssuedAt: time.Now().UTC(),
	}
	if err := c.journal.WriteLaunchCommand(c.commandSecret, command); err != nil {
		return ResolvedRunner{}, opsprotocol.RunnerLaunchCommand{}, err
	}
	return resolved, command, nil
}

func runnerVersionForRequest(request opsprotocol.OperationRequestFile) string {
	if request.RunnerSource == opsprotocol.RunnerSourceTarget {
		return request.ExpectedVersion
	}
	return request.ControllerVersionAtStart
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
		return input, invalidRequest("首次部署必须使用一次性 bootstrap，不能进入升级任务队列")
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
	if input.Action == opsprotocol.ActionUpgrade {
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

func normalizeLimit(value int, fallback int, maximum int) int {
	if value <= 0 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func operationIDForIdempotency(idempotencyKey string) string {
	sum := sha256.Sum256([]byte(idempotencyKey))
	return hex.EncodeToString(sum[:16])
}

func newFencingToken() (string, error) {
	var buffer [32]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer[:]), nil
}
