package agentruntime_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
)

func TestApprovalProposalHashIsStableAndCoversMaterialFacts(t *testing.T) {
	t.Parallel()

	expiresAt := time.Date(2026, time.September, 1, 12, 30, 0, 0, time.UTC)
	base, err := agentruntime.NewApprovalProposal(agentruntime.ApprovalProposalInput{
		Scope: approvalProposalScope(),
		ToolCall: agentruntime.ToolCallDecision{
			ToolCallID: "generate-1", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
			Arguments: json.RawMessage(`{"modelKey":"seedance-2.0","parameters":{"prompt":"雨夜追逐","durationSeconds":5},"mediaKind":"video","sourceResourceIds":["resource-1"],"modelRecordId":"record-1","targetCanvasNodeId":"node-1","clientRequestId":"request-1"}`),
		},
		Effect:         agentruntime.ApprovalEffect{Kind: agentruntime.ApprovalEffectMediaGeneration, Summary: "生成 video 媒体", TargetIDs: []string{"node-1"}},
		Quote:          &agentruntime.ApprovalCostQuote{ModelRecordID: "record-1", ModelKey: "seedance-2.0", PriceVersion: 3, AmountMicrocredits: 2500},
		IdempotencyKey: "run-1:generate-1:1", ExpiresAt: expiresAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	baseHash, err := base.Hash()
	if err != nil {
		t.Fatal(err)
	}

	reordered, err := agentruntime.NewApprovalProposal(agentruntime.ApprovalProposalInput{
		Scope: approvalProposalScope(),
		ToolCall: agentruntime.ToolCallDecision{
			ToolCallID: "generate-1", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
			Arguments: json.RawMessage(`{"clientRequestId":"request-1","targetCanvasNodeId":"node-1","sourceResourceIds":["resource-1"],"parameters":{"durationSeconds":5,"prompt":"雨夜追逐"},"modelKey":"seedance-2.0","modelRecordId":"record-1","mediaKind":"video"}`),
		},
		Effect:         agentruntime.ApprovalEffect{Kind: agentruntime.ApprovalEffectMediaGeneration, Summary: "生成 video 媒体", TargetIDs: []string{"node-1"}},
		Quote:          &agentruntime.ApprovalCostQuote{ModelRecordID: "record-1", ModelKey: "seedance-2.0", PriceVersion: 3, AmountMicrocredits: 2500},
		IdempotencyKey: "run-1:generate-1:1", ExpiresAt: expiresAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	reorderedHash, err := reordered.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if reorderedHash != baseHash {
		t.Fatalf("canonical hash changed with JSON field order: %s != %s", reorderedHash, baseHash)
	}

	mutations := []struct {
		name   string
		mutate func(*agentruntime.ApprovalProposal)
	}{
		{name: "arguments", mutate: func(value *agentruntime.ApprovalProposal) {
			value.Arguments = json.RawMessage(`{"mediaKind":"video","modelRecordId":"record-1","modelKey":"seedance-2.0","parameters":{"prompt":"晴日追逐","durationSeconds":5},"sourceResourceIds":["resource-1"],"targetCanvasNodeId":"node-1","clientRequestId":"request-1"}`)
		}},
		{name: "scope", mutate: func(value *agentruntime.ApprovalProposal) { value.Scope.CanvasID = "canvas-2" }},
		{name: "model", mutate: func(value *agentruntime.ApprovalProposal) {
			value.Quote.ModelKey = "seedance-2.1"
			value.Arguments = json.RawMessage(`{"mediaKind":"video","modelRecordId":"record-1","modelKey":"seedance-2.1","parameters":{"prompt":"雨夜追逐","durationSeconds":5},"sourceResourceIds":["resource-1"],"targetCanvasNodeId":"node-1","clientRequestId":"request-1"}`)
		}},
		{name: "price", mutate: func(value *agentruntime.ApprovalProposal) { value.Quote.PriceVersion++ }},
		{name: "expiry", mutate: func(value *agentruntime.ApprovalProposal) { value.ExpiresAt = value.ExpiresAt.Add(time.Minute) }},
	}
	for _, testCase := range mutations {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			changed := base
			changed.Arguments = append(json.RawMessage(nil), base.Arguments...)
			quote := *base.Quote
			changed.Quote = &quote
			changed.Effect.TargetIDs = append([]string(nil), base.Effect.TargetIDs...)
			testCase.mutate(&changed)
			changedHash, hashErr := changed.Hash()
			if hashErr != nil {
				t.Fatal(hashErr)
			}
			if changedHash == baseHash {
				t.Fatalf("%s mutation did not change approval hash", testCase.name)
			}
		})
	}

	changedEffect := base
	changedEffect.Effect.Summary = "生成另一项媒体"
	if _, err := changedEffect.Hash(); err == nil {
		t.Fatal("approval effect that conflicts with arguments was accepted")
	}
}

func TestApprovalProposalDecisionRejectsMismatchAndExpiry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	proposal, err := agentruntime.NewApprovalProposal(agentruntime.ApprovalProposalInput{
		Scope: approvalProposalScope(),
		ToolCall: agentruntime.ToolCallDecision{
			ToolCallID: "canvas-write-1", ToolName: agentruntime.ToolCanvasApplyOps, ActionVersion: 1,
			Arguments: json.RawMessage(`{"canvasId":"canvas-1","baseRevision":2,"clientMutationId":"mutation-1","operations":[{"operationId":"op-1","type":"select_nodes","nodeIds":[]}]}`),
		},
		Effect:         agentruntime.ApprovalEffect{Kind: agentruntime.ApprovalEffectCanvasMutation, Summary: "执行 1 项画布操作", TargetIDs: []string{"canvas-1"}},
		IdempotencyKey: "run-1:canvas-write-1:1", ExpiresAt: now.Add(15 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	hash, err := proposal.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := agentruntime.ValidateApprovalProposalDecision(proposal, hash, now); err != nil {
		t.Fatalf("valid approval rejected: %v", err)
	}
	if err := agentruntime.ValidateApprovalProposalDecision(proposal, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", now); !errors.Is(err, agentruntime.ErrApprovalProposalMismatch) {
		t.Fatalf("mismatched approval error = %v", err)
	}
	if err := agentruntime.ValidateApprovalProposalDecision(proposal, hash, proposal.ExpiresAt); !errors.Is(err, agentruntime.ErrApprovalProposalExpired) {
		t.Fatalf("expired approval error = %v", err)
	}
}

func TestDecodeApprovalProposalReturnsCanonicalArguments(t *testing.T) {
	t.Parallel()

	payload := json.RawMessage(`{
		"version":1,
		"runId":"run-1",
		"toolCallId":"canvas-write-1",
		"actionVersion":1,
		"scope":{"tenantKind":"personal","tenantId":"user-1","actorUserId":"user-1","domainProjectId":"project-1","canvasId":"canvas-1","threadId":"thread-1"},
		"toolName":"canvas.apply_ops",
		"arguments":{"operations":[{"nodeIds":[],"type":"select_nodes","operationId":"op-1"}],"clientMutationId":"mutation-1","baseRevision":2,"canvasId":"canvas-1"},
		"effect":{"kind":"canvas_mutation","summary":"执行 1 项画布操作","targetIds":["canvas-1"]},
		"idempotencyKey":"run-1:canvas-write-1:1",
		"expiresAt":"2026-09-01T12:15:00+00:00"
	}`)
	proposal, err := agentruntime.DecodeApprovalProposal(payload)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"baseRevision":2,"canvasId":"canvas-1","clientMutationId":"mutation-1","operations":[{"nodeIds":[],"operationId":"op-1","type":"select_nodes"}]}`
	if string(proposal.Arguments) != want {
		t.Fatalf("canonical arguments = %s, want %s", proposal.Arguments, want)
	}
	wantExpiry := time.Date(2026, time.September, 1, 12, 15, 0, 0, time.UTC)
	if !proposal.ExpiresAt.Equal(wantExpiry) || proposal.ExpiresAt.Location() != time.UTC {
		t.Fatalf("canonical expiry = %s (%s)", proposal.ExpiresAt, proposal.ExpiresAt.Location())
	}
}

func TestApprovalProposalRequiresQuoteBoundToMediaModel(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	mediaCall := agentruntime.ToolCallDecision{
		ToolCallID: "generate-quote", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
		Arguments: json.RawMessage(`{"mediaKind":"video","modelRecordId":"record-1","modelKey":"seedance-2.0","parameters":{"prompt":"雨夜追逐"},"sourceResourceIds":[],"targetCanvasNodeId":"node-1","clientRequestId":"request-1"}`),
	}
	validQuote := &agentruntime.ApprovalCostQuote{
		ModelRecordID: "record-1", ModelKey: "seedance-2.0", PriceVersion: 3, AmountMicrocredits: 2_500,
	}

	if _, err := agentruntime.NewApprovalProposalForTool(approvalProposalScope(), mediaCall, nil, "run-1:generate-quote:1", now.Add(time.Minute)); err == nil {
		t.Fatal("paid media proposal without a quote was accepted")
	}
	mismatchedRecord := *validQuote
	mismatchedRecord.ModelRecordID = "record-2"
	if _, err := agentruntime.NewApprovalProposalForTool(approvalProposalScope(), mediaCall, &mismatchedRecord, "run-1:generate-quote:1", now.Add(time.Minute)); err == nil {
		t.Fatal("media proposal with a mismatched model record was accepted")
	}
	mismatchedKey := *validQuote
	mismatchedKey.ModelKey = "seedance-2.1"
	if _, err := agentruntime.NewApprovalProposalForTool(approvalProposalScope(), mediaCall, &mismatchedKey, "run-1:generate-quote:1", now.Add(time.Minute)); err == nil {
		t.Fatal("media proposal with a mismatched model key was accepted")
	}
	proposal, err := agentruntime.NewApprovalProposalForTool(approvalProposalScope(), mediaCall, validQuote, "run-1:generate-quote:1", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("valid media quote was rejected: %v", err)
	}
	validQuote.AmountMicrocredits++
	if proposal.Quote == nil || proposal.Quote.AmountMicrocredits != 2_500 {
		t.Fatalf("proposal quote changed through caller alias: %#v", proposal.Quote)
	}

	canvasCall := agentruntime.ToolCallDecision{
		ToolCallID: "canvas-quote", ToolName: agentruntime.ToolCanvasApplyOps, ActionVersion: 1,
		Arguments: json.RawMessage(`{"canvasId":"canvas-1","baseRevision":2,"clientMutationId":"mutation-1","operations":[{"operationId":"op-1","type":"select_nodes","nodeIds":[]}]}`),
	}
	if _, err := agentruntime.NewApprovalProposalForTool(approvalProposalScope(), canvasCall, validQuote, "run-1:canvas-quote:1", now.Add(time.Minute)); err == nil {
		t.Fatal("non-media proposal with a cost quote was accepted")
	}
}

func TestApprovalProposalRequiresQuoteBoundToVisionModel(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 2, 10, 0, 0, 0, time.UTC)
	call := agentruntime.ToolCallDecision{
		ToolCallID: "vision-quote", ToolName: agentruntime.ToolVisionAnalyze, ActionVersion: 1,
		Arguments: json.RawMessage(`{"modelRecordId":"vision-record-1","modelKey":"deepseek-v4-flash-vision-exp","sourceResourceIds":["resource-1","resource-2"],"prompt":"描述视觉事实","detail":"low","clientRequestId":"vision-request-1"}`),
	}
	if _, err := agentruntime.NewApprovalProposalForTool(approvalProposalScope(), call, nil, "vision-capability-key", now.Add(time.Minute)); err == nil {
		t.Fatal("paid vision proposal without quote was accepted")
	}
	quote := &agentruntime.ApprovalCostQuote{
		ModelRecordID: "vision-record-1", ModelKey: "deepseek-v4-flash-vision-exp", PriceVersion: 4, AmountMicrocredits: 900,
	}
	proposal, err := agentruntime.NewApprovalProposalForTool(approvalProposalScope(), call, quote, "vision-capability-key", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("valid vision proposal rejected: %v", err)
	}
	if proposal.Effect.Kind != agentruntime.ApprovalEffectVisionAnalysis || proposal.Effect.Summary != "理解 2 张图片" {
		t.Fatalf("vision approval effect = %#v", proposal.Effect)
	}
	if len(proposal.Effect.TargetIDs) != 2 || proposal.Effect.TargetIDs[0] != "resource-1" || proposal.Effect.TargetIDs[1] != "resource-2" {
		t.Fatalf("vision approval targets = %#v", proposal.Effect.TargetIDs)
	}
	mismatch := *quote
	mismatch.ModelRecordID = "vision-record-2"
	if _, err := agentruntime.NewApprovalProposalForTool(approvalProposalScope(), call, &mismatch, "vision-capability-key", now.Add(time.Minute)); err == nil {
		t.Fatal("vision proposal with mismatched frozen model was accepted")
	}
}

func approvalProposalScope() agentruntime.Scope {
	return agentruntime.Scope{
		TenantKind: agentruntime.TenantPersonal, TenantID: "user-1", ActorUserID: "user-1",
		DomainProjectID: "project-1", CanvasID: "canvas-1", ThreadID: "thread-1", RunID: "run-1",
		Access: agentruntime.AccessGrant{Level: agentruntime.AccessEditor, SubscriptionActive: true},
	}
}
