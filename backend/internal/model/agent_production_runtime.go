package model

import (
	"time"

	"infinite-canvas/backend/internal/agentruntime"
)

// AgentProductionGraphVersion is an immutable graph definition. A semantic
// edit creates the next Version instead of updating StagesJSON in place.
type AgentProductionGraphVersion struct {
	ID              string                  `json:"id" gorm:"primaryKey;size:80"`
	TenantKind      agentruntime.TenantKind `json:"tenantKind" gorm:"size:16;not null"`
	TenantID        string                  `json:"tenantId" gorm:"size:80;not null"`
	ActorUserID     string                  `json:"actorUserId" gorm:"size:80;not null"`
	DomainProjectID string                  `json:"domainProjectId" gorm:"size:80;not null;default:''"`
	CanvasID        string                  `json:"canvasId" gorm:"size:80;not null"`
	ThreadID        string                  `json:"threadId" gorm:"size:80;not null"`
	RunID           string                  `json:"runId" gorm:"size:80;not null"`
	GraphKey        string                  `json:"graphKey" gorm:"size:120;not null"`
	Version         int64                   `json:"version" gorm:"not null"`
	SchemaVersion   int                     `json:"schemaVersion" gorm:"not null"`
	StagesJSON      string                  `json:"-" gorm:"type:text;not null"`
	CreatedAt       time.Time               `json:"createdAt"`
}

// AgentProductionStage is the mutable CAS lifecycle for one stage in an
// immutable graph version. Content remains in exact Artifact revisions.
type AgentProductionStage struct {
	ID                   string                             `json:"id" gorm:"primaryKey;size:80"`
	TenantKind           agentruntime.TenantKind            `json:"tenantKind" gorm:"size:16;not null"`
	TenantID             string                             `json:"tenantId" gorm:"size:80;not null"`
	ActorUserID          string                             `json:"actorUserId" gorm:"size:80;not null"`
	DomainProjectID      string                             `json:"domainProjectId" gorm:"size:80;not null;default:''"`
	CanvasID             string                             `json:"canvasId" gorm:"size:80;not null"`
	ThreadID             string                             `json:"threadId" gorm:"size:80;not null"`
	RunID                string                             `json:"runId" gorm:"size:80;not null"`
	GraphVersionID       string                             `json:"graphVersionId" gorm:"size:80;not null;uniqueIndex:idx_agent_stage_graph_key,priority:1"`
	StageKey             string                             `json:"stageKey" gorm:"size:120;not null;uniqueIndex:idx_agent_stage_graph_key,priority:2"`
	SpecialistKey        agentruntime.SpecialistKey         `json:"specialistKey" gorm:"size:40;not null"`
	DependsOnStagesJSON  string                             `json:"-" gorm:"type:text;not null"`
	InputRevisionsJSON   string                             `json:"-" gorm:"type:text;not null"`
	ExpectedDeliveryJSON string                             `json:"-" gorm:"type:text;not null"`
	ReviewPolicy         agentruntime.ReviewPolicy          `json:"reviewPolicy" gorm:"size:32;not null"`
	CostPolicy           agentruntime.CostPolicy            `json:"costPolicy" gorm:"size:32;not null"`
	Status               agentruntime.ProductionStageStatus `json:"status" gorm:"size:32;not null;index"`
	Version              int64                              `json:"version" gorm:"not null"`
	ReviewRevisionID     string                             `json:"reviewRevisionId,omitempty" gorm:"size:80;not null;default:''"`
	LastErrorCode        string                             `json:"lastErrorCode,omitempty" gorm:"size:80;not null;default:''"`
	InputTokens          int64                              `json:"inputTokens" gorm:"not null;default:0"`
	CachedTokens         int64                              `json:"cachedTokens" gorm:"not null;default:0"`
	OutputTokens         int64                              `json:"outputTokens" gorm:"not null;default:0"`
	CreatedAt            time.Time                          `json:"createdAt"`
	UpdatedAt            time.Time                          `json:"updatedAt"`
}

