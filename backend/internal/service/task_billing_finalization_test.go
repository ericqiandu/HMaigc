package service

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

func TestAtomicCompletionCommitsResultSettlementAndTaskOutboxTogether(t *testing.T) {
	_, db := newMembershipTestService(t)
	if err := db.AutoMigrate(&model.Session{}, &model.Message{}, &model.Result{}, &model.TaskOutbox{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	account := model.CreditAccount{
		UserID: "atomic-completion-user", AvailableMicrocredits: 96_000_000,
		ReservedMicrocredits: 1_000_000, CreatedAt: now, UpdatedAt: now,
	}
	order := model.BillingOrder{
		ID: "atomic-completion-order", UserID: account.UserID, TaskID: "atomic-completion-task",
		IdempotencyKey: "atomic-completion-order", ChannelID: "atomic-channel", Model: "atomic-model",
		Capability: "video", Scene: "agent", BillingMode: "fixed_request", PriceVersion: 1,
		UnitPriceMicrocredits: 1_000_000, MultiplierBasisPoints: 10_000, Quantity: 1,
		AmountMicrocredits: 1_000_000, Status: model.BillingStatusRunning,
		ProviderRequestID: "provider-completion", StartedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	leaseExpiry := now.Add(time.Minute)
	task := model.Task{
		ID: order.TaskID, UserID: account.UserID, Type: "canvas_video", Status: model.TaskStatusRunning,
		BillingOrderID: order.ID, ProviderRequestID: order.ProviderRequestID, LeaseOwner: "atomic-worker",
		LeaseExpiresAt: &leaseExpiry, LeaseGeneration: 1,
		LeaseToken: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		CreatedAt:  now, UpdatedAt: now,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	completedAt := now.Add(time.Second)
	completed := task
	completed.Status = model.TaskStatusSucceeded
	completed.Stage = "任务完成"
	completed.Progress = 100
	completed.ResultJSON = `{"videos":[{"resourceId":"resource-atomic"}]}`
	completed.CompletedAt = &completedAt
	result := model.Result{
		ID: "atomic-completion-result", UserID: task.UserID, TaskID: task.ID,
		Kind: "generation_result", Payload: completed.ResultJSON, CreatedAt: completedAt,
	}
	outbox := repository.TaskOutboxDraft{
		IdempotencyKey: "run-atomic/task-atomic/terminal", EventType: model.TaskOutboxAgentRunWakeup,
		PayloadJSON: `{"runId":"run-atomic","actorUserId":"atomic-completion-user","wakeup":"generation_task_finished"}`,
		AvailableAt: completedAt,
	}
	if err := repository.New(db).FinalizeSucceededTaskAndBilling(repository.SucceededTaskFinalization{
		Task: &completed, Results: []model.Result{result}, BillingAction: repository.CompletedTaskBillingSettle,
		Outbox: &outbox,
	}); err != nil {
		t.Fatal(err)
	}

	var storedTask model.Task
	var storedOrder model.BillingOrder
	var storedResult model.Result
	var storedOutbox model.TaskOutbox
	var consumeCount int64
	if err := db.First(&storedTask, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedResult, "id = ?", result.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedOutbox, "idempotency_key = ?", outbox.IdempotencyKey).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).
		Where("billing_order_id = ? AND type = ?", order.ID, model.CreditLedgerConsume).
		Count(&consumeCount).Error; err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != model.TaskStatusSucceeded || storedOrder.Status != model.BillingStatusSettled ||
		storedResult.TaskID != task.ID || storedOutbox.Status != model.TaskOutboxPending || consumeCount != 1 {
		t.Fatalf("atomic completion facts mismatch: task=%#v order=%#v result=%#v outbox=%#v consume=%d", storedTask, storedOrder, storedResult, storedOutbox, consumeCount)
	}
}

func TestAtomicCompletionRollsBackWhenTaskOutboxCannotPersist(t *testing.T) {
	_, db := newMembershipTestService(t)
	if err := db.AutoMigrate(&model.Result{}, &model.TaskOutbox{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_task_outbox BEFORE INSERT ON task_outboxes BEGIN SELECT RAISE(ABORT, 'outbox unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	account := model.CreditAccount{UserID: "atomic-rollback-user", AvailableMicrocredits: 9_000_000, ReservedMicrocredits: 1_000_000, CreatedAt: now, UpdatedAt: now}
	order := model.BillingOrder{
		ID: "atomic-rollback-order", UserID: account.UserID, TaskID: "atomic-rollback-task",
		IdempotencyKey: "atomic-rollback-order", ChannelID: "atomic-channel", Model: "atomic-model",
		Capability: "image", Scene: "agent", BillingMode: "fixed_request", AmountMicrocredits: 1_000_000,
		Status: model.BillingStatusRunning, CreatedAt: now, UpdatedAt: now,
	}
	leaseExpiry := now.Add(time.Minute)
	task := model.Task{
		ID: order.TaskID, UserID: account.UserID, Type: "canvas_image", Status: model.TaskStatusRunning,
		BillingOrderID: order.ID, LeaseOwner: "rollback-worker", LeaseExpiresAt: &leaseExpiry, LeaseGeneration: 1,
		LeaseToken: "1123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		CreatedAt:  now, UpdatedAt: now,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	completed := task
	completed.Status = model.TaskStatusSucceeded
	completed.ResultJSON = `{"images":[{"resourceId":"resource-rollback"}]}`
	completed.CompletedAt = &now
	result := model.Result{ID: "atomic-rollback-result", UserID: task.UserID, TaskID: task.ID, Kind: "generation_result", Payload: completed.ResultJSON, CreatedAt: now}
	outbox := repository.TaskOutboxDraft{IdempotencyKey: "atomic-rollback-outbox", EventType: model.TaskOutboxAgentRunWakeup, PayloadJSON: `{}`, AvailableAt: now}
	err := repository.New(db).FinalizeSucceededTaskAndBilling(repository.SucceededTaskFinalization{
		Task: &completed, Results: []model.Result{result}, BillingAction: repository.CompletedTaskBillingSettle, Outbox: &outbox,
	})
	if err == nil {
		t.Fatal("FinalizeSucceededTaskAndBilling error = nil, want outbox transaction failure")
	}
	var storedTask model.Task
	var storedOrder model.BillingOrder
	var resultCount int64
	var consumeCount int64
	if err := db.First(&storedTask, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Result{}).Where("task_id = ?", task.ID).Count(&resultCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ?", order.ID).Count(&consumeCount).Error; err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != model.TaskStatusRunning || storedOrder.Status != model.BillingStatusRunning || resultCount != 0 || consumeCount != 0 {
		t.Fatalf("failed outbox did not roll back completion: task=%s billing=%s results=%d ledger=%d", storedTask.Status, storedOrder.Status, resultCount, consumeCount)
	}
}

func TestSettlementAuditMarksTokenProviderChargeUncertainAtomically(t *testing.T) {
	_, db := newMembershipTestService(t)
	if err := db.AutoMigrate(&model.Result{}, &model.TaskOutbox{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	order := model.BillingOrder{
		ID: "settlement-audit-order", UserID: "settlement-audit-user", TaskID: "settlement-audit-task",
		IdempotencyKey: "settlement-audit-order", ChannelID: "token-channel", Model: "token-model",
		Capability: "text", Scene: "agent", BillingMode: "token_usage", AmountMicrocredits: 30_000_000,
		ReservedAmountMicrocredits: 30_000_000, Status: model.BillingStatusRunning,
		ProviderRequestID: "provider-token-request", CreatedAt: now, UpdatedAt: now,
	}
	leaseExpiry := now.Add(time.Minute)
	task := model.Task{
		ID: order.TaskID, UserID: order.UserID, Type: "agent_runtime_model", Status: model.TaskStatusRunning,
		BillingOrderID: order.ID, ProviderRequestID: order.ProviderRequestID, LeaseOwner: "token-worker",
		LeaseExpiresAt: &leaseExpiry, LeaseGeneration: 1,
		LeaseToken: "2123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		CreatedAt:  now, UpdatedAt: now,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	completed := task
	completed.Status = model.TaskStatusSucceeded
	completed.ResultJSON = `{"message":"done"}`
	completed.CompletedAt = &now
	outbox := repository.TaskOutboxDraft{IdempotencyKey: "settlement-audit-outbox", EventType: model.TaskOutboxAgentRunWakeup, PayloadJSON: `{}`, AvailableAt: now}
	if err := repository.New(db).FinalizeSucceededTaskAndBilling(repository.SucceededTaskFinalization{
		Task: &completed, BillingAction: repository.CompletedTaskBillingUncertain,
		BillingError: "供应商已返回成功，Token 用量与费用待核对", Outbox: &outbox,
	}); err != nil {
		t.Fatal(err)
	}
	var storedTask model.Task
	var storedOrder model.BillingOrder
	if err := db.First(&storedTask, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != model.TaskStatusSucceeded || storedOrder.Status != model.BillingStatusUncertain || storedOrder.Error == "" {
		t.Fatalf("uncertain settlement facts mismatch: task=%#v order=%#v", storedTask, storedOrder)
	}
}

func TestTaskOutboxCrashRecoveryAndDuplicateDelivery(t *testing.T) {
	_, db := newMembershipTestService(t)
	if err := db.AutoMigrate(&model.TaskOutbox{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	now := time.Now().UTC()
	draft := repository.TaskOutboxDraft{
		IdempotencyKey: "crash-recovery-once", EventType: model.TaskOutboxAgentRunWakeup,
		PayloadJSON: `{"runId":"run-recovery"}`, AvailableAt: now,
	}
	if err := repo.EnqueueTaskOutbox("crash-recovery-task", draft); err != nil {
		t.Fatal(err)
	}
	if err := repo.EnqueueTaskOutbox("crash-recovery-task", draft); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.TaskOutbox{}).Where("idempotency_key = ?", draft.IdempotencyKey).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("outbox count = %d, want 1", count)
	}
	claimed, err := repo.ClaimTaskOutbox("dispatcher-a", now, time.Minute, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 || claimed[0].LeaseToken == "" {
		t.Fatalf("claimed outbox = %#v", claimed)
	}
	if err := repo.RescheduleTaskOutbox(claimed[0].ID, "dispatcher-a", claimed[0].LeaseToken, errors.New("timeline unavailable"), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	claimed, err = repo.ClaimTaskOutbox("dispatcher-b", now.Add(2*time.Second), time.Minute, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 || claimed[0].AttemptCount != 2 {
		t.Fatalf("reclaimed outbox = %#v", claimed)
	}
	if err := repo.CompleteTaskOutbox(claimed[0].ID, "dispatcher-b", claimed[0].LeaseToken, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	claimed, err = repo.ClaimTaskOutbox("dispatcher-c", now.Add(4*time.Second), time.Minute, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 0 {
		t.Fatalf("delivered outbox reclaimed: %#v", claimed)
	}
}

func TestTaskOutboxCrashRecoveryRetriesAgentTimelineDelivery(t *testing.T) {
	_, db := newMembershipTestService(t)
	if err := db.AutoMigrate(&model.TaskOutbox{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	svc := New(repo, t.TempDir())
	now := time.Now().UTC()
	if err := db.Create(&model.Task{
		ID: "timeline-recovery-task", UserID: "timeline-user", Type: "canvas_video",
		Status: model.TaskStatusSucceeded, Operation: agentMediaGenerationOperationForRun("timeline-run"), CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.EnqueueTaskOutbox("timeline-recovery-task", repository.TaskOutboxDraft{
		IdempotencyKey: "timeline-recovery-delivery", EventType: model.TaskOutboxAgentRunWakeup,
		PayloadJSON: `{"taskId":"timeline-recovery-task","runId":"timeline-run","actorUserId":"timeline-user","wakeup":"generation_task_finished"}`,
		AvailableAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	deliveryCalls := 0
	deliver := func(reference repository.ActiveAgentRunReference, taskID string, wakeup agentRunWakeup) error {
		deliveryCalls++
		if reference.RunID != "timeline-run" || reference.ActorUserID != "timeline-user" || taskID != "timeline-recovery-task" || wakeup != agentWakeGenerationTaskFinished {
			t.Fatalf("unexpected delivery: reference=%#v task=%s wakeup=%s", reference, taskID, wakeup)
		}
		if deliveryCalls == 1 {
			return errors.New("timeline temporarily unavailable")
		}
		return nil
	}
	if err := svc.dispatchTaskOutboxWithDelivery(now, 10, deliver); err == nil {
		t.Fatal("first delivery error = nil, want durable retry")
	}
	if err := svc.dispatchTaskOutboxWithDelivery(now.Add(time.Minute), 10, deliver); err != nil {
		t.Fatal(err)
	}
	if err := svc.dispatchTaskOutboxWithDelivery(now.Add(2*time.Minute), 10, deliver); err != nil {
		t.Fatal(err)
	}
	if deliveryCalls != 2 {
		t.Fatalf("delivery calls = %d, want 2", deliveryCalls)
	}
	var stored model.TaskOutbox
	if err := db.First(&stored, "idempotency_key = ?", "timeline-recovery-delivery").Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.TaskOutboxDelivered || stored.DeliveredAt == nil || stored.AttemptCount != 2 {
		t.Fatalf("timeline outbox was not delivered after retry: %#v", stored)
	}
}

func TestTaskOutboxRejectsRunScopeThatConflictsWithTerminalTask(t *testing.T) {
	_, db := newMembershipTestService(t)
	if err := db.AutoMigrate(&model.TaskOutbox{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	svc := New(repo, t.TempDir())
	now := time.Now().UTC()
	if err := db.Create(&model.Task{
		ID: "scope-conflict-task", UserID: "scope-user", Type: "canvas_video",
		Status: model.TaskStatusSucceeded, Operation: agentMediaGenerationOperationForRun("task-run"), CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.EnqueueTaskOutbox("scope-conflict-task", repository.TaskOutboxDraft{
		IdempotencyKey: "scope-conflict-delivery", EventType: model.TaskOutboxAgentRunWakeup,
		PayloadJSON: `{"taskId":"scope-conflict-task","runId":"different-run","actorUserId":"scope-user","wakeup":"generation_task_finished"}`,
		AvailableAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	deliveryCalls := 0
	deliver := func(repository.ActiveAgentRunReference, string, agentRunWakeup) error {
		deliveryCalls++
		return nil
	}
	if err := svc.dispatchTaskOutboxWithDelivery(now, 10, deliver); err == nil {
		t.Fatal("scope-conflicting outbox delivery error = nil")
	}
	if deliveryCalls != 0 {
		t.Fatalf("scope-conflicting outbox delivery calls = %d, want 0", deliveryCalls)
	}
	var stored model.TaskOutbox
	if err := db.First(&stored, "idempotency_key = ?", "scope-conflict-delivery").Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.TaskOutboxPending || stored.LastError == "" {
		t.Fatalf("scope-conflicting outbox was not retained for audit: %#v", stored)
	}
}

func TestAgentTaskOutboxIgnoresStaleModelWakeupAfterRunAdvances(t *testing.T) {
	decision := `{"kind":"clarification_request","clarification":{"requestId":"outbox-clarification","questions":[{"id":"duration","prompt":"广告时长是多少？","type":"single_choice","options":[{"id":"15s","label":"15 秒"},{"id":"30s","label":"30 秒"}]}],"expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`
	server, _ := newAgentRuntimeDecisionServer(t, decision)
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "stale-model-outbox", UserMessage: "生成汽车广告剧本",
		MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.ModelTask == nil {
		t.Fatal("initial model task is nil")
	}
	staleTaskID := started.ModelTask.ID
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	waiting, err := svc.advanceAgentRun(scope, agentWakeModelTaskFinished)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.State.Status != agentruntime.RunWaitingInput || waiting.State.PendingClarification == nil {
		t.Fatalf("run did not pause for the current clarification: %#v", waiting.State)
	}
	resumed, err := svc.SubmitAgentClarificationResponse(scope, agentruntime.ClarificationResponseSubmission{
		RequestID: "outbox-clarification", ExpectedStateVersion: waiting.State.StateVersion,
		QuestionID: "duration", Answer: agentruntime.ClarificationAnswerInput{SelectedOptionIDs: []string{"30s"}}, Complete: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.ModelTask == nil {
		t.Fatal("clarification completion did not create the next model task")
	}
	state := resumed.State
	currentTaskID := resumed.ModelTask.ID
	if currentTaskID == staleTaskID {
		t.Fatalf("run did not advance beyond stale task %q", staleTaskID)
	}
	now := time.Now().UTC()
	if err := db.Exec(
		"UPDATE tasks SET status = ?, result_json = ?, completed_at = ?, updated_at = ? WHERE id = ?",
		model.TaskStatusSucceeded, `{}`, now, now, currentTaskID,
	).Error; err != nil {
		t.Fatal(err)
	}
	beforeVersion := state.StateVersion
	if err := svc.advanceAgentRunTaskReference(repository.ActiveAgentRunReference{
		RunID: scope.RunID, ActorUserID: scope.ActorUserID,
	}, staleTaskID, agentWakeModelTaskFinished); err != nil {
		t.Fatal(err)
	}
	state, err = svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if state.StateVersion != beforeVersion {
		t.Fatalf("stale task wakeup advanced state version from %d to %d", beforeVersion, state.StateVersion)
	}
}

func TestExpectedAgentTaskIDReadsFrozenGenerationTaskIdentity(t *testing.T) {
	scope := agentRuntimeServiceScope()
	visionCall := agentruntime.ToolCallDecision{
		ToolCallID: "vision-outbox", ToolName: agentruntime.ToolVisionAnalyze, ActionVersion: 1,
		Arguments: []byte(`{"modelRecordId":"runtime-vision-model","modelKey":"deepseek-v4-flash-vision-exp","sourceResourceIds":["vision-resource"],"prompt":"描述主体","detail":"low","clientRequestId":"vision-outbox-request"}`),
	}
	visionTaskID, err := agentruntime.CapabilityIdempotencyKey(scope, visionCall)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name          string
		toolName      agentruntime.ToolName
		toolCallID    string
		actionVersion int
		arguments     []byte
		want          string
		wantErr       bool
	}{
		{
			name: "retired production render", toolName: agentruntime.ToolProductionRender,
			arguments: []byte(`{"taskId":"production-task"}`), want: "",
		},
		{
			name: "retired media envelope", toolName: agentruntime.ToolMediaGenerate,
			arguments: []byte(`{"commercial":{"taskId":"media-task"}}`), wantErr: true,
		},
		{
			name: "retired visual analysis envelope", toolName: agentruntime.ToolVisionAnalyze,
			arguments: []byte(`{"commercial":{"taskId":"vision-task"}}`), wantErr: true,
		},
		{
			name: "atomic vision capability", toolName: agentruntime.ToolVisionAnalyze,
			toolCallID: visionCall.ToolCallID, actionVersion: visionCall.ActionVersion,
			arguments: visionCall.Arguments, want: visionTaskID,
		},
		{
			name: "atomic media capability", toolName: agentruntime.ToolMediaGenerate,
			arguments: []byte(`{"mediaKind":"image","modelRecordId":"runtime-image-model","modelKey":"kz_gpt_image2","parameters":{"prompt":"鲜橙产品特写","aspectRatio":"1:1","resolution":"1K","quality":"medium","count":1},"sourceResourceIds":[],"targetCanvasNodeId":"image-node-1","clientRequestId":"runtime-image-outbox"}`),
			want: MediaAttemptIdentity(scope, MediaGenerationCommand{
				ArtifactRevisionID: agentMediaCapabilityIdentity(scope, agentruntime.MediaGenerateArguments{
					MediaKind: agentruntime.MediaKindImage, ModelRecordID: "runtime-image-model", ModelKey: "kz_gpt_image2",
					Parameters:        json.RawMessage(`{"prompt":"鲜橙产品特写","aspectRatio":"1:1","resolution":"1K","quality":"medium","count":1}`),
					SourceResourceIDs: []string{}, TargetCanvasNodeID: "image-node-1", ClientRequestID: "runtime-image-outbox",
				}),
				Attempt: 1, TaskType: "canvas_image", Capability: "image",
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := agentruntime.RuntimeState{
				Status: agentruntime.RunWaitingTool, PendingToolStarted: true,
				PendingToolCall: &agentruntime.ToolCallDecision{
					ToolCallID: test.toolCallID, ToolName: test.toolName,
					ActionVersion: test.actionVersion, Arguments: test.arguments,
				},
			}
			got, err := expectedAgentTaskID(state, scope, agentWakeGenerationTaskFinished)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected retired media envelope to fail closed")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("expected task ID = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSettlementAuditPreservesCancelledLateSuccessWithTaskOutbox(t *testing.T) {
	_, db := newMembershipTestService(t)
	if err := db.AutoMigrate(&model.Result{}, &model.TaskOutbox{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	order := model.BillingOrder{
		ID: "late-success-order", UserID: "late-success-user", TaskID: "late-success-task",
		IdempotencyKey: "late-success-order", ChannelID: "late-channel", Model: "late-model",
		Capability: "video", Scene: "agent", BillingMode: "per_second", AmountMicrocredits: 2_000_000,
		Status: model.BillingStatusRunning, ProviderRequestID: "late-provider-request", CreatedAt: now, UpdatedAt: now,
	}
	leaseExpiry := now.Add(time.Minute)
	cancelledAt := now.Add(-time.Second)
	task := model.Task{
		ID: order.TaskID, UserID: order.UserID, Type: "canvas_video", Status: model.TaskStatusCancelled,
		BillingOrderID: order.ID, ProviderRequestID: order.ProviderRequestID, CancelRequestedAt: &cancelledAt,
		LeaseOwner: "late-worker", LeaseExpiresAt: &leaseExpiry, LeaseGeneration: 2,
		LeaseToken: "4123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		CreatedAt:  now, UpdatedAt: now, ResultJSON: `{"videos":[{"resourceId":"late-resource"}]}`,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	result := model.Result{ID: "late-success-result", UserID: task.UserID, TaskID: task.ID, Kind: "generation_result", Payload: task.ResultJSON, CreatedAt: now}
	outbox := repository.TaskOutboxDraft{
		IdempotencyKey: "late-success-outbox", EventType: model.TaskOutboxAgentRunWakeup,
		PayloadJSON: `{"runId":"late-run","actorUserId":"late-success-user","wakeup":"generation_task_finished"}`,
		AvailableAt: now,
	}
	if err := repository.New(db).SaveCancelledTaskResultWithOutbox(&task, result, "上游已返回成功，费用待核对", &outbox); err != nil {
		t.Fatal(err)
	}
	var storedTask model.Task
	var storedOrder model.BillingOrder
	var storedResult model.Result
	var storedOutbox model.TaskOutbox
	if err := db.First(&storedTask, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedResult, "id = ?", result.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedOutbox, "idempotency_key = ?", outbox.IdempotencyKey).Error; err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != model.TaskStatusCancelled || storedTask.ResultJSON == "" ||
		storedOrder.Status != model.BillingStatusUncertain || storedResult.Payload == "" || storedOutbox.Status != model.TaskOutboxPending {
		t.Fatalf("late success facts mismatch: task=%#v order=%#v result=%#v outbox=%#v", storedTask, storedOrder, storedResult, storedOutbox)
	}
}

func TestProcessClaimedTaskKeepsTaskRetryableWhenRefundCannotComplete(t *testing.T) {
	_, db := newMembershipTestService(t)
	if err := db.AutoMigrate(&model.TaskLog{}, &model.ApiCallLog{}); err != nil {
		t.Fatal(err)
	}
	svc := New(repository.New(db), t.TempDir())
	now := time.Now()
	account := model.CreditAccount{
		UserID: "task-billing-user", AvailableMicrocredits: 96_000_000,
		ReservedMicrocredits: 0, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	order := model.BillingOrder{
		ID: "task-billing-order", UserID: account.UserID, TaskID: "task-billing-task",
		IdempotencyKey: "task-billing-order", ChannelID: "task-billing-channel",
		Model: "task-billing-model", Capability: "video", Scene: "canvas",
		BillingMode: "fixed_request", PriceVersion: 1, UnitPriceMicrocredits: 1_000_000,
		MultiplierBasisPoints: 10_000, Quantity: 1, AmountMicrocredits: 1_000_000,
		Status: model.BillingStatusRunning, StartedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	leaseExpiry := now.Add(time.Minute)
	task := model.Task{
		ID: order.TaskID, UserID: account.UserID, Type: "unsupported_test_task",
		Status: model.TaskStatusRunning, BillingOrderID: order.ID, LeaseOwner: svc.workerID,
		LeaseExpiresAt: &leaseExpiry, LeaseGeneration: 1,
		LeaseToken: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		CreatedAt:  now, UpdatedAt: now,
	}
	if err := svc.sealTaskExecutionEnvelope(&task, &order, now); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	if err := svc.processClaimedTask(&task); err == nil {
		t.Fatal("processClaimedTask error = nil, want unsupported task failure")
	}

	storedTask, err := svc.repo.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != model.TaskStatusRunning {
		t.Fatalf("task status = %s, want running so the failed financial transaction can be retried", storedTask.Status)
	}
	storedOrder, err := svc.repo.BillingOrder(order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedOrder.Status != model.BillingStatusRunning {
		t.Fatalf("billing status = %s, want running", storedOrder.Status)
	}
	storedAccount, err := svc.repo.CreditAccount(account.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if storedAccount.AvailableMicrocredits != account.AvailableMicrocredits || storedAccount.ReservedMicrocredits != 0 {
		t.Fatalf("credit account changed after failed finalization: %#v", storedAccount)
	}
	var refundCount int64
	if err := db.Model(&model.CreditLedgerEntry{}).
		Where("billing_order_id = ? AND type = ?", order.ID, model.CreditLedgerRefund).
		Count(&refundCount).Error; err != nil {
		t.Fatal(err)
	}
	if refundCount != 0 {
		t.Fatalf("refund ledger count = %d, want 0", refundCount)
	}
}

func TestFailedTaskAndRefundCommitOrRollbackTogether(t *testing.T) {
	_, db := newMembershipTestService(t)
	now := time.Now()
	account := model.CreditAccount{
		UserID: "atomic-refund-user", AvailableMicrocredits: 96_000_000,
		ReservedMicrocredits: 1_000_000, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	order := model.BillingOrder{
		ID: "atomic-refund-order", UserID: account.UserID, TaskID: "atomic-refund-task",
		IdempotencyKey: "atomic-refund-order", ChannelID: "atomic-refund-channel",
		Model: "atomic-refund-model", Capability: "video", Scene: "canvas",
		BillingMode: "fixed_request", PriceVersion: 1, UnitPriceMicrocredits: 1_000_000,
		MultiplierBasisPoints: 10_000, Quantity: 1, AmountMicrocredits: 1_000_000,
		Status: model.BillingStatusRunning, StartedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	leaseExpiry := now.Add(time.Minute)
	storedTask := model.Task{
		ID: order.TaskID, UserID: account.UserID, Type: "test", Status: model.TaskStatusRunning,
		BillingOrderID: order.ID, LeaseOwner: "current-worker", LeaseExpiresAt: &leaseExpiry, LeaseGeneration: 1,
		LeaseToken: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		CreatedAt:  now, UpdatedAt: now,
	}
	if err := db.Create(&storedTask).Error; err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	failedTask := storedTask
	failedTask.LeaseOwner = "stale-worker"
	failedTask.Stage = "任务失败"
	failedTask.Error = "上游请求未发出"
	if err := repo.FinalizeFailedTaskAndBilling(&failedTask, repository.FailedTaskBillingRefund, failedTask.Error); err == nil {
		t.Fatal("stale worker finalization error = nil")
	}
	assertAtomicRefundState(t, repo, db, storedTask.ID, order.ID, account.UserID, model.TaskStatusRunning, model.BillingStatusRunning, 96_000_000, 1_000_000, 0)

	failedTask.LeaseOwner = storedTask.LeaseOwner
	if err := repo.FinalizeFailedTaskAndBilling(&failedTask, repository.FailedTaskBillingRefund, failedTask.Error); err != nil {
		t.Fatal(err)
	}
	assertAtomicRefundState(t, repo, db, storedTask.ID, order.ID, account.UserID, model.TaskStatusFailed, model.BillingStatusRefunded, 97_000_000, 0, 1)
}

func assertAtomicRefundState(
	t *testing.T,
	repo *repository.Repository,
	db *gorm.DB,
	taskID string,
	orderID string,
	userID string,
	wantTaskStatus model.TaskStatus,
	wantBillingStatus model.BillingStatus,
	wantAvailable int64,
	wantReserved int64,
	wantRefundLedgers int64,
) {
	t.Helper()
	task, err := repo.Task(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != wantTaskStatus {
		t.Fatalf("task status = %s, want %s", task.Status, wantTaskStatus)
	}
	order, err := repo.BillingOrder(orderID)
	if err != nil {
		t.Fatal(err)
	}
	if order.Status != wantBillingStatus {
		t.Fatalf("billing status = %s, want %s", order.Status, wantBillingStatus)
	}
	account, err := repo.CreditAccount(userID)
	if err != nil {
		t.Fatal(err)
	}
	if account.AvailableMicrocredits != wantAvailable || account.ReservedMicrocredits != wantReserved {
		t.Fatalf("credit account = %#v, want available=%d reserved=%d", account, wantAvailable, wantReserved)
	}
	var refundCount int64
	if err := db.Model(&model.CreditLedgerEntry{}).
		Where("billing_order_id = ? AND type = ?", orderID, model.CreditLedgerRefund).
		Count(&refundCount).Error; err != nil {
		t.Fatal(err)
	}
	if refundCount != wantRefundLedgers {
		t.Fatalf("refund ledger count = %d, want %d", refundCount, wantRefundLedgers)
	}
}
