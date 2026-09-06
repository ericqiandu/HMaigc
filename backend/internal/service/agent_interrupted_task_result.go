package service

import (
	"encoding/json"
	"errors"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

// 结果读取只消费已批准提案、持久任务与账单，不重新报价、不创建任务。
func (s *Service) interruptedAgentTaskResult(scope agentruntime.Scope, call agentruntime.ToolCallDecision, task *model.Task) (agentruntime.ToolExecutionResult, error) {
	decoded, err := agentruntime.DecodeCapabilityArguments(call.ToolName, call.Arguments)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, err
	}
	prefix := ""
	switch call.ToolName {
	case agentruntime.ToolMediaGenerate:
		prefix = "media"
	case agentruntime.ToolVisionAnalyze:
		prefix = "vision"
	default:
		return agentruntime.ToolExecutionResult{}, errors.New("interrupted tool has no asynchronous task result")
	}
	_, proposal, err := s.authorizedAgentCapabilityRecord(scope, call, call.ToolName, prefix)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, err
	}
	if task == nil || task.UserID != scope.ActorUserID || task.ProjectID != scope.CanvasID || task.Audience != model.TaskAudienceInternal || task.BillingOrderID == "" || proposal.Quote == nil {
		return agentruntime.ToolExecutionResult{}, errors.New("interrupted task scope or approval is invalid")
	}
	order, err := s.repo.BillingOrder(task.BillingOrderID)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, err
	}
	billingScope, err := billingAccountScopeFromAgent(scope)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, err
	}
	if order.UserID != scope.ActorUserID || order.TeamID != billingScope.TeamID || order.TaskID != task.ID || order.ChannelModelID != proposal.Quote.ModelRecordID || order.Model != proposal.Quote.ModelKey || order.PriceVersion != proposal.Quote.PriceVersion || order.Capability != task.Capability {
		return agentruntime.ToolExecutionResult{}, errors.New("interrupted task billing conflicts with approval")
	}
	switch arguments := decoded.(type) {
	case agentruntime.MediaGenerateArguments:
		var parameters struct {
			Prompt string `json:"prompt"`
		}
		if err := json.Unmarshal(arguments.Parameters, &parameters); err != nil {
			return agentruntime.ToolExecutionResult{}, err
		}
		if task.Type != "canvas_"+string(arguments.MediaKind) || task.Capability != string(arguments.MediaKind) || task.Operation != agentMediaGenerationOperationForRun(scope.RunID) || task.Prompt != strings.TrimSpace(parameters.Prompt) || order.IdempotencyKey != "agent-media:"+task.ID || order.AmountMicrocredits != proposal.Quote.AmountMicrocredits || arguments.ModelRecordID != order.ChannelModelID || arguments.ModelKey != order.Model {
			return agentruntime.ToolExecutionResult{}, errors.New("interrupted media task conflicts with approved call")
		}
		return s.agentMediaTaskResult(scope, call, arguments, task, order)
	case agentruntime.VisionAnalyzeArguments:
		input, err := decodeAgentVisionTaskInput(task.InputJSON)
		if err != nil {
			return agentruntime.ToolExecutionResult{}, err
		}
		actual, err := json.Marshal(input.Arguments)
		if err != nil {
			return agentruntime.ToolExecutionResult{}, err
		}
		if task.Type != agentVisionTaskType || task.Capability != "vision" || task.Operation != agentVisionOperationForRun(scope.RunID) || input.RunID != scope.RunID || input.ToolCallID != call.ToolCallID || !equalAgentToolArguments(string(actual), call.Arguments) || order.IdempotencyKey != "proxy-token:agent-vision:"+task.ID || order.ReservedAmountMicrocredits != proposal.Quote.AmountMicrocredits || arguments.ModelRecordID != order.ChannelModelID || arguments.ModelKey != order.Model {
			return agentruntime.ToolExecutionResult{}, errors.New("interrupted vision task conflicts with approved call")
		}
		return agentVisionTaskResult(arguments, task, order)
	default:
		return agentruntime.ToolExecutionResult{}, errors.New("interrupted task result contract is unsupported")
	}
}
