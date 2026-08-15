package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrAgentScopeConflict = errors.New("agent runtime scope conflict")
var ErrAgentPayloadTooLarge = errors.New("agent runtime payload is too large")

const agentEventPayloadLimit = 256 * 1024
const agentCheckpointPayloadLimit = 1024 * 1024

type CreateAgentRunInput struct {
	Scope           agentruntime.Scope
	ClientRequestID string
	Now             time.Time
}

type AgentRunRecord struct {
	Thread  model.AgentThread
	Run     model.AgentRun
	Created bool
}

type AgentRunIdentity struct {
	Thread model.AgentThread
	Run    model.AgentRun
}

func (r *Repository) CreateAgentThread(scope agentruntime.Scope, now time.Time) (*model.AgentThread, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if now.IsZero() {
		return nil, errors.New("agent thread creation time is required")
	}
	return agentThreadForCreate(r.db, scope, now)
}

func (r *Repository) AgentThreadForActor(threadID string, actorUserID string) (*model.AgentThread, error) {
	threadID = strings.TrimSpace(threadID)
	actorUserID = strings.TrimSpace(actorUserID)
	if threadID == "" || actorUserID == "" {
		return nil, errors.New("agent thread identity is invalid")
	}
	var thread model.AgentThread
	if err := r.db.Where("id = ? AND created_by_user_id = ?", threadID, actorUserID).Take(&thread).Error; err != nil {
		return nil, err
	}
	return &thread, nil
}

func (r *Repository) AgentRunIdentityForActor(runID string, actorUserID string) (*AgentRunIdentity, error) {
	runID = strings.TrimSpace(runID)
	actorUserID = strings.TrimSpace(actorUserID)
	if runID == "" || actorUserID == "" {
		return nil, errors.New("agent run identity is invalid")
	}
	var run model.AgentRun
	if err := r.db.Where("id = ? AND actor_user_id = ?", runID, actorUserID).Take(&run).Error; err != nil {
		return nil, err
	}
	thread, err := r.AgentThreadForActor(run.ThreadID, actorUserID)
	if err != nil {
		return nil, err
	}
	return &AgentRunIdentity{Thread: *thread, Run: run}, nil
}

func (r *Repository) CreateAgentRun(input CreateAgentRunInput) (*AgentRunRecord, error) {
	if err := input.Scope.Validate(); err != nil {
		return nil, err
	}
	input.ClientRequestID = strings.TrimSpace(input.ClientRequestID)
	if input.ClientRequestID == "" || len(input.ClientRequestID) > 120 {
		return nil, errors.New("agent run client request id is invalid")
	}
	if input.Now.IsZero() {
		return nil, errors.New("agent run creation time is required")
	}
	var record AgentRunRecord
	err := r.db.Transaction(func(tx *gorm.DB) error {
		thread, err := agentThreadForCreate(tx, input.Scope, input.Now)
		if err != nil {
			return err
		}
		record.Thread = *thread

		run := model.AgentRun{
			ID: input.Scope.RunID, ThreadID: input.Scope.ThreadID,
			ActorUserID: input.Scope.ActorUserID, ClientRequestID: input.ClientRequestID,
			Status: agentruntime.RunQueued, CreatedAt: input.Now, UpdatedAt: input.Now,
		}
		result := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "thread_id"}, {Name: "client_request_id"}},
			DoNothing: true,
		}).Create(&run)
		if result.Error != nil {
			return fmt.Errorf("create agent run: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			var existing model.AgentRun
			if err := tx.Where("thread_id = ? AND client_request_id = ?", input.Scope.ThreadID, input.ClientRequestID).First(&existing).Error; err != nil {
				return err
			}
			if existing.ActorUserID != input.Scope.ActorUserID {
				return ErrAgentScopeConflict
			}
			record.Run = existing
			return nil
		}
		record.Run = run
		record.Created = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func agentThreadForCreate(db *gorm.DB, scope agentruntime.Scope, now time.Time) (*model.AgentThread, error) {
	candidate := model.AgentThread{
		ID: scope.ThreadID, TenantKind: scope.TenantKind, TenantID: scope.TenantID,
		CreatedByUserID: scope.ActorUserID, DomainProjectID: scope.DomainProjectID,
		CanvasID: scope.CanvasID, Status: agentruntime.ThreadActive,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoNothing: true,
	}).Create(&candidate).Error; err != nil {
		return nil, fmt.Errorf("create agent thread: %w", err)
	}
	var thread model.AgentThread
	if err := db.First(&thread, "id = ?", scope.ThreadID).Error; err != nil {
		return nil, err
	}
	if thread.TenantKind != scope.TenantKind || thread.TenantID != scope.TenantID ||
		thread.CreatedByUserID != scope.ActorUserID || thread.DomainProjectID != scope.DomainProjectID || thread.CanvasID != scope.CanvasID {
		return nil, ErrAgentScopeConflict
	}
	return &thread, nil
}

func (r *Repository) AgentRunForScope(scope agentruntime.Scope) (*model.AgentRun, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	var run model.AgentRun
	err := r.db.Table("agent_runs").
		Select("agent_runs.*").
		Joins("JOIN agent_threads ON agent_threads.id = agent_runs.thread_id").
		Where(`agent_runs.id = ? AND agent_runs.thread_id = ? AND agent_runs.actor_user_id = ?
			AND agent_threads.tenant_kind = ? AND agent_threads.tenant_id = ?
			AND agent_threads.created_by_user_id = ? AND agent_threads.domain_project_id = ?
			AND agent_threads.canvas_id = ?`,
			scope.RunID, scope.ThreadID, scope.ActorUserID,
			scope.TenantKind, scope.TenantID, scope.ActorUserID, scope.DomainProjectID, scope.CanvasID).
		First(&run).Error
	if err != nil {
		return nil, err
	}
	return &run, nil
}

