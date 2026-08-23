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
	kuaiziKlingModel      = "kling-v3-omni"
	kuaiziKlingCreatePath = "/ai-open-platform-api/v1/kling/video/task/create"
	kuaiziKlingStatusPath = "/ai-open-platform-api/v1/kling/video/task/status"
	kuaiziKlingPoll       = 5 * time.Second
)

type kuaiziKlingGatewayResponse struct {
	Code    int                    `json:"code"`
	Message string                 `json:"message"`
	Data    map[string]interface{} `json:"data"`
	TraceID string                 `json:"trace_id"`
}

type kuaiziKlingRequestError struct {
	statusCode int
	code       int
	message    string
	traceID    string
}

func (failure kuaiziKlingRequestError) Error() string {
	details := strings.TrimSpace(failure.message)
	if details == "" {
		details = "请求失败"
	}
	parts := []string{fmt.Sprintf("Kling 上游请求失败（HTTP %d / code %d）：%s", failure.statusCode, failure.code, details)}
	if failure.traceID != "" {
		parts = append(parts, "trace_id="+failure.traceID)
	}
	return strings.Join(parts, "，")
}

func (failure kuaiziKlingRequestError) Unwrap() error {
	return kuaiziCompatibleHTTPError{statusCode: failure.statusCode, code: strconv.Itoa(failure.code), message: failure.message}
}

func runKuaiziKlingTask(ctx context.Context, input canvasGenerationInput) (map[string]interface{}, error) {
	return runKuaiziKlingTaskWithPollInterval(ctx, input, kuaiziKlingPoll)
}

func runKuaiziKlingTaskWithPollInterval(ctx context.Context, input canvasGenerationInput, pollInterval time.Duration) (map[string]interface{}, error) {
	taskID := resumedProviderRequestID(ctx)
	createdThisAttempt := taskID == ""
	if taskID == "" {
		payload, err := kuaiziKlingCreatePayload(input)
		if err != nil {
			return nil, err
		}
		created, _, err := requestKuaiziKlingJSON(withProviderAsyncCreate(withProviderRequestKind(ctx, "create")), input.Config, kuaiziKlingCreatePath, payload)
		if err != nil {
			if compatibleCreateDefinitelyRejected(err) {
				return nil, err
			}
			return nil, &KuaiziCompatibleCreateError{err: err}
		}
		taskID = strings.TrimSpace(stringField(created, "task_id"))
	}
	if !validKuaiziCompatibleTaskID(taskID, input.Config.APIKey) {
		err := errors.New("Kling 接口没有返回有效任务 ID")
		if createdThisAttempt {
			return nil, &KuaiziCompatibleCreateError{err: err}
		}
		return nil, err
	}

	var lastTransientPollError error
	for deadline := providerPollingDeadline(ctx); time.Now().Before(deadline); {
		state, traceID, err := requestKuaiziKlingJSON(withProviderRequestKind(ctx, "poll"), input.Config, kuaiziKlingStatusPath, map[string]string{"task_id": taskID})
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
			return nil, fmt.Errorf("Kling 查询返回了不匹配的任务 ID（task_id=%s，trace_id=%s）", taskID, traceID)
		}
		switch strings.ToLower(strings.TrimSpace(stringField(state, "status"))) {
		case "succeeded":
			videoURL := strings.TrimSpace(stringField(state, "video_url"))
			if err := validateKuaiziKlingResultURL(videoURL, input); err != nil {
				return nil, fmt.Errorf("Kling 任务 %s 已成功但视频地址无效（trace_id=%s）：%w", taskID, traceID, err)
			}
			data, mimeType, err := getExternalBinary(withKuaiziRequest(withProviderRequestKind(ctx, "download")), videoURL)
			if err != nil {
				return nil, fmt.Errorf("Kling 视频下载失败（task_id=%s，trace_id=%s）：%w", taskID, traceID, err)
			}
			mimeType = normalizedMediaMimeType(mimeType, data)
			if !strings.HasPrefix(mimeType, "video/") {
				return nil, fmt.Errorf("Kling 任务 %s 返回的内容不是视频（trace_id=%s）", taskID, traceID)
			}
			video := map[string]interface{}{
				"dataUrl": dataURL(mimeType, data), "mimeType": mimeType, "taskId": taskID,
				"sourceUrl": videoURL, "traceId": traceID,
			}
			if duration := firstInt64(state, "duration"); duration > 0 {
				video["durationMs"] = duration * 1000
			}
			return map[string]interface{}{"mode": "video", "video": video}, nil
		case "failed":
			failure := strings.TrimSpace(stringField(state, "error"))
			if failure == "" {
				failure = "上游未返回失败原因"
			}
			return nil, fmt.Errorf("Kling 生成失败（task_id=%s，trace_id=%s）：%s", taskID, traceID, failure)
		case "running":
			if err := sleepContext(ctx, pollInterval); err != nil {
				return nil, err
			}
		case "":
			return nil, fmt.Errorf("Kling 任务 %s 未返回状态（trace_id=%s）", taskID, traceID)
		default:
			return nil, fmt.Errorf("Kling 任务 %s 返回未知状态（trace_id=%s）", taskID, traceID)
		}
	}
	if lastTransientPollError != nil {
		return nil, fmt.Errorf("Kling 生成超时（task_id=%s，最后一次状态查询失败：%w）", taskID, lastTransientPollError)
	}
	return nil, fmt.Errorf("Kling 生成超时（task_id=%s）", taskID)
}

