package repository

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

func TestAppendAgentProductionPlanDerivesArtifactsFromDeliverables(t *testing.T) {
	tests := []struct {
		name                string
		deliverables        []agentruntime.ProductionShotDeliverable
		imagePrompt         string
		videoPrompt         string
		wantArtifactKinds   []model.AgentProductionArtifactKind
		wantStoryboardCount int
		wantVideoCount      int
	}{
		{
			name: "video only", deliverables: []agentruntime.ProductionShotDeliverable{agentruntime.ProductionShotDeliverableVideoClip},
			videoPrompt:       "原创抽象光影，镜头缓慢推进",
			wantArtifactKinds: []model.AgentProductionArtifactKind{model.AgentProductionArtifactScript, model.AgentProductionArtifactVideoClip},
			wantVideoCount:    1,
		},
		{
			name: "storyboard only", deliverables: []agentruntime.ProductionShotDeliverable{agentruntime.ProductionShotDeliverableStoryboardImage},
			imagePrompt:         "原创抽象光影分镜图",
			wantArtifactKinds:   []model.AgentProductionArtifactKind{model.AgentProductionArtifactScript, model.AgentProductionArtifactStoryboardImage},
			wantStoryboardCount: 1,
		},
		{
			name: "storyboard and video",
			deliverables: []agentruntime.ProductionShotDeliverable{
				agentruntime.ProductionShotDeliverableStoryboardImage,
				agentruntime.ProductionShotDeliverableVideoClip,
			},
			imagePrompt: "原创抽象光影分镜图", videoPrompt: "原创抽象光影，镜头缓慢推进",
			wantArtifactKinds: []model.AgentProductionArtifactKind{
				model.AgentProductionArtifactScript,
				model.AgentProductionArtifactStoryboardImage,
				model.AgentProductionArtifactVideoClip,
			},
			wantStoryboardCount: 1, wantVideoCount: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo, _ := openAgentRuntimeRepositorySQLite(t)
			scope := repositoryAgentScope()
			createAgentRunForTest(t, repo, scope)
			draft := agentruntime.ProductionPlanDraft{
				Title: "5秒原创抽象光影", TargetDurationMS: 5_000, Script: "抽象光带汇聚并消散。",
				Shots: []agentruntime.ShotPlanDraft{{
					ShotKey: "shot-1", Order: 1, DurationMS: 5_000, ScriptText: "光带聚合",
					Deliverables: test.deliverables, ImagePrompt: test.imagePrompt, VideoPrompt: test.videoPrompt,
					Dependencies: []string{},
				}},
			}
			record, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
				Scope: scope, RunID: scope.RunID, PlanKey: "deliverable-plan", BaseVersion: 0,
				Draft: draft, Now: time.Now().UTC(),
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(record.Artifacts) != len(test.wantArtifactKinds) {
				t.Fatalf("artifact count = %d, want %d: %#v", len(record.Artifacts), len(test.wantArtifactKinds), record.Artifacts)
			}
			for index, wantKind := range test.wantArtifactKinds {
				if record.Artifacts[index].Kind != wantKind {
					t.Fatalf("artifact %d kind = %s, want %s", index, record.Artifacts[index].Kind, wantKind)
				}
			}
			var delivery struct {
				Scripts          int `json:"scripts"`
				ReferenceImages  int `json:"referenceImages"`
				StoryboardImages int `json:"storyboardImages"`
				VideoClips       int `json:"videoClips"`
			}
			if err := json.Unmarshal([]byte(record.Plan.ExpectedDeliveryJSON), &delivery); err != nil {
				t.Fatal(err)
			}
			if delivery.Scripts != 1 || delivery.ReferenceImages != 0 ||
				delivery.StoryboardImages != test.wantStoryboardCount || delivery.VideoClips != test.wantVideoCount {
				t.Fatalf("expected delivery = %#v", delivery)
			}
		})
	}
}

