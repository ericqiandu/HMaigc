package repository

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrAdminAgentRunNotFound          = errors.New("admin agent run not found")
	ErrAdminAgentRunStateConflict     = errors.New("admin agent run state conflict")
	ErrAdminAgentRunTerminal          = errors.New("admin agent run is terminal")
	ErrAdminAgentRunBillingUnresolved = errors.New("admin agent run billing is unresolved")
)

const (
	adminAgentRunInterruptedCode = "admin_agent_run_interrupted"
	agentRunInterruptedStage     = "Agent 任务已终止"
)

type AdminAgentRunInterruptCommand struct {
	RunID                string
	ExpectedStateVersion int
	ActorUserID          string
	Reason               string
	Now                  time.Time
}

type AdminAgentRunInterruptResult struct {
	Scope                 agentruntime.Scope
	State                 agentruntime.RuntimeState
	OriginalStatus        agentruntime.RunStatus
	TaskDispositions      []AdminAgentRunTaskDisposition
	ReconciliationPending bool
}

type AdminAgentRunTaskDisposition struct {
	TaskID                   string              `json:"taskId"`
	Kind                     string              `json:"kind"`
	PreviousStatus           model.TaskStatus    `json:"previousStatus"`
	Status                   model.TaskStatus    `json:"status"`
	BillingStatus            model.BillingStatus `json:"billingStatus,omitempty"`
	ProviderRequestSubmitted bool                `json:"providerRequestSubmitted"`
	ReconciliationPending    bool                `json:"reconciliationPending"`
}

type adminAgentRunControlTarget struct {
	TaskID            string           `gorm:"column:task_id"`
	Kind              string           `gorm:"column:kind"`
	UserID            string           `gorm:"column:user_id"`
	Status            model.TaskStatus `gorm:"column:status"`
	BillingOrderID    string           `gorm:"column:billing_order_id"`
	ProviderRequestID string           `gorm:"column:provider_request_id"`
}

type adminAgentRunScopeFacts struct {
	RunID           string                  `gorm:"column:run_id"`
	ThreadID        string                  `gorm:"column:thread_id"`
	ActorUserID     string                  `gorm:"column:actor_user_id"`
	Status          agentruntime.RunStatus  `gorm:"column:status"`
	StateVersion    int                     `gorm:"column:state_version"`
	RuntimeVersion  int                     `gorm:"column:runtime_version"`
	TenantKind      agentruntime.TenantKind `gorm:"column:tenant_kind"`
	TenantID        string                  `gorm:"column:tenant_id"`
	CreatedByUserID string                  `gorm:"column:created_by_user_id"`
	DomainProjectID string                  `gorm:"column:domain_project_id"`
	CanvasID        string                  `gorm:"column:canvas_id"`
}

