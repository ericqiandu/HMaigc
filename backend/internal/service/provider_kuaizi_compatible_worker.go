package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"infinite-canvas/backend/internal/model"
)

func (s *Service) processKuaiziCompatibleTask(ctx context.Context, task model.Task) (map[string]interface{}, error) {
	var input canvasGenerationInput
	if err := json.Unmarshal([]byte(task.InputJSON), &input); err != nil {
		return nil, fmt.Errorf("任务输入解析失败：%w", err)
	}
	if strings.TrimSpace(input.Prompt) == "" {
		input.Prompt = task.Prompt
	}
	watermark, err := taskWatermarkRuntimeFromTask(task)
	if err != nil {
		return nil, err
	}
	input.Watermark = watermark
	input.Config.Model = task.Model
	if resumedProviderRequestID(ctx) == "" {
		if err := s.hydrateGenerationMedia(task.UserID, &input, true); err != nil {
			return nil, err
		}
	}
	runtime, err := s.repo.FrozenProviderRuntime(task)
	if err != nil {
		return nil, fmt.Errorf("读取筷子科技冻结运行配置失败：%w", err)
	}
	apiKey, err := NewProviderSecretCipher(s.dataDir).Decrypt(runtime.ProviderAccountID, runtime.ProviderCredentialID, runtime.CredentialVersion, runtime.KeyCipher)
	if err != nil {
		return nil, fmt.Errorf("解密筷子科技冻结系列 Key 失败：%w", err)
	}
	input.Config.BaseURL = runtime.BaseURL
	input.Config.APIKey = apiKey
	family, spec, ok := kuaiziProviderFamilyForModel(task.Model)
	if !ok {
		return nil, fmt.Errorf("筷子科技模型未登记：%s", task.Model)
	}
	switch spec.Capability {
	case "video":
		input.Config.InterfaceType = "ai-open-platform-video-volcengine"
		return runAIOpenPlatformVolcengineVideoTask(ctx, input)
	case "image":
		input.Mode = "image"
		switch family {
		case "gpt-image2":
			return runKuaiziGPTImage2Task(ctx, input)
		case "seedream":
			return runKuaiziSeedreamTask(ctx, input)
		default:
			return nil, fmt.Errorf("筷子科技图片模型系列未实现：%s", family)
		}
	case "text":
		input.Mode = "text"
		input.Config.InterfaceType = string(model.ChannelInterfaceChatCompletion)
		input.Config.BaseURL = kuaiziChatCompletionsBaseURL(runtime.BaseURL)
		return runKuaiziChatCompletionsTask(ctx, input)
	default:
		return nil, fmt.Errorf("筷子科技模型能力未实现：%s", spec.Capability)
	}
}
