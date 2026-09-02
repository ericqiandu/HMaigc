package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/service"

	"gorm.io/gorm"
)

func TestAgentRuntimeHTTPCreatesScopedThreadAndRejectsMalformedRequests(t *testing.T) {
	fixture := openProviderAccountHandlerFixture(t)
	if err := database.EnsureAgentRuntimeIntegritySchema(fixture.db); err != nil {
		t.Fatal(err)
	}
	svc := service.New(repository.New(fixture.db), t.TempDir())
	RegisterAgentRuntimeRoutes(fixture.router.Group("/api"), svc)
	createAgentRuntimeHandlerCanvas(t, fixture, "handler-agent-canvas")
	configureAgentRuntimeHandlerDefaultVision(t, svc, fixture.db)

	if response := fixture.request(http.MethodPost, "/api/agent/threads", `{"canvasId":"handler-agent-canvas"}`, "", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, body = %s", response.Code, response.Body.String())
	}
	if response := fixture.request(http.MethodPost, "/api/agent/threads", `{"canvasId":"handler-agent-canvas"} trailing`, fixture.userCookie, ""); response.Code != http.StatusBadRequest {
		t.Fatalf("malformed status = %d, body = %s", response.Code, response.Body.String())
	}

	created := fixture.request(http.MethodPost, "/api/agent/threads", `{"canvasId":"handler-agent-canvas"}`, fixture.userCookie, "")
	if created.Code != http.StatusOK {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	for header, want := range map[string]string{
		"Cache-Control": "private, no-store", "Pragma": "no-cache", "Referrer-Policy": "no-referrer", "X-Content-Type-Options": "nosniff",
	} {
		if got := created.Header().Get(header); got != want {
			t.Fatalf("%s = %q, want %q", header, got, want)
		}
	}
	var envelope struct {
		Data model.AgentThread `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.ID == "" || envelope.Data.CanvasID != "handler-agent-canvas" || envelope.Data.CreatedByUserID != fixture.userID || envelope.Data.TenantKind != agentruntime.TenantPersonal {
		t.Fatalf("thread facts = %#v", envelope.Data)
	}

	failedRun := fixture.request(http.MethodPost, "/api/agent/threads/"+envelope.Data.ID+"/runs", `{"clientRequestId":"request-1","userMessage":"生成一张图片","maxSteps":6,"configuration":{"generationModels":{},"skillDirs":[],"attachments":[],"executionMode":"guided"}}`, fixture.userCookie, "")
	if failedRun.Code != http.StatusServiceUnavailable || !strings.Contains(failedRun.Body.String(), "Agent 模型") {
		t.Fatalf("missing model status = %d, body = %s", failedRun.Code, failedRun.Body.String())
	}
}

func TestAgentLocalRuntimeHTTPIsStrictScopedAndCASProtected(t *testing.T) {
	fixture := openProviderAccountHandlerFixture(t)
	if err := database.EnsureAgentRuntimeIntegritySchema(fixture.db); err != nil {
		t.Fatal(err)
	}
	svc := service.New(repository.New(fixture.db), t.TempDir())
	RegisterAgentRuntimeRoutes(fixture.router.Group("/api"), svc)
	createAgentRuntimeHandlerCanvas(t, fixture, "handler-agent-local-canvas")
	configureAgentRuntimeHandlerDefaultVision(t, svc, fixture.db)

	startPath := "/api/agent/local/runs"
	startBody := `{"canvasId":"handler-agent-local-canvas","externalThreadId":"codex-thread-handler","clientRequestId":"local-turn-1","userMessage":"读取当前画布","maxSteps":8,"configuration":{"generationModels":{},"skillDirs":[],"attachments":[],"executionMode":"automatic"}}`
	if response := fixture.request(http.MethodPost, startPath, startBody, "", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous local start status = %d, body = %s", response.Code, response.Body.String())
	}
	if response := fixture.request(http.MethodPost, startPath, strings.TrimSuffix(startBody, "}")+`,"unexpected":true}`, fixture.userCookie, ""); response.Code != http.StatusBadRequest {
		t.Fatalf("unknown local start field status = %d, body = %s", response.Code, response.Body.String())
	}
	if response := fixture.request(http.MethodPost, startPath, startBody+` {}`, fixture.userCookie, ""); response.Code != http.StatusBadRequest {
		t.Fatalf("trailing local start status = %d, body = %s", response.Code, response.Body.String())
	}
	oversizedBody := strings.Replace(startBody, "读取当前画布", strings.Repeat("x", agentRuntimeRequestLimit), 1)
	if response := fixture.request(http.MethodPost, startPath, oversizedBody, fixture.userCookie, ""); response.Code != http.StatusBadRequest {
		t.Fatalf("oversized local start status = %d, body = %s", response.Code, response.Body.String())
	}

	startedResponse := fixture.request(http.MethodPost, startPath, startBody, fixture.userCookie, "")
	if startedResponse.Code != http.StatusOK {
		t.Fatalf("local start status = %d, body = %s", startedResponse.Code, startedResponse.Body.String())
	}
	var startedEnvelope struct {
		Data service.AgentRuntimeView `json:"data"`
	}
	if err := json.Unmarshal(startedResponse.Body.Bytes(), &startedEnvelope); err != nil {
		t.Fatal(err)
	}
	started := startedEnvelope.Data
	if started.Run.ID == "" || started.Run.ReasoningHost != agentruntime.ReasoningHostLocalCodex ||
		started.State.Status != agentruntime.RunRunning || started.State.StateVersion != 2 {
		t.Fatalf("local start facts = %#v", started)
	}

	decisionPath := "/api/agent/local/runs/" + started.Run.ID + "/decisions"
	decisionBody := `{"clientRequestId":"local-decision-1","expectedStateVersion":2,"decision":{"kind":"tool_call","toolCall":{"toolCallId":"handler-local-read","toolName":"canvas.read","actionVersion":1,"arguments":{"canvasId":"handler-agent-local-canvas","selectedNodeIds":[],"includeViewport":true},"expectedDelivery":{"kind":"answer","requiredArtifacts":[],"completionCriteria":[{"fact":"final_message"}]}}}}`
	if response := fixture.request(http.MethodPost, decisionPath, strings.TrimSuffix(decisionBody, "}")+`,"unexpected":true}`, fixture.userCookie, ""); response.Code != http.StatusBadRequest {
		t.Fatalf("unknown local decision field status = %d, body = %s", response.Code, response.Body.String())
	}
	if response := fixture.request(http.MethodPost, decisionPath, decisionBody, fixture.adminCookie, ""); response.Code != http.StatusNotFound {
		t.Fatalf("cross-user local decision status = %d, body = %s", response.Code, response.Body.String())
	}
	decidedResponse := fixture.request(http.MethodPost, decisionPath, decisionBody, fixture.userCookie, "")
	if decidedResponse.Code != http.StatusOK {
		t.Fatalf("local decision status = %d, body = %s", decidedResponse.Code, decidedResponse.Body.String())
	}

	staleBody := `{"clientRequestId":"local-decision-stale","expectedStateVersion":2,"decision":{"kind":"final","final":{"message":"不应成功","expectedDelivery":{"kind":"answer","requiredArtifacts":[],"completionCriteria":[{"fact":"final_message"}]}}}}`
	stale := fixture.request(http.MethodPost, decisionPath, staleBody, fixture.userCookie, "")
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), `"latestStateVersion":4`) {
		t.Fatalf("stale local decision status = %d, body = %s", stale.Code, stale.Body.String())
	}
}

func configureAgentRuntimeHandlerDefaultVision(t *testing.T, svc *service.Service, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC()
	channel := model.ModelChannel{
		ID: "handler-agent-vision-channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "Agent Vision",
		BaseURL: "https://api.deepseek.com", APIKey: "handler-vision-secret", APIFormat: "openai",
		InterfaceType: model.ChannelInterfaceChatCompletion, CreatedAt: now, UpdatedAt: now,
	}
	item := model.ChannelModel{
		ID: "handler-agent-vision-model", ChannelID: channel.ID, ModelKey: "deepseek-v4-flash-vision-exp", DisplayName: "DeepSeek Vision",
		AccessPolicy: model.ModelAccessAuthenticated, Capability: "vision", BillingMode: "token_usage", PriceStrategy: "token",
		PriceConfigured: true, Enabled: true, PriceVersion: 3, CreatedAt: now, UpdatedAt: now,
	}
	pricing := model.ModelPricing{
		ID: "handler-agent-vision-pricing", ChannelID: channel.ID, Model: item.ModelKey, Capability: "vision", Currency: "CNY",
		InputPerMillionMicros: 1_000_000, CachedPerMillionMicros: 20_000, OutputPerMillionMicros: 2_000_000,
		MaxOutputTokens: 8_192, CreatedAt: now, UpdatedAt: now,
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
	if _, err := svc.UpdateAgentDefaultVisionModelSetting(&model.User{ID: "handler-admin", Role: model.UserRoleAdmin}, service.UpdateAgentDefaultVisionModelRequest{ChannelModelID: item.ID}); err != nil {
		t.Fatal(err)
	}
}

func TestAgentRuntimeHTTPReadsPersistedRunAndResumesSSEAfterSequence(t *testing.T) {
	fixture := openProviderAccountHandlerFixture(t)
	if err := database.EnsureAgentRuntimeIntegritySchema(fixture.db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(fixture.db)
	svc := service.New(repo, t.TempDir())
	RegisterAgentRuntimeRoutes(fixture.router.Group("/api"), svc)
	createAgentRuntimeHandlerCanvas(t, fixture, "handler-agent-events-canvas")

	threadResponse := fixture.request(http.MethodPost, "/api/agent/threads", `{"canvasId":"handler-agent-events-canvas"}`, fixture.userCookie, "")
	var threadEnvelope struct {
		Data model.AgentThread `json:"data"`
	}
	if err := json.Unmarshal(threadResponse.Body.Bytes(), &threadEnvelope); err != nil {
		t.Fatal(err)
	}
	scope, err := svc.AuthorizeAgentScope(fixture.userID, threadEnvelope.Data.CanvasID, threadEnvelope.Data.ID, "handler-agent-run")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateAgentRun(repository.CreateAgentRunInput{Scope: scope, ClientRequestID: "handler-event-request", Now: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.InitializeAgentRun(repository.InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "frozen-agent-model", ModelKey: "agent-model",
		MaxSteps: 6, ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion: agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion, UserMessage: "读取事件",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	state, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := agentruntime.Fail(state, "test_terminal")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, state, terminal, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	read := fixture.request(http.MethodGet, "/api/agent/runs/"+scope.RunID, "", fixture.userCookie, "")
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"id":"handler-agent-run"`) {
		t.Fatalf("read status = %d, body = %s", read.Code, read.Body.String())
	}
	if response := fixture.request(http.MethodGet, "/api/agent/runs/"+scope.RunID, "", fixture.adminCookie, ""); response.Code != http.StatusNotFound {
		t.Fatalf("cross-user status = %d, body = %s", response.Code, response.Body.String())
	}
	if response := fixture.request(http.MethodGet, "/api/agent/runs/"+scope.RunID+"/events?afterSequence=-1", "", fixture.userCookie, ""); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid cursor status = %d, body = %s", response.Code, response.Body.String())
	}
	future := fixture.request(http.MethodGet, "/api/agent/runs/"+scope.RunID+"/events?afterSequence=4", "", fixture.userCookie, "")
	if future.Code != http.StatusBadRequest || !strings.Contains(future.Body.String(), `"errorCode":"agent_stream_cursor_invalid"`) {
		t.Fatalf("future cursor status = %d, body = %s", future.Code, future.Body.String())
	}

	events := fixture.request(http.MethodGet, "/api/agent/runs/"+scope.RunID+"/events?afterSequence=0", "", fixture.userCookie, "")
	if events.Code != http.StatusOK {
		t.Fatalf("events status = %d, body = %s", events.Code, events.Body.String())
	}
	if contentType := events.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("events content type = %q", contentType)
	}
	if body := events.Body.String(); !strings.Contains(body, "id: 1\n") || !strings.Contains(body, "event: run.started\n") ||
		!strings.Contains(body, `"protocolVersion":`+strconv.Itoa(agentruntime.CloudAgentUIProtocolVersion)) || !strings.Contains(body, `"threadId":"`+scope.ThreadID+`"`) ||
		!strings.Contains(body, `"runId":"`+scope.RunID+`"`) || !strings.Contains(body, `"sequence":1`) {
		t.Fatalf("events body = %s", body)
	}

	after := fixture.request(http.MethodGet, "/api/agent/runs/"+scope.RunID+"/events?afterSequence="+strconv.FormatInt(1, 10), "", fixture.userCookie, "")
	if after.Code != http.StatusOK || strings.Contains(after.Body.String(), "id: 1\n") {
		t.Fatalf("resumed events status = %d, body = %s", after.Code, after.Body.String())
	}
}