func TestAppendAgentProductionPlanCreatesImmutableVersionAndLedger(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC().Truncate(time.Second)

	first, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "orange-ad", BaseVersion: 0,
		Draft: twoShotProductionPlanDraft("第一版剧本"), Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Plan.Version != 1 || first.Plan.Status != model.AgentProductionPlanActive || first.Plan.Script != "第一版剧本" {
		t.Fatalf("first plan = %#v", first.Plan)
	}
	if len(first.Artifacts) != 5 {
		t.Fatalf("first artifact count = %d, want 5", len(first.Artifacts))
	}
	assertProductionArtifactShape(t, first.Artifacts)

	second, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "orange-ad", BaseVersion: 1,
		Draft: twoShotProductionPlanDraft("第二版剧本"), Now: now.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Plan.Version != 2 || second.Plan.Script != "第二版剧本" {
		t.Fatalf("second plan = %#v", second.Plan)
	}
	var storedFirst model.AgentProductionPlanVersion
	if err := db.First(&storedFirst, "id = ?", first.Plan.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedFirst.Status != model.AgentProductionPlanSuperseded || storedFirst.Script != "第一版剧本" || storedFirst.ShotsJSON != first.Plan.ShotsJSON {
		t.Fatalf("first immutable plan changed = %#v", storedFirst)
	}

	_, err = repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "orange-ad", BaseVersion: 1,
		Draft: twoShotProductionPlanDraft("冲突版本"), Now: now.Add(2 * time.Second),
	})
	if !errors.Is(err, ErrAgentProductionPlanVersionConflict) {
		t.Fatalf("stale base version error = %v", err)
	}
	var planCount int64
	var artifactCount int64
	if err := db.Model(&model.AgentProductionPlanVersion{}).Where("plan_key = ?", "orange-ad").Count(&planCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentProductionArtifact{}).Where("plan_key = ?", "orange-ad").Count(&artifactCount).Error; err != nil {
		t.Fatal(err)
	}
	if planCount != 2 || artifactCount != 10 {
		t.Fatalf("conflict wrote facts: plans=%d artifacts=%d", planCount, artifactCount)
	}
}

