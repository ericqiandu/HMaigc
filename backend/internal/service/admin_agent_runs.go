package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

type AdminAgentRunInterruptRequest struct {
	RunID                string `json:"-"`
	ExpectedStateVersion int    `json:"expectedStateVersion"`
	Reason               string `json:"reason"`
	Confirmation         string `json:"confirmation"`
}

type AdminAgentRunInterruptResponse struct {
	Run                   repository.AdminAgentRunRecord             `json:"run"`
	Disposition           repository.AdminAgentRunControlDisposition `json:"disposition"`
	AffectedTaskIDs       []string                                   `json:"affectedTaskIds"`
	ReconciliationPending bool                                       `json:"reconciliationPending"`
}

type AdminAgentRunControlError struct {
	Status    int                             `json:"-"`
	ErrorCode string                          `json:"errorCode"`
	Message   string                          `json:"-"`
	Latest    *repository.AdminAgentRunRecord `json:"latest,omitempty"`
}

func (e *AdminAgentRunControlError) Error() string {
	return e.Message
}

func (s *Service) AdminAgentRuns(ctx context.Context, actor *model.User, query repository.AdminAgentRunQuery) (*repository.AdminAgentRunPage, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	page, err := s.repo.AdminAgentRuns(query, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return &page, nil
}

func (s *Service) AdminAgentRun(ctx context.Context, actor *model.User, runID string) (*repository.AdminAgentRunRecord, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	record, err := s.repo.AdminAgentRun(strings.TrimSpace(runID), time.Now().UTC())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, adminAgentRunError(http.StatusNotFound, "admin_agent_run_not_found", "Agent 运行不存在", nil)
	}
	return record, err
}

func (s *Service) InterruptAdminAgentRun(ctx context.Context, actor *model.User, request AdminAgentRunInterruptRequest) (*AdminAgentRunInterruptResponse, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	request.RunID = strings.TrimSpace(request.RunID)
	request.Reason = strings.TrimSpace(request.Reason)
	request.Confirmation = strings.TrimSpace(request.Confirmation)
	if request.RunID == "" || request.ExpectedStateVersion < 1 || utf8.RuneCountInString(request.Reason) < 4 || utf8.RuneCountInString(request.Reason) > 500 {
		return nil, adminAgentRunError(http.StatusBadRequest, "admin_agent_run_interrupt_blocked", "终止原因必须为 4–500 个字符", nil)
	}
	current, err := s.repo.AdminAgentRun(request.RunID, time.Now().UTC())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, adminAgentRunError(http.StatusNotFound, "admin_agent_run_not_found", "Agent 运行不存在", nil)
	}
	if err != nil {
		return nil, err
	}
	if request.Confirmation != current.ConfirmationPhrase {
		return nil, adminAgentRunError(http.StatusBadRequest, "admin_agent_run_confirmation_invalid", "确认短语不正确", current)
	}
	if current.ControlDisposition == repository.AdminAgentRunAlreadyTerminal {
		return nil, adminAgentRunError(http.StatusConflict, "admin_agent_run_terminal", "Agent 运行已经结束", current)
	}
	if current.ControlDisposition == repository.AdminAgentRunBlockedByUnresolvedBilling {
		return nil, adminAgentRunError(http.StatusConflict, "admin_agent_run_billing_unresolved", "Agent 运行存在未决账务，需先完成核对", current)
	}
	preflightDisposition := current.ControlDisposition
	interrupted, err := s.repo.InterruptAdminAgentRun(repository.AdminAgentRunInterruptCommand{
		RunID: request.RunID, ExpectedStateVersion: request.ExpectedStateVersion,
		ActorUserID: actor.ID, Reason: request.Reason, Now: time.Now().UTC(),
	})
	if err != nil {
		return nil, s.mapAdminAgentRunInterruptError(request.RunID, err)
	}
	s.cancelActiveTask(agentRuntimeModelTaskID(interrupted.Scope.RunID, interrupted.State.StepNumber))
	affectedTaskIDs := make([]string, 0, len(interrupted.TaskDispositions))
	for _, disposition := range interrupted.TaskDispositions {
		affectedTaskIDs = append(affectedTaskIDs, disposition.TaskID)
		s.cancelActiveTask(disposition.TaskID)
	}
	latest, err := s.repo.AdminAgentRun(request.RunID, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return &AdminAgentRunInterruptResponse{
		Run: *latest, Disposition: preflightDisposition, AffectedTaskIDs: affectedTaskIDs,
		ReconciliationPending: interrupted.ReconciliationPending,
	}, nil
}

func (s *Service) mapAdminAgentRunInterruptError(runID string, err error) error {
	latest, latestErr := s.repo.AdminAgentRun(runID, time.Now().UTC())
	if latestErr != nil && !errors.Is(latestErr, gorm.ErrRecordNotFound) {
		return errors.Join(err, latestErr)
	}
	switch {
	case errors.Is(err, repository.ErrAdminAgentRunNotFound), errors.Is(latestErr, gorm.ErrRecordNotFound):
		return adminAgentRunError(http.StatusNotFound, "admin_agent_run_not_found", "Agent 运行不存在", nil)
	case errors.Is(err, repository.ErrAdminAgentRunTerminal):
		return adminAgentRunError(http.StatusConflict, "admin_agent_run_terminal", "Agent 运行已经结束", latest)
	case errors.Is(err, repository.ErrAdminAgentRunBillingUnresolved):
		return adminAgentRunError(http.StatusConflict, "admin_agent_run_billing_unresolved", "Agent 运行存在未决账务，需先完成核对", latest)
	case errors.Is(err, repository.ErrAdminAgentRunStateConflict):
		return adminAgentRunError(http.StatusConflict, "admin_agent_run_state_conflict", "Agent 运行状态已经变化，请按最新状态重试", latest)
	default:
		return err
	}
}

func adminAgentRunError(status int, code string, message string, latest *repository.AdminAgentRunRecord) *AdminAgentRunControlError {
	return &AdminAgentRunControlError{Status: status, ErrorCode: code, Message: message, Latest: latest}
}
