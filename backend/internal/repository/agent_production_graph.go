package repository

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

var (
	ErrProductionScopeInvalid           = errors.New("production scope is invalid")
	ErrProductionGraphVersionConflict   = errors.New("production graph version conflict")
	ErrProductionRuntimeSnapshotInvalid = errors.New("production runtime snapshot is invalid")
	ErrProductionStageConflict          = errors.New("production stage conflict")
	ErrProductionStageInputInvalid      = errors.New("production stage input is invalid")
)

type AppendProductionGraphVersionResult struct {
	Graph  model.AgentProductionGraphVersion
	Stages []model.AgentProductionStage
}

type ProductionArtifactHeadSnapshot struct {
	Artifact model.AgentArtifact
	Revision model.AgentArtifactRevision
}

type ProductionRuntimeSnapshot struct {
	Graph                     *model.AgentProductionGraphVersion
	Draft                     *agentruntime.ProductionGraphDraft
	Stages                    []model.AgentProductionStage
	Artifacts                 []ProductionArtifactHeadSnapshot
	CharacterIdentityVersions []model.AgentCharacterIdentityVersion
	ShotBindingRevisions      []model.AgentShotBindingRevision
	Progress                  *agentruntime.ProductionNextActionProjection
}

type encodedProductionStageDraft struct {
	draft            agentruntime.ProductionStageDraft
	dependenciesJSON string
	inputsJSON       string
	deliveryJSON     string
}

