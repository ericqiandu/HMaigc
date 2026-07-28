package repository

import (
	"errors"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrMembershipOrderNotPending = errors.New("membership order is not pending")
	ErrTeamSeatLimitReached      = errors.New("team seat limit reached")
)

const automaticMembershipOrderCloseNote = "订单超过 24 小时未支付，系统自动关闭"

func (r *Repository) CreateMembershipPlanIfMissing(plan *model.MembershipPlan) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "code"}},
		DoNothing: true,
	}).Create(plan).Error
}

func (r *Repository) MembershipPlans(enabledOnly bool) ([]model.MembershipPlan, error) {
	var plans []model.MembershipPlan
	query := r.db.Order("audience asc, sort_order asc, price_cents asc")
	if enabledOnly {
		query = query.Where("enabled = ?", true)
	}
	err := query.Find(&plans).Error
	return plans, err
}

func (r *Repository) MembershipPlan(id string) (*model.MembershipPlan, error) {
	var plan model.MembershipPlan
	return &plan, r.db.First(&plan, "id = ?", id).Error
}

func (r *Repository) SaveMembershipPlan(plan *model.MembershipPlan) error {
	return r.db.Save(plan).Error
}

func (r *Repository) MembershipOrders(userID string, limit int, offset int) ([]model.MembershipOrder, int64, error) {
	var items []model.MembershipOrder
	var total int64
	query := r.db.Model(&model.MembershipOrder{})
	if userID != "" {
		query = query.Where("user_id = ?", userID)
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("created_at desc").Limit(limit).Offset(offset).Find(&items).Error
	return items, total, err
}

func (r *Repository) MembershipOrderForUser(userID string, id string) (*model.MembershipOrder, error) {
	var order model.MembershipOrder
	return &order, r.db.First(&order, "id = ? AND user_id = ?", id, userID).Error
}

func (r *Repository) MembershipOrder(id string) (*model.MembershipOrder, error) {
	var order model.MembershipOrder
	return &order, r.db.First(&order, "id = ?", id).Error
}

func (r *Repository) CloseMembershipOrder(orderID string, userID string, actorID string, note string, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var order model.MembershipOrder
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", orderID)
		if userID != "" {
			query = query.Where("user_id = ?", userID)
		}
		if err := query.First(&order).Error; err != nil {
			return err
		}
		if order.Status != model.MembershipOrderPending {
			return ErrMembershipOrderNotPending
		}
		result := tx.Model(&model.MembershipOrder{}).
			Where("id = ? AND status = ?", order.ID, model.MembershipOrderPending).
			Updates(map[string]interface{}{
				"status":          model.MembershipOrderCancelled,
				"resolved_by":     actorID,
				"resolution_note": note,
				"updated_at":      now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrMembershipOrderNotPending
		}
		if err := tx.Model(&model.PaymentCheckoutSession{}).
			Where("order_id = ? AND status = ?", order.ID, model.PaymentCheckoutActive).
			Updates(map[string]interface{}{"status": model.PaymentCheckoutExpired, "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Model(&model.PaymentTransaction{}).
			Where("order_id = ? AND status IN ?", order.ID, []model.PaymentTransactionStatus{model.PaymentTransactionCreated, model.PaymentTransactionPending}).
			Updates(map[string]interface{}{"status": model.PaymentTransactionClosed, "closed_at": now, "updated_at": now}).Error
	})
}

func (r *Repository) ReconcileMembershipLifecycle(now time.Time, pendingCreatedBefore time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.MembershipSubscription{}).
			Where("status = ? AND ends_at IS NOT NULL AND ends_at <= ?", model.MembershipSubscriptionActive, now).
			Updates(map[string]interface{}{"status": model.MembershipSubscriptionExpired, "updated_at": now}).Error; err != nil {
			return err
		}

		var orderIDs []string
		if err := tx.Model(&model.MembershipOrder{}).
			Where("status = ? AND created_at <= ?", model.MembershipOrderPending, pendingCreatedBefore).
			Pluck("id", &orderIDs).Error; err != nil {
			return err
		}
		if len(orderIDs) > 0 {
			if err := tx.Model(&model.MembershipOrder{}).
				Where("id IN ? AND status = ?", orderIDs, model.MembershipOrderPending).
				Updates(map[string]interface{}{
					"status":          model.MembershipOrderCancelled,
					"resolution_note": automaticMembershipOrderCloseNote,
					"updated_at":      now,
				}).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.PaymentCheckoutSession{}).
				Where("order_id IN ? AND status = ?", orderIDs, model.PaymentCheckoutActive).
				Updates(map[string]interface{}{"status": model.PaymentCheckoutExpired, "updated_at": now}).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.PaymentTransaction{}).
				Where("order_id IN ? AND status IN ?", orderIDs, []model.PaymentTransactionStatus{model.PaymentTransactionCreated, model.PaymentTransactionPending}).
				Updates(map[string]interface{}{"status": model.PaymentTransactionClosed, "closed_at": now, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&model.PaymentCheckoutSession{}).
			Where("status = ? AND expires_at <= ?", model.PaymentCheckoutActive, now).
			Updates(map[string]interface{}{"status": model.PaymentCheckoutExpired, "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Model(&model.PaymentTransaction{}).
			Where("status IN ? AND expires_at IS NOT NULL AND expires_at <= ?", []model.PaymentTransactionStatus{model.PaymentTransactionCreated, model.PaymentTransactionPending}, now).
			Updates(map[string]interface{}{"status": model.PaymentTransactionClosed, "closed_at": now, "updated_at": now}).Error
	})
}

func (r *Repository) ActiveMembershipSubscriptions(userID string, now time.Time) ([]model.MembershipSubscription, error) {
	var subscriptions []model.MembershipSubscription
	err := r.db.Raw(`
		SELECT DISTINCT subscriptions.*
		FROM membership_subscriptions subscriptions
		LEFT JOIN team_members members
		  ON members.team_id = subscriptions.team_id
		 AND members.user_id = ?
		 AND members.status = ?
		WHERE subscriptions.status = ?
		  AND subscriptions.starts_at <= ?
		  AND (subscriptions.ends_at IS NULL OR subscriptions.ends_at > ?)
		  AND (subscriptions.user_id = ? OR members.id IS NOT NULL)
		ORDER BY subscriptions.created_at DESC
	`, userID, model.TeamMemberStatusActive, model.MembershipSubscriptionActive, now, now, userID).Scan(&subscriptions).Error
	return subscriptions, err
}

func (r *Repository) LatestMembershipSubscriptionEnd(userID string, teamID string, now time.Time) (*time.Time, error) {
	var subscription model.MembershipSubscription
	query := r.db.Where("user_id = ? AND status = ? AND ends_at > ?", userID, model.MembershipSubscriptionActive, now)
	if teamID == "" {
		query = query.Where("team_id = ''")
	} else {
		query = query.Where("team_id = ?", teamID)
	}
	err := query.Order("ends_at desc").First(&subscription).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return subscription.EndsAt, nil
}

// MembershipSubscriptionsAwaitingCreditGrant 返回已到生效时间但尚未发放周期积分的订阅。
// 是否应发放及发放数量由 service 根据不可变套餐快照决定，repository 只提供事实数据。
func (r *Repository) MembershipSubscriptionsAwaitingCreditGrant(now time.Time) ([]model.MembershipSubscription, error) {
	var subscriptions []model.MembershipSubscription
	err := r.db.Raw(`
		SELECT subscriptions.*
		FROM membership_subscriptions subscriptions
		LEFT JOIN credit_ledger_entries ledger
		  ON ledger.reference_key = ('membership-order:' || subscriptions.order_id)
		 AND ledger.type = ?
		WHERE subscriptions.status = ?
		  AND subscriptions.order_id <> ''
		  AND subscriptions.starts_at <= ?
		  AND ledger.id IS NULL
		ORDER BY subscriptions.starts_at ASC
	`, model.CreditLedgerMembership, model.MembershipSubscriptionActive, now).Scan(&subscriptions).Error
	return subscriptions, err
}

func (r *Repository) GrantMembershipSubscriptionCredits(subscription *model.MembershipSubscription, ledger *model.CreditLedgerEntry, now time.Time) error {
	if subscription == nil || ledger == nil || ledger.AmountMicrocredits <= 0 {
		return errors.New("订阅积分发放参数无效")
	}
	if ledger.ReferenceKey == nil || *ledger.ReferenceKey == "" {
		return errors.New("订阅积分发放引用标识不能为空")
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		var current model.MembershipSubscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", subscription.ID).Error; err != nil {
			return err
		}
		if current.Status != model.MembershipSubscriptionActive || current.StartsAt.After(now) {
			return errors.New("订阅尚未到积分发放时间")
		}
		var existing int64
		if err := tx.Model(&model.CreditLedgerEntry{}).Where("reference_key = ? AND type = ?", *ledger.ReferenceKey, model.CreditLedgerMembership).Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return nil
		}
		account := model.CreditAccount{UserID: current.UserID}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&account).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.CreditAccount{}).Where("user_id = ?", current.UserID).Updates(map[string]interface{}{
			"available_microcredits": gorm.Expr("available_microcredits + ?", ledger.AmountMicrocredits),
			"version":                gorm.Expr("version + 1"),
			"updated_at":             now,
		}).Error; err != nil {
			return err
		}
		var refreshed model.CreditAccount
		if err := tx.First(&refreshed, "user_id = ?", current.UserID).Error; err != nil {
			return err
		}
		ledger.AvailableAfterMicrocredits = refreshed.AvailableMicrocredits
		ledger.ReservedAfterMicrocredits = refreshed.ReservedMicrocredits
		return tx.Create(ledger).Error
	})
}

