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
	ID                     string                 `json:"id" gorm:"primaryKey;size:80"`
	ThreadID               string                 `json:"threadId" gorm:"size:80;not null"`
	ActorUserID            string                 `json:"actorUserId" gorm:"size:80;not null"`
	ClientRequestID        string                 `json:"clientRequestId" gorm:"size:120;not null"`
	Status                 agentruntime.RunStatus `json:"status" gorm:"size:32;not null"`
	LastEventSequence      int64                  `json:"lastEventSequence" gorm:"not null;default:0"`
	StateVersion           int                    `json:"stateVersion" gorm:"not null;default:0"`
	StepNumber             int                    `json:"stepNumber" gorm:"not null;default:0"`
	MaxSteps               int                    `json:"maxSteps" gorm:"not null;default:0"`
	ModelRecordID          string                 `json:"modelRecordId" gorm:"size:80;not null;default:''"`
	ModelKey               string                 `json:"modelKey" gorm:"size:120;not null;default:''"`
	ToolSchemaVersion      int                    `json:"toolSchemaVersion" gorm:"not null;default:0"`
	RuntimeVersion         int                    `json:"runtimeVersion" gorm:"not null;default:0"`
	PolicyVersion          int                    `json:"policyVersion" gorm:"not null;default:0"`
	SpecialistInputTokens  int64                  `json:"specialistInputTokens" gorm:"not null;default:0"`
	SpecialistCachedTokens int64                  `json:"specialistCachedTokens" gorm:"not null;default:0"`
	SpecialistOutputTokens int64                  `json:"specialistOutputTokens" gorm:"not null;default:0"`
	CreatedAt              time.Time              `json:"createdAt"`
	UpdatedAt              time.Time              `json:"updatedAt"`
	CompletedAt            *time.Time             `json:"completedAt,omitempty"`
}

type AgentTimelineItemKind string

const (
	AgentTimelineItemUserMessage   AgentTimelineItemKind = "user_message"
	AgentTimelineItemAgentMessage  AgentTimelineItemKind = "agent_message"
	AgentTimelineItemStatusKind    AgentTimelineItemKind = "status"
	AgentTimelineItemClarification AgentTimelineItemKind = "clarification"
	AgentTimelineItemToolCall      AgentTimelineItemKind = "tool_call"
	AgentTimelineItemToolResult    AgentTimelineItemKind = "tool_result"
	AgentTimelineItemApproval      AgentTimelineItemKind = "approval"
	AgentTimelineItemArtifact      AgentTimelineItemKind = "artifact"
	AgentTimelineItemError         AgentTimelineItemKind = "error"
)

func (kind AgentTimelineItemKind) Valid() bool {
	switch kind {
	case AgentTimelineItemUserMessage, AgentTimelineItemAgentMessage, AgentTimelineItemStatusKind,
		AgentTimelineItemClarification, AgentTimelineItemToolCall, AgentTimelineItemToolResult,
		AgentTimelineItemApproval, AgentTimelineItemArtifact, AgentTimelineItemError:
		return true
	default:
		return false
	}
}

type AgentTimelineItemStatus string

const (
	AgentTimelineItemInProgress  AgentTimelineItemStatus = "in_progress"
	AgentTimelineItemCompleted   AgentTimelineItemStatus = "completed"
	AgentTimelineItemFailed      AgentTimelineItemStatus = "failed"
	AgentTimelineItemDeclined    AgentTimelineItemStatus = "declined"
	AgentTimelineItemInterrupted AgentTimelineItemStatus = "interrupted"
)

func (status AgentTimelineItemStatus) Valid() bool {
	switch status {
	case AgentTimelineItemInProgress, AgentTimelineItemCompleted, AgentTimelineItemFailed,
		AgentTimelineItemDeclined, AgentTimelineItemInterrupted:
		return true
	default:
		return false
	}
}

