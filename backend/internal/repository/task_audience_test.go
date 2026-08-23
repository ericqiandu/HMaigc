package repository

import (
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCustomerTaskQueriesExcludeInternalTasksBeforeLimit(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.TaskLog{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	tasks := []model.Task{
		{ID: "customer", UserID: "user-1", Audience: model.TaskAudienceCustomer, Type: "canvas_image", Status: model.TaskStatusQueued, CreatedAt: now.Add(-time.Minute), UpdatedAt: now},
		{ID: "internal", UserID: "user-1", Audience: model.TaskAudienceInternal, Type: "agent_runtime_model", Status: model.TaskStatusQueued, CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TaskLog{ID: "internal-log", UserID: "user-1", TaskID: "internal", Message: "secret", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}

	repo := New(db)
	listed, err := repo.Tasks("user-1", 1, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != "customer" {
		t.Fatalf("customer list = %#v", listed)
	}
	if _, err := repo.TaskForCustomer("user-1", "internal"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("internal detail error = %v, want record not found", err)
	}
	logs, err := repo.TaskLogsForCustomer("user-1", "internal")
	if !errors.Is(err, gorm.ErrRecordNotFound) || logs != nil {
		t.Fatalf("internal logs = %#v, error = %v", logs, err)
	}
	internal, err := repo.Task("internal")
	if err != nil || internal.Audience != model.TaskAudienceInternal {
		t.Fatalf("unrestricted internal task = %#v, error = %v", internal, err)
	}
}