type AgentSpecialistRunStatus string

const (
	AgentSpecialistRunQueued          AgentSpecialistRunStatus = "queued"
	AgentSpecialistRunRunning         AgentSpecialistRunStatus = "running"
	AgentSpecialistRunWaitingInput    AgentSpecialistRunStatus = "waiting_input"
	AgentSpecialistRunWaitingApproval AgentSpecialistRunStatus = "waiting_approval"
	AgentSpecialistRunWaitingTool     AgentSpecialistRunStatus = "waiting_tool"
	AgentSpecialistRunSucceeded       AgentSpecialistRunStatus = "succeeded"
	AgentSpecialistRunFailed          AgentSpecialistRunStatus = "failed"
	AgentSpecialistRunCancelled       AgentSpecialistRunStatus = "cancelled"
)

// AgentSpecialistRun persists the independently recoverable lifecycle of a
// specialist while retaining the parent AgentRun's exact model and scope.
type AgentSpecialistRun struct {
	ID                    string                     `json:"id" gorm:"primaryKey;size:80"`
	TenantKind            agentruntime.TenantKind    `json:"tenantKind" gorm:"size:16;not null"`
	TenantID              string                     `json:"tenantId" gorm:"size:80;not null"`
	ActorUserID           string                     `json:"actorUserId" gorm:"size:80;not null"`
	DomainProjectID       string                     `json:"domainProjectId" gorm:"size:80;not null;default:''"`
	CanvasID              string                     `json:"canvasId" gorm:"size:80;not null"`
	ThreadID              string                     `json:"threadId" gorm:"size:80;not null"`
	RunID                 string                     `json:"runId" gorm:"size:80;not null"`
	StageID               string                     `json:"stageId" gorm:"size:80;not null;index"`
	ParentSpecialistRunID string                     `json:"parentSpecialistRunId,omitempty" gorm:"size:80;not null;default:'';index"`
	SpecialistKey         agentruntime.SpecialistKey `json:"specialistKey" gorm:"size:40;not null"`
	SpecialistVersion     int                        `json:"specialistVersion" gorm:"not null"`
	Objective             string                     `json:"objective" gorm:"type:text;not null"`
	ModelRecordID         string                     `json:"modelRecordId" gorm:"size:80;not null"`
	ModelKey              string                     `json:"modelKey" gorm:"size:120;not null"`
	ToolSchemaVersion     int                        `json:"toolSchemaVersion" gorm:"not null"`
	InputRevisionsJSON    string                     `json:"-" gorm:"type:text;not null"`
	SkillVersionsJSON     string                     `json:"-" gorm:"type:text;not null"`
	ToolAllowlistJSON     string                     `json:"-" gorm:"type:text;not null"`
	ExpectedOutputSchema  string                     `json:"expectedOutputSchema" gorm:"size:160;not null"`
	ExpectedDeliveryJSON  string                     `json:"-" gorm:"type:text;not null"`
	TaskID                string                     `json:"taskId,omitempty" gorm:"size:36;not null;default:'';index"`
	BillingOrderID        string                     `json:"billingOrderId,omitempty" gorm:"size:36;not null;default:'';index"`
	Attempt               int                        `json:"attempt" gorm:"not null;default:0"`
	ProviderRequestID     string                     `json:"providerRequestId,omitempty" gorm:"size:160;not null;default:'';index"`
	InputTokens           int64                      `json:"inputTokens" gorm:"not null;default:0"`
	CachedTokens          int64                      `json:"cachedTokens" gorm:"not null;default:0"`
	OutputTokens          int64                      `json:"outputTokens" gorm:"not null;default:0"`
	Status                AgentSpecialistRunStatus   `json:"status" gorm:"size:32;not null;index"`
	Version               int64                      `json:"version" gorm:"not null"`
	LastHeartbeatAt       *time.Time                 `json:"lastHeartbeatAt,omitempty"`
	ResultSummary         string                     `json:"resultSummary,omitempty" gorm:"type:text;not null;default:''"`
	ResultJSON            string                     `json:"-" gorm:"type:text;not null;default:''"`
	ErrorCode             string                     `json:"errorCode,omitempty" gorm:"size:80;not null;default:''"`
	CreatedAt             time.Time                  `json:"createdAt"`
	UpdatedAt             time.Time                  `json:"updatedAt"`
	CompletedAt           *time.Time                 `json:"completedAt,omitempty"`
}

