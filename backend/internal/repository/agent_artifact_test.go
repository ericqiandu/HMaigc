package repository

import (
	"encoding/json"
	"errors"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

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

func TestShotRevisionDialogueChangeLocalStaleKeepsOldCandidatesReadable(t *testing.T) {
	repo, db := productionRepositoryFixture(t)
	scope := productionScopeFixture()
	shotOne := appendLocalStaleArtifact(t, repo, scope, "shot-one", "shot_revision", nil)
	shotTwo := appendLocalStaleArtifact(t, repo, scope, "shot-two", "shot_revision", nil)
	videoOne := appendLocalStaleCandidate(t, repo, scope, "video-one", agentruntime.ArtifactVideo, shotOne)
	audioOne := appendLocalStaleCandidate(t, repo, scope, "audio-one", agentruntime.ArtifactAudio, shotOne)
	videoTwo := appendLocalStaleCandidate(t, repo, scope, "video-two", agentruntime.ArtifactVideo, shotTwo)
	assembly := appendLocalStaleArtifact(t, repo, scope, "assembly-one", "assembly_output", []agentruntime.ArtifactRevisionRef{
		artifactRevisionRef(videoOne), artifactRevisionRef(audioOne), artifactRevisionRef(videoTwo),
	})

	updatedShot := agentruntime.ArtifactDraft{
		ArtifactKey: "shot-one", Kind: "shot_revision", SchemaVersion: 1,
		Payload: json.RawMessage(`{"dialogue":"第二版对白"}`), UpstreamRevisions: []agentruntime.ArtifactRevisionRef{},
	}
	if _, err := repo.AppendArtifactRevision(scope, shotOne.ArtifactID, shotOne.Revision, updatedShot); err != nil {
		t.Fatal(err)
	}

	assertArtifactRevisionLifecycle(t, db, videoOne.ID, model.AgentArtifactRevisionStale)
	assertArtifactRevisionLifecycle(t, db, audioOne.ID, model.AgentArtifactRevisionStale)
	assertArtifactRevisionLifecycle(t, db, assembly.ID, model.AgentArtifactRevisionStale)
	assertArtifactRevisionLifecycle(t, db, shotTwo.ID, model.AgentArtifactRevisionAwaitingReview)
	assertArtifactRevisionLifecycle(t, db, videoTwo.ID, model.AgentArtifactRevisionAwaitingReview)

	candidates, err := repo.MediaCandidateRevisionsInScope(scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 3 {
		t.Fatalf("readable media candidates = %d, want 3", len(candidates))
	}
	wantIDs := map[string]struct{}{videoOne.ID: {}, audioOne.ID: {}, videoTwo.ID: {}}
	for _, candidate := range candidates {
		if _, found := wantIDs[candidate.ID]; !found {
			t.Fatalf("unexpected candidate remained readable: %#v", candidate)
		}
	}
}

func TestAssemblyPlanRevisionRejectsStaleCandidateAndAcceptsCurrentReplacement(t *testing.T) {
	repo, db := productionRepositoryFixture(t)
	scope := productionScopeFixture()
	shotOne := appendLocalStaleArtifact(t, repo, scope, "shot-one", "shot_revision", nil)
	shotTwo := appendLocalStaleArtifact(t, repo, scope, "shot-two", "shot_revision", nil)
	videoOne := appendLocalStaleCandidate(t, repo, scope, "video-one-attempt-one", agentruntime.ArtifactVideo, shotOne)
	videoTwo := appendLocalStaleCandidate(t, repo, scope, "video-two-attempt-one", agentruntime.ArtifactVideo, shotTwo)
	firstPlanDraft := assemblyPlanDraftFixture(
		"assembly-plan",
		artifactRevisionRef(videoOne),
		artifactRevisionRef(videoTwo),
	)
	firstPlan, err := repo.AppendArtifactRevision(scope, "assembly-plan-artifact", 0, firstPlanDraft)
	if err != nil {
		t.Fatal(err)
	}

	updatedShot, err := repo.AppendArtifactRevision(scope, shotOne.ArtifactID, shotOne.Revision, agentruntime.ArtifactDraft{
		ArtifactKey: "shot-one", Kind: "shot_revision", SchemaVersion: 1,
		Payload: json.RawMessage(`{"dialogue":"第二版对白"}`), UpstreamRevisions: []agentruntime.ArtifactRevisionRef{},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertArtifactRevisionLifecycle(t, db, videoOne.ID, model.AgentArtifactRevisionStale)
	assertArtifactRevisionLifecycle(t, db, firstPlan.ID, model.AgentArtifactRevisionStale)

	stalePlanDraft := assemblyPlanDraftFixture(
		"assembly-plan",
		artifactRevisionRef(videoOne),
		artifactRevisionRef(videoTwo),
	)
	if _, err := repo.AppendArtifactRevision(scope, firstPlan.ArtifactID, firstPlan.Revision, stalePlanDraft); !errors.Is(err, ErrArtifactUpstreamRevisionStale) {
		t.Fatalf("stale assembly plan append error = %v, want %v", err, ErrArtifactUpstreamRevisionStale)
	}

	replacementVideo := appendLocalStaleCandidate(t, repo, scope, "video-one-attempt-two", agentruntime.ArtifactVideo, updatedShot)
	currentPlanDraft := assemblyPlanDraftFixture(
		"assembly-plan",
		artifactRevisionRef(replacementVideo),
		artifactRevisionRef(videoTwo),
	)
	currentPlan, err := repo.AppendArtifactRevision(scope, firstPlan.ArtifactID, firstPlan.Revision, currentPlanDraft)
	if err != nil {
		t.Fatal(err)
	}
	if currentPlan.Revision != 2 {
		t.Fatalf("current assembly plan revision = %d, want 2", currentPlan.Revision)
	}
	assertArtifactRevisionLifecycle(t, db, firstPlan.ID, model.AgentArtifactRevisionStale)
	assertArtifactRevisionLifecycle(t, db, currentPlan.ID, model.AgentArtifactRevisionAwaitingReview)

	candidates, err := repo.MediaCandidateRevisionsInScope(scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 3 {
		t.Fatalf("immutable media candidates = %d, want 3", len(candidates))
	}
}

func TestArtifactRevisionHeadLockOrderIsDeterministicAcrossAssemblyReorder(t *testing.T) {
	first := []agentruntime.ArtifactRevisionRef{
		{ArtifactID: "video-b", RevisionID: "revision-b"},
		{ArtifactID: "video-a", RevisionID: "revision-a"},
	}
	second := []agentruntime.ArtifactRevisionRef{
		{ArtifactID: "video-a", RevisionID: "revision-a"},
		{ArtifactID: "video-b", RevisionID: "revision-b"},
	}

	firstOrder := artifactRevisionRefsForHeadValidation(first)
	secondOrder := artifactRevisionRefsForHeadValidation(second)
	for index, want := range secondOrder {
		if firstOrder[index] != want {
			t.Fatalf("lock order differs at %d: got %#v want %#v", index, firstOrder[index], want)
		}
	}
	if first[0].ArtifactID != "video-b" || second[0].ArtifactID != "video-a" {
		t.Fatal("head validation lock ordering mutated semantic reference order")
	}
}

func assemblyPlanDraftFixture(
	artifactKey string,
	first agentruntime.ArtifactRevisionRef,
	second agentruntime.ArtifactRevisionRef,
) agentruntime.ArtifactDraft {
	payload, err := json.Marshal(agentruntime.AssemblyPlanV2{
		PlanKey:   artifactKey,
		AudioMode: agentruntime.MediaAudioNone,
		Clips: []agentruntime.AssemblyClipV2{
			{
				ClipKey: "clip-one", SourceRevision: first,
				TrimStartMS: testInt64Pointer(0), TrimEndMS: testInt64Pointer(1000),
				TransitionToNext: agentruntime.AssemblyTransitionV2{Kind: agentruntime.AssemblyTransitionCut, DurationMS: testInt64Pointer(0)},
			},
			{
				ClipKey: "clip-two", SourceRevision: second,
				TrimStartMS: testInt64Pointer(0), TrimEndMS: testInt64Pointer(1000),
				TransitionToNext: agentruntime.AssemblyTransitionV2{Kind: agentruntime.AssemblyTransitionCut, DurationMS: testInt64Pointer(0)},
			},
		},
		AudioTracks: []agentruntime.AssemblyAudioTrackV2{},
		Output: agentruntime.AssemblyOutputV2{
			ArtifactKey: "assembled-video", Container: "mp4", VideoCodec: "h264", AudioCodec: "none",
			Width: testIntPointer(1280), Height: testIntPointer(720), FrameRate: testIntPointer(24),
		},
	})
	if err != nil {
		panic(err)
	}
	return agentruntime.ArtifactDraft{
		ArtifactKey: artifactKey, Kind: "assembly_plan", SchemaVersion: 2,
		Payload: payload, UpstreamRevisions: []agentruntime.ArtifactRevisionRef{first, second},
		SkillVersions: []agentruntime.SkillSelection{},
	}
}

func testInt64Pointer(value int64) *int64 {
	return &value
}

func testIntPointer(value int) *int {
	return &value
}

func appendLocalStaleArtifact(
	t *testing.T,
	repo *Repository,
	scope agentruntime.Scope,
	artifactID string,
	kind string,
	upstream []agentruntime.ArtifactRevisionRef,
) *model.AgentArtifactRevision {
	t.Helper()
	if upstream == nil {
		upstream = []agentruntime.ArtifactRevisionRef{}
	}
	revision, err := repo.AppendArtifactRevision(scope, artifactID, 0, agentruntime.ArtifactDraft{
		ArtifactKey: artifactID, Kind: kind, SchemaVersion: 1,
		Payload: json.RawMessage(`{"state":"current"}`), UpstreamRevisions: upstream,
	})
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

func appendLocalStaleCandidate(
	t *testing.T,
	repo *Repository,
	scope agentruntime.Scope,
	artifactID string,
	mediaKind agentruntime.ArtifactKind,
	upstream *model.AgentArtifactRevision,
) *model.AgentArtifactRevision {
	t.Helper()
	resourceID := "resource-" + artifactID
	requestID := "request-" + artifactID
	draft := agentruntime.ArtifactDraft{
		ArtifactKey: artifactID, Kind: mediaCandidateArtifactKind, SchemaVersion: 1,
		Payload:    json.RawMessage(`{"candidateKey":"` + artifactID + `","mediaKind":"` + string(mediaKind) + `","resourceId":"` + resourceID + `","sourceTaskId":"task-` + artifactID + `","providerRequestIdentity":"` + requestID + `"}`),
		ResourceID: resourceID, UpstreamRevisions: []agentruntime.ArtifactRevisionRef{artifactRevisionRef(upstream)},
		ModelRequestIdentity: requestID, SkillVersions: []agentruntime.SkillSelection{},
	}
	revision, err := repo.AppendMediaCandidateRevision(scope, artifactID, draft)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

func artifactRevisionRef(revision *model.AgentArtifactRevision) agentruntime.ArtifactRevisionRef {
	return agentruntime.ArtifactRevisionRef{ArtifactID: revision.ArtifactID, RevisionID: revision.ID}
}

func assertArtifactRevisionLifecycle(t *testing.T, db *gorm.DB, revisionID string, want string) {
	t.Helper()
	var stored model.AgentArtifactRevision
	if err := db.Select("id", "lifecycle_status").Where("id = ?", revisionID).Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.LifecycleStatus != want {
		t.Fatalf("revision %s lifecycle = %q, want %q", revisionID, stored.LifecycleStatus, want)
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
