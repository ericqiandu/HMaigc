package service

import (
	"crypto/sha256"
	"encoding/hex"
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
	now := time.Now()
	channel, managed, err := s.kuaiziPublicationChannel(descriptor, *endpoint, now)
	if err != nil {
		return nil, err
	}
	if managed != nil {
		channel.ConcurrencyLimit = credential.ConcurrencyLimit
		managed.ConcurrencyLimit = credential.ConcurrencyLimit
	}
	items := make([]model.ChannelModel, 0, len(descriptor.Models))
	modelKeys := make([]string, 0, len(descriptor.Models))
	for _, spec := range descriptor.Models {
		modelKeys = append(modelKeys, spec.ModelKey)
		items = append(items, model.ChannelModel{ID: newID(), ChannelID: channel.ID, ProviderCredentialID: credential.ID, ModelKey: spec.ModelKey, DisplayName: spec.DisplayName, MarketingCopy: spec.MarketingCopy, BrandKey: model.InferModelBrandKey(spec.ModelKey), AccessPolicy: model.ModelAccessAuthenticated, Capability: spec.Capability, BillingMode: "fixed_request", PriceStrategy: "flat", PriceVersion: 1, CreatedAt: now, UpdatedAt: now})
	}
	modelsJSON, err := json.Marshal(modelKeys)
	if err != nil {
		return nil, err
	}
	audit, err := newAdminAuditEvent(actor, "provider.models.publish", "provider_credential", credential.ID, "发布筷子科技模型并绑定系列凭据", map[string]any{"family": descriptor.Family, "channelId": channel.ID, "models": modelKeys})
	if err != nil {
		return nil, err
	}
	if err := s.repo.PublishProviderManagedChannelModels(managed, channel.ID, credential.ID, items, string(modelsJSON), audit); err != nil {
		return nil, err
	}
	return s.adminKuaiziProviderView()
}

func (s *Service) kuaiziPublicationChannel(descriptor ProviderAdapterDescriptor, endpoint model.ProviderEndpointVersion, now time.Time) (model.ModelChannel, *model.ModelChannel, error) {
	if len(descriptor.Models) > 0 && descriptor.Models[0].Capability == "text" {
		channel := model.ModelChannel{
			ID: deterministicKuaiziChatChannelID(descriptor.Family), Scope: model.ChannelScopeSystem, Enabled: true,
			Name: "筷子科技 · " + descriptor.Models[0].DisplayName, BaseURL: kuaiziChatCompletionsBaseURL(endpoint.BaseURL),
			APIFormat: "openai", InterfaceType: model.ChannelInterfaceChatCompletion, CreatedAt: now, UpdatedAt: now,
		}
		return channel, &channel, nil
	}
	channels, _, err := s.repo.AdminSystemChannels("", string(model.ChannelInterfaceAIOpenVideoVolcengine), "enabled", 100, 0)
	if err != nil {
		return model.ModelChannel{}, nil, err
	}
	matching := make([]model.ModelChannel, 0, 1)
	for _, channel := range channels {
		if strings.TrimRight(strings.TrimSpace(channel.BaseURL), "/") == strings.TrimRight(endpoint.BaseURL, "/") {
			matching = append(matching, channel)
		}
	}
	if len(matching) != 1 {
		return model.ModelChannel{}, nil, BadAuthRequest("筷子兼容渠道必须且只能配置一个")
	}
	return matching[0], nil, nil
}

func deterministicKuaiziChatChannelID(family string) string {
	sum := sha256.Sum256([]byte(kuaiziProviderKind + "\nchat\n" + strings.TrimSpace(family)))
	return "mc-" + hex.EncodeToString(sum[:16])
}

func isKuaiziChatChannelID(channelID string) bool {
	for _, descriptor := range kuaiziProviderAdapterDescriptors() {
		for _, spec := range descriptor.Models {
			if spec.Capability == "text" && channelID == deterministicKuaiziChatChannelID(descriptor.Family) {
				return true
			}
		}
	}
	return false
}

func kuaiziChatCompletionsBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(baseURL, "/ai-open-platform-api/v1") {
		return baseURL
	}
	return baseURL + "/ai-open-platform-api/v1"
}
