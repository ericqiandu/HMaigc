package agentruntime_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
)

func TestCloudAgentRuntimeToolBudgetFailsBeforeAcceptingAnExtraTool(t *testing.T) {
	startedAt := time.Date(2026, time.September, 1, 8, 0, 0, 0, time.UTC)
	answer := agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryAnswer,
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}},
	}
	current := agentruntime.RuntimeState{
		StateVersion: 1, StepNumber: 0, MaxSteps: 6, Status: agentruntime.RunRunning,
		UserMessage:   "读取画布后再读取资产",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic},
		Limits: &agentruntime.RuntimeLimits{
			MaxToolCalls: 1, ToolCallsUsed: 0, StartedAt: startedAt, DeadlineAt: startedAt.Add(30 * time.Minute),
		},
	}
	first, err := agentruntime.Advance(current, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall,
		ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "read-canvas", ToolName: agentruntime.ToolCanvasRead, ActionVersion: 1,
			Arguments: validCapabilityArgumentsForTest(agentruntime.ToolCanvasRead), ExpectedDelivery: answer,
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if first.State.Status != agentruntime.RunWaitingTool || first.State.Limits == nil || first.State.Limits.ToolCallsUsed != 1 {
		t.Fatalf("first tool transition = %#v", first.State)
	}
	if current.Limits == nil || current.Limits.ToolCallsUsed != 0 {
		t.Fatalf("advance mutated the previous checkpoint limits = %#v", current.Limits)
	}
	resolved, err := agentruntime.ResolveTool(first.State, agentruntime.ToolResolution{
		ToolCallID: "read-canvas", ActionVersion: 1, Succeeded: true, Output: json.RawMessage(`{"canvasId":"canvas-1"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := agentruntime.Advance(resolved.State, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall,
		ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "read-assets", ToolName: agentruntime.ToolAssetsRead, ActionVersion: 1,
			Arguments: validCapabilityArgumentsForTest(agentruntime.ToolAssetsRead), ExpectedDelivery: answer,
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if second.State.Status != agentruntime.RunFailed || second.State.FailureCode != "tool_call_budget_exhausted" || second.State.PendingToolCall != nil {
		t.Fatalf("exhausted tool transition = %#v", second.State)
	}
}

func TestCloudAgentRuntimeDeadlineSurvivesRecoveryAndFailsExplicitly(t *testing.T) {
	startedAt := time.Date(2026, time.September, 1, 8, 0, 0, 0, time.UTC)
	current := agentruntime.RuntimeState{
		StateVersion: 4, StepNumber: 2, MaxSteps: 6, Status: agentruntime.RunRunning,
		UserMessage:   "继续执行",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided},
		Limits: &agentruntime.RuntimeLimits{
			MaxToolCalls: 4, ToolCallsUsed: 1, StartedAt: startedAt, DeadlineAt: startedAt.Add(30 * time.Minute),
		},
	}
	if _, expired, err := agentruntime.ExpireRuntimeAt(current, startedAt.Add(29*time.Minute)); err != nil || expired {
		t.Fatalf("runtime expired early: expired=%v err=%v", expired, err)
	}
	transition, expired, err := agentruntime.ExpireRuntimeAt(current, startedAt.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !expired || transition.State.Status != agentruntime.RunFailed || transition.State.FailureCode != "runtime_deadline_exceeded" {
		t.Fatalf("deadline transition = %#v expired=%v", transition.State, expired)
	}
	if transition.State.Limits == nil || !transition.State.Limits.DeadlineAt.Equal(startedAt.Add(30*time.Minute)) {
		t.Fatalf("deadline facts changed during recovery = %#v", transition.State.Limits)
	}
}

func TestAdvanceRuntimeTransitionsFromFacts(t *testing.T) {
	answer := agentruntime.ExpectedDelivery{Kind: agentruntime.DeliveryAnswer, CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}}}
	base := agentruntime.RuntimeState{StateVersion: 1, StepNumber: 0, MaxSteps: 3, Status: agentruntime.RunQueued, UserMessage: "请读取画布", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}}

	final := agentruntime.ModelDecision{Kind: agentruntime.DecisionFinal, Final: &agentruntime.FinalDecision{Message: "完成", ExpectedDelivery: answer}}
	transition, err := agentruntime.Advance(base, agentruntime.RuntimeInput{Decision: final, Evidence: agentruntime.DeliveryEvidence{FinalMessage: "完成"}})
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.Status != agentruntime.RunSucceeded || transition.State.StepNumber != 1 || transition.State.StateVersion != 2 {
		t.Fatalf("satisfied transition = %#v", transition)
	}
	if len(transition.EventKinds) != 2 || transition.EventKinds[0] != agentruntime.EventAgentMessageCompleted || transition.EventKinds[1] != agentruntime.EventRunCompleted {
		t.Fatalf("final events = %#v", transition.EventKinds)
	}

	repairable, err := agentruntime.Advance(base, agentruntime.RuntimeInput{Decision: final})
	if err != nil {
		t.Fatal(err)
	}
	if repairable.State.Status != agentruntime.RunRunning || repairable.State.Verification == nil || repairable.State.Verification.Status != agentruntime.VerificationRepairable {
		t.Fatalf("repairable transition = %#v", repairable)
	}

	tool := agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{ToolCallID: "call-1", ToolName: agentruntime.ToolCanvasRead, ActionVersion: 1, Arguments: validCapabilityArgumentsForTest(agentruntime.ToolCanvasRead), ExpectedDelivery: answer}}
	waiting, err := agentruntime.Advance(base, agentruntime.RuntimeInput{Decision: tool})
	if err != nil {
		t.Fatal(err)
	}
	if waiting.State.Status != agentruntime.RunWaitingTool || waiting.State.PendingToolCall == nil {
		t.Fatalf("tool transition = %#v", waiting)
	}

	writeTool := agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{ToolCallID: "call-2", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1, Arguments: validCapabilityArgumentsForTest(agentruntime.ToolMediaGenerate), ExpectedDelivery: answer}}
	waitingApproval, err := agentruntime.Advance(base, agentruntime.RuntimeInput{Decision: writeTool})
	if err != nil {
		t.Fatal(err)
	}
	if waitingApproval.State.Status != agentruntime.RunWaitingApproval || waitingApproval.State.PendingToolCall == nil {
		t.Fatalf("approval transition = %#v", waitingApproval)
	}
}

func TestAdvanceForCurrentToolSchemaRequiresApprovalForAutomaticWrites(t *testing.T) {
	t.Parallel()

	answer := agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryAnswer,
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}},
	}
	base := agentruntime.RuntimeState{
		StateVersion: 1,
		StepNumber:   0,
		MaxSteps:     4,
		Status:       agentruntime.RunRunning,
		UserMessage:  "创建生产图",
		Configuration: agentruntime.RunConfiguration{
			ExecutionMode: agentruntime.ExecutionAutomatic,
		},
	}
	decision := agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall,
		ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID:       "publish-1",
			ToolName:         agentruntime.ToolAssetsPublish,
			ActionVersion:    1,
			Arguments:        validCapabilityArgumentsForTest(agentruntime.ToolAssetsPublish),
			ExpectedDelivery: answer,
		},
	}

	transition, err := agentruntime.AdvanceForToolSchema(base, agentruntime.RuntimeInput{Decision: decision}, agentruntime.CurrentToolSchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.Status != agentruntime.RunWaitingApproval {
		t.Fatalf("expected automatic L1 write to wait for approval, got %s", transition.State.Status)
	}

	if _, err := agentruntime.AdvanceForToolSchema(base, agentruntime.RuntimeInput{Decision: decision}, agentruntime.LegacyToolSchemaVersion); err == nil {
		t.Fatal("expected the legacy schema to reject the production-only tool")
	}
}

func TestBeginModelRequestChangesOnlyQueuedRuntimeStatus(t *testing.T) {
	queued := agentruntime.RuntimeState{
		StateVersion: 1, StepNumber: 0, MaxSteps: 3, Status: agentruntime.RunQueued,
		UserMessage: "请回答", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided},
	}
	transition, err := agentruntime.BeginModelRequest(queued)
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.Status != agentruntime.RunRunning || transition.State.StateVersion != 2 || transition.State.StepNumber != 0 {
		t.Fatalf("model request transition = %#v", transition.State)
	}
	if len(transition.EventKinds) != 1 || transition.EventKinds[0] != agentruntime.EventRunStatusChanged {
		t.Fatalf("model request events = %#v", transition.EventKinds)
	}
	if _, err := agentruntime.BeginModelRequest(transition.State); err == nil {
		t.Fatal("running runtime accepted as a new model request")
	}
}

func TestAgentRuntimeSkillLoadIsRequiredBeforeFinal(t *testing.T) {
	answer := agentruntime.ExpectedDelivery{Kind: agentruntime.DeliveryAnswer, CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}}}
	base := agentruntime.RuntimeState{
		StateVersion: 1, StepNumber: 0, MaxSteps: 6, Status: agentruntime.RunQueued,
		UserMessage: "使用已选 Skill 规划短剧",
		Configuration: agentruntime.RunConfiguration{
			ExecutionMode: agentruntime.ExecutionAutomatic,
			Skills: []agentruntime.SkillSelection{{
				Dir: "storyboard-director", Name: "分镜导演", Description: "拆解镜头",
				Instructions: "先建立可执行镜头计划。", Version: 7, Checksum: testSkillChecksum("先建立可执行镜头计划。"),
			}},
		},
	}
	final := agentruntime.ModelDecision{Kind: agentruntime.DecisionFinal, Final: &agentruntime.FinalDecision{Message: "已完成", ExpectedDelivery: answer}}
	rejected, err := agentruntime.Advance(base, agentruntime.RuntimeInput{Decision: final, Evidence: agentruntime.DeliveryEvidence{FinalMessage: "已完成"}})
	if err != nil {
		t.Fatal(err)
	}
	if rejected.State.Status != agentruntime.RunRunning || rejected.State.DecisionFeedback == nil || rejected.State.DecisionFeedback.Code != "required_skill_not_loaded" {
		t.Fatalf("unloaded skill final = %#v", rejected.State)
	}
	if rejected.State.DecisionFeedback.Reason != "load the missing selected skills before final delivery: storyboard-director" {
		t.Fatalf("unloaded skill feedback = %#v", rejected.State.DecisionFeedback)
	}

	load := agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{
		ToolCallID: "load-storyboard", ToolName: agentruntime.ToolSkillsLoad, ActionVersion: 1,
		Arguments: validCapabilityArgumentsForTest(agentruntime.ToolSkillsLoad), ExpectedDelivery: answer,
	}}
	waiting, err := agentruntime.Advance(rejected.State, agentruntime.RuntimeInput{Decision: load})
	if err != nil {
		t.Fatal(err)
	}
	if waiting.State.Status != agentruntime.RunWaitingTool {
		t.Fatalf("skill load state = %#v", waiting.State)
	}
	loaded, err := agentruntime.ResolveTool(waiting.State, agentruntime.ToolResolution{
		ToolCallID: "load-storyboard", ActionVersion: 1, Succeeded: true,
		Output: json.RawMessage(`{"dir":"storyboard-director","name":"分镜导演","version":7,"instructions":"先建立可执行镜头计划。"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.State.LoadedSkillDirs) != 1 || loaded.State.LoadedSkillDirs[0] != "storyboard-director" {
		t.Fatalf("loaded skill dirs = %#v", loaded.State.LoadedSkillDirs)
	}
	completed, err := agentruntime.Advance(loaded.State, agentruntime.RuntimeInput{Decision: final, Evidence: agentruntime.DeliveryEvidence{FinalMessage: "已完成"}})
	if err != nil {
		t.Fatal(err)
	}
	if completed.State.Status != agentruntime.RunSucceeded {
		t.Fatalf("loaded skill final = %#v", completed.State)
	}
}

func TestAgentRuntimeSkillFeedbackNamesOnlyTheMissingSelection(t *testing.T) {
	answer := agentruntime.ExpectedDelivery{Kind: agentruntime.DeliveryAnswer, CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}}}
	base := agentruntime.RuntimeState{
		StateVersion: 3, StepNumber: 2, MaxSteps: 6, Status: agentruntime.RunRunning,
		UserMessage: "使用已选 Skill 规划广告短剧",
		Configuration: agentruntime.RunConfiguration{
			ExecutionMode: agentruntime.ExecutionAutomatic,
			Skills: []agentruntime.SkillSelection{
				{Dir: "commercial-film-director", Name: "商业广告导演", Instructions: "建立广告诉求。", Version: 1, Checksum: testSkillChecksum("建立广告诉求。")},
				{Dir: "short-drama-director", Name: "短剧总导演", Instructions: "建立短剧计划。", Version: 3, Checksum: testSkillChecksum("建立短剧计划。")},
			},
		},
		LoadedSkillDirs: []string{"short-drama-director"},
	}
	final := agentruntime.ModelDecision{Kind: agentruntime.DecisionFinal, Final: &agentruntime.FinalDecision{Message: "已完成", ExpectedDelivery: answer}}

	rejected, err := agentruntime.Advance(base, agentruntime.RuntimeInput{Decision: final, Evidence: agentruntime.DeliveryEvidence{FinalMessage: "已完成"}})
	if err != nil {
		t.Fatal(err)
	}
	if rejected.State.DecisionFeedback == nil || rejected.State.DecisionFeedback.Reason != "load the missing selected skills before final delivery: commercial-film-director" {
		t.Fatalf("missing skill feedback = %#v", rejected.State.DecisionFeedback)
	}
}

func TestAdvanceRuntimeFreezesExpectedDeliveryAndRejectsFinalDowngrade(t *testing.T) {
	generatedImage := agentruntime.ExpectedDelivery{
		Kind:              agentruntime.DeliveryGeneratedAsset,
		RequiredArtifacts: []agentruntime.ArtifactKind{agentruntime.ArtifactImage},
		CompletionCriteria: []agentruntime.DeliveryCriterion{{
			Fact: agentruntime.DeliveryFactArtifact, Artifact: agentruntime.ArtifactImage,
		}},
	}
	answer := agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryAnswer,
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}},
	}
	base := agentruntime.RuntimeState{
		StateVersion: 1, StepNumber: 0, MaxSteps: 8, Status: agentruntime.RunQueued,
		UserMessage: "生成一张图片", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided},
	}
	firstDecision := agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall,
		ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "read-canvas", ToolName: agentruntime.ToolCanvasRead, ActionVersion: 1,
			Arguments: validCapabilityArgumentsForTest(agentruntime.ToolCanvasRead), ExpectedDelivery: generatedImage,
		},
	}
	waiting, err := agentruntime.Advance(base, agentruntime.RuntimeInput{Decision: firstDecision})
	if err != nil {
		t.Fatal(err)
	}
	if waiting.State.ExpectedDelivery == nil || waiting.State.ExpectedDelivery.Kind != agentruntime.DeliveryGeneratedAsset {
		t.Fatalf("frozen expected delivery = %#v", waiting.State.ExpectedDelivery)
	}

	running := waiting.State
	running.StateVersion++
	running.Status = agentruntime.RunRunning
	running.PendingToolCall = nil
	running.LastToolResult = &agentruntime.ToolResult{
		ToolCallID: "wait-image", ActionVersion: 1, Succeeded: false,
		Output: json.RawMessage(`{"status":"failed"}`), ErrorCode: "generation_failed",
	}
	downgradedFinal := agentruntime.ModelDecision{
		Kind:  agentruntime.DecisionFinal,
		Final: &agentruntime.FinalDecision{Message: "图片生成失败", ExpectedDelivery: answer},
	}
	rejected, err := agentruntime.Advance(running, agentruntime.RuntimeInput{
		Decision: downgradedFinal,
		Evidence: agentruntime.DeliveryEvidence{FinalMessage: "图片生成失败"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rejected.State.Status != agentruntime.RunRunning || rejected.State.DecisionFeedback == nil ||
		rejected.State.DecisionFeedback.Code != "delivery_contract_changed" || rejected.State.ExpectedDelivery == nil ||
		rejected.State.ExpectedDelivery.Kind != agentruntime.DeliveryGeneratedAsset || rejected.State.FinalMessage != "" {
		t.Fatalf("downgraded final transition = %#v", rejected.State)
	}
}

func TestApprovalMatrixDependsOnExecutionModeAndToolRisk(t *testing.T) {
	answer := agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryAnswer,
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}},
	}
	testCases := []struct {
		name       string
		mode       agentruntime.ExecutionMode
		tool       agentruntime.ToolName
		wantStatus agentruntime.RunStatus
	}{
		{name: "guided read runs immediately", mode: agentruntime.ExecutionGuided, tool: agentruntime.ToolCanvasRead, wantStatus: agentruntime.RunWaitingTool},
		{name: "guided asset publish requires approval", mode: agentruntime.ExecutionGuided, tool: agentruntime.ToolAssetsPublish, wantStatus: agentruntime.RunWaitingApproval},
		{name: "guided canvas write requires approval", mode: agentruntime.ExecutionGuided, tool: agentruntime.ToolCanvasApplyOps, wantStatus: agentruntime.RunWaitingApproval},
		{name: "guided paid media requires approval", mode: agentruntime.ExecutionGuided, tool: agentruntime.ToolMediaGenerate, wantStatus: agentruntime.RunWaitingApproval},
		{name: "automatic read runs immediately", mode: agentruntime.ExecutionAutomatic, tool: agentruntime.ToolAssetsRead, wantStatus: agentruntime.RunWaitingTool},
		{name: "automatic asset publish requires approval", mode: agentruntime.ExecutionAutomatic, tool: agentruntime.ToolAssetsPublish, wantStatus: agentruntime.RunWaitingApproval},
		{name: "automatic canvas write requires approval", mode: agentruntime.ExecutionAutomatic, tool: agentruntime.ToolCanvasApplyOps, wantStatus: agentruntime.RunWaitingApproval},
		{name: "automatic paid media requires approval", mode: agentruntime.ExecutionAutomatic, tool: agentruntime.ToolMediaGenerate, wantStatus: agentruntime.RunWaitingApproval},
	}

	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			current := agentruntime.RuntimeState{
				StateVersion: 1,
				MaxSteps:     3,
				Status:       agentruntime.RunQueued,
				UserMessage:  "执行任务",
				Configuration: agentruntime.RunConfiguration{
					ExecutionMode: testCase.mode,
				},
			}
			decision := agentruntime.ModelDecision{
				Kind: agentruntime.DecisionToolCall,
				ToolCall: &agentruntime.ToolCallDecision{
					ToolCallID:       fmt.Sprintf("call-%d", index),
					ToolName:         testCase.tool,
					ActionVersion:    1,
					Arguments:        validCapabilityArgumentsForTest(testCase.tool),
					ExpectedDelivery: answer,
				},
			}

			transition, err := agentruntime.Advance(current, agentruntime.RuntimeInput{Decision: decision})
			if err != nil {
				t.Fatal(err)
			}
			if transition.State.Status != testCase.wantStatus {
				t.Fatalf("status = %s, want %s", transition.State.Status, testCase.wantStatus)
			}
		})
	}
}

