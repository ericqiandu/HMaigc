package service

import (
	"crypto/rsa"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
)

func TestPaymentCreateWechatRequiresSignedResponse(t *testing.T) {
	merchantPrivate, _ := newWebhookTestRSAKey(t)
	platformPrivate, platformPublicPEM := newWebhookTestRSAKey(t)
	wrongPrivate, _ := newWebhookTestRSAKey(t)
	transaction, order := paymentCreateTestFacts(model.PaymentProviderWechat)
	channel := wechatPaymentChannelSettingValue{
		AppID: "wechat-app", MerchantID: "wechat-merchant", MerchantSerialNo: "merchant-serial",
		MerchantPrivateKey: rsaPrivateKeyPEM(t, merchantPrivate), WechatpayPublicKeyID: "PUB_KEY_ID_3000000001", WechatpayPublicKey: platformPublicPEM,
		NotifyURL: "https://merchant.example.com/wechat", GatewayURL: "https://api.mch.weixin.qq.com",
	}

	tests := []struct {
		name      string
		response  func(*http.Request) *http.Response
		wantURL   string
		wantError bool
	}{
		{
			name: "valid signed response",
			response: func(request *http.Request) *http.Response {
				return signedWechatTestResponse(t, request, http.StatusOK, `{"code_url":"weixin://wxpay/signed-create"}`, platformPrivate)
			},
			wantURL: "weixin://wxpay/signed-create",
		},
		{
			name: "missing signature",
			response: func(request *http.Request) *http.Response {
				return paymentTestResponse(request, http.StatusOK, `{"code_url":"weixin://wxpay/unsigned"}`, nil)
			},
			wantError: true,
		},
		{
			name: "body mutation after signature",
			response: func(request *http.Request) *http.Response {
				return signedWechatTestResponse(t, request, http.StatusOK, `{"code_url":"weixin://wxpay/mutated"}`, wrongPrivate)
			},
			wantError: true,
		},
		{
			name: "expired signature",
			response: func(request *http.Request) *http.Response {
				body := `{"code_url":"weixin://wxpay/stale"}`
				return signedWechatCreateTestResponse(t, request, http.StatusOK, body, platformPrivate, time.Now().Add(-10*time.Minute))
			},
			wantError: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withPaymentHTTPTransport(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
				return test.response(request), nil
			}))
			codeURL, err := createWechatNativePayment(transaction, order, channel)
			if test.wantError {
				if err == nil {
					t.Fatalf("untrusted WeChat create response returned QR %q", codeURL)
				}
				if isDeterministicProviderRejection(err) {
					t.Fatalf("untrusted WeChat create response was classified deterministic: %v", err)
				}
				return
			}
			if err != nil || codeURL != test.wantURL {
				t.Fatalf("signed WeChat create response = %q, %v; want %q", codeURL, err, test.wantURL)
			}
		})
	}
}

