package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

const agentRuntimeModelTaskType = "agent_runtime_model"

func agentRuntimeModelOperation(runID string) string {
	return "agent_model:" + strings.TrimSpace(runID)
}

func agentRuntimeModelRunID(operation string) (string, bool) {
	const prefix = "agent_model:"
	operation = strings.TrimSpace(operation)
	if !strings.HasPrefix(operation, prefix) || len(operation) > 64 {
		return "", false
	}
	runID := strings.TrimSpace(strings.TrimPrefix(operation, prefix))
	return runID, runID != ""
}

const agentRuntimeMaxSteps = 24
const agentRuntimeMaxToolCalls = 24
const agentRuntimeMaxElapsed = 30 * time.Minute

type StartAgentRuntimeInput struct {
	Context         context.Context
	Scope           agentruntime.Scope
	ClientRequestID string
	UserMessage     string
	MaxSteps        int
	Configuration   AgentRuntimeConfigurationInput
}

type AgentRuntimeProgress struct {
	Run       model.AgentRun
	State     agentruntime.RuntimeState
	ModelTask *model.Task
}

type agentRuntimeModelTaskInput struct {
	Mode   string         `json:"mode"`
	Prompt string         `json:"prompt"`
	Config providerConfig `json:"config"`
}

type agentRuntimeScopeFact struct {
	TenantKind      agentruntime.TenantKind `json:"tenantKind"`
	TenantID        string                  `json:"tenantId"`
	ActorUserID     string                  `json:"actorUserId"`
	DomainProjectID string                  `json:"domainProjectId"`
	CanvasID        string                  `json:"canvasId"`
	ThreadID        string                  `json:"threadId"`
}

type agentRuntimeModelContext struct {
	RunID                string                                `json:"runId"`
	CanvasID             string                                `json:"canvasId"`
	Scope                agentRuntimeScopeFact                 `json:"scope"`
	ToolSchemaVersion    int                                   `json:"toolSchemaVersion"`
	CanvasRevision       int64                                 `json:"canvasRevision"`
	StepNumber           int                                   `json:"stepNumber"`
	MaxSteps             int                                   `json:"maxSteps"`
	UserMessage          string                                `json:"userMessage"`
	ExpectedDelivery     *agentruntime.ExpectedDelivery        `json:"expectedDelivery,omitempty"`
	DeliveryEvidence     *agentruntime.DeliveryEvidence        `json:"deliveryEvidence,omitempty"`
	Verification         *agentruntime.DeliveryVerification    `json:"deliveryVerification,omitempty"`
	LastToolResult       *agentruntime.ToolResult              `json:"lastToolResult,omitempty"`
	DecisionFeedback     *agentruntime.ModelDecisionFeedback   `json:"decisionFeedback,omitempty"`
	PreviousMessage      string                                `json:"previousMessage,omitempty"`
	Configuration        agentruntime.RunConfiguration         `json:"configuration"`
	LoadedSkillDirs      []string                              `json:"loadedSkillDirs,omitempty"`
	ClarificationHistory []agentruntime.CompletedClarification `json:"clarificationHistory,omitempty"`
	Limits               *agentruntime.RuntimeLimits           `json:"limits,omitempty"`
	CallableTools        []agentRuntimeCallableToolFact        `json:"callableTools"`
	CallableModels       []agentRuntimeCallableModelFact       `json:"callableModels"`
}

