package repository

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
)

func TestPostgresFailedTaskAndRefundCommitAtomically(t *testing.T) {
	db := openPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	account := model.CreditAccount{UserID: "atomic-pg-user", AvailableMicrocredits: 96_000_000, ReservedMicrocredits: 1_000_000, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	order := model.BillingOrder{
		ID: "atomic-pg-order", UserID: account.UserID, TaskID: "atomic-pg-task", IdempotencyKey: "atomic-pg-order",
		ChannelID: "atomic-pg-channel", Model: "atomic-pg-model", Capability: "video", Scene: "canvas",
		BillingMode: "fixed_request", PriceVersion: 1, UnitPriceMicrocredits: 1_000_000,
		MultiplierBasisPoints: 10_000, Quantity: 1, AmountMicrocredits: 1_000_000,
		Status: model.BillingStatusRunning, StartedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	leaseExpiry := now.Add(time.Minute)
	task := model.Task{
		ID: order.TaskID, UserID: account.UserID, Type: "test", Status: model.TaskStatusRunning,
		BillingOrderID: order.ID, LeaseOwner: "pg-worker", LeaseExpiresAt: &leaseExpiry, LeaseGeneration: 1,
		LeaseToken: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		CreatedAt:  now, UpdatedAt: now,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	task.Stage = "任务失败"
	task.Error = "上游请求未发出"
	if err := New(db).FinalizeFailedTaskAndBilling(&task, FailedTaskBillingRefund, task.Error); err != nil {
		t.Fatal(err)
	}
	var storedTask model.Task
	var storedOrder model.BillingOrder
	var storedAccount model.CreditAccount
	var refundCount int64
	if err := db.First(&storedTask, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedAccount, "user_id = ?", account.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ? AND type = ?", order.ID, model.CreditLedgerRefund).Count(&refundCount).Error; err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != model.TaskStatusFailed || storedTask.LeaseOwner != "" || storedOrder.Status != model.BillingStatusRefunded || storedAccount.AvailableMicrocredits != 97_000_000 || storedAccount.ReservedMicrocredits != 0 || refundCount != 1 {
		t.Fatalf("atomic final state mismatch: task=%#v order=%#v account=%#v refundCount=%d", storedTask, storedOrder, storedAccount, refundCount)
	}
}

func TestPostgresStaleLeaseGenerationCannotFinalizeBilling(t *testing.T) {
	db := openPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	account := model.CreditAccount{
		UserID: "stale-lease-pg-user", AvailableMicrocredits: 96_000_000,
		ReservedMicrocredits: 1_000_000, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	order := model.BillingOrder{
		ID: "stale-lease-pg-order", UserID: account.UserID, TaskID: "stale-lease-pg-task", IdempotencyKey: "stale-lease-pg-order",
		ChannelID: "stale-lease-pg-channel", Model: "stale-lease-pg-model", Capability: "video", Scene: "canvas",
		BillingMode: "fixed_request", PriceVersion: 1, UnitPriceMicrocredits: 1_000_000,
		MultiplierBasisPoints: 10_000, Quantity: 1, AmountMicrocredits: 1_000_000,
		Status: model.BillingStatusRunning, StartedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	leaseExpiry := now.Add(time.Minute)
	currentTask := model.Task{
		ID: order.TaskID, UserID: account.UserID, Type: "test", Status: model.TaskStatusRunning,
		BillingOrderID: order.ID, LeaseOwner: "reused-pg-worker", LeaseExpiresAt: &leaseExpiry, LeaseGeneration: 2,
		LeaseToken: "2222222222222222222222222222222222222222222222222222222222222222",
		CreatedAt:  now, UpdatedAt: now,
	}
	if err := db.Create(&currentTask).Error; err != nil {
		t.Fatal(err)
	}

	staleTask := currentTask
	staleTask.LeaseGeneration = 1
	staleTask.LeaseToken = "1111111111111111111111111111111111111111111111111111111111111111"
	staleTask.Stage = "旧 worker 失败"
	staleTask.Error = "旧 worker 不得触发退款"
	err := New(db).FinalizeFailedTaskAndBilling(&staleTask, FailedTaskBillingRefund, staleTask.Error)
	if !errors.Is(err, ErrTaskCompletionStateConflict) {
		t.Fatalf("stale finalization error = %v", err)
	}

	var storedTask model.Task
	var storedOrder model.BillingOrder
	var storedAccount model.CreditAccount
	var ledgerCount int64
	if err := db.First(&storedTask, "id = ?", currentTask.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedAccount, "user_id = ?", account.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ?", order.ID).Count(&ledgerCount).Error; err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != model.TaskStatusRunning || storedTask.LeaseGeneration != 2 || storedTask.LeaseToken != currentTask.LeaseToken ||
		storedOrder.Status != model.BillingStatusRunning || storedAccount.AvailableMicrocredits != account.AvailableMicrocredits ||
		storedAccount.ReservedMicrocredits != account.ReservedMicrocredits || ledgerCount != 0 {
		t.Fatalf("stale finalization mutated facts: task=%#v order=%#v account=%#v ledgerCount=%d", storedTask, storedOrder, storedAccount, ledgerCount)
	}
}

func TestPostgresAtomicCompletionCommitsSettlementResultAndTaskOutbox(t *testing.T) {
	db := openPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	account := model.CreditAccount{
		UserID: "atomic-success-pg-user", AvailableMicrocredits: 96_000_000,
		ReservedMicrocredits: 1_000_000, CreatedAt: now, UpdatedAt: now,
	}
	order := model.BillingOrder{
		ID: "atomic-success-pg-order", UserID: account.UserID, TaskID: "atomic-success-pg-task",
		IdempotencyKey: "atomic-success-pg-order", ChannelID: "atomic-success-pg-channel", Model: "atomic-success-pg-model",
		Capability: "video", Scene: "agent", BillingMode: "per_second", PriceVersion: 1,
		UnitPriceMicrocredits: 1_000_000, MultiplierBasisPoints: 10_000, Quantity: 1,
		AmountMicrocredits: 1_000_000, Status: model.BillingStatusRunning,
		ProviderRequestID: "atomic-success-provider", StartedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	leaseExpiry := now.Add(time.Minute)
	task := model.Task{
		ID: order.TaskID, UserID: account.UserID, Type: "canvas_video", Status: model.TaskStatusRunning,
		BillingOrderID: order.ID, ProviderRequestID: order.ProviderRequestID, LeaseOwner: "atomic-success-pg-worker",
		LeaseExpiresAt: &leaseExpiry, LeaseGeneration: 1,
		LeaseToken: "5123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
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
	completed.Stage = "任务完成"
	completed.Progress = 100
	completed.ResultJSON = `{"videos":[{"resourceId":"atomic-success-pg-resource"}]}`
	completed.CompletedAt = &now
	result := model.Result{
		ID: "atomic-success-pg-result", UserID: task.UserID, TaskID: task.ID,
		Kind: "generation_result", Payload: completed.ResultJSON, CreatedAt: now,
	}
	outbox := TaskOutboxDraft{
		IdempotencyKey: "atomic-success-pg-outbox", EventType: model.TaskOutboxAgentRunWakeup,
		PayloadJSON: `{"taskId":"atomic-success-pg-task","runId":"atomic-success-pg-run","actorUserId":"atomic-success-pg-user","wakeup":"generation_task_finished"}`,
		AvailableAt: now,
	}
	if err := New(db).FinalizeSucceededTaskAndBilling(SucceededTaskFinalization{
		Task: &completed, Results: []model.Result{result}, BillingAction: CompletedTaskBillingSettle, Outbox: &outbox,
	}); err != nil {
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
	if storedTask.Status != model.TaskStatusSucceeded || storedOrder.Status != model.BillingStatusSettled ||
		storedResult.TaskID != task.ID || storedOutbox.Status != model.TaskOutboxPending {
		t.Fatalf("postgres atomic success mismatch: task=%#v order=%#v result=%#v outbox=%#v", storedTask, storedOrder, storedResult, storedOutbox)
	}
}

func TestPostgresDirectResponseUsageCompletionCommitsBillingTaskAndOutboxAtomically(t *testing.T) {
	db := openPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	pricingJSON, err := json.Marshal(struct {
		InputPerMillionMicros  int64 `json:"inputPerMillionMicros"`
		CachedPerMillionMicros int64 `json:"cachedPerMillionMicros"`
		OutputPerMillionMicros int64 `json:"outputPerMillionMicros"`
		MaxOutputTokens        int64 `json:"maxOutputTokens"`
	}{1_000_000, 500_000, 2_000_000, 1_000})
	if err != nil {
		t.Fatal(err)
	}
	account := model.CreditAccount{UserID: "direct-response-pg-user", AvailableMicrocredits: 97_000_000, ReservedMicrocredits: 3_000_000, CreatedAt: now, UpdatedAt: now}
	order := model.BillingOrder{
		ID: "direct-response-pg-order", UserID: account.UserID, TaskID: "direct-response-pg-task", IdempotencyKey: "direct-response-pg-order",
		ChannelID: "direct-response-pg-channel", ChannelModelID: "direct-response-pg-model", Model: "deepseek-v4-flash-vision-exp",
		Capability: "vision", Scene: "agent_vision", BillingMode: "token_usage", MultiplierBasisPoints: 10_000,
		AmountMicrocredits: 3_000_000, ReservedAmountMicrocredits: 3_000_000, TokenPricingSnapshotJSON: string(pricingJSON),
		Status: model.BillingStatusRunning, StartedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	leaseExpiry := now.Add(time.Minute)
	task := model.Task{
		ID: order.TaskID, UserID: account.UserID, Type: "agent_vision_analysis", Status: model.TaskStatusRunning,
		BillingOrderID: order.ID, ProviderRequestID: "resp-direct-response-pg", LeaseOwner: "direct-response-pg-worker",
		LeaseExpiresAt: &leaseExpiry, LeaseGeneration: 1,
		LeaseToken: "6123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", CreatedAt: now, UpdatedAt: now,
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
	completed.ResultJSON = `{"analysis":"done"}`
	completed.CompletedAt = &now
	settlement := ResponseUsageSettlementFact{
		ProviderRequestID: task.ProviderRequestID,
		Usage:             TokenUsageFact{InputTokens: 1_000, CachedTokens: 200, OutputTokens: 100},
		UsageStatus:       "reported", AmountMicrocredits: 1_600, SettledAt: now,
	}
	outbox := TaskOutboxDraft{
		IdempotencyKey: "direct-response-pg-outbox", EventType: model.TaskOutboxAgentRunWakeup,
		PayloadJSON: `{"taskId":"direct-response-pg-task"}`, AvailableAt: now,
	}
	if err := New(db).FinalizeSucceededTaskAndBilling(SucceededTaskFinalization{
		Task: &completed, BillingAction: CompletedTaskBillingSettleFromUsage,
		ResponseUsageSettlement: &settlement, Outbox: &outbox,
	}); err != nil {
		t.Fatal(err)
	}
	var storedTask model.Task
	var storedOrder model.BillingOrder
	var storedAccount model.CreditAccount
	var storedOutbox model.TaskOutbox
	if err := db.First(&storedTask, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedAccount, "user_id = ?", account.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedOutbox, "idempotency_key = ?", outbox.IdempotencyKey).Error; err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != model.TaskStatusSucceeded || storedOrder.Status != model.BillingStatusSettled ||
		storedOrder.ProviderRequestID != settlement.ProviderRequestID || storedAccount.AvailableMicrocredits != 99_998_400 ||
		storedAccount.ReservedMicrocredits != 0 || storedOutbox.Status != model.TaskOutboxPending {
		t.Fatalf("postgres direct response completion mismatch: task=%#v order=%#v account=%#v outbox=%#v", storedTask, storedOrder, storedAccount, storedOutbox)
	}
}
