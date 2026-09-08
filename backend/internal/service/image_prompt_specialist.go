package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
)

const (
	canvasImageCommandVersion     = "canvas-image-command/v1"
	imagePromptEvidenceResultKind = "image_prompt_evidence"
)

type canvasImagePromptCommandV1 struct {
	SchemaVersion string                 `json:"schemaVersion"`
	Command       string                 `json:"command"`
	Objective     string                 `json:"objective"`
	Parameters    map[string]interface{} `json:"parameters"`
	SourceNodeID  string                 `json:"sourceNodeId"`
	NodeRevision  int                    `json:"nodeRevision"`
}

type imagePromptSpecialistEvidenceResolver func(model.Task, model.BillingOrder, canvasGenerationInput) (imagePromptSpecialistEvidenceV1, error)

type imagePromptTaskEvidenceV1 struct {
	SchemaVersion  string                          `json:"schemaVersion"`
	Command        canvasImagePromptCommandV1      `json:"command"`
	Response       imagePromptSpecialistResponseV1 `json:"response"`
	ResponseDigest string                          `json:"responseDigest"`
}

func decodeCanvasImagePromptCommand(input map[string]interface{}) (*canvasImagePromptCommandV1, bool, error) {
	raw, present := input["imagePromptCommand"]
	if !present {
		return nil, false, nil
	}
	if prompt, ok := input["prompt"].(string); ok && strings.TrimSpace(prompt) != "" {
		return nil, true, BadAuthRequest("结构化图片命令不得携带浏览器生成的最终提示词")
	}
	if _, present := input["promptModel"]; present {
		return nil, true, BadAuthRequest("提示词模型绑定只能由服务端冻结")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, true, BadAuthRequest("图片命令格式无效")
	}
	var command canvasImagePromptCommandV1
	if err := decodeStrictJSON(encoded, &command); err != nil {
		return nil, true, BadAuthRequest("图片命令格式无效：" + err.Error())
	}
	if err := validateCanvasImagePromptCommand(command); err != nil {
		return nil, true, BadAuthRequest(err.Error())
	}
	return &command, true, nil
}

func validateCanvasImagePromptCommand(command canvasImagePromptCommandV1) error {
	if command.SchemaVersion != canvasImageCommandVersion {
		return errors.New("图片命令版本无效")
	}
	if command.Command != imagePromptCommandPortraitTexture {
		return errors.New("当前图片提示词 Specialist 只支持 portraitTexture")
	}
	if !boundedTrimmed(command.Objective, 1000) {
		return errors.New("图片命令目标无效")
	}
	if !boundedTrimmed(command.SourceNodeID, 160) || command.NodeRevision < 0 {
		return errors.New("图片命令来源节点事实无效")
	}
	if len(command.Parameters) > 32 {
		return errors.New("图片命令参数超过 32 项")
	}
	for key, value := range command.Parameters {
		if !boundedTrimmed(key, 80) || !imagePromptScalar(value) {
			return errors.New("图片命令参数必须是有限的标量值")
		}
	}
	return nil
}

func resolveCanvasImageTaskPrompt(prompt string, commandObjective string, commandPresent bool) (string, error) {
	prompt = strings.TrimSpace(prompt)
	if commandPresent {
		if prompt != "" {
			return "", BadAuthRequest("结构化图片命令不得携带浏览器生成的最终提示词")
		}
		if !boundedTrimmed(commandObjective, 1000) {
			return "", BadAuthRequest("图片命令目标无效")
		}
		return commandObjective, nil
	}
	if prompt == "" {
		return "", errors.New("prompt is required")
	}
	return prompt, nil
}

