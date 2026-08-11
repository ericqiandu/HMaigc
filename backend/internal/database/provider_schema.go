package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

var providerRequiredStringFields = []schemaField{
	{table: "channel_models", column: "provider_credential_id"},
	{table: "channel_model_price_tiers", column: "supplier_reference_currency"},
	{table: "billing_orders", column: "pricing_input_variant"},
	{table: "provider_accounts", column: "provider_kind"},
	{table: "provider_accounts", column: "name"},
	{table: "provider_endpoint_versions", column: "provider_account_id"},
	{table: "provider_endpoint_versions", column: "base_url"},
	{table: "provider_endpoint_versions", column: "status"},
	{table: "provider_endpoint_versions", column: "created_by"},
	{table: "provider_credentials", column: "provider_account_id"},
	{table: "provider_credentials", column: "family"},
	{table: "provider_credentials", column: "health_status"},
	{table: "provider_credentials", column: "health_code"},
	{table: "provider_credentials", column: "health_message"},
	{table: "provider_credential_versions", column: "provider_credential_id"},
	{table: "provider_credential_versions", column: "key_cipher"},
	{table: "provider_credential_versions", column: "key_fingerprint"},
	{table: "provider_credential_versions", column: "status"},
	{table: "provider_credential_versions", column: "last_verification_code"},
	{table: "provider_credential_versions", column: "last_verification_trace_id"},
	{table: "provider_credential_versions", column: "last_balance_subunits"},
	{table: "provider_credential_versions", column: "created_by"},
	{table: "provider_task_facts", column: "billing_order_id"},
	{table: "provider_task_facts", column: "provider_account_id"},
	{table: "provider_task_facts", column: "provider_endpoint_version_id"},
	{table: "provider_task_facts", column: "provider_credential_id"},
	{table: "provider_task_facts", column: "provider_credential_version_id"},
	{table: "provider_task_facts", column: "channel_model_id"},
	{table: "provider_task_facts", column: "provider_task_id"},
	{table: "provider_task_facts", column: "create_trace_id"},
	{table: "provider_task_facts", column: "last_poll_trace_id"},
	{table: "provider_task_facts", column: "resolution"},
	{table: "provider_task_facts", column: "input_variant"},
	{table: "provider_task_facts", column: "provider_status"},
	{table: "provider_task_facts", column: "asset_source_url"},
	{table: "provider_task_facts", column: "last_frame_url"},
	{table: "provider_task_facts", column: "total_tokens"},
	{table: "provider_task_facts", column: "reconciliation_status"},
	{table: "provider_billing_facts", column: "provider_task_fact_id"},
	{table: "provider_billing_facts", column: "provider_credential_version_id"},
	{table: "provider_billing_facts", column: "upstream_order_id"},
	{table: "provider_billing_facts", column: "provider_task_id"},
	{table: "provider_billing_facts", column: "amount_subunits"},
	{table: "provider_billing_facts", column: "billing_status"},
	{table: "provider_billing_facts", column: "provider_task_status"},
	{table: "provider_billing_facts", column: "total_tokens"},
	{table: "provider_billing_facts", column: "description"},
	{table: "provider_billing_facts", column: "query_trace_id"},
	{table: "provider_billing_facts", column: "payload_digest"},
}

func prepareLegacyProviderSchema(db *gorm.DB) error {
	if !db.Migrator().HasTable("channel_model_price_tiers") {
		return nil
	}
	const legacyIndex = "idx_channel_model_resolution"
	exists, exact, definition, err := legacyChannelModelResolutionIndex(db, legacyIndex)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if !exact {
		return fmt.Errorf("旧价格规格索引 %s 定义错误，拒绝修改现有索引: %s", legacyIndex, definition)
	}
	return db.Exec("DROP INDEX " + legacyIndex).Error
}

