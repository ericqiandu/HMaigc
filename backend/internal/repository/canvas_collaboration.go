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
	ErrCanvasRevisionConflict = errors.New("canvas revision conflict")
	ErrCanvasMutationMismatch = errors.New("canvas mutation id was reused with different content")
	ErrCanvasWriteForbidden   = errors.New("canvas write forbidden")
	ErrCanvasPlanInactive     = errors.New("canvas team subscription inactive")
)

type CanvasCollaboratorRecord struct {
	model.CanvasCollaborator
	Username    string                 `json:"username"`
	DisplayName string                 `json:"displayName"`
	AvatarURL   string                 `json:"avatarUrl,omitempty"`
	TeamRole    model.TeamMemberRole   `json:"teamRole"`
	TeamStatus  model.TeamMemberStatus `json:"teamStatus"`
}

type CanvasProjectSummaryRecord struct {
	ID                 string                  `json:"id"`
	UserID             string                  `json:"userId"`
	TeamID             string                  `json:"teamId,omitempty"`
	ProjectID          string                  `json:"projectId,omitempty"`
	Title              string                  `json:"title"`
	Revision           int64                   `json:"revision"`
	DefaultTeamAccess  model.CanvasAccessLevel `json:"defaultTeamAccess,omitempty"`
	UpdatedByUserID    string                  `json:"updatedByUserId,omitempty"`
	CurrentTeamRole    model.TeamMemberRole    `json:"currentTeamRole,omitempty"`
	OverrideAccess     model.CanvasAccessLevel `json:"overrideAccess,omitempty"`
	SubscriptionActive bool                    `json:"subscriptionActive"`
	CreatedAt          time.Time               `json:"createdAt"`
	UpdatedAt          time.Time               `json:"updatedAt"`
}

type CanvasChangeCommit struct {
	Project *model.CanvasProject
	Change  *model.CanvasChange
}

type CanvasChangeApply func(current *model.CanvasProject) (payloadJSON string, title string, err error)

type canvasProjectRevisionUpdates struct {
	PayloadJSON     string
	Title           string
	Revision        int64
	UpdatedByUserID string
	UpdatedAt       time.Time
}

type AgentCanvasChangeInput struct {
	Scope             agentruntime.Scope
	ToolCallID        string
	ActionVersion     int
	ProposalHash      string
	CanvasID          string
	ChangeID          string
	ActorUserID       string
	BaseRevision      int64
	ClientMutationID  string
	ChangePayloadJSON string
	ToolReceiptJSON   string
	Now               time.Time
	Apply             CanvasChangeApply
}

