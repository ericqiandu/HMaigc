package repository

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func TestCommitRejectedAgentToolDecisionPersistsOneFailedCallAndCheckpoint(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLiteFile(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-1", ModelKey: "gpt-5.5", MaxSteps: 4,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion:    agentruntime.CurrentRuntimeVersion,
		PolicyVersion:     agentruntime.CurrentPolicyVersion,
		UserMessage:       "生成视频",
		Configuration:     agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic}, Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	current, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	call := agentruntime.ToolCallDecision{
		ToolCallID: "render-invalid", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
		Arguments:        json.RawMessage(`{"mediaKind":"video","modelRecordId":"model-video-1","modelKey":"seedance-2.0","parameters":{"durationSeconds":15},"sourceResourceIds":[],"targetCanvasNodeId":"video-node-1","clientRequestId":"render-invalid"}`),
		ExpectedDelivery: repositoryTestImageDelivery(),
	}
	transition, err := agentruntime.RejectToolDecision(current, agentruntime.ToolDecisionFailure{
		Call: call, Class: agentruntime.ToolFailureAgentRepairable,
		ErrorCode: "generation_parameter_unsupported", Output: json.RawMessage(`{"reason":"duration is unsupported"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, current, transition, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	record, err := repo.AgentToolCallForScope(scope, call.ToolCallID, call.ActionVersion)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != agentruntime.ToolCallFailed || record.ToolName != string(call.ToolName) ||
		record.InputJSON != string(call.Arguments) || record.OutputJSON != `{"reason":"duration is unsupported"}` ||
		record.ErrorCode != "generation_parameter_unsupported" || !record.ApprovalRequired || record.ApprovalDecision != "" {
		t.Fatalf("rejected tool record = %#v", record)
	}
	checkpoint, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.LastToolResult == nil || checkpoint.LastToolResult.ToolCallID != call.ToolCallID ||
		checkpoint.LastToolResult.ErrorCode != record.ErrorCode || string(checkpoint.LastToolResult.Output) != record.OutputJSON {
		t.Fatalf("rejected tool checkpoint = %#v", checkpoint)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, current, transition, time.Now().UTC()); !errors.Is(err, ErrAgentRuntimeStepConflict) {
		t.Fatalf("replayed rejected transition error = %v", err)
	}
	var count int64
	if err := db.Model(&model.AgentToolCall{}).Where("run_id = ? AND tool_call_id = ?", scope.RunID, call.ToolCallID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("rejected tool call count = %d, want 1", count)
	}
	var timelineItem model.AgentTimelineItem
	if err := db.Where("run_id = ? AND kind = ?", scope.RunID, model.AgentTimelineItemToolResult).Take(&timelineItem).Error; err != nil {
		t.Fatal(err)
	}
	if timelineItem.Status != model.AgentTimelineItemFailed || strings.Contains(timelineItem.ContentJSON, `"succeeded":true`) {
		t.Fatalf("rejected tool timeline = %#v", timelineItem)
	}
}

func TestInitializeAgentRunFreezesModelAndCreatesCheckpointOnce(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLiteFile(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC()
	input := InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-record-1", ModelKey: "gpt-5.5",
		MaxSteps: 4, ToolSchemaVersion: 1, RuntimeVersion: 1, PolicyVersion: 1, UserMessage: "请根据当前画布继续完成任务",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: now,
		Limits: &agentruntime.RuntimeLimits{
			MaxToolCalls: 8, StartedAt: now, DeadlineAt: now.Add(30 * time.Minute),
		},
	}
	initialized, err := repo.InitializeAgentRun(input)
	if err != nil {
		t.Fatal(err)
	}
	if !initialized.Created || initialized.Run.StateVersion != 1 || initialized.Run.LastEventSequence != 2 || initialized.Run.ModelRecordID != input.ModelRecordID || initialized.Run.ModelKey != input.ModelKey || initialized.Run.MaxSteps != input.MaxSteps || initialized.Run.RuntimeVersion != 1 || initialized.Run.PolicyVersion != 1 {
		t.Fatalf("initialized run = %#v", initialized)
	}
	loaded, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.StateVersion != 1 || loaded.StepNumber != 0 || loaded.Status != agentruntime.RunQueued || loaded.UserMessage != input.UserMessage {
		t.Fatalf("initial checkpoint = %#v", loaded)
	}
	if loaded.Limits == nil || loaded.Limits.MaxToolCalls != 8 || loaded.Limits.ToolCallsUsed != 0 ||
		!loaded.Limits.StartedAt.Equal(now) || !loaded.Limits.DeadlineAt.Equal(now.Add(30*time.Minute)) {
		t.Fatalf("initial checkpoint limits = %#v", loaded.Limits)
	}
	if loaded.ClarificationHistory == nil {
		t.Fatal("clarification history must serialize as an explicit empty array")
	}
	history, err := repo.AgentThreadHistory(scope, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || len(history[0].Turns) != 1 || history[0].Turns[0].Run.RuntimeVersion != input.RuntimeVersion || history[0].Turns[0].Run.PolicyVersion != input.PolicyVersion {
		t.Fatalf("thread history did not preserve runtime policy versions: %#v", history)
	}
	replayed, err := repo.InitializeAgentRun(input)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Created {
		t.Fatalf("initialization replay created facts = %#v", replayed)
	}
	var eventCount, checkpointCount, timelineCount int64
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ?", scope.RunID).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentCheckpoint{}).Where("run_id = ?", scope.RunID).Count(&checkpointCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentTimelineItem{}).Where("run_id = ?", scope.RunID).Count(&timelineCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 2 || checkpointCount != 1 || timelineCount != 1 {
		t.Fatalf("initial facts duplicated: events=%d checkpoints=%d timeline=%d", eventCount, checkpointCount, timelineCount)
	}
	var events []model.AgentRunEvent
	if err := db.Where("run_id = ?", scope.RunID).Order("sequence").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Kind != agentruntime.EventRunCreated || events[1].Kind != agentruntime.EventUserMessageAdded {
		t.Fatalf("initial event history = %#v", events)
	}
	var checkpoint model.AgentCheckpoint
	if err := db.Where("run_id = ?", scope.RunID).Take(&checkpoint).Error; err != nil {
		t.Fatal(err)
	}
	if checkpoint.Sequence != 2 || checkpoint.StateVersion != 1 {
		t.Fatalf("initial checkpoint facts = %#v", checkpoint)
	}
	var item model.AgentTimelineItem
	if err := db.Where("run_id = ?", scope.RunID).Take(&item).Error; err != nil {
		t.Fatal(err)
	}
	if item.TenantKind != scope.TenantKind || item.TenantID != scope.TenantID || item.ThreadID != scope.ThreadID ||
		item.Kind != model.AgentTimelineItemUserMessage || item.Status != model.AgentTimelineItemCompleted ||
		item.Ordinal != 1 || item.SourceEventSequence != 2 || !strings.Contains(item.ContentJSON, input.UserMessage) {
		t.Fatalf("initial timeline item = %#v", item)
	}

	conflict := input
	conflict.ModelKey = "deepseek-v4-pro"
	if _, err := repo.InitializeAgentRun(conflict); !errors.Is(err, ErrAgentRuntimeInitializationConflict) {
		t.Fatalf("different model replay error = %v", err)
	}
	conflict = input
	conflict.UserMessage = "另一个用户请求"
	if _, err := repo.InitializeAgentRun(conflict); !errors.Is(err, ErrAgentRuntimeInitializationConflict) {
		t.Fatalf("different user message replay error = %v", err)
	}
}

func TestCreateInitializedExternalAgentRunAllowsEmptyCloudModelIdentity(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLiteFile(t)
	scope := repositoryAgentScope()
	now := time.Now().UTC()
	input := CreateInitializedExternalAgentRunInput{
		Create: CreateLocalAgentRunInput{
			Scope: scope, ExternalThreadID: "codex-thread-1", ClientRequestID: "codex-turn-1", Now: now,
		},
		Initialize: InitializeAgentRunInput{
			Scope: scope, MaxSteps: 8,
			ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
			RuntimeVersion:    agentruntime.CurrentRuntimeVersion,
			PolicyVersion:     agentruntime.CurrentPolicyVersion,
			UserMessage:       "读取当前画布",
			Configuration:     agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided},
			Now:               now,
		},
	}
	initialized, err := repo.CreateInitializedExternalAgentRun(input)
	if err != nil {
		t.Fatal(err)
	}
	if !initialized.Created || initialized.Run.ReasoningHost != agentruntime.ReasoningHostLocalCodex ||
		initialized.Run.ModelRecordID != "" || initialized.Run.ModelKey != "" || initialized.Run.StateVersion != 1 {
		t.Fatalf("initialized external run = %#v", initialized)
	}
	input.Initialize.Scope.ThreadID = initialized.Run.ThreadID
	input.Initialize.Scope.RunID = initialized.Run.ID
	state, err := repo.LoadAgentCheckpoint(input.Initialize.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != agentruntime.RunQueued || state.StateVersion != 1 || state.UserMessage != input.Initialize.UserMessage {
		t.Fatalf("external initial checkpoint = %#v", state)
	}
	var taskCount int64
	if err := db.Model(&model.Task{}).Where("type = ?", "agent_runtime_model").Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 {
		t.Fatalf("external initialization created %d cloud model tasks", taskCount)
	}

	replayed, err := repo.CreateInitializedExternalAgentRun(input)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Created || replayed.Run.ID != initialized.Run.ID || replayed.Run.ThreadID != initialized.Run.ThreadID {
		t.Fatalf("external initialization replay = %#v", replayed)
	}
}

func TestCommitExternalAgentDecisionReplaysIdenticalRequest(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLiteFile(t)
	seedScope := repositoryAgentScope()
	now := time.Now().UTC()
	initialized, err := repo.CreateInitializedExternalAgentRun(CreateInitializedExternalAgentRunInput{
		Create: CreateLocalAgentRunInput{
			Scope: seedScope, ExternalThreadID: "codex-thread-concurrent", ClientRequestID: "codex-run-concurrent", Now: now,
		},
		Initialize: InitializeAgentRunInput{
			Scope: seedScope, MaxSteps: 8,
			ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
			RuntimeVersion:    agentruntime.CurrentRuntimeVersion,
			PolicyVersion:     agentruntime.CurrentPolicyVersion,
			UserMessage:       "需要补充创作风格",
			Configuration:     agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided},
			Now:               now,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	scope := seedScope
	scope.ThreadID = initialized.Run.ThreadID
	scope.RunID = initialized.Run.ID
	queued, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	started, err := agentruntime.BeginModelRequest(queued)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, queued, started, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	decision := agentruntime.ModelDecision{
		Kind: agentruntime.DecisionClarificationRequest,
		Clarification: &agentruntime.ClarificationDecision{
			RequestID: "clarify-concurrent",
			Questions: []agentruntime.ClarificationQuestion{{
				ID: "style", Prompt: "请选择创作风格", Type: agentruntime.ClarificationFreeText,
			}},
			ExpectedDelivery: repositoryTestAnswerDelivery(),
		},
	}
	transition, err := agentruntime.AdvanceExternalDecision(started.State, agentruntime.ExternalDecisionInput{
		ExpectedStateVersion: started.State.StateVersion,
		Decision:             decision,
	})
	if err != nil {
		t.Fatal(err)
	}
	input := CommitExternalAgentDecisionInput{
		Scope: scope, ClientRequestID: "external-decision-concurrent", RequestHash: strings.Repeat("a", 64),
		ExpectedStateVersion: started.State.StateVersion, Previous: started.State, Transition: transition, Now: now.Add(2 * time.Second),
	}
	firstReplayed, err := repo.CommitExternalAgentDecision(input)
	if err != nil {
		t.Fatal(err)
	}
	if firstReplayed {
		t.Fatal("first external decision unexpectedly reported a replay")
	}
	secondReplayed, err := repo.CommitExternalAgentDecision(input)
	if err != nil {
		t.Fatal(err)
	}
	if !secondReplayed {
		t.Fatal("identical external decision did not replay its committed receipt")
	}
	var receiptCount int64
	if err := db.Model(&model.AgentExternalDecision{}).Where("run_id = ?", scope.RunID).Count(&receiptCount).Error; err != nil {
		t.Fatal(err)
	}
	if receiptCount != 1 {
		t.Fatalf("external decision receipt count = %d, want 1", receiptCount)
	}
}

func TestCreateInitializedAgentRunRollsBackIdentityWhenInitializationFails(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLiteFile(t)
	scope := repositoryAgentScope()
	now := time.Now().UTC()
	_, err := repo.CreateInitializedAgentRun(CreateInitializedAgentRunInput{
		Create: CreateAgentRunInput{Scope: scope, ClientRequestID: "atomic-initialization", Now: now},
		Initialize: InitializeAgentRunInput{
			Scope: scope, ModelRecordID: "", ModelKey: "gpt-5.5", MaxSteps: 4,
			ToolSchemaVersion: 1, RuntimeVersion: 1, PolicyVersion: 1, UserMessage: "读取画布",
			Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: now,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "initialization boundary") {
		t.Fatalf("invalid atomic initialization error = %v", err)
	}
	var runCount int64
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).Count(&runCount).Error; err != nil {
		t.Fatal(err)
	}
	var threadCount int64
	if err := db.Model(&model.AgentThread{}).Where("id = ?", scope.ThreadID).Count(&threadCount).Error; err != nil {
		t.Fatal(err)
	}
	if runCount != 0 || threadCount != 0 {
		t.Fatalf("failed atomic initialization leaked identity facts: runs=%d threads=%d", runCount, threadCount)
	}
}

func TestCommitAgentRuntimeTransitionRegistersAndCompletesToolAtomically(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-1", ModelKey: "gpt-5.5", MaxSteps: 4,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion:    agentruntime.CurrentRuntimeVersion,
		PolicyVersion:     agentruntime.CurrentPolicyVersion,
		UserMessage:       "读取当前画布",
		Configuration:     agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic}, Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	current, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	decision := agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{
		ToolCallID: "call-read-state", ToolName: agentruntime.ToolCanvasRead,
		ActionVersion: 1, Arguments: []byte(`{"canvasId":"agent-canvas-1","selectedNodeIds":[],"includeViewport":true}`), ExpectedDelivery: repositoryTestAnswerDelivery(),
	}}
	requested, err := agentruntime.Advance(current, agentruntime.RuntimeInput{Decision: decision})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, current, requested, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	var call model.AgentToolCall
	if err := db.First(&call, "run_id = ? AND tool_call_id = ? AND action_version = ?", scope.RunID, "call-read-state", 1).Error; err != nil {
		t.Fatal(err)
	}
	if call.Status != agentruntime.ToolCallPending || call.RiskLevel != agentruntime.ToolRiskRead || call.RequiredAccess != agentruntime.AccessViewer || call.ApprovalRequired || call.IdempotencyKey != scope.RunID+":call-read-state:1" || call.InputJSON != `{"canvasId":"agent-canvas-1","selectedNodeIds":[],"includeViewport":true}` {
		t.Fatalf("registered call = %#v", call)
	}
	resolved, err := agentruntime.ResolveTool(requested.State, agentruntime.ToolResolution{
		ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion, Succeeded: true,
		Output: []byte(`{"canvasId":"canvas-1","committedRevision":8}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, requested.State, resolved, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&call, "id = ?", call.ID).Error; err != nil {
		t.Fatal(err)
	}
	if call.Status != agentruntime.ToolCallSucceeded || call.OutputJSON != `{"canvasId":"canvas-1","committedRevision":8}` {
		t.Fatalf("completed call = %#v", call)
	}
	loaded, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.StateVersion != 3 || loaded.StepNumber != 1 || loaded.Status != agentruntime.RunRunning || loaded.LastToolResult == nil {
		t.Fatalf("resolved checkpoint = %#v", loaded)
	}
	var timelineItems []model.AgentTimelineItem
	if err := db.Where("run_id = ? AND kind = ?", scope.RunID, model.AgentTimelineItemToolCall).Find(&timelineItems).Error; err != nil {
		t.Fatal(err)
	}
	if len(timelineItems) != 1 || timelineItems[0].Status != model.AgentTimelineItemCompleted ||
		timelineItems[0].SourceEventSequence != 5 || !strings.Contains(timelineItems[0].ContentJSON, `"succeeded":true`) ||
		!strings.Contains(timelineItems[0].ContentJSON, `"toolName":"canvas.read"`) ||
		strings.Contains(timelineItems[0].ContentJSON, `"output"`) {
		t.Fatalf("completed tool timeline lifecycle = %#v", timelineItems)
	}
	var activeTimelineCount int64
	if err := db.Model(&model.AgentTimelineItem{}).Where("run_id = ? AND status = ?", scope.RunID, model.AgentTimelineItemInProgress).Count(&activeTimelineCount).Error; err != nil {
		t.Fatal(err)
	}
	if activeTimelineCount != 0 {
		t.Fatalf("completed tool left %d active timeline items", activeTimelineCount)
	}
}

func TestCommitAgentRuntimeTransitionPersistsImmutableApprovalProposal(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Date(2026, time.September, 1, 8, 0, 0, 0, time.UTC)
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-1", ModelKey: "gpt-5.5", MaxSteps: 4,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion:    agentruntime.CurrentRuntimeVersion,
		PolicyVersion:     agentruntime.CurrentPolicyVersion,
		UserMessage:       "选择当前画布中的角色节点",
		Configuration:     agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	current, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	decision := agentruntime.ToolCallDecision{
		ToolCallID: "call-apply-v6", ToolName: agentruntime.ToolCanvasApplyOps, ActionVersion: 1,
		Arguments:        json.RawMessage(`{"canvasId":"canvas-1","baseRevision":3,"clientMutationId":"mutation-1","operations":[{"operationId":"op-1","type":"select_nodes","nodeIds":[]}]}`),
		ExpectedDelivery: repositoryTestCanvasDelivery(),
	}
	requested, err := agentruntime.Advance(current, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall, ToolCall: &decision,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if requested.State.Status != agentruntime.RunWaitingApproval {
		t.Fatalf("requested status = %q, want waiting approval", requested.State.Status)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, current, requested, now); err != nil {
		t.Fatal(err)
	}

	record, err := repo.AgentToolCallForScope(scope, decision.ToolCallID, decision.ActionVersion)
	if err != nil {
		t.Fatal(err)
	}
	if record.ApprovalProposalJSON == "" || len(record.ApprovalProposalHash) != 64 || record.ApprovalExpiresAt == nil {
		t.Fatalf("approval proposal facts = %#v", record)
	}
	proposal, err := agentruntime.DecodeApprovalProposal(json.RawMessage(record.ApprovalProposalJSON))
	if err != nil {
		t.Fatal(err)
	}
	if proposal.RunID != scope.RunID || proposal.ToolCallID != decision.ToolCallID || proposal.ToolName != decision.ToolName ||
		proposal.Scope.TenantID != scope.TenantID || proposal.Scope.ActorUserID != scope.ActorUserID ||
		proposal.Scope.DomainProjectID != scope.DomainProjectID || proposal.Scope.CanvasID != scope.CanvasID ||
		proposal.Scope.ThreadID != scope.ThreadID || proposal.Effect.Kind != agentruntime.ApprovalEffectCanvasMutation ||
		proposal.Quote != nil || !proposal.ExpiresAt.Equal(now.Add(agentruntime.DefaultApprovalProposalTTL)) {
		t.Fatalf("persisted proposal = %#v", proposal)
	}
	hash, err := proposal.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if hash != record.ApprovalProposalHash || !record.ApprovalExpiresAt.Equal(proposal.ExpiresAt) {
		t.Fatalf("proposal hash/expiry mismatch: record=%#v proposal=%#v", record, proposal)
	}

	wrongApproval, err := agentruntime.ReviewToolApproval(requested.State, agentruntime.ToolApproval{
		ToolCallID: decision.ToolCallID, ActionVersion: decision.ActionVersion,
		Decision: agentruntime.ToolApprovalApproved, ProposalHash: strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, requested.State, wrongApproval, now.Add(time.Minute)); !errors.Is(err, ErrAgentRuntimeStepConflict) {
		t.Fatalf("mismatched proposal hash error = %v", err)
	}

	approved, err := agentruntime.ReviewToolApproval(requested.State, agentruntime.ToolApproval{
		ToolCallID: decision.ToolCallID, ActionVersion: decision.ActionVersion,
		Decision: agentruntime.ToolApprovalApproved, ProposalHash: record.ApprovalProposalHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, requested.State, approved, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := db.First(record, "id = ?", record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if record.Status != agentruntime.ToolCallPending || record.ApprovalDecision != agentruntime.ToolApprovalApproved || record.ApprovalByUserID != scope.ActorUserID {
		t.Fatalf("approved proposal record = %#v", record)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, requested.State, approved, now.Add(3*time.Minute)); !errors.Is(err, ErrAgentRuntimeStepConflict) {
		t.Fatalf("duplicate approval transition error = %v", err)
	}

	duplicate := *record
	duplicate.ID = "tool-duplicate-proposal"
	duplicate.ToolCallID = "another-call"
	duplicate.ActionVersion = 2
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate approval proposal hash was accepted for the same run")
	}
}

func TestCommitAgentRuntimeTransitionInvalidatesExpiredApprovalWithoutRecordingUserDecision(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Date(2026, time.September, 1, 8, 0, 0, 0, time.UTC)
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-1", ModelKey: "gpt-5.5", MaxSteps: 4,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion:    agentruntime.CurrentRuntimeVersion,
		PolicyVersion:     agentruntime.CurrentPolicyVersion,
		UserMessage:       "发布已确认的角色图",
		Configuration:     agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	current, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	decision := agentruntime.ToolCallDecision{
		ToolCallID: "call-publish-v6", ToolName: agentruntime.ToolAssetsPublish, ActionVersion: 1,
		Arguments:        json.RawMessage(`{"resourceId":"resource-1","domainProjectId":"agent-project-1","displayName":"角色图","clientMutationId":"publish-1"}`),
		ExpectedDelivery: repositoryTestImageDelivery(),
	}
	requested, err := agentruntime.Advance(current, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall, ToolCall: &decision,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, current, requested, now); err != nil {
		t.Fatal(err)
	}
	record, err := repo.AgentToolCallForScope(scope, decision.ToolCallID, decision.ActionVersion)
	if err != nil {
		t.Fatal(err)
	}
	invalidated, err := agentruntime.InvalidateToolApproval(requested.State, agentruntime.ToolApprovalInvalidation{
		ToolCallID: decision.ToolCallID, ActionVersion: decision.ActionVersion,
		ProposalHash: record.ApprovalProposalHash, ErrorCode: agentruntime.ApprovalProposalExpired,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, requested.State, invalidated, record.ApprovalExpiresAt.Add(time.Second)); err != nil {
		t.Fatalf("expired approval invalidation failed: %v", err)
	}
	if err := db.First(record, "id = ?", record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if record.Status != agentruntime.ToolCallFailed || record.ErrorCode != agentruntime.ApprovalProposalExpired ||
		record.ApprovalDecision != "" || record.ApprovalByUserID != "" || record.ApprovalDecidedAt != nil {
		t.Fatalf("invalidated approval record = %#v", record)
	}
}

func TestCommitAgentRuntimeTransitionRequiresFrozenMediaQuote(t *testing.T) {
	repo, _ := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Date(2026, time.September, 1, 9, 0, 0, 0, time.UTC)
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-1", ModelKey: "gpt-5.5", MaxSteps: 4,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion:    agentruntime.CurrentRuntimeVersion,
		PolicyVersion:     agentruntime.CurrentPolicyVersion,
		UserMessage:       "生成视频",
		Configuration:     agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	current, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	decision := agentruntime.ToolCallDecision{
		ToolCallID: "call-media-v6", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
		Arguments:        json.RawMessage(`{"mediaKind":"image","modelRecordId":"model-record-1","modelKey":"gpt-image-2","parameters":{"prompt":"雨夜城市"},"sourceResourceIds":[],"targetCanvasNodeId":"node-1","clientRequestId":"request-1"}`),
		ExpectedDelivery: repositoryTestImageDelivery(),
	}
	requested, err := agentruntime.Advance(current, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall, ToolCall: &decision,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, current, requested, now); err == nil {
		t.Fatal("media approval proposal without a frozen quote was persisted")
	}

	requested.ApprovalCostQuote = &agentruntime.ApprovalCostQuote{
		ModelRecordID: "model-record-1", ModelKey: "gpt-image-2", PriceVersion: 7, AmountMicrocredits: 1_250_000,
	}
	if err := repo.CommitAgentRuntimeTransition(scope, current, requested, now); err != nil {
		t.Fatal(err)
	}
	record, err := repo.AgentToolCallForScope(scope, decision.ToolCallID, decision.ActionVersion)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := agentruntime.DecodeApprovalProposal(json.RawMessage(record.ApprovalProposalJSON))
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Quote == nil || proposal.Quote.ModelRecordID != "model-record-1" || proposal.Quote.ModelKey != "gpt-image-2" ||
		proposal.Quote.PriceVersion != 7 || proposal.Quote.AmountMicrocredits != 1_250_000 {
		t.Fatalf("persisted media quote = %#v", proposal.Quote)
	}
}

func TestCommitAgentRuntimeTransitionPersistsToolExecutionStartAtomically(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-1", ModelKey: "gpt-5.5", MaxSteps: 4,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion:    agentruntime.CurrentRuntimeVersion,
		PolicyVersion:     agentruntime.CurrentPolicyVersion,
		UserMessage:       "修改当前画布",
		Configuration:     agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic}, Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	current, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	requested, err := agentruntime.Advance(current, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall,
		ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "call-apply", ToolName: agentruntime.ToolCanvasApplyOps, ActionVersion: 1,
			Arguments: []byte(`{"canvasId":"agent-canvas-1","baseRevision":7,"clientMutationId":"call-apply","operations":[{"operationId":"delete-obsolete","type":"delete_node","nodeId":"obsolete"}]}`), ExpectedDelivery: repositoryTestCanvasDelivery(),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, current, requested, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	record, err := repo.AgentToolCallForScope(scope, "call-apply", 1)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := agentruntime.ReviewToolApproval(requested.State, agentruntime.ToolApproval{
		ToolCallID: "call-apply", ActionVersion: 1, Decision: agentruntime.ToolApprovalApproved,
		ProposalHash: record.ApprovalProposalHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, requested.State, approved, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	started, err := agentruntime.BeginToolExecution(approved.State, agentruntime.ToolExecution{
		ToolCallID: "call-apply", ActionVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, approved.State, started, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	record, err = repo.AgentToolCallForScope(scope, "call-apply", 1)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != agentruntime.ToolCallRunning || record.StartedAt == nil {
		t.Fatalf("started tool record = %#v", record)
	}
	loaded, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.PendingToolStarted || loaded.StateVersion != started.State.StateVersion || loaded.StepNumber != requested.State.StepNumber {
		t.Fatalf("started checkpoint = %#v", loaded)
	}

	// canvas.apply_ops persists its successful receipt atomically with the
	// canvas revision. A worker crash before checkpoint advancement must resume
	// from that exact receipt instead of treating the terminal tool row as a
	// transition conflict.
	receipt := []byte(`{"canvasId":"agent-canvas-1","baseRevision":7,"committedRevision":8,"clientMutationId":"call-apply","proposalHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","appliedOperationIds":["delete-obsolete"],"evidence":{"addedNodeIds":[],"updatedNodeIds":[],"deletedNodeIds":["obsolete"],"upsertedConnectionIds":[],"deletedConnectionIds":[],"selectedNodeIds":[],"viewportApplied":false}}`)
	if err := db.Model(&model.AgentToolCall{}).Where("id = ?", record.ID).
		Updates(agentToolCompletionUpdates{Status: agentruntime.ToolCallSucceeded, OutputJSON: string(receipt), UpdatedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	resolved, err := agentruntime.ResolveTool(started.State, agentruntime.ToolResolution{
		ToolCallID: "call-apply", ActionVersion: 1, Succeeded: true, Output: receipt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, started.State, resolved, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	loaded, err = repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LastToolResult == nil || !loaded.LastToolResult.Succeeded || string(loaded.LastToolResult.Output) != string(receipt) {
		t.Fatalf("resumed completed tool checkpoint = %#v", loaded)
	}
}

func TestLegacyV3CommitAgentRuntimeTransitionPersistsApprovalDecisionAtomically(t *testing.T) {
	t.Skip("tool schema v3 is terminal-history-only; v4 approval persistence is covered by media.generate service and repository tests")
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-1", ModelKey: "gpt-5.5", MaxSteps: 4,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion:    agentruntime.CurrentRuntimeVersion,
		PolicyVersion:     agentruntime.CurrentPolicyVersion,
		UserMessage:       "生成一张图片",
		Configuration:     agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	current, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "approval-plan", BaseVersion: 0,
		Draft: agentruntime.ProductionPlanDraft{
			Title: "审批计划", TargetDurationMS: 1_000, Script: "镜头",
			Shots: []agentruntime.ShotPlanDraft{{ShotKey: "shot-1", Order: 1, DurationMS: 1_000, ScriptText: "镜头", Deliverables: dualProductionShotDeliverables(), ImagePrompt: "画面", VideoPrompt: "动作", Dependencies: []string{}}},
		},
		Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact := firstProductionArtifact(t, plan.Artifacts, "shot-1", model.AgentProductionArtifactStoryboardImage)
	rawArguments, err := json.Marshal(agentruntime.ProductionRenderArguments{
		PlanKey: plan.Plan.PlanKey, PlanVersion: plan.Plan.Version, ArtifactID: artifact.ID, Attempt: artifact.Attempt,
		GenerationModel: agentruntime.GenerationModelSelection{ChannelID: "image-channel", Model: "image-model"},
		ImageConfig:     &agentruntime.ImageRenderConfig{Size: "1:1", Resolution: "1K", Quality: "medium", Count: 1},
		FrozenRenderQuote: agentruntime.FrozenRenderQuote{
			AmountMicrocredits: 100, PerTaskAmountMicrocredits: 100, PriceVersion: 1,
			BillingMode: "fixed_request", Quantity: 1, QuoteFingerprint: "approval-quote",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	requested, err := agentruntime.Advance(current, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "call-generate", ToolName: agentruntime.ToolProductionRender,
			ActionVersion: 1, Arguments: rawArguments, ExpectedDelivery: repositoryTestImageDelivery(),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, current, requested, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	approved, err := agentruntime.ReviewToolApproval(requested.State, agentruntime.ToolApproval{
		ToolCallID: "call-generate", ActionVersion: 1, Decision: agentruntime.ToolApprovalApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, requested.State, approved, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	var call model.AgentToolCall
	if err := db.First(&call, "run_id = ? AND tool_call_id = ?", scope.RunID, "call-generate").Error; err != nil {
		t.Fatal(err)
	}
	if call.Status != agentruntime.ToolCallPending || call.ApprovalDecision != agentruntime.ToolApprovalApproved || call.ApprovalByUserID != scope.ActorUserID || call.ApprovalDecidedAt == nil {
		t.Fatalf("approved call = %#v", call)
	}
	var toolItems []model.AgentTimelineItem
	if err := db.Where("run_id = ? AND kind = ?", scope.RunID, model.AgentTimelineItemToolCall).Find(&toolItems).Error; err != nil {
		t.Fatal(err)
	}
	if len(toolItems) != 1 || toolItems[0].Status != model.AgentTimelineItemInProgress ||
		!strings.Contains(toolItems[0].ContentJSON, `"decision":"approved"`) {
		t.Fatalf("approved tool timeline lifecycle = %#v", toolItems)
	}
	var approvalItemCount int64
	if err := db.Model(&model.AgentTimelineItem{}).Where("run_id = ? AND kind = ?", scope.RunID, model.AgentTimelineItemApproval).Count(&approvalItemCount).Error; err != nil {
		t.Fatal(err)
	}
	if approvalItemCount != 0 {
		t.Fatalf("approval created %d parallel timeline items", approvalItemCount)
	}
}

func TestLegacyV3CommitProductionRetryApprovalClearsRefundedAttemptBindingsAtomically(t *testing.T) {
	t.Skip("tool schema v3 is terminal-history-only; v4 media recovery preserves commercial facts in agent production recovery tests")
	repo, db := openAgentRuntimeRepositorySQLite(t)
	if err := db.AutoMigrate(&model.Task{}, &model.BillingOrder{}); err != nil {
		t.Fatal(err)
	}
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC()
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-1", ModelKey: "gpt-5.5", MaxSteps: 4,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion:    agentruntime.CurrentRuntimeVersion,
		PolicyVersion:     agentruntime.CurrentPolicyVersion,
		UserMessage:       "重试失败镜头",
		Configuration:     agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	current, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "refunded-retry-plan", BaseVersion: 0,
		Draft: agentruntime.ProductionPlanDraft{
			Title: "退款后重试", TargetDurationMS: 1_000, Script: "镜头",
			Shots: []agentruntime.ShotPlanDraft{{ShotKey: "shot-1", Order: 1, DurationMS: 1_000, ScriptText: "镜头", Deliverables: dualProductionShotDeliverables(), ImagePrompt: "画面", VideoPrompt: "动作", Dependencies: []string{}}},
		},
		Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact := firstProductionArtifact(t, plan.Artifacts, "shot-1", model.AgentProductionArtifactStoryboardImage)
	task := model.Task{
		ID: "refunded-retry-task", UserID: scope.ActorUserID, ProjectID: scope.CanvasID,
		Type: "canvas_image", Capability: "image", Status: model.TaskStatusFailed,
		BillingOrderID: "refunded-retry-order", CreatedAt: now, UpdatedAt: now,
	}
	order := model.BillingOrder{
		ID: task.BillingOrderID, UserID: scope.ActorUserID, TaskID: task.ID,
		IdempotencyKey: "refunded-retry-order-key", Status: model.BillingStatusRefunded,
		AmountMicrocredits: 100, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentProductionArtifact{}).Where("id = ?", artifact.ID).
		Updates(model.AgentProductionArtifact{
			Status: model.AgentProductionArtifactFailed, Attempt: 1,
			TaskID: task.ID, BillingOrderID: order.ID, LastErrorCode: "production_generation_failed",
		}).Error; err != nil {
		t.Fatal(err)
	}
	rawArguments, err := json.Marshal(agentruntime.ProductionRenderArguments{
		PlanKey: plan.Plan.PlanKey, PlanVersion: plan.Plan.Version, ArtifactID: artifact.ID, Attempt: 1,
		GenerationModel: agentruntime.GenerationModelSelection{ChannelID: "image-channel", Model: "image-model"},
		ImageConfig:     &agentruntime.ImageRenderConfig{Size: "1:1", Resolution: "1K", Quality: "medium", Count: 1},
		FrozenRenderQuote: agentruntime.FrozenRenderQuote{
			AmountMicrocredits: 100, PerTaskAmountMicrocredits: 100, PriceVersion: 1,
			BillingMode: "fixed_request", Quantity: 1, QuoteFingerprint: "refunded-retry-quote",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	requested, err := agentruntime.Advance(current, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "call-refunded-retry", ToolName: agentruntime.ToolProductionRender,
			ActionVersion: 1, Arguments: rawArguments, ExpectedDelivery: repositoryTestImageDelivery(),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, current, requested, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var stored model.AgentProductionArtifact
	if err := db.First(&stored, "id = ?", artifact.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.AgentProductionArtifactAwaitingApproval || stored.Attempt != 1 || stored.TaskID != "" || stored.BillingOrderID != "" || stored.ResourceID != "" {
		t.Fatalf("prepared retry artifact = %#v", stored)
	}
	var oldTask model.Task
	var oldOrder model.BillingOrder
	if err := db.First(&oldTask, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&oldOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if oldTask.Status != model.TaskStatusFailed || oldOrder.Status != model.BillingStatusRefunded {
		t.Fatalf("historical commercial facts changed: task=%s billing=%s", oldTask.Status, oldOrder.Status)
	}
}

func repositoryTestAnswerDelivery() agentruntime.ExpectedDelivery {
	return agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryAnswer,
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}},
	}
}

func repositoryTestCanvasDelivery() agentruntime.ExpectedDelivery {
	return agentruntime.ExpectedDelivery{
		Kind: agentruntime.DeliveryCanvasChange, TargetCanvasID: "agent-canvas-1",
		RequiredArtifacts:  []agentruntime.ArtifactKind{agentruntime.ArtifactCanvasRevision},
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactCanvasRevision}},
	}
}

func repositoryTestImageDelivery() agentruntime.ExpectedDelivery {
	return agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryGeneratedAsset,
		RequiredArtifacts:  []agentruntime.ArtifactKind{agentruntime.ArtifactImage},
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactArtifact, Artifact: agentruntime.ArtifactImage}},
	}
}

func TestCommitAgentRuntimeTransitionPersistsRunEventsAndCheckpointAtomically(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).Updates(model.AgentRun{MaxSteps: 3, ModelRecordID: "model-1", ModelKey: "gpt", ToolSchemaVersion: 1}).Error; err != nil {
		t.Fatal(err)
	}
	previous := agentruntime.RuntimeState{StateVersion: 1, StepNumber: 0, MaxSteps: 3, Status: agentruntime.RunQueued, UserMessage: "test"}
	state := agentruntime.RuntimeState{StateVersion: 2, StepNumber: 1, MaxSteps: 3, Status: agentruntime.RunRunning, UserMessage: "test"}
	transition := agentruntime.RuntimeTransition{State: state, EventKinds: []agentruntime.EventKind{agentruntime.EventRunStatusChanged, agentruntime.EventCheckpointSaved}}
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).Update("state_version", 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, previous, transition, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.StepNumber != 1 || loaded.StateVersion != 2 || loaded.Status != agentruntime.RunRunning {
		t.Fatalf("loaded state = %#v", loaded)
	}
	var run model.AgentRun
	if err := db.First(&run, "id = ?", scope.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if run.StepNumber != 1 || run.LastEventSequence != 2 || run.Status != agentruntime.RunRunning {
		t.Fatalf("run = %#v", run)
	}
	var eventCount, checkpointCount int64
	db.Model(&model.AgentRunEvent{}).Where("run_id = ?", scope.RunID).Count(&eventCount)
	db.Model(&model.AgentCheckpoint{}).Where("run_id = ?", scope.RunID).Count(&checkpointCount)
	if eventCount != 2 || checkpointCount != 1 {
		t.Fatalf("facts: events=%d checkpoints=%d", eventCount, checkpointCount)
	}
}

func TestCommitAgentRuntimeTransitionPersistsAgentMessageBeforeTerminalStatus(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC()
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-1", ModelKey: "gpt-5.5", MaxSteps: 4,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion:    agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion,
		UserMessage:   "完成当前画布",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	previous, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	state := previous
	state.StateVersion++
	state.StepNumber++
	state.Status = agentruntime.RunSucceeded
	state.FinalMessage = "画布已完成"
	transition := agentruntime.RuntimeTransition{
		State: state,
		EventKinds: []agentruntime.EventKind{
			agentruntime.EventAgentMessageCompleted,
			agentruntime.EventRunCompleted,
		},
	}
	if err := repo.CommitAgentRuntimeTransition(scope, previous, transition, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var events []model.AgentRunEvent
	if err := db.Where("run_id = ?", scope.RunID).Order("sequence").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 || events[2].Kind != agentruntime.EventAgentMessageCompleted || events[3].Kind != agentruntime.EventRunCompleted {
		t.Fatalf("terminal events = %#v", events)
	}
	var items []model.AgentTimelineItem
	if err := db.Where("run_id = ?", scope.RunID).Order("ordinal").Find(&items).Error; err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[0].Kind != model.AgentTimelineItemUserMessage ||
		items[1].Kind != model.AgentTimelineItemAgentMessage || items[1].SourceEventSequence != 3 ||
		items[2].Kind != model.AgentTimelineItemStatusKind || items[2].Status != model.AgentTimelineItemCompleted || items[2].SourceEventSequence != 4 {
		t.Fatalf("terminal timeline = %#v", items)
	}
	if !strings.Contains(items[1].ContentJSON, state.FinalMessage) {
		t.Fatalf("agent message content = %s", items[1].ContentJSON)
	}
}

func TestAppendAgentSteerPersistsDurableIdempotencyAndKeepsOriginalMessage(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC()
	initialMessage := "生成一个三十秒广告"
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-1", ModelKey: "gpt-5.5", MaxSteps: 8,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion:    agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion,
		UserMessage:   initialMessage,
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	request := agentruntime.SteerRequest{
		ClientRequestID: "steer-keep-costume", Message: "角色服装和道具保持一致", ExpectedStateVersion: 1,
	}
	state, replayed, err := repo.AppendAgentSteer(scope, request, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if replayed || state.StateVersion != 2 || state.UserMessage != initialMessage || len(state.PendingSteers) != 1 {
		t.Fatalf("steered state = %#v replayed=%v", state, replayed)
	}
	replayedState, replayed, err := repo.AppendAgentSteer(scope, request, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !replayed || replayedState.StateVersion != state.StateVersion || len(replayedState.PendingSteers) != 1 {
		t.Fatalf("steer replay state = %#v replayed=%v", replayedState, replayed)
	}
	conflict := request
	conflict.Message = "改成另外一套服装"
	if _, _, err := repo.AppendAgentSteer(scope, conflict, now.Add(3*time.Second)); !errors.Is(err, agentruntime.ErrSteerConflict) {
		t.Fatalf("steer identity conflict = %v", err)
	}
	var eventCount, checkpointCount, timelineCount int64
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ?", scope.RunID).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentCheckpoint{}).Where("run_id = ?", scope.RunID).Count(&checkpointCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentTimelineItem{}).Where("run_id = ?", scope.RunID).Count(&timelineCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 3 || checkpointCount != 2 || timelineCount != 2 {
		t.Fatalf("steer replay duplicated facts: events=%d checkpoints=%d timeline=%d", eventCount, checkpointCount, timelineCount)
	}
}

func TestInterruptAgentRunUsesCASAndPersistsInterruptedTimeline(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLiteFile(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC()
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-1", ModelKey: "gpt-5.5", MaxSteps: 8,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion:    agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion,
		UserMessage:   "生成短片",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	state, err := repo.CancelAgentRunTree(scope, 1, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != agentruntime.RunCancelled || state.StateVersion != 2 {
		t.Fatalf("interrupted state = %#v", state)
	}
	if _, err := repo.CancelAgentRunTree(scope, 1, now.Add(2*time.Second)); !errors.Is(err, agentruntime.ErrInterruptConflict) {
		t.Fatalf("replayed interrupt error = %v", err)
	}
	var run model.AgentRun
	if err := db.First(&run, "id = ?", scope.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != agentruntime.RunCancelled || run.LastEventSequence != 3 || run.CompletedAt == nil {
		t.Fatalf("interrupted run facts = %#v", run)
	}
	var item model.AgentTimelineItem
	if err := db.Where("run_id = ? AND kind = ?", scope.RunID, model.AgentTimelineItemStatusKind).Take(&item).Error; err != nil {
		t.Fatal(err)
	}
	if item.Status != model.AgentTimelineItemInterrupted || item.SourceEventSequence != 3 {
		t.Fatalf("interrupted timeline item = %#v", item)
	}
}

func TestInterruptAgentRunClosesPendingTimelineLifecycle(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		decision agentruntime.ModelDecision
		kind     model.AgentTimelineItemKind
	}{
		{
			name: "clarification",
			decision: agentruntime.ModelDecision{Kind: agentruntime.DecisionClarificationRequest, Clarification: &agentruntime.ClarificationDecision{
				RequestID: "clarify-interrupt",
				Questions: []agentruntime.ClarificationQuestion{{
					ID: "style", Prompt: "需要什么风格？", Type: agentruntime.ClarificationFreeText,
				}},
				ExpectedDelivery: repositoryTestAnswerDelivery(),
			}},
			kind: model.AgentTimelineItemClarification,
		},
		{
			name: "tool awaiting approval",
			decision: agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{
				ToolCallID: "call-interrupt", ToolName: agentruntime.ToolCanvasApplyOps, ActionVersion: 1,
				Arguments: []byte(`{"canvasId":"agent-canvas-1","baseRevision":7,"clientMutationId":"call-interrupt","operations":[{"operationId":"select-artifact","type":"select_nodes","nodeIds":["artifact-1"]}]}`), ExpectedDelivery: repositoryTestCanvasDelivery(),
			}},
			kind: model.AgentTimelineItemToolCall,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repo, db := openAgentRuntimeRepositorySQLite(t)
			scope := repositoryAgentScope()
			scope.RunID += "-" + testCase.name
			scope.ThreadID += "-" + testCase.name
			createAgentRunForTest(t, repo, scope)
			now := time.Now().UTC()
			if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
				Scope: scope, ModelRecordID: "model-1", ModelKey: "gpt-5.5", MaxSteps: 8,
				ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
				RuntimeVersion:    agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion,
				UserMessage: "执行后中断", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: now,
			}); err != nil {
				t.Fatal(err)
			}
			current, err := repo.LoadAgentCheckpoint(scope)
			if err != nil {
				t.Fatal(err)
			}
			requested, err := agentruntime.Advance(current, agentruntime.RuntimeInput{Decision: testCase.decision})
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.CommitAgentRuntimeTransition(scope, current, requested, now.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.CancelAgentRunTree(scope, requested.State.StateVersion, now.Add(2*time.Second)); err != nil {
				t.Fatal(err)
			}
			var item model.AgentTimelineItem
			if err := db.Where("run_id = ? AND kind = ?", scope.RunID, testCase.kind).Take(&item).Error; err != nil {
				t.Fatal(err)
			}
			if item.Status != model.AgentTimelineItemInterrupted {
				t.Fatalf("interrupted lifecycle item = %#v", item)
			}
			var activeCount int64
			if err := db.Model(&model.AgentTimelineItem{}).Where("run_id = ? AND status = ?", scope.RunID, model.AgentTimelineItemInProgress).Count(&activeCount).Error; err != nil {
				t.Fatal(err)
			}
			if activeCount != 0 {
				t.Fatalf("interrupt left %d active timeline items", activeCount)
			}
		})
	}
}

func TestTerminateAgentRunClosesPendingTimelineLifecycle(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		decision agentruntime.ModelDecision
		kind     model.AgentTimelineItemKind
	}{
		{
			name: "clarification",
			decision: agentruntime.ModelDecision{Kind: agentruntime.DecisionClarificationRequest, Clarification: &agentruntime.ClarificationDecision{
				RequestID:        "clarify-terminate",
				Questions:        []agentruntime.ClarificationQuestion{{ID: "style", Prompt: "需要什么风格？", Type: agentruntime.ClarificationFreeText}},
				ExpectedDelivery: repositoryTestAnswerDelivery(),
			}},
			kind: model.AgentTimelineItemClarification,
		},
		{
			name: "tool",
			decision: agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{
				ToolCallID: "call-terminate", ToolName: agentruntime.ToolCanvasRead, ActionVersion: 1,
				Arguments: []byte(`{"canvasId":"agent-canvas-1","selectedNodeIds":["artifact-1"],"includeViewport":true}`), ExpectedDelivery: repositoryTestCanvasDelivery(),
			}},
			kind: model.AgentTimelineItemToolCall,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repo, db := openAgentRuntimeRepositorySQLite(t)
			scope := repositoryAgentScope()
			scope.RunID += "-terminate-" + testCase.name
			scope.ThreadID += "-terminate-" + testCase.name
			createAgentRunForTest(t, repo, scope)
			now := time.Now().UTC()
			if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
				Scope: scope, ModelRecordID: "model-1", ModelKey: "gpt-5.5", MaxSteps: 8,
				ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
				RuntimeVersion:    agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion,
				UserMessage: "执行后失败", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic}, Now: now,
			}); err != nil {
				t.Fatal(err)
			}
			current, err := repo.LoadAgentCheckpoint(scope)
			if err != nil {
				t.Fatal(err)
			}
			requested, err := agentruntime.Advance(current, agentruntime.RuntimeInput{Decision: testCase.decision})
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.CommitAgentRuntimeTransition(scope, current, requested, now.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			terminated, err := agentruntime.Terminate(requested.State, "scope_access_revoked")
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.CommitAgentRuntimeTransition(scope, requested.State, terminated, now.Add(2*time.Second)); err != nil {
				t.Fatal(err)
			}
			var item model.AgentTimelineItem
			if err := db.Where("run_id = ? AND kind = ?", scope.RunID, testCase.kind).Take(&item).Error; err != nil {
				t.Fatal(err)
			}
			if item.Status != model.AgentTimelineItemFailed {
				t.Fatalf("terminated lifecycle item = %#v", item)
			}
			var activeCount int64
			if err := db.Model(&model.AgentTimelineItem{}).Where("run_id = ? AND status = ?", scope.RunID, model.AgentTimelineItemInProgress).Count(&activeCount).Error; err != nil {
				t.Fatal(err)
			}
			if activeCount != 0 {
				t.Fatalf("termination left %d active timeline items", activeCount)
			}
		})
	}
}

func TestAgentTimelineMutationRejectsInvalidFactsOrdinalGapAndTerminalReopen(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC()
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-1", ModelKey: "gpt-5.5", MaxSteps: 4,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion:    agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion,
		UserMessage:   "生成短片",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []TimelineMutation{
		{ItemID: "invalid-json", Kind: model.AgentTimelineItemStatusKind, ToStatus: model.AgentTimelineItemCompleted, SourceEventSequence: 3, ContentJSON: json.RawMessage(`{broken`)},
		{ItemID: "invalid-kind", Kind: model.AgentTimelineItemKind("unknown"), ToStatus: model.AgentTimelineItemCompleted, SourceEventSequence: 3, ContentJSON: json.RawMessage(`{}`)},
		{ItemID: "invalid-status", Kind: model.AgentTimelineItemStatusKind, ToStatus: model.AgentTimelineItemStatus("unknown"), SourceEventSequence: 3, ContentJSON: json.RawMessage(`{}`)},
	} {
		nextOrdinal := int64(2)
		if err := persistAgentTimelineMutation(db, scope, mutation, &nextOrdinal, now.Add(time.Second)); !errors.Is(err, ErrAgentTimelineConflict) {
			t.Fatalf("invalid timeline mutation %#v error = %v", mutation, err)
		}
	}
	gapOrdinal := int64(3)
	if err := persistAgentTimelineMutation(db, scope, TimelineMutation{
		ItemID: "ordinal-gap", Kind: model.AgentTimelineItemStatusKind, ToStatus: model.AgentTimelineItemCompleted,
		SourceEventSequence: 3, ContentJSON: json.RawMessage(`{}`),
	}, &gapOrdinal, now.Add(time.Second)); !errors.Is(err, ErrAgentTimelineConflict) {
		t.Fatalf("ordinal gap error = %v", err)
	}
	var initial model.AgentTimelineItem
	if err := db.Where("run_id = ? AND ordinal = 1", scope.RunID).Take(&initial).Error; err != nil {
		t.Fatal(err)
	}
	fromCompleted := model.AgentTimelineItemCompleted
	nextOrdinal := int64(2)
	if err := persistAgentTimelineMutation(db, scope, TimelineMutation{
		ItemID: initial.ID, Kind: initial.Kind, FromStatus: &fromCompleted, ToStatus: model.AgentTimelineItemInProgress,
		SourceEventSequence: 3, ContentJSON: json.RawMessage(`{"message":"reopen"}`),
	}, &nextOrdinal, now.Add(time.Second)); !errors.Is(err, ErrAgentTimelineConflict) {
		t.Fatalf("terminal reopen error = %v", err)
	}
	if err := persistAgentTimelineEvent(db, scope, agentruntime.RuntimeState{}, agentruntime.RuntimeState{}, agentruntime.EventRunCompleted, 3, &nextOrdinal, time.Time{}); !errors.Is(err, ErrAgentTimelineConflict) {
		t.Fatalf("invalid timeline event boundary error = %v", err)
	}
}

func TestCommitAgentRuntimeTransitionRollsBackAllFactsWhenTimelineSequenceConflicts(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC()
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-1", ModelKey: "gpt-5.5", MaxSteps: 4,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion:    agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion,
		UserMessage:   "生成短片",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	completedAt := now
	poison := model.AgentTimelineItem{
		ID: "timeline-sequence-poison", TenantKind: scope.TenantKind, TenantID: scope.TenantID,
		ThreadID: scope.ThreadID, RunID: scope.RunID,
		Kind: model.AgentTimelineItemStatusKind, Status: model.AgentTimelineItemCompleted,
		Ordinal: 2, SourceEventSequence: 3, ContentJSON: `{"status":"poison"}`,
		StartedAt: now, CompletedAt: &completedAt, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&poison).Error; err != nil {
		t.Fatal(err)
	}
	previous, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	state := previous
	state.StateVersion++
	state.Status = agentruntime.RunRunning
	transition := agentruntime.RuntimeTransition{State: state, EventKinds: []agentruntime.EventKind{agentruntime.EventRunStatusChanged}}
	if err := repo.CommitAgentRuntimeTransition(scope, previous, transition, now.Add(time.Second)); !errors.Is(err, ErrAgentTimelineConflict) {
		t.Fatalf("timeline sequence conflict error = %v", err)
	}
	var run model.AgentRun
	if err := db.First(&run, "id = ?", scope.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if run.StateVersion != 1 || run.LastEventSequence != 2 || run.Status != agentruntime.RunQueued {
		t.Fatalf("run changed despite timeline rollback: %#v", run)
	}
	var eventCount, checkpointCount int64
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ?", scope.RunID).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentCheckpoint{}).Where("run_id = ?", scope.RunID).Count(&checkpointCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 2 || checkpointCount != 1 {
		t.Fatalf("runtime facts changed despite timeline rollback: events=%d checkpoints=%d", eventCount, checkpointCount)
	}
}

func TestCommitAgentRuntimeTransitionFencesConcurrentStepAndRollsBack(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLiteFile(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).Update("max_steps", 3).Error; err != nil {
		t.Fatal(err)
	}
	previous := agentruntime.RuntimeState{StateVersion: 1, StepNumber: 0, MaxSteps: 3, Status: agentruntime.RunQueued, UserMessage: "test"}
	transition := agentruntime.RuntimeTransition{State: agentruntime.RuntimeState{StateVersion: 2, StepNumber: 1, MaxSteps: 3, Status: agentruntime.RunRunning, UserMessage: "test"}, EventKinds: []agentruntime.EventKind{agentruntime.EventRunStatusChanged}}
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).Update("state_version", 1).Error; err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errs <- repo.CommitAgentRuntimeTransition(scope, previous, transition, time.Now().UTC())
		}()
	}
	wait.Wait()
	close(errs)
	success, conflict := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else if errors.Is(err, ErrAgentRuntimeStepConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).Updates(model.AgentRun{Status: agentruntime.RunSucceeded, StepNumber: 2}).Error; err != nil {
		t.Fatal(err)
	}
	terminalPrevious := agentruntime.RuntimeState{StateVersion: 3, StepNumber: 2, MaxSteps: 3, Status: agentruntime.RunRunning, UserMessage: "test"}
	terminalTransition := agentruntime.RuntimeTransition{State: agentruntime.RuntimeState{StateVersion: 4, StepNumber: 3, MaxSteps: 3, Status: agentruntime.RunFailed, UserMessage: "test"}, EventKinds: []agentruntime.EventKind{agentruntime.EventRunFailed}}
	if err := repo.CommitAgentRuntimeTransition(scope, terminalPrevious, terminalTransition, time.Now().UTC()); !errors.Is(err, ErrAgentRuntimeStepConflict) {
		t.Fatalf("terminal overwrite error = %v", err)
	}

	other := scope
	other.TenantID, other.ActorUserID = "other", "other"
	if _, err := repo.LoadAgentCheckpoint(other); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-scope load error = %v", err)
	}
}

func TestLoadAgentCheckpointRejectsSnapshotDrift(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).Update("max_steps", 3).Error; err != nil {
		t.Fatal(err)
	}
	previous := agentruntime.RuntimeState{StateVersion: 1, StepNumber: 0, MaxSteps: 3, Status: agentruntime.RunQueued, UserMessage: "test"}
	transition := agentruntime.RuntimeTransition{State: agentruntime.RuntimeState{StateVersion: 2, StepNumber: 1, MaxSteps: 3, Status: agentruntime.RunRunning, UserMessage: "test"}, EventKinds: []agentruntime.EventKind{agentruntime.EventRunStatusChanged}}
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).Update("state_version", 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, previous, transition, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).Update("status", agentruntime.RunFailed).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.LoadAgentCheckpoint(scope); err == nil {
		t.Fatal("drifted run/checkpoint snapshot was accepted")
	}
}

func TestCommitAgentRuntimeTransitionRollsBackRunAndEventsWhenCheckpointFails(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).Update("max_steps", 3).Error; err != nil {
		t.Fatal(err)
	}
	const callback = "agent_runtime_transition_checkpoint_failure"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*model.AgentCheckpoint); ok {
			tx.AddError(errors.New("transition checkpoint failed"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
	previous := agentruntime.RuntimeState{StateVersion: 1, StepNumber: 0, MaxSteps: 3, Status: agentruntime.RunQueued, UserMessage: "test"}
	transition := agentruntime.RuntimeTransition{State: agentruntime.RuntimeState{StateVersion: 2, StepNumber: 1, MaxSteps: 3, Status: agentruntime.RunRunning, UserMessage: "test"}, EventKinds: []agentruntime.EventKind{agentruntime.EventRunStatusChanged}}
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).Update("state_version", 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, previous, transition, time.Now().UTC()); err == nil || err.Error() != "transition checkpoint failed" {
		t.Fatalf("checkpoint failure = %v", err)
	}
	var run model.AgentRun
	if err := db.First(&run, "id = ?", scope.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if run.StepNumber != 0 || run.LastEventSequence != 0 || run.Status != agentruntime.RunQueued {
		t.Fatalf("rolled back run = %#v", run)
	}
	var eventCount int64
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ?", scope.RunID).Count(&eventCount).Error; err != nil || eventCount != 0 {
		t.Fatalf("rolled back events = %d, %v", eventCount, err)
	}
}
