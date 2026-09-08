package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestImagePromptSpecialistHTTPClientSignsAndValidatesResponse(t *testing.T) {
	now := time.Date(2026, time.September, 7, 12, 0, 0, 0, time.UTC)
	request := imagePromptSpecialistRequestFixture()
	response := imagePromptSpecialistResponseFixture(request, now)
	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, incoming *http.Request) {
		body, err := io.ReadAll(incoming.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		receivedBody = body
		if incoming.Header.Get(imagePromptHeaderServiceID) != "hmaigc-backend" {
			t.Errorf("service id = %q", incoming.Header.Get(imagePromptHeaderServiceID))
		}
		if incoming.Header.Get(imagePromptHeaderIdempotencyKey) != request.IdempotencyKey {
			t.Errorf("idempotency key = %q", incoming.Header.Get(imagePromptHeaderIdempotencyKey))
		}
		bodyDigest := sha256.Sum256(body)
		if incoming.Header.Get(imagePromptHeaderBodySHA256) != hex.EncodeToString(bodyDigest[:]) {
			t.Error("body digest mismatch")
		}
		canonical := strings.Join([]string{
			imagePromptSignatureVersion,
			"hmaigc-backend",
			incoming.Header.Get(imagePromptHeaderTimestamp),
			incoming.Header.Get(imagePromptHeaderNonce),
			"POST",
			imagePromptSpecialistPath,
			hex.EncodeToString(bodyDigest[:]),
			request.IdempotencyKey,
		}, "\n")
		mac := hmac.New(sha256.New, []byte("0123456789abcdef0123456789abcdef"))
		_, _ = mac.Write([]byte(canonical))
		if incoming.Header.Get(imagePromptHeaderSignature) != "v1="+hex.EncodeToString(mac.Sum(nil)) {
			t.Error("signature mismatch")
		}
		encoded, _ := json.Marshal(response)
		canonicalResponse, _ := canonicalJSON(encoded)
		digest := sha256.Sum256(canonicalResponse)
		writer.Header().Set("content-type", "application/json")
		writer.Header().Set(imagePromptHeaderResponseDigest, hex.EncodeToString(digest[:]))
		_, _ = writer.Write(encoded)
	}))
	defer server.Close()

	client, err := newImagePromptSpecialistHTTPClient(imagePromptSpecialistClientConfig{
		Endpoint:  server.URL + imagePromptSpecialistPath,
		ServiceID: "hmaigc-backend",
		Secret:    "0123456789abcdef0123456789abcdef",
		Timeout:   time.Second,
		Now:       func() time.Time { return now },
		Nonce:     func() (string, error) { return "nonce-12345678", nil },
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	execution, err := client.Execute(context.Background(), request)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(receivedBody) == 0 || execution.Response.ImagePrompt != response.ImagePrompt {
		t.Fatalf("execution = %#v", execution)
	}
	if execution.ResponseDigest == "" {
		t.Fatal("response digest is empty")
	}
}

func TestImagePromptSpecialistHTTPClientRejectsInvalidBindingAndDigest(t *testing.T) {
	request := imagePromptSpecialistRequestFixture()
	now := time.Date(2026, time.September, 7, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		mutate   func(*imagePromptSpecialistResponseV1)
		header   string
		wantCode imagePromptSpecialistErrorCode
	}{
		{name: "model binding", mutate: func(response *imagePromptSpecialistResponseV1) { response.Trace.Model.EffectiveModel = "other-model" }, wantCode: imagePromptErrorModelInheritanceFailed},
		{name: "endpoint version binding", mutate: func(response *imagePromptSpecialistResponseV1) {
			response.Trace.Model.ProviderEndpointVersionID = "other-endpoint"
		}, wantCode: imagePromptErrorModelInheritanceFailed},
		{name: "credential version binding", mutate: func(response *imagePromptSpecialistResponseV1) {
			response.Trace.Model.ProviderCredentialVersionID = "other-credential"
		}, wantCode: imagePromptErrorModelInheritanceFailed},
		{name: "evidence digest", mutate: func(response *imagePromptSpecialistResponseV1) {
			response.Trace.EvidenceDigest = strings.Repeat("a", 64)
		}, wantCode: imagePromptErrorEvidenceInvalid},
		{name: "response digest", mutate: func(*imagePromptSpecialistResponseV1) {}, header: strings.Repeat("b", 64), wantCode: imagePromptErrorOutputInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := imagePromptSpecialistResponseFixture(request, now)
			test.mutate(&response)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				encoded, _ := json.Marshal(response)
				digest := test.header
				if digest == "" {
					canonicalResponse, _ := canonicalJSON(encoded)
					sum := sha256.Sum256(canonicalResponse)
					digest = hex.EncodeToString(sum[:])
				}
				writer.Header().Set(imagePromptHeaderResponseDigest, digest)
				_, _ = writer.Write(encoded)
			}))
			defer server.Close()
			client, err := newImagePromptSpecialistHTTPClient(imagePromptSpecialistClientConfig{Endpoint: server.URL + imagePromptSpecialistPath, ServiceID: "hmaigc-backend", Secret: "0123456789abcdef0123456789abcdef", Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Execute(context.Background(), request)
			var failure *imagePromptSpecialistFailure
			if !errors.As(err, &failure) || failure.Code != test.wantCode {
				t.Fatalf("Execute() error = %#v, want code %s", err, test.wantCode)
			}
		})
	}
}

