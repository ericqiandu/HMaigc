package database

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
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
		plans, audit, err := buildPausedRunRetirementBatch(tx, true)
		if err != nil {
			return err
		}
		if len(audit.Blockers) != 0 {
			encoded, err := json.Marshal(audit)
			if err != nil {
				return err
			}
			return fmt.Errorf("%s: %s", agentRuntimeRetirementInvalidCode, encoded)
		}
		for _, plan := range plans {
			if err := applyPausedRunRetirementPlan(tx, plan, now); err != nil {
				return err
			}
		}
		return nil
	})
}

func applyPausedRunRetirementPlan(db *gorm.DB, plan pausedRunRetirementPlan, now time.Time) error {
	run := plan.Run
	sequence := run.LastEventSequence + 1
	updated := db.Model(&model.AgentRun{}).
		Where(
			"id = ? AND status = ? AND state_version = ? AND step_number = ? AND last_event_sequence = ? AND tool_schema_version = ? AND runtime_version = ? AND policy_version = ?",
			run.ID, run.Status, run.StateVersion, run.StepNumber, run.LastEventSequence,
			run.ToolSchemaVersion, run.RuntimeVersion, run.PolicyVersion,
		).
		Select("status", "state_version", "step_number", "last_event_sequence", "updated_at", "completed_at").
		Updates(retiredAgentRunUpdates{
			Status: plan.Terminal.Status, StateVersion: plan.Terminal.StateVersion,
			StepNumber: plan.Terminal.StepNumber, LastEventSequence: sequence,
			UpdatedAt: now, CompletedAt: now,
		})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return fmt.Errorf("%s: retirement state conflict: run_id=%s", agentRuntimeRetirementInvalidCode, run.ID)
	}

	event := model.AgentRunEvent{
		ID:    agentRuntimeMigrationFactID("event", run.ID, strconv.FormatInt(sequence, 10)),
		RunID: run.ID, Sequence: sequence, Kind: agentruntime.EventRunInterrupted,
		PayloadJSON: plan.EventPayload, CreatedAt: now,
	}
	if err := db.Create(&event).Error; err != nil {
		return err
	}
	terminalCheckpoint := model.AgentCheckpoint{
		ID:    agentRuntimeMigrationFactID("checkpoint", run.ID, strconv.FormatInt(sequence, 10)),
		RunID: run.ID, Sequence: sequence, StateVersion: plan.Terminal.StateVersion,
		StateJSON: string(plan.StateJSON), CreatedAt: now,
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
			OutputJSON: `{"reason":"runtime_schema_retired"}`, UpdatedAt: now,
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
