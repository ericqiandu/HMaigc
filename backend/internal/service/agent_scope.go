package service

import (
	"errors"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

func (s *Service) AuthorizeAgentScope(actorUserID, canvasID, threadID, runID string) (agentruntime.Scope, error) {
	actorUserID = strings.TrimSpace(actorUserID)
	canvas, access, err := s.canvasAccess(actorUserID, strings.TrimSpace(canvasID))
	if err != nil {
		return agentruntime.Scope{}, err
	}
	scope, err := scopeFromCanvasAccess(actorUserID, strings.TrimSpace(threadID), strings.TrimSpace(runID), canvas, access)
	if err != nil {
		return agentruntime.Scope{}, err
	}
	if err := scope.Validate(); err != nil {
		return agentruntime.Scope{}, err
	}
	return scope, nil
}

func scopeFromCanvasAccess(actorUserID, threadID, runID string, canvas *model.CanvasProject, access CanvasAccessView) (agentruntime.Scope, error) {
	if canvas == nil {
		return agentruntime.Scope{}, errors.New("agent scope canvas is required")
	}
	level, err := agentAccessLevel(access.Level)
	if err != nil {
		return agentruntime.Scope{}, err
	}
	tenantKind := agentruntime.TenantPersonal
	tenantID := actorUserID
	subscriptionActive := true
	if canvas.TeamID != "" {
		tenantKind = agentruntime.TenantTeam
		tenantID = canvas.TeamID
		subscriptionActive = access.TeamSubscriptionActive
	}
	return agentruntime.Scope{
		TenantKind:      tenantKind,
		TenantID:        tenantID,
		ActorUserID:     actorUserID,
		DomainProjectID: canvas.ProjectID,
		CanvasID:        canvas.ID,
		ThreadID:        threadID,
		RunID:           runID,
		Access: agentruntime.AccessGrant{
			Level:              level,
			SubscriptionActive: subscriptionActive,
		},
	}, nil
}

func agentAccessLevel(level model.CanvasAccessLevel) (agentruntime.AccessLevel, error) {
	switch level {
	case model.CanvasAccessViewer:
		return agentruntime.AccessViewer, nil
	case model.CanvasAccessEditor:
		return agentruntime.AccessEditor, nil
	case model.CanvasAccessManager:
		return agentruntime.AccessManager, nil
	default:
		return "", errors.New("canvas access level is invalid for agent scope")
	}
}
