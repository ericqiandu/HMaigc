package repository

import (
	"errors"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/testsupport"

	"gorm.io/gorm"
)

func TestProductionProgressUsesExactScopedResourceEvidence(t *testing.T) {
	repo, db := productionRepositoryFixture(t)
	if err := db.AutoMigrate(&model.Resource{}, &model.CanvasChange{}); err != nil {
		t.Fatal(err)
	}
	scope := productionScopeFixture()
	draft := agentruntime.ProductionGraphDraft{GraphKey: "resource-progress", Stages: []agentruntime.ProductionStageDraft{{
		StageKey: "video", SpecialistKey: agentruntime.SpecialistVideoAssembly,
		ExpectedDelivery: agentruntime.ExpectedDelivery{
			Kind: agentruntime.DeliveryGeneratedAsset, RequiredArtifacts: []agentruntime.ArtifactKind{agentruntime.ArtifactVideo},
			CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactResource, Artifact: agentruntime.ArtifactVideo}},
		},
		ReviewPolicy: agentruntime.ReviewRequired, CostPolicy: agentruntime.CostApprovalRequired,
	}}}
	graph, err := repo.AppendProductionGraphVersion(scope, 0, draft)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AdvanceProductionStageCAS(scope, graph.Stages[0].ID, 1, agentruntime.StageRunning); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	specialistID := "specialist-progress"
	if err := db.Create(&model.AgentSpecialistRun{
		ID: specialistID, TenantKind: scope.TenantKind, TenantID: scope.TenantID, ActorUserID: scope.ActorUserID,
		DomainProjectID: scope.DomainProjectID, CanvasID: scope.CanvasID, ThreadID: scope.ThreadID, RunID: scope.RunID,
		StageID: graph.Stages[0].ID, SpecialistKey: agentruntime.SpecialistVideoAssembly, SpecialistVersion: 1,
		Objective: "render", ModelRecordID: "model-record", ModelKey: "model", ToolSchemaVersion: agentruntime.ProductionToolSchemaVersion,
		InputRevisionsJSON: `[]`, SkillVersionsJSON: `[]`, ToolAllowlistJSON: `[]`, ExpectedOutputSchema: "video.v1",
		ExpectedDeliveryJSON: `{"kind":"generated_asset","requiredArtifacts":["video"],"completionCriteria":[{"fact":"resource","artifact":"video"}]}`,
		TaskID:               "task-progress", BillingOrderID: "billing-progress", Attempt: 1, Status: model.AgentSpecialistRunSucceeded,
		Version: 2, ResultSummary: "rendered", CreatedAt: now, UpdatedAt: now, CompletedAt: &now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Task{
		ID: "task-progress", UserID: scope.ActorUserID, Audience: model.TaskAudienceInternal,
		ProjectID: scope.CanvasID, Status: model.TaskStatusSucceeded, BillingOrderID: "billing-progress",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.BillingOrder{
		ID: "billing-progress", UserID: scope.ActorUserID, TaskID: "task-progress", Status: model.BillingStatusSettled,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Resource{
		ID: "resource-progress", UserID: scope.ActorUserID, Kind: "video", Status: model.ResourceStatusPending,
		PublicURL: "https://cdn.example/video.mp4",
	}).Error; err != nil {
		t.Fatal(err)
	}
	artifact := model.AgentArtifact{
		ID: "artifact-progress", TenantKind: scope.TenantKind, TenantID: scope.TenantID, ActorUserID: scope.ActorUserID,
		DomainProjectID: scope.DomainProjectID, CanvasID: scope.CanvasID, ThreadID: scope.ThreadID, RunID: scope.RunID,
		ArtifactKey: "video-output", Kind: string(agentruntime.ArtifactVideo), HeadRevision: 1,
		LifecycleStatus: model.AgentArtifactLifecycleActive, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	revision := model.AgentArtifactRevision{
		ID: "revision-progress", TenantKind: scope.TenantKind, TenantID: scope.TenantID, ActorUserID: scope.ActorUserID,
		DomainProjectID: scope.DomainProjectID, CanvasID: scope.CanvasID, ThreadID: scope.ThreadID, RunID: scope.RunID,
		ArtifactID: artifact.ID, ArtifactKey: artifact.ArtifactKey, Revision: 1, Kind: artifact.Kind, SchemaVersion: 1,
		PayloadJSON: `{}`, ResourceID: "resource-progress", UpstreamRevisionsJSON: `[]`, SkillVersionsJSON: `[]`,
		CreatedByRunID: scope.RunID, CreatedBySpecialistID: specialistID,
		LifecycleStatus: model.AgentArtifactRevisionAwaitingReview, CreatedAt: now,
	}
	if err := db.Create(&artifact).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&revision).Error; err != nil {
		t.Fatal(err)
	}

	snapshot, err := repo.ProductionRuntimeSnapshotForScope(scope)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Progress == nil || !productionProjectionHasBlocker(*snapshot.Progress, agentruntime.ProductionBlockerResourceNotReady) {
		t.Fatalf("progress = %#v, want resource_not_ready", snapshot.Progress)
	}
	if len(snapshot.Progress.EligibleActions) != 0 {
		t.Fatalf("eligible actions = %#v, want none", snapshot.Progress.EligibleActions)
	}
}

func TestPostgresIdentityVersionAndShotBindingMigrationIsIdempotent(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.EnsureAgentProductionRuntimeSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureAgentProductionRuntimeSchema(db); err != nil {
		t.Fatalf("second PostgreSQL migration failed: %v", err)
	}
	identity := model.AgentCharacterIdentityVersion{
		ID: "postgres-identity", TenantKind: "personal", TenantID: "user", ActorUserID: "user",
		DomainProjectID: "project", CanvasID: "canvas", ThreadID: "thread", RunID: "run",
		CharacterKey: "character", Version: 1, CharacterBibleRevisionID: "bible-r1", ResourceID: "resource",
		DependencyHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", LifecycleStatus: "current",
	}
	if err := db.Create(&identity).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentCharacterIdentityVersion{}).Where("id = ?", identity.ID).
		Update("resource_id", "other-resource").Error; err == nil {
		t.Fatal("PostgreSQL identity version update was accepted")
	}
	duplicate := identity
	duplicate.ID = "postgres-identity-duplicate"
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("PostgreSQL duplicate identity version was accepted")
	}
	if err := db.Delete(&model.AgentCharacterIdentityVersion{}, "id = ?", identity.ID).Error; err == nil {
		t.Fatal("PostgreSQL identity version delete was accepted")
	}
	shotBinding := model.AgentShotBindingRevision{
		ID: "postgres-shot-binding", TenantKind: "personal", TenantID: "user", ActorUserID: "user",
		DomainProjectID: "project", CanvasID: "canvas", ThreadID: "thread", RunID: "run",
		ShotKey: "shot", CharacterKey: "character", Revision: 1, ShotArtifactRevisionID: "shot-r1",
		IdentityVersionID: identity.ID, ResourceID: "resource",
		DependencyHash:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		LifecycleStatus: agentruntime.ProductionEvidenceCurrent,
	}
	if err := db.Create(&shotBinding).Error; err != nil {
		t.Fatal(err)
	}
	duplicateBinding := shotBinding
	duplicateBinding.ID = "postgres-shot-binding-duplicate"
	if err := db.Create(&duplicateBinding).Error; err == nil {
		t.Fatal("PostgreSQL duplicate shot binding revision was accepted")
	}
	if err := db.Model(&model.AgentShotBindingRevision{}).Where("id = ?", shotBinding.ID).
		Update("resource_id", "other-resource").Error; err == nil {
		t.Fatal("PostgreSQL shot binding update was accepted")
	}
	if err := db.Delete(&model.AgentShotBindingRevision{}, "id = ?", shotBinding.ID).Error; err == nil {
		t.Fatal("PostgreSQL shot binding delete was accepted")
	}
}

