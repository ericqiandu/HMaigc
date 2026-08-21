package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	kuaiziSeedreamLiteModel        = "seedream5.0lite"
	kuaiziSeedreamProModel         = "seedream5.0pro"
	kuaiziSeedreamCreatePath       = "/ai-open-platform-api/v1/seedream/image/task/create"
	kuaiziSeedreamStatusPath       = "/ai-open-platform-api/v1/seedream/image/task/status"
	kuaiziSeedreamPoll             = 5 * time.Second
	seedreamMinOutputPixels  int64 = 3_686_400
	seedreamMaxOutputPixels  int64 = 10_404_496
)

type kuaiziSeedreamRequestError struct {
	statusCode int
	code       int
	message    string
	traceID    string
}

type kuaiziSeedreamGatewayResponse struct {
	Code    int                    `json:"code"`
	Message string                 `json:"message"`
	Data    map[string]interface{} `json:"data"`
	TraceID string                 `json:"trace_id"`
}

func (failure kuaiziSeedreamRequestError) Error() string {
	details := strings.TrimSpace(failure.message)
	if details == "" {
		details = "请求失败"
	}
	parts := []string{fmt.Sprintf("Seedream 上游请求失败（HTTP %d / code %d）：%s", failure.statusCode, failure.code, details)}
	if failure.traceID != "" {
		parts = append(parts, "trace_id="+failure.traceID)
	}
	return strings.Join(parts, "，")
}

func (failure kuaiziSeedreamRequestError) Unwrap() error {
	return kuaiziCompatibleHTTPError{statusCode: failure.statusCode, code: strconv.Itoa(failure.code), message: failure.message}
}

func runKuaiziSeedreamTask(ctx context.Context, input canvasGenerationInput) (map[string]interface{}, error) {
	return runKuaiziSeedreamTaskWithPollInterval(ctx, input, kuaiziSeedreamPoll)
}

func runKuaiziSeedreamTaskWithPollInterval(ctx context.Context, input canvasGenerationInput, pollInterval time.Duration) (map[string]interface{}, error) {
	taskID := resumedProviderRequestID(ctx)
	createdThisAttempt := taskID == ""
	if taskID == "" {
		payload, err := kuaiziSeedreamCreatePayload(input)
		if err != nil {
			return nil, err
		}
		created, _, err := requestKuaiziSeedreamJSON(withProviderAsyncCreate(withProviderRequestKind(ctx, "create")), input.Config, kuaiziSeedreamCreatePath, payload)
		if err != nil {
			if compatibleCreateDefinitelyRejected(err) {
				return nil, err
			}
			return nil, &KuaiziCompatibleCreateError{err: err}
		}
		taskID = strings.TrimSpace(stringField(created, "task_id"))
	}
	if !validKuaiziCompatibleTaskID(taskID, input.Config.APIKey) {
		err := errors.New("Seedream 接口没有返回有效任务 ID")
		if createdThisAttempt {
			return nil, &KuaiziCompatibleCreateError{err: err}
		}
		return nil, err
	}

	var lastTransientPollError error
	for deadline := providerPollingDeadline(ctx); time.Now().Before(deadline); {
		state, traceID, err := requestKuaiziSeedreamJSON(withProviderRequestKind(ctx, "poll"), input.Config, kuaiziSeedreamStatusPath, map[string]string{"task_id": taskID})
		if err != nil {
			if !retryableKuaiziPollError(err) {
				return nil, err
			}
			lastTransientPollError = err
			if err := sleepContext(ctx, pollInterval); err != nil {
				return nil, err
			}
			continue
		}
		if returnedID := strings.TrimSpace(stringField(state, "task_id")); returnedID != taskID {
			return nil, fmt.Errorf("Seedream 查询返回了不匹配的任务 ID（task_id=%s，trace_id=%s）", taskID, traceID)
		}
		switch strings.ToLower(strings.TrimSpace(stringField(state, "status"))) {
		case "succeeded":
			imageURLs, err := stringSliceField(state, "image_urls")
			if err != nil || len(imageURLs) != 1 {
				return nil, fmt.Errorf("Seedream 任务 %s 必须返回且只返回 1 张图片（trace_id=%s）", taskID, traceID)
			}
			imageURL := strings.TrimSpace(imageURLs[0])
			if err := validateKuaiziSeedreamResultURL(imageURL, input); err != nil {
				return nil, fmt.Errorf("Seedream 任务 %s 已成功但图片地址无效（trace_id=%s）：%w", taskID, traceID, err)
			}
			data, mimeType, err := getExternalBinary(withKuaiziRequest(withProviderRequestKind(ctx, "download")), imageURL)
			if err != nil {
				return nil, fmt.Errorf("Seedream 图片下载失败（task_id=%s，trace_id=%s）：%w", taskID, traceID, err)
			}
			mimeType = normalizedMediaMimeType(mimeType, data)
			if !strings.HasPrefix(mimeType, "image/") {
				return nil, fmt.Errorf("Seedream 任务 %s 返回的内容不是图片（trace_id=%s）", taskID, traceID)
			}
			return map[string]interface{}{
				"mode":   "image",
				"images": []map[string]string{{"dataUrl": dataURL(mimeType, data), "mimeType": mimeType, "taskId": taskID, "traceId": traceID}},
			}, nil
		case "failed":
			failure := strings.TrimSpace(stringField(state, "error"))
			if failure == "" {
				failure = "上游未返回失败原因"
			}
			return nil, fmt.Errorf("Seedream 生成失败（task_id=%s，trace_id=%s）：%s", taskID, traceID, failure)
		case "running":
			if err := sleepContext(ctx, pollInterval); err != nil {
				return nil, err
			}
		case "":
			return nil, fmt.Errorf("Seedream 任务 %s 未返回状态（trace_id=%s）", taskID, traceID)
		default:
			return nil, fmt.Errorf("Seedream 任务 %s 返回未知状态（trace_id=%s）", taskID, traceID)
		}
	}
	if lastTransientPollError != nil {
		return nil, fmt.Errorf("Seedream 生成超时（task_id=%s，最后一次状态查询失败：%w）", taskID, lastTransientPollError)
	}
	return nil, fmt.Errorf("Seedream 生成超时（task_id=%s）", taskID)
}

