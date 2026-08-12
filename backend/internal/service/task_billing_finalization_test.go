package service

import (
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

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
		LeaseExpiresAt: &leaseExpiry, CreatedAt: now, UpdatedAt: now,
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
		BillingOrderID: order.ID, LeaseOwner: "current-worker", LeaseExpiresAt: &leaseExpiry,
		CreatedAt: now, UpdatedAt: now,
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
