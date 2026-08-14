package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
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
		if record.Status != agentruntime.ToolCallSucceeded && record.Status != agentruntime.ToolCallFailed {
			return nil, errors.New("agent tool coordination facts conflict")
		}
		return s.agentRuntimeProgressForCurrentState(scope, state)
	}
	call := state.PendingToolCall
	expectedStatus := agentruntime.ToolCallPending
	if state.PendingToolStarted {
		expectedStatus = agentruntime.ToolCallRunning
	}
	record, policy, err := s.frozenAgentToolCall(scope, call, expectedStatus)
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
	case agentruntime.ToolCanvasApplyOps:
		return s.coordinatePendingAgentCanvasMutation(scope, state, call, record)
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

type agentCanvasApplyOpsArguments struct {
	BaseRevision int64               `json:"baseRevision"`
	Patch        CanvasMutationPatch `json:"patch"`
}

type agentCanvasApplyOpsResult struct {
	CanvasID          string `json:"canvasId"`
	BaseRevision      int64  `json:"baseRevision"`
	CommittedRevision int64  `json:"committedRevision"`
	ClientMutationID  string `json:"clientMutationId"`
}

func decodeAgentCanvasApplyOpsArguments(raw json.RawMessage) (agentCanvasApplyOpsArguments, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var arguments agentCanvasApplyOpsArguments
	if err := decoder.Decode(&arguments); err != nil {
		return agentCanvasApplyOpsArguments{}, errors.New("agent canvas mutation arguments are invalid")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || arguments.BaseRevision < 0 {
		return agentCanvasApplyOpsArguments{}, errors.New("agent canvas mutation arguments are invalid")
	}
	if err := validateCanvasMutationPatch(arguments.Patch); err != nil {
		return agentCanvasApplyOpsArguments{}, err
	}
	return arguments, nil
}

func agentCanvasMutationID(idempotencyKey string) string {
	digest := sha256.Sum256([]byte(idempotencyKey))
	return "agent-" + hex.EncodeToString(digest[:])
}

func (s *Service) coordinatePendingAgentCanvasMutation(
	scope agentruntime.Scope,
	state agentruntime.RuntimeState,
	call *agentruntime.ToolCallDecision,
	record *model.AgentToolCall,
) (*AgentRuntimeProgress, error) {
	arguments, err := decodeAgentCanvasApplyOpsArguments(call.Arguments)
	if err != nil {
		return s.resolvePendingAgentToolFailure(scope, state, call, "canvas_mutation_invalid")
	}
	if !state.PendingToolStarted {
		started, beginErr := agentruntime.BeginToolExecution(state, agentruntime.ToolExecution{
			ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion,
		})
		if beginErr != nil {
			return nil, beginErr
		}
		progress, commitErr := s.commitAgentRuntimeState(scope, state, started)
		if commitErr != nil {
			return nil, commitErr
		}
		state = progress.State
		if state.Status != agentruntime.RunWaitingTool || state.PendingToolCall == nil ||
			state.PendingToolCall.ToolCallID != call.ToolCallID || state.PendingToolCall.ActionVersion != call.ActionVersion ||
			!state.PendingToolStarted {
			completed, loadErr := s.repo.AgentToolCallForScope(scope, call.ToolCallID, call.ActionVersion)
			if loadErr != nil {
				return nil, loadErr
			}
			if completed.Status != agentruntime.ToolCallSucceeded && completed.Status != agentruntime.ToolCallFailed {
				return nil, errors.New("agent canvas mutation execution facts conflict")
			}
			return s.agentRuntimeProgressForCurrentState(scope, state)
		}
		record, _, err = s.frozenAgentToolCall(scope, call, agentruntime.ToolCallRunning)
		if err != nil {
			return nil, err
		}
	}
	mutation, err := s.CommitCanvasMutation(&model.User{ID: scope.ActorUserID}, scope.CanvasID, CanvasMutationRequest{
		BaseRevision: arguments.BaseRevision, ClientMutationID: agentCanvasMutationID(record.IdempotencyKey), Patch: arguments.Patch,
	})
	if err != nil {
		if failureCode := agentCanvasMutationFailureCode(err); failureCode != "" {
			return s.resolvePendingAgentToolFailure(scope, state, call, failureCode)
		}
		return nil, err
	}
	output, err := json.Marshal(agentCanvasApplyOpsResult{
		CanvasID: mutation.CanvasID, BaseRevision: arguments.BaseRevision,
		CommittedRevision: mutation.Revision, ClientMutationID: mutation.ClientMutationID,
	})
	if err != nil {
		return nil, err
	}
	latest, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	if latest.Status != agentruntime.RunWaitingTool || latest.PendingToolCall == nil ||
		latest.PendingToolCall.ToolCallID != call.ToolCallID || latest.PendingToolCall.ActionVersion != call.ActionVersion ||
		!latest.PendingToolStarted {
		completed, loadErr := s.repo.AgentToolCallForScope(scope, call.ToolCallID, call.ActionVersion)
		if loadErr != nil {
			return nil, loadErr
		}
		if completed.Status != agentruntime.ToolCallSucceeded && completed.Status != agentruntime.ToolCallFailed {
			return nil, errors.New("agent canvas mutation completion facts conflict")
		}
		return s.agentRuntimeProgressForCurrentState(scope, latest)
	}
	resolved, err := agentruntime.ResolveTool(latest, agentruntime.ToolResolution{
		ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion, Succeeded: true, Output: output,
	})
	if err != nil {
		return nil, err
	}
	progress, err := s.commitAgentRuntimeState(scope, latest, resolved)
	if err != nil {
		return nil, err
	}
	completed, err := s.repo.AgentToolCallForScope(scope, call.ToolCallID, call.ActionVersion)
	if err != nil {
		return nil, err
	}
	if completed.Status != agentruntime.ToolCallSucceeded {
		return nil, errors.New("agent canvas mutation completion facts conflict")
	}
	return s.agentRuntimeProgressForCurrentState(scope, progress.State)
}

func agentCanvasMutationFailureCode(err error) string {
	var conflictErr *CanvasMutationConflictError
	if errors.As(err, &conflictErr) {
		return conflictErr.Code
	}
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		return ""
	}
	switch authErr.Status {
	case http.StatusBadRequest:
		return "canvas_mutation_invalid"
	case http.StatusConflict:
		return "canvas_revision_conflict"
	default:
		return ""
	}
}

func (s *Service) resolvePendingAgentToolFailure(
	scope agentruntime.Scope,
	state agentruntime.RuntimeState,
	call *agentruntime.ToolCallDecision,
	failureCode string,
) (*AgentRuntimeProgress, error) {
	resolved, err := agentruntime.ResolveTool(state, agentruntime.ToolResolution{
		ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion,
		Succeeded: false, Output: json.RawMessage(`{}`), ErrorCode: failureCode,
	})
	if err != nil {
		return nil, err
	}
	progress, err := s.commitAgentRuntimeState(scope, state, resolved)
	if err != nil {
		return nil, err
	}
	recorded, err := s.repo.AgentToolCallForScope(scope, call.ToolCallID, call.ActionVersion)
	if err != nil {
		return nil, err
	}
	if recorded.Status != agentruntime.ToolCallFailed || recorded.ErrorCode != failureCode {
		return nil, errors.New("agent canvas mutation failure facts conflict")
	}
	return s.agentRuntimeProgressForCurrentState(scope, progress.State)
}
