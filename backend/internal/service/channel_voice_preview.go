package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

const channelVoicePreviewTTL = 30 * 24 * time.Hour

type ChannelVoicePreviewRequest struct {
	Model string `json:"model"`
}

type PublicChannelVoicePreview struct {
	AudioDataURL string `json:"audioDataUrl"`
	MimeType     string `json:"mimeType"`
	SHA256       string `json:"sha256"`
	TraceID      string `json:"traceId,omitempty"`
	Cached       bool   `json:"cached"`
}

type channelVoicePreviewResult struct {
	Preview model.ChannelVoicePreview
	Cached  bool
}

func (s *Service) MiniMaxChannelVoicePreview(ctx context.Context, user *model.User, channelID string, voiceID string, req ChannelVoicePreviewRequest) (*PublicChannelVoicePreview, error) {
	if user == nil {
		return nil, Unauthorized("请先登录")
	}
	modelKey := strings.TrimSpace(req.Model)
	if modelKey == "" {
		return nil, BadAuthRequest("试听音色必须指定当前音频模型")
	}
	resolved, err := s.resolveProviderConfig(providerConfig{
		ChannelID: channelID, Model: modelKey,
		AudioFormat: "mp3", AudioSpeed: "1", AudioVolume: "1", AudioPitch: "0",
		AudioLanguageBoost: "auto", AudioSampleRate: "32000", AudioBitrate: "64000", AudioChannel: "1",
	})
	if err != nil {
		return nil, err
	}
	if resolved.InterfaceType != string(model.ChannelInterfaceMiniMaxSpeech) {
		return nil, BadAuthRequest("音色试听仅支持 MiniMax Speech 渠道")
	}
	channelModel, err := s.requireAccessibleChannelModel(user.ID, resolved.ChannelID, resolved.Model)
	if err != nil {
		return nil, err
	}
	if normalizeCapability(channelModel.Capability) != "audio" {
		return nil, BadAuthRequest("当前模型不是音频生成模型")
	}
	voice, err := s.repo.ChannelVoiceByID(resolved.ChannelID, strings.TrimSpace(voiceID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, BadAuthRequest("试听音色不存在或已被删除")
	}
	if err != nil {
		return nil, err
	}
	resolved.AudioVoice = voice.VoiceKey
	voiceLanguage := miniMaxVoiceLanguage(voice.VoiceKey, voice.Language)
	if voiceLanguage != "" {
		resolved.AudioLanguageBoost = voiceLanguage
	}
	if err := s.validateAudioTaskVoice(user.ID, resolved); err != nil {
		return nil, err
	}
	if cached, cacheErr := s.freshChannelVoicePreview(voice.ID, voice.VoiceKey, resolved.Model, time.Now()); cacheErr == nil {
		return publicChannelVoicePreview(*cached, true), nil
	} else if !errors.Is(cacheErr, gorm.ErrRecordNotFound) {
		return nil, cacheErr
	}

	key := voice.ID + "\x00" + resolved.Model
	value, err, shared := s.voicePreviewGroup.Do(key, func() (interface{}, error) {
		if cached, cacheErr := s.freshChannelVoicePreview(voice.ID, voice.VoiceKey, resolved.Model, time.Now()); cacheErr == nil {
			return channelVoicePreviewResult{Preview: *cached, Cached: true}, nil
		} else if !errors.Is(cacheErr, gorm.ErrRecordNotFound) {
			return nil, cacheErr
		}
		analytics := providerAnalyticsContext{
			Service: s, Source: "voice-preview", UserID: user.ID, ChannelID: resolved.ChannelID,
			Capability: "audio", Operation: "voice_preview", Model: resolved.Model, RequestKind: "preview",
		}
		previewContext := context.WithValue(ctx, providerAnalyticsKey{}, analytics)
		output, generateErr := generateMiniMaxAudio(previewContext, canvasGenerationInput{
			Mode: "audio", Prompt: miniMaxVoicePreviewText(voiceLanguage), Config: resolved,
		})
		if generateErr != nil {
			return nil, generateErr
		}
		digest := sha256.Sum256(output.Audio)
		now := time.Now()
		preview := model.ChannelVoicePreview{
			ID: newID(), ChannelID: resolved.ChannelID, ChannelVoiceID: voice.ID, VoiceKey: voice.VoiceKey, Model: resolved.Model,
			MimeType: output.Mime, Audio: output.Audio, SHA256: hex.EncodeToString(digest[:]),
			ProviderTraceID: output.TraceID, CreatedAt: now, UpdatedAt: now,
		}
		if saveErr := s.repo.SaveChannelVoicePreview(&preview); saveErr != nil {
			return nil, fmt.Errorf("保存音色试听缓存失败：%w", saveErr)
		}
		return channelVoicePreviewResult{Preview: preview, Cached: false}, nil
	})
	if err != nil {
		return nil, err
	}
	result, ok := value.(channelVoicePreviewResult)
	if !ok {
		return nil, errors.New("音色试听结果类型异常")
	}
	return publicChannelVoicePreview(result.Preview, result.Cached || shared), nil
}

func (s *Service) freshChannelVoicePreview(voiceID string, voiceKey string, modelKey string, now time.Time) (*model.ChannelVoicePreview, error) {
	preview, err := s.repo.ChannelVoicePreview(voiceID, modelKey)
	if err != nil {
		return nil, err
	}
	if preview.VoiceKey != voiceKey || len(preview.Audio) == 0 || preview.MimeType == "" || preview.SHA256 == "" || preview.UpdatedAt.Before(now.Add(-channelVoicePreviewTTL)) {
		return nil, gorm.ErrRecordNotFound
	}
	digest := sha256.Sum256(preview.Audio)
	if !strings.EqualFold(preview.SHA256, hex.EncodeToString(digest[:])) {
		return nil, errors.New("音色试听缓存校验失败，请联系管理员清理后重试")
	}
	return preview, nil
}

func publicChannelVoicePreview(preview model.ChannelVoicePreview, cached bool) *PublicChannelVoicePreview {
	return &PublicChannelVoicePreview{
		AudioDataURL: "data:" + preview.MimeType + ";base64," + base64.StdEncoding.EncodeToString(preview.Audio),
		MimeType:     preview.MimeType, SHA256: preview.SHA256, TraceID: preview.ProviderTraceID, Cached: cached,
	}
}

func miniMaxVoicePreviewText(language string) string {
	switch language {
	case "English":
		return "Welcome to HMaigc. This is a preview of the selected voice."
	case "Japanese":
		return "HMaigcへようこそ。選択した音声の試聴です。"
	case "Korean":
		return "HMaigc에 오신 것을 환영합니다. 선택한 음성의 미리듣기입니다."
	case "Spanish":
		return "Bienvenido a HMaigc. Esta es una muestra de la voz seleccionada."
	case "Portuguese":
		return "Bem-vindo ao HMaigc. Esta é uma prévia da voz selecionada."
	case "French":
		return "Bienvenue sur HMaigc. Voici un aperçu de la voix sélectionnée."
	case "German":
		return "Willkommen bei HMaigc. Dies ist eine Vorschau der ausgewählten Stimme."
	case "Russian":
		return "Добро пожаловать в HMaigc. Это пример выбранного голоса."
	default:
		return "你好，欢迎使用弘梦。这是当前音色的试听效果。"
	}
}
