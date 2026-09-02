package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

type StartExternalAgentRunInput struct {
	Context          context.Context
	CanvasID         string
	ExternalThreadID string
	ClientRequestID  string
	UserMessage      string
	MaxSteps         int
	Configuration    AgentRuntimeConfigurationInput
}

type SubmitExternalAgentDecisionInput struct {
	ClientRequestID      string
	ExpectedStateVersion int
	Decision             agentruntime.ModelDecision
}

func (s *Service) StartExternalAgentRun(actor *model.User, input StartExternalAgentRunInput) (*AgentRuntimeView, error) {
	if actor == nil {
		return nil, Unauthorized("请先登录")
	}
	input.CanvasID = strings.TrimSpace(input.CanvasID)
	input.ExternalThreadID = strings.TrimSpace(input.ExternalThreadID)
	input.ClientRequestID = strings.TrimSpace(input.ClientRequestID)
	input.UserMessage = strings.TrimSpace(input.UserMessage)
	if input.CanvasID == "" || input.ExternalThreadID == "" || len(input.ExternalThreadID) > 120 ||
		input.ClientRequestID == "" || len(input.ClientRequestID) > 120 || input.UserMessage == "" ||
		len(input.UserMessage) > 64*1024 || input.MaxSteps < 1 || input.MaxSteps > agentRuntimeMaxSteps {
		return nil, BadAuthRequest("外部 Agent 请求事实无效")
	}

	candidateScope, err := s.AuthorizeAgentScope(actor.ID, input.CanvasID, newID(), newID())
	if err != nil {
		return nil, err
	}
	if !candidateScope.CanMutateCanvas() {
		return nil, Forbidden("当前用户没有执行 Agent 的画布权限")
	}
	configuration, err := s.resolveExternalAgentRuntimeConfiguration(input.Context, candidateScope, input.Configuration)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	initialized, err := s.repo.CreateInitializedExternalAgentRun(repository.CreateInitializedExternalAgentRunInput{
		Create: repository.CreateLocalAgentRunInput{
			Scope: candidateScope, ExternalThreadID: input.ExternalThreadID,
			ClientRequestID: input.ClientRequestID, Now: now,
		},
		Initialize: repository.InitializeAgentRunInput{
			Scope: candidateScope, MaxSteps: input.MaxSteps,
			ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
			RuntimeVersion:    agentruntime.CurrentRuntimeVersion,
			PolicyVersion:     agentruntime.CurrentPolicyVersion,
			UserMessage:       input.UserMessage,
			Configuration:     configuration,
			Limits: &agentruntime.RuntimeLimits{
				MaxToolCalls: agentRuntimeMaxToolCalls,
				StartedAt:    now,
				DeadlineAt:   now.Add(agentRuntimeMaxElapsed),
			},
			Now: now,
		},
	})
	if err != nil {
		return nil, err
	}
	if initialized.Run.ReasoningHost != agentruntime.ReasoningHostLocalCodex ||
		initialized.Run.ModelRecordID != "" || initialized.Run.ModelKey != "" {
		return nil, errors.New("external agent run reasoning source conflicts with local Codex")
	}
	scope, err := s.AuthorizeAgentScope(actor.ID, input.CanvasID, initialized.Run.ThreadID, initialized.Run.ID)
	if err != nil {
		return nil, err
	}
	if err := validateAgentRuntimeExecutionContract(initialized.Run); err != nil {
		return nil, err
	}
	state, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	if state.UserMessage != input.UserMessage || state.MaxSteps != input.MaxSteps ||
		!agentRuntimeConfigurationMatchesInput(state.Configuration, input.Configuration) {
		return nil, errors.New("external agent runtime request facts conflict")
	}
	if state.Status == agentruntime.RunQueued {
		transition, transitionErr := agentruntime.BeginModelRequest(state)
		if transitionErr != nil {
			return nil, transitionErr
		}
		progress, commitErr := s.commitAgentRuntimeState(scope, state, transition)
		if commitErr != nil {
			return nil, commitErr
		}
		return s.agentRuntimeViewForProgress(scope, progress)
	}
	switch state.Status {
	case agentruntime.RunRunning, agentruntime.RunSucceeded, agentruntime.RunFailed, agentruntime.RunCancelled,
		agentruntime.RunWaitingInput, agentruntime.RunWaitingApproval, agentruntime.RunWaitingTool:
		return s.agentRuntimeViewForProgress(scope, &AgentRuntimeProgress{Run: initialized.Run, State: state})
	default:
		return nil, errors.New("external agent runtime status is invalid")
	}
}

