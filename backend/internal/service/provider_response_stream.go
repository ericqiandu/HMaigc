package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"infinite-canvas/backend/internal/model"
)

type responsesStreamEvent struct {
	Type     string `json:"type"`
	Delta    string `json:"delta"`
	Response struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Usage  struct {
			InputTokens       int64 `json:"input_tokens"`
			OutputTokens      int64 `json:"output_tokens"`
			InputTokenDetails struct {
				CachedTokens int64 `json:"cached_tokens"`
			} `json:"input_tokens_details"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"response"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type responsesStreamRequest struct {
	Model           string          `json:"model"`
	Input           json.RawMessage `json:"input"`
	Stream          bool            `json:"stream"`
	MaxOutputTokens int64           `json:"max_output_tokens,omitempty"`
	Text            *responsesText  `json:"text,omitempty"`
}

type responsesText struct {
	Format responsesTextFormat `json:"format"`
}

type responsesTextFormat struct {
	Type string `json:"type"`
}

func runAgentTextCompletionStream(ctx context.Context, input canvasGenerationInput, emit func(string) error) (providerTextStreamResult, error) {
	switch input.Config.InterfaceType {
	case string(model.ChannelInterfaceOpenAIResponse):
		return runResponsesTextStream(ctx, input, emit)
	case string(model.ChannelInterfaceChatCompletion):
		if input.Config.ManagedProviderRuntime {
			return runKuaiziChatCompletionStream(ctx, input, emit)
		}
		return runOpenAIChatCompletionStream(ctx, input, emit)
	default:
		return providerTextStreamResult{}, fmt.Errorf("Agent 模型接口不支持流式决策：%s", input.Config.InterfaceType)
	}
}

func runResponsesTextStream(ctx context.Context, input canvasGenerationInput, emit func(string) error) (providerTextStreamResult, error) {
	responseInput, err := textResponseInput(input)
	if err != nil {
		return providerTextStreamResult{}, err
	}
	body := responsesStreamRequest{
		Model:           input.Config.Model,
		Input:           responseInput,
		Stream:          true,
		MaxOutputTokens: input.Config.MaxOutputTokens,
	}
	if input.Config.JSONOutput {
		body.Text = &responsesText{Format: responsesTextFormat{Type: "json_object"}}
	}
	data, err := json.Marshal(body)
	if err != nil {
		return providerTextStreamResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL(input.Config.BaseURL, "/responses"), bytes.NewReader(data))
	if err != nil {
		return providerTextStreamResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+input.Config.APIKey)
	req.Header.Set("Content-Type", "application/json")
	if _, err := ValidateOutboundURL(req.URL.String()); err != nil {
		return providerTextStreamResult{}, err
	}
	response, err := OutboundHTTPClient(providerHTTPTimeout).Do(req)
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
	return parseResponsesSSE(ctx, response.Body, emit)
}

func parseResponsesSSE(ctx context.Context, reader io.Reader, emit func(string) error) (providerTextStreamResult, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 32*1024), 2*1024*1024)
	var result providerTextStreamResult
	eventName := ""
	dataLines := make([]string, 0, 1)
	completed := false
	flush := func() error {
		if len(dataLines) == 0 {
			eventName = ""
			return nil
		}
		payload := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		if payload == "[DONE]" {
			if !completed {
				return errAgentProviderStreamTruncated
			}
			return nil
		}
		var event responsesStreamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return fmt.Errorf("%w: %v", errAgentProviderStreamProtocolInvalid, err)
		}
		eventType := strings.TrimSpace(event.Type)
		if eventType == "" {
			eventType = strings.TrimSpace(eventName)
		}
		eventName = ""
		if responseID := strings.TrimSpace(event.Response.ID); responseID != "" {
			result.ProviderRequestID = responseID
		}
		switch eventType {
		case "response.created", "response.in_progress", "response.output_item.added", "response.content_part.added",
			"response.reasoning_text.delta", "response.reasoning_text.done",
			"response.output_text.done", "response.content_part.done", "response.output_item.done":
			return nil
		case "response.output_text.delta":
			if event.Delta == "" {
				return nil
			}
			if err := emit(event.Delta); err != nil {
				return err
			}
			result.Text += event.Delta
			return nil
		case "response.completed":
			if event.Response.Status != "" && event.Response.Status != "completed" {
				return fmt.Errorf("%w: response status %s", errAgentProviderStreamProtocolInvalid, event.Response.Status)
			}
			result.FinishReason = "completed"
			result.Usage = TokenUsageFact{
				InputTokens: event.Response.Usage.InputTokens, CachedTokens: event.Response.Usage.InputTokenDetails.CachedTokens,
				OutputTokens: event.Response.Usage.OutputTokens, Available: true,
			}
			completed = true
			return nil
		case "response.failed", "response.incomplete", "error":
			message := "Responses 流返回失败"
			if event.Response.Error != nil && strings.TrimSpace(event.Response.Error.Message) != "" {
				message = strings.TrimSpace(event.Response.Error.Message)
			} else if event.Error != nil && strings.TrimSpace(event.Error.Message) != "" {
				message = strings.TrimSpace(event.Error.Message)
			}
			return fmt.Errorf("%w: %s", errAgentProviderStreamProtocolInvalid, message)
		default:
			return fmt.Errorf("%w: unsupported Responses event %q", errAgentProviderStreamProtocolInvalid, eventType)
		}
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
			if completed {
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
		switch {
		case strings.HasPrefix(line, "event:"):
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		default:
			return result, fmt.Errorf("%w: unsupported SSE field", errAgentProviderStreamProtocolInvalid)
		}
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
	if !completed {
		return result, errAgentProviderStreamTruncated
	}
	if result.Text == "" {
		return result, errProviderTextMissing
	}
	return result, nil
}
