package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

func TestConsistencyReviewKeepsAllGeneratedCandidates(t *testing.T) {
	fixture := generatedCandidateFixture(t, 4)

	result, err := fixture.service.ReviewVisualCandidates(context.Background(), fixture.scope, fixture.command)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := fixture.service.repo.MediaCandidateRevisionsInScope(fixture.scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 4 || len(result.CandidateRevisions) != 4 || len(result.RankedCandidateRevisionIDs) != 4 {
		t.Fatalf("candidate ledger = %d, result candidates = %d, ranked = %d", len(stored), len(result.CandidateRevisions), len(result.RankedCandidateRevisionIDs))
	}
	for index, revisionID := range result.RankedCandidateRevisionIDs {
		if revisionID != fixture.candidateRevisions[index].ID {
			t.Fatalf("rank %d revision = %q, want %q", index, revisionID, fixture.candidateRevisions[index].ID)
		}
	}
	if result.ReviewRevision.Kind != "visual_consistency_review" || result.ReviewRevision.SchemaVersion != 1 {
		t.Fatalf("review revision = %#v", result.ReviewRevision)
	}
}

func TestApprovingCandidateNeverDeletesNonSelectedResources(t *testing.T) {
	fixture := generatedCandidateFixture(t, 3)
	review, err := fixture.service.ReviewVisualCandidates(context.Background(), fixture.scope, fixture.command)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&model.AgentProductionStage{}).Where("id = ?", fixture.stageID).Updates(map[string]interface{}{
		"status": agentruntime.StageAwaitingReview, "version": int64(2), "review_revision_id": review.ReviewRevision.ID,
	}).Error; err != nil {
		t.Fatal(err)
	}

	selected := fixture.candidateRevisions[1]
	approvalCommand := StageCandidateApprovalCommand{
		StageVersion: 2, ReviewRevisionID: review.ReviewRevision.ID,
		SelectedCandidateRevisionID: selected.ID, ClientRequestID: "approve-candidate-2",
	}
	approved, err := fixture.service.ApproveStageCandidate(
		context.Background(), fixture.scope, fixture.parentRun, fixture.stageID,
		approvalCommand,
	)
	if err != nil {
		t.Fatal(err)
	}
	if approved.Stage.Status != agentruntime.StageApproved || approved.SelectedCandidate == nil ||
		approved.SelectedCandidate.ResourceID != selected.ResourceID {
		t.Fatalf("candidate approval = %#v", approved)
	}
	replayed, err := fixture.service.ApproveStageCandidate(
		context.Background(), fixture.scope, fixture.parentRun, fixture.stageID, approvalCommand,
	)
	if err != nil || replayed.ReviewID != approved.ReviewID || replayed.SelectedCandidate == nil ||
		replayed.SelectedCandidate.ID != selected.ID {
		t.Fatalf("candidate approval replay = %#v, error = %v", replayed, err)
	}

	var resourceCount int64
	if err := fixture.db.Model(&model.Resource{}).Where("id IN ?", fixture.resourceIDs).Count(&resourceCount).Error; err != nil {
		t.Fatal(err)
	}
	if resourceCount != 3 {
		t.Fatalf("resources after approval = %d, want 3", resourceCount)
	}
	stored, err := fixture.service.repo.MediaCandidateRevisionsInScope(fixture.scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 3 {
		t.Fatalf("candidate revisions after approval = %d, want 3", len(stored))
	}
	var selectionCount int64
	if err := fixture.db.Model(&model.AgentArtifactRevision{}).
		Where("kind = ?", mediaCandidateSelectionKind).Count(&selectionCount).Error; err != nil {
		t.Fatal(err)
	}
	if selectionCount != 1 {
		t.Fatalf("selection revisions after replay = %d, want 1", selectionCount)
	}
	approvedRevisionIDs, err := fixture.service.repo.ApprovedArtifactRevisionIDsForScope(fixture.scope)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := approvedRevisionIDs[selected.ID]; !exists {
		t.Fatalf("selected candidate %q is absent from approved revision facts: %#v", selected.ID, approvedRevisionIDs)
	}
	for _, candidate := range fixture.candidateRevisions {
		if candidate.ID != selected.ID {
			if _, exists := approvedRevisionIDs[candidate.ID]; exists {
				t.Fatalf("non-selected candidate %q was marked approved", candidate.ID)
			}
		}
		if _, err := fixture.service.repo.ArtifactRevisionInScope(fixture.scope, candidate.ID); err != nil {
			t.Fatalf("candidate %q became unreachable: %v", candidate.ID, err)
		}
	}
}

func TestApproveStageCandidatePublishesExactSelectedCandidateFromOneAuthorization(t *testing.T) {
	fixture := generatedCandidateFixture(t, 2)
	review, err := fixture.service.ReviewVisualCandidates(context.Background(), fixture.scope, fixture.command)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&model.AgentProductionStage{}).Where("id = ?", fixture.stageID).Updates(model.AgentProductionStage{
		Status: agentruntime.StageAwaitingReview, Version: 2, ReviewRevisionID: review.ReviewRevision.ID,
	}).Error; err != nil {
		t.Fatal(err)
	}
	selected := fixture.candidateRevisions[1]
	seedCandidateMediaToolLineage(t, fixture, selected)
	nonSelected := fixture.candidateRevisions[0]
	intent := &agentruntime.AssetPublicationIntent{
		PublicationPurpose: "character-library",
		TargetCategory:     string(model.AssetCategoryCharacter),
		TargetBindingKey:   "hero",
	}

	result, err := fixture.service.ApproveStageCandidate(
		context.Background(), fixture.scope, fixture.parentRun, fixture.stageID,
		StageCandidateApprovalCommand{
			StageVersion: 2, ReviewRevisionID: review.ReviewRevision.ID,
			SelectedCandidateRevisionID: selected.ID, ClientRequestID: "approve-and-publish-selected-candidate",
			PublicationIntent: intent,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Publication == nil || result.Publication.ArtifactRevisionID != selected.ID {
		t.Fatalf("candidate publication = %#v, want selected revision %q", result.Publication, selected.ID)
	}
	var storedPublication model.AgentAssetPublication
	if err := fixture.db.First(&storedPublication, "id = ?", result.Publication.ID).Error; err != nil {
		t.Fatal(err)
	}
	var publicationAudit struct {
		ProducerKind string `json:"producerKind"`
		Producer     struct {
			Specialist *json.RawMessage `json:"specialist"`
			MediaTool  *struct {
				RunID                    string `json:"runId"`
				ToolCallID               string `json:"toolCallId"`
				ActionVersion            int    `json:"actionVersion"`
				AgentRequestIdentity     string `json:"agentRequestIdentity"`
				CandidateRequestIdentity string `json:"candidateRequestIdentity"`
			} `json:"mediaTool"`
		} `json:"producer"`
		Authorization struct {
			Kind                string `json:"kind"`
			ReviewRevisionID    string `json:"reviewRevisionId"`
			SelectionRevisionID string `json:"selectionRevisionId"`
		} `json:"authorization"`
	}
	if err := json.Unmarshal([]byte(storedPublication.AuditJSON), &publicationAudit); err != nil {
		t.Fatal(err)
	}
	if publicationAudit.ProducerKind != "media_tool" || publicationAudit.Producer.Specialist != nil ||
		publicationAudit.Producer.MediaTool == nil || publicationAudit.Producer.MediaTool.RunID != fixture.scope.RunID ||
		publicationAudit.Producer.MediaTool.ToolCallID == "" || publicationAudit.Producer.MediaTool.ActionVersion != 1 ||
		publicationAudit.Producer.MediaTool.CandidateRequestIdentity != selected.ModelRequestIdentity ||
		publicationAudit.Authorization.Kind != "candidate_selection" ||
		publicationAudit.Authorization.ReviewRevisionID != review.ReviewRevision.ID {
		t.Fatalf("candidate publication audit = %#v", publicationAudit)
	}
	if result.Publication.ArtifactRevisionID == review.ReviewRevision.ID {
		t.Fatalf("visual review revision %q was published as media", review.ReviewRevision.ID)
	}
	var reviewItem model.AgentTimelineItem
	if err := fixture.db.First(&reviewItem, "id = ?", result.ReviewID).Error; err != nil {
		t.Fatal(err)
	}
	reviewContent, err := agentruntime.DecodeStageReviewResolutionContent([]byte(reviewItem.ContentJSON))
	if err != nil {
		t.Fatal(err)
	}
	if reviewContent.PublicationIntent == nil || *reviewContent.PublicationIntent != *intent {
		t.Fatalf("persisted publication intent = %#v, want %#v", reviewContent.PublicationIntent, intent)
	}
	var selections []model.AgentArtifactRevision
	if err := fixture.db.Where("kind = ?", mediaCandidateSelectionKind).Find(&selections).Error; err != nil {
		t.Fatal(err)
	}
	if len(selections) != 1 {
		t.Fatalf("selection revision count = %d, want 1", len(selections))
	}
	if publicationAudit.Authorization.SelectionRevisionID != selections[0].ID {
		t.Fatalf("audit selection revision = %q, want %q", publicationAudit.Authorization.SelectionRevisionID, selections[0].ID)
	}
	selection, err := agentruntime.DecodeMediaCandidateSelection([]byte(selections[0].PayloadJSON))
	if err != nil {
		t.Fatal(err)
	}
	if selection.ReviewRevision.RevisionID != review.ReviewRevision.ID || selection.SelectedCandidateRevision.RevisionID != selected.ID {
		t.Fatalf("selection bridge = %#v", selection)
	}
	_, err = fixture.service.PublishAsset(context.Background(), fixture.scope, PublishAssetCommand{
		AuthorizationKind:            repository.AgentAssetPublicationCandidateSelection,
		ArtifactRevisionID:           selected.ID,
		ReviewRevisionID:             review.ReviewRevision.ID,
		CandidateSelectionRevisionID: review.ReviewRevision.ID,
		PublicationPurpose:           intent.PublicationPurpose,
		TargetCategory:               model.AssetCategory(intent.TargetCategory),
		TargetBindingKey:             intent.TargetBindingKey,
		ApprovedByUserID:             fixture.scope.ActorUserID,
		StageReviewID:                result.ReviewID,
	})
	if !errors.Is(err, ErrAssetPublicationApprovalRequired) {
		t.Fatalf("mismatched selection publication error = %v, want %v", err, ErrAssetPublicationApprovalRequired)
	}
	for _, revision := range []model.AgentArtifactRevision{selected, nonSelected} {
		if _, err := fixture.service.repo.ArtifactRevisionInScope(fixture.scope, revision.ID); err != nil {
			t.Fatalf("candidate %q became unreachable: %v", revision.ID, err)
		}
		if _, err := fixture.service.productionResourceForScope(fixture.scope, revision.ResourceID); err != nil {
			t.Fatalf("candidate resource %q became unreachable: %v", revision.ResourceID, err)
		}
	}
}

func TestCandidatePublicationRecoversExactApprovalAndPreservesAllCandidates(t *testing.T) {
	fixture, review, selected, command := candidatePublicationScenario(t, 3, 1, "recover-selected-candidate")
	beforeResources := tableCount(t, fixture.db, "resources")
	beforeRevisions := tableCount(t, fixture.db, "agent_artifact_revisions")
	if err := fixture.db.Exec(`CREATE TRIGGER fail_candidate_representation BEFORE INSERT ON asset_representations BEGIN SELECT RAISE(ABORT, 'forced candidate representation failure'); END;`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = fixture.db.Exec(`DROP TRIGGER IF EXISTS fail_candidate_representation`).Error
	})

	_, err := fixture.service.ApproveStageCandidate(
		context.Background(), fixture.scope, fixture.parentRun, fixture.stageID, command,
	)
	if !errors.Is(err, ErrAssetPublicationFailed) {
		t.Fatalf("ApproveStageCandidate() error = %v, want %v", err, ErrAssetPublicationFailed)
	}
	assertTableCount(t, fixture.db, "assets", 0)
	assertTableCount(t, fixture.db, "asset_versions", 0)
	assertTableCount(t, fixture.db, "asset_representations", 0)
	assertTableCount(t, fixture.db, "agent_asset_publications", 1)
	assertTableCount(t, fixture.db, "agent_artifact_revisions", beforeRevisions+1)
	if got := tableCount(t, fixture.db, "resources"); got != beforeResources {
		t.Fatalf("resource count after failed publication = %d, want %d", got, beforeResources)
	}
	var failed model.AgentAssetPublication
	if err := fixture.db.First(&failed).Error; err != nil {
		t.Fatal(err)
	}
	if failed.Status != model.AgentAssetPublicationFailed || failed.ArtifactRevisionID != selected.ID {
		t.Fatalf("failed candidate publication = %#v", failed)
	}
	var stage model.AgentProductionStage
	if err := fixture.db.First(&stage, "id = ?", fixture.stageID).Error; err != nil {
		t.Fatal(err)
	}
	if stage.Status != agentruntime.StageApproved || stage.ReviewRevisionID != review.ReviewRevision.ID {
		t.Fatalf("stage after failed publication = %#v", stage)
	}
	assertTableCount(t, fixture.db, "agent_artifact_revisions", beforeRevisions+1)
	assertTableCount(t, fixture.db, "agent_asset_publications", 1)

	if err := fixture.db.Exec(`DROP TRIGGER fail_candidate_representation`).Error; err != nil {
		t.Fatal(err)
	}
	completedAt := time.Now().UTC()
	if err := fixture.db.Model(&model.AgentRun{}).Where("id = ?", fixture.scope.RunID).
		Updates(model.AgentRun{Status: agentruntime.RunFailed, CompletedAt: &completedAt}).Error; err != nil {
		t.Fatal(err)
	}
	recovered, err := fixture.service.ApproveStageCandidate(
		context.Background(), fixture.scope, fixture.parentRun, fixture.stageID, command,
	)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Publication == nil || recovered.Publication.ID != failed.ID ||
		recovered.Publication.ArtifactRevisionID != selected.ID || recovered.Publication.Status != model.AgentAssetPublicationSucceeded {
		t.Fatalf("recovered publication = %#v, failed = %#v", recovered.Publication, failed)
	}
	replayed, err := fixture.service.ApproveStageCandidate(
		context.Background(), fixture.scope, fixture.parentRun, fixture.stageID, command,
	)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Publication == nil || !replayed.Publication.Replayed ||
		replayed.Publication.ID != recovered.Publication.ID || replayed.Publication.AssetVersionID != recovered.Publication.AssetVersionID {
		t.Fatalf("replayed publication = %#v, recovered = %#v", replayed.Publication, recovered.Publication)
	}
	assertTableCount(t, fixture.db, "agent_asset_publications", 1)
	assertTableCount(t, fixture.db, "assets", 1)
	assertTableCount(t, fixture.db, "asset_versions", 1)
	assertTableCount(t, fixture.db, "asset_representations", 1)
	assertTableCount(t, fixture.db, "agent_artifact_revisions", beforeRevisions+1)
	if got := tableCount(t, fixture.db, "resources"); got != beforeResources {
		t.Fatalf("resource count after recovery = %d, want %d", got, beforeResources)
	}
	for _, candidate := range fixture.candidateRevisions {
		if _, err := fixture.service.repo.ArtifactRevisionInScope(fixture.scope, candidate.ID); err != nil {
			t.Fatalf("candidate %q became unreachable: %v", candidate.ID, err)
		}
	}
}

func TestCandidatePublicationRejectsStageVersionDriftAfterFailedPersistence(t *testing.T) {
	fixture, review, _, command := candidatePublicationScenario(t, 2, 1, "reject-stage-version-drift")
	if err := fixture.db.Exec(`CREATE TRIGGER fail_candidate_representation BEFORE INSERT ON asset_representations BEGIN SELECT RAISE(ABORT, 'forced candidate representation failure'); END;`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = fixture.db.Exec(`DROP TRIGGER IF EXISTS fail_candidate_representation`).Error
	})

	if _, err := fixture.service.ApproveStageCandidate(
		context.Background(), fixture.scope, fixture.parentRun, fixture.stageID, command,
	); !errors.Is(err, ErrAssetPublicationFailed) {
		t.Fatalf("ApproveStageCandidate() error = %v, want %v", err, ErrAssetPublicationFailed)
	}
	if err := fixture.db.Exec(`DROP TRIGGER fail_candidate_representation`).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&model.AgentProductionStage{}).Where("id = ?", fixture.stageID).
		Update("version", int64(5)).Error; err != nil {
		t.Fatal(err)
	}

	_, err := fixture.service.ApproveStageCandidate(
		context.Background(), fixture.scope, fixture.parentRun, fixture.stageID, command,
	)
	if !errors.Is(err, ErrAssetPublicationConflict) {
		t.Fatalf("ApproveStageCandidate() error = %v, want %v", err, ErrAssetPublicationConflict)
	}
	assertTableCount(t, fixture.db, "assets", 0)
	assertTableCount(t, fixture.db, "asset_versions", 0)
	assertTableCount(t, fixture.db, "asset_representations", 0)
	var stage model.AgentProductionStage
	if err := fixture.db.First(&stage, "id = ?", fixture.stageID).Error; err != nil {
		t.Fatal(err)
	}
	if stage.Status != agentruntime.StageApproved || stage.Version != 5 || stage.ReviewRevisionID != review.ReviewRevision.ID {
		t.Fatalf("stage after rejected version drift = %#v", stage)
	}
}

