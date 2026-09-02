package repository

import (
	"errors"
	"math"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const providerBillingSubunitMicrocredits int64 = 1_000_000

type TokenUsageFact struct {
	InputTokens  int64
	CachedTokens int64
	OutputTokens int64
}

type TokenSettlementFact struct {
	ProviderOrderID        string
	ProviderAmountSubunits int64
	ProviderTaskStatus     string
	ProviderTotalTokens    int64
	Usage                  TokenUsageFact
	UsageStatus            string
	ReconciliationWarning  string
	SettledAt              time.Time
}

type ResponseUsageSettlementFact struct {
	ProviderRequestID  string
	Usage              TokenUsageFact
	UsageStatus        string
	AmountMicrocredits int64
	SettledAt          time.Time
}

type tokenBillingSettlement struct {
	ProviderRequestID          string
	ProviderBillingOrderID     string
	ProviderBillingAmount      int64
	ProviderBillingStatus      string
	ProviderBillingUnit        string
	ProviderBillingTotalTokens int64
	ProviderTaskStatus         string
	Usage                      TokenUsageFact
	UsageStatus                string
	ReconciliationWarning      string
	AmountMicrocredits         int64
	SettledAt                  time.Time
}

func (r *Repository) BeginTokenBillingRequest(id string, now time.Time) error {
	result := r.db.Model(&model.BillingOrder{}).
		Where("id = ? AND billing_mode = ? AND status = ?", strings.TrimSpace(id), "token_usage", model.BillingStatusReserved).
		Updates(map[string]any{"status": model.BillingStatusRunning, "started_at": &now, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrBillingStateConflict
	}
	return nil
}

func (r *Repository) MarkTokenBillingForReconciliation(id string, providerTaskID string, reason string, next time.Time) error {
	id = strings.TrimSpace(id)
	providerTaskID = strings.TrimSpace(providerTaskID)
	if id == "" || providerTaskID == "" || next.IsZero() {
		return errors.New("token billing reconciliation facts are invalid")
	}
	result := r.db.Model(&model.BillingOrder{}).
		Where("id = ? AND billing_mode = ? AND status IN ? AND (provider_request_id = '' OR provider_request_id = ?)", id, "token_usage", []model.BillingStatus{model.BillingStatusReserved, model.BillingStatusRunning, model.BillingStatusUncertain}, providerTaskID).
		Updates(map[string]any{
			"status": model.BillingStatusUncertain, "provider_request_id": providerTaskID,
			"error": truncateRepositoryText(reason, 1000), "next_reconcile_at": next, "reconcile_lease_owner": "",
			"reconcile_lease_token": "", "reconcile_lease_expires_at": nil, "updated_at": time.Now(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrBillingStateConflict
	}
	return nil
}

func (r *Repository) RecordTokenBillingUsage(id string, usage TokenUsageFact, status string) error {
	if strings.TrimSpace(id) == "" || (status != "reported" && status != "missing" && status != "invalid") || usage.InputTokens < 0 || usage.CachedTokens < 0 || usage.OutputTokens < 0 || usage.CachedTokens > usage.InputTokens {
		return errors.New("token usage facts are invalid")
	}
	result := r.db.Model(&model.BillingOrder{}).Where("id = ? AND billing_mode = ? AND status IN ?", id, "token_usage", []model.BillingStatus{model.BillingStatusReserved, model.BillingStatusRunning, model.BillingStatusUncertain}).Updates(map[string]any{
		"input_tokens": usage.InputTokens, "cached_tokens": usage.CachedTokens, "output_tokens": usage.OutputTokens, "token_usage_status": status, "updated_at": time.Now(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrBillingStateConflict
	}
	return nil
}

func (r *Repository) RecordProviderBillingObservation(id, providerOrderID string, amount int64, billingStatus, taskStatus string, totalTokens int64) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(providerOrderID) == "" || amount < 0 || totalTokens < 0 || strings.TrimSpace(billingStatus) == "" || strings.TrimSpace(taskStatus) == "" {
		return errors.New("provider billing observation facts are invalid")
	}
	result := r.db.Model(&model.BillingOrder{}).Where("id = ? AND status IN ?", id, []model.BillingStatus{model.BillingStatusReserved, model.BillingStatusRunning, model.BillingStatusUncertain}).Updates(map[string]any{
		"provider_billing_order_id": providerOrderID, "provider_billing_amount": amount, "provider_billing_status": billingStatus, "provider_billing_unit": "fen", "provider_billing_total_tokens": totalTokens, "provider_task_status": taskStatus, "updated_at": time.Now(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrBillingStateConflict
	}
	return nil
}

func (r *Repository) ClaimKuaiziBillingReconciliations(owner string, now time.Time, lease time.Duration, limit int) ([]model.BillingOrder, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" || now.IsZero() || lease <= 0 || limit <= 0 {
		return nil, errors.New("kuaizi billing reconciliation claim is invalid")
	}
	if limit > 100 {
		limit = 100
	}
	claimed := make([]model.BillingOrder, 0, limit)
	err := r.db.Transaction(func(tx *gorm.DB) error {
		query := tx.Where("status = ? AND provider_request_id <> '' AND provider_endpoint_version_id <> '' AND provider_credential_version_id <> '' AND COALESCE(provider_billing_status, '') <> ? AND ((next_reconcile_at IS NOT NULL AND next_reconcile_at <= ?) OR (next_reconcile_at IS NULL AND COALESCE(provider_billing_order_id, '') = '' AND COALESCE(provider_billing_status, '') = '')) AND (reconcile_lease_expires_at IS NULL OR reconcile_lease_expires_at <= ?) AND EXISTS (SELECT 1 FROM provider_endpoint_versions AS endpoints JOIN provider_accounts AS accounts ON accounts.id = endpoints.provider_account_id WHERE endpoints.id = billing_orders.provider_endpoint_version_id AND accounts.provider_kind = ?)", model.BillingStatusUncertain, "requires_review", now, now, "kuaizi").
			Order("next_reconcile_at asc, created_at asc").Limit(limit)
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		var candidates []model.BillingOrder
		if err := query.Find(&candidates).Error; err != nil {
			return err
		}
		leaseExpiresAt := now.Add(lease)
		for index := range candidates {
			candidate := &candidates[index]
			token := newRepositoryID()
			result := tx.Model(&model.BillingOrder{}).
				Where("id = ? AND status = ? AND COALESCE(provider_billing_status, '') <> ? AND ((next_reconcile_at IS NOT NULL AND next_reconcile_at <= ?) OR (next_reconcile_at IS NULL AND COALESCE(provider_billing_order_id, '') = '' AND COALESCE(provider_billing_status, '') = '')) AND (reconcile_lease_expires_at IS NULL OR reconcile_lease_expires_at <= ?)", candidate.ID, model.BillingStatusUncertain, "requires_review", now, now).
				Updates(map[string]any{
					"reconcile_lease_owner": owner, "reconcile_lease_token": token, "reconcile_lease_expires_at": &leaseExpiresAt,
					"reconcile_attempts": gorm.Expr("reconcile_attempts + 1"), "updated_at": now,
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				continue
			}
			candidate.ReconcileLeaseOwner = owner
			candidate.ReconcileLeaseToken = token
			candidate.ReconcileLeaseExpiresAt = &leaseExpiresAt
			candidate.ReconcileAttempts++
			claimed = append(claimed, *candidate)
		}
		return nil
	})
	return claimed, err
}

func (r *Repository) RescheduleKuaiziBillingReconciliation(id string, owner string, leaseToken string, reason string, next time.Time) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(owner) == "" || strings.TrimSpace(leaseToken) == "" || next.IsZero() {
		return errors.New("kuaizi billing reconciliation reschedule is invalid")
	}
	result := r.db.Model(&model.BillingOrder{}).
		Where("id = ? AND status = ? AND reconcile_lease_owner = ? AND reconcile_lease_token = ?", id, model.BillingStatusUncertain, owner, leaseToken).
		Updates(map[string]any{
			"error": truncateRepositoryText(reason, 1000), "next_reconcile_at": next,
			"reconcile_lease_owner": "", "reconcile_lease_token": "", "reconcile_lease_expires_at": nil, "updated_at": time.Now(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrBillingStateConflict
	}
	return nil
}

func (r *Repository) RequireKuaiziBillingReview(id string, owner string, leaseToken string, reason string) error {
	result := r.db.Model(&model.BillingOrder{}).
		Where("id = ? AND status = ? AND reconcile_lease_owner = ? AND reconcile_lease_token = ?", strings.TrimSpace(id), model.BillingStatusUncertain, strings.TrimSpace(owner), strings.TrimSpace(leaseToken)).
		Updates(map[string]any{
			"provider_billing_status": "requires_review", "error": truncateRepositoryText(reason, 1000), "next_reconcile_at": nil,
			"reconcile_lease_owner": "", "reconcile_lease_token": "", "reconcile_lease_expires_at": nil, "updated_at": time.Now(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrBillingStateConflict
	}
	return nil
}

func truncateRepositoryText(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit])
}

func (r *Repository) SettleTokenBilling(id string, fact TokenSettlementFact) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(fact.ProviderOrderID) == "" || fact.ProviderAmountSubunits < 0 || strings.TrimSpace(fact.ProviderTaskStatus) == "" || fact.ProviderTotalTokens < 0 || (fact.UsageStatus != "reported" && fact.UsageStatus != "missing" && fact.UsageStatus != "invalid") || fact.Usage.InputTokens < 0 || fact.Usage.CachedTokens < 0 || fact.Usage.OutputTokens < 0 || fact.Usage.CachedTokens > fact.Usage.InputTokens || fact.SettledAt.IsZero() {
		return errors.New("token billing settlement facts are invalid")
	}
	if fact.ProviderAmountSubunits > int64(^uint64(0)>>1)/providerBillingSubunitMicrocredits {
		return errors.New("token billing amount overflows")
	}
	settlement := tokenBillingSettlement{
		ProviderBillingOrderID: fact.ProviderOrderID, ProviderBillingAmount: fact.ProviderAmountSubunits,
		ProviderBillingStatus: "succeeded", ProviderBillingUnit: "fen", ProviderBillingTotalTokens: fact.ProviderTotalTokens,
		ProviderTaskStatus: fact.ProviderTaskStatus, Usage: fact.Usage, UsageStatus: fact.UsageStatus,
		ReconciliationWarning: fact.ReconciliationWarning, AmountMicrocredits: fact.ProviderAmountSubunits * providerBillingSubunitMicrocredits, SettledAt: fact.SettledAt,
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		return settleTokenBillingTx(tx, id, settlement, false)
	})
}

func (r *Repository) SettleTokenBillingFromResponseUsage(id string, fact ResponseUsageSettlementFact) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		return settleTokenBillingFromResponseUsageTx(tx, id, fact)
	})
}

func settleTokenBillingFromResponseUsageTx(tx *gorm.DB, id string, fact ResponseUsageSettlementFact) error {
	id = strings.TrimSpace(id)
	fact.ProviderRequestID = strings.TrimSpace(fact.ProviderRequestID)
	if id == "" || fact.ProviderRequestID == "" || fact.UsageStatus != "reported" || fact.AmountMicrocredits <= 0 || fact.SettledAt.IsZero() ||
		fact.Usage.InputTokens <= 0 || fact.Usage.CachedTokens < 0 || fact.Usage.OutputTokens <= 0 || fact.Usage.CachedTokens > fact.Usage.InputTokens || fact.Usage.InputTokens > math.MaxInt64-fact.Usage.OutputTokens {
		return errors.New("response usage settlement facts are invalid")
	}
	settlement := tokenBillingSettlement{
		ProviderRequestID: fact.ProviderRequestID, ProviderBillingAmount: fact.AmountMicrocredits,
		ProviderBillingStatus: "succeeded", ProviderBillingUnit: "microcredit",
		ProviderBillingTotalTokens: fact.Usage.InputTokens + fact.Usage.OutputTokens, ProviderTaskStatus: "succeeded",
		Usage: fact.Usage, UsageStatus: fact.UsageStatus, AmountMicrocredits: fact.AmountMicrocredits, SettledAt: fact.SettledAt,
	}
	return settleTokenBillingTx(tx, id, settlement, true)
}

func settleTokenBillingTx(tx *gorm.DB, id string, settlement tokenBillingSettlement, allowChargeAboveReservation bool) error {
	var order model.BillingOrder
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", id).Error; err != nil {
		return err
	}
	if order.BillingMode != "token_usage" {
		return ErrBillingStateConflict
	}
	if settlement.ProviderRequestID != "" {
		if order.ProviderEndpointVersionID != "" || order.ProviderCredentialVersionID != "" || (order.ProviderRequestID != "" && order.ProviderRequestID != settlement.ProviderRequestID) {
			return ErrBillingStateConflict
		}
	}
	if order.Status == model.BillingStatusSettled {
		if settledTokenBillingMatches(order, settlement) {
			return nil
		}
		return ErrBillingStateConflict
	}
	if order.Status != model.BillingStatusReserved && order.Status != model.BillingStatusRunning && order.Status != model.BillingStatusUncertain {
		return ErrBillingStateConflict
	}
	reservedAmount := order.ReservedAmountMicrocredits
	if reservedAmount <= 0 || reservedAmount != order.AmountMicrocredits {
		return errors.New("token billing reservation is inconsistent")
	}
	if settlement.AmountMicrocredits > reservedAmount && !allowChargeAboveReservation {
		return markTokenBillingOverReservationTx(tx, order, settlement)
	}
	if order.TeamID != "" {
		return settleTeamTokenBillingTx(tx, &order, settlement, reservedAmount)
	}
	return settlePersonalTokenBillingTx(tx, &order, settlement, reservedAmount)
}

func settledTokenBillingMatches(order model.BillingOrder, settlement tokenBillingSettlement) bool {
	requestMatches := settlement.ProviderRequestID == "" || order.ProviderRequestID == settlement.ProviderRequestID
	return requestMatches && order.AmountMicrocredits == settlement.AmountMicrocredits &&
		order.ProviderBillingOrderID == settlement.ProviderBillingOrderID && order.ProviderBillingAmount == settlement.ProviderBillingAmount &&
		order.ProviderBillingStatus == settlement.ProviderBillingStatus && order.ProviderBillingUnit == settlement.ProviderBillingUnit &&
		order.ProviderTaskStatus == settlement.ProviderTaskStatus && order.ProviderBillingTotalTokens == settlement.ProviderBillingTotalTokens &&
		order.TokenUsageStatus == settlement.UsageStatus && order.InputTokens == settlement.Usage.InputTokens &&
		order.CachedTokens == settlement.Usage.CachedTokens && order.OutputTokens == settlement.Usage.OutputTokens &&
		order.Error == settlement.ReconciliationWarning
}

func markTokenBillingOverReservationTx(tx *gorm.DB, order model.BillingOrder, settlement tokenBillingSettlement) error {
	columns := []string{
		"status", "provider_billing_order_id", "provider_billing_amount", "provider_billing_status", "provider_billing_unit",
		"provider_billing_total_tokens", "provider_task_status", "token_usage_status", "input_tokens", "cached_tokens", "output_tokens",
		"error", "next_reconcile_at", "reconcile_lease_owner", "reconcile_lease_token", "reconcile_lease_expires_at", "updated_at",
	}
	updates := model.BillingOrder{
		Status:                     model.BillingStatusUncertain,
		ProviderBillingOrderID:     settlement.ProviderBillingOrderID,
		ProviderBillingAmount:      settlement.ProviderBillingAmount,
		ProviderBillingStatus:      settlement.ProviderBillingStatus,
		ProviderBillingUnit:        settlement.ProviderBillingUnit,
		ProviderBillingTotalTokens: settlement.ProviderBillingTotalTokens,
		ProviderTaskStatus:         settlement.ProviderTaskStatus,
		TokenUsageStatus:           settlement.UsageStatus,
		InputTokens:                settlement.Usage.InputTokens,
		CachedTokens:               settlement.Usage.CachedTokens,
		OutputTokens:               settlement.Usage.OutputTokens,
		Error:                      "上游实际扣费超过预留上限",
		ReconcileLeaseOwner:        "",
		ReconcileLeaseToken:        "",
		NextReconcileAt:            nil,
		ReconcileLeaseExpiresAt:    nil,
		UpdatedAt:                  settlement.SettledAt,
	}
	result := tx.Model(&model.BillingOrder{}).
		Where("id = ? AND status IN ?", order.ID, []model.BillingStatus{model.BillingStatusReserved, model.BillingStatusRunning, model.BillingStatusUncertain}).
		Select(columns).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrBillingStateConflict
	}
	return nil
}

func settlePersonalTokenBillingTx(tx *gorm.DB, order *model.BillingOrder, settlement tokenBillingSettlement, reservedAmount int64) error {
	var account model.CreditAccount
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&account, "user_id = ?", order.UserID).Error; err != nil {
		return err
	}
	if account.ReservedMicrocredits < reservedAmount {
		return errors.New("reserved credit balance is inconsistent")
	}
	difference := reservedAmount - settlement.AmountMicrocredits
	if difference < 0 && account.AvailableMicrocredits < -difference {
		return ErrInsufficientCredits
	}
	account.AvailableMicrocredits += difference
	account.ReservedMicrocredits -= reservedAmount
	account.Version++
	account.UpdatedAt = settlement.SettledAt
	if err := tx.Save(&account).Error; err != nil {
		return err
	}
	if err := updateSettledTokenOrderTx(tx, order, settlement); err != nil {
		return err
	}
	consumeReference := "token:settle:" + order.ID + ":consume"
	consumeAvailableDelta := int64(0)
	consumeReservedDelta := -settlement.AmountMicrocredits
	consumeAvailableAfter := account.AvailableMicrocredits - difference
	consumeReservedAfter := account.ReservedMicrocredits + difference
	if difference < 0 {
		consumeAvailableDelta = difference
		consumeReservedDelta = -reservedAmount
		consumeAvailableAfter = account.AvailableMicrocredits
		consumeReservedAfter = account.ReservedMicrocredits
	}
	if err := tx.Create(&model.CreditLedgerEntry{
		ID: newRepositoryID(), UserID: order.UserID, Type: model.CreditLedgerConsume, AmountMicrocredits: -settlement.AmountMicrocredits,
		AvailableDeltaMicrocredits: consumeAvailableDelta, ReservedDeltaMicrocredits: consumeReservedDelta, AvailableAfterMicrocredits: consumeAvailableAfter,
		ReservedAfterMicrocredits: consumeReservedAfter, BillingOrderID: order.ID, Model: order.Model,
		ChannelID: order.ChannelID, Scene: order.Scene, ReferenceKey: &consumeReference, CreatedAt: settlement.SettledAt,
	}).Error; err != nil {
		return err
	}
	if difference <= 0 {
		return nil
	}
	releaseReference := "token:settle:" + order.ID + ":release"
	return tx.Create(&model.CreditLedgerEntry{
		ID: newRepositoryID(), UserID: order.UserID, Type: model.CreditLedgerRelease, AmountMicrocredits: difference,
		AvailableDeltaMicrocredits: difference, ReservedDeltaMicrocredits: -difference,
		AvailableAfterMicrocredits: account.AvailableMicrocredits, ReservedAfterMicrocredits: account.ReservedMicrocredits,
		BillingOrderID: order.ID, Model: order.Model, ChannelID: order.ChannelID, Scene: order.Scene,
		ReferenceKey: &releaseReference, CreatedAt: settlement.SettledAt,
	}).Error
}

