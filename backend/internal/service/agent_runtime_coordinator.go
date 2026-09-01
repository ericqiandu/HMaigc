package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

const agentRuntimeAdvanceTransitionLimit = 4
const agentRuntimeStaleAfter = time.Minute

type agentRunWakeup string

const (
	agentWakeRunStarted             agentRunWakeup = "run_started"
	agentWakeModelTaskFinished      agentRunWakeup = "model_task_finished"
	agentWakeApprovalDecided        agentRunWakeup = "approval_decided"
	agentWakeClarificationAnswered  agentRunWakeup = "clarification_answered"
	agentWakeGenerationTaskFinished agentRunWakeup = "generation_task_finished"
	agentWakeStaleRecovery          agentRunWakeup = "stale_recovery"
)

func (s *Service) advanceAgentRun(scope agentruntime.Scope, wakeup agentRunWakeup) (*AgentRuntimeProgress, error) {
	if err := validateAgentRunWakeup(wakeup); err != nil {
		return nil, err
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if !scope.CanMutateCanvas() {
		return nil, Forbidden("当前用户没有继续执行 Agent 的画布权限")
	}
	for transitionCount := 0; transitionCount < agentRuntimeAdvanceTransitionLimit; transitionCount++ {
		view, err := s.readAgentRuntimeStateView(scope)
		if err != nil {
			return nil, err
		}
		previousVersion := view.State.StateVersion
		expiration, expired, expirationErr := agentruntime.ExpireRuntimeAt(view.State, time.Now().UTC())
		if expirationErr != nil {
			return nil, expirationErr
		}
		if expired {
			return s.commitAgentRuntimeState(scope, view.State, expiration)
		}
		switch view.State.Status {
		case agentruntime.RunQueued, agentruntime.RunRunning:
			progress, stepErr := s.resumeAgentRuntimeStep(scope)
			if stepErr != nil {
				return s.handleAgentRunAdvanceError(scope, view.State, stepErr)
			}
			if progress.ModelTask != nil || agentRuntimeRunTerminal(progress.State.Status) || progress.State.StateVersion == previousVersion {
				return progress, nil
			}
		case agentruntime.RunWaitingTool:
			if view.State.PendingToolCall == nil {
				return nil, errors.New("agent runtime pending tool facts are missing")
			}
			_, found := agentruntime.ToolPolicyForSchema(view.State.PendingToolCall.ToolName, view.Run.ToolSchemaVersion)
			if !found {
				return nil, errors.New("agent runtime tool policy is unavailable")
			}
			progress, stepErr := s.coordinatePendingAgentTool(scope, CoordinateAgentToolInput{
				ToolCallID: view.State.PendingToolCall.ToolCallID, ActionVersion: view.State.PendingToolCall.ActionVersion,
			})
			if stepErr != nil {
				return s.handleAgentRunAdvanceError(scope, view.State, stepErr)
			}
			if progress.State.Status == agentruntime.RunWaitingTool && progress.State.PendingToolStarted {
				return progress, nil
			}
			if agentRuntimeRunTerminal(progress.State.Status) || progress.State.StateVersion == previousVersion {
				return progress, nil
			}
		case agentruntime.RunWaitingInput, agentruntime.RunWaitingApproval, agentruntime.RunSucceeded, agentruntime.RunFailed, agentruntime.RunCancelled:
			return s.agentRuntimeProgressForCurrentState(scope, view.State)
		default:
			return nil, errors.New("agent runtime status is invalid")
		}
	}
	return nil, errors.New("agent runtime advance transition limit exceeded")
}

func (s *Service) handleAgentRunAdvanceError(scope agentruntime.Scope, _ agentruntime.RuntimeState, err error) (*AgentRuntimeProgress, error) {
	failureCode := agentRuntimeNonRetryableAdvanceFailureCode(err)
	if failureCode == "" {
		return nil, err
	}
	state, loadErr := s.repo.LoadAgentCheckpoint(scope)
	if loadErr != nil {
		return nil, errors.Join(err, loadErr)
	}
	if agentRuntimeRunTerminal(state.Status) {
		return s.agentRuntimeProgressForCurrentState(scope, state)
	}
	transition, transitionErr := agentruntime.Terminate(state, failureCode)
	if transitionErr != nil {
		return nil, errors.Join(err, transitionErr)
	}
	return s.commitAgentRuntimeState(scope, state, transition)
}

func validateAgentRunWakeup(wakeup agentRunWakeup) error {
	switch wakeup {
	case agentWakeRunStarted, agentWakeModelTaskFinished, agentWakeApprovalDecided, agentWakeClarificationAnswered, agentWakeGenerationTaskFinished, agentWakeStaleRecovery:
		return nil
	default:
		return errors.New("agent runtime wakeup is invalid")
	}
}

func (s *Service) advanceAgentRunReference(reference repository.ActiveAgentRunReference, wakeup agentRunWakeup) error {
	return s.advanceAgentRunReferenceWithTaskFence(reference, "", wakeup)
}

func (s *Service) advanceAgentRunTaskReference(reference repository.ActiveAgentRunReference, taskID string, wakeup agentRunWakeup) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return errors.New("agent runtime task wakeup identity is invalid")
	}
	return s.advanceAgentRunReferenceWithTaskFence(reference, taskID, wakeup)
}

