package service

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
)

const paymentResponseLimit = 256 << 10

var paymentHTTPClient = &http.Client{Timeout: 15 * time.Second}

type paymentOrderReference struct {
	OrderNumber string
	Description string
}

type providerPaymentState string

const (
	providerPaymentPaid     providerPaymentState = "paid"
	providerPaymentUnpaid   providerPaymentState = "unpaid"
	providerPaymentNotFound providerPaymentState = "not_found"
	providerPaymentUnknown  providerPaymentState = "unknown"
)

type providerPaymentFact struct {
	State           providerPaymentState
	ProviderTradeNo string
	AmountCents     int64
	Currency        string
	PaidAt          time.Time
}

type providerRequestError struct {
	err           error
	deterministic bool
}

func (e *providerRequestError) Error() string { return e.err.Error() }
func (e *providerRequestError) Unwrap() error { return e.err }

func deterministicProviderError(err error) error {
	return &providerRequestError{err: err, deterministic: true}
}

func ambiguousProviderError(err error) error {
	return &providerRequestError{err: err, deterministic: false}
}

func isDeterministicProviderRejection(err error) bool {
	var providerErr *providerRequestError
	if errors.As(err, &providerErr) {
		return providerErr.deterministic
	}
	// Errors raised before the HTTP request (invalid merchant key, payload or URL) prove no provider order was created.
	return true
}

func createProviderPayment(transaction *model.PaymentTransaction, order paymentOrderReference, channel paymentChannelSettingValue) (string, error) {
	switch transaction.Provider {
	case model.PaymentProviderWechat:
		return createWechatNativePayment(transaction, order, channel)
	case model.PaymentProviderAlipay:
		return createAlipayPrecreatePayment(transaction, order, channel)
	default:
		return "", fmt.Errorf("不支持的支付渠道 %q", transaction.Provider)
	}
}

func queryProviderPayment(transaction *model.PaymentTransaction, channel paymentChannelSettingValue) (providerPaymentFact, error) {
	switch transaction.Provider {
	case model.PaymentProviderWechat:
		return queryWechatPayment(transaction, channel)
	case model.PaymentProviderAlipay:
		return queryAlipayPayment(transaction, channel)
	default:
		return providerPaymentFact{}, fmt.Errorf("不支持的支付渠道 %q", transaction.Provider)
	}
}

func closeProviderPayment(transaction *model.PaymentTransaction, channel paymentChannelSettingValue) error {
	switch transaction.Provider {
	case model.PaymentProviderWechat:
		return closeWechatPayment(transaction, channel)
	case model.PaymentProviderAlipay:
		return closeAlipayPayment(transaction, channel)
	default:
		return fmt.Errorf("不支持的支付渠道 %q", transaction.Provider)
	}
}

