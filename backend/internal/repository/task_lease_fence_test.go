package repository

import (
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
)

func TestLeaseGenerationRejectsStaleWorkerWithReusedOwner(t *testing.T) {
	db := openProviderRepositorySQLite(t)
	repo := New(db)
	now := time.Now().UTC()
	task := model.Task{
		ID: "lease-generation-task", UserID: "lease-user", Audience: model.TaskAudienceCustomer,
		ExecutionKind: model.TaskExecutionProvider, Status: model.TaskStatusQueued,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	first, err := repo.ClaimNextTask("reused-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || first.LeaseGeneration == 0 || first.LeaseToken == "" {
		t.Fatalf("first lease facts = %#v", first)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Update("lease_expires_at", now.Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	second, err := repo.ClaimNextTask("reused-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if second == nil || second.LeaseGeneration <= first.LeaseGeneration || second.LeaseToken == first.LeaseToken {
		t.Fatalf("reclaimed lease facts = %#v, first = %#v", second, first)
	}

	if err := repo.RenewTaskLease(first.ID, first.LeaseOwner, first.LeaseGeneration, first.LeaseToken, time.Minute); err == nil {
		t.Fatal("stale heartbeat error = nil")
	}
	if err := repo.UpdateTaskProgress(first.ID, first.LeaseOwner, first.LeaseGeneration, first.LeaseToken, "stale progress", 99); err == nil {
		t.Fatal("stale progress error = nil")
	}
	staleCompletion := *first
	staleCompletion.Status = model.TaskStatusSucceeded
	staleCompletion.Stage = "stale completion"
	staleCompletion.Progress = 100
	if err := repo.SaveTaskCompletion(&staleCompletion, nil, nil, nil); !errors.Is(err, ErrTaskCompletionStateConflict) {
		t.Fatalf("stale completion error = %v", err)
	}
	if err := repo.RenewTaskLease(second.ID, second.LeaseOwner, second.LeaseGeneration, second.LeaseToken, time.Minute); err != nil {
		t.Fatalf("current heartbeat error = %v", err)
	}
	stored, err := repo.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Stage == "stale progress" || stored.Status != model.TaskStatusRunning {
		t.Fatalf("stored task accepted stale write: %#v", stored)
	}
}

func TestTaskFinalizersRejectMissingLeaseIdentity(t *testing.T) {
	db := openProviderRepositorySQLite(t)
	repo := New(db)
	now := time.Now().UTC()
	running := model.Task{
		ID: "missing-running-lease", UserID: "lease-user", Audience: model.TaskAudienceCustomer,
		ExecutionKind: model.TaskExecutionProvider, Status: model.TaskStatusRunning,
		CreatedAt: now, UpdatedAt: now,
	}
	cancelledAt := now.Add(time.Second)
	cancelled := model.Task{
		ID: "missing-cancelled-lease", UserID: "lease-user", Audience: model.TaskAudienceCustomer,
		ExecutionKind: model.TaskExecutionProvider, Status: model.TaskStatusCancelled,
		CancelRequestedAt: &cancelledAt, CancelReasonCode: "user_requested",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&running).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&cancelled).Error; err != nil {
		t.Fatal(err)
	}

	completion := running
	completion.Status = model.TaskStatusSucceeded
	if err := repo.SaveTaskCompletion(&completion, nil, nil, nil); !errors.Is(err, ErrTaskCompletionStateConflict) {
		t.Fatalf("completion without lease error = %v", err)
	}
	failure := running
	failure.Stage = "failed without lease"
	if err := repo.FinalizeFailedTaskAndBilling(&failure, FailedTaskBillingRefund, failure.Stage); !errors.Is(err, ErrTaskCompletionStateConflict) {
		t.Fatalf("failure without lease error = %v", err)
	}
	result := model.Result{ID: "missing-lease-result", UserID: cancelled.UserID, TaskID: cancelled.ID, Kind: "generation_result", Payload: `{}`}
	if err := repo.SaveCancelledTaskResult(&cancelled, result, "late success"); !errors.Is(err, ErrTaskCompletionStateConflict) {
		t.Fatalf("cancelled result without lease error = %v", err)
	}
}

func TestCancellationIntentSurvivesRestartAndInvalidatesClaim(t *testing.T) {
	db := openProviderRepositorySQLite(t)
	now := time.Now().UTC()
	task := model.Task{
		ID: "cancel-before-claim", UserID: "cancel-user", Audience: model.TaskAudienceCustomer,
		ExecutionKind: model.TaskExecutionProvider, Status: model.TaskStatusQueued,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	repo := New(db)
	cancelled, err := repo.CancelTaskIfStatus(task.UserID, task.ID, model.TaskStatusQueued, "user_requested", now)
	if err != nil || !cancelled {
		t.Fatalf("cancelled = %v, error = %v", cancelled, err)
	}

	// A new repository represents a restarted worker process. The database fact,
	// not an in-memory context, must keep the task from being claimed.
	restarted := New(db)
	claimed, err := restarted.ClaimNextTask("restart-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claimed != nil {
		t.Fatalf("cancelled task was reclaimed after restart: %#v", claimed)
	}
	stored, err := restarted.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CancelRequestedAt == nil || !stored.CancelRequestedAt.Equal(now) || stored.CancelReasonCode != "user_requested" {
		t.Fatalf("durable cancellation facts = %#v", stored)
	}
}

func TestCancelledProviderLateSuccessRequiresReconciliationLease(t *testing.T) {
	db := openProviderRepositorySQLite(t)
	repo := New(db)
	now := time.Now().UTC()
	task := model.Task{
		ID: "cancelled-provider-late-success", UserID: "cancel-provider-user", Audience: model.TaskAudienceCustomer,
		ExecutionKind: model.TaskExecutionProvider, Status: model.TaskStatusQueued,
		ProviderRequestID: "provider-task", PollStage: "accepted", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	oldClaim, err := repo.ClaimNextTask("worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := repo.CancelTaskIfStatus(task.UserID, task.ID, model.TaskStatusRunning, "user_requested", now)
	if err != nil || !cancelled {
		t.Fatalf("cancelled = %v, error = %v", cancelled, err)
	}

	stale := *oldClaim
	stale.ResultJSON = `{"url":"https://example.invalid/stale.png"}`
	result := model.Result{ID: "stale-result", UserID: task.UserID, TaskID: task.ID, Kind: "generation_result", Payload: stale.ResultJSON}
	if err := repo.SaveCancelledTaskResult(&stale, result, "late success"); err == nil {
		t.Fatal("old provider worker saved a late result without a reconciliation lease")
	}

	reconciler, err := repo.ClaimCancelledTaskResult(task.ID, task.UserID, oldClaim.LeaseGeneration, oldClaim.LeaseOwner, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if reconciler == nil || reconciler.Status != model.TaskStatusCancelled || reconciler.LeaseGeneration <= oldClaim.LeaseGeneration+1 || reconciler.LeaseToken == oldClaim.LeaseToken {
		t.Fatalf("reconciliation claim = %#v", reconciler)
	}
	reconciler.ResultJSON = `{"url":"https://example.invalid/preserved.png"}`
	result.ID = "preserved-result"
	result.Payload = reconciler.ResultJSON
	if err := repo.SaveCancelledTaskResult(reconciler, result, "late success"); err != nil {
		t.Fatalf("reconciliation save error = %v", err)
	}
}
