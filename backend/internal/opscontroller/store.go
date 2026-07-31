package opscontroller

import (
	"errors"
	"os"
	"time"

	"infinite-canvas/backend/internal/opsprotocol"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type operationRecord struct {
	ID                    string `gorm:"primaryKey;size:36"`
	Action                string `gorm:"index;size:24"`
	TargetVersion         string `gorm:"size:80"`
	CurrentVersionAtStart string `gorm:"size:80"`
	ResultVersion         string `gorm:"size:80"`
	Status                string `gorm:"index;size:24"`
	Phase                 string `gorm:"size:160"`
	Error                 string `gorm:"type:text"`
	ExitCode              *int
	ActorUserID           string    `gorm:"index;size:36"`
	ActorDisplayName      string    `gorm:"size:160"`
	IdempotencyKey        string    `gorm:"uniqueIndex;size:128"`
	RequestHash           string    `gorm:"size:64"`
	CreatedAt             time.Time `gorm:"index"`
	StartedAt             *time.Time
	CompletedAt           *time.Time
	UpdatedAt             time.Time
}

type operationLogRecord struct {
	Sequence    uint64    `gorm:"primaryKey;autoIncrement"`
	OperationID string    `gorm:"index;size:36"`
	Stream      string    `gorm:"size:16"`
	Message     string    `gorm:"type:text"`
	CreatedAt   time.Time `gorm:"index"`
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
	if err := db.AutoMigrate(&operationRecord{}, &operationLogRecord{}); err != nil {
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
	operation := operationFromRecord(result)
	return &operation, created, nil
}

func (s *Store) ClaimNextOperation(now time.Time) (*opsprotocol.Operation, error) {
	var claimed operationRecord
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var record operationRecord
		result := tx.Where("status = ?", opsprotocol.OperationQueued).Order("created_at asc").Limit(1).Find(&record)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		updated := tx.Model(&operationRecord{}).
			Where("id = ? AND status = ?", record.ID, opsprotocol.OperationQueued).
			Updates(map[string]interface{}{
				"status":     opsprotocol.OperationRunning,
				"phase":      "控制器已接管任务",
				"started_at": now,
				"updated_at": now,
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return nil
		}
		record.Status = string(opsprotocol.OperationRunning)
		record.Phase = "控制器已接管任务"
		record.StartedAt = &now
		record.UpdatedAt = now
		claimed = record
		return nil
	})
	if err != nil {
		return nil, err
	}
	if claimed.ID == "" {
		return nil, nil
	}
	operation := operationFromRecord(claimed)
	return &operation, nil
}

func (s *Store) UpdatePhase(id string, phase string, now time.Time) error {
	return s.db.Model(&operationRecord{}).
		Where("id = ? AND status = ?", id, opsprotocol.OperationRunning).
		Updates(map[string]interface{}{"phase": phase, "updated_at": now}).Error
}

func (s *Store) CompleteOperation(id string, status opsprotocol.OperationStatus, phase string, resultVersion string, exitCode *int, errorMessage string, now time.Time) error {
	if status != opsprotocol.OperationSucceeded && status != opsprotocol.OperationFailed {
		return errors.New("完成状态无效")
	}
	result := s.db.Model(&operationRecord{}).
		Where("id = ? AND status = ?", id, opsprotocol.OperationRunning).
		Updates(map[string]interface{}{
			"status":         status,
			"phase":          phase,
			"result_version": resultVersion,
			"exit_code":      exitCode,
			"error":          errorMessage,
			"completed_at":   now,
			"updated_at":     now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("运维任务状态已变化，拒绝覆盖")
	}
	return nil
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
	operation := operationFromRecord(record)
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
		items = append(items, operationFromRecord(record))
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
	result := s.db.Where("status IN ?", []opsprotocol.OperationStatus{opsprotocol.OperationQueued, opsprotocol.OperationRunning}).
		Order("created_at asc").Limit(1).Find(&record)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	operation := operationFromRecord(record)
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
	operation := operationFromRecord(record)
	return &operation, nil
}

func (s *Store) FailInterruptedOperations(now time.Time) error {
	var records []operationRecord
	if err := s.db.Where("status = ?", opsprotocol.OperationRunning).Find(&records).Error; err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		for _, record := range records {
			message := "运维控制器在任务执行期间重启，任务结果未知，请人工核对服务状态后再操作"
			if err := tx.Model(&operationRecord{}).Where("id = ? AND status = ?", record.ID, opsprotocol.OperationRunning).
				Updates(map[string]interface{}{
					"status": opsprotocol.OperationFailed, "phase": "控制器重启中断",
					"error": message, "completed_at": now, "updated_at": now,
				}).Error; err != nil {
				return err
			}
			if err := tx.Create(&operationLogRecord{OperationID: record.ID, Stream: "system", Message: message, CreatedAt: now}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func operationFromRecord(record operationRecord) opsprotocol.Operation {
	return opsprotocol.Operation{
		ID: record.ID, Action: opsprotocol.Action(record.Action), TargetVersion: record.TargetVersion,
		CurrentVersionAtStart: record.CurrentVersionAtStart, ResultVersion: record.ResultVersion,
		Status: opsprotocol.OperationStatus(record.Status), Phase: record.Phase, Error: record.Error,
		ExitCode: record.ExitCode, ActorUserID: record.ActorUserID, ActorDisplayName: record.ActorDisplayName,
		IdempotencyKey: record.IdempotencyKey, CreatedAt: record.CreatedAt, StartedAt: record.StartedAt,
		CompletedAt: record.CompletedAt, UpdatedAt: record.UpdatedAt,
	}
}
