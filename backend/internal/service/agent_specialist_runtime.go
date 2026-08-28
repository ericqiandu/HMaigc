package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

const agentSpecialistModelTaskType = "agent_specialist_model"

const agentSpecialistSystemPrompt = `You are an HMaigc production specialist. Use only the frozen request facts and published Skill instructions in the user payload. Return exactly one JSON object matching SpecialistResult. Do not emit markdown, reasoning_content, hidden chain-of-thought, or facts not supported by the frozen inputs.`

var ErrSpecialistOutputInvalid = errors.New("specialist output is invalid")

type SpecialistCompletion struct {
	Run       model.AgentSpecialistRun
	Revisions []model.AgentArtifactRevision
}

type agentSpecialistTaskInput struct {
	Mode   string         `json:"mode"`
	Prompt string         `json:"prompt"`
	Config providerConfig `json:"config"`
}

type agentSpecialistPrompt struct {
	SpecialistRunID       string                             `json:"specialistRunId"`
	ParentSpecialistRunID string                             `json:"parentSpecialistRunId,omitempty"`
	StageID               string                             `json:"stageId"`
	SpecialistKey         agentruntime.SpecialistKey         `json:"specialistKey"`
	SpecialistVersion     int                                `json:"specialistVersion"`
	Objective             string                             `json:"objective"`
	InputRevisions        []agentruntime.ArtifactRevisionRef `json:"inputRevisions"`
	LoadedSkills          []agentruntime.SkillSelection      `json:"loadedSkills"`
	ToolAllowlist         []agentruntime.AgentToolName       `json:"toolAllowlist"`
	ExpectedOutputSchema  string                             `json:"expectedOutputSchema"`
	ExpectedDelivery      agentruntime.ExpectedDelivery      `json:"expectedDelivery"`
}

