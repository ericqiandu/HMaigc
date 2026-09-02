package repository

import (
	"encoding/json"
	"errors"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func (r *Repository) recordAgentAssetPublicationFailure(input PublishAgentAssetInput, candidate model.AgentAssetPublication, errorCode string) error {
	now := time.Now().UTC()
	candidate.AssetID = ""
	candidate.AssetVersionID = ""
	candidate.ProjectAssetLinkID = ""
	candidate.RepresentationID = ""
	candidate.Status = model.AgentAssetPublicationFailed
	candidate.LastErrorCode = errorCode
	candidate.CompletedAt = nil
	candidate.UpdatedAt = now
	content := agentruntime.AssetPublicationFailureContent{
		ContentType: agentruntime.AssetPublicationFailedType, PublicationID: candidate.ID,
		ArtifactRevisionID: input.ArtifactRevisionID, ErrorCode: errorCode,
	}
	if err := content.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(content)
	if err != nil {
		return err
	}
	return r.db.WithContext(input.Context).Transaction(func(tx *gorm.DB) error {
		existing, found, err := loadAgentAssetPublicationTx(tx, input, candidate.ID)
		if err != nil {
			return err
		}
		if found {
			if !sameAgentAssetPublicationIdentity(existing, candidate) || existing.Status == model.AgentAssetPublicationSucceeded {
				return ErrAgentAssetPublicationConflict
			}
			if existing.Status != model.AgentAssetPublicationFailed {
				candidate.Version = existing.Version + 1
				update := tx.Model(&model.AgentAssetPublication{}).Where("id = ? AND version = ?", existing.ID, existing.Version).
					Select("status", "version", "last_error_code", "updated_at").Updates(&candidate)
				if update.Error != nil || update.RowsAffected != 1 {
					return ErrAgentAssetPublicationConflict
				}
			}
		} else if err := tx.Create(&candidate).Error; err != nil {
			return err
		}
		return appendAgentAssetPublicationTimelineTx(
			tx, input.Scope, candidate.ID+"-failure", model.AgentTimelineItemFailed, payload, now,
		)
	})
}

func (r *Repository) appendAgentAssetPublicationSuccess(input PublishAgentAssetInput, result PublishedAgentAsset) error {
	content := agentruntime.AssetPublicationContent{
		ContentType: agentruntime.AssetPublicationContentType, PublicationID: result.Publication.ID,
		ArtifactRevisionID: result.Publication.ArtifactRevisionID, ResourceID: result.Representation.ResourceID,
		AssetID: result.Asset.ID, AssetVersionID: result.AssetVersion.ID, ProjectAssetLinkID: result.ProjectAssetLink.ID,
		RepresentationID: result.Representation.ID, PublicationPurpose: result.Publication.PublicationPurpose,
		TargetCategory: string(result.Asset.Category), TargetBindingKey: input.TargetBindingKey,
	}
	if err := content.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(content)
	if err != nil {
		return err
	}
	return r.db.WithContext(input.Context).Transaction(func(tx *gorm.DB) error {
		return appendAgentAssetPublicationTimelineTx(
			tx, input.Scope, result.Publication.ID, model.AgentTimelineItemCompleted, payload, input.Now,
		)
	})
}

func appendAgentAssetPublicationTimelineTx(
	tx *gorm.DB,
	scope agentruntime.Scope,
	itemID string,
	status model.AgentTimelineItemStatus,
	payload json.RawMessage,
	now time.Time,
) error {
	var existing model.AgentTimelineItem
	existingErr := tx.Where("id = ?", itemID).First(&existing).Error
	if existingErr == nil {
		if existing.TenantKind != scope.TenantKind || existing.TenantID != scope.TenantID || existing.ThreadID != scope.ThreadID ||
			existing.RunID != scope.RunID || existing.Kind != model.AgentTimelineItemArtifact || existing.Status != status ||
			existing.ContentJSON != string(payload) {
			return ErrAgentAssetPublicationConflict
		}
		return nil
	}
	if !errors.Is(existingErr, gorm.ErrRecordNotFound) {
		return existingErr
	}
	sequence, err := allocateAgentEventSequence(tx, scope, now)
	if err != nil {
		return err
	}
	event := model.AgentRunEvent{
		ID:    agentFactID("event", scope.RunID, string(agentruntime.EventArtifactAvailable), itemID),
		RunID: scope.RunID, Sequence: sequence, Kind: agentruntime.EventArtifactAvailable,
		PayloadJSON: string(payload), CreatedAt: now,
	}
	if err := tx.Create(&event).Error; err != nil {
		return err
	}
	nextOrdinal, err := nextAgentTimelineOrdinal(tx, scope.RunID)
	if err != nil {
		return err
	}
	return persistAgentTimelineMutation(tx, scope, TimelineMutation{
		ItemID: itemID, Kind: model.AgentTimelineItemArtifact, ToStatus: status,
		SourceEventSequence: sequence, ContentJSON: payload,
	}, &nextOrdinal, now)
}