func TestAppendSteerIsIdempotentAndPreservesOriginalUserMessage(t *testing.T) {
	current := agentruntime.RuntimeState{
		StateVersion: 4,
		StepNumber:   1,
		MaxSteps:     8,
		Status:       agentruntime.RunRunning,
		UserMessage:  "制作一支三十秒广告",
		Configuration: agentruntime.RunConfiguration{
			ExecutionMode: agentruntime.ExecutionAutomatic,
		},
	}
	request := agentruntime.SteerRequest{
		ClientRequestID:      "steer-1",
		Message:              "把主角的外套统一改成红色",
		ExpectedStateVersion: 4,
	}

	appended, replayed, err := agentruntime.AppendSteer(current, request)
	if err != nil {
		t.Fatal(err)
	}
	if replayed {
		t.Fatal("first steer was reported as a replay")
	}
	if appended.State.StateVersion != 5 || appended.State.UserMessage != current.UserMessage || len(appended.State.PendingSteers) != 1 {
		t.Fatalf("appended state = %#v", appended.State)
	}
	if len(appended.EventKinds) != 1 || appended.EventKinds[0] != agentruntime.EventRunSteered {
		t.Fatalf("steer events = %#v", appended.EventKinds)
	}

	idempotent, replayed, err := agentruntime.AppendSteer(appended.State, request)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed || idempotent.State.StateVersion != appended.State.StateVersion || len(idempotent.EventKinds) != 0 {
		t.Fatalf("idempotent replay = %#v, replayed=%t", idempotent, replayed)
	}

	conflicting := request
	conflicting.Message = "把主角的外套统一改成蓝色"
	if _, _, err := agentruntime.AppendSteer(appended.State, conflicting); !errors.Is(err, agentruntime.ErrSteerConflict) {
		t.Fatalf("conflicting identity error = %v", err)
	}

	terminal := current
	terminal.Status = agentruntime.RunSucceeded
	if _, _, err := agentruntime.AppendSteer(terminal, agentruntime.SteerRequest{
		ClientRequestID:      "steer-terminal",
		Message:              "继续执行",
		ExpectedStateVersion: terminal.StateVersion,
	}); !errors.Is(err, agentruntime.ErrSteerConflict) {
		t.Fatalf("terminal steer error = %v", err)
	}
}

