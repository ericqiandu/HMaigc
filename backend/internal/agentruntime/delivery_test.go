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
			evidence: agentruntime.DeliveryEvidence{CanvasID: "canvas-1", CanvasRevision: 5, CanvasCurrent: true}, want: agentruntime.VerificationRepairable,
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
		CanvasID: "canvas-1", CanvasRevision: 8, CanvasCurrent: true,
		Artifacts: []agentruntime.DeliveryArtifact{{
			Kind: agentruntime.ArtifactVideo, ArtifactID: "video-1", RevisionID: "video-r1",
			ResourceID: "resource-1", URL: "/api/resources/resource-1/file", ResourceReady: true, Approved: true,
		}},
	})
	if complete.Status != agentruntime.VerificationSatisfied {
		t.Fatalf("complete final delivery = %#v", complete)
	}
}

func TestFinalAssemblyDeliveryRequiresSuccessfulTaskReadyResourceAndCurrentRevisions(t *testing.T) {
	t.Parallel()

	expected := agentruntime.ExpectedDelivery{
		Kind:              agentruntime.DeliveryMixed,
		RequiredArtifacts: []agentruntime.ArtifactKind{agentruntime.ArtifactVideo, agentruntime.ArtifactCanvasRevision},
		TargetCanvasID:    "canvas-1",
		CompletionCriteria: []agentruntime.DeliveryCriterion{
			{Fact: agentruntime.DeliveryFactTaskBackedResource, Artifact: agentruntime.ArtifactVideo},
			{Fact: agentruntime.DeliveryFactCanvasRevision},
		},
	}
	complete := agentruntime.DeliveryEvidence{
		CanvasID: "canvas-1", CanvasRevision: 9, CanvasCurrent: true,
		Artifacts: []agentruntime.DeliveryArtifact{{
			Kind: agentruntime.ArtifactVideo, ArtifactID: "final-video", RevisionID: "final-r1",
			ResourceID: "final-resource", URL: "/api/resources/final-resource/file", ResourceReady: true,
			SourceTaskID: "assembly-task", SourceTaskSucceeded: true, CurrentRevision: true,
		}},
	}
	if verification := agentruntime.VerifyDelivery(expected, complete); verification.Status != agentruntime.VerificationSatisfied {
		t.Fatalf("complete assembly delivery = %#v", verification)
	}

	cases := []struct {
		name   string
		mutate func(*agentruntime.DeliveryEvidence)
	}{
		{name: "canvas revision is stale", mutate: func(e *agentruntime.DeliveryEvidence) { e.CanvasCurrent = false }},
		{name: "assembly task is missing", mutate: func(e *agentruntime.DeliveryEvidence) { e.Artifacts[0].SourceTaskID = "" }},
		{name: "assembly task failed", mutate: func(e *agentruntime.DeliveryEvidence) { e.Artifacts[0].SourceTaskSucceeded = false }},
		{name: "artifact revision is stale", mutate: func(e *agentruntime.DeliveryEvidence) { e.Artifacts[0].CurrentRevision = false }},
		{name: "resource is not ready", mutate: func(e *agentruntime.DeliveryEvidence) { e.Artifacts[0].ResourceReady = false }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			evidence := complete
			evidence.Artifacts = append([]agentruntime.DeliveryArtifact(nil), complete.Artifacts...)
			testCase.mutate(&evidence)
			verification := agentruntime.VerifyDelivery(expected, evidence)
			if verification.Status != agentruntime.VerificationRepairable || len(verification.MissingCriteria) != 1 {
				t.Fatalf("incomplete assembly delivery = %#v", verification)
			}
		})
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
