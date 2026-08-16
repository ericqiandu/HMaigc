package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type billingQuoteHandlerFixture struct {
	router *gin.Engine
	cookie string
}

func newBillingQuoteHandlerFixture(t *testing.T) billingQuoteHandlerFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "quote-handler.db")+"?_busy_timeout=5000"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	user := model.User{ID: "quote-user", Username: "quote-user", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}
	channel := model.ModelChannel{ID: "channel", Scope: model.ChannelScopeSystem, Enabled: true}
	item := model.ChannelModel{
		ID: "image", ChannelID: channel.ID, ModelKey: "gpt-image-2", Capability: "image",
		AccessPolicy: model.ModelAccessAuthenticated, BillingMode: "fixed_request", PriceStrategy: "image_resolution",
		PriceConfigured: true, Enabled: true, PriceVersion: 4,
		PriceTiers: []model.ChannelModelPriceTier{{ID: "image-1k", Resolution: "1K", UnitPriceMicrocredits: 45_000}},
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	cookie := createProviderHandlerSession(t, db, user.ID, "quote-session", "quote-token", now)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	api := router.Group("/api")
	RegisterFinanceRoutes(api, service.New(repository.New(db), t.TempDir()))
	return billingQuoteHandlerFixture{router: router, cookie: cookie}
}

func (fixture billingQuoteHandlerFixture) request(body string, cookie string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/billing/quotes", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		request.AddCookie(&http.Cookie{Name: service.SessionCookieName, Value: cookie})
	}
	response := httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	return response
}

func TestBillingQuoteHandlerReturnsAuthenticatedStrictQuote(t *testing.T) {
	fixture := newBillingQuoteHandlerFixture(t)
	body := `{"type":"canvas_image","operation":"generate","batchCount":3,"input":{"mode":"image","referenceVideoCount":0,"config":{"channelId":"channel","model":"gpt-image-2","size":"1024x1024","quality":"low","videoSeconds":"","vquality":"","videoSuperResolutionEnabled":false,"videoSuperResolutionResolution":"","videoSuperResolutionVersion":"","videoSuperResolutionFps":0}}}`

	response := fixture.request(body, fixture.cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Code int                      `json:"code"`
		Data service.TaskBillingQuote `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != 0 || envelope.Data.AmountMicrocredits != 135_000 || envelope.Data.TaskCount != 3 || envelope.Data.QuoteFingerprint == "" {
		t.Fatalf("quote response = %s", response.Body.String())
	}

	anonymous := fixture.request(body, "")
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, body = %s", anonymous.Code, anonymous.Body.String())
	}

	unknownField := strings.Replace(body, `"batchCount":3`, `"batchCount":3,"unexpected":true`, 1)
	unknown := fixture.request(unknownField, fixture.cookie)
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d, body = %s", unknown.Code, unknown.Body.String())
	}

	oversized := fixture.request(`{"type":"canvas_image","padding":"`+strings.Repeat("a", (64<<10)+1)+`"}`, fixture.cookie)
	if oversized.Code != http.StatusBadRequest {
		t.Fatalf("oversized status = %d, body = %s", oversized.Code, oversized.Body.String())
	}
}

func TestTaskHandlerReturnsPriceChangedContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	failService(context, &service.QuoteChangedError{CurrentQuote: service.TaskBillingQuote{
		AmountMicrocredits: 120_000, PerTaskAmountMicrocredits: 120_000, TaskCount: 1,
		PriceVersion: 2, BillingMode: "fixed_request", Quantity: 1, QuoteFingerprint: "current",
	}})
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			ErrorCode    string                   `json:"errorCode"`
			CurrentQuote service.TaskBillingQuote `json:"currentQuote"`
		} `json:"data"`
		Message string `json:"msg"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != http.StatusConflict || envelope.Data.ErrorCode != "PRICE_CHANGED" || envelope.Data.CurrentQuote.QuoteFingerprint != "current" {
		t.Fatalf("price-changed response = %s", response.Body.String())
	}
	if envelope.Message != "预计积分已变化，请确认新报价后重试" {
		t.Fatalf("message = %q", envelope.Message)
	}
}
