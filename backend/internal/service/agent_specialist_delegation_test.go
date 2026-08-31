package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

func TestSpecialistDelegateCreatesGraphRunsSpecialistAndWaitsForStageReview(t *testing.T) {
	skipRetiredAgentExecutionGraph(t)
	scope := specialistRuntimeScope()
	request := scriptSpecialistRuntimeRequestFixture("runtime-token-agent-model", "deepseek-v4-flash")
	request.StageID = repositoryProductionStageIDForTest(scope, "short-film-production", "script")
	request.SpecialistRunID = specialistDelegateRunIDForTest(scope, "delegate-script", 1, request.StageID)
	delivery := request.ExpectedDelivery
	graph := agentruntime.ProductionGraphDraft{
		GraphKey: "short-film-production",
		Stages: []agentruntime.ProductionStageDraft{{
			StageKey: "script", SpecialistKey: agentruntime.SpecialistNarrative,
			DependsOnStageKeys: []string{}, InputRevisions: []agentruntime.ArtifactRevisionRef{},
			ExpectedDelivery: delivery, ReviewPolicy: agentruntime.ReviewRequired, CostPolicy: agentruntime.CostNone,
		}},
	}
	delegateArguments, err := json.Marshal(SpecialistDelegateArguments{
		ProductionGraph: graph, ExpectedGraphVersion: 0, StageKey: "script",
		SpecialistKey: agentruntime.SpecialistNarrative, Objective: request.Objective,
		InputRevisions: []agentruntime.ArtifactRevisionRef{}, SkillDirs: []string{request.LoadedSkills[0].Dir},
		ToolAllowlist: []agentruntime.AgentToolName{}, ExpectedOutputSchema: request.ExpectedOutputSchema,
		ExpectedDelivery: delivery,
	})
	if err != nil {
		t.Fatal(err)
	}
	selection := request.LoadedSkills[0]
	if _, freezeErr := freezeAgentSpecialistDelegateDecisionArguments(
		agentruntime.RunConfiguration{Skills: []agentruntime.SkillSelection{selection}, ExecutionMode: agentruntime.ExecutionAutomatic},
		[]string{selection.Dir},
		&agentruntime.ToolCallDecision{
			ToolCallID: "delegate-script", ToolName: agentruntime.ToolSpecialistDelegate, ActionVersion: 1,
			Arguments: delegateArguments, ExpectedDelivery: delivery,
		},
	); freezeErr != nil {
		t.Fatalf("delegate fixture is not freezable: %v", freezeErr)
	}
	decisions := []string{
		agentRuntimeToolDecisionWithDelivery(t,
			`{"kind":"tool_call","toolCall":{"toolCallId":"load-narrative","toolName":"skill.load","actionVersion":1,"arguments":{"dir":"narrative-production"}}}`,
			delivery,
		),
		mustMarshalAgentDecision(t, agentruntime.ModelDecision{
			Kind: agentruntime.DecisionToolCall,
			ToolCall: &agentruntime.ToolCallDecision{
				ToolCallID: "delegate-script", ToolName: agentruntime.ToolSpecialistDelegate, ActionVersion: 1,
				Arguments: delegateArguments, ExpectedDelivery: delivery,
			},
		}),
		scriptSpecialistRuntimeResultJSON(t, request),
		revisedScriptSpecialistRuntimeResultJSON(t, request),
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == kuaiziBillingPath {
			var lookup struct {
				TaskID string `json:"task_id"`
			}
			if decodeErr := json.NewDecoder(request.Body).Decode(&lookup); decodeErr != nil {
				t.Error(decodeErr)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(writer, `{"code":0,"data":{"items":[{"order_id":"billing-%s","amount":1,"status":"succeeded","task_id":"%s","task_status":"succeeded","task_duration":1,"total_tokens":120,"created_at":"2026-08-28T00:00:00Z"}],"total":1,"page":1,"page_size":20}}`, lookup.TaskID, lookup.TaskID)
			return
		}
		index := int(calls.Add(1)) - 1
		if index >= len(decisions) {
			t.Errorf("unexpected provider call %d", index+1)
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeAgentRuntimeChatStream(t, writer, fmt.Sprintf("chatcmpl-delegate-%d", index+1), decisions[index], 100, 20, 0)
	}))
	defer server.Close()

	svc, db, _ := newSpecialistRuntimeFixture(t, server.URL, request)
	svc.agentRuntimeSkillResolver = func(_ context.Context, _ string, dir string) (*Skill, error) {
		return &Skill{
			Dir: dir, Name: selection.Name, Description: selection.Description, DetailText: selection.Instructions,
			Version: selection.Version, Checksum: selection.Checksum, CapabilityManifest: selection.CapabilityManifest,
			SourceKind: selection.SourceKind, SourceRevision: selection.SourceRevision,
			SourceLicense: selection.SourceLicense, PublishedAt: selection.PublishedAt,
		}, nil
	}
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "delegate-production", UserMessage: "创作一个短片剧本",
		Configuration: AgentRuntimeConfigurationInput{
			SkillDirs: []string{selection.Dir}, ExecutionMode: agentruntime.ExecutionAutomatic,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	loaded, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State.Status != agentruntime.RunRunning || len(loaded.State.LoadedSkillDirs) != 1 {
		t.Fatalf("loaded state = %#v", loaded.State)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	delegatedState, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if delegatedState.Status != agentruntime.RunWaitingTool || delegatedState.PendingToolCall == nil {
		var tasks []model.Task
		if loadErr := db.Where("user_id = ?", scope.ActorUserID).Order("created_at ASC").Find(&tasks).Error; loadErr != nil {
			t.Fatal(loadErr)
		}
		t.Fatalf("state after delegate decision = %#v, feedback = %#v, tasks = %#v", delegatedState, delegatedState.DecisionFeedback, tasks)
	}
	progress, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Run.ID != started.Run.ID || progress.State.Status != agentruntime.RunWaitingTool || !progress.State.PendingToolStarted {
		var toolCalls []model.AgentToolCall
		if loadErr := db.Where("run_id = ?", scope.RunID).Order("created_at ASC").Find(&toolCalls).Error; loadErr != nil {
			t.Fatal(loadErr)
		}
		t.Fatalf("delegate progress = %#v, feedback = %#v, tool calls = %#v", progress, progress.State.DecisionFeedback, toolCalls)
	}
	snapshot, err := svc.repo.ProductionRuntimeSnapshotForScope(scope)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Graph == nil || snapshot.Graph.Version != 1 || len(snapshot.Stages) != 1 ||
		snapshot.Stages[0].Status != agentruntime.StageAwaitingReview || snapshot.Stages[0].ReviewRevisionID == "" {
		t.Fatalf("production snapshot = %#v", snapshot)
	}
	var tool model.AgentToolCall
	if err := db.Where("run_id = ? AND tool_call_id = ?", scope.RunID, "delegate-script").First(&tool).Error; err != nil {
		t.Fatal(err)
	}
	if tool.Status != agentruntime.ToolCallRunning || calls.Load() != 3 {
		t.Fatalf("delegate tool = %#v, provider calls = %d", tool, calls.Load())
	}
	replayed, err := svc.CoordinatePendingAgentTool(scope, CoordinateAgentToolInput{ToolCallID: "delegate-script", ActionVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if replayed.State.Status != agentruntime.RunWaitingTool || calls.Load() != 3 {
		t.Fatalf("delegate replay = %#v, provider calls = %d", replayed, calls.Load())
	}
	revised, err := svc.ReviewProductionStage(context.Background(), scope, progress.Run, snapshot.Stages[0].ID, agentruntime.StageReviewCommand{
		StageVersion: snapshot.Stages[0].Version, RevisionID: snapshot.Stages[0].ReviewRevisionID,
		ClientRequestID: "revise-delegated-script", Decision: agentruntime.StageReviewRequestRevision,
		Comment: "把主角的动机改为寻找失踪的姐姐。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if revised.Completion == nil || revised.Stage.Status != agentruntime.StageAwaitingReview ||
		revised.Stage.ReviewRevisionID == snapshot.Stages[0].ReviewRevisionID || calls.Load() != 4 {
		t.Fatalf("revised stage = %#v, completion = %#v, provider calls = %d", revised.Stage, revised.Completion, calls.Load())
	}
	checkpointAfterRevision, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if !samePendingStartedTool(checkpointAfterRevision, delegatedState.PendingToolCall) {
		t.Fatalf("state after revision = %#v", checkpointAfterRevision)
	}
	approved, err := svc.ReviewProductionStage(context.Background(), scope, progress.Run, revised.Stage.ID, agentruntime.StageReviewCommand{
		StageVersion: revised.Stage.Version, RevisionID: revised.Stage.ReviewRevisionID,
		ClientRequestID: "approve-delegated-script", Decision: agentruntime.StageReviewApprove,
	})
	if err != nil {
		t.Fatal(err)
	}
	if approved.Stage.Status != agentruntime.StageApproved {
		t.Fatalf("approved stage = %#v", approved.Stage)
	}
	resumedState, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	if resumedState.Status != agentruntime.RunRunning || resumedState.PendingToolCall != nil ||
		resumedState.LastToolResult == nil || !resumedState.LastToolResult.Succeeded ||
		resumedState.LastToolResult.ToolCallID != "delegate-script" {
		t.Fatalf("state after approval = %#v", resumedState)
	}
	if err := db.Where("run_id = ? AND tool_call_id = ?", scope.RunID, "delegate-script").First(&tool).Error; err != nil {
		t.Fatal(err)
	}
	if tool.Status != agentruntime.ToolCallSucceeded || calls.Load() != 4 {
		t.Fatalf("approved delegate tool = %#v, provider calls = %d", tool, calls.Load())
	}
	var queuedModelTasks int64
	if err := db.Model(&model.Task{}).
		Where("user_id = ? AND audience = ? AND type = ? AND status = ?", scope.ActorUserID, model.TaskAudienceInternal, "agent_runtime_model", model.TaskStatusQueued).
		Count(&queuedModelTasks).Error; err != nil {
		t.Fatal(err)
	}
	if queuedModelTasks != 1 {
		t.Fatalf("queued model tasks after approval = %d, want 1", queuedModelTasks)
	}
}

func mustMarshalAgentDecision(t *testing.T, decision agentruntime.ModelDecision) string {
	t.Helper()
	encoded, err := json.Marshal(decision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agentruntime.ParseModelDecision(encoded); err != nil {
		t.Fatalf("invalid model decision: %v", err)
	}
	return string(encoded)
}

func specialistDelegateRunIDForTest(scope agentruntime.Scope, toolCallID string, actionVersion int, stageID string) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		"specialist-delegate", scope.RunID, toolCallID, strconv.Itoa(actionVersion), stageID,
	}, "\x00")))
	return fmt.Sprintf("%x", digest[:])
}
