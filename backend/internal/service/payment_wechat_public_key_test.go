package service

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
)

func TestWechatPaymentSettingRequiresPublicKeyMode(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*PaymentSettingRequest)
	}{
		{name: "missing public key id", mutate: func(value *PaymentSettingRequest) { value.Wechat.WechatpayPublicKeyID = "" }},
		{name: "legacy platform serial", mutate: func(value *PaymentSettingRequest) { value.Wechat.WechatpayPublicKeyID = "platform-serial" }},
		{name: "missing public key", mutate: func(value *PaymentSettingRequest) { value.Wechat.WechatpayPublicKey = "" }},
		{name: "invalid public key", mutate: func(value *PaymentSettingRequest) { value.Wechat.WechatpayPublicKey = "not-a-public-key" }},
		{name: "invalid merchant private key", mutate: func(value *PaymentSettingRequest) { value.Wechat.MerchantPrivateKey = "not-a-private-key" }},
		{name: "short api v3 key", mutate: func(value *PaymentSettingRequest) { value.Wechat.APIv3Key = "short" }},
		{name: "non alphanumeric api v3 key", mutate: func(value *PaymentSettingRequest) { value.Wechat.APIv3Key = strings.Repeat("k", 31) + "-" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svc, admin, _ := newPaymentSettingSecurityService(t)
			t.Setenv("CANVAS_ENVIRONMENT", "production")
			t.Setenv("CANVAS_CORS_ORIGINS", "https://hm.kunagent.com")
			request := validWechatPublicKeyPaymentSetting(t)
			test.mutate(&request)
			if _, err := svc.UpdatePaymentSetting(admin, request); err == nil {
				t.Fatal("invalid WeChat public-key configuration was accepted")
			}
		})
	}
}

