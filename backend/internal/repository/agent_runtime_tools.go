package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrAgentCapabilityAssetInvalid   = errors.New("agent capability asset publication is invalid")
	ErrAgentCapabilityAssetForbidden = errors.New("agent capability asset publication is forbidden")
	ErrAgentCapabilityAssetConflict  = errors.New("agent capability asset publication conflicts with stored facts")
)

type PublishAgentCapabilityAssetInput struct {
	Context          context.Context
	Scope            agentruntime.Scope
	ResourceID       string
	DisplayName      string
	ClientMutationID string
	Now              time.Time
}

type PublishedAgentCapabilityAsset struct {
	Asset      model.Asset
	ResourceID string
	Replayed   bool
}

type AgentCapabilityAssetPublicationFact struct {
	Asset          model.Asset
	Version        model.AssetVersion
	Link           model.ProjectAssetLink
	Representation model.AssetRepresentation
	Resource       model.Resource
}

type ActiveAgentRunReference struct {
	RunID       string `gorm:"column:run_id"`
	ActorUserID string `gorm:"column:actor_user_id"`
}

func (r *Repository) StaleAgentRunsAfter(afterRunID string, updatedBefore time.Time, limit int) ([]ActiveAgentRunReference, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("active agent run limit is invalid")
	}
	if updatedBefore.IsZero() {
		return nil, errors.New("stale agent run cutoff is invalid")
	}
	var references []ActiveAgentRunReference
	query := r.db.Table("agent_runs").Select("id AS run_id, actor_user_id").
		Where("status IN ?", []agentruntime.RunStatus{agentruntime.RunQueued, agentruntime.RunRunning, agentruntime.RunWaitingTool}).
		Where("updated_at < ?", updatedBefore.UTC()).
		Order("id ASC").Limit(limit)
	if afterRunID != "" {
		query = query.Where("id > ?", afterRunID)
	}
	err := query.Scan(&references).Error
	return references, err
}

func (r *Repository) AgentToolCallForScope(scope agentruntime.Scope, toolCallID string, actionVersion int) (*model.AgentToolCall, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	var call model.AgentToolCall
	err := r.db.Table("agent_tool_calls").
		Select("agent_tool_calls.*").
		Joins("JOIN agent_runs ON agent_runs.id = agent_tool_calls.run_id").
		Joins("JOIN agent_threads ON agent_threads.id = agent_runs.thread_id").
		Where(`agent_tool_calls.run_id = ? AND agent_tool_calls.tool_call_id = ? AND agent_tool_calls.action_version = ?
			AND agent_runs.thread_id = ? AND agent_runs.actor_user_id = ?
			AND agent_threads.tenant_kind = ? AND agent_threads.tenant_id = ?
			AND agent_threads.created_by_user_id = ? AND agent_threads.domain_project_id = ?
			AND agent_threads.canvas_id = ?`,
			scope.RunID, toolCallID, actionVersion, scope.ThreadID, scope.ActorUserID,
			scope.TenantKind, scope.TenantID, scope.ActorUserID, scope.DomainProjectID, scope.CanvasID).
		First(&call).Error
	if err != nil {
		return nil, err
	}
	return &call, nil
}

func (r *Repository) AgentToolCallsForScope(scope agentruntime.Scope) ([]model.AgentToolCall, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	var calls []model.AgentToolCall
	err := r.db.Table("agent_tool_calls").
		Select("agent_tool_calls.*").
		Joins("JOIN agent_runs ON agent_runs.id = agent_tool_calls.run_id").
		Joins("JOIN agent_threads ON agent_threads.id = agent_runs.thread_id").
		Where(`agent_tool_calls.run_id = ? AND agent_runs.thread_id = ? AND agent_runs.actor_user_id = ?
			AND agent_threads.tenant_kind = ? AND agent_threads.tenant_id = ?
			AND agent_threads.created_by_user_id = ? AND agent_threads.domain_project_id = ?
			AND agent_threads.canvas_id = ?`,
			scope.RunID, scope.ThreadID, scope.ActorUserID, scope.TenantKind, scope.TenantID,
			scope.ActorUserID, scope.DomainProjectID, scope.CanvasID).
		Order("agent_tool_calls.created_at ASC, agent_tool_calls.id ASC").
		Find(&calls).Error
	return calls, err
}

