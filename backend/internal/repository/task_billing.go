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

const (
	FailedTaskBillingRefund    FailedTaskBillingAction = "refund"
	FailedTaskBillingUncertain FailedTaskBillingAction = "uncertain"
)

// FinalizeFailedTaskAndBilling 将任务终态与计费结果放进同一事务；任一侧失败时保留运行态，等待租约到期后重试。
func (r *Repository) FinalizeFailedTaskAndBilling(task *model.Task, action FailedTaskBillingAction, errorText string) error {
	if task == nil || strings.TrimSpace(task.ID) == "" {
		return errors.New("task is required")
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		if strings.TrimSpace(task.BillingOrderID) != "" {
			var order model.BillingOrder
			query := tx.Where("id = ?", task.BillingOrderID)
			if r.Dialect() == "postgres" {
				query = query.Clauses(clause.Locking{Strength: "UPDATE"})
			}
			if err := query.First(&order).Error; err != nil {
				return err
			}
			if order.TaskID != "" && order.TaskID != task.ID {
				return ErrBillingStateConflict
			}
			if order.Status != model.BillingStatusReserved && order.Status != model.BillingStatusRunning {
				return ErrBillingStateConflict
			}
			if action == FailedTaskBillingRefund {
				if err := refundBillingOrderTx(tx, &order, errorText); err != nil {
					return err
				}
			} else if action == FailedTaskBillingUncertain {
				updated := tx.Model(&model.BillingOrder{}).
					Where("id = ? AND status IN ?", order.ID, []model.BillingStatus{model.BillingStatusReserved, model.BillingStatusRunning}).
					Updates(map[string]any{"status": model.BillingStatusUncertain, "error": errorText, "updated_at": time.Now()})
				if updated.Error != nil {
					return updated.Error
				}
				if updated.RowsAffected != 1 {
					return ErrBillingStateConflict
				}
			} else {
				return fmt.Errorf("unsupported failed task billing action: %s", action)
			}
		}
		now := time.Now()
		updatedTask := tx.Model(&model.Task{}).
			Where("id = ? AND status = ? AND lease_owner = ?", task.ID, model.TaskStatusRunning, task.LeaseOwner).
			Updates(map[string]any{
				"status": model.TaskStatusFailed, "stage": task.Stage, "progress": task.Progress,
				"error": task.Error, "completed_at": &now, "lease_owner": "", "lease_expires_at": nil,
				"updated_at": now,
			})
		if updatedTask.Error != nil {
			return updatedTask.Error
		}
		if updatedTask.RowsAffected != 1 {
			return errors.New("task lease is no longer owned by this worker")
		}
		return nil
	})
}