func TestConsumePendingSteersOnlyAtSafeBoundary(t *testing.T) {
	current := agentruntime.RuntimeState{
		StateVersion: 7,
		StepNumber:   2,
		MaxSteps:     8,
		Status:       agentruntime.RunRunning,
		UserMessage:  "制作短片",
		Configuration: agentruntime.RunConfiguration{
			ExecutionMode: agentruntime.ExecutionAutomatic,
		},
		PendingSteers: []agentruntime.PendingSteer{
			{ClientRequestID: "steer-1", Message: "服装保持一致"},
			{ClientRequestID: "steer-2", Message: "镜头节奏更快"},
		},
	}

	next, consumed, err := agentruntime.ConsumePendingSteersAtSafeBoundary(current)
	if err != nil {
		t.Fatal(err)
	}
	if next.StateVersion != current.StateVersion || len(next.PendingSteers) != 0 || len(consumed) != 2 || consumed[0].ClientRequestID != "steer-1" {
		t.Fatalf("consumed steer state = %#v, consumed=%#v", next, consumed)
	}

	unsafe := current
	unsafe.Status = agentruntime.RunWaitingTool
	unsafe.PendingToolCall = &agentruntime.ToolCallDecision{
		ToolCallID:       "render-1",
		ToolName:         agentruntime.ToolProductionRender,
		ActionVersion:    1,
		Arguments:        json.RawMessage(`{}`),
		ExpectedDelivery: agentruntime.ExpectedDelivery{Kind: agentruntime.DeliveryAnswer, CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}}},
	}
	unsafe.PendingToolStarted = true
	if _, _, err := agentruntime.ConsumePendingSteersAtSafeBoundary(unsafe); !errors.Is(err, agentruntime.ErrSteerConflict) {
		t.Fatalf("unsafe boundary error = %v", err)
	}
}

