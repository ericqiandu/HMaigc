package model

import "time"

type PaymentProvider string
type PaymentCheckoutStatus string
type PaymentTransactionStatus string
type PaymentWebhookStatus string

const (
	PaymentProviderWechat PaymentProvider = "wechat"
	PaymentProviderAlipay PaymentProvider = "alipay"

	PaymentCheckoutActive   PaymentCheckoutStatus = "active"
	PaymentCheckoutExpired  PaymentCheckoutStatus = "expired"
	PaymentCheckoutConsumed PaymentCheckoutStatus = "consumed"

	PaymentTransactionCreated        PaymentTransactionStatus = "created"
	PaymentTransactionPending        PaymentTransactionStatus = "pending"
	PaymentTransactionReviewRequired PaymentTransactionStatus = "review_required"
	PaymentTransactionPaid           PaymentTransactionStatus = "paid"
	PaymentTransactionClosed         PaymentTransactionStatus = "closed"
	PaymentTransactionFailed         PaymentTransactionStatus = "failed"
	PaymentTransactionRefunded       PaymentTransactionStatus = "refunded"

	PaymentWebhookReceived       PaymentWebhookStatus = "received"
	PaymentWebhookProcessed      PaymentWebhookStatus = "processed"
	PaymentWebhookRejected       PaymentWebhookStatus = "rejected"
	PaymentWebhookReviewRequired PaymentWebhookStatus = "review_required"
)

// PaymentCheckoutSession 以哈希校验 bearer token，并以绑定订单事实的密文支持所有者恢复同一链接。
type PaymentCheckoutSession struct {
	ID          string                `json:"id" gorm:"primaryKey;size:36"`
	OrderType   PaymentOrderType      `json:"orderType" gorm:"index;size:24;default:membership"`
	OrderID     string                `json:"orderId" gorm:"uniqueIndex;size:36"`
	UserID      string                `json:"userId" gorm:"index;size:36"`
	TokenHash   string                `json:"-" gorm:"uniqueIndex;size:64"`
	TokenCipher string                `json:"-" gorm:"type:text;not null;default:''"`
	Status      PaymentCheckoutStatus `json:"status" gorm:"index;size:24"`
	ExpiresAt   time.Time             `json:"expiresAt" gorm:"index"`
	CreatedAt   time.Time             `json:"createdAt"`
	UpdatedAt   time.Time             `json:"updatedAt"`
}

type PaymentTransaction struct {
	ID              string                   `json:"id" gorm:"primaryKey;size:36"`
	OrderType       PaymentOrderType         `json:"orderType" gorm:"index;size:24;default:membership"`
	OrderID         string                   `json:"orderId" gorm:"index;size:36"`
	UserID          string                   `json:"userId" gorm:"index;size:36"`
	Provider        PaymentProvider          `json:"provider" gorm:"index;size:24"`
	MerchantOrderNo string                   `json:"merchantOrderNo" gorm:"uniqueIndex;size:64"`
	ProviderTradeNo string                   `json:"providerTradeNo,omitempty" gorm:"index;size:120"`
	AmountCents     int64                    `json:"amountCents"`
	Currency        string                   `json:"currency" gorm:"size:12"`
	Status          PaymentTransactionStatus `json:"status" gorm:"index;size:24"`
	CodeURL         string                   `json:"codeUrl,omitempty" gorm:"type:text"`
	FailureCode     string                   `json:"failureCode,omitempty" gorm:"size:80;not null;default:''"`
	FailureReason   string                   `json:"failureReason,omitempty" gorm:"size:500"`
	ExpiresAt       *time.Time               `json:"expiresAt,omitempty" gorm:"index"`
	PaidAt          *time.Time               `json:"paidAt,omitempty"`
	ClosedAt        *time.Time               `json:"closedAt,omitempty"`
	CreatedAt       time.Time                `json:"createdAt"`
	UpdatedAt       time.Time                `json:"updatedAt"`
}

// PaymentWebhookEvent 是验签成功后的通知审计与幂等依据。
type PaymentWebhookEvent struct {
	ID              string               `json:"id" gorm:"primaryKey;size:36"`
	Provider        PaymentProvider      `json:"provider" gorm:"uniqueIndex:idx_payment_webhook_provider_event,priority:1;size:24"`
	ProviderEventID string               `json:"providerEventId" gorm:"uniqueIndex:idx_payment_webhook_provider_event,priority:2;size:160"`
	TransactionID   string               `json:"transactionId,omitempty" gorm:"index;size:36"`
	MerchantOrderNo string               `json:"merchantOrderNo,omitempty" gorm:"size:64;not null;default:''"`
	ProviderTradeNo string               `json:"providerTradeNo,omitempty" gorm:"size:120;not null;default:''"`
	AmountCents     int64                `json:"amountCents"`
	Currency        string               `json:"currency,omitempty" gorm:"size:12;not null;default:''"`
	PaidAt          *time.Time           `json:"paidAt,omitempty"`
	PayloadDigest   string               `json:"payloadDigest" gorm:"size:64"`
	Status          PaymentWebhookStatus `json:"status" gorm:"index;size:24"`
	FailureCode     string               `json:"failureCode,omitempty" gorm:"size:80;not null;default:''"`
	FailureReason   string               `json:"failureReason,omitempty" gorm:"size:500"`
	ReceivedAt      time.Time            `json:"receivedAt"`
	ProcessedAt     *time.Time           `json:"processedAt,omitempty"`
	CreatedAt       time.Time            `json:"createdAt"`
	UpdatedAt       time.Time            `json:"updatedAt"`
}
