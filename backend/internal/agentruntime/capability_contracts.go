package agentruntime

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	capabilityPayloadLimit      = 128 * 1024
	capabilityParametersLimit   = 32 * 1024
	capabilityInstructionsLimit = 64 * 1024
	capabilityIdentifierLimit   = 120
	capabilityDisplayNameLimit  = 240
	capabilityOperationLimit    = 100
	capabilityResourceLimit     = 100
)

var (
	errCapabilityArgumentsInvalid = errors.New("agent capability arguments are invalid")
	errCapabilityResultInvalid    = errors.New("agent capability result is invalid")
)

// CapabilityArguments is intentionally closed. Only the six v6 capability
// request contracts in this package can cross the model/runtime boundary.
type CapabilityArguments interface {
	capabilityArguments()
}

type CapabilityResult interface {
	capabilityResult()
}

type CanvasReadArguments struct {
	CanvasID        string   `json:"canvasId"`
	SelectedNodeIDs []string `json:"selectedNodeIds,omitempty"`
	IncludeViewport bool     `json:"includeViewport"`
}

func (CanvasReadArguments) capabilityArguments() {}

type CanvasApplyOpsArguments struct {
	CanvasID         string            `json:"canvasId"`
	BaseRevision     int64             `json:"baseRevision"`
	ClientMutationID string            `json:"clientMutationId"`
	Operations       []CanvasOperation `json:"operations"`
}

func (CanvasApplyOpsArguments) capabilityArguments() {}

type AssetsReadArguments struct {
	DomainProjectID string   `json:"domainProjectId"`
	ResourceIDs     []string `json:"resourceIds"`
	Limit           int      `json:"limit"`
}

func (AssetsReadArguments) capabilityArguments() {}

type AssetsPublishArguments struct {
	ResourceID       string `json:"resourceId"`
	DomainProjectID  string `json:"domainProjectId"`
	DisplayName      string `json:"displayName"`
	ClientMutationID string `json:"clientMutationId"`
}

func (AssetsPublishArguments) capabilityArguments() {}

type MediaKind string

const (
	MediaKindImage MediaKind = "image"
	MediaKindVideo MediaKind = "video"
	MediaKindAudio MediaKind = "audio"
)

func (kind MediaKind) Valid() bool {
	return kind == MediaKindImage || kind == MediaKindVideo || kind == MediaKindAudio
}

type MediaGenerateArguments struct {
	MediaKind          MediaKind       `json:"mediaKind"`
	ModelRecordID      string          `json:"modelRecordId"`
	ModelKey           string          `json:"modelKey"`
	Parameters         json.RawMessage `json:"parameters"`
	SourceResourceIDs  []string        `json:"sourceResourceIds"`
	TargetCanvasNodeID string          `json:"targetCanvasNodeId"`
	ClientRequestID    string          `json:"clientRequestId"`
}

func (MediaGenerateArguments) capabilityArguments() {}

type SkillsLoadArguments struct {
	SkillDir string `json:"skillDir"`
	Version  int    `json:"version"`
	Checksum string `json:"checksum"`
}

func (SkillsLoadArguments) capabilityArguments() {}

func DecodeCapabilityArguments(name ToolName, payload json.RawMessage) (CapabilityArguments, error) {
	if !name.ValidForToolSchema(CurrentToolSchemaVersion) {
		return nil, errCapabilityArgumentsInvalid
	}
	switch name {
	case ToolCanvasRead:
		return decodeCanvasReadArguments(payload)
	case ToolCanvasApplyOps:
		return decodeCanvasApplyOpsArguments(payload)
	case ToolAssetsRead:
		return decodeAssetsReadArguments(payload)
	case ToolAssetsPublish:
		return decodeAssetsPublishArguments(payload)
	case ToolMediaGenerate:
		return decodeMediaGenerateArguments(payload)
	case ToolSkillsLoad:
		return decodeSkillsLoadArguments(payload)
	default:
		return nil, errCapabilityArgumentsInvalid
	}
}

func ValidateMediaGenerateModelCapability(arguments MediaGenerateArguments, authoritativeCapability string) error {
	if !arguments.MediaKind.Valid() || string(arguments.MediaKind) != strings.TrimSpace(authoritativeCapability) {
		return errors.New("agent media capability conflicts with the selected model")
	}
	return nil
}

