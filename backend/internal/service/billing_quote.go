package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"infinite-canvas/backend/internal/model"
)

const maxBillingQuoteBatchCount int64 = 15

type TaskBillingQuoteRequest struct {
	ProjectID  string                `json:"projectId"`
	Type       string                `json:"type"`
	Operation  string                `json:"operation"`
	BatchCount int64                 `json:"batchCount"`
	Input      TaskBillingQuoteInput `json:"input"`
}

type TaskBillingQuoteInput struct {
	Mode                string                 `json:"mode"`
	ReferenceImageCount int64                  `json:"referenceImageCount"`
	ReferenceVideoCount int64                  `json:"referenceVideoCount"`
	Config              TaskBillingQuoteConfig `json:"config"`
}

type TaskBillingQuoteConfig struct {
	ChannelID                      string `json:"channelId"`
	Model                          string `json:"model"`
	Size                           string `json:"size"`
	Quality                        string `json:"quality"`
	VideoSeconds                   string `json:"videoSeconds"`
	VideoQuality                   string `json:"vquality"`
	VideoGenerateAudio             bool   `json:"videoGenerateAudio"`
	SuperResolutionEnabled         bool   `json:"videoSuperResolutionEnabled"`
	SuperResolutionResolution      string `json:"videoSuperResolutionResolution"`
	SuperResolutionVersion         string `json:"videoSuperResolutionVersion"`
	SuperResolutionFramesPerSecond int    `json:"videoSuperResolutionFps"`
}

type TaskBillingQuote struct {
	AmountMicrocredits            int64                            `json:"amountMicrocredits"`
	PerTaskAmountMicrocredits     int64                            `json:"perTaskAmountMicrocredits"`
	TaskCount                     int64                            `json:"taskCount"`
	PriceVersion                  int64                            `json:"priceVersion"`
	BillingMode                   string                           `json:"billingMode"`
	PricingResolution             string                           `json:"pricingResolution"`
	PricingInputVariant           string                           `json:"pricingInputVariant"`
	Quantity                      int64                            `json:"quantity"`
	EnhancementAmountMicrocredits int64                            `json:"enhancementAmountMicrocredits"`
	UsageAdjustment               *TaskBillingQuoteUsageAdjustment `json:"usageAdjustment,omitempty"`
	QuoteFingerprint              string                           `json:"quoteFingerprint"`
}

type TaskBillingQuoteUsageAdjustment struct {
	Metric                    string `json:"metric"`
	ActualQuantity            int64  `json:"actualQuantity"`
	IncludedQuantity          int64  `json:"includedQuantity"`
	BillableQuantity          int64  `json:"billableQuantity"`
	UnitPriceMicrocredits     int64  `json:"unitPriceMicrocredits"`
	PerTaskAmountMicrocredits int64  `json:"perTaskAmountMicrocredits"`
	AmountMicrocredits        int64  `json:"amountMicrocredits"`
}

type QuoteChangedError struct {
	CurrentQuote TaskBillingQuote
}

func (e *QuoteChangedError) Error() string {
	return "预计积分已变化，请确认新报价后重试"
}

type billingQuoteFingerprintFacts struct {
	ChannelID                             string `json:"channelId"`
	ChannelModelID                        string `json:"channelModelId"`
	Model                                 string `json:"model"`
	Capability                            string `json:"capability"`
	BillingMode                           string `json:"billingMode"`
	PriceVersion                          int64  `json:"priceVersion"`
	PriceTierID                           string `json:"priceTierId"`
	PricingResolution                     string `json:"pricingResolution"`
	PricingInputVariant                   string `json:"pricingInputVariant"`
	UnitPriceMicrocredits                 int64  `json:"unitPriceMicrocredits"`
	MultiplierBasisPoints                 int64  `json:"multiplierBasisPoints"`
	Quantity                              int64  `json:"quantity"`
	AmountMicrocredits                    int64  `json:"amountMicrocredits"`
	EnhancementPricingRuleID              string `json:"enhancementPricingRuleId"`
	EnhancementUnitPriceMicrocredits      int64  `json:"enhancementUnitPriceMicrocredits"`
	EnhancementAmountMicrocredits         int64  `json:"enhancementAmountMicrocredits"`
	UsageAdjustmentMetric                 string `json:"usageAdjustmentMetric"`
	UsageAdjustmentActualQuantity         int64  `json:"usageAdjustmentActualQuantity"`
	UsageAdjustmentIncludedQuantity       int64  `json:"usageAdjustmentIncludedQuantity"`
	UsageAdjustmentBillableQuantity       int64  `json:"usageAdjustmentBillableQuantity"`
	UsageAdjustmentUserUnitMicrocredits   int64  `json:"usageAdjustmentUserUnitMicrocredits"`
	UsageAdjustmentUserAmountMicrocredits int64  `json:"usageAdjustmentUserAmountMicrocredits"`
}

