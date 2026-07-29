package repository

import (
	"errors"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type UserRegistration struct {
	User                   *model.User
	Identity               *model.UserIdentity
	ReferralProfile        *model.ReferralProfile
	ReferralRelationship   *model.ReferralRelationship
	VerificationCodeID     string
	VerificationCodeUsedAt time.Time
}

type ReferralSummary struct {
	RegisteredCount           int64 `json:"registeredCount"`
	PurchasedCount            int64 `json:"purchasedCount"`
	EarnedInviterMicrocredits int64 `json:"earnedInviterMicrocredits"`
}

type AdminReferralSummary struct {
	RegisteredCount          int64 `json:"registeredCount"`
	PurchasedCount           int64 `json:"purchasedCount"`
	GrantedTotalMicrocredits int64 `json:"grantedTotalMicrocredits"`
}

type ReferralRelationshipRow struct {
	model.ReferralRelationship
	InviteeUsername      string `json:"inviteeUsername" gorm:"column:invitee_username"`
	InviteeDisplayName   string `json:"inviteeDisplayName" gorm:"column:invitee_display_name"`
	PlanName             string `json:"planName,omitempty" gorm:"column:plan_name"`
	RewardedMicrocredits int64  `json:"rewardedMicrocredits" gorm:"column:rewarded_microcredits"`
}

func (r *Repository) CreateUserRegistration(input UserRegistration) error {
	if input.User == nil || input.ReferralProfile == nil {
		return errors.New("user registration requires user and referral profile")
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		if input.VerificationCodeID != "" {
			result := tx.Model(&model.EmailVerificationCode{}).
				Where("id = ? AND used_at IS NULL AND expires_at > ?", input.VerificationCodeID, input.VerificationCodeUsedAt).
				Update("used_at", input.VerificationCodeUsedAt)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errors.New("email verification code is no longer valid")
			}
		}
		if err := tx.Create(input.User).Error; err != nil {
			return err
		}
		if input.Identity != nil {
			if err := tx.Create(input.Identity).Error; err != nil {
				return err
			}
		}
		if err := tx.Create(input.ReferralProfile).Error; err != nil {
			return err
		}
		if input.ReferralRelationship != nil {
			if input.ReferralRelationship.InviteeUserID != input.User.ID {
				return errors.New("referral relationship invitee does not match registered user")
			}
			if input.ReferralRelationship.InviterUserID == input.User.ID {
				return errors.New("self referral is not allowed")
			}
			if err := tx.Create(input.ReferralRelationship).Error; err != nil {
				return err
			}
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.CreditAccount{UserID: input.User.ID}).Error
	})
}

func (r *Repository) ReferralProfileByCode(code string) (*model.ReferralProfile, error) {
	var profile model.ReferralProfile
	return &profile, r.db.First(&profile, "code = ?", code).Error
}

func (r *Repository) ReferralProfileForUser(userID string) (*model.ReferralProfile, error) {
	var profile model.ReferralProfile
	return &profile, r.db.First(&profile, "user_id = ?", userID).Error
}

func (r *Repository) CreateReferralProfile(profile *model.ReferralProfile) (bool, error) {
	result := r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoNothing: true,
	}).Create(profile)
	return result.RowsAffected == 1, result.Error
}

func (r *Repository) ReferralRelationshipForInvitee(inviteeUserID string) (*model.ReferralRelationship, error) {
	var relationship model.ReferralRelationship
	return &relationship, r.db.First(&relationship, "invitee_user_id = ?", inviteeUserID).Error
}

func (r *Repository) ReferralRewardRuleForPlan(planID string) (*model.ReferralRewardRule, error) {
	var rule model.ReferralRewardRule
	return &rule, r.db.First(&rule, "membership_plan_id = ?", planID).Error
}

func (r *Repository) ReferralRewardRules() ([]model.ReferralRewardRule, error) {
	var rules []model.ReferralRewardRule
	return rules, r.db.Order("created_at asc").Find(&rules).Error
}

func (r *Repository) SaveReferralRewardRule(rule *model.ReferralRewardRule, audit *model.AdminAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(rule).Error; err != nil {
			return err
		}
		if audit != nil {
			return tx.Create(audit).Error
		}
		return nil
	})
}

