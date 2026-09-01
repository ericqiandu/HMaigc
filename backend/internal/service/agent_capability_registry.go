package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"infinite-canvas/backend/internal/agentruntime"
)

type AgentCapabilityExecutor interface {
	Execute(context.Context, agentruntime.Scope, agentruntime.ToolCallDecision) (agentruntime.ToolExecutionResult, error)
}

type agentCapabilityDescriptor struct {
	Name            agentruntime.ToolName      `json:"name"`
	ActionVersion   int                        `json:"actionVersion"`
	RiskLevel       agentruntime.ToolRiskLevel `json:"riskLevel"`
	RequiredAccess  agentruntime.AccessLevel   `json:"requiredAccess"`
	ArgumentsSchema json.RawMessage            `json:"argumentsSchema"`
	ResultSchema    json.RawMessage            `json:"resultSchema"`
}

type registeredAgentCapability struct {
	descriptor agentCapabilityDescriptor
	executor   AgentCapabilityExecutor
}

type agentCapabilityRegistry struct {
	ordered []registeredAgentCapability
	byName  map[agentruntime.ToolName]registeredAgentCapability
}

func newAgentCapabilityRegistry(service *Service) (*agentCapabilityRegistry, error) {
	definitions := []struct {
		name            agentruntime.ToolName
		argumentsSchema string
		resultSchema    string
	}{
		{agentruntime.ToolCanvasRead, canvasReadArgumentsSchema, canvasReadResultSchema},
		{agentruntime.ToolCanvasApplyOps, canvasApplyOpsArgumentsSchema, canvasApplyOpsResultSchema},
		{agentruntime.ToolAssetsRead, assetsReadArgumentsSchema, assetsReadResultSchema},
		{agentruntime.ToolAssetsPublish, assetsPublishArgumentsSchema, assetsPublishResultSchema},
		{agentruntime.ToolMediaGenerate, mediaGenerateArgumentsSchema, mediaGenerateResultSchema},
		{agentruntime.ToolSkillsLoad, skillsLoadArgumentsSchema, skillsLoadResultSchema},
	}
	registry := &agentCapabilityRegistry{
		ordered: make([]registeredAgentCapability, 0, len(definitions)),
		byName:  make(map[agentruntime.ToolName]registeredAgentCapability, len(definitions)),
	}
	for _, definition := range definitions {
		policy, found := agentruntime.ToolPolicyForSchema(definition.name, agentruntime.CurrentToolSchemaVersion)
		if !found || !json.Valid([]byte(definition.argumentsSchema)) || !json.Valid([]byte(definition.resultSchema)) {
			return nil, errors.New("agent capability registry definition is invalid")
		}
		registered := registeredAgentCapability{
			descriptor: agentCapabilityDescriptor{
				Name: definition.name, ActionVersion: 1, RiskLevel: policy.RiskLevel, RequiredAccess: policy.RequiredAccess,
				ArgumentsSchema: json.RawMessage(definition.argumentsSchema), ResultSchema: json.RawMessage(definition.resultSchema),
			},
			executor: unconnectedAgentCapabilityExecutor{name: definition.name},
		}
		registry.ordered = append(registry.ordered, registered)
		registry.byName[definition.name] = registered
	}
	if service != nil {
		connections := []struct {
			name     agentruntime.ToolName
			executor AgentCapabilityExecutor
		}{
			{name: agentruntime.ToolCanvasRead, executor: agentCanvasReadCapabilityExecutor{service: service}},
			{name: agentruntime.ToolCanvasApplyOps, executor: agentCanvasApplyOpsCapabilityExecutor{service: service}},
			{name: agentruntime.ToolAssetsRead, executor: agentAssetsReadCapabilityExecutor{service: service}},
			{name: agentruntime.ToolAssetsPublish, executor: agentAssetsPublishCapabilityExecutor{service: service}},
			{name: agentruntime.ToolMediaGenerate, executor: agentMediaGenerateCapabilityExecutor{service: service}},
			{name: agentruntime.ToolSkillsLoad, executor: agentSkillsLoadCapabilityExecutor{service: service}},
		}
		for _, connection := range connections {
			if err := registry.setExecutor(connection.name, connection.executor); err != nil {
				return nil, err
			}
		}
	}
	return registry, nil
}

