package agentruntime_test

import (
	"errors"
	"reflect"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
)

func TestRuntimeV4ProductionContractIsTheOnlyCurrentContract(t *testing.T) {
	if agentruntime.CurrentRuntimeVersion != 4 || agentruntime.CurrentPolicyVersion != 4 || agentruntime.CurrentToolSchemaVersion != 5 {
		t.Fatalf("current runtime did not hard cut to production: runtime=%d policy=%d tools=%d",
			agentruntime.CurrentRuntimeVersion, agentruntime.CurrentPolicyVersion, agentruntime.CurrentToolSchemaVersion)
	}
	if agentruntime.ProductionRuntimeVersion != 4 || agentruntime.ProductionPolicyVersion != 4 || agentruntime.ProductionToolSchemaVersion != 5 ||
		agentruntime.CurrentProductionSchemaVersion != 2 || agentruntime.ProductionAgentUIProtocolVersion != 4 {
		t.Fatalf("staged production contract mismatch: runtime=%d policy=%d tools=%d production=%d ui=%d",
			agentruntime.ProductionRuntimeVersion, agentruntime.ProductionPolicyVersion, agentruntime.ProductionToolSchemaVersion,
			agentruntime.CurrentProductionSchemaVersion, agentruntime.ProductionAgentUIProtocolVersion)
	}
	if agentruntime.LegacyRuntimeVersion != 2 || agentruntime.LegacyPolicyVersion != 2 || agentruntime.LegacyToolSchemaVersion != 3 {
		t.Fatalf("legacy terminal history contract changed: runtime=%d policy=%d tools=%d",
			agentruntime.LegacyRuntimeVersion, agentruntime.LegacyPolicyVersion, agentruntime.LegacyToolSchemaVersion)
	}
}

