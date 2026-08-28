package service

import (
	"context"
	"encoding/json"
	"errors"
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

func TestAssetBindingRejectsUnconfirmedAndCrossScopeResource(t *testing.T) {
	tests := []struct {
		name          string
		confirmed     bool
		resourceOwner string
	}{
		{name: "unconfirmed", confirmed: false, resourceOwner: specialistRuntimeScope().ActorUserID},
		{name: "cross-scope", confirmed: true, resourceOwner: "other-user"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Fatal("provider must not be called while confirming bindings")
			}))
			defer server.Close()

			request := assetBindingSpecialistRuntimeRequestFixture("runtime-token-agent-model", "deepseek-v4-flash")
			svc, db, fixture := newSpecialistRuntimeFixture(t, server.URL, request)
			_ = specialistParentRun(t, svc, db, fixture.channelModel, request)
			bindings, stage, reviewRevision := prepareAssetBindingReviewFixture(t, svc, db, testCase.resourceOwner)
			bindings.Confirmed = testCase.confirmed

			_, err := svc.ConfirmAssetBindings(
				context.Background(), specialistRuntimeScope(), stage.ID, stage.Version, reviewRevision.ID, bindings,
			)
			if !errors.Is(err, ErrAssetBindingUnconfirmed) {
				t.Fatalf("ConfirmAssetBindings() error = %v, want %v", err, ErrAssetBindingUnconfirmed)
			}
			var count int64
			if countErr := db.Model(&model.AgentAssetBindingRevision{}).Count(&count).Error; countErr != nil {
				t.Fatal(countErr)
			}
			if count != 0 {
				t.Fatalf("binding revisions = %d, want 0", count)
			}
		})
	}
}

func TestAssetBindingPersistsConfirmedScopedRevision(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("provider must not be called while confirming bindings")
	}))
	defer server.Close()

	request := assetBindingSpecialistRuntimeRequestFixture("runtime-token-agent-model", "deepseek-v4-flash")
	svc, db, fixture := newSpecialistRuntimeFixture(t, server.URL, request)
	_ = specialistParentRun(t, svc, db, fixture.channelModel, request)
	bindings, stage, reviewRevision := prepareAssetBindingReviewFixture(t, svc, db, specialistRuntimeScope().ActorUserID)

	stored, err := svc.ConfirmAssetBindings(
		context.Background(), specialistRuntimeScope(), stage.ID, stage.Version, reviewRevision.ID, bindings,
	)
	if err != nil {
		t.Fatal(err)
	}
	if stored.BindingKey != bindings.BindingKey || stored.Revision != 1 ||
		stored.CreatedBySpecialistID != reviewRevision.CreatedBySpecialistID || stored.LifecycleStatus != model.AgentAssetBindingRevisionConfirmed {
		t.Fatalf("binding revision = %#v", stored)
	}
	var decoded agentruntime.AssetBindingSet
	if err := json.Unmarshal([]byte(stored.BindingsJSON), &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Confirmed || decoded.Entries[0].ResourceID != bindings.Entries[0].ResourceID {
		t.Fatalf("stored bindings = %#v", decoded)
	}

	replayed, err := svc.ConfirmAssetBindings(
		context.Background(), specialistRuntimeScope(), stage.ID, stage.Version, reviewRevision.ID, bindings,
	)
	if err != nil || replayed.ID != stored.ID {
		t.Fatalf("binding replay = %#v, error = %v", replayed, err)
	}
}

func TestAssetBindingRejectsReviewWithoutMatchingSucceededAssetSpecialist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("provider must not be called while confirming bindings")
	}))
	defer server.Close()

	request := assetBindingSpecialistRuntimeRequestFixture("runtime-token-agent-model", "deepseek-v4-flash")
	svc, db, fixture := newSpecialistRuntimeFixture(t, server.URL, request)
	_ = specialistParentRun(t, svc, db, fixture.channelModel, request)
	bindings, stage, reviewRevision := prepareAssetBindingReviewFixture(t, svc, db, specialistRuntimeScope().ActorUserID)
	if err := db.Model(&model.AgentSpecialistRun{}).Where("id = ?", reviewRevision.CreatedBySpecialistID).
		Update("specialist_key", agentruntime.SpecialistNarrative).Error; err != nil {
		t.Fatal(err)
	}

	_, err := svc.ConfirmAssetBindings(
		context.Background(), specialistRuntimeScope(), stage.ID, stage.Version, reviewRevision.ID, bindings,
	)
	if !errors.Is(err, ErrAssetBindingUnconfirmed) {
		t.Fatalf("ConfirmAssetBindings() error = %v, want %v", err, ErrAssetBindingUnconfirmed)
	}
}

