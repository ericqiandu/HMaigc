package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const agentRuntimeUpgradeBlockerNonRetirableStatus = "non_retirable_active_status"

type AgentRuntimeUpgradeBlocker struct {
	RunID             string                 `json:"runId"`
	Status            agentruntime.RunStatus `json:"status"`
	ToolSchemaVersion int                    `json:"toolSchemaVersion"`
	RuntimeVersion    int                    `json:"runtimeVersion"`
	PolicyVersion     int                    `json:"policyVersion"`
	Category          string                 `json:"category"`
	FactStatus        string                 `json:"factStatus,omitempty"`
	Count             int64                  `json:"count,omitempty"`
}

type AgentRuntimeUpgradeAudit struct {
	CandidateRuns int                          `json:"candidateRuns"`
	RetirableRuns int                          `json:"retirableRuns"`
	Blockers      []AgentRuntimeUpgradeBlocker `json:"blockers"`
}

type agentRuntimeUpgradeBatch struct {
	QueuedPlans []queuedRunRetirementPlan
	PausedPlans []pausedRunRetirementPlan
}

func AuditAgentRuntimeUpgrade(db *gorm.DB) (AgentRuntimeUpgradeAudit, error) {
	var audit AgentRuntimeUpgradeAudit
	err := db.Transaction(func(tx *gorm.DB) error {
		_, result, buildErr := buildAgentRuntimeUpgradeBatch(tx, false)
		if buildErr != nil {
			return buildErr
		}
		audit = result
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return audit, err
}

func buildAgentRuntimeUpgradeBatch(db *gorm.DB, lockRows bool) (agentRuntimeUpgradeBatch, AgentRuntimeUpgradeAudit, error) {
	candidateOrder, err := agentRuntimeUpgradeCandidateOrder(db, lockRows)
	if err != nil {
		return agentRuntimeUpgradeBatch{}, AgentRuntimeUpgradeAudit{}, err
	}
	queuedPlans, queuedAudit, err := buildQueuedRunRetirementBatch(db, lockRows)
	if err != nil {
		return agentRuntimeUpgradeBatch{}, AgentRuntimeUpgradeAudit{}, err
	}
	pausedPlans, pausedAudit, err := buildPausedRunRetirementBatch(db, lockRows)
	if err != nil {
		return agentRuntimeUpgradeBatch{}, AgentRuntimeUpgradeAudit{}, err
	}
	activeBlockers, err := auditNonRetirableIncompatibleAgentRuns(db, lockRows)
	if err != nil {
		return agentRuntimeUpgradeBatch{}, AgentRuntimeUpgradeAudit{}, err
	}
	audit := AgentRuntimeUpgradeAudit{
		CandidateRuns: len(candidateOrder),
		RetirableRuns: len(queuedPlans) + len(pausedPlans),
		Blockers:      make([]AgentRuntimeUpgradeBlocker, 0, len(queuedAudit.Blockers)+len(pausedAudit.Blockers)+len(activeBlockers)),
	}
	audit.Blockers = append(audit.Blockers, queuedAudit.Blockers...)
	audit.Blockers = append(audit.Blockers, pausedAudit.Blockers...)
	audit.Blockers = append(audit.Blockers, activeBlockers...)
	if err := validateAgentRuntimeUpgradeCoverage(candidateOrder, queuedPlans, pausedPlans, audit.Blockers); err != nil {
		return agentRuntimeUpgradeBatch{}, AgentRuntimeUpgradeAudit{}, err
	}
	ordinalByRunID := make(map[string]int, len(candidateOrder))
	for ordinal, runID := range candidateOrder {
		ordinalByRunID[runID] = ordinal
	}
	sort.Slice(audit.Blockers, func(left, right int) bool {
		leftOrdinal, leftFound := ordinalByRunID[audit.Blockers[left].RunID]
		rightOrdinal, rightFound := ordinalByRunID[audit.Blockers[right].RunID]
		if leftFound != rightFound {
			return leftFound
		}
		if leftOrdinal != rightOrdinal {
			return leftOrdinal < rightOrdinal
		}
		if audit.Blockers[left].RunID != audit.Blockers[right].RunID {
			return audit.Blockers[left].RunID < audit.Blockers[right].RunID
		}
		if audit.Blockers[left].Category != audit.Blockers[right].Category {
			return audit.Blockers[left].Category < audit.Blockers[right].Category
		}
		return audit.Blockers[left].FactStatus < audit.Blockers[right].FactStatus
	})
	return agentRuntimeUpgradeBatch{QueuedPlans: queuedPlans, PausedPlans: pausedPlans}, audit, nil
}

func validateAgentRuntimeUpgradeCoverage(
	candidateOrder []string,
	queuedPlans []queuedRunRetirementPlan,
	pausedPlans []pausedRunRetirementPlan,
	blockers []AgentRuntimeUpgradeBlocker,
) error {
	const (
		retirementDisposition uint8 = 1 << iota
		blockingDisposition
	)
	candidates := make(map[string]struct{}, len(candidateOrder))
	duplicateCandidates := 0
	for _, runID := range candidateOrder {
		if _, exists := candidates[runID]; exists {
			duplicateCandidates++
		}
		candidates[runID] = struct{}{}
	}
	dispositions := make(map[string]uint8, len(candidates))
	unknown := make(map[string]struct{})
	duplicatePlans := 0
	markPlan := func(runID string) {
		if _, exists := candidates[runID]; !exists {
			unknown[runID] = struct{}{}
			return
		}
		if dispositions[runID]&retirementDisposition != 0 {
			duplicatePlans++
		}
		dispositions[runID] |= retirementDisposition
	}
	for _, plan := range queuedPlans {
		markPlan(plan.Run.ID)
	}
	for _, plan := range pausedPlans {
		markPlan(plan.Run.ID)
	}
	for _, blocker := range blockers {
		if _, exists := candidates[blocker.RunID]; !exists {
			unknown[blocker.RunID] = struct{}{}
			continue
		}
		dispositions[blocker.RunID] |= blockingDisposition
	}
	missing := 0
	overlap := 0
	accounted := 0
	for runID := range candidates {
		disposition := dispositions[runID]
		if disposition == 0 {
			missing++
			continue
		}
		accounted++
		if disposition == retirementDisposition|blockingDisposition {
			overlap++
		}
	}
	if duplicateCandidates != 0 || duplicatePlans != 0 || len(unknown) != 0 || missing != 0 || overlap != 0 {
		return fmt.Errorf(
			"agent runtime upgrade audit coverage mismatch: candidates=%d accounted=%d missing=%d overlap=%d unknown=%d duplicate_candidates=%d duplicate_plans=%d",
			len(candidates), accounted, missing, overlap, len(unknown), duplicateCandidates, duplicatePlans,
		)
	}
	return nil
}

func agentRuntimeUpgradeCandidateOrder(db *gorm.DB, lockRows bool) ([]string, error) {
	type candidateRun struct {
		ID string `gorm:"column:id"`
	}
	query := db.Model(&model.AgentRun{}).
		Select("id").
		Where(
			"status IN ? AND (tool_schema_version <> ? OR runtime_version <> ? OR policy_version <> ?)",
			[]agentruntime.RunStatus{
				agentruntime.RunQueued, agentruntime.RunRunning, agentruntime.RunWaitingInput,
				agentruntime.RunWaitingApproval, agentruntime.RunWaitingTool,
			},
			agentruntime.CurrentToolSchemaVersion,
			agentruntime.CurrentRuntimeVersion,
			agentruntime.CurrentPolicyVersion,
		).
		Order("created_at, id")
	if lockRows {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var candidates []candidateRun
	if err := query.Scan(&candidates).Error; err != nil {
		return nil, err
	}
	runIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		runIDs = append(runIDs, candidate.ID)
	}
	return runIDs, nil
}

func auditNonRetirableIncompatibleAgentRuns(db *gorm.DB, lockRows bool) ([]AgentRuntimeUpgradeBlocker, error) {
	query := db.Where(
		"status IN ? AND (tool_schema_version <> ? OR runtime_version <> ? OR policy_version <> ?)",
		[]agentruntime.RunStatus{agentruntime.RunRunning, agentruntime.RunWaitingTool},
		agentruntime.CurrentToolSchemaVersion,
		agentruntime.CurrentRuntimeVersion,
		agentruntime.CurrentPolicyVersion,
	).Order("created_at, id")
	if lockRows {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var runs []model.AgentRun
	if err := query.Find(&runs).Error; err != nil {
		return nil, err
	}
	blockers := make([]AgentRuntimeUpgradeBlocker, 0, len(runs))
	for _, run := range runs {
		blockers = append(blockers, newAgentRuntimeUpgradeBlocker(run, agentRuntimeUpgradeBlockerNonRetirableStatus, string(run.Status), 1))
		factBlockers, err := auditAgentRuntimeExecutionFacts(db, run, nil)
		if err != nil {
			return nil, err
		}
		blockers = append(blockers, factBlockers...)
	}
	return blockers, nil
}

func retireIncompatibleAgentRuntimeRuns(db *gorm.DB, now time.Time) error {
	if now.IsZero() {
		return fmt.Errorf("%s: retirement timestamp is required", agentRuntimeRetirementInvalidCode)
	}
	batch, audit, err := buildAgentRuntimeUpgradeBatch(db, true)
	if err != nil {
		return err
	}
	if len(audit.Blockers) != 0 {
		encoded, marshalErr := json.Marshal(audit)
		if marshalErr != nil {
			return marshalErr
		}
		return fmt.Errorf("%s: %s", agentRuntimeRetirementInvalidCode, encoded)
	}
	for _, plan := range batch.QueuedPlans {
		if err := applyQueuedRunRetirementPlan(db, plan, now); err != nil {
			return err
		}
	}
	for _, plan := range batch.PausedPlans {
		if err := applyPausedRunRetirementPlan(db, plan, now); err != nil {
			return err
		}
	}
	return nil
}