func kuaiziKlingCreatePayload(input canvasGenerationInput) (map[string]interface{}, error) {
	if strings.TrimSpace(input.Config.Model) != kuaiziKlingModel {
		return nil, fmt.Errorf("筷子 Kling 接口仅支持模型 %s", kuaiziKlingModel)
	}
	prompt := strings.TrimSpace(input.Prompt)
	if len([]rune(prompt)) > 2500 {
		return nil, errors.New("Kling 提示词不能超过 2500 个字符")
	}
	if input.Mask != nil || len(input.ReferenceAudios) > 0 {
		return nil, errors.New("Kling 视频接口不支持蒙版或音频参考素材")
	}
	if prompt == "" && len(input.ReferenceImages) == 0 && len(input.ReferenceVideos) == 0 {
		return nil, errors.New("Kling 的 prompt、images、videos 至少提供一项")
	}
	if count := strings.TrimSpace(input.Config.Count); count != "" && count != "1" {
		return nil, errors.New("Kling 每个任务只支持生成 1 个视频")
	}
	duration, err := strconv.Atoi(strings.TrimSpace(input.Config.VideoSeconds))
	if err != nil || duration < 3 || duration > 15 {
		return nil, errors.New("Kling 时长必须为 3–15 秒的整数")
	}
	mode := strings.ToLower(strings.TrimSpace(input.Config.VQuality))
	if mode != "std" && mode != "pro" && mode != "4k" {
		return nil, fmt.Errorf("Kling 模式仅支持 std、pro 或 4k，当前为 %s", strings.TrimSpace(input.Config.VQuality))
	}
	ratio := strings.TrimSpace(input.Config.Size)
	if ratio != "" && ratio != "16:9" && ratio != "9:16" && ratio != "1:1" {
		return nil, fmt.Errorf("Kling 不支持画面比例：%s", ratio)
	}

	images, firstFrame, endFrame, err := kuaiziKlingImages(input)
	if err != nil {
		return nil, err
	}
	videos, baseVideo, err := kuaiziKlingVideos(input)
	if err != nil {
		return nil, err
	}
	if len(videos) > 0 && duration > 10 {
		return nil, errors.New("Kling 参考视频任务时长必须为 3–10 秒")
	}
	generateAudio := parseBool(input.Config.VideoGenerateAudio, false)
	if spec, ok := kuaiziProviderModelSpec(kuaiziKlingModel); !ok {
		return nil, errors.New("Kling 模型能力契约不存在")
	} else if generateAudio && !providerGeneratedAudioSupported(spec.SupportsGeneratedAudio, spec.GeneratedAudioResolutions, mode) {
		return nil, fmt.Errorf("Kling %s 不支持同步音频", mode)
	}
	if len(videos) > 0 && generateAudio {
		return nil, errors.New("Kling 携带参考视频时不能生成同步音频")
	}
	if len(videos) > 0 && mode == "4k" {
		return nil, errors.New("Kling 携带参考视频时不支持 4k")
	}
	if baseVideo && (firstFrame || endFrame) {
		return nil, errors.New("Kling 视频编辑不能同时设置首帧或尾帧")
	}
	imageLimit := 7
	if len(videos) > 0 {
		imageLimit = 4
	}
	if len(images) > imageLimit {
		return nil, fmt.Errorf("Kling 当前输入最多支持 %d 张图片", imageLimit)
	}
	if endFrame && !firstFrame {
		return nil, errors.New("Kling 尾帧必须同时提供首帧")
	}
	if endFrame && len(images) > 2 {
		return nil, errors.New("Kling 图片超过 2 张时不支持设置尾帧")
	}
	if ratio == "" && !firstFrame && !baseVideo {
		return nil, errors.New("Kling 文生视频或参考图任务必须设置画面比例")
	}

	payload := map[string]interface{}{
		"model": kuaiziKlingModel, "duration": duration, "generate_audio": generateAudio, "kling_mode": mode,
	}
	if prompt != "" {
		payload["prompt"] = prompt
	}
	if ratio != "" {
		payload["aspect_ratio"] = ratio
	}
	if len(images) > 0 {
		payload["images"] = images
	}
	if len(videos) > 0 {
		payload["videos"] = videos
	}
	return payload, nil
}

