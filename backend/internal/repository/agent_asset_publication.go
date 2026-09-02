package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrAgentAssetPublicationInvalid          = errors.New("agent asset publication is invalid")
	ErrAgentAssetPublicationApprovalRequired = errors.New("agent asset publication approval is required")
	ErrAgentAssetPublicationResourceMissing  = errors.New("agent asset publication resource is unavailable")
	ErrAgentAssetPublicationBillingMissing   = errors.New("agent asset publication billing facts are unavailable")
	ErrAgentAssetPublicationConflict         = errors.New("agent asset publication conflicts with stored facts")
	ErrAgentAssetPublicationFailed           = errors.New("agent asset publication persistence failed")
)

type PublishAgentAssetInput struct {
	Context                      context.Context
	Scope                        agentruntime.Scope
	AuthorizationKind            AgentAssetPublicationAuthorizationKind
	ArtifactRevisionID           string
	ReviewRevisionID             string
	CandidateSelectionRevisionID string
	PublicationPurpose           string
	TargetCategory               model.AssetCategory
	TargetBindingKey             string
	ApprovedByUserID             string
	StageReviewID                string
	Now                          time.Time
}

type PublishedAgentAsset struct {
	Publication      model.AgentAssetPublication
	Asset            model.Asset
	AssetVersion     model.AssetVersion
	ProjectAssetLink model.ProjectAssetLink
	Representation   model.AssetRepresentation
	Stage            model.AgentProductionStage
	Replayed         bool
}

func (r *Repository) SucceededAgentAssetPublicationsForScope(scope agentruntime.Scope) ([]model.AgentAssetPublication, error) {
	if err := validateProductionRepositoryScope(scope, false); err != nil {
		return nil, err
	}
	var publications []model.AgentAssetPublication
	err := r.db.Where(`tenant_kind = ? AND tenant_id = ? AND actor_user_id = ? AND domain_project_id = ?
		AND canvas_id = ? AND thread_id = ? AND run_id = ? AND status = ?`,
		scope.TenantKind, scope.TenantID, scope.ActorUserID, scope.DomainProjectID,
		scope.CanvasID, scope.ThreadID, scope.RunID, model.AgentAssetPublicationSucceeded).
		Order("created_at ASC, id ASC").Find(&publications).Error
	return publications, err
}

