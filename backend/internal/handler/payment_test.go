package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPaymentHeadersOnCheckoutBearerSuccess(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.SystemSetting{}, &model.MembershipOrder{}, &model.PaymentCheckoutSession{},
		&model.PaymentTransaction{}, &model.PaymentWebhookEvent{},
	); err != nil {
		t.Fatal(err)
	}
	plan := model.MembershipPlan{
		ID: "plan-1", Code: "pro", Name: "专业版", Tier: "pro", Audience: model.MembershipAudiencePersonal,
		BillingCycle: model.MembershipBillingCycleMonth, PriceCents: 1200, OriginalPriceCents: 1800,
		Currency: "CNY", CreditsPerPeriod: 300, ImageConcurrency: 1, VideoConcurrency: 1,
	}
	snapshot, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	order := model.MembershipOrder{
		ID: "order-1", OrderNumber: "M202608090001", UserID: "user-1", PlanID: plan.ID, Seats: 1,
		UnitPriceCents: 1200, TotalPriceCents: 1200, Currency: "CNY", Status: model.MembershipOrderPending,
		PlanSnapshotJSON: string(snapshot), CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SystemSetting{Key: "payment", ValueJSON: `{
		"checkoutBaseUrl":"https://checkout.example.com",
		"wechat":{
			"enabled":true,
			"appId":"wx-app-id",
			"merchantId":"merchant-id",
			"merchantSerialNo":"merchant-serial",
			"merchantPrivateKey":"merchant-private-key",
			"platformPublicKey":"platform-public-key",
			"apiV3Key":"api-v3-key",
			"notifyUrl":"https://api.example.com/api/payments/webhooks/wechat",
			"gatewayUrl":"https://api.mch.weixin.qq.com"
		},
		"alipay":{}
	}`}).Error; err != nil {
		t.Fatal(err)
	}
	const token = "TASK4_BEARER_TOKEN_SENTINEL_SUCCESS"
	digest := sha256.Sum256([]byte(token))
	if err := db.Create(&model.PaymentCheckoutSession{
		ID: "session-1", OrderType: model.PaymentOrderMembership, OrderID: order.ID, UserID: order.UserID,
		TokenHash: hex.EncodeToString(digest[:]), Status: model.PaymentCheckoutActive,
		ExpiresAt: now.Add(time.Minute), CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	transactionExpiresAt := now.Add(time.Minute)
	const qrURLSentinel = "https://qr.invalid/TASK4_QR_URL_SENTINEL_HANDLER_RESPONSE"
	if err := db.Create(&model.PaymentTransaction{
		ID: "transaction-1", OrderType: model.PaymentOrderMembership, OrderID: order.ID, UserID: order.UserID,
		Provider: model.PaymentProviderWechat, MerchantOrderNo: "TASK4-MERCHANT-ORDER-1",
		AmountCents: order.TotalPriceCents, Currency: order.Currency, Status: model.PaymentTransactionPending,
		CodeURL: qrURLSentinel, ExpiresAt: &transactionExpiresAt, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	providerFailureOrder := order
	providerFailureOrder.ID = "order-provider-failure"
	providerFailureOrder.OrderNumber = "M202608090003"
	if err := db.Create(&providerFailureOrder).Error; err != nil {
		t.Fatal(err)
	}
	const providerFailureToken = "TASK4_BEARER_TOKEN_SENTINEL_PROVIDER_FAILURE"
	providerFailureDigest := sha256.Sum256([]byte(providerFailureToken))
	if err := db.Create(&model.PaymentCheckoutSession{
		ID: "session-provider-failure", OrderType: model.PaymentOrderMembership, OrderID: providerFailureOrder.ID, UserID: providerFailureOrder.UserID,
		TokenHash: hex.EncodeToString(providerFailureDigest[:]), Status: model.PaymentCheckoutActive,
		ExpiresAt: now.Add(time.Minute), CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterPaymentRoutes(router.Group("/api"), service.New(repository.New(db), t.TempDir()))
	request := httptest.NewRequest(http.MethodGet, "/api/payments/checkout/"+token, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("response status = %d, body = %s", response.Code, response.Body.String())
	}
	assertPaymentCapabilityHeaders(t, response)

	transactionRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/payments/checkout/"+token+"/transactions",
		strings.NewReader(`{"provider":"wechat"}`),
	)
	transactionRequest.Header.Set("Content-Type", "application/json")
	transactionResponse := httptest.NewRecorder()
	router.ServeHTTP(transactionResponse, transactionRequest)
	if transactionResponse.Code != http.StatusOK {
		t.Fatalf("transaction response status = %d, body = %s", transactionResponse.Code, transactionResponse.Body.String())
	}
	if !strings.Contains(transactionResponse.Body.String(), qrURLSentinel) {
		t.Fatalf("transaction response did not return the frozen QR URL: %s", transactionResponse.Body.String())
	}
	assertPaymentCapabilityHeaders(t, transactionResponse)

	providerFailureRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/payments/checkout/"+providerFailureToken+"/transactions",
		strings.NewReader(`{"provider":"wechat"}`),
	)
	providerFailureRequest.Header.Set("Content-Type", "application/json")
	providerFailureResponse := httptest.NewRecorder()
	router.ServeHTTP(providerFailureResponse, providerFailureRequest)
	if providerFailureResponse.Code != http.StatusBadGateway {
		t.Fatalf("provider failure status = %d, body = %s", providerFailureResponse.Code, providerFailureResponse.Body.String())
	}
	assertPaymentCapabilityHeaders(t, providerFailureResponse)
}

func TestPaymentHeadersOnCheckoutBearerServerErrorDoesNotExposeInternalOrderFacts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.SystemSetting{}, &model.MembershipOrder{}, &model.PaymentCheckoutSession{}, &model.PaymentTransaction{},
	); err != nil {
		t.Fatal(err)
	}
	const internalOrderSentinel = "internal-order-id-must-not-leak"
	now := time.Now()
	order := model.MembershipOrder{
		ID: internalOrderSentinel, OrderNumber: "M202608090002", UserID: "user-1", PlanID: "plan-1", Seats: 1,
		UnitPriceCents: 1200, TotalPriceCents: 1200, Currency: "CNY", Status: model.MembershipOrderPending,
		PlanSnapshotJSON: "{", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SystemSetting{Key: "payment", ValueJSON: `{"checkoutBaseUrl":"https://checkout.example.com","wechat":{},"alipay":{}}`}).Error; err != nil {
		t.Fatal(err)
	}
	const token = "TASK4_BEARER_TOKEN_SENTINEL_SERVER_ERROR"
	digest := sha256.Sum256([]byte(token))
	if err := db.Create(&model.PaymentCheckoutSession{
		ID: "session-error", OrderType: model.PaymentOrderMembership, OrderID: order.ID, UserID: order.UserID,
		TokenHash: hex.EncodeToString(digest[:]), Status: model.PaymentCheckoutActive,
		ExpiresAt: now.Add(time.Minute), CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterPaymentRoutes(router.Group("/api"), service.New(repository.New(db), t.TempDir()))
	request := httptest.NewRequest(http.MethodGet, "/api/payments/checkout/"+token, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("response status = %d, body = %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), internalOrderSentinel) {
		t.Fatalf("checkout error leaked internal order ID: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "收银台服务暂时不可用，请稍后重试") {
		t.Fatalf("checkout error body = %s, want stable public error", response.Body.String())
	}
	assertPaymentCapabilityHeaders(t, response)
}

func TestPaymentHeadersCoverClientErrorsAndCheckoutURLCreationFailures(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.AuthSession{}, &model.SystemSetting{}, &model.MembershipOrder{}, &model.MembershipSubscription{},
		&model.CreditLedgerEntry{}, &model.TeamCreditLedgerEntry{}, &model.CreditTopupOrder{},
		&model.PaymentCheckoutSession{}, &model.PaymentTransaction{},
	); err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterPaymentRoutes(router.Group("/api"), service.New(repository.New(db), t.TempDir()))
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{
			name:   "malformed transaction request",
			method: http.MethodPost,
			path:   "/api/payments/checkout/TASK4_BEARER_TOKEN_SENTINEL_CLIENT_ERROR/transactions",
			body:   "{",
		},
		{
			name:   "membership checkout URL creation without session",
			method: http.MethodPost,
			path:   "/api/membership/orders/order-1/checkout",
		},
		{
			name:   "credit checkout URL creation without session",
			method: http.MethodPost,
			path:   "/api/credit-store/orders/order-1/checkout",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			if test.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code < http.StatusBadRequest {
				t.Fatalf("response status = %d, want failure body = %s", response.Code, response.Body.String())
			}
			assertPaymentCapabilityHeaders(t, response)
		})
	}
}

func TestPaymentHeadersOnSuccessfulCheckoutURLCreation(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "production")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.AuthSession{}, &model.SystemSetting{}, &model.MembershipOrder{}, &model.MembershipSubscription{},
		&model.CreditLedgerEntry{}, &model.TeamCreditLedgerEntry{}, &model.CreditTopupOrder{},
		&model.PaymentCheckoutSession{}, &model.PaymentTransaction{},
	); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	user := &model.User{
		ID: "task4-checkout-owner", Username: "task4-owner", Email: "task4-owner@example.com",
		Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	const rawSessionToken = "TASK4_AUTH_SESSION_TOKEN"
	sessionDigest := sha256.Sum256([]byte(rawSessionToken))
	if err := db.Create(&model.AuthSession{
		ID: "task4-auth-session", UserID: user.ID, TokenHash: hex.EncodeToString(sessionDigest[:]),
		ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	plan := model.MembershipPlan{
		ID: "task4-plan", Code: "task4-pro", Name: "Task 4 专业版", Tier: "pro",
		Audience: model.MembershipAudiencePersonal, BillingCycle: model.MembershipBillingCycleMonth,
		PriceCents: 1200, OriginalPriceCents: 1800, Currency: "CNY", CreditsPerPeriod: 300,
		ImageConcurrency: 1, VideoConcurrency: 1,
	}
	planSnapshot, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MembershipOrder{
		ID: "task4-membership-order", OrderNumber: "TASK4-MEMBERSHIP-ORDER", UserID: user.ID,
		PlanID: plan.ID, Seats: 1, UnitPriceCents: 1200, TotalPriceCents: 1200, Currency: "CNY",
		Status: model.MembershipOrderPending, PlanSnapshotJSON: string(planSnapshot), CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CreditTopupOrder{
		ID: "task4-credit-order", OrderNumber: "TASK4-CREDIT-ORDER", UserID: user.ID,
		ProductID: "task4-credit-product", TotalMicrocredits: 1000, TotalPriceCents: 500, Currency: "CNY",
		Status: model.CreditTopupOrderPending, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SystemSetting{Key: "payment", ValueJSON: `{
		"checkoutBaseUrl":"https://checkout.example.com",
		"wechat":{
			"enabled":true,
			"appId":"wx-app-id",
			"merchantId":"merchant-id",
			"merchantSerialNo":"merchant-serial",
			"merchantPrivateKey":"merchant-private-key",
			"platformPublicKey":"platform-public-key",
			"apiV3Key":"api-v3-key",
			"notifyUrl":"https://api.example.com/api/payments/webhooks/wechat",
			"gatewayUrl":"https://api.mch.weixin.qq.com"
		},
		"alipay":{}
	}`}).Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterPaymentRoutes(router.Group("/api"), service.New(repository.New(db), t.TempDir()))
	for _, path := range []string{
		"/api/membership/orders/task4-membership-order/checkout",
		"/api/credit-store/orders/task4-credit-order/checkout",
	} {
		request := httptest.NewRequest(http.MethodPost, path, nil)
		request.AddCookie(&http.Cookie{
			Name: service.SessionCookieName, Value: "task4-auth-session." + rawSessionToken,
		})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s response status = %d, body = %s", path, response.Code, response.Body.String())
		}
		if !strings.Contains(response.Body.String(), `"checkoutUrl":"https://checkout.example.com/pay/`) {
			t.Fatalf("%s did not return a checkout capability URL: %s", path, response.Body.String())
		}
		assertPaymentCapabilityHeaders(t, response)
	}
}

func assertPaymentCapabilityHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	wants := map[string]string{
		"Cache-Control":   "private, no-store",
		"Pragma":          "no-cache",
		"Referrer-Policy": "no-referrer",
	}
	for header, want := range wants {
		if got := response.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}
