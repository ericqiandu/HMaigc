package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

func TestAgentRuntimeRepairCreatesExactlyOneNextModelTask(t *testing.T) {
	decision := `{"kind":"final","final":{"message":"需要先生成图片。","expectedDelivery":{"kind":"generated_asset","requiredArtifacts":["image"],"completionCriteria":[{"fact":"artifact","artifact":"image"}]}}}`
	server, calls := newAgentRuntimeDecisionServer(t, decision)
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	input := StartAgentRuntimeInput{Scope: agentRuntimeServiceScope(), ClientRequestID: "client-request-repair", UserMessage: "生成一张图片", MaxSteps: 4}
	started, err := svc.StartAgentRuntime(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	progress, err := svc.ResumeAgentRuntime(input.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Status != agentruntime.RunRunning || progress.State.StepNumber != 1 || progress.State.Verification == nil || progress.State.Verification.Status != agentruntime.VerificationRepairable || progress.ModelTask == nil {
		t.Fatalf("repair progress = %#v", progress)
	}
	if progress.ModelTask.ID == started.ModelTask.ID || progress.ModelTask.Status != model.TaskStatusQueued {
		t.Fatalf("next model task = %#v", progress.ModelTask)
	}
	replayed, err := svc.ResumeAgentRuntime(input.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ModelTask == nil || replayed.ModelTask.ID != progress.ModelTask.ID || calls.Load() != 1 {
		t.Fatalf("repair replay = %#v calls=%d", replayed, calls.Load())
	}
	var taskCount int64
	var billingCount int64
	if err := db.Model(&model.Task{}).Where("user_id = ? AND type = ?", input.Scope.ActorUserID, agentRuntimeModelTaskType).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("user_id = ? AND idempotency_key LIKE ?", input.Scope.ActorUserID, "agent-runtime:%").Count(&billingCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 2 || billingCount != 2 {
		t.Fatalf("repair facts: tasks=%d billings=%d", taskCount, billingCount)
	}
}

func TestAgentRuntimeToolDecisionWaitsWithoutSubmittingAnotherModelTask(t *testing.T) {
	decision := `{"kind":"tool_call","toolCall":{"toolCallId":"call-read-state","toolName":"canvas.read_state","actionVersion":1,"arguments":{}}}`
	server, calls := newAgentRuntimeDecisionServer(t, decision)
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	input := StartAgentRuntimeInput{Scope: agentRuntimeServiceScope(), ClientRequestID: "client-request-tool", UserMessage: "读取画布", MaxSteps: 4}
	if _, err := svc.StartAgentRuntime(input); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	progress, err := svc.ResumeAgentRuntime(input.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Status != agentruntime.RunWaitingTool || progress.State.PendingToolCall == nil || progress.State.PendingToolCall.ToolName != agentruntime.ToolCanvasReadState || progress.ModelTask != nil {
		t.Fatalf("tool progress = %#v", progress)
	}
	replayed, err := svc.ResumeAgentRuntime(input.Scope)
	if err != nil {
		t.Fatal(err)
	}
	var taskCount int64
	if err := db.Model(&model.Task{}).Where("user_id = ? AND type = ?", input.Scope.ActorUserID, agentRuntimeModelTaskType).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if replayed.State.Status != agentruntime.RunWaitingTool || taskCount != 1 || calls.Load() != 1 {
		t.Fatalf("tool replay = %#v tasks=%d calls=%d", replayed, taskCount, calls.Load())
	}
}

func TestConcurrentAgentRuntimeResumeCommitsOneTerminalTransition(t *testing.T) {
	decision := `{"kind":"final","final":{"message":"完成。","expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`
	server, calls := newAgentRuntimeDecisionServer(t, decision)
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	input := StartAgentRuntimeInput{Scope: agentRuntimeServiceScope(), ClientRequestID: "client-request-concurrent", UserMessage: "给出答案", MaxSteps: 4}
	if _, err := svc.StartAgentRuntime(input); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	results := make([]*AgentRuntimeProgress, 2)
	errorsByWorker := make([]error, 2)
	var workers sync.WaitGroup
	for index := range results {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			results[worker], errorsByWorker[worker] = svc.ResumeAgentRuntime(input.Scope)
		}(index)
	}
	workers.Wait()
	for index, err := range errorsByWorker {
		if err != nil {
			t.Fatalf("resume worker %d: %v", index, err)
		}
		if results[index] == nil || results[index].State.Status != agentruntime.RunSucceeded {
			t.Fatalf("resume worker %d result = %#v", index, results[index])
		}
	}
	var eventCount int64
	var checkpointCount int64
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ? AND kind = ?", input.Scope.RunID, agentruntime.EventRunCompleted).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentCheckpoint{}).Where("run_id = ? AND state_version = ?", input.Scope.RunID, 2).Count(&checkpointCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 || checkpointCount != 1 || calls.Load() != 1 {
		t.Fatalf("concurrent facts: completedEvents=%d checkpoints=%d calls=%d", eventCount, checkpointCount, calls.Load())
	}
}

func newAgentRuntimeDecisionServer(t *testing.T, decision string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	if _, err := agentruntime.ParseModelDecision([]byte(decision)); err != nil {
		t.Fatalf("invalid test decision: %v", err)
	}
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	calls := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		response := agentRuntimeChatResponse{Choices: []agentRuntimeChatChoice{{Message: agentRuntimeChatMessage{Content: decision}}}}
		if err := json.NewEncoder(writer).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	return server, calls
}

type agentRuntimeChatResponse struct {
	Choices []agentRuntimeChatChoice `json:"choices"`
}

type agentRuntimeChatChoice struct {
	Message agentRuntimeChatMessage `json:"message"`
}

type agentRuntimeChatMessage struct {
	Content string `json:"content"`
}
