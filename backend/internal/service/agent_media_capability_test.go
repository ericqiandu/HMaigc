package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

func TestAgentRuntimeFreezesMediaQuoteBeforeApproval(t *testing.T) {
	decision := agentRuntimeToolDecisionWithDelivery(t,
		`{"kind":"tool_call","toolCall":{"toolCallId":"media-runtime-quote","toolName":"media.generate","actionVersion":1,"arguments":{"mediaKind":"image","modelRecordId":"runtime-image-model","modelKey":"kz_gpt_image2","parameters":{"prompt":"鲜橙产品特写","aspectRatio":"1:1","resolution":"1K","quality":"medium","count":1},"sourceResourceIds":[],"targetCanvasNodeId":"image-node-1","clientRequestId":"runtime-image-request"}}}`,
		agentRuntimeTestImageDelivery(),
	)
	server, _ := newAgentRuntimeDecisionServer(t, decision, agentRuntimeTestImageDelivery())
	defer server.Close()
	svc, db, fixture := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeImageModel(t, db, fixture)
	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "runtime-media-quote", UserMessage: "生成一张鲜橙产品图", MaxSteps: 4,
		Configuration: guidedAgentRuntimeConfigurationInput(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.ModelTask == nil {
		t.Fatal("initial model task was not created")
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	waiting, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.State.Status != agentruntime.RunWaitingApproval || waiting.State.PendingToolCall == nil {
		t.Fatalf("media approval state = %#v", waiting.State)
	}
	record, err := svc.repo.AgentToolCallForScope(scope, "media-runtime-quote", 1)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := agentruntime.DecodeApprovalProposal(json.RawMessage(record.ApprovalProposalJSON))
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Quote == nil || proposal.Quote.ModelRecordID != "runtime-image-model" ||
		proposal.Quote.ModelKey != "kz_gpt_image2" || proposal.Quote.PriceVersion != 1 || proposal.Quote.AmountMicrocredits != 250 {
		t.Fatalf("runtime media approval quote = %#v", proposal.Quote)
	}
	var taskCount, orderCount int64
	if err := db.Model(&model.Task{}).Where("operation = ?", agentMediaGenerationOperationForRun(scope.RunID)).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("scene = ?", agentMediaGenerationOperationForRun(scope.RunID)).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 || orderCount != 0 {
		t.Fatalf("approval proposal created commercial side effects: tasks=%d orders=%d", taskCount, orderCount)
	}
}

func TestAgentRuntimeRejectsInvalidMediaQuoteAndSchedulesRepair(t *testing.T) {
	decision := agentRuntimeToolDecisionWithDelivery(t,
		`{"kind":"tool_call","toolCall":{"toolCallId":"media-runtime-invalid-quote","toolName":"media.generate","actionVersion":1,"arguments":{"mediaKind":"image","modelRecordId":"runtime-image-model","modelKey":"kz_gpt_image2","parameters":{"prompt":"鲜橙产品特写","aspectRatio":"1:1","resolution":"1K","quality":"standard","count":1,"transparentBackground":false},"sourceResourceIds":[],"targetCanvasNodeId":"image-node-1","clientRequestId":"runtime-image-invalid-quote"}}}`,
		agentRuntimeTestImageDelivery(),
	)
	server, _ := newAgentRuntimeDecisionServer(t, decision, agentRuntimeTestImageDelivery())
	defer server.Close()
	svc, db, fixture := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeImageModel(t, db, fixture)
	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "runtime-media-invalid-quote", UserMessage: "生成一张鲜橙产品图", MaxSteps: 4,
		Configuration: guidedAgentRuntimeConfigurationInput(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatalf("invalid media decision must become repair feedback instead of a poison outbox: %v", err)
	}
	progress, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Status != agentruntime.RunRunning || progress.State.StepNumber != 1 || progress.State.DecisionFeedback == nil ||
		progress.State.DecisionFeedback.Code != "model_decision_invalid" || progress.ModelTask == nil || progress.ModelTask.ID == started.ModelTask.ID {
		t.Fatalf("invalid media decision repair state = %#v; task=%#v", progress.State, progress.ModelTask)
	}
	var toolCallCount int64
	if err := db.Model(&model.AgentToolCall{}).Where("run_id = ?", scope.RunID).Count(&toolCallCount).Error; err != nil {
		t.Fatal(err)
	}
	if toolCallCount != 0 {
		t.Fatalf("invalid media decision persisted %d approval tool calls", toolCallCount)
	}
}

func TestAgentMediaCapabilityTerminalOutboxResumesWaitingRuntimeOnce(t *testing.T) {
	decision := agentRuntimeToolDecisionWithDelivery(t,
		`{"kind":"tool_call","toolCall":{"toolCallId":"media-runtime-outbox","toolName":"media.generate","actionVersion":1,"arguments":{"mediaKind":"image","modelRecordId":"runtime-image-model","modelKey":"kz_gpt_image2","parameters":{"prompt":"鲜橙产品特写","aspectRatio":"1:1","resolution":"1K","quality":"medium","count":1},"sourceResourceIds":[],"targetCanvasNodeId":"image-node-1","clientRequestId":"runtime-image-outbox"}}}`,
		agentRuntimeTestImageDelivery(),
	)
	server, _ := newAgentRuntimeDecisionServer(t, decision, agentRuntimeTestImageDelivery())
	defer server.Close()
	svc, db, fixture := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeImageModel(t, db, fixture)
	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "runtime-media-outbox", UserMessage: "生成一张鲜橙产品图", MaxSteps: 4,
		Configuration: guidedAgentRuntimeConfigurationInput(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.ModelTask == nil {
		t.Fatal("initial model task was not created")
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	waiting, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	record, err := svc.repo.AgentToolCallForScope(scope, "media-runtime-outbox", 1)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := svc.SubmitAgentToolApproval(scope, AgentToolApprovalSubmission{
		ToolCallID: "media-runtime-outbox", ActionVersion: 1,
		Decision: agentruntime.ToolApprovalApproved, ProposalHash: record.ApprovalProposalHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if approved.State.Status != agentruntime.RunWaitingTool || !approved.State.PendingToolStarted {
		t.Fatalf("approved media runtime state = %#v", approved.State)
	}
	if waiting.State.StateVersion >= approved.State.StateVersion {
		t.Fatalf("approval did not advance runtime state: waiting=%d approved=%d", waiting.State.StateVersion, approved.State.StateVersion)
	}
	var task model.Task
	if err := db.Where("operation = ?", agentMediaGenerationOperationForRun(scope.RunID)).First(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := finalizeAgentMediaCapabilityTaskWithOutbox(t, svc, db, task, "generated-image-outbox", "image"); err != nil {
		t.Fatal(err)
	}
	if err := svc.dispatchTaskOutbox(time.Now().UTC().Add(time.Second), 10); err != nil {
		t.Fatal(err)
	}

	state, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != agentruntime.RunRunning || state.PendingToolCall != nil || state.PendingToolStarted ||
		state.LastToolResult == nil || !state.LastToolResult.Succeeded {
		t.Fatalf("terminal media outbox did not resolve the waiting runtime: %#v", state)
	}
	record, err = svc.repo.AgentToolCallForScope(scope, "media-runtime-outbox", 1)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != agentruntime.ToolCallSucceeded {
		t.Fatalf("media tool status after outbox = %q", record.Status)
	}
	var delivered int64
	if err := db.Model(&model.TaskOutbox{}).
		Where("task_id = ? AND status = ?", task.ID, model.TaskOutboxDelivered).
		Count(&delivered).Error; err != nil {
		t.Fatal(err)
	}
	if delivered != 1 {
		t.Fatalf("delivered terminal media outbox count = %d", delivered)
	}
}

func TestAgentMediaCapabilityFreezesAuthoritativeQuoteWithoutCommercialSideEffects(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeImageModel(t, db, fixture)
	call := agentMediaCapabilityCall("media-quote-1", "request-image-1")

	quote, err := svc.freezeAgentMediaCapabilityQuote(agentRuntimeServiceScope(), call, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if quote.ModelRecordID != "runtime-image-model" || quote.ModelKey != "kz_gpt_image2" ||
		quote.AmountMicrocredits != 250 || quote.PriceVersion != 1 {
		t.Fatalf("authoritative media quote = %#v", quote)
	}
	var taskCount, orderCount, reserveCount int64
	if err := db.Model(&model.Task{}).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).Where("type = ?", model.CreditLedgerReserve).Count(&reserveCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 || orderCount != 0 || reserveCount != 0 {
		t.Fatalf("quote created commercial side effects: tasks=%d orders=%d reserves=%d", taskCount, orderCount, reserveCount)
	}
}

func TestAgentMediaCapabilityApprovedRetriesCreateOneTaskAndReservation(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeImageModel(t, db, fixture)
	call := agentMediaCapabilityCall("media-generate-1", "request-image-1")
	seedApprovedAgentCapabilityProposal(t, svc, db, call)
	if err := db.Model(&model.AgentToolCall{}).
		Where("run_id = ? AND tool_call_id = ? AND action_version = ?", agentRuntimeServiceScope().RunID, call.ToolCallID, call.ActionVersion).
		Update("status", agentruntime.ToolCallRunning).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}

	first, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
	if err != nil {
		t.Fatal(err)
	}
	second, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Pending || !second.Pending || len(first.Output) != 0 || len(second.Output) != 0 {
		t.Fatalf("queued media execution returned a final receipt: first=%#v second=%#v", first, second)
	}
	var task model.Task
	if err := db.Where("operation = ?", agentMediaGenerationOperationForRun(agentRuntimeServiceScope().RunID)).First(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := finalizeAgentMediaCapabilityTask(t, svc, db, task, "generated-image-1", "image"); err != nil {
		t.Fatal(err)
	}
	completed, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Pending || len(completed.Output) == 0 {
		t.Fatalf("completed media execution did not return a final receipt: %#v", completed)
	}
	decoded, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolMediaGenerate, completed.Output)
	if err != nil {
		t.Fatal(err)
	}
	receipt := decoded.(agentruntime.MediaGenerateResult)
	if receipt.TaskID != task.ID || receipt.BillingOrderID != task.BillingOrderID || receipt.MediaKind != agentruntime.MediaKindImage ||
		receipt.ClientRequestID != "request-image-1" || len(receipt.Resources) != 1 ||
		receipt.Resources[0].ResourceID != "generated-image-1" || receipt.Resources[0].Kind != agentruntime.MediaKindImage ||
		receipt.Resources[0].URL != "/api/resources/generated-image-1/file" {
		t.Fatalf("media receipt = %#v", receipt)
	}
	var taskCount, orderCount, reserveCount int64
	if err := db.Model(&model.Task{}).Where("id = ?", receipt.TaskID).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("task_id = ?", receipt.TaskID).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).
		Where("billing_order_id IN (?) AND type = ?", db.Model(&model.BillingOrder{}).Select("id").Where("task_id = ?", receipt.TaskID), model.CreditLedgerReserve).
		Count(&reserveCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 1 || orderCount != 1 || reserveCount != 1 {
		t.Fatalf("media facts tasks=%d orders=%d reserves=%d", taskCount, orderCount, reserveCount)
	}
}

func TestAgentMediaCapabilityDeliveryRejectsReceiptResourceOutsideTaskResult(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeImageModel(t, db, fixture)
	call := agentMediaCapabilityCall("media-delivery-binding", "request-delivery-binding")
	seedApprovedAgentCapabilityProposal(t, svc, db, call)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
	if err != nil || !queued.Pending {
		t.Fatalf("queued media delivery capability = %#v, err = %v", queued, err)
	}
	task := loadAgentMediaCapabilityTask(t, db)
	if err := finalizeAgentMediaCapabilityTask(t, svc, db, task, "generated-delivery-image", "image"); err != nil {
		t.Fatal(err)
	}
	completed, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
	if err != nil {
		t.Fatal(err)
	}
	if result := db.Model(&model.AgentToolCall{}).
		Where("run_id = ? AND tool_call_id = ? AND action_version = ?", agentRuntimeServiceScope().RunID, call.ToolCallID, call.ActionVersion).
		Updates(model.AgentToolCall{Status: agentruntime.ToolCallSucceeded, OutputJSON: string(completed.Output), UpdatedAt: time.Now().UTC()}); result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("persist media tool receipt: rows=%d err=%v", result.RowsAffected, result.Error)
	}
	evidence, err := svc.agentRuntimeDeliveryEvidence(agentRuntimeServiceScope(), "")
	if err != nil || len(evidence.Artifacts) != 1 || evidence.Artifacts[0].ResourceID != "generated-delivery-image" {
		t.Fatalf("authoritative media delivery evidence = %#v, err = %v", evidence, err)
	}

	seedAgentCapabilityResource(t, db, "unrelated-ready-image", "runtime-user", "")
	decoded, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolMediaGenerate, completed.Output)
	if err != nil {
		t.Fatal(err)
	}
	tampered := decoded.(agentruntime.MediaGenerateResult)
	tampered.Resources[0].ResourceID = "unrelated-ready-image"
	tampered.Resources[0].URL = "/api/resources/unrelated-ready-image/file"
	tamperedOutput, err := json.Marshal(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentToolCall{}).
		Where("run_id = ? AND tool_call_id = ? AND action_version = ?", agentRuntimeServiceScope().RunID, call.ToolCallID, call.ActionVersion).
		Update("output_json", string(tamperedOutput)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.agentRuntimeDeliveryEvidence(agentRuntimeServiceScope(), ""); err == nil {
		t.Fatal("media delivery accepted a ready resource that was absent from the authoritative task result")
	}
}

func TestAgentMediaCapabilityPersistsIndependentReceiptsForEveryMediaKind(t *testing.T) {
	tests := []struct {
		name        string
		kind        agentruntime.MediaKind
		modelID     string
		modelKey    string
		parameters  json.RawMessage
		resourceID  string
		createModel func(*testing.T, *gorm.DB, agentRuntimeServiceFixture)
	}{
		{
			name: "image", kind: agentruntime.MediaKindImage, modelID: "runtime-image-model", modelKey: "kz_gpt_image2",
			parameters: json.RawMessage(`{"prompt":"鲜橙产品特写","aspectRatio":"1:1","resolution":"1K","quality":"medium","count":1}`),
			resourceID: "generated-image", createModel: createAgentRuntimeImageModel,
		},
		{
			name: "video", kind: agentruntime.MediaKindVideo, modelID: "runtime-video-model", modelKey: "doubao-seedance-2-5-260628",
			parameters: json.RawMessage(`{"prompt":"鲜橙缓慢旋转的产品镜头","aspectRatio":"16:9","resolution":"720p","durationSeconds":5,"generateAudio":false}`),
			resourceID: "generated-video", createModel: createAgentRuntimeVideoModel,
		},
		{
			name: "audio", kind: agentruntime.MediaKindAudio, modelID: "runtime-audio-model", modelKey: "speech-2.8-hd",
			parameters: json.RawMessage(`{"prompt":"新鲜，从这一刻开始。","voice":"female-shaonv"}`),
			resourceID: "generated-audio", createModel: createAgentRuntimeAudioModel,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
			test.createModel(t, db, fixture)
			call := agentMediaCapabilityCallFor("media-"+test.name, "request-"+test.name, test.kind, test.modelID, test.modelKey, test.parameters, []string{})
			seedApprovedAgentCapabilityProposal(t, svc, db, call)
			registry, err := newAgentCapabilityRegistry(svc)
			if err != nil {
				t.Fatal(err)
			}
			queued, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
			if err != nil || !queued.Pending {
				t.Fatalf("queued %s capability = %#v, err = %v", test.kind, queued, err)
			}
			var task model.Task
			if err := db.Where("operation = ?", agentMediaGenerationOperationForRun(agentRuntimeServiceScope().RunID)).First(&task).Error; err != nil {
				t.Fatal(err)
			}
			if task.Capability != string(test.kind) || task.Type != "canvas_"+string(test.kind) {
				t.Fatalf("%s task identity = type %q capability %q", test.kind, task.Type, task.Capability)
			}
			if err := finalizeAgentMediaCapabilityTask(t, svc, db, task, test.resourceID, string(test.kind)); err != nil {
				t.Fatal(err)
			}
			completed, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolMediaGenerate, completed.Output)
			if err != nil {
				t.Fatal(err)
			}
			receipt := decoded.(agentruntime.MediaGenerateResult)
			if receipt.MediaKind != test.kind || len(receipt.Resources) != 1 || receipt.Resources[0].ResourceID != test.resourceID || receipt.Resources[0].Kind != test.kind {
				t.Fatalf("%s receipt = %#v", test.kind, receipt)
			}
			var taskCount, orderCount, reserveCount, consumeCount int64
			if err := db.Model(&model.Task{}).Where("operation = ?", task.Operation).Count(&taskCount).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.BillingOrder{}).Where("task_id = ?", task.ID).Count(&orderCount).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ? AND type = ?", task.BillingOrderID, model.CreditLedgerReserve).Count(&reserveCount).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ? AND type = ?", task.BillingOrderID, model.CreditLedgerConsume).Count(&consumeCount).Error; err != nil {
				t.Fatal(err)
			}
			if taskCount != 1 || orderCount != 1 || reserveCount != 1 || consumeCount != 1 {
				t.Fatalf("%s commercial facts tasks=%d orders=%d reserves=%d consumes=%d", test.kind, taskCount, orderCount, reserveCount, consumeCount)
			}
		})
	}
}

func TestAgentMediaCapabilityFailureRefundsExactlyOnce(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeImageModel(t, db, fixture)
	call := agentMediaCapabilityCall("media-refund", "request-refund")
	seedApprovedAgentCapabilityProposal(t, svc, db, call)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
	if err != nil || !queued.Pending {
		t.Fatalf("queued failed-media capability = %#v, err = %v", queued, err)
	}
	task := loadAgentMediaCapabilityTask(t, db)
	task = leaseAgentMediaCapabilityTask(t, db, task)
	task.Stage = "生成失败"
	task.Progress = 40
	task.Error = "provider rejected the request"
	if err := svc.repo.FinalizeFailedTaskAndBilling(&task, repository.FailedTaskBillingRefund, task.Error); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call); !agentCapabilityErrorCode(err, "media_generation_failed") {
			t.Fatalf("terminal failure attempt %d error = %v", attempt+1, err)
		}
	}
	assertAgentMediaCommercialCounts(t, db, task, 1, 1, 1, 0, 1)
}

