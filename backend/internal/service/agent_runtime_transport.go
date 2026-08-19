package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

type StartScopedAgentRunInput struct {
	Context         context.Context
	ClientRequestID string
	UserMessage     string
	MaxSteps        int
	Configuration   AgentRuntimeConfigurationInput
}

type AgentRuntimeView struct {
	Run   model.AgentRun            `json:"run"`
	State agentruntime.RuntimeState `json:"state"`
}

type AgentRuntimeEventView struct {
	Sequence  int64                  `json:"sequence"`
	Kind      agentruntime.EventKind `json:"kind"`
	Payload   json.RawMessage        `json:"payload"`
	CreatedAt time.Time              `json:"createdAt"`
}

type AgentClarificationError struct {
	Status             int    `json:"-"`
	ErrorCode          string `json:"errorCode"`
	Message            string `json:"-"`
	LatestStateVersion int    `json:"latestStateVersion,omitempty"`
}

func (err *AgentClarificationError) Error() string {
	return err.Message
}

func (s *Service) CreateAgentThread(actor *model.User, canvasID string) (*model.AgentThread, error) {
	if actor == nil {
		return nil, Unauthorized("请先登录")
	}
	threadID := newID()
	scope, err := s.AuthorizeAgentScope(actor.ID, strings.TrimSpace(canvasID), threadID, "thread-creation-probe")
	if err != nil {
		return nil, err
	}
	if !scope.CanMutateCanvas() {
		return nil, Forbidden("当前用户没有创建 Agent 对话的画布权限")
	}
	return s.repo.CreateAgentThread(scope, time.Now().UTC())
}

func (s *Service) StartScopedAgentRun(actor *model.User, threadID string, input StartScopedAgentRunInput) (*AgentRuntimeView, error) {
	if actor == nil {
		return nil, Unauthorized("请先登录")
	}
	thread, err := s.agentThreadForActor(actor.ID, threadID)
	if err != nil {
		return nil, err
	}
	scope, err := s.scopeForAgentThread(actor.ID, thread, newID())
	if err != nil {
		return nil, err
	}
	progress, err := s.StartAgentRuntime(StartAgentRuntimeInput{
		Context: input.Context, Scope: scope, ClientRequestID: input.ClientRequestID,
		UserMessage: input.UserMessage, MaxSteps: input.MaxSteps, Configuration: input.Configuration,
	})
	if err != nil {
		return nil, err
	}
	return agentRuntimeView(progress), nil
}

func (s *Service) ReadScopedAgentRun(actor *model.User, runID string) (*AgentRuntimeView, error) {
	scope, err := s.scopeForAgentRun(actor, runID)
	if err != nil {
		return nil, err
	}
	return s.readAgentRuntimeView(scope)
}

func (s *Service) readAgentRuntimeView(scope agentruntime.Scope) (*AgentRuntimeView, error) {
	run, err := s.repo.AgentRunForScope(scope)
	if err != nil {
		return nil, err
	}
	state, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	return agentRuntimeViewFromFacts(*run, state)
}

func agentRuntimeViewFromFacts(run model.AgentRun, state agentruntime.RuntimeState) (*AgentRuntimeView, error) {
	if state.StateVersion != run.StateVersion || state.StepNumber != run.StepNumber || state.MaxSteps != run.MaxSteps || state.Status != run.Status {
		return nil, errors.New("agent checkpoint state is inconsistent")
	}
	return &AgentRuntimeView{Run: run, State: state}, nil
}

func (s *Service) ReadScopedAgentEvents(actor *model.User, runID string, afterSequence int64, limit int) ([]AgentRuntimeEventView, *AgentRuntimeView, error) {
	scope, err := s.scopeForAgentRun(actor, runID)
	if err != nil {
		return nil, nil, err
	}
	events, err := s.repo.AgentRunEventsAfter(scope, afterSequence, limit)
	if err != nil {
		return nil, nil, err
	}
	view, err := s.readAgentRuntimeView(scope)
	if err != nil {
		return nil, nil, err
	}
	result := make([]AgentRuntimeEventView, 0, len(events))
	for _, event := range events {
		payload := json.RawMessage(event.PayloadJSON)
		if !json.Valid(payload) {
			return nil, nil, errors.New("agent event payload facts are invalid")
		}
		result = append(result, AgentRuntimeEventView{
			Sequence: event.Sequence, Kind: event.Kind, Payload: append(json.RawMessage(nil), payload...), CreatedAt: event.CreatedAt,
		})
	}
	return result, view, nil
}

func (s *Service) SubmitScopedAgentApproval(actor *model.User, runID string, input AgentToolApprovalSubmission) (*AgentRuntimeView, error) {
	scope, err := s.scopeForAgentRun(actor, runID)
	if err != nil {
		return nil, err
	}
	progress, err := s.SubmitAgentToolApproval(scope, input)
	if err != nil {
		return nil, err
	}
	return agentRuntimeView(progress), nil
}

func (s *Service) SubmitScopedAgentClarificationResponse(actor *model.User, runID string, requestID string, submission agentruntime.ClarificationResponseSubmission) (*AgentRuntimeView, error) {
	scope, err := s.scopeForAgentRun(actor, runID)
	if err != nil {
		return nil, err
	}
	submission.RequestID = strings.TrimSpace(requestID)
	progress, err := s.SubmitAgentClarificationResponse(scope, submission)
	if err != nil {
		return nil, err
	}
	return agentRuntimeView(progress), nil
}

