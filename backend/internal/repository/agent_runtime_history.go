package repository

import (
	"errors"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

const maxAgentThreadHistoryLimit = 20

type AgentThreadHistoryRecord struct {
	Thread     model.AgentThread
	Run        *model.AgentRun
	StateJSON  string
	ActivityAt time.Time
}

type agentThreadHistoryRow struct {
	ThreadID              string                    `gorm:"column:thread_id"`
	ThreadTenantKind      agentruntime.TenantKind   `gorm:"column:thread_tenant_kind"`
	ThreadTenantID        string                    `gorm:"column:thread_tenant_id"`
	ThreadCreatedByUserID string                    `gorm:"column:thread_created_by_user_id"`
	ThreadDomainProjectID string                    `gorm:"column:thread_domain_project_id"`
	ThreadCanvasID        string                    `gorm:"column:thread_canvas_id"`
	ThreadStatus          agentruntime.ThreadStatus `gorm:"column:thread_status"`
	ThreadCreatedAt       time.Time                 `gorm:"column:thread_created_at"`
	ThreadUpdatedAt       time.Time                 `gorm:"column:thread_updated_at"`
	RunID                 *string                   `gorm:"column:run_id"`
	RunThreadID           *string                   `gorm:"column:run_thread_id"`
	RunActorUserID        *string                   `gorm:"column:run_actor_user_id"`
	RunClientRequestID    *string                   `gorm:"column:run_client_request_id"`
	RunStatus             *string                   `gorm:"column:run_status"`
	RunLastEventSequence  *int64                    `gorm:"column:run_last_event_sequence"`
	RunStateVersion       *int                      `gorm:"column:run_state_version"`
	RunStepNumber         *int                      `gorm:"column:run_step_number"`
	RunMaxSteps           *int                      `gorm:"column:run_max_steps"`
	RunModelRecordID      *string                   `gorm:"column:run_model_record_id"`
	RunModelKey           *string                   `gorm:"column:run_model_key"`
	RunToolSchemaVersion  *int                      `gorm:"column:run_tool_schema_version"`
	RunRuntimeVersion     *int                      `gorm:"column:run_runtime_version"`
	RunPolicyVersion      *int                      `gorm:"column:run_policy_version"`
	RunCreatedAt          *time.Time                `gorm:"column:run_created_at"`
	RunUpdatedAt          *time.Time                `gorm:"column:run_updated_at"`
	RunCompletedAt        *time.Time                `gorm:"column:run_completed_at"`
	LatestStateJSON       *string                   `gorm:"column:latest_state_json"`
}

func (r *Repository) AgentThreadHistory(scope agentruntime.Scope, limit int) ([]AgentThreadHistoryRecord, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > maxAgentThreadHistoryLimit {
		return nil, errors.New("agent thread history limit is invalid")
	}
	var rows []agentThreadHistoryRow
	err := r.db.Raw(`
		WITH scoped_threads AS (
			SELECT *
			  FROM agent_threads
			 WHERE tenant_kind = ? AND tenant_id = ? AND created_by_user_id = ?
			   AND domain_project_id = ? AND canvas_id = ? AND status = ?
		), ranked_runs AS (
			SELECT agent_runs.*,
			       ROW_NUMBER() OVER (
					PARTITION BY agent_runs.thread_id
					ORDER BY agent_runs.updated_at DESC, agent_runs.created_at DESC, agent_runs.id DESC
			       ) AS run_rank
			  FROM agent_runs
			  JOIN scoped_threads ON scoped_threads.id = agent_runs.thread_id
		)
		SELECT scoped_threads.id AS thread_id,
		       scoped_threads.tenant_kind AS thread_tenant_kind,
		       scoped_threads.tenant_id AS thread_tenant_id,
		       scoped_threads.created_by_user_id AS thread_created_by_user_id,
		       scoped_threads.domain_project_id AS thread_domain_project_id,
		       scoped_threads.canvas_id AS thread_canvas_id,
		       scoped_threads.status AS thread_status,
		       scoped_threads.created_at AS thread_created_at,
		       scoped_threads.updated_at AS thread_updated_at,
		       ranked_runs.id AS run_id,
		       ranked_runs.thread_id AS run_thread_id,
		       ranked_runs.actor_user_id AS run_actor_user_id,
		       ranked_runs.client_request_id AS run_client_request_id,
		       ranked_runs.status AS run_status,
		       ranked_runs.last_event_sequence AS run_last_event_sequence,
		       ranked_runs.state_version AS run_state_version,
		       ranked_runs.step_number AS run_step_number,
		       ranked_runs.max_steps AS run_max_steps,
		       ranked_runs.model_record_id AS run_model_record_id,
		       ranked_runs.model_key AS run_model_key,
		       ranked_runs.tool_schema_version AS run_tool_schema_version,
		       ranked_runs.runtime_version AS run_runtime_version,
		       ranked_runs.policy_version AS run_policy_version,
		       ranked_runs.created_at AS run_created_at,
		       ranked_runs.updated_at AS run_updated_at,
		       ranked_runs.completed_at AS run_completed_at,
		       agent_checkpoints.state_json AS latest_state_json
		  FROM scoped_threads
		  LEFT JOIN ranked_runs ON ranked_runs.thread_id = scoped_threads.id AND ranked_runs.run_rank = 1
		  LEFT JOIN agent_checkpoints ON agent_checkpoints.run_id = ranked_runs.id
		   AND agent_checkpoints.state_version = ranked_runs.state_version
		   AND agent_checkpoints.sequence = (
		       SELECT MAX(latest_checkpoint.sequence)
		         FROM agent_checkpoints AS latest_checkpoint
		        WHERE latest_checkpoint.run_id = ranked_runs.id
		          AND latest_checkpoint.state_version = ranked_runs.state_version
		   )
		 ORDER BY COALESCE(ranked_runs.updated_at, scoped_threads.updated_at) DESC, scoped_threads.id DESC
		 LIMIT ?`, scope.TenantKind, scope.TenantID, scope.ActorUserID, scope.DomainProjectID, scope.CanvasID, agentruntime.ThreadActive, limit).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	records := make([]AgentThreadHistoryRecord, 0, len(rows))
	for _, row := range rows {
		record, err := agentThreadHistoryRecord(row)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func agentThreadHistoryRecord(row agentThreadHistoryRow) (AgentThreadHistoryRecord, error) {
	record := AgentThreadHistoryRecord{
		Thread: model.AgentThread{
			ID: row.ThreadID, TenantKind: row.ThreadTenantKind, TenantID: row.ThreadTenantID,
			CreatedByUserID: row.ThreadCreatedByUserID, DomainProjectID: row.ThreadDomainProjectID,
			CanvasID: row.ThreadCanvasID, Status: row.ThreadStatus,
			CreatedAt: row.ThreadCreatedAt, UpdatedAt: row.ThreadUpdatedAt,
		},
		ActivityAt: row.ThreadUpdatedAt,
	}
	if row.RunID == nil {
		return record, nil
	}
	if row.RunThreadID == nil || row.RunActorUserID == nil || row.RunClientRequestID == nil || row.RunStatus == nil ||
		row.RunLastEventSequence == nil || row.RunStateVersion == nil || row.RunStepNumber == nil || row.RunMaxSteps == nil ||
		row.RunModelRecordID == nil || row.RunModelKey == nil || row.RunToolSchemaVersion == nil ||
		row.RunRuntimeVersion == nil || row.RunPolicyVersion == nil || row.RunCreatedAt == nil ||
		row.RunUpdatedAt == nil || row.LatestStateJSON == nil {
		return AgentThreadHistoryRecord{}, errors.New("agent thread history facts are incomplete")
	}
	record.Run = &model.AgentRun{
		ID: *row.RunID, ThreadID: *row.RunThreadID, ActorUserID: *row.RunActorUserID,
		ClientRequestID: *row.RunClientRequestID, Status: agentruntime.RunStatus(*row.RunStatus),
		LastEventSequence: *row.RunLastEventSequence, StateVersion: *row.RunStateVersion,
		StepNumber: *row.RunStepNumber, MaxSteps: *row.RunMaxSteps,
		ModelRecordID: *row.RunModelRecordID, ModelKey: *row.RunModelKey,
		ToolSchemaVersion: *row.RunToolSchemaVersion, RuntimeVersion: *row.RunRuntimeVersion,
		PolicyVersion: *row.RunPolicyVersion, CreatedAt: *row.RunCreatedAt,
		UpdatedAt: *row.RunUpdatedAt, CompletedAt: row.RunCompletedAt,
	}
	record.StateJSON = *row.LatestStateJSON
	record.ActivityAt = *row.RunUpdatedAt
	return record, nil
}
