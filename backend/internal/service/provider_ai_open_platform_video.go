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

const (
	aiOpenPlatformVideoCreatePath = "/ai-open-platform-api/v1/lz/video/task/create"
	aiOpenPlatformVideoStatusPath = "/ai-open-platform-api/v1/lz/video/task/status"
	aiOpenPlatformVideoPoll       = 5 * time.Second
)

type aiOpenPlatformVideoMedia struct {
	URL  string `json:"url"`
	Role string `json:"role"`
}

type aiOpenPlatformVideoSuperResolution struct {
	Resolution  string `json:"resolution"`
	Scene       string `json:"scene"`
	ToolVersion string `json:"tool_version"`
	FPS         int    `json:"fps,omitempty"`
}

type aiOpenPlatformVideoCreateRequest struct {
	Prompt                string                              `json:"prompt,omitempty"`
	Mode                  string                              `json:"mode"`
	Images                []aiOpenPlatformVideoMedia          `json:"images,omitempty"`
	Videos                []aiOpenPlatformVideoMedia          `json:"videos,omitempty"`
	Audios                []aiOpenPlatformVideoMedia          `json:"audios,omitempty"`
	Resolution            string                              `json:"resolution"`
	Ratio                 string                              `json:"ratio"`
	Duration              int                                 `json:"duration"`
	GenerateAudio         bool                                `json:"generate_audio"`
	Watermark             bool                                `json:"watermark"`
	ReturnLastFrame       bool                                `json:"return_last_frame"`
	SuperResolutionConfig *aiOpenPlatformVideoSuperResolution `json:"super_resolution_config,omitempty"`
}

type aiOpenPlatformVideoStatusRequest struct {
	TaskID string `json:"task_id"`
}

type aiOpenPlatformVideoEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
	TraceID string          `json:"trace_id"`
}

type aiOpenPlatformVideoCreated struct {
	TaskID string `json:"task_id"`
}

type aiOpenPlatformVideoStatus struct {
	TaskID       string `json:"task_id"`
	Status       string `json:"status"`
	VideoURL     string `json:"video_url"`
	LastFrameURL string `json:"last_frame_url"`
	Error        string `json:"error"`
}

