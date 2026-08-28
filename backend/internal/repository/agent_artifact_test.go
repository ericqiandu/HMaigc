package repository

import (
	"encoding/json"
	"errors"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"

	"gorm.io/gorm"
)

func artifactDraftFixture() agentruntime.ArtifactDraft {
	return agentruntime.ArtifactDraft{
		ArtifactKey:   "script",
		Kind:          "script",
		SchemaVersion: 1,
		Payload:       json.RawMessage(`{"title":"第一版剧本"}`),
	}
}

func TestAppendArtifactRevisionIsMonotonicUnderConflict(t *testing.T) {
	repo, _ := productionRepositoryFixture(t)
	scope := productionScopeFixture()

	first, err := repo.AppendArtifactRevision(scope, "artifact-1", 0, artifactDraftFixture())
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != 1 || first.ArtifactID != "artifact-1" || first.CreatedByRunID != scope.RunID ||
		first.UpstreamRevisionsJSON != `[]` || first.SkillVersionsJSON != `[]` {
		t.Fatalf("first revision = %#v", first)
	}

	if _, err := repo.AppendArtifactRevision(scope, "artifact-1", 0, artifactDraftFixture()); !errors.Is(err, ErrArtifactRevisionConflict) {
		t.Fatalf("stale artifact append error = %v, want %v", err, ErrArtifactRevisionConflict)
	}
	stored, err := repo.ArtifactRevisionInScope(scope, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ID != first.ID || stored.PayloadJSON != first.PayloadJSON {
		t.Fatalf("stored revision = %#v, want %#v", stored, first)
	}
}

func TestAppendArtifactRevisionRequiresExactScopedUpstreamRevision(t *testing.T) {
	repo, _ := productionRepositoryFixture(t)
	scope := productionScopeFixture()
	upstreamDraft := artifactDraftFixture()
	upstreamDraft.ArtifactKey = "brief"
	upstreamDraft.Kind = "brief"
	upstream, err := repo.AppendArtifactRevision(scope, "brief-artifact", 0, upstreamDraft)
	if err != nil {
		t.Fatal(err)
	}

	draft := artifactDraftFixture()
	draft.UpstreamRevisions = []agentruntime.ArtifactRevisionRef{{ArtifactID: upstream.ArtifactID, RevisionID: upstream.ID}}
	stored, err := repo.AppendArtifactRevision(scope, "script-artifact", 0, draft)
	if err != nil {
		t.Fatal(err)
	}
	wantUpstream := `[{"artifactId":"brief-artifact","revisionId":"` + upstream.ID + `"}]`
	if stored.UpstreamRevisionsJSON != wantUpstream {
		t.Fatalf("upstream revisions = %s, want %s", stored.UpstreamRevisionsJSON, wantUpstream)
	}

	draft.UpstreamRevisions[0].RevisionID = "missing-revision"
	if _, err := repo.AppendArtifactRevision(scope, "invalid-script-artifact", 0, draft); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing upstream error = %v, want record not found", err)
	}

	draft.UpstreamRevisions[0].RevisionID = upstream.ID
	draft.UpstreamRevisions[0].ArtifactID = "wrong-artifact"
	if _, err := repo.AppendArtifactRevision(scope, "mismatched-script-artifact", 0, draft); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("mismatched upstream error = %v, want record not found", err)
	}
}

func TestArtifactRevisionInScopeHidesCrossScopeOwnership(t *testing.T) {
	repo, _ := productionRepositoryFixture(t)
	scope := productionScopeFixture()
	revision, err := repo.AppendArtifactRevision(scope, "artifact-1", 0, artifactDraftFixture())
	if err != nil {
		t.Fatal(err)
	}

	other := scope
	other.ActorUserID = "production-user-2"
	other.TenantID = "production-user-2"
	if _, err := repo.ArtifactRevisionInScope(other, revision.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-scope artifact read error = %v, want record not found", err)
	}
}

