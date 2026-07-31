package repository

import (
	"errors"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrStorageMigrationResourceChanged = errors.New("storage migration resource changed")
	ErrStorageMigrationAlreadyActive   = errors.New("storage migration already active")
)

const storageMigrationStartLockID int64 = 202607310013

type LocalResourceMigrationStats struct {
	Items int64 `json:"items"`
	Bytes int64 `json:"bytes"`
}

type storageMigrationJobValues struct {
	Status         model.StorageMigrationStatus
	TotalItems     int64
	FailedItems    int64
	TotalBytes     int64
	CommittedItems int64
	CommittedBytes int64
	Error          string
	StartedAt      *time.Time
	CompletedAt    *time.Time
	UpdatedAt      time.Time
}

type storageMigrationItemValues struct {
	Status       model.StorageMigrationItemStatus
	SourceSHA256 string
	TargetETag   string
	Error        string
	StartedAt    *time.Time
	CompletedAt  *time.Time
	UpdatedAt    time.Time
}

type migratedResourceValues struct {
	Provider         string
	Endpoint         string
	Bucket           string
	StorageSettingID string
	ObjectKey        string
	ETag             string
	UpdatedAt        time.Time
}

func (r *Repository) LocalResourceMigrationStats() (LocalResourceMigrationStats, error) {
	var stats LocalResourceMigrationStats
	err := r.db.Model(&model.Resource{}).
		Select("COUNT(*) AS items, COALESCE(SUM(size), 0) AS bytes").
		Where("provider = ? AND status = ?", "local", model.ResourceStatusReady).
		Scan(&stats).Error
	return stats, err
}

func (r *Repository) LocalResourcesForMigration(cutoff time.Time, afterID string, limit int) ([]model.Resource, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	query := r.db.Where("provider = ? AND status = ? AND created_at <= ?", "local", model.ResourceStatusReady, cutoff)
	if afterID != "" {
		query = query.Where("id > ?", afterID)
	}
	var resources []model.Resource
	err := query.Order("id asc").Limit(limit).Find(&resources).Error
	return resources, err
}

func (r *Repository) ActiveStorageMigrationJob() (*model.StorageMigrationJob, error) {
	var job model.StorageMigrationJob
	err := r.db.Where("status IN ?", []model.StorageMigrationStatus{
		model.StorageMigrationPreparing,
		model.StorageMigrationQueued,
		model.StorageMigrationRunning,
	}).Order("created_at desc").First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &job, err
}

func (r *Repository) LatestStorageMigrationJob() (*model.StorageMigrationJob, error) {
	var job model.StorageMigrationJob
	err := r.db.Order("created_at desc").First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &job, err
}

func (r *Repository) StorageMigrationJob(id string) (*model.StorageMigrationJob, error) {
	var job model.StorageMigrationJob
	if err := r.db.First(&job, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *Repository) CreateStorageMigrationJob(job *model.StorageMigrationJob) error {
	return r.db.Create(job).Error
}

func (r *Repository) CreateStorageMigrationJobIfIdle(job *model.StorageMigrationJob) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if r.Dialect() == "postgres" {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", storageMigrationStartLockID).Error; err != nil {
				return err
			}
		}
		var activeJobs int64
		if err := tx.Model(&model.StorageMigrationJob{}).
			Where("status IN ?", []model.StorageMigrationStatus{
				model.StorageMigrationPreparing,
				model.StorageMigrationQueued,
				model.StorageMigrationRunning,
			}).
			Count(&activeJobs).Error; err != nil {
			return err
		}
		if activeJobs > 0 {
			return ErrStorageMigrationAlreadyActive
		}
		return tx.Create(job).Error
	})
}

func (r *Repository) AppendStorageMigrationItems(items []model.StorageMigrationItem) error {
	if len(items) == 0 {
		return nil
	}
	return r.db.CreateInBatches(items, 200).Error
}

func (r *Repository) QueuePreparedStorageMigration(jobID string, items int64, bytes int64, now time.Time) error {
	result := r.db.Model(&model.StorageMigrationJob{}).
		Where("id = ? AND status = ?", jobID, model.StorageMigrationPreparing).
		Select("Status", "TotalItems", "TotalBytes", "UpdatedAt").
		Updates(storageMigrationJobValues{
			Status: model.StorageMigrationQueued, TotalItems: items, TotalBytes: bytes, UpdatedAt: now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *Repository) FailPreparingStorageMigration(jobID string, message string, now time.Time) error {
	return r.db.Model(&model.StorageMigrationJob{}).
		Where("id = ? AND status = ?", jobID, model.StorageMigrationPreparing).
		Select("Status", "Error", "CompletedAt", "UpdatedAt").
		Updates(storageMigrationJobValues{
			Status: model.StorageMigrationFailed, Error: message, CompletedAt: &now, UpdatedAt: now,
		}).Error
}

func (r *Repository) FailRunningStorageMigration(jobID string, message string, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.StorageMigrationItem{}).
			Where("job_id = ? AND status = ?", jobID, model.StorageMigrationItemRunning).
			Select("Status", "StartedAt", "UpdatedAt").
			Updates(storageMigrationItemValues{
				Status: model.StorageMigrationItemPending, StartedAt: nil, UpdatedAt: now,
			}).Error; err != nil {
			return err
		}
		return tx.Model(&model.StorageMigrationJob{}).
			Where("id = ? AND status = ?", jobID, model.StorageMigrationRunning).
			Select("Status", "Error", "CompletedAt", "UpdatedAt").
			Updates(storageMigrationJobValues{
				Status: model.StorageMigrationFailed, Error: message, CompletedAt: &now, UpdatedAt: now,
			}).Error
	})
}

