package service

import (
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
)

func TestSettledProviderResultRecoveryRequiresExactDurableFacts(t *testing.T) {
	task := &model.Task{
		ID: "task", Status: model.TaskStatusRunning, ProviderRequestID: "provider-task", PollStage: providerResultRecoveryPollStage,
	}
	order := &model.BillingOrder{
		TaskID: task.ID, Status: model.BillingStatusSettled, ProviderRequestID: task.ProviderRequestID, ProviderTaskStatus: "succeeded",
	}
	if !isSettledProviderResultRecovery(task, order) {
		t.Fatal("exact settled provider result was not recognized as recoverable")
	}

	tests := map[string]func(*model.Task, *model.BillingOrder){
		"ordinary running task":    func(task *model.Task, _ *model.BillingOrder) { task.PollStage = "accepted" },
		"missing task provider id": func(task *model.Task, _ *model.BillingOrder) { task.ProviderRequestID = "" },
		"provider mismatch":        func(_ *model.Task, order *model.BillingOrder) { order.ProviderRequestID = "different" },
		"provider did not succeed": func(_ *model.Task, order *model.BillingOrder) { order.ProviderTaskStatus = "failed" },
		"billing not settled":      func(_ *model.Task, order *model.BillingOrder) { order.Status = model.BillingStatusUncertain },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			candidateTask := *task
			candidateOrder := *order
			mutate(&candidateTask, &candidateOrder)
			if isSettledProviderResultRecovery(&candidateTask, &candidateOrder) {
				t.Fatal("invalid recovery facts were accepted")
			}
		})
	}
}

func TestAgentTaskTerminalOutboxIsScopedToExecutionAttempt(t *testing.T) {
	task := model.Task{
		ID: "task", UserID: "user", Type: "canvas_video", Operation: "media_generation:run", Attempts: 1,
	}
	first, err := taskAgentRunOutboxDraft(task, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	task.Attempts = 2
	second, err := taskAgentRunOutboxDraft(task, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if first.IdempotencyKey == second.IdempotencyKey || !strings.Contains(first.IdempotencyKey, ":attempt:1:") ||
		!strings.Contains(second.IdempotencyKey, ":attempt:2:") {
		t.Fatalf("outbox keys do not preserve execution attempt: first=%q second=%q", first.IdempotencyKey, second.IdempotencyKey)
	}
}
