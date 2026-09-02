package repository

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrAgentSpecialistRunConflict    = errors.New("agent specialist run conflict")
	ErrAgentSpecialistTaskLease      = errors.New("agent specialist task lease conflict")
	ErrProductionStageReviewConflict = errors.New("production stage review conflict")
)

type CreateAgentSpecialistRunInput struct {
	Scope             agentruntime.Scope
	Request           agentruntime.SpecialistRequest
	ToolSchemaVersion int
	Now               time.Time
}

type CompleteAgentSpecialistRunInput struct {
	Scope             agentruntime.Scope
	SpecialistRunID   string
	LeaseOwner        string
	LeaseGeneration   uint64
	LeaseToken        string
	ProviderRequestID string
	ResultJSON        string
	ResultSummary     string
	Drafts            []agentruntime.ArtifactDraft
	InputTokens       int64
	CachedTokens      int64
	OutputTokens      int64
	Now               time.Time
}

type FailAgentSpecialistRunInput struct {
	Scope           agentruntime.Scope
	SpecialistRunID string
	LeaseOwner      string
	LeaseGeneration uint64
	LeaseToken      string
	ErrorCode       string
	ErrorText       string
	BillingAction   FailedTaskBillingAction
	Now             time.Time
}

type ReviewProductionStageInput struct {
	Scope              agentruntime.Scope
	StageID            string
	Command            agentruntime.StageReviewCommand
	RevisionRequest    *agentruntime.SpecialistRequest
	CandidateSelection *StageCandidateSelectionInput
	ToolSchemaVersion  int
	Now                time.Time
}

type StageCandidateSelectionInput struct {
	ArtifactID string
	Draft      agentruntime.ArtifactDraft
}

type ReviewProductionStageResult struct {
	Stage                      model.AgentProductionStage
	SpecialistRun              *model.AgentSpecialistRun
	CandidateSelectionRevision *model.AgentArtifactRevision
	ReviewID                   string
}

