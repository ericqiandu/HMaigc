package database

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Models 是应用持久化表的唯一清单，服务启动和跨数据库迁移必须共用它。
func Models() []any {
	return []any{
		&model.User{},
		&model.AuthSession{},
		&model.UserIdentity{},
		&model.OAuthState{},
		&model.EmailVerificationCode{},
		&model.ReferralProfile{},
		&model.ReferralRelationship{},
		&model.ReferralRewardRule{},
		&model.ReferralReward{},
		&model.ModelChannel{},
		&model.ProviderAccount{},
		&model.ProviderEndpointVersion{},
		&model.ProviderCredential{},
		&model.ProviderCredentialVersion{},
		&model.ChannelModel{},
		&model.ChannelVoice{},
		&model.UserVoiceFavorite{},
		&model.ChannelVoicePreview{},
		&model.ChannelModelPriceTier{},
		&model.ApiCallLog{},
		&model.ModelPricing{},
		&model.ModelPricingTier{},
		&model.CreditAccount{},
		&model.CreditLedgerEntry{},
		&model.BillingOrder{},
		&model.ProviderTaskFact{},
		&model.ProviderBillingFact{},
		&model.SuperResolutionPricingRule{},
		&model.MembershipPlan{},
		&model.Team{},
		&model.TeamMember{},
		&model.TeamInvitation{},
		&model.TeamAuditEvent{},
		&model.TeamCreditAccount{},
		&model.TeamCreditLedgerEntry{},
		&model.MembershipOrder{},
		&model.CreditTopupProduct{},
		&model.CreditTopupOrder{},
		&model.MembershipSubscription{},
		&model.InvoiceRequest{},
		&model.PaymentCheckoutSession{},
		&model.PaymentTransaction{},
		&model.PaymentWebhookEvent{},
		&model.RedeemBatch{},
		&model.RedeemCode{},
		&model.AdminAuditEvent{},
		&model.UserDailyActivity{},
		&model.SystemSetting{},
		&model.UserOSSSetting{},
		&model.UserDailyUploadUsage{},
		&model.UserSkillState{},
		&model.Resource{},
		&model.StorageMigrationJob{},
		&model.StorageMigrationItem{},
		&model.Asset{},
		&model.ProjectAssetLink{},
		&model.ProjectAssetCandidate{},
		&model.AssetVersion{},
		&model.AssetRepresentation{},
		&model.VoiceProfile{},
		&model.CharacterVoiceBinding{},
		&model.Project{},
		&model.ProjectCollaborator{},
		&model.ProjectUnit{},
		&model.CanvasUnitLink{},
		&model.Shot{},
		&model.ShotAssetReference{},
		&model.WorkflowTemplateVersion{},
		&model.WorkflowInstance{},
		&model.WorkflowStepInstance{},
		&model.WorkflowStepTask{},
		&model.CanvasProject{},
		&model.CanvasCollaborator{},
		&model.CanvasChange{},
		&model.CanvasShare{},
		&model.StoryboardPromptTemplate{},
		&model.Announcement{},
		&model.UserAnnouncementRead{},
		&model.Task{},
		&model.Session{},
		&model.Message{},
		&model.TaskLog{},
		&model.SessionFile{},
		&model.Result{},
	}
}

