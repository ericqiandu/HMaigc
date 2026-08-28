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

type agentProductionHTTPFacts struct {
	fixture  *providerAccountHandlerFixture
	runID    string
	stage    model.AgentProductionStage
	artifact model.AgentArtifact
	revision model.AgentArtifactRevision
}

func TestStageReviewRejectsStaleRevisionAndUnknownFields(t *testing.T) {
	facts := openAgentProductionHTTPFacts(t, "review")
	path := "/api/agent/runs/" + facts.runID + "/stages/" + facts.stage.ID + "/reviews"

	unknown := facts.fixture.request(http.MethodPost, path,
		`{"stageVersion":3,"revisionId":"`+facts.revision.ID+`","decision":"approved","clientRequestId":"review-unknown","comment":"","extra":true}`,
		facts.fixture.userCookie, "")
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown-field status = %d, want %d, body = %s", unknown.Code, http.StatusBadRequest, unknown.Body.String())
	}
	nestedUnknown := facts.fixture.request(http.MethodPost, path,
		`{"stageVersion":3,"revisionId":"`+facts.revision.ID+`","decision":"approved","clientRequestId":"review-publication-unknown","comment":"","publicationIntent":{"publicationPurpose":"character-library","targetCategory":"character","targetBindingKey":"hero","extra":true}}`,
		facts.fixture.userCookie, "")
	if nestedUnknown.Code != http.StatusBadRequest {
		t.Fatalf("nested unknown-field status = %d, want %d, body = %s", nestedUnknown.Code, http.StatusBadRequest, nestedUnknown.Body.String())
	}

	stale := facts.fixture.request(http.MethodPost, path,
		`{"stageVersion":3,"revisionId":"revision-stale","decision":"approved","clientRequestId":"review-stale","comment":""}`,
		facts.fixture.userCookie, "")
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), `"errorCode":"stage_approval_revision_mismatch"`) {
		t.Fatalf("stale-revision status = %d, body = %s", stale.Code, stale.Body.String())
	}
}

