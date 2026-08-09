package model

import "time"

type MembershipAudience string
type MembershipBillingCycle string
type MembershipSubscriptionStatus string
type MembershipOrderStatus string
type TeamStatus string
type TeamMemberRole string
type TeamMemberStatus string
type TeamInvitationStatus string
type InvoiceRequestStatus string

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

	TeamInvitationStatusPending  TeamInvitationStatus = "pending"
	TeamInvitationStatusAccepted TeamInvitationStatus = "accepted"
	TeamInvitationStatusRevoked  TeamInvitationStatus = "revoked"
	TeamInvitationStatusExpired  TeamInvitationStatus = "expired"

	InvoiceRequestStatusPending  InvoiceRequestStatus = "pending"
	InvoiceRequestStatusIssued   InvoiceRequestStatus = "issued"
	InvoiceRequestStatusRejected InvoiceRequestStatus = "rejected"
)

// MembershipPlan 是可销售权益的唯一配置源，历史订单与订阅通过快照保持审计稳定。
type MembershipPlan struct {
	ID                        string                 `json:"id" gorm:"primaryKey;size:36"`
	Code                      string                 `json:"code" gorm:"uniqueIndex;size:80"`
	Name                      string                 `json:"name" gorm:"size:80"`
	Tier                      string                 `json:"tier" gorm:"index;size:32"`
	Audience                  MembershipAudience     `json:"audience" gorm:"index;size:24"`
	BillingCycle              MembershipBillingCycle `json:"billingCycle" gorm:"index;size:24"`
	PriceCents                int64                  `json:"priceCents"`
	OriginalPriceCents        int64                  `json:"originalPriceCents"`
	Currency                  string                 `json:"currency" gorm:"size:12"`
	CreditsPerPeriod          int64                  `json:"creditsPerPeriod"`
	ImageConcurrency          int                    `json:"imageConcurrency"`
	VideoConcurrency          int                    `json:"videoConcurrency"`
	UnlimitedTaskQueue        bool                   `json:"unlimitedTaskQueue"`
	TeamStorageBytes          int64                  `json:"teamStorageBytes"`
	SharedAssetsEnabled       bool                   `json:"sharedAssetsEnabled"`
	ProjectPermissionsEnabled bool                   `json:"projectPermissionsEnabled"`
	InvoicingEnabled          bool                   `json:"invoicingEnabled"`
	CommercialUseEnabled      bool                   `json:"commercialUseEnabled"`
	TopupDiscountBasisPoints  int                    `json:"topupDiscountBasisPoints"`
	MinSeats                  int                    `json:"minSeats"`
	MaxSeats                  int                    `json:"maxSeats"`
	BenefitsJSON              string                 `json:"benefitsJson" gorm:"type:text"`
	Enabled                   bool                   `json:"enabled" gorm:"index"`
	SortOrder                 int                    `json:"sortOrder" gorm:"index"`
	CreatedAt                 time.Time              `json:"createdAt"`
	UpdatedAt                 time.Time              `json:"updatedAt"`
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
	ID                             string           `json:"id" gorm:"primaryKey;size:36"`
	TeamID                         string           `json:"teamId" gorm:"uniqueIndex:idx_team_member,priority:1;size:36"`
	UserID                         string           `json:"userId" gorm:"uniqueIndex:idx_team_member,priority:2;index;size:36"`
	Role                           TeamMemberRole   `json:"role" gorm:"size:24"`
	Status                         TeamMemberStatus `json:"status" gorm:"index;size:24"`
	MonthlyCreditLimitMicrocredits int64            `json:"monthlyCreditLimitMicrocredits"`
	CreatedAt                      time.Time        `json:"createdAt"`
	UpdatedAt                      time.Time        `json:"updatedAt"`
}

// TeamInvitation 只持久化邀请凭证哈希，原始凭证仅在创建成功时返回一次。
type TeamInvitation struct {
	ID               string               `json:"id" gorm:"primaryKey;size:36"`
	TeamID           string               `json:"teamId" gorm:"uniqueIndex:idx_team_invitation_email,priority:1;index;size:36"`
	InviterUserID    string               `json:"inviterUserId" gorm:"index;size:36"`
	Email            string               `json:"email" gorm:"uniqueIndex:idx_team_invitation_email,priority:2;size:160"`
	Role             TeamMemberRole       `json:"role" gorm:"size:24"`
	Status           TeamInvitationStatus `json:"status" gorm:"index;size:24"`
	TokenHash        string               `json:"-" gorm:"uniqueIndex;size:64"`
	ExpiresAt        time.Time            `json:"expiresAt" gorm:"index"`
	AcceptedByUserID string               `json:"acceptedByUserId,omitempty" gorm:"index;size:36"`
	AcceptedAt       *time.Time           `json:"acceptedAt,omitempty"`
	RevokedAt        *time.Time           `json:"revokedAt,omitempty"`
	CreatedAt        time.Time            `json:"createdAt"`
	UpdatedAt        time.Time            `json:"updatedAt"`
}

// TeamAuditEvent 是团队写操作的追加式审计记录，不允许由普通更新覆盖。
type TeamAuditEvent struct {
	ID                 string    `json:"id" gorm:"primaryKey;size:36"`
	TeamID             string    `json:"teamId" gorm:"index;size:36"`
	ActorUserID        string    `json:"actorUserId" gorm:"index;size:36"`
	Action             string    `json:"action" gorm:"index;size:64"`
	TargetUserID       string    `json:"targetUserId,omitempty" gorm:"index;size:36"`
	TargetInvitationID string    `json:"targetInvitationId,omitempty" gorm:"index;size:36"`
	MetadataJSON       string    `json:"metadataJson" gorm:"type:text"`
	CreatedAt          time.Time `json:"createdAt" gorm:"index"`
}