func TestStageRevisionCreatesFreshSpecialistRunAndAppendsRevision(t *testing.T) {
	request := scriptSpecialistRuntimeRequestFixture("runtime-token-agent-model", "deepseek-v4-flash")
	providerCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		providerCalls++
		response := scriptSpecialistRuntimeResultJSON(t, request)
		if providerCalls == 2 {
			response = revisedScriptSpecialistRuntimeResultJSON(t, request)
		}
		writeAgentRuntimeChatStream(t, writer, "chatcmpl-stage-revision", response, 260, 30, 0)
	}))
	defer server.Close()

	svc, db, fixture := newSpecialistRuntimeFixture(t, server.URL, request)
	parentRun := specialistParentRun(t, svc, db, fixture.channelModel, request)
	first, err := svc.RunSpecialist(context.Background(), specialistRuntimeScope(), parentRun, request)
	if err != nil {
		t.Fatal(err)
	}
	var reviewStage model.AgentProductionStage
	if err := db.First(&reviewStage, "id = ?", request.StageID).Error; err != nil {
		t.Fatal(err)
	}
	comment := "把主角的动机改为寻找失踪的姐姐，并强化第二场冲突。"
	result, err := svc.ReviewProductionStage(context.Background(), specialistRuntimeScope(), parentRun, reviewStage.ID, agentruntime.StageReviewCommand{
		StageVersion: reviewStage.Version, RevisionID: first.Revisions[0].ID,
		ClientRequestID: "stage-revision-request-1", Decision: agentruntime.StageReviewRequestRevision, Comment: comment,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Completion == nil || result.Completion.Run.ID == first.Run.ID ||
		result.Completion.Run.ParentSpecialistRunID != first.Run.ID || providerCalls != 2 {
		t.Fatalf("revision completion = %#v, provider calls = %d", result.Completion, providerCalls)
	}
	if !strings.HasSuffix(result.Completion.Run.Objective, comment) {
		t.Fatalf("revision objective = %q, want exact comment suffix %q", result.Completion.Run.Objective, comment)
	}
	if len(result.Completion.Revisions) != 1 || result.Completion.Revisions[0].ArtifactID != first.Revisions[0].ArtifactID ||
		result.Completion.Revisions[0].Revision != first.Revisions[0].Revision+1 ||
		result.Stage.Status != agentruntime.StageAwaitingReview || result.Stage.ReviewRevisionID != result.Completion.Revisions[0].ID {
		t.Fatalf("revision result = %#v", result)
	}
	var revisions []model.AgentArtifactRevision
	if err := db.Where("artifact_id = ?", first.Revisions[0].ArtifactID).Order("revision ASC").Find(&revisions).Error; err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 2 || revisions[0].PayloadJSON == revisions[1].PayloadJSON {
		t.Fatalf("artifact revisions = %#v", revisions)
	}
	var leaked int64
	if err := db.Model(&model.AgentTimelineItem{}).Where("run_id = ? AND content_json LIKE ?", specialistRuntimeScope().RunID, "%"+comment+"%").Count(&leaked).Error; err != nil {
		t.Fatal(err)
	}
	if leaked != 0 {
		t.Fatalf("revision comment leaked into %d timeline items", leaked)
	}
	replayed, err := svc.ReviewProductionStage(context.Background(), specialistRuntimeScope(), parentRun, reviewStage.ID, agentruntime.StageReviewCommand{
		StageVersion: reviewStage.Version, RevisionID: first.Revisions[0].ID,
		ClientRequestID: "stage-revision-request-1", Decision: agentruntime.StageReviewRequestRevision, Comment: comment,
	})
	if err != nil || replayed.Completion == nil || replayed.Completion.Run.ID != result.Completion.Run.ID ||
		replayed.Completion.Revisions[0].ID != result.Completion.Revisions[0].ID || providerCalls != 2 {
		t.Fatalf("revision replay = %#v, provider calls = %d, error = %v", replayed, providerCalls, err)
	}
}

func TestStageRevisionRejectsStaleParentBeforeMutatingStage(t *testing.T) {
	request := scriptSpecialistRuntimeRequestFixture("runtime-token-agent-model", "deepseek-v4-flash")
	providerCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		providerCalls++
		writeAgentRuntimeChatStream(t, writer, "chatcmpl-stale-parent", scriptSpecialistRuntimeResultJSON(t, request), 200, 20, 0)
	}))
	defer server.Close()

	svc, db, fixture := newSpecialistRuntimeFixture(t, server.URL, request)
	parentRun := specialistParentRun(t, svc, db, fixture.channelModel, request)
	first, err := svc.RunSpecialist(context.Background(), specialistRuntimeScope(), parentRun, request)
	if err != nil {
		t.Fatal(err)
	}
	var before model.AgentProductionStage
	if err := db.First(&before, "id = ?", request.StageID).Error; err != nil {
		t.Fatal(err)
	}
	staleParent := parentRun
	staleParent.ModelKey = "different-model"
	_, err = svc.ReviewProductionStage(context.Background(), specialistRuntimeScope(), staleParent, before.ID, agentruntime.StageReviewCommand{
		StageVersion: before.Version, RevisionID: first.Revisions[0].ID,
		ClientRequestID: "stage-revision-stale-parent", Decision: agentruntime.StageReviewRequestRevision,
		Comment: "调整主角动机。",
	})
	if !errors.Is(err, agentruntime.ErrSpecialistModelInheritance) {
		t.Fatalf("ReviewProductionStage() error = %v, want %v", err, agentruntime.ErrSpecialistModelInheritance)
	}
	var after model.AgentProductionStage
	if err := db.First(&after, "id = ?", request.StageID).Error; err != nil {
		t.Fatal(err)
	}
	if after.Status != before.Status || after.Version != before.Version || after.ReviewRevisionID != before.ReviewRevisionID || providerCalls != 1 {
		t.Fatalf("stage mutated from %#v to %#v; provider calls = %d", before, after, providerCalls)
	}
}

