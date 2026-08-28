package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

func TestRunSpecialistUsesExactParentModelRecordAndPersistsUsage(t *testing.T) {
	request := specialistRuntimeRequestFixture("runtime-token-agent-model", "deepseek-v4-flash")
	providerRequestID := "chatcmpl-specialist-provider-request"
	response := specialistRuntimeResultJSON(t, request, providerRequestID)
	var requestedModel string
	providerCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		providerCalls++
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(httpRequest.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		requestedModel = body.Model
		writeAgentRuntimeChatStream(t, writer, providerRequestID, response, 300, 21, 0)
	}))
	defer server.Close()

	svc, db, fixture := newSpecialistRuntimeFixture(t, server.URL, request)
	parentRun := specialistParentRun(t, svc, db, fixture.channelModel, request)

	completion, err := svc.RunSpecialist(context.Background(), specialistRuntimeScope(), parentRun, request)
	if err != nil {
		t.Fatal(err)
	}
	if requestedModel != parentRun.ModelKey {
		t.Fatalf("provider model = %q, want %q", requestedModel, parentRun.ModelKey)
	}
	if completion.Run.ModelRecordID != parentRun.ModelRecordID || completion.Run.ModelKey != parentRun.ModelKey {
		t.Fatalf("specialist model = %q/%q, want %q/%q", completion.Run.ModelRecordID, completion.Run.ModelKey, parentRun.ModelRecordID, parentRun.ModelKey)
	}
	if completion.Run.Status != model.AgentSpecialistRunSucceeded || len(completion.Revisions) != 1 {
		t.Fatalf("completion = %#v", completion)
	}
	var order model.BillingOrder
	if err := db.First(&order, "id = ?", completion.Run.BillingOrderID).Error; err != nil {
		t.Fatal(err)
	}
	if total := order.InputTokens + order.OutputTokens; total != 321 {
		t.Fatalf("specialist total tokens = %d, want 321", total)
	}
	if order.TaskID != completion.Run.TaskID || order.Scene != agentSpecialistModelTaskType || !strings.Contains(order.IdempotencyKey, request.SpecialistRunID) {
		t.Fatalf("billing facts = %#v", order)
	}
	var task model.Task
	if err := db.First(&task, "id = ?", completion.Run.TaskID).Error; err != nil {
		t.Fatal(err)
	}
	if task.Audience != model.TaskAudienceInternal || task.Type != agentSpecialistModelTaskType || task.Status != model.TaskStatusSucceeded {
		t.Fatalf("task facts = %#v", task)
	}
	var visibleDeltas int64
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ? AND kind = ?", specialistRuntimeScope().RunID, agentruntime.EventModelDelta).Count(&visibleDeltas).Error; err != nil {
		t.Fatal(err)
	}
	if visibleDeltas != 0 {
		t.Fatalf("specialist emitted %d user-visible deltas", visibleDeltas)
	}
	replayed, err := svc.RunSpecialist(context.Background(), specialistRuntimeScope(), parentRun, request)
	if err != nil {
		t.Fatal(err)
	}
	if providerCalls != 1 || replayed.Run.ID != completion.Run.ID || len(replayed.Revisions) != 1 || replayed.Revisions[0].ID != completion.Revisions[0].ID {
		t.Fatalf("replay = %#v, provider calls = %d", replayed, providerCalls)
	}
}

func TestRunSpecialistRejectsMalformedStructuredOutputWithoutArtifactRevision(t *testing.T) {
	request := specialistRuntimeRequestFixture("runtime-token-agent-model", "deepseek-v4-flash")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeAgentRuntimeChatStream(t, writer, "chatcmpl-specialist-malformed-request", `{"summary":"missing artifacts"}`, 20, 4, 0)
	}))
	defer server.Close()

	svc, db, fixture := newSpecialistRuntimeFixture(t, server.URL, request)
	parentRun := specialistParentRun(t, svc, db, fixture.channelModel, request)

	_, err := svc.RunSpecialist(context.Background(), specialistRuntimeScope(), parentRun, request)
	if !errors.Is(err, ErrSpecialistOutputInvalid) {
		t.Fatalf("RunSpecialist() error = %v, want %v", err, ErrSpecialistOutputInvalid)
	}
	var revisionCount int64
	if err := db.Model(&model.AgentArtifactRevision{}).
		Where("created_by_specialist_id = ?", request.SpecialistRunID).
		Count(&revisionCount).Error; err != nil {
		t.Fatal(err)
	}
	if revisionCount != 0 {
		t.Fatalf("artifact revision count = %d, want 0", revisionCount)
	}
	var run model.AgentSpecialistRun
	if err := db.First(&run, "id = ?", request.SpecialistRunID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != model.AgentSpecialistRunFailed || run.ErrorCode != "specialist_output_invalid" {
		t.Fatalf("specialist failure facts = %#v", run)
	}
}

