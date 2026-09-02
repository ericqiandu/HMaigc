package agentruntime

import (
	"errors"
	"strings"
)

type TenantKind string

type ReasoningHost string

const (
	ReasoningHostManaged    ReasoningHost = "managed"
	ReasoningHostLocalCodex ReasoningHost = "local_codex"
)

func (host ReasoningHost) Valid() bool {
	return host == ReasoningHostManaged || host == ReasoningHostLocalCodex
}

const (
	TenantPersonal TenantKind = "personal"
	TenantTeam     TenantKind = "team"
)

func (kind TenantKind) Valid() bool {
	return kind == TenantPersonal || kind == TenantTeam
}

type AccessLevel string

const (
	AccessViewer  AccessLevel = "viewer"
	AccessEditor  AccessLevel = "editor"
	AccessManager AccessLevel = "manager"
)

func (level AccessLevel) Valid() bool {
	return level == AccessViewer || level == AccessEditor || level == AccessManager
}

type ThreadStatus string

const (
	ThreadActive   ThreadStatus = "active"
	ThreadArchived ThreadStatus = "archived"
)

func (status ThreadStatus) Valid() bool {
	return status == ThreadActive || status == ThreadArchived
}

type RunStatus string

const (
	ProductionRuntimeVersion         = 4
	ProductionPolicyVersion          = 4
	ProductionToolSchemaVersion      = 5
	CurrentProductionSchemaVersion   = 2
	ProductionAgentUIProtocolVersion = 4
	CloudRuntimeVersion              = 5
	CloudPolicyVersion               = 5
	CloudToolSchemaVersion           = 8
	CloudAgentUIProtocolVersion      = 5
	RetiredCloudToolSchemaVersion    = 7
	RetiredRuntimeVersion            = 3
	RetiredPolicyVersion             = 3
	RetiredToolSchemaVersion         = 4
	RetiredProductionSchemaVersion   = 1
	RetiredAgentUIProtocolVersion    = 3
	LegacyRuntimeVersion             = 2
	LegacyPolicyVersion              = 2
	LegacyToolSchemaVersion          = 3

	// Current* is a hard cut to the cloud capability runtime. Production*
	// remains an immutable historical fact for v5 audit records only.
	CurrentRuntimeVersion    = CloudRuntimeVersion
	CurrentPolicyVersion     = CloudPolicyVersion
	CurrentToolSchemaVersion = CloudToolSchemaVersion

	FailureRuntimeSchemaRetired = "runtime_schema_retired"

	RunQueued          RunStatus = "queued"
	RunRunning         RunStatus = "running"
	RunWaitingInput    RunStatus = "waiting_input"
	RunWaitingApproval RunStatus = "waiting_approval"
	RunWaitingTool     RunStatus = "waiting_tool"
	RunSucceeded       RunStatus = "succeeded"
	RunFailed          RunStatus = "failed"
	RunCancelled       RunStatus = "cancelled"
)

type SpecialistKey string

const (
	SpecialistNarrative     SpecialistKey = "narrative"
	SpecialistAsset         SpecialistKey = "asset"
	SpecialistStoryboard    SpecialistKey = "storyboard"
	SpecialistVisual        SpecialistKey = "visual"
	SpecialistVideoAssembly SpecialistKey = "video_assembly"
	SpecialistAudio         SpecialistKey = "audio"
)

func (key SpecialistKey) Valid() bool {
	switch key {
	case SpecialistNarrative, SpecialistAsset, SpecialistStoryboard, SpecialistVisual, SpecialistVideoAssembly, SpecialistAudio:
		return true
	default:
		return false
	}
}

type ProductionStageStatus string

const (
	StagePlanned        ProductionStageStatus = "planned"
	StageRunning        ProductionStageStatus = "running"
	StageAwaitingReview ProductionStageStatus = "awaiting_review"
	StageApproved       ProductionStageStatus = "approved"
	StageCompleted      ProductionStageStatus = "completed"
	StageFailed         ProductionStageStatus = "failed"
	StageStopped        ProductionStageStatus = "stopped"
	StageStale          ProductionStageStatus = "stale"
)

func (status ProductionStageStatus) Valid() bool {
	switch status {
	case StagePlanned, StageRunning, StageAwaitingReview, StageApproved, StageCompleted, StageFailed, StageStopped, StageStale:
		return true
	default:
		return false
	}
}

type StageReviewDecision string

const (
	StageReviewApprove         StageReviewDecision = "approved"
	StageReviewRequestRevision StageReviewDecision = "revision_requested"
	StageReviewStop            StageReviewDecision = "stopped"
)

func (decision StageReviewDecision) Valid() bool {
	return decision == StageReviewApprove || decision == StageReviewRequestRevision || decision == StageReviewStop
}

type ReviewPolicy string

const ReviewRequired ReviewPolicy = "required"

func (policy ReviewPolicy) Valid() bool {
	return policy == ReviewRequired
}

type CostPolicy string

const (
	CostNone             CostPolicy = "none"
	CostApprovalRequired CostPolicy = "approval_required"
)

func (policy CostPolicy) Valid() bool {
	return policy == CostNone || policy == CostApprovalRequired
}

// AgentToolName keeps one canonical decoder-facing tool identity across
// current cloud capabilities and immutable historical audit records.
type AgentToolName = ToolName

