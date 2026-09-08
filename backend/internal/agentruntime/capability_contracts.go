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
	visionResourceLimit         = 12
	visionPromptLimit           = 64 * 1024
)

var (
	errCapabilityArgumentsInvalid = errors.New("agent capability arguments are invalid")
	errCapabilityResultInvalid    = errors.New("agent capability result is invalid")
)

// CapabilityArguments is intentionally closed. Only the current capability
// request contracts in this package can cross the model/runtime boundary.
type CapabilityArguments interface {
	capabilityArguments()
}

type CapabilityResult interface {
	capabilityResult()
}

type CanvasReadArguments struct {
	CanvasID        string   `json:"canvasId"`
	SelectedNodeIDs []string `json:"selectedNodeIds"`
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

type ProjectReadArguments struct {
	DomainProjectID string `json:"domainProjectId"`
	CanvasID        string `json:"canvasId"`
}

func (ProjectReadArguments) capabilityArguments() {}

type ProjectOperationType string

const (
	ProjectOperationCreateCharacterCandidates ProjectOperationType = "create_character_candidates"
	ProjectOperationReplaceUnitShots          ProjectOperationType = "replace_unit_shots"
)

type ProjectCharacterCandidateInput struct {
	Name              string   `json:"name"`
	Aliases           []string `json:"aliases"`
	Role              string   `json:"role"`
	Appearance        string   `json:"appearance"`
	Clothing          string   `json:"clothing"`
	Physique          string   `json:"physique"`
	Personality       string   `json:"personality"`
	Props             string   `json:"props"`
	ConsistencyPrompt string   `json:"consistencyPrompt"`
	MultiViewPrompt   string   `json:"multiViewPrompt"`
	VoiceLanguage     string   `json:"voiceLanguage"`
	VoiceAge          string   `json:"voiceAge"`
	VoiceTimbre       string   `json:"voiceTimbre"`
}

type ProjectShotInput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	DurationMS  int64  `json:"durationMs"`
}

type ProjectOperation struct {
	OperationID string                           `json:"operationId"`
	Type        ProjectOperationType             `json:"type"`
	UnitID      string                           `json:"unitId"`
	Candidates  []ProjectCharacterCandidateInput `json:"candidates,omitempty"`
	Shots       []ProjectShotInput               `json:"shots,omitempty"`
}

type ProjectApplyOpsArguments struct {
	DomainProjectID  string             `json:"domainProjectId"`
	BaseRevision     int64              `json:"baseRevision"`
	ClientMutationID string             `json:"clientMutationId"`
	Operations       []ProjectOperation `json:"operations"`
}

func (ProjectApplyOpsArguments) capabilityArguments() {}

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

type VisionDetail string

const (
	VisionDetailLow      VisionDetail = "low"
	VisionDetailOriginal VisionDetail = "original"
)

func (detail VisionDetail) Valid() bool {
	return detail == VisionDetailLow || detail == VisionDetailOriginal
}

type VisionAnalyzeArguments struct {
	ModelRecordID     string       `json:"modelRecordId"`
	ModelKey          string       `json:"modelKey"`
	SourceResourceIDs []string     `json:"sourceResourceIds"`
	Prompt            string       `json:"prompt"`
	Detail            VisionDetail `json:"detail"`
	ClientRequestID   string       `json:"clientRequestId"`
}

func (VisionAnalyzeArguments) capabilityArguments() {}

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
	case ToolProjectRead:
		return decodeProjectReadArguments(payload)
	case ToolProjectApplyOps:
		return decodeProjectApplyOpsArguments(payload)
	case ToolMediaGenerate:
		return decodeMediaGenerateArguments(payload)
	case ToolVisionAnalyze:
		return decodeVisionAnalyzeArguments(payload)
	case ToolSkillsLoad:
		return decodeSkillsLoadArguments(payload)
	default:
		return nil, errCapabilityArgumentsInvalid
	}
}

