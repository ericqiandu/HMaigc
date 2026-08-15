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
	"time"
)

const (
	kuaiziBalancePath          = "/ai-open-platform-api/v1/user/balance"
	kuaiziBillingPath          = "/ai-open-platform-api/v1/user/billing/list"
	kuaiziBalanceResponseLimit = 64 << 10
)

type KuaiziBalanceFact struct {
	WalletBalanceSubunits string `json:"walletBalanceSubunits"`
	TraceID               string `json:"traceId"`
}

type KuaiziVerificationError struct {
	HealthStatus string
	Code         string
	TraceID      string
}

type KuaiziBillingFact struct {
	OrderID      string
	Amount       int64
	Status       string
	TaskID       string
	TaskStatus   string
	TaskDuration int64
	TotalTokens  int64
	CreatedAt    time.Time
	TraceID      string
}

type KuaiziBillingError struct {
	Code    string
	TraceID string
}

func (e *KuaiziBillingError) Error() string {
	if e.TraceID == "" {
		return fmt.Sprintf("筷子账单查询失败（code=%s）", e.Code)
	}
	return fmt.Sprintf("筷子账单查询失败（code=%s, trace_id=%s）", e.Code, e.TraceID)
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
		return KuaiziBalanceFact{}, newKuaiziVerificationError("client_unavailable", "")
	}
	if strings.TrimSpace(apiKey) == "" {
		return KuaiziBalanceFact{}, newKuaiziVerificationError("missing_key", "")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+kuaiziBalancePath, bytes.NewBufferString("{}"))
	if err != nil {
		return KuaiziBalanceFact{}, newKuaiziVerificationError("invalid_request", "")
	}
	request.Header.Set("ApiKey", apiKey)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return KuaiziBalanceFact{}, newKuaiziVerificationError("timeout", "")
		}
		return KuaiziBalanceFact{}, newKuaiziVerificationError("network_error", "")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		switch response.StatusCode {
		case http.StatusUnauthorized:
			return KuaiziBalanceFact{}, newKuaiziVerificationError("invalid_key", "")
		case http.StatusForbidden:
			return KuaiziBalanceFact{}, newKuaiziVerificationError("ip_rejected", "")
		}
		return KuaiziBalanceFact{}, newKuaiziVerificationError("upstream_http_"+strconv.Itoa(response.StatusCode), "")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, kuaiziBalanceResponseLimit+1))
	if err != nil {
		return KuaiziBalanceFact{}, newKuaiziVerificationError("response_read_error", "")
	}
	if len(body) > kuaiziBalanceResponseLimit {
		return KuaiziBalanceFact{}, newKuaiziVerificationError("invalid_response", "")
	}

	var envelope struct {
		Code    json.RawMessage `json:"code"`
		Data    json.RawMessage `json:"data"`
		TraceID string          `json:"trace_id"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&envelope); err != nil {
		return KuaiziBalanceFact{}, newKuaiziVerificationError("invalid_response", "")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return KuaiziBalanceFact{}, newKuaiziVerificationError("invalid_response", "")
	}
	traceID := safeKuaiziTraceID(envelope.TraceID, apiKey)
	upstreamCode, ok := kuaiziScalarString(envelope.Code)
	if !ok || !validKuaiziUpstreamCode(upstreamCode) {
		return KuaiziBalanceFact{}, newKuaiziVerificationError("invalid_response", traceID)
	}
	if upstreamCode != "0" {
		switch upstreamCode {
		case "401":
			return KuaiziBalanceFact{}, newKuaiziVerificationError("invalid_key", traceID)
		case "403":
			return KuaiziBalanceFact{}, newKuaiziVerificationError("ip_rejected", traceID)
		default:
			return KuaiziBalanceFact{}, newKuaiziVerificationError("upstream_code_"+upstreamCode, traceID)
		}
	}
	var data struct {
		WalletBalance json.RawMessage `json:"wallet_balance"`
	}
	if len(envelope.Data) == 0 || json.Unmarshal(envelope.Data, &data) != nil {
		return KuaiziBalanceFact{}, newKuaiziVerificationError("invalid_response", traceID)
	}
	balance, ok := kuaiziScalarString(data.WalletBalance)
	if !ok || !validNonNegativeDecimalInteger(balance) {
		return KuaiziBalanceFact{}, newKuaiziVerificationError("invalid_response", traceID)
	}
	return KuaiziBalanceFact{WalletBalanceSubunits: balance, TraceID: traceID}, nil
}

func (c *KuaiziClient) BillingByTaskID(ctx context.Context, baseURL string, apiKey string, taskID string) (KuaiziBillingFact, error) {
	if c == nil || c.httpClient == nil {
		return KuaiziBillingFact{}, newKuaiziBillingError("client_unavailable", "")
	}
	if strings.TrimSpace(apiKey) == "" {
		return KuaiziBillingFact{}, newKuaiziBillingError("missing_key", "")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || len(taskID) > 255 {
		return KuaiziBillingFact{}, newKuaiziBillingError("invalid_task_id", "")
	}
	payload, err := json.Marshal(struct {
		TaskID   string `json:"task_id"`
		Page     int    `json:"page"`
		PageSize int    `json:"page_size"`
	}{TaskID: taskID, Page: 1, PageSize: 20})
	if err != nil {
		return KuaiziBillingFact{}, newKuaiziBillingError("invalid_request", "")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+kuaiziBillingPath, bytes.NewReader(payload))
	if err != nil {
		return KuaiziBillingFact{}, newKuaiziBillingError("invalid_request", "")
	}
	request.Header.Set("ApiKey", apiKey)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return KuaiziBillingFact{}, newKuaiziBillingError("timeout", "")
		}
		return KuaiziBillingFact{}, newKuaiziBillingError("network_error", "")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return KuaiziBillingFact{}, newKuaiziBillingError(kuaiziHTTPErrorCode(response.StatusCode), "")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, kuaiziBalanceResponseLimit+1))
	if err != nil {
		return KuaiziBillingFact{}, newKuaiziBillingError("response_read_error", "")
	}
	if len(body) > kuaiziBalanceResponseLimit {
		return KuaiziBillingFact{}, newKuaiziBillingError("invalid_response", "")
	}
	var envelope struct {
		Code    json.RawMessage `json:"code"`
		Data    json.RawMessage `json:"data"`
		TraceID string          `json:"trace_id"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&envelope); err != nil {
		return KuaiziBillingFact{}, newKuaiziBillingError("invalid_response", "")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return KuaiziBillingFact{}, newKuaiziBillingError("invalid_response", "")
	}
	traceID := safeKuaiziTraceID(envelope.TraceID, apiKey)
	upstreamCode, ok := kuaiziScalarString(envelope.Code)
	if !ok || !validKuaiziUpstreamCode(upstreamCode) {
		return KuaiziBillingFact{}, newKuaiziBillingError("invalid_response", traceID)
	}
	if upstreamCode != "0" {
		return KuaiziBillingFact{}, newKuaiziBillingError(kuaiziBusinessErrorCode(upstreamCode), traceID)
	}
	var data struct {
		Items []struct {
			OrderID      string          `json:"order_id"`
			Amount       json.RawMessage `json:"amount"`
			Status       string          `json:"status"`
			TaskID       string          `json:"task_id"`
			TaskStatus   string          `json:"task_status"`
			TaskDuration json.RawMessage `json:"task_duration"`
			TotalTokens  json.RawMessage `json:"total_tokens"`
			CreatedAt    string          `json:"created_at"`
		} `json:"items"`
	}
	if len(envelope.Data) == 0 || json.Unmarshal(envelope.Data, &data) != nil {
		return KuaiziBillingFact{}, newKuaiziBillingError("invalid_response", traceID)
	}
	matches := make([]KuaiziBillingFact, 0, 1)
	for _, item := range data.Items {
		if item.TaskID != taskID {
			continue
		}
		fact, ok := parseKuaiziBillingFact(item.OrderID, item.Amount, item.Status, item.TaskID, item.TaskStatus, item.TaskDuration, item.TotalTokens, item.CreatedAt, traceID)
		if !ok {
			return KuaiziBillingFact{}, newKuaiziBillingError("invalid_response", traceID)
		}
		matches = append(matches, fact)
	}
	if len(matches) == 0 {
		return KuaiziBillingFact{}, newKuaiziBillingError("billing_not_found", traceID)
	}
	if len(matches) != 1 {
		return KuaiziBillingFact{}, newKuaiziBillingError("billing_ambiguous", traceID)
	}
	return matches[0], nil
}