const (
	ToolSpecialistDelegate AgentToolName = "specialist.delegate"
	ToolVisionAnalyze      AgentToolName = "vision.analyze"
	ToolCanvasProject      AgentToolName = "canvas.project"
)

type StageReviewCommand struct {
	StageVersion      int64                   `json:"stageVersion"`
	RevisionID        string                  `json:"revisionId"`
	Decision          StageReviewDecision     `json:"decision"`
	ClientRequestID   string                  `json:"clientRequestId"`
	Comment           string                  `json:"comment,omitempty"`
	PublicationIntent *AssetPublicationIntent `json:"publicationIntent,omitempty"`
}

func (status RunStatus) Valid() bool {
	switch status {
	case RunQueued, RunRunning, RunWaitingInput, RunWaitingApproval, RunWaitingTool, RunSucceeded, RunFailed, RunCancelled:
		return true
	default:
		return false
	}
}

type ToolCallStatus string

const (
	ToolCallPending         ToolCallStatus = "pending"
	ToolCallWaitingApproval ToolCallStatus = "waiting_approval"
	ToolCallRunning         ToolCallStatus = "running"
	ToolCallSucceeded       ToolCallStatus = "succeeded"
	ToolCallFailed          ToolCallStatus = "failed"
)

func (status ToolCallStatus) Valid() bool {
	switch status {
	case ToolCallPending, ToolCallWaitingApproval, ToolCallRunning, ToolCallSucceeded, ToolCallFailed:
		return true
	default:
		return false
	}
}

type EventKind string

const (
	EventRunCreated               EventKind = "run.created"
	EventUserMessageAdded         EventKind = "user.message"
	EventRunStatusChanged         EventKind = "run.status_changed"
	EventRunSteered               EventKind = "run.steered"
	EventRunInterrupted           EventKind = "run.interrupted"
	EventModelDelta               EventKind = "model.delta"
	EventModelRejected            EventKind = "model.rejected"
	EventClarificationRequested   EventKind = "clarification.requested"
	EventClarificationAnswerSaved EventKind = "clarification.answer_saved"
	EventClarificationResponded   EventKind = "clarification.responded"
	EventToolCall                 EventKind = "tool.call"
	EventApprovalRequired         EventKind = "approval.required"
	EventApprovalDecided          EventKind = "approval.decided"
	EventToolStarted              EventKind = "tool.started"
	EventToolResult               EventKind = "tool.result"
	EventCheckpointSaved          EventKind = "checkpoint.saved"
	EventRunCompleted             EventKind = "run.completed"
	EventRunFailed                EventKind = "run.failed"
	EventAgentMessageCompleted    EventKind = "agent.message"
	EventAgentMessageFailed       EventKind = "agent.message_failed"
	EventArtifactAvailable        EventKind = "artifact.available"
)

var (
	ErrSteerConflict     = errors.New("agent steer conflict")
	ErrInterruptConflict = errors.New("agent interrupt conflict")
)

func (kind EventKind) Valid() bool {
	switch kind {
	case EventRunCreated, EventUserMessageAdded, EventRunStatusChanged, EventRunSteered, EventRunInterrupted,
		EventModelDelta, EventModelRejected,
		EventClarificationRequested, EventClarificationAnswerSaved, EventClarificationResponded,
		EventToolCall, EventApprovalRequired, EventApprovalDecided, EventToolStarted, EventToolResult,
		EventCheckpointSaved, EventRunCompleted, EventRunFailed, EventAgentMessageCompleted, EventAgentMessageFailed, EventArtifactAvailable:
		return true
	default:
		return false
	}
}

type AccessGrant struct {
	Level              AccessLevel
	SubscriptionActive bool
}

type Scope struct {
	TenantKind      TenantKind
	TenantID        string
	ActorUserID     string
	DomainProjectID string
	CanvasID        string
	ThreadID        string
	RunID           string
	Access          AccessGrant
}

func (scope Scope) Validate() error {
	if !scope.TenantKind.Valid() {
		return errors.New("agent scope tenant kind is invalid")
	}
	if strings.TrimSpace(scope.TenantID) == "" {
		return errors.New("agent scope tenant id is required")
	}
	if strings.TrimSpace(scope.ActorUserID) == "" {
		return errors.New("agent scope actor user id is required")
	}
	if scope.TenantKind == TenantPersonal && scope.TenantID != scope.ActorUserID {
		return errors.New("personal agent scope tenant must equal actor")
	}
	if strings.TrimSpace(scope.CanvasID) == "" {
		return errors.New("agent scope canvas id is required")
	}
	if strings.TrimSpace(scope.ThreadID) == "" {
		return errors.New("agent scope thread id is required")
	}
	if strings.TrimSpace(scope.RunID) == "" {
		return errors.New("agent scope run id is required")
	}
	if !scope.Access.Level.Valid() {
		return errors.New("agent scope access level is invalid")
	}
	return nil
}

func (scope Scope) CanMutateCanvas() bool {
	if err := scope.Validate(); err != nil {
		return false
	}
	if scope.Access.Level != AccessEditor && scope.Access.Level != AccessManager {
		return false
	}
	return scope.TenantKind == TenantPersonal || scope.Access.SubscriptionActive
}
