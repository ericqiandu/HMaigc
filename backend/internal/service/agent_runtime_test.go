package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func TestAgentRuntimeModelTaskSettlesCreditsAndResumesFromStoredDecision(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.Header.Get("ApiKey") != "runtime-secret-key" {
			t.Errorf("ApiKey = %q", request.Header.Get("ApiKey"))
		}
		var body struct {
			Model          string `json:"model"`
			ResponseFormat struct {
				Type string `json:"type"`
			} `json:"response_format"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		encoded, _ := json.Marshal(body)
		if body.Model != "gpt-5.5" || body.ResponseFormat.Type != "json_object" || len(body.Messages) != 2 ||
			!strings.Contains(body.Messages[0].Content, "production.plan") ||
			!strings.Contains(body.Messages[0].Content, `"planKey":"","baseVersion":0`) ||
			!strings.Contains(body.Messages[0].Content, `"targetDurationMs":10000`) ||
			!strings.Contains(body.Messages[0].Content, `"shotKey":"shot-1"`) ||
			!strings.Contains(body.Messages[0].Content, `"referenceKey":"hero"`) ||
			!strings.Contains(body.Messages[0].Content, `"referenceKeys":["hero"]`) ||
			!strings.Contains(body.Messages[0].Content, `"imagePrompt":"..."`) ||
			!strings.Contains(body.Messages[0].Content, `"videoPrompt":"..."`) ||
			!strings.Contains(body.Messages[0].Content, "所有正式镜头 durationMs 必须大于 0 且总和等于 targetDurationMs") ||
			!strings.Contains(body.Messages[0].Content, "禁止添加未声明字段") ||
			!strings.Contains(body.Messages[0].Content, "fact 为 final_message 或 canvas_revision 时必须省略 artifact") ||
			!strings.Contains(body.Messages[0].Content, `{"fact":"artifact","artifact":"image"}`) ||
			!strings.Contains(body.Messages[0].Content, "production.render") ||
			!strings.Contains(body.Messages[0].Content, `"artifactId":"<reference_image 或 storyboard_image artifactId>"`) ||
			!strings.Contains(body.Messages[0].Content, `"imageConfig":{"size":"9:16","count":1}`) ||
			strings.Contains(body.Messages[0].Content, `"quality":"high"`) ||
			!strings.Contains(body.Messages[0].Content, "qualities 为空时必须省略 quality") ||
			!strings.Contains(body.Messages[0].Content, "参数值必须来自所选 callableModels 的 providerCapabilities") ||
			!strings.Contains(body.Messages[0].Content, `"artifactId":"<video_clip artifactId>"`) ||
			!strings.Contains(body.Messages[0].Content, `"videoConfig":{"durationSeconds":10,"quality":"720p","generateAudio":true}`) ||
			!strings.Contains(body.Messages[0].Content, "必须调用 production.render，让 Runtime 冻结报价并进入 waiting_approval") ||
			!strings.Contains(body.Messages[0].Content, "禁止用 final 消息代替扣费确认") ||
			!strings.Contains(body.Messages[0].Content, "禁止重复新建 production.plan") ||
			!strings.Contains(body.Messages[0].Content, "canvas.commit") ||
			strings.Contains(body.Messages[0].Content, "generation.submit") ||
			strings.Contains(body.Messages[0].Content, "canvas.apply_ops") ||
			!strings.Contains(body.Messages[0].Content, "每次新的工具调用必须使用从未出现过的 toolCallId") ||
			strings.Contains(string(encoded), "runtime-secret-key") {
			t.Errorf("model request = %s", encoded)
		}
		decision := `{"kind":"final","final":{"message":"我会先读取画布事实再给出建议。","expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`
		response := struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}{Choices: make([]struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}, 1)}
		response.Choices[0].Message.Content = decision
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	input := StartAgentRuntimeInput{Scope: agentRuntimeServiceScope(), ClientRequestID: "client-request-complete", UserMessage: "告诉我下一步", MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput()}
	started, err := svc.StartAgentRuntime(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		var callLogs []model.ApiCallLog
		if queryErr := db.Order("created_at ASC").Find(&callLogs, "task_id = ?", started.ModelTask.ID).Error; queryErr != nil {
			t.Fatalf("process task: %v; read api logs: %v", err, queryErr)
		}
		t.Fatalf("process task: %v; upstream calls=%d; api logs=%#v", err, calls.Load(), callLogs)
	}
	completedTask, err := svc.repo.TaskForUser(input.Scope.ActorUserID, started.ModelTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completedTask.Status != model.TaskStatusSucceeded {
		t.Fatalf("completed model task = %#v", completedTask)
	}
	progress, err := svc.ResumeAgentRuntime(input.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Status != agentruntime.RunSucceeded || progress.State.StepNumber != 1 || progress.State.Verification == nil || progress.State.Verification.Status != agentruntime.VerificationSatisfied {
		t.Fatalf("resumed runtime = %#v", progress)
	}
	replayed, err := svc.ResumeAgentRuntime(input.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.State.Status != agentruntime.RunSucceeded || calls.Load() != 1 {
		t.Fatalf("terminal replay = %#v calls=%d", replayed, calls.Load())
	}
	restarted, err := svc.StartAgentRuntime(input)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.State.Status != agentruntime.RunSucceeded || restarted.ModelTask != nil || calls.Load() != 1 {
		t.Fatalf("terminal start replay = %#v calls=%d", restarted, calls.Load())
	}
	var order model.BillingOrder
	if err := db.First(&order, "task_id = ?", started.ModelTask.ID).Error; err != nil {
		t.Fatal(err)
	}
	var credits model.CreditAccount
	if err := db.First(&credits, "user_id = ?", input.Scope.ActorUserID).Error; err != nil {
		t.Fatal(err)
	}
	if order.Status != model.BillingStatusSettled || credits.AvailableMicrocredits != 900 || credits.ReservedMicrocredits != 0 {
		t.Fatalf("settled commercial facts: order=%#v credits=%#v", order, credits)
	}
	var checkpoint model.AgentCheckpoint
	if err := db.Order("sequence DESC").First(&checkpoint, "run_id = ?", input.Scope.RunID).Error; err != nil {
		t.Fatal(err)
	}
	combined := completedTask.InputJSON + completedTask.ResultJSON + completedTask.Error + checkpoint.StateJSON
	if strings.Contains(combined, "runtime-secret-key") || strings.Contains(combined, "Authorization") {
		t.Fatalf("secret reached persisted runtime facts: %s", combined)
	}
}

func TestAgentRuntimeModelFailureTerminatesWithoutRetryOrDoubleCharge(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = writer.Write([]byte(`{"error":{"message":"runtime-secret-key Authorization upstream-private-body"}}`))
	}))
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	input := StartAgentRuntimeInput{Scope: agentRuntimeServiceScope(), ClientRequestID: "client-request-failed", UserMessage: "读取画布", MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput()}
	started, err := svc.StartAgentRuntime(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err == nil {
		t.Fatal("failed upstream request was reported as successful")
	}
	progress, err := svc.ResumeAgentRuntime(input.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Status != agentruntime.RunFailed || progress.State.FailureCode != "model_task_failed" || progress.State.StepNumber != 1 {
		t.Fatalf("failed runtime = %#v", progress)
	}
	replayed, err := svc.ResumeAgentRuntime(input.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.State.Status != agentruntime.RunFailed || calls.Load() != 1 {
		t.Fatalf("failed replay = %#v calls=%d", replayed, calls.Load())
	}
	var taskCount int64
	var order model.BillingOrder
	var credits model.CreditAccount
	if err := db.Model(&model.Task{}).Where("id = ?", started.ModelTask.ID).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&order, "task_id = ?", started.ModelTask.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&credits, "user_id = ?", input.Scope.ActorUserID).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 1 || order.Status != model.BillingStatusRefunded || credits.AvailableMicrocredits != 1_000 || credits.ReservedMicrocredits != 0 {
		t.Fatalf("failure commercial facts: taskCount=%d order=%#v credits=%#v", taskCount, order, credits)
	}
	var failedTask model.Task
	var apiLogs []model.ApiCallLog
	var taskLogs []model.TaskLog
	if err := db.First(&failedTask, "id = ?", started.ModelTask.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Find(&apiLogs, "task_id = ?", started.ModelTask.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Find(&taskLogs, "task_id = ?", started.ModelTask.ID).Error; err != nil {
		t.Fatal(err)
	}
	persisted := failedTask.Error
	for _, entry := range apiLogs {
		persisted += " " + entry.Error
	}
	for _, entry := range taskLogs {
		persisted += " " + entry.Message + " " + entry.Payload
	}
	for _, forbidden := range []string{"runtime-secret-key", "Authorization", "upstream-private-body"} {
		if strings.Contains(persisted, forbidden) {
			t.Fatalf("provider secret or raw body reached failure facts: %s", persisted)
		}
	}
}

func TestAgentRuntimeInvalidDecisionIsFedBackIntoNextModelStep(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		response := agentRuntimeChatResponse{Choices: []agentRuntimeChatChoice{{Message: agentRuntimeChatMessage{Content: `{"kind":"final","final":{"message":"已完成","expectedDelivery":{"kind":"answer","requiredArtifacts":["image"],"completionCriteria":[{"fact":"artifact","artifact":"image"}]}}}`}}}}
		if err := json.NewEncoder(writer).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()

	svc, _, _ := newAgentRuntimeServiceFixture(t, server.URL)
	input := StartAgentRuntimeInput{
		Scope: agentRuntimeServiceScope(), ClientRequestID: "client-invalid-decision-repair",
		UserMessage: "生成一张图片", MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput(),
	}
	started, err := svc.StartAgentRuntime(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatalf("invalid model decision must remain a repairable runtime fact: %v", err)
	}

	progress, err := svc.ResumeAgentRuntime(input.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Status != agentruntime.RunRunning || progress.State.StepNumber != 1 || progress.ModelTask == nil {
		t.Fatalf("repairable runtime = %#v", progress)
	}
	nextTask, err := svc.repo.TaskForUser(input.Scope.ActorUserID, progress.ModelTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(nextTask.Prompt, `"code":"model_decision_invalid"`) ||
		!strings.Contains(nextTask.Prompt, `"reason":"answer delivery facts are inconsistent"`) {
		t.Fatalf("next model prompt lacks structured repair facts: %s", nextTask.Prompt)
	}
	if started.ModelTask == nil || nextTask.ID == started.ModelTask.ID {
		t.Fatalf("repair reused the failed model step: started=%#v next=%#v", started.ModelTask, nextTask)
	}
}

func TestStartAgentRuntimeCreatesOneBilledFrozenModelTask(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	input := StartAgentRuntimeInput{
		Scope: agentRuntimeServiceScope(), ClientRequestID: "client-request-1",
		UserMessage: "读取当前画布并告诉我下一步", MaxSteps: 4,
		Configuration: guidedAgentRuntimeConfigurationInput(),
	}
	first, err := svc.StartAgentRuntime(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.State.Status != agentruntime.RunQueued || first.State.StepNumber != 0 || first.State.UserMessage != input.UserMessage {
		t.Fatalf("initial runtime = %#v", first)
	}
	if first.Run.ModelRecordID != fixture.channelModel.ID || first.Run.ModelKey != fixture.channelModel.ModelKey || first.Run.ToolSchemaVersion != agentruntime.CurrentToolSchemaVersion || first.Run.RuntimeVersion != agentruntime.CurrentRuntimeVersion || first.Run.PolicyVersion != agentruntime.CurrentPolicyVersion {
		t.Fatalf("frozen run model = %#v", first.Run)
	}
	if first.ModelTask == nil || first.ModelTask.Status != model.TaskStatusQueued || first.ModelTask.Type != agentRuntimeModelTaskType || first.ModelTask.Model != fixture.channelModel.ModelKey {
		t.Fatalf("model task = %#v", first.ModelTask)
	}
	if first.ModelTask.ProviderAccountID != fixture.account.ID || first.ModelTask.ProviderEndpointVersionID != fixture.endpoint.ID || first.ModelTask.ProviderCredentialVersionID != fixture.version.ID {
		t.Fatalf("frozen task provider = %#v", first.ModelTask)
	}
	var order model.BillingOrder
	if err := db.First(&order, "task_id = ?", first.ModelTask.ID).Error; err != nil {
		t.Fatal(err)
	}
	if order.Status != model.BillingStatusReserved || order.IdempotencyKey != "agent-runtime:"+input.Scope.RunID+":0" || order.AmountMicrocredits != fixture.channelModel.UnitPriceMicrocredits {
		t.Fatalf("billing order = %#v", order)
	}

	replayed, err := svc.StartAgentRuntime(input)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ModelTask == nil || replayed.ModelTask.ID != first.ModelTask.ID {
		t.Fatalf("replayed task = %#v", replayed.ModelTask)
	}
	var taskCount, orderCount, reserveLedgerCount int64
	if err := db.Model(&model.Task{}).Where("type = ?", agentRuntimeModelTaskType).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("user_id = ?", input.Scope.ActorUserID).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).Where("user_id = ? AND type = ?", input.Scope.ActorUserID, model.CreditLedgerReserve).Count(&reserveLedgerCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 1 || orderCount != 1 || reserveLedgerCount != 1 {
		t.Fatalf("idempotent facts: tasks=%d orders=%d reserveLedgers=%d", taskCount, orderCount, reserveLedgerCount)
	}
	var credits model.CreditAccount
	if err := db.First(&credits, "user_id = ?", input.Scope.ActorUserID).Error; err != nil {
		t.Fatal(err)
	}
	if credits.AvailableMicrocredits != 900 || credits.ReservedMicrocredits != 100 {
		t.Fatalf("credits charged more than once = %#v", credits)
	}

	changed := input
	changed.UserMessage = "另一个请求"
	if _, err := svc.StartAgentRuntime(changed); err == nil || !strings.Contains(err.Error(), "request facts conflict") {
		t.Fatalf("changed idempotent request error = %v", err)
	}
	changedMode := input
	changedMode.Configuration.ExecutionMode = agentruntime.ExecutionAutomatic
	if _, err := svc.StartAgentRuntime(changedMode); err == nil || !strings.Contains(err.Error(), "request facts conflict") {
		t.Fatalf("changed execution mode replay error = %v", err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", first.ModelTask.ID).Update("input_json", `{}`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartAgentRuntime(input); err == nil || !strings.Contains(err.Error(), "input facts conflict") {
		t.Fatalf("mutated model task replay error = %v", err)
	}
}

func TestStartAgentRuntimeCreatesTokenBilledFrozenModelTask(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	_, pricing := configureTokenBilledAgentFixture(t, svc, db, fixture)

	input := StartAgentRuntimeInput{
		Scope: agentRuntimeServiceScope(), ClientRequestID: "client-request-token-billing",
		UserMessage: "读取当前画布并告诉我下一步", MaxSteps: 4,
		Configuration: guidedAgentRuntimeConfigurationInput(),
	}
	started, err := svc.StartAgentRuntime(input)
	if err != nil {
		t.Fatal(err)
	}
	if started.ModelTask == nil {
		t.Fatal("token-billed Agent task was not created")
	}
	if started.ModelTask.Audience != model.TaskAudienceInternal {
		t.Fatalf("Agent model task audience = %q", started.ModelTask.Audience)
	}
	var order model.BillingOrder
	if err := db.First(&order, "task_id = ?", started.ModelTask.ID).Error; err != nil {
		t.Fatal(err)
	}
	wantKey := "proxy-token:" + agentRuntimeBillingKey(input.Scope.RunID, 0)
	if order.BillingMode != "token_usage" || order.IdempotencyKey != wantKey || order.Status != model.BillingStatusReserved {
		t.Fatalf("token billing identity = %#v", order)
	}
	if order.EstimatedInputTokens <= int64(len(input.UserMessage)) || order.MaxOutputTokens != pricing.ExpectedOutputTokens || order.TokenPricingSnapshotJSON == "" {
		t.Fatalf("token billing reservation = %#v", order)
	}
	if order.ProviderEndpointVersionID != fixture.endpoint.ID || order.ProviderCredentialVersionID != fixture.version.ID || order.AmountMicrocredits <= 0 {
		t.Fatalf("token billing frozen runtime = %#v", order)
	}
	if started.ModelTask.ProviderEndpointVersionID != order.ProviderEndpointVersionID || started.ModelTask.ProviderCredentialVersionID != order.ProviderCredentialVersionID {
		t.Fatalf("task/order runtime mismatch: task=%#v order=%#v", started.ModelTask, order)
	}
}

func TestStartPersonalAgentRuntimeBillsPersonalAccountWhenUserAlsoHasTeamMembership(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	configureTokenBilledAgentFixture(t, svc, db, fixture)
	createAgentRuntimeTeamMembership(t, db, "runtime-team", 0)

	input := StartAgentRuntimeInput{
		Scope: agentRuntimeServiceScope(), ClientRequestID: "personal-agent-with-team-membership",
		UserMessage: "读取个人画布并告诉我下一步", MaxSteps: 4,
		Configuration: guidedAgentRuntimeConfigurationInput(),
	}
	started, err := svc.StartAgentRuntime(input)
	if err != nil {
		t.Fatalf("StartAgentRuntime() error = %v, want personal credit account", err)
	}
	if started.ModelTask == nil {
		t.Fatalf("StartAgentRuntime() state = %#v, want personal billed model task", started.State)
	}
	var order model.BillingOrder
	if err := db.First(&order, "task_id = ?", started.ModelTask.ID).Error; err != nil {
		t.Fatal(err)
	}
	if order.TeamID != "" {
		t.Fatalf("personal Agent billing order TeamID = %q, want personal account", order.TeamID)
	}
}

func TestStartPersonalAgentRuntimeDoesNotUseUnlimitedTeamEntitlement(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	configureTokenBilledAgentFixture(t, svc, db, fixture)
	createAgentRuntimeTeamMembership(t, db, "runtime-team", 1_000_000_000)
	teamPlan, err := json.Marshal(model.MembershipPlan{
		ID: "runtime-team-plan", Name: "Runtime Team", Tier: "pro", Audience: model.MembershipAudienceTeam,
		ImageConcurrency: 99, VideoConcurrency: 99, UnlimitedTaskQueue: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.MembershipSubscription{}).
		Where("id = ?", "runtime-team-subscription").
		Update("plan_snapshot_json", string(teamPlan)).Error; err != nil {
		t.Fatal(err)
	}

	for index := 0; index < 6; index++ {
		suffix := strconv.Itoa(index)
		scope := agentRuntimeServiceScope()
		scope.RunID = "personal-entitlement-run-" + suffix
		scope.ThreadID = "personal-entitlement-thread-" + suffix
		_, startErr := svc.StartAgentRuntime(StartAgentRuntimeInput{
			Scope: scope, ClientRequestID: "personal-entitlement-request-" + suffix,
			UserMessage: "检查个人账户并发范围", MaxSteps: 4,
			Configuration: guidedAgentRuntimeConfigurationInput(),
		})
		if index < 5 && startErr != nil {
			t.Fatalf("personal Agent task %d error = %v", index+1, startErr)
		}
		if index == 5 && (startErr == nil || !strings.Contains(startErr.Error(), "并发额度")) {
			t.Fatalf("sixth personal Agent task error = %v, want personal concurrency rejection", startErr)
		}
	}
}

func TestStartPersonalAgentRuntimeIgnoresActiveTasksBilledToTeam(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	configureTokenBilledAgentFixture(t, svc, db, fixture)
	createAgentRuntimeTeamMembership(t, db, "runtime-team", 1_000_000_000)
	for index := 0; index < 5; index++ {
		suffix := strconv.Itoa(index)
		orderID := "team-active-order-" + suffix
		if err := db.Create(&model.BillingOrder{
			ID: orderID, UserID: "runtime-user", TeamID: "runtime-team", TaskID: "team-active-task-" + suffix,
			IdempotencyKey: "team-active-idempotency-" + suffix,
			Status:         model.BillingStatusReserved, BillingMode: "fixed_request", AmountMicrocredits: 100,
		}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.Task{
			ID: "team-active-task-" + suffix, UserID: "runtime-user", ProjectID: "runtime-team-canvas",
			Type: agentRuntimeModelTaskType, Capability: taskCapabilityOther, Status: model.TaskStatusQueued,
			BillingOrderID: orderID,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}

	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: agentRuntimeServiceScope(), ClientRequestID: "personal-ignores-team-active-tasks",
		UserMessage: "检查个人账户任务范围", MaxSteps: 4,
		Configuration: guidedAgentRuntimeConfigurationInput(),
	})
	if err != nil {
		t.Fatalf("StartAgentRuntime() error = %v, want team tasks excluded from personal concurrency", err)
	}
	if started.ModelTask == nil {
		t.Fatal("personal Agent model task was not created")
	}
}

func TestStartAgentRuntimeRejectsReplayedBillingOrderFromDifferentAccount(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	configureTokenBilledAgentFixture(t, svc, db, fixture)
	input := StartAgentRuntimeInput{
		Scope: agentRuntimeServiceScope(), ClientRequestID: "agent-replay-billing-account-mismatch",
		UserMessage: "检查当前画布", MaxSteps: 4,
		Configuration: guidedAgentRuntimeConfigurationInput(),
	}
	started, err := svc.StartAgentRuntime(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).
		Where("id = ?", started.ModelTask.BillingOrderID).
		Update("team_id", "different-team").Error; err != nil {
		t.Fatal(err)
	}

	if _, err := svc.StartAgentRuntime(input); err == nil || !strings.Contains(err.Error(), "billing facts conflict") {
		t.Fatalf("replayed Agent billing mismatch error = %v", err)
	}
}

func TestStartTeamAgentRuntimeBillsExactTeamAccount(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	configureTokenBilledAgentFixture(t, svc, db, fixture)
	createAgentRuntimeTeamMembership(t, db, "runtime-team", 1_000_000_000)
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", "runtime-canvas").Updates(map[string]interface{}{
		"team_id": "runtime-team", "default_team_access": model.CanvasAccessManager,
	}).Error; err != nil {
		t.Fatal(err)
	}
	scope := agentRuntimeServiceScope()
	scope.TenantKind = agentruntime.TenantTeam
	scope.TenantID = "runtime-team"

	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "team-agent-exact-billing-scope",
		UserMessage: "读取团队画布并告诉我下一步", MaxSteps: 4,
		Configuration: guidedAgentRuntimeConfigurationInput(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.ModelTask == nil {
		t.Fatalf("StartAgentRuntime() state = %#v, want team billed model task", started.State)
	}
	var order model.BillingOrder
	if err := db.First(&order, "task_id = ?", started.ModelTask.ID).Error; err != nil {
		t.Fatal(err)
	}
	if order.TeamID != "runtime-team" {
		t.Fatalf("team Agent billing order TeamID = %q, want runtime-team", order.TeamID)
	}
}

func TestAgentRuntimeTokenBillingSettlesFromProviderUsageAndBill(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	var chatCalls atomic.Int32
	var billingCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/ai-open-platform-api/v1/chat/completions":
			chatCalls.Add(1)
			var body struct {
				Model     string `json:"model"`
				MaxTokens int64  `json:"max_tokens"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Model != "deepseek-v4-flash" || body.MaxTokens != 50_000 {
				t.Errorf("chat request = %#v", body)
			}
			decision := `{"kind":"final","final":{"message":"已完成。","expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`
			_, _ = writer.Write([]byte(`{"id":"chatcmpl-runtime-token-task","choices":[{"message":{"content":` + strconv.Quote(decision) + `}}],"usage":{"prompt_tokens":30,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":5}}}`))
		case kuaiziBillingPath:
			billingCalls.Add(1)
			_, _ = writer.Write([]byte(`{"code":0,"data":{"items":[{"order_id":"provider-order-1","amount":1,"status":"succeeded","task_id":"runtime-token-task","task_status":"succeeded","task_duration":1,"total_tokens":40,"created_at":"2026-08-16T08:00:00Z"}]}}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	svc, db, fixture := newAgentRuntimeServiceFixture(t, server.URL)
	configureTokenBilledAgentFixture(t, svc, db, fixture)
	input := StartAgentRuntimeInput{
		Scope: agentRuntimeServiceScope(), ClientRequestID: "client-request-token-settlement",
		UserMessage: "告诉我下一步", MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput(),
	}
	started, err := svc.StartAgentRuntime(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	var order model.BillingOrder
	if err := db.First(&order, "task_id = ?", started.ModelTask.ID).Error; err != nil {
		t.Fatal(err)
	}
	var credits model.CreditAccount
	if err := db.First(&credits, "user_id = ?", input.Scope.ActorUserID).Error; err != nil {
		t.Fatal(err)
	}
	if chatCalls.Load() != 1 || billingCalls.Load() != 1 {
		t.Fatalf("provider calls: chat=%d billing=%d", chatCalls.Load(), billingCalls.Load())
	}
	if order.Status != model.BillingStatusSettled || order.AmountMicrocredits != 1_000_000 || order.ProviderBillingOrderID != "provider-order-1" || order.ProviderRequestID != "runtime-token-task" {
		t.Fatalf("settled token bill = %#v", order)
	}
	if order.InputTokens != 30 || order.CachedTokens != 5 || order.OutputTokens != 10 || order.ProviderBillingTotalTokens != 40 {
		t.Fatalf("settled token usage = %#v", order)
	}
	if credits.AvailableMicrocredits != 999_000_000 || credits.ReservedMicrocredits != 0 {
		t.Fatalf("settled credits = %#v", credits)
	}
}

func TestAgentRuntimeInvalidDecisionDoesNotRefundSuccessfulTokenCall(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	var chatCalls atomic.Int32
	var billingCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/ai-open-platform-api/v1/chat/completions":
			chatCalls.Add(1)
			_, _ = writer.Write([]byte(`{"id":"chatcmpl-invalid-decision-task","choices":[{"message":{"content":"not-json"}}],"usage":{"prompt_tokens":20,"completion_tokens":3}}`))
		case kuaiziBillingPath:
			billingCalls.Add(1)
			_, _ = writer.Write([]byte(`{"code":0,"data":{"items":[{"order_id":"provider-order-invalid-decision","amount":1,"status":"succeeded","task_id":"invalid-decision-task","task_status":"succeeded","task_duration":1,"total_tokens":23,"created_at":"2026-08-16T08:00:00Z"}]}}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	svc, db, fixture := newAgentRuntimeServiceFixture(t, server.URL)
	configureTokenBilledAgentFixture(t, svc, db, fixture)
	input := StartAgentRuntimeInput{
		Scope: agentRuntimeServiceScope(), ClientRequestID: "client-request-invalid-token-decision",
		UserMessage: "告诉我下一步", MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput(),
	}
	started, err := svc.StartAgentRuntime(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatalf("invalid Agent decision must remain repairable after the paid request succeeds: %v", err)
	}
	var task model.Task
	var order model.BillingOrder
	if err := db.First(&task, "id = ?", started.ModelTask.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&order, "task_id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if task.Status != model.TaskStatusSucceeded || order.Status != model.BillingStatusSettled {
		t.Fatalf("repairable decision facts: task=%#v order=%#v", task, order)
	}
	progress, err := svc.ResumeAgentRuntime(input.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Status != agentruntime.RunRunning || progress.State.DecisionFeedback == nil || progress.ModelTask == nil {
		t.Fatalf("invalid paid decision was not returned to the same runtime: %#v", progress)
	}
	if err := svc.RunKuaiziBillingReconciliationBatch(context.Background(), time.Now().Add(time.Minute), 10); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&order, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if order.Status != model.BillingStatusSettled || order.AmountMicrocredits != 1_000_000 || chatCalls.Load() != 1 || billingCalls.Load() != 1 {
		t.Fatalf("reconciled failed decision: order=%#v chat=%d billing=%d", order, chatCalls.Load(), billingCalls.Load())
	}
}

func TestStartAgentRuntimeFailsBeforeBillingWithoutConfiguredModel(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	if err := db.Where("key = ?", agentDefaultModelSettingKey).Delete(&model.SystemSetting{}).Error; err != nil {
		t.Fatal(err)
	}
	input := StartAgentRuntimeInput{Scope: agentRuntimeServiceScope(), ClientRequestID: "client-request-no-model", UserMessage: "读取画布", MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput()}
	if _, err := svc.StartAgentRuntime(input); err == nil || !strings.Contains(err.Error(), "尚未配置") {
		t.Fatalf("unconfigured Agent model error = %v", err)
	}
	var taskCount int64
	var billingCount int64
	if err := db.Model(&model.Task{}).Where("type = ?", agentRuntimeModelTaskType).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("idempotency_key LIKE ?", "agent-runtime:%").Count(&billingCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 || billingCount != 0 {
		t.Fatalf("unconfigured Agent model created commercial facts: tasks=%d billings=%d", taskCount, billingCount)
	}
}

type agentRuntimeServiceFixture struct {
	account      model.ProviderAccount
	endpoint     model.ProviderEndpointVersion
	credential   model.ProviderCredential
	version      model.ProviderCredentialVersion
	channel      model.ModelChannel
	channelModel model.ChannelModel
}

func newAgentRuntimeServiceFixture(t *testing.T, endpointURL string) (*Service, *gorm.DB, agentRuntimeServiceFixture) {
	t.Helper()
	svc, db := openProviderCredentialService(t)
	if err := database.EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	fixture := agentRuntimeServiceFixture{
		account:    model.ProviderAccount{ID: "runtime-account", ProviderKind: kuaiziProviderKind, Name: "筷子", Enabled: true, CreatedAt: now, UpdatedAt: now},
		endpoint:   model.ProviderEndpointVersion{ID: "runtime-endpoint-v1", ProviderAccountID: "runtime-account", BaseURL: endpointURL, Status: "active", Version: 1, CreatedAt: now},
		credential: model.ProviderCredential{ID: "runtime-credential", ProviderAccountID: "runtime-account", Family: kuaiziAccountCredentialFamily, Enabled: true, HealthStatus: "healthy", ConcurrencyLimit: 1, CreatedAt: now, UpdatedAt: now},
		channel:    model.ModelChannel{ID: deterministicKuaiziChatChannelID("gpt"), Scope: model.ChannelScopeSystem, Enabled: true, Name: "Agent GPT", APIFormat: "openai", InterfaceType: model.ChannelInterfaceChatCompletion, ModelsJSON: `["gpt-5.5"]`, CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&fixture.account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&fixture.endpoint).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&fixture.credential).Error; err != nil {
		t.Fatal(err)
	}
	ciphertext, err := svc.EncryptProviderSecret(fixture.account.ID, fixture.credential.ID, 1, "runtime-secret-key")
	if err != nil {
		t.Fatal(err)
	}
	fixture.version = model.ProviderCredentialVersion{ID: "runtime-key-v1", ProviderCredentialID: fixture.credential.ID, KeyCipher: ciphertext, Status: "active", Version: 1, CreatedAt: now}
	if err := db.Create(&fixture.version).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&fixture.channel).Error; err != nil {
		t.Fatal(err)
	}
	fixture.channelModel = model.ChannelModel{
		ID: "runtime-agent-model", ChannelID: fixture.channel.ID, ModelKey: "gpt-5.5", DisplayName: "GPT 5.5",
		ProviderCredentialID: fixture.credential.ID, AccessPolicy: model.ModelAccessAuthenticated, Capability: "text",
		BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 100, PriceConfigured: true, Enabled: true,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&fixture.channelModel).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateAgentDefaultModelSetting(providerAdmin(), UpdateAgentDefaultModelRequest{ChannelModelID: fixture.channelModel.ID}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CreditAccount{UserID: "runtime-user", AvailableMicrocredits: 1_000}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CanvasProject{
		ID: "runtime-canvas", UserID: "runtime-user", Title: "Agent Canvas", Revision: 7,
		PayloadJSON: `{"nodes":[],"connections":[]}`, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	return svc, db, fixture
}

func configureTokenBilledAgentFixture(t *testing.T, svc *Service, db *gorm.DB, fixture agentRuntimeServiceFixture) (model.ChannelModel, model.ModelPricing) {
	t.Helper()
	now := time.Now().UTC()
	channel := model.ModelChannel{
		ID: deterministicKuaiziChatChannelID("deepseek"), Scope: model.ChannelScopeSystem, Enabled: true,
		Name: "Agent DeepSeek", APIFormat: "openai", InterfaceType: model.ChannelInterfaceChatCompletion,
		ModelsJSON: `["deepseek-v4-flash"]`, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	item := model.ChannelModel{
		ID: "runtime-token-agent-model", ChannelID: channel.ID, ModelKey: "deepseek-v4-flash", DisplayName: "DeepSeek V4 Flash",
		ProviderCredentialID: fixture.credential.ID, AccessPolicy: model.ModelAccessAuthenticated, Capability: "text",
		BillingMode: "token_usage", PriceStrategy: "token", UnitPriceMicrocredits: 0, PriceConfigured: true, Enabled: true,
		PriceVersion: 3, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	pricing := model.ModelPricing{
		ID: "runtime-token-pricing", ChannelID: channel.ID, Model: item.ModelKey, Capability: "text", Currency: "CNY",
		InputPerMillionMicros: 1_000_000, CachedPerMillionMicros: 20_000, OutputPerMillionMicros: 2_000_000,
		ExpectedOutputTokens: 50_000, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&pricing).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditAccount{}).Where("user_id = ?", "runtime-user").Update("available_microcredits", int64(1_000_000_000)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateAgentDefaultModelSetting(providerAdmin(), UpdateAgentDefaultModelRequest{ChannelModelID: item.ID}); err != nil {
		t.Fatal(err)
	}
	return item, pricing
}

func agentRuntimeServiceScope() agentruntime.Scope {
	return agentruntime.Scope{
		TenantKind: agentruntime.TenantPersonal, TenantID: "runtime-user", ActorUserID: "runtime-user",
		CanvasID: "runtime-canvas", ThreadID: "runtime-thread", RunID: "runtime-run",
		Access: agentruntime.AccessGrant{Level: agentruntime.AccessManager, SubscriptionActive: true},
	}
}

func guidedAgentRuntimeConfigurationInput() AgentRuntimeConfigurationInput {
	return AgentRuntimeConfigurationInput{ExecutionMode: agentruntime.ExecutionGuided}
}

func createAgentRuntimeTeamMembership(t *testing.T, db *gorm.DB, teamID string, availableMicrocredits int64) {
	t.Helper()
	now := time.Now().UTC()
	endsAt := now.Add(24 * time.Hour)
	planSnapshot, err := json.Marshal(model.MembershipPlan{
		ID: teamID + "-plan", Name: "Runtime Team", Tier: "pro", Audience: model.MembershipAudienceTeam,
		ImageConcurrency: 4, VideoConcurrency: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Team{
		ID: teamID, OwnerUserID: "runtime-user", Name: "Runtime Team", Status: model.TeamStatusActive,
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TeamMember{
		ID: teamID + "-member", TeamID: teamID, UserID: "runtime-user",
		Role: model.TeamMemberRoleOwner, Status: model.TeamMemberStatusActive, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MembershipSubscription{
		ID: teamID + "-subscription", TeamID: teamID, PlanID: teamID + "-plan",
		Status: model.MembershipSubscriptionActive, Seats: 2, PlanSnapshotJSON: string(planSnapshot),
		StartsAt: now.Add(-time.Hour), EndsAt: &endsAt, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TeamCreditAccount{
		TeamID: teamID, AvailableMicrocredits: availableMicrocredits, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func agentRuntimeTestSkillChecksum(instructions string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(instructions)))
	return hex.EncodeToString(digest[:])
}

func createAgentRuntimeImageModel(t *testing.T, db *gorm.DB, fixture agentRuntimeServiceFixture) {
	t.Helper()
	now := time.Now().UTC()
	channel := model.ModelChannel{
		ID: "runtime-image-channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "Agent Image",
		APIFormat: "openai", InterfaceType: model.ChannelInterfaceOpenAIImage, ModelsJSON: `["kz_gpt_image2"]`,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	channelModel := model.ChannelModel{
		ID: "runtime-image-model", ChannelID: channel.ID, ModelKey: "kz_gpt_image2", DisplayName: "GPT Image 2",
		ProviderCredentialID: fixture.credential.ID, AccessPolicy: model.ModelAccessAuthenticated, Capability: "image",
		BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 250, PriceConfigured: true, Enabled: true,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&channelModel).Error; err != nil {
		t.Fatal(err)
	}
}
