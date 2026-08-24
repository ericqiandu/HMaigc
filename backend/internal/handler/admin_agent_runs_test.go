package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/service"
)

type adminAgentRunHandlerEnvelope struct {
	Code int             `json:"code"`
	Data json.RawMessage `json:"data"`
	Msg  string          `json:"msg"`
}

type adminAgentRunHandlerErrorData struct {
	ErrorCode string                          `json:"errorCode"`
	Latest    *repository.AdminAgentRunRecord `json:"latestRun"`
}

func TestAdminAgentRunRoutesEnforceAdminAndStableHTTPContracts(t *testing.T) {
	fixture := openProviderAccountHandlerFixture(t)
	if err := database.EnsureAgentRuntimeIntegritySchema(fixture.db); err != nil {
		t.Fatal(err)
	}
	svc := service.New(repository.New(fixture.db), t.TempDir())
	RegisterAdminAgentRunRoutes(fixture.router.Group("/api"), svc)
	now := time.Now().UTC().Add(-time.Minute)
	createAdminAgentRunHandlerFixture(t, fixture, "run-handler-control", agentruntime.RunQueued, now)

	if response := fixture.request(http.MethodGet, "/api/admin/agent-runs", "", "", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous list status=%d body=%s", response.Code, response.Body.String())
	}
	if response := fixture.request(http.MethodGet, "/api/admin/agent-runs", "", fixture.userCookie, ""); response.Code != http.StatusForbidden {
		t.Fatalf("ordinary list status=%d body=%s", response.Code, response.Body.String())
	}
	if response := fixture.request(http.MethodPost, "/api/admin/agent-runs/run-handler-control/interrupt", `{"expectedStateVersion":4,"reason":"普通用户不得终止","confirmation":"STOP run-hand"}`, fixture.userCookie, ""); response.Code != http.StatusForbidden {
		t.Fatalf("ordinary interrupt status=%d body=%s", response.Code, response.Body.String())
	}
	if response := fixture.request(http.MethodPost, "/api/admin/agent-runs/run-handler-control/interrupt", `{`, fixture.userCookie, ""); response.Code != http.StatusForbidden {
		t.Fatalf("ordinary malformed interrupt status=%d body=%s", response.Code, response.Body.String())
	}
	if response := fixture.request(http.MethodGet, "/api/admin/agent-runs?page=1&pageSize=21", "", fixture.adminCookie, ""); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid page size status=%d body=%s", response.Code, response.Body.String())
	}

	list := fixture.request(http.MethodGet, "/api/admin/agent-runs?status=queued&page=1&pageSize=20", "", fixture.adminCookie, "")
	if list.Code != http.StatusOK {
		t.Fatalf("admin list status=%d body=%s", list.Code, list.Body.String())
	}
	var listEnvelope adminAgentRunHandlerEnvelope
	if err := json.Unmarshal(list.Body.Bytes(), &listEnvelope); err != nil {
		t.Fatal(err)
	}
	var page repository.AdminAgentRunPage
	if err := json.Unmarshal(listEnvelope.Data, &page); err != nil {
		t.Fatal(err)
	}
	if listEnvelope.Code != 0 || page.Total != 1 || len(page.Items) != 1 || page.Items[0].RunID != "run-handler-control" {
		t.Fatalf("admin list envelope=%#v page=%#v", listEnvelope, page)
	}

	detailResponse := fixture.request(http.MethodGet, "/api/admin/agent-runs/run-handler-control", "", fixture.adminCookie, "")
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", detailResponse.Code, detailResponse.Body.String())
	}
	var detailEnvelope adminAgentRunHandlerEnvelope
	if err := json.Unmarshal(detailResponse.Body.Bytes(), &detailEnvelope); err != nil {
		t.Fatal(err)
	}
	var detail repository.AdminAgentRunRecord
	if err := json.Unmarshal(detailEnvelope.Data, &detail); err != nil {
		t.Fatal(err)
	}
	if detail.ConfirmationPhrase != "STOP run-hand" || detail.StateVersion != 4 {
		t.Fatalf("detail = %#v", detail)
	}

	unknown := fixture.request(http.MethodPost, "/api/admin/agent-runs/run-handler-control/interrupt", `{"expectedStateVersion":4,"reason":"终止无人处理的运行","confirmation":"STOP run-hand","extra":true}`, fixture.adminCookie, "")
	assertAdminAgentRunHandlerError(t, unknown.Code, unknown.Body.Bytes(), http.StatusBadRequest, "admin_agent_run_interrupt_blocked", false)
	oversized := fixture.request(http.MethodPost, "/api/admin/agent-runs/run-handler-control/interrupt", `{"expectedStateVersion":4,"reason":"终止无人处理的运行","confirmation":"STOP run-hand","padding":"`+strings.Repeat("x", (8<<10)+1)+`"}`, fixture.adminCookie, "")
	assertAdminAgentRunHandlerError(t, oversized.Code, oversized.Body.Bytes(), http.StatusBadRequest, "admin_agent_run_interrupt_blocked", false)
	wrongConfirmation := fixture.request(http.MethodPost, "/api/admin/agent-runs/run-handler-control/interrupt", `{"expectedStateVersion":4,"reason":"终止无人处理的运行","confirmation":"STOP wrong"}`, fixture.adminCookie, "")
	assertAdminAgentRunHandlerError(t, wrongConfirmation.Code, wrongConfirmation.Body.Bytes(), http.StatusBadRequest, "admin_agent_run_confirmation_invalid", true)
	stale := fixture.request(http.MethodPost, "/api/admin/agent-runs/run-handler-control/interrupt", `{"expectedStateVersion":3,"reason":"终止无人处理的运行","confirmation":"STOP run-hand"}`, fixture.adminCookie, "")
	assertAdminAgentRunHandlerError(t, stale.Code, stale.Body.Bytes(), http.StatusConflict, "admin_agent_run_state_conflict", true)

	success := fixture.request(http.MethodPost, "/api/admin/agent-runs/run-handler-control/interrupt", `{"expectedStateVersion":4,"reason":"终止无人处理的运行","confirmation":"STOP run-hand"}`, fixture.adminCookie, "")
	if success.Code != http.StatusOK {
		t.Fatalf("interrupt status=%d body=%s", success.Code, success.Body.String())
	}
	var successEnvelope adminAgentRunHandlerEnvelope
	if err := json.Unmarshal(success.Body.Bytes(), &successEnvelope); err != nil {
		t.Fatal(err)
	}
	var interrupted service.AdminAgentRunInterruptResponse
	if err := json.Unmarshal(successEnvelope.Data, &interrupted); err != nil {
		t.Fatal(err)
	}
	if interrupted.Run.Status != agentruntime.RunCancelled || interrupted.Run.StateVersion != 5 {
		t.Fatalf("interrupt response = %#v", interrupted)
	}
}

