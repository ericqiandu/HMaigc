package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

type StartScopedAgentRunInput struct {
	Context         context.Context
	ClientRequestID string
	UserMessage     string
	MaxSteps        int
	Configuration   AgentRuntimeConfigurationInput
}

type AgentRuntimeView struct {
	Run   model.AgentRun            `json:"run"`
	State agentruntime.RuntimeState `json:"state"`
}

func (view AgentRuntimeView) MarshalJSON() ([]byte, error) {
	type wireAgentRuntimeView AgentRuntimeView
	if !agentRuntimeViewNeedsHistoricalConfiguration(view) {
		return json.Marshal(wireAgentRuntimeView(view))
	}
	projected := view
	projected.State.ClarificationHistory = []agentruntime.CompletedClarification{}
	if projected.State.Configuration.Skills == nil {
		projected.State.Configuration.Skills = []agentruntime.SkillSelection{}
	}
	if projected.State.Configuration.Attachments == nil {
		projected.State.Configuration.Attachments = []agentruntime.ResourceAttachment{}
	}
	if projected.State.Configuration.ExecutionMode == "" {
		projected.State.Configuration.ExecutionMode = agentruntime.ExecutionMode("historical")
	}
	return json.Marshal(wireAgentRuntimeView(projected))
}

func agentRuntimeViewNeedsHistoricalConfiguration(view AgentRuntimeView) bool {
	terminal := view.Run.Status == agentruntime.RunSucceeded || view.Run.Status == agentruntime.RunFailed || view.Run.Status == agentruntime.RunCancelled
	return terminal && view.Run.ToolSchemaVersion == 1 && view.Run.RuntimeVersion == 1 && view.Run.PolicyVersion == 1 &&
		view.State.ClarificationHistory == nil
}

const CurrentAgentUIProtocolVersion = 2

var ErrAgentEventProjectionFailed = errors.New("agent event projection failed")
var ErrAgentStreamCursorInvalid = errors.New("agent stream cursor invalid")

type AgentUIEventKind string

const (
	AgentUIEventRunStarted        AgentUIEventKind = "run.started"
	AgentUIEventRunCompleted      AgentUIEventKind = "run.completed"
	AgentUIEventRunFailed         AgentUIEventKind = "run.failed"
	AgentUIEventRunInterrupted    AgentUIEventKind = "run.interrupted"
	AgentUIEventItemStarted       AgentUIEventKind = "item.started"
	AgentUIEventItemDelta         AgentUIEventKind = "item.delta"
	AgentUIEventItemCompleted     AgentUIEventKind = "item.completed"
	AgentUIEventItemFailed        AgentUIEventKind = "item.failed"
	AgentUIEventApprovalRequested AgentUIEventKind = "approval.requested"
	AgentUIEventApprovalResolved  AgentUIEventKind = "approval.resolved"
	AgentUIEventStateSnapshot     AgentUIEventKind = "state.snapshot"
)

type AgentUIEvent struct {
	ProtocolVersion int                         `json:"protocolVersion"`
	ThreadID        string                      `json:"threadId"`
	RunID           string                      `json:"runId"`
	Sequence        int64                       `json:"sequence"`
	Kind            AgentUIEventKind            `json:"kind"`
	ItemID          string                      `json:"itemId,omitempty"`
	ItemKind        model.AgentTimelineItemKind `json:"itemKind,omitempty"`
	Payload         json.RawMessage             `json:"payload"`
	CreatedAt       time.Time                   `json:"createdAt"`
}

type agentArtifactTimelinePayload struct {
	ArtifactID     string                              `json:"artifactId"`
	Kind           model.AgentProductionArtifactKind   `json:"kind"`
	PlanKey        string                              `json:"planKey"`
	PlanVersion    int                                 `json:"planVersion"`
	ReferenceKey   string                              `json:"referenceKey,omitempty"`
	ShotKey        string                              `json:"shotKey,omitempty"`
	TaskID         string                              `json:"taskId,omitempty"`
	BillingOrderID string                              `json:"billingOrderId,omitempty"`
	ResourceID     string                              `json:"resourceId,omitempty"`
	Status         model.AgentProductionArtifactStatus `json:"status"`
}