func MigrateSchema(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		legacyModelIconColumns := tx.Migrator().HasColumn(&model.ChannelModel{}, "icon_file") || tx.Migrator().HasColumn(&model.ChannelModel{}, "icon_mime_type")
		if err := prepareLegacyPaymentNulls(tx); err != nil {
			return err
		}
		if err := prepareLegacyProviderSchema(tx); err != nil {
			return err
		}
		if err := MigrateBaseSchema(tx); err != nil {
			return err
		}
		if err := backfillProviderDefaults(tx); err != nil {
			return err
		}
		if err := migrateChannelModelBrands(tx, legacyModelIconColumns); err != nil {
			return err
		}
		// 逻辑删除后的同名模型允许重新添加，旧唯一索引不能继续覆盖已删除记录。
		for _, indexName := range []string{"idx_channel_model_key", "idx_channel_voices_idempotency_key", "idx_channel_voice_idempotency", "idx_users_email"} {
			if err := tx.Exec("DROP INDEX IF EXISTS " + indexName).Error; err != nil {
				return err
			}
		}
		// 价格策略字段为显式必填；将迁移前遗留的 NULL、空串和纯空格统一单价记录一次性标记为 flat。
		if err := tx.Model(&model.ChannelModel{}).
			Where("price_strategy IS NULL OR TRIM(price_strategy) = ''").
			Update("price_strategy", "flat").Error; err != nil {
			return err
		}
		const teamStorage130TB int64 = 130 * (1 << 40)
		type teamCommercialDefaults struct {
			UnlimitedTaskQueue        bool  `gorm:"column:unlimited_task_queue"`
			TeamStorageBytes          int64 `gorm:"column:team_storage_bytes"`
			SharedAssetsEnabled       bool  `gorm:"column:shared_assets_enabled"`
			ProjectPermissionsEnabled bool  `gorm:"column:project_permissions_enabled"`
			InvoicingEnabled          bool  `gorm:"column:invoicing_enabled"`
			CommercialUseEnabled      bool  `gorm:"column:commercial_use_enabled"`
		}
		if err := tx.Model(&model.MembershipPlan{}).
			Where("audience = ? AND (team_storage_bytes IS NULL OR team_storage_bytes = 0)", model.MembershipAudienceTeam).
			Select("unlimited_task_queue", "team_storage_bytes", "shared_assets_enabled", "project_permissions_enabled", "invoicing_enabled", "commercial_use_enabled").
			Updates(teamCommercialDefaults{
				UnlimitedTaskQueue: true, TeamStorageBytes: teamStorage130TB,
				SharedAssetsEnabled: true, ProjectPermissionsEnabled: true,
				InvoicingEnabled: true, CommercialUseEnabled: true,
			}).Error; err != nil {
			return err
		}
		if err := backfillLegacyTeamCommercialSnapshots(tx); err != nil {
			return err
		}
		if err := seedSuperResolutionPricingRules(tx); err != nil {
			return err
		}
		if err := seedMiniMaxH3SupplierPricing(tx); err != nil {
			return err
		}
		if err := seedMiniMaxAudioSupplierPricing(tx); err != nil {
			return err
		}
		// 超分已切换为独立服务定价，清除旧模型价格中的 SR_* 双轨数据。
		if err := tx.Where("resolution LIKE ?", "SR_%").Delete(&model.ChannelModelPriceTier{}).Error; err != nil {
			return err
		}
		if err := tx.Where("specification LIKE ?", "SR_%").Delete(&model.ModelPricingTier{}).Error; err != nil {
			return err
		}
		if err := tx.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_nonempty ON users(lower(email)) WHERE email <> ''").Error; err != nil {
			return err
		}
		if err := EnsurePaymentIntegritySchema(tx); err != nil {
			return err
		}
		return EnsureProviderIntegritySchema(tx)
	})
}

type schemaField struct {
	table  string
	column string
}

var paymentRequiredStringFields = []schemaField{
	{table: "membership_orders", column: "idempotency_key"},
	{table: "membership_orders", column: "request_hash"},
	{table: "payment_checkout_sessions", column: "token_cipher"},
	{table: "payment_transactions", column: "failure_code"},
	{table: "payment_webhook_events", column: "merchant_order_no"},
	{table: "payment_webhook_events", column: "provider_trade_no"},
	{table: "payment_webhook_events", column: "currency"},
	{table: "payment_webhook_events", column: "failure_code"},
}

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

// MigrateBaseSchema 只建立可承载历史数据的表、列和默认值；数据复制后再施加支付唯一性约束。
func MigrateBaseSchema(db *gorm.DB) error {
	if err := db.AutoMigrate(Models()...); err != nil {
		return err
	}
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	for _, field := range paymentRequiredStringFields {
		if err := db.Exec(`ALTER TABLE "` + field.table + `" ALTER COLUMN "` + field.column + `" SET DEFAULT ''`).Error; err != nil {
			return fmt.Errorf("设置支付字段默认值 %s.%s: %w", field.table, field.column, err)
		}
	}
	for _, field := range providerRequiredStringFields {
		if err := db.Exec(`ALTER TABLE "` + field.table + `" ALTER COLUMN "` + field.column + `" SET DEFAULT ''`).Error; err != nil {
			return fmt.Errorf("设置平台事实字段默认值 %s.%s: %w", field.table, field.column, err)
		}
	}
	if err := db.Exec(`ALTER TABLE "channel_model_price_tiers" ALTER COLUMN "input_variant" SET DEFAULT 'standard'`).Error; err != nil {
		return fmt.Errorf("设置价格规格默认值: %w", err)
	}
	return nil
}

