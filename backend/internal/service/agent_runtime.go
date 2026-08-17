package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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

const agentRuntimeToolSchemaVersion = 1

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
	RunID            string                              `json:"runId"`
	CanvasID         string                              `json:"canvasId"`
	StepNumber       int                                 `json:"stepNumber"`
	MaxSteps         int                                 `json:"maxSteps"`
	UserMessage      string                              `json:"userMessage"`
	ExpectedDelivery *agentruntime.ExpectedDelivery      `json:"expectedDelivery,omitempty"`
	Verification     *agentruntime.DeliveryVerification  `json:"deliveryVerification,omitempty"`
	LastToolResult   *agentruntime.ToolResult            `json:"lastToolResult,omitempty"`
	DecisionFeedback *agentruntime.ModelDecisionFeedback `json:"decisionFeedback,omitempty"`
	PreviousMessage  string                              `json:"previousMessage,omitempty"`
	Configuration    agentruntime.RunConfiguration       `json:"configuration"`
	CallableModels   []agentRuntimeCallableModelFact     `json:"callableModels"`
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
	if input.ClientRequestID == "" || input.UserMessage == "" || len(input.UserMessage) > 64*1024 || input.MaxSteps < 1 || input.MaxSteps > 24 {
		return nil, BadAuthRequest("Agent 请求事实无效")
	}
	record, err := s.repo.CreateAgentRun(repository.CreateAgentRunInput{
		Scope: input.Scope, ClientRequestID: input.ClientRequestID, Now: time.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	scope := input.Scope
	scope.RunID = record.Run.ID
	run := record.Run
	if run.MaxSteps == 0 {
		configuration, resolveErr := s.resolveAgentRuntimeConfiguration(input.Context, input.Scope.ActorUserID, input.Configuration)
		if resolveErr != nil {
			return nil, resolveErr
		}
		selected, selectErr := s.agentRuntimeDefaultModel()
		if selectErr != nil {
			return nil, selectErr
		}
		initialized, initializeErr := s.repo.InitializeAgentRun(repository.InitializeAgentRunInput{
			Scope: scope, ModelRecordID: selected.ID, ModelKey: selected.ModelKey,
			MaxSteps: input.MaxSteps, ToolSchemaVersion: agentRuntimeToolSchemaVersion,
			UserMessage: input.UserMessage, Configuration: configuration, Now: time.Now().UTC(),
		})
		if initializeErr != nil {
			return nil, initializeErr
		}
		run = initialized.Run
	}
	state, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	if state.UserMessage != input.UserMessage || state.MaxSteps != input.MaxSteps || !agentRuntimeConfigurationMatchesInput(state.Configuration, input.Configuration) {
		return nil, errors.New("agent runtime request facts conflict")
	}
	switch state.Status {
	case agentruntime.RunSucceeded, agentruntime.RunFailed, agentruntime.RunCancelled, agentruntime.RunWaitingApproval, agentruntime.RunWaitingTool:
		return &AgentRuntimeProgress{Run: run, State: state}, nil
	case agentruntime.RunQueued, agentruntime.RunRunning:
	default:
		return nil, errors.New("agent runtime status is invalid")
	}
	task, err := s.ensureAgentRuntimeModelTask(scope, run, state)
	if err != nil {
		return nil, err
	}
	return &AgentRuntimeProgress{Run: run, State: state, ModelTask: taskForOutput(*task)}, nil
}

func (s *Service) ResumeAgentRuntime(scope agentruntime.Scope) (*AgentRuntimeProgress, error) {
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
	state, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	if state.Status == agentruntime.RunSucceeded || state.Status == agentruntime.RunFailed || state.Status == agentruntime.RunCancelled || state.Status == agentruntime.RunWaitingApproval || state.Status == agentruntime.RunWaitingTool {
		return &AgentRuntimeProgress{Run: *run, State: state}, nil
	}
	taskID := agentRuntimeModelTaskID(scope.RunID, state.StepNumber)
	task, err := s.repo.TaskForUser(scope.ActorUserID, taskID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		task, err = s.ensureAgentRuntimeModelTask(scope, *run, state)
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
	activePolicy, capability, err := s.membershipActiveTaskPolicy(scope.ActorUserID, agentRuntimeModelTaskType, policy)
	if err != nil {
		return nil, err
	}
	task := &model.Task{
		ID: taskID, UserID: scope.ActorUserID, ProjectID: scope.CanvasID,
		Type: agentRuntimeModelTaskType, Capability: capability, Status: model.TaskStatusQueued,
		Stage: "等待 Agent 模型调度", Progress: 5, Prompt: prompt, Operation: agentRuntimeModelOperation(scope.RunID),
		Provider: "system", Model: item.ModelKey, InputJSON: string(encodedInput),
	}
	if err := s.ensureTaskProjectActive(scope.ActorUserID, scope.CanvasID); err != nil {
		return nil, err
	}
	var order *model.BillingOrder
	if tokenBilled {
		order, err = s.newTokenBillingOrder(scope.ActorUserID, item.ChannelID, item.ModelKey, "agent_runtime_model", agentRuntimeBillingKey(scope.RunID, state.StepNumber), tokenReservation)
	} else {
		order, err = s.newBillingOrder(scope.ActorUserID, task.ID, agentRuntimeBillingKey(scope.RunID, state.StepNumber), item.ChannelID, item.ModelKey, "text", "agent_runtime_model", BillingUsage{Quantity: 1})
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
	if order.UserID != run.ActorUserID || order.TaskID != task.ID || order.IdempotencyKey != expectedBillingKey ||
		order.ChannelModelID != run.ModelRecordID || order.Model != run.ModelKey || order.Capability != "text" ||
		order.Scene != "agent_runtime_model" || order.Quantity != 1 || order.AmountMicrocredits <= 0 {
		return nil, errors.New("agent runtime billing facts conflict")
	}
	prompt := task.Prompt
	if err := validateFrozenAgentRuntimeModelPrompt(scope, state, prompt); err != nil {
		return nil, err
	}
	expectedInput, err := json.Marshal(agentRuntimeModelTaskInput{
		Mode: "text", Prompt: prompt,
		Config: providerConfig{ChannelID: order.ChannelID, Model: run.ModelKey, SystemPrompt: agentRuntimeSystemPrompt, MaxOutputTokens: order.MaxOutputTokens, JSONOutput: true},
	})
	if err != nil {
		return nil, err
	}
	if task.Prompt != prompt || task.InputJSON != string(expectedInput) {
		return nil, errors.New("agent runtime model task input facts conflict")
	}
	return task, nil
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
1. 直接交付：{"kind":"final","final":{"message":"...","expectedDelivery":{"kind":"answer|canvas_change|generated_asset|mixed","targetCanvasId":"...","requiredArtifacts":["image|video|audio|text|canvas_revision"],"completionCriteria":[{"fact":"final_message|canvas_revision|artifact","artifact":"image|video|audio|text|canvas_revision"}]}}}
2. 调用工具：{"kind":"tool_call","toolCall":{"toolCallId":"...","toolName":"canvas.read_state|canvas.read_selection|canvas.apply_ops|generation.submit|generation.wait","actionVersion":1,"arguments":{},"expectedDelivery":{"kind":"answer|canvas_change|generated_asset|mixed","targetCanvasId":"...","requiredArtifacts":["image|video|audio|text|canvas_revision"],"completionCriteria":[{"fact":"final_message|canvas_revision|artifact","artifact":"image|video|audio|text|canvas_revision"}]}}}
首次决策必须根据用户目标声明 expectedDelivery；Runtime 会立即冻结该合同。之后每个工具调用与 final 都必须逐字段复用同一 expectedDelivery，禁止在工具失败、审批拒绝或证据不足后把资产/画布交付降级成文字回答。
每次新的工具调用必须使用从未出现过的 toolCallId；包括重试同一个工具时也必须生成新的 toolCallId，禁止复用历史 toolCallId + actionVersion。
canvas.read_state 的 arguments 结构是 {} 或 {"expectedRevision":0}；画布身份已由运行作用域冻结，禁止填写 canvasId 或其他字段。canvas.read_selection 的 arguments 必须是 {}。
canvas.apply_ops 的 arguments 结构是 {"baseRevision":0,"patch":{"upsertNodes":[],"deleteNodeIds":[],"upsertConnections":[],"deleteConnectionIds":[],"document":{}}}；baseRevision 必须是当前非负版本，只填写本次实际需要的 patch 字段，节点和连线必须包含稳定 id。
generation.submit 的 arguments 结构是 {"type":"canvas_image|canvas_video|canvas_audio","prompt":"真实生成提示词","input":{"mode":"image|video|audio","config":{}}}；type 与 input.mode 必须对应，input.config 只能使用本轮 callableModels 中同一条记录公开的 channelId、model 与能力参数，不得猜测模型、价格或默认配置。图片生成 config 必须使用规范字段，例如 {"channelId":"...","model":"...","size":"16:9","count":"1","transparentBackground":"false"}，禁止使用 ratio 或 resolution。quality 不是公共必填字段；只有所选 callableModels 记录的 providerCapabilities.qualities 列出非空候选时，才可从候选中选择并填写，否则必须省略 quality，禁止使用示例值或默认值。提交成功后必须保存返回的 taskId，并使用 generation.wait 等待同一任务。
generation.wait 的 arguments 结构是 {"taskId":"generation.submit 返回的 taskId"}；只有返回 succeeded 且包含真实资产 URL 时才构成交付事实，queued 或 running 表示仍在等待，不得重复提交生成任务。
只有真实事实足以满足交付时才能 final；需要画布或生成事实时必须先调用工具。`