func TestWechatPaymentSettingPublicProjection(t *testing.T) {
	svc, admin, _ := newPaymentSettingSecurityService(t)
	t.Setenv("CANVAS_ENVIRONMENT", "production")
	t.Setenv("CANVAS_CORS_ORIGINS", "https://hm.kunagent.com")

	result, err := svc.UpdatePaymentSetting(admin, validWechatPublicKeyPaymentSetting(t))
	if err != nil {
		t.Fatalf("save valid WeChat public-key setting: %v", err)
	}
	if !result.Wechat.Ready || result.Wechat.WechatpayPublicKeyID != "PUB_KEY_ID_3000000001" {
		t.Fatalf("unexpected public WeChat setting: %+v", result.Wechat)
	}
	if !result.Wechat.HasMerchantPrivateKey || !result.Wechat.HasWechatpayPublicKey || !result.Wechat.HasAPIv3Key {
		t.Fatalf("configured secret facts are incomplete: %+v", result.Wechat)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"merchantPrivateKey":`, `"wechatpayPublicKey":`, `"apiV3Key":`, "BEGIN PUBLIC KEY", "BEGIN RSA PRIVATE KEY"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("public payment setting leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestDisabledWechatPaymentSettingAllowsIncompleteDraft(t *testing.T) {
	svc, admin, _ := newPaymentSettingSecurityService(t)
	t.Setenv("CANVAS_ENVIRONMENT", "production")
	t.Setenv("CANVAS_CORS_ORIGINS", "https://hm.kunagent.com")

	result, err := svc.UpdatePaymentSetting(admin, PaymentSettingRequest{
		CheckoutBaseURL: "https://hm.kunagent.com",
		Wechat: WechatPaymentChannelSettingRequest{
			AppID: "draft-app-id",
		},
	})
	if err != nil {
		t.Fatalf("save disabled WeChat draft: %v", err)
	}
	if result.Wechat.Ready {
		t.Fatal("incomplete disabled WeChat draft was reported ready")
	}
}

func validWechatPublicKeyPaymentSetting(t *testing.T) PaymentSettingRequest {
	t.Helper()
	merchantPrivateKey, _ := newWebhookTestRSAKey(t)
	_, wechatpayPublicKey := newWebhookTestRSAKey(t)
	return PaymentSettingRequest{
		CheckoutBaseURL: "https://hm.kunagent.com",
		Wechat: WechatPaymentChannelSettingRequest{
			Enabled:              true,
			AppID:                "wx-app-id",
			MerchantID:           "1900000001",
			MerchantSerialNo:     "merchant-serial",
			MerchantPrivateKey:   rsaPrivateKeyPEM(t, merchantPrivateKey),
			WechatpayPublicKeyID: "PUB_KEY_ID_3000000001",
			WechatpayPublicKey:   wechatpayPublicKey,
			APIv3Key:             "0123456789ABCDEF0123456789ABCDEF",
			NotifyURL:            "https://hm.kunagent.com/api/payments/webhooks/wechat",
			GatewayURL:           "https://api.mch.weixin.qq.com",
		},
	}
}

func TestWechatRequestsBindConfiguredPublicKeyID(t *testing.T) {
	channel, wechatpayPrivateKey := wechatPublicKeyConnectorSetting(t)
	transaction, order := paymentCreateTestFacts(model.PaymentProviderWechat)

	tests := []struct {
		name      string
		status    int
		body      string
		invoke    func() error
		requestID string
	}{
		{
			name: "native create", status: http.StatusOK,
			body: `{"code_url":"weixin://wxpay/public-key-mode"}`, requestID: "wx-create-request",
			invoke: func() error {
				_, err := createWechatNativePayment(transaction, order, channel)
				return err
			},
		},
		{
			name: "query", status: http.StatusOK,
			body:      `{"appid":"wechat-app","mchid":"wechat-merchant","out_trade_no":"MCREATETESTORDER","trade_state":"NOTPAY"}`,
			requestID: "wx-query-request",
			invoke: func() error {
				_, err := queryWechatPayment(transaction, channel)
				return err
			},
		},
		{
			name: "close", status: http.StatusNoContent, body: "", requestID: "wx-close-request",
			invoke: func() error { return closeWechatPayment(transaction, channel) },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withPaymentHTTPTransport(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if got := request.Header.Get("Wechatpay-Serial"); got != channel.WechatpayPublicKeyID {
					t.Fatalf("Wechatpay-Serial = %q, want %q", got, channel.WechatpayPublicKeyID)
				}
				return signedWechatPublicKeyResponse(t, request, test.status, test.body, wechatpayPrivateKey, channel.WechatpayPublicKeyID, test.requestID), nil
			}))
			if err := test.invoke(); err != nil {
				t.Fatalf("%s failed: %v", test.name, err)
			}
		})
	}
}

func TestWechatResponseRejectsMismatchedPublicKeyID(t *testing.T) {
	for _, serial := range []string{"PUB_KEY_ID_FOREIGN", " PUB_KEY_ID_3000000001 "} {
		t.Run(serial, func(t *testing.T) {
			channel, wechatpayPrivateKey := wechatPublicKeyConnectorSetting(t)
			transaction, order := paymentCreateTestFacts(model.PaymentProviderWechat)
			withPaymentHTTPTransport(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
				return signedWechatPublicKeyResponse(
					t, request, http.StatusOK, `{"code_url":"weixin://wxpay/foreign-key"}`,
					wechatpayPrivateKey, serial, "wx-foreign-key-request",
				), nil
			}))
			if _, err := createWechatNativePayment(transaction, order, channel); err == nil {
				t.Fatalf("correctly signed response with public-key ID %q was accepted", serial)
			}
		})
	}
}

func TestWechatResponseLogsOnlySafeRequestMetadata(t *testing.T) {
	channel, wechatpayPrivateKey := wechatPublicKeyConnectorSetting(t)
	tests := []struct {
		name      string
		status    int
		requestID string
		body      string
	}{
		{name: "success", status: http.StatusOK, requestID: "wx-request-123", body: `{"code_url":"weixin://wxpay/LOG_SECRET"}`},
		{name: "signed provider rejection", status: http.StatusBadRequest, requestID: "wx-request-400", body: `{"code":"PARAM_ERROR","message":"PROVIDER_ERROR_SECRET"}`},
		{name: "missing request id", status: http.StatusOK, body: `{}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var captured bytes.Buffer
			previousWriter := log.Writer()
			previousFlags := log.Flags()
			log.SetOutput(&captured)
			log.SetFlags(0)
			t.Cleanup(func() {
				log.SetOutput(previousWriter)
				log.SetFlags(previousFlags)
			})

			withPaymentHTTPTransport(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
				return signedWechatPublicKeyResponse(t, request, test.status, test.body, wechatpayPrivateKey, channel.WechatpayPublicKeyID, test.requestID), nil
			}))
			request, err := http.NewRequest(http.MethodGet, "https://api.mch.weixin.qq.com/v3/pay/transactions/out-trade-no/MERCHANT_ORDER_SECRET", nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "AUTHORIZATION_SECRET")
			_, _, _, _ = doSignedWechatRequest(request, "query", channel)

			output := captured.String()
			for _, expected := range []string{
				"payment_provider_response provider=wechat", "operation=query",
				"status=" + strconv.Itoa(test.status), "request_id=" + strconv.Quote(test.requestID),
				"request_id_missing=" + strconv.FormatBool(test.requestID == ""),
			} {
				if !strings.Contains(output, expected) {
					t.Fatalf("safe provider log %q does not contain %q", output, expected)
				}
			}
			for _, forbidden := range []string{
				"AUTHORIZATION_SECRET", "MERCHANT_ORDER_SECRET", "LOG_SECRET", "PROVIDER_ERROR_SECRET", "weixin://", "/v3/pay/",
			} {
				if strings.Contains(output, forbidden) {
					t.Fatalf("safe provider log leaked %q: %s", forbidden, output)
				}
			}
		})
	}
}