func (r *Repository) CreateAgentSpecialistRun(input CreateAgentSpecialistRunInput) (*model.AgentSpecialistRun, error) {
	if err := validateProductionRepositoryScope(input.Scope, true); err != nil {
		return nil, err
	}
	if input.Now.IsZero() || input.ToolSchemaVersion < 1 {
		return nil, ErrAgentSpecialistRunConflict
	}
	request := input.Request
	candidate, err := newAgentSpecialistRunCandidate(input.Scope, request, input.ToolSchemaVersion, input.Now)
	if err != nil {
		return nil, err
	}
	var stored model.AgentSpecialistRun
	err = r.db.Transaction(func(tx *gorm.DB) error {
		if err := requireActiveProductionAgentRunTx(tx, input.Scope); err != nil {
			return err
		}
		existingErr := agentSpecialistScopeQuery(tx, input.Scope).Where("id = ?", candidate.ID).First(&stored).Error
		if existingErr == nil {
			if !sameAgentSpecialistRunFacts(stored, candidate) {
				return ErrAgentSpecialistRunConflict
			}
			return nil
		}
		if !errors.Is(existingErr, gorm.ErrRecordNotFound) {
			return existingErr
		}
		var stage model.AgentProductionStage
		if stageErr := productionStageScopeQuery(tx.Model(&model.AgentProductionStage{}), input.Scope).
			Where("id = ?", request.StageID).First(&stage).Error; stageErr != nil {
			return stageErr
		}
		if stage.SpecialistKey != request.SpecialistKey || stage.InputRevisionsJSON != candidate.InputRevisionsJSON ||
			stage.ExpectedDeliveryJSON != candidate.ExpectedDeliveryJSON {
			return ErrAgentSpecialistRunConflict
		}
		// Revision children are created atomically with the review command by
		// ReviewProductionStage. The general create path is initial-run only.
		if stage.Status != agentruntime.StagePlanned || candidate.ParentSpecialistRunID != "" {
			return ErrAgentSpecialistRunConflict
		}
		createErr := tx.Create(&candidate).Error
		if createErr != nil && !isUniqueConstraintError(createErr) {
			return createErr
		}
		if err := agentSpecialistScopeQuery(tx, input.Scope).Where("id = ?", candidate.ID).First(&stored).Error; err != nil {
			return err
		}
		if !sameAgentSpecialistRunFacts(stored, candidate) {
			return ErrAgentSpecialistRunConflict
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &stored, nil
}

func (r *Repository) AgentSpecialistRunForScope(scope agentruntime.Scope, specialistRunID string) (*model.AgentSpecialistRun, error) {
	if err := validateProductionRepositoryScope(scope, false); err != nil {
		return nil, err
	}
	var run model.AgentSpecialistRun
	if err := agentSpecialistScopeQuery(r.db, scope).Where("id = ?", specialistRunID).First(&run).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *Repository) AgentSpecialistRunForActor(specialistRunID string, actorUserID string) (*model.AgentSpecialistRun, error) {
	specialistRunID = strings.TrimSpace(specialistRunID)
	actorUserID = strings.TrimSpace(actorUserID)
	if specialistRunID == "" || actorUserID == "" {
		return nil, errors.New("agent specialist run identity is invalid")
	}
	var run model.AgentSpecialistRun
	if err := r.db.Where("id = ? AND actor_user_id = ?", specialistRunID, actorUserID).Take(&run).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *Repository) AgentSpecialistRevisions(scope agentruntime.Scope, specialistRunID string) ([]model.AgentArtifactRevision, error) {
	if err := validateProductionRepositoryScope(scope, false); err != nil {
		return nil, err
	}
	var revisions []model.AgentArtifactRevision
	err := productionArtifactRevisionScopeQuery(r.db, scope).
		Where("created_by_specialist_id = ?", specialistRunID).
		Order("created_at ASC, id ASC").Find(&revisions).Error
	return revisions, err
}

func (r *Repository) ReviewProductionStage(input ReviewProductionStageInput) (*ReviewProductionStageResult, error) {
	if err := validateProductionRepositoryScope(input.Scope, true); err != nil {
		return nil, err
	}
	if input.Now.IsZero() || strings.TrimSpace(input.StageID) != input.StageID || input.StageID == "" || len(input.StageID) > 80 {
		return nil, ErrProductionStageConflict
	}
	var candidate *model.AgentSpecialistRun
	if input.RevisionRequest != nil {
		created, err := newAgentSpecialistRunCandidate(input.Scope, *input.RevisionRequest, input.ToolSchemaVersion, input.Now)
		if err != nil {
			return nil, err
		}
		candidate = &created
	}
	if input.CandidateSelection != nil {
		selection := input.CandidateSelection
		if input.Command.Decision != agentruntime.StageReviewApprove || input.RevisionRequest != nil ||
			strings.TrimSpace(selection.ArtifactID) != selection.ArtifactID || selection.ArtifactID == "" ||
			selection.Draft.Kind != "media_candidate_selection" || selection.Draft.SchemaVersion != 1 ||
			selection.Draft.ResourceID != "" || selection.Draft.ModelRequestIdentity != "" ||
			agentruntime.ValidateArtifactDraft(selection.Draft) != nil {
			return nil, ErrProductionStageReviewConflict
		}
	}
	var result ReviewProductionStageResult
	err := r.db.Transaction(func(tx *gorm.DB) error {
		replayed, err := loadStageReviewReplayTx(tx, input, candidate, &result)
		if err != nil || replayed {
			return err
		}
		if err := requireActiveProductionAgentRunTx(tx, input.Scope); err != nil {
			return err
		}
		// The parent-run lock serializes duplicate review commands in production.
		// Re-check after acquiring it so a concurrent exact approval can replay
		// the committed fact instead of attempting the stage transition again.
		replayed, err = loadStageReviewReplayTx(tx, input, candidate, &result)
		if err != nil || replayed {
			return err
		}

		var stage model.AgentProductionStage
		stageQuery := productionStageScopeQuery(tx, input.Scope).Where("id = ?", input.StageID)
		if r.Dialect() == "postgres" {
			stageQuery = stageQuery.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := stageQuery.First(&stage).Error; err != nil {
			return err
		}
		next, err := agentruntime.TransitionProductionStage(agentruntime.ProductionStageState{
			StageKey: stage.StageKey, Status: stage.Status, Version: stage.Version, ReviewRevisionID: stage.ReviewRevisionID,
		}, input.Command)
		if err != nil {
			return err
		}
		if input.Command.Decision == agentruntime.StageReviewRequestRevision {
			if candidate == nil || candidate.StageID != stage.ID || candidate.SpecialistKey != stage.SpecialistKey ||
				candidate.InputRevisionsJSON != stage.InputRevisionsJSON || candidate.ExpectedDeliveryJSON != stage.ExpectedDeliveryJSON ||
				candidate.ParentSpecialistRunID == "" {
				return ErrAgentSpecialistRunConflict
			}
			if err := requireSucceededSpecialistParentTx(tx, input.Scope, stage.ID, candidate.ParentSpecialistRunID); err != nil {
				return err
			}
		} else if candidate != nil {
			return ErrAgentSpecialistRunConflict
		}
		if input.CandidateSelection != nil {
			selection, err := appendOrReplayStageCandidateSelectionTx(tx, input.Scope, *input.CandidateSelection)
			if err != nil {
				return err
			}
			result.CandidateSelectionRevision = selection
		}
		update := productionStageScopeQuery(tx, input.Scope).
			Where("id = ? AND status = ? AND version = ? AND review_revision_id = ?", stage.ID, stage.Status, stage.Version, stage.ReviewRevisionID).
			Select("status", "version", "review_revision_id", "updated_at").
			Updates(model.AgentProductionStage{Status: next.Status, Version: next.Version, ReviewRevisionID: next.ReviewRevisionID, UpdatedAt: input.Now})
		if update.Error != nil || update.RowsAffected != 1 {
			return ErrProductionStageConflict
		}
		if candidate != nil {
			if err := tx.Create(candidate).Error; err != nil {
				if isUniqueConstraintError(err) {
					return ErrAgentSpecialistRunConflict
				}
				return err
			}
			result.SpecialistRun = candidate
		}
		if err := productionStageScopeQuery(tx, input.Scope).Where("id = ?", stage.ID).First(&result.Stage).Error; err != nil {
			return err
		}
		if err := appendStageReviewResolutionTx(tx, input.Scope, result.Stage, input.Command, input.Now); err != nil {
			return err
		}
		result.ReviewID = agentStageReviewTimelineItemID(input.Scope.RunID, input.StageID, input.Command.RevisionID)
		if input.Command.Decision == agentruntime.StageReviewStop {
			checkpoint, err := loadAgentCheckpointForScope(tx, input.Scope, true)
			if err != nil {
				return err
			}
			if _, err := r.interruptAgentRunTreeTx(tx, input.Scope, checkpoint.StateVersion, input.Now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func loadStageReviewReplayTx(
	tx *gorm.DB,
	input ReviewProductionStageInput,
	candidate *model.AgentSpecialistRun,
	result *ReviewProductionStageResult,
) (bool, error) {
	itemID := agentStageReviewTimelineItemID(input.Scope.RunID, input.StageID, input.Command.RevisionID)
	var item model.AgentTimelineItem
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", itemID).Take(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	content, decodeErr := agentruntime.DecodeStageReviewResolutionContent([]byte(item.ContentJSON))
	if decodeErr != nil || item.TenantKind != input.Scope.TenantKind || item.TenantID != input.Scope.TenantID ||
		item.ThreadID != input.Scope.ThreadID || item.RunID != input.Scope.RunID ||
		item.Kind != model.AgentTimelineItemApproval || item.Status != model.AgentTimelineItemCompleted ||
		content.StageID != input.StageID || content.StageVersion != input.Command.StageVersion ||
		content.RevisionID != input.Command.RevisionID || content.Decision != input.Command.Decision ||
		content.ClientRequestID != input.Command.ClientRequestID ||
		!sameAssetPublicationIntent(content.PublicationIntent, input.Command.PublicationIntent) {
		return false, ErrProductionStageReviewConflict
	}
	var stage model.AgentProductionStage
	if err := productionStageScopeQuery(tx, input.Scope).Where("id = ?", input.StageID).First(&stage).Error; err != nil {
		return false, err
	}
	if stage.Version < content.ResultStageVersion {
		return false, ErrProductionStageReviewConflict
	}
	stage.Status = content.ResultStatus
	stage.Version = content.ResultStageVersion
	stage.ReviewRevisionID = content.ResultReviewRevisionID
	stage.LastErrorCode = ""
	stage.UpdatedAt = content.ResultUpdatedAt
	result.Stage = stage
	result.ReviewID = item.ID
	if input.CandidateSelection != nil {
		selection, err := loadExactArtifactRevisionOnceTx(
			tx, input.Scope, input.CandidateSelection.ArtifactID, input.CandidateSelection.Draft,
		)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, ErrArtifactRevisionConflict) {
				return false, ErrProductionStageReviewConflict
			}
			return false, err
		}
		result.CandidateSelectionRevision = selection
	}
	if candidate != nil {
		var stored model.AgentSpecialistRun
		if err := agentSpecialistScopeQuery(tx, input.Scope).Where("id = ?", candidate.ID).First(&stored).Error; err != nil {
			return false, err
		}
		if !sameAgentSpecialistRunFacts(stored, *candidate) {
			return false, ErrAgentSpecialistRunConflict
		}
		result.SpecialistRun = &stored
	}
	return true, nil
}

func appendOrReplayStageCandidateSelectionTx(
	tx *gorm.DB,
	scope agentruntime.Scope,
	selection StageCandidateSelectionInput,
) (*model.AgentArtifactRevision, error) {
	replayed, err := loadExactArtifactRevisionOnceTx(tx, scope, selection.ArtifactID, selection.Draft)
	if err == nil {
		return replayed, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return appendArtifactRevisionTx(tx, scope, selection.ArtifactID, 0, selection.Draft, "")
}

func appendStageReviewResolutionTx(
	tx *gorm.DB,
	scope agentruntime.Scope,
	resolvedStage model.AgentProductionStage,
	command agentruntime.StageReviewCommand,
	now time.Time,
) error {
	content := agentruntime.StageReviewResolutionContent{
		ContentType: agentruntime.StageReviewContentType, StageID: resolvedStage.ID,
		StageVersion: command.StageVersion, RevisionID: command.RevisionID, Decision: command.Decision,
		ClientRequestID: command.ClientRequestID, PublicationIntent: command.PublicationIntent,
		ResultStageVersion: resolvedStage.Version, ResultStatus: resolvedStage.Status,
		ResultReviewRevisionID: resolvedStage.ReviewRevisionID, ResultUpdatedAt: resolvedStage.UpdatedAt,
	}
	return appendStageReviewResolutionContentTx(tx, scope, content, now)
}

func sameAssetPublicationIntent(left *agentruntime.AssetPublicationIntent, right *agentruntime.AssetPublicationIntent) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func appendStageReviewResolutionContentTx(
	tx *gorm.DB,
	scope agentruntime.Scope,
	content agentruntime.StageReviewResolutionContent,
	now time.Time,
) error {
	if err := content.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(content)
	if err != nil {
		return err
	}
	sequence, err := allocateAgentEventSequence(tx, scope, now)
	if err != nil {
		return err
	}
	event := model.AgentRunEvent{
		ID: agentFactID("event", scope.RunID, strconv.FormatInt(sequence, 10)), RunID: scope.RunID,
		Sequence: sequence, Kind: agentruntime.EventApprovalDecided, PayloadJSON: string(payload), CreatedAt: now,
	}
	if err := tx.Create(&event).Error; err != nil {
		return err
	}
	nextOrdinal, err := nextAgentTimelineOrdinal(tx, scope.RunID)
	if err != nil {
		return err
	}
	return persistAgentTimelineMutation(tx, scope, TimelineMutation{
		ItemID: agentStageReviewTimelineItemID(scope.RunID, content.StageID, content.RevisionID),
		Kind:   model.AgentTimelineItemApproval, ToStatus: model.AgentTimelineItemCompleted,
		SourceEventSequence: sequence, ContentJSON: payload,
	}, &nextOrdinal, now)
}

func agentStageReviewTimelineItemID(runID string, stageID string, revisionID string) string {
	return agentFactID("timeline", runID, "stage-review", stageID, revisionID)
}

func (r *Repository) ClaimAgentSpecialistRun(scope agentruntime.Scope, specialistRunID string, taskID string, billingOrderID string, owner string, leaseDuration time.Duration) (*model.AgentSpecialistRun, *model.Task, error) {
	if err := validateProductionRepositoryScope(scope, true); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(owner) == "" || leaseDuration <= 0 {
		return nil, nil, ErrAgentSpecialistTaskLease
	}
	leaseToken, err := newTaskLeaseToken()
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	var run model.AgentSpecialistRun
	var task model.Task
	var stage model.AgentProductionStage
	err = r.db.Transaction(func(tx *gorm.DB) error {
		if err := requireActiveProductionAgentRunTx(tx, scope); err != nil {
			return err
		}
		query := agentSpecialistScopeQuery(tx, scope).Where("id = ?", specialistRunID)
		if r.Dialect() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(&run).Error; err != nil {
			return err
		}
		if run.Status != model.AgentSpecialistRunQueued || run.TaskID != "" || run.BillingOrderID != "" {
			return ErrAgentSpecialistRunConflict
		}
		taskQuery := tx.Where("id = ? AND user_id = ? AND audience = ?", taskID, scope.ActorUserID, model.TaskAudienceInternal)
		if r.Dialect() == "postgres" {
			taskQuery = taskQuery.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := taskQuery.First(&task).Error; err != nil {
			return err
		}
		if task.Status != model.TaskStatusQueued || task.BillingOrderID != billingOrderID {
			return ErrAgentSpecialistTaskLease
		}
		stageQuery := productionStageScopeQuery(tx, scope).Where("id = ?", run.StageID)
		if r.Dialect() == "postgres" {
			stageQuery = stageQuery.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := stageQuery.First(&stage).Error; err != nil {
			return err
		}
		initialClaim := stage.Status == agentruntime.StagePlanned && run.ParentSpecialistRunID == ""
		revisionClaim := stage.Status == agentruntime.StageRunning && run.ParentSpecialistRunID != ""
		if !initialClaim && !revisionClaim {
			return ErrProductionStageConflict
		}
		if revisionClaim {
			if err := requireSucceededSpecialistParentTx(tx, scope, stage.ID, run.ParentSpecialistRunID); err != nil {
				return err
			}
		}
		leaseExpiresAt := now.Add(leaseDuration)
		claimed := tx.Model(&model.Task{}).
			Where("id = ? AND status = ? AND cancel_requested_at IS NULL AND lease_generation = ? AND lease_token = ?", task.ID, model.TaskStatusQueued, task.LeaseGeneration, task.LeaseToken).
			Updates(map[string]any{
				"status": model.TaskStatusRunning, "stage": "Specialist 模型执行中", "progress": 20,
				"attempts": gorm.Expr("attempts + ?", 1), "started_at": &now, "lease_owner": owner,
				"lease_expires_at": &leaseExpiresAt, "lease_generation": gorm.Expr("lease_generation + ?", 1),
				"lease_token": leaseToken, "updated_at": now,
			})
		if claimed.Error != nil || claimed.RowsAffected != 1 {
			return ErrAgentSpecialistTaskLease
		}
		if initialClaim {
			stageUpdate := productionStageScopeQuery(tx, scope).
				Where("id = ? AND status = ? AND version = ?", stage.ID, agentruntime.StagePlanned, stage.Version).
				Select("status", "version", "updated_at").
				Updates(model.AgentProductionStage{Status: agentruntime.StageRunning, Version: stage.Version + 1, UpdatedAt: now})
			if stageUpdate.Error != nil || stageUpdate.RowsAffected != 1 {
				return ErrProductionStageConflict
			}
		}
		runUpdate := agentSpecialistScopeQuery(tx, scope).
			Where("id = ? AND status = ? AND version = ?", run.ID, model.AgentSpecialistRunQueued, run.Version).
			Select("task_id", "billing_order_id", "status", "attempt", "version", "last_heartbeat_at", "updated_at").
			Updates(model.AgentSpecialistRun{
				TaskID: task.ID, BillingOrderID: billingOrderID, Status: model.AgentSpecialistRunRunning,
				Attempt: run.Attempt + 1, Version: run.Version + 1, LastHeartbeatAt: &now, UpdatedAt: now,
			})
		if runUpdate.Error != nil || runUpdate.RowsAffected != 1 {
			return ErrAgentSpecialistRunConflict
		}
		if err := agentSpecialistScopeQuery(tx, scope).Where("id = ?", run.ID).First(&run).Error; err != nil {
			return err
		}
		return tx.First(&task, "id = ?", task.ID).Error
	})
	if err != nil {
		return nil, nil, err
	}
	return &run, &task, nil
}

func (r *Repository) CompleteAgentSpecialistRun(input CompleteAgentSpecialistRunInput) (*model.AgentSpecialistRun, []model.AgentArtifactRevision, error) {
	if err := validateProductionRepositoryScope(input.Scope, true); err != nil {
		return nil, nil, err
	}
	if input.Now.IsZero() || !validTaskLease(input.LeaseOwner, input.LeaseGeneration, input.LeaseToken) || strings.TrimSpace(input.ProviderRequestID) == "" ||
		strings.TrimSpace(input.ResultSummary) == "" || len(input.Drafts) == 0 || input.InputTokens < 0 || input.CachedTokens < 0 ||
		input.OutputTokens < 0 || input.CachedTokens > input.InputTokens {
		return nil, nil, ErrAgentSpecialistRunConflict
	}
	var completed model.AgentSpecialistRun
	revisions := make([]model.AgentArtifactRevision, 0, len(input.Drafts))
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var run model.AgentSpecialistRun
		query := agentSpecialistScopeQuery(tx, input.Scope).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", input.SpecialistRunID)
		if err := query.First(&run).Error; err != nil {
			return err
		}
		if run.Status == model.AgentSpecialistRunCancelled {
			lateRun, lateRevisions, lateErr := r.completeCancelledAgentSpecialistRunTx(tx, input, run)
			if lateErr != nil {
				return lateErr
			}
			completed = *lateRun
			revisions = lateRevisions
			return nil
		}
		if run.Status != model.AgentSpecialistRunRunning || run.TaskID == "" {
			return ErrAgentSpecialistRunConflict
		}
		var stage model.AgentProductionStage
		stageQuery := productionStageScopeQuery(tx, input.Scope).Where("id = ?", run.StageID)
		if r.Dialect() == "postgres" {
			stageQuery = stageQuery.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := stageQuery.First(&stage).Error; err != nil {
			return err
		}
		if stage.Status != agentruntime.StageRunning {
			return ErrProductionStageConflict
		}
		for _, draft := range input.Drafts {
			artifactID := productionArtifactID(input.Scope, draft.ArtifactKey)
			var artifact model.AgentArtifact
			expectedRevision := int64(0)
			err := productionArtifactScopeQuery(tx, input.Scope).Where("id = ?", artifactID).First(&artifact).Error
			if err == nil {
				expectedRevision = artifact.HeadRevision
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			revision, err := appendArtifactRevisionTx(tx, input.Scope, artifactID, expectedRevision, draft, run.ID)
			if err != nil {
				return err
			}
			revisions = append(revisions, *revision)
		}
		taskResult := tx.Model(&model.Task{}).
			Where("id = ? AND status = ? AND lease_owner = ? AND lease_generation = ? AND lease_token = ?", run.TaskID, model.TaskStatusRunning, input.LeaseOwner, input.LeaseGeneration, input.LeaseToken).
			Select("status", "stage", "progress", "provider_request_id", "result_json", "completed_at", "lease_owner", "lease_expires_at", "lease_token", "updated_at").
			Updates(model.Task{
				Status: model.TaskStatusSucceeded, Stage: "Specialist 模型任务完成", Progress: 100,
				ProviderRequestID: input.ProviderRequestID, ResultJSON: input.ResultJSON,
				CompletedAt: &input.Now, LeaseOwner: "", LeaseExpiresAt: nil, LeaseToken: "", UpdatedAt: input.Now,
			})
		if taskResult.Error != nil || taskResult.RowsAffected != 1 {
			return ErrAgentSpecialistTaskLease
		}
		runResult := agentSpecialistScopeQuery(tx, input.Scope).
			Where("id = ? AND status = ? AND version = ?", run.ID, model.AgentSpecialistRunRunning, run.Version).
			Select("status", "version", "provider_request_id", "input_tokens", "cached_tokens", "output_tokens", "result_summary", "result_json", "error_code", "completed_at", "last_heartbeat_at", "updated_at").
			Updates(model.AgentSpecialistRun{
				Status: model.AgentSpecialistRunSucceeded, Version: run.Version + 1,
				ProviderRequestID: input.ProviderRequestID, InputTokens: input.InputTokens,
				CachedTokens: input.CachedTokens, OutputTokens: input.OutputTokens,
				ResultSummary: input.ResultSummary, ResultJSON: input.ResultJSON, ErrorCode: "",
				CompletedAt: &input.Now, LastHeartbeatAt: &input.Now, UpdatedAt: input.Now,
			})
		if runResult.Error != nil || runResult.RowsAffected != 1 {
			return ErrAgentSpecialistRunConflict
		}
		stageResult := productionStageScopeQuery(tx, input.Scope).
			Where("id = ? AND status = ? AND version = ?", stage.ID, agentruntime.StageRunning, stage.Version).
			Select("status", "version", "review_revision_id", "last_error_code", "input_tokens", "cached_tokens", "output_tokens", "updated_at").
			Updates(model.AgentProductionStage{
				Status: agentruntime.StageAwaitingReview, Version: stage.Version + 1,
				ReviewRevisionID: revisions[0].ID, LastErrorCode: "", InputTokens: stage.InputTokens + input.InputTokens,
				CachedTokens: stage.CachedTokens + input.CachedTokens, OutputTokens: stage.OutputTokens + input.OutputTokens, UpdatedAt: input.Now,
			})
		if stageResult.Error != nil || stageResult.RowsAffected != 1 {
			return ErrProductionStageConflict
		}
		runUsage := tx.Exec(`
			UPDATE agent_runs
			   SET specialist_input_tokens = specialist_input_tokens + ?, specialist_cached_tokens = specialist_cached_tokens + ?,
			       specialist_output_tokens = specialist_output_tokens + ?, updated_at = ?
			 WHERE id = ? AND thread_id = ? AND actor_user_id = ? AND status IN (?, ?)`,
			input.InputTokens, input.CachedTokens, input.OutputTokens, input.Now,
			input.Scope.RunID, input.Scope.ThreadID, input.Scope.ActorUserID,
			agentruntime.RunRunning, agentruntime.RunWaitingTool)
		if runUsage.Error != nil || runUsage.RowsAffected != 1 {
			return ErrAgentSpecialistRunConflict
		}
		if err := appendSpecialistArtifactReviewTx(tx, input.Scope, stage, revisions[0], input.ResultSummary, input.Now); err != nil {
			return err
		}
		return agentSpecialistScopeQuery(tx, input.Scope).Where("id = ?", run.ID).First(&completed).Error
	})
	if err != nil {
		return nil, nil, err
	}
	return &completed, revisions, nil
}

func appendSpecialistArtifactReviewTx(
	tx *gorm.DB,
	scope agentruntime.Scope,
	stage model.AgentProductionStage,
	revision model.AgentArtifactRevision,
	summary string,
	now time.Time,
) error {
	content := agentruntime.ArtifactReviewContent{
		ContentType: agentruntime.ArtifactReviewContentType,
		StageID:     stage.ID, StageVersion: stage.Version + 1,
		ArtifactID: revision.ArtifactID, RevisionID: revision.ID,
		ArtifactSchema: revision.Kind + ".v" + strconv.Itoa(revision.SchemaVersion),
		Summary:        summary,
	}
	if err := content.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(content)
	if err != nil {
		return err
	}
	sequence, err := allocateAgentEventSequence(tx, scope, now)
	if err != nil {
		return err
	}
	event := model.AgentRunEvent{
		ID:    agentFactID("event", scope.RunID, strconv.FormatInt(sequence, 10)),
		RunID: scope.RunID, Sequence: sequence, Kind: agentruntime.EventArtifactAvailable,
		PayloadJSON: string(payload), CreatedAt: now,
	}
	if err := tx.Create(&event).Error; err != nil {
		return err
	}
	nextOrdinal, err := nextAgentTimelineOrdinal(tx, scope.RunID)
	if err != nil {
		return err
	}
	return persistAgentTimelineMutation(tx, scope, TimelineMutation{
		ItemID: agentArtifactReviewTimelineItemID(scope.RunID, content),
		Kind:   model.AgentTimelineItemArtifact, ToStatus: model.AgentTimelineItemCompleted,
		SourceEventSequence: sequence, ContentJSON: payload,
	}, &nextOrdinal, now)
}

func agentArtifactReviewTimelineItemID(runID string, content agentruntime.ArtifactReviewContent) string {
	return agentFactID("timeline", runID, "artifact-review", content.StageID, content.RevisionID)
}

func (r *Repository) completeCancelledAgentSpecialistRunTx(
	tx *gorm.DB,
	input CompleteAgentSpecialistRunInput,
	run model.AgentSpecialistRun,
) (*model.AgentSpecialistRun, []model.AgentArtifactRevision, error) {
	if run.TaskID == "" {
		return nil, nil, ErrAgentSpecialistRunConflict
	}
	if run.ProviderRequestID != "" {
		if run.ProviderRequestID != input.ProviderRequestID || run.ResultJSON != input.ResultJSON || run.ResultSummary != input.ResultSummary ||
			run.InputTokens != input.InputTokens || run.CachedTokens != input.CachedTokens || run.OutputTokens != input.OutputTokens {
			return nil, nil, ErrAgentSpecialistRunConflict
		}
		var stored []model.AgentArtifactRevision
		if err := productionArtifactRevisionScopeQuery(tx, input.Scope).
			Where("created_by_specialist_id = ? AND model_request_identity = ? AND lifecycle_status = ?", run.ID, input.ProviderRequestID, model.AgentArtifactRevisionUnadopted).
			Order("artifact_id ASC, revision ASC").Find(&stored).Error; err != nil {
			return nil, nil, err
		}
		if len(stored) != len(input.Drafts) {
			return nil, nil, ErrAgentSpecialistRunConflict
		}
		matches, err := lateSpecialistRevisionsMatchDrafts(input.Scope, run.ID, stored, input.Drafts)
		if err != nil {
			return nil, nil, err
		}
		if !matches {
			return nil, nil, ErrAgentSpecialistRunConflict
		}
		return &run, stored, nil
	}

	revisions := make([]model.AgentArtifactRevision, 0, len(input.Drafts))
	for _, draft := range input.Drafts {
		artifactID := productionArtifactID(input.Scope, draft.ArtifactKey)
		revision, err := appendUnadoptedArtifactRevisionTx(tx, input.Scope, artifactID, draft, run.ID, input.Now)
		if err != nil {
			return nil, nil, err
		}
		revisions = append(revisions, *revision)
	}
	taskResult := tx.Model(&model.Task{}).
		Where("id = ? AND user_id = ? AND status = ? AND cancel_requested_at IS NOT NULL AND lease_owner = ? AND lease_generation = ? AND lease_token = ?", run.TaskID, input.Scope.ActorUserID, model.TaskStatusCancelled, input.LeaseOwner, input.LeaseGeneration, input.LeaseToken).
		Select("stage", "provider_request_id", "result_json", "poll_stage", "next_poll_at", "lease_owner", "lease_expires_at", "lease_token", "updated_at").
		Updates(model.Task{
			Stage: "迟到供应商结果已保存为未采纳事实", ProviderRequestID: input.ProviderRequestID,
			ResultJSON: input.ResultJSON, PollStage: "cancel_reconciled", NextPollAt: nil,
			LeaseOwner: "", LeaseExpiresAt: nil, LeaseToken: "", UpdatedAt: input.Now,
		})
	if taskResult.Error != nil {
		return nil, nil, taskResult.Error
	}
	if taskResult.RowsAffected != 1 {
		return nil, nil, ErrAgentSpecialistTaskLease
	}
	runResult := agentSpecialistScopeQuery(tx, input.Scope).
		Where("id = ? AND status = ? AND version = ?", run.ID, model.AgentSpecialistRunCancelled, run.Version).
		Select("version", "provider_request_id", "input_tokens", "cached_tokens", "output_tokens", "result_summary", "result_json", "last_heartbeat_at", "updated_at").
		Updates(model.AgentSpecialistRun{
			Version: run.Version + 1, ProviderRequestID: input.ProviderRequestID,
			InputTokens: input.InputTokens, CachedTokens: input.CachedTokens, OutputTokens: input.OutputTokens,
			ResultSummary: input.ResultSummary, ResultJSON: input.ResultJSON, LastHeartbeatAt: &input.Now, UpdatedAt: input.Now,
		})
	if runResult.Error != nil {
		return nil, nil, runResult.Error
	}
	if runResult.RowsAffected != 1 {
		return nil, nil, ErrAgentSpecialistRunConflict
	}
	stageResult := tx.Exec(`
		UPDATE agent_production_stages
		   SET input_tokens = input_tokens + ?, cached_tokens = cached_tokens + ?, output_tokens = output_tokens + ?, updated_at = ?
		 WHERE id = ? AND tenant_kind = ? AND tenant_id = ? AND actor_user_id = ? AND domain_project_id = ?
		   AND canvas_id = ? AND thread_id = ? AND run_id = ? AND status = ?`,
		input.InputTokens, input.CachedTokens, input.OutputTokens, input.Now,
		run.StageID, input.Scope.TenantKind, input.Scope.TenantID, input.Scope.ActorUserID, input.Scope.DomainProjectID,
		input.Scope.CanvasID, input.Scope.ThreadID, input.Scope.RunID, agentruntime.StageStopped)
	if stageResult.Error != nil {
		return nil, nil, stageResult.Error
	}
	if stageResult.RowsAffected != 1 {
		return nil, nil, ErrProductionStageConflict
	}
	runUsage := tx.Exec(`
		UPDATE agent_runs
		   SET specialist_input_tokens = specialist_input_tokens + ?, specialist_cached_tokens = specialist_cached_tokens + ?,
		       specialist_output_tokens = specialist_output_tokens + ?, updated_at = ?
		 WHERE id = ? AND thread_id = ? AND actor_user_id = ? AND status = ?`,
		input.InputTokens, input.CachedTokens, input.OutputTokens, input.Now,
		input.Scope.RunID, input.Scope.ThreadID, input.Scope.ActorUserID, agentruntime.RunCancelled)
	if runUsage.Error != nil {
		return nil, nil, runUsage.Error
	}
	if runUsage.RowsAffected != 1 {
		return nil, nil, ErrAgentSpecialistRunConflict
	}
	var completed model.AgentSpecialistRun
	if err := agentSpecialistScopeQuery(tx, input.Scope).Where("id = ?", run.ID).First(&completed).Error; err != nil {
		return nil, nil, err
	}
	return &completed, revisions, nil
}

func lateSpecialistRevisionsMatchDrafts(
	scope agentruntime.Scope,
	specialistRunID string,
	stored []model.AgentArtifactRevision,
	drafts []agentruntime.ArtifactDraft,
) (bool, error) {
	storedByArtifactID := make(map[string]model.AgentArtifactRevision, len(stored))
	for _, revision := range stored {
		if _, duplicated := storedByArtifactID[revision.ArtifactID]; duplicated {
			return false, nil
		}
		storedByArtifactID[revision.ArtifactID] = revision
	}
	seenArtifacts := make(map[string]struct{}, len(drafts))
	for _, draft := range drafts {
		if err := agentruntime.ValidateArtifactDraft(draft); err != nil {
			return false, err
		}
		if draft.UpstreamRevisions == nil {
			draft.UpstreamRevisions = []agentruntime.ArtifactRevisionRef{}
		}
		if draft.SkillVersions == nil {
			draft.SkillVersions = []agentruntime.SkillSelection{}
		}
		artifactID := productionArtifactID(scope, draft.ArtifactKey)
		if _, duplicated := seenArtifacts[artifactID]; duplicated {
			return false, nil
		}
		seenArtifacts[artifactID] = struct{}{}
		revision, found := storedByArtifactID[artifactID]
		if !found {
			return false, nil
		}
		upstreamJSON, err := json.Marshal(draft.UpstreamRevisions)
		if err != nil {
			return false, err
		}
		skillsJSON, err := json.Marshal(draft.SkillVersions)
		if err != nil {
			return false, err
		}
		if revision.ArtifactKey != draft.ArtifactKey || revision.Kind != draft.Kind || revision.SchemaVersion != draft.SchemaVersion ||
			revision.PayloadJSON != string(draft.Payload) || revision.ResourceID != draft.ResourceID ||
			revision.UpstreamRevisionsJSON != string(upstreamJSON) || revision.ModelRequestIdentity != draft.ModelRequestIdentity ||
			revision.SkillVersionsJSON != string(skillsJSON) || revision.CreatedBySpecialistID != specialistRunID ||
			revision.LifecycleStatus != model.AgentArtifactRevisionUnadopted {
			return false, nil
		}
	}
	return len(seenArtifacts) == len(storedByArtifactID), nil
}

func (r *Repository) FailAgentSpecialistRun(input FailAgentSpecialistRunInput) (*model.AgentSpecialistRun, error) {
	if err := validateProductionRepositoryScope(input.Scope, true); err != nil {
		return nil, err
	}
	if input.Now.IsZero() || !validTaskLease(input.LeaseOwner, input.LeaseGeneration, input.LeaseToken) || strings.TrimSpace(input.ErrorCode) == "" || strings.TrimSpace(input.ErrorText) == "" {
		return nil, ErrAgentSpecialistRunConflict
	}
	var failed model.AgentSpecialistRun
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var run model.AgentSpecialistRun
		runQuery := agentSpecialistScopeQuery(tx, input.Scope).Where("id = ?", input.SpecialistRunID)
		if r.Dialect() == "postgres" {
			runQuery = runQuery.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := runQuery.First(&run).Error; err != nil {
			return err
		}
		if run.Status != model.AgentSpecialistRunRunning || run.TaskID == "" {
			return ErrAgentSpecialistRunConflict
		}
		var stage model.AgentProductionStage
		stageQuery := productionStageScopeQuery(tx, input.Scope).Where("id = ?", run.StageID)
		if r.Dialect() == "postgres" {
			stageQuery = stageQuery.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := stageQuery.First(&stage).Error; err != nil {
			return err
		}
		if stage.Status != agentruntime.StageRunning {
			return ErrProductionStageConflict
		}
		if err := r.finalizeFailedBillingTx(tx, run.BillingOrderID, run.TaskID, "", input.BillingAction, input.ErrorText); err != nil {
			return err
		}
		taskResult := tx.Model(&model.Task{}).
			Where("id = ? AND status = ? AND lease_owner = ? AND lease_generation = ? AND lease_token = ?", run.TaskID, model.TaskStatusRunning, input.LeaseOwner, input.LeaseGeneration, input.LeaseToken).
			Select("status", "stage", "progress", "error", "completed_at", "lease_owner", "lease_expires_at", "lease_token", "updated_at").
			Updates(model.Task{
				Status: model.TaskStatusFailed, Stage: "Specialist 模型任务失败", Progress: 100,
				Error: input.ErrorText, CompletedAt: &input.Now, LeaseOwner: "", LeaseExpiresAt: nil, LeaseToken: "", UpdatedAt: input.Now,
			})
		if taskResult.Error != nil || taskResult.RowsAffected != 1 {
			return ErrAgentSpecialistTaskLease
		}
		runResult := agentSpecialistScopeQuery(tx, input.Scope).
			Where("id = ? AND status = ? AND version = ?", run.ID, model.AgentSpecialistRunRunning, run.Version).
			Select("status", "version", "error_code", "completed_at", "last_heartbeat_at", "updated_at").
			Updates(model.AgentSpecialistRun{
				Status: model.AgentSpecialistRunFailed, Version: run.Version + 1, ErrorCode: input.ErrorCode,
				CompletedAt: &input.Now, LastHeartbeatAt: &input.Now, UpdatedAt: input.Now,
			})
		if runResult.Error != nil || runResult.RowsAffected != 1 {
			return ErrAgentSpecialistRunConflict
		}
		stageResult := productionStageScopeQuery(tx, input.Scope).
			Where("id = ? AND status = ? AND version = ?", stage.ID, agentruntime.StageRunning, stage.Version).
			Select("status", "version", "last_error_code", "updated_at").
			Updates(model.AgentProductionStage{
				Status: agentruntime.StageFailed, Version: stage.Version + 1, LastErrorCode: input.ErrorCode, UpdatedAt: input.Now,
			})
		if stageResult.Error != nil || stageResult.RowsAffected != 1 {
			return ErrProductionStageConflict
		}
		return agentSpecialistScopeQuery(tx, input.Scope).Where("id = ?", run.ID).First(&failed).Error
	})
	if err != nil {
		return nil, err
	}
	return &failed, nil
}

func sameAgentSpecialistRunFacts(stored model.AgentSpecialistRun, candidate model.AgentSpecialistRun) bool {
	return stored.ID == candidate.ID && stored.TenantKind == candidate.TenantKind && stored.TenantID == candidate.TenantID &&
		stored.ActorUserID == candidate.ActorUserID && stored.DomainProjectID == candidate.DomainProjectID && stored.CanvasID == candidate.CanvasID &&
		stored.ThreadID == candidate.ThreadID && stored.RunID == candidate.RunID && stored.StageID == candidate.StageID &&
		stored.ParentSpecialistRunID == candidate.ParentSpecialistRunID &&
		stored.SpecialistKey == candidate.SpecialistKey && stored.SpecialistVersion == candidate.SpecialistVersion && stored.Objective == candidate.Objective &&
		stored.ModelRecordID == candidate.ModelRecordID && stored.ModelKey == candidate.ModelKey && stored.ToolSchemaVersion == candidate.ToolSchemaVersion &&
		stored.InputRevisionsJSON == candidate.InputRevisionsJSON && stored.SkillVersionsJSON == candidate.SkillVersionsJSON &&
		stored.ToolAllowlistJSON == candidate.ToolAllowlistJSON && stored.ExpectedOutputSchema == candidate.ExpectedOutputSchema &&
		stored.ExpectedDeliveryJSON == candidate.ExpectedDeliveryJSON
}

func newAgentSpecialistRunCandidate(
	scope agentruntime.Scope,
	request agentruntime.SpecialistRequest,
	toolSchemaVersion int,
	now time.Time,
) (model.AgentSpecialistRun, error) {
	inputsJSON, err := json.Marshal(request.InputRevisions)
	if err != nil {
		return model.AgentSpecialistRun{}, err
	}
	skillsJSON, err := json.Marshal(request.LoadedSkills)
	if err != nil {
		return model.AgentSpecialistRun{}, err
	}
	toolsJSON, err := json.Marshal(request.ToolAllowlist)
	if err != nil {
		return model.AgentSpecialistRun{}, err
	}
	deliveryJSON, err := json.Marshal(request.ExpectedDelivery)
	if err != nil {
		return model.AgentSpecialistRun{}, err
	}
	return model.AgentSpecialistRun{
		ID: request.SpecialistRunID, TenantKind: scope.TenantKind, TenantID: scope.TenantID,
		ActorUserID: scope.ActorUserID, DomainProjectID: scope.DomainProjectID,
		CanvasID: scope.CanvasID, ThreadID: scope.ThreadID, RunID: scope.RunID,
		StageID: request.StageID, ParentSpecialistRunID: request.ParentSpecialistRunID,
		SpecialistKey: request.SpecialistKey, SpecialistVersion: request.SpecialistVersion,
		Objective: request.Objective, ModelRecordID: request.ParentModelRecordID, ModelKey: request.ParentModelKey,
		ToolSchemaVersion: toolSchemaVersion, InputRevisionsJSON: string(inputsJSON), SkillVersionsJSON: string(skillsJSON),
		ToolAllowlistJSON: string(toolsJSON), ExpectedOutputSchema: request.ExpectedOutputSchema,
		ExpectedDeliveryJSON: string(deliveryJSON), Status: model.AgentSpecialistRunQueued, Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

func requireSucceededSpecialistParentTx(
	tx *gorm.DB,
	scope agentruntime.Scope,
	stageID string,
	parentSpecialistRunID string,
) error {
	var parent model.AgentSpecialistRun
	if err := agentSpecialistScopeQuery(tx, scope).
		Where("id = ? AND stage_id = ?", parentSpecialistRunID, stageID).
		First(&parent).Error; err != nil {
		return ErrAgentSpecialistRunConflict
	}
	if parent.Status != model.AgentSpecialistRunSucceeded {
		return ErrAgentSpecialistRunConflict
	}
	return nil
}

func agentSpecialistScopeQuery(query *gorm.DB, scope agentruntime.Scope) *gorm.DB {
	return query.Model(&model.AgentSpecialistRun{}).Where(
		"agent_specialist_runs.tenant_kind = ? AND agent_specialist_runs.tenant_id = ? AND agent_specialist_runs.actor_user_id = ? AND agent_specialist_runs.domain_project_id = ? AND agent_specialist_runs.canvas_id = ? AND agent_specialist_runs.thread_id = ? AND agent_specialist_runs.run_id = ?",
		scope.TenantKind, scope.TenantID, scope.ActorUserID, scope.DomainProjectID, scope.CanvasID, scope.ThreadID, scope.RunID,
	)
}

func productionArtifactID(scope agentruntime.Scope, artifactKey string) string {
	return agentFactID("production-artifact", string(scope.TenantKind), scope.TenantID, scope.ActorUserID, scope.DomainProjectID, scope.CanvasID, scope.ThreadID, scope.RunID, artifactKey)
}
