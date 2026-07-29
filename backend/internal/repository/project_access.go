package repository

import (
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ProjectCollaboratorRecord struct {
	model.ProjectCollaborator
	Username    string               `json:"username"`
	DisplayName string               `json:"displayName"`
	TeamRole    model.TeamMemberRole `json:"teamRole"`
}

func (r *Repository) ProjectReadableForUser(userID string, projectID string) (*model.Project, error) {
	var project model.Project
	err := r.db.Raw(`
		SELECT projects.*
		  FROM projects
		  LEFT JOIN team_members members
		    ON members.team_id = projects.team_id
		   AND members.user_id = ?
		   AND members.status = ?
		 WHERE projects.id = ?
		   AND (projects.user_id = ? OR (projects.team_id <> '' AND members.id IS NOT NULL))
		 LIMIT 1
	`, userID, model.TeamMemberStatusActive, projectID, userID).Scan(&project).Error
	if err != nil {
		return nil, err
	}
	if project.ID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	return &project, nil
}

func (r *Repository) ProjectEditableForUser(userID string, projectID string, now time.Time) (*model.Project, error) {
	return r.projectForEffectiveRole(userID, projectID, now, []model.ProjectAccessRole{model.ProjectAccessEditor, model.ProjectAccessManager})
}

func (r *Repository) ProjectManageableForUser(userID string, projectID string, now time.Time) (*model.Project, error) {
	return r.projectForEffectiveRole(userID, projectID, now, []model.ProjectAccessRole{model.ProjectAccessManager})
}

func (r *Repository) projectForEffectiveRole(userID string, projectID string, now time.Time, roles []model.ProjectAccessRole) (*model.Project, error) {
	var project model.Project
	err := r.db.Raw(`
		SELECT projects.*
		  FROM projects
		  LEFT JOIN team_members members
		    ON members.team_id = projects.team_id
		   AND members.user_id = ?
		   AND members.status = ?
		  LEFT JOIN project_collaborators collaborators
		    ON collaborators.project_id = projects.id
		   AND collaborators.user_id = ?
		  LEFT JOIN membership_subscriptions subscriptions
		    ON subscriptions.team_id = projects.team_id
		   AND subscriptions.status = ?
		   AND subscriptions.starts_at <= ?
		   AND (subscriptions.ends_at IS NULL OR subscriptions.ends_at > ?)
		 WHERE projects.id = ?
		   AND (
		     projects.user_id = ?
		     OR (
		       projects.team_id <> ''
		       AND members.id IS NOT NULL
		       AND subscriptions.id IS NOT NULL
		       AND COALESCE(collaborators.role,
		         CASE members.role WHEN 'owner' THEN 'manager' WHEN 'admin' THEN 'manager' ELSE 'editor' END
		       ) IN ?
		     )
		   )
		 LIMIT 1
	`, userID, model.TeamMemberStatusActive, userID, model.MembershipSubscriptionActive, now, now, projectID, userID, roles).Scan(&project).Error
	if err != nil {
		return nil, err
	}
	if project.ID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	return &project, nil
}

func (r *Repository) AccessibleProjects(userID string) ([]model.Project, error) {
	var projects []model.Project
	err := r.db.Raw(`
		SELECT DISTINCT projects.*
		  FROM projects
		  LEFT JOIN team_members members
		    ON members.team_id = projects.team_id
		   AND members.user_id = ?
		   AND members.status = ?
		 WHERE projects.user_id = ?
		    OR (projects.team_id <> '' AND members.id IS NOT NULL)
		 ORDER BY projects.updated_at DESC
	`, userID, model.TeamMemberStatusActive, userID).Scan(&projects).Error
	return projects, err
}

func (r *Repository) AssignProjectTeam(projectID string, ownerUserID string, teamID string, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.Project{}).
			Where("id = ? AND user_id = ?", projectID, ownerUserID).
			Updates(map[string]interface{}{"team_id": teamID, "revision": gorm.Expr("revision + 1"), "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return tx.Where("project_id = ?", projectID).Delete(&model.ProjectCollaborator{}).Error
	})
}

func (r *Repository) ProjectCollaboratorRecords(projectID string) ([]ProjectCollaboratorRecord, error) {
	var records []ProjectCollaboratorRecord
	err := r.db.Raw(`
		SELECT collaborators.*, users.username, users.display_name, members.role AS team_role
		  FROM project_collaborators collaborators
		  JOIN users ON users.id = collaborators.user_id
		  JOIN projects ON projects.id = collaborators.project_id
		  JOIN team_members members
		    ON members.team_id = projects.team_id
		   AND members.user_id = collaborators.user_id
		   AND members.status = ?
		 WHERE collaborators.project_id = ?
		 ORDER BY users.display_name, users.username
	`, model.TeamMemberStatusActive, projectID).Scan(&records).Error
	return records, err
}

func (r *Repository) SaveProjectCollaborator(collaborator *model.ProjectCollaborator) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "project_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"role", "updated_at"}),
	}).Create(collaborator).Error
}
