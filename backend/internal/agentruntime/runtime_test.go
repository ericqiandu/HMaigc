package agentruntime_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
)

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

	repairable, err := agentruntime.Advance(base, agentruntime.RuntimeInput{Decision: final})
	if err != nil {
		t.Fatal(err)
	}
	if repairable.State.Status != agentruntime.RunRunning || repairable.State.Verification == nil || repairable.State.Verification.Status != agentruntime.VerificationRepairable {
		t.Fatalf("repairable transition = %#v", repairable)
	}

	tool := agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{ToolCallID: "call-1", ToolName: agentruntime.ToolSkillLoad, ActionVersion: 1, Arguments: []byte(`{}`), ExpectedDelivery: answer}}
	waiting, err := agentruntime.Advance(base, agentruntime.RuntimeInput{Decision: tool})
	if err != nil {
		t.Fatal(err)
	}
	if waiting.State.Status != agentruntime.RunWaitingTool || waiting.State.PendingToolCall == nil {
		t.Fatalf("tool transition = %#v", waiting)
	}

	writeTool := agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{ToolCallID: "call-2", ToolName: agentruntime.ToolProductionRender, ActionVersion: 1, Arguments: []byte(`{"baseRevision":3,"ops":[]}`), ExpectedDelivery: answer}}
	waitingApproval, err := agentruntime.Advance(base, agentruntime.RuntimeInput{Decision: writeTool})
	if err != nil {
		t.Fatal(err)
	}
	if waitingApproval.State.Status != agentruntime.RunWaitingApproval || waitingApproval.State.PendingToolCall == nil {
		t.Fatalf("approval transition = %#v", waitingApproval)
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
		ToolCallID: "load-storyboard", ToolName: agentruntime.ToolSkillLoad, ActionVersion: 1,
		Arguments: json.RawMessage(`{"dir":"storyboard-director"}`), ExpectedDelivery: answer,
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
			ToolCallID: "read-canvas", ToolName: agentruntime.ToolSkillLoad, ActionVersion: 1,
			Arguments: json.RawMessage(`{}`), ExpectedDelivery: generatedImage,
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

func TestProductionRenderApprovalCannotBeBypassedByExecutionMode(t *testing.T) {
	answer := agentruntime.ExpectedDelivery{Kind: agentruntime.DeliveryAnswer, CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}}}
	writeTool := agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{ToolCallID: "call-write", ToolName: agentruntime.ToolCanvasCommit, ActionVersion: 1, Arguments: []byte(`{"planKey":"plan-1","planVersion":1,"baseRevision":3}`), ExpectedDelivery: answer}}
	costTool := agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{ToolCallID: "call-cost", ToolName: agentruntime.ToolProductionRender, ActionVersion: 1, Arguments: []byte(`{"model":"image"}`), ExpectedDelivery: answer}}

	guided := agentruntime.RuntimeState{StateVersion: 1, MaxSteps: 3, Status: agentruntime.RunQueued, UserMessage: "修改画布", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}}
	guidedWrite, err := agentruntime.Advance(guided, agentruntime.RuntimeInput{Decision: writeTool})
	if err != nil {
		t.Fatal(err)
	}
	if guidedWrite.State.Status != agentruntime.RunWaitingTool {
		t.Fatalf("guided write state = %s", guidedWrite.State.Status)
	}

	automatic := guided
	automatic.Configuration.ExecutionMode = agentruntime.ExecutionAutomatic
	automaticWrite, err := agentruntime.Advance(automatic, agentruntime.RuntimeInput{Decision: writeTool})
	if err != nil {
		t.Fatal(err)
	}
	if automaticWrite.State.Status != agentruntime.RunWaitingTool {
		t.Fatalf("automatic write state = %s", automaticWrite.State.Status)
	}
	automaticCost, err := agentruntime.Advance(automatic, agentruntime.RuntimeInput{Decision: costTool})
	if err != nil {
		t.Fatal(err)
	}
	if automaticCost.State.Status != agentruntime.RunWaitingApproval {
		t.Fatalf("automatic cost state = %s", automaticCost.State.Status)
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
			ToolCallID: "last-tool", ToolName: agentruntime.ToolSkillLoad, ActionVersion: 1, Arguments: json.RawMessage(`{"taskId":"task-1"}`),
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
	if _, err := agentruntime.ReviewToolApproval(current, agentruntime.ToolApproval{
		ToolCallID: "another-call", ActionVersion: 2, Decision: agentruntime.ToolApprovalApproved,
	}); err == nil {
		t.Fatal("mismatched approval identity was accepted")
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
		ToolCallID: "last-step-generation", ToolName: agentruntime.ToolProductionRender, ActionVersion: 1,
		Arguments: json.RawMessage(`{"type":"canvas_image"}`), ExpectedDelivery: answer,
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
