package service

import (
	"bytes"
	"errors"
	"image"
	"io"
	"mime"
	"net/http"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	_ "golang.org/x/image/webp"
)

const (
	agentVisionResourceLimit    = 12
	agentVisionResourceMaxBytes = int64(32 * 1024 * 1024)
	agentVisionResourceMaxEdge  = 8192
)

type agentVisionResource struct {
	Fact     repository.AgentCapabilityResourceFact
	Resource model.Resource
	Width    int
	Height   int
}

func (s *Service) agentVisionResourcesForRun(scope agentruntime.Scope, configuration agentruntime.RunConfiguration, resourceIDs []string) ([]agentVisionResource, error) {
	if err := scope.Validate(); err != nil || len(resourceIDs) < 1 || len(resourceIDs) > agentVisionResourceLimit {
		return nil, errors.New("agent vision resource request is invalid")
	}
	requested := make([]string, len(resourceIDs))
	seen := make(map[string]struct{}, len(resourceIDs))
	for index, rawResourceID := range resourceIDs {
		resourceID := strings.TrimSpace(rawResourceID)
		if resourceID == "" || resourceID != rawResourceID || len(resourceID) > 80 {
			return nil, errors.New("agent vision resource identity is invalid")
		}
		if _, duplicate := seen[resourceID]; duplicate {
			return nil, errors.New("agent vision resources contain a duplicate identity")
		}
		seen[resourceID] = struct{}{}
		requested[index] = resourceID
	}
	canvas, _, err := s.canvasAccess(scope.ActorUserID, scope.CanvasID)
	if err != nil {
		return nil, err
	}
	if !agentVisionCanvasMatchesScope(*canvas, scope) {
		return nil, errors.New("agent vision canvas scope is stale")
	}

	attachmentNames := make(map[string]string, len(configuration.Attachments))
	for _, attachment := range configuration.Attachments {
		attachmentNames[attachment.ResourceID] = attachment.Name
	}
	projectFacts := make(map[string]repository.AgentCapabilityResourceFact)
	if scope.DomainProjectID != "" {
		facts, err := s.repo.AgentCapabilityResourcesForScope(scope, requested, len(requested))
		if err != nil {
			return nil, err
		}
		for _, fact := range facts {
			projectFacts[fact.ResourceID] = fact
		}
	}

	needsCanvasAuthorization := false
	for _, resourceID := range requested {
		_, attached := attachmentNames[resourceID]
		_, projectLinked := projectFacts[resourceID]
		if !attached && !projectLinked {
			needsCanvasAuthorization = true
			break
		}
	}
	canvasResourceIDs := make(map[string]struct{})
	if needsCanvasAuthorization {
		canvasResourceIDs, err = canvasPayloadBoundResourceIDs(canvas.PayloadJSON)
		if err != nil {
			return nil, err
		}
	}

	ownedFacts, err := s.repo.AgentReadyResourcesForTenant(scope, requested)
	if err != nil {
		return nil, err
	}
	ownedByID := make(map[string]repository.AgentCapabilityResourceFact, len(ownedFacts))
	for _, fact := range ownedFacts {
		ownedByID[fact.ResourceID] = fact
	}
	resources := make([]agentVisionResource, 0, len(requested))
	for _, resourceID := range requested {
		fact, owned := ownedByID[resourceID]
		if !owned {
			return nil, errors.New("agent vision resource is unavailable in the current tenant")
		}
		if attachmentName, attached := attachmentNames[resourceID]; attached {
			fact.Name = attachmentName
		} else if projectFact, projectLinked := projectFacts[resourceID]; projectLinked {
			fact = projectFact
		} else if _, bound := canvasResourceIDs[resourceID]; !bound {
			return nil, errors.New("agent vision resource is not authorized by the current Run")
		}
		resource, err := s.probeAgentVisionResource(scope, fact)
		if err != nil {
			return nil, err
		}
		resources = append(resources, resource)
	}
	return resources, nil
}