func TestCandidatePublicationRejectsConflictingReplay(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*StageCandidateApprovalCommand, generatedCandidateReviewFixture)
	}{
		{
			name: "changed candidate",
			mutate: func(command *StageCandidateApprovalCommand, fixture generatedCandidateReviewFixture) {
				command.SelectedCandidateRevisionID = fixture.candidateRevisions[0].ID
			},
		},
		{
			name: "changed target",
			mutate: func(command *StageCandidateApprovalCommand, _ generatedCandidateReviewFixture) {
				command.PublicationIntent = &agentruntime.AssetPublicationIntent{
					PublicationPurpose: "style-library", TargetCategory: string(model.AssetCategoryStyle), TargetBindingKey: "visual-style",
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, _, _, command := candidatePublicationScenario(t, 2, 1, "conflicting-candidate-replay")
			if _, err := fixture.service.ApproveStageCandidate(
				context.Background(), fixture.scope, fixture.parentRun, fixture.stageID, command,
			); err != nil {
				t.Fatal(err)
			}
			test.mutate(&command, fixture)
			_, err := fixture.service.ApproveStageCandidate(
				context.Background(), fixture.scope, fixture.parentRun, fixture.stageID, command,
			)
			if !errors.Is(err, repository.ErrProductionStageReviewConflict) {
				t.Fatalf("ApproveStageCandidate() error = %v, want %v", err, repository.ErrProductionStageReviewConflict)
			}
			assertTableCount(t, fixture.db, "agent_asset_publications", 1)
			assertTableCount(t, fixture.db, "assets", 1)
		})
	}
}

func TestCandidatePublicationConcurrentExactApprovalPublishesOnce(t *testing.T) {
	fixture, _, selected, command := candidatePublicationScenario(t, 3, 1, "concurrent-selected-candidate")
	sqlDB, err := fixture.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)

	start := make(chan struct{})
	results := make([]StageReviewResult, 2)
	errorsByWorker := make([]error, len(results))
	var workers sync.WaitGroup
	for index := range results {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			<-start
			results[worker], errorsByWorker[worker] = fixture.service.ApproveStageCandidate(
				context.Background(), fixture.scope, fixture.parentRun, fixture.stageID, command,
			)
		}(index)
	}
	close(start)
	workers.Wait()

	replayCount := 0
	for index, approvalErr := range errorsByWorker {
		if approvalErr != nil {
			t.Fatalf("worker %d error = %v", index, approvalErr)
		}
		publication := results[index].Publication
		if publication == nil || publication.ArtifactRevisionID != selected.ID || publication.Status != model.AgentAssetPublicationSucceeded {
			t.Fatalf("worker %d publication = %#v", index, publication)
		}
		if publication.Replayed {
			replayCount++
		}
	}
	if replayCount != 1 || results[0].Publication.ID != results[1].Publication.ID ||
		results[0].Publication.AssetVersionID != results[1].Publication.AssetVersionID {
		t.Fatalf("concurrent results = %#v, replay count = %d", results, replayCount)
	}
	assertTableCount(t, fixture.db, "agent_asset_publications", 1)
	assertTableCount(t, fixture.db, "assets", 1)
	assertTableCount(t, fixture.db, "asset_versions", 1)
	assertTableCount(t, fixture.db, "asset_representations", 1)
	assertTableCount(t, fixture.db, "project_asset_links", 1)
}

func TestCandidatePublicationCancelledBeforeReviewTransactionWritesNothing(t *testing.T) {
	fixture, _, _, command := candidatePublicationScenario(t, 2, 1, "cancelled-selected-candidate")
	beforeRevisions := tableCount(t, fixture.db, "agent_artifact_revisions")
	beforeTimeline := tableCount(t, fixture.db, "agent_timeline_items")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fixture.service.ApproveStageCandidate(
		ctx, fixture.scope, fixture.parentRun, fixture.stageID, command,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ApproveStageCandidate() error = %v, want %v", err, context.Canceled)
	}
	assertTableCount(t, fixture.db, "agent_artifact_revisions", beforeRevisions)
	assertTableCount(t, fixture.db, "agent_timeline_items", beforeTimeline)
	assertTableCount(t, fixture.db, "agent_asset_publications", 0)
	assertTableCount(t, fixture.db, "assets", 0)
	assertTableCount(t, fixture.db, "asset_versions", 0)
	assertTableCount(t, fixture.db, "asset_representations", 0)
	var stage model.AgentProductionStage
	if err := fixture.db.First(&stage, "id = ?", fixture.stageID).Error; err != nil {
		t.Fatal(err)
	}
	if stage.Status != agentruntime.StageAwaitingReview || stage.Version != 2 {
		t.Fatalf("stage after cancelled approval = %#v", stage)
	}
}

func candidatePublicationScenario(
	t *testing.T,
	candidateCount int,
	selectedIndex int,
	clientRequestID string,
) (generatedCandidateReviewFixture, VisualCandidateReviewResult, model.AgentArtifactRevision, StageCandidateApprovalCommand) {
	t.Helper()
	fixture := generatedCandidateFixture(t, candidateCount)
	review, err := fixture.service.ReviewVisualCandidates(context.Background(), fixture.scope, fixture.command)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&model.AgentProductionStage{}).Where("id = ?", fixture.stageID).
		Updates(model.AgentProductionStage{
			Status: agentruntime.StageAwaitingReview, Version: 2, ReviewRevisionID: review.ReviewRevision.ID,
		}).Error; err != nil {
		t.Fatal(err)
	}
	selected := fixture.candidateRevisions[selectedIndex]
	seedCandidateMediaToolLineage(t, fixture, selected)
	return fixture, review, selected, StageCandidateApprovalCommand{
		StageVersion: 2, ReviewRevisionID: review.ReviewRevision.ID,
		SelectedCandidateRevisionID: selected.ID, ClientRequestID: clientRequestID,
		PublicationIntent: &agentruntime.AssetPublicationIntent{
			PublicationPurpose: "character-library", TargetCategory: string(model.AssetCategoryCharacter), TargetBindingKey: "hero",
		},
	}
}

