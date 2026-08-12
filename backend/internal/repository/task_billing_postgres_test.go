package repository

import (
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
		BillingOrderID: order.ID, LeaseOwner: "pg-worker", LeaseExpiresAt: &leaseExpiry,
		CreatedAt: now, UpdatedAt: now,
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
