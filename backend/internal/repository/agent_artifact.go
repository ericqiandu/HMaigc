package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrArtifactRevisionConflict      = errors.New("artifact revision conflict")
	ErrArtifactUpstreamRevisionStale = errors.New("artifact upstream revision is stale")
	ErrAssetBindingRevisionConflict  = errors.New("asset binding revision conflict")
	ErrMediaCandidateInvalid         = errors.New("media candidate revision is invalid")
)

const mediaCandidateArtifactKind = "media_candidate"

func (r *Repository) AppendArtifactRevision(
	scope agentruntime.Scope,
	artifactID string,
	expectedRevision int64,
	draft agentruntime.ArtifactDraft,
) (*model.AgentArtifactRevision, error) {
	if err := validateProductionRepositoryScope(scope, true); err != nil {
		return nil, err
	}
	var revision *model.AgentArtifactRevision
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var appendErr error
		revision, appendErr = appendArtifactRevisionTx(tx, scope, artifactID, expectedRevision, draft, "")
		return appendErr
	})
	return revision, err
}

// AppendArtifactRevisionOnce persists one immutable fact and returns that exact
// fact on an idempotent replay. It never creates a second revision.
func (r *Repository) AppendArtifactRevisionOnce(
	scope agentruntime.Scope,
	artifactID string,
	draft agentruntime.ArtifactDraft,
) (*model.AgentArtifactRevision, error) {
	if err := validateProductionRepositoryScope(scope, true); err != nil {
		return nil, err
	}
	if replayed, err := loadExactArtifactRevisionOnceTx(r.db, scope, artifactID, draft); err == nil {
		return replayed, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	stored, err := r.AppendArtifactRevision(scope, artifactID, 0, draft)
	if err == nil {
		return stored, nil
	}
	if !errors.Is(err, ErrArtifactRevisionConflict) {
		return nil, err
	}
	return loadExactArtifactRevisionOnceTx(r.db, scope, artifactID, draft)
}

func (r *Repository) AppendMediaCandidateRevision(
	scope agentruntime.Scope,
	artifactID string,
	draft agentruntime.ArtifactDraft,
) (*model.AgentArtifactRevision, error) {
	if draft.Kind != mediaCandidateArtifactKind || draft.SchemaVersion != 1 ||
		strings.TrimSpace(draft.ResourceID) == "" || strings.TrimSpace(draft.ModelRequestIdentity) == "" {
		return nil, ErrMediaCandidateInvalid
	}
	return r.AppendArtifactRevisionOnce(scope, artifactID, draft)
}

func (r *Repository) MediaCandidateRevisionsInScope(scope agentruntime.Scope) ([]model.AgentArtifactRevision, error) {
	if err := validateProductionRepositoryScope(scope, false); err != nil {
		return nil, err
	}
	var revisions []model.AgentArtifactRevision
	err := productionArtifactRevisionScopeQuery(r.db, scope).
		Where("kind = ?", mediaCandidateArtifactKind).
		Order("created_at ASC, id ASC").
		Find(&revisions).Error
	return revisions, err
}

func (r *Repository) ApprovedArtifactRevisionIDsForScope(scope agentruntime.Scope) (map[string]struct{}, error) {
	if err := validateProductionRepositoryScope(scope, false); err != nil {
		return nil, err
	}
	var approvals []model.AgentTimelineItem
	if err := r.db.Table("agent_timeline_items").Select("agent_timeline_items.*").
		Joins("JOIN agent_runs ON agent_runs.id = agent_timeline_items.run_id").
		Joins("JOIN agent_threads ON agent_threads.id = agent_runs.thread_id").
		Where(`agent_timeline_items.run_id = ? AND agent_timeline_items.thread_id = ?
			AND agent_timeline_items.tenant_kind = ? AND agent_timeline_items.tenant_id = ?
			AND agent_timeline_items.kind = ? AND agent_timeline_items.status = ?
			AND agent_runs.actor_user_id = ? AND agent_threads.created_by_user_id = ?
			AND agent_threads.domain_project_id = ? AND agent_threads.canvas_id = ?`,
			scope.RunID, scope.ThreadID, scope.TenantKind, scope.TenantID,
			model.AgentTimelineItemApproval, model.AgentTimelineItemCompleted,
			scope.ActorUserID, scope.ActorUserID, scope.DomainProjectID, scope.CanvasID).
		Order("agent_timeline_items.ordinal ASC, agent_timeline_items.id ASC").Find(&approvals).Error; err != nil {
		return nil, err
	}
	approved := make(map[string]struct{}, len(approvals))
	for _, item := range approvals {
		content, err := agentruntime.DecodeStageReviewResolutionContent([]byte(item.ContentJSON))
		if err != nil {
			return nil, err
		}
		if content.Decision == agentruntime.StageReviewApprove {
			approved[content.RevisionID] = struct{}{}
		}
	}
	var selections []model.AgentArtifactRevision
	if err := productionArtifactRevisionScopeQuery(r.db, scope).
		Where("kind = ? AND schema_version = ?", "media_candidate_selection", 1).
		Order("created_at ASC, id ASC").Find(&selections).Error; err != nil {
		return nil, err
	}
	for _, revision := range selections {
		selection, err := agentruntime.DecodeMediaCandidateSelection([]byte(revision.PayloadJSON))
		if err != nil || !artifactRevisionRefsStartWithStored(revision.UpstreamRevisionsJSON, []agentruntime.ArtifactRevisionRef{
			selection.ReviewRevision, selection.SelectedCandidateRevision,
		}) {
			return nil, ErrMediaCandidateInvalid
		}
		if _, reviewApproved := approved[selection.ReviewRevision.RevisionID]; reviewApproved {
			approved[selection.SelectedCandidateRevision.RevisionID] = struct{}{}
		}
	}
	return approved, nil
}

func artifactRevisionRefsStartWithStored(raw string, want []agentruntime.ArtifactRevisionRef) bool {
	var got []agentruntime.ArtifactRevisionRef
	if err := json.Unmarshal([]byte(raw), &got); err != nil || len(got) < len(want) {
		return false
	}
	for index := range got {
		if got[index].Validate() != nil {
			return false
		}
	}
	for index := range want {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func loadExactArtifactRevisionOnceTx(
	tx *gorm.DB,
	scope agentruntime.Scope,
	artifactID string,
	draft agentruntime.ArtifactDraft,
) (*model.AgentArtifactRevision, error) {
	if draft.UpstreamRevisions == nil {
		draft.UpstreamRevisions = []agentruntime.ArtifactRevisionRef{}
	}
	if draft.SkillVersions == nil {
		draft.SkillVersions = []agentruntime.SkillSelection{}
	}
	upstreamJSON, err := json.Marshal(draft.UpstreamRevisions)
	if err != nil {
		return nil, err
	}
	skillsJSON, err := json.Marshal(draft.SkillVersions)
	if err != nil {
		return nil, err
	}
	var revision model.AgentArtifactRevision
	if err := productionArtifactRevisionScopeQuery(tx, scope).
		Where("artifact_id = ? AND revision = 1", artifactID).
		Take(&revision).Error; err != nil {
		return nil, err
	}
	if revision.ArtifactKey != draft.ArtifactKey || revision.Kind != draft.Kind ||
		revision.SchemaVersion != draft.SchemaVersion || revision.PayloadJSON != string(draft.Payload) ||
		revision.ResourceID != draft.ResourceID || revision.UpstreamRevisionsJSON != string(upstreamJSON) ||
		revision.ModelRequestIdentity != draft.ModelRequestIdentity || revision.SkillVersionsJSON != string(skillsJSON) ||
		revision.LifecycleStatus != model.AgentArtifactRevisionAwaitingReview {
		return nil, ErrArtifactRevisionConflict
	}
	return &revision, nil
}

func appendArtifactRevisionTx(
	tx *gorm.DB,
	scope agentruntime.Scope,
	artifactID string,
	expectedRevision int64,
	draft agentruntime.ArtifactDraft,
	createdBySpecialistID string,
) (*model.AgentArtifactRevision, error) {
	if strings.TrimSpace(artifactID) != artifactID || artifactID == "" || len(artifactID) > 80 || expectedRevision < 0 {
		return nil, ErrArtifactRevisionConflict
	}
	if strings.TrimSpace(createdBySpecialistID) != createdBySpecialistID || len(createdBySpecialistID) > 80 {
		return nil, ErrArtifactRevisionConflict
	}
	if err := agentruntime.ValidateArtifactDraft(draft); err != nil {
		return nil, err
	}
	if draft.UpstreamRevisions == nil {
		draft.UpstreamRevisions = []agentruntime.ArtifactRevisionRef{}
	}
	if draft.SkillVersions == nil {
		draft.SkillVersions = []agentruntime.SkillSelection{}
	}
	upstreamRevisionsJSON, err := json.Marshal(draft.UpstreamRevisions)
	if err != nil {
		return nil, fmt.Errorf("encode artifact upstream revisions: %w", err)
	}
	skillVersionsJSON, err := json.Marshal(draft.SkillVersions)
	if err != nil {
		return nil, fmt.Errorf("encode artifact skill versions: %w", err)
	}

	nextRevision := expectedRevision + 1
	now := time.Now().UTC()
	revision := &model.AgentArtifactRevision{
		ID:                    productionArtifactRevisionID(scope, artifactID, nextRevision),
		TenantKind:            scope.TenantKind,
		TenantID:              scope.TenantID,
		ActorUserID:           scope.ActorUserID,
		DomainProjectID:       scope.DomainProjectID,
		CanvasID:              scope.CanvasID,
		ThreadID:              scope.ThreadID,
		RunID:                 scope.RunID,
		ArtifactID:            artifactID,
		ArtifactKey:           draft.ArtifactKey,
		Revision:              nextRevision,
		Kind:                  draft.Kind,
		SchemaVersion:         draft.SchemaVersion,
		PayloadJSON:           string(draft.Payload),
		ResourceID:            draft.ResourceID,
		UpstreamRevisionsJSON: string(upstreamRevisionsJSON),
		ModelRequestIdentity:  draft.ModelRequestIdentity,
		SkillVersionsJSON:     string(skillVersionsJSON),
		CreatedByRunID:        scope.RunID,
		CreatedBySpecialistID: createdBySpecialistID,
		LifecycleStatus:       model.AgentArtifactRevisionAwaitingReview,
		CreatedAt:             now,
	}

	if err := validateArtifactRevisionRefsInScope(tx, scope, draft.UpstreamRevisions); err != nil {
		return nil, err
	}
	if artifactDraftRequiresCurrentUpstreamHeads(draft) {
		if err := validateArtifactRevisionHeadsInScope(tx, scope, draft.UpstreamRevisions); err != nil {
			return nil, err
		}
	}
	var artifact model.AgentArtifact
	queryErr := productionArtifactScopeQuery(tx.Clauses(clause.Locking{Strength: "UPDATE"}), scope).
		Where("id = ?", artifactID).
		First(&artifact).Error
	if errors.Is(queryErr, gorm.ErrRecordNotFound) {
		if expectedRevision != 0 {
			return nil, ErrArtifactRevisionConflict
		}
		artifact = model.AgentArtifact{
			ID:              artifactID,
			TenantKind:      scope.TenantKind,
			TenantID:        scope.TenantID,
			ActorUserID:     scope.ActorUserID,
			DomainProjectID: scope.DomainProjectID,
			CanvasID:        scope.CanvasID,
			ThreadID:        scope.ThreadID,
			RunID:           scope.RunID,
			ArtifactKey:     draft.ArtifactKey,
			Kind:            draft.Kind,
			HeadRevision:    0,
			LifecycleStatus: model.AgentArtifactLifecycleActive,
			Version:         1,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if createErr := tx.Create(&artifact).Error; createErr != nil {
			if isUniqueConstraintError(createErr) {
				return nil, ErrArtifactRevisionConflict
			}
			return nil, createErr
		}
	} else if queryErr != nil {
		return nil, queryErr
	}
	if artifact.ArtifactKey != draft.ArtifactKey || artifact.Kind != draft.Kind ||
		artifact.LifecycleStatus != model.AgentArtifactLifecycleActive || artifact.HeadRevision != expectedRevision {
		return nil, ErrArtifactRevisionConflict
	}

	artifactUpdate := model.AgentArtifact{
		HeadRevision: expectedRevision + 1,
		Version:      artifact.Version + 1,
		UpdatedAt:    now,
	}
	updateResult := productionArtifactScopeQuery(tx.Model(&model.AgentArtifact{}), scope).
		Where("id = ? AND head_revision = ? AND version = ?", artifact.ID, expectedRevision, artifact.Version).
		Select("head_revision", "version", "updated_at").
		Updates(&artifactUpdate)
	if updateResult.Error != nil {
		return nil, updateResult.Error
	}
	if updateResult.RowsAffected != 1 {
		return nil, ErrArtifactRevisionConflict
	}
	if createErr := tx.Create(revision).Error; createErr != nil {
		if isUniqueConstraintError(createErr) {
			return nil, ErrArtifactRevisionConflict
		}
		return nil, createErr
	}
	if expectedRevision > 0 {
		var previous model.AgentArtifactRevision
		if err := productionArtifactRevisionScopeQuery(tx, scope).
			Where("artifact_id = ? AND revision = ?", artifactID, expectedRevision).
			Take(&previous).Error; err != nil {
			return nil, err
		}
		if err := markDependentVisualEvidenceStaleTx(tx, scope, agentruntime.ArtifactRevisionRef{
			ArtifactID: previous.ArtifactID,
			RevisionID: previous.ID,
		}); err != nil {
			return nil, err
		}
	}
	return revision, nil
}

func artifactDraftRequiresCurrentUpstreamHeads(draft agentruntime.ArtifactDraft) bool {
	switch draft.SchemaID() {
	case agentruntime.ArtifactSchemaVisualEvidenceV1,
		agentruntime.ArtifactSchemaVisualConsistencyReviewV1,
		agentruntime.ArtifactSchemaMediaCandidateSelectionV1:
		return true
	default:
		return false
	}
}

func markDependentVisualEvidenceStaleTx(
	tx *gorm.DB,
	scope agentruntime.Scope,
	source agentruntime.ArtifactRevisionRef,
) error {
	var candidates []model.AgentArtifactRevision
	if err := productionArtifactRevisionScopeQuery(tx, scope).
		Where("kind = ? AND lifecycle_status <> ?", "visual_evidence", model.AgentArtifactRevisionStale).
		Find(&candidates).Error; err != nil {
		return err
	}
	staleIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		var upstream []agentruntime.ArtifactRevisionRef
		if err := json.Unmarshal([]byte(candidate.UpstreamRevisionsJSON), &upstream); err != nil {
			return fmt.Errorf("decode visual evidence upstream revisions: %w", err)
		}
		for _, reference := range upstream {
			if reference == source {
				staleIDs = append(staleIDs, candidate.ID)
				break
			}
		}
	}
	if len(staleIDs) == 0 {
		return nil
	}
	result := productionArtifactRevisionScopeQuery(tx.Model(&model.AgentArtifactRevision{}), scope).
		Where("id IN ? AND lifecycle_status <> ?", staleIDs, model.AgentArtifactRevisionStale).
		Update("lifecycle_status", model.AgentArtifactRevisionStale)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != int64(len(staleIDs)) {
		return fmt.Errorf("mark visual evidence stale: expected %d revisions, updated %d", len(staleIDs), result.RowsAffected)
	}
	return nil
}

func appendUnadoptedArtifactRevisionTx(
	tx *gorm.DB,
	scope agentruntime.Scope,
	artifactID string,
	draft agentruntime.ArtifactDraft,
	createdBySpecialistID string,
	now time.Time,
) (*model.AgentArtifactRevision, error) {
	if strings.TrimSpace(artifactID) != artifactID || artifactID == "" || len(artifactID) > 80 ||
		strings.TrimSpace(createdBySpecialistID) != createdBySpecialistID || len(createdBySpecialistID) > 80 || now.IsZero() {
		return nil, ErrArtifactRevisionConflict
	}
	if err := agentruntime.ValidateArtifactDraft(draft); err != nil {
		return nil, err
	}
	if draft.UpstreamRevisions == nil {
		draft.UpstreamRevisions = []agentruntime.ArtifactRevisionRef{}
	}
	if draft.SkillVersions == nil {
		draft.SkillVersions = []agentruntime.SkillSelection{}
	}
	if err := validateArtifactRevisionRefsInScope(tx, scope, draft.UpstreamRevisions); err != nil {
		return nil, err
	}
	upstreamRevisionsJSON, err := json.Marshal(draft.UpstreamRevisions)
	if err != nil {
		return nil, fmt.Errorf("encode unadopted artifact upstream revisions: %w", err)
	}
	skillVersionsJSON, err := json.Marshal(draft.SkillVersions)
	if err != nil {
		return nil, fmt.Errorf("encode unadopted artifact skill versions: %w", err)
	}

	var artifact model.AgentArtifact
	queryErr := productionArtifactScopeQuery(tx.Clauses(clause.Locking{Strength: "UPDATE"}), scope).Where("id = ?", artifactID).First(&artifact).Error
	if errors.Is(queryErr, gorm.ErrRecordNotFound) {
		artifact = model.AgentArtifact{
			ID: artifactID, TenantKind: scope.TenantKind, TenantID: scope.TenantID,
			ActorUserID: scope.ActorUserID, DomainProjectID: scope.DomainProjectID, CanvasID: scope.CanvasID,
			ThreadID: scope.ThreadID, RunID: scope.RunID, ArtifactKey: draft.ArtifactKey, Kind: draft.Kind,
			HeadRevision: 0, LifecycleStatus: model.AgentArtifactLifecycleUnadopted, Version: 1,
			CreatedAt: now, UpdatedAt: now,
		}
		if createErr := tx.Create(&artifact).Error; createErr != nil {
			if isUniqueConstraintError(createErr) {
				return nil, ErrArtifactRevisionConflict
			}
			return nil, createErr
		}
	} else if queryErr != nil {
		return nil, queryErr
	}
	if artifact.ArtifactKey != draft.ArtifactKey || artifact.Kind != draft.Kind || artifact.Version < 1 ||
		(artifact.LifecycleStatus != model.AgentArtifactLifecycleActive && artifact.LifecycleStatus != model.AgentArtifactLifecycleUnadopted) ||
		(artifact.LifecycleStatus == model.AgentArtifactLifecycleUnadopted && artifact.HeadRevision != 0) {
		return nil, ErrArtifactRevisionConflict
	}

	var maximumRevision struct {
		Revision int64 `gorm:"column:revision"`
	}
	if err := productionArtifactRevisionScopeQuery(tx, scope).
		Select("COALESCE(MAX(revision), 0) AS revision").Where("artifact_id = ?", artifactID).
		Scan(&maximumRevision).Error; err != nil {
		return nil, err
	}
	nextRevision := maximumRevision.Revision + 1
	revision := &model.AgentArtifactRevision{
		ID: productionArtifactRevisionID(scope, artifactID, nextRevision), TenantKind: scope.TenantKind, TenantID: scope.TenantID,
		ActorUserID: scope.ActorUserID, DomainProjectID: scope.DomainProjectID, CanvasID: scope.CanvasID,
		ThreadID: scope.ThreadID, RunID: scope.RunID, ArtifactID: artifactID, ArtifactKey: draft.ArtifactKey,
		Revision: nextRevision, Kind: draft.Kind, SchemaVersion: draft.SchemaVersion, PayloadJSON: string(draft.Payload),
		ResourceID: draft.ResourceID, UpstreamRevisionsJSON: string(upstreamRevisionsJSON), ModelRequestIdentity: draft.ModelRequestIdentity,
		SkillVersionsJSON: string(skillVersionsJSON), CreatedByRunID: scope.RunID, CreatedBySpecialistID: createdBySpecialistID,
		LifecycleStatus: model.AgentArtifactRevisionUnadopted, CreatedAt: now,
	}
	if createErr := tx.Create(revision).Error; createErr != nil {
		if isUniqueConstraintError(createErr) {
			return nil, ErrArtifactRevisionConflict
		}
		return nil, createErr
	}
	return revision, nil
}

func validateArtifactRevisionRefsInScope(db *gorm.DB, scope agentruntime.Scope, references []agentruntime.ArtifactRevisionRef) error {
	if len(references) == 0 {
		return nil
	}
	revisionIDs := make([]string, 0, len(references))
	for _, reference := range references {
		revisionIDs = append(revisionIDs, reference.RevisionID)
	}
	var revisions []model.AgentArtifactRevision
	if err := productionArtifactRevisionScopeQuery(db, scope).
		Where("id IN ?", revisionIDs).
		Find(&revisions).Error; err != nil {
		return err
	}
	if len(revisions) != len(references) {
		return gorm.ErrRecordNotFound
	}
	artifactByRevisionID := make(map[string]string, len(revisions))
	for _, revision := range revisions {
		artifactByRevisionID[revision.ID] = revision.ArtifactID
	}
	for _, reference := range references {
		if artifactByRevisionID[reference.RevisionID] != reference.ArtifactID {
			return gorm.ErrRecordNotFound
		}
	}
	return nil
}

func validateArtifactRevisionHeadsInScope(
	db *gorm.DB,
	scope agentruntime.Scope,
	references []agentruntime.ArtifactRevisionRef,
) error {
	for _, reference := range references {
		var artifact model.AgentArtifact
		if err := productionArtifactScopeQuery(db, scope).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", reference.ArtifactID).
			Take(&artifact).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrArtifactUpstreamRevisionStale
			}
			return err
		}
		if artifact.LifecycleStatus != model.AgentArtifactLifecycleActive || artifact.HeadRevision < 1 {
			return ErrArtifactUpstreamRevisionStale
		}

		var revision model.AgentArtifactRevision
		if err := productionArtifactRevisionScopeQuery(db, scope).
			Where("artifact_id = ? AND id = ?", reference.ArtifactID, reference.RevisionID).
			Take(&revision).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrArtifactUpstreamRevisionStale
			}
			return err
		}
		if revision.Revision != artifact.HeadRevision || revision.LifecycleStatus == model.AgentArtifactRevisionStale {
			return ErrArtifactUpstreamRevisionStale
		}
	}
	return nil
}

