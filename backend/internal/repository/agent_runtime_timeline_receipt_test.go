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

func TestAgentTimelineToolResultPublishesExactMediaGenerateReceipt(t *testing.T) {
	call := &agentruntime.ToolCallDecision{
		ToolCallID: "call-media", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
	}
	receipt := agentruntime.MediaGenerateResult{
		TaskID: "task-1", BillingOrderID: "billing-1", MediaKind: agentruntime.MediaKindImage,
		ClientRequestID: "request-1",
		Resources: []agentruntime.MediaGeneratedResourceResult{{
			ResourceID: "resource-1", Kind: agentruntime.MediaKindImage, URL: "/api/resources/resource-1/file",
		}},
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
		t.Fatal("media generate timeline mutation is missing")
	}
	var content struct {
		ToolName agentruntime.ToolName            `json:"toolName"`
		Output   agentruntime.MediaGenerateResult `json:"output"`
	}
	if err := json.Unmarshal(mutation.ContentJSON, &content); err != nil {
		t.Fatal(err)
	}
	if content.ToolName != agentruntime.ToolMediaGenerate || content.Output.TaskID != receipt.TaskID ||
		content.Output.BillingOrderID != receipt.BillingOrderID || len(content.Output.Resources) != 1 ||
		content.Output.Resources[0].ResourceID != receipt.Resources[0].ResourceID ||
		content.Output.Resources[0].URL != receipt.Resources[0].URL {
		t.Fatalf("media generate timeline receipt = %#v", content)
	}
}

func TestAgentTimelineToolResultRejectsMalformedMediaGenerateReceipt(t *testing.T) {
	call := &agentruntime.ToolCallDecision{
		ToolCallID: "call-media", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
	}
	mutation, err := agentTimelineMutationForEvent(
		"run-1",
		agentruntime.RuntimeState{PendingToolCall: call},
		agentruntime.RuntimeState{LastToolResult: &agentruntime.ToolResult{
			ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion, Succeeded: true,
			Output: json.RawMessage(`{"taskId":"task-1","billingOrderId":"billing-1","mediaKind":"image","clientRequestId":"request-1","resources":[]}`),
		}},
		agentruntime.EventToolResult,
		4,
	)
	if err == nil || mutation != nil {
		t.Fatalf("malformed media receipt mutation = %#v, err = %v", mutation, err)
	}
}

func TestAgentTimelineToolResultPublishesExactVisionAnalyzeReceipt(t *testing.T) {
	call := &agentruntime.ToolCallDecision{
		ToolCallID: "call-vision", ToolName: agentruntime.ToolVisionAnalyze, ActionVersion: 1,
	}
	receipt := agentruntime.VisionAnalyzeResult{
		TaskID: "task-vision-1", BillingOrderID: "billing-vision-1",
		ModelRecordID: "vision-record-1", ModelKey: "deepseek-v4-flash-vision-exp",
		ClientRequestID: "vision-request-1", SourceResourceIDs: []string{"resource-1"},
		Detail: agentruntime.VisionDetailLow, Analysis: "画面以蓝紫色为主，比例约为 4:1。",
		Usage: agentruntime.VisionTokenUsage{InputTokens: 215, CachedTokens: 0, OutputTokens: 42},
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
		t.Fatal("vision analyze timeline mutation is missing")
	}
	var content struct {
		ToolName agentruntime.ToolName            `json:"toolName"`
		Output   agentruntime.VisionAnalyzeResult `json:"output"`
	}
	if err := json.Unmarshal(mutation.ContentJSON, &content); err != nil {
		t.Fatal(err)
	}
	if content.ToolName != agentruntime.ToolVisionAnalyze || content.Output.TaskID != receipt.TaskID ||
		content.Output.BillingOrderID != receipt.BillingOrderID || content.Output.Analysis != receipt.Analysis ||
		content.Output.Usage.InputTokens != receipt.Usage.InputTokens || content.Output.Usage.OutputTokens != receipt.Usage.OutputTokens {
		t.Fatalf("vision analyze timeline receipt = %#v", content)
	}
}

func TestAgentTimelineToolResultRejectsMalformedVisionAnalyzeReceipt(t *testing.T) {
	call := &agentruntime.ToolCallDecision{
		ToolCallID: "call-vision", ToolName: agentruntime.ToolVisionAnalyze, ActionVersion: 1,
	}
	mutation, err := agentTimelineMutationForEvent(
		"run-1",
		agentruntime.RuntimeState{PendingToolCall: call},
		agentruntime.RuntimeState{LastToolResult: &agentruntime.ToolResult{
			ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion, Succeeded: true,
			Output: json.RawMessage(`{"taskId":"task-vision-1","analysis":""}`),
		}},
		agentruntime.EventToolResult,
		4,
	)
	if err == nil || mutation != nil {
		t.Fatalf("malformed vision receipt mutation = %#v, err = %v", mutation, err)
	}
}
