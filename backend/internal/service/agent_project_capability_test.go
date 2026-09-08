package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func TestAgentProjectCapabilityCommitsCandidatesAndShotsExactlyOnce(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	seedAgentProjectUnit(t, db)
	call := agentProjectApplyOpsCall(`{"domainProjectId":"runtime-project","baseRevision":1,"clientMutationId":"chapter-structure-1","operations":[{"operationId":"characters","type":"create_character_candidates","unitId":"runtime-unit","candidates":[{"name":"林夏","aliases":[],"role":"女主角","appearance":"黑色长发，年轻女性","clothing":"红色风衣","physique":"中等身高，匀称体型","personality":"克制而坚定","props":"红伞","consistencyPrompt":"保持黑色长发、红色风衣与红伞","multiViewPrompt":"展示红色风衣与黑色长发的正侧背结构","voiceLanguage":"普通话","voiceAge":"青年女声","voiceTimbre":"低声、清晰、克制"}]},{"operationId":"shots","type":"replace_unit_shots","unitId":"runtime-unit","shots":[{"title":"雨夜车站","description":"远景，林夏撑红伞站在旧车站门口，雨线清晰，镜头缓慢推进。","durationMs":5000},{"title":"钥匙交接","description":"近景，顾沉把沾雨的银色钥匙放入林夏掌心。","durationMs":4000}]}]}`)
	seedApprovedAgentCanvasProposal(t, svc, db, call)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	first, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
	if err != nil {
		t.Fatal(err)
	}
	second, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Output) != string(second.Output) {
		t.Fatalf("replayed receipt changed: first=%s second=%s", first.Output, second.Output)
	}
	decoded, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolProjectApplyOps, first.Output)
	if err != nil {
		t.Fatal(err)
	}
	result := decoded.(agentruntime.ProjectApplyOpsResult)
	if result.CommittedRevision != 2 || len(result.CandidateIDs) != 1 || len(result.ShotIDs) != 2 {
		t.Fatalf("project receipt = %#v", result)
	}
	var project model.Project
	if err := db.First(&project, "id = ?", "runtime-project").Error; err != nil {
		t.Fatal(err)
	}
	var candidates []model.ProjectAssetCandidate
	if err := db.Where("project_id = ?", project.ID).Find(&candidates).Error; err != nil {
		t.Fatal(err)
	}
	var shots []model.Shot
	if err := db.Where("project_id = ? AND unit_id = ?", project.ID, "runtime-unit").Order("position asc").Find(&shots).Error; err != nil {
		t.Fatal(err)
	}
	if project.Revision != 2 || len(candidates) != 1 || candidates[0].Name != "林夏" || len(shots) != 2 || shots[1].Title != "钥匙交接" {
		t.Fatalf("persisted project facts: project=%#v candidates=%#v shots=%#v", project, candidates, shots)
	}
	var record model.AgentToolCall
	if err := db.First(&record, "run_id = ? AND tool_call_id = ?", "runtime-run", call.ToolCallID).Error; err != nil {
		t.Fatal(err)
	}
	if record.Status != agentruntime.ToolCallSucceeded || record.OutputJSON != string(first.Output) {
		t.Fatalf("project tool receipt was not committed atomically: %#v", record)
	}
	evidence, err := svc.agentRuntimeDeliveryEvidence(agentRuntimeServiceScope(), "已写入项目")
	if err != nil {
		t.Fatal(err)
	}
	if evidence.ProjectID != project.ID || evidence.ProjectRevision != 2 || !evidence.ProjectCurrent {
		t.Fatalf("project delivery evidence = %#v", evidence)
	}
}

