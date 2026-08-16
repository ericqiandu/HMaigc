package repository

import (
	"errors"
	"time"

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

func (r *Repository) CanvasProject(id string) (*model.CanvasProject, error) {
	var project model.CanvasProject
	if err := r.db.First(&project, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &project, nil
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
		var current model.CanvasProject
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", canvasID).Error; err != nil {
			return err
		}
		if err := authorizeCanvasWrite(tx, &current, actorUserID, now); err != nil {
			return err
		}
		var duplicate model.CanvasChange
		duplicateErr := tx.First(&duplicate, "canvas_id = ? AND client_mutation_id = ?", canvasID, clientMutationID).Error
		if duplicateErr == nil {
			if duplicate.ActorUserID != actorUserID || duplicate.PayloadJSON != changePayloadJSON {
				return ErrCanvasMutationMismatch
			}
			committed = CanvasChangeCommit{Project: &current, Change: &duplicate}
			return nil
		}
		if !errors.Is(duplicateErr, gorm.ErrRecordNotFound) {
			return duplicateErr
		}

		if current.Revision != baseRevision {
			return ErrCanvasRevisionConflict
		}
		payloadJSON, title, err := apply(&current)
		if err != nil {
			return err
		}
		nextRevision := current.Revision + 1
		result := tx.Model(&model.CanvasProject{}).
			Where("id = ? AND revision = ?", current.ID, current.Revision).
			Updates(map[string]any{
				"payload_json":       payloadJSON,
				"title":              title,
				"revision":           nextRevision,
				"updated_by_user_id": actorUserID,
				"updated_at":         now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrCanvasRevisionConflict
		}
		change := &model.CanvasChange{
			ID: changeID, CanvasID: current.ID, Revision: nextRevision,
			ActorUserID: actorUserID, ClientMutationID: clientMutationID,
			PayloadJSON: changePayloadJSON, CreatedAt: now,
		}
		if err := tx.Create(change).Error; err != nil {
			return err
		}
		current.PayloadJSON = payloadJSON
		current.Title = title
		current.Revision = nextRevision
		current.UpdatedByUserID = actorUserID
		current.UpdatedAt = now
		committed = CanvasChangeCommit{Project: &current, Change: change}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &committed, nil
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

func (r *Repository) DeleteCanvasProjectWithCollaboration(canvasID string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("canvas_id = ?", canvasID).Delete(&model.CanvasChange{}).Error; err != nil {
			return err
		}
		if err := tx.Where("canvas_id = ?", canvasID).Delete(&model.CanvasCollaborator{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", canvasID).Delete(&model.CanvasShare{}).Error; err != nil {
			return err
		}
		result := tx.Delete(&model.CanvasProject{}, "id = ?", canvasID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}