func decodeProjectReadArguments(payload json.RawMessage) (CapabilityArguments, error) {
	var wire ProjectReadArguments
	if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error { return decoder.Decode(&wire) }, capabilityPayloadLimit) != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	projectID, err := normalizeIdentifier(wire.DomainProjectID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	canvasID, err := normalizeIdentifier(wire.CanvasID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	return ProjectReadArguments{DomainProjectID: projectID, CanvasID: canvasID}, nil
}

func decodeProjectApplyOpsArguments(payload json.RawMessage) (CapabilityArguments, error) {
	var wire struct {
		DomainProjectID  string            `json:"domainProjectId"`
		BaseRevision     *int64            `json:"baseRevision"`
		ClientMutationID string            `json:"clientMutationId"`
		Operations       []json.RawMessage `json:"operations"`
	}
	if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error {
		return decoder.Decode(&wire)
	}, capabilityPayloadLimit) != nil || wire.BaseRevision == nil || *wire.BaseRevision < 0 || len(wire.Operations) < 1 || len(wire.Operations) > capabilityOperationLimit {
		return nil, errCapabilityArgumentsInvalid
	}
	projectID, err := normalizeIdentifier(wire.DomainProjectID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	mutationID, err := normalizeIdentifier(wire.ClientMutationID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	operations := make([]ProjectOperation, 0, len(wire.Operations))
	operationIDs := make(map[string]struct{}, len(wire.Operations))
	touchedUnits := make(map[string]map[ProjectOperationType]struct{})
	for _, raw := range wire.Operations {
		operation, decodeErr := decodeProjectOperation(raw)
		if decodeErr != nil {
			return nil, errCapabilityArgumentsInvalid
		}
		if _, duplicate := operationIDs[operation.OperationID]; duplicate {
			return nil, errCapabilityArgumentsInvalid
		}
		operationIDs[operation.OperationID] = struct{}{}
		if touchedUnits[operation.UnitID] == nil {
			touchedUnits[operation.UnitID] = make(map[ProjectOperationType]struct{})
		}
		if _, duplicate := touchedUnits[operation.UnitID][operation.Type]; duplicate {
			return nil, errCapabilityArgumentsInvalid
		}
		touchedUnits[operation.UnitID][operation.Type] = struct{}{}
		operations = append(operations, operation)
	}
	return ProjectApplyOpsArguments{DomainProjectID: projectID, BaseRevision: *wire.BaseRevision, ClientMutationID: mutationID, Operations: operations}, nil
}

func decodeProjectOperation(payload json.RawMessage) (ProjectOperation, error) {
	var header struct {
		OperationID string               `json:"operationId"`
		Type        ProjectOperationType `json:"type"`
	}
	if json.Unmarshal(payload, &header) != nil {
		return ProjectOperation{}, errCapabilityArgumentsInvalid
	}
	operationID, err := normalizeIdentifier(header.OperationID, capabilityIdentifierLimit)
	if err != nil {
		return ProjectOperation{}, errCapabilityArgumentsInvalid
	}
	switch header.Type {
	case ProjectOperationCreateCharacterCandidates:
		var wire struct {
			OperationID string                           `json:"operationId"`
			Type        ProjectOperationType             `json:"type"`
			UnitID      string                           `json:"unitId"`
			Candidates  []ProjectCharacterCandidateInput `json:"candidates"`
		}
		if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error { return decoder.Decode(&wire) }, capabilityPayloadLimit) != nil || len(wire.Candidates) < 1 || len(wire.Candidates) > capabilityResourceLimit {
			return ProjectOperation{}, errCapabilityArgumentsInvalid
		}
		unitID, normalizeErr := normalizeIdentifier(wire.UnitID, capabilityIdentifierLimit)
		if normalizeErr != nil {
			return ProjectOperation{}, errCapabilityArgumentsInvalid
		}
		candidates := make([]ProjectCharacterCandidateInput, 0, len(wire.Candidates))
		names := make(map[string]struct{}, len(wire.Candidates))
		for _, candidate := range wire.Candidates {
			normalized, candidateErr := normalizeProjectCharacterCandidate(candidate)
			if candidateErr != nil {
				return ProjectOperation{}, errCapabilityArgumentsInvalid
			}
			if _, duplicate := names[normalized.Name]; duplicate {
				return ProjectOperation{}, errCapabilityArgumentsInvalid
			}
			names[normalized.Name] = struct{}{}
			candidates = append(candidates, normalized)
		}
		return ProjectOperation{OperationID: operationID, Type: header.Type, UnitID: unitID, Candidates: candidates}, nil
	case ProjectOperationReplaceUnitShots:
		var wire struct {
			OperationID string               `json:"operationId"`
			Type        ProjectOperationType `json:"type"`
			UnitID      string               `json:"unitId"`
			Shots       []ProjectShotInput   `json:"shots"`
		}
		if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error { return decoder.Decode(&wire) }, capabilityPayloadLimit) != nil || len(wire.Shots) < 1 || len(wire.Shots) > 200 {
			return ProjectOperation{}, errCapabilityArgumentsInvalid
		}
		unitID, normalizeErr := normalizeIdentifier(wire.UnitID, capabilityIdentifierLimit)
		if normalizeErr != nil {
			return ProjectOperation{}, errCapabilityArgumentsInvalid
		}
		shots := make([]ProjectShotInput, 0, len(wire.Shots))
		for _, shot := range wire.Shots {
			title, titleErr := normalizeText(shot.Title, capabilityDisplayNameLimit)
			description, descriptionErr := normalizeText(shot.Description, 8*1024)
			if titleErr != nil || descriptionErr != nil || shot.DurationMS < 1_000 || shot.DurationMS > 30_000 {
				return ProjectOperation{}, errCapabilityArgumentsInvalid
			}
			shots = append(shots, ProjectShotInput{Title: title, Description: description, DurationMS: shot.DurationMS})
		}
		return ProjectOperation{OperationID: operationID, Type: header.Type, UnitID: unitID, Shots: shots}, nil
	default:
		return ProjectOperation{}, errCapabilityArgumentsInvalid
	}
}

