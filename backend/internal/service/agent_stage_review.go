package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

var ErrAssetBindingUnconfirmed = errors.New("asset binding is not confirmed")

type StageReviewResult struct {
	Stage             model.AgentProductionStage
	Completion        *SpecialistCompletion
	ReviewID          string
	Publication       *AssetPublicationResult
	SelectedCandidate *model.AgentArtifactRevision
}

type StageCandidateApprovalCommand struct {
	StageVersion                int64
	ReviewRevisionID            string
	SelectedCandidateRevisionID string
	ClientRequestID             string
	PublicationIntent           *agentruntime.AssetPublicationIntent
}

func (s *Service) ConfirmAssetBindings(
	ctx context.Context,
	scope agentruntime.Scope,
	stageID string,
	stageVersion int64,
	reviewRevisionID string,
	bindings agentruntime.AssetBindingSet,
) (*model.AgentAssetBindingRevision, error) {
	if ctx == nil {
		return nil, ErrAssetBindingUnconfirmed
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	encoded, err := json.Marshal(bindings)
	if err != nil || !bindings.Confirmed {
		return nil, ErrAssetBindingUnconfirmed
	}
	if err := agentruntime.ValidateArtifactDraft(agentruntime.ArtifactDraft{
		ArtifactKey: bindings.BindingKey, Kind: "asset_binding", SchemaVersion: 1,
		Payload: encoded, UpstreamRevisions: []agentruntime.ArtifactRevisionRef{bindings.ScriptRevision},
		SkillVersions: []agentruntime.SkillSelection{},
	}); err != nil {
		return nil, ErrAssetBindingUnconfirmed
	}
	snapshot, err := s.repo.ProductionRuntimeSnapshotForScope(scope)
	if err != nil {
		return nil, ErrAssetBindingUnconfirmed
	}
	stage, found := productionStageByID(snapshot.Stages, stageID)
	if !found || stage.Status != agentruntime.StageAwaitingReview || stage.Version != stageVersion || stage.ReviewRevisionID != reviewRevisionID {
		return nil, ErrAssetBindingUnconfirmed
	}
	reviewRevision, err := s.repo.ArtifactRevisionInScope(scope, reviewRevisionID)
	if err != nil || reviewRevision.Kind != "asset_binding" || reviewRevision.SchemaVersion != 1 ||
		reviewRevision.CreatedBySpecialistID == "" {
		return nil, ErrAssetBindingUnconfirmed
	}
	var reviewedBindings agentruntime.AssetBindingSet
	if err := decodeStrictStoredJSONDocument(reviewRevision.PayloadJSON, func(decoder *json.Decoder) error {
		return decoder.Decode(&reviewedBindings)
	}); err != nil || !reflect.DeepEqual(reviewedBindings, bindings) {
		return nil, ErrAssetBindingUnconfirmed
	}
	reviewRun, err := s.repo.AgentSpecialistRunForScope(scope, reviewRevision.CreatedBySpecialistID)
	if err != nil || reviewRun.Status != model.AgentSpecialistRunSucceeded || reviewRun.StageID != stage.ID ||
		reviewRun.SpecialistKey != agentruntime.SpecialistAsset || reviewRun.ExpectedOutputSchema != agentruntime.ArtifactSchemaAssetBindingV1 {
		return nil, ErrAssetBindingUnconfirmed
	}
	scriptRevision, err := s.repo.ArtifactRevisionInScope(scope, bindings.ScriptRevision.RevisionID)
	if err != nil || scriptRevision.ArtifactID != bindings.ScriptRevision.ArtifactID ||
		scriptRevision.Kind != "script_bundle" || scriptRevision.SchemaVersion != 1 {
		return nil, ErrAssetBindingUnconfirmed
	}
	for _, entry := range bindings.Entries {
		if entry.State != agentruntime.AssetBindingMatched {
			return nil, ErrAssetBindingUnconfirmed
		}
		resource, err := s.productionResourceForScope(scope, entry.ResourceID)
		if err != nil || resource.Status != model.ResourceStatusReady || resource.Kind != assetBindingResourceKind(entry.RequirementKind) {
			return nil, ErrAssetBindingUnconfirmed
		}
	}
	stored, err := s.repo.AppendAgentAssetBindingRevision(scope, reviewRevision.CreatedBySpecialistID, bindings, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return stored, nil
}

func (s *Service) ReviewProductionStage(
	ctx context.Context,
	scope agentruntime.Scope,
	parentRun model.AgentRun,
	stageID string,
	command agentruntime.StageReviewCommand,
) (StageReviewResult, error) {
	return s.reviewProductionStage(ctx, scope, parentRun, stageID, command, nil, nil)
}

func (s *Service) ApproveStageCandidate(
	ctx context.Context,
	scope agentruntime.Scope,
	parentRun model.AgentRun,
	stageID string,
	command StageCandidateApprovalCommand,
) (StageReviewResult, error) {
	if ctx == nil || command.StageVersion < 1 || !validStoredText(stageID, 80) ||
		!validStoredText(command.ReviewRevisionID, 80) ||
		!validStoredText(command.SelectedCandidateRevisionID, 80) ||
		!validStoredText(command.ClientRequestID, 120) ||
		(command.PublicationIntent != nil && command.PublicationIntent.Validate() != nil) {
		return StageReviewResult{}, ErrVisualCandidateSelectionInvalid
	}
	select {
	case <-ctx.Done():
		return StageReviewResult{}, ctx.Err()
	default:
	}
	review, err := s.repo.ArtifactRevisionInScope(scope, command.ReviewRevisionID)
	if err != nil {
		return StageReviewResult{}, mapNotFoundToCandidateSelection(err)
	}
	payload, err := s.decodeVisualConsistencyReview(scope, *review)
	if err != nil {
		return StageReviewResult{}, err
	}
	selectedRef, found := findReviewCandidate(payload, command.SelectedCandidateRevisionID)
	if !found || s.requireExactReviewRevision(scope, selectedRef, mediaCandidateArtifactKind) != nil {
		return StageReviewResult{}, ErrVisualCandidateSelectionInvalid
	}
	selected, err := s.repo.ArtifactRevisionForArtifactInScope(scope, selectedRef.ArtifactID, selectedRef.RevisionID)
	if err != nil {
		return StageReviewResult{}, mapNotFoundToCandidateSelection(err)
	}
	if !validStoredMediaCandidate(*selected) {
		return StageReviewResult{}, ErrVisualCandidateSelectionInvalid
	}
	candidate, err := agentruntime.DecodeMediaCandidateContent([]byte(selected.PayloadJSON))
	if err != nil {
		return StageReviewResult{}, ErrVisualCandidateSelectionInvalid
	}
	resource, err := s.productionResourceForScope(scope, selected.ResourceID)
	if err != nil || !reviewCandidateResourceReady(resource, string(candidate.MediaKind)) {
		return StageReviewResult{}, errors.Join(ErrVisualCandidateSelectionInvalid, err)
	}
	selectionID, selectionDraft, err := candidateSelectionDraft(
		scope, stageID, *review, *selected, command.ClientRequestID,
	)
	if err != nil {
		return StageReviewResult{}, err
	}
	reviewCommand := agentruntime.StageReviewCommand{
		StageVersion: command.StageVersion, RevisionID: command.ReviewRevisionID,
		Decision: agentruntime.StageReviewApprove, ClientRequestID: command.ClientRequestID,
		PublicationIntent: command.PublicationIntent,
	}
	selection := &repository.StageCandidateSelectionInput{ArtifactID: selectionID, Draft: selectionDraft}
	return s.reviewProductionStage(ctx, scope, parentRun, stageID, reviewCommand, selection, selected)
}

func (s *Service) reviewProductionStage(
	ctx context.Context,
	scope agentruntime.Scope,
	parentRun model.AgentRun,
	stageID string,
	command agentruntime.StageReviewCommand,
	candidateSelection *repository.StageCandidateSelectionInput,
	selectedCandidate *model.AgentArtifactRevision,
) (StageReviewResult, error) {
	if ctx == nil {
		return StageReviewResult{}, errors.New("stage review context is required")
	}
	allowTerminalPublicationReplay := command.Decision == agentruntime.StageReviewApprove && command.PublicationIntent != nil
	storedParent, err := s.validateProductionStageReviewParentRun(
		scope,
		parentRun,
		command.Decision == agentruntime.StageReviewStop,
		allowTerminalPublicationReplay,
	)
	if err != nil {
		return StageReviewResult{}, err
	}
	snapshot, err := s.repo.ProductionRuntimeSnapshotForScope(scope)
	if err != nil {
		return StageReviewResult{}, err
	}
	stage, found := productionStageByID(snapshot.Stages, strings.TrimSpace(stageID))
	if !found {
		return StageReviewResult{}, repository.ErrProductionStageConflict
	}
	reviewRevision, err := s.repo.ArtifactRevisionInScope(scope, command.RevisionID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return StageReviewResult{}, agentruntime.ErrStageApprovalRevisionMismatch
	}
	if err != nil {
		return StageReviewResult{}, err
	}
	if command.Decision == agentruntime.StageReviewApprove && reviewRevision.Kind == visualConsistencyReviewKind &&
		reviewRevision.SchemaVersion == 1 && candidateSelection == nil {
		return StageReviewResult{}, ErrVisualCandidateSelectionRequired
	}
	if candidateSelection != nil && (reviewRevision.Kind != visualConsistencyReviewKind || reviewRevision.SchemaVersion != 1) {
		return StageReviewResult{}, ErrVisualCandidateSelectionInvalid
	}
	var revisionRequest *agentruntime.SpecialistRequest
	if command.Decision == agentruntime.StageReviewRequestRevision {
		if reviewRevision.CreatedBySpecialistID == "" {
			return StageReviewResult{}, repository.ErrAgentSpecialistRunConflict
		}
		priorRun, loadErr := s.repo.AgentSpecialistRunForScope(scope, reviewRevision.CreatedBySpecialistID)
		if loadErr != nil || priorRun.Status != model.AgentSpecialistRunSucceeded {
			return StageReviewResult{}, repository.ErrAgentSpecialistRunConflict
		}
		if priorRun.StageID != stage.ID {
			return StageReviewResult{}, repository.ErrProductionStageConflict
		}
		request, buildErr := revisionSpecialistRequest(*priorRun, command)
		if buildErr != nil {
			return StageReviewResult{}, buildErr
		}
		revisionRequest = &request
	}
	persisted, err := s.repo.ReviewProductionStage(repository.ReviewProductionStageInput{
		Scope: scope, StageID: stage.ID, Command: command, RevisionRequest: revisionRequest, CandidateSelection: candidateSelection,
		ToolSchemaVersion: storedParent.ToolSchemaVersion, Now: time.Now().UTC(),
	})
	if err != nil {
		return StageReviewResult{}, err
	}
	if candidateSelection != nil && (persisted.CandidateSelectionRevision == nil ||
		persisted.CandidateSelectionRevision.ArtifactID != candidateSelection.ArtifactID ||
		persisted.CandidateSelectionRevision.Kind != mediaCandidateSelectionKind ||
		persisted.CandidateSelectionRevision.SchemaVersion != 1) {
		return StageReviewResult{}, repository.ErrProductionStageReviewConflict
	}
	result := StageReviewResult{
		Stage: persisted.Stage, ReviewID: persisted.ReviewID, SelectedCandidate: selectedCandidate,
	}
	if command.Decision == agentruntime.StageReviewStop {
		if err := s.cancelAgentRunTreeContexts(scope); err != nil {
			return StageReviewResult{}, err
		}
	}
	if command.Decision == agentruntime.StageReviewApprove && command.PublicationIntent != nil {
		publicationRevisionID := command.RevisionID
		authorizationKind := repository.AgentAssetPublicationDirectReview
		selectionRevisionID := ""
		if candidateSelection != nil {
			if selectedCandidate == nil || persisted.CandidateSelectionRevision == nil {
				return StageReviewResult{}, repository.ErrProductionStageReviewConflict
			}
			publicationRevisionID = selectedCandidate.ID
			authorizationKind = repository.AgentAssetPublicationCandidateSelection
			selectionRevisionID = persisted.CandidateSelectionRevision.ID
		}
		publication, publishErr := s.PublishAsset(ctx, scope, PublishAssetCommand{
			AuthorizationKind:            authorizationKind,
			ArtifactRevisionID:           publicationRevisionID,
			ReviewRevisionID:             command.RevisionID,
			CandidateSelectionRevisionID: selectionRevisionID,
			PublicationPurpose:           command.PublicationIntent.PublicationPurpose,
			TargetCategory:               model.AssetCategory(command.PublicationIntent.TargetCategory),
			TargetBindingKey:             command.PublicationIntent.TargetBindingKey,
			ApprovedByUserID:             scope.ActorUserID,
			StageReviewID:                persisted.ReviewID,
		})
		if publishErr != nil {
			return StageReviewResult{}, publishErr
		}
		latest, loadErr := s.repo.ProductionRuntimeSnapshotForScope(scope)
		if loadErr != nil {
			return StageReviewResult{}, loadErr
		}
		publishedStage, exists := productionStageByID(latest.Stages, stage.ID)
		if !exists {
			return StageReviewResult{}, repository.ErrProductionStageConflict
		}
		result.Stage = publishedStage
		result.Publication = publication
	}
	if revisionRequest == nil {
		return result, nil
	}
	completion, err := s.RunSpecialist(ctx, scope, *storedParent, *revisionRequest)
	if err != nil {
		return StageReviewResult{}, err
	}
	latest, err := s.repo.ProductionRuntimeSnapshotForScope(scope)
	if err != nil {
		return StageReviewResult{}, err
	}
	stage, found = productionStageByID(latest.Stages, stage.ID)
	if !found {
		return StageReviewResult{}, repository.ErrProductionStageConflict
	}
	result.Stage = stage
	result.Completion = &completion
	return result, nil
}

func (s *Service) validateProductionStageReviewParentRun(
	scope agentruntime.Scope,
	parentRun model.AgentRun,
	allowCancelled bool,
	allowTerminalPublicationReplay bool,
) (*model.AgentRun, error) {
	storedParent, err := s.repo.AgentRunForScope(scope)
	if err != nil {
		return nil, err
	}
	statusAllowed := storedParent.Status == agentruntime.RunRunning ||
		allowCancelled && storedParent.Status == agentruntime.RunCancelled ||
		allowTerminalPublicationReplay && agentRuntimeRunTerminal(storedParent.Status)
	if storedParent.ID != parentRun.ID || storedParent.ModelRecordID != parentRun.ModelRecordID || storedParent.ModelKey != parentRun.ModelKey ||
		storedParent.RuntimeVersion != agentruntime.ProductionRuntimeVersion || storedParent.PolicyVersion != agentruntime.ProductionPolicyVersion ||
		storedParent.ToolSchemaVersion != agentruntime.ProductionToolSchemaVersion || !statusAllowed {
		return nil, agentruntime.ErrSpecialistModelInheritance
	}
	return storedParent, nil
}

func revisionSpecialistRequest(prior model.AgentSpecialistRun, command agentruntime.StageReviewCommand) (agentruntime.SpecialistRequest, error) {
	var inputs []agentruntime.ArtifactRevisionRef
	var skills []agentruntime.SkillSelection
	var tools []agentruntime.AgentToolName
	var delivery agentruntime.ExpectedDelivery
	if err := decodeStrictStoredJSONDocument(prior.InputRevisionsJSON, func(decoder *json.Decoder) error {
		return decoder.Decode(&inputs)
	}); err != nil {
		return agentruntime.SpecialistRequest{}, err
	}
	if err := decodeStrictStoredJSONDocument(prior.SkillVersionsJSON, func(decoder *json.Decoder) error {
		return decoder.Decode(&skills)
	}); err != nil {
		return agentruntime.SpecialistRequest{}, err
	}
	if err := decodeStrictStoredJSONDocument(prior.ToolAllowlistJSON, func(decoder *json.Decoder) error {
		return decoder.Decode(&tools)
	}); err != nil {
		return agentruntime.SpecialistRequest{}, err
	}
	if err := decodeStrictStoredJSONDocument(prior.ExpectedDeliveryJSON, func(decoder *json.Decoder) error {
		return decoder.Decode(&delivery)
	}); err != nil {
		return agentruntime.SpecialistRequest{}, err
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{
		"stage-revision", prior.RunID, prior.StageID, prior.ID, command.RevisionID, command.ClientRequestID,
	}, "\x00")))
	return agentruntime.SpecialistRequest{
		SpecialistRunID: fmt.Sprintf("%x", digest[:]), ParentSpecialistRunID: prior.ID,
		StageID: prior.StageID, SpecialistKey: prior.SpecialistKey, SpecialistVersion: prior.SpecialistVersion,
		ParentModelRecordID: prior.ModelRecordID, ParentModelKey: prior.ModelKey,
		Objective:      prior.Objective + "\n\n用户要求修改：\n" + command.Comment,
		InputRevisions: inputs, LoadedSkills: skills, ToolAllowlist: tools,
		ExpectedOutputSchema: prior.ExpectedOutputSchema, ExpectedDelivery: delivery,
	}, nil
}

func decodeStrictStoredJSONDocument(raw string, decode func(*json.Decoder) error) error {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decode(decoder); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("stored specialist snapshot contains trailing JSON")
	}
	return nil
}

func productionStageByID(stages []model.AgentProductionStage, stageID string) (model.AgentProductionStage, bool) {
	for _, stage := range stages {
		if stage.ID == stageID {
			return stage, true
		}
	}
	return model.AgentProductionStage{}, false
}

func assetBindingResourceKind(kind agentruntime.AssetRequirementKind) string {
	if kind == agentruntime.AssetRequirementVoiceRole {
		return "audio"
	}
	return "image"
}
