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

	failedRun := fixture.request(http.MethodPost, "/api/agent/threads/"+envelope.Data.ID+"/runs", `{"clientRequestId":"request-1","userMessage":"生成一张图片","maxSteps":6}`, fixture.userCookie, "")
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
		MaxSteps: 6, ToolSchemaVersion: 1, UserMessage: "读取事件", Now: time.Now().UTC(),
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
