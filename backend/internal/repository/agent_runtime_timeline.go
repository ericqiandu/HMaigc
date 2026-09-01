package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TimelineMutation struct {
	ItemID              string
	Kind                model.AgentTimelineItemKind
	FromStatus          *model.AgentTimelineItemStatus
	ToStatus            model.AgentTimelineItemStatus
	SourceEventSequence int64
	ContentJSON         json.RawMessage
	AllowInsert         bool
}

type AppendAgentMessageDeltaInput struct {
	Scope       agentruntime.Scope
	ItemID      string
	PayloadJSON string
	Message     string
	Started     bool
	Now         time.Time
}

type FailAgentMessageStreamInput struct {
	Scope       agentruntime.Scope
	ItemID      string
	Message     string
	FailureCode string
	Now         time.Time
}

type agentMessageFailurePayload struct {
	ItemID      string `json:"itemId"`
	Message     string `json:"message"`
	FailureCode string `json:"failureCode"`
}

type agentMessageFailureContent struct {
	Message     string `json:"message"`
	FailureCode string `json:"failureCode"`
}

var ErrAgentMessageStreamClosed = errors.New("agent message stream is closed")

func (r *Repository) AppendAgentMessageDelta(input AppendAgentMessageDeltaInput) (*model.AgentRunEvent, error) {
	if err := input.Scope.Validate(); err != nil || input.ItemID == "" || input.Message == "" || input.Now.IsZero() || !json.Valid([]byte(input.PayloadJSON)) {
		return nil, errors.Join(ErrAgentTimelineConflict, errors.New("agent message delta boundary is invalid"), err)
	}
	content, err := marshalAgentTimelineContent(struct {
		Message string `json:"message"`
	}{Message: input.Message})
	if err != nil {
		return nil, err
	}
	var event model.AgentRunEvent
	err = r.db.Transaction(func(tx *gorm.DB) error {
		if writableErr := ensureAgentMessageStreamWritable(tx, input.Scope); writableErr != nil {
			return writableErr
		}
		sequence, allocateErr := allocateAgentEventSequence(tx, input.Scope, input.Now)
		if allocateErr != nil {
			return allocateErr
		}
		event = model.AgentRunEvent{ID: agentFactID("event", input.Scope.RunID, strconv.FormatInt(sequence, 10)), RunID: input.Scope.RunID, Sequence: sequence, Kind: agentruntime.EventModelDelta, PayloadJSON: input.PayloadJSON, CreatedAt: input.Now}
		if createErr := tx.Create(&event).Error; createErr != nil {
			return createErr
		}
		nextOrdinal, ordinalErr := nextAgentTimelineOrdinal(tx, input.Scope.RunID)
		if ordinalErr != nil {
			return ordinalErr
		}
		fromStatus := model.AgentTimelineItemInProgress
		mutation := TimelineMutation{
			ItemID: input.ItemID, Kind: model.AgentTimelineItemAgentMessage,
			FromStatus: &fromStatus, ToStatus: model.AgentTimelineItemInProgress,
			SourceEventSequence: sequence, ContentJSON: content, AllowInsert: input.Started,
		}
		return persistAgentTimelineMutation(tx, input.Scope, mutation, &nextOrdinal, input.Now)
	})
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func (r *Repository) FailAgentMessageStream(input FailAgentMessageStreamInput) (*model.AgentRunEvent, error) {
	if err := input.Scope.Validate(); err != nil || input.ItemID == "" || input.Message == "" ||
		input.FailureCode == "" || len(input.FailureCode) > 80 || input.Now.IsZero() {
		return nil, errors.Join(ErrAgentTimelineConflict, errors.New("agent message failure boundary is invalid"), err)
	}
	payload, err := json.Marshal(agentMessageFailurePayload{ItemID: input.ItemID, Message: input.Message, FailureCode: input.FailureCode})
	if err != nil {
		return nil, err
	}
	content, err := marshalAgentTimelineContent(agentMessageFailureContent{Message: input.Message, FailureCode: input.FailureCode})
	if err != nil {
		return nil, err
	}
	var event model.AgentRunEvent
	err = r.db.Transaction(func(tx *gorm.DB) error {
		if writableErr := ensureAgentMessageStreamWritable(tx, input.Scope); writableErr != nil {
			return writableErr
		}
		sequence, allocateErr := allocateAgentEventSequence(tx, input.Scope, input.Now)
		if allocateErr != nil {
			return allocateErr
		}
		event = model.AgentRunEvent{
			ID: agentFactID("event", input.Scope.RunID, strconv.FormatInt(sequence, 10)), RunID: input.Scope.RunID,
			Sequence: sequence, Kind: agentruntime.EventAgentMessageFailed, PayloadJSON: string(payload), CreatedAt: input.Now,
		}
		if createErr := tx.Create(&event).Error; createErr != nil {
			return createErr
		}
		nextOrdinal, ordinalErr := nextAgentTimelineOrdinal(tx, input.Scope.RunID)
		if ordinalErr != nil {
			return ordinalErr
		}
		fromStatus := model.AgentTimelineItemInProgress
		return persistAgentTimelineMutation(tx, input.Scope, TimelineMutation{
			ItemID: input.ItemID, Kind: model.AgentTimelineItemAgentMessage,
			FromStatus: &fromStatus, ToStatus: model.AgentTimelineItemFailed,
			SourceEventSequence: sequence, ContentJSON: content,
		}, &nextOrdinal, input.Now)
	})
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func ensureAgentMessageStreamWritable(db *gorm.DB, scope agentruntime.Scope) error {
	state, err := loadAgentCheckpointForScope(db, scope, true)
	if err != nil {
		return err
	}
	if state.Status == agentruntime.RunSucceeded || state.Status == agentruntime.RunFailed || state.Status == agentruntime.RunCancelled {
		return ErrAgentMessageStreamClosed
	}
	return nil
}

func nextAgentTimelineOrdinal(db *gorm.DB, runID string) (int64, error) {
	var latest model.AgentTimelineItem
	err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("run_id = ?", runID).
		Order("ordinal DESC").
		Take(&latest).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	return latest.Ordinal + 1, nil
}

func persistAgentTimelineEvent(
	db *gorm.DB,
	scope agentruntime.Scope,
	previous agentruntime.RuntimeState,
	state agentruntime.RuntimeState,
	kind agentruntime.EventKind,
	sequence int64,
	nextOrdinal *int64,
	now time.Time,
) error {
	if now.IsZero() || nextOrdinal == nil || *nextOrdinal < 1 || sequence < 1 {
		return errors.Join(ErrAgentTimelineConflict, errors.New("agent timeline event boundary is invalid"))
	}
	if kind == agentruntime.EventRunInterrupted && previous.PendingClarification == nil && previous.PendingToolCall == nil {
		mutation, found, err := activeAgentTimelineInterruptMutation(db, scope, sequence)
		if err != nil {
			return err
		}
		if found {
			return persistAgentTimelineMutation(db, scope, mutation, nextOrdinal, now)
		}
	}
	mutation, err := agentTimelineMutationForEvent(scope.RunID, previous, state, kind, sequence)
	if err != nil || mutation == nil {
		return err
	}
	return persistAgentTimelineMutation(db, scope, *mutation, nextOrdinal, now)
}

func activeAgentTimelineInterruptMutation(db *gorm.DB, scope agentruntime.Scope, sequence int64) (TimelineMutation, bool, error) {
	var items []model.AgentTimelineItem
	if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("run_id = ? AND status = ?", scope.RunID, model.AgentTimelineItemInProgress).
		Order("ordinal DESC").
		Limit(2).
		Find(&items).Error; err != nil {
		return TimelineMutation{}, false, err
	}
	if len(items) > 1 {
		return TimelineMutation{}, false, errors.Join(ErrAgentTimelineConflict, errors.New("agent interrupt has multiple active timeline items"))
	}
	if len(items) == 0 {
		return TimelineMutation{}, false, nil
	}
	item := items[0]
	content, err := interruptedAgentTimelineContent(item)
	if err != nil {
		return TimelineMutation{}, false, err
	}
	fromStatus := model.AgentTimelineItemInProgress
	return TimelineMutation{
		ItemID: item.ID, Kind: item.Kind, FromStatus: &fromStatus, ToStatus: model.AgentTimelineItemInterrupted,
		SourceEventSequence: sequence, ContentJSON: content,
	}, true, nil
}

func interruptedAgentTimelineContent(item model.AgentTimelineItem) (json.RawMessage, error) {
	stored := json.RawMessage(item.ContentJSON)
	if item.Kind != model.AgentTimelineItemToolCall {
		return stored, nil
	}
	var envelope struct {
		ContentType string `json:"contentType"`
	}
	if err := json.Unmarshal(stored, &envelope); err != nil || envelope.ContentType != agentruntime.MediaAssemblyContentType {
		return stored, nil
	}
	content, err := agentruntime.DecodeMediaAssemblyTimelineContent(stored)
	if err != nil || (content.TaskStatus != agentruntime.MediaAssemblyTaskQueued && content.TaskStatus != agentruntime.MediaAssemblyTaskRunning) {
		return nil, errors.Join(ErrAgentTimelineConflict, errors.New("agent media assembly interrupt facts are invalid"))
	}
	content.TaskStatus = agentruntime.MediaAssemblyTaskCancelled
	content.Stage = agentRunInterruptedStage
	content.Final = nil
	content.ErrorCode = "media_assembly_cancelled"
	encoded, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func persistAgentTimelineMutation(
	db *gorm.DB,
	scope agentruntime.Scope,
	mutation TimelineMutation,
	nextOrdinal *int64,
	now time.Time,
) error {
	if now.IsZero() || nextOrdinal == nil || *nextOrdinal < 1 {
		return errors.Join(ErrAgentTimelineConflict, errors.New("agent timeline mutation boundary is invalid"))
	}
	if mutation.ItemID == "" || !mutation.Kind.Valid() || !mutation.ToStatus.Valid() ||
		mutation.SourceEventSequence < 1 || !json.Valid(mutation.ContentJSON) || len(mutation.ContentJSON) > agentEventPayloadLimit {
		return errors.Join(ErrAgentTimelineConflict, errors.New("agent timeline mutation is invalid"))
	}
	var existing model.AgentTimelineItem
	err := db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", mutation.ItemID).Take(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if mutation.FromStatus != nil && !mutation.AllowInsert {
			return errors.Join(ErrAgentTimelineConflict, errors.New("agent timeline insert has a source status"))
		}
		expectedOrdinal, ordinalErr := nextAgentTimelineOrdinal(db, scope.RunID)
		if ordinalErr != nil {
			return ordinalErr
		}
		if *nextOrdinal != expectedOrdinal {
			return errors.Join(ErrAgentTimelineConflict, errors.New("agent timeline ordinal is not contiguous"))
		}
		completedAt := agentTimelineCompletedAt(mutation.ToStatus, now)
		item := model.AgentTimelineItem{
			ID: mutation.ItemID, TenantKind: scope.TenantKind, TenantID: scope.TenantID,
			ThreadID: scope.ThreadID, RunID: scope.RunID,
			Kind: mutation.Kind, Status: mutation.ToStatus,
			Ordinal: *nextOrdinal, SourceEventSequence: mutation.SourceEventSequence, ContentJSON: string(mutation.ContentJSON),
			StartedAt: now, CompletedAt: completedAt, CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(&item).Error; err != nil {
			if isUniqueConstraintError(err) {
				return errors.Join(ErrAgentTimelineConflict, err)
			}
			return err
		}
		*nextOrdinal++
		return nil
	}
	if err != nil {
		return err
	}
	if existing.TenantKind != scope.TenantKind || existing.TenantID != scope.TenantID ||
		existing.ThreadID != scope.ThreadID || existing.RunID != scope.RunID || existing.Kind != mutation.Kind ||
		existing.SourceEventSequence >= mutation.SourceEventSequence || agentTimelineStatusTerminal(existing.Status) ||
		mutation.FromStatus == nil || existing.Status != *mutation.FromStatus {
		return errors.Join(ErrAgentTimelineConflict, errors.New("agent timeline mutation conflicts with stored facts"))
	}
	updates := struct {
		Status              model.AgentTimelineItemStatus `gorm:"column:status"`
		SourceEventSequence int64                         `gorm:"column:source_event_sequence"`
		ContentJSON         string                        `gorm:"column:content_json"`
		CompletedAt         *time.Time                    `gorm:"column:completed_at"`
		UpdatedAt           time.Time                     `gorm:"column:updated_at"`
	}{
		Status: mutation.ToStatus, SourceEventSequence: mutation.SourceEventSequence, ContentJSON: string(mutation.ContentJSON),
		CompletedAt: agentTimelineCompletedAt(mutation.ToStatus, now), UpdatedAt: now,
	}
	result := db.Model(&model.AgentTimelineItem{}).
		Where("id = ? AND source_event_sequence = ? AND status = ?", existing.ID, existing.SourceEventSequence, existing.Status).
		Select("status", "source_event_sequence", "content_json", "completed_at", "updated_at").
		Updates(updates)
	if result.Error != nil {
		if isUniqueConstraintError(result.Error) {
			return errors.Join(ErrAgentTimelineConflict, result.Error)
		}
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.Join(ErrAgentTimelineConflict, errors.New("agent timeline mutation lost its compare-and-swap fence"))
	}
	return nil
}

func agentTimelineCompletedAt(status model.AgentTimelineItemStatus, now time.Time) *time.Time {
	if !agentTimelineStatusTerminal(status) {
		return nil
	}
	completedAt := now
	return &completedAt
}

func agentTimelineStatusTerminal(status model.AgentTimelineItemStatus) bool {
	return status == model.AgentTimelineItemCompleted || status == model.AgentTimelineItemFailed ||
		status == model.AgentTimelineItemDeclined || status == model.AgentTimelineItemInterrupted
}

func agentTimelineMutationForEvent(
	runID string,
	previous agentruntime.RuntimeState,
	state agentruntime.RuntimeState,
	kind agentruntime.EventKind,
	sequence int64,
) (*TimelineMutation, error) {
	statusContent := func(status agentruntime.RunStatus) (json.RawMessage, error) {
		return marshalAgentTimelineContent(struct {
			Status agentruntime.RunStatus `json:"status"`
		}{Status: status})
	}
	toolIdentity := func(call *agentruntime.ToolCallDecision) string {
		if call == nil {
			return ""
		}
		return call.ToolCallID + ":" + strconv.Itoa(call.ActionVersion)
	}

	switch kind {
	case agentruntime.EventAgentMessageCompleted:
		content, err := marshalAgentTimelineContent(struct {
			Message string `json:"message"`
		}{Message: state.FinalMessage})
		if err != nil {
			return nil, err
		}
		fromStatus := model.AgentTimelineItemInProgress
		return &TimelineMutation{
			ItemID: agentruntime.AgentMessageItemID(runID, state.StepNumber-1),
			Kind:   model.AgentTimelineItemAgentMessage, FromStatus: &fromStatus, ToStatus: model.AgentTimelineItemCompleted,
			SourceEventSequence: sequence, ContentJSON: content, AllowInsert: true,
		}, nil
	case agentruntime.EventRunInterrupted:
		if previous.PendingClarification != nil {
			content, err := marshalAgentTimelineContent(struct {
				RequestID   string `json:"requestId"`
				Interrupted bool   `json:"interrupted"`
			}{RequestID: previous.PendingClarification.Request.RequestID, Interrupted: true})
			if err != nil {
				return nil, err
			}
			fromStatus := model.AgentTimelineItemInProgress
			return &TimelineMutation{
				ItemID: agentFactID("timeline", runID, "clarification", previous.PendingClarification.Request.RequestID),
				Kind:   model.AgentTimelineItemClarification, FromStatus: &fromStatus, ToStatus: model.AgentTimelineItemInterrupted,
				SourceEventSequence: sequence, ContentJSON: content,
			}, nil
		}
		pendingToolResolved := previous.PendingToolCall != nil && state.LastToolResult != nil &&
			state.LastToolResult.ToolCallID == previous.PendingToolCall.ToolCallID &&
			state.LastToolResult.ActionVersion == previous.PendingToolCall.ActionVersion
		if previous.PendingToolCall != nil && !pendingToolResolved {
			content, err := marshalAgentTimelineContent(struct {
				ToolCallID    string `json:"toolCallId"`
				ActionVersion int    `json:"actionVersion"`
				Interrupted   bool   `json:"interrupted"`
			}{ToolCallID: previous.PendingToolCall.ToolCallID, ActionVersion: previous.PendingToolCall.ActionVersion, Interrupted: true})
			if err != nil {
				return nil, err
			}
			fromStatus := model.AgentTimelineItemInProgress
			return &TimelineMutation{
				ItemID: agentFactID("timeline", runID, "tool-call", toolIdentity(previous.PendingToolCall)),
				Kind:   model.AgentTimelineItemToolCall, FromStatus: &fromStatus, ToStatus: model.AgentTimelineItemInterrupted,
				SourceEventSequence: sequence, ContentJSON: content,
			}, nil
		}
		content, err := statusContent(state.Status)
		if err != nil {
			return nil, err
		}
		return &TimelineMutation{
			ItemID: agentFactID("timeline", runID, "status", strconv.FormatInt(sequence, 10)),
			Kind:   model.AgentTimelineItemStatusKind, ToStatus: model.AgentTimelineItemInterrupted,
			SourceEventSequence: sequence, ContentJSON: content,
		}, nil
	case agentruntime.EventRunFailed:
		if previous.PendingClarification != nil {
			content, err := marshalAgentTimelineContent(struct {
				RequestID string `json:"requestId"`
				ErrorCode string `json:"errorCode"`
			}{RequestID: previous.PendingClarification.Request.RequestID, ErrorCode: state.FailureCode})
			if err != nil {
				return nil, err
			}
			fromStatus := model.AgentTimelineItemInProgress
			return &TimelineMutation{
				ItemID: agentFactID("timeline", runID, "clarification", previous.PendingClarification.Request.RequestID),
				Kind:   model.AgentTimelineItemClarification, FromStatus: &fromStatus, ToStatus: model.AgentTimelineItemFailed,
				SourceEventSequence: sequence, ContentJSON: content,
			}, nil
		}
		content, err := statusContent(state.Status)
		if err != nil {
			return nil, err
		}
		return &TimelineMutation{
			ItemID: agentFactID("timeline", runID, "status", strconv.FormatInt(sequence, 10)),
			Kind:   model.AgentTimelineItemStatusKind, ToStatus: model.AgentTimelineItemFailed,
			SourceEventSequence: sequence, ContentJSON: content,
		}, nil
	case agentruntime.EventRunStatusChanged, agentruntime.EventRunCompleted:
		content, err := statusContent(state.Status)
		if err != nil {
			return nil, err
		}
		itemStatus := model.AgentTimelineItemCompleted
		return &TimelineMutation{
			ItemID: agentFactID("timeline", runID, "status", strconv.FormatInt(sequence, 10)),
			Kind:   model.AgentTimelineItemStatusKind, ToStatus: itemStatus,
			SourceEventSequence: sequence, ContentJSON: content,
		}, nil
	case agentruntime.EventRunSteered:
		if len(state.PendingSteers) == 0 {
			return nil, errors.New("agent steer timeline facts are missing")
		}
		steer := state.PendingSteers[len(state.PendingSteers)-1]
		content, err := marshalAgentTimelineContent(steer)
		if err != nil {
			return nil, err
		}
		return &TimelineMutation{
			ItemID: agentFactID("timeline", runID, "steer", steer.ClientRequestID),
			Kind:   model.AgentTimelineItemUserMessage, ToStatus: model.AgentTimelineItemCompleted,
			SourceEventSequence: sequence, ContentJSON: content,
		}, nil
	case agentruntime.EventClarificationRequested:
		if state.PendingClarification == nil {
			return nil, errors.New("agent clarification timeline facts are missing")
		}
		content, err := marshalAgentTimelineContent(state.PendingClarification)
		if err != nil {
			return nil, err
		}
		return &TimelineMutation{
			ItemID: agentFactID("timeline", runID, "clarification", state.PendingClarification.Request.RequestID),
			Kind:   model.AgentTimelineItemClarification, ToStatus: model.AgentTimelineItemInProgress,
			SourceEventSequence: sequence, ContentJSON: content,
		}, nil
	case agentruntime.EventClarificationAnswerSaved:
		var content json.RawMessage
		var requestID string
		var err error
		if state.PendingClarification != nil {
			requestID = state.PendingClarification.Request.RequestID
			content, err = marshalAgentTimelineContent(state.PendingClarification)
		} else if len(state.ClarificationHistory) > 0 {
			completed := state.ClarificationHistory[len(state.ClarificationHistory)-1]
			requestID = completed.Request.RequestID
			content, err = marshalAgentTimelineContent(completed)
		} else {
			return nil, errors.New("agent clarification answer timeline facts are missing")
		}
		if err != nil {
			return nil, err
		}
		fromStatus := model.AgentTimelineItemInProgress
		return &TimelineMutation{
			ItemID: agentFactID("timeline", runID, "clarification", requestID),
			Kind:   model.AgentTimelineItemClarification, FromStatus: &fromStatus, ToStatus: model.AgentTimelineItemInProgress,
			SourceEventSequence: sequence, ContentJSON: content,
		}, nil
	case agentruntime.EventClarificationResponded:
		if len(state.ClarificationHistory) == 0 {
			return nil, errors.New("agent clarification response timeline facts are missing")
		}
		completed := state.ClarificationHistory[len(state.ClarificationHistory)-1]
		content, err := marshalAgentTimelineContent(completed)
		if err != nil {
			return nil, err
		}
		fromStatus := model.AgentTimelineItemInProgress
		return &TimelineMutation{
			ItemID: agentFactID("timeline", runID, "clarification", completed.Request.RequestID),
			Kind:   model.AgentTimelineItemClarification, FromStatus: &fromStatus, ToStatus: model.AgentTimelineItemCompleted,
			SourceEventSequence: sequence, ContentJSON: content,
		}, nil
	case agentruntime.EventToolCall:
		if state.PendingToolCall == nil {
			return nil, errors.New("agent tool call timeline facts are missing")
		}
		content, err := marshalAgentTimelineContent(struct {
			ToolCallID    string                `json:"toolCallId"`
			ToolName      agentruntime.ToolName `json:"toolName"`
			ActionVersion int                   `json:"actionVersion"`
		}{ToolCallID: state.PendingToolCall.ToolCallID, ToolName: state.PendingToolCall.ToolName, ActionVersion: state.PendingToolCall.ActionVersion})
		if err != nil {
			return nil, err
		}
		return &TimelineMutation{
			ItemID: agentFactID("timeline", runID, "tool-call", toolIdentity(state.PendingToolCall)),
			Kind:   model.AgentTimelineItemToolCall, ToStatus: model.AgentTimelineItemInProgress,
			SourceEventSequence: sequence, ContentJSON: content,
		}, nil
	case agentruntime.EventToolStarted:
		if state.PendingToolCall == nil || !state.PendingToolStarted {
			return nil, errors.New("agent tool start timeline facts are missing")
		}
		content, err := marshalAgentTimelineContent(struct {
			ToolCallID    string `json:"toolCallId"`
			ActionVersion int    `json:"actionVersion"`
			Started       bool   `json:"started"`
		}{ToolCallID: state.PendingToolCall.ToolCallID, ActionVersion: state.PendingToolCall.ActionVersion, Started: true})
		if err != nil {
			return nil, err
		}
		fromStatus := model.AgentTimelineItemInProgress
		return &TimelineMutation{
			ItemID: agentFactID("timeline", runID, "tool-call", toolIdentity(state.PendingToolCall)),
			Kind:   model.AgentTimelineItemToolCall, FromStatus: &fromStatus, ToStatus: model.AgentTimelineItemInProgress,
			SourceEventSequence: sequence, ContentJSON: content,
		}, nil
	case agentruntime.EventToolResult:
		if state.LastToolResult == nil {
			return nil, errors.New("agent tool result timeline facts are missing")
		}
		if previous.PendingToolCall != nil && previous.PendingToolCall.ToolName == agentruntime.ToolMediaAssemble {
			content, err := agentruntime.DecodeMediaAssemblyTimelineContent(state.LastToolResult.Output)
			if err != nil || content.ToolCallID != state.LastToolResult.ToolCallID ||
				content.ActionVersion != state.LastToolResult.ActionVersion ||
				content.ToolCallID != previous.PendingToolCall.ToolCallID ||
				content.ActionVersion != previous.PendingToolCall.ActionVersion {
				return nil, errors.New("agent media assembly tool result facts are invalid")
			}
			if state.LastToolResult.Succeeded {
				if content.TaskStatus != agentruntime.MediaAssemblyTaskSucceeded || state.LastToolResult.ErrorCode != "" {
					return nil, errors.New("agent media assembly success facts are invalid")
				}
			} else if (content.TaskStatus != agentruntime.MediaAssemblyTaskFailed && content.TaskStatus != agentruntime.MediaAssemblyTaskCancelled) ||
				state.LastToolResult.ErrorCode == "" || state.LastToolResult.ErrorCode != content.ErrorCode {
				return nil, errors.New("agent media assembly failure facts are invalid")
			}
			itemStatus, err := mediaAssemblyTimelineItemStatus(content)
			if err != nil {
				return nil, err
			}
			fromStatus := model.AgentTimelineItemInProgress
			return &TimelineMutation{
				ItemID: agentFactID("timeline", runID, "tool-call", state.LastToolResult.ToolCallID+":"+strconv.Itoa(state.LastToolResult.ActionVersion)),
				Kind:   model.AgentTimelineItemToolCall, FromStatus: &fromStatus, ToStatus: itemStatus,
				SourceEventSequence: sequence, ContentJSON: append(json.RawMessage(nil), state.LastToolResult.Output...),
			}, nil
		}
		var toolName agentruntime.ToolName
		var safeOutput *agentruntime.CanvasApplyOpsResult
		if previous.PendingToolCall != nil {
			toolName = previous.PendingToolCall.ToolName
			if toolName == agentruntime.ToolCanvasApplyOps && state.LastToolResult.Succeeded {
				decoded, err := agentruntime.DecodeCapabilityResult(toolName, state.LastToolResult.Output)
				output, ok := decoded.(agentruntime.CanvasApplyOpsResult)
				if err != nil || !ok {
					return nil, errors.New("agent canvas apply ops timeline output is invalid")
				}
				safeOutput = &output
			}
		}
		content, err := marshalAgentTimelineContent(struct {
			ToolCallID    string                             `json:"toolCallId"`
			ToolName      agentruntime.ToolName              `json:"toolName,omitempty"`
			ActionVersion int                                `json:"actionVersion"`
			Succeeded     bool                               `json:"succeeded"`
			ErrorCode     string                             `json:"errorCode,omitempty"`
			Output        *agentruntime.CanvasApplyOpsResult `json:"output,omitempty"`
		}{
			ToolCallID: state.LastToolResult.ToolCallID, ToolName: toolName, ActionVersion: state.LastToolResult.ActionVersion,
			Succeeded: state.LastToolResult.Succeeded, ErrorCode: state.LastToolResult.ErrorCode, Output: safeOutput,
		})
		if err != nil {
			return nil, err
		}
		itemStatus := model.AgentTimelineItemCompleted
		if state.LastToolResult.ErrorCode == "tool_approval_rejected" {
			itemStatus = model.AgentTimelineItemDeclined
		} else if !state.LastToolResult.Succeeded {
			itemStatus = model.AgentTimelineItemFailed
		}
		if previous.PendingToolCall == nil {
			return &TimelineMutation{
				ItemID: agentFactID("timeline", runID, "tool-result", state.LastToolResult.ToolCallID, strconv.Itoa(state.LastToolResult.ActionVersion), strconv.FormatInt(sequence, 10)),
				Kind:   model.AgentTimelineItemToolResult, ToStatus: itemStatus,
				SourceEventSequence: sequence, ContentJSON: content,
			}, nil
		}
		fromStatus := model.AgentTimelineItemInProgress
		return &TimelineMutation{
			ItemID: agentFactID("timeline", runID, "tool-call", state.LastToolResult.ToolCallID+":"+strconv.Itoa(state.LastToolResult.ActionVersion)),
			Kind:   model.AgentTimelineItemToolCall, FromStatus: &fromStatus, ToStatus: itemStatus,
			SourceEventSequence: sequence, ContentJSON: content,
		}, nil
	case agentruntime.EventApprovalRequired:
		if state.PendingToolCall == nil {
			return nil, errors.New("agent approval timeline facts are missing")
		}
		content, err := marshalAgentTimelineContent(struct {
			ToolCallID    string `json:"toolCallId"`
			ActionVersion int    `json:"actionVersion"`
		}{ToolCallID: state.PendingToolCall.ToolCallID, ActionVersion: state.PendingToolCall.ActionVersion})
		if err != nil {
			return nil, err
		}
		fromStatus := model.AgentTimelineItemInProgress
		return &TimelineMutation{
			ItemID: agentFactID("timeline", runID, "tool-call", toolIdentity(state.PendingToolCall)),
			Kind:   model.AgentTimelineItemToolCall, FromStatus: &fromStatus, ToStatus: model.AgentTimelineItemInProgress,
			SourceEventSequence: sequence, ContentJSON: content,
		}, nil
	case agentruntime.EventApprovalDecided:
		call := previous.PendingToolCall
		if call == nil {
			return nil, errors.New("agent approval decision timeline facts are missing")
		}
		decision := agentruntime.ToolApprovalApproved
		if state.LastToolResult != nil && state.LastToolResult.ErrorCode == "tool_approval_rejected" {
			decision = agentruntime.ToolApprovalRejected
		}
		content, err := marshalAgentTimelineContent(struct {
			ToolCallID    string                            `json:"toolCallId"`
			ActionVersion int                               `json:"actionVersion"`
			Decision      agentruntime.ToolApprovalDecision `json:"decision"`
		}{ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion, Decision: decision})
		if err != nil {
			return nil, err
		}
		fromStatus := model.AgentTimelineItemInProgress
		return &TimelineMutation{
			ItemID: agentFactID("timeline", runID, "tool-call", toolIdentity(call)),
			Kind:   model.AgentTimelineItemToolCall, FromStatus: &fromStatus, ToStatus: model.AgentTimelineItemInProgress,
			SourceEventSequence: sequence, ContentJSON: content,
		}, nil
	case agentruntime.EventModelRejected:
		if state.DecisionFeedback == nil {
			return nil, errors.New("agent model rejection timeline facts are missing")
		}
		content, err := marshalAgentTimelineContent(struct {
			Code   string `json:"code"`
			Reason string `json:"reason"`
		}{Code: state.DecisionFeedback.Code, Reason: state.DecisionFeedback.Reason})
		if err != nil {
			return nil, err
		}
		return &TimelineMutation{
			ItemID: agentFactID("timeline", runID, "error", strconv.FormatInt(sequence, 10)),
			Kind:   model.AgentTimelineItemError, ToStatus: model.AgentTimelineItemFailed,
			SourceEventSequence: sequence, ContentJSON: content,
		}, nil
	case agentruntime.EventRunCreated, agentruntime.EventUserMessageAdded, agentruntime.EventModelDelta,
		agentruntime.EventCheckpointSaved, agentruntime.EventArtifactAvailable:
		return nil, nil
	default:
		return nil, fmt.Errorf("agent timeline event kind is unsupported: %s", kind)
	}
}

func marshalAgentTimelineContent(value interface{}) (json.RawMessage, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if !json.Valid(payload) || len(payload) > agentEventPayloadLimit {
		return nil, ErrAgentPayloadTooLarge
	}
	return json.RawMessage(payload), nil
}
