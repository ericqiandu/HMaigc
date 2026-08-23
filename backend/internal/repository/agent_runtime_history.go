package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

const maxAgentThreadHistoryLimit = 20

type AgentThreadHistoryRecord struct {
	Thread     model.AgentThread
	Turns      []AgentThreadTurnRecord
	ActivityAt time.Time
}

type AgentThreadTurnRecord struct {
	Run       model.AgentRun
	StateJSON string
	Items     []model.AgentTimelineItem
}

type AgentTimelineEventRecord struct {
	Event model.AgentRunEvent
	Item  *model.AgentTimelineItem
}

func (r *Repository) AgentTimelineEventsAfter(scope agentruntime.Scope, afterSequence int64, limit int) ([]AgentTimelineEventRecord, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	events, err := r.AgentRunEventsAfter(scope, afterSequence, limit)
	if err != nil || len(events) == 0 {
		return nil, err
	}
	for _, event := range events {
		if event.RunID != scope.RunID || event.Sequence <= afterSequence {
			return nil, errors.New("agent timeline event facts are inconsistent")
		}
	}
	statesByVersion, err := r.agentCheckpointStatesByVersion(scope.RunID)
	if err != nil {
		return nil, err
	}
	mutations := make([]*TimelineMutation, len(events))
	itemIDs := make([]string, 0, len(events))
	itemIDSet := make(map[string]struct{}, len(events))
	deltaSequences := make([]int64, 0)
	for index, event := range events {
		if event.Kind == agentruntime.EventModelDelta {
			deltaSequences = append(deltaSequences, event.Sequence)
			continue
		}
		mutation, mutationErr := agentTimelineMutationForStoredEvent(scope.RunID, event, statesByVersion)
		if mutationErr != nil {
			return nil, mutationErr
		}
		mutations[index] = mutation
		if mutation == nil {
			continue
		}
		if _, exists := itemIDSet[mutation.ItemID]; !exists {
			itemIDSet[mutation.ItemID] = struct{}{}
			itemIDs = append(itemIDs, mutation.ItemID)
		}
	}
	itemsByID, itemsBySequence, err := r.agentTimelineProjectionItems(scope, itemIDs, deltaSequences)
	if err != nil {
		return nil, err
	}
	records := make([]AgentTimelineEventRecord, 0, len(events))
	for index, event := range events {
		record := AgentTimelineEventRecord{Event: event}
		if event.Kind == agentruntime.EventModelDelta {
			if item, ok := itemsBySequence[event.Sequence]; ok {
				copied := item
				record.Item = &copied
			}
			records = append(records, record)
			continue
		}
		mutation := mutations[index]
		if mutation != nil {
			stored, ok := itemsByID[mutation.ItemID]
			if !ok || stored.Kind != mutation.Kind || stored.SourceEventSequence < event.Sequence {
				return nil, errors.New("agent timeline materialized item is inconsistent with event history")
			}
			item := stored
			item.Status = mutation.ToStatus
			item.SourceEventSequence = event.Sequence
			item.ContentJSON = string(mutation.ContentJSON)
			item.UpdatedAt = event.CreatedAt
			item.CompletedAt = agentTimelineCompletedAt(mutation.ToStatus, event.CreatedAt)
			copied := item
			record.Item = &copied
		}
		records = append(records, record)
	}
	return records, nil
}

type agentThreadHistoryRow struct {
	model.AgentThread
	ActivityAt time.Time `gorm:"column:activity_at"`
}

