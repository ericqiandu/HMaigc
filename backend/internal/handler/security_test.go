package handler

import (
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/service"
)

func TestPrepareTokenBilledProxyRequestEnforcesOutputLimitAndCountsCompleteBody(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"你好"}],"max_tokens":9999,"stream":true,"stream_options":{"include_usage":false}}`)
	prepared, estimatedInput, err := prepareTokenBilledProxyRequest("/chat/completions", body, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if estimatedInput != int64(len(prepared))+tokenBillingProtocolMarginBytes {
		t.Fatalf("estimated input = %d", estimatedInput)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(prepared, &payload); err != nil {
		t.Fatal(err)
	}
	if string(payload["max_tokens"]) != "2048" {
		t.Fatalf("max_tokens = %s", payload["max_tokens"])
	}
	if string(payload["messages"]) != `[{"role":"user","content":"你好"}]` {
		t.Fatalf("messages changed = %s", payload["messages"])
	}
	if string(payload["stream_options"]) != `{"include_usage":true}` {
		t.Fatalf("stream_options = %s", payload["stream_options"])
	}
}

func TestPrepareTokenBilledProxyRequestRejectsUnsupportedOrInvalidPayload(t *testing.T) {
	for _, test := range []struct {
		path string
		body []byte
		max  int64
	}{
		{path: "/responses", body: []byte(`{"model":"deepseek"}`), max: 10},
		{path: "/chat/completions", body: []byte(`not-json`), max: 10},
		{path: "/chat/completions", body: []byte(`[]`), max: 10},
		{path: "/chat/completions", body: []byte(`{"model":"deepseek"}`), max: 0},
	} {
		if _, _, err := prepareTokenBilledProxyRequest(test.path, test.body, test.max); err == nil {
			t.Fatalf("prepareTokenBilledProxyRequest(%q, %s, %d) succeeded", test.path, test.body, test.max)
		}
	}
}

func TestSafeProxyProviderRequestIDRejectsSecretAndUnstructuredValues(t *testing.T) {
	if got := safeProxyProviderRequestID("kz-cgt-task_1", "sentinel-key"); got != "kz-cgt-task_1" {
		t.Fatalf("safe ID = %q", got)
	}
	for _, value := range []string{"sentinel-key", "prompt with spaces", strings.Repeat("x", 161)} {
		if got := safeProxyProviderRequestID(value, "sentinel-key"); got != "" {
			t.Fatalf("unsafe ID %q accepted as %q", value, got)
		}
	}
}

func TestApplySystemProxyAuthenticationUsesManagedKuaiziApiKeyWithoutBearer(t *testing.T) {
	request, err := http.NewRequest(http.MethodPost, "https://aiopenapi.kuaizi.cn/ai-open-platform-api/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	applySystemProxyAuthentication(request, service.SystemProxyRuntime{HeaderName: "ApiKey", APIKey: "sentinel-key"})
	if request.Header.Get("ApiKey") != "sentinel-key" || request.Header.Get("Authorization") != "" {
		t.Fatalf("proxy authentication headers = %#v", request.Header)
	}
}

func TestAuthorizeSystemProxyAllowsConfiguredGenerationModel(t *testing.T) {
	channel := &model.ModelChannel{APIFormat: "openai", ModelsJSON: `["gpt-image-1"]`}
	body := []byte(`{"model":"gpt-image-1","prompt":"test"}`)
	if err := authorizeSystemProxy(channel, http.MethodPost, "/images/generations", "application/json", body); err != nil {
		t.Fatalf("authorizeSystemProxy() error = %v", err)
	}
}

func TestAuthorizeCustomRelayAllowsModelsAndAgentEndpoints(t *testing.T) {
	tests := []struct {
		method      string
		target      string
		apiFormat   string
		contentType string
	}{
		{method: http.MethodGet, target: "https://api.example.com/v1/models", apiFormat: "openai"},
		{method: http.MethodPost, target: "https://api.example.com/v1/responses", apiFormat: "openai", contentType: "application/json"},
		{method: http.MethodPost, target: "https://api.example.com/v1/chat/completions", apiFormat: "openai", contentType: "application/json; charset=utf-8"},
		{method: http.MethodPost, target: "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-pro:streamGenerateContent?alt=sse", apiFormat: "gemini", contentType: "application/json"},
	}
	for _, test := range tests {
		target, err := url.Parse(test.target)
		if err != nil {
			t.Fatal(err)
		}
		if err := authorizeCustomRelay(test.method, target, test.apiFormat, test.contentType); err != nil {
			t.Fatalf("authorizeCustomRelay(%s %s) error = %v", test.method, test.target, err)
		}
	}
}

func TestAuthorizeCustomRelayRejectsArbitraryRequestsAndCredentialQueries(t *testing.T) {
	tests := []struct {
		method      string
		target      string
		apiFormat   string
		contentType string
	}{
		{method: http.MethodDelete, target: "https://api.example.com/v1/models", apiFormat: "openai"},
		{method: http.MethodGet, target: "https://api.example.com/account", apiFormat: "openai"},
		{method: http.MethodGet, target: "https://api.example.com/v1/models?api_key=secret", apiFormat: "openai"},
		{method: http.MethodPost, target: "https://api.example.com/v1/responses", apiFormat: "openai", contentType: "text/plain"},
		{method: http.MethodPost, target: "https://api.example.com/v1/../account/chat/completions", apiFormat: "openai", contentType: "application/json"},
		{method: http.MethodPost, target: "https://api.example.com/v1/models/gemini:streamGenerateContent?alt=sse&token=secret", apiFormat: "gemini", contentType: "application/json"},
	}
	for _, test := range tests {
		target, err := url.Parse(test.target)
		if err != nil {
			t.Fatal(err)
		}
		if err := authorizeCustomRelay(test.method, target, test.apiFormat, test.contentType); err == nil {
			t.Fatalf("authorizeCustomRelay(%s %s) should fail", test.method, test.target)
		}
	}
}

func TestAuthorizeSystemProxyRejectsArbitraryPathAndModel(t *testing.T) {
	channel := &model.ModelChannel{APIFormat: "openai", ModelsJSON: `["gpt-image-1"]`}
	if err := authorizeSystemProxy(channel, http.MethodDelete, "/account", "application/json", nil); err == nil {
		t.Fatal("expected arbitrary path to be rejected")
	}
	if err := authorizeSystemProxy(channel, http.MethodPost, "/images/generations", "application/json", []byte(`{"model":"unapproved"}`)); err == nil {
		t.Fatal("expected unapproved model to be rejected")
	}
}

func TestProxyRequestModelReadsMultipartField(t *testing.T) {
	var body strings.Builder
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("model", "gpt-image-1")
	_ = writer.Close()
	if got := proxyRequestModel(writer.FormDataContentType(), []byte(body.String())); got != "gpt-image-1" {
		t.Fatalf("proxyRequestModel() = %q", got)
	}
}

func TestAuthorizeSystemProxyRestrictsConfiguredInterfaceType(t *testing.T) {
	body := []byte(`{"model":"gpt-4.1"}`)
	channel := &model.ModelChannel{APIFormat: "openai", InterfaceType: model.ChannelInterfaceChatCompletion, ModelsJSON: `["gpt-4.1"]`}
	if err := authorizeSystemProxy(channel, http.MethodPost, "/chat/completions", "application/json", body); err != nil {
		t.Fatalf("authorizeSystemProxy() error = %v", err)
	}
	if err := authorizeSystemProxy(channel, http.MethodPost, "/responses", "application/json", body); err == nil {
		t.Fatal("authorizeSystemProxy() error = nil for mismatched interface")
	}
}

func TestAuthorizeSystemProxyBlocksBackendOnlyVideoInterfaces(t *testing.T) {
	body := []byte(`{"model":"grok-image-video"}`)
	for _, interfaceType := range []model.ChannelInterfaceType{model.ChannelInterfaceNewAPIVideo, model.ChannelInterfaceXAIVideo, model.ChannelInterfaceAIOpenVideoVolcengine, model.ChannelInterfaceMiniMaxSpeech} {
		channel := &model.ModelChannel{APIFormat: "openai", InterfaceType: interfaceType, ModelsJSON: `["grok-image-video"]`}
		if err := authorizeSystemProxy(channel, http.MethodPost, "/video/generations", "application/json", body); err == nil {
			t.Fatalf("authorizeSystemProxy() error = nil for backend-only interface %q", interfaceType)
		}
	}
}

func TestProxyBillingCapabilityUsesRequestSemantics(t *testing.T) {
	tests := []struct {
		name          string
		interfaceType model.ChannelInterfaceType
		path          string
		want          string
	}{
		{name: "chat", path: "/chat/completions", want: "text"},
		{name: "image", path: "/images/edits", want: "image"},
		{name: "audio", path: "/audio/speech", want: "audio"},
		{name: "video backend", interfaceType: model.ChannelInterfaceNewAPIVideo, path: "/video/generations", want: "video"},
		{name: "gemini catalog derived", path: "/models/gemini:generateContent", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := proxyBillingCapability(test.interfaceType, test.path); got != test.want {
				t.Fatalf("proxyBillingCapability() = %q, want %q", got, test.want)
			}
		})
	}
}