// prepareLegacyPaymentNulls 是运行时旧库的数据准备阶段，不属于跨库复制前的基础结构创建。
func prepareLegacyPaymentNulls(db *gorm.DB) error {
	for _, field := range paymentRequiredStringFields {
		if !db.Migrator().HasTable(field.table) || !db.Migrator().HasColumn(field.table, field.column) {
			continue
		}
		if err := db.Exec("UPDATE " + field.table + " SET " + field.column + " = '' WHERE " + field.column + " IS NULL").Error; err != nil {
			return fmt.Errorf("准备旧支付字段 %s.%s: %w", field.table, field.column, err)
		}
	}
	return nil
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

type paymentIntegrityIndex struct {
	name      string
	table     string
	columns   string
	predicate string
	createSQL string
}

var paymentIntegrityIndexes = []paymentIntegrityIndex{
	{
		name: "idx_membership_order_user_idempotency", table: "membership_orders", columns: "user_id,idempotency_key", predicate: "idempotency_key <> ''",
		createSQL: `CREATE UNIQUE INDEX idx_membership_order_user_idempotency ON membership_orders(user_id, idempotency_key) WHERE idempotency_key <> ''`,
	},
	{
		name: "idx_payment_transactions_payable_order", table: "payment_transactions", columns: "order_type,order_id", predicate: "status IN ('created', 'pending', 'review_required')",
		createSQL: `CREATE UNIQUE INDEX idx_payment_transactions_payable_order ON payment_transactions(order_type, order_id) WHERE status IN ('created', 'pending', 'review_required')`,
	},
	{
		name: "idx_payment_transactions_provider_trade", table: "payment_transactions", columns: "provider,provider_trade_no", predicate: "provider_trade_no <> ''",
		createSQL: `CREATE UNIQUE INDEX idx_payment_transactions_provider_trade ON payment_transactions(provider, provider_trade_no) WHERE provider_trade_no <> ''`,
	},
}

// EnsurePaymentIntegritySchema 先验证历史事实，再创建支付部分唯一索引；冲突数据不会被挑选、覆盖或删除。
func EnsurePaymentIntegritySchema(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		missing := make([]paymentIntegrityIndex, 0, len(paymentIntegrityIndexes))
		for _, specification := range paymentIntegrityIndexes {
			exists, err := verifyPaymentIntegrityIndex(tx, specification)
			if err != nil {
				return err
			}
			if !exists {
				missing = append(missing, specification)
			}
		}
		if err := backfillProcessedPaymentWebhookFacts(tx); err != nil {
			return err
		}
		if err := rejectDuplicateMembershipOrderIdempotency(tx); err != nil {
			return err
		}
		if err := rejectDuplicatePayableTransactions(tx); err != nil {
			return err
		}
		if err := rejectDuplicateProviderTrades(tx); err != nil {
			return err
		}
		for _, specification := range missing {
			if err := tx.Exec(specification.createSQL).Error; err != nil {
				return fmt.Errorf("创建支付完整性索引 %s 失败: %w", specification.name, err)
			}
		}
		return nil
	})
}

