package service

import (
	"fmt"
	"strings"

	"infinite-canvas/backend/internal/model"
)

func validateTaskWatermarkInput(input map[string]any) error {
	if _, exists := input["watermark"]; exists {
		return BadAuthRequest("水印由账号级设置决定，任务不能提交水印参数")
	}
	for _, section := range []string{"config", "metadata"} {
		value, exists := input[section]
		if !exists || value == nil {
			continue
		}
		fields, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if _, exists := fields["watermark"]; exists {
			return BadAuthRequest("水印由账号级设置决定，任务不能提交水印参数")
		}
		if _, exists := fields["videoWatermark"]; exists {
			return BadAuthRequest("水印由账号级设置决定，任务不能提交水印参数")
		}
	}
	return nil
}

func (s *Service) taskWatermarkCapability(taskCapability string, order *model.BillingOrder) (model.WatermarkCapability, error) {
	capability := normalizeCapability(taskCapability)
	if capability != "image" && capability != "video" {
		return model.WatermarkCapabilityNotApplicable, nil
	}
	if order == nil || strings.TrimSpace(order.ChannelModelID) == "" {
		return "", fmt.Errorf("媒体任务缺少模型水印能力事实")
	}
	channelModel, err := s.repo.ChannelModelByRecordID(order.ChannelModelID)
	if err != nil {
		return "", err
	}
	channel, err := s.repo.SystemChannel(channelModel.ChannelID)
	if err != nil {
		return "", err
	}
	return resolveTaskWatermarkCapability(capability, *channel, *channelModel)
}

func resolveTaskWatermarkCapability(taskCapability string, channel model.ModelChannel, channelModel model.ChannelModel) (model.WatermarkCapability, error) {
	if normalizeCapability(channelModel.Capability) != taskCapability {
		return "", fmt.Errorf("媒体模型 %s 的能力类型与任务不一致", channelModel.ModelKey)
	}
	if spec, ok := kuaiziProviderModelSpec(channelModel.ModelKey); ok {
		if spec.Capability != taskCapability {
			return "", fmt.Errorf("媒体模型 %s 的注册能力与任务不一致", channelModel.ModelKey)
		}
		return spec.WatermarkCapability, nil
	}
	if strings.TrimSpace(channelModel.ProviderCredentialID) != "" {
		return "", fmt.Errorf("媒体模型 %s 缺少明确的水印能力契约", channelModel.ModelKey)
	}
	switch {
	case taskCapability == "video" && channel.InterfaceType == model.ChannelInterfaceNewAPIVideo,
		taskCapability == "video" && channel.InterfaceType == model.ChannelInterfaceAIOpenVideoVolcengine,
		taskCapability == "video" && channel.InterfaceType == model.ChannelInterfaceMiniMaxVideo:
		return model.WatermarkCapabilityControlled, nil
	case taskCapability == "image" && channel.InterfaceType == model.ChannelInterfaceOpenAIImage,
		taskCapability == "image" && channel.InterfaceType == model.ChannelInterfaceAPIMartImage,
		taskCapability == "video" && channel.InterfaceType == model.ChannelInterfaceXAIVideo,
		taskCapability == "video" && channel.InterfaceType == model.ChannelInterfaceKlingVideo:
		return model.WatermarkCapabilityUnsupported, nil
	default:
		return "", fmt.Errorf("媒体模型 %s 缺少明确的水印能力契约", channelModel.ModelKey)
	}
}

func publicWatermarkCapability(channel model.ModelChannel, channelModel model.ChannelModel) model.WatermarkCapability {
	capability := normalizeCapability(channelModel.Capability)
	if capability != "image" && capability != "video" {
		return model.WatermarkCapabilityNotApplicable
	}
	resolved, err := resolveTaskWatermarkCapability(capability, channel, channelModel)
	if err != nil {
		return ""
	}
	return resolved
}