type AgentCheckpointInput struct {
	StateVersion int
	StateJSON    string
}

type AppendAgentEventInput struct {
	Scope       agentruntime.Scope
	Kind        agentruntime.EventKind
	PayloadJSON string
	Checkpoint  *AgentCheckpointInput
	Now         time.Time
}

func (r *Repository) AppendAgentEvent(input AppendAgentEventInput) (*model.AgentRunEvent, error) {
	if err := validateAgentEventInput(input); err != nil {
		return nil, err
	}
	var event model.AgentRunEvent
	err := r.db.Transaction(func(tx *gorm.DB) error {
		sequence, err := allocateAgentEventSequence(tx, input.Scope, input.Now)
		if err != nil {
			return err
		}
		event = model.AgentRunEvent{
			ID:    agentFactID("event", input.Scope.RunID, strconv.FormatInt(sequence, 10)),
			RunID: input.Scope.RunID, Sequence: sequence, Kind: input.Kind,
			PayloadJSON: input.PayloadJSON, CreatedAt: input.Now,
		}
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		if input.Checkpoint == nil {
			return nil
		}
		checkpoint := model.AgentCheckpoint{
			ID:    agentFactID("checkpoint", input.Scope.RunID, strconv.FormatInt(sequence, 10)),
			RunID: input.Scope.RunID, Sequence: sequence,
			StateVersion: input.Checkpoint.StateVersion, StateJSON: input.Checkpoint.StateJSON,
			CreatedAt: input.Now,
		}
		return tx.Create(&checkpoint).Error
	})
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func validateAgentEventInput(input AppendAgentEventInput) error {
	if err := input.Scope.Validate(); err != nil {
		return err
	}
	if !input.Kind.Valid() {
		return errors.New("agent event kind is invalid")
	}
	if len(input.PayloadJSON) > agentEventPayloadLimit {
		return ErrAgentPayloadTooLarge
	}
	if !json.Valid([]byte(input.PayloadJSON)) {
		return errors.New("agent event payload must be valid json")
	}
	if input.Now.IsZero() {
		return errors.New("agent event creation time is required")
	}
	if input.Checkpoint == nil {
		return nil
	}
	if input.Checkpoint.StateVersion < 1 {
		return errors.New("agent checkpoint state version is invalid")
	}
	if len(input.Checkpoint.StateJSON) > agentCheckpointPayloadLimit {
		return ErrAgentPayloadTooLarge
	}
	if !json.Valid([]byte(input.Checkpoint.StateJSON)) {
		return errors.New("agent checkpoint state must be valid json")
	}
	return nil
}

func allocateAgentEventSequence(db *gorm.DB, scope agentruntime.Scope, now time.Time) (int64, error) {
	var facts struct {
		Sequence int64 `gorm:"column:last_event_sequence"`
	}
	result := db.Raw(`
		UPDATE agent_runs
		   SET last_event_sequence = last_event_sequence + 1, updated_at = ?
		 WHERE id = ? AND thread_id = ? AND actor_user_id = ?
		   AND EXISTS (
		       SELECT 1 FROM agent_threads
		        WHERE agent_threads.id = agent_runs.thread_id
		          AND tenant_kind = ? AND tenant_id = ? AND created_by_user_id = ?
		          AND domain_project_id = ? AND canvas_id = ?
		   )
		 RETURNING last_event_sequence`,
		now, scope.RunID, scope.ThreadID, scope.ActorUserID,
		scope.TenantKind, scope.TenantID, scope.ActorUserID, scope.DomainProjectID, scope.CanvasID,
	).Scan(&facts)
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected != 1 {
		return 0, gorm.ErrRecordNotFound
	}
	return facts.Sequence, nil
}

func (r *Repository) AgentRunEventsAfter(scope agentruntime.Scope, afterSequence int64, limit int) ([]model.AgentRunEvent, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if afterSequence < 0 || limit < 1 || limit > 500 {
		return nil, errors.New("agent event cursor or limit is invalid")
	}
	var events []model.AgentRunEvent
	err := r.db.Table("agent_run_events").
		Select("agent_run_events.*").
		Joins("JOIN agent_runs ON agent_runs.id = agent_run_events.run_id").
		Joins("JOIN agent_threads ON agent_threads.id = agent_runs.thread_id").
		Where(`agent_run_events.run_id = ? AND agent_run_events.sequence > ?
			AND agent_runs.thread_id = ? AND agent_runs.actor_user_id = ?
			AND agent_threads.tenant_kind = ? AND agent_threads.tenant_id = ?
			AND agent_threads.created_by_user_id = ? AND agent_threads.domain_project_id = ?
			AND agent_threads.canvas_id = ?`,
			scope.RunID, afterSequence, scope.ThreadID, scope.ActorUserID,
			scope.TenantKind, scope.TenantID, scope.ActorUserID, scope.DomainProjectID, scope.CanvasID).
		Order("agent_run_events.sequence ASC").Limit(limit).Find(&events).Error
	if err != nil || len(events) > 0 {
		return events, err
	}
	if _, err := r.AgentRunForScope(scope); err != nil {
		return nil, err
	}
	return events, nil
}

func agentFactID(parts ...string) string {
	digest := sha256.New()
	for _, part := range parts {
		_, _ = digest.Write([]byte(part))
		_, _ = digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))
}
