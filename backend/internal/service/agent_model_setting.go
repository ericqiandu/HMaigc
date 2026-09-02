package service

import (
	"encoding/json"
	"errors"
	"strings"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

const (
	agentDefaultModelSettingKey       = "agent_default_model"
	agentDefaultVisionModelSettingKey = "agent_default_vision_model"
)

type agentDefaultModelSettingValue struct {
	ChannelModelID string `json:"channelModelId"`
}

type UpdateAgentDefaultModelRequest struct {
	ChannelModelID string `json:"channelModelId"`
}

type UpdateAgentDefaultVisionModelRequest struct {
	ChannelModelID string `json:"channelModelId"`
}

type AgentDefaultModelSetting struct {
	Configured     bool   `json:"configured"`
	ChannelModelID string `json:"channelModelId"`
	ChannelID      string `json:"channelId"`
	ModelKey       string `json:"modelKey"`
	DisplayName    string `json:"displayName"`
}

type PublicAgentDefaultModel struct {
	ChannelModelID string `json:"channelModelId"`
	ChannelID      string `json:"channelId"`
	ModelKey       string `json:"modelKey"`
}

func (s *Service) AdminAgentDefaultModelSetting(actor *model.User) (AgentDefaultModelSetting, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return AgentDefaultModelSetting{}, err
	}
	return s.agentDefaultModelSetting()
}

func (s *Service) AdminAgentDefaultVisionModelSetting(actor *model.User) (AgentDefaultModelSetting, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return AgentDefaultModelSetting{}, err
	}
	return s.agentDefaultVisionModelSetting()
}

