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

type ProviderTaskLease struct {
	Owner string
	Token string
}

type ProviderTaskFailureResolution struct {
	ExpectedStatuses     []string
	ProviderStatus       string
	ReconciliationStatus string
	TraceID              string
	TaskStage            string
	TaskError            string
}

type ProviderTaskCreationWrite struct {
	HandedOff           bool
	ResolvedObservation bool
}

type ProviderTaskCancelDecision string

const (
	ProviderTaskCancelledBeforeOutbound ProviderTaskCancelDecision = "cancelled_before_outbound"
	ProviderTaskCancellationRequested   ProviderTaskCancelDecision = "cancellation_requested"
)

type ProviderTaskCancelResult struct {
	Decision ProviderTaskCancelDecision
	Task     model.Task
}

var ErrProviderTaskCancelGenerationConflict = errors.New("provider cancellation generation conflict")

type ProviderCreateUncertainDecision string

const (
	ProviderCreateUncertainRequeued     ProviderCreateUncertainDecision = "requeued"
	ProviderCreateUncertainManualReview ProviderCreateUncertainDecision = "manual_review"
)

func (r *Repository) ProviderTaskFact(taskID string) (*model.ProviderTaskFact, error) {
	var fact model.ProviderTaskFact
	if err := r.db.First(&fact, "task_id = ?", taskID).Error; err != nil {
		return nil, err
	}
	return &fact, nil
}

func (r *Repository) ProviderTaskFactForBillingOrder(billingOrderID string) (*model.ProviderTaskFact, error) {
	var facts []model.ProviderTaskFact
	if err := r.db.Where("billing_order_id = ?", billingOrderID).Order("task_id").Limit(2).Find(&facts).Error; err != nil {
		return nil, err
	}
	if len(facts) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	if len(facts) != 1 {
		return nil, errors.New("billing order provider task fact cardinality conflict")
	}
	return &facts[0], nil
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

func (r *Repository) SaveProviderTaskCreationForLease(taskID string, lease ProviderTaskLease, providerTaskID string, traceID string) (ProviderTaskCreationWrite, error) {
	providerTaskID = strings.TrimSpace(providerTaskID)
	if providerTaskID == "" {
		return ProviderTaskCreationWrite{}, errors.New("provider task ID is required")
	}
	write := ProviderTaskCreationWrite{}
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var fact model.ProviderTaskFact
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&fact, "task_id = ?", taskID).Error; err != nil {
			return err
		}
		if fact.ProviderTaskID != "" || fact.CreateLeaseToken != lease.Token || !containsProviderStatus([]string{"creating", "create_uncertain", "create_uncertain_resolved"}, fact.ProviderStatus) {
			return errors.New("provider task creation fact state conflict")
		}
		if fact.BillingOrderID != "" {
			var order model.BillingOrder
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", fact.BillingOrderID).Error; err != nil {
				return err
			}
			if fact.ReconciliationStatus == "resolved" {
				if order.Status != model.BillingStatusSettled && order.Status != model.BillingStatusRefunded {
					return errors.New("resolved provider observation billing state conflict")
				}
				if strings.TrimSpace(order.ResolvedBy) == "" {
					return errors.New("resolved provider observation requires an administrative decision")
				}
			}
		}
		var task model.Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, "id = ?", taskID).Error; err != nil {
			return err
		}
		now := time.Now()
		currentLease := task.Status == model.TaskStatusRunning && task.LeaseOwner == lease.Owner && task.LeaseToken == lease.Token && task.LeaseExpiresAt != nil && task.LeaseExpiresAt.After(now)
		write.ResolvedObservation = fact.ReconciliationStatus == "resolved"
		if write.ResolvedObservation && task.Status != model.TaskStatusFailed {
			return errors.New("resolved provider observation task state conflict")
		}
		write.HandedOff = !currentLease || write.ResolvedObservation
		updates := map[string]any{
			"provider_task_id": providerTaskID, "create_trace_id": strings.TrimSpace(traceID), "updated_at": now,
		}
		if write.ResolvedObservation {
			updates["provider_status"] = "resolved_observation_submitted"
			updates["execution_lease_token"] = ""
		} else if fact.ProviderStatus == "creating" {
			updates["provider_status"] = "submitted"
			updates["execution_lease_token"] = task.LeaseToken
		}
		factUpdate := tx.Model(&model.ProviderTaskFact{}).Where("task_id = ? AND provider_task_id = '' AND create_lease_token = ?", taskID, lease.Token).Updates(updates)
		if factUpdate.Error != nil {
			return factUpdate.Error
		}
		if factUpdate.RowsAffected != 1 {
			return errors.New("provider task creation fact state conflict")
		}
		taskUpdates := map[string]any{"provider_request_id": providerTaskID, "poll_stage": "accepted", "updated_at": now}
		if write.ResolvedObservation {
			taskUpdates["status"] = model.TaskStatusQueued
			taskUpdates["stage"] = "等待核对已决任务的上游观察"
			taskUpdates["completed_at"] = nil
			taskUpdates["next_poll_at"] = now.Add(time.Second)
			taskUpdates["lease_owner"] = ""
			taskUpdates["lease_token"] = ""
			taskUpdates["lease_expires_at"] = nil
		}
		if err := tx.Model(&model.Task{}).Where("id = ?", taskID).Updates(taskUpdates).Error; err != nil {
			return err
		}
		if fact.BillingOrderID != "" && !write.ResolvedObservation {
			if err := tx.Model(&model.BillingOrder{}).Where("id = ?", fact.BillingOrderID).Update("provider_request_id", providerTaskID).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return write, err
}