func (r *Repository) CanvasProject(id string) (*model.CanvasProject, error) {
	var project model.CanvasProject
	if err := r.db.First(&project, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &project, nil
}

func (r *Repository) CanvasProjectDeletion(canvasID string) (*model.CanvasProjectDeletion, error) {
	var deletion model.CanvasProjectDeletion
	if err := r.db.First(&deletion, "canvas_id = ?", canvasID).Error; err != nil {
		return nil, err
	}
	return &deletion, nil
}

func (r *Repository) CanvasProjectDeletionsForActor(userID string) ([]model.CanvasProjectDeletion, error) {
	var deletions []model.CanvasProjectDeletion
	err := r.canvasProjectDeletionsForActorQuery(userID).
		Order("canvas_project_deletions.deleted_at DESC").
		Find(&deletions).Error
	return deletions, err
}

func (r *Repository) CanvasProjectDeletionForActor(userID string, canvasID string) (*model.CanvasProjectDeletion, error) {
	var deletion model.CanvasProjectDeletion
	if err := r.canvasProjectDeletionsForActorQuery(userID).
		Where("canvas_project_deletions.canvas_id = ?", canvasID).
		First(&deletion).Error; err != nil {
		return nil, err
	}
	return &deletion, nil
}

func (r *Repository) canvasProjectDeletionsForActorQuery(userID string) *gorm.DB {
	return r.db.Model(&model.CanvasProjectDeletion{}).Where(`
		(canvas_project_deletions.team_id = '' AND canvas_project_deletions.user_id = ?)
		OR (canvas_project_deletions.team_id <> '' AND EXISTS (
			SELECT 1
			FROM team_members
			WHERE team_members.team_id = canvas_project_deletions.team_id
			  AND team_members.user_id = ?
			  AND team_members.status = ?
		))
	`, userID, userID, model.TeamMemberStatusActive)
}

func (r *Repository) CanvasProjectSummariesForActor(userID string) ([]CanvasProjectSummaryRecord, error) {
	var records []CanvasProjectSummaryRecord
	now := time.Now()
	err := r.db.Raw(`
		SELECT DISTINCT canvases.id, canvases.user_id, canvases.team_id, canvases.project_id,
		       canvases.title, canvases.revision, canvases.default_team_access,
		       canvases.updated_by_user_id, canvases.created_at, canvases.updated_at,
		       COALESCE(members.role, '') AS current_team_role,
		       COALESCE(overrides.access, '') AS override_access,
		       CASE
		         WHEN canvases.team_id = '' THEN TRUE
		         ELSE EXISTS (
		           SELECT 1
		           FROM membership_subscriptions subscriptions
		           WHERE subscriptions.team_id = canvases.team_id
		             AND subscriptions.status = ?
		             AND subscriptions.starts_at <= ?
		             AND (subscriptions.ends_at IS NULL OR subscriptions.ends_at > ?)
		         )
		       END AS subscription_active
		FROM canvas_projects canvases
		LEFT JOIN team_members members
		  ON members.team_id = canvases.team_id
		 AND members.user_id = ?
		 AND members.status = ?
		LEFT JOIN canvas_collaborators overrides
		  ON overrides.canvas_id = canvases.id
		 AND overrides.user_id = ?
		WHERE (canvases.team_id = '' AND canvases.user_id = ?)
		   OR (canvases.team_id <> '' AND members.id IS NOT NULL)
		ORDER BY canvases.updated_at DESC
	`,
		model.MembershipSubscriptionActive, now, now,
		userID, model.TeamMemberStatusActive, userID, userID,
	).Scan(&records).Error
	return records, err
}

func (r *Repository) TeamCanvasProjectSummaries(teamID string) ([]CanvasProjectSummaryRecord, error) {
	var records []CanvasProjectSummaryRecord
	err := r.db.Model(&model.CanvasProject{}).
		Select("id", "user_id", "team_id", "project_id", "title", "revision", "default_team_access", "updated_by_user_id", "created_at", "updated_at").
		Where("team_id = ?", teamID).
		Order("updated_at DESC").
		Scan(&records).Error
	return records, err
}

func (r *Repository) CanvasCollaboratorForUser(canvasID string, userID string) (*model.CanvasCollaborator, error) {
	var collaborator model.CanvasCollaborator
	if err := r.db.First(&collaborator, "canvas_id = ? AND user_id = ?", canvasID, userID).Error; err != nil {
		return nil, err
	}
	return &collaborator, nil
}

func (r *Repository) CanvasCollaboratorRecords(canvasID string, teamID string) ([]CanvasCollaboratorRecord, error) {
	var records []CanvasCollaboratorRecord
	err := r.db.Raw(`
		SELECT collaborators.*, users.username, users.display_name,
		       COALESCE((
		           SELECT identities.avatar_url
		           FROM user_identities identities
		           WHERE identities.user_id = users.id AND identities.avatar_url <> ''
		           ORDER BY identities.updated_at DESC
		           LIMIT 1
		       ), '') AS avatar_url,
		       members.role AS team_role, members.status AS team_status
		FROM canvas_collaborators collaborators
		JOIN users ON users.id = collaborators.user_id
		JOIN team_members members
		  ON members.team_id = ?
		 AND members.user_id = collaborators.user_id
		WHERE collaborators.canvas_id = ?
		ORDER BY collaborators.created_at ASC
	`, teamID, canvasID).Scan(&records).Error
	return records, err
}

func (r *Repository) ConfigureCanvasTeam(
	canvasID string,
	ownerUserID string,
	teamID string,
	defaultAccess model.CanvasAccessLevel,
	updatedByUserID string,
	audit *model.TeamAuditEvent,
	now time.Time,
) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var canvas model.CanvasProject
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&canvas, "id = ?", canvasID).Error; err != nil {
			return err
		}
		if canvas.UserID != ownerUserID {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Model(&model.CanvasProject{}).Where("id = ?", canvas.ID).Updates(map[string]any{
			"team_id":             teamID,
			"default_team_access": defaultAccess,
			"updated_by_user_id":  updatedByUserID,
			"updated_at":          now,
		}).Error; err != nil {
			return err
		}
		if teamID == "" {
			if err := tx.Where("canvas_id = ?", canvas.ID).Delete(&model.CanvasCollaborator{}).Error; err != nil {
				return err
			}
		}
		if audit != nil {
			return tx.Create(audit).Error
		}
		return nil
	})
}

