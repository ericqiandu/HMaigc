package repository

import (
	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

func (r *Repository) AgentToolCallForScope(scope agentruntime.Scope, toolCallID string, actionVersion int) (*model.AgentToolCall, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	var call model.AgentToolCall
	err := r.db.Table("agent_tool_calls").
		Select("agent_tool_calls.*").
		Joins("JOIN agent_runs ON agent_runs.id = agent_tool_calls.run_id").
		Joins("JOIN agent_threads ON agent_threads.id = agent_runs.thread_id").
		Where(`agent_tool_calls.run_id = ? AND agent_tool_calls.tool_call_id = ? AND agent_tool_calls.action_version = ?
			AND agent_runs.thread_id = ? AND agent_runs.actor_user_id = ?
			AND agent_threads.tenant_kind = ? AND agent_threads.tenant_id = ?
			AND agent_threads.created_by_user_id = ? AND agent_threads.domain_project_id = ?
			AND agent_threads.canvas_id = ?`,
			scope.RunID, toolCallID, actionVersion, scope.ThreadID, scope.ActorUserID,
			scope.TenantKind, scope.TenantID, scope.ActorUserID, scope.DomainProjectID, scope.CanvasID).
		First(&call).Error
	if err != nil {
		return nil, err
	}
	return &call, nil
}

func (r *Repository) AgentToolCallsForScope(scope agentruntime.Scope) ([]model.AgentToolCall, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	var calls []model.AgentToolCall
	err := r.db.Table("agent_tool_calls").
		Select("agent_tool_calls.*").
		Joins("JOIN agent_runs ON agent_runs.id = agent_tool_calls.run_id").
		Joins("JOIN agent_threads ON agent_threads.id = agent_runs.thread_id").
		Where(`agent_tool_calls.run_id = ? AND agent_runs.thread_id = ? AND agent_runs.actor_user_id = ?
			AND agent_threads.tenant_kind = ? AND agent_threads.tenant_id = ?
			AND agent_threads.created_by_user_id = ? AND agent_threads.domain_project_id = ?
			AND agent_threads.canvas_id = ?`,
			scope.RunID, scope.ThreadID, scope.ActorUserID, scope.TenantKind, scope.TenantID,
			scope.ActorUserID, scope.DomainProjectID, scope.CanvasID).
		Order("agent_tool_calls.created_at ASC, agent_tool_calls.id ASC").
		Find(&calls).Error
	return calls, err
}