func (registry *agentCapabilityRegistry) setExecutor(name agentruntime.ToolName, executor AgentCapabilityExecutor) error {
	if registry == nil {
		return errors.New("agent capability registry is unavailable")
	}
	registered, found := registry.byName[name]
	if !found || executor == nil {
		return errors.New("agent capability executor connection is invalid")
	}
	registered.executor = executor
	registry.byName[name] = registered
	for index := range registry.ordered {
		if registry.ordered[index].descriptor.Name == name {
			registry.ordered[index] = registered
			return nil
		}
	}
	return errors.New("agent capability registry order is inconsistent")
}

func (registry *agentCapabilityRegistry) Descriptors() []agentCapabilityDescriptor {
	if registry == nil {
		return nil
	}
	result := make([]agentCapabilityDescriptor, 0, len(registry.ordered))
	for _, registered := range registry.ordered {
		descriptor := registered.descriptor
		descriptor.ArgumentsSchema = append(json.RawMessage(nil), descriptor.ArgumentsSchema...)
		descriptor.ResultSchema = append(json.RawMessage(nil), descriptor.ResultSchema...)
		result = append(result, descriptor)
	}
	return result
}

func (registry *agentCapabilityRegistry) Execute(ctx context.Context, scope agentruntime.Scope, call agentruntime.ToolCallDecision) (agentruntime.ToolExecutionResult, error) {
	if registry == nil {
		return agentruntime.ToolExecutionResult{}, errors.New("agent capability registry is unavailable")
	}
	registered, found := registry.byName[call.ToolName]
	if !found || call.ActionVersion != registered.descriptor.ActionVersion {
		return agentruntime.ToolExecutionResult{}, errors.New("agent capability is not registered")
	}
	return registered.executor.Execute(ctx, scope, call)
}

type unconnectedAgentCapabilityExecutor struct {
	name agentruntime.ToolName
}

func (executor unconnectedAgentCapabilityExecutor) Execute(context.Context, agentruntime.Scope, agentruntime.ToolCallDecision) (agentruntime.ToolExecutionResult, error) {
	return agentruntime.ToolExecutionResult{}, fmt.Errorf("agent capability %s executor is not connected", executor.name)
}