func (s *Service) UpdateAgentDefaultModelSetting(actor *model.User, request UpdateAgentDefaultModelRequest) (AgentDefaultModelSetting, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return AgentDefaultModelSetting{}, err
	}
	item, channel, err := s.eligibleAgentDefaultModel(strings.TrimSpace(request.ChannelModelID))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, errAgentDefaultModelIneligible) {
			return AgentDefaultModelSetting{}, BadAuthRequest("请选择已启用、已定价且支持 Agent 决策协议的系统文本模型")
		}
		return AgentDefaultModelSetting{}, err
	}
	encoded, err := json.Marshal(agentDefaultModelSettingValue{ChannelModelID: item.ID})
	if err != nil {
		return AgentDefaultModelSetting{}, err
	}
	setting := model.SystemSetting{Key: agentDefaultModelSettingKey, ValueJSON: string(encoded), UpdatedBy: actor.ID}
	current, err := s.repo.SystemSetting(agentDefaultModelSettingKey)
	if err == nil {
		setting.CreatedAt = current.CreatedAt
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return AgentDefaultModelSetting{}, err
	}
	audit, err := newAdminAuditEvent(actor, "agent_default_model.update", "system_setting", agentDefaultModelSettingKey, "更新全站 Agent 默认模型", map[string]string{
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

func (s *Service) UpdateAgentDefaultVisionModelSetting(actor *model.User, request UpdateAgentDefaultVisionModelRequest) (AgentDefaultModelSetting, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return AgentDefaultModelSetting{}, err
	}
	item, channel, err := s.eligibleAgentDefaultVisionModel(strings.TrimSpace(request.ChannelModelID))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, errAgentDefaultVisionModelIneligible) {
			return AgentDefaultModelSetting{}, BadAuthRequest("请选择已启用、已完整定价且凭据健康的视觉理解模型")
		}
		return AgentDefaultModelSetting{}, err
	}
	encoded, err := json.Marshal(agentDefaultModelSettingValue{ChannelModelID: item.ID})
	if err != nil {
		return AgentDefaultModelSetting{}, err
	}
	setting := model.SystemSetting{Key: agentDefaultVisionModelSettingKey, ValueJSON: string(encoded), UpdatedBy: actor.ID}
	current, err := s.repo.SystemSetting(agentDefaultVisionModelSettingKey)
	if err == nil {
		setting.CreatedAt = current.CreatedAt
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return AgentDefaultModelSetting{}, err
	}
	audit, err := newAdminAuditEvent(actor, "agent_default_vision_model.update", "system_setting", agentDefaultVisionModelSettingKey, "更新全站 Agent 默认视觉理解模型", map[string]string{
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

func (s *Service) PublicAgentDefaultModel() (*PublicAgentDefaultModel, error) {
	setting, err := s.agentDefaultModelSetting()
	if err != nil {
		return nil, err
	}
	if !setting.Configured {
		return nil, nil
	}
	item, err := s.repo.ChannelModelByRecordID(setting.ChannelModelID)
	if err != nil {
		return nil, err
	}
	callable, err := s.publiclyCallableChannelModels([]model.ChannelModel{*item})
	if err != nil {
		return nil, err
	}
	if len(callable) != 1 {
		return nil, nil
	}
	return &PublicAgentDefaultModel{ChannelModelID: setting.ChannelModelID, ChannelID: setting.ChannelID, ModelKey: setting.ModelKey}, nil
}

func (s *Service) agentDefaultModelSetting() (AgentDefaultModelSetting, error) {
	stored, err := s.repo.SystemSetting(agentDefaultModelSettingKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AgentDefaultModelSetting{}, nil
	}
	if err != nil {
		return AgentDefaultModelSetting{}, err
	}
	value, err := parseAgentDefaultModelSetting(stored)
	if err != nil {
		return AgentDefaultModelSetting{}, err
	}
	item, channel, err := s.eligibleAgentDefaultModel(value.ChannelModelID)
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, errAgentDefaultModelIneligible) {
		return AgentDefaultModelSetting{}, nil
	}
	if err != nil {
		return AgentDefaultModelSetting{}, err
	}
	return agentDefaultModelView(*item, *channel), nil
}

func (s *Service) agentDefaultVisionModelSetting() (AgentDefaultModelSetting, error) {
	stored, err := s.repo.SystemSetting(agentDefaultVisionModelSettingKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AgentDefaultModelSetting{}, nil
	}
	if err != nil {
		return AgentDefaultModelSetting{}, err
	}
	value, err := parseAgentDefaultModelSetting(stored)
	if err != nil {
		return AgentDefaultModelSetting{}, err
	}
	item, channel, err := s.eligibleAgentDefaultVisionModel(value.ChannelModelID)
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, errAgentDefaultVisionModelIneligible) {
		return AgentDefaultModelSetting{}, nil
	}
	if err != nil {
		return AgentDefaultModelSetting{}, err
	}
	return agentDefaultModelView(*item, *channel), nil
}

func (s *Service) agentRuntimeDefaultVisionModel() (*model.ChannelModel, *model.ModelChannel, error) {
	stored, err := s.repo.SystemSetting(agentDefaultVisionModelSettingKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, ServiceUnavailable("管理员尚未配置可用的 Agent 视觉理解模型")
	}
	if err != nil {
		return nil, nil, err
	}
	value, err := parseAgentDefaultModelSetting(stored)
	if err != nil {
		return nil, nil, err
	}
	item, channel, err := s.eligibleAgentDefaultVisionModel(value.ChannelModelID)
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, errAgentDefaultVisionModelIneligible) {
		return nil, nil, ServiceUnavailable("当前 Agent 视觉理解模型不可执行")
	}
	if err != nil {
		return nil, nil, err
	}
	return item, channel, nil
}

func parseAgentDefaultModelSetting(setting *model.SystemSetting) (agentDefaultModelSettingValue, error) {
	if setting == nil {
		return agentDefaultModelSettingValue{}, errors.New("Agent 默认模型配置缺失")
	}
	var value agentDefaultModelSettingValue
	if err := json.Unmarshal([]byte(setting.ValueJSON), &value); err != nil {
		return agentDefaultModelSettingValue{}, errors.New("Agent 默认模型配置格式无效")
	}
	value.ChannelModelID = strings.TrimSpace(value.ChannelModelID)
	if value.ChannelModelID == "" {
		return agentDefaultModelSettingValue{}, errors.New("Agent 默认模型配置缺少模型记录")
	}
	return value, nil
}

