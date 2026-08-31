package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

func TestCanvasProjectCommitsApprovedExactArtifactRevisionOnce(t *testing.T) {
	skipRetiredAgentExecutionGraph(t)
	var decision string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeAgentRuntimeChatStream(t, writer, "chatcmpl-canvas-project", decision, 0, 0, 0)
	}))
	t.Cleanup(server.Close)
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	svc, db, _ := newAgentRuntimeServiceFixture(t, server.URL)
	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "canvas-project-current", UserMessage: "把批准的剧本放到画布",
		Configuration: AgentRuntimeConfigurationInput{ExecutionMode: agentruntime.ExecutionAutomatic},
	})
	if err != nil {
		t.Fatal(err)
	}
	artifactDelivery := agentruntime.ExpectedDelivery{
		Kind:              agentruntime.DeliveryGeneratedAsset,
		RequiredArtifacts: []agentruntime.ArtifactKind{agentruntime.ArtifactText},
		CompletionCriteria: []agentruntime.DeliveryCriterion{{
			Fact: agentruntime.DeliveryFactArtifactRevision, Artifact: agentruntime.ArtifactText,
		}},
	}
	graph, err := svc.repo.AppendProductionGraphVersion(scope, 0, agentruntime.ProductionGraphDraft{
		GraphKey: "short-film-production",
		Stages: []agentruntime.ProductionStageDraft{{
			StageKey: "script", SpecialistKey: agentruntime.SpecialistNarrative,
			DependsOnStageKeys: []string{}, InputRevisions: []agentruntime.ArtifactRevisionRef{},
			ExpectedDelivery: artifactDelivery, ReviewPolicy: agentruntime.ReviewRequired, CostPolicy: agentruntime.CostNone,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	revision, err := svc.repo.AppendArtifactRevision(scope, "artifact-script-current", 0, agentruntime.ArtifactDraft{
		ArtifactKey: "main-script", Kind: "script_bundle", SchemaVersion: 1,
		Payload:           json.RawMessage(`{"title":"雨夜来信","logline":"失联多年的来信在雨夜重现。","script":"顾棠拆开信封。","characters":[{"key":"gu-tang","name":"顾棠","description":"年轻调查员"}],"scenes":[],"props":[],"voiceRoles":[]}`),
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{}, SkillVersions: []agentruntime.SkillSelection{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentProductionStage{}).Where("id = ?", graph.Stages[0].ID).Updates(map[string]interface{}{
		"status":             agentruntime.StageApproved,
		"version":            2,
		"review_revision_id": revision.ID,
	}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	reviewJSON, err := json.Marshal(agentruntime.StageReviewResolutionContent{
		ContentType: agentruntime.StageReviewContentType, StageID: graph.Stages[0].ID, StageVersion: 1,
		RevisionID: revision.ID, Decision: agentruntime.StageReviewApprove, ClientRequestID: "approve-script",
		ResultStageVersion: 2, ResultStatus: agentruntime.StageApproved,
		ResultReviewRevisionID: revision.ID, ResultUpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AgentTimelineItem{
		ID: "timeline-approve-script", TenantKind: scope.TenantKind, TenantID: scope.TenantID,
		ThreadID: scope.ThreadID, RunID: scope.RunID, Kind: model.AgentTimelineItemApproval,
		Status: model.AgentTimelineItemCompleted, Ordinal: 100, SourceEventSequence: 100,
		ContentJSON: string(reviewJSON), StartedAt: now, CompletedAt: &now, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	delivery := agentruntime.ExpectedDelivery{
		Kind: agentruntime.DeliveryCanvasChange, TargetCanvasID: scope.CanvasID,
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactCanvasRevision}},
	}
	arguments, err := json.Marshal(CanvasProjectArguments{
		ArtifactRevisions: []agentruntime.ArtifactRevisionRef{{ArtifactID: revision.ArtifactID, RevisionID: revision.ID}},
		BaseRevision:      7, ExpectedDelivery: delivery,
	})
	if err != nil {
		t.Fatal(err)
	}
	encodedDecision, err := json.Marshal(agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &agentruntime.ToolCallDecision{
		ToolCallID: "project-approved-script", ToolName: agentruntime.ToolCanvasProject, ActionVersion: 1,
		Arguments: arguments, ExpectedDelivery: delivery,
	}})
	if err != nil {
		t.Fatal(err)
	}
	decision = string(encodedDecision)
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	progress, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.LastToolResult == nil || !progress.State.LastToolResult.Succeeded {
		t.Fatalf("canvas project state = %#v", progress.State)
	}
	result, err := decodeAgentCanvasProjectionResult(progress.State.LastToolResult.Output)
	if err != nil {
		t.Fatal(err)
	}
	if result.CanvasID != scope.CanvasID || result.BaseRevision != 7 || result.CommittedRevision != 8 ||
		len(result.Bindings) != 1 || result.Bindings[0].RevisionID != revision.ID {
		t.Fatalf("canvas project result = %#v", result)
	}
	evidence, err := svc.agentRuntimeDeliveryEvidence(scope, "")
	if err != nil {
		t.Fatal(err)
	}
	if evidence.CanvasID != scope.CanvasID || evidence.CanvasRevision != 8 {
		t.Fatalf("canvas project delivery evidence = %#v", evidence)
	}
	if _, err := svc.CoordinatePendingAgentTool(scope, CoordinateAgentToolInput{
		ToolCallID: "project-approved-script", ActionVersion: 1,
	}); err != nil {
		t.Fatal(err)
	}
	var canvas model.CanvasProject
	if err := db.First(&canvas, "id = ?", scope.CanvasID).Error; err != nil {
		t.Fatal(err)
	}
	if canvas.Revision != 8 || !json.Valid([]byte(canvas.PayloadJSON)) ||
		!containsAll(canvas.PayloadJSON, revision.ArtifactID, revision.ID, result.Bindings[0].NodeID) {
		t.Fatalf("projected canvas = revision:%d payload:%s", canvas.Revision, canvas.PayloadJSON)
	}
	if started.Run.ToolSchemaVersion != agentruntime.CurrentToolSchemaVersion {
		t.Fatalf("tool schema version = %d", started.Run.ToolSchemaVersion)
	}
}

func containsAll(value string, expected ...string) bool {
	for _, item := range expected {
		if !strings.Contains(value, item) {
			return false
		}
	}
	return true
}

func TestFreezeAgentCanvasProjectDecisionArgumentsCanonicalizesExactUniqueRevisions(t *testing.T) {
	t.Parallel()

	expected := agentruntime.ExpectedDelivery{
		Kind:           agentruntime.DeliveryCanvasChange,
		TargetCanvasID: "canvas-projection",
		CompletionCriteria: []agentruntime.DeliveryCriterion{{
			Fact: agentruntime.DeliveryFactCanvasRevision,
		}},
	}
	call := &agentruntime.ToolCallDecision{
		ToolCallID: "project-artifacts", ToolName: agentruntime.ToolCanvasProject, ActionVersion: 1,
		Arguments:        json.RawMessage(`{"artifactRevisions":[{"artifactId":"artifact-video","revisionId":"revision-video"},{"artifactId":"artifact-script","revisionId":"revision-script"}],"baseRevision":4,"expectedDelivery":{"kind":"canvas_change","targetCanvasId":"canvas-projection","completionCriteria":[{"fact":"canvas_revision"}]}}`),
		ExpectedDelivery: expected,
	}

	frozen, err := freezeAgentCanvasProjectDecisionArguments("canvas-projection", call)
	if err != nil {
		t.Fatal(err)
	}
	arguments, err := decodeCanvasProjectArguments(frozen)
	if err != nil {
		t.Fatal(err)
	}
	if arguments.ArtifactRevisions[0].ArtifactID != "artifact-script" || arguments.ArtifactRevisions[1].ArtifactID != "artifact-video" {
		t.Fatalf("artifact revisions were not canonicalized: %#v", arguments.ArtifactRevisions)
	}

	call.Arguments = json.RawMessage(`{"artifactRevisions":[{"artifactId":"artifact-script","revisionId":"revision-1"},{"artifactId":"artifact-script","revisionId":"revision-2"}],"baseRevision":4,"expectedDelivery":{"kind":"canvas_change","targetCanvasId":"canvas-projection","completionCriteria":[{"fact":"canvas_revision"}]}}`)
	if _, err := freezeAgentCanvasProjectDecisionArguments("canvas-projection", call); !errors.Is(err, ErrAgentRuntimeToolArgumentsInvalid) {
		t.Fatalf("duplicate artifact error = %v, want %v", err, ErrAgentRuntimeToolArgumentsInvalid)
	}

	call.Arguments = json.RawMessage(`{"artifactRevisions":[{"artifactId":"artifact-script","revisionId":"revision-script"}],"baseRevision":4,"expectedDelivery":{"kind":"canvas_change","targetCanvasId":"other-canvas","completionCriteria":[{"fact":"canvas_revision"}]}}`)
	if _, err := freezeAgentCanvasProjectDecisionArguments("canvas-projection", call); !errors.Is(err, ErrAgentRuntimeToolArgumentsInvalid) {
		t.Fatalf("canvas delivery mismatch error = %v, want %v", err, ErrAgentRuntimeToolArgumentsInvalid)
	}
}

func TestBuildAgentCanvasProjectionPatchPreservesExactArtifactAndMediaFacts(t *testing.T) {
	t.Parallel()

	scriptRef := agentruntime.ArtifactRevisionRef{ArtifactID: "artifact-script", RevisionID: "revision-script-2"}
	videoRef := agentruntime.ArtifactRevisionRef{ArtifactID: "artifact-video", RevisionID: "revision-video-1"}
	revisions := []model.AgentArtifactRevision{
		{
			ID: scriptRef.RevisionID, ArtifactID: scriptRef.ArtifactID, ArtifactKey: "main-script", Revision: 2,
			Kind: "script_bundle", SchemaVersion: 1,
			PayloadJSON:           `{"title":"雨夜来信","logline":"失联多年的来信在雨夜重现。","script":"顾棠拆开信封，雨声盖过她的呼吸。","characters":[{"key":"gu-tang","name":"顾棠","description":"年轻调查员"}],"scenes":[],"props":[],"voiceRoles":[]}`,
			UpstreamRevisionsJSON: `[]`, SkillVersionsJSON: `[]`, LifecycleStatus: model.AgentArtifactRevisionAwaitingReview,
		},
		{
			ID: videoRef.RevisionID, ArtifactID: videoRef.ArtifactID, ArtifactKey: "opening-video", Revision: 1,
			Kind: "media_candidate", SchemaVersion: 1, ResourceID: "resource-video",
			PayloadJSON:           `{"candidateKey":"opening-video-candidate","mediaKind":"video","providerRequestIdentity":"provider-request-1","resourceId":"resource-video","sourceTaskId":"task-video"}`,
			UpstreamRevisionsJSON: `[{"artifactId":"artifact-script","revisionId":"revision-script-2"}]`,
			SkillVersionsJSON:     `[]`, ModelRequestIdentity: "provider-request-1", LifecycleStatus: model.AgentArtifactRevisionAwaitingReview,
		},
	}
	resources := map[string]model.Resource{
		"resource-video": {
			ID: "resource-video", Kind: "video", Status: model.ResourceStatusReady,
			Provider: "oss", ObjectKey: "agent/video.mp4", ETag: "video-etag", MimeType: "video/mp4", DurationMs: 5_000,
		},
	}
	mediaArguments := map[agentruntime.ArtifactRevisionRef]agentMediaGenerationArguments{
		videoRef: {
			GenerationModel: agentruntime.GenerationModelSelection{ChannelID: "seedance-channel", Model: "seedance-2.0-mini"},
			Capability:      "video", Parameters: json.RawMessage(`{"prompt":"雨夜中缓慢推近顾棠","aspectRatio":"16:9","resolution":"720p","durationSeconds":5,"generateAudio":true}`),
		},
	}

	patch, bindings, err := buildAgentCanvasProjectionPatch("canvas-projection", revisions, resources, mediaArguments)
	if err != nil {
		t.Fatal(err)
	}
	if len(patch.UpsertNodes) != 2 || len(patch.UpsertConnections) != 1 || len(bindings) != 2 {
		t.Fatalf("projection counts: nodes=%d connections=%d bindings=%d", len(patch.UpsertNodes), len(patch.UpsertConnections), len(bindings))
	}

	var scriptNode, videoNode agentCanvasProjectionNode
	if err := json.Unmarshal(patch.UpsertNodes[0], &scriptNode); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(patch.UpsertNodes[1], &videoNode); err != nil {
		t.Fatal(err)
	}
	if scriptNode.Type != "script" || scriptNode.Title != "雨夜来信" || scriptNode.Metadata.ComposerContent == "" ||
		scriptNode.Metadata.ArtifactID != scriptRef.ArtifactID || scriptNode.Metadata.ArtifactRevisionID != scriptRef.RevisionID {
		t.Fatalf("script node = %#v", scriptNode)
	}
	if videoNode.Type != "video" || videoNode.Metadata.Content != "/api/resources/resource-video/file" ||
		videoNode.Metadata.StorageKey != "resource:resource-video" || videoNode.Metadata.DurationMS != 5_000 ||
		videoNode.Metadata.ChannelID != "seedance-channel" || videoNode.Metadata.Model != "seedance-2.0-mini" ||
		videoNode.Metadata.Size != "16:9" || videoNode.Metadata.VQuality != "720p" || videoNode.Metadata.Seconds != "5" ||
		videoNode.Metadata.GenerateAudio != "true" || videoNode.Metadata.ArtifactID != videoRef.ArtifactID ||
		videoNode.Metadata.ArtifactRevisionID != videoRef.RevisionID {
		t.Fatalf("video node = %#v", videoNode)
	}
	if scriptNode.ID != agentCanvasProjectionNodeID("canvas-projection", scriptRef.ArtifactID) ||
		videoNode.ID != agentCanvasProjectionNodeID("canvas-projection", videoRef.ArtifactID) {
		t.Fatalf("projection node identities = %q, %q", scriptNode.ID, videoNode.ID)
	}
	if bindings[0].RevisionID != scriptRef.RevisionID || bindings[1].RevisionID != videoRef.RevisionID ||
		bindings[0].ProjectionID == "" || bindings[1].ProjectionID == "" {
		t.Fatalf("projection bindings = %#v", bindings)
	}

	var connection productionCanvasConnection
	if err := json.Unmarshal(patch.UpsertConnections[0], &connection); err != nil {
		t.Fatal(err)
	}
	if connection.FromNodeID != scriptNode.ID || connection.ToNodeID != videoNode.ID {
		t.Fatalf("projection connection = %#v", connection)
	}

	revisions[0].ID = "revision-script-3"
	updated, _, err := buildAgentCanvasProjectionPatch("canvas-projection", revisions[:1], map[string]model.Resource{}, map[agentruntime.ArtifactRevisionRef]agentMediaGenerationArguments{})
	if err != nil {
		t.Fatal(err)
	}
	var updatedScript agentCanvasProjectionNode
	if err := json.Unmarshal(updated.UpsertNodes[0], &updatedScript); err != nil {
		t.Fatal(err)
	}
	if updatedScript.ID != scriptNode.ID || updatedScript.Metadata.ArtifactRevisionID != "revision-script-3" {
		t.Fatalf("stable artifact projection was not updated in place: %#v", updatedScript)
	}
}