// ResumeStorageMigrations 把进程中断时的运行态恢复为可重试状态；对象键是确定性的，重复 PUT 不会产生孤儿对象。
func (r *Repository) ResumeStorageMigrations(now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.StorageMigrationJob{}).
			Where("status = ?", model.StorageMigrationPreparing).
			Select("Status", "Error", "CompletedAt", "UpdatedAt").
			Updates(storageMigrationJobValues{
				Status: model.StorageMigrationFailed, Error: "服务重启中断迁移快照创建，请重新创建任务",
				CompletedAt: &now, UpdatedAt: now,
			}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.StorageMigrationItem{}).
			Where("status = ?", model.StorageMigrationItemRunning).
			Select("Status", "StartedAt", "UpdatedAt").
			Updates(storageMigrationItemValues{Status: model.StorageMigrationItemPending, StartedAt: nil, UpdatedAt: now}).Error; err != nil {
			return err
		}
		return tx.Model(&model.StorageMigrationJob{}).
			Where("status = ?", model.StorageMigrationRunning).
			Select("Status", "StartedAt", "UpdatedAt").
			Updates(storageMigrationJobValues{Status: model.StorageMigrationQueued, StartedAt: nil, UpdatedAt: now}).Error
	})
}

func (r *Repository) ClaimNextStorageMigrationJob(now time.Time) (*model.StorageMigrationJob, error) {
	var job model.StorageMigrationJob
	err := r.db.Transaction(func(tx *gorm.DB) error {
		query := tx.Where("status = ?", model.StorageMigrationQueued).Order("created_at asc")
		if r.Dialect() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		if err := query.First(&job).Error; err != nil {
			return err
		}
		result := tx.Model(&model.StorageMigrationJob{}).
			Where("id = ? AND status = ?", job.ID, model.StorageMigrationQueued).
			Select("Status", "StartedAt", "Error", "UpdatedAt").
			Updates(storageMigrationJobValues{Status: model.StorageMigrationRunning, StartedAt: &now, Error: "", UpdatedAt: now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		job.Status = model.StorageMigrationRunning
		job.StartedAt = &now
		job.UpdatedAt = now
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &job, err
}

func (r *Repository) NextStorageMigrationItem(jobID string) (*model.StorageMigrationItem, error) {
	var item model.StorageMigrationItem
	err := r.db.Where("job_id = ? AND status = ?", jobID, model.StorageMigrationItemPending).
		Order("created_at asc, id asc").First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &item, err
}

func (r *Repository) MarkStorageMigrationItemRunning(itemID string, now time.Time) error {
	result := r.db.Exec(`
		UPDATE storage_migration_items
		SET status = ?, attempt_count = attempt_count + 1, error = ?, started_at = ?, updated_at = ?
		WHERE id = ? AND status = ?
	`, model.StorageMigrationItemRunning, "", now, now, itemID, model.StorageMigrationItemPending)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *Repository) FailStorageMigrationItem(itemID string, message string, now time.Time) error {
	return r.db.Model(&model.StorageMigrationItem{}).
		Where("id = ?", itemID).
		Select("Status", "Error", "CompletedAt", "UpdatedAt").
		Updates(storageMigrationItemValues{
			Status: model.StorageMigrationItemFailed, Error: message, CompletedAt: &now, UpdatedAt: now,
		}).Error
}

type CommitStorageMigrationInput struct {
	JobID           string
	ItemID          string
	ResourceID      string
	SourceObjectKey string
	Provider        string
	Endpoint        string
	Bucket          string
	TargetObjectKey string
	SourceSHA256    string
	TargetETag      string
	Size            int64
	Now             time.Time
}

func (r *Repository) CommitStorageMigrationItem(input CommitStorageMigrationInput) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		resourceQuery := tx.Where("id = ?", input.ResourceID)
		if r.Dialect() == "postgres" {
			resourceQuery = resourceQuery.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		var resource model.Resource
		if err := resourceQuery.First(&resource).Error; err != nil {
			return err
		}
		if resource.Provider != "local" || resource.Status != model.ResourceStatusReady || resource.ObjectKey != input.SourceObjectKey || resource.Size != input.Size {
			return ErrStorageMigrationResourceChanged
		}
		if err := tx.Model(&model.Resource{}).Where("id = ?", resource.ID).
			Select("Provider", "Endpoint", "Bucket", "StorageSettingID", "ObjectKey", "ETag", "UpdatedAt").
			Updates(migratedResourceValues{
				Provider:         input.Provider,
				Endpoint:         input.Endpoint,
				Bucket:           input.Bucket,
				StorageSettingID: "",
				ObjectKey:        input.TargetObjectKey,
				ETag:             input.TargetETag,
				UpdatedAt:        input.Now,
			}).Error; err != nil {
			return err
		}
		itemResult := tx.Model(&model.StorageMigrationItem{}).
			Where("id = ? AND job_id = ? AND status = ?", input.ItemID, input.JobID, model.StorageMigrationItemRunning).
			Select("Status", "SourceSHA256", "TargetETag", "Error", "CompletedAt", "UpdatedAt").
			Updates(storageMigrationItemValues{
				Status:       model.StorageMigrationItemCommitted,
				SourceSHA256: input.SourceSHA256,
				TargetETag:   input.TargetETag,
				Error:        "",
				CompletedAt:  &input.Now,
				UpdatedAt:    input.Now,
			})
		if itemResult.Error != nil {
			return itemResult.Error
		}
		if itemResult.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return tx.Exec(`
			UPDATE storage_migration_jobs
			SET committed_items = committed_items + 1,
				committed_bytes = committed_bytes + ?,
				updated_at = ?
			WHERE id = ?
		`, input.Size, input.Now, input.JobID).Error
	})
}

func (r *Repository) FinishStorageMigrationJob(jobID string, now time.Time) (*model.StorageMigrationJob, error) {
	var committedItems int64
	var failedItems int64
	var committedBytes int64
	if err := r.db.Model(&model.StorageMigrationItem{}).
		Where("job_id = ? AND status = ?", jobID, model.StorageMigrationItemCommitted).
		Count(&committedItems).Error; err != nil {
		return nil, err
	}
	if err := r.db.Model(&model.StorageMigrationItem{}).
		Where("job_id = ? AND status = ?", jobID, model.StorageMigrationItemFailed).
		Count(&failedItems).Error; err != nil {
		return nil, err
	}
	if err := r.db.Model(&model.StorageMigrationItem{}).
		Select("COALESCE(SUM(size), 0)").
		Where("job_id = ? AND status = ?", jobID, model.StorageMigrationItemCommitted).
		Scan(&committedBytes).Error; err != nil {
		return nil, err
	}
	status := model.StorageMigrationSucceeded
	if failedItems > 0 {
		status = model.StorageMigrationPartialFailed
	}
	if committedItems == 0 && failedItems > 0 {
		status = model.StorageMigrationFailed
	}
	if err := r.db.Model(&model.StorageMigrationJob{}).Where("id = ?", jobID).
		Select("Status", "CommittedItems", "FailedItems", "CommittedBytes", "CompletedAt", "UpdatedAt").
		Updates(storageMigrationJobValues{
			Status:         status,
			CommittedItems: committedItems,
			FailedItems:    failedItems,
			CommittedBytes: committedBytes,
			CompletedAt:    &now,
			UpdatedAt:      now,
		}).Error; err != nil {
		return nil, err
	}
	return r.StorageMigrationJob(jobID)
}

func (r *Repository) StorageMigrationItems(jobID string, limit int) ([]model.StorageMigrationItem, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var items []model.StorageMigrationItem
	err := r.db.Where("job_id = ?", jobID).
		Order("CASE WHEN status = 'failed' THEN 0 ELSE 1 END, created_at asc").
		Limit(limit).Find(&items).Error
	return items, err
}

func (r *Repository) RetryFailedStorageMigration(jobID string, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var job model.StorageMigrationJob
		if err := tx.First(&job, "id = ?", jobID).Error; err != nil {
			return err
		}
		if job.Status != model.StorageMigrationPartialFailed && job.Status != model.StorageMigrationFailed {
			return errors.New("storage migration job is not retryable")
		}
		if job.TotalItems <= 0 {
			return errors.New("storage migration job snapshot is incomplete")
		}
		result := tx.Model(&model.StorageMigrationItem{}).
			Where("job_id = ? AND status = ?", jobID, model.StorageMigrationItemFailed).
			Select("Status", "Error", "StartedAt", "CompletedAt", "UpdatedAt").
			Updates(storageMigrationItemValues{
				Status: model.StorageMigrationItemPending, Error: "", StartedAt: nil, CompletedAt: nil, UpdatedAt: now,
			})
		if result.Error != nil {
			return result.Error
		}
		var pendingItems int64
		if err := tx.Model(&model.StorageMigrationItem{}).
			Where("job_id = ? AND status = ?", jobID, model.StorageMigrationItemPending).
			Count(&pendingItems).Error; err != nil {
			return err
		}
		if result.RowsAffected == 0 && pendingItems == 0 {
			return errors.New("storage migration job has no retryable items")
		}
		return tx.Model(&model.StorageMigrationJob{}).Where("id = ?", jobID).
			Select("Status", "FailedItems", "Error", "StartedAt", "CompletedAt", "UpdatedAt").
			Updates(storageMigrationJobValues{
				Status: model.StorageMigrationQueued, FailedItems: 0, Error: "",
				StartedAt: nil, CompletedAt: nil, UpdatedAt: now,
			}).Error
	})
}
