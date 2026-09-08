package database

import (
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const productRolloutSettingKey = "product_rollouts"

const disabledProductRolloutSettingJSON = `{"canvasToolRegistryV2":{"percentage":0,"adminAllowlist":[]},"canvasMediaToolsV2":{"percentage":0,"adminAllowlist":[]},"canvasProductionNodesV2":{"percentage":0,"adminAllowlist":[]},"creativeWorkspaceV2":{"percentage":0,"adminAllowlist":[]},"skillLibraryV2":{"percentage":0,"adminAllowlist":[]}}`

func seedDisabledProductRolloutSetting(db *gorm.DB) error {
	setting := model.SystemSetting{
		Key:       productRolloutSettingKey,
		ValueJSON: disabledProductRolloutSettingJSON,
		UpdatedBy: "system-migration",
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&setting).Error
}
