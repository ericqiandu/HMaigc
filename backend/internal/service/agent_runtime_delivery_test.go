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