func settleTeamTokenBillingTx(tx *gorm.DB, order *model.BillingOrder, settlement tokenBillingSettlement, reservedAmount int64) error {
	var account model.TeamCreditAccount
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&account, "team_id = ?", order.TeamID).Error; err != nil {
		return err
	}
	if account.ReservedMicrocredits < reservedAmount {
		return errors.New("team reserved credit balance is inconsistent")
	}
	difference := reservedAmount - settlement.AmountMicrocredits
	if difference < 0 && account.AvailableMicrocredits < -difference {
		return ErrInsufficientCredits
	}
	account.AvailableMicrocredits += difference
	account.ReservedMicrocredits -= reservedAmount
	account.Version++
	account.UpdatedAt = settlement.SettledAt
	if err := tx.Save(&account).Error; err != nil {
		return err
	}
	if err := updateSettledTokenOrderTx(tx, order, settlement); err != nil {
		return err
	}
	consumeReference := "token:settle:" + order.ID + ":consume"
	consumeAvailableDelta := int64(0)
	consumeReservedDelta := -settlement.AmountMicrocredits
	consumeAvailableAfter := account.AvailableMicrocredits - difference
	consumeReservedAfter := account.ReservedMicrocredits + difference
	if difference < 0 {
		consumeAvailableDelta = difference
		consumeReservedDelta = -reservedAmount
		consumeAvailableAfter = account.AvailableMicrocredits
		consumeReservedAfter = account.ReservedMicrocredits
	}
	if err := tx.Create(&model.TeamCreditLedgerEntry{
		ID: newRepositoryID(), TeamID: order.TeamID, ActorUserID: order.UserID, Type: model.CreditLedgerConsume,
		AmountMicrocredits: -settlement.AmountMicrocredits, AvailableDeltaMicrocredits: consumeAvailableDelta, ReservedDeltaMicrocredits: consumeReservedDelta,
		AvailableAfterMicrocredits: consumeAvailableAfter, ReservedAfterMicrocredits: consumeReservedAfter,
		BillingOrderID: order.ID, Model: order.Model, ChannelID: order.ChannelID, Scene: order.Scene,
		ReferenceKey: &consumeReference, CreatedAt: settlement.SettledAt,
	}).Error; err != nil {
		return err
	}
	if difference <= 0 {
		return nil
	}
	releaseReference := "token:settle:" + order.ID + ":release"
	return tx.Create(&model.TeamCreditLedgerEntry{
		ID: newRepositoryID(), TeamID: order.TeamID, ActorUserID: order.UserID, Type: model.CreditLedgerRelease,
		AmountMicrocredits: difference, AvailableDeltaMicrocredits: difference, ReservedDeltaMicrocredits: -difference,
		AvailableAfterMicrocredits: account.AvailableMicrocredits, ReservedAfterMicrocredits: account.ReservedMicrocredits,
		BillingOrderID: order.ID, Model: order.Model, ChannelID: order.ChannelID, Scene: order.Scene,
		ReferenceKey: &releaseReference, CreatedAt: settlement.SettledAt,
	}).Error
}

