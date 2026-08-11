package main

import (
	"bytes"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"infinite-canvas/backend/internal/handler"

	"github.com/gin-gonic/gin"
)

func TestDevelopmentConfigAllowsBothLocalBrowserOrigins(t *testing.T) {
	repositoryRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	expectedConfiguration := map[string]string{
		".env.example":       "CANVAS_CORS_ORIGINS=http://localhost:3000,http://127.0.0.1:3000",
		"docker-compose.yml": "${CANVAS_CORS_ORIGINS:-http://localhost:3000,http://127.0.0.1:3000}",
	}
	for relativePath, expected := range expectedConfiguration {
		contents, err := os.ReadFile(filepath.Join(repositoryRoot, relativePath))
		if err != nil {
			t.Fatalf("read %s: %v", relativePath, err)
		}
		if !strings.Contains(string(contents), expected) {
			t.Errorf("%s development CORS config does not contain %q", relativePath, expected)
		}
	}
}

func TestAllowedOriginRejectsWildcard(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_CORS_ORIGINS", "*")
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "http://backend/api/health", nil)
	if allowedOrigin(context, "https://example.com") {
		t.Fatal("wildcard CORS must not bypass the explicit origin allowlist")
	}
	if allowedOrigin(context, "ftp://example.com") {
		t.Fatal("non-HTTP origin was allowed")
	}
}

func TestAllowedOriginRequiresConfiguredProductionHTTPSOrigin(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "production")
	t.Setenv("CANVAS_CORS_ORIGINS", "https://canvas.example.com")
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "http://backend/api/health", nil)
	context.Request.Header.Set("X-Forwarded-Host", "canvas.example.com")
	context.Request.Header.Set("X-Forwarded-Proto", "https")
	if allowedOrigin(context, "http://canvas.example.com") {
		t.Fatal("production HTTP origin bypassed the allowlist through forwarded host")
	}
	if !allowedOrigin(context, "https://canvas.example.com") {
		t.Fatal("configured production HTTPS origin was rejected")
	}
	t.Setenv("CANVAS_CORS_ORIGINS", "")
	if allowedOrigin(context, "https://canvas.example.com") {
		t.Fatal("forwarded host was accepted without an explicit configured origin")
	}
}

func TestAllowedOriginRejectsInvalidConfiguredOriginsAndEnvironment(t *testing.T) {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "http://backend/api/health", nil)
	tests := []struct {
		name        string
		environment string
		origin      string
	}{
		{name: "missing environment", origin: "https://canvas.example.com"},
		{name: "unknown environment", environment: "preview", origin: "https://canvas.example.com"},
		{name: "origin path", environment: "production", origin: "https://canvas.example.com/path"},
		{name: "origin userinfo", environment: "production", origin: "https://user@canvas.example.com"},
		{name: "origin query", environment: "production", origin: "https://canvas.example.com?tenant=unsafe"},
		{name: "origin fragment", environment: "production", origin: "https://canvas.example.com#fragment"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CANVAS_ENVIRONMENT", test.environment)
			t.Setenv("CANVAS_CORS_ORIGINS", test.origin)
			if allowedOrigin(context, test.origin) {
				t.Fatalf("invalid configured origin %q was allowed", test.origin)
			}
		})
	}
}

func TestRedactCanvasSharePath(t *testing.T) {
	got := redactSensitiveRequestPath("/api/public/canvas-shares/private-token/resources/resource-1/file")
	if got != "/api/public/canvas-shares/:token/resources/resource-1/file" {
		t.Fatalf("unexpected redacted path: %s", got)
	}
	if got := redactSensitiveRequestPath("/api/tasks"); got != "/api/tasks" {
		t.Fatalf("unrelated path changed: %s", got)
	}
}

