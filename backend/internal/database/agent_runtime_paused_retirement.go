package database

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const agentRuntimeRetirementInvalidCode = "agent_runtime_retirement_invalid"

type agentRuntimeMigrationInterruptAudit struct {
	Source            string                 `json:"source"`
	Reason            string                 `json:"reason"`
	OriginalStatus    agentruntime.RunStatus `json:"originalStatus"`
	ToolSchemaVersion int                    `json:"toolSchemaVersion"`
	RuntimeVersion    int                    `json:"runtimeVersion"`
	PolicyVersion     int                    `json:"policyVersion"`
	TargetToolSchema  int                    `json:"targetToolSchemaVersion"`
	TargetRuntime     int                    `json:"targetRuntimeVersion"`
	TargetPolicy      int                    `json:"targetPolicyVersion"`
}

type pausedTimelineRetirementUpdates struct {
	Status      model.AgentTimelineItemStatus `gorm:"column:status"`
	CompletedAt time.Time                     `gorm:"column:completed_at"`
	UpdatedAt   time.Time                     `gorm:"column:updated_at"`
}

type pausedToolCallRetirementUpdates struct {
	Status     agentruntime.ToolCallStatus `gorm:"column:status"`
	ErrorCode  string                      `gorm:"column:error_code"`
	OutputJSON string                      `gorm:"column:output_json"`
	UpdatedAt  time.Time                   `gorm:"column:updated_at"`
}

type pausedArtifactRetirementUpdates struct {
	Status        model.AgentProductionArtifactStatus `gorm:"column:status"`
	LastErrorCode string                              `gorm:"column:last_error_code"`
	UpdatedAt     time.Time                           `gorm:"column:updated_at"`
}

func retireIncompatiblePausedAgentRuns(db *gorm.DB, now time.Time) error {
	if now.IsZero() {
		return fmt.Errorf("%s: retirement timestamp is required", agentRuntimeRetirementInvalidCode)
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := rejectFuturePausedAgentRuntimeContracts(tx); err != nil {
			return err
		}
		var runs []model.AgentRun
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(
				`status IN ? AND tool_schema_version <= ? AND runtime_version <= ? AND policy_version <= ?
				 AND (tool_schema_version <> ? OR runtime_version <> ? OR policy_version <> ?)`,
				[]agentruntime.RunStatus{agentruntime.RunWaitingInput, agentruntime.RunWaitingApproval},
				agentruntime.CurrentToolSchemaVersion,
				agentruntime.CurrentRuntimeVersion,
				agentruntime.CurrentPolicyVersion,
				agentruntime.CurrentToolSchemaVersion,
				agentruntime.CurrentRuntimeVersion,
				agentruntime.CurrentPolicyVersion,
			).
			Order("created_at, id").
			Find(&runs)
		if result.Error != nil {
			return result.Error
		}
		for _, run := range runs {
			if err := retireIncompatiblePausedAgentRun(tx, run, now); err != nil {
				return err
			}
		}
		return nil
	})
}

