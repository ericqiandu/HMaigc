package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

type MembershipCheckoutSummary struct {
	Audience              model.MembershipAudience     `json:"audience"`
	Code                  string                       `json:"code"`
	Name                  string                       `json:"name"`
	Tier                  string                       `json:"tier"`
	BillingCycle          model.MembershipBillingCycle `json:"billingCycle"`
	Seats                 int                          `json:"seats"`
	ActualPriceCents      int64                        `json:"actualPriceCents"`
	OriginalPriceCents    int64                        `json:"originalPriceCents"`
	CreditsPerPeriod      int64                        `json:"creditsPerPeriod"`
	TotalCreditsPerPeriod int64                        `json:"totalCreditsPerPeriod"`
}

type CreditTopupCheckoutSummary struct {
	ActualPriceCents  int64 `json:"actualPriceCents"`
	TotalMicrocredits int64 `json:"totalMicrocredits"`
}

type PaymentCheckoutTransactionView struct {
	Provider  model.PaymentProvider          `json:"provider"`
	Status    model.PaymentTransactionStatus `json:"status"`
	CodeURL   string                         `json:"codeUrl"`
	ExpiresAt time.Time                      `json:"expiresAt"`
}

type PaymentCheckoutView struct {
	OrderType          model.PaymentOrderType          `json:"orderType"`
	OrderNumber        string                          `json:"orderNumber"`
	OrderStatus        string                          `json:"orderStatus"`
	CheckoutStatus     model.PaymentCheckoutStatus     `json:"checkoutStatus"`
	Currency           string                          `json:"currency"`
	ServerNow          time.Time                       `json:"serverNow"`
	ExpiresAt          time.Time                       `json:"expiresAt"`
	Providers          []model.PaymentProvider         `json:"providers"`
	ActiveTransaction  *PaymentCheckoutTransactionView `json:"activeTransaction,omitempty"`
	MembershipSummary  *MembershipCheckoutSummary      `json:"membershipSummary,omitempty"`
	CreditTopupSummary *CreditTopupCheckoutSummary     `json:"creditTopupSummary,omitempty"`
}

type validatedMembershipOrderFacts struct {
	Plan                  model.MembershipPlan
	ActualPriceCents      int64
	OriginalPriceCents    int64
	TotalCreditsPerPeriod int64
}

func checkedInt64Product(left int64, right int64, field string) (int64, error) {
	if left < 0 || right < 0 {
		return 0, fmt.Errorf("%s不能为负数", field)
	}
	if left != 0 && right > math.MaxInt64/left {
		return 0, fmt.Errorf("%s超出 int64 范围", field)
	}
	return left * right, nil
}

func parseMembershipOrderPlanSnapshot(order *model.MembershipOrder) (*model.MembershipPlan, error) {
	if order == nil {
		return nil, errors.New("会员订单不能为空")
	}
	if strings.TrimSpace(order.PlanSnapshotJSON) == "" {
		return nil, fmt.Errorf("会员订单 %s 缺少套餐快照，禁止使用冻结权益", order.ID)
	}
	var snapshot model.MembershipPlan
	if err := json.Unmarshal([]byte(order.PlanSnapshotJSON), &snapshot); err != nil {
		return nil, fmt.Errorf("会员订单 %s 的套餐快照损坏: %w", order.ID, err)
	}
	if strings.TrimSpace(snapshot.ID) == "" || snapshot.ID != order.PlanID {
		return nil, fmt.Errorf("会员订单 %s 的套餐快照与订单套餐不一致", order.ID)
	}
	return &snapshot, nil
}

