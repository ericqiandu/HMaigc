package agentruntime

import (
	"errors"
	"slices"
	"strings"
	"unicode/utf8"
)

var (
	ErrClarificationAnswerInvalid   = errors.New("agent clarification answer is invalid")
	ErrClarificationConflict        = errors.New("agent clarification facts conflict")
	ErrClarificationIdentityReused  = errors.New("agent clarification identity is reused")
	ErrClarificationIncomplete      = errors.New("agent clarification response is incomplete")
	ErrClarificationNotPending      = errors.New("agent clarification is not pending")
	ErrClarificationVersionConflict = errors.New("agent clarification state version conflicts")
)

const clarificationCustomTextLimit = 1000

type ClarificationAnswerInput struct {
	SelectedOptionIDs []string `json:"selectedOptionIds,omitempty"`
	CustomText        string   `json:"customText,omitempty"`
	Skipped           bool     `json:"skipped"`
}

type ClarificationAnswer struct {
	QuestionID        string   `json:"questionId"`
	SelectedOptionIDs []string `json:"selectedOptionIds,omitempty"`
	CustomText        string   `json:"customText,omitempty"`
	Skipped           bool     `json:"skipped"`
}

type PendingClarification struct {
	Request ClarificationDecision `json:"request"`
	Answers []ClarificationAnswer `json:"answers"`
}

type CompletedClarification struct {
	Request                        ClarificationDecision `json:"request"`
	Answers                        []ClarificationAnswer `json:"answers"`
	CompletionQuestionID           string                `json:"completionQuestionId"`
	CompletionExpectedStateVersion int                   `json:"completionExpectedStateVersion"`
}

type ClarificationResponseSubmission struct {
	RequestID            string                   `json:"-"`
	ExpectedStateVersion int                      `json:"expectedStateVersion"`
	QuestionID           string                   `json:"questionId"`
	Answer               ClarificationAnswerInput `json:"answer"`
	Complete             bool                     `json:"complete"`
}

func ApplyClarificationResponse(current RuntimeState, submission ClarificationResponseSubmission) (RuntimeTransition, bool, error) {
	if err := validateRuntimeState(current); err != nil {
		return RuntimeTransition{}, false, err
	}
	submission.RequestID = strings.TrimSpace(submission.RequestID)
	submission.QuestionID = strings.TrimSpace(submission.QuestionID)
	if !boundedDecisionText(submission.RequestID, 120) || !boundedDecisionText(submission.QuestionID, 120) || submission.ExpectedStateVersion < 1 {
		return RuntimeTransition{}, false, ErrClarificationAnswerInvalid
	}

	if completed, found := completedClarificationByRequestID(current.ClarificationHistory, submission.RequestID); found {
		answer, err := canonicalClarificationAnswer(completed.Request, submission.QuestionID, submission.Answer)
		if err != nil {
			return RuntimeTransition{}, false, err
		}
		stored, exists := clarificationAnswerByQuestionID(completed.Answers, submission.QuestionID)
		if submission.Complete &&
			submission.QuestionID == completed.CompletionQuestionID &&
			submission.ExpectedStateVersion == completed.CompletionExpectedStateVersion &&
			exists && clarificationAnswersEqual(stored, answer) {
			return RuntimeTransition{State: current}, true, nil
		}
		return RuntimeTransition{}, false, errors.Join(ErrClarificationConflict, ErrClarificationIdentityReused)
	}

	if current.Status != RunWaitingInput || current.PendingClarification == nil {
		return RuntimeTransition{}, false, errors.Join(ErrClarificationConflict, ErrClarificationNotPending)
	}
	pending := current.PendingClarification
	if pending.Request.RequestID != submission.RequestID {
		return RuntimeTransition{}, false, ErrClarificationConflict
	}
	answer, err := canonicalClarificationAnswer(pending.Request, submission.QuestionID, submission.Answer)
	if err != nil {
		return RuntimeTransition{}, false, err
	}
	stored, exists := clarificationAnswerByQuestionID(pending.Answers, submission.QuestionID)
	answerChanged := !exists || !clarificationAnswersEqual(stored, answer)
	if !submission.Complete && !answerChanged {
		return RuntimeTransition{State: current}, true, nil
	}
	if submission.ExpectedStateVersion != current.StateVersion {
		return RuntimeTransition{}, false, ErrClarificationVersionConflict
	}

	answers := upsertClarificationAnswer(pending.Request, pending.Answers, answer)
	if submission.Complete && !clarificationAnswersComplete(pending.Request, answers) {
		return RuntimeTransition{}, false, ErrClarificationIncomplete
	}

	next := current
	next.StateVersion++
	next.PendingClarification = &PendingClarification{Request: cloneClarificationDecision(pending.Request), Answers: answers}
	events := make([]EventKind, 0, 3)
	if answerChanged {
		events = append(events, EventClarificationAnswerSaved)
	}
	if !submission.Complete {
		return RuntimeTransition{State: next, EventKinds: events}, false, nil
	}
	next.Status = RunRunning
	next.ClarificationHistory = appendCompletedClarification(next.ClarificationHistory, CompletedClarification{
		Request:                        pending.Request,
		Answers:                        answers,
		CompletionQuestionID:           submission.QuestionID,
		CompletionExpectedStateVersion: submission.ExpectedStateVersion,
	})
	next.PendingClarification = nil
	events = append(events, EventClarificationResponded, EventRunStatusChanged)
	return RuntimeTransition{State: next, EventKinds: events}, false, nil
}

