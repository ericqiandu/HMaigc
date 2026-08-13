package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const miniMaxH3Model = "MiniMax-H3"

var miniMaxH3Ratios = map[string]bool{
	"adaptive": true,
	"21:9":     true,
	"16:9":     true,
	"4:3":      true,
	"1:1":      true,
	"3:4":      true,
	"9:16":     true,
}

func runMiniMaxH3VideoTask(ctx context.Context, input canvasGenerationInput) (map[string]interface{}, error) {
	body, err := miniMaxH3VideoBody(input)
	if err != nil {
		return nil, err
	}
	taskID := resumedProviderRequestID(ctx)
	if taskID == "" {
		var created map[string]interface{}
		if err := postJSON(ctx, input.Config, "/v2/video_generation", body, &created); err != nil {
			return nil, err
		}
		taskID = firstNonEmptyString(stringField(created, "task_id"), stringField(created, "taskId"))
		if taskID == "" {
			return nil, errors.New("MiniMax H3 创建任务成功但未返回 task_id")
		}
	}

	for deadline := providerPollingDeadline(ctx); time.Now().Before(deadline); {
		var state map[string]interface{}
		if err := getJSON(ctx, input.Config, "/v2/query/video_generation/"+url.PathEscape(taskID), &state); err != nil {
			return nil, err
		}
		state = miniMaxH3TaskState(state)
		status := strings.ToLower(firstNonEmptyString(stringField(state, "status"), stringField(state, "state")))
		switch status {
		case "success", "succeeded", "completed", "done":
			videoURL := miniMaxH3ResultURL(state)
			if videoURL == "" {
				return nil, fmt.Errorf("MiniMax H3 任务 %s 已成功，但响应缺少 content.url", taskID)
			}
			data, mimeType, err := getExternalBinary(withProviderRequestKind(ctx, "download"), videoURL)
			if err != nil {
				return nil, fmt.Errorf("MiniMax H3 视频结果下载失败（任务 %s）：%w", taskID, err)
			}
			mimeType = normalizedMediaMimeType(mimeType, data)
			return map[string]interface{}{
				"mode":   "video",
				"taskId": taskID,
				"video":  map[string]interface{}{"dataUrl": dataURL(mimeType, data), "mimeType": mimeType},
			}, nil
		case "fail", "failed", "cancelled", "canceled", "expired":
			return nil, fmt.Errorf("MiniMax H3 视频生成失败（任务 %s）：%s", taskID, defaultString(miniMaxH3FailureMessage(state), status))
		case "", "preparing", "queueing", "queued", "processing", "running":
			// MiniMax 是异步接口，只有明确成功或失败才结束轮询。
		default:
			return nil, fmt.Errorf("MiniMax H3 任务 %s 返回未知状态：%s", taskID, status)
		}
		if err := sleepContext(ctx, 10*time.Second); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("MiniMax H3 视频生成超时（任务 %s）", taskID)
}

func miniMaxH3TaskState(response map[string]interface{}) map[string]interface{} {
	task, ok := response["task"].(map[string]interface{})
	if ok {
		return task
	}
	return response
}

func miniMaxH3VideoBody(input canvasGenerationInput) (map[string]interface{}, error) {
	if strings.TrimSpace(input.Config.Model) != miniMaxH3Model {
		return nil, fmt.Errorf("MiniMax H3 渠道只接受模型 %s，当前为 %s", miniMaxH3Model, input.Config.Model)
	}
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		return nil, errors.New("MiniMax H3 视频提示词不能为空")
	}
	if len([]rune(prompt)) > 7000 {
		return nil, errors.New("MiniMax H3 视频提示词不能超过 7000 个字符")
	}
	duration, err := strconv.Atoi(strings.TrimSpace(input.Config.VideoSeconds))
	if err != nil || duration < 4 || duration > 15 {
		return nil, errors.New("MiniMax H3 视频时长必须是 4 至 15 秒的整数")
	}
	resolution, err := miniMaxH3Resolution(input.Config.VQuality)
	if err != nil {
		return nil, err
	}
	ratio := strings.TrimSpace(input.Config.Size)
	if !miniMaxH3Ratios[ratio] {
		return nil, fmt.Errorf("MiniMax H3 不支持画面比例：%s", ratio)
	}
	mode := metadataString(input.Metadata, "videoGenerationMode")
	if mode == "" {
		mode = "text"
	}
	if mode == "text" && ratio == "adaptive" {
		return nil, errors.New("MiniMax H3 文生视频必须选择明确画面比例，不能使用 adaptive")
	}
	if mode != "text" {
		ratio = "adaptive"
	}

	content := []map[string]interface{}{{"type": "text", "text": prompt}}
	media, err := miniMaxH3MediaContent(input, mode)
	if err != nil {
		return nil, err
	}
	content = append(content, media...)
	body := map[string]interface{}{
		"model":      miniMaxH3Model,
		"content":    content,
		"duration":   duration,
		"resolution": resolution,
		"ratio":      ratio,
	}
	insertFrozenWatermark(body, "aigc_watermark", input.Watermark)
	return body, nil
}