func TestAgentProductionArtifactTransitionUsesStatusAndAttemptCAS(t *testing.T) {
	repo, _ := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC().Truncate(time.Second)
	created, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "artifact-cas", BaseVersion: 0,
		Draft: twoShotProductionPlanDraft("CAS 剧本"), Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact := firstProductionArtifact(t, created.Artifacts, "shot-1", model.AgentProductionArtifactStoryboardImage)

	queued, err := repo.TransitionAgentProductionArtifact(scope, ArtifactTransition{
		ArtifactID: artifact.ID, ExpectedStatus: model.AgentProductionArtifactPlanned,
		NextStatus: model.AgentProductionArtifactQueued, ExpectedAttempt: 0, NextAttempt: 1,
		TaskID: "task-image-1", Now: now.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if queued.Status != model.AgentProductionArtifactQueued || queued.Attempt != 1 || queued.TaskID != "task-image-1" {
		t.Fatalf("queued artifact = %#v", queued)
	}

	_, err = repo.TransitionAgentProductionArtifact(scope, ArtifactTransition{
		ArtifactID: artifact.ID, ExpectedStatus: model.AgentProductionArtifactPlanned,
		NextStatus: model.AgentProductionArtifactQueued, ExpectedAttempt: 0, NextAttempt: 1,
		TaskID: "task-stale", Now: now.Add(2 * time.Second),
	})
	if !errors.Is(err, ErrAgentProductionArtifactConflict) {
		t.Fatalf("stale artifact transition error = %v", err)
	}

	succeeded, err := repo.TransitionAgentProductionArtifact(scope, ArtifactTransition{
		ArtifactID: artifact.ID, ExpectedStatus: model.AgentProductionArtifactQueued,
		NextStatus: model.AgentProductionArtifactSucceeded, ExpectedAttempt: 1, NextAttempt: 1,
		TaskID: "task-image-1", BillingOrderID: "billing-image-1", ResourceID: "resource-image-1",
		Now: now.Add(3 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if succeeded.Status != model.AgentProductionArtifactSucceeded || succeeded.ResourceID != "resource-image-1" || succeeded.BillingOrderID != "billing-image-1" {
		t.Fatalf("succeeded artifact = %#v", succeeded)
	}
}

func TestLateResultAfterRunCancelledIsUnadoptedAndDoesNotOverwriteProductionArtifact(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-1", ModelKey: "gpt-5.5", MaxSteps: 8,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion:    agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion,
		UserMessage:   "生成分镜图",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	created, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "late-artifact", BaseVersion: 0,
		Draft: twoShotProductionPlanDraft("迟到资产剧本"), Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact := firstProductionArtifact(t, created.Artifacts, "shot-1", model.AgentProductionArtifactStoryboardImage)
	rawArguments, err := json.Marshal(struct {
		OutputArtifactID string `json:"outputArtifactId"`
		Commercial       struct {
			ArtifactRevisionID  string `json:"artifactRevisionId"`
			Attempt             int    `json:"attempt"`
			TaskID              string `json:"taskId"`
			ApprovalFingerprint string `json:"approvalFingerprint"`
		} `json:"commercial"`
	}{
		OutputArtifactID: artifact.ID,
		Commercial: struct {
			ArtifactRevisionID  string `json:"artifactRevisionId"`
			Attempt             int    `json:"attempt"`
			TaskID              string `json:"taskId"`
			ApprovalFingerprint string `json:"approvalFingerprint"`
		}{ArtifactRevisionID: artifact.ID, Attempt: 1, TaskID: "task-late-image", ApprovalFingerprint: "approval-late-image"},
	})
	if err != nil {
		t.Fatal(err)
	}
	current, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	requested, err := agentruntime.Advance(current, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall,
		ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "production-render-1", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
			Arguments:        json.RawMessage(`{"mediaKind":"image","modelRecordId":"model-image-1","modelKey":"gpt-image-2","parameters":{"prompt":"late callback"},"sourceResourceIds":[],"targetCanvasNodeId":"image-node-late","clientRequestId":"production-render-1"}`),
			ExpectedDelivery: repositoryTestImageDelivery(),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	requested.ApprovalCostQuote = &agentruntime.ApprovalCostQuote{
		ModelRecordID: "model-image-1", ModelKey: "gpt-image-2", PriceVersion: 1, AmountMicrocredits: 1_000,
	}
	if err := repo.CommitAgentRuntimeTransition(scope, current, requested, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	record, err := repo.AgentToolCallForScope(scope, "production-render-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := agentruntime.ReviewToolApproval(requested.State, agentruntime.ToolApproval{
		ToolCallID: "production-render-1", ActionVersion: 1, Decision: agentruntime.ToolApprovalApproved,
		ProposalHash: record.ApprovalProposalHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, requested.State, approved, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	started, err := agentruntime.BeginToolExecution(approved.State, agentruntime.ToolExecution{ToolCallID: "production-render-1", ActionVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, approved.State, started, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	updateResult := db.Model(&model.AgentToolCall{}).
		Where("id = ? AND status = ?", record.ID, agentruntime.ToolCallRunning).
		Update("input_json", string(rawArguments))
	if updateResult.Error != nil {
		t.Fatal(updateResult.Error)
	}
	if updateResult.RowsAffected != 1 {
		t.Fatal("production media attempt facts were not frozen")
	}
	fence := MediaAttemptCompletionFence{
		ToolCallID: "production-render-1", ActionVersion: 1, ExpectedTaskID: "task-late-image",
		ExpectedAttempt: 1, ExpectedArtifactRevisionID: artifact.ID, ApprovalFingerprint: "approval-late-image",
	}
	queued, err := repo.TransitionAgentProductionMediaAttempt(scope, fence, ArtifactTransition{
		ArtifactID: artifact.ID, ExpectedStatus: model.AgentProductionArtifactPlanned,
		NextStatus: model.AgentProductionArtifactQueued, ExpectedAttempt: 0, NextAttempt: 1,
		TaskID: "task-late-image", BillingOrderID: "billing-late-image", Now: now.Add(4 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if queued.Disposition != MediaAttemptWriteAdopted {
		t.Fatalf("queued production attempt disposition = %q", queued.Disposition)
	}
	runningTransition := ArtifactTransition{
		ArtifactID: artifact.ID, ExpectedStatus: model.AgentProductionArtifactQueued,
		NextStatus: model.AgentProductionArtifactRunning, ExpectedAttempt: 1, NextAttempt: 1,
		TaskID: "task-late-image", BillingOrderID: "billing-late-image", Now: now.Add(5 * time.Second),
	}
	running, err := repo.TransitionAgentProductionMediaAttempt(scope, fence, runningTransition)
	if err != nil {
		t.Fatal(err)
	}
	if running.Disposition != MediaAttemptWriteAdopted || running.Artifact.Status != model.AgentProductionArtifactRunning {
		t.Fatalf("running production attempt = %#v", running)
	}
	if _, err := repo.CancelAgentRunTree(scope, started.State.StateVersion, now.Add(6*time.Second)); err != nil {
		t.Fatal(err)
	}
	var eventCountBefore, itemCountBefore int64
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ?", scope.RunID).Count(&eventCountBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentTimelineItem{}).Where("run_id = ?", scope.RunID).Count(&itemCountBefore).Error; err != nil {
		t.Fatal(err)
	}
	lateProgress, err := repo.TransitionAgentProductionMediaAttempt(scope, fence, runningTransition)
	if err != nil {
		t.Fatal(err)
	}
	if lateProgress.Disposition != MediaAttemptWriteUnadopted || lateProgress.Artifact.Status != model.AgentProductionArtifactRunning {
		t.Fatalf("late production progress = %#v", lateProgress)
	}
	completion := ProductionMediaAttemptCompletion{
		Fence:      fence,
		ArtifactID: artifact.ID, ExpectedStatus: model.AgentProductionArtifactRunning,
		BillingOrderID: "billing-late-image", ResourceID: "resource-late-image",
		LateArtifactID: "late-production-image",
		LateDraft:      mediaCandidateDraftFixture("late-production-image", "resource-late-image", "task-late-image", "task-late-image:01"),
		Now:            now.Add(8 * time.Second),
	}
	result, err := repo.CompleteAgentProductionMediaAttempt(scope, completion)
	if err != nil {
		t.Fatal(err)
	}
	if result.Disposition != MediaAttemptWriteUnadopted || result.Artifact.Status != model.AgentProductionArtifactRunning ||
		result.Artifact.ResourceID != "" || result.LateRevision == nil || result.LateRevision.LifecycleStatus != model.AgentArtifactRevisionUnadopted {
		t.Fatalf("late production completion = %#v", result)
	}
	var run model.AgentRun
	if err := db.First(&run, "id = ?", scope.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != agentruntime.RunCancelled || run.StateVersion != started.State.StateVersion+1 || run.LastEventSequence != eventCountBefore {
		t.Fatalf("late artifact changed terminal runtime facts = %#v", run)
	}
	var checkpoint model.AgentCheckpoint
	if err := db.Where("run_id = ?", scope.RunID).Order("sequence DESC").Take(&checkpoint).Error; err != nil {
		t.Fatal(err)
	}
	if checkpoint.Sequence != eventCountBefore {
		t.Fatalf("late artifact rewrote runtime checkpoint = %#v", checkpoint)
	}
	if _, err := repo.CompleteAgentProductionMediaAttempt(scope, completion); err != nil {
		t.Fatalf("identical late artifact callback replay = %v", err)
	}
	var eventCount, itemCount int64
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ?", scope.RunID).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentTimelineItem{}).Where("run_id = ?", scope.RunID).Count(&itemCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != eventCountBefore || itemCount != itemCountBefore {
		t.Fatalf("late artifact replay duplicated facts: events=%d items=%d", eventCount, itemCount)
	}
}

func TestProductionMediaCompletionKeepsTerminalResourceImmutable(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC().Truncate(time.Second)
	created, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "immutable-completion", BaseVersion: 0,
		Draft: twoShotProductionPlanDraft("不可改写的成功资产"), Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact := firstProductionArtifact(t, created.Artifacts, "shot-1", model.AgentProductionArtifactStoryboardImage)
	fence := MediaAttemptCompletionFence{
		ToolCallID: "immutable-production-render", ActionVersion: 1, ExpectedTaskID: "immutable-media-task",
		ExpectedAttempt: 1, ExpectedArtifactRevisionID: artifact.ID, ApprovalFingerprint: "immutable-approval",
	}
	arguments := json.RawMessage(`{"outputArtifactId":"` + artifact.ID + `","commercial":{"artifactRevisionId":"` + artifact.ID + `","attempt":1,"taskId":"immutable-media-task","approvalFingerprint":"immutable-approval"}}`)
	startRepositoryMediaAttempt(t, repo, scope, fence.ToolCallID, arguments, now)
	queued, err := repo.TransitionAgentProductionMediaAttempt(scope, fence, ArtifactTransition{
		ArtifactID: artifact.ID, ExpectedStatus: model.AgentProductionArtifactPlanned,
		NextStatus: model.AgentProductionArtifactQueued, ExpectedAttempt: 0, NextAttempt: 1,
		TaskID: fence.ExpectedTaskID, BillingOrderID: "immutable-billing-order", Now: now.Add(4 * time.Second),
	})
	if err != nil || queued.Disposition != MediaAttemptWriteAdopted {
		t.Fatalf("queue production attempt = %#v, err=%v", queued, err)
	}
	running, err := repo.TransitionAgentProductionMediaAttempt(scope, fence, ArtifactTransition{
		ArtifactID: artifact.ID, ExpectedStatus: model.AgentProductionArtifactQueued,
		NextStatus: model.AgentProductionArtifactRunning, ExpectedAttempt: 1, NextAttempt: 1,
		TaskID: fence.ExpectedTaskID, BillingOrderID: "immutable-billing-order", Now: now.Add(5 * time.Second),
	})
	if err != nil || running.Disposition != MediaAttemptWriteAdopted {
		t.Fatalf("run production attempt = %#v, err=%v", running, err)
	}

	completion := ProductionMediaAttemptCompletion{
		Fence: fence, ArtifactID: artifact.ID, ExpectedStatus: model.AgentProductionArtifactRunning,
		BillingOrderID: "immutable-billing-order", ResourceID: "resource-original",
		LateArtifactID: "immutable-late-original",
		LateDraft:      mediaCandidateDraftFixture("immutable-late-original", "resource-original", fence.ExpectedTaskID, "immutable-media-task:01"),
		Now:            now.Add(6 * time.Second),
	}
	completed, err := repo.CompleteAgentProductionMediaAttempt(scope, completion)
	if err != nil || completed.Disposition != MediaAttemptWriteAdopted || completed.Artifact.Status != model.AgentProductionArtifactSucceeded {
		t.Fatalf("complete production attempt = %#v, err=%v", completed, err)
	}

	conflict := completion
	conflict.ExpectedStatus = model.AgentProductionArtifactSucceeded
	conflict.ResourceID = "resource-conflicting-duplicate"
	conflict.LateArtifactID = "immutable-late-conflict"
	conflict.LateDraft = mediaCandidateDraftFixture(conflict.LateArtifactID, conflict.ResourceID, fence.ExpectedTaskID, "immutable-media-task:conflict")
	conflict.Now = now.Add(7 * time.Second)
	conflicting, err := repo.CompleteAgentProductionMediaAttempt(scope, conflict)
	if err != nil {
		t.Fatal(err)
	}
	if conflicting.Disposition != MediaAttemptWriteUnadopted || conflicting.LateRevision == nil ||
		conflicting.LateRevision.LifecycleStatus != model.AgentArtifactRevisionUnadopted {
		t.Fatalf("conflicting duplicate completion = %#v", conflicting)
	}
	var stored model.AgentProductionArtifact
	if err := db.First(&stored, "id = ?", artifact.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.AgentProductionArtifactSucceeded || stored.ResourceID != completion.ResourceID {
		t.Fatalf("conflicting duplicate rewrote successful artifact = %#v", stored)
	}

	committed, err := repo.CommitAgentProductionArtifactCanvasNode(scope, ArtifactCanvasCommit{
		ArtifactID: artifact.ID, ExpectedStatus: model.AgentProductionArtifactSucceeded,
		ExpectedAttempt: fence.ExpectedAttempt, CanvasNodeID: "immutable-canvas-node", Now: now.Add(8 * time.Second),
	})
	if err != nil || committed.Status != model.AgentProductionArtifactCommitted {
		t.Fatalf("commit production artifact = %#v, err=%v", committed, err)
	}
	replayed, err := repo.CompleteAgentProductionMediaAttempt(scope, completion)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Disposition != MediaAttemptWriteAdopted || replayed.LateRevision != nil ||
		replayed.Artifact.Status != model.AgentProductionArtifactCommitted || replayed.Artifact.ResourceID != completion.ResourceID {
		t.Fatalf("terminal completion replay = %#v", replayed)
	}
	var conflictRevisionCount int64
	if err := db.Model(&model.AgentArtifactRevision{}).Where("artifact_id = ?", conflict.LateArtifactID).Count(&conflictRevisionCount).Error; err != nil {
		t.Fatal(err)
	}
	if conflictRevisionCount != 1 {
		t.Fatalf("conflicting duplicate revision count = %d, want 1", conflictRevisionCount)
	}
}

func TestAgentProductionPlanReadIsScopeIsolated(t *testing.T) {
	repo, _ := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	created, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "isolated-plan", BaseVersion: 0,
		Draft: twoShotProductionPlanDraft("隔离剧本"), Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AgentProductionPlanVersionForScope(scope, created.Plan.PlanKey, created.Plan.Version); err != nil {
		t.Fatal(err)
	}
	other := scope
	other.TenantID = "other-user"
	other.ActorUserID = "other-user"
	if _, err := repo.AgentProductionPlanVersionForScope(other, created.Plan.PlanKey, created.Plan.Version); err == nil {
		t.Fatal("cross-tenant plan read succeeded")
	}
	sameTenantOtherActor := scope
	sameTenantOtherActor.ActorUserID = "other-user"
	if _, err := repo.AgentProductionPlanVersionForScope(sameTenantOtherActor, created.Plan.PlanKey, created.Plan.Version); err == nil {
		t.Fatal("same-tenant cross-actor plan read succeeded")
	}
	if _, err := repo.ActiveAgentProductionPlanForThread(sameTenantOtherActor); err == nil {
		t.Fatal("same-tenant cross-actor active plan read succeeded")
	}
	wrongProject := scope
	wrongProject.DomainProjectID = "another-project"
	if _, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: wrongProject, RunID: wrongProject.RunID, PlanKey: "wrong-project-plan", BaseVersion: 0,
		Draft: twoShotProductionPlanDraft("越权剧本"), Now: time.Now().UTC(),
	}); err == nil {
		t.Fatal("cross-project plan append succeeded")
	}
}

func TestAgentProductionPlanIdentityIsScopedAcrossTenants(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	firstScope := repositoryAgentScope()
	secondScope := repositoryAgentScope()
	secondScope.TenantID = "agent-user-2"
	secondScope.ActorUserID = "agent-user-2"
	secondScope.CanvasID = "agent-canvas-2"
	secondScope.ThreadID = "agent-thread-2"
	secondScope.RunID = "agent-run-2"
	createAgentRunForTest(t, repo, firstScope)
	createAgentRunForTest(t, repo, secondScope)

	first, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{Scope: firstScope, RunID: firstScope.RunID, PlanKey: "shared-plan-key", BaseVersion: 0, Draft: twoShotProductionPlanDraft("租户一"), Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{Scope: secondScope, RunID: secondScope.RunID, PlanKey: "shared-plan-key", BaseVersion: 0, Draft: twoShotProductionPlanDraft("租户二"), Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if first.Plan.ID == second.Plan.ID || first.Artifacts[0].ID == second.Artifacts[0].ID {
		t.Fatalf("cross-tenant production identities collided: first=%s second=%s", first.Plan.ID, second.Plan.ID)
	}
	var planCount int64
	if err := db.Model(&model.AgentProductionPlanVersion{}).Where("plan_key = ? AND version = 1", "shared-plan-key").Count(&planCount).Error; err != nil {
		t.Fatal(err)
	}
	if planCount != 2 {
		t.Fatalf("scoped plan count = %d, want 2", planCount)
	}
}

func TestActiveAgentProductionPlanForThreadFollowsThreadAndScope(t *testing.T) {
	repo, _ := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC().Truncate(time.Second)
	created, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "active-run-plan", BaseVersion: 0,
		Draft: twoShotProductionPlanDraft("活动计划"), Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	latest, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "latest-run-plan", BaseVersion: 0,
		Draft: twoShotProductionPlanDraft("最新活动计划"), Now: now.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	active, err := repo.ActiveAgentProductionPlanForThread(scope)
	if err != nil {
		t.Fatal(err)
	}
	if active == nil || active.Plan.ID != latest.Plan.ID || active.Plan.ID == created.Plan.ID || len(active.Artifacts) != len(latest.Artifacts) {
		t.Fatalf("active run plan = %#v", active)
	}

	otherRun := scope
	otherRun.RunID = "another-run"
	createAgentRunForTest(t, repo, otherRun)
	active, err = repo.ActiveAgentProductionPlanForThread(otherRun)
	if err != nil || active == nil || active.Plan.ID != latest.Plan.ID {
		t.Fatalf("other run active plan = %#v, err = %v", active, err)
	}

	otherThread := scope
	otherThread.RunID = "another-thread-run"
	otherThread.ThreadID = "another-thread"
	createAgentRunForTest(t, repo, otherThread)
	active, err = repo.ActiveAgentProductionPlanForThread(otherThread)
	if err != nil || active != nil {
		t.Fatalf("other thread active plan = %#v, err = %v", active, err)
	}

	otherTenant := scope
	otherTenant.TenantID = "another-user"
	otherTenant.ActorUserID = "another-user"
	active, err = repo.ActiveAgentProductionPlanForThread(otherTenant)
	if err == nil || active != nil {
		t.Fatalf("other tenant active plan = %#v, err = %v", active, err)
	}
}

func TestAppendAgentProductionPlanRejectsStructurallyInvalidDraft(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*agentruntime.ProductionPlanDraft)
	}{
		{name: "duplicate shot key", mutate: func(draft *agentruntime.ProductionPlanDraft) { draft.Shots[1].ShotKey = draft.Shots[0].ShotKey }},
		{name: "non-contiguous order", mutate: func(draft *agentruntime.ProductionPlanDraft) { draft.Shots[1].Order = 3 }},
		{name: "missing dependency", mutate: func(draft *agentruntime.ProductionPlanDraft) { draft.Shots[1].Dependencies = []string{"missing-shot"} }},
		{name: "future dependency", mutate: func(draft *agentruntime.ProductionPlanDraft) { draft.Shots[0].Dependencies = []string{"shot-2"} }},
		{name: "duration mismatch", mutate: func(draft *agentruntime.ProductionPlanDraft) { draft.TargetDurationMS = 9_000 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo, _ := openAgentRuntimeRepositorySQLite(t)
			scope := repositoryAgentScope()
			createAgentRunForTest(t, repo, scope)
			draft := twoShotProductionPlanDraft("无效剧本")
			test.mutate(&draft)
			if _, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
				Scope: scope, RunID: scope.RunID, PlanKey: "invalid-plan", BaseVersion: 0,
				Draft: draft, Now: time.Now().UTC(),
			}); err == nil {
				t.Fatal("structurally invalid production plan was accepted")
			}
		})
	}
}

func TestAppendAgentProductionPlanAllowsOnlyOneConcurrentNextVersion(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLiteFile(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	// SQLite has no row-level FOR UPDATE semantics. Keep this focused unit test
	// on one connection; the PostgreSQL gate below proves cross-connection CAS.
	sqlDB.SetMaxOpenConns(1)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC().Truncate(time.Second)
	if _, err = repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "concurrent-plan", BaseVersion: 0,
		Draft: twoShotProductionPlanDraft("基础版本"), Now: now,
	}); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, script := range []string{"并发版本 A", "并发版本 B"} {
		script := script
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
				Scope: scope, RunID: scope.RunID, PlanKey: "concurrent-plan", BaseVersion: 1,
				Draft: twoShotProductionPlanDraft(script), Now: now.Add(time.Second),
			})
			results <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	succeeded := 0
	conflicted := 0
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrAgentProductionPlanVersionConflict):
			conflicted++
		default:
			t.Fatalf("unexpected concurrent append error: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("concurrent append results: succeeded=%d conflicted=%d", succeeded, conflicted)
	}
	var planCount int64
	var artifactCount int64
	if err := db.Model(&model.AgentProductionPlanVersion{}).Where("plan_key = ?", "concurrent-plan").Count(&planCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentProductionArtifact{}).Where("plan_key = ?", "concurrent-plan").Count(&artifactCount).Error; err != nil {
		t.Fatal(err)
	}
	if planCount != 2 || artifactCount != 10 {
		t.Fatalf("concurrent append facts: plans=%d artifacts=%d", planCount, artifactCount)
	}
}

func TestAppendAgentProductionPlanReplaysIdenticalVersionWithoutNewFacts(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	input := AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "replay-plan", BaseVersion: 0,
		Draft: twoShotProductionPlanDraft("可重放剧本"), Now: time.Now().UTC().Truncate(time.Second),
	}
	first, err := repo.AppendAgentProductionPlanVersion(input)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repo.AppendAgentProductionPlanVersion(input)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Plan.ID != first.Plan.ID || len(replayed.Artifacts) != len(first.Artifacts) {
		t.Fatalf("replayed production plan = %#v", replayed)
	}
	for index := range first.Artifacts {
		if replayed.Artifacts[index].ID != first.Artifacts[index].ID {
			t.Fatalf("replayed artifact %d = %s, want %s", index, replayed.Artifacts[index].ID, first.Artifacts[index].ID)
		}
	}
	conflict := input
	conflict.Draft.Script = "不同剧本不得冒充重放"
	if _, err := repo.AppendAgentProductionPlanVersion(conflict); !errors.Is(err, ErrAgentProductionPlanVersionConflict) {
		t.Fatalf("different replay error = %v", err)
	}
	var plans int64
	var artifacts int64
	if err := db.Model(&model.AgentProductionPlanVersion{}).Where("plan_key = ?", input.PlanKey).Count(&plans).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentProductionArtifact{}).Where("plan_key = ?", input.PlanKey).Count(&artifacts).Error; err != nil {
		t.Fatal(err)
	}
	if plans != 1 || artifacts != 5 {
		t.Fatalf("replay duplicated facts: plans=%d artifacts=%d", plans, artifacts)
	}
}

func twoShotProductionPlanDraft(script string) agentruntime.ProductionPlanDraft {
	return agentruntime.ProductionPlanDraft{
		Title: "10 秒橙子广告", TargetDurationMS: 10_000, Script: script,
		Shots: []agentruntime.ShotPlanDraft{
			{
				ShotKey: "shot-1", Order: 1, DurationMS: 5_000, ScriptText: "鲜橙落水",
				Deliverables: dualProductionShotDeliverables(), ImagePrompt: "橙子产品特写", VideoPrompt: "慢镜头水花", Dependencies: []string{},
			},
			{
				ShotKey: "shot-2", Order: 2, DurationMS: 5_000, ScriptText: "果汁收尾",
				Deliverables: dualProductionShotDeliverables(), ImagePrompt: "果汁英雄镜头", VideoPrompt: "镜头推进", Dependencies: []string{"shot-1"},
			},
		},
	}
}

func dualProductionShotDeliverables() []agentruntime.ProductionShotDeliverable {
	return []agentruntime.ProductionShotDeliverable{
		agentruntime.ProductionShotDeliverableStoryboardImage,
		agentruntime.ProductionShotDeliverableVideoClip,
	}
}

func TestAppendAgentProductionPlanCreatesDurableReferenceArtifacts(t *testing.T) {
	repo, _ := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	draft := twoShotProductionPlanDraft("带角色参考的广告")
	draft.References = []agentruntime.ReferenceAssetDraft{{
		ReferenceKey: "hero", Role: "character", Title: "主角", ImagePrompt: "主角角色参考图",
	}}
	draft.Shots[0].ReferenceKeys = []string{"hero"}
	draft.Shots[1].ReferenceKeys = []string{"hero"}

	record, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "reference-plan", BaseVersion: 0, Draft: draft, Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Plan.ReferencesJSON == "" {
		t.Fatal("reference plan facts were not persisted")
	}
	var referenceArtifact model.AgentProductionArtifact
	for _, artifact := range record.Artifacts {
		if artifact.Kind == model.AgentProductionArtifactReferenceImage {
			referenceArtifact = artifact
			break
		}
	}
	if referenceArtifact.ID == "" || referenceArtifact.ReferenceKey != "hero" || referenceArtifact.ShotKey != "" || referenceArtifact.Status != model.AgentProductionArtifactPlanned {
		t.Fatalf("reference artifact = %#v", referenceArtifact)
	}
}

func assertProductionArtifactShape(t *testing.T, artifacts []model.AgentProductionArtifact) {
	t.Helper()
	want := map[string]bool{
		"/script":                 false,
		"shot-1/storyboard_image": false,
		"shot-1/video_clip":       false,
		"shot-2/storyboard_image": false,
		"shot-2/video_clip":       false,
	}
	ids := map[string]bool{}
	for _, artifact := range artifacts {
		key := artifact.ShotKey + "/" + string(artifact.Kind)
		if _, ok := want[key]; !ok {
			t.Fatalf("unexpected production artifact %q", key)
		}
		want[key] = true
		if ids[artifact.ID] {
			t.Fatalf("duplicate production artifact id %s", artifact.ID)
		}
		ids[artifact.ID] = true
		wantStatus := model.AgentProductionArtifactPlanned
		if artifact.Kind == model.AgentProductionArtifactScript {
			wantStatus = model.AgentProductionArtifactSucceeded
		}
		if artifact.Status != wantStatus || artifact.Attempt != 0 {
			t.Fatalf("initial artifact = %#v", artifact)
		}
	}
	for key, found := range want {
		if !found {
			t.Fatalf("missing production artifact %q", key)
		}
	}
}

func firstProductionArtifact(t *testing.T, artifacts []model.AgentProductionArtifact, shotKey string, kind model.AgentProductionArtifactKind) model.AgentProductionArtifact {
	t.Helper()
	for _, artifact := range artifacts {
		if artifact.ShotKey == shotKey && artifact.Kind == kind {
			return artifact
		}
	}
	t.Fatalf("missing artifact %s/%s", shotKey, kind)
	return model.AgentProductionArtifact{}
}