func (r *Repository) ArtifactRevisionInScope(scope agentruntime.Scope, revisionID string) (*model.AgentArtifactRevision, error) {
	if err := validateProductionRepositoryScope(scope, false); err != nil {
		return nil, err
	}
	if strings.TrimSpace(revisionID) != revisionID || revisionID == "" || len(revisionID) > 80 {
		return nil, gorm.ErrRecordNotFound
	}
	var revision model.AgentArtifactRevision
	if err := productionArtifactRevisionScopeQuery(r.db, scope).
		Where("id = ?", revisionID).
		First(&revision).Error; err != nil {
		return nil, err
	}
	return &revision, nil
}

func (r *Repository) ArtifactRevisionForArtifactInScope(
	scope agentruntime.Scope,
	artifactID string,
	revisionID string,
) (*model.AgentArtifactRevision, error) {
	if err := validateProductionRepositoryScope(scope, false); err != nil {
		return nil, err
	}
	if strings.TrimSpace(artifactID) != artifactID || artifactID == "" || len(artifactID) > 80 ||
		strings.TrimSpace(revisionID) != revisionID || revisionID == "" || len(revisionID) > 80 {
		return nil, gorm.ErrRecordNotFound
	}
	var revision model.AgentArtifactRevision
	if err := productionArtifactRevisionScopeQuery(r.db, scope).
		Where("artifact_id = ? AND id = ?", artifactID, revisionID).
		First(&revision).Error; err != nil {
		return nil, err
	}
	return &revision, nil
}

