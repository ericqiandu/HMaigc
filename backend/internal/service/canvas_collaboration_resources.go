package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

// CanvasResource authorizes a resource through the canvas access boundary and
// then resolves its actual owner. A team member never gains access to a resource
// that is not structurally referenced by this canvas.
func (s *Service) CanvasResource(userID string, canvasID string, resourceID string) (*model.Resource, error) {
	project, _, err := s.canvasAccess(userID, canvasID)
	if err != nil {
		return nil, err
	}
	allowedResources, err := canvasPayloadResourceIDs(project.PayloadJSON)
	if err != nil {
		return nil, err
	}
	resourceID = strings.TrimSpace(resourceID)
	if !allowedResources[resourceID] {
		return nil, gorm.ErrRecordNotFound
	}
	return s.repo.Resource(resourceID)
}

func (s *Service) OpenCanvasResourceRange(userID string, canvasID string, resourceID string, rangeHeader string) (*ResourceStream, error) {
	project, _, err := s.canvasAccess(userID, canvasID)
	if err != nil {
		return nil, err
	}
	allowedResources, err := canvasPayloadResourceIDs(project.PayloadJSON)
	if err != nil {
		return nil, err
	}
	resourceID = strings.TrimSpace(resourceID)
	if !allowedResources[resourceID] {
		return nil, gorm.ErrRecordNotFound
	}
	resource, err := s.repo.Resource(resourceID)
	if err != nil {
		return nil, err
	}
	return s.OpenResourceRange(resource.UserID, resource.ID, rangeHeader)
}

func canvasProjectResponse(project *model.CanvasProject, access CanvasAccessView) (json.RawMessage, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(project.PayloadJSON), &payload); err != nil {
		return nil, fmt.Errorf("画布数据损坏：%w", err)
	}
	fields := map[string]any{
		"ownerUserId": project.UserID,
		"teamId":      project.TeamID, "revision": project.Revision,
		"defaultTeamAccess": project.DefaultTeamAccess, "accessLevel": access.Level,
		"canEdit": access.CanEdit, "canManage": access.CanManage,
		"teamSubscriptionActive": access.TeamSubscriptionActive,
	}
	for key, value := range fields {
		payload[key] = value
	}
	if project.TeamID != "" {
		rewritten, ok := rewriteTeamCanvasResourceURLs(payload, project.ID).(map[string]any)
		if !ok {
			return nil, errors.New("团队画布资源地址转换失败")
		}
		payload = rewritten
	}
	return json.Marshal(payload)
}

func rewriteTeamCanvasResourceURLs(value any, canvasID string) any {
	switch item := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(item))
		for key, child := range item {
			result[key] = rewriteTeamCanvasResourceURLs(child, canvasID)
		}
		for storageKey, urlKeys := range canvasResourceFieldPairs(item) {
			resourceID := canvasResourceID(stringValue(item[storageKey]))
			if resourceID == "" {
				continue
			}
			for _, urlKey := range urlKeys {
				if _, exists := item[urlKey]; exists {
					result[urlKey] = teamCanvasResourceURL(canvasID, resourceID)
				}
			}
		}
		return result
	case []any:
		result := make([]any, len(item))
		for index, child := range item {
			result[index] = rewriteTeamCanvasResourceURLs(child, canvasID)
		}
		return result
	default:
		return value
	}
}

func canvasPayloadResourceIDs(payloadJSON string) (map[string]bool, error) {
	var payload any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return nil, fmt.Errorf("画布数据损坏：%w", err)
	}
	result := map[string]bool{}
	collectCanvasResourceIDs(payload, result)
	return result, nil
}

func collectCanvasResourceIDs(value any, result map[string]bool) {
	switch item := value.(type) {
	case map[string]any:
		for key, child := range item {
			if isCanvasResourceStorageKey(key) {
				if resourceID := canvasResourceID(stringValue(child)); resourceID != "" {
					result[resourceID] = true
				}
			}
			collectCanvasResourceIDs(child, result)
		}
	case []any:
		for _, child := range item {
			collectCanvasResourceIDs(child, result)
		}
	}
}

