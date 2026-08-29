package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

type CoordinateAgentToolInput struct {
	ToolCallID    string
	ActionVersion int
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
		if state.Status == agentruntime.RunCancelled {
			if err := s.cancelAgentRunTreeContexts(scope); err != nil {
				return nil, err
			}
		}
		return s.agentRuntimeProgressForCurrentState(scope, state)
	}
	if state.PendingToolCall == nil {
		return nil, errors.New("agent approval tool facts are missing")
	}
	_, policy, err := s.frozenAgentToolCall(scope, state.PendingToolCall, agentruntime.ToolCallWaitingApproval, state.Configuration.ExecutionMode)
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
	if progress.State.Status == agentruntime.RunCancelled {
		if err := s.cancelAgentRunTreeContexts(scope); err != nil {
			return nil, err
		}
	}
	if progress.State.Status != agentruntime.RunRunning {
		if progress.State.Status != agentruntime.RunWaitingTool {
			return progress, nil
		}
	}
	return s.advanceAgentRun(scope, agentWakeApprovalDecided)
}

func (s *Service) CoordinatePendingAgentTool(scope agentruntime.Scope, input CoordinateAgentToolInput) (*AgentRuntimeProgress, error) {
	progress, err := s.coordinatePendingAgentTool(scope, input)
	if err != nil || progress == nil || progress.State.Status != agentruntime.RunRunning {
		return progress, err
	}
	return s.advanceAgentRun(scope, agentWakeStaleRecovery)
}

func (s *Service) coordinatePendingAgentTool(scope agentruntime.Scope, input CoordinateAgentToolInput) (*AgentRuntimeProgress, error) {
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
	record, policy, err := s.frozenAgentToolCall(scope, call, expectedStatus, state.Configuration.ExecutionMode)
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
	case agentruntime.ToolSkillLoad:
		output, err = executeAgentSkillLoad(state.Configuration, call.Arguments)
		if err != nil {
			return s.resolvePendingAgentToolFailureWithOutput(scope, state, call, "skill_load_invalid", map[string]string{"reason": err.Error()})
		}
	case agentruntime.ToolProductionPlan:
		output, err = s.executeAgentProductionPlan(scope, call.Arguments)
		if errors.Is(err, errAgentRuntimeProductionPlanInput) {
			return s.resolvePendingAgentToolFailureWithOutput(scope, state, call, "production_plan_invalid", map[string]string{"reason": err.Error()})
		}
		if errors.Is(err, repository.ErrAgentProductionPlanVersionConflict) {
			return s.resolvePendingAgentToolFailureWithOutput(scope, state, call, "production_plan_version_conflict", map[string]string{"reason": err.Error()})
		}
	case agentruntime.ToolSpecialistDelegate:
		return s.coordinatePendingAgentSpecialistDelegate(scope, state, call)
	case agentruntime.ToolProductionRender:
		return s.coordinatePendingAgentProductionRender(scope, state, call, record)
	case agentruntime.ToolVisionAnalyze:
		return s.coordinatePendingAgentVisualAnalysis(scope, state, call, record)
	case agentruntime.ToolMediaGenerate:
		return s.coordinatePendingAgentMediaGeneration(scope, state, call, record)
	case agentruntime.ToolCanvasCommit:
		return s.coordinatePendingAgentProductionCanvasCommit(scope, state, call, record)
	case agentruntime.ToolCanvasProject:
		return s.coordinatePendingAgentCanvasProjection(scope, state, call, record)
	case agentruntime.ToolMediaAssemble:
		return s.coordinatePendingAgentMediaAssembly(scope, state, call, record)
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

func (s *Service) agentRuntimeProgressForCurrentState(scope agentruntime.Scope, state agentruntime.RuntimeState) (*AgentRuntimeProgress, error) {
	run, err := s.repo.AgentRunForScope(scope)
	if err != nil {
		return nil, err
	}
	return &AgentRuntimeProgress{Run: *run, State: state}, nil
}

func (s *Service) frozenAgentToolCall(scope agentruntime.Scope, call *agentruntime.ToolCallDecision, expectedStatus agentruntime.ToolCallStatus, executionMode agentruntime.ExecutionMode) (*model.AgentToolCall, agentruntime.ToolPolicy, error) {
	if call == nil {
		return nil, agentruntime.ToolPolicy{}, errors.New("agent tool facts are missing")
	}
	record, err := s.repo.AgentToolCallForScope(scope, call.ToolCallID, call.ActionVersion)
	if err != nil {
		return nil, agentruntime.ToolPolicy{}, err
	}
	run, err := s.repo.AgentRunForScope(scope)
	if err != nil {
		return nil, agentruntime.ToolPolicy{}, err
	}
	policy, ok := agentruntime.ToolPolicyForSchema(call.ToolName, run.ToolSchemaVersion)
	approvalRequired := agentruntime.ApprovalRequiredFor(policy, executionMode)
	if !ok || record.ToolName != string(call.ToolName) || record.Status != expectedStatus ||
		record.RiskLevel != policy.RiskLevel || record.RequiredAccess != policy.RequiredAccess ||
		record.ApprovalRequired != approvalRequired || !equalAgentToolArguments(record.InputJSON, call.Arguments) ||
		record.IdempotencyKey != scope.RunID+":"+call.ToolCallID+":"+strconv.Itoa(call.ActionVersion) {
		return nil, agentruntime.ToolPolicy{}, errors.New("agent tool frozen facts conflict")
	}
	return record, policy, nil
}

func equalAgentToolArguments(stored string, current json.RawMessage) bool {
	storedCanonical, err := canonicalAgentJSON([]byte(stored))
	if err != nil {
		return false
	}
	currentCanonical, err := canonicalAgentJSON(current)
	return err == nil && bytes.Equal(storedCanonical, currentCanonical)
}

func canonicalAgentJSON(value []byte) ([]byte, error) {
	value = bytes.TrimSpace(value)
	if len(value) == 0 || !json.Valid(value) {
		return nil, errors.New("agent JSON is invalid")
	}
	switch value[0] {
	case '{':
		var object map[string]json.RawMessage
		if err := json.Unmarshal(value, &object); err != nil {
			return nil, err
		}
		keys := make([]string, 0, len(object))
		for key := range object {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var buffer bytes.Buffer
		buffer.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				buffer.WriteByte(',')
			}
			encodedKey, err := json.Marshal(key)
			if err != nil {
				return nil, err
			}
			encodedValue, err := canonicalAgentJSON(object[key])
			if err != nil {
				return nil, err
			}
			buffer.Write(encodedKey)
			buffer.WriteByte(':')
			buffer.Write(encodedValue)
		}
		buffer.WriteByte('}')
		return buffer.Bytes(), nil
	case '[':
		var array []json.RawMessage
		if err := json.Unmarshal(value, &array); err != nil {
			return nil, err
		}
		var buffer bytes.Buffer
		buffer.WriteByte('[')
		for index := range array {
			if index > 0 {
				buffer.WriteByte(',')
			}
			encodedValue, err := canonicalAgentJSON(array[index])
			if err != nil {
				return nil, err
			}
			buffer.Write(encodedValue)
		}
		buffer.WriteByte(']')
		return buffer.Bytes(), nil
	default:
		var compact bytes.Buffer
		if err := json.Compact(&compact, value); err != nil {
			return nil, err
		}
		return compact.Bytes(), nil
	}
}