func (r *Repository) UpdateProviderTaskPollForLease(taskID string, lease ProviderTaskLease, providerStatus string, traceID string) error {
	providerStatus = strings.TrimSpace(providerStatus)
	return r.db.Transaction(func(tx *gorm.DB) error {
		fact, _, err := lockProviderTaskTransitionTx(tx, taskID, lease)
		if err != nil {
			return err
		}
		storedStatus := providerStatus
		if fact.ReconciliationStatus == "resolved" && resolvedProviderObservationActive(fact.ProviderStatus) {
			storedStatus = resolvedProviderObservationStatus(providerStatus)
			if storedStatus == "" || !allowedResolvedProviderObservationTransition(fact.ProviderStatus, storedStatus) {
				return errors.New("resolved provider observation poll fact state conflict")
			}
		} else if fact.ReconciliationStatus == "resolved" || !allowedProviderPollTransition(fact.ProviderStatus, providerStatus) {
			return errors.New("provider task poll fact state conflict")
		}
		if fact.ProviderTaskID == "" {
			return errors.New("provider task poll fact state conflict")
		}
		updated := tx.Model(&model.ProviderTaskFact{}).Where("task_id = ? AND provider_status = ?", taskID, fact.ProviderStatus).Updates(map[string]any{
			"provider_status": storedStatus, "last_poll_trace_id": strings.TrimSpace(traceID), "updated_at": time.Now(),
		})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return errors.New("provider task poll fact state conflict")
		}
		return nil
	})
}

func (r *Repository) SaveProviderTaskSuccessForLease(taskID string, lease ProviderTaskLease, traceID string, assetSourceURL string, lastFrameURL string, actualDuration int, totalTokens string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		fact, _, err := lockProviderTaskTransitionTx(tx, taskID, lease)
		if err != nil {
			return err
		}
		storedStatus := "succeeded"
		if fact.ReconciliationStatus == "resolved" && resolvedProviderObservationActive(fact.ProviderStatus) {
			storedStatus = "resolved_observation_succeeded"
		} else if fact.ReconciliationStatus == "resolved" || !providerSuccessPredecessor(fact.ProviderStatus) {
			return errors.New("provider task success fact state conflict")
		}
		if fact.ProviderTaskID == "" {
			return errors.New("provider task success fact state conflict")
		}
		updated := tx.Model(&model.ProviderTaskFact{}).Where("task_id = ? AND provider_status = ?", taskID, fact.ProviderStatus).Updates(map[string]any{
			"provider_status": storedStatus, "last_poll_trace_id": strings.TrimSpace(traceID),
			"asset_source_url": strings.TrimSpace(assetSourceURL), "last_frame_url": strings.TrimSpace(lastFrameURL),
			"actual_duration_seconds": actualDuration, "total_tokens": strings.TrimSpace(totalTokens), "updated_at": time.Now(),
		})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return errors.New("provider task success fact state conflict")
		}
		return nil
	})
}

func lockProviderTaskTransitionTx(tx *gorm.DB, taskID string, lease ProviderTaskLease) (model.ProviderTaskFact, model.Task, error) {
	var fact model.ProviderTaskFact
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&fact, "task_id = ?", taskID).Error; err != nil {
		return model.ProviderTaskFact{}, model.Task{}, err
	}
	var task model.Task
	now := time.Now()
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, "id = ?", taskID).Error; err != nil {
		return model.ProviderTaskFact{}, model.Task{}, err
	}
	if task.Status != model.TaskStatusRunning || task.LeaseOwner != lease.Owner || task.LeaseToken != lease.Token || task.LeaseExpiresAt == nil || !task.LeaseExpiresAt.After(now) {
		return model.ProviderTaskFact{}, model.Task{}, errors.New("provider task transition lease state conflict")
	}
	if fact.ExecutionLeaseToken == "" || fact.ExecutionLeaseToken != lease.Token {
		return model.ProviderTaskFact{}, model.Task{}, errors.New("provider task transition generation conflict")
	}
	return fact, task, nil
}

func allowedProviderPollTransition(current string, next string) bool {
	switch next {
	case "submitted":
		return current == "submitted"
	case "pending":
		return current == "submitted" || current == "pending"
	case "running":
		return current == "submitted" || current == "pending" || current == "running" || current == "poll_uncertain"
	default:
		return false
	}
}

func providerSuccessPredecessor(status string) bool {
	switch status {
	case "submitted", "pending", "running", "poll_uncertain":
		return true
	default:
		return false
	}
}

func resolvedProviderObservationStatus(providerStatus string) string {
	switch providerStatus {
	case "submitted", "pending", "running":
		return "resolved_observation_" + providerStatus
	default:
		return ""
	}
}

func resolvedProviderObservationActive(status string) bool {
	switch status {
	case "resolved_observation_submitted", "resolved_observation_pending", "resolved_observation_running", "resolved_observation_poll_uncertain":
		return true
	default:
		return false
	}
}

func ResolvedProviderObservationStatus(status string) bool {
	return resolvedProviderObservationActive(status)
}

func ResolvedProviderSuccessStatus(status string) bool {
	return status == "succeeded" || status == "resolved_observation_succeeded"
}

func allowedResolvedProviderObservationTransition(current string, next string) bool {
	switch next {
	case "resolved_observation_submitted":
		return current == "resolved_observation_submitted" || current == "resolved_observation_poll_uncertain"
	case "resolved_observation_pending":
		return current == "resolved_observation_submitted" || current == "resolved_observation_pending" || current == "resolved_observation_poll_uncertain"
	case "resolved_observation_running":
		return current == "resolved_observation_submitted" || current == "resolved_observation_pending" || current == "resolved_observation_running" || current == "resolved_observation_poll_uncertain"
	default:
		return false
	}
}

