package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

func TestProjectCompletedAgentMediaTaskResultAfterRunCancellation(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeImageModel(t, db, fixture)
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", "runtime-canvas").Update(
		"payload_json",
		`{"nodes":[{"id":"image-node-1","type":"image","title":"Agent image","position":{"x":0,"y":0},"width":320,"height":180,"metadata":{"prompt":"stale prompt","composerContent":"stale prompt","status":"loading"}}],"connections":[]}`,
	).Error; err != nil {
		t.Fatal(err)
	}
	call := agentMediaCapabilityCall("cancelled-run-binding", "cancelled-run-binding-request")
	seedApprovedAgentCapabilityProposal(t, svc, db, call)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
	if err != nil || !queued.Pending {
		t.Fatalf("queued media task = %#v, err = %v", queued, err)
	}
	task := loadAgentMediaCapabilityTask(t, db)
	if err := finalizeAgentMediaCapabilityTaskWithOutbox(t, svc, db, task, "cancelled-run-image", "image"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if result := db.Model(&model.AgentRun{}).Where("id = ?", agentRuntimeServiceScope().RunID).
		Select("status", "completed_at", "updated_at").
		Updates(model.AgentRun{Status: agentruntime.RunCancelled, CompletedAt: &now, UpdatedAt: now}); result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("cancel agent run: rows=%d err=%v", result.RowsAffected, result.Error)
	}
	if result := db.Model(&model.AgentToolCall{}).
		Where("run_id = ? AND tool_call_id = ? AND action_version = ?", agentRuntimeServiceScope().RunID, call.ToolCallID, call.ActionVersion).
		Select("status", "error_code", "updated_at").
		Updates(model.AgentToolCall{Status: agentruntime.ToolCallFailed, ErrorCode: "media_canvas_result_binding_failed", UpdatedAt: now}); result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("fail agent tool call: rows=%d err=%v", result.RowsAffected, result.Error)
	}
	var approvedCall model.AgentToolCall
	if err := db.First(&approvedCall,
		"run_id = ? AND tool_call_id = ? AND action_version = ?",
		agentRuntimeServiceScope().RunID, call.ToolCallID, call.ActionVersion,
	).Error; err != nil {
		t.Fatal(err)
	}
	unapprovedRetry := approvedCall
	unapprovedRetry.ID = "cancelled-run-unapproved-retry"
	unapprovedRetry.ToolCallID = "cancelled-run-unapproved-retry"
	unapprovedRetry.Status = agentruntime.ToolCallPending
	unapprovedRetry.ApprovalDecision = ""
	unapprovedRetry.ApprovalByUserID = ""
	unapprovedRetry.ApprovalDecidedAt = nil
	unapprovedRetry.ApprovalProposalJSON = ""
	unapprovedRetry.ApprovalProposalHash = ""
	unapprovedRetry.ApprovalExpiresAt = nil
	unapprovedRetry.IdempotencyKey = "cancelled-run-unapproved-retry"
	unapprovedRetry.CapabilityIdempotencyKey = ""
	unapprovedRetry.ErrorCode = ""
	unapprovedRetry.CreatedAt = now.Add(time.Second)
	unapprovedRetry.UpdatedAt = unapprovedRetry.CreatedAt
	if err := db.Create(&unapprovedRetry).Error; err != nil {
		t.Fatal(err)
	}
	completedTask, err := svc.repo.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	orderBefore, err := svc.repo.BillingOrder(completedTask.BillingOrderID)
	if err != nil {
		t.Fatal(err)
	}
	var ledgerCountBefore int64
	if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ?", orderBefore.ID).Count(&ledgerCountBefore).Error; err != nil {
		t.Fatal(err)
	}

	deliveryCount := 0
	if err := svc.dispatchTaskOutboxWithDelivery(now.Add(3*time.Second), 10, func(reference repository.ActiveAgentRunReference, taskID string, wakeup agentRunWakeup) error {
		deliveryCount++
		if reference.RunID != agentRuntimeServiceScope().RunID || reference.ActorUserID != agentRuntimeServiceScope().ActorUserID ||
			taskID != completedTask.ID || wakeup != agentWakeGenerationTaskFinished {
			t.Fatalf("unexpected outbox delivery: reference=%#v task=%q wakeup=%q", reference, taskID, wakeup)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if deliveryCount != 1 {
		t.Fatalf("Agent outbox delivery count = %d, want 1", deliveryCount)
	}
	canvas, err := svc.repo.CanvasProject(agentRuntimeServiceScope().CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Nodes []struct {
			ID       string `json:"id"`
			Metadata struct {
				Prompt          string `json:"prompt"`
				ComposerContent string `json:"composerContent"`
				Content         string `json:"content"`
				StorageKey      string `json:"storageKey"`
				Status          string `json:"status"`
				TaskID          string `json:"taskId"`
				TaskStatus      string `json:"taskStatus"`
			} `json:"metadata"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(canvas.PayloadJSON), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Nodes) != 1 || document.Nodes[0].ID != "image-node-1" ||
		document.Nodes[0].Metadata.Prompt != "鲜橙产品特写" ||
		document.Nodes[0].Metadata.ComposerContent != "鲜橙产品特写" ||
		document.Nodes[0].Metadata.Content != "/api/resources/cancelled-run-image/file" ||
		document.Nodes[0].Metadata.StorageKey != "resource:cancelled-run-image" ||
		document.Nodes[0].Metadata.Status != "success" || document.Nodes[0].Metadata.TaskID != task.ID ||
		document.Nodes[0].Metadata.TaskStatus != string(model.TaskStatusSucceeded) {
		t.Fatalf("completed media task was not projected to the canvas: revision=%d payload=%s", canvas.Revision, canvas.PayloadJSON)
	}
	orderAfter, err := svc.repo.BillingOrder(completedTask.BillingOrderID)
	if err != nil {
		t.Fatal(err)
	}
	var ledgerCountAfter int64
	if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ?", orderAfter.ID).Count(&ledgerCountAfter).Error; err != nil {
		t.Fatal(err)
	}
	if orderAfter.Status != model.BillingStatusSettled || orderAfter.UpdatedAt != orderBefore.UpdatedAt || ledgerCountAfter != ledgerCountBefore {
		t.Fatalf("canvas projection changed billing facts: before=%#v after=%#v ledgers=%d/%d", orderBefore, orderAfter, ledgerCountBefore, ledgerCountAfter)
	}
	revision := canvas.Revision
	if err := svc.projectCompletedAgentMediaTaskResult(completedTask); err != nil {
		t.Fatal(err)
	}
	replayed, err := svc.repo.CanvasProject(agentRuntimeServiceScope().CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Revision != revision || replayed.PayloadJSON != canvas.PayloadJSON {
		t.Fatalf("idempotent task projection changed canvas: before=%d after=%d", revision, replayed.Revision)
	}
}
