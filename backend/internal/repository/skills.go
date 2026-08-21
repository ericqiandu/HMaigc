package repository

import (
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PublishedSkillRecord struct {
	ID               string
	Dir              string
	Name             string
	Description      string
	Icon             string
	CoverURL         string
	CategoriesJSON   string
	Status           model.SkillStatus
	CurrentVersionID string
	SourceKind       string
	SourceURL        string
	SourceRevision   string
	SourceLicense    string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Version          int
	Instructions     string
	Checksum         string
	Changelog        string
	PublishedAt      *time.Time
}

type PublishedSkillQuery struct {
	Page       int
	PageSize   int
	Search     string
	Categories []string
}

func (r *Repository) PublishedSkills(query PublishedSkillQuery) ([]PublishedSkillRecord, int64, error) {
	base := r.publishedSkillQuery()
	if query.Search != "" {
		like := "%" + query.Search + "%"
		base = base.Where("skills.name LIKE ? OR skills.description LIKE ?", like, like)
	}
	for _, category := range query.Categories {
		base = base.Where("skills.categories_json LIKE ?", "%\""+category+"\"%")
	}
	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var records []PublishedSkillRecord
	err := base.Select(publishedSkillCatalogSelect()).
		Order("skill_versions.published_at DESC, skills.dir ASC").
		Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).Scan(&records).Error
	return records, total, err
}

func (r *Repository) PublishedSkillByDir(dir string) (*PublishedSkillRecord, error) {
	var record PublishedSkillRecord
	err := r.publishedSkillQuery().Select(publishedSkillSelect()).Where("skills.dir = ?", dir).Take(&record).Error
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (r *Repository) PublishedSkillsByDirs(dirs []string) ([]PublishedSkillRecord, error) {
	if len(dirs) == 0 {
		return nil, nil
	}
	var records []PublishedSkillRecord
	err := r.publishedSkillQuery().Select(publishedSkillSelect()).Where("skills.dir IN ?", dirs).Scan(&records).Error
	return records, err
}

func (r *Repository) PublishedSkillCategoryValues() ([]string, error) {
	var values []string
	err := r.db.Model(&model.Skill{}).
		Where("status = ? AND visibility = ?", model.SkillStatusPublished, "public").
		Order("dir ASC").Pluck("categories_json", &values).Error
	return values, err
}

func (r *Repository) SetUserSkillActivated(state model.UserSkillState) (*model.UserSkillState, error) {
	return r.upsertUserSkillState(state, "activated")
}

func (r *Repository) SetUserSkillLiked(state model.UserSkillState) (*model.UserSkillState, error) {
	return r.upsertUserSkillState(state, "liked")
}

func (r *Repository) upsertUserSkillState(state model.UserSkillState, field string) (*model.UserSkillState, error) {
	err := r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "skill_dir"}},
		DoUpdates: clause.AssignmentColumns([]string{field, "updated_at"}),
	}).Create(&state).Error
	if err != nil {
		return nil, err
	}
	return r.UserSkillState(state.UserID, state.SkillDir)
}

func (r *Repository) publishedSkillQuery() *gorm.DB {
	return r.db.Table("skills").
		Joins("JOIN skill_versions ON skill_versions.id = skills.current_version_id").
		Where("skills.status = ? AND skills.visibility = ? AND skill_versions.published_at IS NOT NULL", model.SkillStatusPublished, "public")
}

func publishedSkillSelect() string {
	return "skills.id, skills.dir, skills.name, skills.description, skills.icon, skills.cover_url, skills.categories_json, skills.status, " +
		"skills.current_version_id, skills.source_kind, skills.source_url, skills.source_revision, skills.source_license, skills.created_at, skills.updated_at, " +
		"skill_versions.version, skill_versions.instructions, skill_versions.checksum, skill_versions.changelog, skill_versions.published_at"
}

func publishedSkillCatalogSelect() string {
	return "skills.id, skills.dir, skills.name, skills.description, skills.icon, skills.cover_url, skills.categories_json, skills.status, " +
		"skills.current_version_id, skills.source_kind, skills.source_url, skills.source_revision, skills.source_license, skills.created_at, skills.updated_at, " +
		"skill_versions.version, skill_versions.checksum, skill_versions.published_at"
}