type agentArtifactUIEventPayload struct {
	ArtifactID   string                              `json:"artifactId"`
	Kind         model.AgentProductionArtifactKind   `json:"kind"`
	PlanKey      string                              `json:"planKey"`
	PlanVersion  int                                 `json:"planVersion"`
	ReferenceKey string                              `json:"referenceKey,omitempty"`
	ShotKey      string                              `json:"shotKey,omitempty"`
	ResourceID   string                              `json:"resourceId"`
	Status       model.AgentProductionArtifactStatus `json:"status"`
}

func ProjectAgentEvent(threadID string, event model.AgentRunEvent, item *model.AgentTimelineItem, protocolVersion int) (AgentUIEvent, error) {
	threadID = strings.TrimSpace(threadID)
	if protocolVersion != CurrentAgentUIProtocolVersion || threadID == "" || strings.TrimSpace(event.RunID) == "" ||
		event.Sequence < 1 || event.CreatedAt.IsZero() || !json.Valid([]byte(event.PayloadJSON)) {
		return AgentUIEvent{}, errors.Join(ErrAgentEventProjectionFailed, errors.New("agent event projection boundary is invalid"))
	}
	projected := AgentUIEvent{
		ProtocolVersion: protocolVersion, ThreadID: threadID, RunID: event.RunID,
		Sequence: event.Sequence, CreatedAt: event.CreatedAt,
	}
	switch event.Kind {
	case agentruntime.EventRunCreated:
		return projectAgentRunEvent(projected, event, item, AgentUIEventRunStarted, false)
	case agentruntime.EventRunCompleted:
		return projectAgentRunEvent(projected, event, item, AgentUIEventRunCompleted, true)
	case agentruntime.EventRunFailed:
		return projectAgentRunEvent(projected, event, item, AgentUIEventRunFailed, true)
	case agentruntime.EventRunInterrupted:
		return projectAgentRunEvent(projected, event, item, AgentUIEventRunInterrupted, true)
	case agentruntime.EventRunStatusChanged:
		return projectAgentRunEvent(projected, event, item, AgentUIEventStateSnapshot, true)
	case agentruntime.EventCheckpointSaved:
		return projectAgentRunEvent(projected, event, item, AgentUIEventStateSnapshot, false)
	case agentruntime.EventUserMessageAdded, agentruntime.EventRunSteered,
		agentruntime.EventAgentMessageCompleted, agentruntime.EventClarificationResponded,
		agentruntime.EventArtifactAvailable:
		projected.Kind = AgentUIEventItemCompleted
	case agentruntime.EventAgentMessageFailed:
		projected.Kind = AgentUIEventItemFailed
	case agentruntime.EventClarificationRequested, agentruntime.EventToolCall:
		projected.Kind = AgentUIEventItemStarted
	case agentruntime.EventClarificationAnswerSaved, agentruntime.EventToolStarted:
		projected.Kind = AgentUIEventItemDelta
	case agentruntime.EventToolResult:
		if item == nil {
			return AgentUIEvent{}, errors.Join(ErrAgentEventProjectionFailed, errors.New("agent tool result item is missing"))
		}
		if item.Status == model.AgentTimelineItemFailed || item.Status == model.AgentTimelineItemDeclined {
			projected.Kind = AgentUIEventItemFailed
		} else {
			projected.Kind = AgentUIEventItemCompleted
		}
	case agentruntime.EventApprovalRequired:
		projected.Kind = AgentUIEventApprovalRequested
	case agentruntime.EventApprovalDecided:
		projected.Kind = AgentUIEventApprovalResolved
	case agentruntime.EventModelRejected:
		projected.Kind = AgentUIEventItemFailed
	case agentruntime.EventModelDelta:
		return projectAgentVisibleModelDelta(projected, event, item)
	default:
		return AgentUIEvent{}, errors.Join(ErrAgentEventProjectionFailed, errors.New("agent event kind is unsupported"))
	}
	return projectAgentItemEvent(projected, event, item)
}