func miniMaxH3MediaContent(input canvasGenerationInput, mode string) ([]map[string]interface{}, error) {
	if len(input.ReferenceImages) > 9 || len(input.ReferenceVideos) > 3 || len(input.ReferenceAudios) > 3 {
		return nil, errors.New("MiniMax H3 最多支持 9 张参考图、3 个参考视频和 3 个参考音频")
	}
	if len(input.ReferenceImages)+len(input.ReferenceVideos)+len(input.ReferenceAudios) > 12 {
		return nil, errors.New("MiniMax H3 混合参考素材总数不能超过 12 个")
	}
	if mode == "text" {
		if len(input.ReferenceImages)+len(input.ReferenceVideos)+len(input.ReferenceAudios) != 0 {
			return nil, errors.New("MiniMax H3 文生视频模式不能携带参考素材")
		}
		return nil, nil
	}
	if mode == "image" || mode == "first_last_frame" {
		expected := 1
		if mode == "first_last_frame" {
			expected = 2
		}
		if len(input.ReferenceImages) != expected || len(input.ReferenceVideos) != 0 || len(input.ReferenceAudios) != 0 {
			return nil, fmt.Errorf("MiniMax H3 %s模式需要 %d 张图片且不能混用参考视频或音频", mode, expected)
		}
		items := make([]map[string]interface{}, 0, expected)
		for _, image := range input.ReferenceImages {
			role := seedanceImageRole(input, image)
			if mode == "image" {
				role = "first_frame"
			}
			mediaURL, err := miniMaxH3PublicMediaURL(image)
			if err != nil {
				return nil, err
			}
			items = append(items, map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": mediaURL}, "role": role})
		}
		return items, nil
	}
	if mode != "image_reference" && mode != "omni_reference" {
		return nil, fmt.Errorf("MiniMax H3 不支持视频生成模式：%s", mode)
	}
	if mode == "image_reference" && (len(input.ReferenceImages) == 0 || len(input.ReferenceVideos) != 0 || len(input.ReferenceAudios) != 0) {
		return nil, errors.New("MiniMax H3 图片参考模式只接受一张或多张参考图片")
	}
	if mode == "omni_reference" && len(input.ReferenceImages)+len(input.ReferenceVideos) == 0 {
		return nil, errors.New("MiniMax H3 全能参考模式不能只提供音频，至少需要图片或视频")
	}
	items := make([]map[string]interface{}, 0, len(input.ReferenceImages)+len(input.ReferenceVideos)+len(input.ReferenceAudios))
	appendMedia := func(mediaType, role string, media providerMedia) error {
		mediaURL, err := miniMaxH3PublicMediaURL(media)
		if err != nil {
			return err
		}
		items = append(items, map[string]interface{}{"type": mediaType, mediaType: map[string]interface{}{"url": mediaURL}, "role": role})
		return nil
	}
	for _, image := range input.ReferenceImages {
		if err := appendMedia("image_url", "reference_image", image); err != nil {
			return nil, err
		}
	}
	for _, video := range input.ReferenceVideos {
		if err := appendMedia("video_url", "reference_video", video); err != nil {
			return nil, err
		}
	}
	for _, audio := range input.ReferenceAudios {
		if err := appendMedia("audio_url", "reference_audio", audio); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func miniMaxH3Resolution(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "768p", "768":
		return "768P", nil
	case "2k", "1440p", "1440":
		return "2K", nil
	default:
		return "", fmt.Errorf("MiniMax H3 仅支持 768P 或 2K，当前为 %s", value)
	}
}

func miniMaxH3PublicMediaURL(media providerMedia) (string, error) {
	value := strings.TrimSpace(media.URL)
	if !isPublicMediaURL(value) {
		return "", fmt.Errorf("MiniMax H3 参考素材 %s 缺少公网可访问 URL", defaultString(media.Name, media.ID))
	}
	return value, nil
}

func miniMaxH3ResultURL(state map[string]interface{}) string {
	content, _ := state["content"].(map[string]interface{})
	return firstNonEmptyString(stringField(content, "url"), stringField(state, "url"))
}

func miniMaxH3FailureMessage(state map[string]interface{}) string {
	return firstNonEmptyString(stringField(state, "message"), stringField(state, "error_message"), stringField(state, "error"))
}
