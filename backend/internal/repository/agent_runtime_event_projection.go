package repository

import (
	"encoding/json"
	"errors"
	"strconv"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

func (r *Repository) agentCheckpointStatesByVersion(runID string) (map[int]agentruntime.RuntimeState, error) {
	var checkpoints []model.AgentCheckpoint
	if err := r.db.Where("run_id = ?", runID).Order("state_version ASC, sequence ASC").Find(&checkpoints).Error; err != nil {
		return nil, err
	}
	states := make(map[int]agentruntime.RuntimeState, len(checkpoints))
	canonicalStates := make(map[int]string, len(checkpoints))
	for _, checkpoint := range checkpoints {
		var state agentruntime.RuntimeState
		if checkpoint.RunID != runID || checkpoint.StateVersion < 1 || checkpoint.Sequence < 1 ||
			json.Unmarshal([]byte(checkpoint.StateJSON), &state) != nil || state.StateVersion != checkpoint.StateVersion || !state.Status.Valid() {
			return nil, errors.New("agent checkpoint facts are inconsistent for event projection")
		}
		canonical, err := json.Marshal(state)
		if err != nil {
			return nil, err
		}
		if existing, exists := canonicalStates[state.StateVersion]; exists && existing != string(canonical) {
			return nil, errors.New("agent checkpoint state version is ambiguous for event projection")
		}
		states[state.StateVersion] = state
		canonicalStates[state.StateVersion] = string(canonical)
	}
	return states, nil
}

func agentTimelineMutationForStoredEvent(
	runID string,
	event model.AgentRunEvent,
	statesByVersion map[int]agentruntime.RuntimeState,
) (*TimelineMutation, error) {
	switch event.Kind {
	case agentruntime.EventUserMessageAdded:
		var payload agentHistoryUserMessagePayload
		if json.Unmarshal([]byte(event.PayloadJSON), &payload) != nil || payload.ClientRequestID == "" || payload.Message == "" {
			return nil, errors.New("agent user message event facts are invalid")
		}
		return &TimelineMutation{
			ItemID: agentFactID("timeline", runID, payload.ClientRequestID), Kind: model.AgentTimelineItemUserMessage,
			ToStatus: model.AgentTimelineItemCompleted, SourceEventSequence: event.Sequence,
			ContentJSON: json.RawMessage(event.PayloadJSON),
		}, nil
	case agentruntime.EventArtifactAvailable:
		return agentArtifactTimelineMutation(runID, event)
	case agentruntime.EventApprovalDecided:
		mutation, matched, err := agentStageReviewTimelineMutation(runID, event)
		if err != nil || matched {
			return mutation, err
		}
		return agentRuntimeStateTimelineMutation(runID, event, statesByVersion)
	case agentruntime.EventAgentMessageFailed:
		var payload agentMessageFailurePayload
		if json.Unmarshal([]byte(event.PayloadJSON), &payload) != nil || payload.ItemID == "" || payload.Message == "" ||
			payload.FailureCode == "" || len(payload.FailureCode) > 80 {
			return nil, errors.New("agent message failure event facts are invalid")
		}
		content, err := marshalAgentTimelineContent(agentMessageFailureContent{Message: payload.Message, FailureCode: payload.FailureCode})
		if err != nil {
			return nil, err
		}
		fromStatus := model.AgentTimelineItemInProgress
		return &TimelineMutation{
			ItemID: payload.ItemID, Kind: model.AgentTimelineItemAgentMessage,
			FromStatus: &fromStatus, ToStatus: model.AgentTimelineItemFailed,
			SourceEventSequence: event.Sequence, ContentJSON: content,
		}, nil
	case agentruntime.EventModelDelta:
		return nil, nil
	default:
		return agentRuntimeStateTimelineMutation(runID, event, statesByVersion)
	}
}

func agentRuntimeStateTimelineMutation(
	runID string,
	event model.AgentRunEvent,
	statesByVersion map[int]agentruntime.RuntimeState,
) (*TimelineMutation, error) {
	var state agentruntime.RuntimeState
	if json.Unmarshal([]byte(event.PayloadJSON), &state) != nil || state.StateVersion < 1 || !state.Status.Valid() {
		return nil, errors.New("agent runtime event state is invalid for projection")
	}
	var previous agentruntime.RuntimeState
	if state.StateVersion > 1 {
		var ok bool
		previous, ok = statesByVersion[state.StateVersion-1]
		if !ok {
			return nil, errors.New("agent runtime predecessor checkpoint is missing for event projection")
		}
	}
	return agentTimelineMutationForEvent(runID, previous, state, event.Kind, event.Sequence)
}

func agentStageReviewTimelineMutation(runID string, event model.AgentRunEvent) (*TimelineMutation, bool, error) {
	var envelope struct {
		ContentType string `json:"contentType"`
	}
	if err := json.Unmarshal([]byte(event.PayloadJSON), &envelope); err != nil {
		return nil, false, errors.New("agent stage review event facts are invalid")
	}
	if envelope.ContentType == "" {
		return nil, false, nil
	}
	if envelope.ContentType != agentruntime.StageReviewContentType {
		return nil, false, errors.New("agent stage review event content type is invalid")
	}
	content, err := agentruntime.DecodeStageReviewResolutionContent([]byte(event.PayloadJSON))
	if err != nil {
		return nil, false, errors.New("agent stage review event facts are invalid")
	}
	return &TimelineMutation{
		ItemID: agentStageReviewTimelineItemID(runID, content.StageID, content.RevisionID),
		Kind:   model.AgentTimelineItemApproval, ToStatus: model.AgentTimelineItemCompleted,
		SourceEventSequence: event.Sequence, ContentJSON: json.RawMessage(event.PayloadJSON),
	}, true, nil
}

func agentArtifactTimelineMutation(runID string, event model.AgentRunEvent) (*TimelineMutation, error) {
	var envelope struct {
		ContentType string `json:"contentType"`
	}
	if err := json.Unmarshal([]byte(event.PayloadJSON), &envelope); err != nil {
		return nil, errors.New("agent artifact event facts are invalid")
	}
	if envelope.ContentType == agentruntime.ArtifactReviewContentType {
		content, err := agentruntime.DecodeArtifactReviewContent([]byte(event.PayloadJSON))
		if err != nil {
			return nil, errors.New("agent artifact review event facts are invalid")
		}
		return &TimelineMutation{
			ItemID: agentArtifactReviewTimelineItemID(runID, content), Kind: model.AgentTimelineItemArtifact,
			ToStatus: model.AgentTimelineItemCompleted, SourceEventSequence: event.Sequence,
			ContentJSON: json.RawMessage(event.PayloadJSON),
		}, nil
	}
	if envelope.ContentType == agentruntime.MediaAssemblyContentType {
		content, err := agentruntime.DecodeMediaAssemblyTimelineContent([]byte(event.PayloadJSON))
		if err != nil {
			return nil, errors.New("agent media assembly event facts are invalid")
		}
		status, err := mediaAssemblyTimelineItemStatus(content)
		if err != nil {
			return nil, errors.New("agent media assembly event status is invalid")
		}
		fromStatus := model.AgentTimelineItemInProgress
		return &TimelineMutation{
			ItemID: agentFactID("timeline", runID, "tool-call", content.ToolCallID+":"+strconv.Itoa(content.ActionVersion)),
			Kind:   model.AgentTimelineItemToolCall, FromStatus: &fromStatus, ToStatus: status,
			SourceEventSequence: event.Sequence, ContentJSON: json.RawMessage(event.PayloadJSON),
		}, nil
	}
	if envelope.ContentType != "" {
		return nil, errors.New("agent artifact event content type is invalid")
	}
	var payload agentHistoryArtifactPayload
	if json.Unmarshal([]byte(event.PayloadJSON), &payload) != nil || payload.ArtifactID == "" ||
		payload.PlanKey == "" || payload.PlanVersion < 1 || payload.ResourceID == "" || !payload.Status.Valid() {
		return nil, errors.New("agent artifact event facts are invalid")
	}
	return &TimelineMutation{
		ItemID: agentFactID("timeline", runID, "artifact", payload.ArtifactID), Kind: model.AgentTimelineItemArtifact,
		ToStatus: model.AgentTimelineItemCompleted, SourceEventSequence: event.Sequence,
		ContentJSON: json.RawMessage(event.PayloadJSON),
	}, nil
}

func (r *Repository) agentTimelineProjectionItems(
	scope agentruntime.Scope,
	itemIDs []string,
	deltaSequences []int64,
) (map[string]model.AgentTimelineItem, map[int64]model.AgentTimelineItem, error) {
	items := make([]model.AgentTimelineItem, 0, len(itemIDs)+len(deltaSequences))
	deltaSequenceSet := make(map[int64]struct{}, len(deltaSequences))
	for _, sequence := range deltaSequences {
		deltaSequenceSet[sequence] = struct{}{}
	}
	if len(itemIDs) > 0 {
		var stored []model.AgentTimelineItem
		if err := r.db.Where("run_id = ? AND id IN ?", scope.RunID, itemIDs).Find(&stored).Error; err != nil {
			return nil, nil, err
		}
		items = append(items, stored...)
	}
	if len(deltaSequences) > 0 {
		var stored []model.AgentTimelineItem
		if err := r.db.Where("run_id = ? AND source_event_sequence IN ?", scope.RunID, deltaSequences).Find(&stored).Error; err != nil {
			return nil, nil, err
		}
		items = append(items, stored...)
	}
	itemsByID := make(map[string]model.AgentTimelineItem, len(items))
	itemsBySequence := make(map[int64]model.AgentTimelineItem, len(deltaSequences))
	for _, item := range items {
		if item.TenantKind != scope.TenantKind || item.TenantID != scope.TenantID || item.ThreadID != scope.ThreadID ||
			item.RunID != scope.RunID || !item.Kind.Valid() || !item.Status.Valid() || !json.Valid([]byte(item.ContentJSON)) {
			return nil, nil, errors.New("agent timeline item facts are inconsistent")
		}
		if existing, exists := itemsByID[item.ID]; exists && existing.SourceEventSequence != item.SourceEventSequence {
			return nil, nil, errors.New("agent timeline item identity is ambiguous")
		}
		itemsByID[item.ID] = item
		if _, isDeltaSequence := deltaSequenceSet[item.SourceEventSequence]; isDeltaSequence {
			if _, exists := itemsBySequence[item.SourceEventSequence]; exists {
				return nil, nil, errors.New("agent timeline event has multiple item projections")
			}
			itemsBySequence[item.SourceEventSequence] = item
		}
	}
	return itemsByID, itemsBySequence, nil
}