func (r *Repository) FinalizeResolvedProviderObservationFailure(taskID string, lease ProviderTaskLease, traceID string, stage string, taskError string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var fact model.ProviderTaskFact
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&fact, "task_id = ?", taskID).Error; err != nil {
			return err
		}
		if fact.ReconciliationStatus != "resolved" || !resolvedProviderObservationActive(fact.ProviderStatus) || fact.ExecutionLeaseToken != lease.Token {
			return errors.New("resolved provider observation failure fact state conflict")
		}
		var order model.BillingOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", fact.BillingOrderID).Error; err != nil {
			return err
		}
		if order.Status != model.BillingStatusSettled && order.Status != model.BillingStatusRefunded {
			return errors.New("resolved provider observation failure billing state conflict")
		}
		now := time.Now()
		var task model.Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, "id = ?", taskID).Error; err != nil {
			return err
		}
		if task.Status != model.TaskStatusRunning || task.LeaseOwner != lease.Owner || task.LeaseToken != lease.Token || task.LeaseExpiresAt == nil || !task.LeaseExpiresAt.After(now) {
			return errors.New("resolved provider observation failure task lease conflict")
		}
		factUpdate := tx.Model(&model.ProviderTaskFact{}).Where("task_id = ? AND provider_status = ?", taskID, fact.ProviderStatus).Updates(map[string]any{
			"provider_status": "resolved_observation_failed", "last_poll_trace_id": strings.TrimSpace(traceID), "updated_at": now,
		})
		if factUpdate.Error != nil {
			return factUpdate.Error
		}
		if factUpdate.RowsAffected != 1 {
			return errors.New("resolved provider observation failure fact conflict")
		}
		updated := tx.Model(&model.Task{}).Where("id = ? AND status = ? AND lease_owner = ? AND lease_token = ?", taskID, model.TaskStatusRunning, lease.Owner, lease.Token).Updates(map[string]any{
			"status": model.TaskStatusFailed, "stage": strings.TrimSpace(stage), "error": strings.TrimSpace(taskError), "completed_at": &now,
			"lease_owner": "", "lease_token": "", "lease_expires_at": nil, "updated_at": now,
		})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return errors.New("resolved provider observation failure task conflict")
		}
		return nil
	})
}

func (r *Repository) RequeueResolvedProviderObservation(taskID string, lease ProviderTaskLease, traceID string, reason string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var fact model.ProviderTaskFact
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&fact, "task_id = ?", taskID).Error; err != nil {
			return err
		}
		if fact.ReconciliationStatus != "resolved" || !resolvedProviderObservationActive(fact.ProviderStatus) || fact.ExecutionLeaseToken != lease.Token {
			return errors.New("resolved provider observation requeue fact state conflict")
		}
		var order model.BillingOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", fact.BillingOrderID).Error; err != nil {
			return err
		}
		if order.Status != model.BillingStatusSettled && order.Status != model.BillingStatusRefunded {
			return errors.New("resolved provider observation requeue billing state conflict")
		}
		now := time.Now()
		var task model.Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, "id = ?", taskID).Error; err != nil {
			return err
		}
		if task.Status != model.TaskStatusRunning || task.LeaseOwner != lease.Owner || task.LeaseToken != lease.Token || task.LeaseExpiresAt == nil || !task.LeaseExpiresAt.After(now) {
			return errors.New("resolved provider observation requeue task lease conflict")
		}
		factUpdate := tx.Model(&model.ProviderTaskFact{}).Where("task_id = ? AND provider_status = ?", taskID, fact.ProviderStatus).Updates(map[string]any{
			"provider_status": "resolved_observation_poll_uncertain", "last_poll_trace_id": strings.TrimSpace(traceID), "updated_at": now,
		})
		if factUpdate.Error != nil {
			return factUpdate.Error
		}
		if factUpdate.RowsAffected != 1 {
			return errors.New("resolved provider observation requeue fact conflict")
		}
		updated := tx.Model(&model.Task{}).Where("id = ? AND status = ? AND lease_owner = ? AND lease_token = ?", taskID, model.TaskStatusRunning, lease.Owner, lease.Token).Updates(map[string]any{
			"status": model.TaskStatusQueued, "stage": "等待继续观察已决上游任务", "error": strings.TrimSpace(reason), "completed_at": nil,
			"next_poll_at": now.Add(5 * time.Second), "lease_owner": "", "lease_token": "", "lease_expires_at": nil, "updated_at": now,
		})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return errors.New("resolved provider observation requeue task conflict")
		}
		return nil
	})
}

func (r *Repository) FinalizeProviderTaskRefund(taskID string, lease ProviderTaskLease, resolution ProviderTaskFailureResolution) error {
	return r.finalizeProviderTaskFailure(taskID, lease, resolution, func(tx *gorm.DB, fact model.ProviderTaskFact) error {
		return refundBillingOrderTx(tx, fact.BillingOrderID, resolution.TaskError)
	})
}

func (r *Repository) FinalizeProviderTaskUncertain(taskID string, lease ProviderTaskLease, resolution ProviderTaskFailureResolution) error {
	return r.finalizeProviderTaskFailure(taskID, lease, resolution, func(tx *gorm.DB, fact model.ProviderTaskFact) error {
		var order model.BillingOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", fact.BillingOrderID).Error; err != nil {
			return err
		}
		if order.Status != model.BillingStatusReserved && order.Status != model.BillingStatusRunning && order.Status != model.BillingStatusUncertain {
			return errors.New("provider failure billing state conflict")
		}
		return tx.Model(&model.BillingOrder{}).Where("id = ?", order.ID).Updates(map[string]any{
			"status": model.BillingStatusUncertain, "error": resolution.TaskError, "updated_at": time.Now(),
		}).Error
	})
}

