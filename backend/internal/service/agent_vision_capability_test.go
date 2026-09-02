package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

func TestAgentVisionTaskDirectSuccessSettlesAndWritesOutboxAtomically(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	var providerCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		providerCalls.Add(1)
		if request.URL.Path != "/v1/chat/completions" {
			t.Errorf("provider path = %q", request.URL.Path)
			http.NotFound(writer, request)
			return
		}
		writeAgentRuntimeChatStream(t, writer, "vision-direct-request-id", "画面主体是一只站在窗边的猫。", 520, 31, 20)
	}))
	defer server.Close()

	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://managed.example.com")
	directModel := configureDirectAgentVisionFixture(t, svc, db, server.URL)
	if err := db.Model(&model.CreditAccount{}).
		Where("user_id = ?", "runtime-user").
		Update("available_microcredits", int64(1_000_000_000)).Error; err != nil {
		t.Fatal(err)
	}
	resource := createLocalVisionTestResource(t, svc, db, model.Resource{
		ID: "vision-direct-resource", UserID: "runtime-user", Kind: "image",
		Status: model.ResourceStatusReady, MimeType: "image/png",
	}, encodeVisionTestImage(t, "png", 4, 3))
	createVisionTestProjectAsset(t, db, "runtime-project", resource)
	call := agentVisionCapabilityCall(
		"vision-direct-call", "vision-direct-client-request", directModel,
		[]string{resource.ID}, "描述图片中的主体", agentruntime.VisionDetailLow,
	)
	seedApprovedAgentCapabilityProposal(t, svc, db, call)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
	if err != nil || !queued.Pending {
		t.Fatalf("queued direct vision capability = %#v, err = %v", queued, err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	if providerCalls.Load() != 1 {
		t.Fatalf("provider calls = %d, want 1", providerCalls.Load())
	}

	var task model.Task
	if err := db.Where("type = ?", agentVisionTaskType).Take(&task).Error; err != nil {
		t.Fatal(err)
	}
	if task.Status != model.TaskStatusSucceeded || task.ProviderRequestID != "vision-direct-request-id" ||
		task.PollStage != agentVisionProviderDispatchStarted {
		t.Fatalf("completed direct vision task = %#v", task)
	}
	var result agentruntime.VisionAnalyzeResult
	if err := json.Unmarshal([]byte(task.ResultJSON), &result); err != nil {
		t.Fatal(err)
	}
	if result.TaskID != task.ID || result.BillingOrderID != task.BillingOrderID ||
		result.ModelRecordID != directModel.ID || result.ModelKey != directModel.ModelKey ||
		result.ClientRequestID != "vision-direct-client-request" || result.Analysis != "画面主体是一只站在窗边的猫。" ||
		result.Usage.InputTokens != 520 || result.Usage.CachedTokens != 20 || result.Usage.OutputTokens != 31 ||
		len(result.SourceResourceIDs) != 1 || result.SourceResourceIDs[0] != resource.ID {
		t.Fatalf("direct vision result = %#v", result)
	}
	order, err := svc.repo.BillingOrder(task.BillingOrderID)
	if err != nil {
		t.Fatal(err)
	}
	if order.Status != model.BillingStatusSettled || order.ProviderRequestID != task.ProviderRequestID ||
		order.InputTokens != 520 || order.CachedTokens != 20 || order.OutputTokens != 31 ||
		order.ProviderEndpointVersionID != "" || order.ProviderCredentialVersionID != "" {
		t.Fatalf("settled direct vision order = %#v", order)
	}
	var outboxCount int64
	if err := db.Model(&model.TaskOutbox{}).Where("task_id = ?", task.ID).Count(&outboxCount).Error; err != nil {
		t.Fatal(err)
	}
	if outboxCount != 1 {
		t.Fatalf("direct vision outbox count = %d, want 1", outboxCount)
	}
}

func TestAgentVisionTaskValidationFailureBeforeDispatchRefundsWithoutProviderCall(t *testing.T) {
	var providerCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		providerCalls.Add(1)
	}))
	defer server.Close()

	fixture := queueDirectAgentVisionTaskForTest(t, server.URL, "pre-send")
	if err := fixture.db.Delete(&model.ProjectAssetLink{}, "id = ?", fixture.projectAssetLinkID).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.ProcessNextTask(); err == nil {
		t.Fatal("expected authorization failure before provider dispatch")
	}
	if providerCalls.Load() != 0 {
		t.Fatalf("provider calls = %d, want 0", providerCalls.Load())
	}
	assertFailedAgentVisionTaskBilling(t, fixture, model.BillingStatusRefunded, "", "")
}

