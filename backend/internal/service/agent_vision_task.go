package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

var errAgentVisionDispatchAmbiguous = errors.New("vision.analyze provider dispatch is ambiguous")

type agentVisionExecution struct {
	Result            agentruntime.VisionAnalyzeResult
	ProviderRequestID string
	Usage             TokenUsageFact
	RequestSent       bool
	ManagedBilling    bool
}

func (s *Service) ensureAgentVisionTask(
	ctx context.Context,
	scope agentruntime.Scope,
	attempt agentVisionAttempt,
) (*model.Task, *model.BillingOrder, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if existing, err := s.repo.TaskForUser(scope.ActorUserID, attempt.TaskID); err == nil {
		return s.validateAgentVisionTaskFacts(scope, attempt, existing)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, err
	}

	billingScope, err := billingAccountScopeFromAgent(scope)
	if err != nil {
		return nil, nil, err
	}
	reservation := TokenBillingReservation{
		TaskID: attempt.TaskID, EstimatedInputTokens: attempt.EstimatedInputTokens,
		MaxOutputTokens: attempt.MaxOutputTokens, Pricing: attempt.Pricing,
	}
	order, err := s.newTaskTokenBillingOrder(
		scope.ActorUserID, billingScope, attempt.Model.ChannelID, attempt.Model.ModelKey,
		agentVisionTaskType, attempt.BillingIdempotencyKey, reservation,
	)
	if err != nil {
		return nil, nil, err
	}
	if order.AmountMicrocredits != attempt.AmountMicrocredits || order.ReservedAmountMicrocredits != attempt.AmountMicrocredits {
		return nil, nil, errors.New("vision.analyze billing quote changed after approval")
	}
	inputJSON, err := json.Marshal(attempt.Input)
	if err != nil {
		return nil, nil, err
	}
	policy, err := s.RuntimePolicy()
	if err != nil {
		return nil, nil, err
	}
	activePolicy, _, err := s.membershipActiveTaskPolicy(scope.ActorUserID, billingScope, agentVisionTaskType, policy)
	if err != nil {
		return nil, nil, err
	}
	task := &model.Task{
		ID: attempt.TaskID, UserID: scope.ActorUserID, Audience: model.TaskAudienceInternal,
		ProjectID: scope.CanvasID, Type: agentVisionTaskType, Status: model.TaskStatusQueued,
		Stage: "等待图片理解模型调度", Progress: 5, Prompt: attempt.Input.Arguments.Prompt,
		Operation: agentVisionOperationForRun(scope.RunID), Provider: "system", Model: attempt.Model.ModelKey,
		Capability: "vision", BillingOrderID: order.ID, InputJSON: string(inputJSON),
	}
	if err := s.ensureTaskProjectActive(scope.ActorUserID, scope.CanvasID); err != nil {
		return nil, nil, err
	}
	watermark, err := s.taskWatermarkCapability(task.Capability, order)
	if err != nil {
		return nil, nil, err
	}
	err = s.createTaskWithinStorageQuota(task, order, policy, activePolicy, watermark)
	if err == nil {
		s.recordActivity(scope.ActorUserID, "task", 1)
		_ = s.log(scope.ActorUserID, task.ID, "info", "图片理解任务已进入队列", "")
		return s.validateAgentVisionTaskFacts(scope, attempt, task)
	}
	if existing, lookupErr := s.repo.TaskForUser(scope.ActorUserID, attempt.TaskID); lookupErr == nil {
		return s.validateAgentVisionTaskFacts(scope, attempt, existing)
	}
	if errors.Is(err, repository.ErrInsufficientCredits) {
		return nil, nil, errors.Join(BadAuthRequest(creditInsufficientMessage(order.TeamID)), err)
	}
	if errors.Is(err, repository.ErrTeamMemberCreditLimit) {
		return nil, nil, errors.Join(BadAuthRequest("本月团队积分额度已用尽，请联系团队管理员调整额度"), err)
	}
	if errors.Is(err, repository.ErrActiveTaskLimit) || errors.Is(err, repository.ErrCapabilityTaskLimit) {
		return nil, nil, BadAuthRequest("当前 Agent 任务并发额度已用尽，请等待已有任务完成")
	}
	return nil, nil, err
}