func TestStageRevisionChildCannotBypassAtomicReviewCommand(t *testing.T) {
	request := scriptSpecialistRuntimeRequestFixture("runtime-token-agent-model", "deepseek-v4-flash")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeAgentRuntimeChatStream(t, writer, "chatcmpl-stage-bypass", scriptSpecialistRuntimeResultJSON(t, request), 200, 20, 0)
	}))
	defer server.Close()

	svc, db, fixture := newSpecialistRuntimeFixture(t, server.URL, request)
	parentRun := specialistParentRun(t, svc, db, fixture.channelModel, request)
	first, err := svc.RunSpecialist(context.Background(), specialistRuntimeScope(), parentRun, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentProductionStage{}).Where("id = ?", request.StageID).Updates(map[string]interface{}{
		"status":             agentruntime.StageRunning,
		"review_revision_id": "",
	}).Error; err != nil {
		t.Fatal(err)
	}
	bypass := request
	bypass.SpecialistRunID = "specialist-run-bypass-review"
	bypass.ParentSpecialistRunID = first.Run.ID
	bypass.Objective += "\n用户修订意见：绕开审核入口。"

	_, err = svc.repo.CreateAgentSpecialistRun(repository.CreateAgentSpecialistRunInput{
		Scope: specialistRuntimeScope(), Request: bypass,
		ToolSchemaVersion: first.Run.ToolSchemaVersion, Now: time.Now().UTC(),
	})
	if !errors.Is(err, repository.ErrAgentSpecialistRunConflict) {
		t.Fatalf("CreateAgentSpecialistRun() error = %v, want %v", err, repository.ErrAgentSpecialistRunConflict)
	}
	var count int64
	if err := db.Model(&model.AgentSpecialistRun{}).Where("id = ?", bypass.SpecialistRunID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("bypass specialist run count = %d, want 0", count)
	}
}