func parseKuaiziBillingFact(orderID string, amountRaw json.RawMessage, status string, taskID string, taskStatus string, durationRaw json.RawMessage, tokensRaw json.RawMessage, createdAtRaw string, traceID string) (KuaiziBillingFact, bool) {
	amount, amountOK := kuaiziNonNegativeInt64(amountRaw)
	duration, durationOK := kuaiziNonNegativeInt64(durationRaw)
	tokens, tokensOK := kuaiziNonNegativeInt64(tokensRaw)
	createdAt, timeErr := time.Parse(time.RFC3339, createdAtRaw)
	if strings.TrimSpace(orderID) == "" || len(orderID) > 255 || !amountOK || !durationOK || !tokensOK || tokens > int64(^uint32(0)) || !validKuaiziBillingStatus(status) || !validKuaiziTaskStatus(taskStatus) || timeErr != nil {
		return KuaiziBillingFact{}, false
	}
	return KuaiziBillingFact{OrderID: orderID, Amount: amount, Status: status, TaskID: taskID, TaskStatus: taskStatus, TaskDuration: duration, TotalTokens: tokens, CreatedAt: createdAt, TraceID: traceID}, true
}

func kuaiziNonNegativeInt64(raw json.RawMessage) (int64, bool) {
	value, ok := kuaiziScalarString(raw)
	if !ok || !validNonNegativeDecimalInteger(value) {
		return 0, false
	}
	integer, ok := new(big.Int).SetString(value, 10)
	if !ok || !integer.IsInt64() {
		return 0, false
	}
	return integer.Int64(), true
}

