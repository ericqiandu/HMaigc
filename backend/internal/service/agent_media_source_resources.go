package service

import (
	"encoding/json"
	"errors"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/repository"
)

func (s *Service) agentMediaInputResourcesForScope(scope agentruntime.Scope, resourceIDs []string) ([]repository.AgentCapabilityResourceFact, error) {
	if err := scope.Validate(); err != nil || len(resourceIDs) < 1 || len(resourceIDs) > 100 {
		return nil, errors.New("agent media input resource scope is invalid")
	}
	canvas, _, err := s.canvasAccess(scope.ActorUserID, scope.CanvasID)
	if err != nil || canvas.ProjectID != scope.DomainProjectID {
		return nil, errors.Join(errors.New("agent media input canvas scope is invalid"), err)
	}
	boundResourceIDs, err := canvasPayloadBoundResourceIDs(canvas.PayloadJSON)
	if err != nil {
		return nil, err
	}
	ownedFacts, err := s.repo.AgentReadyResourcesForTenant(scope, resourceIDs)
	if err != nil {
		return nil, err
	}
	ownedByID := make(map[string]repository.AgentCapabilityResourceFact, len(ownedFacts))
	for _, fact := range ownedFacts {
		ownedByID[fact.ResourceID] = fact
	}
	projectByID := map[string]repository.AgentCapabilityResourceFact{}
	if scope.DomainProjectID != "" {
		projectFacts, projectErr := s.repo.AgentCapabilityResourcesForScope(scope, resourceIDs, len(resourceIDs))
		if projectErr != nil {
			return nil, projectErr
		}
		for _, fact := range projectFacts {
			projectByID[fact.ResourceID] = fact
		}
	}
	result := make([]repository.AgentCapabilityResourceFact, 0, len(resourceIDs))
	for _, resourceID := range resourceIDs {
		owned, ownedExists := ownedByID[resourceID]
		if !ownedExists {
			return nil, errors.New("agent media input resource is unavailable")
		}
		if projectFact, projectExists := projectByID[resourceID]; projectExists {
			result = append(result, projectFact)
			continue
		}
		if _, bound := boundResourceIDs[resourceID]; !bound {
			return nil, errors.New("agent media input resource is not bound to the current canvas")
		}
		result = append(result, owned)
	}
	return result, nil
}

func canvasPayloadBoundResourceIDs(payload string) (map[string]struct{}, error) {
	var document struct {
		Nodes []struct {
			Type     string `json:"type"`
			Metadata struct {
				Content    string `json:"content"`
				StorageKey string `json:"storageKey"`
				Status     string `json:"status"`
			} `json:"metadata"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(payload), &document); err != nil || document.Nodes == nil {
		return nil, errors.New("agent media input canvas facts are invalid")
	}
	result := make(map[string]struct{})
	for _, node := range document.Nodes {
		if node.Metadata.Status != "success" || (node.Type != "image" && node.Type != "video" && node.Type != "audio") {
			continue
		}
		resourceID, found := strings.CutPrefix(node.Metadata.StorageKey, "resource:")
		if !found || strings.TrimSpace(resourceID) == "" || node.Metadata.Content != "/api/resources/"+resourceID+"/file" {
			continue
		}
		result[resourceID] = struct{}{}
	}
	return result, nil
}
