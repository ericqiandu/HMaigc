package repository

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrWatermarkPolicyUnavailable     = errors.New("watermark policy unavailable")
	ErrWatermarkPolicyVersionConflict = errors.New("watermark policy version conflict")
)

func (r *Repository) CurrentWatermarkPolicy() (*model.PolicyPublication, error) {
	return currentWatermarkPolicyTx(r.db)
}

func currentWatermarkPolicyTx(db *gorm.DB) (*model.PolicyPublication, error) {
	var head model.PolicyPublicationHead
	if err := db.First(&head, "kind = ?", model.PolicyKindAIWatermark).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if strings.TrimSpace(head.CurrentPublicationID) == "" {
		return nil, nil
	}
	var publication model.PolicyPublication
	if err := db.First(&publication, "id = ? AND kind = ?", head.CurrentPublicationID, model.PolicyKindAIWatermark).Error; err != nil {
		return nil, fmt.Errorf("current watermark publication %s: %w", head.CurrentPublicationID, err)
	}
	return &publication, nil
}

// PublishWatermarkPolicy serializes the immutable publication and its audit fact in one transaction.
func (r *Repository) PublishWatermarkPolicy(publication *model.PolicyPublication, audit *model.AdminAuditEvent) error {
	if publication == nil || audit == nil || strings.TrimSpace(publication.ID) == "" || strings.TrimSpace(publication.ContentHash) == "" {
		return gorm.ErrInvalidData
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		now := publication.PublishedAt
		if now.IsZero() {
			now = time.Now().UTC()
			publication.PublishedAt = now
		}
		head := model.PolicyPublicationHead{Kind: model.PolicyKindAIWatermark, UpdatedAt: now}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&head).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&head, "kind = ?", model.PolicyKindAIWatermark).Error; err != nil {
			return err
		}
		publication.Kind = model.PolicyKindAIWatermark
		publication.Version = head.CurrentVersion + 1
		if err := tx.Create(publication).Error; err != nil {
			return err
		}
		result := tx.Model(&model.PolicyPublicationHead{}).
			Where("kind = ? AND current_version = ?", head.Kind, head.CurrentVersion).
			Updates(map[string]any{"current_publication_id": publication.ID, "current_version": publication.Version, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrWatermarkPolicyVersionConflict
		}
		metadata, err := json.Marshal(map[string]any{"publicationId": publication.ID, "version": publication.Version, "contentHash": publication.ContentHash})
		if err != nil {
			return err
		}
		audit.TargetID = publication.ID
		audit.MetadataJSON = string(metadata)
		return tx.Create(audit).Error
	})
}

func (r *Repository) WatermarkPreference(userID string) (*model.UserWatermarkPreference, *model.PolicyPublication, error) {
	var preference model.UserWatermarkPreference
	err := r.db.First(&preference, "user_id = ?", strings.TrimSpace(userID)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		preference = model.UserWatermarkPreference{}
	} else if err != nil {
		return nil, nil, err
	}
	publication, err := currentWatermarkPolicyTx(r.db)
	if err != nil {
		return nil, nil, err
	}
	if preference.UserID == "" {
		return nil, publication, nil
	}
	return &preference, publication, nil
}

// SaveWatermarkPreference commits preference, consent and immutable event as one account fact.
func (r *Repository) SaveWatermarkPreference(userID string, remove bool, publicationID string, event *model.UserWatermarkPreferenceEvent, now time.Time) (*model.UserWatermarkPreference, *model.PolicyPublication, error) {
	userID = strings.TrimSpace(userID)
	publicationID = strings.TrimSpace(publicationID)
	if userID == "" || event == nil || strings.TrimSpace(event.ID) == "" {
		return nil, nil, gorm.ErrInvalidData
	}
	var saved model.UserWatermarkPreference
	var current *model.PolicyPublication
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var head model.PolicyPublicationHead
		headErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&head, "kind = ?", model.PolicyKindAIWatermark).Error
		if headErr != nil && !errors.Is(headErr, gorm.ErrRecordNotFound) {
			return headErr
		}
		if headErr == nil && strings.TrimSpace(head.CurrentPublicationID) != "" {
			var publication model.PolicyPublication
			if err := tx.First(&publication, "id = ? AND kind = ?", head.CurrentPublicationID, model.PolicyKindAIWatermark).Error; err != nil {
				return err
			}
			current = &publication
		}
		if remove {
			if current == nil {
				return ErrWatermarkPolicyUnavailable
			}
			if publicationID == "" || publicationID != current.ID {
				return ErrWatermarkPolicyVersionConflict
			}
			consentID, err := newWatermarkFactID()
			if err != nil {
				return err
			}
			consent := model.UserPolicyConsent{ID: consentID, UserID: userID, PolicyPublicationID: current.ID, AcceptedAt: now}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&consent).Error; err != nil {
				return err
			}
		}

		lookup := tx.First(&saved, "user_id = ?", userID)
		if errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
			saved = model.UserWatermarkPreference{UserID: userID, CreatedAt: now}
		} else if lookup.Error != nil {
			return lookup.Error
		}
		saved.RemoveWatermark = remove
		saved.UpdatedAt = now
		if remove {
			saved.AcceptedPublicationID = current.ID
			saved.AcceptedAt = &now
		}
		if saved.CreatedAt.IsZero() {
			saved.CreatedAt = now
		}
		if err := tx.Save(&saved).Error; err != nil {
			return err
		}
		event.UserID = userID
		event.RemoveWatermark = remove
		if remove {
			event.PolicyPublicationID = current.ID
		} else {
			event.PolicyPublicationID = ""
		}
		event.ResultStatus = "succeeded"
		event.CreatedAt = now
		return tx.Create(event).Error
	})
	if err != nil {
		return nil, nil, err
	}
	return &saved, current, nil
}

