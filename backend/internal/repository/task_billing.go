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

type FailedTaskBillingAction string

var ErrTaskCompletionStateConflict = errors.New("task completion state conflict")

const (
	FailedTaskBillingRefund    FailedTaskBillingAction = "refund"
	FailedTaskBillingUncertain FailedTaskBillingAction = "uncertain"
)

// FinalizeFailedTaskAndBilling 将任务终态与计费结果放进同一事务；任一侧失败时保留运行态，等待租约到期后重试。
func (r *Repository) FinalizeFailedTaskAndBilling(task *model.Task, action FailedTaskBillingAction, errorText string) error {
	if task == nil || strings.TrimSpace(task.ID) == "" || !validTaskLease(task.LeaseOwner, task.LeaseGeneration, task.LeaseToken) {
		return ErrTaskCompletionStateConflict
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := r.finalizeFailedBillingTx(tx, task.BillingOrderID, task.ID, action, errorText); err != nil {
			return err
		}
		now := time.Now()
		updatedTask := tx.Model(&model.Task{}).
			Where("id = ? AND status = ? AND lease_owner = ? AND lease_generation = ? AND lease_token = ?", task.ID, model.TaskStatusRunning, task.LeaseOwner, task.LeaseGeneration, task.LeaseToken).
			Updates(map[string]any{
				"status": model.TaskStatusFailed, "stage": task.Stage, "progress": task.Progress,
				"error": task.Error, "completed_at": &now, "lease_owner": "", "lease_expires_at": nil, "lease_token": "",
				"updated_at": now,
			})
		if updatedTask.Error != nil {
			return updatedTask.Error
		}
		if updatedTask.RowsAffected != 1 {
			return ErrTaskCompletionStateConflict
		}
		return nil
	})
}

func (r *Repository) finalizeFailedBillingTx(tx *gorm.DB, orderID string, taskID string, action FailedTaskBillingAction, errorText string) error {
	if strings.TrimSpace(orderID) == "" {
		return nil
	}
	var order model.BillingOrder
	query := tx.Where("id = ?", orderID)
	if r.Dialect() == "postgres" {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.First(&order).Error; err != nil {
		return err
	}
	if order.TaskID != "" && order.TaskID != taskID {
		return ErrBillingStateConflict
	}
	switch action {
	case FailedTaskBillingRefund:
		if order.Status != model.BillingStatusReserved && order.Status != model.BillingStatusRunning {
			return ErrBillingStateConflict
		}
		return refundBillingOrderTx(tx, &order, errorText)
	case FailedTaskBillingUncertain:
		if order.Status != model.BillingStatusUncertain {
			now := time.Now()
			updated := tx.Model(&model.BillingOrder{}).
				Where("id = ? AND status IN ?", order.ID, []model.BillingStatus{model.BillingStatusReserved, model.BillingStatusRunning}).
				Updates(uncertainBillingUpdates(order, errorText, now))
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return ErrBillingStateConflict
			}
			return nil
		}
		updates := uncertainBillingUpdates(order, order.Error, time.Now())
		if strings.TrimSpace(order.Error) == "" {
			updates["error"] = errorText
		}
		return tx.Model(&model.BillingOrder{}).Where("id = ? AND status = ?", order.ID, model.BillingStatusUncertain).Updates(updates).Error
	default:
		return fmt.Errorf("unsupported failed task billing action: %s", action)
	}
}

func uncertainBillingUpdates(order model.BillingOrder, errorText string, now time.Time) map[string]any {
	updates := map[string]any{
		"status": model.BillingStatusUncertain, "error": errorText, "updated_at": now,
	}
	if order.BillingMode != "token_usage" && strings.TrimSpace(order.ProviderRequestID) != "" && strings.TrimSpace(order.ProviderEndpointVersionID) != "" && strings.TrimSpace(order.ProviderCredentialVersionID) != "" {
		updates["next_reconcile_at"] = now.Add(5 * time.Second)
	}
	return updates
}
