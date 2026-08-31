package service

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

func TestExpectedDeliveryRequiresExactApprovedMediaRevisionAndReadyResource(t *testing.T) {
	legacy := agentruntime.ExpectedDelivery{
		Kind:              agentruntime.DeliveryGeneratedAsset,
		RequiredArtifacts: []agentruntime.ArtifactKind{agentruntime.ArtifactVideo},
		CompletionCriteria: []agentruntime.DeliveryCriterion{{
			Fact: agentruntime.DeliveryFactArtifact, Artifact: agentruntime.ArtifactVideo,
		}},
	}
	if expectedDeliveryRequiresMedia(legacy, "video") {
		t.Fatal("legacy URL-only delivery contract accepted for governed media generation")
	}

	exact := exactAgentMediaExpectedDelivery(agentruntime.ArtifactVideo)
	if !expectedDeliveryRequiresMedia(exact, "video") {
		t.Fatal("exact revision and ready resource delivery contract rejected")
	}
}

func TestDirectedRegenerationFreezesNextAttemptAndLeavesCurrentCandidateUntouched(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeVideoModel(t, db, fixture)
	scope := agentRuntimeServiceScope()
	scope.DomainProjectID = "runtime-project"
	now := time.Now().UTC()
	if err := db.Create(&model.AgentThread{
		ID: scope.ThreadID, TenantKind: scope.TenantKind, TenantID: scope.TenantID,
		CreatedByUserID: scope.ActorUserID, DomainProjectID: scope.DomainProjectID,
		CanvasID: scope.CanvasID, Status: agentruntime.ThreadActive, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AgentRun{
		ID: scope.RunID, ThreadID: scope.ThreadID, ActorUserID: scope.ActorUserID,
		ClientRequestID: "directed-regeneration-run", Status: agentruntime.RunRunning,
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	shotOne := seedDirectedRegenerationRevision(t, svc, scope, "shot-one", "shot-one", "shot_revision", nil)
	shotTwo := seedDirectedRegenerationRevision(t, svc, scope, "shot-two", "shot-two", "shot_revision", nil)
	configuration, callable := directedRegenerationVideoConfiguration(t)
	firstFrozen := freezeDirectedRegenerationSourceAttempt(t, svc, db, scope, configuration, callable, "source-shot-one", shotOne)
	secondFrozen := freezeDirectedRegenerationSourceAttempt(t, svc, db, scope, configuration, callable, "source-shot-two", shotTwo)
	candidateOne := seedDirectedVideoCandidate(t, svc, scope, firstFrozen.OutputArtifactID+"-01", shotOne, firstFrozen.Commercial.TaskID)
	candidateTwo := seedDirectedVideoCandidate(t, svc, scope, secondFrozen.OutputArtifactID+"-01", shotTwo, secondFrozen.Commercial.TaskID)

	updatedShotOne, err := svc.repo.AppendArtifactRevision(scope, shotOne.ArtifactID, shotOne.Revision, agentruntime.ArtifactDraft{
		ArtifactKey: "shot-one", Kind: "shot_revision", SchemaVersion: 1,
		Payload:           json.RawMessage(`{"dialogue":"第二版对白"}`),
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{}, SkillVersions: []agentruntime.SkillSelection{},
	})
	if err != nil {
		t.Fatal(err)
	}

	proposal := directedRegenerationVideoProposal(callable, exactAgentRevisionRef(updatedShotOne), &DirectedVideoRegenerationArguments{
		SourceShotRevision: exactAgentRevisionRef(shotOne), ApprovedCandidateRevision: exactAgentRevisionRef(candidateOne),
	})
	call := &agentruntime.ToolCallDecision{
		ToolCallID: "regenerate-shot-one", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
		ExpectedDelivery: proposal.ExpectedDelivery, Arguments: mustMarshalAgentMediaTestJSON(t, proposal),
	}
	if _, err := svc.freezeAgentMediaGenerationDecisionArguments(scope, configuration, []agentRuntimeCallableModelFact{callable}, call); !errors.Is(err, errDirectedVideoRegenerationInvalid) {
		t.Fatalf("unapproved candidate error = %v, want %v", err, errDirectedVideoRegenerationInvalid)
	}
	approveMediaAssemblyRevision(t, db, scope, candidateOne.ID, 1)
	approveMediaAssemblyRevision(t, db, scope, candidateTwo.ID, 2)
	if _, err := svc.freezeAgentMediaGenerationDecisionArguments(scope, configuration, []agentRuntimeCallableModelFact{callable}, call); !errors.Is(err, errDirectedVideoRegenerationInvalid) {
		t.Fatalf("missing source task error = %v, want %v", err, errDirectedVideoRegenerationInvalid)
	}
	persistDirectedRegenerationSourceTask(t, svc, db, scope, firstFrozen)
	frozenJSON, err := svc.freezeAgentMediaGenerationDecisionArguments(scope, configuration, []agentRuntimeCallableModelFact{callable}, call)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := decodeFrozenAgentMediaGenerationArguments(frozenJSON)
	if err != nil {
		t.Fatal(err)
	}
	if frozen.Commercial.Attempt != 2 || frozen.Commercial.TaskID == firstFrozen.Commercial.TaskID {
		t.Fatalf("directed commercial attempt = %d task=%q", frozen.Commercial.Attempt, frozen.Commercial.TaskID)
	}
	command, err := svc.currentAgentMediaGenerationCommand(scope, frozen)
	if err != nil || command.Attempt != 2 {
		t.Fatalf("current directed command = %#v, err=%v", command, err)
	}

	currentCandidate, err := svc.repo.ArtifactHeadRevisionForScope(scope, candidateTwo.ArtifactID)
	if err != nil || currentCandidate.ID != candidateTwo.ID || currentCandidate.LifecycleStatus == model.AgentArtifactRevisionStale {
		t.Fatalf("unaffected candidate changed = %#v, err=%v", currentCandidate, err)
	}
	currentProposal := directedRegenerationVideoProposal(callable, exactAgentRevisionRef(shotTwo), &DirectedVideoRegenerationArguments{
		SourceShotRevision: exactAgentRevisionRef(shotTwo), ApprovedCandidateRevision: exactAgentRevisionRef(candidateTwo),
	})
	currentCall := &agentruntime.ToolCallDecision{
		ToolCallID: "regenerate-current-shot", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
		ExpectedDelivery: currentProposal.ExpectedDelivery, Arguments: mustMarshalAgentMediaTestJSON(t, currentProposal),
	}
	_, err = svc.freezeAgentMediaGenerationDecisionArguments(scope, configuration, []agentRuntimeCallableModelFact{callable}, currentCall)
	if !errors.Is(err, errDirectedVideoRegenerationInvalid) {
		t.Fatalf("current shot regeneration error = %v, want %v", err, errDirectedVideoRegenerationInvalid)
	}
}

func TestM9ShotEditRegeneratesOnlyTransitiveCandidateAndKeepsCommercialFactsImmutable(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeVideoModel(t, db, fixture)
	scope := agentRuntimeServiceScope()
	scope.DomainProjectID = "runtime-project"
	now := time.Now().UTC()
	if err := db.Model(&model.CreditAccount{}).Where("user_id = ?", scope.ActorUserID).
		Update("available_microcredits", 10_000).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AgentThread{
		ID: scope.ThreadID, TenantKind: scope.TenantKind, TenantID: scope.TenantID,
		CreatedByUserID: scope.ActorUserID, DomainProjectID: scope.DomainProjectID,
		CanvasID: scope.CanvasID, Status: agentruntime.ThreadActive, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AgentRun{
		ID: scope.RunID, ThreadID: scope.ThreadID, ActorUserID: scope.ActorUserID,
		ClientRequestID: "m9-directed-regeneration-run", Status: agentruntime.RunRunning,
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	shotOne := seedDirectedRegenerationRevision(t, svc, scope, "m9-shot-one", "m9-shot-one", "shot_revision", nil)
	shotTwo := seedDirectedRegenerationRevision(t, svc, scope, "m9-shot-two", "m9-shot-two", "shot_revision", nil)
	configuration, callable := directedRegenerationVideoConfiguration(t)
	firstFrozen := freezeDirectedRegenerationSourceAttempt(t, svc, db, scope, configuration, callable, "m9-source-shot-one", shotOne)
	secondFrozen := freezeDirectedRegenerationSourceAttempt(t, svc, db, scope, configuration, callable, "m9-source-shot-two", shotTwo)
	persistDirectedRegenerationSourceTask(t, svc, db, scope, firstFrozen)
	persistDirectedRegenerationSourceTask(t, svc, db, scope, secondFrozen)
	firstCommercial := settleAndSnapshotDirectedCommercialFacts(t, svc, db, firstFrozen.Commercial.TaskID)
	secondCommercial := settleAndSnapshotDirectedCommercialFacts(t, svc, db, secondFrozen.Commercial.TaskID)

	candidateOne := seedDirectedVideoCandidate(t, svc, scope, firstFrozen.OutputArtifactID+"-01", shotOne, firstFrozen.Commercial.TaskID)
	candidateTwo := seedDirectedVideoCandidate(t, svc, scope, secondFrozen.OutputArtifactID+"-01", shotTwo, secondFrozen.Commercial.TaskID)
	approveMediaAssemblyRevision(t, db, scope, candidateOne.ID, 1)
	approveMediaAssemblyRevision(t, db, scope, candidateTwo.ID, 2)
	firstPlan, err := svc.repo.AppendArtifactRevision(
		scope,
		"m9-assembly-plan-artifact",
		0,
		m9AssemblyPlanDraft("m9-assembly-plan", exactAgentRevisionRef(candidateOne), exactAgentRevisionRef(candidateTwo)),
	)
	if err != nil {
		t.Fatal(err)
	}

	updatedShotOne, err := svc.repo.AppendArtifactRevision(scope, shotOne.ArtifactID, shotOne.Revision, agentruntime.ArtifactDraft{
		ArtifactKey: "m9-shot-one", Kind: "shot_revision", SchemaVersion: 1,
		Payload: json.RawMessage(`{"dialogue":"第二版对白"}`), UpstreamRevisions: []agentruntime.ArtifactRevisionRef{},
		SkillVersions: []agentruntime.SkillSelection{},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertAgentArtifactLifecycle(t, svc, scope, candidateOne.ID, model.AgentArtifactRevisionStale)
	assertAgentArtifactLifecycle(t, svc, scope, firstPlan.ID, model.AgentArtifactRevisionStale)
	assertAgentArtifactLifecycle(t, svc, scope, candidateTwo.ID, model.AgentArtifactRevisionAwaitingReview)

	proposal := directedRegenerationVideoProposal(callable, exactAgentRevisionRef(updatedShotOne), &DirectedVideoRegenerationArguments{
		SourceShotRevision: exactAgentRevisionRef(shotOne), ApprovedCandidateRevision: exactAgentRevisionRef(candidateOne),
	})
	call := &agentruntime.ToolCallDecision{
		ToolCallID: "m9-regenerate-shot-one", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
		ExpectedDelivery: proposal.ExpectedDelivery, Arguments: mustMarshalAgentMediaTestJSON(t, proposal),
	}
	frozenJSON, err := svc.freezeAgentMediaGenerationDecisionArguments(
		scope, configuration, []agentRuntimeCallableModelFact{callable}, call,
	)
	if err != nil {
		t.Fatal(err)
	}
	directedFrozen, err := decodeFrozenAgentMediaGenerationArguments(frozenJSON)
	if err != nil {
		t.Fatal(err)
	}
	if directedFrozen.Commercial.Attempt != 2 || directedFrozen.Commercial.TaskID == firstFrozen.Commercial.TaskID ||
		directedFrozen.Commercial.TaskID == secondFrozen.Commercial.TaskID {
		t.Fatalf("directed task fact = attempt %d task %q", directedFrozen.Commercial.Attempt, directedFrozen.Commercial.TaskID)
	}
	persistDirectedRegenerationSourceTask(t, svc, db, scope, directedFrozen)
	replacementCandidate := seedDirectedVideoCandidate(
		t, svc, scope, directedFrozen.OutputArtifactID+"-01", updatedShotOne, directedFrozen.Commercial.TaskID,
	)
	approveMediaAssemblyRevision(t, db, scope, replacementCandidate.ID, 3)
	secondPlan, err := svc.repo.AppendArtifactRevision(
		scope,
		firstPlan.ArtifactID,
		firstPlan.Revision,
		m9AssemblyPlanDraft("m9-assembly-plan", exactAgentRevisionRef(replacementCandidate), exactAgentRevisionRef(candidateTwo)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if secondPlan.Revision != 2 {
		t.Fatalf("replacement assembly revision = %d, want 2", secondPlan.Revision)
	}

	currentCandidateTwo, err := svc.repo.ArtifactHeadRevisionForScope(scope, candidateTwo.ArtifactID)
	if err != nil || currentCandidateTwo.ID != candidateTwo.ID || currentCandidateTwo.LifecycleStatus != model.AgentArtifactRevisionAwaitingReview {
		t.Fatalf("unaffected candidate changed = %#v, err=%v", currentCandidateTwo, err)
	}
	candidates, err := svc.repo.MediaCandidateRevisionsInScope(scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 3 {
		t.Fatalf("candidate facts = %d, want two original plus one replacement", len(candidates))
	}
	assertDirectedCommercialFactsUnchanged(t, svc, db, firstCommercial)
	assertDirectedCommercialFactsUnchanged(t, svc, db, secondCommercial)
}

type directedCommercialFactsSnapshot struct {
	task   model.Task
	order  model.BillingOrder
	ledger []model.CreditLedgerEntry
}

func settleAndSnapshotDirectedCommercialFacts(
	t *testing.T,
	svc *Service,
	db *gorm.DB,
	taskID string,
) directedCommercialFactsSnapshot {
	t.Helper()
	task, err := svc.repo.Task(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.SettleBillingOrder(task.BillingOrderID, "provider-"+task.ID); err != nil {
		t.Fatal(err)
	}
	task, err = svc.repo.Task(taskID)
	if err != nil {
		t.Fatal(err)
	}
	order, err := svc.repo.BillingOrder(task.BillingOrderID)
	if err != nil {
		t.Fatal(err)
	}
	var ledger []model.CreditLedgerEntry
	if err := db.Where("billing_order_id = ?", order.ID).Order("created_at ASC, id ASC").Find(&ledger).Error; err != nil {
		t.Fatal(err)
	}
	if len(ledger) != 2 || ledger[0].Type != model.CreditLedgerReserve || ledger[1].Type != model.CreditLedgerConsume {
		t.Fatalf("settled commercial ledger = %#v", ledger)
	}
	return directedCommercialFactsSnapshot{task: *task, order: *order, ledger: ledger}
}

func assertDirectedCommercialFactsUnchanged(
	t *testing.T,
	svc *Service,
	db *gorm.DB,
	want directedCommercialFactsSnapshot,
) {
	t.Helper()
	gotTask, err := svc.repo.Task(want.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotTask.Status != want.task.Status || gotTask.Stage != want.task.Stage || gotTask.Progress != want.task.Progress ||
		gotTask.BillingOrderID != want.task.BillingOrderID || gotTask.InputJSON != want.task.InputJSON ||
		gotTask.ResultJSON != want.task.ResultJSON || !gotTask.UpdatedAt.Equal(want.task.UpdatedAt) {
		t.Fatalf("provider task mutated: got=%#v want=%#v", gotTask, want.task)
	}
	gotOrder, err := svc.repo.BillingOrder(want.order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotOrder.Status != want.order.Status || gotOrder.AmountMicrocredits != want.order.AmountMicrocredits ||
		gotOrder.ProviderRequestID != want.order.ProviderRequestID || !equalOptionalTime(gotOrder.SettledAt, want.order.SettledAt) ||
		!gotOrder.UpdatedAt.Equal(want.order.UpdatedAt) {
		t.Fatalf("billing order mutated: got=%#v want=%#v", gotOrder, want.order)
	}
	var gotLedger []model.CreditLedgerEntry
	if err := db.Where("billing_order_id = ?", want.order.ID).Order("created_at ASC, id ASC").Find(&gotLedger).Error; err != nil {
		t.Fatal(err)
	}
	if len(gotLedger) != len(want.ledger) {
		t.Fatalf("billing ledger entries = %d, want %d", len(gotLedger), len(want.ledger))
	}
	for index := range want.ledger {
		got := gotLedger[index]
		expected := want.ledger[index]
		if got.ID != expected.ID || got.Type != expected.Type || got.AmountMicrocredits != expected.AmountMicrocredits ||
			got.AvailableDeltaMicrocredits != expected.AvailableDeltaMicrocredits ||
			got.ReservedDeltaMicrocredits != expected.ReservedDeltaMicrocredits ||
			got.AvailableAfterMicrocredits != expected.AvailableAfterMicrocredits ||
			got.ReservedAfterMicrocredits != expected.ReservedAfterMicrocredits ||
			got.BillingOrderID != expected.BillingOrderID || !got.CreatedAt.Equal(expected.CreatedAt) {
			t.Fatalf("billing ledger entry %d mutated: got=%#v want=%#v", index, got, expected)
		}
	}
}

func equalOptionalTime(first *time.Time, second *time.Time) bool {
	if first == nil || second == nil {
		return first == nil && second == nil
	}
	return first.Equal(*second)
}

func assertAgentArtifactLifecycle(
	t *testing.T,
	svc *Service,
	scope agentruntime.Scope,
	revisionID string,
	want string,
) {
	t.Helper()
	revision, err := svc.repo.ArtifactRevisionInScope(scope, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	if revision.LifecycleStatus != want {
		t.Fatalf("artifact revision %q lifecycle = %q, want %q", revisionID, revision.LifecycleStatus, want)
	}
}

func m9AssemblyPlanDraft(
	artifactKey string,
	first agentruntime.ArtifactRevisionRef,
	second agentruntime.ArtifactRevisionRef,
) agentruntime.ArtifactDraft {
	payload, err := json.Marshal(agentruntime.AssemblyPlanV2{
		PlanKey: artifactKey, AudioMode: agentruntime.MediaAudioNone,
		Clips: []agentruntime.AssemblyClipV2{
			{
				ClipKey: "clip-one", SourceRevision: first,
				TrimStartMS: m9Int64Pointer(0), TrimEndMS: m9Int64Pointer(1000),
				TransitionToNext: agentruntime.AssemblyTransitionV2{Kind: agentruntime.AssemblyTransitionCut, DurationMS: m9Int64Pointer(0)},
			},
			{
				ClipKey: "clip-two", SourceRevision: second,
				TrimStartMS: m9Int64Pointer(0), TrimEndMS: m9Int64Pointer(1000),
				TransitionToNext: agentruntime.AssemblyTransitionV2{Kind: agentruntime.AssemblyTransitionCut, DurationMS: m9Int64Pointer(0)},
			},
		},
		AudioTracks: []agentruntime.AssemblyAudioTrackV2{},
		Output: agentruntime.AssemblyOutputV2{
			ArtifactKey: "m9-assembled-video", Container: "mp4", VideoCodec: "h264", AudioCodec: "none",
			Width: m9IntPointer(1280), Height: m9IntPointer(720), FrameRate: m9IntPointer(24),
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

func m9Int64Pointer(value int64) *int64 {
	return &value
}

func m9IntPointer(value int) *int {
	return &value
}

func persistDirectedRegenerationSourceTask(
	t *testing.T,
	svc *Service,
	db *gorm.DB,
	scope agentruntime.Scope,
	frozen agentMediaGenerationArguments,
) {
	t.Helper()
	capabilities, err := decodeFrozenMediaCapabilities(frozen.Commercial.ProviderCapabilitiesJSON)
	if err != nil {
		t.Fatal(err)
	}
	command, err := agentMediaGenerationCommand(scope, frozen, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := svc.ApproveMediaAttempt(scope, frozen.Commercial, command, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := svc.EnsureMediaTask(t.Context(), scope, *approved)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Updates(model.Task{
		Status: model.TaskStatusSucceeded, Stage: "任务完成", Progress: 100,
		CompletedAt: &now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func directedRegenerationVideoConfiguration(t *testing.T) (agentruntime.RunConfiguration, agentRuntimeCallableModelFact) {
	t.Helper()
	selection := agentruntime.GenerationModelSelection{ChannelID: "runtime-video-channel", Model: "doubao-seedance-2-5-260628"}
	callable := agentRuntimeCallableModelFact{
		ChannelID: selection.ChannelID, Model: selection.Model, DisplayName: "Seedance 2.5", Capability: "video",
		BillingMode: "per_second", PriceStrategy: "video_resolution",
		ProviderCapabilities: publicProviderModelCapabilities(model.ChannelInterfaceAIOpenVideoVolcengine, selection.Model),
	}
	return agentruntime.RunConfiguration{
		ExecutionMode:    agentruntime.ExecutionGuided,
		GenerationModels: agentruntime.GenerationModelSelections{Video: &selection},
		Skills:           []agentruntime.SkillSelection{},
	}, callable
}

func freezeDirectedRegenerationSourceAttempt(
	t *testing.T,
	svc *Service,
	db *gorm.DB,
	scope agentruntime.Scope,
	configuration agentruntime.RunConfiguration,
	callable agentRuntimeCallableModelFact,
	toolCallID string,
	shot *model.AgentArtifactRevision,
) agentMediaGenerationArguments {
	t.Helper()
	proposal := directedRegenerationVideoProposal(callable, exactAgentRevisionRef(shot), nil)
	call := &agentruntime.ToolCallDecision{
		ToolCallID: toolCallID, ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
		ExpectedDelivery: proposal.ExpectedDelivery, Arguments: mustMarshalAgentMediaTestJSON(t, proposal),
	}
	frozenJSON, err := svc.freezeAgentMediaGenerationDecisionArguments(scope, configuration, []agentRuntimeCallableModelFact{callable}, call)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := decodeFrozenAgentMediaGenerationArguments(frozenJSON)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.Create(&model.AgentToolCall{
		ID: "stored-" + toolCallID, RunID: scope.RunID, ToolCallID: toolCallID, ActionVersion: 1,
		ToolName: string(agentruntime.ToolMediaGenerate), Status: agentruntime.ToolCallSucceeded,
		IdempotencyKey: scope.RunID + ":" + toolCallID, InputJSON: string(frozenJSON), OutputJSON: `{}`,
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	return frozen
}

func directedRegenerationVideoProposal(
	callable agentRuntimeCallableModelFact,
	shot agentruntime.ArtifactRevisionRef,
	directed *DirectedVideoRegenerationArguments,
) MediaGenerateArguments {
	return MediaGenerateArguments{
		InputRevisions:    []agentruntime.ArtifactRevisionRef{shot},
		GenerationModel:   agentruntime.GenerationModelSelection{ChannelID: callable.ChannelID, Model: callable.Model},
		Capability:        "video",
		Parameters:        json.RawMessage(`{"prompt":"雨夜追逐镜头","aspectRatio":"16:9","resolution":"720p","durationSeconds":5,"generateAudio":false}`),
		OutputArtifactKey: "directed-video", ExpectedOutputSchema: agentMediaCandidateSchema,
		ExpectedDelivery:     exactAgentMediaExpectedDelivery(agentruntime.ArtifactVideo),
		DirectedRegeneration: directed,
	}
}

func seedDirectedRegenerationRevision(
	t *testing.T,
	svc *Service,
	scope agentruntime.Scope,
	artifactID string,
	artifactKey string,
	kind string,
	upstream []agentruntime.ArtifactRevisionRef,
) *model.AgentArtifactRevision {
	t.Helper()
	if upstream == nil {
		upstream = []agentruntime.ArtifactRevisionRef{}
	}
	revision, err := svc.repo.AppendArtifactRevision(scope, artifactID, 0, agentruntime.ArtifactDraft{
		ArtifactKey: artifactKey, Kind: kind, SchemaVersion: 1,
		Payload: json.RawMessage(`{"state":"current"}`), UpstreamRevisions: upstream,
		SkillVersions: []agentruntime.SkillSelection{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

func seedDirectedVideoCandidate(
	t *testing.T,
	svc *Service,
	scope agentruntime.Scope,
	artifactID string,
	shot *model.AgentArtifactRevision,
	taskID string,
) *model.AgentArtifactRevision {
	t.Helper()
	payload, err := json.Marshal(agentruntime.MediaCandidateContent{
		CandidateKey: artifactID, MediaKind: agentruntime.ArtifactVideo,
		ProviderRequestIdentity: "provider-" + taskID, ResourceID: "resource-" + artifactID,
		SourceTaskID: taskID,
	})
	if err != nil {
		t.Fatal(err)
	}
	revision, err := svc.repo.AppendMediaCandidateRevision(scope, artifactID, agentruntime.ArtifactDraft{
		ArtifactKey: artifactID, Kind: "media_candidate", SchemaVersion: 1, Payload: payload,
		ResourceID: "resource-" + artifactID, UpstreamRevisions: []agentruntime.ArtifactRevisionRef{exactAgentRevisionRef(shot)},
		ModelRequestIdentity: "provider-" + taskID, SkillVersions: []agentruntime.SkillSelection{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

func exactAgentRevisionRef(revision *model.AgentArtifactRevision) agentruntime.ArtifactRevisionRef {
	return agentruntime.ArtifactRevisionRef{ArtifactID: revision.ArtifactID, RevisionID: revision.ID}
}

func exactAgentMediaExpectedDelivery(kind agentruntime.ArtifactKind) agentruntime.ExpectedDelivery {
	return agentruntime.ExpectedDelivery{
		Kind:              agentruntime.DeliveryGeneratedAsset,
		RequiredArtifacts: []agentruntime.ArtifactKind{kind},
		CompletionCriteria: []agentruntime.DeliveryCriterion{
			{Fact: agentruntime.DeliveryFactArtifactRevision, Artifact: kind},
			{Fact: agentruntime.DeliveryFactResource, Artifact: kind},
		},
	}
}

func TestFreezeAgentMediaGenerationBindsExactInputsModelAndQuote(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeImageModel(t, db, fixture)
	scope := agentRuntimeServiceScope()
	scope.DomainProjectID = "runtime-project"
	resource := seedAgentMediaInputResource(t, db, scope, "media-input-resource", "image", "inputs/reference.png", "etag-reference")
	revision := seedAgentMediaInputRevision(t, svc, scope, "media-input-artifact", "media-input", resource.ID)
	callable := agentRuntimeCallableModelFact{
		ChannelID: "runtime-image-channel", Model: "kz_gpt_image2", DisplayName: "GPT Image 2",
		Capability: "image", BillingMode: "fixed_request", PriceStrategy: "image_resolution",
		UnitPriceMicrocredits: 250,
		ProviderCapabilities:  agentRuntimeGPTImageCapabilitiesForTest(t),
	}
	proposal := MediaGenerateArguments{
		InputRevisions:    []agentruntime.ArtifactRevisionRef{{ArtifactID: revision.ArtifactID, RevisionID: revision.ID}},
		GenerationModel:   agentruntime.GenerationModelSelection{ChannelID: callable.ChannelID, Model: callable.Model},
		Capability:        "image",
		Parameters:        json.RawMessage(`{"prompt":"角色定妆照","aspectRatio":"1:1","resolution":"1K","quality":"medium","count":1}`),
		OutputArtifactKey: "character-portrait", ExpectedOutputSchema: agentMediaCandidateSchema,
		ExpectedDelivery: exactAgentMediaExpectedDelivery(agentruntime.ArtifactImage),
	}
	call := &agentruntime.ToolCallDecision{
		ToolCallID: "media-call-1", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
		ExpectedDelivery: proposal.ExpectedDelivery,
	}
	call.Arguments = mustMarshalAgentMediaTestJSON(t, proposal)

	frozenJSON, err := svc.freezeAgentMediaGenerationDecisionArguments(
		scope,
		agentruntime.RunConfiguration{
			ExecutionMode: agentruntime.ExecutionGuided,
			GenerationModels: agentruntime.GenerationModelSelections{
				Image: &proposal.GenerationModel,
			},
			Skills: []agentruntime.SkillSelection{},
		},
		[]agentRuntimeCallableModelFact{callable},
		call,
	)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := decodeFrozenAgentMediaGenerationArguments(frozenJSON)
	if err != nil {
		t.Fatal(err)
	}
	if frozen.GenerationModelRecordID != "runtime-image-model" || frozen.Commercial.ChannelModelID != "runtime-image-model" {
		t.Fatalf("frozen model records = %q / %q", frozen.GenerationModelRecordID, frozen.Commercial.ChannelModelID)
	}
	if len(frozen.InputResources) != 1 || frozen.InputResources[0].ResourceID != resource.ID ||
		frozen.InputResources[0].ObjectKey != resource.ObjectKey || frozen.InputResources[0].ETag != resource.ETag {
		t.Fatalf("frozen input resources = %#v", frozen.InputResources)
	}
	if frozen.Commercial.QuoteID == "" || frozen.Commercial.ApprovalFingerprint == "" || frozen.RequestIdentity == "" {
		t.Fatalf("frozen approval facts are incomplete: %#v", frozen)
	}

	if err := db.Model(&model.Resource{}).Where("id = ?", resource.ID).Update("object_key", "inputs/changed.png").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.currentAgentMediaGenerationCommand(scope, frozen); !errors.Is(err, errAgentMediaInputChanged) {
		t.Fatalf("changed resource error = %v, want %v", err, errAgentMediaInputChanged)
	}
}

func TestDirectedRegenerationFreezesStructuralLineageWithoutProviderMedia(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeImageModel(t, db, fixture)
	scope := agentRuntimeServiceScope()
	scope.DomainProjectID = "runtime-project"
	shot := seedDirectedRegenerationRevision(t, svc, scope, "shot-lineage", "shot-lineage", "shot_revision", nil)
	resource := seedAgentMediaInputResource(t, db, scope, "lineage-reference", "image", "inputs/lineage.png", "etag-lineage")
	reference := seedAgentMediaInputRevision(t, svc, scope, "lineage-reference-artifact", "lineage-reference", resource.ID)
	callable := agentRuntimeCallableModelFact{
		ChannelID: "runtime-image-channel", Model: "kz_gpt_image2", DisplayName: "GPT Image 2",
		Capability: "image", BillingMode: "fixed_request", PriceStrategy: "image_resolution",
		UnitPriceMicrocredits: 250, ProviderCapabilities: agentRuntimeGPTImageCapabilitiesForTest(t),
	}
	proposal := MediaGenerateArguments{
		InputRevisions:  []agentruntime.ArtifactRevisionRef{exactAgentRevisionRef(shot), exactAgentRevisionRef(&reference)},
		GenerationModel: agentruntime.GenerationModelSelection{ChannelID: callable.ChannelID, Model: callable.Model},
		Capability:      "image", Parameters: json.RawMessage(`{"prompt":"镜头首帧","aspectRatio":"1:1","resolution":"1K","quality":"medium","count":1}`),
		OutputArtifactKey: "shot-lineage-frame", ExpectedOutputSchema: agentMediaCandidateSchema,
		ExpectedDelivery: exactAgentMediaExpectedDelivery(agentruntime.ArtifactImage),
	}
	call := &agentruntime.ToolCallDecision{
		ToolCallID: "directed-lineage-call", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
		ExpectedDelivery: proposal.ExpectedDelivery, Arguments: mustMarshalAgentMediaTestJSON(t, proposal),
	}
	frozenJSON, err := svc.freezeAgentMediaGenerationDecisionArguments(
		scope,
		agentruntime.RunConfiguration{
			ExecutionMode:    agentruntime.ExecutionGuided,
			GenerationModels: agentruntime.GenerationModelSelections{Image: &proposal.GenerationModel},
			Skills:           []agentruntime.SkillSelection{},
		},
		[]agentRuntimeCallableModelFact{callable},
		call,
	)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := decodeFrozenAgentMediaGenerationArguments(frozenJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(frozen.InputRevisions) != 2 || frozen.InputRevisions[0] != exactAgentRevisionRef(shot) ||
		len(frozen.InputResources) != 1 || frozen.InputResources[0].Revision != exactAgentRevisionRef(&reference) {
		t.Fatalf("frozen lineage/resources = %#v / %#v", frozen.InputRevisions, frozen.InputResources)
	}
	if _, err := svc.currentAgentMediaGenerationCommand(scope, frozen); err != nil {
		t.Fatalf("current command rejected exact structural lineage: %v", err)
	}
	if _, err := svc.repo.AppendArtifactRevision(scope, shot.ArtifactID, shot.Revision, agentruntime.ArtifactDraft{
		ArtifactKey: "shot-lineage", Kind: "shot_revision", SchemaVersion: 1,
		Payload: json.RawMessage(`{"state":"revised"}`), UpstreamRevisions: []agentruntime.ArtifactRevisionRef{},
		SkillVersions: []agentruntime.SkillSelection{},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.currentAgentMediaGenerationCommand(scope, frozen); !errors.Is(err, errAgentMediaInputChanged) {
		t.Fatalf("changed structural lineage error = %v, want %v", err, errAgentMediaInputChanged)
	}
}

func TestAgentMediaGenerationMaterializesEveryCandidateExactlyOnce(t *testing.T) {
	skipRetiredAgentExecutionGraph(t)
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	scope := agentRuntimeServiceScope()
	scope.DomainProjectID = "runtime-project"
	image := seedAgentMediaInputResource(t, db, scope, "generated-image", "image", "outputs/image.png", "etag-image")
	video := seedAgentMediaInputResource(t, db, scope, "generated-video", "video", "outputs/video.mp4", "etag-video")
	audio := seedAgentMediaInputResource(t, db, scope, "generated-audio", "audio", "outputs/audio.mp3", "etag-audio")
	arguments := agentMediaGenerationArguments{
		InputRevisions: []agentruntime.ArtifactRevisionRef{}, InputResources: []agentMediaInputResource{},
		GenerationModel:         agentruntime.GenerationModelSelection{ChannelID: "channel", Model: "model"},
		GenerationModelRecordID: "model-record", Capability: "image",
		Parameters:       json.RawMessage(`{"prompt":"候选资产","aspectRatio":"1:1","resolution":"1K","quality":"medium","count":1}`),
		OutputArtifactID: "media-output-root", OutputArtifactKey: "media-output",
		ExpectedOutputSchema: agentMediaCandidateSchema,
		ExpectedDelivery:     exactAgentMediaExpectedDelivery(agentruntime.ArtifactImage),
		RequestIdentity:      "media-generation:request-1", SkillVersions: []agentruntime.SkillSelection{},
		Commercial: MediaGenerationAttempt{
			ArtifactRevisionID: "media-output-root", Attempt: 1, TaskID: "media-task",
			ApprovalFingerprint: "media-approval",
		},
	}
	resultJSON := `{"images":[{"resourceId":"generated-image"}],"videos":[{"resourceId":"generated-video"}],"audio":{"resourceId":"generated-audio"}}`
	task := model.Task{ID: "media-task", UserID: scope.ActorUserID, ProjectID: scope.CanvasID, Status: model.TaskStatusSucceeded, ResultJSON: resultJSON}
	call := startRunningMediaMaterializationAttempt(t, svc, scope, arguments)

	first, firstDisposition, err := svc.materializeAgentMediaCandidates(scope, task, arguments, call)
	if err != nil {
		t.Fatal(err)
	}
	second, secondDisposition, err := svc.materializeAgentMediaCandidates(scope, task, arguments, call)
	if err != nil {
		t.Fatal(err)
	}
	if firstDisposition != repository.MediaAttemptWriteAdopted || secondDisposition != repository.MediaAttemptWriteAdopted {
		t.Fatalf("candidate dispositions = %q / %q", firstDisposition, secondDisposition)
	}
	if len(first) != 3 || len(second) != 3 {
		t.Fatalf("candidate counts first=%d second=%d", len(first), len(second))
	}
	for index := range first {
		if first[index].ID != second[index].ID {
			t.Fatalf("candidate replay %d = %q, want %q", index, second[index].ID, first[index].ID)
		}
	}
	stored, err := svc.repo.MediaCandidateRevisionsInScope(scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 3 {
		t.Fatalf("stored candidate count = %d, want 3", len(stored))
	}
	wantResources := map[string]string{"image": image.ID, "video": video.ID, "audio": audio.ID}
	for index := range stored {
		var payload agentruntime.MediaCandidateContent
		if err := json.Unmarshal([]byte(stored[index].PayloadJSON), &payload); err != nil {
			t.Fatal(err)
		}
		if wantResources[string(payload.MediaKind)] != payload.ResourceID {
			t.Fatalf("candidate %d payload = %#v", index, payload)
		}
	}
}

func TestAgentMediaGenerationLateResultAfterCancellationIsUnadopted(t *testing.T) {
	skipRetiredAgentExecutionGraph(t)
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	scope := agentRuntimeServiceScope()
	scope.DomainProjectID = "runtime-project"
	resource := seedAgentMediaInputResource(t, db, scope, "late-generated-image", "image", "outputs/late-image.png", "etag-late-image")
	arguments := agentMediaGenerationArguments{
		InputRevisions: []agentruntime.ArtifactRevisionRef{}, InputResources: []agentMediaInputResource{},
		GenerationModel:         agentruntime.GenerationModelSelection{ChannelID: "channel", Model: "model"},
		GenerationModelRecordID: "model-record", Capability: "image",
		Parameters:           json.RawMessage(`{"prompt":"迟到资产","aspectRatio":"1:1","resolution":"1K","quality":"medium","count":1}`),
		OutputArtifactID:     "late-media-output-root",
		OutputArtifactKey:    "late-media-output",
		ExpectedOutputSchema: agentMediaCandidateSchema,
		ExpectedDelivery:     exactAgentMediaExpectedDelivery(agentruntime.ArtifactImage),
		RequestIdentity:      "media-generation:late-request",
		SkillVersions:        []agentruntime.SkillSelection{},
		Commercial: MediaGenerationAttempt{
			ArtifactRevisionID: "late-media-output-root", Attempt: 1, TaskID: "late-media-task",
			ApprovalFingerprint: "late-media-approval",
		},
	}
	call := startRunningMediaMaterializationAttempt(t, svc, scope, arguments)
	state, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repo.CancelAgentRunTree(scope, state.StateVersion, time.Now().UTC().Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	candidates, disposition, err := svc.materializeAgentMediaCandidates(scope, model.Task{
		ID: arguments.Commercial.TaskID, UserID: scope.ActorUserID, ProjectID: scope.CanvasID,
		Status: model.TaskStatusSucceeded, ResultJSON: `{"images":[{"resourceId":"` + resource.ID + `"}]}`,
	}, arguments, call)
	if err != nil {
		t.Fatal(err)
	}
	if disposition != repository.MediaAttemptWriteUnadopted || len(candidates) != 1 || candidates[0].LifecycleStatus != model.AgentArtifactRevisionUnadopted {
		t.Fatalf("late media candidates = disposition %q candidates %#v", disposition, candidates)
	}
	if _, err := svc.repo.ArtifactHeadRevisionForScope(scope, candidates[0].ArtifactID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("late media candidate advanced artifact head: %v", err)
	}
}

func startRunningMediaMaterializationAttempt(
	t *testing.T,
	svc *Service,
	scope agentruntime.Scope,
	arguments agentMediaGenerationArguments,
) *agentruntime.ToolCallDecision {
	t.Helper()
	now := time.Now().UTC()
	if _, err := svc.repo.CreateInitializedAgentRun(repository.CreateInitializedAgentRunInput{
		Create: repository.CreateAgentRunInput{Scope: scope, ClientRequestID: "media-materialization", Now: now},
		Initialize: repository.InitializeAgentRunInput{
			Scope: scope, ModelRecordID: "media-model-record", ModelKey: "media-model", MaxSteps: 8,
			ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion, RuntimeVersion: agentruntime.CurrentRuntimeVersion,
			PolicyVersion: agentruntime.CurrentPolicyVersion, UserMessage: "生成候选资产",
			Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic}, Now: now,
		},
	}); err != nil {
		t.Fatal(err)
	}
	queued, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	running, err := agentruntime.BeginModelRequest(queued)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CommitAgentRuntimeTransition(scope, queued, running, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	call := &agentruntime.ToolCallDecision{
		ToolCallID: "media-materialization-call", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
		Arguments: mustMarshalAgentMediaTestJSON(t, arguments), ExpectedDelivery: arguments.ExpectedDelivery,
	}
	waitingApproval, err := agentruntime.Advance(running.State, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall, ToolCall: call,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CommitAgentRuntimeTransition(scope, running.State, waitingApproval, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	approved, err := agentruntime.ReviewToolApproval(waitingApproval.State, agentruntime.ToolApproval{
		ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion, Decision: agentruntime.ToolApprovalApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CommitAgentRuntimeTransition(scope, waitingApproval.State, approved, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	started, err := agentruntime.BeginToolExecution(approved.State, agentruntime.ToolExecution{
		ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CommitAgentRuntimeTransition(scope, approved.State, started, now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	return call
}

func TestAgentMediaGenerationOperationWakesOwningRun(t *testing.T) {
	want := "runtime-run"
	operation := agentMediaGenerationOperationForRun(want)
	if got, ok := agentMediaGenerationRunID(operation); !ok || got != want {
		t.Fatalf("operation parser = %q/%v, want %q/true", got, ok, want)
	}
	for _, invalid := range []string{"", "media_generation:", "other:" + want} {
		if got, ok := agentMediaGenerationRunID(invalid); ok || got != "" {
			t.Fatalf("invalid operation %q parsed as %q/%v", invalid, got, ok)
		}
	}
}

func TestNativeAudioVideoUsesOneVideoTaskWithoutIndependentAudioArtifact(t *testing.T) {
	arguments := agentMediaGenerationArguments{
		Capability: "video", GenerationModel: agentruntime.GenerationModelSelection{ChannelID: "video-channel", Model: "video-model"},
		Parameters: json.RawMessage(`{"prompt":"角色说出对白","aspectRatio":"16:9","resolution":"720p","durationSeconds":5,"generateAudio":true}`),
	}
	capabilities := &PublicProviderCapabilities{
		Capability: "video", ModelKey: "video-model", Ratios: []string{"16:9"}, Resolutions: []string{"720p"},
		DurationMin: 1, DurationMax: 15, SupportsGeneratedAudio: true, GeneratedAudioResolutions: []string{"720p"},
	}
	input, _, _, err := buildAgentMediaGenerationTaskInput(arguments, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	mode, err := agentMediaAudioModeForArguments(arguments)
	if err != nil {
		t.Fatal(err)
	}
	if mode != agentruntime.MediaAudioNative || input.Mode != "video" || input.Config.VideoGenerateAudio != "true" {
		t.Fatalf("native audio task = mode %q input %#v", mode, input)
	}
	if len(input.ReferenceAudios) != 0 {
		t.Fatalf("native audio task created independent audio inputs: %#v", input.ReferenceAudios)
	}
}

func TestNativeAudioVideoRejectsModelWithoutFrozenCapability(t *testing.T) {
	arguments := agentMediaGenerationArguments{
		Capability: "video", GenerationModel: agentruntime.GenerationModelSelection{ChannelID: "video-channel", Model: "video-model"},
		Parameters: json.RawMessage(`{"prompt":"角色说出对白","aspectRatio":"16:9","resolution":"720p","durationSeconds":5,"generateAudio":true}`),
	}
	capabilities := &PublicProviderCapabilities{
		Capability: "video", ModelKey: "video-model", Ratios: []string{"16:9"}, Resolutions: []string{"720p"},
		DurationMin: 1, DurationMax: 15,
	}
	if _, _, _, err := buildAgentMediaGenerationTaskInput(arguments, capabilities); !errors.Is(err, errAgentNativeAudioUnavailable) {
		t.Fatalf("native audio capability error = %v, want %v", err, errAgentNativeAudioUnavailable)
	}
	code, class, ok := agentMediaGenerationFailureDetails(errAgentNativeAudioUnavailable)
	if !ok || code != "native_audio_capability_unavailable" || class != agentruntime.ToolFailureAgentRepairable {
		t.Fatalf("native audio failure classification = %q/%q/%v", code, class, ok)
	}
}

func TestIndependentAudioUsesSeparateAudioCapabilityAndQuoteIdentity(t *testing.T) {
	arguments := agentMediaGenerationArguments{
		Capability: "audio", GenerationModel: agentruntime.GenerationModelSelection{ChannelID: "audio-channel", Model: "speech-model"},
		Parameters: json.RawMessage(`{"prompt":"别回头。","voice":"hero-voice"}`),
		Commercial: MediaGenerationAttempt{QuoteID: "audio-quote"},
	}
	capabilities := &PublicProviderCapabilities{Capability: "audio", ModelKey: "speech-model", SupportsAudioOnly: true}
	input, _, quantity, err := buildAgentMediaGenerationTaskInput(arguments, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	mode, err := agentMediaAudioModeForArguments(arguments)
	if err != nil {
		t.Fatal(err)
	}
	if mode != agentruntime.MediaAudioIndependent || input.Mode != "audio" || quantity != 1 || arguments.Commercial.QuoteID == "" {
		t.Fatalf("independent audio task = mode %q quantity %d input %#v quote %q", mode, quantity, input, arguments.Commercial.QuoteID)
	}
}

func TestCoordinatePendingAgentMediaGenerationCreatesInternalTaskAndResolvesCandidates(t *testing.T) {
	skipRetiredAgentExecutionGraph(t)
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeCanvas(t, db)
	createAgentRuntimeImageModel(t, db, fixture)
	scope := agentRuntimeServiceScope()
	scope.DomainProjectID = "runtime-project"
	now := time.Now().UTC()
	if err := db.Save(&model.Project{
		ID: scope.DomainProjectID, UserID: scope.ActorUserID, Name: "Media Runtime Project",
		Type: "short-drama", Status: model.ProjectStatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", scope.CanvasID).
		Update("project_id", scope.DomainProjectID).Error; err != nil {
		t.Fatal(err)
	}
	selection := agentruntime.GenerationModelSelection{ChannelID: "runtime-image-channel", Model: "kz_gpt_image2"}
	configuration := agentruntime.RunConfiguration{
		ExecutionMode:    agentruntime.ExecutionGuided,
		GenerationModels: agentruntime.GenerationModelSelections{Image: &selection},
		Skills:           []agentruntime.SkillSelection{},
	}
	if _, err := svc.repo.CreateInitializedAgentRun(repository.CreateInitializedAgentRunInput{
		Create: repository.CreateAgentRunInput{Scope: scope, ClientRequestID: "media-coordinator", Now: now},
		Initialize: repository.InitializeAgentRunInput{
			Scope: scope, ModelRecordID: fixture.channelModel.ID, ModelKey: fixture.channelModel.ModelKey,
			MaxSteps: 8, ToolSchemaVersion: agentruntime.ProductionToolSchemaVersion,
			RuntimeVersion: agentruntime.ProductionRuntimeVersion, PolicyVersion: agentruntime.ProductionPolicyVersion,
			UserMessage: "生成角色图", Configuration: configuration, Now: now,
		},
	}); err != nil {
		t.Fatal(err)
	}
	queued, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	running, err := agentruntime.BeginModelRequest(queued)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CommitAgentRuntimeTransition(scope, queued, running, now); err != nil {
		t.Fatal(err)
	}
	delivery := exactAgentMediaExpectedDelivery(agentruntime.ArtifactImage)
	proposal := MediaGenerateArguments{
		InputRevisions: []agentruntime.ArtifactRevisionRef{}, GenerationModel: selection, Capability: "image",
		Parameters:        json.RawMessage(`{"prompt":"角色定妆照","aspectRatio":"1:1","resolution":"1K","quality":"medium","count":1}`),
		OutputArtifactKey: "character-portrait", ExpectedOutputSchema: agentMediaCandidateSchema, ExpectedDelivery: delivery,
	}
	callable := agentRuntimeCallableModelFact{
		ChannelID: selection.ChannelID, Model: selection.Model, DisplayName: "GPT Image 2", Capability: "image",
		BillingMode: "fixed_request", PriceStrategy: "image_resolution", UnitPriceMicrocredits: 250,
		ProviderCapabilities: agentRuntimeGPTImageCapabilitiesForTest(t),
	}
	toolDecision := agentruntime.ToolCallDecision{
		ToolCallID: "media-coordinator", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
		ExpectedDelivery: delivery,
	}
	toolDecision.Arguments = mustMarshalAgentMediaTestJSON(t, proposal)
	frozen, err := svc.freezeAgentMediaGenerationDecisionArguments(scope, configuration, []agentRuntimeCallableModelFact{callable}, &toolDecision)
	if err != nil {
		t.Fatal(err)
	}
	toolDecision.Arguments = frozen
	waitingApproval, err := agentruntime.AdvanceForToolSchema(running.State, agentruntime.RuntimeInput{
		Decision: agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &toolDecision},
	}, agentruntime.ProductionToolSchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CommitAgentRuntimeTransition(scope, running.State, waitingApproval, now); err != nil {
		t.Fatal(err)
	}
	approved, err := agentruntime.ReviewToolApproval(waitingApproval.State, agentruntime.ToolApproval{
		ToolCallID: toolDecision.ToolCallID, ActionVersion: toolDecision.ActionVersion, Decision: agentruntime.ToolApprovalApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CommitAgentRuntimeTransition(scope, waitingApproval.State, approved, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	progress, err := svc.coordinatePendingAgentTool(scope, CoordinateAgentToolInput{
		ToolCallID: toolDecision.ToolCallID, ActionVersion: toolDecision.ActionVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Status != agentruntime.RunWaitingTool || !progress.State.PendingToolStarted {
		t.Fatalf("media coordinator start state = %#v result=%#v", progress.State, progress.State.LastToolResult)
	}
	var task model.Task
	if err := db.Where("operation = ?", agentMediaGenerationOperationForRun(scope.RunID)).First(&task).Error; err != nil {
		t.Fatal(err)
	}
	if task.Audience != model.TaskAudienceInternal {
		t.Fatalf("media task audience = %q", task.Audience)
	}
	resource := seedAgentMediaInputResource(t, db, scope, "media-coordinator-result", "image", "outputs/result.png", "etag-result")
	resultJSON := `{"images":[{"resourceId":"` + resource.ID + `"}]}`
	if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Updates(map[string]interface{}{
		"status": model.TaskStatusSucceeded, "result_json": resultJSON,
	}).Error; err != nil {
		t.Fatal(err)
	}
	progress, err = svc.coordinatePendingAgentTool(scope, CoordinateAgentToolInput{
		ToolCallID: toolDecision.ToolCallID, ActionVersion: toolDecision.ActionVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := svc.repo.AgentToolCallForScope(scope, toolDecision.ToolCallID, toolDecision.ActionVersion)
	if err != nil {
		t.Fatal(err)
	}
	if recorded.Status != agentruntime.ToolCallSucceeded || progress.State.Status != agentruntime.RunRunning {
		t.Fatalf("media coordinator completion = status %q state %q", recorded.Status, progress.State.Status)
	}
	stored, err := svc.repo.MediaCandidateRevisionsInScope(scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].ResourceID != resource.ID {
		t.Fatalf("media candidates = %#v", stored)
	}
}

func TestCurrentAgentMediaGenerationCommandRejectsChangedProviderCapabilities(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeImageModel(t, db, fixture)
	scope := agentRuntimeServiceScope()
	selection := agentruntime.GenerationModelSelection{ChannelID: "runtime-image-channel", Model: "kz_gpt_image2"}
	delivery := exactAgentMediaExpectedDelivery(agentruntime.ArtifactImage)
	proposal := MediaGenerateArguments{
		InputRevisions: []agentruntime.ArtifactRevisionRef{}, GenerationModel: selection, Capability: "image",
		Parameters:        json.RawMessage(`{"prompt":"角色定妆照","aspectRatio":"1:1","resolution":"1K","quality":"medium","count":1}`),
		OutputArtifactKey: "character-portrait", ExpectedOutputSchema: agentMediaCandidateSchema, ExpectedDelivery: delivery,
	}
	call := &agentruntime.ToolCallDecision{
		ToolCallID: "media-capability-drift", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
		ExpectedDelivery: delivery,
	}
	call.Arguments = mustMarshalAgentMediaTestJSON(t, proposal)
	callable := agentRuntimeCallableModelFact{
		ChannelID: selection.ChannelID, Model: selection.Model, DisplayName: "GPT Image 2", Capability: "image",
		BillingMode: "fixed_request", PriceStrategy: "image_resolution", UnitPriceMicrocredits: 250,
		ProviderCapabilities: agentRuntimeGPTImageCapabilitiesForTest(t),
	}
	frozenJSON, err := svc.freezeAgentMediaGenerationDecisionArguments(
		scope,
		agentruntime.RunConfiguration{
			ExecutionMode:    agentruntime.ExecutionGuided,
			GenerationModels: agentruntime.GenerationModelSelections{Image: &selection},
			Skills:           []agentruntime.SkillSelection{},
		},
		[]agentRuntimeCallableModelFact{callable},
		call,
	)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := decodeFrozenAgentMediaGenerationArguments(frozenJSON)
	if err != nil {
		t.Fatal(err)
	}
	quotedCapabilities, err := decodeFrozenMediaCapabilities(frozen.Commercial.ProviderCapabilitiesJSON)
	if err != nil {
		t.Fatal(err)
	}
	quotedCapabilities.MaxImages++
	changedCapabilities, err := canonicalMediaCapabilities(quotedCapabilities)
	if err != nil {
		t.Fatal(err)
	}
	frozen.Commercial.ProviderCapabilitiesJSON = string(changedCapabilities)
	frozen.Commercial.ApprovalFingerprint, err = mediaApprovalFingerprint(scope, frozen.Commercial)
	if err != nil {
		t.Fatal(err)
	}
	frozen.Commercial.QuoteID = mediaQuoteID(frozen.Commercial.ApprovalFingerprint)

	if _, err := svc.currentAgentMediaGenerationCommand(scope, frozen); err == nil {
		t.Fatal("currentAgentMediaGenerationCommand accepted provider capability drift")
	}
}

func agentRuntimeGPTImageCapabilitiesForTest(t *testing.T) *PublicProviderCapabilities {
	t.Helper()
	capabilities := publicProviderModelCapabilities(model.ChannelInterfaceOpenAIImage, "kz_gpt_image2")
	if capabilities == nil {
		t.Fatal("GPT Image 2 provider capabilities are unavailable")
	}
	return capabilities
}

func seedAgentMediaInputResource(
	t *testing.T,
	db *gorm.DB,
	scope agentruntime.Scope,
	id string,
	kind string,
	objectKey string,
	etag string,
) model.Resource {
	t.Helper()
	now := time.Now().UTC()
	resource := model.Resource{
		ID: id, UserID: scope.ActorUserID, Kind: kind, Status: model.ResourceStatusReady,
		Provider: "oss", Endpoint: "oss.example.com", Bucket: "agent-media", ObjectKey: objectKey,
		MimeType: "application/octet-stream", ETag: etag, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	return resource
}

func seedAgentMediaInputRevision(
	t *testing.T,
	svc *Service,
	scope agentruntime.Scope,
	artifactID string,
	artifactKey string,
	resourceID string,
) model.AgentArtifactRevision {
	t.Helper()
	revision, err := svc.repo.AppendArtifactRevisionOnce(scope, artifactID, agentruntime.ArtifactDraft{
		ArtifactKey: artifactKey, Kind: "uploaded_media", SchemaVersion: 1,
		Payload: json.RawMessage(`{"source":"user"}`), ResourceID: resourceID,
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{}, SkillVersions: []agentruntime.SkillSelection{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return *revision
}

func mustMarshalAgentMediaTestJSON(t *testing.T, value interface{}) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