func TestAgentMediaCapabilityProviderTimeoutRefundsExactlyOnce(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeImageModel(t, db, fixture)
	call := agentMediaCapabilityCall("media-timeout", "request-timeout")
	seedApprovedAgentCapabilityProposal(t, svc, db, call)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
	if err != nil || !queued.Pending {
		t.Fatalf("queued timed-out media capability = %#v, err = %v", queued, err)
	}
	task := loadAgentMediaCapabilityTask(t, db)
	task = leaseAgentMediaCapabilityTask(t, db, task)
	task.Stage = "生成超时"
	task.Progress = 70
	task.Error = "provider polling timed out"
	if err := svc.repo.FinalizeFailedTaskAndBilling(&task, repository.FailedTaskBillingRefund, task.Error); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call); !agentCapabilityErrorCode(err, "media_generation_failed") {
			t.Fatalf("timed-out media attempt %d error = %v", attempt+1, err)
		}
	}
	assertAgentMediaCommercialCounts(t, db, task, 1, 1, 1, 0, 1)
}

func TestAgentMediaCapabilityCancelledBeforeProviderCallReturnsRefundedFailure(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeImageModel(t, db, fixture)
	call := agentMediaCapabilityCall("media-cancelled", "request-cancelled")
	seedApprovedAgentCapabilityProposal(t, svc, db, call)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
	if err != nil || !queued.Pending {
		t.Fatalf("queued cancellable media capability = %#v, err = %v", queued, err)
	}
	task := loadAgentMediaCapabilityTask(t, db)
	cancelledAt := time.Now().UTC()
	cancelled, err := svc.repo.CancelTaskIfStatus(task.UserID, task.ID, model.TaskStatusQueued, "agent_run_cancelled", cancelledAt)
	if err != nil || !cancelled {
		t.Fatalf("cancel queued media task = %t, err = %v", cancelled, err)
	}
	if err := svc.RefundBilling(task.BillingOrderID, "agent run cancelled before provider call"); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call); !agentCapabilityErrorCode(err, "media_generation_cancelled") {
			t.Fatalf("cancelled media attempt %d error = %v", attempt+1, err)
		}
	}
	assertAgentMediaCommercialCounts(t, db, task, 1, 1, 1, 0, 1)
}