// validatedMembershipOrderSnapshot 是收银台展示与履约共同消费的冻结订单事实边界。
func validatedMembershipOrderSnapshot(order *model.MembershipOrder) (*validatedMembershipOrderFacts, error) {
	snapshotValue, err := parseMembershipOrderPlanSnapshot(order)
	if err != nil {
		return nil, err
	}
	snapshot := *snapshotValue
	if strings.TrimSpace(snapshot.Code) == "" || strings.TrimSpace(snapshot.Name) == "" || strings.TrimSpace(snapshot.Tier) == "" {
		return nil, fmt.Errorf("会员订单 %s 的套餐快照缺少展示身份", order.ID)
	}
	if order.Seats < 1 {
		return nil, fmt.Errorf("会员订单 %s 的席位数无效", order.ID)
	}
	switch snapshot.Audience {
	case model.MembershipAudiencePersonal:
		if order.TeamID != "" || order.Seats != 1 {
			return nil, fmt.Errorf("会员订单 %s 的个人套餐受众与席位不一致", order.ID)
		}
	case model.MembershipAudienceTeam:
		if strings.TrimSpace(order.TeamID) == "" || order.Seats < 2 {
			return nil, fmt.Errorf("会员订单 %s 的团队套餐受众与席位不一致", order.ID)
		}
		if snapshot.MinSeats > 0 && order.Seats < snapshot.MinSeats || snapshot.MaxSeats > 0 && order.Seats > snapshot.MaxSeats {
			return nil, fmt.Errorf("会员订单 %s 的席位数超出冻结套餐范围", order.ID)
		}
	default:
		return nil, fmt.Errorf("会员订单 %s 的套餐快照受众无效", order.ID)
	}
	if snapshot.BillingCycle != model.MembershipBillingCycleMonth && snapshot.BillingCycle != model.MembershipBillingCycleYear {
		return nil, fmt.Errorf("会员订单 %s 的套餐快照计费周期无效", order.ID)
	}
	if snapshot.PriceCents != order.UnitPriceCents || strings.TrimSpace(snapshot.Currency) == "" || snapshot.Currency != order.Currency {
		return nil, fmt.Errorf("会员订单 %s 的套餐快照金额或币种与订单不一致", order.ID)
	}
	if snapshot.OriginalPriceCents < 0 || snapshot.CreditsPerPeriod < 0 || snapshot.ImageConcurrency < 1 || snapshot.VideoConcurrency < 1 {
		return nil, fmt.Errorf("会员订单 %s 的套餐快照权益无效", order.ID)
	}
	actualPriceCents, err := checkedInt64Product(snapshot.PriceCents, int64(order.Seats), "会员订单实付金额")
	if err != nil {
		return nil, fmt.Errorf("会员订单 %s: %w", order.ID, err)
	}
	if actualPriceCents != order.TotalPriceCents {
		return nil, fmt.Errorf("会员订单 %s 的总价与冻结套餐不一致", order.ID)
	}
	originalPriceCents, err := checkedInt64Product(snapshot.OriginalPriceCents, int64(order.Seats), "会员订单原价")
	if err != nil {
		return nil, fmt.Errorf("会员订单 %s: %w", order.ID, err)
	}
	totalCredits, err := checkedInt64Product(snapshot.CreditsPerPeriod, int64(order.Seats), "会员订单周期积分")
	if err != nil {
		return nil, fmt.Errorf("会员订单 %s: %w", order.ID, err)
	}
	return &validatedMembershipOrderFacts{
		Plan: snapshot, ActualPriceCents: actualPriceCents,
		OriginalPriceCents: originalPriceCents, TotalCreditsPerPeriod: totalCredits,
	}, nil
}

func membershipCheckoutSummaryFromOrder(order *model.MembershipOrder) (*MembershipCheckoutSummary, error) {
	facts, err := validatedMembershipOrderSnapshot(order)
	if err != nil {
		return nil, err
	}
	return &MembershipCheckoutSummary{
		Audience: facts.Plan.Audience, Code: facts.Plan.Code, Name: facts.Plan.Name, Tier: facts.Plan.Tier,
		BillingCycle: facts.Plan.BillingCycle, Seats: order.Seats,
		ActualPriceCents: facts.ActualPriceCents, OriginalPriceCents: facts.OriginalPriceCents,
		CreditsPerPeriod: facts.Plan.CreditsPerPeriod, TotalCreditsPerPeriod: facts.TotalCreditsPerPeriod,
	}, nil
}

func creditTopupCheckoutSummaryFromOrder(order *model.CreditTopupOrder) (*CreditTopupCheckoutSummary, error) {
	if order == nil {
		return nil, errors.New("积分充值订单不能为空")
	}
	if order.TotalPriceCents < 0 || order.TotalMicrocredits < 0 || strings.TrimSpace(order.Currency) == "" {
		return nil, fmt.Errorf("积分充值订单 %s 的冻结金额或积分无效", order.ID)
	}
	return &CreditTopupCheckoutSummary{ActualPriceCents: order.TotalPriceCents, TotalMicrocredits: order.TotalMicrocredits}, nil
}

func paymentCheckoutTransactionView(transaction *model.PaymentTransaction) (*PaymentCheckoutTransactionView, error) {
	if transaction == nil || transaction.Status != model.PaymentTransactionPending || strings.TrimSpace(transaction.CodeURL) == "" || transaction.ExpiresAt == nil {
		return nil, errors.New("活动支付交易事实不完整")
	}
	return &PaymentCheckoutTransactionView{
		Provider: transaction.Provider, Status: transaction.Status, CodeURL: transaction.CodeURL, ExpiresAt: *transaction.ExpiresAt,
	}, nil
}

func (s *Service) buildPaymentCheckoutView(session *model.PaymentCheckoutSession, order *paymentOrderDetails, setting paymentSettingValue, now time.Time) (*PaymentCheckoutView, error) {
	view := &PaymentCheckoutView{
		OrderType: session.OrderType, OrderNumber: order.OrderNumber, OrderStatus: order.Status,
		CheckoutStatus: session.Status, Currency: order.Currency, ServerNow: now, ExpiresAt: session.ExpiresAt,
		Providers: readyPaymentProviders(setting),
	}
	switch session.OrderType {
	case model.PaymentOrderMembership:
		summary, err := membershipCheckoutSummaryFromOrder(order.MembershipOrder)
		if err != nil {
			return nil, err
		}
		view.MembershipSummary = summary
	case model.PaymentOrderCreditTopup:
		summary, err := creditTopupCheckoutSummaryFromOrder(order.CreditTopupOrder)
		if err != nil {
			return nil, err
		}
		view.CreditTopupSummary = summary
	default:
		return nil, errors.New("收银台订单类型无效")
	}
	transaction, err := s.repo.ActivePaymentTransaction(session.OrderType, order.ID, now)
	if err == nil {
		view.ActiveTransaction, err = paymentCheckoutTransactionView(transaction)
		if err != nil {
			return nil, err
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return view, nil
}
