package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrCreditTopupOrderNotPending = errors.New("credit topup order is not pending")
	ErrCreditProductOutOfStock    = errors.New("credit product is out of stock")
	ErrCreditProductWeeklyLimit   = errors.New("credit product weekly purchase limit reached")
	ErrCreditProductChanged       = errors.New("credit product changed while creating order")
	ErrCreditProductExpired       = errors.New("credit product sale has ended")
)

func (r *Repository) CreditTopupProducts(includeDisabled bool) ([]model.CreditTopupProduct, error) {
	var products []model.CreditTopupProduct
	query := r.db.Order("sort_order asc, created_at asc")
	if !includeDisabled {
		query = query.Where("enabled = ?", true)
	}
	return products, query.Find(&products).Error
}

func (r *Repository) CreditTopupProduct(id string) (*model.CreditTopupProduct, error) {
	var product model.CreditTopupProduct
	return &product, r.db.First(&product, "id = ?", id).Error
}

func (r *Repository) CreateCreditTopupProductIfMissing(product *model.CreditTopupProduct) error {
	return r.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "code"}}, DoNothing: true}).Create(product).Error
}

func (r *Repository) SaveCreditTopupProduct(product *model.CreditTopupProduct, audit *model.AdminAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var existing model.CreditTopupProduct
		err := tx.First(&existing, "id = ?", product.ID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Create(product).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			product.SoldCount = existing.SoldCount
			product.CreatedAt = existing.CreatedAt
			if err := tx.Save(product).Error; err != nil {
				return err
			}
		}
		return tx.Create(audit).Error
	})
}

func (r *Repository) CreateCreditTopupOrder(order *model.CreditTopupOrder) (*model.CreditTopupOrder, bool, error) {
	created := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var replay model.CreditTopupOrder
		lookup := tx.First(&replay, "user_id = ? AND idempotency_key = ?", order.UserID, order.IdempotencyKey)
		if lookup.Error == nil {
			*order = replay
			return nil
		}
		if !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
			return lookup.Error
		}
		var product model.CreditTopupProduct
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&product, "id = ? AND enabled = ?", order.ProductID, true).Error; err != nil {
			return err
		}
		if product.SaleEndsAt != nil && !product.SaleEndsAt.After(time.Now()) {
			return ErrCreditProductExpired
		}
		currentSnapshot, err := json.Marshal(&product)
		if err != nil {
			return err
		}
		currentDigest := sha256.Sum256(currentSnapshot)
		if order.RequestHash != hex.EncodeToString(currentDigest[:]) {
			return ErrCreditProductChanged
		}
		if product.StockLimit >= 0 {
			var pending int64
			if err := tx.Model(&model.CreditTopupOrder{}).Where("product_id = ? AND status = ?", product.ID, model.CreditTopupOrderPending).Count(&pending).Error; err != nil {
				return err
			}
			if product.SoldCount+pending >= product.StockLimit {
				return ErrCreditProductOutOfStock
			}
		}
		if product.WeeklyPurchaseLimit > 0 {
			weekStart := time.Now().UTC().AddDate(0, 0, -7)
			var count int64
			if err := tx.Model(&model.CreditTopupOrder{}).
				Where("user_id = ? AND product_id = ? AND status IN ? AND created_at >= ?", order.UserID, product.ID, []model.CreditTopupOrderStatus{model.CreditTopupOrderPending, model.CreditTopupOrderPaid}, weekStart).
				Count(&count).Error; err != nil {
				return err
			}
			if count >= int64(product.WeeklyPurchaseLimit) {
				return ErrCreditProductWeeklyLimit
			}
		}
		result := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "idempotency_key"}},
			DoNothing: true,
		}).Create(order)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 1 {
			created = true
			return nil
		}
		var existing model.CreditTopupOrder
		if err := tx.First(&existing, "user_id = ? AND idempotency_key = ?", order.UserID, order.IdempotencyKey).Error; err != nil {
			return err
		}
		*order = existing
		return nil
	})
	return order, created, err
}