func (r *Repository) TeamForOwner(ownerID string, teamID string) (*model.Team, error) {
	var team model.Team
	return &team, r.db.First(&team, "id = ? AND owner_user_id = ?", teamID, ownerID).Error
}

func (r *Repository) TeamsForUser(userID string) ([]model.Team, error) {
	var teams []model.Team
	err := r.db.Raw(`
		SELECT teams.* FROM teams
		JOIN team_members ON team_members.team_id = teams.id
		WHERE team_members.user_id = ? AND team_members.status = ? AND teams.status = ?
		ORDER BY teams.created_at DESC
	`, userID, model.TeamMemberStatusActive, model.TeamStatusActive).Scan(&teams).Error
	return teams, err
}

func (r *Repository) TeamMembers(teamID string) ([]model.TeamMember, error) {
	var members []model.TeamMember
	err := r.db.Where("team_id = ? AND status = ?", teamID, model.TeamMemberStatusActive).Order("created_at asc").Find(&members).Error
	return members, err
}

func (r *Repository) CreateTeam(team *model.Team, owner *model.TeamMember) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(team).Error; err != nil {
			return err
		}
		return tx.Create(owner).Error
	})
}

func (r *Repository) AddTeamMember(member *model.TeamMember, seatLimit int) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&model.TeamMember{}).Where("team_id = ? AND status = ?", member.TeamID, model.TeamMemberStatusActive).Count(&count).Error; err != nil {
			return err
		}
		if count >= int64(seatLimit) {
			return ErrTeamSeatLimitReached
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "team_id"}, {Name: "user_id"}},
			DoUpdates: clause.Assignments(map[string]interface{}{"role": member.Role, "status": member.Status, "updated_at": member.UpdatedAt}),
		}).Create(member).Error
	})
}

