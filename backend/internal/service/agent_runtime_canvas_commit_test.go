package service

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

func TestProductionCanvasCommitBuildsStableTypedProjection(t *testing.T) {
	now := time.Now().UTC()
	plan := model.AgentProductionPlanVersion{
		ID: "plan-version-1", PlanKey: "orange-ad", Version: 1, Title: "10秒橙子广告", TargetDurationMS: 10_000,
		Script:         "鲜橙唤醒清晨。",
		ReferencesJSON: `[]`,
		ShotsJSON:      `[{"shotKey":"shot-1","order":1,"durationMs":10000,"scriptText":"鲜橙落水","deliverables":["storyboard_image","video_clip"],"imagePrompt":"鲜橙产品特写","videoPrompt":"慢镜头水花","dependencies":[]}]`,
	}
	artifacts := []model.AgentProductionArtifact{
		{ID: "artifact-script", PlanKey: plan.PlanKey, PlanVersionID: plan.ID, PlanVersion: 1, Kind: model.AgentProductionArtifactScript, Status: model.AgentProductionArtifactSucceeded},
		{ID: "artifact-image", PlanKey: plan.PlanKey, PlanVersionID: plan.ID, PlanVersion: 1, ShotKey: "shot-1", Kind: model.AgentProductionArtifactStoryboardImage, Status: model.AgentProductionArtifactSucceeded, Attempt: 1, TaskID: "task-image", BillingOrderID: "bill-image", ResourceID: "resource-image"},
		{ID: "artifact-video", PlanKey: plan.PlanKey, PlanVersionID: plan.ID, PlanVersion: 1, ShotKey: "shot-1", Kind: model.AgentProductionArtifactVideoClip, Status: model.AgentProductionArtifactSucceeded, Attempt: 1, TaskID: "task-video", BillingOrderID: "bill-video", ResourceID: "resource-video"},
	}
	resources := map[string]model.Resource{
		"resource-image": {ID: "resource-image", Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png", ObjectKey: "production/shot-1.png", CreatedAt: now, UpdatedAt: now},
		"resource-video": {ID: "resource-video", Kind: "video", Status: model.ResourceStatusReady, MimeType: "video/mp4", ObjectKey: "production/shot-1.mp4", DurationMs: 10_000, CreatedAt: now, UpdatedAt: now},
	}

	first, bindings, err := buildProductionCanvasPatch(plan, artifacts, resources)
	if err != nil {
		t.Fatal(err)
	}
	second, secondBindings, err := buildProductionCanvasPatch(plan, artifacts, resources)
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstJSON, secondJSON) || len(bindings) != 3 || len(secondBindings) != 3 {
		t.Fatalf("production projection is not stable: first=%s second=%s bindings=%#v", firstJSON, secondJSON, bindings)
	}
	if len(first.UpsertNodes) != 3 || len(first.UpsertConnections) != 2 {
		t.Fatalf("production projection counts: nodes=%d connections=%d", len(first.UpsertNodes), len(first.UpsertConnections))
	}

	var nodes []productionCanvasNode
	for _, raw := range first.UpsertNodes {
		var node productionCanvasNode
		if err := json.Unmarshal(raw, &node); err != nil {
			t.Fatal(err)
		}
		nodes = append(nodes, node)
	}
	if nodes[0].Type != "script" || nodes[0].Metadata.Storyboard == nil || len(nodes[0].Metadata.Storyboard.Rows) != 1 {
		t.Fatalf("script node = %#v", nodes[0])
	}
	if nodes[1].Type != "image" || nodes[1].Metadata.Content != "/api/resources/resource-image/file" || nodes[1].Metadata.StorageKey != "resource:resource-image" || nodes[1].Metadata.Status != "success" {
		t.Fatalf("image node = %#v", nodes[1])
	}
	if nodes[2].Type != "video" || nodes[2].Metadata.Content != "/api/resources/resource-video/file" || nodes[2].Metadata.DurationMS != 10_000 || nodes[2].Metadata.Status != "success" {
		t.Fatalf("video node = %#v", nodes[2])
	}
	if nodes[0].ID != productionCanvasNodeID(plan, artifacts[0]) || nodes[1].ID != productionCanvasNodeID(plan, artifacts[1]) || nodes[2].ID != productionCanvasNodeID(plan, artifacts[2]) {
		t.Fatalf("production node identities = %s, %s, %s", nodes[0].ID, nodes[1].ID, nodes[2].ID)
	}
}