func TestWechatWebhookRequiresConfiguredPublicKeyID(t *testing.T) {
	for _, serial := range []string{"", "PUB_KEY_ID_FOREIGN", " PUB_KEY_ID_3000000001 "} {
		t.Run("serial="+serial, func(t *testing.T) {
			svc, db := newMembershipTestService(t)
			t.Setenv("CANVAS_ENVIRONMENT", "production")
			t.Setenv("CANVAS_CORS_ORIGINS", "https://hm.kunagent.com")
			admin := &model.User{ID: "wechat-public-key-admin", Username: "wechat-public-key-admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
			if err := db.Create(admin).Error; err != nil {
				t.Fatal(err)
			}
			wechatpayPrivateKey, wechatpayPublicKey := newWebhookTestRSAKey(t)
			setting := validWechatPublicKeyPaymentSetting(t)
			setting.Wechat.WechatpayPublicKey = wechatpayPublicKey
			if _, err := svc.UpdatePaymentSetting(admin, setting); err != nil {
				t.Fatalf("save payment setting: %v", err)
			}

			body := validWechatPublicKeyWebhookBody(t, setting.Wechat.APIv3Key)
			timestamp := strconv.FormatInt(time.Now().Unix(), 10)
			nonce := "wechat-public-key-webhook-nonce"
			err := svc.HandleWechatPaymentWebhook(WechatPaymentWebhookHeaders{
				Serial: serial, Timestamp: timestamp, Nonce: nonce,
				Signature: signWebhookTestMessage(t, wechatpayPrivateKey, timestamp+"\n"+nonce+"\n"+string(body)+"\n"),
			}, body)
			if err == nil {
				t.Fatal("webhook with missing or foreign public-key ID was accepted")
			}

			var eventCount int64
			if countErr := db.Model(&model.PaymentWebhookEvent{}).Count(&eventCount).Error; countErr != nil || eventCount != 0 {
				t.Fatalf("invalid serial webhook event count = %d, err=%v", eventCount, countErr)
			}
			var subscriptionCount int64
			if countErr := db.Model(&model.MembershipSubscription{}).Count(&subscriptionCount).Error; countErr != nil || subscriptionCount != 0 {
				t.Fatalf("invalid serial subscription count = %d, err=%v", subscriptionCount, countErr)
			}
			var ledgerCount int64
			if countErr := db.Model(&model.CreditLedgerEntry{}).Count(&ledgerCount).Error; countErr != nil || ledgerCount != 0 {
				t.Fatalf("invalid serial credit ledger count = %d, err=%v", ledgerCount, countErr)
			}
			var paidTransactions int64
			if countErr := db.Model(&model.PaymentTransaction{}).Where("status = ?", model.PaymentTransactionPaid).Count(&paidTransactions).Error; countErr != nil {
				t.Fatalf("count paid transactions: %v", countErr)
			}
			if paidTransactions != 0 {
				t.Fatalf("invalid serial created %d paid transactions", paidTransactions)
			}
		})
	}
}

func wechatPublicKeyConnectorSetting(t *testing.T) (wechatPaymentChannelSettingValue, *rsa.PrivateKey) {
	t.Helper()
	merchantPrivateKey, _ := newWebhookTestRSAKey(t)
	wechatpayPrivateKey, wechatpayPublicKey := newWebhookTestRSAKey(t)
	return wechatPaymentChannelSettingValue{
		Enabled: true, AppID: "wechat-app", MerchantID: "wechat-merchant", MerchantSerialNo: "merchant-serial",
		MerchantPrivateKey:   rsaPrivateKeyPEM(t, merchantPrivateKey),
		WechatpayPublicKeyID: "PUB_KEY_ID_3000000001", WechatpayPublicKey: wechatpayPublicKey,
		APIv3Key: strings.Repeat("k", 32), NotifyURL: "https://merchant.example.com/wechat",
		GatewayURL: "https://api.mch.weixin.qq.com",
	}, wechatpayPrivateKey
}

func signedWechatPublicKeyResponse(
	t *testing.T,
	request *http.Request,
	status int,
	body string,
	privateKey *rsa.PrivateKey,
	serial string,
	requestID string,
) *http.Response {
	t.Helper()
	response := signedWechatCreateTestResponse(t, request, status, body, privateKey, time.Now())
	response.Header.Set("Wechatpay-Serial", serial)
	if requestID != "" {
		response.Header.Set("Request-ID", requestID)
	}
	return response
}

func validWechatPublicKeyWebhookBody(t *testing.T, apiV3Key string) []byte {
	t.Helper()
	notification := wechatPaymentNotification{
		AppID: "wx-app-id", MerchantID: "1900000001", MerchantOrderNo: "UNKNOWN-WECHAT-MERCHANT-ORDER",
		TransactionID: "wechat-public-key-trade", TradeState: "SUCCESS", SuccessTime: time.Now().UTC().Format(time.RFC3339),
	}
	notification.Amount.Total = 100
	notification.Amount.Currency = "CNY"
	plaintext, err := json.Marshal(notification)
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher([]byte(apiV3Key))
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := []byte("0123456789ab")
	associatedData := "wechat-public-key-associated-data"
	ciphertext := gcm.Seal(nil, nonce, plaintext, []byte(associatedData))
	envelope := wechatWebhookEnvelope{ID: "wechat-public-key-event", EventType: "TRANSACTION.SUCCESS"}
	envelope.Resource.Algorithm = "AEAD_AES_256_GCM"
	envelope.Resource.Ciphertext = base64.StdEncoding.EncodeToString(ciphertext)
	envelope.Resource.Nonce = string(nonce)
	envelope.Resource.AssociatedData = associatedData
	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
