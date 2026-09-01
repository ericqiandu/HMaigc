package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

type agentMediaGenerateCapabilityExecutor struct {
	service *Service
}

func (executor agentMediaGenerateCapabilityExecutor) Execute(ctx context.Context, scope agentruntime.Scope, call agentruntime.ToolCallDecision) (agentruntime.ToolExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return agentruntime.ToolExecutionResult{}, err
	}
	if executor.service == nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_unavailable", "media.generate executor is unavailable")
	}
	decoded, err := agentruntime.DecodeCapabilityArguments(call.ToolName, call.Arguments)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_arguments_invalid", "media.generate arguments are invalid")
	}
	arguments, ok := decoded.(agentruntime.MediaGenerateArguments)
	if !ok || call.ToolName != agentruntime.ToolMediaGenerate || call.ActionVersion != 1 {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_arguments_invalid", "media.generate call identity is invalid")
	}
	record, proposal, err := executor.service.authorizedAgentCapabilityRecord(scope, call, agentruntime.ToolMediaGenerate, "media")
	if err != nil {
		return agentruntime.ToolExecutionResult{}, err
	}
	if record.Status == agentruntime.ToolCallSucceeded {
		result, decodeErr := agentruntime.DecodeCapabilityResult(agentruntime.ToolMediaGenerate, json.RawMessage(record.OutputJSON))
		if decodeErr != nil {
			return agentruntime.ToolExecutionResult{}, failAgentCapability("media_receipt_invalid", "media.generate stored receipt is invalid")
		}
		return agentruntime.NewToolExecutionResult(agentruntime.ToolMediaGenerate, result)
	}
	if proposal.Quote == nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("media_quote_missing", "media.generate approval quote is missing")
	}
	command, err := executor.service.agentMediaCapabilityCommand(scope, arguments)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "media_facts_changed", Err: err}
	}
	current, err := executor.service.freezeMediaQuoteAt(scope, command, proposal.ExpiresAt)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "media_quote_changed", Err: err}
	}
	if current.ChannelModelID != proposal.Quote.ModelRecordID || current.ModelKey != proposal.Quote.ModelKey ||
		current.PriceVersion != proposal.Quote.PriceVersion || current.AmountMicrocredits != proposal.Quote.AmountMicrocredits {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("media_quote_changed", "media.generate authoritative quote changed after approval")
	}
	current.ApprovedAt = record.ApprovalDecidedAt.UTC()
	task, order, err := executor.service.EnsureMediaTask(ctx, scope, *current)
	if err != nil {
		if errors.Is(err, ErrCostApprovalQuoteMismatch) {
			return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "media_quote_changed", Err: err}
		}
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "media_task_failed", Err: err}
	}
	switch task.Status {
	case model.TaskStatusQueued, model.TaskStatusRunning:
		if order.Status == model.BillingStatusUncertain {
			return agentruntime.ToolExecutionResult{}, failAgentCapability("media_settlement_uncertain", "media.generate billing requires reconciliation")
		}
		if order.Status != model.BillingStatusReserved && order.Status != model.BillingStatusRunning {
			return agentruntime.ToolExecutionResult{}, failAgentCapability("media_settlement_incomplete", "media.generate billing state conflicts with an active task")
		}
		return agentruntime.ToolExecutionResult{Pending: true}, nil
	case model.TaskStatusFailed:
		return agentMediaCapabilityTerminalFailure(order.Status, "media_generation_failed", "media.generate task failed")
	case model.TaskStatusCancelled:
		return agentMediaCapabilityTerminalFailure(order.Status, "media_generation_cancelled", "media.generate task was cancelled")
	case model.TaskStatusSucceeded:
		if order.Status == model.BillingStatusUncertain {
			return agentruntime.ToolExecutionResult{}, failAgentCapability("media_settlement_uncertain", "media.generate billing requires reconciliation")
		}
		if order.Status != model.BillingStatusSettled {
			return agentruntime.ToolExecutionResult{}, failAgentCapability("media_settlement_incomplete", "media.generate succeeded without a settled billing order")
		}
	default:
		return agentruntime.ToolExecutionResult{}, failAgentCapability("media_task_invalid", "media.generate task status is invalid")
	}
	resources, err := executor.service.agentMediaCapabilityResources(scope, task, arguments.MediaKind)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "media_result_invalid", Err: err}
	}
	return agentruntime.NewToolExecutionResult(agentruntime.ToolMediaGenerate, agentruntime.MediaGenerateResult{
		TaskID: task.ID, BillingOrderID: order.ID, MediaKind: arguments.MediaKind,
		ClientRequestID: arguments.ClientRequestID, Resources: resources,
	})
}

