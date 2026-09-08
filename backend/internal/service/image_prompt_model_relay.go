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

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

const (
	imagePromptModelRelayRequestVersion  = "image-prompt-model-relay-request/v1"
	imagePromptModelRelayResponseVersion = "image-prompt-model-relay-response/v1"
	imagePromptModelRelayProviderTimeout = 90 * time.Second
	imagePromptModelRelayMaxResponse     = 1 << 20
)

type ImagePromptModelRelayMessageV1 struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ImagePromptModelRelayRequestV1 struct {
	SchemaVersion     string                                   `json:"schemaVersion"`
	CorrelationID     string                                   `json:"correlationId"`
	CommercialContext imagePromptSpecialistCommercialContextV1 `json:"commercialContext"`
	PromptModel       imagePromptSpecialistModelBindingV1      `json:"promptModel"`
	Messages          []ImagePromptModelRelayMessageV1         `json:"messages"`
	Tools             []struct{}                               `json:"tools"`
	Stream            *bool                                    `json:"stream"`
}

type ImagePromptModelRelayUsageV1 struct {
	InputTokens  int64 `json:"inputTokens"`
	CachedTokens int64 `json:"cachedTokens"`
	OutputTokens int64 `json:"outputTokens"`
	TotalTokens  int64 `json:"totalTokens"`
}

type ImagePromptModelRelayResponseV1 struct {
	SchemaVersion     string                       `json:"schemaVersion"`
	Text              string                       `json:"text"`
	EffectiveModel    string                       `json:"effectiveModel"`
	ProviderRequestID string                       `json:"providerRequestId"`
	Usage             ImagePromptModelRelayUsageV1 `json:"usage"`
}

type ImagePromptModelRelayError struct {
	Code       imagePromptSpecialistErrorCode
	Reason     string
	HTTPStatus int
	Cause      error
}

func (failure *ImagePromptModelRelayError) Error() string {
	if failure == nil {
		return "image prompt model relay failed"
	}
	return fmt.Sprintf("图片提示词模型中继失败（%s）：%s", failure.Code, failure.Reason)
}

func (failure *ImagePromptModelRelayError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.Cause
}

func (failure *ImagePromptModelRelayError) StableCode() string {
	if failure == nil {
		return string(imagePromptErrorUnavailable)
	}
	return string(failure.Code)
}

func (failure *ImagePromptModelRelayError) SafeReason() string {
	if failure == nil || strings.TrimSpace(failure.Reason) == "" {
		return "图片提示词模型中继失败"
	}
	return truncateRunes(strings.TrimSpace(failure.Reason), 500)
}

func (failure *ImagePromptModelRelayError) StatusCode() int {
	if failure == nil || failure.HTTPStatus == 0 {
		return http.StatusServiceUnavailable
	}
	return failure.HTTPStatus
}

type imagePromptModelRelayProviderResult struct {
	Text              string
	EffectiveModel    string
	ProviderRequestID string
	Usage             ImagePromptModelRelayUsageV1
	UsageAvailable    bool
	StatusCode        int
	Duration          time.Duration
	UpstreamURL       string
}

type imagePromptModelRelayProviderError struct {
	statusCode  int
	duration    time.Duration
	upstreamURL string
	cause       error
}

func (failure *imagePromptModelRelayProviderError) Error() string {
	return "图片提示词上游模型调用失败"
}

func (failure *imagePromptModelRelayProviderError) Unwrap() error { return failure.cause }

func validateImagePromptModelRelayRequest(request ImagePromptModelRelayRequestV1) error {
	if request.SchemaVersion != imagePromptModelRelayRequestVersion || !boundedTrimmed(request.CorrelationID, 160) {
		return errors.New("模型中继请求身份无效")
	}
	if !boundedTrimmed(request.CommercialContext.GenerationTaskID, 160) || !boundedTrimmed(request.CommercialContext.BillingOrderID, 160) || !validSHA256Hex(request.CommercialContext.QuoteFingerprint) {
		return errors.New("模型中继商业上下文无效")
	}
	if !boundedTrimmed(request.PromptModel.CatalogRecordID, 160) || !boundedTrimmed(request.PromptModel.ModelKey, 160) || !boundedTrimmed(request.PromptModel.ConfigurationRevision, 160) || !boundedTrimmed(request.PromptModel.ProviderEndpointVersionID, 160) || !boundedTrimmed(request.PromptModel.ProviderCredentialVersionID, 160) {
		return errors.New("模型中继冻结模型绑定无效")
	}
	if len(request.Messages) != 2 || request.Messages[0].Role != "system" || request.Messages[1].Role != "user" {
		return errors.New("模型中继只允许 system/user 两条消息")
	}
	for _, message := range request.Messages {
		if !boundedTrimmed(message.Content, 64<<10) {
			return errors.New("模型中继消息正文无效")
		}
	}
	if request.Tools == nil || len(request.Tools) != 0 || request.Stream == nil || *request.Stream {
		return errors.New("模型中继只允许空工具和非流式调用")
	}
	return nil
}

