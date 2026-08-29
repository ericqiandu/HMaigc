package database

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"

	"gorm.io/gorm"
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
		name: "idx_agent_runs_runtime_retirement", table: "agent_runs", columns: "runtime_version,status,created_at,id",
		createSQL: `CREATE INDEX idx_agent_runs_runtime_retirement ON agent_runs(runtime_version, status, created_at, id)`,
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
		if err := retireIncompatibleAgentRuntimeRuns(tx, time.Now().UTC()); err != nil {
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
	retiredAgentRuntimeContractFailureCode       = agentruntime.FailureRuntimeSchemaRetired
	legacyAgentToolSchemaVersion                 = agentruntime.LegacyToolSchemaVersion
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