func (s *Service) QuoteTaskBilling(userID string, request TaskBillingQuoteRequest) (*TaskBillingQuote, error) {
	if request.BatchCount < 1 || request.BatchCount > maxBillingQuoteBatchCount {
		return nil, BadAuthRequest("生成数量必须在 1-15 之间")
	}
	capability := normalizeCapability(request.Input.Mode)
	taskCapability := capabilityFromTaskType(strings.TrimSpace(request.Type))
	if taskCapability == "" || capability == "" || taskCapability != capability {
		return nil, BadAuthRequest("任务类型与模型能力不匹配")
	}
	channelID := strings.TrimSpace(request.Input.Config.ChannelID)
	modelKey := strings.TrimPrefix(strings.TrimSpace(request.Input.Config.Model), "models/")
	if channelID == "" || modelKey == "" {
		return nil, BadAuthRequest("当前功能必须使用后台配置的系统模型")
	}
	billingScope, err := s.billingAccountScopeForTask(userID, request.ProjectID)
	if err != nil {
		return nil, err
	}
	usage, err := billingUsageFromQuoteInput(capability, modelKey, request.Input)
	if err != nil {
		return nil, err
	}
	order, err := s.newBillingOrder(
		userID,
		billingScope,
		"",
		"quote:"+newID(),
		channelID,
		modelKey,
		capability,
		firstNonEmpty(strings.TrimSpace(request.Operation), strings.TrimSpace(request.Type)),
		usage,
	)
	if err != nil {
		return nil, err
	}
	return taskBillingQuoteFromOrder(order, request.BatchCount)
}

func billingUsageFromQuoteInput(capability string, modelKey string, input TaskBillingQuoteInput) (BillingUsage, error) {
	config := input.Config
	switch capability {
	case "image":
		if input.ReferenceImageCount < 0 {
			return BillingUsage{}, BadAuthRequest("参考图片数量无效")
		}
		return BillingUsage{
			Quantity: 1, InputImageCount: input.ReferenceImageCount,
			Resolution: imagePricingResolutionFromConfig(map[string]any{
				"size": config.Size, "quality": config.Quality,
			}),
		}, nil
	case "video":
		if input.ReferenceVideoCount < 0 {
			return BillingUsage{}, BadAuthRequest("参考视频数量无效")
		}
		usage := BillingUsage{
			Quantity: positiveInteger(config.VideoSeconds), Resolution: strings.TrimSpace(config.VideoQuality),
			InputVariant: "standard", SuperResolutionEnabled: config.SuperResolutionEnabled,
			SuperResolutionResolution: strings.TrimSpace(config.SuperResolutionResolution),
			SuperResolutionVersion:    strings.TrimSpace(config.SuperResolutionVersion),
			SuperResolutionFPS:        config.SuperResolutionFramesPerSecond,
		}
		if _, spec, managed := kuaiziProviderFamilyForModel(modelKey); managed && spec.Capability == "video" && input.ReferenceVideoCount > 0 {
			usage.InputVariant = "reference_video"
		} else if managed && spec.Capability == "video" && config.VideoGenerateAudio {
			usage.InputVariant = "standard_audio"
		}
		return usage, nil
	default:
		return BillingUsage{}, BadAuthRequest("任务能力类型无效，无法计费")
	}
}

func multiplyQuoteAmount(amount int64, count int64) (int64, error) {
	if amount < 0 || count <= 0 {
		return 0, errors.New("积分报价参数无效")
	}
	if amount > (1<<63-1)/count {
		return 0, errors.New("积分报价金额溢出")
	}
	return amount * count, nil
}

