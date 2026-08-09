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

var (
	ErrPaymentWebhookConflict        = errors.New("payment webhook conflicts with an existing event")
	ErrPaymentTransactionNotPayable  = errors.New("payment transaction is not payable")
	ErrPaymentChannelLocked          = errors.New("another payment channel already owns the payable transaction")
	ErrPaymentOrderNotPayable        = errors.New("payment order is not payable")
	ErrPaymentVerifiedFactExists     = errors.New("a verified payment fact already exists for the order")
	ErrPaymentReconciliationRequired = errors.New("payment transaction requires provider reconciliation")
)

type PaymentFulfillment struct {
	Event            *model.PaymentWebhookEvent
	TransactionID    string
	ProviderTradeNo  string
	PaidAt           time.Time
	PaymentConfirmed bool
	Activation       MembershipActivation
	Audit            *model.AdminAuditEvent
}

type PaymentTransactionFilter struct {
	Keyword  string
	Provider model.PaymentProvider
	Status   model.PaymentTransactionStatus
	Limit    int
	Offset   int
}

type PaymentWebhookFilter struct {
	Provider model.PaymentProvider
	Status   model.PaymentWebhookStatus
	Limit    int
	Offset   int
}

func (r *Repository) PaymentCheckoutSessionByOrderID(orderID string) (*model.PaymentCheckoutSession, error) {
	var session model.PaymentCheckoutSession
	return &session, r.db.First(&session, "order_id = ?", orderID).Error
}

// CreateOrGetPaymentCheckoutSession 只允许首个候选成为订单的收银台事实，冲突调用读取胜者且绝不覆盖。
func (r *Repository) CreateOrGetPaymentCheckoutSession(candidate *model.PaymentCheckoutSession) (*model.PaymentCheckoutSession, error) {
	if err := r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "order_id"}},
		DoNothing: true,
	}).Create(candidate).Error; err != nil {
		return nil, err
	}
	return r.PaymentCheckoutSessionByOrderID(candidate.OrderID)
}

func (r *Repository) PaymentCheckoutSessionByTokenHash(tokenHash string) (*model.PaymentCheckoutSession, error) {
	var session model.PaymentCheckoutSession
	return &session, r.db.First(&session, "token_hash = ?", tokenHash).Error
}

func (r *Repository) UpdatePaymentTransactionCreation(id string, status model.PaymentTransactionStatus, codeURL string, failureCode string, failureReason string, now time.Time) error {
	result := r.db.Model(&model.PaymentTransaction{}).
		Where("id = ? AND status = ?", id, model.PaymentTransactionCreated).
		Select("status", "code_url", "failure_code", "failure_reason", "updated_at").
		Updates(&model.PaymentTransaction{
			Status: status, CodeURL: codeURL, FailureCode: failureCode, FailureReason: failureReason, UpdatedAt: now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("payment transaction creation state changed unexpectedly")
	}
	return nil
}

// ClaimPayablePaymentTransaction serializes the order, checkout and payable slot before any provider I/O.
func (r *Repository) ClaimPayablePaymentTransaction(candidate *model.PaymentTransaction) (*model.PaymentTransaction, bool, error) {
	if candidate == nil || candidate.Status != model.PaymentTransactionCreated || candidate.OrderID == "" || candidate.MerchantOrderNo == "" {
		return nil, false, errors.New("payable payment transaction candidate is incomplete")
	}
	var winner model.PaymentTransaction
	claimed := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := lockPayableOrderTx(tx, candidate.OrderType, candidate.OrderID, candidate.UserID); err != nil {
			return err
		}
		var checkout model.PaymentCheckoutSession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&checkout,
			"order_type = ? AND order_id = ?", candidate.OrderType, candidate.OrderID).Error; err != nil {
			return err
		}
		if checkout.Status != model.PaymentCheckoutActive || !checkout.ExpiresAt.After(candidate.CreatedAt) {
			return ErrPaymentOrderNotPayable
		}
		verified, err := verifiedPaymentFactExistsTx(tx, candidate.OrderType, candidate.OrderID)
		if err != nil {
			return err
		}
		if verified {
			return ErrPaymentVerifiedFactExists
		}
		var paidCount int64
		if err := tx.Model(&model.PaymentTransaction{}).
			Where("order_type = ? AND order_id = ? AND status = ?", candidate.OrderType, candidate.OrderID, model.PaymentTransactionPaid).
			Count(&paidCount).Error; err != nil {
			return err
		}
		if paidCount > 0 {
			return ErrPaymentOrderNotPayable
		}
		lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("order_type = ? AND order_id = ? AND status IN ?", candidate.OrderType, candidate.OrderID, []model.PaymentTransactionStatus{
				model.PaymentTransactionCreated, model.PaymentTransactionPending, model.PaymentTransactionReviewRequired,
			}).Order("created_at asc").First(&winner)
		if lookup.Error == nil {
			if winner.Provider != candidate.Provider {
				return ErrPaymentChannelLocked
			}
			return nil
		}
		if !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
			return lookup.Error
		}
		if err := tx.Create(candidate).Error; err != nil {
			return err
		}
		winner = *candidate
		claimed = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return &winner, claimed, nil
}