func (r *Repository) UpsertCanvasCollaborator(collaborator *model.CanvasCollaborator, audit *model.TeamAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "canvas_id"}, {Name: "user_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"access":     collaborator.Access,
				"created_by": collaborator.CreatedBy,
				"team_id":    collaborator.TeamID,
				"updated_at": collaborator.UpdatedAt,
			}),
		}).Create(collaborator)
		if result.Error != nil {
			return result.Error
		}
		return tx.Create(audit).Error
	})
}

func (r *Repository) DeleteCanvasCollaborator(canvasID string, userID string, audit *model.TeamAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Delete(&model.CanvasCollaborator{}, "canvas_id = ? AND user_id = ?", canvasID, userID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return tx.Create(audit).Error
	})
}

func (r *Repository) CommitCanvasChange(
	canvasID string,
	changeID string,
	actorUserID string,
	baseRevision int64,
	clientMutationID string,
	changePayloadJSON string,
	now time.Time,
	apply CanvasChangeApply,
) (*CanvasChangeCommit, error) {
	var committed CanvasChangeCommit
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var err error
		committed, err = commitCanvasChangeTx(tx, canvasID, changeID, actorUserID, baseRevision, clientMutationID, changePayloadJSON, now, apply)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &committed, nil
}

// CommitAgentCanvasChange persists the authoritative canvas mutation and its
// successful tool receipt in one transaction. This closes the crash window in
// which the canvas could advance while the runtime still considered the tool
// running.
func (r *Repository) CommitAgentCanvasChange(input AgentCanvasChangeInput) (*CanvasChangeCommit, error) {
	if err := validateAgentCanvasChangeInput(input); err != nil {
		return nil, err
	}
	var committed CanvasChangeCommit
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if _, err := agentRunForScopeDB(tx, input.Scope); err != nil {
			return err
		}
		var toolCall model.AgentToolCall
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("run_id = ? AND tool_call_id = ? AND action_version = ?", input.Scope.RunID, input.ToolCallID, input.ActionVersion).
			Take(&toolCall).Error; err != nil {
			return err
		}
		if toolCall.ToolName != string(agentruntime.ToolCanvasApplyOps) || toolCall.ApprovalDecision != agentruntime.ToolApprovalApproved ||
			toolCall.ApprovalByUserID != input.Scope.ActorUserID || toolCall.ApprovalProposalHash != input.ProposalHash || toolCall.ApprovalDecidedAt == nil {
			return ErrAgentRuntimeStepConflict
		}
		if toolCall.Status != agentruntime.ToolCallPending && toolCall.Status != agentruntime.ToolCallRunning && toolCall.Status != agentruntime.ToolCallSucceeded {
			return ErrAgentRuntimeStepConflict
		}
		if toolCall.Status == agentruntime.ToolCallSucceeded && toolCall.OutputJSON != input.ToolReceiptJSON {
			return ErrAgentRuntimeStepConflict
		}

		var err error
		committed, err = commitCanvasChangeTx(tx, input.CanvasID, input.ChangeID, input.ActorUserID, input.BaseRevision,
			input.ClientMutationID, input.ChangePayloadJSON, input.Now, input.Apply)
		if err != nil {
			return err
		}
		if committed.Change.Revision != input.BaseRevision+1 {
			return ErrCanvasMutationMismatch
		}
		if toolCall.Status == agentruntime.ToolCallSucceeded {
			return nil
		}
		result := tx.Model(&model.AgentToolCall{}).
			Where("id = ? AND status IN ?", toolCall.ID, []agentruntime.ToolCallStatus{agentruntime.ToolCallPending, agentruntime.ToolCallRunning}).
			Updates(agentToolCompletionUpdates{
				Status: agentruntime.ToolCallSucceeded, OutputJSON: input.ToolReceiptJSON, UpdatedAt: input.Now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrAgentRuntimeStepConflict
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &committed, nil
}

func validateAgentCanvasChangeInput(input AgentCanvasChangeInput) error {
	if err := input.Scope.Validate(); err != nil {
		return err
	}
	if input.CanvasID != input.Scope.CanvasID || input.ActorUserID != input.Scope.ActorUserID || input.ToolCallID == "" ||
		input.ActionVersion < 1 || input.ChangeID == "" || input.ClientMutationID == "" || input.Now.IsZero() || input.Apply == nil {
		return ErrAgentRuntimeStepConflict
	}
	decoded, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolCanvasApplyOps, []byte(input.ToolReceiptJSON))
	receipt, ok := decoded.(agentruntime.CanvasApplyOpsResult)
	if err != nil || !ok || receipt.CanvasID != input.CanvasID || receipt.BaseRevision != input.BaseRevision ||
		receipt.CommittedRevision != input.BaseRevision+1 || receipt.ClientMutationID != input.ClientMutationID ||
		receipt.ProposalHash != input.ProposalHash {
		return ErrAgentRuntimeStepConflict
	}
	return nil
}

func commitCanvasChangeTx(
	tx *gorm.DB,
	canvasID string,
	changeID string,
	actorUserID string,
	baseRevision int64,
	clientMutationID string,
	changePayloadJSON string,
	now time.Time,
	apply CanvasChangeApply,
) (CanvasChangeCommit, error) {
	var current model.CanvasProject
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", canvasID).Error; err != nil {
		return CanvasChangeCommit{}, err
	}
	if err := authorizeCanvasWrite(tx, &current, actorUserID, now); err != nil {
		return CanvasChangeCommit{}, err
	}
	var duplicate model.CanvasChange
	duplicateErr := tx.First(&duplicate, "canvas_id = ? AND client_mutation_id = ?", canvasID, clientMutationID).Error
	if duplicateErr == nil {
		if duplicate.ActorUserID != actorUserID || duplicate.PayloadJSON != changePayloadJSON {
			return CanvasChangeCommit{}, ErrCanvasMutationMismatch
		}
		return CanvasChangeCommit{Project: &current, Change: &duplicate}, nil
	}
	if !errors.Is(duplicateErr, gorm.ErrRecordNotFound) {
		return CanvasChangeCommit{}, duplicateErr
	}
	if current.Revision != baseRevision {
		return CanvasChangeCommit{}, ErrCanvasRevisionConflict
	}
	payloadJSON, title, err := apply(&current)
	if err != nil {
		return CanvasChangeCommit{}, err
	}
	nextRevision := current.Revision + 1
	result := tx.Model(&model.CanvasProject{}).
		Where("id = ? AND revision = ?", current.ID, current.Revision).
		Select("payload_json", "title", "revision", "updated_by_user_id", "updated_at").
		Updates(canvasProjectRevisionUpdates{
			PayloadJSON: payloadJSON, Title: title, Revision: nextRevision,
			UpdatedByUserID: actorUserID, UpdatedAt: now,
		})
	if result.Error != nil {
		return CanvasChangeCommit{}, result.Error
	}
	if result.RowsAffected != 1 {
		return CanvasChangeCommit{}, ErrCanvasRevisionConflict
	}
	change := &model.CanvasChange{
		ID: changeID, CanvasID: current.ID, Revision: nextRevision,
		ActorUserID: actorUserID, ClientMutationID: clientMutationID,
		PayloadJSON: changePayloadJSON, CreatedAt: now,
	}
	if err := tx.Create(change).Error; err != nil {
		return CanvasChangeCommit{}, err
	}
	current.PayloadJSON = payloadJSON
	current.Title = title
	current.Revision = nextRevision
	current.UpdatedByUserID = actorUserID
	current.UpdatedAt = now
	return CanvasChangeCommit{Project: &current, Change: change}, nil
}