func (r *Repository) CancelProviderTask(userID string, taskID string, expectedLeaseToken string, reason string) (ProviderTaskCancelResult, error) {
	result := ProviderTaskCancelResult{}
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var fact model.ProviderTaskFact
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&fact, "task_id = ?", taskID).Error; err != nil {
			return err
		}
		if fact.ReconciliationStatus == "resolved" {
			return errors.New("provider cancellation fact is already resolved")
		}
		beforeOutbound := fact.ProviderTaskID == "" && fact.CreateLeaseToken == "" && containsProviderStatus([]string{"reserved", "execution_claimed"}, fact.ProviderStatus)
		if beforeOutbound {
			if err := refundBillingOrderTx(tx, fact.BillingOrderID, reason); err != nil {
				return err
			}
		} else {
			var order model.BillingOrder
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", fact.BillingOrderID).Error; err != nil {
				return err
			}
			if order.Status != model.BillingStatusReserved && order.Status != model.BillingStatusRunning && order.Status != model.BillingStatusUncertain {
				return errors.New("provider cancellation billing state conflict")
			}
			if order.Status != model.BillingStatusUncertain {
				if err := tx.Model(&model.BillingOrder{}).Where("id = ? AND status = ?", order.ID, order.Status).Updates(map[string]any{
					"status": model.BillingStatusUncertain, "error": reason, "updated_at": time.Now(),
				}).Error; err != nil {
					return err
				}
			}
		}

		var task model.Task
		now := time.Now()
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, "id = ? AND user_id = ?", taskID, userID).Error; err != nil {
			return err
		}
		if task.Status != model.TaskStatusQueued && task.Status != model.TaskStatusRunning {
			return errors.New("provider task cannot be cancelled in its current state")
		}
		if task.Status == model.TaskStatusRunning && (expectedLeaseToken == "" || task.LeaseToken != expectedLeaseToken) {
			return ErrProviderTaskCancelGenerationConflict
		}
		if task.Status == model.TaskStatusQueued && expectedLeaseToken != task.LeaseToken {
			return ErrProviderTaskCancelGenerationConflict
		}
		if !beforeOutbound {
			if err := handoffCancelledSourceTaskResourceTx(tx, userID, taskID, now); err != nil {
				return err
			}
		}

		if beforeOutbound {
			factUpdate := tx.Model(&model.ProviderTaskFact{}).Where("task_id = ? AND reconciliation_status <> ?", taskID, "resolved").Updates(map[string]any{
				"provider_status": "cancelled_before_create", "reconciliation_status": "resolved", "updated_at": now,
			})
			if factUpdate.Error != nil {
				return factUpdate.Error
			}
			if factUpdate.RowsAffected != 1 {
				return errors.New("provider cancellation fact state conflict")
			}
			taskUpdate := tx.Model(&model.Task{}).Where("id = ? AND user_id = ? AND status IN ?", taskID, userID, []model.TaskStatus{model.TaskStatusQueued, model.TaskStatusRunning}).Updates(map[string]any{
				"status": model.TaskStatusCancelled, "stage": "任务已取消", "error": "", "completed_at": &now,
				"lease_owner": "", "lease_token": "", "lease_expires_at": nil, "updated_at": now,
			})
			if taskUpdate.Error != nil {
				return taskUpdate.Error
			}
			if taskUpdate.RowsAffected != 1 {
				return errors.New("provider cancellation task state conflict")
			}
			if err := resolveCancelledPendingResourceTx(tx, userID, taskID, reason, now); err != nil {
				return err
			}
			result.Decision = ProviderTaskCancelledBeforeOutbound
		} else {
			factUpdate := tx.Model(&model.ProviderTaskFact{}).Where("task_id = ? AND reconciliation_status <> ?", taskID, "resolved").Updates(map[string]any{
				"reconciliation_status": "cancel_requested", "updated_at": now,
			})
			if factUpdate.Error != nil {
				return factUpdate.Error
			}
			if factUpdate.RowsAffected != 1 {
				return errors.New("provider cancellation fact state conflict")
			}
			taskUpdate := tx.Model(&model.Task{}).Where("id = ? AND user_id = ? AND status IN ?", taskID, userID, []model.TaskStatus{model.TaskStatusQueued, model.TaskStatusRunning}).Updates(map[string]any{
				"status": model.TaskStatusQueued, "stage": "取消请求待上游核对", "error": reason, "completed_at": nil,
				"next_poll_at": now.Add(5 * time.Second), "lease_owner": "", "lease_token": "", "lease_expires_at": nil, "updated_at": now,
			})
			if taskUpdate.Error != nil {
				return taskUpdate.Error
			}
			if taskUpdate.RowsAffected != 1 {
				return errors.New("provider cancellation task state conflict")
			}
			result.Decision = ProviderTaskCancellationRequested
		}
		return tx.First(&result.Task, "id = ?", taskID).Error
	})
	return result, err
}

func handoffCancelledSourceTaskResourceTx(tx *gorm.DB, userID string, taskID string, now time.Time) error {
	var resource model.Resource
	query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND source_task_id = ?", userID, taskID).Limit(1).Find(&resource)
	if query.Error != nil || query.RowsAffected == 0 || resource.Status == model.ResourceStatusReady {
		return query.Error
	}
	if resource.Status != model.ResourceStatusPending && resource.Status != model.ResourceStatusFailed {
		return errors.New("provider cancellation source resource state is invalid")
	}
	updated := tx.Model(&model.Resource{}).Where("id = ? AND status IN ?", resource.ID, []model.ResourceStatus{model.ResourceStatusPending, model.ResourceStatusFailed}).Updates(map[string]any{
		"write_resolution": "cancel_resolving", "updated_at": now,
	})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return errors.New("provider cancellation resource handoff conflict")
	}
	return nil
}

func resolveCancelledPendingResourceTx(tx *gorm.DB, userID string, taskID string, reason string, now time.Time) error {
	var resource model.Resource
	query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND source_task_id = ?", userID, taskID).Limit(1).Find(&resource)
	if query.Error != nil || query.RowsAffected == 0 || resource.Status != model.ResourceStatusPending {
		return query.Error
	}
	if resource.QuotaReserved {
		usageID := resource.UserID + ":" + resource.QuotaDay
		if err := tx.Model(&model.UserDailyUploadUsage{}).Where("id = ?", usageID).Updates(map[string]any{
			"bytes": gorm.Expr("CASE WHEN bytes >= ? THEN bytes - ? ELSE 0 END", resource.Size, resource.Size), "updated_at": now,
		}).Error; err != nil {
			return err
		}
	}
	return tx.Model(&model.Resource{}).Where("id = ? AND status = ?", resource.ID, model.ResourceStatusPending).Updates(map[string]any{
		"status": model.ResourceStatusFailed, "error": reason, "quota_reserved": false,
		"write_token": "", "write_task_lease_token": "", "write_lease_expires_at": nil, "write_resolution": "resolved", "updated_at": now,
	}).Error
}

