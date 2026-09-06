package repository

import (
	"errors"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrProjectUnitScopeConflict = errors.New("project unit scope conflict")

// Asset association writers serialize on the project row; checks, writes and
// the revision increment belong to the same transaction.
func (r *Repository) writeProjectAssets(userID, projectID string, write func(*Repository) error) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&model.Project{}, "id = ?", projectID).Error; err != nil {
			return err
		}
		repo := New(tx)
		if _, err := repo.ProjectEditableForUser(userID, projectID, time.Now()); err != nil {
			return err
		}
		if err := write(repo); err != nil {
			return err
		}
		return repo.BumpProjectRevision(projectID)
	})
}

func (r *Repository) UpsertShotAssetReference(userID, projectID string, reference *model.ShotAssetReference) error {
	return r.writeProjectAssets(userID, projectID, func(repo *Repository) error {
		if _, err := repo.ShotForProject(projectID, reference.ShotID); err != nil {
			return err
		}
		if _, err := repo.AssetVersionForProject(projectID, reference.AssetVersionID); err != nil {
			return err
		}
		var existing model.ShotAssetReference
		err := repo.db.Where("shot_id = ? AND asset_version_id = ? AND role = ?", reference.ShotID, reference.AssetVersionID, reference.Role).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return repo.db.Create(reference).Error
		}
		if err != nil {
			return err
		}
		if err := repo.db.Model(&existing).Update("status", reference.Status).Error; err != nil {
			return err
		}
		existing.Status = reference.Status
		*reference = existing
		return nil
	})
}

func (r *Repository) CreateProjectAssetCandidates(userID, projectID string, candidates []model.ProjectAssetCandidate) error {
	return r.writeProjectAssets(userID, projectID, func(repo *Repository) error {
		if len(candidates) == 0 {
			return errors.New("asset candidates are empty")
		}
		for _, candidate := range candidates {
			if candidate.ProjectID != projectID {
				return ErrProjectUnitScopeConflict
			}
			if candidate.UnitID != "" {
				if _, err := repo.ProjectUnit(projectID, candidate.UnitID); err != nil {
					return err
				}
			}
			if candidate.ShotID != "" {
				shot, err := repo.ShotForProject(projectID, candidate.ShotID)
				if err != nil {
					return err
				}
				if candidate.UnitID != "" && shot.UnitID != candidate.UnitID {
					return ErrProjectUnitScopeConflict
				}
			}
		}
		return repo.db.Create(&candidates).Error
	})
}