func kuaiziKlingImages(input canvasGenerationInput) ([]map[string]string, bool, bool, error) {
	result := make([]map[string]string, 0, len(input.ReferenceImages))
	firstFrame := false
	endFrame := false
	for _, image := range input.ReferenceImages {
		mediaURL, err := klingPublicMediaURL(image)
		if err != nil {
			return nil, false, false, err
		}
		role := ""
		switch seedanceImageRole(input, image) {
		case "first_frame":
			role = "first_frame"
			firstFrame = true
		case "last_frame":
			role = "end_frame"
			endFrame = true
		}
		result = append(result, map[string]string{"url": mediaURL, "role": role})
	}
	return result, firstFrame, endFrame, nil
}

func kuaiziKlingVideos(input canvasGenerationInput) ([]map[string]string, bool, error) {
	if len(input.ReferenceVideos) > 1 {
		return nil, false, errors.New("Kling 最多支持 1 个参考视频")
	}
	if len(input.ReferenceVideos) == 0 {
		return nil, false, nil
	}
	video := input.ReferenceVideos[0]
	if video.DurationMs < 3_000 || video.DurationMs > 10_000 {
		return nil, false, errors.New("Kling 参考视频时长必须为 3–10 秒且必须可验证")
	}
	mediaURL, err := klingPublicMediaURL(video)
	if err != nil {
		return nil, false, err
	}
	referType := "base"
	if metadataString(input.Metadata, "videoGenerationMode") == "omni_reference" {
		referType = "feature"
	}
	return []map[string]string{{"url": mediaURL, "refer_type": referType}}, referType == "base", nil
}

func requestKuaiziKlingJSON(ctx context.Context, config providerConfig, path string, payload interface{}) (map[string]interface{}, string, error) {
	apiKey := strings.TrimSpace(config.APIKey)
	if apiKey == "" {
		return nil, "", errors.New("Kling API Key 不能为空")
	}
	base, err := ValidateKuaiziBaseURL(ctx, config.BaseURL, strings.TrimSpace(os.Getenv("CANVAS_ENVIRONMENT")))
	if err != nil {
		return nil, "", err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, "", fmt.Errorf("编码 Kling 请求失败：%w", err)
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
			response := kuaiziKlingGatewayResponse{}
			if json.Unmarshal([]byte(httpErr.Body), &response) == nil {
				return nil, strings.TrimSpace(response.TraceID), kuaiziKlingRequestError{statusCode: httpErr.StatusCode, code: response.Code, message: response.Message, traceID: strings.TrimSpace(response.TraceID)}
			}
			return nil, "", kuaiziKlingRequestError{statusCode: httpErr.StatusCode, code: httpErr.StatusCode}
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, "", err
		}
		if strings.Contains(err.Error(), "不允许重定向") {
			return nil, "", errors.New("Kling 上游请求不允许重定向")
		}
		return nil, "", fmt.Errorf("Kling 上游请求失败：%w", err)
	}
	if !strings.Contains(mimeType, "json") && !json.Valid(responseBody) {
		return nil, "", errors.New("Kling 上游返回了非 JSON 内容")
	}
	var response kuaiziKlingGatewayResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, "", errors.New("Kling 上游响应格式无效")
	}
	traceID := strings.TrimSpace(response.TraceID)
	if response.Code != 0 {
		statusCode := response.Code
		if statusCode < 100 || statusCode > 599 {
			statusCode = http.StatusInternalServerError
		}
		return nil, traceID, kuaiziKlingRequestError{statusCode: statusCode, code: response.Code, message: response.Message, traceID: traceID}
	}
	if response.Data == nil {
		return nil, traceID, fmt.Errorf("Kling 上游响应缺少 data 对象（trace_id=%s）", traceID)
	}
	return response.Data, traceID, nil
}

func validateKuaiziKlingResultURL(value string, input canvasGenerationInput) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil {
		return errors.New("视频地址格式无效")
	}
	if parsed.Scheme != "https" && !(strings.TrimSpace(os.Getenv("CANVAS_ENVIRONMENT")) == "development" && parsed.Scheme == "http") {
		return errors.New("视频地址必须使用 HTTPS")
	}
	for _, secret := range []string{strings.TrimSpace(input.Config.APIKey), strings.TrimSpace(input.Prompt)} {
		if secret != "" && strings.Contains(value, secret) {
			return errors.New("视频地址包含敏感请求内容")
		}
	}
	return nil
}