func (r *Repository) PublishAgentAsset(input PublishAgentAssetInput) (*PublishedAgentAsset, error) {
	if err := validatePublishAgentAssetInput(input); err != nil {
		return nil, err
	}
	db := r.db.WithContext(input.Context)
	var result PublishedAgentAsset
	var failureCandidate model.AgentAssetPublication
	err := db.Transaction(func(tx *gorm.DB) error {
		if _, err := New(tx).ProjectEditableForUser(input.ApprovedByUserID, input.Scope.DomainProjectID, input.Now); err != nil {
			return ErrAgentAssetPublicationApprovalRequired
		}

		authorization, err := loadAssetPublicationAuthorizationTx(tx, input)
		if err != nil {
			return err
		}
		revision, resource, provenance, err := loadAssetPublicationFactsTx(tx, input, authorization)
		if err != nil {
			return err
		}
		auditJSON, err := buildAgentAssetPublicationAuditJSON(input, authorization, revision, resource, provenance)
		if err != nil {
			return err
		}
		publicationID := agentFactID("asset-publication", string(input.Scope.TenantKind), input.Scope.TenantID,
			input.Scope.DomainProjectID, revision.ID, input.PublicationPurpose)
		failureCandidate = model.AgentAssetPublication{
			ID: publicationID, TenantKind: input.Scope.TenantKind, TenantID: input.Scope.TenantID,
			ActorUserID: input.Scope.ActorUserID, DomainProjectID: input.Scope.DomainProjectID,
			CanvasID: input.Scope.CanvasID, ThreadID: input.Scope.ThreadID, RunID: input.Scope.RunID,
			ArtifactRevisionID: revision.ID, PublicationPurpose: input.PublicationPurpose,
			ApprovedByUserID: input.ApprovedByUserID, AuditJSON: auditJSON,
			Status: model.AgentAssetPublicationPending, Version: 1, CreatedAt: input.Now, UpdatedAt: input.Now,
		}
		existing, found, err := loadAgentAssetPublicationTx(tx, input, publicationID)
		if err != nil {
			return err
		}
		if found {
			if !sameAgentAssetPublicationIdentity(existing, failureCandidate) {
				return ErrAgentAssetPublicationConflict
			}
			failureCandidate = existing
			if existing.Status == model.AgentAssetPublicationSucceeded {
				if err := loadPublishedAgentAssetTx(tx, existing, &result); err != nil {
					return err
				}
				result.Replayed = true
				return nil
			}
			if existing.Status != model.AgentAssetPublicationFailed {
				return ErrAgentAssetPublicationConflict
			}
		}
		if !found {
			if err := requireActiveProductionAgentRunTx(tx, input.Scope); err != nil {
				return err
			}
		}

		assetID := agentAssetRecordID("asset", publicationID)
		assetVersionID := agentAssetRecordID("asset-version", publicationID)
		projectAssetLinkID := agentAssetRecordID("project-asset-link", publicationID)
		representationID := agentAssetRecordID("asset-representation", publicationID)
		assetPayload, definitionJSON, metadataJSON, err := buildPublishedAssetJSON(input, revision, resource, auditJSON)
		if err != nil {
			return err
		}
		asset := model.Asset{
			ID: assetID, UserID: input.Scope.ActorUserID, Kind: resource.Kind, Category: input.TargetCategory,
			Status: model.AssetVersionStatusConfirmed, PrimaryVersionID: assetVersionID, Title: revision.ArtifactKey,
			PayloadJSON: assetPayload, CreatedAt: input.Now, UpdatedAt: input.Now,
		}
		version := model.AssetVersion{
			ID: assetVersionID, AssetID: assetID, Version: 1, Status: model.AssetVersionStatusConfirmed,
			DefinitionJSON: definitionJSON, Prompt: provenance.Task.Prompt, Note: "Agent approved artifact revision " + revision.ID,
			CreatedAt: input.Now, UpdatedAt: input.Now,
		}
		link := model.ProjectAssetLink{
			ID: projectAssetLinkID, ProjectID: input.Scope.DomainProjectID, AssetID: assetID, CreatedAt: input.Now,
		}
		representation := model.AssetRepresentation{
			ID: representationID, TaskID: provenance.Task.ID, AssetVersionID: assetVersionID, ResourceID: resource.ID,
			MediaType: resource.Kind, Role: "primary", MetadataJSON: metadataJSON, CreatedAt: input.Now,
		}
		for _, create := range []func() error{
			func() error { return tx.Create(&asset).Error },
			func() error { return tx.Create(&version).Error },
			func() error { return tx.Create(&link).Error },
			func() error { return tx.Create(&representation).Error },
		} {
			if err := create(); err != nil {
				if isUniqueConstraintError(err) {
					return ErrAgentAssetPublicationConflict
				}
				return err
			}
		}
		completedAt := input.Now
		publication := failureCandidate
		publication.AssetID = assetID
		publication.AssetVersionID = assetVersionID
		publication.ProjectAssetLinkID = projectAssetLinkID
		publication.RepresentationID = representationID
		publication.Status = model.AgentAssetPublicationSucceeded
		publication.LastErrorCode = ""
		publication.CompletedAt = &completedAt
		publication.UpdatedAt = input.Now
		if found {
			publication.Version = existing.Version + 1
			update := tx.Model(&model.AgentAssetPublication{}).
				Where("id = ? AND status = ? AND version = ?", existing.ID, model.AgentAssetPublicationFailed, existing.Version).
				Select("asset_id", "asset_version_id", "project_asset_link_id", "representation_id", "status", "version", "last_error_code", "completed_at", "updated_at").
				Updates(&publication)
			if update.Error != nil || update.RowsAffected != 1 {
				return ErrAgentAssetPublicationConflict
			}
		} else if err := tx.Create(&publication).Error; err != nil {
			if isUniqueConstraintError(err) {
				return ErrAgentAssetPublicationConflict
			}
			return err
		}
		stageUpdate := productionStageScopeQuery(tx, input.Scope).
			Where("id = ? AND status = ? AND review_revision_id = ? AND version = ?", authorization.Approval.StageID,
				agentruntime.StageApproved, authorization.ReviewRevision.ID, authorization.Approval.ResultStageVersion).
			Select("status", "version", "updated_at").
			Updates(&model.AgentProductionStage{Status: agentruntime.StageCompleted, Version: authorization.Approval.ResultStageVersion + 1, UpdatedAt: input.Now})
		if stageUpdate.Error != nil || stageUpdate.RowsAffected != 1 {
			return ErrAgentAssetPublicationConflict
		}
		if err := productionStageScopeQuery(tx, input.Scope).Where("id = ?", authorization.Approval.StageID).First(&result.Stage).Error; err != nil {
			return err
		}
		result.Publication = publication
		result.Asset = asset
		result.AssetVersion = version
		result.ProjectAssetLink = link
		result.Representation = representation
		return nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		if errors.Is(err, ErrAgentAssetPublicationConflict) && failureCandidate.ID != "" {
			replayed, replayErr := r.replayPublishedAgentAsset(input, failureCandidate)
			if replayErr == nil {
				if appendErr := r.appendAgentAssetPublicationSuccess(input, *replayed); appendErr != nil {
					return nil, errors.Join(ErrAgentAssetPublicationFailed, appendErr)
				}
				return replayed, nil
			}
		}
		if failureCandidate.ID == "" || isAgentAssetPublicationDomainError(err) {
			return nil, err
		}
		if recordErr := r.recordAgentAssetPublicationFailure(input, failureCandidate, "asset_publication_persistence_failed"); recordErr != nil {
			return nil, errors.Join(ErrAgentAssetPublicationFailed, err, recordErr)
		}
		return nil, errors.Join(ErrAgentAssetPublicationFailed, err)
	}
	if err := r.appendAgentAssetPublicationSuccess(input, result); err != nil {
		return nil, errors.Join(ErrAgentAssetPublicationFailed, err)
	}
	return &result, nil
}

func validatePublishAgentAssetInput(input PublishAgentAssetInput) error {
	if input.Context == nil || input.Now.IsZero() || validateProductionRepositoryScope(input.Scope, true) != nil ||
		!input.AuthorizationKind.Valid() ||
		!validAgentPublicationText(input.ArtifactRevisionID, 80) || !validAgentPublicationText(input.PublicationPurpose, 120) ||
		!validAgentPublicationCategory(input.TargetCategory) || !validAgentPublicationText(input.TargetBindingKey, 120) ||
		input.ApprovedByUserID != input.Scope.ActorUserID || !validAgentPublicationText(input.StageReviewID, 80) ||
		!validAgentPublicationText(input.ReviewRevisionID, 80) ||
		(input.AuthorizationKind == AgentAssetPublicationDirectReview && input.CandidateSelectionRevisionID != "") ||
		(input.AuthorizationKind == AgentAssetPublicationCandidateSelection && !validAgentPublicationText(input.CandidateSelectionRevisionID, 80)) {
		return ErrAgentAssetPublicationInvalid
	}
	select {
	case <-input.Context.Done():
		return input.Context.Err()
	default:
		return nil
	}
}

func loadAssetPublicationFactsTx(
	tx *gorm.DB,
	input PublishAgentAssetInput,
	authorization agentAssetPublicationAuthorization,
) (model.AgentArtifactRevision, model.Resource, agentAssetPublicationProvenance, error) {
	var revision model.AgentArtifactRevision
	if err := productionArtifactRevisionScopeQuery(tx, input.Scope).Where("id = ?", input.ArtifactRevisionID).First(&revision).Error; err != nil {
		return revision, model.Resource{}, agentAssetPublicationProvenance{}, ErrAgentAssetPublicationResourceMissing
	}
	if revision.ResourceID == "" || revision.ModelRequestIdentity == "" ||
		revision.LifecycleStatus != model.AgentArtifactRevisionAwaitingReview {
		return revision, model.Resource{}, agentAssetPublicationProvenance{}, ErrAgentAssetPublicationResourceMissing
	}
	var resource model.Resource
	query := tx.Where("id = ? AND status = ?", revision.ResourceID, model.ResourceStatusReady)
	if input.Scope.TenantKind == agentruntime.TenantTeam {
		query = query.Where("team_id = ?", input.Scope.TenantID)
	} else {
		query = query.Where("user_id = ? AND team_id = ''", input.Scope.ActorUserID)
	}
	if err := query.First(&resource).Error; err != nil || (resource.Kind != "image" && resource.Kind != "audio") {
		return revision, resource, agentAssetPublicationProvenance{}, ErrAgentAssetPublicationResourceMissing
	}
	provenance, err := loadAgentAssetPublicationProvenanceTx(tx, input, authorization, revision, resource)
	if err != nil {
		return revision, resource, agentAssetPublicationProvenance{}, err
	}
	return revision, resource, provenance, nil
}

func loadAgentAssetPublicationTx(tx *gorm.DB, input PublishAgentAssetInput, publicationID string) (model.AgentAssetPublication, bool, error) {
	var publication model.AgentAssetPublication
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_kind = ? AND tenant_id = ? AND domain_project_id = ? AND artifact_revision_id = ? AND publication_purpose = ?",
			input.Scope.TenantKind, input.Scope.TenantID, input.Scope.DomainProjectID, input.ArtifactRevisionID, input.PublicationPurpose).
		First(&publication).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return publication, false, nil
	}
	if err != nil {
		return publication, false, err
	}
	if publication.ID != publicationID {
		return publication, false, ErrAgentAssetPublicationConflict
	}
	return publication, true, nil
}