func TestProductionCanvasCommitProjectsVideoOnlyPlanWithoutImageNode(t *testing.T) {
	now := time.Now().UTC()
	plan := model.AgentProductionPlanVersion{
		ID: "plan-version-video-only", PlanKey: "video-only", Version: 1, Title: "5秒原创抽象光影", TargetDurationMS: 5_000,
		Script:         "抽象光带汇聚并消散。",
		ReferencesJSON: `[]`,
		ShotsJSON:      `[{"shotKey":"shot-1","order":1,"durationMs":5000,"scriptText":"光带聚合","deliverables":["video_clip"],"videoPrompt":"原创抽象光影，镜头缓慢推进","dependencies":[]}]`,
	}
	artifacts := []model.AgentProductionArtifact{
		{ID: "artifact-script", PlanKey: plan.PlanKey, PlanVersionID: plan.ID, PlanVersion: 1, Kind: model.AgentProductionArtifactScript, Status: model.AgentProductionArtifactSucceeded},
		{ID: "artifact-video", PlanKey: plan.PlanKey, PlanVersionID: plan.ID, PlanVersion: 1, ShotKey: "shot-1", Kind: model.AgentProductionArtifactVideoClip, Status: model.AgentProductionArtifactSucceeded, ResourceID: "resource-video"},
	}
	resources := map[string]model.Resource{
		"resource-video": {ID: "resource-video", Kind: "video", Status: model.ResourceStatusReady, MimeType: "video/mp4", ObjectKey: "production/video-only.mp4", DurationMs: 5_000, CreatedAt: now, UpdatedAt: now},
	}

	patch, bindings, err := buildProductionCanvasPatch(plan, artifacts, resources)
	if err != nil {
		t.Fatal(err)
	}
	if len(patch.UpsertNodes) != 2 || len(patch.UpsertConnections) != 1 || len(bindings) != 2 {
		t.Fatalf("video-only projection counts: nodes=%d connections=%d bindings=%d", len(patch.UpsertNodes), len(patch.UpsertConnections), len(bindings))
	}
	var scriptNode, videoNode productionCanvasNode
	if err := json.Unmarshal(patch.UpsertNodes[0], &scriptNode); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(patch.UpsertNodes[1], &videoNode); err != nil {
		t.Fatal(err)
	}
	if scriptNode.Type != "script" || videoNode.Type != "video" || videoNode.Metadata.Content != "/api/resources/resource-video/file" {
		t.Fatalf("video-only nodes: script=%#v video=%#v", scriptNode, videoNode)
	}
	if scriptNode.Metadata.Storyboard == nil || len(scriptNode.Metadata.Storyboard.Rows) != 1 ||
		scriptNode.Metadata.Storyboard.Rows[0].ImageNodeID != "" || scriptNode.Metadata.Storyboard.Rows[0].VideoNodeID != videoNode.ID {
		t.Fatalf("video-only storyboard row = %#v", scriptNode.Metadata.Storyboard)
	}
	if bytes.Contains(patch.UpsertNodes[0], []byte(`"imageNodeId"`)) {
		t.Fatalf("video-only script node serialized an image binding: %s", patch.UpsertNodes[0])
	}
	if !bytes.Contains(patch.UpsertNodes[0], []byte(`"deliverables":["video_clip"]`)) {
		t.Fatalf("video-only script node omitted the explicit deliverable contract: %s", patch.UpsertNodes[0])
	}
	var connection productionCanvasConnection
	if err := json.Unmarshal(patch.UpsertConnections[0], &connection); err != nil {
		t.Fatal(err)
	}
	if connection.FromNodeID != scriptNode.ID || connection.ToNodeID != videoNode.ID || connection.FromHandleID != "row:shot-1" {
		t.Fatalf("video-only connection = %#v", connection)
	}
}