func verifyPaymentIntegrityIndex(db *gorm.DB, specification paymentIntegrityIndex) (bool, error) {
	if db.Dialector.Name() == "postgres" {
		return verifyPostgresPaymentIntegrityIndex(db, specification)
	}
	var definition string
	row := db.Raw("SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?", specification.name).Row()
	if err := row.Scan(&definition); errors.Is(err, sql.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if compactSchemaSQL(definition) != compactSchemaSQL(specification.createSQL) {
		return true, fmt.Errorf("支付完整性索引 %s 定义错误，拒绝信任现有索引: %s", specification.name, definition)
	}
	return true, nil
}

func verifyPostgresPaymentIntegrityIndex(db *gorm.DB, specification paymentIntegrityIndex) (bool, error) {
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
		       pg_get_expr(indexes.indpred, indexes.indrelid) AS predicate
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
	if !facts.Unique || facts.TableName != specification.table || facts.Columns != specification.columns || !paymentPredicateMatches(specification.predicate, facts.Predicate) {
		return true, fmt.Errorf("支付完整性索引 %s 定义错误，实际 table=%s columns=%s predicate=%s", specification.name, facts.TableName, facts.Columns, facts.Predicate)
	}
	return true, nil
}

func compactSchemaSQL(value string) string {
	value = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(value, "\"", ""), "`", ""))
	return strings.Join(strings.Fields(value), " ")
}

func paymentPredicateMatches(expected string, actual string) bool {
	canonical := canonicalPaymentPredicate(actual)
	switch expected {
	case "idempotency_key <> ''":
		return canonical == "idempotency_key<>''"
	case "provider_trade_no <> ''":
		return canonical == "provider_trade_no<>''"
	case "status IN ('created', 'pending', 'review_required')":
		return canonical == "statusin'created','pending','review_required'" || canonical == "status=anyarray['created','pending','review_required']"
	default:
		return false
	}
}