func retryableKuaiziPollError(err error) bool {
	var failure kuaiziCompatibleHTTPError
	if !errors.As(err, &failure) {
		return false
	}
	switch failure.statusCode {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func kuaiziSeedreamCreatePayload(input canvasGenerationInput) (map[string]interface{}, error) {
	spec, ok := kuaiziProviderModelSpec(strings.TrimSpace(input.Config.Model))
	if !ok {
		return nil, fmt.Errorf("Seedream 模型未登记：%s", strings.TrimSpace(input.Config.Model))
	}
	family, _, familyExists := kuaiziProviderFamilyForModel(spec.ModelKey)
	if !familyExists || family != "seedream" {
		return nil, fmt.Errorf("Seedream 接口不支持模型：%s", spec.ModelKey)
	}
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		return nil, errors.New("Seedream 提示词不能为空")
	}
	if input.Mask != nil {
		return nil, errors.New("Seedream 当前画布契约不支持蒙版编辑")
	}
	if len(input.ReferenceVideos) > 0 || len(input.ReferenceAudios) > 0 {
		return nil, errors.New("Seedream 只支持图片参考素材")
	}
	if count := strings.TrimSpace(input.Config.Count); count != "" && count != "1" {
		return nil, errors.New("Seedream 每个任务只支持生成 1 张图片")
	}
	if len(input.ReferenceImages) > spec.MaxImages {
		return nil, fmt.Errorf("%s 最多支持 %d 张参考图", spec.DisplayName, spec.MaxImages)
	}
	if parseBool(input.Config.TransparentBackground, false) {
		return nil, errors.New("Seedream 不支持透明背景")
	}
	if strings.TrimSpace(input.Config.Quality) != "" {
		return nil, errors.New("Seedream 不支持画质档位参数")
	}
	size, err := kuaiziSeedreamSize(input.Config.Size)
	if err != nil {
		return nil, err
	}
	payload := map[string]interface{}{
		"model":         spec.ModelKey,
		"prompt":        prompt,
		"size":          size,
		"output_format": "jpeg",
	}
	if spec.ModelKey == kuaiziSeedreamLiteModel {
		payload["sequential_image_generation"] = "disabled"
	}
	insertFrozenWatermark(payload, "watermark", input.Watermark)
	if len(input.ReferenceImages) > 0 {
		images := make([]string, 0, len(input.ReferenceImages))
		for _, image := range input.ReferenceImages {
			imageURL, err := mediaReferenceURL(image)
			if err != nil || !isPublicMediaURL(imageURL) {
				return nil, errors.New("Seedream 参考图必须使用公网 URL 或已上传 OSS 的资源")
			}
			images = append(images, imageURL)
		}
		payload["image"] = images
	}
	return payload, nil
}

func kuaiziSeedreamSize(value string) (string, error) {
	size := strings.ToLower(strings.TrimSpace(value))
	if size == "2k" || size == "3k" {
		return size, nil
	}
	parts := strings.Split(size, "x")
	if len(parts) != 2 {
		return "", fmt.Errorf("Seedream 图片尺寸无效：%s", strings.TrimSpace(value))
	}
	width, widthErr := strconv.Atoi(parts[0])
	height, heightErr := strconv.Atoi(parts[1])
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return "", fmt.Errorf("Seedream 图片尺寸无效：%s", strings.TrimSpace(value))
	}
	ratio := float64(width) / float64(height)
	if ratio < 1.0/16.0 || ratio > 16 {
		return "", errors.New("Seedream 图片宽高比必须在 1:16–16:1 之间")
	}
	pixels := int64(width) * int64(height)
	if pixels < seedreamMinOutputPixels || pixels > seedreamMaxOutputPixels {
		return "", fmt.Errorf("Seedream 图片总像素必须在 %d–%d 之间", seedreamMinOutputPixels, seedreamMaxOutputPixels)
	}
	return fmt.Sprintf("%dx%d", width, height), nil
}