func (s *Service) freezeCanvasImagePromptCommand(userID string, taskType string, input map[string]interface{}) (string, bool, error) {
	command, present, err := decodeCanvasImagePromptCommand(input)
	if err != nil || !present {
		return "", present, err
	}
	if taskType != "canvas_image" || strings.TrimSpace(fmt.Sprint(input["mode"])) != "image" {
		return "", true, BadAuthRequest("portraitTexture 只能用于 canvas_image 图片任务")
	}
	references, ok := input["referenceImages"].([]interface{})
	if !ok || len(references) < 1 || len(references) > 8 {
		return "", true, BadAuthRequest("portraitTexture 必须包含 1-8 个真实图片资源")
	}
	if s.imagePromptSpecialistClient == nil {
		return "", true, ServiceUnavailable("图片提示词 Specialist 尚未启用")
	}
	rollouts, err := s.productRolloutSetting()
	if err != nil {
		return "", true, err
	}
	if !rolloutEnabled(userID, "canvasToolRegistryV2", rollouts.CanvasToolRegistryV2) {
		return "", true, BadAuthRequest("当前账号尚未启用新版图片工具")
	}
	binding, err := s.freezeImagePromptModelBinding()
	if err != nil {
		return "", true, err
	}
	input["promptModel"] = binding
	return command.Objective, true, nil
}

func (s *Service) freezeImagePromptModelBinding() (imagePromptSpecialistModelBindingV1, error) {
	selected, err := s.imagePromptModelSetting()
	if err != nil {
		return imagePromptSpecialistModelBindingV1{}, err
	}
	if !selected.Configured {
		return imagePromptSpecialistModelBindingV1{}, ServiceUnavailable("管理员尚未配置可用的图片提示词模型")
	}
	modelRecord, err := s.repo.ChannelModelByRecordID(selected.ChannelModelID)
	if err != nil {
		return imagePromptSpecialistModelBindingV1{}, err
	}
	if modelRecord.UpdatedAt.IsZero() || !boundedTrimmed(modelRecord.ID, 160) || !boundedTrimmed(modelRecord.ModelKey, 160) || modelRecord.ChannelID != selected.ChannelID || modelRecord.ModelKey != selected.ModelKey || normalizeCapability(modelRecord.Capability) != "text" || !modelRecord.Enabled {
		return imagePromptSpecialistModelBindingV1{}, ServiceUnavailable("图片提示词模型绑定事实无效")
	}
	if err := validateAgentRuntimeProviderRecord(*modelRecord); err != nil {
		return imagePromptSpecialistModelBindingV1{}, err
	}
	channel, err := s.repo.SystemChannel(modelRecord.ChannelID)
	if err != nil {
		return imagePromptSpecialistModelBindingV1{}, err
	}
	runtime, err := s.resolveVersionedSystemProxyRuntime(channel, modelRecord.ModelKey)
	if err != nil {
		return imagePromptSpecialistModelBindingV1{}, err
	}
	if !boundedTrimmed(runtime.ProviderEndpointVersionID, 160) || !boundedTrimmed(runtime.ProviderCredentialVersionID, 160) {
		return imagePromptSpecialistModelBindingV1{}, ServiceUnavailable("图片提示词模型必须使用版本化供应商凭据")
	}
	return imagePromptSpecialistModelBindingV1{
		CatalogRecordID:             modelRecord.ID,
		ModelKey:                    modelRecord.ModelKey,
		ConfigurationRevision:       modelRecord.UpdatedAt.UTC().Format(time.RFC3339Nano),
		ProviderEndpointVersionID:   runtime.ProviderEndpointVersionID,
		ProviderCredentialVersionID: runtime.ProviderCredentialVersionID,
	}, nil
}

