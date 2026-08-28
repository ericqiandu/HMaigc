package repository

import (
	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AgentAssetPublicationAuthorizationKind string

const (
	AgentAssetPublicationDirectReview       AgentAssetPublicationAuthorizationKind = "direct_review"
	AgentAssetPublicationCandidateSelection AgentAssetPublicationAuthorizationKind = "candidate_selection"
)

func (kind AgentAssetPublicationAuthorizationKind) Valid() bool {
	return kind == AgentAssetPublicationDirectReview || kind == AgentAssetPublicationCandidateSelection
}

type agentAssetPublicationAuthorization struct {
	Kind              AgentAssetPublicationAuthorizationKind
	ApprovalItem      model.AgentTimelineItem
	Approval          agentruntime.StageReviewResolutionContent
	Stage             model.AgentProductionStage
	ReviewRevision    model.AgentArtifactRevision
	SelectionRevision *model.AgentArtifactRevision
	Selection         *agentruntime.MediaCandidateSelection
}

func loadAssetPublicationAuthorizationTx(
	tx *gorm.DB,
	input PublishAgentAssetInput,
) (agentAssetPublicationAuthorization, error) {
	authorization := agentAssetPublicationAuthorization{Kind: input.AuthorizationKind}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", input.StageReviewID).
		First(&authorization.ApprovalItem).Error; err != nil {
		return agentAssetPublicationAuthorization{}, ErrAgentAssetPublicationApprovalRequired
	}
	approval, err := agentruntime.DecodeStageReviewResolutionContent([]byte(authorization.ApprovalItem.ContentJSON))
	if err != nil || authorization.ApprovalItem.TenantKind != input.Scope.TenantKind ||
		authorization.ApprovalItem.TenantID != input.Scope.TenantID ||
		authorization.ApprovalItem.ThreadID != input.Scope.ThreadID || authorization.ApprovalItem.RunID != input.Scope.RunID ||
		authorization.ApprovalItem.Kind != model.AgentTimelineItemApproval ||
		authorization.ApprovalItem.Status != model.AgentTimelineItemCompleted ||
		approval.Decision != agentruntime.StageReviewApprove || approval.RevisionID != input.ReviewRevisionID ||
		approval.PublicationIntent == nil || approval.PublicationIntent.PublicationPurpose != input.PublicationPurpose ||
		approval.PublicationIntent.TargetCategory != string(input.TargetCategory) ||
		approval.PublicationIntent.TargetBindingKey != input.TargetBindingKey {
		return agentAssetPublicationAuthorization{}, ErrAgentAssetPublicationApprovalRequired
	}
	authorization.Approval = approval

	if err := productionStageScopeQuery(tx, input.Scope).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", approval.StageID).First(&authorization.Stage).Error; err != nil ||
		authorization.Stage.ReviewRevisionID != input.ReviewRevisionID ||
		(authorization.Stage.Status != agentruntime.StageApproved && authorization.Stage.Status != agentruntime.StageCompleted) {
		return agentAssetPublicationAuthorization{}, ErrAgentAssetPublicationApprovalRequired
	}
	if err := productionArtifactRevisionScopeQuery(tx, input.Scope).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", input.ReviewRevisionID).First(&authorization.ReviewRevision).Error; err != nil {
		return agentAssetPublicationAuthorization{}, ErrAgentAssetPublicationApprovalRequired
	}

	if input.AuthorizationKind == AgentAssetPublicationDirectReview {
		if input.ArtifactRevisionID != input.ReviewRevisionID {
			return agentAssetPublicationAuthorization{}, ErrAgentAssetPublicationApprovalRequired
		}
		return authorization, nil
	}

	var selectionRevision model.AgentArtifactRevision
	if err := productionArtifactRevisionScopeQuery(tx, input.Scope).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", input.CandidateSelectionRevisionID).First(&selectionRevision).Error; err != nil ||
		selectionRevision.Kind != "media_candidate_selection" || selectionRevision.SchemaVersion != 1 ||
		selectionRevision.ResourceID != "" || selectionRevision.ModelRequestIdentity != "" ||
		selectionRevision.CreatedBySpecialistID != "" ||
		selectionRevision.LifecycleStatus != model.AgentArtifactRevisionAwaitingReview {
		return agentAssetPublicationAuthorization{}, ErrAgentAssetPublicationApprovalRequired
	}
	selection, err := agentruntime.DecodeMediaCandidateSelection([]byte(selectionRevision.PayloadJSON))
	if err != nil || selection.StageID != approval.StageID ||
		selection.ReviewRevision.ArtifactID != authorization.ReviewRevision.ArtifactID ||
		selection.ReviewRevision.RevisionID != input.ReviewRevisionID ||
		selection.SelectedCandidateRevision.RevisionID != input.ArtifactRevisionID ||
		selection.ApprovedByUserID != input.ApprovedByUserID || selection.ApprovedByUserID != input.Scope.ActorUserID ||
		selection.ClientRequestID != approval.ClientRequestID ||
		!artifactRevisionRefsStartWithStored(selectionRevision.UpstreamRevisionsJSON, []agentruntime.ArtifactRevisionRef{
			selection.ReviewRevision, selection.SelectedCandidateRevision,
		}) {
		return agentAssetPublicationAuthorization{}, ErrAgentAssetPublicationApprovalRequired
	}
	var selected model.AgentArtifactRevision
	if err := productionArtifactRevisionScopeQuery(tx, input.Scope).Where("id = ?", input.ArtifactRevisionID).
		First(&selected).Error; err != nil || selected.ArtifactID != selection.SelectedCandidateRevision.ArtifactID {
		return agentAssetPublicationAuthorization{}, ErrAgentAssetPublicationApprovalRequired
	}
	authorization.SelectionRevision = &selectionRevision
	authorization.Selection = &selection
	return authorization, nil
}
