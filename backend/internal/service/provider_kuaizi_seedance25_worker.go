package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
)

const kuaiziSeedance25PollInterval = 5 * time.Second

func (s *Service) processKuaiziSeedance25Task(ctx context.Context, task model.Task) (map[string]interface{}, error) {
	var input canvasGenerationInput
	if err := json.Unmarshal([]byte(task.InputJSON), &input); err != nil {
		return nil, fmt.Errorf("任务输入解析失败：%w", err)
	}
	if strings.TrimSpace(input.Prompt) == "" {
		input.Prompt = task.Prompt
	}
	input.Config.Model = "kuaizi-seedance-2.5"
	if err := s.hydrateGenerationMedia(task.UserID, &input, true); err != nil {
		return nil, err
	}
	runtime, err := s.repo.FrozenProviderRuntime(task)
	if err != nil {
		return nil, errors.New("读取筷子 Seedance 2.5 冻结运行配置失败")
	}
	apiKey, err := NewProviderSecretCipher(s.dataDir).Decrypt(runtime.ProviderAccountID, runtime.ProviderCredentialID, runtime.CredentialVersion, runtime.KeyCipher)
	if err != nil {
		return nil, errors.New("解密筷子 Seedance 2.5 冻结系列 Key 失败")
	}
	request, err := newKuaiziSeedance25Request(input)
	if err != nil {
		return nil, err
	}
	httpClient := KuaiziHTTPClient(strings.TrimSpace(os.Getenv("CANVAS_ENVIRONMENT")), providerHTTPTimeout)
	defer httpClient.CloseIdleConnections()
	client := NewKuaiziSeedance25Client(httpClient)
	taskID := strings.TrimSpace(task.ProviderRequestID)
	if taskID == "" {
		created, createErr := client.Create(ctx, runtime.BaseURL, apiKey, request)
		if logErr := s.recordKuaiziSeedance25Call(task, "create", created.TaskID, createErr); logErr != nil {
			return nil, logErr
		}
		if createErr != nil {
			return nil, createErr
		}
		taskID = created.TaskID
	}
	for deadline := providerPollingDeadline(ctx); time.Now().Before(deadline); {
		state, statusErr := client.Status(ctx, runtime.BaseURL, apiKey, taskID)
		if logErr := s.recordKuaiziSeedance25Call(task, "poll", taskID, statusErr); logErr != nil {
			return nil, logErr
		}
		if statusErr != nil {
			return nil, statusErr
		}
		switch state.Status {
		case "succeeded":
			data, mimeType, downloadErr := getExternalBinary(withProviderRequestKind(ctx, "download"), state.VideoURL)
			if downloadErr != nil {
				return nil, fmt.Errorf("筷子 Seedance 2.5 视频结果下载失败：%w", downloadErr)
			}
			mimeType = normalizedMediaMimeType(mimeType, data)
			video := map[string]interface{}{"dataUrl": dataURL(mimeType, data), "mimeType": mimeType, "taskId": taskID, "sourceUrl": state.VideoURL, "durationMs": state.Duration * 1000}
			if state.LastFrameURL != "" {
				video["lastFrameUrl"] = state.LastFrameURL
			}
			return map[string]interface{}{"mode": "video", "video": video}, nil
		case "failed":
			return nil, errors.New("筷子 Seedance 2.5 上游任务失败")
		case "submitted", "pending", "running":
		}
		if err := sleepContext(ctx, kuaiziSeedance25PollInterval); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("筷子 Seedance 2.5 生成超时（任务 %s）", taskID)
}

func (s *Service) recordKuaiziSeedance25Call(task model.Task, requestKind string, providerRequestID string, requestErr error) error {
	status := model.ApiCallStatusSucceeded
	errorText := ""
	if requestErr != nil {
		status = model.ApiCallStatusFailed
		errorText = safeProviderLogError(requestErr)
	}
	return s.LogAPICall(model.ApiCallLog{
		UserID: task.UserID, TaskID: task.ID, BillingOrderID: task.BillingOrderID,
		Source: "backend-task", Capability: "video", Operation: task.Operation,
		RequestKind: requestKind, Billable: requestKind == "create", APIFormat: "kuaizi",
		Method: http.MethodPost, Model: task.Model, ProviderRequestID: providerRequestID,
		Status: status, StatusCode: http.StatusOK, Error: errorText,
	})
}
