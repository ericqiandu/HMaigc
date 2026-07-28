package service

import (
	"encoding/json"
	"errors"
	"strings"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

const modelPricingOperationsSettingKey = "model_pricing_operations"

type ModelPricingOperationsSetting struct {
	Configured              bool   `json:"configured"`
	Currency                string `json:"currency"`
	CreditRevenueMicros     int64  `json:"creditRevenueMicros"`
	TargetMarginBasisPoints int64  `json:"targetMarginBasisPoints"`
}

func (s *Service) AdminModelPricingOperationsSetting(actor *model.User) (ModelPricingOperationsSetting, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return ModelPricingOperationsSetting{}, err
	}
	setting, err := s.repo.SystemSetting(modelPricingOperationsSettingKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ModelPricingOperationsSetting{Configured: false}, nil
	}
	if err != nil {
		return ModelPricingOperationsSetting{}, err
	}
	var value ModelPricingOperationsSetting
	if err := json.Unmarshal([]byte(setting.ValueJSON), &value); err != nil {
		return ModelPricingOperationsSetting{}, errors.New("模型定价经营参数格式无效")
	}
	value.Configured = true
	if err := validateModelPricingOperationsSetting(value); err != nil {
		return ModelPricingOperationsSetting{}, err
	}
	return value, nil
}

func (s *Service) UpdateModelPricingOperationsSetting(actor *model.User, value ModelPricingOperationsSetting) (ModelPricingOperationsSetting, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return ModelPricingOperationsSetting{}, err
	}
	value.Currency = strings.ToUpper(strings.TrimSpace(value.Currency))
	value.Configured = true
	if err := validateModelPricingOperationsSetting(value); err != nil {
		return ModelPricingOperationsSetting{}, err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return ModelPricingOperationsSetting{}, err
	}
	setting := model.SystemSetting{Key: modelPricingOperationsSettingKey, ValueJSON: string(encoded), UpdatedBy: actor.ID}
	current, err := s.repo.SystemSetting(modelPricingOperationsSettingKey)
	if err == nil {
		setting.CreatedAt = current.CreatedAt
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return ModelPricingOperationsSetting{}, err
	}
	if err := s.repo.SaveSystemSetting(&setting); err != nil {
		return ModelPricingOperationsSetting{}, err
	}
	if err := s.appendAdminAudit(actor, "model_pricing_operations.update", "system_setting", modelPricingOperationsSettingKey, "更新模型定价经营参数", value); err != nil {
		return ModelPricingOperationsSetting{}, err
	}
	return value, nil
}

func validateModelPricingOperationsSetting(value ModelPricingOperationsSetting) error {
	if value.Currency == "" || len(value.Currency) > 12 {
		return BadAuthRequest("请选择有效的核算币种")
	}
	if value.CreditRevenueMicros <= 0 {
		return BadAuthRequest("每积分收入价值必须大于 0")
	}
	if value.TargetMarginBasisPoints < 0 || value.TargetMarginBasisPoints > 10_000 {
		return BadAuthRequest("目标利润率必须在 0% 到 100% 之间")
	}
	return nil
}
