package service

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

type AssignProjectTeamRequest struct {
	TeamID string `json:"teamId"`
}

type UpdateProjectCollaboratorRequest struct {
	Role model.ProjectAccessRole `json:"role"`
}

type ProjectAccessMember struct {
	UserID      string                  `json:"userId"`
	Username    string                  `json:"username"`
	DisplayName string                  `json:"displayName"`
	TeamRole    model.TeamMemberRole    `json:"teamRole"`
	Role        model.ProjectAccessRole `json:"role"`
	Explicit    bool                    `json:"explicit"`
}

type ProjectAccessOverview struct {
	ProjectID string                `json:"projectId"`
	TeamID    string                `json:"teamId"`
	Members   []ProjectAccessMember `json:"members"`
}

func (s *Service) AssignProjectTeam(user *model.User, projectID string, req AssignProjectTeamRequest) (*model.Project, error) {
	project, err := s.repo.ProjectReadableForUser(user.ID, strings.TrimSpace(projectID))
	if err != nil {
		return nil, err
	}
	if project.UserID != user.ID {
		return nil, Forbidden("只有项目所有者可以调整项目归属")
	}
	teamID := strings.TrimSpace(req.TeamID)
	if teamID != "" {
		team, member, accessErr := s.teamAccess(user.ID, teamID)
		if accessErr != nil {
			return nil, accessErr
		}
		if member.Role != model.TeamMemberRoleOwner && member.Role != model.TeamMemberRoleAdmin {
			return nil, Forbidden("只有团队所有者或管理员可以接收团队项目")
		}
		entitlement, entitlementErr := s.teamEntitlement(user.ID, team.ID)
		if entitlementErr != nil {
			return nil, entitlementErr
		}
		if !entitlement.ProjectPermissionsEnabled {
			return nil, Forbidden("当前团队套餐未开通项目权限管理")
		}
	}
	if err := s.repo.AssignProjectTeam(project.ID, user.ID, teamID, time.Now()); err != nil {
		return nil, err
	}
	return s.repo.ProjectReadableForUser(user.ID, project.ID)
}

func (s *Service) ProjectAccessOverview(user *model.User, projectID string) (*ProjectAccessOverview, error) {
	project, err := s.repo.ProjectManageableForUser(user.ID, strings.TrimSpace(projectID), time.Now())
	if err != nil {
		return nil, err
	}
	if project.TeamID == "" {
		return nil, BadAuthRequest("个人项目不需要团队权限配置")
	}
	members, err := s.repo.TeamMemberRecords(project.TeamID)
	if err != nil {
		return nil, err
	}
	overrides, err := s.repo.ProjectCollaboratorRecords(project.ID)
	if err != nil {
		return nil, err
	}
	overrideByUser := make(map[string]repository.ProjectCollaboratorRecord, len(overrides))
	for _, override := range overrides {
		overrideByUser[override.UserID] = override
	}
	result := make([]ProjectAccessMember, 0, len(members))
	for _, member := range members {
		role := defaultProjectRole(member.Role)
		override, explicit := overrideByUser[member.UserID]
		if explicit {
			role = override.Role
		}
		result = append(result, ProjectAccessMember{
			UserID: member.UserID, Username: member.Username, DisplayName: member.DisplayName,
			TeamRole: member.Role, Role: role, Explicit: explicit,
		})
	}
	return &ProjectAccessOverview{ProjectID: project.ID, TeamID: project.TeamID, Members: result}, nil
}

func (s *Service) UpdateProjectCollaborator(user *model.User, projectID string, targetUserID string, req UpdateProjectCollaboratorRequest) error {
	project, err := s.repo.ProjectManageableForUser(user.ID, strings.TrimSpace(projectID), time.Now())
	if err != nil {
		return err
	}
	if project.TeamID == "" {
		return BadAuthRequest("个人项目不支持团队成员权限")
	}
	if req.Role != model.ProjectAccessViewer && req.Role != model.ProjectAccessEditor && req.Role != model.ProjectAccessManager {
		return BadAuthRequest("项目权限角色无效")
	}
	target, err := s.repo.TeamMemberForUser(project.TeamID, strings.TrimSpace(targetUserID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &AuthError{Status: http.StatusNotFound, Message: "目标用户不是当前团队成员"}
	}
	if err != nil {
		return err
	}
	if target.Role == model.TeamMemberRoleOwner && req.Role != model.ProjectAccessManager {
		return Forbidden("团队所有者必须保留项目管理权限")
	}
	now := time.Now()
	return s.repo.SaveProjectCollaborator(&model.ProjectCollaborator{
		ID: newID(), ProjectID: project.ID, UserID: target.UserID, Role: req.Role,
		CreatedAt: now, UpdatedAt: now,
	})
}

func defaultProjectRole(role model.TeamMemberRole) model.ProjectAccessRole {
	if role == model.TeamMemberRoleOwner || role == model.TeamMemberRoleAdmin {
		return model.ProjectAccessManager
	}
	return model.ProjectAccessEditor
}