func (r *Repository) finalizeProviderTaskFailure(taskID string, lease ProviderTaskLease, resolution ProviderTaskFailureResolution, mutateBilling providerBillingMutation) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var fact model.ProviderTaskFact
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&fact, "task_id = ?", taskID).Error; err != nil {
			return err
		}
		if fact.ReconciliationStatus == "resolved" || !containsProviderStatus(resolution.ExpectedStatuses, fact.ProviderStatus) {
			return errors.New("provider failure fact state conflict")
		}
		if fact.ExecutionLeaseToken == "" || fact.ExecutionLeaseToken != lease.Token {
			return errors.New("provider failure generation state conflict")
		}
		if err := mutateBilling(tx, fact); err != nil {
			return err
		}
		var task model.Task
		now := time.Now()
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, "id = ?", taskID).Error; err != nil {
			return err
		}
		if task.Status != model.TaskStatusRunning || task.LeaseOwner != lease.Owner || task.LeaseToken != lease.Token || task.LeaseExpiresAt == nil || !task.LeaseExpiresAt.After(now) {
			return errors.New("provider failure task lease state conflict")
		}
		taskUpdate := tx.Model(&model.Task{}).Where("id = ? AND status = ? AND lease_owner = ? AND lease_token = ?", taskID, model.TaskStatusRunning, lease.Owner, lease.Token).Updates(map[string]any{
			"status": model.TaskStatusFailed, "stage": resolution.TaskStage, "error": resolution.TaskError, "completed_at": &now,
			"lease_owner": "", "lease_token": "", "lease_expires_at": nil, "updated_at": now,
		})
		if taskUpdate.Error != nil {
			return taskUpdate.Error
		}
		if taskUpdate.RowsAffected != 1 {
			return errors.New("provider failure task state conflict")
		}
		factUpdates := map[string]any{
			"provider_status": resolution.ProviderStatus, "reconciliation_status": resolution.ReconciliationStatus, "updated_at": now,
		}
		if strings.HasPrefix(resolution.ProviderStatus, "create_") {
			factUpdates["create_trace_id"] = strings.TrimSpace(resolution.TraceID)
		} else {
			factUpdates["last_poll_trace_id"] = strings.TrimSpace(resolution.TraceID)
		}
		factUpdate := tx.Model(&model.ProviderTaskFact{}).Where("task_id = ? AND provider_status = ?", taskID, fact.ProviderStatus).Updates(factUpdates)
		if factUpdate.Error != nil {
			return factUpdate.Error
		}
		if factUpdate.RowsAffected != 1 {
			return errors.New("provider failure fact update conflict")
		}
		return nil
	})
}

func containsProviderStatus(statuses []string, candidate string) bool {
	for _, status := range statuses {
		if status == candidate {
			return true
		}
	}
	return false
}

func (r *Repository) MarkProviderTaskCreateStartedForLease(taskID string, lease ProviderTaskLease) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		fact, _, err := lockProviderTaskTransitionTx(tx, taskID, lease)
		if err != nil {
			return err
		}
		if fact.ProviderTaskID != "" || (fact.ProviderStatus != "reserved" && fact.ProviderStatus != "execution_claimed") {
			return errors.New("provider task create start state conflict")
		}
		updated := tx.Model(&model.ProviderTaskFact{}).Where("task_id = ? AND provider_status IN ?", taskID, []string{"reserved", "execution_claimed"}).Updates(map[string]any{
			"provider_status": "creating", "execution_lease_token": lease.Token, "create_lease_token": lease.Token, "updated_at": time.Now(),
		})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return errors.New("provider task create start state conflict")
		}
		return nil
	})
}