// TeamCreditAccount 是团队积分的唯一余额真源；成员调用只记录 actor，不落入个人积分账户。
type TeamCreditAccount struct {
	TeamID                string    `json:"teamId" gorm:"primaryKey;size:36"`
	AvailableMicrocredits int64     `json:"availableMicrocredits"`
	ReservedMicrocredits  int64     `json:"reservedMicrocredits"`
	Version               int64     `json:"version"`
	CreatedAt             time.Time `json:"createdAt"`
	UpdatedAt             time.Time `json:"updatedAt"`
}

// TeamCreditLedgerEntry 是追加式团队积分流水，ActorUserID 用于成员额度与审计归因。
type TeamCreditLedgerEntry struct {
	ID                         string           `json:"id" gorm:"primaryKey;size:36"`
	TeamID                     string           `json:"teamId" gorm:"index:idx_team_credit_ledger_created,priority:1;size:36"`
	ActorUserID                string           `json:"actorUserId" gorm:"index:idx_team_credit_actor_created,priority:1;size:36"`
	Type                       CreditLedgerType `json:"type" gorm:"size:32;index"`
	AmountMicrocredits         int64            `json:"amountMicrocredits"`
	AvailableDeltaMicrocredits int64            `json:"availableDeltaMicrocredits"`
	ReservedDeltaMicrocredits  int64            `json:"reservedDeltaMicrocredits"`
	AvailableAfterMicrocredits int64            `json:"availableAfterMicrocredits"`
	ReservedAfterMicrocredits  int64            `json:"reservedAfterMicrocredits"`
	BillingOrderID             string           `json:"billingOrderId,omitempty" gorm:"index;size:36"`
	Model                      string           `json:"model,omitempty" gorm:"size:120;index"`
	ChannelID                  string           `json:"channelId,omitempty" gorm:"size:36;index"`
	Scene                      string           `json:"scene,omitempty" gorm:"size:80;index"`
	Note                       string           `json:"note,omitempty" gorm:"size:500"`
	ReferenceKey               *string          `json:"referenceKey,omitempty" gorm:"size:180;uniqueIndex"`
	CreatedAt                  time.Time        `json:"createdAt" gorm:"index:idx_team_credit_ledger_created,priority:2;index:idx_team_credit_actor_created,priority:2"`
}

// InvoiceRequest 只表示开票业务状态；真实电子发票必须由后台或已配置的税务服务商回填。
type InvoiceRequest struct {
	ID                string               `json:"id" gorm:"primaryKey;size:36"`
	UserID            string               `json:"userId" gorm:"index;size:36"`
	TeamID            string               `json:"teamId,omitempty" gorm:"index;size:36"`
	MembershipOrderID string               `json:"membershipOrderId" gorm:"uniqueIndex;size:36"`
	Title             string               `json:"title" gorm:"size:200"`
	TaxNumber         string               `json:"taxNumber,omitempty" gorm:"size:80"`
	Email             string               `json:"email" gorm:"size:160"`
	AmountCents       int64                `json:"amountCents"`
	Status            InvoiceRequestStatus `json:"status" gorm:"index;size:24"`
	InvoiceNumber     string               `json:"invoiceNumber,omitempty" gorm:"size:120"`
	InvoiceURL        string               `json:"invoiceUrl,omitempty"`
	ResolutionNote    string               `json:"resolutionNote,omitempty" gorm:"size:500"`
	ResolvedBy        string               `json:"resolvedBy,omitempty" gorm:"index;size:36"`
	ResolvedAt        *time.Time           `json:"resolvedAt,omitempty"`
	CreatedAt         time.Time            `json:"createdAt" gorm:"index"`
	UpdatedAt         time.Time            `json:"updatedAt"`
}

type MembershipOrder struct {
	ID               string                `json:"id" gorm:"primaryKey;size:36"`
	OrderNumber      string                `json:"orderNumber" gorm:"uniqueIndex;size:40"`
	UserID           string                `json:"userId" gorm:"index;size:36"`
	IdempotencyKey   string                `json:"-" gorm:"size:120;not null;default:''"`
	RequestHash      string                `json:"-" gorm:"size:64;not null;default:''"`
	TeamID           string                `json:"teamId,omitempty" gorm:"index;size:36"`
	PlanID           string                `json:"planId" gorm:"index;size:36"`
	Seats            int                   `json:"seats"`
	UnitPriceCents   int64                 `json:"unitPriceCents"`
	TotalPriceCents  int64                 `json:"totalPriceCents"`
	Currency         string                `json:"currency" gorm:"size:12"`
	Status           MembershipOrderStatus `json:"status" gorm:"index;size:24"`
	PaymentProvider  string                `json:"paymentProvider" gorm:"size:40"`
	ProviderTradeNo  string                `json:"providerTradeNo" gorm:"size:120"`
	PlanSnapshotJSON string                `json:"planSnapshotJson" gorm:"type:text"`
	ResolvedBy       string                `json:"resolvedBy,omitempty" gorm:"size:36"`
	ResolutionNote   string                `json:"resolutionNote,omitempty" gorm:"size:500"`
	PaidAt           *time.Time            `json:"paidAt,omitempty"`
	CreatedAt        time.Time             `json:"createdAt"`
	UpdatedAt        time.Time             `json:"updatedAt"`
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