func (r *Repository) AgentThreadHistory(scope agentruntime.Scope, limit int) ([]AgentThreadHistoryRecord, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > maxAgentThreadHistoryLimit {
		return nil, errors.New("agent thread history limit is invalid")
	}
	var records []AgentThreadHistoryRecord
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var threads []model.AgentThread
		if err := tx.Where(`tenant_kind = ? AND tenant_id = ? AND created_by_user_id = ?
			AND domain_project_id = ? AND canvas_id = ? AND status = ?`,
			scope.TenantKind, scope.TenantID, scope.ActorUserID, scope.DomainProjectID,
			scope.CanvasID, agentruntime.ThreadActive).
			Order(`COALESCE((SELECT MAX(agent_runs.updated_at) FROM agent_runs
				WHERE agent_runs.thread_id = agent_threads.id), agent_threads.updated_at) DESC, agent_threads.id DESC`).
			Limit(limit).Find(&threads).Error; err != nil {
			return err
		}
		if len(threads) == 0 {
			records = []AgentThreadHistoryRecord{}
			return nil
		}
		threadIDs := make([]string, 0, len(threads))
		for _, thread := range threads {
			threadIDs = append(threadIDs, thread.ID)
		}
		var runs []model.AgentRun
		if err := tx.Where("thread_id IN ?", threadIDs).
			Order("thread_id ASC, created_at ASC, id ASC").Find(&runs).Error; err != nil {
			return err
		}
		runIDs := make([]string, 0, len(runs))
		for _, run := range runs {
			runIDs = append(runIDs, run.ID)
		}
		var checkpoints []model.AgentCheckpoint
		var items []model.AgentTimelineItem
		var events []model.AgentRunEvent
		if len(runIDs) > 0 {
			if err := tx.Where("run_id IN ?", runIDs).Order("run_id ASC, sequence ASC").Find(&checkpoints).Error; err != nil {
				return err
			}
			if err := tx.Where("run_id IN ?", runIDs).Order("run_id ASC, ordinal ASC").Find(&items).Error; err != nil {
				return err
			}
			if err := tx.Where("run_id IN ?", runIDs).Order("run_id ASC, sequence ASC").Find(&events).Error; err != nil {
				return err
			}
		}
		activityByThreadID := make(map[string]time.Time, len(threads))
		for _, thread := range threads {
			activityByThreadID[thread.ID] = thread.UpdatedAt
		}
		for _, run := range runs {
			if run.UpdatedAt.After(activityByThreadID[run.ThreadID]) {
				activityByThreadID[run.ThreadID] = run.UpdatedAt
			}
		}
		rows := make([]agentThreadHistoryRow, 0, len(threads))
		for _, thread := range threads {
			rows = append(rows, agentThreadHistoryRow{AgentThread: thread, ActivityAt: activityByThreadID[thread.ID]})
		}
		var err error
		records, err = agentThreadHistoryFacts(scope, rows, runs, checkpoints, items, events)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return records, err
}

func agentThreadHistoryFacts(
	scope agentruntime.Scope,
	rows []agentThreadHistoryRow,
	runs []model.AgentRun,
	checkpoints []model.AgentCheckpoint,
	items []model.AgentTimelineItem,
	events []model.AgentRunEvent,
) ([]AgentThreadHistoryRecord, error) {
	threadByID := make(map[string]model.AgentThread, len(rows))
	recordIndexByThreadID := make(map[string]int, len(rows))
	records := make([]AgentThreadHistoryRecord, 0, len(rows))
	for _, row := range rows {
		thread := row.AgentThread
		if thread.TenantKind != scope.TenantKind || thread.TenantID != scope.TenantID ||
			thread.CreatedByUserID != scope.ActorUserID || thread.DomainProjectID != scope.DomainProjectID ||
			thread.CanvasID != scope.CanvasID || thread.Status != agentruntime.ThreadActive || row.ActivityAt.IsZero() {
			return nil, errors.New("agent thread history scope facts are inconsistent")
		}
		threadByID[thread.ID] = thread
		recordIndexByThreadID[thread.ID] = len(records)
		records = append(records, AgentThreadHistoryRecord{
			Thread: thread, Turns: []AgentThreadTurnRecord{}, ActivityAt: row.ActivityAt,
		})
	}
	runByID := make(map[string]model.AgentRun, len(runs))
	for _, run := range runs {
		thread, ok := threadByID[run.ThreadID]
		if !ok || run.ActorUserID != scope.ActorUserID || run.UpdatedAt.Before(run.CreatedAt) ||
			run.LastEventSequence < 0 || run.StateVersion < 0 || run.StepNumber < 0 || run.MaxSteps < 0 {
			return nil, errors.New("agent thread history run facts are inconsistent")
		}
		if thread.ID != run.ThreadID {
			return nil, errors.New("agent thread history run scope is inconsistent")
		}
		runByID[run.ID] = run
	}
	checkpointByRunID := make(map[string]model.AgentCheckpoint, len(runs))
	for _, checkpoint := range checkpoints {
		run, ok := runByID[checkpoint.RunID]
		if !ok || checkpoint.Sequence < 1 || checkpoint.Sequence > run.LastEventSequence {
			return nil, errors.New("agent thread history checkpoint facts are inconsistent")
		}
		if checkpoint.StateVersion != run.StateVersion {
			continue
		}
		stored, exists := checkpointByRunID[checkpoint.RunID]
		if !exists || checkpoint.Sequence > stored.Sequence {
			checkpointByRunID[checkpoint.RunID] = checkpoint
		}
	}
	itemsByRunID := make(map[string][]model.AgentTimelineItem, len(runs))
	for _, item := range items {
		run, ok := runByID[item.RunID]
		if !ok || item.ThreadID != run.ThreadID || item.TenantKind != scope.TenantKind ||
			item.TenantID != scope.TenantID || !item.Kind.Valid() || !item.Status.Valid() ||
			item.SourceEventSequence < 1 || item.SourceEventSequence > run.LastEventSequence ||
			!json.Valid([]byte(item.ContentJSON)) {
			return nil, errors.New("agent thread history timeline facts are inconsistent")
		}
		itemsByRunID[item.RunID] = append(itemsByRunID[item.RunID], item)
	}
	eventsByRunID := make(map[string][]model.AgentRunEvent, len(runs))
	for _, event := range events {
		run, ok := runByID[event.RunID]
		if !ok || event.Sequence < 1 || event.Sequence > run.LastEventSequence || !event.Kind.Valid() ||
			!json.Valid([]byte(event.PayloadJSON)) {
			return nil, errors.New("agent thread history event facts are inconsistent")
		}
		eventsByRunID[event.RunID] = append(eventsByRunID[event.RunID], event)
	}
	for _, run := range runs {
		checkpoint, ok := checkpointByRunID[run.ID]
		if !ok {
			return nil, errors.New("agent thread history checkpoint is missing")
		}
		runItems := itemsByRunID[run.ID]
		if len(runItems) == 0 && run.LastEventSequence > 0 {
			if !agentHistoryRunTerminal(run.Status) {
				return nil, errors.New("active agent run timeline projection is missing")
			}
			var rebuildErr error
			runItems, rebuildErr = rebuildTerminalAgentTimeline(scope, run, eventsByRunID[run.ID])
			if rebuildErr != nil {
				return nil, rebuildErr
			}
		}
		for index, item := range runItems {
			if item.Ordinal != int64(index+1) {
				return nil, errors.New("agent thread history timeline ordinal is not contiguous")
			}
		}
		recordIndex := recordIndexByThreadID[run.ThreadID]
		records[recordIndex].Turns = append(records[recordIndex].Turns, AgentThreadTurnRecord{
			Run: run, StateJSON: checkpoint.StateJSON, Items: runItems,
		})
	}
	return records, nil
}