func lockPayableOrderTx(tx *gorm.DB, orderType model.PaymentOrderType, orderID string, userID string) error {
	payable, err := lockPaymentOrderTx(tx, orderType, orderID, userID)
	if err != nil {
		return err
	}
	if !payable {
		return ErrPaymentOrderNotPayable
	}
	return nil
}

func lockPaymentOrderTx(tx *gorm.DB, orderType model.PaymentOrderType, orderID string, userID string) (bool, error) {
	switch orderType {
	case model.PaymentOrderMembership:
		var order model.MembershipOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ? AND user_id = ?", orderID, userID).Error; err != nil {
			return false, err
		}
		return order.Status == model.MembershipOrderPending, nil
	case model.PaymentOrderCreditTopup:
		var order model.CreditTopupOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ? AND user_id = ?", orderID, userID).Error; err != nil {
			return false, err
		}
		return order.Status == model.CreditTopupOrderPending, nil
	default:
		return false, errors.New("payment transaction has unsupported order type")
	}
}

// RecordPaymentWebhookEvent commits the verified provider facts independently from entitlement fulfillment.
func (r *Repository) RecordPaymentWebhookEvent(candidate *model.PaymentWebhookEvent) (*model.PaymentWebhookEvent, bool, error) {
	if candidate == nil || candidate.Provider == "" || candidate.ProviderEventID == "" || candidate.MerchantOrderNo == "" ||
		candidate.ProviderTradeNo == "" || candidate.AmountCents <= 0 || candidate.Currency == "" || candidate.PaidAt == nil ||
		len(candidate.PayloadDigest) != 64 {
		return nil, false, errors.New("verified payment event facts are incomplete")
	}
	var stored model.PaymentWebhookEvent
	created := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var transaction model.PaymentTransaction
		if candidate.TransactionID != "" {
			var snapshot model.PaymentTransaction
			if err := tx.First(&snapshot, "id = ?", candidate.TransactionID).Error; err != nil {
				return err
			}
			if _, err := lockPaymentOrderTx(tx, snapshot.OrderType, snapshot.OrderID, snapshot.UserID); err != nil {
				return err
			}
			var checkout model.PaymentCheckoutSession
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&checkout,
				"order_type = ? AND order_id = ?", snapshot.OrderType, snapshot.OrderID).Error; err != nil {
				return err
			}
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&transaction, "id = ?", candidate.TransactionID).Error; err != nil {
				return err
			}
			if transaction.Provider != candidate.Provider || transaction.MerchantOrderNo != candidate.MerchantOrderNo {
				return ErrPaymentWebhookConflict
			}
		}
		if err := lockPaymentProviderTradeTx(tx, candidate.Provider, candidate.ProviderTradeNo); err != nil {
			return err
		}
		lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&stored, "provider = ? AND provider_event_id = ?", candidate.Provider, candidate.ProviderEventID)
		if lookup.Error == nil {
			if !samePaymentWebhookFacts(&stored, candidate) {
				return ErrPaymentWebhookConflict
			}
			return nil
		}
		if !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
			return lookup.Error
		}
		if candidate.ProviderTradeNo != "" {
			var existingTrade model.PaymentWebhookEvent
			tradeLookup := tx.
				Where("provider = ? AND provider_trade_no = ?", candidate.Provider, candidate.ProviderTradeNo).
				Order("created_at asc").First(&existingTrade)
			if tradeLookup.Error == nil && (candidate.TransactionID == "" || existingTrade.TransactionID != candidate.TransactionID) {
				candidate.Status = model.PaymentWebhookReviewRequired
				candidate.FailureCode = "provider_trade_conflict"
				candidate.FailureReason = "渠道交易号已绑定其他支付交易，必须人工复核"
			}
			if tradeLookup.Error != nil && !errors.Is(tradeLookup.Error, gorm.ErrRecordNotFound) {
				return tradeLookup.Error
			}
		}
		if err := tx.Create(candidate).Error; err != nil {
			return err
		}
		stored = *candidate
		created = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return &stored, created, nil
}