func runAIOpenPlatformVideoTask(ctx context.Context, input canvasGenerationInput) (map[string]interface{}, error) {
	taskID := resumedProviderRequestID(ctx)
	if taskID == "" {
		body, err := aiOpenPlatformVideoBody(input)
		if err != nil {
			return nil, err
		}
		var created aiOpenPlatformVideoCreated
		if err := requestAIOpenPlatformVideoJSON(ctx, input.Config, aiOpenPlatformVideoCreatePath, body, &created); err != nil {
			return nil, err
		}
		taskID = strings.TrimSpace(created.TaskID)
	}
	if taskID == "" {
		return nil, errors.New("AI 开放平台视频接口没有返回任务 ID")
	}

	for deadline := providerPollingDeadline(ctx); time.Now().Before(deadline); {
		var state aiOpenPlatformVideoStatus
		if err := requestAIOpenPlatformVideoJSON(
			withProviderRequestKind(ctx, "poll"),
			input.Config,
			aiOpenPlatformVideoStatusPath,
			aiOpenPlatformVideoStatusRequest{TaskID: taskID},
			&state,
		); err != nil {
			return nil, err
		}
		switch strings.ToLower(strings.TrimSpace(state.Status)) {
		case "succeeded":
			videoURL := strings.TrimSpace(state.VideoURL)
			if !isPublicMediaURL(videoURL) {
				return nil, fmt.Errorf("AI 开放平台视频任务 %s 已成功但没有返回有效视频地址", taskID)
			}
			data, mimeType, err := getExternalBinary(withProviderRequestKind(ctx, "download"), videoURL)
			if err != nil {
				return nil, fmt.Errorf("AI 开放平台视频结果下载失败（任务 %s）：%w", taskID, err)
			}
			mimeType = normalizedMediaMimeType(mimeType, data)
			video := map[string]interface{}{
				"dataUrl":   dataURL(mimeType, data),
				"mimeType":  mimeType,
				"taskId":    taskID,
				"sourceUrl": videoURL,
			}
			if lastFrameURL := strings.TrimSpace(state.LastFrameURL); lastFrameURL != "" {
				if !isPublicMediaURL(lastFrameURL) {
					return nil, fmt.Errorf("AI 开放平台视频任务 %s 返回了无效尾帧地址", taskID)
				}
				video["lastFrameUrl"] = lastFrameURL
			}
			return map[string]interface{}{"mode": "video", "video": video}, nil
		case "failed":
			return nil, aiOpenPlatformVideoTaskError(taskID, state)
		case "pending", "submitted", "running":
		default:
			return nil, fmt.Errorf("AI 开放平台视频任务 %s 返回未知状态：%s", taskID, state.Status)
		}
		if err := sleepContext(ctx, aiOpenPlatformVideoPoll); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("AI 开放平台视频生成超时（任务 %s）", taskID)
}

func aiOpenPlatformVideoBody(input canvasGenerationInput) (aiOpenPlatformVideoCreateRequest, error) {
	modelMode, err := aiOpenPlatformVideoModelMode(input.Config.Model)
	if err != nil {
		return aiOpenPlatformVideoCreateRequest{}, err
	}
	duration, err := aiOpenPlatformVideoDuration(input.Config.VideoSeconds)
	if err != nil {
		return aiOpenPlatformVideoCreateRequest{}, err
	}
	resolution, err := aiOpenPlatformVideoResolution(input.Config.VQuality, modelMode)
	if err != nil {
		return aiOpenPlatformVideoCreateRequest{}, err
	}
	ratio, err := aiOpenPlatformVideoRatio(input.Config.Size)
	if err != nil {
		return aiOpenPlatformVideoCreateRequest{}, err
	}
	images, videos, audios, err := aiOpenPlatformVideoMediaLists(input)
	if err != nil {
		return aiOpenPlatformVideoCreateRequest{}, err
	}
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" && len(images) == 0 && len(videos) == 0 {
		return aiOpenPlatformVideoCreateRequest{}, errors.New("AI 开放平台视频生成至少需要提示词、参考图片或参考视频")
	}
	superResolution, err := aiOpenPlatformVideoSuperResolutionConfig(input.Config, resolution)
	if err != nil {
		return aiOpenPlatformVideoCreateRequest{}, err
	}
	return aiOpenPlatformVideoCreateRequest{
		Prompt:                prompt,
		Mode:                  modelMode,
		Images:                images,
		Videos:                videos,
		Audios:                audios,
		Resolution:            resolution,
		Ratio:                 ratio,
		Duration:              duration,
		GenerateAudio:         parseBool(input.Config.VideoGenerateAudio, true),
		Watermark:             parseBool(input.Config.VideoWatermark, false),
		ReturnLastFrame:       true,
		SuperResolutionConfig: superResolution,
	}, nil
}

func aiOpenPlatformVideoModelMode(modelName string) (string, error) {
	// 该旧渠道仅拥有下列 Seedance 2.0 模型；2.5 必须经冻结凭据运行时适配器执行。
	switch strings.TrimSpace(modelName) {
	case "doubao-seedance-2-0-fast-260128":
		return "fast", nil
	case "doubao-seedance-2-0-260128":
		return "pro", nil
	case "doubao-seedance-2-0-mini-260615":
		return "mini", nil
	default:
		return "", fmt.Errorf("AI 开放平台视频渠道不支持模型：%s", strings.TrimSpace(modelName))
	}
}

func aiOpenPlatformVideoDuration(value string) (int, error) {
	duration, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || (duration != -1 && (duration < 4 || duration > 15)) {
		return 0, errors.New("AI 开放平台视频时长必须为 4–15 秒或 -1")
	}
	return duration, nil
}

