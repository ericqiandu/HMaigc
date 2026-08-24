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

func TestInterruptAdminAgentRunCommitsAuditedTerminalFacts(t *testing.T) {
	repo, db := openAdminAgentRunRepositorySQLite(t)
	now := time.Date(2026, time.August, 24, 16, 0, 0, 0, time.UTC)
	fixture := createAdminInterruptFixture(t, db, "run-admin-approval", agentruntime.RunWaitingApproval, now)

	result, err := repo.InterruptAdminAgentRun(AdminAgentRunInterruptCommand{
		RunID: fixture.scope.RunID, ExpectedStateVersion: fixture.state.StateVersion,
		ActorUserID: "admin-operator", Reason: "  用户离开审批页面，结束本次运行  ", Now: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.State.Status != agentruntime.RunCancelled || result.State.StateVersion != fixture.state.StateVersion+1 || result.OriginalStatus != agentruntime.RunWaitingApproval {
		t.Fatalf("interrupt result = %#v", result)
	}

	var storedRun model.AgentRun
	if err := db.First(&storedRun, "id = ?", fixture.scope.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if storedRun.Status != agentruntime.RunCancelled || storedRun.CompletedAt == nil || storedRun.LastEventSequence != fixture.sequence+1 {
		t.Fatalf("stored run = %#v", storedRun)
	}
	var events []model.AgentRunEvent
	if err := db.Where("run_id = ? AND kind = ?", fixture.scope.RunID, agentruntime.EventRunInterrupted).Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("interrupt events = %#v", events)
	}
	var payload struct {
		StateVersion   int `json:"stateVersion"`
		InterruptAudit struct {
			Source         string                 `json:"source"`
			ActorUserID    string                 `json:"actorUserId"`
			Reason         string                 `json:"reason"`
			OriginalStatus agentruntime.RunStatus `json:"originalStatus"`
		} `json:"interruptAudit"`
	}
	if err := json.Unmarshal([]byte(events[0].PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.StateVersion != fixture.state.StateVersion+1 || payload.InterruptAudit.Source != "admin" ||
		payload.InterruptAudit.ActorUserID != "admin-operator" || payload.InterruptAudit.Reason != "用户离开审批页面，结束本次运行" ||
		payload.InterruptAudit.OriginalStatus != agentruntime.RunWaitingApproval {
		t.Fatalf("interrupt audit = %#v", payload)
	}

	var checkpoints []model.AgentCheckpoint
	if err := db.Where("run_id = ?", fixture.scope.RunID).Order("sequence ASC").Find(&checkpoints).Error; err != nil {
		t.Fatal(err)
	}
	if len(checkpoints) != 2 || strings.Contains(checkpoints[1].StateJSON, "interruptAudit") {
		t.Fatalf("checkpoints = %#v", checkpoints)
	}
	var call model.AgentToolCall
	if err := db.First(&call, "run_id = ?", fixture.scope.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if call.Status != agentruntime.ToolCallFailed || call.ErrorCode != "admin_agent_run_interrupted" || !strings.Contains(call.OutputJSON, "admin_agent_run_interrupted") {
		t.Fatalf("tool call = %#v", call)
	}
	var artifact model.AgentProductionArtifact
	if err := db.First(&artifact, "id = ?", fixture.artifactID).Error; err != nil {
		t.Fatal(err)
	}
	if artifact.Status != model.AgentProductionArtifactFailed || artifact.LastErrorCode != "admin_agent_run_interrupted" || artifact.TaskID != "" || artifact.BillingOrderID != "" {
		t.Fatalf("artifact = %#v", artifact)
	}
	var taskCount int64
	if err := db.Model(&model.Task{}).Where("user_id = ?", fixture.scope.ActorUserID).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	var billingCount int64
	if err := db.Model(&model.BillingOrder{}).Where("user_id = ?", fixture.scope.ActorUserID).Count(&billingCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 || billingCount != 0 {
		t.Fatalf("unapproved production render created cost facts: tasks=%d billing=%d", taskCount, billingCount)
	}
	var item model.AgentTimelineItem
	if err := db.First(&item, "id = ?", fixture.timelineItemID).Error; err != nil {
		t.Fatal(err)
	}
	if item.Status != model.AgentTimelineItemInterrupted || item.CompletedAt == nil || item.SourceEventSequence != fixture.sequence+1 {
		t.Fatalf("timeline item = %#v", item)
	}
}

func TestInterruptAdminAgentRunRejectsCASAndTerminalReplayWithoutWrites(t *testing.T) {
	repo, db := openAdminAgentRunRepositorySQLite(t)
	now := time.Date(2026, time.August, 24, 16, 30, 0, 0, time.UTC)
	fixture := createAdminInterruptFixture(t, db, "run-admin-cas", agentruntime.RunQueued, now)

	_, err := repo.InterruptAdminAgentRun(AdminAgentRunInterruptCommand{
		RunID: fixture.scope.RunID, ExpectedStateVersion: fixture.state.StateVersion - 1,
		ActorUserID: "admin-operator", Reason: "等待时间超过运营窗口", Now: now.Add(time.Minute),
	})
	if !errors.Is(err, ErrAdminAgentRunStateConflict) {
		t.Fatalf("stale interrupt error = %v", err)
	}
	assertAdminInterruptFactCounts(t, db, fixture.scope.RunID, 0, 1)

	first, err := repo.InterruptAdminAgentRun(AdminAgentRunInterruptCommand{
		RunID: fixture.scope.RunID, ExpectedStateVersion: fixture.state.StateVersion,
		ActorUserID: "admin-operator", Reason: "等待时间超过运营窗口", Now: now.Add(2 * time.Minute),
	})
	if err != nil || first.State.Status != agentruntime.RunCancelled {
		t.Fatalf("first interrupt = %#v, err=%v", first, err)
	}
	_, err = repo.InterruptAdminAgentRun(AdminAgentRunInterruptCommand{
		RunID: fixture.scope.RunID, ExpectedStateVersion: fixture.state.StateVersion,
		ActorUserID: "admin-operator", Reason: "重复提交不应新增事实", Now: now.Add(3 * time.Minute),
	})
	if !errors.Is(err, ErrAdminAgentRunTerminal) {
		t.Fatalf("terminal replay error = %v", err)
	}
	assertAdminInterruptFactCounts(t, db, fixture.scope.RunID, 1, 2)
}

func TestInterruptAdminAgentRunClosesAnActiveModelMessage(t *testing.T) {
	repo, db := openAdminAgentRunRepositorySQLite(t)
	now := time.Date(2026, time.August, 24, 16, 45, 0, 0, time.UTC)
	fixture := createAdminInterruptFixture(t, db, "run-admin-model", agentruntime.RunRunning, now)
	itemID := agentruntime.AgentMessageItemID(fixture.scope.RunID, fixture.state.StepNumber)
	item := model.AgentTimelineItem{
		ID: itemID, TenantKind: fixture.scope.TenantKind, TenantID: fixture.scope.TenantID,
		ThreadID: fixture.scope.ThreadID, RunID: fixture.scope.RunID,
		Kind: model.AgentTimelineItemAgentMessage, Status: model.AgentTimelineItemInProgress,
		Ordinal: 1, SourceEventSequence: fixture.sequence, ContentJSON: `{"message":"已输出的可见片段"}`,
		StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}

	result, err := repo.InterruptAdminAgentRun(AdminAgentRunInterruptCommand{
		RunID: fixture.scope.RunID, ExpectedStateVersion: fixture.state.StateVersion,
		ActorUserID: "admin-operator", Reason: "停止持续输出的模型请求", Now: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.State.Status != agentruntime.RunCancelled {
		t.Fatalf("interrupt result = %#v", result)
	}
	var stored model.AgentTimelineItem
	if err := db.First(&stored, "id = ?", itemID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.AgentTimelineItemInterrupted || stored.CompletedAt == nil || stored.SourceEventSequence != fixture.sequence+1 || !strings.Contains(stored.ContentJSON, "已输出的可见片段") {
		t.Fatalf("active model item = %#v", stored)
	}
}

func TestInterruptAdminAgentRunClosesPendingClarification(t *testing.T) {
	repo, db := openAdminAgentRunRepositorySQLite(t)
	now := time.Date(2026, time.August, 24, 16, 50, 0, 0, time.UTC)
	fixture := createAdminInterruptFixture(t, db, "run-admin-clarification", agentruntime.RunWaitingInput, now)

	result, err := repo.InterruptAdminAgentRun(AdminAgentRunInterruptCommand{
		RunID: fixture.scope.RunID, ExpectedStateVersion: fixture.state.StateVersion,
		ActorUserID: "admin-operator", Reason: "结束长期无人回答的询问", Now: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.State.Status != agentruntime.RunCancelled || result.State.PendingClarification != nil {
		t.Fatalf("interrupt result = %#v", result)
	}
	var item model.AgentTimelineItem
	if err := db.First(&item, "id = ?", fixture.timelineItemID).Error; err != nil {
		t.Fatal(err)
	}
	if item.Status != model.AgentTimelineItemInterrupted || item.CompletedAt == nil || item.SourceEventSequence != fixture.sequence+1 {
		t.Fatalf("clarification item = %#v", item)
	}
}

func TestInterruptAdminAgentRunBlocksUnresolvedBillingWithoutWrites(t *testing.T) {
	repo, db := openAdminAgentRunRepositorySQLite(t)
	now := time.Date(2026, time.August, 24, 17, 0, 0, 0, time.UTC)
	fixture := createAdminInterruptFixture(t, db, "run-admin-billing", agentruntime.RunRunning, now)
	order := model.BillingOrder{
		ID: "order-admin-billing", UserID: fixture.scope.ActorUserID,
		IdempotencyKey: "agent-runtime:" + fixture.scope.RunID + ":2",
		Status:         model.BillingStatusUncertain, Error: "供应商账务正文不得进入管理响应", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}

	_, err := repo.InterruptAdminAgentRun(AdminAgentRunInterruptCommand{
		RunID: fixture.scope.RunID, ExpectedStateVersion: fixture.state.StateVersion,
		ActorUserID: "admin-operator", Reason: "账务核对未完成时不得收口", Now: now.Add(time.Minute),
	})
	if !errors.Is(err, ErrAdminAgentRunBillingUnresolved) {
		t.Fatalf("unresolved billing error = %v", err)
	}
	assertAdminInterruptFactCounts(t, db, fixture.scope.RunID, 0, 1)
	var stored model.AgentRun
	if err := db.First(&stored, "id = ?", fixture.scope.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != agentruntime.RunRunning || stored.StateVersion != fixture.state.StateVersion {
		t.Fatalf("blocked run changed = %#v", stored)
	}
}

func TestInterruptAdminAgentRunMapsMissingLinkedBillingToStableFailure(t *testing.T) {
	repo, db := openAdminAgentRunRepositorySQLite(t)
	now := time.Date(2026, time.August, 24, 17, 5, 0, 0, time.UTC)
	fixture := createAdminInterruptFixture(t, db, "run-admin-missing-billing", agentruntime.RunQueued, now)
	task := model.Task{
		ID: "task-admin-missing-billing", UserID: fixture.scope.ActorUserID, Audience: model.TaskAudienceInternal,
		Type: "agent_runtime_model", Capability: "text", Status: model.TaskStatusQueued,
		Operation: "agent_model:" + fixture.scope.RunID, BillingOrderID: "missing-order", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	_, err := repo.InterruptAdminAgentRun(AdminAgentRunInterruptCommand{
		RunID: fixture.scope.RunID, ExpectedStateVersion: fixture.state.StateVersion,
		ActorUserID: "admin-operator", Reason: "账务关联缺失时不得终止", Now: now.Add(time.Minute),
	})
	if !errors.Is(err, ErrAdminAgentRunBillingUnresolved) {
		t.Fatalf("missing linked billing error = %v", err)
	}
	assertAdminInterruptFactCounts(t, db, fixture.scope.RunID, 0, 1)
}

func TestInterruptAdminAgentRunBlocksActiveTaskWithoutBillingReference(t *testing.T) {
	repo, db := openAdminAgentRunRepositorySQLite(t)
	now := time.Date(2026, time.August, 24, 17, 10, 0, 0, time.UTC)
	fixture := createAdminInterruptFixture(t, db, "run-admin-empty-billing", agentruntime.RunQueued, now)
	task := model.Task{
		ID: "task-admin-empty-billing", UserID: fixture.scope.ActorUserID, Audience: model.TaskAudienceInternal,
		Type: "agent_runtime_model", Capability: "text", Status: model.TaskStatusQueued,
		Operation: "agent_model:" + fixture.scope.RunID, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	_, err := repo.InterruptAdminAgentRun(AdminAgentRunInterruptCommand{
		RunID: fixture.scope.RunID, ExpectedStateVersion: fixture.state.StateVersion,
		ActorUserID: "admin-operator", Reason: "账单引用缺失时不得终止", Now: now.Add(time.Minute),
	})
	if !errors.Is(err, ErrAdminAgentRunBillingUnresolved) {
		t.Fatalf("empty linked billing error = %v", err)
	}
	assertAdminInterruptFactCounts(t, db, fixture.scope.RunID, 0, 1)
}

func TestInterruptAdminAgentRunConcurrentCASCommitsOneTerminalTransition(t *testing.T) {
	repo, db := openAdminAgentRunRepositorySQLite(t)
	now := time.Date(2026, time.August, 24, 17, 15, 0, 0, time.UTC)
	fixture := createAdminInterruptFixture(t, db, "run-admin-concurrent", agentruntime.RunQueued, now)
	start := make(chan struct{})
	errorsByAttempt := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for attempt := 0; attempt < 2; attempt++ {
		go func(attempt int) {
			ready.Done()
			<-start
			_, err := repo.InterruptAdminAgentRun(AdminAgentRunInterruptCommand{
				RunID: fixture.scope.RunID, ExpectedStateVersion: fixture.state.StateVersion,
				ActorUserID: "admin-operator", Reason: "并发终止只允许一个提交", Now: now.Add(time.Duration(attempt+1) * time.Second),
			})
			errorsByAttempt <- err
		}(attempt)
	}
	ready.Wait()
	close(start)
	successes := 0
	conflicts := 0
	for attempt := 0; attempt < 2; attempt++ {
		err := <-errorsByAttempt
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrAdminAgentRunStateConflict), errors.Is(err, ErrAdminAgentRunTerminal):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent error = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent outcomes: successes=%d conflicts=%d", successes, conflicts)
	}
	assertAdminInterruptFactCounts(t, db, fixture.scope.RunID, 1, 2)
}

type adminInterruptFixture struct {
	scope          agentruntime.Scope
	state          agentruntime.RuntimeState
	sequence       int64
	timelineItemID string
	artifactID     string
}

func createAdminInterruptFixture(t *testing.T, db *gorm.DB, runID string, status agentruntime.RunStatus, now time.Time) adminInterruptFixture {
	t.Helper()
	const sequence = int64(7)
	createAdminAgentRunUser(t, db, "user-"+runID, runID+"@example.com", "运行用户")
	scope := agentruntime.Scope{
		TenantKind: agentruntime.TenantPersonal, TenantID: "user-" + runID,
		ThreadID: "thread-" + runID, RunID: runID, ActorUserID: "user-" + runID,
		Access:          agentruntime.AccessGrant{Level: agentruntime.AccessManager, SubscriptionActive: true},
		DomainProjectID: "project-" + runID, CanvasID: "canvas-" + runID,
	}
	delivery := agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryAnswer,
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}},
	}
	state := agentruntime.RuntimeState{
		StateVersion: 4, StepNumber: 2, MaxSteps: 24, Status: status, ExpectedDelivery: &delivery,
		UserMessage: "创建五秒视频", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic},
	}
	fixture := adminInterruptFixture{scope: scope, state: state, sequence: sequence}
	if status == agentruntime.RunWaitingApproval {
		fixture.artifactID = "artifact-" + runID
		state.PendingToolCall = &agentruntime.ToolCallDecision{
			ToolCallID: "tool-" + runID, ToolName: agentruntime.ToolProductionRender, ActionVersion: 1,
			Arguments: json.RawMessage(`{"artifactId":"` + fixture.artifactID + `"}`), ExpectedDelivery: delivery,
		}
		fixture.state = state
		fixture.timelineItemID = agentFactID("timeline", runID, "tool-call", state.PendingToolCall.ToolCallID+":1")
	} else if status == agentruntime.RunWaitingInput {
		state.PendingClarification = &agentruntime.PendingClarification{
			Request: agentruntime.ClarificationDecision{
				RequestID: "clarification-" + runID,
				Questions: []agentruntime.ClarificationQuestion{{
					ID: "style", Prompt: "请选择视频风格", Type: agentruntime.ClarificationFreeText,
				}},
				ExpectedDelivery: delivery,
			},
			Answers: []agentruntime.ClarificationAnswer{},
		}
		fixture.state = state
		fixture.timelineItemID = agentFactID("timeline", runID, "clarification", state.PendingClarification.Request.RequestID)
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	thread := model.AgentThread{
		ID: scope.ThreadID, TenantKind: scope.TenantKind, TenantID: scope.TenantID, CreatedByUserID: scope.ActorUserID,
		DomainProjectID: scope.DomainProjectID, CanvasID: scope.CanvasID, Status: agentruntime.ThreadActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&thread).Error; err != nil {
		t.Fatal(err)
	}
	run := model.AgentRun{
		ID: runID, ThreadID: scope.ThreadID, ActorUserID: scope.ActorUserID, ClientRequestID: "request-" + runID,
		Status: status, LastEventSequence: sequence, StateVersion: state.StateVersion, StepNumber: state.StepNumber, MaxSteps: state.MaxSteps,
		ModelRecordID: "model-record", ModelKey: "agent-model", ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion: agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	checkpoint := model.AgentCheckpoint{
		ID: "checkpoint-" + runID, RunID: runID, Sequence: sequence, StateVersion: state.StateVersion, StateJSON: string(stateJSON), CreatedAt: now,
	}
	if err := db.Create(&checkpoint).Error; err != nil {
		t.Fatal(err)
	}
	if status == agentruntime.RunWaitingApproval {
		call := model.AgentToolCall{
			ID: "record-tool-" + runID, RunID: runID, ToolCallID: state.PendingToolCall.ToolCallID, ActionVersion: 1,
			ToolName: string(state.PendingToolCall.ToolName), Status: agentruntime.ToolCallWaitingApproval,
			ApprovalRequired: true, IdempotencyKey: runID + ":tool:1", InputJSON: string(state.PendingToolCall.Arguments), OutputJSON: `{}`,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(&call).Error; err != nil {
			t.Fatal(err)
		}
		item := model.AgentTimelineItem{
			ID: fixture.timelineItemID, TenantKind: scope.TenantKind, TenantID: scope.TenantID, ThreadID: scope.ThreadID, RunID: runID,
			Kind: model.AgentTimelineItemToolCall, Status: model.AgentTimelineItemInProgress, Ordinal: 1,
			SourceEventSequence: sequence, ContentJSON: `{"toolCallId":"` + state.PendingToolCall.ToolCallID + `"}`, StartedAt: now, CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
		plan := model.AgentProductionPlanVersion{
			ID: "plan-" + runID, PlanKey: "plan-key-" + runID, TenantKind: scope.TenantKind, TenantID: scope.TenantID,
			DomainProjectID: scope.DomainProjectID, CanvasID: scope.CanvasID, CreatedByRunID: runID, Version: 1,
			Status: model.AgentProductionPlanActive, Title: "计划", Script: "脚本", ReferencesJSON: `[]`, ShotsJSON: `[]`, ExpectedDeliveryJSON: `{}`,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(&plan).Error; err != nil {
			t.Fatal(err)
		}
		artifact := model.AgentProductionArtifact{
			ID: fixture.artifactID, PlanKey: plan.PlanKey, PlanVersionID: plan.ID, PlanVersion: 1,
			Kind: model.AgentProductionArtifactVideoClip, Status: model.AgentProductionArtifactAwaitingApproval, CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(&artifact).Error; err != nil {
			t.Fatal(err)
		}
	} else if status == agentruntime.RunWaitingInput {
		item := model.AgentTimelineItem{
			ID: fixture.timelineItemID, TenantKind: scope.TenantKind, TenantID: scope.TenantID, ThreadID: scope.ThreadID, RunID: runID,
			Kind: model.AgentTimelineItemClarification, Status: model.AgentTimelineItemInProgress, Ordinal: 1,
			SourceEventSequence: sequence, ContentJSON: `{"requestId":"` + state.PendingClarification.Request.RequestID + `"}`,
			StartedAt: now, CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
	}
	return fixture
}

func assertAdminInterruptFactCounts(t *testing.T, db *gorm.DB, runID string, interruptEvents int64, checkpoints int64) {
	t.Helper()
	var eventCount int64
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ? AND kind = ?", runID, agentruntime.EventRunInterrupted).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	var checkpointCount int64
	if err := db.Model(&model.AgentCheckpoint{}).Where("run_id = ?", runID).Count(&checkpointCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != interruptEvents || checkpointCount != checkpoints {
		t.Fatalf("fact counts: events=%d checkpoints=%d", eventCount, checkpointCount)
	}
}
