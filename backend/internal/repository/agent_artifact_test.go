package repository

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

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

func TestMediaAttemptFenceRetainsLateResultWithoutMovingArtifactHead(t *testing.T) {
	repo, db := productionRepositoryFixture(t)
	scope := productionScopeFixture()
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC().Truncate(time.Second)
	fence := MediaAttemptCompletionFence{
		ToolCallID: "media-call-1", ActionVersion: 1, ExpectedTaskID: "media-task-1",
		ExpectedAttempt: 1, ExpectedArtifactRevisionID: "media-output-1", ApprovalFingerprint: "approval-1",
	}
	input := `{"outputArtifactId":"media-output-1","commercial":{"artifactRevisionId":"media-output-1","attempt":1,"taskId":"media-task-1","approvalFingerprint":"approval-1"}}`
	state := startRepositoryMediaAttempt(t, repo, scope, fence.ToolCallID, json.RawMessage(input), now)

	mismatchedDraft := mediaCandidateDraftFixture("candidate-mismatched-task", "resource-mismatched-task", "different-task", "request-mismatched-task")
	if _, err := repo.AppendMediaCandidateRevisionForAttempt(scope, "candidate-mismatched-task", mismatchedDraft, fence, now.Add(4*time.Second)); !errors.Is(err, ErrMediaCandidateInvalid) {
		t.Fatalf("mismatched candidate source task error = %v", err)
	}
	var mismatchedCount int64
	if err := db.Model(&model.AgentArtifactRevision{}).Where("artifact_id = ?", "candidate-mismatched-task").Count(&mismatchedCount).Error; err != nil {
		t.Fatal(err)
	}
	if mismatchedCount != 0 {
		t.Fatalf("mismatched candidate wrote %d revisions", mismatchedCount)
	}

	draft := mediaCandidateDraftFixture("candidate-current", "resource-current", fence.ExpectedTaskID, "request-current")
	current, err := repo.AppendMediaCandidateRevisionForAttempt(scope, "candidate-current", draft, fence, now.Add(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if current.Disposition != MediaAttemptWriteAdopted || current.Revision.LifecycleStatus != model.AgentArtifactRevisionAwaitingReview {
		t.Fatalf("current media result = %#v", current)
	}

	if _, err := repo.CancelAgentRunTree(scope, state.StateVersion, now.Add(6*time.Second)); err != nil {
		t.Fatal(err)
	}
	lateDraft := mediaCandidateDraftFixture("candidate-late", "resource-late", fence.ExpectedTaskID, "request-late")
	late, err := repo.AppendMediaCandidateRevisionForAttempt(scope, "candidate-late", lateDraft, fence, now.Add(7*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if late.Disposition != MediaAttemptWriteUnadopted || late.Revision.LifecycleStatus != model.AgentArtifactRevisionUnadopted {
		t.Fatalf("late media result = %#v", late)
	}
	var lateRoot model.AgentArtifact
	if err := db.First(&lateRoot, "id = ?", "candidate-late").Error; err != nil {
		t.Fatal(err)
	}
	if lateRoot.HeadRevision != 0 || lateRoot.LifecycleStatus != model.AgentArtifactLifecycleUnadopted {
		t.Fatalf("late artifact root = %#v", lateRoot)
	}
	replayed, err := repo.AppendMediaCandidateRevisionForAttempt(scope, "candidate-late", lateDraft, fence, now.Add(8*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Disposition != MediaAttemptWriteUnadopted || replayed.Revision.ID != late.Revision.ID {
		t.Fatalf("late duplicate replay = %#v, want revision %s", replayed, late.Revision.ID)
	}
	var lateRevisionCount int64
	if err := db.Model(&model.AgentArtifactRevision{}).Where("artifact_id = ?", "candidate-late").Count(&lateRevisionCount).Error; err != nil {
		t.Fatal(err)
	}
	if lateRevisionCount != 1 {
		t.Fatalf("late duplicate revisions = %d, want 1", lateRevisionCount)
	}
}

func TestMediaAttemptFenceRetainsReplacedAttemptResultAsUnadopted(t *testing.T) {
	repo, db := productionRepositoryFixture(t)
	scope := productionScopeFixture()
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC().Truncate(time.Second)
	oldFence := MediaAttemptCompletionFence{
		ToolCallID: "media-call-old", ActionVersion: 1, ExpectedTaskID: "media-task-old",
		ExpectedAttempt: 1, ExpectedArtifactRevisionID: "media-output-old", ApprovalFingerprint: "approval-old",
	}
	oldInput := `{"outputArtifactId":"media-output-old","commercial":{"artifactRevisionId":"media-output-old","attempt":1,"taskId":"media-task-old","approvalFingerprint":"approval-old"}}`
	oldState := startRepositoryMediaAttempt(t, repo, scope, oldFence.ToolCallID, json.RawMessage(oldInput), now)

	resolvedOld, err := agentruntime.ResolveTool(oldState, agentruntime.ToolResolution{
		ToolCallID: oldFence.ToolCallID, ActionVersion: oldFence.ActionVersion, Succeeded: false,
		Output:    json.RawMessage(`{"reason":"provider interrupted before callback"}`),
		ErrorCode: "provider_interrupted", FailureClass: agentruntime.ToolFailureAgentRepairable,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, oldState, resolvedOld, now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}

	newFence := MediaAttemptCompletionFence{
		ToolCallID: "media-call-new", ActionVersion: 1, ExpectedTaskID: "media-task-new",
		ExpectedAttempt: 2, ExpectedArtifactRevisionID: "media-output-new", ApprovalFingerprint: "approval-new",
	}
	newInput := `{"outputArtifactId":"media-output-new","commercial":{"artifactRevisionId":"media-output-new","attempt":2,"taskId":"media-task-new","approvalFingerprint":"approval-new"}}`
	requestedNew, err := agentruntime.Advance(resolvedOld.State, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall,
		ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: newFence.ToolCallID, ToolName: agentruntime.ToolMediaGenerate, ActionVersion: newFence.ActionVersion,
			Arguments: json.RawMessage(newInput), ExpectedDelivery: repositoryTestImageDelivery(),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, resolvedOld.State, requestedNew, now.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	approvedNew, err := agentruntime.ReviewToolApproval(requestedNew.State, agentruntime.ToolApproval{
		ToolCallID: newFence.ToolCallID, ActionVersion: newFence.ActionVersion, Decision: agentruntime.ToolApprovalApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, requestedNew.State, approvedNew, now.Add(6*time.Second)); err != nil {
		t.Fatal(err)
	}
	startedNew, err := agentruntime.BeginToolExecution(approvedNew.State, agentruntime.ToolExecution{
		ToolCallID: newFence.ToolCallID, ActionVersion: newFence.ActionVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, approvedNew.State, startedNew, now.Add(7*time.Second)); err != nil {
		t.Fatal(err)
	}

	currentDraft := mediaCandidateDraftFixture("candidate-current-attempt", "resource-current-attempt", newFence.ExpectedTaskID, "request-current-attempt")
	current, err := repo.AppendMediaCandidateRevisionForAttempt(scope, "candidate-current-attempt", currentDraft, newFence, now.Add(8*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if current.Disposition != MediaAttemptWriteAdopted || current.Revision.LifecycleStatus != model.AgentArtifactRevisionAwaitingReview {
		t.Fatalf("replacement attempt result = %#v", current)
	}

	lateDraft := mediaCandidateDraftFixture("candidate-replaced-attempt", "resource-replaced-attempt", oldFence.ExpectedTaskID, "request-replaced-attempt")
	late, err := repo.AppendMediaCandidateRevisionForAttempt(scope, "candidate-replaced-attempt", lateDraft, oldFence, now.Add(9*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if late.Disposition != MediaAttemptWriteUnadopted || late.Revision.LifecycleStatus != model.AgentArtifactRevisionUnadopted {
		t.Fatalf("replaced attempt result = %#v", late)
	}
	latest, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if latest.PendingToolCall == nil || latest.PendingToolCall.ToolCallID != newFence.ToolCallID || !latest.PendingToolStarted {
		t.Fatalf("current execution identity changed by stale callback: %#v", latest)
	}
	var lateRoot model.AgentArtifact
	if err := db.First(&lateRoot, "id = ?", "candidate-replaced-attempt").Error; err != nil {
		t.Fatal(err)
	}
	if lateRoot.HeadRevision != 0 || lateRoot.LifecycleStatus != model.AgentArtifactLifecycleUnadopted {
		t.Fatalf("replaced attempt artifact root = %#v", lateRoot)
	}
}

func startRepositoryMediaAttempt(
	t *testing.T,
	repo *Repository,
	scope agentruntime.Scope,
	toolCallID string,
	arguments json.RawMessage,
	now time.Time,
) agentruntime.RuntimeState {
	t.Helper()
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-1", ModelKey: "deepseek-v4-pro", MaxSteps: 8,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion:    agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion,
		UserMessage: "生成图片", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	current, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	requested, err := agentruntime.Advance(current, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall,
		ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: toolCallID, ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
			Arguments: arguments, ExpectedDelivery: repositoryTestImageDelivery(),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, current, requested, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	approved, err := agentruntime.ReviewToolApproval(requested.State, agentruntime.ToolApproval{
		ToolCallID: toolCallID, ActionVersion: 1, Decision: agentruntime.ToolApprovalApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, requested.State, approved, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	started, err := agentruntime.BeginToolExecution(approved.State, agentruntime.ToolExecution{ToolCallID: toolCallID, ActionVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, approved.State, started, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	return started.State
}

func mediaCandidateDraftFixture(artifactKey string, resourceID string, taskID string, requestID string) agentruntime.ArtifactDraft {
	return agentruntime.ArtifactDraft{
		ArtifactKey: artifactKey, Kind: "media_candidate", SchemaVersion: 1,
		Payload:    json.RawMessage(`{"candidateKey":"` + artifactKey + `","mediaKind":"image","resourceId":"` + resourceID + `","sourceTaskId":"` + taskID + `","providerRequestIdentity":"` + requestID + `"}`),
		ResourceID: resourceID, UpstreamRevisions: []agentruntime.ArtifactRevisionRef{},
		ModelRequestIdentity: requestID, SkillVersions: []agentruntime.SkillSelection{},
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
