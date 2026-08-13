package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const klingVideoPollInterval = 10 * time.Second

type klingCredentials struct {
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
}

func runKlingVideoTask(ctx context.Context, input canvasGenerationInput) (map[string]interface{}, error) {
	credentials, err := parseKlingCredentials(input.Config.APIKey)
	if err != nil {
		return nil, err
	}
	requestPath, body, err := klingVideoRequest(input)
	if err != nil {
		return nil, err
	}
	taskID := resumedProviderRequestID(ctx)
	if taskID == "" {
		var created map[string]interface{}
		if err := klingJSONRequest(ctx, input.Config, credentials, http.MethodPost, requestPath, body, &created); err != nil {
			return nil, err
		}
		data := klingResponseData(created)
		taskID = firstNonEmptyString(stringField(data, "task_id"), stringField(data, "taskId"))
		if taskID == "" {
			return nil, errors.New("可灵视频创建任务成功但未返回 task_id")
		}
	}

	pollPath := requestPath + "/" + url.PathEscape(taskID)
	for deadline := providerPollingDeadline(ctx); time.Now().Before(deadline); {
		var response map[string]interface{}
		if err := klingJSONRequest(ctx, input.Config, credentials, http.MethodGet, pollPath, nil, &response); err != nil {
			return nil, err
		}
		state := klingResponseData(response)
		status := strings.ToLower(strings.TrimSpace(firstNonEmptyString(stringField(state, "task_status"), stringField(state, "status"))))
		switch status {
		case "succeed", "succeeded", "success", "completed":
			videoURL := klingVideoResultURL(state)
			if videoURL == "" {
				return nil, fmt.Errorf("可灵视频任务 %s 已成功，但响应缺少视频地址", taskID)
			}
			data, mimeType, err := getExternalBinary(withProviderRequestKind(ctx, "download"), videoURL)
			if err != nil {
				return nil, fmt.Errorf("可灵视频结果下载失败（任务 %s）：%w", taskID, err)
			}
			mimeType = normalizedMediaMimeType(mimeType, data)
			return map[string]interface{}{
				"mode":   "video",
				"taskId": taskID,
				"video":  map[string]interface{}{"dataUrl": dataURL(mimeType, data), "mimeType": mimeType},
			}, nil
		case "failed", "fail", "cancelled", "canceled":
			return nil, fmt.Errorf("可灵视频生成失败（任务 %s）：%s", taskID, defaultString(klingFailureMessage(state), status))
		case "submitted", "pending", "queued", "processing", "running":
			// 可灵为异步任务，继续轮询直到明确成功或失败。
		case "":
			return nil, fmt.Errorf("可灵视频任务 %s 的响应缺少 task_status", taskID)
		default:
			return nil, fmt.Errorf("可灵视频任务 %s 返回未知状态：%s", taskID, status)
		}
		if err := sleepContext(ctx, klingVideoPollInterval); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("可灵视频生成超时（任务 %s）", taskID)
}

func parseKlingCredentials(raw string) (klingCredentials, error) {
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	decoder.DisallowUnknownFields()
	var credentials klingCredentials
	if err := decoder.Decode(&credentials); err != nil {
		return klingCredentials{}, errors.New("可灵渠道密钥必须是 JSON：{\"accessKey\":\"...\",\"secretKey\":\"...\"}")
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return klingCredentials{}, errors.New("可灵渠道密钥只能包含一个 JSON 对象")
	}
	credentials.AccessKey = strings.TrimSpace(credentials.AccessKey)
	credentials.SecretKey = strings.TrimSpace(credentials.SecretKey)
	if credentials.AccessKey == "" || credentials.SecretKey == "" {
		return klingCredentials{}, errors.New("可灵渠道密钥缺少 accessKey 或 secretKey")
	}
	return credentials, nil
}

func klingVideoRequest(input canvasGenerationInput) (string, map[string]interface{}, error) {
	modelName := strings.TrimSpace(input.Config.Model)
	if modelName == "" {
		return "", nil, errors.New("可灵视频模型名称不能为空")
	}
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		return "", nil, errors.New("可灵视频提示词不能为空")
	}
	if len([]rune(prompt)) > 2500 {
		return "", nil, errors.New("可灵视频提示词不能超过 2500 个字符")
	}
	duration := strings.TrimSpace(input.Config.VideoSeconds)
	if duration != "5" && duration != "10" {
		return "", nil, fmt.Errorf("可灵视频仅支持 5 秒或 10 秒，当前为 %s", duration)
	}
	mode, err := klingQualityMode(input.Config.VQuality)
	if err != nil {
		return "", nil, err
	}
	if strings.EqualFold(strings.TrimSpace(input.Config.VideoGenerateAudio), "true") {
		return "", nil, errors.New("当前可灵视频接口不支持同步生成音频")
	}
	generationMode := metadataString(input.Metadata, "videoGenerationMode")
	if generationMode == "" {
		generationMode = "text"
	}
	body := map[string]interface{}{
		"model_name": modelName,
		"prompt":     prompt,
		"duration":   duration,
		"mode":       mode,
	}
	switch generationMode {
	case "text":
		if len(input.ReferenceImages)+len(input.ReferenceVideos)+len(input.ReferenceAudios) != 0 {
			return "", nil, errors.New("可灵文生视频不能携带参考素材")
		}
		ratio := strings.TrimSpace(input.Config.Size)
		if ratio != "16:9" && ratio != "9:16" && ratio != "1:1" {
			return "", nil, fmt.Errorf("可灵文生视频仅支持 16:9、9:16 或 1:1，当前为 %s", ratio)
		}
		body["aspect_ratio"] = ratio
		return "/v1/videos/text2video", body, nil
	case "image", "first_last_frame":
		expected := 1
		if generationMode == "first_last_frame" {
			expected = 2
		}
		if len(input.ReferenceImages) != expected || len(input.ReferenceVideos) != 0 || len(input.ReferenceAudios) != 0 {
			return "", nil, fmt.Errorf("可灵 %s 模式需要 %d 张图片且不能混用视频或音频素材", generationMode, expected)
		}
		for index, image := range input.ReferenceImages {
			mediaURL, mediaErr := klingPublicMediaURL(image)
			if mediaErr != nil {
				return "", nil, mediaErr
			}
			if index == 0 {
				body["image"] = mediaURL
			} else {
				body["image_tail"] = mediaURL
			}
		}
		return "/v1/videos/image2video", body, nil
	default:
		return "", nil, fmt.Errorf("可灵视频渠道不支持生成模式：%s", generationMode)
	}
}

