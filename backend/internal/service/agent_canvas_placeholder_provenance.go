package service

import (
	"encoding/json"
	"errors"

	"infinite-canvas/backend/internal/agentruntime"
)

func stampAgentCanvasPlaceholderProvenance(scope agentruntime.Scope, decision agentruntime.ModelDecision) (agentruntime.ModelDecision, error) {
	if decision.ToolCall == nil || decision.ToolCall.ToolName != agentruntime.ToolCanvasApplyOps {
		return decision, nil
	}
	if err := scope.Validate(); err != nil {
		return agentruntime.ModelDecision{}, err
	}
	decoded, err := agentruntime.DecodeCapabilityArguments(decision.ToolCall.ToolName, decision.ToolCall.Arguments)
	if err != nil {
		return agentruntime.ModelDecision{}, errors.Join(errors.New("agent canvas placeholder arguments are invalid"), err)
	}
	arguments, ok := decoded.(agentruntime.CanvasApplyOpsArguments)
	if !ok {
		return agentruntime.ModelDecision{}, errors.New("agent canvas placeholder arguments have an invalid capability type")
	}

	changed := false
	for index := range arguments.Operations {
		operation := &arguments.Operations[index]
		switch operation.Type {
		case agentruntime.CanvasOperationAddNode:
			if operation.Node == nil {
				continue
			}
			metadata, stamped, stampErr := stampAgentLoadingMetadata(operation.Node.Metadata, scope.RunID)
			if stampErr != nil {
				return agentruntime.ModelDecision{}, stampErr
			}
			if stamped {
				operation.Node.Metadata = metadata
				changed = true
			}
		case agentruntime.CanvasOperationUpdateNode:
			patch := map[string]json.RawMessage{}
			if err := json.Unmarshal(operation.Patch, &patch); err != nil {
				return agentruntime.ModelDecision{}, errors.Join(errors.New("agent canvas node patch is invalid"), err)
			}
			metadata, exists := patch["metadata"]
			if !exists {
				continue
			}
			metadata, stamped, stampErr := stampAgentLoadingMetadata(metadata, scope.RunID)
			if stampErr != nil {
				return agentruntime.ModelDecision{}, stampErr
			}
			if stamped {
				patch["metadata"] = metadata
				operation.Patch, err = json.Marshal(patch)
				if err != nil {
					return agentruntime.ModelDecision{}, err
				}
				changed = true
			}
		}
	}
	if !changed {
		return decision, nil
	}

	argumentsJSON, err := json.Marshal(arguments)
	if err != nil {
		return agentruntime.ModelDecision{}, err
	}
	toolCall := *decision.ToolCall
	toolCall.Arguments = argumentsJSON
	decision.ToolCall = &toolCall
	return decision, nil
}

func stampAgentLoadingMetadata(raw json.RawMessage, runID string) (json.RawMessage, bool, error) {
	metadata := map[string]json.RawMessage{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &metadata); err != nil {
			return nil, false, errors.Join(errors.New("agent canvas placeholder metadata is invalid"), err)
		}
	}
	statusRaw, exists := metadata["status"]
	if !exists {
		return raw, false, nil
	}
	var status agentruntime.CanvasNodeStatus
	if err := json.Unmarshal(statusRaw, &status); err != nil {
		return nil, false, errors.Join(errors.New("agent canvas placeholder status is invalid"), err)
	}
	if status != agentruntime.CanvasNodeLoading {
		return raw, false, nil
	}
	runIDJSON, err := json.Marshal(runID)
	if err != nil {
		return nil, false, err
	}
	metadata["agentRunId"] = runIDJSON
	stamped, err := json.Marshal(metadata)
	if err != nil {
		return nil, false, err
	}
	return stamped, true, nil
}