const canvasReadArgumentsSchema = `{"type":"object","additionalProperties":false,"properties":{"canvasId":{"type":"string"},"selectedNodeIds":{"type":"array","items":{"type":"string"}},"includeViewport":{"type":"boolean"}},"required":["canvasId","selectedNodeIds","includeViewport"]}`
const canvasReadResultSchema = `{"type":"object","additionalProperties":false,"properties":{"canvasId":{"type":"string"},"revision":{"type":"integer"},"nodes":{"type":"array","items":{"type":"object"}},"edges":{"type":"array","items":{"type":"object"}},"selectedNodeIds":{"type":"array","items":{"type":"string"}},"viewport":{"type":"object"}},"required":["canvasId","revision","nodes","edges","selectedNodeIds","viewport"]}`
const canvasApplyOpsArgumentsSchema = `{"type":"object","additionalProperties":false,"properties":{"canvasId":{"type":"string"},"baseRevision":{"type":"integer"},"clientMutationId":{"type":"string"},"operations":{"type":"array","items":{"type":"object"}}},"required":["canvasId","baseRevision","clientMutationId","operations"]}`
const canvasApplyOpsResultSchema = `{"type":"object","additionalProperties":false,"properties":{"canvasId":{"type":"string"},"baseRevision":{"type":"integer"},"committedRevision":{"type":"integer"},"clientMutationId":{"type":"string"},"proposalHash":{"type":"string"},"appliedOperationIds":{"type":"array","items":{"type":"string"}},"evidence":{"type":"object","additionalProperties":false,"properties":{"addedNodeIds":{"type":"array","items":{"type":"string"}},"updatedNodeIds":{"type":"array","items":{"type":"string"}},"deletedNodeIds":{"type":"array","items":{"type":"string"}},"upsertedConnectionIds":{"type":"array","items":{"type":"string"}},"deletedConnectionIds":{"type":"array","items":{"type":"string"}},"selectedNodeIds":{"type":"array","items":{"type":"string"}},"viewportApplied":{"type":"boolean"}},"required":["addedNodeIds","updatedNodeIds","deletedNodeIds","upsertedConnectionIds","deletedConnectionIds","selectedNodeIds","viewportApplied"]}},"required":["canvasId","baseRevision","committedRevision","clientMutationId","proposalHash","appliedOperationIds","evidence"]}`
const assetsReadArgumentsSchema = `{"type":"object","additionalProperties":false,"properties":{"domainProjectId":{"type":"string"},"resourceIds":{"type":"array","items":{"type":"string"}},"limit":{"type":"integer"}},"required":["domainProjectId","resourceIds","limit"]}`
const assetsReadResultSchema = `{"type":"object","additionalProperties":false,"properties":{"domainProjectId":{"type":"string"},"resources":{"type":"array","items":{"type":"object"}}},"required":["domainProjectId","resources"]}`
const assetsPublishArgumentsSchema = `{"type":"object","additionalProperties":false,"properties":{"resourceId":{"type":"string"},"domainProjectId":{"type":"string"},"displayName":{"type":"string"},"clientMutationId":{"type":"string"}},"required":["resourceId","domainProjectId","displayName","clientMutationId"]}`
const assetsPublishResultSchema = `{"type":"object","additionalProperties":false,"properties":{"domainProjectId":{"type":"string"},"resourceId":{"type":"string"},"assetId":{"type":"string"},"clientMutationId":{"type":"string"}},"required":["domainProjectId","resourceId","assetId","clientMutationId"]}`
const mediaGenerateArgumentsSchema = `{"type":"object","additionalProperties":false,"properties":{"mediaKind":{"type":"string","enum":["image","video","audio"]},"modelRecordId":{"type":"string"},"modelKey":{"type":"string"},"parameters":{"type":"object"},"sourceResourceIds":{"type":"array","items":{"type":"string"}},"targetCanvasNodeId":{"type":"string"},"clientRequestId":{"type":"string"}},"required":["mediaKind","modelRecordId","modelKey","parameters","sourceResourceIds","targetCanvasNodeId","clientRequestId"]}`
const mediaGenerateResultSchema = `{"type":"object","additionalProperties":false,"properties":{"taskId":{"type":"string"},"billingOrderId":{"type":"string"},"mediaKind":{"type":"string","enum":["image","video","audio"]},"clientRequestId":{"type":"string"},"resources":{"type":"array","minItems":1,"items":{"type":"object","additionalProperties":false,"properties":{"resourceId":{"type":"string"},"kind":{"type":"string","enum":["image","video","audio"]},"url":{"type":"string"}},"required":["resourceId","kind","url"]}}},"required":["taskId","billingOrderId","mediaKind","clientRequestId","resources"]}`
const skillsLoadArgumentsSchema = `{"type":"object","additionalProperties":false,"properties":{"skillDir":{"type":"string"},"version":{"type":"integer"},"checksum":{"type":"string"}},"required":["skillDir","version","checksum"]}`
const skillsLoadResultSchema = `{"type":"object","additionalProperties":false,"properties":{"skillDir":{"type":"string"},"version":{"type":"integer"},"checksum":{"type":"string"},"instructions":{"type":"string"}},"required":["skillDir","version","checksum","instructions"]}`
