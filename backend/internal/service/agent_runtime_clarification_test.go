package service

import (
	"sync"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

func TestSubmitAgentClarificationResponseResumesSameRunOnce(t *testing.T) {
	decision := `{"kind":"clarification_request","clarification":{"requestId":"clarify-service","questions":[{"id":"duration","prompt":"广告时长是多少？","type":"single_choice","options":[{"id":"15s","label":"15 秒"},{"id":"30s","label":"30 秒"}]}],"expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`
	server, calls := newAgentRuntimeDecisionServer(t, decision)
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "clarification-service-run", UserMessage: "生成汽车广告剧本",
		Configuration: guidedAgentRuntimeConfigurationInput(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	waiting, err := svc.advanceAgentRun(scope, agentWakeModelTaskFinished)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.State.Status != agentruntime.RunWaitingInput {
		t.Fatalf("waiting state = %#v", waiting.State)
	}

	submission := agentruntime.ClarificationResponseSubmission{
		RequestID: "clarify-service", ExpectedStateVersion: waiting.State.StateVersion,
		QuestionID: "duration", Answer: agentruntime.ClarificationAnswerInput{SelectedOptionIDs: []string{"30s"}}, Complete: true,
	}
	completed, err := svc.SubmitAgentClarificationResponse(scope, submission)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State.Status != agentruntime.RunRunning || completed.State.PendingClarification != nil || len(completed.State.ClarificationHistory) != 1 || completed.ModelTask == nil {
		t.Fatalf("completed clarification = %#v", completed)
	}
	if completed.Run.ID != started.Run.ID || completed.State.StepNumber != waiting.State.StepNumber {
		t.Fatalf("run identity changed: started=%s completed=%s state=%#v", started.Run.ID, completed.Run.ID, completed.State)
	}

	replayed, err := svc.SubmitAgentClarificationResponse(scope, submission)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.State.StateVersion != completed.State.StateVersion || replayed.ModelTask != nil {
		t.Fatalf("replayed clarification = %#v", replayed)
	}
	var taskCount int64
	if err := db.Model(&model.Task{}).Where("user_id = ? AND type = ?", scope.ActorUserID, agentRuntimeModelTaskType).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 2 || calls.Load() != 1 {
		t.Fatalf("resume side effects: tasks=%d calls=%d", taskCount, calls.Load())
	}
}

func TestConcurrentFinalClarificationSubmissionsCommitAndWakeOnce(t *testing.T) {
	decision := `{"kind":"clarification_request","clarification":{"requestId":"clarify-concurrent","questions":[{"id":"duration","prompt":"广告时长是多少？","type":"single_choice","options":[{"id":"15s","label":"15 秒"},{"id":"30s","label":"30 秒"}]}],"expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`
	server, calls := newAgentRuntimeDecisionServer(t, decision)
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	scope := agentRuntimeServiceScope()
	if _, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "clarification-concurrent-run", UserMessage: "生成汽车广告剧本",
		Configuration: guidedAgentRuntimeConfigurationInput(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	waiting, err := svc.advanceAgentRun(scope, agentWakeModelTaskFinished)
	if err != nil {
		t.Fatal(err)
	}
	submission := agentruntime.ClarificationResponseSubmission{
		RequestID: "clarify-concurrent", ExpectedStateVersion: waiting.State.StateVersion,
		QuestionID: "duration", Answer: agentruntime.ClarificationAnswerInput{SelectedOptionIDs: []string{"30s"}}, Complete: true,
	}

	var workers sync.WaitGroup
	errorsByWorker := make([]error, 2)
	progressByWorker := make([]*AgentRuntimeProgress, 2)
	for index := range errorsByWorker {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			progressByWorker[worker], errorsByWorker[worker] = svc.SubmitAgentClarificationResponse(scope, submission)
		}(index)
	}
	workers.Wait()
	for index, submitErr := range errorsByWorker {
		if submitErr != nil {
			t.Fatalf("worker %d error = %v", index, submitErr)
		}
		if progressByWorker[index] == nil || progressByWorker[index].State.Status != agentruntime.RunRunning {
			t.Fatalf("worker %d progress = %#v", index, progressByWorker[index])
		}
	}
	var taskCount, respondedCount int64
	if err := db.Model(&model.Task{}).Where("user_id = ? AND type = ?", scope.ActorUserID, agentRuntimeModelTaskType).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ? AND kind = ?", scope.RunID, agentruntime.EventClarificationResponded).Count(&respondedCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 2 || respondedCount != 1 || calls.Load() != 1 {
		t.Fatalf("concurrent side effects: tasks=%d responded=%d calls=%d", taskCount, respondedCount, calls.Load())
	}
}

func TestSubmitAgentClarificationResponseMapsVersionAndIdentityConflicts(t *testing.T) {
	decision := `{"kind":"clarification_request","clarification":{"requestId":"clarify-errors","questions":[{"id":"duration","prompt":"广告时长是多少？","type":"single_choice","options":[{"id":"15s","label":"15 秒"},{"id":"30s","label":"30 秒"}]}],"expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`
	server, _ := newAgentRuntimeDecisionServer(t, decision)
	defer server.Close()
	svc, _, _ := newAgentRuntimeServiceFixture(t, server.URL)
	scope := agentRuntimeServiceScope()
	if _, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "clarification-error-run", UserMessage: "生成汽车广告剧本",
		Configuration: guidedAgentRuntimeConfigurationInput(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	waiting, err := svc.advanceAgentRun(scope, agentWakeModelTaskFinished)
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.SubmitAgentClarificationResponse(scope, agentruntime.ClarificationResponseSubmission{
		RequestID: "clarify-errors", ExpectedStateVersion: waiting.State.StateVersion - 1,
		QuestionID: "duration", Answer: agentruntime.ClarificationAnswerInput{SelectedOptionIDs: []string{"30s"}},
	})
	assertAgentClarificationServiceError(t, err, "agent_clarification_conflict", waiting.State.StateVersion)

	_, err = svc.SubmitAgentClarificationResponse(scope, agentruntime.ClarificationResponseSubmission{
		RequestID: "another-request", ExpectedStateVersion: waiting.State.StateVersion,
		QuestionID: "duration", Answer: agentruntime.ClarificationAnswerInput{SelectedOptionIDs: []string{"30s"}},
	})
	assertAgentClarificationServiceError(t, err, "agent_clarification_conflict", waiting.State.StateVersion)
}

func assertAgentClarificationServiceError(t *testing.T, err error, code string, latestVersion int) {
	t.Helper()
	clarificationErr, ok := err.(*AgentClarificationError)
	if !ok || clarificationErr.ErrorCode != code || clarificationErr.LatestStateVersion != latestVersion {
		t.Fatalf("clarification error = %#v, want code=%q latest=%d", err, code, latestVersion)
	}
}
