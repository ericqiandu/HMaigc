package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

type agentRuntimeModelTaskResult struct {
	Mode             string                              `json:"mode"`
	Text             string                              `json:"text,omitempty"`
	DecisionFeedback *agentruntime.ModelDecisionFeedback `json:"decisionFeedback,omitempty"`
}

type agentRuntimeModelDecisionRejectedError struct {
	feedback agentruntime.ModelDecisionFeedback
}

func (err *agentRuntimeModelDecisionRejectedError) Error() string {
	return err.feedback.Reason
}

func (s *Service) processAgentRuntimeModelText(ctx context.Context, task model.Task) (agentRuntimeModelTaskResult, error) {
	var input agentRuntimeModelTaskInput
	decoder := json.NewDecoder(bytes.NewBufferString(task.InputJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return agentRuntimeModelTaskResult{}, errors.New("Agent 模型任务输入格式无效")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || input.Mode != "text" || strings.TrimSpace(input.Prompt) == "" {
		return agentRuntimeModelTaskResult{}, errors.New("Agent 模型任务输入事实无效")
	}
	config, err := s.resolveTextTaskProviderConfig(task, input.Config)
	if err != nil {
		return agentRuntimeModelTaskResult{}, errors.New("Agent 冻结模型配置不可用")
	}
	text := ""
	if config.MaxOutputTokens > 0 {
		result, requestErr := runKuaiziChatCompletion(ctx, canvasGenerationInput{Mode: "text", Prompt: input.Prompt, Config: config})
		if requestErr != nil {
			return agentRuntimeModelTaskResult{}, errors.New("Agent 模型请求失败")
		}
		text = result.Text
		if result.ProviderRequestID == "" {
			usageStatus, usage := normalizeTokenUsageFact(result.Usage)
			if recordErr := s.repo.RecordTokenBillingUsage(task.BillingOrderID, repository.TokenUsageFact{InputTokens: usage.InputTokens, CachedTokens: usage.CachedTokens, OutputTokens: usage.OutputTokens}, usageStatus); recordErr != nil {
				return agentRuntimeModelTaskResult{}, errors.New("Agent 模型计费事实保存失败")
			}
			if markErr := s.MarkBillingUncertain(task.BillingOrderID, "上游响应缺少可核对的任务 ID"); markErr != nil {
				return agentRuntimeModelTaskResult{}, errors.New("Agent 模型计费状态保存失败")
			}
		} else if scheduleErr := s.ScheduleTokenBillingReconciliation(task.BillingOrderID, result.ProviderRequestID, "等待 Agent 结果持久化后核对", result.Usage); scheduleErr != nil {
			return agentRuntimeModelTaskResult{}, errors.New("Agent 模型计费事实保存失败")
		}
	} else {
		result, requestErr := runTextTask(ctx, canvasGenerationInput{Mode: "text", Prompt: input.Prompt, Config: config})
		if requestErr != nil {
			return agentRuntimeModelTaskResult{}, errors.New("Agent 模型请求失败")
		}
		text, _ = result["text"].(string)
	}
	text = strings.TrimSpace(text)
	if _, err := agentruntime.ParseModelDecision([]byte(text)); err != nil {
		return agentRuntimeModelTaskResult{
			Mode:             "text",
			DecisionFeedback: &agentruntime.ModelDecisionFeedback{Code: "model_decision_invalid", Reason: err.Error()},
		}, nil
	}
	return agentRuntimeModelTaskResult{Mode: "text", Text: text}, nil
}

func parseAgentRuntimeModelTaskResult(raw string) (agentruntime.ModelDecision, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	var result agentRuntimeModelTaskResult
	if err := decoder.Decode(&result); err != nil {
		return agentruntime.ModelDecision{}, errors.New("Agent 模型任务结果格式无效")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || result.Mode != "text" {
		return agentruntime.ModelDecision{}, errors.New("Agent 模型任务结果事实无效")
	}
	result.Text = strings.TrimSpace(result.Text)
	if result.DecisionFeedback != nil {
		feedback := *result.DecisionFeedback
		feedback.Code = strings.TrimSpace(feedback.Code)
		feedback.Reason = strings.TrimSpace(feedback.Reason)
		if result.Text != "" || feedback.Code != "model_decision_invalid" || feedback.Reason == "" || len(feedback.Reason) > 240 {
			return agentruntime.ModelDecision{}, errors.New("Agent 模型任务结果事实无效")
		}
		return agentruntime.ModelDecision{}, &agentRuntimeModelDecisionRejectedError{feedback: feedback}
	}
	if result.Text == "" {
		return agentruntime.ModelDecision{}, errors.New("Agent 模型任务结果事实无效")
	}
	return agentruntime.ParseModelDecision([]byte(result.Text))
}
