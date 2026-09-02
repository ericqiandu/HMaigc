package service

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/skillcatalog"
)

func TestAdvanceAgentRunExpiresPersistedDeadlineBeforeResumingWork(t *testing.T) {
	server, calls := newAgentRuntimeDecisionServer(t, `{"kind":"final","final":{"message":"完成。","expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`)
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "expired-runtime", UserMessage: "回答问题",
		MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput(),
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	state.Limits = &agentruntime.RuntimeLimits{
		MaxToolCalls: agentRuntimeMaxToolCalls, StartedAt: now.Add(-time.Hour), DeadlineAt: now.Add(-time.Minute),
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentCheckpoint{}).
		Where("run_id = ? AND state_version = ?", scope.RunID, state.StateVersion).
		Update("state_json", string(stateJSON)).Error; err != nil {
		t.Fatal(err)
	}

	progress, err := svc.advanceAgentRun(scope, agentWakeStaleRecovery)
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Status != agentruntime.RunFailed || progress.State.FailureCode != "runtime_deadline_exceeded" || progress.ModelTask != nil {
		t.Fatalf("expired runtime progress = %#v", progress)
	}
	if calls.Load() != 0 || started.ModelTask == nil {
		t.Fatalf("deadline recovery side effects: modelCalls=%d startedTask=%#v", calls.Load(), started.ModelTask)
	}
	var storedTask model.Task
	if err := db.Where("id = ?", started.ModelTask.ID).First(&storedTask).Error; err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != model.TaskStatusCancelled {
		t.Fatalf("expired runtime model task status = %q, want %q", storedTask.Status, model.TaskStatusCancelled)
	}
}

