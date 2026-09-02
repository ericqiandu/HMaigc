package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func TestAgentRuntimeRepairCreatesExactlyOneNextModelTask(t *testing.T) {
	decision := `{"kind":"final","final":{"message":"需要先生成图片。","expectedDelivery":{"kind":"generated_asset","requiredArtifacts":["image"],"completionCriteria":[{"fact":"artifact","artifact":"image"}]}}}`
	server, calls := newAgentRuntimeDecisionServer(t, decision)
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	input := StartAgentRuntimeInput{Scope: agentRuntimeServiceScope(), ClientRequestID: "client-request-repair", UserMessage: "生成一张图片", MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput()}
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

func TestAgentModelCompletionWakeupTerminatesRunWhenNextReservationHasInsufficientCredits(t *testing.T) {
	decision := `{"kind":"final","final":{"message":"需要先生成图片。","expectedDelivery":{"kind":"generated_asset","requiredArtifacts":["image"],"completionCriteria":[{"fact":"artifact","artifact":"image"}]}}}`
	server, calls := newAgentRuntimeDecisionServer(t, decision)
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	scope := agentRuntimeServiceScope()
	if _, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "client-drive-insufficient-credits", UserMessage: "生成一张图片", MaxSteps: 4,
		Configuration: guidedAgentRuntimeConfigurationInput(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditAccount{}).Where("user_id = ?", scope.ActorUserID).Update("available_microcredits", 0).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	state, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != agentruntime.RunFailed || state.FailureCode != "insufficient_credits" {
		t.Fatalf("insufficient-credit run state = %#v", state)
	}
	var taskCount int64
	var billingCount int64
	if err := db.Model(&model.Task{}).Where("user_id = ? AND type = ?", scope.ActorUserID, agentRuntimeModelTaskType).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("user_id = ? AND scene = ?", scope.ActorUserID, "agent_runtime_model").Count(&billingCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 1 || billingCount != 1 || calls.Load() != 1 {
		t.Fatalf("insufficient-credit facts: tasks=%d billings=%d calls=%d", taskCount, billingCount, calls.Load())
	}
	if _, err := svc.ResumeAgentRuntime(scope); err != nil {
		t.Fatalf("replay terminal run: %v", err)
	}
}

func TestConcurrentAgentRuntimeResumeCommitsOneTerminalTransition(t *testing.T) {
	decision := `{"kind":"final","final":{"message":"完成。","expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`
	server, calls := newAgentRuntimeDecisionServer(t, decision)
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	input := StartAgentRuntimeInput{Scope: agentRuntimeServiceScope(), ClientRequestID: "client-request-concurrent", UserMessage: "给出答案", MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput()}
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

func createAgentRuntimeCanvas(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC()
	if err := db.Save(&model.CanvasProject{
		ID: "runtime-canvas", ProjectID: "runtime-project", UserID: "runtime-user", Title: "Agent Canvas", Revision: 7,
		PayloadJSON: `{"nodes":[],"connections":[]}`, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func newAgentRuntimeDecisionServer(t *testing.T, decision string, expected ...agentruntime.ExpectedDelivery) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	if len(expected) > 1 {
		t.Fatal("test decision accepts at most one expected delivery")
	}
	if len(expected) == 1 {
		decision = agentRuntimeToolDecisionWithDelivery(t, decision, expected[0])
	}
	if _, err := agentruntime.ParseModelDecision([]byte(decision)); err != nil {
		t.Fatalf("invalid test decision: %v", err)
	}
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	calls := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeAgentRuntimeChatStream(t, writer, "chatcmpl-decision", decision, 0, 0, 0)
	}))
	return server, calls
}

func agentRuntimeToolDecisionWithDelivery(t *testing.T, payload string, expected agentruntime.ExpectedDelivery) string {
	t.Helper()
	var envelope struct {
		Kind     agentruntime.DecisionKind `json:"kind"`
		ToolCall struct {
			ToolCallID    string                `json:"toolCallId"`
			ToolName      agentruntime.ToolName `json:"toolName"`
			ActionVersion int                   `json:"actionVersion"`
			Arguments     json.RawMessage       `json:"arguments"`
		} `json:"toolCall"`
	}
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		t.Fatalf("decode test tool decision: %v", err)
	}
	decision := agentruntime.ModelDecision{
		Kind: envelope.Kind,
		ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: envelope.ToolCall.ToolCallID, ToolName: envelope.ToolCall.ToolName,
			ActionVersion: envelope.ToolCall.ActionVersion, Arguments: envelope.ToolCall.Arguments,
			ExpectedDelivery: expected,
		},
	}
	encoded, err := json.Marshal(decision)
	if err != nil {
		t.Fatalf("encode test tool decision: %v", err)
	}
	return string(encoded)
}

func agentRuntimeTestAnswerDelivery() agentruntime.ExpectedDelivery {
	return agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryAnswer,
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}},
	}
}

func agentRuntimeTestImageDelivery() agentruntime.ExpectedDelivery {
	return agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryGeneratedAsset,
		RequiredArtifacts:  []agentruntime.ArtifactKind{agentruntime.ArtifactImage},
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactArtifact, Artifact: agentruntime.ArtifactImage}},
	}
}

func agentRuntimeTestCanvasDelivery() agentruntime.ExpectedDelivery {
	return agentruntime.ExpectedDelivery{
		Kind: agentruntime.DeliveryCanvasChange, TargetCanvasID: "runtime-canvas",
		RequiredArtifacts:  []agentruntime.ArtifactKind{agentruntime.ArtifactCanvasRevision},
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactCanvasRevision}},
	}
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
