package model

import (
	"time"

	"infinite-canvas/backend/internal/agentruntime"
)

type AgentThread struct {
	ID              string                    `json:"id" gorm:"primaryKey;size:80"`
	TenantKind      agentruntime.TenantKind   `json:"tenantKind" gorm:"size:16;not null"`
	TenantID        string                    `json:"tenantId" gorm:"size:80;not null"`
	CreatedByUserID string                    `json:"createdByUserId" gorm:"size:80;not null"`
	DomainProjectID string                    `json:"domainProjectId,omitempty" gorm:"size:80;not null;default:''"`
	CanvasID        string                    `json:"canvasId" gorm:"size:80;not null"`
	Status          agentruntime.ThreadStatus `json:"status" gorm:"size:24;not null"`
	CreatedAt       time.Time                 `json:"createdAt"`
	UpdatedAt       time.Time                 `json:"updatedAt"`
}

type AgentRun struct {
	ID                string                 `json:"id" gorm:"primaryKey;size:80"`
	ThreadID          string                 `json:"threadId" gorm:"size:80;not null"`
	ActorUserID       string                 `json:"actorUserId" gorm:"size:80;not null"`
	ClientRequestID   string                 `json:"clientRequestId" gorm:"size:120;not null"`
	Status            agentruntime.RunStatus `json:"status" gorm:"size:32;not null"`
	LastEventSequence int64                  `json:"lastEventSequence" gorm:"not null;default:0"`
	StepNumber        int                    `json:"stepNumber" gorm:"not null;default:0"`
	MaxSteps          int                    `json:"maxSteps" gorm:"not null;default:0"`
	ModelRecordID     string                 `json:"modelRecordId" gorm:"size:80;not null;default:''"`
	ModelKey          string                 `json:"modelKey" gorm:"size:120;not null;default:''"`
	ToolSchemaVersion int                    `json:"toolSchemaVersion" gorm:"not null;default:0"`
	CreatedAt         time.Time              `json:"createdAt"`
	UpdatedAt         time.Time              `json:"updatedAt"`
	CompletedAt       *time.Time             `json:"completedAt,omitempty"`
}

type AgentRunEvent struct {
	ID          string                 `json:"id" gorm:"primaryKey;size:80"`
	RunID       string                 `json:"runId" gorm:"size:80;not null"`
	Sequence    int64                  `json:"sequence" gorm:"not null"`
	Kind        agentruntime.EventKind `json:"kind" gorm:"size:48;not null"`
	PayloadJSON string                 `json:"-" gorm:"type:text;not null"`
	CreatedAt   time.Time              `json:"createdAt"`
}

type AgentCheckpoint struct {
	ID           string    `json:"id" gorm:"primaryKey;size:80"`
	RunID        string    `json:"runId" gorm:"size:80;not null"`
	Sequence     int64     `json:"sequence" gorm:"not null"`
	StateVersion int       `json:"stateVersion" gorm:"not null"`
	StateJSON    string    `json:"-" gorm:"type:text;not null"`
	CreatedAt    time.Time `json:"createdAt"`
}

type AgentToolCall struct {
	ID            string                      `json:"id" gorm:"primaryKey;size:80"`
	RunID         string                      `json:"runId" gorm:"size:80;not null"`
	ToolCallID    string                      `json:"toolCallId" gorm:"size:120;not null"`
	ActionVersion int                         `json:"actionVersion" gorm:"not null"`
	ToolName      string                      `json:"toolName" gorm:"size:120;not null"`
	Status        agentruntime.ToolCallStatus `json:"status" gorm:"size:32;not null"`
	InputJSON     string                      `json:"-" gorm:"type:text;not null"`
	OutputJSON    string                      `json:"-" gorm:"type:text;not null"`
	ErrorCode     string                      `json:"errorCode,omitempty" gorm:"size:80;not null;default:''"`
	CreatedAt     time.Time                   `json:"createdAt"`
	UpdatedAt     time.Time                   `json:"updatedAt"`
}