func TestSensitivePathRedactionRemovesBearerAndQuery(t *testing.T) {
	const (
		bearerTokenSentinel = "TASK4_BEARER_TOKEN_SENTINEL_GIN_LOG"
		tokenHashSentinel   = "TASK4_TOKEN_HASH_SENTINEL_GIN_QUERY"
		qrURLSentinel       = "TASK4_QR_URL_SENTINEL_GIN_QUERY"
	)
	tests := []struct {
		path string
		want string
	}{
		{
			path: "/api/payments/checkout/" + bearerTokenSentinel + "/transactions?token_hash=" + tokenHashSentinel + "&code_url=https%3A%2F%2Fqr.invalid%2F" + qrURLSentinel,
			want: "/api/payments/checkout/:token/transactions",
		},
		{
			path: "/pay/" + bearerTokenSentinel + "?token_hash=" + tokenHashSentinel,
			want: "/pay/:token",
		},
	}
	for _, test := range tests {
		got := redactSensitiveRequestPath(test.path)
		if got != test.want {
			t.Errorf("redacted path = %q, want %q", got, test.want)
		}
		for _, sentinel := range []string{bearerTokenSentinel, tokenHashSentinel, qrURLSentinel} {
			if strings.Contains(got, sentinel) {
				t.Errorf("redacted path leaked %q: %s", sentinel, got)
			}
		}
	}
}

func TestSensitivePathLoggerOmitsQueryProviderErrorAndReferer(t *testing.T) {
	const (
		bearerTokenSentinel = "TASK4_BEARER_TOKEN_SENTINEL_GIN_MIDDLEWARE"
		tokenHashSentinel   = "TASK4_TOKEN_HASH_SENTINEL_GIN_MIDDLEWARE"
		qrURLSentinel       = "TASK4_QR_URL_SENTINEL_GIN_MIDDLEWARE"
		providerError       = "TASK4_PROVIDER_ERROR_SENTINEL_GIN_MIDDLEWARE"
		refererSentinel     = "TASK4_REFERER_SENTINEL_GIN_MIDDLEWARE"
	)
	var output bytes.Buffer
	router := gin.New()
	router.Use(requestLogger(&output), requestRecovery(&output))
	router.GET("/api/payments/checkout/:token/transactions", func(c *gin.Context) {
		_ = c.Error(errors.New(providerError))
		c.Status(http.StatusBadGateway)
	})
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/payments/checkout/"+bearerTokenSentinel+"/transactions?token_hash="+tokenHashSentinel+"&code_url=https%3A%2F%2Fqr.invalid%2F"+qrURLSentinel,
		nil,
	)
	request.Header.Set("Referer", "https://referer.invalid/"+refererSentinel)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("response status = %d", response.Code)
	}
	logOutput := output.String()
	if !strings.Contains(logOutput, "/api/payments/checkout/:token/transactions") {
		t.Fatalf("request log omitted redacted route template: %s", logOutput)
	}
	for _, sentinel := range []string{
		bearerTokenSentinel, tokenHashSentinel, qrURLSentinel, providerError, refererSentinel,
	} {
		if strings.Contains(logOutput, sentinel) {
			t.Errorf("request log leaked %q: %s", sentinel, logOutput)
		}
	}
}

func TestSensitivePathRecoveryDoesNotDumpBearerRequest(t *testing.T) {
	const (
		bearerTokenSentinel = "TASK4_BEARER_TOKEN_SENTINEL_GIN_RECOVERY"
		querySentinel       = "TASK4_QUERY_SENTINEL_GIN_RECOVERY"
		refererSentinel     = "TASK4_REFERER_SENTINEL_GIN_RECOVERY"
	)
	var output bytes.Buffer
	router := gin.New()
	router.Use(requestLogger(&output), requestRecovery(&output))
	router.GET("/api/payments/checkout/:token", func(*gin.Context) {
		panic(&net.OpError{Op: "write", Net: "tcp", Err: syscall.EPIPE})
	})
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/payments/checkout/"+bearerTokenSentinel+"?trace="+querySentinel,
		nil,
	)
	request.Header.Set("Referer", "https://referer.invalid/"+refererSentinel)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("response status = %d, want 500", response.Code)
	}
	logOutput := output.String()
	if !strings.Contains(logOutput, "panic recovered") || !strings.Contains(logOutput, "/api/payments/checkout/:token") {
		t.Fatalf("recovery log omitted safe diagnostics: %s", logOutput)
	}
	for _, sentinel := range []string{bearerTokenSentinel, querySentinel, refererSentinel} {
		if strings.Contains(logOutput, sentinel) {
			t.Errorf("recovery log leaked %q: %s", sentinel, logOutput)
		}
	}
}