func authorizeCanvasWrite(tx *gorm.DB, canvas *model.CanvasProject, actorUserID string, now time.Time) error {
	if canvas.TeamID == "" {
		if canvas.UserID != actorUserID {
			return ErrCanvasWriteForbidden
		}
		return nil
	}
	var member model.TeamMember
	if err := tx.First(&member,
		"team_id = ? AND user_id = ? AND status = ?",
		canvas.TeamID, actorUserID, model.TeamMemberStatusActive,
	).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrCanvasWriteForbidden
	} else if err != nil {
		return err
	}
	var subscriptions int64
	if err := tx.Model(&model.MembershipSubscription{}).
		Where(
			"team_id = ? AND status = ? AND starts_at <= ? AND (ends_at IS NULL OR ends_at > ?)",
			canvas.TeamID, model.MembershipSubscriptionActive, now, now,
		).
		Count(&subscriptions).Error; err != nil {
		return err
	}
	if subscriptions == 0 {
		return ErrCanvasPlanInactive
	}
	if member.Role == model.TeamMemberRoleOwner || member.Role == model.TeamMemberRoleAdmin {
		return nil
	}
	level := canvas.DefaultTeamAccess
	var collaborator model.CanvasCollaborator
	if err := tx.First(&collaborator, "canvas_id = ? AND user_id = ?", canvas.ID, actorUserID).Error; err == nil {
		level = collaborator.Access
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if level != model.CanvasAccessEditor {
		return ErrCanvasWriteForbidden
	}
	return nil
}