func TestInterruptUsesStateVersionAndPreservesStartedToolFacts(t *testing.T) {
	answer := agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryAnswer,
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}},
	}
	pendingApproval := agentruntime.RuntimeState{
		StateVersion: 9,
		StepNumber:   3,
		MaxSteps:     8,
		Status:       agentruntime.RunWaitingApproval,
		UserMessage:  "制作短片",
		Configuration: agentruntime.RunConfiguration{
			ExecutionMode: agentruntime.ExecutionGuided,
		},
		PendingToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "commit-1", ToolName: agentruntime.ToolCanvasCommit, ActionVersion: 1,
			Arguments: json.RawMessage(`{}`), ExpectedDelivery: answer,
		},
		PendingSteers: []agentruntime.PendingSteer{{ClientRequestID: "steer-1", Message: "先停止"}},
	}

	interrupted, err := agentruntime.Interrupt(pendingApproval, 9)
	if err != nil {
		t.Fatal(err)
	}
	if interrupted.State.Status != agentruntime.RunCancelled || interrupted.State.StateVersion != 10 ||
		interrupted.State.PendingToolCall != nil || interrupted.State.PendingToolStarted || len(interrupted.State.PendingSteers) != 0 {
		t.Fatalf("interrupted pending state = %#v", interrupted.State)
	}
	if len(interrupted.EventKinds) != 1 || interrupted.EventKinds[0] != agentruntime.EventRunInterrupted {
		t.Fatalf("interrupt events = %#v", interrupted.EventKinds)
	}

	startedPaidTool := pendingApproval
	startedPaidTool.Status = agentruntime.RunWaitingTool
	startedPaidTool.PendingToolCall = &agentruntime.ToolCallDecision{
		ToolCallID: "render-1", ToolName: agentruntime.ToolProductionRender, ActionVersion: 1,
		Arguments: json.RawMessage(`{}`), ExpectedDelivery: answer,
	}
	startedPaidTool.PendingToolStarted = true
	startedInterrupted, err := agentruntime.Interrupt(startedPaidTool, 9)
	if err != nil {
		t.Fatal(err)
	}
	if startedInterrupted.State.PendingToolCall == nil || !startedInterrupted.State.PendingToolStarted || startedInterrupted.State.LastToolResult != nil {
		t.Fatalf("started paid tool facts were rewritten = %#v", startedInterrupted.State)
	}

	if _, err := agentruntime.Interrupt(pendingApproval, 8); !errors.Is(err, agentruntime.ErrInterruptConflict) {
		t.Fatalf("version conflict error = %v", err)
	}
	terminal := pendingApproval
	terminal.Status = agentruntime.RunFailed
	terminal.PendingToolCall = nil
	if _, err := agentruntime.Interrupt(terminal, terminal.StateVersion); !errors.Is(err, agentruntime.ErrInterruptConflict) {
		t.Fatalf("terminal interrupt error = %v", err)
	}
}