func aiOpenPlatformVideoResolution(value string, modelMode string) (string, error) {
	rawResolution := strings.ToLower(strings.TrimSpace(value))
	resolution := normalizeVideoResolution(rawResolution)
	if rawResolution == "4k" {
		resolution = "4k"
	}
	switch resolution {
	case "480p", "720p", "1080p", "4k":
	default:
		return "", fmt.Errorf("AI 开放平台视频不支持分辨率：%s", resolution)
	}
	if (resolution == "1080p" || resolution == "4k") && modelMode != "pro" {
		return "", fmt.Errorf("%s 模型不支持 %s", modelMode, resolution)
	}
	return resolution, nil
}

func aiOpenPlatformVideoRatio(value string) (string, error) {
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
		return "", fmt.Errorf("AI 开放平台视频不支持画面比例：%s", strings.TrimSpace(value))
	}
}

func aiOpenPlatformVideoMediaLists(input canvasGenerationInput) ([]aiOpenPlatformVideoMedia, []aiOpenPlatformVideoMedia, []aiOpenPlatformVideoMedia, error) {
	if len(input.ReferenceImages) > 9 {
		return nil, nil, nil, errors.New("AI 开放平台视频最多支持 9 张参考图片")
	}
	if len(input.ReferenceVideos) > 3 {
		return nil, nil, nil, errors.New("AI 开放平台视频最多支持 3 个参考视频")
	}
	if len(input.ReferenceAudios) > 3 {
		return nil, nil, nil, errors.New("AI 开放平台视频最多支持 3 个参考音频")
	}
	images := make([]aiOpenPlatformVideoMedia, 0, len(input.ReferenceImages))
	for _, media := range input.ReferenceImages {
		mediaURL, err := aiOpenPlatformVideoMediaURL(media)
		if err != nil {
			return nil, nil, nil, err
		}
		images = append(images, aiOpenPlatformVideoMedia{URL: mediaURL, Role: seedanceImageRole(input, media)})
	}
	videos := make([]aiOpenPlatformVideoMedia, 0, len(input.ReferenceVideos))
	for _, media := range input.ReferenceVideos {
		mediaURL, err := aiOpenPlatformVideoMediaURL(media)
		if err != nil {
			return nil, nil, nil, err
		}
		videos = append(videos, aiOpenPlatformVideoMedia{URL: mediaURL, Role: "reference_video"})
	}
	audios := make([]aiOpenPlatformVideoMedia, 0, len(input.ReferenceAudios))
	for _, media := range input.ReferenceAudios {
		mediaURL, err := aiOpenPlatformVideoMediaURL(media)
		if err != nil {
			return nil, nil, nil, err
		}
		audios = append(audios, aiOpenPlatformVideoMedia{URL: mediaURL, Role: "reference_audio"})
	}
	return images, videos, audios, nil
}

func aiOpenPlatformVideoMediaURL(media providerMedia) (string, error) {
	for _, value := range []string{strings.TrimSpace(media.URL), strings.TrimSpace(media.DataURL)} {
		if isPublicMediaURL(value) || strings.HasPrefix(value, "asset://") {
			return value, nil
		}
	}
	return "", errors.New("AI 开放平台视频参考素材必须使用公网 HTTP(S) 地址或 asset:// 素材地址")
}