func canonicalPaymentPredicate(value string) string {
	canonical := strings.ToLower(strings.ReplaceAll(value, "\"", ""))
	for _, cast := range []string{"::character varying[]", "::character varying", "::text[]", "::text"} {
		canonical = strings.ReplaceAll(canonical, cast, "")
	}
	canonical = strings.ReplaceAll(canonical, " ", "")
	canonical = strings.ReplaceAll(canonical, "\n", "")
	canonical = strings.ReplaceAll(canonical, "\t", "")
	canonical = strings.ReplaceAll(canonical, "(", "")
	canonical = strings.ReplaceAll(canonical, ")", "")
	return canonical
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

func backfillProcessedPaymentWebhookFacts(db *gorm.DB) error {
	type factUpdate struct {
		MerchantOrderNo string     `gorm:"column:merchant_order_no"`
		ProviderTradeNo string     `gorm:"column:provider_trade_no"`
		AmountCents     int64      `gorm:"column:amount_cents"`
		Currency        string     `gorm:"column:currency"`
		PaidAt          *time.Time `gorm:"column:paid_at"`
	}
	var events []model.PaymentWebhookEvent
	if err := db.Where("status = ?", model.PaymentWebhookProcessed).Order("id asc").Find(&events).Error; err != nil {
		return err
	}
	for index := range events {
		event := &events[index]
		if strings.TrimSpace(event.TransactionID) == "" {
			return fmt.Errorf("已处理支付回调 %s 缺少 transaction_id，无法回填商户事实", event.ID)
		}
		var transaction model.PaymentTransaction
		if err := db.First(&transaction, "id = ?", event.TransactionID).Error; err != nil {
			return fmt.Errorf("已处理支付回调 %s 找不到交易 %s，无法回填商户事实: %w", event.ID, event.TransactionID, err)
		}
		normalized, changed, err := NormalizeProcessedPaymentWebhookFacts(*event, transaction)
		if err != nil {
			return err
		}
		if !changed {
			continue
		}
		if err := db.Model(event).
			Select("merchant_order_no", "provider_trade_no", "amount_cents", "currency", "paid_at").
			Updates(factUpdate{
				MerchantOrderNo: normalized.MerchantOrderNo,
				ProviderTradeNo: normalized.ProviderTradeNo,
				AmountCents:     normalized.AmountCents,
				Currency:        normalized.Currency,
				PaidAt:          normalized.PaidAt,
			}).Error; err != nil {
			return err
		}
	}
	return nil
}

// NormalizeProcessedPaymentWebhookFacts 用交易不可变事实规范化已处理回调，供运行时迁移与跨库复制共用。
func NormalizeProcessedPaymentWebhookFacts(event model.PaymentWebhookEvent, transaction model.PaymentTransaction) (model.PaymentWebhookEvent, bool, error) {
	if event.Status != model.PaymentWebhookProcessed {
		return event, false, nil
	}
	if strings.TrimSpace(event.TransactionID) == "" {
		return event, false, fmt.Errorf("已处理支付回调 %s 缺少 transaction_id，无法回填商户事实", event.ID)
	}
	if transaction.ID != event.TransactionID || event.Provider != transaction.Provider || strings.TrimSpace(transaction.MerchantOrderNo) == "" || strings.TrimSpace(transaction.ProviderTradeNo) == "" || transaction.AmountCents <= 0 || strings.TrimSpace(transaction.Currency) == "" || transaction.PaidAt == nil {
		return event, false, fmt.Errorf("已处理支付回调 %s 的交易 %s 缺少确定事实: order=%s/%s merchant=%s provider=%s trade=%s amount=%d currency=%s paid_at=%v", event.ID, transaction.ID, transaction.OrderType, transaction.OrderID, transaction.MerchantOrderNo, transaction.Provider, transaction.ProviderTradeNo, transaction.AmountCents, transaction.Currency, transaction.PaidAt)
	}
	if (event.MerchantOrderNo != "" && event.MerchantOrderNo != transaction.MerchantOrderNo) ||
		(event.ProviderTradeNo != "" && event.ProviderTradeNo != transaction.ProviderTradeNo) ||
		(event.AmountCents != 0 && event.AmountCents != transaction.AmountCents) ||
		(event.Currency != "" && event.Currency != transaction.Currency) ||
		(event.PaidAt != nil && !event.PaidAt.Equal(*transaction.PaidAt)) {
		return event, false, fmt.Errorf("已处理支付回调 %s 与交易 %s 的确定事实冲突: order=%s/%s merchant=%s trade=%s", event.ID, transaction.ID, transaction.OrderType, transaction.OrderID, transaction.MerchantOrderNo, transaction.ProviderTradeNo)
	}
	changed := event.MerchantOrderNo != transaction.MerchantOrderNo ||
		event.ProviderTradeNo != transaction.ProviderTradeNo ||
		event.AmountCents != transaction.AmountCents ||
		event.Currency != transaction.Currency ||
		event.PaidAt == nil
	event.MerchantOrderNo = transaction.MerchantOrderNo
	event.ProviderTradeNo = transaction.ProviderTradeNo
	event.AmountCents = transaction.AmountCents
	event.Currency = transaction.Currency
	event.PaidAt = transaction.PaidAt
	return event, changed, nil
}

func rejectDuplicateMembershipOrderIdempotency(db *gorm.DB) error {
	type duplicate struct {
		UserID         string `gorm:"column:user_id"`
		IdempotencyKey string `gorm:"column:idempotency_key"`
		Count          int64  `gorm:"column:count"`
	}
	var conflict duplicate
	result := db.Model(&model.MembershipOrder{}).
		Select("user_id, idempotency_key, COUNT(*) AS count").
		Where("idempotency_key <> ''").
		Group("user_id, idempotency_key").Having("COUNT(*) > 1").Order("user_id asc, idempotency_key asc").Limit(1).Scan(&conflict)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return fmt.Errorf("会员订单幂等事实冲突: user=%s key=%s rows=%d", conflict.UserID, conflict.IdempotencyKey, conflict.Count)
	}
	return nil
}

func rejectDuplicatePayableTransactions(db *gorm.DB) error {
	type duplicate struct {
		OrderType model.PaymentOrderType `gorm:"column:order_type"`
		OrderID   string                 `gorm:"column:order_id"`
	}
	var conflict duplicate
	result := db.Model(&model.PaymentTransaction{}).
		Select("order_type, order_id").
		Where("status IN ?", []model.PaymentTransactionStatus{model.PaymentTransactionCreated, model.PaymentTransactionPending, model.PaymentTransactionReviewRequired}).
		Group("order_type, order_id").Having("COUNT(*) > 1").Order("order_type asc, order_id asc").Limit(1).Scan(&conflict)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return nil
	}
	facts, err := paymentTransactionConflictFacts(db.Where("order_type = ? AND order_id = ? AND status IN ?", conflict.OrderType, conflict.OrderID, []model.PaymentTransactionStatus{model.PaymentTransactionCreated, model.PaymentTransactionPending, model.PaymentTransactionReviewRequired}))
	if err != nil {
		return err
	}
	return fmt.Errorf("可支付交易事实冲突: order=%s/%s facts=[%s]", conflict.OrderType, conflict.OrderID, facts)
}

