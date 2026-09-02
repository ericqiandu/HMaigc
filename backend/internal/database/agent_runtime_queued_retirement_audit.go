package database

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	queuedRunBlockerFutureContract       = "future_contract"
	queuedRunBlockerUnsupportedContract  = "unsupported_contract"
	queuedRunBlockerNotPristine          = "queued_not_pristine"
	queuedRunBlockerExternalModelTask    = "external_model_task"
	queuedRunBlockerExternalBilling      = "external_billing"
	queuedRunBlockerExternalToolCall     = "external_tool_call"
	queuedRunBlockerExternalPlan         = "external_production_plan"
	queuedRunBlockerExternalTimeline     = "external_timeline_item"
	queuedRunBlockerEventHistoryInvalid  = "event_history_invalid"
	queuedRunBlockerInitialEventMissing  = "initial_event_missing"
	queuedRunBlockerCheckpointMissing    = "checkpoint_missing"
	queuedRunBlockerCheckpointTooLarge   = "checkpoint_too_large"
	queuedRunBlockerCheckpointInvalid    = "checkpoint_decode_invalid"
	queuedRunBlockerCheckpointTrailing   = "checkpoint_trailing_data"
	queuedRunBlockerCheckpointMismatch   = "checkpoint_mismatch"
	queuedRunBlockerInitialFactsMismatch = "initial_facts_mismatch"
	queuedRunBlockerInitialEventTooLarge = "initial_event_too_large"
	queuedRunBlockerTerminalInvalid      = "terminal_state_invalid"
	queuedRunBlockerTerminalTooLarge     = "terminal_event_too_large"
)

type queuedRunRetirementPlan struct {
	Run            model.AgentRun
	TerminalStatus agentruntime.RunStatus
	StateVersion   int
	StepNumber     int
	StateJSON      []byte
}

type queuedRunContractKind string

const (
	queuedRunContractUnsupported queuedRunContractKind = "unsupported"
	queuedRunContractLegacy      queuedRunContractKind = "legacy"
	queuedRunContractCurrent     queuedRunContractKind = "current"
)