// AgentArtifact is the stable identity and mutable head pointer. Artifact
// content is never stored here; it is append-only in AgentArtifactRevision.
type AgentArtifact struct {
	ID              string                  `json:"id" gorm:"primaryKey;size:80"`
	TenantKind      agentruntime.TenantKind `json:"tenantKind" gorm:"size:16;not null"`
	TenantID        string                  `json:"tenantId" gorm:"size:80;not null"`
	ActorUserID     string                  `json:"actorUserId" gorm:"size:80;not null"`
	DomainProjectID string                  `json:"domainProjectId" gorm:"size:80;not null;default:''"`
	CanvasID        string                  `json:"canvasId" gorm:"size:80;not null"`
	ThreadID        string                  `json:"threadId" gorm:"size:80;not null"`
	RunID           string                  `json:"runId" gorm:"size:80;not null"`
	ArtifactKey     string                  `json:"artifactKey" gorm:"size:120;not null"`
	Kind            string                  `json:"kind" gorm:"size:120;not null"`
	HeadRevision    int64                   `json:"headRevision" gorm:"not null;default:0"`
	LifecycleStatus string                  `json:"lifecycleStatus" gorm:"size:32;not null;index"`
	Version         int64                   `json:"version" gorm:"not null"`
	CreatedAt       time.Time               `json:"createdAt"`
	UpdatedAt       time.Time               `json:"updatedAt"`
}

const (
	AgentArtifactLifecycleActive    = "active"
	AgentArtifactLifecycleUnadopted = "unadopted"
)

const (
	AgentArtifactRevisionAwaitingReview = "awaiting_review"
	AgentArtifactRevisionUnadopted      = "unadopted"
	AgentArtifactRevisionStale          = "stale"
)

// AgentArtifactRevision is immutable after creation. It binds structured
// payload, Resource truth and exact upstream revisions into one audit fact.
type AgentArtifactRevision struct {
	ID                    string                  `json:"id" gorm:"primaryKey;size:80"`
	TenantKind            agentruntime.TenantKind `json:"tenantKind" gorm:"size:16;not null"`
	TenantID              string                  `json:"tenantId" gorm:"size:80;not null"`
	ActorUserID           string                  `json:"actorUserId" gorm:"size:80;not null"`
	DomainProjectID       string                  `json:"domainProjectId" gorm:"size:80;not null;default:''"`
	CanvasID              string                  `json:"canvasId" gorm:"size:80;not null"`
	ThreadID              string                  `json:"threadId" gorm:"size:80;not null"`
	RunID                 string                  `json:"runId" gorm:"size:80;not null"`
	ArtifactID            string                  `json:"artifactId" gorm:"size:80;not null;uniqueIndex:idx_agent_artifact_revision_number,priority:1"`
	ArtifactKey           string                  `json:"artifactKey" gorm:"size:120;not null"`
	Revision              int64                   `json:"revision" gorm:"not null;uniqueIndex:idx_agent_artifact_revision_number,priority:2"`
	Kind                  string                  `json:"kind" gorm:"size:120;not null"`
	SchemaVersion         int                     `json:"schemaVersion" gorm:"not null"`
	PayloadJSON           string                  `json:"-" gorm:"type:text;not null"`
	ResourceID            string                  `json:"resourceId,omitempty" gorm:"size:80;not null;default:'';index"`
	UpstreamRevisionsJSON string                  `json:"-" gorm:"type:text;not null"`
	ModelRequestIdentity  string                  `json:"modelRequestIdentity,omitempty" gorm:"size:180;not null;default:'';index"`
	SkillVersionsJSON     string                  `json:"-" gorm:"type:text;not null"`
	CreatedByRunID        string                  `json:"createdByRunId" gorm:"size:80;not null;index"`
	CreatedBySpecialistID string                  `json:"createdBySpecialistId,omitempty" gorm:"size:80;not null;default:'';index"`
	LifecycleStatus       string                  `json:"lifecycleStatus" gorm:"size:32;not null;index"`
	CreatedAt             time.Time               `json:"createdAt"`
}