func TestAgentProjectCapabilityRejectsTamperedStoredEntityIDs(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	seedAgentProjectUnit(t, db)
	call := agentProjectApplyOpsCall(`{"domainProjectId":"runtime-project","baseRevision":1,"clientMutationId":"tampered-receipt","operations":[{"operationId":"characters","type":"create_character_candidates","unitId":"runtime-unit","candidates":[{"name":"林夏","aliases":[],"role":"女主角","appearance":"黑色长发","clothing":"红色风衣","physique":"匀称体型","personality":"克制坚定","props":"红伞","consistencyPrompt":"保持黑色长发、红色风衣与红伞","multiViewPrompt":"保持正侧背面身份与服装结构一致","voiceLanguage":"普通话","voiceAge":"青年女声","voiceTimbre":"低声、清晰、克制"}]}]}`)
	seedApprovedAgentCanvasProposal(t, svc, db, call)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	first, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolProjectApplyOps, first.Output)
	if err != nil {
		t.Fatal(err)
	}
	tampered := decoded.(agentruntime.ProjectApplyOpsResult)
	tampered.CandidateIDs[0] = "tampered-candidate-id"
	tamperedResult, err := agentruntime.NewToolExecutionResult(agentruntime.ToolProjectApplyOps, tampered)
	if err != nil {
		t.Fatal(err)
	}
	if result := db.Model(&model.AgentToolCall{}).
		Where("run_id = ? AND tool_call_id = ? AND action_version = ?", agentRuntimeServiceScope().RunID, call.ToolCallID, call.ActionVersion).
		Update("output_json", string(tamperedResult.Output)); result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("tamper stored receipt: rows=%d err=%v", result.RowsAffected, result.Error)
	}
	if _, err := svc.agentRuntimeDeliveryEvidence(agentRuntimeServiceScope(), "已写入项目"); err == nil {
		t.Fatal("delivery evidence accepted tampered project entity IDs")
	}
	if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call); !agentCapabilityErrorCode(err, "project_receipt_invalid") {
		t.Fatalf("error = %v, want project_receipt_invalid", err)
	}
}

func TestAgentProjectCapabilityRejectsStaleRevisionWithoutPartialWrites(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	seedAgentProjectUnit(t, db)
	call := agentProjectApplyOpsCall(`{"domainProjectId":"runtime-project","baseRevision":0,"clientMutationId":"stale-project","operations":[{"operationId":"shots","type":"replace_unit_shots","unitId":"runtime-unit","shots":[{"title":"不应写入","description":"不应写入项目","durationMs":3000}]}]}`)
	seedApprovedAgentCanvasProposal(t, svc, db, call)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call); !agentCapabilityErrorCode(err, "project_revision_conflict") {
		t.Fatalf("error = %v, want project_revision_conflict", err)
	}
	var shotCount int64
	if err := db.Model(&model.Shot{}).Where("project_id = ?", "runtime-project").Count(&shotCount).Error; err != nil {
		t.Fatal(err)
	}
	if shotCount != 0 {
		t.Fatalf("stale mutation wrote %d shots", shotCount)
	}
}