func TestAgentMediaCapabilitySuccessfulGenerationWithUncertainBillingIsNotDeliveredOrRefunded(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeImageModel(t, db, fixture)
	call := agentMediaCapabilityCall("media-uncertain", "request-uncertain")
	seedApprovedAgentCapabilityProposal(t, svc, db, call)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
	if err != nil || !queued.Pending {
		t.Fatalf("queued uncertain-media capability = %#v, err = %v", queued, err)
	}
	task := loadAgentMediaCapabilityTask(t, db)
	if err := finalizeAgentMediaCapabilityTaskWithBillingAction(t, svc, db, task, "uncertain-image", "image", repository.CompletedTaskBillingUncertain); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call); !agentCapabilityErrorCode(err, "media_settlement_uncertain") {
			t.Fatalf("uncertain settlement attempt %d error = %v", attempt+1, err)
		}
	}
	assertAgentMediaCommercialCounts(t, db, task, 1, 1, 1, 0, 0)
	var order model.BillingOrder
	if err := db.First(&order, "id = ?", task.BillingOrderID).Error; err != nil {
		t.Fatal(err)
	}
	if order.Status != model.BillingStatusUncertain {
		t.Fatalf("uncertain media billing status = %q", order.Status)
	}
}

func TestAgentMediaCapabilityRejectsForeignSourceBeforeApprovalOrBilling(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeImageModel(t, db, fixture)
	seedAgentCapabilityResource(t, db, "foreign-source", "other-user", "")
	call := agentMediaCapabilityCallFor(
		"media-foreign-source", "request-foreign-source", agentruntime.MediaKindImage,
		"runtime-image-model", "kz_gpt_image2",
		json.RawMessage(`{"prompt":"使用参考图生成产品图","aspectRatio":"1:1","resolution":"1K","quality":"medium","count":1}`),
		[]string{"foreign-source"},
	)
	if _, err := svc.freezeAgentMediaCapabilityQuote(agentRuntimeServiceScope(), call, time.Now().UTC()); !errors.Is(err, errAgentMediaInputChanged) {
		t.Fatalf("foreign source quote error = %v", err)
	}
	var taskCount, orderCount, reserveCount int64
	if err := db.Model(&model.Task{}).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).Where("type = ?", model.CreditLedgerReserve).Count(&reserveCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 || orderCount != 0 || reserveCount != 0 {
		t.Fatalf("foreign source created side effects: tasks=%d orders=%d reserves=%d", taskCount, orderCount, reserveCount)
	}
}