func validKuaiziBillingStatus(status string) bool {
	return status == "pending" || status == "succeeded" || status == "failed"
}

func validKuaiziTaskStatus(status string) bool {
	return status == "pending" || status == "submitted" || status == "running" || status == "succeeded" || status == "failed"
}

func kuaiziHTTPErrorCode(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "invalid_key"
	case http.StatusForbidden:
		return "ip_rejected"
	default:
		return "upstream_http_" + strconv.Itoa(status)
	}
}

func kuaiziBusinessErrorCode(code string) string {
	switch code {
	case "401":
		return "invalid_key"
	case "403":
		return "ip_rejected"
	default:
		return "upstream_code_" + code
	}
}

func newKuaiziBillingError(code string, traceID string) *KuaiziBillingError {
	return &KuaiziBillingError{Code: code, TraceID: traceID}
}

func newKuaiziVerificationError(code string, traceID string) *KuaiziVerificationError {
	return &KuaiziVerificationError{HealthStatus: kuaiziHealthStatusForCode(code), Code: code, TraceID: traceID}
}

func kuaiziHealthStatusForCode(code string) string {
	switch code {
	case "missing_key", "invalid_key":
		return "invalid"
	case "ip_rejected":
		return "blocked"
	case "timeout", "network_error", "response_read_error":
		return "unavailable"
	}
	if strings.HasPrefix(code, "upstream_http_") {
		return "unavailable"
	}
	if strings.HasPrefix(code, "upstream_code_") {
		return "rejected"
	}
	return "unknown"
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
