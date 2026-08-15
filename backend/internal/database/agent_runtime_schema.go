package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

type agentRuntimeIntegrityIndex struct {
	name      string
	table     string
	columns   string
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
		for _, specification := range missing {
			if err := tx.Exec(specification.createSQL).Error; err != nil {
				return fmt.Errorf("create agent runtime integrity index %s: %w", specification.name, err)
			}
		}
		return nil
	})
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
		if facts.Unique != specification.unique || facts.TableName != specification.table || facts.Columns != specification.columns || strings.TrimSpace(facts.Predicate) != "" {
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

func rejectAgentRuntimeIntegrityConflicts(db *gorm.DB) error {
	type duplicate struct {
		First  string `gorm:"column:first_value"`
		Second string `gorm:"column:second_value"`
		Third  string `gorm:"column:third_value"`
		Count  int64  `gorm:"column:count"`
	}
	checks := []struct {
		table     string
		selectSQL string
		groupSQL  string
		label     string
	}{
		{"agent_runs", "thread_id AS first_value, client_request_id AS second_value, '' AS third_value, COUNT(*) AS count", "thread_id, client_request_id", "agent run request"},
		{"agent_run_events", "run_id AS first_value, CAST(sequence AS TEXT) AS second_value, '' AS third_value, COUNT(*) AS count", "run_id, sequence", "agent run event"},
		{"agent_checkpoints", "run_id AS first_value, CAST(sequence AS TEXT) AS second_value, '' AS third_value, COUNT(*) AS count", "run_id, sequence", "agent checkpoint"},
		{"agent_tool_calls", "run_id AS first_value, tool_call_id AS second_value, CAST(action_version AS TEXT) AS third_value, COUNT(*) AS count", "run_id, tool_call_id, action_version", "agent tool action"},
	}
	for _, check := range checks {
		var conflict duplicate
		result := db.Table(check.table).
			Select(check.selectSQL).
			Group(check.groupSQL).
			Having("COUNT(*) > 1").
			Order(check.groupSQL).
			Limit(1).
			Scan(&conflict)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			return fmt.Errorf("%s facts conflict: scope=%s/%s/%s rows=%d", check.label, conflict.First, conflict.Second, conflict.Third, conflict.Count)
		}
	}
	return nil
}