func productionProjectionHasBlocker(projection agentruntime.ProductionNextActionProjection, code agentruntime.ProductionBlockerCode) bool {
	for _, blocker := range projection.Blockers {
		if blocker.Code == code {
			return true
		}
	}
	return false
}

func productionRepositoryFixture(t *testing.T) (*Repository, *gorm.DB) {
	t.Helper()
	repo, db := openAgentRuntimeRepositorySQLite(t)
	if err := database.EnsureAgentProductionRuntimeSchema(db); err != nil {
		t.Fatalf("migrate production repository schema: %v", err)
	}
	return repo, db
}

func productionScopeFixture() agentruntime.Scope {
	return agentruntime.Scope{
		TenantKind:      agentruntime.TenantPersonal,
		TenantID:        "production-user-1",
		ActorUserID:     "production-user-1",
		DomainProjectID: "production-project-1",
		CanvasID:        "production-canvas-1",
		ThreadID:        "production-thread-1",
		RunID:           "production-run-1",
		Access: agentruntime.AccessGrant{
			Level:              agentruntime.AccessManager,
			SubscriptionActive: true,
		},
	}
}

func productionStageFixture(stageKey string, specialist agentruntime.SpecialistKey, dependencies ...string) agentruntime.ProductionStageDraft {
	return agentruntime.ProductionStageDraft{
		StageKey:           stageKey,
		SpecialistKey:      specialist,
		DependsOnStageKeys: dependencies,
		ExpectedDelivery: agentruntime.ExpectedDelivery{
			Kind: agentruntime.DeliveryAnswer,
			CompletionCriteria: []agentruntime.DeliveryCriterion{
				{Fact: agentruntime.DeliveryFactFinalMessage},
			},
		},
		ReviewPolicy: agentruntime.ReviewRequired,
		CostPolicy:   agentruntime.CostNone,
	}
}

