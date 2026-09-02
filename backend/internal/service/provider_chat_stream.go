package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

var (
	errAgentProviderStreamProtocolInvalid = errors.New("agent_provider_stream_protocol_invalid")
	errAgentProviderStreamTruncated       = errors.New("agent_provider_stream_truncated")
)

type chatCompletionStreamChunk struct {
	ID      string `json:"id"`
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens       int64 `json:"prompt_tokens"`
		CompletionTokens   int64 `json:"completion_tokens"`
		InputTokens        int64 `json:"input_tokens"`
		OutputTokens       int64 `json:"output_tokens"`
		PromptTokenDetails struct {
			CachedTokens int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
		InputTokenDetails struct {
			CachedTokens int64 `json:"cached_tokens"`
		} `json:"input_tokens_details"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func runKuaiziChatCompletionStream(ctx context.Context, input canvasGenerationInput, emit func(string) error) (providerTextStreamResult, error) {
	return runChatCompletionStream(ctx, input, "ApiKey", input.Config.APIKey, true, emit)
}

func runOpenAIChatCompletionStream(ctx context.Context, input canvasGenerationInput, emit func(string) error) (providerTextStreamResult, error) {
	return runChatCompletionStream(ctx, input, "Authorization", "Bearer "+input.Config.APIKey, false, emit)
}

func runChatCompletionStream(ctx context.Context, input canvasGenerationInput, headerName string, headerValue string, strictKuaizi bool, emit func(string) error) (providerTextStreamResult, error) {
	data, err := chatCompletionStreamRequestBody(input)
	if err != nil {
		return providerTextStreamResult{}, err
	}
	requestContext := ctx
	if strictKuaizi {
		requestContext = withKuaiziRequest(ctx)
	}
	req, err := http.NewRequestWithContext(requestContext, http.MethodPost, apiURL(input.Config.BaseURL, "/chat/completions"), bytes.NewReader(data))
	if err != nil {
		return providerTextStreamResult{}, err
	}
	req.Header.Set(headerName, headerValue)
	req.Header.Set("Content-Type", "application/json")
	if _, err := ValidateOutboundURL(req.URL.String()); err != nil {
		return providerTextStreamResult{}, err
	}
	client := OutboundHTTPClient(providerHTTPTimeout)
	if strictKuaizi {
		client = KuaiziHTTPClient(strings.TrimSpace(os.Getenv("CANVAS_ENVIRONMENT")), providerHTTPTimeout)
	}
	response, err := client.Do(req)
	if err != nil {
		return providerTextStreamResult{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
		return providerTextStreamResult{}, providerHTTPError{StatusCode: response.StatusCode, Status: response.Status, Body: string(data)}
	}
	if !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		return providerTextStreamResult{}, fmt.Errorf("%w: content type %q", errAgentProviderStreamProtocolInvalid, response.Header.Get("Content-Type"))
	}
	return parseChatCompletionSSE(ctx, response.Body, emit)
}

func chatCompletionStreamRequestBody(input canvasGenerationInput) ([]byte, error) {
	data, _, err := kuaiziChatCompletionsRequestBody(input)
	if err != nil {
		return nil, err
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, err
	}
	body["stream"] = json.RawMessage(`true`)
	body["stream_options"] = json.RawMessage(`{"include_usage":true}`)
	data, err = json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func parseChatCompletionSSE(ctx context.Context, reader io.Reader, emit func(string) error) (providerTextStreamResult, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 32*1024), 2*1024*1024)
	var result providerTextStreamResult
	var frame []string
	done := false
	flush := func() error {
		if len(frame) == 0 {
			return nil
		}
		payload := strings.Join(frame, "\n")
		frame = frame[:0]
		if payload == "[DONE]" {
			done = true
			return nil
		}
		var chunk chatCompletionStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return fmt.Errorf("%w: %v", errAgentProviderStreamProtocolInvalid, err)
		}
		if chunk.Error != nil && strings.TrimSpace(chunk.Error.Message) != "" {
			return fmt.Errorf("%w: %s", errAgentProviderStreamProtocolInvalid, strings.TrimSpace(chunk.Error.Message))
		}
		if strings.TrimSpace(chunk.ID) != "" {
			result.ProviderRequestID = strings.TrimSpace(chunk.ID)
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				if err := emit(choice.Delta.Content); err != nil {
					return err
				}
				result.Text += choice.Delta.Content
			}
			if choice.FinishReason != "" {
				result.FinishReason = choice.FinishReason
			}
		}
		if chunk.Usage != nil {
			result.Usage = TokenUsageFact{InputTokens: chunk.Usage.InputTokens, OutputTokens: chunk.Usage.OutputTokens, Available: true}
			if result.Usage.InputTokens == 0 {
				result.Usage.InputTokens = chunk.Usage.PromptTokens
			}
			if result.Usage.OutputTokens == 0 {
				result.Usage.OutputTokens = chunk.Usage.CompletionTokens
			}
			result.Usage.CachedTokens = chunk.Usage.InputTokenDetails.CachedTokens
			if result.Usage.CachedTokens == 0 {
				result.Usage.CachedTokens = chunk.Usage.PromptTokenDetails.CachedTokens
			}
		}
		return nil
	}
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return result, err
			}
			if done {
				if result.Text == "" {
					return result, errProviderTextMissing
				}
				return result, nil
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			return result, fmt.Errorf("%w: unsupported SSE field", errAgentProviderStreamProtocolInvalid)
		}
		frame = append(frame, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
	}
	if err := scanner.Err(); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := flush(); err != nil {
		return result, err
	}
	if !done {
		return result, errAgentProviderStreamTruncated
	}
	if result.Text == "" {
		return result, errProviderTextMissing
	}
	return result, nil
}