func (r *Repository) ClaimProviderTaskExecution(taskID string, leaseOwner string, leaseToken string) (bool, error) {
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
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, "id = ? AND status = ? AND lease_owner = ? AND lease_token = ? AND lease_expires_at > ?", taskID, model.TaskStatusRunning, leaseOwner, leaseToken, time.Now()).Error; err != nil {
			return err
		}
		if providerTaskRecoveryStatus(fact.ProviderStatus) {
			if err := tx.Model(&model.ProviderTaskFact{}).Where("task_id = ?", taskID).Updates(map[string]any{"execution_lease_token": leaseToken, "updated_at": time.Now()}).Error; err != nil {
				return err
			}
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
			Where("provider_credential_id = ? AND task_id <> ? AND ((reconciliation_status IN ? AND provider_status IN ?) OR provider_status IN ?)", credential.ID, taskID, []string{"pending", "cancel_requested"}, providerCapacityOccupancyStatuses(), resolvedProviderObservationOccupancyStatuses()).
			Count(&active).Error; err != nil {
			return err
		}
		if active >= int64(credential.ConcurrencyLimit) {
			return nil
		}
		result := tx.Model(&model.ProviderTaskFact{}).Where("task_id = ? AND provider_status = ?", taskID, "reserved").Updates(map[string]any{"provider_status": "execution_claimed", "execution_lease_token": leaseToken, "updated_at": time.Now()})
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

func (r *Repository) RequeueTaskWaitingForProviderCapacity(taskID string, leaseOwner string, leaseToken string) error {
	now := time.Now()
	result := r.db.Model(&model.Task{}).
		Where("id = ? AND status = ? AND lease_owner = ? AND lease_token = ? AND lease_expires_at > ?", taskID, model.TaskStatusRunning, leaseOwner, leaseToken, now).
		Updates(map[string]any{
			"status": model.TaskStatusQueued, "stage": "等待上游并发名额", "progress": 5,
			"next_poll_at": now.Add(5 * time.Second), "lease_owner": "", "lease_token": "", "lease_expires_at": nil, "updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("provider capacity requeue state conflict")
	}
	return nil
}

func (r *Repository) RequeueProviderTaskPostprocess(taskID string, lease ProviderTaskLease, reason string) error {
	now := time.Now()
	return r.db.Transaction(func(tx *gorm.DB) error {
		var fact model.ProviderTaskFact
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&fact, "task_id = ?", taskID).Error; err != nil {
			return err
		}
		if !ResolvedProviderSuccessStatus(fact.ProviderStatus) || fact.ExecutionLeaseToken != lease.Token {
			return errors.New("provider postprocess fact state conflict")
		}
		var order model.BillingOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", fact.BillingOrderID).Error; err != nil {
			return err
		}
		if fact.ReconciliationStatus == "resolved" && order.Status != model.BillingStatusSettled && order.Status != model.BillingStatusRefunded {
			return errors.New("resolved provider postprocess billing state conflict")
		}
		var task model.Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, "id = ?", taskID).Error; err != nil {
			return err
		}
		if task.Status != model.TaskStatusRunning || task.LeaseOwner != lease.Owner || task.LeaseToken != lease.Token || task.LeaseExpiresAt == nil || !task.LeaseExpiresAt.After(now) {
			return errors.New("provider postprocess task lease state conflict")
		}
		if fact.ReconciliationStatus == "resolved" {
			var resource model.Resource
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&resource, "source_task_id = ?", taskID).Error; err != nil {
				return err
			}
			if (resource.Status != model.ResourceStatusReady && resource.Status != model.ResourceStatusFailed) || resource.WriteResolution != "resolved" {
				return errors.New("resolved provider postprocess resource state conflict")
			}
			if resource.Status == model.ResourceStatusFailed {
				if err := tx.Model(&model.Resource{}).Where("id = ? AND status = ? AND write_resolution = ?", resource.ID, model.ResourceStatusFailed, "resolved").Update("write_resolution", "resolving").Error; err != nil {
					return err
				}
			}
		}
		updated := tx.Model(&model.Task{}).
			Where("id = ? AND status = ? AND lease_owner = ? AND lease_token = ?", taskID, model.TaskStatusRunning, lease.Owner, lease.Token).
			Updates(map[string]any{
				"status": model.TaskStatusQueued, "stage": "等待保存上游成功结果", "error": strings.TrimSpace(reason),
				"next_poll_at": now.Add(5 * time.Second), "lease_owner": "", "lease_token": "", "lease_expires_at": nil, "updated_at": now,
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return errors.New("provider postprocess task lease state conflict")
		}
		return nil
	})
}

func (r *Repository) DecideProviderCreateUncertain(taskID string, lease ProviderTaskLease, traceID string, reason string) (ProviderCreateUncertainDecision, error) {
	decision := ProviderCreateUncertainDecision("")
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var fact model.ProviderTaskFact
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&fact, "task_id = ?", taskID).Error; err != nil {
			return err
		}
		if fact.ReconciliationStatus == "resolved" || fact.ExecutionLeaseToken == "" || fact.ExecutionLeaseToken != lease.Token {
			return errors.New("provider create uncertainty generation state conflict")
		}
		var order model.BillingOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", fact.BillingOrderID).Error; err != nil {
			return err
		}
		now := time.Now()
		var task model.Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, "id = ?", taskID).Error; err != nil {
			return err
		}
		if task.Status != model.TaskStatusRunning || task.LeaseOwner != lease.Owner || task.LeaseToken != lease.Token || task.LeaseExpiresAt == nil || !task.LeaseExpiresAt.After(now) {
			return errors.New("provider create uncertainty task lease state conflict")
		}
		if fact.ProviderTaskID != "" && ProviderExecutionStatus(fact.ProviderStatus) {
			updated := tx.Model(&model.Task{}).
				Where("id = ? AND status = ? AND lease_owner = ? AND lease_token = ?", taskID, model.TaskStatusRunning, lease.Owner, lease.Token).
				Updates(map[string]any{
					"status": model.TaskStatusQueued, "stage": "等待接管上游任务", "error": strings.TrimSpace(reason),
					"next_poll_at": now.Add(time.Second), "lease_owner": "", "lease_token": "", "lease_expires_at": nil, "updated_at": now,
				})
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return errors.New("provider create handoff task state conflict")
			}
			decision = ProviderCreateUncertainRequeued
			return nil
		}
		if fact.ProviderTaskID != "" || fact.ProviderStatus != "creating" {
			return errors.New("provider create uncertainty fact state conflict")
		}
		if order.Status != model.BillingStatusReserved && order.Status != model.BillingStatusRunning && order.Status != model.BillingStatusUncertain {
			return errors.New("provider create uncertainty billing state conflict")
		}
		if order.Status != model.BillingStatusUncertain {
			billingUpdate := tx.Model(&model.BillingOrder{}).Where("id = ? AND status = ?", order.ID, order.Status).Updates(map[string]any{
				"status": model.BillingStatusUncertain, "error": strings.TrimSpace(reason), "updated_at": now,
			})
			if billingUpdate.Error != nil {
				return billingUpdate.Error
			}
			if billingUpdate.RowsAffected != 1 {
				return errors.New("provider create uncertainty billing update conflict")
			}
		}
		factUpdate := tx.Model(&model.ProviderTaskFact{}).Where("task_id = ? AND provider_status = ? AND provider_task_id = ''", taskID, "creating").Updates(map[string]any{
			"provider_status": "create_uncertain", "reconciliation_status": "manual_review", "create_trace_id": strings.TrimSpace(traceID), "updated_at": now,
		})
		if factUpdate.Error != nil {
			return factUpdate.Error
		}
		if factUpdate.RowsAffected != 1 {
			return errors.New("provider create uncertainty fact update conflict")
		}
		taskUpdate := tx.Model(&model.Task{}).
			Where("id = ? AND status = ? AND lease_owner = ? AND lease_token = ?", taskID, model.TaskStatusRunning, lease.Owner, lease.Token).
			Updates(map[string]any{
				"status": model.TaskStatusFailed, "stage": "任务失败", "error": strings.TrimSpace(reason), "completed_at": &now,
				"lease_owner": "", "lease_token": "", "lease_expires_at": nil, "updated_at": now,
			})
		if taskUpdate.Error != nil {
			return taskUpdate.Error
		}
		if taskUpdate.RowsAffected != 1 {
			return errors.New("provider create uncertainty task state conflict")
		}
		decision = ProviderCreateUncertainManualReview
		return nil
	})
	return decision, err
}

