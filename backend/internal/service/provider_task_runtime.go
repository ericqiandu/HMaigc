package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
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

func (s *Service) finalizeProviderExecutionFailure(task model.Task, cause error) error {
	fact, err := s.repo.ProviderTaskFact(task.ID)
	if err != nil {
		return err
	}
	lease := repository.ProviderTaskLease{Owner: task.LeaseOwner, Token: task.LeaseToken}
	message := taskFailureMessage(cause)
	resolution := repository.ProviderTaskFailureResolution{
		ExpectedStatuses: []string{fact.ProviderStatus}, TaskStage: "任务失败", TaskError: message,
	}
	var providerErr *KuaiziSeedance25Error
	if errors.As(cause, &providerErr) {
		resolution.TraceID = providerErr.TraceID
	}
	switch fact.ProviderStatus {
	case "reserved", "execution_claimed":
		resolution.ProviderStatus = "execution_failed"
		resolution.ReconciliationStatus = "resolved"
		return s.repo.FinalizeProviderTaskRefund(task.ID, lease, resolution)
	case "creating":
		definitiveCreateRejection := false
		if errors.As(cause, &providerErr) && providerErr.Stage == "create" && providerErr.Kind == "http" {
			if statusCode, parseErr := strconv.Atoi(providerErr.Code); parseErr == nil {
				definitiveCreateRejection = kuaiziCreateHTTPDefinitiveRejection(statusCode)
			}
		}
		if definitiveCreateRejection {
			resolution.ProviderStatus = "create_failed"
			resolution.ReconciliationStatus = "resolved"
			return s.repo.FinalizeProviderTaskRefund(task.ID, lease, resolution)
		}
		resolution.ProviderStatus = "create_uncertain"
		resolution.ReconciliationStatus = "manual_review"
		return s.repo.FinalizeProviderTaskUncertain(task.ID, lease, resolution)
	case "submitted", "pending", "running", "poll_uncertain":
		resolution.ProviderStatus = "poll_uncertain"
		if errors.As(cause, &providerErr) && providerErr.Stage == "poll" && providerErr.Kind == "provider_failed" {
			resolution.ProviderStatus = "failed"
		}
		resolution.ReconciliationStatus = "manual_review"
		return s.repo.FinalizeProviderTaskUncertain(task.ID, lease, resolution)
	case "create_failed":
		resolution.ProviderStatus = "create_failed"
		resolution.ReconciliationStatus = "resolved"
		return s.repo.FinalizeProviderTaskRefund(task.ID, lease, resolution)
	case "create_uncertain":
		resolution.ProviderStatus = "create_uncertain"
		resolution.ReconciliationStatus = "manual_review"
		return s.repo.FinalizeProviderTaskUncertain(task.ID, lease, resolution)
	case "failed":
		resolution.ProviderStatus = "failed"
		resolution.ReconciliationStatus = "manual_review"
		return s.repo.FinalizeProviderTaskUncertain(task.ID, lease, resolution)
	default:
		return fmt.Errorf("provider failure cannot finalize from status %s", fact.ProviderStatus)
	}
}