func TestAgentVisionTaskPostDispatchStreamFailureBecomesUncertain(t *testing.T) {
	var providerCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		providerCalls.Add(1)
		if request.URL.Path != "/v1/chat/completions" {
			t.Errorf("provider path = %q", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"id\":\"vision-truncated-request\",\"choices\":[{\"delta\":{\"content\":\"部分结果\"},\"finish_reason\":null}]}\n\n"))
	}))
	defer server.Close()

	fixture := queueDirectAgentVisionTaskForTest(t, server.URL, "post-send")
	if err := fixture.service.ProcessNextTask(); err == nil {
		t.Fatal("expected truncated provider stream failure")
	}
	if providerCalls.Load() != 1 {
		t.Fatalf("provider calls = %d, want 1", providerCalls.Load())
	}
	assertFailedAgentVisionTaskBilling(
		t, fixture, model.BillingStatusUncertain,
		agentVisionProviderDispatchStarted, "vision-truncated-request",
	)
	claimed, err := fixture.service.repo.ClaimKuaiziBillingReconciliations("vision-test", time.Now().UTC(), time.Minute, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 0 {
		t.Fatalf("direct vision uncertain orders entered Kuaizi reconciliation: %#v", claimed)
	}
}

func TestAgentVisionTaskRestartAfterDispatchNeverResendsProviderRequest(t *testing.T) {
	var providerCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		providerCalls.Add(1)
	}))
	defer server.Close()

	fixture := queueDirectAgentVisionTaskForTest(t, server.URL, "restart")
	claimed, err := fixture.service.repo.ClaimNextTask("crashed-vision-worker", taskLeaseDuration)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.ID != fixture.task.ID {
		t.Fatalf("claimed vision task = %#v", claimed)
	}
	if err := fixture.service.repo.BeginClaimedTokenProviderDispatch(claimed, agentVisionProviderDispatchStarted, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&model.Task{}).Where("id = ?", claimed.ID).
		Update("lease_expires_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}

	if err := fixture.service.ProcessNextTask(); !errors.Is(err, errAgentVisionDispatchAmbiguous) {
		t.Fatalf("restart recovery error = %v, want dispatch ambiguity", err)
	}
	if providerCalls.Load() != 0 {
		t.Fatalf("provider calls after recovery = %d, want 0", providerCalls.Load())
	}
	assertFailedAgentVisionTaskBilling(
		t, fixture, model.BillingStatusUncertain,
		agentVisionProviderDispatchStarted, "",
	)
}

