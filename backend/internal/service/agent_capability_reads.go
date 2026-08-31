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
	if strings.TrimSpace(scope.DomainProjectID) == "" || canvas.ProjectID != scope.DomainProjectID {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_scope_conflict", "canvas.read project scope is stale")
	}
	result, err := decodeScopedCanvasReadResult(canvas.ID, canvas.Revision, canvas.PayloadJSON, arguments.SelectedNodeIDs)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, err
	}
	execution, err := agentruntime.NewToolExecutionResult(agentruntime.ToolCanvasRead, result)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "capability_result_invalid", Err: err}
	}
	return execution, nil
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
	}, nil
}