func (s *Service) StartAgentRuntime(input StartAgentRuntimeInput) (*AgentRuntimeProgress, error) {
	input.ClientRequestID = strings.TrimSpace(input.ClientRequestID)
	input.UserMessage = strings.TrimSpace(input.UserMessage)
	if err := input.Scope.Validate(); err != nil {
		return nil, err
	}
	if !input.Scope.CanMutateCanvas() {
		return nil, Forbidden("当前用户没有执行 Agent 的画布权限")
	}
	if input.ClientRequestID == "" || input.UserMessage == "" || len(input.UserMessage) > 64*1024 || input.MaxSteps < 1 || input.MaxSteps > agentRuntimeMaxSteps {
		return nil, BadAuthRequest("Agent 请求事实无效")
	}
	scope := input.Scope
	existing, err := s.repo.AgentRunForClientRequest(scope, input.ClientRequestID)
	var run model.AgentRun
	if err == nil {
		run = *existing
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		configuration, resolveErr := s.resolveAgentRuntimeConfiguration(input.Context, input.Scope.ActorUserID, input.Configuration)
		if resolveErr != nil {
			return nil, resolveErr
		}
		selected, selectErr := s.agentRuntimeDefaultModel()
		if selectErr != nil {
			return nil, selectErr
		}
		now := time.Now().UTC()
		initialized, initializeErr := s.repo.CreateInitializedAgentRun(repository.CreateInitializedAgentRunInput{
			Create: repository.CreateAgentRunInput{
				Scope: scope, ClientRequestID: input.ClientRequestID, Now: now,
			},
			Initialize: repository.InitializeAgentRunInput{
				Scope: scope, ModelRecordID: selected.ID, ModelKey: selected.ModelKey,
				MaxSteps: input.MaxSteps, ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
				RuntimeVersion: agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion,
				UserMessage: input.UserMessage, Configuration: configuration, Now: now,
				Limits: &agentruntime.RuntimeLimits{
					MaxToolCalls: agentRuntimeMaxToolCalls,
					StartedAt:    now,
					DeadlineAt:   now.Add(agentRuntimeMaxElapsed),
				},
			},
		})
		if initializeErr != nil {
			return nil, initializeErr
		}
		run = initialized.Run
	} else {
		return nil, err
	}
	scope.RunID = run.ID
	if err := validateAgentRuntimeExecutionContract(run); err != nil {
		return nil, err
	}
	state, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	if state.UserMessage != input.UserMessage || state.MaxSteps != input.MaxSteps || !agentRuntimeConfigurationMatchesInput(state.Configuration, input.Configuration) {
		return nil, errors.New("agent runtime request facts conflict")
	}
	switch state.Status {
	case agentruntime.RunSucceeded, agentruntime.RunFailed, agentruntime.RunCancelled, agentruntime.RunWaitingInput, agentruntime.RunWaitingApproval, agentruntime.RunWaitingTool:
		return &AgentRuntimeProgress{Run: run, State: state}, nil
	case agentruntime.RunQueued, agentruntime.RunRunning:
	default:
		return nil, errors.New("agent runtime status is invalid")
	}
	return s.advanceAgentRun(scope, agentWakeRunStarted)
}

func (s *Service) ResumeAgentRuntime(scope agentruntime.Scope) (*AgentRuntimeProgress, error) {
	return s.advanceAgentRun(scope, agentWakeStaleRecovery)
}

