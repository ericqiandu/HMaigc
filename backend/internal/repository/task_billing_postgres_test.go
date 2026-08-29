package repository

import (
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