func canonicalClarificationAnswer(request ClarificationDecision, questionID string, input ClarificationAnswerInput) (ClarificationAnswer, error) {
	question, found := clarificationQuestionByID(request.Questions, questionID)
	if !found {
		return ClarificationAnswer{}, ErrClarificationAnswerInvalid
	}
	customText := strings.TrimSpace(input.CustomText)
	if !utf8.ValidString(customText) || utf8.RuneCountInString(customText) > clarificationCustomTextLimit {
		return ClarificationAnswer{}, ErrClarificationAnswerInvalid
	}
	if input.Skipped {
		if len(input.SelectedOptionIDs) != 0 || customText != "" {
			return ClarificationAnswer{}, ErrClarificationAnswerInvalid
		}
		return ClarificationAnswer{QuestionID: question.ID, Skipped: true}, nil
	}

	answer := ClarificationAnswer{QuestionID: question.ID, CustomText: customText}
	switch question.Type {
	case ClarificationFreeText:
		if len(input.SelectedOptionIDs) != 0 || customText == "" {
			return ClarificationAnswer{}, ErrClarificationAnswerInvalid
		}
	case ClarificationSingleChoice, ClarificationMultiChoice:
		selected, err := canonicalSelectedOptions(question, input.SelectedOptionIDs)
		if err != nil {
			return ClarificationAnswer{}, err
		}
		if question.Type == ClarificationSingleChoice && len(selected) > 1 {
			return ClarificationAnswer{}, ErrClarificationAnswerInvalid
		}
		if customText != "" && !question.AllowCustomAnswer {
			return ClarificationAnswer{}, ErrClarificationAnswerInvalid
		}
		if len(selected) == 0 && customText == "" {
			return ClarificationAnswer{}, ErrClarificationAnswerInvalid
		}
		answer.SelectedOptionIDs = selected
	default:
		return ClarificationAnswer{}, ErrClarificationAnswerInvalid
	}
	return answer, nil
}

func canonicalSelectedOptions(question ClarificationQuestion, selectedIDs []string) ([]string, error) {
	selected := make(map[string]struct{}, len(selectedIDs))
	for _, rawID := range selectedIDs {
		id := strings.TrimSpace(rawID)
		if id == "" {
			return nil, ErrClarificationAnswerInvalid
		}
		if _, duplicated := selected[id]; duplicated {
			return nil, ErrClarificationAnswerInvalid
		}
		selected[id] = struct{}{}
	}
	ordered := make([]string, 0, len(selected))
	for _, option := range question.Options {
		if _, exists := selected[option.ID]; exists {
			ordered = append(ordered, option.ID)
			delete(selected, option.ID)
		}
	}
	if len(selected) != 0 {
		return nil, ErrClarificationAnswerInvalid
	}
	return ordered, nil
}

func validateClarificationRecord(request ClarificationDecision, answers []ClarificationAnswer, requireComplete bool) error {
	requestCopy := cloneClarificationDecision(request)
	if err := requestCopy.Validate(); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(answers))
	for _, stored := range answers {
		if _, duplicated := seen[stored.QuestionID]; duplicated {
			return ErrClarificationAnswerInvalid
		}
		seen[stored.QuestionID] = struct{}{}
		input := ClarificationAnswerInput{
			SelectedOptionIDs: append([]string(nil), stored.SelectedOptionIDs...),
			CustomText:        stored.CustomText,
			Skipped:           stored.Skipped,
		}
		canonical, err := canonicalClarificationAnswer(requestCopy, stored.QuestionID, input)
		if err != nil || !clarificationAnswersEqual(canonical, stored) {
			return ErrClarificationAnswerInvalid
		}
	}
	if requireComplete && !clarificationAnswersComplete(requestCopy, answers) {
		return ErrClarificationIncomplete
	}
	return nil
}

