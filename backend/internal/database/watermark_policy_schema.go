package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

type watermarkPolicyIndex struct {
	name      string
	table     string
	columns   string
	unique    bool
	createSQL string
}

var watermarkPolicyIndexes = []watermarkPolicyIndex{
	{name: "idx_policy_publication_version", table: "policy_publications", columns: "kind,version", unique: true, createSQL: `CREATE UNIQUE INDEX idx_policy_publication_version ON policy_publications(kind, version)`},
	{name: "idx_user_policy_consent", table: "user_policy_consents", columns: "user_id,policy_publication_id", unique: true, createSQL: `CREATE UNIQUE INDEX idx_user_policy_consent ON user_policy_consents(user_id, policy_publication_id)`},
	{name: "idx_user_watermark_preference_events_user_created", table: "user_watermark_preference_events", columns: "user_id,created_at", createSQL: `CREATE INDEX idx_user_watermark_preference_events_user_created ON user_watermark_preference_events(user_id, created_at)`},
}

// EnsureWatermarkPolicyIntegritySchema refuses to trust a same-named index with weaker facts.
func EnsureWatermarkPolicyIntegritySchema(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		missing := make([]watermarkPolicyIndex, 0, len(watermarkPolicyIndexes))
		for _, specification := range watermarkPolicyIndexes {
			exists, err := verifyWatermarkPolicyIndex(tx, specification)
			if err != nil {
				return err
			}
			if !exists {
				missing = append(missing, specification)
			}
		}
		for _, specification := range missing {
			if err := tx.Exec(specification.createSQL).Error; err != nil {
				return fmt.Errorf("创建水印规范完整性索引 %s 失败: %w", specification.name, err)
			}
		}
		return nil
	})
}

func verifyWatermarkPolicyIndex(db *gorm.DB, specification watermarkPolicyIndex) (bool, error) {
	if db.Dialector.Name() == "postgres" {
		type indexFacts struct {
			Unique    bool   `gorm:"column:is_unique"`
			TableName string `gorm:"column:table_name"`
			Columns   string `gorm:"column:columns"`
		}
		var facts indexFacts
		result := db.Raw(`
			SELECT indexes.indisunique AS is_unique,
			       tables.relname AS table_name,
			       string_agg(attributes.attname, ',' ORDER BY keys.ordinality) AS columns
			FROM pg_class index_names
			JOIN pg_namespace namespaces ON namespaces.oid = index_names.relnamespace
			JOIN pg_index indexes ON indexes.indexrelid = index_names.oid
			JOIN pg_class tables ON tables.oid = indexes.indrelid
			JOIN LATERAL unnest(indexes.indkey) WITH ORDINALITY AS keys(attnum, ordinality) ON keys.attnum > 0
			JOIN pg_attribute attributes ON attributes.attrelid = tables.oid AND attributes.attnum = keys.attnum
			WHERE namespaces.nspname = current_schema() AND index_names.relname = ?
			GROUP BY indexes.indisunique, tables.relname`, specification.name).Scan(&facts)
		if result.Error != nil {
			return false, result.Error
		}
		if result.RowsAffected == 0 {
			return false, nil
		}
		if facts.Unique != specification.unique || facts.TableName != specification.table || facts.Columns != specification.columns {
			return true, fmt.Errorf("水印规范完整性索引 %s 定义错误，实际 unique=%t table=%s columns=%s", specification.name, facts.Unique, facts.TableName, facts.Columns)
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
	if canonicalWatermarkIndexSQL(definition) != canonicalWatermarkIndexSQL(specification.createSQL) {
		return true, fmt.Errorf("水印规范完整性索引 %s 定义错误，拒绝信任现有索引: %s", specification.name, definition)
	}
	return true, nil
}

func canonicalWatermarkIndexSQL(value string) string {
	value = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(value, "\"", ""), "`", ""))
	return strings.Join(strings.Fields(value), "")
}
