package service

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

type StartScopedAgentRunInput struct {
	ClientRequestID string
	UserMessage     string
	MaxSteps        int
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
		Scope: scope, ClientRequestID: input.ClientRequestID, UserMessage: input.UserMessage, MaxSteps: input.MaxSteps,
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
	return &AgentRuntimeView{Run: *run, State: state}, nil
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

func (s *Service) SubmitScopedAgentToolResult(actor *model.User, runID string, input CoordinateAgentToolInput) (*AgentRuntimeView, error) {
	scope, err := s.scopeForAgentRun(actor, runID)
	if err != nil {
		return nil, err
	}
	progress, err := s.CoordinatePendingAgentTool(scope, input)
	if err != nil {
		return nil, err
	}
	return agentRuntimeView(progress), nil
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
