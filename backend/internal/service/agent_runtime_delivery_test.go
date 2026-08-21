package service

import (
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

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

func TestCommittedPlanDeliveryArtifactsRejectsIncompleteCommitFacts(t *testing.T) {
	_, err := committedPlanDeliveryArtifacts("canvas-1", []model.AgentProductionArtifact{{
		ID: "image-1", Kind: model.AgentProductionArtifactStoryboardImage,
		Status: model.AgentProductionArtifactSucceeded, CanvasNodeID: "node-image", ResourceID: "resource-image",
	}}, map[string]model.Resource{"resource-image": {ID: "resource-image", Status: model.ResourceStatusReady}})
	if err == nil {
		t.Fatal("expected incomplete committed artifact evidence to fail")
	}
}
