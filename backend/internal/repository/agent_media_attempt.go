package repository

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrMediaAttemptFenceConflict = errors.New("media attempt completion fence conflict")

type MediaAttemptWriteDisposition string

const (
	MediaAttemptWriteAdopted   MediaAttemptWriteDisposition = "adopted"
	MediaAttemptWriteUnadopted MediaAttemptWriteDisposition = "unadopted"
)

type MediaAttemptCompletionFence struct {
	ToolCallID                 string
	ActionVersion              int
	ExpectedTaskID             string
	ExpectedAttempt            int
	ExpectedArtifactRevisionID string
	ApprovalFingerprint        string
}

type MediaCandidateAttemptInput struct {
	ArtifactID string
	Draft      agentruntime.ArtifactDraft
}

type MediaCandidateAppendResult struct {
	Revision    model.AgentArtifactRevision
	Disposition MediaAttemptWriteDisposition
}

type ProductionMediaAttemptCompletion struct {
	Fence          MediaAttemptCompletionFence
	ArtifactID     string
	ExpectedStatus model.AgentProductionArtifactStatus
	BillingOrderID string
	ResourceID     string
	LateArtifactID string
	LateDraft      agentruntime.ArtifactDraft
	Recovery       bool
	Now            time.Time
}

type ProductionMediaCompletionResult struct {
	Artifact     model.AgentProductionArtifact
	LateRevision *model.AgentArtifactRevision
	Disposition  MediaAttemptWriteDisposition
}

type ProductionMediaTransitionResult struct {
	Artifact    model.AgentProductionArtifact
	Disposition MediaAttemptWriteDisposition
}

