package repository

import (
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

var ErrAgentProductionPlanVersionConflict = errors.New("agent production plan version conflict")
var ErrAgentProductionArtifactConflict = errors.New("agent production artifact state conflict")

type AppendAgentProductionPlanInput struct {
	Scope       agentruntime.Scope
	RunID       string
	PlanKey     string
	BaseVersion int
	Draft       agentruntime.ProductionPlanDraft
	Now         time.Time
}

type AgentProductionPlanRecord struct {
	Plan      model.AgentProductionPlanVersion
	Artifacts []model.AgentProductionArtifact
}

type ArtifactTransition struct {
	ArtifactID      string
	ExpectedStatus  model.AgentProductionArtifactStatus
	NextStatus      model.AgentProductionArtifactStatus
	ExpectedAttempt int
	NextAttempt     int
	TaskID          string
	BillingOrderID  string
	ResourceID      string
	LastErrorCode   string
	Now             time.Time
}

type ArtifactCanvasCommit struct {
	ArtifactID      string
	ExpectedStatus  model.AgentProductionArtifactStatus
	ExpectedAttempt int
	CanvasNodeID    string
	Now             time.Time
}

type agentProductionPlanStatusUpdate struct {
	Status    model.AgentProductionPlanStatus `gorm:"column:status"`
	UpdatedAt time.Time                       `gorm:"column:updated_at"`
}

type agentProductionArtifactUpdate struct {
	Status         model.AgentProductionArtifactStatus `gorm:"column:status"`
	Attempt        int                                 `gorm:"column:attempt"`
	TaskID         string                              `gorm:"column:task_id"`
	BillingOrderID string                              `gorm:"column:billing_order_id"`
	ResourceID     string                              `gorm:"column:resource_id"`
	LastErrorCode  string                              `gorm:"column:last_error_code"`
	UpdatedAt      time.Time                           `gorm:"column:updated_at"`
}

type agentProductionArtifactCanvasCommitUpdate struct {
	Status       model.AgentProductionArtifactStatus `gorm:"column:status"`
	CanvasNodeID string                              `gorm:"column:canvas_node_id"`
	UpdatedAt    time.Time                           `gorm:"column:updated_at"`
}

func (r *Repository) AppendAgentProductionPlanVersion(input AppendAgentProductionPlanInput) (*AgentProductionPlanRecord, error) {
	if err := validateAppendAgentProductionPlanInput(input); err != nil {
		return nil, err
	}
	shotsJSON, err := json.Marshal(input.Draft.Shots)
	if err != nil {
		return nil, fmt.Errorf("encode production plan shots: %w", err)
	}
	expectedDeliveryJSON, err := json.Marshal(struct {
		Scripts          int `json:"scripts"`
		StoryboardImages int `json:"storyboardImages"`
		VideoClips       int `json:"videoClips"`
	}{Scripts: 1, StoryboardImages: len(input.Draft.Shots), VideoClips: len(input.Draft.Shots)})
	if err != nil {
		return nil, fmt.Errorf("encode production plan delivery: %w", err)
	}
	nextVersion := input.BaseVersion + 1
	plan := model.AgentProductionPlanVersion{
		ID:      agentFactID("production-plan", input.PlanKey, strconv.Itoa(nextVersion)),
		PlanKey: input.PlanKey, TenantKind: input.Scope.TenantKind, TenantID: input.Scope.TenantID, DomainProjectID: input.Scope.DomainProjectID,
		CanvasID: input.Scope.CanvasID, CreatedByRunID: input.RunID, Version: nextVersion,
		Status: model.AgentProductionPlanActive, Title: strings.TrimSpace(input.Draft.Title),
		TargetDurationMS: input.Draft.TargetDurationMS, Script: input.Draft.Script,
		ShotsJSON: string(shotsJSON), ExpectedDeliveryJSON: string(expectedDeliveryJSON),
		CreatedAt: input.Now, UpdatedAt: input.Now,
	}
	expectedArtifacts := productionArtifactsForPlan(plan, input.Draft.Shots, input.Now)

	record := AgentProductionPlanRecord{}
	err = r.db.Transaction(func(tx *gorm.DB) error {
		if err := verifyAgentRunScope(tx, input.Scope); err != nil {
			return err
		}
		var current model.AgentProductionPlanVersion
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("plan_key = ? AND tenant_kind = ? AND tenant_id = ? AND domain_project_id = ? AND canvas_id = ?", input.PlanKey, input.Scope.TenantKind, input.Scope.TenantID, input.Scope.DomainProjectID, input.Scope.CanvasID).
			Order("version DESC").Limit(1).Take(&current)
		if query.Error != nil && !errors.Is(query.Error, gorm.ErrRecordNotFound) {
			return query.Error
		}
		latestVersion := 0
		if query.Error == nil {
			latestVersion = current.Version
		}
		if latestVersion != input.BaseVersion {
			if latestVersion == nextVersion && sameAgentProductionPlanContent(current, plan) {
				artifacts, replayErr := loadAgentProductionArtifacts(tx, current.ID)
				if replayErr != nil {
					return replayErr
				}
				if !sameAgentProductionArtifactIdentities(artifacts, expectedArtifacts) {
					return ErrAgentProductionPlanVersionConflict
				}
				record.Plan = current
				record.Artifacts = artifacts
				return nil
			}
			return ErrAgentProductionPlanVersionConflict
		}
		if current.ID != "" {
			result := tx.Model(&model.AgentProductionPlanVersion{}).
				Where("id = ? AND version = ? AND status = ?", current.ID, current.Version, model.AgentProductionPlanActive).
				Select("status", "updated_at").
				Updates(agentProductionPlanStatusUpdate{Status: model.AgentProductionPlanSuperseded, UpdatedAt: input.Now})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrAgentProductionPlanVersionConflict
			}
		}
		if err := tx.Create(&plan).Error; err != nil {
			if isUniqueConstraintError(err) {
				return ErrAgentProductionPlanVersionConflict
			}
			return err
		}
		artifacts := expectedArtifacts
		if err := tx.Create(&artifacts).Error; err != nil {
			return err
		}
		record.Plan = plan
		record.Artifacts = artifacts
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func sameAgentProductionPlanContent(current model.AgentProductionPlanVersion, expected model.AgentProductionPlanVersion) bool {
	return current.ID == expected.ID && current.PlanKey == expected.PlanKey && current.Version == expected.Version &&
		current.TenantKind == expected.TenantKind && current.TenantID == expected.TenantID && current.DomainProjectID == expected.DomainProjectID && current.CanvasID == expected.CanvasID &&
		current.CreatedByRunID == expected.CreatedByRunID && current.Status == model.AgentProductionPlanActive &&
		current.Title == expected.Title && current.TargetDurationMS == expected.TargetDurationMS && current.Script == expected.Script &&
		current.ShotsJSON == expected.ShotsJSON && current.ExpectedDeliveryJSON == expected.ExpectedDeliveryJSON
}

func loadAgentProductionArtifacts(db *gorm.DB, planVersionID string) ([]model.AgentProductionArtifact, error) {
	var artifacts []model.AgentProductionArtifact
	if err := db.Where("plan_version_id = ?", planVersionID).Order("shot_key ASC, kind ASC").Find(&artifacts).Error; err != nil {
		return nil, err
	}
	return artifacts, nil
}

func sameAgentProductionArtifactIdentities(current []model.AgentProductionArtifact, expected []model.AgentProductionArtifact) bool {
	if len(current) != len(expected) {
		return false
	}
	expectedByID := make(map[string]model.AgentProductionArtifact, len(expected))
	for _, artifact := range expected {
		expectedByID[artifact.ID] = artifact
	}
	for _, artifact := range current {
		expectedArtifact, ok := expectedByID[artifact.ID]
		if !ok || artifact.PlanKey != expectedArtifact.PlanKey || artifact.PlanVersionID != expectedArtifact.PlanVersionID ||
			artifact.PlanVersion != expectedArtifact.PlanVersion || artifact.ShotKey != expectedArtifact.ShotKey || artifact.Kind != expectedArtifact.Kind {
			return false
		}
	}
	return true
}

func (r *Repository) AgentProductionPlanVersionForScope(scope agentruntime.Scope, planKey string, version int) (*model.AgentProductionPlanVersion, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	planKey = strings.TrimSpace(planKey)
	if planKey == "" || version < 1 {
		return nil, errors.New("agent production plan identity is invalid")
	}
	var plan model.AgentProductionPlanVersion
	err := r.db.Where("plan_key = ? AND version = ? AND tenant_kind = ? AND tenant_id = ? AND domain_project_id = ? AND canvas_id = ?", planKey, version, scope.TenantKind, scope.TenantID, scope.DomainProjectID, scope.CanvasID).Take(&plan).Error
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

func (r *Repository) AgentProductionArtifactsForVersion(scope agentruntime.Scope, planKey string, version int) ([]model.AgentProductionArtifact, error) {
	plan, err := r.AgentProductionPlanVersionForScope(scope, planKey, version)
	if err != nil {
		return nil, err
	}
	var artifacts []model.AgentProductionArtifact
	if err := r.db.Where("plan_version_id = ?", plan.ID).Order("shot_key ASC, kind ASC").Find(&artifacts).Error; err != nil {
		return nil, err
	}
	return artifacts, nil
}

func (r *Repository) ActiveAgentProductionPlanForThread(scope agentruntime.Scope) (*AgentProductionPlanRecord, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	var plan model.AgentProductionPlanVersion
	err := r.db.Table("agent_production_plan_versions").
		Select("agent_production_plan_versions.*").
		Joins("JOIN agent_runs ON agent_runs.id = agent_production_plan_versions.created_by_run_id").
		Where(`agent_production_plan_versions.tenant_kind = ?
			AND agent_production_plan_versions.tenant_id = ?
			AND agent_production_plan_versions.domain_project_id = ?
			AND agent_production_plan_versions.canvas_id = ?
			AND agent_production_plan_versions.status = ?
			AND agent_runs.thread_id = ?`,
			scope.TenantKind, scope.TenantID, scope.DomainProjectID, scope.CanvasID,
			model.AgentProductionPlanActive, scope.ThreadID).
		Order("agent_production_plan_versions.created_at DESC, agent_production_plan_versions.version DESC, agent_production_plan_versions.id DESC").
		Take(&plan).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var artifacts []model.AgentProductionArtifact
	if err := r.db.Where("plan_version_id = ?", plan.ID).Order("shot_key ASC, kind ASC").Find(&artifacts).Error; err != nil {
		return nil, err
	}
	return &AgentProductionPlanRecord{Plan: plan, Artifacts: artifacts}, nil
}

func (r *Repository) TransitionAgentProductionArtifact(scope agentruntime.Scope, input ArtifactTransition) (*model.AgentProductionArtifact, error) {
	if err := validateArtifactTransition(scope, input); err != nil {
		return nil, err
	}
	var transitioned model.AgentProductionArtifact
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var current model.AgentProductionArtifact
		err := tx.Table("agent_production_artifacts").Select("agent_production_artifacts.*").
			Joins("JOIN agent_production_plan_versions ON agent_production_plan_versions.id = agent_production_artifacts.plan_version_id").
			Where(`agent_production_artifacts.id = ?
				AND agent_production_plan_versions.tenant_kind = ?
				AND agent_production_plan_versions.tenant_id = ?
				AND agent_production_plan_versions.domain_project_id = ?
				AND agent_production_plan_versions.canvas_id = ?`,
				strings.TrimSpace(input.ArtifactID), scope.TenantKind, scope.TenantID, scope.DomainProjectID, scope.CanvasID).
			Take(&current).Error
		if err != nil {
			return err
		}
		if current.Status != input.ExpectedStatus || current.Attempt != input.ExpectedAttempt {
			return ErrAgentProductionArtifactConflict
		}
		if err := validateArtifactFactBindings(current, input); err != nil {
			return err
		}
		if input.NextStatus == model.AgentProductionArtifactSucceeded && current.Kind != model.AgentProductionArtifactScript && strings.TrimSpace(input.ResourceID) == "" && current.ResourceID == "" {
			return errors.New("successful media artifact requires resource id")
		}
		updates := agentProductionArtifactUpdate{
			Status: input.NextStatus, Attempt: input.NextAttempt,
			TaskID:         keepProductionFact(current.TaskID, input.TaskID),
			BillingOrderID: keepProductionFact(current.BillingOrderID, input.BillingOrderID),
			ResourceID:     keepProductionFact(current.ResourceID, input.ResourceID),
			LastErrorCode:  strings.TrimSpace(input.LastErrorCode), UpdatedAt: input.Now,
		}
		result := tx.Model(&model.AgentProductionArtifact{}).
			Where("id = ? AND status = ? AND attempt = ?", current.ID, input.ExpectedStatus, input.ExpectedAttempt).
			Select("status", "attempt", "task_id", "billing_order_id", "resource_id", "last_error_code", "updated_at").
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrAgentProductionArtifactConflict
		}
		return tx.First(&transitioned, "id = ?", current.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return &transitioned, nil
}

func (r *Repository) CommitAgentProductionArtifactCanvasNode(scope agentruntime.Scope, input ArtifactCanvasCommit) (*model.AgentProductionArtifact, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	input.ArtifactID = strings.TrimSpace(input.ArtifactID)
	input.CanvasNodeID = strings.TrimSpace(input.CanvasNodeID)
	if !scope.CanMutateCanvas() || input.ArtifactID == "" || input.CanvasNodeID == "" || len(input.CanvasNodeID) > 120 ||
		!input.ExpectedStatus.Valid() || input.ExpectedAttempt < 0 || input.Now.IsZero() {
		return nil, errors.New("agent production artifact canvas commit is invalid")
	}
	var committed model.AgentProductionArtifact
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var current model.AgentProductionArtifact
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Table("agent_production_artifacts").Select("agent_production_artifacts.*").
			Joins("JOIN agent_production_plan_versions ON agent_production_plan_versions.id = agent_production_artifacts.plan_version_id").
			Where(`agent_production_artifacts.id = ?
				AND agent_production_plan_versions.tenant_kind = ?
				AND agent_production_plan_versions.tenant_id = ?
				AND agent_production_plan_versions.domain_project_id = ?
				AND agent_production_plan_versions.canvas_id = ?`,
				input.ArtifactID, scope.TenantKind, scope.TenantID, scope.DomainProjectID, scope.CanvasID).
			Take(&current).Error
		if err != nil {
			return err
		}
		replayStatus := current.Status == input.ExpectedStatus ||
			(input.ExpectedStatus == model.AgentProductionArtifactSucceeded && current.Status == model.AgentProductionArtifactCommitted)
		if current.CanvasNodeID == input.CanvasNodeID && current.Attempt == input.ExpectedAttempt && replayStatus {
			committed = current
			return nil
		}
		if current.CanvasNodeID != "" || current.Status != input.ExpectedStatus || current.Attempt != input.ExpectedAttempt {
			return ErrAgentProductionArtifactConflict
		}
		nextStatus := current.Status
		if current.Status == model.AgentProductionArtifactSucceeded {
			nextStatus = model.AgentProductionArtifactCommitted
		}
		result := tx.Model(&model.AgentProductionArtifact{}).
			Where("id = ? AND status = ? AND attempt = ? AND canvas_node_id = ''", current.ID, current.Status, current.Attempt).
			Select("status", "canvas_node_id", "updated_at").
			Updates(agentProductionArtifactCanvasCommitUpdate{Status: nextStatus, CanvasNodeID: input.CanvasNodeID, UpdatedAt: input.Now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrAgentProductionArtifactConflict
		}
		return tx.First(&committed, "id = ?", current.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return &committed, nil
}

func validateAppendAgentProductionPlanInput(input AppendAgentProductionPlanInput) error {
	if err := input.Scope.Validate(); err != nil {
		return err
	}
	if !input.Scope.CanMutateCanvas() {
		return errors.New("agent production plan requires canvas editor access")
	}
	if strings.TrimSpace(input.RunID) == "" || strings.TrimSpace(input.RunID) != input.Scope.RunID {
		return errors.New("agent production plan run id is invalid")
	}
	if strings.TrimSpace(input.PlanKey) == "" || len(strings.TrimSpace(input.PlanKey)) > 120 || input.BaseVersion < 0 {
		return errors.New("agent production plan version identity is invalid")
	}
	if input.Now.IsZero() {
		return errors.New("agent production plan creation time is required")
	}
	return input.Draft.Validate()
}

func verifyAgentRunScope(db *gorm.DB, scope agentruntime.Scope) error {
	var count int64
	err := db.Table("agent_runs").
		Joins("JOIN agent_threads ON agent_threads.id = agent_runs.thread_id").
		Where(`agent_runs.id = ? AND agent_runs.thread_id = ? AND agent_runs.actor_user_id = ?
			AND agent_threads.tenant_kind = ? AND agent_threads.tenant_id = ?
			AND agent_threads.created_by_user_id = ? AND agent_threads.domain_project_id = ?
			AND agent_threads.canvas_id = ?`, scope.RunID, scope.ThreadID, scope.ActorUserID,
			scope.TenantKind, scope.TenantID, scope.ActorUserID, scope.DomainProjectID, scope.CanvasID).
		Count(&count).Error
	if err != nil {
		return err
	}
	if count != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func productionArtifactsForPlan(plan model.AgentProductionPlanVersion, shots []agentruntime.ShotPlanDraft, now time.Time) []model.AgentProductionArtifact {
	artifacts := make([]model.AgentProductionArtifact, 0, 1+len(shots)*2)
	appendArtifact := func(shotKey string, kind model.AgentProductionArtifactKind, status model.AgentProductionArtifactStatus) {
		artifacts = append(artifacts, model.AgentProductionArtifact{
			ID:      agentFactID("production-artifact", plan.PlanKey, strconv.Itoa(plan.Version), shotKey, string(kind)),
			PlanKey: plan.PlanKey, PlanVersionID: plan.ID, PlanVersion: plan.Version,
			ShotKey: shotKey, Kind: kind, Status: status, CreatedAt: now, UpdatedAt: now,
		})
	}
	appendArtifact("", model.AgentProductionArtifactScript, model.AgentProductionArtifactSucceeded)
	for _, shot := range shots {
		appendArtifact(shot.ShotKey, model.AgentProductionArtifactStoryboardImage, model.AgentProductionArtifactPlanned)
		appendArtifact(shot.ShotKey, model.AgentProductionArtifactVideoClip, model.AgentProductionArtifactPlanned)
	}
	return artifacts
}

func validateArtifactTransition(scope agentruntime.Scope, input ArtifactTransition) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if !scope.CanMutateCanvas() {
		return errors.New("agent production artifact transition requires canvas editor access")
	}
	if strings.TrimSpace(input.ArtifactID) == "" || input.Now.IsZero() {
		return errors.New("agent production artifact transition identity is invalid")
	}
	if !input.ExpectedStatus.Valid() || !input.NextStatus.Valid() || !productionArtifactTransitionAllowed(input.ExpectedStatus, input.NextStatus) {
		return errors.New("agent production artifact transition is invalid")
	}
	if input.ExpectedAttempt < 0 || input.NextAttempt < input.ExpectedAttempt || input.NextAttempt > input.ExpectedAttempt+1 {
		return errors.New("agent production artifact attempt transition is invalid")
	}
	if input.NextAttempt == input.ExpectedAttempt+1 && input.NextStatus != model.AgentProductionArtifactQueued {
		return errors.New("agent production artifact attempt can only advance when queued")
	}
	return nil
}

func productionArtifactTransitionAllowed(current model.AgentProductionArtifactStatus, next model.AgentProductionArtifactStatus) bool {
	switch current {
	case model.AgentProductionArtifactPlanned:
		return next == model.AgentProductionArtifactAwaitingApproval || next == model.AgentProductionArtifactQueued || next == model.AgentProductionArtifactFailed
	case model.AgentProductionArtifactAwaitingApproval:
		return next == model.AgentProductionArtifactQueued || next == model.AgentProductionArtifactFailed
	case model.AgentProductionArtifactQueued:
		return next == model.AgentProductionArtifactRunning || next == model.AgentProductionArtifactSucceeded || next == model.AgentProductionArtifactFailed
	case model.AgentProductionArtifactRunning:
		return next == model.AgentProductionArtifactSucceeded || next == model.AgentProductionArtifactFailed
	case model.AgentProductionArtifactFailed:
		return next == model.AgentProductionArtifactAwaitingApproval || next == model.AgentProductionArtifactQueued
	case model.AgentProductionArtifactSucceeded:
		return next == model.AgentProductionArtifactCommitted
	default:
		return false
	}
}

func validateArtifactFactBindings(current model.AgentProductionArtifact, input ArtifactTransition) error {
	bindings := []struct {
		name     string
		current  string
		incoming string
	}{
		{name: "task", current: current.TaskID, incoming: strings.TrimSpace(input.TaskID)},
		{name: "billing order", current: current.BillingOrderID, incoming: strings.TrimSpace(input.BillingOrderID)},
		{name: "resource", current: current.ResourceID, incoming: strings.TrimSpace(input.ResourceID)},
	}
	for _, binding := range bindings {
		if binding.current != "" && binding.incoming != "" && binding.current != binding.incoming {
			return fmt.Errorf("agent production artifact %s fact conflict: %w", binding.name, ErrAgentProductionArtifactConflict)
		}
	}
	return nil
}

func keepProductionFact(current string, incoming string) string {
	incoming = strings.TrimSpace(incoming)
	if incoming != "" {
		return incoming
	}
	return current
}

func isUniqueConstraintError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint") || strings.Contains(message, "duplicate key")
}