func TestGuidedAndAutomaticStopAtVisibleReviewWithoutPaidMediaTasks(t *testing.T) {
	for _, mode := range []agentruntime.ExecutionMode{agentruntime.ExecutionGuided, agentruntime.ExecutionAutomatic} {
		t.Run(string(mode), func(t *testing.T) {
			request := scriptSpecialistRuntimeRequestFixture("runtime-token-agent-model", "deepseek-v4-flash")
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writeAgentRuntimeChatStream(t, writer, "chatcmpl-review-mode-"+string(mode), scriptSpecialistRuntimeResultJSON(t, request), 200, 20, 0)
			}))
			defer server.Close()

			svc, db, fixture := newSpecialistRuntimeFixture(t, server.URL, request)
			parentRun := specialistParentRun(t, svc, db, fixture.channelModel, request)
			setCheckpointExecutionMode(t, db, parentRun.ID, mode)
			completion, err := svc.RunSpecialist(context.Background(), specialistRuntimeScope(), parentRun, request)
			if err != nil {
				t.Fatal(err)
			}
			var stage model.AgentProductionStage
			if err := db.First(&stage, "id = ?", request.StageID).Error; err != nil {
				t.Fatal(err)
			}
			if stage.Status != agentruntime.StageAwaitingReview || stage.ReviewRevisionID != completion.Revisions[0].ID {
				t.Fatalf("stage = %#v", stage)
			}
			var paidMediaTasks int64
			if err := db.Model(&model.Task{}).
				Where("audience = ? AND type <> ?", model.TaskAudienceCustomer, agentSpecialistModelTaskType).
				Count(&paidMediaTasks).Error; err != nil {
				t.Fatal(err)
			}
			if paidMediaTasks != 0 {
				t.Fatalf("paid media tasks = %d, want 0", paidMediaTasks)
			}
		})
	}
}

func assetBindingSpecialistRuntimeRequestFixture(modelRecordID string, modelKey string) agentruntime.SpecialistRequest {
	request := specialistRuntimeRequestFixture(modelRecordID, modelKey)
	request.SpecialistRunID = "specialist-run-asset-binding-1"
	request.SpecialistKey = agentruntime.SpecialistAsset
	request.Objective = "将剧本资产需求映射到当前作用域的已确认素材"
	request.InputRevisions = []agentruntime.ArtifactRevisionRef{}
	request.ToolAllowlist = []agentruntime.AgentToolName{}
	request.ExpectedOutputSchema = agentruntime.ArtifactSchemaAssetBindingV1
	request.LoadedSkills[0].CapabilityManifest = agentruntime.SkillCapabilityManifest{
		Specialists: []agentruntime.SpecialistKey{agentruntime.SpecialistAsset}, Tools: []agentruntime.AgentToolName{},
		ArtifactSchemas: []string{agentruntime.ArtifactSchemaAssetBindingV1},
	}
	return request
}