type AgentTimelineItem struct {
	ID                  string                  `json:"id" gorm:"primaryKey;size:80"`
	TenantKind          agentruntime.TenantKind `json:"tenantKind" gorm:"size:16;not null"`
	TenantID            string                  `json:"tenantId" gorm:"size:80;not null"`
	ThreadID            string                  `json:"threadId" gorm:"size:80;not null"`
	RunID               string                  `json:"runId" gorm:"size:80;not null"`
	Kind                AgentTimelineItemKind   `json:"kind" gorm:"size:32;not null"`
	Status              AgentTimelineItemStatus `json:"status" gorm:"size:24;not null"`
	Ordinal             int64                   `json:"ordinal" gorm:"not null"`
	SourceEventSequence int64                   `json:"sourceEventSequence" gorm:"not null"`
	ContentJSON         string                  `json:"-" gorm:"type:text;not null"`
	StartedAt           time.Time               `json:"startedAt"`
	CompletedAt         *time.Time              `json:"completedAt,omitempty"`
	CreatedAt           time.Time               `json:"createdAt"`
	UpdatedAt           time.Time               `json:"updatedAt"`
}

type AgentProductionPlanStatus string

const (
	AgentProductionPlanActive     AgentProductionPlanStatus = "active"
	AgentProductionPlanSuperseded AgentProductionPlanStatus = "superseded"
)

type AgentProductionArtifactKind string

const (
	AgentProductionArtifactScript          AgentProductionArtifactKind = "script"
	AgentProductionArtifactReferenceImage  AgentProductionArtifactKind = "reference_image"
	AgentProductionArtifactStoryboardImage AgentProductionArtifactKind = "storyboard_image"
	AgentProductionArtifactVideoClip       AgentProductionArtifactKind = "video_clip"
)

type AgentProductionArtifactStatus string

const (
	AgentProductionArtifactPlanned          AgentProductionArtifactStatus = "planned"
	AgentProductionArtifactAwaitingApproval AgentProductionArtifactStatus = "awaiting_approval"
	AgentProductionArtifactQueued           AgentProductionArtifactStatus = "queued"
	AgentProductionArtifactRunning          AgentProductionArtifactStatus = "running"
	AgentProductionArtifactSucceeded        AgentProductionArtifactStatus = "succeeded"
	AgentProductionArtifactFailed           AgentProductionArtifactStatus = "failed"
	AgentProductionArtifactCommitted        AgentProductionArtifactStatus = "committed"
)

func (status AgentProductionArtifactStatus) Valid() bool {
	switch status {
	case AgentProductionArtifactPlanned, AgentProductionArtifactAwaitingApproval,
		AgentProductionArtifactQueued, AgentProductionArtifactRunning,
		AgentProductionArtifactSucceeded, AgentProductionArtifactFailed,
		AgentProductionArtifactCommitted:
		return true
	default:
		return false
	}
}

// AgentProductionPlanVersion is an immutable production plan snapshot. Only Status
// changes when a newer version supersedes it; the plan content is append-only.
type AgentProductionPlanVersion struct {
	ID                   string                    `json:"id" gorm:"primaryKey;size:80"`
	PlanKey              string                    `json:"planKey" gorm:"size:120;not null"`
	TenantKind           agentruntime.TenantKind   `json:"tenantKind" gorm:"size:16;not null"`
	TenantID             string                    `json:"tenantId" gorm:"size:80;not null"`
	DomainProjectID      string                    `json:"domainProjectId,omitempty" gorm:"size:80;not null;default:''"`
	CanvasID             string                    `json:"canvasId" gorm:"size:80;not null"`
	CreatedByRunID       string                    `json:"createdByRunId" gorm:"size:80;not null"`
	Version              int                       `json:"version" gorm:"not null"`
	Status               AgentProductionPlanStatus `json:"status" gorm:"size:24;not null"`
	Title                string                    `json:"title" gorm:"size:240;not null"`
	TargetDurationMS     int                       `json:"targetDurationMs" gorm:"not null"`
	Script               string                    `json:"script" gorm:"type:text;not null"`
	ReferencesJSON       string                    `json:"-" gorm:"type:text;not null;default:'[]'"`
	ShotsJSON            string                    `json:"-" gorm:"type:text;not null"`
	ExpectedDeliveryJSON string                    `json:"-" gorm:"type:text;not null"`
	CreatedAt            time.Time                 `json:"createdAt"`
	UpdatedAt            time.Time                 `json:"updatedAt"`
}