func rejectDuplicateProviderTrades(db *gorm.DB) error {
	type duplicate struct {
		Provider        model.PaymentProvider `gorm:"column:provider"`
		ProviderTradeNo string                `gorm:"column:provider_trade_no"`
	}
	var conflict duplicate
	result := db.Model(&model.PaymentTransaction{}).
		Select("provider, provider_trade_no").Where("provider_trade_no <> ''").
		Group("provider, provider_trade_no").Having("COUNT(*) > 1").Order("provider asc, provider_trade_no asc").Limit(1).Scan(&conflict)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return nil
	}
	facts, err := paymentTransactionConflictFacts(db.Where("provider = ? AND provider_trade_no = ?", conflict.Provider, conflict.ProviderTradeNo))
	if err != nil {
		return err
	}
	return fmt.Errorf("渠道交易号事实冲突: provider=%s trade=%s facts=[%s]", conflict.Provider, conflict.ProviderTradeNo, facts)
}

func paymentTransactionConflictFacts(query *gorm.DB) (string, error) {
	var transactions []model.PaymentTransaction
	if err := query.Order("id asc").Find(&transactions).Error; err != nil {
		return "", err
	}
	facts := make([]string, 0, len(transactions))
	for _, transaction := range transactions {
		facts = append(facts, fmt.Sprintf("id=%s order=%s/%s merchant=%s provider=%s trade=%s status=%s", transaction.ID, transaction.OrderType, transaction.OrderID, transaction.MerchantOrderNo, transaction.Provider, transaction.ProviderTradeNo, transaction.Status))
	}
	return strings.Join(facts, "; "), nil
}