func TestStageReviewRecognizesExactSelectedCandidateApprovalContract(t *testing.T) {
	facts := openAgentProductionHTTPFacts(t, "candidate-contract")
	path := "/api/agent/runs/" + facts.runID + "/stages/" + facts.stage.ID + "/reviews"
	body := `{"stageVersion":3,"revisionId":"` + facts.revision.ID + `","decision":"approved","selectedCandidateRevisionId":"candidate-revision","clientRequestId":"candidate-approval","comment":"","publicationIntent":{"publicationPurpose":"character-library","targetCategory":"character","targetBindingKey":"hero"}}`

	response := facts.fixture.request(http.MethodPost, path, body, facts.fixture.userCookie, "")
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"errorCode":"visual_candidate_selection_invalid"`) {
		t.Fatalf("candidate contract status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestStageReviewRejectsCrossScopeRevisionEvenWhenStagePointerIsCorrupt(t *testing.T) {
	owned := openAgentProductionHTTPFacts(t, "review-scope-owned")
	other := createAgentProductionHTTPRun(t, owned.fixture, "review-scope-other", false)
	if err := owned.fixture.db.Model(&model.AgentProductionStage{}).Where("id = ?", owned.stage.ID).
		Update("review_revision_id", other.revision.ID).Error; err != nil {
		t.Fatal(err)
	}

	path := "/api/agent/runs/" + owned.runID + "/stages/" + owned.stage.ID + "/reviews"
	response := owned.fixture.request(http.MethodPost, path,
		`{"stageVersion":3,"revisionId":"`+other.revision.ID+`","decision":"approved","clientRequestId":"review-cross-scope","comment":""}`,
		owned.fixture.userCookie, "")
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"errorCode":"stage_approval_revision_mismatch"`) {
		t.Fatalf("cross-scope revision status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestStageReviewPersistsSequencedResolutionAndReplaysIdentity(t *testing.T) {
	facts := openAgentProductionHTTPFacts(t, "approve")
	path := "/api/agent/runs/" + facts.runID + "/stages/" + facts.stage.ID + "/reviews"
	body := `{"stageVersion":3,"revisionId":"` + facts.revision.ID + `","decision":"approved","clientRequestId":"review-approve","comment":""}`

	first := facts.fixture.request(http.MethodPost, path, body, facts.fixture.userCookie, "")
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"status":"approved"`) || !strings.Contains(first.Body.String(), `"version":4`) {
		t.Fatalf("first review status = %d, body = %s", first.Code, first.Body.String())
	}
	replayed := facts.fixture.request(http.MethodPost, path, body, facts.fixture.userCookie, "")
	if replayed.Code != http.StatusOK || replayed.Body.String() != first.Body.String() {
		t.Fatalf("replayed review status = %d, body = %s, first = %s", replayed.Code, replayed.Body.String(), first.Body.String())
	}
	if err := facts.fixture.db.Model(&model.AgentProductionStage{}).Where("id = ?", facts.stage.ID).Updates(map[string]interface{}{
		"status": agentruntime.StageCompleted, "version": int64(5), "updated_at": time.Now().UTC().Add(time.Minute),
	}).Error; err != nil {
		t.Fatal(err)
	}
	afterProgress := facts.fixture.request(http.MethodPost, path, body, facts.fixture.userCookie, "")
	if afterProgress.Code != http.StatusOK || afterProgress.Body.String() != first.Body.String() {
		t.Fatalf("replay after stage progress status = %d, body = %s, first = %s", afterProgress.Code, afterProgress.Body.String(), first.Body.String())
	}

	var resolutionEvents int64
	if err := facts.fixture.db.Model(&model.AgentRunEvent{}).
		Where("run_id = ? AND kind = ?", facts.runID, agentruntime.EventApprovalDecided).
		Count(&resolutionEvents).Error; err != nil {
		t.Fatal(err)
	}
	if resolutionEvents != 1 {
		t.Fatalf("resolution events = %d, want 1", resolutionEvents)
	}

	conflict := facts.fixture.request(http.MethodPost, path,
		`{"stageVersion":3,"revisionId":"`+facts.revision.ID+`","decision":"approved","clientRequestId":"review-other","comment":""}`,
		facts.fixture.userCookie, "")
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), `"errorCode":"stage_review_conflict"`) {
		t.Fatalf("different identity status = %d, body = %s", conflict.Code, conflict.Body.String())
	}
}

func TestStageReviewStopAtomicallyInterruptsRun(t *testing.T) {
	facts := openAgentProductionHTTPFacts(t, "stop")
	path := "/api/agent/runs/" + facts.runID + "/stages/" + facts.stage.ID + "/reviews"
	body := `{"stageVersion":3,"revisionId":"` + facts.revision.ID + `","decision":"stopped","clientRequestId":"review-stop","comment":""}`

	response := facts.fixture.request(http.MethodPost, path, body, facts.fixture.userCookie, "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"stopped"`) {
		t.Fatalf("stop review status = %d, body = %s", response.Code, response.Body.String())
	}
	var run model.AgentRun
	if err := facts.fixture.db.First(&run, "id = ?", facts.runID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != agentruntime.RunCancelled {
		t.Fatalf("run status = %q, want %q", run.Status, agentruntime.RunCancelled)
	}
	var events []model.AgentRunEvent
	if err := facts.fixture.db.Where("run_id = ? AND kind IN ?", facts.runID,
		[]agentruntime.EventKind{agentruntime.EventApprovalDecided, agentruntime.EventRunInterrupted}).
		Order("sequence ASC").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Kind != agentruntime.EventApprovalDecided || events[1].Kind != agentruntime.EventRunInterrupted ||
		events[0].Sequence >= events[1].Sequence {
		t.Fatalf("stop events = %#v", events)
	}
	var approvalItems int64
	if err := facts.fixture.db.Model(&model.AgentTimelineItem{}).
		Where("run_id = ? AND kind = ? AND status = ?", facts.runID, model.AgentTimelineItemApproval, model.AgentTimelineItemCompleted).
		Count(&approvalItems).Error; err != nil {
		t.Fatal(err)
	}
	if approvalItems != 1 {
		t.Fatalf("approval items = %d, want 1", approvalItems)
	}

	replayed := facts.fixture.request(http.MethodPost, path, body, facts.fixture.userCookie, "")
	if replayed.Code != http.StatusOK || replayed.Body.String() != response.Body.String() {
		t.Fatalf("stop replay status = %d, body = %s, first = %s", replayed.Code, replayed.Body.String(), response.Body.String())
	}
}

func TestArtifactRevisionReadIsScopeBound(t *testing.T) {
	owned := openAgentProductionHTTPFacts(t, "artifact-owned")
	other := createAgentProductionHTTPRun(t, owned.fixture, "artifact-other", false)

	path := "/api/agent/runs/" + owned.runID + "/artifacts/" + owned.artifact.ID + "/revisions/" + owned.revision.ID
	response := owned.fixture.request(http.MethodGet, path, "", owned.fixture.userCookie, "")
	if response.Code != http.StatusOK {
		t.Fatalf("owned revision status = %d, body = %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data struct {
			ArtifactID      string          `json:"artifactId"`
			RevisionID      string          `json:"revisionId"`
			Payload         json.RawMessage `json:"payload"`
			LifecycleStatus string          `json:"lifecycleStatus"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.ArtifactID != owned.artifact.ID || envelope.Data.RevisionID != owned.revision.ID ||
		string(envelope.Data.Payload) != owned.revision.PayloadJSON || envelope.Data.LifecycleStatus != owned.revision.LifecycleStatus {
		t.Fatalf("revision response = %s", response.Body.String())
	}

	crossRunPath := "/api/agent/runs/" + owned.runID + "/artifacts/" + other.artifact.ID + "/revisions/" + other.revision.ID
	crossRun := owned.fixture.request(http.MethodGet, crossRunPath, "", owned.fixture.userCookie, "")
	if crossRun.Code != http.StatusNotFound {
		t.Fatalf("cross-run revision status = %d, body = %s", crossRun.Code, crossRun.Body.String())
	}
}

func openAgentProductionHTTPFacts(t *testing.T, suffix string) agentProductionHTTPFacts {
	t.Helper()
	fixture := openProviderAccountHandlerFixture(t)
	if err := database.EnsureAgentRuntimeIntegritySchema(fixture.db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureAgentProductionRuntimeSchema(fixture.db); err != nil {
		t.Fatal(err)
	}
	RegisterAgentRuntimeRoutes(fixture.router.Group("/api"), service.New(repository.New(fixture.db), t.TempDir()))
	return createAgentProductionHTTPRun(t, fixture, suffix, true)
}

func createAgentProductionHTTPRun(t *testing.T, fixture *providerAccountHandlerFixture, suffix string, withReviewStage bool) agentProductionHTTPFacts {
	t.Helper()
	canvasID := "production-canvas-" + suffix
	runID := "production-run-" + suffix
	createAgentRuntimeHandlerCanvas(t, fixture, canvasID)
	if err := fixture.db.Model(&model.CanvasProject{}).Where("id = ?", canvasID).
		Update("project_id", "production-project-"+suffix).Error; err != nil {
		t.Fatal(err)
	}
	repo := repository.New(fixture.db)
	svc := service.New(repo, t.TempDir())
	now := time.Now().UTC()
	scope, err := svc.AuthorizeAgentScope(fixture.userID, canvasID, "production-thread-"+suffix, runID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateAgentRun(repository.CreateAgentRunInput{Scope: scope, ClientRequestID: "production-request-" + suffix, Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.InitializeAgentRun(repository.InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "production-model-record", ModelKey: "production-model",
		MaxSteps: 16, ToolSchemaVersion: agentruntime.ProductionToolSchemaVersion,
		RuntimeVersion: agentruntime.ProductionRuntimeVersion, PolicyVersion: agentruntime.ProductionPolicyVersion,
		UserMessage: "制作短片", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	queued, err := repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	running, err := agentruntime.BeginModelRequest(queued)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitAgentRuntimeTransition(scope, queued, running, now.Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}

	artifact := model.AgentArtifact{
		ID: "artifact-" + suffix, TenantKind: scope.TenantKind, TenantID: scope.TenantID, ActorUserID: scope.ActorUserID,
		DomainProjectID: scope.DomainProjectID, CanvasID: scope.CanvasID, ThreadID: scope.ThreadID, RunID: scope.RunID,
		ArtifactKey: "script-" + suffix, Kind: "script_bundle", HeadRevision: 1,
		LifecycleStatus: model.AgentArtifactLifecycleActive, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	revision := model.AgentArtifactRevision{
		ID: "revision-" + suffix, TenantKind: scope.TenantKind, TenantID: scope.TenantID, ActorUserID: scope.ActorUserID,
		DomainProjectID: scope.DomainProjectID, CanvasID: scope.CanvasID, ThreadID: scope.ThreadID, RunID: scope.RunID,
		ArtifactID: artifact.ID, ArtifactKey: artifact.ArtifactKey, Revision: 1, Kind: artifact.Kind, SchemaVersion: 1,
		PayloadJSON: `{"title":"测试剧本"}`, UpstreamRevisionsJSON: `[]`, SkillVersionsJSON: `[]`,
		CreatedByRunID: scope.RunID, LifecycleStatus: model.AgentArtifactRevisionAwaitingReview, CreatedAt: now,
	}
	if err := fixture.db.Create(&artifact).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Create(&revision).Error; err != nil {
		t.Fatal(err)
	}

	facts := agentProductionHTTPFacts{fixture: fixture, runID: runID, artifact: artifact, revision: revision}
	if !withReviewStage {
		return facts
	}
	graph, err := repo.AppendProductionGraphVersion(scope, 0, agentruntime.ProductionGraphDraft{
		GraphKey: "short-film",
		Stages: []agentruntime.ProductionStageDraft{{
			StageKey: "script", SpecialistKey: agentruntime.SpecialistNarrative,
			ExpectedDelivery: agentruntime.ExpectedDelivery{
				Kind:               agentruntime.DeliveryAnswer,
				CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}},
			},
			ReviewPolicy: agentruntime.ReviewRequired, CostPolicy: agentruntime.CostNone,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	stage := graph.Stages[0]
	if err := fixture.db.Model(&model.AgentProductionStage{}).Where("id = ?", stage.ID).Updates(map[string]interface{}{
		"status": agentruntime.StageAwaitingReview, "version": int64(3), "review_revision_id": revision.ID, "updated_at": now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	stage.Status = agentruntime.StageAwaitingReview
	stage.Version = 3
	stage.ReviewRevisionID = revision.ID
	facts.stage = stage
	return facts
}
