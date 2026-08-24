package database

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type agentRuntimeIntegrityIndex struct {
	name      string
	table     string
	columns   string
	predicate string
	unique    bool
	createSQL string
}

var agentRuntimeIntegrityIndexes = []agentRuntimeIntegrityIndex{
	{
		name: "idx_agent_runs_thread_client_request", table: "agent_runs", columns: "thread_id,client_request_id", unique: true,
		createSQL: `CREATE UNIQUE INDEX idx_agent_runs_thread_client_request ON agent_runs(thread_id, client_request_id)`,
	},
	{
		name: "idx_agent_run_events_run_sequence", table: "agent_run_events", columns: "run_id,sequence", unique: true,
		createSQL: `CREATE UNIQUE INDEX idx_agent_run_events_run_sequence ON agent_run_events(run_id, sequence)`,
	},
	{
		name: "idx_agent_checkpoints_run_sequence", table: "agent_checkpoints", columns: "run_id,sequence", unique: true,
		createSQL: `CREATE UNIQUE INDEX idx_agent_checkpoints_run_sequence ON agent_checkpoints(run_id, sequence)`,
	},
	{
		name: "idx_agent_timeline_items_run_ordinal", table: "agent_timeline_items", columns: "run_id,ordinal", unique: true,
		createSQL: `CREATE UNIQUE INDEX idx_agent_timeline_items_run_ordinal ON agent_timeline_items(run_id, ordinal)`,
	},
	{
		name: "idx_agent_timeline_items_run_sequence", table: "agent_timeline_items", columns: "run_id,source_event_sequence", unique: true,
		createSQL: `CREATE UNIQUE INDEX idx_agent_timeline_items_run_sequence ON agent_timeline_items(run_id, source_event_sequence)`,
	},
	{
		name: "idx_agent_timeline_items_thread_query", table: "agent_timeline_items", columns: "thread_id,created_at,id",
		createSQL: `CREATE INDEX idx_agent_timeline_items_thread_query ON agent_timeline_items(thread_id, created_at, id)`,
	},
	{
		name: "idx_agent_tool_calls_action", table: "agent_tool_calls", columns: "run_id,tool_call_id,action_version", unique: true,
		createSQL: `CREATE UNIQUE INDEX idx_agent_tool_calls_action ON agent_tool_calls(run_id, tool_call_id, action_version)`,
	},
	{
		name: "idx_agent_threads_scope", table: "agent_threads", columns: "tenant_kind,tenant_id,canvas_id,updated_at",
		createSQL: `CREATE INDEX idx_agent_threads_scope ON agent_threads(tenant_kind, tenant_id, canvas_id, updated_at)`,
	},
	{
		name: "idx_agent_production_plan_versions_scope_key_version", table: "agent_production_plan_versions", columns: "tenant_kind,tenant_id,domain_project_id,canvas_id,plan_key,version", unique: true,
		createSQL: `CREATE UNIQUE INDEX idx_agent_production_plan_versions_scope_key_version ON agent_production_plan_versions(tenant_kind, tenant_id, domain_project_id, canvas_id, plan_key, version)`,
	},
	{
		name: "idx_agent_production_artifacts_version_reference_shot_kind", table: "agent_production_artifacts", columns: "plan_version_id,reference_key,shot_key,kind", unique: true,
		createSQL: `CREATE UNIQUE INDEX idx_agent_production_artifacts_version_reference_shot_kind ON agent_production_artifacts(plan_version_id, reference_key, shot_key, kind)`,
	},
	{
		name: "idx_agent_production_artifacts_task", table: "agent_production_artifacts", columns: "task_id", predicate: "task_id <> ''", unique: true,
		createSQL: `CREATE UNIQUE INDEX idx_agent_production_artifacts_task ON agent_production_artifacts(task_id) WHERE task_id <> ''`,
	},
	{
		name: "idx_agent_production_artifacts_billing", table: "agent_production_artifacts", columns: "billing_order_id", predicate: "billing_order_id <> ''", unique: true,
		createSQL: `CREATE UNIQUE INDEX idx_agent_production_artifacts_billing ON agent_production_artifacts(billing_order_id) WHERE billing_order_id <> ''`,
	},
	{
		name: "idx_agent_production_artifacts_resource", table: "agent_production_artifacts", columns: "resource_id", predicate: "resource_id <> ''", unique: true,
		createSQL: `CREATE UNIQUE INDEX idx_agent_production_artifacts_resource ON agent_production_artifacts(resource_id) WHERE resource_id <> ''`,
	},
}

