package service

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

var miniMaxAudioEmotions = map[string]struct{}{
	"happy": {}, "sad": {}, "angry": {}, "fearful": {}, "disgusted": {},
	"surprised": {}, "calm": {}, "fluent": {}, "whisper": {},
}

var miniMaxLanguageBoosts = map[string]struct{}{
	"Chinese": {}, "Chinese,Yue": {}, "English": {}, "Arabic": {}, "Russian": {},
	"Spanish": {}, "French": {}, "Portuguese": {}, "German": {}, "Turkish": {},
	"Dutch": {}, "Ukrainian": {}, "Vietnamese": {}, "Indonesian": {}, "Japanese": {},
	"Italian": {}, "Korean": {}, "Thai": {}, "Polish": {}, "Romanian": {},
	"Greek": {}, "Czech": {}, "Finnish": {}, "Hindi": {}, "Bulgarian": {},
	"Danish": {}, "Hebrew": {}, "Malay": {}, "Persian": {}, "Slovak": {},
	"Swedish": {}, "Croatian": {}, "Filipino": {}, "Hungarian": {}, "Norwegian": {},
	"Slovenian": {}, "Catalan": {}, "Nynorsk": {}, "Tamil": {}, "Afrikaans": {},
	"auto": {},
}

var miniMaxAudioSampleRates = map[int]struct{}{8000: {}, 16000: {}, 22050: {}, 24000: {}, 32000: {}, 44100: {}}
var miniMaxAudioBitrates = map[int]struct{}{32000: {}, 64000: {}, 128000: {}, 256000: {}}

type miniMaxAudioSettings struct {
	Format        string
	Speed         float64
	Volume        float64
	Pitch         int
	Emotion       string
	LanguageBoost string
	SampleRate    int
	Bitrate       int
	Channel       int
}

type miniMaxAudioOutput struct {
	Audio   []byte
	Mime    string
	Format  string
	TraceID string
}

func runMiniMaxAudioTask(ctx context.Context, input canvasGenerationInput) (map[string]interface{}, error) {
	output, err := generateMiniMaxAudio(ctx, input)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"mode": "audio",
		"audio": map[string]interface{}{
			"dataUrl": dataURL(output.Mime, output.Audio), "mimeType": output.Mime,
			"format": output.Format, "traceId": output.TraceID,
		},
	}, nil
}

func generateMiniMaxAudio(ctx context.Context, input canvasGenerationInput) (miniMaxAudioOutput, error) {
	if strings.TrimSpace(input.Prompt) == "" {
		return miniMaxAudioOutput{}, errors.New("MiniMax 语音文本不能为空")
	}
	if utf8.RuneCountInString(input.Prompt) >= 10000 {
		return miniMaxAudioOutput{}, errors.New("MiniMax 同步语音文本必须少于 10000 个字符")
	}
	if strings.TrimSpace(input.Config.AudioVoice) == "" {
		return miniMaxAudioOutput{}, errors.New("MiniMax 语音生成必须选择后台启用的音色")
	}
	if strings.TrimSpace(input.Config.AudioInstructions) != "" {
		return miniMaxAudioOutput{}, errors.New("MiniMax Speech 不支持 OpenAI instructions 参数，请清空额外语音指令后重试")
	}
	settings, err := parseMiniMaxAudioSettings(input.Config)
	if err != nil {
		return miniMaxAudioOutput{}, err
	}
	voiceSetting := map[string]interface{}{
		"voice_id": input.Config.AudioVoice,
		"speed":    settings.Speed,
		"vol":      settings.Volume,
		"pitch":    settings.Pitch,
	}
	if settings.Emotion != "" {
		voiceSetting["emotion"] = settings.Emotion
	}
	audioSetting := map[string]interface{}{
		"sample_rate": settings.SampleRate,
		"format":      settings.Format,
		"channel":     settings.Channel,
	}
	if settings.Format == "mp3" {
		audioSetting["bitrate"] = settings.Bitrate
	}
	body := map[string]interface{}{
		"model":         input.Config.Model,
		"text":          input.Prompt,
		"stream":        false,
		"voice_setting": voiceSetting,
		"audio_setting": audioSetting,
		"output_format": "hex",
	}
	if settings.LanguageBoost != "" {
		body["language_boost"] = settings.LanguageBoost
	}
	var response map[string]interface{}
	if err := postJSON(ctx, input.Config, "/t2a_v2", body, &response); err != nil {
		return miniMaxAudioOutput{}, err
	}
	data, _ := response["data"].(map[string]interface{})
	if status := firstInt64(data, "status"); status != 2 {
		return miniMaxAudioOutput{}, fmt.Errorf("MiniMax 语音接口返回未完成状态：%d", status)
	}
	audioHex := stringField(data, "audio")
	if audioHex == "" {
		return miniMaxAudioOutput{}, errors.New("MiniMax 语音接口成功响应缺少音频数据")
	}
	audio, err := hex.DecodeString(audioHex)
	if err != nil {
		return miniMaxAudioOutput{}, fmt.Errorf("解析 MiniMax 音频数据失败：%w", err)
	}
	if len(audio) == 0 {
		return miniMaxAudioOutput{}, errors.New("MiniMax 语音接口返回空音频")
	}
	return miniMaxAudioOutput{
		Audio: audio, Mime: miniMaxAudioMimeType(settings.Format),
		Format: settings.Format, TraceID: stringField(response, "trace_id"),
	}, nil
}

