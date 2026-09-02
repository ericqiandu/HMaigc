package agentruntime

import (
	"errors"
	"testing"
	"time"
)

func TestBuildProductionProgressDoesNotTreatCompletedTaskAsDelivered(t *testing.T) {
	now := time.Date(2026, time.August, 29, 8, 0, 0, 0, time.UTC)
	projection, err := BuildProductionProgress(ProductionProgressFacts{
		GraphVersionID: "graph-version-1",
		GraphVersion:   1,
		ComputedAt:     now,
		Stages: []ProductionProgressStageFacts{{
			StageKey: "render", Status: StageRunning,
			ExpectedDelivery: ExpectedDelivery{
				Kind: DeliveryGeneratedAsset, RequiredArtifacts: []ArtifactKind{ArtifactVideo},
				CompletionCriteria: []DeliveryCriterion{{Fact: DeliveryFactResource, Artifact: ArtifactVideo}},
			},
			Tasks:    []ProductionTaskEvidence{{TaskID: "task-1", Status: "succeeded", BillingOrderID: "billing-1"}},
			Billings: []ProductionBillingEvidence{{BillingOrderID: "billing-1", Status: "settled"}},
			DeliveryEvidence: DeliveryEvidence{Artifacts: []DeliveryArtifact{{
				Kind: ArtifactVideo, ArtifactID: "artifact-1", RevisionID: "revision-1",
				Approved: true, ResourceID: "resource-1", URL: "https://cdn.example/video.mp4",
			}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasProductionBlocker(projection, ProductionBlockerResourceNotReady) {
		t.Fatalf("blockers = %#v, want %q", projection.Blockers, ProductionBlockerResourceNotReady)
	}
	if len(projection.EligibleActions) != 0 {
		t.Fatalf("eligible actions = %#v, want none", projection.EligibleActions)
	}
	if !projection.ComputedAt.Equal(now) {
		t.Fatalf("computed at = %v, want %v", projection.ComputedAt, now)
	}
}

func TestBuildProductionProgressExposesOnlyStructurallyEligibleAction(t *testing.T) {
	projection, err := BuildProductionProgress(ProductionProgressFacts{
		GraphVersionID: "graph-version-2", GraphVersion: 2,
		ComputedAt: time.Date(2026, time.August, 29, 8, 5, 0, 0, time.UTC),
		Stages: []ProductionProgressStageFacts{
			{StageKey: "script", Status: StageCompleted, ExpectedDelivery: answerProgressDelivery(), DeliveryEvidence: DeliveryEvidence{FinalMessage: "done"}},
			{StageKey: "storyboard", Status: StagePlanned, DependsOnStageKeys: []string{"script"}, ExpectedDelivery: answerProgressDelivery()},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if projection.CurrentStageKey != "storyboard" || projection.StageStatus != StagePlanned {
		t.Fatalf("current stage = %q/%q", projection.CurrentStageKey, projection.StageStatus)
	}
	if len(projection.Blockers) != 0 {
		t.Fatalf("blockers = %#v, want none", projection.Blockers)
	}
	if len(projection.EligibleActions) != 1 || projection.EligibleActions[0].Action != ProductionActionExecuteStage {
		t.Fatalf("eligible actions = %#v", projection.EligibleActions)
	}
}

func TestBuildProductionProgressDoesNotCompleteStageWithFailedTask(t *testing.T) {
	projection, err := BuildProductionProgress(ProductionProgressFacts{
		GraphVersionID: "graph-version-failed-task", GraphVersion: 1,
		ComputedAt: time.Date(2026, time.August, 29, 8, 10, 0, 0, time.UTC),
		Stages: []ProductionProgressStageFacts{{
			StageKey: "script", Status: StageCompleted,
			ExpectedDelivery: answerProgressDelivery(), DeliveryEvidence: DeliveryEvidence{FinalMessage: "done"},
			Tasks: []ProductionTaskEvidence{{TaskID: "task-failed", Status: "failed"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if projection.CurrentStageKey != "script" || !hasProductionBlocker(projection, ProductionBlockerTaskFailed) {
		t.Fatalf("progress = %#v, want failed task to keep script current", projection)
	}
	if len(projection.EligibleActions) != 0 {
		t.Fatalf("eligible actions = %#v, want none", projection.EligibleActions)
	}
}

func TestBuildProductionProgressAllowsFailedStageRetryAfterRefund(t *testing.T) {
	projection, err := BuildProductionProgress(ProductionProgressFacts{
		GraphVersionID: "graph-version-retry", GraphVersion: 1,
		ComputedAt: time.Date(2026, time.August, 29, 8, 12, 0, 0, time.UTC),
		Stages: []ProductionProgressStageFacts{{
			StageKey: "script", Status: StageFailed, ExpectedDelivery: answerProgressDelivery(),
			Tasks:    []ProductionTaskEvidence{{TaskID: "task-failed", Status: "failed", BillingOrderID: "billing-refunded"}},
			Billings: []ProductionBillingEvidence{{BillingOrderID: "billing-refunded", Status: "refunded"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.Blockers) != 0 {
		t.Fatalf("blockers = %#v, want none after terminal refund", projection.Blockers)
	}
	if len(projection.EligibleActions) != 1 || projection.EligibleActions[0].Action != ProductionActionExecuteStage {
		t.Fatalf("eligible actions = %#v, want execute_stage", projection.EligibleActions)
	}
}

func TestBuildProductionProgressResourceBlockerFallsBackToStageEvidence(t *testing.T) {
	projection, err := BuildProductionProgress(ProductionProgressFacts{
		GraphVersionID: "graph-version-missing-resource", GraphVersion: 1,
		ComputedAt: time.Date(2026, time.August, 29, 8, 15, 0, 0, time.UTC),
		Stages: []ProductionProgressStageFacts{{
			StageKey: "render", Status: StageRunning,
			ExpectedDelivery: ExpectedDelivery{
				Kind: DeliveryGeneratedAsset, RequiredArtifacts: []ArtifactKind{ArtifactVideo},
				CompletionCriteria: []DeliveryCriterion{{Fact: DeliveryFactResource, Artifact: ArtifactVideo}},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, blocker := range projection.Blockers {
		if blocker.Code != ProductionBlockerResourceNotReady {
			continue
		}
		if len(blocker.EvidenceRefs) != 1 || blocker.EvidenceRefs[0] != (ProductionEvidenceRef{Kind: "stage", ID: "render"}) {
			t.Fatalf("resource blocker refs = %#v, want render stage", blocker.EvidenceRefs)
		}
		return
	}
	t.Fatalf("blockers = %#v, want %q", projection.Blockers, ProductionBlockerResourceNotReady)
}

func TestShotBindingRequiresExactIdentityAndResourceRevision(t *testing.T) {
	err := ValidateShotBindingRevision(ShotBindingRevision{ShotKey: "shot-1", IdentityVersionID: "identity-v2"})
	if !errors.Is(err, ErrProductionEvidenceIncomplete) {
		t.Fatalf("ValidateShotBindingRevision() error = %v, want %v", err, ErrProductionEvidenceIncomplete)
	}
}

func TestCharacterIdentityVersionRejectsDuplicatedContentTruth(t *testing.T) {
	err := ValidateCharacterIdentityVersion(CharacterIdentityVersion{
		CharacterKey: "character-1", Version: 1, CharacterBibleRevisionID: "character-bible-r1",
		ResourceID: "resource-1", DependencyHash: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		LifecycleStatus: ProductionEvidenceCurrent, Payload: "duplicated character bible",
	})
	if !errors.Is(err, ErrProductionEvidenceContentDuplicated) {
		t.Fatalf("ValidateCharacterIdentityVersion() error = %v, want %v", err, ErrProductionEvidenceContentDuplicated)
	}
}

func answerProgressDelivery() ExpectedDelivery {
	return ExpectedDelivery{
		Kind:               DeliveryAnswer,
		CompletionCriteria: []DeliveryCriterion{{Fact: DeliveryFactFinalMessage}},
	}
}

func hasProductionBlocker(projection ProductionNextActionProjection, code ProductionBlockerCode) bool {
	for _, blocker := range projection.Blockers {
		if blocker.Code == code {
			return true
		}
	}
	return false
}
