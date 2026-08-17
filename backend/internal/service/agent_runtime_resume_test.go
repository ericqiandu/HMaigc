package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func TestAgentRuntimeRepairCreatesExactlyOneNextModelTask(t *testing.T) {
	decision := `{"kind":"final","final":{"message":"需要先生成图片。","expectedDelivery":{"kind":"generated_asset","requiredArtifacts":["image"],"completionCriteria":[{"fact":"artifact","artifact":"image"}]}}}`
	server, calls := newAgentRuntimeDecisionServer(t, decision)
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	input := StartAgentRuntimeInput{Scope: agentRuntimeServiceScope(), ClientRequestID: "client-request-repair", UserMessage: "生成一张图片", MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput()}
	started, err := svc.StartAgentRuntime(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	progress, err := svc.ResumeAgentRuntime(input.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Status != agentruntime.RunRunning || progress.State.StepNumber != 1 || progress.State.Verification == nil || progress.State.Verification.Status != agentruntime.VerificationRepairable || progress.ModelTask == nil {
		t.Fatalf("repair progress = %#v", progress)
	}
	if progress.ModelTask.ID == started.ModelTask.ID || progress.ModelTask.Status != model.TaskStatusQueued {
		t.Fatalf("next model task = %#v", progress.ModelTask)
	}
	replayed, err := svc.ResumeAgentRuntime(input.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ModelTask == nil || replayed.ModelTask.ID != progress.ModelTask.ID || calls.Load() != 1 {
		t.Fatalf("repair replay = %#v calls=%d", replayed, calls.Load())
	}
	var taskCount int64
	var billingCount int64
	if err := db.Model(&model.Task{}).Where("user_id = ? AND type = ?", input.Scope.ActorUserID, agentRuntimeModelTaskType).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("user_id = ? AND idempotency_key LIKE ?", input.Scope.ActorUserID, "agent-runtime:%").Count(&billingCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 2 || billingCount != 2 {
		t.Fatalf("repair facts: tasks=%d billings=%d", taskCount, billingCount)
	}
}

func TestAgentRuntimeToolDecisionWaitsWithoutSubmittingAnotherModelTask(t *testing.T) {
	decision := `{"kind":"tool_call","toolCall":{"toolCallId":"call-read-state","toolName":"canvas.read_state","actionVersion":1,"arguments":{}}}`
	server, calls := newAgentRuntimeDecisionServer(t, decision, agentRuntimeTestAnswerDelivery())
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	input := StartAgentRuntimeInput{Scope: agentRuntimeServiceScope(), ClientRequestID: "client-request-tool", UserMessage: "读取画布", MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput()}
	if _, err := svc.StartAgentRuntime(input); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	progress, err := svc.ResumeAgentRuntime(input.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Status != agentruntime.RunWaitingTool || progress.State.PendingToolCall == nil || progress.State.PendingToolCall.ToolName != agentruntime.ToolCanvasReadState || progress.ModelTask != nil {
		t.Fatalf("tool progress = %#v", progress)
	}
	replayed, err := svc.ResumeAgentRuntime(input.Scope)
	if err != nil {
		t.Fatal(err)
	}
	var taskCount int64
	if err := db.Model(&model.Task{}).Where("user_id = ? AND type = ?", input.Scope.ActorUserID, agentRuntimeModelTaskType).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if replayed.State.Status != agentruntime.RunWaitingTool || taskCount != 1 || calls.Load() != 1 {
		t.Fatalf("tool replay = %#v tasks=%d calls=%d", replayed, taskCount, calls.Load())
	}
}

func TestAgentRuntimeRepairsReusedToolIdentityWithoutRepositoryConflict(t *testing.T) {
	decision := `{"kind":"tool_call","toolCall":{"toolCallId":"call-read-state","toolName":"canvas.read_state","actionVersion":1,"arguments":{}}}`
	decision = agentRuntimeToolDecisionWithDelivery(t, decision, agentRuntimeTestAnswerDelivery())
	server := newAgentRuntimeDecisionSequenceServer(t, decision, decision)
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	scope := agentRuntimeServiceScope()
	input := StartAgentRuntimeInput{Scope: scope, ClientRequestID: "client-reused-tool-identity", UserMessage: "读取两次画布", MaxSteps: 5, Configuration: guidedAgentRuntimeConfigurationInput()}
	if _, err := svc.StartAgentRuntime(input); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	if err := svc.DriveAgentRuns(10); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	progress, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Status != agentruntime.RunRunning || progress.State.StepNumber != 2 || progress.State.LastToolResult == nil ||
		progress.State.LastToolResult.Succeeded || progress.State.LastToolResult.ErrorCode != "tool_identity_reused" || progress.ModelTask == nil {
		t.Fatalf("reused tool identity repair = %#v", progress)
	}
	var toolCallCount int64
	if err := db.Model(&model.AgentToolCall{}).Where("run_id = ? AND tool_call_id = ? AND action_version = ?", scope.RunID, "call-read-state", 1).Count(&toolCallCount).Error; err != nil {
		t.Fatal(err)
	}
	if toolCallCount != 1 {
		t.Fatalf("reused tool identity persisted %d records", toolCallCount)
	}
}

func TestDriveAgentRunsResumesModelDecisionAndExecutesServerTool(t *testing.T) {
	decision := `{"kind":"tool_call","toolCall":{"toolCallId":"call-drive-read-state","toolName":"canvas.read_state","actionVersion":1,"arguments":{}}}`
	server, calls := newAgentRuntimeDecisionServer(t, decision, agentRuntimeTestAnswerDelivery())
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	input := StartAgentRuntimeInput{Scope: agentRuntimeServiceScope(), ClientRequestID: "client-drive-tool", UserMessage: "读取画布", MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput()}
	if _, err := svc.StartAgentRuntime(input); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	if err := svc.DriveAgentRuns(10); err != nil {
		t.Fatal(err)
	}
	progress, err := svc.ResumeAgentRuntime(input.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Status != agentruntime.RunRunning || progress.State.LastToolResult == nil || !progress.State.LastToolResult.Succeeded || progress.ModelTask == nil {
		t.Fatalf("driven progress = %#v", progress)
	}
	var taskCount int64
	if err := db.Model(&model.Task{}).Where("user_id = ? AND type = ?", input.Scope.ActorUserID, agentRuntimeModelTaskType).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 2 || calls.Load() != 1 {
		t.Fatalf("driven facts: tasks=%d calls=%d", taskCount, calls.Load())
	}
}

func TestDriveAgentRunsTerminatesRunWhenNextModelReservationHasInsufficientCredits(t *testing.T) {
	decision := `{"kind":"final","final":{"message":"需要先生成图片。","expectedDelivery":{"kind":"generated_asset","requiredArtifacts":["image"],"completionCriteria":[{"fact":"artifact","artifact":"image"}]}}}`
	server, calls := newAgentRuntimeDecisionServer(t, decision)
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	scope := agentRuntimeServiceScope()
	if _, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "client-drive-insufficient-credits", UserMessage: "生成一张图片", MaxSteps: 4,
		Configuration: guidedAgentRuntimeConfigurationInput(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditAccount{}).Where("user_id = ?", scope.ActorUserID).Update("available_microcredits", 0).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}

	if err := svc.DriveAgentRuns(10); err != nil {
		t.Fatalf("drive insufficient-credit run: %v", err)
	}
	state, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != agentruntime.RunFailed || state.FailureCode != "insufficient_credits" {
		t.Fatalf("insufficient-credit run state = %#v", state)
	}
	var taskCount int64
	var billingCount int64
	if err := db.Model(&model.Task{}).Where("user_id = ? AND type = ?", scope.ActorUserID, agentRuntimeModelTaskType).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("user_id = ? AND scene = ?", scope.ActorUserID, "agent_runtime_model").Count(&billingCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 1 || billingCount != 1 || calls.Load() != 1 {
		t.Fatalf("insufficient-credit facts: tasks=%d billings=%d calls=%d", taskCount, billingCount, calls.Load())
	}
	if err := svc.DriveAgentRuns(10); err != nil {
		t.Fatalf("replay terminal run: %v", err)
	}
}

func TestDriveAgentRunsRecordsInvalidReadArgumentsAndLetsModelRepair(t *testing.T) {
	decision := `{"kind":"tool_call","toolCall":{"toolCallId":"call-invalid-read-state","toolName":"canvas.read_state","actionVersion":1,"arguments":{"canvasId":"runtime-canvas"}}}`
	server, calls := newAgentRuntimeDecisionServer(t, decision, agentRuntimeTestAnswerDelivery())
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	input := StartAgentRuntimeInput{Scope: agentRuntimeServiceScope(), ClientRequestID: "client-invalid-read-tool", UserMessage: "读取画布", MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput()}
	if _, err := svc.StartAgentRuntime(input); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	if err := svc.DriveAgentRuns(10); err != nil {
		t.Fatal(err)
	}
	progress, err := svc.ResumeAgentRuntime(input.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Status != agentruntime.RunRunning || progress.State.LastToolResult == nil || progress.State.LastToolResult.Succeeded || progress.State.LastToolResult.ErrorCode != "canvas_read_invalid" || progress.ModelTask == nil {
		t.Fatalf("repair progress = %#v", progress)
	}
	record, err := svc.repo.AgentToolCallForScope(input.Scope, "call-invalid-read-state", 1)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != agentruntime.ToolCallFailed || record.ErrorCode != "canvas_read_invalid" || calls.Load() != 1 {
		t.Fatalf("failed read facts = %#v calls=%d", record, calls.Load())
	}
}

func TestDriveAgentRunsTerminatesRunAfterCanvasAccessIsRevoked(t *testing.T) {
	server, _ := newAgentRuntimeDecisionServer(t, `{"kind":"tool_call","toolCall":{"toolCallId":"call-revoked-selection","toolName":"canvas.read_selection","actionVersion":1,"arguments":{}}}`, agentRuntimeTestAnswerDelivery())
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	scope := agentRuntimeServiceScope()
	if _, err := svc.StartAgentRuntime(StartAgentRuntimeInput{Scope: scope, ClientRequestID: "client-revoked-scope", UserMessage: "读取事实", MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput()}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	waiting, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.State.Status != agentruntime.RunWaitingTool {
		t.Fatalf("run before access revocation = %#v", waiting.State)
	}
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", scope.CanvasID).Update("user_id", "another-user").Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.DriveAgentRuns(10); err != nil {
		t.Fatal(err)
	}
	state, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != agentruntime.RunFailed || state.FailureCode != "scope_access_revoked" {
		t.Fatalf("revoked run state = %#v", state)
	}
	toolCall, err := svc.repo.AgentToolCallForScope(scope, "call-revoked-selection", 1)
	if err != nil {
		t.Fatal(err)
	}
	if toolCall.Status != agentruntime.ToolCallFailed || toolCall.ErrorCode != "scope_access_revoked" {
		t.Fatalf("revoked tool call = %#v", toolCall)
	}
}

func TestCoordinatePendingAgentReadToolsReauthorizesCanvasAndResumesModel(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		decision  string
		selection *AgentCanvasSelectionFacts
		wantNode  string
	}{
		{
			name:     "read state",
			decision: `{"kind":"tool_call","toolCall":{"toolCallId":"call-read-state","toolName":"canvas.read_state","actionVersion":1,"arguments":{"expectedRevision":7}}}`,
		},
		{
			name:      "read selection",
			decision:  `{"kind":"tool_call","toolCall":{"toolCallId":"call-read-selection","toolName":"canvas.read_selection","actionVersion":1,"arguments":{}}}`,
			selection: &AgentCanvasSelectionFacts{Revision: 7, NodeIDs: []string{"node-1"}}, wantNode: "node-1",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server, _ := newAgentRuntimeDecisionServer(t, testCase.decision, agentRuntimeTestAnswerDelivery())
			defer server.Close()
			svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
			now := time.Now().UTC()
			project := model.CanvasProject{
				ID: "runtime-canvas", UserID: "runtime-user", Title: "第一幕", Revision: 7,
				PayloadJSON: `{"id":"runtime-canvas","nodes":[{"id":"node-1","type":"image","data":{"prompt":"日落"}}],"connections":[]}`,
				CreatedAt:   now, UpdatedAt: now,
			}
			if err := db.Save(&project).Error; err != nil {
				t.Fatal(err)
			}
			input := StartAgentRuntimeInput{Scope: agentRuntimeServiceScope(), ClientRequestID: "client-" + strings.ReplaceAll(testCase.name, " ", "-"), UserMessage: "读取画布", MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput()}
			if _, err := svc.StartAgentRuntime(input); err != nil {
				t.Fatal(err)
			}
			if err := svc.ProcessNextTask(); err != nil {
				t.Fatal(err)
			}
			waiting, err := svc.ResumeAgentRuntime(input.Scope)
			if err != nil {
				t.Fatal(err)
			}
			coordinateInput := CoordinateAgentToolInput{
				ToolCallID: waiting.State.PendingToolCall.ToolCallID, ActionVersion: waiting.State.PendingToolCall.ActionVersion,
				Selection: testCase.selection,
			}
			progress, err := svc.CoordinatePendingAgentTool(input.Scope, coordinateInput)
			if err != nil {
				t.Fatal(err)
			}
			if progress.State.Status != agentruntime.RunRunning || progress.State.StepNumber != waiting.State.StepNumber || progress.State.StateVersion != waiting.State.StateVersion+1 || progress.State.LastToolResult == nil || !progress.State.LastToolResult.Succeeded || progress.ModelTask == nil {
				t.Fatalf("coordinated progress = %#v", progress)
			}
			result := string(progress.State.LastToolResult.Output)
			if !strings.Contains(result, `"revision":7`) || (testCase.wantNode != "" && !strings.Contains(result, testCase.wantNode)) {
				t.Fatalf("tool result = %s", result)
			}
			if !strings.Contains(progress.ModelTask.Prompt, `"lastToolResult"`) || !strings.Contains(progress.ModelTask.Prompt, `"revision":7`) {
				t.Fatalf("next model prompt = %s", progress.ModelTask.Prompt)
			}
			replayed, err := svc.CoordinatePendingAgentTool(input.Scope, coordinateInput)
			if err != nil {
				t.Fatal(err)
			}
			if replayed.State.StateVersion != progress.State.StateVersion || replayed.ModelTask == nil || replayed.ModelTask.ID != progress.ModelTask.ID {
				t.Fatalf("tool replay = %#v", replayed)
			}
		})
	}
}

func TestSubmitAgentToolApprovalRequiresExactFrozenIdentity(t *testing.T) {
	decision := `{"kind":"tool_call","toolCall":{"toolCallId":"call-apply","toolName":"canvas.apply_ops","actionVersion":3,"arguments":{
		"baseRevision": 7,
		"ops": []
	}}}`
	server, _ := newAgentRuntimeDecisionServer(t, decision, agentRuntimeTestCanvasDelivery())
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	input := StartAgentRuntimeInput{Scope: agentRuntimeServiceScope(), ClientRequestID: "client-approval", UserMessage: "修改画布", MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput()}
	if _, err := svc.StartAgentRuntime(input); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	waiting, err := svc.ResumeAgentRuntime(input.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.State.Status != agentruntime.RunWaitingApproval {
		t.Fatalf("approval state = %#v", waiting.State)
	}
	if err := db.Model(&model.AgentToolCall{}).
		Where("run_id = ? AND tool_call_id = ? AND action_version = ?", input.Scope.RunID, "call-apply", 3).
		Update("input_json", "{\n  \"baseRevision\": 7,\n  \"ops\": []\n}").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", input.Scope.CanvasID).Update("user_id", "another-user").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitAgentToolApproval(input.Scope, AgentToolApprovalSubmission{
		ToolCallID: "call-apply", ActionVersion: 3, Decision: agentruntime.ToolApprovalApproved,
	}); err == nil {
		t.Fatal("approval accepted after canvas access was revoked")
	}
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", input.Scope.CanvasID).Update("user_id", input.Scope.ActorUserID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitAgentToolApproval(input.Scope, AgentToolApprovalSubmission{
		ToolCallID: "wrong-call", ActionVersion: 3, Decision: agentruntime.ToolApprovalApproved,
	}); err == nil {
		t.Fatal("mismatched approval identity was accepted")
	}
	approved, err := svc.SubmitAgentToolApproval(input.Scope, AgentToolApprovalSubmission{
		ToolCallID: "call-apply", ActionVersion: 3, Decision: agentruntime.ToolApprovalApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if approved.State.Status != agentruntime.RunWaitingTool || approved.State.StepNumber != waiting.State.StepNumber || approved.State.StateVersion != waiting.State.StateVersion+1 {
		t.Fatalf("approved state = %#v", approved.State)
	}
	replayed, err := svc.SubmitAgentToolApproval(input.Scope, AgentToolApprovalSubmission{
		ToolCallID: "call-apply", ActionVersion: 3, Decision: agentruntime.ToolApprovalApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if replayed.State.StateVersion != approved.State.StateVersion || replayed.State.Status != agentruntime.RunWaitingTool {
		t.Fatalf("approval replay = %#v", replayed.State)
	}
}

func TestCoordinatePendingAgentCanvasMutationCommitsFrozenPatchAndResumesModel(t *testing.T) {
	decision := `{"kind":"tool_call","toolCall":{"toolCallId":"call-apply-title","toolName":"canvas.apply_ops","actionVersion":1,"arguments":{"baseRevision":7,"patch":{"upsertNodes":[{"id":"agent-title","type":"text","data":{"text":"第一幕"}}]}}}}`
	server, _ := newAgentRuntimeDecisionServer(t, decision, agentRuntimeTestCanvasDelivery())
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	scope := agentRuntimeServiceScope()
	input := StartAgentRuntimeInput{Scope: scope, ClientRequestID: "client-agent-canvas-write", UserMessage: "在画布加入第一幕标题", MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput()}
	if _, err := svc.StartAgentRuntime(input); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResumeAgentRuntime(scope); err != nil {
		t.Fatal(err)
	}
	approved, err := svc.SubmitAgentToolApproval(scope, AgentToolApprovalSubmission{
		ToolCallID: "call-apply-title", ActionVersion: 1, Decision: agentruntime.ToolApprovalApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if approved.State.Status != agentruntime.RunWaitingTool {
		t.Fatalf("approved state = %#v", approved.State)
	}
	progress, err := svc.CoordinatePendingAgentTool(scope, CoordinateAgentToolInput{
		ToolCallID: "call-apply-title", ActionVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Status != agentruntime.RunRunning || progress.State.PendingToolCall != nil ||
		progress.State.LastToolResult == nil || !progress.State.LastToolResult.Succeeded || progress.ModelTask == nil {
		t.Fatalf("canvas mutation progress = %#v", progress)
	}
	resultJSON := string(progress.State.LastToolResult.Output)
	if !strings.Contains(resultJSON, `"baseRevision":7`) || !strings.Contains(resultJSON, `"committedRevision":8`) ||
		!strings.Contains(resultJSON, `"clientMutationId":"agent-`) {
		t.Fatalf("canvas mutation result = %s", resultJSON)
	}
	project, err := svc.repo.CanvasProject(scope.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	if project.Revision != 8 || !strings.Contains(project.PayloadJSON, `"id":"agent-title"`) {
		t.Fatalf("committed canvas = %#v", project)
	}
	replayed, err := svc.CoordinatePendingAgentTool(scope, CoordinateAgentToolInput{
		ToolCallID: "call-apply-title", ActionVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	project, err = svc.repo.CanvasProject(scope.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	if project.Revision != 8 || replayed.State.StateVersion != progress.State.StateVersion ||
		replayed.ModelTask == nil || replayed.ModelTask.ID != progress.ModelTask.ID {
		t.Fatalf("canvas mutation replay = %#v project=%#v", replayed, project)
	}
}

func TestCoordinatePendingAgentCanvasMutationAutomaticModeUsesEffectiveApprovalPolicy(t *testing.T) {
	decision := `{"kind":"tool_call","toolCall":{"toolCallId":"call-auto-apply-title","toolName":"canvas.apply_ops","actionVersion":1,"arguments":{"baseRevision":7,"patch":{"upsertNodes":[{"id":"agent-auto-title","type":"text","data":{"text":"自动模式标题"}}]}}}}`
	server, _ := newAgentRuntimeDecisionServer(t, decision, agentRuntimeTestCanvasDelivery())
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	scope := agentRuntimeServiceScope()
	input := StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "client-agent-auto-canvas-write", UserMessage: "在画布加入自动模式标题", MaxSteps: 4,
		Configuration: AgentRuntimeConfigurationInput{ExecutionMode: agentruntime.ExecutionAutomatic},
	}
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
	if waiting.State.Status != agentruntime.RunWaitingTool || waiting.State.PendingToolCall == nil {
		t.Fatalf("automatic write state = %#v", waiting.State)
	}
	progress, err := svc.CoordinatePendingAgentTool(scope, CoordinateAgentToolInput{
		ToolCallID: "call-auto-apply-title", ActionVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Status != agentruntime.RunRunning || progress.State.LastToolResult == nil || !progress.State.LastToolResult.Succeeded {
		t.Fatalf("automatic canvas mutation progress = %#v", progress)
	}
	project, err := svc.repo.CanvasProject(scope.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	if project.Revision != 8 || !strings.Contains(project.PayloadJSON, `"id":"agent-auto-title"`) {
		t.Fatalf("automatic canvas mutation project = %#v", project)
	}
}

func TestCoordinatePendingAgentCanvasMutationReportsRevisionConflictForModelRepair(t *testing.T) {
	decision := `{"kind":"tool_call","toolCall":{"toolCallId":"call-stale-apply","toolName":"canvas.apply_ops","actionVersion":1,"arguments":{"baseRevision":7,"patch":{"upsertNodes":[{"id":"stale-node","type":"text"}]}}}}`
	server, _ := newAgentRuntimeDecisionServer(t, decision, agentRuntimeTestCanvasDelivery())
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	scope := agentRuntimeServiceScope()
	input := StartAgentRuntimeInput{Scope: scope, ClientRequestID: "client-stale-agent-write", UserMessage: "增加节点", MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput()}
	if _, err := svc.StartAgentRuntime(input); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResumeAgentRuntime(scope); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitAgentToolApproval(scope, AgentToolApprovalSubmission{
		ToolCallID: "call-stale-apply", ActionVersion: 1, Decision: agentruntime.ToolApprovalApproved,
	}); err != nil {
		t.Fatal(err)
	}
	title := "协作者已更新"
	if _, err := svc.CommitCanvasMutation(&model.User{ID: scope.ActorUserID}, scope.CanvasID, CanvasMutationRequest{
		BaseRevision: 7, ClientMutationID: "external-collaborator-write",
		Patch: CanvasMutationPatch{Document: &CanvasDocumentPatch{Title: &title}},
	}); err != nil {
		t.Fatal(err)
	}
	progress, err := svc.CoordinatePendingAgentTool(scope, CoordinateAgentToolInput{
		ToolCallID: "call-stale-apply", ActionVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Status != agentruntime.RunRunning || progress.State.LastToolResult == nil ||
		progress.State.LastToolResult.Succeeded || progress.State.LastToolResult.ErrorCode != "canvas_revision_conflict" ||
		progress.ModelTask == nil {
		t.Fatalf("revision conflict progress = %#v", progress)
	}
	project, err := svc.repo.CanvasProject(scope.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	if project.Revision != 8 || strings.Contains(project.PayloadJSON, `"id":"stale-node"`) {
		t.Fatalf("stale mutation changed canvas = %#v", project)
	}
	replayed, err := svc.CoordinatePendingAgentTool(scope, CoordinateAgentToolInput{
		ToolCallID: "call-stale-apply", ActionVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if replayed.State.StateVersion != progress.State.StateVersion || replayed.State.LastToolResult == nil ||
		replayed.State.LastToolResult.ErrorCode != "canvas_revision_conflict" || replayed.ModelTask == nil ||
		replayed.ModelTask.ID != progress.ModelTask.ID {
		t.Fatalf("revision conflict replay = %#v", replayed)
	}
}

func TestCoordinatePendingAgentCanvasMutationRecoversAfterCommittedCanvasBeforeToolResult(t *testing.T) {
	decision := `{"kind":"tool_call","toolCall":{"toolCallId":"call-crash-recovery","toolName":"canvas.apply_ops","actionVersion":1,"arguments":{"baseRevision":7,"patch":{"upsertNodes":[{"id":"recovered-node","type":"text"}]}}}}`
	server, _ := newAgentRuntimeDecisionServer(t, decision, agentRuntimeTestCanvasDelivery())
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	scope := agentRuntimeServiceScope()
	input := StartAgentRuntimeInput{Scope: scope, ClientRequestID: "client-agent-crash-recovery", UserMessage: "增加可恢复节点", MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput()}
	if _, err := svc.StartAgentRuntime(input); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResumeAgentRuntime(scope); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitAgentToolApproval(scope, AgentToolApprovalSubmission{
		ToolCallID: "call-crash-recovery", ActionVersion: 1, Decision: agentruntime.ToolApprovalApproved,
	}); err != nil {
		t.Fatal(err)
	}
	callbackName := "test:fail-agent-tool-result-checkpoint"
	if err := db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		checkpoint, ok := tx.Statement.Dest.(*model.AgentCheckpoint)
		if ok && strings.Contains(checkpoint.StateJSON, `"lastToolResult"`) {
			tx.AddError(errors.New("injected checkpoint failure after canvas commit"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	_, firstErr := svc.CoordinatePendingAgentTool(scope, CoordinateAgentToolInput{
		ToolCallID: "call-crash-recovery", ActionVersion: 1,
	})
	if firstErr == nil || !strings.Contains(firstErr.Error(), "injected checkpoint failure") {
		t.Fatalf("first coordination error = %v", firstErr)
	}
	if err := db.Callback().Create().Remove(callbackName); err != nil {
		t.Fatal(err)
	}
	project, err := svc.repo.CanvasProject(scope.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	if project.Revision != 8 || !strings.Contains(project.PayloadJSON, `"id":"recovered-node"`) {
		t.Fatalf("canvas was not committed before interruption = %#v", project)
	}
	interrupted, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if interrupted.Status != agentruntime.RunWaitingTool || !interrupted.PendingToolStarted {
		t.Fatalf("interrupted runtime = %#v", interrupted)
	}
	recovered, err := svc.CoordinatePendingAgentTool(scope, CoordinateAgentToolInput{
		ToolCallID: "call-crash-recovery", ActionVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	project, err = svc.repo.CanvasProject(scope.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	if project.Revision != 8 || recovered.State.LastToolResult == nil || !recovered.State.LastToolResult.Succeeded {
		t.Fatalf("recovered coordination = %#v project=%#v", recovered, project)
	}
	var changes int64
	if err := db.Model(&model.CanvasChange{}).Where("canvas_id = ?", scope.CanvasID).Count(&changes).Error; err != nil {
		t.Fatal(err)
	}
	if changes != 1 {
		t.Fatalf("canvas changes after recovery = %d", changes)
	}
}

func TestCoordinatePendingAgentCanvasMutationRejectsLegacyOpsShapeWithoutCompatibilityPath(t *testing.T) {
	decision := `{"kind":"tool_call","toolCall":{"toolCallId":"call-legacy-ops","toolName":"canvas.apply_ops","actionVersion":1,"arguments":{"baseRevision":7,"ops":[]}}}`
	server, _ := newAgentRuntimeDecisionServer(t, decision, agentRuntimeTestCanvasDelivery())
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	scope := agentRuntimeServiceScope()
	input := StartAgentRuntimeInput{Scope: scope, ClientRequestID: "client-legacy-agent-ops", UserMessage: "修改画布", MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput()}
	if _, err := svc.StartAgentRuntime(input); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResumeAgentRuntime(scope); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitAgentToolApproval(scope, AgentToolApprovalSubmission{
		ToolCallID: "call-legacy-ops", ActionVersion: 1, Decision: agentruntime.ToolApprovalApproved,
	}); err != nil {
		t.Fatal(err)
	}
	progress, err := svc.CoordinatePendingAgentTool(scope, CoordinateAgentToolInput{
		ToolCallID: "call-legacy-ops", ActionVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.LastToolResult == nil || progress.State.LastToolResult.Succeeded ||
		progress.State.LastToolResult.ErrorCode != "canvas_mutation_invalid" || progress.ModelTask == nil {
		t.Fatalf("legacy shape progress = %#v", progress)
	}
	project, err := svc.repo.CanvasProject(scope.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	if project.Revision != 7 {
		t.Fatalf("legacy shape changed canvas revision = %d", project.Revision)
	}
}

func TestCoordinatePendingAgentToolRejectsRevokedCanvasAccess(t *testing.T) {
	decision := `{"kind":"tool_call","toolCall":{"toolCallId":"call-read-state","toolName":"canvas.read_state","actionVersion":1,"arguments":{}}}`
	server, _ := newAgentRuntimeDecisionServer(t, decision, agentRuntimeTestAnswerDelivery())
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	now := time.Now().UTC()
	if err := db.Save(&model.CanvasProject{
		ID: "runtime-canvas", UserID: "runtime-user", Title: "第一幕", Revision: 7,
		PayloadJSON: `{"nodes":[],"connections":[]}`, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	input := StartAgentRuntimeInput{Scope: agentRuntimeServiceScope(), ClientRequestID: "client-revoked", UserMessage: "读取画布", MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput()}
	if _, err := svc.StartAgentRuntime(input); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	waiting, err := svc.ResumeAgentRuntime(input.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", input.Scope.CanvasID).Update("user_id", "another-user").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CoordinatePendingAgentTool(input.Scope, CoordinateAgentToolInput{
		ToolCallID: waiting.State.PendingToolCall.ToolCallID, ActionVersion: waiting.State.PendingToolCall.ActionVersion,
	}); err == nil {
		t.Fatal("revoked canvas access was accepted")
	}
	checkpoint, err := svc.repo.LoadAgentCheckpoint(input.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.Status != agentruntime.RunWaitingTool || checkpoint.StateVersion != waiting.State.StateVersion {
		t.Fatalf("revoked access changed runtime = %#v", checkpoint)
	}
}

func createAgentRuntimeCanvas(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC()
	if err := db.Save(&model.CanvasProject{
		ID: "runtime-canvas", UserID: "runtime-user", Title: "Agent Canvas", Revision: 7,
		PayloadJSON: `{"nodes":[],"connections":[]}`, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentAgentToolApprovalDoesNotAcknowledgeOppositeDecision(t *testing.T) {
	decision := `{"kind":"tool_call","toolCall":{"toolCallId":"call-apply","toolName":"canvas.apply_ops","actionVersion":1,"arguments":{"baseRevision":7,"ops":[]}}}`
	server, _ := newAgentRuntimeDecisionServer(t, decision, agentRuntimeTestCanvasDelivery())
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	input := StartAgentRuntimeInput{Scope: agentRuntimeServiceScope(), ClientRequestID: "client-approval-race", UserMessage: "修改画布", MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput()}
	if _, err := svc.StartAgentRuntime(input); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResumeAgentRuntime(input.Scope); err != nil {
		t.Fatal(err)
	}
	decisions := []agentruntime.ToolApprovalDecision{agentruntime.ToolApprovalApproved, agentruntime.ToolApprovalRejected}
	errs := make([]error, len(decisions))
	var workers sync.WaitGroup
	for index, approvalDecision := range decisions {
		workers.Add(1)
		go func(worker int, candidate agentruntime.ToolApprovalDecision) {
			defer workers.Done()
			_, errs[worker] = svc.SubmitAgentToolApproval(input.Scope, AgentToolApprovalSubmission{
				ToolCallID: "call-apply", ActionVersion: 1, Decision: candidate,
			})
		}(index, approvalDecision)
	}
	workers.Wait()
	var successCount int
	for _, err := range errs {
		if err == nil {
			successCount++
		}
	}
	if successCount != 1 {
		t.Fatalf("opposite approvals acknowledged=%d errors=%v", successCount, errs)
	}
	var call model.AgentToolCall
	if err := db.First(&call, "run_id = ? AND tool_call_id = ?", input.Scope.RunID, "call-apply").Error; err != nil {
		t.Fatal(err)
	}
	if call.ApprovalDecision != agentruntime.ToolApprovalApproved && call.ApprovalDecision != agentruntime.ToolApprovalRejected {
		t.Fatalf("persisted approval = %#v", call)
	}
}

func TestAgentCanvasReadStateReturnsBoundedCompressedFacts(t *testing.T) {
	prompt := strings.Repeat("镜", 600)
	project := &model.CanvasProject{
		ID: "canvas-compact", UserID: "runtime-user", Title: "压缩事实", Revision: 9,
		PayloadJSON: `{"nodes":[{"id":"node-1","type":"image","title":"参考图","position":{"x":1,"y":2},"width":320,"height":240,"metadata":{"prompt":"` + prompt + `","providerSecret":"must-not-pass"}}],"connections":[]}`,
	}
	output, err := executeAgentCanvasReadState(project, []byte(`{"expectedRevision":9}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), "providerSecret") || strings.Contains(string(output), "must-not-pass") {
		t.Fatalf("unapproved metadata reached Agent facts: %s", output)
	}
	var result agentCanvasReadStateResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	if result.NodeCount != 1 || result.NodesTruncated || len(result.Nodes) != 1 || len([]rune(result.Nodes[0].Metadata.Prompt)) != agentCanvasTextFactLimit {
		t.Fatalf("compressed result = %#v", result)
	}
}

func TestConcurrentAgentRuntimeResumeCommitsOneTerminalTransition(t *testing.T) {
	decision := `{"kind":"final","final":{"message":"完成。","expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`
	server, calls := newAgentRuntimeDecisionServer(t, decision)
	defer server.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	input := StartAgentRuntimeInput{Scope: agentRuntimeServiceScope(), ClientRequestID: "client-request-concurrent", UserMessage: "给出答案", MaxSteps: 4, Configuration: guidedAgentRuntimeConfigurationInput()}
	if _, err := svc.StartAgentRuntime(input); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	results := make([]*AgentRuntimeProgress, 2)
	errorsByWorker := make([]error, 2)
	var workers sync.WaitGroup
	for index := range results {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			results[worker], errorsByWorker[worker] = svc.ResumeAgentRuntime(input.Scope)
		}(index)
	}
	workers.Wait()
	for index, err := range errorsByWorker {
		if err != nil {
			t.Fatalf("resume worker %d: %v", index, err)
		}
		if results[index] == nil || results[index].State.Status != agentruntime.RunSucceeded {
			t.Fatalf("resume worker %d result = %#v", index, results[index])
		}
	}
	var eventCount int64
	var checkpointCount int64
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ? AND kind = ?", input.Scope.RunID, agentruntime.EventRunCompleted).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentCheckpoint{}).Where("run_id = ? AND state_version = ?", input.Scope.RunID, 2).Count(&checkpointCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 || checkpointCount != 1 || calls.Load() != 1 {
		t.Fatalf("concurrent facts: completedEvents=%d checkpoints=%d calls=%d", eventCount, checkpointCount, calls.Load())
	}
}

func newAgentRuntimeDecisionServer(t *testing.T, decision string, expected ...agentruntime.ExpectedDelivery) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	if len(expected) > 1 {
		t.Fatal("test decision accepts at most one expected delivery")
	}
	if len(expected) == 1 {
		decision = agentRuntimeToolDecisionWithDelivery(t, decision, expected[0])
	}
	if _, err := agentruntime.ParseModelDecision([]byte(decision)); err != nil {
		t.Fatalf("invalid test decision: %v", err)
	}
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	calls := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		response := agentRuntimeChatResponse{Choices: []agentRuntimeChatChoice{{Message: agentRuntimeChatMessage{Content: decision}}}}
		if err := json.NewEncoder(writer).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	return server, calls
}

func agentRuntimeToolDecisionWithDelivery(t *testing.T, payload string, expected agentruntime.ExpectedDelivery) string {
	t.Helper()
	var envelope struct {
		Kind     agentruntime.DecisionKind `json:"kind"`
		ToolCall struct {
			ToolCallID    string                `json:"toolCallId"`
			ToolName      agentruntime.ToolName `json:"toolName"`
			ActionVersion int                   `json:"actionVersion"`
			Arguments     json.RawMessage       `json:"arguments"`
		} `json:"toolCall"`
	}
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		t.Fatalf("decode test tool decision: %v", err)
	}
	decision := agentruntime.ModelDecision{
		Kind: envelope.Kind,
		ToolCall: &agentruntime.ToolCallDecision{
			ToolCallID: envelope.ToolCall.ToolCallID, ToolName: envelope.ToolCall.ToolName,
			ActionVersion: envelope.ToolCall.ActionVersion, Arguments: envelope.ToolCall.Arguments,
			ExpectedDelivery: expected,
		},
	}
	encoded, err := json.Marshal(decision)
	if err != nil {
		t.Fatalf("encode test tool decision: %v", err)
	}
	return string(encoded)
}

func agentRuntimeTestAnswerDelivery() agentruntime.ExpectedDelivery {
	return agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryAnswer,
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}},
	}
}

func agentRuntimeTestImageDelivery() agentruntime.ExpectedDelivery {
	return agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryGeneratedAsset,
		RequiredArtifacts:  []agentruntime.ArtifactKind{agentruntime.ArtifactImage},
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactArtifact, Artifact: agentruntime.ArtifactImage}},
	}
}

func agentRuntimeTestCanvasDelivery() agentruntime.ExpectedDelivery {
	return agentruntime.ExpectedDelivery{
		Kind: agentruntime.DeliveryCanvasChange, TargetCanvasID: "runtime-canvas",
		RequiredArtifacts:  []agentruntime.ArtifactKind{agentruntime.ArtifactCanvasRevision},
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactCanvasRevision}},
	}
}

type agentRuntimeChatResponse struct {
	Choices []agentRuntimeChatChoice `json:"choices"`
}

type agentRuntimeChatChoice struct {
	Message agentRuntimeChatMessage `json:"message"`
}

type agentRuntimeChatMessage struct {
	Content string `json:"content"`
}
