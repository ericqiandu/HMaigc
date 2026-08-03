package service

import (
	"errors"
	"strings"

	"infinite-canvas/backend/internal/model"
)

const (
	miniMaxSpeechCharacterSpecification = "TEN_THOUSAND_CHARACTERS"
	miniMaxVoiceCloneSpecification      = "VOICE_DESIGN_OR_CLONE"
	miniMaxVoiceCloningPricingModel     = "MiniMax-Voice-Cloning"
)

func miniMaxAudioEstimatedCost(log *model.ApiCallLog, pricing *model.ModelPricing) (int64, error) {
	if log == nil || pricing == nil {
		return 0, errors.New("MiniMax 音频成本核算缺少调用日志或定价配置")
	}
	specification := miniMaxSpeechCharacterSpecification
	quantity := int64(log.InputCharacterCount)
	denominator := int64(10_000)
	if strings.EqualFold(strings.TrimSpace(log.Model), miniMaxVoiceCloningPricingModel) {
		specification = miniMaxVoiceCloneSpecification
		quantity = 1
		denominator = 1
	} else if quantity <= 0 {
		return 0, errors.New("MiniMax 语音合成缺少输入字符数，无法核算供应商成本")
	}
	for _, tier := range pricing.Tiers {
		if strings.EqualFold(strings.TrimSpace(tier.Specification), specification) {
			if tier.SupplierCostMicros <= 0 {
				return 0, errors.New("MiniMax 音频供应商成本未配置")
			}
			return (quantity*tier.SupplierCostMicros + denominator - 1) / denominator, nil
		}
	}
	return 0, errors.New("MiniMax 音频缺少供应商定价规格 " + specification)
}
