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
)

var ErrAgentRuntimeStepConflict = errors.New("agent runtime step conflict")
var ErrAgentRuntimeInitializationConflict = errors.New("agent runtime initialization conflict")

type InitializeAgentRunInput struct {
	Scope             agentruntime.Scope
	ModelRecordID     string
	ModelKey          string
	MaxSteps          int
	ToolSchemaVersion int
	UserMessage       string
	Now               time.Time
}

type InitializedAgentRun struct {
	Run     model.AgentRun
	Created bool
}

type agentRunInitializationUpdates struct {
	MaxSteps          int       `gorm:"column:max_steps"`
	ModelRecordID     string    `gorm:"column:model_record_id"`
	ModelKey          string    `gorm:"column:model_key"`
	ToolSchemaVersion int       `gorm:"column:tool_schema_version"`
	LastEventSequence int64     `gorm:"column:last_event_sequence"`
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
		input.MaxSteps < 1 || input.MaxSteps > 24 || input.ToolSchemaVersion < 1 || input.UserMessage == "" || len(input.UserMessage) > 64*1024 || input.Now.IsZero() {
		return nil, errors.New("agent runtime initialization boundary is invalid")
	}
	state := agentruntime.RuntimeState{
		StateVersion: 1, StepNumber: 0, MaxSteps: input.MaxSteps,
		Status: agentruntime.RunQueued, UserMessage: input.UserMessage,
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
				AND max_steps = 0 AND model_record_id = '' AND model_key = '' AND tool_schema_version = 0
				AND last_event_sequence = 0 AND EXISTS (
					SELECT 1 FROM agent_threads WHERE agent_threads.id = agent_runs.thread_id
					AND tenant_kind = ? AND tenant_id = ? AND created_by_user_id = ?
					AND domain_project_id = ? AND canvas_id = ?
				)`, input.Scope.RunID, input.Scope.ThreadID, input.Scope.ActorUserID, agentruntime.RunQueued,
				input.Scope.TenantKind, input.Scope.TenantID, input.Scope.ActorUserID, input.Scope.DomainProjectID, input.Scope.CanvasID).
			Updates(agentRunInitializationUpdates{
				MaxSteps: input.MaxSteps, ModelRecordID: input.ModelRecordID, ModelKey: input.ModelKey,
				ToolSchemaVersion: input.ToolSchemaVersion, LastEventSequence: 1, UpdatedAt: input.Now,
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
		if updated.RowsAffected == 0 {
			if run.MaxSteps != input.MaxSteps || run.ModelRecordID != input.ModelRecordID || run.ModelKey != input.ModelKey || run.ToolSchemaVersion != input.ToolSchemaVersion || run.LastEventSequence != 1 {
				return ErrAgentRuntimeInitializationConflict
			}
			var event model.AgentRunEvent
			if err := tx.Where("run_id = ? AND sequence = 1", input.Scope.RunID).Take(&event).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrAgentRuntimeInitializationConflict
				}
				return err
			}
			var checkpoint model.AgentCheckpoint
			if err := tx.Where("run_id = ? AND sequence = 1", input.Scope.RunID).Take(&checkpoint).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrAgentRuntimeInitializationConflict
				}
				return err
			}
			if event.Kind != agentruntime.EventRunCreated || event.PayloadJSON != string(stateJSON) || checkpoint.StateVersion != 1 || checkpoint.StateJSON != string(stateJSON) {
				return ErrAgentRuntimeInitializationConflict
			}
			result.Run = run
			return nil
		}
		event := model.AgentRunEvent{
			ID: agentFactID("event", input.Scope.RunID, "1"), RunID: input.Scope.RunID, Sequence: 1,
			Kind: agentruntime.EventRunCreated, PayloadJSON: string(stateJSON), CreatedAt: input.Now,
		}
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		checkpoint := model.AgentCheckpoint{
			ID: agentFactID("checkpoint", input.Scope.RunID, "1"), RunID: input.Scope.RunID, Sequence: 1,
			StateVersion: 1, StateJSON: string(stateJSON), CreatedAt: input.Now,
		}
		if err := tx.Create(&checkpoint).Error; err != nil {
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

func (r *Repository) CommitAgentRuntimeTransition(scope agentruntime.Scope, expectedStep int, transition agentruntime.RuntimeTransition, now time.Time) error {
	state := transition.State
	if err := validateAgentRuntimeTransition(scope, expectedStep, transition, now); err != nil {
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
		var facts struct {
			LastEventSequence int64 `gorm:"column:last_event_sequence"`
		}
		var completedAt *time.Time
		if state.Status == agentruntime.RunSucceeded || state.Status == agentruntime.RunFailed || state.Status == agentruntime.RunCancelled {
			completedAt = &now
		}
		result := tx.Raw(`
			UPDATE agent_runs
			   SET step_number = ?, status = ?, last_event_sequence = last_event_sequence + ?, updated_at = ?, completed_at = ?
			 WHERE id = ? AND thread_id = ? AND actor_user_id = ? AND step_number = ? AND max_steps = ?
			   AND status IN (?, ?)
			   AND EXISTS (
			       SELECT 1 FROM agent_threads
			        WHERE agent_threads.id = agent_runs.thread_id
			          AND tenant_kind = ? AND tenant_id = ? AND created_by_user_id = ?
			          AND domain_project_id = ? AND canvas_id = ?
			   )
			 RETURNING last_event_sequence`,
			state.StepNumber, state.Status, len(transition.EventKinds), now, completedAt,
			scope.RunID, scope.ThreadID, scope.ActorUserID, expectedStep, state.MaxSteps, agentruntime.RunQueued, agentruntime.RunRunning,
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
		firstSequence := facts.LastEventSequence - int64(len(transition.EventKinds)) + 1
		for index, kind := range transition.EventKinds {
			sequence := firstSequence + int64(index)
			event := model.AgentRunEvent{ID: agentFactID("event", scope.RunID, strconv.FormatInt(sequence, 10)), RunID: scope.RunID, Sequence: sequence, Kind: kind, PayloadJSON: string(stateJSON), CreatedAt: now}
			if err := tx.Create(&event).Error; err != nil {
				return err
			}
		}
		checkpoint := model.AgentCheckpoint{ID: agentFactID("checkpoint", scope.RunID, strconv.FormatInt(facts.LastEventSequence, 10)), RunID: scope.RunID, Sequence: facts.LastEventSequence, StateVersion: state.StateVersion, StateJSON: string(stateJSON), CreatedAt: now}
		return tx.Create(&checkpoint).Error
	})
}

func validateAgentRuntimeTransition(scope agentruntime.Scope, expectedStep int, transition agentruntime.RuntimeTransition, now time.Time) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	state := transition.State
	if expectedStep < 0 || state.StepNumber != expectedStep+1 || state.StateVersion != state.StepNumber+1 || state.MaxSteps < 1 || state.MaxSteps > 24 || !state.Status.Valid() || now.IsZero() {
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
	return nil
}

func (r *Repository) LoadAgentCheckpoint(scope agentruntime.Scope) (agentruntime.RuntimeState, error) {
	if err := scope.Validate(); err != nil {
		return agentruntime.RuntimeState{}, err
	}
	var facts struct {
		StateJSON     string                 `gorm:"column:state_json"`
		StateVersion  int                    `gorm:"column:state_version"`
		RunStepNumber int                    `gorm:"column:run_step_number"`
		RunMaxSteps   int                    `gorm:"column:run_max_steps"`
		RunStatus     agentruntime.RunStatus `gorm:"column:run_status"`
	}
	err := r.db.Table("agent_checkpoints").Select(`agent_checkpoints.state_json, agent_checkpoints.state_version,
		agent_runs.step_number AS run_step_number, agent_runs.max_steps AS run_max_steps, agent_runs.status AS run_status`).
		Joins("JOIN agent_runs ON agent_runs.id = agent_checkpoints.run_id").
		Joins("JOIN agent_threads ON agent_threads.id = agent_runs.thread_id").
		Where(`agent_runs.id = ? AND agent_runs.thread_id = ? AND agent_runs.actor_user_id = ?
			AND agent_threads.tenant_kind = ? AND agent_threads.tenant_id = ?
			AND agent_threads.created_by_user_id = ? AND agent_threads.domain_project_id = ?
			AND agent_threads.canvas_id = ?`, scope.RunID, scope.ThreadID, scope.ActorUserID,
			scope.TenantKind, scope.TenantID, scope.ActorUserID, scope.DomainProjectID, scope.CanvasID).
		Order("agent_checkpoints.sequence DESC").Take(&facts).Error
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
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || state.StateVersion != facts.StateVersion || state.StepNumber != facts.RunStepNumber || state.MaxSteps != facts.RunMaxSteps || state.Status != facts.RunStatus {
		return agentruntime.RuntimeState{}, errors.New("agent checkpoint state is inconsistent")
	}
	return state, nil
}