func (r *Repository) ArtifactHeadRevisionForScope(scope agentruntime.Scope, artifactID string) (*model.AgentArtifactRevision, error) {
	if err := validateProductionRepositoryScope(scope, false); err != nil {
		return nil, err
	}
	if strings.TrimSpace(artifactID) != artifactID || artifactID == "" || len(artifactID) > 80 {
		return nil, gorm.ErrRecordNotFound
	}
	var artifact model.AgentArtifact
	if err := productionArtifactScopeQuery(r.db, scope).Where("id = ?", artifactID).First(&artifact).Error; err != nil {
		return nil, err
	}
	if artifact.HeadRevision < 1 {
		return nil, gorm.ErrRecordNotFound
	}
	var revision model.AgentArtifactRevision
	if err := productionArtifactRevisionScopeQuery(r.db, scope).
		Where("artifact_id = ? AND revision = ?", artifact.ID, artifact.HeadRevision).
		First(&revision).Error; err != nil {
		return nil, err
	}
	return &revision, nil
}

func (r *Repository) AppendAgentAssetBindingRevision(
	scope agentruntime.Scope,
	createdBySpecialistID string,
	bindings agentruntime.AssetBindingSet,
	now time.Time,
) (*model.AgentAssetBindingRevision, error) {
	if err := validateProductionRepositoryScope(scope, true); err != nil {
		return nil, err
	}
	if strings.TrimSpace(createdBySpecialistID) != createdBySpecialistID || createdBySpecialistID == "" ||
		len(createdBySpecialistID) > 80 || now.IsZero() || !bindings.Confirmed {
		return nil, ErrAssetBindingRevisionConflict
	}
	bindingsJSON, err := json.Marshal(bindings)
	if err != nil {
		return nil, err
	}
	upstream := []agentruntime.ArtifactRevisionRef{bindings.ScriptRevision}
	upstreamJSON, err := json.Marshal(upstream)
	if err != nil {
		return nil, err
	}
	if err := agentruntime.ValidateArtifactDraft(agentruntime.ArtifactDraft{
		ArtifactKey: bindings.BindingKey, Kind: "asset_binding", SchemaVersion: 1,
		Payload: bindingsJSON, UpstreamRevisions: upstream, SkillVersions: []agentruntime.SkillSelection{},
	}); err != nil {
		return nil, err
	}

	var stored model.AgentAssetBindingRevision
	err = r.db.Transaction(func(tx *gorm.DB) error {
		if err := validateArtifactRevisionRefsInScope(tx, scope, upstream); err != nil {
			return err
		}
		var latest model.AgentAssetBindingRevision
		queryErr := productionAssetBindingScopeQuery(tx, scope).
			Where("binding_key = ?", bindings.BindingKey).Order("revision DESC").First(&latest).Error
		if queryErr == nil && latest.BindingsJSON == string(bindingsJSON) && latest.UpstreamRevisionsJSON == string(upstreamJSON) &&
			latest.CreatedBySpecialistID == createdBySpecialistID && latest.LifecycleStatus == model.AgentAssetBindingRevisionConfirmed {
			stored = latest
			return nil
		}
		if queryErr != nil && !errors.Is(queryErr, gorm.ErrRecordNotFound) {
			return queryErr
		}
		nextRevision := int64(1)
		if queryErr == nil {
			nextRevision = latest.Revision + 1
		}
		stored = model.AgentAssetBindingRevision{
			ID: agentFactID(
				"asset-binding-revision", string(scope.TenantKind), scope.TenantID, scope.ActorUserID,
				scope.DomainProjectID, scope.CanvasID, scope.ThreadID, scope.RunID, bindings.BindingKey, fmt.Sprintf("%d", nextRevision),
			),
			TenantKind: scope.TenantKind, TenantID: scope.TenantID, ActorUserID: scope.ActorUserID,
			DomainProjectID: scope.DomainProjectID, CanvasID: scope.CanvasID, ThreadID: scope.ThreadID, RunID: scope.RunID,
			BindingKey: bindings.BindingKey, Revision: nextRevision, BindingsJSON: string(bindingsJSON),
			UpstreamRevisionsJSON: string(upstreamJSON), CreatedBySpecialistID: createdBySpecialistID,
			LifecycleStatus: model.AgentAssetBindingRevisionConfirmed, CreatedAt: now,
		}
		if err := tx.Create(&stored).Error; err != nil {
			if isUniqueConstraintError(err) {
				return ErrAssetBindingRevisionConflict
			}
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &stored, nil
}

func productionArtifactScopeQuery(query *gorm.DB, scope agentruntime.Scope) *gorm.DB {
	return query.Where(
		"agent_artifacts.tenant_kind = ? AND agent_artifacts.tenant_id = ? AND agent_artifacts.actor_user_id = ? AND agent_artifacts.domain_project_id = ? AND agent_artifacts.canvas_id = ? AND agent_artifacts.thread_id = ? AND agent_artifacts.run_id = ?",
		scope.TenantKind, scope.TenantID, scope.ActorUserID, scope.DomainProjectID, scope.CanvasID, scope.ThreadID, scope.RunID,
	)
}

func productionArtifactRevisionScopeQuery(query *gorm.DB, scope agentruntime.Scope) *gorm.DB {
	return query.Model(&model.AgentArtifactRevision{}).Where(
		"agent_artifact_revisions.tenant_kind = ? AND agent_artifact_revisions.tenant_id = ? AND agent_artifact_revisions.actor_user_id = ? AND agent_artifact_revisions.domain_project_id = ? AND agent_artifact_revisions.canvas_id = ? AND agent_artifact_revisions.thread_id = ? AND agent_artifact_revisions.run_id = ?",
		scope.TenantKind, scope.TenantID, scope.ActorUserID, scope.DomainProjectID, scope.CanvasID, scope.ThreadID, scope.RunID,
	)
}

func productionAssetBindingScopeQuery(query *gorm.DB, scope agentruntime.Scope) *gorm.DB {
	return query.Model(&model.AgentAssetBindingRevision{}).Where(
		"agent_asset_binding_revisions.tenant_kind = ? AND agent_asset_binding_revisions.tenant_id = ? AND agent_asset_binding_revisions.actor_user_id = ? AND agent_asset_binding_revisions.domain_project_id = ? AND agent_asset_binding_revisions.canvas_id = ? AND agent_asset_binding_revisions.thread_id = ? AND agent_asset_binding_revisions.run_id = ?",
		scope.TenantKind, scope.TenantID, scope.ActorUserID, scope.DomainProjectID, scope.CanvasID, scope.ThreadID, scope.RunID,
	)
}

func productionArtifactRevisionID(scope agentruntime.Scope, artifactID string, revision int64) string {
	return agentFactID(
		"production-artifact-revision",
		string(scope.TenantKind),
		scope.TenantID,
		scope.ActorUserID,
		scope.DomainProjectID,
		scope.CanvasID,
		scope.ThreadID,
		scope.RunID,
		artifactID,
		fmt.Sprintf("%d", revision),
	)
}
