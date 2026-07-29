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
	ErrPaymentWebhookConflict       = errors.New("payment webhook conflicts with an existing event")
	ErrPaymentTransactionNotPayable = errors.New("payment transaction is not payable")
)

type PaymentFulfillment struct {
	Event           *model.PaymentWebhookEvent
	TransactionID   string
	ProviderTradeNo string
	PaidAt          time.Time
	Activation      MembershipActivation
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

func (r *Repository) SavePaymentCheckoutSession(session *model.PaymentCheckoutSession) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "order_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"token_hash", "status", "expires_at", "updated_at"}),
	}).Create(session).Error
}

func (r *Repository) PaymentCheckoutSessionByTokenHash(tokenHash string) (*model.PaymentCheckoutSession, error) {
	var session model.PaymentCheckoutSession
	return &session, r.db.First(&session, "token_hash = ?", tokenHash).Error
}

func (r *Repository) CreatePaymentTransaction(transaction *model.PaymentTransaction) error {
	return r.db.Create(transaction).Error
}

func (r *Repository) UpdatePaymentTransactionCreation(id string, status model.PaymentTransactionStatus, codeURL string, failureReason string, now time.Time) error {
	result := r.db.Model(&model.PaymentTransaction{}).
		Where("id = ? AND status = ?", id, model.PaymentTransactionCreated).
		Updates(map[string]interface{}{
			"status": status, "code_url": codeURL, "failure_reason": failureReason, "updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("payment transaction creation state changed unexpectedly")
	}
	return nil
}

func (r *Repository) LatestPaymentTransaction(orderID string, provider model.PaymentProvider) (*model.PaymentTransaction, error) {
	var transaction model.PaymentTransaction
	err := r.db.Where("order_id = ? AND provider = ?", orderID, provider).
		Order("created_at desc").
		First(&transaction).Error
	return &transaction, err
}

func (r *Repository) PaymentTransactionByMerchantOrderNo(provider model.PaymentProvider, merchantOrderNo string) (*model.PaymentTransaction, error) {
	var transaction model.PaymentTransaction
	return &transaction, r.db.First(&transaction, "provider = ? AND merchant_order_no = ?", provider, merchantOrderNo).Error
}

func (r *Repository) FulfillPaymentTransaction(input PaymentFulfillment) (bool, error) {
	alreadyProcessed := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var existing model.PaymentWebhookEvent
		eventLookup := tx.Where("provider = ? AND provider_event_id = ?", input.Event.Provider, input.Event.ProviderEventID).First(&existing)
		if eventLookup.Error == nil {
			if existing.PayloadDigest != input.Event.PayloadDigest {
				return ErrPaymentWebhookConflict
			}
			alreadyProcessed = existing.Status == model.PaymentWebhookProcessed
			if alreadyProcessed {
				return nil
			}
			return ErrPaymentWebhookConflict
		}
		if !errors.Is(eventLookup.Error, gorm.ErrRecordNotFound) {
			return eventLookup.Error
		}
		var transaction model.PaymentTransaction
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&transaction, "id = ?", input.TransactionID).Error; err != nil {
			return err
		}
		now := input.PaidAt
		if transaction.Status == model.PaymentTransactionPaid && transaction.ProviderTradeNo == input.ProviderTradeNo {
			input.Event.TransactionID = transaction.ID
			input.Event.Status = model.PaymentWebhookProcessed
			input.Event.ProcessedAt = &now
			return tx.Create(input.Event).Error
		}
		if transaction.Status != model.PaymentTransactionPending {
			return ErrPaymentTransactionNotPayable
		}
		input.Event.TransactionID = transaction.ID
		input.Event.Status = model.PaymentWebhookReceived
		if err := tx.Create(input.Event).Error; err != nil {
			return err
		}
		result := tx.Model(&model.PaymentTransaction{}).Where("id = ? AND status = ?", transaction.ID, model.PaymentTransactionPending).Updates(map[string]interface{}{
			"status": model.PaymentTransactionPaid, "provider_trade_no": input.ProviderTradeNo, "paid_at": now, "updated_at": now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrPaymentTransactionNotPayable
		}
		if err := activateMembershipOrderTx(tx, transaction.OrderID, "", string(transaction.Provider), input.ProviderTradeNo, "支付渠道回调自动开通", input.Activation, now); err != nil {
			return err
		}
		if err := tx.Model(&model.PaymentCheckoutSession{}).Where("order_id = ?", transaction.OrderID).Updates(map[string]interface{}{
			"status": model.PaymentCheckoutConsumed, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&model.PaymentWebhookEvent{}).Where("id = ?", input.Event.ID).Updates(map[string]interface{}{
			"status": model.PaymentWebhookProcessed, "processed_at": now, "updated_at": now,
		}).Error
	})
	return alreadyProcessed, err
}

func (r *Repository) ExpirePaymentCheckoutSession(id string, now time.Time) error {
	return r.db.Model(&model.PaymentCheckoutSession{}).
		Where("id = ? AND status = ?", id, model.PaymentCheckoutActive).
		Updates(model.PaymentCheckoutSession{Status: model.PaymentCheckoutExpired, UpdatedAt: now}).Error
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
