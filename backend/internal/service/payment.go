package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

const paymentSettingKey = "payment"
const paymentCheckoutLifetime = 15 * time.Minute

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

type PaymentCheckoutView struct {
	OrderID     string                      `json:"orderId"`
	OrderNumber string                      `json:"orderNumber"`
	AmountCents int64                       `json:"amountCents"`
	Currency    string                      `json:"currency"`
	Status      model.MembershipOrderStatus `json:"status"`
	ExpiresAt   time.Time                   `json:"expiresAt"`
	Providers   []model.PaymentProvider     `json:"providers"`
}

type CreatePaymentCheckoutResult struct {
	CheckoutURL string    `json:"checkoutUrl"`
	ExpiresAt   time.Time `json:"expiresAt"`
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
	token, tokenHash, err := newPaymentCheckoutToken()
	if err != nil {
		return nil, err
	}
	session := model.PaymentCheckoutSession{
		ID: newID(), OrderID: order.ID, UserID: user.ID, TokenHash: tokenHash,
		Status: model.PaymentCheckoutActive, ExpiresAt: now.Add(paymentCheckoutLifetime),
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.SavePaymentCheckoutSession(&session); err != nil {
		return nil, err
	}
	return &CreatePaymentCheckoutResult{
		CheckoutURL: setting.CheckoutBaseURL + "/pay/" + token,
		ExpiresAt:   session.ExpiresAt,
	}, nil
}

func (s *Service) PaymentCheckout(token string) (*PaymentCheckoutView, error) {
	session, order, setting, err := s.paymentCheckoutViewContext(token)
	if err != nil {
		return nil, err
	}
	return &PaymentCheckoutView{
		OrderID: order.ID, OrderNumber: order.OrderNumber, AmountCents: order.TotalPriceCents,
		Currency: order.Currency, Status: order.Status, ExpiresAt: session.ExpiresAt,
		Providers: readyPaymentProviders(setting),
	}, nil
}

func (s *Service) CreatePaymentTransaction(token string, req CreatePaymentTransactionRequest) (*model.PaymentTransaction, error) {
	session, order, setting, err := s.payableCheckoutContext(token)
	if err != nil {
		return nil, err
	}
	channel, ok := paymentChannel(setting, req.Provider)
	if !ok || !paymentChannelReady(req.Provider, channel) {
		return nil, BadAuthRequest("所选支付渠道尚未完成商户配置")
	}
	existing, err := s.repo.LatestPaymentTransaction(order.ID, req.Provider)
	if err == nil && existing.Status == model.PaymentTransactionPending && existing.ExpiresAt != nil && existing.ExpiresAt.After(time.Now()) {
		return existing, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	now := time.Now()
	expiresAt := session.ExpiresAt
	transaction := &model.PaymentTransaction{
		ID: newID(), OrderID: order.ID, UserID: order.UserID, Provider: req.Provider,
		MerchantOrderNo: paymentMerchantOrderNo(order.OrderNumber, now), AmountCents: order.TotalPriceCents,
		Currency: order.Currency, Status: model.PaymentTransactionCreated, ExpiresAt: &expiresAt,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.CreatePaymentTransaction(transaction); err != nil {
		return nil, err
	}
	codeURL, err := createProviderPayment(transaction, order, channel)
	if err != nil {
		if saveErr := s.repo.UpdatePaymentTransactionCreation(transaction.ID, model.PaymentTransactionFailed, "", err.Error(), time.Now()); saveErr != nil {
			return nil, fmt.Errorf("渠道下单失败且交易审计写入失败: %v: %w", saveErr, err)
		}
		return nil, &AuthError{Status: 502, Message: "支付渠道下单失败：" + err.Error()}
	}
	transaction.Status = model.PaymentTransactionPending
	transaction.CodeURL = codeURL
	transaction.UpdatedAt = time.Now()
	if err := s.repo.UpdatePaymentTransactionCreation(transaction.ID, transaction.Status, codeURL, "", transaction.UpdatedAt); err != nil {
		return nil, err
	}
	return transaction, nil
}

func (s *Service) paymentCheckoutByToken(token string) (*model.PaymentCheckoutSession, *model.MembershipOrder, paymentSettingValue, error) {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return nil, nil, paymentSettingValue{}, BadAuthRequest("收银台令牌不能为空")
	}
	digest := sha256.Sum256([]byte(trimmed))
	session, err := s.repo.PaymentCheckoutSessionByTokenHash(hex.EncodeToString(digest[:]))
	if err != nil {
		return nil, nil, paymentSettingValue{}, err
	}
	order, err := s.repo.MembershipOrder(session.OrderID)
	if err != nil {
		return nil, nil, paymentSettingValue{}, err
	}
	_, setting, err := s.readPaymentSetting()
	return session, order, setting, err
}

func (s *Service) paymentCheckoutViewContext(token string) (*model.PaymentCheckoutSession, *model.MembershipOrder, paymentSettingValue, error) {
	session, order, setting, err := s.paymentCheckoutByToken(token)
	if err != nil {
		return nil, nil, paymentSettingValue{}, err
	}
	if session.Status == model.PaymentCheckoutConsumed && order.Status == model.MembershipOrderPaid {
		return session, order, setting, nil
	}
	if err := s.requireActiveCheckout(session); err != nil {
		return nil, nil, paymentSettingValue{}, err
	}
	return session, order, setting, nil
}

func (s *Service) payableCheckoutContext(token string) (*model.PaymentCheckoutSession, *model.MembershipOrder, paymentSettingValue, error) {
	session, order, setting, err := s.paymentCheckoutByToken(token)
	if err != nil {
		return nil, nil, paymentSettingValue{}, err
	}
	if err := s.requireActiveCheckout(session); err != nil {
		return nil, nil, paymentSettingValue{}, err
	}
	if order.Status != model.MembershipOrderPending {
		return nil, nil, paymentSettingValue{}, BadAuthRequest("订单当前不可支付")
	}
	return session, order, setting, nil
}

func (s *Service) requireActiveCheckout(session *model.PaymentCheckoutSession) error {
	now := time.Now()
	if session.Status == model.PaymentCheckoutActive && session.ExpiresAt.After(now) {
		return nil
	}
	if session.Status == model.PaymentCheckoutActive {
		if err := s.repo.ExpirePaymentCheckoutSession(session.ID, now); err != nil {
			return err
		}
	}
	return BadAuthRequest("收银台已失效，请重新发起支付")
}

func (s *Service) readPaymentSetting() (*model.SystemSetting, paymentSettingValue, error) {
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
	case "", model.PaymentWebhookReceived, model.PaymentWebhookProcessed, model.PaymentWebhookRejected:
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

func paymentMerchantOrderNo(orderNumber string, now time.Time) string {
	digest := sha256.Sum256([]byte(orderNumber + now.UTC().Format(time.RFC3339Nano)))
	return "M" + now.UTC().Format("20060102150405") + strings.ToUpper(hex.EncodeToString(digest[:6]))
}
