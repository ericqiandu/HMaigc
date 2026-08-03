package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

type ChannelModelRequest struct {
	ModelKey                 string                         `json:"modelKey"`
	DisplayName              string                         `json:"displayName"`
	MarketingCopy            string                         `json:"marketingCopy"`
	PromotionBadge           string                         `json:"promotionBadge"`
	EstimatedDurationSeconds int                            `json:"estimatedDurationSeconds"`
	BrandKey                 string                         `json:"brandKey"`
	AccessPolicy             model.ModelAccessPolicy        `json:"accessPolicy"`
	Capability               string                         `json:"capability"`
	BillingMode              string                         `json:"billingMode"`
	PriceStrategy            string                         `json:"priceStrategy"`
	UnitPriceMicrocredits    int64                          `json:"unitPriceMicrocredits"`
	PriceTiers               []ChannelModelPriceTierRequest `json:"priceTiers"`
	PriceConfigured          bool                           `json:"priceConfigured"`
	Enabled                  *bool                          `json:"enabled"`
}

type ChannelModelPriceTierRequest struct {
	Resolution            string `json:"resolution"`
	UnitPriceMicrocredits int64  `json:"unitPriceMicrocredits"`
}

// AdminChannelModelFetchResult 是管理员从上游拉目录后的汇总：models 为去重后的标识，added 为本次新建条数。
type AdminChannelModelFetchResult struct {
	Models []string `json:"models"`
	Added  int64    `json:"added"`
}