type ProviderTaskBillingResolution struct {
	ExpectedProviderStatus       string
	ExpectedReconciliationStatus string
	ExpectedBillingStatus        model.BillingStatus
	ResolvedProviderStatus       string
	ActorUserID                  string
	Note                         string
	TaskStatus                   model.TaskStatus
	TaskStage                    string
	TaskError                    string
	PostprocessStage             string
}

type providerBillingMutation func(*gorm.DB, model.ProviderTaskFact) error

func (r *Repository) SettleProviderTaskBilling(billingOrderID string, resolution ProviderTaskBillingResolution) (bool, error) {
	return r.resolveProviderTaskBilling(billingOrderID, resolution, func(tx *gorm.DB, fact model.ProviderTaskFact) error {
		return settleBillingOrderTx(tx, billingOrderID, fact.ProviderTaskID)
	})
}

func (r *Repository) RefundProviderTaskBilling(billingOrderID string, resolution ProviderTaskBillingResolution) (bool, error) {
	return r.resolveProviderTaskBilling(billingOrderID, resolution, func(tx *gorm.DB, _ model.ProviderTaskFact) error {
		return refundBillingOrderTx(tx, billingOrderID, resolution.Note)
	})
}

func (r *Repository) resolveProviderTaskBilling(billingOrderID string, resolution ProviderTaskBillingResolution, mutateBilling providerBillingMutation) (bool, error) {
	resolved := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var fact model.ProviderTaskFact
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&fact, "billing_order_id = ?", billingOrderID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		if fact.ProviderStatus != resolution.ExpectedProviderStatus || fact.ReconciliationStatus != resolution.ExpectedReconciliationStatus {
			return errors.New("provider reconciliation fact changed before resolution")
		}
		var order model.BillingOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", billingOrderID).Error; err != nil {
			return err
		}
		if order.Status != resolution.ExpectedBillingStatus || order.TaskID != fact.TaskID {
			return errors.New("provider reconciliation billing order changed before resolution")
		}
		if err := mutateBilling(tx, fact); err != nil {
			return err
		}
		now := time.Now()
		orderUpdate := tx.Model(&model.BillingOrder{}).Where("id = ?", billingOrderID).Updates(map[string]any{
			"resolved_by": resolution.ActorUserID, "resolution_note": resolution.Note, "updated_at": now,
		})
		if orderUpdate.Error != nil {
			return orderUpdate.Error
		}
		if orderUpdate.RowsAffected != 1 {
			return errors.New("provider reconciliation billing order state conflict")
		}
		var task model.Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, "id = ?", fact.TaskID).Error; err != nil {
			return err
		}
		if task.Status != model.TaskStatusQueued && task.Status != model.TaskStatusRunning && task.Status != model.TaskStatusFailed {
			return errors.New("provider reconciliation task eligibility changed before resolution")
		}
		var resource model.Resource
		resourceQuery := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("source_task_id = ?", fact.TaskID).Limit(1).Find(&resource)
		if resourceQuery.Error != nil {
			return resourceQuery.Error
		}
		if resourceQuery.RowsAffected == 1 && resource.Status != model.ResourceStatusPending && resource.Status != model.ResourceStatusFailed && resource.Status != model.ResourceStatusReady {
			return errors.New("provider reconciliation source resource state is invalid")
		}
		hasSucceededPostprocess := fact.ProviderStatus == "succeeded"
		adoptResolvedObservation := fact.ProviderStatus == "create_uncertain" && fact.ReconciliationStatus == "manual_review" && strings.TrimSpace(fact.ProviderTaskID) != ""
		resourceNeedsPostprocess := resourceQuery.RowsAffected == 0 || resource.Status != model.ResourceStatusReady
		deferTerminalForPostprocess := hasSucceededPostprocess && resourceNeedsPostprocess
		if task.Status == model.TaskStatusQueued && fact.ReconciliationStatus != "cancel_requested" && !deferTerminalForPostprocess && !adoptResolvedObservation {
			return errors.New("provider reconciliation task is not running or terminal")
		}
		if deferTerminalForPostprocess && resourceQuery.RowsAffected == 1 {
			resourceUpdate := tx.Model(&model.Resource{}).Where("id = ? AND status IN ?", resource.ID, []model.ResourceStatus{model.ResourceStatusPending, model.ResourceStatusFailed}).Update("write_resolution", "resolving")
			if resourceUpdate.Error != nil {
				return resourceUpdate.Error
			}
			if resourceUpdate.RowsAffected != 1 {
				return errors.New("provider reconciliation resource writer changed before resolution")
			}
		}
		if adoptResolvedObservation {
			taskUpdate := tx.Model(&model.Task{}).Where("id = ? AND status = ?", fact.TaskID, task.Status).Updates(map[string]any{
				"status": model.TaskStatusQueued, "stage": "等待核对已决任务的上游观察", "error": resolution.TaskError, "completed_at": nil,
				"next_poll_at": now.Add(time.Second), "lease_owner": "", "lease_token": "", "lease_expires_at": nil, "updated_at": now,
			})
			if taskUpdate.Error != nil {
				return taskUpdate.Error
			}
			if taskUpdate.RowsAffected != 1 {
				return errors.New("provider reconciliation resolved observation handoff conflict")
			}
		} else if deferTerminalForPostprocess {
			taskUpdate := tx.Model(&model.Task{}).Where("id = ? AND status = ?", fact.TaskID, task.Status).Updates(map[string]any{
				"status": model.TaskStatusQueued, "stage": resolution.PostprocessStage, "error": resolution.TaskError, "completed_at": nil,
				"next_poll_at": now.Add(time.Second), "lease_owner": "", "lease_token": "", "lease_expires_at": nil, "updated_at": now,
			})
			if taskUpdate.Error != nil {
				return taskUpdate.Error
			}
			if taskUpdate.RowsAffected != 1 {
				return errors.New("provider reconciliation postprocess handoff conflict")
			}
		} else if task.Status == model.TaskStatusQueued || task.Status == model.TaskStatusRunning {
			taskUpdate := tx.Model(&model.Task{}).Where("id = ? AND status = ?", fact.TaskID, task.Status).Updates(map[string]any{
				"status": resolution.TaskStatus, "stage": resolution.TaskStage, "error": resolution.TaskError, "completed_at": &now,
				"lease_owner": "", "lease_token": "", "lease_expires_at": nil, "updated_at": now,
			})
			if taskUpdate.Error != nil {
				return taskUpdate.Error
			}
			if taskUpdate.RowsAffected != 1 {
				return errors.New("provider reconciliation task state conflict")
			}
		}
		resolvedProviderStatus := resolution.ResolvedProviderStatus
		factUpdates := map[string]any{"provider_status": resolvedProviderStatus, "reconciliation_status": "resolved", "updated_at": now}
		if adoptResolvedObservation {
			factUpdates["provider_status"] = "resolved_observation_submitted"
			factUpdates["execution_lease_token"] = ""
		}
		factUpdate := tx.Model(&model.ProviderTaskFact{}).Where("task_id = ?", fact.TaskID).Updates(factUpdates)
		if factUpdate.Error != nil {
			return factUpdate.Error
		}
		if factUpdate.RowsAffected != 1 {
			return errors.New("provider reconciliation fact state conflict")
		}
		resolved = true
		return nil
	})
	return resolved, err
}

