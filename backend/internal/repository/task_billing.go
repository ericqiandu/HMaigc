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
type CompletedTaskBillingAction string

var ErrTaskCompletionStateConflict = errors.New("task completion state conflict")
var ErrTaskOutboxConflict = errors.New("task outbox conflict")
var ErrTaskProviderDispatchConflict = errors.New("task provider dispatch conflict")

const (
	FailedTaskBillingRefund    FailedTaskBillingAction = "refund"
	FailedTaskBillingUncertain FailedTaskBillingAction = "uncertain"

	CompletedTaskBillingSettle          CompletedTaskBillingAction = "settle"
	CompletedTaskBillingSettleFromUsage CompletedTaskBillingAction = "settle_from_usage"
	CompletedTaskBillingUncertain       CompletedTaskBillingAction = "uncertain"
)

type TaskOutboxDraft struct {
	IdempotencyKey string
	EventType      model.TaskOutboxEventType
	PayloadJSON    string
	AvailableAt    time.Time
}

type SucceededTaskFinalization struct {
	Task                    *model.Task
	Session                 *model.Session
	Message                 *model.Message
	Results                 []model.Result
	BillingAction           CompletedTaskBillingAction
	BillingError            string
	ReportedTokenUsage      *TokenUsageFact
	ResponseUsageSettlement *ResponseUsageSettlementFact
	Outbox                  *TaskOutboxDraft
}

// BeginClaimedTokenProviderDispatch is the durable no-resend boundary for a
// synchronous provider call. The task marker and token reservation become
// running in one transaction immediately before network I/O.
func (r *Repository) BeginClaimedTokenProviderDispatch(task *model.Task, pollStage string, now time.Time) error {
	if task == nil || strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.UserID) == "" ||
		strings.TrimSpace(task.BillingOrderID) == "" || strings.TrimSpace(pollStage) == "" || now.IsZero() ||
		!validTaskLease(task.LeaseOwner, task.LeaseGeneration, task.LeaseToken) {
		return ErrTaskProviderDispatchConflict
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		var stored model.Task
		query := tx.Where(
			"id = ? AND user_id = ? AND status = ? AND lease_owner = ? AND lease_generation = ? AND lease_token = ?",
			task.ID, task.UserID, model.TaskStatusRunning, task.LeaseOwner, task.LeaseGeneration, task.LeaseToken,
		)
		if r.Dialect() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.Take(&stored).Error; err != nil {
			return ErrTaskProviderDispatchConflict
		}
		if stored.BillingOrderID != task.BillingOrderID || stored.PollStage != "" || stored.ProviderRequestID != "" {
			return ErrTaskProviderDispatchConflict
		}
		var order model.BillingOrder
		orderQuery := tx.Where("id = ?", stored.BillingOrderID)
		if r.Dialect() == "postgres" {
			orderQuery = orderQuery.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := orderQuery.Take(&order).Error; err != nil {
			return err
		}
		if order.TaskID != stored.ID || order.UserID != stored.UserID || order.BillingMode != "token_usage" ||
			order.Status != model.BillingStatusReserved || order.ProviderRequestID != "" {
			return ErrTaskProviderDispatchConflict
		}
		updatedOrder := tx.Model(&model.BillingOrder{}).
			Where("id = ? AND status = ? AND provider_request_id = ?", order.ID, model.BillingStatusReserved, "").
			Select("status", "started_at", "updated_at").
			Updates(model.BillingOrder{Status: model.BillingStatusRunning, StartedAt: &now, UpdatedAt: now})
		if updatedOrder.Error != nil {
			return updatedOrder.Error
		}
		if updatedOrder.RowsAffected != 1 {
			return ErrTaskProviderDispatchConflict
		}
		updatedTask := tx.Model(&model.Task{}).
			Where(
				"id = ? AND status = ? AND lease_owner = ? AND lease_generation = ? AND lease_token = ? AND poll_stage = ? AND provider_request_id = ?",
				stored.ID, model.TaskStatusRunning, stored.LeaseOwner, stored.LeaseGeneration, stored.LeaseToken, "", "",
			).
			Select("poll_stage", "updated_at").
			Updates(model.Task{PollStage: pollStage, UpdatedAt: now})
		if updatedTask.Error != nil {
			return updatedTask.Error
		}
		if updatedTask.RowsAffected != 1 {
			return ErrTaskProviderDispatchConflict
		}
		return nil
	})
}

