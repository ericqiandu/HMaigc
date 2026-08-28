package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrLegacyRunRetirementBlocked = errors.New("legacy agent run retirement is blocked")

type LegacyRunRetirementAudit struct {
	RuntimeVersion       int                    `json:"runtimeVersion"`
	Status               agentruntime.RunStatus `json:"status"`
	Count                int64                  `json:"count"`
	HasPendingApproval   bool                   `json:"hasPendingApproval"`
	HasPendingTool       bool                   `json:"hasPendingTool"`
	HasRunningTool       bool                   `json:"hasRunningTool"`
	HasPendingTask       bool                   `json:"hasPendingTask"`
	HasActiveArtifact    bool                   `json:"hasActiveArtifact"`
	HasUnresolvedBilling bool                   `json:"hasUnresolvedBilling"`
	HasCheckpointIssue   bool                   `json:"hasCheckpointIssue"`
}

type legacyRunRetirementFacts struct {
	PendingApproval   bool
	PendingTool       bool
	RunningTool       bool
	PendingTask       bool
	ActiveArtifact    bool
	UnresolvedBilling bool
	CheckpointReady   bool
}

type legacyRunRetirementAuditKey struct {
	RuntimeVersion int
	Status         agentruntime.RunStatus
}

type legacyRunRetirementPayload struct {
	FailureCode          string                 `json:"failureCode"`
	SourceRuntimeVersion int                    `json:"sourceRuntimeVersion"`
	TargetRuntimeVersion int                    `json:"targetRuntimeVersion"`
	OriginalStatus       agentruntime.RunStatus `json:"originalStatus"`
}

type legacyRunRetirementUpdates struct {
	Status            agentruntime.RunStatus `gorm:"column:status"`
	StateVersion      int                    `gorm:"column:state_version"`
	StepNumber        int                    `gorm:"column:step_number"`
	LastEventSequence int64                  `gorm:"column:last_event_sequence"`
	UpdatedAt         time.Time              `gorm:"column:updated_at"`
	CompletedAt       time.Time              `gorm:"column:completed_at"`
}

type legacyTimelineRetirementUpdates struct {
	Status      model.AgentTimelineItemStatus `gorm:"column:status"`
	CompletedAt time.Time                     `gorm:"column:completed_at"`
	UpdatedAt   time.Time                     `gorm:"column:updated_at"`
}

type legacyToolRetirementUpdates struct {
	Status     agentruntime.ToolCallStatus `gorm:"column:status"`
	OutputJSON string                      `gorm:"column:output_json"`
	ErrorCode  string                      `gorm:"column:error_code"`
	UpdatedAt  time.Time                   `gorm:"column:updated_at"`
}

func (r *Repository) AuditRetirableLegacyAgentRuns(sourceRuntimeVersion int, targetRuntimeVersion int) ([]LegacyRunRetirementAudit, error) {
	if err := validateLegacyRunRetirementBoundary(r.db, sourceRuntimeVersion, targetRuntimeVersion); err != nil {
		return nil, err
	}
	runs, err := legacyRunRetirementCandidates(r.db, sourceRuntimeVersion, targetRuntimeVersion, false)
	if err != nil {
		return nil, err
	}
	grouped := make(map[legacyRunRetirementAuditKey]LegacyRunRetirementAudit)
	for _, run := range runs {
		facts, err := inspectLegacyRunRetirementFacts(r.db, run)
		if err != nil {
			return nil, err
		}
		key := legacyRunRetirementAuditKey{RuntimeVersion: run.RuntimeVersion, Status: run.Status}
		entry := grouped[key]
		entry.RuntimeVersion = run.RuntimeVersion
		entry.Status = run.Status
		entry.Count++
		entry.HasPendingApproval = entry.HasPendingApproval || facts.PendingApproval
		entry.HasPendingTool = entry.HasPendingTool || facts.PendingTool
		entry.HasRunningTool = entry.HasRunningTool || facts.RunningTool
		entry.HasPendingTask = entry.HasPendingTask || facts.PendingTask
		entry.HasActiveArtifact = entry.HasActiveArtifact || facts.ActiveArtifact
		entry.HasUnresolvedBilling = entry.HasUnresolvedBilling || facts.UnresolvedBilling
		entry.HasCheckpointIssue = entry.HasCheckpointIssue || !facts.CheckpointReady
		grouped[key] = entry
	}
	audit := make([]LegacyRunRetirementAudit, 0, len(grouped))
	for _, entry := range grouped {
		audit = append(audit, entry)
	}
	sort.Slice(audit, func(left int, right int) bool {
		if audit[left].RuntimeVersion != audit[right].RuntimeVersion {
			return audit[left].RuntimeVersion < audit[right].RuntimeVersion
		}
		return audit[left].Status < audit[right].Status
	})
	return audit, nil
}

