package repository

import (
	"errors"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

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