func TestAgentProjectMutationDetectsExactShotReplacementAsNoop(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	seedAgentProjectUnit(t, db)
	now := time.Now().UTC()
	shots := []model.Shot{
		{ID: "shot-1", ProjectID: "runtime-project", UnitID: "runtime-unit", Title: "雨夜车站", Description: "远景，林夏站在旧车站。", Position: 0, DurationMs: 5000, Status: "draft", CreatedAt: now, UpdatedAt: now},
		{ID: "shot-2", ProjectID: "runtime-project", UnitID: "runtime-unit", Title: "纸船入水", Description: "近景，白色纸船顺水漂远。", Position: 1, DurationMs: 4000, Status: "draft", CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&shots).Error; err != nil {
		t.Fatal(err)
	}
	arguments := agentruntime.ProjectApplyOpsArguments{
		DomainProjectID: "runtime-project", BaseRevision: 1, ClientMutationID: "duplicate-shots",
		Operations: []agentruntime.ProjectOperation{{
			OperationID: "replace-shots", Type: agentruntime.ProjectOperationReplaceUnitShots, UnitID: "runtime-unit",
			Shots: []agentruntime.ProjectShotInput{
				{Title: "雨夜车站", Description: "远景，林夏站在旧车站。", DurationMS: 5000},
				{Title: "纸船入水", Description: "近景，白色纸船顺水漂远。", DurationMS: 4000},
			},
		}},
	}
	wouldChange, err := svc.agentProjectApplyOpsWouldChange(agentRuntimeServiceScope(), arguments)
	if err != nil {
		t.Fatal(err)
	}
	if wouldChange {
		t.Fatal("exact shot replacement was treated as a project change")
	}
	arguments.Operations[0].Shots[1].DurationMS = 4500
	wouldChange, err = svc.agentProjectApplyOpsWouldChange(agentRuntimeServiceScope(), arguments)
	if err != nil {
		t.Fatal(err)
	}
	if !wouldChange {
		t.Fatal("changed shot duration was treated as a no-op")
	}
}

func TestPrepareAgentApprovalRejectsExactProjectShotNoop(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	seedAgentProjectUnit(t, db)
	now := time.Now().UTC()
	if err := db.Create(&model.Shot{
		ID: "shot-1", ProjectID: "runtime-project", UnitID: "runtime-unit", Title: "雨夜车站",
		Description: "远景，林夏站在旧车站。", Position: 0, DurationMs: 5000, Status: "draft", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	call := agentProjectApplyOpsCall(`{"domainProjectId":"runtime-project","baseRevision":1,"clientMutationId":"duplicate-shots","operations":[{"operationId":"replace-shots","type":"replace_unit_shots","unitId":"runtime-unit","shots":[{"title":"雨夜车站","description":"远景，林夏站在旧车站。","durationMs":5000}]}]}`)
	current := agentruntime.RuntimeState{
		StateVersion: 1, StepNumber: 1, MaxSteps: 4, Status: agentruntime.RunRunning, UserMessage: "继续生成",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided},
		Limits:        &agentruntime.RuntimeLimits{MaxToolCalls: 4, StartedAt: now, DeadlineAt: now.Add(time.Hour)},
	}
	transition, err := agentruntime.AdvanceForToolSchema(current, agentruntime.RuntimeInput{
		Decision: agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &call},
	}, agentruntime.CurrentToolSchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := svc.prepareAgentRuntimeApproval(agentRuntimeServiceScope(), current, transition, now)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.State.Status != agentruntime.RunRunning || prepared.State.PendingToolCall != nil || prepared.State.DecisionFeedback == nil ||
		prepared.State.DecisionFeedback.Code != "model_decision_invalid" {
		t.Fatalf("no-op approval was not rejected for model repair: %#v", prepared.State)
	}
}

func TestAgentProjectCapabilityRollsBackEarlierUnitReplacementWhenLaterUnitIsMissing(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	seedAgentProjectUnit(t, db)
	now := time.Now().UTC()
	if err := db.Create(&model.Shot{
		ID: "original-shot", ProjectID: "runtime-project", UnitID: "runtime-unit", Title: "原镜头",
		Description: "必须在事务失败后保留", Position: 0, DurationMs: 3000, Status: "draft", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	call := agentProjectApplyOpsCall(`{"domainProjectId":"runtime-project","baseRevision":1,"clientMutationId":"rollback-project","operations":[{"operationId":"valid-shots","type":"replace_unit_shots","unitId":"runtime-unit","shots":[{"title":"不应提交","description":"后续操作失败时必须回滚","durationMs":3000}]},{"operationId":"missing-unit-shots","type":"replace_unit_shots","unitId":"missing-unit","shots":[{"title":"不存在章节","description":"应触发整笔事务回滚","durationMs":3000}]}]}`)
	seedApprovedAgentCanvasProposal(t, svc, db, call)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call); !agentCapabilityErrorCode(err, "project_unit_scope_conflict") {
		t.Fatalf("error = %v, want project_unit_scope_conflict", err)
	}
	var shots []model.Shot
	if err := db.Where("project_id = ? AND unit_id = ?", "runtime-project", "runtime-unit").Find(&shots).Error; err != nil {
		t.Fatal(err)
	}
	var project model.Project
	if err := db.First(&project, "id = ?", "runtime-project").Error; err != nil {
		t.Fatal(err)
	}
	if len(shots) != 1 || shots[0].ID != "original-shot" || project.Revision != 1 {
		t.Fatalf("failed mutation leaked partial writes: project=%#v shots=%#v", project, shots)
	}
}

func TestAgentProjectCapabilityRejectsCharacterCandidateForForeignUnit(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	seedAgentProjectUnit(t, db)
	call := agentProjectApplyOpsCall(`{"domainProjectId":"runtime-project","baseRevision":1,"clientMutationId":"foreign-unit-character","operations":[{"operationId":"characters","type":"create_character_candidates","unitId":"foreign-unit","candidates":[{"name":"林夏","aliases":[],"role":"女主角","appearance":"黑色长发","clothing":"红色风衣","physique":"匀称体型","personality":"克制坚定","props":"红伞","consistencyPrompt":"保持黑色长发、红色风衣与红伞","multiViewPrompt":"保持正侧背面身份与服装结构一致","voiceLanguage":"普通话","voiceAge":"青年女声","voiceTimbre":"低声、清晰、克制"}]}]}`)
	seedApprovedAgentCanvasProposal(t, svc, db, call)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call); !agentCapabilityErrorCode(err, "project_unit_scope_conflict") {
		t.Fatalf("error = %v, want project_unit_scope_conflict", err)
	}
	var candidateCount int64
	if err := db.Model(&model.ProjectAssetCandidate{}).Where("project_id = ?", "runtime-project").Count(&candidateCount).Error; err != nil {
		t.Fatal(err)
	}
	var project model.Project
	if err := db.First(&project, "id = ?", "runtime-project").Error; err != nil {
		t.Fatal(err)
	}
	if candidateCount != 0 || project.Revision != 1 {
		t.Fatalf("foreign unit candidate leaked writes: project=%#v candidateCount=%d", project, candidateCount)
	}
}

func TestTranslateAgentProjectOperationsUsesStableEntityIDs(t *testing.T) {
	operations := []agentruntime.ProjectOperation{
		{
			OperationID: "characters", Type: agentruntime.ProjectOperationCreateCharacterCandidates, UnitID: "runtime-unit",
			Candidates: []agentruntime.ProjectCharacterCandidateInput{{Name: "林夏"}},
		},
		{
			OperationID: "shots", Type: agentruntime.ProjectOperationReplaceUnitShots, UnitID: "runtime-unit",
			Shots: []agentruntime.ProjectShotInput{{Title: "雨夜车站", Description: "远景", DurationMS: 3000}},
		},
	}
	_, _, firstCandidateIDs, firstShotIDs, err := translateAgentProjectOperations("runtime-project", "stable-mutation", operations)
	if err != nil {
		t.Fatal(err)
	}
	_, _, secondCandidateIDs, secondShotIDs, err := translateAgentProjectOperations("runtime-project", "stable-mutation", operations)
	if err != nil {
		t.Fatal(err)
	}
	if !sameStringSequence(firstCandidateIDs, secondCandidateIDs) || !sameStringSequence(firstShotIDs, secondShotIDs) {
		t.Fatalf("entity IDs changed between identical executions: candidates=%v/%v shots=%v/%v", firstCandidateIDs, secondCandidateIDs, firstShotIDs, secondShotIDs)
	}
	if len(firstCandidateIDs) != 1 || len(firstShotIDs) != 1 || firstCandidateIDs[0] == firstShotIDs[0] {
		t.Fatalf("stable IDs are not unique by entity kind: candidates=%v shots=%v", firstCandidateIDs, firstShotIDs)
	}
}

func agentProjectApplyOpsCall(arguments string) agentruntime.ToolCallDecision {
	return agentruntime.ToolCallDecision{
		ToolCallID: "project-apply", ToolName: agentruntime.ToolProjectApplyOps, ActionVersion: 1,
		Arguments: json.RawMessage(arguments),
		ExpectedDelivery: agentruntime.ExpectedDelivery{
			Kind: agentruntime.DeliveryProjectChange, TargetProjectID: "runtime-project",
			CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactProjectRevision}},
		},
	}
}

func seedAgentProjectUnit(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC()
	if err := db.Create(&model.ProjectUnit{
		ID: "runtime-unit", ProjectID: "runtime-project", Kind: model.ProjectUnitKindChapter,
		Title: "第一章", SourceText: "雨夜重逢", Status: model.ProjectUnitStatusReady,
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
}