func (s *Service) ExecuteImagePromptModelRelay(ctx context.Context, request ImagePromptModelRelayRequestV1) (ImagePromptModelRelayResponseV1, error) {
	if err := validateImagePromptModelRelayRequest(request); err != nil {
		return ImagePromptModelRelayResponseV1{}, &ImagePromptModelRelayError{Code: imagePromptErrorRequestInvalid, Reason: err.Error(), HTTPStatus: http.StatusBadRequest}
	}
	task, order, record, runtime, err := s.resolveImagePromptModelRelayRuntime(request)
	if err != nil {
		return ImagePromptModelRelayResponseV1{}, err
	}
	apiKey, err := NewProviderSecretCipher(s.dataDir).Decrypt(runtime.ProviderAccountID, runtime.ProviderCredentialID, runtime.CredentialVersion, runtime.KeyCipher)
	if err != nil || strings.TrimSpace(apiKey) == "" {
		return ImagePromptModelRelayResponseV1{}, &ImagePromptModelRelayError{Code: imagePromptErrorModelBindingInvalid, Reason: "冻结的图片提示词模型凭据无法解密", HTTPStatus: http.StatusConflict, Cause: err}
	}
	providerConfig := providerConfig{
		ChannelID: record.ChannelID, APIFormat: "openai", InterfaceType: string(model.ChannelInterfaceChatCompletion),
		BaseURL: kuaiziChatCompletionsBaseURL(runtime.BaseURL), APIKey: apiKey, Model: request.PromptModel.ModelKey, ManagedProviderRuntime: true,
	}
	callProvider := s.imagePromptModelRelayProvider
	if callProvider == nil {
		callProvider = s.callImagePromptModelRelayProvider
	}
	result, callErr := callProvider(ctx, providerConfig, request.Messages)
	if callErr != nil {
		providerFailure := &imagePromptModelRelayProviderError{}
		if errors.As(callErr, &providerFailure) {
			if logErr := s.saveImagePromptModelRelayCall(*task, *order, *record, imagePromptModelRelayProviderResult{StatusCode: providerFailure.statusCode, Duration: providerFailure.duration, UpstreamURL: providerFailure.upstreamURL}, callErr); logErr != nil {
				return ImagePromptModelRelayResponseV1{}, relayReconciliationError(logErr)
			}
		}
		if logErr := s.log(task.UserID, task.ID, "error", "图片提示词模型中继调用失败", relayTaskLogPayload(request.CorrelationID, request.PromptModel, "", ImagePromptModelRelayUsageV1{})); logErr != nil {
			return ImagePromptModelRelayResponseV1{}, relayReconciliationError(logErr)
		}
		return ImagePromptModelRelayResponseV1{}, &ImagePromptModelRelayError{Code: imagePromptErrorUnavailable, Reason: "图片提示词上游模型调用失败", HTTPStatus: http.StatusBadGateway, Cause: callErr}
	}
	if result.EffectiveModel != request.PromptModel.ModelKey {
		if logErr := s.saveImagePromptModelRelayCall(*task, *order, *record, result, errors.New("供应商返回模型与冻结模型不一致")); logErr != nil {
			return ImagePromptModelRelayResponseV1{}, relayReconciliationError(logErr)
		}
		if logErr := s.log(task.UserID, task.ID, "error", "图片提示词模型继承校验失败", relayTaskLogPayload(request.CorrelationID, request.PromptModel, result.ProviderRequestID, result.Usage)); logErr != nil {
			return ImagePromptModelRelayResponseV1{}, relayReconciliationError(logErr)
		}
		return ImagePromptModelRelayResponseV1{}, &ImagePromptModelRelayError{Code: imagePromptErrorModelInheritanceFailed, Reason: "供应商返回模型与冻结模型不一致", HTTPStatus: http.StatusBadGateway}
	}
	if !boundedTrimmed(result.Text, 128<<10) || !boundedTrimmed(result.ProviderRequestID, 160) || !result.UsageAvailable || result.Usage.InputTokens < 0 || result.Usage.CachedTokens < 0 || result.Usage.CachedTokens > result.Usage.InputTokens || result.Usage.OutputTokens < 0 || result.Usage.TotalTokens != result.Usage.InputTokens+result.Usage.OutputTokens {
		if logErr := s.saveImagePromptModelRelayCall(*task, *order, *record, result, errors.New("供应商响应证据无效")); logErr != nil {
			return ImagePromptModelRelayResponseV1{}, relayReconciliationError(logErr)
		}
		if logErr := s.log(task.UserID, task.ID, "error", "图片提示词模型响应证据无效", relayTaskLogPayload(request.CorrelationID, request.PromptModel, result.ProviderRequestID, result.Usage)); logErr != nil {
			return ImagePromptModelRelayResponseV1{}, relayReconciliationError(logErr)
		}
		return ImagePromptModelRelayResponseV1{}, &ImagePromptModelRelayError{Code: imagePromptErrorOutputInvalid, Reason: "图片提示词上游响应证据无效", HTTPStatus: http.StatusBadGateway}
	}
	if err := s.saveImagePromptModelRelayCall(*task, *order, *record, result, nil); err != nil {
		return ImagePromptModelRelayResponseV1{}, relayReconciliationError(err)
	}
	if err := s.log(task.UserID, task.ID, "info", "图片提示词模型中继已完成", relayTaskLogPayload(request.CorrelationID, request.PromptModel, result.ProviderRequestID, result.Usage)); err != nil {
		return ImagePromptModelRelayResponseV1{}, relayReconciliationError(err)
	}
	return ImagePromptModelRelayResponseV1{
		SchemaVersion: imagePromptModelRelayResponseVersion, Text: result.Text, EffectiveModel: result.EffectiveModel,
		ProviderRequestID: result.ProviderRequestID, Usage: result.Usage,
	}, nil
}