func TestPaymentCreateAlipayRequiresSignedMerchantOrderResponse(t *testing.T) {
	merchantPrivate, _ := newWebhookTestRSAKey(t)
	platformPrivate, platformPublicPEM := newWebhookTestRSAKey(t)
	wrongPrivate, _ := newWebhookTestRSAKey(t)
	transaction, order := paymentCreateTestFacts(model.PaymentProviderAlipay)
	channel := alipayPaymentChannelSettingValue{
		AppID: "alipay-app", MerchantID: "alipay-merchant",
		MerchantPrivateKey: rsaPrivateKeyPEM(t, merchantPrivate), PlatformPublicKey: platformPublicPEM,
		NotifyURL: "https://merchant.example.com/alipay", GatewayURL: "https://openapi.alipay.com/gateway.do",
	}

	tests := []struct {
		name        string
		inner       string
		signer      *rsa.PrivateKey
		includeSign bool
		status      int
		wantURL     string
		wantError   bool
	}{
		{
			name:   "valid signed and bound response",
			inner:  fmt.Sprintf(`{"code":"10000","msg":"Success","out_trade_no":%q,"qr_code":"https://qr.example.com/signed"}`, transaction.MerchantOrderNo),
			signer: platformPrivate, includeSign: true, wantURL: "https://qr.example.com/signed",
		},
		{
			name:        "missing signature",
			inner:       fmt.Sprintf(`{"code":"10000","msg":"Success","out_trade_no":%q,"qr_code":"https://qr.example.com/unsigned"}`, transaction.MerchantOrderNo),
			includeSign: false, wantError: true,
		},
		{
			name:        "unsigned HTTP client error",
			inner:       `{"code":"40004","msg":"Business Failed","sub_code":"ACQ.INVALID_PARAMETER"}`,
			includeSign: false, status: http.StatusBadRequest, wantError: true,
		},
		{
			name:   "inner JSON mutation",
			inner:  fmt.Sprintf(`{"code":"10000","msg":"Success","out_trade_no":%q,"qr_code":"https://qr.example.com/mutated"}`, transaction.MerchantOrderNo),
			signer: wrongPrivate, includeSign: true, wantError: true,
		},
		{
			name:   "merchant order mismatch",
			inner:  `{"code":"10000","msg":"Success","out_trade_no":"different-merchant-order","qr_code":"https://qr.example.com/wrong-order"}`,
			signer: platformPrivate, includeSign: true, wantError: true,
		},
		{
			name:   "missing merchant order",
			inner:  `{"code":"10000","msg":"Success","qr_code":"https://qr.example.com/missing-order"}`,
			signer: platformPrivate, includeSign: true, wantError: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withPaymentHTTPTransport(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
				body := alipayCreateTestEnvelope(t, test.inner, test.signer, test.includeSign)
				status := test.status
				if status == 0 {
					status = http.StatusOK
				}
				return paymentTestResponse(request, status, body, nil), nil
			}))
			codeURL, err := createAlipayPrecreatePayment(transaction, order, channel)
			if test.wantError {
				if err == nil {
					t.Fatalf("untrusted Alipay create response returned QR %q", codeURL)
				}
				if isDeterministicProviderRejection(err) {
					t.Fatalf("untrusted Alipay create response was classified deterministic: %v", err)
				}
				return
			}
			if err != nil || codeURL != test.wantURL {
				t.Fatalf("signed Alipay create response = %q, %v; want %q", codeURL, err, test.wantURL)
			}
		})
	}
}