func (s *Service) processFrozenProviderCanvasTask(ctx context.Context, task model.Task, fact model.ProviderTaskFact) (map[string]interface{}, error) {
	if fact.TaskID != task.ID {
		return nil, errors.New("冻结上游任务事实与执行任务不一致")
	}
	if fact.ProviderStatus == "succeeded" {
		return s.processSucceededProviderTask(ctx, task, fact)
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
	lastFrameURL, _ := providerResult["lastFrameUrl"].(string)
	return s.downloadKuaiziSeedance25Result(ctx, providerTaskID, sourceURL, lastFrameURL, providerResult["duration"], providerResult["totalTokens"])
}

func (s *Service) processSucceededProviderTask(ctx context.Context, task model.Task, fact model.ProviderTaskFact) (map[string]interface{}, error) {
	order, err := s.repo.BillingOrder(fact.BillingOrderID)
	if err != nil {
		return nil, err
	}
	if fact.TaskID != task.ID || order.ID != task.BillingOrderID || order.TaskID != task.ID {
		return nil, errors.New("上游成功恢复的任务与计费事实不一致")
	}
	if err := validateStoredKuaiziSeedance25Success(fact); err != nil {
		return nil, err
	}
	sensitiveValues := []string{task.Prompt}
	providerTaskID, err := safeKuaiziSeedance25TaskID(fact.ProviderTaskID, sensitiveValues)
	if err != nil || containsKuaiziSensitiveURLValue(fact.AssetSourceURL, sensitiveValues) || containsKuaiziSensitiveURLValue(fact.LastFrameURL, sensitiveValues) {
		return nil, &KuaiziSeedance25Error{Stage: "recovery", Kind: "unsafe_success_fact", Message: "冻结成功事实包含不安全字段"}
	}
	if resource, resourceErr := s.repo.ResourceForSourceTask(task.UserID, task.ID); resourceErr == nil {
		if resource.Status == model.ResourceStatusReady {
			return kuaiziSeedance25ResourceResult(resource, providerTaskID, fact.LastFrameURL, fact.ActualDurationSeconds, fact.TotalTokens), nil
		}
	} else if !errors.Is(resourceErr, gorm.ErrRecordNotFound) {
		return nil, resourceErr
	}
	return s.downloadKuaiziSeedance25Result(ctx, providerTaskID, fact.AssetSourceURL, fact.LastFrameURL, fact.ActualDurationSeconds, fact.TotalTokens)
}

func (s *Service) downloadKuaiziSeedance25Result(ctx context.Context, providerTaskID string, sourceURL string, lastFrameURL string, duration any, totalTokens any) (map[string]interface{}, error) {
	data, mimeType, err := getStrictExternalBinary(withProviderRequestKind(ctx, "download"), sourceURL)
	if err != nil {
		return nil, fmt.Errorf("Seedance 2.5 上游已成功但结果下载失败（任务 %s）：%w", providerTaskID, err)
	}
	mimeType = normalizedMediaMimeType(mimeType, data)
	video := map[string]interface{}{
		"dataUrl": dataURL(mimeType, data), "mimeType": mimeType, "taskId": providerTaskID,
		"lastFrameUrl": lastFrameURL, "duration": duration, "totalTokens": totalTokens,
	}
	return map[string]interface{}{"mode": "video", "video": video}, nil
}

func (s *Service) persistKuaiziSeedance25Resource(task model.Task, result map[string]interface{}) (map[string]interface{}, error) {
	if resource, err := s.repo.ResourceForSourceTask(task.UserID, task.ID); err == nil {
		if resource.Status == model.ResourceStatusReady {
			fact, factErr := s.repo.ProviderTaskFact(task.ID)
			if factErr != nil {
				return nil, factErr
			}
			return kuaiziSeedance25ResourceResult(resource, fact.ProviderTaskID, fact.LastFrameURL, fact.ActualDurationSeconds, fact.TotalTokens), nil
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	video, ok := result["video"].(map[string]interface{})
	if !ok {
		return nil, errors.New("Seedance 2.5 成功结果缺少视频对象")
	}
	raw := inlineMediaValue(video)
	if raw == "" {
		return nil, errors.New("Seedance 2.5 成功结果缺少内联视频")
	}
	mimeType, data, err := s.decodeDataURL(raw)
	if err != nil {
		return nil, err
	}
	resource, err := s.prepareSourceTaskResource(task.UserID, task.ID, "video", "generated."+extensionFromMimeType(mimeType), mimeType, int64(len(data)), 0, 0, int64(intValue(video["durationMs"])))
	if err != nil {
		return nil, err
	}
	if resource.Size != int64(len(data)) || resource.MimeType != mimeType {
		return nil, errors.New("Seedance 2.5 任务资源幂等事实与下载内容不一致")
	}
	if err := s.reservePreparedGeneratedResourceQuota(task.UserID, resource); err != nil {
		return nil, err
	}
	writeToken, err := newResourceWriteToken()
	if err != nil {
		return nil, err
	}
	resource, err = s.repo.ClaimSourceTaskResourceWrite(task.UserID, task.ID, task.LeaseOwner, task.LeaseToken, writeToken, resourceWriteObjectKey(resource, writeToken), 2*time.Minute)
	if err != nil {
		return nil, err
	}
	resource, err = s.writeClaimedSourceTaskResource(task.UserID, resource, task.LeaseOwner, task.LeaseToken, writeToken, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("Seedance 2.5 生成内容写入资源存储失败：%w", err)
	}
	applyGeneratedResource(video, raw, resource)
	return result, nil
}

func kuaiziSeedance25ResourceResult(resource *model.Resource, providerTaskID string, lastFrameURL string, duration any, totalTokens any) map[string]interface{} {
	video := map[string]interface{}{
		"dataUrl": "", "mimeType": resource.MimeType, "taskId": providerTaskID,
		"lastFrameUrl": lastFrameURL, "duration": duration, "totalTokens": totalTokens,
	}
	applyGeneratedResource(video, "", resource)
	return map[string]interface{}{"mode": "video", "video": video}
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
	lease := repository.ProviderTaskLease{Owner: runtime.Task.LeaseOwner, Token: runtime.Task.LeaseToken}
	switch runtime.ProviderFact.ProviderStatus {
	case "create_uncertain":
		return nil, &KuaiziSeedance25CreateUncertainError{Cause: errors.New("stored create uncertainty")}
	case "create_failed":
		return nil, &KuaiziSeedance25Error{Stage: "create", Kind: "definitive_rejection", TraceID: runtime.ProviderFact.CreateTraceID, Message: "上游已明确拒绝创建任务"}
	case "failed":
		return nil, &KuaiziSeedance25Error{Stage: "poll", Kind: "provider_failed", TraceID: runtime.ProviderFact.LastPollTraceID, Message: "上游任务已失败"}
	}
	providerTaskID := strings.TrimSpace(runtime.ProviderFact.ProviderTaskID)
	if providerTaskID == "" {
		if runtime.ProviderFact.ProviderStatus == "creating" {
			latest, latestErr := s.repo.ProviderTaskFact(runtime.Task.ID)
			if latestErr != nil {
				return nil, latestErr
			}
			if latest.ExecutionLeaseToken == lease.Token && latest.ProviderTaskID != "" && repository.ProviderExecutionStatus(latest.ProviderStatus) {
				runtime.ProviderFact = *latest
				providerTaskID = latest.ProviderTaskID
			} else {
				return nil, &KuaiziSeedance25CreateUncertainError{Cause: errors.New("worker recovered after create boundary")}
			}
		}
		if providerTaskID == "" {
			if err := s.repo.MarkProviderTaskCreateStartedForLease(runtime.Task.ID, lease); err != nil {
				return nil, err
			}
			runtime.ProviderFact.ProviderStatus = "creating"
			created, err := client.Create(ctx, runtime, input)
			if err != nil {
				return nil, err
			}
			creation, err := s.repo.SaveProviderTaskCreationForLease(runtime.Task.ID, lease, created.TaskID, created.TraceID)
			if err != nil {
				return nil, err
			}
			if creation.HandedOff {
				return nil, errors.New("provider create response handed off to a newer task generation")
			}
			providerTaskID = created.TaskID
			runtime.ProviderFact.ProviderTaskID = created.TaskID
			runtime.ProviderFact.CreateTraceID = created.TraceID
			runtime.ProviderFact.ProviderStatus = "submitted"
		}
	}
	polled, err := client.PollUntilTerminal(ctx, runtime, providerTaskID, pollInterval, func(observed KuaiziSeedance25Polled) error {
		if observed.State.Terminal {
			return nil
		}
		return s.repo.UpdateProviderTaskPollForLease(runtime.Task.ID, lease, observed.State.ProviderStatus, observed.TraceID)
	})
	if err != nil {
		return nil, err
	}
	if !polled.State.Succeeded {
		return nil, &KuaiziSeedance25Error{Stage: "poll", Kind: "provider_failed", TraceID: polled.TraceID, Message: polled.State.FailureReason}
	}
	if err := s.repo.SaveProviderTaskSuccessForLease(runtime.Task.ID, lease, polled.TraceID, polled.State.AssetSourceURL, polled.State.LastFrameURL, polled.State.ActualDurationSeconds, polled.State.TotalTokens); err != nil {
		return nil, err
	}
	return map[string]any{
		"taskId": providerTaskID, "sourceUrl": polled.State.AssetSourceURL, "lastFrameUrl": polled.State.LastFrameURL,
		"duration": polled.State.ActualDurationSeconds, "totalTokens": polled.State.TotalTokens,
	}, nil
}

func validateStoredKuaiziSeedance25Success(fact model.ProviderTaskFact) error {
	if strings.TrimSpace(fact.ProviderTaskID) == "" || strings.TrimSpace(fact.AssetSourceURL) == "" || strings.TrimSpace(fact.LastFrameURL) == "" ||
		fact.ActualDurationSeconds < 4 || fact.ActualDurationSeconds > 30 || validateKuaiziSeedance25Decimal(strings.TrimSpace(fact.TotalTokens)) != nil {
		return &KuaiziSeedance25Error{Stage: "recovery", Kind: "invalid_success_fact", Message: "已成功的上游任务缺少完整冻结事实"}
	}
	if _, err := validateKuaiziSeedance25OutputURL(fact.AssetSourceURL, "视频"); err != nil {
		return &KuaiziSeedance25Error{Stage: "recovery", Kind: "invalid_success_fact", Message: "冻结视频地址不符合安全契约", Cause: err}
	}
	if _, err := validateKuaiziSeedance25OutputURL(fact.LastFrameURL, "尾帧"); err != nil {
		return &KuaiziSeedance25Error{Stage: "recovery", Kind: "invalid_success_fact", Message: "冻结尾帧地址不符合安全契约", Cause: err}
	}
	return nil
}