func taskBillingQuoteFromOrder(order *model.BillingOrder, taskCount int64) (*TaskBillingQuote, error) {
	if order == nil {
		return nil, errors.New("积分报价事实不存在")
	}
	amount, err := multiplyQuoteAmount(order.AmountMicrocredits, taskCount)
	if err != nil {
		return nil, err
	}
	enhancementAmount, err := multiplyQuoteAmount(order.EnhancementAmountMicrocredits, taskCount)
	if err != nil {
		return nil, err
	}
	fingerprint, err := billingOrderQuoteFingerprint(order)
	if err != nil {
		return nil, err
	}
	quote := &TaskBillingQuote{
		AmountMicrocredits: amount, PerTaskAmountMicrocredits: order.AmountMicrocredits,
		TaskCount: taskCount, PriceVersion: order.PriceVersion, BillingMode: order.BillingMode,
		PricingResolution: order.PricingResolution, PricingInputVariant: order.PricingInputVariant,
		Quantity: order.Quantity, EnhancementAmountMicrocredits: enhancementAmount,
		QuoteFingerprint: fingerprint,
	}
	if order.UsageAdjustmentMetric != "" {
		adjustmentAmount, multiplyErr := multiplyQuoteAmount(order.UsageAdjustmentUserAmountMicrocredits, taskCount)
		if multiplyErr != nil {
			return nil, multiplyErr
		}
		quote.UsageAdjustment = &TaskBillingQuoteUsageAdjustment{
			Metric: order.UsageAdjustmentMetric, ActualQuantity: order.UsageAdjustmentActualQuantity,
			IncludedQuantity: order.UsageAdjustmentIncludedQuantity, BillableQuantity: order.UsageAdjustmentBillableQuantity,
			UnitPriceMicrocredits:     order.UsageAdjustmentUserUnitMicrocredits,
			PerTaskAmountMicrocredits: order.UsageAdjustmentUserAmountMicrocredits, AmountMicrocredits: adjustmentAmount,
		}
	}
	return quote, nil
}

func validateTaskBillingQuoteConfirmation(request CreateTaskRequest, order *model.BillingOrder) error {
	if order == nil || (order.Capability != "image" && order.Capability != "video") {
		return nil
	}
	current, err := taskBillingQuoteFromOrder(order, 1)
	if err != nil {
		return err
	}
	if request.QuotePriceVersion != current.PriceVersion || strings.TrimSpace(request.QuoteFingerprint) != current.QuoteFingerprint {
		return &QuoteChangedError{CurrentQuote: *current}
	}
	return nil
}

func billingOrderQuoteFingerprint(order *model.BillingOrder) (string, error) {
	if order == nil {
		return "", errors.New("积分报价事实不存在")
	}
	facts := billingQuoteFingerprintFacts{
		ChannelID: order.ChannelID, ChannelModelID: order.ChannelModelID, Model: order.Model,
		Capability: order.Capability, BillingMode: order.BillingMode, PriceVersion: order.PriceVersion,
		PriceTierID: order.PriceTierID, PricingResolution: order.PricingResolution,
		PricingInputVariant: order.PricingInputVariant, UnitPriceMicrocredits: order.UnitPriceMicrocredits,
		MultiplierBasisPoints: order.MultiplierBasisPoints, Quantity: order.Quantity,
		AmountMicrocredits: order.AmountMicrocredits, EnhancementPricingRuleID: order.EnhancementPricingRuleID,
		EnhancementUnitPriceMicrocredits:      order.EnhancementUnitPriceMicrocredits,
		EnhancementAmountMicrocredits:         order.EnhancementAmountMicrocredits,
		UsageAdjustmentMetric:                 order.UsageAdjustmentMetric,
		UsageAdjustmentActualQuantity:         order.UsageAdjustmentActualQuantity,
		UsageAdjustmentIncludedQuantity:       order.UsageAdjustmentIncludedQuantity,
		UsageAdjustmentBillableQuantity:       order.UsageAdjustmentBillableQuantity,
		UsageAdjustmentUserUnitMicrocredits:   order.UsageAdjustmentUserUnitMicrocredits,
		UsageAdjustmentUserAmountMicrocredits: order.UsageAdjustmentUserAmountMicrocredits,
	}
	encoded, err := json.Marshal(facts)
	if err != nil {
		return "", fmt.Errorf("编码积分报价事实: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}
