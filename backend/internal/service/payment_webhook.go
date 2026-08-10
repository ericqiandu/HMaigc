package service

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

const paymentWebhookClockSkew = 5 * time.Minute

type WechatPaymentWebhookHeaders struct {
	Serial    string
	Timestamp string
	Nonce     string
	Signature string
}

type wechatWebhookEnvelope struct {
	ID        string `json:"id"`
	EventType string `json:"event_type"`
	Resource  struct {
		Algorithm      string `json:"algorithm"`
		Ciphertext     string `json:"ciphertext"`
		Nonce          string `json:"nonce"`
		AssociatedData string `json:"associated_data"`
	} `json:"resource"`
}

type wechatPaymentNotification struct {
	AppID           string `json:"appid"`
	MerchantID      string `json:"mchid"`
	MerchantOrderNo string `json:"out_trade_no"`
	TransactionID   string `json:"transaction_id"`
	TradeState      string `json:"trade_state"`
	SuccessTime     string `json:"success_time"`
	Amount          struct {
		Total         int64  `json:"total"`
		PayerTotal    int64  `json:"payer_total"`
		Currency      string `json:"currency"`
		PayerCurrency string `json:"payer_currency"`
	} `json:"amount"`
}

func (s *Service) HandleWechatPaymentWebhook(headers WechatPaymentWebhookHeaders, body []byte) error {
	_, setting, err := s.readPaymentSetting()
	if err != nil {
		return err
	}
	channel := setting.Wechat
	if !wechatPaymentChannelReady(channel) {
		return paymentWebhookRequestError("微信支付渠道尚未完整配置")
	}
	if headers.Serial != channel.WechatpayPublicKeyID {
		return paymentWebhookRequestError("微信支付通知公钥 ID 与当前配置不一致")
	}
	if err := verifyWechatWebhookSignature(headers, body, channel.WechatpayPublicKey, time.Now()); err != nil {
		return paymentWebhookRequestError(err.Error())
	}
	var envelope wechatWebhookEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return paymentWebhookRequestError("微信支付通知格式无效")
	}
	if strings.TrimSpace(envelope.ID) == "" || envelope.EventType != "TRANSACTION.SUCCESS" {
		return paymentWebhookRequestError("微信支付通知事件无效")
	}
	plaintext, err := decryptWechatResource(envelope.Resource.Algorithm, envelope.Resource.Ciphertext, envelope.Resource.Nonce, envelope.Resource.AssociatedData, channel.APIv3Key)
	if err != nil {
		return paymentWebhookRequestError(err.Error())
	}
	var notification wechatPaymentNotification
	if err := json.Unmarshal(plaintext, &notification); err != nil {
		return paymentWebhookRequestError("微信支付通知资源格式无效")
	}
	if notification.TradeState != "SUCCESS" {
		return paymentWebhookRequestError("微信支付交易状态不是成功")
	}
	if notification.AppID != channel.AppID || notification.MerchantID != channel.MerchantID {
		return paymentWebhookRequestError("微信支付通知商户身份不匹配")
	}
	paidAt, err := time.Parse(time.RFC3339, notification.SuccessTime)
	if err != nil {
		return paymentWebhookRequestError("微信支付成功时间无效")
	}
	return s.fulfillVerifiedPayment(
		model.PaymentProviderWechat,
		envelope.ID,
		notification.MerchantOrderNo,
		notification.TransactionID,
		notification.Amount.Total,
		notification.Amount.Currency,
		paidAt,
		body,
	)
}

func (s *Service) HandleAlipayPaymentWebhook(body []byte) error {
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return paymentWebhookRequestError("支付宝通知表单格式无效")
	}
	_, setting, err := s.readPaymentSetting()
	if err != nil {
		return err
	}
	channel := setting.Alipay
	if !alipayPaymentChannelReady(channel) {
		return paymentWebhookRequestError("支付宝渠道尚未完整配置")
	}
	if err := verifyAlipayWebhookSignature(values, channel.PlatformPublicKey); err != nil {
		return paymentWebhookRequestError(err.Error())
	}
	if values.Get("app_id") != channel.AppID || values.Get("seller_id") != channel.MerchantID {
		return paymentWebhookRequestError("支付宝通知商户身份不匹配")
	}
	status := values.Get("trade_status")
	if status != "TRADE_SUCCESS" && status != "TRADE_FINISHED" {
		return paymentWebhookRequestError("支付宝交易状态不是成功")
	}
	amountCents, err := parseAmountCents(values.Get("total_amount"))
	if err != nil {
		return paymentWebhookRequestError("支付宝通知金额无效")
	}
	paidAt, err := time.ParseInLocation("2006-01-02 15:04:05", values.Get("gmt_payment"), time.FixedZone("UTC+8", 8*60*60))
	if err != nil {
		return paymentWebhookRequestError("支付宝支付时间无效")
	}
	eventID := strings.TrimSpace(values.Get("notify_id"))
	if eventID == "" {
		return paymentWebhookRequestError("支付宝通知缺少 notify_id")
	}
	return s.fulfillVerifiedPayment(
		model.PaymentProviderAlipay,
		eventID,
		values.Get("out_trade_no"),
		values.Get("trade_no"),
		amountCents,
		"CNY",
		paidAt,
		body,
	)
}