func (s *Service) resolveImagePromptModelRelayRuntime(request ImagePromptModelRelayRequestV1) (*model.Task, *model.BillingOrder, *model.ChannelModel, *repository.FrozenProviderRuntime, error) {
	task, err := s.repo.Task(request.CommercialContext.GenerationTaskID)
	if err != nil || task.ID != request.CorrelationID || task.Type != "canvas_image" || task.Capability != "image" || task.Status != model.TaskStatusRunning || task.BillingOrderID != request.CommercialContext.BillingOrderID {
		return nil, nil, nil, nil, &ImagePromptModelRelayError{Code: imagePromptErrorRequestInvalid, Reason: "图片生成父任务状态或身份不一致", HTTPStatus: http.StatusConflict, Cause: err}
	}
	order, err := s.repo.BillingOrder(request.CommercialContext.BillingOrderID)
	if err != nil || order.TaskID != task.ID || order.UserID != task.UserID || order.Capability != "image" || (order.Status != model.BillingStatusReserved && order.Status != model.BillingStatusRunning) {
		return nil, nil, nil, nil, &ImagePromptModelRelayError{Code: imagePromptErrorRequestInvalid, Reason: "图片生成订单状态或身份不一致", HTTPStatus: http.StatusConflict, Cause: err}
	}
	fingerprint, err := billingOrderQuoteFingerprint(order)
	if err != nil || !constantTimeStringEqual(fingerprint, request.CommercialContext.QuoteFingerprint) {
		return nil, nil, nil, nil, &ImagePromptModelRelayError{Code: imagePromptErrorRequestInvalid, Reason: "图片生成报价事实不一致", HTTPStatus: http.StatusConflict, Cause: err}
	}
	var input canvasGenerationInput
	if err := json.Unmarshal([]byte(task.InputJSON), &input); err != nil || input.ImagePromptCommand == nil || input.PromptModel == nil || *input.PromptModel != request.PromptModel {
		return nil, nil, nil, nil, &ImagePromptModelRelayError{Code: imagePromptErrorModelBindingInvalid, Reason: "父任务未冻结相同的图片提示词模型", HTTPStatus: http.StatusConflict, Cause: err}
	}
	if err := validateCanvasImagePromptCommand(*input.ImagePromptCommand); err != nil {
		return nil, nil, nil, nil, &ImagePromptModelRelayError{Code: imagePromptErrorRequestInvalid, Reason: "父任务图片命令无效", HTTPStatus: http.StatusConflict, Cause: err}
	}
	if err := s.verifyFrozenImagePromptModel(request.PromptModel); err != nil {
		return nil, nil, nil, nil, &ImagePromptModelRelayError{Code: imagePromptErrorModelBindingInvalid, Reason: "冻结的图片提示词模型已变更或不可用", HTTPStatus: http.StatusConflict, Cause: err}
	}
	record, err := s.repo.ChannelModelByRecordID(request.PromptModel.CatalogRecordID)
	if err != nil {
		return nil, nil, nil, nil, &ImagePromptModelRelayError{Code: imagePromptErrorModelBindingInvalid, Reason: "冻结的图片提示词模型记录不存在", HTTPStatus: http.StatusConflict, Cause: err}
	}
	runtime, err := s.repo.FrozenProviderRuntimeByVersionIDs(request.PromptModel.ProviderEndpointVersionID, request.PromptModel.ProviderCredentialVersionID)
	if err != nil || runtime.ProviderKind != kuaiziProviderKind || runtime.ProviderCredentialID != record.ProviderCredentialID {
		return nil, nil, nil, nil, &ImagePromptModelRelayError{Code: imagePromptErrorModelBindingInvalid, Reason: "冻结的图片提示词供应商版本不一致", HTTPStatus: http.StatusConflict, Cause: err}
	}
	return task, order, record, runtime, nil
}

