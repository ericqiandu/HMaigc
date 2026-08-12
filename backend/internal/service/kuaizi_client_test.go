package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestKuaiziBalanceSendsExactContractAndPreservesLargeBalance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/ai-open-platform-api/v1/user/balance" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		if got := request.Header.Get("ApiKey"); got != "sentinel-key" {
			t.Fatalf("ApiKey = %q", got)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != "{}" {
			t.Fatalf("body = %q, want {}", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"code":0,"data":{"wallet_balance":9007199254740993123456789},"trace_id":"trace-large"}`)
	}))
	defer server.Close()

	client := NewKuaiziClient(server.Client())
	fact, err := client.Balance(context.Background(), server.URL, "sentinel-key")
	if err != nil {
		t.Fatal(err)
	}
	if fact.WalletBalanceSubunits != "9007199254740993123456789" || fact.TraceID != "trace-large" {
		t.Fatalf("balance fact = %#v", fact)
	}
}

func TestKuaiziBalanceAcceptsZeroAsVerifiedFact(t *testing.T) {
	fixture, closeServer := kuaiziBalanceTestClient(t, http.StatusOK, `{"code":0,"data":{"wallet_balance":"0"},"trace_id":"trace-zero"}`, 0)
	defer closeServer()
	fact, err := fixture.client.Balance(context.Background(), fixture.baseURL, "key")
	if err != nil {
		t.Fatal(err)
	}
	if fact.WalletBalanceSubunits != "0" {
		t.Fatalf("balance = %q, want 0", fact.WalletBalanceSubunits)
	}
}

func TestKuaiziBalanceRedactsKeyIfUpstreamEchoesItAsTraceID(t *testing.T) {
	const key = "sentinel-echoed-key"
	fixture, closeServer := kuaiziBalanceTestClient(t, http.StatusOK, `{"code":0,"data":{"wallet_balance":"1"},"trace_id":"sentinel-echoed-key"}`, 0)
	defer closeServer()
	fact, err := fixture.client.Balance(context.Background(), fixture.baseURL, key)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fact.TraceID, key) {
		t.Fatalf("balance fact trace leaked key: %#v", fact)
	}
}

func TestKuaiziBalanceMapsExplicitFailures(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		delay      time.Duration
		timeout    time.Duration
		wantCode   string
		wantHealth string
		secretBody string
	}{
		{name: "http non 200", status: http.StatusBadGateway, body: `gateway`, wantCode: "upstream_http_502", wantHealth: "unavailable"},
		{name: "http invalid key", status: http.StatusUnauthorized, body: `unauthorized`, wantCode: "invalid_key", wantHealth: "invalid"},
		{name: "http ip rejected", status: http.StatusForbidden, body: `forbidden`, wantCode: "ip_rejected", wantHealth: "blocked"},
		{name: "oversized http 502", status: http.StatusBadGateway, body: "sentinel-oversized-502" + strings.Repeat("x", kuaiziBalanceResponseLimit), wantCode: "upstream_http_502", wantHealth: "unavailable", secretBody: "sentinel-oversized-502"},
		{name: "oversized http 401", status: http.StatusUnauthorized, body: "sentinel-oversized-401" + strings.Repeat("x", kuaiziBalanceResponseLimit), wantCode: "invalid_key", wantHealth: "invalid", secretBody: "sentinel-oversized-401"},
		{name: "oversized http 403", status: http.StatusForbidden, body: "sentinel-oversized-403" + strings.Repeat("x", kuaiziBalanceResponseLimit), wantCode: "ip_rejected", wantHealth: "blocked", secretBody: "sentinel-oversized-403"},
		{name: "invalid key", status: http.StatusOK, body: `{"code":401,"message":"invalid api key","trace_id":"trace-key"}`, wantCode: "invalid_key", wantHealth: "invalid"},
		{name: "ip rejected", status: http.StatusOK, body: `{"code":403,"message":"ip rejected","trace_id":"trace-ip"}`, wantCode: "ip_rejected", wantHealth: "blocked"},
		{name: "other business code", status: http.StatusOK, body: `{"code":9001,"message":"rejected","trace_id":"trace-business"}`, wantCode: "upstream_code_9001", wantHealth: "rejected"},
		{name: "unsafe business code", status: http.StatusOK, body: `{"code":"sentinel-secret","message":"rejected"}`, wantCode: "invalid_response", wantHealth: "unknown"},
		{name: "timeout", status: http.StatusOK, body: `{"code":0,"data":{"wallet_balance":"1"}}`, delay: 100 * time.Millisecond, timeout: 10 * time.Millisecond, wantCode: "timeout", wantHealth: "unavailable"},
		{name: "unknown payload", status: http.StatusOK, body: `{"code":0,"data":{"credits":"1"}}`, wantCode: "invalid_response", wantHealth: "unknown"},
		{name: "trailing garbage", status: http.StatusOK, body: `{"code":0,"data":{"wallet_balance":"1"}} trailing`, wantCode: "invalid_response", wantHealth: "unknown"},
		{name: "second json value", status: http.StatusOK, body: `{"code":0,"data":{"wallet_balance":"1"}} {}`, wantCode: "invalid_response", wantHealth: "unknown"},
		{name: "oversized payload", status: http.StatusOK, body: `{"code":0,"data":{"wallet_balance":"1"},"trace_id":"sentinel-oversized-200"}` + strings.Repeat(" ", kuaiziBalanceResponseLimit), wantCode: "invalid_response", wantHealth: "unknown", secretBody: "sentinel-oversized-200"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, closeServer := kuaiziBalanceTestClient(t, test.status, test.body, test.delay)
			defer closeServer()
			if test.timeout > 0 {
				fixture.client = NewKuaiziClient(&http.Client{Timeout: test.timeout})
			}
			_, err := fixture.client.Balance(context.Background(), fixture.baseURL, "sentinel-secret")
			var verificationError *KuaiziVerificationError
			if !errors.As(err, &verificationError) {
				t.Fatalf("error = %T %v, want KuaiziVerificationError", err, err)
			}
			if verificationError.Code != test.wantCode || verificationError.HealthStatus != test.wantHealth {
				t.Fatalf("verification error = %#v", verificationError)
			}
			if strings.Contains(err.Error(), "sentinel-secret") {
				t.Fatalf("error leaked key: %v", err)
			}
			if test.secretBody != "" && strings.Contains(err.Error(), test.secretBody) {
				t.Fatalf("error leaked oversized upstream body: %v", err)
			}
		})
	}
}

type kuaiziBalanceFixture struct {
	client  *KuaiziClient
	baseURL string
}

func kuaiziBalanceTestClient(t *testing.T, status int, body string, delay time.Duration) (kuaiziBalanceFixture, func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		_, _ = io.WriteString(writer, body)
	}))
	return kuaiziBalanceFixture{client: NewKuaiziClient(server.Client()), baseURL: server.URL}, server.Close
}
