package model

import "time"

type MembershipAudience string
type MembershipBillingCycle string
type MembershipSubscriptionStatus string
type MembershipOrderStatus string
type TeamStatus string
type TeamMemberRole string
type TeamMemberStatus string

const (
	MembershipAudiencePersonal MembershipAudience = "personal"
	MembershipAudienceTeam     MembershipAudience = "team"

	MembershipBillingCycleFree  MembershipBillingCycle = "free"
	MembershipBillingCycleMonth MembershipBillingCycle = "month"
	MembershipBillingCycleYear  MembershipBillingCycle = "year"

	MembershipSubscriptionActive    MembershipSubscriptionStatus = "active"
	MembershipSubscriptionExpired   MembershipSubscriptionStatus = "expired"
	MembershipSubscriptionCancelled MembershipSubscriptionStatus = "cancelled"

	MembershipOrderPending   MembershipOrderStatus = "pending"
	MembershipOrderPaid      MembershipOrderStatus = "paid"
	MembershipOrderCancelled MembershipOrderStatus = "cancelled"
	MembershipOrderRefunded  MembershipOrderStatus = "refunded"

	TeamStatusActive   TeamStatus = "active"
	TeamStatusDisabled TeamStatus = "disabled"

	TeamMemberRoleOwner  TeamMemberRole = "owner"
	TeamMemberRoleAdmin  TeamMemberRole = "admin"
	TeamMemberRoleMember TeamMemberRole = "member"

	TeamMemberStatusActive  TeamMemberStatus = "active"
	TeamMemberStatusRemoved TeamMemberStatus = "removed"
)

// MembershipPlan 是可销售权益的唯一配置源，历史订单与订阅通过快照保持审计稳定。
type MembershipPlan struct {
	ID                         string                 `json:"id" gorm:"primaryKey;size:36"`
	Code                       string                 `json:"code" gorm:"uniqueIndex;size:80"`
	Name                       string                 `json:"name" gorm:"size:80"`
	Tier                       string                 `json:"tier" gorm:"index;size:32"`
	Audience                   MembershipAudience     `json:"audience" gorm:"index;size:24"`
	BillingCycle               MembershipBillingCycle `json:"billingCycle" gorm:"index;size:24"`
	PriceCents                 int64                  `json:"priceCents"`
	OriginalPriceCents         int64                  `json:"originalPriceCents"`
	Currency                   string                 `json:"currency" gorm:"size:12"`
	CreditsPerPeriod           int64                  `json:"creditsPerPeriod"`
	ImageConcurrency           int                    `json:"imageConcurrency"`
	VideoConcurrency           int                    `json:"videoConcurrency"`
	TopupDiscountBasisPoints   int                    `json:"topupDiscountBasisPoints"`
	MinSeats                   int                    `json:"minSeats"`
	MaxSeats                   int                    `json:"maxSeats"`
	BenefitsJSON               string                 `json:"benefitsJson" gorm:"type:text"`
	Enabled                    bool                   `json:"enabled" gorm:"index"`
	SortOrder                  int                    `json:"sortOrder" gorm:"index"`
	CreatedAt                  time.Time              `json:"createdAt"`
	UpdatedAt                  time.Time              `json:"updatedAt"`
}

type Team struct {
	ID          string     `json:"id" gorm:"primaryKey;size:36"`
	OwnerUserID string     `json:"ownerUserId" gorm:"index;size:36"`
	Name        string     `json:"name" gorm:"size:120"`
	Status      TeamStatus `json:"status" gorm:"index;size:24"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type TeamMember struct {
	ID                           string           `json:"id" gorm:"primaryKey;size:36"`
	TeamID                       string           `json:"teamId" gorm:"uniqueIndex:idx_team_member,priority:1;size:36"`
	UserID                       string           `json:"userId" gorm:"uniqueIndex:idx_team_member,priority:2;index;size:36"`
	Role                         TeamMemberRole   `json:"role" gorm:"size:24"`
	Status                       TeamMemberStatus `json:"status" gorm:"index;size:24"`
	MonthlyCreditLimitMicrocredits int64          `json:"monthlyCreditLimitMicrocredits"`
	CreatedAt                    time.Time        `json:"createdAt"`
	UpdatedAt                    time.Time        `json:"updatedAt"`
}

type MembershipOrder struct {
	ID                 string                `json:"id" gorm:"primaryKey;size:36"`
	OrderNumber        string                `json:"orderNumber" gorm:"uniqueIndex;size:40"`
	UserID             string                `json:"userId" gorm:"index;size:36"`
	TeamID             string                `json:"teamId,omitempty" gorm:"index;size:36"`
	PlanID             string                `json:"planId" gorm:"index;size:36"`
	Seats              int                   `json:"seats"`
	UnitPriceCents     int64                 `json:"unitPriceCents"`
	TotalPriceCents    int64                 `json:"totalPriceCents"`
	Currency           string                `json:"currency" gorm:"size:12"`
	Status             MembershipOrderStatus `json:"status" gorm:"index;size:24"`
	PaymentProvider    string                `json:"paymentProvider" gorm:"size:40"`
	ProviderTradeNo    string                `json:"providerTradeNo" gorm:"size:120"`
	PlanSnapshotJSON   string                `json:"planSnapshotJson" gorm:"type:text"`
	ResolvedBy         string                `json:"resolvedBy,omitempty" gorm:"size:36"`
	ResolutionNote     string                `json:"resolutionNote,omitempty" gorm:"size:500"`
	PaidAt             *time.Time            `json:"paidAt,omitempty"`
	CreatedAt          time.Time             `json:"createdAt"`
	UpdatedAt          time.Time             `json:"updatedAt"`
}

type MembershipSubscription struct {
	ID               string                       `json:"id" gorm:"primaryKey;size:36"`
	UserID           string                       `json:"userId" gorm:"index;size:36"`
	TeamID           string                       `json:"teamId,omitempty" gorm:"index;size:36"`
	PlanID           string                       `json:"planId" gorm:"index;size:36"`
	OrderID          string                       `json:"orderId,omitempty" gorm:"uniqueIndex;size:36"`
	Status           MembershipSubscriptionStatus `json:"status" gorm:"index;size:24"`
	Seats            int                          `json:"seats"`
	PlanSnapshotJSON string                       `json:"planSnapshotJson" gorm:"type:text"`
	StartsAt         time.Time                    `json:"startsAt" gorm:"index"`
	EndsAt           *time.Time                   `json:"endsAt,omitempty" gorm:"index"`
	CreatedAt        time.Time                    `json:"createdAt"`
	UpdatedAt        time.Time                    `json:"updatedAt"`
}
