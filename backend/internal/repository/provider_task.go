package repository

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) ProviderTaskFact(taskID string) (*model.ProviderTaskFact, error) {
	var fact model.ProviderTaskFact
	if err := r.db.First(&fact, "task_id = ?", taskID).Error; err != nil {
		return nil, err
	}
	return &fact, nil
}

func (r *Repository) ProviderEndpointVersion(id string) (*model.ProviderEndpointVersion, error) {
	var version model.ProviderEndpointVersion
	if err := r.db.First(&version, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &version, nil
}

func (r *Repository) ProviderCredential(id string) (*model.ProviderCredential, error) {
	var credential model.ProviderCredential
	if err := r.db.First(&credential, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &credential, nil
}

func (r *Repository) ProviderCredentialVersion(id string) (*model.ProviderCredentialVersion, error) {
	var version model.ProviderCredentialVersion
	if err := r.db.First(&version, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &version, nil
}

func (r *Repository) ProviderChannelModelFact(id string) (*model.ChannelModel, error) {
	var item model.ChannelModel
	if err := r.db.Unscoped().Preload("PriceTiers").First(&item, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) SaveProviderTaskCreation(taskID string, providerTaskID string, traceID string) error {
	providerTaskID = strings.TrimSpace(providerTaskID)
	if providerTaskID == "" {
		return errors.New("provider task ID is required")
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.ProviderTaskFact{}).
			Where("task_id = ? AND provider_task_id = '' AND provider_status IN ?", taskID, []string{"reserved", "execution_claimed", "creating"}).
			Updates(map[string]any{"provider_task_id": providerTaskID, "create_trace_id": strings.TrimSpace(traceID), "provider_status": "submitted", "updated_at": time.Now()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("provider task creation fact state conflict")
		}
		if err := tx.Model(&model.Task{}).Where("id = ?", taskID).Updates(map[string]any{"provider_request_id": providerTaskID, "poll_stage": "accepted", "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		return tx.Model(&model.BillingOrder{}).Where("task_id = ?", taskID).Update("provider_request_id", providerTaskID).Error
	})
}

func (r *Repository) UpdateProviderTaskPoll(taskID string, providerStatus string, traceID string) error {
	result := r.db.Model(&model.ProviderTaskFact{}).Where("task_id = ? AND provider_task_id <> ''", taskID).Updates(map[string]any{
		"provider_status": strings.TrimSpace(providerStatus), "last_poll_trace_id": strings.TrimSpace(traceID), "updated_at": time.Now(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("provider task poll fact state conflict")
	}
	return nil
}

func (r *Repository) SaveProviderTaskSuccess(taskID string, stateStatus string, traceID string, assetSourceURL string, lastFrameURL string, actualDuration int, totalTokens string) error {
	result := r.db.Model(&model.ProviderTaskFact{}).Where("task_id = ? AND provider_task_id <> ''", taskID).Updates(map[string]any{
		"provider_status": stateStatus, "last_poll_trace_id": strings.TrimSpace(traceID),
		"asset_source_url": strings.TrimSpace(assetSourceURL), "last_frame_url": strings.TrimSpace(lastFrameURL),
		"actual_duration_seconds": actualDuration, "total_tokens": strings.TrimSpace(totalTokens), "updated_at": time.Now(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("provider task success fact state conflict")
	}
	return nil
}

func (r *Repository) MarkProviderTaskCreateUncertain(taskID string, reason string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		fact := tx.Model(&model.ProviderTaskFact{}).Where("task_id = ? AND provider_task_id = ''", taskID).Updates(map[string]any{
			"provider_status": "create_uncertain", "updated_at": time.Now(),
		})
		if fact.Error != nil {
			return fact.Error
		}
		if fact.RowsAffected != 1 {
			return errors.New("provider task create uncertainty state conflict")
		}
		order := tx.Model(&model.BillingOrder{}).Where("task_id = ? AND status IN ?", taskID, []model.BillingStatus{model.BillingStatusReserved, model.BillingStatusRunning, model.BillingStatusUncertain}).Updates(map[string]any{
			"status": model.BillingStatusUncertain, "error": strings.TrimSpace(reason), "updated_at": time.Now(),
		})
		if order.Error != nil {
			return order.Error
		}
		if order.RowsAffected != 1 {
			return errors.New("provider task billing uncertainty state conflict")
		}
		return nil
	})
}

func (r *Repository) MarkProviderTaskCreateFailed(taskID string, traceID string) error {
	result := r.db.Model(&model.ProviderTaskFact{}).
		Where("task_id = ? AND provider_task_id = '' AND provider_status IN ?", taskID, []string{"reserved", "execution_claimed", "creating"}).
		Updates(map[string]any{"provider_status": "create_failed", "create_trace_id": strings.TrimSpace(traceID), "updated_at": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("provider task create failure state conflict")
	}
	return nil
}

func (r *Repository) MarkProviderTaskCreateStarted(taskID string) error {
	result := r.db.Model(&model.ProviderTaskFact{}).
		Where("task_id = ? AND provider_task_id = '' AND provider_status IN ?", taskID, []string{"reserved", "execution_claimed"}).
		Updates(map[string]any{"provider_status": "creating", "updated_at": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("provider task create start state conflict")
	}
	return nil
}

func (r *Repository) MarkProviderTaskPollUncertain(taskID string, traceID string, reason string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		fact := tx.Model(&model.ProviderTaskFact{}).
			Where("task_id = ? AND provider_task_id <> '' AND provider_status IN ?", taskID, providerExecutionStatuses()).
			Updates(map[string]any{"provider_status": "poll_uncertain", "last_poll_trace_id": strings.TrimSpace(traceID), "updated_at": time.Now()})
		if fact.Error != nil {
			return fact.Error
		}
		if fact.RowsAffected != 1 {
			return errors.New("provider task poll uncertainty state conflict")
		}
		order := tx.Model(&model.BillingOrder{}).
			Where("task_id = ? AND status IN ?", taskID, []model.BillingStatus{model.BillingStatusReserved, model.BillingStatusRunning, model.BillingStatusUncertain}).
			Updates(map[string]any{"status": model.BillingStatusUncertain, "error": strings.TrimSpace(reason), "updated_at": time.Now()})
		if order.Error != nil {
			return order.Error
		}
		if order.RowsAffected != 1 {
			return errors.New("provider task poll billing uncertainty state conflict")
		}
		return nil
	})
}

func (r *Repository) ClaimProviderTaskExecution(taskID string, leaseOwner string) (bool, error) {
	claimed := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var fact model.ProviderTaskFact
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&fact, "task_id = ?", taskID).Error; err != nil {
			return err
		}
		if err := lockProviderExecutionScopeTx(tx, fact.ProviderCredentialID); err != nil {
			return err
		}
		var task model.Task
		if err := tx.First(&task, "id = ? AND status = ? AND lease_owner = ? AND lease_expires_at > ?", taskID, model.TaskStatusRunning, leaseOwner, time.Now()).Error; err != nil {
			return err
		}
		if providerExecutionStatus(fact.ProviderStatus) {
			claimed = true
			return nil
		}
		if fact.ProviderStatus != "reserved" {
			return fmt.Errorf("provider task %s cannot claim execution from status %s", taskID, fact.ProviderStatus)
		}
		var credential model.ProviderCredential
		if err := tx.First(&credential, "id = ?", fact.ProviderCredentialID).Error; err != nil {
			return err
		}
		if credential.ConcurrencyLimit <= 0 {
			return errors.New("provider credential concurrency limit is invalid")
		}
		var active int64
		if err := tx.Model(&model.ProviderTaskFact{}).
			Where("provider_credential_id = ? AND task_id <> ? AND provider_status IN ?", credential.ID, taskID, providerExecutionStatuses()).
			Count(&active).Error; err != nil {
			return err
		}
		if active >= int64(credential.ConcurrencyLimit) {
			return nil
		}
		result := tx.Model(&model.ProviderTaskFact{}).Where("task_id = ? AND provider_status = ?", taskID, "reserved").Updates(map[string]any{"provider_status": "execution_claimed", "updated_at": time.Now()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("provider execution claim state conflict")
		}
		claimed = true
		return nil
	})
	return claimed, err
}

func (r *Repository) RequeueTaskWaitingForProviderCapacity(taskID string, leaseOwner string) error {
	now := time.Now()
	result := r.db.Model(&model.Task{}).
		Where("id = ? AND status = ? AND lease_owner = ?", taskID, model.TaskStatusRunning, leaseOwner).
		Updates(map[string]any{
			"status": model.TaskStatusQueued, "stage": "等待上游并发名额", "progress": 5,
			"next_poll_at": now.Add(5 * time.Second), "lease_owner": "", "lease_expires_at": nil, "updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("provider capacity requeue state conflict")
	}
	return nil
}

func lockProviderExecutionScopeTx(tx *gorm.DB, credentialID string) error {
	switch tx.Dialector.Name() {
	case "postgres":
		return tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, "provider-execution\n"+credentialID).Error
	case "sqlite":
		return tx.Exec("UPDATE provider_credentials SET updated_at = updated_at WHERE id = ?", credentialID).Error
	default:
		return fmt.Errorf("provider execution locking is unsupported for %s", tx.Dialector.Name())
	}
}

func providerExecutionStatuses() []string {
	return []string{"execution_claimed", "creating", "submitted", "pending", "running"}
}

func providerExecutionStatus(status string) bool {
	for _, candidate := range providerExecutionStatuses() {
		if status == candidate {
			return true
		}
	}
	return false
}