func TestRunConfigurationRejectsMissingModeAndInvalidAttachmentFacts(t *testing.T) {
	if err := agentruntime.ValidateRunConfiguration(agentruntime.RunConfiguration{}); err == nil {
		t.Fatal("missing execution mode was accepted")
	}
	if err := agentruntime.ValidateRunConfiguration(agentruntime.RunConfiguration{
		ExecutionMode: agentruntime.ExecutionGuided,
		Attachments:   []agentruntime.ResourceAttachment{{ResourceID: "resource-1", Name: "参考图.png", MIMEType: ""}},
	}); err == nil {
		t.Fatal("attachment without mime type was accepted")
	}
	if err := agentruntime.ValidateRunConfiguration(agentruntime.RunConfiguration{
		ExecutionMode: agentruntime.ExecutionGuided,
		Skills: []agentruntime.SkillSelection{{
			Dir: "director", Name: "导演", Instructions: "执行导演技能。", Version: 1,
			Checksum: strings.Repeat("z", 64),
		}},
	}); err == nil {
		t.Fatal("skill selection with a non-SHA-256 checksum was accepted")
	}
	if err := agentruntime.ValidateRunConfiguration(agentruntime.RunConfiguration{
		ExecutionMode: agentruntime.ExecutionGuided,
		Skills: []agentruntime.SkillSelection{{
			Dir: "director", Name: "导演", Instructions: "执行导演技能。", Version: 1,
			Checksum: testSkillChecksum("另一份技能指令。"),
		}},
	}); err == nil {
		t.Fatal("skill selection with a checksum for different instructions was accepted")
	}
}

func testSkillChecksum(instructions string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(instructions)))
	return hex.EncodeToString(digest[:])
}

