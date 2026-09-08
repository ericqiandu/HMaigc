package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
)

func TestImagePromptModelRelayExecutesFrozenProviderAndPersistsInternalCostEvidence(t *testing.T) {
	fixture, request := newImagePromptModelRelayFixture(t)
	var calls atomic.Int32
	fixture.svc.imagePromptModelRelayProvider = func(_ context.Context, config providerConfig, messages []ImagePromptModelRelayMessageV1) (imagePromptModelRelayProviderResult, error) {
		calls.Add(1)
		if config.Model != request.PromptModel.ModelKey || config.APIKey != "prompt-api-key" || len(messages) != 2 {
			t.Fatalf("provider input config=%#v messages=%#v", config, messages)
		}
		return imagePromptModelRelayProviderResult{
			Text: "商业级人像质感提示词", EffectiveModel: request.PromptModel.ModelKey, ProviderRequestID: "prompt-provider-request-1",
			Usage: ImagePromptModelRelayUsageV1{InputTokens: 20, CachedTokens: 2, OutputTokens: 8, TotalTokens: 28}, UsageAvailable: true,
			StatusCode: http.StatusOK, Duration: 15 * time.Millisecond, UpstreamURL: "https://provider.example/chat/completions",
		}, nil
	}
	response, err := fixture.svc.ExecuteImagePromptModelRelay(context.Background(), request)
	if err != nil {
		t.Fatalf("execute model relay: %v", err)
	}
	if calls.Load() != 1 || response.Text != "商业级人像质感提示词" || response.EffectiveModel != request.PromptModel.ModelKey || response.Usage.TotalTokens != 28 {
		t.Fatalf("relay response = %#v calls=%d", response, calls.Load())
	}
	var logs []model.ApiCallLog
	if err := fixture.db.Where("task_id = ? AND source = ?", request.CorrelationID, "image-prompt-model-relay").Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Billable || logs[0].BillingOrderID != request.CommercialContext.BillingOrderID || logs[0].ProviderRequestID != response.ProviderRequestID || !logs[0].UsageAvailable {
		t.Fatalf("internal provider logs = %#v", logs)
	}
	if !logs[0].CostAvailable && strings.TrimSpace(logs[0].CostCalculationError) == "" {
		t.Fatalf("internal provider cost evidence is ambiguous: %#v", logs[0])
	}
	order, err := fixture.svc.repo.BillingOrder(request.CommercialContext.BillingOrderID)
	if err != nil {
		t.Fatal(err)
	}
	if order.ProviderRequestID != "" {
		t.Fatalf("internal cost call mutated image billing provider request = %q", order.ProviderRequestID)
	}
}

func TestImagePromptModelRelayRejectsTerminatedParentBeforeProviderCall(t *testing.T) {
	fixture, request := newImagePromptModelRelayFixture(t)
	if err := fixture.db.Model(&model.Task{}).Where("id = ?", request.CorrelationID).Update("status", model.TaskStatusCancelled).Error; err != nil {
		t.Fatal(err)
	}
	calls := 0
	fixture.svc.imagePromptModelRelayProvider = func(context.Context, providerConfig, []ImagePromptModelRelayMessageV1) (imagePromptModelRelayProviderResult, error) {
		calls++
		return imagePromptModelRelayProviderResult{}, nil
	}
	_, err := fixture.svc.ExecuteImagePromptModelRelay(context.Background(), request)
	var failure *ImagePromptModelRelayError
	if !errors.As(err, &failure) || failure.Code != imagePromptErrorRequestInvalid || calls != 0 {
		t.Fatalf("terminated parent result error=%#v calls=%d", err, calls)
	}
}

func TestImagePromptModelRelayProviderSendsOneNonStreamingToolFreeRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.URL.Path != "/ai-open-platform-api/v1/chat/completions" || request.Header.Get("ApiKey") != "secret-key" {
			t.Fatalf("provider request path=%q apiKey=%q", request.URL.Path, request.Header.Get("ApiKey"))
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		var payload struct {
			Model    string                           `json:"model"`
			Messages []ImagePromptModelRelayMessageV1 `json:"messages"`
			Tools    []struct{}                       `json:"tools"`
			Stream   bool                             `json:"stream"`
		}
		if err := decodeStrictJSON(body, &payload); err != nil || payload.Model != "gpt-5.5" || len(payload.Messages) != 2 || payload.Tools == nil || len(payload.Tools) != 0 || payload.Stream {
			t.Fatalf("provider payload=%s error=%v", body, err)
		}
		response.Header().Set("content-type", "application/json")
		_, _ = response.Write([]byte(`{"id":"provider-request","model":"gpt-5.5","choices":[{"message":{"content":"prompt"}}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`))
	}))
	defer server.Close()
	svc := &Service{imagePromptModelRelayHTTPClient: server.Client()}
	result, err := svc.callImagePromptModelRelayProvider(context.Background(), providerConfig{BaseURL: server.URL + "/ai-open-platform-api/v1", APIKey: "secret-key", Model: "gpt-5.5"}, validImagePromptRelayMessages())
	if err != nil {
		t.Fatalf("provider relay call: %v", err)
	}
	if calls.Load() != 1 || result.Text != "prompt" || result.ProviderRequestID != "provider-request" || !result.UsageAvailable || result.Usage.TotalTokens != 8 {
		t.Fatalf("provider result=%#v calls=%d", result, calls.Load())
	}
}