func (s *Service) prepareCanvasImagePromptSpecialist(ctx context.Context, task model.Task, input *canvasGenerationInput) (*imagePromptTaskEvidenceV1, error) {
	if input == nil || input.ImagePromptCommand == nil {
		return nil, nil
	}
	if task.Type != "canvas_image" || input.Mode != "image" || strings.TrimSpace(input.Prompt) != "" || input.PromptModel == nil {
		return nil, &imagePromptSpecialistFailure{Code: imagePromptErrorRequestInvalid, Reason: "冻结的图片命令任务事实无效"}
	}
	if s.imagePromptSpecialistClient == nil {
		return nil, &imagePromptSpecialistFailure{Code: imagePromptErrorUnavailable, Reason: "图片提示词 Specialist 未启用"}
	}
	if err := validateCanvasImagePromptCommand(*input.ImagePromptCommand); err != nil {
		return nil, &imagePromptSpecialistFailure{Code: imagePromptErrorRequestInvalid, Reason: err.Error()}
	}
	order, err := s.repo.BillingOrder(task.BillingOrderID)
	if err != nil || order.TaskID != task.ID || order.UserID != task.UserID {
		return nil, &imagePromptSpecialistFailure{Code: imagePromptErrorRequestInvalid, Reason: "图片任务计费事实不一致"}
	}
	if err := s.verifyFrozenImagePromptModel(*input.PromptModel); err != nil {
		return nil, err
	}
	resolver := s.imagePromptSpecialistEvidenceResolver
	if resolver == nil {
		resolver = s.resolveImagePromptSpecialistEvidence
	}
	evidence, err := resolver(task, *order, *input)
	if err != nil {
		return nil, &imagePromptSpecialistFailure{Code: imagePromptErrorEvidenceInvalid, Reason: taskFailureMessage(err)}
	}
	quoteFingerprint, err := billingOrderQuoteFingerprint(order)
	if err != nil {
		return nil, &imagePromptSpecialistFailure{Code: imagePromptErrorRequestInvalid, Reason: "图片任务报价摘要无法生成"}
	}
	tenantID := task.UserID
	if strings.TrimSpace(order.TeamID) != "" {
		tenantID = order.TeamID
	}
	nodeID := strings.TrimSpace(stringValue(input.Metadata["nodeId"]))
	if nodeID == "" {
		return nil, &imagePromptSpecialistFailure{Code: imagePromptErrorRequestInvalid, Reason: "图片任务缺少目标节点"}
	}
	request := imagePromptSpecialistRequestV1{
		SchemaVersion:     imagePromptRequestVersion,
		RequestID:         "image-prompt-" + task.ID,
		CorrelationID:     task.ID,
		IdempotencyKey:    "image-prompt:" + task.ID,
		Actor:             imagePromptSpecialistActorV1{TenantID: tenantID, UserID: task.UserID},
		Scope:             imagePromptSpecialistScopeV1{ProjectID: task.ProjectID, CanvasID: task.ProjectID, NodeID: nodeID, NodeRevision: input.ImagePromptCommand.NodeRevision},
		CommercialContext: imagePromptSpecialistCommercialContextV1{GenerationTaskID: task.ID, BillingOrderID: task.BillingOrderID, QuoteFingerprint: quoteFingerprint},
		Operation:         imagePromptSpecialistOperationV1{Command: input.ImagePromptCommand.Command, Objective: input.ImagePromptCommand.Objective, Parameters: input.ImagePromptCommand.Parameters},
		Evidence:          evidence,
		PromptModel:       *input.PromptModel,
	}
	execution, err := s.imagePromptSpecialistClient.Execute(ctx, request)
	if err != nil {
		return nil, err
	}
	if err := validateImagePromptSpecialistResponse(request, execution.Response); err != nil {
		return nil, err
	}
	if !validSHA256Hex(execution.ResponseDigest) {
		return nil, &imagePromptSpecialistFailure{Code: imagePromptErrorOutputInvalid, Reason: "图片提示词响应摘要无效"}
	}
	frozen := &imagePromptTaskEvidenceV1{SchemaVersion: imagePromptResponseVersion, Command: *input.ImagePromptCommand, Response: execution.Response, ResponseDigest: execution.ResponseDigest}
	encoded, err := json.Marshal(frozen)
	if err != nil {
		return nil, &imagePromptSpecialistFailure{Code: imagePromptErrorOutputInvalid, Reason: "图片提示词证据无法编码"}
	}
	if err := s.repo.UpsertTaskResult(model.Result{ID: newID(), UserID: task.UserID, TaskID: task.ID, SessionID: task.SessionID, Kind: imagePromptEvidenceResultKind, Payload: string(encoded), CreatedAt: time.Now().UTC()}); err != nil {
		return nil, &imagePromptSpecialistFailure{Code: imagePromptErrorUnavailable, Reason: "图片提示词证据无法持久化"}
	}
	logPayload, _ := json.Marshal(map[string]interface{}{
		"requestId":      request.RequestID,
		"correlationId":  request.CorrelationID,
		"command":        request.Operation.Command,
		"evidenceDigest": execution.Response.Trace.EvidenceDigest,
		"responseDigest": execution.ResponseDigest,
		"model":          execution.Response.Trace.Model,
		"usage":          execution.Response.Trace.Usage,
	})
	if err := s.log(task.UserID, task.ID, "info", "图片提示词 Specialist 已完成", string(logPayload)); err != nil {
		return nil, &imagePromptSpecialistFailure{Code: imagePromptErrorUnavailable, Reason: "图片提示词审计日志无法持久化"}
	}
	input.Prompt = execution.Response.ImagePrompt
	return frozen, nil
}

