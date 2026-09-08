package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

type agentCapabilityExecutionError struct {
	Code string
	Err  error
	// 只有任务与账单均确认终态的失败才可关闭已中断工具；协议或结算错误不能冒充结果。
	Settled bool
}

func (failure *agentCapabilityExecutionError) Error() string {
	if failure == nil || failure.Err == nil {
		return "agent capability execution failed"
	}
	return failure.Err.Error()
}

func (failure *agentCapabilityExecutionError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.Err
}

func failAgentCapability(code string, message string) error {
	return &agentCapabilityExecutionError{Code: code, Err: errors.New(message)}
}

type agentCanvasReadCapabilityExecutor struct {
	service *Service
}

type agentAssetsReadCapabilityExecutor struct {
	service *Service
}

type agentProjectReadCapabilityExecutor struct {
	service *Service
}

type agentSkillsLoadCapabilityExecutor struct {
	service *Service
}

func (executor agentSkillsLoadCapabilityExecutor) Execute(ctx context.Context, scope agentruntime.Scope, call agentruntime.ToolCallDecision) (agentruntime.ToolExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return agentruntime.ToolExecutionResult{}, err
	}
	if executor.service == nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_unavailable", "skills.load executor is unavailable")
	}
	if err := scope.Validate(); err != nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_scope_invalid", err.Error())
	}
	decoded, err := agentruntime.DecodeCapabilityArguments(call.ToolName, call.Arguments)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_arguments_invalid", "skills.load arguments are invalid")
	}
	arguments, ok := decoded.(agentruntime.SkillsLoadArguments)
	if !ok || call.ToolName != agentruntime.ToolSkillsLoad || call.ActionVersion != 1 {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_arguments_invalid", "skills.load call identity is invalid")
	}
	state, err := executor.service.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "skill_run_unavailable", Err: err}
	}
	var selected *agentruntime.SkillSelection
	for index := range state.Configuration.Skills {
		candidate := &state.Configuration.Skills[index]
		if candidate.Dir == arguments.SkillDir && candidate.Version == arguments.Version && candidate.Checksum == arguments.Checksum {
			selected = candidate
			break
		}
	}
	if selected == nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("skill_not_authorized", "skills.load requested version is not authorized for this run")
	}
	published, err := executor.service.repo.PublishedSkillVersionByDir(arguments.SkillDir, arguments.Version)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "skill_version_unavailable", Err: err}
	}
	if published.Status != model.SkillStatusPublished || published.Checksum != selected.Checksum || published.Instructions != selected.Instructions ||
		published.Name != selected.Name || published.Description != selected.Description {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("skill_version_conflict", "skills.load published facts conflict with the frozen run selection")
	}
	result, err := agentruntime.NewToolExecutionResult(agentruntime.ToolSkillsLoad, agentruntime.SkillsLoadResult{
		SkillDir: selected.Dir, Version: selected.Version, Checksum: selected.Checksum, Instructions: selected.Instructions,
	})
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "capability_result_invalid", Err: err}
	}
	return result, nil
}

func (executor agentAssetsReadCapabilityExecutor) Execute(ctx context.Context, scope agentruntime.Scope, call agentruntime.ToolCallDecision) (agentruntime.ToolExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return agentruntime.ToolExecutionResult{}, err
	}
	if executor.service == nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_unavailable", "assets.read executor is unavailable")
	}
	if err := scope.Validate(); err != nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_scope_invalid", err.Error())
	}
	decoded, err := agentruntime.DecodeCapabilityArguments(call.ToolName, call.Arguments)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_arguments_invalid", "assets.read arguments are invalid")
	}
	arguments, ok := decoded.(agentruntime.AssetsReadArguments)
	if !ok || call.ToolName != agentruntime.ToolAssetsRead || call.ActionVersion != 1 {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_arguments_invalid", "assets.read call identity is invalid")
	}
	if arguments.DomainProjectID != scope.DomainProjectID || scope.DomainProjectID == "" {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_scope_conflict", "assets.read project scope is stale")
	}
	canvas, _, err := executor.service.canvasAccess(scope.ActorUserID, scope.CanvasID)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "capability_ownership_forbidden", Err: err}
	}
	if canvas.ProjectID != scope.DomainProjectID {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_scope_conflict", "assets.read canvas project scope is stale")
	}
	facts, err := executor.service.repo.AgentCapabilityResourcesForScope(scope, arguments.ResourceIDs, arguments.Limit)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "capability_resource_query_failed", Err: err}
	}
	if len(arguments.ResourceIDs) > 0 && len(facts) != len(arguments.ResourceIDs) {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_ownership_forbidden", "one or more requested assets are outside the authorized project scope")
	}
	resources := make([]agentruntime.AssetResourceResult, 0, len(facts))
	for _, fact := range facts {
		resources = append(resources, agentruntime.AssetResourceResult{
			ResourceID: fact.ResourceID, Name: fact.Name, Kind: agentruntime.MediaKind(fact.Kind), MimeType: fact.MimeType,
			Width: fact.Width, Height: fact.Height, DurationMS: fact.DurationMS,
		})
	}
	result, err := agentruntime.NewToolExecutionResult(agentruntime.ToolAssetsRead, agentruntime.AssetsReadResult{
		DomainProjectID: scope.DomainProjectID, Resources: resources,
	})
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "capability_result_invalid", Err: err}
	}
	return result, nil
}