func (s *Service) fulfillVerifiedPayment(provider model.PaymentProvider, eventID string, merchantOrderNo string, providerTradeNo string, amountCents int64, currency string, paidAt time.Time, body []byte) error {
	if strings.TrimSpace(merchantOrderNo) == "" || strings.TrimSpace(providerTradeNo) == "" {
		return paymentWebhookRequestError("支付通知缺少交易标识")
	}
	digest := sha256.Sum256(body)
	now := time.Now()
	transaction, err := s.repo.PaymentTransactionByMerchantOrderNo(provider, merchantOrderNo)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	event := &model.PaymentWebhookEvent{
		ID: newID(), Provider: provider, ProviderEventID: eventID,
		MerchantOrderNo: merchantOrderNo, ProviderTradeNo: providerTradeNo, AmountCents: amountCents,
		Currency: currency, PaidAt: &paidAt, PayloadDigest: hex.EncodeToString(digest[:]), Status: model.PaymentWebhookReceived,
		ReceivedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if transaction != nil {
		event.TransactionID = transaction.ID
	}
	storedEvent, _, recordErr := s.repo.RecordPaymentWebhookEvent(event)
	if recordErr != nil {
		return recordErr
	}
	event = storedEvent
	if event.Status == model.PaymentWebhookProcessed {
		return nil
	}
	if event.Status == model.PaymentWebhookReviewRequired && event.FailureCode == "provider_trade_conflict" {
		return &paymentWebhookDispositionError{
			err: errors.New("渠道交易号已绑定其他支付交易，验签事实已持久化并等待人工复核"), acknowledge: true,
		}
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return s.rejectVerifiedPaymentEvent(event, "unknown_merchant_order", "验签支付通知的商户订单号不存在", now)
	}
	if transaction.AmountCents != amountCents || transaction.Currency != currency {
		return s.rejectVerifiedPaymentEvent(event, "amount_currency_mismatch", "支付通知金额或币种与本地交易不一致", now)
	}
	if transaction.ExpiresAt != nil && paidAt.After(*transaction.ExpiresAt) {
		return s.reviewVerifiedPaymentEvent(event, "late_payment", "支付时间晚于本地交易有效期，必须人工对账", now, true)
	}
	if transaction.Status == model.PaymentTransactionPaid && transaction.ProviderTradeNo != providerTradeNo {
		return s.reviewVerifiedPaymentEvent(event, "second_payment", "订单已由另一渠道交易到账，必须人工对账", now, true)
	}
	if transaction.Status == model.PaymentTransactionFailed || transaction.Status == model.PaymentTransactionClosed || transaction.Status == model.PaymentTransactionRefunded {
		return s.reviewVerifiedPaymentEvent(event, "non_payable_transaction", "验签到账对应的本地交易已终止，必须人工对账", now, true)
	}
	var activation repository.MembershipActivation
	if transaction.OrderType == model.PaymentOrderMembership {
		order, lookupErr := s.repo.MembershipOrder(transaction.OrderID)
		if lookupErr != nil {
			return s.reviewVerifiedPaymentEvent(event, "membership_order_lookup_failed", truncateRunes(lookupErr.Error(), 500), now, false)
		}
		activation, err = s.membershipFulfillmentForOrder(order, model.SystemActorID, paidAt)
		if err != nil {
			return s.reviewVerifiedPaymentEvent(event, "membership_activation_invalid", truncateRunes(err.Error(), 500), now, true)
		}
	} else if transaction.OrderType != model.PaymentOrderCreditTopup {
		return s.reviewVerifiedPaymentEvent(event, "unsupported_order_type", "支付交易订单类型无效", now, true)
	}
	_, err = s.repo.FulfillPaymentTransaction(repository.PaymentFulfillment{
		Event: event, TransactionID: transaction.ID, ProviderTradeNo: providerTradeNo,
		PaidAt: paidAt, PaymentConfirmed: true, Activation: activation,
	})
	if err != nil {
		if markErr := s.repo.MarkPaymentWebhookOutcome(event.ID, model.PaymentWebhookReviewRequired, "fulfillment_failed", truncateRunes(err.Error(), 500), now); markErr != nil {
			return fmt.Errorf("支付履约失败且验签事实结果更新失败: %v: %w", markErr, err)
		}
		return &paymentWebhookDispositionError{err: err, acknowledge: false}
	}
	return nil
}

type paymentWebhookDispositionError struct {
	err         error
	acknowledge bool
}

func (e *paymentWebhookDispositionError) Error() string { return e.err.Error() }
func (e *paymentWebhookDispositionError) Unwrap() error { return e.err }

func ShouldAcknowledgePaymentWebhook(err error) bool {
	if err == nil {
		return true
	}
	var disposition *paymentWebhookDispositionError
	return errors.As(err, &disposition) && disposition.acknowledge
}

func (s *Service) rejectVerifiedPaymentEvent(event *model.PaymentWebhookEvent, code string, reason string, now time.Time) error {
	if err := s.repo.MarkPaymentWebhookOutcome(event.ID, model.PaymentWebhookRejected, code, reason, now); err != nil {
		return err
	}
	return &paymentWebhookDispositionError{err: errors.New(reason), acknowledge: true}
}

func (s *Service) reviewVerifiedPaymentEvent(event *model.PaymentWebhookEvent, code string, reason string, now time.Time, acknowledge bool) error {
	if err := s.repo.MarkPaymentWebhookOutcome(event.ID, model.PaymentWebhookReviewRequired, code, reason, now); err != nil {
		return err
	}
	return &paymentWebhookDispositionError{err: errors.New(reason), acknowledge: acknowledge}
}

func verifyWechatWebhookSignature(headers WechatPaymentWebhookHeaders, body []byte, publicKeyPEM string, now time.Time) error {
	timestamp, err := strconv.ParseInt(strings.TrimSpace(headers.Timestamp), 10, 64)
	if err != nil {
		return errors.New("微信支付通知时间戳无效")
	}
	signedAt := time.Unix(timestamp, 0)
	if signedAt.Before(now.Add(-paymentWebhookClockSkew)) || signedAt.After(now.Add(paymentWebhookClockSkew)) {
		return errors.New("微信支付通知时间戳超出允许范围")
	}
	if strings.TrimSpace(headers.Nonce) == "" || strings.TrimSpace(headers.Signature) == "" {
		return errors.New("微信支付通知签名头缺失")
	}
	message := headers.Timestamp + "\n" + headers.Nonce + "\n" + string(body) + "\n"
	if err := verifyRSA2(publicKeyPEM, message, headers.Signature); err != nil {
		return fmt.Errorf("微信支付通知验签失败: %w", err)
	}
	return nil
}

func verifyAlipayWebhookSignature(values url.Values, publicKeyPEM string) error {
	signature := values.Get("sign")
	if signature == "" || values.Get("sign_type") != "RSA2" {
		return errors.New("支付宝通知签名参数无效")
	}
	if err := verifyRSA2(publicKeyPEM, canonicalAlipayWebhookForm(values), signature); err != nil {
		return fmt.Errorf("支付宝通知验签失败: %w", err)
	}
	return nil
}

func verifyRSA2(publicKeyPEM string, message string, signatureValue string) error {
	key, err := parseRSAPublicKey(publicKeyPEM)
	if err != nil {
		return err
	}
	signature, err := base64.StdEncoding.DecodeString(signatureValue)
	if err != nil {
		return errors.New("签名不是有效的 Base64")
	}
	digest := sha256.Sum256([]byte(message))
	return rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature)
}

