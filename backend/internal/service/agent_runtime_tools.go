package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

type CoordinateAgentToolInput struct {
	ToolCallID    string
	ActionVersion int
}

type AgentToolApprovalSubmission struct {
	ToolCallID    string
	ActionVersion int
	Decision      agentruntime.ToolApprovalDecision
	ProposalHash  string
}

func (s *Service) SubmitAgentToolApproval(scope agentruntime.Scope, input AgentToolApprovalSubmission) (*AgentRuntimeProgress, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	input.ToolCallID = strings.TrimSpace(input.ToolCallID)
	input.ProposalHash = strings.TrimSpace(input.ProposalHash)
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
		if err := validateStoredApprovalProposal(scope, record, input.ProposalHash, time.Now().UTC(), false); err != nil {
			return nil, err
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
	record, policy, err := s.frozenAgentToolCall(scope, state.PendingToolCall, agentruntime.ToolCallWaitingApproval, state.Configuration.ExecutionMode)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if proposalErr := validateStoredApprovalProposal(scope, record, input.ProposalHash, now, true); proposalErr != nil {
		invalidationCode := approvalProposalInvalidationCode(proposalErr)
		if invalidationCode == "" {
			return nil, proposalErr
		}
		transition, transitionErr := agentruntime.InvalidateToolApproval(state, agentruntime.ToolApprovalInvalidation{
			ToolCallID: input.ToolCallID, ActionVersion: input.ActionVersion,
			ProposalHash: record.ApprovalProposalHash, ErrorCode: invalidationCode,
		})
		if transitionErr != nil {
			return nil, transitionErr
		}
		progress, commitErr := s.commitAgentRuntimeState(scope, state, transition)
		if commitErr != nil {
			return nil, commitErr
		}
		if progress.State.Status != agentruntime.RunRunning {
			return progress, nil
		}
		return s.advanceAgentRun(scope, agentWakeApprovalDecided)
	}
	project, access, err := s.canvasAccess(scope.ActorUserID, scope.CanvasID)
	if err != nil {
		return nil, err
	}
	if err := authorizeAgentToolScope(scope, project, access, policy.RequiredAccess); err != nil {
		return nil, err
	}
	transition, err := agentruntime.ReviewToolApproval(state, agentruntime.ToolApproval{
		ToolCallID: input.ToolCallID, ActionVersion: input.ActionVersion, Decision: input.Decision, ProposalHash: input.ProposalHash,
	})
	if err != nil {
		return nil, err
	}
	progress, err := s.commitAgentRuntimeState(scope, state, transition)
	if err != nil {
		return nil, err
	}
	record, err = s.repo.AgentToolCallForScope(scope, input.ToolCallID, input.ActionVersion)
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

func approvalProposalInvalidationCode(err error) string {
	switch {
	case errors.Is(err, agentruntime.ErrApprovalProposalExpired):
		return agentruntime.ApprovalProposalExpired
	case errors.Is(err, agentruntime.ErrApprovalProposalMismatch):
		return agentruntime.ApprovalProposalMismatch
	default:
		return ""
	}
}

func validateStoredApprovalProposal(scope agentruntime.Scope, record *model.AgentToolCall, proposalHash string, now time.Time, requireFresh bool) error {
	if record == nil {
		return errors.New("agent approval proposal facts are missing")
	}
	if record.ApprovalProposalHash == "" {
		if proposalHash != "" {
			return errors.New("agent approval proposal facts conflict")
		}
		return nil
	}
	if proposalHash == "" || proposalHash != record.ApprovalProposalHash {
		return agentruntime.ErrApprovalProposalMismatch
	}
	if record.ApprovalExpiresAt == nil {
		return errors.New("agent approval proposal facts conflict")
	}
	proposal, err := agentruntime.DecodeApprovalProposal(json.RawMessage(record.ApprovalProposalJSON))
	if err != nil {
		return err
	}
	if proposal.RunID != scope.RunID || proposal.ToolCallID != record.ToolCallID || proposal.ActionVersion != record.ActionVersion ||
		string(proposal.ToolName) != record.ToolName || proposal.Scope.TenantKind != scope.TenantKind || proposal.Scope.TenantID != scope.TenantID ||
		proposal.Scope.ActorUserID != scope.ActorUserID || proposal.Scope.DomainProjectID != scope.DomainProjectID || proposal.Scope.CanvasID != scope.CanvasID ||
		proposal.Scope.ThreadID != scope.ThreadID || !record.ApprovalExpiresAt.Equal(proposal.ExpiresAt) {
		return errors.New("agent approval proposal facts conflict")
	}
	if requireFresh {
		return agentruntime.ValidateApprovalProposalDecision(proposal, proposalHash, now)
	}
	computedHash, err := proposal.Hash()
	if err != nil || computedHash != proposalHash {
		return errors.New("agent approval proposal facts conflict")
	}
	return nil
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
	run, err := s.repo.AgentRunForScope(scope)
	if err != nil {
		return nil, err
	}
	if err := validateAgentRuntimeExecutionContract(*run); err != nil {
		return nil, err
	}
	call := state.PendingToolCall
	expectedStatus := agentruntime.ToolCallPending
	if state.PendingToolStarted {
		expectedStatus = agentruntime.ToolCallRunning
	}
	_, policy, err := s.frozenAgentToolCall(scope, call, expectedStatus, state.Configuration.ExecutionMode)
	if err != nil && expectedStatus == agentruntime.ToolCallRunning {
		// A capability with an authoritative external effect may persist its
		// success receipt atomically with that effect before the runtime
		// checkpoint advances. Resume from that exact frozen receipt.
		_, policy, err = s.frozenAgentToolCall(scope, call, agentruntime.ToolCallSucceeded, state.Configuration.ExecutionMode)
	}
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
	registry, registryErr := newAgentCapabilityRegistry(s)
	if registryErr != nil {
		return nil, registryErr
	}
	execution, executionErr := registry.Execute(context.Background(), scope, *call)
	if executionErr != nil {
		failureCode := "capability_execution_failed"
		var capabilityFailure *agentCapabilityExecutionError
		if errors.As(executionErr, &capabilityFailure) {
			failureCode = capabilityFailure.Code
		}
		return s.resolvePendingAgentToolFailureWithOutput(scope, state, call, failureCode, map[string]string{"reason": executionErr.Error()})
	}
	if execution.Pending {
		if len(execution.Output) != 0 {
			return nil, errors.New("pending agent capability returned terminal output")
		}
		if state.PendingToolStarted {
			return s.agentRuntimeProgressForCurrentState(scope, state)
		}
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
		return s.agentRuntimeProgressForCurrentState(scope, progress.State)
	}
	output := execution.Output
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