var legacyAgentProductionIndexes = []string{
	"idx_agent_production_plan_versions_key_version",
	"idx_agent_production_artifacts_plan_shot_kind",
	"idx_agent_production_artifacts_version_shot_kind",
}

// EnsureAgentRuntimeIntegritySchema creates only missing indexes after proving existing definitions and rows are safe.
func EnsureAgentRuntimeIntegritySchema(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		missing := make([]agentRuntimeIntegrityIndex, 0, len(agentRuntimeIntegrityIndexes))
		for _, specification := range agentRuntimeIntegrityIndexes {
			exists, err := verifyAgentRuntimeIntegrityIndex(tx, specification)
			if err != nil {
				return err
			}
			if !exists {
				missing = append(missing, specification)
			}
		}
		if err := rejectAgentRuntimeIntegrityConflicts(tx); err != nil {
			return err
		}
		if err := retireIncompatibleQueuedAgentRuns(tx); err != nil {
			return err
		}
		if err := retireIncompatiblePausedAgentRuns(tx, time.Now().UTC()); err != nil {
			return err
		}
		if err := rejectIncompatibleActiveAgentRuns(tx); err != nil {
			return err
		}
		for _, specification := range missing {
			if err := tx.Exec(specification.createSQL).Error; err != nil {
				return fmt.Errorf("create agent runtime integrity index %s: %w", specification.name, err)
			}
		}
		for _, legacyIndex := range legacyAgentProductionIndexes {
			if err := tx.Exec(`DROP INDEX IF EXISTS ` + legacyIndex).Error; err != nil {
				return fmt.Errorf("drop legacy agent production index %s: %w", legacyIndex, err)
			}
		}
		return nil
	})
}

const (
	retiredAgentToolSchemaFailureCode            = "tool_schema_retired"
	retiredAgentRuntimeContractFailureCode       = "runtime_contract_retired"
	legacyAgentToolSchemaVersion                 = 2
	agentRuntimeMigrationTargetToolSchemaVersion = 3
	legacyAgentModelTaskOperationPrefix          = "agent_model:"
	agentRuntimeMigrationEventPayloadLimit       = 256 * 1024
	agentRuntimeMigrationCheckpointPayloadLimit  = 1024 * 1024
)

type legacyAgentRuntimeStateV1 struct {
	StateVersion       int                                `json:"stateVersion"`
	StepNumber         int                                `json:"stepNumber"`
	MaxSteps           int                                `json:"maxSteps"`
	Status             agentruntime.RunStatus             `json:"status"`
	ExpectedDelivery   *agentruntime.ExpectedDelivery     `json:"expectedDelivery,omitempty"`
	Verification       *agentruntime.DeliveryVerification `json:"verification,omitempty"`
	PendingToolCall    *agentruntime.ToolCallDecision     `json:"pendingToolCall,omitempty"`
	PendingToolStarted bool                               `json:"pendingToolStarted,omitempty"`
	LastToolResult     *agentruntime.ToolResult           `json:"lastToolResult,omitempty"`
	FinalMessage       string                             `json:"finalMessage,omitempty"`
	FailureCode        string                             `json:"failureCode,omitempty"`
	UserMessage        string                             `json:"userMessage"`
}

type retiredAgentRunUpdates struct {
	Status            agentruntime.RunStatus `gorm:"column:status"`
	StateVersion      int                    `gorm:"column:state_version"`
	StepNumber        int                    `gorm:"column:step_number"`
	LastEventSequence int64                  `gorm:"column:last_event_sequence"`
	UpdatedAt         time.Time              `gorm:"column:updated_at"`
	CompletedAt       time.Time              `gorm:"column:completed_at"`
}