func (r *Repository) CompletedCapabilityCallForThread(
	scope agentruntime.Scope,
	toolName agentruntime.ToolName,
	capabilityIdempotencyKey string,
) (*model.AgentToolCall, error) {
	capabilityIdempotencyKey = strings.TrimSpace(capabilityIdempotencyKey)
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if !toolName.Known() || capabilityIdempotencyKey == "" || len(capabilityIdempotencyKey) > 80 {
		return nil, errors.New("agent capability replay identity is invalid")
	}
	var call model.AgentToolCall
	err := r.db.Table("agent_tool_calls").
		Select("agent_tool_calls.*").
		Joins("JOIN agent_runs ON agent_runs.id = agent_tool_calls.run_id").
		Joins("JOIN agent_threads ON agent_threads.id = agent_runs.thread_id").
		Where(`agent_tool_calls.tool_name = ? AND agent_tool_calls.capability_idempotency_key = ?
			AND agent_tool_calls.status = ? AND agent_runs.thread_id = ? AND agent_runs.actor_user_id = ?
			AND agent_threads.tenant_kind = ? AND agent_threads.tenant_id = ?
			AND agent_threads.created_by_user_id = ? AND agent_threads.domain_project_id = ?
			AND agent_threads.canvas_id = ?`,
			toolName, capabilityIdempotencyKey, agentruntime.ToolCallSucceeded,
			scope.ThreadID, scope.ActorUserID, scope.TenantKind, scope.TenantID,
			scope.ActorUserID, scope.DomainProjectID, scope.CanvasID).
		Order("agent_tool_calls.updated_at DESC, agent_tool_calls.id DESC").
		First(&call).Error
	if err != nil {
		return nil, err
	}
	return &call, nil
}

func (r *Repository) InFlightCapabilityCallForThread(
	scope agentruntime.Scope,
	toolName agentruntime.ToolName,
	capabilityIdempotencyKey string,
) (*model.AgentToolCall, error) {
	capabilityIdempotencyKey = strings.TrimSpace(capabilityIdempotencyKey)
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if !toolName.Known() || capabilityIdempotencyKey == "" || len(capabilityIdempotencyKey) > 80 {
		return nil, errors.New("agent capability in-flight identity is invalid")
	}
	var call model.AgentToolCall
	err := r.db.Table("agent_tool_calls").
		Select("agent_tool_calls.*").
		Joins("JOIN agent_runs ON agent_runs.id = agent_tool_calls.run_id").
		Joins("JOIN agent_threads ON agent_threads.id = agent_runs.thread_id").
		Where(`agent_tool_calls.tool_name = ? AND agent_tool_calls.capability_idempotency_key = ?
			AND agent_tool_calls.status IN ? AND agent_tool_calls.approval_decision = ?
			AND agent_runs.thread_id = ? AND agent_runs.actor_user_id = ?
			AND agent_threads.tenant_kind = ? AND agent_threads.tenant_id = ?
			AND agent_threads.created_by_user_id = ? AND agent_threads.domain_project_id = ?
			AND agent_threads.canvas_id = ?`,
			toolName, capabilityIdempotencyKey,
			[]agentruntime.ToolCallStatus{agentruntime.ToolCallPending, agentruntime.ToolCallRunning},
			agentruntime.ToolApprovalApproved,
			scope.ThreadID, scope.ActorUserID, scope.TenantKind, scope.TenantID,
			scope.ActorUserID, scope.DomainProjectID, scope.CanvasID).
		Order("agent_tool_calls.updated_at DESC, agent_tool_calls.id DESC").
		First(&call).Error
	if err != nil {
		return nil, err
	}
	return &call, nil
}

