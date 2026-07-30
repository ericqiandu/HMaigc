package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	aiOpenPlatformVolcengineCreatePath = "/ai-open-platform-api/api/v3/contents/generations/tasks"
	aiOpenPlatformVolcenginePollPath   = "/ai-open-platform-api/api/v3/contents/generations/tasks/"
	aiOpenPlatformVolcenginePoll       = 5 * time.Second
)

func runAIOpenPlatformVolcengineVideoTask(ctx context.Context, input canvasGenerationInput) (map[string]interface{}, error) {
	taskID := resumedProviderRequestID(ctx)
	if taskID == "" {
		modelMode, err := aiOpenPlatformVideoModelMode(input.Config.Model)
		if err != nil {
			return nil, err
		}
		ratio, err := aiOpenPlatformVideoRatio(input.Config.Size)
		if err != nil {
			return nil, err
		}
		resolution, err := aiOpenPlatformVideoResolution(input.Config.VQuality, modelMode)
		if err != nil {
			return nil, err
		}
		duration, err := aiOpenPlatformVideoDuration(input.Config.VideoSeconds)
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
			ctx,
			input.Config,
			http.MethodPost,
			aiOpenPlatformVolcengineCreatePath,
			request,
			&created,
		); err != nil {
			return nil, err
		}
		if data, ok := created["data"].(map[string]interface{}); ok {
			created = data
		}
		taskID = strings.TrimSpace(stringField(created, "id"))
	}
	if taskID == "" {
		return nil, errors.New("AI 开放平台火山兼容接口没有返回任务 ID")
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
		if data, ok := state["data"].(map[string]interface{}); ok {
			state = data
		}
		status := strings.ToLower(strings.TrimSpace(stringField(state, "status")))
		switch status {
		case "succeeded":
			content, _ := state["content"].(map[string]interface{})
			videoURL := firstNonEmptyString(stringField(content, "kz_video_url"), stringField(content, "video_url"))
			if !isPublicMediaURL(videoURL) {
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
			if lastFrameURL := strings.TrimSpace(stringField(content, "last_frame_url")); lastFrameURL != "" {
				if !isPublicMediaURL(lastFrameURL) {
					return nil, fmt.Errorf("AI 开放平台火山兼容任务 %s 返回了无效尾帧地址", taskID)
				}
				video["lastFrameUrl"] = lastFrameURL
			}
			return map[string]interface{}{"mode": "video", "video": video}, nil
		case "failed", "cancelled", "expired":
			message := seedanceErrorMessage(state)
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
		return fmt.Errorf("AI 开放平台火山兼容接口请求失败：%w", err)
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
