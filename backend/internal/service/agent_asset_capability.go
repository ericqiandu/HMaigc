package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/repository"
)

type agentAssetsPublishCapabilityExecutor struct {
	service *Service
}

func (executor agentAssetsPublishCapabilityExecutor) Execute(ctx context.Context, scope agentruntime.Scope, call agentruntime.ToolCallDecision) (agentruntime.ToolExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return agentruntime.ToolExecutionResult{}, err
	}
	if executor.service == nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_unavailable", "assets.publish executor is unavailable")
	}
	decoded, err := agentruntime.DecodeCapabilityArguments(call.ToolName, call.Arguments)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_arguments_invalid", "assets.publish arguments are invalid")
	}
	arguments, ok := decoded.(agentruntime.AssetsPublishArguments)
	if !ok || call.ToolName != agentruntime.ToolAssetsPublish || call.ActionVersion != 1 {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_arguments_invalid", "assets.publish call identity is invalid")
	}
	if arguments.DomainProjectID != scope.DomainProjectID {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_scope_conflict", "assets.publish project scope is stale")
	}
	record, _, err := executor.service.authorizedAgentCapabilityRecord(scope, call, agentruntime.ToolAssetsPublish, "asset")
	if err != nil {
		return agentruntime.ToolExecutionResult{}, err
	}
	if record.Status == agentruntime.ToolCallSucceeded {
		result, decodeErr := agentruntime.DecodeCapabilityResult(agentruntime.ToolAssetsPublish, json.RawMessage(record.OutputJSON))
		if decodeErr != nil {
			return agentruntime.ToolExecutionResult{}, failAgentCapability("asset_receipt_invalid", "assets.publish stored receipt is invalid")
		}
		return agentruntime.NewToolExecutionResult(agentruntime.ToolAssetsPublish, result)
	}
	canvas, _, err := executor.service.canvasAccess(scope.ActorUserID, scope.CanvasID)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "asset_ownership_forbidden", Err: err}
	}
	if canvas.ProjectID != arguments.DomainProjectID {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_scope_conflict", "assets.publish canvas project scope is stale")
	}
	published, err := executor.service.repo.PublishAgentCapabilityAsset(repository.PublishAgentCapabilityAssetInput{
		Context: ctx, Scope: scope, ResourceID: arguments.ResourceID, DisplayName: arguments.DisplayName,
		ClientMutationID: arguments.ClientMutationID, Now: time.Now().UTC(),
	})
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrAgentCapabilityAssetForbidden):
			return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "asset_ownership_forbidden", Err: err}
		case errors.Is(err, repository.ErrAgentCapabilityAssetConflict):
			return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "asset_publication_conflict", Err: err}
		default:
			return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "asset_publication_failed", Err: err}
		}
	}
	return agentruntime.NewToolExecutionResult(agentruntime.ToolAssetsPublish, agentruntime.AssetsPublishResult{
		DomainProjectID: arguments.DomainProjectID, ResourceID: arguments.ResourceID,
		AssetID: published.Asset.ID, ClientMutationID: arguments.ClientMutationID,
	})
}