func lockPaymentProviderTradeTx(tx *gorm.DB, provider model.PaymentProvider, providerTradeNo string) error {
	lockKey := string(provider) + "\n" + providerTradeNo
	switch tx.Dialector.Name() {
	case "postgres":
		return tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, lockKey).Error
	case "sqlite":
		// SQLite has no row or advisory locks. Acquire its database writer lock before reading the trade facts.
		return tx.Exec(`UPDATE payment_webhook_events SET updated_at = updated_at WHERE 1 = 0`).Error
	default:
		return fmt.Errorf("payment provider trade locking is unsupported for %s", tx.Dialector.Name())
	}
}

func samePaymentWebhookFacts(stored *model.PaymentWebhookEvent, candidate *model.PaymentWebhookEvent) bool {
	if stored == nil || candidate == nil || stored.PaidAt == nil || candidate.PaidAt == nil {
		return false
	}
	return stored.PayloadDigest == candidate.PayloadDigest && stored.TransactionID == candidate.TransactionID &&
		stored.MerchantOrderNo == candidate.MerchantOrderNo && stored.ProviderTradeNo == candidate.ProviderTradeNo &&
		stored.AmountCents == candidate.AmountCents && stored.Currency == candidate.Currency && stored.PaidAt.Equal(*candidate.PaidAt)
}

func verifiedPaymentFactExistsTx(tx *gorm.DB, orderType model.PaymentOrderType, orderID string) (bool, error) {
	var count int64
	if err := tx.Model(&model.PaymentWebhookEvent{}).
		Joins("JOIN payment_transactions ON payment_transactions.id = payment_webhook_events.transaction_id").
		Where("payment_transactions.order_type = ? AND payment_transactions.order_id = ? AND payment_webhook_events.status IN ?",
			orderType, orderID, []model.PaymentWebhookStatus{
				model.PaymentWebhookReceived, model.PaymentWebhookReviewRequired, model.PaymentWebhookProcessed,
			}).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *Repository) MarkPaymentWebhookOutcome(id string, status model.PaymentWebhookStatus, failureCode string, failureReason string, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var event model.PaymentWebhookEvent
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&event, "id = ?", id).Error; err != nil {
			return err
		}
		if event.Status == model.PaymentWebhookProcessed {
			return nil
		}
		update := &model.PaymentWebhookEvent{
			Status: status, FailureCode: failureCode, FailureReason: failureReason, UpdatedAt: now,
		}
		fields := []string{"status", "failure_code", "failure_reason", "updated_at"}
		if status == model.PaymentWebhookProcessed {
			update.ProcessedAt = &now
			fields = append(fields, "processed_at")
		}
		return tx.Model(&model.PaymentWebhookEvent{}).Where("id = ?", id).Select(fields).Updates(update).Error
	})
}

func (r *Repository) ActivePaymentTransaction(orderType model.PaymentOrderType, orderID string, now time.Time) (*model.PaymentTransaction, error) {
	var transaction model.PaymentTransaction
	err := r.db.Where(
		"order_type = ? AND order_id = ? AND status = ? AND code_url <> ? AND expires_at > ?",
		orderType, orderID, model.PaymentTransactionPending, "", now,
	).Order("created_at desc").First(&transaction).Error
	return &transaction, err
}