func (s *Service) EnsureSystemChannelModels() error {
	channels, err := s.repo.SystemChannels(true)
	if err != nil {
		return err
	}
	for index := range channels {
		items, err := s.repo.ChannelModels(channels[index].ID, true)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			if err := s.syncInitialChannelModels(&channels[index], channelModelNames(channels[index])); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) AdminChannelModels(actor *model.User, channelID string) ([]model.ChannelModel, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if _, err := s.repo.AdminSystemChannel(channelID); err != nil {
		return nil, err
	}
	return s.ensureChannelModels(channelID, true)
}

func (s *Service) FetchAdminChannelModels(ctx context.Context, actor *model.User, channelID string) (*AdminChannelModelFetchResult, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	channel, err := s.repo.AdminSystemChannel(channelID)
	if err != nil {
		return nil, err
	}
	// 使用服务端保存的渠道密钥请求上游，避免密钥为了拉目录再次经过浏览器。
	models, err := s.FetchChannelModels(ctx, actor, ChannelModelsRequest{BaseURL: channel.BaseURL, APIKey: channel.APIKey, APIFormat: channel.APIFormat})
	if err != nil {
		return nil, err
	}
	// 删除项作为 tombstone 参与去重，避免再次拉取上游目录时自动恢复到页面。
	existing, err := s.repo.ChannelModelsIncludingDeleted(channelID)
	if err != nil {
		return nil, err
	}
	known := make(map[string]struct{}, len(existing))
	for _, item := range existing {
		known[item.ModelKey] = struct{}{}
	}
	missing := make([]model.ChannelModel, 0, len(models))
	for _, name := range models {
		if _, ok := known[name]; ok {
			continue
		}
		// 自动发现不能绕过定价边界；新模型由管理员定价后再手动启用。
		missing = append(missing, model.ChannelModel{ID: newID(), ChannelID: channelID, ModelKey: name, DisplayName: name, BrandKey: model.InferModelBrandKey(name), AccessPolicy: model.ModelAccessAuthenticated, Capability: capabilityForChannel(*channel), BillingMode: "fixed_request", PriceStrategy: "flat", Enabled: false, PriceVersion: 1})
	}
	added, err := s.repo.CreateMissingChannelModels(missing)
	if err != nil {
		return nil, err
	}
	return &AdminChannelModelFetchResult{Models: models, Added: added}, nil
}

func (s *Service) SaveAdminChannelModel(actor *model.User, channelID string, id string, req ChannelModelRequest) (*model.ChannelModel, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	channel, err := s.repo.AdminSystemChannel(channelID)
	if err != nil {
		return nil, err
	}
	modelKey := strings.TrimPrefix(strings.TrimSpace(req.ModelKey), "models/")
	if modelKey == "" {
		return nil, BadAuthRequest("请填写模型标识")
	}
	marketingCopy := strings.TrimSpace(req.MarketingCopy)
	if len([]rune(marketingCopy)) > 120 {
		return nil, BadAuthRequest("模型推广文案不能超过 120 个字符")
	}
	if strings.IndexFunc(marketingCopy, unicode.IsControl) >= 0 {
		return nil, BadAuthRequest("模型推广文案不能包含换行或控制字符")
	}
	promotionBadge := strings.TrimSpace(req.PromotionBadge)
	if len([]rune(promotionBadge)) > 12 {
		return nil, BadAuthRequest("模型促销角标不能超过 12 个字符")
	}
	if strings.IndexFunc(promotionBadge, unicode.IsControl) >= 0 {
		return nil, BadAuthRequest("模型促销角标不能包含换行或控制字符")
	}
	if req.EstimatedDurationSeconds < 0 || req.EstimatedDurationSeconds > 86_400 {
		return nil, BadAuthRequest("预计生成耗时必须在 0 到 86400 秒之间")
	}
	brandKey := strings.TrimSpace(req.BrandKey)
	if !model.IsModelBrandKey(brandKey) {
		return nil, BadAuthRequest("请选择有效的系统模型品牌")
	}
	capability := normalizeCapability(req.Capability)
	if capability == "" {
		capability = capabilityForChannel(*channel)
	}
	if capability == "" {
		return nil, BadAuthRequest("请选择模型能力")
	}
	accessPolicy := req.AccessPolicy
	if accessPolicy == "" {
		return nil, BadAuthRequest("请选择模型使用权限")
	}
	if accessPolicy != model.ModelAccessAuthenticated && accessPolicy != model.ModelAccessMember {
		return nil, BadAuthRequest("模型访问策略无效")
	}
	billingMode := strings.TrimSpace(req.BillingMode)
	if billingMode == "" {
		billingMode = "fixed_request"
	}
	if billingMode != "fixed_request" && billingMode != "per_second" {
		return nil, BadAuthRequest("模型计费方式仅支持按次或按秒")
	}
	if billingMode == "per_second" && capability != "video" {
		return nil, BadAuthRequest("只有视频模型可以按秒计费")
	}
	priceStrategy := strings.TrimSpace(req.PriceStrategy)
	if priceStrategy == "" {
		return nil, BadAuthRequest("请选择模型价格策略")
	}
	if priceStrategy != "flat" && priceStrategy != "image_resolution" && priceStrategy != "video_resolution" {
		return nil, BadAuthRequest("模型价格策略无效")
	}
	if priceStrategy == "image_resolution" && (capability != "image" || billingMode != "fixed_request") {
		return nil, BadAuthRequest("分辨率阶梯定价仅适用于按次计费的图片模型")
	}
	if priceStrategy == "video_resolution" && capability != "video" {
		return nil, BadAuthRequest("视频分辨率定价仅适用于视频模型")
	}
	if req.UnitPriceMicrocredits < 0 {
		return nil, BadAuthRequest("模型积分价格不能小于 0")
	}
	if req.PriceConfigured && priceStrategy == "flat" && req.UnitPriceMicrocredits <= 0 {
		return nil, BadAuthRequest("启用用户积分价格时，价格必须大于 0")
	}
	item := &model.ChannelModel{ID: newID(), ChannelID: channelID, AccessPolicy: model.ModelAccessAuthenticated, Enabled: true, PriceVersion: 1}
	var previousPricing *channelModelPricingSnapshot
	if id != "" {
		item, err = s.repo.ChannelModelByID(channelID, id)
		if err != nil {
			return nil, err
		}
		snapshot := snapshotChannelModelPricing(item)
		previousPricing = &snapshot
	}
	item.ModelKey = modelKey
	item.DisplayName = strings.TrimSpace(req.DisplayName)
	if item.DisplayName == "" {
		item.DisplayName = modelKey
	}
	item.MarketingCopy = marketingCopy
	item.PromotionBadge = promotionBadge
	item.EstimatedDurationSeconds = req.EstimatedDurationSeconds
	item.BrandKey = brandKey
	item.AccessPolicy = accessPolicy
	item.Capability = capability
	item.BillingMode = billingMode
	item.PriceStrategy = priceStrategy
	item.UnitPriceMicrocredits = req.UnitPriceMicrocredits
	item.PriceConfigured = req.PriceConfigured
	if req.Enabled != nil {
		item.Enabled = *req.Enabled
	}
	if priceStrategy == "image_resolution" || priceStrategy == "video_resolution" {
		item.UnitPriceMicrocredits = 0
	}
	tiers, err := buildChannelModelPriceTiers(item, req.PriceTiers)
	if err != nil {
		return nil, err
	}
	pricingChanged := previousPricing != nil && previousPricing.differsFrom(item, tiers)
	if pricingChanged {
		item.PriceVersion++
	}
	for index := range tiers {
		tiers[index].PriceVersion = item.PriceVersion
	}
	currentModels, err := s.repo.ChannelModels(channelID, true)
	if err != nil {
		return nil, err
	}
	modelsJSON, err := json.Marshal(channelModelNamesAfterSave(currentModels, item))
	if err != nil {
		return nil, err
	}
	audit, err := newAdminAuditEvent(actor, "channel_model.save", "channel_model", item.ID, "保存模型目录、展示配置、访问策略与计费配置", map[string]any{
		"channelId": item.ChannelID, "modelKey": item.ModelKey, "displayName": item.DisplayName,
		"marketingCopy": item.MarketingCopy, "promotionBadge": item.PromotionBadge, "estimatedDurationSeconds": item.EstimatedDurationSeconds, "brandKey": item.BrandKey,
		"accessPolicy": item.AccessPolicy, "capability": item.Capability, "enabled": item.Enabled,
		"priceConfigured": item.PriceConfigured, "priceVersion": item.PriceVersion, "pricingChanged": pricingChanged,
	})
	if err != nil {
		return nil, err
	}
	if err := s.repo.SaveChannelModelPricing(item, tiers, string(modelsJSON), audit); err != nil {
		return nil, err
	}
	saved, err := s.repo.ChannelModelByID(channelID, item.ID)
	if err != nil {
		return nil, err
	}
	normalized := normalizeChannelModelPriceTierCollections([]model.ChannelModel{*saved})
	return &normalized[0], nil
}

type channelModelPricingSnapshot struct {
	billingMode           string
	priceStrategy         string
	unitPriceMicrocredits int64
	priceConfigured       bool
	priceTiers            map[string]int64
}

func snapshotChannelModelPricing(item *model.ChannelModel) channelModelPricingSnapshot {
	priceTiers := make(map[string]int64, len(item.PriceTiers))
	for _, tier := range item.PriceTiers {
		priceTiers[tier.Resolution] = tier.UnitPriceMicrocredits
	}
	return channelModelPricingSnapshot{
		billingMode:           item.BillingMode,
		priceStrategy:         item.PriceStrategy,
		unitPriceMicrocredits: item.UnitPriceMicrocredits,
		priceConfigured:       item.PriceConfigured,
		priceTiers:            priceTiers,
	}
}

func (snapshot channelModelPricingSnapshot) differsFrom(item *model.ChannelModel, tiers []model.ChannelModelPriceTier) bool {
	if snapshot.billingMode != item.BillingMode ||
		snapshot.priceStrategy != item.PriceStrategy ||
		snapshot.unitPriceMicrocredits != item.UnitPriceMicrocredits ||
		snapshot.priceConfigured != item.PriceConfigured ||
		len(snapshot.priceTiers) != len(tiers) {
		return true
	}
	for _, tier := range tiers {
		if unitPrice, exists := snapshot.priceTiers[tier.Resolution]; !exists || unitPrice != tier.UnitPriceMicrocredits {
			return true
		}
	}
	return false
}

func channelModelNamesAfterSave(items []model.ChannelModel, saved *model.ChannelModel) []string {
	names := make([]string, 0, len(items)+1)
	for _, item := range items {
		if item.ID == saved.ID || !item.Enabled {
			continue
		}
		names = append(names, item.ModelKey)
	}
	if saved.Enabled {
		names = append(names, saved.ModelKey)
	}
	return uniqueNonEmpty(names)
}

func (s *Service) DeleteAdminChannelModel(actor *model.User, channelID string, id string) error {
	if err := s.RequireAdmin(actor); err != nil {
		return err
	}
	if _, err := s.repo.AdminSystemChannel(channelID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return BadAuthRequest("系统渠道不存在或已删除")
		}
		return err
	}
	if _, err := s.repo.ChannelModelByID(channelID, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return BadAuthRequest("渠道模型不存在或已删除")
		}
		return err
	}
	items, err := s.repo.ChannelModels(channelID, false)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		if item.ID != id {
			names = append(names, item.ModelKey)
		}
	}
	encoded, err := json.Marshal(names)
	if err != nil {
		return err
	}
	// 删除模型与渠道的兼容模型清单必须同事务提交，避免接口报错但列表已部分变化。
	err = s.repo.DeleteChannelModel(channelID, id, string(encoded), time.Now())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return BadAuthRequest("渠道模型不存在或已删除")
	}
	return err
}