// FinalizeSucceededTaskAndBilling commits the durable facts that prove a
// provider success. External timeline delivery is represented by Outbox and is
// intentionally performed only after this transaction commits.
func (r *Repository) FinalizeSucceededTaskAndBilling(input SucceededTaskFinalization) error {
	task := input.Task
	if task == nil || strings.TrimSpace(task.ID) == "" || task.Status != model.TaskStatusSucceeded || task.CompletedAt == nil ||
		!validTaskLease(task.LeaseOwner, task.LeaseGeneration, task.LeaseToken) {
		return ErrTaskCompletionStateConflict
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := r.finalizeSucceededBillingTx(tx, task, input); err != nil {
			return err
		}
		now := time.Now().UTC()
		updated := tx.Model(&model.Task{}).
			Where("id = ? AND user_id = ? AND status = ? AND lease_owner = ? AND lease_generation = ? AND lease_token = ?", task.ID, task.UserID, model.TaskStatusRunning, task.LeaseOwner, task.LeaseGeneration, task.LeaseToken).
			Select("status", "stage", "progress", "result_json", "input_json", "error", "provider_request_id", "poll_stage", "completed_at", "lease_owner", "lease_expires_at", "lease_token", "updated_at").
			Updates(model.Task{
				Status: task.Status, Stage: task.Stage, Progress: task.Progress, ResultJSON: task.ResultJSON,
				InputJSON: task.InputJSON, Error: "", ProviderRequestID: task.ProviderRequestID, PollStage: task.PollStage,
				CompletedAt: task.CompletedAt,
				LeaseOwner:  "", LeaseExpiresAt: nil, LeaseToken: "", UpdatedAt: now,
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrTaskCompletionStateConflict
		}
		if input.Session != nil {
			if err := tx.Save(input.Session).Error; err != nil {
				return err
			}
		}
		if input.Message != nil {
			if err := tx.Create(input.Message).Error; err != nil {
				return err
			}
		}
		for index := range input.Results {
			if err := tx.Create(&input.Results[index]).Error; err != nil {
				return err
			}
		}
		if input.Outbox != nil {
			return enqueueTaskOutboxTx(tx, task.ID, *input.Outbox, now)
		}
		return nil
	})
}

func (r *Repository) finalizeSucceededBillingTx(tx *gorm.DB, task *model.Task, input SucceededTaskFinalization) error {
	if strings.TrimSpace(task.BillingOrderID) == "" {
		return nil
	}
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
	switch input.BillingAction {
	case CompletedTaskBillingSettle:
		if order.BillingMode == "token_usage" {
			return errors.New("token usage billing must be reconciled explicitly")
		}
		return New(tx).SettleBillingOrder(order.ID, task.ProviderRequestID)
	case CompletedTaskBillingSettleFromUsage:
		if input.ResponseUsageSettlement == nil || strings.TrimSpace(task.ProviderRequestID) == "" || strings.TrimSpace(task.ProviderRequestID) != strings.TrimSpace(input.ResponseUsageSettlement.ProviderRequestID) {
			return errors.New("response usage completion billing requires exact provider request facts")
		}
		return settleTokenBillingFromResponseUsageTx(tx, order.ID, *input.ResponseUsageSettlement)
	case CompletedTaskBillingUncertain:
		if strings.TrimSpace(input.BillingError) == "" {
			return errors.New("uncertain completion billing requires an audit reason")
		}
		if input.ReportedTokenUsage != nil &&
			(input.ReportedTokenUsage.InputTokens <= 0 || input.ReportedTokenUsage.CachedTokens < 0 ||
				input.ReportedTokenUsage.CachedTokens > input.ReportedTokenUsage.InputTokens || input.ReportedTokenUsage.OutputTokens <= 0) {
			return errors.New("reported completion token usage is invalid")
		}
		if order.Status == model.BillingStatusUncertain {
			fields := []string{"error", "updated_at"}
			if strings.TrimSpace(task.ProviderRequestID) != "" {
				order.ProviderRequestID = task.ProviderRequestID
				fields = append(fields, "provider_request_id")
			}
			if input.ReportedTokenUsage != nil {
				order.InputTokens = input.ReportedTokenUsage.InputTokens
				order.CachedTokens = input.ReportedTokenUsage.CachedTokens
				order.OutputTokens = input.ReportedTokenUsage.OutputTokens
				order.TokenUsageStatus = "reported"
				fields = append(fields, "input_tokens", "cached_tokens", "output_tokens", "token_usage_status")
			}
			order.Error = input.BillingError
			order.UpdatedAt = time.Now().UTC()
			return tx.Model(&model.BillingOrder{}).
				Where("id = ? AND status = ?", order.ID, model.BillingStatusUncertain).
				Select(fields).
				Updates(order).Error
		}
		if order.Status != model.BillingStatusReserved && order.Status != model.BillingStatusRunning {
			return ErrBillingStateConflict
		}
		now := time.Now().UTC()
		updates := uncertainBillingUpdates(order, input.BillingError, now)
		if strings.TrimSpace(task.ProviderRequestID) != "" {
			updates["provider_request_id"] = task.ProviderRequestID
		}
		if input.ReportedTokenUsage != nil {
			updates["input_tokens"] = input.ReportedTokenUsage.InputTokens
			updates["cached_tokens"] = input.ReportedTokenUsage.CachedTokens
			updates["output_tokens"] = input.ReportedTokenUsage.OutputTokens
			updates["token_usage_status"] = "reported"
		}
		result := tx.Model(&model.BillingOrder{}).
			Where("id = ? AND status IN ?", order.ID, []model.BillingStatus{model.BillingStatusReserved, model.BillingStatusRunning}).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrBillingStateConflict
		}
		return nil
	default:
		return fmt.Errorf("unsupported completed task billing action: %s", input.BillingAction)
	}
}

