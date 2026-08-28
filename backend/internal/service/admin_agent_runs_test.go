package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAdminAgentRunsRequiresAdministrator(t *testing.T) {
	svc, _ := openAdminAgentRunServiceSQLite(t)
	query := repository.AdminAgentRunQuery{Page: 1, PageSize: 20}
	if _, err := svc.AdminAgentRuns(context.Background(), &model.User{ID: "ordinary", Role: model.UserRoleUser}, query); err == nil {
		t.Fatal("ordinary user listed cross-tenant agent runs")
	} else {
		var authErr *AuthError
		if !errors.As(err, &authErr) || authErr.Status != 403 {
			t.Fatalf("ordinary list error = %v", err)
		}
	}
	if _, err := svc.AdminAgentRun(context.Background(), &model.User{ID: "ordinary", Role: model.UserRoleUser}, "run-hidden"); err == nil {
		t.Fatal("ordinary user read a cross-tenant agent run")
	}
	if _, err := svc.InterruptAdminAgentRun(context.Background(), &model.User{ID: "ordinary", Role: model.UserRoleUser}, AdminAgentRunInterruptRequest{
		RunID: "run-hidden", ExpectedStateVersion: 4, Reason: "普通用户不得终止", Confirmation: "STOP run-hidd",
	}); err == nil {
		t.Fatal("ordinary user interrupted a cross-tenant agent run")
	}
}

