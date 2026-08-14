package service

import (
	"errors"
	"strconv"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

type AgentCanvasSelectionFacts struct {
	Revision int64    `json:"revision"`
	NodeIDs  []string `json:"nodeIds"`
}

type CoordinateAgentToolInput struct {
	ToolCallID    string
	ActionVersion int
	Selection     *AgentCanvasSelectionFacts
}

type AgentToolApprovalSubmission struct {
	ToolCallID    string
	ActionVersion int
	Decision      agentruntime.ToolApprovalDecision
}

func (s *Service) SubmitAgentToolApproval(scope agentruntime.Scope, input AgentToolApprovalSubmission) (*AgentRuntimeProgress, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	input.ToolCallID = strings.TrimSpace(input.ToolCallID)
	if input.ToolCallID == "" || input.ActionVersion < 1 || (input.Decision != agentruntime.ToolApprovalApproved && input.Decision != agentruntime.ToolApprovalRejected) {
		return nil, errors.New("agent tool approval submission is invalid")
	}
	state, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	if state.Status != agentruntime.RunWaitingApproval {
		record, loadErr := s.repo.AgentToolCallForScope(scope, input.ToolCallID, input.ActionVersion)
		if loadErr != nil {
			return nil, loadErr
		}
		if record.ApprovalDecision != input.Decision || record.ApprovalByUserID != scope.ActorUserID || record.ApprovalDecidedAt == nil {
			return nil, errors.New("agent tool approval facts conflict")
		}
		return s.agentRuntimeProgressForCurrentState(scope, state)
	}
	if state.PendingToolCall == nil {
		return nil, errors.New("agent approval tool facts are missing")
	}
	_, policy, err := s.frozenAgentToolCall(scope, state.PendingToolCall, agentruntime.ToolCallWaitingApproval)
	if err != nil {
		return nil, err
	}
	project, access, err := s.canvasAccess(scope.ActorUserID, scope.CanvasID)
	if err != nil {
		return nil, err
	}
	if err := authorizeAgentToolScope(scope, project, access, policy.RequiredAccess); err != nil {
		return nil, err
	}
	transition, err := agentruntime.ReviewToolApproval(state, agentruntime.ToolApproval{
		ToolCallID: input.ToolCallID, ActionVersion: input.ActionVersion, Decision: input.Decision,
	})
	if err != nil {
		return nil, err
	}
	progress, err := s.commitAgentRuntimeState(scope, state, transition)
	if err != nil {
		return nil, err
	}
	record, err := s.repo.AgentToolCallForScope(scope, input.ToolCallID, input.ActionVersion)
	if err != nil {
		return nil, err
	}
	if record.ApprovalDecision != input.Decision || record.ApprovalByUserID != scope.ActorUserID || record.ApprovalDecidedAt == nil {
		return nil, errors.New("agent tool approval facts conflict")
	}
	if progress.State.Status != agentruntime.RunRunning {
		return progress, nil
	}
	return s.resumeAgentRuntimeModelAfterTool(scope, progress)
}

func (s *Service) CoordinatePendingAgentTool(scope agentruntime.Scope, input CoordinateAgentToolInput) (*AgentRuntimeProgress, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	input.ToolCallID = strings.TrimSpace(input.ToolCallID)
	if input.ToolCallID == "" || input.ActionVersion < 1 {
		return nil, errors.New("agent tool coordination identity is invalid")
	}
	state, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	if state.Status != agentruntime.RunWaitingTool || state.PendingToolCall == nil ||
		state.PendingToolCall.ToolCallID != input.ToolCallID || state.PendingToolCall.ActionVersion != input.ActionVersion {
		record, loadErr := s.repo.AgentToolCallForScope(scope, input.ToolCallID, input.ActionVersion)
		if loadErr != nil {
			return nil, loadErr
		}
		if record.Status != agentruntime.ToolCallSucceeded {
			return nil, errors.New("agent tool coordination facts conflict")
		}
		return s.agentRuntimeProgressForCurrentState(scope, state)
	}
	call := state.PendingToolCall
	_, policy, err := s.frozenAgentToolCall(scope, call, agentruntime.ToolCallPending)
	if err != nil {
		return nil, err
	}
	project, access, err := s.canvasAccess(scope.ActorUserID, scope.CanvasID)
	if err != nil {
		return nil, err
	}
	if err := authorizeAgentToolScope(scope, project, access, policy.RequiredAccess); err != nil {
		return nil, err
	}
	var output []byte
	switch call.ToolName {
	case agentruntime.ToolCanvasReadState:
		output, err = executeAgentCanvasReadState(project, call.Arguments)
	case agentruntime.ToolCanvasReadSelection:
		if err = decodeAgentCanvasReadSelectionArguments(call.Arguments); err == nil {
			output, err = executeAgentCanvasReadSelection(project, input.Selection)
		}
	default:
		return nil, errors.New("agent tool executor is not connected")
	}
	if err != nil {
		return nil, err
	}
	transition, err := agentruntime.ResolveTool(state, agentruntime.ToolResolution{
		ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion, Succeeded: true, Output: output,
	})
	if err != nil {
		return nil, err
	}
	progress, err := s.commitAgentRuntimeState(scope, state, transition)
	if err != nil {
		return nil, err
	}
	recorded, err := s.repo.AgentToolCallForScope(scope, input.ToolCallID, input.ActionVersion)
	if err != nil {
		return nil, err
	}
	if recorded.Status != agentruntime.ToolCallSucceeded {
		return nil, errors.New("agent tool completion facts conflict")
	}
	return s.agentRuntimeProgressForCurrentState(scope, progress.State)
}

func (s *Service) resumeAgentRuntimeModelAfterTool(scope agentruntime.Scope, progress *AgentRuntimeProgress) (*AgentRuntimeProgress, error) {
	run, err := s.repo.AgentRunForScope(scope)
	if err != nil {
		return nil, err
	}
	task, err := s.ensureAgentRuntimeModelTask(scope, *run, progress.State)
	if err != nil {
		return nil, err
	}
	progress.Run = *run
	progress.ModelTask = taskForOutput(*task)
	return progress, nil
}

func (s *Service) agentRuntimeProgressForCurrentState(scope agentruntime.Scope, state agentruntime.RuntimeState) (*AgentRuntimeProgress, error) {
	run, err := s.repo.AgentRunForScope(scope)
	if err != nil {
		return nil, err
	}
	progress := &AgentRuntimeProgress{Run: *run, State: state}
	if state.Status != agentruntime.RunRunning {
		return progress, nil
	}
	return s.resumeAgentRuntimeModelAfterTool(scope, progress)
}

func (s *Service) frozenAgentToolCall(scope agentruntime.Scope, call *agentruntime.ToolCallDecision, expectedStatus agentruntime.ToolCallStatus) (*model.AgentToolCall, agentruntime.ToolPolicy, error) {
	if call == nil {
		return nil, agentruntime.ToolPolicy{}, errors.New("agent tool facts are missing")
	}
	record, err := s.repo.AgentToolCallForScope(scope, call.ToolCallID, call.ActionVersion)
	if err != nil {
		return nil, agentruntime.ToolPolicy{}, err
	}
	policy, ok := agentruntime.ToolPolicyFor(call.ToolName)
	if !ok || record.ToolName != string(call.ToolName) || record.Status != expectedStatus ||
		record.RiskLevel != policy.RiskLevel || record.RequiredAccess != policy.RequiredAccess ||
		record.ApprovalRequired != policy.ApprovalRequired || record.InputJSON != string(call.Arguments) ||
		record.IdempotencyKey != scope.RunID+":"+call.ToolCallID+":"+strconv.Itoa(call.ActionVersion) {
		return nil, agentruntime.ToolPolicy{}, errors.New("agent tool frozen facts conflict")
	}
	return record, policy, nil
}

func authorizeAgentToolScope(scope agentruntime.Scope, project *model.CanvasProject, access CanvasAccessView, required agentruntime.AccessLevel) error {
	if project == nil || project.ID != scope.CanvasID {
		return errors.New("agent canvas scope is invalid")
	}
	if scope.TenantKind == agentruntime.TenantPersonal {
		if project.TeamID != "" || project.UserID != scope.ActorUserID || scope.TenantID != scope.ActorUserID {
			return errors.New("agent personal canvas scope conflict")
		}
	} else if project.TeamID == "" || project.TeamID != scope.TenantID {
		return errors.New("agent team canvas scope conflict")
	}
	switch required {
	case agentruntime.AccessViewer:
		return nil
	case agentruntime.AccessEditor:
		if access.CanEdit {
			return nil
		}
	case agentruntime.AccessManager:
		if access.CanManage {
			return nil
		}
	}
	return Forbidden("当前用户没有执行该 Agent 工具的画布权限")
}