func decodeCanvasReadArguments(payload json.RawMessage) (CapabilityArguments, error) {
	var wire struct {
		CanvasID        string   `json:"canvasId"`
		SelectedNodeIDs []string `json:"selectedNodeIds,omitempty"`
		IncludeViewport *bool    `json:"includeViewport"`
	}
	if decodeStrictCapabilityJSON(payload, &wire, capabilityPayloadLimit) != nil || wire.SelectedNodeIDs == nil || wire.IncludeViewport == nil {
		return nil, errCapabilityArgumentsInvalid
	}
	canvasID, err := normalizeIdentifier(wire.CanvasID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	nodeIDs, err := normalizeIdentifiers(wire.SelectedNodeIDs, capabilityResourceLimit, true)
	if err != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	return CanvasReadArguments{CanvasID: canvasID, SelectedNodeIDs: nodeIDs, IncludeViewport: *wire.IncludeViewport}, nil
}

func decodeCanvasApplyOpsArguments(payload json.RawMessage) (CapabilityArguments, error) {
	var wire struct {
		CanvasID         string            `json:"canvasId"`
		BaseRevision     *int64            `json:"baseRevision"`
		ClientMutationID string            `json:"clientMutationId"`
		Operations       []json.RawMessage `json:"operations"`
	}
	if decodeStrictCapabilityJSON(payload, &wire, capabilityPayloadLimit) != nil || wire.BaseRevision == nil || *wire.BaseRevision < 0 || len(wire.Operations) < 1 || len(wire.Operations) > capabilityOperationLimit {
		return nil, errCapabilityArgumentsInvalid
	}
	canvasID, err := normalizeIdentifier(wire.CanvasID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	mutationID, err := normalizeIdentifier(wire.ClientMutationID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	operations := make([]CanvasOperation, 0, len(wire.Operations))
	operationIDs := make(map[string]struct{}, len(wire.Operations))
	for _, raw := range wire.Operations {
		operation, decodeErr := decodeCanvasOperation(raw)
		if decodeErr != nil {
			return nil, errCapabilityArgumentsInvalid
		}
		if _, exists := operationIDs[operation.OperationID]; exists {
			return nil, errCapabilityArgumentsInvalid
		}
		operationIDs[operation.OperationID] = struct{}{}
		operations = append(operations, operation)
	}
	return CanvasApplyOpsArguments{CanvasID: canvasID, BaseRevision: *wire.BaseRevision, ClientMutationID: mutationID, Operations: operations}, nil
}

func decodeAssetsReadArguments(payload json.RawMessage) (CapabilityArguments, error) {
	var wire struct {
		DomainProjectID string   `json:"domainProjectId"`
		ResourceIDs     []string `json:"resourceIds"`
		Limit           *int     `json:"limit"`
	}
	if decodeStrictCapabilityJSON(payload, &wire, capabilityPayloadLimit) != nil || wire.ResourceIDs == nil || wire.Limit == nil || *wire.Limit < 1 || *wire.Limit > capabilityResourceLimit {
		return nil, errCapabilityArgumentsInvalid
	}
	projectID, err := normalizeIdentifier(wire.DomainProjectID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	resourceIDs, err := normalizeResourceIdentifiers(wire.ResourceIDs, capabilityResourceLimit, true)
	if err != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	return AssetsReadArguments{DomainProjectID: projectID, ResourceIDs: resourceIDs, Limit: *wire.Limit}, nil
}

func decodeAssetsPublishArguments(payload json.RawMessage) (CapabilityArguments, error) {
	var arguments AssetsPublishArguments
	if decodeStrictCapabilityJSON(payload, &arguments, capabilityPayloadLimit) != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	resourceID, err := normalizeResourceIdentifier(arguments.ResourceID)
	if err != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	projectID, err := normalizeIdentifier(arguments.DomainProjectID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	displayName, err := normalizeText(arguments.DisplayName, capabilityDisplayNameLimit)
	if err != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	mutationID, err := normalizeIdentifier(arguments.ClientMutationID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	return AssetsPublishArguments{ResourceID: resourceID, DomainProjectID: projectID, DisplayName: displayName, ClientMutationID: mutationID}, nil
}

func decodeMediaGenerateArguments(payload json.RawMessage) (CapabilityArguments, error) {
	var wire struct {
		MediaKind          MediaKind       `json:"mediaKind"`
		ModelRecordID      string          `json:"modelRecordId"`
		ModelKey           string          `json:"modelKey"`
		Parameters         json.RawMessage `json:"parameters"`
		SourceResourceIDs  []string        `json:"sourceResourceIds"`
		TargetCanvasNodeID string          `json:"targetCanvasNodeId"`
		ClientRequestID    string          `json:"clientRequestId"`
	}
	if decodeStrictCapabilityJSON(payload, &wire, capabilityPayloadLimit) != nil || !wire.MediaKind.Valid() || wire.SourceResourceIDs == nil {
		return nil, errCapabilityArgumentsInvalid
	}
	modelRecordID, err := normalizeIdentifier(wire.ModelRecordID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	modelKey, err := normalizeText(wire.ModelKey, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	parameters, err := normalizeJSONObject(wire.Parameters, capabilityParametersLimit, false)
	if err != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	resourceIDs, err := normalizeResourceIdentifiers(wire.SourceResourceIDs, capabilityResourceLimit, true)
	if err != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	targetNodeID, err := normalizeIdentifier(wire.TargetCanvasNodeID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	requestID, err := normalizeIdentifier(wire.ClientRequestID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	return MediaGenerateArguments{MediaKind: wire.MediaKind, ModelRecordID: modelRecordID, ModelKey: modelKey, Parameters: parameters, SourceResourceIDs: resourceIDs, TargetCanvasNodeID: targetNodeID, ClientRequestID: requestID}, nil
}

func decodeSkillsLoadArguments(payload json.RawMessage) (CapabilityArguments, error) {
	var arguments SkillsLoadArguments
	if decodeStrictCapabilityJSON(payload, &arguments, capabilityPayloadLimit) != nil || arguments.Version < 1 {
		return nil, errCapabilityArgumentsInvalid
	}
	skillDir, err := normalizeText(arguments.SkillDir, capabilityDisplayNameLimit)
	if err != nil || !validSHA256(arguments.Checksum) {
		return nil, errCapabilityArgumentsInvalid
	}
	return SkillsLoadArguments{SkillDir: skillDir, Version: arguments.Version, Checksum: arguments.Checksum}, nil
}

func decodeStrictCapabilityJSON(payload json.RawMessage, target interface{}, limit int) error {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || len(trimmed) > limit || bytes.Equal(trimmed, []byte("null")) || trimmed[0] != '{' {
		return errCapabilityArgumentsInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errCapabilityArgumentsInvalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errCapabilityArgumentsInvalid
	}
	return nil
}

func normalizeOperationAndNodeID(rawOperationID string, rawNodeID string) (string, string, error) {
	operationID, err := normalizeIdentifier(rawOperationID, capabilityIdentifierLimit)
	if err != nil {
		return "", "", err
	}
	nodeID, err := normalizeIdentifier(rawNodeID, capabilityIdentifierLimit)
	if err != nil {
		return "", "", err
	}
	return operationID, nodeID, nil
}

func normalizeResourceIdentifiers(values []string, limit int, allowEmpty bool) ([]string, error) {
	if len(values) > limit || (!allowEmpty && len(values) == 0) {
		return nil, errCapabilityArgumentsInvalid
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized, err := normalizeResourceIdentifier(value)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[normalized]; exists {
			return nil, errCapabilityArgumentsInvalid
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result, nil
}

func normalizeResourceIdentifier(value string) (string, error) {
	normalized, err := normalizeIdentifier(value, capabilityIdentifierLimit)
	if err != nil {
		return "", err
	}
	parsed, parseErr := url.Parse(normalized)
	if parseErr == nil && parsed.IsAbs() {
		return "", errCapabilityArgumentsInvalid
	}
	return normalized, nil
}

func normalizeIdentifiers(values []string, limit int, allowEmpty bool) ([]string, error) {
	if len(values) > limit || (!allowEmpty && len(values) == 0) {
		return nil, errCapabilityArgumentsInvalid
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized, err := normalizeIdentifier(value, capabilityIdentifierLimit)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[normalized]; exists {
			return nil, errCapabilityArgumentsInvalid
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result, nil
}

func normalizeIdentifier(value string, limit int) (string, error) {
	normalized, err := normalizeText(value, limit)
	if err != nil {
		return "", err
	}
	for _, character := range normalized {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return "", errCapabilityArgumentsInvalid
		}
	}
	return normalized, nil
}

func normalizeOptionalIdentifier(value string, limit int) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	return normalizeIdentifier(value, limit)
}

func normalizeText(value string, limit int) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" || !utf8.ValidString(normalized) || utf8.RuneCountInString(normalized) > limit {
		return "", errCapabilityArgumentsInvalid
	}
	return normalized, nil
}

func normalizeJSONObject(payload json.RawMessage, limit int, optional bool) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 && optional {
		return nil, nil
	}
	if len(trimmed) == 0 || len(trimmed) > limit || trimmed[0] != '{' || !json.Valid(trimmed) {
		return nil, errCapabilityArgumentsInvalid
	}
	var document map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	if err := decoder.Decode(&document); err != nil || document == nil {
		return nil, errCapabilityArgumentsInvalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errCapabilityArgumentsInvalid
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, trimmed); err != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	return append(json.RawMessage(nil), compact.Bytes()...), nil
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && strings.ToLower(value) == value
}