func TestAgentMediaCapabilityUsesReadyResourceBoundToStandaloneCanvas(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeVideoModel(t, db, fixture)
	seedAgentCapabilityResource(t, db, "standalone-source", "runtime-user", "")
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", "runtime-canvas").
		Select("project_id", "payload_json").
		Updates(model.CanvasProject{
			ProjectID:   "",
			PayloadJSON: `{"nodes":[{"id":"source-image","type":"image","title":"参考图","position":{"x":0,"y":0},"width":480,"height":270,"metadata":{"status":"success","content":"/api/resources/standalone-source/file","storageKey":"resource:standalone-source"}},{"id":"video-node-1","type":"video","title":"成片","position":{"x":520,"y":0},"width":480,"height":270,"metadata":{"status":"loading"}}],"connections":[]}`,
		}).Error; err != nil {
		t.Fatal(err)
	}
	scope := agentRuntimeServiceScope()
	scope.DomainProjectID = ""
	call := agentMediaCapabilityCallFor(
		"media-standalone-source", "request-standalone-source", agentruntime.MediaKindVideo,
		"runtime-video-model", "doubao-seedance-2-5-260628",
		json.RawMessage(`{"prompt":"纸船被风轻轻吹动","aspectRatio":"16:9","resolution":"720p","durationSeconds":5,"generateAudio":false}`),
		[]string{"standalone-source"},
	)

	quote, err := svc.freezeAgentMediaCapabilityQuote(scope, call, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if quote.ModelRecordID != "runtime-video-model" || quote.ModelKey != "doubao-seedance-2-5-260628" || quote.AmountMicrocredits < 1 {
		t.Fatalf("standalone canvas media quote = %#v", quote)
	}
}

func TestAgentMediaCapabilityRejectsOwnedResourceNotBoundToStandaloneCanvas(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeVideoModel(t, db, fixture)
	seedAgentCapabilityResource(t, db, "unbound-standalone-source", "runtime-user", "")
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", "runtime-canvas").
		Select("project_id", "payload_json").
		Updates(model.CanvasProject{
			ProjectID:   "",
			PayloadJSON: `{"nodes":[{"id":"video-node-1","type":"video","title":"成片","position":{"x":0,"y":0},"width":480,"height":270,"metadata":{"status":"loading"}}],"connections":[]}`,
		}).Error; err != nil {
		t.Fatal(err)
	}
	scope := agentRuntimeServiceScope()
	scope.DomainProjectID = ""
	call := agentMediaCapabilityCallFor(
		"media-unbound-standalone-source", "request-unbound-standalone-source", agentruntime.MediaKindVideo,
		"runtime-video-model", "doubao-seedance-2-5-260628",
		json.RawMessage(`{"prompt":"纸船被风轻轻吹动","aspectRatio":"16:9","resolution":"720p","durationSeconds":5,"generateAudio":false}`),
		[]string{"unbound-standalone-source"},
	)

	if _, err := svc.freezeAgentMediaCapabilityQuote(scope, call, time.Now().UTC()); !errors.Is(err, errAgentMediaInputChanged) {
		t.Fatalf("unbound standalone source quote error = %v", err)
	}
}

func TestAgentMediaCapabilityApprovalCannotAuthorizeDifferentMediaArguments(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeImageModel(t, db, fixture)
	approved := agentMediaCapabilityCall("media-approval-isolation", "request-approved-image")
	seedApprovedAgentCapabilityProposal(t, svc, db, approved)
	changed := agentMediaCapabilityCall("media-approval-isolation", "request-different-image")
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), changed); !agentCapabilityErrorCode(err, "media_proposal_conflict") {
		t.Fatalf("changed media arguments error = %v", err)
	}
	var taskCount, orderCount int64
	if err := db.Model(&model.Task{}).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 || orderCount != 0 {
		t.Fatalf("changed approval created side effects: tasks=%d orders=%d", taskCount, orderCount)
	}
}

