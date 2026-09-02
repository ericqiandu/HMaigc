package model

import "time"

type TaskOutboxStatus string
type TaskOutboxEventType string

const (
	TaskOutboxPending    TaskOutboxStatus = "pending"
	TaskOutboxProcessing TaskOutboxStatus = "processing"
	TaskOutboxDelivered  TaskOutboxStatus = "delivered"

	TaskOutboxAgentRunWakeup TaskOutboxEventType = "agent_run_wakeup"
)

// TaskOutbox records a post-commit side effect without making that side effect
// part of the database transaction. Delivery is leased and idempotent so a
// process crash can only cause a replay, never a lost Task terminal fact.
type TaskOutbox struct {
	ID             string              `json:"id" gorm:"primaryKey;size:36"`
	IdempotencyKey string              `json:"idempotencyKey" gorm:"not null;size:200;uniqueIndex"`
	TaskID         string              `json:"taskId" gorm:"not null;size:36;index"`
	EventType      TaskOutboxEventType `json:"eventType" gorm:"not null;size:48;index"`
	PayloadJSON    string              `json:"payloadJson" gorm:"not null;type:text"`
	Status         TaskOutboxStatus    `json:"status" gorm:"not null;size:24;index:idx_task_outbox_delivery,priority:1;check:task_outbox_status_valid,status IN ('pending','processing','delivered')"`
	AttemptCount   int                 `json:"attemptCount"`
	AvailableAt    time.Time           `json:"availableAt" gorm:"not null;index:idx_task_outbox_delivery,priority:2"`
	LeaseOwner     string              `json:"-" gorm:"not null;default:'';size:120"`
	LeaseToken     string              `json:"-" gorm:"not null;default:'';size:64"`
	LeaseExpiresAt *time.Time          `json:"-" gorm:"index"`
	LastError      string              `json:"lastError,omitempty" gorm:"type:text"`
	DeliveredAt    *time.Time          `json:"deliveredAt,omitempty"`
	CreatedAt      time.Time           `json:"createdAt"`
	UpdatedAt      time.Time           `json:"updatedAt"`
}
