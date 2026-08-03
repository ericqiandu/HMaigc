package repository

import (
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func (r *Repository) SuperResolutionPricingRules() ([]model.SuperResolutionPricingRule, error) {
	var rules []model.SuperResolutionPricingRule
	err := r.db.
		Order("edition asc").
		Order("CASE resolution WHEN '720P' THEN 1 WHEN '1080P' THEN 2 WHEN '2K' THEN 3 WHEN '4K' THEN 4 WHEN '8K' THEN 5 ELSE 99 END").
		Order("fps_min_exclusive asc").
		Find(&rules).Error
	return rules, err
}

func (r *Repository) SuperResolutionPricingRule(edition string, resolution string, fps int) (*model.SuperResolutionPricingRule, error) {
	var rule model.SuperResolutionPricingRule
	err := r.db.First(&rule,
		"edition = ? AND resolution = ? AND fps_min_exclusive < ? AND fps_max_inclusive >= ? AND enabled = ?",
		edition, resolution, fps, fps, true,
	).Error
	return &rule, err
}

func (r *Repository) ReplaceSuperResolutionPricingRules(rules []model.SuperResolutionPricingRule, audit *model.AdminAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.SuperResolutionPricingRule{}).Error; err != nil {
			return err
		}
		if len(rules) > 0 {
			if err := tx.Create(&rules).Error; err != nil {
				return err
			}
		}
		if audit != nil {
			return tx.Create(audit).Error
		}
		return nil
	})
}