func normalizeProjectCharacterCandidate(candidate ProjectCharacterCandidateInput) (ProjectCharacterCandidateInput, error) {
	name, err := normalizeText(candidate.Name, capabilityDisplayNameLimit)
	if err != nil || candidate.Aliases == nil || len(candidate.Aliases) > 20 {
		return ProjectCharacterCandidateInput{}, errCapabilityArgumentsInvalid
	}
	aliases := make([]string, 0, len(candidate.Aliases))
	seenAliases := make(map[string]struct{}, len(candidate.Aliases))
	for _, alias := range candidate.Aliases {
		normalized, aliasErr := normalizeText(alias, capabilityDisplayNameLimit)
		if aliasErr != nil || normalized == name {
			return ProjectCharacterCandidateInput{}, errCapabilityArgumentsInvalid
		}
		if _, duplicate := seenAliases[normalized]; duplicate {
			return ProjectCharacterCandidateInput{}, errCapabilityArgumentsInvalid
		}
		seenAliases[normalized] = struct{}{}
		aliases = append(aliases, normalized)
	}
	normalizeRequired := func(value string) (string, error) { return normalizeText(value, 2*1024) }
	role, roleErr := normalizeRequired(candidate.Role)
	appearance, appearanceErr := normalizeRequired(candidate.Appearance)
	clothing, clothingErr := normalizeRequired(candidate.Clothing)
	physique, physiqueErr := normalizeRequired(candidate.Physique)
	personality, personalityErr := normalizeRequired(candidate.Personality)
	consistencyPrompt, consistencyErr := normalizeRequired(candidate.ConsistencyPrompt)
	multiViewPrompt, multiViewErr := normalizeRequired(candidate.MultiViewPrompt)
	voiceLanguage, languageErr := normalizeRequired(candidate.VoiceLanguage)
	voiceAge, ageErr := normalizeRequired(candidate.VoiceAge)
	voiceTimbre, timbreErr := normalizeRequired(candidate.VoiceTimbre)
	props := strings.TrimSpace(candidate.Props)
	if !utf8.ValidString(props) || utf8.RuneCountInString(props) > 2*1024 || roleErr != nil || appearanceErr != nil || clothingErr != nil || physiqueErr != nil || personalityErr != nil || consistencyErr != nil || multiViewErr != nil || languageErr != nil || ageErr != nil || timbreErr != nil {
		return ProjectCharacterCandidateInput{}, errCapabilityArgumentsInvalid
	}
	return ProjectCharacterCandidateInput{
		Name: name, Aliases: aliases, Role: role, Appearance: appearance, Clothing: clothing, Physique: physique,
		Personality: personality, Props: props, ConsistencyPrompt: consistencyPrompt, MultiViewPrompt: multiViewPrompt,
		VoiceLanguage: voiceLanguage, VoiceAge: voiceAge, VoiceTimbre: voiceTimbre,
	}, nil
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
	if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error {
		return decoder.Decode(&wire)
	}, capabilityPayloadLimit) != nil || wire.SelectedNodeIDs == nil || wire.IncludeViewport == nil {
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
	if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error {
		return decoder.Decode(&wire)
	}, capabilityPayloadLimit) != nil || wire.BaseRevision == nil || *wire.BaseRevision < 0 || len(wire.Operations) < 1 || len(wire.Operations) > capabilityOperationLimit {
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
	if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error {
		return decoder.Decode(&wire)
	}, capabilityPayloadLimit) != nil || wire.ResourceIDs == nil || wire.Limit == nil || *wire.Limit < 1 || *wire.Limit > capabilityResourceLimit || len(wire.ResourceIDs) > *wire.Limit {
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
	if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error {
		return decoder.Decode(&arguments)
	}, capabilityPayloadLimit) != nil {
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
	if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error {
		return decoder.Decode(&wire)
	}, capabilityPayloadLimit) != nil || !wire.MediaKind.Valid() || wire.SourceResourceIDs == nil {
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

func decodeVisionAnalyzeArguments(payload json.RawMessage) (CapabilityArguments, error) {
	var wire VisionAnalyzeArguments
	if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error {
		return decoder.Decode(&wire)
	}, capabilityPayloadLimit) != nil || !wire.Detail.Valid() || wire.SourceResourceIDs == nil {
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
	resourceIDs, err := normalizeResourceIdentifiers(wire.SourceResourceIDs, visionResourceLimit, false)
	if err != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	prompt, err := normalizeText(wire.Prompt, visionPromptLimit)
	if err != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	requestID, err := normalizeIdentifier(wire.ClientRequestID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityArgumentsInvalid
	}
	return VisionAnalyzeArguments{
		ModelRecordID: modelRecordID, ModelKey: modelKey, SourceResourceIDs: resourceIDs,
		Prompt: prompt, Detail: wire.Detail, ClientRequestID: requestID,
	}, nil
}

func decodeSkillsLoadArguments(payload json.RawMessage) (CapabilityArguments, error) {
	var arguments SkillsLoadArguments
	if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error {
		return decoder.Decode(&arguments)
	}, capabilityPayloadLimit) != nil || arguments.Version < 1 {
		return nil, errCapabilityArgumentsInvalid
	}
	skillDir, err := normalizeText(arguments.SkillDir, capabilityDisplayNameLimit)
	if err != nil || !validSHA256(arguments.Checksum) {
		return nil, errCapabilityArgumentsInvalid
	}
	return SkillsLoadArguments{SkillDir: skillDir, Version: arguments.Version, Checksum: arguments.Checksum}, nil
}

func decodeStrictCapabilityJSON(payload json.RawMessage, decode func(*json.Decoder) error, limit int) error {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || len(trimmed) > limit || bytes.Equal(trimmed, []byte("null")) || trimmed[0] != '{' {
		return errCapabilityArgumentsInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decode(decoder); err != nil {
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