func (r *Repository) CompleteResolvedProviderPostprocess(taskID string, lease ProviderTaskLease, status model.TaskStatus, stage string, taskError string, inputJSON string, resultJSON string) error {
	if status != model.TaskStatusFailed && status != model.TaskStatusCancelled {
		return errors.New("resolved provider postprocess terminal status is invalid")
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		var fact model.ProviderTaskFact
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&fact, "task_id = ?", taskID).Error; err != nil {
			return err
		}
		if !ResolvedProviderSuccessStatus(fact.ProviderStatus) || fact.ReconciliationStatus != "resolved" || fact.ExecutionLeaseToken != lease.Token {
			return errors.New("resolved provider postprocess fact state conflict")
		}
		var order model.BillingOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", fact.BillingOrderID).Error; err != nil {
			return err
		}
		if order.Status != model.BillingStatusSettled && order.Status != model.BillingStatusRefunded {
			return errors.New("resolved provider postprocess billing state conflict")
		}
		now := time.Now()
		var task model.Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, "id = ?", taskID).Error; err != nil {
			return err
		}
		if task.Status != model.TaskStatusRunning || task.LeaseOwner != lease.Owner || task.LeaseToken != lease.Token || task.LeaseExpiresAt == nil || !task.LeaseExpiresAt.After(now) {
			return errors.New("resolved provider postprocess task lease state conflict")
		}
		var resource model.Resource
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&resource, "source_task_id = ?", taskID).Error; err != nil {
			return err
		}
		if resource.Status != model.ResourceStatusReady || resource.WriteResolution != "resolved" {
			return errors.New("resolved provider postprocess resource state conflict")
		}
		updated := tx.Model(&model.Task{}).Where("id = ? AND status = ? AND lease_owner = ? AND lease_token = ?", taskID, model.TaskStatusRunning, lease.Owner, lease.Token).Updates(map[string]any{
			"status": status, "stage": strings.TrimSpace(stage), "progress": 100, "error": strings.TrimSpace(taskError), "completed_at": &now,
			"input_json": inputJSON, "result_json": resultJSON,
			"lease_owner": "", "lease_token": "", "lease_expires_at": nil, "updated_at": now,
		})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return errors.New("resolved provider postprocess task state conflict")
		}
		return nil
	})
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
	return []string{"execution_claimed", "creating", "submitted", "pending", "running", "poll_uncertain"}
}

func ProviderExecutionStatus(status string) bool {
	for _, candidate := range providerExecutionStatuses() {
		if status == candidate {
			return true
		}
	}
	return false
}

func providerCapacityOccupancyStatuses() []string {
	return []string{"execution_claimed", "creating", "submitted", "pending", "running", "poll_uncertain", "create_uncertain"}
}

func resolvedProviderObservationOccupancyStatuses() []string {
	return []string{"resolved_observation_submitted", "resolved_observation_pending", "resolved_observation_running", "resolved_observation_poll_uncertain"}
}

func providerTaskRecoveryStatus(status string) bool {
	switch status {
	case "execution_claimed", "creating", "submitted", "pending", "running", "poll_uncertain", "succeeded", "failed", "create_failed", "create_uncertain",
		"resolved_observation_submitted", "resolved_observation_pending", "resolved_observation_running", "resolved_observation_poll_uncertain", "resolved_observation_succeeded":
		return true
	default:
		return false
	}
}
