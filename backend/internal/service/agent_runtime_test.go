package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		encoded, _ := json.Marshal(body)
		if body.Model != "gpt-5.5" || len(body.Messages) != 2 || !strings.Contains(body.Messages[0].Content, "canvas.read_state") || strings.Contains(string(encoded), "runtime-secret-key") {
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
	input := StartAgentRuntimeInput{Scope: agentRuntimeServiceScope(), ClientRequestID: "client-request-complete", UserMessage: "告诉我下一步", MaxSteps: 4}
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
	input := StartAgentRuntimeInput{Scope: agentRuntimeServiceScope(), ClientRequestID: "client-request-failed", UserMessage: "读取画布", MaxSteps: 4}
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

func TestStartAgentRuntimeCreatesOneBilledFrozenModelTask(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	input := StartAgentRuntimeInput{
		Scope: agentRuntimeServiceScope(), ClientRequestID: "client-request-1",
		UserMessage: "读取当前画布并告诉我下一步", MaxSteps: 4,
	}
	first, err := svc.StartAgentRuntime(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.State.Status != agentruntime.RunQueued || first.State.StepNumber != 0 || first.State.UserMessage != input.UserMessage {
		t.Fatalf("initial runtime = %#v", first)
	}
	if first.Run.ModelRecordID != fixture.channelModel.ID || first.Run.ModelKey != fixture.channelModel.ModelKey || first.Run.ToolSchemaVersion != 1 {
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
	if err := db.Model(&model.Task{}).Where("id = ?", first.ModelTask.ID).Update("input_json", `{}`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartAgentRuntime(input); err == nil || !strings.Contains(err.Error(), "input facts conflict") {
		t.Fatalf("mutated model task replay error = %v", err)
	}
}

func TestStartAgentRuntimeFailsBeforeBillingWithoutConfiguredModel(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	if err := db.Where("key = ?", agentDefaultModelSettingKey).Delete(&model.SystemSetting{}).Error; err != nil {
		t.Fatal(err)
	}
	input := StartAgentRuntimeInput{Scope: agentRuntimeServiceScope(), ClientRequestID: "client-request-no-model", UserMessage: "读取画布", MaxSteps: 4}
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
	return svc, db, fixture
}

func agentRuntimeServiceScope() agentruntime.Scope {
	return agentruntime.Scope{
		TenantKind: agentruntime.TenantPersonal, TenantID: "runtime-user", ActorUserID: "runtime-user",
		CanvasID: "runtime-canvas", ThreadID: "runtime-thread", RunID: "runtime-run",
		Access: agentruntime.AccessGrant{Level: agentruntime.AccessManager, SubscriptionActive: true},
	}
}