func projectAgentRunEvent(projected AgentUIEvent, event model.AgentRunEvent, item *model.AgentTimelineItem, kind AgentUIEventKind, requireItem bool) (AgentUIEvent, error) {
	state, err := decodeAgentRuntimeState(event.PayloadJSON)
	if err != nil {
		return AgentUIEvent{}, errors.Join(ErrAgentEventProjectionFailed, err)
	}
	type runItemPayload struct {
		Kind    model.AgentTimelineItemKind   `json:"kind"`
		Status  model.AgentTimelineItemStatus `json:"status"`
		Content json.RawMessage               `json:"content"`
	}
	var projectedItem *runItemPayload
	if item != nil {
		if err := validateAgentProjectedItem(projected, event, item); err != nil {
			return AgentUIEvent{}, err
		}
		projected.ItemID = item.ID
		projectedItem = &runItemPayload{Kind: item.Kind, Status: item.Status, Content: json.RawMessage(item.ContentJSON)}
	} else if requireItem {
		return AgentUIEvent{}, errors.Join(ErrAgentEventProjectionFailed, errors.New("agent run event item is missing"))
	}
	payload, err := json.Marshal(struct {
		Status       agentruntime.RunStatus `json:"status"`
		StateVersion int                    `json:"stateVersion"`
		FailureCode  string                 `json:"failureCode,omitempty"`
		Item         *runItemPayload        `json:"item,omitempty"`
	}{Status: state.Status, StateVersion: state.StateVersion, FailureCode: state.FailureCode, Item: projectedItem})
	if err != nil {
		return AgentUIEvent{}, errors.Join(ErrAgentEventProjectionFailed, err)
	}
	projected.Kind = kind
	projected.Payload = payload
	return projected, nil
}

func projectAgentItemEvent(projected AgentUIEvent, event model.AgentRunEvent, item *model.AgentTimelineItem) (AgentUIEvent, error) {
	if item == nil {
		return AgentUIEvent{}, errors.Join(ErrAgentEventProjectionFailed, errors.New("agent event item is missing"))
	}
	if err := validateAgentProjectedItem(projected, event, item); err != nil {
		return AgentUIEvent{}, err
	}
	projected.ItemID = item.ID
	projected.ItemKind = item.Kind
	if item.Kind != model.AgentTimelineItemArtifact {
		projected.Payload = append(json.RawMessage(nil), item.ContentJSON...)
		return projected, nil
	}
	internal, err := decodeAgentArtifactTimelinePayload(item.ContentJSON)
	if err != nil || internal.ArtifactID == "" ||
		internal.PlanKey == "" || internal.PlanVersion < 1 || internal.ResourceID == "" || !internal.Status.Valid() {
		return AgentUIEvent{}, errors.Join(ErrAgentEventProjectionFailed, errors.New("agent artifact timeline facts are invalid"))
	}
	safePayload, err := json.Marshal(agentArtifactUIEventPayload{
		ArtifactID: internal.ArtifactID, Kind: internal.Kind, PlanKey: internal.PlanKey,
		PlanVersion: internal.PlanVersion, ReferenceKey: internal.ReferenceKey, ShotKey: internal.ShotKey,
		ResourceID: internal.ResourceID, Status: internal.Status,
	})
	if err != nil {
		return AgentUIEvent{}, errors.Join(ErrAgentEventProjectionFailed, err)
	}
	projected.Payload = safePayload
	return projected, nil
}

func validateAgentProjectedItem(projected AgentUIEvent, event model.AgentRunEvent, item *model.AgentTimelineItem) error {
	if strings.TrimSpace(item.ID) == "" || item.ThreadID != projected.ThreadID || item.RunID != event.RunID ||
		item.SourceEventSequence != event.Sequence || !item.Kind.Valid() || !item.Status.Valid() || !json.Valid([]byte(item.ContentJSON)) {
		return errors.Join(ErrAgentEventProjectionFailed, errors.New("agent event item facts are invalid"))
	}
	return nil
}

