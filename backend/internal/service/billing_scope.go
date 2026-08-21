package service

import (
	"errors"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"

	"gorm.io/gorm"
)

// billingAccountScope identifies the credit account that owns a charge.
// Membership grants model access; it must never implicitly decide which account pays.
type billingAccountScope struct {
	TeamID string
}

func personalBillingAccountScope() billingAccountScope {
	return billingAccountScope{}
}

func teamBillingAccountScope(teamID string) (billingAccountScope, error) {
	teamID = strings.TrimSpace(teamID)
	if teamID == "" {
		return billingAccountScope{}, BadAuthRequest("团队计费范围缺少团队标识")
	}
	return billingAccountScope{TeamID: teamID}, nil
}

func billingAccountScopeFromAgent(scope agentruntime.Scope) (billingAccountScope, error) {
	switch scope.TenantKind {
	case agentruntime.TenantPersonal:
		if strings.TrimSpace(scope.TenantID) == "" || strings.TrimSpace(scope.TenantID) != strings.TrimSpace(scope.ActorUserID) {
			return billingAccountScope{}, BadAuthRequest("个人 Agent 计费范围无效")
		}
		return personalBillingAccountScope(), nil
	case agentruntime.TenantTeam:
		return teamBillingAccountScope(scope.TenantID)
	default:
		return billingAccountScope{}, BadAuthRequest("Agent 计费范围类型无效")
	}
}

func (s *Service) billingAccountScopeForTask(userID string, canvasOrProjectID string) (billingAccountScope, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return billingAccountScope{}, BadAuthRequest("计费用户无效")
	}
	id := strings.TrimSpace(canvasOrProjectID)
	if id == "" {
		return personalBillingAccountScope(), nil
	}

	canvas, err := s.repo.CanvasProject(id)
	if err == nil {
		canvas, _, err = s.canvasAccess(userID, canvas.ID)
		if err != nil {
			return billingAccountScope{}, err
		}
		if strings.TrimSpace(canvas.TeamID) == "" {
			return personalBillingAccountScope(), nil
		}
		return teamBillingAccountScope(canvas.TeamID)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return billingAccountScope{}, err
	}

	project, err := s.repo.ProjectEditableForUser(userID, id, time.Now())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return billingAccountScope{}, BadAuthRequest("画布尚未同步云端，无法确定计费账户")
		}
		return billingAccountScope{}, err
	}
	if strings.TrimSpace(project.TeamID) == "" {
		return personalBillingAccountScope(), nil
	}
	return teamBillingAccountScope(project.TeamID)
}
