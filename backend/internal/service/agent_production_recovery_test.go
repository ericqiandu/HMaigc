package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

func TestRejectCostApprovalCancelsMainAndSpecialists(t *testing.T) {
	fixture := newAgentProductionRecoveryFixture(t)
	queued, err := fixture.service.repo.CreateAgentSpecialistRun(repository.CreateAgentSpecialistRunInput{
		Scope: fixture.scope, Request: fixture.request,
		ToolSchemaVersion: agentruntime.ProductionToolSchemaVersion, Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	waiting := putProductionRunInCostApproval(t, fixture)
	rejectedAt := time.Now().UTC()
	progress, err := fixture.service.SubmitAgentToolApproval(fixture.scope, AgentToolApprovalSubmission{
		ToolCallID: waiting.PendingToolCall.ToolCallID, ActionVersion: waiting.PendingToolCall.ActionVersion,
		Decision: agentruntime.ToolApprovalRejected,
	})
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Status != agentruntime.RunCancelled {
		t.Fatalf("main run status = %q, want %q", progress.State.Status, agentruntime.RunCancelled)
	}
	storedSpecialist, err := fixture.service.repo.AgentSpecialistRunForScope(fixture.scope, queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedSpecialist.Status != model.AgentSpecialistRunCancelled {
		t.Fatalf("specialist status = %q, want %q", storedSpecialist.Status, model.AgentSpecialistRunCancelled)
	}
	var mediaTaskCount int64
	if err := fixture.db.Model(&model.Task{}).
		Where("created_at >= ? AND type IN ?", rejectedAt, []string{"image", "video", "audio"}).
		Count(&mediaTaskCount).Error; err != nil {
		t.Fatal(err)
	}
	if mediaTaskCount != 0 {
		t.Fatalf("media task count after rejection = %d, want 0", mediaTaskCount)
	}
}

func TestStopSpecialistStageReviewCancelsWaitingDelegateTree(t *testing.T) {
	fixture := newAgentProductionRecoveryFixture(t)
	queuedSpecialist, err := fixture.service.repo.CreateAgentSpecialistRun(repository.CreateAgentSpecialistRunInput{
		Scope: fixture.scope, Request: fixture.request,
		ToolSchemaVersion: agentruntime.ProductionToolSchemaVersion, Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := fixture.service.repo.ProductionRuntimeSnapshotForScope(fixture.scope)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Graph == nil || len(snapshot.Stages) != 1 {
		t.Fatalf("production snapshot = %#v", snapshot)
	}
	checkpoint, err := fixture.service.repo.LoadAgentCheckpoint(fixture.scope)
	if err != nil {
		t.Fatal(err)
	}
	selectedSkill := fixture.request.LoadedSkills[0]
	skillCall := agentruntime.ToolCallDecision{
		ToolCallID: "load-skill-before-stop", ToolName: agentruntime.ToolSkillLoad, ActionVersion: 1,
		Arguments: json.RawMessage(`{"dir":"` + selectedSkill.Dir + `"}`), ExpectedDelivery: fixture.request.ExpectedDelivery,
	}
	skillWaiting, err := agentruntime.AdvanceForToolSchema(checkpoint, agentruntime.RuntimeInput{
		Decision: agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &skillCall},
	}, agentruntime.ProductionToolSchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := fixture.service.repo.CommitAgentRuntimeTransition(fixture.scope, checkpoint, skillWaiting, now); err != nil {
		t.Fatal(err)
	}
	skillStarted, err := agentruntime.BeginToolExecution(skillWaiting.State, agentruntime.ToolExecution{
		ToolCallID: skillCall.ToolCallID, ActionVersion: skillCall.ActionVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.repo.CommitAgentRuntimeTransition(fixture.scope, skillWaiting.State, skillStarted, now.Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	skillOutput, err := json.Marshal(struct {
		Dir          string `json:"dir"`
		Name         string `json:"name"`
		Version      int    `json:"version"`
		Instructions string `json:"instructions"`
	}{
		Dir: selectedSkill.Dir, Name: selectedSkill.Name,
		Version: selectedSkill.Version, Instructions: selectedSkill.Instructions,
	})
	if err != nil {
		t.Fatal(err)
	}
	skillResolved, err := agentruntime.ResolveTool(skillStarted.State, agentruntime.ToolResolution{
		ToolCallID: skillCall.ToolCallID, ActionVersion: skillCall.ActionVersion, Succeeded: true, Output: skillOutput,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.repo.CommitAgentRuntimeTransition(fixture.scope, skillStarted.State, skillResolved, now.Add(2*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	stage := snapshot.Stages[0]
	graph := agentruntime.ProductionGraphDraft{
		GraphKey: snapshot.Graph.GraphKey,
		Stages: []agentruntime.ProductionStageDraft{{
			StageKey: stage.StageKey, SpecialistKey: stage.SpecialistKey,
			DependsOnStageKeys: []string{}, InputRevisions: fixture.request.InputRevisions,
			ExpectedDelivery: fixture.request.ExpectedDelivery,
			ReviewPolicy:     stage.ReviewPolicy, CostPolicy: stage.CostPolicy,
		}},
	}
	arguments, err := json.Marshal(SpecialistDelegateArguments{
		ProductionGraph: graph, ExpectedGraphVersion: 0, StageKey: stage.StageKey,
		SpecialistKey: fixture.request.SpecialistKey, Objective: fixture.request.Objective,
		InputRevisions: fixture.request.InputRevisions, SkillDirs: []string{selectedSkill.Dir},
		ToolAllowlist: []agentruntime.AgentToolName{}, ExpectedOutputSchema: fixture.request.ExpectedOutputSchema,
		ExpectedDelivery: fixture.request.ExpectedDelivery,
	})
	if err != nil {
		t.Fatal(err)
	}
	call := agentruntime.ToolCallDecision{
		ToolCallID: "delegate-before-stop", ToolName: agentruntime.ToolSpecialistDelegate, ActionVersion: 1,
		Arguments: arguments, ExpectedDelivery: fixture.request.ExpectedDelivery,
	}
	waiting, err := agentruntime.AdvanceForToolSchema(skillResolved.State, agentruntime.RuntimeInput{
		Decision: agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &call},
	}, agentruntime.ProductionToolSchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.repo.CommitAgentRuntimeTransition(fixture.scope, skillResolved.State, waiting, now.Add(3*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	approved, err := agentruntime.ReviewToolApproval(waiting.State, agentruntime.ToolApproval{
		ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion, Decision: agentruntime.ToolApprovalApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.repo.CommitAgentRuntimeTransition(fixture.scope, waiting.State, approved, now.Add(4*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	started, err := agentruntime.BeginToolExecution(approved.State, agentruntime.ToolExecution{
		ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.repo.CommitAgentRuntimeTransition(fixture.scope, approved.State, started, now.Add(5*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	reviewRevisionID := fixture.request.InputRevisions[0].RevisionID
	if err := fixture.db.Model(&model.AgentProductionStage{}).Where("id = ?", stage.ID).Updates(map[string]interface{}{
		"status": agentruntime.StageAwaitingReview, "version": int64(3),
		"review_revision_id": reviewRevisionID, "updated_at": now.Add(6 * time.Millisecond),
	}).Error; err != nil {
		t.Fatal(err)
	}

	result, err := fixture.service.ReviewProductionStage(
		context.Background(), fixture.scope, fixture.parentRun, stage.ID,
		agentruntime.StageReviewCommand{
			StageVersion: 3, RevisionID: reviewRevisionID, Decision: agentruntime.StageReviewStop,
			ClientRequestID: "stop-waiting-specialist-delegate",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stage.Status != agentruntime.StageStopped {
		t.Fatalf("stage status = %q, want %q", result.Stage.Status, agentruntime.StageStopped)
	}
	storedParent, err := fixture.service.repo.AgentRunForScope(fixture.scope)
	if err != nil {
		t.Fatal(err)
	}
	if storedParent.Status != agentruntime.RunCancelled {
		t.Fatalf("parent status = %q, want %q", storedParent.Status, agentruntime.RunCancelled)
	}
	storedSpecialist, err := fixture.service.repo.AgentSpecialistRunForScope(fixture.scope, queuedSpecialist.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedSpecialist.Status != model.AgentSpecialistRunCancelled {
		t.Fatalf("specialist status = %q, want %q", storedSpecialist.Status, model.AgentSpecialistRunCancelled)
	}
	var tool model.AgentToolCall
	if err := fixture.db.Where("run_id = ? AND tool_call_id = ?", fixture.scope.RunID, call.ToolCallID).First(&tool).Error; err != nil {
		t.Fatal(err)
	}
	if tool.Status != agentruntime.ToolCallFailed || tool.ErrorCode != "parent_run_cancelled" {
		t.Fatalf("delegate tool = %#v, want failed parent_run_cancelled", tool)
	}
	var mediaTaskCount int64
	if err := fixture.db.Model(&model.Task{}).Where("type IN ?", []string{"image", "video", "audio"}).Count(&mediaTaskCount).Error; err != nil {
		t.Fatal(err)
	}
	if mediaTaskCount != 0 {
		t.Fatalf("media task count = %d, want 0", mediaTaskCount)
	}
}

func TestLateProviderSuccessPersistsUnadoptedArtifact(t *testing.T) {
	fixture := newAgentProductionRecoveryFixture(t)
	run, err := fixture.service.repo.CreateAgentSpecialistRun(repository.CreateAgentSpecialistRunInput{
		Scope: fixture.scope, Request: fixture.request,
		ToolSchemaVersion: agentruntime.ProductionToolSchemaVersion, Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := fixture.service.ensureAgentSpecialistTask(fixture.scope, fixture.parentRun, fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	owner := "late-result-test-worker"
	run, _, err = fixture.service.repo.ClaimAgentSpecialistRun(
		fixture.scope, run.ID, task.ID, task.BillingOrderID, owner, time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	state, err := fixture.service.repo.LoadAgentCheckpoint(fixture.scope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.repo.CancelAgentRunTree(fixture.scope, state.StateVersion, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	draft := agentruntime.ArtifactDraft{
		ArtifactKey: "visual-evidence", Kind: "visual_evidence", SchemaVersion: 1,
		Payload:           visualEvidencePayloadFixture(t, fixture.request.InputRevisions[0], fixture.request.ParentModelRecordID, "provider-late-success", "迟到但有效的供应商结果"),
		ResourceID:        "resource-late-provider-success",
		UpstreamRevisions: fixture.request.InputRevisions, ModelRequestIdentity: "provider-late-success",
		SkillVersions: fixture.request.LoadedSkills,
	}
	completion := repository.CompleteAgentSpecialistRunInput{
		Scope: fixture.scope, SpecialistRunID: run.ID, LeaseOwner: owner, ProviderRequestID: "provider-late-success",
		ResultJSON: `{"summary":"迟到结果已入账本"}`, ResultSummary: "迟到结果已入账本", Drafts: []agentruntime.ArtifactDraft{draft},
		InputTokens: 30, CachedTokens: 5, OutputTokens: 12, Now: time.Now().UTC(),
	}
	completed, revisions, err := fixture.service.repo.CompleteAgentSpecialistRun(completion)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != model.AgentSpecialistRunCancelled {
		t.Fatalf("late specialist status = %q, want %q", completed.Status, model.AgentSpecialistRunCancelled)
	}
	if len(revisions) != 1 || revisions[0].LifecycleStatus != model.AgentArtifactRevisionUnadopted ||
		revisions[0].ResourceID != draft.ResourceID {
		t.Fatalf("late revisions = %#v", revisions)
	}
	var artifact model.AgentArtifact
	if err := fixture.db.First(&artifact, "id = ?", revisions[0].ArtifactID).Error; err != nil {
		t.Fatal(err)
	}
	if artifact.HeadRevision != 0 || artifact.LifecycleStatus != model.AgentArtifactLifecycleUnadopted {
		t.Fatalf("late artifact head = %#v", artifact)
	}
	replayed, replayedRevisions, err := fixture.service.repo.CompleteAgentSpecialistRun(completion)
	if err != nil {
		t.Fatalf("exact late callback replay failed: %v", err)
	}
	if replayed.ProviderRequestID != completed.ProviderRequestID || len(replayedRevisions) != 1 || replayedRevisions[0].ID != revisions[0].ID {
		t.Fatalf("late callback replay changed facts: run=%#v revisions=%#v", replayed, replayedRevisions)
	}
	conflicting := completion
	conflicting.Drafts = append([]agentruntime.ArtifactDraft(nil), completion.Drafts...)
	conflicting.Drafts[0].Payload = visualEvidencePayloadFixture(t, fixture.request.InputRevisions[0], fixture.request.ParentModelRecordID, "provider-late-success", "同一个请求返回了不同资产")
	if _, _, err := fixture.service.repo.CompleteAgentSpecialistRun(conflicting); !errors.Is(err, repository.ErrAgentSpecialistRunConflict) {
		t.Fatalf("conflicting late callback error = %v, want %v", err, repository.ErrAgentSpecialistRunConflict)
	}
	snapshot, err := fixture.service.repo.ProductionRuntimeSnapshotForScope(fixture.scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Artifacts) != 1 || snapshot.Artifacts[0].Artifact.ID != fixture.request.InputRevisions[0].ArtifactID ||
		snapshot.Artifacts[0].Revision.ID != fixture.request.InputRevisions[0].RevisionID ||
		snapshot.Graph == nil || snapshot.Graph.Version != 1 {
		t.Fatalf("active snapshot changed by late result: %#v", snapshot)
	}
}

func TestRecoverAgentRunTreeRejectsChangedFrozenSkillVersion(t *testing.T) {
	fixture := newAgentProductionRecoveryFixture(t)
	run, err := fixture.service.repo.CreateAgentSpecialistRun(repository.CreateAgentSpecialistRunInput{
		Scope: fixture.scope, Request: fixture.request,
		ToolSchemaVersion: agentruntime.ProductionToolSchemaVersion, Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	brokenSkills := append([]agentruntime.SkillSelection(nil), fixture.request.LoadedSkills...)
	brokenSkills[0].Checksum = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	encoded, err := json.Marshal(brokenSkills)
	if err != nil {
		t.Fatal(err)
	}
	update := struct {
		SkillVersionsJSON string `gorm:"column:skill_versions_json"`
	}{SkillVersionsJSON: string(encoded)}
	if err := fixture.db.Model(&model.AgentSpecialistRun{}).Where("id = ?", run.ID).
		Select("skill_versions_json").Updates(&update).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.RecoverAgentRunTree(fixture.scope); !errors.Is(err, ErrAgentRunRecoveryFactsInvalid) {
		t.Fatalf("RecoverAgentRunTree() error = %v, want %v", err, ErrAgentRunRecoveryFactsInvalid)
	}
}

func TestRecoverAgentRunTreeAcceptsExactPublishedSkillVersion(t *testing.T) {
	fixture := newAgentProductionRecoveryFixture(t)
	if _, err := fixture.service.repo.CreateAgentSpecialistRun(repository.CreateAgentSpecialistRunInput{
		Scope: fixture.scope, Request: fixture.request,
		ToolSchemaVersion: agentruntime.ProductionToolSchemaVersion, Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.RecoverAgentRunTree(fixture.scope); err != nil {
		t.Fatalf("RecoverAgentRunTree() error = %v", err)
	}
}

func TestRecoverAgentRunTreeProgressExposesStructuralStageAction(t *testing.T) {
	fixture := newAgentProductionRecoveryFixture(t)
	projection, err := fixture.service.RecoverAgentRunTreeProgress(fixture.scope)
	if err != nil {
		t.Fatal(err)
	}
	if projection == nil || projection.CurrentStageKey != "visual-analysis" ||
		len(projection.EligibleActions) != 1 || projection.EligibleActions[0].Action != agentruntime.ProductionActionExecuteStage {
		t.Fatalf("recovered progress = %#v", projection)
	}
	var stage model.AgentProductionStage
	if err := fixture.db.First(&stage, "id = ?", fixture.request.StageID).Error; err != nil {
		t.Fatal(err)
	}
	if stage.Status != agentruntime.StagePlanned || stage.Version != 1 {
		t.Fatalf("recovery mutated stage = %#v", stage)
	}
}

type agentProductionRecoveryFixture struct {
	service   *Service
	db        *gorm.DB
	scope     agentruntime.Scope
	request   agentruntime.SpecialistRequest
	parentRun model.AgentRun
	model     model.ChannelModel
}

func newAgentProductionRecoveryFixture(t *testing.T) agentProductionRecoveryFixture {
	t.Helper()
	request := specialistRuntimeRequestFixture("runtime-token-agent-model", "deepseek-v4-flash")
	service, db, base := newSpecialistRuntimeFixture(t, "http://127.0.0.1:1", request)
	seedPublishedRecoverySkill(t, db, request.LoadedSkills[0])
	scope := specialistRuntimeScope()
	_, err := service.repo.CreateInitializedAgentRun(repository.CreateInitializedAgentRunInput{
		Create: repository.CreateAgentRunInput{Scope: scope, ClientRequestID: "production-recovery-request", Now: time.Now().UTC()},
		Initialize: repository.InitializeAgentRunInput{
			Scope: scope, ModelRecordID: base.channelModel.ID, ModelKey: base.channelModel.ModelKey, MaxSteps: 24,
			ToolSchemaVersion: agentruntime.ProductionToolSchemaVersion, RuntimeVersion: agentruntime.ProductionRuntimeVersion,
			PolicyVersion: agentruntime.ProductionPolicyVersion, UserMessage: "创建一条需要逐阶段确认的短片生产流程",
			Configuration: agentruntime.RunConfiguration{
				Skills: request.LoadedSkills, Attachments: []agentruntime.ResourceAttachment{},
				ExecutionMode: agentruntime.ExecutionGuided,
			},
			Now: time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := service.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	begin, err := agentruntime.BeginModelRequest(queued)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.repo.CommitAgentRuntimeTransition(scope, queued, begin, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	graph, err := service.repo.AppendProductionGraphVersion(scope, 0, agentruntime.ProductionGraphDraft{
		GraphKey: "specialist-test-graph",
		Stages: []agentruntime.ProductionStageDraft{{
			StageKey: "visual-analysis", SpecialistKey: request.SpecialistKey,
			DependsOnStageKeys: []string{}, InputRevisions: request.InputRevisions,
			ExpectedDelivery: request.ExpectedDelivery, ReviewPolicy: agentruntime.ReviewRequired, CostPolicy: agentruntime.CostNone,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if graph.Stages[0].ID != request.StageID {
		t.Fatalf("stage id = %q, want %q", graph.Stages[0].ID, request.StageID)
	}
	parent, err := service.repo.AgentRunForScope(scope)
	if err != nil {
		t.Fatal(err)
	}
	return agentProductionRecoveryFixture{
		service: service, db: db, scope: scope, request: request, parentRun: *parent, model: base.channelModel,
	}
}

func seedPublishedRecoverySkill(t *testing.T, db *gorm.DB, selection agentruntime.SkillSelection) {
	t.Helper()
	publishedAt, err := time.Parse(time.RFC3339, selection.PublishedAt)
	if err != nil {
		t.Fatal(err)
	}
	manifestJSON, err := json.Marshal(selection.CapabilityManifest)
	if err != nil {
		t.Fatal(err)
	}
	skillID := "recovery-skill-" + selection.Dir
	versionID := skillID + "-v1"
	skill := model.Skill{
		ID: skillID, Dir: selection.Dir, Name: selection.Name, Description: selection.Description,
		Visibility: "public", Status: model.SkillStatusPublished, CurrentVersionID: versionID,
		SourceKind: selection.SourceKind, SourceURL: selection.SourceURL, SourceRevision: selection.SourceRevision,
		SourceLicense: selection.SourceLicense, CategoriesJSON: `[]`, CreatedAt: publishedAt, UpdatedAt: publishedAt,
	}
	version := model.SkillVersion{
		ID: versionID, SkillID: skillID, Version: selection.Version, Instructions: selection.Instructions,
		Checksum: selection.Checksum, CapabilityManifestJSON: string(manifestJSON), PublishedAt: &publishedAt,
		CreatedBy: "recovery-test", CreatedAt: publishedAt,
	}
	if err := db.Create(&skill).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&version).Error; err != nil {
		t.Fatal(err)
	}
}

func putProductionRunInCostApproval(t *testing.T, fixture agentProductionRecoveryFixture) agentruntime.RuntimeState {
	t.Helper()
	current, err := fixture.service.repo.LoadAgentCheckpoint(fixture.scope)
	if err != nil {
		t.Fatal(err)
	}
	expected := exactAgentMediaExpectedDelivery(agentruntime.ArtifactImage)
	arguments, err := json.Marshal(MediaGenerateArguments{
		InputRevisions:  []agentruntime.ArtifactRevisionRef{},
		GenerationModel: agentruntime.GenerationModelSelection{ChannelID: fixture.model.ChannelID, Model: fixture.model.ModelKey},
		Capability:      "image", Parameters: json.RawMessage(`{"size":"1024x1024"}`), OutputArtifactKey: "generated-image",
		ExpectedOutputSchema: "generated_image.v1", ExpectedDelivery: expected,
	})
	if err != nil {
		t.Fatal(err)
	}
	transition, err := agentruntime.AdvanceForToolSchema(current, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall,
		ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "cost-media-generation", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
			Arguments: arguments, ExpectedDelivery: expected,
		},
	}}, agentruntime.ProductionToolSchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.repo.CommitAgentRuntimeTransition(fixture.scope, current, transition, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	return transition.State
}

func TestAgentProductionRecoveryFixtureStartsRunning(t *testing.T) {
	fixture := newAgentProductionRecoveryFixture(t)
	state, err := fixture.service.repo.LoadAgentCheckpoint(fixture.scope)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != agentruntime.RunRunning || fixture.parentRun.Status != agentruntime.RunRunning {
		t.Fatalf("fixture state = %q, run = %q", state.Status, fixture.parentRun.Status)
	}
	if err := fixture.scope.Validate(); err != nil {
		t.Fatal(err)
	}
}