func (s *Service) RunSpecialist(ctx context.Context, scope agentruntime.Scope, parentRun model.AgentRun, request agentruntime.SpecialistRequest) (SpecialistCompletion, error) {
	if ctx == nil {
		return SpecialistCompletion{}, errors.New("specialist context is required")
	}
	request = canonicalSpecialistRequest(request)
	if err := agentruntime.ValidateSpecialistRequest(request, parentRun.ModelRecordID, parentRun.ModelKey); err != nil {
		return SpecialistCompletion{}, err
	}
	storedParent, err := s.validateSpecialistParentRun(scope, parentRun)
	if err != nil {
		return SpecialistCompletion{}, err
	}
	run, err := s.repo.CreateAgentSpecialistRun(repository.CreateAgentSpecialistRunInput{
		Scope: scope, Request: request, ToolSchemaVersion: storedParent.ToolSchemaVersion, Now: time.Now().UTC(),
	})
	if err != nil {
		return SpecialistCompletion{}, err
	}
	if run.Status == model.AgentSpecialistRunSucceeded {
		revisions, revisionsErr := s.repo.AgentSpecialistRevisions(scope, run.ID)
		return SpecialistCompletion{Run: *run, Revisions: revisions}, revisionsErr
	}
	if run.Status != model.AgentSpecialistRunQueued {
		return SpecialistCompletion{}, repository.ErrAgentSpecialistRunConflict
	}

	task, config, err := s.ensureAgentSpecialistTask(scope, *storedParent, request)
	if err != nil {
		return SpecialistCompletion{}, err
	}
	owner := strings.TrimSpace(s.workerID)
	if owner == "" {
		owner = "agent-specialist-runtime"
	}
	run, task, err = s.repo.ClaimAgentSpecialistRun(scope, run.ID, task.ID, task.BillingOrderID, owner, 5*time.Minute)
	if err != nil {
		return SpecialistCompletion{}, err
	}
	if err := s.BeginTokenBillingRequest(task.BillingOrderID); err != nil {
		failErr := s.failSpecialistAfterClaim(scope, *run, owner, "specialist_billing_start_failed", "Specialist 计费请求未能开始", repository.FailedTaskBillingRefund)
		return SpecialistCompletion{}, errors.Join(err, failErr)
	}

	resolved, err := s.resolveTextTaskProviderConfig(*task, config)
	if err != nil {
		failErr := s.failSpecialistAfterClaim(scope, *run, owner, "specialist_model_unavailable", "Specialist 冻结模型配置不可执行", repository.FailedTaskBillingRefund)
		return SpecialistCompletion{}, errors.Join(err, failErr)
	}
	encodedResolved, err := json.Marshal(agentSpecialistTaskInput{Mode: "text", Prompt: task.Prompt, Config: resolved})
	if err != nil {
		failErr := s.failSpecialistAfterClaim(scope, *run, owner, "specialist_model_config_invalid", "Specialist 冻结模型配置无法序列化", repository.FailedTaskBillingRefund)
		return SpecialistCompletion{}, errors.Join(err, failErr)
	}
	task.InputJSON = string(encodedResolved)
	runContext, cancelRun := context.WithCancel(ctx)
	s.registerActiveTask(task.ID, cancelRun)
	defer func() {
		s.unregisterActiveTask(task.ID)
		cancelRun()
	}()
	providerContext := withProviderAnalytics(runContext, s, *task)
	result, providerErr := runKuaiziChatCompletionStream(providerContext, canvasGenerationInput{
		Mode: "text", Prompt: task.Prompt, Config: resolved,
	}, func(string) error { return nil })
	if evidenceErr := s.persistAgentRuntimeProviderStreamEvidence(*task, result, providerErr); evidenceErr != nil {
		providerErr = errors.Join(providerErr, evidenceErr)
	}
	if providerErr != nil {
		action := repository.FailedTaskBillingRefund
		if result.ProviderRequestID != "" || result.Usage.Available {
			action = repository.FailedTaskBillingUncertain
			if evidenceErr := s.persistAgentRuntimeTokenBillingEvidence(*task, result, "Specialist 上游请求失败，等待账单核对"); evidenceErr != nil {
				providerErr = errors.Join(providerErr, evidenceErr)
			}
		}
		failErr := s.failSpecialistAfterClaim(scope, *run, owner, "specialist_provider_failed", "Specialist 模型请求失败", action)
		return SpecialistCompletion{}, errors.Join(providerErr, failErr)
	}
	if err := s.persistAgentRuntimeTokenBillingEvidence(*task, result, "等待 Specialist 结果持久化后核对"); err != nil {
		failErr := s.failSpecialistAfterClaim(scope, *run, owner, "specialist_billing_evidence_failed", "Specialist 计费事实保存失败", repository.FailedTaskBillingUncertain)
		return SpecialistCompletion{}, errors.Join(err, failErr)
	}

	specialistResult, err := parseAndValidateSpecialistResult(result.Text, request, result.ProviderRequestID)
	if err != nil {
		failErr := s.failSpecialistAfterClaim(scope, *run, owner, "specialist_output_invalid", "Specialist 结构化结果无效", repository.FailedTaskBillingUncertain)
		return SpecialistCompletion{}, errors.Join(ErrSpecialistOutputInvalid, err, failErr)
	}
	resultJSON, err := json.Marshal(specialistResult)
	if err != nil {
		return SpecialistCompletion{}, err
	}
	completed, revisions, err := s.repo.CompleteAgentSpecialistRun(repository.CompleteAgentSpecialistRunInput{
		Scope: scope, SpecialistRunID: run.ID, LeaseOwner: owner, ProviderRequestID: result.ProviderRequestID,
		ResultJSON: string(resultJSON), ResultSummary: specialistResult.Summary, Drafts: specialistResult.Artifacts,
		InputTokens: result.Usage.InputTokens, CachedTokens: result.Usage.CachedTokens, OutputTokens: result.Usage.OutputTokens, Now: time.Now().UTC(),
	})
	if err != nil {
		return SpecialistCompletion{}, err
	}
	return SpecialistCompletion{Run: *completed, Revisions: revisions}, nil
}

func (s *Service) validateSpecialistParentRun(scope agentruntime.Scope, parentRun model.AgentRun) (*model.AgentRun, error) {
	storedParent, err := s.repo.AgentRunForScope(scope)
	if err != nil {
		return nil, err
	}
	if storedParent.ID != parentRun.ID || storedParent.ModelRecordID != parentRun.ModelRecordID || storedParent.ModelKey != parentRun.ModelKey ||
		storedParent.RuntimeVersion != agentruntime.ProductionRuntimeVersion || storedParent.PolicyVersion != agentruntime.ProductionPolicyVersion ||
		storedParent.ToolSchemaVersion != agentruntime.ProductionToolSchemaVersion || storedParent.Status != agentruntime.RunRunning {
		return nil, agentruntime.ErrSpecialistModelInheritance
	}
	return storedParent, nil
}

