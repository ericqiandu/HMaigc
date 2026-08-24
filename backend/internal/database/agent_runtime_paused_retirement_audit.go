package database

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	pausedRunBlockerFutureContract             = "future_contract"
	pausedRunBlockerCheckpointMissing          = "checkpoint_missing"
	pausedRunBlockerCheckpointTooLarge         = "checkpoint_too_large"
	pausedRunBlockerCheckpointDecodeInvalid    = "checkpoint_decode_invalid"
	pausedRunBlockerCheckpointTrailingData     = "checkpoint_trailing_data"
	pausedRunBlockerCheckpointMismatch         = "checkpoint_mismatch"
	pausedRunBlockerPendingToolStarted         = "pending_tool_started"
	pausedRunBlockerPendingToolTerminal        = "pending_tool_terminal"
	agentRuntimeBlockerStartedToolCall         = "started_tool_call"
	agentRuntimeBlockerActiveProviderTask      = "active_provider_task"
	agentRuntimeBlockerUnresolvedBilling       = "unresolved_billing"
	agentRuntimeBlockerActiveOrUnknownArtifact = "active_or_unknown_artifact"
	pausedRunBlockerInterruptInvalid           = "interrupt_invalid"
	pausedRunBlockerTerminalCheckpointLarge    = "terminal_checkpoint_too_large"
	pausedRunBlockerTerminalEventLarge         = "terminal_event_too_large"
)

type agentRuntimeRetirementSubsetAudit struct {
	CandidateRuns int                          `json:"candidateRuns"`
	RetirableRuns int                          `json:"retirableRuns"`
	Blockers      []AgentRuntimeUpgradeBlocker `json:"blockers"`
}

type pausedRunRetirementPlan struct {
	Run          model.AgentRun
	Terminal     agentruntime.RuntimeState
	StateJSON    []byte
	EventPayload string
}

type agentRuntimeUpgradeStatusCount struct {
	Status string `gorm:"column:status"`
	Count  int64  `gorm:"column:count"`
}

func auditIncompatiblePausedAgentRuns(db *gorm.DB) (agentRuntimeRetirementSubsetAudit, error) {
	_, audit, err := buildPausedRunRetirementBatch(db, false)
	return audit, err
}