func (s *Service) syncInitialChannelModels(channel *model.ModelChannel, names []string) error {
	existing, err := s.repo.ChannelModels(channel.ID, true)
	if err != nil {
		return err
	}
	byKey := make(map[string]*model.ChannelModel, len(existing))
	for index := range existing {
		byKey[existing[index].ModelKey] = &existing[index]
	}
	desired := make(map[string]bool, len(names))
	for _, name := range uniqueNonEmpty(names) {
		name = strings.TrimPrefix(name, "models/")
		desired[name] = true
		if item := byKey[name]; item != nil {
			if !item.Enabled {
				item.Enabled = true
				item.PriceVersion++
				if err := s.repo.SaveChannelModel(item); err != nil {
					return err
				}
			}
			continue
		}
		item := model.ChannelModel{ID: newID(), ChannelID: channel.ID, ModelKey: name, DisplayName: name, BrandKey: model.InferModelBrandKey(name), AccessPolicy: model.ModelAccessAuthenticated, Capability: capabilityForChannel(*channel), BillingMode: "fixed_request", PriceStrategy: "flat", Enabled: true, PriceVersion: 1}
		if err := s.repo.SaveChannelModel(&item); err != nil {
			return err
		}
	}
	for index := range existing {
		if existing[index].Enabled && !desired[existing[index].ModelKey] {
			existing[index].Enabled = false
			existing[index].PriceVersion++
			if err := s.repo.SaveChannelModel(&existing[index]); err != nil {
				return err
			}
		}
	}
	return nil
}