func loadPublishedAgentAssetTx(tx *gorm.DB, publication model.AgentAssetPublication, result *PublishedAgentAsset) error {
	if publication.AssetID == "" || publication.AssetVersionID == "" || publication.ProjectAssetLinkID == "" || publication.RepresentationID == "" {
		return ErrAgentAssetPublicationConflict
	}
	result.Publication = publication
	for _, load := range []func() error{
		func() error { return tx.First(&result.Asset, "id = ?", publication.AssetID).Error },
		func() error { return tx.First(&result.AssetVersion, "id = ?", publication.AssetVersionID).Error },
		func() error {
			return tx.First(&result.ProjectAssetLink, "id = ?", publication.ProjectAssetLinkID).Error
		},
		func() error { return tx.First(&result.Representation, "id = ?", publication.RepresentationID).Error },
	} {
		if err := load(); err != nil {
			return ErrAgentAssetPublicationConflict
		}
	}
	var audit agentAssetPublicationAudit
	if err := json.Unmarshal([]byte(publication.AuditJSON), &audit); err != nil || validateAgentAssetPublicationAudit(audit) != nil {
		return ErrAgentAssetPublicationConflict
	}
	if result.Asset.ID != publication.AssetID || result.Asset.UserID != publication.ActorUserID ||
		result.Asset.Kind != result.Representation.MediaType || result.Asset.Category != audit.Approval.TargetCategory ||
		result.Asset.Status != model.AssetVersionStatusConfirmed || result.Asset.PrimaryVersionID != publication.AssetVersionID ||
		result.AssetVersion.ID != publication.AssetVersionID || result.AssetVersion.AssetID != publication.AssetID ||
		result.AssetVersion.Version != 1 || result.AssetVersion.Status != model.AssetVersionStatusConfirmed ||
		result.ProjectAssetLink.ID != publication.ProjectAssetLinkID || result.ProjectAssetLink.ProjectID != publication.DomainProjectID ||
		result.ProjectAssetLink.AssetID != publication.AssetID || result.Representation.ID != publication.RepresentationID ||
		result.Representation.AssetVersionID != publication.AssetVersionID || result.Representation.ResourceID != audit.ArtifactRevision.ResourceID ||
		result.Representation.TaskID != audit.ModelRequest.TaskID || result.Representation.Role != "primary" ||
		(result.Representation.MediaType != "image" && result.Representation.MediaType != "audio") ||
		audit.Approval.StageReviewID == "" || audit.Approval.ApprovedByUserID != publication.ApprovedByUserID ||
		audit.Approval.PublicationPurpose != publication.PublicationPurpose || audit.ArtifactRevision.ArtifactRevisionID != publication.ArtifactRevisionID {
		return ErrAgentAssetPublicationConflict
	}
	stageID, err := stageIDForReviewItemTx(tx, audit.Approval.StageReviewID)
	if err != nil {
		return err
	}
	if err := productionStageScopeQuery(tx, agentruntime.Scope{
		TenantKind: publication.TenantKind, TenantID: publication.TenantID, ActorUserID: publication.ActorUserID,
		DomainProjectID: publication.DomainProjectID, CanvasID: publication.CanvasID,
		ThreadID: publication.ThreadID, RunID: publication.RunID,
	}).Where("id = ?", stageID).First(&result.Stage).Error; err != nil ||
		result.Stage.Status != agentruntime.StageCompleted || result.Stage.ReviewRevisionID != audit.Authorization.ReviewRevisionID {
		return ErrAgentAssetPublicationConflict
	}
	return nil
}

