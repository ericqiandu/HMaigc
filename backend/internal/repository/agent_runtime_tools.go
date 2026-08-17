package repository

import (
	"errors"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

type ActiveAgentRunReference struct {
	RunID       string `gorm:"column:run_id"`
	ActorUserID string `gorm:"column:actor_user_id"`
}

func (r *Repository) StaleAgentRunsAfter(afterRunID string, updatedBefore time.Time, limit int) ([]ActiveAgentRunReference, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("active agent run limit is invalid")
	}
	if updatedBefore.IsZero() {
		return nil, errors.New("stale agent run cutoff is invalid")
	}
	var references []ActiveAgentRunReference
	query := r.db.Table("agent_runs").Select("id AS run_id, actor_user_id").
		Where("status IN ?", []agentruntime.RunStatus{agentruntime.RunQueued, agentruntime.RunRunning, agentruntime.RunWaitingTool}).
		Where("updated_at < ?", updatedBefore.UTC()).
		Order("id ASC").Limit(limit)
	if afterRunID != "" {
		query = query.Where("id > ?", afterRunID)
	}
	err := query.Scan(&references).Error
	return references, err
}

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
