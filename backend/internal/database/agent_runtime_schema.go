package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

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
		name: "idx_agent_run_events_run_sequence", table: "agent_run_events", columns: "run_id,sequence", unique: true,
		createSQL: `CREATE UNIQUE INDEX idx_agent_run_events_run_sequence ON agent_run_events(run_id, sequence)`,
	},
	{
		name: "idx_agent_checkpoints_run_sequence", table: "agent_checkpoints", columns: "run_id,sequence", unique: true,
		createSQL: `CREATE UNIQUE INDEX idx_agent_checkpoints_run_sequence ON agent_checkpoints(run_id, sequence)`,
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
		name: "idx_agent_production_plan_versions_key_version", table: "agent_production_plan_versions", columns: "plan_key,version", unique: true,
		createSQL: `CREATE UNIQUE INDEX idx_agent_production_plan_versions_key_version ON agent_production_plan_versions(plan_key, version)`,
	},
	{
		name: "idx_agent_production_artifacts_plan_shot_kind", table: "agent_production_artifacts", columns: "plan_key,plan_version,shot_key,kind", unique: true,
		createSQL: `CREATE UNIQUE INDEX idx_agent_production_artifacts_plan_shot_kind ON agent_production_artifacts(plan_key, plan_version, shot_key, kind)`,
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
		if err := rejectIncompatibleActiveAgentRuns(tx); err != nil {
			return err
		}
		for _, specification := range missing {
			if err := tx.Exec(specification.createSQL).Error; err != nil {
				return fmt.Errorf("create agent runtime integrity index %s: %w", specification.name, err)
			}
		}
		return nil
	})
}

func rejectIncompatibleActiveAgentRuns(db *gorm.DB) error {
	type incompatibleRun struct {
		ID                string                 `gorm:"column:id"`
		Status            agentruntime.RunStatus `gorm:"column:status"`
		ToolSchemaVersion int                    `gorm:"column:tool_schema_version"`
	}
	var run incompatibleRun
	result := db.Table("agent_runs").
		Select("id, status, tool_schema_version").
		Where("status IN ? AND tool_schema_version <> ?", []agentruntime.RunStatus{
			agentruntime.RunQueued,
			agentruntime.RunRunning,
			agentruntime.RunWaitingApproval,
			agentruntime.RunWaitingTool,
		}, agentruntime.CurrentToolSchemaVersion).
		Order("created_at, id").
		Limit(1).
		Scan(&run)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return nil
	}
	return fmt.Errorf(
		"active agent run uses incompatible tool schema: run_id=%s status=%s tool_schema_version=%d required=%d",
		run.ID,
		run.Status,
		run.ToolSchemaVersion,
		agentruntime.CurrentToolSchemaVersion,
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
		Count  int64  `gorm:"column:count"`
	}
	checks := []struct {
		table     string
		selectSQL string
		whereSQL  string
		groupSQL  string
		label     string
	}{
		{"agent_runs", "thread_id AS first_value, client_request_id AS second_value, '' AS third_value, '' AS fourth_value, COUNT(*) AS count", "", "thread_id, client_request_id", "agent run request"},
		{"agent_run_events", "run_id AS first_value, CAST(sequence AS TEXT) AS second_value, '' AS third_value, '' AS fourth_value, COUNT(*) AS count", "", "run_id, sequence", "agent run event"},
		{"agent_checkpoints", "run_id AS first_value, CAST(sequence AS TEXT) AS second_value, '' AS third_value, '' AS fourth_value, COUNT(*) AS count", "", "run_id, sequence", "agent checkpoint"},
		{"agent_tool_calls", "run_id AS first_value, tool_call_id AS second_value, CAST(action_version AS TEXT) AS third_value, '' AS fourth_value, COUNT(*) AS count", "", "run_id, tool_call_id, action_version", "agent tool action"},
		{"agent_production_plan_versions", "plan_key AS first_value, CAST(version AS TEXT) AS second_value, '' AS third_value, '' AS fourth_value, COUNT(*) AS count", "", "plan_key, version", "agent production plan version"},
		{"agent_production_artifacts", "plan_key AS first_value, CAST(plan_version AS TEXT) AS second_value, shot_key AS third_value, kind AS fourth_value, COUNT(*) AS count", "", "plan_key, plan_version, shot_key, kind", "agent production artifact"},
		{"agent_production_artifacts", "task_id AS first_value, '' AS second_value, '' AS third_value, '' AS fourth_value, COUNT(*) AS count", "task_id <> ''", "task_id", "agent production task"},
		{"agent_production_artifacts", "billing_order_id AS first_value, '' AS second_value, '' AS third_value, '' AS fourth_value, COUNT(*) AS count", "billing_order_id <> ''", "billing_order_id", "agent production billing order"},
		{"agent_production_artifacts", "resource_id AS first_value, '' AS second_value, '' AS third_value, '' AS fourth_value, COUNT(*) AS count", "resource_id <> ''", "resource_id", "agent production resource"},
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
			return fmt.Errorf("%s facts conflict: scope=%s/%s/%s/%s rows=%d", check.label, conflict.First, conflict.Second, conflict.Third, conflict.Fourth, conflict.Count)
		}
	}
	return nil
}
