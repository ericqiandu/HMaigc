package database

import (
	"encoding/json"
	"fmt"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

type legacyAgentMediaCapabilityCall struct {
	ID              string
	RunID           string
	ThreadID        string
	ActorUserID     string
	TenantKind      agentruntime.TenantKind
	TenantID        string
	DomainProjectID string
	CanvasID        string
	ToolCallID      string
	ActionVersion   int
	InputJSON       string
}

func backfillAgentMediaCapabilityIdempotencyKeys(db *gorm.DB) error {
	var calls []legacyAgentMediaCapabilityCall
	if err := db.Table("agent_tool_calls").
		Select(`agent_tool_calls.id, agent_tool_calls.run_id, agent_runs.thread_id,
			agent_runs.actor_user_id, agent_threads.tenant_kind, agent_threads.tenant_id,
			agent_threads.domain_project_id, agent_threads.canvas_id,
			agent_tool_calls.tool_call_id, agent_tool_calls.action_version, agent_tool_calls.input_json`).
		Joins("JOIN agent_runs ON agent_runs.id = agent_tool_calls.run_id").
		Joins("JOIN agent_threads ON agent_threads.id = agent_runs.thread_id").
		Where(`agent_tool_calls.tool_name = ? AND agent_tool_calls.capability_idempotency_key = ''
			AND agent_tool_calls.replay_source_run_id = '' AND agent_tool_calls.replay_source_tool_call_id = ''
			AND agent_tool_calls.replay_source_action_version = 0 AND agent_tool_calls.status IN ?`,
			agentruntime.ToolMediaGenerate,
			[]agentruntime.ToolCallStatus{
				agentruntime.ToolCallWaitingApproval,
				agentruntime.ToolCallPending,
				agentruntime.ToolCallRunning,
				agentruntime.ToolCallSucceeded,
			}).
		Order("agent_tool_calls.created_at ASC, agent_tool_calls.id ASC").
		Find(&calls).Error; err != nil {
		return fmt.Errorf("query legacy agent media capability calls: %w", err)
	}
	for _, call := range calls {
		toolCall := agentruntime.ToolCallDecision{
			ToolCallID: call.ToolCallID, ToolName: agentruntime.ToolMediaGenerate,
			ActionVersion: call.ActionVersion, Arguments: json.RawMessage(call.InputJSON),
		}
		key, err := agentruntime.CapabilityIdempotencyKey(agentruntime.Scope{
			TenantKind: call.TenantKind, TenantID: call.TenantID, ActorUserID: call.ActorUserID,
			DomainProjectID: call.DomainProjectID, CanvasID: call.CanvasID,
			ThreadID: call.ThreadID, RunID: call.RunID,
			Access: agentruntime.AccessGrant{Level: agentruntime.AccessViewer},
		}, toolCall)
		if err != nil {
			return fmt.Errorf("backfill agent media capability call %s: %w", call.ID, err)
		}
		updated := db.Model(&model.AgentToolCall{}).
			Where("id = ? AND capability_idempotency_key = ''", call.ID).
			Update("capability_idempotency_key", key)
		if updated.Error != nil {
			return fmt.Errorf("persist agent media capability key %s: %w", call.ID, updated.Error)
		}
		if updated.RowsAffected != 1 {
			return fmt.Errorf("agent media capability call %s changed during backfill", call.ID)
		}
	}
	return nil
}