func klingQualityMode(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "720", "720p", "std", "standard":
		return "std", nil
	case "1080", "1080p", "pro", "professional":
		return "pro", nil
	default:
		return "", fmt.Errorf("可灵视频质量仅支持 720P（std）或 1080P（pro），当前为 %s", value)
	}
}

func klingPublicMediaURL(media providerMedia) (string, error) {
	value := strings.TrimSpace(media.URL)
	if !isPublicMediaURL(value) {
		return "", fmt.Errorf("可灵参考素材 %s 缺少公网可访问 URL", defaultString(media.Name, media.ID))
	}
	return value, nil
}

func klingJSONRequest(ctx context.Context, config providerConfig, credentials klingCredentials, method string, path string, body interface{}, target interface{}) error {
	token, err := klingJWT(credentials, time.Now())
	if err != nil {
		return err
	}
	var requestBody *bytes.Reader
	if body == nil {
		requestBody = bytes.NewReader(nil)
	} else {
		data, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return marshalErr
		}
		requestBody = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, apiURL(config.BaseURL, path), requestBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return doJSON(req, target)
}

func klingJWT(credentials klingCredentials, now time.Time) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(map[string]interface{}{
		"iss": credentials.AccessKey,
		"exp": now.Add(30 * time.Minute).Unix(),
		"nbf": now.Add(-5 * time.Second).Unix(),
	})
	if err != nil {
		return "", err
	}
	encode := base64.RawURLEncoding.EncodeToString
	unsigned := encode(header) + "." + encode(claims)
	mac := hmac.New(sha256.New, []byte(credentials.SecretKey))
	if _, err := mac.Write([]byte(unsigned)); err != nil {
		return "", err
	}
	return unsigned + "." + encode(mac.Sum(nil)), nil
}

func klingResponseData(response map[string]interface{}) map[string]interface{} {
	data, ok := response["data"].(map[string]interface{})
	if ok {
		return data
	}
	return response
}

func klingVideoResultURL(state map[string]interface{}) string {
	result, _ := state["task_result"].(map[string]interface{})
	videos, _ := result["videos"].([]interface{})
	for _, item := range videos {
		video, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if videoURL := strings.TrimSpace(stringField(video, "url")); isPublicMediaURL(videoURL) {
			return videoURL
		}
	}
	return ""
}

func klingFailureMessage(state map[string]interface{}) string {
	return firstNonEmptyString(stringField(state, "task_status_msg"), stringField(state, "message"), stringField(state, "error"))
}