func TestMediaCandidateLedgerKeepsDistinctProviderOutputsAndReplaysExactly(t *testing.T) {
	repo, _ := productionRepositoryFixture(t)
	scope := productionScopeFixture()
	candidates := []struct {
		artifactID string
		resourceID string
		requestID  string
	}{
		{artifactID: "candidate-a", resourceID: "resource-a", requestID: "provider-request-a"},
		{artifactID: "candidate-b", resourceID: "resource-b", requestID: "provider-request-b"},
		{artifactID: "candidate-c", resourceID: "resource-c", requestID: "provider-request-c"},
	}
	for _, candidate := range candidates {
		draft := agentruntime.ArtifactDraft{
			ArtifactKey: candidate.artifactID, Kind: "media_candidate", SchemaVersion: 1,
			Payload:    json.RawMessage(`{"candidateKey":"` + candidate.artifactID + `","mediaKind":"image","resourceId":"` + candidate.resourceID + `","sourceTaskId":"task-` + candidate.artifactID + `","providerRequestIdentity":"` + candidate.requestID + `"}`),
			ResourceID: candidate.resourceID, UpstreamRevisions: []agentruntime.ArtifactRevisionRef{},
			ModelRequestIdentity: candidate.requestID, SkillVersions: []agentruntime.SkillSelection{},
		}
		first, err := repo.AppendMediaCandidateRevision(scope, candidate.artifactID, draft)
		if err != nil {
			t.Fatal(err)
		}
		replayed, err := repo.AppendMediaCandidateRevision(scope, candidate.artifactID, draft)
		if err != nil {
			t.Fatal(err)
		}
		if replayed.ID != first.ID || replayed.ResourceID != candidate.resourceID || replayed.ModelRequestIdentity != candidate.requestID {
			t.Fatalf("candidate replay = %#v, want %#v", replayed, first)
		}
	}

	stored, err := repo.MediaCandidateRevisionsInScope(scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != len(candidates) {
		t.Fatalf("candidate revisions = %d, want %d", len(stored), len(candidates))
	}
}

func TestVisualConsistencyReviewRejectsStaleUpstreamRevision(t *testing.T) {
	repo, _ := productionRepositoryFixture(t)
	scope := productionScopeFixture()
	sourceDraft := artifactDraftFixture()
	sourceDraft.ArtifactKey = "candidate"
	sourceDraft.Kind = "media_candidate"
	sourceDraft.ResourceID = "resource-candidate"
	sourceDraft.ModelRequestIdentity = "provider-request-candidate"
	first, err := repo.AppendArtifactRevision(scope, "candidate-artifact", 0, sourceDraft)
	if err != nil {
		t.Fatal(err)
	}
	sourceDraft.Payload = json.RawMessage(`{"title":"第二个候选版本"}`)
	if _, err := repo.AppendArtifactRevision(scope, "candidate-artifact", 1, sourceDraft); err != nil {
		t.Fatal(err)
	}

	_, err = repo.AppendArtifactRevisionOnce(scope, "consistency-review", agentruntime.ArtifactDraft{
		ArtifactKey: "consistency-review", Kind: "visual_consistency_review", SchemaVersion: 1,
		Payload:              json.RawMessage(`{"reviewRunId":"review-run"}`),
		UpstreamRevisions:    []agentruntime.ArtifactRevisionRef{{ArtifactID: first.ArtifactID, RevisionID: first.ID}},
		ModelRequestIdentity: "review-request", SkillVersions: []agentruntime.SkillSelection{},
	})
	if !errors.Is(err, ErrArtifactUpstreamRevisionStale) {
		t.Fatalf("stale review append error = %v, want %v", err, ErrArtifactUpstreamRevisionStale)
	}
}

func TestAppendingSourceRevisionMarksDependentVisualEvidenceStale(t *testing.T) {
	repo, db := productionRepositoryFixture(t)
	scope := productionScopeFixture()
	sourceDraft := agentruntime.ArtifactDraft{
		ArtifactKey: "source-image", Kind: "image_asset", SchemaVersion: 1,
		Payload: json.RawMessage(`{"caption":"雨夜街道"}`), ResourceID: "resource-image-1",
	}
	sourceRevision, err := repo.AppendArtifactRevision(scope, "source-image-artifact", 0, sourceDraft)
	if err != nil {
		t.Fatal(err)
	}
	sourceRef := agentruntime.ArtifactRevisionRef{ArtifactID: sourceRevision.ArtifactID, RevisionID: sourceRevision.ID}
	evidencePayload := json.RawMessage(`{
		"sourceRevision":{"artifactId":"source-image-artifact","revisionId":"` + sourceRevision.ID + `"},
		"characters":[],"identityEvidence":[],
		"scene":{"key":"scene-1","description":"雨夜街道"},
		"props":[],"spatialRelations":[],
		"shot":{"shotSize":"全景","angle":"平视","composition":"中心构图","screenDirection":"无","gaze":"无","firstFrameCondition":"空镜","lastFrameCondition":"空镜"},
		"actionState":"无人物动作","ocrText":[],"uncertainties":[],"conflicts":[],
		"confidenceBasisPoints":9000,"visionModelRecordId":"vision-model-record-1","requestIdentity":"provider-request-1"
	}`)
	evidenceRevision, err := repo.AppendArtifactRevision(scope, "visual-evidence-artifact", 0, agentruntime.ArtifactDraft{
		ArtifactKey: "visual-evidence-source-image", Kind: "visual_evidence", SchemaVersion: 1,
		Payload: evidencePayload, UpstreamRevisions: []agentruntime.ArtifactRevisionRef{sourceRef},
		ModelRequestIdentity: "provider-request-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	sourceDraft.Payload = json.RawMessage(`{"caption":"雨夜街道，人物已进入画面"}`)
	if _, err := repo.AppendArtifactRevision(scope, sourceRevision.ArtifactID, sourceRevision.Revision, sourceDraft); err != nil {
		t.Fatal(err)
	}

	var stored struct {
		LifecycleStatus string `gorm:"column:lifecycle_status"`
	}
	if err := db.Table("agent_artifact_revisions").Select("lifecycle_status").Where("id = ?", evidenceRevision.ID).Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.LifecycleStatus != "stale" {
		t.Fatalf("visual evidence lifecycle = %q, want stale", stored.LifecycleStatus)
	}
}

func TestAppendVisualEvidenceRejectsNonHeadSourceRevision(t *testing.T) {
	repo, _ := productionRepositoryFixture(t)
	scope := productionScopeFixture()
	sourceDraft := agentruntime.ArtifactDraft{
		ArtifactKey: "source-image", Kind: "image_asset", SchemaVersion: 1,
		Payload: json.RawMessage(`{"caption":"雨夜街道"}`), ResourceID: "resource-image-1",
	}
	first, err := repo.AppendArtifactRevision(scope, "source-image-artifact", 0, sourceDraft)
	if err != nil {
		t.Fatal(err)
	}
	sourceDraft.Payload = json.RawMessage(`{"caption":"雨夜街道，人物已进入画面"}`)
	if _, err := repo.AppendArtifactRevision(scope, first.ArtifactID, first.Revision, sourceDraft); err != nil {
		t.Fatal(err)
	}

	sourceRef := agentruntime.ArtifactRevisionRef{ArtifactID: first.ArtifactID, RevisionID: first.ID}
	evidencePayload := json.RawMessage(`{
		"sourceRevision":{"artifactId":"source-image-artifact","revisionId":"` + first.ID + `"},
		"characters":[],"identityEvidence":[],
		"scene":{"key":"scene-1","description":"雨夜街道"},
		"props":[],"spatialRelations":[],
		"shot":{"shotSize":"全景","angle":"平视","composition":"中心构图","screenDirection":"无","gaze":"无","firstFrameCondition":"空镜","lastFrameCondition":"空镜"},
		"actionState":"无人物动作","ocrText":[],"uncertainties":[],"conflicts":[],
		"confidenceBasisPoints":9000,"visionModelRecordId":"vision-model-record-1","requestIdentity":"provider-request-1"
	}`)
	_, err = repo.AppendArtifactRevision(scope, "visual-evidence-artifact", 0, agentruntime.ArtifactDraft{
		ArtifactKey: "visual-evidence-source-image", Kind: "visual_evidence", SchemaVersion: 1,
		Payload: evidencePayload, UpstreamRevisions: []agentruntime.ArtifactRevisionRef{sourceRef},
		ModelRequestIdentity: "provider-request-1",
	})
	if !errors.Is(err, ErrArtifactUpstreamRevisionStale) {
		t.Fatalf("non-head visual evidence source error = %v, want %v", err, ErrArtifactUpstreamRevisionStale)
	}
}