func (r *Repository) InterruptAdminAgentRun(command AdminAgentRunInterruptCommand) (*AdminAgentRunInterruptResult, error) {
	command.RunID = strings.TrimSpace(command.RunID)
	command.ActorUserID = strings.TrimSpace(command.ActorUserID)
	command.Reason = strings.TrimSpace(command.Reason)
	if command.RunID == "" || command.ActorUserID == "" || command.Reason == "" || command.ExpectedStateVersion < 1 || command.Now.IsZero() {
		return nil, ErrAdminAgentRunStateConflict
	}
	if r.Dialect() == "sqlite" {
		r.adminAgentRunInterruptMu.Lock()
		defer r.adminAgentRunInterruptMu.Unlock()
	}
	var interrupted AdminAgentRunInterruptResult
	err := r.db.Transaction(func(tx *gorm.DB) error {
		facts, err := loadAdminAgentRunScopeFacts(tx, command.RunID)
		if err != nil {
			return err
		}
		if !adminAgentRunNonTerminalStatus(facts.Status) {
			return ErrAdminAgentRunTerminal
		}
		if facts.StateVersion != command.ExpectedStateVersion {
			return ErrAdminAgentRunStateConflict
		}
		scope := agentruntime.Scope{
			TenantKind: facts.TenantKind, TenantID: facts.TenantID,
			ThreadID: facts.ThreadID, RunID: facts.RunID, ActorUserID: facts.ActorUserID,
			Access:          agentruntime.AccessGrant{Level: agentruntime.AccessManager, SubscriptionActive: true},
			DomainProjectID: facts.DomainProjectID, CanvasID: facts.CanvasID,
		}
		current, err := loadAgentCheckpointForScope(tx, scope, true)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAdminAgentRunNotFound
			}
			return err
		}
		if current.StateVersion != command.ExpectedStateVersion {
			return ErrAdminAgentRunStateConflict
		}
		loadTargets := func() ([]adminAgentRunControlTarget, error) {
			if facts.RuntimeVersion == agentruntime.ProductionRuntimeVersion {
				return loadAgentRunTreeControlTargets(tx, scope)
			}
			return loadAdminAgentRunControlTargets(tx, scope.RunID)
		}
		targets, err := loadTargets()
		if err != nil {
			return err
		}
		unresolvedBilling, err := adminAgentRunHasUnresolvedBilling(tx, scope.RunID, targets)
		if err != nil {
			return err
		}
		if unresolvedBilling {
			return ErrAdminAgentRunBillingUnresolved
		}
		transition, err := agentruntime.Interrupt(current, command.ExpectedStateVersion)
		if err != nil {
			if errors.Is(err, agentruntime.ErrInterruptConflict) {
				return ErrAdminAgentRunStateConflict
			}
			return err
		}
		if err := validateAgentRuntimeTransition(scope, current, transition, command.Now); err != nil {
			return err
		}
		stateJSON, err := json.Marshal(transition.State)
		if err != nil {
			return err
		}
		if len(stateJSON) > agentEventPayloadLimit {
			return ErrAgentPayloadTooLarge
		}
		audit := &agentRuntimeInterruptAudit{
			Source: "admin", ActorUserID: command.ActorUserID, Reason: command.Reason, OriginalStatus: current.Status,
		}
		if err := r.commitAgentRuntimeTransitionTx(tx, scope, current, transition, string(stateJSON), audit, command.Now); err != nil {
			if errors.Is(err, ErrAgentRuntimeStepConflict) {
				return ErrAdminAgentRunStateConflict
			}
			return err
		}
		if facts.RuntimeVersion == agentruntime.ProductionRuntimeVersion {
			if err := cancelAgentSpecialistLifecyclesTx(tx, scope, command.Now); err != nil {
				return err
			}
		}
		if err := failAdminAgentRunPendingFacts(tx, scope, current, command.Now); err != nil {
			return err
		}
		taskDispositions, reconciliationPending, err := disposeAdminAgentRunControlTargets(tx, targets, command.Reason, command.Now, false)
		if err != nil {
			return err
		}
		interrupted = AdminAgentRunInterruptResult{
			Scope: scope, State: transition.State, OriginalStatus: current.Status,
			TaskDispositions: taskDispositions, ReconciliationPending: reconciliationPending,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &interrupted, nil
}

func loadAdminAgentRunControlTargets(db *gorm.DB, runID string) ([]adminAgentRunControlTarget, error) {
	targetByID := make(map[string]adminAgentRunControlTarget)
	var modelTargets []adminAgentRunControlTarget
	if err := db.Model(&model.Task{}).
		Select("id AS task_id, 'model' AS kind, user_id, status, billing_order_id, provider_request_id").
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("operation = ? AND status IN ?", "agent_model:"+runID, []model.TaskStatus{model.TaskStatusQueued, model.TaskStatusRunning}).
		Find(&modelTargets).Error; err != nil {
		return nil, err
	}
	for _, target := range modelTargets {
		targetByID[target.TaskID] = target
	}
	var mediaTargets []adminAgentRunControlTarget
	if err := db.Table("tasks AS tasks").
		Select("tasks.id AS task_id, 'media' AS kind, tasks.user_id AS user_id, tasks.status AS status, tasks.billing_order_id AS billing_order_id, tasks.provider_request_id AS provider_request_id").
		Joins("JOIN agent_production_artifacts AS artifacts ON artifacts.task_id = tasks.id").
		Joins("JOIN agent_production_plan_versions AS plans ON plans.id = artifacts.plan_version_id").
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("plans.created_by_run_id = ? AND tasks.status IN ?", runID, []model.TaskStatus{model.TaskStatusQueued, model.TaskStatusRunning}).
		Find(&mediaTargets).Error; err != nil {
		return nil, err
	}
	for _, target := range mediaTargets {
		targetByID[target.TaskID] = target
	}
	assemblyOperation, err := agentruntime.MediaAssemblyOperationForRun(runID)
	if err != nil {
		return nil, err
	}
	var assemblyTargets []adminAgentRunControlTarget
	if err := db.Model(&model.Task{}).
		Select("id AS task_id, 'assembly' AS kind, user_id, status, billing_order_id, provider_request_id").
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("operation = ? AND type = ? AND audience = ? AND execution_kind = ? AND status IN ?",
			assemblyOperation, agentruntime.MediaAssemblyTaskType, model.TaskAudienceInternal,
			model.TaskExecutionLocalMediaAssembly, []model.TaskStatus{model.TaskStatusQueued, model.TaskStatusRunning}).
		Find(&assemblyTargets).Error; err != nil {
		return nil, err
	}
	for _, target := range assemblyTargets {
		targetByID[target.TaskID] = target
	}
	targets := make([]adminAgentRunControlTarget, 0, len(targetByID))
	for _, target := range targetByID {
		targets = append(targets, target)
	}
	sort.Slice(targets, func(left int, right int) bool {
		return targets[left].TaskID < targets[right].TaskID
	})
	return targets, nil
}

func adminAgentRunHasUnresolvedBilling(db *gorm.DB, runID string, targets []adminAgentRunControlTarget) (bool, error) {
	taskIDs := make([]string, 0, len(targets))
	billingOrderIDs := make([]string, 0, len(targets))
	for _, target := range targets {
		taskIDs = append(taskIDs, target.TaskID)
		if strings.TrimSpace(target.BillingOrderID) == "" {
			if target.Kind == "assembly" {
				continue
			}
			return true, nil
		}
		if target.BillingOrderID != "" {
			billingOrderIDs = append(billingOrderIDs, target.BillingOrderID)
		}
	}
	var rows []struct {
		ID string `gorm:"column:id"`
	}
	query := db.Model(&model.BillingOrder{}).
		Select("billing_orders.id AS id").
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("billing_orders.status IN ?", []model.BillingStatus{
			model.BillingStatusReserved, model.BillingStatusRunning, model.BillingStatusUncertain,
		}).
		Where(`(
			billing_orders.idempotency_key LIKE ? OR billing_orders.idempotency_key LIKE ? OR EXISTS (
				SELECT 1 FROM agent_production_artifacts
				JOIN agent_production_plan_versions ON agent_production_plan_versions.id = agent_production_artifacts.plan_version_id
				WHERE agent_production_artifacts.billing_order_id = billing_orders.id
				  AND agent_production_plan_versions.created_by_run_id = ?
			)
		)`, "agent-runtime:"+runID+":%", "proxy-token:agent-runtime:"+runID+":%", runID)
	if len(taskIDs) > 0 {
		query = query.Where("billing_orders.task_id NOT IN ?", taskIDs)
	}
	if len(billingOrderIDs) > 0 {
		query = query.Where("billing_orders.id NOT IN ?", billingOrderIDs)
	}
	err := query.Limit(1).Find(&rows).Error
	if err != nil {
		return false, err
	}
	return len(rows) != 0, nil
}

type adminAgentTaskCancelUpdates struct {
	Status         model.TaskStatus `gorm:"column:status"`
	Stage          string           `gorm:"column:stage"`
	PollStage      string           `gorm:"column:poll_stage"`
	NextPollAt     *time.Time       `gorm:"column:next_poll_at"`
	CompletedAt    *time.Time       `gorm:"column:completed_at"`
	LeaseOwner     string           `gorm:"column:lease_owner"`
	LeaseExpiresAt *time.Time       `gorm:"column:lease_expires_at"`
	UpdatedAt      time.Time        `gorm:"column:updated_at"`
}

func disposeAdminAgentRunControlTargets(
	db *gorm.DB,
	targets []adminAgentRunControlTarget,
	reason string,
	now time.Time,
	allowRunningReconciliation bool,
) ([]AdminAgentRunTaskDisposition, bool, error) {
	dispositions := make([]AdminAgentRunTaskDisposition, 0, len(targets))
	reconciliationPending := false
	for _, target := range targets {
		providerSubmitted := strings.TrimSpace(target.ProviderRequestID) != ""
		providerPossiblySubmitted := providerSubmitted || (allowRunningReconciliation && target.Status == model.TaskStatusRunning)
		billingStatus := model.BillingStatus("")
		if target.BillingOrderID != "" {
			var order model.BillingOrder
			if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", target.BillingOrderID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, false, ErrAdminAgentRunBillingUnresolved
				}
				return nil, false, err
			}
			if order.UserID != target.UserID || (order.TaskID != "" && order.TaskID != target.TaskID) {
				return nil, false, ErrAdminAgentRunBillingUnresolved
			}
			providerSubmitted = providerSubmitted || strings.TrimSpace(order.ProviderRequestID) != ""
			providerPossiblySubmitted = providerPossiblySubmitted || providerSubmitted
			switch {
			case !providerPossiblySubmitted && target.Status == model.TaskStatusQueued && order.Status == model.BillingStatusReserved:
				if err := refundBillingOrderTx(db, &order, "终止未提交的 Agent 任务："+reason); err != nil {
					return nil, false, err
				}
				billingStatus = model.BillingStatusRefunded
			case providerPossiblySubmitted && (order.Status == model.BillingStatusReserved || order.Status == model.BillingStatusRunning || order.Status == model.BillingStatusUncertain):
				if order.Status != model.BillingStatusUncertain {
					updates := uncertainBillingUpdates(order, "管理员终止已提交的 Agent 任务，费用状态待核对", now)
					updated := db.Model(&model.BillingOrder{}).Where("id = ? AND status = ?", order.ID, order.Status).Updates(updates)
					if updated.Error != nil {
						return nil, false, updated.Error
					}
					if updated.RowsAffected != 1 {
						return nil, false, ErrAdminAgentRunBillingUnresolved
					}
				}
				billingStatus = model.BillingStatusUncertain
				reconciliationPending = true
			case !providerPossiblySubmitted && adminAgentRunBillingStatusUnresolved(order.Status):
				return nil, false, ErrAdminAgentRunBillingUnresolved
			default:
				billingStatus = order.Status
			}
		}
		pollStage := ""
		var nextPollAt *time.Time
		stage := agentRunInterruptedStage
		if providerPossiblySubmitted {
			pollStage = "cancel_reconcile"
			next := now
			nextPollAt = &next
			stage = "已请求取消上游 Agent 任务，等待结果核对"
			reconciliationPending = true
		}
		result := db.Model(&model.Task{}).
			Where("id = ? AND user_id = ? AND status = ?", target.TaskID, target.UserID, target.Status).
			Select("status", "stage", "poll_stage", "next_poll_at", "completed_at", "lease_owner", "lease_expires_at", "updated_at").
			Updates(adminAgentTaskCancelUpdates{
				Status: model.TaskStatusCancelled, Stage: stage, PollStage: pollStage, NextPollAt: nextPollAt,
				CompletedAt: &now, LeaseOwner: "", LeaseExpiresAt: nil, UpdatedAt: now,
			})
		if result.Error != nil {
			return nil, false, result.Error
		}
		if result.RowsAffected != 1 {
			return nil, false, ErrAdminAgentRunStateConflict
		}
		dispositions = append(dispositions, AdminAgentRunTaskDisposition{
			TaskID: target.TaskID, Kind: target.Kind, PreviousStatus: target.Status, Status: model.TaskStatusCancelled,
			BillingStatus: billingStatus, ProviderRequestSubmitted: providerSubmitted,
			ReconciliationPending: providerPossiblySubmitted || billingStatus == model.BillingStatusUncertain,
		})
	}
	return dispositions, reconciliationPending, nil
}