func projectAgentVisibleModelDelta(projected AgentUIEvent, event model.AgentRunEvent, item *model.AgentTimelineItem) (AgentUIEvent, error) {
	if item == nil || item.Kind != model.AgentTimelineItemAgentMessage || item.Status != model.AgentTimelineItemInProgress {
		return AgentUIEvent{}, errors.Join(ErrAgentEventProjectionFailed, errors.New("agent model delta is not bound to a visible message"))
	}
	payload, err := decodeAgentVisibleModelDeltaPayload(event.PayloadJSON)
	if err != nil || !payload.UserVisible || payload.Delta == "" || payload.ItemID == "" || payload.ItemID != item.ID {
		return AgentUIEvent{}, errors.Join(ErrAgentEventProjectionFailed, errors.New("agent model delta is not user visible"))
	}
	projected.Kind = AgentUIEventItemDelta
	if payload.Started {
		projected.Kind = AgentUIEventItemStarted
	}
	projected.ItemID = payload.ItemID
	projected.ItemKind = item.Kind
	projected.Payload = json.RawMessage(event.PayloadJSON)
	return projected, nil
}

func decodeAgentArtifactTimelinePayload(raw string) (agentArtifactTimelinePayload, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload agentArtifactTimelinePayload
	if err := decoder.Decode(&payload); err != nil {
		return agentArtifactTimelinePayload{}, err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return agentArtifactTimelinePayload{}, errors.New("agent event payload has trailing data")
	}
	return payload, nil
}

type agentVisibleModelDeltaPayload struct {
	ItemID      string `json:"itemId"`
	Delta       string `json:"delta"`
	UserVisible bool   `json:"userVisible"`
	Started     bool   `json:"started,omitempty"`
}

func decodeAgentVisibleModelDeltaPayload(raw string) (agentVisibleModelDeltaPayload, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload agentVisibleModelDeltaPayload
	if err := decoder.Decode(&payload); err != nil {
		return agentVisibleModelDeltaPayload{}, err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return agentVisibleModelDeltaPayload{}, errors.New("agent event payload has trailing data")
	}
	return payload, nil
}

type AgentClarificationError struct {
	Status             int    `json:"-"`
	ErrorCode          string `json:"errorCode"`
	Message            string `json:"-"`
	LatestStateVersion int    `json:"latestStateVersion,omitempty"`
}

type AgentControlError struct {
	Status             int    `json:"-"`
	ErrorCode          string `json:"errorCode"`
	Message            string `json:"-"`
	LatestStateVersion int    `json:"latestStateVersion,omitempty"`
}

func (err *AgentControlError) Error() string {
	return err.Message
}

func (err *AgentClarificationError) Error() string {
	return err.Message
}

func (s *Service) CreateAgentThread(actor *model.User, canvasID string) (*model.AgentThread, error) {
	if actor == nil {
		return nil, Unauthorized("请先登录")
	}
	threadID := newID()
	scope, err := s.AuthorizeAgentScope(actor.ID, strings.TrimSpace(canvasID), threadID, "thread-creation-probe")
	if err != nil {
		return nil, err
	}
	if !scope.CanMutateCanvas() {
		return nil, Forbidden("当前用户没有创建 Agent 对话的画布权限")
	}
	return s.repo.CreateAgentThread(scope, time.Now().UTC())
}

func (s *Service) StartScopedAgentRun(actor *model.User, threadID string, input StartScopedAgentRunInput) (*AgentRuntimeView, error) {
	if actor == nil {
		return nil, Unauthorized("请先登录")
	}
	thread, err := s.agentThreadForActor(actor.ID, threadID)
	if err != nil {
		return nil, err
	}
	scope, err := s.scopeForAgentThread(actor.ID, thread, newID())
	if err != nil {
		return nil, err
	}
	progress, err := s.StartAgentRuntime(StartAgentRuntimeInput{
		Context: input.Context, Scope: scope, ClientRequestID: input.ClientRequestID,
		UserMessage: input.UserMessage, MaxSteps: input.MaxSteps, Configuration: input.Configuration,
	})
	if err != nil {
		return nil, s.mapAgentItemStateConflict(scope, err)
	}
	return agentRuntimeView(progress), nil
}

func (s *Service) ReadScopedAgentRun(actor *model.User, runID string) (*AgentRuntimeView, error) {
	scope, err := s.scopeForAgentRun(actor, runID)
	if err != nil {
		return nil, err
	}
	return s.readAgentRuntimeView(scope)
}

