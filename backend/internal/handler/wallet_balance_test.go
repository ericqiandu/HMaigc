package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

func TestWalletBalanceHandlerReturnsOnlyAuthenticatedAccount(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "wallet-balance.db")+"?_busy_timeout=5000"), &gorm.Config{})
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
	user := model.User{ID: "wallet-balance-user", Username: "wallet-balance-user", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}
	account := model.CreditAccount{UserID: user.ID, AvailableMicrocredits: 12_345_000_000, ReservedMicrocredits: 500_000, Version: 7, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	cookie := createProviderHandlerSession(t, db, user.ID, "wallet-balance-session", "wallet-balance-token", now)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterFinanceRoutes(router.Group("/api"), service.New(repository.New(db), t.TempDir()))

	request := httptest.NewRequest(http.MethodGet, "/api/wallet/balance", nil)
	request.AddCookie(&http.Cookie{Name: service.SessionCookieName, Value: cookie})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("cache-control = %q", response.Header().Get("Cache-Control"))
	}
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Account model.CreditAccount `json:"account"`
			Entries json.RawMessage     `json:"entries"`
			Policy  json.RawMessage     `json:"policy"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != 0 || envelope.Data.Account.AvailableMicrocredits != account.AvailableMicrocredits {
		t.Fatalf("balance response = %s", response.Body.String())
	}
	if envelope.Data.Entries != nil || envelope.Data.Policy != nil {
		t.Fatalf("balance endpoint returned wallet-detail fields: %s", response.Body.String())
	}

	anonymous := httptest.NewRequest(http.MethodGet, "/api/wallet/balance", nil)
	anonymousResponse := httptest.NewRecorder()
	router.ServeHTTP(anonymousResponse, anonymous)
	if anonymousResponse.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, body = %s", anonymousResponse.Code, anonymousResponse.Body.String())
	}
}