func TestMediaToolPublicationRejectsEveryBrokenCommercialLink(t *testing.T) {
	tests := map[string]func(*testing.T, *gorm.DB, candidateMediaToolLineage){
		"missing tool call": func(t *testing.T, db *gorm.DB, lineage candidateMediaToolLineage) {
			if err := db.Delete(&model.AgentToolCall{}, "id = ?", lineage.ToolCallID).Error; err != nil {
				t.Fatal(err)
			}
		},
		"customer task audience": func(t *testing.T, db *gorm.DB, lineage candidateMediaToolLineage) {
			if err := db.Model(&model.Task{}).Where("id = ?", lineage.TaskID).
				Update("audience", model.TaskAudienceCustomer).Error; err != nil {
				t.Fatal(err)
			}
		},
		"unsettled billing order": func(t *testing.T, db *gorm.DB, lineage candidateMediaToolLineage) {
			if err := db.Model(&model.BillingOrder{}).Where("id = ?", lineage.BillingOrderID).
				Update("status", model.BillingStatusRunning).Error; err != nil {
				t.Fatal(err)
			}
		},
		"duplicate matching tool calls": func(t *testing.T, db *gorm.DB, lineage candidateMediaToolLineage) {
			var call model.AgentToolCall
			if err := db.First(&call, "id = ?", lineage.ToolCallID).Error; err != nil {
				t.Fatal(err)
			}
			call.ID += "-duplicate"
			call.ToolCallID += "-duplicate"
			call.IdempotencyKey += "-duplicate"
			if err := db.Create(&call).Error; err != nil {
				t.Fatal(err)
			}
		},
		"pricing resolution drift": func(t *testing.T, db *gorm.DB, lineage candidateMediaToolLineage) {
			if err := db.Model(&model.BillingOrder{}).Where("id = ?", lineage.BillingOrderID).
				Update("pricing_resolution", "2K").Error; err != nil {
				t.Fatal(err)
			}
		},
		"pricing input variant drift": func(t *testing.T, db *gorm.DB, lineage candidateMediaToolLineage) {
			if err := db.Model(&model.BillingOrder{}).Where("id = ?", lineage.BillingOrderID).
				Update("pricing_input_variant", "medium").Error; err != nil {
				t.Fatal(err)
			}
		},
		"unsuffixed candidate request identity": func(t *testing.T, db *gorm.DB, lineage candidateMediaToolLineage) {
			var call model.AgentToolCall
			if err := db.First(&call, "id = ?", lineage.ToolCallID).Error; err != nil {
				t.Fatal(err)
			}
			var input agentMediaGenerationArguments
			if err := json.Unmarshal([]byte(call.InputJSON), &input); err != nil {
				t.Fatal(err)
			}
			input.RequestIdentity = lineage.CandidateRequestIdentity
			encoded, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.AgentToolCall{}).Where("id = ?", lineage.ToolCallID).
				Update("input_json", string(encoded)).Error; err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, breakLineage := range tests {
		t.Run(name, func(t *testing.T) {
			fixture := generatedCandidateFixture(t, 2)
			review, err := fixture.service.ReviewVisualCandidates(context.Background(), fixture.scope, fixture.command)
			if err != nil {
				t.Fatal(err)
			}
			if err := fixture.db.Model(&model.AgentProductionStage{}).Where("id = ?", fixture.stageID).
				Updates(model.AgentProductionStage{Status: agentruntime.StageAwaitingReview, Version: 2, ReviewRevisionID: review.ReviewRevision.ID}).Error; err != nil {
				t.Fatal(err)
			}
			selected := fixture.candidateRevisions[1]
			lineage := seedCandidateMediaToolLineage(t, fixture, selected)
			breakLineage(t, fixture.db, lineage)
			_, err = fixture.service.ApproveStageCandidate(
				context.Background(), fixture.scope, fixture.parentRun, fixture.stageID,
				StageCandidateApprovalCommand{
					StageVersion: 2, ReviewRevisionID: review.ReviewRevision.ID,
					SelectedCandidateRevisionID: selected.ID, ClientRequestID: "reject-broken-media-lineage",
					PublicationIntent: &agentruntime.AssetPublicationIntent{
						PublicationPurpose: "character-library", TargetCategory: string(model.AssetCategoryCharacter), TargetBindingKey: "hero",
					},
				},
			)
			if !errors.Is(err, ErrAssetPublicationBillingMissing) {
				t.Fatalf("ApproveStageCandidate() error = %v, want %v", err, ErrAssetPublicationBillingMissing)
			}
			assertTableCount(t, fixture.db, "agent_asset_publications", 0)
			assertTableCount(t, fixture.db, "assets", 0)
		})
	}
}

func TestMediaToolPublicationSupportsIndependentAudioAndRejectsVideo(t *testing.T) {
	tests := []struct {
		name       string
		mediaKind  agentruntime.ArtifactKind
		wantErr    error
		wantAssets int64
	}{
		{name: "independent audio", mediaKind: agentruntime.ArtifactAudio, wantAssets: 1},
		{name: "video remains assembly output", mediaKind: agentruntime.ArtifactVideo, wantErr: ErrAssetPublicationResourceMissing},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := generatedCandidateFixture(t, 1)
			setGeneratedCandidateMediaKind(t, fixture, 0, test.mediaKind)
			review, err := fixture.service.ReviewVisualCandidates(context.Background(), fixture.scope, fixture.command)
			if err != nil {
				t.Fatal(err)
			}
			if err := fixture.db.Model(&model.AgentProductionStage{}).Where("id = ?", fixture.stageID).
				Updates(model.AgentProductionStage{Status: agentruntime.StageAwaitingReview, Version: 2, ReviewRevisionID: review.ReviewRevision.ID}).Error; err != nil {
				t.Fatal(err)
			}
			selected := fixture.candidateRevisions[0]
			seedCandidateMediaToolLineage(t, fixture, selected)
			result, err := fixture.service.ApproveStageCandidate(
				context.Background(), fixture.scope, fixture.parentRun, fixture.stageID,
				StageCandidateApprovalCommand{
					StageVersion: 2, ReviewRevisionID: review.ReviewRevision.ID,
					SelectedCandidateRevisionID: selected.ID, ClientRequestID: "publish-" + string(test.mediaKind),
					PublicationIntent: &agentruntime.AssetPublicationIntent{
						PublicationPurpose: "approved-media", TargetCategory: string(model.AssetCategoryOther), TargetBindingKey: "approved-media",
					},
				},
			)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("ApproveStageCandidate() error = %v, want %v", err, test.wantErr)
				}
			} else if err != nil {
				t.Fatal(err)
			} else if result.Publication == nil || result.Publication.ArtifactRevisionID != selected.ID {
				t.Fatalf("publication = %#v", result.Publication)
			}
			assertTableCount(t, fixture.db, "assets", test.wantAssets)
		})
	}
}

func setGeneratedCandidateMediaKind(
	t *testing.T,
	fixture generatedCandidateReviewFixture,
	index int,
	mediaKind agentruntime.ArtifactKind,
) {
	t.Helper()
	candidate := &fixture.candidateRevisions[index]
	content, err := agentruntime.DecodeMediaCandidateContent([]byte(candidate.PayloadJSON))
	if err != nil {
		t.Fatal(err)
	}
	content.MediaKind = mediaKind
	payload, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&model.Resource{}).Where("id = ?", candidate.ResourceID).
		Select("kind", "object_key", "mime_type").
		Updates(model.Resource{
			Kind: string(mediaKind), ObjectKey: "candidates/selected." + string(mediaKind),
			MimeType: string(mediaKind) + "/test",
		}).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&model.AgentArtifactRevision{}).Where("id = ?", candidate.ID).
		Update("payload_json", string(payload)).Error; err != nil {
		t.Fatal(err)
	}
	candidate.PayloadJSON = string(payload)
	fixture.command.Candidates[index].MediaKind = string(mediaKind)
}

type candidateMediaToolLineage struct {
	TaskID                   string
	BillingOrderID           string
	ToolCallID               string
	CandidateRequestIdentity string
}

