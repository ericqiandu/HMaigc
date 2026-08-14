package service

import (
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
const agentRuntimeToolSchemaVersion = 1

type StartAgentRuntimeInput struct {
	Scope           agentruntime.Scope
	ClientRequestID string
	UserMessage     string
	MaxSteps        int
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
	RunID            string                             `json:"runId"`
	CanvasID         string                             `json:"canvasId"`
	StepNumber       int                                `json:"stepNumber"`
	MaxSteps         int                                `json:"maxSteps"`
	UserMessage      string                             `json:"userMessage"`
	ExpectedDelivery *agentruntime.ExpectedDelivery     `json:"expectedDelivery,omitempty"`
	Verification     *agentruntime.DeliveryVerification `json:"deliveryVerification,omitempty"`
	LastToolResult   *agentruntime.ToolResult           `json:"lastToolResult,omitempty"`
	PreviousMessage  string                             `json:"previousMessage,omitempty"`
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
		selected, selectErr := s.agentRuntimeDefaultModel()
		if selectErr != nil {
			return nil, selectErr
		}
		initialized, initializeErr := s.repo.InitializeAgentRun(repository.InitializeAgentRunInput{
			Scope: scope, ModelRecordID: selected.ID, ModelKey: selected.ModelKey,
			MaxSteps: input.MaxSteps, ToolSchemaVersion: agentRuntimeToolSchemaVersion,
			UserMessage: input.UserMessage, Now: time.Now().UTC(),
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
	if state.UserMessage != input.UserMessage || state.MaxSteps != input.MaxSteps {
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
		transition, transitionErr := agentruntime.Fail(state, "model_decision_invalid")
		if transitionErr != nil {
			return nil, errors.Join(err, transitionErr)
		}
		return s.commitAgentRuntimeState(scope, state, transition)
	}
	evidence := agentruntime.DeliveryEvidence{}
	if decision.Final != nil {
		evidence.FinalMessage = decision.Final.Message
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
	prompt, err := agentRuntimeModelPrompt(scope, state)
	if err != nil {
		return nil, err
	}
	config := providerConfig{ChannelID: item.ChannelID, Model: item.ModelKey, SystemPrompt: agentRuntimeSystemPrompt}
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
		Stage: "等待 Agent 模型调度", Progress: 5, Prompt: prompt, Operation: "agent_runtime_model",
		Provider: "system", Model: item.ModelKey, InputJSON: string(encodedInput),
	}
	if err := s.ensureTaskProjectActive(scope.ActorUserID, scope.CanvasID); err != nil {
		return nil, err
	}
	order, err := s.newBillingOrder(scope.ActorUserID, task.ID, agentRuntimeBillingKey(scope.RunID, state.StepNumber), item.ChannelID, item.ModelKey, "text", "agent_runtime_model", BillingUsage{Quantity: 1})
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
		return nil, BadAuthRequest(creditInsufficientMessage(order.TeamID))
	}
	if errors.Is(err, repository.ErrTeamMemberCreditLimit) {
		return nil, BadAuthRequest("本月团队积分额度已用尽，请联系团队管理员调整额度")
	}
	if errors.Is(err, repository.ErrActiveTaskLimit) || errors.Is(err, repository.ErrCapabilityTaskLimit) {
		return nil, BadAuthRequest("当前 Agent 任务并发额度已用尽，请等待已有任务完成")
	}
	return nil, err
}

func (s *Service) validateAgentRuntimeModelTask(scope agentruntime.Scope, task *model.Task, run model.AgentRun, state agentruntime.RuntimeState) (*model.Task, error) {
	if task == nil || task.ID != agentRuntimeModelTaskID(run.ID, state.StepNumber) || task.UserID != run.ActorUserID ||
		task.ProjectID != scope.CanvasID || task.Type != agentRuntimeModelTaskType || strings.TrimSpace(task.Capability) == "" ||
		task.Model != run.ModelKey || task.Operation != "agent_runtime_model" || task.Provider != "system" ||
		task.ProviderAccountID == "" || task.ProviderEndpointVersionID == "" || task.ProviderCredentialVersionID == "" {
		return nil, errors.New("agent runtime model task facts conflict")
	}
	order, err := s.repo.BillingOrder(task.BillingOrderID)
	if err != nil {
		return nil, err
	}
	if order.UserID != run.ActorUserID || order.TaskID != task.ID || order.IdempotencyKey != agentRuntimeBillingKey(run.ID, state.StepNumber) ||
		order.ChannelModelID != run.ModelRecordID || order.Model != run.ModelKey || order.Capability != "text" ||
		order.Scene != "agent_runtime_model" || order.Quantity != 1 || order.AmountMicrocredits <= 0 {
		return nil, errors.New("agent runtime billing facts conflict")
	}
	prompt, err := agentRuntimeModelPrompt(scope, state)
	if err != nil {
		return nil, err
	}
	expectedInput, err := json.Marshal(agentRuntimeModelTaskInput{
		Mode: "text", Prompt: prompt,
		Config: providerConfig{ChannelID: order.ChannelID, Model: run.ModelKey, SystemPrompt: agentRuntimeSystemPrompt},
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

func agentRuntimeModelPrompt(scope agentruntime.Scope, state agentruntime.RuntimeState) (string, error) {
	context := agentRuntimeModelContext{
		RunID: scope.RunID, CanvasID: scope.CanvasID, StepNumber: state.StepNumber, MaxSteps: state.MaxSteps,
		UserMessage: state.UserMessage, ExpectedDelivery: state.ExpectedDelivery,
		Verification: state.Verification, LastToolResult: state.LastToolResult, PreviousMessage: state.FinalMessage,
	}
	encoded, err := json.Marshal(context)
	if err != nil {
		return "", err
	}
	return "以下 JSON 是本轮唯一可信的运行事实。请自主决定直接交付或调用一个可用工具，并严格按系统约定返回一个 JSON 对象：\n" + string(encoded), nil
}

const agentRuntimeSystemPrompt = `你是弘梦短剧创作主 Agent。你应基于真实运行事实自主理解用户意图，不使用固定工作流或默认路由。
你每次只能返回一个 JSON 对象，禁止 Markdown 和额外文本：
1. 直接交付：{"kind":"final","final":{"message":"...","expectedDelivery":{"kind":"answer|canvas_change|generated_asset|mixed","targetCanvasId":"...","requiredArtifacts":["image|video|audio|text|canvas_revision"],"completionCriteria":[{"fact":"final_message|canvas_revision|artifact","artifact":"image|video|audio|text|canvas_revision"}]}}}
2. 调用工具：{"kind":"tool_call","toolCall":{"toolCallId":"...","toolName":"canvas.read_state|canvas.read_selection|canvas.apply_ops|generation.submit|generation.wait","actionVersion":1,"arguments":{}}}
只有真实事实足以满足交付时才能 final；需要画布或生成事实时必须先调用工具。`