func (s *Service) readAgentRuntimeView(scope agentruntime.Scope) (*AgentRuntimeView, error) {
	run, err := s.repo.AgentRunForScope(scope)
	if err != nil {
		return nil, err
	}
	state, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	return agentRuntimeViewFromFacts(*run, state)
}

func agentRuntimeViewFromFacts(run model.AgentRun, state agentruntime.RuntimeState) (*AgentRuntimeView, error) {
	if state.StateVersion != run.StateVersion || state.StepNumber != run.StepNumber || state.MaxSteps != run.MaxSteps || state.Status != run.Status {
		return nil, errors.New("agent checkpoint state is inconsistent")
	}
	return &AgentRuntimeView{Run: run, State: state}, nil
}

func (s *Service) ReadScopedAgentEvents(actor *model.User, runID string, afterSequence int64, limit int) ([]AgentUIEvent, *AgentRuntimeView, error) {
	scope, err := s.scopeForAgentRun(actor, runID)
	if err != nil {
		return nil, nil, err
	}
	view, err := s.readAgentRuntimeView(scope)
	if err != nil {
		return nil, nil, err
	}
	if err := validateAgentStreamCursor(afterSequence, view.Run.LastEventSequence); err != nil {
		return nil, nil, err
	}
	records, err := s.repo.AgentTimelineEventsAfter(scope, afterSequence, limit)
	if err != nil {
		return nil, nil, err
	}
	result := make([]AgentUIEvent, 0, len(records))
	for _, record := range records {
		projected, err := ProjectAgentEvent(scope.ThreadID, record.Event, record.Item, CurrentAgentUIProtocolVersion)
		if err != nil {
			return nil, nil, errors.Join(ErrAgentEventProjectionFailed, err)
		}
		result = append(result, projected)
	}
	return result, view, nil
}

func validateAgentStreamCursor(afterSequence int64, lastEventSequence int64) error {
	if afterSequence < 0 || lastEventSequence < 0 || afterSequence > lastEventSequence {
		return ErrAgentStreamCursorInvalid
	}
	return nil
}

func (s *Service) SubmitScopedAgentApproval(actor *model.User, runID string, input AgentToolApprovalSubmission) (*AgentRuntimeView, error) {
	scope, err := s.scopeForAgentRun(actor, runID)
	if err != nil {
		return nil, err
	}
	progress, err := s.SubmitAgentToolApproval(scope, input)
	if err != nil {
		return nil, s.mapAgentItemStateConflict(scope, err)
	}
	return agentRuntimeView(progress), nil
}

func (s *Service) SubmitScopedAgentClarificationResponse(actor *model.User, runID string, requestID string, submission agentruntime.ClarificationResponseSubmission) (*AgentRuntimeView, error) {
	scope, err := s.scopeForAgentRun(actor, runID)
	if err != nil {
		return nil, err
	}
	submission.RequestID = strings.TrimSpace(requestID)
	progress, err := s.SubmitAgentClarificationResponse(scope, submission)
	if err != nil {
		return nil, s.mapAgentItemStateConflict(scope, err)
	}
	return agentRuntimeView(progress), nil
}

func (s *Service) SubmitScopedAgentSteer(actor *model.User, runID string, request agentruntime.SteerRequest) (*AgentRuntimeView, error) {
	scope, err := s.scopeForAgentRun(actor, runID)
	if err != nil {
		return nil, err
	}
	if !scope.CanMutateCanvas() {
		return nil, Forbidden("当前用户没有追加 Agent 指令的画布权限")
	}
	request.ClientRequestID = strings.TrimSpace(request.ClientRequestID)
	request.Message = strings.TrimSpace(request.Message)
	if request.ClientRequestID == "" || len(request.ClientRequestID) > 120 || request.Message == "" ||
		len(request.Message) > 64*1024 || request.ExpectedStateVersion < 1 {
		return nil, &AgentControlError{
			Status: http.StatusBadRequest, ErrorCode: "agent_steer_conflict", Message: "Agent 追加指令格式无效",
		}
	}
	state, _, err := s.repo.AppendAgentSteer(scope, request, time.Now().UTC())
	if err != nil {
		return nil, s.mapAgentControlError(scope, err, "agent_steer_conflict", "Agent 追加指令状态已经变化，请按最新状态重试")
	}
	return s.agentRuntimeViewForState(scope, state)
}