func queryWechatPayment(transaction *model.PaymentTransaction, channel paymentChannelSettingValue) (providerPaymentFact, error) {
	endpoint := strings.TrimRight(channel.GatewayURL, "/") + "/v3/pay/transactions/out-trade-no/" + url.PathEscape(transaction.MerchantOrderNo) + "?mchid=" + url.QueryEscape(channel.MerchantID)
	req, err := signedWechatRequest(http.MethodGet, endpoint, nil, channel)
	if err != nil {
		return providerPaymentFact{}, err
	}
	body, status, _, err := doSignedWechatRequest(req, channel.PlatformPublicKey)
	if err != nil {
		return providerPaymentFact{}, err
	}
	if status == http.StatusNotFound {
		return providerPaymentFact{State: providerPaymentNotFound}, nil
	}
	if status != http.StatusOK {
		return providerPaymentFact{State: providerPaymentUnknown}, nil
	}
	var response struct {
		AppID           string `json:"appid"`
		MerchantID      string `json:"mchid"`
		MerchantOrderNo string `json:"out_trade_no"`
		TransactionID   string `json:"transaction_id"`
		TradeState      string `json:"trade_state"`
		SuccessTime     string `json:"success_time"`
		Amount          struct {
			Total    int64  `json:"total"`
			Currency string `json:"currency"`
		} `json:"amount"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return providerPaymentFact{}, fmt.Errorf("微信支付查单响应格式无效: %w", err)
	}
	if response.AppID != channel.AppID || response.MerchantID != channel.MerchantID || response.MerchantOrderNo != transaction.MerchantOrderNo {
		return providerPaymentFact{}, errors.New("微信支付查单商户事实不匹配")
	}
	switch response.TradeState {
	case "SUCCESS":
		paidAt, err := time.Parse(time.RFC3339, response.SuccessTime)
		if err != nil || response.TransactionID == "" {
			return providerPaymentFact{}, errors.New("微信支付查单到账事实不完整")
		}
		return providerPaymentFact{
			State: providerPaymentPaid, ProviderTradeNo: response.TransactionID,
			AmountCents: response.Amount.Total, Currency: response.Amount.Currency, PaidAt: paidAt,
		}, nil
	case "NOTPAY", "USERPAYING":
		return providerPaymentFact{State: providerPaymentUnpaid}, nil
	case "CLOSED", "REVOKED", "PAYERROR":
		return providerPaymentFact{State: providerPaymentNotFound}, nil
	default:
		return providerPaymentFact{State: providerPaymentUnknown}, nil
	}
}

func closeWechatPayment(transaction *model.PaymentTransaction, channel paymentChannelSettingValue) error {
	body, err := json.Marshal(map[string]string{"mchid": channel.MerchantID})
	if err != nil {
		return err
	}
	endpoint := strings.TrimRight(channel.GatewayURL, "/") + "/v3/pay/transactions/out-trade-no/" + url.PathEscape(transaction.MerchantOrderNo) + "/close"
	req, err := signedWechatRequest(http.MethodPost, endpoint, body, channel)
	if err != nil {
		return err
	}
	_, status, _, err := doSignedWechatRequest(req, channel.PlatformPublicKey)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent {
		return fmt.Errorf("微信支付关单返回 HTTP %d", status)
	}
	return nil
}

func signedWechatRequest(method string, endpoint string, body []byte, channel paymentChannelSettingValue) (*http.Request, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce, err := randomNonce()
	if err != nil {
		return nil, err
	}
	message := method + "\n" + parsed.RequestURI() + "\n" + timestamp + "\n" + nonce + "\n" + string(body) + "\n"
	signature, err := signRSA2(channel.MerchantPrivateKey, message)
	if err != nil {
		return nil, fmt.Errorf("微信商户私钥无效: %w", err)
	}
	req, err := http.NewRequest(method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", fmt.Sprintf(
		`WECHATPAY2-SHA256-RSA2048 mchid="%s",nonce_str="%s",timestamp="%s",serial_no="%s",signature="%s"`,
		channel.MerchantID, nonce, timestamp, channel.MerchantSerialNo, signature,
	))
	return req, nil
}

func doSignedWechatRequest(req *http.Request, publicKeyPEM string) ([]byte, int, http.Header, error) {
	responseBody, status, headers, err := doPaymentRequestWithHeaders(req)
	if err != nil {
		return nil, 0, nil, err
	}
	timestamp := headers.Get("Wechatpay-Timestamp")
	nonce := headers.Get("Wechatpay-Nonce")
	signature := headers.Get("Wechatpay-Signature")
	if timestamp == "" || nonce == "" || signature == "" {
		return nil, status, headers, errors.New("微信支付响应缺少验签头")
	}
	signedUnix, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return nil, status, headers, errors.New("微信支付响应时间戳无效")
	}
	signedAt := time.Unix(signedUnix, 0)
	if signedAt.Before(time.Now().Add(-paymentWebhookClockSkew)) || signedAt.After(time.Now().Add(paymentWebhookClockSkew)) {
		return nil, status, headers, errors.New("微信支付响应时间戳超出允许范围")
	}
	if err := verifyRSA2(publicKeyPEM, timestamp+"\n"+nonce+"\n"+string(responseBody)+"\n", signature); err != nil {
		return nil, status, headers, fmt.Errorf("微信支付响应验签失败: %w", err)
	}
	return responseBody, status, headers, nil
}

func queryAlipayPayment(transaction *model.PaymentTransaction, channel paymentChannelSettingValue) (providerPaymentFact, error) {
	inner, err := alipayTradeOperation("alipay.trade.query", transaction, channel)
	if err != nil {
		return providerPaymentFact{}, err
	}
	var response struct {
		Code            string `json:"code"`
		SubCode         string `json:"sub_code"`
		MerchantOrderNo string `json:"out_trade_no"`
		TradeNo         string `json:"trade_no"`
		TradeStatus     string `json:"trade_status"`
		TotalAmount     string `json:"total_amount"`
		PaidAt          string `json:"send_pay_date"`
	}
	if err := json.Unmarshal(inner, &response); err != nil {
		return providerPaymentFact{}, fmt.Errorf("支付宝查单响应格式无效: %w", err)
	}
	if response.Code == "40004" && response.SubCode == "ACQ.TRADE_NOT_EXIST" {
		return providerPaymentFact{State: providerPaymentNotFound}, nil
	}
	if response.Code != "10000" {
		return providerPaymentFact{State: providerPaymentUnknown}, nil
	}
	if response.MerchantOrderNo != transaction.MerchantOrderNo {
		return providerPaymentFact{}, errors.New("支付宝查单商户订单号不匹配")
	}
	switch response.TradeStatus {
	case "TRADE_SUCCESS", "TRADE_FINISHED":
		amount, amountErr := parseAmountCents(response.TotalAmount)
		paidAt, paidAtErr := time.ParseInLocation("2006-01-02 15:04:05", response.PaidAt, time.FixedZone("UTC+8", 8*60*60))
		if amountErr != nil || paidAtErr != nil || response.TradeNo == "" {
			return providerPaymentFact{}, errors.New("支付宝查单到账事实不完整")
		}
		return providerPaymentFact{State: providerPaymentPaid, ProviderTradeNo: response.TradeNo, AmountCents: amount, Currency: "CNY", PaidAt: paidAt}, nil
	case "WAIT_BUYER_PAY":
		return providerPaymentFact{State: providerPaymentUnpaid}, nil
	case "TRADE_CLOSED":
		return providerPaymentFact{State: providerPaymentNotFound}, nil
	default:
		return providerPaymentFact{State: providerPaymentUnknown}, nil
	}
}

func closeAlipayPayment(transaction *model.PaymentTransaction, channel paymentChannelSettingValue) error {
	inner, err := alipayTradeOperation("alipay.trade.close", transaction, channel)
	if err != nil {
		return err
	}
	var response struct {
		Code    string `json:"code"`
		Message string `json:"msg"`
		SubCode string `json:"sub_code"`
		SubMsg  string `json:"sub_msg"`
	}
	if err := json.Unmarshal(inner, &response); err != nil {
		return fmt.Errorf("支付宝关单响应格式无效: %w", err)
	}
	if response.Code != "10000" {
		return fmt.Errorf("支付宝拒绝关单: %s %s", response.SubCode, firstPaymentMessage(response.SubMsg, response.Message))
	}
	return nil
}

func alipayTradeOperation(method string, transaction *model.PaymentTransaction, channel paymentChannelSettingValue) (json.RawMessage, error) {
	bizContent, err := json.Marshal(map[string]string{"out_trade_no": transaction.MerchantOrderNo})
	if err != nil {
		return nil, err
	}
	values := url.Values{
		"app_id": {channel.AppID}, "method": {method}, "format": {"JSON"}, "charset": {"utf-8"},
		"sign_type": {"RSA2"}, "timestamp": {time.Now().Format("2006-01-02 15:04:05")}, "version": {"1.0"},
		"biz_content": {string(bizContent)},
	}
	signature, err := signRSA2(channel.MerchantPrivateKey, canonicalForm(values))
	if err != nil {
		return nil, fmt.Errorf("支付宝商户私钥无效: %w", err)
	}
	values.Set("sign", signature)
	req, err := http.NewRequest(http.MethodPost, channel.GatewayURL, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	body, status, err := doPaymentRequest(req)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("支付宝网关返回 HTTP %d", status)
	}
	responseField := strings.ReplaceAll(method, ".", "_") + "_response"
	return verifiedAlipayResponse(body, responseField, channel.PlatformPublicKey)
}

func verifiedAlipayResponse(body []byte, responseField string, publicKeyPEM string) (json.RawMessage, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("支付宝响应格式无效: %w", err)
	}
	inner, ok := envelope[responseField]
	if !ok || len(inner) == 0 {
		return nil, errors.New("支付宝响应缺少业务结果")
	}
	var signatureValue string
	if rawSignature, exists := envelope["sign"]; !exists || json.Unmarshal(rawSignature, &signatureValue) != nil || signatureValue == "" {
		return nil, errors.New("支付宝响应缺少签名")
	}
	if err := verifyRSA2(publicKeyPEM, string(inner), signatureValue); err != nil {
		return nil, fmt.Errorf("支付宝响应验签失败: %w", err)
	}
	return inner, nil
}

func createWechatNativePayment(transaction *model.PaymentTransaction, order paymentOrderReference, channel paymentChannelSettingValue) (string, error) {
	if transaction.ExpiresAt == nil {
		return "", errors.New("支付交易缺少过期时间")
	}
	payload := struct {
		AppID          string `json:"appid"`
		MerchantID     string `json:"mchid"`
		Description    string `json:"description"`
		MerchantOrder  string `json:"out_trade_no"`
		NotifyURL      string `json:"notify_url"`
		ExpirationTime string `json:"time_expire"`
		Amount         struct {
			Total    int64  `json:"total"`
			Currency string `json:"currency"`
		} `json:"amount"`
	}{
		AppID: channel.AppID, MerchantID: channel.MerchantID, Description: order.Description,
		MerchantOrder: transaction.MerchantOrderNo, NotifyURL: channel.NotifyURL,
		ExpirationTime: transaction.ExpiresAt.Format(time.RFC3339),
	}
	payload.Amount.Total, payload.Amount.Currency = transaction.AmountCents, transaction.Currency
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	endpoint := strings.TrimRight(channel.GatewayURL, "/") + "/v3/pay/transactions/native"
	req, err := signedWechatRequest(http.MethodPost, endpoint, body, channel)
	if err != nil {
		return "", err
	}
	responseBody, status, _, err := doSignedWechatRequest(req, channel.PlatformPublicKey)
	if err != nil {
		return "", ambiguousProviderError(err)
	}
	if status != http.StatusOK {
		var response struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(responseBody, &response); err != nil || strings.TrimSpace(response.Code) == "" {
			return "", ambiguousProviderError(fmt.Errorf("微信支付下单返回无法识别的 HTTP %d 结果", status))
		}
		rejection := fmt.Errorf("微信支付拒绝下单: %s", response.Code)
		if deterministicWechatCreateRejection(response.Code) {
			return "", deterministicProviderError(rejection)
		}
		return "", ambiguousProviderError(rejection)
	}
	var response struct {
		CodeURL string `json:"code_url"`
	}
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", ambiguousProviderError(fmt.Errorf("微信支付响应格式无效: %w", err))
	}
	if strings.TrimSpace(response.CodeURL) == "" {
		return "", ambiguousProviderError(errors.New("微信支付响应缺少 code_url"))
	}
	return response.CodeURL, nil
}

func deterministicWechatCreateRejection(code string) bool {
	switch strings.TrimSpace(code) {
	case "PARAM_ERROR":
		return true
	default:
		return false
	}
}

func createAlipayPrecreatePayment(transaction *model.PaymentTransaction, order paymentOrderReference, channel paymentChannelSettingValue) (string, error) {
	if transaction.Currency != "CNY" {
		return "", fmt.Errorf("支付宝当面付仅支持 CNY，当前币种为 %s", transaction.Currency)
	}
	bizContent, err := json.Marshal(map[string]string{
		"out_trade_no":    transaction.MerchantOrderNo,
		"total_amount":    formatAmountCents(transaction.AmountCents),
		"subject":         order.Description,
		"timeout_express": "15m",
	})
	if err != nil {
		return "", err
	}
	values := url.Values{
		"app_id":      {channel.AppID},
		"method":      {"alipay.trade.precreate"},
		"format":      {"JSON"},
		"charset":     {"utf-8"},
		"sign_type":   {"RSA2"},
		"timestamp":   {time.Now().Format("2006-01-02 15:04:05")},
		"version":     {"1.0"},
		"notify_url":  {channel.NotifyURL},
		"biz_content": {string(bizContent)},
	}
	signature, err := signRSA2(channel.MerchantPrivateKey, canonicalForm(values))
	if err != nil {
		return "", fmt.Errorf("支付宝商户私钥无效: %w", err)
	}
	values.Set("sign", signature)
	req, err := http.NewRequest(http.MethodPost, channel.GatewayURL, strings.NewReader(values.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	responseBody, status, err := doPaymentRequest(req)
	if err != nil {
		return "", ambiguousProviderError(err)
	}
	if status != http.StatusOK {
		return "", ambiguousProviderError(fmt.Errorf("支付宝返回无法验签的 HTTP %d 结果", status))
	}
	inner, err := verifiedAlipayResponse(responseBody, "alipay_trade_precreate_response", channel.PlatformPublicKey)
	if err != nil {
		return "", ambiguousProviderError(err)
	}
	var response struct {
		Code            string `json:"code"`
		SubCode         string `json:"sub_code"`
		MerchantOrderNo string `json:"out_trade_no"`
		QRCode          string `json:"qr_code"`
	}
	if err := json.Unmarshal(inner, &response); err != nil {
		return "", ambiguousProviderError(fmt.Errorf("支付宝下单业务响应格式无效: %w", err))
	}
	if response.Code != "10000" {
		rejection := fmt.Errorf("支付宝拒绝下单: %s/%s", response.Code, response.SubCode)
		if deterministicAlipayCreateRejection(response.Code, response.SubCode) {
			return "", deterministicProviderError(rejection)
		}
		return "", ambiguousProviderError(rejection)
	}
	if response.MerchantOrderNo != transaction.MerchantOrderNo {
		return "", ambiguousProviderError(errors.New("支付宝下单响应商户订单号不匹配"))
	}
	if strings.TrimSpace(response.QRCode) == "" {
		return "", ambiguousProviderError(errors.New("支付宝响应缺少 qr_code"))
	}
	return response.QRCode, nil
}

func deterministicAlipayCreateRejection(code string, subCode string) bool {
	return strings.TrimSpace(code) == "40004" && strings.TrimSpace(subCode) == "ACQ.INVALID_PARAMETER"
}

func formatAmountCents(amount int64) string {
	return fmt.Sprintf("%d.%02d", amount/100, amount%100)
}

func signRSA2(privateKeyPEM string, message string) (string, error) {
	key, err := parseRSAPrivateKey(privateKeyPEM)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(message))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

func parseRSAPrivateKey(value string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, errors.New("PEM 编码无效")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("私钥不是 RSA 类型")
		}
		return rsaKey, nil
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

func canonicalForm(values url.Values) string {
	keys := make([]string, 0, len(values))
	for key, entries := range values {
		if key != "sign" && len(entries) > 0 && entries[0] != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values.Get(key))
	}
	return strings.Join(parts, "&")
}

func doPaymentRequest(req *http.Request) ([]byte, int, error) {
	body, status, _, err := doPaymentRequestWithHeaders(req)
	return body, status, err
}

func doPaymentRequestWithHeaders(req *http.Request) ([]byte, int, http.Header, error) {
	resp, err := paymentHTTPClient.Do(req)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("支付网关请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, paymentResponseLimit+1))
	if err != nil {
		return nil, resp.StatusCode, resp.Header, err
	}
	if len(body) > paymentResponseLimit {
		return nil, resp.StatusCode, resp.Header, errors.New("支付网关响应超过大小限制")
	}
	return body, resp.StatusCode, resp.Header, nil
}

func randomNonce() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func firstPaymentMessage(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
