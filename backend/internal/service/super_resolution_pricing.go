package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

type SuperResolutionPricingRuleRequest struct {
	Edition               string `json:"edition"`
	Resolution            string `json:"resolution"`
	FPSMinExclusive       int    `json:"fpsMinExclusive"`
	FPSMaxInclusive       int    `json:"fpsMaxInclusive"`
	Currency              string `json:"currency"`
	SupplierCostMinMicros int64  `json:"supplierCostMinMicros"`
	SupplierCostMaxMicros int64  `json:"supplierCostMaxMicros"`
	UnitPriceMicrocredits int64  `json:"unitPriceMicrocredits"`
	PriceConfigured       bool   `json:"priceConfigured"`
	Enabled               bool   `json:"enabled"`
}

type ReplaceSuperResolutionPricingRequest struct {
	Rules []SuperResolutionPricingRuleRequest `json:"rules"`
}

func (s *Service) AdminSuperResolutionPricingRules(actor *model.User) ([]model.SuperResolutionPricingRule, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	return s.repo.SuperResolutionPricingRules()
}

func (s *Service) ReplaceAdminSuperResolutionPricingRules(actor *model.User, request ReplaceSuperResolutionPricingRequest) ([]model.SuperResolutionPricingRule, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if len(request.Rules) != 30 {
		return nil, BadAuthRequest("超分定价必须完整包含 2 个版本、5 个分辨率和 3 个帧率档，共 30 条规则")
	}
	existingRules, err := s.repo.SuperResolutionPricingRules()
	if err != nil {
		return nil, err
	}
	existingByScope := make(map[string]model.SuperResolutionPricingRule, len(existingRules))
	for _, existing := range existingRules {
		existingByScope[superResolutionPricingScope(existing.Edition, existing.Resolution, existing.FPSMinExclusive, existing.FPSMaxInclusive)] = existing
	}
	now := time.Now()
	rules := make([]model.SuperResolutionPricingRule, 0, len(request.Rules))
	seen := make(map[string]struct{}, len(request.Rules))
	for index, input := range request.Rules {
		edition := strings.ToLower(strings.TrimSpace(input.Edition))
		if edition != "standard" && edition != "professional" {
			return nil, BadAuthRequest(fmt.Sprintf("第 %d 条规则的版本无效", index+1))
		}
		resolution := normalizeSuperResolutionPricingResolution(input.Resolution)
		if resolution == "" {
			return nil, BadAuthRequest(fmt.Sprintf("第 %d 条规则的分辨率无效", index+1))
		}
		if !validSuperResolutionFPSBand(input.FPSMinExclusive, input.FPSMaxInclusive) {
			return nil, BadAuthRequest(fmt.Sprintf("第 %d 条规则的帧率区间无效", index+1))
		}
		currency := strings.ToUpper(strings.TrimSpace(input.Currency))
		if len(currency) != 3 {
			return nil, BadAuthRequest(fmt.Sprintf("第 %d 条规则的币种无效", index+1))
		}
		if input.SupplierCostMinMicros < 0 || input.SupplierCostMaxMicros < input.SupplierCostMinMicros {
			return nil, BadAuthRequest(fmt.Sprintf("第 %d 条规则的供应商成本区间无效", index+1))
		}
		priceConfigured := input.UnitPriceMicrocredits > 0
		if input.PriceConfigured != priceConfigured {
			return nil, BadAuthRequest(fmt.Sprintf("第 %d 条规则的售价状态与积分售价不一致", index+1))
		}
		if priceConfigured && input.UnitPriceMicrocredits <= 0 {
			return nil, BadAuthRequest(fmt.Sprintf("第 %d 条规则已标记售价生效，但积分售价不是正数", index+1))
		}
		key := superResolutionPricingScope(edition, resolution, input.FPSMinExclusive, input.FPSMaxInclusive)
		if _, exists := seen[key]; exists {
			return nil, BadAuthRequest("超分定价存在重复规格：" + key)
		}
		seen[key] = struct{}{}
		id := newID()
		createdAt := now
		priceVersion := int64(1)
		if existing, exists := existingByScope[key]; exists {
			id = existing.ID
			createdAt = existing.CreatedAt
			priceVersion = existing.PriceVersion + 1
		}
		rules = append(rules, model.SuperResolutionPricingRule{
			ID: id, Edition: edition, Resolution: resolution,
			FPSMinExclusive: input.FPSMinExclusive, FPSMaxInclusive: input.FPSMaxInclusive,
			Currency: currency, SupplierCostMinMicros: input.SupplierCostMinMicros, SupplierCostMaxMicros: input.SupplierCostMaxMicros,
			UnitPriceMicrocredits: input.UnitPriceMicrocredits, PriceConfigured: priceConfigured,
			Enabled: input.Enabled, PriceVersion: priceVersion, CreatedAt: createdAt, UpdatedAt: now,
		})
	}
	encoded, err := json.Marshal(rules)
	if err != nil {
		return nil, err
	}
	audit := &model.AdminAuditEvent{
		ID: newID(), ActorUserID: actor.ID, Action: "super_resolution_pricing.replace", TargetType: "super_resolution_pricing",
		TargetID: "global", Summary: "更新独立超分定价规则", MetadataJSON: string(encoded), CreatedAt: now,
	}
	if err := s.repo.ReplaceSuperResolutionPricingRules(rules, audit); err != nil {
		return nil, err
	}
	return s.repo.SuperResolutionPricingRules()
}

func superResolutionPricingScope(edition string, resolution string, minExclusive int, maxInclusive int) string {
	return fmt.Sprintf("%s:%s:%d:%d", strings.ToLower(strings.TrimSpace(edition)), normalizeSuperResolutionPricingResolution(resolution), minExclusive, maxInclusive)
}

func normalizeSuperResolutionPricingResolution(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "720", "720p":
		return "720P"
	case "1080", "1080p":
		return "1080P"
	case "2k":
		return "2K"
	case "4k":
		return "4K"
	case "8k":
		return "8K"
	default:
		return ""
	}
}

func validSuperResolutionFPSBand(minExclusive int, maxInclusive int) bool {
	return (minExclusive == 0 && maxInclusive == 30) || (minExclusive == 30 && maxInclusive == 60) || (minExclusive == 60 && maxInclusive == 120)
}

func (s *Service) matchSuperResolutionPricingRule(usage BillingUsage) (*model.SuperResolutionPricingRule, error) {
	edition := strings.ToLower(strings.TrimSpace(usage.SuperResolutionVersion))
	resolution := normalizeSuperResolutionPricingResolution(usage.SuperResolutionResolution)
	if edition == "" || resolution == "" || usage.SuperResolutionFPS < 1 || usage.SuperResolutionFPS > 120 {
		return nil, BadAuthRequest("超分版本、目标分辨率或输出帧率无效，无法匹配超分价格")
	}
	rule, err := s.repo.SuperResolutionPricingRule(edition, resolution, usage.SuperResolutionFPS)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, BadAuthRequest("当前超分规格未配置价格")
	}
	if err != nil {
		return nil, err
	}
	if !rule.PriceConfigured || rule.UnitPriceMicrocredits <= 0 {
		return nil, BadAuthRequest("当前超分规格尚未配置用户积分售价")
	}
	return rule, nil
}
