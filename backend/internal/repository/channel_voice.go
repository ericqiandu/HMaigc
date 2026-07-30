package repository

import (
	"errors"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) ChannelVoices(channelID string, includeDisabled bool) ([]model.ChannelVoice, error) {
	var voices []model.ChannelVoice
	query := r.db.Where("channel_id = ?", channelID).Order("display_name asc, created_at asc")
	if !includeDisabled {
		query = query.Where("enabled = ? AND provider_status IN ?", true, []string{"active", "pending_activation"})
	}
	return voices, query.Find(&voices).Error
}

func (r *Repository) ChannelVoicesForUser(channelID string, userID string, includeDisabled bool) ([]model.ChannelVoice, error) {
	var voices []model.ChannelVoice
	query := r.db.
		Where("channel_id = ? AND (owner_user_id = '' OR owner_user_id = ?)", channelID, userID).
		Order("display_name asc, created_at asc")
	if !includeDisabled {
		query = query.Where("enabled = ? AND provider_status IN ?", true, []string{"active", "pending_activation"})
	}
	return voices, query.Find(&voices).Error
}

func (r *Repository) ChannelVoiceByID(channelID string, id string) (*model.ChannelVoice, error) {
	var voice model.ChannelVoice
	if err := r.db.First(&voice, "id = ? AND channel_id = ?", id, channelID).Error; err != nil {
		return nil, err
	}
	return &voice, nil
}

func (r *Repository) ChannelVoiceByIDForUser(channelID string, id string, userID string) (*model.ChannelVoice, error) {
	var voice model.ChannelVoice
	if err := r.db.First(&voice, "id = ? AND channel_id = ? AND (owner_user_id = '' OR owner_user_id = ?)", id, channelID, userID).Error; err != nil {
		return nil, err
	}
	return &voice, nil
}

func (r *Repository) ChannelVoiceByKey(channelID string, voiceKey string) (*model.ChannelVoice, error) {
	var voice model.ChannelVoice
	if err := r.db.First(&voice, "channel_id = ? AND voice_key = ?", channelID, voiceKey).Error; err != nil {
		return nil, err
	}
	return &voice, nil
}

func (r *Repository) ChannelVoiceByIdempotencyKey(channelID string, ownerUserID string, key string) (*model.ChannelVoice, error) {
	var voice model.ChannelVoice
	if err := r.db.First(&voice, "channel_id = ? AND owner_user_id = ? AND idempotency_key = ?", channelID, ownerUserID, key).Error; err != nil {
		return nil, err
	}
	return &voice, nil
}

func (r *Repository) UserVoiceFavoriteIDs(userID string, voiceIDs []string) (map[string]bool, error) {
	result := make(map[string]bool)
	if len(voiceIDs) == 0 {
		return result, nil
	}
	var favorites []model.UserVoiceFavorite
	if err := r.db.Where("user_id = ? AND channel_voice_id IN ?", userID, voiceIDs).Find(&favorites).Error; err != nil {
		return nil, err
	}
	for _, favorite := range favorites {
		result[favorite.ChannelVoiceID] = true
	}
	return result, nil
}

func (r *Repository) SetUserVoiceFavorite(userID string, channelVoiceID string, favorite bool, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if favorite {
			item := model.UserVoiceFavorite{
				ID: newRepositoryID(), UserID: userID, ChannelVoiceID: channelVoiceID, CreatedAt: now,
			}
			return tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "user_id"}, {Name: "channel_voice_id"}},
				DoNothing: true,
			}).Create(&item).Error
		}
		return tx.Where("user_id = ? AND channel_voice_id = ?", userID, channelVoiceID).
			Delete(&model.UserVoiceFavorite{}).Error
	})
}

func (r *Repository) ChannelVoicePreview(channelVoiceID string, modelKey string) (*model.ChannelVoicePreview, error) {
	var preview model.ChannelVoicePreview
	if err := r.db.First(&preview, "channel_voice_id = ? AND model = ?", channelVoiceID, modelKey).Error; err != nil {
		return nil, err
	}
	return &preview, nil
}

func (r *Repository) SaveChannelVoicePreview(preview *model.ChannelVoicePreview) error {
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "channel_voice_id"}, {Name: "model"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"channel_id", "voice_key", "mime_type", "audio", "sha256", "provider_trace_id", "updated_at",
		}),
	}).Create(preview).Error
}

func (r *Repository) SaveChannelVoiceWithAudit(voice *model.ChannelVoice, audit *model.AdminAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(voice).Error; err != nil {
			return err
		}
		if audit != nil {
			return tx.Create(audit).Error
		}
		return nil
	})
}

func (r *Repository) UpsertChannelVoices(voices []model.ChannelVoice, audit *model.AdminAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if len(voices) == 0 {
			return errors.New("音色同步目录不能为空")
		}
		channelID := voices[0].ChannelID
		voiceKeys := make([]string, 0, len(voices))
		for index := range voices {
			voice := &voices[index]
			if voice.ChannelID != channelID {
				return errors.New("音色同步目录包含多个渠道")
			}
			voiceKeys = append(voiceKeys, voice.VoiceKey)
			var existing model.ChannelVoice
			findErr := tx.First(&existing, "channel_id = ? AND voice_key = ?", voice.ChannelID, voice.VoiceKey).Error
			switch {
			case findErr == nil:
				if err := tx.Model(&existing).Updates(map[string]any{
					"kind": voice.Kind, "provider_status": voice.ProviderStatus, "last_error": "",
					"language":   gorm.Expr("CASE WHEN TRIM(COALESCE(language, '')) = '' THEN ? ELSE language END", voice.Language),
					"updated_at": voice.UpdatedAt,
				}).Error; err != nil {
					return err
				}
			case errors.Is(findErr, gorm.ErrRecordNotFound):
				if err := tx.Create(voice).Error; err != nil {
					return err
				}
			default:
				return findErr
			}
		}
		if err := tx.Model(&model.ChannelVoice{}).
			Where("channel_id = ? AND owner_user_id = '' AND provider_status = ? AND voice_key NOT IN ?", channelID, "active", voiceKeys).
			Updates(map[string]any{
				"enabled": false, "provider_status": "missing", "last_error": "供应商音色目录已不再返回该音色", "updated_at": time.Now(),
			}).Error; err != nil {
			return err
		}
		if audit != nil {
			return tx.Create(audit).Error
		}
		return nil
	})
}

func (r *Repository) DeleteChannelVoice(channelID string, id string, audit *model.AdminAuditEvent, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.ChannelVoice{}).
			Where("id = ? AND channel_id = ?", id, channelID).
			Updates(map[string]any{"enabled": false, "provider_status": "deleted", "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Where("id = ? AND channel_id = ?", id, channelID).Delete(&model.ChannelVoice{}).Error; err != nil {
			return err
		}
		if err := tx.Where("channel_voice_id = ?", id).Delete(&model.ChannelVoicePreview{}).Error; err != nil {
			return err
		}
		if err := tx.Where("channel_voice_id = ?", id).Delete(&model.UserVoiceFavorite{}).Error; err != nil {
			return err
		}
		if audit != nil {
			return tx.Create(audit).Error
		}
		return nil
	})
}