func (s *Service) validateAgentVisionTaskFacts(
	scope agentruntime.Scope,
	attempt agentVisionAttempt,
	task *model.Task,
) (*model.Task, *model.BillingOrder, error) {
	if task == nil || task.ID != attempt.TaskID || task.UserID != scope.ActorUserID ||
		task.ProjectID != scope.CanvasID || task.Audience != model.TaskAudienceInternal ||
		task.Type != agentVisionTaskType || task.Capability != "vision" ||
		task.Operation != agentVisionOperationForRun(scope.RunID) || task.Provider != "system" ||
		task.Model != attempt.Model.ModelKey || task.Prompt != attempt.Input.Arguments.Prompt || strings.TrimSpace(task.BillingOrderID) == "" {
		return nil, nil, errors.New("vision.analyze task identity facts conflict")
	}
	expectedInput, err := json.Marshal(attempt.Input)
	if err != nil {
		return nil, nil, err
	}
	expectedInput, err = canonicalAgentJSON(expectedInput)
	if err != nil {
		return nil, nil, err
	}
	actualInput, err := canonicalAgentJSON([]byte(task.InputJSON))
	if err != nil || !bytes.Equal(actualInput, expectedInput) {
		return nil, nil, errors.New("vision.analyze task input facts conflict")
	}
	if err := s.verifyTaskExecutionEnvelope(task, time.Now().UTC()); err != nil {
		return nil, nil, err
	}
	order, err := s.repo.BillingOrder(task.BillingOrderID)
	if err != nil {
		return nil, nil, err
	}
	billingScope, err := billingAccountScopeFromAgent(scope)
	if err != nil {
		return nil, nil, err
	}
	if order.UserID != scope.ActorUserID || order.TeamID != billingScope.TeamID || order.TaskID != task.ID ||
		order.IdempotencyKey != "proxy-token:"+attempt.BillingIdempotencyKey || order.ChannelID != attempt.Model.ChannelID ||
		order.ChannelModelID != attempt.Model.ID || order.Model != attempt.Model.ModelKey || order.Capability != "vision" ||
		order.Scene != agentVisionTaskType || order.BillingMode != "token_usage" || order.PriceVersion != attempt.Model.PriceVersion ||
		order.MultiplierBasisPoints != basisPointsScale || order.Quantity != 1 ||
		order.ReservedAmountMicrocredits != attempt.AmountMicrocredits ||
		order.EstimatedInputTokens != attempt.EstimatedInputTokens || order.MaxOutputTokens != attempt.MaxOutputTokens {
		return nil, nil, errors.New("vision.analyze task billing facts conflict")
	}
	if order.Status != model.BillingStatusSettled && order.AmountMicrocredits != attempt.AmountMicrocredits {
		return nil, nil, errors.New("vision.analyze task billing reservation changed before settlement")
	}
	var pricing TokenPricingSnapshot
	if err := json.Unmarshal([]byte(order.TokenPricingSnapshotJSON), &pricing); err != nil || pricing != attempt.Pricing {
		return nil, nil, errors.New("vision.analyze task pricing snapshot conflicts with approval")
	}
	switch attempt.BillingAuthority {
	case tokenBillingManagedReconciliation:
		if strings.TrimSpace(task.ProviderAccountID) == "" || strings.TrimSpace(task.ProviderEndpointVersionID) == "" ||
			strings.TrimSpace(task.ProviderCredentialVersionID) == "" ||
			order.ProviderEndpointVersionID != task.ProviderEndpointVersionID || order.ProviderCredentialVersionID != task.ProviderCredentialVersionID {
			return nil, nil, errors.New("vision.analyze managed provider runtime facts conflict")
		}
	case tokenBillingResponseUsage:
		if task.ProviderAccountID != "" || task.ProviderEndpointVersionID != "" || task.ProviderCredentialVersionID != "" ||
			order.ProviderEndpointVersionID != "" || order.ProviderCredentialVersionID != "" {
			return nil, nil, errors.New("vision.analyze direct provider runtime facts conflict")
		}
	default:
		return nil, nil, errors.New("vision.analyze billing authority is invalid")
	}
	return taskForOutput(*task), order, nil
}