func (r *Repository) AppendProductionGraphVersion(
	scope agentruntime.Scope,
	expectedVersion int64,
	draft agentruntime.ProductionGraphDraft,
) (*AppendProductionGraphVersionResult, error) {
	if err := validateProductionRepositoryScope(scope, true); err != nil {
		return nil, err
	}
	if expectedVersion < 0 {
		return nil, ErrProductionGraphVersionConflict
	}
	draft = canonicalProductionGraphDraft(draft)
	if err := agentruntime.ValidateProductionGraph(draft); err != nil {
		return nil, err
	}

	stagesJSON, err := json.Marshal(draft.Stages)
	if err != nil {
		return nil, fmt.Errorf("encode production graph stages: %w", err)
	}
	encodedStages := make([]encodedProductionStageDraft, 0, len(draft.Stages))
	for _, stage := range draft.Stages {
		dependenciesJSON, marshalErr := json.Marshal(stage.DependsOnStageKeys)
		if marshalErr != nil {
			return nil, fmt.Errorf("encode production stage dependencies: %w", marshalErr)
		}
		inputsJSON, marshalErr := json.Marshal(stage.InputRevisions)
		if marshalErr != nil {
			return nil, fmt.Errorf("encode production stage inputs: %w", marshalErr)
		}
		deliveryJSON, marshalErr := json.Marshal(stage.ExpectedDelivery)
		if marshalErr != nil {
			return nil, fmt.Errorf("encode production stage delivery: %w", marshalErr)
		}
		encodedStages = append(encodedStages, encodedProductionStageDraft{
			draft:            stage,
			dependenciesJSON: string(dependenciesJSON),
			inputsJSON:       string(inputsJSON),
			deliveryJSON:     string(deliveryJSON),
		})
	}

	nextVersion := expectedVersion + 1
	now := time.Now().UTC()
	result := &AppendProductionGraphVersionResult{
		Graph: model.AgentProductionGraphVersion{
			ID:              productionGraphVersionID(scope, draft.GraphKey, nextVersion),
			TenantKind:      scope.TenantKind,
			TenantID:        scope.TenantID,
			ActorUserID:     scope.ActorUserID,
			DomainProjectID: scope.DomainProjectID,
			CanvasID:        scope.CanvasID,
			ThreadID:        scope.ThreadID,
			RunID:           scope.RunID,
			GraphKey:        draft.GraphKey,
			Version:         nextVersion,
			SchemaVersion:   agentruntime.CurrentProductionSchemaVersion,
			StagesJSON:      string(stagesJSON),
			CreatedAt:       now,
		},
		Stages: make([]model.AgentProductionStage, 0, len(encodedStages)),
	}
	for _, encoded := range encodedStages {
		result.Stages = append(result.Stages, model.AgentProductionStage{
			ID:                   agentFactID("production-stage", result.Graph.ID, encoded.draft.StageKey),
			TenantKind:           scope.TenantKind,
			TenantID:             scope.TenantID,
			ActorUserID:          scope.ActorUserID,
			DomainProjectID:      scope.DomainProjectID,
			CanvasID:             scope.CanvasID,
			ThreadID:             scope.ThreadID,
			RunID:                scope.RunID,
			GraphVersionID:       result.Graph.ID,
			StageKey:             encoded.draft.StageKey,
			SpecialistKey:        encoded.draft.SpecialistKey,
			DependsOnStagesJSON:  encoded.dependenciesJSON,
			InputRevisionsJSON:   encoded.inputsJSON,
			ExpectedDeliveryJSON: encoded.deliveryJSON,
			ReviewPolicy:         encoded.draft.ReviewPolicy,
			CostPolicy:           encoded.draft.CostPolicy,
			Status:               agentruntime.StagePlanned,
			Version:              1,
			CreatedAt:            now,
			UpdatedAt:            now,
		})
	}

	err = r.db.Transaction(func(tx *gorm.DB) error {
		inputRevisions := make([]agentruntime.ArtifactRevisionRef, 0)
		for _, stage := range draft.Stages {
			inputRevisions = append(inputRevisions, stage.InputRevisions...)
		}
		if err := validateArtifactRevisionRefsInScope(tx, scope, uniqueArtifactRevisionRefs(inputRevisions)); err != nil {
			return err
		}
		var currentVersion int64
		if queryErr := productionGraphScopeQuery(tx.Model(&model.AgentProductionGraphVersion{}), scope).
			Where("graph_key = ?", draft.GraphKey).
			Select("COALESCE(MAX(version), 0)").
			Scan(&currentVersion).Error; queryErr != nil {
			return queryErr
		}
		if currentVersion != expectedVersion {
			return ErrProductionGraphVersionConflict
		}
		if createErr := tx.Create(&result.Graph).Error; createErr != nil {
			if isUniqueConstraintError(createErr) {
				return ErrProductionGraphVersionConflict
			}
			return createErr
		}
		if createErr := tx.Create(&result.Stages).Error; createErr != nil {
			if isUniqueConstraintError(createErr) {
				return ErrProductionGraphVersionConflict
			}
			return createErr
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Repository) ProductionRuntimeSnapshotForScope(scope agentruntime.Scope) (*ProductionRuntimeSnapshot, error) {
	if err := validateProductionRepositoryScope(scope, false); err != nil {
		return nil, err
	}
	var snapshot *ProductionRuntimeSnapshot
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var snapshotErr error
		snapshot, snapshotErr = productionRuntimeSnapshotTx(tx, scope)
		return snapshotErr
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

func productionRuntimeSnapshotTx(tx *gorm.DB, scope agentruntime.Scope) (*ProductionRuntimeSnapshot, error) {
	snapshot := &ProductionRuntimeSnapshot{
		Stages:                    []model.AgentProductionStage{},
		Artifacts:                 []ProductionArtifactHeadSnapshot{},
		CharacterIdentityVersions: []model.AgentCharacterIdentityVersion{},
		ShotBindingRevisions:      []model.AgentShotBindingRevision{},
	}
	if err := func() error {
		var graphKeys []string
		if err := productionGraphScopeQuery(tx.Model(&model.AgentProductionGraphVersion{}), scope).
			Distinct("graph_key").Order("graph_key ASC").Pluck("graph_key", &graphKeys).Error; err != nil {
			return err
		}
		if len(graphKeys) > 1 {
			return ErrProductionRuntimeSnapshotInvalid
		}

		artifacts, err := productionArtifactHeadSnapshots(tx, scope)
		if err != nil {
			return err
		}
		snapshot.Artifacts = artifacts
		identities, bindings, err := productionIdentityFacts(tx, scope)
		if err != nil {
			return err
		}
		snapshot.CharacterIdentityVersions = identities
		snapshot.ShotBindingRevisions = bindings
		if len(graphKeys) == 0 {
			var stageCount int64
			if err := productionStageScopeQuery(tx.Model(&model.AgentProductionStage{}), scope).Count(&stageCount).Error; err != nil {
				return err
			}
			if stageCount != 0 || len(artifacts) != 0 || len(identities) != 0 || len(bindings) != 0 {
				return ErrProductionRuntimeSnapshotInvalid
			}
			return nil
		}

		var graph model.AgentProductionGraphVersion
		if err := productionGraphScopeQuery(tx.Model(&model.AgentProductionGraphVersion{}), scope).
			Where("graph_key = ?", graphKeys[0]).Order("version DESC").First(&graph).Error; err != nil {
			return err
		}
		if graph.Version < 1 || graph.SchemaVersion != agentruntime.CurrentProductionSchemaVersion {
			return ErrProductionRuntimeSnapshotInvalid
		}
		var stageDrafts []agentruntime.ProductionStageDraft
		if err := decodeProductionSnapshotJSON(graph.StagesJSON, &stageDrafts); err != nil {
			return err
		}
		draft := canonicalProductionGraphDraft(agentruntime.ProductionGraphDraft{GraphKey: graph.GraphKey, Stages: stageDrafts})
		if err := agentruntime.ValidateProductionGraph(draft); err != nil {
			return ErrProductionRuntimeSnapshotInvalid
		}

		var storedStages []model.AgentProductionStage
		if err := productionStageScopeQuery(tx.Model(&model.AgentProductionStage{}), scope).
			Where("graph_version_id = ?", graph.ID).Find(&storedStages).Error; err != nil {
			return err
		}
		if len(storedStages) != len(draft.Stages) {
			return ErrProductionRuntimeSnapshotInvalid
		}
		stageByKey := make(map[string]model.AgentProductionStage, len(storedStages))
		for _, stage := range storedStages {
			if _, duplicate := stageByKey[stage.StageKey]; duplicate {
				return ErrProductionRuntimeSnapshotInvalid
			}
			stageByKey[stage.StageKey] = stage
		}
		orderedStages := make([]model.AgentProductionStage, 0, len(draft.Stages))
		for _, expected := range draft.Stages {
			stage, found := stageByKey[expected.StageKey]
			if !found || !productionStageSnapshotMatches(stage, graph.ID, expected) {
				return ErrProductionRuntimeSnapshotInvalid
			}
			orderedStages = append(orderedStages, stage)
		}
		snapshot.Graph = &graph
		snapshot.Draft = &draft
		snapshot.Stages = orderedStages
		progress, err := productionProgressProjection(tx, scope, graph, draft, orderedStages, artifacts)
		if err != nil {
			return err
		}
		snapshot.Progress = progress
		return nil
	}(); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func productionArtifactHeadSnapshots(tx *gorm.DB, scope agentruntime.Scope) ([]ProductionArtifactHeadSnapshot, error) {
	var artifacts []model.AgentArtifact
	if err := productionArtifactScopeQuery(tx.Model(&model.AgentArtifact{}), scope).
		Where("agent_artifacts.lifecycle_status = ?", model.AgentArtifactLifecycleActive).
		Order("agent_artifacts.id ASC").Find(&artifacts).Error; err != nil {
		return nil, err
	}
	if len(artifacts) == 0 {
		return []ProductionArtifactHeadSnapshot{}, nil
	}
	artifactIDs := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		if strings.TrimSpace(artifact.ID) != artifact.ID || artifact.ID == "" ||
			strings.TrimSpace(artifact.ArtifactKey) != artifact.ArtifactKey || artifact.ArtifactKey == "" ||
			strings.TrimSpace(artifact.Kind) != artifact.Kind || artifact.Kind == "" ||
			artifact.HeadRevision < 1 || artifact.Version < 1 || artifact.LifecycleStatus != model.AgentArtifactLifecycleActive {
			return nil, ErrProductionRuntimeSnapshotInvalid
		}
		artifactIDs = append(artifactIDs, artifact.ID)
	}

	var revisions []model.AgentArtifactRevision
	if err := productionArtifactRevisionScopeQuery(tx, scope).
		Omit("payload_json").
		Joins("JOIN agent_artifacts AS head_artifacts ON head_artifacts.id = agent_artifact_revisions.artifact_id AND head_artifacts.head_revision = agent_artifact_revisions.revision").
		Where("head_artifacts.id IN ?", artifactIDs).
		Order("head_artifacts.id ASC").Find(&revisions).Error; err != nil {
		return nil, err
	}
	if len(revisions) != len(artifacts) {
		return nil, ErrProductionRuntimeSnapshotInvalid
	}
	revisionByArtifactID := make(map[string]model.AgentArtifactRevision, len(revisions))
	for _, revision := range revisions {
		if _, duplicate := revisionByArtifactID[revision.ArtifactID]; duplicate {
			return nil, ErrProductionRuntimeSnapshotInvalid
		}
		revisionByArtifactID[revision.ArtifactID] = revision
	}
	result := make([]ProductionArtifactHeadSnapshot, 0, len(artifacts))
	for _, artifact := range artifacts {
		revision, found := revisionByArtifactID[artifact.ID]
		if !found || revision.PayloadJSON != "" || revision.Revision != artifact.HeadRevision ||
			revision.ArtifactKey != artifact.ArtifactKey || revision.Kind != artifact.Kind || revision.SchemaVersion < 1 ||
			strings.TrimSpace(revision.ID) != revision.ID || revision.ID == "" || strings.TrimSpace(revision.LifecycleStatus) == "" {
			return nil, ErrProductionRuntimeSnapshotInvalid
		}
		result = append(result, ProductionArtifactHeadSnapshot{Artifact: artifact, Revision: revision})
	}
	return result, nil
}

func productionIdentityFacts(
	tx *gorm.DB,
	scope agentruntime.Scope,
) ([]model.AgentCharacterIdentityVersion, []model.AgentShotBindingRevision, error) {
	identities := []model.AgentCharacterIdentityVersion{}
	if err := productionCharacterIdentityScopeQuery(tx, scope).
		Order("character_key ASC, version ASC").Find(&identities).Error; err != nil {
		return nil, nil, err
	}
	bindings := []model.AgentShotBindingRevision{}
	if err := productionShotBindingScopeQuery(tx, scope).
		Order("shot_key ASC, character_key ASC, revision ASC").Find(&bindings).Error; err != nil {
		return nil, nil, err
	}
	if len(identities) == 0 && len(bindings) == 0 {
		return identities, bindings, nil
	}

	revisionIDs := make([]string, 0, len(identities)+len(bindings))
	resourceIDs := make([]string, 0, len(identities)+len(bindings))
	identityByID := make(map[string]model.AgentCharacterIdentityVersion, len(identities))
	for _, identity := range identities {
		if err := agentruntime.ValidateCharacterIdentityVersion(agentruntime.CharacterIdentityVersion{
			CharacterKey: identity.CharacterKey, Version: identity.Version,
			CharacterBibleRevisionID: identity.CharacterBibleRevisionID, ResourceID: identity.ResourceID,
			DependencyHash: identity.DependencyHash, LifecycleStatus: identity.LifecycleStatus,
		}); err != nil {
			return nil, nil, ErrProductionRuntimeSnapshotInvalid
		}
		identityByID[identity.ID] = identity
		revisionIDs = append(revisionIDs, identity.CharacterBibleRevisionID)
		resourceIDs = append(resourceIDs, identity.ResourceID)
	}
	for _, binding := range bindings {
		if err := agentruntime.ValidateShotBindingRevision(agentruntime.ShotBindingRevision{
			ShotKey: binding.ShotKey, CharacterKey: binding.CharacterKey, Revision: binding.Revision,
			ShotArtifactRevisionID: binding.ShotArtifactRevisionID, IdentityVersionID: binding.IdentityVersionID,
			ResourceID: binding.ResourceID, DependencyHash: binding.DependencyHash, LifecycleStatus: binding.LifecycleStatus,
		}); err != nil {
			return nil, nil, ErrProductionRuntimeSnapshotInvalid
		}
		identity, found := identityByID[binding.IdentityVersionID]
		if !found || identity.CharacterKey != binding.CharacterKey {
			return nil, nil, ErrProductionRuntimeSnapshotInvalid
		}
		revisionIDs = append(revisionIDs, binding.ShotArtifactRevisionID)
		resourceIDs = append(resourceIDs, binding.ResourceID)
	}

	revisions, err := productionRevisionsByID(tx, scope, uniqueStrings(revisionIDs))
	if err != nil {
		return nil, nil, err
	}
	resources, err := productionResourcesByID(tx, scope, uniqueStrings(resourceIDs))
	if err != nil {
		return nil, nil, err
	}
	for _, identity := range identities {
		revision, found := revisions[identity.CharacterBibleRevisionID]
		if !found || revision.ResourceID != identity.ResourceID {
			return nil, nil, ErrProductionRuntimeSnapshotInvalid
		}
		if _, found := resources[identity.ResourceID]; !found {
			return nil, nil, ErrProductionRuntimeSnapshotInvalid
		}
	}
	for _, binding := range bindings {
		revision, found := revisions[binding.ShotArtifactRevisionID]
		if !found || revision.ResourceID != binding.ResourceID {
			return nil, nil, ErrProductionRuntimeSnapshotInvalid
		}
		if _, found := resources[binding.ResourceID]; !found {
			return nil, nil, ErrProductionRuntimeSnapshotInvalid
		}
	}
	return identities, bindings, nil
}

func productionProgressProjection(
	tx *gorm.DB,
	scope agentruntime.Scope,
	graph model.AgentProductionGraphVersion,
	draft agentruntime.ProductionGraphDraft,
	stages []model.AgentProductionStage,
	artifacts []ProductionArtifactHeadSnapshot,
) (*agentruntime.ProductionNextActionProjection, error) {
	stageIDs := make([]string, 0, len(stages))
	stageByID := make(map[string]model.AgentProductionStage, len(stages))
	for _, stage := range stages {
		stageIDs = append(stageIDs, stage.ID)
		stageByID[stage.ID] = stage
	}
	var specialists []model.AgentSpecialistRun
	if err := agentSpecialistScopeQuery(tx, scope).Where("stage_id IN ?", stageIDs).
		Order("stage_id ASC, attempt ASC, created_at ASC, id ASC").Find(&specialists).Error; err != nil {
		return nil, err
	}
	latestSpecialistByStage := make(map[string]model.AgentSpecialistRun, len(stages))
	specialistStageByID := make(map[string]string, len(specialists))
	for _, specialist := range specialists {
		if _, found := stageByID[specialist.StageID]; !found {
			return nil, ErrProductionRuntimeSnapshotInvalid
		}
		specialistStageByID[specialist.ID] = specialist.StageID
		latestSpecialistByStage[specialist.StageID] = specialist
	}

	taskIDs := make([]string, 0, len(latestSpecialistByStage))
	billingIDs := make([]string, 0, len(latestSpecialistByStage))
	for _, specialist := range latestSpecialistByStage {
		if specialist.TaskID != "" {
			taskIDs = append(taskIDs, specialist.TaskID)
		}
		if specialist.BillingOrderID != "" {
			billingIDs = append(billingIDs, specialist.BillingOrderID)
		}
	}
	tasks, err := productionTasksByID(tx, scope, uniqueStrings(taskIDs))
	if err != nil {
		return nil, err
	}
	billings, err := productionBillingsByID(tx, scope, uniqueStrings(billingIDs))
	if err != nil {
		return nil, err
	}

	resourceIDs := make([]string, 0, len(artifacts))
	revisionIDs := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		revisionIDs = append(revisionIDs, artifact.Revision.ID)
		if artifact.Revision.ResourceID != "" {
			resourceIDs = append(resourceIDs, artifact.Revision.ResourceID)
		}
	}
	resources, err := productionResourcesByID(tx, scope, uniqueStrings(resourceIDs))
	if err != nil {
		return nil, err
	}
	publications, err := productionPublicationsByRevision(tx, scope, uniqueStrings(revisionIDs))
	if err != nil {
		return nil, err
	}

	draftByKey := make(map[string]agentruntime.ProductionStageDraft, len(draft.Stages))
	for _, stageDraft := range draft.Stages {
		draftByKey[stageDraft.StageKey] = stageDraft
	}
	progressStages := make([]agentruntime.ProductionProgressStageFacts, 0, len(stages))
	for _, stage := range stages {
		stageDraft, found := draftByKey[stage.StageKey]
		if !found {
			return nil, ErrProductionRuntimeSnapshotInvalid
		}
		stageFacts := agentruntime.ProductionProgressStageFacts{
			StageKey: stage.StageKey, Status: stage.Status,
			DependsOnStageKeys: append([]string(nil), stageDraft.DependsOnStageKeys...),
			ReviewRevisionID:   stage.ReviewRevisionID, ExpectedDelivery: stageDraft.ExpectedDelivery,
			Tasks: []agentruntime.ProductionTaskEvidence{}, Billings: []agentruntime.ProductionBillingEvidence{},
			DeliveryEvidence: agentruntime.DeliveryEvidence{CanvasID: stageDraft.ExpectedDelivery.TargetCanvasID, Artifacts: []agentruntime.DeliveryArtifact{}},
			StaleRevisionIDs: []string{},
		}
		if specialist, found := latestSpecialistByStage[stage.ID]; found {
			if specialist.Status == model.AgentSpecialistRunSucceeded {
				stageFacts.DeliveryEvidence.FinalMessage = specialist.ResultSummary
			}
			if specialist.TaskID != "" {
				task, found := tasks[specialist.TaskID]
				if !found || task.BillingOrderID != specialist.BillingOrderID {
					return nil, ErrProductionRuntimeSnapshotInvalid
				}
				stageFacts.Tasks = append(stageFacts.Tasks, agentruntime.ProductionTaskEvidence{
					TaskID: task.ID, Status: string(task.Status), BillingOrderID: task.BillingOrderID,
				})
			}
			if specialist.BillingOrderID != "" {
				billing, found := billings[specialist.BillingOrderID]
				if !found || billing.TaskID != specialist.TaskID {
					return nil, ErrProductionRuntimeSnapshotInvalid
				}
				stageFacts.Billings = append(stageFacts.Billings, agentruntime.ProductionBillingEvidence{
					BillingOrderID: billing.ID, Status: string(billing.Status),
				})
			}
		}
		for _, artifact := range artifacts {
			if specialistStageByID[artifact.Revision.CreatedBySpecialistID] != stage.ID {
				continue
			}
			if artifact.Revision.LifecycleStatus == model.AgentArtifactRevisionStale {
				stageFacts.StaleRevisionIDs = append(stageFacts.StaleRevisionIDs, artifact.Revision.ID)
				continue
			}
			kind := agentruntime.ArtifactKind(artifact.Revision.Kind)
			resource, hasResource := resources[artifact.Revision.ResourceID]
			if !kind.Valid() && hasResource {
				kind = agentruntime.ArtifactKind(resource.Kind)
			}
			if !kind.Valid() && artifact.Revision.ResourceID == "" {
				kind = agentruntime.ArtifactText
			}
			if !kind.Valid() {
				continue
			}
			deliveryArtifact := agentruntime.DeliveryArtifact{
				Kind: kind, ArtifactID: artifact.Artifact.ID, RevisionID: artifact.Revision.ID,
				ResourceID: artifact.Revision.ResourceID,
				Approved: stage.ReviewRevisionID == artifact.Revision.ID &&
					(stage.Status == agentruntime.StageApproved || stage.Status == agentruntime.StageCompleted),
			}
			if hasResource {
				deliveryArtifact.URL = resource.PublicURL
				deliveryArtifact.ResourceReady = resource.Status == model.ResourceStatusReady
			} else if artifact.Revision.ResourceID != "" {
				return nil, ErrProductionRuntimeSnapshotInvalid
			}
			if publication, found := publications[artifact.Revision.ID]; found {
				deliveryArtifact.PublicationID = publication.ID
			}
			stageFacts.DeliveryEvidence.Artifacts = append(stageFacts.DeliveryEvidence.Artifacts, deliveryArtifact)
		}
		if stageDraft.ExpectedDelivery.TargetCanvasID != "" {
			if stageDraft.ExpectedDelivery.TargetCanvasID != scope.CanvasID {
				return nil, ErrProductionRuntimeSnapshotInvalid
			}
			if err := tx.Model(&model.CanvasChange{}).Where("canvas_id = ?", scope.CanvasID).
				Select("COALESCE(MAX(revision), 0)").Scan(&stageFacts.DeliveryEvidence.CanvasRevision).Error; err != nil {
				return nil, err
			}
		}
		progressStages = append(progressStages, stageFacts)
	}
	projection, err := agentruntime.BuildProductionProgress(agentruntime.ProductionProgressFacts{
		GraphVersionID: graph.ID, GraphVersion: graph.Version, Stages: progressStages, ComputedAt: time.Now().UTC(),
	})
	if err != nil {
		return nil, ErrProductionRuntimeSnapshotInvalid
	}
	return &projection, nil
}

func productionRevisionsByID(tx *gorm.DB, scope agentruntime.Scope, ids []string) (map[string]model.AgentArtifactRevision, error) {
	result := make(map[string]model.AgentArtifactRevision, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	var revisions []model.AgentArtifactRevision
	if err := productionArtifactRevisionScopeQuery(tx, scope).Omit("payload_json").Where("id IN ?", ids).Find(&revisions).Error; err != nil {
		return nil, err
	}
	if len(revisions) != len(ids) {
		return nil, ErrProductionRuntimeSnapshotInvalid
	}
	for _, revision := range revisions {
		result[revision.ID] = revision
	}
	return result, nil
}

func productionResourcesByID(tx *gorm.DB, scope agentruntime.Scope, ids []string) (map[string]model.Resource, error) {
	result := make(map[string]model.Resource, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	query := tx.Model(&model.Resource{}).Where("id IN ?", ids)
	if scope.TenantKind == agentruntime.TenantTeam {
		query = query.Where("team_id = ?", scope.TenantID)
	} else {
		query = query.Where("user_id = ? AND (team_id = '' OR team_id IS NULL)", scope.ActorUserID)
	}
	var resources []model.Resource
	if err := query.Find(&resources).Error; err != nil {
		return nil, err
	}
	if len(resources) != len(ids) {
		return nil, ErrProductionRuntimeSnapshotInvalid
	}
	for _, resource := range resources {
		result[resource.ID] = resource
	}
	return result, nil
}

func productionTasksByID(tx *gorm.DB, scope agentruntime.Scope, ids []string) (map[string]model.Task, error) {
	result := make(map[string]model.Task, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	var tasks []model.Task
	if err := tx.Model(&model.Task{}).Where(
		"id IN ? AND user_id = ? AND project_id = ? AND audience = ?", ids, scope.ActorUserID, scope.CanvasID, model.TaskAudienceInternal,
	).Find(&tasks).Error; err != nil {
		return nil, err
	}
	if len(tasks) != len(ids) {
		return nil, ErrProductionRuntimeSnapshotInvalid
	}
	for _, task := range tasks {
		result[task.ID] = task
	}
	return result, nil
}

func productionBillingsByID(tx *gorm.DB, scope agentruntime.Scope, ids []string) (map[string]model.BillingOrder, error) {
	result := make(map[string]model.BillingOrder, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	query := tx.Model(&model.BillingOrder{}).Where("id IN ? AND user_id = ?", ids, scope.ActorUserID)
	if scope.TenantKind == agentruntime.TenantTeam {
		query = query.Where("team_id = ?", scope.TenantID)
	} else {
		query = query.Where("team_id = '' OR team_id IS NULL")
	}
	var billings []model.BillingOrder
	if err := query.Find(&billings).Error; err != nil {
		return nil, err
	}
	if len(billings) != len(ids) {
		return nil, ErrProductionRuntimeSnapshotInvalid
	}
	for _, billing := range billings {
		result[billing.ID] = billing
	}
	return result, nil
}

func productionPublicationsByRevision(
	tx *gorm.DB,
	scope agentruntime.Scope,
	revisionIDs []string,
) (map[string]model.AgentAssetPublication, error) {
	result := make(map[string]model.AgentAssetPublication, len(revisionIDs))
	if len(revisionIDs) == 0 {
		return result, nil
	}
	var publications []model.AgentAssetPublication
	if err := tx.Model(&model.AgentAssetPublication{}).Where(
		`tenant_kind = ? AND tenant_id = ? AND actor_user_id = ? AND domain_project_id = ?
			AND canvas_id = ? AND thread_id = ? AND run_id = ? AND artifact_revision_id IN ? AND status = ?`,
		scope.TenantKind, scope.TenantID, scope.ActorUserID, scope.DomainProjectID, scope.CanvasID, scope.ThreadID, scope.RunID,
		revisionIDs, model.AgentAssetPublicationSucceeded,
	).Order("id ASC").Find(&publications).Error; err != nil {
		return nil, err
	}
	for _, publication := range publications {
		if _, found := result[publication.ArtifactRevisionID]; !found {
			result[publication.ArtifactRevisionID] = publication
		}
	}
	return result, nil
}

func uniqueStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, found := seen[value]; found {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func productionCharacterIdentityScopeQuery(query *gorm.DB, scope agentruntime.Scope) *gorm.DB {
	return query.Model(&model.AgentCharacterIdentityVersion{}).Where(
		`tenant_kind = ? AND tenant_id = ? AND actor_user_id = ? AND domain_project_id = ?
			AND canvas_id = ? AND thread_id = ? AND run_id = ?`,
		scope.TenantKind, scope.TenantID, scope.ActorUserID, scope.DomainProjectID, scope.CanvasID, scope.ThreadID, scope.RunID,
	)
}

func productionShotBindingScopeQuery(query *gorm.DB, scope agentruntime.Scope) *gorm.DB {
	return query.Model(&model.AgentShotBindingRevision{}).Where(
		`tenant_kind = ? AND tenant_id = ? AND actor_user_id = ? AND domain_project_id = ?
			AND canvas_id = ? AND thread_id = ? AND run_id = ?`,
		scope.TenantKind, scope.TenantID, scope.ActorUserID, scope.DomainProjectID, scope.CanvasID, scope.ThreadID, scope.RunID,
	)
}

func productionStageSnapshotMatches(stage model.AgentProductionStage, graphVersionID string, expected agentruntime.ProductionStageDraft) bool {
	if strings.TrimSpace(stage.ID) != stage.ID || stage.ID == "" || stage.GraphVersionID != graphVersionID ||
		stage.StageKey != expected.StageKey || stage.SpecialistKey != expected.SpecialistKey ||
		stage.ReviewPolicy != expected.ReviewPolicy || stage.CostPolicy != expected.CostPolicy ||
		!stage.Status.Valid() || stage.Version < 1 || strings.TrimSpace(stage.ReviewRevisionID) != stage.ReviewRevisionID ||
		strings.TrimSpace(stage.LastErrorCode) != stage.LastErrorCode {
		return false
	}
	var dependencies []string
	if decodeProductionSnapshotJSON(stage.DependsOnStagesJSON, &dependencies) != nil || !reflect.DeepEqual(dependencies, expected.DependsOnStageKeys) {
		return false
	}
	var inputs []agentruntime.ArtifactRevisionRef
	if decodeProductionSnapshotJSON(stage.InputRevisionsJSON, &inputs) != nil || !reflect.DeepEqual(inputs, expected.InputRevisions) {
		return false
	}
	var delivery agentruntime.ExpectedDelivery
	return decodeProductionSnapshotJSON(stage.ExpectedDeliveryJSON, &delivery) == nil && reflect.DeepEqual(delivery, expected.ExpectedDelivery)
}

func decodeProductionSnapshotJSON(encoded string, target interface{}) error {
	decoder := json.NewDecoder(bytes.NewBufferString(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return ErrProductionRuntimeSnapshotInvalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ErrProductionRuntimeSnapshotInvalid
	}
	return nil
}

func canonicalProductionGraphDraft(draft agentruntime.ProductionGraphDraft) agentruntime.ProductionGraphDraft {
	stages := make([]agentruntime.ProductionStageDraft, len(draft.Stages))
	for index, stage := range draft.Stages {
		if stage.DependsOnStageKeys == nil {
			stage.DependsOnStageKeys = []string{}
		}
		if stage.InputRevisions == nil {
			stage.InputRevisions = []agentruntime.ArtifactRevisionRef{}
		}
		stages[index] = stage
	}
	draft.Stages = stages
	return draft
}

func uniqueArtifactRevisionRefs(references []agentruntime.ArtifactRevisionRef) []agentruntime.ArtifactRevisionRef {
	unique := make([]agentruntime.ArtifactRevisionRef, 0, len(references))
	seen := make(map[string]struct{}, len(references))
	for _, reference := range references {
		identity := reference.ArtifactID + "\x00" + reference.RevisionID
		if _, exists := seen[identity]; exists {
			continue
		}
		seen[identity] = struct{}{}
		unique = append(unique, reference)
	}
	return unique
}

func (r *Repository) AdvanceProductionStageCAS(
	scope agentruntime.Scope,
	stageID string,
	expectedVersion int64,
	next agentruntime.ProductionStageStatus,
) error {
	if err := validateProductionRepositoryScope(scope, true); err != nil {
		return err
	}
	if strings.TrimSpace(stageID) != stageID || stageID == "" || len(stageID) > 80 || expectedVersion < 1 || !next.Valid() {
		return ErrProductionStageInputInvalid
	}
	var current model.AgentProductionStage
	if err := productionStageScopeQuery(r.db.Model(&model.AgentProductionStage{}), scope).
		Where("id = ?", stageID).
		First(&current).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrProductionStageConflict
		}
		return err
	}
	if current.Version != expectedVersion {
		return ErrProductionStageConflict
	}
	if err := agentruntime.ValidateProductionStageStatusTransition(current.Status, next); err != nil {
		return err
	}
	if next == agentruntime.StageAwaitingReview ||
		(current.Status == agentruntime.StageAwaitingReview && next != agentruntime.StageStopped) {
		return ErrProductionStageInputInvalid
	}

	now := time.Now().UTC()
	update := model.AgentProductionStage{Status: next, Version: expectedVersion + 1, UpdatedAt: now}
	result := productionStageScopeQuery(r.db.Model(&model.AgentProductionStage{}), scope).
		Where("id = ? AND version = ? AND status = ?", stageID, expectedVersion, current.Status).
		Select("status", "version", "updated_at").
		Updates(&update)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrProductionStageConflict
	}
	return nil
}

func (r *Repository) MarkDependentStagesStale(scope agentruntime.Scope, changedStageKey string, revisionID string) error {
	if err := validateProductionRepositoryScope(scope, true); err != nil {
		return err
	}
	if strings.TrimSpace(changedStageKey) != changedStageKey || changedStageKey == "" || len(changedStageKey) > 120 ||
		strings.TrimSpace(revisionID) != revisionID || revisionID == "" || len(revisionID) > 80 {
		return ErrProductionStageInputInvalid
	}

	return r.db.Transaction(func(tx *gorm.DB) error {
		var revision model.AgentArtifactRevision
		if err := productionArtifactRevisionScopeQuery(tx, scope).
			Where("id = ?", revisionID).
			First(&revision).Error; err != nil {
			return err
		}

		var changed model.AgentProductionStage
		if err := productionStageScopeQuery(tx.Model(&model.AgentProductionStage{}), scope).
			Joins("JOIN agent_production_graph_versions ON agent_production_graph_versions.id = agent_production_stages.graph_version_id").
			Where("agent_production_stages.stage_key = ?", changedStageKey).
			Order("agent_production_graph_versions.created_at DESC").
			Order("agent_production_graph_versions.version DESC").
			First(&changed).Error; err != nil {
			return err
		}
		if err := agentruntime.ValidateProductionStageStatusTransition(changed.Status, agentruntime.StageAwaitingReview); err != nil {
			return err
		}

		var graph model.AgentProductionGraphVersion
		if err := productionGraphScopeQuery(tx.Model(&model.AgentProductionGraphVersion{}), scope).
			Where("id = ?", changed.GraphVersionID).
			First(&graph).Error; err != nil {
			return err
		}
		var stageDrafts []agentruntime.ProductionStageDraft
		if err := json.Unmarshal([]byte(graph.StagesJSON), &stageDrafts); err != nil {
			return fmt.Errorf("decode production graph stages: %w", err)
		}
		graphDraft := agentruntime.ProductionGraphDraft{GraphKey: graph.GraphKey, Stages: stageDrafts}
		staleStageKeys, err := agentruntime.StaleDependentStages(graphDraft, changedStageKey)
		if err != nil {
			return err
		}

		var dependents []model.AgentProductionStage
		if len(staleStageKeys) > 0 {
			if err := productionStageScopeQuery(tx.Model(&model.AgentProductionStage{}), scope).
				Where("graph_version_id = ? AND stage_key IN ?", graph.ID, staleStageKeys).
				Find(&dependents).Error; err != nil {
				return err
			}
			if len(dependents) != len(staleStageKeys) {
				return ErrProductionStageConflict
			}
		}

		now := time.Now().UTC()
		changedUpdate := model.AgentProductionStage{
			Status:           agentruntime.StageAwaitingReview,
			Version:          changed.Version + 1,
			ReviewRevisionID: revision.ID,
			LastErrorCode:    "",
			UpdatedAt:        now,
		}
		changedResult := productionStageScopeQuery(tx.Model(&model.AgentProductionStage{}), scope).
			Where("id = ? AND version = ?", changed.ID, changed.Version).
			Select("status", "version", "review_revision_id", "last_error_code", "updated_at").
			Updates(&changedUpdate)
		if changedResult.Error != nil {
			return changedResult.Error
		}
		if changedResult.RowsAffected != 1 {
			return ErrProductionStageConflict
		}

		for _, dependent := range dependents {
			if dependent.Status == agentruntime.StageStopped {
				continue
			}
			staleUpdate := model.AgentProductionStage{
				Status:           agentruntime.StageStale,
				Version:          dependent.Version + 1,
				ReviewRevisionID: "",
				LastErrorCode:    "",
				UpdatedAt:        now,
			}
			staleResult := productionStageScopeQuery(tx.Model(&model.AgentProductionStage{}), scope).
				Where("id = ? AND version = ?", dependent.ID, dependent.Version).
				Select("status", "version", "review_revision_id", "last_error_code", "updated_at").
				Updates(&staleUpdate)
			if staleResult.Error != nil {
				return staleResult.Error
			}
			if staleResult.RowsAffected != 1 {
				return ErrProductionStageConflict
			}
		}
		return nil
	})
}

func validateProductionRepositoryScope(scope agentruntime.Scope, requireMutation bool) error {
	if err := scope.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrProductionScopeInvalid, err)
	}
	for _, value := range []string{
		scope.TenantID,
		scope.ActorUserID,
		scope.DomainProjectID,
		scope.CanvasID,
		scope.ThreadID,
		scope.RunID,
	} {
		if strings.TrimSpace(value) != value || value == "" || len(value) > 80 {
			return ErrProductionScopeInvalid
		}
	}
	if requireMutation && !scope.CanMutateCanvas() {
		return ErrProductionScopeInvalid
	}
	return nil
}

func productionGraphScopeQuery(query *gorm.DB, scope agentruntime.Scope) *gorm.DB {
	return query.Where(
		"agent_production_graph_versions.tenant_kind = ? AND agent_production_graph_versions.tenant_id = ? AND agent_production_graph_versions.actor_user_id = ? AND agent_production_graph_versions.domain_project_id = ? AND agent_production_graph_versions.canvas_id = ? AND agent_production_graph_versions.thread_id = ? AND agent_production_graph_versions.run_id = ?",
		scope.TenantKind, scope.TenantID, scope.ActorUserID, scope.DomainProjectID, scope.CanvasID, scope.ThreadID, scope.RunID,
	)
}

func productionStageScopeQuery(query *gorm.DB, scope agentruntime.Scope) *gorm.DB {
	return query.Where(
		"agent_production_stages.tenant_kind = ? AND agent_production_stages.tenant_id = ? AND agent_production_stages.actor_user_id = ? AND agent_production_stages.domain_project_id = ? AND agent_production_stages.canvas_id = ? AND agent_production_stages.thread_id = ? AND agent_production_stages.run_id = ?",
		scope.TenantKind, scope.TenantID, scope.ActorUserID, scope.DomainProjectID, scope.CanvasID, scope.ThreadID, scope.RunID,
	)
}

func productionGraphVersionID(scope agentruntime.Scope, graphKey string, version int64) string {
	return agentFactID(
		"production-graph-version",
		string(scope.TenantKind),
		scope.TenantID,
		scope.ActorUserID,
		scope.DomainProjectID,
		scope.CanvasID,
		scope.ThreadID,
		scope.RunID,
		graphKey,
		fmt.Sprintf("%d", version),
	)
}
