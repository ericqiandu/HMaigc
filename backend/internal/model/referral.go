package model

import "time"

type ReferralRelationshipStatus string
type ReferralRewardStatus string

const (
	ReferralRelationshipEligible     ReferralRelationshipStatus = "eligible"
	ReferralRelationshipRewarded     ReferralRelationshipStatus = "rewarded"
	ReferralRelationshipDisqualified ReferralRelationshipStatus = "disqualified"

	ReferralRewardGranted ReferralRewardStatus = "granted"
)

// ReferralProfile owns the public invitation code for one user.
type ReferralProfile struct {
	UserID    string    `json:"userId" gorm:"primaryKey;size:36"`
	Code      string    `json:"code" gorm:"uniqueIndex;size:16"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ReferralRelationship is immutable after registration. Operational status may
// only move from eligible to rewarded or disqualified.
type ReferralRelationship struct {
	ID                     string                     `json:"id" gorm:"primaryKey;size:36"`
	InviterUserID          string                     `json:"inviterUserId" gorm:"index:idx_referral_inviter_bound,priority:1;size:36"`
	InviteeUserID          string                     `json:"inviteeUserId" gorm:"uniqueIndex;size:36"`
	ReferralCode           string                     `json:"referralCode" gorm:"size:16"`
	BindingIP              string                     `json:"bindingIp,omitempty" gorm:"size:64"`
	Status                 ReferralRelationshipStatus `json:"status" gorm:"index;size:24"`
	DisqualifiedBy         string                     `json:"disqualifiedBy,omitempty" gorm:"size:36"`
	DisqualificationReason string                     `json:"disqualificationReason,omitempty" gorm:"size:500"`
	DisqualifiedAt         *time.Time                 `json:"disqualifiedAt,omitempty"`
	RewardedAt             *time.Time                 `json:"rewardedAt,omitempty"`
	BoundAt                time.Time                  `json:"boundAt" gorm:"index:idx_referral_inviter_bound,priority:2"`
	CreatedAt              time.Time                  `json:"createdAt"`
	UpdatedAt              time.Time                  `json:"updatedAt"`
}

// ReferralRewardRule is the active commercial reward contract for one personal
// membership plan. Amounts use the same microcredit unit as the credit ledger.
type ReferralRewardRule struct {
	ID                        string    `json:"id" gorm:"primaryKey;size:36"`
	MembershipPlanID          string    `json:"membershipPlanId" gorm:"uniqueIndex;size:36"`
	InviterRewardMicrocredits int64     `json:"inviterRewardMicrocredits"`
	InviteeRewardMicrocredits int64     `json:"inviteeRewardMicrocredits"`
	Enabled                   bool      `json:"enabled" gorm:"index"`
	CreatedBy                 string    `json:"createdBy,omitempty" gorm:"size:36"`
	UpdatedBy                 string    `json:"updatedBy,omitempty" gorm:"size:36"`
	CreatedAt                 time.Time `json:"createdAt"`
	UpdatedAt                 time.Time `json:"updatedAt"`
}

// ReferralReward is an append-only fact proving that one invitee's first paid
// personal membership order granted both sides of the configured reward.
type ReferralReward struct {
	ID                        string               `json:"id" gorm:"primaryKey;size:36"`
	RelationshipID            string               `json:"relationshipId" gorm:"uniqueIndex;size:36"`
	MembershipOrderID         string               `json:"membershipOrderId" gorm:"uniqueIndex;size:36"`
	MembershipPlanID          string               `json:"membershipPlanId" gorm:"index;size:36"`
	RewardRuleID              string               `json:"rewardRuleId" gorm:"index;size:36"`
	InviterUserID             string               `json:"inviterUserId" gorm:"index;size:36"`
	InviteeUserID             string               `json:"inviteeUserId" gorm:"index;size:36"`
	InviterRewardMicrocredits int64                `json:"inviterRewardMicrocredits"`
	InviteeRewardMicrocredits int64                `json:"inviteeRewardMicrocredits"`
	Status                    ReferralRewardStatus `json:"status" gorm:"index;size:24"`
	GrantedAt                 time.Time            `json:"grantedAt" gorm:"index"`
	CreatedAt                 time.Time            `json:"createdAt"`
}