func retireIncompatibleQueuedAgentRuns(db *gorm.DB) error {
	if agentruntime.CurrentToolSchemaVersion != agentRuntimeMigrationTargetToolSchemaVersion {
		return nil
	}
	var runs []model.AgentRun
	result := db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(
			`status = ? AND tool_schema_version <= ? AND runtime_version <= ? AND policy_version <= ?
			 AND (tool_schema_version = ? OR runtime_version <> ? OR policy_version <> ?)`,
			agentruntime.RunQueued,
			agentruntime.CurrentToolSchemaVersion,
			agentruntime.CurrentRuntimeVersion,
			agentruntime.CurrentPolicyVersion,
			legacyAgentToolSchemaVersion,
			agentruntime.CurrentRuntimeVersion,
			agentruntime.CurrentPolicyVersion,
		).
		Order("created_at, id").
		Find(&runs)
	if result.Error != nil {
		return result.Error
	}
	for _, run := range runs {
		if err := verifyAgentRuntimeHasNoExternalFacts(db, run); err != nil {
			return err
		}
		var err error
		if run.ToolSchemaVersion == legacyAgentToolSchemaVersion {
			err = retireLegacyIncompatibleQueuedAgentRun(db, run, time.Now().UTC())
		} else {
			err = retireCurrentIncompatibleQueuedAgentRun(db, run, time.Now().UTC())
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func retireLegacyIncompatibleQueuedAgentRun(db *gorm.DB, run model.AgentRun, now time.Time) error {
	if run.StateVersion != 1 || run.StepNumber != 0 || run.LastEventSequence != 1 {
		return fmt.Errorf(
			"queued incompatible agent run is not pristine: run_id=%s state_version=%d step_number=%d last_event_sequence=%d",
			run.ID, run.StateVersion, run.StepNumber, run.LastEventSequence,
		)
	}
	var toolCallCount int64
	if err := db.Model(&model.AgentToolCall{}).Where("run_id = ?", run.ID).Count(&toolCallCount).Error; err != nil {
		return err
	}
	if toolCallCount != 0 {
		return fmt.Errorf("queued incompatible agent run has tool call facts: run_id=%s tool_calls=%d", run.ID, toolCallCount)
	}
	var eventCount int64
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ?", run.ID).Count(&eventCount).Error; err != nil {
		return err
	}
	if eventCount != 1 {
		return fmt.Errorf("queued incompatible agent run has invalid event history: run_id=%s events=%d", run.ID, eventCount)
	}
	var initialEvent model.AgentRunEvent
	if err := db.Where("run_id = ? AND sequence = ?", run.ID, run.LastEventSequence).Take(&initialEvent).Error; err != nil {
		return fmt.Errorf("load incompatible queued agent event: run_id=%s: %w", run.ID, err)
	}

	var checkpoint model.AgentCheckpoint
	if err := db.Where("run_id = ?", run.ID).Order("sequence DESC").Take(&checkpoint).Error; err != nil {
		return fmt.Errorf("load incompatible queued agent checkpoint: run_id=%s: %w", run.ID, err)
	}
	if len(checkpoint.StateJSON) > agentRuntimeMigrationCheckpointPayloadLimit {
		return fmt.Errorf("incompatible queued agent checkpoint is too large: run_id=%s bytes=%d", run.ID, len(checkpoint.StateJSON))
	}
	decoder := json.NewDecoder(bytes.NewBufferString(checkpoint.StateJSON))
	decoder.DisallowUnknownFields()
	var state legacyAgentRuntimeStateV1
	if err := decoder.Decode(&state); err != nil {
		return fmt.Errorf("decode incompatible queued agent checkpoint: run_id=%s: %w", run.ID, err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("incompatible queued agent checkpoint has trailing data: run_id=%s", run.ID)
	}
	if state.Status != agentruntime.RunQueued || state.StateVersion != run.StateVersion || state.StepNumber != run.StepNumber ||
		state.MaxSteps != run.MaxSteps || checkpoint.StateVersion != run.StateVersion || checkpoint.Sequence != run.LastEventSequence {
		return fmt.Errorf("incompatible queued agent checkpoint is inconsistent: run_id=%s", run.ID)
	}
	if initialEvent.Kind != agentruntime.EventRunCreated || initialEvent.PayloadJSON != checkpoint.StateJSON {
		return fmt.Errorf("incompatible queued agent initial facts disagree: run_id=%s", run.ID)
	}
	if len(initialEvent.PayloadJSON) > agentRuntimeMigrationEventPayloadLimit {
		return fmt.Errorf("incompatible queued agent event is too large: run_id=%s bytes=%d", run.ID, len(initialEvent.PayloadJSON))
	}

	terminal, err := retireLegacyAgentRuntimeStateV1(state)
	if err != nil {
		return fmt.Errorf("retire incompatible queued agent run: run_id=%s: %w", run.ID, err)
	}
	terminalJSON, err := json.Marshal(terminal)
	if err != nil {
		return err
	}
	if len(terminalJSON) > agentRuntimeMigrationEventPayloadLimit {
		return fmt.Errorf("retired incompatible agent event is too large: run_id=%s bytes=%d", run.ID, len(terminalJSON))
	}
	sequence := run.LastEventSequence + 1
	updated := db.Model(&model.AgentRun{}).
		Where("id = ? AND status = ? AND tool_schema_version = ? AND state_version = ? AND step_number = ? AND last_event_sequence = ?",
			run.ID, agentruntime.RunQueued, run.ToolSchemaVersion, run.StateVersion, run.StepNumber, run.LastEventSequence).
		Select("status", "state_version", "step_number", "last_event_sequence", "updated_at", "completed_at").
		Updates(retiredAgentRunUpdates{
			Status: terminal.Status, StateVersion: terminal.StateVersion,
			StepNumber: terminal.StepNumber, LastEventSequence: sequence,
			UpdatedAt: now, CompletedAt: now,
		})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return fmt.Errorf("retire incompatible queued agent run conflict: run_id=%s", run.ID)
	}
	event := model.AgentRunEvent{
		ID:    agentRuntimeMigrationFactID("event", run.ID, strconv.FormatInt(sequence, 10)),
		RunID: run.ID, Sequence: sequence, Kind: agentruntime.EventRunFailed,
		PayloadJSON: string(terminalJSON), CreatedAt: now,
	}
	if err := db.Create(&event).Error; err != nil {
		return err
	}
	checkpoint = model.AgentCheckpoint{
		ID:    agentRuntimeMigrationFactID("checkpoint", run.ID, strconv.FormatInt(sequence, 10)),
		RunID: run.ID, Sequence: sequence, StateVersion: terminal.StateVersion,
		StateJSON: string(terminalJSON), CreatedAt: now,
	}
	return db.Create(&checkpoint).Error
}

func retireCurrentIncompatibleQueuedAgentRun(db *gorm.DB, run model.AgentRun, now time.Time) error {
	if run.ToolSchemaVersion != agentruntime.CurrentToolSchemaVersion ||
		run.RuntimeVersion >= agentruntime.CurrentRuntimeVersion && run.PolicyVersion >= agentruntime.CurrentPolicyVersion {
		return fmt.Errorf(
			"queued incompatible agent run cannot use current-contract retirement: run_id=%s tool_schema_version=%d runtime_version=%d policy_version=%d",
			run.ID, run.ToolSchemaVersion, run.RuntimeVersion, run.PolicyVersion,
		)
	}
	if run.StateVersion != 1 || run.StepNumber != 0 || run.LastEventSequence != 1 {
		return fmt.Errorf(
			"queued incompatible agent run is not pristine: run_id=%s state_version=%d step_number=%d last_event_sequence=%d",
			run.ID, run.StateVersion, run.StepNumber, run.LastEventSequence,
		)
	}
	var eventCount int64
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ?", run.ID).Count(&eventCount).Error; err != nil {
		return err
	}
	if eventCount != 1 {
		return fmt.Errorf("queued incompatible agent run has invalid event history: run_id=%s events=%d", run.ID, eventCount)
	}
	var initialEvent model.AgentRunEvent
	if err := db.Where("run_id = ? AND sequence = ?", run.ID, run.LastEventSequence).Take(&initialEvent).Error; err != nil {
		return fmt.Errorf("load incompatible queued agent event: run_id=%s: %w", run.ID, err)
	}
	var checkpoint model.AgentCheckpoint
	if err := db.Where("run_id = ?", run.ID).Order("sequence DESC").Take(&checkpoint).Error; err != nil {
		return fmt.Errorf("load incompatible queued agent checkpoint: run_id=%s: %w", run.ID, err)
	}
	if len(checkpoint.StateJSON) > agentRuntimeMigrationCheckpointPayloadLimit {
		return fmt.Errorf("incompatible queued agent checkpoint is too large: run_id=%s bytes=%d", run.ID, len(checkpoint.StateJSON))
	}
	decoder := json.NewDecoder(bytes.NewBufferString(checkpoint.StateJSON))
	decoder.DisallowUnknownFields()
	var state agentruntime.RuntimeState
	if err := decoder.Decode(&state); err != nil {
		return fmt.Errorf("decode incompatible queued agent checkpoint: run_id=%s: %w", run.ID, err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("incompatible queued agent checkpoint has trailing data: run_id=%s", run.ID)
	}
	if state.Status != agentruntime.RunQueued || state.StateVersion != run.StateVersion || state.StepNumber != run.StepNumber ||
		state.MaxSteps != run.MaxSteps || checkpoint.StateVersion != run.StateVersion || checkpoint.Sequence != run.LastEventSequence {
		return fmt.Errorf("incompatible queued agent checkpoint is inconsistent: run_id=%s", run.ID)
	}
	if initialEvent.Kind != agentruntime.EventRunCreated || initialEvent.PayloadJSON != checkpoint.StateJSON {
		return fmt.Errorf("incompatible queued agent initial facts disagree: run_id=%s", run.ID)
	}
	if len(initialEvent.PayloadJSON) > agentRuntimeMigrationEventPayloadLimit {
		return fmt.Errorf("incompatible queued agent event is too large: run_id=%s bytes=%d", run.ID, len(initialEvent.PayloadJSON))
	}
	transition, err := agentruntime.Terminate(state, retiredAgentRuntimeContractFailureCode)
	if err != nil {
		return fmt.Errorf("retire incompatible queued agent run: run_id=%s: %w", run.ID, err)
	}
	terminalJSON, err := json.Marshal(transition.State)
	if err != nil {
		return err
	}
	if len(terminalJSON) > agentRuntimeMigrationEventPayloadLimit {
		return fmt.Errorf("retired incompatible agent event is too large: run_id=%s bytes=%d", run.ID, len(terminalJSON))
	}
	sequence := run.LastEventSequence + 1
	updated := db.Model(&model.AgentRun{}).
		Where(
			"id = ? AND status = ? AND tool_schema_version = ? AND runtime_version = ? AND policy_version = ? AND state_version = ? AND step_number = ? AND last_event_sequence = ?",
			run.ID, agentruntime.RunQueued, run.ToolSchemaVersion, run.RuntimeVersion, run.PolicyVersion,
			run.StateVersion, run.StepNumber, run.LastEventSequence,
		).
		Select("status", "state_version", "step_number", "last_event_sequence", "updated_at", "completed_at").
		Updates(retiredAgentRunUpdates{
			Status: transition.State.Status, StateVersion: transition.State.StateVersion,
			StepNumber: transition.State.StepNumber, LastEventSequence: sequence,
			UpdatedAt: now, CompletedAt: now,
		})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return fmt.Errorf("retire incompatible queued agent run conflict: run_id=%s", run.ID)
	}
	event := model.AgentRunEvent{
		ID:    agentRuntimeMigrationFactID("event", run.ID, strconv.FormatInt(sequence, 10)),
		RunID: run.ID, Sequence: sequence, Kind: agentruntime.EventRunFailed,
		PayloadJSON: string(terminalJSON), CreatedAt: now,
	}
	if err := db.Create(&event).Error; err != nil {
		return err
	}
	checkpoint = model.AgentCheckpoint{
		ID:    agentRuntimeMigrationFactID("checkpoint", run.ID, strconv.FormatInt(sequence, 10)),
		RunID: run.ID, Sequence: sequence, StateVersion: transition.State.StateVersion,
		StateJSON: string(terminalJSON), CreatedAt: now,
	}
	return db.Create(&checkpoint).Error
}

func verifyAgentRuntimeHasNoExternalFacts(db *gorm.DB, run model.AgentRun) error {
	operation := legacyAgentModelTaskOperationPrefix + run.ID
	expectedTaskID := legacyAgentModelTaskID(run.ID, 0)
	var taskCount int64
	if err := db.Model(&model.Task{}).Where("id = ? OR operation = ?", expectedTaskID, operation).Count(&taskCount).Error; err != nil {
		return fmt.Errorf("count agent runtime model tasks: run_id=%s: %w", run.ID, err)
	}
	billingKeys := []string{
		"agent-runtime:" + run.ID + ":0",
		"proxy-token:agent-runtime:" + run.ID + ":0",
	}
	var billingCount int64
	if err := db.Model(&model.BillingOrder{}).
		Where("user_id = ? AND idempotency_key IN ?", run.ActorUserID, billingKeys).
		Count(&billingCount).Error; err != nil {
		return fmt.Errorf("count agent runtime billing orders: run_id=%s: %w", run.ID, err)
	}
	var toolCallCount int64
	if err := db.Model(&model.AgentToolCall{}).Where("run_id = ?", run.ID).Count(&toolCallCount).Error; err != nil {
		return fmt.Errorf("count agent runtime tool calls: run_id=%s: %w", run.ID, err)
	}
	var planCount int64
	if err := db.Model(&model.AgentProductionPlanVersion{}).Where("created_by_run_id = ?", run.ID).Count(&planCount).Error; err != nil {
		return fmt.Errorf("count agent runtime production plans: run_id=%s: %w", run.ID, err)
	}
	if taskCount != 0 || billingCount != 0 || toolCallCount != 0 || planCount != 0 {
		return fmt.Errorf(
			"queued incompatible agent run has external facts: run_id=%s model_tasks=%d billing_orders=%d tool_calls=%d production_plans=%d",
			run.ID, taskCount, billingCount, toolCallCount, planCount,
		)
	}
	return nil
}

func legacyAgentModelTaskID(runID string, step int) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("agent-runtime-model\x00%s\x00%d", runID, step)))
	return fmt.Sprintf("agt_%x", digest[:16])
}

