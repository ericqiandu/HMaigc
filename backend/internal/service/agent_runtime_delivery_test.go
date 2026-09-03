package service

import (
	"encoding/json"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

func TestCurrentCanvasApplyOpsDeliveryAcceptsExactApprovedReceipt(t *testing.T) {
	call := currentCanvasApplyOpsDeliveryCall(t)

	result, err := currentCanvasApplyOpsDelivery(agentRuntimeServiceScope(), call)
	if err != nil {
		t.Fatal(err)
	}
	if result.CanvasID != "runtime-canvas" || result.BaseRevision != 4 || result.CommittedRevision != 5 {
		t.Fatalf("canvas delivery result = %#v", result)
	}
	if len(result.AppliedOperationIDs) != 1 || result.AppliedOperationIDs[0] != "operation-select" {
		t.Fatalf("applied operation ids = %#v", result.AppliedOperationIDs)
	}
}

func TestCurrentCanvasApplyOpsDeliveryRejectsApprovalHashMismatch(t *testing.T) {
	call := currentCanvasApplyOpsDeliveryCall(t)
	call.ApprovalProposalHash = strings.Repeat("b", 64)

	if _, err := currentCanvasApplyOpsDelivery(agentRuntimeServiceScope(), call); err == nil {
		t.Fatal("expected mismatched approval hash to fail")
	}
}

func TestCanvasPayloadBindsGeneratedResourceOnlyWithExactVisibleMediaFacts(t *testing.T) {
	artifact := agentruntime.DeliveryArtifact{
		Kind: agentruntime.ArtifactImage, ResourceID: "resource-1", URL: "/api/resources/resource-1/file",
		TargetCanvasNodeID: "image-node-1",
	}
	expectedPrompt := "雨夜纸船"
	bound := `{"nodes":[{"id":"image-node-1","type":"image","title":"主视觉","position":{"x":0,"y":0},"width":512,"height":512,"metadata":{"content":"/api/resources/resource-1/file","storageKey":"resource:resource-1","status":"success","prompt":"雨夜纸船","composerContent":"雨夜纸船"}}],"connections":[]}`
	if !canvasPayloadBindsGeneratedResource(bound, artifact, expectedPrompt) {
		t.Fatal("exact generated media binding was not recognized")
	}
	for _, payload := range []string{
		`{"nodes":[{"id":"image-node-1","type":"image","title":"主视觉","position":{"x":0,"y":0},"width":512,"height":512,"metadata":{"content":"/api/resources/resource-1/file","storageKey":"resource:resource-1","status":"success"}}],"connections":[]}`,
		`{"nodes":[{"id":"image-node-1","type":"image","title":"主视觉","position":{"x":0,"y":0},"width":512,"height":512,"metadata":{"content":"/api/resources/resource-1/file","storageKey":"resource:resource-1","status":"success","prompt":"雨夜纸船"}}],"connections":[]}`,
		`{"nodes":[{"id":"image-node-1","type":"image","title":"主视觉","position":{"x":0,"y":0},"width":512,"height":512,"metadata":{"content":"/api/resources/resource-1/file","storageKey":"resource:resource-1","status":"success","prompt":"晴日纸船"}}],"connections":[]}`,
		`{"nodes":[{"id":"image-node-1","type":"image","title":"主视觉","position":{"x":0,"y":0},"width":512,"height":512,"metadata":{"content":"/api/resources/resource-1/file","storageKey":"resource:resource-1","status":"success","prompt":"雨夜纸船","composerContent":""}}],"connections":[]}`,
		`{"nodes":[{"id":"image-node-1","type":"image","title":"主视觉","position":{"x":0,"y":0},"width":512,"height":512,"metadata":{"content":"/api/resources/resource-1/file","storageKey":"resource:resource-1","status":"success","prompt":"雨夜纸船","composerContent":"晴日纸船"}}],"connections":[]}`,
		`{"nodes":[{"id":"image-node-1","type":"image","title":"主视觉","position":{"x":0,"y":0},"width":512,"height":512,"metadata":{"content":"/api/resources/resource-1/file","storageKey":"resource:resource-1","status":"loading"}}],"connections":[]}`,
		`{"nodes":[{"id":"image-node-1","type":"image","title":"主视觉","position":{"x":0,"y":0},"width":512,"height":512,"metadata":{"content":"/api/resources/resource-other/file","storageKey":"resource:resource-1","status":"success"}}],"connections":[]}`,
		`{"nodes":[{"id":"image-node-1","type":"image","title":"主视觉","position":{"x":0,"y":0},"width":512,"height":512,"metadata":{"content":"/api/resources/resource-1/file","storageKey":"resource:resource-other","status":"success"}}],"connections":[]}`,
		`{"nodes":[{"id":"other-node","type":"image","title":"主视觉","position":{"x":0,"y":0},"width":512,"height":512,"metadata":{"content":"/api/resources/resource-1/file","storageKey":"resource:resource-1","status":"success"}}],"connections":[]}`,
	} {
		if canvasPayloadBindsGeneratedResource(payload, artifact, expectedPrompt) {
			t.Fatalf("invalid generated media binding was accepted: %s", payload)
		}
	}
}

func TestCanvasPayloadCreatedTextDeliveryArtifactsUseOnlyCurrentApprovedNodes(t *testing.T) {
	payload := `{"nodes":[
		{"id":"script-node-1","type":"script","metadata":{"content":"五秒单镜头脚本","status":"idle"}},
		{"id":"text-node-2","type":"text","metadata":{"content":"分镜说明","status":"success"}},
		{"id":"image-node-1","type":"image","metadata":{"content":"/api/resources/image/file","status":"success"}},
		{"id":"failed-text-node","type":"text","metadata":{"content":"失败草稿","status":"error"}}
	],"connections":[]}`
	candidates := map[string]struct{}{
		"script-node-1":    {},
		"image-node-1":     {},
		"failed-text-node": {},
	}

	artifacts, err := canvasPayloadCreatedTextDeliveryArtifacts(payload, candidates, 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("text delivery artifacts = %#v", artifacts)
	}
	artifact := artifacts[0]
	if artifact.Kind != agentruntime.ArtifactText || artifact.ArtifactID != "script-node-1" ||
		artifact.RevisionID != "canvas-revision-6" || !artifact.Approved || !artifact.CurrentRevision ||
		artifact.ResourceID != "" || artifact.URL != "" {
		t.Fatalf("text delivery artifact = %#v", artifact)
	}
}

func TestReconcileCanvasDeliveryEvidenceAcceptsNewerCurrentRevision(t *testing.T) {
	evidence := agentruntime.DeliveryEvidence{
		CanvasID:       "runtime-canvas",
		CanvasRevision: 17,
		Artifacts: []agentruntime.DeliveryArtifact{{
			Kind: agentruntime.ArtifactVideo, ResourceID: "resource-1", URL: "/api/resources/resource-1/file",
			SourceTaskID: "video-task-1", TargetCanvasNodeID: "video-node-1",
		}},
	}
	project := model.CanvasProject{
		ID: "runtime-canvas", UserID: "runtime-user", ProjectID: "runtime-project", Revision: 18,
		PayloadJSON: `{"nodes":[{"id":"video-node-1","type":"video","metadata":{"content":"/api/resources/resource-1/file","storageKey":"resource:resource-1","status":"success","prompt":"镜头推进","composerContent":"镜头推进","assetId":"asset-1"}}],"connections":[]}`,
	}

	if err := reconcileCanvasDeliveryEvidence(&evidence, project, agentRuntimeServiceScope(), map[string]string{"video-task-1": "镜头推进"}); err != nil {
		t.Fatal(err)
	}
	if !evidence.CanvasCurrent || evidence.CanvasRevision != 18 {
		t.Fatalf("canvas evidence was not advanced to current revision: %#v", evidence)
	}
	if len(evidence.Artifacts) != 1 || !evidence.Artifacts[0].CanvasBound {
		t.Fatalf("current generated resource binding was not preserved: %#v", evidence.Artifacts)
	}
}

func TestReconcileCanvasDeliveryEvidenceRejectsRevisionRegression(t *testing.T) {
	evidence := agentruntime.DeliveryEvidence{CanvasID: "runtime-canvas", CanvasRevision: 18}
	project := model.CanvasProject{
		ID: "runtime-canvas", UserID: "runtime-user", ProjectID: "runtime-project", Revision: 17,
	}

	if err := reconcileCanvasDeliveryEvidence(&evidence, project, agentRuntimeServiceScope(), map[string]string{}); err == nil {
		t.Fatal("expected a current project revision older than the committed receipt to fail")
	}
}

func currentCanvasApplyOpsDeliveryCall(t *testing.T) model.AgentToolCall {
	t.Helper()
	proposalHash := strings.Repeat("a", 64)
	arguments, err := json.Marshal(agentruntime.CanvasApplyOpsArguments{
		CanvasID: "runtime-canvas", BaseRevision: 4, ClientMutationID: "mutation-select",
		Operations: []agentruntime.CanvasOperation{{
			OperationID: "operation-select", Type: agentruntime.CanvasOperationSelectNodes, NodeIDs: []string{},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := json.Marshal(agentruntime.CanvasApplyOpsResult{
		CanvasID: "runtime-canvas", BaseRevision: 4, CommittedRevision: 5,
		ClientMutationID: "mutation-select", ProposalHash: proposalHash,
		AppliedOperationIDs: []string{"operation-select"},
		Evidence: agentruntime.CanvasApplyOpsEvidence{
			AddedNodeIDs: []string{}, UpdatedNodeIDs: []string{}, DeletedNodeIDs: []string{},
			UpsertedConnectionIDs: []string{}, DeletedConnectionIDs: []string{}, SelectedNodeIDs: []string{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return model.AgentToolCall{
		ToolName: string(agentruntime.ToolCanvasApplyOps), Status: agentruntime.ToolCallSucceeded,
		InputJSON: string(arguments), OutputJSON: string(result), ApprovalProposalHash: proposalHash,
	}
}
