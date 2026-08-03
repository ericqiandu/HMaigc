package database

import (
	"encoding/json"
	"errors"
	"fmt"

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
	if err := seedSuperResolutionPricingRules(db); err != nil {
		return err
	}
	if err := seedMiniMaxH3SupplierPricing(db); err != nil {
		return err
	}
	if err := seedMiniMaxAudioSupplierPricing(db); err != nil {
		return err
	}
	// 超分已切换为独立服务定价，清除旧模型价格中的 SR_* 双轨数据。
	if err := db.Where("resolution LIKE ?", "SR_%").Delete(&model.ChannelModelPriceTier{}).Error; err != nil {
		return err
	}
	if err := db.Where("specification LIKE ?", "SR_%").Delete(&model.ModelPricingTier{}).Error; err != nil {
		return err
	}
	return db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_nonempty ON users(lower(email)) WHERE email <> ''").Error
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
		{ID: "minimax-h3-cost-regenerate-input-image", ModelPricingID: pricing.ID, Specification: "REGENERATE_INPUT_IMAGE_OVERAGE", SupplierCostMicros: 150_000},
		{ID: "minimax-h3-cost-regenerate-input-video", ModelPricingID: pricing.ID, Specification: "REGENERATE_INPUT_VIDEO_768P", SupplierCostMicros: 300_000},
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