func decodeAgentVisionTaskInput(raw string) (agentVisionTaskInput, error) {
	var input agentVisionTaskInput
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return agentVisionTaskInput{}, errors.New("vision.analyze task input format is invalid")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return agentVisionTaskInput{}, errors.New("vision.analyze task input contains trailing data")
	}
	argumentsJSON, err := json.Marshal(input.Arguments)
	if err != nil {
		return agentVisionTaskInput{}, err
	}
	if _, err := agentruntime.DecodeCapabilityArguments(agentruntime.ToolVisionAnalyze, argumentsJSON); err != nil {
		return agentVisionTaskInput{}, errors.New("vision.analyze task arguments are invalid")
	}
	if strings.TrimSpace(input.RunID) == "" || strings.TrimSpace(input.ToolCallID) == "" ||
		strings.TrimSpace(input.FrozenModel.ChannelID) == "" || strings.TrimSpace(input.FrozenModel.ModelRecordID) == "" ||
		strings.TrimSpace(input.FrozenModel.Model) == "" || input.FrozenModel.PriceVersion < 1 {
		return agentVisionTaskInput{}, errors.New("vision.analyze task input facts are incomplete")
	}
	return input, nil
}

func (s *Service) processAgentVisionAnalysisTask(ctx context.Context, task *model.Task) (*agentVisionExecution, error) {
	if task == nil || task.Type != agentVisionTaskType || task.Status != model.TaskStatusRunning ||
		strings.TrimSpace(task.BillingOrderID) == "" {
		return nil, errors.New("vision.analyze claimed task facts are invalid")
	}
	input, err := decodeAgentVisionTaskInput(task.InputJSON)
	if err != nil {
		return nil, err
	}
	runID, ok := agentVisionRunID(task.Operation)
	if !ok || runID != input.RunID {
		return nil, errors.New("vision.analyze task Run identity is invalid")
	}
	scope, err := s.scopeForAgentRun(&model.User{ID: task.UserID}, runID)
	if err != nil {
		return nil, errors.New("vision.analyze task Run scope is unavailable")
	}
	state, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, errors.New("vision.analyze task Run checkpoint is unavailable")
	}
	if state.Configuration.GenerationModels.Vision == nil || *state.Configuration.GenerationModels.Vision != input.FrozenModel {
		return nil, errors.New("vision.analyze task frozen model conflicts with the Run")
	}
	argumentsJSON, err := json.Marshal(input.Arguments)
	if err != nil {
		return nil, err
	}
	record, err := s.repo.AgentToolCallForScope(scope, input.ToolCallID, 1)
	if err != nil || record.ToolName != string(agentruntime.ToolVisionAnalyze) ||
		!equalAgentToolArguments(record.InputJSON, argumentsJSON) ||
		record.ApprovalDecision != agentruntime.ToolApprovalApproved || record.ApprovalByUserID != scope.ActorUserID ||
		record.ApprovalDecidedAt == nil ||
		(record.Status != agentruntime.ToolCallPending && record.Status != agentruntime.ToolCallRunning) {
		return nil, errors.Join(errors.New("vision.analyze task approval facts are invalid"), err)
	}
	order, err := s.repo.BillingOrder(task.BillingOrderID)
	if err != nil {
		return nil, err
	}
	if order.TaskID != task.ID || order.UserID != task.UserID || order.ChannelID != input.FrozenModel.ChannelID ||
		order.ChannelModelID != input.FrozenModel.ModelRecordID || order.Model != input.FrozenModel.Model ||
		order.PriceVersion != input.FrozenModel.PriceVersion || order.Capability != "vision" || order.BillingMode != "token_usage" {
		return nil, errors.New("vision.analyze task billing facts are invalid")
	}
	if task.PollStage != "" || task.ProviderRequestID != "" || order.ProviderRequestID != "" || order.Status != model.BillingStatusReserved {
		return &agentVisionExecution{RequestSent: true}, errAgentVisionDispatchAmbiguous
	}
	resources, err := s.agentVisionResourcesForRun(scope, state.Configuration, input.Arguments.SourceResourceIDs)
	if err != nil {
		return nil, err
	}
	media, err := s.agentVisionProviderMedia(scope, resources)
	if err != nil {
		return nil, err
	}
	config, err := s.resolveAgentVisionTaskProviderConfig(*task, input, *order)
	if err != nil {
		return nil, err
	}
	execution := &agentVisionExecution{ManagedBilling: order.ProviderEndpointVersionID != ""}
	providerResult, err := runVisionAnalysis(ctx, canvasGenerationInput{
		Mode: "text", Prompt: input.Arguments.Prompt, ImageDetail: input.Arguments.Detail,
		ReferenceImages: media, Config: config,
	}, func() error {
		if err := s.repo.BeginClaimedTokenProviderDispatch(task, agentVisionProviderDispatchStarted, time.Now().UTC()); err != nil {
			return errors.Join(errAgentVisionDispatchAmbiguous, err)
		}
		task.PollStage = agentVisionProviderDispatchStarted
		return nil
	})
	execution.RequestSent = providerResult.RequestSent || task.PollStage == agentVisionProviderDispatchStarted
	execution.ProviderRequestID = providerResult.ProviderRequestID
	execution.Usage = providerResult.Usage
	if err != nil {
		return execution, err
	}
	execution.Result = agentruntime.VisionAnalyzeResult{
		TaskID: task.ID, BillingOrderID: task.BillingOrderID,
		ModelRecordID: input.FrozenModel.ModelRecordID, ModelKey: input.FrozenModel.Model,
		ClientRequestID:   input.Arguments.ClientRequestID,
		SourceResourceIDs: append([]string(nil), input.Arguments.SourceResourceIDs...), Detail: input.Arguments.Detail,
		Analysis: providerResult.Analysis,
		Usage: agentruntime.VisionTokenUsage{
			InputTokens: providerResult.Usage.InputTokens, CachedTokens: providerResult.Usage.CachedTokens,
			OutputTokens: providerResult.Usage.OutputTokens,
		},
	}
	return execution, nil
}