// AgentAssetBindingRevision is an immutable snapshot mapping narrative roles,
// locations and props to exact approved media revisions or existing assets.
type AgentAssetBindingRevision struct {
	ID                    string                  `json:"id" gorm:"primaryKey;size:80"`
	TenantKind            agentruntime.TenantKind `json:"tenantKind" gorm:"size:16;not null"`
	TenantID              string                  `json:"tenantId" gorm:"size:80;not null"`
	ActorUserID           string                  `json:"actorUserId" gorm:"size:80;not null"`
	DomainProjectID       string                  `json:"domainProjectId" gorm:"size:80;not null;default:''"`
	CanvasID              string                  `json:"canvasId" gorm:"size:80;not null"`
	ThreadID              string                  `json:"threadId" gorm:"size:80;not null"`
	RunID                 string                  `json:"runId" gorm:"size:80;not null"`
	BindingKey            string                  `json:"bindingKey" gorm:"size:120;not null"`
	Revision              int64                   `json:"revision" gorm:"not null"`
	BindingsJSON          string                  `json:"-" gorm:"type:text;not null"`
	UpstreamRevisionsJSON string                  `json:"-" gorm:"type:text;not null"`
	CreatedBySpecialistID string                  `json:"createdBySpecialistId" gorm:"size:80;not null;index"`
	LifecycleStatus       string                  `json:"lifecycleStatus" gorm:"size:32;not null;index"`
	CreatedAt             time.Time               `json:"createdAt"`
}

const AgentAssetBindingRevisionConfirmed = "confirmed"

// AgentCharacterIdentityVersion is an append-only identity pointer. Character
// Bible content and media URLs remain owned by ArtifactRevision and Resource.
type AgentCharacterIdentityVersion struct {
	ID                       string                                   `json:"id" gorm:"primaryKey;size:80"`
	TenantKind               agentruntime.TenantKind                  `json:"tenantKind" gorm:"size:16;not null"`
	TenantID                 string                                   `json:"tenantId" gorm:"size:80;not null"`
	ActorUserID              string                                   `json:"actorUserId" gorm:"size:80;not null"`
	DomainProjectID          string                                   `json:"domainProjectId" gorm:"size:80;not null;default:''"`
	CanvasID                 string                                   `json:"canvasId" gorm:"size:80;not null"`
	ThreadID                 string                                   `json:"threadId" gorm:"size:80;not null"`
	RunID                    string                                   `json:"runId" gorm:"size:80;not null"`
	CharacterKey             string                                   `json:"characterKey" gorm:"size:120;not null"`
	Version                  int64                                    `json:"version" gorm:"not null"`
	CharacterBibleRevisionID string                                   `json:"characterBibleRevisionId" gorm:"size:80;not null;index"`
	ResourceID               string                                   `json:"resourceId" gorm:"size:80;not null;index"`
	DependencyHash           string                                   `json:"dependencyHash" gorm:"size:64;not null"`
	LifecycleStatus          agentruntime.ProductionEvidenceLifecycle `json:"lifecycleStatus" gorm:"size:32;not null;index"`
	CreatedAt                time.Time                                `json:"createdAt"`
}

