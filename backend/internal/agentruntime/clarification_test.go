package agentruntime_test

import (
	"errors"
	"reflect"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
)

func clarificationDelivery() agentruntime.ExpectedDelivery {
	return agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryAnswer,
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}},
	}
}

func clarificationDecision(requestID string) agentruntime.ModelDecision {
	return agentruntime.ModelDecision{
		Kind: agentruntime.DecisionClarificationRequest,
		Clarification: &agentruntime.ClarificationDecision{
			RequestID: requestID,
			Questions: []agentruntime.ClarificationQuestion{
				{
					ID: "style", Prompt: "广告的核心风格是什么？", Type: agentruntime.ClarificationMultiChoice,
					Options: []agentruntime.ClarificationOption{
						{ID: "luxury", Label: "豪华感"},
						{ID: "performance", Label: "性能激情"},
						{ID: "family", Label: "家庭出行"},
					},
					AllowCustomAnswer: true,
				},
				{
					ID: "duration", Prompt: "广告时长是多少？", Type: agentruntime.ClarificationSingleChoice,
					Options: []agentruntime.ClarificationOption{
						{ID: "15s", Label: "15 秒"},
						{ID: "30s", Label: "30 秒"},
					},
				},
			},
			ExpectedDelivery: clarificationDelivery(),
		},
	}
}

func clarificationBase() agentruntime.RuntimeState {
	return agentruntime.RuntimeState{
		StateVersion: 1, StepNumber: 0, MaxSteps: 6, Status: agentruntime.RunRunning,
		UserMessage:   "生成一个汽车广告剧本",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic},
	}
}