func parseMiniMaxAudioSettings(config providerConfig) (miniMaxAudioSettings, error) {
	speed, err := parseMiniMaxFloat(config.AudioSpeed, 1, 0.5, 2, "语速", false)
	if err != nil {
		return miniMaxAudioSettings{}, err
	}
	volume, err := parseMiniMaxFloat(config.AudioVolume, 1, 0, 10, "音量", true)
	if err != nil {
		return miniMaxAudioSettings{}, err
	}
	pitch, err := parseMiniMaxInt(config.AudioPitch, 0, -12, 12, "音调")
	if err != nil {
		return miniMaxAudioSettings{}, err
	}
	format := strings.ToLower(defaultString(config.AudioFormat, "mp3"))
	switch format {
	case "mp3", "wav", "flac":
	default:
		return miniMaxAudioSettings{}, fmt.Errorf("MiniMax 非流式语音仅支持 mp3、wav 或 flac，当前为 %s", format)
	}
	emotion := strings.TrimSpace(config.AudioEmotion)
	if emotion != "" {
		if _, ok := miniMaxAudioEmotions[emotion]; !ok {
			return miniMaxAudioSettings{}, fmt.Errorf("MiniMax 情绪参数无效：%s", emotion)
		}
		if strings.HasPrefix(strings.TrimSpace(config.Model), "speech-2.8-") && emotion == "whisper" {
			return miniMaxAudioSettings{}, errors.New("MiniMax speech-2.8 不支持 whisper 情绪")
		}
	}
	languageBoost := strings.TrimSpace(config.AudioLanguageBoost)
	if languageBoost != "" {
		if _, ok := miniMaxLanguageBoosts[languageBoost]; !ok {
			return miniMaxAudioSettings{}, fmt.Errorf("MiniMax 语言增强参数无效：%s", languageBoost)
		}
	}
	sampleRate, err := parseMiniMaxChoice(config.AudioSampleRate, 32000, miniMaxAudioSampleRates, "采样率")
	if err != nil {
		return miniMaxAudioSettings{}, err
	}
	bitrate, err := parseMiniMaxChoice(config.AudioBitrate, 128000, miniMaxAudioBitrates, "比特率")
	if err != nil {
		return miniMaxAudioSettings{}, err
	}
	channel, err := parseMiniMaxInt(config.AudioChannel, 1, 1, 2, "声道数")
	if err != nil {
		return miniMaxAudioSettings{}, err
	}
	return miniMaxAudioSettings{
		Format: format, Speed: speed, Volume: volume, Pitch: pitch, Emotion: emotion,
		LanguageBoost: languageBoost, SampleRate: sampleRate, Bitrate: bitrate, Channel: channel,
	}, nil
}

func parseMiniMaxFloat(raw string, fallback float64, minimum float64, maximum float64, label string, exclusiveMinimum bool) (float64, error) {
	value := fallback
	if trimmed := strings.TrimSpace(raw); trimmed != "" {
		parsed, err := strconv.ParseFloat(trimmed, 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return 0, fmt.Errorf("MiniMax %s必须是有效数字", label)
		}
		value = parsed
	}
	if (exclusiveMinimum && value <= minimum) || (!exclusiveMinimum && value < minimum) || value > maximum {
		left := "["
		if exclusiveMinimum {
			left = "("
		}
		return 0, fmt.Errorf("MiniMax %s必须在 %s%g, %g] 范围内", label, left, minimum, maximum)
	}
	return value, nil
}

func parseMiniMaxInt(raw string, fallback int, minimum int, maximum int, label string) (int, error) {
	value := fallback
	if trimmed := strings.TrimSpace(raw); trimmed != "" {
		parsed, err := strconv.Atoi(trimmed)
		if err != nil {
			return 0, fmt.Errorf("MiniMax %s必须是整数", label)
		}
		value = parsed
	}
	if value < minimum || value > maximum {
		return 0, fmt.Errorf("MiniMax %s必须在 [%d, %d] 范围内", label, minimum, maximum)
	}
	return value, nil
}

func parseMiniMaxChoice(raw string, fallback int, choices map[int]struct{}, label string) (int, error) {
	value, err := parseMiniMaxInt(raw, fallback, 1, math.MaxInt, label)
	if err != nil {
		return 0, err
	}
	if _, ok := choices[value]; !ok {
		return 0, fmt.Errorf("MiniMax %s不支持 %d", label, value)
	}
	return value, nil
}

func miniMaxAudioMimeType(format string) string {
	switch format {
	case "wav":
		return "audio/wav"
	case "flac":
		return "audio/flac"
	default:
		return "audio/mpeg"
	}
}