func seedCandidateMediaToolLineage(
	t *testing.T,
	fixture generatedCandidateReviewFixture,
	candidate model.AgentArtifactRevision,
) candidateMediaToolLineage {
	t.Helper()
	content, err := agentruntime.DecodeMediaCandidateContent([]byte(candidate.PayloadJSON))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	taskID := content.SourceTaskID
	billingOrderID := "billing-" + taskID
	supplierRequestID := "supplier-" + taskID
	capability := string(content.MediaKind)
	modelKey := map[string]string{"image": "image-2.0", "audio": "voice-pro", "video": "seedance-2.0-pro"}[capability]
	parametersJSON := map[string]string{
		"image": `{"size":"1024x1024","quality":"high"}`,
		"audio": `{"voice":"narrator","format":"wav"}`,
		"video": `{"resolution":"720p","durationSeconds":5}`,
	}[capability]
	resultCollection := map[string]string{"image": "images", "audio": "audios", "video": "videos"}[capability]
	toolParameters := map[string]json.RawMessage{
		"image": json.RawMessage(`{"prompt":"林夏角色定妆照","aspectRatio":"1:1","resolution":"1K","quality":"high","count":1}`),
		"audio": json.RawMessage(`{"prompt":"林夏旁白","voice":"narrator","format":"wav"}`),
		"video": json.RawMessage(`{"prompt":"林夏走入雨幕","aspectRatio":"16:9","resolution":"720p","durationSeconds":5,"generateAudio":false}`),
	}[capability]
	channelID := "candidate-channel"
	channelModelID := "candidate-channel-model"
	requestIdentity := strings.TrimSuffix(candidate.ModelRequestIdentity, ":01")
	if requestIdentity == candidate.ModelRequestIdentity {
		t.Fatalf("candidate request identity %q does not contain the production ordinal suffix", candidate.ModelRequestIdentity)
	}
	pricingResolution := map[string]string{"image": "1K", "audio": "", "video": "720P"}[capability]
	pricingInputVariant := map[string]string{"image": "high", "audio": "standard_audio", "video": "standard"}[capability]
	task := model.Task{
		ID: taskID, UserID: fixture.scope.ActorUserID, Audience: model.TaskAudienceInternal,
		ProjectID: fixture.scope.CanvasID, Type: "agent_runtime_media", Capability: capability,
		Status: model.TaskStatusSucceeded, Stage: "已完成", Progress: 100, Prompt: "林夏角色定妆照",
		Operation: agentMediaGenerationOperationForRun(fixture.scope.RunID), Provider: "kuaizi", Model: modelKey,
		BillingOrderID: billingOrderID, ProviderEndpointVersionID: "candidate-endpoint-version",
		ProviderCredentialVersionID: "candidate-credential-version", ProviderRequestID: supplierRequestID,
		InputJSON:  parametersJSON,
		ResultJSON: `{"` + resultCollection + `":[{"resourceId":"` + candidate.ResourceID + `"}]}`,
		StartedAt:  &now, CompletedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	order := model.BillingOrder{
		ID: billingOrderID, UserID: fixture.scope.ActorUserID, IdempotencyKey: "billing-idempotency-" + taskID,
		TaskID: taskID, ChannelID: channelID, ChannelModelID: channelModelID, Model: modelKey,
		Capability: capability, Scene: task.Operation, BillingMode: "fixed_request", PriceVersion: 1,
		PricingResolution: pricingResolution, PricingInputVariant: pricingInputVariant,
		UnitPriceMicrocredits: 1_000_000, MultiplierBasisPoints: 10_000, Quantity: 1,
		AmountMicrocredits: 1_000_000, Status: model.BillingStatusSettled,
		ProviderRequestID: supplierRequestID, ProviderBillingOrderID: "supplier-order-" + taskID,
		ProviderBillingAmount: 1_000_000, ProviderBillingStatus: "settled", ProviderBillingUnit: "CNY_MICRO",
		SettledAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	delivery := exactAgentMediaExpectedDelivery(content.MediaKind)
	input := agentMediaGenerationArguments{
		InputRevisions: []agentruntime.ArtifactRevisionRef{}, InputResources: []agentMediaInputResource{},
		GenerationModel:         agentruntime.GenerationModelSelection{ChannelID: channelID, Model: modelKey},
		GenerationModelRecordID: channelModelID, Capability: capability,
		Parameters:       toolParameters,
		OutputArtifactID: candidate.ArtifactID, OutputArtifactKey: candidate.ArtifactKey,
		ExpectedOutputSchema: agentMediaCandidateSchema, ExpectedDelivery: delivery,
		RequestIdentity: requestIdentity, SkillVersions: []agentruntime.SkillSelection{},
		Commercial: MediaGenerationAttempt{
			ArtifactRevisionID: candidate.ArtifactID, Attempt: 1, TaskID: taskID,
			BillingIdempotencyKey: order.IdempotencyKey, TaskType: task.Type, Operation: task.Operation,
			Prompt: task.Prompt, Capability: task.Capability, ChannelID: channelID, ChannelModelID: channelModelID,
			ModelKey: modelKey, ParametersJSON: task.InputJSON, ProviderCapabilitiesJSON: `{}`,
			Quantity: 1, AmountMicrocredits: order.AmountMicrocredits, PerTaskAmountMicrocredits: order.AmountMicrocredits,
			PriceVersion: order.PriceVersion, BillingMode: order.BillingMode,
			PricingResolution: pricingResolution, PricingInputVariant: pricingInputVariant,
			BillingQuoteFingerprint: "quote-fingerprint-" + taskID,
			QuoteID:                 "quote-" + taskID, ApprovalFingerprint: "approval-" + taskID,
			ExpiresAt: now.Add(time.Hour), ApprovedAt: now,
		},
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	audioMode := map[string]agentruntime.MediaAudioMode{
		"image": agentruntime.MediaAudioNone, "audio": agentruntime.MediaAudioIndependent, "video": agentruntime.MediaAudioNone,
	}[capability]
	outputJSON, err := json.Marshal(agentruntime.MediaGenerationToolResult{
		TaskID: taskID, BillingOrderID: billingOrderID, AudioMode: audioMode,
		Candidates: []agentruntime.ArtifactRevisionRef{{ArtifactID: candidate.ArtifactID, RevisionID: candidate.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	toolCall := model.AgentToolCall{
		ID: "tool-call-" + taskID, RunID: fixture.scope.RunID, ToolCallID: "media-generate-" + taskID,
		ActionVersion: 1, ToolName: string(agentruntime.ToolMediaGenerate), Status: agentruntime.ToolCallSucceeded,
		IdempotencyKey: fixture.scope.RunID + ":media:" + taskID, InputJSON: string(inputJSON), OutputJSON: string(outputJSON),
		StartedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	for _, create := range []func() error{
		func() error { return fixture.db.Create(&order).Error },
		func() error { return fixture.db.Create(&task).Error },
		func() error { return fixture.db.Create(&toolCall).Error },
	} {
		if err := create(); err != nil {
			t.Fatal(err)
		}
	}
	return candidateMediaToolLineage{
		TaskID: taskID, BillingOrderID: billingOrderID, ToolCallID: toolCall.ID,
		CandidateRequestIdentity: candidate.ModelRequestIdentity,
	}
}

func TestConsistencyReviewFailureStillKeepsNewProviderCandidate(t *testing.T) {
	fixture := generatedCandidateFixture(t, 2)
	now := time.Now().UTC()
	resourceID := "candidate-resource-unreviewed"
	if err := fixture.db.Create(&model.Resource{
		ID: resourceID, UserID: fixture.scope.ActorUserID, Kind: "image", Status: model.ResourceStatusReady,
		Provider: "oss", Endpoint: "oss-cn-hangzhou.aliyuncs.com", Bucket: "candidate-test",
		ObjectKey: "candidates/unreviewed.png", MimeType: "image/png", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	fixture.command.Candidates = append(fixture.command.Candidates, GeneratedMediaCandidate{
		ArtifactID: "media-candidate-unreviewed", ArtifactKey: "shot-1-candidate-unreviewed", MediaKind: "image",
		ResourceID: resourceID, SourceTaskID: "media-task-unreviewed", ProviderRequestIdentity: "provider-request-unreviewed",
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{}, SkillVersions: []agentruntime.SkillSelection{},
	})

	if _, err := fixture.service.ReviewVisualCandidates(context.Background(), fixture.scope, fixture.command); !errors.Is(err, ErrVisualCandidateReviewInvalid) {
		t.Fatalf("review error = %v, want %v", err, ErrVisualCandidateReviewInvalid)
	}
	stored, err := fixture.service.repo.MediaCandidateRevisionsInScope(fixture.scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 3 || stored[2].ResourceID != resourceID {
		t.Fatalf("candidate ledger after failed review = %#v", stored)
	}
}

func TestVisualConsistencyReviewCannotBypassCandidateSelection(t *testing.T) {
	fixture := generatedCandidateFixture(t, 2)
	review, err := fixture.service.ReviewVisualCandidates(context.Background(), fixture.scope, fixture.command)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&model.AgentProductionStage{}).Where("id = ?", fixture.stageID).Updates(model.AgentProductionStage{
		Status: agentruntime.StageAwaitingReview, Version: 2, ReviewRevisionID: review.ReviewRevision.ID,
	}).Error; err != nil {
		t.Fatal(err)
	}

	_, err = fixture.service.ReviewProductionStage(context.Background(), fixture.scope, fixture.parentRun, fixture.stageID, agentruntime.StageReviewCommand{
		StageVersion: 2, RevisionID: review.ReviewRevision.ID,
		Decision: agentruntime.StageReviewApprove, ClientRequestID: "bypass-candidate-selection",
	})
	if !errors.Is(err, ErrVisualCandidateSelectionRequired) {
		t.Fatalf("bypass approval error = %v, want %v", err, ErrVisualCandidateSelectionRequired)
	}
	var stage model.AgentProductionStage
	if err := fixture.db.Where("id = ?", fixture.stageID).First(&stage).Error; err != nil {
		t.Fatal(err)
	}
	if stage.Status != agentruntime.StageAwaitingReview || stage.Version != 2 {
		t.Fatalf("stage after rejected bypass = %#v", stage)
	}
}

func TestCandidateSelectionRollsBackWhenStageCASFails(t *testing.T) {
	fixture := generatedCandidateFixture(t, 2)
	review, err := fixture.service.ReviewVisualCandidates(context.Background(), fixture.scope, fixture.command)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&model.AgentProductionStage{}).Where("id = ?", fixture.stageID).Updates(model.AgentProductionStage{
		Status: agentruntime.StageAwaitingReview, Version: 2, ReviewRevisionID: review.ReviewRevision.ID,
	}).Error; err != nil {
		t.Fatal(err)
	}

	_, err = fixture.service.ApproveStageCandidate(context.Background(), fixture.scope, fixture.parentRun, fixture.stageID, StageCandidateApprovalCommand{
		StageVersion: 3, ReviewRevisionID: review.ReviewRevision.ID,
		SelectedCandidateRevisionID: fixture.candidateRevisions[0].ID, ClientRequestID: "stale-stage-version",
	})
	if !errors.Is(err, agentruntime.ErrProductionStageVersionConflict) {
		t.Fatalf("stale approval error = %v, want %v", err, agentruntime.ErrProductionStageVersionConflict)
	}
	var selectionCount int64
	if err := fixture.db.Model(&model.AgentArtifactRevision{}).
		Where("kind = ?", mediaCandidateSelectionKind).Count(&selectionCount).Error; err != nil {
		t.Fatal(err)
	}
	if selectionCount != 0 {
		t.Fatalf("selection revisions after failed CAS = %d, want 0", selectionCount)
	}
}

func TestCandidateApprovalRejectsMalformedPersistedReview(t *testing.T) {
	fixture := generatedCandidateFixture(t, 2)
	candidateRefs := make([]agentruntime.ArtifactRevisionRef, 0, len(fixture.candidateRevisions))
	for _, revision := range fixture.candidateRevisions {
		candidateRefs = append(candidateRefs, agentruntime.ArtifactRevisionRef{
			ArtifactID: revision.ArtifactID, RevisionID: revision.ID,
		})
	}
	malformedPayload, err := json.Marshal(visualConsistencyReviewPayload{
		ReviewRunID: "malformed-review-run", ReviewModelRecordID: fixture.command.ReviewModelRecordID,
		ReviewRequestIdentity: "malformed-review-request", CandidateRevisions: candidateRefs,
		ConfirmedReferenceRevisions: fixture.command.ConfirmedReferenceRevisions,
		Assessments:                 []storedVisualCandidateAssessment{{}, {}},
		RankedCandidateRevisionIDs:  []string{candidateRefs[0].RevisionID, candidateRefs[0].RevisionID},
		Uncertainties:               []string{}, Conflicts: []string{}, RetrySuggestions: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	upstream := append([]agentruntime.ArtifactRevisionRef{}, candidateRefs...)
	upstream = append(upstream, fixture.command.ConfirmedReferenceRevisions...)
	malformedReview, err := fixture.service.repo.AppendArtifactRevisionOnce(fixture.scope, "malformed-review", agentruntime.ArtifactDraft{
		ArtifactKey: "malformed-review", Kind: visualConsistencyReviewKind, SchemaVersion: 1,
		Payload: malformedPayload, UpstreamRevisions: upstream, ModelRequestIdentity: "malformed-review-request",
		SkillVersions: []agentruntime.SkillSelection{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&model.AgentProductionStage{}).Where("id = ?", fixture.stageID).Updates(model.AgentProductionStage{
		Status: agentruntime.StageAwaitingReview, Version: 2, ReviewRevisionID: malformedReview.ID,
	}).Error; err != nil {
		t.Fatal(err)
	}

	_, err = fixture.service.ApproveStageCandidate(context.Background(), fixture.scope, fixture.parentRun, fixture.stageID, StageCandidateApprovalCommand{
		StageVersion: 2, ReviewRevisionID: malformedReview.ID,
		SelectedCandidateRevisionID: candidateRefs[0].RevisionID, ClientRequestID: "reject-malformed-review",
	})
	if !errors.Is(err, ErrVisualCandidateSelectionInvalid) {
		t.Fatalf("malformed review approval error = %v, want %v", err, ErrVisualCandidateSelectionInvalid)
	}
}

func TestVisualConsistencyFindingsRejectDuplicateEvidenceReferences(t *testing.T) {
	confirmedReference := agentruntime.ArtifactRevisionRef{ArtifactID: "confirmed", RevisionID: "confirmed-r1"}
	visualEvidence := agentruntime.ArtifactRevisionRef{ArtifactID: "candidate-evidence", RevisionID: "candidate-evidence-r1"}
	findings := make([]VisualConsistencyFinding, 0, len(allVisualConsistencyDimensions()))
	for _, dimension := range allVisualConsistencyDimensions() {
		findings = append(findings, VisualConsistencyFinding{
			Dimension: dimension, Outcome: VisualConsistencyMatched, Description: "matches confirmed evidence",
			EvidenceRevisions:     []agentruntime.ArtifactRevisionRef{visualEvidence, visualEvidence, confirmedReference},
			ConfidenceBasisPoints: 9_000,
		})
	}
	if validConsistencyFindings(
		findings,
		visualEvidence,
		map[agentruntime.ArtifactRevisionRef]struct{}{confirmedReference: {}},
	) {
		t.Fatal("duplicate evidence reference was accepted")
	}
}

func TestCandidateSelectionDraftLocksReviewEvidenceRevisions(t *testing.T) {
	fixture := generatedCandidateFixture(t, 2)
	review, err := fixture.service.ReviewVisualCandidates(context.Background(), fixture.scope, fixture.command)
	if err != nil {
		t.Fatal(err)
	}
	_, draft, err := candidateSelectionDraft(
		fixture.scope,
		fixture.stageID,
		review.ReviewRevision,
		fixture.candidateRevisions[0],
		"lock-review-evidence",
	)
	if err != nil {
		t.Fatal(err)
	}
	var reviewUpstream []agentruntime.ArtifactRevisionRef
	if err := json.Unmarshal([]byte(review.ReviewRevision.UpstreamRevisionsJSON), &reviewUpstream); err != nil {
		t.Fatal(err)
	}
	expected := uniqueRevisionRefs(append([]agentruntime.ArtifactRevisionRef{
		{ArtifactID: review.ReviewRevision.ArtifactID, RevisionID: review.ReviewRevision.ID},
		{ArtifactID: fixture.candidateRevisions[0].ArtifactID, RevisionID: fixture.candidateRevisions[0].ID},
	}, reviewUpstream...))
	if !reflect.DeepEqual(draft.UpstreamRevisions, expected) {
		t.Fatalf("selection upstream = %#v, want %#v", draft.UpstreamRevisions, expected)
	}
}

type generatedCandidateReviewFixture struct {
	service            *Service
	db                 *gorm.DB
	scope              agentruntime.Scope
	parentRun          model.AgentRun
	stageID            string
	command            VisualCandidateReviewCommand
	resourceIDs        []string
	candidateRevisions []model.AgentArtifactRevision
}

func generatedCandidateFixture(t *testing.T, count int) generatedCandidateReviewFixture {
	t.Helper()
	request := specialistRuntimeRequestFixture("runtime-token-agent-model", "deepseek-v4-flash")
	request.SpecialistRunID = "specialist-run-consistency-review"
	request.InputRevisions = []agentruntime.ArtifactRevisionRef{}
	request.ExpectedOutputSchema = "visual_consistency_review.v1"
	request.LoadedSkills[0].CapabilityManifest.ArtifactSchemas = []string{"visual_consistency_review.v1"}
	service, db, runtimeFixture := newSpecialistRuntimeFixture(t, "https://example.com", request)
	parentRun := specialistParentRun(t, service, db, runtimeFixture.channelModel, request)
	scope := specialistRuntimeScope()
	now := time.Now().UTC()

	baselineResourceID := "consistency-baseline-resource"
	if err := db.Create(&model.Resource{
		ID: baselineResourceID, UserID: scope.ActorUserID, Kind: "image", Status: model.ResourceStatusReady,
		Provider: "oss", Endpoint: "oss-cn-hangzhou.aliyuncs.com", Bucket: "candidate-test",
		ObjectKey: "references/baseline.png", MimeType: "image/png", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	baselineSource, err := service.repo.AppendArtifactRevision(scope, "consistency-baseline", 0, agentruntime.ArtifactDraft{
		ArtifactKey: "consistency-baseline", Kind: "reference_image", SchemaVersion: 1,
		Payload: json.RawMessage(`{"caption":"已确认角色参考图"}`), ResourceID: baselineResourceID,
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{}, SkillVersions: []agentruntime.SkillSelection{},
	})
	if err != nil {
		t.Fatal(err)
	}
	baselineRef := agentruntime.ArtifactRevisionRef{ArtifactID: baselineSource.ArtifactID, RevisionID: baselineSource.ID}
	baselineEvidence, err := service.repo.AppendArtifactRevision(scope, "consistency-baseline-evidence", 0, agentruntime.ArtifactDraft{
		ArtifactKey: "consistency-baseline-evidence", Kind: "visual_evidence", SchemaVersion: 1,
		Payload:           visualEvidencePayloadFixture(t, baselineRef, runtimeFixture.channelModel.ID, "baseline-evidence-request", "人物静止"),
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{baselineRef}, ModelRequestIdentity: "baseline-evidence-request",
		SkillVersions: []agentruntime.SkillSelection{},
	})
	if err != nil {
		t.Fatal(err)
	}
	baselineEvidenceRef := agentruntime.ArtifactRevisionRef{ArtifactID: baselineEvidence.ArtifactID, RevisionID: baselineEvidence.ID}

	command := VisualCandidateReviewCommand{
		ReviewArtifactID: "visual-consistency-review-artifact", ReviewArtifactKey: "visual-consistency-review",
		ReviewModelRecordID: runtimeFixture.channelModel.ID, ReviewRequestIdentity: "visual-consistency-provider-request",
		ReviewRunID: "visual-consistency-run-1", ConfirmedReferenceRevisions: []agentruntime.ArtifactRevisionRef{baselineEvidenceRef},
		Candidates: []GeneratedMediaCandidate{}, Assessments: []VisualCandidateAssessment{}, RankedCandidateArtifactIDs: []string{},
		Uncertainties: []string{}, Conflicts: []string{}, RetrySuggestions: []string{}, SkillVersions: []agentruntime.SkillSelection{},
	}
	resourceIDs := make([]string, 0, count)
	candidateRevisions := make([]model.AgentArtifactRevision, 0, count)
	for index := 0; index < count; index++ {
		candidateNumber := index + 1
		artifactID := fmt.Sprintf("media-candidate-%d", candidateNumber)
		artifactKey := fmt.Sprintf("shot-1-candidate-%d", candidateNumber)
		resourceID := fmt.Sprintf("candidate-resource-%d", candidateNumber)
		requestIdentity := fmt.Sprintf("provider-candidate-request-%d:01", candidateNumber)
		if err := db.Create(&model.Resource{
			ID: resourceID, UserID: scope.ActorUserID, Kind: "image", Status: model.ResourceStatusReady,
			Provider: "oss", Endpoint: "oss-cn-hangzhou.aliyuncs.com", Bucket: "candidate-test",
			ObjectKey: fmt.Sprintf("candidates/%d.png", candidateNumber), MimeType: "image/png", CreatedAt: now, UpdatedAt: now,
		}).Error; err != nil {
			t.Fatal(err)
		}
		candidate := GeneratedMediaCandidate{
			ArtifactID: artifactID, ArtifactKey: artifactKey, MediaKind: "image", ResourceID: resourceID,
			SourceTaskID: fmt.Sprintf("media-task-%d", candidateNumber), ProviderRequestIdentity: requestIdentity,
			UpstreamRevisions: []agentruntime.ArtifactRevisionRef{}, SkillVersions: []agentruntime.SkillSelection{},
		}
		payload, err := json.Marshal(map[string]string{
			"candidateKey": artifactKey, "mediaKind": "image", "resourceId": resourceID,
			"sourceTaskId": candidate.SourceTaskID, "providerRequestIdentity": requestIdentity,
		})
		if err != nil {
			t.Fatal(err)
		}
		revision, err := service.repo.AppendMediaCandidateRevision(scope, artifactID, agentruntime.ArtifactDraft{
			ArtifactKey: artifactKey, Kind: "media_candidate", SchemaVersion: 1, Payload: payload,
			ResourceID: resourceID, UpstreamRevisions: []agentruntime.ArtifactRevisionRef{},
			ModelRequestIdentity: requestIdentity, SkillVersions: []agentruntime.SkillSelection{},
		})
		if err != nil {
			t.Fatal(err)
		}
		candidateRef := agentruntime.ArtifactRevisionRef{ArtifactID: revision.ArtifactID, RevisionID: revision.ID}
		evidenceArtifactID := fmt.Sprintf("candidate-evidence-%d", candidateNumber)
		evidenceRequestID := fmt.Sprintf("candidate-evidence-request-%d", candidateNumber)
		evidence, err := service.repo.AppendArtifactRevision(scope, evidenceArtifactID, 0, agentruntime.ArtifactDraft{
			ArtifactKey: evidenceArtifactID, Kind: "visual_evidence", SchemaVersion: 1,
			Payload:           visualEvidencePayloadFixture(t, candidateRef, runtimeFixture.channelModel.ID, evidenceRequestID, "人物向前行走"),
			UpstreamRevisions: []agentruntime.ArtifactRevisionRef{candidateRef}, ModelRequestIdentity: evidenceRequestID,
			SkillVersions: []agentruntime.SkillSelection{},
		})
		if err != nil {
			t.Fatal(err)
		}
		evidenceRef := agentruntime.ArtifactRevisionRef{ArtifactID: evidence.ArtifactID, RevisionID: evidence.ID}
		findings := make([]VisualConsistencyFinding, 0, len(allVisualConsistencyDimensions()))
		for _, dimension := range allVisualConsistencyDimensions() {
			findings = append(findings, VisualConsistencyFinding{
				Dimension: dimension, Outcome: VisualConsistencyMatched, Description: "候选与已确认参考证据一致",
				EvidenceRevisions: []agentruntime.ArtifactRevisionRef{evidenceRef, baselineEvidenceRef}, ConfidenceBasisPoints: 9000,
			})
		}
		command.Candidates = append(command.Candidates, candidate)
		command.Assessments = append(command.Assessments, VisualCandidateAssessment{
			CandidateArtifactID: artifactID, VisualEvidenceRevision: evidenceRef, Findings: findings,
		})
		command.RankedCandidateArtifactIDs = append(command.RankedCandidateArtifactIDs, artifactID)
		resourceIDs = append(resourceIDs, resourceID)
		candidateRevisions = append(candidateRevisions, *revision)
	}
	return generatedCandidateReviewFixture{
		service: service, db: db, scope: scope, parentRun: parentRun, stageID: request.StageID,
		command: command, resourceIDs: resourceIDs, candidateRevisions: candidateRevisions,
	}
}

func TestCoordinatePendingAgentVisualAnalysisCreatesTaskAndResolvesPersistedEvidence(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	var evidenceJSON string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/ai-open-platform-api/v1/chat/completions" {
			t.Fatalf("visual provider request path = %q", request.URL.Path)
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"id":      "visual-provider-coordinator",
			"choices": []map[string]any{{"message": map[string]any{"content": evidenceJSON}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 20, "completion_tokens": 30},
		})
	}))
	defer server.Close()

	svc, db, fixture := newAgentRuntimeServiceFixture(t, server.URL)
	configureAgentVisualAnalysisOSS(t, svc)
	createAgentRuntimeCanvas(t, db)
	scope := agentVisualAnalysisTestScope()
	now := time.Now().UTC()
	if err := db.Create(&model.Project{
		ID: scope.DomainProjectID, UserID: scope.ActorUserID, Name: "Visual Runtime Project",
		Type: "short-drama", Status: model.ProjectStatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", scope.CanvasID).
		Update("project_id", scope.DomainProjectID).Error; err != nil {
		t.Fatal(err)
	}
	visionModel := createAgentRuntimeVisionModel(t, db, fixture)
	source := createAgentRuntimeVisualSource(t, svc, db, scope, "visual-coordinator-source", model.ResourceStatusReady)
	configuration := agentruntime.RunConfiguration{
		ExecutionMode: agentruntime.ExecutionGuided,
		GenerationModels: agentruntime.GenerationModelSelections{Vision: &agentruntime.GenerationModelSelection{
			ChannelID: visionModel.ChannelID, Model: visionModel.ModelKey,
		}},
	}
	if _, err := svc.repo.CreateInitializedAgentRun(repository.CreateInitializedAgentRunInput{
		Create: repository.CreateAgentRunInput{Scope: scope, ClientRequestID: "visual-coordinator", Now: time.Now().UTC()},
		Initialize: repository.InitializeAgentRunInput{
			Scope: scope, ModelRecordID: fixture.channelModel.ID, ModelKey: fixture.channelModel.ModelKey,
			MaxSteps: 8, ToolSchemaVersion: agentruntime.ProductionToolSchemaVersion,
			RuntimeVersion: agentruntime.ProductionRuntimeVersion, PolicyVersion: agentruntime.ProductionPolicyVersion,
			UserMessage: "分析角色参考图", Configuration: configuration, Now: time.Now().UTC(),
		},
	}); err != nil {
		t.Fatal(err)
	}
	queued, err := svc.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		t.Fatal(err)
	}
	running, err := agentruntime.BeginModelRequest(queued)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CommitAgentRuntimeTransition(scope, queued, running, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	frozen := freezeAgentRuntimeVisualAnalysisForTest(t, svc, scope, visionModel, source)
	encodedEvidence, err := json.Marshal(validAgentRuntimeVisualEvidence(frozen))
	if err != nil {
		t.Fatal(err)
	}
	evidenceJSON = string(encodedEvidence)
	frozenArguments, err := json.Marshal(frozen)
	if err != nil {
		t.Fatal(err)
	}
	toolDecision := agentruntime.ToolCallDecision{
		ToolCallID: "vision-coordinator", ToolName: agentruntime.ToolVisionAnalyze, ActionVersion: 1,
		Arguments: frozenArguments, ExpectedDelivery: frozen.ExpectedDelivery,
	}
	waitingApproval, err := agentruntime.AdvanceForToolSchema(running.State, agentruntime.RuntimeInput{
		Decision: agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &toolDecision},
	}, agentruntime.ProductionToolSchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CommitAgentRuntimeTransition(scope, running.State, waitingApproval, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	approved, err := agentruntime.ReviewToolApproval(waitingApproval.State, agentruntime.ToolApproval{
		ToolCallID: toolDecision.ToolCallID, ActionVersion: toolDecision.ActionVersion,
		Decision: agentruntime.ToolApprovalApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CommitAgentRuntimeTransition(scope, waitingApproval.State, approved, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	progress, err := svc.coordinatePendingAgentTool(scope, CoordinateAgentToolInput{
		ToolCallID: toolDecision.ToolCallID, ActionVersion: toolDecision.ActionVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	if progress.State.Status != agentruntime.RunWaitingTool || !progress.State.PendingToolStarted {
		t.Fatalf("visual coordinator start state = %#v", progress.State)
	}
	var internalTaskCount int64
	if err := db.Model(&model.Task{}).
		Where("type = ? AND audience = ?", agentVisualAnalysisTaskType, model.TaskAudienceInternal).
		Count(&internalTaskCount).Error; err != nil {
		t.Fatal(err)
	}
	if internalTaskCount != 1 {
		t.Fatalf("visual coordinator internal task count = %d", internalTaskCount)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	call, err := svc.repo.AgentToolCallForScope(scope, toolDecision.ToolCallID, toolDecision.ActionVersion)
	if err != nil {
		t.Fatal(err)
	}
	if call.Status != agentruntime.ToolCallSucceeded || strings.TrimSpace(call.OutputJSON) == "" {
		var tasks []model.Task
		if err := db.Where("type = ?", agentVisualAnalysisTaskType).Find(&tasks).Error; err != nil {
			t.Fatal(err)
		}
		var logs []model.TaskLog
		if err := db.Where("task_id IN ?", []string{tasks[0].ID}).Order("created_at asc").Find(&logs).Error; err != nil {
			t.Fatal(err)
		}
		checkpoint, checkpointErr := svc.repo.LoadAgentCheckpoint(scope)
		t.Fatalf("visual coordinator tool call = %#v; tasks=%#v; logs=%#v; checkpoint=%#v; checkpointErr=%v", call, tasks, logs, checkpoint, checkpointErr)
	}
	if _, err := svc.repo.ArtifactHeadRevisionForScope(scope, frozen.OutputArtifactID); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureAgentVisualAnalysisTaskCreatesOneInternalTaskAndFrozenBillingOrder(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	scope := agentVisualAnalysisTestScope()
	visionModel := createAgentRuntimeVisionModel(t, db, fixture)
	source := createAgentRuntimeVisualSource(t, svc, db, scope, "visual-task-source", model.ResourceStatusReady)
	arguments := freezeAgentRuntimeVisualAnalysisForTest(t, svc, scope, visionModel, source)
	record := approvedAgentVisualToolCall(arguments, "runtime-run:vision:1")

	task, order, err := svc.ensureAgentVisualAnalysisTask(scope, record, arguments)
	if err != nil {
		t.Fatal(err)
	}
	if task.Audience != model.TaskAudienceInternal || task.Type != agentVisualAnalysisTaskType ||
		task.Operation != agentVisualAnalysisOperationForRun(scope.RunID) {
		t.Fatalf("visual analysis task facts = %#v", task)
	}
	if runID, ok := agentVisualAnalysisRunID(task.Operation); !ok || runID != scope.RunID {
		t.Fatalf("visual analysis task run identity = %q, %t", runID, ok)
	}
	if order.UserID != scope.ActorUserID || order.TaskID != task.ID || order.ChannelID != visionModel.ChannelID ||
		order.ChannelModelID != visionModel.ID || order.Model != visionModel.ModelKey || order.Capability != "vision" ||
		order.Scene != agentVisualAnalysisOperationForRun(scope.RunID) || order.Quantity != 1 || order.AmountMicrocredits != arguments.Commercial.AmountMicrocredits ||
		order.PriceVersion != arguments.Commercial.PriceVersion || order.IdempotencyKey != arguments.Commercial.BillingIdempotencyKey {
		t.Fatalf("visual analysis billing facts = %#v", order)
	}
	if _, err := svc.repo.TaskForCustomer(scope.ActorUserID, task.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("internal visual task was customer-visible: %v", err)
	}

	repeatedTask, repeatedOrder, err := svc.ensureAgentVisualAnalysisTask(scope, record, arguments)
	if err != nil {
		t.Fatal(err)
	}
	if repeatedTask.ID != task.ID || repeatedOrder.ID != order.ID {
		t.Fatalf("idempotent visual task changed identity: task=%s/%s order=%s/%s", task.ID, repeatedTask.ID, order.ID, repeatedOrder.ID)
	}
	var taskCount int64
	var orderCount int64
	if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("task_id = ?", task.ID).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 1 || orderCount != 1 {
		t.Fatalf("visual task idempotency counts: tasks=%d orders=%d", taskCount, orderCount)
	}
}

func TestEnsureAgentVisualAnalysisTaskRejectsChangedQuoteWithoutCommercialWrites(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	scope := agentVisualAnalysisTestScope()
	visionModel := createAgentRuntimeVisionModel(t, db, fixture)
	source := createAgentRuntimeVisualSource(t, svc, db, scope, "visual-quote-change-source", model.ResourceStatusReady)
	arguments := freezeAgentRuntimeVisualAnalysisForTest(t, svc, scope, visionModel, source)
	record := approvedAgentVisualToolCall(arguments, "runtime-run:vision:quote-change")

	if err := db.Model(&model.ChannelModel{}).Where("id = ?", visionModel.ID).Updates(map[string]any{
		"unit_price_microcredits": visionModel.UnitPriceMicrocredits + 25,
		"price_version":           visionModel.PriceVersion + 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	_, _, err := svc.ensureAgentVisualAnalysisTask(scope, record, arguments)
	if !errors.Is(err, ErrCostApprovalQuoteMismatch) {
		t.Fatalf("changed visual quote error = %v, want ErrCostApprovalQuoteMismatch", err)
	}
	var taskCount int64
	var orderCount int64
	var ledgerCount int64
	if err := db.Model(&model.Task{}).Where("type = ?", agentVisualAnalysisTaskType).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("capability = ?", "vision").Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).Count(&ledgerCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 || orderCount != 0 || ledgerCount != 0 {
		t.Fatalf("visual quote conflict wrote tasks=%d orders=%d ledgers=%d", taskCount, orderCount, ledgerCount)
	}
}

func TestEnsureAgentVisualAnalysisTaskRejectsMissingOrExpiredApprovalWithoutCommercialWrites(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	scope := agentVisualAnalysisTestScope()
	visionModel := createAgentRuntimeVisionModel(t, db, fixture)
	source := createAgentRuntimeVisualSource(t, svc, db, scope, "visual-expired-approval-source", model.ResourceStatusReady)
	arguments := freezeAgentRuntimeVisualAnalysisForTest(t, svc, scope, visionModel, source)

	missing := &model.AgentToolCall{IdempotencyKey: "runtime-run:vision:missing-approval", Status: agentruntime.ToolCallRunning}
	if _, _, err := svc.ensureAgentVisualAnalysisTask(scope, missing, arguments); !errors.Is(err, ErrCostApprovalQuoteMismatch) {
		t.Fatalf("missing approval error = %v, want ErrCostApprovalQuoteMismatch", err)
	}

	expired := approvedAgentVisualToolCall(arguments, "runtime-run:vision:expired-approval")
	expiredAt := arguments.Commercial.ExpiresAt
	expired.ApprovalDecidedAt = &expiredAt
	if _, _, err := svc.ensureAgentVisualAnalysisTask(scope, expired, arguments); !errors.Is(err, ErrCostApprovalQuoteMismatch) {
		t.Fatalf("expired approval error = %v, want ErrCostApprovalQuoteMismatch", err)
	}

	var taskCount, orderCount, ledgerCount int64
	if err := db.Model(&model.Task{}).Where("type = ?", agentVisualAnalysisTaskType).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("capability = ?", "vision").Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).Count(&ledgerCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 || orderCount != 0 || ledgerCount != 0 {
		t.Fatalf("rejected visual approvals wrote tasks=%d orders=%d ledgers=%d", taskCount, orderCount, ledgerCount)
	}
}

func TestProcessAgentVisualAnalysisTaskSignsInputAndPersistsOneStrictArtifactRevision(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	var evidenceJSON string
	providerCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		providerCalls++
		if request.URL.Path != "/ai-open-platform-api/v1/chat/completions" || request.Header.Get("ApiKey") != "runtime-secret-key" {
			t.Fatalf("visual provider request = %s key=%q", request.URL.Path, request.Header.Get("ApiKey"))
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["model"] != "vision-model" {
			t.Fatalf("visual provider model = %#v", payload["model"])
		}
		responseFormat, ok := payload["response_format"].(map[string]any)
		if !ok || responseFormat["type"] != "json_object" {
			t.Fatalf("visual provider response format = %#v", payload["response_format"])
		}
		bodyText := string(body)
		if !strings.Contains(bodyText, "OSSAccessKeyId=access-id") || !strings.Contains(bodyText, "Signature=") {
			t.Fatalf("visual provider request has no signed source URL: %s", bodyText)
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"id":      "visual-provider-request-1",
			"choices": []map[string]any{{"message": map[string]any{"content": evidenceJSON}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 20, "completion_tokens": 30},
		})
	}))
	defer server.Close()

	svc, db, fixture := newAgentRuntimeServiceFixture(t, server.URL)
	configureAgentVisualAnalysisOSS(t, svc)
	scope := agentVisualAnalysisTestScope()
	visionModel := createAgentRuntimeVisionModel(t, db, fixture)
	source := createAgentRuntimeVisualSource(t, svc, db, scope, "visual-provider-source", model.ResourceStatusReady)
	arguments := freezeAgentRuntimeVisualAnalysisForTest(t, svc, scope, visionModel, source)
	evidence := validAgentRuntimeVisualEvidence(arguments)
	encodedEvidence, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	evidenceJSON = string(encodedEvidence)
	record := approvedAgentVisualToolCall(arguments, "runtime-run:vision:provider")
	task, _, err := svc.ensureAgentVisualAnalysisTask(scope, record, arguments)
	if err != nil {
		t.Fatal(err)
	}

	first, canvasOps, err := svc.processTask(context.Background(), *task)
	if err != nil {
		t.Fatal(err)
	}
	if len(canvasOps) != 0 {
		t.Fatalf("visual analysis task produced canvas operations: %#v", canvasOps)
	}
	second, repeatedCanvasOps, err := svc.processTask(context.Background(), *task)
	if err != nil {
		t.Fatal(err)
	}
	if len(repeatedCanvasOps) != 0 {
		t.Fatalf("replayed visual analysis task produced canvas operations: %#v", repeatedCanvasOps)
	}
	if first["artifactId"] != arguments.OutputArtifactID || second["artifactId"] != arguments.OutputArtifactID || providerCalls != 1 {
		t.Fatalf("visual execution result=%#v repeat=%#v providerCalls=%d", first, second, providerCalls)
	}
	head, err := svc.repo.ArtifactHeadRevisionForScope(scope, arguments.OutputArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	storedEvidence, err := agentruntime.DecodeVisualEvidence([]byte(head.PayloadJSON))
	if err != nil {
		t.Fatal(err)
	}
	if head.Revision != 1 || head.Kind != "visual_evidence" || head.SchemaVersion != 1 ||
		head.ModelRequestIdentity != arguments.RequestIdentity || storedEvidence.RequestIdentity != arguments.RequestIdentity ||
		storedEvidence.VisionModelRecordID != arguments.VisionModelRecordID || storedEvidence.SourceRevision != arguments.SourceRevision {
		t.Fatalf("stored visual evidence = %#v revision=%#v", storedEvidence, head)
	}
	var revisionCount int64
	if err := db.Model(&model.AgentArtifactRevision{}).Where("artifact_id = ?", arguments.OutputArtifactID).Count(&revisionCount).Error; err != nil {
		t.Fatal(err)
	}
	if revisionCount != 1 {
		t.Fatalf("visual artifact revision count = %d", revisionCount)
	}
	if strings.Contains(task.InputJSON, "OSSAccessKeyId") || strings.Contains(task.InputJSON, "Signature=") ||
		strings.Contains(head.PayloadJSON, "OSSAccessKeyId") || strings.Contains(head.PayloadJSON, "Signature=") {
		t.Fatal("signed visual source URL leaked into persistent facts")
	}
}

func TestProcessAgentVisualAnalysisTaskRejectsSourceThatBecameStaleBeforeProviderCall(t *testing.T) {
	providerCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { providerCalls++ }))
	defer server.Close()
	svc, db, fixture := newAgentRuntimeServiceFixture(t, server.URL)
	configureAgentVisualAnalysisOSS(t, svc)
	scope := agentVisualAnalysisTestScope()
	visionModel := createAgentRuntimeVisionModel(t, db, fixture)
	source := createAgentRuntimeVisualSource(t, svc, db, scope, "visual-execution-stale", model.ResourceStatusReady)
	arguments := freezeAgentRuntimeVisualAnalysisForTest(t, svc, scope, visionModel, source)
	record := approvedAgentVisualToolCall(arguments, "runtime-run:vision:stale")
	task, _, err := svc.ensureAgentVisualAnalysisTask(scope, record, arguments)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repo.AppendArtifactRevision(scope, source.ArtifactID, 1, agentruntime.ArtifactDraft{
		ArtifactKey: source.ArtifactKey, Kind: source.Kind, SchemaVersion: source.SchemaVersion,
		Payload: json.RawMessage(`{"caption":"new source"}`), ResourceID: source.ResourceID,
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{}, SkillVersions: []agentruntime.SkillSelection{},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.processAgentVisualAnalysisTask(context.Background(), *task); !errors.Is(err, errAgentVisualSourceRevisionStale) {
		t.Fatalf("stale visual execution error = %v", err)
	}
	if providerCalls != 0 {
		t.Fatalf("stale visual execution called provider %d times", providerCalls)
	}
}

func TestProcessClaimedAgentVisualAnalysisTaskMarksBillingUncertainWhenSourceChangesDuringProviderCall(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	var svc *Service
	var scope agentruntime.Scope
	var source model.AgentArtifactRevision
	var evidenceJSON string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if _, err := svc.repo.AppendArtifactRevision(scope, source.ArtifactID, 1, agentruntime.ArtifactDraft{
			ArtifactKey: source.ArtifactKey, Kind: source.Kind, SchemaVersion: source.SchemaVersion,
			Payload: json.RawMessage(`{"caption":"source changed during provider call"}`), ResourceID: source.ResourceID,
			UpstreamRevisions: []agentruntime.ArtifactRevisionRef{}, SkillVersions: []agentruntime.SkillSelection{},
		}); err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"id":      "visual-provider-stale-after-request",
			"choices": []map[string]any{{"message": map[string]any{"content": evidenceJSON}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 20, "completion_tokens": 30},
		})
	}))
	defer server.Close()

	var db *gorm.DB
	var fixture agentRuntimeServiceFixture
	svc, db, fixture = newAgentRuntimeServiceFixture(t, server.URL)
	configureAgentVisualAnalysisOSS(t, svc)
	scope = agentVisualAnalysisTestScope()
	visionModel := createAgentRuntimeVisionModel(t, db, fixture)
	source = createAgentRuntimeVisualSource(t, svc, db, scope, "visual-provider-race-source", model.ResourceStatusReady)
	arguments := freezeAgentRuntimeVisualAnalysisForTest(t, svc, scope, visionModel, source)
	encodedEvidence, err := json.Marshal(validAgentRuntimeVisualEvidence(arguments))
	if err != nil {
		t.Fatal(err)
	}
	evidenceJSON = string(encodedEvidence)
	record := approvedAgentVisualToolCall(arguments, "runtime-run:vision:provider-race")
	task, order, err := svc.ensureAgentVisualAnalysisTask(scope, record, arguments)
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.ProcessNextTask(); !errors.Is(err, errAgentVisualSourceRevisionStale) {
		t.Fatalf("visual provider/source race error = %v", err)
	}
	storedTask, err := svc.repo.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	storedOrder, err := svc.repo.BillingOrder(order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != model.TaskStatusFailed || storedTask.Error != errAgentVisualSourceRevisionStale.Error() {
		t.Fatalf("visual provider/source race task = %#v", storedTask)
	}
	if storedOrder.Status != model.BillingStatusUncertain {
		t.Fatalf("visual provider/source race billing status = %q, want uncertain", storedOrder.Status)
	}
	hasSuccessfulCall, err := svc.repo.TaskHasSuccessfulBillableCall(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSuccessfulCall {
		t.Fatal("visual provider/source race did not retain successful provider call evidence")
	}
	if _, err := svc.repo.ArtifactHeadRevisionForScope(scope, arguments.OutputArtifactID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("stale visual evidence was persisted after provider success: %v", err)
	}
}

func TestFreezeAgentVisualAnalysisArgumentsFreezesCurrentSourceModelAndQuoteWithoutCommercialFacts(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	scope := agentVisualAnalysisTestScope()
	visionModel := createAgentRuntimeVisionModel(t, db, fixture)
	source := createAgentRuntimeVisualSource(t, svc, db, scope, "visual-source", model.ResourceStatusReady)
	delivery := agentRuntimeVisualEvidenceDelivery()
	payload, err := json.Marshal(VisionAnalyzeArguments{
		InputRevisions: []agentruntime.ArtifactRevisionRef{{ArtifactID: source.ArtifactID, RevisionID: source.ID}},
		ResourceIDs:    []string{source.ResourceID}, ExpectedOutputSchema: agentruntime.ArtifactSchemaVisualEvidenceV1,
		ExpectedDelivery: delivery,
	})
	if err != nil {
		t.Fatal(err)
	}
	configuration := agentruntime.RunConfiguration{
		ExecutionMode: agentruntime.ExecutionGuided,
		GenerationModels: agentruntime.GenerationModelSelections{Vision: &agentruntime.GenerationModelSelection{
			ChannelID: visionModel.ChannelID, Model: visionModel.ModelKey,
		}},
	}
	callable := []agentRuntimeCallableModelFact{{
		ChannelID: visionModel.ChannelID, Model: visionModel.ModelKey, DisplayName: visionModel.DisplayName,
		Capability: "vision", BillingMode: visionModel.BillingMode, PriceStrategy: visionModel.PriceStrategy,
		UnitPriceMicrocredits: visionModel.UnitPriceMicrocredits,
	}}

	frozenJSON, err := svc.freezeAgentVisualAnalysisArguments(scope, configuration, callable, payload, "visual-freeze", 1)
	if err != nil {
		t.Fatal(err)
	}
	var frozen agentVisualAnalysisArguments
	if err := json.Unmarshal(frozenJSON, &frozen); err != nil {
		t.Fatal(err)
	}
	if frozen.SourceRevision.ArtifactID != source.ArtifactID || frozen.SourceRevision.RevisionID != source.ID ||
		frozen.ResourceID != source.ResourceID || frozen.VisionModelRecordID != visionModel.ID ||
		frozen.VisionModel.ChannelID != visionModel.ChannelID || frozen.VisionModel.Model != visionModel.ModelKey ||
		frozen.ExpectedOutputSchema != agentruntime.ArtifactSchemaVisualEvidenceV1 || !frozen.ExpectedDelivery.Equal(delivery) {
		t.Fatalf("frozen visual facts = %#v", frozen)
	}
	if frozen.Commercial.AmountMicrocredits != visionModel.UnitPriceMicrocredits || frozen.Commercial.PriceVersion != visionModel.PriceVersion ||
		frozen.Commercial.BillingQuoteFingerprint == "" || frozen.Commercial.QuoteID == "" ||
		frozen.Commercial.ApprovalFingerprint == "" || frozen.Commercial.ChannelModelID != visionModel.ID ||
		frozen.Commercial.Capability != "vision" || frozen.Commercial.ProviderCapabilitiesJSON == "" ||
		frozen.Commercial.ExpiresAt.IsZero() || frozen.OutputArtifactID == "" || frozen.OutputArtifactKey == "" || frozen.RequestIdentity == "" {
		t.Fatalf("frozen visual commercial facts = %#v", frozen)
	}
	var taskCount int64
	var billingCount int64
	if err := db.Model(&model.Task{}).Where("type = ?", agentVisualAnalysisTaskType).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("scene = ?", agentVisualAnalysisOperationForRun(scope.RunID)).Count(&billingCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 || billingCount != 0 {
		t.Fatalf("approval-free freeze created commercial facts: tasks=%d billings=%d", taskCount, billingCount)
	}
}

func TestAgentRuntimeVisionAnalyzeFreezesPaidFactsBeforeApproval(t *testing.T) {
	var decision string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeAgentRuntimeChatStream(t, writer, "chatcmpl-visual-freeze", decision, 0, 0, 0)
	}))
	defer server.Close()
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")

	svc, db, fixture := newAgentRuntimeServiceFixture(t, server.URL)
	visionModel := createAgentRuntimeVisionModel(t, db, fixture)
	scope := agentVisualAnalysisTestScope()
	now := time.Now().UTC()
	if err := db.Create(&model.Project{
		ID: scope.DomainProjectID, UserID: scope.ActorUserID, Name: "Visual Freeze Project",
		Type: "short-drama", Status: model.ProjectStatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", scope.CanvasID).
		Update("project_id", scope.DomainProjectID).Error; err != nil {
		t.Fatal(err)
	}
	source := createAgentRuntimeVisualSource(t, svc, db, scope, "visual-runtime-source", model.ResourceStatusReady)
	delivery := agentRuntimeVisualEvidenceDelivery()
	if _, err := svc.repo.AppendProductionGraphVersion(scope, 0, agentruntime.ProductionGraphDraft{
		GraphKey: "visual-analysis-runtime",
		Stages: []agentruntime.ProductionStageDraft{{
			StageKey: "visual-analysis", SpecialistKey: agentruntime.SpecialistVisual,
			DependsOnStageKeys: []string{},
			InputRevisions: []agentruntime.ArtifactRevisionRef{{
				ArtifactID: source.ArtifactID, RevisionID: source.ID,
			}},
			ExpectedDelivery: delivery, ReviewPolicy: agentruntime.ReviewRequired,
			CostPolicy: agentruntime.CostApprovalRequired,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	deliveryJSON, err := json.Marshal(delivery)
	if err != nil {
		t.Fatal(err)
	}
	decision = agentRuntimeToolDecisionWithDelivery(t,
		`{"kind":"tool_call","toolCall":{"toolCallId":"analyze-runtime-source","toolName":"vision.analyze","actionVersion":1,"arguments":{"inputRevisions":[{"artifactId":"`+source.ArtifactID+`","revisionId":"`+source.ID+`"}],"resourceIds":["`+source.ResourceID+`"],"expectedOutputSchema":"visual_evidence.v1","expectedDelivery":`+string(deliveryJSON)+`}}}`,
		delivery,
	)
	configuration := agentruntime.RunConfiguration{
		ExecutionMode: agentruntime.ExecutionGuided,
		GenerationModels: agentruntime.GenerationModelSelections{Vision: &agentruntime.GenerationModelSelection{
			ChannelID: visionModel.ChannelID, Model: visionModel.ModelKey,
		}},
	}
	if _, err := svc.repo.CreateInitializedAgentRun(repository.CreateInitializedAgentRunInput{
		Create: repository.CreateAgentRunInput{Scope: scope, ClientRequestID: "visual-freeze-before-approval", Now: now},
		Initialize: repository.InitializeAgentRunInput{
			Scope: scope, ModelRecordID: fixture.channelModel.ID, ModelKey: fixture.channelModel.ModelKey,
			MaxSteps: 8, ToolSchemaVersion: agentruntime.ProductionToolSchemaVersion,
			RuntimeVersion: agentruntime.ProductionRuntimeVersion, PolicyVersion: agentruntime.ProductionPolicyVersion,
			UserMessage: "分析这张角色参考图", Configuration: configuration, Now: now,
		},
	}); err != nil {
		t.Fatal(err)
	}
	started, err := svc.advanceAgentRun(scope, agentWakeRunStarted)
	if err != nil {
		t.Fatal(err)
	}
	if started.ModelTask == nil {
		t.Fatal("agent runtime did not create the model decision task")
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	waiting, err := svc.ResumeAgentRuntime(scope)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.State.Status != agentruntime.RunWaitingApproval || waiting.State.PendingToolCall == nil {
		if waiting.State.DecisionFeedback != nil {
			t.Fatalf("visual analysis approval state = %s; feedback=%+v", waiting.State.Status, *waiting.State.DecisionFeedback)
		}
		t.Fatalf("visual analysis approval state = %#v", waiting.State)
	}
	frozen, err := decodeFrozenAgentVisualAnalysisArguments(waiting.State.PendingToolCall.Arguments)
	if err != nil {
		t.Fatalf("pending visual analysis arguments are not frozen: %v; arguments=%s", err, waiting.State.PendingToolCall.Arguments)
	}
	if frozen.SourceRevision.ArtifactID != source.ArtifactID || frozen.SourceRevision.RevisionID != source.ID ||
		frozen.ResourceID != source.ResourceID || frozen.VisionModelRecordID != visionModel.ID ||
		frozen.Commercial.AmountMicrocredits != visionModel.UnitPriceMicrocredits ||
		!frozen.ExpectedDelivery.Equal(delivery) || frozen.RequestIdentity == "" {
		t.Fatalf("pending visual analysis frozen facts = %#v", frozen)
	}
	var taskCount int64
	var billingCount int64
	if err := db.Model(&model.Task{}).Where("type = ?", agentVisualAnalysisTaskType).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("scene = ?", agentVisualAnalysisOperationForRun(waiting.Run.ID)).Count(&billingCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 || billingCount != 0 {
		t.Fatalf("visual analysis approval created commercial facts: tasks=%d billings=%d", taskCount, billingCount)
	}
}

func TestFreezeAgentVisualAnalysisArgumentsRejectsStaleSourceAndCrossScopeResource(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	scope := agentVisualAnalysisTestScope()
	visionModel := createAgentRuntimeVisionModel(t, db, fixture)
	source := createAgentRuntimeVisualSource(t, svc, db, scope, "visual-stale-source", model.ResourceStatusReady)
	delivery := agentRuntimeVisualEvidenceDelivery()
	configuration := agentruntime.RunConfiguration{
		ExecutionMode: agentruntime.ExecutionGuided,
		GenerationModels: agentruntime.GenerationModelSelections{Vision: &agentruntime.GenerationModelSelection{
			ChannelID: visionModel.ChannelID, Model: visionModel.ModelKey,
		}},
	}
	callable := []agentRuntimeCallableModelFact{{
		ChannelID: visionModel.ChannelID, Model: visionModel.ModelKey, DisplayName: visionModel.DisplayName,
		Capability: "vision", BillingMode: visionModel.BillingMode, PriceStrategy: visionModel.PriceStrategy,
		UnitPriceMicrocredits: visionModel.UnitPriceMicrocredits,
	}}
	encode := func(revision model.AgentArtifactRevision, resourceID string) []byte {
		payload, err := json.Marshal(VisionAnalyzeArguments{
			InputRevisions: []agentruntime.ArtifactRevisionRef{{ArtifactID: revision.ArtifactID, RevisionID: revision.ID}},
			ResourceIDs:    []string{resourceID}, ExpectedOutputSchema: agentruntime.ArtifactSchemaVisualEvidenceV1,
			ExpectedDelivery: delivery,
		})
		if err != nil {
			t.Fatal(err)
		}
		return payload
	}

	if _, err := svc.repo.AppendArtifactRevision(scope, source.ArtifactID, 1, agentruntime.ArtifactDraft{
		ArtifactKey: source.ArtifactKey, Kind: source.Kind, SchemaVersion: source.SchemaVersion,
		Payload: json.RawMessage(`{"caption":"new source"}`), ResourceID: source.ResourceID,
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{}, SkillVersions: []agentruntime.SkillSelection{},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.freezeAgentVisualAnalysisArguments(scope, configuration, callable, encode(source, source.ResourceID), "visual-stale", 1); !errors.Is(err, errAgentVisualSourceRevisionStale) {
		t.Fatalf("stale visual source error = %v", err)
	}

	crossScope := model.Resource{
		ID: "cross-scope-visual-resource", UserID: "different-user", Kind: "image", Status: model.ResourceStatusReady,
		Provider: "aliyun", Endpoint: "oss-cn-hangzhou.aliyuncs.com", Bucket: "bucket", ObjectKey: "visual/cross.png",
		MimeType: "image/png", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := db.Create(&crossScope).Error; err != nil {
		t.Fatal(err)
	}
	crossSource, err := svc.repo.AppendArtifactRevision(scope, "visual-cross-scope-source", 0, agentruntime.ArtifactDraft{
		ArtifactKey: "visual-cross-scope-source-key", Kind: "reference_image", SchemaVersion: 1,
		Payload: json.RawMessage(`{"caption":"cross-scope source"}`), ResourceID: crossScope.ID,
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{}, SkillVersions: []agentruntime.SkillSelection{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.freezeAgentVisualAnalysisArguments(scope, configuration, callable, encode(*crossSource, crossScope.ID), "visual-cross-scope", 1); !errors.Is(err, errAgentVisualInputUnavailable) {
		t.Fatalf("cross-scope visual resource error = %v", err)
	}
}

func TestFreezeAgentVisualAnalysisDecisionArgumentsRejectsDeliveryContractConflict(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	scope := agentVisualAnalysisTestScope()
	visionModel := createAgentRuntimeVisionModel(t, db, fixture)
	source := createAgentRuntimeVisualSource(t, svc, db, scope, "visual-delivery-conflict-source", model.ResourceStatusReady)
	delivery := agentRuntimeVisualEvidenceDelivery()
	configuration := agentruntime.RunConfiguration{
		ExecutionMode: agentruntime.ExecutionGuided,
		GenerationModels: agentruntime.GenerationModelSelections{Vision: &agentruntime.GenerationModelSelection{
			ChannelID: visionModel.ChannelID, Model: visionModel.ModelKey,
		}},
	}
	callable := []agentRuntimeCallableModelFact{{
		ChannelID: visionModel.ChannelID, Model: visionModel.ModelKey, DisplayName: visionModel.DisplayName,
		Capability: "vision", BillingMode: visionModel.BillingMode, PriceStrategy: visionModel.PriceStrategy,
		UnitPriceMicrocredits: visionModel.UnitPriceMicrocredits,
	}}
	payload, err := json.Marshal(VisionAnalyzeArguments{
		InputRevisions: []agentruntime.ArtifactRevisionRef{{ArtifactID: source.ArtifactID, RevisionID: source.ID}},
		ResourceIDs:    []string{source.ResourceID}, ExpectedOutputSchema: agentruntime.ArtifactSchemaVisualEvidenceV1,
		ExpectedDelivery: delivery,
	})
	if err != nil {
		t.Fatal(err)
	}
	conflictingDelivery := agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryAnswer,
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactFinalMessage}},
	}
	call := &agentruntime.ToolCallDecision{
		ToolCallID: "visual-delivery-conflict", ToolName: agentruntime.ToolVisionAnalyze,
		ActionVersion: 1, Arguments: payload, ExpectedDelivery: conflictingDelivery,
	}
	if _, err := svc.freezeAgentVisualAnalysisDecisionArguments(scope, configuration, callable, call); !errors.Is(err, errAgentVisualArgumentsInvalid) {
		t.Fatalf("delivery contract conflict error = %v", err)
	}
}

func TestFreezeAgentVisualAnalysisDecisionArgumentsUsesExactToolInvocationIdentity(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	scope := agentVisualAnalysisTestScope()
	visionModel := createAgentRuntimeVisionModel(t, db, fixture)
	source := createAgentRuntimeVisualSource(t, svc, db, scope, "visual-invocation-source", model.ResourceStatusReady)
	delivery := agentRuntimeVisualEvidenceDelivery()
	configuration := agentruntime.RunConfiguration{
		ExecutionMode: agentruntime.ExecutionGuided,
		GenerationModels: agentruntime.GenerationModelSelections{Vision: &agentruntime.GenerationModelSelection{
			ChannelID: visionModel.ChannelID, Model: visionModel.ModelKey,
		}},
	}
	callable := []agentRuntimeCallableModelFact{{
		ChannelID: visionModel.ChannelID, Model: visionModel.ModelKey, DisplayName: visionModel.DisplayName,
		Capability: "vision", BillingMode: visionModel.BillingMode, PriceStrategy: visionModel.PriceStrategy,
		UnitPriceMicrocredits: visionModel.UnitPriceMicrocredits,
	}}
	payload, err := json.Marshal(VisionAnalyzeArguments{
		InputRevisions: []agentruntime.ArtifactRevisionRef{{ArtifactID: source.ArtifactID, RevisionID: source.ID}},
		ResourceIDs:    []string{source.ResourceID}, ExpectedOutputSchema: agentruntime.ArtifactSchemaVisualEvidenceV1,
		ExpectedDelivery: delivery,
	})
	if err != nil {
		t.Fatal(err)
	}
	freeze := func(toolCallID string) agentVisualAnalysisArguments {
		t.Helper()
		call := &agentruntime.ToolCallDecision{
			ToolCallID: toolCallID, ToolName: agentruntime.ToolVisionAnalyze,
			ActionVersion: 1, Arguments: payload, ExpectedDelivery: delivery,
		}
		frozenJSON, freezeErr := svc.freezeAgentVisualAnalysisDecisionArguments(scope, configuration, callable, call)
		if freezeErr != nil {
			t.Fatal(freezeErr)
		}
		frozen, decodeErr := decodeFrozenAgentVisualAnalysisArguments(frozenJSON)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		return frozen
	}

	first := freeze("visual-invocation-1")
	repeated := freeze("visual-invocation-1")
	second := freeze("visual-invocation-2")
	if first.RequestIdentity != repeated.RequestIdentity || first.OutputArtifactID != repeated.OutputArtifactID {
		t.Fatalf("same invocation identity changed: first=%#v repeated=%#v", first, repeated)
	}
	if first.RequestIdentity == second.RequestIdentity || first.OutputArtifactID == second.OutputArtifactID {
		t.Fatalf("distinct visual invocations reused paid output identity: first=%#v second=%#v", first, second)
	}
}

func TestAgentVisualAnalysisTaskFailureCodePreservesStaleAndUnavailableFacts(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "stale source", err: errAgentVisualSourceRevisionStale, want: "visual_evidence_stale"},
		{name: "unavailable input", err: errAgentVisualInputUnavailable, want: "visual_analysis_input_unavailable"},
		{name: "generic provider failure", err: errors.New("provider failed"), want: "visual_analysis_failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			task := model.Task{Error: test.err.Error()}
			if got := agentVisualAnalysisTaskFailureCode(task); got != test.want {
				t.Fatalf("visual analysis task failure code = %q, want %q", got, test.want)
			}
		})
	}
}

func TestAppendAgentVisualEvidenceRejectsSourceChangedAfterFreeze(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	scope := agentVisualAnalysisTestScope()
	visionModel := createAgentRuntimeVisionModel(t, db, fixture)
	source := createAgentRuntimeVisualSource(t, svc, db, scope, "visual-source-changed", model.ResourceStatusReady)
	arguments := freezeAgentRuntimeVisualAnalysisForTest(t, svc, scope, visionModel, source)

	if _, err := svc.repo.AppendArtifactRevision(scope, source.ArtifactID, source.Revision, agentruntime.ArtifactDraft{
		ArtifactKey: source.ArtifactKey, Kind: source.Kind, SchemaVersion: source.SchemaVersion,
		Payload: json.RawMessage(`{"caption":"source changed during provider request"}`), ResourceID: source.ResourceID,
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{}, SkillVersions: []agentruntime.SkillSelection{},
	}); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(validAgentRuntimeVisualEvidence(arguments))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.appendAgentVisualEvidence(scope, agentVisualAnalysisExecution(arguments), payload); !errors.Is(err, errAgentVisualSourceRevisionStale) {
		t.Fatalf("changed source append error = %v, want %v", err, errAgentVisualSourceRevisionStale)
	}
	if _, err := svc.repo.ArtifactHeadRevisionForScope(scope, arguments.OutputArtifactID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("stale visual evidence artifact was persisted: %v", err)
	}
}

func agentVisualAnalysisTestScope() agentruntime.Scope {
	scope := agentRuntimeServiceScope()
	scope.DomainProjectID = "runtime-project"
	return scope
}

func createAgentRuntimeVisionModel(t *testing.T, db *gorm.DB, fixture agentRuntimeServiceFixture) model.ChannelModel {
	t.Helper()
	now := time.Now().UTC()
	channel := model.ModelChannel{
		ID: "runtime-vision-channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "Agent Vision",
		APIFormat: "openai", InterfaceType: model.ChannelInterfaceChatCompletion,
		ModelsJSON: `["vision-model"]`, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	item := model.ChannelModel{
		ID: "runtime-vision-model", ChannelID: channel.ID, ModelKey: "vision-model", DisplayName: "Vision Model",
		ProviderCredentialID: fixture.credential.ID, AccessPolicy: model.ModelAccessAuthenticated, Capability: "vision",
		BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 275, PriceConfigured: true,
		Enabled: true, PriceVersion: 4, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	return item
}

func createAgentRuntimeVisualSource(
	t *testing.T,
	svc *Service,
	db *gorm.DB,
	scope agentruntime.Scope,
	artifactID string,
	status model.ResourceStatus,
) model.AgentArtifactRevision {
	t.Helper()
	now := time.Now().UTC()
	resource := model.Resource{
		ID: artifactID + "-resource", UserID: scope.ActorUserID, Kind: "image", Status: status,
		Provider: "aliyun", Endpoint: "oss-cn-hangzhou.aliyuncs.com", Bucket: "bucket", ObjectKey: "visual/" + artifactID + ".png",
		MimeType: "image/png", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	revision, err := svc.repo.AppendArtifactRevision(scope, artifactID, 0, agentruntime.ArtifactDraft{
		ArtifactKey: artifactID + "-key", Kind: "reference_image", SchemaVersion: 1,
		Payload: json.RawMessage(`{"caption":"source"}`), ResourceID: resource.ID,
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{}, SkillVersions: []agentruntime.SkillSelection{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return *revision
}

func agentRuntimeVisualEvidenceDelivery() agentruntime.ExpectedDelivery {
	return agentruntime.ExpectedDelivery{
		Kind: agentruntime.DeliveryGeneratedAsset, RequiredArtifacts: []agentruntime.ArtifactKind{agentruntime.ArtifactText},
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactArtifact, Artifact: agentruntime.ArtifactText}},
	}
}

func freezeAgentRuntimeVisualAnalysisForTest(
	t *testing.T,
	svc *Service,
	scope agentruntime.Scope,
	visionModel model.ChannelModel,
	source model.AgentArtifactRevision,
) agentVisualAnalysisArguments {
	t.Helper()
	payload, err := json.Marshal(VisionAnalyzeArguments{
		InputRevisions: []agentruntime.ArtifactRevisionRef{{ArtifactID: source.ArtifactID, RevisionID: source.ID}},
		ResourceIDs:    []string{source.ResourceID}, ExpectedOutputSchema: agentruntime.ArtifactSchemaVisualEvidenceV1,
		ExpectedDelivery: agentRuntimeVisualEvidenceDelivery(),
	})
	if err != nil {
		t.Fatal(err)
	}
	configuration := agentruntime.RunConfiguration{
		ExecutionMode: agentruntime.ExecutionGuided,
		GenerationModels: agentruntime.GenerationModelSelections{Vision: &agentruntime.GenerationModelSelection{
			ChannelID: visionModel.ChannelID, Model: visionModel.ModelKey,
		}},
	}
	callable := []agentRuntimeCallableModelFact{{
		ChannelID: visionModel.ChannelID, Model: visionModel.ModelKey, DisplayName: visionModel.DisplayName,
		Capability: "vision", BillingMode: visionModel.BillingMode, PriceStrategy: visionModel.PriceStrategy,
		UnitPriceMicrocredits: visionModel.UnitPriceMicrocredits,
	}}
	frozenJSON, err := svc.freezeAgentVisualAnalysisArguments(scope, configuration, callable, payload, "visual-test", 1)
	if err != nil {
		t.Fatal(err)
	}
	var arguments agentVisualAnalysisArguments
	if err := json.Unmarshal(frozenJSON, &arguments); err != nil {
		t.Fatal(err)
	}
	return arguments
}

func approvedAgentVisualToolCall(arguments agentVisualAnalysisArguments, idempotencyKey string) *model.AgentToolCall {
	approvedAt := arguments.Commercial.ExpiresAt.Add(-time.Minute)
	return &model.AgentToolCall{
		IdempotencyKey: idempotencyKey, Status: agentruntime.ToolCallRunning,
		ApprovalDecision: agentruntime.ToolApprovalApproved, ApprovalDecidedAt: &approvedAt,
	}
}

func configureAgentVisualAnalysisOSS(t *testing.T, svc *Service) {
	t.Helper()
	settingJSON, err := json.Marshal(ossSettingValue{
		Enabled: true, Provider: "aliyun", Endpoint: "https://oss-cn-hangzhou.aliyuncs.com", Bucket: "bucket",
		AccessKeyID: "access-id", AccessKeySecret: "access-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.SaveSystemSetting(&model.SystemSetting{Key: ossSettingKey, ValueJSON: string(settingJSON)}); err != nil {
		t.Fatal(err)
	}
}

func validAgentRuntimeVisualEvidence(arguments agentVisualAnalysisArguments) agentruntime.VisualEvidence {
	return agentruntime.VisualEvidence{
		SourceRevision: arguments.SourceRevision,
		Characters: []agentruntime.VisualCharacter{{
			Key: "character-1", Name: "角色一", Clothing: "蓝色外套", Hair: "黑色短发",
			StableFeatures: []string{"左眉上方有痣"},
		}},
		IdentityEvidence: []agentruntime.VisualIdentityEvidence{{CharacterKey: "character-1", Observations: []string{"正面脸部清晰"}}},
		Scene:            agentruntime.VisualSceneEvidence{Key: "scene-1", Description: "室内暖光客厅"},
		Props:            []agentruntime.VisualPropEvidence{},
		SpatialRelations: []agentruntime.VisualSpatialRelation{{SubjectKey: "character-1", Relation: "位于", ObjectKey: "scene-1"}},
		Shot: agentruntime.VisualShotEvidence{
			ShotSize: "中景", Angle: "平视", Composition: "居中构图", ScreenDirection: "面向画面右侧",
			Gaze: "看向镜头外", FirstFrameCondition: "人物站立", LastFrameCondition: "人物仍站立",
		},
		ActionState: "人物静止", OCRText: []string{}, Uncertainties: []agentruntime.VisualEvidenceIssue{},
		Conflicts: []agentruntime.VisualEvidenceIssue{}, ConfidenceBasisPoints: 9200,
		VisionModelRecordID: arguments.VisionModelRecordID, RequestIdentity: arguments.RequestIdentity,
	}
}