func updateSettledTokenOrderTx(tx *gorm.DB, order *model.BillingOrder, settlement tokenBillingSettlement) error {
	fields := []string{
		"status", "amount_microcredits", "provider_billing_order_id", "provider_billing_amount", "provider_billing_status", "provider_billing_unit",
		"provider_billing_total_tokens", "provider_task_status", "token_usage_status", "input_tokens", "cached_tokens", "output_tokens", "settled_at",
		"error", "next_reconcile_at", "reconcile_lease_owner", "reconcile_lease_token", "reconcile_lease_expires_at", "updated_at",
	}
	updates := model.BillingOrder{
		Status: model.BillingStatusSettled, AmountMicrocredits: settlement.AmountMicrocredits,
		ProviderBillingOrderID: settlement.ProviderBillingOrderID, ProviderBillingAmount: settlement.ProviderBillingAmount,
		ProviderBillingStatus: settlement.ProviderBillingStatus, ProviderBillingUnit: settlement.ProviderBillingUnit,
		ProviderBillingTotalTokens: settlement.ProviderBillingTotalTokens, ProviderTaskStatus: settlement.ProviderTaskStatus,
		TokenUsageStatus: settlement.UsageStatus, InputTokens: settlement.Usage.InputTokens, CachedTokens: settlement.Usage.CachedTokens,
		OutputTokens: settlement.Usage.OutputTokens, SettledAt: &settlement.SettledAt, Error: truncateRepositoryText(settlement.ReconciliationWarning, 1000),
		NextReconcileAt: nil, ReconcileLeaseOwner: "", ReconcileLeaseToken: "", ReconcileLeaseExpiresAt: nil, UpdatedAt: settlement.SettledAt,
	}
	if settlement.ProviderRequestID != "" {
		fields = append(fields, "provider_request_id")
		updates.ProviderRequestID = settlement.ProviderRequestID
	}
	result := tx.Model(&model.BillingOrder{}).Where("id = ? AND status IN ?", order.ID, []model.BillingStatus{model.BillingStatusReserved, model.BillingStatusRunning, model.BillingStatusUncertain}).Select(fields).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrBillingStateConflict
	}
	return nil
}