func (s *Service) resumeAgentRuntimeStep(scope agentruntime.Scope) (*AgentRuntimeProgress, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if !scope.CanMutateCanvas() {
		return nil, Forbidden("当前用户没有继续执行 Agent 的画布权限")
	}
	run, err := s.repo.AgentRunForScope(scope)
	if err != nil {
		return nil, err
	}
	if err := validateAgentRuntimeExecutionContract(*run); err != nil {
		return nil, err
	}
	state, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	if state.Status == agentruntime.RunSucceeded || state.Status == agentruntime.RunFailed || state.Status == agentruntime.RunCancelled || state.Status == agentruntime.RunWaitingInput || state.Status == agentruntime.RunWaitingApproval || state.Status == agentruntime.RunWaitingTool {
		return &AgentRuntimeProgress{Run: *run, State: state}, nil
	}
	taskID := agentRuntimeModelTaskID(scope.RunID, state.StepNumber)
	task, err := s.repo.TaskForUser(scope.ActorUserID, taskID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		task, err = s.ensureAgentRuntimeModelTask(scope, *run, state)
	} else if err == nil && (task.Status == model.TaskStatusQueued || task.Status == model.TaskStatusRunning) {
		task, err = s.validateAgentRuntimeModelTask(scope, task, *run, state)
	}
	if err != nil {
		return nil, err
	}
	if task.Status == model.TaskStatusQueued || task.Status == model.TaskStatusRunning {
		return &AgentRuntimeProgress{Run: *run, State: state, ModelTask: taskForOutput(*task)}, nil
	}
	if task.Status != model.TaskStatusSucceeded {
		transition, transitionErr := agentruntime.Fail(state, "model_task_failed")
		if transitionErr != nil {
			return nil, transitionErr
		}
		return s.commitAgentRuntimeState(scope, state, transition)
	}
	decision, err := parseAgentRuntimeModelTaskResult(task.ResultJSON, run.ToolSchemaVersion)
	if err != nil {
		var rejected *agentRuntimeModelDecisionRejectedError
		if errors.As(err, &rejected) {
			if run.ToolSchemaVersion == agentruntime.CurrentToolSchemaVersion {
				transition, failErr := agentruntime.Fail(state, "model_decision_invalid")
				if failErr != nil {
					return nil, errors.Join(err, failErr)
				}
				return s.commitAgentRuntimeState(scope, state, transition)
			}
			transition, rejectErr := agentruntime.RejectModelDecision(state, rejected.feedback)
			if rejectErr != nil {
				return nil, rejectErr
			}
			progress, commitErr := s.commitAgentRuntimeState(scope, state, transition)
			if commitErr != nil {
				return nil, commitErr
			}
			if progress.State.Status == agentruntime.RunRunning {
				nextTask, taskErr := s.ensureAgentRuntimeModelTask(scope, progress.Run, progress.State)
				if taskErr != nil {
					return nil, taskErr
				}
				progress.ModelTask = taskForOutput(*nextTask)
			}
			return progress, nil
		}
		transition, transitionErr := agentruntime.Fail(state, "model_decision_invalid")
		if transitionErr != nil {
			return nil, errors.Join(err, transitionErr)
		}
		return s.commitAgentRuntimeState(scope, state, transition)
	}
	if decision.ToolCall != nil {
		_, lookupErr := s.repo.AgentToolCallForScope(scope, decision.ToolCall.ToolCallID, decision.ToolCall.ActionVersion)
		switch {
		case lookupErr == nil:
			transition, rejectErr := agentruntime.RejectReusedToolIdentity(state, *decision.ToolCall)
			if rejectErr != nil {
				return nil, rejectErr
			}
			progress, commitErr := s.commitAgentRuntimeState(scope, state, transition)
			if commitErr != nil {
				return nil, commitErr
			}
			if progress.State.Status == agentruntime.RunRunning {
				nextTask, taskErr := s.ensureAgentRuntimeModelTask(scope, progress.Run, progress.State)
				if taskErr != nil {
					return nil, taskErr
				}
				progress.ModelTask = taskForOutput(*nextTask)
			}
			return progress, nil
		case !errors.Is(lookupErr, gorm.ErrRecordNotFound):
			return nil, lookupErr
		}
	}
	finalMessage := ""
	if decision.Final != nil {
		finalMessage = decision.Final.Message
	}
	evidence, err := s.agentRuntimeDeliveryEvidence(scope, finalMessage)
	if err != nil {
		transition, transitionErr := agentruntime.Fail(state, "delivery_evidence_invalid")
		if transitionErr != nil {
			return nil, errors.Join(err, transitionErr)
		}
		return s.commitAgentRuntimeState(scope, state, transition)
	}
	transition, err := agentruntime.AdvanceForToolSchema(
		state,
		agentruntime.RuntimeInput{Decision: decision, Evidence: evidence},
		run.ToolSchemaVersion,
	)
	if err != nil {
		return nil, err
	}
	if run.ToolSchemaVersion == agentruntime.CurrentToolSchemaVersion &&
		transition.State.Status == agentruntime.RunWaitingApproval && transition.State.PendingToolCall != nil &&
		transition.State.PendingToolCall.ToolName == agentruntime.ToolMediaGenerate {
		quote, quoteErr := s.freezeAgentMediaCapabilityQuote(scope, *transition.State.PendingToolCall, time.Now().UTC())
		if quoteErr != nil {
			return nil, errors.Join(errors.New("media.generate approval quote could not be frozen"), quoteErr)
		}
		transition.ApprovalCostQuote = quote
	}
	progress, err := s.commitAgentRuntimeState(scope, state, transition)
	if err != nil {
		return nil, err
	}
	if progress.State.Status == agentruntime.RunRunning {
		nextTask, taskErr := s.ensureAgentRuntimeModelTask(scope, progress.Run, progress.State)
		if taskErr != nil {
			return nil, taskErr
		}
		progress.ModelTask = taskForOutput(*nextTask)
	}
	return progress, nil
}

