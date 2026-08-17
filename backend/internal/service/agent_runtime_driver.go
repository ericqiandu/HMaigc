package service

import (
	"errors"
	"fmt"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

const agentRuntimeDriveTransitionLimit = 4

func (s *Service) DriveAgentRuns(limit int) error {
	s.agentDriveMu.Lock()
	defer s.agentDriveMu.Unlock()
	references, err := s.repo.ActiveAgentRunsAfter(s.agentDriveCursor, limit)
	if err != nil {
		return err
	}
	if len(references) == 0 && s.agentDriveCursor != "" {
		s.agentDriveCursor = ""
		references, err = s.repo.ActiveAgentRunsAfter("", limit)
		if err != nil {
			return err
		}
	}
	if len(references) > 0 {
		s.agentDriveCursor = references[len(references)-1].RunID
	}
	var driveErrors []error
	for _, reference := range references {
		if driveErr := s.driveAgentRunReference(reference); driveErr != nil {
			driveErrors = append(driveErrors, fmt.Errorf("drive agent run %s: %w", reference.RunID, driveErr))
		}
	}
	return errors.Join(driveErrors...)
}

func (s *Service) driveAgentRunReference(reference repository.ActiveAgentRunReference) error {
	scope, terminated, err := s.authorizeActiveAgentRun(reference)
	if err != nil || terminated {
		return err
	}
	return s.driveAgentRun(scope)
}

func (s *Service) resumeAgentRunReference(reference repository.ActiveAgentRunReference) error {
	scope, terminated, err := s.authorizeActiveAgentRun(reference)
	if err != nil || terminated {
		return err
	}
	_, err = s.ResumeAgentRuntime(scope)
	return err
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

func (s *Service) driveAgentRun(scope agentruntime.Scope) error {
	for transition := 0; transition < agentRuntimeDriveTransitionLimit; transition++ {
		view, err := s.readAgentRuntimeView(scope)
		if err != nil {
			return err
		}
		previousVersion := view.State.StateVersion
		var progress *AgentRuntimeProgress
		switch view.State.Status {
		case agentruntime.RunQueued, agentruntime.RunRunning:
			progress, err = s.ResumeAgentRuntime(scope)
		case agentruntime.RunWaitingTool:
			if view.State.PendingToolCall == nil {
				return errors.New("agent runtime pending tool facts are missing")
			}
			policy, found := agentruntime.ToolPolicyFor(view.State.PendingToolCall.ToolName)
			if !found {
				return errors.New("agent runtime tool policy is unavailable")
			}
			if policy.Execution == agentruntime.ToolExecutionClientFact {
				return nil
			}
			progress, err = s.CoordinatePendingAgentTool(scope, CoordinateAgentToolInput{
				ToolCallID: view.State.PendingToolCall.ToolCallID, ActionVersion: view.State.PendingToolCall.ActionVersion,
			})
		default:
			return nil
		}
		if err != nil {
			failureCode := agentRuntimeNonRetryableDriveFailureCode(err)
			if failureCode != "" {
				transition, transitionErr := agentruntime.Terminate(view.State, failureCode)
				if transitionErr != nil {
					return errors.Join(err, transitionErr)
				}
				_, commitErr := s.commitAgentRuntimeState(scope, view.State, transition)
				return commitErr
			}
			return err
		}
		if progress == nil || progress.ModelTask != nil || agentRuntimeRunTerminal(progress.State.Status) || progress.State.StateVersion == previousVersion {
			return nil
		}
	}
	return errors.New("agent runtime drive transition limit exceeded")
}

func agentRuntimeNonRetryableDriveFailureCode(err error) string {
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
