package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

const taskOutboxLeaseDuration = time.Minute

type agentTaskOutboxPayload struct {
	TaskID      string         `json:"taskId"`
	RunID       string         `json:"runId"`
	ActorUserID string         `json:"actorUserId"`
	Wakeup      agentRunWakeup `json:"wakeup"`
}

type taskOutboxDelivery func(repository.ActiveAgentRunReference, string, agentRunWakeup) error

func taskAgentRunDelivery(task model.Task) (string, agentRunWakeup, bool, error) {
	runID, found := agentTaskParentRunID(task.Operation)
	if !found {
		if task.Type == agentRuntimeModelTaskType {
			return "", "", false, errors.New("Agent 模型任务缺少有效运行标识")
		}
		return "", "", false, nil
	}
	wakeup := agentWakeGenerationTaskFinished
	if task.Type == agentRuntimeModelTaskType {
		wakeup = agentWakeModelTaskFinished
	}
	return runID, wakeup, true, nil
}

func taskAgentRunOutboxDraft(task model.Task, availableAt time.Time) (*repository.TaskOutboxDraft, error) {
	runID, wakeup, found, err := taskAgentRunDelivery(task)
	if err != nil || !found {
		return nil, err
	}
	if strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.UserID) == "" || availableAt.IsZero() {
		return nil, errors.New("Agent 任务 Outbox 事实无效")
	}
	payload, err := json.Marshal(agentTaskOutboxPayload{
		TaskID: task.ID, RunID: runID, ActorUserID: task.UserID, Wakeup: wakeup,
	})
	if err != nil {
		return nil, err
	}
	return &repository.TaskOutboxDraft{
		IdempotencyKey: "agent-run:" + runID + ":task:" + task.ID + ":terminal",
		EventType:      model.TaskOutboxAgentRunWakeup, PayloadJSON: string(payload), AvailableAt: availableAt.UTC(),
	}, nil
}

func decodeAgentTaskOutbox(record model.TaskOutbox) (agentTaskOutboxPayload, error) {
	if record.EventType != model.TaskOutboxAgentRunWakeup {
		return agentTaskOutboxPayload{}, fmt.Errorf("unsupported task outbox event type: %s", record.EventType)
	}
	var payload agentTaskOutboxPayload
	decoder := json.NewDecoder(strings.NewReader(record.PayloadJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return agentTaskOutboxPayload{}, fmt.Errorf("decode Agent task outbox: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return agentTaskOutboxPayload{}, errors.New("Agent task outbox contains trailing data")
	}
	if payload.TaskID != record.TaskID || strings.TrimSpace(payload.RunID) == "" || strings.TrimSpace(payload.ActorUserID) == "" {
		return agentTaskOutboxPayload{}, errors.New("Agent task outbox scope is invalid")
	}
	if err := validateAgentRunWakeup(payload.Wakeup); err != nil {
		return agentTaskOutboxPayload{}, err
	}
	return payload, nil
}

func validateAgentTaskOutboxScope(task model.Task, payload agentTaskOutboxPayload) error {
	runID, wakeup, found, err := taskAgentRunDelivery(task)
	if err != nil {
		return err
	}
	if !found || task.ID != payload.TaskID || task.UserID != payload.ActorUserID || runID != payload.RunID || wakeup != payload.Wakeup {
		return errors.New("Agent task outbox conflicts with terminal Task scope")
	}
	return nil
}

func taskOutboxRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 6 {
		attempt = 6
	}
	return time.Duration(1<<attempt) * time.Second
}

func expectedAgentTaskID(state agentruntime.RuntimeState, scope agentruntime.Scope, wakeup agentRunWakeup) (string, error) {
	if err := scope.Validate(); err != nil {
		return "", err
	}
	switch wakeup {
	case agentWakeModelTaskFinished:
		if state.Status != agentruntime.RunQueued && state.Status != agentruntime.RunRunning {
			return "", nil
		}
		return agentRuntimeModelTaskID(scope.RunID, state.StepNumber), nil
	case agentWakeGenerationTaskFinished:
		if state.Status != agentruntime.RunWaitingTool || state.PendingToolCall == nil || !state.PendingToolStarted {
			return "", nil
		}
		if state.PendingToolCall.ToolName != agentruntime.ToolMediaGenerate {
			return "", nil
		}
		decoded, err := agentruntime.DecodeCapabilityArguments(
			agentruntime.ToolMediaGenerate,
			state.PendingToolCall.Arguments,
		)
		if err != nil {
			return "", fmt.Errorf("decode pending media capability task identity: %w", err)
		}
		arguments, ok := decoded.(agentruntime.MediaGenerateArguments)
		if !ok {
			return "", errors.New("pending media capability arguments have an invalid type")
		}
		return MediaAttemptIdentity(scope, MediaGenerationCommand{
			ArtifactRevisionID: agentMediaCapabilityIdentity(scope, arguments),
			Attempt:            1,
			TaskType:           "canvas_" + string(arguments.MediaKind),
			Capability:         string(arguments.MediaKind),
		}), nil
	default:
		return "", errors.New("task outbox wakeup is invalid")
	}
}

func (s *Service) dispatchTaskOutbox(now time.Time, limit int) error {
	return s.dispatchTaskOutboxWithDelivery(now, limit, s.advanceAgentRunTaskReference)
}

func (s *Service) dispatchTaskOutboxWithDelivery(now time.Time, limit int, deliver taskOutboxDelivery) error {
	if now.IsZero() || limit <= 0 || deliver == nil {
		return errors.New("task outbox dispatcher input is invalid")
	}
	owner := s.workerID + ":task-outbox"
	records, err := s.repo.ClaimTaskOutbox(owner, now.UTC(), taskOutboxLeaseDuration, limit)
	if err != nil {
		return err
	}
	var deliveryErrors []error
	for index := range records {
		record := records[index]
		payload, deliveryErr := decodeAgentTaskOutbox(record)
		if deliveryErr == nil {
			var task *model.Task
			task, deliveryErr = s.repo.Task(record.TaskID)
			if deliveryErr == nil && task.Status != model.TaskStatusSucceeded && task.Status != model.TaskStatusCancelled && task.Status != model.TaskStatusFailed {
				deliveryErr = errors.New("task outbox terminal task fact is unavailable")
			}
			if deliveryErr == nil {
				deliveryErr = validateAgentTaskOutboxScope(*task, payload)
			}
		}
		if deliveryErr == nil {
			deliveryErr = deliver(repository.ActiveAgentRunReference{
				RunID: payload.RunID, ActorUserID: payload.ActorUserID,
			}, payload.TaskID, payload.Wakeup)
		}
		if deliveryErr != nil {
			retryAt := now.UTC().Add(taskOutboxRetryDelay(record.AttemptCount))
			if retryErr := s.repo.RescheduleTaskOutbox(record.ID, owner, record.LeaseToken, deliveryErr, retryAt); retryErr != nil {
				deliveryErr = errors.Join(deliveryErr, retryErr)
			}
			deliveryErrors = append(deliveryErrors, fmt.Errorf("deliver task outbox %s: %w", record.ID, deliveryErr))
			continue
		}
		if err := s.repo.CompleteTaskOutbox(record.ID, owner, record.LeaseToken, time.Now().UTC()); err != nil {
			deliveryErrors = append(deliveryErrors, fmt.Errorf("complete task outbox %s: %w", record.ID, err))
		}
	}
	return errors.Join(deliveryErrors...)
}
