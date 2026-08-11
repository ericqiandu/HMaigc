package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"
	"infinite-canvas/backend/internal/model"
)

type TaskCommercialFacts struct {
	Task         *model.Task
	BillingOrder *model.BillingOrder
	ProviderFact *model.ProviderTaskFact
}

// ProviderTaskRuntime 只携带任务创建时冻结的版本事实；明文 Key 永不成为运行时字段。
type ProviderTaskRuntime struct {
	Task              model.Task
	BillingOrder      model.BillingOrder
	ProviderFact      model.ProviderTaskFact
	Account           model.ProviderAccount
	EndpointVersion   model.ProviderEndpointVersion
	Credential        model.ProviderCredential
	CredentialVersion model.ProviderCredentialVersion
	ChannelModel      model.ChannelModel
}

func (s *Service) processFrozenProviderCanvasTask(ctx context.Context, task model.Task, fact model.ProviderTaskFact) (map[string]interface{}, error) {
	if fact.TaskID != task.ID {
		return nil, errors.New("冻结上游任务事实与执行任务不一致")
	}
	var input canvasGenerationInput
	if err := json.Unmarshal([]byte(task.InputJSON), &input); err != nil {
		return nil, fmt.Errorf("冻结上游任务输入解析失败：%w", err)
	}
	if strings.TrimSpace(input.Config.BaseURL) != "" || strings.TrimSpace(input.Config.APIKey) != "" {
		return nil, errors.New("冻结上游任务输入禁止携带 Base URL 或 API Key")
	}
	if strings.TrimSpace(input.Prompt) == "" {
		input.Prompt = task.Prompt
	}
	runtime, err := s.resolveProviderTaskRuntime(task.ID)
	if err != nil {
		return nil, err
	}
	if input.Mode != "video" || input.Config.Model != runtime.ChannelModel.ModelKey {
		return nil, errors.New("冻结上游任务输入与模型能力不一致")
	}
	if err := s.hydrateGenerationMedia(task.UserID, &input, true); err != nil {
		return nil, err
	}
	environment := strings.TrimSpace(os.Getenv("CANVAS_ENVIRONMENT"))
	if runtime.ProviderFact.ProviderStatus != "succeeded" {
		if _, err := ValidateKuaiziBaseURL(ctx, runtime.EndpointVersion.BaseURL, environment); err != nil {
			return nil, fmt.Errorf("冻结筷子服务地址运行时校验失败：%w", err)
		}
	}
	client := NewKuaiziSeedance25Client(KuaiziHTTPClient(environment, providerHTTPTimeout), NewProviderSecretCipher(s.dataDir))
	providerResult, err := s.executeKuaiziSeedance25Task(ctx, runtime, input, client, 5*time.Second)
	if err != nil {
		return nil, err
	}
	providerTaskID, _ := providerResult["taskId"].(string)
	sourceURL, _ := providerResult["sourceUrl"].(string)
	data, mimeType, err := getExternalBinary(withProviderRequestKind(ctx, "download"), sourceURL)
	if err != nil {
		return nil, fmt.Errorf("Seedance 2.5 上游已成功但结果下载失败（任务 %s）：%w", providerTaskID, err)
	}
	mimeType = normalizedMediaMimeType(mimeType, data)
	video := map[string]interface{}{
		"dataUrl": dataURL(mimeType, data), "mimeType": mimeType, "taskId": providerTaskID, "sourceUrl": sourceURL,
		"lastFrameUrl": providerResult["lastFrameUrl"], "duration": providerResult["duration"], "totalTokens": providerResult["totalTokens"],
	}
	return map[string]interface{}{"mode": "video", "video": video}, nil
}