func (s *Service) commitAgentRuntimeState(scope agentruntime.Scope, previous agentruntime.RuntimeState, transition agentruntime.RuntimeTransition) (*AgentRuntimeProgress, error) {
	if err := s.repo.CommitAgentRuntimeTransition(scope, previous, transition, time.Now().UTC()); err != nil {
		if !errors.Is(err, repository.ErrAgentRuntimeStepConflict) {
			return nil, err
		}
		latestRun, loadRunErr := s.repo.AgentRunForScope(scope)
		if loadRunErr != nil {
			return nil, loadRunErr
		}
		latestState, loadStateErr := s.repo.LoadAgentCheckpoint(scope)
		if loadStateErr != nil {
			return nil, loadStateErr
		}
		return &AgentRuntimeProgress{Run: *latestRun, State: latestState}, nil
	}
	updatedRun, err := s.repo.AgentRunForScope(scope)
	if err != nil {
		return nil, err
	}
	return &AgentRuntimeProgress{Run: *updatedRun, State: transition.State}, nil
}

func (s *Service) agentRuntimeDefaultModel() (*model.ChannelModel, error) {
	selected, err := s.PublicAgentDefaultModel()
	if err != nil {
		return nil, err
	}
	if selected == nil {
		return nil, ServiceUnavailable("管理员尚未配置可用的 Agent 模型")
	}
	item, err := s.repo.ChannelModelByRecordID(selected.ChannelModelID)
	if err != nil {
		return nil, err
	}
	_, spec, managed := kuaiziProviderFamilyForModel(item.ModelKey)
	if !managed || spec.Capability != "text" || item.ProviderCredentialID == "" || item.ModelKey != selected.ModelKey || item.ChannelID != selected.ChannelID {
		return nil, ServiceUnavailable("当前 Agent 模型没有版本化筷子账号凭据")
	}
	return item, nil
}

