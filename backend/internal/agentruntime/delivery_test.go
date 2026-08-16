package agentruntime_test

import (
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
)

func TestVerifyDeliveryUsesOnlyStructuredFacts(t *testing.T) {
	cases := []struct {
		name     string
		expected agentruntime.ExpectedDelivery
		evidence agentruntime.DeliveryEvidence
		want     agentruntime.VerificationStatus
	}{
		{
			name:     "answer satisfied",
			expected: agentruntime.ExpectedDelivery{Kind: agentruntime.DeliveryAnswer, CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}}},
			evidence: agentruntime.DeliveryEvidence{FinalMessage: "真实答复"}, want: agentruntime.VerificationSatisfied,
		},
		{
			name:     "canvas change missing",
			expected: agentruntime.ExpectedDelivery{Kind: agentruntime.DeliveryCanvasChange, TargetCanvasID: "canvas-1", CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactCanvasRevision}}},
			evidence: agentruntime.DeliveryEvidence{}, want: agentruntime.VerificationRepairable,
		},
		{
			name:     "generated image satisfied",
			expected: agentruntime.ExpectedDelivery{Kind: agentruntime.DeliveryGeneratedAsset, RequiredArtifacts: []agentruntime.ArtifactKind{agentruntime.ArtifactImage}, CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactArtifact, Artifact: agentruntime.ArtifactImage}}},
			evidence: agentruntime.DeliveryEvidence{Artifacts: []agentruntime.DeliveryArtifact{{Kind: agentruntime.ArtifactImage, URL: "https://cdn.example.com/result.png"}}}, want: agentruntime.VerificationSatisfied,
		},
		{
			name:     "mixed requires both facts",
			expected: agentruntime.ExpectedDelivery{Kind: agentruntime.DeliveryMixed, RequiredArtifacts: []agentruntime.ArtifactKind{agentruntime.ArtifactVideo}, TargetCanvasID: "canvas-1", CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactCanvasRevision}, {Fact: agentruntime.DeliveryFactArtifact, Artifact: agentruntime.ArtifactVideo}}},
			evidence: agentruntime.DeliveryEvidence{CanvasID: "canvas-1", CanvasRevision: 5}, want: agentruntime.VerificationRepairable,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			actual := agentruntime.VerifyDelivery(test.expected, test.evidence)
			if actual.Status != test.want {
				t.Fatalf("verification = %#v, want %s", actual, test.want)
			}
		})
	}
}

func TestVerifyDeliveryRejectsInvalidContracts(t *testing.T) {
	invalid := []agentruntime.ExpectedDelivery{
		{},
		{Kind: agentruntime.DeliveryAnswer},
		{Kind: agentruntime.DeliveryGeneratedAsset, CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactArtifact}}},
		{Kind: agentruntime.DeliveryCanvasChange, CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactCanvasRevision}}},
		{Kind: agentruntime.DeliveryAnswer, TargetCanvasID: "canvas-1", CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactCanvasRevision}}},
		{Kind: agentruntime.DeliveryCanvasChange, TargetCanvasID: "canvas-1", CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}}},
		{Kind: agentruntime.DeliveryGeneratedAsset, CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactArtifact, Artifact: agentruntime.ArtifactImage}}},
	}
	for _, expected := range invalid {
		actual := agentruntime.VerifyDelivery(expected, agentruntime.DeliveryEvidence{})
		if actual.Status != agentruntime.VerificationFailed {
			t.Fatalf("invalid contract verification = %#v", actual)
		}
	}
}