func (s *Service) buildTaskCommercialFacts(userID string, task *model.Task, input map[string]any) (TaskCommercialFacts, map[string]any, error) {
	order, err := s.taskBillingOrder(userID, task, input)
	if err != nil {
		return TaskCommercialFacts{}, nil, err
	}
	facts := TaskCommercialFacts{Task: task, BillingOrder: order}
	item, err := s.repo.ProviderChannelModelFact(order.ChannelModelID)
	if err != nil {
		return TaskCommercialFacts{}, nil, err
	}
	if strings.TrimSpace(item.ProviderCredentialID) == "" {
		return facts, input, nil
	}
	if item.ModelKey != "kuaizi-seedance-2.5" || item.Capability != "video" || order.Model != item.ModelKey {
		return TaskCommercialFacts{}, nil, BadAuthRequest("当前凭据绑定模型没有已实现的筷子运行时适配器")
	}
	credential, err := s.repo.ProviderCredential(item.ProviderCredentialID)
	if err != nil {
		return TaskCommercialFacts{}, nil, err
	}
	account, err := s.repo.ProviderAccountByKind(kuaiziProviderKind)
	if err != nil {
		return TaskCommercialFacts{}, nil, err
	}
	if credential.ProviderAccountID != account.ID {
		return TaskCommercialFacts{}, nil, errors.New("筷子模型凭据与账号事实不一致")
	}
	endpointVersions, err := s.repo.ProviderEndpointVersions(account.ID)
	if err != nil {
		return TaskCommercialFacts{}, nil, err
	}
	endpoint := activeEndpointVersion(endpointVersions)
	credentialVersions, err := s.repo.ProviderCredentialVersions(credential.ID)
	if err != nil {
		return TaskCommercialFacts{}, nil, err
	}
	credentialVersion := activeCredentialVersion(credentialVersions)
	if err := validateKuaiziSeedance25Availability(*account, endpoint, *credential, credentialVersion, *item); err != nil {
		return TaskCommercialFacts{}, nil, err
	}
	typedInput, err := decodeCanvasGenerationInput(input)
	if err != nil {
		return TaskCommercialFacts{}, nil, err
	}
	duration, resolution, err := s.validateKuaiziSeedance25CreationInput(userID, typedInput)
	if err != nil {
		return TaskCommercialFacts{}, nil, err
	}
	inputVariant := "standard"
	if len(typedInput.ReferenceVideos) > 0 {
		inputVariant = "reference_video"
	}
	order.PricingInputVariant = inputVariant
	fact := &model.ProviderTaskFact{
		TaskID: task.ID, BillingOrderID: order.ID, ProviderAccountID: account.ID,
		ProviderEndpointVersionID: endpoint.ID, ProviderCredentialID: credential.ID, ProviderCredentialVersionID: credentialVersion.ID,
		ChannelModelID: item.ID, RequestedDurationSeconds: duration, Resolution: resolution, InputVariant: inputVariant,
		InputImageCount: len(typedInput.ReferenceImages), InputVideoCount: len(typedInput.ReferenceVideos), InputAudioCount: len(typedInput.ReferenceAudios),
		ProviderStatus: "reserved", ReconciliationStatus: "pending", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	facts.ProviderFact = fact
	return facts, sanitizedProviderTaskInput(input), nil
}

func decodeCanvasGenerationInput(input map[string]any) (canvasGenerationInput, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return canvasGenerationInput{}, BadAuthRequest("任务输入格式无效")
	}
	var decoded canvasGenerationInput
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return canvasGenerationInput{}, BadAuthRequest("任务输入格式无效")
	}
	return decoded, nil
}

func (s *Service) validateKuaiziSeedance25CreationInput(userID string, input canvasGenerationInput) (int, string, error) {
	if input.Mode != "video" || input.Config.Model != "kuaizi-seedance-2.5" {
		return 0, "", BadAuthRequest("Seedance 2.5 任务能力或模型不匹配")
	}
	duration, err := kuaiziSeedance25Duration(input.Config.VideoSeconds)
	if err != nil {
		return 0, "", err
	}
	resolution, err := kuaiziSeedance25Resolution(input.Config.VQuality)
	if err != nil {
		return 0, "", err
	}
	if _, err := kuaiziSeedance25Ratio(input.Config.Size); err != nil {
		return 0, "", err
	}
	groups := []struct {
		kind  string
		limit int
		items []providerMedia
	}{{"image", 30, input.ReferenceImages}, {"video", 10, input.ReferenceVideos}, {"audio", 10, input.ReferenceAudios}}
	for _, group := range groups {
		if len(group.items) > group.limit {
			return 0, "", BadAuthRequest(fmt.Sprintf("Seedance 2.5 最多支持 %d 个%s参考素材", group.limit, group.kind))
		}
		for _, media := range group.items {
			if _, err := kuaiziSeedance25MediaRole(group.kind, media, input); err != nil {
				return 0, "", err
			}
			storageKey := strings.TrimSpace(media.StorageKey)
			if !strings.HasPrefix(storageKey, "resource:") {
				return 0, "", BadAuthRequest("Seedance 2.5 参考素材必须来自已授权的平台资源")
			}
			resourceID := strings.TrimSpace(strings.TrimPrefix(storageKey, "resource:"))
			if resourceID == "" {
				return 0, "", BadAuthRequest("Seedance 2.5 参考素材资源标识无效")
			}
			resource, err := s.repo.ResourceForUser(userID, resourceID)
			if err != nil {
				return 0, "", BadAuthRequest("Seedance 2.5 参考素材不存在或无权访问")
			}
			if resource.Status != "ready" || resource.Provider == "local" {
				return 0, "", BadAuthRequest("Seedance 2.5 参考素材必须已就绪且可生成公网签名地址")
			}
		}
	}
	if strings.TrimSpace(input.Prompt) == "" && len(input.ReferenceImages)+len(input.ReferenceVideos)+len(input.ReferenceAudios) == 0 {
		return 0, "", BadAuthRequest("Seedance 2.5 至少需要提示词或一个参考素材")
	}
	return duration, resolution, nil
}