func TestAgentMediaCapabilityApprovalCannotAuthorizeDifferentMediaKind(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeImageModel(t, db, fixture)
	createAgentRuntimeVideoModel(t, db, fixture)
	approved := agentMediaCapabilityCall("media-kind-isolation", "request-approved-image")
	seedApprovedAgentCapabilityProposal(t, svc, db, approved)
	changed := agentMediaCapabilityCallFor(
		"media-kind-isolation", "request-different-video", agentruntime.MediaKindVideo,
		"runtime-video-model", "doubao-seedance-2-5-260628",
		json.RawMessage(`{"prompt":"鲜橙缓慢旋转的产品镜头","aspectRatio":"16:9","resolution":"720p","durationSeconds":5,"generateAudio":false}`),
		[]string{},
	)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), changed); !agentCapabilityErrorCode(err, "media_proposal_conflict") {
		t.Fatalf("cross-media approval error = %v", err)
	}
	var taskCount, orderCount, reserveCount int64
	if err := db.Model(&model.Task{}).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).Where("type = ?", model.CreditLedgerReserve).Count(&reserveCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 || orderCount != 0 || reserveCount != 0 {
		t.Fatalf("cross-media approval created side effects: tasks=%d orders=%d reserves=%d", taskCount, orderCount, reserveCount)
	}
}

