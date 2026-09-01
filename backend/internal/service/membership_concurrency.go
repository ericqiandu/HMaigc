package service

import (
	"fmt"
	"strings"

	"infinite-canvas/backend/internal/repository"
)

const (
	taskCapabilityImage  = "image"
	taskCapabilityVideo  = "video"
	taskCapabilityVision = "vision"
	taskCapabilityOther  = "other"
)

func taskConcurrencyCapability(taskType string) (string, error) {
	switch {
	case taskType == "canvas_image":
		return taskCapabilityImage, nil
	case taskType == "canvas_video" || strings.HasPrefix(taskType, "video_"):
		return taskCapabilityVideo, nil
	case taskType == agentVisualAnalysisTaskType:
		return taskCapabilityVision, nil
	case taskType == "canvas_audio",
		taskType == "canvas_text",
		taskType == "agent_session",
		taskType == agentRuntimeModelTaskType,
		taskType == agentSpecialistModelTaskType,
		taskType == "agent_storyboard",
		taskType == "agent_storyboard_rows":
		return taskCapabilityOther, nil
	default:
		return "", BadAuthRequest(fmt.Sprintf("任务类型 %q 未声明并发能力分类", taskType))
	}
}

func (s *Service) membershipActiveTaskPolicy(userID string, billingScope billingAccountScope, taskType string, runtimePolicy RuntimePolicySetting) (repository.ActiveTaskPolicy, string, error) {
	concurrencyClass, err := taskConcurrencyCapability(taskType)
	if err != nil {
		return repository.ActiveTaskPolicy{}, "", err
	}
	entitlement, err := s.membershipEntitlementForBillingAccount(userID, billingScope)
	if err != nil {
		return repository.ActiveTaskPolicy{}, "", err
	}
	capabilityLimit := runtimePolicy.Task.ActiveTaskLimit
	switch concurrencyClass {
	case taskCapabilityImage:
		capabilityLimit = entitlement.ImageConcurrency
	case taskCapabilityVideo:
		capabilityLimit = entitlement.VideoConcurrency
	}
	if capabilityLimit <= 0 {
		return repository.ActiveTaskPolicy{}, "", BadAuthRequest("当前会员套餐未配置有效的任务并发额度")
	}
	totalLimit := entitlement.ImageConcurrency + entitlement.VideoConcurrency + runtimePolicy.Task.ActiveTaskLimit
	billingTeamID := billingScope.TeamID
	return repository.ActiveTaskPolicy{
		TotalLimit:       totalLimit,
		ConcurrencyClass: concurrencyClass,
		Capabilities:     concurrencyClassCapabilities(concurrencyClass),
		ClassLimit:       capabilityLimit,
		Unlimited:        entitlement.UnlimitedTaskQueue,
		BillingTeamID:    &billingTeamID,
	}, concurrencyClass, nil
}

func concurrencyClassCapabilities(concurrencyClass string) []string {
	switch concurrencyClass {
	case taskCapabilityImage:
		return []string{"image"}
	case taskCapabilityVideo:
		return []string{"video"}
	case taskCapabilityVision:
		return []string{"vision"}
	case taskCapabilityOther:
		return []string{"audio", "text"}
	default:
		return nil
	}
}

func capabilityLimitMessage(capability string, limit int) string {
	label := "其他"
	switch capability {
	case taskCapabilityImage:
		label = "图片"
	case taskCapabilityVideo:
		label = "视频"
	case taskCapabilityVision:
		label = "视觉分析"
	}
	return fmt.Sprintf("当前会员套餐的%s任务并发上限为 %d，请等待已有任务完成或升级套餐", label, limit)
}
