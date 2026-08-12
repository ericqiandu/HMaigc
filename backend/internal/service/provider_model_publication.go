package service

import (
	"encoding/json"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
)

func (s *Service) PublishKuaiziFamilyModels(actor *model.User, family string) (*AdminProviderAccountView, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	registry, err := NewProviderRegistry(kuaiziProviderAdapterDescriptors())
	if err != nil {
		return nil, err
	}
	descriptor, ok := registry.Descriptor(kuaiziProviderKind, strings.TrimSpace(family))
	if !ok {
		return nil, BadAuthRequest("模型系列未登记")
	}
	account, err := s.repo.ProviderAccountByKind(kuaiziProviderKind)
	if err != nil {
		return nil, err
	}
	if !account.Enabled {
		return nil, BadAuthRequest("筷子科技账号未启用")
	}
	credential, err := s.repo.ProviderCredentialByFamily(account.ID, descriptor.Family)
	if err != nil {
		return nil, err
	}
	if !credential.Enabled || credential.HealthStatus != "healthy" {
		return nil, BadAuthRequest("模型系列凭据尚未验证健康")
	}
	versions, err := s.repo.ProviderEndpointVersions(account.ID)
	if err != nil {
		return nil, err
	}
	endpoint := activeEndpointVersion(versions)
	if endpoint == nil {
		return nil, BadAuthRequest("筷子科技服务地址尚未激活")
	}
	channels, _, err := s.repo.AdminSystemChannels("", string(model.ChannelInterfaceAIOpenVideoVolcengine), "enabled", 100, 0)
	if err != nil {
		return nil, err
	}
	matching := make([]model.ModelChannel, 0, 1)
	for _, channel := range channels {
		if strings.TrimRight(strings.TrimSpace(channel.BaseURL), "/") == strings.TrimRight(endpoint.BaseURL, "/") {
			matching = append(matching, channel)
		}
	}
	if len(matching) != 1 {
		return nil, BadAuthRequest("筷子兼容渠道必须且只能配置一个")
	}
	now := time.Now()
	items := make([]model.ChannelModel, 0, len(descriptor.Models))
	modelKeys := make([]string, 0, len(descriptor.Models))
	for _, spec := range descriptor.Models {
		modelKeys = append(modelKeys, spec.ModelKey)
		items = append(items, model.ChannelModel{ID: newID(), ChannelID: matching[0].ID, ProviderCredentialID: credential.ID, ModelKey: spec.ModelKey, DisplayName: spec.DisplayName, BrandKey: model.InferModelBrandKey(spec.ModelKey), AccessPolicy: model.ModelAccessAuthenticated, Capability: spec.Capability, BillingMode: "fixed_request", PriceStrategy: "flat", PriceVersion: 1, CreatedAt: now, UpdatedAt: now})
	}
	modelsJSON, err := json.Marshal(modelKeys)
	if err != nil {
		return nil, err
	}
	audit, err := newAdminAuditEvent(actor, "provider.models.publish", "provider_credential", credential.ID, "发布筷子科技模型并绑定系列凭据", map[string]any{"family": descriptor.Family, "channelId": matching[0].ID, "models": modelKeys})
	if err != nil {
		return nil, err
	}
	if err := s.repo.PublishProviderChannelModels(matching[0].ID, credential.ID, items, string(modelsJSON), audit); err != nil {
		return nil, err
	}
	return s.adminKuaiziProviderView()
}
