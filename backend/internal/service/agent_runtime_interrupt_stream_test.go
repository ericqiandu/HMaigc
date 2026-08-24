package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func TestAgentRuntimeInterruptCancelsProviderWithoutVisibleDeltaAcrossServices(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	requestStarted := make(chan struct{})
	providerCancelled := make(chan struct{})
	releaseProvider := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseProvider) }) }

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := writer.(http.Flusher)
		if !ok {
			t.Error("test server does not support streaming flush")
			return
		}
		writer.WriteHeader(http.StatusOK)
		flusher.Flush()
		close(requestStarted)
		select {
		case <-request.Context().Done():
			close(providerCancelled)
		case <-releaseProvider:
		}
	}))
	defer server.Close()
	defer release()

	workerService, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	controlService := New(workerService.repo, workerService.dataDir)
	scope := agentRuntimeServiceScope()
	started, err := workerService.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "interrupt-provider-without-visible-delta", UserMessage: "只用文字回答",
		MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.ModelTask == nil {
		t.Fatal("agent runtime did not create its internal model task")
	}
	processed := make(chan error, 1)
	go func() { processed <- workerService.ProcessNextTask() }()

	select {
	case <-requestStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("provider request did not start")
	}
	state, err := workerService.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != agentruntime.RunRunning {
		t.Fatalf("provider request runtime status = %s", state.Status)
	}
	interrupted, err := controlService.SubmitScopedAgentInterrupt(&model.User{ID: scope.ActorUserID}, scope.RunID, state.StateVersion)
	if err != nil {
		t.Fatal(err)
	}
	if interrupted.State.Status != agentruntime.RunCancelled {
		t.Fatalf("interrupt status = %s", interrupted.State.Status)
	}

	select {
	case <-providerCancelled:
	case <-time.After(2 * time.Second):
		release()
		select {
		case <-processed:
		case <-time.After(3 * time.Second):
		}
		t.Fatal("durable run interruption did not cancel the provider request")
	}
	select {
	case processErr := <-processed:
		if processErr != nil {
			t.Fatalf("cancelled worker returned an operational error: %v", processErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("provider worker did not finish after cancellation")
	}

	var task model.Task
	if err := db.First(&task, "id = ?", started.ModelTask.ID).Error; err != nil {
		t.Fatal(err)
	}
	if task.Status != model.TaskStatusCancelled {
		t.Fatalf("internal model task status = %s", task.Status)
	}
	var order model.BillingOrder
	if err := db.First(&order, "task_id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if order.Status != model.BillingStatusUncertain {
		t.Fatalf("cancelled provider billing status = %s", order.Status)
	}
}

func TestAgentRuntimeInterruptWinsAgainstImmediateNonVisibleProviderCompletion(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	requestStarted := make(chan struct{})
	releaseProvider := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := writer.(http.Flusher)
		if !ok {
			t.Error("test server does not support streaming flush")
			return
		}
		writer.WriteHeader(http.StatusOK)
		flusher.Flush()
		close(requestStarted)
		<-releaseProvider
		writeAgentRuntimeChatStream(t, writer, "chatcmpl-interrupted-tool", `{"kind":"tool_call","toolCall":{"toolCallId":"tool-after-interrupt","toolName":"skill.load","actionVersion":1,"arguments":{"dir":"selected-skill"},"expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`, 12, 0, 3)
	}))
	defer server.Close()

	workerService, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	controlService := New(workerService.repo, workerService.dataDir)
	scope := agentRuntimeServiceScope()
	started, err := workerService.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "interrupt-immediate-non-visible-completion", UserMessage: "读取技能",
		MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput(),
	})
	if err != nil {
		t.Fatal(err)
	}
	processed := make(chan error, 1)
	go func() { processed <- workerService.ProcessNextTask() }()
	select {
	case <-requestStarted:
	case <-time.After(3 * time.Second):
		close(releaseProvider)
		t.Fatal("provider request did not start")
	}
	state, err := workerService.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		close(releaseProvider)
		t.Fatal(err)
	}
	if _, err := controlService.SubmitScopedAgentInterrupt(&model.User{ID: scope.ActorUserID}, scope.RunID, state.StateVersion); err != nil {
		close(releaseProvider)
		t.Fatal(err)
	}
	close(releaseProvider)
	select {
	case processErr := <-processed:
		if processErr != nil {
			t.Fatalf("cancelled worker returned an operational error: %v", processErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("provider worker did not finish")
	}

	var task model.Task
	if err := db.First(&task, "id = ?", started.ModelTask.ID).Error; err != nil {
		t.Fatal(err)
	}
	if task.Status != model.TaskStatusCancelled {
		t.Fatalf("internal model task status = %s", task.Status)
	}
	var order model.BillingOrder
	if err := db.First(&order, "task_id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if order.Status != model.BillingStatusUncertain {
		t.Fatalf("cancelled provider billing status = %s", order.Status)
	}
}

func TestAgentRuntimeInterruptStopsProviderStreamOutput(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	firstDeltaSent := make(chan struct{})
	releaseSecondDelta := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseSecondDelta) }) }
	defer release()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := writer.(http.Flusher)
		if !ok {
			t.Error("test server does not support streaming flush")
			return
		}
		writeAgentRuntimeInterruptChunk(t, writer, `{"kind":"final","final":{"message":"第一段`)
		flusher.Flush()
		close(firstDeltaSent)
		<-releaseSecondDelta
		writeAgentRuntimeInterruptChunk(t, writer, `第二段","expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`)
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "interrupt-active-provider-stream", UserMessage: "只用文字回答",
		MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.ModelTask == nil {
		t.Fatal("agent runtime did not create its internal model task")
	}
	processed := make(chan error, 1)
	go func() { processed <- svc.ProcessNextTask() }()

	select {
	case <-firstDeltaSent:
	case <-time.After(3 * time.Second):
		t.Fatal("provider did not flush the first visible delta")
	}
	itemID := agentruntime.AgentMessageItemID(scope.RunID, 0)
	waitForAgentMessageContent(t, db, itemID, "第一段")
	state, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	interrupted, err := svc.SubmitScopedAgentInterrupt(&model.User{ID: scope.ActorUserID}, scope.RunID, state.StateVersion)
	if err != nil {
		t.Fatal(err)
	}
	if interrupted.State.Status != agentruntime.RunCancelled {
		t.Fatalf("interrupt status = %s", interrupted.State.Status)
	}
	interruptSequence := interrupted.Run.LastEventSequence
	release()
	select {
	case processErr := <-processed:
		if processErr != nil {
			t.Fatalf("cancelled worker returned an operational error: %v", processErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("provider worker did not stop after the run interruption fence")
	}

	var task model.Task
	if err := db.First(&task, "id = ?", started.ModelTask.ID).Error; err != nil {
		t.Fatal(err)
	}
	if task.Status != model.TaskStatusCancelled {
		t.Fatalf("internal model task status = %s", task.Status)
	}
	var item model.AgentTimelineItem
	if err := db.First(&item, "id = ?", itemID).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(item.ContentJSON, "第二段") {
		t.Fatalf("late provider delta reached the durable message projection: %s", item.ContentJSON)
	}
	records, err := svc.repo.AgentTimelineEventsAfter(scope, interruptSequence, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("provider stream persisted facts after interruption: %#v", records)
	}
}

func writeAgentRuntimeInterruptChunk(t *testing.T, writer http.ResponseWriter, content string) {
	t.Helper()
	chunk := map[string]any{
		"id":      "chatcmpl-interrupted",
		"choices": []map[string]any{{"delta": map[string]any{"content": content}}},
	}
	encoded, err := json.Marshal(chunk)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = writer.Write([]byte("data: " + string(encoded) + "\n\n"))
}

func waitForAgentMessageContent(t *testing.T, db *gorm.DB, itemID string, expected string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var item model.AgentTimelineItem
		err := db.First(&item, "id = ?", itemID).Error
		if err == nil && strings.Contains(item.ContentJSON, expected) {
			return
		}
		if err != nil && err != gorm.ErrRecordNotFound {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("agent message %s did not persist %q", itemID, expected)
}
