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
)

func TestStartExternalAgentRunDoesNotRequireManagedVisionConfiguration(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	if err := db.Where("key = ?", agentDefaultVisionModelSettingKey).Delete(&model.SystemSetting{}).Error; err != nil {
		t.Fatal(err)
	}
	actor := &model.User{ID: "runtime-user"}
	input := StartExternalAgentRunInput{
		Context: context.Background(), CanvasID: "runtime-canvas", ExternalThreadID: "codex-thread-1",
		ClientRequestID: "codex-turn-1", UserMessage: "读取当前画布", MaxSteps: 8,
		Configuration: guidedAgentRuntimeConfigurationInput(),
	}
	view, err := svc.StartExternalAgentRun(actor, input)
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.ReasoningHost != agentruntime.ReasoningHostLocalCodex || view.Run.ModelRecordID != "" || view.Run.ModelKey != "" ||
		view.State.Status != agentruntime.RunRunning || view.State.StateVersion != 2 || view.State.MaxSteps != input.MaxSteps ||
		view.State.UserMessage != input.UserMessage || view.State.Configuration.ExecutionMode != agentruntime.ExecutionGuided {
		t.Fatalf("external run view = %#v", view)
	}
	if vision := view.State.Configuration.GenerationModels.Vision; vision != nil {
		t.Fatalf("external run unexpectedly froze the managed-only vision model: %#v", vision)
	}
	var modelTaskCount int64
	if err := db.Model(&model.Task{}).Where("type = ?", agentRuntimeModelTaskType).Count(&modelTaskCount).Error; err != nil {
		t.Fatal(err)
	}
	if modelTaskCount != 0 {
		t.Fatalf("external run created %d cloud model tasks", modelTaskCount)
	}

	replayed, err := svc.StartExternalAgentRun(actor, input)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Run.ID != view.Run.ID || replayed.Run.ThreadID != view.Run.ThreadID || replayed.State.StateVersion != view.State.StateVersion {
		t.Fatalf("external start replay = %#v, want run %s thread %s version %d", replayed, view.Run.ID, view.Run.ThreadID, view.State.StateVersion)
	}
}

