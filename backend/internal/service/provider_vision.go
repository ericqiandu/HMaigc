package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

const maxVisionRequestBytes = 48 << 20

type providerVisionResult struct {
	Analysis          string
	ProviderRequestID string
	FinishReason      string
	Usage             TokenUsageFact
	RequestSent       bool
}

type providerVisionHTTPError struct {
	StatusCode int
	Status     string
}

func (err providerVisionHTTPError) Error() string {
	return fmt.Sprintf("图片理解接口请求失败：%s", err.Status)
}

func runVisionAnalysis(ctx context.Context, input canvasGenerationInput, beforeSend func() error) (providerVisionResult, error) {
	if strings.TrimSpace(input.Prompt) == "" || len(input.ReferenceImages) < 1 || len(input.ReferenceImages) > agentVisionResourceLimit || !input.ImageDetail.Valid() {
		return providerVisionResult{}, errors.New("图片理解请求不符合输入契约")
	}
	if strings.TrimSpace(input.Config.BaseURL) == "" || strings.TrimSpace(input.Config.APIKey) == "" || strings.TrimSpace(input.Config.Model) == "" {
		return providerVisionResult{}, errors.New("图片理解请求缺少冻结供应商配置")
	}
	if beforeSend == nil {
		return providerVisionResult{}, errors.New("图片理解请求缺少发送边界回调")
	}

	var (
		data        []byte
		path        string
		headerName  string
		headerValue string
		parse       func(context.Context, io.Reader, func(string) error) (providerTextStreamResult, error)
		err         error
	)
	switch input.Config.InterfaceType {
	case string(model.ChannelInterfaceChatCompletion):
		data, err = chatCompletionStreamRequestBody(input)
		path = "/chat/completions"
		if input.Config.ManagedProviderRuntime {
			headerName = "ApiKey"
			headerValue = input.Config.APIKey
		} else {
			headerName = "Authorization"
			headerValue = "Bearer " + input.Config.APIKey
		}
		parse = parseChatCompletionSSE
	case string(model.ChannelInterfaceOpenAIResponse):
		if input.Config.ManagedProviderRuntime {
			return providerVisionResult{}, errors.New("托管图片理解模型不支持 Responses 接口")
		}
		data, err = visionResponsesRequestBody(input)
		path = "/responses"
		headerName = "Authorization"
		headerValue = "Bearer " + input.Config.APIKey
		parse = parseResponsesSSE
	default:
		return providerVisionResult{}, errors.New("图片理解模型接口不受支持")
	}
	if err != nil {
		return providerVisionResult{}, err
	}
	if len(data) > maxVisionRequestBytes {
		return providerVisionResult{}, errors.New("图片理解请求体超过 48 MiB")
	}
	endpoint := apiURL(input.Config.BaseURL, path)
	if _, err := ValidateOutboundURL(endpoint); err != nil {
		return providerVisionResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return providerVisionResult{}, err
	}
	req.Header.Set(headerName, headerValue)
	req.Header.Set("Content-Type", "application/json")
	if err := beforeSend(); err != nil {
		return providerVisionResult{}, err
	}

	client := OutboundHTTPClient(providerHTTPTimeout)
	if input.Config.ManagedProviderRuntime {
		client = KuaiziHTTPClient(strings.TrimSpace(os.Getenv("CANVAS_ENVIRONMENT")), providerHTTPTimeout)
		req = req.WithContext(withKuaiziRequest(req.Context()))
	}
	result := providerVisionResult{RequestSent: true}
	response, err := client.Do(req)
	if err != nil {
		return result, errors.New("图片理解接口连接失败")
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))
		return result, providerVisionHTTPError{StatusCode: response.StatusCode, Status: response.Status}
	}
	if !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		return result, errors.New("图片理解接口返回了无效流协议")
	}
	streamResult, err := parse(ctx, response.Body, func(string) error { return nil })
	result.Analysis = streamResult.Text
	result.ProviderRequestID = streamResult.ProviderRequestID
	result.FinishReason = streamResult.FinishReason
	result.Usage = streamResult.Usage
	if err != nil {
		return result, errors.New("图片理解响应流不完整或无效")
	}
	if err := validateProviderVisionResult(result); err != nil {
		return result, err
	}
	return result, nil
}

func visionResponsesRequestBody(input canvasGenerationInput) ([]byte, error) {
	responseInput, err := textResponseInput(input)
	if err != nil {
		return nil, err
	}
	body := responsesStreamRequest{
		Model:           input.Config.Model,
		Input:           responseInput,
		Stream:          true,
		MaxOutputTokens: input.Config.MaxOutputTokens,
	}
	return json.Marshal(body)
}

func validateProviderVisionResult(result providerVisionResult) error {
	if strings.TrimSpace(result.Analysis) == "" {
		return errors.New("图片理解接口没有返回分析正文")
	}
	if strings.TrimSpace(result.ProviderRequestID) == "" {
		return errors.New("图片理解接口没有返回请求标识")
	}
	if strings.TrimSpace(result.FinishReason) == "" {
		return errors.New("图片理解接口没有返回结束状态")
	}
	if !result.Usage.Available || result.Usage.InputTokens <= 0 || result.Usage.OutputTokens <= 0 || result.Usage.CachedTokens < 0 || result.Usage.CachedTokens > result.Usage.InputTokens {
		return errors.New("图片理解接口没有返回完整有效的 Token 用量")
	}
	return nil
}

func (s *Service) agentVisionProviderMedia(scope agentruntime.Scope, resources []agentVisionResource) ([]providerMedia, error) {
	if err := scope.Validate(); err != nil || len(resources) < 1 || len(resources) > agentVisionResourceLimit {
		return nil, errors.New("图片理解资源输入无效")
	}
	media := make([]providerMedia, 0, len(resources))
	for _, source := range resources {
		resource := source.Resource
		if resource.ID == "" || resource.ID != source.Fact.ResourceID || !agentVisionResourceMatchesScope(resource, scope) || resource.Status != model.ResourceStatusReady || resource.Kind != "image" || resource.Size < 1 || resource.Size > agentVisionResourceMaxBytes || resource.Size != source.Fact.SizeBytes {
			return nil, errors.New("图片理解资源事实已变化")
		}
		item := providerMedia{ID: resource.ID, Name: source.Fact.Name, Type: source.Fact.MimeType, MimeType: source.Fact.MimeType}
		if resource.Provider == "local" {
			stream, err := s.openAgentVisionResource(scope, resource.ID)
			if err != nil {
				return nil, err
			}
			data, readErr := io.ReadAll(io.LimitReader(stream.Body, agentVisionResourceMaxBytes+1))
			closeErr := stream.Body.Close()
			if readErr != nil {
				return nil, readErr
			}
			if closeErr != nil {
				return nil, closeErr
			}
			if int64(len(data)) != resource.Size || int64(len(data)) > agentVisionResourceMaxBytes {
				return nil, errors.New("图片理解本地资源大小已变化")
			}
			item.DataURL = dataURL(source.Fact.MimeType, data)
		} else {
			setting, err := s.ossSettingForResource(resource.UserID, &resource)
			if err != nil {
				return nil, err
			}
			if setting.Provider != "aliyun" {
				return nil, errors.New("图片理解对象存储协议不受支持")
			}
			item.URL, err = signedOSSObjectURL(setting, resource.ObjectKey, time.Now().Add(providerResourceURLTTL))
			if err != nil {
				return nil, err
			}
		}
		media = append(media, item)
	}
	return media, nil
}