func finalizeAgentMediaCapabilityTask(t *testing.T, svc *Service, db *gorm.DB, task model.Task, resourceID string, kind string) error {
	return finalizeAgentMediaCapabilityTaskWithBillingAction(t, svc, db, task, resourceID, kind, repository.CompletedTaskBillingSettle)
}

func finalizeAgentMediaCapabilityTaskWithBillingAction(
	t *testing.T,
	svc *Service,
	db *gorm.DB,
	task model.Task,
	resourceID string,
	kind string,
	billingAction repository.CompletedTaskBillingAction,
) error {
	return finalizeAgentMediaCapabilityTaskWithDraftAndBillingAction(t, svc, db, task, resourceID, kind, nil, billingAction)
}

func finalizeAgentMediaCapabilityTaskWithOutbox(t *testing.T, svc *Service, db *gorm.DB, task model.Task, resourceID string, kind string) error {
	t.Helper()
	outbox, err := taskAgentRunOutboxDraft(task, time.Now().UTC().Add(time.Second))
	if err != nil {
		return err
	}
	return finalizeAgentMediaCapabilityTaskWithDraftAndBillingAction(t, svc, db, task, resourceID, kind, outbox, repository.CompletedTaskBillingSettle)
}

func finalizeAgentMediaCapabilityTaskWithDraftAndBillingAction(
	t *testing.T,
	svc *Service,
	db *gorm.DB,
	task model.Task,
	resourceID string,
	kind string,
	outbox *repository.TaskOutboxDraft,
	billingAction repository.CompletedTaskBillingAction,
) error {
	t.Helper()
	now := time.Now().UTC()
	resultKey := ""
	mimeType := ""
	switch kind {
	case "image":
		resultKey, mimeType = "images", "image/png"
	case "video":
		resultKey, mimeType = "videos", "video/mp4"
	case "audio":
		resultKey, mimeType = "audios", "audio/mpeg"
	default:
		t.Fatalf("unsupported generated media kind %q", kind)
	}
	if err := db.Create(&model.Resource{
		ID: resourceID, UserID: task.UserID, Kind: kind, Status: model.ResourceStatusReady,
		Provider: "local", ObjectKey: "agent/" + resourceID, MimeType: mimeType, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		return err
	}
	task = leaseAgentMediaCapabilityTask(t, db, task)
	completedAt := now.Add(time.Second)
	task.Status = model.TaskStatusSucceeded
	task.Stage = "任务完成"
	task.Progress = 100
	task.ResultJSON = `{"` + resultKey + `":[{"resourceId":"` + resourceID + `"}]}`
	task.CompletedAt = &completedAt
	billingError := ""
	if billingAction == repository.CompletedTaskBillingUncertain {
		billingError = "provider charge requires reconciliation"
	}
	return svc.repo.FinalizeSucceededTaskAndBilling(repository.SucceededTaskFinalization{
		Task: &task, BillingAction: billingAction, BillingError: billingError, Outbox: outbox,
	})
}

func loadAgentMediaCapabilityTask(t *testing.T, db *gorm.DB) model.Task {
	t.Helper()
	var task model.Task
	if err := db.Where("operation = ?", agentMediaGenerationOperationForRun(agentRuntimeServiceScope().RunID)).First(&task).Error; err != nil {
		t.Fatal(err)
	}
	return task
}

func leaseAgentMediaCapabilityTask(t *testing.T, db *gorm.DB, task model.Task) model.Task {
	t.Helper()
	now := time.Now().UTC()
	leaseExpiry := now.Add(time.Minute)
	task.Status = model.TaskStatusRunning
	task.LeaseOwner = "agent-media-test-worker"
	task.LeaseExpiresAt = &leaseExpiry
	task.LeaseGeneration = 1
	task.LeaseToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	result := db.Model(&model.Task{}).
		Where("id = ? AND status = ?", task.ID, model.TaskStatusQueued).
		Select("status", "lease_owner", "lease_expires_at", "lease_generation", "lease_token", "updated_at").
		Updates(model.Task{
			Status: task.Status, LeaseOwner: task.LeaseOwner, LeaseExpiresAt: task.LeaseExpiresAt,
			LeaseGeneration: task.LeaseGeneration, LeaseToken: task.LeaseToken, UpdatedAt: now,
		})
	if result.Error != nil {
		t.Fatal(result.Error)
	}
	if result.RowsAffected != 1 {
		t.Fatalf("leased media task rows = %d", result.RowsAffected)
	}
	return task
}

func assertAgentMediaCommercialCounts(
	t *testing.T,
	db *gorm.DB,
	task model.Task,
	wantTasks int64,
	wantOrders int64,
	wantReserves int64,
	wantConsumes int64,
	wantRefunds int64,
) {
	t.Helper()
	var taskCount, orderCount, reserveCount, consumeCount, refundCount int64
	if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("task_id = ?", task.ID).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	for ledgerType, target := range map[model.CreditLedgerType]*int64{
		model.CreditLedgerReserve: &reserveCount,
		model.CreditLedgerConsume: &consumeCount,
		model.CreditLedgerRefund:  &refundCount,
	} {
		if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ? AND type = ?", task.BillingOrderID, ledgerType).Count(target).Error; err != nil {
			t.Fatal(err)
		}
	}
	if taskCount != wantTasks || orderCount != wantOrders || reserveCount != wantReserves || consumeCount != wantConsumes || refundCount != wantRefunds {
		t.Fatalf("commercial facts tasks=%d orders=%d reserves=%d consumes=%d refunds=%d", taskCount, orderCount, reserveCount, consumeCount, refundCount)
	}
}