func (s *Service) SubmitScopedAgentInterrupt(actor *model.User, runID string, expectedStateVersion int) (*AgentRuntimeView, error) {
	scope, err := s.scopeForAgentRun(actor, runID)
	if err != nil {
		return nil, err
	}
	if !scope.CanMutateCanvas() {
		return nil, Forbidden("当前用户没有停止 Agent 的画布权限")
	}
	if expectedStateVersion < 1 {
		return nil, &AgentControlError{
			Status: http.StatusBadRequest, ErrorCode: "agent_interrupt_conflict", Message: "Agent 停止请求格式无效",
		}
	}
	state, err := s.repo.InterruptAgentRun(scope, expectedStateVersion, time.Now().UTC())
	if err != nil {
		return nil, s.mapAgentControlError(scope, err, "agent_interrupt_conflict", "Agent 运行状态已经变化，请按最新状态重试")
	}
	s.cancelActiveTask(agentRuntimeModelTaskID(scope.RunID, state.StepNumber))
	return s.agentRuntimeViewForState(scope, state)
}

func (s *Service) agentRuntimeViewForState(scope agentruntime.Scope, state agentruntime.RuntimeState) (*AgentRuntimeView, error) {
	run, err := s.repo.AgentRunForScope(scope)
	if err != nil {
		return nil, err
	}
	return agentRuntimeViewFromFacts(*run, state)
}

func (s *Service) mapAgentControlError(scope agentruntime.Scope, err error, errorCode string, message string) error {
	if errors.Is(err, repository.ErrAgentTimelineConflict) {
		return s.mapAgentItemStateConflict(scope, err)
	}
	if !errors.Is(err, agentruntime.ErrSteerConflict) && !errors.Is(err, agentruntime.ErrInterruptConflict) &&
		!errors.Is(err, repository.ErrAgentRuntimeStepConflict) {
		return err
	}
	latest, loadErr := s.repo.LoadAgentCheckpoint(scope)
	if loadErr != nil {
		return errors.Join(err, loadErr)
	}
	return &AgentControlError{
		Status: http.StatusConflict, ErrorCode: errorCode, Message: message, LatestStateVersion: latest.StateVersion,
	}
}

func (s *Service) mapAgentItemStateConflict(scope agentruntime.Scope, err error) error {
	if !errors.Is(err, repository.ErrAgentTimelineConflict) {
		return err
	}
	latest, loadErr := s.repo.LoadAgentCheckpoint(scope)
	if loadErr != nil {
		return errors.Join(err, loadErr)
	}
	return &AgentControlError{
		Status: http.StatusConflict, ErrorCode: "agent_item_state_conflict",
		Message: "Agent 时间线状态已经变化，请重新读取后重试", LatestStateVersion: latest.StateVersion,
	}
}

func (s *Service) SubmitAgentClarificationResponse(scope agentruntime.Scope, submission agentruntime.ClarificationResponseSubmission) (*AgentRuntimeProgress, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if !scope.CanMutateCanvas() {
		return nil, Forbidden("当前用户没有提交 Agent 追问回答的画布权限")
	}
	current, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	transition, replayed, err := agentruntime.ApplyClarificationResponse(current, submission)
	if err != nil {
		return nil, mapAgentClarificationError(err, current.StateVersion)
	}
	if replayed {
		return s.agentRuntimeProgressForCurrentState(scope, current)
	}
	if err := s.repo.CommitAgentRuntimeTransition(scope, current, transition, time.Now().UTC()); err != nil {
		if !errors.Is(err, repository.ErrAgentRuntimeStepConflict) {
			return nil, err
		}
		latest, loadErr := s.repo.LoadAgentCheckpoint(scope)
		if loadErr != nil {
			return nil, loadErr
		}
		_, replayed, replayErr := agentruntime.ApplyClarificationResponse(latest, submission)
		if replayErr != nil {
			return nil, mapAgentClarificationError(replayErr, latest.StateVersion)
		}
		if !replayed {
			return nil, clarificationConflict(latest.StateVersion)
		}
		return s.agentRuntimeProgressForCurrentState(scope, latest)
	}
	if submission.Complete {
		return s.advanceAgentRun(scope, agentWakeClarificationAnswered)
	}
	return s.agentRuntimeProgressForCurrentState(scope, transition.State)
}