func TestInterruptAdminAgentRunValidatesConfirmationReasonAndAuthority(t *testing.T) {
	svc, db := openAdminAgentRunServiceSQLite(t)
	now := time.Now().UTC().Add(-time.Minute)
	createServiceAdminRunFixture(t, db, "run-service-control", agentruntime.RunQueued, now)
	admin := &model.User{ID: "admin-service", Username: "admin-service", Role: model.UserRoleAdmin, Status: model.UserStatusActive}

	detail, err := svc.AdminAgentRun(context.Background(), admin, "run-service-control")
	if err != nil {
		t.Fatal(err)
	}
	if detail.ConfirmationPhrase != "STOP run-serv" || detail.StateVersion != 4 {
		t.Fatalf("detail = %#v", detail)
	}
	for _, testCase := range []struct {
		name     string
		reason   string
		confirm  string
		wantCode string
	}{
		{name: "short reason", reason: "短", confirm: detail.ConfirmationPhrase, wantCode: "admin_agent_run_interrupt_blocked"},
		{name: "wrong confirmation", reason: "停止无人处理的运行", confirm: "STOP wrong", wantCode: "admin_agent_run_confirmation_invalid"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, interruptErr := svc.InterruptAdminAgentRun(context.Background(), admin, AdminAgentRunInterruptRequest{
				RunID: detail.RunID, ExpectedStateVersion: detail.StateVersion, Reason: testCase.reason, Confirmation: testCase.confirm,
			})
			var controlErr *AdminAgentRunControlError
			if !errors.As(interruptErr, &controlErr) || controlErr.ErrorCode != testCase.wantCode {
				t.Fatalf("interrupt error = %v", interruptErr)
			}
		})
	}
	var before model.AgentRun
	if err := db.First(&before, "id = ?", detail.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if before.Status != agentruntime.RunQueued || before.StateVersion != detail.StateVersion {
		t.Fatalf("invalid requests changed run = %#v", before)
	}

	response, err := svc.InterruptAdminAgentRun(context.Background(), admin, AdminAgentRunInterruptRequest{
		RunID: detail.RunID, ExpectedStateVersion: detail.StateVersion,
		Reason: "  超过运营窗口，管理员确认终止  ", Confirmation: detail.ConfirmationPhrase,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Run.Status != agentruntime.RunCancelled || response.Run.StateVersion != detail.StateVersion+1 || response.Disposition != repository.AdminAgentRunInterruptibleNow {
		t.Fatalf("interrupt response = %#v", response)
	}

	_, err = svc.InterruptAdminAgentRun(context.Background(), admin, AdminAgentRunInterruptRequest{
		RunID: detail.RunID, ExpectedStateVersion: detail.StateVersion,
		Reason: "重复提交不应增加终态事实", Confirmation: detail.ConfirmationPhrase,
	})
	var terminalErr *AdminAgentRunControlError
	if !errors.As(err, &terminalErr) || terminalErr.ErrorCode != "admin_agent_run_terminal" || terminalErr.Latest == nil || terminalErr.Latest.Status != agentruntime.RunCancelled {
		t.Fatalf("terminal replay error = %#v", err)
	}
}

func TestInterruptAdminAgentRunRejectsUnresolvedBilling(t *testing.T) {
	svc, db := openAdminAgentRunServiceSQLite(t)
	now := time.Now().UTC().Add(-time.Minute)
	createServiceAdminRunFixture(t, db, "run-service-billing", agentruntime.RunRunning, now)
	order := model.BillingOrder{
		ID: "order-service-billing", UserID: "user-run-service-billing",
		IdempotencyKey: "agent-runtime:run-service-billing:2", Status: model.BillingStatusUncertain,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	admin := &model.User{ID: "admin-service", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	detail, err := svc.AdminAgentRun(context.Background(), admin, "run-service-billing")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.InterruptAdminAgentRun(context.Background(), admin, AdminAgentRunInterruptRequest{
		RunID: detail.RunID, ExpectedStateVersion: detail.StateVersion,
		Reason: "账务状态未决，必须先核对", Confirmation: detail.ConfirmationPhrase,
	})
	var controlErr *AdminAgentRunControlError
	if !errors.As(err, &controlErr) || controlErr.ErrorCode != "admin_agent_run_billing_unresolved" || controlErr.Latest == nil {
		t.Fatalf("billing error = %#v", err)
	}
}

func TestInterruptAdminAgentRunDisposesLinkedModelTasksAndBilling(t *testing.T) {
	t.Run("queued request refunds untouched reservation", func(t *testing.T) {
		svc, db := openAdminAgentRunServiceSQLite(t)
		now := time.Now().UTC().Add(-time.Minute)
		const runID = "run-service-queued-task"
		createServiceAdminRunFixture(t, db, runID, agentruntime.RunQueued, now)
		task, order := createServiceAdminRunTaskBilling(t, db, runID, model.TaskStatusQueued, model.BillingStatusReserved, "", now)
		admin := &model.User{ID: "admin-service", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
		detail, err := svc.AdminAgentRun(context.Background(), admin, runID)
		if err != nil {
			t.Fatal(err)
		}
		if detail.ControlDisposition != repository.AdminAgentRunCancelRequestRequired {
			t.Fatalf("queued disposition = %#v", detail)
		}
		response, err := svc.InterruptAdminAgentRun(context.Background(), admin, AdminAgentRunInterruptRequest{
			RunID: runID, ExpectedStateVersion: detail.StateVersion,
			Reason: "管理员终止尚未提交的模型任务", Confirmation: detail.ConfirmationPhrase,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(response.AffectedTaskIDs) != 1 || response.AffectedTaskIDs[0] != task.ID || response.ReconciliationPending {
			t.Fatalf("queued response = %#v", response)
		}
		assertServiceAdminTaskBilling(t, db, task.ID, order.ID, model.TaskStatusCancelled, model.BillingStatusRefunded, 1_000, 0)
	})

	t.Run("submitted request records cancellation and reconciliation", func(t *testing.T) {
		svc, db := openAdminAgentRunServiceSQLite(t)
		now := time.Now().UTC().Add(-time.Minute)
		const runID = "run-service-provider-task"
		createServiceAdminRunFixture(t, db, runID, agentruntime.RunRunning, now)
		task, order := createServiceAdminRunTaskBilling(t, db, runID, model.TaskStatusRunning, model.BillingStatusRunning, "provider-request-secret", now)
		cancelled := false
		svc.registerActiveTask(task.ID, func() { cancelled = true })
		admin := &model.User{ID: "admin-service", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
		detail, err := svc.AdminAgentRun(context.Background(), admin, runID)
		if err != nil {
			t.Fatal(err)
		}
		if detail.ControlDisposition != repository.AdminAgentRunCancelRequestRequired || detail.ProviderRequestState != "submitted" {
			t.Fatalf("provider disposition = %#v", detail)
		}
		response, err := svc.InterruptAdminAgentRun(context.Background(), admin, AdminAgentRunInterruptRequest{
			RunID: runID, ExpectedStateVersion: detail.StateVersion,
			Reason: "管理员终止已提交的模型请求", Confirmation: detail.ConfirmationPhrase,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !cancelled || len(response.AffectedTaskIDs) != 1 || response.AffectedTaskIDs[0] != task.ID || !response.ReconciliationPending {
			t.Fatalf("provider response = %#v cancelled=%t", response, cancelled)
		}
		assertServiceAdminTaskBilling(t, db, task.ID, order.ID, model.TaskStatusCancelled, model.BillingStatusUncertain, 900, 100)
	})
}

func TestInterruptAdminAgentRunDisposesLinkedMediaTaskWithoutDiscardingReconciliation(t *testing.T) {
	svc, db := openAdminAgentRunServiceSQLite(t)
	now := time.Now().UTC().Add(-time.Minute)
	const runID = "run-service-media-task"
	createServiceAdminRunFixture(t, db, runID, agentruntime.RunWaitingTool, now)
	task, order := createServiceAdminRunTaskBilling(t, db, runID, model.TaskStatusRunning, model.BillingStatusRunning, "provider-media-secret", now)
	if err := db.Model(&model.Task{}).Where("id = ?", task.ID).
		Select("operation", "type", "capability").
		Updates(model.Task{Operation: "", Type: "canvas_video", Capability: "video"}).Error; err != nil {
		t.Fatal(err)
	}
	plan := model.AgentProductionPlanVersion{
		ID: "plan-" + runID, PlanKey: "plan-key-" + runID, TenantKind: agentruntime.TenantPersonal,
		TenantID: task.UserID, DomainProjectID: "project-" + runID, CanvasID: "canvas-" + runID,
		CreatedByRunID: runID, Version: 1, Status: model.AgentProductionPlanActive,
		Title: "媒体计划", Script: "媒体脚本", ReferencesJSON: `[]`, ShotsJSON: `[]`, ExpectedDeliveryJSON: `{}`,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	artifact := model.AgentProductionArtifact{
		ID: "artifact-" + runID, PlanKey: plan.PlanKey, PlanVersionID: plan.ID, PlanVersion: 1,
		Kind: model.AgentProductionArtifactVideoClip, Status: model.AgentProductionArtifactRunning,
		TaskID: task.ID, BillingOrderID: order.ID, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&artifact).Error; err != nil {
		t.Fatal(err)
	}
	cancelled := false
	svc.registerActiveTask(task.ID, func() { cancelled = true })
	admin := &model.User{ID: "admin-service", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	detail, err := svc.AdminAgentRun(context.Background(), admin, runID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.LinkedMediaTaskStatus != string(model.TaskStatusRunning) || detail.ControlDisposition != repository.AdminAgentRunCancelRequestRequired {
		t.Fatalf("media detail = %#v", detail)
	}
	response, err := svc.InterruptAdminAgentRun(context.Background(), admin, AdminAgentRunInterruptRequest{
		RunID: runID, ExpectedStateVersion: detail.StateVersion,
		Reason: "停止媒体任务并保留迟到结果核对", Confirmation: detail.ConfirmationPhrase,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cancelled || !response.ReconciliationPending || len(response.AffectedTaskIDs) != 1 || response.AffectedTaskIDs[0] != task.ID {
		t.Fatalf("media response = %#v cancelled=%t", response, cancelled)
	}
	var storedTask model.Task
	if err := db.First(&storedTask, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != model.TaskStatusCancelled || storedTask.PollStage != "cancel_reconcile" || storedTask.ProviderRequestID != task.ProviderRequestID {
		t.Fatalf("media cancellation fact = %#v", storedTask)
	}
	var storedArtifact model.AgentProductionArtifact
	if err := db.First(&storedArtifact, "id = ?", artifact.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedArtifact.Status != model.AgentProductionArtifactRunning || storedArtifact.TaskID != task.ID || storedArtifact.BillingOrderID != order.ID {
		t.Fatalf("started media artifact was rewritten = %#v", storedArtifact)
	}
}

func TestInterruptAdminAgentRunDoesNotRefundSettledBilling(t *testing.T) {
	svc, db := openAdminAgentRunServiceSQLite(t)
	now := time.Now().UTC().Add(-time.Minute)
	const runID = "run-service-settled"
	createServiceAdminRunFixture(t, db, runID, agentruntime.RunQueued, now)
	userID := "user-" + runID
	task := model.Task{
		ID: "task-" + runID, UserID: userID, Audience: model.TaskAudienceInternal,
		Type: "agent_runtime_model", Capability: "text", Status: model.TaskStatusSucceeded,
		Operation: "agent_model:" + runID, BillingOrderID: "order-" + runID, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	order := model.BillingOrder{
		ID: task.BillingOrderID, UserID: userID, TaskID: task.ID, IdempotencyKey: "agent-runtime:" + runID + ":1",
		Status: model.BillingStatusSettled, AmountMicrocredits: 100, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	admin := &model.User{ID: "admin-service", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	detail, err := svc.AdminAgentRun(context.Background(), admin, runID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.BillingState != string(model.BillingStatusSettled) || detail.ControlDisposition != repository.AdminAgentRunInterruptibleNow {
		t.Fatalf("settled detail = %#v", detail)
	}
	response, err := svc.InterruptAdminAgentRun(context.Background(), admin, AdminAgentRunInterruptRequest{
		RunID: runID, ExpectedStateVersion: detail.StateVersion,
		Reason: "终止运行但不得改写已结算订单", Confirmation: detail.ConfirmationPhrase,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.AffectedTaskIDs) != 0 || response.ReconciliationPending {
		t.Fatalf("settled response = %#v", response)
	}
	var storedOrder model.BillingOrder
	if err := db.First(&storedOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedOrder.Status != model.BillingStatusSettled {
		t.Fatalf("settled billing changed = %#v", storedOrder)
	}
}

func openAdminAgentRunServiceSQLite(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared&_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.Task{}, &model.BillingOrder{}, &model.CreditAccount{}, &model.CreditLedgerEntry{},
		&model.AgentThread{}, &model.AgentRun{}, &model.AgentTimelineItem{}, &model.AgentRunEvent{},
		&model.AgentCheckpoint{}, &model.AgentToolCall{}, &model.AgentProductionPlanVersion{}, &model.AgentProductionArtifact{},
	); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureAgentProductionRuntimeSchema(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	return New(repo, t.TempDir()), db
}

func createServiceAdminRunFixture(t *testing.T, db *gorm.DB, runID string, status agentruntime.RunStatus, now time.Time) {
	t.Helper()
	userID := "user-" + runID
	user := model.User{ID: userID, Username: userID, Email: runID + "@example.com", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	threadID := "thread-" + runID
	thread := model.AgentThread{
		ID: threadID, TenantKind: agentruntime.TenantPersonal, TenantID: userID, CreatedByUserID: userID,
		DomainProjectID: "project-" + runID, CanvasID: "canvas-" + runID, Status: agentruntime.ThreadActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&thread).Error; err != nil {
		t.Fatal(err)
	}
	state := agentruntime.RuntimeState{
		StateVersion: 4, StepNumber: 2, MaxSteps: 24, Status: status,
		UserMessage: "测试管理员终止", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic},
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	run := model.AgentRun{
		ID: runID, ThreadID: threadID, ActorUserID: userID, ClientRequestID: "request-" + runID,
		Status: status, LastEventSequence: 7, StateVersion: state.StateVersion, StepNumber: state.StepNumber, MaxSteps: state.MaxSteps,
		ModelRecordID: "model-record", ModelKey: "agent-model", ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion: agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	checkpoint := model.AgentCheckpoint{
		ID: "checkpoint-" + runID, RunID: runID, Sequence: 7, StateVersion: state.StateVersion, StateJSON: string(stateJSON), CreatedAt: now,
	}
	if err := db.Create(&checkpoint).Error; err != nil {
		t.Fatal(err)
	}
}

func createServiceAdminRunTaskBilling(
	t *testing.T,
	db *gorm.DB,
	runID string,
	taskStatus model.TaskStatus,
	billingStatus model.BillingStatus,
	providerRequestID string,
	now time.Time,
) (model.Task, model.BillingOrder) {
	t.Helper()
	userID := "user-" + runID
	account := model.CreditAccount{
		UserID: userID, AvailableMicrocredits: 900, ReservedMicrocredits: 100, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	task := model.Task{
		ID: "task-" + runID, UserID: userID, Audience: model.TaskAudienceInternal,
		Type: "agent_runtime_model", Capability: "text", Status: taskStatus,
		Operation: "agent_model:" + runID, BillingOrderID: "order-" + runID,
		ProviderRequestID: providerRequestID, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	order := model.BillingOrder{
		ID: task.BillingOrderID, UserID: userID, TaskID: task.ID,
		IdempotencyKey: "agent-runtime:" + runID + ":2", Capability: "text", Scene: "agent_runtime_model",
		AmountMicrocredits: 100, ReservedAmountMicrocredits: 100, Status: billingStatus,
		ProviderRequestID: providerRequestID, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	return task, order
}

func assertServiceAdminTaskBilling(
	t *testing.T,
	db *gorm.DB,
	taskID string,
	orderID string,
	wantTask model.TaskStatus,
	wantBilling model.BillingStatus,
	wantAvailable int64,
	wantReserved int64,
) {
	t.Helper()
	var task model.Task
	if err := db.First(&task, "id = ?", taskID).Error; err != nil {
		t.Fatal(err)
	}
	var order model.BillingOrder
	if err := db.First(&order, "id = ?", orderID).Error; err != nil {
		t.Fatal(err)
	}
	var account model.CreditAccount
	if err := db.First(&account, "user_id = ?", task.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if task.Status != wantTask || order.Status != wantBilling || account.AvailableMicrocredits != wantAvailable || account.ReservedMicrocredits != wantReserved {
		t.Fatalf("task=%#v order=%#v account=%#v", task, order, account)
	}
}
