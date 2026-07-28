package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCustomRelayIsDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterCustomRelayRoutes(router.Group("/api"), nil)

	request := httptest.NewRequest(http.MethodPost, "/api/ai/custom", strings.NewReader(`{"model":"test"}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if !strings.Contains(response.Body.String(), errCustomRelayDisabled.Error()) {
		t.Fatalf("body = %q", response.Body.String())
	}
}
