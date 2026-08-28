package service

import (
	"reflect"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

func TestSuccessfulPublicationIDsByRevisionUsesStableFirstSuccessfulPublication(t *testing.T) {
	publications := []model.AgentAssetPublication{
		{ID: "publication-library", ArtifactRevisionID: "revision-1", Status: model.AgentAssetPublicationSucceeded},
		{ID: "publication-project", ArtifactRevisionID: "revision-1", Status: model.AgentAssetPublicationSucceeded},
	}

	actual, err := successfulPublicationIDsByRevision(publications)
	if err != nil {
		t.Fatal(err)
	}
	if actual["revision-1"] != "publication-library" {
		t.Fatalf("publication id = %q, want stable first successful publication", actual["revision-1"])
	}
}

func TestMediaCandidateDeliveryArtifactCarriesExactApprovedResourceAndPublicationFacts(t *testing.T) {
	revision := model.AgentArtifactRevision{
		ID: "candidate-revision-1", ArtifactID: "candidate-artifact-1", ArtifactKey: "final-video",
		Kind: mediaCandidateArtifactKind, SchemaVersion: 1, ResourceID: "resource-video-1",
		ModelRequestIdentity: "provider-request-1",
		PayloadJSON:          `{"candidateKey":"final-video","mediaKind":"video","providerRequestIdentity":"provider-request-1","resourceId":"resource-video-1","sourceTaskId":"task-video-1"}`,
	}
	resource := model.Resource{
		ID: "resource-video-1", Kind: "video", Status: model.ResourceStatusReady,
		Provider: "oss", ObjectKey: "videos/final.mp4", ETag: "etag-final-video",
	}

	actual, err := mediaCandidateDeliveryArtifact(revision, resource, true, "publication-1")
	if err != nil {
		t.Fatal(err)
	}
	want := agentruntime.DeliveryArtifact{
		Kind: agentruntime.ArtifactVideo, ArtifactID: revision.ArtifactID, RevisionID: revision.ID,
		ResourceID: resource.ID, URL: "/api/resources/resource-video-1/file", ResourceReady: true,
		Approved: true, PublicationID: "publication-1",
	}
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("delivery artifact = %#v, want %#v", actual, want)
	}
}

func TestMediaCandidateDeliveryArtifactRejectsPublicationWithoutApproval(t *testing.T) {
	revision := model.AgentArtifactRevision{
		ID: "candidate-revision-1", ArtifactID: "candidate-artifact-1", ArtifactKey: "voiceover",
		Kind: mediaCandidateArtifactKind, SchemaVersion: 1, ResourceID: "resource-audio-1",
		ModelRequestIdentity: "provider-request-audio-1",
		PayloadJSON:          `{"candidateKey":"voiceover","mediaKind":"audio","providerRequestIdentity":"provider-request-audio-1","resourceId":"resource-audio-1","sourceTaskId":"task-audio-1"}`,
	}
	resource := model.Resource{
		ID: "resource-audio-1", Kind: "audio", Status: model.ResourceStatusReady,
		Provider: "oss", ObjectKey: "audio/voiceover.mp3", ETag: "etag-voiceover",
	}

	if _, err := mediaCandidateDeliveryArtifact(revision, resource, false, "publication-1"); err == nil {
		t.Fatal("expected publication without exact approval to fail")
	}
}

func TestCommittedPlanDeliveryArtifactsIncludesResumedPlanFacts(t *testing.T) {
	artifacts := []model.AgentProductionArtifact{
		{ID: "script-1", Kind: model.AgentProductionArtifactScript, Status: model.AgentProductionArtifactCommitted, CanvasNodeID: "node-script"},
		{ID: "image-1", Kind: model.AgentProductionArtifactStoryboardImage, Status: model.AgentProductionArtifactCommitted, CanvasNodeID: "node-image", ResourceID: "resource-image"},
		{ID: "video-1", Kind: model.AgentProductionArtifactVideoClip, Status: model.AgentProductionArtifactCommitted, CanvasNodeID: "node-video", ResourceID: "resource-video"},
	}
	resources := map[string]model.Resource{
		"resource-image": {ID: "resource-image", Status: model.ResourceStatusReady},
		"resource-video": {ID: "resource-video", Status: model.ResourceStatusReady},
	}

	actual, err := committedPlanDeliveryArtifacts("canvas-1", artifacts, resources)
	if err != nil {
		t.Fatal(err)
	}
	want := []agentruntime.DeliveryArtifact{
		{Kind: agentruntime.ArtifactText, URL: "canvas://canvas-1/nodes/node-script"},
		{Kind: agentruntime.ArtifactImage, URL: "/api/resources/resource-image/file"},
		{Kind: agentruntime.ArtifactVideo, URL: "/api/resources/resource-video/file"},
	}
	if len(actual) != len(want) {
		t.Fatalf("delivery artifacts = %#v", actual)
	}
	for index := range want {
		if actual[index] != want[index] {
			t.Fatalf("delivery artifact %d = %#v, want %#v", index, actual[index], want[index])
		}
	}
}

func TestCommittedPlanDeliveryArtifactsAcceptsVideoOnlyPlan(t *testing.T) {
	artifacts := []model.AgentProductionArtifact{
		{ID: "script-1", Kind: model.AgentProductionArtifactScript, Status: model.AgentProductionArtifactCommitted, CanvasNodeID: "node-script"},
		{ID: "video-1", Kind: model.AgentProductionArtifactVideoClip, Status: model.AgentProductionArtifactCommitted, CanvasNodeID: "node-video", ResourceID: "resource-video"},
	}
	resources := map[string]model.Resource{
		"resource-video": {ID: "resource-video", Status: model.ResourceStatusReady},
	}

	actual, err := committedPlanDeliveryArtifacts("canvas-video-only", artifacts, resources)
	if err != nil {
		t.Fatal(err)
	}
	want := []agentruntime.DeliveryArtifact{
		{Kind: agentruntime.ArtifactText, URL: "canvas://canvas-video-only/nodes/node-script"},
		{Kind: agentruntime.ArtifactVideo, URL: "/api/resources/resource-video/file"},
	}
	if len(actual) != len(want) {
		t.Fatalf("video-only delivery artifacts = %#v", actual)
	}
	for index := range want {
		if actual[index] != want[index] {
			t.Fatalf("video-only delivery artifact %d = %#v, want %#v", index, actual[index], want[index])
		}
	}
}

func TestCommittedPlanDeliveryArtifactsRejectsIncompleteCommitFacts(t *testing.T) {
	_, err := committedPlanDeliveryArtifacts("canvas-1", []model.AgentProductionArtifact{{
		ID: "image-1", Kind: model.AgentProductionArtifactStoryboardImage,
		Status: model.AgentProductionArtifactSucceeded, CanvasNodeID: "node-image", ResourceID: "resource-image",
	}}, map[string]model.Resource{"resource-image": {ID: "resource-image", Status: model.ResourceStatusReady}})
	if err == nil {
		t.Fatal("expected incomplete committed artifact evidence to fail")
	}
}