func (s *Service) advanceAgentRunReferenceWithTaskFence(reference repository.ActiveAgentRunReference, taskID string, wakeup agentRunWakeup) error {
	release, acquired, err := s.coordinator.acquire(context.Background(), "agent-run-advance:"+reference.RunID, 1, 30*time.Second)
	if err != nil {
		return err
	}
	if !acquired {
		return errors.New("agent runtime advance is already in progress")
	}
	defer release()
	scope, terminated, err := s.authorizeActiveAgentRun(reference)
	if err != nil || terminated {
		return err
	}
	if taskID != "" {
		state, loadErr := s.repo.LoadAgentCheckpoint(scope)
		if loadErr != nil {
			return loadErr
		}
		expectedTaskID, expectedErr := expectedAgentTaskID(state, reference.RunID, wakeup)
		if expectedErr != nil {
			return expectedErr
		}
		if expectedTaskID != taskID {
			return nil
		}
	}
	_, err = s.advanceAgentRun(scope, wakeup)
	return err
}

func (s *Service) RecoverStaleAgentRuns(now time.Time, limit int) error {
	if now.IsZero() {
		return errors.New("agent runtime recovery time is invalid")
	}
	s.agentRecoveryMu.Lock()
	defer s.agentRecoveryMu.Unlock()
	references, err := s.repo.StaleAgentRunsAfter(s.agentRecoveryCursor, now.UTC().Add(-agentRuntimeStaleAfter), limit)
	if err != nil {
		return err
	}
	if len(references) == 0 && s.agentRecoveryCursor != "" {
		s.agentRecoveryCursor = ""
		references, err = s.repo.StaleAgentRunsAfter("", now.UTC().Add(-agentRuntimeStaleAfter), limit)
		if err != nil {
			return err
		}
	}
	if len(references) > 0 {
		s.agentRecoveryCursor = references[len(references)-1].RunID
	}
	var recoveryErrors []error
	for _, reference := range references {
		recoverErr := s.advanceAgentRunReference(reference, agentWakeStaleRecovery)
		if recoverErr != nil {
			recoveryErrors = append(recoveryErrors, fmt.Errorf("recover agent run %s: %w", reference.RunID, recoverErr))
		}
	}
	return errors.Join(recoveryErrors...)
}

func (s *Service) authorizeActiveAgentRun(reference repository.ActiveAgentRunReference) (agentruntime.Scope, bool, error) {
	scope, err := s.scopeForAgentRun(&model.User{ID: reference.ActorUserID}, reference.RunID)
	if err == nil {
		return scope, false, nil
	}
	var authErr *AuthError
	if !errors.As(err, &authErr) || (authErr.Status != 403 && authErr.Status != 404) {
		return agentruntime.Scope{}, false, err
	}
	if err := s.terminateAgentRunAfterScopeRevocation(reference); err != nil {
		return agentruntime.Scope{}, false, err
	}
	return agentruntime.Scope{}, true, nil
}

func (s *Service) terminateAgentRunAfterScopeRevocation(reference repository.ActiveAgentRunReference) error {
	identity, err := s.repo.AgentRunIdentityForActor(reference.RunID, reference.ActorUserID)
	if err != nil {
		return err
	}
	scope := agentruntime.Scope{
		TenantKind: identity.Thread.TenantKind, TenantID: identity.Thread.TenantID,
		ActorUserID: identity.Run.ActorUserID, DomainProjectID: identity.Thread.DomainProjectID,
		CanvasID: identity.Thread.CanvasID, ThreadID: identity.Thread.ID, RunID: identity.Run.ID,
		Access: agentruntime.AccessGrant{Level: agentruntime.AccessViewer},
	}
	state, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return err
	}
	if agentRuntimeRunTerminal(state.Status) {
		return nil
	}
	transition, err := agentruntime.Terminate(state, "scope_access_revoked")
	if err != nil {
		return err
	}
	_, err = s.commitAgentRuntimeState(scope, state, transition)
	return err
}

func agentRuntimeNonRetryableAdvanceFailureCode(err error) string {
	switch {
	case errors.Is(err, repository.ErrInsufficientCredits):
		return "insufficient_credits"
	case errors.Is(err, repository.ErrTeamMemberCreditLimit):
		return "team_credit_limit_reached"
	default:
		return ""
	}
}

func agentRuntimeRunTerminal(status agentruntime.RunStatus) bool {
	return status == agentruntime.RunSucceeded || status == agentruntime.RunFailed || status == agentruntime.RunCancelled
}