func (r *Repository) RetireLegacyAgentRunsAtSafeBoundary(sourceRuntimeVersion int, targetRuntimeVersion int, failureCode string) (int64, error) {
	failureCode = strings.TrimSpace(failureCode)
	if failureCode != agentruntime.FailureRuntimeSchemaRetired {
		return 0, errors.New("legacy agent run retirement failure code is invalid")
	}
	if _, err := r.AuditRetirableLegacyAgentRuns(sourceRuntimeVersion, targetRuntimeVersion); err != nil {
		return 0, err
	}
	var retired int64
	now := time.Now().UTC()
	err := r.db.Transaction(func(tx *gorm.DB) error {
		runs, err := legacyRunRetirementCandidates(tx, sourceRuntimeVersion, targetRuntimeVersion, true)
		if err != nil {
			return err
		}
		for _, run := range runs {
			facts, err := inspectLegacyRunRetirementFacts(tx, run)
			if err != nil {
				return err
			}
			if !facts.CheckpointReady || facts.RunningTool || facts.PendingTask || facts.ActiveArtifact || facts.UnresolvedBilling {
				return fmt.Errorf("%w: run_id=%s checkpoint_ready=%t running_tool=%t pending_task=%t active_artifact=%t unresolved_billing=%t",
					ErrLegacyRunRetirementBlocked, run.ID, facts.CheckpointReady, facts.RunningTool, facts.PendingTask,
					facts.ActiveArtifact, facts.UnresolvedBilling)
			}
			if err := retireLegacyAgentRun(tx, run, sourceRuntimeVersion, targetRuntimeVersion, failureCode, now); err != nil {
				return err
			}
			retired++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return retired, nil
}

func validateLegacyRunRetirementBoundary(db *gorm.DB, sourceRuntimeVersion int, targetRuntimeVersion int) error {
	if sourceRuntimeVersion < 1 || targetRuntimeVersion <= sourceRuntimeVersion {
		return errors.New("legacy agent run retirement version boundary is invalid")
	}
	for _, table := range []string{
		"agent_production_graph_versions", "agent_production_stages", "agent_specialist_runs",
		"agent_artifacts", "agent_artifact_revisions", "agent_asset_binding_revisions", "agent_asset_publications",
	} {
		if !db.Migrator().HasTable(table) {
			return errors.New("agent production runtime schema marker is missing")
		}
	}
	return nil
}

func legacyRunRetirementCandidates(db *gorm.DB, sourceRuntimeVersion int, targetRuntimeVersion int, lock bool) ([]model.AgentRun, error) {
	query := db.Where("runtime_version = ? AND runtime_version < ? AND status IN ?", sourceRuntimeVersion, targetRuntimeVersion, []agentruntime.RunStatus{
		agentruntime.RunQueued, agentruntime.RunRunning, agentruntime.RunWaitingInput,
		agentruntime.RunWaitingApproval, agentruntime.RunWaitingTool,
	}).Order("created_at, id")
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var runs []model.AgentRun
	if err := query.Find(&runs).Error; err != nil {
		return nil, err
	}
	return runs, nil
}

func inspectLegacyRunRetirementFacts(db *gorm.DB, run model.AgentRun) (legacyRunRetirementFacts, error) {
	facts := legacyRunRetirementFacts{PendingApproval: run.Status == agentruntime.RunWaitingApproval}
	var checkpoint model.AgentCheckpoint
	err := db.Where("run_id = ? AND sequence = ? AND state_version = ?", run.ID, run.LastEventSequence, run.StateVersion).
		Take(&checkpoint).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return facts, err
	}
	if err == nil {
		state, decodeErr := decodeAgentCheckpointState(
			checkpoint.StateJSON, checkpoint.StateVersion, run.StateVersion, run.StepNumber, run.MaxSteps, run.Status,
		)
		if decodeErr == nil {
			facts.PendingTool = state.PendingToolCall != nil
			facts.RunningTool = state.PendingToolStarted
			if _, terminateErr := agentruntime.Terminate(state, agentruntime.FailureRuntimeSchemaRetired); terminateErr == nil {
				facts.CheckpointReady = true
			}
		}
	}

	var pendingTools []model.AgentToolCall
	if err := db.Select("status").Where("run_id = ? AND status IN ?", run.ID, []agentruntime.ToolCallStatus{
		agentruntime.ToolCallPending, agentruntime.ToolCallWaitingApproval, agentruntime.ToolCallRunning,
	}).Find(&pendingTools).Error; err != nil {
		return facts, err
	}
	for _, tool := range pendingTools {
		facts.PendingTool = true
		facts.PendingApproval = facts.PendingApproval || tool.Status == agentruntime.ToolCallWaitingApproval
		facts.RunningTool = facts.RunningTool || tool.Status == agentruntime.ToolCallRunning
	}

	planVersionIDs := db.Model(&model.AgentProductionPlanVersion{}).
		Select("id").Where("created_by_run_id = ?", run.ID)
	linkedTaskIDs := db.Model(&model.AgentProductionArtifact{}).
		Select("task_id").Where("plan_version_id IN (?) AND task_id <> ''", planVersionIDs)
	relatedTaskIDs := db.Model(&model.Task{}).
		Select("id").Where("operation = ? OR id IN (?)", "agent_model:"+run.ID, linkedTaskIDs)
	var pendingTaskCount int64
	if err := db.Model(&model.Task{}).Where("id IN (?) AND status IN ?", relatedTaskIDs, []model.TaskStatus{
		model.TaskStatusQueued, model.TaskStatusRunning,
	}).Count(&pendingTaskCount).Error; err != nil {
		return facts, err
	}
	facts.PendingTask = pendingTaskCount != 0

	var activeArtifactCount int64
	if err := db.Model(&model.AgentProductionArtifact{}).
		Where("plan_version_id IN (?) AND status NOT IN ?", planVersionIDs, []model.AgentProductionArtifactStatus{
			model.AgentProductionArtifactPlanned, model.AgentProductionArtifactAwaitingApproval,
			model.AgentProductionArtifactSucceeded, model.AgentProductionArtifactFailed,
			model.AgentProductionArtifactCommitted,
		}).Count(&activeArtifactCount).Error; err != nil {
		return facts, err
	}
	facts.ActiveArtifact = activeArtifactCount != 0

	linkedBillingIDs := db.Model(&model.AgentProductionArtifact{}).
		Select("billing_order_id").Where("plan_version_id IN (?) AND billing_order_id <> ''", planVersionIDs)
	var unresolvedBillingCount int64
	if err := db.Model(&model.BillingOrder{}).Where(
		"user_id = ? AND (idempotency_key LIKE ? OR idempotency_key LIKE ? OR id IN (?) OR task_id IN (?)) AND status IN ?",
		run.ActorUserID, "agent-runtime:"+run.ID+":%", "proxy-token:agent-runtime:"+run.ID+":%", linkedBillingIDs, relatedTaskIDs,
		[]model.BillingStatus{model.BillingStatusReserved, model.BillingStatusRunning, model.BillingStatusUncertain},
	).Count(&unresolvedBillingCount).Error; err != nil {
		return facts, err
	}
	facts.UnresolvedBilling = unresolvedBillingCount != 0
	return facts, nil
}

func retireLegacyAgentRun(db *gorm.DB, run model.AgentRun, sourceRuntimeVersion int, targetRuntimeVersion int, failureCode string, now time.Time) error {
	var thread model.AgentThread
	if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&thread, "id = ?", run.ThreadID).Error; err != nil {
		return err
	}
	scope := agentruntime.Scope{
		TenantKind: thread.TenantKind, TenantID: thread.TenantID, ActorUserID: run.ActorUserID,
		DomainProjectID: thread.DomainProjectID, CanvasID: thread.CanvasID, ThreadID: thread.ID, RunID: run.ID,
		Access: agentruntime.AccessGrant{Level: agentruntime.AccessManager, SubscriptionActive: true},
	}
	current, err := loadAgentCheckpointForScope(db, scope, true)
	if err != nil {
		return fmt.Errorf("%w: run_id=%s checkpoint_invalid: %v", ErrLegacyRunRetirementBlocked, run.ID, err)
	}
	transition, err := agentruntime.Terminate(current, failureCode)
	if err != nil {
		return fmt.Errorf("%w: run_id=%s unsafe_state: %v", ErrLegacyRunRetirementBlocked, run.ID, err)
	}
	stateJSON, err := json.Marshal(transition.State)
	if err != nil {
		return err
	}
	if len(stateJSON) > agentCheckpointPayloadLimit {
		return ErrAgentPayloadTooLarge
	}
	payload, err := json.Marshal(legacyRunRetirementPayload{
		FailureCode: failureCode, SourceRuntimeVersion: sourceRuntimeVersion,
		TargetRuntimeVersion: targetRuntimeVersion, OriginalStatus: run.Status,
	})
	if err != nil {
		return err
	}
	if len(payload) > agentEventPayloadLimit {
		return ErrAgentPayloadTooLarge
	}
	sequence := run.LastEventSequence + 1
	result := db.Model(&model.AgentRun{}).
		Where("id = ? AND status = ? AND state_version = ? AND step_number = ? AND last_event_sequence = ? AND runtime_version = ?",
			run.ID, run.Status, run.StateVersion, run.StepNumber, run.LastEventSequence, sourceRuntimeVersion).
		Select("status", "state_version", "step_number", "last_event_sequence", "updated_at", "completed_at").
		Updates(legacyRunRetirementUpdates{
			Status: transition.State.Status, StateVersion: transition.State.StateVersion, StepNumber: transition.State.StepNumber,
			LastEventSequence: sequence, UpdatedAt: now, CompletedAt: now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrAgentRuntimeStepConflict
	}
	event := model.AgentRunEvent{
		ID: agentFactID("event", run.ID, strconv.FormatInt(sequence, 10)), RunID: run.ID,
		Sequence: sequence, Kind: agentruntime.EventRunFailed, PayloadJSON: string(payload), CreatedAt: now,
	}
	if err := db.Create(&event).Error; err != nil {
		return err
	}
	checkpoint := model.AgentCheckpoint{
		ID: agentFactID("checkpoint", run.ID, strconv.FormatInt(sequence, 10)), RunID: run.ID,
		Sequence: sequence, StateVersion: transition.State.StateVersion, StateJSON: string(stateJSON), CreatedAt: now,
	}
	if err := db.Create(&checkpoint).Error; err != nil {
		return err
	}
	if err := db.Model(&model.AgentTimelineItem{}).Where("run_id = ? AND status = ?", run.ID, model.AgentTimelineItemInProgress).
		Select("status", "completed_at", "updated_at").
		Updates(legacyTimelineRetirementUpdates{Status: model.AgentTimelineItemInterrupted, CompletedAt: now, UpdatedAt: now}).Error; err != nil {
		return err
	}
	if err := db.Model(&model.AgentToolCall{}).Where("run_id = ? AND status IN ?", run.ID, []agentruntime.ToolCallStatus{
		agentruntime.ToolCallPending, agentruntime.ToolCallWaitingApproval,
	}).Select("status", "output_json", "error_code", "updated_at").Updates(legacyToolRetirementUpdates{
		Status: agentruntime.ToolCallFailed, OutputJSON: string(payload),
		ErrorCode: failureCode, UpdatedAt: now,
	}).Error; err != nil {
		return err
	}
	ordinal, err := nextAgentTimelineOrdinal(db, run.ID)
	if err != nil {
		return err
	}
	errorItem := model.AgentTimelineItem{
		ID:         agentFactID("timeline", run.ID, "runtime-schema-retired", strconv.FormatInt(sequence, 10)),
		TenantKind: thread.TenantKind, TenantID: thread.TenantID, ThreadID: thread.ID, RunID: run.ID,
		Kind: model.AgentTimelineItemError, Status: model.AgentTimelineItemFailed, Ordinal: ordinal,
		SourceEventSequence: sequence, ContentJSON: string(payload), StartedAt: now, CompletedAt: &now,
		CreatedAt: now, UpdatedAt: now,
	}
	return db.Create(&errorItem).Error
}