func (s *Service) SubmitExternalAgentDecision(actor *model.User, runID string, input SubmitExternalAgentDecisionInput) (view *AgentRuntimeView, resultErr error) {
	input.ClientRequestID = strings.TrimSpace(input.ClientRequestID)
	if input.ClientRequestID == "" || len(input.ClientRequestID) > 120 || input.ExpectedStateVersion < 1 {
		return nil, agentruntime.ErrExternalDecisionConflict
	}
	scope, err := s.scopeForAgentRun(actor, strings.TrimSpace(runID))
	if err != nil {
		return nil, err
	}
	defer func() {
		if !errors.Is(resultErr, agentruntime.ErrExternalDecisionConflict) {
			return
		}
		latest, loadErr := s.repo.LoadAgentCheckpoint(scope)
		if loadErr != nil {
			resultErr = errors.Join(resultErr, loadErr)
			return
		}
		resultErr = &AgentControlError{
			Status:             http.StatusConflict,
			ErrorCode:          "agent_external_decision_conflict",
			Message:            "Agent 本机决策状态已经变化，请按最新状态重试",
			LatestStateVersion: latest.StateVersion,
			Cause:              resultErr,
		}
	}()
	run, err := s.repo.AgentRunForScope(scope)
	if err != nil {
		return nil, err
	}
	if run.ReasoningHost != agentruntime.ReasoningHostLocalCodex || run.ModelRecordID != "" || run.ModelKey != "" {
		return nil, agentruntime.ErrExternalDecisionConflict
	}
	if err := validateAgentRuntimeExecutionContract(*run); err != nil {
		return nil, err
	}
	input.Decision, err = stampAgentCanvasPlaceholderProvenance(scope, input.Decision)
	if err != nil {
		return nil, err
	}
	requestHash, err := externalAgentDecisionRequestHash(input)
	if err != nil {
		return nil, agentruntime.ErrExternalDecisionConflict
	}
	receipt, err := s.repo.ExternalAgentDecisionForScope(scope, input.ClientRequestID)
	if err == nil {
		if receipt.RequestHash != requestHash || receipt.ExpectedStateVersion != input.ExpectedStateVersion {
			return nil, agentruntime.ErrExternalDecisionConflict
		}
		return s.externalAgentDecisionResult(scope, receipt.ResultStateVersion)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	state, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	if state.Status != agentruntime.RunRunning || state.StateVersion != input.ExpectedStateVersion {
		return nil, agentruntime.ErrExternalDecisionConflict
	}
	if input.Decision.ToolCall != nil {
		_, lookupErr := s.repo.AgentToolCallForScope(scope, input.Decision.ToolCall.ToolCallID, input.Decision.ToolCall.ActionVersion)
		if lookupErr == nil {
			transition, rejectErr := agentruntime.RejectReusedToolIdentity(state, *input.Decision.ToolCall)
			if rejectErr != nil {
				return nil, rejectErr
			}
			return s.commitAndCompleteExternalDecision(scope, input, requestHash, state, transition)
		}
		if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			return nil, lookupErr
		}
	}
	finalMessage := ""
	if input.Decision.Final != nil {
		finalMessage = input.Decision.Final.Message
	}
	evidence, err := s.agentRuntimeDeliveryEvidence(scope, finalMessage)
	if err != nil {
		return nil, err
	}
	transition, err := agentruntime.AdvanceExternalDecision(state, agentruntime.ExternalDecisionInput{
		ExpectedStateVersion: input.ExpectedStateVersion,
		Decision:             input.Decision,
		Evidence:             evidence,
	})
	if err != nil {
		return nil, err
	}
	transition, err = s.prepareAgentRuntimeApproval(scope, state, transition, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return s.commitAndCompleteExternalDecision(scope, input, requestHash, state, transition)
}

func (s *Service) commitAndCompleteExternalDecision(
	scope agentruntime.Scope,
	input SubmitExternalAgentDecisionInput,
	requestHash string,
	previous agentruntime.RuntimeState,
	transition agentruntime.RuntimeTransition,
) (*AgentRuntimeView, error) {
	_, err := s.repo.CommitExternalAgentDecision(repository.CommitExternalAgentDecisionInput{
		Scope: scope, ClientRequestID: input.ClientRequestID, RequestHash: requestHash,
		ExpectedStateVersion: input.ExpectedStateVersion, Previous: previous, Transition: transition, Now: time.Now().UTC(),
	})
	if err != nil {
		if errors.Is(err, repository.ErrAgentRuntimeStepConflict) {
			return nil, agentruntime.ErrExternalDecisionConflict
		}
		return nil, err
	}
	return s.externalAgentDecisionResult(scope, transition.State.StateVersion)
}

func (s *Service) externalAgentDecisionResult(scope agentruntime.Scope, decisionStateVersion int) (*AgentRuntimeView, error) {
	state, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	progress, err := s.agentRuntimeProgressForCurrentState(scope, state)
	if err != nil {
		return nil, err
	}
	if progress.State.StateVersion == decisionStateVersion && progress.State.Status == agentruntime.RunWaitingTool && progress.State.PendingToolCall != nil {
		progress, err = s.coordinatePendingAgentTool(scope, CoordinateAgentToolInput{
			ToolCallID: progress.State.PendingToolCall.ToolCallID, ActionVersion: progress.State.PendingToolCall.ActionVersion,
		})
		if err != nil {
			return nil, err
		}
	}
	return s.agentRuntimeViewForProgress(scope, progress)
}

func externalAgentDecisionRequestHash(input SubmitExternalAgentDecisionInput) (string, error) {
	payload, err := json.Marshal(struct {
		ExpectedStateVersion int                        `json:"expectedStateVersion"`
		Decision             agentruntime.ModelDecision `json:"decision"`
	}{ExpectedStateVersion: input.ExpectedStateVersion, Decision: input.Decision})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}