func (s *Service) verifyFrozenImagePromptModel(binding imagePromptSpecialistModelBindingV1) error {
	record, err := s.repo.ChannelModelByRecordID(binding.CatalogRecordID)
	if err != nil || !record.Enabled || normalizeCapability(record.Capability) != "text" || record.ModelKey != binding.ModelKey || record.UpdatedAt.IsZero() || record.UpdatedAt.UTC().Format(time.RFC3339Nano) != binding.ConfigurationRevision || !boundedTrimmed(binding.ProviderEndpointVersionID, 160) || !boundedTrimmed(binding.ProviderCredentialVersionID, 160) {
		return &imagePromptSpecialistFailure{Code: imagePromptErrorModelBindingInvalid, Reason: "冻结的图片提示词模型已变更或不可用"}
	}
	if err := validateAgentRuntimeProviderRecord(*record); err != nil || record.ProviderCredentialID == "" {
		return &imagePromptSpecialistFailure{Code: imagePromptErrorModelBindingInvalid, Reason: "冻结的图片提示词模型缺少版本化供应商身份"}
	}
	account, err := s.repo.ProviderAccountByKind(kuaiziProviderKind)
	if err != nil || !account.Enabled {
		return &imagePromptSpecialistFailure{Code: imagePromptErrorModelBindingInvalid, Reason: "冻结的图片提示词供应商账号不可用"}
	}
	endpoints, err := s.repo.ProviderEndpointVersions(account.ID)
	if err != nil {
		return &imagePromptSpecialistFailure{Code: imagePromptErrorModelBindingInvalid, Reason: "读取冻结的图片提示词服务版本失败"}
	}
	endpointFound := false
	for index := range endpoints {
		if endpoints[index].ID == binding.ProviderEndpointVersionID && boundedTrimmed(endpoints[index].BaseURL, 2048) && (endpoints[index].Status == "active" || endpoints[index].Status == "retired") {
			endpointFound = true
			break
		}
	}
	credential, err := s.repo.ProviderCredential(record.ProviderCredentialID)
	if err != nil || credential.ProviderAccountID != account.ID || !credential.Enabled || credential.HealthStatus != "healthy" {
		return &imagePromptSpecialistFailure{Code: imagePromptErrorModelBindingInvalid, Reason: "冻结的图片提示词凭据身份不可用"}
	}
	versions, err := s.repo.ProviderCredentialVersions(credential.ID)
	if err != nil {
		return &imagePromptSpecialistFailure{Code: imagePromptErrorModelBindingInvalid, Reason: "读取冻结的图片提示词凭据版本失败"}
	}
	credentialFound := false
	for index := range versions {
		if versions[index].ID == binding.ProviderCredentialVersionID && boundedTrimmed(versions[index].KeyCipher, 8192) && (versions[index].Status == "active" || versions[index].Status == "retired") {
			credentialFound = true
			break
		}
	}
	if !endpointFound || !credentialFound {
		return &imagePromptSpecialistFailure{Code: imagePromptErrorModelBindingInvalid, Reason: "冻结的图片提示词供应商版本不匹配"}
	}
	return nil
}

