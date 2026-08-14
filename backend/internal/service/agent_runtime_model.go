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
)

type agentRuntimeModelTaskResult struct {
	Mode string `json:"mode"`
	Text string `json:"text"`
}

func (s *Service) processAgentRuntimeModelText(ctx context.Context, task model.Task) (string, error) {
	var input agentRuntimeModelTaskInput
	decoder := json.NewDecoder(bytes.NewBufferString(task.InputJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return "", errors.New("Agent 模型任务输入格式无效")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || input.Mode != "text" || strings.TrimSpace(input.Prompt) == "" {
		return "", errors.New("Agent 模型任务输入事实无效")
	}
	config, err := s.resolveTextTaskProviderConfig(task, input.Config)
	if err != nil {
		return "", errors.New("Agent 冻结模型配置不可用")
	}
	result, err := runTextTask(ctx, canvasGenerationInput{Mode: "text", Prompt: input.Prompt, Config: config})
	if err != nil {
		return "", errors.New("Agent 模型请求失败")
	}
	text, _ := result["text"].(string)
	text = strings.TrimSpace(text)
	if _, err := agentruntime.ParseModelDecision([]byte(text)); err != nil {
		return "", err
	}
	return text, nil
}

func parseAgentRuntimeModelTaskResult(raw string) (agentruntime.ModelDecision, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	var result agentRuntimeModelTaskResult
	if err := decoder.Decode(&result); err != nil {
		return agentruntime.ModelDecision{}, errors.New("Agent 模型任务结果格式无效")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || result.Mode != "text" || strings.TrimSpace(result.Text) == "" {
		return agentruntime.ModelDecision{}, errors.New("Agent 模型任务结果事实无效")
	}
	return agentruntime.ParseModelDecision([]byte(result.Text))
}
