package service

import (
	"encoding/json"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func TestInterruptedAgentRunProjectsOwnedLoadingPlaceholdersToCanvas(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	scope := agentRuntimeServiceScope()
	seedApprovedAgentCanvasProposal(t, svc, db, agentCanvasApplyOpsCall(`{"canvasId":"runtime-canvas","baseRevision":7,"clientMutationId":"unused-proposal","operations":[{"operationId":"unused-operation","type":"add_node","node":{"id":"unused-node","type":"text","title":"unused","position":{"x":0,"y":0},"width":200,"height":100,"metadata":{}}}]}`))
	setAgentCanvasCapabilityDocument(t, db, 7, `{"nodes":[{"id":"owned-image","type":"image","title":"Agent image","position":{"x":0,"y":0},"width":320,"height":180,"metadata":{"status":"loading","taskStage":"正在创建任务","taskProgress":0,"agentRunId":"runtime-run"}},{"id":"other-run-image","type":"image","title":"Other image","position":{"x":400,"y":0},"width":320,"height":180,"metadata":{"status":"loading","taskStage":"正在创建任务","agentRunId":"other-run"}},{"id":"completed-image","type":"image","title":"Completed image","position":{"x":800,"y":0},"width":320,"height":180,"metadata":{"status":"success","content":"/api/resources/ready/file","agentRunId":"runtime-run"}}],"connections":[]}`)
	state, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.SubmitScopedAgentInterrupt(&model.User{ID: scope.ActorUserID}, scope.RunID, state.StateVersion); err != nil {
		t.Fatal(err)
	}

	canvas := loadAgentTerminalProjectionCanvas(t, db)
	if canvas.Revision != 8 {
		t.Fatalf("canvas revision = %d, want 8", canvas.Revision)
	}
	owned := terminalProjectionNodeMetadata(t, canvas.PayloadJSON, "owned-image")
	assertTerminalProjectionString(t, owned, "status", "error")
	assertTerminalProjectionString(t, owned, "generationErrorCode", "agent_run_cancelled")
	assertTerminalProjectionString(t, owned, "errorDetails", "Agent 已停止，此节点未完成。")
	if _, exists := owned["taskStage"]; exists {
		t.Fatal("terminal projection retained taskStage")
	}
	if _, exists := owned["taskProgress"]; exists {
		t.Fatal("terminal projection retained taskProgress")
	}
	assertTerminalProjectionString(t, terminalProjectionNodeMetadata(t, canvas.PayloadJSON, "other-run-image"), "status", "loading")
	assertTerminalProjectionString(t, terminalProjectionNodeMetadata(t, canvas.PayloadJSON, "completed-image"), "status", "success")

	if _, err := svc.ReadScopedAgentRun(&model.User{ID: scope.ActorUserID}, scope.RunID); err != nil {
		t.Fatal(err)
	}
	if replayed := loadAgentTerminalProjectionCanvas(t, db); replayed.Revision != canvas.Revision {
		t.Fatalf("idempotent replay changed revision from %d to %d", canvas.Revision, replayed.Revision)
	}
}

func TestInterruptedAgentRunTreatsMissingNodeCollectionAsEmptyCanvas(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	scope := agentRuntimeServiceScope()
	seedApprovedAgentCanvasProposal(t, svc, db, agentCanvasApplyOpsCall(`{"canvasId":"runtime-canvas","baseRevision":7,"clientMutationId":"unused-proposal","operations":[{"operationId":"unused-operation","type":"add_node","node":{"id":"unused-node","type":"text","title":"unused","position":{"x":0,"y":0},"width":200,"height":100,"metadata":{}}}]}`))
	setAgentCanvasCapabilityDocument(t, db, 7, `{}`)
	state, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.SubmitScopedAgentInterrupt(&model.User{ID: scope.ActorUserID}, scope.RunID, state.StateVersion); err != nil {
		t.Fatal(err)
	}
	if canvas := loadAgentTerminalProjectionCanvas(t, db); canvas.Revision != 7 {
		t.Fatalf("empty canvas revision = %d, want 7", canvas.Revision)
	}
}

func TestInterruptedAgentRunMarksSubmittedMediaAsSettlementPending(t *testing.T) {
	svc, db, started, _ := newStartedMediaSettlementFixture(t)
	scope := agentRuntimeServiceScope()
	setAgentCanvasCapabilityDocument(t, db, 7, `{"nodes":[{"id":"image-node-1","type":"image","title":"Agent image","position":{"x":0,"y":0},"width":320,"height":180,"metadata":{"status":"loading","taskStage":"生成中","taskProgress":45,"agentRunId":"runtime-run"}}],"connections":[]}`)

	if _, err := svc.SubmitScopedAgentInterrupt(&model.User{ID: scope.ActorUserID}, scope.RunID, started.StateVersion); err != nil {
		t.Fatal(err)
	}

	metadata := terminalProjectionNodeMetadata(t, loadAgentTerminalProjectionCanvas(t, db).PayloadJSON, "image-node-1")
	assertTerminalProjectionString(t, metadata, "status", "error")
	assertTerminalProjectionString(t, metadata, "generationErrorCode", "agent_media_settlement_pending")
	assertTerminalProjectionString(t, metadata, "errorDetails", "Agent 已停止，已提交的生成任务仍在处理或核对费用；完成后会自动回填，请勿重复提交。")
}

func TestInterruptedAgentRunProjectsFinalMediaSettlementFailure(t *testing.T) {
	svc, db, started, _ := newStartedMediaSettlementFixture(t)
	scope := agentRuntimeServiceScope()
	setAgentCanvasCapabilityDocument(t, db, 7, `{"nodes":[{"id":"image-node-1","type":"image","title":"Agent image","position":{"x":0,"y":0},"width":320,"height":180,"metadata":{"status":"loading","taskStage":"生成中","taskProgress":45,"agentRunId":"runtime-run"}}],"connections":[]}`)

	if _, err := svc.SubmitScopedAgentInterrupt(&model.User{ID: scope.ActorUserID}, scope.RunID, started.StateVersion); err != nil {
		t.Fatal(err)
	}
	if pending := loadAgentTerminalProjectionCanvas(t, db); pending.Revision != 8 {
		t.Fatalf("pending settlement canvas revision = %d, want 8", pending.Revision)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := svc.dispatchTaskOutbox(time.Now().UTC().Add(time.Minute), 10); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.ReadScopedAgentRun(&model.User{ID: scope.ActorUserID}, scope.RunID); err != nil {
		t.Fatal(err)
	}

	settled := loadAgentTerminalProjectionCanvas(t, db)
	if settled.Revision != 9 {
		t.Fatalf("settled canvas revision = %d, want 9", settled.Revision)
	}
	metadata := terminalProjectionNodeMetadata(t, settled.PayloadJSON, "image-node-1")
	assertTerminalProjectionString(t, metadata, "status", "error")
	assertTerminalProjectionString(t, metadata, "generationErrorCode", "media_generation_cancelled")
	assertTerminalProjectionString(t, metadata, "errorDetails", "Agent 媒体生成未完成，此节点可重新生成。")
}

func loadAgentTerminalProjectionCanvas(t *testing.T, db *gorm.DB) model.CanvasProject {
	t.Helper()
	var canvas model.CanvasProject
	if err := db.First(&canvas, "id = ?", agentRuntimeServiceScope().CanvasID).Error; err != nil {
		t.Fatal(err)
	}
	return canvas
}

func terminalProjectionNodeMetadata(t *testing.T, payload string, nodeID string) map[string]json.RawMessage {
	t.Helper()
	var document struct {
		Nodes []struct {
			ID       string                     `json:"id"`
			Metadata map[string]json.RawMessage `json:"metadata"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(payload), &document); err != nil {
		t.Fatal(err)
	}
	for _, node := range document.Nodes {
		if node.ID == nodeID {
			return node.Metadata
		}
	}
	t.Fatalf("node %q is missing", nodeID)
	return nil
}

func assertTerminalProjectionString(t *testing.T, metadata map[string]json.RawMessage, key string, want string) {
	t.Helper()
	var got string
	if err := json.Unmarshal(metadata[key], &got); err != nil {
		t.Fatalf("metadata[%s] = %s: %v", key, metadata[key], err)
	}
	if got != want {
		t.Fatalf("metadata[%s] = %q, want %q", key, got, want)
	}
}