func retireIncompatiblePausedAgentRun(db *gorm.DB, run model.AgentRun, now time.Time) error {
	var checkpoint model.AgentCheckpoint
	checkpointResult := db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("run_id = ?", run.ID).
		Order("sequence DESC").
		Limit(1).
		Find(&checkpoint)
	if checkpointResult.Error != nil {
		return fmt.Errorf("%s: load checkpoint: run_id=%s: %w", agentRuntimeRetirementInvalidCode, run.ID, checkpointResult.Error)
	}
	if checkpointResult.RowsAffected != 1 {
		return fmt.Errorf("%s: checkpoint missing: run_id=%s", agentRuntimeRetirementInvalidCode, run.ID)
	}
	if len(checkpoint.StateJSON) > agentRuntimeMigrationCheckpointPayloadLimit {
		return fmt.Errorf("%s: checkpoint too large: run_id=%s", agentRuntimeRetirementInvalidCode, run.ID)
	}
	decoder := json.NewDecoder(bytes.NewBufferString(checkpoint.StateJSON))
	decoder.DisallowUnknownFields()
	var current agentruntime.RuntimeState
	if err := decoder.Decode(&current); err != nil {
		return fmt.Errorf("%s: decode checkpoint: run_id=%s: %w", agentRuntimeRetirementInvalidCode, run.ID, err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s: checkpoint has trailing data: run_id=%s", agentRuntimeRetirementInvalidCode, run.ID)
	}
	if checkpoint.Sequence != run.LastEventSequence || checkpoint.StateVersion != run.StateVersion ||
		current.Status != run.Status || current.StateVersion != run.StateVersion || current.StepNumber != run.StepNumber || current.MaxSteps != run.MaxSteps {
		return fmt.Errorf("%s: run and checkpoint disagree: run_id=%s", agentRuntimeRetirementInvalidCode, run.ID)
	}
	if current.PendingToolStarted {
		return fmt.Errorf("%s: pending tool already started: run_id=%s", agentRuntimeRetirementInvalidCode, run.ID)
	}
	if err := validatePausedRunRetirementFacts(db, run); err != nil {
		return err
	}

	transition, err := agentruntime.Interrupt(current, run.StateVersion)
	if err != nil {
		return fmt.Errorf("%s: interrupt paused run: run_id=%s: %w", agentRuntimeRetirementInvalidCode, run.ID, err)
	}
	stateJSON, err := json.Marshal(transition.State)
	if err != nil {
		return err
	}
	if len(stateJSON) > agentRuntimeMigrationCheckpointPayloadLimit {
		return fmt.Errorf("%s: terminal checkpoint too large: run_id=%s", agentRuntimeRetirementInvalidCode, run.ID)
	}
	sequence := run.LastEventSequence + 1
	updated := db.Model(&model.AgentRun{}).
		Where(
			"id = ? AND status = ? AND state_version = ? AND step_number = ? AND last_event_sequence = ? AND tool_schema_version = ? AND runtime_version = ? AND policy_version = ?",
			run.ID, run.Status, run.StateVersion, run.StepNumber, run.LastEventSequence,
			run.ToolSchemaVersion, run.RuntimeVersion, run.PolicyVersion,
		).
		Select("status", "state_version", "step_number", "last_event_sequence", "updated_at", "completed_at").
		Updates(retiredAgentRunUpdates{
			Status: transition.State.Status, StateVersion: transition.State.StateVersion,
			StepNumber: transition.State.StepNumber, LastEventSequence: sequence,
			UpdatedAt: now, CompletedAt: now,
		})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return fmt.Errorf("%s: retirement state conflict: run_id=%s", agentRuntimeRetirementInvalidCode, run.ID)
	}

	eventPayload, err := agentRuntimeMigrationInterruptPayload(stateJSON, run)
	if err != nil {
		return err
	}
	event := model.AgentRunEvent{
		ID:    agentRuntimeMigrationFactID("event", run.ID, strconv.FormatInt(sequence, 10)),
		RunID: run.ID, Sequence: sequence, Kind: agentruntime.EventRunInterrupted,
		PayloadJSON: eventPayload, CreatedAt: now,
	}
	if err := db.Create(&event).Error; err != nil {
		return err
	}
	terminalCheckpoint := model.AgentCheckpoint{
		ID:    agentRuntimeMigrationFactID("checkpoint", run.ID, strconv.FormatInt(sequence, 10)),
		RunID: run.ID, Sequence: sequence, StateVersion: transition.State.StateVersion,
		StateJSON: string(stateJSON), CreatedAt: now,
	}
	if err := db.Create(&terminalCheckpoint).Error; err != nil {
		return err
	}
	if err := db.Model(&model.AgentTimelineItem{}).
		Where("run_id = ? AND status = ?", run.ID, model.AgentTimelineItemInProgress).
		Select("status", "completed_at", "updated_at").
		Updates(pausedTimelineRetirementUpdates{Status: model.AgentTimelineItemInterrupted, CompletedAt: now, UpdatedAt: now}).Error; err != nil {
		return err
	}
	if err := db.Model(&model.AgentToolCall{}).
		Where("run_id = ? AND started_at IS NULL AND status IN ?", run.ID, []agentruntime.ToolCallStatus{agentruntime.ToolCallPending, agentruntime.ToolCallWaitingApproval}).
		Select("status", "error_code", "output_json", "updated_at").
		Updates(pausedToolCallRetirementUpdates{
			Status: agentruntime.ToolCallFailed, ErrorCode: retiredAgentRuntimeContractFailureCode,
			OutputJSON: `{"reason":"runtime_contract_retired"}`, UpdatedAt: now,
		}).Error; err != nil {
		return err
	}
	return db.Model(&model.AgentProductionArtifact{}).
		Where(
			"plan_version_id IN (?) AND status IN ?",
			db.Model(&model.AgentProductionPlanVersion{}).Select("id").Where("created_by_run_id = ?", run.ID),
			[]model.AgentProductionArtifactStatus{model.AgentProductionArtifactPlanned, model.AgentProductionArtifactAwaitingApproval},
		).
		Select("status", "last_error_code", "updated_at").
		Updates(pausedArtifactRetirementUpdates{
			Status:        model.AgentProductionArtifactFailed,
			LastErrorCode: retiredAgentRuntimeContractFailureCode,
			UpdatedAt:     now,
		}).Error
}

func rejectFuturePausedAgentRuntimeContracts(db *gorm.DB) error {
	var runs []model.AgentRun
	result := db.Where(
		`status IN ? AND (tool_schema_version > ? OR runtime_version > ? OR policy_version > ?)`,
		[]agentruntime.RunStatus{agentruntime.RunWaitingInput, agentruntime.RunWaitingApproval},
		agentruntime.CurrentToolSchemaVersion,
		agentruntime.CurrentRuntimeVersion,
		agentruntime.CurrentPolicyVersion,
	).Order("created_at, id").Limit(1).Find(&runs)
	if result.Error != nil {
		return result.Error
	}
	if len(runs) == 0 {
		return nil
	}
	run := runs[0]
	return fmt.Errorf(
		"%s: future runtime contract: run_id=%s status=%s tool_schema_version=%d runtime_version=%d policy_version=%d",
		agentRuntimeRetirementInvalidCode, run.ID, run.Status, run.ToolSchemaVersion, run.RuntimeVersion, run.PolicyVersion,
	)
}