func sanitizedProviderTaskInput(input map[string]any) map[string]any {
	encoded, _ := json.Marshal(input)
	var sanitized map[string]any
	_ = json.Unmarshal(encoded, &sanitized)
	removeProviderRuntimeFields(sanitized)
	return sanitized
}

func removeProviderRuntimeFields(value any) {
	switch item := value.(type) {
	case map[string]any:
		delete(item, "apiKey")
		delete(item, "baseUrl")
		for _, child := range item {
			removeProviderRuntimeFields(child)
		}
	case []any:
		for _, child := range item {
			removeProviderRuntimeFields(child)
		}
	}
}

func validateKuaiziSeedance25Availability(account model.ProviderAccount, endpoint *model.ProviderEndpointVersion, credential model.ProviderCredential, credentialVersion *model.ProviderCredentialVersion, item model.ChannelModel) error {
	if !account.Enabled {
		return BadAuthRequest("筷子账号未启用")
	}
	if endpoint == nil || endpoint.Status != "active" || strings.TrimSpace(endpoint.BaseURL) == "" {
		return BadAuthRequest("筷子服务地址没有有效活动版本")
	}
	if !credential.Enabled || credential.HealthStatus != "healthy" || credential.Family != "seedance" {
		return BadAuthRequest("筷子 Seedance 凭据未启用或健康状态不可用")
	}
	if credentialVersion == nil || credentialVersion.Status != "active" || strings.TrimSpace(credentialVersion.KeyCipher) == "" {
		return BadAuthRequest("筷子 Seedance 没有可用的活动 Key 版本")
	}
	if !item.Enabled || !item.PriceConfigured || !kuaiziSeedance25PricesPublished(item.PriceTiers) {
		return BadAuthRequest("Seedance 2.5 尚未发布完整的四档用户积分价格")
	}
	return nil
}

