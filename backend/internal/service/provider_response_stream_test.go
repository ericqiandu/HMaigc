package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunResponsesTextStreamRequestsJSONOutputForAgentDecision(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			t.Errorf("request path = %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer direct-key" {
			t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
		}
		var body struct {
			Text *struct {
				Format struct {
					Type string `json:"type"`
				} `json:"format"`
			} `json:"text"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Text == nil || body.Text.Format.Type != "json_object" {
			t.Errorf("Agent Responses JSON output contract = %#v", body.Text)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"{\\\"kind\\\":\\\"final\\\",\\\"final\\\":{\\\"message\\\":\\\"ok\\\",\\\"expectedDelivery\\\":{\\\"kind\\\":\\\"answer\\\",\\\"completionCriteria\\\":[{\\\"fact\\\":\\\"final_message\\\"}]}}}\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-json\",\"status\":\"completed\",\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}}\n\n"))
	}))
	defer server.Close()

	result, err := runResponsesTextStream(context.Background(), canvasGenerationInput{
		Prompt: "return one Agent decision",
		Config: providerConfig{BaseURL: server.URL, APIKey: "direct-key", Model: "model", JSONOutput: true},
	}, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if result.ProviderRequestID != "resp-json" || result.Text == "" {
		t.Fatalf("result = %#v", result)
	}
}

func TestParseResponsesSSEEmitsTextUsageAndCompletion(t *testing.T) {
	stream := strings.Join([]string{
		`event: response.created`,
		`data: {"type":"response.created","response":{"id":"resp-1","status":"in_progress"}}`,
		``,
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":"你"}`,
		``,
		`event: response.reasoning_text.delta`,
		`data: {"type":"response.reasoning_text.delta","delta":"private reasoning"}`,
		``,
		`event: response.reasoning_text.done`,
		`data: {"type":"response.reasoning_text.done","text":"private reasoning"}`,
		``,
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":"好"}`,
		``,
		`event: response.completed`,
		`data: {"type":"response.completed","response":{"id":"resp-1","status":"completed","usage":{"input_tokens":12,"output_tokens":2,"input_tokens_details":{"cached_tokens":3}}}}`,
		``,
	}, "\n")
	var deltas []string
	result, err := parseResponsesSSE(context.Background(), strings.NewReader(stream), func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(deltas, "") != "你好" || result.Text != "你好" || result.ProviderRequestID != "resp-1" || result.FinishReason != "completed" {
		t.Fatalf("result = %#v deltas = %#v", result, deltas)
	}
	if !result.Usage.Available || result.Usage.InputTokens != 12 || result.Usage.CachedTokens != 3 || result.Usage.OutputTokens != 2 {
		t.Fatalf("usage = %#v", result.Usage)
	}
}

func TestParseResponsesSSERejectsFailedAndTruncatedStreams(t *testing.T) {
	failed := "event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp-failed\",\"status\":\"failed\",\"error\":{\"message\":\"provider rejected\"}}}\n\n"
	if _, err := parseResponsesSSE(context.Background(), strings.NewReader(failed), func(string) error { return nil }); err == nil || !strings.Contains(err.Error(), "provider rejected") {
		t.Fatalf("failed stream error = %v", err)
	}
	truncated := "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"x\"}\n\n"
	if _, err := parseResponsesSSE(context.Background(), strings.NewReader(truncated), func(string) error { return nil }); !errors.Is(err, errAgentProviderStreamTruncated) {
		t.Fatalf("truncated stream error = %v", err)
	}
}