func TestPaymentHeadersApplyBeforeCORSOptionsShortCircuit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("CANVAS_ENVIRONMENT", "production")
	t.Setenv("CANVAS_CORS_ORIGINS", "https://example.com")
	router := gin.New()
	router.Use(handler.PaymentCapabilityHeaders(), cors())
	request := httptest.NewRequest(http.MethodOptions, "/api/payments/checkout/TASK4_BEARER_TOKEN_SENTINEL_OPTIONS", nil)
	request.Header.Set("Origin", "https://example.com")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("response status = %d, want 204", response.Code)
	}
	wants := map[string]string{
		"Cache-Control": "private, no-store", "Pragma": "no-cache", "Referrer-Policy": "no-referrer",
	}
	for headerName, want := range wants {
		if got := response.Header().Get(headerName); got != want {
			t.Errorf("%s = %q, want %q", headerName, got, want)
		}
	}
}

type paymentRuntimeValidatorStub struct {
	events *[]string
	err    error
}

func (stub paymentRuntimeValidatorStub) ValidatePaymentRuntime() error {
	*stub.events = append(*stub.events, "validate")
	return stub.err
}

func TestPaymentRuntimeGateRunsBeforeStartupWork(t *testing.T) {
	t.Run("validation failure blocks startup", func(t *testing.T) {
		events := make([]string, 0, 2)
		err := runPaymentRuntimeGate(paymentRuntimeValidatorStub{events: &events, err: errors.New("invalid runtime")}, func() error {
			events = append(events, "startup")
			return nil
		})
		if err == nil {
			t.Fatal("runtime gate succeeded, want validation error")
		}
		if got := strings.Join(events, ","); got != "validate" {
			t.Fatalf("startup events = %q, want validation only", got)
		}
	})

	t.Run("validation precedes startup", func(t *testing.T) {
		events := make([]string, 0, 2)
		err := runPaymentRuntimeGate(paymentRuntimeValidatorStub{events: &events}, func() error {
			events = append(events, "startup")
			return nil
		})
		if err != nil {
			t.Fatalf("runtime gate failed: %v", err)
		}
		if got := strings.Join(events, ","); got != "validate,startup" {
			t.Fatalf("startup events = %q, want validate,startup", got)
		}
	})
}

func TestNewHTTPServerUsesProductionLimits(t *testing.T) {
	handler := http.NewServeMux()
	server := newHTTPServer(":9000", handler)
	if server.Addr != ":9000" || server.Handler != handler {
		t.Fatal("server address or handler was not preserved")
	}
	if server.ReadHeaderTimeout != 10*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s", server.ReadHeaderTimeout)
	}
	if server.ReadTimeout != 15*time.Minute {
		t.Fatalf("ReadTimeout = %s", server.ReadTimeout)
	}
	if server.WriteTimeout != 65*time.Minute {
		t.Fatalf("WriteTimeout = %s", server.WriteTimeout)
	}
	if server.IdleTimeout != 2*time.Minute {
		t.Fatalf("IdleTimeout = %s", server.IdleTimeout)
	}
	if server.MaxHeaderBytes != 64<<10 {
		t.Fatalf("MaxHeaderBytes = %d", server.MaxHeaderBytes)
	}
}
