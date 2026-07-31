package repository

import (
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

type ResourcePayloadDocument struct {
	Kind        string
	ID          string
	PayloadJSON string
}

type deletedResourceValues struct {
	Status    model.ResourceStatus
	Error     string
	UpdatedAt time.Time
}

func (r *Repository) AssetRepresentationResourceIDs(assetID string) ([]string, error) {
	var resourceIDs []string
	err := r.db.Table("asset_representations").
		Distinct("asset_representations.resource_id").
		Joins("JOIN asset_versions ON asset_versions.id = asset_representations.asset_version_id").
		Where("asset_versions.asset_id = ? AND asset_representations.resource_id <> ?", assetID, "").
		Pluck("asset_representations.resource_id", &resourceIDs).Error
	return resourceIDs, err
}

func (r *Repository) ResourceHasExplicitReferenceOutsideAsset(resourceID string, assetID string) (bool, error) {
	var representationCount int64
	if err := r.db.Table("asset_representations").
		Joins("JOIN asset_versions ON asset_versions.id = asset_representations.asset_version_id").
		Where("asset_representations.resource_id = ? AND asset_versions.asset_id <> ?", resourceID, assetID).
		Count(&representationCount).Error; err != nil {
		return false, err
	}
	if representationCount > 0 {
		return true, nil
	}
	var voiceCount int64
	if err := r.db.Model(&model.VoiceProfile{}).
		Where("sample_resource_id = ?", resourceID).
		Count(&voiceCount).Error; err != nil {
		return false, err
	}
	return voiceCount > 0, nil
}

func (r *Repository) ResourceHasActiveStorageMigration(resourceID string) (bool, error) {
	var count int64
	err := r.db.Model(&model.StorageMigrationItem{}).
		Where("resource_id = ? AND status IN ?", resourceID, []model.StorageMigrationItemStatus{
			model.StorageMigrationItemPending,
			model.StorageMigrationItemRunning,
		}).
		Count(&count).Error
	return count > 0, err
}

// PotentialResourcePayloadDocuments 只用 SQL 缩小候选集，调用方仍必须解析 JSON 并做精确结构校验。
func (r *Repository) PotentialResourcePayloadDocuments(resourceID string, excludedAssetID string) ([]ResourcePayloadDocument, error) {
	pattern := "%resource:" + resourceID + "%"
	documents := make([]ResourcePayloadDocument, 0)

	var assets []model.Asset
	if err := r.db.Select("id", "payload_json").
		Where("id <> ? AND payload_json LIKE ?", excludedAssetID, pattern).
		Find(&assets).Error; err != nil {
		return nil, err
	}
	for index := range assets {
		documents = append(documents, ResourcePayloadDocument{
			Kind: "asset", ID: assets[index].ID, PayloadJSON: assets[index].PayloadJSON,
		})
	}

	var canvases []model.CanvasProject
	if err := r.db.Select("id", "payload_json").
		Where("payload_json LIKE ?", pattern).
		Find(&canvases).Error; err != nil {
		return nil, err
	}
	for index := range canvases {
		documents = append(documents, ResourcePayloadDocument{
			Kind: "canvas", ID: canvases[index].ID, PayloadJSON: canvases[index].PayloadJSON,
		})
	}

	var sessions []model.Session
	if err := r.db.Select("id", "canvas_snapshot_json", "canvas_ops_json").
		Where("canvas_snapshot_json LIKE ? OR canvas_ops_json LIKE ?", pattern, pattern).
		Find(&sessions).Error; err != nil {
		return nil, err
	}
	for index := range sessions {
		if sessions[index].CanvasSnapshotJSON != "" {
			documents = append(documents, ResourcePayloadDocument{
				Kind: "session_snapshot", ID: sessions[index].ID, PayloadJSON: sessions[index].CanvasSnapshotJSON,
			})
		}
		if sessions[index].CanvasOpsJSON != "" {
			documents = append(documents, ResourcePayloadDocument{
				Kind: "session_operations", ID: sessions[index].ID, PayloadJSON: sessions[index].CanvasOpsJSON,
			})
		}
	}
	return documents, nil
}

func (r *Repository) DeleteAssetAndMarkResourcesDeleted(userID string, assetID string, resourceIDs []string, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		versionIDs := tx.Model(&model.AssetVersion{}).Select("id").Where("asset_id = ?", assetID)
		if err := tx.Where("asset_version_id IN (?)", versionIDs).Delete(&model.CharacterVoiceBinding{}).Error; err != nil {
			return err
		}
		if err := tx.Where("asset_version_id IN (?)", versionIDs).Delete(&model.AssetRepresentation{}).Error; err != nil {
			return err
		}
		if err := tx.Where("asset_id = ?", assetID).Delete(&model.AssetVersion{}).Error; err != nil {
			return err
		}
		result := tx.Delete(&model.Asset{}, "id = ? AND user_id = ?", assetID, userID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		if len(resourceIDs) == 0 {
			return nil
		}
		return tx.Model(&model.Resource{}).
			Where("id IN ? AND user_id = ?", resourceIDs, userID).
			Select("Status", "Error", "UpdatedAt").
			Updates(deletedResourceValues{
				Status: model.ResourceStatusDeleted, Error: "", UpdatedAt: now,
			}).Error
	})
}
