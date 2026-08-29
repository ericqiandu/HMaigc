package service

import (
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUserTaskOperationsCannotAccessInternalTask(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.TaskLog{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	internal := model.Task{ID: "internal", UserID: "user-1", Audience: model.TaskAudienceInternal, Type: agentRuntimeModelTaskType, Status: model.TaskStatusFailed, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&internal).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TaskLog{ID: "log", UserID: internal.UserID, TaskID: internal.ID, Message: "private", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db)}

	for name, operation := range map[string]func() error{
		"detail": func() error { _, err := svc.Task(internal.UserID, internal.ID); return err },
		"logs":   func() error { _, err := svc.TaskLogs(internal.UserID, internal.ID); return err },
		"cancel": func() error { _, err := svc.CancelTask(internal.UserID, internal.ID); return err },
		"retry":  func() error { _, err := svc.RetryTask(internal.UserID, internal.ID); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := operation(); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("error = %v, want record not found", err)
			}
		})
	}
	stored, err := svc.repo.Task(internal.ID)
	if err != nil || stored.Status != model.TaskStatusFailed {
		t.Fatalf("internal worker task = %#v, error = %v", stored, err)
	}
}

func TestCancellationIntentPersistsWhenActiveContextIsAbsent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.TaskLog{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	task := model.Task{
		ID: "cancel-without-active-context", UserID: "cancel-user", Audience: model.TaskAudienceCustomer,
		ExecutionKind: model.TaskExecutionProvider, Status: model.TaskStatusRunning,
		LeaseOwner: "dead-worker", LeaseGeneration: 7, LeaseToken: "dead-lease-token",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db)}

	cancelled, err := svc.CancelTask(task.UserID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.CancelRequestedAt == nil || cancelled.CancelReasonCode != "user_requested" {
		t.Fatalf("cancelled task facts = %#v", cancelled)
	}
	if cancelled.LeaseOwner != "" || cancelled.LeaseToken != "" || cancelled.LeaseGeneration <= task.LeaseGeneration {
		t.Fatalf("cancelled task lease was not invalidated: %#v", cancelled)
	}
}

func TestProcessClaimedTaskStopsWhenCancellationInvalidatesClaimBeforeExecution(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.TaskLog{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	claimed := model.Task{
		ID: "cancel-before-execution", UserID: "cancel-user", Audience: model.TaskAudienceCustomer,
		ExecutionKind: model.TaskExecutionProvider, Status: model.TaskStatusRunning,
		LeaseOwner: "claimed-worker", LeaseGeneration: 3, LeaseToken: "claimed-lease-token",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&claimed).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db)}
	if _, err := svc.CancelTask(claimed.UserID, claimed.ID); err != nil {
		t.Fatal(err)
	}

	if err := svc.processClaimedTask(&claimed); err != nil {
		t.Fatalf("processClaimedTask() error = %v, want cancelled stale claim to stop without execution", err)
	}
	stored, err := svc.repo.Task(claimed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.TaskStatusCancelled || stored.CancelRequestedAt == nil {
		t.Fatalf("stored task = %#v, want durable cancellation", stored)
	}
}
