package repository

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrAgentRuntimeStepConflict = errors.New("agent runtime step conflict")
var ErrAgentRuntimeInitializationConflict = errors.New("agent runtime initialization conflict")
var ErrAgentTimelineConflict = errors.New("agent timeline conflict")

type InitializeAgentRunInput struct {
	Scope             agentruntime.Scope
	ModelRecordID     string
	ModelKey          string
	MaxSteps          int
	ToolSchemaVersion int
	RuntimeVersion    int
	PolicyVersion     int
	UserMessage       string
	Configuration     agentruntime.RunConfiguration
	Now               time.Time
}

type InitializedAgentRun struct {
	Run     model.AgentRun
	Created bool
}

type CreateInitializedAgentRunInput struct {
	Create     CreateAgentRunInput
	Initialize InitializeAgentRunInput
}

func (r *Repository) CreateInitializedAgentRun(input CreateInitializedAgentRunInput) (*InitializedAgentRun, error) {
	var result *InitializedAgentRun
	err := r.db.Transaction(func(tx *gorm.DB) error {
		txRepository := New(tx)
		record, err := txRepository.CreateAgentRun(input.Create)
		if err != nil {
			return err
		}
		if !record.Created {
			if record.Run.StateVersion == 0 || record.Run.MaxSteps == 0 || record.Run.LastEventSequence == 0 {
				return ErrAgentRuntimeInitializationConflict
			}
			result = &InitializedAgentRun{Run: record.Run}
			return nil
		}
		input.Initialize.Scope.RunID = record.Run.ID
		initialized, err := txRepository.InitializeAgentRun(input.Initialize)
		if err != nil {
			return err
		}
		result = initialized
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type agentRunInitializationUpdates struct {
	MaxSteps          int       `gorm:"column:max_steps"`
	ModelRecordID     string    `gorm:"column:model_record_id"`
	ModelKey          string    `gorm:"column:model_key"`
	ToolSchemaVersion int       `gorm:"column:tool_schema_version"`
	RuntimeVersion    int       `gorm:"column:runtime_version"`
	PolicyVersion     int       `gorm:"column:policy_version"`
	LastEventSequence int64     `gorm:"column:last_event_sequence"`
	StateVersion      int       `gorm:"column:state_version"`
	UpdatedAt         time.Time `gorm:"column:updated_at"`
}

func (r *Repository) InitializeAgentRun(input InitializeAgentRunInput) (*InitializedAgentRun, error) {
	input.ModelRecordID = strings.TrimSpace(input.ModelRecordID)
	input.ModelKey = strings.TrimSpace(input.ModelKey)
	input.UserMessage = strings.TrimSpace(input.UserMessage)
	if err := input.Scope.Validate(); err != nil {
		return nil, err
	}
	if input.ModelRecordID == "" || len(input.ModelRecordID) > 80 || input.ModelKey == "" || len(input.ModelKey) > 120 ||
		input.MaxSteps < 1 || input.MaxSteps > 24 || input.ToolSchemaVersion < 1 || input.RuntimeVersion < 1 || input.PolicyVersion < 1 || input.UserMessage == "" || len(input.UserMessage) > 64*1024 || input.Now.IsZero() {
		return nil, errors.New("agent runtime initialization boundary is invalid")
	}
	if err := agentruntime.ValidateRunConfiguration(input.Configuration); err != nil {
		return nil, err
	}
	state := agentruntime.RuntimeState{
		StateVersion: 1, StepNumber: 0, MaxSteps: input.MaxSteps,
		Status: agentruntime.RunQueued, UserMessage: input.UserMessage, Configuration: input.Configuration,
		ClarificationHistory: []agentruntime.CompletedClarification{},
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	if len(stateJSON) > agentEventPayloadLimit {
		return nil, ErrAgentPayloadTooLarge
	}
	result := &InitializedAgentRun{}
	err = r.db.Transaction(func(tx *gorm.DB) error {
		updated := tx.Model(&model.AgentRun{}).
			Where(`id = ? AND thread_id = ? AND actor_user_id = ? AND status = ? AND step_number = 0
				AND state_version = 0
				AND max_steps = 0 AND model_record_id = '' AND model_key = '' AND tool_schema_version = 0
				AND runtime_version = 0 AND policy_version = 0
				AND last_event_sequence = 0 AND EXISTS (
					SELECT 1 FROM agent_threads WHERE agent_threads.id = agent_runs.thread_id
					AND tenant_kind = ? AND tenant_id = ? AND created_by_user_id = ?
					AND domain_project_id = ? AND canvas_id = ?
				)`, input.Scope.RunID, input.Scope.ThreadID, input.Scope.ActorUserID, agentruntime.RunQueued,
				input.Scope.TenantKind, input.Scope.TenantID, input.Scope.ActorUserID, input.Scope.DomainProjectID, input.Scope.CanvasID).
			Updates(agentRunInitializationUpdates{
				MaxSteps: input.MaxSteps, ModelRecordID: input.ModelRecordID, ModelKey: input.ModelKey,
				ToolSchemaVersion: input.ToolSchemaVersion, RuntimeVersion: input.RuntimeVersion, PolicyVersion: input.PolicyVersion,
				LastEventSequence: 2, UpdatedAt: input.Now,
				StateVersion: 1,
			})
		if updated.Error != nil {
			return updated.Error
		}
		var run model.AgentRun
		if err := tx.Table("agent_runs").Select("agent_runs.*").
			Joins("JOIN agent_threads ON agent_threads.id = agent_runs.thread_id").
			Where(`agent_runs.id = ? AND agent_runs.thread_id = ? AND agent_runs.actor_user_id = ?
				AND agent_threads.tenant_kind = ? AND agent_threads.tenant_id = ?
				AND agent_threads.created_by_user_id = ? AND agent_threads.domain_project_id = ?
				AND agent_threads.canvas_id = ?`, input.Scope.RunID, input.Scope.ThreadID, input.Scope.ActorUserID,
				input.Scope.TenantKind, input.Scope.TenantID, input.Scope.ActorUserID, input.Scope.DomainProjectID, input.Scope.CanvasID).
			Take(&run).Error; err != nil {
			return err
		}
		userMessagePayload, err := json.Marshal(struct {
			ClientRequestID string `json:"clientRequestId"`
			Message         string `json:"message"`
		}{ClientRequestID: run.ClientRequestID, Message: input.UserMessage})
		if err != nil {
			return err
		}
		if updated.RowsAffected == 0 {
			if run.StateVersion != 1 || run.MaxSteps != input.MaxSteps || run.ModelRecordID != input.ModelRecordID || run.ModelKey != input.ModelKey || run.ToolSchemaVersion != input.ToolSchemaVersion || run.RuntimeVersion != input.RuntimeVersion || run.PolicyVersion != input.PolicyVersion || run.LastEventSequence != 2 {
				return ErrAgentRuntimeInitializationConflict
			}
			var events []model.AgentRunEvent
			if err := tx.Where("run_id = ? AND sequence IN ?", input.Scope.RunID, []int64{1, 2}).Order("sequence").Find(&events).Error; err != nil {
				return err
			}
			if len(events) != 2 {
				return ErrAgentRuntimeInitializationConflict
			}
			var checkpoint model.AgentCheckpoint
			if err := tx.Where("run_id = ? AND sequence = 2", input.Scope.RunID).Take(&checkpoint).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrAgentRuntimeInitializationConflict
				}
				return err
			}
			var item model.AgentTimelineItem
			if err := tx.Where("run_id = ? AND ordinal = 1", input.Scope.RunID).Take(&item).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrAgentRuntimeInitializationConflict
				}
				return err
			}
			if events[0].Kind != agentruntime.EventRunCreated || events[0].PayloadJSON != string(stateJSON) ||
				events[1].Kind != agentruntime.EventUserMessageAdded || events[1].PayloadJSON != string(userMessagePayload) ||
				checkpoint.StateVersion != 1 || checkpoint.StateJSON != string(stateJSON) ||
				item.TenantKind != input.Scope.TenantKind || item.TenantID != input.Scope.TenantID || item.ThreadID != input.Scope.ThreadID ||
				item.Kind != model.AgentTimelineItemUserMessage || item.Status != model.AgentTimelineItemCompleted ||
				item.SourceEventSequence != 2 || item.ContentJSON != string(userMessagePayload) {
				return ErrAgentRuntimeInitializationConflict
			}
			result.Run = run
			return nil
		}
		createdEvent := model.AgentRunEvent{
			ID: agentFactID("event", input.Scope.RunID, "1"), RunID: input.Scope.RunID, Sequence: 1,
			Kind: agentruntime.EventRunCreated, PayloadJSON: string(stateJSON), CreatedAt: input.Now,
		}
		if err := tx.Create(&createdEvent).Error; err != nil {
			return err
		}
		messageEvent := model.AgentRunEvent{
			ID: agentFactID("event", input.Scope.RunID, "2"), RunID: input.Scope.RunID, Sequence: 2,
			Kind: agentruntime.EventUserMessageAdded, PayloadJSON: string(userMessagePayload), CreatedAt: input.Now,
		}
		if err := tx.Create(&messageEvent).Error; err != nil {
			return err
		}
		checkpoint := model.AgentCheckpoint{
			ID: agentFactID("checkpoint", input.Scope.RunID, "2"), RunID: input.Scope.RunID, Sequence: 2,
			StateVersion: 1, StateJSON: string(stateJSON), CreatedAt: input.Now,
		}
		if err := tx.Create(&checkpoint).Error; err != nil {
			return err
		}
		completedAt := input.Now
		item := model.AgentTimelineItem{
			ID:         agentFactID("timeline", input.Scope.RunID, run.ClientRequestID),
			TenantKind: input.Scope.TenantKind, TenantID: input.Scope.TenantID,
			ThreadID: input.Scope.ThreadID, RunID: input.Scope.RunID,
			Kind: model.AgentTimelineItemUserMessage, Status: model.AgentTimelineItemCompleted,
			Ordinal: 1, SourceEventSequence: 2, ContentJSON: string(userMessagePayload),
			StartedAt: input.Now, CompletedAt: &completedAt, CreatedAt: input.Now, UpdatedAt: input.Now,
		}
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		result.Run = run
		result.Created = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Repository) CommitAgentRuntimeTransition(scope agentruntime.Scope, previous agentruntime.RuntimeState, transition agentruntime.RuntimeTransition, now time.Time) error {
	state := transition.State
	if err := validateAgentRuntimeTransition(scope, previous, transition, now); err != nil {
		return err
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if len(stateJSON) > agentEventPayloadLimit {
		return ErrAgentPayloadTooLarge
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		return r.commitAgentRuntimeTransitionTx(tx, scope, previous, transition, string(stateJSON), now)
	})
}

func (r *Repository) commitAgentRuntimeTransitionTx(
	tx *gorm.DB,
	scope agentruntime.Scope,
	previous agentruntime.RuntimeState,
	transition agentruntime.RuntimeTransition,
	stateJSON string,
	now time.Time,
) error {
	state := transition.State
	var facts struct {
		LastEventSequence int64 `gorm:"column:last_event_sequence"`
	}
	var completedAt *time.Time
	if state.Status == agentruntime.RunSucceeded || state.Status == agentruntime.RunFailed || state.Status == agentruntime.RunCancelled {
		completedAt = &now
	}
	result := tx.Raw(`
			UPDATE agent_runs
			   SET state_version = ?, step_number = ?, status = ?, last_event_sequence = last_event_sequence + ?, updated_at = ?, completed_at = ?
			 WHERE id = ? AND thread_id = ? AND actor_user_id = ? AND state_version = ? AND step_number = ? AND max_steps = ?
			   AND status = ?
			   AND EXISTS (
			       SELECT 1 FROM agent_threads
			        WHERE agent_threads.id = agent_runs.thread_id
			          AND tenant_kind = ? AND tenant_id = ? AND created_by_user_id = ?
			          AND domain_project_id = ? AND canvas_id = ?
			   )
			 RETURNING last_event_sequence`,
		state.StateVersion, state.StepNumber, state.Status, len(transition.EventKinds), now, completedAt,
		scope.RunID, scope.ThreadID, scope.ActorUserID, previous.StateVersion, previous.StepNumber, state.MaxSteps, previous.Status,
		scope.TenantKind, scope.TenantID, scope.ActorUserID, scope.DomainProjectID, scope.CanvasID,
	).Scan(&facts)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		if _, err := r.AgentRunForScope(scope); err != nil {
			return err
		}
		return ErrAgentRuntimeStepConflict
	}
	if err := persistRejectedAgentToolDecision(tx, scope, previous, transition, now); err != nil {
		return err
	}
	if err := persistAgentToolTransition(tx, scope, previous, state, now); err != nil {
		return err
	}
	nextTimelineOrdinal, err := nextAgentTimelineOrdinal(tx, scope.RunID)
	if err != nil {
		return err
	}
	firstSequence := facts.LastEventSequence - int64(len(transition.EventKinds)) + 1
	for index, kind := range transition.EventKinds {
		sequence := firstSequence + int64(index)
		event := model.AgentRunEvent{ID: agentFactID("event", scope.RunID, strconv.FormatInt(sequence, 10)), RunID: scope.RunID, Sequence: sequence, Kind: kind, PayloadJSON: string(stateJSON), CreatedAt: now}
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		if err := persistAgentTimelineEvent(tx, scope, previous, state, kind, sequence, &nextTimelineOrdinal, now); err != nil {
			return err
		}
	}
	checkpoint := model.AgentCheckpoint{ID: agentFactID("checkpoint", scope.RunID, strconv.FormatInt(facts.LastEventSequence, 10)), RunID: scope.RunID, Sequence: facts.LastEventSequence, StateVersion: state.StateVersion, StateJSON: string(stateJSON), CreatedAt: now}
	return tx.Create(&checkpoint).Error
}

func (r *Repository) AppendAgentSteer(
	scope agentruntime.Scope,
	request agentruntime.SteerRequest,
	now time.Time,
) (agentruntime.RuntimeState, bool, error) {
	if err := scope.Validate(); err != nil {
		return agentruntime.RuntimeState{}, false, err
	}
	if now.IsZero() {
		return agentruntime.RuntimeState{}, false, errors.New("agent steer timestamp is required")
	}
	var state agentruntime.RuntimeState
	var replayed bool
	err := r.db.Transaction(func(tx *gorm.DB) error {
		current, err := loadAgentCheckpointForScope(tx, scope, true)
		if err != nil {
			return err
		}
		itemID := agentFactID("timeline", scope.RunID, "steer", strings.TrimSpace(request.ClientRequestID))
		var existing model.AgentTimelineItem
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", itemID).Take(&existing).Error
		if err == nil {
			var stored agentruntime.PendingSteer
			if existing.TenantKind != scope.TenantKind || existing.TenantID != scope.TenantID ||
				existing.ThreadID != scope.ThreadID || existing.RunID != scope.RunID ||
				existing.Kind != model.AgentTimelineItemUserMessage || existing.Status != model.AgentTimelineItemCompleted ||
				json.Unmarshal([]byte(existing.ContentJSON), &stored) != nil ||
				stored.ClientRequestID != strings.TrimSpace(request.ClientRequestID) || stored.Message != strings.TrimSpace(request.Message) {
				return agentruntime.ErrSteerConflict
			}
			state = current
			replayed = true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		transition, domainReplay, err := agentruntime.AppendSteer(current, request)
		if err != nil {
			return err
		}
		if domainReplay {
			return ErrAgentTimelineConflict
		}
		if err := validateAgentRuntimeTransition(scope, current, transition, now); err != nil {
			return err
		}
		stateJSON, err := json.Marshal(transition.State)
		if err != nil {
			return err
		}
		if len(stateJSON) > agentEventPayloadLimit {
			return ErrAgentPayloadTooLarge
		}
		if err := r.commitAgentRuntimeTransitionTx(tx, scope, current, transition, string(stateJSON), now); err != nil {
			return err
		}
		state = transition.State
		return nil
	})
	if err != nil {
		return agentruntime.RuntimeState{}, false, err
	}
	return state, replayed, nil
}

func (r *Repository) InterruptAgentRun(
	scope agentruntime.Scope,
	expectedStateVersion int,
	now time.Time,
) (agentruntime.RuntimeState, error) {
	if err := scope.Validate(); err != nil {
		return agentruntime.RuntimeState{}, err
	}
	if expectedStateVersion < 1 || now.IsZero() {
		return agentruntime.RuntimeState{}, agentruntime.ErrInterruptConflict
	}
	var state agentruntime.RuntimeState
	err := r.db.Transaction(func(tx *gorm.DB) error {
		current, err := loadAgentCheckpointForScope(tx, scope, true)
		if err != nil {
			return err
		}
		transition, err := agentruntime.Interrupt(current, expectedStateVersion)
		if err != nil {
			return err
		}
		if err := validateAgentRuntimeTransition(scope, current, transition, now); err != nil {
			return err
		}
		stateJSON, err := json.Marshal(transition.State)
		if err != nil {
			return err
		}
		if len(stateJSON) > agentEventPayloadLimit {
			return ErrAgentPayloadTooLarge
		}
		if err := r.commitAgentRuntimeTransitionTx(tx, scope, current, transition, string(stateJSON), now); err != nil {
			if errors.Is(err, ErrAgentRuntimeStepConflict) {
				return agentruntime.ErrInterruptConflict
			}
			return err
		}
		state = transition.State
		return nil
	})
	if err != nil {
		return agentruntime.RuntimeState{}, err
	}
	return state, nil
}

func validateAgentRuntimeTransition(scope agentruntime.Scope, previous agentruntime.RuntimeState, transition agentruntime.RuntimeTransition, now time.Time) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	state := transition.State
	if previous.StateVersion < 1 || state.StateVersion != previous.StateVersion+1 ||
		(state.StepNumber != previous.StepNumber && state.StepNumber != previous.StepNumber+1) ||
		state.MaxSteps != previous.MaxSteps || state.UserMessage != previous.UserMessage ||
		state.MaxSteps < 1 || state.MaxSteps > 24 || !previous.Status.Valid() || !state.Status.Valid() || now.IsZero() {
		return errors.New("agent runtime transition boundary is invalid")
	}
	if len(transition.EventKinds) == 0 || len(transition.EventKinds) > 8 {
		return errors.New("agent runtime transition events are invalid")
	}
	for _, kind := range transition.EventKinds {
		if !kind.Valid() {
			return errors.New("agent runtime transition event kind is invalid")
		}
	}
	if transition.RejectedToolCall != nil {
		if previous.PendingToolCall != nil || state.PendingToolCall != nil || state.LastToolResult == nil ||
			state.LastToolResult.ToolCallID != transition.RejectedToolCall.ToolCallID ||
			state.LastToolResult.ActionVersion != transition.RejectedToolCall.ActionVersion || state.LastToolResult.Succeeded ||
			!transition.RejectedToolCall.ToolName.Valid() {
			return errors.New("agent rejected tool transition is invalid")
		}
	}
	return nil
}

func persistRejectedAgentToolDecision(db *gorm.DB, scope agentruntime.Scope, previous agentruntime.RuntimeState, transition agentruntime.RuntimeTransition, now time.Time) error {
	call := transition.RejectedToolCall
	if call == nil {
		return nil
	}
	policy, ok := agentruntime.ToolPolicyFor(call.ToolName)
	if !ok || transition.State.LastToolResult == nil {
		return errors.New("agent rejected tool policy is unavailable")
	}
	approvalRequired := agentruntime.ApprovalRequiredFor(policy, previous.Configuration.ExecutionMode)
	record := model.AgentToolCall{
		ID:    agentFactID("tool", scope.RunID, call.ToolCallID, strconv.Itoa(call.ActionVersion)),
		RunID: scope.RunID, ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion,
		ToolName: string(call.ToolName), Status: agentruntime.ToolCallFailed,
		RiskLevel: policy.RiskLevel, RequiredAccess: policy.RequiredAccess, ApprovalRequired: approvalRequired,
		IdempotencyKey: scope.RunID + ":" + call.ToolCallID + ":" + strconv.Itoa(call.ActionVersion),
		InputJSON:      string(call.Arguments), OutputJSON: string(transition.State.LastToolResult.Output),
		ErrorCode: transition.State.LastToolResult.ErrorCode, CreatedAt: now, UpdatedAt: now,
	}
	return db.Create(&record).Error
}

func persistAgentToolTransition(db *gorm.DB, scope agentruntime.Scope, previous agentruntime.RuntimeState, next agentruntime.RuntimeState, now time.Time) error {
	runID := scope.RunID
	if previous.PendingToolCall == nil && next.PendingToolCall != nil {
		policy, ok := agentruntime.ToolPolicyFor(next.PendingToolCall.ToolName)
		if !ok {
			return errors.New("agent tool policy is unavailable")
		}
		approvalRequired := agentruntime.ApprovalRequiredFor(policy, next.Configuration.ExecutionMode)
		status := agentruntime.ToolCallPending
		if approvalRequired {
			status = agentruntime.ToolCallWaitingApproval
		}
		call := model.AgentToolCall{
			ID:    agentFactID("tool", runID, next.PendingToolCall.ToolCallID, strconv.Itoa(next.PendingToolCall.ActionVersion)),
			RunID: runID, ToolCallID: next.PendingToolCall.ToolCallID, ActionVersion: next.PendingToolCall.ActionVersion,
			ToolName: string(next.PendingToolCall.ToolName), Status: status,
			RiskLevel: policy.RiskLevel, RequiredAccess: policy.RequiredAccess, ApprovalRequired: approvalRequired,
			IdempotencyKey: runID + ":" + next.PendingToolCall.ToolCallID + ":" + strconv.Itoa(next.PendingToolCall.ActionVersion),
			InputJSON:      string(next.PendingToolCall.Arguments), OutputJSON: `{}`, CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(&call).Error; err != nil {
			return err
		}
		if next.PendingToolCall.ToolName == agentruntime.ToolProductionRender {
			return persistProductionRenderAwaitingApproval(db, scope, next.PendingToolCall.Arguments, now)
		}
		return nil
	}
	if previous.Status == agentruntime.RunWaitingApproval && next.Status == agentruntime.RunWaitingTool &&
		previous.PendingToolCall != nil && next.PendingToolCall != nil &&
		previous.PendingToolCall.ToolCallID == next.PendingToolCall.ToolCallID &&
		previous.PendingToolCall.ActionVersion == next.PendingToolCall.ActionVersion {
		result := db.Model(&model.AgentToolCall{}).
			Where("run_id = ? AND tool_call_id = ? AND action_version = ? AND status = ?", runID,
				previous.PendingToolCall.ToolCallID, previous.PendingToolCall.ActionVersion, agentruntime.ToolCallWaitingApproval).
			Updates(agentToolApprovalUpdates{
				Status: agentruntime.ToolCallPending, ApprovalDecision: agentruntime.ToolApprovalApproved,
				ApprovalByUserID: scope.ActorUserID, ApprovalDecidedAt: now, UpdatedAt: now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrAgentRuntimeStepConflict
		}
		return nil
	}
	if previous.Status == agentruntime.RunWaitingTool && next.Status == agentruntime.RunWaitingTool &&
		previous.PendingToolCall != nil && next.PendingToolCall != nil &&
		previous.PendingToolCall.ToolCallID == next.PendingToolCall.ToolCallID &&
		previous.PendingToolCall.ActionVersion == next.PendingToolCall.ActionVersion &&
		!previous.PendingToolStarted && next.PendingToolStarted {
		result := db.Model(&model.AgentToolCall{}).
			Where("run_id = ? AND tool_call_id = ? AND action_version = ? AND status = ?", runID,
				previous.PendingToolCall.ToolCallID, previous.PendingToolCall.ActionVersion, agentruntime.ToolCallPending).
			Updates(agentToolExecutionUpdates{Status: agentruntime.ToolCallRunning, StartedAt: now, UpdatedAt: now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrAgentRuntimeStepConflict
		}
		return nil
	}
	if previous.PendingToolCall == nil || next.LastToolResult == nil ||
		next.LastToolResult.ToolCallID != previous.PendingToolCall.ToolCallID ||
		next.LastToolResult.ActionVersion != previous.PendingToolCall.ActionVersion {
		return nil
	}
	status := agentruntime.ToolCallFailed
	if next.LastToolResult.Succeeded {
		status = agentruntime.ToolCallSucceeded
	}
	result := db.Model(&model.AgentToolCall{}).
		Where("run_id = ? AND tool_call_id = ? AND action_version = ? AND status IN (?, ?, ?)", runID,
			previous.PendingToolCall.ToolCallID, previous.PendingToolCall.ActionVersion,
			agentruntime.ToolCallPending, agentruntime.ToolCallRunning, agentruntime.ToolCallWaitingApproval).
		Updates(agentToolCompletionUpdates{
			Status: status, OutputJSON: string(next.LastToolResult.Output),
			ErrorCode: next.LastToolResult.ErrorCode, UpdatedAt: now,
			ApprovalDecision:  approvalDecisionForToolResult(previous, next),
			ApprovalByUserID:  approvalActorForToolResult(scope, previous, next),
			ApprovalDecidedAt: approvalTimeForToolResult(now, previous, next),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrAgentRuntimeStepConflict
	}
	if previous.Status == agentruntime.RunWaitingApproval && previous.PendingToolCall.ToolName == agentruntime.ToolProductionRender && !next.LastToolResult.Succeeded {
		return persistProductionRenderApprovalFailure(db, scope, previous.PendingToolCall.Arguments, next.LastToolResult.ErrorCode, now)
	}
	return nil
}

func persistProductionRenderAwaitingApproval(db *gorm.DB, scope agentruntime.Scope, raw json.RawMessage, now time.Time) error {
	var arguments agentruntime.ProductionRenderArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return errors.New("frozen production render arguments are invalid")
	}
	if strings.TrimSpace(arguments.PlanKey) == "" || arguments.PlanVersion < 1 || strings.TrimSpace(arguments.ArtifactID) == "" || arguments.Attempt < 0 ||
		strings.TrimSpace(arguments.GenerationModel.ChannelID) == "" || strings.TrimSpace(arguments.GenerationModel.Model) == "" ||
		(arguments.ImageConfig == nil) == (arguments.VideoConfig == nil) || arguments.AmountMicrocredits <= 0 || arguments.PerTaskAmountMicrocredits <= 0 ||
		arguments.PriceVersion < 0 || arguments.Quantity <= 0 || strings.TrimSpace(arguments.BillingMode) == "" || strings.TrimSpace(arguments.QuoteFingerprint) == "" {
		return errors.New("frozen production render arguments are incomplete")
	}
	var current model.AgentProductionArtifact
	query := db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(`id = ? AND plan_key = ? AND plan_version = ? AND attempt = ? AND status IN (?, ?)
			AND EXISTS (
				SELECT 1 FROM agent_production_plan_versions
				 WHERE agent_production_plan_versions.id = agent_production_artifacts.plan_version_id
				   AND tenant_kind = ? AND tenant_id = ? AND domain_project_id = ? AND canvas_id = ?
				   AND status = ?
			)`,
			arguments.ArtifactID, arguments.PlanKey, arguments.PlanVersion, arguments.Attempt,
			model.AgentProductionArtifactPlanned, model.AgentProductionArtifactFailed,
			scope.TenantKind, scope.TenantID, scope.DomainProjectID, scope.CanvasID, model.AgentProductionPlanActive).
		Take(&current)
	if query.Error != nil {
		if errors.Is(query.Error, gorm.ErrRecordNotFound) {
			return ErrAgentProductionArtifactConflict
		}
		return query.Error
	}

	base := db.Model(&model.AgentProductionArtifact{}).
		Where("id = ? AND status = ? AND attempt = ?", current.ID, current.Status, current.Attempt)
	if current.Status == model.AgentProductionArtifactFailed && (current.TaskID != "" || current.BillingOrderID != "" || current.ResourceID != "") {
		if current.TaskID == "" || current.BillingOrderID == "" || current.ResourceID != "" {
			return ErrAgentProductionArtifactConflict
		}
		base = base.Where(`EXISTS (
				SELECT 1 FROM tasks
				 WHERE tasks.id = agent_production_artifacts.task_id
				   AND tasks.user_id = ? AND tasks.billing_order_id = agent_production_artifacts.billing_order_id
				   AND tasks.status IN (?, ?)
			) AND EXISTS (
				SELECT 1 FROM billing_orders
				 WHERE billing_orders.id = agent_production_artifacts.billing_order_id
				   AND billing_orders.user_id = ? AND billing_orders.task_id = agent_production_artifacts.task_id
				   AND billing_orders.status = ?
			)`,
			scope.ActorUserID, model.TaskStatusFailed, model.TaskStatusCancelled,
			scope.ActorUserID, model.BillingStatusRefunded)
	}
	result := base.Select("status", "task_id", "billing_order_id", "resource_id", "last_error_code", "updated_at").
		Updates(productionRenderApprovalUpdate{
			Status: model.AgentProductionArtifactAwaitingApproval, TaskID: "", BillingOrderID: "", ResourceID: "", LastErrorCode: "", UpdatedAt: now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrAgentProductionArtifactConflict
	}
	return nil
}

type productionRenderApprovalUpdate struct {
	Status         model.AgentProductionArtifactStatus `gorm:"column:status"`
	TaskID         string                              `gorm:"column:task_id"`
	BillingOrderID string                              `gorm:"column:billing_order_id"`
	ResourceID     string                              `gorm:"column:resource_id"`
	LastErrorCode  string                              `gorm:"column:last_error_code"`
	UpdatedAt      time.Time                           `gorm:"column:updated_at"`
}

func persistProductionRenderApprovalFailure(db *gorm.DB, scope agentruntime.Scope, raw json.RawMessage, failureCode string, now time.Time) error {
	var arguments agentruntime.ProductionRenderArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return errors.New("frozen production render arguments are invalid")
	}
	failureCode = strings.TrimSpace(failureCode)
	if strings.TrimSpace(arguments.ArtifactID) == "" || arguments.Attempt < 0 || failureCode == "" {
		return errors.New("production render approval failure facts are incomplete")
	}
	result := db.Model(&model.AgentProductionArtifact{}).
		Where(`id = ? AND plan_key = ? AND plan_version = ? AND attempt = ? AND status = ?
			AND EXISTS (
				SELECT 1 FROM agent_production_plan_versions
				 WHERE agent_production_plan_versions.id = agent_production_artifacts.plan_version_id
				   AND tenant_kind = ? AND tenant_id = ? AND domain_project_id = ? AND canvas_id = ?
			)`,
			arguments.ArtifactID, arguments.PlanKey, arguments.PlanVersion, arguments.Attempt, model.AgentProductionArtifactAwaitingApproval,
			scope.TenantKind, scope.TenantID, scope.DomainProjectID, scope.CanvasID).
		Select("status", "last_error_code", "updated_at").
		Updates(productionRenderApprovalUpdate{
			Status: model.AgentProductionArtifactFailed, LastErrorCode: failureCode, UpdatedAt: now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrAgentProductionArtifactConflict
	}
	return nil
}

type agentToolCompletionUpdates struct {
	Status            agentruntime.ToolCallStatus       `gorm:"column:status"`
	OutputJSON        string                            `gorm:"column:output_json"`
	ErrorCode         string                            `gorm:"column:error_code"`
	ApprovalDecision  agentruntime.ToolApprovalDecision `gorm:"column:approval_decision"`
	ApprovalByUserID  string                            `gorm:"column:approval_by_user_id"`
	ApprovalDecidedAt *time.Time                        `gorm:"column:approval_decided_at"`
	UpdatedAt         time.Time                         `gorm:"column:updated_at"`
}

type agentToolApprovalUpdates struct {
	Status            agentruntime.ToolCallStatus       `gorm:"column:status"`
	ApprovalDecision  agentruntime.ToolApprovalDecision `gorm:"column:approval_decision"`
	ApprovalByUserID  string                            `gorm:"column:approval_by_user_id"`
	ApprovalDecidedAt time.Time                         `gorm:"column:approval_decided_at"`
	UpdatedAt         time.Time                         `gorm:"column:updated_at"`
}

type agentToolExecutionUpdates struct {
	Status    agentruntime.ToolCallStatus `gorm:"column:status"`
	StartedAt time.Time                   `gorm:"column:started_at"`
	UpdatedAt time.Time                   `gorm:"column:updated_at"`
}

func approvalDecisionForToolResult(previous agentruntime.RuntimeState, next agentruntime.RuntimeState) agentruntime.ToolApprovalDecision {
	if previous.Status == agentruntime.RunWaitingApproval && next.LastToolResult != nil && next.LastToolResult.ErrorCode == "tool_approval_rejected" {
		return agentruntime.ToolApprovalRejected
	}
	return ""
}

func approvalActorForToolResult(scope agentruntime.Scope, previous agentruntime.RuntimeState, next agentruntime.RuntimeState) string {
	if approvalDecisionForToolResult(previous, next) != "" {
		return scope.ActorUserID
	}
	return ""
}

func approvalTimeForToolResult(now time.Time, previous agentruntime.RuntimeState, next agentruntime.RuntimeState) *time.Time {
	if approvalDecisionForToolResult(previous, next) == "" {
		return nil
	}
	return &now
}

func (r *Repository) LoadAgentCheckpoint(scope agentruntime.Scope) (agentruntime.RuntimeState, error) {
	return loadAgentCheckpointForScope(r.db, scope, false)
}

func loadAgentCheckpointForScope(db *gorm.DB, scope agentruntime.Scope, lock bool) (agentruntime.RuntimeState, error) {
	if err := scope.Validate(); err != nil {
		return agentruntime.RuntimeState{}, err
	}
	var facts struct {
		StateJSON       string                 `gorm:"column:state_json"`
		StateVersion    int                    `gorm:"column:state_version"`
		RunStateVersion int                    `gorm:"column:run_state_version"`
		RunStepNumber   int                    `gorm:"column:run_step_number"`
		RunMaxSteps     int                    `gorm:"column:run_max_steps"`
		RunStatus       agentruntime.RunStatus `gorm:"column:run_status"`
	}
	query := db.Table("agent_checkpoints").Select(`agent_checkpoints.state_json, agent_checkpoints.state_version,
		agent_runs.state_version AS run_state_version, agent_runs.step_number AS run_step_number, agent_runs.max_steps AS run_max_steps, agent_runs.status AS run_status`).
		Joins("JOIN agent_runs ON agent_runs.id = agent_checkpoints.run_id").
		Joins("JOIN agent_threads ON agent_threads.id = agent_runs.thread_id").
		Where(`agent_runs.id = ? AND agent_runs.thread_id = ? AND agent_runs.actor_user_id = ?
			AND agent_threads.tenant_kind = ? AND agent_threads.tenant_id = ?
			AND agent_threads.created_by_user_id = ? AND agent_threads.domain_project_id = ?
			AND agent_threads.canvas_id = ?`, scope.RunID, scope.ThreadID, scope.ActorUserID,
			scope.TenantKind, scope.TenantID, scope.ActorUserID, scope.DomainProjectID, scope.CanvasID).
		Order("agent_checkpoints.sequence DESC")
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := query.Take(&facts).Error
	if err != nil {
		return agentruntime.RuntimeState{}, err
	}
	decoder := json.NewDecoder(bytes.NewBufferString(facts.StateJSON))
	decoder.DisallowUnknownFields()
	var state agentruntime.RuntimeState
	if err := decoder.Decode(&state); err != nil {
		return agentruntime.RuntimeState{}, errors.New("agent checkpoint state is invalid")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || state.StateVersion != facts.StateVersion || state.StateVersion != facts.RunStateVersion || state.StepNumber != facts.RunStepNumber || state.MaxSteps != facts.RunMaxSteps || state.Status != facts.RunStatus {
		return agentruntime.RuntimeState{}, errors.New("agent checkpoint state is inconsistent")
	}
	return state, nil
}