func TestRunSpecialistRejectsMultipleArtifactsForSingleReviewStage(t *testing.T) {
	request := scriptSpecialistRuntimeRequestFixture("runtime-token-agent-model", "deepseek-v4-flash")
	var result agentruntime.SpecialistResult
	if err := json.Unmarshal([]byte(scriptSpecialistRuntimeResultJSON(t, request)), &result); err != nil {
		t.Fatal(err)
	}
	second := result.Artifacts[0]
	second.ArtifactKey = "alternate-script"
	result.Artifacts = append(result.Artifacts, second)
	response, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeAgentRuntimeChatStream(t, writer, "chatcmpl-specialist-multiple-artifacts", string(response), 40, 8, 0)
	}))
	defer server.Close()

	svc, db, fixture := newSpecialistRuntimeFixture(t, server.URL, request)
	parentRun := specialistParentRun(t, svc, db, fixture.channelModel, request)

	_, err = svc.RunSpecialist(context.Background(), specialistRuntimeScope(), parentRun, request)
	if !errors.Is(err, ErrSpecialistOutputInvalid) {
		t.Fatalf("RunSpecialist() error = %v, want %v", err, ErrSpecialistOutputInvalid)
	}
	var revisionCount int64
	if err := db.Model(&model.AgentArtifactRevision{}).Count(&revisionCount).Error; err != nil {
		t.Fatal(err)
	}
	if revisionCount != 0 {
		t.Fatalf("artifact revision count = %d, want 0", revisionCount)
	}
}

func TestFirstVisibleReviewIsScriptBundle(t *testing.T) {
	request := scriptSpecialistRuntimeRequestFixture("runtime-token-agent-model", "deepseek-v4-flash")
	response := scriptSpecialistRuntimeResultJSON(t, request)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeAgentRuntimeChatStream(t, writer, "chatcmpl-script-review", response, 280, 35, 0)
	}))
	defer server.Close()

	svc, db, fixture := newSpecialistRuntimeFixture(t, server.URL, request)
	parentRun := specialistParentRun(t, svc, db, fixture.channelModel, request)
	completion, err := svc.RunSpecialist(context.Background(), specialistRuntimeScope(), parentRun, request)
	if err != nil {
		t.Fatal(err)
	}

	var item model.AgentTimelineItem
	if err := db.Where("run_id = ? AND kind = ?", specialistRuntimeScope().RunID, model.AgentTimelineItemArtifact).
		Order("ordinal ASC").First(&item).Error; err != nil {
		t.Fatal(err)
	}
	var content agentruntime.ArtifactReviewContent
	if err := json.Unmarshal([]byte(item.ContentJSON), &content); err != nil {
		t.Fatal(err)
	}
	if content.ContentType != "artifact_review" || content.ArtifactSchema != agentruntime.ArtifactSchemaScriptBundleV1 ||
		content.RevisionID != completion.Revisions[0].ID || content.StageID != request.StageID {
		t.Fatalf("artifact review content = %#v", content)
	}
	var stage model.AgentProductionStage
	if err := db.First(&stage, "id = ?", request.StageID).Error; err != nil {
		t.Fatal(err)
	}
	if stage.Status != agentruntime.StageAwaitingReview || stage.ReviewRevisionID != completion.Revisions[0].ID ||
		content.StageVersion != stage.Version {
		t.Fatalf("review stage = %#v, content = %#v", stage, content)
	}
	records, err := svc.repo.AgentTimelineEventsAfter(specialistRuntimeScope(), item.SourceEventSequence-1, 10)
	if err != nil || len(records) != 1 {
		t.Fatalf("first replay records = %#v, error = %v", records, err)
	}
	firstReplay, err := ProjectAgentEvent(specialistRuntimeScope().ThreadID, records[0].Event, records[0].Item, CurrentAgentUIProtocolVersion)
	if err != nil {
		t.Fatal(err)
	}
	reconnected, err := svc.repo.AgentTimelineEventsAfter(specialistRuntimeScope(), item.SourceEventSequence-1, 10)
	if err != nil || len(reconnected) != 1 {
		t.Fatalf("reconnected records = %#v, error = %v", reconnected, err)
	}
	secondReplay, err := ProjectAgentEvent(specialistRuntimeScope().ThreadID, reconnected[0].Event, reconnected[0].Item, CurrentAgentUIProtocolVersion)
	if err != nil {
		t.Fatal(err)
	}
	if firstReplay.Sequence != item.SourceEventSequence || secondReplay.Sequence != item.SourceEventSequence ||
		firstReplay.ItemID != item.ID || secondReplay.ItemID != item.ID || string(firstReplay.Payload) != string(secondReplay.Payload) {
		t.Fatalf("SSE replay mismatch: first=%#v second=%#v", firstReplay, secondReplay)
	}
}