func (s *Service) rejectedToolFailureClass(
	scope agentruntime.Scope,
	state agentruntime.RuntimeState,
	call *agentruntime.ToolCallDecision,
	failureCode string,
	currentOutput json.RawMessage,
	initial agentruntime.ToolFailureClass,
) (agentruntime.ToolFailureClass, error) {
	if initial == agentruntime.ToolFailureTerminal || state.LastToolResult == nil ||
		state.LastToolResult.Succeeded || state.LastToolResult.ErrorCode != failureCode {
		return initial, nil
	}
	previous, err := s.repo.AgentToolCallForScope(
		scope,
		state.LastToolResult.ToolCallID,
		state.LastToolResult.ActionVersion,
	)
	if err != nil {
		return "", err
	}
	if previous.Status != agentruntime.ToolCallFailed || previous.ErrorCode != failureCode {
		return "", errors.New("agent repeated tool failure facts conflict")
	}
	if !equalAgentToolArguments(previous.OutputJSON, state.LastToolResult.Output) {
		return "", errors.New("agent repeated tool failure output facts conflict")
	}
	if call != nil && previous.ToolName == string(call.ToolName) &&
		equalAgentToolArguments(previous.InputJSON, call.Arguments) &&
		equalAgentToolArguments(previous.OutputJSON, currentOutput) {
		return agentruntime.ToolFailureTerminal, nil
	}
	return initial, nil
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

func agentCanvasMutationID(idempotencyKey string) string {
	digest := sha256.Sum256([]byte(idempotencyKey))
	return "agent-" + hex.EncodeToString(digest[:])
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
	return s.resolvePendingAgentToolFailureWithOutput(scope, state, call, failureCode, map[string]string{})
}

func (s *Service) resolvePendingAgentToolFailureWithOutput(
	scope agentruntime.Scope,
	state agentruntime.RuntimeState,
	call *agentruntime.ToolCallDecision,
	failureCode string,
	outputValue map[string]string,
) (*AgentRuntimeProgress, error) {
	output, err := json.Marshal(outputValue)
	if err != nil {
		return nil, err
	}
	return s.resolvePendingAgentToolFailureWithJSONOutput(scope, state, call, failureCode, output)
}

func (s *Service) resolvePendingAgentToolFailureWithJSONOutput(
	scope agentruntime.Scope,
	state agentruntime.RuntimeState,
	call *agentruntime.ToolCallDecision,
	failureCode string,
	output json.RawMessage,
) (*AgentRuntimeProgress, error) {
	failureClass, err := s.rejectedToolFailureClass(
		scope,
		state,
		call,
		failureCode,
		output,
		agentruntime.ToolFailureAgentRepairable,
	)
	if err != nil {
		return nil, err
	}
	resolved, err := agentruntime.ResolveTool(state, agentruntime.ToolResolution{
		ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion,
		Succeeded: false, Output: output, ErrorCode: failureCode, FailureClass: failureClass,
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
