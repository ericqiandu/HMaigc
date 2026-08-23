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