func legacyChannelModelResolutionIndex(db *gorm.DB, name string) (bool, bool, string, error) {
	if db.Dialector.Name() == "postgres" {
		type facts struct {
			Unique    bool   `gorm:"column:is_unique"`
			TableName string `gorm:"column:table_name"`
			Columns   string `gorm:"column:columns"`
			Predicate string `gorm:"column:predicate"`
		}
		var actual facts
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
			GROUP BY indexes.indisunique, tables.relname, indexes.indpred, indexes.indrelid`, name).Scan(&actual)
		if result.Error != nil || result.RowsAffected == 0 {
			return result.RowsAffected > 0, false, "", result.Error
		}
		definition := fmt.Sprintf("unique=%t table=%s columns=%s predicate=%s", actual.Unique, actual.TableName, actual.Columns, actual.Predicate)
		return true, actual.Unique && actual.TableName == "channel_model_price_tiers" && actual.Columns == "channel_model_id,resolution" && strings.TrimSpace(actual.Predicate) == "", definition, nil
	}
	var definition string
	row := db.Raw("SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?", name).Row()
	if err := row.Scan(&definition); errors.Is(err, sql.ErrNoRows) {
		return false, false, "", nil
	} else if err != nil {
		return false, false, "", err
	}
	expected := `CREATE UNIQUE INDEX idx_channel_model_resolution ON channel_model_price_tiers(channel_model_id, resolution)`
	return true, compactSchemaSQL(definition) == compactSchemaSQL(expected), definition, nil
}

func backfillProviderDefaults(db *gorm.DB) error {
	for _, field := range providerRequiredStringFields {
		if !db.Migrator().HasTable(field.table) || !db.Migrator().HasColumn(field.table, field.column) {
			continue
		}
		if err := db.Exec("UPDATE " + field.table + " SET " + field.column + " = '' WHERE " + field.column + " IS NULL").Error; err != nil {
			return fmt.Errorf("回填平台事实字段 %s.%s: %w", field.table, field.column, err)
		}
	}
	if db.Migrator().HasTable("channel_model_price_tiers") && db.Migrator().HasColumn("channel_model_price_tiers", "input_variant") {
		if err := db.Exec("UPDATE channel_model_price_tiers SET input_variant = 'standard' WHERE input_variant IS NULL OR TRIM(input_variant) = ''").Error; err != nil {
			return fmt.Errorf("回填价格输入规格: %w", err)
		}
	}
	return nil
}

type providerIntegrityIndex struct {
	name      string
	table     string
	columns   string
	predicate string
	createSQL string
}

var providerIntegrityIndexes = []providerIntegrityIndex{
	{
		name: "idx_provider_endpoint_active", table: "provider_endpoint_versions", columns: "provider_account_id", predicate: "status = 'active'",
		createSQL: `CREATE UNIQUE INDEX idx_provider_endpoint_active ON provider_endpoint_versions(provider_account_id) WHERE status = 'active'`,
	},
	{
		name: "idx_provider_credential_account_family", table: "provider_credentials", columns: "provider_account_id,family",
		createSQL: `CREATE UNIQUE INDEX idx_provider_credential_account_family ON provider_credentials(provider_account_id, family)`,
	},
	{
		name: "idx_provider_credential_version_active", table: "provider_credential_versions", columns: "provider_credential_id", predicate: "status = 'active'",
		createSQL: `CREATE UNIQUE INDEX idx_provider_credential_version_active ON provider_credential_versions(provider_credential_id) WHERE status = 'active'`,
	},
	{
		name: "idx_provider_task_fact_provider_task", table: "provider_task_facts", columns: "provider_credential_version_id,provider_task_id", predicate: "provider_task_id <> ''",
		createSQL: `CREATE UNIQUE INDEX idx_provider_task_fact_provider_task ON provider_task_facts(provider_credential_version_id, provider_task_id) WHERE provider_task_id <> ''`,
	},
	{
		name: "idx_provider_billing_upstream_order", table: "provider_billing_facts", columns: "provider_credential_version_id,upstream_order_id", predicate: "upstream_order_id <> ''",
		createSQL: `CREATE UNIQUE INDEX idx_provider_billing_upstream_order ON provider_billing_facts(provider_credential_version_id, upstream_order_id) WHERE upstream_order_id <> ''`,
	},
	{
		name: "idx_channel_model_resolution_variant", table: "channel_model_price_tiers", columns: "channel_model_id,resolution,input_variant",
		createSQL: `CREATE UNIQUE INDEX idx_channel_model_resolution_variant ON channel_model_price_tiers(channel_model_id, resolution, input_variant)`,
	},
}

// EnsureProviderIntegritySchema 在创建索引前验证索引定义和历史事实，绝不挑选、覆盖或删除冲突行。
func EnsureProviderIntegritySchema(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		missing := make([]providerIntegrityIndex, 0, len(providerIntegrityIndexes))
		for _, specification := range providerIntegrityIndexes {
			exists, err := verifyProviderIntegrityIndex(tx, specification)
			if err != nil {
				return err
			}
			if !exists {
				missing = append(missing, specification)
			}
		}
		if err := rejectProviderIntegrityConflicts(tx); err != nil {
			return err
		}
		for _, specification := range missing {
			if err := tx.Exec(specification.createSQL).Error; err != nil {
				return fmt.Errorf("创建平台事实完整性索引 %s 失败: %w", specification.name, err)
			}
		}
		return nil
	})
}

func verifyProviderIntegrityIndex(db *gorm.DB, specification providerIntegrityIndex) (bool, error) {
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
		if !facts.Unique || facts.TableName != specification.table || facts.Columns != specification.columns || canonicalProviderPredicate(facts.Predicate) != canonicalProviderPredicate(specification.predicate) {
			return true, fmt.Errorf("平台事实完整性索引 %s 定义错误，实际 table=%s columns=%s predicate=%s", specification.name, facts.TableName, facts.Columns, facts.Predicate)
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
		return true, fmt.Errorf("平台事实完整性索引 %s 定义错误，拒绝信任现有索引: %s", specification.name, definition)
	}
	return true, nil
}

func canonicalProviderPredicate(value string) string {
	canonical := strings.ToLower(strings.ReplaceAll(value, "\"", ""))
	for _, cast := range []string{"::character varying", "::text"} {
		canonical = strings.ReplaceAll(canonical, cast, "")
	}
	canonical = strings.ReplaceAll(canonical, " ", "")
	canonical = strings.ReplaceAll(canonical, "\n", "")
	canonical = strings.ReplaceAll(canonical, "\t", "")
	canonical = strings.TrimPrefix(canonical, "(")
	canonical = strings.TrimSuffix(canonical, ")")
	return canonical
}

func rejectProviderIntegrityConflicts(db *gorm.DB) error {
	type duplicate struct {
		First  string `gorm:"column:first_value"`
		Second string `gorm:"column:second_value"`
		Count  int64  `gorm:"column:count"`
	}
	checks := []struct {
		model     any
		selectSQL string
		whereSQL  string
		groupSQL  string
		label     string
	}{
		{&model.ProviderEndpointVersion{}, "provider_account_id AS first_value, '' AS second_value, COUNT(*) AS count", "status = 'active'", "provider_account_id", "账号活动 endpoint"},
		{&model.ProviderCredential{}, "provider_account_id AS first_value, family AS second_value, COUNT(*) AS count", "", "provider_account_id, family", "账号凭据系列"},
		{&model.ProviderCredentialVersion{}, "provider_credential_id AS first_value, '' AS second_value, COUNT(*) AS count", "status = 'active'", "provider_credential_id", "活动凭据版本"},
		{&model.ProviderTaskFact{}, "provider_credential_version_id AS first_value, provider_task_id AS second_value, COUNT(*) AS count", "provider_task_id <> ''", "provider_credential_version_id, provider_task_id", "上游任务"},
		{&model.ProviderBillingFact{}, "provider_credential_version_id AS first_value, upstream_order_id AS second_value, COUNT(*) AS count", "upstream_order_id <> ''", "provider_credential_version_id, upstream_order_id", "上游账单"},
		{&model.ChannelModelPriceTier{}, "channel_model_id AS first_value, resolution || ':' || input_variant AS second_value, COUNT(*) AS count", "", "channel_model_id, resolution, input_variant", "模型价格规格"},
	}
	for _, check := range checks {
		var conflict duplicate
		query := db.Model(check.model).Select(check.selectSQL)
		if check.whereSQL != "" {
			query = query.Where(check.whereSQL)
		}
		result := query.Group(check.groupSQL).Having("COUNT(*) > 1").Order(check.groupSQL).Limit(1).Scan(&conflict)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			return fmt.Errorf("%s事实冲突: scope=%s/%s rows=%d", check.label, conflict.First, conflict.Second, conflict.Count)
		}
	}
	return nil
}
