package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

var (
	ErrAssetPublicationInvalid          = repository.ErrAgentAssetPublicationInvalid
	ErrAssetPublicationApprovalRequired = repository.ErrAgentAssetPublicationApprovalRequired
	ErrAssetPublicationResourceMissing  = repository.ErrAgentAssetPublicationResourceMissing
	ErrAssetPublicationBillingMissing   = repository.ErrAgentAssetPublicationBillingMissing
	ErrAssetPublicationConflict         = repository.ErrAgentAssetPublicationConflict
	ErrAssetPublicationFailed           = repository.ErrAgentAssetPublicationFailed
)

type PublishAssetCommand struct {
	AuthorizationKind            repository.AgentAssetPublicationAuthorizationKind
	ArtifactRevisionID           string
	ReviewRevisionID             string
	CandidateSelectionRevisionID string
	PublicationPurpose           string
	TargetCategory               model.AssetCategory
	TargetBindingKey             string
	ApprovedByUserID             string
	StageReviewID                string
}

type AssetPublicationResult struct {
	ID                 string                            `json:"id"`
	ArtifactRevisionID string                            `json:"artifactRevisionId"`
	AssetID            string                            `json:"assetId"`
	AssetVersionID     string                            `json:"assetVersionId"`
	ProjectAssetLinkID string                            `json:"projectAssetLinkId"`
	RepresentationID   string                            `json:"representationId"`
	Status             model.AgentAssetPublicationStatus `json:"status"`
	Replayed           bool                              `json:"replayed"`
}

func (s *Service) PublishAsset(
	ctx context.Context,
	scope agentruntime.Scope,
	command PublishAssetCommand,
) (*AssetPublicationResult, error) {
	command.ArtifactRevisionID = strings.TrimSpace(command.ArtifactRevisionID)
	command.ReviewRevisionID = strings.TrimSpace(command.ReviewRevisionID)
	command.CandidateSelectionRevisionID = strings.TrimSpace(command.CandidateSelectionRevisionID)
	command.PublicationPurpose = strings.TrimSpace(command.PublicationPurpose)
	command.TargetBindingKey = strings.TrimSpace(command.TargetBindingKey)
	command.ApprovedByUserID = strings.TrimSpace(command.ApprovedByUserID)
	command.StageReviewID = strings.TrimSpace(command.StageReviewID)
	if ctx == nil || !command.AuthorizationKind.Valid() || command.ArtifactRevisionID == "" || command.ReviewRevisionID == "" || command.PublicationPurpose == "" ||
		command.TargetBindingKey == "" || command.ApprovedByUserID == "" || command.StageReviewID == "" ||
		!validAssetCategory(command.TargetCategory) {
		return nil, ErrAssetPublicationInvalid
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	published, err := s.repo.PublishAgentAsset(repository.PublishAgentAssetInput{
		Context: ctx, Scope: scope, AuthorizationKind: command.AuthorizationKind,
		ArtifactRevisionID: command.ArtifactRevisionID, ReviewRevisionID: command.ReviewRevisionID,
		CandidateSelectionRevisionID: command.CandidateSelectionRevisionID,
		PublicationPurpose:           command.PublicationPurpose, TargetCategory: command.TargetCategory,
		TargetBindingKey: command.TargetBindingKey, ApprovedByUserID: command.ApprovedByUserID,
		StageReviewID: command.StageReviewID, Now: time.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	if published == nil || published.Publication.Status != model.AgentAssetPublicationSucceeded {
		return nil, errors.New("asset publication returned an invalid result")
	}
	return &AssetPublicationResult{
		ID: published.Publication.ID, ArtifactRevisionID: published.Publication.ArtifactRevisionID,
		AssetID: published.Asset.ID, AssetVersionID: published.AssetVersion.ID,
		ProjectAssetLinkID: published.ProjectAssetLink.ID, RepresentationID: published.Representation.ID,
		Status: published.Publication.Status, Replayed: published.Replayed,
	}, nil
}
