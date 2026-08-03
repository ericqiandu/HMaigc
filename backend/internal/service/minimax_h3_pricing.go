package service

import (
	"errors"
	"strings"

	"infinite-canvas/backend/internal/model"
)

const (
	miniMaxH3InputImageOverageSpecification = "INPUT_IMAGE_OVERAGE"
	miniMaxH3InputVideo768PSpecification    = "INPUT_VIDEO_768P"
	miniMaxH3InputVideo2KSpecification      = "INPUT_VIDEO_2K"
	miniMaxH3FreeInputImages                = 5
)

func miniMaxH3EstimatedCost(log *model.ApiCallLog, pricing *model.ModelPricing) (int64, error) {
	if log == nil || pricing == nil {
		return 0, errors.New("MiniMax H3 成本核算缺少调用日志或定价配置")
	}
	tiers := make(map[string]int64, len(pricing.Tiers))
	for _, tier := range pricing.Tiers {
		tiers[strings.ToUpper(strings.TrimSpace(tier.Specification))] = tier.SupplierCostMicros
	}
	resolution := strings.ToUpper(strings.TrimSpace(log.PricingSpecification))
	generatedRate, ok := tiers[resolution]
	if !ok || generatedRate <= 0 {
		return 0, errors.New("MiniMax H3 缺少生成分辨率 " + resolution + " 的供应商成本")
	}
	if log.VideoSeconds <= 0 {
		return 0, errors.New("MiniMax H3 缺少生成视频时长，无法核算供应商成本")
	}
	cost := int64(log.VideoSeconds) * generatedRate
	if overage := log.InputImageCount - miniMaxH3FreeInputImages; overage > 0 {
		imageRate, exists := tiers[miniMaxH3InputImageOverageSpecification]
		if !exists || imageRate <= 0 {
			return 0, errors.New("MiniMax H3 缺少超额输入图片供应商成本")
		}
		cost += int64(overage) * imageRate
	}
	if log.InputVideoCount > 0 {
		if !log.InputVideoDurationComplete {
			return 0, errors.New("MiniMax H3 输入视频缺少时长元数据，无法核算供应商成本")
		}
		specification := miniMaxH3InputVideo768PSpecification
		if resolution == "2K" {
			specification = miniMaxH3InputVideo2KSpecification
		}
		videoRate, exists := tiers[specification]
		if !exists || videoRate <= 0 {
			return 0, errors.New("MiniMax H3 缺少输入视频供应商成本")
		}
		cost += (log.InputVideoDurationMs*videoRate + 999) / 1000
	}
	return cost, nil
}