// AgentProductionArtifact is the durable ledger entry for one plan-level or
// shot-level deliverable. Empty ShotKey denotes the single plan-level script.
type AgentProductionArtifact struct {
	ID             string                        `json:"id" gorm:"primaryKey;size:80"`
	PlanKey        string                        `json:"planKey" gorm:"size:120;not null"`
	PlanVersionID  string                        `json:"planVersionId" gorm:"size:80;not null"`
	PlanVersion    int                           `json:"planVersion" gorm:"not null"`
	ReferenceKey   string                        `json:"referenceKey,omitempty" gorm:"size:120;not null;default:''"`
	ShotKey        string                        `json:"shotKey" gorm:"size:120;not null;default:''"`
	Kind           AgentProductionArtifactKind   `json:"kind" gorm:"size:32;not null"`
	Status         AgentProductionArtifactStatus `json:"status" gorm:"size:32;not null"`
	Attempt        int                           `json:"attempt" gorm:"not null;default:0"`
	CanvasNodeID   string                        `json:"canvasNodeId" gorm:"size:120;not null;default:''"`
	TaskID         string                        `json:"taskId" gorm:"size:80;not null;default:''"`
	BillingOrderID string                        `json:"billingOrderId" gorm:"size:80;not null;default:''"`
	ResourceID     string                        `json:"resourceId" gorm:"size:80;not null;default:''"`
	LastErrorCode  string                        `json:"lastErrorCode" gorm:"size:80;not null;default:''"`
	CreatedAt      time.Time                     `json:"createdAt"`
	UpdatedAt      time.Time                     `json:"updatedAt"`
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
	ID                string                            `json:"id" gorm:"primaryKey;size:80"`
	RunID             string                            `json:"runId" gorm:"size:80;not null"`
	ToolCallID        string                            `json:"toolCallId" gorm:"size:120;not null"`
	ActionVersion     int                               `json:"actionVersion" gorm:"not null"`
	ToolName          string                            `json:"toolName" gorm:"size:120;not null"`
	Status            agentruntime.ToolCallStatus       `json:"status" gorm:"size:32;not null"`
	RiskLevel         agentruntime.ToolRiskLevel        `json:"riskLevel" gorm:"size:8;not null;default:''"`
	RequiredAccess    agentruntime.AccessLevel          `json:"requiredAccess" gorm:"size:16;not null;default:''"`
	ApprovalRequired  bool                              `json:"approvalRequired" gorm:"not null;default:false"`
	ApprovalDecision  agentruntime.ToolApprovalDecision `json:"approvalDecision,omitempty" gorm:"size:16;not null;default:''"`
	ApprovalByUserID  string                            `json:"approvalByUserId,omitempty" gorm:"size:80;not null;default:''"`
	ApprovalDecidedAt *time.Time                        `json:"approvalDecidedAt,omitempty"`
	IdempotencyKey    string                            `json:"idempotencyKey" gorm:"size:256;not null;default:''"`
	InputJSON         string                            `json:"-" gorm:"type:text;not null"`
	OutputJSON        string                            `json:"-" gorm:"type:text;not null"`
	ErrorCode         string                            `json:"errorCode,omitempty" gorm:"size:80;not null;default:''"`
	StartedAt         *time.Time                        `json:"startedAt,omitempty"`
	CreatedAt         time.Time                         `json:"createdAt"`
	UpdatedAt         time.Time                         `json:"updatedAt"`
}