func (s *Service) callImagePromptModelRelayProvider(ctx context.Context, config providerConfig, messages []ImagePromptModelRelayMessageV1) (imagePromptModelRelayProviderResult, error) {
	body := struct {
		Model    string                           `json:"model"`
		Messages []ImagePromptModelRelayMessageV1 `json:"messages"`
		Tools    []struct{}                       `json:"tools"`
		Stream   bool                             `json:"stream"`
	}{Model: config.Model, Messages: messages, Tools: []struct{}{}, Stream: false}
	encoded, err := json.Marshal(body)
	if err != nil {
		return imagePromptModelRelayProviderResult{}, err
	}
	upstreamURL := apiURL(config.BaseURL, "/chat/completions")
	if s.imagePromptModelRelayHTTPClient == nil {
		if _, err := ValidateOutboundURL(upstreamURL); err != nil {
			return imagePromptModelRelayProviderResult{}, err
		}
	}
	requestCtx, cancel := context.WithTimeout(ctx, imagePromptModelRelayProviderTimeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(withKuaiziRequest(requestCtx), http.MethodPost, upstreamURL, bytes.NewReader(encoded))
	if err != nil {
		return imagePromptModelRelayProviderResult{}, err
	}
	httpRequest.Header.Set("ApiKey", config.APIKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	client := s.imagePromptModelRelayHTTPClient
	if client == nil {
		client = KuaiziHTTPClient(strings.TrimSpace(os.Getenv("CANVAS_ENVIRONMENT")), imagePromptModelRelayProviderTimeout)
	}
	startedAt := time.Now()
	response, err := client.Do(httpRequest)
	if err != nil {
		return imagePromptModelRelayProviderResult{}, &imagePromptModelRelayProviderError{duration: time.Since(startedAt), upstreamURL: upstreamURL, cause: err}
	}
	defer response.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, imagePromptModelRelayMaxResponse+1))
	if readErr != nil || len(responseBody) > imagePromptModelRelayMaxResponse {
		if readErr == nil {
			readErr = errors.New("供应商响应超过大小限制")
		}
		return imagePromptModelRelayProviderResult{}, &imagePromptModelRelayProviderError{statusCode: response.StatusCode, duration: time.Since(startedAt), upstreamURL: upstreamURL, cause: readErr}
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return imagePromptModelRelayProviderResult{}, &imagePromptModelRelayProviderError{statusCode: response.StatusCode, duration: time.Since(startedAt), upstreamURL: upstreamURL, cause: errors.New("供应商返回非成功状态")}
	}
	var payload imagePromptProviderChatResponse
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return imagePromptModelRelayProviderResult{}, &imagePromptModelRelayProviderError{statusCode: response.StatusCode, duration: time.Since(startedAt), upstreamURL: upstreamURL, cause: errors.New("供应商返回无效 JSON")}
	}
	if payload.Data != nil {
		payload = *payload.Data
	}
	result := imagePromptModelRelayProviderResult{
		Text: payload.text(), EffectiveModel: strings.TrimSpace(payload.Model), ProviderRequestID: strings.TrimSpace(payload.ID),
		Usage: payload.Usage.normalized(), UsageAvailable: payload.Usage != nil, StatusCode: response.StatusCode, Duration: time.Since(startedAt), UpstreamURL: upstreamURL,
	}
	return result, nil
}