func agentVisionCanvasMatchesScope(canvas model.CanvasProject, scope agentruntime.Scope) bool {
	if canvas.ID != scope.CanvasID || canvas.ProjectID != scope.DomainProjectID {
		return false
	}
	switch scope.TenantKind {
	case agentruntime.TenantPersonal:
		return canvas.TeamID == "" && canvas.UserID == scope.TenantID
	case agentruntime.TenantTeam:
		return canvas.TeamID == scope.TenantID
	default:
		return false
	}
}

func (s *Service) probeAgentVisionResource(scope agentruntime.Scope, fact repository.AgentCapabilityResourceFact) (agentVisionResource, error) {
	if fact.Kind != "image" || fact.SizeBytes < 1 || fact.SizeBytes > agentVisionResourceMaxBytes {
		return agentVisionResource{}, errors.New("agent vision resource media facts are invalid")
	}
	declaredMIME, _, err := mime.ParseMediaType(strings.TrimSpace(fact.MimeType))
	if err != nil || !supportedAgentVisionMIME(declaredMIME) {
		return agentVisionResource{}, errors.New("agent vision resource MIME is unsupported")
	}
	stream, err := s.openAgentVisionResource(scope, fact.ResourceID)
	if err != nil {
		return agentVisionResource{}, err
	}
	data, readErr := io.ReadAll(io.LimitReader(stream.Body, agentVisionResourceMaxBytes+1))
	closeErr := stream.Body.Close()
	if readErr != nil {
		return agentVisionResource{}, readErr
	}
	if closeErr != nil {
		return agentVisionResource{}, closeErr
	}
	if int64(len(data)) > agentVisionResourceMaxBytes || int64(len(data)) != fact.SizeBytes || stream.Resource == nil || stream.Resource.ID != fact.ResourceID || stream.Resource.Size != fact.SizeBytes {
		return agentVisionResource{}, errors.New("agent vision resource size facts conflict")
	}
	actualMIME := http.DetectContentType(data)
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || actualMIME != declaredMIME || actualMIME != agentVisionMIMEForFormat(format) {
		return agentVisionResource{}, errors.New("agent vision resource content does not match its MIME")
	}
	if config.Width < 1 || config.Height < 1 || config.Width > agentVisionResourceMaxEdge || config.Height > agentVisionResourceMaxEdge {
		return agentVisionResource{}, errors.New("agent vision resource dimensions are invalid")
	}
	resource := *stream.Resource
	if !agentVisionResourceMatchesScope(resource, scope) || resource.Status != model.ResourceStatusReady || resource.Kind != "image" {
		return agentVisionResource{}, errors.New("agent vision resource ownership facts conflict")
	}
	resource.MimeType = actualMIME
	resource.Size = int64(len(data))
	resource.Width = config.Width
	resource.Height = config.Height
	fact.MimeType = actualMIME
	fact.SizeBytes = resource.Size
	fact.Width = config.Width
	fact.Height = config.Height
	return agentVisionResource{Fact: fact, Resource: resource, Width: config.Width, Height: config.Height}, nil
}

func (s *Service) openAgentVisionResource(scope agentruntime.Scope, resourceID string) (*ResourceStream, error) {
	switch scope.TenantKind {
	case agentruntime.TenantPersonal:
		return s.OpenResourceRange(scope.TenantID, resourceID, "")
	case agentruntime.TenantTeam:
		return s.OpenTeamResourceRange(scope.ActorUserID, scope.TenantID, resourceID, "")
	default:
		return nil, errors.New("agent vision resource tenant is invalid")
	}
}

func agentVisionResourceMatchesScope(resource model.Resource, scope agentruntime.Scope) bool {
	switch scope.TenantKind {
	case agentruntime.TenantPersonal:
		return resource.UserID == scope.TenantID && resource.TeamID == ""
	case agentruntime.TenantTeam:
		return resource.TeamID == scope.TenantID
	default:
		return false
	}
}

func supportedAgentVisionMIME(value string) bool {
	switch value {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func agentVisionMIMEForFormat(format string) string {
	switch format {
	case "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	default:
		return ""
	}
}