func validateCompletedClarification(completed CompletedClarification) error {
	if err := validateClarificationRecord(completed.Request, completed.Answers, true); err != nil {
		return err
	}
	if !boundedDecisionText(completed.CompletionQuestionID, 120) || completed.CompletionExpectedStateVersion < 1 {
		return ErrClarificationAnswerInvalid
	}
	if _, exists := clarificationAnswerByQuestionID(completed.Answers, completed.CompletionQuestionID); !exists {
		return ErrClarificationAnswerInvalid
	}
	return nil
}

func clarificationQuestionByID(questions []ClarificationQuestion, questionID string) (ClarificationQuestion, bool) {
	for _, question := range questions {
		if question.ID == questionID {
			return question, true
		}
	}
	return ClarificationQuestion{}, false
}

func clarificationAnswerByQuestionID(answers []ClarificationAnswer, questionID string) (ClarificationAnswer, bool) {
	for _, answer := range answers {
		if answer.QuestionID == questionID {
			return answer, true
		}
	}
	return ClarificationAnswer{}, false
}

func clarificationAnswersEqual(left ClarificationAnswer, right ClarificationAnswer) bool {
	return left.QuestionID == right.QuestionID &&
		left.CustomText == right.CustomText &&
		left.Skipped == right.Skipped &&
		slices.Equal(left.SelectedOptionIDs, right.SelectedOptionIDs)
}

func clarificationAnswersComplete(request ClarificationDecision, answers []ClarificationAnswer) bool {
	if len(answers) != len(request.Questions) {
		return false
	}
	for _, question := range request.Questions {
		if _, exists := clarificationAnswerByQuestionID(answers, question.ID); !exists {
			return false
		}
	}
	return true
}

func upsertClarificationAnswer(request ClarificationDecision, answers []ClarificationAnswer, answer ClarificationAnswer) []ClarificationAnswer {
	byQuestion := make(map[string]ClarificationAnswer, len(answers)+1)
	for _, existing := range answers {
		byQuestion[existing.QuestionID] = cloneClarificationAnswer(existing)
	}
	byQuestion[answer.QuestionID] = cloneClarificationAnswer(answer)
	ordered := make([]ClarificationAnswer, 0, len(byQuestion))
	for _, question := range request.Questions {
		if stored, exists := byQuestion[question.ID]; exists {
			ordered = append(ordered, stored)
		}
	}
	return ordered
}

func appendCompletedClarification(history []CompletedClarification, completed CompletedClarification) []CompletedClarification {
	next := make([]CompletedClarification, len(history), len(history)+1)
	for index, existing := range history {
		next[index] = cloneCompletedClarification(existing)
	}
	return append(next, cloneCompletedClarification(completed))
}

func completedClarificationByRequestID(history []CompletedClarification, requestID string) (CompletedClarification, bool) {
	for _, completed := range history {
		if completed.Request.RequestID == requestID {
			return completed, true
		}
	}
	return CompletedClarification{}, false
}

func cloneClarificationDecision(decision ClarificationDecision) ClarificationDecision {
	clone := decision
	clone.Questions = make([]ClarificationQuestion, len(decision.Questions))
	for index, question := range decision.Questions {
		clone.Questions[index] = question
		clone.Questions[index].Options = append([]ClarificationOption(nil), question.Options...)
	}
	clone.ExpectedDelivery.RequiredArtifacts = append([]ArtifactKind(nil), decision.ExpectedDelivery.RequiredArtifacts...)
	clone.ExpectedDelivery.CompletionCriteria = append([]DeliveryCriterion(nil), decision.ExpectedDelivery.CompletionCriteria...)
	return clone
}

func cloneClarificationAnswer(answer ClarificationAnswer) ClarificationAnswer {
	clone := answer
	clone.SelectedOptionIDs = append([]string(nil), answer.SelectedOptionIDs...)
	return clone
}

func cloneCompletedClarification(completed CompletedClarification) CompletedClarification {
	clone := CompletedClarification{
		Request:                        cloneClarificationDecision(completed.Request),
		CompletionQuestionID:           completed.CompletionQuestionID,
		CompletionExpectedStateVersion: completed.CompletionExpectedStateVersion,
	}
	clone.Answers = make([]ClarificationAnswer, len(completed.Answers))
	for index, answer := range completed.Answers {
		clone.Answers[index] = cloneClarificationAnswer(answer)
	}
	return clone
}
