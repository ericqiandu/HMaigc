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

func TestRunKuaiziChatCompletionStreamRequestsStrictStreaming(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		options, _ := body["stream_options"].(map[string]any)
		if body["stream"] != true || options["include_usage"] != true {
			t.Errorf("stream request = %#v", body)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"id\":\"req-1\",\"choices\":[{\"delta\":{\"content\":\"{\\\"kind\\\":\\\"final\\\"}\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	result, err := runKuaiziChatCompletionStream(context.Background(), canvasGenerationInput{Prompt: "hello", Config: providerConfig{BaseURL: server.URL, APIKey: "key", Model: "model", MaxOutputTokens: 100, JSONOutput: true}}, func(string) error { return nil })
	if err != nil || result.ProviderRequestID != "req-1" {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestParseChatCompletionSSEEmitsContentUsageAndDone(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"你","reasoning_content":"private"}}]}`,
		"",
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"好"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":3}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	var deltas []string
	result, err := parseChatCompletionSSE(context.Background(), strings.NewReader(stream), func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(deltas, "") != "你好" {
		t.Fatalf("deltas = %#v", deltas)
	}
	if result.Text != "你好" || result.ProviderRequestID != "chatcmpl-1" || result.FinishReason != "stop" {
		t.Fatalf("result = %#v", result)
	}
	if !result.Usage.Available || result.Usage.InputTokens != 12 || result.Usage.CachedTokens != 3 || result.Usage.OutputTokens != 2 {
		t.Fatalf("usage = %#v", result.Usage)
	}
}

func TestParseChatCompletionSSEFailsOnTruncationAndCancellation(t *testing.T) {
	if _, err := parseChatCompletionSSE(context.Background(), strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n"), func(string) error { return nil }); !errors.Is(err, errAgentProviderStreamTruncated) {
		t.Fatalf("truncated error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := parseChatCompletionSSE(ctx, strings.NewReader("data: [DONE]\n\n"), func(string) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
}
