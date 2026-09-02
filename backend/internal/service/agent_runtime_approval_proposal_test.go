package service

import (
	"encoding/json"
	"errors"
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
	if err := validateStoredApprovalProposal(scope, record, strings.Repeat("a", 64), now, true); !errors.Is(err, agentruntime.ErrApprovalProposalMismatch) {
		t.Fatalf("mismatched proposal error = %v", err)
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

	if err := validateStoredApprovalProposal(scope, record, proposalHash, expiresAt, true); !errors.Is(err, agentruntime.ErrApprovalProposalExpired) {
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

func TestSubmitAgentToolApprovalInvalidatesExpiredProposalAndRequeuesSameRun(t *testing.T) {
	decision := `{"kind":"tool_call","toolCall":{"toolCallId":"publish-expired-1","toolName":"assets.publish","actionVersion":1,"arguments":{"resourceId":"resource-1","domainProjectId":"runtime-project","displayName":"角色图","clientMutationId":"publish-1"}}}`
	server, _ := newAgentRuntimeDecisionServer(t, decision, agentRuntimeTestImageDelivery())
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "expired-approval-run", UserMessage: "发布角色图", MaxSteps: 4,
		Configuration: guidedAgentRuntimeConfigurationInput(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.ModelTask == nil {
		t.Fatal("initial model task was not created")
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	waiting, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.State.Status != agentruntime.RunWaitingApproval || waiting.State.PendingToolCall == nil {
		t.Fatalf("waiting approval progress = %#v", waiting)
	}
	record, err := svc.repo.AgentToolCallForScope(scope, "publish-expired-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := agentruntime.DecodeApprovalProposal(json.RawMessage(record.ApprovalProposalJSON))
	if err != nil {
		t.Fatal(err)
	}
	proposal.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	payload, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	proposalHash, err := proposal.Hash()
	if err != nil {
		t.Fatal(err)
	}
	expiredFacts := struct {
		ApprovalProposalJSON string    `gorm:"column:approval_proposal_json"`
		ApprovalProposalHash string    `gorm:"column:approval_proposal_hash"`
		ApprovalExpiresAt    time.Time `gorm:"column:approval_expires_at"`
	}{ApprovalProposalJSON: string(payload), ApprovalProposalHash: proposalHash, ApprovalExpiresAt: proposal.ExpiresAt}
	if err := db.Model(&model.AgentToolCall{}).Where("id = ?", record.ID).Updates(expiredFacts).Error; err != nil {
		t.Fatal(err)
	}

	progress, err := svc.SubmitAgentToolApproval(scope, AgentToolApprovalSubmission{
		ToolCallID: "publish-expired-1", ActionVersion: 1,
		Decision: agentruntime.ToolApprovalApproved, ProposalHash: proposalHash,
	})
	if err != nil {
		t.Fatalf("expired proposal should become a factual tool result: %v", err)
	}
	if progress.State.Status != agentruntime.RunRunning || progress.State.PendingToolCall != nil ||
		progress.State.LastToolResult == nil || progress.State.LastToolResult.ErrorCode != agentruntime.ApprovalProposalExpired ||
		progress.ModelTask == nil || progress.ModelTask.Status != model.TaskStatusQueued {
		t.Fatalf("expired approval progress = %#v", progress)
	}
	if err := db.First(record, "id = ?", record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if record.Status != agentruntime.ToolCallFailed || record.ErrorCode != agentruntime.ApprovalProposalExpired ||
		record.ApprovalDecision != "" || record.ApprovalByUserID != "" || record.ApprovalDecidedAt != nil {
		t.Fatalf("expired approval persistence = %#v", record)
	}
}

func TestSubmitAgentToolApprovalInvalidatesMismatchedProposalWithoutExecutingTool(t *testing.T) {
	decision := `{"kind":"tool_call","toolCall":{"toolCallId":"publish-mismatch-1","toolName":"assets.publish","actionVersion":1,"arguments":{"resourceId":"resource-1","domainProjectId":"runtime-project","displayName":"角色图","clientMutationId":"publish-2"}}}`
	server, _ := newAgentRuntimeDecisionServer(t, decision, agentRuntimeTestImageDelivery())
	defer server.Close()
	svc, _, _ := newAgentRuntimeServiceFixture(t, server.URL)
	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "mismatched-approval-run", UserMessage: "发布角色图", MaxSteps: 4,
		Configuration: guidedAgentRuntimeConfigurationInput(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.ModelTask == nil {
		t.Fatal("initial model task was not created")
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	waiting, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.State.Status != agentruntime.RunWaitingApproval || waiting.State.PendingToolCall == nil {
		t.Fatalf("waiting approval progress = %#v", waiting)
	}
	record, err := svc.repo.AgentToolCallForScope(scope, "publish-mismatch-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	progress, err := svc.SubmitAgentToolApproval(scope, AgentToolApprovalSubmission{
		ToolCallID: "publish-mismatch-1", ActionVersion: 1,
		Decision: agentruntime.ToolApprovalApproved, ProposalHash: strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatalf("mismatched proposal should become a factual tool result: %v", err)
	}
	if progress.State.Status != agentruntime.RunRunning || progress.State.PendingToolCall != nil ||
		progress.State.LastToolResult == nil || progress.State.LastToolResult.ErrorCode != agentruntime.ApprovalProposalMismatch ||
		progress.ModelTask == nil || progress.ModelTask.Status != model.TaskStatusQueued {
		t.Fatalf("mismatched approval progress = %#v", progress)
	}
	record, err = svc.repo.AgentToolCallForScope(scope, "publish-mismatch-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != agentruntime.ToolCallFailed || record.ErrorCode != agentruntime.ApprovalProposalMismatch ||
		record.ApprovalDecision != "" || record.ApprovalByUserID != "" || record.ApprovalDecidedAt != nil {
		t.Fatalf("mismatched approval persistence = %#v", record)
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
