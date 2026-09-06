package agentruntime_test

import (
	"infinite-canvas/backend/internal/agentruntime"
	"testing"
)

func TestMixedProjectAnswerDoesNotRequireCanvas(t *testing.T) {
	expected := agentruntime.ExpectedDelivery{Kind: agentruntime.DeliveryMixed, TargetProjectID: "project-1", CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactProjectRevision}, {Fact: agentruntime.DeliveryFactFinalMessage}}}
	if err := expected.Validate(); err != nil {
		t.Fatal(err)
	}
	result := agentruntime.VerifyDelivery(expected, agentruntime.DeliveryEvidence{ProjectID: "project-1", ProjectRevision: 2, ProjectCurrent: true, FinalMessage: "done"})
	if result.Status != agentruntime.VerificationSatisfied {
		t.Fatalf("valid project+answer rejected: %#v", result)
	}
	expected.TargetProjectID = ""
	if expected.Validate() == nil {
		t.Fatal("missing project target accepted")
	}
}
