package opscontroller

import (
	"encoding/json"
	"errors"
	"os"
	"time"

	"infinite-canvas/backend/internal/opsprotocol"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type operationRecord struct {
	ID                       string `gorm:"primaryKey;size:36"`
	Action                   string `gorm:"index;size:24"`
	TargetVersion            string `gorm:"size:80"`
	CurrentVersionAtStart    string `gorm:"size:80"`
	ResultVersion            string `gorm:"size:80"`
	Status                   string `gorm:"index;size:24"`
	Stage                    string `gorm:"size:48"`
	Phase                    string `gorm:"size:160"`
	RunnerVersion            string `gorm:"size:80"`
	RunnerDigest             string `gorm:"size:256"`
	RunnerGeneration         uint64
	HeartbeatAt              *time.Time
	ServiceState             string `gorm:"size:32"`
	CheckpointSequence       uint64
	CancelRequestedAt        *time.Time
	RecoveryAction           string `gorm:"size:64"`
	ControllerVersionAtStart string `gorm:"size:80"`
	ResultControllerVersion  string `gorm:"size:80"`
	ControllerHandoff        string `gorm:"size:48"`
	WarningsJSON             string `gorm:"type:text"`
	ErrorCode                string `gorm:"size:64"`
	Error                    string `gorm:"type:text"`
	ExitCode                 *int
	ActorUserID              string    `gorm:"index;size:36"`
	ActorDisplayName         string    `gorm:"size:160"`
	IdempotencyKey           string    `gorm:"uniqueIndex;size:128"`
	RequestHash              string    `gorm:"size:64"`
	CreatedAt                time.Time `gorm:"index"`
	StartedAt                *time.Time
	CompletedAt              *time.Time
	UpdatedAt                time.Time
}

type operationLogRecord struct {
	Sequence    uint64    `gorm:"primaryKey;autoIncrement"`
	OperationID string    `gorm:"index;size:36"`
	Stream      string    `gorm:"size:16"`
	Message     string    `gorm:"type:text"`
	CreatedAt   time.Time `gorm:"index"`
}

type operationEventProjectionRecord struct {
	OperationID   string `gorm:"primaryKey;size:64"`
	Generation    uint64 `gorm:"primaryKey"`
	EventSequence uint64 `gorm:"primaryKey"`
}

type Store struct {
	db *gorm.DB
}

func OpenStore(path string) (*Store, error) {
	dsn := path + "?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=on&_synchronous=FULL"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	if err := db.AutoMigrate(&operationRecord{}, &operationLogRecord{}, &operationEventProjectionRecord{}); err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (s *Store) CreateOrGetOperation(record operationRecord) (*opsprotocol.Operation, bool, error) {
	var result operationRecord
	created := false
	err := s.db.Transaction(func(tx *gorm.DB) error {
		query := tx.First(&result, "idempotency_key = ?", record.IdempotencyKey)
		if query.Error == nil {
			if result.RequestHash != record.RequestHash {
				return ErrIdempotencyConflict
			}
			return nil
		}
		if !errors.Is(query.Error, gorm.ErrRecordNotFound) {
			return query.Error
		}
		if err := tx.Create(&record).Error; err != nil {
			if lookupErr := tx.First(&result, "idempotency_key = ?", record.IdempotencyKey).Error; lookupErr == nil {
				if result.RequestHash != record.RequestHash {
					return ErrIdempotencyConflict
				}
				return nil
			}
			return err
		}
		result = record
		created = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	operation, err := operationFromRecord(result)
	if err != nil {
		return nil, false, err
	}
	return &operation, created, nil
}

func (s *Store) ImportRequest(request opsprotocol.OperationRequestFile) (*opsprotocol.Operation, bool, error) {
	serviceState := opsprotocol.ServiceUnknown
	if request.CurrentVersion != "" {
		serviceState = opsprotocol.ServiceCurrentOnline
	}
	record := operationRecord{
		ID: request.OperationID, Action: string(request.Request.Action),
		TargetVersion: operationTargetVersion(request), CurrentVersionAtStart: request.CurrentVersion,
		Status: string(opsprotocol.OperationQueued), Stage: string(opsprotocol.StageAccepted),
		Phase: "等待控制器调度", ServiceState: string(serviceState),
		ControllerVersionAtStart: request.ControllerVersionAtStart,
		ActorUserID:              request.Request.ActorUserID, ActorDisplayName: request.Request.ActorDisplayName,
		IdempotencyKey: request.Request.IdempotencyKey, RequestHash: request.RequestHash,
		CreatedAt: request.CreatedAt, UpdatedAt: request.CreatedAt,
	}
	return s.CreateOrGetOperation(record)
}

func operationTargetVersion(request opsprotocol.OperationRequestFile) string {
	switch request.Request.Action {
	case opsprotocol.ActionInstall, opsprotocol.ActionUpgrade, opsprotocol.ActionRollback:
		return request.ExpectedVersion
	default:
		return ""
	}
}

func (s *Store) OperationByIdempotencyKey(key string) (*opsprotocol.Operation, error) {
	var record operationRecord
	result := s.db.Where("idempotency_key = ?", key).Limit(1).Find(&record)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	operation, err := operationFromRecord(record)
	if err != nil {
		return nil, err
	}
	return &operation, nil
}

func (s *Store) QueuedOperations(limit int) ([]opsprotocol.Operation, error) {
	var records []operationRecord
	if err := s.db.Where("status = ?", opsprotocol.OperationQueued).
		Order("created_at asc").Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	operations := make([]opsprotocol.Operation, 0, len(records))
	for _, record := range records {
		operation, err := operationFromRecord(record)
		if err != nil {
			return nil, err
		}
		operations = append(operations, operation)
	}
	return operations, nil
}

func (s *Store) MarkRunnerStarted(id string, runner ResolvedRunner, generation uint64, now time.Time) error {
	result := s.db.Model(&operationRecord{}).
		Where("id = ? AND status = ? AND stage = ?", id, opsprotocol.OperationQueued, opsprotocol.StageAccepted).
		Updates(map[string]interface{}{
			"status": opsprotocol.OperationRunning, "stage": opsprotocol.StageRunnerPreparing,
			"phase": "独立 Runner 已调度", "runner_version": runner.Version,
			"runner_digest": runner.Digest, "runner_generation": generation,
			"started_at": now, "updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("运维任务状态已变化，拒绝重复调度 Runner")
	}
	return nil
}

func (s *Store) MarkRunnerStartFailed(id string, message string, now time.Time) error {
	result := s.db.Model(&operationRecord{}).
		Where("id = ? AND status = ? AND stage = ?", id, opsprotocol.OperationRunning, opsprotocol.StageRunnerPreparing).
		Updates(map[string]interface{}{
			"status": opsprotocol.OperationFailed, "phase": "Runner 启动失败",
			"error_code": opsprotocol.ErrorRunnerStartFailed, "error": message,
			"completed_at": now, "updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("运维任务状态已变化，拒绝覆盖 Runner 启动失败事实")
	}
	return nil
}

func (s *Store) MarkCancelling(id string, expectedStatus opsprotocol.OperationStatus, now time.Time) error {
	if err := opsprotocol.ValidateStatusTransition(expectedStatus, opsprotocol.OperationCancelling); err != nil {
		return err
	}
	result := s.db.Model(&operationRecord{}).
		Where("id = ? AND status = ?", id, expectedStatus).
		Updates(map[string]interface{}{
			"status": opsprotocol.OperationCancelling, "phase": "已收到停止请求，等待安全点",
			"cancel_requested_at": now, "updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("运维任务状态已变化，拒绝覆盖停止事实")
	}
	return nil
}

func (s *Store) MarkRecovering(id string, stage opsprotocol.OperationStage, runner ResolvedRunner, generation uint64, now time.Time) error {
	result := s.db.Model(&operationRecord{}).
		Where("id = ? AND status = ?", id, opsprotocol.OperationRecoveryRequired).
		Updates(map[string]interface{}{
			"status": opsprotocol.OperationRecovering, "stage": stage,
			"phase":          "已验证旧 Runner 停止，正在安全恢复",
			"runner_version": runner.Version, "runner_digest": runner.Digest,
			"runner_generation": generation, "error_code": "", "error": "", "updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("运维任务状态已变化，拒绝重复启动恢复 Runner")
	}
	return nil
}

func (s *Store) MarkRecoveryLaunchFailed(
	id string,
	generation uint64,
	recoveryAction opsprotocol.RecoveryAction,
	message string,
	now time.Time,
) error {
	result := s.db.Model(&operationRecord{}).
		Where("id = ? AND status = ? AND runner_generation = ?", id, opsprotocol.OperationRecovering, generation).
		Updates(map[string]interface{}{
			"status": opsprotocol.OperationRecoveryRequired, "phase": "恢复 Runner 启动失败",
			"error_code": opsprotocol.ErrorRunnerStartFailed, "error": message,
			"recovery_action": recoveryAction, "updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("运维任务状态已变化，拒绝覆盖恢复启动失败事实")
	}
	return nil
}

type operationProjection struct {
	Status                  opsprotocol.OperationStatus
	Stage                   opsprotocol.OperationStage
	Phase                   string
	RunnerDigest            string
	RunnerVersion           string
	RunnerGeneration        uint64
	StartedAt               *time.Time
	HeartbeatAt             *time.Time
	ServiceState            opsprotocol.ServiceState
	CheckpointSequence      uint64
	CancelRequestedAt       *time.Time
	RecoveryAction          opsprotocol.RecoveryAction
	ResultVersion           string
	ResultControllerVersion string
	ControllerHandoff       opsprotocol.ControllerHandoff
	Warnings                []opsprotocol.OperationWarning
	ErrorCode               opsprotocol.OperationErrorCode
	Error                   string
	ExitCode                *int
	CompletedAt             *time.Time
	UpdatedAt               time.Time
}

func (s *Store) ProjectOperation(id string, expectedStatus opsprotocol.OperationStatus, expectedStage opsprotocol.OperationStage, projection operationProjection) error {
	warnings, err := json.Marshal(projection.Warnings)
	if err != nil {
		return err
	}
	result := s.db.Model(&operationRecord{}).
		Where("id = ? AND status = ? AND stage = ?", id, expectedStatus, expectedStage).
		Updates(map[string]interface{}{
			"status": projection.Status, "stage": projection.Stage, "phase": projection.Phase,
			"runner_version": projection.RunnerVersion, "runner_digest": projection.RunnerDigest,
			"runner_generation": projection.RunnerGeneration, "started_at": projection.StartedAt,
			"heartbeat_at": projection.HeartbeatAt, "service_state": projection.ServiceState,
			"checkpoint_sequence": projection.CheckpointSequence, "recovery_action": projection.RecoveryAction,
			"cancel_requested_at":       projection.CancelRequestedAt,
			"result_version":            projection.ResultVersion,
			"result_controller_version": projection.ResultControllerVersion,
			"controller_handoff":        projection.ControllerHandoff, "warnings_json": string(warnings),
			"error_code": projection.ErrorCode, "error": projection.Error,
			"exit_code": projection.ExitCode, "completed_at": projection.CompletedAt,
			"updated_at": projection.UpdatedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("运维任务状态或阶段已变化，拒绝覆盖投影")
	}
	return nil
}

func (s *Store) MarkRecoveryRequired(
	id string,
	expectedStatus opsprotocol.OperationStatus,
	expectedStage opsprotocol.OperationStage,
	recoveryAction opsprotocol.RecoveryAction,
	message string,
	now time.Time,
) error {
	phase := phaseForRecoveryAction(recoveryAction)
	result := s.db.Model(&operationRecord{}).
		Where("id = ? AND status = ? AND stage = ?", id, expectedStatus, expectedStage).
		Updates(map[string]interface{}{
			"status": opsprotocol.OperationRecoveryRequired, "phase": phase,
			"error_code": opsprotocol.ErrorStateConflict, "error": message,
			"recovery_action": recoveryAction, "updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("运维任务状态或阶段已变化，拒绝覆盖恢复事实")
	}
	return nil
}

func (s *Store) AppendProjectedEvent(event opsprotocol.OperationEvent) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		marker := operationEventProjectionRecord{
			OperationID: event.OperationID, Generation: event.Generation, EventSequence: event.Sequence,
		}
		result := tx.Create(&marker)
		if result.Error != nil {
			var existing operationEventProjectionRecord
			if lookupErr := tx.First(&existing,
				"operation_id = ? AND generation = ? AND event_sequence = ?",
				event.OperationID, event.Generation, event.Sequence,
			).Error; lookupErr == nil {
				return nil
			}
			return result.Error
		}
		return tx.Create(&operationLogRecord{
			OperationID: event.OperationID, Stream: event.Stream,
			Message: event.Message, CreatedAt: event.CreatedAt,
		}).Error
	})
}

func (s *Store) AppendLog(operationID string, stream string, message string, now time.Time) error {
	return s.db.Create(&operationLogRecord{
		OperationID: operationID,
		Stream:      stream,
		Message:     message,
		CreatedAt:   now,
	}).Error
}

func (s *Store) Operation(id string) (*opsprotocol.Operation, error) {
	var record operationRecord
	if err := s.db.First(&record, "id = ?", id).Error; err != nil {
		return nil, err
	}
	operation, err := operationFromRecord(record)
	if err != nil {
		return nil, err
	}
	return &operation, nil
}

func (s *Store) Operations(limit int) (*opsprotocol.OperationPage, error) {
	var total int64
	if err := s.db.Model(&operationRecord{}).Count(&total).Error; err != nil {
		return nil, err
	}
	var records []operationRecord
	if err := s.db.Order("created_at desc").Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	items := make([]opsprotocol.Operation, 0, len(records))
	for _, record := range records {
		operation, err := operationFromRecord(record)
		if err != nil {
			return nil, err
		}
		items = append(items, operation)
	}
	return &opsprotocol.OperationPage{Items: items, Total: total}, nil
}

func (s *Store) OperationLogs(operationID string, after uint64, limit int) (*opsprotocol.OperationLogPage, error) {
	var records []operationLogRecord
	if err := s.db.Where("operation_id = ? AND sequence > ?", operationID, after).
		Order("sequence asc").Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	items := make([]opsprotocol.OperationLog, 0, len(records))
	nextCursor := after
	for _, record := range records {
		items = append(items, opsprotocol.OperationLog{
			Sequence: record.Sequence, OperationID: record.OperationID, Stream: record.Stream,
			Message: record.Message, CreatedAt: record.CreatedAt,
		})
		nextCursor = record.Sequence
	}
	return &opsprotocol.OperationLogPage{Items: items, NextCursor: nextCursor}, nil
}

func (s *Store) ActiveOperation() (*opsprotocol.Operation, error) {
	var record operationRecord
	result := s.db.Where("status IN ?", []opsprotocol.OperationStatus{
		opsprotocol.OperationQueued, opsprotocol.OperationRunning, opsprotocol.OperationCancelling,
		opsprotocol.OperationRecovering, opsprotocol.OperationRecoveryRequired,
	}).
		Order("created_at asc").Limit(1).Find(&record)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	operation, err := operationFromRecord(record)
	if err != nil {
		return nil, err
	}
	return &operation, nil
}

func (s *Store) LatestOperation() (*opsprotocol.Operation, error) {
	var record operationRecord
	result := s.db.Order("created_at desc").Limit(1).Find(&record)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	operation, err := operationFromRecord(record)
	if err != nil {
		return nil, err
	}
	return &operation, nil
}

func (s *Store) LatestPublicVerification() (opsprotocol.PublicVerification, error) {
	var record operationRecord
	result := s.db.Where("action = ? AND status IN ?", opsprotocol.ActionVerify, []opsprotocol.OperationStatus{
		opsprotocol.OperationSucceeded,
		opsprotocol.OperationFailed,
	}).Order("completed_at desc, created_at desc").Limit(1).Find(&record)
	if result.Error != nil {
		return opsprotocol.PublicVerification{}, result.Error
	}
	if result.RowsAffected == 0 {
		return opsprotocol.PublicVerification{Status: opsprotocol.PublicVerificationNotRun}, nil
	}
	if record.CompletedAt == nil {
		return opsprotocol.PublicVerification{}, errors.New("已完成的公网校验缺少完成时间")
	}
	status := opsprotocol.PublicVerificationFailed
	if opsprotocol.OperationStatus(record.Status) == opsprotocol.OperationSucceeded {
		status = opsprotocol.PublicVerificationSucceeded
	}
	return opsprotocol.PublicVerification{
		Status: status, OperationID: record.ID, CheckedAt: record.CompletedAt,
		ErrorCode: opsprotocol.OperationErrorCode(record.ErrorCode), Error: record.Error,
	}, nil
}

func operationFromRecord(record operationRecord) (opsprotocol.Operation, error) {
	warnings := make([]opsprotocol.OperationWarning, 0)
	if record.WarningsJSON != "" {
		if err := json.Unmarshal([]byte(record.WarningsJSON), &warnings); err != nil {
			return opsprotocol.Operation{}, errors.New("运维任务告警投影损坏")
		}
	}
	return opsprotocol.Operation{
		ID: record.ID, Action: opsprotocol.Action(record.Action), TargetVersion: record.TargetVersion,
		CurrentVersionAtStart: record.CurrentVersionAtStart, ResultVersion: record.ResultVersion,
		Status: opsprotocol.OperationStatus(record.Status), Stage: opsprotocol.OperationStage(record.Stage),
		Phase: record.Phase, RunnerVersion: record.RunnerVersion, RunnerDigest: record.RunnerDigest,
		RunnerGeneration: record.RunnerGeneration, HeartbeatAt: record.HeartbeatAt,
		ServiceState: opsprotocol.ServiceState(record.ServiceState), CheckpointSequence: record.CheckpointSequence,
		CancelRequestedAt: record.CancelRequestedAt, RecoveryAction: opsprotocol.RecoveryAction(record.RecoveryAction),
		ControllerVersionAtStart: record.ControllerVersionAtStart,
		ResultControllerVersion:  record.ResultControllerVersion,
		ControllerHandoff:        opsprotocol.ControllerHandoff(record.ControllerHandoff),
		Warnings:                 warnings, ErrorCode: opsprotocol.OperationErrorCode(record.ErrorCode), Error: record.Error,
		ExitCode: record.ExitCode, ActorUserID: record.ActorUserID, ActorDisplayName: record.ActorDisplayName,
		IdempotencyKey: record.IdempotencyKey, CreatedAt: record.CreatedAt, StartedAt: record.StartedAt,
		CompletedAt: record.CompletedAt, UpdatedAt: record.UpdatedAt,
	}, nil
}