func agentMediaCapabilityTerminalFailure(status model.BillingStatus, failureCode string, message string) (agentruntime.ToolExecutionResult, error) {
	switch status {
	case model.BillingStatusRefunded:
		return agentruntime.ToolExecutionResult{}, failAgentCapability(failureCode, message)
	case model.BillingStatusUncertain:
		return agentruntime.ToolExecutionResult{}, failAgentCapability("media_settlement_uncertain", "media.generate billing requires reconciliation")
	default:
		return agentruntime.ToolExecutionResult{}, failAgentCapability("media_settlement_incomplete", "media.generate terminal task has unresolved billing")
	}
}

func (s *Service) agentMediaCapabilityResources(scope agentruntime.Scope, task *model.Task, mediaKind agentruntime.MediaKind) ([]agentruntime.MediaGeneratedResourceResult, error) {
	if task == nil || task.Status != model.TaskStatusSucceeded {
		return nil, errors.New("media.generate task success facts are missing")
	}
	decoded, err := agentruntime.DecodeMediaTaskResultResources([]byte(task.ResultJSON))
	if err != nil {
		return nil, err
	}
	resources := make([]agentruntime.MediaGeneratedResourceResult, 0, len(decoded))
	for _, candidate := range decoded {
		kind := agentruntime.MediaKind(candidate.Kind)
		if kind != mediaKind {
			return nil, errors.New("media.generate result kind conflicts with the approved capability")
		}
		resource, loadErr := s.agentResourceForScope(scope, candidate.ResourceID)
		if loadErr != nil || resource.Status != model.ResourceStatusReady || resource.Kind != string(kind) {
			return nil, errors.Join(errors.New("media.generate result resource is not ready in the approved tenant scope"), loadErr)
		}
		resources = append(resources, agentruntime.MediaGeneratedResourceResult{
			ResourceID: resource.ID, Kind: kind, URL: "/api/resources/" + resource.ID + "/file",
		})
	}
	return resources, nil
}

func (s *Service) agentResourceForScope(scope agentruntime.Scope, resourceID string) (*model.Resource, error) {
	if scope.TenantKind == agentruntime.TenantTeam {
		return s.repo.ResourceForTeam(scope.TenantID, resourceID)
	}
	return s.repo.ResourceForUser(scope.ActorUserID, resourceID)
}

func (s *Service) freezeAgentMediaCapabilityQuote(scope agentruntime.Scope, call agentruntime.ToolCallDecision, now time.Time) (*agentruntime.ApprovalCostQuote, error) {
	decoded, err := agentruntime.DecodeCapabilityArguments(call.ToolName, call.Arguments)
	if err != nil {
		return nil, err
	}
	arguments, ok := decoded.(agentruntime.MediaGenerateArguments)
	if !ok || call.ToolName != agentruntime.ToolMediaGenerate || call.ActionVersion != 1 {
		return nil, errors.New("media.generate call identity is invalid")
	}
	command, err := s.agentMediaCapabilityCommand(scope, arguments)
	if err != nil {
		return nil, err
	}
	attempt, err := s.FreezeMediaQuote(scope, command, now)
	if err != nil {
		return nil, err
	}
	return &agentruntime.ApprovalCostQuote{
		ModelRecordID: attempt.ChannelModelID, ModelKey: attempt.ModelKey,
		PriceVersion: attempt.PriceVersion, AmountMicrocredits: attempt.AmountMicrocredits,
	}, nil
}