func (r *Repository) PublishAgentCapabilityAsset(input PublishAgentCapabilityAssetInput) (*PublishedAgentCapabilityAsset, error) {
	input.ResourceID = strings.TrimSpace(input.ResourceID)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.ClientMutationID = strings.TrimSpace(input.ClientMutationID)
	if input.Context == nil || input.Scope.Validate() != nil || input.Scope.DomainProjectID == "" ||
		input.ResourceID == "" || input.DisplayName == "" || input.ClientMutationID == "" || input.Now.IsZero() {
		return nil, ErrAgentCapabilityAssetInvalid
	}
	var result PublishedAgentCapabilityAsset
	err := r.db.WithContext(input.Context).Transaction(func(tx *gorm.DB) error {
		project, err := New(tx).ProjectEditableForUser(input.Scope.ActorUserID, input.Scope.DomainProjectID, input.Now)
		if err != nil || project.ID != input.Scope.DomainProjectID {
			return ErrAgentCapabilityAssetForbidden
		}
		if err := requireActiveCurrentAgentRunTx(tx, input.Scope); err != nil {
			return ErrAgentCapabilityAssetForbidden
		}
		resource, err := agentCapabilityOwnedResourceTx(tx, input.Scope, input.ResourceID)
		if err != nil {
			return err
		}
		assetID := agentAssetRecordID("capability-asset", strings.Join([]string{
			string(input.Scope.TenantKind), input.Scope.TenantID, input.Scope.DomainProjectID, input.ClientMutationID,
		}, "\x00"))
		existing, found, err := loadAgentCapabilityAssetTx(tx, input.Scope.DomainProjectID, assetID, input.ResourceID, input.DisplayName)
		if err != nil {
			return err
		}
		if found {
			result = PublishedAgentCapabilityAsset{Asset: existing, ResourceID: input.ResourceID, Replayed: true}
			return nil
		}
		alreadyPublished, found, err := loadAgentCapabilityAssetByResourceTx(tx, input.Scope.DomainProjectID, input.ResourceID)
		if err != nil {
			return err
		}
		if found {
			if alreadyPublished.Title != input.DisplayName || alreadyPublished.Status != model.AssetVersionStatusConfirmed {
				return ErrAgentCapabilityAssetConflict
			}
			result = PublishedAgentCapabilityAsset{Asset: alreadyPublished, ResourceID: input.ResourceID, Replayed: true}
			return nil
		}
		versionID := agentAssetRecordID("capability-version", assetID)
		linkID := agentAssetRecordID("capability-link", assetID)
		representationID := agentAssetRecordID("capability-representation", assetID)
		payload, err := json.Marshal(map[string]string{"source": "agent_capability", "resourceId": resource.ID})
		if err != nil {
			return err
		}
		asset := model.Asset{
			ID: assetID, UserID: input.Scope.ActorUserID, Kind: resource.Kind, Category: model.AssetCategoryOther,
			Status: model.AssetVersionStatusConfirmed, PrimaryVersionID: versionID, Title: input.DisplayName,
			PayloadJSON: string(payload), CreatedAt: input.Now, UpdatedAt: input.Now,
		}
		version := model.AssetVersion{
			ID: versionID, AssetID: assetID, Version: 1, Status: model.AssetVersionStatusConfirmed,
			DefinitionJSON: string(payload), Note: "Published by approved agent capability", CreatedAt: input.Now, UpdatedAt: input.Now,
		}
		link := model.ProjectAssetLink{ID: linkID, ProjectID: input.Scope.DomainProjectID, AssetID: assetID, CreatedAt: input.Now}
		representation := model.AssetRepresentation{
			ID: representationID, AssetVersionID: versionID, ResourceID: resource.ID,
			MediaType: resource.Kind, Role: "primary", MetadataJSON: string(payload), CreatedAt: input.Now,
		}
		for _, create := range []func() error{
			func() error { return tx.Create(&asset).Error },
			func() error { return tx.Create(&version).Error },
			func() error { return tx.Create(&link).Error },
			func() error { return tx.Create(&representation).Error },
		} {
			if err := create(); err != nil {
				if isUniqueConstraintError(err) {
					return ErrAgentCapabilityAssetConflict
				}
				return err
			}
		}
		result = PublishedAgentCapabilityAsset{Asset: asset, ResourceID: resource.ID}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *Repository) AgentCapabilityAssetPublicationForScope(
	scope agentruntime.Scope,
	assetID string,
	resourceID string,
) (*AgentCapabilityAssetPublicationFact, error) {
	assetID = strings.TrimSpace(assetID)
	resourceID = strings.TrimSpace(resourceID)
	if scope.Validate() != nil || scope.DomainProjectID == "" || assetID == "" || resourceID == "" {
		return nil, ErrAgentCapabilityAssetInvalid
	}
	var fact AgentCapabilityAssetPublicationFact
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", assetID).Take(&fact.Asset).Error; err != nil {
			return err
		}
		if scope.TenantKind == agentruntime.TenantPersonal && fact.Asset.UserID != scope.ActorUserID {
			return ErrAgentCapabilityAssetForbidden
		}
		if fact.Asset.PrimaryVersionID == "" || fact.Asset.Status != model.AssetVersionStatusConfirmed {
			return ErrAgentCapabilityAssetConflict
		}
		if err := tx.Where("project_id = ? AND asset_id = ?", scope.DomainProjectID, assetID).
			Take(&fact.Link).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ? AND asset_id = ? AND status = ?", fact.Asset.PrimaryVersionID, assetID, model.AssetVersionStatusConfirmed).
			Take(&fact.Version).Error; err != nil {
			return err
		}
		if err := tx.Where("asset_version_id = ? AND resource_id = ? AND role = ?", fact.Version.ID, resourceID, "primary").
			Take(&fact.Representation).Error; err != nil {
			return err
		}
		resourceQuery := tx.Where("id = ? AND status = ?", resourceID, model.ResourceStatusReady)
		switch scope.TenantKind {
		case agentruntime.TenantPersonal:
			resourceQuery = resourceQuery.Where("user_id = ? AND team_id = ''", scope.TenantID)
		case agentruntime.TenantTeam:
			resourceQuery = resourceQuery.Where("team_id = ?", scope.TenantID)
		default:
			return ErrAgentCapabilityAssetForbidden
		}
		if err := resourceQuery.Take(&fact.Resource).Error; err != nil {
			return err
		}
		if fact.Asset.Kind != fact.Resource.Kind || fact.Representation.MediaType != fact.Resource.Kind ||
			(fact.Resource.Kind != "image" && fact.Resource.Kind != "video" && fact.Resource.Kind != "audio") {
			return ErrAgentCapabilityAssetConflict
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &fact, nil
}

func requireActiveCurrentAgentRunTx(tx *gorm.DB, scope agentruntime.Scope) error {
	var run model.AgentRun
	err := tx.Table("agent_runs").Select("agent_runs.*").
		Joins("JOIN agent_threads ON agent_threads.id = agent_runs.thread_id").
		Where(`agent_runs.id = ? AND agent_runs.thread_id = ? AND agent_runs.actor_user_id = ?
			AND agent_threads.tenant_kind = ? AND agent_threads.tenant_id = ?
			AND agent_threads.created_by_user_id = ? AND agent_threads.domain_project_id = ?
			AND agent_threads.canvas_id = ?`,
			scope.RunID, scope.ThreadID, scope.ActorUserID, scope.TenantKind, scope.TenantID,
			scope.ActorUserID, scope.DomainProjectID, scope.CanvasID).
		Clauses(clause.Locking{Strength: "UPDATE"}).First(&run).Error
	if err != nil {
		return err
	}
	if (run.Status != agentruntime.RunRunning && run.Status != agentruntime.RunWaitingTool) ||
		run.RuntimeVersion != agentruntime.CurrentRuntimeVersion ||
		run.PolicyVersion != agentruntime.CurrentPolicyVersion ||
		run.ToolSchemaVersion != agentruntime.CurrentToolSchemaVersion {
		return ErrAgentCapabilityAssetConflict
	}
	return nil
}

func agentCapabilityOwnedResourceTx(tx *gorm.DB, scope agentruntime.Scope, resourceID string) (model.Resource, error) {
	var resource model.Resource
	query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND status = ?", resourceID, model.ResourceStatusReady)
	switch scope.TenantKind {
	case agentruntime.TenantPersonal:
		query = query.Where("user_id = ? AND team_id = ''", scope.TenantID)
	case agentruntime.TenantTeam:
		query = query.Where("team_id = ?", scope.TenantID)
	default:
		return resource, ErrAgentCapabilityAssetForbidden
	}
	if err := query.Take(&resource).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return resource, ErrAgentCapabilityAssetForbidden
		}
		return resource, err
	}
	if resource.Kind != "image" && resource.Kind != "video" && resource.Kind != "audio" {
		return resource, ErrAgentCapabilityAssetInvalid
	}
	return resource, nil
}

