package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

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

var errAgentRuntimeStateObservation = errors.New("Agent 运行状态观察失败")

const agentRuntimeStateObservationInterval = 250 * time.Millisecond

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
	{
		runID, ok := agentRuntimeModelRunID(task.Operation)
		if !ok {
			return agentRuntimeModelTaskResult{}, errors.New("Agent 模型任务运行身份无效")
		}
		scope, scopeErr := s.scopeForAgentRun(&model.User{ID: task.UserID}, runID)
		if scopeErr != nil {
			return agentRuntimeModelTaskResult{}, errors.New("Agent 模型任务运行作用域无效")
		}
		run, runErr := s.repo.AgentRunForScope(scope)
		if runErr != nil {
			return agentRuntimeModelTaskResult{}, errors.New("Agent 模型任务运行事实不可用")
		}
		state, stateErr := s.beginAgentRuntimeModelRequest(scope, run.StepNumber)
		if stateErr != nil {
			return agentRuntimeModelTaskResult{}, stateErr
		}
		observer := agentruntime.NewDecisionStreamObserver()
		itemID := agentruntime.AgentMessageItemID(runID, state.StepNumber)
		started := false
		visibleMessage := ""
		providerContext, cancelProvider := context.WithCancelCause(ctx)
		stopObservation := make(chan struct{})
		observationStopped := make(chan struct{})
		go s.observeAgentRuntimeModelRequest(providerContext, cancelProvider, stopObservation, observationStopped, scope)
		result, requestErr := runKuaiziChatCompletionStream(providerContext, canvasGenerationInput{Mode: "text", Prompt: input.Prompt, Config: config}, func(rawDelta string) error {
			visible, observeErr := observer.Push(rawDelta)
			if observeErr != nil || visible == "" {
				return observeErr
			}
			payload, marshalErr := json.Marshal(agentVisibleModelDeltaPayload{ItemID: itemID, Delta: visible, UserVisible: true, Started: !started})
			if marshalErr != nil {
				return marshalErr
			}
			nextVisibleMessage := visibleMessage + visible
			if _, appendErr := s.repo.AppendAgentMessageDelta(repository.AppendAgentMessageDeltaInput{Scope: scope, ItemID: itemID, PayloadJSON: string(payload), Message: nextVisibleMessage, Started: !started, Now: time.Now().UTC()}); appendErr != nil {
				return fmt.Errorf("agent_visible_delta_projection_failed: %w", appendErr)
			}
			visibleMessage = nextVisibleMessage
			started = true
			return nil
		})
		close(stopObservation)
		<-observationStopped
		providerCause := context.Cause(providerContext)
		if providerCause == nil {
			latestState, stateErr := s.repo.LoadAgentCheckpoint(scope)
			switch {
			case stateErr != nil:
				providerCause = fmt.Errorf("%w: %v", errAgentRuntimeStateObservation, stateErr)
			case agentRuntimeRunTerminal(latestState.Status):
				providerCause = context.Canceled
			}
		}
		cancelProvider(nil)
		if providerCause != nil {
			requestErr = providerCause
		}
		if evidenceErr := s.persistAgentRuntimeProviderStreamEvidence(task, result, requestErr); evidenceErr != nil {
			return agentRuntimeModelTaskResult{}, evidenceErr
		}
		if requestErr != nil {
			if errors.Is(requestErr, context.Canceled) || errors.Is(requestErr, repository.ErrAgentMessageStreamClosed) {
				if config.MaxOutputTokens > 0 && (result.ProviderRequestID != "" || result.Usage.Available) {
					if evidenceErr := s.persistAgentRuntimeTokenBillingEvidence(task, result, "Agent 运行已停止，等待上游账单核对"); evidenceErr != nil {
						return agentRuntimeModelTaskResult{}, evidenceErr
					}
				}
				return agentRuntimeModelTaskResult{}, context.Canceled
			}
			if failErr := s.failAgentRuntimeVisibleMessage(scope, itemID, visibleMessage, agentRuntimeProviderStreamFailureCode(requestErr)); failErr != nil {
				return agentRuntimeModelTaskResult{}, failErr
			}
			if config.MaxOutputTokens > 0 && (result.ProviderRequestID != "" || result.Usage.Available) {
				if evidenceErr := s.persistAgentRuntimeTokenBillingEvidence(task, result, "Agent 响应协议失败，等待上游账单核对"); evidenceErr != nil {
					return agentRuntimeModelTaskResult{}, evidenceErr
				}
			}
			if errors.Is(requestErr, errKuaiziChatCompletionTextMissing) {
				return agentRuntimeModelTaskResult{}, errors.New("Agent 模型响应没有正文")
			}
			if errors.Is(requestErr, errAgentRuntimeStateObservation) {
				return agentRuntimeModelTaskResult{}, errAgentRuntimeStateObservation
			}
			return agentRuntimeModelTaskResult{}, errors.New("Agent 模型请求失败")
		}
		text = result.Text
		if _, finishErr := observer.Finish(); finishErr != nil {
			if failErr := s.failAgentRuntimeVisibleMessage(scope, itemID, visibleMessage, "model_decision_invalid"); failErr != nil {
				return agentRuntimeModelTaskResult{}, failErr
			}
			if config.MaxOutputTokens > 0 {
				if evidenceErr := s.persistAgentRuntimeTokenBillingEvidence(task, result, "Agent 决策校验失败，等待上游账单核对"); evidenceErr != nil {
					return agentRuntimeModelTaskResult{}, evidenceErr
				}
			}
			return agentRuntimeModelTaskResult{Mode: "text", DecisionFeedback: &agentruntime.ModelDecisionFeedback{Code: "model_decision_invalid", Reason: finishErr.Error()}}, nil
		}
		if config.MaxOutputTokens > 0 {
			if evidenceErr := s.persistAgentRuntimeTokenBillingEvidence(task, result, "等待 Agent 结果持久化后核对"); evidenceErr != nil {
				return agentRuntimeModelTaskResult{}, evidenceErr
			}
		}
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

func (s *Service) observeAgentRuntimeModelRequest(
	ctx context.Context,
	cancel context.CancelCauseFunc,
	stop <-chan struct{},
	stopped chan<- struct{},
	scope agentruntime.Scope,
) {
	defer close(stopped)
	ticker := time.NewTicker(agentRuntimeStateObservationInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			state, err := s.repo.LoadAgentCheckpoint(scope)
			if err != nil {
				cancel(fmt.Errorf("%w: %v", errAgentRuntimeStateObservation, err))
				return
			}
			if agentRuntimeRunTerminal(state.Status) {
				cancel(context.Canceled)
				return
			}
		}
	}
}