func (r *Repository) PaymentTransactionByMerchantOrderNo(provider model.PaymentProvider, merchantOrderNo string) (*model.PaymentTransaction, error) {
	var transaction model.PaymentTransaction
	return &transaction, r.db.First(&transaction, "provider = ? AND merchant_order_no = ?", provider, merchantOrderNo).Error
}

func (r *Repository) FulfillPaymentTransaction(input PaymentFulfillment) (bool, error) {
	if input.Event == nil || !input.PaymentConfirmed {
		return false, errors.New("payment fulfillment requires an independently verified provider fact")
	}
	alreadyProcessed := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var transactionSnapshot model.PaymentTransaction
		if err := tx.First(&transactionSnapshot, "id = ?", input.TransactionID).Error; err != nil {
			return err
		}
		if err := lockPayableOrderTx(tx, transactionSnapshot.OrderType, transactionSnapshot.OrderID, transactionSnapshot.UserID); err != nil && !errors.Is(err, ErrPaymentOrderNotPayable) {
			return err
		}
		var checkout model.PaymentCheckoutSession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&checkout,
			"order_type = ? AND order_id = ?", transactionSnapshot.OrderType, transactionSnapshot.OrderID).Error; err != nil {
			return err
		}
		var transaction model.PaymentTransaction
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&transaction, "id = ?", input.TransactionID).Error; err != nil {
			return err
		}
		var event model.PaymentWebhookEvent
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&event, "id = ?", input.Event.ID).Error; err != nil {
			return err
		}
		if event.PayloadDigest != input.Event.PayloadDigest || event.Provider != transaction.Provider ||
			event.TransactionID != transaction.ID || event.MerchantOrderNo != transaction.MerchantOrderNo ||
			event.ProviderTradeNo != input.ProviderTradeNo {
			return ErrPaymentWebhookConflict
		}
		if event.Status == model.PaymentWebhookProcessed {
			alreadyProcessed = true
			if input.Audit != nil {
				return tx.Create(input.Audit).Error
			}
			return nil
		}
		if event.Status == model.PaymentWebhookRejected {
			return ErrPaymentTransactionNotPayable
		}
		now := input.PaidAt
		if transaction.Status == model.PaymentTransactionPaid && transaction.ProviderTradeNo == input.ProviderTradeNo {
			if err := tx.Model(&model.PaymentWebhookEvent{}).Where("id = ?", event.ID).
				Select("status", "processed_at", "failure_code", "failure_reason", "updated_at").
				Updates(&model.PaymentWebhookEvent{
					Status: model.PaymentWebhookProcessed, ProcessedAt: &now, FailureCode: "", FailureReason: "", UpdatedAt: now,
				}).Error; err != nil {
				return err
			}
			if input.Audit != nil {
				return tx.Create(input.Audit).Error
			}
			return nil
		}
		if transaction.Status != model.PaymentTransactionCreated && transaction.Status != model.PaymentTransactionPending && transaction.Status != model.PaymentTransactionReviewRequired {
			return ErrPaymentTransactionNotPayable
		}
		result := tx.Model(&model.PaymentTransaction{}).Where("id = ? AND status IN ?", transaction.ID, []model.PaymentTransactionStatus{
			model.PaymentTransactionCreated, model.PaymentTransactionPending, model.PaymentTransactionReviewRequired,
		}).Select("status", "provider_trade_no", "paid_at", "failure_code", "failure_reason", "updated_at").
			Updates(&model.PaymentTransaction{
				Status: model.PaymentTransactionPaid, ProviderTradeNo: input.ProviderTradeNo, PaidAt: &now,
				FailureCode: "", FailureReason: "", UpdatedAt: now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrPaymentTransactionNotPayable
		}
		switch transaction.OrderType {
		case model.PaymentOrderMembership:
			if err := activateMembershipOrderTx(tx, transaction.OrderID, model.SystemActorID, string(transaction.Provider), input.ProviderTradeNo, "支付渠道确认到账自动开通", input.Activation, now); err != nil {
				return err
			}
		case model.PaymentOrderCreditTopup:
			if err := fulfillCreditTopupOrderTx(tx, transaction.OrderID, string(transaction.Provider), input.ProviderTradeNo, now); err != nil {
				return err
			}
		default:
			return errors.New("payment transaction has unsupported order type")
		}
		if err := tx.Model(&model.PaymentCheckoutSession{}).Where("order_id = ?", transaction.OrderID).
			Select("status", "updated_at").Updates(&model.PaymentCheckoutSession{
			Status: model.PaymentCheckoutConsumed, UpdatedAt: now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.PaymentWebhookEvent{}).Where("id = ?", event.ID).
			Select("status", "processed_at", "failure_code", "failure_reason", "updated_at").
			Updates(&model.PaymentWebhookEvent{
				Status: model.PaymentWebhookProcessed, ProcessedAt: &now, FailureCode: "", FailureReason: "", UpdatedAt: now,
			}).Error; err != nil {
			return err
		}
		if input.Audit != nil {
			return tx.Create(input.Audit).Error
		}
		return nil
	})
	return alreadyProcessed, err
}

