package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

func TestAgentCanvasCapabilityAppliesApprovedProposalExactlyOnce(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	setAgentCanvasCapabilityDocument(t, db, 7, `{
		"nodes":[
			{"id":"script-1","type":"script","title":"分镜","position":{"x":0,"y":0},"width":480,"height":320,"metadata":{"storyboard":{"rows":[{"id":"shot-1"}]}}},
			{"id":"old-1","type":"text","title":"旧节点","position":{"x":50,"y":50},"width":200,"height":100,"metadata":{}}
		],
		"connections":[{"id":"edge-old","fromNodeId":"old-1","toNodeId":"script-1","toHandleId":"storyboard:context"}],
		"viewport":{"x":0,"y":0,"k":1}
	}`)
	call := agentCanvasApplyOpsCall(`{
		"canvasId":"runtime-canvas",
		"baseRevision":7,
		"clientMutationId":"agent-mutation-1",
		"operations":[
			{"operationId":"op-add","type":"add_node","node":{"id":"image-1","type":"image","title":"角色图","position":{"x":600,"y":20},"width":320,"height":320,"metadata":{}}},
			{"operationId":"op-update","type":"update_node","nodeId":"script-1","patch":{"title":"已确认分镜"}},
			{"operationId":"op-connect","type":"connect_nodes","connection":{"id":"edge-shot","fromNodeId":"script-1","toNodeId":"image-1","fromHandleId":"row:shot-1"}},
			{"operationId":"op-delete","type":"delete_node","nodeId":"old-1"},
			{"operationId":"op-viewport","type":"set_viewport","viewport":{"x":80,"y":-20,"zoom":1.25}},
			{"operationId":"op-select","type":"select_nodes","nodeIds":["image-1"]}
		]
	}`)
	proposalHash := seedApprovedAgentCanvasProposal(t, svc, db, call)
	if err := db.Model(&model.AgentToolCall{}).
		Where("run_id = ? AND tool_call_id = ? AND action_version = ?", agentRuntimeServiceScope().RunID, call.ToolCallID, call.ActionVersion).
		Update("status", agentruntime.ToolCallRunning).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}

	first, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolCanvasApplyOps, first.Output)
	if err != nil {
		t.Fatal(err)
	}
	receipt := decoded.(agentruntime.CanvasApplyOpsResult)
	if receipt.CanvasID != "runtime-canvas" || receipt.BaseRevision != 7 || receipt.CommittedRevision != 8 ||
		receipt.ClientMutationID != "agent-mutation-1" || receipt.ProposalHash != proposalHash ||
		len(receipt.AppliedOperationIDs) != 6 || receipt.Evidence.AddedNodeIDs[0] != "image-1" ||
		receipt.Evidence.UpdatedNodeIDs[0] != "script-1" || receipt.Evidence.DeletedNodeIDs[0] != "old-1" ||
		receipt.Evidence.UpsertedConnectionIDs[0] != "edge-shot" || receipt.Evidence.DeletedConnectionIDs[0] != "edge-old" ||
		!receipt.Evidence.ViewportApplied || len(receipt.Evidence.SelectedNodeIDs) != 1 || receipt.Evidence.SelectedNodeIDs[0] != "image-1" {
		t.Fatalf("receipt = %#v", receipt)
	}
	var completed model.AgentToolCall
	if err := db.Where("run_id = ? AND tool_call_id = ? AND action_version = ?", agentRuntimeServiceScope().RunID, call.ToolCallID, call.ActionVersion).
		Take(&completed).Error; err != nil {
		t.Fatal(err)
	}
	if completed.Status != agentruntime.ToolCallSucceeded || completed.OutputJSON != string(first.Output) {
		t.Fatalf("atomic tool receipt = %#v, want succeeded receipt", completed)
	}

	second, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
	if err != nil {
		t.Fatal(err)
	}
	if string(second.Output) != string(first.Output) {
		t.Fatalf("idempotent receipt changed:\nfirst=%s\nsecond=%s", first.Output, second.Output)
	}
	var canvas model.CanvasProject
	if err := db.First(&canvas, "id = ?", "runtime-canvas").Error; err != nil {
		t.Fatal(err)
	}
	if canvas.Revision != 8 || !strings.Contains(canvas.PayloadJSON, `"id":"image-1"`) || strings.Contains(canvas.PayloadJSON, `"id":"old-1"`) || !strings.Contains(canvas.PayloadJSON, `"k":1.25`) {
		t.Fatalf("committed canvas = %#v", canvas)
	}
	var changeCount int64
	if err := db.Model(&model.CanvasChange{}).Where("canvas_id = ?", canvas.ID).Count(&changeCount).Error; err != nil {
		t.Fatal(err)
	}
	if changeCount != 1 {
		t.Fatalf("change count = %d, want 1", changeCount)
	}
}