func agentHistoryRunTerminal(status agentruntime.RunStatus) bool {
	return status == agentruntime.RunSucceeded || status == agentruntime.RunFailed || status == agentruntime.RunCancelled
}

type agentHistoryUserMessagePayload struct {
	ClientRequestID string `json:"clientRequestId"`
	Message         string `json:"message"`
}

type agentHistoryArtifactPayload struct {
	ArtifactID     string                              `json:"artifactId"`
	Kind           model.AgentProductionArtifactKind   `json:"kind"`
	PlanKey        string                              `json:"planKey"`
	PlanVersion    int                                 `json:"planVersion"`
	ReferenceKey   string                              `json:"referenceKey,omitempty"`
	ShotKey        string                              `json:"shotKey,omitempty"`
	TaskID         string                              `json:"taskId,omitempty"`
	BillingOrderID string                              `json:"billingOrderId,omitempty"`
	ResourceID     string                              `json:"resourceId,omitempty"`
	Status         model.AgentProductionArtifactStatus `json:"status"`
}

func rebuildTerminalAgentTimeline(scope agentruntime.Scope, run model.AgentRun, events []model.AgentRunEvent) ([]model.AgentTimelineItem, error) {
	if !agentHistoryRunTerminal(run.Status) || int64(len(events)) != run.LastEventSequence {
		return nil, errors.New("terminal agent run event history is incomplete")
	}
	hasUserMessageEvent := false
	for index, event := range events {
		if event.Sequence != int64(index+1) {
			return nil, errors.New("terminal agent run event sequence is not contiguous")
		}
		hasUserMessageEvent = hasUserMessageEvent || event.Kind == agentruntime.EventUserMessageAdded
	}
	items := make([]model.AgentTimelineItem, 0, len(events))
	itemIndexByID := make(map[string]int, len(events))
	var latestState agentruntime.RuntimeState
	var transitionPrevious agentruntime.RuntimeState
	transitionStateVersion := 0
	for _, event := range events {
		var mutation *TimelineMutation
		switch event.Kind {
		case agentruntime.EventUserMessageAdded:
			var payload agentHistoryUserMessagePayload
			if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil || payload.ClientRequestID == "" || payload.Message == "" {
				return nil, errors.New("terminal agent user message facts are invalid")
			}
			mutation = &TimelineMutation{
				ItemID: agentFactID("timeline", run.ID, payload.ClientRequestID), Kind: model.AgentTimelineItemUserMessage,
				ToStatus: model.AgentTimelineItemCompleted, SourceEventSequence: event.Sequence,
				ContentJSON: json.RawMessage(event.PayloadJSON),
			}
		case agentruntime.EventArtifactAvailable:
			var payload agentHistoryArtifactPayload
			if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil || payload.ArtifactID == "" ||
				payload.PlanKey == "" || payload.PlanVersion < 1 || payload.ResourceID == "" || !payload.Status.Valid() {
				return nil, errors.New("terminal agent artifact facts are invalid")
			}
			mutation = &TimelineMutation{
				ItemID: agentFactID("timeline", run.ID, "artifact", payload.ArtifactID), Kind: model.AgentTimelineItemArtifact,
				ToStatus: model.AgentTimelineItemCompleted, SourceEventSequence: event.Sequence,
				ContentJSON: json.RawMessage(event.PayloadJSON),
			}
		case agentruntime.EventModelDelta:
			continue
		default:
			var state agentruntime.RuntimeState
			if err := json.Unmarshal([]byte(event.PayloadJSON), &state); err != nil || state.StateVersion < 1 || !state.Status.Valid() {
				return nil, errors.New("terminal agent runtime event state is invalid")
			}
			if state.StateVersion != transitionStateVersion {
				transitionPrevious = latestState
				transitionStateVersion = state.StateVersion
			}
			if event.Kind == agentruntime.EventRunCreated && !hasUserMessageEvent {
				content, err := json.Marshal(agentHistoryUserMessagePayload{ClientRequestID: run.ClientRequestID, Message: state.UserMessage})
				if err != nil || run.ClientRequestID == "" || state.UserMessage == "" {
					return nil, errors.New("terminal agent initial message facts are invalid")
				}
				mutation = &TimelineMutation{
					ItemID: agentFactID("timeline", run.ID, run.ClientRequestID), Kind: model.AgentTimelineItemUserMessage,
					ToStatus: model.AgentTimelineItemCompleted, SourceEventSequence: event.Sequence, ContentJSON: content,
				}
			} else {
				var err error
				mutation, err = agentTimelineMutationForEvent(run.ID, transitionPrevious, state, event.Kind, event.Sequence)
				if err != nil {
					return nil, err
				}
			}
			latestState = state
		}
		if mutation == nil {
			continue
		}
		if err := applyRebuiltAgentTimelineMutation(scope, run, event.CreatedAt, *mutation, &items, itemIndexByID); err != nil {
			return nil, err
		}
	}
	if latestState.StateVersion != run.StateVersion || latestState.StepNumber != run.StepNumber ||
		latestState.MaxSteps != run.MaxSteps || latestState.Status != run.Status {
		return nil, errors.New("terminal agent run state does not match event history")
	}
	return items, nil
}