func TestAgentVisionCapabilityRunsFromApprovalThroughOutboxAndAgentContinuation(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	var agentCalls atomic.Int64
	var visionCalls atomic.Int64
	var visionDecision string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/ai-open-platform-api/v1/chat/completions":
			callNumber := agentCalls.Add(1)
			if callNumber == 1 {
				writeAgentRuntimeChatStream(t, writer, "agent-vision-decision", visionDecision, 40, 20, 3)
				return
			}
			writeAgentRuntimeChatStream(t, writer, "agent-vision-final", `{"kind":"final","final":{"message":"图片主体是一只站在窗边的猫。","expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`, 35, 15, 2)
		case "/v1/chat/completions":
			visionCalls.Add(1)
			writeAgentRuntimeChatStream(t, writer, "vision-runtime-provider-request", "画面主体是一只站在窗边的猫。", 521, 32, 21)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	directModel := configureDirectAgentVisionFixture(t, svc, db, server.URL)
	if err := db.Model(&model.CreditAccount{}).
		Where("user_id = ?", "runtime-user").
		Update("available_microcredits", int64(1_000_000_000)).Error; err != nil {
		t.Fatal(err)
	}
	resource := createLocalVisionTestResource(t, svc, db, model.Resource{
		ID: "vision-runtime-flow-resource", UserID: "runtime-user", Kind: "image",
		Status: model.ResourceStatusReady, MimeType: "image/png",
	}, encodeVisionTestImage(t, "png", 4, 3))
	createVisionTestProjectAsset(t, db, "runtime-project", resource)
	call := agentVisionCapabilityCall(
		"vision-runtime-flow-call", "vision-runtime-flow-request", directModel,
		[]string{resource.ID}, "描述图片中的主体", agentruntime.VisionDetailLow,
	)
	call.ExpectedDelivery = agentRuntimeTestAnswerDelivery()
	decision, err := json.Marshal(agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &call})
	if err != nil {
		t.Fatal(err)
	}
	visionDecision = string(decision)

	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "vision-runtime-flow-start", UserMessage: "理解这张项目图片",
		MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.ModelTask == nil {
		t.Fatal("initial Agent model task was not created")
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	waiting, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.State.Status != agentruntime.RunWaitingApproval || waiting.State.PendingToolCall == nil {
		t.Fatalf("vision approval state = %#v", waiting.State)
	}
	record, err := svc.repo.AgentToolCallForScope(scope, call.ToolCallID, call.ActionVersion)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := svc.SubmitAgentToolApproval(scope, AgentToolApprovalSubmission{
		ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion,
		Decision: agentruntime.ToolApprovalApproved, ProposalHash: record.ApprovalProposalHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if approved.State.Status != agentruntime.RunWaitingTool || !approved.State.PendingToolStarted {
		t.Fatalf("approved vision state = %#v", approved.State)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	afterVision, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if afterVision.Status != agentruntime.RunRunning || afterVision.PendingToolCall != nil || afterVision.PendingToolStarted ||
		afterVision.LastToolResult == nil || !afterVision.LastToolResult.Succeeded {
		if afterVision.LastToolResult != nil {
			t.Fatalf("vision Outbox continuation state = %#v; result=%#v", afterVision, *afterVision.LastToolResult)
		}
		t.Fatalf("vision Outbox continuation state = %#v", afterVision)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	completed, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State.Status != agentruntime.RunSucceeded || completed.State.FinalMessage != "图片主体是一只站在窗边的猫。" {
		t.Fatalf("completed vision runtime = %#v", completed.State)
	}
	if agentCalls.Load() != 2 || visionCalls.Load() != 1 {
		t.Fatalf("runtime provider calls: agent=%d vision=%d", agentCalls.Load(), visionCalls.Load())
	}
	record, err = svc.repo.AgentToolCallForScope(scope, call.ToolCallID, call.ActionVersion)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != agentruntime.ToolCallSucceeded {
		t.Fatalf("vision tool call status = %q", record.Status)
	}
	var tasks []model.Task
	if err := db.Where("type = ?", agentVisionTaskType).Find(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Status != model.TaskStatusSucceeded {
		t.Fatalf("vision runtime tasks = %#v", tasks)
	}
	order, err := svc.repo.BillingOrder(tasks[0].BillingOrderID)
	if err != nil {
		t.Fatal(err)
	}
	if order.Status != model.BillingStatusSettled || order.ProviderRequestID != "vision-runtime-provider-request" ||
		order.InputTokens != 521 || order.CachedTokens != 21 || order.OutputTokens != 32 {
		t.Fatalf("vision runtime billing = %#v", order)
	}
}

func TestAgentVisionTaskManagedSuccessPreservesUsageForKuaiziReconciliation(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	var visionCalls atomic.Int64
	var billingCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/ai-open-platform-api/v1/chat/completions":
			visionCalls.Add(1)
			writeAgentRuntimeChatStream(t, writer, "chatcmpl-vision-managed-provider-task", "画面主体是一座海边灯塔。", 610, 44, 18)
		case kuaiziBillingPath:
			billingCalls.Add(1)
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"code":0,"data":{"items":[{"order_id":"vision-managed-bill","amount":1,"status":"succeeded","task_id":"vision-managed-provider-task","task_status":"succeeded","task_duration":1,"total_tokens":654,"created_at":"2026-09-02T10:00:00Z"}]}}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	svc, db, fixture := newAgentRuntimeServiceFixture(t, server.URL)
	if err := db.Model(&model.CreditAccount{}).
		Where("user_id = ?", "runtime-user").
		Update("available_microcredits", int64(1_000_000_000)).Error; err != nil {
		t.Fatal(err)
	}
	resource := createLocalVisionTestResource(t, svc, db, model.Resource{
		ID: "vision-managed-resource", UserID: "runtime-user", Kind: "image",
		Status: model.ResourceStatusReady, MimeType: "image/png",
	}, encodeVisionTestImage(t, "png", 4, 3))
	createVisionTestProjectAsset(t, db, "runtime-project", resource)
	call := agentVisionCapabilityCall(
		"vision-managed-call", "vision-managed-request", fixture.visionChannelModel,
		[]string{resource.ID}, "描述图片中的主体", agentruntime.VisionDetailLow,
	)
	seedApprovedAgentCapabilityProposal(t, svc, db, call)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
	if err != nil || !queued.Pending {
		t.Fatalf("queued managed vision capability = %#v, err = %v", queued, err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	if visionCalls.Load() != 1 || billingCalls.Load() != 1 {
		t.Fatalf("managed vision calls: provider=%d billing=%d", visionCalls.Load(), billingCalls.Load())
	}
	var task model.Task
	if err := db.Where("type = ?", agentVisionTaskType).Take(&task).Error; err != nil {
		t.Fatal(err)
	}
	order, err := svc.repo.BillingOrder(task.BillingOrderID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != model.TaskStatusSucceeded || order.Status != model.BillingStatusSettled ||
		order.ProviderRequestID != "vision-managed-provider-task" || order.ProviderBillingOrderID != "vision-managed-bill" ||
		order.TokenUsageStatus != "reported" || order.InputTokens != 610 || order.CachedTokens != 18 || order.OutputTokens != 44 {
		t.Fatalf("managed vision billing facts: task=%#v order=%#v", task, order)
	}
	replayed, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
	if err != nil || replayed.Pending {
		t.Fatalf("managed vision terminal replay = %#v, err = %v", replayed, err)
	}
}

func TestAgentVisionCapabilityApprovedRetriesCreateOneTaskAndReservation(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	if err := db.Model(&model.CreditAccount{}).
		Where("user_id = ?", "runtime-user").
		Update("available_microcredits", int64(1_000_000_000)).Error; err != nil {
		t.Fatal(err)
	}
	resource := createLocalVisionTestResource(t, svc, db, model.Resource{
		ID: "vision-task-resource", UserID: "runtime-user", Kind: "image",
		Status: model.ResourceStatusReady, MimeType: "image/png",
	}, encodeVisionTestImage(t, "png", 4, 3))
	createVisionTestProjectAsset(t, db, "runtime-project", resource)
	call := agentVisionCapabilityCall(
		"vision-task-call", "vision-task-request", fixture.visionChannelModel,
		[]string{resource.ID}, "描述主体、构图与空间关系", agentruntime.VisionDetailOriginal,
	)
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
		t.Fatalf("queued vision execution returned a final receipt: first=%#v second=%#v", first, second)
	}

	var tasks []model.Task
	if err := db.Where("type = ? AND operation = ?", agentVisionTaskType, agentVisionOperationForRun(agentRuntimeServiceScope().RunID)).Find(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("vision task count = %d, want 1", len(tasks))
	}
	task := tasks[0]
	if task.UserID != agentRuntimeServiceScope().ActorUserID || task.ProjectID != agentRuntimeServiceScope().CanvasID ||
		task.Audience != model.TaskAudienceInternal || task.Capability != "vision" || task.Status != model.TaskStatusQueued ||
		task.ProviderEndpointVersionID != fixture.endpoint.ID || task.ProviderCredentialVersionID != fixture.version.ID ||
		task.WatermarkCapability != model.WatermarkCapabilityNotApplicable {
		t.Fatalf("vision task facts = %#v", task)
	}
	var input agentVisionTaskInput
	if err := json.Unmarshal([]byte(task.InputJSON), &input); err != nil {
		t.Fatal(err)
	}
	if input.RunID != agentRuntimeServiceScope().RunID || input.ToolCallID != call.ToolCallID ||
		input.FrozenModel.ChannelID != fixture.visionChannel.ID || input.FrozenModel.ModelRecordID != fixture.visionChannelModel.ID ||
		input.FrozenModel.Model != fixture.visionChannelModel.ModelKey || input.FrozenModel.PriceVersion != fixture.visionChannelModel.PriceVersion ||
		input.Arguments.ModelRecordID != fixture.visionChannelModel.ID || input.Arguments.ModelKey != fixture.visionChannelModel.ModelKey ||
		input.Arguments.Prompt != "描述主体、构图与空间关系" || input.Arguments.Detail != agentruntime.VisionDetailOriginal ||
		input.Arguments.ClientRequestID != "vision-task-request" || len(input.Arguments.SourceResourceIDs) != 1 ||
		input.Arguments.SourceResourceIDs[0] != resource.ID {
		t.Fatalf("vision task input = %#v", input)
	}

	var orders []model.BillingOrder
	if err := db.Where("task_id = ?", task.ID).Find(&orders).Error; err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 {
		t.Fatalf("vision billing order count = %d, want 1", len(orders))
	}
	order := orders[0]
	if order.ID != task.BillingOrderID || order.Status != model.BillingStatusReserved || order.BillingMode != "token_usage" ||
		order.ChannelModelID != fixture.visionChannelModel.ID || order.PriceVersion != fixture.visionChannelModel.PriceVersion ||
		order.ProviderEndpointVersionID != fixture.endpoint.ID || order.ProviderCredentialVersionID != fixture.version.ID ||
		order.EstimatedInputTokens <= 0 || order.MaxOutputTokens != 8_192 ||
		order.AmountMicrocredits <= 0 || order.AmountMicrocredits != order.ReservedAmountMicrocredits {
		t.Fatalf("vision billing order facts = %#v", order)
	}
	var reserveCount int64
	if err := db.Model(&model.CreditLedgerEntry{}).
		Where("billing_order_id = ? AND type = ?", order.ID, model.CreditLedgerReserve).
		Count(&reserveCount).Error; err != nil {
		t.Fatal(err)
	}
	if reserveCount != 1 {
		t.Fatalf("vision reserve ledger count = %d, want 1", reserveCount)
	}
	conflicting := call
	conflicting.Arguments, err = json.Marshal(agentruntime.VisionAnalyzeArguments{
		ModelRecordID: fixture.visionChannelModel.ID, ModelKey: fixture.visionChannelModel.ModelKey,
		SourceResourceIDs: []string{resource.ID}, Prompt: "改成另一条提示词",
		Detail: agentruntime.VisionDetailOriginal, ClientRequestID: "vision-task-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), conflicting); err == nil {
		t.Fatal("conflicting vision tool facts reused an approved task")
	}
	var taskCount int64
	if err := db.Model(&model.Task{}).Where("type = ?", agentVisionTaskType).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 1 {
		t.Fatalf("conflicting vision retry created %d tasks", taskCount)
	}
}

func TestAgentVisionCapabilityFreezesConservativeTokenQuoteWithoutCommercialSideEffects(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	resource := createLocalVisionTestResource(t, svc, db, model.Resource{
		ID: "vision-quote-resource", UserID: "runtime-user", Kind: "image",
		Status: model.ResourceStatusReady, MimeType: "image/png",
	}, encodeVisionTestImage(t, "png", 4, 3))
	createVisionTestProjectAsset(t, db, "runtime-project", resource)
	call := agentVisionCapabilityCall(
		"vision-quote-call", "vision-quote-request", fixture.visionChannelModel,
		[]string{resource.ID}, "描述画面的主体与空间关系", agentruntime.VisionDetailLow,
	)
	initializeAgentVisionRunForTest(t, svc)

	quote, err := svc.freezeAgentVisionCapabilityQuote(agentRuntimeServiceScope(), call, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	estimatedInputTokens := int64(len([]byte("描述画面的主体与空间关系"))) + tokenBillingProtocolMarginBytes + 384
	pricing := TokenPricingSnapshot{
		InputPerMillionMicros: 1_000_000, CachedPerMillionMicros: 20_000,
		OutputPerMillionMicros: 2_000_000, MaxOutputTokens: 8_192,
	}
	wantAmount, err := tokenChargeMicrocredits(pricing, TokenUsageFact{
		InputTokens: estimatedInputTokens, OutputTokens: pricing.MaxOutputTokens,
	}, basisPointsScale)
	if err != nil {
		t.Fatal(err)
	}
	if quote.ModelRecordID != fixture.visionChannelModel.ID || quote.ModelKey != fixture.visionChannelModel.ModelKey ||
		quote.PriceVersion != fixture.visionChannelModel.PriceVersion || quote.AmountMicrocredits != wantAmount {
		t.Fatalf("vision quote = %#v, want amount %d", quote, wantAmount)
	}
	var taskCount, orderCount, reserveCount int64
	if err := db.Model(&model.Task{}).Where("type = ?", agentVisionTaskType).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("scene = ?", agentVisionOperationForRun(agentRuntimeServiceScope().RunID)).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).Where("type = ?", model.CreditLedgerReserve).Count(&reserveCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 || orderCount != 0 || reserveCount != 0 {
		t.Fatalf("vision quote created commercial side effects: tasks=%d orders=%d reserves=%d", taskCount, orderCount, reserveCount)
	}
}

func TestAgentRuntimeFreezesVisionQuoteBeforeApproval(t *testing.T) {
	decision := agentRuntimeToolDecisionWithDelivery(t,
		`{"kind":"tool_call","toolCall":{"toolCallId":"vision-runtime-quote","toolName":"vision.analyze","actionVersion":1,"arguments":{"modelRecordId":"runtime-vision-model","modelKey":"deepseek-v4-flash-vision-exp","sourceResourceIds":["vision-runtime-resource"],"prompt":"说明主体与画面关系","detail":"low","clientRequestId":"vision-runtime-request"}}}`,
		agentRuntimeTestAnswerDelivery(),
	)
	server, _ := newAgentRuntimeDecisionServer(t, decision, agentRuntimeTestAnswerDelivery())
	defer server.Close()
	svc, db, fixture := newAgentRuntimeServiceFixture(t, server.URL)
	resource := createLocalVisionTestResource(t, svc, db, model.Resource{
		ID: "vision-runtime-resource", UserID: "runtime-user", Kind: "image",
		Status: model.ResourceStatusReady, MimeType: "image/png",
	}, encodeVisionTestImage(t, "png", 4, 3))
	createVisionTestProjectAsset(t, db, "runtime-project", resource)
	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "runtime-vision-quote", UserMessage: "理解这张图片", MaxSteps: 4,
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
		t.Fatalf("vision approval state = %#v", waiting.State)
	}
	record, err := svc.repo.AgentToolCallForScope(scope, "vision-runtime-quote", 1)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := agentruntime.DecodeApprovalProposal(json.RawMessage(record.ApprovalProposalJSON))
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Quote == nil || proposal.Quote.ModelRecordID != fixture.visionChannelModel.ID ||
		proposal.Quote.ModelKey != fixture.visionChannelModel.ModelKey || proposal.Quote.PriceVersion != fixture.visionChannelModel.PriceVersion ||
		proposal.Quote.AmountMicrocredits <= 0 {
		t.Fatalf("runtime vision approval quote = %#v", proposal.Quote)
	}
	var taskCount int64
	if err := db.Model(&model.Task{}).Where("type = ?", agentVisionTaskType).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 {
		t.Fatalf("vision approval created %d tasks", taskCount)
	}
}

func initializeAgentVisionRunForTest(t *testing.T, svc *Service) {
	t.Helper()
	scope := agentRuntimeServiceScope()
	now := time.Now().UTC()
	configuration, err := svc.resolveAgentRuntimeConfiguration(context.Background(), scope, guidedAgentRuntimeConfigurationInput())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repo.CreateInitializedAgentRun(repository.CreateInitializedAgentRunInput{
		Create: repository.CreateAgentRunInput{Scope: scope, ClientRequestID: "agent-vision-run", Now: now},
		Initialize: repository.InitializeAgentRunInput{
			Scope: scope, ModelRecordID: "runtime-agent-model", ModelKey: "gpt-5.5", MaxSteps: 8,
			ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion, RuntimeVersion: agentruntime.CurrentRuntimeVersion,
			PolicyVersion: agentruntime.CurrentPolicyVersion, UserMessage: "理解图片", Configuration: configuration, Now: now,
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func agentVisionCapabilityCall(
	toolCallID string,
	clientRequestID string,
	selected model.ChannelModel,
	resourceIDs []string,
	prompt string,
	detail agentruntime.VisionDetail,
) agentruntime.ToolCallDecision {
	arguments, err := json.Marshal(agentruntime.VisionAnalyzeArguments{
		ModelRecordID: selected.ID, ModelKey: selected.ModelKey,
		SourceResourceIDs: resourceIDs, Prompt: prompt, Detail: detail, ClientRequestID: clientRequestID,
	})
	if err != nil {
		panic(err)
	}
	return agentruntime.ToolCallDecision{
		ToolCallID: toolCallID, ToolName: agentruntime.ToolVisionAnalyze, ActionVersion: 1, Arguments: arguments,
	}
}

func configureDirectAgentVisionFixture(t *testing.T, svc *Service, db *gorm.DB, baseURL string) model.ChannelModel {
	t.Helper()
	now := time.Now().UTC()
	channel := model.ModelChannel{
		ID: "runtime-direct-vision-channel", Scope: model.ChannelScopeSystem, Enabled: true,
		Name: "Official DeepSeek Vision", BaseURL: baseURL, APIKey: "direct-vision-secret", APIFormat: "openai",
		InterfaceType: model.ChannelInterfaceChatCompletion, ModelsJSON: `["deepseek-v4-flash-vision-exp"]`,
		CreatedAt: now, UpdatedAt: now,
	}
	item := model.ChannelModel{
		ID: "runtime-direct-vision-model", ChannelID: channel.ID, ModelKey: "deepseek-v4-flash-vision-exp",
		DisplayName: "Official DeepSeek Vision", AccessPolicy: model.ModelAccessAuthenticated, Capability: "vision",
		BillingMode: "token_usage", PriceStrategy: "token", PriceConfigured: true, Enabled: true, PriceVersion: 5,
		CreatedAt: now, UpdatedAt: now,
	}
	pricing := model.ModelPricing{
		ID: "runtime-direct-vision-pricing", ChannelID: channel.ID, Model: item.ModelKey, Capability: "vision", Currency: "CNY",
		InputPerMillionMicros: 1_000_000, CachedPerMillionMicros: 20_000,
		OutputPerMillionMicros: 2_000_000, MaxOutputTokens: 8_192, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&pricing).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateAgentDefaultVisionModelSetting(providerAdmin(), UpdateAgentDefaultVisionModelRequest{ChannelModelID: item.ID}); err != nil {
		t.Fatal(err)
	}
	return item
}

type queuedDirectAgentVisionFixture struct {
	service            *Service
	db                 *gorm.DB
	task               model.Task
	order              model.BillingOrder
	projectAssetLinkID string
}

func queueDirectAgentVisionTaskForTest(t *testing.T, providerURL string, suffix string) queuedDirectAgentVisionFixture {
	t.Helper()
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://managed.example.com")
	directModel := configureDirectAgentVisionFixture(t, svc, db, providerURL)
	if err := db.Model(&model.CreditAccount{}).
		Where("user_id = ?", "runtime-user").
		Update("available_microcredits", int64(1_000_000_000)).Error; err != nil {
		t.Fatal(err)
	}
	resource := createLocalVisionTestResource(t, svc, db, model.Resource{
		ID: "vision-" + suffix + "-resource", UserID: "runtime-user", Kind: "image",
		Status: model.ResourceStatusReady, MimeType: "image/png",
	}, encodeVisionTestImage(t, "png", 4, 3))
	linkID := createVisionTestProjectAsset(t, db, "runtime-project", resource)
	call := agentVisionCapabilityCall(
		"vision-"+suffix+"-call", "vision-"+suffix+"-request", directModel,
		[]string{resource.ID}, "描述图片中的主体", agentruntime.VisionDetailLow,
	)
	seedApprovedAgentCapabilityProposal(t, svc, db, call)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
	if err != nil || !queued.Pending {
		t.Fatalf("queued direct vision capability = %#v, err = %v", queued, err)
	}
	var task model.Task
	if err := db.Where("type = ?", agentVisionTaskType).Take(&task).Error; err != nil {
		t.Fatal(err)
	}
	order, err := svc.repo.BillingOrder(task.BillingOrderID)
	if err != nil {
		t.Fatal(err)
	}
	return queuedDirectAgentVisionFixture{
		service: svc, db: db, task: task, order: *order, projectAssetLinkID: linkID,
	}
}

func assertFailedAgentVisionTaskBilling(
	t *testing.T,
	fixture queuedDirectAgentVisionFixture,
	wantBillingStatus model.BillingStatus,
	wantPollStage string,
	wantProviderRequestID string,
) {
	t.Helper()
	storedTask, err := fixture.service.repo.Task(fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != model.TaskStatusFailed || storedTask.PollStage != wantPollStage ||
		storedTask.ProviderRequestID != wantProviderRequestID {
		t.Fatalf("failed vision task = %#v", storedTask)
	}
	storedOrder, err := fixture.service.repo.BillingOrder(fixture.order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedOrder.Status != wantBillingStatus || storedOrder.ProviderRequestID != wantProviderRequestID {
		t.Fatalf("failed vision billing order = %#v", storedOrder)
	}
	var outboxCount int64
	if err := fixture.db.Model(&model.TaskOutbox{}).Where("task_id = ?", fixture.task.ID).Count(&outboxCount).Error; err != nil {
		t.Fatal(err)
	}
	if outboxCount != 1 {
		t.Fatalf("vision terminal outbox count = %d, want 1", outboxCount)
	}
}