func enterClarification(t *testing.T) agentruntime.RuntimeState {
	t.Helper()
	transition, err := agentruntime.Advance(clarificationBase(), agentruntime.RuntimeInput{
		Decision: clarificationDecision("clarify-1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return transition.State
}

func TestAdvanceClarificationFreezesRequestAndWaitsForInput(t *testing.T) {
	transition, err := agentruntime.Advance(clarificationBase(), agentruntime.RuntimeInput{
		Decision: clarificationDecision("clarify-1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	state := transition.State
	if state.Status != agentruntime.RunWaitingInput || state.StateVersion != 2 || state.StepNumber != 1 || state.PendingClarification == nil {
		t.Fatalf("clarification transition = %#v", transition)
	}
	if state.PendingClarification.Request.RequestID != "clarify-1" || state.ExpectedDelivery == nil || !state.ExpectedDelivery.Equal(clarificationDelivery()) {
		t.Fatalf("frozen clarification facts = %#v", state)
	}
	if state.PendingClarification.Answers == nil {
		t.Fatal("pending clarification answers must serialize as an explicit empty array")
	}
	wantEvents := []agentruntime.EventKind{agentruntime.EventClarificationRequested, agentruntime.EventRunStatusChanged}
	if !reflect.DeepEqual(transition.EventKinds, wantEvents) {
		t.Fatalf("events = %#v, want %#v", transition.EventKinds, wantEvents)
	}
}

func TestSaveClarificationAnswerCanonicalizesAndDoesNotConsumeModelStep(t *testing.T) {
	current := enterClarification(t)
	transition, replayed, err := agentruntime.ApplyClarificationResponse(current, agentruntime.ClarificationResponseSubmission{
		RequestID: "clarify-1", ExpectedStateVersion: 2, QuestionID: "style",
		Answer: agentruntime.ClarificationAnswerInput{
			SelectedOptionIDs: []string{"family", "luxury"}, CustomText: "  都市夜景  ",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if replayed || transition.State.StateVersion != 3 || transition.State.StepNumber != 1 || transition.State.Status != agentruntime.RunWaitingInput {
		t.Fatalf("saved transition = %#v, replayed = %v", transition, replayed)
	}
	wantAnswer := agentruntime.ClarificationAnswer{
		QuestionID: "style", SelectedOptionIDs: []string{"luxury", "family"}, CustomText: "都市夜景",
	}
	if transition.State.PendingClarification == nil || !reflect.DeepEqual(transition.State.PendingClarification.Answers, []agentruntime.ClarificationAnswer{wantAnswer}) {
		t.Fatalf("saved answers = %#v", transition.State.PendingClarification)
	}
	if !reflect.DeepEqual(transition.EventKinds, []agentruntime.EventKind{agentruntime.EventClarificationAnswerSaved}) {
		t.Fatalf("events = %#v", transition.EventKinds)
	}

	replayedTransition, replayed, err := agentruntime.ApplyClarificationResponse(transition.State, agentruntime.ClarificationResponseSubmission{
		RequestID: "clarify-1", ExpectedStateVersion: 2, QuestionID: "style",
		Answer: agentruntime.ClarificationAnswerInput{
			SelectedOptionIDs: []string{"luxury", "family"}, CustomText: "都市夜景",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !replayed || !reflect.DeepEqual(replayedTransition.State, transition.State) || len(replayedTransition.EventKinds) != 0 {
		t.Fatalf("idempotent replay = %#v, replayed = %v", replayedTransition, replayed)
	}
}

func TestClarificationAnswerRequiresCurrentVersionForChangedFacts(t *testing.T) {
	current := enterClarification(t)
	first, _, err := agentruntime.ApplyClarificationResponse(current, agentruntime.ClarificationResponseSubmission{
		RequestID: "clarify-1", ExpectedStateVersion: 2, QuestionID: "style",
		Answer: agentruntime.ClarificationAnswerInput{SelectedOptionIDs: []string{"luxury"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = agentruntime.ApplyClarificationResponse(first.State, agentruntime.ClarificationResponseSubmission{
		RequestID: "clarify-1", ExpectedStateVersion: 2, QuestionID: "style",
		Answer: agentruntime.ClarificationAnswerInput{SelectedOptionIDs: []string{"family"}},
	})
	if !errors.Is(err, agentruntime.ErrClarificationVersionConflict) {
		t.Fatalf("changed stale answer error = %v", err)
	}
}

func TestClarificationResponseValidatesQuestionContract(t *testing.T) {
	current := enterClarification(t)
	tests := []agentruntime.ClarificationResponseSubmission{
		{
			RequestID: "clarify-1", ExpectedStateVersion: 2, QuestionID: "duration",
			Answer: agentruntime.ClarificationAnswerInput{SelectedOptionIDs: []string{"15s", "30s"}},
		},
		{
			RequestID: "clarify-1", ExpectedStateVersion: 2, QuestionID: "duration",
			Answer: agentruntime.ClarificationAnswerInput{SelectedOptionIDs: []string{"unknown"}},
		},
		{
			RequestID: "clarify-1", ExpectedStateVersion: 2, QuestionID: "duration",
			Answer: agentruntime.ClarificationAnswerInput{CustomText: "45 秒"},
		},
		{
			RequestID: "clarify-1", ExpectedStateVersion: 2, QuestionID: "style",
			Answer: agentruntime.ClarificationAnswerInput{SelectedOptionIDs: []string{"luxury", "luxury"}},
		},
	}
	for _, submission := range tests {
		if _, _, err := agentruntime.ApplyClarificationResponse(current, submission); !errors.Is(err, agentruntime.ErrClarificationAnswerInvalid) {
			t.Fatalf("invalid answer accepted: %#v, err = %v", submission, err)
		}
	}
}

func TestClarificationFreeTextAndSkippedAnswersRemainExplicitFacts(t *testing.T) {
	base := clarificationBase()
	decision := clarificationDecision("clarify-text")
	decision.Clarification.Questions = []agentruntime.ClarificationQuestion{
		{ID: "brand", Prompt: "品牌名称是什么？", Type: agentruntime.ClarificationFreeText},
		{
			ID: "duration", Prompt: "广告时长是多少？", Type: agentruntime.ClarificationSingleChoice,
			Options: []agentruntime.ClarificationOption{{ID: "15s", Label: "15 秒"}, {ID: "30s", Label: "30 秒"}},
		},
	}
	waiting, err := agentruntime.Advance(base, agentruntime.RuntimeInput{Decision: decision})
	if err != nil {
		t.Fatal(err)
	}
	brand, _, err := agentruntime.ApplyClarificationResponse(waiting.State, agentruntime.ClarificationResponseSubmission{
		RequestID: "clarify-text", ExpectedStateVersion: waiting.State.StateVersion, QuestionID: "brand",
		Answer: agentruntime.ClarificationAnswerInput{CustomText: "  橙光  "},
	})
	if err != nil {
		t.Fatal(err)
	}
	completed, _, err := agentruntime.ApplyClarificationResponse(brand.State, agentruntime.ClarificationResponseSubmission{
		RequestID: "clarify-text", ExpectedStateVersion: brand.State.StateVersion, QuestionID: "duration",
		Answer: agentruntime.ClarificationAnswerInput{Skipped: true}, Complete: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	answers := completed.State.ClarificationHistory[0].Answers
	if answers[0].CustomText != "橙光" || answers[0].Skipped || !answers[1].Skipped || answers[1].CustomText != "" || len(answers[1].SelectedOptionIDs) != 0 {
		t.Fatalf("completed answers = %#v", answers)
	}
}

func TestCompleteClarificationRequiresAllAnswersAndResumesRun(t *testing.T) {
	current := enterClarification(t)
	style, _, err := agentruntime.ApplyClarificationResponse(current, agentruntime.ClarificationResponseSubmission{
		RequestID: "clarify-1", ExpectedStateVersion: 2, QuestionID: "style",
		Answer: agentruntime.ClarificationAnswerInput{SelectedOptionIDs: []string{"performance"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := agentruntime.ApplyClarificationResponse(style.State, agentruntime.ClarificationResponseSubmission{
		RequestID: "clarify-1", ExpectedStateVersion: 3, QuestionID: "style",
		Answer: agentruntime.ClarificationAnswerInput{SelectedOptionIDs: []string{"performance"}}, Complete: true,
	}); !errors.Is(err, agentruntime.ErrClarificationIncomplete) {
		t.Fatalf("incomplete response error = %v", err)
	}

	completed, replayed, err := agentruntime.ApplyClarificationResponse(style.State, agentruntime.ClarificationResponseSubmission{
		RequestID: "clarify-1", ExpectedStateVersion: 3, QuestionID: "duration",
		Answer: agentruntime.ClarificationAnswerInput{SelectedOptionIDs: []string{"30s"}}, Complete: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if replayed || completed.State.Status != agentruntime.RunRunning || completed.State.PendingClarification != nil || completed.State.StateVersion != 4 || completed.State.StepNumber != 1 {
		t.Fatalf("completed transition = %#v, replayed = %v", completed, replayed)
	}
	if len(completed.State.ClarificationHistory) != 1 || completed.State.ClarificationHistory[0].Request.RequestID != "clarify-1" || len(completed.State.ClarificationHistory[0].Answers) != 2 {
		t.Fatalf("history = %#v", completed.State.ClarificationHistory)
	}
	wantEvents := []agentruntime.EventKind{
		agentruntime.EventClarificationAnswerSaved,
		agentruntime.EventClarificationResponded,
		agentruntime.EventRunStatusChanged,
	}
	if !reflect.DeepEqual(completed.EventKinds, wantEvents) {
		t.Fatalf("events = %#v, want %#v", completed.EventKinds, wantEvents)
	}

	replay, replayed, err := agentruntime.ApplyClarificationResponse(completed.State, agentruntime.ClarificationResponseSubmission{
		RequestID: "clarify-1", ExpectedStateVersion: 3, QuestionID: "duration",
		Answer: agentruntime.ClarificationAnswerInput{SelectedOptionIDs: []string{"30s"}}, Complete: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !replayed || !reflect.DeepEqual(replay.State, completed.State) || len(replay.EventKinds) != 0 {
		t.Fatalf("completed replay = %#v, replayed = %v", replay, replayed)
	}
}

func TestCompletedClarificationRejectsConflictingReplay(t *testing.T) {
	current := enterClarification(t)
	style, _, err := agentruntime.ApplyClarificationResponse(current, agentruntime.ClarificationResponseSubmission{
		RequestID: "clarify-1", ExpectedStateVersion: 2, QuestionID: "style",
		Answer: agentruntime.ClarificationAnswerInput{SelectedOptionIDs: []string{"performance"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	completed, _, err := agentruntime.ApplyClarificationResponse(style.State, agentruntime.ClarificationResponseSubmission{
		RequestID: "clarify-1", ExpectedStateVersion: 3, QuestionID: "duration",
		Answer: agentruntime.ClarificationAnswerInput{SelectedOptionIDs: []string{"30s"}}, Complete: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = agentruntime.ApplyClarificationResponse(completed.State, agentruntime.ClarificationResponseSubmission{
		RequestID: "clarify-1", ExpectedStateVersion: 4, QuestionID: "duration",
		Answer: agentruntime.ClarificationAnswerInput{SelectedOptionIDs: []string{"15s"}}, Complete: true,
	})
	if !errors.Is(err, agentruntime.ErrClarificationConflict) {
		t.Fatalf("conflicting completed replay error = %v", err)
	}
}

func TestCompletedClarificationOnlyReplaysTheExactCompletionSubmission(t *testing.T) {
	current := enterClarification(t)
	style, _, err := agentruntime.ApplyClarificationResponse(current, agentruntime.ClarificationResponseSubmission{
		RequestID: "clarify-1", ExpectedStateVersion: 2, QuestionID: "style",
		Answer: agentruntime.ClarificationAnswerInput{SelectedOptionIDs: []string{"performance"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	completed, _, err := agentruntime.ApplyClarificationResponse(style.State, agentruntime.ClarificationResponseSubmission{
		RequestID: "clarify-1", ExpectedStateVersion: 3, QuestionID: "duration",
		Answer: agentruntime.ClarificationAnswerInput{SelectedOptionIDs: []string{"30s"}}, Complete: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []agentruntime.ClarificationResponseSubmission{
		{
			RequestID: "clarify-1", ExpectedStateVersion: 2, QuestionID: "duration",
			Answer: agentruntime.ClarificationAnswerInput{SelectedOptionIDs: []string{"30s"}}, Complete: true,
		},
		{
			RequestID: "clarify-1", ExpectedStateVersion: 3, QuestionID: "style",
			Answer: agentruntime.ClarificationAnswerInput{SelectedOptionIDs: []string{"performance"}}, Complete: true,
		},
	}
	for _, submission := range tests {
		if _, replayed, err := agentruntime.ApplyClarificationResponse(completed.State, submission); err == nil || replayed || !errors.Is(err, agentruntime.ErrClarificationConflict) {
			t.Fatalf("non-exact completion replay accepted: submission=%#v replayed=%v err=%v", submission, replayed, err)
		}
	}
}

func TestClarificationRequestIdentityCannotBeReused(t *testing.T) {
	current := clarificationBase()
	current.StateVersion = 4
	current.StepNumber = 2
	current.ExpectedDelivery = func() *agentruntime.ExpectedDelivery {
		delivery := clarificationDelivery()
		return &delivery
	}()
	current.ClarificationHistory = []agentruntime.CompletedClarification{{
		Request: *clarificationDecision("clarify-1").Clarification,
		Answers: []agentruntime.ClarificationAnswer{
			{QuestionID: "style", SelectedOptionIDs: []string{"luxury"}},
			{QuestionID: "duration", SelectedOptionIDs: []string{"30s"}},
		},
		CompletionQuestionID: "duration", CompletionExpectedStateVersion: 3,
	}}
	transition, err := agentruntime.Advance(current, agentruntime.RuntimeInput{Decision: clarificationDecision("clarify-1")})
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.Status != agentruntime.RunRunning || transition.State.DecisionFeedback == nil || transition.State.DecisionFeedback.Code != "clarification_identity_reused" {
		t.Fatalf("reused clarification transition = %#v", transition)
	}
	wantEvents := []agentruntime.EventKind{agentruntime.EventModelRejected, agentruntime.EventRunStatusChanged}
	if !reflect.DeepEqual(transition.EventKinds, wantEvents) {
		t.Fatalf("events = %#v", transition.EventKinds)
	}
}

func TestClarificationCannotStartOnFinalModelStep(t *testing.T) {
	current := clarificationBase()
	current.StepNumber = 5
	transition, err := agentruntime.Advance(current, agentruntime.RuntimeInput{Decision: clarificationDecision("clarify-last")})
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.Status != agentruntime.RunFailed || transition.State.FailureCode != "step_budget_exhausted" || transition.State.PendingClarification != nil {
		t.Fatalf("final-step clarification = %#v", transition.State)
	}
}

func TestTerminateWaitingInputClearsPendingClarification(t *testing.T) {
	current := enterClarification(t)
	transition, err := agentruntime.Terminate(current, "scope_access_revoked")
	if err != nil {
		t.Fatal(err)
	}
	if transition.State.Status != agentruntime.RunFailed || transition.State.PendingClarification != nil {
		t.Fatalf("terminated clarification = %#v", transition.State)
	}
}
