package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type kuaiziSeedance25Media struct {
	URL  string `json:"url"`
	Role string `json:"role"`
}

type kuaiziSeedance25Request struct {
	Prompt          string                  `json:"prompt,omitempty"`
	Mode            string                  `json:"mode"`
	Images          []kuaiziSeedance25Media `json:"images,omitempty"`
	Videos          []kuaiziSeedance25Media `json:"videos,omitempty"`
	Audios          []kuaiziSeedance25Media `json:"audios,omitempty"`
	Resolution      string                  `json:"resolution"`
	Ratio           string                  `json:"ratio"`
	Duration        int                     `json:"duration"`
	GenerateAudio   bool                    `json:"generate_audio"`
	Watermark       bool                    `json:"watermark"`
	ReturnLastFrame bool                    `json:"return_last_frame"`
}

type kuaiziSeedance25Envelope struct {
	Code    *int            `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
	TraceID string          `json:"trace_id"`
}

type kuaiziSeedance25CreateData struct {
	TaskID string `json:"task_id"`
}

type kuaiziSeedance25StatusData struct {
	TaskID       string          `json:"task_id"`
	Status       string          `json:"status"`
	VideoURL     string          `json:"video_url"`
	LastFrameURL string          `json:"last_frame_url"`
	Duration     *int            `json:"duration"`
	Usage        json.RawMessage `json:"usage"`
	Error        string          `json:"error"`
}

type kuaiziSeedance25Usage struct {
	TotalTokens json.RawMessage `json:"total_tokens"`
}

type KuaiziSeedance25Created struct {
	TaskID  string
	TraceID string
}

type KuaiziSeedance25State struct {
	TaskID       string
	Status       string
	Terminal     bool
	VideoURL     string
	LastFrameURL string
	Duration     int
	TotalTokens  string
	Error        string
	TraceID      string
}

func newKuaiziSeedance25Request(input canvasGenerationInput) (kuaiziSeedance25Request, error) {
	if strings.TrimSpace(input.Config.Model) != "kuaizi-seedance-2.5" {
		return kuaiziSeedance25Request{}, fmt.Errorf("筷子 Seedance 适配器不支持模型：%s", strings.TrimSpace(input.Config.Model))
	}
	duration, err := kuaiziSeedance25Duration(input.Config.VideoSeconds)
	if err != nil {
		return kuaiziSeedance25Request{}, err
	}
	resolution, err := kuaiziSeedance25Resolution(input.Config.VQuality)
	if err != nil {
		return kuaiziSeedance25Request{}, err
	}
	ratio, err := aiOpenPlatformVideoRatio(input.Config.Size)
	if err != nil {
		return kuaiziSeedance25Request{}, err
	}
	images, videos, audios, err := kuaiziSeedance25MediaLists(input)
	if err != nil {
		return kuaiziSeedance25Request{}, err
	}
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" && len(images) == 0 && len(videos) == 0 {
		return kuaiziSeedance25Request{}, errors.New("筷子 Seedance 2.5 在没有图片或视频时必须提供提示词")
	}
	if ratio != "adaptive" && kuaiziSeedance25HasFrameInput(images) {
		return kuaiziSeedance25Request{}, errors.New("筷子 Seedance 2.5 首帧或首尾帧任务必须使用 adaptive 比例")
	}
	return kuaiziSeedance25Request{
		Prompt:          prompt,
		Mode:            "seedance2.5",
		Images:          images,
		Videos:          videos,
		Audios:          audios,
		Resolution:      resolution,
		Ratio:           ratio,
		Duration:        duration,
		GenerateAudio:   parseBool(input.Config.VideoGenerateAudio, true),
		Watermark:       parseBool(input.Config.VideoWatermark, false),
		ReturnLastFrame: true,
	}, nil
}

func kuaiziSeedance25Duration(value string) (int, error) {
	duration, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || (duration != -1 && (duration < 4 || duration > 30)) {
		return 0, errors.New("筷子 Seedance 2.5 时长必须为 4–30 秒或 -1")
	}
	return duration, nil
}

func kuaiziSeedance25Resolution(value string) (string, error) {
	resolution := strings.ToLower(strings.TrimSpace(value))
	if resolution != "480p" && resolution != "720p" {
		return "", errors.New("筷子 Seedance 2.5 分辨率只支持 480p 或 720p")
	}
	return resolution, nil
}

func kuaiziSeedance25MediaLists(input canvasGenerationInput) ([]kuaiziSeedance25Media, []kuaiziSeedance25Media, []kuaiziSeedance25Media, error) {
	if len(input.ReferenceImages) > 30 {
		return nil, nil, nil, errors.New("筷子 Seedance 2.5 最多支持 30 张参考图片")
	}
	if len(input.ReferenceVideos) > 10 {
		return nil, nil, nil, errors.New("筷子 Seedance 2.5 最多支持 10 个参考视频")
	}
	if len(input.ReferenceAudios) > 10 {
		return nil, nil, nil, errors.New("筷子 Seedance 2.5 最多支持 10 段参考音频")
	}
	images := make([]kuaiziSeedance25Media, 0, len(input.ReferenceImages))
	for _, media := range input.ReferenceImages {
		url, err := kuaiziSeedance25MediaURL(media)
		if err != nil {
			return nil, nil, nil, err
		}
		images = append(images, kuaiziSeedance25Media{URL: url, Role: seedanceImageRole(input, media)})
	}
	videos, err := kuaiziSeedance25FixedRoleMedia(input.ReferenceVideos, "reference_video")
	if err != nil {
		return nil, nil, nil, err
	}
	audios, err := kuaiziSeedance25FixedRoleMedia(input.ReferenceAudios, "reference_audio")
	if err != nil {
		return nil, nil, nil, err
	}
	return images, videos, audios, nil
}

func kuaiziSeedance25FixedRoleMedia(source []providerMedia, role string) ([]kuaiziSeedance25Media, error) {
	result := make([]kuaiziSeedance25Media, 0, len(source))
	for _, media := range source {
		url, err := kuaiziSeedance25MediaURL(media)
		if err != nil {
			return nil, err
		}
		result = append(result, kuaiziSeedance25Media{URL: url, Role: role})
	}
	return result, nil
}

func kuaiziSeedance25MediaURL(media providerMedia) (string, error) {
	for _, candidate := range []string{strings.TrimSpace(media.URL), strings.TrimSpace(media.DataURL)} {
		if strings.HasPrefix(candidate, "asset://") || isPublicMediaURL(candidate) {
			return candidate, nil
		}
	}
	return "", errors.New("筷子 Seedance 2.5 参考素材必须使用公网 HTTP(S) 地址或 asset:// 素材地址")
}

func kuaiziSeedance25HasFrameInput(images []kuaiziSeedance25Media) bool {
	for _, image := range images {
		if image.Role == "first_frame" || image.Role == "last_frame" {
			return true
		}
	}
	return false
}

func parseKuaiziSeedance25Create(payload []byte) (KuaiziSeedance25Created, error) {
	envelope, err := decodeKuaiziSeedance25Envelope(payload)
	if err != nil {
		return KuaiziSeedance25Created{}, err
	}
	var data kuaiziSeedance25CreateData
	if err := decodeKuaiziSeedance25Data(envelope.Data, &data); err != nil {
		return KuaiziSeedance25Created{}, err
	}
	taskID := strings.TrimSpace(data.TaskID)
	if taskID == "" {
		return KuaiziSeedance25Created{}, errors.New("筷子 Seedance 2.5 创建响应缺少任务 ID")
	}
	return KuaiziSeedance25Created{TaskID: taskID, TraceID: strings.TrimSpace(envelope.TraceID)}, nil
}

func parseKuaiziSeedance25Status(payload []byte) (KuaiziSeedance25State, error) {
	envelope, err := decodeKuaiziSeedance25Envelope(payload)
	if err != nil {
		return KuaiziSeedance25State{}, err
	}
	var data kuaiziSeedance25StatusData
	if err := decodeKuaiziSeedance25Data(envelope.Data, &data); err != nil {
		return KuaiziSeedance25State{}, err
	}
	state := KuaiziSeedance25State{
		TaskID:       strings.TrimSpace(data.TaskID),
		Status:       strings.ToLower(strings.TrimSpace(data.Status)),
		VideoURL:     strings.TrimSpace(data.VideoURL),
		LastFrameURL: strings.TrimSpace(data.LastFrameURL),
		Error:        strings.TrimSpace(data.Error),
		TraceID:      strings.TrimSpace(envelope.TraceID),
	}
	if state.TaskID == "" {
		return KuaiziSeedance25State{}, errors.New("筷子 Seedance 2.5 状态响应缺少任务 ID")
	}
	switch state.Status {
	case "submitted", "pending", "running":
		return state, nil
	case "failed":
		state.Terminal = true
		if state.Error == "" {
			state.Error = "上游任务失败"
		}
		return state, nil
	case "succeeded":
		state.Terminal = true
		if !isPublicMediaURL(state.VideoURL) {
			return KuaiziSeedance25State{}, errors.New("筷子 Seedance 2.5 成功响应缺少有效视频地址")
		}
		if data.Duration == nil || *data.Duration <= 0 {
			return KuaiziSeedance25State{}, errors.New("筷子 Seedance 2.5 成功响应缺少有效时长")
		}
		state.Duration = *data.Duration
		state.TotalTokens, err = kuaiziSeedance25TotalTokens(data.Usage)
		if err != nil {
			return KuaiziSeedance25State{}, err
		}
		return state, nil
	default:
		return KuaiziSeedance25State{}, fmt.Errorf("筷子 Seedance 2.5 返回未知状态：%s", state.Status)
	}
}

func decodeKuaiziSeedance25Envelope(payload []byte) (kuaiziSeedance25Envelope, error) {
	var envelope kuaiziSeedance25Envelope
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(&envelope); err != nil {
		return envelope, errors.New("筷子 Seedance 2.5 返回了无效 JSON")
	}
	if err := ensureKuaiziSeedance25JSONEOF(decoder); err != nil {
		return envelope, err
	}
	if envelope.Code == nil {
		return envelope, errors.New("筷子 Seedance 2.5 响应缺少业务码")
	}
	if *envelope.Code != 0 {
		return envelope, fmt.Errorf("筷子 Seedance 2.5 请求失败（code=%d）", *envelope.Code)
	}
	return envelope, nil
}

func decodeKuaiziSeedance25Data(raw json.RawMessage, target interface{}) error {
	if len(raw) == 0 || string(raw) == "null" {
		return errors.New("筷子 Seedance 2.5 响应缺少 data")
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return errors.New("筷子 Seedance 2.5 data 无效")
	}
	return nil
}

func ensureKuaiziSeedance25JSONEOF(decoder *json.Decoder) error {
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("筷子 Seedance 2.5 响应包含尾随内容")
	}
	return nil
}

func kuaiziSeedance25TotalTokens(raw json.RawMessage) (string, error) {
	var usage kuaiziSeedance25Usage
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &usage) != nil || len(usage.TotalTokens) == 0 {
		return "", errors.New("筷子 Seedance 2.5 成功响应缺少 total_tokens")
	}
	value := strings.Trim(string(usage.TotalTokens), "\"")
	if value == "" || strings.TrimLeft(value, "0123456789") != "" {
		return "", errors.New("筷子 Seedance 2.5 total_tokens 无效")
	}
	return value, nil
}