func (s *Service) resolveAgentVisionTaskProviderConfig(task model.Task, input agentVisionTaskInput, order model.BillingOrder) (providerConfig, error) {
	config := providerConfig{
		ChannelID: input.FrozenModel.ChannelID, Model: input.FrozenModel.Model,
		MaxOutputTokens: order.MaxOutputTokens,
	}
	resolved, err := s.resolveProviderConfig(config)
	if err != nil {
		return providerConfig{}, err
	}
	if resolved.InterfaceType != string(model.ChannelInterfaceChatCompletion) && resolved.InterfaceType != string(model.ChannelInterfaceOpenAIResponse) {
		return providerConfig{}, errors.New("vision.analyze frozen provider interface is unsupported")
	}
	hasProviderRuntime := task.ProviderAccountID != "" && task.ProviderEndpointVersionID != "" && task.ProviderCredentialVersionID != ""
	hasPartialProviderRuntime := task.ProviderAccountID != "" || task.ProviderEndpointVersionID != "" || task.ProviderCredentialVersionID != ""
	if !hasProviderRuntime {
		if hasPartialProviderRuntime || order.ProviderEndpointVersionID != "" || order.ProviderCredentialVersionID != "" {
			return providerConfig{}, errors.New("vision.analyze frozen provider runtime is incomplete")
		}
		return resolved, nil
	}
	_, spec, managed := kuaiziProviderFamilyForModel(resolved.Model)
	if !managed || spec.Capability != "vision" || order.ProviderEndpointVersionID != task.ProviderEndpointVersionID ||
		order.ProviderCredentialVersionID != task.ProviderCredentialVersionID {
		return providerConfig{}, errors.New("vision.analyze managed provider runtime conflicts with the frozen model")
	}
	runtime, err := s.repo.FrozenProviderRuntime(task)
	if err != nil {
		return providerConfig{}, errors.New("vision.analyze frozen provider runtime is unavailable")
	}
	key, err := NewProviderSecretCipher(s.dataDir).Decrypt(runtime.ProviderAccountID, runtime.ProviderCredentialID, runtime.CredentialVersion, runtime.KeyCipher)
	if err != nil {
		return providerConfig{}, errors.New("vision.analyze frozen provider credential cannot be decrypted")
	}
	resolved.BaseURL = kuaiziChatCompletionsBaseURL(runtime.BaseURL)
	resolved.APIKey = key
	resolved.InterfaceType = string(model.ChannelInterfaceChatCompletion)
	resolved.ManagedProviderRuntime = true
	return resolved, nil
}

