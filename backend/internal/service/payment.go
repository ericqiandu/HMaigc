package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

const paymentSettingKey = "payment"
const paymentCheckoutLifetime = 15 * time.Minute
const paymentCheckoutCipherPrefix = "enc:v1:"

type PaymentChannelSettingRequest struct {
	Enabled            bool   `json:"enabled"`
	AppID              string `json:"appId"`
	MerchantID         string `json:"merchantId"`
	MerchantSerialNo   string `json:"merchantSerialNo"`
	MerchantPrivateKey string `json:"merchantPrivateKey"`
	PlatformPublicKey  string `json:"platformPublicKey"`
	APIv3Key           string `json:"apiV3Key"`
	NotifyURL          string `json:"notifyUrl"`
	GatewayURL         string `json:"gatewayUrl"`
}

type PaymentSettingRequest struct {
	CheckoutBaseURL string                       `json:"checkoutBaseUrl"`
	Wechat          PaymentChannelSettingRequest `json:"wechat"`
	Alipay          PaymentChannelSettingRequest `json:"alipay"`
}

type PublicPaymentChannelSetting struct {
	Enabled               bool   `json:"enabled"`
	AppID                 string `json:"appId"`
	MerchantID            string `json:"merchantId"`
	MerchantSerialNo      string `json:"merchantSerialNo"`
	NotifyURL             string `json:"notifyUrl"`
	GatewayURL            string `json:"gatewayUrl"`
	HasMerchantPrivateKey bool   `json:"hasMerchantPrivateKey"`
	HasPlatformPublicKey  bool   `json:"hasPlatformPublicKey"`
	HasAPIv3Key           bool   `json:"hasApiV3Key"`
	Ready                 bool   `json:"ready"`
}

type PublicPaymentSetting struct {
	CheckoutBaseURL string                      `json:"checkoutBaseUrl"`
	Wechat          PublicPaymentChannelSetting `json:"wechat"`
	Alipay          PublicPaymentChannelSetting `json:"alipay"`
	UpdatedBy       string                      `json:"updatedBy"`
	CreatedAt       time.Time                   `json:"createdAt"`
	UpdatedAt       time.Time                   `json:"updatedAt"`
}

type paymentOrderDetails struct {
	ID               string
	UserID           string
	OrderNumber      string
	AmountCents      int64
	Currency         string
	Status           string
	Description      string
	MembershipOrder  *model.MembershipOrder
	CreditTopupOrder *model.CreditTopupOrder
}

