package service

import (
	"strings"

	"infinite-canvas/backend/internal/model"
)

const providerResultRecoveryPollStage = "provider_resume"

func isSettledProviderResultRecovery(task *model.Task, order *model.BillingOrder) bool {
	if task == nil || order == nil || task.Status != model.TaskStatusRunning ||
		strings.TrimSpace(task.PollStage) != providerResultRecoveryPollStage ||
		order.Status != model.BillingStatusSettled || strings.TrimSpace(order.ProviderTaskStatus) != "succeeded" ||
		order.TaskID != task.ID {
		return false
	}
	taskProviderRequestID := strings.TrimSpace(task.ProviderRequestID)
	return taskProviderRequestID != "" && taskProviderRequestID == strings.TrimSpace(order.ProviderRequestID)
}