// FinalizeFailedTaskAndBilling 将任务终态与计费结果放进同一事务；任一侧失败时保留运行态，等待租约到期后重试。
func (r *Repository) FinalizeFailedTaskAndBilling(task *model.Task, action FailedTaskBillingAction, errorText string) error {
	return r.FinalizeFailedTaskAndBillingWithOutbox(task, action, errorText, nil)
}

func (r *Repository) FinalizeFailedTaskAndBillingWithOutbox(task *model.Task, action FailedTaskBillingAction, errorText string, outbox *TaskOutboxDraft) error {
	if task == nil || strings.TrimSpace(task.ID) == "" || !validTaskLease(task.LeaseOwner, task.LeaseGeneration, task.LeaseToken) {
		return ErrTaskCompletionStateConflict
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := r.finalizeFailedBillingTx(tx, task.BillingOrderID, task.ID, task.ProviderRequestID, action, errorText); err != nil {
			return err
		}
		now := time.Now()
		updatedTask := tx.Model(&model.Task{}).
			Where("id = ? AND status = ? AND lease_owner = ? AND lease_generation = ? AND lease_token = ?", task.ID, model.TaskStatusRunning, task.LeaseOwner, task.LeaseGeneration, task.LeaseToken).
			Updates(map[string]any{
				"status": model.TaskStatusFailed, "stage": task.Stage, "progress": task.Progress,
				"error": task.Error, "provider_request_id": task.ProviderRequestID, "poll_stage": task.PollStage,
				"completed_at": &now, "lease_owner": "", "lease_expires_at": nil, "lease_token": "",
				"updated_at": now,
			})
		if updatedTask.Error != nil {
			return updatedTask.Error
		}
		if updatedTask.RowsAffected != 1 {
			return ErrTaskCompletionStateConflict
		}
		if outbox != nil {
			return enqueueTaskOutboxTx(tx, task.ID, *outbox, now.UTC())
		}
		return nil
	})
}

func (r *Repository) EnqueueTaskOutbox(taskID string, draft TaskOutboxDraft) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		return enqueueTaskOutboxTx(tx, taskID, draft, time.Now().UTC())
	})
}

func enqueueTaskOutboxTx(tx *gorm.DB, taskID string, draft TaskOutboxDraft, now time.Time) error {
	taskID = strings.TrimSpace(taskID)
	draft.IdempotencyKey = strings.TrimSpace(draft.IdempotencyKey)
	if taskID == "" || draft.IdempotencyKey == "" || draft.EventType == "" || strings.TrimSpace(draft.PayloadJSON) == "" {
		return ErrTaskOutboxConflict
	}
	if draft.AvailableAt.IsZero() {
		draft.AvailableAt = now
	}
	record := model.TaskOutbox{
		ID: newRepositoryID(), IdempotencyKey: draft.IdempotencyKey, TaskID: taskID,
		EventType: draft.EventType, PayloadJSON: draft.PayloadJSON, Status: model.TaskOutboxPending,
		AvailableAt: draft.AvailableAt.UTC(), CreatedAt: now, UpdatedAt: now,
	}
	created := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "idempotency_key"}}, DoNothing: true}).Create(&record)
	if created.Error != nil {
		return created.Error
	}
	if created.RowsAffected == 1 {
		return nil
	}
	var existing model.TaskOutbox
	if err := tx.Where("idempotency_key = ?", draft.IdempotencyKey).First(&existing).Error; err != nil {
		return err
	}
	if existing.TaskID != taskID || existing.EventType != draft.EventType || existing.PayloadJSON != draft.PayloadJSON {
		return ErrTaskOutboxConflict
	}
	return nil
}