func parseRSAPublicKey(value string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, errors.New("平台公钥 PEM 编码无效")
	}
	if publicKey, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		rsaKey, ok := publicKey.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("平台公钥不是 RSA 类型")
		}
		return rsaKey, nil
	}
	if certificate, err := x509.ParseCertificate(block.Bytes); err == nil {
		rsaKey, ok := certificate.PublicKey.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("平台证书公钥不是 RSA 类型")
		}
		return rsaKey, nil
	}
	return x509.ParsePKCS1PublicKey(block.Bytes)
}

func decryptWechatResource(algorithm string, ciphertextValue string, nonce string, associatedData string, apiV3Key string) ([]byte, error) {
	if algorithm != "AEAD_AES_256_GCM" {
		return nil, fmt.Errorf("不支持的微信支付通知加密算法 %q", algorithm)
	}
	if len(apiV3Key) != 32 {
		return nil, errors.New("微信支付 APIv3 密钥必须为 32 字节")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextValue)
	if err != nil {
		return nil, errors.New("微信支付通知密文不是有效的 Base64")
	}
	block, err := aes.NewCipher([]byte(apiV3Key))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, errors.New("微信支付通知随机串长度无效")
	}
	plaintext, err := gcm.Open(nil, []byte(nonce), ciphertext, []byte(associatedData))
	if err != nil {
		return nil, errors.New("微信支付通知资源解密失败")
	}
	return plaintext, nil
}

func parseAmountCents(value string) (int64, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 || len(parts[1]) != 2 || parts[0] == "" {
		return 0, errors.New("金额必须包含两位小数")
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole < 0 {
		return 0, errors.New("金额整数部分无效")
	}
	fraction, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || fraction < 0 {
		return 0, errors.New("金额小数部分无效")
	}
	if whole > (math.MaxInt64-fraction)/100 {
		return 0, errors.New("金额超出范围")
	}
	return whole*100 + fraction, nil
}

func canonicalAlipayWebhookForm(values url.Values) string {
	filtered := make(url.Values, len(values))
	for key, entries := range values {
		if key == "sign" || key == "sign_type" {
			continue
		}
		filtered[key] = entries
	}
	return canonicalForm(filtered)
}

func paymentWebhookRequestError(message string) error {
	return &AuthError{Status: http.StatusBadRequest, Message: message}
}