func buildPausedRunRetirementBatch(db *gorm.DB, lockRows bool) ([]pausedRunRetirementPlan, agentRuntimeRetirementSubsetAudit, error) {
	runsQuery := db.Where(
		`status IN ? AND (tool_schema_version <> ? OR runtime_version <> ? OR policy_version <> ?)`,
		[]agentruntime.RunStatus{agentruntime.RunWaitingInput, agentruntime.RunWaitingApproval},
		agentruntime.CurrentToolSchemaVersion,
		agentruntime.CurrentRuntimeVersion,
		agentruntime.CurrentPolicyVersion,
	).Order("created_at, id")
	if lockRows {
		runsQuery = runsQuery.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var runs []model.AgentRun
	if err := runsQuery.Find(&runs).Error; err != nil {
		return nil, agentRuntimeRetirementSubsetAudit{}, err
	}

	audit := agentRuntimeRetirementSubsetAudit{CandidateRuns: len(runs), Blockers: []AgentRuntimeUpgradeBlocker{}}
	plans := make([]pausedRunRetirementPlan, 0, len(runs))
	for _, run := range runs {
		plan, blockers, err := auditPausedRunRetirement(db, run, lockRows)
		if err != nil {
			return nil, agentRuntimeRetirementSubsetAudit{}, err
		}
		sort.Slice(blockers, func(left, right int) bool {
			if blockers[left].Category != blockers[right].Category {
				return blockers[left].Category < blockers[right].Category
			}
			return blockers[left].FactStatus < blockers[right].FactStatus
		})
		audit.Blockers = append(audit.Blockers, blockers...)
		if len(blockers) == 0 {
			plans = append(plans, plan)
		}
	}
	audit.RetirableRuns = len(plans)
	return plans, audit, nil
}

func auditPausedRunRetirement(db *gorm.DB, run model.AgentRun, lockRows bool) (pausedRunRetirementPlan, []AgentRuntimeUpgradeBlocker, error) {
	blockers := make([]AgentRuntimeUpgradeBlocker, 0, 8)
	appendBlocker := func(category, factStatus string, count int64) {
		blockers = append(blockers, newAgentRuntimeUpgradeBlocker(run, category, factStatus, count))
	}
	if run.ToolSchemaVersion > agentruntime.CurrentToolSchemaVersion ||
		run.RuntimeVersion > agentruntime.CurrentRuntimeVersion ||
		run.PolicyVersion > agentruntime.CurrentPolicyVersion {
		appendBlocker(pausedRunBlockerFutureContract, "", 0)
	}

	checkpointQuery := db.Where("run_id = ?", run.ID).Order("sequence DESC").Limit(1)
	if lockRows {
		checkpointQuery = checkpointQuery.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var checkpoint model.AgentCheckpoint
	checkpointResult := checkpointQuery.Find(&checkpoint)
	if checkpointResult.Error != nil {
		return pausedRunRetirementPlan{}, nil, fmt.Errorf("load paused retirement checkpoint: run_id=%s: %w", run.ID, checkpointResult.Error)
	}
	var current *agentruntime.RuntimeState
	if checkpointResult.RowsAffected != 1 {
		appendBlocker(pausedRunBlockerCheckpointMissing, "", 0)
	} else if len(checkpoint.StateJSON) > agentRuntimeMigrationCheckpointPayloadLimit {
		appendBlocker(pausedRunBlockerCheckpointTooLarge, "", 0)
	} else {
		decoder := json.NewDecoder(bytes.NewBufferString(checkpoint.StateJSON))
		decoder.DisallowUnknownFields()
		var decoded agentruntime.RuntimeState
		if err := decoder.Decode(&decoded); err != nil {
			appendBlocker(pausedRunBlockerCheckpointDecodeInvalid, "", 0)
		} else {
			current = &decoded
			var trailing json.RawMessage
			if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
				appendBlocker(pausedRunBlockerCheckpointTrailingData, "", 0)
			}
			if checkpoint.Sequence != run.LastEventSequence || checkpoint.StateVersion != run.StateVersion ||
				decoded.Status != run.Status || decoded.StateVersion != run.StateVersion ||
				decoded.StepNumber != run.StepNumber || decoded.MaxSteps != run.MaxSteps {
				appendBlocker(pausedRunBlockerCheckpointMismatch, "", 0)
			}
			if decoded.PendingToolStarted {
				appendBlocker(pausedRunBlockerPendingToolStarted, "", 0)
			}
		}
	}

	factBlockers, err := auditAgentRuntimeExecutionFacts(db, run, current)
	if err != nil {
		return pausedRunRetirementPlan{}, nil, err
	}
	blockers = append(blockers, factBlockers...)
	if len(blockers) != 0 || current == nil {
		return pausedRunRetirementPlan{}, blockers, nil
	}

	transition, err := agentruntime.Interrupt(*current, run.StateVersion)
	if err != nil {
		appendBlocker(pausedRunBlockerInterruptInvalid, "", 0)
		return pausedRunRetirementPlan{}, blockers, nil
	}
	stateJSON, err := json.Marshal(transition.State)
	if err != nil {
		appendBlocker(pausedRunBlockerInterruptInvalid, "", 0)
		return pausedRunRetirementPlan{}, blockers, nil
	}
	if len(stateJSON) > agentRuntimeMigrationCheckpointPayloadLimit {
		appendBlocker(pausedRunBlockerTerminalCheckpointLarge, "", 0)
		return pausedRunRetirementPlan{}, blockers, nil
	}
	eventPayload, err := agentRuntimeMigrationInterruptPayload(stateJSON, run)
	if err != nil {
		appendBlocker(pausedRunBlockerTerminalEventLarge, "", 0)
		return pausedRunRetirementPlan{}, blockers, nil
	}
	return pausedRunRetirementPlan{Run: run, Terminal: transition.State, StateJSON: stateJSON, EventPayload: eventPayload}, blockers, nil
}

func auditAgentRuntimeExecutionFacts(db *gorm.DB, run model.AgentRun, current *agentruntime.RuntimeState) ([]AgentRuntimeUpgradeBlocker, error) {
	blockers := make([]AgentRuntimeUpgradeBlocker, 0, 5)
	terminalToolStatuses := []agentruntime.ToolCallStatus{agentruntime.ToolCallSucceeded, agentruntime.ToolCallFailed}
	if current != nil && current.PendingToolCall != nil {
		var count int64
		if err := db.Model(&model.AgentToolCall{}).Where(
			"run_id = ? AND tool_call_id = ? AND action_version = ? AND status IN ?",
			run.ID, current.PendingToolCall.ToolCallID, current.PendingToolCall.ActionVersion, terminalToolStatuses,
		).Count(&count).Error; err != nil {
			return nil, err
		}
		if count != 0 {
			blockers = append(blockers, newAgentRuntimeUpgradeBlocker(run, pausedRunBlockerPendingToolTerminal, "", count))
		}
	}

	startedTools, err := agentRuntimeUpgradeStatusCounts(db.Model(&model.AgentToolCall{}).
		Select("CAST(status AS TEXT) AS status, COUNT(*) AS count").
		Where("run_id = ? AND (status = ? OR (started_at IS NOT NULL AND status NOT IN ?))", run.ID, agentruntime.ToolCallRunning, terminalToolStatuses).
		Group("status").Order("status"))
	if err != nil {
		return nil, err
	}
	blockers = appendAgentRuntimeUpgradeStatusBlockers(blockers, run, agentRuntimeBlockerStartedToolCall, startedTools)

	planVersionIDs := db.Model(&model.AgentProductionPlanVersion{}).Select("id").Where("created_by_run_id = ?", run.ID)
	linkedTaskIDs := db.Model(&model.AgentProductionArtifact{}).Select("task_id").Where("plan_version_id IN (?) AND task_id <> ''", planVersionIDs)
	activeTasks, err := agentRuntimeUpgradeStatusCounts(db.Model(&model.Task{}).
		Select("CAST(status AS TEXT) AS status, COUNT(*) AS count").
		Where("(operation = ? OR id IN (?)) AND status IN ?", legacyAgentModelTaskOperationPrefix+run.ID, linkedTaskIDs, []model.TaskStatus{model.TaskStatusQueued, model.TaskStatusRunning}).
		Group("status").Order("status"))
	if err != nil {
		return nil, err
	}
	blockers = appendAgentRuntimeUpgradeStatusBlockers(blockers, run, agentRuntimeBlockerActiveProviderTask, activeTasks)

	linkedBillingIDs := db.Model(&model.AgentProductionArtifact{}).Select("billing_order_id").Where("plan_version_id IN (?) AND billing_order_id <> ''", planVersionIDs)
	unresolvedBilling, err := agentRuntimeUpgradeStatusCounts(db.Model(&model.BillingOrder{}).
		Select("CAST(status AS TEXT) AS status, COUNT(*) AS count").
		Where(
			`user_id = ? AND (idempotency_key LIKE ? OR idempotency_key LIKE ? OR id IN (?)) AND status IN ?`,
			run.ActorUserID, "agent-runtime:"+run.ID+":%", "proxy-token:agent-runtime:"+run.ID+":%", linkedBillingIDs,
			[]model.BillingStatus{model.BillingStatusReserved, model.BillingStatusRunning, model.BillingStatusUncertain},
		).Group("status").Order("status"))
	if err != nil {
		return nil, err
	}
	blockers = appendAgentRuntimeUpgradeStatusBlockers(blockers, run, agentRuntimeBlockerUnresolvedBilling, unresolvedBilling)

	unsafeArtifacts, err := agentRuntimeUpgradeStatusCounts(db.Model(&model.AgentProductionArtifact{}).
		Select("CAST(status AS TEXT) AS status, COUNT(*) AS count").
		Where("plan_version_id IN (?) AND status NOT IN ?", planVersionIDs, []model.AgentProductionArtifactStatus{
			model.AgentProductionArtifactPlanned, model.AgentProductionArtifactAwaitingApproval,
			model.AgentProductionArtifactSucceeded, model.AgentProductionArtifactFailed, model.AgentProductionArtifactCommitted,
		}).Group("status").Order("status"))
	if err != nil {
		return nil, err
	}
	blockers = appendAgentRuntimeUpgradeStatusBlockers(blockers, run, agentRuntimeBlockerActiveOrUnknownArtifact, unsafeArtifacts)
	return blockers, nil
}

func agentRuntimeUpgradeStatusCounts(query *gorm.DB) ([]agentRuntimeUpgradeStatusCount, error) {
	var counts []agentRuntimeUpgradeStatusCount
	if err := query.Scan(&counts).Error; err != nil {
		return nil, err
	}
	return counts, nil
}

func appendAgentRuntimeUpgradeStatusBlockers(
	blockers []AgentRuntimeUpgradeBlocker,
	run model.AgentRun,
	category string,
	counts []agentRuntimeUpgradeStatusCount,
) []AgentRuntimeUpgradeBlocker {
	for _, count := range counts {
		blockers = append(blockers, newAgentRuntimeUpgradeBlocker(run, category, count.Status, count.Count))
	}
	return blockers
}

func newAgentRuntimeUpgradeBlocker(run model.AgentRun, category, factStatus string, count int64) AgentRuntimeUpgradeBlocker {
	return AgentRuntimeUpgradeBlocker{
		RunID: run.ID, Status: run.Status,
		ToolSchemaVersion: run.ToolSchemaVersion, RuntimeVersion: run.RuntimeVersion, PolicyVersion: run.PolicyVersion,
		Category: category, FactStatus: factStatus, Count: count,
	}
}
