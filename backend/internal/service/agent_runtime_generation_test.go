package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func TestAgentGenerationSubmitCreatesOneBilledTaskAcrossReplay(t *testing.T) {
	decision := `{"kind":"tool_call","toolCall":{"toolCallId":"call-generate-image","toolName":"generation.submit","actionVersion":1,"arguments":{"type":"canvas_image","prompt":"生成一张雨夜街道图片","input":{"mode":"image","config":{"channelId":"runtime-image-channel","model":"kz_gpt_image2","quality":"medium","size":"1:1","resolution":"1K","count":1}}}}}`
	server, _ := newAgentRuntimeDecisionServer(t, decision)
	defer server.Close()
	svc, db, fixture := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	createAgentRuntimeImageModel(t, db, fixture)
	scope := agentRuntimeServiceScope()
	input := StartAgentRuntimeInput{Scope: scope, ClientRequestID: "client-generation-submit", UserMessage: "生成一张雨夜街道图片", MaxSteps: 6}
	if _, err := svc.StartAgentRuntime(input); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	waiting, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.State.Status != agentruntime.RunWaitingApproval {
		t.Fatalf("submit decision state = %#v", waiting.State)
	}
	approved, err := svc.SubmitAgentToolApproval(scope, AgentToolApprovalSubmission{
		ToolCallID: "call-generate-image", ActionVersion: 1, Decision: agentruntime.ToolApprovalApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if approved.State.Status != agentruntime.RunWaitingTool {
		t.Fatalf("approved state = %#v", approved.State)
	}
	progress, err := svc.CoordinatePendingAgentTool(scope, CoordinateAgentToolInput{ToolCallID: "call-generate-image", ActionVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.LastToolResult == nil || !progress.State.LastToolResult.Succeeded || progress.ModelTask == nil {
		t.Fatalf("submit progress = %#v", progress)
	}
	var result agentGenerationSubmitResult
	if err := json.Unmarshal(progress.State.LastToolResult.Output, &result); err != nil {
		t.Fatal(err)
	}
	if result.TaskID == "" || result.BillingOrderID == "" || result.Status != model.TaskStatusQueued || result.Capability != "image" || result.Model != "kz_gpt_image2" {
		t.Fatalf("submit result = %#v", result)
	}
	var generationTasks int64
	var generationOrders int64
	if err := db.Model(&model.Task{}).Where("id = ? AND user_id = ? AND project_id = ? AND type = ?", result.TaskID, scope.ActorUserID, scope.CanvasID, "canvas_image").Count(&generationTasks).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("id = ? AND task_id = ? AND user_id = ?", result.BillingOrderID, result.TaskID, scope.ActorUserID).Count(&generationOrders).Error; err != nil {
		t.Fatal(err)
	}
	var before model.CreditAccount
	if err := db.First(&before, "user_id = ?", scope.ActorUserID).Error; err != nil {
		t.Fatal(err)
	}
	replayed, err := svc.CoordinatePendingAgentTool(scope, CoordinateAgentToolInput{ToolCallID: "call-generate-image", ActionVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	var after model.CreditAccount
	if err := db.First(&after, "user_id = ?", scope.ActorUserID).Error; err != nil {
		t.Fatal(err)
	}
	if replayed.State.StateVersion != progress.State.StateVersion || generationTasks != 1 || generationOrders != 1 || before.AvailableMicrocredits != after.AvailableMicrocredits || before.ReservedMicrocredits != after.ReservedMicrocredits {
		t.Fatalf("submit replay = %#v tasks=%d orders=%d before=%#v after=%#v", replayed, generationTasks, generationOrders, before, after)
	}
}

func TestAgentGenerationSubmitRecoversAfterCommercialCommitBeforeToolResult(t *testing.T) {
	decision := `{"kind":"tool_call","toolCall":{"toolCallId":"call-generate-recover","toolName":"generation.submit","actionVersion":1,"arguments":{"type":"canvas_image","prompt":"生成可恢复图片","input":{"mode":"image","config":{"channelId":"runtime-image-channel","model":"kz_gpt_image2","quality":"medium","size":"1:1","resolution":"1K","count":1}}}}}`
	server, _ := newAgentRuntimeDecisionServer(t, decision)
	defer server.Close()
	svc, db, fixture := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	createAgentRuntimeImageModel(t, db, fixture)
	scope := agentRuntimeServiceScope()
	if _, err := svc.StartAgentRuntime(StartAgentRuntimeInput{Scope: scope, ClientRequestID: "client-generation-recover", UserMessage: "生成可恢复图片", MaxSteps: 6}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	waiting, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitAgentToolApproval(scope, AgentToolApprovalSubmission{
		ToolCallID: waiting.State.PendingToolCall.ToolCallID, ActionVersion: waiting.State.PendingToolCall.ActionVersion, Decision: agentruntime.ToolApprovalApproved,
	}); err != nil {
		t.Fatal(err)
	}
	callbackName := "test:fail-generation-tool-result-checkpoint"
	if err := db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		checkpoint, ok := tx.Statement.Dest.(*model.AgentCheckpoint)
		if ok && strings.Contains(checkpoint.StateJSON, `"lastToolResult"`) {
			tx.AddError(errors.New("injected checkpoint failure after generation commercial commit"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	_, firstErr := svc.CoordinatePendingAgentTool(scope, CoordinateAgentToolInput{ToolCallID: "call-generate-recover", ActionVersion: 1})
	if firstErr == nil || !strings.Contains(firstErr.Error(), "injected checkpoint failure") {
		t.Fatalf("first coordination error = %v", firstErr)
	}
	if err := db.Callback().Create().Remove(callbackName); err != nil {
		t.Fatal(err)
	}
	interrupted, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if interrupted.Status != agentruntime.RunWaitingTool || !interrupted.PendingToolStarted {
		t.Fatalf("interrupted generation state = %#v", interrupted)
	}
	var taskCount, orderCount, reserveLedgerCount int64
	if err := db.Model(&model.Task{}).Where("operation = ? AND type = ?", agentGenerationOperation(scope.RunID), "canvas_image").Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("idempotency_key LIKE ?", "agent-generation:%").Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).Where("user_id = ? AND type = ?", scope.ActorUserID, model.CreditLedgerReserve).Count(&reserveLedgerCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 1 || orderCount != 1 || reserveLedgerCount != 2 {
		t.Fatalf("interrupted commercial facts: tasks=%d orders=%d reserveLedgers=%d", taskCount, orderCount, reserveLedgerCount)
	}
	recovered, err := svc.CoordinatePendingAgentTool(scope, CoordinateAgentToolInput{ToolCallID: "call-generate-recover", ActionVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State.LastToolResult == nil || !recovered.State.LastToolResult.Succeeded {
		t.Fatalf("recovered generation = %#v", recovered)
	}
	var recoveredTaskCount, recoveredOrderCount int64
	if err := db.Model(&model.Task{}).Where("operation = ? AND type = ?", agentGenerationOperation(scope.RunID), "canvas_image").Count(&recoveredTaskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("idempotency_key LIKE ?", "agent-generation:%").Count(&recoveredOrderCount).Error; err != nil {
		t.Fatal(err)
	}
	if recoveredTaskCount != 1 || recoveredOrderCount != 1 {
		t.Fatalf("recovered commercial facts: tasks=%d orders=%d", recoveredTaskCount, recoveredOrderCount)
	}
}

func TestAgentGenerationWaitPersistsUntilRealTerminalAsset(t *testing.T) {
	decision := `{"kind":"tool_call","toolCall":{"toolCallId":"call-wait-image","toolName":"generation.wait","actionVersion":1,"arguments":{"taskId":"agent-generation-task"}}}`
	server, _ := newAgentRuntimeDecisionServer(t, decision)
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	scope := agentRuntimeServiceScope()
	input := StartAgentRuntimeInput{Scope: scope, ClientRequestID: "client-generation-wait", UserMessage: "等待图片生成完成", MaxSteps: 6}
	if _, err := svc.StartAgentRuntime(input); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	leaseExpiresAt := now.Add(time.Hour)
	providerTaskCreatedAt := now.Add(time.Minute)
	if err := db.Create(&model.Task{
		ID: "agent-generation-task", UserID: scope.ActorUserID, ProjectID: scope.CanvasID,
		Type: "canvas_image", Capability: "image", Status: model.TaskStatusRunning,
		Stage: "供应商生成中", Progress: 50, Operation: agentGenerationOperation(scope.RunID), Model: "kz_gpt_image2",
		LeaseOwner: "active-generation-worker", LeaseExpiresAt: &leaseExpiresAt,
		CreatedAt: providerTaskCreatedAt, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	waiting, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.State.Status != agentruntime.RunWaitingTool || waiting.State.PendingToolCall == nil {
		t.Fatalf("wait decision state = %#v", waiting.State)
	}
	progress, err := svc.CoordinatePendingAgentTool(scope, CoordinateAgentToolInput{ToolCallID: "call-wait-image", ActionVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Status != agentruntime.RunWaitingTool || !progress.State.PendingToolStarted || progress.State.LastToolResult != nil || progress.ModelTask != nil {
		t.Fatalf("nonterminal wait progress = %#v", progress)
	}
	completedAt := time.Now().UTC()
	resultJSON := `{"images":[{"url":"https://cdn.example/generated.png"}]}`
	if err := db.Model(&model.Task{}).Where("id = ?", "agent-generation-task").Updates(map[string]interface{}{
		"status": model.TaskStatusSucceeded, "stage": "任务完成", "progress": 100,
		"result_json": resultJSON, "completed_at": completedAt, "updated_at": completedAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.DriveAgentRuns(10); err != nil {
		t.Fatal(err)
	}
	completed, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State.LastToolResult == nil || !completed.State.LastToolResult.Succeeded || completed.ModelTask == nil {
		t.Fatalf("terminal wait progress = %#v", completed)
	}
	var result agentGenerationWaitResult
	if err := json.Unmarshal(completed.State.LastToolResult.Output, &result); err != nil {
		t.Fatal(err)
	}
	if result.TaskID != "agent-generation-task" || result.Status != model.TaskStatusSucceeded || len(result.Artifacts) != 1 || result.Artifacts[0].Kind != agentruntime.ArtifactImage || result.Artifacts[0].URL != "https://cdn.example/generated.png" {
		t.Fatalf("terminal wait result = %#v", result)
	}
	replayed, err := svc.CoordinatePendingAgentTool(scope, CoordinateAgentToolInput{ToolCallID: "call-wait-image", ActionVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if replayed.State.StateVersion != completed.State.StateVersion || replayed.ModelTask == nil || replayed.ModelTask.ID != completed.ModelTask.ID {
		t.Fatalf("terminal wait replay = %#v", replayed)
	}
}

func TestAgentRuntimeFinalDeliveryUsesPersistedGenerationEvidence(t *testing.T) {
	server := newAgentRuntimeDecisionSequenceServer(t,
		`{"kind":"tool_call","toolCall":{"toolCallId":"call-wait-delivery","toolName":"generation.wait","actionVersion":1,"arguments":{"taskId":"delivery-generation-task"}}}`,
		`{"kind":"final","final":{"message":"图片已生成。","expectedDelivery":{"kind":"generated_asset","requiredArtifacts":["image"],"completionCriteria":[{"fact":"artifact","artifact":"image"}]}}}`,
	)
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	scope := agentRuntimeServiceScope()
	input := StartAgentRuntimeInput{Scope: scope, ClientRequestID: "client-generation-delivery", UserMessage: "生成并交付图片", MaxSteps: 6}
	if _, err := svc.StartAgentRuntime(input); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.Create(&model.Task{
		ID: "delivery-generation-task", UserID: scope.ActorUserID, ProjectID: scope.CanvasID,
		Type: "canvas_image", Capability: "image", Status: model.TaskStatusSucceeded,
		Stage: "任务完成", Progress: 100, Operation: agentGenerationOperation(scope.RunID), Model: "kz_gpt_image2",
		ResultJSON: `{"images":[{"url":"https://cdn.example/delivery.png"}]}`, CompletedAt: &now, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	if err := svc.DriveAgentRuns(10); err != nil {
		t.Fatal(err)
	}
	coordinated, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if coordinated.ModelTask == nil {
		t.Fatalf("coordinated wait = %#v", coordinated)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	completed, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State.Status != agentruntime.RunSucceeded || completed.State.Verification == nil || completed.State.Verification.Status != agentruntime.VerificationSatisfied {
		t.Fatalf("final delivery = %#v", completed.State)
	}
}

func TestAgentGenerationSubmitRejectsUnknownContractWithoutBilling(t *testing.T) {
	decision := `{"kind":"tool_call","toolCall":{"toolCallId":"call-invalid-generate","toolName":"generation.submit","actionVersion":1,"arguments":{"request":{"type":"canvas_image"},"prompt":"图片","input":{"mode":"image"}}}}`
	server, _ := newAgentRuntimeDecisionServer(t, decision)
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	scope := agentRuntimeServiceScope()
	input := StartAgentRuntimeInput{Scope: scope, ClientRequestID: "client-invalid-generation", UserMessage: "生成图片", MaxSteps: 6}
	if _, err := svc.StartAgentRuntime(input); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	waiting, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitAgentToolApproval(scope, AgentToolApprovalSubmission{
		ToolCallID: "call-invalid-generate", ActionVersion: 1, Decision: agentruntime.ToolApprovalApproved,
	}); err != nil {
		t.Fatal(err)
	}
	progress, err := svc.CoordinatePendingAgentTool(scope, CoordinateAgentToolInput{
		ToolCallID: waiting.State.PendingToolCall.ToolCallID, ActionVersion: waiting.State.PendingToolCall.ActionVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.LastToolResult == nil || progress.State.LastToolResult.Succeeded || progress.State.LastToolResult.ErrorCode != "generation_request_invalid" {
		t.Fatalf("invalid generation result = %#v", progress.State.LastToolResult)
	}
	var count int64
	if err := db.Model(&model.Task{}).Where("user_id = ? AND type = ?", scope.ActorUserID, "canvas_image").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("invalid generation created %d tasks", count)
	}
}

func TestAgentGenerationWaitRejectsTaskOutsideCurrentRun(t *testing.T) {
	decision := `{"kind":"tool_call","toolCall":{"toolCallId":"call-wait-foreign","toolName":"generation.wait","actionVersion":1,"arguments":{"taskId":"foreign-generation-task"}}}`
	server, _ := newAgentRuntimeDecisionServer(t, decision)
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	scope := agentRuntimeServiceScope()
	input := StartAgentRuntimeInput{Scope: scope, ClientRequestID: "client-foreign-generation", UserMessage: "等待生成", MaxSteps: 6}
	if _, err := svc.StartAgentRuntime(input); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.Create(&model.Task{
		ID: "foreign-generation-task", UserID: scope.ActorUserID, ProjectID: scope.CanvasID,
		Type: "canvas_image", Capability: "image", Status: model.TaskStatusSucceeded,
		Operation: agentGenerationOperation("another-run"), ResultJSON: `{"images":[{"url":"https://cdn.example/foreign.png"}]}`,
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	if err := svc.DriveAgentRuns(10); err != nil {
		t.Fatal(err)
	}
	progress, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.LastToolResult == nil || progress.State.LastToolResult.Succeeded || progress.State.LastToolResult.ErrorCode != "generation_task_scope_conflict" {
		t.Fatalf("foreign wait result = %#v", progress.State.LastToolResult)
	}
}

func TestAgentGenerationWaitNeverTurnsFailureIntoArtifact(t *testing.T) {
	testCases := []struct {
		name       string
		status     model.TaskStatus
		resultJSON string
		errorCode  string
	}{
		{name: "failed task", status: model.TaskStatusFailed, errorCode: "generation_failed"},
		{name: "succeeded without media", status: model.TaskStatusSucceeded, resultJSON: `{"mode":"image","images":[]}`, errorCode: "generation_result_invalid"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			decision := `{"kind":"tool_call","toolCall":{"toolCallId":"call-wait-no-artifact","toolName":"generation.wait","actionVersion":1,"arguments":{"taskId":"generation-no-artifact"}}}`
			server, _ := newAgentRuntimeDecisionServer(t, decision)
			defer server.Close()
			svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
			createAgentRuntimeCanvas(t, db)
			scope := agentRuntimeServiceScope()
			if _, err := svc.StartAgentRuntime(StartAgentRuntimeInput{Scope: scope, ClientRequestID: "client-wait-no-artifact", UserMessage: "等待生成", MaxSteps: 6}); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			if err := db.Create(&model.Task{
				ID: "generation-no-artifact", UserID: scope.ActorUserID, ProjectID: scope.CanvasID,
				Type: "canvas_image", Capability: "image", Status: testCase.status,
				Operation: agentGenerationOperation(scope.RunID), ResultJSON: testCase.resultJSON, CreatedAt: now, UpdatedAt: now,
			}).Error; err != nil {
				t.Fatal(err)
			}
			if err := svc.ProcessNextTask(); err != nil {
				t.Fatal(err)
			}
			if err := svc.DriveAgentRuns(10); err != nil {
				t.Fatal(err)
			}
			progress, err := svc.ResumeAgentRuntime(scope)
			if err != nil {
				t.Fatal(err)
			}
			if progress.State.LastToolResult == nil || progress.State.LastToolResult.Succeeded || progress.State.LastToolResult.ErrorCode != testCase.errorCode {
				t.Fatalf("wait failure = %#v", progress.State.LastToolResult)
			}
		})
	}
}

func TestAgentGenerationWaitRecognizesStoredAudioAsset(t *testing.T) {
	previewURL, previewKind := taskMediaPreview(`{"mode":"audio","audio":{"dataUrl":"/api/resources/generated-audio"}}`, "canvas_audio")
	if previewURL != "/api/resources/generated-audio" || previewKind != "audio" {
		t.Fatalf("audio task media = %q %q", previewURL, previewKind)
	}
}

func newAgentRuntimeDecisionSequenceServer(t *testing.T, decisions ...string) *httptest.Server {
	t.Helper()
	for _, decision := range decisions {
		if _, err := agentruntime.ParseModelDecision([]byte(decision)); err != nil {
			t.Fatalf("invalid test decision: %v", err)
		}
	}
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	var call atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		index := int(call.Add(1)) - 1
		if index >= len(decisions) {
			t.Errorf("unexpected model call %d", index+1)
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		response := agentRuntimeChatResponse{Choices: []agentRuntimeChatChoice{{Message: agentRuntimeChatMessage{Content: decisions[index]}}}}
		if err := json.NewEncoder(writer).Encode(response); err != nil {
			t.Error(err)
		}
	}))
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