func newSpecialistRuntimeFixture(t *testing.T, endpointURL string, request agentruntime.SpecialistRequest) (*Service, *gorm.DB, agentRuntimeServiceFixture) {
	t.Helper()
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	svc, db, fixture := newAgentRuntimeServiceFixture(t, endpointURL)
	if err := database.EnsureAgentProductionRuntimeSchema(db); err != nil {
		t.Fatal(err)
	}
	item, _ := configureTokenBilledAgentFixture(t, svc, db, fixture)
	fixture.channelModel = item
	now := time.Now().UTC()
	project := model.Project{
		ID: specialistRuntimeScope().DomainProjectID, UserID: specialistRuntimeScope().ActorUserID,
		Name: "Specialist Runtime Project", Type: "short-drama", Status: model.ProjectStatusActive,
		Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", specialistRuntimeScope().CanvasID).Update("project_id", specialistRuntimeScope().DomainProjectID).Error; err != nil {
		t.Fatal(err)
	}
	if request.ExpectedOutputSchema == agentruntime.ArtifactSchemaVisualEvidenceV1 {
		if len(request.InputRevisions) != 1 {
			t.Fatalf("visual specialist input revisions = %#v, want exactly one", request.InputRevisions)
		}
		source, err := svc.repo.AppendArtifactRevision(specialistRuntimeScope(), request.InputRevisions[0].ArtifactID, 0, agentruntime.ArtifactDraft{
			ArtifactKey: "specialist-source", Kind: "source_image", SchemaVersion: 1,
			Payload: json.RawMessage(`{"caption":"雨夜街道中的人物参考图"}`), ResourceID: "resource-specialist-source",
			UpstreamRevisions: []agentruntime.ArtifactRevisionRef{}, SkillVersions: []agentruntime.SkillSelection{},
		})
		if err != nil {
			t.Fatal(err)
		}
		if source.ID != request.InputRevisions[0].RevisionID {
			t.Fatalf("visual specialist source revision = %q, want %q", source.ID, request.InputRevisions[0].RevisionID)
		}
	}
	return svc, db, fixture
}

