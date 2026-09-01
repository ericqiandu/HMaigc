package repository

import (
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

func TestValidateAgentCanvasChangeInputRejectsCrossScopeAndReceiptMismatch(t *testing.T) {
	valid := AgentCanvasChangeInput{
		Scope: agentruntime.Scope{
			TenantKind: agentruntime.TenantPersonal, TenantID: "actor-1", ActorUserID: "actor-1",
			DomainProjectID: "project-1", CanvasID: "canvas-1", ThreadID: "thread-1", RunID: "run-1",
			Access: agentruntime.AccessGrant{Level: agentruntime.AccessEditor, SubscriptionActive: true},
		},
		ToolCallID: "call-1", ActionVersion: 1, ProposalHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		CanvasID: "canvas-1", ChangeID: "change-1", ActorUserID: "actor-1", BaseRevision: 7,
		ClientMutationID: "mutation-1", ChangePayloadJSON: `{}`,
		ToolReceiptJSON: `{"canvasId":"canvas-1","baseRevision":7,"committedRevision":8,"clientMutationId":"mutation-1","proposalHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","appliedOperationIds":["operation-1"],"evidence":{"addedNodeIds":["node-1"],"updatedNodeIds":[],"deletedNodeIds":[],"upsertedConnectionIds":[],"deletedConnectionIds":[],"selectedNodeIds":["node-1"],"viewportApplied":false}}`,
		Now:             time.Now().UTC(), Apply: func(*model.CanvasProject) (string, string, error) { return `{}`, "title", nil },
	}
	if err := validateAgentCanvasChangeInput(valid); err != nil {
		t.Fatalf("valid agent canvas input rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*AgentCanvasChangeInput)
	}{
		{name: "canvas differs from scope", mutate: func(input *AgentCanvasChangeInput) { input.CanvasID = "canvas-2" }},
		{name: "actor differs from scope", mutate: func(input *AgentCanvasChangeInput) { input.ActorUserID = "actor-2" }},
		{name: "receipt revision differs", mutate: func(input *AgentCanvasChangeInput) { input.BaseRevision = 6 }},
		{name: "receipt proposal differs", mutate: func(input *AgentCanvasChangeInput) {
			input.ProposalHash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		}},
		{name: "missing apply", mutate: func(input *AgentCanvasChangeInput) { input.Apply = nil }},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			input := valid
			testCase.mutate(&input)
			if err := validateAgentCanvasChangeInput(input); !errors.Is(err, ErrAgentRuntimeStepConflict) {
				t.Fatalf("error = %v, want step conflict", err)
			}
		})
	}
}