// AgentShotBindingRevision pins one shot/character occurrence to exact
// identity, Artifact revision and Resource facts without duplicating content.
type AgentShotBindingRevision struct {
	ID                     string                                   `json:"id" gorm:"primaryKey;size:80"`
	TenantKind             agentruntime.TenantKind                  `json:"tenantKind" gorm:"size:16;not null"`
	TenantID               string                                   `json:"tenantId" gorm:"size:80;not null"`
	ActorUserID            string                                   `json:"actorUserId" gorm:"size:80;not null"`
	DomainProjectID        string                                   `json:"domainProjectId" gorm:"size:80;not null;default:''"`
	CanvasID               string                                   `json:"canvasId" gorm:"size:80;not null"`
	ThreadID               string                                   `json:"threadId" gorm:"size:80;not null"`
	RunID                  string                                   `json:"runId" gorm:"size:80;not null"`
	ShotKey                string                                   `json:"shotKey" gorm:"size:120;not null"`
	CharacterKey           string                                   `json:"characterKey" gorm:"size:120;not null"`
	Revision               int64                                    `json:"revision" gorm:"not null"`
	ShotArtifactRevisionID string                                   `json:"shotArtifactRevisionId" gorm:"size:80;not null;index"`
	IdentityVersionID      string                                   `json:"identityVersionId" gorm:"size:80;not null;index"`
	ResourceID             string                                   `json:"resourceId" gorm:"size:80;not null;index"`
	DependencyHash         string                                   `json:"dependencyHash" gorm:"size:64;not null"`
	LifecycleStatus        agentruntime.ProductionEvidenceLifecycle `json:"lifecycleStatus" gorm:"size:32;not null;index"`
	CreatedAt              time.Time                                `json:"createdAt"`
}

type AgentAssetPublicationStatus string

const (
	AgentAssetPublicationPending   AgentAssetPublicationStatus = "pending"
	AgentAssetPublicationSucceeded AgentAssetPublicationStatus = "succeeded"
	AgentAssetPublicationFailed    AgentAssetPublicationStatus = "failed"
)

// AgentAssetPublication is the idempotent bridge from an approved Artifact
// revision to the existing Asset/Version/Link/Representation truth.
type AgentAssetPublication struct {
	ID                 string                      `json:"id" gorm:"primaryKey;size:80"`
	TenantKind         agentruntime.TenantKind     `json:"tenantKind" gorm:"size:16;not null"`
	TenantID           string                      `json:"tenantId" gorm:"size:80;not null"`
	ActorUserID        string                      `json:"actorUserId" gorm:"size:80;not null"`
	DomainProjectID    string                      `json:"domainProjectId" gorm:"size:80;not null;default:''"`
	CanvasID           string                      `json:"canvasId" gorm:"size:80;not null"`
	ThreadID           string                      `json:"threadId" gorm:"size:80;not null"`
	RunID              string                      `json:"runId" gorm:"size:80;not null"`
	ArtifactRevisionID string                      `json:"artifactRevisionId" gorm:"size:80;not null;index"`
	PublicationPurpose string                      `json:"publicationPurpose" gorm:"size:120;not null"`
	BindingRevisionID  string                      `json:"bindingRevisionId,omitempty" gorm:"size:80;not null;default:'';index"`
	AssetID            string                      `json:"assetId,omitempty" gorm:"size:80;not null;default:'';index"`
	AssetVersionID     string                      `json:"assetVersionId,omitempty" gorm:"size:80;not null;default:'';index"`
	ProjectAssetLinkID string                      `json:"projectAssetLinkId,omitempty" gorm:"size:80;not null;default:'';index"`
	RepresentationID   string                      `json:"representationId,omitempty" gorm:"size:80;not null;default:'';index"`
	ApprovedByUserID   string                      `json:"approvedByUserId" gorm:"size:80;not null"`
	AuditJSON          string                      `json:"-" gorm:"type:text;not null"`
	Status             AgentAssetPublicationStatus `json:"status" gorm:"size:32;not null;index"`
	Version            int64                       `json:"version" gorm:"not null"`
	LastErrorCode      string                      `json:"lastErrorCode,omitempty" gorm:"size:80;not null;default:''"`
	CreatedAt          time.Time                   `json:"createdAt"`
	UpdatedAt          time.Time                   `json:"updatedAt"`
	CompletedAt        *time.Time                  `json:"completedAt,omitempty"`
}