func (s *Service) ensureAgentRuntimeModelTask(scope agentruntime.Scope, run model.AgentRun, state agentruntime.RuntimeState) (*model.Task, error) {
	taskID := agentRuntimeModelTaskID(scope.RunID, state.StepNumber)
	if existing, err := s.repo.TaskForUser(scope.ActorUserID, taskID); err == nil {
		return s.validateAgentRuntimeModelTask(scope, existing, run, state)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	item, err := s.repo.ChannelModelByRecordID(run.ModelRecordID)
	if err != nil {
		return nil, err
	}
	_, spec, managed := kuaiziProviderFamilyForModel(item.ModelKey)
	if !managed || spec.Capability != "text" || item.ProviderCredentialID == "" || item.ModelKey != run.ModelKey {
		return nil, ServiceUnavailable("Agent 冻结模型事实不可执行")
	}
	prompt, err := s.agentRuntimeModelPrompt(scope, state)
	if err != nil {
		return nil, err
	}
	systemPrompt, err := agentRuntimeSystemPromptForToolSchema(run.ToolSchemaVersion)
	if err != nil {
		return nil, err
	}
	config := providerConfig{ChannelID: item.ChannelID, Model: item.ModelKey, SystemPrompt: systemPrompt, JSONOutput: true}
	tokenPricing, tokenBilled, err := s.ProxyTokenBillingConfig(scope.ActorUserID, item.ChannelID, item.ModelKey)
	if err != nil {
		return nil, err
	}
	var tokenReservation TokenBillingReservation
	if tokenBilled {
		channel, channelErr := s.repo.SystemChannel(item.ChannelID)
		if channelErr != nil {
			return nil, channelErr
		}
		runtime, runtimeErr := s.ResolveSystemProxyRuntime(channel, item.ModelKey)
		if runtimeErr != nil {
			return nil, runtimeErr
		}
		config.MaxOutputTokens = tokenPricing.MaxOutputTokens
		_, estimatedInputTokens, requestErr := kuaiziChatCompletionsRequestBody(canvasGenerationInput{Mode: "text", Prompt: prompt, Config: config})
		if requestErr != nil {
			return nil, requestErr
		}
		tokenReservation = TokenBillingReservation{
			TaskID: taskID, EstimatedInputTokens: estimatedInputTokens, MaxOutputTokens: tokenPricing.MaxOutputTokens, Pricing: tokenPricing,
			EndpointVersionID: runtime.ProviderEndpointVersionID, CredentialVersionID: runtime.ProviderCredentialVersionID,
		}
	}
	encodedInput, err := json.Marshal(agentRuntimeModelTaskInput{Mode: "text", Prompt: prompt, Config: config})
	if err != nil {
		return nil, err
	}
	policy, err := s.RuntimePolicy()
	if err != nil {
		return nil, err
	}
	billingScope, err := billingAccountScopeFromAgent(scope)
	if err != nil {
		return nil, err
	}
	activePolicy, _, err := s.membershipActiveTaskPolicy(scope.ActorUserID, billingScope, agentRuntimeModelTaskType, policy)
	if err != nil {
		return nil, err
	}
	task := &model.Task{
		ID: taskID, UserID: scope.ActorUserID, Audience: model.TaskAudienceInternal, ProjectID: scope.CanvasID,
		Type: agentRuntimeModelTaskType, Status: model.TaskStatusQueued,
		Stage: "等待 Agent 模型调度", Progress: 5, Prompt: prompt, Operation: agentRuntimeModelOperation(scope.RunID),
		Provider: "system", Model: item.ModelKey, InputJSON: string(encodedInput),
	}
	if err := s.ensureTaskProjectActive(scope.ActorUserID, scope.CanvasID); err != nil {
		return nil, err
	}
	var order *model.BillingOrder
	if tokenBilled {
		order, err = s.newTokenBillingOrder(scope.ActorUserID, billingScope, item.ChannelID, item.ModelKey, "agent_runtime_model", agentRuntimeBillingKey(scope.RunID, state.StepNumber), tokenReservation)
	} else {
		order, err = s.newBillingOrder(scope.ActorUserID, billingScope, task.ID, agentRuntimeBillingKey(scope.RunID, state.StepNumber), item.ChannelID, item.ModelKey, "text", "agent_runtime_model", BillingUsage{Quantity: 1})
	}
	if err != nil {
		return nil, err
	}
	task.Capability = order.Capability
	task.BillingOrderID = order.ID
	watermark, err := s.taskWatermarkCapability(task.Capability, order)
	if err != nil {
		return nil, err
	}
	err = s.createTaskWithinStorageQuota(task, order, policy, activePolicy, watermark)
	if err == nil {
		s.recordActivity(scope.ActorUserID, "task", 1)
		_ = s.log(scope.ActorUserID, task.ID, "info", "Agent 模型任务已进入队列", "")
		return task, nil
	}
	if existing, lookupErr := s.repo.TaskForUser(scope.ActorUserID, taskID); lookupErr == nil {
		return s.validateAgentRuntimeModelTask(scope, existing, run, state)
	}
	if errors.Is(err, repository.ErrInsufficientCredits) {
		return nil, errors.Join(BadAuthRequest(creditInsufficientMessage(order.TeamID)), err)
	}
	if errors.Is(err, repository.ErrTeamMemberCreditLimit) {
		return nil, errors.Join(BadAuthRequest("本月团队积分额度已用尽，请联系团队管理员调整额度"), err)
	}
	if errors.Is(err, repository.ErrActiveTaskLimit) || errors.Is(err, repository.ErrCapabilityTaskLimit) {
		return nil, BadAuthRequest("当前 Agent 任务并发额度已用尽，请等待已有任务完成")
	}
	return nil, err
}

func (s *Service) validateAgentRuntimeModelTask(scope agentruntime.Scope, task *model.Task, run model.AgentRun, state agentruntime.RuntimeState) (*model.Task, error) {
	if task == nil || task.ID != agentRuntimeModelTaskID(run.ID, state.StepNumber) || task.UserID != run.ActorUserID ||
		task.ProjectID != scope.CanvasID || task.Type != agentRuntimeModelTaskType || strings.TrimSpace(task.Capability) == "" ||
		task.Model != run.ModelKey || task.Operation != agentRuntimeModelOperation(scope.RunID) || task.Provider != "system" ||
		task.ProviderAccountID == "" || task.ProviderEndpointVersionID == "" || task.ProviderCredentialVersionID == "" {
		return nil, errors.New("agent runtime model task facts conflict")
	}
	if err := s.verifyTaskExecutionEnvelope(task, time.Now().UTC()); err != nil {
		return nil, err
	}
	order, err := s.repo.BillingOrder(task.BillingOrderID)
	if err != nil {
		return nil, err
	}
	expectedBillingKey := agentRuntimeBillingKey(run.ID, state.StepNumber)
	if order.BillingMode == "token_usage" {
		expectedBillingKey = "proxy-token:" + expectedBillingKey
	}
	expectedBillingScope, err := billingAccountScopeFromAgent(scope)
	if err != nil {
		return nil, err
	}
	if order.UserID != run.ActorUserID || order.TaskID != task.ID || order.IdempotencyKey != expectedBillingKey ||
		strings.TrimSpace(order.TeamID) != expectedBillingScope.TeamID ||
		order.ChannelModelID != run.ModelRecordID || order.Model != run.ModelKey || order.Capability != "text" ||
		order.Scene != "agent_runtime_model" || order.Quantity != 1 || order.AmountMicrocredits <= 0 {
		return nil, errors.New("agent runtime billing facts conflict")
	}
	prompt := task.Prompt
	if err := validateFrozenAgentRuntimeModelPrompt(scope, state, prompt); err != nil {
		return nil, err
	}
	systemPrompt, err := agentRuntimeSystemPromptForToolSchema(run.ToolSchemaVersion)
	if err != nil {
		return nil, err
	}
	expectedInput := agentRuntimeModelTaskInput{
		Mode: "text", Prompt: prompt,
		Config: providerConfig{ChannelID: order.ChannelID, Model: run.ModelKey, SystemPrompt: systemPrompt, MaxOutputTokens: order.MaxOutputTokens, JSONOutput: true},
	}
	decoder := json.NewDecoder(bytes.NewBufferString(task.InputJSON))
	decoder.DisallowUnknownFields()
	var actualInput agentRuntimeModelTaskInput
	if err := decoder.Decode(&actualInput); err != nil {
		return nil, errors.New("agent runtime model task input facts conflict")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("agent runtime model task input facts conflict")
	}
	if task.Prompt != prompt || actualInput != expectedInput {
		return nil, fmt.Errorf(
			"agent runtime model task input facts conflict: task=%s status=%s step=%d mode=%t prompt=%t channel=%t model=%t system=%t max_output=%t json=%t empty_provider=%t",
			task.ID,
			task.Status,
			state.StepNumber,
			actualInput.Mode == expectedInput.Mode,
			actualInput.Prompt == expectedInput.Prompt,
			actualInput.Config.ChannelID == expectedInput.Config.ChannelID,
			actualInput.Config.Model == expectedInput.Config.Model,
			actualInput.Config.SystemPrompt == expectedInput.Config.SystemPrompt,
			actualInput.Config.MaxOutputTokens == expectedInput.Config.MaxOutputTokens,
			actualInput.Config.JSONOutput == expectedInput.Config.JSONOutput,
			agentRuntimeProviderConfigHasOnlyModelFacts(actualInput.Config),
		)
	}
	return task, nil
}

func agentRuntimeProviderConfigHasOnlyModelFacts(config providerConfig) bool {
	config.ChannelID = ""
	config.Model = ""
	config.SystemPrompt = ""
	config.MaxOutputTokens = 0
	config.JSONOutput = false
	return config == (providerConfig{})
}

func agentRuntimeModelTaskID(runID string, step int) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("agent-runtime-model\x00%s\x00%d", runID, step)))
	return fmt.Sprintf("agt_%x", digest[:16])
}