func loadAgentCapabilityAssetTx(tx *gorm.DB, projectID string, assetID string, resourceID string, displayName string) (model.Asset, bool, error) {
	var asset model.Asset
	err := tx.Table("assets").Select("assets.*").
		Joins("JOIN project_asset_links ON project_asset_links.asset_id = assets.id").
		Joins("JOIN asset_versions ON asset_versions.id = assets.primary_version_id").
		Joins("JOIN asset_representations ON asset_representations.asset_version_id = asset_versions.id").
		Where("assets.id = ? AND project_asset_links.project_id = ?", assetID, projectID).Take(&asset).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Asset{}, false, nil
	}
	if err != nil {
		return model.Asset{}, false, err
	}
	var count int64
	if err := tx.Model(&model.AssetRepresentation{}).
		Where("asset_version_id = ? AND resource_id = ? AND role = ?", asset.PrimaryVersionID, resourceID, "primary").
		Count(&count).Error; err != nil {
		return model.Asset{}, false, err
	}
	if count != 1 || asset.Title != displayName || asset.Status != model.AssetVersionStatusConfirmed {
		return model.Asset{}, false, ErrAgentCapabilityAssetConflict
	}
	return asset, true, nil
}

func loadAgentCapabilityAssetByResourceTx(tx *gorm.DB, projectID string, resourceID string) (model.Asset, bool, error) {
	var asset model.Asset
	err := tx.Table("assets").Select("assets.*").
		Joins("JOIN project_asset_links ON project_asset_links.asset_id = assets.id").
		Joins("JOIN asset_versions ON asset_versions.id = assets.primary_version_id").
		Joins("JOIN asset_representations ON asset_representations.asset_version_id = asset_versions.id").
		Where("project_asset_links.project_id = ? AND asset_representations.resource_id = ? AND asset_representations.role = ?", projectID, resourceID, "primary").
		Take(&asset).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Asset{}, false, nil
	}
	if err != nil {
		return model.Asset{}, false, err
	}
	return asset, true, nil
}