func (r *Repository) RefundTokenBilling(id, providerOrderID string, providerAmountSubunits int64, providerTaskStatus string, providerTotalTokens int64, errorText string) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(providerOrderID) == "" || providerAmountSubunits != 0 || strings.TrimSpace(providerTaskStatus) == "" || providerTotalTokens < 0 {
		return errors.New("token billing refund facts are invalid")
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		var order model.BillingOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", id).Error; err != nil {
			return err
		}
		if order.BillingMode != "token_usage" {
			return ErrBillingStateConflict
		}
		if order.Status == model.BillingStatusRefunded {
			if order.ProviderBillingOrderID == providerOrderID && order.ProviderBillingAmount == providerAmountSubunits && order.ProviderBillingStatus == "failed" && order.ProviderTaskStatus == providerTaskStatus && order.ProviderBillingTotalTokens == providerTotalTokens {
				return nil
			}
			return ErrBillingStateConflict
		}
		if order.Status == model.BillingStatusSettled {
			return ErrBillingStateConflict
		}
		if err := tx.Model(&order).Updates(map[string]any{
			"provider_billing_order_id": providerOrderID, "provider_billing_amount": providerAmountSubunits, "provider_billing_status": "failed", "provider_billing_unit": "fen", "provider_billing_total_tokens": providerTotalTokens, "provider_task_status": providerTaskStatus,
		}).Error; err != nil {
			return err
		}
		return refundBillingOrderTx(tx, &order, errorText)
	})
}
