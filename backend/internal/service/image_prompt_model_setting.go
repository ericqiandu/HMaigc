package service

import (
	"encoding/json"
	"errors"
	"strings"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

const imagePromptModelSettingKey = "image_prompt_specialist_model"

var errImagePromptModelIneligible = errors.New("image prompt model is ineligible")

type UpdateImagePromptModelRequest struct {
	ChannelModelID string `json:"channelModelId"`
}

func (s *Service) AdminImagePromptModelSetting(actor *model.User) (AgentDefaultModelSetting, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return AgentDefaultModelSetting{}, err
	}
	return s.imagePromptModelSetting()
}

func (s *Service) UpdateImagePromptModelSetting(actor *model.User, request UpdateImagePromptModelRequest) (AgentDefaultModelSetting, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return AgentDefaultModelSetting{}, err
	}
	item, channel, _, err := s.eligibleImagePromptModel(strings.TrimSpace(request.ChannelModelID))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, errImagePromptModelIneligible) {
			return AgentDefaultModelSetting{}, BadAuthRequest("请选择已启用、已定价且使用健康版本化凭据的系统文本模型")
		}
		return AgentDefaultModelSetting{}, err
	}
	encoded, err := json.Marshal(agentDefaultModelSettingValue{ChannelModelID: item.ID})
	if err != nil {
		return AgentDefaultModelSetting{}, err
	}
	setting := model.SystemSetting{Key: imagePromptModelSettingKey, ValueJSON: string(encoded), UpdatedBy: actor.ID}
	current, err := s.repo.SystemSetting(imagePromptModelSettingKey)
	if err == nil {
		setting.CreatedAt = current.CreatedAt
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return AgentDefaultModelSetting{}, err
	}
	audit, err := newAdminAuditEvent(actor, "image_prompt_specialist_model.update", "system_setting", imagePromptModelSettingKey, "更新图片提示词 Specialist 模型", map[string]string{
		"channelModelId": item.ID, "channelId": channel.ID, "modelKey": item.ModelKey,
	})
	if err != nil {
		return AgentDefaultModelSetting{}, err
	}
	if err := s.repo.SaveSystemSettingWithAudit(&setting, audit); err != nil {
		return AgentDefaultModelSetting{}, err
	}
	return agentDefaultModelView(*item, *channel), nil
}

func (s *Service) imagePromptModelSetting() (AgentDefaultModelSetting, error) {
	stored, err := s.repo.SystemSetting(imagePromptModelSettingKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AgentDefaultModelSetting{}, nil
	}
	if err != nil {
		return AgentDefaultModelSetting{}, err
	}
	value, err := parseImagePromptModelSetting(stored)
	if err != nil {
		return AgentDefaultModelSetting{}, err
	}
	item, channel, _, err := s.eligibleImagePromptModel(value.ChannelModelID)
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, errImagePromptModelIneligible) {
		return AgentDefaultModelSetting{}, ServiceUnavailable("已配置的图片提示词模型不存在或不可执行")
	}
	if err != nil {
		return AgentDefaultModelSetting{}, err
	}
	return agentDefaultModelView(*item, *channel), nil
}

func (s *Service) eligibleImagePromptModel(id string) (*model.ChannelModel, *model.ModelChannel, SystemProxyRuntime, error) {
	item, channel, err := s.eligibleAgentDefaultModel(strings.TrimSpace(id))
	if err != nil {
		if errors.Is(err, errAgentDefaultModelIneligible) {
			return nil, nil, SystemProxyRuntime{}, errImagePromptModelIneligible
		}
		return nil, nil, SystemProxyRuntime{}, err
	}
	if strings.TrimSpace(item.ProviderCredentialID) == "" {
		return nil, nil, SystemProxyRuntime{}, errImagePromptModelIneligible
	}
	family, spec, managed := kuaiziProviderFamilyForModel(item.ModelKey)
	if !managed || spec.Capability != "text" || channel.ID != deterministicKuaiziChatChannelID(family) {
		return nil, nil, SystemProxyRuntime{}, errImagePromptModelIneligible
	}
	runtime, err := s.resolveVersionedSystemProxyRuntime(channel, item.ModelKey)
	if err != nil {
		return nil, nil, SystemProxyRuntime{}, err
	}
	if !boundedTrimmed(runtime.ProviderEndpointVersionID, 160) || !boundedTrimmed(runtime.ProviderCredentialVersionID, 160) {
		return nil, nil, SystemProxyRuntime{}, errImagePromptModelIneligible
	}
	return item, channel, runtime, nil
}

func parseImagePromptModelSetting(setting *model.SystemSetting) (agentDefaultModelSettingValue, error) {
	if setting == nil {
		return agentDefaultModelSettingValue{}, errors.New("图片提示词模型配置缺失")
	}
	var value agentDefaultModelSettingValue
	if err := json.Unmarshal([]byte(setting.ValueJSON), &value); err != nil {
		return agentDefaultModelSettingValue{}, errors.New("图片提示词模型配置格式无效")
	}
	value.ChannelModelID = strings.TrimSpace(value.ChannelModelID)
	if value.ChannelModelID == "" {
		return agentDefaultModelSettingValue{}, errors.New("图片提示词模型配置缺少模型记录")
	}
	return value, nil
}