func mapAgentClarificationError(err error, latestStateVersion int) error {
	switch {
	case errors.Is(err, agentruntime.ErrClarificationAnswerInvalid), errors.Is(err, agentruntime.ErrClarificationIncomplete):
		return &AgentClarificationError{
			Status: http.StatusBadRequest, ErrorCode: "agent_clarification_invalid",
			Message: "Agent 追问回答格式无效", LatestStateVersion: latestStateVersion,
		}
	case errors.Is(err, agentruntime.ErrClarificationIdentityReused):
		return &AgentClarificationError{
			Status: http.StatusConflict, ErrorCode: "agent_clarification_identity_reused",
			Message: "Agent 追问身份已完成且回答事实冲突", LatestStateVersion: latestStateVersion,
		}
	case errors.Is(err, agentruntime.ErrClarificationNotPending):
		return &AgentClarificationError{
			Status: http.StatusConflict, ErrorCode: "agent_clarification_not_pending",
			Message: "Agent 当前没有等待该追问回答", LatestStateVersion: latestStateVersion,
		}
	case errors.Is(err, agentruntime.ErrClarificationVersionConflict), errors.Is(err, agentruntime.ErrClarificationConflict):
		return clarificationConflict(latestStateVersion)
	default:
		return err
	}
}

func clarificationConflict(latestStateVersion int) *AgentClarificationError {
	return &AgentClarificationError{
		Status: http.StatusConflict, ErrorCode: "agent_clarification_conflict",
		Message: "Agent 追问状态已经变化，请按最新状态重试", LatestStateVersion: latestStateVersion,
	}
}

func (s *Service) agentThreadForActor(actorUserID string, threadID string) (*model.AgentThread, error) {
	thread, err := s.repo.AgentThreadForActor(strings.TrimSpace(threadID), strings.TrimSpace(actorUserID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NotFound("Agent 对话不存在")
	}
	return thread, err
}

func (s *Service) scopeForAgentThread(actorUserID string, thread *model.AgentThread, runID string) (agentruntime.Scope, error) {
	if thread == nil || thread.Status != agentruntime.ThreadActive {
		return agentruntime.Scope{}, NotFound("Agent 对话不存在")
	}
	scope, err := s.AuthorizeAgentScope(actorUserID, thread.CanvasID, thread.ID, runID)
	if err != nil {
		return agentruntime.Scope{}, err
	}
	if scope.TenantKind != thread.TenantKind || scope.TenantID != thread.TenantID || scope.DomainProjectID != thread.DomainProjectID {
		return agentruntime.Scope{}, Forbidden("Agent 对话作用域已经变化")
	}
	return scope, nil
}

func (s *Service) scopeForAgentRun(actor *model.User, runID string) (agentruntime.Scope, error) {
	if actor == nil {
		return agentruntime.Scope{}, Unauthorized("请先登录")
	}
	identity, err := s.repo.AgentRunIdentityForActor(strings.TrimSpace(runID), actor.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return agentruntime.Scope{}, NotFound("Agent 运行不存在")
	}
	if err != nil {
		return agentruntime.Scope{}, err
	}
	scope, err := s.scopeForAgentThread(actor.ID, &identity.Thread, identity.Run.ID)
	if err != nil {
		return agentruntime.Scope{}, err
	}
	if identity.Run.ThreadID != scope.ThreadID || identity.Run.ActorUserID != scope.ActorUserID {
		return agentruntime.Scope{}, Forbidden("Agent 运行作用域冲突")
	}
	return scope, nil
}

func agentRuntimeView(progress *AgentRuntimeProgress) *AgentRuntimeView {
	if progress == nil {
		return nil
	}
	return &AgentRuntimeView{Run: progress.Run, State: progress.State}
}
