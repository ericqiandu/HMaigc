package service

import (
	"errors"
	"fmt"
	"strings"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

type mediaInputUsageAdjustment struct {
	Metric           string
	ActualQuantity   int64
	IncludedQuantity int64
	BillableQuantity int64
	UnitAmount       int64
	Amount           int64
}

type mediaInputUsagePricingSnapshot struct {
	Metric                 string
	ActualQuantity         int64
	IncludedQuantity       int64
	BillableQuantity       int64
	SupplierUnitMicros     int64
	UserUnitMicrocredits   int64
	SupplierAmountMicros   int64
	UserAmountMicrocredits int64
}

func calculateMediaInputUsageAdjustment(metric string, actualQuantity int64, includedQuantity int64, unitAmount int64) (mediaInputUsageAdjustment, error) {
	metric = strings.ToLower(strings.TrimSpace(metric))
	if metric != inputImageUsageMetric {
		return mediaInputUsageAdjustment{}, errors.New("不支持的媒体输入用量指标")
	}
	if actualQuantity < 0 {
		return mediaInputUsageAdjustment{}, errors.New("媒体输入实际数量不能小于 0")
	}
	if includedQuantity < 0 {
		return mediaInputUsageAdjustment{}, errors.New("媒体输入免费数量不能小于 0")
	}
	if unitAmount <= 0 {
		return mediaInputUsageAdjustment{}, errors.New("媒体输入超额单价必须大于 0")
	}
	billableQuantity := actualQuantity - includedQuantity
	if billableQuantity < 0 {
		billableQuantity = 0
	}
	const maxInt64 = int64(^uint64(0) >> 1)
	if billableQuantity > 0 && unitAmount > maxInt64/billableQuantity {
		return mediaInputUsageAdjustment{}, errors.New("媒体输入超额金额溢出")
	}
	return mediaInputUsageAdjustment{
		Metric: metric, ActualQuantity: actualQuantity, IncludedQuantity: includedQuantity,
		BillableQuantity: billableQuantity, UnitAmount: unitAmount, Amount: billableQuantity * unitAmount,
	}, nil
}

func channelInputImageUsageTier(tiers []model.ChannelModelPriceTier) (*model.ChannelModelPriceTier, error) {
	var matched *model.ChannelModelPriceTier
	for index := range tiers {
		metric := strings.ToLower(strings.TrimSpace(tiers[index].UsageMetric))
		if metric == "" {
			continue
		}
		if metric != inputImageUsageMetric {
			return nil, fmt.Errorf("渠道模型配置了不支持的用量指标: %s", metric)
		}
		if matched != nil {
			return nil, errors.New("渠道模型的输入图片用量价格存在重复配置")
		}
		matched = &tiers[index]
	}
	return matched, nil
}

func supplierInputImageUsageTier(tiers []model.ModelPricingTier) (*model.ModelPricingTier, error) {
	var matched *model.ModelPricingTier
	for index := range tiers {
		metric := strings.ToLower(strings.TrimSpace(tiers[index].UsageMetric))
		if metric == "" {
			continue
		}
		if metric != inputImageUsageMetric {
			return nil, fmt.Errorf("上游成本配置了不支持的用量指标: %s", metric)
		}
		if matched != nil {
			return nil, errors.New("上游输入图片用量成本存在重复配置")
		}
		matched = &tiers[index]
	}
	return matched, nil
}

func (s *Service) mediaInputUsagePricingSnapshot(item *model.ChannelModel, actualQuantity int64, multiplierBPS int64) (*mediaInputUsagePricingSnapshot, error) {
	userTier, err := channelInputImageUsageTier(item.PriceTiers)
	if err != nil {
		return nil, err
	}
	if userTier == nil {
		return nil, nil
	}
	pricing, err := s.repo.ModelPricing(item.ChannelID, item.ModelKey, item.Capability)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, BadAuthRequest("当前模型已配置输入图片超额积分，但缺少对应的上游成本配置")
	}
	if err != nil {
		return nil, err
	}
	supplierTier, err := supplierInputImageUsageTier(pricing.Tiers)
	if err != nil {
		return nil, err
	}
	if supplierTier == nil {
		return nil, BadAuthRequest("当前模型已配置输入图片超额积分，但缺少对应的上游成本规则")
	}
	if userTier.IncludedQuantity != supplierTier.IncludedQuantity {
		return nil, BadAuthRequest("输入图片免费数量与上游成本规则不一致")
	}
	userAdjustment, err := calculateMediaInputUsageAdjustment(
		userTier.UsageMetric, actualQuantity, userTier.IncludedQuantity, userTier.UnitPriceMicrocredits,
	)
	if err != nil {
		return nil, err
	}
	supplierAdjustment, err := calculateMediaInputUsageAdjustment(
		supplierTier.UsageMetric, actualQuantity, supplierTier.IncludedQuantity, supplierTier.SupplierCostMicros,
	)
	if err != nil {
		return nil, err
	}
	userAmount := int64(0)
	if userAdjustment.BillableQuantity > 0 {
		userAmount, err = creditAmount(userTier.UnitPriceMicrocredits, userAdjustment.BillableQuantity, multiplierBPS)
		if err != nil {
			return nil, err
		}
	}
	return &mediaInputUsagePricingSnapshot{
		Metric: inputImageUsageMetric, ActualQuantity: actualQuantity,
		IncludedQuantity: userAdjustment.IncludedQuantity, BillableQuantity: userAdjustment.BillableQuantity,
		SupplierUnitMicros: supplierTier.SupplierCostMicros, UserUnitMicrocredits: userTier.UnitPriceMicrocredits,
		SupplierAmountMicros: supplierAdjustment.Amount, UserAmountMicrocredits: userAmount,
	}, nil
}
