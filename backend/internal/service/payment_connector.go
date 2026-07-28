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

func createProviderPayment(transaction *model.PaymentTransaction, order *model.MembershipOrder, channel paymentChannelSettingValue) (string, error) {
	switch transaction.Provider {
	case model.PaymentProviderWechat:
		return createWechatNativePayment(transaction, order, channel)
	case model.PaymentProviderAlipay:
		return createAlipayPrecreatePayment(transaction, order, channel)
	default:
		return "", fmt.Errorf("不支持的支付渠道 %q", transaction.Provider)
	}
}

func createWechatNativePayment(transaction *model.PaymentTransaction, order *model.MembershipOrder, channel paymentChannelSettingValue) (string, error) {
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
		AppID: channel.AppID, MerchantID: channel.MerchantID, Description: "会员订单 " + order.OrderNumber,
		MerchantOrder: transaction.MerchantOrderNo, NotifyURL: channel.NotifyURL,
		ExpirationTime: transaction.ExpiresAt.Format(time.RFC3339),
	}
	payload.Amount.Total, payload.Amount.Currency = transaction.AmountCents, transaction.Currency
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	endpoint := strings.TrimRight(channel.GatewayURL, "/") + "/v3/pay/transactions/native"
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce, err := randomNonce()
	if err != nil {
		return "", err
	}
	message := "POST\n" + parsed.RequestURI() + "\n" + timestamp + "\n" + nonce + "\n" + string(body) + "\n"
	signature, err := signRSA2(channel.MerchantPrivateKey, message)
	if err != nil {
		return "", fmt.Errorf("微信商户私钥无效: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf(
		`WECHATPAY2-SHA256-RSA2048 mchid="%s",nonce_str="%s",timestamp="%s",serial_no="%s",signature="%s"`,
		channel.MerchantID, nonce, timestamp, channel.MerchantSerialNo, signature,
	))
	responseBody, status, err := doPaymentRequest(req)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("微信支付返回 HTTP %d: %s", status, paymentErrorMessage(responseBody))
	}
	var response struct {
		CodeURL string `json:"code_url"`
	}
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", fmt.Errorf("微信支付响应格式无效: %w", err)
	}
	if strings.TrimSpace(response.CodeURL) == "" {
		return "", errors.New("微信支付响应缺少 code_url")
	}
	return response.CodeURL, nil
}

func createAlipayPrecreatePayment(transaction *model.PaymentTransaction, order *model.MembershipOrder, channel paymentChannelSettingValue) (string, error) {
	if transaction.Currency != "CNY" {
		return "", fmt.Errorf("支付宝当面付仅支持 CNY，当前币种为 %s", transaction.Currency)
	}
	bizContent, err := json.Marshal(map[string]string{
		"out_trade_no":    transaction.MerchantOrderNo,
		"total_amount":    formatAmountCents(transaction.AmountCents),
		"subject":         "会员订单 " + order.OrderNumber,
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
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("支付宝返回 HTTP %d: %s", status, paymentErrorMessage(responseBody))
	}
	var envelope struct {
		Response struct {
			Code    string `json:"code"`
			Message string `json:"msg"`
			SubCode string `json:"sub_code"`
			SubMsg  string `json:"sub_msg"`
			QRCode  string `json:"qr_code"`
		} `json:"alipay_trade_precreate_response"`
	}
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return "", fmt.Errorf("支付宝响应格式无效: %w", err)
	}
	if envelope.Response.Code != "10000" {
		return "", fmt.Errorf("支付宝拒绝下单: %s %s", envelope.Response.SubCode, firstPaymentMessage(envelope.Response.SubMsg, envelope.Response.Message))
	}
	if strings.TrimSpace(envelope.Response.QRCode) == "" {
		return "", errors.New("支付宝响应缺少 qr_code")
	}
	return envelope.Response.QRCode, nil
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
	resp, err := paymentHTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("支付网关请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, paymentResponseLimit+1))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if len(body) > paymentResponseLimit {
		return nil, resp.StatusCode, errors.New("支付网关响应超过大小限制")
	}
	return body, resp.StatusCode, nil
}

func randomNonce() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func paymentErrorMessage(body []byte) string {
	value := strings.TrimSpace(string(body))
	if len(value) > 500 {
		return value[:500]
	}
	return value
}

func firstPaymentMessage(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
