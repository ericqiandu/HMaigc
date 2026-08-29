package repository

import (
	"errors"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrProviderCreateStateConflict = errors.New("provider create state conflict")
	ErrProviderCreateUncertain     = errors.New("provider create may already have been sent")
)

// BeginProviderCreate 在任何异步创建请求发出前提交不可重复的创建边界。
// worker 若在此后崩溃，只能进入人工核对，不能再次向供应商 POST。
func (r *Repository) BeginProviderCreate(taskID string, leaseOwner string, leaseGeneration uint64, leaseToken string) error {
	if !validTaskLease(leaseOwner, leaseGeneration, leaseToken) {
		return ErrProviderCreateStateConflict
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		var task model.Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, "id = ?", taskID).Error; err != nil {
			return err
		}
		if task.Status != model.TaskStatusRunning || task.LeaseOwner != leaseOwner || task.LeaseGeneration != leaseGeneration || task.LeaseToken != leaseToken {
			return ErrProviderCreateStateConflict
		}
		if strings.TrimSpace(task.ProviderRequestID) != "" {
			return ErrProviderCreateStateConflict
		}
		if task.PollStage == "creating" {
			return ErrProviderCreateUncertain
		}
		if strings.TrimSpace(task.PollStage) != "" {
			return ErrProviderCreateStateConflict
		}
		updated := tx.Model(&model.Task{}).
			Where("id = ? AND status = ? AND lease_owner = ? AND lease_generation = ? AND lease_token = ? AND provider_request_id = '' AND (poll_stage = '' OR poll_stage IS NULL)", task.ID, model.TaskStatusRunning, leaseOwner, leaseGeneration, leaseToken).
			Updates(map[string]any{"poll_stage": "creating", "next_poll_at": nil, "updated_at": time.Now()})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrProviderCreateStateConflict
		}
		return nil
	})
}

// SaveProviderCall 将关键上游任务 ID、账单关联和审计日志作为一个事实提交。
func (r *Repository) SaveProviderCall(log *model.ApiCallLog, leaseOwner string, leaseGeneration uint64, leaseToken string, asyncCreate bool) error {
	providerRequestID := strings.TrimSpace(log.ProviderRequestID)
	if asyncCreate && log.Status == model.ApiCallStatusSucceeded && providerRequestID != "" {
		if !validTaskLease(leaseOwner, leaseGeneration, leaseToken) || leaseGeneration == ^uint64(0) {
			return ErrProviderCreateStateConflict
		}
		if err := r.db.Transaction(func(tx *gorm.DB) error {
			now := time.Now()
			updated := tx.Model(&model.Task{}).
				Where("id = ? AND status = ? AND lease_owner = ? AND lease_generation = ? AND lease_token = ? AND poll_stage = ? AND provider_request_id = ''", log.TaskID, model.TaskStatusRunning, leaseOwner, leaseGeneration, leaseToken, "creating").
				Updates(map[string]any{
					"provider_request_id": providerRequestID,
					"poll_stage":          "accepted",
					"next_poll_at":        now.Add(2 * time.Second),
					"updated_at":          now,
				})
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected == 0 {
				// Cancellation may win after the provider accepted the request but before
				// the response ID was persisted. This branch stores only the immutable
				// recovery identity; it does not restore the stale worker lease or allow
				// that worker to publish progress/completion.
				updated = tx.Model(&model.Task{}).
					Where("id = ? AND status = ? AND cancel_requested_at IS NOT NULL AND lease_generation = ? AND lease_owner = '' AND lease_token = '' AND poll_stage = ? AND provider_request_id = ''", log.TaskID, model.TaskStatusCancelled, leaseGeneration+1, "creating").
					Updates(map[string]any{
						"provider_request_id": providerRequestID,
						"poll_stage":          "cancel_reconcile",
						"next_poll_at":        now,
						"updated_at":          now,
					})
				if updated.Error != nil {
					return updated.Error
				}
			}
			if updated.RowsAffected != 1 {
				return ErrProviderCreateStateConflict
			}
			if log.BillingOrderID != "" {
				if err := tx.Model(&model.BillingOrder{}).Where("id = ?", log.BillingOrderID).Update("provider_request_id", providerRequestID).Error; err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	// 上游任务 ID 是防止重复付费请求的恢复事实，必须先于可失败的运营日志提交。
	if err := r.db.Create(log).Error; err != nil {
		return err
	}
	if !asyncCreate && log.BillingOrderID != "" && providerRequestID != "" {
		if err := r.db.Model(&model.BillingOrder{}).Where("id = ?", log.BillingOrderID).Update("provider_request_id", providerRequestID).Error; err != nil {
			return err
		}
	}
	return nil
}
