package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

const miniMaxCloneAudioLimit int64 = 20 << 20

var miniMaxVoiceIDPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{6,254}[A-Za-z0-9]$`)

type ChannelVoiceRequest struct {
	VoiceKey         string                  `json:"voiceKey"`
	DisplayName      string                  `json:"displayName"`
	Description      string                  `json:"description"`
	Language         string                  `json:"language"`
	Kind             string                  `json:"kind"`
	AccessPolicy     model.ModelAccessPolicy `json:"accessPolicy"`
	CompatibleModels []string                `json:"compatibleModels"`
	Enabled          *bool                   `json:"enabled"`
}

type CloneChannelVoiceRequest struct {
	VoiceKey         string
	DisplayName      string
	Description      string
	Language         string
	AccessPolicy     model.ModelAccessPolicy
	CompatibleModels []string
	ConsentConfirmed bool
	IdempotencyKey   string
}

func (s *Service) AdminChannelVoices(actor *model.User, channelID string) ([]PublicChannelVoice, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if _, err := s.requireMiniMaxSpeechChannel(channelID); err != nil {
		return nil, err
	}
	voices, err := s.repo.ChannelVoices(channelID, true)
	if err != nil {
		return nil, err
	}
	return publicChannelVoices(voices, true, true)
}

func (s *Service) SaveAdminChannelVoice(actor *model.User, channelID string, id string, req ChannelVoiceRequest) (*PublicChannelVoice, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if _, err := s.requireMiniMaxSpeechChannel(channelID); err != nil {
		return nil, err
	}
	voiceKey := strings.TrimSpace(req.VoiceKey)
	displayName := strings.TrimSpace(req.DisplayName)
	if voiceKey == "" || displayName == "" {
		return nil, BadAuthRequest("音色标识和展示名称不能为空")
	}
	if existing, findErr := s.repo.ChannelVoiceByKey(channelID, voiceKey); findErr == nil && existing.ID != strings.TrimSpace(id) {
		return nil, BadAuthRequest("同一渠道内音色标识不能重复")
	} else if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return nil, findErr
	}
	kind := normalizeChannelVoiceKind(req.Kind)
	if kind == "" {
		return nil, BadAuthRequest("音色类型仅支持 system、voice_cloning 或 voice_generation")
	}
	accessPolicy := normalizeModelAccessPolicy(req.AccessPolicy)
	compatibleModels := uniqueNonEmpty(req.CompatibleModels)
	compatibleModelsJSON, err := json.Marshal(compatibleModels)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	voice := model.ChannelVoice{
		ID: newID(), ChannelID: channelID, VoiceKey: voiceKey, DisplayName: displayName,
		Description: strings.TrimSpace(req.Description), Language: strings.TrimSpace(req.Language),
		Kind: kind, AccessPolicy: accessPolicy, CompatibleModelsJSON: string(compatibleModelsJSON),
		ProviderStatus: "active", Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if strings.TrimSpace(id) != "" {
		current, findErr := s.repo.ChannelVoiceByID(channelID, id)
		if findErr != nil {
			return nil, findErr
		}
		voice.ID = current.ID
		voice.CreatedAt = current.CreatedAt
		voice.ProviderStatus = current.ProviderStatus
		voice.SourceFilename = current.SourceFilename
		voice.SourceSHA256 = current.SourceSHA256
		voice.SourceBytes = current.SourceBytes
		voice.ConsentConfirmedAt = current.ConsentConfirmedAt
		voice.IdempotencyKey = current.IdempotencyKey
		voice.LastError = current.LastError
	}
	if req.Enabled != nil {
		voice.Enabled = *req.Enabled
	}
	audit, err := newAdminAuditEvent(actor, "channel_voice.save", "channel_voice", voice.ID, "保存音色目录与访问策略", map[string]any{
		"channelId": channelID, "voiceKey": voice.VoiceKey, "kind": voice.Kind, "accessPolicy": voice.AccessPolicy, "enabled": voice.Enabled,
	})
	if err != nil {
		return nil, err
	}
	if err := s.repo.SaveChannelVoiceWithAudit(&voice, audit); err != nil {
		return nil, err
	}
	public, err := publicChannelVoice(voice, true, true)
	if err != nil {
		return nil, err
	}
	return &public, nil
}

func (s *Service) SyncMiniMaxChannelVoices(ctx context.Context, actor *model.User, channelID string) ([]PublicChannelVoice, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	channel, err := s.requireMiniMaxSpeechChannel(channelID)
	if err != nil {
		return nil, err
	}
	var response map[string]interface{}
	if err := postJSON(ctx, providerConfig{BaseURL: channel.BaseURL, APIKey: channel.APIKey}, "/get_voice", map[string]interface{}{"voice_type": "all"}, &response); err != nil {
		return nil, fmt.Errorf("同步 MiniMax 音色失败：%w", err)
	}
	now := time.Now()
	voices, err := miniMaxVoicesFromResponse(channel.ID, response, now)
	if err != nil {
		return nil, fmt.Errorf("解析 MiniMax 音色目录失败：%w", err)
	}
	if len(voices) == 0 {
		return nil, errors.New("MiniMax 音色接口未返回任何可识别音色，未写入本地目录")
	}
	audit, err := newAdminAuditEvent(actor, "channel_voice.sync", "model_channel", channel.ID, "同步 MiniMax 音色目录", map[string]any{"count": len(voices)})
	if err != nil {
		return nil, err
	}
	if err := s.repo.UpsertChannelVoices(voices, audit); err != nil {
		return nil, err
	}
	return s.AdminChannelVoices(actor, channel.ID)
}

func (s *Service) CloneMiniMaxChannelVoice(ctx context.Context, actor *model.User, channelID string, req CloneChannelVoiceRequest, file multipart.File, filename string) (*PublicChannelVoice, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	channel, err := s.requireMiniMaxSpeechChannel(channelID)
	if err != nil {
		return nil, err
	}
	if !req.ConsentConfirmed {
		return nil, BadAuthRequest("必须确认已获得声音本人授权后才能克隆音色")
	}
	req.VoiceKey = strings.TrimSpace(req.VoiceKey)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	if !miniMaxVoiceIDPattern.MatchString(req.VoiceKey) {
		return nil, BadAuthRequest("音色标识须为 8-256 位，以字母开头，仅含字母、数字、下划线或连字符，并以字母或数字结尾")
	}
	if req.DisplayName == "" || req.IdempotencyKey == "" {
		return nil, BadAuthRequest("展示名称和幂等键不能为空")
	}
	if existing, findErr := s.repo.ChannelVoiceByIdempotencyKey(channelID, req.IdempotencyKey); findErr == nil {
		if existing.ProviderStatus != "active" && existing.ProviderStatus != "pending_activation" {
			return nil, BadAuthRequest("该克隆请求已存在但未确认成功，请先同步供应商音色或人工核对，禁止重复提交")
		}
		public, publicErr := publicChannelVoice(*existing, true, true)
		if publicErr != nil {
			return nil, publicErr
		}
		return &public, nil
	} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return nil, findErr
	}
	if _, findErr := s.repo.ChannelVoiceByKey(channelID, req.VoiceKey); findErr == nil {
		return nil, BadAuthRequest("同一渠道内音色标识不能重复")
	} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return nil, findErr
	}
	data, digest, err := readMiniMaxCloneAudio(file, filename)
	if err != nil {
		return nil, err
	}
	compatibleModelsJSON, err := json.Marshal(uniqueNonEmpty(req.CompatibleModels))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	voice := model.ChannelVoice{
		ID: newID(), ChannelID: channelID, VoiceKey: req.VoiceKey, DisplayName: req.DisplayName,
		Description: strings.TrimSpace(req.Description), Language: strings.TrimSpace(req.Language),
		Kind: "voice_cloning", AccessPolicy: normalizeModelAccessPolicy(req.AccessPolicy),
		CompatibleModelsJSON: string(compatibleModelsJSON), ProviderStatus: "creating", Enabled: false,
		SourceFilename: filepath.Base(filename), SourceSHA256: digest, SourceBytes: int64(len(data)),
		ConsentConfirmedAt: &now, IdempotencyKey: req.IdempotencyKey, CreatedAt: now, UpdatedAt: now,
	}
	attemptAudit, err := newAdminAuditEvent(actor, "channel_voice.clone_attempt", "channel_voice", voice.ID, "提交 MiniMax 音色克隆", map[string]any{
		"channelId": channelID, "voiceKey": voice.VoiceKey, "sourceFilename": voice.SourceFilename,
		"sourceBytes": voice.SourceBytes, "sourceSha256": voice.SourceSHA256, "consentConfirmedAt": voice.ConsentConfirmedAt,
	})
	if err != nil {
		return nil, err
	}
	if err := s.repo.SaveChannelVoiceWithAudit(&voice, attemptAudit); err != nil {
		if existing, findErr := s.repo.ChannelVoiceByIdempotencyKey(channelID, req.IdempotencyKey); findErr == nil {
			if existing.ProviderStatus != "active" && existing.ProviderStatus != "pending_activation" {
				return nil, BadAuthRequest("该克隆请求已存在但未确认成功，请先同步供应商音色或人工核对，禁止重复提交")
			}
			public, publicErr := publicChannelVoice(*existing, true, true)
			if publicErr != nil {
				return nil, publicErr
			}
			return &public, nil
		}
		return nil, err
	}
	fileID, uploadErr := uploadMiniMaxCloneAudio(ctx, *channel, data, filepath.Base(filename))
	if uploadErr != nil {
		voice.ProviderStatus = "failed"
		voice.LastError = truncateRunes(uploadErr.Error(), 500)
		voice.UpdatedAt = time.Now()
		_ = s.repo.Save(&voice)
		return nil, fmt.Errorf("上传 MiniMax 克隆音频失败：%w", uploadErr)
	}
	var response map[string]interface{}
	cloneErr := postJSON(ctx, providerConfig{BaseURL: channel.BaseURL, APIKey: channel.APIKey}, "/voice_clone", map[string]interface{}{
		"file_id": fileID, "voice_id": req.VoiceKey, "need_noise_reduction": false, "need_volume_normalization": false,
	}, &response)
	if cloneErr != nil {
		voice.ProviderStatus = miniMaxCloneFailureStatus(cloneErr)
		voice.LastError = truncateRunes(cloneErr.Error(), 500)
		voice.UpdatedAt = time.Now()
		_ = s.repo.Save(&voice)
		if voice.ProviderStatus == "failed" {
			return nil, fmt.Errorf("MiniMax 音色克隆被供应商明确拒绝：%w", cloneErr)
		}
		return nil, fmt.Errorf("MiniMax 音色克隆结果不确定，请先到供应商后台核对，禁止直接重试：%w", cloneErr)
	}
	voice.ProviderStatus = "pending_activation"
	voice.Enabled = true
	voice.LastError = ""
	voice.UpdatedAt = time.Now()
	audit, err := newAdminAuditEvent(actor, "channel_voice.clone", "channel_voice", voice.ID, "创建 MiniMax 克隆音色", map[string]any{
		"channelId": channelID, "voiceKey": voice.VoiceKey, "sourceFilename": voice.SourceFilename,
		"sourceBytes": voice.SourceBytes, "sourceSha256": voice.SourceSHA256, "consentConfirmedAt": voice.ConsentConfirmedAt,
	})
	if err != nil {
		return nil, err
	}
	if err := s.repo.SaveChannelVoiceWithAudit(&voice, audit); err != nil {
		return nil, err
	}
	public, err := publicChannelVoice(voice, true, true)
	if err != nil {
		return nil, err
	}
	return &public, nil
}

func (s *Service) DeleteAdminChannelVoice(ctx context.Context, actor *model.User, channelID string, id string) error {
	if err := s.RequireAdmin(actor); err != nil {
		return err
	}
	channel, err := s.requireMiniMaxSpeechChannel(channelID)
	if err != nil {
		return err
	}
	voice, err := s.repo.ChannelVoiceByID(channelID, id)
	if err != nil {
		return err
	}
	if (voice.Kind == "voice_cloning" || voice.Kind == "voice_generation") &&
		(voice.ProviderStatus == "active" || voice.ProviderStatus == "pending_activation" || voice.ProviderStatus == "uncertain") {
		var response map[string]interface{}
		if err := postJSON(ctx, providerConfig{BaseURL: channel.BaseURL, APIKey: channel.APIKey}, "/delete_voice", map[string]interface{}{
			"voice_type": voice.Kind, "voice_id": voice.VoiceKey,
		}, &response); err != nil {
			return fmt.Errorf("删除 MiniMax 上游音色失败，本地目录未变更：%w", err)
		}
	}
	audit, err := newAdminAuditEvent(actor, "channel_voice.delete", "channel_voice", voice.ID, "删除音色目录", map[string]any{"channelId": channelID, "voiceKey": voice.VoiceKey})
	if err != nil {
		return err
	}
	return s.repo.DeleteChannelVoice(channelID, voice.ID, audit, time.Now())
}

func miniMaxCloneFailureStatus(err error) string {
	var httpErr providerHTTPError
	if errors.As(err, &httpErr) && httpErr.StatusCode >= 400 && httpErr.StatusCode < 500 {
		return "failed"
	}
	if strings.Contains(err.Error(), "MiniMax 接口错误") {
		return "failed"
	}
	return "uncertain"
}

func (s *Service) requireMiniMaxSpeechChannel(channelID string) (*model.ModelChannel, error) {
	channel, err := s.repo.AdminSystemChannel(strings.TrimSpace(channelID))
	if err != nil {
		return nil, err
	}
	if channel.InterfaceType != model.ChannelInterfaceMiniMaxSpeech {
		return nil, BadAuthRequest("该操作仅支持 MiniMax Speech 渠道")
	}
	if strings.TrimSpace(channel.APIKey) == "" {
		return nil, BadAuthRequest("MiniMax Speech 渠道尚未配置 API Key")
	}
	return channel, nil
}

func (s *Service) validateAudioTaskVoice(userID string, config providerConfig) error {
	if config.InterfaceType != string(model.ChannelInterfaceMiniMaxSpeech) {
		return nil
	}
	voiceKey := strings.TrimSpace(config.AudioVoice)
	if voiceKey == "" {
		return BadAuthRequest("请选择后台启用的 MiniMax 音色")
	}
	voice, err := s.repo.ChannelVoiceByKey(config.ChannelID, voiceKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return BadAuthRequest("所选音色不存在或已被删除，请刷新音色列表后重试")
	}
	if err != nil {
		return err
	}
	if !voice.Enabled || (voice.ProviderStatus != "active" && voice.ProviderStatus != "pending_activation") {
		return BadAuthRequest("所选音色当前不可用，请联系管理员检查供应商状态")
	}
	if voice.AccessPolicy == model.ModelAccessMember {
		hasMembership, membershipErr := s.HasActiveMembership(userID)
		if membershipErr != nil {
			return membershipErr
		}
		if !hasMembership {
			return Forbidden("该音色仅限会员使用")
		}
	}
	var compatibleModels []string
	if err := json.Unmarshal([]byte(voice.CompatibleModelsJSON), &compatibleModels); err != nil {
		return fmt.Errorf("音色 %s 的兼容模型配置损坏：%w", voice.VoiceKey, err)
	}
	if len(compatibleModels) > 0 && !stringInSlice(config.Model, compatibleModels) {
		return BadAuthRequest("所选音色不支持当前音频模型")
	}
	return nil
}

func (s *Service) validateAudioTaskInput(userID string, input map[string]any) error {
	if strings.TrimSpace(stringField(input, "mode")) != "audio" {
		return nil
	}
	rawConfig, ok := input["config"].(map[string]any)
	if !ok {
		return BadAuthRequest("音频任务缺少模型配置")
	}
	encoded, err := json.Marshal(rawConfig)
	if err != nil {
		return BadAuthRequest("音频任务模型配置格式无效")
	}
	var config providerConfig
	if err := json.Unmarshal(encoded, &config); err != nil {
		return BadAuthRequest("音频任务模型配置格式无效")
	}
	resolved, err := s.resolveProviderConfig(config)
	if err != nil {
		return err
	}
	return s.validateAudioTaskVoice(userID, resolved)
}

func publicChannelVoices(voices []model.ChannelVoice, hasMembership bool, includeAdminDetails bool) ([]PublicChannelVoice, error) {
	result := make([]PublicChannelVoice, 0, len(voices))
	for _, voice := range voices {
		public, err := publicChannelVoice(voice, hasMembership, includeAdminDetails)
		if err != nil {
			return nil, err
		}
		result = append(result, public)
	}
	return result, nil
}

func publicChannelVoice(voice model.ChannelVoice, hasMembership bool, includeAdminDetails bool) (PublicChannelVoice, error) {
	var compatibleModels []string
	if err := json.Unmarshal([]byte(voice.CompatibleModelsJSON), &compatibleModels); err != nil {
		return PublicChannelVoice{}, fmt.Errorf("音色 %s 的兼容模型配置损坏：%w", voice.VoiceKey, err)
	}
	public := PublicChannelVoice{
		ID: voice.ID, VoiceKey: voice.VoiceKey, DisplayName: voice.DisplayName, Description: voice.Description,
		Language: miniMaxVoiceLanguage(voice.VoiceKey, voice.Language), Kind: voice.Kind, AccessPolicy: voice.AccessPolicy,
		Accessible:       voice.AccessPolicy == model.ModelAccessAuthenticated || hasMembership,
		CompatibleModels: compatibleModels, ProviderStatus: voice.ProviderStatus, Enabled: voice.Enabled,
	}
	if includeAdminDetails {
		public.LastError = voice.LastError
	}
	return public, nil
}

func normalizeChannelVoiceKind(value string) string {
	switch strings.TrimSpace(value) {
	case "system", "voice_cloning", "voice_generation":
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

func normalizeModelAccessPolicy(value model.ModelAccessPolicy) model.ModelAccessPolicy {
	if value == model.ModelAccessMember {
		return model.ModelAccessMember
	}
	return model.ModelAccessAuthenticated
}

func miniMaxVoicesFromResponse(channelID string, response map[string]interface{}, now time.Time) ([]model.ChannelVoice, error) {
	groups := []struct {
		Key  string
		Kind string
	}{{"system_voice", "system"}, {"voice_cloning", "voice_cloning"}, {"voice_generation", "voice_generation"}}
	result := make([]model.ChannelVoice, 0)
	for _, group := range groups {
		rawValues, exists := response[group.Key]
		if !exists {
			return nil, fmt.Errorf("响应缺少 %s 字段", group.Key)
		}
		values, ok := rawValues.([]interface{})
		if !ok {
			return nil, fmt.Errorf("%s 字段不是数组", group.Key)
		}
		for index, value := range values {
			item, ok := value.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("%s[%d] 不是对象", group.Key, index)
			}
			voiceKey := firstNonEmptyString(stringField(item, "voice_id"), stringField(item, "voiceId"))
			if voiceKey == "" {
				return nil, fmt.Errorf("%s[%d] 缺少 voice_id", group.Key, index)
			}
			description, err := miniMaxVoiceDescription(item["description"])
			if err != nil {
				return nil, fmt.Errorf("%s[%d] 的 description 无效：%w", group.Key, index, err)
			}
			displayName := firstNonEmptyString(stringField(item, "voice_name"), stringField(item, "voiceName"), voiceKey)
			result = append(result, model.ChannelVoice{
				ID: newID(), ChannelID: channelID, VoiceKey: voiceKey, DisplayName: displayName,
				Description: firstNonEmptyString(description, stringField(item, "desc")),
				Language:    miniMaxVoiceLanguage(voiceKey, firstNonEmptyString(stringField(item, "language"), stringField(item, "language_type"))),
				Kind:        group.Kind, AccessPolicy: model.ModelAccessAuthenticated, CompatibleModelsJSON: "[]",
				ProviderStatus: "active", Enabled: true, CreatedAt: now, UpdatedAt: now,
			})
		}
	}
	return result, nil
}

func miniMaxVoiceLanguage(voiceKey string, providerLanguage string) string {
	if value := strings.TrimSpace(providerLanguage); value != "" {
		if _, ok := miniMaxLanguageBoosts[value]; ok && value != "auto" {
			return value
		}
	}
	normalized := strings.TrimSpace(voiceKey)
	switch {
	case strings.HasPrefix(normalized, "Chinese (Mandarin)_"):
		return "Chinese"
	case strings.HasPrefix(normalized, "Cantonese_"):
		return "Chinese,Yue"
	}
	prefix, _, found := strings.Cut(normalized, "_")
	if !found {
		return ""
	}
	if _, ok := miniMaxLanguageBoosts[prefix]; ok && prefix != "auto" {
		return prefix
	}
	return ""
}

func miniMaxVoiceDescription(value interface{}) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return strings.TrimSpace(typed), nil
	case []interface{}:
		parts := make([]string, 0, len(typed))
		for index, item := range typed {
			text, ok := item.(string)
			if !ok {
				return "", fmt.Errorf("第 %d 项不是字符串", index)
			}
			if trimmed := strings.TrimSpace(text); trimmed != "" {
				parts = append(parts, trimmed)
			}
		}
		return strings.Join(parts, "；"), nil
	default:
		return "", fmt.Errorf("应为字符串或字符串数组")
	}
}

func readMiniMaxCloneAudio(file multipart.File, filename string) ([]byte, string, error) {
	extension := strings.ToLower(filepath.Ext(filename))
	if extension != ".mp3" && extension != ".m4a" && extension != ".wav" {
		return nil, "", BadAuthRequest("克隆音频仅支持 MP3、M4A 或 WAV")
	}
	data, err := io.ReadAll(io.LimitReader(file, miniMaxCloneAudioLimit+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 {
		return nil, "", BadAuthRequest("克隆音频不能为空")
	}
	if int64(len(data)) > miniMaxCloneAudioLimit {
		return nil, "", BadAuthRequest("克隆音频不能超过 20MB")
	}
	digest := sha256.Sum256(data)
	return data, hex.EncodeToString(digest[:]), nil
}

func uploadMiniMaxCloneAudio(ctx context.Context, channel model.ModelChannel, data []byte, filename string) (int64, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("purpose", "voice_clone"); err != nil {
		return 0, err
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return 0, err
	}
	if _, err := part.Write(data); err != nil {
		return 0, err
	}
	if err := writer.Close(); err != nil {
		return 0, err
	}
	var response map[string]interface{}
	if err := postForm(ctx, providerConfig{BaseURL: channel.BaseURL, APIKey: channel.APIKey}, "/files/upload", writer.FormDataContentType(), body, &response); err != nil {
		return 0, err
	}
	file, _ := response["file"].(map[string]interface{})
	fileID, ok := numberField(file, "file_id")
	if !ok || fileID <= 0 {
		return 0, errors.New("MiniMax 文件上传成功响应缺少 file_id")
	}
	return int64(fileID), nil
}

func numberField(value map[string]interface{}, key string) (float64, bool) {
	if value == nil {
		return 0, false
	}
	number, ok := value[key].(float64)
	return number, ok
}