type imagePromptProviderChatResponse struct {
	ID      string                           `json:"id"`
	Model   string                           `json:"model"`
	Choices []imagePromptProviderChatChoice  `json:"choices"`
	Usage   *imagePromptProviderChatUsage    `json:"usage"`
	Data    *imagePromptProviderChatResponse `json:"data"`
}

type imagePromptProviderChatChoice struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Text string `json:"text"`
}

type imagePromptProviderChatUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
	PromptDetails    struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	InputDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
}

func (payload imagePromptProviderChatResponse) text() string {
	var chunks []string
	for _, choice := range payload.Choices {
		if text := strings.TrimSpace(choice.Message.Content); text != "" {
			chunks = append(chunks, text)
		} else if text := strings.TrimSpace(choice.Text); text != "" {
			chunks = append(chunks, text)
		}
	}
	return strings.Join(chunks, "")
}

func (usage *imagePromptProviderChatUsage) normalized() ImagePromptModelRelayUsageV1 {
	if usage == nil {
		return ImagePromptModelRelayUsageV1{}
	}
	input := usage.InputTokens
	if input == 0 {
		input = usage.PromptTokens
	}
	output := usage.OutputTokens
	if output == 0 {
		output = usage.CompletionTokens
	}
	cached := usage.InputDetails.CachedTokens
	if cached == 0 {
		cached = usage.PromptDetails.CachedTokens
	}
	return ImagePromptModelRelayUsageV1{InputTokens: input, CachedTokens: cached, OutputTokens: output, TotalTokens: usage.TotalTokens}
}

func (s *Service) saveImagePromptModelRelayCall(task model.Task, order model.BillingOrder, record model.ChannelModel, result imagePromptModelRelayProviderResult, callErr error) error {
	status := model.ApiCallStatusSucceeded
	errorText := ""
	if callErr != nil {
		status = model.ApiCallStatusFailed
		errorText = "图片提示词模型中继上游调用失败"
	}
	log := model.ApiCallLog{
		ID: newID(), UserID: task.UserID, ChannelID: record.ChannelID, TaskID: task.ID, BillingOrderID: order.ID,
		Source: "image-prompt-model-relay", Capability: "text", Operation: imagePromptSpecialistName,
		RequestKind: "create", Billable: false, APIFormat: "openai", Method: http.MethodPost, Path: "/chat/completions",
		Model: record.ModelKey, Status: status, StatusCode: result.StatusCode, DurationMs: result.Duration.Milliseconds(),
		InputTokens: result.Usage.InputTokens, CachedTokens: result.Usage.CachedTokens, OutputTokens: result.Usage.OutputTokens,
		UsageAvailable:    result.UsageAvailable,
		ProviderRequestID: result.ProviderRequestID, Error: errorText, UpstreamURL: result.UpstreamURL, CreatedAt: time.Now().UTC(),
	}
	s.estimateCallCost(&log)
	if log.UsageAvailable && !log.CostAvailable && strings.TrimSpace(log.CostCalculationError) == "" {
		log.CostCalculationError = "图片提示词模型未配置内部成本价格"
	}
	if err := s.repo.Create(&log); err != nil {
		return fmt.Errorf("保存图片提示词模型调用证据: %w", err)
	}
	return nil
}

func relayTaskLogPayload(correlationID string, binding imagePromptSpecialistModelBindingV1, providerRequestID string, usage ImagePromptModelRelayUsageV1) string {
	payload := struct {
		CorrelationID     string                              `json:"correlationId"`
		PromptModel       imagePromptSpecialistModelBindingV1 `json:"promptModel"`
		ProviderRequestID string                              `json:"providerRequestId,omitempty"`
		Usage             ImagePromptModelRelayUsageV1        `json:"usage"`
	}{CorrelationID: correlationID, PromptModel: binding, ProviderRequestID: providerRequestID, Usage: usage}
	encoded, _ := json.Marshal(payload)
	return string(encoded)
}

func relayReconciliationError(cause error) error {
	return &ImagePromptModelRelayError{Code: imagePromptErrorUnavailable, Reason: "供应商调用已发生但本地证据提交失败，需要人工核对", HTTPStatus: http.StatusServiceUnavailable, Cause: cause}
}
