package repository

import (
	"errors"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

type AgentCapabilityResourceFact struct {
	ResourceID string
	Name       string
	Kind       string
	MimeType   string
	SizeBytes  int64
	Width      int
	Height     int
	DurationMS int64
}

func (r *Repository) AgentReadyResourcesForTenant(scope agentruntime.Scope, resourceIDs []string) ([]AgentCapabilityResourceFact, error) {
	if err := scope.Validate(); err != nil || len(resourceIDs) < 1 || len(resourceIDs) > 100 {
		return nil, errors.New("agent ready resource scope is invalid")
	}
	query := r.db.Table("resources").Where("resources.status = ?", model.ResourceStatusReady)
	switch scope.TenantKind {
	case agentruntime.TenantPersonal:
		query = query.Where("resources.user_id = ? AND resources.team_id = ''", scope.TenantID)
	case agentruntime.TenantTeam:
		query = query.Where("resources.team_id = ?", scope.TenantID)
	default:
		return nil, errors.New("agent ready resource tenant is invalid")
	}
	var facts []AgentCapabilityResourceFact
	err := query.Where("resources.id IN ?", resourceIDs).Select(
		"resources.id AS resource_id, resources.id AS name, resources.kind, resources.mime_type, resources.size AS size_bytes, resources.width, resources.height, resources.duration_ms AS duration_ms",
	).Order("resources.id ASC").Find(&facts).Error
	return facts, err
}

func (r *Repository) AgentCapabilityResourcesForScope(scope agentruntime.Scope, resourceIDs []string, limit int) ([]AgentCapabilityResourceFact, error) {
	if err := scope.Validate(); err != nil || scope.DomainProjectID == "" || limit < 1 || limit > 100 {
		return nil, errors.New("agent capability resource scope is invalid")
	}
	query := r.db.Table("resources").
		Joins("JOIN asset_representations ON asset_representations.resource_id = resources.id").
		Joins("JOIN asset_versions ON asset_versions.id = asset_representations.asset_version_id").
		Joins("JOIN assets ON assets.id = asset_versions.asset_id").
		Joins("JOIN project_asset_links ON project_asset_links.asset_id = assets.id").
		Where("project_asset_links.project_id = ?", scope.DomainProjectID).
		Where("resources.status = ?", model.ResourceStatusReady).
		Where("asset_versions.status = ? AND assets.status = ?", model.AssetVersionStatusConfirmed, model.AssetVersionStatusConfirmed)
	switch scope.TenantKind {
	case agentruntime.TenantPersonal:
		query = query.Where("resources.user_id = ? AND resources.team_id = ''", scope.TenantID)
	case agentruntime.TenantTeam:
		query = query.Where("resources.team_id = ?", scope.TenantID)
	default:
		return nil, errors.New("agent capability resource tenant is invalid")
	}
	if len(resourceIDs) > 0 {
		query = query.Where("resources.id IN ?", resourceIDs)
	}
	var facts []AgentCapabilityResourceFact
	err := query.Distinct().Select(
		"resources.id AS resource_id, assets.title AS name, resources.kind, resources.mime_type, resources.size AS size_bytes, resources.width, resources.height, resources.duration_ms AS duration_ms",
	).Order("resources.id ASC").Limit(limit).Scan(&facts).Error
	return facts, err
}
