package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
)

const kuaiziBalancePath = "/ai-open-platform-api/v1/user/balance"

type KuaiziBalanceFact struct {
	WalletBalanceSubunits string `json:"walletBalanceSubunits"`
	TraceID               string `json:"traceId"`
}

type KuaiziVerificationError struct {
	HealthStatus string
	Code         string
	TraceID      string
}

func (e *KuaiziVerificationError) Error() string {
	if e.TraceID == "" {
		return fmt.Sprintf("筷子凭据验证失败（code=%s）", e.Code)
	}
	return fmt.Sprintf("筷子凭据验证失败（code=%s, trace_id=%s）", e.Code, e.TraceID)
}

type KuaiziClient struct {
	httpClient *http.Client
}

func NewKuaiziClient(httpClient *http.Client) *KuaiziClient {
	return &KuaiziClient{httpClient: httpClient}
}

func (c *KuaiziClient) Balance(ctx context.Context, baseURL string, apiKey string) (KuaiziBalanceFact, error) {
	if c == nil || c.httpClient == nil {
		return KuaiziBalanceFact{}, newKuaiziVerificationError("unknown", "client_unavailable", "")
	}
	if strings.TrimSpace(apiKey) == "" {
		return KuaiziBalanceFact{}, newKuaiziVerificationError("invalid", "missing_key", "")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+kuaiziBalancePath, bytes.NewBufferString("{}"))
	if err != nil {
		return KuaiziBalanceFact{}, newKuaiziVerificationError("unknown", "invalid_request", "")
	}
	request.Header.Set("ApiKey", apiKey)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return KuaiziBalanceFact{}, newKuaiziVerificationError("unavailable", "timeout", "")
		}
		return KuaiziBalanceFact{}, newKuaiziVerificationError("unavailable", "network_error", "")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return KuaiziBalanceFact{}, newKuaiziVerificationError("unavailable", "response_read_error", "")
	}
	if response.StatusCode != http.StatusOK {
		switch response.StatusCode {
		case http.StatusUnauthorized:
			return KuaiziBalanceFact{}, newKuaiziVerificationError("invalid", "invalid_key", "")
		case http.StatusForbidden:
			return KuaiziBalanceFact{}, newKuaiziVerificationError("blocked", "ip_rejected", "")
		}
		return KuaiziBalanceFact{}, newKuaiziVerificationError("unavailable", "upstream_http_"+strconv.Itoa(response.StatusCode), "")
	}

	var envelope struct {
		Code    json.RawMessage `json:"code"`
		Data    json.RawMessage `json:"data"`
		TraceID string          `json:"trace_id"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&envelope); err != nil {
		return KuaiziBalanceFact{}, newKuaiziVerificationError("unknown", "invalid_response", "")
	}
	traceID := safeKuaiziTraceID(envelope.TraceID, apiKey)
	upstreamCode, ok := kuaiziScalarString(envelope.Code)
	if !ok || !validKuaiziUpstreamCode(upstreamCode) {
		return KuaiziBalanceFact{}, newKuaiziVerificationError("unknown", "invalid_response", traceID)
	}
	if upstreamCode != "0" {
		switch upstreamCode {
		case "401":
			return KuaiziBalanceFact{}, newKuaiziVerificationError("invalid", "invalid_key", traceID)
		case "403":
			return KuaiziBalanceFact{}, newKuaiziVerificationError("blocked", "ip_rejected", traceID)
		default:
			return KuaiziBalanceFact{}, newKuaiziVerificationError("rejected", "upstream_code_"+upstreamCode, traceID)
		}
	}
	var data struct {
		Balance json.RawMessage `json:"balance"`
	}
	if len(envelope.Data) == 0 || json.Unmarshal(envelope.Data, &data) != nil {
		return KuaiziBalanceFact{}, newKuaiziVerificationError("unknown", "invalid_response", traceID)
	}
	balance, ok := kuaiziScalarString(data.Balance)
	if !ok || !validNonNegativeDecimalInteger(balance) {
		return KuaiziBalanceFact{}, newKuaiziVerificationError("unknown", "invalid_response", traceID)
	}
	return KuaiziBalanceFact{WalletBalanceSubunits: balance, TraceID: traceID}, nil
}

func newKuaiziVerificationError(healthStatus string, code string, traceID string) *KuaiziVerificationError {
	return &KuaiziVerificationError{HealthStatus: healthStatus, Code: code, TraceID: traceID}
}

func kuaiziScalarString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var text string
	if raw[0] == '"' {
		if err := json.Unmarshal(raw, &text); err != nil {
			return "", false
		}
		text = strings.TrimSpace(text)
		return text, text != ""
	}
	text = strings.TrimSpace(string(raw))
	if text == "" {
		return "", false
	}
	return text, true
}

func validNonNegativeDecimalInteger(value string) bool {
	integer, ok := new(big.Int).SetString(value, 10)
	return ok && integer.Sign() >= 0 && integer.String() == value
}

func validKuaiziUpstreamCode(value string) bool {
	if value == "" || len(value) > 20 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func safeKuaiziTraceID(value string, apiKey string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 160 || strings.Contains(value, apiKey) {
		return ""
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("-_.:/", character) {
			continue
		}
		return ""
	}
	return value
}