func TestSubmitExternalAgentDecisionExecutesCanonicalReadAndReplaysOnce(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	actor := &model.User{ID: "runtime-user"}
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", "runtime-canvas").
		Update("payload_json", `{"nodes":[],"connections":[],"viewport":{"x":0,"y":0,"k":1}}`).Error; err != nil {
		t.Fatal(err)
	}
	started, err := svc.StartExternalAgentRun(actor, StartExternalAgentRunInput{
		Context: context.Background(), CanvasID: "runtime-canvas", ExternalThreadID: "codex-thread-read",
		ClientRequestID: "codex-turn-read", UserMessage: "读取当前画布", MaxSteps: 8,
		Configuration: AgentRuntimeConfigurationInput{ExecutionMode: agentruntime.ExecutionAutomatic},
	})
	if err != nil {
		t.Fatal(err)
	}
	input := SubmitExternalAgentDecisionInput{
		ClientRequestID:      "codex-decision-read-1",
		ExpectedStateVersion: started.State.StateVersion,
		Decision: agentruntime.ModelDecision{
			Kind: agentruntime.DecisionToolCall,
			ToolCall: &agentruntime.ToolCallDecision{
				ToolCallID: "external-read-1", ToolName: agentruntime.ToolCanvasRead, ActionVersion: 1,
				Arguments:        json.RawMessage(`{"canvasId":"runtime-canvas","selectedNodeIds":[],"includeViewport":true}`),
				ExpectedDelivery: agentRuntimeTestAnswerDelivery(),
			},
		},
	}
	view, err := svc.SubmitExternalAgentDecision(actor, started.Run.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if view.State.Status != agentruntime.RunRunning || view.State.StateVersion != started.State.StateVersion+2 ||
		view.State.LastToolResult == nil || view.State.LastToolResult.ToolCallID != "external-read-1" || !view.State.LastToolResult.Succeeded {
		t.Fatalf("external read decision status=%q version=%d wantVersion=%d result=%#v", view.State.Status, view.State.StateVersion, started.State.StateVersion+2, view.State.LastToolResult)
	}
	replayed, err := svc.SubmitExternalAgentDecision(actor, started.Run.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.State.StateVersion != view.State.StateVersion || replayed.State.LastToolResult == nil ||
		replayed.State.LastToolResult.ToolCallID != view.State.LastToolResult.ToolCallID {
		t.Fatalf("external decision replay = %#v", replayed)
	}
	conflict := input
	conflict.Decision.ToolCall = &agentruntime.ToolCallDecision{
		ToolCallID: "external-read-conflict", ToolName: agentruntime.ToolCanvasRead, ActionVersion: 1,
		Arguments:        json.RawMessage(`{"canvasId":"runtime-canvas","selectedNodeIds":[],"includeViewport":true}`),
		ExpectedDelivery: agentRuntimeTestAnswerDelivery(),
	}
	if _, err := svc.SubmitExternalAgentDecision(actor, started.Run.ID, conflict); !errors.Is(err, agentruntime.ErrExternalDecisionConflict) {
		t.Fatalf("conflicting external decision error = %v", err)
	}
	if _, err := svc.SubmitExternalAgentDecision(&model.User{ID: "other-user"}, started.Run.ID, SubmitExternalAgentDecisionInput{
		ClientRequestID: "other-user-decision", ExpectedStateVersion: view.State.StateVersion,
		Decision: agentruntime.ModelDecision{Kind: agentruntime.DecisionFinal, Final: &agentruntime.FinalDecision{
			Message: "不应成功", ExpectedDelivery: agentRuntimeTestAnswerDelivery(),
		}},
	}); err == nil {
		t.Fatal("cross-user external decision was accepted")
	}
	if _, err := svc.SubmitExternalAgentDecision(actor, started.Run.ID, SubmitExternalAgentDecisionInput{
		ClientRequestID: "stale-decision", ExpectedStateVersion: started.State.StateVersion,
		Decision: agentruntime.ModelDecision{Kind: agentruntime.DecisionFinal, Final: &agentruntime.FinalDecision{
			Message: "不应成功", ExpectedDelivery: agentRuntimeTestAnswerDelivery(),
		}},
	}); !errors.Is(err, agentruntime.ErrExternalDecisionConflict) {
		t.Fatalf("stale external decision error = %v", err)
	}
	completed, err := svc.SubmitExternalAgentDecision(actor, started.Run.ID, SubmitExternalAgentDecisionInput{
		ClientRequestID: "codex-decision-final-1", ExpectedStateVersion: view.State.StateVersion,
		Decision: agentruntime.ModelDecision{Kind: agentruntime.DecisionFinal, Final: &agentruntime.FinalDecision{
			Message: "画布读取完成。", ExpectedDelivery: agentRuntimeTestAnswerDelivery(),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if completed.State.Status != agentruntime.RunSucceeded || completed.State.FinalMessage != "画布读取完成。" ||
		completed.State.Verification == nil || completed.State.Verification.Status != agentruntime.VerificationSatisfied {
		t.Fatalf("external final decision = %#v", completed)
	}
	var receiptCount, toolCount, modelTaskCount int64
	if err := db.Model(&model.AgentExternalDecision{}).Where("run_id = ?", started.Run.ID).Count(&receiptCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentToolCall{}).Where("run_id = ? AND tool_call_id = ?", started.Run.ID, "external-read-1").Count(&toolCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("type = ?", agentRuntimeModelTaskType).Count(&modelTaskCount).Error; err != nil {
		t.Fatal(err)
	}
	if receiptCount != 2 || toolCount != 1 || modelTaskCount != 0 {
		t.Fatalf("external decision facts: receipts=%d tools=%d modelTasks=%d", receiptCount, toolCount, modelTaskCount)
	}
}

func TestCommitExternalAgentDecisionMapsConcurrentStateChangeToExternalConflict(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	actor := &model.User{ID: "runtime-user"}
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", "runtime-canvas").
		Update("payload_json", `{"nodes":[],"connections":[],"viewport":{"x":0,"y":0,"k":1}}`).Error; err != nil {
		t.Fatal(err)
	}
	started, err := svc.StartExternalAgentRun(actor, StartExternalAgentRunInput{
		Context: context.Background(), CanvasID: "runtime-canvas", ExternalThreadID: "codex-thread-concurrent-conflict",
		ClientRequestID: "codex-turn-concurrent-conflict", UserMessage: "读取当前画布", MaxSteps: 8,
		Configuration: AgentRuntimeConfigurationInput{ExecutionMode: agentruntime.ExecutionAutomatic},
	})
	if err != nil {
		t.Fatal(err)
	}
	first := SubmitExternalAgentDecisionInput{
		ClientRequestID: "codex-concurrent-winner", ExpectedStateVersion: started.State.StateVersion,
		Decision: agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "external-concurrent-winner", ToolName: agentruntime.ToolCanvasRead, ActionVersion: 1,
			Arguments:        json.RawMessage(`{"canvasId":"runtime-canvas","selectedNodeIds":[],"includeViewport":true}`),
			ExpectedDelivery: agentRuntimeTestAnswerDelivery(),
		}},
	}
	if _, err := svc.SubmitExternalAgentDecision(actor, started.Run.ID, first); err != nil {
		t.Fatal(err)
	}
	scope, err := svc.scopeForAgentRun(actor, started.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	loser := SubmitExternalAgentDecisionInput{
		ClientRequestID: "codex-concurrent-loser", ExpectedStateVersion: started.State.StateVersion,
		Decision: agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "external-concurrent-loser", ToolName: agentruntime.ToolCanvasRead, ActionVersion: 1,
			Arguments:        json.RawMessage(`{"canvasId":"runtime-canvas","selectedNodeIds":[],"includeViewport":true}`),
			ExpectedDelivery: agentRuntimeTestAnswerDelivery(),
		}},
	}
	transition, err := agentruntime.AdvanceExternalDecision(started.State, agentruntime.ExternalDecisionInput{
		ExpectedStateVersion: loser.ExpectedStateVersion,
		Decision:             loser.Decision,
		Evidence:             agentruntime.DeliveryEvidence{},
	})
	if err != nil {
		t.Fatal(err)
	}
	requestHash, err := externalAgentDecisionRequestHash(loser)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.commitAndCompleteExternalDecision(scope, loser, requestHash, started.State, transition); !errors.Is(err, agentruntime.ErrExternalDecisionConflict) {
		t.Fatalf("concurrent state change error = %v", err)
	}
}

func TestSubmitExternalAgentDecisionStopsWriteAtCanonicalApproval(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	actor := &model.User{ID: "runtime-user"}
	started, err := svc.StartExternalAgentRun(actor, StartExternalAgentRunInput{
		Context: context.Background(), CanvasID: "runtime-canvas", ExternalThreadID: "codex-thread-write",
		ClientRequestID: "codex-turn-write", UserMessage: "在画布添加文本节点", MaxSteps: 8,
		Configuration: AgentRuntimeConfigurationInput{ExecutionMode: agentruntime.ExecutionAutomatic},
	})
	if err != nil {
		t.Fatal(err)
	}
	view, err := svc.SubmitExternalAgentDecision(actor, started.Run.ID, SubmitExternalAgentDecisionInput{
		ClientRequestID: "codex-decision-write-1", ExpectedStateVersion: started.State.StateVersion,
		Decision: agentruntime.ModelDecision{
			Kind: agentruntime.DecisionToolCall,
			ToolCall: &agentruntime.ToolCallDecision{
				ToolCallID: "external-write-1", ToolName: agentruntime.ToolCanvasApplyOps, ActionVersion: 1,
				Arguments:        json.RawMessage(`{"canvasId":"runtime-canvas","baseRevision":7,"clientMutationId":"external-write-1","operations":[{"operationId":"external-add-1","type":"add_node","node":{"id":"external-node-1","type":"text","title":"待审批节点","position":{"x":0,"y":0},"width":240,"height":120,"metadata":{}}}]}`),
				ExpectedDelivery: agentRuntimeTestCanvasDelivery(),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.State.Status != agentruntime.RunWaitingApproval || view.State.PendingToolCall == nil ||
		view.State.PendingToolCall.ToolCallID != "external-write-1" || view.PendingApproval == nil ||
		view.PendingApproval.ProposalHash == "" {
		t.Fatalf("external write approval view = %#v", view)
	}
	var canvas model.CanvasProject
	if err := db.Where("id = ?", "runtime-canvas").Take(&canvas).Error; err != nil {
		t.Fatal(err)
	}
	if canvas.Revision != 7 || strings.Contains(canvas.PayloadJSON, "external-node-1") {
		t.Fatalf("write executed before approval: revision=%d payload=%s", canvas.Revision, canvas.PayloadJSON)
	}
	var modelTaskCount int64
	if err := db.Model(&model.Task{}).Where("type = ?", agentRuntimeModelTaskType).Count(&modelTaskCount).Error; err != nil {
		t.Fatal(err)
	}
	if modelTaskCount != 0 {
		t.Fatalf("external write created %d cloud model tasks", modelTaskCount)
	}
}

func TestSubmitExternalAgentDecisionReturnsInvalidMediaArgumentsForLocalRepair(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeImageModel(t, db, fixture)
	actor := &model.User{ID: "runtime-user"}
	started, err := svc.StartExternalAgentRun(actor, StartExternalAgentRunInput{
		Context: context.Background(), CanvasID: "runtime-canvas", ExternalThreadID: "codex-thread-invalid-media",
		ClientRequestID: "codex-turn-invalid-media", UserMessage: "生成一张图片", MaxSteps: 8,
		Configuration: AgentRuntimeConfigurationInput{ExecutionMode: agentruntime.ExecutionAutomatic},
	})
	if err != nil {
		t.Fatal(err)
	}

	view, err := svc.SubmitExternalAgentDecision(actor, started.Run.ID, SubmitExternalAgentDecisionInput{
		ClientRequestID:      "codex-decision-invalid-media",
		ExpectedStateVersion: started.State.StateVersion,
		Decision: agentruntime.ModelDecision{
			Kind: agentruntime.DecisionToolCall,
			ToolCall: &agentruntime.ToolCallDecision{
				ToolCallID: "external-invalid-media", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
				Arguments:        json.RawMessage(`{"mediaKind":"image","modelRecordId":"runtime-image-model","modelKey":"kz_gpt_image2","parameters":{"prompt":"蓝色玻璃球","aspectRatio":"1:1","resolution":"1K","quality":"standard","count":1,"transparentBackground":false},"sourceResourceIds":[],"targetCanvasNodeId":"image-node-1","clientRequestId":"external-invalid-media-request"}`),
				ExpectedDelivery: agentRuntimeTestImageDelivery(),
			},
		},
	})
	if err != nil {
		t.Fatalf("invalid external media decision must return repair feedback: %v", err)
	}
	if view.State.Status != agentruntime.RunRunning || view.State.StepNumber != 1 || view.State.DecisionFeedback == nil ||
		view.State.DecisionFeedback.Code != "model_decision_invalid" || view.State.PendingToolCall != nil || view.PendingApproval != nil {
		t.Fatalf("invalid external media decision repair view = %#v", view)
	}
	var toolCallCount, modelTaskCount int64
	if err := db.Model(&model.AgentToolCall{}).Where("run_id = ?", started.Run.ID).Count(&toolCallCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("type = ?", agentRuntimeModelTaskType).Count(&modelTaskCount).Error; err != nil {
		t.Fatal(err)
	}
	if toolCallCount != 0 || modelTaskCount != 0 {
		t.Fatalf("invalid external media decision created side effects: toolCalls=%d modelTasks=%d", toolCallCount, modelTaskCount)
	}
}

func TestSubmitExternalAgentDecisionReplaysCompletedMediaAcrossRunsWithoutApprovalOrCharge(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeImageModel(t, db, fixture)
	actor := &model.User{ID: "runtime-user"}
	configuration := AgentRuntimeConfigurationInput{ExecutionMode: agentruntime.ExecutionAutomatic}
	first, err := svc.StartExternalAgentRun(actor, StartExternalAgentRunInput{
		Context: context.Background(), CanvasID: "runtime-canvas", ExternalThreadID: "codex-thread-media-replay",
		ClientRequestID: "codex-turn-media-original", UserMessage: "生成一张鲜橙产品图", MaxSteps: 8,
		Configuration: configuration,
	})
	if err != nil {
		t.Fatal(err)
	}
	originalCall := agentMediaCapabilityCall("external-media-original", "stable-media-request")
	originalCall.ExpectedDelivery = agentRuntimeTestImageDelivery()
	waiting, err := svc.SubmitExternalAgentDecision(actor, first.Run.ID, SubmitExternalAgentDecisionInput{
		ClientRequestID: "codex-decision-media-original", ExpectedStateVersion: first.State.StateVersion,
		Decision: agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &originalCall},
	})
	if err != nil {
		t.Fatal(err)
	}
	if waiting.PendingApproval == nil {
		t.Fatalf("original media approval is missing: %#v", waiting)
	}
	approved, err := svc.SubmitScopedAgentApproval(actor, first.Run.ID, AgentToolApprovalSubmission{
		ToolCallID: originalCall.ToolCallID, ActionVersion: originalCall.ActionVersion,
		Decision: agentruntime.ToolApprovalApproved, ProposalHash: waiting.PendingApproval.ProposalHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if approved.State.Status != agentruntime.RunWaitingTool || !approved.State.PendingToolStarted {
		t.Fatalf("approved original media state = %#v", approved.State)
	}
	originalScope, err := svc.scopeForAgentRun(actor, first.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var task model.Task
	if err := db.Where("operation = ?", agentMediaGenerationOperationForRun(originalScope.RunID)).First(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := finalizeAgentMediaCapabilityTaskWithOutbox(t, svc, db, task, "generated-media-replay", "image"); err != nil {
		t.Fatal(err)
	}
	if err := svc.dispatchTaskOutbox(time.Now().UTC().Add(time.Second), 10); err != nil {
		t.Fatal(err)
	}
	originalRecord, err := svc.repo.AgentToolCallForScope(originalScope, originalCall.ToolCallID, originalCall.ActionVersion)
	if err != nil {
		t.Fatal(err)
	}
	if originalRecord.Status != agentruntime.ToolCallSucceeded || originalRecord.OutputJSON == "" {
		t.Fatalf("original media receipt = %#v", originalRecord)
	}

	second, err := svc.StartExternalAgentRun(actor, StartExternalAgentRunInput{
		Context: context.Background(), CanvasID: "runtime-canvas", ExternalThreadID: "codex-thread-media-replay",
		ClientRequestID: "codex-turn-media-recovery", UserMessage: "恢复上一轮已经生成的鲜橙产品图", MaxSteps: 8,
		Configuration: configuration,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Run.ThreadID != first.Run.ThreadID || second.Run.ID == first.Run.ID {
		t.Fatalf("recovery run identity = run %q thread %q; original run %q thread %q", second.Run.ID, second.Run.ThreadID, first.Run.ID, first.Run.ThreadID)
	}
	replayCall := agentMediaCapabilityCall("external-media-replay", "stable-media-request")
	replayCall.ExpectedDelivery = agentRuntimeTestImageDelivery()
	replayed, err := svc.SubmitExternalAgentDecision(actor, second.Run.ID, SubmitExternalAgentDecisionInput{
		ClientRequestID: "codex-decision-media-replay", ExpectedStateVersion: second.State.StateVersion,
		Decision: agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &replayCall},
	})
	if err != nil {
		t.Fatal(err)
	}
	if replayed.PendingApproval != nil || replayed.State.Status != agentruntime.RunRunning || replayed.State.PendingToolCall != nil ||
		replayed.State.LastToolResult == nil || !replayed.State.LastToolResult.Succeeded ||
		string(replayed.State.LastToolResult.Output) != originalRecord.OutputJSON {
		t.Fatalf("cross-run media replay = %#v", replayed)
	}
	secondScope, err := svc.scopeForAgentRun(actor, second.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	replayRecord, err := svc.repo.AgentToolCallForScope(secondScope, replayCall.ToolCallID, replayCall.ActionVersion)
	if err != nil {
		t.Fatal(err)
	}
	if replayRecord.Status != agentruntime.ToolCallSucceeded || replayRecord.ApprovalRequired ||
		replayRecord.CapabilityIdempotencyKey != "" ||
		replayRecord.ReplaySourceRunID != originalScope.RunID || replayRecord.ReplaySourceToolCallID != originalCall.ToolCallID ||
		replayRecord.ReplaySourceActionVersion != originalCall.ActionVersion {
		t.Fatalf("persisted replay receipt = %#v", replayRecord)
	}
	var taskCount, orderCount, reserveCount, consumeCount int64
	if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("task_id = ?", task.ID).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ? AND type = ?", task.BillingOrderID, model.CreditLedgerReserve).Count(&reserveCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ? AND type = ?", task.BillingOrderID, model.CreditLedgerConsume).Count(&consumeCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 1 || orderCount != 1 || reserveCount != 1 || consumeCount != 1 {
		t.Fatalf("media replay commercial facts tasks=%d orders=%d reserves=%d consumes=%d", taskCount, orderCount, reserveCount, consumeCount)
	}
}

func TestSubmitExternalAgentDecisionRejectsDuplicatePaidMediaWhileOriginalIsRunning(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeImageModel(t, db, fixture)
	actor := &model.User{ID: "runtime-user"}
	configuration := AgentRuntimeConfigurationInput{ExecutionMode: agentruntime.ExecutionAutomatic}
	first, err := svc.StartExternalAgentRun(actor, StartExternalAgentRunInput{
		Context: context.Background(), CanvasID: "runtime-canvas", ExternalThreadID: "codex-thread-media-in-flight",
		ClientRequestID: "codex-turn-media-in-flight-original", UserMessage: "生成一张鲜橙产品图", MaxSteps: 8,
		Configuration: configuration,
	})
	if err != nil {
		t.Fatal(err)
	}
	originalCall := agentMediaCapabilityCall("external-media-in-flight-original", "stable-media-in-flight-request")
	originalCall.ExpectedDelivery = agentRuntimeTestImageDelivery()
	waiting, err := svc.SubmitExternalAgentDecision(actor, first.Run.ID, SubmitExternalAgentDecisionInput{
		ClientRequestID: "codex-decision-media-in-flight-original", ExpectedStateVersion: first.State.StateVersion,
		Decision: agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &originalCall},
	})
	if err != nil {
		t.Fatal(err)
	}
	if waiting.PendingApproval == nil {
		t.Fatalf("original media approval is missing: %#v", waiting)
	}
	approved, err := svc.SubmitScopedAgentApproval(actor, first.Run.ID, AgentToolApprovalSubmission{
		ToolCallID: originalCall.ToolCallID, ActionVersion: originalCall.ActionVersion,
		Decision: agentruntime.ToolApprovalApproved, ProposalHash: waiting.PendingApproval.ProposalHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if approved.State.Status != agentruntime.RunWaitingTool || !approved.State.PendingToolStarted {
		t.Fatalf("approved original media state = %#v", approved.State)
	}

	second, err := svc.StartExternalAgentRun(actor, StartExternalAgentRunInput{
		Context: context.Background(), CanvasID: "runtime-canvas", ExternalThreadID: "codex-thread-media-in-flight",
		ClientRequestID: "codex-turn-media-in-flight-retry", UserMessage: "恢复上一轮鲜橙产品图", MaxSteps: 8,
		Configuration: configuration,
	})
	if err != nil {
		t.Fatal(err)
	}
	retryCall := agentMediaCapabilityCall("external-media-in-flight-retry", "stable-media-in-flight-request")
	retryCall.ExpectedDelivery = agentRuntimeTestImageDelivery()
	rejected, err := svc.SubmitExternalAgentDecision(actor, second.Run.ID, SubmitExternalAgentDecisionInput{
		ClientRequestID: "codex-decision-media-in-flight-retry", ExpectedStateVersion: second.State.StateVersion,
		Decision: agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &retryCall},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rejected.PendingApproval != nil || rejected.State.Status != agentruntime.RunRunning ||
		rejected.State.DecisionFeedback == nil ||
		rejected.State.DecisionFeedback.Reason != "media.generate request is already paid and still running; do not request another approval or create another generation" {
		t.Fatalf("in-flight retry view = %#v", rejected)
	}
	var toolCallCount, taskCount, orderCount int64
	if err := db.Model(&model.AgentToolCall{}).Count(&toolCallCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("operation = ?", agentMediaGenerationOperationForRun(first.Run.ID)).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	if toolCallCount != 1 || taskCount != 1 || orderCount != 1 {
		t.Fatalf("in-flight retry side effects toolCalls=%d tasks=%d orders=%d", toolCallCount, taskCount, orderCount)
	}
}

func TestSubmitExternalAgentDecisionRejectsChangedArgumentsForExistingMediaRequest(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeImageModel(t, db, fixture)
	actor := &model.User{ID: "runtime-user"}
	configuration := AgentRuntimeConfigurationInput{ExecutionMode: agentruntime.ExecutionAutomatic}
	first, err := svc.StartExternalAgentRun(actor, StartExternalAgentRunInput{
		Context: context.Background(), CanvasID: "runtime-canvas", ExternalThreadID: "codex-thread-media-conflict",
		ClientRequestID: "codex-turn-media-conflict-original", UserMessage: "生成一张鲜橙产品图", MaxSteps: 8,
		Configuration: configuration,
	})
	if err != nil {
		t.Fatal(err)
	}
	originalCall := agentMediaCapabilityCall("external-media-conflict-original", "stable-media-conflict-request")
	originalCall.ExpectedDelivery = agentRuntimeTestImageDelivery()
	waiting, err := svc.SubmitExternalAgentDecision(actor, first.Run.ID, SubmitExternalAgentDecisionInput{
		ClientRequestID: "codex-decision-media-conflict-original", ExpectedStateVersion: first.State.StateVersion,
		Decision: agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &originalCall},
	})
	if err != nil {
		t.Fatal(err)
	}
	if waiting.PendingApproval == nil {
		t.Fatalf("original media approval is missing: %#v", waiting)
	}
	approved, err := svc.SubmitScopedAgentApproval(actor, first.Run.ID, AgentToolApprovalSubmission{
		ToolCallID: originalCall.ToolCallID, ActionVersion: originalCall.ActionVersion,
		Decision: agentruntime.ToolApprovalApproved, ProposalHash: waiting.PendingApproval.ProposalHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if approved.State.Status != agentruntime.RunWaitingTool || !approved.State.PendingToolStarted {
		t.Fatalf("approved original media state = %#v", approved.State)
	}

	second, err := svc.StartExternalAgentRun(actor, StartExternalAgentRunInput{
		Context: context.Background(), CanvasID: "runtime-canvas", ExternalThreadID: "codex-thread-media-conflict",
		ClientRequestID: "codex-turn-media-conflict-retry", UserMessage: "把上一轮请求改成别的图片", MaxSteps: 8,
		Configuration: configuration,
	})
	if err != nil {
		t.Fatal(err)
	}
	changedCall := agentMediaCapabilityCall("external-media-conflict-retry", "stable-media-conflict-request")
	changedCall.Arguments = json.RawMessage(strings.Replace(string(changedCall.Arguments), "鲜橙产品特写", "蓝色玻璃球", 1))
	changedCall.ExpectedDelivery = agentRuntimeTestImageDelivery()
	_, err = svc.SubmitExternalAgentDecision(actor, second.Run.ID, SubmitExternalAgentDecisionInput{
		ClientRequestID: "codex-decision-media-conflict-retry", ExpectedStateVersion: second.State.StateVersion,
		Decision: agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &changedCall},
	})
	if err == nil || !strings.Contains(err.Error(), "clientRequestId was already used with different arguments") {
		t.Fatalf("changed media request conflict error = %v", err)
	}
	var toolCallCount, taskCount, orderCount int64
	if err := db.Model(&model.AgentToolCall{}).Count(&toolCallCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	if toolCallCount != 1 || taskCount != 1 || orderCount != 1 {
		t.Fatalf("changed retry side effects toolCalls=%d tasks=%d orders=%d", toolCallCount, taskCount, orderCount)
	}
}

func TestExternalAgentApprovalExecutesFrozenWriteAndWaitsForNextDecision(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	actor := &model.User{ID: "runtime-user"}
	started, err := svc.StartExternalAgentRun(actor, StartExternalAgentRunInput{
		Context: context.Background(), CanvasID: "runtime-canvas", ExternalThreadID: "codex-thread-approved-write",
		ClientRequestID: "codex-turn-approved-write", UserMessage: "在画布添加文本节点", MaxSteps: 8,
		Configuration: AgentRuntimeConfigurationInput{ExecutionMode: agentruntime.ExecutionAutomatic},
	})
	if err != nil {
		t.Fatal(err)
	}
	waiting, err := svc.SubmitExternalAgentDecision(actor, started.Run.ID, SubmitExternalAgentDecisionInput{
		ClientRequestID: "codex-decision-approved-write", ExpectedStateVersion: started.State.StateVersion,
		Decision: agentruntime.ModelDecision{
			Kind: agentruntime.DecisionToolCall,
			ToolCall: &agentruntime.ToolCallDecision{
				ToolCallID: "external-approved-write", ToolName: agentruntime.ToolCanvasApplyOps, ActionVersion: 1,
				Arguments:        json.RawMessage(`{"canvasId":"runtime-canvas","baseRevision":7,"clientMutationId":"external-approved-write","operations":[{"operationId":"external-approved-add","type":"add_node","node":{"id":"external-approved-node","type":"text","title":"已审批节点","position":{"x":0,"y":0},"width":240,"height":120,"metadata":{}}}]}`),
				ExpectedDelivery: agentRuntimeTestCanvasDelivery(),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if waiting.PendingApproval == nil {
		t.Fatalf("external approval proposal is missing: %#v", waiting)
	}
	approved, err := svc.SubmitScopedAgentApproval(actor, started.Run.ID, AgentToolApprovalSubmission{
		ToolCallID: "external-approved-write", ActionVersion: 1,
		Decision: agentruntime.ToolApprovalApproved, ProposalHash: waiting.PendingApproval.ProposalHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if approved.Run.ReasoningHost != agentruntime.ReasoningHostLocalCodex || approved.State.Status != agentruntime.RunRunning ||
		approved.State.PendingToolCall != nil || approved.State.LastToolResult == nil || !approved.State.LastToolResult.Succeeded {
		t.Fatalf("external approved write did not return to external decision boundary: %#v", approved)
	}
	var canvas model.CanvasProject
	if err := db.Where("id = ?", "runtime-canvas").Take(&canvas).Error; err != nil {
		t.Fatal(err)
	}
	if canvas.Revision != 8 || !strings.Contains(canvas.PayloadJSON, "external-approved-node") {
		t.Fatalf("approved write was not applied exactly once: revision=%d payload=%s", canvas.Revision, canvas.PayloadJSON)
	}
	var modelTaskCount int64
	if err := db.Model(&model.Task{}).Where("type = ?", agentRuntimeModelTaskType).Count(&modelTaskCount).Error; err != nil {
		t.Fatal(err)
	}
	if modelTaskCount != 0 {
		t.Fatalf("external approval created %d cloud model tasks", modelTaskCount)
	}
}
