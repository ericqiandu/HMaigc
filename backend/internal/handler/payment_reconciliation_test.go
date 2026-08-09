package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPaymentReconciliationRouteRequiresAuthenticatedAdmin(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Session{}); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterPaymentRoutes(router.Group("/api"), service.New(repository.New(db), t.TempDir()))
	request := httptest.NewRequest(http.MethodPost, "/api/admin/payments/transactions/payment-1/reconcile", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code == http.StatusNotFound {
		t.Fatalf("payment reconciliation route was not registered: %s", response.Body.String())
	}
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous reconciliation status = %d, want 401", response.Code)
	}
}