type CreatePaymentCheckoutResult struct {
	CheckoutURL string    `json:"checkoutUrl"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type paymentCheckoutCipherPayload struct {
	Token       string `json:"token"`
	CheckoutURL string `json:"checkoutUrl"`
}

type CreatePaymentTransactionRequest struct {
	Provider model.PaymentProvider `json:"provider"`
}

type AdminPaymentTransactionPage struct {
	Items []model.PaymentTransaction `json:"items"`
	Total int64                      `json:"total"`
	Page  int                        `json:"page"`
	Limit int                        `json:"limit"`
}

type AdminPaymentWebhookPage struct {
	Items []model.PaymentWebhookEvent `json:"items"`
	Total int64                       `json:"total"`
	Page  int                         `json:"page"`
	Limit int                         `json:"limit"`
}

type AdminPaymentReconciliationResult struct {
	Transaction   model.PaymentTransaction `json:"transaction"`
	ProviderState providerPaymentState     `json:"providerState"`
}

type paymentReconciliationAudit struct {
	Provider        model.PaymentProvider `json:"provider"`
	MerchantOrderNo string                `json:"merchantOrderNo"`
	ProviderState   providerPaymentState  `json:"providerState,omitempty"`
	Outcome         string                `json:"outcome"`
	FailureCode     string                `json:"failureCode,omitempty"`
}

type paymentReconciliationAccessAudit struct {
	Outcome     string `json:"outcome"`
	FailureCode string `json:"failureCode"`
}

type paymentChannelSettingValue struct {
	Enabled            bool   `json:"enabled"`
	AppID              string `json:"appId"`
	MerchantID         string `json:"merchantId"`
	MerchantSerialNo   string `json:"merchantSerialNo"`
	MerchantPrivateKey string `json:"merchantPrivateKey"`
	PlatformPublicKey  string `json:"platformPublicKey"`
	APIv3Key           string `json:"apiV3Key"`
	NotifyURL          string `json:"notifyUrl"`
	GatewayURL         string `json:"gatewayUrl"`
}

type paymentSettingValue struct {
	CheckoutBaseURL string                     `json:"checkoutBaseUrl"`
	Wechat          paymentChannelSettingValue `json:"wechat"`
	Alipay          paymentChannelSettingValue `json:"alipay"`
}

func (s *Service) AdminPaymentSetting(actor *model.User) (*PublicPaymentSetting, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	setting, value, err := s.readPaymentSetting()
	if err != nil {
		return nil, err
	}
	result := publicPaymentSetting(setting, value)
	return &result, nil
}

func (s *Service) UpdatePaymentSetting(actor *model.User, req PaymentSettingRequest) (*PublicPaymentSetting, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	currentSetting, current, err := s.readPaymentSetting()
	if err != nil {
		return nil, err
	}
	next := paymentSettingValue{
		CheckoutBaseURL: strings.TrimRight(strings.TrimSpace(req.CheckoutBaseURL), "/"),
		Wechat:          mergePaymentChannel(req.Wechat, current.Wechat),
		Alipay:          mergePaymentChannel(req.Alipay, current.Alipay),
	}
	if err := validatePaymentSetting(next); err != nil {
		return nil, BadAuthRequest(err.Error())
	}
	stored := next
	if err := s.encryptPaymentSecrets(&stored); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		return nil, err
	}
	setting := model.SystemSetting{Key: paymentSettingKey, ValueJSON: string(encoded), UpdatedBy: actor.ID}
	if currentSetting != nil {
		setting.CreatedAt = currentSetting.CreatedAt
	}
	if err := s.repo.SaveSystemSetting(&setting); err != nil {
		return nil, err
	}
	if err := s.appendAdminAudit(actor, "payment_setting.update", "system_setting", paymentSettingKey, "更新支付渠道配置", publicPaymentSetting(&setting, next)); err != nil {
		return nil, err
	}
	result := publicPaymentSetting(&setting, next)
	return &result, nil
}

func (s *Service) AdminPaymentTransactions(actor *model.User, query AdminListQuery) (*AdminPaymentTransactionPage, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	provider, err := parsePaymentProvider(query.Type)
	if err != nil {
		return nil, err
	}
	status, err := parsePaymentTransactionStatus(query.Status)
	if err != nil {
		return nil, err
	}
	page, limit := normalizeAdminPage(query.Page, query.Limit)
	items, total, err := s.repo.AdminPaymentTransactions(repository.PaymentTransactionFilter{
		Keyword: query.Keyword, Provider: provider, Status: status, Limit: limit, Offset: (page - 1) * limit,
	})
	if err != nil {
		return nil, err
	}
	return &AdminPaymentTransactionPage{Items: items, Total: total, Page: page, Limit: limit}, nil
}

func (s *Service) AdminPaymentWebhookEvents(actor *model.User, query AdminListQuery) (*AdminPaymentWebhookPage, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	provider, err := parsePaymentProvider(query.Type)
	if err != nil {
		return nil, err
	}
	status, err := parsePaymentWebhookStatus(query.Status)
	if err != nil {
		return nil, err
	}
	page, limit := normalizeAdminPage(query.Page, query.Limit)
	items, total, err := s.repo.AdminPaymentWebhookEvents(repository.PaymentWebhookFilter{
		Provider: provider, Status: status, Limit: limit, Offset: (page - 1) * limit,
	})
	if err != nil {
		return nil, err
	}
	return &AdminPaymentWebhookPage{Items: items, Total: total, Page: page, Limit: limit}, nil
}

func (s *Service) AdminReconcilePaymentTransaction(actor *model.User, id string) (*AdminPaymentReconciliationResult, error) {
	if err := s.RequireAdmin(actor); err != nil {
		if actor != nil && strings.TrimSpace(actor.ID) != "" {
			targetID := paymentReconciliationAuditTargetID(id)
			if auditErr := s.appendAdminAudit(actor, "payment_transaction.reconcile_rejected", "payment_transaction", targetID, "拒绝无管理员权限的支付对账请求", paymentReconciliationAccessAudit{
				Outcome: "rejected", FailureCode: "admin_required",
			}); auditErr != nil {
				return nil, fmt.Errorf("支付对账拒绝审计写入失败: %w", auditErr)
			}
		}
		return nil, err
	}
	transactionID := strings.TrimSpace(id)
	transaction, err := s.repo.PaymentTransaction(transactionID)
	if err != nil {
		failureCode := "transaction_lookup_failed"
		if errors.Is(err, gorm.ErrRecordNotFound) {
			failureCode = "transaction_not_found"
		}
		if auditErr := s.appendAdminAudit(actor, "payment_transaction.reconcile_failed", "payment_transaction", paymentReconciliationAuditTargetID(transactionID), "支付对账目标交易查询失败", paymentReconciliationAccessAudit{
			Outcome: "failed", FailureCode: failureCode,
		}); auditErr != nil {
			return nil, fmt.Errorf("支付对账目标查询失败且审计写入失败: %v: %w", auditErr, err)
		}
		return nil, err
	}
	attempt := paymentReconciliationAudit{
		Provider: transaction.Provider, MerchantOrderNo: transaction.MerchantOrderNo, Outcome: "attempted",
	}
	if err := s.appendAdminAudit(actor, "payment_transaction.reconcile_attempt", "payment_transaction", transaction.ID, "发起支付渠道查单对账", attempt); err != nil {
		return nil, err
	}
	_, setting, err := s.readPaymentSetting()
	if err != nil {
		return nil, s.failPaymentReconciliation(actor, transaction, providerPaymentUnknown, "payment_setting_unavailable", err)
	}
	channel, ok := paymentChannel(setting, transaction.Provider)
	if !ok || !paymentChannelReady(transaction.Provider, channel) {
		return nil, s.failPaymentReconciliation(actor, transaction, providerPaymentUnknown, "payment_channel_unavailable", errors.New("支付渠道尚未完整配置"))
	}
	fact, err := queryProviderPayment(transaction, channel)
	if err != nil {
		return nil, s.failPaymentReconciliation(actor, transaction, providerPaymentUnknown, "provider_query_failed", err)
	}
	switch fact.State {
	case providerPaymentPaid:
		return s.reconcileConfirmedPayment(actor, transaction, fact)
	case providerPaymentUnpaid, providerPaymentNotFound:
		if err := closeProviderPayment(transaction, channel); err != nil {
			return nil, s.failPaymentReconciliation(actor, transaction, fact.State, "provider_close_failed", err)
		}
		now := time.Now()
		outcome := paymentReconciliationAudit{
			Provider: transaction.Provider, MerchantOrderNo: transaction.MerchantOrderNo,
			ProviderState: fact.State, Outcome: "provider_closed",
		}
		audit, err := newAdminAuditEvent(actor, "payment_transaction.reconcile_closed", "payment_transaction", transaction.ID, "支付渠道确认未到账并完成远端关单", outcome)
		if err != nil {
			return nil, err
		}
		if err := s.repo.ClosePaymentTransactionAfterProviderConfirmation(transaction.ID, now, audit); err != nil {
			if auditErr := s.appendAdminAudit(actor, "payment_transaction.reconcile_failed", "payment_transaction", transaction.ID, "远端关单成功但本地状态已变化", paymentReconciliationAudit{
				Provider: transaction.Provider, MerchantOrderNo: transaction.MerchantOrderNo, ProviderState: fact.State,
				Outcome: "local_close_failed", FailureCode: "local_state_changed",
			}); auditErr != nil {
				return nil, fmt.Errorf("本地关单失败且失败审计写入失败: %v: %w", auditErr, err)
			}
			return nil, err
		}
	case providerPaymentUnknown:
		return nil, s.failPaymentReconciliation(actor, transaction, fact.State, "provider_state_unknown", errors.New("支付渠道返回无法确认的交易状态"))
	default:
		return nil, s.failPaymentReconciliation(actor, transaction, providerPaymentUnknown, "provider_state_invalid", errors.New("支付渠道返回无效交易状态"))
	}
	updated, err := s.repo.PaymentTransaction(transaction.ID)
	if err != nil {
		return nil, err
	}
	return &AdminPaymentReconciliationResult{Transaction: *updated, ProviderState: fact.State}, nil
}

func paymentReconciliationAuditTargetID(id string) string {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return "unknown"
	}
	return truncateRunes(trimmed, 160)
}

func (s *Service) reconcileConfirmedPayment(actor *model.User, transaction *model.PaymentTransaction, fact providerPaymentFact) (*AdminPaymentReconciliationResult, error) {
	now := time.Now()
	normalized := strings.Join([]string{
		string(transaction.Provider), transaction.MerchantOrderNo, fact.ProviderTradeNo,
		strconv.FormatInt(fact.AmountCents, 10), fact.Currency, fact.PaidAt.UTC().Format(time.RFC3339Nano),
	}, "\x00")
	digest := sha256.Sum256([]byte(normalized))
	event := &model.PaymentWebhookEvent{
		ID: newID(), Provider: transaction.Provider,
		ProviderEventID: "reconcile:" + hex.EncodeToString(digest[:]), TransactionID: transaction.ID,
		MerchantOrderNo: transaction.MerchantOrderNo, ProviderTradeNo: fact.ProviderTradeNo,
		AmountCents: fact.AmountCents, Currency: fact.Currency, PaidAt: &fact.PaidAt,
		PayloadDigest: hex.EncodeToString(digest[:]), Status: model.PaymentWebhookReceived,
		ReceivedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	stored, _, err := s.repo.RecordPaymentWebhookEvent(event)
	if err != nil {
		return nil, s.failPaymentReconciliation(actor, transaction, providerPaymentPaid, "verified_fact_conflict", err)
	}
	event = stored
	if event.Status == model.PaymentWebhookReviewRequired && event.FailureCode == "provider_trade_conflict" {
		return nil, s.failPaymentReconciliation(actor, transaction, providerPaymentPaid, "provider_trade_conflict", errors.New("渠道交易号已绑定其他支付交易，必须人工复核"))
	}
	if fact.ProviderTradeNo == "" || fact.AmountCents != transaction.AmountCents || fact.Currency != transaction.Currency || fact.PaidAt.IsZero() {
		if markErr := s.repo.MarkPaymentWebhookOutcome(event.ID, model.PaymentWebhookReviewRequired, "query_fact_mismatch", "渠道查单到账事实与本地交易不一致", now); markErr != nil {
			return nil, markErr
		}
		return nil, s.failPaymentReconciliation(actor, transaction, providerPaymentPaid, "query_fact_mismatch", errors.New("支付渠道查单到账事实与本地交易不一致"))
	}
	var activation repository.MembershipActivation
	if transaction.OrderType == model.PaymentOrderMembership {
		order, lookupErr := s.repo.MembershipOrder(transaction.OrderID)
		if lookupErr != nil {
			return nil, s.failReconciliationFulfillment(actor, transaction, event, "membership_order_lookup_failed", lookupErr)
		}
		activation, err = s.membershipFulfillmentForOrder(order, model.SystemActorID, fact.PaidAt)
		if err != nil {
			return nil, s.failReconciliationFulfillment(actor, transaction, event, "membership_activation_invalid", err)
		}
	} else if transaction.OrderType != model.PaymentOrderCreditTopup {
		return nil, s.failReconciliationFulfillment(actor, transaction, event, "unsupported_order_type", errors.New("支付交易订单类型无效"))
	}
	outcome := paymentReconciliationAudit{
		Provider: transaction.Provider, MerchantOrderNo: transaction.MerchantOrderNo,
		ProviderState: providerPaymentPaid, Outcome: "fulfilled",
	}
	audit, err := newAdminAuditEvent(actor, "payment_transaction.reconcile_paid", "payment_transaction", transaction.ID, "支付渠道确认到账并完成履约", outcome)
	if err != nil {
		return nil, err
	}
	if _, err := s.repo.FulfillPaymentTransaction(repository.PaymentFulfillment{
		Event: event, TransactionID: transaction.ID, ProviderTradeNo: fact.ProviderTradeNo,
		PaidAt: fact.PaidAt, PaymentConfirmed: true, Activation: activation, Audit: audit,
	}); err != nil {
		return nil, s.failReconciliationFulfillment(actor, transaction, event, "fulfillment_failed", err)
	}
	updated, err := s.repo.PaymentTransaction(transaction.ID)
	if err != nil {
		return nil, err
	}
	return &AdminPaymentReconciliationResult{Transaction: *updated, ProviderState: providerPaymentPaid}, nil
}

func (s *Service) failReconciliationFulfillment(actor *model.User, transaction *model.PaymentTransaction, event *model.PaymentWebhookEvent, code string, cause error) error {
	if err := s.repo.MarkPaymentWebhookOutcome(event.ID, model.PaymentWebhookReviewRequired, code, truncateRunes(cause.Error(), 500), time.Now()); err != nil {
		return fmt.Errorf("对账履约失败且验签事实结果更新失败: %v: %w", err, cause)
	}
	return s.failPaymentReconciliation(actor, transaction, providerPaymentPaid, code, cause)
}

func (s *Service) failPaymentReconciliation(actor *model.User, transaction *model.PaymentTransaction, state providerPaymentState, code string, cause error) error {
	outcome := paymentReconciliationAudit{
		Provider: transaction.Provider, MerchantOrderNo: transaction.MerchantOrderNo,
		ProviderState: state, Outcome: "review_required", FailureCode: code,
	}
	audit, err := newAdminAuditEvent(actor, "payment_transaction.reconcile_failed", "payment_transaction", transaction.ID, "支付对账未能确认终态，保留人工复核", outcome)
	if err != nil {
		return err
	}
	if err := s.repo.MarkPaymentTransactionReviewWithAudit(transaction.ID, code, truncateRunes(cause.Error(), 500), time.Now(), audit); err != nil {
		return fmt.Errorf("支付对账失败且结果审计写入失败: %v: %w", err, cause)
	}
	return &AuthError{Status: http.StatusConflict, Message: "支付渠道状态仍需人工复核"}
}

func (s *Service) CreatePaymentCheckout(user *model.User, orderID string) (*CreatePaymentCheckoutResult, error) {
	if user == nil {
		return nil, Unauthorized("请先登录")
	}
	now := time.Now()
	if err := s.reconcileMembershipLifecycle(now); err != nil {
		return nil, err
	}
	order, err := s.repo.MembershipOrderForUser(user.ID, strings.TrimSpace(orderID))
	if err != nil {
		return nil, err
	}
	if order.Status != model.MembershipOrderPending {
		return nil, BadAuthRequest("只有待支付订单可以创建收银台")
	}
	existing, hasExistingSession, err := s.recoverExistingPaymentCheckout(
		model.PaymentOrderMembership, order.ID, user.ID,
	)
	if err != nil {
		return nil, err
	}
	if hasExistingSession {
		return existing, nil
	}
	_, setting, err := s.readPaymentSetting()
	if err != nil {
		return nil, err
	}
	if setting.CheckoutBaseURL == "" {
		return nil, BadAuthRequest("支付收银台地址尚未配置")
	}
	if len(readyPaymentProviders(setting)) == 0 {
		return nil, BadAuthRequest("微信支付和支付宝均未完成商户配置")
	}
	return s.createOrRecoverPaymentCheckout(
		model.PaymentOrderMembership, order.ID, user.ID, setting.CheckoutBaseURL, now,
	)
}

func (s *Service) CreateCreditTopupCheckout(user *model.User, orderID string) (*CreatePaymentCheckoutResult, error) {
	if user == nil {
		return nil, Unauthorized("请先登录")
	}
	order, err := s.repo.CreditTopupOrderForUser(user.ID, strings.TrimSpace(orderID))
	if err != nil {
		return nil, err
	}
	if order.Status != model.CreditTopupOrderPending {
		return nil, BadAuthRequest("只有待支付积分订单可以创建收银台")
	}
	existing, hasExistingSession, err := s.recoverExistingPaymentCheckout(
		model.PaymentOrderCreditTopup, order.ID, user.ID,
	)
	if err != nil {
		return nil, err
	}
	if hasExistingSession {
		return existing, nil
	}
	_, setting, err := s.readPaymentSetting()
	if err != nil {
		return nil, err
	}
	if setting.CheckoutBaseURL == "" || len(readyPaymentProviders(setting)) == 0 {
		return nil, BadAuthRequest("支付收银台或商户渠道尚未完成配置")
	}
	now := time.Now()
	return s.createOrRecoverPaymentCheckout(
		model.PaymentOrderCreditTopup, order.ID, user.ID, setting.CheckoutBaseURL, now,
	)
}

func (s *Service) PaymentCheckout(token string) (*PaymentCheckoutView, error) {
	now := time.Now()
	session, order, setting, err := s.paymentCheckoutViewContext(token, now)
	if err != nil {
		return nil, err
	}
	return s.buildPaymentCheckoutView(session, order, setting, now)
}

func (s *Service) createOrRecoverPaymentCheckout(orderType model.PaymentOrderType, orderID string, userID string, checkoutBaseURL string, now time.Time) (*CreatePaymentCheckoutResult, error) {
	token, tokenHash, err := newPaymentCheckoutToken()
	if err != nil {
		return nil, err
	}
	candidate := &model.PaymentCheckoutSession{
		ID: newID(), OrderType: orderType, OrderID: orderID, UserID: userID, TokenHash: tokenHash,
		Status: model.PaymentCheckoutActive, ExpiresAt: now.Add(paymentCheckoutLifetime), CreatedAt: now, UpdatedAt: now,
	}
	payload := paymentCheckoutCipherPayload{
		Token: token, CheckoutURL: strings.TrimRight(checkoutBaseURL, "/") + "/pay/" + token,
	}
	candidate.TokenCipher, err = s.encryptPaymentCheckoutToken(candidate, payload)
	if err != nil {
		return nil, err
	}
	winner, err := s.repo.CreateOrGetPaymentCheckoutSession(candidate)
	if err != nil {
		return nil, err
	}
	winnerPayload, err := s.recoverPaymentCheckoutPayload(winner, orderType, orderID, userID)
	if err != nil {
		return nil, err
	}
	return paymentCheckoutResult(winner, winnerPayload), nil
}

func (s *Service) recoverExistingPaymentCheckout(orderType model.PaymentOrderType, orderID string, userID string) (*CreatePaymentCheckoutResult, bool, error) {
	session, err := s.repo.PaymentCheckoutSessionByOrderID(orderID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	payload, err := s.recoverPaymentCheckoutPayload(session, orderType, orderID, userID)
	if err != nil {
		return nil, false, err
	}
	return paymentCheckoutResult(session, payload), true, nil
}

func (s *Service) recoverPaymentCheckoutPayload(session *model.PaymentCheckoutSession, orderType model.PaymentOrderType, orderID string, userID string) (*paymentCheckoutCipherPayload, error) {
	if session == nil || session.OrderType != orderType || session.OrderID != orderID || session.UserID != userID {
		return nil, errors.New("已有收银台会话与订单事实不一致")
	}
	if strings.TrimSpace(session.TokenCipher) == "" {
		return nil, errors.New("已有收银台会话仅保存令牌哈希，无法恢复所有者支付链接")
	}
	payload, err := s.decryptPaymentCheckoutToken(session)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func paymentCheckoutResult(session *model.PaymentCheckoutSession, payload *paymentCheckoutCipherPayload) *CreatePaymentCheckoutResult {
	return &CreatePaymentCheckoutResult{
		CheckoutURL: payload.CheckoutURL,
		ExpiresAt:   session.ExpiresAt,
	}
}

func (s *Service) CreatePaymentTransaction(token string, req CreatePaymentTransactionRequest) (*PaymentCheckoutTransactionView, error) {
	session, order, setting, err := s.payableCheckoutContext(token)
	if err != nil {
		return nil, err
	}
	channel, ok := paymentChannel(setting, req.Provider)
	if !ok || !paymentChannelReady(req.Provider, channel) {
		return nil, BadAuthRequest("所选支付渠道尚未完成商户配置")
	}
	now := time.Now()
	expiresAt := session.ExpiresAt
	transaction := &model.PaymentTransaction{
		ID: newID(), OrderType: session.OrderType, OrderID: order.ID, UserID: order.UserID, Provider: req.Provider,
		MerchantOrderNo: paymentMerchantOrderNo(order.OrderNumber, now), AmountCents: order.AmountCents,
		Currency: order.Currency, Status: model.PaymentTransactionCreated, ExpiresAt: &expiresAt,
		CreatedAt: now, UpdatedAt: now,
	}
	winner, claimed, err := s.repo.ClaimPayablePaymentTransaction(transaction)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrPaymentChannelLocked):
			return nil, &AuthError{Status: 409, Message: "订单已有其他支付渠道交易，需先完成支付对账"}
		case errors.Is(err, repository.ErrPaymentVerifiedFactExists), errors.Is(err, repository.ErrPaymentOrderNotPayable):
			return nil, &AuthError{Status: 409, Message: "订单当前不可创建新的支付交易"}
		default:
			return nil, err
		}
	}
	if !claimed {
		if winner.Status == model.PaymentTransactionPending {
			return paymentCheckoutTransactionView(winner)
		}
		return nil, &AuthError{Status: 409, Message: "支付交易结果待渠道对账，请勿重复创建"}
	}
	codeURL, err := createProviderPayment(transaction, paymentOrderReference{OrderNumber: order.OrderNumber, Description: order.Description}, channel)
	if err != nil {
		status := model.PaymentTransactionReviewRequired
		failureCode := "provider_result_unknown"
		if isDeterministicProviderRejection(err) {
			status = model.PaymentTransactionFailed
			failureCode = "provider_rejected"
		}
		if saveErr := s.repo.UpdatePaymentTransactionCreation(transaction.ID, status, "", failureCode, truncateRunes(err.Error(), 500), time.Now()); saveErr != nil {
			return nil, fmt.Errorf("渠道下单失败且交易审计写入失败: %v: %w", saveErr, err)
		}
		return nil, &AuthError{Status: 502, Message: "支付渠道暂时无法创建订单，请稍后重试"}
	}
	transaction.Status = model.PaymentTransactionPending
	transaction.CodeURL = codeURL
	transaction.UpdatedAt = time.Now()
	if err := s.repo.UpdatePaymentTransactionCreation(transaction.ID, transaction.Status, codeURL, "", "", transaction.UpdatedAt); err != nil {
		return nil, err
	}
	return paymentCheckoutTransactionView(transaction)
}

func (s *Service) paymentCheckoutByToken(token string) (*model.PaymentCheckoutSession, *paymentOrderDetails, error) {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return nil, nil, BadAuthRequest("收银台令牌不能为空")
	}
	digest := sha256.Sum256([]byte(trimmed))
	session, err := s.repo.PaymentCheckoutSessionByTokenHash(hex.EncodeToString(digest[:]))
	if err != nil {
		return nil, nil, err
	}
	var order *paymentOrderDetails
	switch session.OrderType {
	case model.PaymentOrderMembership:
		membershipOrder, lookupErr := s.repo.MembershipOrder(session.OrderID)
		if lookupErr != nil {
			return nil, nil, lookupErr
		}
		order = &paymentOrderDetails{
			ID: membershipOrder.ID, UserID: membershipOrder.UserID, OrderNumber: membershipOrder.OrderNumber,
			AmountCents: membershipOrder.TotalPriceCents, Currency: membershipOrder.Currency, Status: string(membershipOrder.Status),
			Description: "会员订单 " + membershipOrder.OrderNumber, MembershipOrder: membershipOrder,
		}
	case model.PaymentOrderCreditTopup:
		topupOrder, lookupErr := s.repo.CreditTopupOrder(session.OrderID)
		if lookupErr != nil {
			return nil, nil, lookupErr
		}
		order = &paymentOrderDetails{
			ID: topupOrder.ID, UserID: topupOrder.UserID, OrderNumber: topupOrder.OrderNumber,
			AmountCents: topupOrder.TotalPriceCents, Currency: topupOrder.Currency, Status: string(topupOrder.Status),
			Description: "积分充值订单 " + topupOrder.OrderNumber, CreditTopupOrder: topupOrder,
		}
	default:
		return nil, nil, errors.New("收银台订单类型无效")
	}
	if session.UserID != order.UserID {
		return nil, nil, errors.New("收银台会话与订单所有者不一致")
	}
	return session, order, nil
}

func (s *Service) paymentCheckoutViewContext(token string, now time.Time) (*model.PaymentCheckoutSession, *paymentOrderDetails, paymentSettingValue, error) {
	session, order, err := s.paymentCheckoutByToken(token)
	if err != nil {
		return nil, nil, paymentSettingValue{}, err
	}
	if session.Status == model.PaymentCheckoutActive && !session.ExpiresAt.After(now) {
		expired, expireErr := s.repo.ExpirePaymentCheckoutSession(session.ID, now)
		if expireErr != nil {
			return nil, nil, paymentSettingValue{}, expireErr
		}
		if expired {
			session.Status = model.PaymentCheckoutExpired
		} else {
			session, order, err = s.paymentCheckoutByToken(token)
			if err != nil {
				return nil, nil, paymentSettingValue{}, err
			}
		}
	}
	var setting paymentSettingValue
	if session.Status == model.PaymentCheckoutActive && order.Status == "pending" {
		_, setting, err = s.readPaymentProjectionSetting()
		if err != nil {
			return nil, nil, paymentSettingValue{}, err
		}
	}
	return session, order, setting, nil
}

func (s *Service) payableCheckoutContext(token string) (*model.PaymentCheckoutSession, *paymentOrderDetails, paymentSettingValue, error) {
	session, order, err := s.paymentCheckoutByToken(token)
	if err != nil {
		return nil, nil, paymentSettingValue{}, err
	}
	if err := s.requireActiveCheckout(session); err != nil {
		return nil, nil, paymentSettingValue{}, err
	}
	if order.Status != "pending" {
		return nil, nil, paymentSettingValue{}, BadAuthRequest("订单当前不可支付")
	}
	_, setting, err := s.readPaymentSetting()
	if err != nil {
		return nil, nil, paymentSettingValue{}, err
	}
	return session, order, setting, nil
}

func (s *Service) requireActiveCheckout(session *model.PaymentCheckoutSession) error {
	now := time.Now()
	if session.Status == model.PaymentCheckoutActive && session.ExpiresAt.After(now) {
		return nil
	}
	if session.Status == model.PaymentCheckoutActive {
		if _, err := s.repo.ExpirePaymentCheckoutSession(session.ID, now); err != nil {
			return err
		}
	}
	return BadAuthRequest("收银台已失效，请重新发起支付")
}

func (s *Service) readPaymentSetting() (*model.SystemSetting, paymentSettingValue, error) {
	setting, value, err := s.readPaymentProjectionSetting()
	if err != nil {
		return nil, paymentSettingValue{}, err
	}
	for _, secret := range []*string{
		&value.Wechat.MerchantPrivateKey, &value.Wechat.PlatformPublicKey, &value.Wechat.APIv3Key,
		&value.Alipay.MerchantPrivateKey, &value.Alipay.PlatformPublicKey, &value.Alipay.APIv3Key,
	} {
		plain, decryptErr := s.decryptSettingSecret(*secret)
		if decryptErr != nil {
			return nil, paymentSettingValue{}, decryptErr
		}
		*secret = plain
	}
	return setting, value, nil
}

func (s *Service) readPaymentProjectionSetting() (*model.SystemSetting, paymentSettingValue, error) {
	setting, err := s.repo.SystemSetting(paymentSettingKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, paymentSettingValue{}, nil
	}
	if err != nil {
		return nil, paymentSettingValue{}, err
	}
	var value paymentSettingValue
	if err := json.Unmarshal([]byte(setting.ValueJSON), &value); err != nil {
		return nil, paymentSettingValue{}, errors.New("支付配置格式无效")
	}
	return setting, value, nil
}

func (s *Service) encryptPaymentSecrets(value *paymentSettingValue) error {
	for _, secret := range []*string{
		&value.Wechat.MerchantPrivateKey, &value.Wechat.PlatformPublicKey, &value.Wechat.APIv3Key,
		&value.Alipay.MerchantPrivateKey, &value.Alipay.PlatformPublicKey, &value.Alipay.APIv3Key,
	} {
		encrypted, err := s.encryptSettingSecret(*secret)
		if err != nil {
			return err
		}
		*secret = encrypted
	}
	return nil
}

func mergePaymentChannel(req PaymentChannelSettingRequest, current paymentChannelSettingValue) paymentChannelSettingValue {
	next := paymentChannelSettingValue{
		Enabled: req.Enabled, AppID: strings.TrimSpace(req.AppID), MerchantID: strings.TrimSpace(req.MerchantID),
		MerchantSerialNo: strings.TrimSpace(req.MerchantSerialNo), MerchantPrivateKey: strings.TrimSpace(req.MerchantPrivateKey),
		PlatformPublicKey: strings.TrimSpace(req.PlatformPublicKey), APIv3Key: strings.TrimSpace(req.APIv3Key),
		NotifyURL: strings.TrimSpace(req.NotifyURL), GatewayURL: strings.TrimSpace(req.GatewayURL),
	}
	if next.MerchantPrivateKey == "" {
		next.MerchantPrivateKey = current.MerchantPrivateKey
	}
	if next.PlatformPublicKey == "" {
		next.PlatformPublicKey = current.PlatformPublicKey
	}
	if next.APIv3Key == "" {
		next.APIv3Key = current.APIv3Key
	}
	return next
}

func validatePaymentSetting(value paymentSettingValue) error {
	if value.CheckoutBaseURL != "" {
		if err := validateHTTPURL(value.CheckoutBaseURL, "收银台地址"); err != nil {
			return err
		}
	}
	for _, item := range []struct {
		name     string
		provider model.PaymentProvider
		channel  paymentChannelSettingValue
	}{
		{name: "微信支付", provider: model.PaymentProviderWechat, channel: value.Wechat},
		{name: "支付宝", provider: model.PaymentProviderAlipay, channel: value.Alipay},
	} {
		name, channel := item.name, item.channel
		if channel.NotifyURL != "" {
			if err := validateHTTPURL(channel.NotifyURL, name+"回调地址"); err != nil {
				return err
			}
		}
		if channel.GatewayURL != "" {
			if err := validatePaymentGatewayURL(item.provider, channel.GatewayURL); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateHTTPURL(raw string, field string) error {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("%s必须是完整的 HTTP 或 HTTPS 地址", field)
	}
	return nil
}

func validatePaymentGatewayURL(provider model.PaymentProvider, raw string) error {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("%s网关地址必须是完整的 HTTPS 地址", provider)
	}
	host := strings.ToLower(parsed.Hostname())
	allowed := false
	switch provider {
	case model.PaymentProviderWechat:
		allowed = host == "api.mch.weixin.qq.com"
	case model.PaymentProviderAlipay:
		allowed = host == "openapi.alipay.com" || host == "openapi-sandbox.dl.alipaydev.com"
	default:
		return fmt.Errorf("不支持的支付渠道 %q", provider)
	}
	if !allowed {
		return fmt.Errorf("%s网关域名 %q 不在官方白名单内", provider, host)
	}
	return nil
}

func parsePaymentProvider(value string) (model.PaymentProvider, error) {
	provider := model.PaymentProvider(strings.TrimSpace(value))
	switch provider {
	case "", model.PaymentProviderWechat, model.PaymentProviderAlipay:
		return provider, nil
	default:
		return "", BadAuthRequest("不支持的支付渠道")
	}
}

func parsePaymentTransactionStatus(value string) (model.PaymentTransactionStatus, error) {
	status := model.PaymentTransactionStatus(strings.TrimSpace(value))
	switch status {
	case "",
		model.PaymentTransactionCreated,
		model.PaymentTransactionPending,
		model.PaymentTransactionReviewRequired,
		model.PaymentTransactionPaid,
		model.PaymentTransactionClosed,
		model.PaymentTransactionFailed,
		model.PaymentTransactionRefunded:
		return status, nil
	default:
		return "", BadAuthRequest("不支持的支付交易状态")
	}
}

func parsePaymentWebhookStatus(value string) (model.PaymentWebhookStatus, error) {
	status := model.PaymentWebhookStatus(strings.TrimSpace(value))
	switch status {
	case "", model.PaymentWebhookReceived, model.PaymentWebhookProcessed, model.PaymentWebhookRejected, model.PaymentWebhookReviewRequired:
		return status, nil
	default:
		return "", BadAuthRequest("不支持的支付回调状态")
	}
}

func readyPaymentProviders(value paymentSettingValue) []model.PaymentProvider {
	providers := make([]model.PaymentProvider, 0, 2)
	if paymentChannelReady(model.PaymentProviderWechat, value.Wechat) {
		providers = append(providers, model.PaymentProviderWechat)
	}
	if paymentChannelReady(model.PaymentProviderAlipay, value.Alipay) {
		providers = append(providers, model.PaymentProviderAlipay)
	}
	return providers
}

func paymentChannel(value paymentSettingValue, provider model.PaymentProvider) (paymentChannelSettingValue, bool) {
	switch provider {
	case model.PaymentProviderWechat:
		return value.Wechat, true
	case model.PaymentProviderAlipay:
		return value.Alipay, true
	default:
		return paymentChannelSettingValue{}, false
	}
}

func paymentChannelReady(provider model.PaymentProvider, value paymentChannelSettingValue) bool {
	common := value.Enabled && value.AppID != "" && value.MerchantPrivateKey != "" && value.NotifyURL != "" && value.GatewayURL != ""
	if provider == model.PaymentProviderWechat {
		return common && value.MerchantID != "" && value.MerchantSerialNo != "" && value.PlatformPublicKey != "" && value.APIv3Key != ""
	}
	return common && value.MerchantID != "" && value.PlatformPublicKey != ""
}

func publicPaymentSetting(setting *model.SystemSetting, value paymentSettingValue) PublicPaymentSetting {
	result := PublicPaymentSetting{
		CheckoutBaseURL: value.CheckoutBaseURL,
		Wechat:          publicPaymentChannel(model.PaymentProviderWechat, value.Wechat),
		Alipay:          publicPaymentChannel(model.PaymentProviderAlipay, value.Alipay),
	}
	if setting != nil {
		result.UpdatedBy, result.CreatedAt, result.UpdatedAt = setting.UpdatedBy, setting.CreatedAt, setting.UpdatedAt
	}
	return result
}

func publicPaymentChannel(provider model.PaymentProvider, value paymentChannelSettingValue) PublicPaymentChannelSetting {
	return PublicPaymentChannelSetting{
		Enabled: value.Enabled, AppID: value.AppID, MerchantID: value.MerchantID, MerchantSerialNo: value.MerchantSerialNo,
		NotifyURL: value.NotifyURL, GatewayURL: value.GatewayURL,
		HasMerchantPrivateKey: value.MerchantPrivateKey != "", HasPlatformPublicKey: value.PlatformPublicKey != "",
		HasAPIv3Key: value.APIv3Key != "", Ready: paymentChannelReady(provider, value),
	}
}

func newPaymentCheckoutToken() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(digest[:]), nil
}

func paymentCheckoutCipherAAD(session *model.PaymentCheckoutSession) []byte {
	return []byte(strings.Join([]string{
		"v1", string(session.OrderType), session.OrderID, session.UserID,
	}, "\x00"))
}

func (s *Service) encryptPaymentCheckoutToken(session *model.PaymentCheckoutSession, payload paymentCheckoutCipherPayload) (string, error) {
	plaintext, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	key, err := s.settingsEncryptionKey()
	if err != nil {
		return "", fmt.Errorf("读取收银台令牌加密密钥失败：%w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, paymentCheckoutCipherAAD(session))
	envelope := append(nonce, ciphertext...)
	return paymentCheckoutCipherPrefix + base64.RawStdEncoding.EncodeToString(envelope), nil
}

func (s *Service) decryptPaymentCheckoutToken(session *model.PaymentCheckoutSession) (*paymentCheckoutCipherPayload, error) {
	if session == nil || !strings.HasPrefix(session.TokenCipher, paymentCheckoutCipherPrefix) {
		return nil, errors.New("收银台令牌密文版本无效")
	}
	payload, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(session.TokenCipher, paymentCheckoutCipherPrefix))
	if err != nil {
		return nil, errors.New("收银台令牌密文格式无效")
	}
	keyPath := filepath.Join(s.dataDir, ".settings-key")
	key, err := os.ReadFile(keyPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("收银台令牌加密密钥缺失")
	}
	if err != nil {
		return nil, fmt.Errorf("读取收银台令牌加密密钥失败：%w", err)
	}
	if len(key) != 32 {
		return nil, errors.New("收银台令牌加密密钥长度无效")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(payload) < gcm.NonceSize()+gcm.Overhead() {
		return nil, errors.New("收银台令牌密文长度无效")
	}
	plaintext, err := gcm.Open(nil, payload[:gcm.NonceSize()], payload[gcm.NonceSize():], paymentCheckoutCipherAAD(session))
	if err != nil {
		return nil, errors.New("收银台令牌解密失败，密文或绑定订单事实已损坏")
	}
	var decoded paymentCheckoutCipherPayload
	if err := json.Unmarshal(plaintext, &decoded); err != nil {
		return nil, errors.New("收银台令牌密文载荷无效")
	}
	if strings.TrimSpace(decoded.Token) == "" || strings.TrimSpace(decoded.CheckoutURL) == "" || !strings.HasSuffix(decoded.CheckoutURL, "/pay/"+decoded.Token) {
		return nil, errors.New("收银台令牌密文载荷事实不完整")
	}
	expectedHash, err := hex.DecodeString(session.TokenHash)
	if err != nil || len(expectedHash) != sha256.Size {
		return nil, errors.New("收银台令牌哈希格式无效")
	}
	digest := sha256.Sum256([]byte(decoded.Token))
	if subtle.ConstantTimeCompare(expectedHash, digest[:]) != 1 {
		return nil, errors.New("收银台令牌密文与哈希不一致")
	}
	return &decoded, nil
}

func paymentMerchantOrderNo(orderNumber string, now time.Time) string {
	digest := sha256.Sum256([]byte(orderNumber + now.UTC().Format(time.RFC3339Nano)))
	return "M" + now.UTC().Format("20060102150405") + strings.ToUpper(hex.EncodeToString(digest[:6]))
}
