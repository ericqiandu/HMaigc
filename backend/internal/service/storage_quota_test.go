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

func TestValidateStructuredStorageQuotaRejectsBytesAndCounts(t *testing.T) {
	policy := defaultRuntimePolicy().Resource
	usage := repository.UserStorageUsage{AssetBytes: megabytes(policy.StructuredDataMB) - 8, AssetCount: policy.AssetCount}
	if err := validateStructuredStorageQuotaWithPolicy(usage, "asset", false, 9, policy); err == nil {
		t.Fatal("validateStructuredStorageQuota() byte error = nil")
	}
	if err := validateStructuredStorageQuotaWithPolicy(usage, "asset", true, 0, policy); err == nil {
		t.Fatal("validateStructuredStorageQuota() count error = nil")
	}
}

func TestValidateStructuredStorageQuotaAllowsReplacementThatShrinksData(t *testing.T) {
	policy := defaultRuntimePolicy().Resource
	usage := repository.UserStorageUsage{AssetBytes: megabytes(policy.StructuredDataMB), AssetCount: policy.AssetCount}
	if err := validateStructuredStorageQuotaWithPolicy(usage, "asset", false, -1, policy); err != nil {
		t.Fatalf("validateStructuredStorageQuota() error = %v", err)
	}
}

func TestValidateTaskStorageQuotaRejectsHistoryGrowth(t *testing.T) {
	policy := defaultRuntimePolicy().Resource
	if err := validateTaskStorageQuotaWithPolicy(repository.UserStorageUsage{TaskCount: policy.TaskCount}, 0, policy); err == nil {
		t.Fatal("validateTaskStorageQuota() count error = nil")
	}
	if err := validateTaskStorageQuotaWithPolicy(repository.UserStorageUsage{TaskBytes: gigabytes(policy.TaskDataGB)}, 1, policy); err == nil {
		t.Fatal("validateTaskStorageQuota() byte error = nil")
	}
}

func TestUserStorageUsageCountsPersistedPayloads(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.SystemSetting{}, &model.Asset{}, &model.CanvasProject{}, &model.Session{}, &model.Message{}, &model.Task{}, &model.TaskLog{}, &model.Result{}, &model.ApiCallLog{}); err != nil {
		t.Fatal(err)
	}
	items := []any{
		&model.Asset{ID: "asset-1", UserID: "user-1", PayloadJSON: "abcd"},
		&model.CanvasProject{ID: "canvas-1", UserID: "user-1", PayloadJSON: "xy"},
		&model.Session{ID: "session-1", UserID: "user-1", Prompt: "p", CanvasSnapshotJSON: "{}"},
		&model.Message{ID: "message-1", UserID: "user-1", SessionID: "session-1", Content: "hi", Payload: "z"},
		&model.Task{ID: "task-1", UserID: "user-1", Prompt: "p", InputJSON: "{}", ResultJSON: "{}"},
		&model.TaskLog{ID: "log-1", UserID: "user-1", TaskID: "task-1", Message: "m", Payload: "p"},
		&model.Result{ID: "result-1", UserID: "user-1", TaskID: "task-1", URL: "u", Payload: "r"},
		&model.ApiCallLog{ID: "api-log-1", UserID: "user-1", Path: "p", Model: "m", ProviderRequestID: "i", Error: "e", UpstreamURL: "u"},
	}
	for _, item := range items {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	usage, err := repository.New(db).UserStorageUsage("user-1")
	if err != nil {
		t.Fatal(err)
	}
	if usage.AssetCount != 1 || usage.AssetBytes != 4 || usage.CanvasCount != 1 || usage.CanvasBytes != 2 || usage.SessionCount != 1 || usage.SessionBytes != 6 || usage.TaskCount != 1 || usage.TaskBytes != 14 || usage.APICallCount != 1 {
		t.Fatalf("UserStorageUsage() = %#v", usage)
	}
}

func TestSaveTaskCompletionPersistsRelatedRowsTogether(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.SystemSetting{}, &model.Asset{}, &model.CanvasProject{}, &model.Session{}, &model.Message{}, &model.Task{}, &model.TaskLog{}, &model.Result{}, &model.ApiCallLog{}); err != nil {
		t.Fatal(err)
	}
	session := model.Session{ID: "session-1", UserID: "user-1", Status: model.SessionStatusActive}
	originalInputJSON := `{"mode":"text","secret":"must-remain-signed-at-rest"}`
	task := model.Task{ID: "task-1", UserID: "user-1", SessionID: session.ID, Status: model.TaskStatusRunning, LeaseOwner: "worker-1", InputJSON: originalInputJSON}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db)}
	if err := svc.saveTaskCompletion(&task, []byte(`{"ok":true}`), []byte(`[{"op":"add"}]`), true); err != nil {
		t.Fatal(err)
	}
	var messageCount int64
	var resultCount int64
	if err := db.Model(&model.Message{}).Count(&messageCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Result{}).Count(&resultCount).Error; err != nil {
		t.Fatal(err)
	}
	if task.Status != model.TaskStatusSucceeded || messageCount != 1 || resultCount != 2 {
		t.Fatalf("completion = status:%s messages:%d results:%d", task.Status, messageCount, resultCount)
	}
	stored, err := svc.repo.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.InputJSON != originalInputJSON || task.InputJSON != originalInputJSON {
		t.Fatalf("signed task input changed on completion: stored=%q task=%q", stored.InputJSON, task.InputJSON)
	}
	if output := taskForOutput(*stored); output.InputJSON != `{"mode":"text"}` {
		t.Fatalf("public task input = %q, want redacted metadata", output.InputJSON)
	}
}