func agentRuntimeBillingKey(runID string, step int) string {
	return fmt.Sprintf("agent-runtime:%s:%d", runID, step)
}

func validateAgentRuntimeExecutionContract(run model.AgentRun) error {
	if run.RuntimeVersion != agentruntime.CurrentRuntimeVersion ||
		run.PolicyVersion != agentruntime.CurrentPolicyVersion ||
		run.ToolSchemaVersion != agentruntime.CurrentToolSchemaVersion {
		return fmt.Errorf("%s: run contract %d/%d/%d is read-only", agentruntime.FailureRuntimeSchemaRetired,
			run.RuntimeVersion, run.PolicyVersion, run.ToolSchemaVersion)
	}
	return nil
}

func agentRuntimeSystemPromptForToolSchema(toolSchemaVersion int) (string, error) {
	if toolSchemaVersion == agentruntime.CurrentToolSchemaVersion {
		return agentRuntimeCloudSystemPrompt, nil
	}
	return "", fmt.Errorf("%s: tool schema %d is read-only", agentruntime.FailureRuntimeSchemaRetired, toolSchemaVersion)
}

const agentRuntimeCloudSystemPrompt = `你是弘梦云端创作 Agent。你必须只依据本轮 JSON 中的真实项目事实、用户消息、已加载 Skill、工具结果、交付证据和动态模型目录自主决策；禁止使用固定工作流、关键词路由、默认模型、静默降级或旧 production graph/specialist/stage 执行链。
每次只能返回一个 JSON 对象，禁止 Markdown、额外文本和 reasoning_content：
1. 直接交付：{"kind":"final","final":{"message":"...","expectedDelivery":{"kind":"answer","requiredArtifacts":[],"completionCriteria":[{"fact":"final_message"}]}}}
2. 结构化追问：{"kind":"clarification_request","clarification":{"requestId":"...","questions":[{"id":"...","prompt":"...","type":"single_choice|multi_choice|free_text","options":[{"id":"...","label":"..."}],"allowCustomAnswer":false}],"expectedDelivery":{"kind":"answer","requiredArtifacts":[],"completionCriteria":[{"fact":"final_message"}]}}}
3. 原子能力调用：{"kind":"tool_call","toolCall":{"toolCallId":"...","toolName":"canvas.read|canvas.apply_ops|assets.read|assets.publish|media.generate|skills.load","actionVersion":1,"arguments":{},"expectedDelivery":{"kind":"answer","requiredArtifacts":[],"completionCriteria":[{"fact":"final_message"}]}}}
首次决策必须声明 expectedDelivery；Runtime 随即冻结该合同，后续工具与 final 必须逐字段复用。只有 deliveryVerification 已满足全部 completionCriteria 时才能 final；否则继续修正或显式失败。每个新工具调用必须使用新的 toolCallId，重试也不得复用。
仅当完成用户目标所需事实确实缺失时才允许追问；能够通过只读能力取得的事实必须先读取。每次收口必须同时核对 deliveryEvidence 与 deliveryVerification；已经满足的 criterion 禁止重复执行，missingCriteria 只剩 final_message 时必须直接返回 final。
canvas.read、assets.read、skills.load 是只读能力并立即执行。canvas.apply_ops、assets.publish 是写能力，media.generate 是付费能力；每次写入或付费动作都会形成独立、不可变、带哈希的审批提案，只有用户批准该精确提案后才执行。审批拒绝、提案过期或哈希不匹配都是本轮事实，必须据此继续规划，禁止降低交付目标或把拒绝伪装成成功。
canvas.read arguments 精确结构为 {"canvasId":"...","selectedNodeIds":[],"includeViewport":true}。canvas.apply_ops arguments 精确结构为 {"canvasId":"...","baseRevision":0,"clientMutationId":"...","operations":[...]}，operations 只能使用工具契约允许的结构化画布操作。
assets.read arguments 精确结构为 {"domainProjectId":"...","resourceIds":[],"limit":100}。assets.publish arguments 精确结构为 {"resourceId":"...","domainProjectId":"...","displayName":"...","clientMutationId":"..."}。
media.generate arguments 精确结构为 {"mediaKind":"image|video|audio","modelRecordId":"...","modelKey":"...","parameters":{},"sourceResourceIds":[],"targetCanvasNodeId":"...","clientRequestId":"..."}。modelRecordId、modelKey、媒体类型和参数必须来自本轮 callableModels 的真实能力事实；禁止猜测能力、参数或价格。生成只返回任务事实，最终交付仍必须由真实任务结果、资源和画布证据验证。
skills.load arguments 精确结构为 {"skillDir":"...","version":1,"checksum":"小写 SHA-256"}。只能加载本轮配置中已授权的精确 Skill 版本；加载后才能使用其方法论。
当事实不足时必须追问或调用读取能力；禁止根据文本、节点连线、占位状态或旧记录猜测资产已经存在。实际产物类型、执行动作、结果落点必须与用户目标一致。`
