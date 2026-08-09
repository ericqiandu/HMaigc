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

func TestPaymentCheckoutBearerResponseIsNotCacheable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.SystemSetting{}, &model.MembershipOrder{}, &model.PaymentCheckoutSession{}, &model.PaymentTransaction{},
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
	if err := db.Create(&model.SystemSetting{Key: "payment", ValueJSON: `{"checkoutBaseUrl":"https://checkout.example.com","wechat":{},"alipay":{}}`}).Error; err != nil {
		t.Fatal(err)
	}
	token := "public-checkout-token"
	digest := sha256.Sum256([]byte(token))
	if err := db.Create(&model.PaymentCheckoutSession{
		ID: "session-1", OrderType: model.PaymentOrderMembership, OrderID: order.ID, UserID: order.UserID,
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

	if response.Code != http.StatusOK {
		t.Fatalf("response status = %d, body = %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestPaymentCheckoutBearerErrorDoesNotExposeInternalOrderFacts(t *testing.T) {
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
	token := "public-checkout-error-token"
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
}