func (r *Repository) PaymentTransaction(id string) (*model.PaymentTransaction, error) {
	var transaction model.PaymentTransaction
	return &transaction, r.db.First(&transaction, "id = ?", id).Error
}

func (r *Repository) MarkPaymentTransactionReviewWithAudit(id string, failureCode string, failureReason string, now time.Time, audit *model.AdminAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var transaction model.PaymentTransaction
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&transaction, "id = ?", id).Error; err != nil {
			return err
		}
		if transaction.Status != model.PaymentTransactionPaid && transaction.Status != model.PaymentTransactionClosed && transaction.Status != model.PaymentTransactionRefunded {
			if err := tx.Model(&model.PaymentTransaction{}).Where("id = ?", id).
				Select("status", "failure_code", "failure_reason", "updated_at").
				Updates(&model.PaymentTransaction{
					Status: model.PaymentTransactionReviewRequired, FailureCode: failureCode,
					FailureReason: failureReason, UpdatedAt: now,
				}).Error; err != nil {
				return err
			}
		}
		if audit != nil {
			return tx.Create(audit).Error
		}
		return nil
	})
}

func (r *Repository) ClosePaymentTransactionAfterProviderConfirmation(id string, now time.Time, audit *model.AdminAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var snapshot model.PaymentTransaction
		if err := tx.First(&snapshot, "id = ?", id).Error; err != nil {
			return err
		}
		if err := lockPayableOrderTx(tx, snapshot.OrderType, snapshot.OrderID, snapshot.UserID); err != nil {
			return err
		}
		var checkout model.PaymentCheckoutSession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&checkout,
			"order_type = ? AND order_id = ?", snapshot.OrderType, snapshot.OrderID).Error; err != nil {
			return err
		}
		var transaction model.PaymentTransaction
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&transaction, "id = ?", id).Error; err != nil {
			return err
		}
		verified, err := verifiedPaymentFactExistsTx(tx, transaction.OrderType, transaction.OrderID)
		if err != nil {
			return err
		}
		if verified {
			return ErrPaymentVerifiedFactExists
		}
		if transaction.Status != model.PaymentTransactionCreated && transaction.Status != model.PaymentTransactionPending && transaction.Status != model.PaymentTransactionReviewRequired {
			return ErrPaymentTransactionNotPayable
		}
		if err := tx.Model(&model.PaymentTransaction{}).Where("id = ?", id).
			Select("status", "closed_at", "failure_code", "failure_reason", "updated_at").
			Updates(&model.PaymentTransaction{
				Status: model.PaymentTransactionClosed, ClosedAt: &now, FailureCode: "provider_confirmed_closed",
				FailureReason: "", UpdatedAt: now,
			}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.PaymentCheckoutSession{}).Where("id = ?", checkout.ID).
			Select("status", "updated_at").Updates(&model.PaymentCheckoutSession{
			Status: model.PaymentCheckoutExpired, UpdatedAt: now,
		}).Error; err != nil {
			return err
		}
		switch transaction.OrderType {
		case model.PaymentOrderMembership:
			if err := tx.Model(&model.MembershipOrder{}).Where("id = ? AND status = ?", transaction.OrderID, model.MembershipOrderPending).
				Select("status", "resolved_by", "resolution_note", "updated_at").
				Updates(&model.MembershipOrder{
					Status: model.MembershipOrderCancelled, ResolvedBy: model.SystemActorID,
					ResolutionNote: "支付渠道确认未到账并关单", UpdatedAt: now,
				}).Error; err != nil {
				return err
			}
		case model.PaymentOrderCreditTopup:
			if err := tx.Model(&model.CreditTopupOrder{}).Where("id = ? AND status = ?", transaction.OrderID, model.CreditTopupOrderPending).
				Select("status", "resolution_note", "updated_at").
				Updates(&model.CreditTopupOrder{
					Status: model.CreditTopupOrderCancelled, ResolutionNote: "支付渠道确认未到账并关单", UpdatedAt: now,
				}).Error; err != nil {
				return err
			}
		}
		if audit != nil {
			return tx.Create(audit).Error
		}
		return nil
	})
}

