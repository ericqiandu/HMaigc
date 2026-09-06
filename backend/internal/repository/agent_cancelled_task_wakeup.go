package repository

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
	"infinite-canvas/backend/internal/model"
)

// 取消、退款与唤醒必须同事务提交；执行器只收取既有任务的最终事实。
func enqueueCancelledAgentTaskWakeup(tx *gorm.DB, runID string, target adminAgentRunControlTarget, now time.Time) error {
	payload, err := json.Marshal(struct {
		TaskID      string `json:"taskId"`
		RunID       string `json:"runId"`
		ActorUserID string `json:"actorUserId"`
		Wakeup      string `json:"wakeup"`
	}{TaskID: target.TaskID, RunID: runID, ActorUserID: target.UserID, Wakeup: "generation_task_finished"})
	if err != nil {
		return err
	}
	return enqueueTaskOutboxTx(tx, target.TaskID, TaskOutboxDraft{
		IdempotencyKey: "agent-run:" + runID + ":task:" + target.TaskID + ":terminal",
		EventType:      model.TaskOutboxAgentRunWakeup, PayloadJSON: string(payload), AvailableAt: now,
	}, now)
}
