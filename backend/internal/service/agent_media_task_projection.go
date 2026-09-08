package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

type completedAgentMediaCall struct {
	decision  agentruntime.ToolCallDecision
	arguments agentruntime.MediaGenerateArguments
	proposal  agentruntime.ApprovalProposal
}

// projectCompletedAgentMediaTaskResult uses the approved tool call and settled
// task facts as the sole authority. It deliberately does not require an active
// Agent run: a paid provider result must still reach its target node after the
// reasoning session is cancelled, interrupted, or restarted.
func (s *Service) projectCompletedAgentMediaTaskResult(task *model.Task) error {
	if task == nil || strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.UserID) == "" ||
		task.Status != model.TaskStatusSucceeded || task.CompletedAt == nil {
		return errors.New("completed Agent media task facts are invalid")
	}
	runID, ok := agentMediaGenerationRunID(task.Operation)
	if !ok {
		return errors.New("completed task is not an Agent media generation")
	}
	scope, err := s.scopeForAgentRun(&model.User{ID: task.UserID}, runID)
	if err != nil {
		return fmt.Errorf("resolve completed Agent media task scope: %w", err)
	}
	if task.ProjectID != scope.CanvasID {
		return errors.New("completed Agent media task canvas scope conflicts with the approved run")
	}
	call, err := s.completedAgentMediaCallForTask(scope, *task)
	if err != nil {
		return err
	}
	order, err := s.repo.BillingOrder(task.BillingOrderID)
	if err != nil {
		return fmt.Errorf("load completed Agent media billing facts: %w", err)
	}
	if err := validateCompletedAgentMediaBilling(scope, *task, *order, *call); err != nil {
		return err
	}
	resources, err := s.agentMediaCapabilityResources(scope, task, call.arguments.MediaKind)
	if err != nil {
		return fmt.Errorf("load completed Agent media resources: %w", err)
	}
	receipt := agentruntime.MediaGenerateResult{
		TaskID: task.ID, BillingOrderID: order.ID, MediaKind: call.arguments.MediaKind,
		ClientRequestID: call.arguments.ClientRequestID, Resources: resources,
	}
	if err := s.persistAgentMediaResultOnCanvas(scope, call.decision, call.arguments, task, receipt); err != nil {
		return fmt.Errorf("project completed Agent media result to canvas: %w", err)
	}
	return nil
}

func (s *Service) completedAgentMediaCallForTask(scope agentruntime.Scope, task model.Task) (*completedAgentMediaCall, error) {
	records, err := s.repo.AgentToolCallsForScope(scope)
	if err != nil {
		return nil, fmt.Errorf("load approved Agent media calls: %w", err)
	}
	var matched *completedAgentMediaCall
	for index := range records {
		record := &records[index]
		if record.ToolName != string(agentruntime.ToolMediaGenerate) || record.ApprovalDecision != agentruntime.ToolApprovalApproved {
			continue
		}
		decision := agentruntime.ToolCallDecision{
			ToolCallID: record.ToolCallID, ToolName: agentruntime.ToolName(record.ToolName),
			ActionVersion: record.ActionVersion, Arguments: json.RawMessage(record.InputJSON),
		}
		decoded, decodeErr := agentruntime.DecodeCapabilityArguments(decision.ToolName, decision.Arguments)
		arguments, validArguments := decoded.(agentruntime.MediaGenerateArguments)
		if decodeErr != nil || !validArguments || decision.ActionVersion != 1 {
			return nil, errors.New("stored Agent media call arguments are invalid")
		}
		expectedTaskID := MediaAttemptIdentity(scope, MediaGenerationCommand{
			ArtifactRevisionID: agentMediaCapabilityIdentity(scope, arguments), Attempt: 1,
			TaskType: "canvas_" + string(arguments.MediaKind), Capability: string(arguments.MediaKind),
		})
		if expectedTaskID != task.ID {
			continue
		}
		if matched != nil {
			return nil, errors.New("completed Agent media task matches multiple approved tool calls")
		}
		if record.ApprovalByUserID != scope.ActorUserID ||
			record.ApprovalDecidedAt == nil || strings.TrimSpace(record.ApprovalProposalHash) == "" {
			return nil, errors.New("completed Agent media task lacks approved tool-call facts")
		}
		if err := validateStoredApprovalProposal(scope, record, record.ApprovalProposalHash, task.CreatedAt, false); err != nil {
			return nil, fmt.Errorf("validate completed Agent media approval: %w", err)
		}
		proposal, err := agentruntime.DecodeApprovalProposal(json.RawMessage(record.ApprovalProposalJSON))
		if err != nil || !equalAgentToolArguments(record.InputJSON, proposal.Arguments) {
			return nil, errors.Join(errors.New("completed Agent media approval arguments conflict with the tool call"), err)
		}
		matched = &completedAgentMediaCall{decision: decision, arguments: arguments, proposal: proposal}
	}
	if matched == nil {
		return nil, errors.New("approved Agent media tool call for the completed task is missing")
	}
	return matched, nil
}

func validateCompletedAgentMediaBilling(
	scope agentruntime.Scope,
	task model.Task,
	order model.BillingOrder,
	call completedAgentMediaCall,
) error {
	quote := call.proposal.Quote
	if quote == nil || task.BillingOrderID == "" || order.ID != task.BillingOrderID || order.TaskID != task.ID ||
		order.UserID != task.UserID || order.Status != model.BillingStatusSettled ||
		order.ChannelModelID != call.arguments.ModelRecordID || order.Model != call.arguments.ModelKey ||
		order.Capability != string(call.arguments.MediaKind) || task.Capability != string(call.arguments.MediaKind) ||
		order.PriceVersion != quote.PriceVersion || order.AmountMicrocredits != quote.AmountMicrocredits ||
		quote.ModelRecordID != call.arguments.ModelRecordID || quote.ModelKey != call.arguments.ModelKey {
		return errors.New("completed Agent media task conflicts with its settled billing and approval facts")
	}
	switch scope.TenantKind {
	case agentruntime.TenantPersonal:
		if order.TeamID != "" || scope.TenantID != task.UserID {
			return errors.New("completed Agent media task personal billing scope conflicts with the approved run")
		}
	case agentruntime.TenantTeam:
		if order.TeamID != scope.TenantID {
			return errors.New("completed Agent media task team billing scope conflicts with the approved run")
		}
	default:
		return errors.New("completed Agent media task tenant scope is invalid")
	}
	if task.ProviderRequestID != "" && order.ProviderRequestID != "" && task.ProviderRequestID != order.ProviderRequestID {
		return errors.New("completed Agent media provider request conflicts with settled billing")
	}
	if order.ProviderTaskStatus != "" && order.ProviderTaskStatus != "succeeded" {
		return errors.New("completed Agent media provider status is not successful")
	}
	return nil
}