func (executor agentProjectReadCapabilityExecutor) Execute(ctx context.Context, scope agentruntime.Scope, call agentruntime.ToolCallDecision) (agentruntime.ToolExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return agentruntime.ToolExecutionResult{}, err
	}
	if executor.service == nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_unavailable", "project.read executor is unavailable")
	}
	if err := scope.Validate(); err != nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_scope_invalid", err.Error())
	}
	decoded, err := agentruntime.DecodeCapabilityArguments(call.ToolName, call.Arguments)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_arguments_invalid", "project.read arguments are invalid")
	}
	arguments, ok := decoded.(agentruntime.ProjectReadArguments)
	if !ok || call.ToolName != agentruntime.ToolProjectRead || call.ActionVersion != 1 {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_arguments_invalid", "project.read call identity is invalid")
	}
	if arguments.DomainProjectID != scope.DomainProjectID || arguments.CanvasID != scope.CanvasID {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_scope_conflict", "project.read scope is stale")
	}
	canvas, _, err := executor.service.canvasAccess(scope.ActorUserID, scope.CanvasID)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "capability_ownership_forbidden", Err: err}
	}
	if canvas.ProjectID != scope.DomainProjectID {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_scope_conflict", "project.read canvas project scope is stale")
	}
	project, err := executor.service.repo.ProjectReadableForUser(scope.ActorUserID, scope.DomainProjectID)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "capability_ownership_forbidden", Err: err}
	}
	links, err := executor.service.repo.ProjectCanvasUnitLinks(project.ID)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "project_facts_unavailable", Err: err}
	}
	allUnits, err := executor.service.repo.ProjectUnits(project.ID)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "project_facts_unavailable", Err: err}
	}
	allShots, err := executor.service.repo.ProjectShots(project.ID)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "project_facts_unavailable", Err: err}
	}
	linkRoles := make(map[string]string)
	for _, link := range links {
		if link.CanvasID == scope.CanvasID {
			linkRoles[link.UnitID] = link.Role
		}
	}
	shotsByUnit := make(map[string][]agentruntime.ProjectReadShot)
	for _, shot := range allShots {
		if _, linked := linkRoles[shot.UnitID]; !linked {
			continue
		}
		shotsByUnit[shot.UnitID] = append(shotsByUnit[shot.UnitID], agentruntime.ProjectReadShot{
			ShotID: shot.ID, Title: shot.Title, Description: shot.Description, Position: shot.Position,
			DurationMS: shot.DurationMs, Status: shot.Status,
		})
	}
	units := make([]agentruntime.ProjectReadUnit, 0, len(linkRoles))
	for _, unit := range allUnits {
		linkRole, linked := linkRoles[unit.ID]
		if !linked {
			continue
		}
		unitShots := shotsByUnit[unit.ID]
		if unitShots == nil {
			unitShots = []agentruntime.ProjectReadShot{}
		}
		units = append(units, agentruntime.ProjectReadUnit{
			UnitID: unit.ID, Kind: string(unit.Kind), Title: unit.Title, SourceText: unit.SourceText,
			Status: string(unit.Status), Position: unit.Position, LinkRole: linkRole, Shots: unitShots,
		})
	}
	assets, err := executor.service.repo.ProjectAssets(scope.ActorUserID, project.ID)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "project_facts_unavailable", Err: err}
	}
	characters := make([]agentruntime.ProjectReadCharacter, 0)
	for _, asset := range assets {
		if asset.Category != model.AssetCategoryCharacter || asset.Status != model.AssetVersionStatusConfirmed {
			continue
		}
		if strings.TrimSpace(asset.PrimaryVersionID) == "" {
			return agentruntime.ToolExecutionResult{}, failAgentCapability("project_character_invalid", "confirmed character is missing its primary version")
		}
		version, versionErr := executor.service.repo.AssetVersion(asset.PrimaryVersionID)
		if versionErr != nil {
			return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "project_character_invalid", Err: versionErr}
		}
		if version.AssetID != asset.ID || version.Status != model.AssetVersionStatusConfirmed {
			return agentruntime.ToolExecutionResult{}, failAgentCapability("project_character_invalid", "confirmed character primary version conflicts with the project asset")
		}
		definition := json.RawMessage(version.DefinitionJSON)
		if !json.Valid(definition) {
			return agentruntime.ToolExecutionResult{}, failAgentCapability("project_character_invalid", "confirmed character definition is invalid")
		}
		storedRepresentations, representationErr := executor.service.repo.AssetRepresentations(version.ID)
		if representationErr != nil {
			return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "project_character_invalid", Err: representationErr}
		}
		representations := make([]agentruntime.ProjectReadRepresentation, 0, len(storedRepresentations))
		for _, representation := range storedRepresentations {
			if strings.TrimSpace(representation.ResourceID) == "" {
				continue
			}
			representations = append(representations, agentruntime.ProjectReadRepresentation{
				RepresentationID: representation.ID, ResourceID: representation.ResourceID,
				MediaType: representation.MediaType, Role: representation.Role,
			})
		}
		characters = append(characters, agentruntime.ProjectReadCharacter{
			AssetID: asset.ID, Name: asset.Title, VersionID: version.ID, Version: version.Version,
			Status: string(version.Status), Definition: definition, Representations: representations,
		})
	}
	result, err := agentruntime.NewToolExecutionResult(agentruntime.ToolProjectRead, agentruntime.ProjectReadResult{
		DomainProjectID: project.ID, CanvasID: canvas.ID, Name: project.Name, ProjectType: project.Type,
		AspectRatio: project.AspectRatio, Revision: project.Revision, Units: units, Characters: characters,
	})
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "capability_result_invalid", Err: err}
	}
	return result, nil
}

