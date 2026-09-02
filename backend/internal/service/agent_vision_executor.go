package service

import (
	"context"
	"encoding/json"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

type agentVisionAnalyzeCapabilityExecutor struct {
	service *Service
}

func (executor agentVisionAnalyzeCapabilityExecutor) Execute(
	ctx context.Context,
	scope agentruntime.Scope,
	call agentruntime.ToolCallDecision,
) (agentruntime.ToolExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return agentruntime.ToolExecutionResult{}, err
	}
	if executor.service == nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_unavailable", "vision.analyze executor is unavailable")
	}
	decoded, err := agentruntime.DecodeCapabilityArguments(call.ToolName, call.Arguments)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_arguments_invalid", "vision.analyze arguments are invalid")
	}
	arguments, ok := decoded.(agentruntime.VisionAnalyzeArguments)
	if !ok || call.ToolName != agentruntime.ToolVisionAnalyze || call.ActionVersion != 1 {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_arguments_invalid", "vision.analyze call identity is invalid")
	}
	record, proposal, err := executor.service.authorizedAgentCapabilityRecord(scope, call, agentruntime.ToolVisionAnalyze, "vision")
	if err != nil {
		return agentruntime.ToolExecutionResult{}, err
	}
	if record.Status == agentruntime.ToolCallSucceeded {
		result, decodeErr := agentruntime.DecodeCapabilityResult(agentruntime.ToolVisionAnalyze, json.RawMessage(record.OutputJSON))
		if decodeErr != nil {
			return agentruntime.ToolExecutionResult{}, failAgentCapability("vision_receipt_invalid", "vision.analyze stored receipt is invalid")
		}
		return agentruntime.NewToolExecutionResult(agentruntime.ToolVisionAnalyze, result)
	}
	if proposal.Quote == nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("vision_quote_missing", "vision.analyze approval quote is missing")
	}
	attempt, err := executor.service.prepareAgentVisionAttempt(scope, call, proposal.ExpiresAt)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "vision_facts_changed", Err: err}
	}
	if attempt.Model.ID != proposal.Quote.ModelRecordID || attempt.Model.ModelKey != proposal.Quote.ModelKey ||
		attempt.Model.PriceVersion != proposal.Quote.PriceVersion || attempt.AmountMicrocredits != proposal.Quote.AmountMicrocredits {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("vision_quote_changed", "vision.analyze authoritative quote changed after approval")
	}
	task, order, err := executor.service.ensureAgentVisionTask(ctx, scope, *attempt)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "vision_task_failed", Err: err}
	}
	switch task.Status {
	case model.TaskStatusQueued, model.TaskStatusRunning:
		if order.Status == model.BillingStatusUncertain {
			return agentruntime.ToolExecutionResult{}, failAgentCapability("vision_settlement_uncertain", "vision.analyze billing requires reconciliation")
		}
		if order.Status != model.BillingStatusReserved && order.Status != model.BillingStatusRunning {
			return agentruntime.ToolExecutionResult{}, failAgentCapability("vision_settlement_incomplete", "vision.analyze billing state conflicts with an active task")
		}
		return agentruntime.ToolExecutionResult{Pending: true}, nil
	case model.TaskStatusFailed:
		return agentVisionCapabilityTerminalFailure(order.Status, "vision_analysis_failed", "vision.analyze task failed")
	case model.TaskStatusCancelled:
		return agentVisionCapabilityTerminalFailure(order.Status, "vision_analysis_cancelled", "vision.analyze task was cancelled")
	case model.TaskStatusSucceeded:
		if order.Status == model.BillingStatusUncertain {
			return agentruntime.ToolExecutionResult{}, failAgentCapability("vision_settlement_uncertain", "vision.analyze billing requires reconciliation")
		}
		if order.Status != model.BillingStatusSettled {
			return agentruntime.ToolExecutionResult{}, failAgentCapability("vision_settlement_incomplete", "vision.analyze succeeded without a settled billing order")
		}
	default:
		return agentruntime.ToolExecutionResult{}, failAgentCapability("vision_task_invalid", "vision.analyze task status is invalid")
	}
	result, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolVisionAnalyze, json.RawMessage(task.ResultJSON))
	if err != nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("vision_result_invalid", "vision.analyze task result is invalid")
	}
	visionResult, ok := result.(agentruntime.VisionAnalyzeResult)
	if !ok {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("vision_result_invalid", "vision.analyze task result has an invalid type")
	}
	if validateErr := validateAgentVisionCompletedResult(*task, *order, arguments, visionResult); validateErr != nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("vision_result_invalid", "vision.analyze task result conflicts with settled facts")
	}
	return agentruntime.NewToolExecutionResult(agentruntime.ToolVisionAnalyze, visionResult)
}

func agentVisionCapabilityTerminalFailure(
	status model.BillingStatus,
	failureCode string,
	message string,
) (agentruntime.ToolExecutionResult, error) {
	switch status {
	case model.BillingStatusRefunded:
		return agentruntime.ToolExecutionResult{}, failAgentCapability(failureCode, message)
	case model.BillingStatusUncertain:
		return agentruntime.ToolExecutionResult{}, failAgentCapability("vision_settlement_uncertain", "vision.analyze billing requires reconciliation")
	default:
		return agentruntime.ToolExecutionResult{}, failAgentCapability("vision_settlement_incomplete", "vision.analyze terminal task has unresolved billing")
	}
}

var _ AgentCapabilityExecutor = agentVisionAnalyzeCapabilityExecutor{}