func (r *Repository) ExpirePaymentCheckoutSession(id string, now time.Time) (bool, error) {
	expired := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var snapshot model.PaymentCheckoutSession
		if err := tx.First(&snapshot, "id = ?", id).Error; err != nil {
			return err
		}
		if snapshot.Status != model.PaymentCheckoutActive || snapshot.ExpiresAt.After(now) {
			return nil
		}
		if _, err := lockPaymentOrderTx(tx, snapshot.OrderType, snapshot.OrderID, snapshot.UserID); err != nil {
			return err
		}
		var checkout model.PaymentCheckoutSession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&checkout,
			"id = ? AND order_type = ? AND order_id = ? AND user_id = ?", snapshot.ID, snapshot.OrderType, snapshot.OrderID, snapshot.UserID).Error; err != nil {
			return err
		}
		if checkout.Status != model.PaymentCheckoutActive || checkout.ExpiresAt.After(now) {
			return nil
		}
		var payableCount int64
		if err := tx.Model(&model.PaymentTransaction{}).
			Where("order_type = ? AND order_id = ? AND status IN ?", checkout.OrderType, checkout.OrderID, []model.PaymentTransactionStatus{
				model.PaymentTransactionCreated, model.PaymentTransactionPending, model.PaymentTransactionReviewRequired,
			}).Count(&payableCount).Error; err != nil {
			return err
		}
		if payableCount > 0 {
			return ErrPaymentReconciliationRequired
		}
		verified, err := verifiedPaymentFactExistsTx(tx, checkout.OrderType, checkout.OrderID)
		if err != nil {
			return err
		}
		if verified {
			return ErrPaymentVerifiedFactExists
		}
		result := tx.Model(&model.PaymentCheckoutSession{}).
			Where("id = ? AND status = ? AND expires_at <= ?", checkout.ID, model.PaymentCheckoutActive, now).
			Updates(model.PaymentCheckoutSession{Status: model.PaymentCheckoutExpired, UpdatedAt: now})
		if result.Error != nil {
			return result.Error
		}
		expired = result.RowsAffected == 1
		return nil
	})
	return expired, err
}

func (r *Repository) AdminPaymentTransactions(filter PaymentTransactionFilter) ([]model.PaymentTransaction, int64, error) {
	query := r.db.Model(&model.PaymentTransaction{})
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where(
			"merchant_order_no LIKE ? OR provider_trade_no LIKE ? OR order_id LIKE ? OR user_id LIKE ?",
			like, like, like, like,
		)
	}
	if filter.Provider != "" {
		query = query.Where("provider = ?", filter.Provider)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var transactions []model.PaymentTransaction
	err := query.Order("created_at desc").Limit(filter.Limit).Offset(filter.Offset).Find(&transactions).Error
	return transactions, total, err
}

func (r *Repository) AdminPaymentWebhookEvents(filter PaymentWebhookFilter) ([]model.PaymentWebhookEvent, int64, error) {
	query := r.db.Model(&model.PaymentWebhookEvent{})
	if filter.Provider != "" {
		query = query.Where("provider = ?", filter.Provider)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var events []model.PaymentWebhookEvent
	err := query.Order("received_at desc").Limit(filter.Limit).Offset(filter.Offset).Find(&events).Error
	return events, total, err
}