func prepareAssetBindingReviewFixture(
	t *testing.T,
	svc *Service,
	db *gorm.DB,
	resourceOwner string,
) (agentruntime.AssetBindingSet, model.AgentProductionStage, model.AgentArtifactRevision) {
	t.Helper()
	scope := specialistRuntimeScope()
	scriptPayload, err := json.Marshal(agentruntime.ScriptBundle{
		Title: "雨夜追踪", Logline: "女记者追查失踪案", Script: "第一场：林夏进入车站。",
		Characters: []agentruntime.CharacterNeed{{Key: "hero", Name: "林夏", Description: "冷静敏锐的女记者"}},
		Scenes:     []agentruntime.SceneNeed{}, Props: []agentruntime.PropNeed{}, VoiceRoles: []agentruntime.VoiceRoleNeed{},
	})
	if err != nil {
		t.Fatal(err)
	}
	scriptRevision, err := svc.repo.AppendArtifactRevision(scope, "script-artifact", 0, agentruntime.ArtifactDraft{
		ArtifactKey: "script", Kind: "script_bundle", SchemaVersion: 1, Payload: scriptPayload,
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{}, SkillVersions: []agentruntime.SkillSelection{},
	})
	if err != nil {
		t.Fatal(err)
	}
	bindings := agentruntime.AssetBindingSet{
		BindingKey:     "script-assets",
		ScriptRevision: agentruntime.ArtifactRevisionRef{ArtifactID: scriptRevision.ArtifactID, RevisionID: scriptRevision.ID},
		Confirmed:      true,
		Entries: []agentruntime.AssetBindingEntry{{
			RequirementKey: "hero", RequirementKind: agentruntime.AssetRequirementCharacter,
			State: agentruntime.AssetBindingMatched, ResourceID: "resource-hero", CandidateResourceIDs: []string{},
		}},
	}
	bindingPayload, err := json.Marshal(bindings)
	if err != nil {
		t.Fatal(err)
	}
	bindingRevision, err := svc.repo.AppendArtifactRevision(scope, "binding-artifact", 0, agentruntime.ArtifactDraft{
		ArtifactKey: "script-assets", Kind: "asset_binding", SchemaVersion: 1, Payload: bindingPayload,
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{bindings.ScriptRevision}, SkillVersions: []agentruntime.SkillSelection{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentArtifactRevision{}).Where("id = ?", bindingRevision.ID).
		Updates(map[string]interface{}{
			"created_by_specialist_id": "asset-specialist-run",
			"payload_json":             "\n  " + string(bindingPayload) + "\n",
		}).Error; err != nil {
		t.Fatal(err)
	}
	bindingRevision.CreatedBySpecialistID = "asset-specialist-run"
	bindingRevision.PayloadJSON = "\n  " + string(bindingPayload) + "\n"
	resource := model.Resource{
		ID: "resource-hero", UserID: resourceOwner, Kind: "image", Status: model.ResourceStatusReady,
		MimeType: "image/png", ObjectKey: "production/hero.png", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	stageID := repositoryProductionStageIDForTest(scope, "specialist-test-graph", "visual-analysis")
	now := time.Now().UTC()
	assetRun := model.AgentSpecialistRun{
		ID: "asset-specialist-run", TenantKind: scope.TenantKind, TenantID: scope.TenantID,
		ActorUserID: scope.ActorUserID, DomainProjectID: scope.DomainProjectID, CanvasID: scope.CanvasID,
		ThreadID: scope.ThreadID, RunID: scope.RunID, StageID: stageID, SpecialistKey: agentruntime.SpecialistAsset,
		SpecialistVersion: 1, Objective: "映射已确认素材", ModelRecordID: "runtime-token-agent-model",
		ModelKey: "deepseek-v4-flash", ToolSchemaVersion: 1, InputRevisionsJSON: "[]",
		SkillVersionsJSON: "[]", ToolAllowlistJSON: "[]", ExpectedOutputSchema: agentruntime.ArtifactSchemaAssetBindingV1,
		ExpectedDeliveryJSON: "{}", Status: model.AgentSpecialistRunSucceeded, Version: 1,
		CreatedAt: now, UpdatedAt: now, CompletedAt: &now,
	}
	if err := db.Create(&assetRun).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentProductionStage{}).Where("id = ?", stageID).Updates(map[string]interface{}{
		"status": agentruntime.StageAwaitingReview, "version": int64(2), "review_revision_id": bindingRevision.ID,
	}).Error; err != nil {
		t.Fatal(err)
	}
	var stage model.AgentProductionStage
	if err := db.First(&stage, "id = ?", stageID).Error; err != nil {
		t.Fatal(err)
	}
	return bindings, stage, *bindingRevision
}

func revisedScriptSpecialistRuntimeResultJSON(t *testing.T, request agentruntime.SpecialistRequest) string {
	t.Helper()
	var result agentruntime.SpecialistResult
	if err := json.Unmarshal([]byte(scriptSpecialistRuntimeResultJSON(t, request)), &result); err != nil {
		t.Fatal(err)
	}
	result.Summary = "剧本已按用户意见修订，等待再次确认"
	result.Artifacts[0].Payload = json.RawMessage(`{
		"title":"雨夜追踪","logline":"女记者在暴雨夜寻找失踪的姐姐",
		"script":"第一场：林夏进入废弃车站。第二场：她发现姐姐留下的录音并与追踪者冲突。",
		"characters":[{"key":"hero","name":"林夏","description":"为寻找失踪姐姐追查真相的女记者"}],
		"scenes":[{"key":"station","name":"废弃车站","description":"暴雨中的旧站台"}],"props":[],"voiceRoles":[]
	}`)
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func setCheckpointExecutionMode(t *testing.T, db *gorm.DB, runID string, mode agentruntime.ExecutionMode) {
	t.Helper()
	state := agentruntime.RuntimeState{
		StateVersion: 1, Status: agentruntime.RunRunning, MaxSteps: 24, UserMessage: "生成短片",
		Configuration: agentruntime.RunConfiguration{
			ExecutionMode: mode, Skills: []agentruntime.SkillSelection{}, Attachments: []agentruntime.ResourceAttachment{},
		},
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := model.AgentCheckpoint{
		ID: "mode-checkpoint-" + string(mode), RunID: runID, Sequence: 1, StateVersion: 1,
		StateJSON: string(encoded), CreatedAt: time.Now().UTC(),
	}
	if err := db.Create(&checkpoint).Error; err != nil {
		t.Fatal(err)
	}
}