func (executor agentCanvasReadCapabilityExecutor) Execute(ctx context.Context, scope agentruntime.Scope, call agentruntime.ToolCallDecision) (agentruntime.ToolExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return agentruntime.ToolExecutionResult{}, err
	}
	if executor.service == nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_unavailable", "canvas.read executor is unavailable")
	}
	if err := scope.Validate(); err != nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_scope_invalid", err.Error())
	}
	decoded, err := agentruntime.DecodeCapabilityArguments(call.ToolName, call.Arguments)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_arguments_invalid", "canvas.read arguments are invalid")
	}
	arguments, ok := decoded.(agentruntime.CanvasReadArguments)
	if !ok || call.ToolName != agentruntime.ToolCanvasRead || call.ActionVersion != 1 {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_arguments_invalid", "canvas.read call identity is invalid")
	}
	if arguments.CanvasID != scope.CanvasID || !arguments.IncludeViewport {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_scope_conflict", "canvas.read scope conflicts with the requested canvas facts")
	}
	canvas, _, err := executor.service.canvasAccess(scope.ActorUserID, scope.CanvasID)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "capability_ownership_forbidden", Err: err}
	}
	if canvas.ProjectID != scope.DomainProjectID {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_scope_conflict", "canvas.read project scope is stale")
	}
	result, err := decodeScopedCanvasReadResult(canvas.ID, canvas.Revision, canvas.PayloadJSON, arguments.SelectedNodeIDs)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, err
	}
	callableModels, err := executor.service.agentRuntimeCallableModels(scope.ActorUserID)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "canvas_model_facts_unavailable", Err: err}
	}
	result.DomainProjectID = scope.DomainProjectID
	result.CallableModels, err = canvasReadCallableModelFacts(callableModels)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "canvas_model_facts_invalid", Err: err}
	}
	execution, err := agentruntime.NewToolExecutionResult(agentruntime.ToolCanvasRead, result)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "capability_result_invalid", Err: err}
	}
	return execution, nil
}