func (s *Service) agentMediaCapabilityCommand(scope agentruntime.Scope, arguments agentruntime.MediaGenerateArguments) (MediaGenerationCommand, error) {
	if err := scope.Validate(); err != nil {
		return MediaGenerationCommand{}, err
	}
	selected, err := s.repo.ChannelModelByRecordID(arguments.ModelRecordID)
	if err != nil || selected == nil || !selected.Enabled || !selected.PriceConfigured || selected.ModelKey != arguments.ModelKey {
		return MediaGenerationCommand{}, errors.Join(errAgentMediaModelUnavailable, err)
	}
	if err := agentruntime.ValidateMediaGenerateModelCapability(arguments, selected.Capability); err != nil {
		return MediaGenerationCommand{}, err
	}
	accessible, err := s.requireAccessibleChannelModel(scope.ActorUserID, selected.ChannelID, selected.ModelKey)
	if err != nil || accessible.ID != selected.ID {
		return MediaGenerationCommand{}, errors.Join(errAgentMediaModelUnavailable, err)
	}
	channel, err := s.repo.SystemChannel(selected.ChannelID)
	if err != nil {
		return MediaGenerationCommand{}, err
	}
	capabilities := publicProviderModelCapabilities(channel.InterfaceType, selected.ModelKey)
	if capabilities == nil {
		return MediaGenerationCommand{}, errAgentMediaModelUnavailable
	}
	facts := []repository.AgentCapabilityResourceFact{}
	if len(arguments.SourceResourceIDs) > 0 {
		facts, err = s.repo.AgentCapabilityResourcesForScope(scope, arguments.SourceResourceIDs, len(arguments.SourceResourceIDs))
		if err != nil || len(facts) != len(arguments.SourceResourceIDs) {
			return MediaGenerationCommand{}, errors.Join(errAgentMediaInputChanged, err)
		}
	}
	factsByID := make(map[string]repository.AgentCapabilityResourceFact, len(facts))
	for _, fact := range facts {
		factsByID[fact.ResourceID] = fact
	}
	resources := make([]agentMediaInputResource, 0, len(arguments.SourceResourceIDs))
	for _, resourceID := range arguments.SourceResourceIDs {
		fact, found := factsByID[resourceID]
		if !found {
			return MediaGenerationCommand{}, errAgentMediaInputChanged
		}
		resources = append(resources, agentMediaInputResource{
			ResourceID: resourceID, Kind: fact.Kind, URL: "/api/resources/" + resourceID + "/file",
			MimeType: fact.MimeType, DurationMS: fact.DurationMS,
		})
	}
	input, prompt, quantity, err := buildAgentMediaGenerationTaskInput(agentMediaGenerationArguments{
		InputResources: resources, Capability: string(arguments.MediaKind), Parameters: arguments.Parameters,
		GenerationModel: agentruntime.GenerationModelSelection{ChannelID: selected.ChannelID, Model: selected.ModelKey},
	}, capabilities)
	if err != nil {
		return MediaGenerationCommand{}, err
	}
	parameters, err := json.Marshal(input)
	if err != nil {
		return MediaGenerationCommand{}, err
	}
	return MediaGenerationCommand{
		ArtifactRevisionID: agentMediaCapabilityIdentity(scope, arguments), Attempt: 1,
		TaskType: "canvas_" + string(arguments.MediaKind), Operation: agentMediaGenerationOperationForRun(scope.RunID),
		Prompt: prompt, Capability: string(arguments.MediaKind), ChannelID: selected.ChannelID, ModelKey: selected.ModelKey,
		ParametersJSON: string(parameters), Quantity: quantity, ProviderCapabilities: capabilities,
	}, nil
}

func agentMediaCapabilityIdentity(scope agentruntime.Scope, arguments agentruntime.MediaGenerateArguments) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		string(scope.TenantKind), scope.TenantID, scope.DomainProjectID, scope.CanvasID, scope.ThreadID, scope.RunID,
		arguments.TargetCanvasNodeID, arguments.ClientRequestID,
	}, "\x00")))
	return "media-capability-" + hex.EncodeToString(digest[:16])
}

func (s *Service) authorizedAgentCapabilityRecord(scope agentruntime.Scope, call agentruntime.ToolCallDecision, toolName agentruntime.ToolName, prefix string) (*model.AgentToolCall, agentruntime.ApprovalProposal, error) {
	record, err := s.repo.AgentToolCallForScope(scope, call.ToolCallID, call.ActionVersion)
	if err != nil {
		return nil, agentruntime.ApprovalProposal{}, &agentCapabilityExecutionError{Code: prefix + "_proposal_missing", Err: err}
	}
	if record.ToolName != string(toolName) || record.ToolName != string(call.ToolName) ||
		!equalAgentToolArguments(record.InputJSON, call.Arguments) || record.ApprovalProposalHash == "" {
		return nil, agentruntime.ApprovalProposal{}, failAgentCapability(prefix+"_proposal_conflict", string(toolName)+" frozen proposal conflicts with the requested call")
	}
	if record.Status != agentruntime.ToolCallPending && record.Status != agentruntime.ToolCallRunning && record.Status != agentruntime.ToolCallSucceeded {
		return nil, agentruntime.ApprovalProposal{}, failAgentCapability(prefix+"_proposal_conflict", string(toolName)+" proposal is not executable")
	}
	if record.ApprovalDecision != agentruntime.ToolApprovalApproved || record.ApprovalByUserID != scope.ActorUserID || record.ApprovalDecidedAt == nil {
		return nil, agentruntime.ApprovalProposal{}, failAgentCapability(prefix+"_proposal_not_approved", string(toolName)+" proposal has not been approved by the current actor")
	}
	if err := validateStoredApprovalProposal(scope, record, record.ApprovalProposalHash, time.Now().UTC(), false); err != nil {
		return nil, agentruntime.ApprovalProposal{}, &agentCapabilityExecutionError{Code: prefix + "_proposal_invalid", Err: err}
	}
	proposal, err := agentruntime.DecodeApprovalProposal(json.RawMessage(record.ApprovalProposalJSON))
	if err != nil {
		return nil, agentruntime.ApprovalProposal{}, &agentCapabilityExecutionError{Code: prefix + "_proposal_invalid", Err: err}
	}
	return record, proposal, nil
}