func TestAgentMediaCapabilityRejectsChangedPriceAfterApproval(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeImageModel(t, db, fixture)
	call := agentMediaCapabilityCall("media-price-change", "request-image-price-change")
	seedApprovedAgentCapabilityProposal(t, svc, db, call)
	if err := db.Model(&model.ChannelModelPriceTier{}).
		Where("channel_model_id = ? AND resolution = ? AND input_variant = ?", "runtime-image-model", "1K", "medium").
		Update("unit_price_microcredits", int64(999)).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call); !agentCapabilityErrorCode(err, "media_quote_changed") {
		t.Fatalf("changed price error = %v", err)
	}
	var taskCount int64
	if err := db.Model(&model.Task{}).Where("operation = ?", agentMediaGenerationOperationForRun(agentRuntimeServiceScope().RunID)).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 {
		t.Fatalf("changed quote created %d media tasks", taskCount)
	}
}

func agentMediaCapabilityCall(toolCallID string, clientRequestID string) agentruntime.ToolCallDecision {
	return agentMediaCapabilityCallFor(
		toolCallID, clientRequestID, agentruntime.MediaKindImage, "runtime-image-model", "kz_gpt_image2",
		json.RawMessage(`{"prompt":"鲜橙产品特写","aspectRatio":"1:1","resolution":"1K","quality":"medium","count":1}`), []string{},
	)
}

func agentMediaCapabilityCallFor(
	toolCallID string,
	clientRequestID string,
	mediaKind agentruntime.MediaKind,
	modelRecordID string,
	modelKey string,
	parameters json.RawMessage,
	sourceResourceIDs []string,
) agentruntime.ToolCallDecision {
	arguments, err := json.Marshal(agentruntime.MediaGenerateArguments{
		MediaKind: mediaKind, ModelRecordID: modelRecordID, ModelKey: modelKey,
		Parameters: parameters, SourceResourceIDs: sourceResourceIDs,
		TargetCanvasNodeID: string(mediaKind) + "-node-1", ClientRequestID: clientRequestID,
	})
	if err != nil {
		panic(err)
	}
	return agentruntime.ToolCallDecision{ToolCallID: toolCallID, ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1, Arguments: arguments}
}