func (s *Service) beginAgentRuntimeModelRequest(scope agentruntime.Scope, expectedStep int) (agentruntime.RuntimeState, error) {
	state, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return agentruntime.RuntimeState{}, errors.New("Agent 模型任务检查点不可用")
	}
	if state.StepNumber != expectedStep {
		return agentruntime.RuntimeState{}, errors.New("Agent 模型任务步骤事实冲突")
	}
	if state.Status == agentruntime.RunRunning {
		return state, nil
	}
	transition, err := agentruntime.BeginModelRequest(state)
	if err != nil {
		return agentruntime.RuntimeState{}, errors.New("Agent 模型任务运行状态不可推进")
	}
	progress, err := s.commitAgentRuntimeState(scope, state, transition)
	if err != nil {
		return agentruntime.RuntimeState{}, errors.New("Agent 模型任务运行状态保存失败")
	}
	if progress.State.Status != agentruntime.RunRunning || progress.State.StepNumber != expectedStep {
		return agentruntime.RuntimeState{}, errors.New("Agent 模型任务运行状态冲突")
	}
	return progress.State, nil
}

func (s *Service) failAgentRuntimeVisibleMessage(scope agentruntime.Scope, itemID string, message string, failureCode string) error {
	if message == "" {
		return nil
	}
	if _, err := s.repo.FailAgentMessageStream(repository.FailAgentMessageStreamInput{
		Scope: scope, ItemID: itemID, Message: message, FailureCode: failureCode, Now: time.Now().UTC(),
	}); err != nil {
		return errors.New("Agent 可见消息失败事实保存失败")
	}
	return nil
}

func (s *Service) persistAgentRuntimeProviderStreamEvidence(task model.Task, result kuaiziChatCompletionResult, streamErr error) error {
	payload, err := json.Marshal(struct {
		ProviderRequestID string `json:"providerRequestId,omitempty"`
		FinishReason      string `json:"finishReason,omitempty"`
		UsageAvailable    bool   `json:"usageAvailable"`
		ErrorCode         string `json:"errorCode,omitempty"`
	}{ProviderRequestID: result.ProviderRequestID, FinishReason: result.FinishReason, UsageAvailable: result.Usage.Available, ErrorCode: agentRuntimeProviderStreamFailureCode(streamErr)})
	if err != nil || s.log(task.UserID, task.ID, "info", "Agent provider stream evidence", string(payload)) != nil {
		return errors.New("Agent 供应商流式事实保存失败")
	}
	return nil
}

func agentRuntimeProviderStreamFailureCode(streamErr error) string {
	switch {
	case errors.Is(streamErr, context.Canceled), errors.Is(streamErr, repository.ErrAgentMessageStreamClosed):
		return "agent_provider_stream_cancelled"
	case errors.Is(streamErr, errAgentProviderStreamTruncated):
		return "agent_provider_stream_truncated"
	case streamErr != nil:
		return "agent_provider_stream_failed"
	default:
		return ""
	}
}

func (s *Service) persistAgentRuntimeTokenBillingEvidence(task model.Task, result kuaiziChatCompletionResult, reason string) error {
	if result.ProviderRequestID != "" {
		if err := s.ScheduleTokenBillingReconciliation(task.BillingOrderID, result.ProviderRequestID, reason, result.Usage); err != nil {
			return errors.New("Agent 模型计费事实保存失败")
		}
		return nil
	}
	usageStatus, usage := normalizeTokenUsageFact(result.Usage)
	if err := s.repo.RecordTokenBillingUsage(task.BillingOrderID, repository.TokenUsageFact{InputTokens: usage.InputTokens, CachedTokens: usage.CachedTokens, OutputTokens: usage.OutputTokens}, usageStatus); err != nil {
		return errors.New("Agent 模型计费事实保存失败")
	}
	if err := s.MarkBillingUncertain(task.BillingOrderID, "上游响应缺少可核对的任务 ID"); err != nil {
		return errors.New("Agent 模型计费状态保存失败")
	}
	return nil
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