func (s *Service) ensureAgentSpecialistTask(scope agentruntime.Scope, parentRun model.AgentRun, request agentruntime.SpecialistRequest) (*model.Task, providerConfig, error) {
	taskID := agentSpecialistTaskID(request.SpecialistRunID)
	item, err := s.repo.ChannelModelByRecordID(parentRun.ModelRecordID)
	if err != nil {
		return nil, providerConfig{}, err
	}
	_, spec, managed := kuaiziProviderFamilyForModel(item.ModelKey)
	if !managed || spec.Capability != "text" || item.ProviderCredentialID == "" || item.ModelKey != parentRun.ModelKey || !item.Enabled {
		return nil, providerConfig{}, ServiceUnavailable("Specialist 冻结模型事实不可执行")
	}
	promptPayload := agentSpecialistPrompt{
		SpecialistRunID: request.SpecialistRunID, ParentSpecialistRunID: request.ParentSpecialistRunID,
		StageID: request.StageID, SpecialistKey: request.SpecialistKey,
		SpecialistVersion: request.SpecialistVersion, Objective: request.Objective, InputRevisions: request.InputRevisions,
		LoadedSkills: request.LoadedSkills, ToolAllowlist: request.ToolAllowlist, ExpectedOutputSchema: request.ExpectedOutputSchema,
		ExpectedDelivery: request.ExpectedDelivery,
	}
	promptJSON, err := json.Marshal(promptPayload)
	if err != nil {
		return nil, providerConfig{}, err
	}
	prompt := string(promptJSON)
	config := providerConfig{ChannelID: item.ChannelID, Model: item.ModelKey, SystemPrompt: agentSpecialistSystemPrompt, JSONOutput: true}
	pricing, tokenBilled, err := s.ProxyTokenBillingConfig(scope.ActorUserID, item.ChannelID, item.ModelKey)
	if err != nil {
		return nil, providerConfig{}, err
	}
	if !tokenBilled {
		return nil, providerConfig{}, BadAuthRequest("Specialist 文本模型必须启用 Token 用量计费")
	}
	channel, err := s.repo.SystemChannel(item.ChannelID)
	if err != nil {
		return nil, providerConfig{}, err
	}
	runtime, err := s.ResolveSystemProxyRuntime(channel, item.ModelKey)
	if err != nil {
		return nil, providerConfig{}, err
	}
	config.MaxOutputTokens = pricing.MaxOutputTokens
	_, estimatedInputTokens, err := kuaiziChatCompletionsRequestBody(canvasGenerationInput{Mode: "text", Prompt: prompt, Config: config})
	if err != nil {
		return nil, providerConfig{}, err
	}
	encodedInput, err := json.Marshal(agentSpecialistTaskInput{Mode: "text", Prompt: prompt, Config: config})
	if err != nil {
		return nil, providerConfig{}, err
	}
	if existing, lookupErr := s.repo.TaskForUser(scope.ActorUserID, taskID); lookupErr == nil {
		if existing.Audience != model.TaskAudienceInternal || existing.Type != agentSpecialistModelTaskType || existing.ProjectID != scope.CanvasID ||
			existing.Model != parentRun.ModelKey || existing.Operation != agentSpecialistOperation(request.SpecialistRunID) {
			return nil, providerConfig{}, errors.New("specialist model task facts conflict")
		}
		return existing, config, nil
	} else if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		return nil, providerConfig{}, lookupErr
	}
	policy, err := s.RuntimePolicy()
	if err != nil {
		return nil, providerConfig{}, err
	}
	billingScope, err := billingAccountScopeFromAgent(scope)
	if err != nil {
		return nil, providerConfig{}, err
	}
	activePolicy, capability, err := s.membershipActiveTaskPolicy(scope.ActorUserID, billingScope, agentSpecialistModelTaskType, policy)
	if err != nil {
		return nil, providerConfig{}, err
	}
	task := &model.Task{
		ID: taskID, UserID: scope.ActorUserID, Audience: model.TaskAudienceInternal, ProjectID: scope.CanvasID,
		Type: agentSpecialistModelTaskType, Capability: capability, Status: model.TaskStatusQueued,
		Stage: "等待 Specialist 模型调度", Progress: 5, Prompt: prompt, Operation: agentSpecialistOperation(request.SpecialistRunID),
		Provider: "system", Model: item.ModelKey, InputJSON: string(encodedInput),
	}
	reservation := TokenBillingReservation{
		TaskID: task.ID, EstimatedInputTokens: estimatedInputTokens, MaxOutputTokens: pricing.MaxOutputTokens, Pricing: pricing,
		EndpointVersionID: runtime.ProviderEndpointVersionID, CredentialVersionID: runtime.ProviderCredentialVersionID,
	}
	order, err := s.newTokenBillingOrder(scope.ActorUserID, billingScope, item.ChannelID, item.ModelKey, agentSpecialistModelTaskType, agentSpecialistBillingKey(parentRun.ID, request.SpecialistRunID, 1), reservation)
	if err != nil {
		return nil, providerConfig{}, err
	}
	task.BillingOrderID = order.ID
	if err := s.ensureTaskProjectActive(scope.ActorUserID, scope.CanvasID); err != nil {
		return nil, providerConfig{}, err
	}
	watermark, err := s.taskWatermarkCapability(capability, order)
	if err != nil {
		return nil, providerConfig{}, err
	}
	if err := s.createTaskWithinStorageQuota(task, order, policy, activePolicy, watermark); err != nil {
		return nil, providerConfig{}, err
	}
	return task, config, nil
}