func TestProductionCanvasCommitProjectsReferenceNodesAndBindings(t *testing.T) {
	now := time.Now().UTC()
	plan := model.AgentProductionPlanVersion{
		ID: "plan-version-reference", PlanKey: "watch-film", Version: 1, Title: "雨夜怀表", TargetDurationMS: 6_000,
		Script:         "顾棠在雨夜钟表店为怀表上弦。",
		ReferencesJSON: `[{"referenceKey":"character-gu-tang","role":"character","title":"顾棠角色定妆","imagePrompt":"黑色齐下巴短发，右侧银色发夹，深青色风衣"}]`,
		ShotsJSON:      `[{"shotKey":"shot-1","order":1,"durationMs":6000,"scriptText":"顾棠拿起怀表","deliverables":["storyboard_image","video_clip"],"imagePrompt":"雨夜钟表店中景","videoPrompt":"缓慢推进","referenceKeys":["character-gu-tang"],"dependencies":[]}]`,
	}
	artifacts := []model.AgentProductionArtifact{
		{ID: "artifact-script", PlanKey: plan.PlanKey, PlanVersionID: plan.ID, PlanVersion: 1, Kind: model.AgentProductionArtifactScript, Status: model.AgentProductionArtifactSucceeded},
		{ID: "artifact-reference", PlanKey: plan.PlanKey, PlanVersionID: plan.ID, PlanVersion: 1, ReferenceKey: "character-gu-tang", Kind: model.AgentProductionArtifactReferenceImage, Status: model.AgentProductionArtifactSucceeded, ResourceID: "resource-reference"},
		{ID: "artifact-image", PlanKey: plan.PlanKey, PlanVersionID: plan.ID, PlanVersion: 1, ShotKey: "shot-1", Kind: model.AgentProductionArtifactStoryboardImage, Status: model.AgentProductionArtifactSucceeded, ResourceID: "resource-image"},
		{ID: "artifact-video", PlanKey: plan.PlanKey, PlanVersionID: plan.ID, PlanVersion: 1, ShotKey: "shot-1", Kind: model.AgentProductionArtifactVideoClip, Status: model.AgentProductionArtifactSucceeded, ResourceID: "resource-video"},
	}
	resources := map[string]model.Resource{
		"resource-reference": {ID: "resource-reference", Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png", ObjectKey: "production/character.png", CreatedAt: now, UpdatedAt: now},
		"resource-image":     {ID: "resource-image", Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png", ObjectKey: "production/shot-1.png", CreatedAt: now, UpdatedAt: now},
		"resource-video":     {ID: "resource-video", Kind: "video", Status: model.ResourceStatusReady, MimeType: "video/mp4", ObjectKey: "production/shot-1.mp4", DurationMs: 6_000, CreatedAt: now, UpdatedAt: now},
	}

	patch, bindings, err := buildProductionCanvasPatch(plan, artifacts, resources)
	if err != nil {
		t.Fatal(err)
	}
	if len(patch.UpsertNodes) != 4 || len(patch.UpsertConnections) != 4 || len(bindings) != 4 {
		t.Fatalf("reference projection counts: nodes=%d connections=%d bindings=%d", len(patch.UpsertNodes), len(patch.UpsertConnections), len(bindings))
	}
	var scriptNode, referenceNode productionCanvasNode
	if err := json.Unmarshal(patch.UpsertNodes[0], &scriptNode); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(patch.UpsertNodes[1], &referenceNode); err != nil {
		t.Fatal(err)
	}
	if referenceNode.Type != "image" || referenceNode.Metadata.WorkflowKind != "reference" || referenceNode.Metadata.Content != "/api/resources/resource-reference/file" {
		t.Fatalf("reference node = %#v", referenceNode)
	}
	if scriptNode.Metadata.Storyboard == nil || len(scriptNode.Metadata.Storyboard.ReferenceNodeIDs) != 1 ||
		len(scriptNode.Metadata.Storyboard.Rows) != 1 || len(scriptNode.Metadata.Storyboard.Rows[0].ReferenceNodeIDs) != 1 ||
		scriptNode.Metadata.Storyboard.ReferenceNodeIDs[0] != referenceNode.ID || scriptNode.Metadata.Storyboard.Rows[0].ReferenceNodeIDs[0] != referenceNode.ID {
		t.Fatalf("storyboard reference bindings = %#v", scriptNode.Metadata.Storyboard)
	}
}

func TestProductionCanvasCommitCommitsOnceAndRejectsStaleRevision(t *testing.T) {
	t.Run("commit and replay", func(t *testing.T) {
		svc, db, scope, plan, artifacts, setDecision := prepareProductionCanvasCommitTest(t)
		setDecision(productionCanvasCommitDecision(t, "commit-production", plan, artifacts, 7))
		if err := svc.ProcessNextTask(); err != nil {
			t.Fatal(err)
		}
		progress, err := svc.ResumeAgentRuntime(scope)
		if err != nil {
			t.Fatal(err)
		}
		if progress.State.Status != agentruntime.RunRunning || progress.State.PendingToolCall != nil ||
			progress.State.LastToolResult == nil || !progress.State.LastToolResult.Succeeded {
			t.Fatalf("canvas commit result = %#v", progress.State.LastToolResult)
		}
		if _, err := svc.CoordinatePendingAgentTool(scope, CoordinateAgentToolInput{ToolCallID: "commit-production", ActionVersion: 1}); err != nil {
			t.Fatal(err)
		}
		var canvas model.CanvasProject
		if err := db.First(&canvas, "id = ?", scope.CanvasID).Error; err != nil {
			t.Fatal(err)
		}
		if canvas.Revision != 8 {
			t.Fatalf("canvas revision after replay = %d", canvas.Revision)
		}
		committed, err := svc.repo.AgentProductionArtifactsForVersion(scope, plan.PlanKey, plan.Version)
		if err != nil {
			t.Fatal(err)
		}
		for _, artifact := range committed {
			if artifact.CanvasNodeID == "" || artifact.Status != model.AgentProductionArtifactCommitted {
				t.Fatalf("committed artifact = %#v", artifact)
			}
			if !bytes.Contains([]byte(canvas.PayloadJSON), []byte(artifact.CanvasNodeID)) {
				t.Fatalf("canvas payload does not contain artifact node %s", artifact.CanvasNodeID)
			}
		}
	})

	t.Run("stale revision", func(t *testing.T) {
		svc, db, scope, plan, artifacts, setDecision := prepareProductionCanvasCommitTest(t)
		setDecision(productionCanvasCommitDecision(t, "commit-stale", plan, artifacts, 7))
		title := "External revision"
		if _, err := svc.CommitCanvasMutation(&model.User{ID: scope.ActorUserID}, scope.CanvasID, CanvasMutationRequest{
			BaseRevision: 7, ClientMutationID: "external-revision", Patch: CanvasMutationPatch{Document: &CanvasDocumentPatch{Title: &title}},
		}); err != nil {
			t.Fatal(err)
		}
		if err := svc.ProcessNextTask(); err != nil {
			t.Fatal(err)
		}
		progress, err := svc.ResumeAgentRuntime(scope)
		if err != nil {
			t.Fatal(err)
		}
		if progress.State.LastToolResult == nil || progress.State.LastToolResult.Succeeded || progress.State.LastToolResult.ErrorCode != "canvas_revision_conflict" {
			t.Fatalf("stale canvas commit result = %#v", progress.State.LastToolResult)
		}
		var conflictFacts struct {
			CurrentRevision string `json:"currentRevision"`
		}
		if err := json.Unmarshal(progress.State.LastToolResult.Output, &conflictFacts); err != nil {
			t.Fatal(err)
		}
		if conflictFacts.CurrentRevision != "8" {
			t.Fatalf("stale canvas conflict facts = %#v", conflictFacts)
		}
		var canvas model.CanvasProject
		if err := db.First(&canvas, "id = ?", scope.CanvasID).Error; err != nil {
			t.Fatal(err)
		}
		if canvas.Revision != 8 || canvas.Title != title || bytes.Contains([]byte(canvas.PayloadJSON), []byte("production-node-")) {
			t.Fatalf("stale commit changed canvas: revision=%d title=%q payload=%s", canvas.Revision, canvas.Title, canvas.PayloadJSON)
		}
		current, err := svc.repo.AgentProductionArtifactsForVersion(scope, plan.PlanKey, plan.Version)
		if err != nil {
			t.Fatal(err)
		}
		for _, artifact := range current {
			if artifact.CanvasNodeID != "" {
				t.Fatalf("stale commit bound artifact = %#v", artifact)
			}
		}
	})
}

func prepareProductionCanvasCommitTest(t *testing.T) (*Service, *gorm.DB, agentruntime.Scope, model.AgentProductionPlanVersion, []model.AgentProductionArtifact, func(string)) {
	t.Helper()
	var decision string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeAgentRuntimeChatStream(t, writer, "chatcmpl-canvas-commit", decision, 0, 0, 0)
	}))
	t.Cleanup(server.Close)
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	createAgentRuntimeCanvas(t, db)
	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "production-canvas-" + strings.ReplaceAll(t.Name(), "/", "-"),
		UserMessage: "把生产计划提交到画布", MaxSteps: 6,
		Configuration: AgentRuntimeConfigurationInput{ExecutionMode: agentruntime.ExecutionAutomatic},
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.State.MaxSteps != agentRuntimeMaxSteps || started.Run.MaxSteps != agentRuntimeMaxSteps {
		t.Fatalf("server-owned agent step budget = state:%d run:%d", started.State.MaxSteps, started.Run.MaxSteps)
	}
	record, err := svc.repo.AppendAgentProductionPlanVersion(repository.AppendAgentProductionPlanInput{
		Scope: scope, RunID: started.Run.ID, PlanKey: "plan-canvas-commit", BaseVersion: 0,
		Draft: agentruntime.ProductionPlanDraft{
			Title: "橙子广告", TargetDurationMS: 5_000, Script: "鲜橙落水。",
			Shots: []agentruntime.ShotPlanDraft{{ShotKey: "shot-1", Order: 1, DurationMS: 5_000, ScriptText: "鲜橙落水", Deliverables: agentRuntimeDualProductionDeliverables(), ImagePrompt: "鲜橙特写", VideoPrompt: "慢镜水花", Dependencies: []string{}}},
		},
		Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, artifact := range record.Artifacts {
		if artifact.Kind == model.AgentProductionArtifactScript {
			continue
		}
		kind := "image"
		mimeType := "image/png"
		if artifact.Kind == model.AgentProductionArtifactVideoClip {
			kind = "video"
			mimeType = "video/mp4"
		}
		resource := model.Resource{ID: "resource-" + artifact.ID[len(artifact.ID)-8:], UserID: scope.ActorUserID, Kind: kind, Status: model.ResourceStatusReady, MimeType: mimeType, ObjectKey: "production/" + artifact.ID, CreatedAt: now, UpdatedAt: now}
		if err := db.Create(&resource).Error; err != nil {
			t.Fatal(err)
		}
		queued, err := svc.repo.TransitionAgentProductionArtifact(scope, repository.ArtifactTransition{
			ArtifactID: artifact.ID, ExpectedStatus: model.AgentProductionArtifactPlanned, NextStatus: model.AgentProductionArtifactQueued,
			ExpectedAttempt: 0, NextAttempt: 1, TaskID: "task-" + artifact.ID, BillingOrderID: "bill-" + artifact.ID, Now: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.repo.TransitionAgentProductionArtifact(scope, repository.ArtifactTransition{
			ArtifactID: artifact.ID, ExpectedStatus: queued.Status, NextStatus: model.AgentProductionArtifactSucceeded,
			ExpectedAttempt: queued.Attempt, NextAttempt: queued.Attempt, ResourceID: resource.ID, Now: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	artifacts, err := svc.repo.AgentProductionArtifactsForVersion(scope, record.Plan.PlanKey, record.Plan.Version)
	if err != nil {
		t.Fatal(err)
	}
	return svc, db, scope, record.Plan, artifacts, func(value string) { decision = value }
}

func productionCanvasCommitDecision(t *testing.T, toolCallID string, plan model.AgentProductionPlanVersion, artifacts []model.AgentProductionArtifact, baseRevision int64) string {
	t.Helper()
	artifactIDs := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		artifactIDs = append(artifactIDs, artifact.ID)
	}
	arguments, err := json.Marshal(agentProductionCanvasCommitArguments{PlanKey: plan.PlanKey, PlanVersion: plan.Version, BaseRevision: baseRevision, ArtifactIDs: artifactIDs})
	if err != nil {
		t.Fatal(err)
	}
	decision := agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{
		ToolCallID: toolCallID, ToolName: agentruntime.ToolCanvasCommit, ActionVersion: 1, Arguments: arguments,
		ExpectedDelivery: agentRuntimeTestCanvasDelivery(),
	}}
	encoded, err := json.Marshal(decision)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