func requestKuaiziSeedreamJSON(ctx context.Context, config providerConfig, path string, payload interface{}) (map[string]interface{}, string, error) {
	apiKey := strings.TrimSpace(config.APIKey)
	if apiKey == "" {
		return nil, "", errors.New("Seedream API Key 不能为空")
	}
	base, err := ValidateKuaiziBaseURL(ctx, config.BaseURL, strings.TrimSpace(os.Getenv("CANVAS_ENVIRONMENT")))
	if err != nil {
		return nil, "", err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, "", fmt.Errorf("编码 Seedream 请求失败：%w", err)
	}
	request, err := http.NewRequestWithContext(withKuaiziRequest(ctx), http.MethodPost, strings.TrimRight(base.String(), "/")+path, bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	request.Header.Set("ApiKey", apiKey)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	responseBody, mimeType, err := doBinary(request)
	if err != nil {
		var httpErr providerHTTPError
		if errors.As(err, &httpErr) {
			response := kuaiziSeedreamGatewayResponse{}
			if json.Unmarshal([]byte(httpErr.Body), &response) == nil {
				return nil, strings.TrimSpace(response.TraceID), kuaiziSeedreamRequestError{
					statusCode: httpErr.StatusCode,
					code:       response.Code,
					message:    response.Message,
					traceID:    strings.TrimSpace(response.TraceID),
				}
			}
			return nil, "", kuaiziSeedreamRequestError{statusCode: httpErr.StatusCode, code: httpErr.StatusCode}
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, "", err
		}
		if strings.Contains(err.Error(), "不允许重定向") {
			return nil, "", errors.New("Seedream 上游请求不允许重定向")
		}
		return nil, "", fmt.Errorf("Seedream 上游请求失败：%w", err)
	}
	if !strings.Contains(mimeType, "json") && !json.Valid(responseBody) {
		return nil, "", errors.New("Seedream 上游返回了非 JSON 内容")
	}
	var response kuaiziSeedreamGatewayResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, "", errors.New("Seedream 上游响应格式无效")
	}
	traceID := strings.TrimSpace(response.TraceID)
	if response.Code != 0 {
		statusCode := response.Code
		if statusCode < 100 || statusCode > 599 {
			statusCode = http.StatusInternalServerError
		}
		return nil, traceID, kuaiziSeedreamRequestError{statusCode: statusCode, code: response.Code, message: response.Message, traceID: traceID}
	}
	if response.Data == nil {
		return nil, traceID, fmt.Errorf("Seedream 上游响应缺少 data 对象（trace_id=%s）", traceID)
	}
	return response.Data, traceID, nil
}

func stringSliceField(data map[string]interface{}, field string) ([]string, error) {
	raw, ok := data[field].([]interface{})
	if !ok {
		return nil, fmt.Errorf("%s 格式无效", field)
	}
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		value, ok := item.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%s 格式无效", field)
		}
		values = append(values, value)
	}
	return values, nil
}

func validateKuaiziSeedreamResultURL(value string, input canvasGenerationInput) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil {
		return errors.New("图片地址格式无效")
	}
	if parsed.Scheme != "https" && !(strings.TrimSpace(os.Getenv("CANVAS_ENVIRONMENT")) == "development" && parsed.Scheme == "http") {
		return errors.New("图片地址必须使用 HTTPS")
	}
	apiKey := strings.TrimSpace(input.Config.APIKey)
	if apiKey != "" && strings.Contains(value, apiKey) {
		return errors.New("图片地址包含供应商凭据")
	}
	return nil
}
