package database

import (
	"encoding/json"
	"fmt"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/skillcatalog"

	"gorm.io/gorm"
)

func seedFirstPartySkills(db *gorm.DB) error {
	builtins, err := skillcatalog.Builtins()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, builtin := range builtins {
		categories, err := json.Marshal(builtin.Categories)
		if err != nil {
			return fmt.Errorf("序列化第一方技能 %s 分类失败: %w", builtin.Dir, err)
		}
		skillID := "skill-" + builtin.Dir
		versionID := fmt.Sprintf("skill-%s-v%d", builtin.Dir, builtin.Version)
		var skill model.Skill
		lookup := db.Where("dir = ?", builtin.Dir).Limit(1).Find(&skill)
		if lookup.Error != nil {
			return lookup.Error
		}
		if lookup.RowsAffected == 0 {
			skill = model.Skill{ID: skillID, Dir: builtin.Dir, CreatedAt: now}
			if err := db.Create(&skill).Error; err != nil {
				return fmt.Errorf("创建第一方技能 %s 失败: %w", builtin.Dir, err)
			}
		} else if skill.ID != skillID {
			return fmt.Errorf("第一方技能 %s 的稳定标识冲突", builtin.Dir)
		}

		var version model.SkillVersion
		versionLookup := db.Where("skill_id = ? AND version = ?", skillID, builtin.Version).Limit(1).Find(&version)
		if versionLookup.Error != nil {
			return versionLookup.Error
		}
		if versionLookup.RowsAffected == 0 {
			publishedAt := now
			version = model.SkillVersion{
				ID: versionID, SkillID: skillID, Version: builtin.Version, Instructions: builtin.Instructions,
				Checksum: builtin.Checksum, CapabilityManifestJSON: builtin.CapabilityManifestJSON,
				Changelog: builtin.Changelog, CreatedBy: "system", PublishedAt: &publishedAt, CreatedAt: now,
			}
			if err := db.Create(&version).Error; err != nil {
				return fmt.Errorf("发布第一方技能 %s v%d 失败: %w", builtin.Dir, builtin.Version, err)
			}
		} else if !publishedSkillVersionMatches(version, builtin, versionID, skillID) {
			return fmt.Errorf("第一方技能 %s v%d 已发布事实与代码不一致", builtin.Dir, builtin.Version)
		}

		catalogEntry := model.Skill{
			Name: builtin.Name, Description: builtin.Description, Icon: builtin.Icon, CoverURL: builtin.CoverURL,
			CategoriesJSON: string(categories), Visibility: builtin.Visibility, Status: model.SkillStatusPublished,
			CurrentVersionID: versionID, SourceKind: builtin.SourceKind, SourceURL: builtin.SourceURL,
			SourceRevision: builtin.SourceRevision, SourceLicense: builtin.SourceLicense, UpdatedAt: now,
		}
		if skillCatalogMetadataMatches(skill, catalogEntry) {
			continue
		}
		if err := db.Model(&model.Skill{}).Where("id = ?", skillID).
			Select("name", "description", "icon", "cover_url", "categories_json", "visibility", "status", "current_version_id", "source_kind", "source_url", "source_revision", "source_license", "updated_at").
			Updates(&catalogEntry).Error; err != nil {
			return fmt.Errorf("更新第一方技能 %s 目录失败: %w", builtin.Dir, err)
		}
	}
	return nil
}

func publishedSkillVersionMatches(version model.SkillVersion, builtin skillcatalog.BuiltinSkill, versionID string, skillID string) bool {
	return version.ID == versionID && version.SkillID == skillID && version.Version == builtin.Version &&
		version.Checksum == builtin.Checksum && version.Instructions == builtin.Instructions &&
		version.CapabilityManifestJSON == builtin.CapabilityManifestJSON &&
		version.Changelog == builtin.Changelog && version.CreatedBy == "system" && version.PublishedAt != nil
}

func skillCatalogMetadataMatches(current model.Skill, expected model.Skill) bool {
	return current.Name == expected.Name && current.Description == expected.Description && current.Icon == expected.Icon &&
		current.CoverURL == expected.CoverURL && current.CategoriesJSON == expected.CategoriesJSON &&
		current.Visibility == expected.Visibility && current.Status == expected.Status &&
		current.CurrentVersionID == expected.CurrentVersionID && current.SourceKind == expected.SourceKind &&
		current.SourceURL == expected.SourceURL && current.SourceRevision == expected.SourceRevision &&
		current.SourceLicense == expected.SourceLicense
}
