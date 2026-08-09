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

type ReferralFulfillment struct {
	Reward        *model.ReferralReward
	InviterLedger *model.CreditLedgerEntry
	InviteeLedger *model.CreditLedgerEntry
}

type MembershipActivation struct {
	Subscription     *model.MembershipSubscription
	MembershipLedger *model.CreditLedgerEntry
	Referral         *ReferralFulfillment
}

const automaticMembershipOrderCloseNote = "订单超过 24 小时未支付，系统自动关闭"

// ApplyMembershipPlanCatalogRevision replaces the sellable catalog exactly once.
// Existing IDs are retained so historical orders and subscription snapshots keep
// their original references. Plans outside the current catalog are archived by
// disabling them instead of deleting financial history.
func (r *Repository) ApplyMembershipPlanCatalogRevision(settingKey string, revision string, plans []model.MembershipPlan) error {
	if settingKey == "" || revision == "" || len(plans) == 0 {
		return errors.New("membership plan catalog revision is incomplete")
	}
	seenCodes := make(map[string]struct{}, len(plans))
	for _, plan := range plans {
		if plan.Code == "" {
			return errors.New("membership plan catalog contains an empty code")
		}
		if _, exists := seenCodes[plan.Code]; exists {
			return errors.New("membership plan catalog contains a duplicate code")
		}
		seenCodes[plan.Code] = struct{}{}
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		marker := model.SystemSetting{Key: settingKey}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&marker).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&marker, "key = ?", settingKey).Error; err != nil {
			return err
		}
		if marker.ValueJSON == revision {
			return nil
		}

		var stored []model.MembershipPlan
		if err := tx.Find(&stored).Error; err != nil {
			return err
		}
		storedByCode := make(map[string]model.MembershipPlan, len(stored))
		for _, plan := range stored {
			storedByCode[plan.Code] = plan
		}

		codes := make([]string, 0, len(plans))
		for index := range plans {
			plan := plans[index]
			codes = append(codes, plan.Code)
			if current, exists := storedByCode[plan.Code]; exists {
				plan.ID = current.ID
				plan.CreatedAt = current.CreatedAt
				if err := tx.Save(&plan).Error; err != nil {
					return err
				}
				continue
			}
			if err := tx.Create(&plan).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&model.MembershipPlan{}).
			Where("code NOT IN ?", codes).
			Updates(map[string]interface{}{"enabled": false, "updated_at": time.Now()}).Error; err != nil {
			return err
		}

		marker.ValueJSON = revision
		marker.UpdatedBy = ""
		return tx.Save(&marker).Error
	})
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

func (r *Repository) SaveMembershipPlan(plan *model.MembershipPlan, audit *model.AdminAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(plan).Error; err != nil {
			return err
		}
		if audit != nil {
			return tx.Create(audit).Error
		}
		return nil
	})
}

func (r *Repository) SaveMembershipStorefrontSetting(setting *model.SystemSetting, audit *model.AdminAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(setting).Error; err != nil {
			return err
		}
		if audit != nil {
			return tx.Create(audit).Error
		}
		return nil
	})
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

