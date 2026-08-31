package agentruntime

import (
	"encoding/json"
	"errors"
)

type ToolExecutionResult struct {
	Output json.RawMessage
}

func NewToolExecutionResult(name ToolName, result CapabilityResult) (ToolExecutionResult, error) {
	if result == nil {
		return ToolExecutionResult{}, errors.New("agent capability result is missing")
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > agentToolResultLimit {
		return ToolExecutionResult{}, errCapabilityResultInvalid
	}
	if _, err := DecodeCapabilityResult(name, encoded); err != nil {
		return ToolExecutionResult{}, err
	}
	return ToolExecutionResult{Output: append(json.RawMessage(nil), encoded...)}, nil
}

type CanvasReadResult struct {
	CanvasID        string            `json:"canvasId"`
	Revision        int64             `json:"revision"`
	Nodes           []json.RawMessage `json:"nodes"`
	Edges           []json.RawMessage `json:"edges"`
	SelectedNodeIDs []string          `json:"selectedNodeIds"`
	Viewport        CanvasViewport    `json:"viewport"`
}

func (CanvasReadResult) capabilityResult() {}

type CanvasApplyOpsResult struct {
	CanvasID            string   `json:"canvasId"`
	BaseRevision        int64    `json:"baseRevision"`
	CommittedRevision   int64    `json:"committedRevision"`
	ClientMutationID    string   `json:"clientMutationId"`
	AppliedOperationIDs []string `json:"appliedOperationIds"`
}

func (CanvasApplyOpsResult) capabilityResult() {}

type AssetResourceResult struct {
	ResourceID string    `json:"resourceId"`
	Name       string    `json:"name"`
	Kind       MediaKind `json:"kind"`
	MimeType   string    `json:"mimeType"`
	Width      int       `json:"width,omitempty"`
	Height     int       `json:"height,omitempty"`
	DurationMS int64     `json:"durationMs,omitempty"`
}

type AssetsReadResult struct {
	DomainProjectID string                `json:"domainProjectId"`
	Resources       []AssetResourceResult `json:"resources"`
}

func (AssetsReadResult) capabilityResult() {}

type AssetsPublishResult struct {
	DomainProjectID  string `json:"domainProjectId"`
	ResourceID       string `json:"resourceId"`
	AssetID          string `json:"assetId"`
	ClientMutationID string `json:"clientMutationId"`
}

func (AssetsPublishResult) capabilityResult() {}

type MediaGenerateResult struct {
	TaskID          string    `json:"taskId"`
	MediaKind       MediaKind `json:"mediaKind"`
	ClientRequestID string    `json:"clientRequestId"`
}

func (MediaGenerateResult) capabilityResult() {}

type SkillsLoadResult struct {
	SkillDir     string `json:"skillDir"`
	Version      int    `json:"version"`
	Checksum     string `json:"checksum"`
	Instructions string `json:"instructions"`
}

func (SkillsLoadResult) capabilityResult() {}

func DecodeCapabilityResult(name ToolName, payload json.RawMessage) (CapabilityResult, error) {
	if !name.ValidForToolSchema(CurrentToolSchemaVersion) {
		return nil, errCapabilityResultInvalid
	}
	switch name {
	case ToolCanvasRead:
		return decodeCanvasReadResult(payload)
	case ToolCanvasApplyOps:
		return decodeCanvasApplyOpsResult(payload)
	case ToolAssetsRead:
		return decodeAssetsReadResult(payload)
	case ToolAssetsPublish:
		return decodeAssetsPublishResult(payload)
	case ToolMediaGenerate:
		return decodeMediaGenerateResult(payload)
	case ToolSkillsLoad:
		return decodeSkillsLoadResult(payload)
	default:
		return nil, errCapabilityResultInvalid
	}
}

func decodeCanvasReadResult(payload json.RawMessage) (CapabilityResult, error) {
	var wire struct {
		CanvasID        string            `json:"canvasId"`
		Revision        *int64            `json:"revision"`
		Nodes           []json.RawMessage `json:"nodes"`
		Edges           []json.RawMessage `json:"edges"`
		SelectedNodeIDs []string          `json:"selectedNodeIds"`
		Viewport        *CanvasViewport   `json:"viewport"`
	}
	if decodeStrictCapabilityJSON(payload, &wire, capabilityPayloadLimit) != nil || wire.Revision == nil || *wire.Revision < 0 || wire.Nodes == nil || wire.Edges == nil || wire.SelectedNodeIDs == nil || wire.Viewport == nil || !validViewport(*wire.Viewport) {
		return nil, errCapabilityResultInvalid
	}
	canvasID, err := normalizeIdentifier(wire.CanvasID, capabilityIdentifierLimit)
	if err != nil || validateRawObjectList(wire.Nodes) != nil || validateRawObjectList(wire.Edges) != nil {
		return nil, errCapabilityResultInvalid
	}
	nodeIDs, err := normalizeIdentifiers(wire.SelectedNodeIDs, capabilityResourceLimit, true)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	return CanvasReadResult{CanvasID: canvasID, Revision: *wire.Revision, Nodes: cloneRawMessages(wire.Nodes), Edges: cloneRawMessages(wire.Edges), SelectedNodeIDs: nodeIDs, Viewport: *wire.Viewport}, nil
}

func decodeCanvasApplyOpsResult(payload json.RawMessage) (CapabilityResult, error) {
	var wire struct {
		CanvasID            string   `json:"canvasId"`
		BaseRevision        *int64   `json:"baseRevision"`
		CommittedRevision   *int64   `json:"committedRevision"`
		ClientMutationID    string   `json:"clientMutationId"`
		AppliedOperationIDs []string `json:"appliedOperationIds"`
	}
	if decodeStrictCapabilityJSON(payload, &wire, capabilityPayloadLimit) != nil || wire.BaseRevision == nil || wire.CommittedRevision == nil || *wire.BaseRevision < 0 || *wire.CommittedRevision <= *wire.BaseRevision || len(wire.AppliedOperationIDs) < 1 {
		return nil, errCapabilityResultInvalid
	}
	canvasID, err := normalizeIdentifier(wire.CanvasID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	mutationID, err := normalizeIdentifier(wire.ClientMutationID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	operationIDs, err := normalizeIdentifiers(wire.AppliedOperationIDs, capabilityOperationLimit, false)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	return CanvasApplyOpsResult{CanvasID: canvasID, BaseRevision: *wire.BaseRevision, CommittedRevision: *wire.CommittedRevision, ClientMutationID: mutationID, AppliedOperationIDs: operationIDs}, nil
}

func decodeAssetsReadResult(payload json.RawMessage) (CapabilityResult, error) {
	var wire AssetsReadResult
	if decodeStrictCapabilityJSON(payload, &wire, capabilityPayloadLimit) != nil || wire.Resources == nil || len(wire.Resources) > capabilityResourceLimit {
		return nil, errCapabilityResultInvalid
	}
	projectID, err := normalizeIdentifier(wire.DomainProjectID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	resources := make([]AssetResourceResult, 0, len(wire.Resources))
	seen := make(map[string]struct{}, len(wire.Resources))
	for _, resource := range wire.Resources {
		resourceID, normalizeErr := normalizeResourceIdentifier(resource.ResourceID)
		if normalizeErr != nil || !resource.Kind.Valid() || resource.Width < 0 || resource.Height < 0 || resource.DurationMS < 0 {
			return nil, errCapabilityResultInvalid
		}
		if _, exists := seen[resourceID]; exists {
			return nil, errCapabilityResultInvalid
		}
		seen[resourceID] = struct{}{}
		name, normalizeErr := normalizeText(resource.Name, capabilityDisplayNameLimit)
		if normalizeErr != nil {
			return nil, errCapabilityResultInvalid
		}
		mimeType, normalizeErr := normalizeText(resource.MimeType, capabilityIdentifierLimit)
		if normalizeErr != nil {
			return nil, errCapabilityResultInvalid
		}
		resources = append(resources, AssetResourceResult{ResourceID: resourceID, Name: name, Kind: resource.Kind, MimeType: mimeType, Width: resource.Width, Height: resource.Height, DurationMS: resource.DurationMS})
	}
	return AssetsReadResult{DomainProjectID: projectID, Resources: resources}, nil
}

func decodeAssetsPublishResult(payload json.RawMessage) (CapabilityResult, error) {
	var wire AssetsPublishResult
	if decodeStrictCapabilityJSON(payload, &wire, capabilityPayloadLimit) != nil {
		return nil, errCapabilityResultInvalid
	}
	projectID, err := normalizeIdentifier(wire.DomainProjectID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	resourceID, err := normalizeResourceIdentifier(wire.ResourceID)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	assetID, err := normalizeIdentifier(wire.AssetID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	mutationID, err := normalizeIdentifier(wire.ClientMutationID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	return AssetsPublishResult{DomainProjectID: projectID, ResourceID: resourceID, AssetID: assetID, ClientMutationID: mutationID}, nil
}

func decodeMediaGenerateResult(payload json.RawMessage) (CapabilityResult, error) {
	var wire MediaGenerateResult
	if decodeStrictCapabilityJSON(payload, &wire, capabilityPayloadLimit) != nil || !wire.MediaKind.Valid() {
		return nil, errCapabilityResultInvalid
	}
	taskID, err := normalizeIdentifier(wire.TaskID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	requestID, err := normalizeIdentifier(wire.ClientRequestID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	return MediaGenerateResult{TaskID: taskID, MediaKind: wire.MediaKind, ClientRequestID: requestID}, nil
}

func decodeSkillsLoadResult(payload json.RawMessage) (CapabilityResult, error) {
	var wire SkillsLoadResult
	if decodeStrictCapabilityJSON(payload, &wire, capabilityPayloadLimit) != nil || wire.Version < 1 || !validSHA256(wire.Checksum) {
		return nil, errCapabilityResultInvalid
	}
	skillDir, err := normalizeText(wire.SkillDir, capabilityDisplayNameLimit)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	instructions, err := normalizeText(wire.Instructions, capabilityInstructionsLimit)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	return SkillsLoadResult{SkillDir: skillDir, Version: wire.Version, Checksum: wire.Checksum, Instructions: instructions}, nil
}

func validateRawObjectList(values []json.RawMessage) error {
	for _, value := range values {
		if _, err := normalizeJSONObject(value, capabilityParametersLimit, false); err != nil {
			return errCapabilityResultInvalid
		}
	}
	return nil
}

func cloneRawMessages(values []json.RawMessage) []json.RawMessage {
	result := make([]json.RawMessage, len(values))
	for index, value := range values {
		result[index] = append(json.RawMessage(nil), value...)
	}
	return result
}
