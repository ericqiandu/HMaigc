package service

import (
	"context"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

func TestAgentThreadHistoryReconcilesAllTerminalCanvasPlaceholdersOnce(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	now := time.Now().UTC()
	configuration, err := svc.resolveAgentRuntimeConfiguration(context.Background(), agentRuntimeServiceScope(), guidedAgentRuntimeConfigurationInput())
	if err != nil {
		t.Fatal(err)
	}
	firstScope := agentRuntimeServiceScope()
	secondScope := firstScope
	secondScope.ThreadID = "runtime-thread-2"
	secondScope.RunID = "runtime-run-2"
	for index, scope := range []agentruntime.Scope{firstScope, secondScope} {
		if _, err := svc.repo.CreateInitializedAgentRun(repository.CreateInitializedAgentRunInput{
			Create: repository.CreateAgentRunInput{Scope: scope, ClientRequestID: "history-terminal-run-" + scope.RunID, Now: now.Add(time.Duration(index) * time.Second)},
			Initialize: repository.InitializeAgentRunInput{
				Scope: scope, ModelRecordID: "runtime-agent-model", ModelKey: "gpt-5.5", MaxSteps: 8,
				ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion, RuntimeVersion: agentruntime.CurrentRuntimeVersion,
				PolicyVersion: agentruntime.CurrentPolicyVersion, UserMessage: "创建图片节点", Configuration: configuration,
				Now: now.Add(time.Duration(index) * time.Second),
			},
		}); err != nil {
			t.Fatal(err)
		}
		current, err := svc.repo.LoadAgentCheckpoint(scope)
		if err != nil {
			t.Fatal(err)
		}
		cancelled, err := agentruntime.Interrupt(current, current.StateVersion)
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.repo.CommitAgentRuntimeTransition(scope, current, cancelled, now.Add(time.Duration(index+2)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	setAgentCanvasCapabilityDocument(t, db, 7, `{"nodes":[{"id":"history-node-1","type":"image","title":"One","position":{"x":0,"y":0},"width":320,"height":180,"metadata":{"status":"loading","taskStage":"正在创建任务","agentRunId":"runtime-run"}},{"id":"history-node-2","type":"video","title":"Two","position":{"x":400,"y":0},"width":320,"height":180,"metadata":{"status":"loading","taskStage":"正在创建任务","agentRunId":"runtime-run-2"}}],"connections":[]}`)

	history, err := svc.ListAgentThreads(&model.User{ID: firstScope.ActorUserID}, firstScope.CanvasID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if history.CanvasProjectionRevision == nil || *history.CanvasProjectionRevision != 8 {
		t.Fatalf("history canvas projection revision = %v, want 8", history.CanvasProjectionRevision)
	}
	canvas := loadAgentTerminalProjectionCanvas(t, db)
	if canvas.Revision != 8 {
		t.Fatalf("canvas revision = %d, want one batch commit to revision 8", canvas.Revision)
	}
	for _, nodeID := range []string{"history-node-1", "history-node-2"} {
		metadata := terminalProjectionNodeMetadata(t, canvas.PayloadJSON, nodeID)
		assertTerminalProjectionString(t, metadata, "status", "error")
		assertTerminalProjectionString(t, metadata, "generationErrorCode", "agent_run_cancelled")
	}

	replayed, err := svc.ListAgentThreads(&model.User{ID: firstScope.ActorUserID}, firstScope.CanvasID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.CanvasProjectionRevision == nil || *replayed.CanvasProjectionRevision != 8 {
		t.Fatalf("idempotent history replay projection revision = %v, want 8", replayed.CanvasProjectionRevision)
	}
	if canvas = loadAgentTerminalProjectionCanvas(t, db); canvas.Revision != 8 {
		t.Fatalf("idempotent history replay changed canvas revision to %d", canvas.Revision)
	}
}

func TestAgentThreadHistoryTurnViewNormalizesTimesToUTC(t *testing.T) {
	shanghai := time.FixedZone("Asia/Shanghai", 8*60*60)
	createdAt := time.Date(2026, time.September, 7, 8, 9, 10, 123000000, shanghai)
	updatedAt := createdAt.Add(time.Minute)
	completedAt := updatedAt.Add(time.Minute)

	view, err := agentThreadHistoryTurnView(model.AgentRun{
		ID: "run-utc", ThreadID: "thread-utc", CreatedAt: createdAt, UpdatedAt: updatedAt, CompletedAt: &completedAt,
	}, []model.AgentTimelineItem{{
		ID: "item-utc", RunID: "run-utc", ContentJSON: `{}`, StartedAt: createdAt,
		CreatedAt: createdAt, UpdatedAt: updatedAt, CompletedAt: &completedAt,
	}})
	if err != nil {
		t.Fatalf("build history turn view: %v", err)
	}

	assertUTCTime(t, "run.createdAt", view.Run.CreatedAt)
	assertUTCTime(t, "run.updatedAt", view.Run.UpdatedAt)
	assertUTCTimePointer(t, "run.completedAt", view.Run.CompletedAt)
	assertUTCTime(t, "item.startedAt", view.Items[0].StartedAt)
	assertUTCTime(t, "item.createdAt", view.Items[0].CreatedAt)
	assertUTCTime(t, "item.updatedAt", view.Items[0].UpdatedAt)
	assertUTCTimePointer(t, "item.completedAt", view.Items[0].CompletedAt)
}

func assertUTCTime(t *testing.T, label string, value time.Time) {
	t.Helper()
	if value.Location() != time.UTC {
		t.Fatalf("%s location = %s, want UTC", label, value.Location())
	}
}

func assertUTCTimePointer(t *testing.T, label string, value *time.Time) {
	t.Helper()
	if value == nil {
		t.Fatalf("%s is nil", label)
	}
	assertUTCTime(t, label, *value)
}