func buildChannelModelPriceTiers(item *model.ChannelModel, requests []ChannelModelPriceTierRequest) ([]model.ChannelModelPriceTier, error) {
	if item.PriceStrategy != "image_resolution" && item.PriceStrategy != "video_resolution" {
		return nil, nil
	}
	allowed := map[string]bool{"1K": true, "2K": true, "4K": true}
	requireAll := true
	if item.PriceStrategy == "video_resolution" {
		allowed = map[string]bool{
			"480P": true, "720P": true, "768P": true, "1080P": true, "2K": true, "4K": true,
			"SR_720P": true, "SR_1080P": true, "SR_2K": true, "SR_4K": true,
		}
		requireAll = false
	}
	configured := make(map[string]bool, len(allowed))
	tiers := make([]model.ChannelModelPriceTier, 0, len(requests))
	for _, request := range requests {
		resolution := strings.ToUpper(strings.TrimSpace(request.Resolution))
		if !allowed[resolution] {
			return nil, BadAuthRequest("价格规格无效：" + resolution)
		}
		if configured[resolution] {
			return nil, BadAuthRequest("价格规格不能重复")
		}
		if request.UnitPriceMicrocredits <= 0 {
			return nil, BadAuthRequest(resolution + " 积分价格必须大于 0")
		}
		configured[resolution] = true
		tiers = append(tiers, model.ChannelModelPriceTier{
			ID: newID(), ChannelModelID: item.ID, Resolution: resolution,
			UnitPriceMicrocredits: request.UnitPriceMicrocredits, PriceVersion: item.PriceVersion,
		})
	}
	if item.PriceConfigured {
		if len(tiers) == 0 {
			return nil, BadAuthRequest("启用分辨率定价前必须至少配置一个价格规格")
		}
		if requireAll {
			for resolution := range allowed {
				if configured[resolution] {
					continue
				}
				return nil, BadAuthRequest("启用分辨率定价前必须配置 " + resolution + " 价格")
			}
		}
	}
	return tiers, nil
}

func (s *Service) ensureChannelModels(channelID string, includeDisabled bool) ([]model.ChannelModel, error) {
	items, err := s.repo.ChannelModels(channelID, includeDisabled)
	if err != nil || len(items) > 0 {
		return normalizeChannelModelPriceTierCollections(items), err
	}
	channel, err := s.repo.AdminSystemChannel(channelID)
	if err != nil {
		return nil, err
	}
	if err := s.syncInitialChannelModels(channel, channelModelNames(*channel)); err != nil {
		return nil, err
	}
	items, err = s.repo.ChannelModels(channelID, includeDisabled)
	if err != nil {
		return nil, err
	}
	return normalizeChannelModelPriceTierCollections(items), nil
}

func normalizeChannelModelPriceTierCollections(items []model.ChannelModel) []model.ChannelModel {
	normalized := make([]model.ChannelModel, len(items))
	copy(normalized, items)
	for index := range normalized {
		if normalized[index].PriceTiers == nil {
			normalized[index].PriceTiers = make([]model.ChannelModelPriceTier, 0)
		}
		if !model.IsModelBrandKey(normalized[index].BrandKey) {
			normalized[index].BrandKey = model.InferModelBrandKey(normalized[index].ModelKey)
		}
	}
	return normalized
}

func capabilityForChannel(channel model.ModelChannel) string {
	switch channel.InterfaceType {
	case model.ChannelInterfaceOpenAIImage, model.ChannelInterfaceAPIMartImage:
		return "image"
	case model.ChannelInterfaceNewAPIVideo, model.ChannelInterfaceXAIVideo, model.ChannelInterfaceAIOpenVideo, model.ChannelInterfaceAIOpenVideoVolcengine, model.ChannelInterfaceMiniMaxVideo, model.ChannelInterfaceKlingVideo:
		return "video"
	case model.ChannelInterfaceMiniMaxSpeech:
		return "audio"
	default:
		return "text"
	}
}