func stageIDForReviewItemTx(tx *gorm.DB, reviewID string) (string, error) {
	var item model.AgentTimelineItem
	if err := tx.Where("id = ?", reviewID).First(&item).Error; err != nil {
		return "", ErrAgentAssetPublicationConflict
	}
	content, err := agentruntime.DecodeStageReviewResolutionContent([]byte(item.ContentJSON))
	if err != nil || item.Kind != model.AgentTimelineItemApproval || item.Status != model.AgentTimelineItemCompleted ||
		content.Decision != agentruntime.StageReviewApprove || content.StageID == "" {
		return "", ErrAgentAssetPublicationConflict
	}
	return content.StageID, nil
}

func (r *Repository) replayPublishedAgentAsset(
	input PublishAgentAssetInput,
	candidate model.AgentAssetPublication,
) (*PublishedAgentAsset, error) {
	var result PublishedAgentAsset
	err := r.db.WithContext(input.Context).Transaction(func(tx *gorm.DB) error {
		stored, found, err := loadAgentAssetPublicationTx(tx, input, candidate.ID)
		if err != nil {
			return err
		}
		if !found || stored.Status != model.AgentAssetPublicationSucceeded || !sameAgentAssetPublicationIdentity(stored, candidate) {
			return ErrAgentAssetPublicationConflict
		}
		if err := loadPublishedAgentAssetTx(tx, stored, &result); err != nil {
			return err
		}
		result.Replayed = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func sameAgentAssetPublicationIdentity(stored model.AgentAssetPublication, candidate model.AgentAssetPublication) bool {
	return stored.ID == candidate.ID && stored.TenantKind == candidate.TenantKind && stored.TenantID == candidate.TenantID &&
		stored.ActorUserID == candidate.ActorUserID && stored.DomainProjectID == candidate.DomainProjectID &&
		stored.CanvasID == candidate.CanvasID && stored.ThreadID == candidate.ThreadID && stored.RunID == candidate.RunID &&
		stored.ArtifactRevisionID == candidate.ArtifactRevisionID && stored.PublicationPurpose == candidate.PublicationPurpose &&
		stored.ApprovedByUserID == candidate.ApprovedByUserID && stored.AuditJSON == candidate.AuditJSON
}

func validAgentPublicationText(value string, limit int) bool {
	return strings.TrimSpace(value) == value && value != "" && len(value) <= limit
}

func validAgentPublicationCategory(category model.AssetCategory) bool {
	switch category {
	case model.AssetCategoryCharacter, model.AssetCategoryEnvironment, model.AssetCategoryWardrobe,
		model.AssetCategoryProp, model.AssetCategoryWeapon, model.AssetCategoryStyle, model.AssetCategoryOther:
		return true
	default:
		return false
	}
}

func isAgentAssetPublicationDomainError(err error) bool {
	return errors.Is(err, ErrAgentAssetPublicationInvalid) || errors.Is(err, ErrAgentAssetPublicationApprovalRequired) ||
		errors.Is(err, ErrAgentAssetPublicationResourceMissing) || errors.Is(err, ErrAgentAssetPublicationBillingMissing) ||
		errors.Is(err, ErrAgentAssetPublicationConflict) || errors.Is(err, ErrAgentSpecialistRunConflict)
}