func (r *Repository) ClaimTaskOutbox(owner string, now time.Time, leaseDuration time.Duration, limit int) ([]model.TaskOutbox, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" || now.IsZero() || leaseDuration <= 0 || limit <= 0 || limit > 100 {
		return nil, ErrTaskOutboxConflict
	}
	claimed := make([]model.TaskOutbox, 0, limit)
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var candidates []model.TaskOutbox
		query := tx.Where(
			"(status = ? AND available_at <= ?) OR (status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?)",
			model.TaskOutboxPending, now, model.TaskOutboxProcessing, now,
		).Order("available_at ASC, created_at ASC").Limit(limit)
		if r.Dialect() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		if err := query.Find(&candidates).Error; err != nil {
			return err
		}
		for index := range candidates {
			token, err := newTaskLeaseToken()
			if err != nil {
				return err
			}
			expiresAt := now.Add(leaseDuration)
			candidate := &candidates[index]
			updated := tx.Exec(`UPDATE task_outboxes
				SET status = ?, attempt_count = attempt_count + 1, lease_owner = ?, lease_token = ?,
					lease_expires_at = ?, last_error = ?, updated_at = ?
				WHERE id = ? AND ((status = ? AND available_at <= ?)
					OR (status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?))`,
				model.TaskOutboxProcessing, owner, token, expiresAt, "", now,
				candidate.ID, model.TaskOutboxPending, now, model.TaskOutboxProcessing, now)
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				continue
			}
			candidate.Status = model.TaskOutboxProcessing
			candidate.AttemptCount++
			candidate.LeaseOwner = owner
			candidate.LeaseToken = token
			candidate.LeaseExpiresAt = &expiresAt
			candidate.LastError = ""
			candidate.UpdatedAt = now
			claimed = append(claimed, *candidate)
		}
		return nil
	})
	return claimed, err
}

func (r *Repository) CompleteTaskOutbox(id string, owner string, token string, deliveredAt time.Time) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(owner) == "" || strings.TrimSpace(token) == "" || deliveredAt.IsZero() {
		return ErrTaskOutboxConflict
	}
	result := r.db.Model(&model.TaskOutbox{}).
		Where("id = ? AND status = ? AND lease_owner = ? AND lease_token = ?", id, model.TaskOutboxProcessing, owner, token).
		Select("status", "delivered_at", "lease_owner", "lease_token", "lease_expires_at", "last_error", "updated_at").
		Updates(model.TaskOutbox{
			Status: model.TaskOutboxDelivered, DeliveredAt: &deliveredAt,
			LeaseOwner: "", LeaseToken: "", LeaseExpiresAt: nil, LastError: "", UpdatedAt: deliveredAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrTaskOutboxConflict
	}
	return nil
}

func (r *Repository) RescheduleTaskOutbox(id string, owner string, token string, deliveryError error, availableAt time.Time) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(owner) == "" || strings.TrimSpace(token) == "" || deliveryError == nil || availableAt.IsZero() {
		return ErrTaskOutboxConflict
	}
	result := r.db.Model(&model.TaskOutbox{}).
		Where("id = ? AND status = ? AND lease_owner = ? AND lease_token = ?", id, model.TaskOutboxProcessing, owner, token).
		Select("status", "available_at", "lease_owner", "lease_token", "lease_expires_at", "last_error", "updated_at").
		Updates(model.TaskOutbox{
			Status: model.TaskOutboxPending, AvailableAt: availableAt, LeaseOwner: "", LeaseToken: "",
			LeaseExpiresAt: nil, LastError: deliveryError.Error(), UpdatedAt: time.Now().UTC(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrTaskOutboxConflict
	}
	return nil
}

func (r *Repository) finalizeFailedBillingTx(tx *gorm.DB, orderID string, taskID string, providerRequestID string, action FailedTaskBillingAction, errorText string) error {
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
			updates := uncertainBillingUpdates(order, errorText, now)
			if strings.TrimSpace(providerRequestID) != "" {
				updates["provider_request_id"] = strings.TrimSpace(providerRequestID)
			}
			updated := tx.Model(&model.BillingOrder{}).
				Where("id = ? AND status IN ?", order.ID, []model.BillingStatus{model.BillingStatusReserved, model.BillingStatusRunning}).
				Updates(updates)
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return ErrBillingStateConflict
			}
			return nil
		}
		updates := uncertainBillingUpdates(order, order.Error, time.Now())
		if strings.TrimSpace(providerRequestID) != "" {
			updates["provider_request_id"] = strings.TrimSpace(providerRequestID)
		}
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