func TestResolveToolPreservesModelStepAndAdvancesStateVersion(t *testing.T) {
	current := agentruntime.RuntimeState{
		StateVersion: 2, StepNumber: 1, MaxSteps: 4, Status: agentruntime.RunWaitingTool,
		UserMessage: "读取当前画布", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided},
		PendingToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "call-1", ToolName: agentruntime.ToolCanvasCommit,
			ActionVersion: 1, Arguments: []byte(`{}`),
		},
	}
	transition, err := agentruntime.ResolveTool(current, agentruntime.ToolResolution{
		ToolCallID: "call-1", ActionVersion: 1, Succeeded: true,
		Output: []byte(`{"canvasId":"canvas-1","revision":7}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.Status != agentruntime.RunRunning || transition.State.StepNumber != 1 || transition.State.StateVersion != 3 {
		t.Fatalf("resolved state = %#v", transition.State)
	}
	if transition.State.PendingToolCall != nil || transition.State.LastToolResult == nil || !transition.State.LastToolResult.Succeeded {
		t.Fatalf("resolved tool facts = %#v", transition.State)
	}
}

func TestResolveToolOnFinalStepRecordsResultAndTerminates(t *testing.T) {
	current := agentruntime.RuntimeState{
		StateVersion: 9, StepNumber: 8, MaxSteps: 8, Status: agentruntime.RunWaitingTool,
		UserMessage: "生成图片", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided},
		PendingToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "last-tool", ToolName: agentruntime.ToolSkillsLoad, ActionVersion: 1, Arguments: json.RawMessage(`{"taskId":"task-1"}`),
		},
	}
	transition, err := agentruntime.ResolveTool(current, agentruntime.ToolResolution{
		ToolCallID: "last-tool", ActionVersion: 1, Succeeded: false, Output: json.RawMessage(`{"status":"failed"}`),
		ErrorCode: "generation_failed", FailureClass: agentruntime.ToolFailureAgentRepairable,
	})
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.Status != agentruntime.RunFailed || transition.State.FailureCode != "step_budget_exhausted" || transition.State.LastToolResult == nil {
		t.Fatalf("final-step resolution = %#v", transition.State)
	}
}

func TestResolveToolTerminalFailureEndsRunBeforeStepBudget(t *testing.T) {
	answer := agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryAnswer,
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}},
	}
	current := agentruntime.RuntimeState{
		StateVersion: 4, StepNumber: 2, MaxSteps: 8, Status: agentruntime.RunWaitingTool,
		UserMessage:   "继续执行生产计划",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided},
		PendingToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "repeat-plan", ToolName: agentruntime.ToolProductionPlan, ActionVersion: 1,
			Arguments: json.RawMessage(`{"planKey":"plan-1"}`), ExpectedDelivery: answer,
		},
		ExpectedDelivery: &answer,
	}
	transition, err := agentruntime.ResolveTool(current, agentruntime.ToolResolution{
		ToolCallID: "repeat-plan", ActionVersion: 1, Succeeded: false,
		Output:    json.RawMessage(`{"reason":"same deterministic failure repeated"}`),
		ErrorCode: "production_plan_version_conflict", FailureClass: agentruntime.ToolFailureTerminal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.Status != agentruntime.RunFailed || transition.State.FailureCode != "production_plan_version_conflict" {
		t.Fatalf("terminal tool failure state = %#v", transition.State)
	}
	if len(transition.EventKinds) != 2 || transition.EventKinds[1] != agentruntime.EventRunFailed {
		t.Fatalf("terminal tool failure events = %#v", transition.EventKinds)
	}
}

func TestBeginToolExecutionPersistsRunningInterruptionWithoutConsumingModelStep(t *testing.T) {
	current := agentruntime.RuntimeState{
		StateVersion: 3, StepNumber: 1, MaxSteps: 4, Status: agentruntime.RunWaitingTool,
		UserMessage: "在画布中增加标题节点", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided},
		PendingToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "call-apply", ToolName: agentruntime.ToolProductionRender,
			ActionVersion: 2, Arguments: json.RawMessage(`{"baseRevision":7,"patch":{"upsertNodes":[{"id":"title-node"}]}}`),
		},
	}

	transition, err := agentruntime.BeginToolExecution(current, agentruntime.ToolExecution{
		ToolCallID: "call-apply", ActionVersion: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.StateVersion != 4 || transition.State.StepNumber != 1 ||
		transition.State.Status != agentruntime.RunWaitingTool || !transition.State.PendingToolStarted {
		t.Fatalf("started state = %#v", transition.State)
	}
	if len(transition.EventKinds) != 1 || transition.EventKinds[0] != agentruntime.EventToolStarted {
		t.Fatalf("started events = %#v", transition.EventKinds)
	}
	if _, err := agentruntime.BeginToolExecution(transition.State, agentruntime.ToolExecution{
		ToolCallID: "call-apply", ActionVersion: 2,
	}); err == nil {
		t.Fatal("already started tool execution was accepted as a new transition")
	}
}

func TestCloudAgentApprovalRejectionReturnsToRunningWithFactualResult(t *testing.T) {
	current := agentruntime.RuntimeState{
		StateVersion: 4, StepNumber: 2, MaxSteps: 6, Status: agentruntime.RunWaitingApproval,
		UserMessage: "更新画布", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided},
		PendingToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "apply-ops-1", ToolName: agentruntime.ToolCanvasApplyOps,
			ActionVersion: 1,
			Arguments:     json.RawMessage(`{"canvasId":"canvas-1","baseRevision":7,"clientMutationId":"mutation-1","operations":[{"operationId":"operation-1","kind":"remove_node","nodeId":"node-1"}]}`),
		},
	}

	transition, err := agentruntime.ReviewToolApproval(current, agentruntime.ToolApproval{
		ToolCallID: "apply-ops-1", ActionVersion: 1, Decision: agentruntime.ToolApprovalRejected,
		ProposalHash: strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.Status != agentruntime.RunRunning || transition.State.PendingToolCall != nil || transition.State.PendingToolStarted {
		t.Fatalf("rejected approval state = %#v", transition.State)
	}
	if transition.State.LastToolResult == nil || transition.State.LastToolResult.Succeeded || transition.State.LastToolResult.ErrorCode != "tool_approval_rejected" {
		t.Fatalf("rejected approval result = %#v", transition.State.LastToolResult)
	}
	if transition.State.FailureCode != "" {
		t.Fatalf("rejected approval failure code = %q", transition.State.FailureCode)
	}
	if len(transition.EventKinds) != 3 || transition.EventKinds[0] != agentruntime.EventApprovalDecided ||
		transition.EventKinds[1] != agentruntime.EventToolResult || transition.EventKinds[2] != agentruntime.EventRunStatusChanged {
		t.Fatalf("rejected approval events = %#v", transition.EventKinds)
	}
}

func TestCloudAgentExpiredApprovalReturnsToRunningWithoutExecutingTool(t *testing.T) {
	current := agentruntime.RuntimeState{
		StateVersion: 7, StepNumber: 3, MaxSteps: 8, Status: agentruntime.RunWaitingApproval,
		UserMessage: "发布资产", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided},
		PendingToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "publish-asset-1", ToolName: agentruntime.ToolAssetsPublish,
			ActionVersion: 2,
			Arguments:     json.RawMessage(`{"resourceId":"resource-1","domainProjectId":"project-1","displayName":"主角参考图","clientMutationId":"publish-1"}`),
		},
	}

	transition, err := agentruntime.InvalidateToolApproval(current, agentruntime.ToolApprovalInvalidation{
		ToolCallID: "publish-asset-1", ActionVersion: 2,
		ProposalHash: strings.Repeat("b", 64), ErrorCode: agentruntime.ApprovalProposalExpired,
	})
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.Status != agentruntime.RunRunning || transition.State.PendingToolCall != nil || transition.State.PendingToolStarted {
		t.Fatalf("expired approval state = %#v", transition.State)
	}
	if transition.State.LastToolResult == nil || transition.State.LastToolResult.Succeeded || transition.State.LastToolResult.ErrorCode != agentruntime.ApprovalProposalExpired {
		t.Fatalf("expired approval result = %#v", transition.State.LastToolResult)
	}
	if transition.ApprovalProposalHash != strings.Repeat("b", 64) {
		t.Fatalf("expired approval proposal hash = %q", transition.ApprovalProposalHash)
	}
	if len(transition.EventKinds) != 2 || transition.EventKinds[0] != agentruntime.EventToolResult || transition.EventKinds[1] != agentruntime.EventRunStatusChanged {
		t.Fatalf("expired approval events = %#v", transition.EventKinds)
	}
}

func TestReviewToolApprovalMatchesFrozenAction(t *testing.T) {
	current := agentruntime.RuntimeState{
		StateVersion: 2, StepNumber: 1, MaxSteps: 4, Status: agentruntime.RunWaitingApproval,
		UserMessage: "生成一张图片", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided},
		PendingToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "call-generate", ToolName: agentruntime.ToolProductionRender,
			ActionVersion: 2, Arguments: []byte(`{"model":"image"}`),
		},
	}
	approved, err := agentruntime.ReviewToolApproval(current, agentruntime.ToolApproval{
		ToolCallID: "call-generate", ActionVersion: 2, Decision: agentruntime.ToolApprovalApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if approved.State.Status != agentruntime.RunWaitingTool || approved.State.StateVersion != 3 || approved.State.StepNumber != 1 || approved.State.PendingToolCall == nil {
		t.Fatalf("approved transition = %#v", approved)
	}
	rejected, err := agentruntime.ReviewToolApproval(current, agentruntime.ToolApproval{
		ToolCallID: "call-generate", ActionVersion: 2, Decision: agentruntime.ToolApprovalRejected,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rejected.State.Status != agentruntime.RunRunning || rejected.State.PendingToolCall != nil || rejected.State.LastToolResult == nil || rejected.State.LastToolResult.ErrorCode != "tool_approval_rejected" {
		t.Fatalf("rejected transition = %#v", rejected)
	}
	if len(rejected.EventKinds) != 3 || rejected.EventKinds[2] != agentruntime.EventRunStatusChanged {
		t.Fatalf("rejected events = %#v", rejected.EventKinds)
	}
	if _, err := agentruntime.ReviewToolApproval(current, agentruntime.ToolApproval{
		ToolCallID: "another-call", ActionVersion: 2, Decision: agentruntime.ToolApprovalApproved,
	}); err == nil {
		t.Fatal("mismatched approval identity was accepted")
	}
}

func TestReviewWriteToolRejectionReturnsToRunning(t *testing.T) {
	current := agentruntime.RuntimeState{
		StateVersion: 4, StepNumber: 2, MaxSteps: 6, Status: agentruntime.RunWaitingApproval,
		UserMessage: "更新生产计划", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided},
		PendingToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "plan-update", ToolName: agentruntime.ToolProductionPlan,
			ActionVersion: 1, Arguments: json.RawMessage(`{"planKey":"plan-1"}`),
		},
	}

	transition, err := agentruntime.ReviewToolApproval(current, agentruntime.ToolApproval{
		ToolCallID: "plan-update", ActionVersion: 1, Decision: agentruntime.ToolApprovalRejected,
	})
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.Status != agentruntime.RunRunning || transition.State.PendingToolCall != nil ||
		transition.State.LastToolResult == nil || transition.State.LastToolResult.ErrorCode != "tool_approval_rejected" {
		t.Fatalf("write rejection = %#v", transition.State)
	}
	if len(transition.EventKinds) != 3 || transition.EventKinds[2] != agentruntime.EventRunStatusChanged {
		t.Fatalf("write rejection events = %#v", transition.EventKinds)
	}
}

func TestRejectFinalStepApprovalTerminatesWithoutExecutingTool(t *testing.T) {
	current := agentruntime.RuntimeState{
		StateVersion: 9, StepNumber: 8, MaxSteps: 8, Status: agentruntime.RunWaitingApproval,
		UserMessage: "生成图片", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided},
		PendingToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "last-generation", ToolName: agentruntime.ToolProductionRender, ActionVersion: 1, Arguments: json.RawMessage(`{"type":"canvas_image"}`),
		},
	}
	transition, err := agentruntime.ReviewToolApproval(current, agentruntime.ToolApproval{
		ToolCallID: "last-generation", ActionVersion: 1, Decision: agentruntime.ToolApprovalRejected,
	})
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.Status != agentruntime.RunFailed || transition.State.FailureCode != "step_budget_exhausted" || transition.State.LastToolResult == nil || transition.State.LastToolResult.ErrorCode != "tool_approval_rejected" {
		t.Fatalf("final-step rejection = %#v", transition.State)
	}
}

func TestApproveFinalStepApprovalTerminatesWithoutExecutingTool(t *testing.T) {
	current := agentruntime.RuntimeState{
		StateVersion: 9, StepNumber: 8, MaxSteps: 8, Status: agentruntime.RunWaitingApproval,
		UserMessage: "生成图片", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided},
		PendingToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "last-generation", ToolName: agentruntime.ToolProductionRender, ActionVersion: 1, Arguments: json.RawMessage(`{"type":"canvas_image"}`),
		},
	}
	transition, err := agentruntime.ReviewToolApproval(current, agentruntime.ToolApproval{
		ToolCallID: "last-generation", ActionVersion: 1, Decision: agentruntime.ToolApprovalApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.Status != agentruntime.RunFailed || transition.State.FailureCode != "step_budget_exhausted" || transition.State.PendingToolCall != nil || transition.State.LastToolResult == nil || transition.State.LastToolResult.ErrorCode != "step_budget_exhausted" {
		t.Fatalf("final-step approval = %#v", transition.State)
	}
}

func TestAdvanceRuntimeFailsClosedAtBoundaries(t *testing.T) {
	answer := agentruntime.ExpectedDelivery{Kind: agentruntime.DeliveryAnswer, CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}}}
	final := agentruntime.ModelDecision{Kind: agentruntime.DecisionFinal, Final: &agentruntime.FinalDecision{Message: "未完成", ExpectedDelivery: answer}}

	exhausted := agentruntime.RuntimeState{StateVersion: 3, StepNumber: 2, MaxSteps: 3, Status: agentruntime.RunRunning, UserMessage: "请读取画布", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}}
	transition, err := agentruntime.Advance(exhausted, agentruntime.RuntimeInput{Decision: final})
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.Status != agentruntime.RunFailed || transition.State.FailureCode != "step_budget_exhausted" {
		t.Fatalf("exhausted transition = %#v", transition)
	}

	invalid := []agentruntime.RuntimeState{
		{StateVersion: 0, MaxSteps: 3, Status: agentruntime.RunQueued},
		{StateVersion: 1, MaxSteps: 0, Status: agentruntime.RunQueued},
		{StateVersion: 1, MaxSteps: 25, Status: agentruntime.RunQueued},
		{StateVersion: 1, MaxSteps: 3, Status: agentruntime.RunSucceeded, UserMessage: "请读取画布"},
		{StateVersion: 1, StepNumber: 4, MaxSteps: 3, Status: agentruntime.RunRunning, UserMessage: "请读取画布"},
	}
	for _, state := range invalid {
		if _, err := agentruntime.Advance(state, agentruntime.RuntimeInput{Decision: final}); err == nil {
			t.Fatalf("invalid state accepted: %#v", state)
		}
	}
}

func TestAdvanceRuntimeRejectsNewToolCallOnFinalModelStep(t *testing.T) {
	answer := agentruntime.ExpectedDelivery{Kind: agentruntime.DeliveryAnswer, CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}}}
	current := agentruntime.RuntimeState{
		StateVersion: 8, StepNumber: 7, MaxSteps: 8, Status: agentruntime.RunRunning,
		UserMessage: "生成图片", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided},
	}
	decision := agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{
		ToolCallID: "last-step-generation", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
		Arguments: validCapabilityArgumentsForTest(agentruntime.ToolMediaGenerate), ExpectedDelivery: answer,
	}}
	transition, err := agentruntime.Advance(current, agentruntime.RuntimeInput{Decision: decision})
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.Status != agentruntime.RunFailed || transition.State.FailureCode != "step_budget_exhausted" || transition.State.PendingToolCall != nil {
		t.Fatalf("final-step tool transition = %#v", transition.State)
	}
}

func TestFailRuntimeConsumesCurrentStepAndTerminates(t *testing.T) {
	base := agentruntime.RuntimeState{StateVersion: 1, StepNumber: 0, MaxSteps: 3, Status: agentruntime.RunQueued, UserMessage: "请读取画布", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}}
	transition, err := agentruntime.Fail(base, "model_task_failed")
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.Status != agentruntime.RunFailed || transition.State.StepNumber != 1 || transition.State.StateVersion != 2 || transition.State.FailureCode != "model_task_failed" {
		t.Fatalf("failure transition = %#v", transition)
	}
	if len(transition.EventKinds) != 1 || transition.EventKinds[0] != agentruntime.EventRunFailed {
		t.Fatalf("failure events = %#v", transition.EventKinds)
	}
	if _, err := agentruntime.Fail(base, "invalid code"); err == nil {
		t.Fatal("invalid failure code was accepted")
	}
}

func TestTerminatePendingToolEmitsResultBeforeRunFailure(t *testing.T) {
	current := agentruntime.RuntimeState{
		StateVersion: 2, StepNumber: 1, MaxSteps: 4, Status: agentruntime.RunWaitingTool,
		UserMessage: "生成画面", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic},
		PendingToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "call-terminate", ToolName: agentruntime.ToolCanvasCommit, ActionVersion: 1,
			Arguments: json.RawMessage(`{"expectedRevision":7}`), ExpectedDelivery: agentruntime.ExpectedDelivery{
				Kind:               agentruntime.DeliveryCanvasChange,
				CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactCanvasRevision}},
			},
		},
	}
	transition, err := agentruntime.Terminate(current, "scope_access_revoked")
	if err != nil {
		t.Fatal(err)
	}
	if len(transition.EventKinds) != 2 || transition.EventKinds[0] != agentruntime.EventToolResult || transition.EventKinds[1] != agentruntime.EventRunFailed {
		t.Fatalf("terminated tool events = %#v", transition.EventKinds)
	}
}