func aiOpenPlatformVideoSuperResolutionConfig(config providerConfig, sourceResolution string) (*aiOpenPlatformVideoSuperResolution, error) {
	if !parseBool(config.VideoSuperResolutionEnabled, false) {
		return nil, nil
	}
	target := strings.ToLower(strings.TrimSpace(config.VideoSuperResolutionResolution))
	allowedTargets := map[string]map[string]bool{
		"480p":  {"720p": true, "1080p": true, "2k": true, "4k": true},
		"720p":  {"1080p": true, "2k": true, "4k": true},
		"1080p": {"2k": true, "4k": true},
	}
	if !allowedTargets[sourceResolution][target] {
		return nil, fmt.Errorf("AI 开放平台视频不支持从 %s 超分到 %s", sourceResolution, target)
	}
	scene := strings.TrimSpace(config.VideoSuperResolutionScene)
	switch scene {
	case "aigc", "short_series", "ugc", "old_film":
	default:
		return nil, fmt.Errorf("AI 开放平台视频不支持超分场景：%s", scene)
	}
	toolVersion := strings.TrimSpace(config.VideoSuperResolutionVersion)
	switch toolVersion {
	case "standard", "professional":
	default:
		return nil, fmt.Errorf("AI 开放平台视频不支持超分版本：%s", toolVersion)
	}
	fps := 0
	if rawFPS := strings.TrimSpace(config.VideoSuperResolutionFPS); rawFPS != "" {
		parsedFPS, err := strconv.Atoi(rawFPS)
		if err != nil || parsedFPS < 1 || parsedFPS > 120 {
			return nil, errors.New("AI 开放平台视频超分 FPS 必须为 1–120")
		}
		fps = parsedFPS
	}
	return &aiOpenPlatformVideoSuperResolution{
		Resolution:  target,
		Scene:       scene,
		ToolVersion: toolVersion,
		FPS:         fps,
	}, nil
}

func requestAIOpenPlatformVideoJSON(
	ctx context.Context,
	config providerConfig,
	path string,
	body interface{},
	target interface{},
) error {
	apiKey := strings.TrimSpace(config.APIKey)
	if apiKey == "" {
		return errors.New("AI 开放平台视频 API Key 不能为空")
	}
	endpoint, err := aiOpenPlatformVideoURL(config.BaseURL, path)
	if err != nil {
		return err
	}
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("编码 AI 开放平台视频请求失败：%w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("ApiKey", apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	responseBody, mimeType, err := doBinary(req)
	if err != nil {
		return aiOpenPlatformVideoRequestError(err)
	}
	if !strings.Contains(strings.ToLower(mimeType), "json") && !json.Valid(responseBody) {
		return fmt.Errorf("AI 开放平台视频接口返回非 JSON 内容：%s", mimeType)
	}
	var envelope aiOpenPlatformVideoEnvelope
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return fmt.Errorf("解析 AI 开放平台视频响应失败：%w", err)
	}
	if envelope.Code != 0 {
		return fmt.Errorf(
			"AI 开放平台视频接口错误（code=%d, trace_id=%s）：%s",
			envelope.Code,
			strings.TrimSpace(envelope.TraceID),
			strings.TrimSpace(envelope.Message),
		)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return fmt.Errorf("AI 开放平台视频接口没有返回 data（trace_id=%s）", strings.TrimSpace(envelope.TraceID))
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		return fmt.Errorf("解析 AI 开放平台视频 data 失败（trace_id=%s）：%w", strings.TrimSpace(envelope.TraceID), err)
	}
	return nil
}

func aiOpenPlatformVideoRequestError(err error) error {
	var httpErr providerHTTPError
	if !errors.As(err, &httpErr) {
		return err
	}
	var payload aiOpenPlatformVideoEnvelope
	if json.Unmarshal([]byte(httpErr.Body), &payload) != nil || strings.TrimSpace(payload.Message) == "" {
		return err
	}
	return fmt.Errorf(
		"AI 开放平台视频接口错误（HTTP %d, code=%d, trace_id=%s）：%s",
		httpErr.StatusCode,
		payload.Code,
		strings.TrimSpace(payload.TraceID),
		strings.TrimSpace(payload.Message),
	)
}

func aiOpenPlatformVideoURL(baseURL string, path string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return "", errors.New("AI 开放平台视频 Base URL 不能为空")
	}
	if strings.TrimSpace(path) == "" {
		return "", errors.New("AI 开放平台视频接口路径不能为空")
	}
	return base + "/" + strings.TrimLeft(path, "/"), nil
}

func aiOpenPlatformVideoTaskError(taskID string, state aiOpenPlatformVideoStatus) error {
	message := strings.TrimSpace(state.Error)
	if message == "" {
		return fmt.Errorf("AI 开放平台视频任务 %s 状态为 %s，但上游错误信息为空", taskID, state.Status)
	}
	return fmt.Errorf("AI 开放平台视频生成失败（任务 %s）：%s", taskID, message)
}