func TestPaymentCreateProviderBusinessCodesReleaseOnlyProvenRejections(t *testing.T) {
	t.Run("wechat", func(t *testing.T) {
		merchantPrivate, _ := newWebhookTestRSAKey(t)
		platformPrivate, platformPublicPEM := newWebhookTestRSAKey(t)
		transaction, order := paymentCreateTestFacts(model.PaymentProviderWechat)
		channel := wechatPaymentChannelSettingValue{
			AppID: "wechat-app", MerchantID: "wechat-merchant", MerchantSerialNo: "merchant-serial",
			MerchantPrivateKey: rsaPrivateKeyPEM(t, merchantPrivate), WechatpayPublicKeyID: "PUB_KEY_ID_3000000001", WechatpayPublicKey: platformPublicPEM,
			NotifyURL: "https://merchant.example.com/wechat", GatewayURL: "https://api.mch.weixin.qq.com",
		}
		tests := []struct {
			code          string
			status        int
			deterministic bool
		}{
			{code: "PARAM_ERROR", status: http.StatusBadRequest, deterministic: true},
			{code: "OUT_TRADE_NO_USED", status: http.StatusBadRequest, deterministic: false},
			{code: "SYSTEM_ERROR", status: http.StatusInternalServerError, deterministic: false},
			{code: "UNKNOWN_CODE", status: http.StatusBadRequest, deterministic: false},
		}
		for _, test := range tests {
			t.Run(test.code, func(t *testing.T) {
				withPaymentHTTPTransport(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
					body := fmt.Sprintf(`{"code":%q,"message":"provider response"}`, test.code)
					return signedWechatTestResponse(t, request, test.status, body, platformPrivate), nil
				}))
				_, err := createWechatNativePayment(transaction, order, channel)
				if err == nil {
					t.Fatal("provider rejection unexpectedly returned a QR")
				}
				if got := isDeterministicProviderRejection(err); got != test.deterministic {
					t.Fatalf("code %s deterministic = %v, want %v; err=%v", test.code, got, test.deterministic, err)
				}
			})
		}
	})

	t.Run("alipay", func(t *testing.T) {
		merchantPrivate, _ := newWebhookTestRSAKey(t)
		platformPrivate, platformPublicPEM := newWebhookTestRSAKey(t)
		transaction, order := paymentCreateTestFacts(model.PaymentProviderAlipay)
		channel := alipayPaymentChannelSettingValue{
			AppID: "alipay-app", MerchantID: "alipay-merchant",
			MerchantPrivateKey: rsaPrivateKeyPEM(t, merchantPrivate), PlatformPublicKey: platformPublicPEM,
			NotifyURL: "https://merchant.example.com/alipay", GatewayURL: "https://openapi.alipay.com/gateway.do",
		}
		tests := []struct {
			name          string
			code          string
			subCode       string
			deterministic bool
		}{
			{name: "invalid parameter", code: "40004", subCode: "ACQ.INVALID_PARAMETER", deterministic: true},
			{name: "provider unknown code", code: "20000", subCode: "", deterministic: false},
			{name: "unknown subcode", code: "40004", subCode: "UNKNOWN", deterministic: false},
			{name: "system error", code: "40004", subCode: "ACQ.SYSTEM_ERROR", deterministic: false},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				inner := fmt.Sprintf(`{"code":%q,"msg":"provider response","sub_code":%q,"sub_msg":"provider response"}`, test.code, test.subCode)
				withPaymentHTTPTransport(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
					return paymentTestResponse(request, http.StatusOK, alipayCreateTestEnvelope(t, inner, platformPrivate, true), nil), nil
				}))
				_, err := createAlipayPrecreatePayment(transaction, order, channel)
				if err == nil {
					t.Fatal("provider rejection unexpectedly returned a QR")
				}
				if got := isDeterministicProviderRejection(err); got != test.deterministic {
					t.Fatalf("code/subcode %s/%s deterministic = %v, want %v; err=%v", test.code, test.subCode, got, test.deterministic, err)
				}
			})
		}
	})
}

func paymentCreateTestFacts(provider model.PaymentProvider) (*model.PaymentTransaction, paymentOrderReference) {
	now := time.Now().UTC()
	expiresAt := now.Add(15 * time.Minute)
	return &model.PaymentTransaction{
		ID: "create-test-transaction", OrderType: model.PaymentOrderMembership, OrderID: "create-test-order",
		UserID: "create-test-user", Provider: provider, MerchantOrderNo: "MCREATETESTORDER",
		AmountCents: 1990, Currency: "CNY", Status: model.PaymentTransactionCreated,
		ExpiresAt: &expiresAt, CreatedAt: now, UpdatedAt: now,
	}, paymentOrderReference{OrderNumber: "M-CREATE-TEST", Description: "Create payment test"}
}

func signedWechatCreateTestResponse(t *testing.T, request *http.Request, status int, body string, privateKey *rsa.PrivateKey, signedAt time.Time) *http.Response {
	t.Helper()
	timestamp := strconv.FormatInt(signedAt.Unix(), 10)
	nonce := "wechat-create-response-nonce"
	signature := signWebhookTestMessage(t, privateKey, timestamp+"\n"+nonce+"\n"+body+"\n")
	header := make(http.Header)
	header.Set("Wechatpay-Timestamp", timestamp)
	header.Set("Wechatpay-Nonce", nonce)
	header.Set("Wechatpay-Signature", signature)
	header.Set("Wechatpay-Serial", "PUB_KEY_ID_3000000001")
	return paymentTestResponse(request, status, body, header)
}

func alipayCreateTestEnvelope(t *testing.T, inner string, signer *rsa.PrivateKey, includeSign bool) string {
	t.Helper()
	if !includeSign {
		return fmt.Sprintf(`{"alipay_trade_precreate_response":%s}`, inner)
	}
	signature := signWebhookTestMessage(t, signer, inner)
	return fmt.Sprintf(`{"alipay_trade_precreate_response":%s,"sign":%q}`, inner, signature)
}