func assertAdminAgentRunHandlerError(t *testing.T, actualStatus int, body []byte, wantStatus int, wantCode string, wantLatest bool) {
	t.Helper()
	if actualStatus != wantStatus {
		t.Fatalf("error status=%d want=%d body=%s", actualStatus, wantStatus, string(body))
	}
	var envelope adminAgentRunHandlerEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	var data adminAgentRunHandlerErrorData
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != wantStatus || data.ErrorCode != wantCode || (data.Latest != nil) != wantLatest {
		t.Fatalf("error envelope=%#v data=%#v", envelope, data)
	}
}

func createAdminAgentRunHandlerFixture(t *testing.T, fixture *providerAccountHandlerFixture, runID string, status agentruntime.RunStatus, now time.Time) {
	t.Helper()
	thread := model.AgentThread{
		ID: "thread-" + runID, TenantKind: agentruntime.TenantPersonal, TenantID: fixture.userID,
		CreatedByUserID: fixture.userID, DomainProjectID: "project-" + runID, CanvasID: "canvas-" + runID,
		Status: agentruntime.ThreadActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := fixture.db.Create(&thread).Error; err != nil {
		t.Fatal(err)
	}
	state := agentruntime.RuntimeState{
		StateVersion: 4, StepNumber: 2, MaxSteps: 24, Status: status,
		UserMessage: "管理员 HTTP 终止测试", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic},
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	run := model.AgentRun{
		ID: runID, ThreadID: thread.ID, ActorUserID: fixture.userID, ClientRequestID: "request-" + runID,
		Status: status, LastEventSequence: 7, StateVersion: state.StateVersion, StepNumber: state.StepNumber, MaxSteps: state.MaxSteps,
		ModelRecordID: "model-record", ModelKey: "agent-model", ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion: agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := fixture.db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	checkpoint := model.AgentCheckpoint{
		ID: "checkpoint-" + runID, RunID: runID, Sequence: run.LastEventSequence,
		StateVersion: state.StateVersion, StateJSON: string(stateJSON), CreatedAt: now,
	}
	if err := fixture.db.Create(&checkpoint).Error; err != nil {
		t.Fatal(err)
	}
}
