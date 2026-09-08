package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

type fakeImagePromptModelRelayService struct {
	authCalls    int
	executeCalls int
	authInput    service.ImagePromptModelRelayAuthInput
	authErr      error
}

func (fake *fakeImagePromptModelRelayService) VerifyImagePromptModelRelayAuth(_ context.Context, input service.ImagePromptModelRelayAuthInput) error {
	fake.authCalls++
	fake.authInput = input
	return fake.authErr
}

func (fake *fakeImagePromptModelRelayService) ExecuteImagePromptModelRelay(_ context.Context, request service.ImagePromptModelRelayRequestV1) (service.ImagePromptModelRelayResponseV1, error) {
	fake.executeCalls++
	return service.ImagePromptModelRelayResponseV1{SchemaVersion: "image-prompt-model-relay-response/v1", Text: "prompt", EffectiveModel: "gpt-5.5", ProviderRequestID: "provider-request", Usage: service.ImagePromptModelRelayUsageV1{InputTokens: 2, OutputTokens: 1, TotalTokens: 3}}, nil
}

func TestImagePromptModelRelayHandlerAuthenticatesRawBodyAndRejectsUnknownFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &fakeImagePromptModelRelayService{}
	router := gin.New()
	RegisterImagePromptModelRelayRoutes(router.Group("/internal/v1"), fake)
	body := validImagePromptModelRelayBody(`,"unexpected":true`)
	request := httptest.NewRequest(http.MethodPost, service.ImagePromptModelRelayPath, strings.NewReader(body))
	setImagePromptRelayBindingHeaders(request, "task-1", "task-1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || fake.authCalls != 1 || fake.executeCalls != 0 || string(fake.authInput.Body) != body {
		t.Fatalf("response=%d auth=%d execute=%d body=%q", response.Code, fake.authCalls, fake.executeCalls, fake.authInput.Body)
	}
}

func TestImagePromptModelRelayHandlerRejectsHeaderBodyBindingMismatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &fakeImagePromptModelRelayService{}
	router := gin.New()
	RegisterImagePromptModelRelayRoutes(router.Group("/internal/v1"), fake)
	body := validImagePromptModelRelayBody("")
	request := httptest.NewRequest(http.MethodPost, service.ImagePromptModelRelayPath, strings.NewReader(body))
	setImagePromptRelayBindingHeaders(request, "other-task", "task-1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || fake.authCalls != 1 || fake.executeCalls != 0 {
		t.Fatalf("response=%d auth=%d execute=%d", response.Code, fake.authCalls, fake.executeCalls)
	}
}

func TestImagePromptModelRelayHandlerReturnsStrictSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &fakeImagePromptModelRelayService{}
	router := gin.New()
	RegisterImagePromptModelRelayRoutes(router.Group("/internal/v1"), fake)
	body := validImagePromptModelRelayBody("")
	request := httptest.NewRequest(http.MethodPost, service.ImagePromptModelRelayPath, strings.NewReader(body))
	setImagePromptRelayBindingHeaders(request, "task-1", "task-1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || fake.authCalls != 1 || fake.executeCalls != 1 || !strings.Contains(response.Body.String(), `"providerRequestId":"provider-request"`) {
		t.Fatalf("response=%d auth=%d execute=%d body=%s", response.Code, fake.authCalls, fake.executeCalls, response.Body.String())
	}
}

func validImagePromptModelRelayBody(extra string) string {
	return `{"schemaVersion":"image-prompt-model-relay-request/v1","correlationId":"task-1","commercialContext":{"generationTaskId":"task-1","billingOrderId":"order-1","quoteFingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"promptModel":{"catalogRecordId":"record-1","modelKey":"gpt-5.5","configurationRevision":"revision-1","providerEndpointVersionId":"endpoint-1","providerCredentialVersionId":"credential-version-1"},"messages":[{"role":"system","content":"system"},{"role":"user","content":"user"}],"tools":[],"stream":false` + extra + `}`
}

func setImagePromptRelayBindingHeaders(request *http.Request, generationTaskID string, correlationID string) {
	request.Header.Set(service.ImagePromptModelRelayHeaderServiceID, "hono")
	request.Header.Set(service.ImagePromptModelRelayHeaderTimestamp, "1")
	request.Header.Set(service.ImagePromptModelRelayHeaderNonce, "nonce")
	request.Header.Set(service.ImagePromptModelRelayHeaderBodySHA256, strings.Repeat("a", 64))
	request.Header.Set(service.ImagePromptModelRelayHeaderGenerationTaskID, generationTaskID)
	request.Header.Set(service.ImagePromptModelRelayHeaderCorrelationID, correlationID)
	request.Header.Set(service.ImagePromptModelRelayHeaderSignature, "v1=signature")
}