func (r *Repository) DeleteCanvasProjectWithCollaboration(project *model.CanvasProject, deletedByUserID string, deletedAt time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		deletion := model.CanvasProjectDeletion{
			CanvasID:        project.ID,
			UserID:          project.UserID,
			TeamID:          project.TeamID,
			DeletedByUserID: deletedByUserID,
			DeletedAt:       deletedAt,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "canvas_id"}},
			DoNothing: true,
		}).Create(&deletion).Error; err != nil {
			return err
		}
		if err := tx.Where("canvas_id = ?", project.ID).Delete(&model.CanvasChange{}).Error; err != nil {
			return err
		}
		if err := tx.Where("canvas_id = ?", project.ID).Delete(&model.CanvasCollaborator{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", project.ID).Delete(&model.CanvasShare{}).Error; err != nil {
			return err
		}
		result := tx.Delete(&model.CanvasProject{}, "id = ? AND user_id = ? AND team_id = ?", project.ID, project.UserID, project.TeamID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			var existing model.CanvasProjectDeletion
			if err := tx.First(&existing, "canvas_id = ?", project.ID).Error; err != nil {
				return err
			}
			if existing.UserID != project.UserID || existing.TeamID != project.TeamID {
				return errors.New("canvas deletion scope mismatch")
			}
		}
		return nil
	})
}