func TestAgentCanvasCapabilityRejectsStaleRevisionWithoutPartialMutation(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	call := agentCanvasApplyOpsCall(`{"canvasId":"runtime-canvas","baseRevision":6,"clientMutationId":"stale-mutation","operations":[{"operationId":"op-add","type":"add_node","node":{"id":"node-stale","type":"text","title":"不应写入","position":{"x":0,"y":0},"width":200,"height":100,"metadata":{}}}]}`)
	seedApprovedAgentCanvasProposal(t, svc, db, call)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call); !agentCapabilityErrorCode(err, "canvas_revision_conflict") {
		t.Fatalf("stale revision error = %v", err)
	}
	assertAgentCanvasUnchanged(t, db, 7, "node-stale")
}

func TestAgentCanvasCapabilityRejectsUnknownHandleWithoutPartialMutation(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	setAgentCanvasCapabilityDocument(t, db, 7, `{"nodes":[{"id":"script-1","type":"script","title":"分镜","position":{"x":0,"y":0},"width":400,"height":300,"metadata":{"storyboard":{"rows":[{"id":"shot-1"}]}}},{"id":"image-1","type":"image","title":"图","position":{"x":500,"y":0},"width":300,"height":300,"metadata":{}}],"connections":[],"viewport":{"x":0,"y":0,"k":1}}`)
	call := agentCanvasApplyOpsCall(`{"canvasId":"runtime-canvas","baseRevision":7,"clientMutationId":"bad-handle-mutation","operations":[{"operationId":"op-connect","type":"connect_nodes","connection":{"id":"edge-bad","fromNodeId":"script-1","toNodeId":"image-1","fromHandleId":"row:missing"}}]}`)
	seedApprovedAgentCanvasProposal(t, svc, db, call)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call); !agentCapabilityErrorCode(err, "canvas_handle_invalid") {
		t.Fatalf("invalid handle error = %v", err)
	}
	assertAgentCanvasUnchanged(t, db, 7, "edge-bad")
}

func TestAgentCanvasCapabilityRejectsSelectionDeletedLaterInProposal(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	setAgentCanvasCapabilityDocument(t, db, 7, `{"nodes":[{"id":"image-1","type":"image","title":"图","position":{"x":0,"y":0},"width":300,"height":300,"metadata":{}}],"connections":[],"viewport":{"x":0,"y":0,"k":1}}`)
	call := agentCanvasApplyOpsCall(`{"canvasId":"runtime-canvas","baseRevision":7,"clientMutationId":"stale-selection-mutation","operations":[{"operationId":"op-select","type":"select_nodes","nodeIds":["image-1"]},{"operationId":"op-delete","type":"delete_node","nodeId":"image-1"}]}`)
	seedApprovedAgentCanvasProposal(t, svc, db, call)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call); !agentCapabilityErrorCode(err, "canvas_selection_stale") {
		t.Fatalf("stale final selection error = %v", err)
	}
	assertAgentCanvasUnchanged(t, db, 7, "stale-selection-mutated")
}

func TestAgentCanvasCapabilityRejectsConnectionInvalidatedLaterInProposal(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	setAgentCanvasCapabilityDocument(t, db, 7, `{"nodes":[{"id":"script-1","type":"script","title":"分镜","position":{"x":0,"y":0},"width":400,"height":300,"metadata":{"storyboard":{"rows":[{"id":"shot-1"}]}}},{"id":"image-1","type":"image","title":"图","position":{"x":500,"y":0},"width":300,"height":300,"metadata":{}}],"connections":[{"id":"edge-1","fromNodeId":"script-1","toNodeId":"image-1","fromHandleId":"row:shot-1"}],"viewport":{"x":0,"y":0,"k":1}}`)
	call := agentCanvasApplyOpsCall(`{"canvasId":"runtime-canvas","baseRevision":7,"clientMutationId":"stale-handle-after-update","operations":[{"operationId":"op-update","type":"update_node","nodeId":"script-1","patch":{"metadata":{"storyboard":{"rows":[{"id":"shot-2"}]}}}}]}`)
	seedApprovedAgentCanvasProposal(t, svc, db, call)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call); !agentCapabilityErrorCode(err, "canvas_handle_invalid") {
		t.Fatalf("stale final handle error = %v", err)
	}
	assertAgentCanvasUnchanged(t, db, 7, `"shot-2"`)
}