func (r *Repository) CreditTopupOrderForUser(userID string, id string) (*model.CreditTopupOrder, error) {
	var order model.CreditTopupOrder
	return &order, r.db.First(&order, "id = ? AND user_id = ?", id, userID).Error
}

func (r *Repository) CreditTopupOrder(id string) (*model.CreditTopupOrder, error) {
	var order model.CreditTopupOrder
	return &order, r.db.First(&order, "id = ?", id).Error
}

func (r *Repository) CreditTopupOrders(userID string, limit int, offset int) ([]model.CreditTopupOrder, int64, error) {
	query := r.db.Model(&model.CreditTopupOrder{})
	if userID != "" {
		query = query.Where("user_id = ?", userID)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var orders []model.CreditTopupOrder
	err := query.Order("created_at desc, id desc").Limit(limit).Offset(offset).Find(&orders).Error
	return orders, total, err
}

func (r *Repository) CancelCreditTopupOrder(userID string, id string, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.CreditTopupOrder{}).
			Where("id = ? AND user_id = ? AND status = ?", id, userID, model.CreditTopupOrderPending).
			Updates(map[string]interface{}{"status": model.CreditTopupOrderCancelled, "resolution_note": "用户主动取消订单", "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrCreditTopupOrderNotPending
		}
		return tx.Model(&model.PaymentCheckoutSession{}).
			Where("order_id = ? AND order_type = ? AND status = ?", id, model.PaymentOrderCreditTopup, model.PaymentCheckoutActive).
			Updates(map[string]interface{}{"status": model.PaymentCheckoutExpired, "updated_at": now}).Error
	})
}

func fulfillCreditTopupOrderTx(tx *gorm.DB, orderID string, provider string, providerTradeNo string, now time.Time) error {
	var order model.CreditTopupOrder
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", orderID).Error; err != nil {
		return err
	}
	if order.Status != model.CreditTopupOrderPending {
		return ErrCreditTopupOrderNotPending
	}
	result := tx.Model(&model.CreditTopupOrder{}).Where("id = ? AND status = ?", order.ID, model.CreditTopupOrderPending).Updates(map[string]interface{}{
		"status": model.CreditTopupOrderPaid, "payment_provider": provider, "provider_trade_no": providerTradeNo,
		"resolution_note": "支付渠道回调自动到账", "paid_at": now, "updated_at": now,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrCreditTopupOrderNotPending
	}
	account := model.CreditAccount{UserID: order.UserID}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&account).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.CreditAccount{}).Where("user_id = ?", order.UserID).Updates(map[string]interface{}{
		"available_microcredits": gorm.Expr("available_microcredits + ?", order.TotalMicrocredits),
		"version":                gorm.Expr("version + 1"), "updated_at": now,
	}).Error; err != nil {
		return err
	}
	if err := tx.First(&account, "user_id = ?", order.UserID).Error; err != nil {
		return err
	}
	referenceKey := "credit-topup:" + order.ID
	ledger := model.CreditLedgerEntry{
		ID: newRepositoryID(), UserID: order.UserID, Type: model.CreditLedgerTopup,
		AmountMicrocredits: order.TotalMicrocredits, AvailableDeltaMicrocredits: order.TotalMicrocredits,
		AvailableAfterMicrocredits: account.AvailableMicrocredits, ReservedAfterMicrocredits: account.ReservedMicrocredits,
		Scene: "credit_store", Note: "积分超市订单 " + order.OrderNumber, ReferenceKey: &referenceKey, CreatedAt: now,
	}
	if err := tx.Create(&ledger).Error; err != nil {
		return err
	}
	return tx.Model(&model.CreditTopupProduct{}).Where("id = ?", order.ProductID).UpdateColumn("sold_count", gorm.Expr("sold_count + 1")).Error
}