func validatePausedRunRetirementFacts(db *gorm.DB, run model.AgentRun) error {
	var startedToolCalls int64
	if err := db.Model(&model.AgentToolCall{}).
		Where("run_id = ? AND (started_at IS NOT NULL OR status = ?)", run.ID, agentruntime.ToolCallRunning).
		Count(&startedToolCalls).Error; err != nil {
		return err
	}
	if startedToolCalls != 0 {
		return fmt.Errorf("%s: started tool call: run_id=%s count=%d", agentRuntimeRetirementInvalidCode, run.ID, startedToolCalls)
	}

	linkedArtifactTaskIDs := db.Model(&model.AgentProductionArtifact{}).
		Select("task_id").
		Where(
			"plan_version_id IN (?) AND task_id <> ''",
			db.Model(&model.AgentProductionPlanVersion{}).Select("id").Where("created_by_run_id = ?", run.ID),
		)
	var activeTasks int64
	if err := db.Model(&model.Task{}).
		Where(
			"(operation = ? OR id IN (?)) AND status IN ?",
			legacyAgentModelTaskOperationPrefix+run.ID,
			linkedArtifactTaskIDs,
			[]model.TaskStatus{model.TaskStatusQueued, model.TaskStatusRunning},
		).
		Count(&activeTasks).Error; err != nil {
		return err
	}
	if activeTasks != 0 {
		return fmt.Errorf("%s: active provider task: run_id=%s count=%d", agentRuntimeRetirementInvalidCode, run.ID, activeTasks)
	}

	linkedArtifactBillingIDs := db.Model(&model.AgentProductionArtifact{}).
		Select("billing_order_id").
		Where(
			"plan_version_id IN (?) AND billing_order_id <> ''",
			db.Model(&model.AgentProductionPlanVersion{}).Select("id").Where("created_by_run_id = ?", run.ID),
		)
	var unresolvedBilling int64
	if err := db.Model(&model.BillingOrder{}).
		Where(
			`user_id = ? AND (idempotency_key LIKE ? OR idempotency_key LIKE ? OR id IN (?)) AND status IN ?`,
			run.ActorUserID,
			"agent-runtime:"+run.ID+":%",
			"proxy-token:agent-runtime:"+run.ID+":%",
			linkedArtifactBillingIDs,
			[]model.BillingStatus{model.BillingStatusReserved, model.BillingStatusRunning, model.BillingStatusUncertain},
		).
		Count(&unresolvedBilling).Error; err != nil {
		return err
	}
	if unresolvedBilling != 0 {
		return fmt.Errorf("%s: unresolved billing: run_id=%s count=%d", agentRuntimeRetirementInvalidCode, run.ID, unresolvedBilling)
	}

	var unsafeArtifacts int64
	if err := db.Model(&model.AgentProductionArtifact{}).
		Where(
			"plan_version_id IN (?) AND status NOT IN ?",
			db.Model(&model.AgentProductionPlanVersion{}).Select("id").Where("created_by_run_id = ?", run.ID),
			[]model.AgentProductionArtifactStatus{model.AgentProductionArtifactPlanned, model.AgentProductionArtifactAwaitingApproval},
		).
		Count(&unsafeArtifacts).Error; err != nil {
		return err
	}
	if unsafeArtifacts != 0 {
		return fmt.Errorf("%s: unsafe production artifact: run_id=%s count=%d", agentRuntimeRetirementInvalidCode, run.ID, unsafeArtifacts)
	}
	return nil
}

func agentRuntimeMigrationInterruptPayload(stateJSON []byte, run model.AgentRun) (string, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(stateJSON, &payload); err != nil {
		return "", err
	}
	auditJSON, err := json.Marshal(agentRuntimeMigrationInterruptAudit{
		Source: "upgrade_migration", Reason: retiredAgentRuntimeContractFailureCode, OriginalStatus: run.Status,
		ToolSchemaVersion: run.ToolSchemaVersion, RuntimeVersion: run.RuntimeVersion, PolicyVersion: run.PolicyVersion,
		TargetToolSchema: agentruntime.CurrentToolSchemaVersion, TargetRuntime: agentruntime.CurrentRuntimeVersion, TargetPolicy: agentruntime.CurrentPolicyVersion,
	})
	if err != nil {
		return "", err
	}
	payload["interruptAudit"] = auditJSON
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	if len(encoded) > agentRuntimeMigrationEventPayloadLimit {
		return "", fmt.Errorf("%s: terminal event too large: run_id=%s", agentRuntimeRetirementInvalidCode, run.ID)
	}
	return string(encoded), nil
}