func buildQueuedRunRetirementBatch(db *gorm.DB, lockRows bool) ([]queuedRunRetirementPlan, agentRuntimeRetirementSubsetAudit, error) {
	runsQuery := db.Where(
		"status = ? AND (tool_schema_version <> ? OR runtime_version <> ? OR policy_version <> ?)",
		agentruntime.RunQueued,
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
	plans := make([]queuedRunRetirementPlan, 0, len(runs))
	for _, run := range runs {
		plan, blockers, err := auditQueuedRunRetirement(db, run, lockRows)
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

func auditQueuedRunRetirement(db *gorm.DB, run model.AgentRun, lockRows bool) (queuedRunRetirementPlan, []AgentRuntimeUpgradeBlocker, error) {
	blockers := make([]AgentRuntimeUpgradeBlocker, 0, 10)
	appendBlocker := func(category, factStatus string, count int64) {
		blockers = append(blockers, newAgentRuntimeUpgradeBlocker(run, category, factStatus, count))
	}
	contractKind := queuedRunRetirementContract(run)
	if run.ToolSchemaVersion > agentruntime.CurrentToolSchemaVersion ||
		run.RuntimeVersion > agentruntime.CurrentRuntimeVersion ||
		run.PolicyVersion > agentruntime.CurrentPolicyVersion {
		appendBlocker(queuedRunBlockerFutureContract, "", 0)
	} else if contractKind == queuedRunContractUnsupported {
		appendBlocker(queuedRunBlockerUnsupportedContract, "", 0)
	}
	if run.StateVersion != 1 || run.StepNumber != 0 || run.LastEventSequence != 1 {
		appendBlocker(queuedRunBlockerNotPristine, "", 0)
	}

	externalBlockers, err := auditQueuedRunExternalFacts(db, run)
	if err != nil {
		return queuedRunRetirementPlan{}, nil, err
	}
	blockers = append(blockers, externalBlockers...)

	var eventCount int64
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ?", run.ID).Count(&eventCount).Error; err != nil {
		return queuedRunRetirementPlan{}, nil, err
	}
	if eventCount != 1 {
		appendBlocker(queuedRunBlockerEventHistoryInvalid, "", eventCount)
	}
	eventQuery := db.Where("run_id = ? AND sequence = ?", run.ID, run.LastEventSequence).Limit(1)
	if lockRows {
		eventQuery = eventQuery.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var initialEvent model.AgentRunEvent
	eventResult := eventQuery.Find(&initialEvent)
	if eventResult.Error != nil {
		return queuedRunRetirementPlan{}, nil, eventResult.Error
	}
	if eventResult.RowsAffected != 1 {
		appendBlocker(queuedRunBlockerInitialEventMissing, "", 0)
	}

	checkpointQuery := db.Where("run_id = ?", run.ID).Order("sequence DESC").Limit(1)
	if lockRows {
		checkpointQuery = checkpointQuery.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var checkpoint model.AgentCheckpoint
	checkpointResult := checkpointQuery.Find(&checkpoint)
	if checkpointResult.Error != nil {
		return queuedRunRetirementPlan{}, nil, checkpointResult.Error
	}
	var legacyState *legacyAgentRuntimeStateV1
	var currentState *agentruntime.RuntimeState
	if checkpointResult.RowsAffected != 1 {
		appendBlocker(queuedRunBlockerCheckpointMissing, "", 0)
	} else if len(checkpoint.StateJSON) > agentRuntimeMigrationCheckpointPayloadLimit {
		appendBlocker(queuedRunBlockerCheckpointTooLarge, "", int64(len(checkpoint.StateJSON)))
	} else if contractKind == queuedRunContractLegacy {
		decoded, valid, trailing := decodeLegacyQueuedRunState(checkpoint.StateJSON)
		if !valid {
			appendBlocker(queuedRunBlockerCheckpointInvalid, "", 0)
		} else {
			legacyState = &decoded
			if trailing {
				appendBlocker(queuedRunBlockerCheckpointTrailing, "", 0)
			}
			if decoded.Status != run.Status || decoded.StateVersion != run.StateVersion || decoded.StepNumber != run.StepNumber ||
				decoded.MaxSteps != run.MaxSteps || checkpoint.StateVersion != run.StateVersion || checkpoint.Sequence != run.LastEventSequence {
				appendBlocker(queuedRunBlockerCheckpointMismatch, "", 0)
			}
		}
	} else if contractKind == queuedRunContractCurrent {
		decoded, valid, trailing := decodeCurrentQueuedRunState(checkpoint.StateJSON)
		if !valid {
			appendBlocker(queuedRunBlockerCheckpointInvalid, "", 0)
		} else {
			currentState = &decoded
			if trailing {
				appendBlocker(queuedRunBlockerCheckpointTrailing, "", 0)
			}
			if decoded.Status != run.Status || decoded.StateVersion != run.StateVersion || decoded.StepNumber != run.StepNumber ||
				decoded.MaxSteps != run.MaxSteps || checkpoint.StateVersion != run.StateVersion || checkpoint.Sequence != run.LastEventSequence {
				appendBlocker(queuedRunBlockerCheckpointMismatch, "", 0)
			}
		}
	}
	if eventResult.RowsAffected == 1 {
		if len(initialEvent.PayloadJSON) > agentRuntimeMigrationEventPayloadLimit {
			appendBlocker(queuedRunBlockerInitialEventTooLarge, "", int64(len(initialEvent.PayloadJSON)))
		}
		if checkpointResult.RowsAffected == 1 && (initialEvent.Kind != agentruntime.EventRunCreated || initialEvent.PayloadJSON != checkpoint.StateJSON) {
			appendBlocker(queuedRunBlockerInitialFactsMismatch, "", 0)
		}
	}
	if len(blockers) != 0 {
		return queuedRunRetirementPlan{}, blockers, nil
	}

	var terminalStatus agentruntime.RunStatus
	var stateVersion int
	var stepNumber int
	var terminalJSON []byte
	if contractKind == queuedRunContractLegacy && legacyState != nil {
		terminal, terminalErr := retireLegacyAgentRuntimeStateV1(*legacyState)
		if terminalErr == nil {
			terminalJSON, terminalErr = json.Marshal(terminal)
			terminalStatus, stateVersion, stepNumber = terminal.Status, terminal.StateVersion, terminal.StepNumber
		}
		if terminalErr != nil {
			appendBlocker(queuedRunBlockerTerminalInvalid, "", 0)
		}
	} else if contractKind == queuedRunContractCurrent && currentState != nil {
		transition, terminalErr := agentruntime.Terminate(*currentState, retiredAgentRuntimeContractFailureCode)
		if terminalErr == nil {
			terminalJSON, terminalErr = json.Marshal(transition.State)
			terminalStatus, stateVersion, stepNumber = transition.State.Status, transition.State.StateVersion, transition.State.StepNumber
		}
		if terminalErr != nil {
			appendBlocker(queuedRunBlockerTerminalInvalid, "", 0)
		}
	} else {
		appendBlocker(queuedRunBlockerTerminalInvalid, "", 0)
	}
	if len(terminalJSON) > agentRuntimeMigrationEventPayloadLimit {
		appendBlocker(queuedRunBlockerTerminalTooLarge, "", int64(len(terminalJSON)))
	}
	if len(blockers) != 0 {
		return queuedRunRetirementPlan{}, blockers, nil
	}
	return queuedRunRetirementPlan{
		Run: run, TerminalStatus: terminalStatus, StateVersion: stateVersion,
		StepNumber: stepNumber, StateJSON: terminalJSON,
	}, blockers, nil
}

func queuedRunRetirementContract(run model.AgentRun) queuedRunContractKind {
	if run.ToolSchemaVersion == legacyAgentToolSchemaVersion &&
		run.RuntimeVersion <= agentruntime.CurrentRuntimeVersion && run.PolicyVersion <= agentruntime.CurrentPolicyVersion {
		return queuedRunContractLegacy
	}
	if run.ToolSchemaVersion == agentruntime.RetiredToolSchemaVersion &&
		run.RuntimeVersion == agentruntime.RetiredRuntimeVersion && run.PolicyVersion == agentruntime.RetiredPolicyVersion {
		return queuedRunContractCurrent
	}
	if run.ToolSchemaVersion == agentruntime.RetiredCloudToolSchemaVersion &&
		run.RuntimeVersion == agentruntime.CurrentRuntimeVersion && run.PolicyVersion == agentruntime.CurrentPolicyVersion {
		return queuedRunContractCurrent
	}
	if run.ToolSchemaVersion == agentruntime.CurrentToolSchemaVersion &&
		run.RuntimeVersion <= agentruntime.CurrentRuntimeVersion && run.PolicyVersion <= agentruntime.CurrentPolicyVersion &&
		(run.RuntimeVersion != agentruntime.CurrentRuntimeVersion || run.PolicyVersion != agentruntime.CurrentPolicyVersion) {
		return queuedRunContractCurrent
	}
	return queuedRunContractUnsupported
}

func decodeLegacyQueuedRunState(stateJSON string) (legacyAgentRuntimeStateV1, bool, bool) {
	decoder := json.NewDecoder(bytes.NewBufferString(stateJSON))
	decoder.DisallowUnknownFields()
	var state legacyAgentRuntimeStateV1
	if err := decoder.Decode(&state); err != nil {
		return legacyAgentRuntimeStateV1{}, false, false
	}
	var trailing json.RawMessage
	return state, true, !errors.Is(decoder.Decode(&trailing), io.EOF)
}

func decodeCurrentQueuedRunState(stateJSON string) (agentruntime.RuntimeState, bool, bool) {
	decoder := json.NewDecoder(bytes.NewBufferString(stateJSON))
	decoder.DisallowUnknownFields()
	var state agentruntime.RuntimeState
	if err := decoder.Decode(&state); err != nil {
		return agentruntime.RuntimeState{}, false, false
	}
	var trailing json.RawMessage
	return state, true, !errors.Is(decoder.Decode(&trailing), io.EOF)
}

func auditQueuedRunExternalFacts(db *gorm.DB, run model.AgentRun) ([]AgentRuntimeUpgradeBlocker, error) {
	blockers := make([]AgentRuntimeUpgradeBlocker, 0, 5)
	operation := legacyAgentModelTaskOperationPrefix + run.ID
	expectedTaskID := legacyAgentModelTaskID(run.ID, 0)
	tasks, err := agentRuntimeUpgradeStatusCounts(db.Model(&model.Task{}).
		Select("CAST(status AS TEXT) AS status, COUNT(*) AS count").
		Where("id = ? OR operation = ?", expectedTaskID, operation).Group("status").Order("status"))
	if err != nil {
		return nil, err
	}
	blockers = appendAgentRuntimeUpgradeStatusBlockers(blockers, run, queuedRunBlockerExternalModelTask, tasks)

	billingKeys := []string{"agent-runtime:" + run.ID + ":0", "proxy-token:agent-runtime:" + run.ID + ":0"}
	billing, err := agentRuntimeUpgradeStatusCounts(db.Model(&model.BillingOrder{}).
		Select("CAST(status AS TEXT) AS status, COUNT(*) AS count").
		Where("user_id = ? AND idempotency_key IN ?", run.ActorUserID, billingKeys).Group("status").Order("status"))
	if err != nil {
		return nil, err
	}
	blockers = appendAgentRuntimeUpgradeStatusBlockers(blockers, run, queuedRunBlockerExternalBilling, billing)

	tools, err := agentRuntimeUpgradeStatusCounts(db.Model(&model.AgentToolCall{}).
		Select("CAST(status AS TEXT) AS status, COUNT(*) AS count").
		Where("run_id = ?", run.ID).Group("status").Order("status"))
	if err != nil {
		return nil, err
	}
	blockers = appendAgentRuntimeUpgradeStatusBlockers(blockers, run, queuedRunBlockerExternalToolCall, tools)

	timeline, err := agentRuntimeUpgradeStatusCounts(db.Model(&model.AgentTimelineItem{}).
		Select("CAST(status AS TEXT) AS status, COUNT(*) AS count").
		Where("run_id = ?", run.ID).Group("status").Order("status"))
	if err != nil {
		return nil, err
	}
	blockers = appendAgentRuntimeUpgradeStatusBlockers(blockers, run, queuedRunBlockerExternalTimeline, timeline)

	plans, err := agentRuntimeUpgradeStatusCounts(db.Model(&model.AgentProductionPlanVersion{}).
		Select("CAST(status AS TEXT) AS status, COUNT(*) AS count").
		Where("created_by_run_id = ?", run.ID).Group("status").Order("status"))
	if err != nil {
		return nil, err
	}
	return appendAgentRuntimeUpgradeStatusBlockers(blockers, run, queuedRunBlockerExternalPlan, plans), nil
}

func applyQueuedRunRetirementPlan(db *gorm.DB, plan queuedRunRetirementPlan, now time.Time) error {
	run := plan.Run
	sequence := run.LastEventSequence + 1
	updated := db.Model(&model.AgentRun{}).
		Where(
			"id = ? AND status = ? AND tool_schema_version = ? AND runtime_version = ? AND policy_version = ? AND state_version = ? AND step_number = ? AND last_event_sequence = ?",
			run.ID, agentruntime.RunQueued, run.ToolSchemaVersion, run.RuntimeVersion, run.PolicyVersion,
			run.StateVersion, run.StepNumber, run.LastEventSequence,
		).
		Select("status", "state_version", "step_number", "last_event_sequence", "updated_at", "completed_at").
		Updates(retiredAgentRunUpdates{
			Status: plan.TerminalStatus, StateVersion: plan.StateVersion, StepNumber: plan.StepNumber,
			LastEventSequence: sequence, UpdatedAt: now, CompletedAt: now,
		})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return fmt.Errorf("%s: queued retirement state conflict: run_id=%s", agentRuntimeRetirementInvalidCode, run.ID)
	}
	event := model.AgentRunEvent{
		ID:    agentRuntimeMigrationFactID("event", run.ID, strconv.FormatInt(sequence, 10)),
		RunID: run.ID, Sequence: sequence, Kind: agentruntime.EventRunFailed,
		PayloadJSON: string(plan.StateJSON), CreatedAt: now,
	}
	if err := db.Create(&event).Error; err != nil {
		return err
	}
	checkpoint := model.AgentCheckpoint{
		ID:    agentRuntimeMigrationFactID("checkpoint", run.ID, strconv.FormatInt(sequence, 10)),
		RunID: run.ID, Sequence: sequence, StateVersion: plan.StateVersion,
		StateJSON: string(plan.StateJSON), CreatedAt: now,
	}
	return db.Create(&checkpoint).Error
}