func (s *Service) kuaiziSeedance25ModelAvailable(item model.ChannelModel) (bool, error) {
	if strings.TrimSpace(item.ProviderCredentialID) == "" {
		return true, nil
	}
	if item.ModelKey != "kuaizi-seedance-2.5" || item.Capability != "video" {
		return false, nil
	}
	credential, err := s.repo.ProviderCredential(item.ProviderCredentialID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	account, err := s.repo.ProviderAccountByKind(kuaiziProviderKind)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if credential.ProviderAccountID != account.ID {
		return false, nil
	}
	endpointVersions, err := s.repo.ProviderEndpointVersions(account.ID)
	if err != nil {
		return false, err
	}
	credentialVersions, err := s.repo.ProviderCredentialVersions(credential.ID)
	if err != nil {
		return false, err
	}
	if err := validateKuaiziSeedance25Availability(*account, activeEndpointVersion(endpointVersions), *credential, activeCredentialVersion(credentialVersions), item); err != nil {
		return false, nil
	}
	return true, nil
}

func kuaiziSeedance25PricesPublished(tiers []model.ChannelModelPriceTier) bool {
	required := map[string]bool{"480P\nstandard": false, "480P\nreference_video": false, "720P\nstandard": false, "720P\nreference_video": false}
	for _, tier := range tiers {
		key := strings.ToUpper(strings.TrimSpace(tier.Resolution)) + "\n" + strings.TrimSpace(tier.InputVariant)
		if _, exists := required[key]; exists && tier.UnitPriceMicrocredits > 0 {
			required[key] = true
		}
	}
	for _, published := range required {
		if !published {
			return false
		}
	}
	return true
}

func (s *Service) resolveProviderTaskRuntime(taskID string) (*ProviderTaskRuntime, error) {
	task, err := s.repo.Task(taskID)
	if err != nil {
		return nil, err
	}
	fact, err := s.repo.ProviderTaskFact(taskID)
	if err != nil {
		return nil, err
	}
	order, err := s.repo.BillingOrder(fact.BillingOrderID)
	if err != nil {
		return nil, err
	}
	account, err := s.repo.ProviderAccount(fact.ProviderAccountID)
	if err != nil {
		return nil, err
	}
	endpoint, err := s.repo.ProviderEndpointVersion(fact.ProviderEndpointVersionID)
	if err != nil {
		return nil, err
	}
	credential, err := s.repo.ProviderCredential(fact.ProviderCredentialID)
	if err != nil {
		return nil, err
	}
	credentialVersion, err := s.repo.ProviderCredentialVersion(fact.ProviderCredentialVersionID)
	if err != nil {
		return nil, err
	}
	channelModel, err := s.repo.ProviderChannelModelFact(fact.ChannelModelID)
	if err != nil {
		return nil, err
	}
	runtime := &ProviderTaskRuntime{Task: *task, BillingOrder: *order, ProviderFact: *fact, Account: *account, EndpointVersion: *endpoint, Credential: *credential, CredentialVersion: *credentialVersion, ChannelModel: *channelModel}
	if err := validateFrozenProviderRuntime(runtime); err != nil {
		return nil, err
	}
	return runtime, nil
}

func validateFrozenProviderRuntime(runtime *ProviderTaskRuntime) error {
	if runtime.Task.ID == "" || runtime.ProviderFact.TaskID != runtime.Task.ID || runtime.ProviderFact.BillingOrderID != runtime.BillingOrder.ID || runtime.BillingOrder.TaskID != runtime.Task.ID || runtime.Task.BillingOrderID != runtime.BillingOrder.ID {
		return errors.New("冻结上游任务、计费与本地任务事实不一致")
	}
	if runtime.ProviderFact.ProviderAccountID != runtime.Account.ID || runtime.EndpointVersion.ProviderAccountID != runtime.Account.ID || runtime.Credential.ProviderAccountID != runtime.Account.ID {
		return errors.New("冻结上游账号或地址事实不一致")
	}
	if runtime.ProviderFact.ProviderCredentialID != runtime.Credential.ID || runtime.ProviderFact.ProviderCredentialVersionID != runtime.CredentialVersion.ID || runtime.CredentialVersion.ProviderCredentialID != runtime.Credential.ID {
		return errors.New("冻结上游凭据事实不一致")
	}
	if runtime.ProviderFact.ChannelModelID != runtime.ChannelModel.ID || runtime.ChannelModel.ProviderCredentialID != runtime.Credential.ID || runtime.BillingOrder.ChannelModelID != runtime.ChannelModel.ID {
		return errors.New("冻结上游模型事实不一致")
	}
	if runtime.Task.Model != runtime.ChannelModel.ModelKey || runtime.BillingOrder.Model != runtime.ChannelModel.ModelKey || runtime.ChannelModel.ModelKey != "kuaizi-seedance-2.5" || runtime.ChannelModel.Capability != "video" {
		return errors.New("冻结上游模型或能力事实不一致")
	}
	if strings.TrimSpace(runtime.EndpointVersion.BaseURL) == "" || strings.TrimSpace(runtime.CredentialVersion.KeyCipher) == "" {
		return errors.New("冻结上游地址或凭据密文缺失")
	}
	return nil
}

func (s *Service) executeKuaiziSeedance25Task(ctx context.Context, runtime *ProviderTaskRuntime, input canvasGenerationInput, client *KuaiziSeedance25Client, pollInterval time.Duration) (map[string]any, error) {
	switch runtime.ProviderFact.ProviderStatus {
	case "create_uncertain":
		return nil, &KuaiziSeedance25CreateUncertainError{Cause: errors.New("stored create uncertainty")}
	case "create_failed":
		return nil, &KuaiziSeedance25Error{Stage: "create", Kind: "definitive_rejection", TraceID: runtime.ProviderFact.CreateTraceID, Message: "上游已明确拒绝创建任务"}
	case "failed":
		return nil, &KuaiziSeedance25Error{Stage: "poll", Kind: "provider_failed", TraceID: runtime.ProviderFact.LastPollTraceID, Message: "上游任务已失败"}
	case "succeeded":
		if err := validateStoredKuaiziSeedance25Success(runtime.ProviderFact); err != nil {
			return nil, err
		}
		return kuaiziSeedance25StoredResult(runtime.ProviderFact), nil
	}
	providerTaskID := strings.TrimSpace(runtime.ProviderFact.ProviderTaskID)
	if providerTaskID == "" {
		if runtime.ProviderFact.ProviderStatus == "creating" {
			uncertain := &KuaiziSeedance25CreateUncertainError{Cause: errors.New("worker recovered after create boundary")}
			if persistErr := s.repo.MarkProviderTaskCreateUncertain(runtime.Task.ID, "上游创建边界中断，结果不确定"); persistErr != nil {
				return nil, errors.Join(uncertain, persistErr)
			}
			return nil, uncertain
		}
		if err := s.repo.MarkProviderTaskCreateStarted(runtime.Task.ID); err != nil {
			return nil, err
		}
		runtime.ProviderFact.ProviderStatus = "creating"
		created, err := client.Create(ctx, runtime, input)
		if err != nil {
			var uncertain *KuaiziSeedance25CreateUncertainError
			if errors.As(err, &uncertain) {
				if persistErr := s.repo.MarkProviderTaskCreateUncertain(runtime.Task.ID, "上游创建结果不确定"); persistErr != nil {
					return nil, errors.Join(err, persistErr)
				}
				return nil, err
			}
			var providerErr *KuaiziSeedance25Error
			traceID := ""
			if errors.As(err, &providerErr) {
				traceID = providerErr.TraceID
			}
			if persistErr := s.repo.MarkProviderTaskCreateFailed(runtime.Task.ID, traceID); persistErr != nil {
				return nil, errors.Join(err, persistErr)
			}
			return nil, err
		}
		if err := s.repo.SaveProviderTaskCreation(runtime.Task.ID, created.TaskID, created.TraceID); err != nil {
			return nil, err
		}
		providerTaskID = created.TaskID
		runtime.ProviderFact.ProviderTaskID = created.TaskID
		runtime.ProviderFact.CreateTraceID = created.TraceID
		runtime.ProviderFact.ProviderStatus = "submitted"
	}
	polled, err := client.PollUntilTerminal(ctx, runtime, providerTaskID, pollInterval, func(observed KuaiziSeedance25Polled) error {
		return s.repo.UpdateProviderTaskPoll(runtime.Task.ID, observed.State.ProviderStatus, observed.TraceID)
	})
	if err != nil {
		var providerErr *KuaiziSeedance25Error
		traceID := ""
		if errors.As(err, &providerErr) {
			traceID = providerErr.TraceID
		}
		if persistErr := s.repo.MarkProviderTaskPollUncertain(runtime.Task.ID, traceID, "上游轮询结果不确定"); persistErr != nil {
			return nil, errors.Join(err, persistErr)
		}
		return nil, err
	}
	if !polled.State.Succeeded {
		return nil, &KuaiziSeedance25Error{Stage: "poll", Kind: "provider_failed", TraceID: polled.TraceID, Message: polled.State.FailureReason}
	}
	if err := s.repo.SaveProviderTaskSuccess(runtime.Task.ID, polled.State.ProviderStatus, polled.TraceID, polled.State.AssetSourceURL, polled.State.LastFrameURL, polled.State.ActualDurationSeconds, polled.State.TotalTokens); err != nil {
		return nil, err
	}
	return map[string]any{
		"taskId": providerTaskID, "sourceUrl": polled.State.AssetSourceURL, "lastFrameUrl": polled.State.LastFrameURL,
		"duration": polled.State.ActualDurationSeconds, "totalTokens": polled.State.TotalTokens,
	}, nil
}

func validateStoredKuaiziSeedance25Success(fact model.ProviderTaskFact) error {
	if strings.TrimSpace(fact.ProviderTaskID) == "" || strings.TrimSpace(fact.AssetSourceURL) == "" || strings.TrimSpace(fact.LastFrameURL) == "" ||
		fact.ActualDurationSeconds < 4 || fact.ActualDurationSeconds > 30 || strings.TrimSpace(fact.TotalTokens) == "" {
		return &KuaiziSeedance25Error{Stage: "recovery", Kind: "invalid_success_fact", Message: "已成功的上游任务缺少完整冻结事实"}
	}
	return nil
}

func kuaiziSeedance25StoredResult(fact model.ProviderTaskFact) map[string]any {
	return map[string]any{
		"taskId": fact.ProviderTaskID, "sourceUrl": fact.AssetSourceURL, "lastFrameUrl": fact.LastFrameURL,
		"duration": fact.ActualDurationSeconds, "totalTokens": fact.TotalTokens,
	}
}