func (r *Repository) CloseMembershipOrder(orderID string, userID string, actorID string, note string, now time.Time, audit *model.AdminAuditEvent) error {
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
		if err := tx.Model(&model.PaymentTransaction{}).
			Where("order_id = ? AND status IN ?", order.ID, []model.PaymentTransactionStatus{model.PaymentTransactionCreated, model.PaymentTransactionPending}).
			Updates(map[string]interface{}{"status": model.PaymentTransactionClosed, "closed_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		if audit != nil {
			return tx.Create(audit).Error
		}
		return nil
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
		LEFT JOIN credit_ledger_entries user_ledger
		  ON user_ledger.reference_key = ('membership-order:' || subscriptions.order_id)
		 AND user_ledger.type = ?
		LEFT JOIN team_credit_ledger_entries team_ledger
		  ON team_ledger.reference_key = ('membership-order:' || subscriptions.order_id)
		 AND team_ledger.type = ?
		WHERE subscriptions.status = ?
		  AND subscriptions.order_id <> ''
		  AND subscriptions.starts_at <= ?
		  AND ((subscriptions.team_id = '' AND user_ledger.id IS NULL)
		    OR (subscriptions.team_id <> '' AND team_ledger.id IS NULL))
		ORDER BY subscriptions.starts_at ASC
	`, model.CreditLedgerMembership, model.CreditLedgerMembership, model.MembershipSubscriptionActive, now).Scan(&subscriptions).Error
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
		if current.TeamID != "" {
			var existing int64
			if err := tx.Model(&model.TeamCreditLedgerEntry{}).Where("reference_key = ? AND type = ?", *ledger.ReferenceKey, model.CreditLedgerMembership).Count(&existing).Error; err != nil {
				return err
			}
			if existing > 0 {
				return nil
			}
			account := model.TeamCreditAccount{TeamID: current.TeamID}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&account).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.TeamCreditAccount{}).Where("team_id = ?", current.TeamID).Updates(map[string]interface{}{
				"available_microcredits": gorm.Expr("available_microcredits + ?", ledger.AmountMicrocredits),
				"version":                gorm.Expr("version + 1"), "updated_at": now,
			}).Error; err != nil {
				return err
			}
			if err := tx.First(&account, "team_id = ?", current.TeamID).Error; err != nil {
				return err
			}
			return tx.Create(&model.TeamCreditLedgerEntry{
				ID: ledger.ID, TeamID: current.TeamID, ActorUserID: ledger.ActorUserID, Type: ledger.Type,
				AmountMicrocredits: ledger.AmountMicrocredits, AvailableDeltaMicrocredits: ledger.AvailableDeltaMicrocredits,
				AvailableAfterMicrocredits: account.AvailableMicrocredits, ReservedAfterMicrocredits: account.ReservedMicrocredits,
				Scene: ledger.Scene, Note: ledger.Note, ReferenceKey: ledger.ReferenceKey, CreatedAt: ledger.CreatedAt,
			}).Error
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

func (r *Repository) TeamForOwner(ownerID string, teamID string) (*model.Team, error) {
	var team model.Team
	return &team, r.db.First(&team, "id = ? AND owner_user_id = ? AND status = ?", teamID, ownerID, model.TeamStatusActive).Error
}

func (r *Repository) TeamMembers(teamID string) ([]model.TeamMember, error) {
	var members []model.TeamMember
	err := r.db.Where("team_id = ? AND status = ?", teamID, model.TeamMemberStatusActive).Order("created_at asc").Find(&members).Error
	return members, err
}

func (r *Repository) ActivateMembershipOrder(orderID string, actorID string, provider string, providerTradeNo string, note string, activation MembershipActivation, audit *model.AdminAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := activateMembershipOrderTx(tx, orderID, actorID, provider, providerTradeNo, note, activation, time.Now()); err != nil {
			return err
		}
		if audit != nil {
			return tx.Create(audit).Error
		}
		return nil
	})
}

func activateMembershipOrderTx(tx *gorm.DB, orderID string, actorID string, provider string, providerTradeNo string, note string, activation MembershipActivation, now time.Time) error {
	if activation.Subscription == nil {
		return errors.New("membership activation requires subscription")
	}
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
	if err := tx.Create(activation.Subscription).Error; err != nil {
		return err
	}
	if activation.MembershipLedger != nil && activation.MembershipLedger.AmountMicrocredits > 0 {
		if order.TeamID != "" {
			if err := grantTeamMembershipLedgerTx(tx, order.TeamID, activation.MembershipLedger, now); err != nil {
				return err
			}
		} else if err := grantPersonalCreditLedgerTx(tx, activation.MembershipLedger, now); err != nil {
			return err
		}
	}
	if activation.Referral != nil {
		return grantReferralRewardTx(tx, &order, activation.Referral, now)
	}
	return nil
}

func grantTeamMembershipLedgerTx(tx *gorm.DB, teamID string, ledger *model.CreditLedgerEntry, now time.Time) error {
	account := model.TeamCreditAccount{TeamID: teamID}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&account).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.TeamCreditAccount{}).Where("team_id = ?", teamID).Updates(map[string]interface{}{
		"available_microcredits": gorm.Expr("available_microcredits + ?", ledger.AmountMicrocredits),
		"version":                gorm.Expr("version + 1"), "updated_at": now,
	}).Error; err != nil {
		return err
	}
	if err := tx.First(&account, "team_id = ?", teamID).Error; err != nil {
		return err
	}
	return tx.Create(&model.TeamCreditLedgerEntry{
		ID: ledger.ID, TeamID: teamID, ActorUserID: ledger.ActorUserID, Type: ledger.Type,
		AmountMicrocredits: ledger.AmountMicrocredits, AvailableDeltaMicrocredits: ledger.AvailableDeltaMicrocredits,
		AvailableAfterMicrocredits: account.AvailableMicrocredits, ReservedAfterMicrocredits: account.ReservedMicrocredits,
		Scene: ledger.Scene, Note: ledger.Note, ReferenceKey: ledger.ReferenceKey, CreatedAt: ledger.CreatedAt,
	}).Error
}

func grantPersonalCreditLedgerTx(tx *gorm.DB, ledger *model.CreditLedgerEntry, now time.Time) error {
	if ledger == nil || ledger.UserID == "" || ledger.AmountMicrocredits <= 0 {
		return errors.New("credit ledger grant is invalid")
	}
	account := model.CreditAccount{UserID: ledger.UserID}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&account).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.CreditAccount{}).Where("user_id = ?", ledger.UserID).Updates(map[string]interface{}{
		"available_microcredits": gorm.Expr("available_microcredits + ?", ledger.AmountMicrocredits),
		"version":                gorm.Expr("version + 1"), "updated_at": now,
	}).Error; err != nil {
		return err
	}
	if err := tx.First(&account, "user_id = ?", ledger.UserID).Error; err != nil {
		return err
	}
	ledger.AvailableAfterMicrocredits = account.AvailableMicrocredits
	ledger.ReservedAfterMicrocredits = account.ReservedMicrocredits
	return tx.Create(ledger).Error
}

func grantReferralRewardTx(tx *gorm.DB, order *model.MembershipOrder, fulfillment *ReferralFulfillment, now time.Time) error {
	if order.TeamID != "" {
		return errors.New("team membership orders cannot grant referral rewards")
	}
	if fulfillment == nil || fulfillment.Reward == nil || fulfillment.InviterLedger == nil || fulfillment.InviteeLedger == nil {
		return errors.New("referral fulfillment is incomplete")
	}
	reward := fulfillment.Reward
	var relationship model.ReferralRelationship
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&relationship, "id = ?", reward.RelationshipID).Error; err != nil {
		return err
	}
	if relationship.InviteeUserID != order.UserID || relationship.InviterUserID != reward.InviterUserID ||
		reward.InviteeUserID != order.UserID || reward.MembershipOrderID != order.ID {
		return errors.New("referral fulfillment does not match membership order")
	}
	if relationship.Status == model.ReferralRelationshipDisqualified || relationship.Status == model.ReferralRelationshipRewarded {
		return nil
	}
	var priorPaidCount int64
	if err := tx.Model(&model.MembershipOrder{}).
		Where("user_id = ? AND team_id = '' AND status = ? AND id <> ?", order.UserID, model.MembershipOrderPaid, order.ID).
		Count(&priorPaidCount).Error; err != nil {
		return err
	}
	if priorPaidCount > 0 {
		return nil
	}
	if fulfillment.InviterLedger.UserID != reward.InviterUserID ||
		fulfillment.InviteeLedger.UserID != reward.InviteeUserID ||
		fulfillment.InviterLedger.AmountMicrocredits != reward.InviterRewardMicrocredits ||
		fulfillment.InviteeLedger.AmountMicrocredits != reward.InviteeRewardMicrocredits {
		return errors.New("referral credit ledgers do not match reward")
	}
	if err := tx.Create(reward).Error; err != nil {
		return err
	}
	if err := grantPersonalCreditLedgerTx(tx, fulfillment.InviterLedger, now); err != nil {
		return err
	}
	if err := grantPersonalCreditLedgerTx(tx, fulfillment.InviteeLedger, now); err != nil {
		return err
	}
	updated := tx.Model(&model.ReferralRelationship{}).
		Where("id = ? AND status = ?", relationship.ID, model.ReferralRelationshipEligible).
		Updates(map[string]interface{}{"status": model.ReferralRelationshipRewarded, "rewarded_at": now, "updated_at": now})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return errors.New("referral relationship state changed during fulfillment")
	}
	return nil
}
