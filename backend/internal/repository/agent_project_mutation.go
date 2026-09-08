package repository

import (
	"errors"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrProjectRevisionConflict  = errors.New("project revision conflict")
	ErrProjectShotsInUse        = errors.New("project shots have asset associations; replacement is forbidden")
)

type AgentProjectMutationInput struct {
	Scope            agentruntime.Scope
	ToolCallID       string
	ActionVersion    int
	ProposalHash     string
	BaseRevision     int64
	ToolReceiptJSON  string
	Candidates       []model.ProjectAssetCandidate
	ShotReplacements []AgentProjectShotReplacement
	Now              time.Time
}

type AgentProjectShotReplacement struct {
	UnitID string
	Shots  []model.Shot
}

func (r *Repository) CommitAgentProjectMutation(input AgentProjectMutationInput) error {
	if err := validateAgentProjectMutationInput(input); err != nil {
		return err
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		if _, err := agentRunForScopeDB(tx, input.Scope); err != nil {
			return err
		}
		var toolCall model.AgentToolCall
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("run_id = ? AND tool_call_id = ? AND action_version = ?", input.Scope.RunID, input.ToolCallID, input.ActionVersion).
			Take(&toolCall).Error; err != nil {
			return err
		}
		if toolCall.ToolName != string(agentruntime.ToolProjectApplyOps) || toolCall.ApprovalDecision != agentruntime.ToolApprovalApproved ||
			toolCall.ApprovalByUserID != input.Scope.ActorUserID || toolCall.ApprovalProposalHash != input.ProposalHash || toolCall.ApprovalDecidedAt == nil {
			return ErrAgentRuntimeStepConflict
		}
		if toolCall.Status == agentruntime.ToolCallSucceeded {
			if toolCall.OutputJSON != input.ToolReceiptJSON {
				return ErrAgentRuntimeStepConflict
			}
			return nil
		}
		if toolCall.Status != agentruntime.ToolCallPending && toolCall.Status != agentruntime.ToolCallRunning {
			return ErrAgentRuntimeStepConflict
		}

		project, err := New(tx).ProjectEditableForUser(input.Scope.ActorUserID, input.Scope.DomainProjectID, input.Now)
		if err != nil {
			return err
		}
		if project.ID != input.Scope.DomainProjectID || project.Revision != input.BaseRevision {
			return ErrProjectRevisionConflict
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&model.Project{}, "id = ? AND revision = ?", project.ID, input.BaseRevision).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProjectRevisionConflict
			}
			return err
		}
		if err := lockAgentProjectMutationUnitsTx(tx, project.ID, input.Candidates, input.ShotReplacements); err != nil {
			return err
		}
		for _, replacement := range input.ShotReplacements {
			if err := replaceProjectUnitShotsTx(tx, project.ID, replacement.UnitID, replacement.Shots); err != nil {
				return err
			}
		}
		if len(input.Candidates) > 0 {
			if err := tx.Create(&input.Candidates).Error; err != nil {
				return err
			}
		}
		update := struct {
			Revision  int64     `gorm:"column:revision"`
			UpdatedAt time.Time `gorm:"column:updated_at"`
		}{Revision: input.BaseRevision + 1, UpdatedAt: input.Now}
		result := tx.Model(&model.Project{}).Where("id = ? AND revision = ?", project.ID, input.BaseRevision).Updates(update)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrProjectRevisionConflict
		}
		result = tx.Model(&model.AgentToolCall{}).
			Where("id = ? AND status IN ?", toolCall.ID, []agentruntime.ToolCallStatus{agentruntime.ToolCallPending, agentruntime.ToolCallRunning}).
			Updates(agentToolCompletionUpdates{Status: agentruntime.ToolCallSucceeded, OutputJSON: input.ToolReceiptJSON, UpdatedAt: input.Now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrAgentRuntimeStepConflict
		}
		return nil
	})
}

func lockAgentProjectMutationUnitsTx(tx *gorm.DB, projectID string, candidates []model.ProjectAssetCandidate, replacements []AgentProjectShotReplacement) error {
	unitIDs := make(map[string]struct{})
	for _, candidate := range candidates {
		unitIDs[candidate.UnitID] = struct{}{}
	}
	for _, replacement := range replacements {
		unitIDs[replacement.UnitID] = struct{}{}
	}
	ids := make([]string, 0, len(unitIDs))
	for unitID := range unitIDs {
		if unitID == "" {
			return ErrProjectUnitScopeConflict
		}
		ids = append(ids, unitID)
	}
	if len(ids) == 0 {
		return ErrProjectUnitScopeConflict
	}
	var units []model.ProjectUnit
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("project_id = ? AND id IN ?", projectID, ids).
		Find(&units).Error; err != nil {
		return err
	}
	if len(units) != len(ids) {
		return ErrProjectUnitScopeConflict
	}
	return nil
}

func validateAgentProjectMutationInput(input AgentProjectMutationInput) error {
	if err := input.Scope.Validate(); err != nil {
		return err
	}
	decoded, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolProjectApplyOps, []byte(input.ToolReceiptJSON))
	receipt, ok := decoded.(agentruntime.ProjectApplyOpsResult)
	if err != nil || !ok || input.ToolCallID == "" || input.ActionVersion < 1 || input.ProposalHash == "" || input.Now.IsZero() ||
		receipt.DomainProjectID != input.Scope.DomainProjectID || receipt.BaseRevision != input.BaseRevision ||
		receipt.CommittedRevision != input.BaseRevision+1 || receipt.ProposalHash != input.ProposalHash {
		return ErrAgentRuntimeStepConflict
	}
	return nil
}

func replaceProjectUnitShotsTx(tx *gorm.DB, projectID string, unitID string, shots []model.Shot) error {
	var unit model.ProjectUnit
	if err := tx.First(&unit, "id = ? AND project_id = ?", unitID, projectID).Error; err != nil {
		return err
	}
	shotIDs := tx.Model(&model.Shot{}).Select("id").Where("project_id = ? AND unit_id = ?", projectID, unitID)
	var references int64
	if err := tx.Model(&model.ShotAssetReference{}).Where("shot_id IN (?)", shotIDs).Count(&references).Error; err != nil {
		return err
	}
	var candidates int64
	if err := tx.Model(&model.ProjectAssetCandidate{}).Where("shot_id IN (?)", shotIDs).Count(&candidates).Error; err != nil {
		return err
	}
	if references > 0 || candidates > 0 {
		return ErrProjectShotsInUse
	}
	if err := tx.Where("project_id = ? AND unit_id = ?", projectID, unitID).Delete(&model.Shot{}).Error; err != nil {
		return err
	}
	return tx.Create(&shots).Error
}
