package repository

import (
	"errors"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrCanvasProjectScopeConflict = errors.New("canvas project scope conflict")

// LinkCanvasUnit atomically assigns the canvas, upserts the chapter link, and advances the project aggregate revision.
func (r *Repository) LinkCanvasUnit(userID string, link *model.CanvasUnitLink, now time.Time) (int64, error) {
	var committedRevision int64
	err := r.db.Transaction(func(tx *gorm.DB) error {
		project, err := New(tx).ProjectEditableForUser(userID, link.ProjectID, now)
		if err != nil {
			return err
		}
		var lockedProject model.Project
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND revision = ?", project.ID, project.Revision).
			Take(&lockedProject).Error; err != nil {
			return err
		}
		var unit model.ProjectUnit
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND project_id = ?", link.UnitID, link.ProjectID).
			Take(&unit).Error; err != nil {
			return err
		}
		canvasUpdate := struct {
			ProjectID string    `gorm:"column:project_id"`
			UpdatedAt time.Time `gorm:"column:updated_at"`
		}{ProjectID: link.ProjectID, UpdatedAt: now}
		canvasResult := tx.Model(&model.CanvasProject{}).
			Where("id = ? AND user_id = ? AND team_id = ? AND (project_id = '' OR project_id = ?)", link.CanvasID, userID, project.TeamID, link.ProjectID).
			Select("project_id", "updated_at").Updates(canvasUpdate)
		if canvasResult.Error != nil {
			return canvasResult.Error
		}
		if canvasResult.RowsAffected != 1 {
			return ErrCanvasProjectScopeConflict
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "project_id"}, {Name: "canvas_id"}, {Name: "unit_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"role"}),
		}).Create(link).Error; err != nil {
			return err
		}
		var storedLink model.CanvasUnitLink
		if err := tx.Where("project_id = ? AND canvas_id = ? AND unit_id = ?", link.ProjectID, link.CanvasID, link.UnitID).First(&storedLink).Error; err != nil {
			return err
		}
		*link = storedLink
		projectUpdate := struct {
			Revision  int64     `gorm:"column:revision"`
			UpdatedAt time.Time `gorm:"column:updated_at"`
		}{Revision: lockedProject.Revision + 1, UpdatedAt: now}
		projectResult := tx.Model(&model.Project{}).
			Where("id = ? AND revision = ?", link.ProjectID, lockedProject.Revision).
			Select("revision", "updated_at").Updates(projectUpdate)
		if projectResult.Error != nil {
			return projectResult.Error
		}
		if projectResult.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		committedRevision = projectUpdate.Revision
		return nil
	})
	return committedRevision, err
}