func retireLegacyAgentRuntimeStateV1(state legacyAgentRuntimeStateV1) (legacyAgentRuntimeStateV1, error) {
	if state.StateVersion != 1 || state.StepNumber != 0 || state.MaxSteps < 1 || state.MaxSteps > 24 ||
		state.Status != agentruntime.RunQueued || strings.TrimSpace(state.UserMessage) == "" || len(state.UserMessage) > 64*1024 ||
		state.ExpectedDelivery != nil || state.Verification != nil || state.PendingToolCall != nil || state.PendingToolStarted ||
		state.LastToolResult != nil || state.FinalMessage != "" || state.FailureCode != "" {
		return legacyAgentRuntimeStateV1{}, errors.New("legacy agent runtime state is not pristine")
	}
	state.StateVersion++
	state.Status = agentruntime.RunFailed
	state.FailureCode = retiredAgentToolSchemaFailureCode
	return state, nil
}

func agentRuntimeMigrationFactID(parts ...string) string {
	digest := sha256.New()
	for _, part := range parts {
		_, _ = digest.Write([]byte(part))
		_, _ = digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func rejectIncompatibleActiveAgentRuns(db *gorm.DB) error {
	type incompatibleRun struct {
		ID                string                 `gorm:"column:id"`
		Status            agentruntime.RunStatus `gorm:"column:status"`
		ToolSchemaVersion int                    `gorm:"column:tool_schema_version"`
		RuntimeVersion    int                    `gorm:"column:runtime_version"`
		PolicyVersion     int                    `gorm:"column:policy_version"`
	}
	activeStatuses := []agentruntime.RunStatus{
		agentruntime.RunQueued,
		agentruntime.RunRunning,
		agentruntime.RunWaitingInput,
		agentruntime.RunWaitingApproval,
		agentruntime.RunWaitingTool,
	}
	var count int64
	if err := db.Table("agent_runs").
		Where(
			"status IN ? AND (tool_schema_version <> ? OR runtime_version <> ? OR policy_version <> ?)",
			activeStatuses,
			agentruntime.CurrentToolSchemaVersion,
			agentruntime.CurrentRuntimeVersion,
			agentruntime.CurrentPolicyVersion,
		).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	const detailLimit = 20
	var runs []incompatibleRun
	result := db.Table("agent_runs").
		Select("id, status, tool_schema_version, runtime_version, policy_version").
		Where(
			"status IN ? AND (tool_schema_version <> ? OR runtime_version <> ? OR policy_version <> ?)",
			activeStatuses,
			agentruntime.CurrentToolSchemaVersion,
			agentruntime.CurrentRuntimeVersion,
			agentruntime.CurrentPolicyVersion,
		).
		Order("created_at, id").
		Limit(detailLimit).
		Scan(&runs)
	if result.Error != nil {
		return result.Error
	}
	details := make([]string, 0, len(runs)+1)
	for _, run := range runs {
		details = append(details, fmt.Sprintf(
			"{run_id=%s status=%s tool_schema_version=%d runtime_version=%d policy_version=%d}",
			run.ID,
			run.Status,
			run.ToolSchemaVersion,
			run.RuntimeVersion,
			run.PolicyVersion,
		))
	}
	if count > int64(len(runs)) {
		details = append(details, fmt.Sprintf("+%d_more", count-int64(len(runs))))
	}
	return fmt.Errorf(
		"active agent runs use incompatible runtime contracts: incompatible_active_runs=%d required=%d required_tool_schema_version=%d required_runtime_version=%d required_policy_version=%d runs=[%s]",
		count,
		agentruntime.CurrentToolSchemaVersion,
		agentruntime.CurrentToolSchemaVersion,
		agentruntime.CurrentRuntimeVersion,
		agentruntime.CurrentPolicyVersion,
		strings.Join(details, ","),
	)
}

func verifyAgentRuntimeIntegrityIndex(db *gorm.DB, specification agentRuntimeIntegrityIndex) (bool, error) {
	if db.Dialector.Name() == "postgres" {
		type indexFacts struct {
			Unique    bool   `gorm:"column:is_unique"`
			TableName string `gorm:"column:table_name"`
			Columns   string `gorm:"column:columns"`
			Predicate string `gorm:"column:predicate"`
		}
		var facts indexFacts
		result := db.Raw(`
			SELECT indexes.indisunique AS is_unique,
			       tables.relname AS table_name,
			       string_agg(attributes.attname, ',' ORDER BY keys.ordinality) AS columns,
			       COALESCE(pg_get_expr(indexes.indpred, indexes.indrelid), '') AS predicate
			FROM pg_class index_names
			JOIN pg_namespace namespaces ON namespaces.oid = index_names.relnamespace
			JOIN pg_index indexes ON indexes.indexrelid = index_names.oid
			JOIN pg_class tables ON tables.oid = indexes.indrelid
			JOIN LATERAL unnest(indexes.indkey) WITH ORDINALITY AS keys(attnum, ordinality) ON keys.attnum > 0
			JOIN pg_attribute attributes ON attributes.attrelid = tables.oid AND attributes.attnum = keys.attnum
			WHERE namespaces.nspname = current_schema() AND index_names.relname = ?
			GROUP BY indexes.indisunique, tables.relname, indexes.indpred, indexes.indrelid`, specification.name).Scan(&facts)
		if result.Error != nil {
			return false, result.Error
		}
		if result.RowsAffected == 0 {
			return false, nil
		}
		if facts.Unique != specification.unique || facts.TableName != specification.table || facts.Columns != specification.columns || canonicalAgentRuntimePredicate(facts.Predicate) != canonicalAgentRuntimePredicate(specification.predicate) {
			return true, fmt.Errorf("agent runtime integrity index %s is invalid: unique=%t table=%s columns=%s predicate=%s", specification.name, facts.Unique, facts.TableName, facts.Columns, facts.Predicate)
		}
		return true, nil
	}

	var definition string
	row := db.Raw("SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?", specification.name).Row()
	if err := row.Scan(&definition); errors.Is(err, sql.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if compactSchemaSQL(definition) != compactSchemaSQL(specification.createSQL) {
		return true, fmt.Errorf("agent runtime integrity index %s is invalid: %s", specification.name, definition)
	}
	return true, nil
}

func canonicalAgentRuntimePredicate(value string) string {
	canonical := strings.ToLower(strings.ReplaceAll(value, "\"", ""))
	for _, cast := range []string{"::character varying", "::text"} {
		canonical = strings.ReplaceAll(canonical, cast, "")
	}
	canonical = strings.ReplaceAll(canonical, " ", "")
	canonical = strings.ReplaceAll(canonical, "\n", "")
	canonical = strings.ReplaceAll(canonical, "\t", "")
	canonical = strings.ReplaceAll(canonical, "(", "")
	canonical = strings.ReplaceAll(canonical, ")", "")
	return canonical
}

func rejectAgentRuntimeIntegrityConflicts(db *gorm.DB) error {
	type duplicate struct {
		First  string `gorm:"column:first_value"`
		Second string `gorm:"column:second_value"`
		Third  string `gorm:"column:third_value"`
		Fourth string `gorm:"column:fourth_value"`
		Fifth  string `gorm:"column:fifth_value"`
		Sixth  string `gorm:"column:sixth_value"`
		Count  int64  `gorm:"column:count"`
	}
	checks := []struct {
		table     string
		selectSQL string
		whereSQL  string
		groupSQL  string
		label     string
	}{
		{"agent_runs", "thread_id AS first_value, client_request_id AS second_value, '' AS third_value, '' AS fourth_value, '' AS fifth_value, '' AS sixth_value, COUNT(*) AS count", "", "thread_id, client_request_id", "agent run request"},
		{"agent_run_events", "run_id AS first_value, CAST(sequence AS TEXT) AS second_value, '' AS third_value, '' AS fourth_value, '' AS fifth_value, '' AS sixth_value, COUNT(*) AS count", "", "run_id, sequence", "agent run event"},
		{"agent_checkpoints", "run_id AS first_value, CAST(sequence AS TEXT) AS second_value, '' AS third_value, '' AS fourth_value, '' AS fifth_value, '' AS sixth_value, COUNT(*) AS count", "", "run_id, sequence", "agent checkpoint"},
		{"agent_timeline_items", "run_id AS first_value, CAST(ordinal AS TEXT) AS second_value, '' AS third_value, '' AS fourth_value, '' AS fifth_value, '' AS sixth_value, COUNT(*) AS count", "", "run_id, ordinal", "agent timeline ordinal"},
		{"agent_timeline_items", "run_id AS first_value, CAST(source_event_sequence AS TEXT) AS second_value, '' AS third_value, '' AS fourth_value, '' AS fifth_value, '' AS sixth_value, COUNT(*) AS count", "", "run_id, source_event_sequence", "agent timeline source event"},
		{"agent_tool_calls", "run_id AS first_value, tool_call_id AS second_value, CAST(action_version AS TEXT) AS third_value, '' AS fourth_value, '' AS fifth_value, '' AS sixth_value, COUNT(*) AS count", "", "run_id, tool_call_id, action_version", "agent tool action"},
		{"agent_production_plan_versions", "tenant_kind AS first_value, tenant_id AS second_value, domain_project_id AS third_value, canvas_id AS fourth_value, plan_key AS fifth_value, CAST(version AS TEXT) AS sixth_value, COUNT(*) AS count", "", "tenant_kind, tenant_id, domain_project_id, canvas_id, plan_key, version", "agent production plan version"},
		{"agent_production_artifacts", "plan_version_id AS first_value, reference_key AS second_value, shot_key AS third_value, kind AS fourth_value, '' AS fifth_value, '' AS sixth_value, COUNT(*) AS count", "", "plan_version_id, reference_key, shot_key, kind", "agent production artifact"},
		{"agent_production_artifacts", "task_id AS first_value, '' AS second_value, '' AS third_value, '' AS fourth_value, '' AS fifth_value, '' AS sixth_value, COUNT(*) AS count", "task_id <> ''", "task_id", "agent production task"},
		{"agent_production_artifacts", "billing_order_id AS first_value, '' AS second_value, '' AS third_value, '' AS fourth_value, '' AS fifth_value, '' AS sixth_value, COUNT(*) AS count", "billing_order_id <> ''", "billing_order_id", "agent production billing order"},
		{"agent_production_artifacts", "resource_id AS first_value, '' AS second_value, '' AS third_value, '' AS fourth_value, '' AS fifth_value, '' AS sixth_value, COUNT(*) AS count", "resource_id <> ''", "resource_id", "agent production resource"},
	}
	for _, check := range checks {
		var conflict duplicate
		query := db.Table(check.table).Select(check.selectSQL)
		if check.whereSQL != "" {
			query = query.Where(check.whereSQL)
		}
		result := query.
			Group(check.groupSQL).
			Having("COUNT(*) > 1").
			Order(check.groupSQL).
			Limit(1).
			Scan(&conflict)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			return fmt.Errorf("%s facts conflict: scope=%s/%s/%s/%s/%s/%s rows=%d", check.label, conflict.First, conflict.Second, conflict.Third, conflict.Fourth, conflict.Fifth, conflict.Sixth, conflict.Count)
		}
	}
	return nil
}