func loadAdminAgentRunScopeFacts(db *gorm.DB, runID string) (adminAgentRunScopeFacts, error) {
	var facts adminAgentRunScopeFacts
	result := db.Table("agent_runs AS runs").
		Select(`runs.id AS run_id, runs.thread_id AS thread_id, runs.actor_user_id AS actor_user_id,
			runs.status AS status, runs.state_version AS state_version, runs.runtime_version AS runtime_version,
			threads.tenant_kind AS tenant_kind,
			threads.tenant_id AS tenant_id, threads.created_by_user_id AS created_by_user_id,
			threads.domain_project_id AS domain_project_id, threads.canvas_id AS canvas_id`).
		Joins("JOIN agent_threads AS threads ON threads.id = runs.thread_id").
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("runs.id = ?", runID).
		Take(&facts)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return adminAgentRunScopeFacts{}, ErrAdminAgentRunNotFound
	}
	if result.Error != nil {
		return adminAgentRunScopeFacts{}, result.Error
	}
	if facts.ActorUserID == "" || facts.ActorUserID != facts.CreatedByUserID {
		return adminAgentRunScopeFacts{}, ErrAdminAgentRunNotFound
	}
	return facts, nil
}

func failAdminAgentRunPendingFacts(db *gorm.DB, scope agentruntime.Scope, previous agentruntime.RuntimeState, now time.Time) error {
	if previous.PendingToolCall == nil || previous.PendingToolStarted {
		return nil
	}
	outputJSON := `{"reason":"` + adminAgentRunInterruptedCode + `"}`
	result := db.Model(&model.AgentToolCall{}).
		Where("run_id = ? AND tool_call_id = ? AND action_version = ? AND status IN ?", scope.RunID,
			previous.PendingToolCall.ToolCallID, previous.PendingToolCall.ActionVersion,
			[]agentruntime.ToolCallStatus{agentruntime.ToolCallPending, agentruntime.ToolCallWaitingApproval}).
		Select("status", "output_json", "error_code", "updated_at").
		Updates(agentToolCompletionUpdates{
			Status: agentruntime.ToolCallFailed, OutputJSON: outputJSON, ErrorCode: adminAgentRunInterruptedCode, UpdatedAt: now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrAdminAgentRunStateConflict
	}
	if previous.PendingToolCall.ToolName != agentruntime.ToolProductionRender {
		return nil
	}
	artifactResult := db.Model(&model.AgentProductionArtifact{}).
		Where(`status IN ? AND task_id = '' AND billing_order_id = '' AND resource_id = '' AND EXISTS (
			SELECT 1 FROM agent_production_plan_versions
			 WHERE agent_production_plan_versions.id = agent_production_artifacts.plan_version_id
			   AND agent_production_plan_versions.created_by_run_id = ?
		)`, []model.AgentProductionArtifactStatus{
			model.AgentProductionArtifactPlanned, model.AgentProductionArtifactAwaitingApproval,
		}, scope.RunID).
		Select("status", "last_error_code", "updated_at").
		Updates(productionRenderApprovalUpdate{
			Status: model.AgentProductionArtifactFailed, LastErrorCode: adminAgentRunInterruptedCode, UpdatedAt: now,
		})
	return artifactResult.Error
}
