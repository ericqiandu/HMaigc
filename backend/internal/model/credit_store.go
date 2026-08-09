package model

import "time"

type CreditProductCategory string
type CreditTopupOrderStatus string
type PaymentOrderType string

const (
	CreditProductCategorySurprise CreditProductCategory = "surprise"
	CreditProductCategoryGeneral  CreditProductCategory = "general"

	CreditTopupOrderPending   CreditTopupOrderStatus = "pending"
	CreditTopupOrderPaid      CreditTopupOrderStatus = "paid"
	CreditTopupOrderCancelled CreditTopupOrderStatus = "cancelled"
	CreditTopupOrderRefunded  CreditTopupOrderStatus = "refunded"

	PaymentOrderMembership  PaymentOrderType = "membership"
	PaymentOrderCreditTopup PaymentOrderType = "credit_topup"
)

// CreditTopupProduct 是管理员维护的积分商品目录。订单始终保存商品快照，后续改价不会改写历史事实。
type CreditTopupProduct struct {
	ID                     string                `json:"id" gorm:"primaryKey;size:36"`
	Code                   string                `json:"code" gorm:"uniqueIndex;size:64"`
	Name                   string                `json:"name" gorm:"size:120"`
	Category               CreditProductCategory `json:"category" gorm:"index;size:24"`
	BaseMicrocredits       int64                 `json:"baseMicrocredits"`
	BonusMicrocredits      int64                 `json:"bonusMicrocredits"`
	PriceCents             int64                 `json:"priceCents"`
	OriginalPriceCents     int64                 `json:"originalPriceCents"`
	Currency               string                `json:"currency" gorm:"size:12"`
	RequiredMembershipTier string                `json:"requiredMembershipTier,omitempty" gorm:"size:40"`
	WeeklyPurchaseLimit    int                   `json:"weeklyPurchaseLimit"`
	StockLimit             int64                 `json:"stockLimit"`
	SoldCount              int64                 `json:"soldCount"`
	SaleEndsAt             *time.Time            `json:"saleEndsAt,omitempty" gorm:"index"`
	Badge                  string                `json:"badge,omitempty" gorm:"size:80"`
	Description            string                `json:"description,omitempty" gorm:"size:300"`
	ImageURL               string                `json:"imageUrl,omitempty" gorm:"size:500"`
	Enabled                bool                  `json:"enabled" gorm:"index"`
	SortOrder              int                   `json:"sortOrder" gorm:"index"`
	CreatedAt              time.Time             `json:"createdAt"`
	UpdatedAt              time.Time             `json:"updatedAt"`
}

type CreditTopupOrder struct {
	ID                  string                 `json:"id" gorm:"primaryKey;size:36"`
	OrderNumber         string                 `json:"orderNumber" gorm:"uniqueIndex;size:40"`
	UserID              string                 `json:"userId" gorm:"index;uniqueIndex:idx_credit_topup_user_idempotency,priority:1;size:36"`
	ProductID           string                 `json:"productId" gorm:"index;size:36"`
	BaseMicrocredits    int64                  `json:"baseMicrocredits"`
	BonusMicrocredits   int64                  `json:"bonusMicrocredits"`
	TotalMicrocredits   int64                  `json:"totalMicrocredits"`
	TotalPriceCents     int64                  `json:"totalPriceCents"`
	Currency            string                 `json:"currency" gorm:"size:12"`
	Status              CreditTopupOrderStatus `json:"status" gorm:"index;size:24"`
	ProductSnapshotJSON string                 `json:"productSnapshotJson" gorm:"type:text"`
	IdempotencyKey      string                 `json:"-" gorm:"uniqueIndex:idx_credit_topup_user_idempotency,priority:2;size:120"`
	RequestHash         string                 `json:"-" gorm:"size:64"`
	PaymentProvider     string                 `json:"paymentProvider,omitempty" gorm:"size:40"`
	ProviderTradeNo     string                 `json:"providerTradeNo,omitempty" gorm:"size:120"`
	ResolutionNote      string                 `json:"resolutionNote,omitempty" gorm:"size:500"`
	PaidAt              *time.Time             `json:"paidAt,omitempty"`
	CreatedAt           time.Time              `json:"createdAt"`
	UpdatedAt           time.Time              `json:"updatedAt"`
}
