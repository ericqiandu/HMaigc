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

func TestFinalDeliveryRequiresApprovedExactRevisionReadyResourceAndCanvas(t *testing.T) {
	expected := agentruntime.ExpectedDelivery{
		Kind:              agentruntime.DeliveryMixed,
		RequiredArtifacts: []agentruntime.ArtifactKind{agentruntime.ArtifactVideo},
		TargetCanvasID:    "canvas-1",
		CompletionCriteria: []agentruntime.DeliveryCriterion{
			{Fact: agentruntime.DeliveryFactArtifactRevision, Artifact: agentruntime.ArtifactVideo},
			{Fact: agentruntime.DeliveryFactResource, Artifact: agentruntime.ArtifactVideo},
			{Fact: agentruntime.DeliveryFactCanvasRevision},
		},
	}

	incomplete := agentruntime.VerifyDelivery(expected, agentruntime.DeliveryEvidence{
		Artifacts: []agentruntime.DeliveryArtifact{{
			Kind: agentruntime.ArtifactVideo, ArtifactID: "video-1", RevisionID: "video-r1", Approved: true,
		}},
	})
	if incomplete.Status != agentruntime.VerificationRepairable {
		t.Fatalf("incomplete final delivery = %#v", incomplete)
	}
	wantMissing := []agentruntime.DeliveryCriterion{
		{Fact: agentruntime.DeliveryFactResource, Artifact: agentruntime.ArtifactVideo},
		{Fact: agentruntime.DeliveryFactCanvasRevision},
	}
	if len(incomplete.MissingCriteria) != len(wantMissing) {
		t.Fatalf("missing criteria = %#v, want %#v", incomplete.MissingCriteria, wantMissing)
	}
	for index := range wantMissing {
		if incomplete.MissingCriteria[index] != wantMissing[index] {
			t.Fatalf("missing criterion %d = %#v, want %#v", index, incomplete.MissingCriteria[index], wantMissing[index])
		}
	}

	complete := agentruntime.VerifyDelivery(expected, agentruntime.DeliveryEvidence{
		CanvasID: "canvas-1", CanvasRevision: 8,
		Artifacts: []agentruntime.DeliveryArtifact{{
			Kind: agentruntime.ArtifactVideo, ArtifactID: "video-1", RevisionID: "video-r1",
			ResourceID: "resource-1", URL: "/api/resources/resource-1/file", ResourceReady: true, Approved: true,
		}},
	})
	if complete.Status != agentruntime.VerificationSatisfied {
		t.Fatalf("complete final delivery = %#v", complete)
	}
}

func TestFinalDeliveryRejectsUnapprovedRevisionAndPublicationWithoutExactResource(t *testing.T) {
	expected := agentruntime.ExpectedDelivery{
		Kind:              agentruntime.DeliveryGeneratedAsset,
		RequiredArtifacts: []agentruntime.ArtifactKind{agentruntime.ArtifactImage},
		CompletionCriteria: []agentruntime.DeliveryCriterion{
			{Fact: agentruntime.DeliveryFactArtifactRevision, Artifact: agentruntime.ArtifactImage},
			{Fact: agentruntime.DeliveryFactPublication, Artifact: agentruntime.ArtifactImage},
		},
	}
	verification := agentruntime.VerifyDelivery(expected, agentruntime.DeliveryEvidence{
		Artifacts: []agentruntime.DeliveryArtifact{{
			Kind: agentruntime.ArtifactImage, ArtifactID: "image-1", RevisionID: "image-r1",
			ResourceID: "resource-1", URL: "/api/resources/resource-1/file",
		}},
	})
	if verification.Status != agentruntime.VerificationRepairable || len(verification.MissingCriteria) != 2 {
		t.Fatalf("unapproved publication verification = %#v", verification)
	}
}
