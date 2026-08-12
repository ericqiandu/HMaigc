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
	kuaiziGPTImage2Model      = "kz_gpt_image2"
	kuaiziGPTImage2CreatePath = "/ai-open-platform-api/v1/chatgpt/image/task/create"
	kuaiziGPTImage2StatusPath = "/ai-open-platform-api/v1/chatgpt/image/task/status"
	kuaiziGPTImage2Poll       = 5 * time.Second
)

func runKuaiziGPTImage2Task(ctx context.Context, input canvasGenerationInput) (map[string]interface{}, error) {
	taskID := resumedProviderRequestID(ctx)
	createdThisAttempt := taskID == ""
	if taskID == "" {
		payload, err := kuaiziGPTImage2CreatePayload(input)
		if err != nil {
			return nil, err
		}
		var created map[string]interface{}
		if err := requestKuaiziGPTImage2JSON(withProviderRequestKind(ctx, "create"), input.Config, kuaiziGPTImage2CreatePath, payload, &created); err != nil {
			if compatibleCreateDefinitelyRejected(err) {
				return nil, err
			}
			return nil, &KuaiziCompatibleCreateError{err: err}
		}
		taskID = strings.TrimSpace(stringField(created, "task_id"))
	}
	if !validKuaiziCompatibleTaskID(taskID, input.Config.APIKey) {
		err := errors.New("GPT Image 2 接口没有返回有效任务 ID")
		if createdThisAttempt {
			return nil, &KuaiziCompatibleCreateError{err: err}
		}
		return nil, err
	}

	for deadline := providerPollingDeadline(ctx); time.Now().Before(deadline); {
		var state map[string]interface{}
		if err := requestKuaiziGPTImage2JSON(withProviderRequestKind(ctx, "poll"), input.Config, kuaiziGPTImage2StatusPath, map[string]string{"task_id": taskID}, &state); err != nil {
			return nil, err
		}
		if returnedID := strings.TrimSpace(stringField(state, "task_id")); returnedID != taskID {
			return nil, errors.New("GPT Image 2 查询返回了不匹配的任务 ID")
		}
		switch strings.ToLower(strings.TrimSpace(stringField(state, "status"))) {
		case "succeeded":
			imageURL := strings.TrimSpace(stringField(state, "image_url"))
			if err := validateKuaiziGPTImage2ResultURL(imageURL, input); err != nil {
				return nil, fmt.Errorf("GPT Image 2 任务 %s 已成功但图片地址无效：%w", taskID, err)
			}
			data, mimeType, err := getExternalBinary(withKuaiziRequest(withProviderRequestKind(ctx, "download")), imageURL)
			if err != nil {
				return nil, fmt.Errorf("GPT Image 2 图片下载失败（任务 %s）：%w", taskID, err)
			}
			mimeType = normalizedMediaMimeType(mimeType, data)
			if !strings.HasPrefix(mimeType, "image/") {
				return nil, fmt.Errorf("GPT Image 2 任务 %s 返回的内容不是图片", taskID)
			}
			return map[string]interface{}{
				"mode":   "image",
				"images": []map[string]string{{"dataUrl": dataURL(mimeType, data), "mimeType": mimeType, "taskId": taskID}},
			}, nil
		case "failed":
			return nil, fmt.Errorf("GPT Image 2 生成失败（任务 %s），请在请求日志中按任务 ID 核对上游原因", taskID)
		case "running":
			if err := sleepContext(ctx, kuaiziGPTImage2Poll); err != nil {
				return nil, err
			}
		case "":
			return nil, fmt.Errorf("GPT Image 2 任务 %s 未返回状态", taskID)
		default:
			return nil, fmt.Errorf("GPT Image 2 任务 %s 返回未知状态", taskID)
		}
	}
	return nil, fmt.Errorf("GPT Image 2 生成超时（任务 %s）", taskID)
}

func kuaiziGPTImage2CreatePayload(input canvasGenerationInput) (map[string]interface{}, error) {
	if strings.TrimSpace(input.Config.Model) != kuaiziGPTImage2Model {
		return nil, fmt.Errorf("GPT Image 2 接口不支持模型：%s", strings.TrimSpace(input.Config.Model))
	}
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		return nil, errors.New("GPT Image 2 提示词不能为空")
	}
	if input.Mask != nil {
		return nil, errors.New("GPT Image 2 暂不支持画布蒙版编辑")
	}
	if parseBool(input.Config.TransparentBackground, false) {
		return nil, errors.New("GPT Image 2 暂不支持透明背景")
	}
	if len(input.ReferenceVideos) > 0 || len(input.ReferenceAudios) > 0 {
		return nil, errors.New("GPT Image 2 只支持图片参考素材")
	}
	if count := strings.TrimSpace(input.Config.Count); count != "" && count != "1" {
		return nil, errors.New("GPT Image 2 当前每个任务只支持生成 1 张图片")
	}
	quality, err := kuaiziGPTImage2Quality(input.Config.Quality)
	if err != nil {
		return nil, err
	}
	payload := map[string]interface{}{"model": kuaiziGPTImage2Model, "prompt": prompt, "model_version": quality}
	if err := applyKuaiziGPTImage2Size(payload, input.Config.Size); err != nil {
		return nil, err
	}
	if len(input.ReferenceImages) > 0 {
		images := make([]string, 0, len(input.ReferenceImages))
		for _, image := range input.ReferenceImages {
			imageURL, err := mediaReferenceURL(image)
			if err != nil || !isPublicMediaURL(imageURL) {
				return nil, errors.New("GPT Image 2 参考图必须使用公网 URL 或已上传 OSS 的资源")
			}
			images = append(images, imageURL)
		}
		payload["images"] = images
	}
	return payload, nil
}

