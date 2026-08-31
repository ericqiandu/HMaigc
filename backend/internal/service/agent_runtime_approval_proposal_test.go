package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

func TestValidateStoredApprovalProposalRequiresExactFreshFacts(t *testing.T) {
	t.Parallel()

	scope := agentRuntimeServiceScope()
	now := time.Date(2026, time.September, 1, 8, 0, 0, 0, time.UTC)
	record, proposalHash := storedCanvasApprovalProposal(t, scope, now.Add(agentruntime.DefaultApprovalProposalTTL))

	if err := validateStoredApprovalProposal(scope, record, proposalHash, now, true); err != nil {
		t.Fatalf("fresh proposal rejected: %v", err)
	}
	if err := validateStoredApprovalProposal(scope, record, "", now, true); err == nil {
		t.Fatal("missing proposal hash was accepted")
	}
	if err := validateStoredApprovalProposal(scope, record, strings.Repeat("a", 64), now, true); err == nil {
		t.Fatal("mismatched proposal hash was accepted")
	}

	conflictingScope := scope
	conflictingScope.CanvasID = "another-canvas"
	if err := validateStoredApprovalProposal(conflictingScope, record, proposalHash, now, true); err == nil {
		t.Fatal("proposal with conflicting scope was accepted")
	}
}

func TestValidateStoredApprovalProposalRejectsExpiredDecisionButAllowsExactReplay(t *testing.T) {
	t.Parallel()

	scope := agentRuntimeServiceScope()
	expiresAt := time.Date(2026, time.September, 1, 8, 15, 0, 0, time.UTC)
	record, proposalHash := storedCanvasApprovalProposal(t, scope, expiresAt)

	if err := validateStoredApprovalProposal(scope, record, proposalHash, expiresAt, true); err == nil || err.Error() != "approval proposal expired" {
		t.Fatalf("expired decision error = %v", err)
	}
	if err := validateStoredApprovalProposal(scope, record, proposalHash, expiresAt.Add(time.Hour), false); err != nil {
		t.Fatalf("exact decided replay was rejected after expiry: %v", err)
	}
}

func TestValidateStoredApprovalProposalRejectsPaidMediaWithoutAuthoritativeQuote(t *testing.T) {
	t.Parallel()

	scope := agentRuntimeServiceScope()
	now := time.Date(2026, time.September, 1, 8, 0, 0, 0, time.UTC)
	expiresAt := now.Add(agentruntime.DefaultApprovalProposalTTL)
	proposalHash := strings.Repeat("a", 64)
	record := &model.AgentToolCall{
		RunID: scope.RunID, ToolCallID: "call-media", ActionVersion: 1, ToolName: string(agentruntime.ToolMediaGenerate),
		ApprovalProposalHash: proposalHash, ApprovalExpiresAt: &expiresAt,
		ApprovalProposalJSON: `{"version":1,"runId":"runtime-run","toolCallId":"call-media","actionVersion":1,"scope":{"tenantKind":"personal","tenantId":"runtime-user","actorUserId":"runtime-user","domainProjectId":"runtime-project","canvasId":"runtime-canvas","threadId":"runtime-thread"},"toolName":"media.generate","arguments":{"clientRequestId":"request-1","mediaKind":"video","modelKey":"seedance-2.0","modelRecordId":"model-record-1","parameters":{"prompt":"雨夜追逐"},"sourceResourceIds":[],"targetCanvasNodeId":"node-1"},"effect":{"kind":"media_generation","summary":"生成 video 媒体","targetIds":["node-1"]},"idempotencyKey":"runtime-run:call-media:1","expiresAt":"2026-09-01T08:15:00Z"}`,
	}

	if err := validateStoredApprovalProposal(scope, record, proposalHash, now, true); err == nil || err.Error() != "approval proposal cost quote is required" {
		t.Fatalf("missing quote error = %v", err)
	}
}

func storedCanvasApprovalProposal(t *testing.T, scope agentruntime.Scope, expiresAt time.Time) (*model.AgentToolCall, string) {
	t.Helper()
	call := agentruntime.ToolCallDecision{
		ToolCallID: "call-apply", ToolName: agentruntime.ToolCanvasApplyOps, ActionVersion: 1,
		Arguments: json.RawMessage(`{"canvasId":"runtime-canvas","baseRevision":3,"clientMutationId":"mutation-1","operations":[{"operationId":"op-1","type":"select_nodes","nodeIds":[]}]}`),
	}
	proposal, err := agentruntime.NewApprovalProposalForTool(scope, call, nil, "runtime-run:call-apply:1", expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	return storedApprovalProposalRecord(t, proposal)
}

func storedApprovalProposalRecord(t *testing.T, proposal agentruntime.ApprovalProposal) (*model.AgentToolCall, string) {
	t.Helper()
	payload, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	proposalHash, err := proposal.Hash()
	if err != nil {
		t.Fatal(err)
	}
	return &model.AgentToolCall{
		RunID: proposal.RunID, ToolCallID: proposal.ToolCallID, ActionVersion: proposal.ActionVersion,
		ToolName: string(proposal.ToolName), ApprovalProposalJSON: string(payload), ApprovalProposalHash: proposalHash,
		ApprovalExpiresAt: &proposal.ExpiresAt,
	}, proposalHash
}