func validProductionStage(stageKey string, dependencies ...string) agentruntime.ProductionStageDraft {
	return agentruntime.ProductionStageDraft{
		StageKey:           stageKey,
		SpecialistKey:      agentruntime.SpecialistNarrative,
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

func TestValidateProductionGraphRejectsCycle(t *testing.T) {
	draft := agentruntime.ProductionGraphDraft{
		GraphKey: "graph-1",
		Stages: []agentruntime.ProductionStageDraft{
			validProductionStage("a", "b"),
			validProductionStage("b", "a"),
		},
	}

	err := agentruntime.ValidateProductionGraph(draft)
	if !errors.Is(err, agentruntime.ErrProductionGraphCycle) {
		t.Fatalf("cyclic production graph error = %v, want %v", err, agentruntime.ErrProductionGraphCycle)
	}
}

func TestValidateProductionGraphRejectsDuplicateStageKeys(t *testing.T) {
	draft := agentruntime.ProductionGraphDraft{
		GraphKey: "graph-1",
		Stages: []agentruntime.ProductionStageDraft{
			validProductionStage("script"),
			validProductionStage("script"),
		},
	}

	if err := agentruntime.ValidateProductionGraph(draft); !errors.Is(err, agentruntime.ErrProductionGraphInvalid) {
		t.Fatalf("duplicate stage error = %v, want %v", err, agentruntime.ErrProductionGraphInvalid)
	}
}

func TestApproveStageRequiresExactReviewRevision(t *testing.T) {
	stage := agentruntime.ProductionStageState{
		StageKey:         "script",
		Status:           agentruntime.StageAwaitingReview,
		Version:          4,
		ReviewRevisionID: "rev-2",
	}

	_, err := agentruntime.TransitionProductionStage(stage, agentruntime.StageReviewCommand{
		StageVersion:    4,
		RevisionID:      "rev-1",
		Decision:        agentruntime.StageReviewApprove,
		ClientRequestID: "request-1",
	})
	if !errors.Is(err, agentruntime.ErrStageApprovalRevisionMismatch) {
		t.Fatalf("stage approval error = %v, want %v", err, agentruntime.ErrStageApprovalRevisionMismatch)
	}
}

func TestTransitionProductionStageAppliesReviewDecision(t *testing.T) {
	stage := agentruntime.ProductionStageState{
		StageKey:         "script",
		Status:           agentruntime.StageAwaitingReview,
		Version:          4,
		ReviewRevisionID: "rev-2",
	}

	approved, err := agentruntime.TransitionProductionStage(stage, agentruntime.StageReviewCommand{
		StageVersion:    4,
		RevisionID:      "rev-2",
		Decision:        agentruntime.StageReviewApprove,
		ClientRequestID: "request-1",
	})
	if err != nil {
		t.Fatalf("approve exact review revision: %v", err)
	}
	if approved.Status != agentruntime.StageApproved || approved.Version != 5 || approved.ReviewRevisionID != "rev-2" {
		t.Fatalf("approved stage = %+v, want approved version 5 on rev-2", approved)
	}

	revisionRequested, err := agentruntime.TransitionProductionStage(stage, agentruntime.StageReviewCommand{
		StageVersion:    4,
		RevisionID:      "rev-2",
		Decision:        agentruntime.StageReviewRequestRevision,
		ClientRequestID: "request-2",
		Comment:         "请缩短第二场。",
	})
	if err != nil {
		t.Fatalf("request stage revision: %v", err)
	}
	if revisionRequested.Status != agentruntime.StageRunning || revisionRequested.Version != 5 || revisionRequested.ReviewRevisionID != "" {
		t.Fatalf("revision-requested stage = %+v, want running version 5 without review revision", revisionRequested)
	}
}

func TestValidateProductionStageStatusTransitionRejectsLifecycleJumps(t *testing.T) {
	for _, transition := range []struct {
		current agentruntime.ProductionStageStatus
		next    agentruntime.ProductionStageStatus
	}{
		{current: agentruntime.StagePlanned, next: agentruntime.StageRunning},
		{current: agentruntime.StageRunning, next: agentruntime.StageAwaitingReview},
		{current: agentruntime.StageApproved, next: agentruntime.StageCompleted},
		{current: agentruntime.StageCompleted, next: agentruntime.StageStale},
		{current: agentruntime.StageStale, next: agentruntime.StageRunning},
	} {
		if err := agentruntime.ValidateProductionStageStatusTransition(transition.current, transition.next); err != nil {
			t.Fatalf("valid transition %s -> %s: %v", transition.current, transition.next, err)
		}
	}

	for _, transition := range []struct {
		current agentruntime.ProductionStageStatus
		next    agentruntime.ProductionStageStatus
	}{
		{current: agentruntime.StagePlanned, next: agentruntime.StageCompleted},
		{current: agentruntime.StageCompleted, next: agentruntime.StageRunning},
		{current: agentruntime.StageStopped, next: agentruntime.StageRunning},
	} {
		if err := agentruntime.ValidateProductionStageStatusTransition(transition.current, transition.next); !errors.Is(err, agentruntime.ErrProductionStageTransitionInvalid) {
			t.Fatalf("invalid transition %s -> %s error = %v", transition.current, transition.next, err)
		}
	}
}

func TestStaleDependentStagesIsTransitive(t *testing.T) {
	draft := agentruntime.ProductionGraphDraft{
		GraphKey: "graph-1",
		Stages: []agentruntime.ProductionStageDraft{
			validProductionStage("script"),
			validProductionStage("storyboard", "script"),
			validProductionStage("video", "storyboard"),
			validProductionStage("audio"),
		},
	}

	stale, err := agentruntime.StaleDependentStages(draft, "script")
	if err != nil {
		t.Fatalf("calculate stale dependents: %v", err)
	}
	want := []string{"storyboard", "video"}
	if !reflect.DeepEqual(stale, want) {
		t.Fatalf("stale stages = %v, want %v", stale, want)
	}
}