func (s *Service) validateCanvasMutationResources(userID string, currentPayload string, patch CanvasMutationPatch) error {
	currentResources, err := canvasPayloadResourceIDs(currentPayload)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	patchResources, err := canvasPayloadResourceIDs(string(encoded))
	if err != nil {
		return err
	}
	for resourceID := range patchResources {
		if currentResources[resourceID] {
			continue
		}
		if _, err := s.repo.ResourceForUser(userID, resourceID); errors.Is(err, gorm.ErrRecordNotFound) {
			return Forbidden("画布变更引用了当前用户无权使用的资源")
		} else if err != nil {
			return err
		}
	}
	return nil
}

func isCanvasResourceStorageKey(key string) bool {
	return key == "storageKey" || strings.HasSuffix(key, "StorageKey")
}

func canvasResourceFieldPairs(value map[string]any) map[string][]string {
	pairs := map[string][]string{}
	for key := range value {
		if !isCanvasResourceStorageKey(key) {
			continue
		}
		if key == "storageKey" {
			pairs[key] = []string{"content", "url", "dataUrl"}
			continue
		}
		prefix := strings.TrimSuffix(key, "StorageKey")
		pairs[key] = []string{prefix + "Url"}
	}
	return pairs
}

func teamCanvasResourceURL(canvasID string, resourceID string) string {
	return "/api/canvas-projects/" + canvasID + "/resources/" + resourceID + "/file"
}

func canonicalizeCanvasMutationResourceURLs(patch CanvasMutationPatch, canvasID string) (CanvasMutationPatch, error) {
	return transformCanvasMutationResourceURLs(patch, func(value any) any {
		return canonicalizeTeamCanvasResourceURLs(value, canvasID)
	})
}

func rewriteCanvasMutationResourceURLs(patch CanvasMutationPatch, canvasID string) (CanvasMutationPatch, error) {
	return transformCanvasMutationResourceURLs(patch, func(value any) any {
		return rewriteTeamCanvasResourceURLs(value, canvasID)
	})
}

func transformCanvasMutationResourceURLs(patch CanvasMutationPatch, transform func(any) any) (CanvasMutationPatch, error) {
	encoded, err := json.Marshal(patch)
	if err != nil {
		return CanvasMutationPatch{}, err
	}
	var value any
	if err := json.Unmarshal(encoded, &value); err != nil {
		return CanvasMutationPatch{}, err
	}
	encoded, err = json.Marshal(transform(value))
	if err != nil {
		return CanvasMutationPatch{}, err
	}
	var result CanvasMutationPatch
	if err := json.Unmarshal(encoded, &result); err != nil {
		return CanvasMutationPatch{}, err
	}
	return result, nil
}

func canonicalizeTeamCanvasResourceURLs(value any, canvasID string) any {
	switch item := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(item))
		for key, child := range item {
			result[key] = canonicalizeTeamCanvasResourceURLs(child, canvasID)
		}
		for storageKey, urlKeys := range canvasResourceFieldPairs(item) {
			resourceID := canvasResourceID(stringValue(item[storageKey]))
			if resourceID == "" {
				continue
			}
			for _, urlKey := range urlKeys {
				if teamResourceID := teamCanvasResourceID(stringValue(item[urlKey]), canvasID); teamResourceID == resourceID {
					result[urlKey] = "/api/resources/" + resourceID + "/file"
				}
			}
		}
		return result
	case []any:
		result := make([]any, len(item))
		for index, child := range item {
			result[index] = canonicalizeTeamCanvasResourceURLs(child, canvasID)
		}
		return result
	default:
		return value
	}
}

func teamCanvasResourceID(value string, canvasID string) string {
	prefix := "/api/canvas-projects/" + canvasID + "/resources/"
	index := strings.Index(value, prefix)
	if index < 0 {
		return ""
	}
	remainder := value[index+len(prefix):]
	if end := strings.IndexByte(remainder, '/'); end >= 0 {
		remainder = remainder[:end]
	}
	return validCanvasResourceID(remainder)
}
