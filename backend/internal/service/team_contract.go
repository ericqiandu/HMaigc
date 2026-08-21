package service

import (
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

type TeamCapabilities struct {
	CanRenameTeam               bool                   `json:"canRenameTeam"`
	CanManageSubscription       bool                   `json:"canManageSubscription"`
	CanInviteMembers            bool                   `json:"canInviteMembers"`
	InviteRoles                 []model.TeamMemberRole `json:"inviteRoles"`
	CanManageMemberRoles        bool                   `json:"canManageMemberRoles"`
	CanManageMemberCreditLimits bool                   `json:"canManageMemberCreditLimits"`
	CanRemoveMembers            bool                   `json:"canRemoveMembers"`
	CanLeaveTeam                bool                   `json:"canLeaveTeam"`
	CanManageProjects           bool                   `json:"canManageProjects"`
	CanUploadSharedAssets       bool                   `json:"canUploadSharedAssets"`
	CanViewAudit                bool                   `json:"canViewAudit"`
}

func BuildTeamCapabilities(role model.TeamMemberRole, sharedAssetsEnabled bool, projectPermissionsEnabled bool) TeamCapabilities {
	capabilities := TeamCapabilities{
		InviteRoles:           make([]model.TeamMemberRole, 0),
		CanLeaveTeam:          role != model.TeamMemberRoleOwner,
		CanUploadSharedAssets: sharedAssetsEnabled && (role == model.TeamMemberRoleOwner || role == model.TeamMemberRoleAdmin),
	}
	switch role {
	case model.TeamMemberRoleOwner:
		capabilities.CanRenameTeam = true
		capabilities.CanManageSubscription = true
		capabilities.CanInviteMembers = true
		capabilities.InviteRoles = []model.TeamMemberRole{model.TeamMemberRoleAdmin, model.TeamMemberRoleMember}
		capabilities.CanManageMemberRoles = true
		capabilities.CanManageMemberCreditLimits = true
		capabilities.CanRemoveMembers = true
		capabilities.CanManageProjects = projectPermissionsEnabled
		capabilities.CanViewAudit = true
	case model.TeamMemberRoleAdmin:
		capabilities.CanInviteMembers = true
		capabilities.InviteRoles = []model.TeamMemberRole{model.TeamMemberRoleMember}
		capabilities.CanRemoveMembers = true
		capabilities.CanManageProjects = projectPermissionsEnabled
		capabilities.CanViewAudit = true
	}
	return capabilities
}

func normalizeTeamWorkspaceCollections(workspace *TeamWorkspace) {
	if workspace.Teams == nil {
		workspace.Teams = make([]TeamSummary, 0)
	}
	if workspace.IncomingInvitations == nil {
		workspace.IncomingInvitations = make([]repository.IncomingTeamInvitationRecord, 0)
	}
}

func normalizeTeamDetailCollections(detail *TeamDetail) {
	if detail.Members == nil {
		detail.Members = make([]TeamMemberView, 0)
	}
	if detail.Invitations == nil {
		detail.Invitations = make([]model.TeamInvitation, 0)
	}
	if detail.AuditEvents == nil {
		detail.AuditEvents = make([]repository.TeamAuditRecord, 0)
	}
}
