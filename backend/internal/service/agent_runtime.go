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

type agentRuntimeModelContext struct {
	RunID                string                                `json:"runId"`
	CanvasID             string                                `json:"canvasId"`
	CanvasRevision       int64                                 `json:"canvasRevision"`
	StepNumber           int                                   `json:"stepNumber"`
	MaxSteps             int                                   `json:"maxSteps"`
	UserMessage          string                                `json:"userMessage"`
	ExpectedDelivery     *agentruntime.ExpectedDelivery        `json:"expectedDelivery,omitempty"`
	Verification         *agentruntime.DeliveryVerification    `json:"deliveryVerification,omitempty"`
	LastToolResult       *agentruntime.ToolResult              `json:"lastToolResult,omitempty"`
	DecisionFeedback     *agentruntime.ModelDecisionFeedback   `json:"decisionFeedback,omitempty"`
	PreviousMessage      string                                `json:"previousMessage,omitempty"`
	Configuration        agentruntime.RunConfiguration         `json:"configuration"`
	LoadedSkillDirs      []string                              `json:"loadedSkillDirs,omitempty"`
	ClarificationHistory []agentruntime.CompletedClarification `json:"clarificationHistory,omitempty"`
	CallableModels       []agentRuntimeCallableModelFact       `json:"callableModels"`
	ProductionPlan       *agentRuntimeProductionPlanFact       `json:"productionPlan,omitempty"`
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
	if input.ClientRequestID == "" || input.UserMessage == "" || len(input.UserMessage) > 64*1024 {
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
				MaxSteps: agentRuntimeMaxSteps, ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
				RuntimeVersion: agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion,
				UserMessage: input.UserMessage, Configuration: configuration, Now: now,
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
	state, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	if state.UserMessage != input.UserMessage || state.MaxSteps != agentRuntimeMaxSteps || !agentRuntimeConfigurationMatchesInput(state.Configuration, input.Configuration) {
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
	if err := s.reconcileSucceededProductionArtifacts(scope); err != nil {
		return nil, err
	}
	run, err := s.repo.AgentRunForScope(scope)
	if err != nil {
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
	decision, err := parseAgentRuntimeModelTaskResult(task.ResultJSON)
	if err != nil {
		var rejected *agentRuntimeModelDecisionRejectedError
		if errors.As(err, &rejected) {
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
		if decision.ToolCall.ToolName == agentruntime.ToolProductionRender {
			if state.ExpectedDelivery != nil && !state.ExpectedDelivery.Equal(decision.ToolCall.ExpectedDelivery) {
				transition, rejectErr := agentruntime.RejectModelDecision(state, agentruntime.ModelDecisionFeedback{
					Code: "delivery_contract_changed", Reason: "expectedDelivery must exactly match the contract frozen by the first model decision",
				})
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
			frozenContext, contextErr := frozenAgentRuntimeModelContext(scope, state, task.Prompt)
			if contextErr != nil {
				return nil, contextErr
			}
			frozenArguments, freezeErr := s.freezeAgentProductionRenderArguments(scope, frozenContext.CallableModels, decision.ToolCall.Arguments)
			if freezeErr != nil {
				failureCode, failureClass, classified := agentProductionRenderFailureDetails(freezeErr)
				if !classified {
					return nil, freezeErr
				}
				output, marshalErr := json.Marshal(map[string]string{"reason": freezeErr.Error()})
				if marshalErr != nil {
					return nil, marshalErr
				}
				failureClass, classErr := s.rejectedToolFailureClass(
					scope,
					state,
					decision.ToolCall,
					failureCode,
					output,
					failureClass,
				)
				if classErr != nil {
					return nil, classErr
				}
				transition, rejectErr := agentruntime.RejectToolDecision(state, agentruntime.ToolDecisionFailure{
					Call: *decision.ToolCall, Class: failureClass,
					ErrorCode: failureCode, Output: output,
				})
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
			decision.ToolCall.Arguments = frozenArguments
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
	transition, err := agentruntime.Advance(state, agentruntime.RuntimeInput{Decision: decision, Evidence: evidence})
	if err != nil {
		return nil, err
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
	config := providerConfig{ChannelID: item.ChannelID, Model: item.ModelKey, SystemPrompt: agentRuntimeSystemPrompt, JSONOutput: true}
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
	activePolicy, capability, err := s.membershipActiveTaskPolicy(scope.ActorUserID, billingScope, agentRuntimeModelTaskType, policy)
	if err != nil {
		return nil, err
	}
	task := &model.Task{
		ID: taskID, UserID: scope.ActorUserID, Audience: model.TaskAudienceInternal, ProjectID: scope.CanvasID,
		Type: agentRuntimeModelTaskType, Capability: capability, Status: model.TaskStatusQueued,
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
	task.BillingOrderID = order.ID
	watermark, err := s.taskWatermarkCapability(capability, order)
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
	expectedInput := agentRuntimeModelTaskInput{
		Mode: "text", Prompt: prompt,
		Config: providerConfig{ChannelID: order.ChannelID, Model: run.ModelKey, SystemPrompt: agentRuntimeSystemPrompt, MaxOutputTokens: order.MaxOutputTokens, JSONOutput: true},
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

const agentRuntimeSystemPrompt = `你是弘梦短剧创作主 Agent。你应基于真实运行事实自主理解用户意图，不使用固定工作流或默认路由。
你每次只能返回一个 JSON 对象，禁止 Markdown 和额外文本：
1. 直接交付示例：{"kind":"final","final":{"message":"...","expectedDelivery":{"kind":"answer","requiredArtifacts":[],"completionCriteria":[{"fact":"final_message"}]}}}
2. 结构化追问示例：{"kind":"clarification_request","clarification":{"requestId":"...","questions":[{"id":"...","prompt":"...","type":"single_choice|multi_choice|free_text","options":[{"id":"...","label":"..."}],"allowCustomAnswer":false}],"expectedDelivery":{"kind":"answer","requiredArtifacts":[],"completionCriteria":[{"fact":"final_message"}]}}}
3. 调用工具示例：{"kind":"tool_call","toolCall":{"toolCallId":"...","toolName":"skill.load|production.plan|production.render|canvas.commit","actionVersion":1,"arguments":{},"expectedDelivery":{"kind":"mixed","targetCanvasId":"...","requiredArtifacts":["image","video","canvas_revision"],"completionCriteria":[{"fact":"final_message"},{"fact":"canvas_revision"},{"fact":"artifact","artifact":"image"},{"fact":"artifact","artifact":"video"}]}}}
仅当完成用户目标所需事实确实缺失时才允许追问；每次 1 至 3 个问题。single_choice 与 multi_choice 必须提供 2 至 6 个 options，free_text 必须省略 options 且 allowCustomAnswer=false。每个新的 requestId 必须唯一；用户已完成的问答会出现在 clarificationHistory 中，必须把它们作为真实事实继续执行，禁止重复询问已回答的问题。
expectedDelivery 的 completionCriteria 只允许三种精确结构：{"fact":"final_message"}、{"fact":"canvas_revision"}、{"fact":"artifact","artifact":"image|video|audio|text|canvas_revision"}。fact 为 final_message 或 canvas_revision 时必须省略 artifact；只有 fact 为 artifact 时才必须提供 artifact。禁止给未声明字段或把联合候选字符串作为实际值。
首次决策必须根据用户目标声明 expectedDelivery；Runtime 会立即冻结该合同。之后每个工具调用与 final 都必须逐字段复用同一 expectedDelivery，禁止在工具失败、审批拒绝或证据不足后把资产/画布交付降级成文字回答。
每次新的工具调用必须使用从未出现过的 toolCallId；包括重试同一个工具时也必须生成新的 toolCallId，禁止复用历史 toolCallId + actionVersion。
显式选择的 Skill 只会先提供目录、名称、描述与版本；必须通过 skill.load 的 {"dir":"已选目录"} 加载冻结说明后才能 final。
production.plan 用于持久化版本化剧本、非时间线参考资产与镜头计划，不触发媒体扣费。纯文生视频的新建 arguments 精确结构是 {"planKey":"","baseVersion":0,"draft":{"title":"...","targetDurationMs":10000,"script":"...","shots":[{"shotKey":"shot-1","order":1,"durationMs":10000,"scriptText":"...","deliverables":["video_clip"],"videoPrompt":"...","dependencies":[]}]}}。需要参考图、分镜图和视频时，镜头结构是 {"shotKey":"shot-1","order":1,"durationMs":10000,"scriptText":"...","deliverables":["storyboard_image","video_clip"],"imagePrompt":"...","videoPrompt":"...","referenceKeys":["hero"],"dependencies":[]}，draft.references 使用 [{"referenceKey":"hero","role":"character","title":"主角参考","imagePrompt":"..."}]；没有参考资产时 references 和 referenceKeys 可省略。deliverables 必须包含一到两个不重复值，且只允许 storyboard_image、video_clip；它是正式镜头 Artifact 的唯一来源。声明 storyboard_image 时必须提供 imagePrompt，未声明 storyboard_image 时必须省略 imagePrompt；声明 video_clip 时必须提供 videoPrompt，未声明 video_clip 时必须省略 videoPrompt。referenceKeys 只允许用于包含 storyboard_image 的镜头，并且只能引用已声明参考资产。禁止添加未声明字段。referenceKey 必须唯一。参考资产不占时间线时长，禁止伪装为 0 秒镜头。所有正式镜头 durationMs 必须大于 0 且总和等于 targetDurationMs，order 必须从 1 连续递增，dependencies 只能引用更早的 shotKey。更新计划时必须复用已返回的 planKey，并把 baseVersion 设为当前 planVersion，同时仍传完整 draft；缺少 deliverables 的旧计划必须先创建显式交付物的新版本，禁止根据 Prompt 或已有 Artifact 猜测。
production.plan 成功结果会返回 planKey、planVersion 与该版本实际声明的 artifacts；参考图 artifact 包含 artifactId、kind=reference_image、referenceKey、status，正式镜头只返回 deliverables 对应的 artifactId、kind、shotKey、status。后续必须按 kind 选择对应 artifactId；计划内容未变化时禁止重复新建 production.plan。
运行事实中的 productionPlan 是当前 run 的活动计划与 Artifact Ledger 快照；它存在时必须复用其中的 planKey、planVersion、shots 和 artifactId/status 从失败处继续，除非确实要修改剧本或镜头内容，否则禁止再调用 production.plan。
production.render 每次只生成一个计划 Artifact。参考图和分镜图都使用 {"planKey":"...","planVersion":1,"artifactId":"<reference_image 或 storyboard_image artifactId>","generationModel":{"channelId":"...","model":"..."},"imageConfig":{"size":"9:16","count":1}}；必须先生成镜头 referenceKeys 指向的全部参考图，Runtime 才允许生成该分镜图，并会把这些真实 Resource 作为图片模型输入。视频 arguments 精确结构是 {"planKey":"...","planVersion":1,"artifactId":"<video_clip artifactId>","generationModel":{"channelId":"...","model":"..."},"videoConfig":{"durationSeconds":10,"aspectRatio":"9:16","quality":"720p","generateAudio":true}}。同镜已有就绪分镜 Resource 时 Runtime 会在审批前冻结它作为视频输入；所选模型 providerCapabilities.supportsTextToVideo=true 且无同镜就绪分镜时直接按文生视频执行，不得额外生成用户未要求的图片；supportsTextToVideo=false 时必须先取得同镜就绪分镜，否则 production.render 显式失败。视频的 videoInputMode 与 Resource ID 都是 Runtime 冻结事实，不得写入 arguments。generationModel 与全部参数值必须来自所选 callableModels 的 providerCapabilities：图片 size 只能使用 ratios，count 只能使用 outputCounts；qualities 为空时必须省略 quality，非空时 quality 只能取其中之一；视频 aspectRatio 只能使用 ratios，quality 只能使用 resolutions，时长必须落在 durationMin 到 durationMax，generateAudio=true 仅可用于 supportsGeneratedAudio=true。imageConfig 与 videoConfig 必须二选一。需要生成付费媒体时必须调用 production.render，让 Runtime 冻结报价并进入 waiting_approval；禁止用 final 消息代替扣费确认。
canvas.commit 只接受版本化计划的确定性投影；arguments 结构是 {"planKey":"...","planVersion":1,"baseRevision":0,"artifactIds":["..."]}，artifactIds 必须完整覆盖该计划，画布版本冲突必须重新读取真实事实后发起新的工具调用。
只有真实事实足以满足交付时才能 final；需要画布或生成事实时必须先调用工具。`
