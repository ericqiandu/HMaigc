package repository

import (
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUpsertTaskResultRequiresTaskScopeAndKeepsOneResultPerKind(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.Result{}); err != nil {
		t.Fatal(err)
	}
	repo := New(db)
	now := time.Date(2026, time.September, 7, 12, 0, 0, 0, time.UTC)
	result := model.Result{ID: "result-1", UserID: "user-1", TaskID: "task-1", SessionID: "session-1", Kind: "image_prompt_evidence", Payload: `{"revision":1}`, CreatedAt: now}
	if err := repo.UpsertTaskResult(result); err == nil {
		t.Fatal("result without an owning task was accepted")
	}
	if err := db.Create(&model.Task{ID: "task-1", UserID: "user-1", SessionID: "session-1", Status: model.TaskStatusRunning, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertTaskResult(result); err != nil {
		t.Fatalf("create result: %v", err)
	}
	result.ID = "result-2"
	result.Payload = `{"revision":2}`
	if err := repo.UpsertTaskResult(result); err != nil {
		t.Fatalf("update result: %v", err)
	}
	var results []model.Result
	if err := db.Where("task_id = ? AND kind = ?", result.TaskID, result.Kind).Find(&results).Error; err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != "result-1" || results[0].Payload != result.Payload {
		t.Fatalf("stored results = %#v", results)
	}
	conflicting := result
	conflicting.UserID = "user-2"
	if err := repo.UpsertTaskResult(conflicting); err == nil {
		t.Fatal("cross-user result scope was accepted")
	}
}