func (s *Service) saveAgentVisionTaskCompletion(task *model.Task, execution agentVisionExecution) error {
	if task == nil || strings.TrimSpace(execution.ProviderRequestID) == "" || !execution.Usage.Available {
		return errors.New("vision.analyze completion facts are invalid")
	}
	resultJSON, err := json.Marshal(execution.Result)
	if err != nil {
		return err
	}
	completed := *task
	completed.Status = model.TaskStatusSucceeded
	completed.Stage = "图片理解完成"
	completed.Progress = 100
	completed.ResultJSON = string(resultJSON)
	completed.ProviderRequestID = execution.ProviderRequestID
	completed.PollStage = agentVisionProviderDispatchStarted
	completedAt := time.Now().UTC()
	completed.CompletedAt = &completedAt
	outbox, err := taskAgentRunOutboxDraft(completed, completedAt)
	if err != nil {
		return err
	}
	finalization := repository.SucceededTaskFinalization{
		Task: &completed, BillingAction: repository.CompletedTaskBillingUncertain,
		BillingError: "供应商已返回图片理解结果，Token 用量与费用待核对", Outbox: outbox,
	}
	if execution.ManagedBilling {
		finalization.ReportedTokenUsage = &repository.TokenUsageFact{
			InputTokens: execution.Usage.InputTokens, CachedTokens: execution.Usage.CachedTokens,
			OutputTokens: execution.Usage.OutputTokens,
		}
	} else {
		order, loadErr := s.repo.BillingOrder(task.BillingOrderID)
		if loadErr != nil {
			return loadErr
		}
		settlement, settlementErr := responseUsageSettlementFromOrder(*order, execution.ProviderRequestID, execution.Usage, completedAt)
		if settlementErr != nil {
			return settlementErr
		}
		finalization.BillingAction = repository.CompletedTaskBillingSettleFromUsage
		finalization.BillingError = ""
		finalization.ResponseUsageSettlement = &settlement
	}
	if err := s.repo.FinalizeSucceededTaskAndBilling(finalization); err != nil {
		return err
	}
	*task = completed
	return nil
}

func validateAgentVisionCompletedResult(
	task model.Task,
	order model.BillingOrder,
	arguments agentruntime.VisionAnalyzeArguments,
	result agentruntime.VisionAnalyzeResult,
) error {
	if task.Status != model.TaskStatusSucceeded || order.Status != model.BillingStatusSettled ||
		result.TaskID != task.ID || result.BillingOrderID != order.ID ||
		result.ModelRecordID != arguments.ModelRecordID || result.ModelKey != arguments.ModelKey ||
		result.ClientRequestID != arguments.ClientRequestID || result.Detail != arguments.Detail ||
		!equalOrderedStrings(result.SourceResourceIDs, arguments.SourceResourceIDs) ||
		strings.TrimSpace(task.ProviderRequestID) == "" {
		return errors.New("vision.analyze settled identity facts conflict")
	}
	usage := TokenUsageFact{
		InputTokens: result.Usage.InputTokens, CachedTokens: result.Usage.CachedTokens,
		OutputTokens: result.Usage.OutputTokens, Available: true,
	}
	if usage.InputTokens <= 0 || usage.CachedTokens < 0 || usage.CachedTokens > usage.InputTokens || usage.OutputTokens <= 0 ||
		order.TokenUsageStatus != "reported" || order.InputTokens != usage.InputTokens ||
		order.CachedTokens != usage.CachedTokens || order.OutputTokens != usage.OutputTokens {
		return errors.New("vision.analyze settled usage facts conflict")
	}
	if order.ProviderEndpointVersionID == "" && order.ProviderCredentialVersionID == "" {
		if order.ProviderRequestID != task.ProviderRequestID {
			return errors.New("vision.analyze direct provider request facts conflict")
		}
		settlement, err := responseUsageSettlementFromOrder(order, task.ProviderRequestID, usage, time.Now().UTC())
		if err != nil || settlement.AmountMicrocredits != order.AmountMicrocredits {
			return errors.Join(errors.New("vision.analyze direct settlement facts conflict"), err)
		}
	} else {
		providerTaskID, err := storedTokenBillingTaskID(task.ProviderRequestID)
		if err != nil || providerTaskID != order.ProviderRequestID {
			return errors.Join(errors.New("vision.analyze managed provider request facts conflict"), err)
		}
	}
	return nil
}

func equalOrderedStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