func (s *Service) resolveImagePromptSpecialistEvidence(task model.Task, order model.BillingOrder, input canvasGenerationInput) (imagePromptSpecialistEvidenceV1, error) {
	if len(input.ReferenceImages) < 1 || len(input.ReferenceImages) > 8 {
		return imagePromptSpecialistEvidenceV1{}, errors.New("portraitTexture 必须包含 1-8 个真实图片资源")
	}
	references := make([]imagePromptSpecialistReferenceV1, 0, len(input.ReferenceImages))
	for index, media := range input.ReferenceImages {
		resourceID := strings.TrimPrefix(strings.TrimSpace(media.StorageKey), "resource:")
		if resourceID == "" || media.StorageKey != "resource:"+resourceID {
			return imagePromptSpecialistEvidenceV1{}, errors.New("图片引用必须是服务端资源")
		}
		resource, digest, accessURL, err := s.resolveImagePromptResource(task, order, resourceID)
		if err != nil {
			return imagePromptSpecialistEvidenceV1{}, err
		}
		role := "source"
		if index > 0 {
			role = "character"
		}
		references = append(references, imagePromptSpecialistReferenceV1{ResourceID: resource.ID, MediaType: "image", SemanticRole: role, Ordinal: index + 1, ContentDigest: digest, AccessURL: accessURL})
	}
	return imagePromptSpecialistEvidenceV1{References: references}, nil
}

func (s *Service) resolveImagePromptResource(task model.Task, order model.BillingOrder, resourceID string) (*model.Resource, string, string, error) {
	var resource *model.Resource
	var err error
	if strings.TrimSpace(order.TeamID) != "" {
		resource, err = s.repo.ResourceForTeam(order.TeamID, resourceID)
	} else {
		resource, err = s.repo.ResourceForUser(task.UserID, resourceID)
	}
	if err != nil {
		return nil, "", "", fmt.Errorf("图片资源不属于当前任务租户：%w", err)
	}
	if resource.Status != model.ResourceStatusReady || resource.Kind != "image" || resource.Size <= 0 {
		return nil, "", "", errors.New("图片资源尚未就绪或类型无效")
	}
	if resource.Provider == "local" {
		return nil, "", "", errors.New("图片提示词 Specialist 需要 OSS 签名资源地址")
	}
	setting, err := s.ossSettingForResource(resource.UserID, resource)
	if err != nil {
		return nil, "", "", err
	}
	if setting.Provider != "aliyun" {
		return nil, "", "", errors.New("图片提示词 Specialist 仅支持阿里云 OSS 签名资源")
	}
	accessURL, err := signedOSSObjectURL(setting, resource.ObjectKey, time.Now().Add(providerResourceURLTTL))
	if err != nil {
		return nil, "", "", fmt.Errorf("图片资源签名失败：%w", err)
	}

	var stream *ResourceStream
	if strings.TrimSpace(order.TeamID) != "" {
		stream, err = s.OpenTeamResourceRange(task.UserID, order.TeamID, resourceID, "")
	} else {
		stream, err = s.OpenResourceRange(task.UserID, resourceID, "")
	}
	if err != nil {
		return nil, "", "", fmt.Errorf("读取图片资源失败：%w", err)
	}
	defer stream.Body.Close()
	digest := sha256.New()
	readBytes, err := io.Copy(digest, io.LimitReader(stream.Body, resource.Size+1))
	if err != nil {
		return nil, "", "", fmt.Errorf("计算图片资源摘要失败：%w", err)
	}
	if readBytes != resource.Size {
		return nil, "", "", errors.New("图片资源大小与存储事实不一致")
	}
	return resource, hex.EncodeToString(digest.Sum(nil)), accessURL, nil
}

func attachImagePromptEvidence(result map[string]interface{}, evidence *imagePromptTaskEvidenceV1) (map[string]interface{}, error) {
	if evidence == nil {
		return result, nil
	}
	if result == nil {
		return nil, errors.New("图片供应商返回空结果")
	}
	// The server-owned evidence always wins over an untrusted provider field.
	// Attaching evidence happens after the paid provider call, so it must never
	// turn a successful upstream generation into a failed/refunded task.
	result["imagePromptEvidence"] = evidence
	return result, nil
}
