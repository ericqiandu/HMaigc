package service

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
)

const defaultMiniMaxSystemVoiceCount = 303

//go:embed defaultdata/minimax-system-voices.json
var defaultMiniMaxSystemVoicesJSON []byte

type defaultChannelVoiceSpec struct {
	VoiceKey    string `json:"voiceKey"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Language    string `json:"language"`
}

func (s *Service) EnsureDefaultChannelVoices() error {
	channels, err := s.repo.SystemChannels(true)
	if err != nil {
		return err
	}
	for index := range channels {
		if err := s.ensureDefaultChannelVoicesForChannel(&channels[index]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ensureDefaultChannelVoicesForChannel(channel *model.ModelChannel) error {
	if channel == nil || channel.InterfaceType != model.ChannelInterfaceMiniMaxSpeech {
		return nil
	}
	voices, err := defaultMiniMaxSystemVoices(channel.ID, time.Now())
	if err != nil {
		return err
	}
	return s.repo.EnsureDefaultChannelVoices(voices)
}

func defaultMiniMaxSystemVoices(channelID string, now time.Time) ([]model.ChannelVoice, error) {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return nil, errors.New("默认 MiniMax 音色缺少渠道 ID")
	}
	decoder := json.NewDecoder(bytes.NewReader(defaultMiniMaxSystemVoicesJSON))
	decoder.DisallowUnknownFields()
	var specs []defaultChannelVoiceSpec
	if err := decoder.Decode(&specs); err != nil {
		return nil, fmt.Errorf("解析默认 MiniMax 音色失败: %w", err)
	}
	if err := ensureJSONDocumentEnded(decoder); err != nil {
		return nil, err
	}
	if len(specs) != defaultMiniMaxSystemVoiceCount {
		return nil, fmt.Errorf("默认 MiniMax 音色数量为 %d，预期 %d", len(specs), defaultMiniMaxSystemVoiceCount)
	}
	seen := make(map[string]bool, len(specs))
	voices := make([]model.ChannelVoice, 0, len(specs))
	for _, spec := range specs {
		spec.VoiceKey = strings.TrimSpace(spec.VoiceKey)
		spec.DisplayName = strings.TrimSpace(spec.DisplayName)
		spec.Language = strings.TrimSpace(spec.Language)
		if spec.VoiceKey == "" || spec.DisplayName == "" || spec.Language == "" {
			return nil, fmt.Errorf("默认 MiniMax 音色字段不完整: %q", spec.VoiceKey)
		}
		if seen[spec.VoiceKey] {
			return nil, fmt.Errorf("默认 MiniMax 音色标识重复: %s", spec.VoiceKey)
		}
		if _, supported := miniMaxLanguageBoosts[spec.Language]; !supported || spec.Language == "auto" {
			return nil, fmt.Errorf("默认 MiniMax 音色 %s 的语言无效: %s", spec.VoiceKey, spec.Language)
		}
		seen[spec.VoiceKey] = true
		voices = append(voices, model.ChannelVoice{
			ID: newID(), ChannelID: channelID, VoiceKey: spec.VoiceKey, DisplayName: spec.DisplayName,
			Description: spec.Description, Language: spec.Language, Kind: "system",
			AccessPolicy: model.ModelAccessAuthenticated, CompatibleModelsJSON: "[]",
			ProviderStatus: "active", Enabled: true, CreatedAt: now, UpdatedAt: now,
		})
	}
	return voices, nil
}

func ensureJSONDocumentEnded(decoder *json.Decoder) error {
	var trailing struct{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("默认 MiniMax 音色包含多个 JSON 文档")
		}
		return fmt.Errorf("解析默认 MiniMax 音色尾部失败: %w", err)
	}
	return nil
}
