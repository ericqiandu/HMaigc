package repository

import (
	"encoding/json"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
)

func TestAgentTimelineToolResultPublishesExactCanvasApplyOpsReceipt(t *testing.T) {
	call := &agentruntime.ToolCallDecision{
		ToolCallID: "call-apply", ToolName: agentruntime.ToolCanvasApplyOps, ActionVersion: 1,
	}
	receipt := agentruntime.CanvasApplyOpsResult{
		CanvasID: "canvas-1", BaseRevision: 7, CommittedRevision: 8,
		ClientMutationID: "mutation-1", ProposalHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AppliedOperationIDs: []string{"operation-1"},
		Evidence: agentruntime.CanvasApplyOpsEvidence{
			AddedNodeIDs: []string{"node-1"}, UpdatedNodeIDs: []string{}, DeletedNodeIDs: []string{},
			UpsertedConnectionIDs: []string{}, DeletedConnectionIDs: []string{}, SelectedNodeIDs: []string{"node-1"},
			ViewportApplied: false,
		},
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	mutation, err := agentTimelineMutationForEvent(
		"run-1",
		agentruntime.RuntimeState{PendingToolCall: call},
		agentruntime.RuntimeState{LastToolResult: &agentruntime.ToolResult{
			ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion, Succeeded: true, Output: encoded,
		}},
		agentruntime.EventToolResult,
		4,
	)
	if err != nil {
		t.Fatal(err)
	}
	if mutation == nil {
		t.Fatal("canvas apply ops timeline mutation is missing")
	}
	var content struct {
		ToolName agentruntime.ToolName             `json:"toolName"`
		Output   agentruntime.CanvasApplyOpsResult `json:"output"`
	}
	if err := json.Unmarshal(mutation.ContentJSON, &content); err != nil {
		t.Fatal(err)
	}
	if content.ToolName != agentruntime.ToolCanvasApplyOps || content.Output.ProposalHash != receipt.ProposalHash ||
		content.Output.CommittedRevision != receipt.CommittedRevision || len(content.Output.Evidence.AddedNodeIDs) != 1 ||
		content.Output.Evidence.AddedNodeIDs[0] != "node-1" {
		t.Fatalf("canvas apply ops timeline receipt = %#v", content)
	}
}

func TestAgentTimelineToolResultRejectsMalformedCanvasApplyOpsReceipt(t *testing.T) {
	call := &agentruntime.ToolCallDecision{
		ToolCallID: "call-apply", ToolName: agentruntime.ToolCanvasApplyOps, ActionVersion: 1,
	}
	mutation, err := agentTimelineMutationForEvent(
		"run-1",
		agentruntime.RuntimeState{PendingToolCall: call},
		agentruntime.RuntimeState{LastToolResult: &agentruntime.ToolResult{
			ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion, Succeeded: true,
			Output: json.RawMessage(`{"canvasId":"canvas-1"}`),
		}},
		agentruntime.EventToolResult,
		4,
	)
	if err == nil || mutation != nil {
		t.Fatalf("malformed receipt mutation = %#v, err = %v", mutation, err)
	}
}