// RecordWatermarkPreferenceFailure appends a safe business-rejection fact after the
// preference transaction has rolled back. System/database failures are not rewritten
// as successful audit facts by callers.
func (r *Repository) RecordWatermarkPreferenceFailure(event *model.UserWatermarkPreferenceEvent) error {
	if event == nil || strings.TrimSpace(event.ID) == "" || strings.TrimSpace(event.UserID) == "" || strings.TrimSpace(event.ResultStatus) == "" {
		return gorm.ErrInvalidData
	}
	return r.db.Create(event).Error
}

func freezeTaskWatermarkTx(tx *gorm.DB, task *model.Task, capability model.WatermarkCapability) error {
	task.WatermarkCapability = capability
	task.WatermarkDirective = model.WatermarkDirectiveProviderDefault
	task.WatermarkParameterApplied = false
	task.WatermarkParameterValue = nil
	task.WatermarkPolicyPublicationID = ""
	task.WatermarkPolicyVersion = 0

	switch capability {
	case model.WatermarkCapabilityUnsupported, model.WatermarkCapabilityNotApplicable:
		return nil
	case model.WatermarkCapabilityControlled:
	default:
		return fmt.Errorf("unknown watermark capability %q", capability)
	}

	withWatermark := true
	task.WatermarkDirective = model.WatermarkDirectiveWithWatermark
	task.WatermarkParameterApplied = true
	task.WatermarkParameterValue = &withWatermark

	var head model.PolicyPublicationHead
	// Task creation only needs a stable publication snapshot. A shared row lock blocks a
	// concurrent publication writer without serializing unrelated task creations.
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&head, "kind = ?", model.PolicyKindAIWatermark).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if strings.TrimSpace(head.CurrentPublicationID) == "" {
		return nil
	}
	var publication model.PolicyPublication
	if err := tx.First(&publication, "id = ? AND kind = ? AND version = ?", head.CurrentPublicationID, model.PolicyKindAIWatermark, head.CurrentVersion).Error; err != nil {
		return fmt.Errorf("freeze current watermark publication %s: %w", head.CurrentPublicationID, err)
	}
	var preference model.UserWatermarkPreference
	if err := tx.First(&preference, "user_id = ?", task.UserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if !preference.RemoveWatermark || preference.AcceptedPublicationID != head.CurrentPublicationID {
		return nil
	}
	withoutWatermark := false
	task.WatermarkDirective = model.WatermarkDirectiveWithoutWatermark
	task.WatermarkParameterValue = &withoutWatermark
	task.WatermarkPolicyPublicationID = head.CurrentPublicationID
	task.WatermarkPolicyVersion = head.CurrentVersion
	return nil
}

type frozenWatermarkTaskLogPayload struct {
	Capability          model.WatermarkCapability `json:"capability"`
	Directive           model.WatermarkDirective  `json:"directive"`
	ParameterApplied    bool                      `json:"parameterApplied"`
	ParameterValue      *bool                     `json:"parameterValue"`
	PolicyPublicationID string                    `json:"policyPublicationId"`
	PolicyVersion       int64                     `json:"policyVersion"`
}

func createFrozenWatermarkTaskLogTx(tx *gorm.DB, task *model.Task) error {
	payload, err := json.Marshal(frozenWatermarkTaskLogPayload{
		Capability: task.WatermarkCapability, Directive: task.WatermarkDirective,
		ParameterApplied: task.WatermarkParameterApplied, ParameterValue: task.WatermarkParameterValue,
		PolicyPublicationID: task.WatermarkPolicyPublicationID, PolicyVersion: task.WatermarkPolicyVersion,
	})
	if err != nil {
		return fmt.Errorf("encode frozen watermark task log: %w", err)
	}
	logID, err := newWatermarkFactID()
	if err != nil {
		return err
	}
	return tx.Create(&model.TaskLog{
		ID: logID, UserID: task.UserID, TaskID: task.ID, Level: "info",
		Message: "水印执行指令已冻结", Payload: string(payload), CreatedAt: time.Now().UTC(),
	}).Error
}

func newWatermarkFactID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate watermark fact id: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}
