package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

func TestInterruptedMediaRefundedFailureClosesPendingTool(t *testing.T) {
	for _, taskStatus := range []model.TaskStatus{model.TaskStatusFailed, model.TaskStatusCancelled} {
		t.Run(string(taskStatus), func(t *testing.T) {
			svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
			createAgentRuntimeImageModel(t, db, fixture)
			call := agentMediaCapabilityCall("interrupted-media", "interrupted-request")
			seedApprovedAgentCapabilityProposal(t, svc, db, call)
			scope := agentRuntimeServiceScope()
			registry, err := newAgentCapabilityRegistry(svc)
			if err != nil {
				t.Fatal(err)
			}
			queued, err := registry.Execute(context.Background(), scope, call)
			if err != nil || !queued.Pending {
				t.Fatalf("queue: %#v, %v", queued, err)
			}
			state, err := svc.repo.LoadAgentCheckpoint(scope)
			if err != nil {
				t.Fatal(err)
			}
			started, err := agentruntime.BeginToolExecution(state, agentruntime.ToolExecution{ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.commitAgentRuntimeState(scope, state, started); err != nil {
				t.Fatal(err)
			}
			task := loadAgentMediaCapabilityTask(t, db)
			wantCode := "media_generation_cancelled"
			if taskStatus == model.TaskStatusFailed {
				task = leaseAgentMediaCapabilityTask(t, db, task)
				task.Error = "provider rejected request"
				if err := svc.repo.FinalizeFailedTaskAndBilling(&task, repository.FailedTaskBillingRefund, task.Error); err != nil {
					t.Fatal(err)
				}
				wantCode = "media_generation_failed"
			} else {
				ok, err := svc.repo.CancelTaskIfStatus(task.UserID, task.ID, model.TaskStatusQueued, "agent_run_cancelled", time.Now().UTC())
				if err != nil || !ok {
					t.Fatalf("cancel: %t, %v", ok, err)
				}
				if err := svc.RefundBilling(task.BillingOrderID, "cancelled before submission"); err != nil {
					t.Fatal(err)
				}
			}
			interrupted, err := svc.SubmitScopedAgentInterrupt(&model.User{ID: scope.ActorUserID}, scope.RunID, started.State.StateVersion)
			if err != nil {
				t.Fatal(err)
			}
			if err := svc.settleInterruptedAgentTool(scope, interrupted.State); err != nil {
				t.Fatalf("terminal failure must settle: %v", err)
			}
			settled, err := svc.repo.LoadAgentCheckpoint(scope)
			if err != nil {
				t.Fatal(err)
			}
			if settled.Status != agentruntime.RunCancelled || settled.PendingToolCall != nil || settled.PendingToolStarted || settled.LastToolResult == nil || settled.LastToolResult.Succeeded || settled.LastToolResult.ErrorCode != wantCode {
				t.Fatalf("unsettled failure: %#v", settled)
			}
			assertAgentMediaCommercialCounts(t, db, task, 1, 1, 1, 0, 1)
		})
	}
}

func newStartedMediaSettlementFixture(t *testing.T) (*Service, *gorm.DB, agentruntime.RuntimeState, model.Task) {
	t.Helper()
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeImageModel(t, db, fixture)
	call := agentMediaCapabilityCall("settlement-media", "settlement-request")
	seedApprovedAgentCapabilityProposal(t, svc, db, call)
	scope := agentRuntimeServiceScope()
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := registry.Execute(context.Background(), scope, call)
	if err != nil || !queued.Pending {
		t.Fatalf("queue: %#v, %v", queued, err)
	}
	state, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	started, err := agentruntime.BeginToolExecution(state, agentruntime.ToolExecution{ToolCallID: call.ToolCallID, ActionVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.commitAgentRuntimeState(scope, state, started); err != nil {
		t.Fatal(err)
	}
	return svc, db, started.State, loadAgentMediaCapabilityTask(t, db)
}

func TestInterruptedMediaLateSuccessIsRetainedAndWakeupIsIdempotent(t *testing.T) {
	svc, db, started, task := newStartedMediaSettlementFixture(t)
	scope := agentRuntimeServiceScope()
	if err := finalizeAgentMediaCapabilityTask(t, svc, db, task, "late-ready", "image"); err != nil {
		t.Fatal(err)
	}
	// 供应商已完成，但结果通知尚未消费时用户取消，迟到通知仍须回填资产。
	if _, err := svc.SubmitScopedAgentInterrupt(&model.User{ID: scope.ActorUserID}, scope.RunID, started.StateVersion); err != nil {
		t.Fatal(err)
	}
	reference := repository.ActiveAgentRunReference{RunID: scope.RunID, ActorUserID: scope.ActorUserID}
	for attempt := 0; attempt < 2; attempt++ {
		if err := svc.advanceAgentRunTaskReference(reference, task.ID, agentWakeGenerationTaskFinished); err != nil {
			t.Fatal(err)
		}
	}
	state, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != agentruntime.RunCancelled || state.PendingToolCall != nil || state.LastToolResult == nil || !state.LastToolResult.Succeeded || state.StateVersion != started.StateVersion+2 {
		t.Fatalf("late success state: %#v", state)
	}
	var canvas model.CanvasProject
	if err := db.First(&canvas, "id = ?", scope.CanvasID).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(canvas.PayloadJSON, "resource:late-ready") {
		t.Fatal("late asset missing from canvas")
	}
	var count int64
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ? AND kind = ?", scope.RunID, agentruntime.EventToolResult).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("tool results = %d", count)
	}
	assertAgentMediaCommercialCounts(t, db, task, 1, 1, 1, 1, 0)
}

func TestInterruptedMediaUncertainBillingDoesNotClosePendingTool(t *testing.T) {
	svc, db, started, task := newStartedMediaSettlementFixture(t)
	scope := agentRuntimeServiceScope()
	if err := finalizeAgentMediaCapabilityTaskWithBillingAction(t, svc, db, task, "uncertain-ready", "image", repository.CompletedTaskBillingUncertain); err != nil {
		t.Fatal(err)
	}
	interrupted, err := svc.SubmitScopedAgentInterrupt(&model.User{ID: scope.ActorUserID}, scope.RunID, started.StateVersion)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.settleInterruptedAgentTool(scope, interrupted.State); !agentCapabilityErrorCode(err, "media_settlement_uncertain") {
		t.Fatalf("uncertain error: %v", err)
	}
	state, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if state.StateVersion != interrupted.State.StateVersion || state.PendingToolCall == nil || state.Status != agentruntime.RunCancelled {
		t.Fatalf("uncertain state: %#v", state)
	}
	var resource model.Resource
	if err := db.First(&resource, "id = ?", "uncertain-ready").Error; err != nil {
		t.Fatal(err)
	}
	if resource.Status != model.ResourceStatusReady {
		t.Fatal("uncertain billing discarded generated asset")
	}
	assertAgentMediaCommercialCounts(t, db, task, 1, 1, 1, 0, 0)
}

func TestInterruptedMediaMissingTaskCannotCreateAnotherTask(t *testing.T) {
	svc, db, started, task := newStartedMediaSettlementFixture(t)
	scope := agentRuntimeServiceScope()
	interrupted, err := svc.SubmitScopedAgentInterrupt(&model.User{ID: scope.ActorUserID}, scope.RunID, started.StateVersion)
	if err != nil {
		t.Fatal(err)
	}
	// 仅移除独立测试库中的任务，模拟存储事实不完整，不触碰真实数据。
	if err := db.Delete(&model.Task{}, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.settleInterruptedAgentTool(scope, interrupted.State); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing task error: %v", err)
	}
	var count int64
	if err := db.Model(&model.Task{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("settlement created %d tasks", count)
	}
}

func TestUserInterruptQueuedMediaCancelsTaskAndRefundsThroughOutbox(t *testing.T) {
	svc, db, started, task := newStartedMediaSettlementFixture(t)
	scope := agentRuntimeServiceScope()
	if _, err := svc.SubmitScopedAgentInterrupt(&model.User{ID: scope.ActorUserID}, scope.RunID, started.StateVersion); err != nil {
		t.Fatal(err)
	}
	stored := loadAgentMediaCapabilityTask(t, db)
	if stored.Status != model.TaskStatusCancelled {
		t.Fatalf("interrupted queued task status = %s", stored.Status)
	}
	var outbox model.TaskOutbox
	if err := db.First(&outbox, "task_id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if outbox.Status != model.TaskOutboxPending {
		t.Fatalf("outbox status = %s", outbox.Status)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := svc.dispatchTaskOutbox(time.Now().UTC().Add(time.Minute), 10); err != nil {
			t.Fatal(err)
		}
	}
	state, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != agentruntime.RunCancelled || state.PendingToolCall != nil || state.LastToolResult == nil || state.LastToolResult.ErrorCode != "media_generation_cancelled" {
		t.Fatalf("cancelled task state: %#v", state)
	}
	assertAgentMediaCommercialCounts(t, db, task, 1, 1, 1, 0, 1)
}

func TestInterruptedSubmittedMediaRetainsLateResourceWhileBillingIsUncertain(t *testing.T) {
	svc, db, started, task := newStartedMediaSettlementFixture(t)
	scope := agentRuntimeServiceScope()
	task = leaseAgentMediaCapabilityTask(t, db, task)
	task.ProviderRequestID = "submitted-before-interrupt"
	if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Update("provider_request_id", task.ProviderRequestID).Error; err != nil {
		t.Fatal(err)
	}
	cancelled := false
	svc.registerActiveTask(task.ID, func() { cancelled = true })
	interrupted, err := svc.SubmitScopedAgentInterrupt(&model.User{ID: scope.ActorUserID}, scope.RunID, started.StateVersion)
	if err != nil {
		t.Fatal(err)
	}
	stored := loadAgentMediaCapabilityTask(t, db)
	if !cancelled || stored.Status != model.TaskStatusCancelled || stored.PollStage != "cancel_reconcile" {
		t.Fatalf("submitted cancellation: context=%t task=%#v", cancelled, stored)
	}
	lease, err := svc.repo.ClaimCancelledTaskResult(task.ID, task.UserID, task.LeaseGeneration, task.LeaseOwner, time.Minute)
	if err != nil || lease == nil {
		t.Fatalf("reconcile lease: %v", err)
	}
	seedAgentCapabilityResource(t, db, "submitted-late-ready", task.UserID, "")
	lease.ResultJSON = `{"images":[{"resourceId":"submitted-late-ready"}]}`
	if err := svc.saveCancelledTaskResult(lease, []byte(lease.ResultJSON), "费用待核对"); err != nil {
		t.Fatal(err)
	}
	if err := svc.settleInterruptedAgentTool(scope, interrupted.State); !agentCapabilityErrorCode(err, "media_settlement_uncertain") {
		t.Fatalf("unresolved result: %v", err)
	}
	stored = loadAgentMediaCapabilityTask(t, db)
	if stored.ResultJSON != lease.ResultJSON || stored.Status != model.TaskStatusCancelled {
		t.Fatal("late result was discarded or cancellation overwritten")
	}
	var resource model.Resource
	if err := db.First(&resource, "id = ?", "submitted-late-ready").Error; err != nil {
		t.Fatal(err)
	}
	if resource.Status != model.ResourceStatusReady {
		t.Fatal("late resource is not retained")
	}
	assertAgentMediaCommercialCounts(t, db, task, 1, 1, 1, 0, 0)
}

func TestInterruptedMediaSettlesFrozenTaskAfterModelDisabled(t *testing.T) {
	svc, db, started, task := newStartedMediaSettlementFixture(t)
	scope := agentRuntimeServiceScope()
	if err := finalizeAgentMediaCapabilityTask(t, svc, db, task, "disabled-model-result", "image"); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ChannelModel{}).Where("id = ?", "runtime-image-model").Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	interrupted, err := svc.SubmitScopedAgentInterrupt(&model.User{ID: scope.ActorUserID}, scope.RunID, started.StateVersion)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.settleInterruptedAgentTool(scope, interrupted.State); err != nil {
		t.Fatalf("completed task must not be requoted: %v", err)
	}
	assertAgentMediaCommercialCounts(t, db, task, 1, 1, 1, 1, 0)
}

func TestAdminInterruptQueuedMediaDispatchesCancelledResult(t *testing.T) {
	svc, db, started, task := newStartedMediaSettlementFixture(t)
	scope := agentRuntimeServiceScope()
	if _, err := svc.repo.InterruptAdminAgentRun(repository.AdminAgentRunInterruptCommand{RunID: scope.RunID, ExpectedStateVersion: started.StateVersion, ActorUserID: "administrator", Reason: "停止排队任务", Now: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := svc.dispatchTaskOutbox(time.Now().UTC().Add(time.Minute), 10); err != nil {
		t.Fatal(err)
	}
	state, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if state.PendingToolCall != nil || state.Status != agentruntime.RunCancelled || state.LastToolResult == nil || state.LastToolResult.ErrorCode != "media_generation_cancelled" {
		t.Fatalf("admin cancellation: %#v", state)
	}
	assertAgentMediaCommercialCounts(t, db, task, 1, 1, 1, 0, 1)
}

func TestInterruptedMediaRejectsWrongBillingScope(t *testing.T) {
	svc, db, started, task := newStartedMediaSettlementFixture(t)
	scope := agentRuntimeServiceScope()
	if err := finalizeAgentMediaCapabilityTask(t, svc, db, task, "scope-ready", "image"); err != nil {
		t.Fatal(err)
	}
	interrupted, err := svc.SubmitScopedAgentInterrupt(&model.User{ID: scope.ActorUserID}, scope.RunID, started.StateVersion)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("id = ?", task.BillingOrderID).Update("team_id", "another-team").Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.settleInterruptedAgentTool(scope, interrupted.State); err == nil {
		t.Fatal("foreign billing was delivered")
	}
	state, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if state.StateVersion != interrupted.State.StateVersion || state.PendingToolCall == nil {
		t.Fatal("invalid scope closed pending tool")
	}
}

func TestInterruptDoesNotCancelForeignMediaWithMatchingOperation(t *testing.T) {
	svc, db, started, _ := newStartedMediaSettlementFixture(t)
	scope := agentRuntimeServiceScope()
	foreign := model.Task{ID: "foreign-task", UserID: "foreign-user", ProjectID: scope.CanvasID, Audience: model.TaskAudienceInternal, Operation: agentMediaGenerationOperationForRun(scope.RunID), Type: "canvas_image", Capability: "image", Status: model.TaskStatusQueued}
	if err := db.Create(&foreign).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitScopedAgentInterrupt(&model.User{ID: scope.ActorUserID}, scope.RunID, started.StateVersion); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&foreign, "id = ?", foreign.ID).Error; err != nil {
		t.Fatal(err)
	}
	if foreign.Status != model.TaskStatusQueued {
		t.Fatal("foreign task was cancelled")
	}
}

func TestInterruptedVisionRefundedResultClosesPendingTool(t *testing.T) {
	fixture := queueDirectAgentVisionTaskForTest(t, "https://example.com", "interrupt")
	svc := fixture.service
	scope := agentRuntimeServiceScope()
	state, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	started, err := agentruntime.BeginToolExecution(state, agentruntime.ToolExecution{ToolCallID: state.PendingToolCall.ToolCallID, ActionVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.commitAgentRuntimeState(scope, state, started); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitScopedAgentInterrupt(&model.User{ID: scope.ActorUserID}, scope.RunID, started.State.StateVersion); err != nil {
		t.Fatal(err)
	}
	if err := svc.dispatchTaskOutbox(time.Now().UTC().Add(time.Minute), 10); err != nil {
		t.Fatal(err)
	}
	settled, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if settled.Status != agentruntime.RunCancelled || settled.PendingToolCall != nil || settled.LastToolResult == nil || settled.LastToolResult.ErrorCode != "vision_analysis_cancelled" {
		t.Fatalf("vision settlement: %#v", settled)
	}
	order, err := svc.repo.BillingOrder(fixture.order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if order.Status != model.BillingStatusRefunded {
		t.Fatalf("vision billing = %s", order.Status)
	}
}