func (r *Repository) SaveReferralProgramSetting(setting *model.SystemSetting, audit *model.AdminAuditEvent) error {
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

func (r *Repository) ReferralSummaryForInviter(inviterUserID string) (ReferralSummary, error) {
	var summary ReferralSummary
	if err := r.db.Model(&model.ReferralRelationship{}).
		Where("inviter_user_id = ?", inviterUserID).
		Count(&summary.RegisteredCount).Error; err != nil {
		return summary, err
	}
	if err := r.db.Model(&model.ReferralReward{}).
		Select("COUNT(*) AS purchased_count, COALESCE(SUM(inviter_reward_microcredits), 0) AS earned_inviter_microcredits").
		Where("inviter_user_id = ? AND status = ?", inviterUserID, model.ReferralRewardGranted).
		Scan(&summary).Error; err != nil {
		return summary, err
	}
	return summary, nil
}

func (r *Repository) ReferralRelationshipsForInviter(inviterUserID string, limit int, offset int) ([]ReferralRelationshipRow, int64, error) {
	var total int64
	if err := r.db.Model(&model.ReferralRelationship{}).Where("inviter_user_id = ?", inviterUserID).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []ReferralRelationshipRow
	err := r.db.Table("referral_relationships AS relationships").
		Select(`relationships.*, users.username AS invitee_username, users.display_name AS invitee_display_name,
			COALESCE(membership_plans.name, '') AS plan_name,
			COALESCE(referral_rewards.inviter_reward_microcredits, 0) AS rewarded_microcredits`).
		Joins("JOIN users ON users.id = relationships.invitee_user_id").
		Joins("LEFT JOIN referral_rewards ON referral_rewards.relationship_id = relationships.id").
		Joins("LEFT JOIN membership_plans ON membership_plans.id = referral_rewards.membership_plan_id").
		Where("relationships.inviter_user_id = ?", inviterUserID).
		Order("relationships.bound_at DESC").
		Limit(limit).Offset(offset).
		Scan(&rows).Error
	return rows, total, err
}

func (r *Repository) AdminReferralRelationships(limit int, offset int) ([]ReferralRelationshipRow, int64, error) {
	var total int64
	if err := r.db.Model(&model.ReferralRelationship{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []ReferralRelationshipRow
	err := r.db.Table("referral_relationships AS relationships").
		Select(`relationships.*, users.username AS invitee_username, users.display_name AS invitee_display_name,
			COALESCE(membership_plans.name, '') AS plan_name,
			COALESCE(referral_rewards.inviter_reward_microcredits, 0) AS rewarded_microcredits`).
		Joins("JOIN users ON users.id = relationships.invitee_user_id").
		Joins("LEFT JOIN referral_rewards ON referral_rewards.relationship_id = relationships.id").
		Joins("LEFT JOIN membership_plans ON membership_plans.id = referral_rewards.membership_plan_id").
		Order("relationships.bound_at DESC").
		Limit(limit).Offset(offset).
		Scan(&rows).Error
	return rows, total, err
}

func (r *Repository) AdminReferralSummary() (AdminReferralSummary, error) {
	var summary AdminReferralSummary
	if err := r.db.Model(&model.ReferralRelationship{}).Count(&summary.RegisteredCount).Error; err != nil {
		return summary, err
	}
	if err := r.db.Model(&model.ReferralReward{}).
		Select(`COUNT(*) AS purchased_count,
			COALESCE(SUM(inviter_reward_microcredits + invitee_reward_microcredits), 0) AS granted_total_microcredits`).
		Where("status = ?", model.ReferralRewardGranted).
		Scan(&summary).Error; err != nil {
		return summary, err
	}
	return summary, nil
}

func (r *Repository) HasPaidPersonalMembershipOrder(userID string, excludingOrderID string) (bool, error) {
	var count int64
	query := r.db.Model(&model.MembershipOrder{}).
		Where("user_id = ? AND team_id = '' AND status = ?", userID, model.MembershipOrderPaid)
	if excludingOrderID != "" {
		query = query.Where("id <> ?", excludingOrderID)
	}
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *Repository) DisqualifyReferralRelationship(id string, actorID string, reason string, now time.Time, audit *model.AdminAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.ReferralRelationship{}).
			Where("id = ? AND status = ?", id, model.ReferralRelationshipEligible).
			Updates(map[string]interface{}{
				"status": model.ReferralRelationshipDisqualified, "disqualified_by": actorID,
				"disqualification_reason": reason, "disqualified_at": now, "updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		if audit != nil {
			return tx.Create(audit).Error
		}
		return nil
	})
}