func specialistParentRun(t *testing.T, svc *Service, db *gorm.DB, item model.ChannelModel, request agentruntime.SpecialistRequest) model.AgentRun {
	t.Helper()
	scope := specialistRuntimeScope()
	record, err := svc.repo.CreateAgentRun(repository.CreateAgentRunInput{Scope: scope, ClientRequestID: "specialist-parent-request", Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	updates := map[string]any{
		"status": agentruntime.RunRunning, "model_record_id": item.ID, "model_key": item.ModelKey,
		"tool_schema_version": agentruntime.ProductionToolSchemaVersion, "runtime_version": agentruntime.ProductionRuntimeVersion,
		"policy_version": agentruntime.ProductionPolicyVersion, "max_steps": 24,
	}
	if err := db.Model(&model.AgentRun{}).Where("id = ?", record.Run.ID).Updates(updates).Error; err != nil {
		t.Fatal(err)
	}
	graph, err := svc.repo.AppendProductionGraphVersion(scope, 0, agentruntime.ProductionGraphDraft{
		GraphKey: "specialist-test-graph",
		Stages: []agentruntime.ProductionStageDraft{{
			StageKey: "visual-analysis", SpecialistKey: request.SpecialistKey,
			DependsOnStageKeys: []string{}, InputRevisions: request.InputRevisions,
			ExpectedDelivery: request.ExpectedDelivery, ReviewPolicy: agentruntime.ReviewRequired, CostPolicy: agentruntime.CostNone,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	requestStageID := graph.Stages[0].ID
	if request.StageID != requestStageID {
		t.Fatalf("request stage id = %q, fixture stage id = %q", request.StageID, requestStageID)
	}
	stored, err := svc.repo.AgentRunForScope(scope)
	if err != nil {
		t.Fatal(err)
	}
	return *stored
}

func specialistRuntimeScope() agentruntime.Scope {
	return agentruntime.Scope{
		TenantKind: agentruntime.TenantPersonal, TenantID: "runtime-user", ActorUserID: "runtime-user",
		DomainProjectID: "runtime-project", CanvasID: "runtime-canvas", ThreadID: "runtime-specialist-thread", RunID: "runtime-specialist-parent",
		Access: agentruntime.AccessGrant{Level: agentruntime.AccessManager, SubscriptionActive: true},
	}
}

func specialistRuntimeRequestFixture(modelRecordID string, modelKey string) agentruntime.SpecialistRequest {
	scope := specialistRuntimeScope()
	stageID := repositoryProductionStageIDForTest(scope, "specialist-test-graph", "visual-analysis")
	sourceRevision := specialistVisualSourceRevisionFixture(scope)
	return agentruntime.SpecialistRequest{
		SpecialistRunID: "specialist-run-visual-1", StageID: stageID,
		SpecialistKey: agentruntime.SpecialistVisual, SpecialistVersion: 1,
		ParentModelRecordID: modelRecordID, ParentModelKey: modelKey,
		Objective:      "分析已冻结视觉输入并形成结构化证据",
		InputRevisions: []agentruntime.ArtifactRevisionRef{sourceRevision},
		LoadedSkills: []agentruntime.SkillSelection{{
			Dir: "visual-evidence-analysis", Name: "视觉证据分析", Description: "形成结构化视觉证据", Instructions: "只基于真实视觉输入形成可追溯证据。",
			Version: 1, Checksum: "f96f5010f4ae35a614425b182b060ae328eb572b16e98fde4c36af1dc816e857",
			CapabilityManifest: agentruntime.SkillCapabilityManifest{Specialists: []agentruntime.SpecialistKey{agentruntime.SpecialistVisual}, Tools: []agentruntime.AgentToolName{agentruntime.ToolVisionAnalyze}, ArtifactSchemas: []string{"visual_evidence.v1"}},
			SourceKind:         "original", SourceRevision: "test-v1", SourceLicense: "HMaigc-Proprietary", PublishedAt: "2026-08-27T00:00:00Z",
		}},
		ToolAllowlist: []agentruntime.AgentToolName{agentruntime.ToolVisionAnalyze}, ExpectedOutputSchema: "visual_evidence.v1",
		ExpectedDelivery: agentruntime.ExpectedDelivery{Kind: agentruntime.DeliveryAnswer, CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}}},
	}
}

func scriptSpecialistRuntimeRequestFixture(modelRecordID string, modelKey string) agentruntime.SpecialistRequest {
	request := specialistRuntimeRequestFixture(modelRecordID, modelKey)
	request.SpecialistRunID = "specialist-run-script-1"
	request.SpecialistKey = agentruntime.SpecialistNarrative
	request.Objective = "根据已冻结需求形成完整剧本包与角色目录"
	request.InputRevisions = []agentruntime.ArtifactRevisionRef{}
	request.ToolAllowlist = []agentruntime.AgentToolName{}
	request.ExpectedOutputSchema = agentruntime.ArtifactSchemaScriptBundleV1
	request.LoadedSkills[0].Dir = "narrative-production"
	request.LoadedSkills[0].Name = "剧本生产"
	request.LoadedSkills[0].Description = "生成结构化剧本与角色目录"
	request.LoadedSkills[0].Instructions = "只基于已冻结输入生成完整剧本包。"
	checksum := sha256.Sum256([]byte(request.LoadedSkills[0].Instructions))
	request.LoadedSkills[0].Checksum = hex.EncodeToString(checksum[:])
	request.LoadedSkills[0].CapabilityManifest = agentruntime.SkillCapabilityManifest{
		Specialists:     []agentruntime.SpecialistKey{agentruntime.SpecialistNarrative},
		Tools:           []agentruntime.AgentToolName{},
		ArtifactSchemas: []string{agentruntime.ArtifactSchemaScriptBundleV1},
	}
	return request
}

func specialistRuntimeResultJSON(t *testing.T, request agentruntime.SpecialistRequest, providerRequestID string) string {
	t.Helper()
	result := agentruntime.SpecialistResult{
		Summary: "视觉证据已形成",
		Artifacts: []agentruntime.ArtifactDraft{{
			ArtifactKey: "visual-evidence", Kind: "visual_evidence", SchemaVersion: 1,
			Payload:           visualEvidencePayloadFixture(t, request.InputRevisions[0], request.ParentModelRecordID, providerRequestID, "人物站在雨夜街道中央"),
			UpstreamRevisions: request.InputRevisions, ModelRequestIdentity: providerRequestID, SkillVersions: request.LoadedSkills,
		}},
		Delivery: agentruntime.DeliveryEvidence{FinalMessage: "视觉证据已形成"}, NextActions: []agentruntime.RequiredAction{},
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func specialistVisualSourceRevisionFixture(scope agentruntime.Scope) agentruntime.ArtifactRevisionRef {
	const artifactID = "specialist-source-image"
	return agentruntime.ArtifactRevisionRef{
		ArtifactID: artifactID,
		RevisionID: repositoryFactIDForTest(
			"production-artifact-revision", string(scope.TenantKind), scope.TenantID, scope.ActorUserID,
			scope.DomainProjectID, scope.CanvasID, scope.ThreadID, scope.RunID, artifactID, "1",
		),
	}
}

func visualEvidencePayloadFixture(
	t *testing.T,
	sourceRevision agentruntime.ArtifactRevisionRef,
	modelRecordID string,
	requestIdentity string,
	actionState string,
) json.RawMessage {
	t.Helper()
	evidence := agentruntime.VisualEvidence{
		SourceRevision: sourceRevision,
		Characters: []agentruntime.VisualCharacter{{
			Key: "character-1", Name: "林夏", Clothing: "深色风衣", Hair: "黑色长发",
			StableFeatures: []string{"左眼下方有浅痣"},
		}},
		IdentityEvidence: []agentruntime.VisualIdentityEvidence{{
			CharacterKey: "character-1", Observations: []string{"人物位于画面中央"},
		}},
		Scene:            agentruntime.VisualSceneEvidence{Key: "scene-1", Description: "雨夜街道"},
		Props:            []agentruntime.VisualPropEvidence{},
		SpatialRelations: []agentruntime.VisualSpatialRelation{},
		Shot: agentruntime.VisualShotEvidence{
			ShotSize: "中景", Angle: "平视", Composition: "中心构图", ScreenDirection: "面向画面右侧",
			Gaze: "看向前方", FirstFrameCondition: "人物静止", LastFrameCondition: "人物静止",
		},
		ActionState: actionState, OCRText: []string{}, Uncertainties: []agentruntime.VisualEvidenceIssue{},
		Conflicts: []agentruntime.VisualEvidenceIssue{}, ConfidenceBasisPoints: 9200,
		VisionModelRecordID: modelRecordID, RequestIdentity: requestIdentity,
	}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func scriptSpecialistRuntimeResultJSON(t *testing.T, request agentruntime.SpecialistRequest) string {
	t.Helper()
	result := agentruntime.SpecialistResult{
		Summary: "完整剧本与角色目录已生成，等待确认",
		Artifacts: []agentruntime.ArtifactDraft{{
			ArtifactKey: "script", Kind: "script_bundle", SchemaVersion: 1,
			Payload: json.RawMessage(`{
				"title":"雨夜追踪",
				"logline":"女记者在暴雨夜追查失踪案",
				"script":"第一场：林夏进入废弃车站。",
				"characters":[{"key":"hero","name":"林夏","description":"冷静敏锐的女记者"}],
				"scenes":[{"key":"station","name":"废弃车站","description":"暴雨中的旧站台"}],
				"props":[],
				"voiceRoles":[]
			}`),
			UpstreamRevisions: []agentruntime.ArtifactRevisionRef{}, SkillVersions: request.LoadedSkills,
		}},
		Delivery:    agentruntime.DeliveryEvidence{FinalMessage: "完整剧本与角色目录已生成"},
		NextActions: []agentruntime.RequiredAction{},
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func repositoryProductionStageIDForTest(scope agentruntime.Scope, graphKey string, stageKey string) string {
	graphID := repositoryFactIDForTest("production-graph-version", string(scope.TenantKind), scope.TenantID, scope.ActorUserID, scope.DomainProjectID, scope.CanvasID, scope.ThreadID, scope.RunID, graphKey, "1")
	return repositoryFactIDForTest("production-stage", graphID, stageKey)
}

func repositoryFactIDForTest(namespace string, parts ...string) string {
	// Mirrors the repository's deterministic fact identity without exposing a production helper.
	digest := sha256.New()
	for _, part := range append([]string{namespace}, parts...) {
		_, _ = digest.Write([]byte(part))
		_, _ = digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))
}
