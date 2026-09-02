package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
)

func TestPaidDeepSeekVisionCapabilityUsesDisposableStateAndSettlesExactlyOnce(t *testing.T) {
	if strings.TrimSpace(os.Getenv("CANVAS_RUN_PAID_DEEPSEEK_VISION")) != "1" {
		t.Skip("set CANVAS_RUN_PAID_DEEPSEEK_VISION=1 to authorize the real paid provider call")
	}
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	sourceDBPath := strings.TrimSpace(os.Getenv("CANVAS_PAID_DEEPSEEK_SOURCE_DB"))
	if sourceDBPath == "" {
		t.Fatal("CANVAS_PAID_DEEPSEEK_SOURCE_DB is required")
	}
	sourceDB, err := database.Open(database.Config{Driver: "sqlite", DSN: sourceDBPath})
	if err != nil {
		t.Fatalf("open local credential source: %v", err)
	}
	if sqlDB, sqlErr := sourceDB.DB(); sqlErr == nil {
		defer func() { _ = sqlDB.Close() }()
	}
	var sourceChannel model.ModelChannel
	if err := sourceDB.
		Where("scope = ? AND enabled = ? AND api_key <> ''", model.ChannelScopeSystem, true).
		Where("interface_type IN ?", []model.ChannelInterfaceType{model.ChannelInterfaceChatCompletion, model.ChannelInterfaceOpenAIResponse}).
		Where("lower(base_url) LIKE ?", "https://api.deepseek.com%").
		Order("updated_at DESC").Take(&sourceChannel).Error; err != nil {
		t.Fatalf("load configured local DeepSeek credential: %v", err)
	}
	requireOfficialDeepSeekEndpoint(t, sourceChannel.BaseURL)
	if strings.TrimSpace(sourceChannel.APIKey) == "" {
		t.Fatal("configured local DeepSeek credential is empty")
	}

	var agentCalls atomic.Int64
	var visionDecision string
	agentServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/ai-open-platform-api/v1/chat/completions" {
			http.NotFound(writer, request)
			return
		}
		switch agentCalls.Add(1) {
		case 1:
			writeAgentRuntimeChatStream(t, writer, "paid-agent-vision-decision", visionDecision, 40, 20, 3)
		case 2:
			writeAgentRuntimeChatStream(t, writer, "paid-agent-vision-final", `{"kind":"final","final":{"message":"已完成图片理解。","expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`, 35, 15, 2)
		default:
			t.Errorf("unexpected paid acceptance Agent call")
			http.Error(writer, "unexpected Agent call", http.StatusConflict)
		}
	}))
	defer agentServer.Close()

	svc, db, _ := newAgentRuntimeServiceFixture(t, agentServer.URL)
	selected := configureDirectAgentVisionFixture(t, svc, db, sourceChannel.BaseURL)
	if err := db.Model(&model.ModelChannel{}).Where("id = ?", selected.ChannelID).
		Select("base_url", "api_key", "interface_type").Updates(model.ModelChannel{
		BaseURL: sourceChannel.BaseURL, APIKey: sourceChannel.APIKey, InterfaceType: sourceChannel.InterfaceType,
	}).Error; err != nil {
		t.Fatal(err)
	}
	const initialCredits = int64(10_000 * 1_000_000)
	if err := db.Model(&model.CreditAccount{}).Where("user_id = ?", "runtime-user").
		Select("available_microcredits", "reserved_microcredits").Updates(model.CreditAccount{
		AvailableMicrocredits: initialCredits, ReservedMicrocredits: 0,
	}).Error; err != nil {
		t.Fatal(err)
	}
	resource := createLocalVisionTestResource(t, svc, db, model.Resource{
		ID: "paid-deepseek-vision-resource", UserID: "runtime-user", Kind: "image",
		Status: model.ResourceStatusReady, MimeType: "image/png",
	}, encodeVisionTestImage(t, "png", 64, 64))
	createVisionTestProjectAsset(t, db, "runtime-project", resource)
	call := agentVisionCapabilityCall(
		"paid-deepseek-vision-call", "paid-deepseek-vision-request", selected,
		[]string{resource.ID}, "请简短描述图片的主色和尺寸比例。", agentruntime.VisionDetailLow,
	)
	call.ExpectedDelivery = agentRuntimeTestAnswerDelivery()
	decision, err := json.Marshal(agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &call})
	if err != nil {
		t.Fatal(err)
	}
	visionDecision = string(decision)

	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "paid-deepseek-vision-run", UserMessage: "理解这张项目图片",
		MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.ModelTask == nil {
		t.Fatal("paid acceptance did not create the initial Agent model task")
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatalf("execute paid acceptance Agent decision: %v", err)
	}
	waiting, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.State.Status != agentruntime.RunWaitingApproval || waiting.State.PendingToolCall == nil {
		t.Fatalf("paid vision approval state=%q pending_call=%t", waiting.State.Status, waiting.State.PendingToolCall != nil)
	}
	toolRecord, err := svc.repo.AgentToolCallForScope(scope, call.ToolCallID, call.ActionVersion)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := svc.SubmitAgentToolApproval(scope, AgentToolApprovalSubmission{
		ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion,
		Decision: agentruntime.ToolApprovalApproved, ProposalHash: toolRecord.ApprovalProposalHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if approved.State.Status != agentruntime.RunWaitingTool || !approved.State.PendingToolStarted {
		t.Fatalf("approved paid vision state=%q started=%t", approved.State.Status, approved.State.PendingToolStarted)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatalf("execute paid DeepSeek vision task: %v", err)
	}

	var tasks []model.Task
	if err := db.Where("type = ? AND operation = ?", agentVisionTaskType, agentVisionOperationForRun(agentRuntimeServiceScope().RunID)).Find(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Status != model.TaskStatusSucceeded || strings.TrimSpace(tasks[0].ProviderRequestID) == "" {
		t.Fatalf("paid vision task facts are incomplete: count=%d status=%q provider_request_present=%t", len(tasks), firstTaskStatus(tasks), firstTaskProviderRequestPresent(tasks))
	}
	task := tasks[0]
	var result agentruntime.VisionAnalyzeResult
	if err := json.Unmarshal([]byte(task.ResultJSON), &result); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(result.Analysis) == "" || result.Usage.InputTokens <= 0 || result.Usage.OutputTokens <= 0 {
		t.Fatalf("paid vision result facts are incomplete: analysis_length=%d usage=%#v", len([]rune(result.Analysis)), result.Usage)
	}
	order, err := svc.repo.BillingOrder(task.BillingOrderID)
	if err != nil {
		t.Fatal(err)
	}
	if order.Status != model.BillingStatusSettled || order.ProviderRequestID != task.ProviderRequestID || order.AmountMicrocredits <= 0 || order.ReservedAmountMicrocredits <= 0 {
		t.Fatalf("paid vision billing facts are incomplete: status=%q amount=%d reserved=%d request_match=%t", order.Status, order.AmountMicrocredits, order.ReservedAmountMicrocredits, order.ProviderRequestID == task.ProviderRequestID)
	}
	var finalLedgerCount int64
	if err := db.Model(&model.CreditLedgerEntry{}).
		Where("billing_order_id = ? AND type IN ?", order.ID, []model.CreditLedgerType{model.CreditLedgerConsume, model.CreditLedgerRelease}).
		Count(&finalLedgerCount).Error; err != nil {
		t.Fatal(err)
	}
	if finalLedgerCount != 2 {
		t.Fatalf("paid vision final ledger count=%d, want 2", finalLedgerCount)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatalf("execute post-vision Agent continuation: %v", err)
	}
	completed, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State.Status != agentruntime.RunSucceeded || completed.State.FinalMessage != "已完成图片理解。" || agentCalls.Load() != 2 {
		t.Fatalf("paid Agent continuation status=%q message=%q calls=%d", completed.State.Status, completed.State.FinalMessage, agentCalls.Load())
	}
	var settledTotal int64
	if err := db.Model(&model.BillingOrder{}).
		Where("user_id = ? AND status = ?", task.UserID, model.BillingStatusSettled).
		Select("COALESCE(SUM(amount_microcredits), 0)").Scan(&settledTotal).Error; err != nil {
		t.Fatal(err)
	}
	var account model.CreditAccount
	if err := db.First(&account, "user_id = ?", task.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if account.ReservedMicrocredits != 0 || account.AvailableMicrocredits != initialCredits-settledTotal {
		t.Fatalf("paid Agent account facts are inconsistent: available=%d reserved=%d settled_total=%d", account.AvailableMicrocredits, account.ReservedMicrocredits, settledTotal)
	}

	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
	if err != nil || replayed.Pending {
		t.Fatalf("replay paid DeepSeek vision result: pending=%t err=%v", replayed.Pending, err)
	}
	conflicting := call
	conflicting.Arguments, err = json.Marshal(agentruntime.VisionAnalyzeArguments{
		ModelRecordID: selected.ID, ModelKey: selected.ModelKey, SourceResourceIDs: []string{resource.ID},
		Prompt: "请改为详细描述图片。", Detail: agentruntime.VisionDetailLow, ClientRequestID: "paid-deepseek-vision-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), conflicting); err == nil {
		t.Fatal("changed paid vision facts did not return an idempotency conflict")
	}
	var taskCount, orderCount, consumeCount int64
	if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("id = ?", order.ID).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ? AND type = ?", order.ID, model.CreditLedgerConsume).Count(&consumeCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 1 || orderCount != 1 || consumeCount != 1 {
		t.Fatalf("paid vision replay duplicated commercial facts: tasks=%d orders=%d consumes=%d", taskCount, orderCount, consumeCount)
	}
	t.Logf("paid DeepSeek Agent vision accepted: run=%s tool_call=%s task=%s billing=%s provider_request=%s usage=%d/%d/%d charged=%d remaining=%d analysis_length=%d", scope.RunID, call.ToolCallID, task.ID, order.ID, task.ProviderRequestID, result.Usage.InputTokens, result.Usage.CachedTokens, result.Usage.OutputTokens, order.AmountMicrocredits, account.AvailableMicrocredits, len([]rune(result.Analysis)))
}

func requireOfficialDeepSeekEndpoint(t *testing.T, raw string) {
	t.Helper()
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "api.deepseek.com") {
		t.Fatalf("paid vision credential source is not the official DeepSeek HTTPS endpoint: %q", raw)
	}
}

func firstTaskStatus(tasks []model.Task) model.TaskStatus {
	if len(tasks) == 0 {
		return ""
	}
	return tasks[0].Status
}

func firstTaskProviderRequestPresent(tasks []model.Task) bool {
	return len(tasks) > 0 && strings.TrimSpace(tasks[0].ProviderRequestID) != ""
}