func TestAgentApprovalRejectsMalformedIdentityBeforeRunLookup(t *testing.T) {
	fixture := openProviderAccountHandlerFixture(t)
	if err := database.EnsureAgentRuntimeIntegritySchema(fixture.db); err != nil {
		t.Fatal(err)
	}
	RegisterAgentRuntimeRoutes(fixture.router.Group("/api"), service.New(repository.New(fixture.db), t.TempDir()))

	for _, test := range []struct {
		name string
		body string
	}{
		{name: "empty tool call", body: `{"toolCallId":"","actionVersion":1,"proposalHash":"` + strings.Repeat("a", 64) + `","decision":"approved"}`},
		{name: "invalid action version", body: `{"toolCallId":"call-1","actionVersion":0,"proposalHash":"` + strings.Repeat("a", 64) + `","decision":"approved"}`},
		{name: "invalid proposal hash", body: `{"toolCallId":"call-1","actionVersion":1,"proposalHash":"ABC","decision":"approved"}`},
		{name: "invalid decision", body: `{"toolCallId":"call-1","actionVersion":1,"proposalHash":"` + strings.Repeat("a", 64) + `","decision":"allow"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := fixture.request(http.MethodPost, "/api/agent/runs/missing-run/approvals", test.body, fixture.userCookie, "")
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"errorCode":"agent_approval_invalid"`) {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestAgentEventV5DoesNotRegisterRetiredProductionGraphRoutes(t *testing.T) {
	fixture := openProviderAccountHandlerFixture(t)
	RegisterAgentRuntimeRoutes(fixture.router.Group("/api"), service.New(repository.New(fixture.db), t.TempDir()))

	for _, request := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPost, path: "/api/agent/runs/run-1/stages/stage-1/reviews", body: `{}`},
		{method: http.MethodGet, path: "/api/agent/runs/run-1/artifacts/artifact-1/revisions/revision-1"},
	} {
		response := fixture.request(request.method, request.path, request.body, fixture.userCookie, "")
		if response.Code != http.StatusNotFound || strings.TrimSpace(response.Body.String()) != "404 page not found" {
			t.Fatalf("retired route %s %s returned status=%d body=%s", request.method, request.path, response.Code, response.Body.String())
		}
	}
}

func TestAgentRuntimeHTTPReplaysCompletedToolLifecycleFromEarlierCursor(t *testing.T) {
	fixture := openProviderAccountHandlerFixture(t)
	if err := database.EnsureAgentRuntimeIntegritySchema(fixture.db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(fixture.db)
	svc := service.New(repo, t.TempDir())
	RegisterAgentRuntimeRoutes(fixture.router.Group("/api"), svc)
	createAgentRuntimeHandlerCanvas(t, fixture, "handler-agent-tool-replay-canvas")
	scope, err := svc.AuthorizeAgentScope(
		fixture.userID, "handler-agent-tool-replay-canvas", "handler-tool-replay-thread", "handler-tool-replay-run",
	)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := repo.CreateAgentRun(repository.CreateAgentRunInput{Scope: scope, ClientRequestID: "handler-tool-replay-request", Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.InitializeAgentRun(repository.InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "handler-agent-model", ModelKey: "agent-model", MaxSteps: 6,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion:    agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion, UserMessage: "读取并更新画布",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}

	current, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	requested, err := agentruntime.Advance(current, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionToolCall,
		ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: "handler-tool-replay-call", ToolName: agentruntime.ToolCanvasRead,
			ActionVersion: 1, Arguments: json.RawMessage(`{"canvasId":"handler-agent-tool-replay-canvas","selectedNodeIds":[],"includeViewport":true}`),
			ExpectedDelivery: agentruntime.ExpectedDelivery{
				Kind:               agentruntime.DeliveryAnswer,
				CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}},
			},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, current, requested, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	resolved, err := agentruntime.ResolveTool(requested.State, agentruntime.ToolResolution{
		ToolCallID: "handler-tool-replay-call", ActionVersion: 1, Succeeded: true,
		Output: json.RawMessage(`{"canvasId":"handler-agent-tool-replay-canvas","revision":13}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, requested.State, resolved, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	terminal, err := agentruntime.Fail(resolved.State, "handler_tool_replay_complete")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, resolved.State, terminal, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	replayed := fixture.request(http.MethodGet, "/api/agent/runs/"+scope.RunID+"/events?afterSequence=2", "", fixture.userCookie, "")
	if replayed.Code != http.StatusOK {
		t.Fatalf("tool lifecycle replay status = %d, body = %s", replayed.Code, replayed.Body.String())
	}
	body := replayed.Body.String()
	if !strings.Contains(body, "id: 3\n") || !strings.Contains(body, "event: item.started\n") ||
		!strings.Contains(body, `"toolCallId":"handler-tool-replay-call"`) ||
		!strings.Contains(body, "id: 5\n") || !strings.Contains(body, "event: item.completed\n") ||
		!strings.Contains(body, `"succeeded":true`) {
		t.Fatalf("tool lifecycle replay body = %s", body)
	}
}

func TestAgentRuntimeHTTPStrictSteerAndInterruptContracts(t *testing.T) {
	fixture := openProviderAccountHandlerFixture(t)
	if err := database.EnsureAgentRuntimeIntegritySchema(fixture.db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(fixture.db)
	svc := service.New(repo, t.TempDir())
	RegisterAgentRuntimeRoutes(fixture.router.Group("/api"), svc)
	createAgentRuntimeHandlerCanvas(t, fixture, "handler-agent-control-canvas")
	scope := createAgentRuntimeHistoryRun(t, svc, repo, fixture.userID, "handler-agent-control-canvas", "control-thread", "control-run", "创建一段短片", time.Now().UTC())
	steerPath := "/api/agent/runs/" + scope.RunID + "/steer"
	interruptPath := "/api/agent/runs/" + scope.RunID + "/interrupt"

	if response := fixture.request(http.MethodPost, steerPath, `{"clientRequestId":"steer-1","message":"补充夜景","expectedStateVersion":1,"extra":true}`, fixture.userCookie, ""); response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"errorCode":"agent_steer_conflict"`) {
		t.Fatalf("unknown steer field status = %d, body = %s", response.Code, response.Body.String())
	}
	overlongBody, err := json.Marshal(steerAgentRunRequest{
		ClientRequestID: "steer-long", Message: strings.Repeat("a", 64*1024+1), ExpectedStateVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response := fixture.request(http.MethodPost, steerPath, string(overlongBody), fixture.userCookie, ""); response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"errorCode":"agent_steer_conflict"`) {
		t.Fatalf("overlong steer status = %d, body = %s", response.Code, response.Body.String())
	}
	steerBody := `{"clientRequestId":"steer-1","message":"补充夜景","expectedStateVersion":1}`
	steered := fixture.request(http.MethodPost, steerPath, steerBody, fixture.userCookie, "")
	if steered.Code != http.StatusOK || !strings.Contains(steered.Body.String(), `"stateVersion":2`) || !strings.Contains(steered.Body.String(), `"clientRequestId":"steer-1"`) {
		t.Fatalf("steer status = %d, body = %s", steered.Code, steered.Body.String())
	}
	var eventCount int64
	if err := fixture.db.Model(&model.AgentRunEvent{}).Where("run_id = ?", scope.RunID).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 3 {
		t.Fatalf("event count after steer = %d", eventCount)
	}
	replayed := fixture.request(http.MethodPost, steerPath, steerBody, fixture.userCookie, "")
	if replayed.Code != http.StatusOK {
		t.Fatalf("steer replay status = %d, body = %s", replayed.Code, replayed.Body.String())
	}
	if err := fixture.db.Model(&model.AgentRunEvent{}).Where("run_id = ?", scope.RunID).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 3 {
		t.Fatalf("steer replay created events = %d", eventCount)
	}
	conflictBody := `{"clientRequestId":"steer-1","message":"改成白天","expectedStateVersion":1}`
	if response := fixture.request(http.MethodPost, steerPath, conflictBody, fixture.userCookie, ""); response.Code != http.StatusConflict ||
		!strings.Contains(response.Body.String(), `"errorCode":"agent_steer_conflict"`) || !strings.Contains(response.Body.String(), `"latestStateVersion":2`) {
		t.Fatalf("steer conflict status = %d, body = %s", response.Code, response.Body.String())
	}
	if response := fixture.request(http.MethodPost, steerPath, steerBody, fixture.adminCookie, ""); response.Code != http.StatusNotFound {
		t.Fatalf("cross-user steer status = %d, body = %s", response.Code, response.Body.String())
	}

	if response := fixture.request(http.MethodPost, interruptPath, `{"expectedStateVersion":2,"extra":true}`, fixture.userCookie, ""); response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"errorCode":"agent_interrupt_conflict"`) {
		t.Fatalf("unknown interrupt field status = %d, body = %s", response.Code, response.Body.String())
	}
	if response := fixture.request(http.MethodPost, interruptPath, `{"expectedStateVersion":1}`, fixture.userCookie, ""); response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"latestStateVersion":2`) {
		t.Fatalf("stale interrupt status = %d, body = %s", response.Code, response.Body.String())
	}
	interrupted := fixture.request(http.MethodPost, interruptPath, `{"expectedStateVersion":2}`, fixture.userCookie, "")
	if interrupted.Code != http.StatusOK || !strings.Contains(interrupted.Body.String(), `"stateVersion":3`) || !strings.Contains(interrupted.Body.String(), `"status":"cancelled"`) {
		t.Fatalf("interrupt status = %d, body = %s", interrupted.Code, interrupted.Body.String())
	}
	terminalEvents := fixture.request(http.MethodGet, "/api/agent/runs/"+scope.RunID+"/events?afterSequence=3", "", fixture.userCookie, "")
	if terminalEvents.Code != http.StatusOK || !strings.Contains(terminalEvents.Body.String(), "id: 4\n") ||
		!strings.Contains(terminalEvents.Body.String(), "event: run.interrupted\n") ||
		!strings.Contains(terminalEvents.Body.String(), `"itemId":`) ||
		!strings.Contains(terminalEvents.Body.String(), `"status":"interrupted"`) {
		t.Fatalf("terminal events status = %d, body = %s", terminalEvents.Code, terminalEvents.Body.String())
	}
	terminalResume := fixture.request(http.MethodGet, "/api/agent/runs/"+scope.RunID+"/events?afterSequence=4", "", fixture.userCookie, "")
	if terminalResume.Code != http.StatusOK || strings.Contains(terminalResume.Body.String(), "id:") {
		t.Fatalf("terminal resume status = %d, body = %s", terminalResume.Code, terminalResume.Body.String())
	}
	if response := fixture.request(http.MethodPost, interruptPath, `{"expectedStateVersion":3}`, fixture.userCookie, ""); response.Code != http.StatusConflict ||
		!strings.Contains(response.Body.String(), `"errorCode":"agent_interrupt_conflict"`) || !strings.Contains(response.Body.String(), `"latestStateVersion":3`) {
		t.Fatalf("terminal interrupt status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestAgentRuntimeHTTPListsOnlyCurrentCanvasActorThreadsByActivity(t *testing.T) {
	fixture := openProviderAccountHandlerFixture(t)
	if err := database.EnsureAgentRuntimeIntegritySchema(fixture.db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(fixture.db)
	svc := service.New(repo, t.TempDir())
	RegisterAgentRuntimeRoutes(fixture.router.Group("/api"), svc)
	createAgentRuntimeHandlerCanvas(t, fixture, "handler-agent-history-canvas")
	createAgentRuntimeHandlerCanvas(t, fixture, "handler-agent-other-canvas")

	baseTime := time.Date(2026, time.August, 15, 1, 0, 0, 0, time.UTC)
	olderScope := createAgentRuntimeHistoryRun(t, svc, repo, fixture.userID, "handler-agent-history-canvas", "history-thread-older", "history-run-older", "较早的任务", baseTime.Add(time.Hour))
	newerScope := createAgentRuntimeHistoryRun(t, svc, repo, fixture.userID, "handler-agent-history-canvas", "history-thread-newer", "history-run-newer", "较新的任务", baseTime.Add(2*time.Hour))
	continuedScope := createAgentRuntimeHistoryRun(t, svc, repo, fixture.userID, "handler-agent-history-canvas", newerScope.ThreadID, "history-run-newer-continued", "继续完成任务", baseTime.Add(3*time.Hour))

	emptyScope, err := svc.AuthorizeAgentScope(fixture.userID, "handler-agent-history-canvas", "history-thread-empty", "history-empty-probe")
	if err != nil {
		t.Fatal(err)
	}
	emptyThread, err := repo.CreateAgentThread(emptyScope, baseTime)
	if err != nil {
		t.Fatal(err)
	}
	createAgentRuntimeHistoryRun(t, svc, repo, fixture.userID, "handler-agent-other-canvas", "history-thread-other-canvas", "history-run-other-canvas", "其他画布任务", baseTime.Add(3*time.Hour))
	if err := fixture.db.Create(&model.AgentThread{
		ID: "history-thread-other-user", TenantKind: agentruntime.TenantPersonal, TenantID: "handler-admin",
		CreatedByUserID: "handler-admin", CanvasID: "handler-agent-history-canvas", Status: agentruntime.ThreadActive,
		CreatedAt: baseTime.Add(4 * time.Hour), UpdatedAt: baseTime.Add(4 * time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Create(&model.AgentThread{
		ID: "history-thread-archived", TenantKind: agentruntime.TenantPersonal, TenantID: fixture.userID,
		CreatedByUserID: fixture.userID, CanvasID: "handler-agent-history-canvas", Status: agentruntime.ThreadArchived,
		CreatedAt: baseTime.Add(5 * time.Hour), UpdatedAt: baseTime.Add(5 * time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}

	response := fixture.request(http.MethodGet, "/api/agent/threads?canvasId=handler-agent-history-canvas&limit=20", "", fixture.userCookie, "")
	if response.Code != http.StatusOK {
		t.Fatalf("history status = %d, body = %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data struct {
			Items []struct {
				Thread     model.AgentThread
				ActivityAt time.Time
				Turns      []struct {
					Run   model.AgentRun
					Items []struct {
						Kind    model.AgentTimelineItemKind
						Ordinal int64
						Content json.RawMessage
					}
				}
			}
		}
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data.Items) != 3 {
		t.Fatalf("history item count = %d, body = %s", len(envelope.Data.Items), response.Body.String())
	}
	if strings.Contains(response.Body.String(), `"tenantId"`) || strings.Contains(response.Body.String(), `"createdByUserId"`) ||
		strings.Contains(response.Body.String(), `"domainProjectId"`) || strings.Contains(response.Body.String(), `"actorUserId"`) ||
		strings.Contains(response.Body.String(), `"latestRun"`) {
		t.Fatalf("history response exposed internal scope facts: %s", response.Body.String())
	}
	if first := envelope.Data.Items[0]; first.Thread.ID != newerScope.ThreadID || len(first.Turns) != 2 ||
		len(first.Turns[0].Items) != 1 || len(first.Turns[1].Items) != 1 ||
		first.Turns[0].Run.ID != newerScope.RunID || !strings.Contains(string(first.Turns[0].Items[0].Content), "较新的任务") ||
		first.Turns[1].Run.ID != continuedScope.RunID || !strings.Contains(string(first.Turns[1].Items[0].Content), "继续完成任务") ||
		first.Turns[0].Items[0].Kind != model.AgentTimelineItemUserMessage ||
		first.Turns[0].Items[0].Ordinal != 1 {
		t.Fatalf("newest item = %#v", first)
	}
	if second := envelope.Data.Items[1]; second.Thread.ID != olderScope.ThreadID || len(second.Turns) != 1 || len(second.Turns[0].Items) != 1 ||
		second.Turns[0].Run.ID != olderScope.RunID || !strings.Contains(string(second.Turns[0].Items[0].Content), "较早的任务") {
		t.Fatalf("older item = %#v", second)
	}
	empty := envelope.Data.Items[2]
	if empty.Thread.ID != emptyThread.ID || len(empty.Turns) != 0 || !empty.ActivityAt.Equal(empty.Thread.UpdatedAt) {
		t.Fatalf("empty item = %#v", empty)
	}
	limited := fixture.request(http.MethodGet, "/api/agent/threads?canvasId=handler-agent-history-canvas&limit=1", "", fixture.userCookie, "")
	if limited.Code != http.StatusOK {
		t.Fatalf("limited history status = %d, body = %s", limited.Code, limited.Body.String())
	}
	var limitedEnvelope struct {
		Data struct {
			Items []json.RawMessage
		}
	}
	if err := json.Unmarshal(limited.Body.Bytes(), &limitedEnvelope); err != nil {
		t.Fatal(err)
	}
	if len(limitedEnvelope.Data.Items) != 1 {
		t.Fatalf("limited history item count = %d, body = %s", len(limitedEnvelope.Data.Items), limited.Body.String())
	}

	for _, test := range []struct {
		name string
		path string
	}{
		{name: "missing canvas", path: "/api/agent/threads?limit=20"},
		{name: "zero limit", path: "/api/agent/threads?canvasId=handler-agent-history-canvas&limit=0"},
		{name: "limit too large", path: "/api/agent/threads?canvasId=handler-agent-history-canvas&limit=21"},
		{name: "decimal limit", path: "/api/agent/threads?canvasId=handler-agent-history-canvas&limit=1.5"},
		{name: "signed limit", path: "/api/agent/threads?canvasId=handler-agent-history-canvas&limit=%2B1"},
		{name: "trailing limit", path: "/api/agent/threads?canvasId=handler-agent-history-canvas&limit=1x"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := fixture.request(http.MethodGet, test.path, "", fixture.userCookie, "")
			if got.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", got.Code, got.Body.String())
			}
		})
	}
	if anonymous := fixture.request(http.MethodGet, "/api/agent/threads?canvasId=handler-agent-history-canvas", "", "", ""); anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, body = %s", anonymous.Code, anonymous.Body.String())
	}
}

func TestAgentRuntimeHTTPClarificationResponseIsScopedStrictAndIdempotent(t *testing.T) {
	fixture := openProviderAccountHandlerFixture(t)
	if err := database.EnsureAgentRuntimeIntegritySchema(fixture.db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(fixture.db)
	svc := service.New(repo, t.TempDir())
	RegisterAgentRuntimeRoutes(fixture.router.Group("/api"), svc)
	createAgentRuntimeHandlerCanvas(t, fixture, "handler-agent-clarification-canvas")
	scope := createWaitingClarificationHandlerRun(t, svc, repo, fixture.userID)
	path := "/api/agent/runs/" + scope.RunID + "/clarifications/clarify-handler/responses"
	body := `{"expectedStateVersion":2,"questionId":"duration","answer":{"selectedOptionIds":["30s"],"customText":"","skipped":false},"complete":false}`

	if response := fixture.request(http.MethodPost, path, body, "", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, body = %s", response.Code, response.Body.String())
	}
	if response := fixture.request(http.MethodPost, path, body, fixture.adminCookie, ""); response.Code != http.StatusNotFound {
		t.Fatalf("cross-user status = %d, body = %s", response.Code, response.Body.String())
	}
	if response := fixture.request(http.MethodPost, path, strings.TrimSuffix(body, "}")+`,"extra":true}`, fixture.userCookie, ""); response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"errorCode":"agent_clarification_invalid"`) {
		t.Fatalf("unknown-field status = %d, body = %s", response.Code, response.Body.String())
	}
	if response := fixture.request(http.MethodPost, strings.Replace(path, "clarify-handler", "wrong-request", 1), body, fixture.userCookie, ""); response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"errorCode":"agent_clarification_conflict"`) {
		t.Fatalf("wrong-request status = %d, body = %s", response.Code, response.Body.String())
	}
	invalidQuestion := strings.Replace(body, `"questionId":"duration"`, `"questionId":"unknown"`, 1)
	if response := fixture.request(http.MethodPost, path, invalidQuestion, fixture.userCookie, ""); response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"errorCode":"agent_clarification_invalid"`) {
		t.Fatalf("invalid-question status = %d, body = %s", response.Code, response.Body.String())
	}
	stale := strings.Replace(body, `"expectedStateVersion":2`, `"expectedStateVersion":1`, 1)
	if response := fixture.request(http.MethodPost, path, stale, fixture.userCookie, ""); response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"latestStateVersion":2`) {
		t.Fatalf("stale status = %d, body = %s", response.Code, response.Body.String())
	}

	saved := fixture.request(http.MethodPost, path, body, fixture.userCookie, "")
	if saved.Code != http.StatusOK || !strings.Contains(saved.Body.String(), `"stateVersion":3`) || !strings.Contains(saved.Body.String(), `"duration"`) {
		t.Fatalf("saved status = %d, body = %s", saved.Code, saved.Body.String())
	}
	var eventCount int64
	if err := fixture.db.Model(&model.AgentRunEvent{}).Where("run_id = ?", scope.RunID).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	replayed := fixture.request(http.MethodPost, path, body, fixture.userCookie, "")
	if replayed.Code != http.StatusOK || !strings.Contains(replayed.Body.String(), `"stateVersion":3`) {
		t.Fatalf("replayed status = %d, body = %s", replayed.Code, replayed.Body.String())
	}
	var replayedEventCount int64
	if err := fixture.db.Model(&model.AgentRunEvent{}).Where("run_id = ?", scope.RunID).Count(&replayedEventCount).Error; err != nil {
		t.Fatal(err)
	}
	if replayedEventCount != eventCount {
		t.Fatalf("idempotent replay changed events: before=%d after=%d", eventCount, replayedEventCount)
	}
}

func createWaitingClarificationHandlerRun(t *testing.T, svc *service.Service, repo *repository.Repository, actorUserID string) agentruntime.Scope {
	t.Helper()
	scope, err := svc.AuthorizeAgentScope(actorUserID, "handler-agent-clarification-canvas", "handler-clarification-thread", "handler-clarification-run")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := repo.CreateAgentRun(repository.CreateAgentRunInput{Scope: scope, ClientRequestID: "handler-clarification-request", Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.InitializeAgentRun(repository.InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "handler-agent-model", ModelKey: "agent-model", MaxSteps: 6,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion:    agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion, UserMessage: "生成汽车广告剧本",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	state, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	delivery := agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryAnswer,
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}},
	}
	transition, err := agentruntime.Advance(state, agentruntime.RuntimeInput{Decision: agentruntime.ModelDecision{
		Kind: agentruntime.DecisionClarificationRequest,
		Clarification: &agentruntime.ClarificationDecision{
			RequestID: "clarify-handler",
			Questions: []agentruntime.ClarificationQuestion{{
				ID: "duration", Prompt: "广告时长是多少？", Type: agentruntime.ClarificationSingleChoice,
				Options: []agentruntime.ClarificationOption{{ID: "15s", Label: "15 秒"}, {ID: "30s", Label: "30 秒"}},
			}},
			ExpectedDelivery: delivery,
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, state, transition, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	return scope
}

func createAgentRuntimeHistoryRun(t *testing.T, svc *service.Service, repo *repository.Repository, actorUserID, canvasID, threadID, runID, userMessage string, now time.Time) agentruntime.Scope {
	t.Helper()
	scope, err := svc.AuthorizeAgentScope(actorUserID, canvasID, threadID, runID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateAgentRun(repository.CreateAgentRunInput{Scope: scope, ClientRequestID: "request-" + runID, Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.InitializeAgentRun(repository.InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "history-model-record", ModelKey: "history-model",
		MaxSteps: 8, ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion: agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion, UserMessage: userMessage,
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	return scope
}

func createAgentRuntimeHandlerCanvas(t *testing.T, fixture *providerAccountHandlerFixture, canvasID string) {
	t.Helper()
	now := time.Now().UTC()
	canvas := model.CanvasProject{
		ID: canvasID, UserID: fixture.userID, ProjectID: "", Title: "Agent Canvas", PayloadJSON: `{}`,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := fixture.db.Create(&canvas).Error; err != nil {
		t.Fatal(err)
	}
}