func seedMiniMaxH3SupplierPricing(db *gorm.DB) error {
	const pricingID = "model-pricing-minimax-h3-default"
	pricing := model.ModelPricing{}
	err := db.Where("channel_id = ? AND model = ? AND capability = ?", "", "MiniMax-H3", "video").First(&pricing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		pricing = model.ModelPricing{ID: pricingID, ChannelID: "", Model: "MiniMax-H3", Capability: "video", Currency: "CNY"}
		if err := db.Create(&pricing).Error; err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	tiers := []model.ModelPricingTier{
		{ID: "minimax-h3-cost-generated-768p", ModelPricingID: pricing.ID, Specification: "768P", SupplierCostMicros: 500_000},
		{ID: "minimax-h3-cost-generated-2k", ModelPricingID: pricing.ID, Specification: "2K", SupplierCostMicros: 800_000},
		{ID: "minimax-h3-cost-input-image", ModelPricingID: pricing.ID, Specification: "INPUT_IMAGE_OVERAGE", SupplierCostMicros: 200_000},
		{ID: "minimax-h3-cost-input-video-768p", ModelPricingID: pricing.ID, Specification: "INPUT_VIDEO_768P", SupplierCostMicros: 500_000},
		{ID: "minimax-h3-cost-input-video-2k", ModelPricingID: pricing.ID, Specification: "INPUT_VIDEO_2K", SupplierCostMicros: 800_000},
		{ID: "minimax-h3-cost-regenerate-2k", ModelPricingID: pricing.ID, Specification: "REGENERATE_768P_TO_2K", SupplierCostMicros: 300_000},
		{ID: "minimax-h3-regen-input-image", ModelPricingID: pricing.ID, Specification: "REGENERATE_INPUT_IMAGE_OVERAGE", SupplierCostMicros: 150_000},
		{ID: "minimax-h3-regen-input-video", ModelPricingID: pricing.ID, Specification: "REGENERATE_INPUT_VIDEO_768P", SupplierCostMicros: 300_000},
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&tiers).Error
}

func seedMiniMaxAudioSupplierPricing(db *gorm.DB) error {
	type audioPricingSeed struct {
		pricingID, tierID, model, specification string
		costMicros                              int64
	}
	seeds := []audioPricingSeed{
		{pricingID: "pricing-minimax-speech-hd", tierID: "cost-minimax-speech-hd-chars", model: "speech-2.8-hd", specification: "TEN_THOUSAND_CHARACTERS", costMicros: 3_500_000},
		{pricingID: "pricing-minimax-speech-turbo", tierID: "cost-minimax-speech-turbo-chars", model: "speech-2.8-turbo", specification: "TEN_THOUSAND_CHARACTERS", costMicros: 2_000_000},
		{pricingID: "pricing-minimax-voice-clone", tierID: "cost-minimax-voice-design-clone", model: "MiniMax-Voice-Cloning", specification: "VOICE_DESIGN_OR_CLONE", costMicros: 9_900_000},
	}
	for _, seed := range seeds {
		pricing := model.ModelPricing{}
		err := db.Where("channel_id = ? AND model = ? AND capability = ?", "", seed.model, "audio").First(&pricing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			pricing = model.ModelPricing{ID: seed.pricingID, ChannelID: "", Model: seed.model, Capability: "audio", Currency: "CNY"}
			if err := db.Create(&pricing).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		tier := model.ModelPricingTier{ID: seed.tierID, ModelPricingID: pricing.ID, Specification: seed.specification, SupplierCostMicros: seed.costMicros}
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&tier).Error; err != nil {
			return err
		}
	}
	return nil
}

func seedSuperResolutionPricingRules(db *gorm.DB) error {
	var count int64
	if err := db.Model(&model.SuperResolutionPricingRule{}).Count(&count).Error; err != nil || count > 0 {
		return err
	}
	type seed struct {
		edition, resolution string
		min, max            int
		cost                float64
	}
	seeds := []seed{
		{"standard", "720P", 0, 30, .02}, {"standard", "720P", 30, 60, .03}, {"standard", "720P", 60, 120, .05},
		{"standard", "1080P", 0, 30, .03}, {"standard", "1080P", 30, 60, .05}, {"standard", "1080P", 60, 120, .10},
		{"standard", "2K", 0, 30, .05}, {"standard", "2K", 30, 60, .10}, {"standard", "2K", 60, 120, .20},
		{"standard", "4K", 0, 30, .10}, {"standard", "4K", 30, 60, .20}, {"standard", "4K", 60, 120, .40},
		{"standard", "8K", 0, 30, .40}, {"standard", "8K", 30, 60, .80}, {"standard", "8K", 60, 120, 1.60},
		{"professional", "720P", 0, 30, .13}, {"professional", "720P", 30, 60, .25}, {"professional", "720P", 60, 120, .50},
		{"professional", "1080P", 0, 30, .25}, {"professional", "1080P", 30, 60, .50}, {"professional", "1080P", 60, 120, 1.00},
		{"professional", "2K", 0, 30, .50}, {"professional", "2K", 30, 60, 1.00}, {"professional", "2K", 60, 120, 2.00},
		{"professional", "4K", 0, 30, 1.00}, {"professional", "4K", 30, 60, 2.00}, {"professional", "4K", 60, 120, 4.00},
		{"professional", "8K", 0, 30, 4.00}, {"professional", "8K", 30, 60, 8.00}, {"professional", "8K", 60, 120, 16.00},
	}
	rules := make([]model.SuperResolutionPricingRule, 0, len(seeds))
	for index, item := range seeds {
		rules = append(rules, model.SuperResolutionPricingRule{
			ID: fmt.Sprintf("super-resolution-seed-%02d", index+1), Edition: item.edition, Resolution: item.resolution,
			FPSMinExclusive: item.min, FPSMaxInclusive: item.max, Currency: "CNY",
			SupplierCostMinMicros: int64(item.cost*1_000_000 + .5), SupplierCostMaxMicros: int64(item.cost*1_000_000 + .5),
			PriceConfigured: false, Enabled: true, PriceVersion: 1,
		})
	}
	return db.Create(&rules).Error
}

func migrateChannelModelBrands(db *gorm.DB, legacyModelIconColumns bool) error {
	if !legacyModelIconColumns {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var models []model.ChannelModel
		if err := tx.Unscoped().Find(&models).Error; err != nil {
			return err
		}
		for index := range models {
			if err := tx.Unscoped().Table("channel_models").Where("id = ?", models[index].ID).UpdateColumn("brand_key", model.InferModelBrandKey(models[index].ModelKey)).Error; err != nil {
				return err
			}
		}
		for _, column := range []string{"icon_file", "icon_mime_type"} {
			if tx.Migrator().HasColumn("channel_models", column) {
				if err := tx.Exec("ALTER TABLE channel_models DROP COLUMN " + column).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// backfillLegacyTeamCommercialSnapshots 只迁移新增商业权益字段为空的旧团队快照。
// 历史价格、积分、并发和席位仍保留购买时事实；新增权益按同一 plan_id 当前已授予能力补齐。
func backfillLegacyTeamCommercialSnapshots(db *gorm.DB) error {
	var plans []model.MembershipPlan
	if err := db.Where("audience = ?", model.MembershipAudienceTeam).Find(&plans).Error; err != nil {
		return err
	}
	planByID := make(map[string]model.MembershipPlan, len(plans))
	for _, plan := range plans {
		planByID[plan.ID] = plan
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var subscriptions []model.MembershipSubscription
		if err := tx.Where("team_id <> ''").Find(&subscriptions).Error; err != nil {
			return err
		}
		for index := range subscriptions {
			subscription := &subscriptions[index]
			updated, snapshot, err := commercialSnapshotBackfill(subscription.PlanID, subscription.PlanSnapshotJSON, planByID)
			if err != nil {
				return fmt.Errorf("迁移团队订阅 %s 的商业权益快照失败: %w", subscription.ID, err)
			}
			if updated {
				if err := tx.Model(subscription).Update("plan_snapshot_json", snapshot).Error; err != nil {
					return err
				}
			}
		}
		var orders []model.MembershipOrder
		if err := tx.Where("team_id <> ''").Find(&orders).Error; err != nil {
			return err
		}
		for index := range orders {
			order := &orders[index]
			updated, snapshot, err := commercialSnapshotBackfill(order.PlanID, order.PlanSnapshotJSON, planByID)
			if err != nil {
				return fmt.Errorf("迁移团队订单 %s 的商业权益快照失败: %w", order.ID, err)
			}
			if updated {
				if err := tx.Model(order).Update("plan_snapshot_json", snapshot).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func commercialSnapshotBackfill(planID string, encoded string, planByID map[string]model.MembershipPlan) (bool, string, error) {
	if encoded == "" {
		return false, "", fmt.Errorf("套餐快照为空")
	}
	var snapshot model.MembershipPlan
	if err := json.Unmarshal([]byte(encoded), &snapshot); err != nil {
		return false, "", err
	}
	if snapshot.ID == "" || snapshot.ID != planID || snapshot.Audience != model.MembershipAudienceTeam {
		return false, "", fmt.Errorf("套餐快照与团队订单不一致")
	}
	if snapshot.TeamStorageBytes > 0 || snapshot.UnlimitedTaskQueue || snapshot.SharedAssetsEnabled ||
		snapshot.ProjectPermissionsEnabled || snapshot.InvoicingEnabled || snapshot.CommercialUseEnabled {
		return false, encoded, nil
	}
	plan, exists := planByID[planID]
	if !exists {
		return false, "", fmt.Errorf("找不到团队套餐 %s", planID)
	}
	snapshot.UnlimitedTaskQueue = plan.UnlimitedTaskQueue
	snapshot.TeamStorageBytes = plan.TeamStorageBytes
	snapshot.SharedAssetsEnabled = plan.SharedAssetsEnabled
	snapshot.ProjectPermissionsEnabled = plan.ProjectPermissionsEnabled
	snapshot.InvoicingEnabled = plan.InvoicingEnabled
	snapshot.CommercialUseEnabled = plan.CommercialUseEnabled
	updated, err := json.Marshal(snapshot)
	if err != nil {
		return false, "", err
	}
	return true, string(updated), nil
}