func (r *Repository) ActivateMembershipOrder(orderID string, actorID string, provider string, providerTradeNo string, note string, subscription *model.MembershipSubscription, ledger *model.CreditLedgerEntry) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		return activateMembershipOrderTx(tx, orderID, actorID, provider, providerTradeNo, note, subscription, ledger, time.Now())
	})
}

func activateMembershipOrderTx(tx *gorm.DB, orderID string, actorID string, provider string, providerTradeNo string, note string, subscription *model.MembershipSubscription, ledger *model.CreditLedgerEntry, now time.Time) error {
	var order model.MembershipOrder
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", orderID).Error; err != nil {
		return err
	}
	if order.Status != model.MembershipOrderPending {
		return ErrMembershipOrderNotPending
	}
	updated := tx.Model(&model.MembershipOrder{}).Where("id = ? AND status = ?", orderID, model.MembershipOrderPending).Updates(map[string]interface{}{
		"status": model.MembershipOrderPaid, "payment_provider": provider, "provider_trade_no": providerTradeNo, "resolved_by": actorID,
		"resolution_note": note, "paid_at": now, "updated_at": now,
	})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return ErrMembershipOrderNotPending
	}
	if err := tx.Create(subscription).Error; err != nil {
		return err
	}
	if ledger == nil || ledger.AmountMicrocredits <= 0 {
		return nil
	}
	account := model.CreditAccount{UserID: order.UserID}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&account).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.CreditAccount{}).Where("user_id = ?", order.UserID).Updates(map[string]interface{}{
		"available_microcredits": gorm.Expr("available_microcredits + ?", ledger.AmountMicrocredits),
		"version":                gorm.Expr("version + 1"), "updated_at": now,
	}).Error; err != nil {
		return err
	}
	var refreshed model.CreditAccount
	if err := tx.First(&refreshed, "user_id = ?", order.UserID).Error; err != nil {
		return err
	}
	ledger.AvailableAfterMicrocredits = refreshed.AvailableMicrocredits
	ledger.ReservedAfterMicrocredits = refreshed.ReservedMicrocredits
	return tx.Create(ledger).Error
}
