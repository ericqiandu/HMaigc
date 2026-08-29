package repository

import (
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type agentMediaAssemblyTimelineUpdate struct {
	Status              model.AgentTimelineItemStatus `gorm:"column:status"`
	SourceEventSequence int64                         `gorm:"column:source_event_sequence"`
	ContentJSON         string                        `gorm:"column:content_json"`
	CompletedAt         *time.Time                    `gorm:"column:completed_at"`
	UpdatedAt           time.Time                     `gorm:"column:updated_at"`
}

// AppendAgentMediaAssemblyTimeline appends the persisted internal Task state
// before exposing the corresponding sequence event. All lifecycle updates
// mutate the original media.assemble tool item.
func (r *Repository) AppendAgentMediaAssemblyTimeline(
	scope agentruntime.Scope,
	content agentruntime.MediaAssemblyTimelineContent,
) (*model.AgentRunEvent, error) {
	if err := scope.Validate(); err != nil {
		return nil, errors.Join(ErrAgentTimelineConflict, err)
	}
	if err := content.Validate(); err != nil {
		return nil, errors.Join(ErrAgentTimelineConflict, err)
	}
	itemID := agentFactID("timeline", scope.RunID, "tool-call", content.ToolCallID+":"+strconv.Itoa(content.ActionVersion))
	payload, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	var event model.AgentRunEvent
	appended := false
	err = r.db.Transaction(func(tx *gorm.DB) error {
		var existing model.AgentTimelineItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", itemID).Take(&existing).Error; err != nil {
			return err
		}
		if existing.TenantKind != scope.TenantKind || existing.TenantID != scope.TenantID ||
			existing.ThreadID != scope.ThreadID || existing.RunID != scope.RunID ||
			existing.Kind != model.AgentTimelineItemToolCall {
			return ErrAgentTimelineConflict
		}
		targetStatus, targetErr := mediaAssemblyTimelineItemStatus(content)
		if targetErr != nil {
			return targetErr
		}
		if existing.Status == targetStatus && existing.ContentJSON == string(payload) {
			return nil
		}
		lateCancelledOutput := isLateCancelledAssemblyOutput(existing, TimelineMutation{
			Kind: model.AgentTimelineItemToolCall, ToStatus: targetStatus, ContentJSON: payload,
		})
		if agentTimelineStatusTerminal(existing.Status) && !lateCancelledOutput {
			return ErrAgentTimelineConflict
		}
		sequence, allocateErr := allocateAgentEventSequence(tx, scope, now)
		if allocateErr != nil {
			return allocateErr
		}
		event = model.AgentRunEvent{
			ID:    agentFactID("event", scope.RunID, strconv.FormatInt(sequence, 10)),
			RunID: scope.RunID, Sequence: sequence, Kind: agentruntime.EventArtifactAvailable,
			PayloadJSON: string(payload), CreatedAt: now,
		}
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		if lateCancelledOutput {
			update := agentMediaAssemblyTimelineUpdate{
				Status: targetStatus, SourceEventSequence: sequence, ContentJSON: string(payload),
				CompletedAt: existing.CompletedAt, UpdatedAt: now,
			}
			result := tx.Model(&model.AgentTimelineItem{}).
				Where("id = ? AND source_event_sequence = ? AND status = ?", existing.ID, existing.SourceEventSequence, existing.Status).
				Select("status", "source_event_sequence", "content_json", "completed_at", "updated_at").
				Updates(update)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrAgentTimelineConflict
			}
		} else {
			nextOrdinal, ordinalErr := nextAgentTimelineOrdinal(tx, scope.RunID)
			if ordinalErr != nil {
				return ordinalErr
			}
			fromStatus := model.AgentTimelineItemInProgress
			if err := persistAgentTimelineMutation(tx, scope, TimelineMutation{
				ItemID: itemID, Kind: model.AgentTimelineItemToolCall,
				FromStatus: &fromStatus, ToStatus: targetStatus,
				SourceEventSequence: sequence, ContentJSON: payload,
			}, &nextOrdinal, now); err != nil {
				return err
			}
		}
		appended = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !appended {
		return nil, nil
	}
	return &event, nil
}

func mediaAssemblyTimelineItemStatus(content agentruntime.MediaAssemblyTimelineContent) (model.AgentTimelineItemStatus, error) {
	switch content.TaskStatus {
	case agentruntime.MediaAssemblyTaskQueued, agentruntime.MediaAssemblyTaskRunning:
		return model.AgentTimelineItemInProgress, nil
	case agentruntime.MediaAssemblyTaskSucceeded:
		return model.AgentTimelineItemCompleted, nil
	case agentruntime.MediaAssemblyTaskFailed:
		return model.AgentTimelineItemFailed, nil
	case agentruntime.MediaAssemblyTaskCancelled:
		return model.AgentTimelineItemInterrupted, nil
	default:
		return "", ErrAgentTimelineConflict
	}
}
