package service

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

const productRolloutSettingKey = "product_rollouts"

var productRolloutFlagKeys = []string{
	"canvasToolRegistryV2",
	"canvasMediaToolsV2",
	"canvasProductionNodesV2",
	"creativeWorkspaceV2",
	"skillLibraryV2",
}

type ProductRolloutRule struct {
	Percentage     int      `json:"percentage"`
	AdminAllowlist []string `json:"adminAllowlist"`
}

type ProductRolloutSetting struct {
	CanvasToolRegistryV2    ProductRolloutRule `json:"canvasToolRegistryV2"`
	CanvasMediaToolsV2      ProductRolloutRule `json:"canvasMediaToolsV2"`
	CanvasProductionNodesV2 ProductRolloutRule `json:"canvasProductionNodesV2"`
	CreativeWorkspaceV2     ProductRolloutRule `json:"creativeWorkspaceV2"`
	SkillLibraryV2          ProductRolloutRule `json:"skillLibraryV2"`
}

type ProductRolloutFlags struct {
	CanvasToolRegistryV2    bool `json:"canvasToolRegistryV2"`
	CanvasMediaToolsV2      bool `json:"canvasMediaToolsV2"`
	CanvasProductionNodesV2 bool `json:"canvasProductionNodesV2"`
	CreativeWorkspaceV2     bool `json:"creativeWorkspaceV2"`
	SkillLibraryV2          bool `json:"skillLibraryV2"`
}

func (s *Service) ProductRollouts(actor *model.User) (*ProductRolloutFlags, error) {
	if actor == nil || strings.TrimSpace(actor.ID) == "" {
		return nil, Unauthorized("请先登录")
	}
	setting, err := s.productRolloutSetting()
	if err != nil {
		return nil, err
	}
	return &ProductRolloutFlags{
		CanvasToolRegistryV2:    rolloutEnabled(actor.ID, "canvasToolRegistryV2", setting.CanvasToolRegistryV2),
		CanvasMediaToolsV2:      rolloutEnabled(actor.ID, "canvasMediaToolsV2", setting.CanvasMediaToolsV2),
		CanvasProductionNodesV2: rolloutEnabled(actor.ID, "canvasProductionNodesV2", setting.CanvasProductionNodesV2),
		CreativeWorkspaceV2:     rolloutEnabled(actor.ID, "creativeWorkspaceV2", setting.CreativeWorkspaceV2),
		SkillLibraryV2:          rolloutEnabled(actor.ID, "skillLibraryV2", setting.SkillLibraryV2),
	}, nil
}

func (s *Service) AdminProductRollouts(actor *model.User) (*ProductRolloutSetting, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	setting, err := s.productRolloutSetting()
	return setting, err
}

func (s *Service) UpdateProductRollouts(actor *model.User, requested ProductRolloutSetting) (*ProductRolloutSetting, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	normalized, err := normalizeProductRolloutSetting(requested)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	setting := model.SystemSetting{Key: productRolloutSettingKey, ValueJSON: string(encoded), UpdatedBy: actor.ID}
	current, err := s.repo.SystemSetting(productRolloutSettingKey)
	if err == nil {
		setting.CreatedAt = current.CreatedAt
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	audit, err := newAdminAuditEvent(actor, "product_rollouts.update", "system_setting", productRolloutSettingKey, "更新产品能力灰度配置", normalized)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SaveSystemSettingWithAudit(&setting, audit); err != nil {
		return nil, err
	}
	return &normalized, nil
}

func (s *Service) productRolloutSetting() (*ProductRolloutSetting, error) {
	stored, err := s.repo.SystemSetting(productRolloutSettingKey)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("产品能力配置不可用: %w", err)
		}
		return nil, err
	}
	normalized, err := ParseProductRolloutSettingJSON([]byte(stored.ValueJSON))
	if err != nil {
		return nil, fmt.Errorf("产品能力配置数据损坏: %w", err)
	}
	return normalized, nil
}

func ParseProductRolloutSettingJSON(data []byte) (*ProductRolloutSetting, error) {
	var flags map[string]json.RawMessage
	if err := json.Unmarshal(data, &flags); err != nil {
		return nil, err
	}
	if len(flags) != len(productRolloutFlagKeys) {
		return nil, errors.New("产品能力配置缺少或包含未知字段")
	}
	for _, key := range productRolloutFlagKeys {
		raw, ok := flags[key]
		if !ok {
			return nil, fmt.Errorf("产品能力配置缺少字段: %s", key)
		}
		var ruleFields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &ruleFields); err != nil {
			return nil, fmt.Errorf("%s 规则无效: %w", key, err)
		}
		if len(ruleFields) != 2 || ruleFields["percentage"] == nil || ruleFields["adminAllowlist"] == nil {
			return nil, fmt.Errorf("%s 规则必须仅包含 percentage 和 adminAllowlist", key)
		}
	}
	var setting ProductRolloutSetting
	if err := json.Unmarshal(data, &setting); err != nil {
		return nil, err
	}
	normalized, err := normalizeProductRolloutSetting(setting)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}

func normalizeProductRolloutSetting(setting ProductRolloutSetting) (ProductRolloutSetting, error) {
	rules := []*ProductRolloutRule{
		&setting.CanvasToolRegistryV2,
		&setting.CanvasMediaToolsV2,
		&setting.CanvasProductionNodesV2,
		&setting.CreativeWorkspaceV2,
		&setting.SkillLibraryV2,
	}
	for _, rule := range rules {
		if rule.Percentage < 0 || rule.Percentage > 100 {
			return ProductRolloutSetting{}, errors.New("灰度百分比必须在 0..100 之间")
		}
		seen := make(map[string]struct{}, len(rule.AdminAllowlist))
		normalized := make([]string, 0, len(rule.AdminAllowlist))
		for _, value := range rule.AdminAllowlist {
			id := strings.TrimSpace(value)
			if id == "" {
				return ProductRolloutSetting{}, errors.New("灰度 allowlist 不得包含空用户 ID")
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			normalized = append(normalized, id)
		}
		sort.Strings(normalized)
		rule.AdminAllowlist = normalized
	}
	return setting, nil
}

func rolloutEnabled(userID string, flagKey string, rule ProductRolloutRule) bool {
	for _, allowedID := range rule.AdminAllowlist {
		if userID == allowedID {
			return true
		}
	}
	if rule.Percentage == 0 {
		return false
	}
	if rule.Percentage == 100 {
		return true
	}
	digest := sha256.Sum256([]byte(userID + "\x00" + flagKey))
	bucket := binary.BigEndian.Uint64(digest[:8]) % 100
	return bucket < uint64(rule.Percentage)
}
