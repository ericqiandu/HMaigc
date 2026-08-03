package database

import (
	"encoding/json"
	"fmt"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
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
		&model.MembershipPlan{},
		&model.Team{},
		&model.TeamMember{},
		&model.TeamInvitation{},
		&model.TeamAuditEvent{},
		&model.TeamCreditAccount{},
		&model.TeamCreditLedgerEntry{},
		&model.MembershipOrder{},
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
	legacyModelIconColumns := db.Migrator().HasColumn(&model.ChannelModel{}, "icon_file") || db.Migrator().HasColumn(&model.ChannelModel{}, "icon_mime_type")
	if err := db.AutoMigrate(Models()...); err != nil {
		return err
	}
	if err := migrateChannelModelBrands(db, legacyModelIconColumns); err != nil {
		return err
	}
	// 逻辑删除后的同名模型允许重新添加，旧唯一索引不能继续覆盖已删除记录。
	if err := db.Exec("DROP INDEX IF EXISTS idx_channel_model_key").Error; err != nil {
		return err
	}
	if err := db.Exec("DROP INDEX IF EXISTS idx_channel_voices_idempotency_key").Error; err != nil {
		return err
	}
	if err := db.Exec("DROP INDEX IF EXISTS idx_channel_voice_idempotency").Error; err != nil {
		return err
	}
	if err := db.Exec("DROP INDEX IF EXISTS idx_users_email").Error; err != nil {
		return err
	}
	// 价格策略字段为显式必填；将迁移前遗留的 NULL、空串和纯空格统一单价记录一次性标记为 flat。
	if err := db.Model(&model.ChannelModel{}).
		Where("price_strategy IS NULL OR TRIM(price_strategy) = ''").
		Update("price_strategy", "flat").Error; err != nil {
		return err
	}
	const teamStorage130TB int64 = 130 * (1 << 40)
	if err := db.Model(&model.MembershipPlan{}).
		Where("audience = ? AND (team_storage_bytes IS NULL OR team_storage_bytes = 0)", model.MembershipAudienceTeam).
		Updates(map[string]any{
			"unlimited_task_queue": true, "team_storage_bytes": teamStorage130TB,
			"shared_assets_enabled": true, "project_permissions_enabled": true,
			"invoicing_enabled": true, "commercial_use_enabled": true,
		}).Error; err != nil {
		return err
	}
	if err := backfillLegacyTeamCommercialSnapshots(db); err != nil {
		return err
	}
	return db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_nonempty ON users(lower(email)) WHERE email <> ''").Error
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
