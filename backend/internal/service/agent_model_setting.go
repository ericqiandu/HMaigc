package service

import (
	"encoding/json"
	"errors"
	"strings"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

const agentDefaultModelSettingKey = "agent_default_model"

type agentDefaultModelSettingValue struct {
	ChannelModelID string `json:"channelModelId"`
}

type UpdateAgentDefaultModelRequest struct {
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

func (s *Service) UpdateAgentDefaultModelSetting(actor *model.User, request UpdateAgentDefaultModelRequest) (AgentDefaultModelSetting, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return AgentDefaultModelSetting{}, err
	}
	item, channel, err := s.eligibleAgentDefaultModel(strings.TrimSpace(request.ChannelModelID))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, errAgentDefaultModelIneligible) {
			return AgentDefaultModelSetting{}, BadAuthRequest("请选择已启用、已定价的系统文本模型")
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

func (s *Service) PublicAgentDefaultModel() (*PublicAgentDefaultModel, error) {
	setting, err := s.agentDefaultModelSetting()
	if err != nil {
		return nil, err
	}
	if !setting.Configured {
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
	if !item.Enabled || !item.PriceConfigured || item.AccessPolicy != model.ModelAccessAuthenticated || normalizeCapability(item.Capability) != "text" || item.BillingMode != "fixed_request" || item.PriceStrategy != "flat" || item.UnitPriceMicrocredits <= 0 {
		return nil, nil, errAgentDefaultModelIneligible
	}
	return item, channel, nil
}

func agentDefaultModelView(item model.ChannelModel, channel model.ModelChannel) AgentDefaultModelSetting {
	return AgentDefaultModelSetting{Configured: true, ChannelModelID: item.ID, ChannelID: channel.ID, ModelKey: item.ModelKey, DisplayName: item.DisplayName}
}