func kuaiziGPTImage2Quality(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto", "medium":
		return "image2_medium", nil
	case "low":
		return "image2_low", nil
	case "high":
		return "image2_high", nil
	default:
		return "", fmt.Errorf("GPT Image 2 不支持画质：%s", strings.TrimSpace(value))
	}
}

func applyKuaiziGPTImage2Size(payload map[string]interface{}, value string) error {
	size := strings.TrimSpace(value)
	if size == "" || size == "auto" {
		payload["aspect_ratio"] = "1:1"
		return nil
	}
	if strings.Contains(size, ":") {
		spec, _ := kuaiziProviderModelSpec(kuaiziGPTImage2Model)
		if !containsProviderCapability(spec.Ratios, size) {
			return fmt.Errorf("GPT Image 2 不支持画面比例：%s", size)
		}
		payload["aspect_ratio"] = size
		return nil
	}
	parts := strings.Split(strings.ToLower(size), "x")
	if len(parts) != 2 {
		return fmt.Errorf("GPT Image 2 图片尺寸无效：%s", size)
	}
	width, widthErr := strconv.Atoi(parts[0])
	height, heightErr := strconv.Atoi(parts[1])
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return fmt.Errorf("GPT Image 2 图片尺寸无效：%s", size)
	}
	if width%16 != 0 || height%16 != 0 {
		return errors.New("GPT Image 2 图片宽高必须是 16 的倍数")
	}
	if width > 3840 || height > 3840 {
		return errors.New("GPT Image 2 图片最长边不能超过 3840")
	}
	pixels := int64(width) * int64(height)
	if pixels < 655_360 || pixels > 8_294_400 {
		return errors.New("GPT Image 2 图片总像素必须在 655360–8294400 之间")
	}
	payload["size"] = fmt.Sprintf("%dx%d", width, height)
	return nil
}

func requestKuaiziGPTImage2JSON(ctx context.Context, config providerConfig, path string, payload interface{}, target *map[string]interface{}) error {
	apiKey := strings.TrimSpace(config.APIKey)
	if apiKey == "" {
		return errors.New("GPT Image 2 API Key 不能为空")
	}
	base, err := ValidateKuaiziBaseURL(ctx, config.BaseURL, strings.TrimSpace(os.Getenv("CANVAS_ENVIRONMENT")))
	if err != nil {
		return err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("编码 GPT Image 2 请求失败：%w", err)
	}
	request, err := http.NewRequestWithContext(withKuaiziRequest(ctx), http.MethodPost, strings.TrimRight(base.String(), "/")+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	request.Header.Set("ApiKey", apiKey)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	responseBody, mimeType, err := doBinary(request)
	if err != nil {
		var httpErr providerHTTPError
		if errors.As(err, &httpErr) {
			return kuaiziCompatibleHTTPError{statusCode: httpErr.StatusCode}
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if strings.Contains(err.Error(), "不允许重定向") {
			return errors.New("GPT Image 2 上游请求不允许重定向")
		}
		return errors.New("GPT Image 2 上游请求失败")
	}
	if !strings.Contains(mimeType, "json") && !json.Valid(responseBody) {
		return errors.New("GPT Image 2 上游返回了非 JSON 内容")
	}
	var response map[string]interface{}
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return errors.New("GPT Image 2 上游响应格式无效")
	}
	if path == kuaiziGPTImage2CreatePath {
		businessCode := int(firstInt64(response, "code"))
		if _, hasBusinessCode := response["code"]; hasBusinessCode && businessCode != http.StatusOK {
			if businessCode < 100 || businessCode > 599 {
				businessCode = http.StatusInternalServerError
			}
			return kuaiziCompatibleHTTPError{statusCode: businessCode}
		}
		responseData, ok := response["data"].(map[string]interface{})
		if !ok {
			return errors.New("GPT Image 2 创建响应缺少 data 对象")
		}
		*target = responseData
		return nil
	}
	responseData, ok := response["data"].(map[string]interface{})
	if !ok {
		return errors.New("GPT Image 2 状态响应缺少 data 对象")
	}
	*target = responseData
	return nil
}

func validateKuaiziGPTImage2ResultURL(value string, input canvasGenerationInput) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil {
		return errors.New("图片地址格式无效")
	}
	if parsed.Scheme != "https" && !(strings.TrimSpace(os.Getenv("CANVAS_ENVIRONMENT")) == "development" && parsed.Scheme == "http") {
		return errors.New("图片地址必须使用 HTTPS")
	}
	for _, secret := range []string{strings.TrimSpace(input.Config.APIKey), strings.TrimSpace(input.Prompt)} {
		if secret != "" && strings.Contains(value, secret) {
			return errors.New("图片地址包含敏感请求内容")
		}
	}
	return nil
}