func TestCoordinateCompletedCloudToolNeverExecutesAgainAfterRestart(t *testing.T) {
	server, _ := newAgentRuntimeDecisionServer(t, `{"kind":"final","final":{"message":"完成。","expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`)
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	scope := agentRuntimeServiceScope()
	if _, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "completed-tool-restart", UserMessage: "读取画布",
		MaxSteps: 4, Configuration: AgentRuntimeConfigurationInput{ExecutionMode: agentruntime.ExecutionAutomatic},
	}); err != nil {
		t.Fatal(err)
	}
	state, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	requested, err := agentruntime.Advance(state, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall,
		ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "completed-read", ToolName: agentruntime.ToolCanvasRead, ActionVersion: 1,
			Arguments:        json.RawMessage(`{"canvasId":"runtime-canvas","selectedNodeIds":[],"includeViewport":true}`),
			ExpectedDelivery: agentRuntimeTestAnswerDelivery(),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CommitAgentRuntimeTransition(scope, state, requested, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	resolved, err := agentruntime.ResolveTool(requested.State, agentruntime.ToolResolution{
		ToolCallID: "completed-read", ActionVersion: 1, Succeeded: true,
		Output: json.RawMessage(`{"canvasId":"runtime-canvas","revision":7}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CommitAgentRuntimeTransition(scope, requested.State, resolved, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	progress, err := svc.coordinatePendingAgentTool(scope, CoordinateAgentToolInput{
		ToolCallID: "completed-read", ActionVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.StateVersion != resolved.State.StateVersion || progress.State.Status != agentruntime.RunRunning {
		t.Fatalf("completed tool replay changed checkpoint = %#v", progress.State)
	}
	var toolCount int64
	if err := db.Model(&model.AgentToolCall{}).Where("run_id = ? AND tool_call_id = ?", scope.RunID, "completed-read").Count(&toolCount).Error; err != nil {
		t.Fatal(err)
	}
	if toolCount != 1 {
		t.Fatalf("completed tool replay count = %d, want 1", toolCount)
	}
}

func TestCoordinatePendingCloudReadExecutesRegistryCapability(t *testing.T) {
	server, _ := newAgentRuntimeDecisionServer(t, `{"kind":"final","final":{"message":"完成。","expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`)
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	scope := agentRuntimeServiceScope()
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", scope.CanvasID).
		Update("payload_json", `{"nodes":[{"id":"node-1","type":"image"}],"connections":[],"viewport":{"x":2,"y":3,"k":1.5},"privateDraft":"must-not-leak"}`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "pending-cloud-read", UserMessage: "读取画布",
		MaxSteps: 4, Configuration: AgentRuntimeConfigurationInput{ExecutionMode: agentruntime.ExecutionAutomatic},
	}); err != nil {
		t.Fatal(err)
	}
	state, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	requested, err := agentruntime.Advance(state, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall,
		ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "pending-cloud-read-call", ToolName: agentruntime.ToolCanvasRead, ActionVersion: 1,
			Arguments:        json.RawMessage(`{"canvasId":"runtime-canvas","selectedNodeIds":["node-1"],"includeViewport":true}`),
			ExpectedDelivery: agentRuntimeTestAnswerDelivery(),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CommitAgentRuntimeTransition(scope, state, requested, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	progress, err := svc.coordinatePendingAgentTool(scope, CoordinateAgentToolInput{
		ToolCallID: "pending-cloud-read-call", ActionVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Status != agentruntime.RunRunning || progress.State.LastToolResult == nil || !progress.State.LastToolResult.Succeeded {
		t.Fatalf("cloud read progress = %#v; last result = %#v", progress.State, progress.State.LastToolResult)
	}
	decoded, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolCanvasRead, progress.State.LastToolResult.Output)
	if err != nil {
		t.Fatal(err)
	}
	result := decoded.(agentruntime.CanvasReadResult)
	if result.CanvasID != scope.CanvasID || len(result.Nodes) != 1 || !strings.Contains(string(result.Nodes[0]), `"id":"node-1"`) || result.Viewport.Zoom != 1.5 {
		t.Fatalf("cloud read result = %#v", result)
	}
	if strings.Contains(string(progress.State.LastToolResult.Output), "must-not-leak") {
		t.Fatalf("cloud read leaked private canvas facts: %s", progress.State.LastToolResult.Output)
	}
	recorded, err := svc.repo.AgentToolCallForScope(scope, "pending-cloud-read-call", 1)
	if err != nil {
		t.Fatal(err)
	}
	if recorded.Status != agentruntime.ToolCallSucceeded {
		t.Fatalf("cloud read tool status = %q", recorded.Status)
	}
}

func TestCoordinatePendingAgentToolRejectsRetiredRunContractBeforeExecution(t *testing.T) {
	server, _ := newAgentRuntimeDecisionServer(t, `{"kind":"final","final":{"message":"完成。","expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`)
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	scope := agentRuntimeServiceScope()
	if _, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "retired-tool-contract", UserMessage: "读取画布",
		MaxSteps: 4, Configuration: AgentRuntimeConfigurationInput{ExecutionMode: agentruntime.ExecutionAutomatic},
	}); err != nil {
		t.Fatal(err)
	}
	state, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	requested, err := agentruntime.Advance(state, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall,
		ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "retired-contract-read", ToolName: agentruntime.ToolCanvasRead, ActionVersion: 1,
			Arguments:        json.RawMessage(`{"canvasId":"runtime-canvas","selectedNodeIds":[],"includeViewport":true}`),
			ExpectedDelivery: agentRuntimeTestAnswerDelivery(),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CommitAgentRuntimeTransition(scope, state, requested, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentRun{}).Where("id = ?", scope.RunID).
		Update("tool_schema_version", agentruntime.ProductionToolSchemaVersion).Error; err != nil {
		t.Fatal(err)
	}

	_, err = svc.coordinatePendingAgentTool(scope, CoordinateAgentToolInput{
		ToolCallID: "retired-contract-read", ActionVersion: 1,
	})
	if err == nil || !strings.Contains(err.Error(), agentruntime.FailureRuntimeSchemaRetired) {
		t.Fatalf("error = %v, want retired runtime contract failure", err)
	}
}

func TestAdvanceAgentRunReusesOneDeterministicModelTask(t *testing.T) {
	server, calls := newAgentRuntimeDecisionServer(t, `{"kind":"final","final":{"message":"完成。","expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`)
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "coordinator-start", UserMessage: "回答问题",
		MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput(),
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.advanceAgentRun(scope, agentWakeRunStarted)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.advanceAgentRun(scope, agentWakeStaleRecovery)
	if err != nil {
		t.Fatal(err)
	}
	if started.ModelTask == nil || first.ModelTask == nil || second.ModelTask == nil ||
		started.ModelTask.ID != first.ModelTask.ID || first.ModelTask.ID != second.ModelTask.ID {
		t.Fatalf("coordinator model tasks: started=%#v first=%#v second=%#v", started.ModelTask, first.ModelTask, second.ModelTask)
	}
	var taskCount int64
	if err := db.Model(&model.Task{}).Where("user_id = ? AND type = ?", scope.ActorUserID, agentRuntimeModelTaskType).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 1 || calls.Load() != 0 {
		t.Fatalf("coordinator start side effects: tasks=%d modelCalls=%d", taskCount, calls.Load())
	}
}

func TestRecoverStaleAgentRunsSerializesConcurrentRecovery(t *testing.T) {
	server, calls := newAgentRuntimeDecisionServer(t, `{"kind":"final","final":{"message":"完成。","expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`)
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "stale-recovery", UserMessage: "回答问题",
		MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput(),
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.Model(&model.AgentRun{}).Where("id = ?", started.Run.ID).Update("updated_at", now.Add(-2*time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	errs := make([]error, 2)
	for index := range errs {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			errs[worker] = svc.RecoverStaleAgentRuns(now, 10)
		}(index)
	}
	workers.Wait()
	for _, recoverErr := range errs {
		if recoverErr != nil {
			t.Fatal(recoverErr)
		}
	}
	var taskCount int64
	if err := db.Model(&model.Task{}).Where("user_id = ? AND type = ?", scope.ActorUserID, agentRuntimeModelTaskType).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 1 || calls.Load() != 0 {
		t.Fatalf("stale recovery side effects: tasks=%d modelCalls=%d", taskCount, calls.Load())
	}
}

func TestAdvanceAgentRunExecutesFreeSkillOnceAndCreatesOneNextModelTask(t *testing.T) {
	builtins, err := skillcatalog.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	var selected skillcatalog.BuiltinSkill
	for _, builtin := range builtins {
		if builtin.Dir == "short-drama-director" {
			selected = builtin
			break
		}
	}
	if selected.Dir == "" {
		t.Fatal("short-drama-director is not published")
	}
	decision := `{"kind":"tool_call","toolCall":{"toolCallId":"coordinator-skill","toolName":"skills.load","actionVersion":1,"arguments":{"skillDir":"` + selected.Dir + `","version":` + strconv.Itoa(selected.Version) + `,"checksum":"` + selected.Checksum + `"},"expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`
	server, calls := newAgentRuntimeDecisionServer(t, decision, agentRuntimeTestAnswerDelivery())
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "coordinator-skill-run", UserMessage: "加载分镜技能",
		MaxSteps: 4, Configuration: AgentRuntimeConfigurationInput{SkillDirs: []string{selected.Dir}, ExecutionMode: agentruntime.ExecutionAutomatic},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	progress, err := svc.advanceAgentRun(scope, agentWakeModelTaskFinished)
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Status != agentruntime.RunRunning || progress.ModelTask == nil || progress.ModelTask.ID == started.ModelTask.ID ||
		len(progress.State.LoadedSkillDirs) != 1 || progress.State.LoadedSkillDirs[0] != selected.Dir {
		t.Fatalf("coordinator skill progress = %#v", progress)
	}
	replayed, err := svc.advanceAgentRun(scope, agentWakeStaleRecovery)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ModelTask == nil || replayed.ModelTask.ID != progress.ModelTask.ID {
		t.Fatalf("coordinator replay = %#v", replayed)
	}
	var taskCount int64
	var toolCount int64
	if err := db.Model(&model.Task{}).Where("user_id = ? AND type = ?", scope.ActorUserID, agentRuntimeModelTaskType).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentToolCall{}).Where("run_id = ?", scope.RunID).Count(&toolCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 2 || toolCount != 1 || calls.Load() != 1 {
		t.Fatalf("coordinator skill side effects: tasks=%d tools=%d modelCalls=%d", taskCount, toolCount, calls.Load())
	}
}

func TestAdvanceAgentRunPausesAtStructuredClarification(t *testing.T) {
	decision := `{"kind":"clarification_request","clarification":{"requestId":"clarify-ad","questions":[{"id":"duration","prompt":"广告时长是多少？","type":"single_choice","options":[{"id":"15s","label":"15 秒"},{"id":"30s","label":"30 秒"}]}],"expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`
	server, calls := newAgentRuntimeDecisionServer(t, decision)
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "coordinator-clarification", UserMessage: "生成汽车广告剧本",
		MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	progress, err := svc.advanceAgentRun(scope, agentWakeModelTaskFinished)
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Status != agentruntime.RunWaitingInput || progress.State.PendingClarification == nil || progress.ModelTask != nil {
		t.Fatalf("clarification progress = %#v", progress)
	}
	if progress.Run.ID != started.Run.ID {
		t.Fatalf("clarification run = %q, want %q", progress.Run.ID, started.Run.ID)
	}
	if progress.State.PendingClarification.Request.RequestID != "clarify-ad" || calls.Load() != 1 {
		t.Fatalf("clarification facts = %#v, calls = %d", progress.State.PendingClarification, calls.Load())
	}
	var taskCount int64
	if err := db.Model(&model.Task{}).Where("user_id = ? AND type = ?", scope.ActorUserID, agentRuntimeModelTaskType).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 1 {
		t.Fatalf("clarification created %d model tasks, want 1", taskCount)
	}
}