func TestImagePromptSpecialistHTTPClientMapsStableErrorsAndCancellation(t *testing.T) {
	request := imagePromptSpecialistRequestFixture()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, incoming *http.Request) {
		if incoming.URL.Query().Get("wait") == "1" {
			<-incoming.Context().Done()
			return
		}
		writer.WriteHeader(http.StatusConflict)
		_, _ = writer.Write([]byte(`{"code":"specialist_idempotency_conflict","reason":"request is pending"}`))
	}))
	defer server.Close()

	client, err := newImagePromptSpecialistHTTPClient(imagePromptSpecialistClientConfig{Endpoint: server.URL + imagePromptSpecialistPath, ServiceID: "hmaigc-backend", Secret: "0123456789abcdef0123456789abcdef", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Execute(context.Background(), request)
	var failure *imagePromptSpecialistFailure
	if !errors.As(err, &failure) || failure.Code != imagePromptErrorIdempotencyConflict {
		t.Fatalf("stable error = %#v", err)
	}

	cancelled, err := newImagePromptSpecialistHTTPClient(imagePromptSpecialistClientConfig{Endpoint: server.URL + imagePromptSpecialistPath + "?wait=1", ServiceID: "hmaigc-backend", Secret: "0123456789abcdef0123456789abcdef", Timeout: time.Second})
	if err == nil || cancelled != nil {
		t.Fatalf("query endpoint must be rejected, client=%v err=%v", cancelled, err)
	}

	waitServer := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, incoming *http.Request) { <-incoming.Context().Done() }))
	defer waitServer.Close()
	waitClient, err := newImagePromptSpecialistHTTPClient(imagePromptSpecialistClientConfig{Endpoint: waitServer.URL + imagePromptSpecialistPath, ServiceID: "hmaigc-backend", Secret: "0123456789abcdef0123456789abcdef", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = waitClient.Execute(ctx, request)
	if !errors.As(err, &failure) || failure.Code != imagePromptErrorUnavailable {
		t.Fatalf("cancel error = %#v", err)
	}
}

func TestImagePromptSpecialistClientConfigurationIsExplicit(t *testing.T) {
	t.Setenv("CANVAS_IMAGE_PROMPT_SPECIALIST_ENABLED", "true")
	t.Setenv("CANVAS_IMAGE_PROMPT_SPECIALIST_URL", "")
	_, err := imagePromptSpecialistClientFromEnvironment()
	if err == nil || !strings.Contains(err.Error(), "CANVAS_IMAGE_PROMPT_SPECIALIST_URL") {
		t.Fatalf("config error = %v", err)
	}
	t.Setenv("CANVAS_IMAGE_PROMPT_SPECIALIST_ENABLED", "false")
	client, err := imagePromptSpecialistClientFromEnvironment()
	if err != nil || client != nil {
		t.Fatalf("disabled config = %v, %v", client, err)
	}
}

func TestImagePromptSpecialistClientRejectsPlainHTTPInProduction(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "production")
	t.Setenv("CANVAS_IMAGE_PROMPT_SPECIALIST_ENABLED", "true")
	t.Setenv("CANVAS_IMAGE_PROMPT_SPECIALIST_URL", "http://agents.internal"+imagePromptSpecialistPath)
	t.Setenv("CANVAS_IMAGE_PROMPT_HMAC_SERVICE_ID", "hmaigc-backend")
	t.Setenv("CANVAS_IMAGE_PROMPT_HMAC_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("CANVAS_IMAGE_PROMPT_TIMEOUT_MS", "30000")

	client, err := imagePromptSpecialistClientFromEnvironment()
	if err == nil || client != nil {
		t.Fatalf("production plaintext config must fail, client=%v err=%v", client, err)
	}
}

func imagePromptSpecialistRequestFixture() imagePromptSpecialistRequestV1 {
	evidence := imagePromptSpecialistEvidenceV1{References: []imagePromptSpecialistReferenceV1{{ResourceID: "resource-1", MediaType: "image", SemanticRole: "source", Ordinal: 1, ContentDigest: strings.Repeat("1", 64), AccessURL: "https://cdn.example.com/resource-1.png"}}}
	return imagePromptSpecialistRequestV1{
		SchemaVersion:     imagePromptRequestVersion,
		RequestID:         "image-prompt-task-1",
		CorrelationID:     "task-1",
		IdempotencyKey:    "image-prompt:task-1",
		Actor:             imagePromptSpecialistActorV1{TenantID: "user-1", UserID: "user-1"},
		Scope:             imagePromptSpecialistScopeV1{ProjectID: "canvas-1", CanvasID: "canvas-1", NodeID: "node-1", NodeRevision: 3},
		CommercialContext: imagePromptSpecialistCommercialContextV1{GenerationTaskID: "task-1", BillingOrderID: "bill-1", QuoteFingerprint: strings.Repeat("2", 64)},
		Operation:         imagePromptSpecialistOperationV1{Command: imagePromptCommandPortraitTexture, Objective: "优化人像质感", Parameters: map[string]interface{}{"preserveIdentity": true}},
		Evidence:          evidence,
		PromptModel: imagePromptSpecialistModelBindingV1{
			CatalogRecordID: "model-record-1", ModelKey: "deepseek-chat", ConfigurationRevision: "2026-09-07T12:00:00Z",
			ProviderEndpointVersionID: "endpoint-version-1", ProviderCredentialVersionID: "credential-version-1",
		},
	}
}

func TestValidateImagePromptSpecialistRequestRequiresFrozenProviderVersions(t *testing.T) {
	request := imagePromptSpecialistRequestFixture()
	request.PromptModel.ProviderEndpointVersionID = ""
	if err := validateImagePromptSpecialistRequest(request); err == nil {
		t.Fatal("missing provider endpoint version must fail")
	}
	request = imagePromptSpecialistRequestFixture()
	request.PromptModel.ProviderCredentialVersionID = ""
	if err := validateImagePromptSpecialistRequest(request); err == nil {
		t.Fatal("missing provider credential version must fail")
	}
}

func imagePromptSpecialistResponseFixture(request imagePromptSpecialistRequestV1, now time.Time) imagePromptSpecialistResponseV1 {
	digest, _ := imagePromptEvidenceDigest(request.Evidence)
	return imagePromptSpecialistResponseV1{
		SchemaVersion: imagePromptResponseVersion,
		ImagePrompt:   "保留同一人物身份、构图与服饰，提升真实皮肤纹理与面部细节。",
		Trace: imagePromptSpecialistTraceV1{
			RequestID: request.RequestID, CorrelationID: request.CorrelationID, Specialist: imagePromptSpecialistName, EvidenceDigest: digest,
			Model: imagePromptSpecialistTraceModelV1{
				CatalogRecordID: request.PromptModel.CatalogRecordID, ModelKey: request.PromptModel.ModelKey, ConfigurationRevision: request.PromptModel.ConfigurationRevision,
				ProviderEndpointVersionID: request.PromptModel.ProviderEndpointVersionID, ProviderCredentialVersionID: request.PromptModel.ProviderCredentialVersionID,
				EffectiveModel: request.PromptModel.ModelKey,
			},
			Usage:     imagePromptSpecialistUsageV1{InputTokens: 100, OutputTokens: 30, TotalTokens: 130},
			StartedAt: now.Format(time.RFC3339), CompletedAt: now.Add(time.Second).Format(time.RFC3339),
		},
	}
}