func TestAgentCanvasCapabilityRejectsConnectionAddedToDeletedEndpoint(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	setAgentCanvasCapabilityDocument(t, db, 7, `{"nodes":[{"id":"text-1","type":"text","title":"文本","position":{"x":0,"y":0},"width":300,"height":200,"metadata":{}},{"id":"image-1","type":"image","title":"图","position":{"x":500,"y":0},"width":300,"height":300,"metadata":{}}],"connections":[],"viewport":{"x":0,"y":0,"k":1}}`)
	call := agentCanvasApplyOpsCall(`{"canvasId":"runtime-canvas","baseRevision":7,"clientMutationId":"transient-connection-mutation","operations":[{"operationId":"op-connect","type":"connect_nodes","connection":{"id":"edge-transient","fromNodeId":"text-1","toNodeId":"image-1"}},{"operationId":"op-delete","type":"delete_node","nodeId":"image-1"}]}`)
	seedApprovedAgentCanvasProposal(t, svc, db, call)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call); !agentCapabilityErrorCode(err, "canvas_operation_conflict") {
		t.Fatalf("transient connection error = %v", err)
	}
	assertAgentCanvasUnchanged(t, db, 7, "edge-transient")
}

func TestAgentCanvasCapabilityRejectsInvalidEdgeWithoutPartialMutation(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	setAgentCanvasCapabilityDocument(t, db, 7, `{"nodes":[{"id":"video-1","type":"video","title":"视频","position":{"x":0,"y":0},"width":400,"height":300,"metadata":{}},{"id":"image-1","type":"image","title":"图","position":{"x":500,"y":0},"width":300,"height":300,"metadata":{}}],"connections":[],"viewport":{"x":0,"y":0,"k":1}}`)
	call := agentCanvasApplyOpsCall(`{"canvasId":"runtime-canvas","baseRevision":7,"clientMutationId":"bad-edge-mutation","operations":[{"operationId":"op-connect","type":"connect_nodes","connection":{"id":"edge-bad","fromNodeId":"video-1","toNodeId":"image-1"}}]}`)
	seedApprovedAgentCanvasProposal(t, svc, db, call)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call); err == nil {
		t.Fatal("invalid video-to-image edge unexpectedly succeeded")
	}
	assertAgentCanvasUnchanged(t, db, 7, "edge-bad")
}

