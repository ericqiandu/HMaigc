package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

func TestAdvanceAgentRunReusesOneDeterministicModelTask(t *testing.T) {
	server, calls := newAgentRuntimeDecisionServer(t, `{"kind":"final","final":{"message":"完成。","expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`)
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "coordinator-start", UserMessage: "回答问题",
		Configuration: guidedAgentRuntimeConfigurationInput(),
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
		Configuration: guidedAgentRuntimeConfigurationInput(),
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
	decision := `{"kind":"tool_call","toolCall":{"toolCallId":"coordinator-skill","toolName":"skill.load","actionVersion":1,"arguments":{"dir":"storyboard-director"}}}`
	server, calls := newAgentRuntimeDecisionServer(t, decision, agentRuntimeTestAnswerDelivery())
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	svc.agentRuntimeSkillResolver = func(_ context.Context, _ string, dir string) (*Skill, error) {
		instructions := "冻结说明"
		return &Skill{Dir: dir, Name: "分镜导演", Description: "拆解镜头", DetailText: instructions, Version: 1, Checksum: agentRuntimeTestSkillChecksum(instructions)}, nil
	}
	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "coordinator-skill-run", UserMessage: "加载分镜技能",
		Configuration: AgentRuntimeConfigurationInput{SkillDirs: []string{"storyboard-director"}, ExecutionMode: agentruntime.ExecutionAutomatic},
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
		len(progress.State.LoadedSkillDirs) != 1 || progress.State.LoadedSkillDirs[0] != "storyboard-director" {
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
		Configuration: guidedAgentRuntimeConfigurationInput(),
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
