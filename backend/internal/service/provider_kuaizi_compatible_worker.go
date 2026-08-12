package service

import (
	"context"
	"encoding/json"
	"errors"
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
	input.Config.Model = task.Model
	input.Config.InterfaceType = "ai-open-platform-video-volcengine"
	if err := s.hydrateGenerationMedia(task.UserID, &input, true); err != nil {
		return nil, err
	}
	runtime, err := s.repo.FrozenProviderRuntime(task)
	if err != nil {
		return nil, errors.New("读取筷子 Seedance 冻结运行配置失败")
	}
	apiKey, err := NewProviderSecretCipher(s.dataDir).Decrypt(runtime.ProviderAccountID, runtime.ProviderCredentialID, runtime.CredentialVersion, runtime.KeyCipher)
	if err != nil {
		return nil, errors.New("解密筷子 Seedance 冻结系列 Key 失败")
	}
	input.Config.BaseURL = runtime.BaseURL
	input.Config.APIKey = apiKey
	return runAIOpenPlatformVolcengineVideoTask(ctx, input)
}
