package database

import (
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

// Models 是应用持久化表的唯一清单，服务启动和跨数据库迁移必须共用它。
func Models() []any {
	return []any{
		&model.User{},
		&model.UserPublicIdentity{},
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
		&model.PolicyPublicationHead{},
		&model.PolicyPublication{},
		&model.UserWatermarkPreference{},
		&model.UserPolicyConsent{},
		&model.UserWatermarkPreferenceEvent{},
		&model.UserDailyActivity{},
		&model.SystemSetting{},
		&model.DataMigration{},
		&model.UserOSSSetting{},
		&model.UserDailyUploadUsage{},
		&model.Skill{},
		&model.SkillVersion{},
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
		&model.AgentThread{},
		&model.AgentRun{},
		&model.AgentTimelineItem{},
		&model.AgentProductionPlanVersion{},
		&model.AgentProductionArtifact{},
		&model.AgentRunEvent{},
		&model.AgentCheckpoint{},
		&model.AgentToolCall{},
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
		if err := migrateLegacyAgentContractVersions(tx); err != nil {
			return err
		}
		if err := backfillLegacyTokenOutputCeilings(tx); err != nil {
			return err
		}
		if err := EnsureUserPublicIdentitySchema(tx); err != nil {
			return err
		}
		if err := migrateAgentRuntimeSkillChecksums(tx); err != nil {
			return err
		}
		if err := seedFirstPartySkills(tx); err != nil {
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
		if err := EnsureTeamIntegritySchema(tx); err != nil {
			return err
		}
		if err := EnsureProviderIntegritySchema(tx); err != nil {
			return err
		}
		if err := EnsureAgentRuntimeIntegritySchema(tx); err != nil {
			return err
		}
		return EnsureWatermarkPolicyIntegritySchema(tx)
	})
}
