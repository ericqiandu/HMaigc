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
)

func TestAgentRuntimeHTTPCreatesScopedThreadAndRejectsMalformedRequests(t *testing.T) {
	fixture := openProviderAccountHandlerFixture(t)
	if err := database.EnsureAgentRuntimeIntegritySchema(fixture.db); err != nil {
		t.Fatal(err)
	}
	svc := service.New(repository.New(fixture.db), t.TempDir())
	RegisterAgentRuntimeRoutes(fixture.router.Group("/api"), svc)
	createAgentRuntimeHandlerCanvas(t, fixture, "handler-agent-canvas")

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
		MaxSteps: 6, ToolSchemaVersion: 2, RuntimeVersion: 1, PolicyVersion: 1, UserMessage: "读取事件",
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

	events := fixture.request(http.MethodGet, "/api/agent/runs/"+scope.RunID+"/events?afterSequence=0", "", fixture.userCookie, "")
	if events.Code != http.StatusOK {
		t.Fatalf("events status = %d, body = %s", events.Code, events.Body.String())
	}
	if contentType := events.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("events content type = %q", contentType)
	}
	if body := events.Body.String(); !strings.Contains(body, "id: 1\n") || !strings.Contains(body, "event: run.created\n") || !strings.Contains(body, `"sequence":1`) {
		t.Fatalf("events body = %s", body)
	}

	after := fixture.request(http.MethodGet, "/api/agent/runs/"+scope.RunID+"/events?afterSequence="+strconv.FormatInt(1, 10), "", fixture.userCookie, "")
	if after.Code != http.StatusOK || strings.Contains(after.Body.String(), "id: 1\n") {
		t.Fatalf("resumed events status = %d, body = %s", after.Code, after.Body.String())
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
				LatestRun  *struct {
					Run   model.AgentRun
					State agentruntime.RuntimeState
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
	if strings.Contains(response.Body.String(), `"tenantId"`) || strings.Contains(response.Body.String(), `"createdByUserId"`) || strings.Contains(response.Body.String(), `"domainProjectId"`) {
		t.Fatalf("history response exposed internal scope facts: %s", response.Body.String())
	}
	if first := envelope.Data.Items[0]; first.Thread.ID != newerScope.ThreadID || first.LatestRun == nil || first.LatestRun.Run.ID != newerScope.RunID || first.LatestRun.State.UserMessage != "较新的任务" {
		t.Fatalf("newest item = %#v", first)
	}
	if second := envelope.Data.Items[1]; second.Thread.ID != olderScope.ThreadID || second.LatestRun == nil || second.LatestRun.Run.ID != olderScope.RunID || second.LatestRun.State.UserMessage != "较早的任务" {
		t.Fatalf("older item = %#v", second)
	}
	empty := envelope.Data.Items[2]
	if empty.Thread.ID != emptyThread.ID || empty.LatestRun != nil || !empty.ActivityAt.Equal(empty.Thread.UpdatedAt) {
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
		ToolSchemaVersion: 2, RuntimeVersion: 2, PolicyVersion: 1, UserMessage: "生成汽车广告剧本",
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
		MaxSteps: 8, ToolSchemaVersion: 2, RuntimeVersion: 1, PolicyVersion: 1, UserMessage: userMessage,
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