func productionGraphFixture() agentruntime.ProductionGraphDraft {
	return agentruntime.ProductionGraphDraft{
		GraphKey: "short-film",
		Stages: []agentruntime.ProductionStageDraft{
			productionStageFixture("script", agentruntime.SpecialistNarrative),
			productionStageFixture("storyboard", agentruntime.SpecialistStoryboard, "script"),
			productionStageFixture("video", agentruntime.SpecialistVideoAssembly, "storyboard"),
		},
	}
}

func TestAppendProductionGraphVersionPersistsImmutableScopedStages(t *testing.T) {
	repo, _ := productionRepositoryFixture(t)
	scope := productionScopeFixture()

	result, err := repo.AppendProductionGraphVersion(scope, 0, productionGraphFixture())
	if err != nil {
		t.Fatal(err)
	}
	if result.Graph.Version != 1 || result.Graph.SchemaVersion != agentruntime.CurrentProductionSchemaVersion || len(result.Stages) != 3 {
		t.Fatalf("graph result = %#v", result)
	}
	for _, stage := range result.Stages {
		if stage.GraphVersionID != result.Graph.ID || stage.Status != agentruntime.StagePlanned || stage.Version != 1 {
			t.Fatalf("stage = %#v", stage)
		}
		if stage.TenantID != scope.TenantID || stage.ActorUserID != scope.ActorUserID || stage.DomainProjectID != scope.DomainProjectID ||
			stage.CanvasID != scope.CanvasID || stage.ThreadID != scope.ThreadID || stage.RunID != scope.RunID {
			t.Fatalf("stage scope = %#v, want %#v", stage, scope)
		}
	}
	if result.Stages[0].DependsOnStagesJSON != `[]` || result.Stages[0].InputRevisionsJSON != `[]` {
		t.Fatalf("empty stage arrays were not canonicalized: %#v", result.Stages[0])
	}

	if _, err := repo.AppendProductionGraphVersion(scope, 0, productionGraphFixture()); !errors.Is(err, ErrProductionGraphVersionConflict) {
		t.Fatalf("stale graph append error = %v, want %v", err, ErrProductionGraphVersionConflict)
	}
}

func TestAppendProductionGraphVersionRequiresScopedInputRevisions(t *testing.T) {
	repo, _ := productionRepositoryFixture(t)
	scope := productionScopeFixture()
	draft := productionGraphFixture()
	draft.Stages[0].InputRevisions = []agentruntime.ArtifactRevisionRef{{
		ArtifactID: "missing-artifact", RevisionID: "missing-revision",
	}}

	if _, err := repo.AppendProductionGraphVersion(scope, 0, draft); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing graph input error = %v, want record not found", err)
	}
}

