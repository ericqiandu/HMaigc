package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type KuaiziCompatibleCreateError struct{ err error }

func (failure *KuaiziCompatibleCreateError) Error() string { return failure.err.Error() }
func (failure *KuaiziCompatibleCreateError) Unwrap() error { return failure.err }

type kuaiziCompatibleHTTPError struct{ statusCode int }

func (failure kuaiziCompatibleHTTPError) Error() string {
	return fmt.Sprintf("AI 开放平台火山兼容接口返回 HTTP %d", failure.statusCode)
}

const (
	aiOpenPlatformVolcengineCreatePath = "/ai-open-platform-api/api/v3/contents/generations/tasks"
	aiOpenPlatformVolcenginePollPath   = "/ai-open-platform-api/api/v3/contents/generations/tasks/"
	aiOpenPlatformVolcenginePoll       = 5 * time.Second
)

func runAIOpenPlatformVolcengineVideoTask(ctx context.Context, input canvasGenerationInput) (map[string]interface{}, error) {
	taskID := resumedProviderRequestID(ctx)
	createdThisAttempt := taskID == ""
	if taskID == "" {
		spec, ok := kuaiziSeedanceModelSpec(input.Config.Model)
		if !ok {
			return nil, fmt.Errorf("筷子兼容视频接口不支持模型：%s", strings.TrimSpace(input.Config.Model))
		}
		ratio, resolution, duration, err := validateKuaiziCompatibleVideoInput(input, spec)
		if err != nil {
			return nil, err
		}
		content, err := seedanceContent(input)
		if err != nil {
			return nil, err
		}
		request := map[string]interface{}{
			"model":             input.Config.Model,
			"content":           content,
			"ratio":             ratio,
			"resolution":        resolution,
			"duration":          duration,
			"generate_audio":    parseBool(input.Config.VideoGenerateAudio, true),
			"watermark":         parseBool(input.Config.VideoWatermark, false),
			"return_last_frame": true,
		}
		var created map[string]interface{}
		if err := requestAIOpenPlatformVolcengineJSON(
			withProviderRequestKind(ctx, "create"),
			input.Config,
			http.MethodPost,
			aiOpenPlatformVolcengineCreatePath,
			request,
			&created,
		); err != nil {
			if compatibleCreateDefinitelyRejected(err) {
				return nil, err
			}
			return nil, &KuaiziCompatibleCreateError{err: err}
		}
		taskID = strings.TrimSpace(stringField(created, "id"))
	}
	if !validKuaiziCompatibleTaskID(taskID, input.Config.APIKey) {
		err := errors.New("AI 开放平台火山兼容接口没有返回有效任务 ID")
		if createdThisAttempt {
			return nil, &KuaiziCompatibleCreateError{err: err}
		}
		return nil, err
	}

	for deadline := providerPollingDeadline(ctx); time.Now().Before(deadline); {
		var state map[string]interface{}
		if err := requestAIOpenPlatformVolcengineJSON(
			withProviderRequestKind(ctx, "poll"),
			input.Config,
			http.MethodGet,
			aiOpenPlatformVolcenginePollPath+taskID,
			nil,
			&state,
		); err != nil {
			return nil, err
		}
		status := strings.ToLower(strings.TrimSpace(stringField(state, "status")))
		if returnedID := strings.TrimSpace(stringField(state, "id")); returnedID != "" && returnedID != taskID {
			return nil, errors.New("AI 开放平台火山兼容查询返回了不匹配的任务 ID")
		}
		switch status {
		case "succeeded":
			content, _ := state["content"].(map[string]interface{})
			videoURL := firstNonEmptyString(stringField(content, "kz_video_url"), stringField(content, "video_url"))
			if !isPublicMediaURL(videoURL) || containsKuaiziCompatibleSecret(videoURL, input.Config.APIKey) {
				return nil, fmt.Errorf("AI 开放平台火山兼容任务 %s 已成功但没有返回有效视频地址", taskID)
			}
			data, mimeType, err := getExternalBinary(withProviderRequestKind(ctx, "download"), videoURL)
			if err != nil {
				return nil, fmt.Errorf("AI 开放平台火山兼容视频下载失败（任务 %s）：%w", taskID, err)
			}
			mimeType = normalizedMediaMimeType(mimeType, data)
			video := map[string]interface{}{
				"dataUrl":   dataURL(mimeType, data),
				"mimeType":  mimeType,
				"taskId":    taskID,
				"sourceUrl": videoURL,
			}
			if duration := firstInt64(state, "duration"); duration > 0 {
				video["durationMs"] = duration * 1000
			}
			if lastFrameURL := strings.TrimSpace(stringField(content, "last_frame_url")); lastFrameURL != "" {
				if !isPublicMediaURL(lastFrameURL) || containsKuaiziCompatibleSecret(lastFrameURL, input.Config.APIKey) {
					return nil, fmt.Errorf("AI 开放平台火山兼容任务 %s 返回了无效尾帧地址", taskID)
				}
				video["lastFrameUrl"] = lastFrameURL
			}
			return map[string]interface{}{"mode": "video", "video": video}, nil
		case "failed", "cancelled", "expired":
			message := strings.ReplaceAll(seedanceErrorMessage(state), strings.TrimSpace(input.Config.APIKey), "[REDACTED]")
			if message == "" {
				message = "上游未返回失败原因"
			}
			return nil, fmt.Errorf("AI 开放平台火山兼容视频生成失败（任务 %s）：%s", taskID, message)
		case "queued", "running":
			// 协议明确声明的非终态，继续轮询。
		default:
			if status == "" {
				return nil, fmt.Errorf("AI 开放平台火山兼容任务 %s 未返回状态", taskID)
			}
			return nil, fmt.Errorf("AI 开放平台火山兼容任务 %s 返回未知状态：%s", taskID, status)
		}
		if err := sleepContext(ctx, aiOpenPlatformVolcenginePoll); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("AI 开放平台火山兼容视频生成超时（任务 %s）", taskID)
}

func compatibleCreateDefinitelyRejected(err error) bool {
	var httpErr kuaiziCompatibleHTTPError
	if !errors.As(err, &httpErr) {
		return false
	}
	switch httpErr.statusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity, http.StatusTooManyRequests:
		return true
	default:
		return false
	}
}

