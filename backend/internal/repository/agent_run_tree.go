package repository

import (
	"database/sql"
	"sort"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const agentRunTreeCancelledCode = "parent_run_cancelled"

var nonTerminalAgentSpecialistStatuses = []model.AgentSpecialistRunStatus{
	model.AgentSpecialistRunQueued,
	model.AgentSpecialistRunRunning,
	model.AgentSpecialistRunWaitingInput,
	model.AgentSpecialistRunWaitingApproval,
	model.AgentSpecialistRunWaitingTool,
}

type AgentRunTreeRecoverySnapshot struct {
	Run         model.AgentRun
	State       agentruntime.RuntimeState
	Production  *ProductionRuntimeSnapshot
	Specialists []model.AgentSpecialistRun
}

type agentSpecialistCancelUpdates struct {
	Status          model.AgentSpecialistRunStatus `gorm:"column:status"`
	Version         int64                          `gorm:"column:version"`
	ErrorCode       string                         `gorm:"column:error_code"`
	LastHeartbeatAt *time.Time                     `gorm:"column:last_heartbeat_at"`
	CompletedAt     *time.Time                     `gorm:"column:completed_at"`
	UpdatedAt       time.Time                      `gorm:"column:updated_at"`
}

type agentProductionStageStopUpdates struct {
	Status        agentruntime.ProductionStageStatus `gorm:"column:status"`
	Version       int64                              `gorm:"column:version"`
	LastErrorCode string                             `gorm:"column:last_error_code"`
	UpdatedAt     time.Time                          `gorm:"column:updated_at"`
}

type agentToolTreeCancelUpdates struct {
	Status     agentruntime.ToolCallStatus `gorm:"column:status"`
	OutputJSON string                      `gorm:"column:output_json"`
	ErrorCode  string                      `gorm:"column:error_code"`
	UpdatedAt  time.Time                   `gorm:"column:updated_at"`
}

func (r *Repository) cancelAgentRunTreeTx(tx *gorm.DB, scope agentruntime.Scope, now time.Time) error {
	if err := cancelAgentToolCallsTx(tx, scope, now); err != nil {
		return err
	}
	if err := cancelAgentSpecialistLifecyclesTx(tx, scope, now); err != nil {
		return err
	}
	targets, err := loadAgentRunTreeControlTargets(tx, scope)
	if err != nil {
		return err
	}
	_, _, err = disposeAdminAgentRunControlTargets(tx, targets, "父 Agent 运行已终止", now, true)
	return err
}

func cancelAgentToolCallsTx(tx *gorm.DB, scope agentruntime.Scope, now time.Time) error {
	return tx.Model(&model.AgentToolCall{}).
		Where("run_id = ? AND status IN ?", scope.RunID, []agentruntime.ToolCallStatus{
			agentruntime.ToolCallPending,
			agentruntime.ToolCallWaitingApproval,
			agentruntime.ToolCallRunning,
		}).
		Select("status", "output_json", "error_code", "updated_at").
		Updates(agentToolTreeCancelUpdates{
			Status: agentruntime.ToolCallFailed, OutputJSON: `{}`,
			ErrorCode: agentRunTreeCancelledCode, UpdatedAt: now,
		}).Error
}

func cancelAgentSpecialistLifecyclesTx(tx *gorm.DB, scope agentruntime.Scope, now time.Time) error {
	var runs []model.AgentSpecialistRun
	query := agentSpecialistScopeQuery(tx, scope).
		Where("status IN ?", nonTerminalAgentSpecialistStatuses).
		Order("id ASC")
	query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	if err := query.Find(&runs).Error; err != nil {
		return err
	}
	stageByID := make(map[string]struct{}, len(runs))
	for _, run := range runs {
		result := agentSpecialistScopeQuery(tx.Model(&model.AgentSpecialistRun{}), scope).
			Where("id = ? AND status = ? AND version = ?", run.ID, run.Status, run.Version).
			Select("status", "version", "error_code", "last_heartbeat_at", "completed_at", "updated_at").
			Updates(agentSpecialistCancelUpdates{
				Status: model.AgentSpecialistRunCancelled, Version: run.Version + 1, ErrorCode: agentRunTreeCancelledCode,
				LastHeartbeatAt: &now, CompletedAt: &now, UpdatedAt: now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrAgentSpecialistRunConflict
		}
		stageByID[run.StageID] = struct{}{}
	}
	stageIDs := make([]string, 0, len(stageByID))
	for stageID := range stageByID {
		stageIDs = append(stageIDs, stageID)
	}
	sort.Strings(stageIDs)
	for _, stageID := range stageIDs {
		var stage model.AgentProductionStage
		query := productionStageScopeQuery(tx.Model(&model.AgentProductionStage{}), scope).
			Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", stageID)
		if err := query.First(&stage).Error; err != nil {
			return err
		}
		if stage.Status == agentruntime.StageCompleted || stage.Status == agentruntime.StageFailed ||
			stage.Status == agentruntime.StageStopped || stage.Status == agentruntime.StageStale {
			continue
		}
		result := productionStageScopeQuery(tx.Model(&model.AgentProductionStage{}), scope).
			Where("id = ? AND status = ? AND version = ?", stage.ID, stage.Status, stage.Version).
			Select("status", "version", "last_error_code", "updated_at").
			Updates(agentProductionStageStopUpdates{
				Status: agentruntime.StageStopped, Version: stage.Version + 1,
				LastErrorCode: agentRunTreeCancelledCode, UpdatedAt: now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrProductionStageConflict
		}
	}
	return nil
}

func loadAgentRunTreeControlTargets(tx *gorm.DB, scope agentruntime.Scope) ([]adminAgentRunControlTarget, error) {
	targets, err := loadAdminAgentRunControlTargets(tx, scope.RunID)
	if err != nil {
		return nil, err
	}
	targetByID := make(map[string]adminAgentRunControlTarget, len(targets))
	for _, target := range targets {
		targetByID[target.TaskID] = target
	}
	assemblyOperation, err := agentruntime.MediaAssemblyOperationForRun(scope.RunID)
	if err != nil {
		return nil, err
	}
	var assemblyTargets []adminAgentRunControlTarget
	if err := tx.Model(&model.Task{}).
		Select("id AS task_id, 'assembly' AS kind, user_id, status, billing_order_id, provider_request_id").
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("operation = ? AND user_id = ? AND project_id = ? AND audience = ? AND execution_kind = ? AND status IN ?",
			assemblyOperation, scope.ActorUserID, scope.CanvasID, model.TaskAudienceInternal, model.TaskExecutionLocalMediaAssembly,
			[]model.TaskStatus{model.TaskStatusQueued, model.TaskStatusRunning}).
		Find(&assemblyTargets).Error; err != nil {
		return nil, err
	}
	for _, target := range assemblyTargets {
		targetByID[target.TaskID] = target
	}
	var specialistTaskIDs []string
	if err := agentSpecialistScopeQuery(tx, scope).
		Where("task_id <> ''").Pluck("task_id", &specialistTaskIDs).Error; err != nil {
		return nil, err
	}
	var billingTaskIDs []string
	if err := tx.Model(&model.BillingOrder{}).
		Where("idempotency_key LIKE ? AND task_id <> ''", "agent-specialist:"+scope.RunID+":%").
		Pluck("task_id", &billingTaskIDs).Error; err != nil {
		return nil, err
	}
	specialistTaskIDs = append(specialistTaskIDs, billingTaskIDs...)
	if len(specialistTaskIDs) > 0 {
		var specialistTargets []adminAgentRunControlTarget
		if err := tx.Model(&model.Task{}).
			Select("id AS task_id, 'specialist' AS kind, user_id, status, billing_order_id, provider_request_id").
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id IN ? AND user_id = ? AND status IN ?", specialistTaskIDs, scope.ActorUserID, []model.TaskStatus{model.TaskStatusQueued, model.TaskStatusRunning}).
			Find(&specialistTargets).Error; err != nil {
			return nil, err
		}
		for _, target := range specialistTargets {
			targetByID[target.TaskID] = target
		}
	}
	result := make([]adminAgentRunControlTarget, 0, len(targetByID))
	for _, target := range targetByID {
		result = append(result, target)
	}
	sort.Slice(result, func(left int, right int) bool { return result[left].TaskID < result[right].TaskID })
	return result, nil
}

func (r *Repository) AgentRunTreeTaskIDs(scope agentruntime.Scope) ([]string, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	taskIDs := make(map[string]struct{})
	var directTaskIDs []string
	if err := r.db.Model(&model.Task{}).
		Where("operation = ? AND user_id = ?", "agent_model:"+scope.RunID, scope.ActorUserID).
		Pluck("id", &directTaskIDs).Error; err != nil {
		return nil, err
	}
	assemblyOperation, err := agentruntime.MediaAssemblyOperationForRun(scope.RunID)
	if err != nil {
		return nil, err
	}
	var assemblyTaskIDs []string
	if err := r.db.Model(&model.Task{}).
		Where("operation = ? AND user_id = ? AND project_id = ? AND audience = ? AND execution_kind = ?",
			assemblyOperation, scope.ActorUserID, scope.CanvasID, model.TaskAudienceInternal, model.TaskExecutionLocalMediaAssembly).
		Pluck("id", &assemblyTaskIDs).Error; err != nil {
		return nil, err
	}
	var specialistTaskIDs []string
	if err := agentSpecialistScopeQuery(r.db, scope).Where("task_id <> ''").Pluck("task_id", &specialistTaskIDs).Error; err != nil {
		return nil, err
	}
	var billedTaskIDs []string
	if err := r.db.Model(&model.BillingOrder{}).
		Where("(idempotency_key LIKE ? OR idempotency_key LIKE ? OR idempotency_key LIKE ?) AND task_id <> ''",
			"agent-runtime:"+scope.RunID+":%", "proxy-token:agent-runtime:"+scope.RunID+":%", "agent-specialist:"+scope.RunID+":%").
		Pluck("task_id", &billedTaskIDs).Error; err != nil {
		return nil, err
	}
	allTaskIDs := append(directTaskIDs, assemblyTaskIDs...)
	allTaskIDs = append(allTaskIDs, specialistTaskIDs...)
	allTaskIDs = append(allTaskIDs, billedTaskIDs...)
	for _, taskID := range allTaskIDs {
		if strings.TrimSpace(taskID) != "" {
			taskIDs[taskID] = struct{}{}
		}
	}
	result := make([]string, 0, len(taskIDs))
	for taskID := range taskIDs {
		result = append(result, taskID)
	}
	sort.Strings(result)
	return result, nil
}

func (r *Repository) AgentRunTreeRecoverySnapshot(scope agentruntime.Scope) (*AgentRunTreeRecoverySnapshot, error) {
	if err := validateProductionRepositoryScope(scope, false); err != nil {
		return nil, err
	}
	var snapshot AgentRunTreeRecoverySnapshot
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("agent_runs").Select("agent_runs.*").
			Joins("JOIN agent_threads ON agent_threads.id = agent_runs.thread_id").
			Where(`agent_runs.id = ? AND agent_runs.thread_id = ? AND agent_runs.actor_user_id = ?
				AND agent_threads.tenant_kind = ? AND agent_threads.tenant_id = ?
				AND agent_threads.created_by_user_id = ? AND agent_threads.domain_project_id = ?
				AND agent_threads.canvas_id = ?`,
				scope.RunID, scope.ThreadID, scope.ActorUserID, scope.TenantKind, scope.TenantID,
				scope.ActorUserID, scope.DomainProjectID, scope.CanvasID).
			Take(&snapshot.Run).Error; err != nil {
			return err
		}
		state, err := loadAgentCheckpointForScope(tx, scope, false)
		if err != nil {
			return err
		}
		snapshot.State = state
		production, err := productionRuntimeSnapshotTx(tx, scope)
		if err != nil {
			return err
		}
		snapshot.Production = production
		if err := agentSpecialistScopeQuery(tx, scope).Order("id ASC").Find(&snapshot.Specialists).Error; err != nil {
			return err
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	if snapshot.Specialists == nil {
		snapshot.Specialists = []model.AgentSpecialistRun{}
	}
	return &snapshot, nil
}

func requireActiveProductionAgentRunTx(tx *gorm.DB, scope agentruntime.Scope) error {
	var run model.AgentRun
	query := tx.Table("agent_runs").Select("agent_runs.*").
		Joins("JOIN agent_threads ON agent_threads.id = agent_runs.thread_id").
		Where(`agent_runs.id = ? AND agent_runs.thread_id = ? AND agent_runs.actor_user_id = ?
			AND agent_threads.tenant_kind = ? AND agent_threads.tenant_id = ?
			AND agent_threads.created_by_user_id = ? AND agent_threads.domain_project_id = ?
			AND agent_threads.canvas_id = ?`,
			scope.RunID, scope.ThreadID, scope.ActorUserID, scope.TenantKind, scope.TenantID,
			scope.ActorUserID, scope.DomainProjectID, scope.CanvasID).
		Clauses(clause.Locking{Strength: "UPDATE"})
	if err := query.First(&run).Error; err != nil {
		return err
	}
	if (run.Status != agentruntime.RunRunning && run.Status != agentruntime.RunWaitingTool) ||
		run.RuntimeVersion != agentruntime.ProductionRuntimeVersion ||
		run.PolicyVersion != agentruntime.ProductionPolicyVersion || run.ToolSchemaVersion != agentruntime.ProductionToolSchemaVersion {
		return ErrAgentSpecialistRunConflict
	}
	return nil
}