func parseAndValidateSpecialistResult(raw string, request agentruntime.SpecialistRequest, providerRequestID string) (agentruntime.SpecialistResult, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	var result agentruntime.SpecialistResult
	if err := decoder.Decode(&result); err != nil {
		return agentruntime.SpecialistResult{}, err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return agentruntime.SpecialistResult{}, ErrSpecialistOutputInvalid
	}
	result.Summary = strings.TrimSpace(result.Summary)
	if result.Summary == "" || len(result.Summary) > 16*1024 || len(result.Artifacts) != 1 || result.NextActions == nil || strings.TrimSpace(providerRequestID) == "" {
		return agentruntime.SpecialistResult{}, ErrSpecialistOutputInvalid
	}
	seenArtifacts := make(map[string]struct{}, len(result.Artifacts))
	for index := range result.Artifacts {
		draft := &result.Artifacts[index]
		if _, duplicated := seenArtifacts[draft.ArtifactKey]; duplicated {
			return agentruntime.SpecialistResult{}, ErrSpecialistOutputInvalid
		}
		seenArtifacts[draft.ArtifactKey] = struct{}{}
		if draft.Kind+".v"+strconv.Itoa(draft.SchemaVersion) != request.ExpectedOutputSchema ||
			!reflect.DeepEqual(draft.UpstreamRevisions, request.InputRevisions) || !reflect.DeepEqual(draft.SkillVersions, request.LoadedSkills) ||
			(draft.ModelRequestIdentity != "" && draft.ModelRequestIdentity != providerRequestID) {
			return agentruntime.SpecialistResult{}, ErrSpecialistOutputInvalid
		}
		draft.ModelRequestIdentity = providerRequestID
		if err := agentruntime.ValidateArtifactDraft(*draft); err != nil {
			return agentruntime.SpecialistResult{}, err
		}
	}
	for _, action := range result.NextActions {
		if strings.TrimSpace(action.ActionType) != action.ActionType || action.ActionType == "" || len(action.ActionType) > 80 ||
			strings.TrimSpace(action.TargetKey) != action.TargetKey || len(action.TargetKey) > 120 ||
			strings.TrimSpace(action.Rationale) != action.Rationale || action.Rationale == "" || len(action.Rationale) > 2000 {
			return agentruntime.SpecialistResult{}, ErrSpecialistOutputInvalid
		}
	}
	if verification := agentruntime.VerifyDelivery(request.ExpectedDelivery, result.Delivery); verification.Status != agentruntime.VerificationSatisfied {
		return agentruntime.SpecialistResult{}, fmt.Errorf("%w: %s", ErrSpecialistOutputInvalid, verification.Rationale)
	}
	return result, nil
}

func (s *Service) failSpecialistAfterClaim(scope agentruntime.Scope, run model.AgentSpecialistRun, owner string, code string, message string, action repository.FailedTaskBillingAction) error {
	_, err := s.repo.FailAgentSpecialistRun(repository.FailAgentSpecialistRunInput{
		Scope: scope, SpecialistRunID: run.ID, LeaseOwner: owner, ErrorCode: code, ErrorText: message, BillingAction: action, Now: time.Now().UTC(),
	})
	return err
}

func canonicalSpecialistRequest(request agentruntime.SpecialistRequest) agentruntime.SpecialistRequest {
	if request.InputRevisions == nil {
		request.InputRevisions = []agentruntime.ArtifactRevisionRef{}
	}
	if request.ToolAllowlist == nil {
		request.ToolAllowlist = []agentruntime.AgentToolName{}
	}
	return request
}

func agentSpecialistTaskID(specialistRunID string) string {
	digest := sha256.Sum256([]byte("agent-specialist-task\x00" + specialistRunID))
	return fmt.Sprintf("asp_%x", digest[:16])
}

func agentSpecialistOperation(specialistRunID string) string {
	digest := sha256.Sum256([]byte("agent-specialist-operation\x00" + specialistRunID))
	return fmt.Sprintf("agent-specialist:%x", digest[:16])
}

func agentSpecialistBillingKey(parentRunID string, specialistRunID string, attempt int) string {
	return fmt.Sprintf("agent-specialist:%s:%s:%d", parentRunID, specialistRunID, attempt)
}