func TestSaveTaskCompletionRejectsStaleWorkerAfterCancellation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.Result{}); err != nil {
		t.Fatal(err)
	}
	stored := model.Task{ID: "task-stale", UserID: "user", Status: model.TaskStatusCancelled, LeaseOwner: "", InputJSON: `{}`}
	if err := db.Create(&stored).Error; err != nil {
		t.Fatal(err)
	}
	stale := stored
	stale.Status = model.TaskStatusRunning
	stale.LeaseOwner = "stale-worker"
	svc := &Service{repo: repository.New(db)}
	err = svc.saveTaskCompletion(&stale, []byte(`{"ok":true}`), nil, false)
	if !errors.Is(err, repository.ErrTaskCompletionStateConflict) {
		t.Fatalf("stale completion error = %v", err)
	}
	latest, err := svc.repo.Task(stored.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Status != model.TaskStatusCancelled || latest.ResultJSON != "" {
		t.Fatalf("stale worker overwrote cancelled task = %#v", latest)
	}
}

func TestSaveCancelledTaskResultKeepsCancelledStatusAndPersistsAssetFact(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.SystemSetting{}, &model.Task{}, &model.Result{}, &model.Asset{}, &model.CanvasProject{}, &model.Session{}, &model.Message{}, &model.TaskLog{}, &model.ApiCallLog{}, &model.BillingOrder{}); err != nil {
		t.Fatal(err)
	}
	originalInputJSON := `{"mode":"video","secret":"must-remain-signed-at-rest"}`
	task := model.Task{ID: "cancelled-task", UserID: "user", Status: model.TaskStatusCancelled, BillingOrderID: "billing", ProviderRequestID: "provider-task", InputJSON: originalInputJSON}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.BillingOrder{ID: task.BillingOrderID, UserID: task.UserID, TaskID: task.ID, BillingMode: "per_second", Status: model.BillingStatusRunning, ProviderRequestID: task.ProviderRequestID, ProviderEndpointVersionID: "endpoint-v1", ProviderCredentialVersionID: "key-v1"}).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db)}
	resultJSON := []byte(`{"videoUrl":"https://cdn.example/result.mp4"}`)
	if err := svc.saveCancelledTaskResult(&task, resultJSON, "费用待核对"); err != nil {
		t.Fatal(err)
	}
	stored, err := svc.repo.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	var result model.Result
	if err := db.First(&result, "task_id = ? AND kind = ?", task.ID, "generation_result").Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.TaskStatusCancelled || stored.ResultJSON != string(resultJSON) || result.Payload != string(resultJSON) {
		t.Fatalf("cancelled result = task:%#v result:%#v", stored, result)
	}
	if stored.InputJSON != originalInputJSON || task.InputJSON != originalInputJSON {
		t.Fatalf("signed task input changed on cancellation: stored=%q task=%q", stored.InputJSON, task.InputJSON)
	}
	var order model.BillingOrder
	if err := db.First(&order, "id = ?", task.BillingOrderID).Error; err != nil {
		t.Fatal(err)
	}
	if order.Status != model.BillingStatusUncertain || order.Error != "费用待核对" || order.NextReconcileAt == nil {
		t.Fatalf("cancelled billing = %#v", order)
	}
}

func TestCancelledProviderTaskRemainsClaimableForResultReconciliation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Minute)
	task := model.Task{ID: "cancel-reconcile", UserID: "user", Status: model.TaskStatusCancelled, ProviderRequestID: "provider-task", PollStage: "cancel_reconcile", NextPollAt: &past}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	claimed, err := repo.ClaimNextTask("reconcile-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.ID != task.ID || claimed.Status != model.TaskStatusCancelled || claimed.LeaseOwner != "reconcile-worker" {
		t.Fatalf("cancel reconciliation claim = %#v", claimed)
	}
	if err := repo.RenewTaskLease(task.ID, "reconcile-worker", time.Minute); err != nil {
		t.Fatal(err)
	}
}