func TestAdvanceProductionStageCASRejectsStaleVersionAndCrossScope(t *testing.T) {
	repo, _ := productionRepositoryFixture(t)
	scope := productionScopeFixture()
	result, err := repo.AppendProductionGraphVersion(scope, 0, productionGraphFixture())
	if err != nil {
		t.Fatal(err)
	}
	stageID := result.Stages[0].ID

	other := scope
	other.TenantID = "production-user-2"
	other.ActorUserID = "production-user-2"
	if err := repo.AdvanceProductionStageCAS(other, stageID, 1, agentruntime.StageRunning); !errors.Is(err, ErrProductionStageConflict) {
		t.Fatalf("cross-scope stage advance error = %v, want %v", err, ErrProductionStageConflict)
	}
	if err := repo.AdvanceProductionStageCAS(scope, stageID, 1, agentruntime.StageRunning); err != nil {
		t.Fatal(err)
	}
	if err := repo.AdvanceProductionStageCAS(scope, stageID, 1, agentruntime.StageCompleted); !errors.Is(err, ErrProductionStageConflict) {
		t.Fatalf("stale stage advance error = %v, want %v", err, ErrProductionStageConflict)
	}
}

func TestAdvanceProductionStageCASRejectsInvalidLifecycleJump(t *testing.T) {
	repo, db := productionRepositoryFixture(t)
	scope := productionScopeFixture()
	result, err := repo.AppendProductionGraphVersion(scope, 0, productionGraphFixture())
	if err != nil {
		t.Fatal(err)
	}
	stageID := result.Stages[0].ID

	if err := repo.AdvanceProductionStageCAS(scope, stageID, 1, agentruntime.StageCompleted); !errors.Is(err, agentruntime.ErrProductionStageTransitionInvalid) {
		t.Fatalf("invalid lifecycle jump error = %v", err)
	}
	var stored model.AgentProductionStage
	if err := db.First(&stored, "id = ?", stageID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != agentruntime.StagePlanned || stored.Version != 1 {
		t.Fatalf("invalid lifecycle jump mutated stage: %#v", stored)
	}
}

func TestMarkDependentStagesStaleIsTransitive(t *testing.T) {
	repo, db := productionRepositoryFixture(t)
	scope := productionScopeFixture()
	result, err := repo.AppendProductionGraphVersion(scope, 0, productionGraphFixture())
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AdvanceProductionStageCAS(scope, result.Stages[0].ID, 1, agentruntime.StageRunning); err != nil {
		t.Fatal(err)
	}
	revision, err := repo.AppendArtifactRevision(scope, "script-artifact", 0, artifactDraftFixture())
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.MarkDependentStagesStale(scope, "script", revision.ID); err != nil {
		t.Fatal(err)
	}
	var changed model.AgentProductionStage
	if err := db.Where("id = ?", result.Stages[0].ID).First(&changed).Error; err != nil {
		t.Fatal(err)
	}
	if changed.Status != agentruntime.StageAwaitingReview || changed.Version != 3 || changed.ReviewRevisionID != revision.ID {
		t.Fatalf("changed stage = %#v", changed)
	}
	for _, stage := range result.Stages[1:] {
		var stored model.AgentProductionStage
		if err := db.Where("id = ?", stage.ID).First(&stored).Error; err != nil {
			t.Fatal(err)
		}
		if stored.Status != agentruntime.StageStale || stored.Version != 2 || stored.ReviewRevisionID != "" {
			t.Fatalf("dependent stage = %#v", stored)
		}
	}
}

func TestMarkDependentStagesStaleRejectsStageThatWasNeverRunning(t *testing.T) {
	repo, db := productionRepositoryFixture(t)
	scope := productionScopeFixture()
	result, err := repo.AppendProductionGraphVersion(scope, 0, productionGraphFixture())
	if err != nil {
		t.Fatal(err)
	}
	revision, err := repo.AppendArtifactRevision(scope, "script-artifact", 0, artifactDraftFixture())
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.MarkDependentStagesStale(scope, "script", revision.ID); !errors.Is(err, agentruntime.ErrProductionStageTransitionInvalid) {
		t.Fatalf("unstarted stage review error = %v, want %v", err, agentruntime.ErrProductionStageTransitionInvalid)
	}
	var stored model.AgentProductionStage
	if err := db.First(&stored, "id = ?", result.Stages[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != agentruntime.StagePlanned || stored.Version != 1 || stored.ReviewRevisionID != "" {
		t.Fatalf("unstarted stage was mutated: %#v", stored)
	}
}

func TestProductionRuntimeSnapshotReturnsLatestScopedGraphAndHeadArtifacts(t *testing.T) {
	repo, _ := productionRepositoryFixture(t)
	scope := productionScopeFixture()
	if _, err := repo.AppendProductionGraphVersion(scope, 0, productionGraphFixture()); err != nil {
		t.Fatal(err)
	}
	latestGraph, err := repo.AppendProductionGraphVersion(scope, 1, productionGraphFixture())
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AdvanceProductionStageCAS(scope, latestGraph.Stages[0].ID, 1, agentruntime.StageRunning); err != nil {
		t.Fatal(err)
	}
	firstRevision, err := repo.AppendArtifactRevision(scope, "script-artifact", 0, artifactDraftFixture())
	if err != nil {
		t.Fatal(err)
	}
	secondRevision, err := repo.AppendArtifactRevision(scope, "script-artifact", firstRevision.Revision, artifactDraftFixture())
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := repo.ProductionRuntimeSnapshotForScope(scope)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Graph == nil || snapshot.Draft == nil || snapshot.Graph.ID != latestGraph.Graph.ID || snapshot.Graph.Version != 2 {
		t.Fatalf("snapshot graph = %#v, draft = %#v", snapshot.Graph, snapshot.Draft)
	}
	if len(snapshot.Stages) != len(snapshot.Draft.Stages) || snapshot.Stages[0].ID != latestGraph.Stages[0].ID ||
		snapshot.Stages[0].Status != agentruntime.StageRunning || snapshot.Stages[0].Version != 2 {
		t.Fatalf("snapshot stages = %#v", snapshot.Stages)
	}
	if len(snapshot.Artifacts) != 1 || snapshot.Artifacts[0].Artifact.HeadRevision != 2 ||
		snapshot.Artifacts[0].Revision.ID != secondRevision.ID || snapshot.Artifacts[0].Revision.Revision != 2 {
		t.Fatalf("snapshot artifacts = %#v", snapshot.Artifacts)
	}
	if snapshot.Artifacts[0].Revision.PayloadJSON != "" {
		t.Fatalf("snapshot exposed artifact payload: %q", snapshot.Artifacts[0].Revision.PayloadJSON)
	}

	other := scope
	other.TenantID = "production-user-2"
	other.ActorUserID = "production-user-2"
	hidden, err := repo.ProductionRuntimeSnapshotForScope(other)
	if err != nil {
		t.Fatal(err)
	}
	if hidden.Graph != nil || hidden.Draft != nil || len(hidden.Stages) != 0 || len(hidden.Artifacts) != 0 {
		t.Fatalf("cross-scope snapshot leaked facts: %#v", hidden)
	}
}

func TestProductionIdentityFactsRejectCrossScopeArtifactAndResourceReferences(t *testing.T) {
	for _, testCase := range []struct {
		name string
		seed func(t *testing.T, db *gorm.DB, scope agentruntime.Scope, other agentruntime.Scope)
	}{
		{
			name: "character identity",
			seed: func(t *testing.T, db *gorm.DB, scope agentruntime.Scope, other agentruntime.Scope) {
				t.Helper()
				createProductionEvidenceResourceAndRevision(t, db, other, "foreign-character-resource", "foreign-character-revision")
				identity := productionIdentityForScope(scope, "identity-cross-scope", "foreign-character-revision", "foreign-character-resource")
				if err := db.Create(&identity).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "shot binding",
			seed: func(t *testing.T, db *gorm.DB, scope agentruntime.Scope, other agentruntime.Scope) {
				t.Helper()
				createProductionEvidenceResourceAndRevision(t, db, scope, "character-resource", "character-revision")
				identity := productionIdentityForScope(scope, "identity-local", "character-revision", "character-resource")
				if err := db.Create(&identity).Error; err != nil {
					t.Fatal(err)
				}
				createProductionEvidenceResourceAndRevision(t, db, other, "foreign-shot-resource", "foreign-shot-revision")
				binding := model.AgentShotBindingRevision{
					ID: "binding-cross-scope", TenantKind: scope.TenantKind, TenantID: scope.TenantID, ActorUserID: scope.ActorUserID,
					DomainProjectID: scope.DomainProjectID, CanvasID: scope.CanvasID, ThreadID: scope.ThreadID, RunID: scope.RunID,
					ShotKey: "shot-1", CharacterKey: identity.CharacterKey, Revision: 1,
					ShotArtifactRevisionID: "foreign-shot-revision", IdentityVersionID: identity.ID, ResourceID: "foreign-shot-resource",
					DependencyHash: strings.Repeat("c", 64), LifecycleStatus: agentruntime.ProductionEvidenceCurrent,
				}
				if err := db.Create(&binding).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repo, db := productionRepositoryFixture(t)
			if err := db.AutoMigrate(&model.Resource{}); err != nil {
				t.Fatal(err)
			}
			scope := productionScopeFixture()
			if _, err := repo.AppendProductionGraphVersion(scope, 0, productionGraphFixture()); err != nil {
				t.Fatal(err)
			}
			other := scope
			other.TenantID = "production-user-2"
			other.ActorUserID = "production-user-2"
			testCase.seed(t, db, scope, other)

			if _, err := repo.ProductionRuntimeSnapshotForScope(scope); !errors.Is(err, ErrProductionRuntimeSnapshotInvalid) {
				t.Fatalf("cross-scope evidence error = %v, want %v", err, ErrProductionRuntimeSnapshotInvalid)
			}
		})
	}
}

func productionIdentityForScope(
	scope agentruntime.Scope,
	id string,
	revisionID string,
	resourceID string,
) model.AgentCharacterIdentityVersion {
	return model.AgentCharacterIdentityVersion{
		ID: id, TenantKind: scope.TenantKind, TenantID: scope.TenantID, ActorUserID: scope.ActorUserID,
		DomainProjectID: scope.DomainProjectID, CanvasID: scope.CanvasID, ThreadID: scope.ThreadID, RunID: scope.RunID,
		CharacterKey: "character-1", Version: 1, CharacterBibleRevisionID: revisionID, ResourceID: resourceID,
		DependencyHash: strings.Repeat("a", 64), LifecycleStatus: agentruntime.ProductionEvidenceCurrent,
	}
}

func createProductionEvidenceResourceAndRevision(
	t *testing.T,
	db *gorm.DB,
	scope agentruntime.Scope,
	resourceID string,
	revisionID string,
) {
	t.Helper()
	if err := db.Create(&model.Resource{
		ID: resourceID, UserID: scope.ActorUserID, Kind: string(agentruntime.ArtifactImage),
		Status: model.ResourceStatusReady, PublicURL: "https://cdn.example/" + resourceID + ".png",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AgentArtifactRevision{
		ID: revisionID, TenantKind: scope.TenantKind, TenantID: scope.TenantID, ActorUserID: scope.ActorUserID,
		DomainProjectID: scope.DomainProjectID, CanvasID: scope.CanvasID, ThreadID: scope.ThreadID, RunID: scope.RunID,
		ArtifactID: "artifact-" + revisionID, ArtifactKey: "artifact-key-" + revisionID, Revision: 1,
		Kind: string(agentruntime.ArtifactImage), SchemaVersion: 1, PayloadJSON: `{}`, ResourceID: resourceID,
		UpstreamRevisionsJSON: `[]`, SkillVersionsJSON: `[]`, CreatedByRunID: scope.RunID,
		LifecycleStatus: model.AgentArtifactRevisionAwaitingReview,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestProductionRuntimeSnapshotRejectsMultipleGraphIdentities(t *testing.T) {
	repo, _ := productionRepositoryFixture(t)
	scope := productionScopeFixture()
	if _, err := repo.AppendProductionGraphVersion(scope, 0, productionGraphFixture()); err != nil {
		t.Fatal(err)
	}
	second := productionGraphFixture()
	second.GraphKey = "alternate-short-film"
	if _, err := repo.AppendProductionGraphVersion(scope, 0, second); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.ProductionRuntimeSnapshotForScope(scope); !errors.Is(err, ErrProductionRuntimeSnapshotInvalid) {
		t.Fatalf("multiple graph snapshot error = %v, want %v", err, ErrProductionRuntimeSnapshotInvalid)
	}
}