func (s *Service) SubmitAgentClarificationResponse(scope agentruntime.Scope, submission agentruntime.ClarificationResponseSubmission) (*AgentRuntimeProgress, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if !scope.CanMutateCanvas() {
		return nil, Forbidden("当前用户没有提交 Agent 追问回答的画布权限")
	}
	current, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	transition, replayed, err := agentruntime.ApplyClarificationResponse(current, submission)
	if err != nil {
		return nil, mapAgentClarificationError(err, current.StateVersion)
	}
	if replayed {
		return s.agentRuntimeProgressForCurrentState(scope, current)
	}
	if err := s.repo.CommitAgentRuntimeTransition(scope, current, transition, time.Now().UTC()); err != nil {
		if !errors.Is(err, repository.ErrAgentRuntimeStepConflict) {
			return nil, err
		}
		latest, loadErr := s.repo.LoadAgentCheckpoint(scope)
		if loadErr != nil {
			return nil, loadErr
		}
		_, replayed, replayErr := agentruntime.ApplyClarificationResponse(latest, submission)
		if replayErr != nil {
			return nil, mapAgentClarificationError(replayErr, latest.StateVersion)
		}
		if !replayed {
			return nil, clarificationConflict(latest.StateVersion)
		}
		return s.agentRuntimeProgressForCurrentState(scope, latest)
	}
	if submission.Complete {
		return s.advanceAgentRun(scope, agentWakeClarificationAnswered)
	}
	return s.agentRuntimeProgressForCurrentState(scope, transition.State)
}

func mapAgentClarificationError(err error, latestStateVersion int) error {
	switch {
	case errors.Is(err, agentruntime.ErrClarificationAnswerInvalid), errors.Is(err, agentruntime.ErrClarificationIncomplete):
		return &AgentClarificationError{
			Status: http.StatusBadRequest, ErrorCode: "agent_clarification_invalid",
			Message: "Agent 追问回答格式无效", LatestStateVersion: latestStateVersion,
		}
	case errors.Is(err, agentruntime.ErrClarificationIdentityReused):
		return &AgentClarificationError{
			Status: http.StatusConflict, ErrorCode: "agent_clarification_identity_reused",
			Message: "Agent 追问身份已完成且回答事实冲突", LatestStateVersion: latestStateVersion,
		}
	case errors.Is(err, agentruntime.ErrClarificationNotPending):
		return &AgentClarificationError{
			Status: http.StatusConflict, ErrorCode: "agent_clarification_not_pending",
			Message: "Agent 当前没有等待该追问回答", LatestStateVersion: latestStateVersion,
		}
	case errors.Is(err, agentruntime.ErrClarificationVersionConflict), errors.Is(err, agentruntime.ErrClarificationConflict):
		return clarificationConflict(latestStateVersion)
	default:
		return err
	}
}

func clarificationConflict(latestStateVersion int) *AgentClarificationError {
	return &AgentClarificationError{
		Status: http.StatusConflict, ErrorCode: "agent_clarification_conflict",
		Message: "Agent 追问状态已经变化，请按最新状态重试", LatestStateVersion: latestStateVersion,
	}
}

func (s *Service) agentThreadForActor(actorUserID string, threadID string) (*model.AgentThread, error) {
	thread, err := s.repo.AgentThreadForActor(strings.TrimSpace(threadID), strings.TrimSpace(actorUserID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NotFound("Agent 对话不存在")
	}
	return thread, err
}

func (s *Service) scopeForAgentThread(actorUserID string, thread *model.AgentThread, runID string) (agentruntime.Scope, error) {
	if thread == nil || thread.Status != agentruntime.ThreadActive {
		return agentruntime.Scope{}, NotFound("Agent 对话不存在")
	}
	scope, err := s.AuthorizeAgentScope(actorUserID, thread.CanvasID, thread.ID, runID)
	if err != nil {
		return agentruntime.Scope{}, err
	}
	if scope.TenantKind != thread.TenantKind || scope.TenantID != thread.TenantID || scope.DomainProjectID != thread.DomainProjectID {
		return agentruntime.Scope{}, Forbidden("Agent 对话作用域已经变化")
	}
	return scope, nil
}

func (s *Service) scopeForAgentRun(actor *model.User, runID string) (agentruntime.Scope, error) {
	if actor == nil {
		return agentruntime.Scope{}, Unauthorized("请先登录")
	}
	identity, err := s.repo.AgentRunIdentityForActor(strings.TrimSpace(runID), actor.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return agentruntime.Scope{}, NotFound("Agent 运行不存在")
	}
	if err != nil {
		return agentruntime.Scope{}, err
	}
	scope, err := s.scopeForAgentThread(actor.ID, &identity.Thread, identity.Run.ID)
	if err != nil {
		return agentruntime.Scope{}, err
	}
	if identity.Run.ThreadID != scope.ThreadID || identity.Run.ActorUserID != scope.ActorUserID {
		return agentruntime.Scope{}, Forbidden("Agent 运行作用域冲突")
	}
	return scope, nil
}

func agentRuntimeView(progress *AgentRuntimeProgress) *AgentRuntimeView {
	if progress == nil {
		return nil
	}
	return &AgentRuntimeView{Run: progress.Run, State: progress.State}
}