func canvasReadCallableModelFacts(values []agentRuntimeCallableModelFact) ([]agentruntime.CanvasCallableModelFact, error) {
	result := make([]agentruntime.CanvasCallableModelFact, 0, len(values))
	for _, value := range values {
		// canvas.read publishes only models that media.generate can invoke. The
		// administrator-selected vision model is frozen separately in the Run
		// configuration and is consumed exclusively by vision.analyze.
		if value.Capability == "vision" {
			continue
		}
		priceTiers := make([]agentruntime.CanvasCallableModelPriceTier, len(value.PriceTiers))
		for index, tier := range value.PriceTiers {
			priceTiers[index] = agentruntime.CanvasCallableModelPriceTier{
				Resolution: tier.Resolution, InputVariant: tier.InputVariant, UsageMetric: tier.UsageMetric,
				IncludedQuantity: tier.IncludedQuantity, UnitPriceMicrocredits: tier.UnitPriceMicrocredits,
			}
		}
		var providerCapabilities json.RawMessage
		if value.ProviderCapabilities != nil {
			encoded, err := json.Marshal(value.ProviderCapabilities)
			if err != nil {
				return nil, err
			}
			providerCapabilities = encoded
		}
		result = append(result, agentruntime.CanvasCallableModelFact{
			ChannelID: value.ChannelID, ModelRecordID: value.ModelRecordID, ModelKey: value.Model,
			DisplayName: value.DisplayName, Capability: value.Capability, BillingMode: value.BillingMode,
			PriceStrategy: value.PriceStrategy, UnitPriceMicrocredits: value.UnitPriceMicrocredits,
			PriceTiers: priceTiers, ProviderCapabilities: providerCapabilities,
		})
	}
	return result, nil
}

func decodeScopedCanvasReadResult(canvasID string, revision int64, payload string, selectedNodeIDs []string) (agentruntime.CanvasReadResult, error) {
	var document struct {
		Nodes       []json.RawMessage `json:"nodes"`
		Connections []json.RawMessage `json:"connections"`
		Viewport    *struct {
			X *float64 `json:"x"`
			Y *float64 `json:"y"`
			K *float64 `json:"k"`
		} `json:"viewport"`
	}
	if err := json.Unmarshal([]byte(payload), &document); err != nil || document.Nodes == nil || document.Connections == nil || document.Viewport == nil || document.Viewport.X == nil || document.Viewport.Y == nil || document.Viewport.K == nil {
		return agentruntime.CanvasReadResult{}, failAgentCapability("canvas_facts_invalid", "canvas.read stored canvas facts are invalid")
	}
	nodeIDs := make(map[string]struct{}, len(document.Nodes))
	for _, node := range document.Nodes {
		var identity struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(node, &identity) != nil || strings.TrimSpace(identity.ID) == "" {
			return agentruntime.CanvasReadResult{}, failAgentCapability("canvas_facts_invalid", "canvas.read stored node identity is invalid")
		}
		if _, duplicate := nodeIDs[identity.ID]; duplicate {
			return agentruntime.CanvasReadResult{}, failAgentCapability("canvas_facts_invalid", "canvas.read stored node identity is duplicated")
		}
		nodeIDs[identity.ID] = struct{}{}
	}
	for _, selectedNodeID := range selectedNodeIDs {
		if _, found := nodeIDs[selectedNodeID]; !found {
			return agentruntime.CanvasReadResult{}, failAgentCapability("canvas_selection_stale", fmt.Sprintf("canvas.read selected node %q no longer exists", selectedNodeID))
		}
	}
	return agentruntime.CanvasReadResult{
		CanvasID: canvasID, Revision: revision,
		Nodes: append([]json.RawMessage{}, document.Nodes...), Edges: append([]json.RawMessage{}, document.Connections...),
		SelectedNodeIDs: append([]string{}, selectedNodeIDs...),
		Viewport:        agentruntime.CanvasViewport{X: *document.Viewport.X, Y: *document.Viewport.Y, Zoom: *document.Viewport.K},
		CallableModels:  []agentruntime.CanvasCallableModelFact{},
	}, nil
}