func TestAgentRuntimeViewProjectsExactPendingApprovalProposal(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	call := agentCanvasApplyOpsCall(`{"canvasId":"runtime-canvas","baseRevision":7,"clientMutationId":"approval-view-mutation","operations":[{"operationId":"op-add","type":"add_node","node":{"id":"node-approved","type":"text","title":"待确认","position":{"x":0,"y":0},"width":200,"height":100,"metadata":{}}}]}`)
	proposalHash := seedAgentCanvasProposal(t, svc, db, call, false)
	scope := agentRuntimeServiceScope()
	run, err := svc.repo.AgentRunForScope(scope)
	if err != nil {
		t.Fatal(err)
	}
	view, err := svc.enrichAgentRuntimeView(scope, &AgentRuntimeView{
		Run: *run,
		State: agentruntime.RuntimeState{
			Status:          agentruntime.RunWaitingApproval,
			PendingToolCall: &call,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.PendingApproval == nil || view.PendingApproval.ToolCallID != call.ToolCallID || view.PendingApproval.ToolName != call.ToolName ||
		view.PendingApproval.ActionVersion != call.ActionVersion || view.PendingApproval.ProposalHash != proposalHash ||
		view.PendingApproval.Effect.Kind != agentruntime.ApprovalEffectCanvasMutation || len(view.PendingApproval.Effect.TargetIDs) != 1 ||
		view.PendingApproval.Effect.TargetIDs[0] != scope.CanvasID || view.PendingApproval.ExpiresAt.IsZero() {
		t.Fatalf("pending approval projection = %#v", view.PendingApproval)
	}
	state, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	state.Status = agentruntime.RunWaitingApproval
	state.PendingToolCall = &call
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).Update("status", agentruntime.RunWaitingApproval).Error; err != nil {
		t.Fatal(err)
	}
	stateView, err := svc.agentRuntimeViewForState(scope, state)
	if err != nil {
		t.Fatal(err)
	}
	if stateView.PendingApproval == nil || stateView.PendingApproval.ProposalHash != proposalHash {
		t.Fatalf("state response omitted frozen approval proposal: %#v", stateView.PendingApproval)
	}
}

func TestAgentRuntimeViewProjectsAuthoritativeMediaApprovalQuote(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	scope := agentRuntimeServiceScope()
	seedAgentCanvasProposal(t, svc, db, agentCanvasApplyOpsCall(`{"canvasId":"runtime-canvas","baseRevision":7,"clientMutationId":"seed-media-view","operations":[{"operationId":"seed-select","type":"select_nodes","nodeIds":[]}]}`), false)
	call := agentruntime.ToolCallDecision{
		ToolCallID: "media-approval-view", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
		Arguments: json.RawMessage(`{"mediaKind":"video","modelRecordId":"video-record","modelKey":"seedance-2.0","parameters":{"prompt":"雨夜追逐","durationSeconds":5},"sourceResourceIds":[],"targetCanvasNodeId":"video-node","clientRequestId":"generate-video-1"}`),
	}
	quote := &agentruntime.ApprovalCostQuote{
		ModelRecordID: "video-record", ModelKey: "seedance-2.0", PriceVersion: 7, AmountMicrocredits: 1_250_000,
	}
	proposal, err := agentruntime.NewApprovalProposalForTool(scope, call, quote, scope.RunID+":"+call.ToolCallID+":1", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	record, _ := storedApprovalProposalRecord(t, proposal)
	policy, found := agentruntime.ToolPolicyForSchema(call.ToolName, agentruntime.CurrentToolSchemaVersion)
	if !found {
		t.Fatal("media.generate policy is missing")
	}
	record.ID = newID()
	record.Status = agentruntime.ToolCallPending
	record.RiskLevel = policy.RiskLevel
	record.RequiredAccess = policy.RequiredAccess
	record.ApprovalRequired = true
	record.IdempotencyKey = proposal.IdempotencyKey
	record.InputJSON = string(call.Arguments)
	record.OutputJSON = `{}`
	record.CreatedAt = time.Now().UTC()
	record.UpdatedAt = record.CreatedAt
	if err := db.Create(record).Error; err != nil {
		t.Fatal(err)
	}
	run, err := svc.repo.AgentRunForScope(scope)
	if err != nil {
		t.Fatal(err)
	}
	view, err := svc.enrichAgentRuntimeView(scope, &AgentRuntimeView{
		Run:   *run,
		State: agentruntime.RuntimeState{Status: agentruntime.RunWaitingApproval, PendingToolCall: &call},
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.PendingApproval == nil || view.PendingApproval.Quote == nil ||
		view.PendingApproval.Quote.ModelRecordID != quote.ModelRecordID || view.PendingApproval.Quote.ModelKey != quote.ModelKey ||
		view.PendingApproval.Quote.PriceVersion != quote.PriceVersion || view.PendingApproval.Quote.AmountMicrocredits != quote.AmountMicrocredits {
		t.Fatalf("pending media quote = %#v", view.PendingApproval)
	}
}

func TestAgentCanvasCapabilityRejectsUnapprovedAndUnauthorizedCalls(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		configure func(*testing.T, *Service, *gorm.DB, agentruntime.ToolCallDecision)
		wantCode  string
	}{
		{
			name: "not approved",
			configure: func(t *testing.T, svc *Service, db *gorm.DB, call agentruntime.ToolCallDecision) {
				seedAgentCanvasProposal(t, svc, db, call, false)
			},
			wantCode: "canvas_proposal_not_approved",
		},
		{
			name: "ownership changed",
			configure: func(t *testing.T, svc *Service, db *gorm.DB, call agentruntime.ToolCallDecision) {
				seedApprovedAgentCanvasProposal(t, svc, db, call)
				if err := db.Model(&model.CanvasProject{}).Where("id = ?", "runtime-canvas").Update("user_id", "other-user").Error; err != nil {
					t.Fatal(err)
				}
			},
			wantCode: "capability_ownership_forbidden",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
			call := agentCanvasApplyOpsCall(`{"canvasId":"runtime-canvas","baseRevision":7,"clientMutationId":"forbidden-mutation","operations":[{"operationId":"op-add","type":"add_node","node":{"id":"node-forbidden","type":"text","title":"不应写入","position":{"x":0,"y":0},"width":200,"height":100,"metadata":{}}}]}`)
			testCase.configure(t, svc, db, call)
			registry, err := newAgentCapabilityRegistry(svc)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call); !agentCapabilityErrorCode(err, testCase.wantCode) {
				t.Fatalf("error = %v, want code %s", err, testCase.wantCode)
			}
			assertAgentCanvasUnchanged(t, db, 7, "node-forbidden")
		})
	}
}

func agentCanvasApplyOpsCall(arguments string) agentruntime.ToolCallDecision {
	return agentruntime.ToolCallDecision{ToolCallID: "call-apply", ToolName: agentruntime.ToolCanvasApplyOps, ActionVersion: 1, Arguments: json.RawMessage(arguments)}
}

func seedApprovedAgentCanvasProposal(t *testing.T, svc *Service, db *gorm.DB, call agentruntime.ToolCallDecision) string {
	t.Helper()
	return seedAgentCanvasProposal(t, svc, db, call, true)
}

func seedAgentCanvasProposal(t *testing.T, svc *Service, db *gorm.DB, call agentruntime.ToolCallDecision, approved bool) string {
	t.Helper()
	scope := agentRuntimeServiceScope()
	now := time.Now().UTC()
	configuration, err := svc.resolveAgentRuntimeConfiguration(context.Background(), scope.ActorUserID, guidedAgentRuntimeConfigurationInput())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repo.CreateInitializedAgentRun(repository.CreateInitializedAgentRunInput{
		Create: repository.CreateAgentRunInput{Scope: scope, ClientRequestID: "canvas-capability-run", Now: now},
		Initialize: repository.InitializeAgentRunInput{
			Scope: scope, ModelRecordID: "runtime-agent-model", ModelKey: "gpt-5.5", MaxSteps: 8,
			ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion, RuntimeVersion: agentruntime.CurrentRuntimeVersion,
			PolicyVersion: agentruntime.CurrentPolicyVersion, UserMessage: "更新画布", Configuration: configuration, Now: now,
		},
	}); err != nil {
		t.Fatal(err)
	}
	proposal, err := agentruntime.NewApprovalProposalForTool(scope, call, nil, scope.RunID+":"+call.ToolCallID+":1", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	proposalHash, err := proposal.Hash()
	if err != nil {
		t.Fatal(err)
	}
	policy, found := agentruntime.ToolPolicyForSchema(call.ToolName, agentruntime.CurrentToolSchemaVersion)
	if !found {
		t.Fatal("canvas.apply_ops policy is missing")
	}
	record := model.AgentToolCall{
		ID: "canvas-capability-call", RunID: scope.RunID, ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion,
		ToolName: string(call.ToolName), Status: agentruntime.ToolCallPending, RiskLevel: policy.RiskLevel,
		RequiredAccess: policy.RequiredAccess, ApprovalRequired: true, IdempotencyKey: scope.RunID + ":" + call.ToolCallID + ":1",
		InputJSON: string(call.Arguments), OutputJSON: `{}`, ApprovalProposalJSON: string(payload),
		ApprovalProposalHash: proposalHash, ApprovalExpiresAt: &proposal.ExpiresAt, CreatedAt: now, UpdatedAt: now,
	}
	if approved {
		record.ApprovalDecision = agentruntime.ToolApprovalApproved
		record.ApprovalByUserID = scope.ActorUserID
		record.ApprovalDecidedAt = &now
	}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	return proposalHash
}

func setAgentCanvasCapabilityDocument(t *testing.T, db *gorm.DB, revision int64, payload string) {
	t.Helper()
	updates := struct {
		Revision    int64  `gorm:"column:revision"`
		PayloadJSON string `gorm:"column:payload_json"`
	}{Revision: revision, PayloadJSON: payload}
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", "runtime-canvas").Updates(updates).Error; err != nil {
		t.Fatal(err)
	}
}

func assertAgentCanvasUnchanged(t *testing.T, db *gorm.DB, revision int64, forbiddenText string) {
	t.Helper()
	var canvas model.CanvasProject
	if err := db.First(&canvas, "id = ?", "runtime-canvas").Error; err != nil {
		t.Fatal(err)
	}
	if canvas.Revision != revision || strings.Contains(canvas.PayloadJSON, forbiddenText) {
		t.Fatalf("canvas changed unexpectedly: %#v", canvas)
	}
	var changeCount int64
	if err := db.Model(&model.CanvasChange{}).Where("canvas_id = ?", canvas.ID).Count(&changeCount).Error; err != nil {
		t.Fatal(err)
	}
	if changeCount != 0 {
		t.Fatalf("change count = %d, want 0", changeCount)
	}
}

func agentCapabilityErrorCode(err error, want string) bool {
	var failure *agentCapabilityExecutionError
	return errors.As(err, &failure) && failure.Code == want
}