func applyRebuiltAgentTimelineMutation(
	scope agentruntime.Scope,
	run model.AgentRun,
	now time.Time,
	mutation TimelineMutation,
	items *[]model.AgentTimelineItem,
	itemIndexByID map[string]int,
) error {
	if mutation.ItemID == "" || !mutation.Kind.Valid() || !mutation.ToStatus.Valid() || mutation.SourceEventSequence < 1 ||
		!json.Valid(mutation.ContentJSON) || now.IsZero() {
		return errors.New("rebuilt agent timeline mutation is invalid")
	}
	index, exists := itemIndexByID[mutation.ItemID]
	if !exists {
		if mutation.FromStatus != nil {
			return errors.New("rebuilt agent timeline update has no source item")
		}
		completedAt := agentTimelineCompletedAt(mutation.ToStatus, now)
		item := model.AgentTimelineItem{
			ID: mutation.ItemID, TenantKind: scope.TenantKind, TenantID: scope.TenantID,
			ThreadID: run.ThreadID, RunID: run.ID, Kind: mutation.Kind, Status: mutation.ToStatus,
			Ordinal: int64(len(*items) + 1), SourceEventSequence: mutation.SourceEventSequence,
			ContentJSON: string(mutation.ContentJSON), StartedAt: now, CompletedAt: completedAt,
			CreatedAt: now, UpdatedAt: now,
		}
		*items = append(*items, item)
		itemIndexByID[mutation.ItemID] = len(*items) - 1
		return nil
	}
	if mutation.FromStatus == nil {
		return errors.New("rebuilt agent timeline item identity is duplicated")
	}
	item := &(*items)[index]
	if item.Kind != mutation.Kind || item.Status != *mutation.FromStatus || agentTimelineStatusTerminal(item.Status) {
		return errors.New("rebuilt agent timeline transition conflicts with prior facts")
	}
	item.Status = mutation.ToStatus
	item.SourceEventSequence = mutation.SourceEventSequence
	item.ContentJSON = string(mutation.ContentJSON)
	item.CompletedAt = agentTimelineCompletedAt(mutation.ToStatus, now)
	item.UpdatedAt = now
	return nil
}
