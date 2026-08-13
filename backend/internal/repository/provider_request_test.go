package repository

import (
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
)

func TestSaveProviderCallKeepsUpstreamIDWhenAuditInsertFails(t *testing.T) {
	db := openProviderRepositorySQLite(t)
	repo := New(db)
	now := time.Now().UTC()
	task := model.Task{
		ID: "task-provider-response", UserID: "user", Status: model.TaskStatusRunning,
		LeaseOwner: "worker", LeaseExpiresAt: &now, PollStage: "creating",
	}
	order := model.BillingOrder{ID: "order-provider-response", UserID: task.UserID, TaskID: task.ID, Status: model.BillingStatusRunning}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_provider_audit BEFORE INSERT ON api_call_logs BEGIN SELECT RAISE(FAIL, 'audit unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}

	log := &model.ApiCallLog{
		ID: "log-provider-response", UserID: task.UserID, TaskID: task.ID, BillingOrderID: order.ID,
		Status: model.ApiCallStatusSucceeded, StatusCode: 200, ProviderRequestID: "upstream-task-id",
	}
	err := repo.SaveProviderCall(log, task.LeaseOwner, true)
	if err == nil {
		t.Fatal("SaveProviderCall() error = nil, want audit failure")
	}
	storedTask, taskErr := repo.Task(task.ID)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	if storedTask.ProviderRequestID != "upstream-task-id" || storedTask.PollStage != "accepted" {
		t.Fatalf("stored task provider facts = id:%q stage:%q", storedTask.ProviderRequestID, storedTask.PollStage)
	}
	storedOrder, orderErr := repo.BillingOrder(order.ID)
	if orderErr != nil {
		t.Fatal(orderErr)
	}
	if storedOrder.ProviderRequestID != "upstream-task-id" {
		t.Fatalf("billing provider request id = %q", storedOrder.ProviderRequestID)
	}
	if beginErr := repo.BeginProviderCreate(task.ID, task.LeaseOwner); !errors.Is(beginErr, ErrProviderCreateStateConflict) {
		t.Fatalf("BeginProviderCreate() error = %v, want state conflict", beginErr)
	}
}