func createAgentRuntimeAudioModel(t *testing.T, db *gorm.DB, fixture agentRuntimeServiceFixture) {
	t.Helper()
	now := time.Now().UTC()
	channel := model.ModelChannel{
		ID: "runtime-audio-channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "Agent Audio",
		APIFormat: "openai", InterfaceType: model.ChannelInterfaceMiniMaxSpeech, ModelsJSON: `["speech-2.8-hd"]`,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	channelModel := model.ChannelModel{
		ID: "runtime-audio-model", ChannelID: channel.ID, ModelKey: "speech-2.8-hd", DisplayName: "MiniMax Speech",
		ProviderCredentialID: fixture.credential.ID, AccessPolicy: model.ModelAccessAuthenticated, Capability: "audio",
		BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 75,
		PriceConfigured: true, Enabled: true, PriceVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&channelModel).Error; err != nil {
		t.Fatal(err)
	}
	voice := model.ChannelVoice{
		ID: "runtime-audio-voice", ChannelID: channel.ID, VoiceKey: "female-shaonv", DisplayName: "少女音",
		Kind: "system", AccessPolicy: model.ModelAccessAuthenticated, CompatibleModelsJSON: `["speech-2.8-hd"]`,
		ProviderStatus: "active", Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&voice).Error; err != nil {
		t.Fatal(err)
	}
}

func seedApprovedAgentCapabilityProposal(t *testing.T, svc *Service, _ *gorm.DB, call agentruntime.ToolCallDecision) string {
	t.Helper()
	scope := agentRuntimeServiceScope()
	now := time.Now().UTC()
	configuration, err := svc.resolveAgentRuntimeConfiguration(context.Background(), scope, guidedAgentRuntimeConfigurationInput())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repo.CreateInitializedAgentRun(repository.CreateInitializedAgentRunInput{
		Create: repository.CreateAgentRunInput{Scope: scope, ClientRequestID: "agent-capability-run", Now: now},
		Initialize: repository.InitializeAgentRunInput{
			Scope: scope, ModelRecordID: "runtime-agent-model", ModelKey: "gpt-5.5", MaxSteps: 8,
			ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion, RuntimeVersion: agentruntime.CurrentRuntimeVersion,
			PolicyVersion: agentruntime.CurrentPolicyVersion, UserMessage: "执行能力", Configuration: configuration, Now: now,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := call.ExpectedDelivery.Validate(); err != nil {
		call.ExpectedDelivery = agentRuntimeTestAnswerDelivery()
	}
	queued, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if queued.Status == agentruntime.RunWaitingTool && queued.PendingToolCall != nil {
		prior, loadErr := svc.repo.AgentToolCallForScope(
			scope,
			queued.PendingToolCall.ToolCallID,
			queued.PendingToolCall.ActionVersion,
		)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if prior.Status != agentruntime.ToolCallSucceeded {
			t.Fatalf("previous capability is not resolved: status=%q", prior.Status)
		}
		resolved, resolveErr := agentruntime.ResolveTool(queued, agentruntime.ToolResolution{
			ToolCallID: prior.ToolCallID, ActionVersion: prior.ActionVersion,
			Succeeded: true, Output: json.RawMessage(prior.OutputJSON),
		})
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		if _, commitErr := svc.commitAgentRuntimeState(scope, queued, resolved); commitErr != nil {
			t.Fatal(commitErr)
		}
		queued = resolved.State
	}
	waitingApproval, err := agentruntime.AdvanceForToolSchema(
		queued,
		agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &call}},
		agentruntime.CurrentToolSchemaVersion,
	)
	if err != nil {
		t.Fatal(err)
	}
	waitingApproval, err = svc.prepareAgentRuntimeApproval(scope, queued, waitingApproval, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.commitAgentRuntimeState(scope, queued, waitingApproval); err != nil {
		t.Fatal(err)
	}
	record, err := svc.repo.AgentToolCallForScope(scope, call.ToolCallID, call.ActionVersion)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := agentruntime.ReviewToolApproval(waitingApproval.State, agentruntime.ToolApproval{
		ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion,
		Decision: agentruntime.ToolApprovalApproved, ProposalHash: record.ApprovalProposalHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.commitAgentRuntimeState(scope, waitingApproval.State, approved); err != nil {
		t.Fatal(err)
	}
	return record.ApprovalProposalHash
}

func resolveSuccessfulAgentCapabilityForTest(
	t *testing.T,
	svc *Service,
	call agentruntime.ToolCallDecision,
	result agentruntime.ToolExecutionResult,
) {
	t.Helper()
	if result.Pending {
		t.Fatal("cannot resolve a pending capability as successful")
	}
	scope := agentRuntimeServiceScope()
	current, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := agentruntime.ResolveTool(current, agentruntime.ToolResolution{
		ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion,
		Succeeded: true, Output: result.Output,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.commitAgentRuntimeState(scope, current, resolved); err != nil {
		t.Fatal(err)
	}
}