func validKuaiziCompatibleTaskID(taskID string, apiKey string) bool {
	return strings.HasPrefix(taskID, "kz-cgt-") && len(taskID) <= 160 && !containsKuaiziCompatibleSecret(taskID, apiKey)
}

func containsKuaiziCompatibleSecret(value string, apiKey string) bool {
	secret := strings.TrimSpace(apiKey)
	return secret != "" && strings.Contains(value, secret)
}

func validateKuaiziCompatibleVideoInput(input canvasGenerationInput, spec ProviderModelSpec) (string, string, int, error) {
	ratio, err := kuaiziCompatibleVideoRatio(input.Config.Size)
	if err != nil {
		return "", "", 0, err
	}
	resolution := normalizeVideoResolution(strings.ToLower(strings.TrimSpace(input.Config.VQuality)))
	if !containsProviderCapability(spec.Resolutions, resolution) {
		return "", "", 0, fmt.Errorf("%s 不支持分辨率 %s", spec.DisplayName, resolution)
	}
	duration, err := strconv.Atoi(strings.TrimSpace(input.Config.VideoSeconds))
	if err != nil || (duration != -1 && (duration < spec.DurationMin || duration > spec.DurationMax)) {
		return "", "", 0, fmt.Errorf("%s 时长必须为 %d–%d 秒或 -1", spec.DisplayName, spec.DurationMin, spec.DurationMax)
	}
	if len(input.ReferenceImages) > spec.MaxImages || len(input.ReferenceVideos) > spec.MaxVideos || len(input.ReferenceAudios) > spec.MaxAudios {
		return "", "", 0, fmt.Errorf("%s 参考素材上限为 %d 图 / %d 视频 / %d 音频", spec.DisplayName, spec.MaxImages, spec.MaxVideos, spec.MaxAudios)
	}
	if spec.ModelKey != "doubao-seedance-2-5-260628" && len(input.ReferenceAudios) > 0 && len(input.ReferenceImages)+len(input.ReferenceVideos) == 0 {
		return "", "", 0, errors.New("Seedance 2.0 参考音频必须同时连接参考图片或参考视频")
	}
	if spec.ModelKey == "doubao-seedance-2-5-260628" && len(input.ReferenceAudios) > 0 && len(input.ReferenceImages)+len(input.ReferenceVideos) == 0 && seedancePromptText(input) == "" {
		return "", "", 0, errors.New("Seedance 2.5 纯音频参考必须同时提供提示词")
	}
	if spec.ModelKey == "doubao-seedance-2-5-260628" && ratio != "adaptive" {
		for _, image := range input.ReferenceImages {
			role := seedanceImageRole(input, image)
			if role == "first_frame" || role == "last_frame" {
				return "", "", 0, errors.New("Seedance 2.5 首帧或首尾帧任务只支持自适应比例")
			}
		}
	}
	return ratio, resolution, duration, nil
}

func kuaiziCompatibleVideoRatio(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "", "auto", "adaptive":
		return "adaptive", nil
	case "16:9", "4:3", "1:1", "3:4", "9:16", "21:9":
		return strings.TrimSpace(value), nil
	case "1280x720", "1920x1080":
		return "16:9", nil
	case "720x1280", "1080x1920":
		return "9:16", nil
	default:
		return "", fmt.Errorf("筷子兼容视频接口不支持画面比例：%s", strings.TrimSpace(value))
	}
}

func containsProviderCapability(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func requestAIOpenPlatformVolcengineJSON(
	ctx context.Context,
	config providerConfig,
	method string,
	path string,
	body interface{},
	target interface{},
) error {
	apiKey := strings.TrimSpace(config.APIKey)
	if apiKey == "" {
		return errors.New("AI 开放平台火山兼容 API Key 不能为空")
	}
	endpoint, err := aiOpenPlatformVolcengineURL(config.BaseURL, path)
	if err != nil {
		return err
	}
	var requestBody *bytes.Reader
	if body == nil {
		requestBody = bytes.NewReader(nil)
	} else {
		data, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return fmt.Errorf("编码 AI 开放平台火山兼容请求失败：%w", marshalErr)
		}
		requestBody = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, requestBody)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if err := doJSON(request, target); err != nil {
		var httpErr providerHTTPError
		if errors.As(err, &httpErr) {
			return kuaiziCompatibleHTTPError{statusCode: httpErr.StatusCode}
		}
		message := strings.ReplaceAll(err.Error(), apiKey, "[REDACTED]")
		return fmt.Errorf("AI 开放平台火山兼容接口请求失败：%s", message)
	}
	return nil
}

func aiOpenPlatformVolcengineURL(baseURL string, path string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return "", errors.New("AI 开放平台火山兼容 Base URL 不能为空")
	}
	if strings.TrimSpace(path) == "" {
		return "", errors.New("AI 开放平台火山兼容接口路径不能为空")
	}
	return base + "/" + strings.TrimLeft(path, "/"), nil
}