var errAgentDefaultModelIneligible = errors.New("Agent default model is ineligible")
var errAgentDefaultVisionModelIneligible = errors.New("Agent default vision model is ineligible")

func (s *Service) eligibleAgentDefaultModel(id string) (*model.ChannelModel, *model.ModelChannel, error) {
	if id == "" {
		return nil, nil, errAgentDefaultModelIneligible
	}
	item, err := s.repo.ChannelModelByRecordID(id)
	if err != nil {
		return nil, nil, err
	}
	channel, err := s.repo.SystemChannel(item.ChannelID)
	if err != nil {
		return nil, nil, err
	}
	if !item.Enabled || !item.PriceConfigured || item.AccessPolicy != model.ModelAccessAuthenticated || normalizeCapability(item.Capability) != "text" {
		return nil, nil, errAgentDefaultModelIneligible
	}
	if !supportsAgentDecisionInterface(channel.InterfaceType) {
		return nil, nil, errAgentDefaultModelIneligible
	}
	if item.BillingMode == "token_usage" {
		pricing, pricingErr := s.repo.ModelPricing(item.ChannelID, item.ModelKey, "text")
		if pricingErr != nil {
			if errors.Is(pricingErr, gorm.ErrRecordNotFound) {
				return nil, nil, errAgentDefaultModelIneligible
			}
			return nil, nil, pricingErr
		}
		if _, _, pricingErr = tokenBillingContract(*channel, *item, pricing); pricingErr != nil {
			return nil, nil, errAgentDefaultModelIneligible
		}
		return item, channel, nil
	}
	if item.BillingMode != "fixed_request" || item.PriceStrategy != "flat" || item.UnitPriceMicrocredits <= 0 {
		return nil, nil, errAgentDefaultModelIneligible
	}
	return item, channel, nil
}

func supportsAgentDecisionInterface(interfaceType model.ChannelInterfaceType) bool {
	return interfaceType == model.ChannelInterfaceOpenAIResponse || interfaceType == model.ChannelInterfaceChatCompletion
}

func (s *Service) eligibleAgentDefaultVisionModel(id string) (*model.ChannelModel, *model.ModelChannel, error) {
	if strings.TrimSpace(id) == "" {
		return nil, nil, errAgentDefaultVisionModelIneligible
	}
	item, err := s.repo.ChannelModelByRecordID(strings.TrimSpace(id))
	if err != nil {
		return nil, nil, err
	}
	channel, err := s.repo.SystemChannel(item.ChannelID)
	if err != nil {
		return nil, nil, err
	}
	if !channel.Enabled || !item.Enabled || !item.PriceConfigured || item.AccessPolicy != model.ModelAccessAuthenticated || normalizeCapability(item.Capability) != "vision" || !supportsAgentDecisionInterface(channel.InterfaceType) {
		return nil, nil, errAgentDefaultVisionModelIneligible
	}
	pricing, err := s.repo.ModelPricing(item.ChannelID, item.ModelKey, "vision")
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, errAgentDefaultVisionModelIneligible
		}
		return nil, nil, err
	}
	if _, _, err := tokenBillingContract(*channel, *item, pricing); err != nil {
		return nil, nil, errAgentDefaultVisionModelIneligible
	}
	callable, err := s.publiclyCallableChannelModels([]model.ChannelModel{*item})
	if err != nil {
		return nil, nil, err
	}
	if len(callable) != 1 {
		return nil, nil, errAgentDefaultVisionModelIneligible
	}
	if item.ProviderCredentialID == "" && (strings.TrimSpace(channel.BaseURL) == "" || strings.TrimSpace(channel.APIKey) == "") {
		return nil, nil, errAgentDefaultVisionModelIneligible
	}
	return item, channel, nil
}

func agentDefaultModelView(item model.ChannelModel, channel model.ModelChannel) AgentDefaultModelSetting {
	return AgentDefaultModelSetting{Configured: true, ChannelModelID: item.ID, ChannelID: channel.ID, ModelKey: item.ModelKey, DisplayName: item.DisplayName}
}
