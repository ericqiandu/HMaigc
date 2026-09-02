package service

import (
	"encoding/json"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
)

func TestStampAgentCanvasPlaceholderProvenanceBeforeApproval(t *testing.T) {
	decision := agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall,
		ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "create-media-placeholders", ToolName: agentruntime.ToolCanvasApplyOps, ActionVersion: 1,
			Arguments: json.RawMessage(`{"canvasId":"runtime-canvas","baseRevision":7,"clientMutationId":"create-placeholders","operations":[{"operationId":"add-image","type":"add_node","node":{"id":"image-node-1","type":"image","title":"关键帧","position":{"x":0,"y":0},"width":480,"height":270,"metadata":{"status":"loading","prompt":"雨夜纸船"}}},{"operationId":"update-video","type":"update_node","nodeId":"video-node-1","patch":{"metadata":{"status":"loading","prompt":"纸船被风吹动"}}},{"operationId":"add-text","type":"add_node","node":{"id":"text-node-1","type":"text","title":"脚本","position":{"x":0,"y":320},"width":480,"height":180,"metadata":{"status":"idle"}}}]}`),
		},
	}

	stamped, err := stampAgentCanvasPlaceholderProvenance(agentRuntimeServiceScope(), decision)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := agentruntime.DecodeCapabilityArguments(stamped.ToolCall.ToolName, stamped.ToolCall.Arguments)
	if err != nil {
		t.Fatal(err)
	}
	arguments := decoded.(agentruntime.CanvasApplyOpsArguments)
	var imageMetadata, videoPatch, videoMetadata, textMetadata map[string]json.RawMessage
	if err := json.Unmarshal(arguments.Operations[0].Node.Metadata, &imageMetadata); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(arguments.Operations[1].Patch, &videoPatch); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(videoPatch["metadata"], &videoMetadata); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(arguments.Operations[2].Node.Metadata, &textMetadata); err != nil {
		t.Fatal(err)
	}
	var runID, videoRunID string
	if err := json.Unmarshal(imageMetadata["agentRunId"], &runID); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(videoMetadata["agentRunId"], &videoRunID); err != nil {
		t.Fatal(err)
	}
	if runID != agentRuntimeServiceScope().RunID {
		t.Fatalf("agentRunId = %q", runID)
	}
	if videoRunID != agentRuntimeServiceScope().RunID {
		t.Fatalf("video agentRunId = %q", videoRunID)
	}
	if _, exists := textMetadata["agentRunId"]; exists {
		t.Fatal("non-media idle node received Agent placeholder provenance")
	}
}