// BeginAgentProductionMediaRecovery claims a paid provider result whose
// original tool call failed only while materializing that result locally.
// Terminal runs and superseded plans never regain ownership.
func (r *Repository) BeginAgentProductionMediaRecovery(
	scope agentruntime.Scope,
	fence MediaAttemptCompletionFence,
	artifactID string,
	billingOrderID string,
	now time.Time,
) (*ProductionMediaTransitionResult, error) {
	if err := validateMediaAttemptFence(scope, fence, now); err != nil {
		return nil, err
	}
	if artifactID != fence.ExpectedArtifactRevisionID || strings.TrimSpace(billingOrderID) == "" {
		return nil, ErrMediaAttemptFenceConflict
	}
	var recovered ProductionMediaTransitionResult
	err := r.db.Transaction(func(tx *gorm.DB) error {
		eligible, err := mediaAttemptRecoveryEligibleTx(tx, scope, fence)
		if err != nil {
			return err
		}
		var plan model.AgentProductionPlanVersion
		var artifact model.AgentProductionArtifact
		if err := loadProductionArtifactAttemptTx(tx, scope, artifactID, &artifact, &plan); err != nil {
			return err
		}
		if artifact.Status == model.AgentProductionArtifactQueued &&
			artifact.LastErrorCode == "production_result_materializing" &&
			artifact.Attempt == fence.ExpectedAttempt && artifact.TaskID == fence.ExpectedTaskID &&
			artifact.BillingOrderID == billingOrderID {
			disposition := MediaAttemptWriteAdopted
			if !eligible || plan.Status != model.AgentProductionPlanActive {
				disposition = MediaAttemptWriteUnadopted
			}
			recovered = ProductionMediaTransitionResult{Artifact: artifact, Disposition: disposition}
			return nil
		}
		if !eligible || plan.Status != model.AgentProductionPlanActive {
			recovered = ProductionMediaTransitionResult{Artifact: artifact, Disposition: MediaAttemptWriteUnadopted}
			return nil
		}
		if artifact.Status != model.AgentProductionArtifactFailed || artifact.LastErrorCode != "production_result_invalid" ||
			artifact.Attempt != fence.ExpectedAttempt || artifact.TaskID != fence.ExpectedTaskID || artifact.BillingOrderID != billingOrderID {
			return ErrMediaAttemptFenceConflict
		}
		result := tx.Model(&model.AgentProductionArtifact{}).
			Where(`id = ? AND status = ? AND attempt = ? AND task_id = ? AND billing_order_id = ? AND last_error_code = ?`,
				artifact.ID, model.AgentProductionArtifactFailed, fence.ExpectedAttempt, fence.ExpectedTaskID,
				billingOrderID, "production_result_invalid").
			Select("status", "last_error_code", "updated_at").
			Updates(agentProductionArtifactUpdate{
				Status: model.AgentProductionArtifactQueued, Attempt: artifact.Attempt,
				TaskID: artifact.TaskID, BillingOrderID: artifact.BillingOrderID,
				ResourceID: artifact.ResourceID, LastErrorCode: "production_result_materializing", UpdatedAt: now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrMediaAttemptFenceConflict
		}
		if err := tx.First(&artifact, "id = ?", artifact.ID).Error; err != nil {
			return err
		}
		recovered = ProductionMediaTransitionResult{Artifact: artifact, Disposition: MediaAttemptWriteAdopted}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &recovered, nil
}

// TransitionAgentProductionMediaAttempt applies non-terminal production
// progress only while the exact approved tool attempt still owns the run.
func (r *Repository) TransitionAgentProductionMediaAttempt(
	scope agentruntime.Scope,
	fence MediaAttemptCompletionFence,
	input ArtifactTransition,
) (*ProductionMediaTransitionResult, error) {
	if err := validateMediaAttemptFence(scope, fence, input.Now); err != nil {
		return nil, err
	}
	if err := validateArtifactTransition(scope, input); err != nil {
		return nil, err
	}
	if input.ArtifactID != fence.ExpectedArtifactRevisionID || input.NextStatus == model.AgentProductionArtifactSucceeded {
		return nil, ErrMediaAttemptFenceConflict
	}
	var transitioned ProductionMediaTransitionResult
	err := r.db.Transaction(func(tx *gorm.DB) error {
		currentAttempt, err := mediaAttemptCurrentTx(
			tx,
			scope,
			fence,
			agentruntime.ToolMediaGenerate,
			agentruntime.ToolProductionRender,
		)
		if err != nil {
			return err
		}
		var plan model.AgentProductionPlanVersion
		var artifact model.AgentProductionArtifact
		if err := loadProductionArtifactAttemptTx(tx, scope, input.ArtifactID, &artifact, &plan); err != nil {
			return err
		}
		if productionTransitionAlreadyApplied(artifact, fence, input) {
			disposition := MediaAttemptWriteAdopted
			if !currentAttempt || plan.Status != model.AgentProductionPlanActive {
				disposition = MediaAttemptWriteUnadopted
			}
			transitioned = ProductionMediaTransitionResult{Artifact: artifact, Disposition: disposition}
			return nil
		}
		if !currentAttempt || plan.Status != model.AgentProductionPlanActive {
			transitioned = ProductionMediaTransitionResult{Artifact: artifact, Disposition: MediaAttemptWriteUnadopted}
			return nil
		}
		if artifact.Status != input.ExpectedStatus || artifact.Attempt != input.ExpectedAttempt ||
			(input.NextAttempt != fence.ExpectedAttempt && input.ExpectedAttempt != fence.ExpectedAttempt) {
			return ErrAgentProductionArtifactConflict
		}
		if artifact.TaskID != "" && artifact.TaskID != fence.ExpectedTaskID {
			return ErrMediaAttemptFenceConflict
		}
		if input.TaskID != "" && input.TaskID != fence.ExpectedTaskID {
			return ErrMediaAttemptFenceConflict
		}
		if err := validateArtifactFactBindings(artifact, input); err != nil {
			return err
		}
		updates := agentProductionArtifactUpdate{
			Status: input.NextStatus, Attempt: input.NextAttempt,
			TaskID:         keepProductionFact(artifact.TaskID, input.TaskID),
			BillingOrderID: keepProductionFact(artifact.BillingOrderID, input.BillingOrderID),
			ResourceID:     keepProductionFact(artifact.ResourceID, input.ResourceID),
			LastErrorCode:  strings.TrimSpace(input.LastErrorCode), UpdatedAt: input.Now,
		}
		result := tx.Model(&model.AgentProductionArtifact{}).
			Where("id = ? AND status = ? AND attempt = ?", artifact.ID, input.ExpectedStatus, input.ExpectedAttempt).
			Select("status", "attempt", "task_id", "billing_order_id", "resource_id", "last_error_code", "updated_at").
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrAgentProductionArtifactConflict
		}
		if err := tx.First(&artifact, "id = ?", artifact.ID).Error; err != nil {
			return err
		}
		transitioned = ProductionMediaTransitionResult{Artifact: artifact, Disposition: MediaAttemptWriteAdopted}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &transitioned, nil
}

func (r *Repository) AppendMediaCandidateRevisionForAttempt(
	scope agentruntime.Scope,
	artifactID string,
	draft agentruntime.ArtifactDraft,
	fence MediaAttemptCompletionFence,
	now time.Time,
) (*MediaCandidateAppendResult, error) {
	results, err := r.AppendMediaCandidateRevisionsForAttempt(scope, []MediaCandidateAttemptInput{{
		ArtifactID: artifactID,
		Draft:      draft,
	}}, fence, now)
	if err != nil {
		return nil, err
	}
	return &results[0], nil
}

// AppendMediaCandidateRevisionsForAttempt atomically decides whether every
// provider result still belongs to the running tool attempt. A stale result is
// retained as immutable evidence without advancing an Artifact head.
func (r *Repository) AppendMediaCandidateRevisionsForAttempt(
	scope agentruntime.Scope,
	inputs []MediaCandidateAttemptInput,
	fence MediaAttemptCompletionFence,
	now time.Time,
) ([]MediaCandidateAppendResult, error) {
	if err := validateMediaAttemptFence(scope, fence, now); err != nil {
		return nil, err
	}
	if len(inputs) == 0 {
		return nil, ErrMediaCandidateInvalid
	}
	for _, input := range inputs {
		if err := validateMediaCandidateAttemptInput(input, fence.ExpectedTaskID); err != nil {
			return nil, err
		}
	}

	var results []MediaCandidateAppendResult
	err := r.db.Transaction(func(tx *gorm.DB) error {
		current, err := mediaAttemptCurrentTx(tx, scope, fence, agentruntime.ToolMediaGenerate)
		if err != nil {
			return err
		}
		replayed, found, err := loadExactMediaCandidateBatchTx(tx, scope, inputs)
		if err != nil {
			return err
		}
		if found {
			results = replayed
			return nil
		}
		results = make([]MediaCandidateAppendResult, 0, len(inputs))
		for _, input := range inputs {
			var revision *model.AgentArtifactRevision
			if current {
				revision, err = appendArtifactRevisionTx(tx, scope, input.ArtifactID, 0, input.Draft, "")
			} else {
				revision, err = appendUnadoptedArtifactRevisionTx(tx, scope, input.ArtifactID, input.Draft, "", now)
			}
			if err != nil {
				return err
			}
			disposition := MediaAttemptWriteAdopted
			if !current {
				disposition = MediaAttemptWriteUnadopted
			}
			results = append(results, MediaCandidateAppendResult{Revision: *revision, Disposition: disposition})
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrArtifactRevisionConflict) {
			replayed, found, replayErr := loadExactMediaCandidateBatchTx(r.db, scope, inputs)
			if replayErr != nil {
				return nil, replayErr
			}
			if found {
				return replayed, nil
			}
		}
		return nil, err
	}
	return results, nil
}

func (r *Repository) CompleteAgentProductionMediaAttempt(
	scope agentruntime.Scope,
	input ProductionMediaAttemptCompletion,
) (*ProductionMediaCompletionResult, error) {
	if err := validateProductionMediaAttemptCompletion(scope, input); err != nil {
		return nil, err
	}
	var completed ProductionMediaCompletionResult
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var currentAttempt bool
		var err error
		if input.Recovery {
			currentAttempt, err = mediaAttemptRecoveryEligibleTx(tx, scope, input.Fence)
		} else {
			currentAttempt, err = mediaAttemptCurrentTx(tx, scope, input.Fence, agentruntime.ToolMediaGenerate, agentruntime.ToolProductionRender)
		}
		if err != nil {
			return err
		}

		var plan model.AgentProductionPlanVersion
		var artifact model.AgentProductionArtifact
		if err := loadProductionArtifactAttemptTx(tx, scope, input.ArtifactID, &artifact, &plan); err != nil {
			return err
		}
		terminal := artifact.Status == model.AgentProductionArtifactSucceeded || artifact.Status == model.AgentProductionArtifactCommitted
		if terminal &&
			artifact.Attempt == input.Fence.ExpectedAttempt && artifact.TaskID == input.Fence.ExpectedTaskID &&
			artifact.BillingOrderID == input.BillingOrderID && artifact.ResourceID == input.ResourceID {
			completed = ProductionMediaCompletionResult{Artifact: artifact, Disposition: MediaAttemptWriteAdopted}
			return nil
		}

		canAdopt := !terminal && currentAttempt && plan.Status == model.AgentProductionPlanActive &&
			artifact.Status == input.ExpectedStatus && artifact.Attempt == input.Fence.ExpectedAttempt &&
			artifact.TaskID == input.Fence.ExpectedTaskID && artifact.BillingOrderID == input.BillingOrderID
		if input.Recovery {
			canAdopt = canAdopt && artifact.LastErrorCode == "production_result_materializing"
		}
		if !canAdopt {
			revision, replayErr := loadExactArtifactRevisionAnyTx(tx, scope, input.LateArtifactID, input.LateDraft)
			if replayErr == nil {
				if revision.LifecycleStatus != model.AgentArtifactRevisionUnadopted {
					return ErrMediaAttemptFenceConflict
				}
				completed = ProductionMediaCompletionResult{Artifact: artifact, LateRevision: revision, Disposition: MediaAttemptWriteUnadopted}
				return nil
			}
			if !errors.Is(replayErr, gorm.ErrRecordNotFound) {
				return replayErr
			}
			revision, appendErr := appendUnadoptedArtifactRevisionTx(tx, scope, input.LateArtifactID, input.LateDraft, "", input.Now)
			if appendErr != nil {
				return appendErr
			}
			completed = ProductionMediaCompletionResult{Artifact: artifact, LateRevision: revision, Disposition: MediaAttemptWriteUnadopted}
			return nil
		}

		result := tx.Model(&model.AgentProductionArtifact{}).
			Where(`id = ? AND status = ? AND attempt = ? AND task_id = ? AND billing_order_id = ?`,
				artifact.ID, input.ExpectedStatus, input.Fence.ExpectedAttempt, input.Fence.ExpectedTaskID, input.BillingOrderID).
			Select("status", "resource_id", "last_error_code", "updated_at").
			Updates(agentProductionArtifactUpdate{
				Status: model.AgentProductionArtifactSucceeded, Attempt: artifact.Attempt,
				TaskID: artifact.TaskID, BillingOrderID: artifact.BillingOrderID,
				ResourceID: input.ResourceID, LastErrorCode: "", UpdatedAt: input.Now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrMediaAttemptFenceConflict
		}
		if err := tx.First(&artifact, "id = ?", artifact.ID).Error; err != nil {
			return err
		}
		if err := appendAgentProductionArtifactTimeline(tx, scope, artifact, input.Now); err != nil {
			return err
		}
		completed = ProductionMediaCompletionResult{Artifact: artifact, Disposition: MediaAttemptWriteAdopted}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &completed, nil
}

func loadProductionArtifactAttemptTx(
	tx *gorm.DB,
	scope agentruntime.Scope,
	artifactID string,
	artifact *model.AgentProductionArtifact,
	plan *model.AgentProductionPlanVersion,
) error {
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Table("agent_production_artifacts").Select("agent_production_artifacts.*").
		Joins("JOIN agent_production_plan_versions ON agent_production_plan_versions.id = agent_production_artifacts.plan_version_id").
		Where(`agent_production_artifacts.id = ?
			AND agent_production_plan_versions.tenant_kind = ?
			AND agent_production_plan_versions.tenant_id = ?
			AND agent_production_plan_versions.domain_project_id = ?
			AND agent_production_plan_versions.canvas_id = ?`,
			artifactID, scope.TenantKind, scope.TenantID, scope.DomainProjectID, scope.CanvasID).
		Take(artifact).Error; err != nil {
		return err
	}
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(plan, "id = ?", artifact.PlanVersionID).Error
}

func productionTransitionAlreadyApplied(
	artifact model.AgentProductionArtifact,
	fence MediaAttemptCompletionFence,
	input ArtifactTransition,
) bool {
	if artifact.Status != input.NextStatus || artifact.Attempt != input.NextAttempt || artifact.TaskID != fence.ExpectedTaskID {
		return false
	}
	return (input.BillingOrderID == "" || artifact.BillingOrderID == input.BillingOrderID) &&
		(input.ResourceID == "" || artifact.ResourceID == input.ResourceID) &&
		artifact.LastErrorCode == strings.TrimSpace(input.LastErrorCode)
}

func validateMediaAttemptFence(scope agentruntime.Scope, fence MediaAttemptCompletionFence, now time.Time) error {
	if err := validateProductionRepositoryScope(scope, true); err != nil {
		return err
	}
	if strings.TrimSpace(fence.ToolCallID) != fence.ToolCallID || fence.ToolCallID == "" || len(fence.ToolCallID) > 120 ||
		fence.ActionVersion < 1 || strings.TrimSpace(fence.ExpectedTaskID) != fence.ExpectedTaskID || fence.ExpectedTaskID == "" || len(fence.ExpectedTaskID) > 80 ||
		fence.ExpectedAttempt < 1 || strings.TrimSpace(fence.ExpectedArtifactRevisionID) != fence.ExpectedArtifactRevisionID ||
		fence.ExpectedArtifactRevisionID == "" || len(fence.ExpectedArtifactRevisionID) > 120 ||
		strings.TrimSpace(fence.ApprovalFingerprint) != fence.ApprovalFingerprint || fence.ApprovalFingerprint == "" || len(fence.ApprovalFingerprint) > 128 || now.IsZero() {
		return ErrMediaAttemptFenceConflict
	}
	return nil
}

func validateMediaCandidateAttemptInput(input MediaCandidateAttemptInput, expectedTaskID string) error {
	if agentruntime.ValidateArtifactDraft(input.Draft) != nil ||
		input.Draft.Kind != mediaCandidateArtifactKind || input.Draft.SchemaVersion != 1 ||
		strings.TrimSpace(input.Draft.ResourceID) == "" || strings.TrimSpace(input.Draft.ModelRequestIdentity) == "" ||
		strings.TrimSpace(input.ArtifactID) != input.ArtifactID || input.ArtifactID == "" || len(input.ArtifactID) > 80 {
		return ErrMediaCandidateInvalid
	}
	content, err := agentruntime.DecodeMediaCandidateContent(input.Draft.Payload)
	if err != nil || content.CandidateKey != input.Draft.ArtifactKey || content.ResourceID != input.Draft.ResourceID ||
		content.ProviderRequestIdentity != input.Draft.ModelRequestIdentity || content.SourceTaskID != expectedTaskID {
		return ErrMediaCandidateInvalid
	}
	return nil
}

func validateProductionMediaAttemptCompletion(scope agentruntime.Scope, input ProductionMediaAttemptCompletion) error {
	if err := validateMediaAttemptFence(scope, input.Fence, input.Now); err != nil {
		return err
	}
	if input.ArtifactID != input.Fence.ExpectedArtifactRevisionID ||
		strings.TrimSpace(input.BillingOrderID) != input.BillingOrderID || input.BillingOrderID == "" || len(input.BillingOrderID) > 80 ||
		strings.TrimSpace(input.ResourceID) != input.ResourceID || input.ResourceID == "" || len(input.ResourceID) > 80 ||
		!input.ExpectedStatus.Valid() || strings.TrimSpace(input.LateArtifactID) != input.LateArtifactID ||
		input.LateArtifactID == "" || len(input.LateArtifactID) > 80 || input.LateArtifactID == input.ArtifactID ||
		input.LateDraft.ResourceID != input.ResourceID {
		return ErrMediaAttemptFenceConflict
	}
	return validateMediaCandidateAttemptInput(MediaCandidateAttemptInput{ArtifactID: input.LateArtifactID, Draft: input.LateDraft}, input.Fence.ExpectedTaskID)
}

func mediaAttemptCurrentTx(
	tx *gorm.DB,
	scope agentruntime.Scope,
	fence MediaAttemptCompletionFence,
	allowedToolNames ...agentruntime.ToolName,
) (bool, error) {
	state, err := loadAgentCheckpointForScope(tx, scope, true)
	if err != nil {
		return false, err
	}
	var call model.AgentToolCall
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("run_id = ? AND tool_call_id = ? AND action_version = ?", scope.RunID, fence.ToolCallID, fence.ActionVersion).
		Take(&call).Error; err != nil {
		return false, err
	}
	toolName := agentruntime.ToolName(call.ToolName)
	allowed := false
	for _, candidate := range allowedToolNames {
		if toolName == candidate {
			allowed = true
			break
		}
	}
	if !allowed || !mediaAttemptFenceMatchesInput(toolName, []byte(call.InputJSON), fence) {
		return false, ErrMediaAttemptFenceConflict
	}
	return state.Status == agentruntime.RunWaitingTool && state.PendingToolStarted && state.PendingToolCall != nil &&
		state.PendingToolCall.ToolCallID == fence.ToolCallID && state.PendingToolCall.ActionVersion == fence.ActionVersion &&
		state.PendingToolCall.ToolName == toolName && call.Status == agentruntime.ToolCallRunning &&
		call.ApprovalRequired && call.ApprovalDecision == agentruntime.ToolApprovalApproved, nil
}

func mediaAttemptRecoveryEligibleTx(
	tx *gorm.DB,
	scope agentruntime.Scope,
	fence MediaAttemptCompletionFence,
) (bool, error) {
	state, err := loadAgentCheckpointForScope(tx, scope, true)
	if err != nil {
		return false, err
	}
	var call model.AgentToolCall
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("run_id = ? AND tool_call_id = ? AND action_version = ?", scope.RunID, fence.ToolCallID, fence.ActionVersion).
		Take(&call).Error; err != nil {
		return false, err
	}
	toolName := agentruntime.ToolName(call.ToolName)
	if (toolName != agentruntime.ToolMediaGenerate && toolName != agentruntime.ToolProductionRender) ||
		!mediaAttemptFenceMatchesInput(toolName, []byte(call.InputJSON), fence) {
		return false, ErrMediaAttemptFenceConflict
	}
	terminal := state.Status == agentruntime.RunSucceeded || state.Status == agentruntime.RunFailed || state.Status == agentruntime.RunCancelled
	return !terminal && call.Status == agentruntime.ToolCallFailed &&
		call.ErrorCode == "production_result_invalid" && call.ApprovalRequired &&
		call.ApprovalDecision == agentruntime.ToolApprovalApproved, nil
}

func mediaAttemptFenceMatchesInput(toolName agentruntime.ToolName, raw []byte, fence MediaAttemptCompletionFence) bool {
	switch toolName {
	case agentruntime.ToolMediaGenerate:
		var input struct {
			OutputArtifactID string `json:"outputArtifactId"`
			Commercial       struct {
				ArtifactRevisionID  string `json:"artifactRevisionId"`
				Attempt             int    `json:"attempt"`
				TaskID              string `json:"taskId"`
				ApprovalFingerprint string `json:"approvalFingerprint"`
			} `json:"commercial"`
		}
		return json.Unmarshal(raw, &input) == nil && input.OutputArtifactID == fence.ExpectedArtifactRevisionID &&
			input.Commercial.ArtifactRevisionID == fence.ExpectedArtifactRevisionID && input.Commercial.Attempt == fence.ExpectedAttempt &&
			input.Commercial.TaskID == fence.ExpectedTaskID && input.Commercial.ApprovalFingerprint == fence.ApprovalFingerprint
	case agentruntime.ToolProductionRender:
		var input struct {
			ArtifactID          string `json:"artifactId"`
			Attempt             int    `json:"attempt"`
			TaskID              string `json:"taskId"`
			ApprovalFingerprint string `json:"approvalFingerprint"`
		}
		return json.Unmarshal(raw, &input) == nil && input.ArtifactID == fence.ExpectedArtifactRevisionID &&
			input.Attempt+1 == fence.ExpectedAttempt && input.TaskID == fence.ExpectedTaskID &&
			input.ApprovalFingerprint == fence.ApprovalFingerprint
	default:
		return false
	}
}

func loadExactMediaCandidateBatchTx(
	tx *gorm.DB,
	scope agentruntime.Scope,
	inputs []MediaCandidateAttemptInput,
) ([]MediaCandidateAppendResult, bool, error) {
	results := make([]MediaCandidateAppendResult, 0, len(inputs))
	foundCount := 0
	for _, input := range inputs {
		revision, err := loadExactArtifactRevisionAnyTx(tx, scope, input.ArtifactID, input.Draft)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		foundCount++
		disposition := MediaAttemptWriteAdopted
		if revision.LifecycleStatus == model.AgentArtifactRevisionUnadopted {
			disposition = MediaAttemptWriteUnadopted
		} else if revision.LifecycleStatus != model.AgentArtifactRevisionAwaitingReview {
			return nil, false, ErrArtifactRevisionConflict
		}
		results = append(results, MediaCandidateAppendResult{Revision: *revision, Disposition: disposition})
	}
	if foundCount == 0 {
		return nil, false, nil
	}
	if foundCount != len(inputs) || len(results) != len(inputs) {
		return nil, false, ErrArtifactRevisionConflict
	}
	for index := 1; index < len(results); index++ {
		if results[index].Disposition != results[0].Disposition {
			return nil, false, ErrArtifactRevisionConflict
		}
	}
	return results, true, nil
}

func loadExactArtifactRevisionAnyTx(
	tx *gorm.DB,
	scope agentruntime.Scope,
	artifactID string,
	draft agentruntime.ArtifactDraft,
) (*model.AgentArtifactRevision, error) {
	if draft.UpstreamRevisions == nil {
		draft.UpstreamRevisions = []agentruntime.ArtifactRevisionRef{}
	}
	if draft.SkillVersions == nil {
		draft.SkillVersions = []agentruntime.SkillSelection{}
	}
	upstreamJSON, err := json.Marshal(draft.UpstreamRevisions)
	if err != nil {
		return nil, err
	}
	skillsJSON, err := json.Marshal(draft.SkillVersions)
	if err != nil {
		return nil, err
	}
	var revisions []model.AgentArtifactRevision
	if err := productionArtifactRevisionScopeQuery(tx, scope).
		Where("artifact_id = ?", artifactID).Order("revision ASC").Find(&revisions).Error; err != nil {
		return nil, err
	}
	var exact *model.AgentArtifactRevision
	for index := range revisions {
		revision := &revisions[index]
		if revision.ArtifactKey == draft.ArtifactKey && revision.Kind == draft.Kind && revision.SchemaVersion == draft.SchemaVersion &&
			revision.PayloadJSON == string(draft.Payload) && revision.ResourceID == draft.ResourceID &&
			revision.UpstreamRevisionsJSON == string(upstreamJSON) && revision.ModelRequestIdentity == draft.ModelRequestIdentity &&
			revision.SkillVersionsJSON == string(skillsJSON) && revision.CreatedBySpecialistID == "" {
			if exact != nil {
				return nil, ErrArtifactRevisionConflict
			}
			exact = revision
		}
	}
	if exact == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return exact, nil
}