func newImagePromptModelRelayFixture(t *testing.T) (*imagePromptModelFixture, ImagePromptModelRelayRequestV1) {
	t.Helper()
	fixture := newImagePromptModelFixture(t)
	binding, err := fixture.svc.freezeImagePromptModelBinding()
	if err != nil {
		t.Fatal(err)
	}
	command := &canvasImagePromptCommandV1{SchemaVersion: canvasImageCommandVersion, Command: imagePromptCommandPortraitTexture, Objective: "提升人像质感", Parameters: map[string]interface{}{"strength": 0.8}, SourceNodeID: "node-source", NodeRevision: 3}
	input := canvasGenerationInput{Mode: "image", ImagePromptCommand: command, PromptModel: &binding}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	order := model.BillingOrder{
		ID: "relay-order", UserID: "relay-user", TaskID: "relay-task", ChannelID: "image-channel", ChannelModelID: "image-model",
		Model: "gpt-image-2", Capability: "image", BillingMode: "fixed_request", PriceVersion: 1, UnitPriceMicrocredits: 100,
		MultiplierBasisPoints: 10_000, Quantity: 1, AmountMicrocredits: 100, ReservedAmountMicrocredits: 100,
		Status: model.BillingStatusReserved, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	task := model.Task{ID: order.TaskID, UserID: order.UserID, ProjectID: "canvas-1", Type: "canvas_image", Capability: "image", Status: model.TaskStatusRunning, BillingOrderID: order.ID, InputJSON: string(inputJSON), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	for _, row := range []interface{}{&order, &task} {
		if err := fixture.db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	fingerprint, err := billingOrderQuoteFingerprint(&order)
	if err != nil {
		t.Fatal(err)
	}
	stream := false
	request := ImagePromptModelRelayRequestV1{
		SchemaVersion: imagePromptModelRelayRequestVersion, CorrelationID: task.ID,
		CommercialContext: imagePromptSpecialistCommercialContextV1{GenerationTaskID: task.ID, BillingOrderID: order.ID, QuoteFingerprint: fingerprint},
		PromptModel:       binding, Messages: validImagePromptRelayMessages(), Tools: []struct{}{}, Stream: &stream,
	}
	return fixture, request
}

func validImagePromptRelayMessages() []ImagePromptModelRelayMessageV1 {
	return []ImagePromptModelRelayMessageV1{{Role: "system", Content: "只输出严格 JSON"}, {Role: "user", Content: "生成图片提示词"}}
}

func TestImagePromptModelRelayRejectsMissingUsageAfterProviderFact(t *testing.T) {
	fixture, request := newImagePromptModelRelayFixture(t)
	fixture.svc.imagePromptModelRelayProvider = func(context.Context, providerConfig, []ImagePromptModelRelayMessageV1) (imagePromptModelRelayProviderResult, error) {
		return imagePromptModelRelayProviderResult{Text: "prompt", EffectiveModel: request.PromptModel.ModelKey, ProviderRequestID: "provider-request", StatusCode: http.StatusOK}, nil
	}
	_, err := fixture.svc.ExecuteImagePromptModelRelay(context.Background(), request)
	var failure *ImagePromptModelRelayError
	if !errors.As(err, &failure) || failure.Code != imagePromptErrorOutputInvalid {
		t.Fatalf("missing usage error = %#v", err)
	}
	var log model.ApiCallLog
	if err := fixture.db.Where("task_id = ? AND source = ?", request.CorrelationID, "image-prompt-model-relay").Take(&log).Error; err != nil {
		t.Fatal(err)
	}
	if log.Status != model.ApiCallStatusFailed || !strings.Contains(log.Error, "失败") {
		t.Fatalf("failed provider evidence = %#v", log)
	}
}
