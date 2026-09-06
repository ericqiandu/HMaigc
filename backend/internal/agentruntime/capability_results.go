package agentruntime

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

type ToolExecutionResult struct {
	Output  json.RawMessage
	Pending bool
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
	CanvasID        string                    `json:"canvasId"`
	DomainProjectID string                    `json:"domainProjectId"`
	Revision        int64                     `json:"revision"`
	Nodes           []json.RawMessage         `json:"nodes"`
	Edges           []json.RawMessage         `json:"edges"`
	SelectedNodeIDs []string                  `json:"selectedNodeIds"`
	Viewport        CanvasViewport            `json:"viewport"`
	CallableModels  []CanvasCallableModelFact `json:"callableModels"`
}

func (CanvasReadResult) capabilityResult() {}

// CanvasCallableModelFact is the public, execution-safe model catalog snapshot
// returned with canvas.read. It contains only facts required to form a valid
// media.generate request; provider credentials never enter this contract.
type CanvasCallableModelFact struct {
	ChannelID             string                         `json:"channelId"`
	ModelRecordID         string                         `json:"modelRecordId"`
	ModelKey              string                         `json:"modelKey"`
	DisplayName           string                         `json:"displayName"`
	Capability            string                         `json:"capability"`
	BillingMode           string                         `json:"billingMode"`
	PriceStrategy         string                         `json:"priceStrategy"`
	UnitPriceMicrocredits int64                          `json:"unitPriceMicrocredits"`
	PriceTiers            []CanvasCallableModelPriceTier `json:"priceTiers"`
	ProviderCapabilities  json.RawMessage                `json:"providerCapabilities,omitempty"`
}

type CanvasCallableModelPriceTier struct {
	Resolution            string `json:"resolution"`
	InputVariant          string `json:"inputVariant"`
	UsageMetric           string `json:"usageMetric"`
	IncludedQuantity      int64  `json:"includedQuantity"`
	UnitPriceMicrocredits int64  `json:"unitPriceMicrocredits"`
}

type CanvasApplyOpsResult struct {
	CanvasID            string                 `json:"canvasId"`
	BaseRevision        int64                  `json:"baseRevision"`
	CommittedRevision   int64                  `json:"committedRevision"`
	ClientMutationID    string                 `json:"clientMutationId"`
	ProposalHash        string                 `json:"proposalHash"`
	AppliedOperationIDs []string               `json:"appliedOperationIds"`
	Evidence            CanvasApplyOpsEvidence `json:"evidence"`
}

func (CanvasApplyOpsResult) capabilityResult() {}

type CanvasApplyOpsEvidence struct {
	AddedNodeIDs          []string `json:"addedNodeIds"`
	UpdatedNodeIDs        []string `json:"updatedNodeIds"`
	DeletedNodeIDs        []string `json:"deletedNodeIds"`
	UpsertedConnectionIDs []string `json:"upsertedConnectionIds"`
	DeletedConnectionIDs  []string `json:"deletedConnectionIds"`
	SelectedNodeIDs       []string `json:"selectedNodeIds"`
	ViewportApplied       bool     `json:"viewportApplied"`
}

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

type ProjectReadShot struct {
	ShotID      string `json:"shotId"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Position    int    `json:"position"`
	DurationMS  int64  `json:"durationMs"`
	Status      string `json:"status"`
}

type ProjectReadUnit struct {
	UnitID     string            `json:"unitId"`
	Kind       string            `json:"kind"`
	Title      string            `json:"title"`
	SourceText string            `json:"sourceText"`
	Status     string            `json:"status"`
	Position   int               `json:"position"`
	LinkRole   string            `json:"linkRole"`
	Shots      []ProjectReadShot `json:"shots"`
}

type ProjectReadRepresentation struct {
	RepresentationID string `json:"representationId"`
	ResourceID       string `json:"resourceId"`
	MediaType        string `json:"mediaType"`
	Role             string `json:"role"`
}

type ProjectReadCharacter struct {
	AssetID         string                      `json:"assetId"`
	Name            string                      `json:"name"`
	VersionID       string                      `json:"versionId"`
	Version         int                         `json:"version"`
	Status          string                      `json:"status"`
	Definition      json.RawMessage             `json:"definition"`
	Representations []ProjectReadRepresentation `json:"representations"`
}

type ProjectReadResult struct {
	DomainProjectID string                 `json:"domainProjectId"`
	CanvasID        string                 `json:"canvasId"`
	Name            string                 `json:"name"`
	ProjectType     string                 `json:"projectType"`
	AspectRatio     string                 `json:"aspectRatio"`
	Revision        int64                  `json:"revision"`
	Units           []ProjectReadUnit      `json:"units"`
	Characters      []ProjectReadCharacter `json:"characters"`
}

func (ProjectReadResult) capabilityResult() {}

type ProjectApplyOpsResult struct {
	DomainProjectID     string   `json:"domainProjectId"`
	BaseRevision        int64    `json:"baseRevision"`
	CommittedRevision   int64    `json:"committedRevision"`
	ClientMutationID    string   `json:"clientMutationId"`
	ProposalHash        string   `json:"proposalHash"`
	AppliedOperationIDs []string `json:"appliedOperationIds"`
	CandidateIDs        []string `json:"candidateIds"`
	ShotIDs             []string `json:"shotIds"`
}

func (ProjectApplyOpsResult) capabilityResult() {}

type MediaGenerateResult struct {
	TaskID          string                         `json:"taskId"`
	BillingOrderID  string                         `json:"billingOrderId"`
	MediaKind       MediaKind                      `json:"mediaKind"`
	ClientRequestID string                         `json:"clientRequestId"`
	Resources       []MediaGeneratedResourceResult `json:"resources"`
}

func (MediaGenerateResult) capabilityResult() {}

type MediaGeneratedResourceResult struct {
	ResourceID string    `json:"resourceId"`
	Kind       MediaKind `json:"kind"`
	URL        string    `json:"url"`
}

type VisionTokenUsage struct {
	InputTokens  int64 `json:"inputTokens"`
	CachedTokens int64 `json:"cachedTokens"`
	OutputTokens int64 `json:"outputTokens"`
}

type VisionAnalyzeResult struct {
	TaskID            string           `json:"taskId"`
	BillingOrderID    string           `json:"billingOrderId"`
	ModelRecordID     string           `json:"modelRecordId"`
	ModelKey          string           `json:"modelKey"`
	ClientRequestID   string           `json:"clientRequestId"`
	SourceResourceIDs []string         `json:"sourceResourceIds"`
	Detail            VisionDetail     `json:"detail"`
	Analysis          string           `json:"analysis"`
	Usage             VisionTokenUsage `json:"usage"`
}

func (VisionAnalyzeResult) capabilityResult() {}

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
	case ToolProjectRead:
		return decodeProjectReadResult(payload)
	case ToolProjectApplyOps:
		return decodeProjectApplyOpsResult(payload)
	case ToolMediaGenerate:
		return decodeMediaGenerateResult(payload)
	case ToolVisionAnalyze:
		return decodeVisionAnalyzeResult(payload)
	case ToolSkillsLoad:
		return decodeSkillsLoadResult(payload)
	default:
		return nil, errCapabilityResultInvalid
	}
}

func decodeProjectReadResult(payload json.RawMessage) (CapabilityResult, error) {
	var wire ProjectReadResult
	if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error { return decoder.Decode(&wire) }, capabilityPayloadLimit) != nil || wire.Revision < 0 || wire.Units == nil || wire.Characters == nil {
		return nil, errCapabilityResultInvalid
	}
	projectID, err := normalizeIdentifier(wire.DomainProjectID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	canvasID, err := normalizeIdentifier(wire.CanvasID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	name, err := normalizeText(wire.Name, capabilityDisplayNameLimit)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	projectType, err := normalizeText(wire.ProjectType, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	aspectRatio, err := normalizeText(wire.AspectRatio, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	units := make([]ProjectReadUnit, 0, len(wire.Units))
	seenUnits := make(map[string]struct{}, len(wire.Units))
	for _, unit := range wire.Units {
		unitID, unitErr := normalizeIdentifier(unit.UnitID, capabilityIdentifierLimit)
		if unitErr != nil || unit.Position < 0 || unit.Shots == nil {
			return nil, errCapabilityResultInvalid
		}
		if _, duplicate := seenUnits[unitID]; duplicate {
			return nil, errCapabilityResultInvalid
		}
		seenUnits[unitID] = struct{}{}
		kind, kindErr := normalizeText(unit.Kind, capabilityIdentifierLimit)
		title, titleErr := normalizeText(unit.Title, capabilityDisplayNameLimit)
		status, statusErr := normalizeText(unit.Status, capabilityIdentifierLimit)
		linkRole, linkRoleErr := normalizeText(unit.LinkRole, capabilityIdentifierLimit)
		if kindErr != nil || titleErr != nil || statusErr != nil || linkRoleErr != nil || !utf8.ValidString(unit.SourceText) || utf8.RuneCountInString(unit.SourceText) > 64*1024 {
			return nil, errCapabilityResultInvalid
		}
		shots := make([]ProjectReadShot, 0, len(unit.Shots))
		for _, shot := range unit.Shots {
			shotID, shotErr := normalizeIdentifier(shot.ShotID, capabilityIdentifierLimit)
			shotTitle, shotTitleErr := normalizeText(shot.Title, capabilityDisplayNameLimit)
			description, descriptionErr := normalizeText(shot.Description, 8*1024)
			shotStatus, shotStatusErr := normalizeText(shot.Status, capabilityIdentifierLimit)
			if shotErr != nil || shotTitleErr != nil || descriptionErr != nil || shotStatusErr != nil || shot.Position < 0 || shot.DurationMS < 1_000 || shot.DurationMS > 30_000 {
				return nil, errCapabilityResultInvalid
			}
			shots = append(shots, ProjectReadShot{ShotID: shotID, Title: shotTitle, Description: description, Position: shot.Position, DurationMS: shot.DurationMS, Status: shotStatus})
		}
		units = append(units, ProjectReadUnit{UnitID: unitID, Kind: kind, Title: title, SourceText: unit.SourceText, Status: status, Position: unit.Position, LinkRole: linkRole, Shots: shots})
	}
	characters := make([]ProjectReadCharacter, 0, len(wire.Characters))
	seenCharacters := make(map[string]struct{}, len(wire.Characters))
	for _, character := range wire.Characters {
		assetID, assetErr := normalizeIdentifier(character.AssetID, capabilityIdentifierLimit)
		versionID, versionErr := normalizeIdentifier(character.VersionID, capabilityIdentifierLimit)
		name, nameErr := normalizeText(character.Name, capabilityDisplayNameLimit)
		status, statusErr := normalizeText(character.Status, capabilityIdentifierLimit)
		definition, definitionErr := normalizeJSONObject(character.Definition, capabilityInstructionsLimit, false)
		if assetErr != nil || versionErr != nil || nameErr != nil || statusErr != nil || definitionErr != nil || character.Version < 1 || character.Representations == nil {
			return nil, errCapabilityResultInvalid
		}
		if _, duplicate := seenCharacters[assetID]; duplicate {
			return nil, errCapabilityResultInvalid
		}
		seenCharacters[assetID] = struct{}{}
		representations := make([]ProjectReadRepresentation, 0, len(character.Representations))
		for _, representation := range character.Representations {
			representationID, representationErr := normalizeIdentifier(representation.RepresentationID, capabilityIdentifierLimit)
			resourceID, resourceErr := normalizeResourceIdentifier(representation.ResourceID)
			mediaType, mediaErr := normalizeText(representation.MediaType, capabilityIdentifierLimit)
			role, roleErr := normalizeText(representation.Role, capabilityIdentifierLimit)
			if representationErr != nil || resourceErr != nil || mediaErr != nil || roleErr != nil {
				return nil, errCapabilityResultInvalid
			}
			representations = append(representations, ProjectReadRepresentation{RepresentationID: representationID, ResourceID: resourceID, MediaType: mediaType, Role: role})
		}
		characters = append(characters, ProjectReadCharacter{AssetID: assetID, Name: name, VersionID: versionID, Version: character.Version, Status: status, Definition: definition, Representations: representations})
	}
	return ProjectReadResult{DomainProjectID: projectID, CanvasID: canvasID, Name: name, ProjectType: projectType, AspectRatio: aspectRatio, Revision: wire.Revision, Units: units, Characters: characters}, nil
}

func decodeProjectApplyOpsResult(payload json.RawMessage) (CapabilityResult, error) {
	var wire struct {
		DomainProjectID     string   `json:"domainProjectId"`
		BaseRevision        *int64   `json:"baseRevision"`
		CommittedRevision   *int64   `json:"committedRevision"`
		ClientMutationID    string   `json:"clientMutationId"`
		ProposalHash        string   `json:"proposalHash"`
		AppliedOperationIDs []string `json:"appliedOperationIds"`
		CandidateIDs        []string `json:"candidateIds"`
		ShotIDs             []string `json:"shotIds"`
	}
	if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error { return decoder.Decode(&wire) }, capabilityPayloadLimit) != nil || wire.BaseRevision == nil || wire.CommittedRevision == nil || *wire.BaseRevision < 0 || *wire.CommittedRevision != *wire.BaseRevision+1 || wire.AppliedOperationIDs == nil || wire.CandidateIDs == nil || wire.ShotIDs == nil || !validSHA256(wire.ProposalHash) {
		return nil, errCapabilityResultInvalid
	}
	projectID, err := normalizeIdentifier(wire.DomainProjectID, capabilityIdentifierLimit)
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
	candidateIDs, err := normalizeIdentifiers(wire.CandidateIDs, capabilityResourceLimit, true)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	shotIDs, err := normalizeIdentifiers(wire.ShotIDs, 200, true)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	return ProjectApplyOpsResult{DomainProjectID: projectID, BaseRevision: *wire.BaseRevision, CommittedRevision: *wire.CommittedRevision, ClientMutationID: mutationID, ProposalHash: wire.ProposalHash, AppliedOperationIDs: operationIDs, CandidateIDs: candidateIDs, ShotIDs: shotIDs}, nil
}

func decodeCanvasReadResult(payload json.RawMessage) (CapabilityResult, error) {
	var wire struct {
		CanvasID        string                    `json:"canvasId"`
		DomainProjectID string                    `json:"domainProjectId"`
		Revision        *int64                    `json:"revision"`
		Nodes           []json.RawMessage         `json:"nodes"`
		Edges           []json.RawMessage         `json:"edges"`
		SelectedNodeIDs []string                  `json:"selectedNodeIds"`
		Viewport        *CanvasViewport           `json:"viewport"`
		CallableModels  []CanvasCallableModelFact `json:"callableModels"`
	}
	if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error {
		return decoder.Decode(&wire)
	}, capabilityPayloadLimit) != nil || wire.Revision == nil || *wire.Revision < 0 || wire.Nodes == nil || wire.Edges == nil || wire.SelectedNodeIDs == nil || wire.Viewport == nil || wire.CallableModels == nil || !validViewport(*wire.Viewport) {
		return nil, errCapabilityResultInvalid
	}
	canvasID, err := normalizeIdentifier(wire.CanvasID, capabilityIdentifierLimit)
	if err != nil || validateRawObjectList(wire.Nodes) != nil || validateRawObjectList(wire.Edges) != nil {
		return nil, errCapabilityResultInvalid
	}
	domainProjectID := strings.TrimSpace(wire.DomainProjectID)
	if domainProjectID != wire.DomainProjectID || len(domainProjectID) > capabilityIdentifierLimit {
		return nil, errCapabilityResultInvalid
	}
	nodeIDs, err := normalizeIdentifiers(wire.SelectedNodeIDs, capabilityResourceLimit, true)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	callableModels, err := normalizeCanvasCallableModels(wire.CallableModels)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	return CanvasReadResult{
		CanvasID: canvasID, DomainProjectID: domainProjectID, Revision: *wire.Revision,
		Nodes: cloneRawMessages(wire.Nodes), Edges: cloneRawMessages(wire.Edges),
		SelectedNodeIDs: nodeIDs, Viewport: *wire.Viewport, CallableModels: callableModels,
	}, nil
}

func normalizeCanvasCallableModels(values []CanvasCallableModelFact) ([]CanvasCallableModelFact, error) {
	if len(values) > capabilityResourceLimit {
		return nil, errCapabilityResultInvalid
	}
	result := make([]CanvasCallableModelFact, 0, len(values))
	seenRecords := make(map[string]struct{}, len(values))
	seenModels := make(map[string]struct{}, len(values))
	previousKey := ""
	for _, value := range values {
		channelID, err := normalizeIdentifier(value.ChannelID, capabilityIdentifierLimit)
		if err != nil {
			return nil, errCapabilityResultInvalid
		}
		modelRecordID, err := normalizeIdentifier(value.ModelRecordID, capabilityIdentifierLimit)
		if err != nil {
			return nil, errCapabilityResultInvalid
		}
		modelKey, err := normalizeText(value.ModelKey, capabilityIdentifierLimit)
		if err != nil {
			return nil, errCapabilityResultInvalid
		}
		displayName, err := normalizeText(value.DisplayName, capabilityDisplayNameLimit)
		if err != nil {
			return nil, errCapabilityResultInvalid
		}
		billingMode, err := normalizeText(value.BillingMode, capabilityIdentifierLimit)
		if err != nil {
			return nil, errCapabilityResultInvalid
		}
		priceStrategy, err := normalizeText(value.PriceStrategy, capabilityIdentifierLimit)
		if err != nil || !validCanvasCallableCapability(value.Capability) || value.PriceTiers == nil {
			return nil, errCapabilityResultInvalid
		}
		if _, duplicate := seenRecords[modelRecordID]; duplicate {
			return nil, errCapabilityResultInvalid
		}
		seenRecords[modelRecordID] = struct{}{}
		catalogKey := channelID + "\x00" + modelKey
		if _, duplicate := seenModels[catalogKey]; duplicate || (previousKey != "" && catalogKey < previousKey) {
			return nil, errCapabilityResultInvalid
		}
		seenModels[catalogKey] = struct{}{}
		previousKey = catalogKey
		priced := value.UnitPriceMicrocredits > 0
		tiers := make([]CanvasCallableModelPriceTier, len(value.PriceTiers))
		for index, tier := range value.PriceTiers {
			if tier.UnitPriceMicrocredits <= 0 || tier.IncludedQuantity < 0 {
				return nil, errCapabilityResultInvalid
			}
			resolution := strings.TrimSpace(tier.Resolution)
			inputVariant := strings.TrimSpace(tier.InputVariant)
			usageMetric := strings.TrimSpace(tier.UsageMetric)
			if resolution != tier.Resolution || inputVariant != tier.InputVariant || usageMetric != tier.UsageMetric ||
				(resolution == "" && inputVariant == "" && usageMetric == "") {
				return nil, errCapabilityResultInvalid
			}
			tiers[index] = CanvasCallableModelPriceTier{
				Resolution: resolution, InputVariant: inputVariant, UsageMetric: usageMetric,
				IncludedQuantity: tier.IncludedQuantity, UnitPriceMicrocredits: tier.UnitPriceMicrocredits,
			}
			priced = true
		}
		if !priced {
			return nil, errCapabilityResultInvalid
		}
		var providerCapabilities json.RawMessage
		if len(value.ProviderCapabilities) > 0 {
			normalized, normalizeErr := normalizeJSONObject(value.ProviderCapabilities, capabilityParametersLimit, false)
			if normalizeErr != nil {
				return nil, errCapabilityResultInvalid
			}
			providerCapabilities = normalized
		}
		result = append(result, CanvasCallableModelFact{
			ChannelID: channelID, ModelRecordID: modelRecordID, ModelKey: modelKey, DisplayName: displayName,
			Capability: value.Capability, BillingMode: billingMode, PriceStrategy: priceStrategy,
			UnitPriceMicrocredits: value.UnitPriceMicrocredits, PriceTiers: tiers,
			ProviderCapabilities: providerCapabilities,
		})
	}
	return result, nil
}

func validCanvasCallableCapability(value string) bool {
	switch value {
	case "image", "video", "audio", "vision":
		return true
	default:
		return false
	}
}

func decodeCanvasApplyOpsResult(payload json.RawMessage) (CapabilityResult, error) {
	var wire struct {
		CanvasID            string                  `json:"canvasId"`
		BaseRevision        *int64                  `json:"baseRevision"`
		CommittedRevision   *int64                  `json:"committedRevision"`
		ClientMutationID    string                  `json:"clientMutationId"`
		ProposalHash        string                  `json:"proposalHash"`
		AppliedOperationIDs []string                `json:"appliedOperationIds"`
		Evidence            *CanvasApplyOpsEvidence `json:"evidence"`
	}
	if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error {
		return decoder.Decode(&wire)
	}, capabilityPayloadLimit) != nil || wire.BaseRevision == nil || wire.CommittedRevision == nil || *wire.BaseRevision < 0 || *wire.CommittedRevision != *wire.BaseRevision+1 || len(wire.AppliedOperationIDs) < 1 || wire.Evidence == nil || !validSHA256(wire.ProposalHash) {
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
	evidence, err := normalizeCanvasApplyOpsEvidence(*wire.Evidence)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	return CanvasApplyOpsResult{
		CanvasID: canvasID, BaseRevision: *wire.BaseRevision, CommittedRevision: *wire.CommittedRevision,
		ClientMutationID: mutationID, ProposalHash: wire.ProposalHash, AppliedOperationIDs: operationIDs, Evidence: evidence,
	}, nil
}

func normalizeCanvasApplyOpsEvidence(evidence CanvasApplyOpsEvidence) (CanvasApplyOpsEvidence, error) {
	normalize := func(values []string) ([]string, error) {
		if values == nil {
			return nil, errCapabilityResultInvalid
		}
		return normalizeIdentifiers(values, capabilityOperationLimit, true)
	}
	addedNodeIDs, err := normalize(evidence.AddedNodeIDs)
	if err != nil {
		return CanvasApplyOpsEvidence{}, err
	}
	updatedNodeIDs, err := normalize(evidence.UpdatedNodeIDs)
	if err != nil {
		return CanvasApplyOpsEvidence{}, err
	}
	deletedNodeIDs, err := normalize(evidence.DeletedNodeIDs)
	if err != nil {
		return CanvasApplyOpsEvidence{}, err
	}
	upsertedConnectionIDs, err := normalize(evidence.UpsertedConnectionIDs)
	if err != nil {
		return CanvasApplyOpsEvidence{}, err
	}
	deletedConnectionIDs, err := normalize(evidence.DeletedConnectionIDs)
	if err != nil {
		return CanvasApplyOpsEvidence{}, err
	}
	selectedNodeIDs, err := normalizeIdentifiers(evidence.SelectedNodeIDs, capabilityResourceLimit, true)
	if err != nil || evidence.SelectedNodeIDs == nil {
		return CanvasApplyOpsEvidence{}, errCapabilityResultInvalid
	}
	return CanvasApplyOpsEvidence{
		AddedNodeIDs: addedNodeIDs, UpdatedNodeIDs: updatedNodeIDs, DeletedNodeIDs: deletedNodeIDs,
		UpsertedConnectionIDs: upsertedConnectionIDs, DeletedConnectionIDs: deletedConnectionIDs,
		SelectedNodeIDs: selectedNodeIDs, ViewportApplied: evidence.ViewportApplied,
	}, nil
}

func decodeAssetsReadResult(payload json.RawMessage) (CapabilityResult, error) {
	var wire AssetsReadResult
	if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error {
		return decoder.Decode(&wire)
	}, capabilityPayloadLimit) != nil || wire.Resources == nil || len(wire.Resources) > capabilityResourceLimit {
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
	if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error {
		return decoder.Decode(&wire)
	}, capabilityPayloadLimit) != nil {
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
	if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error {
		return decoder.Decode(&wire)
	}, capabilityPayloadLimit) != nil || !wire.MediaKind.Valid() ||
		len(wire.Resources) < 1 || len(wire.Resources) > capabilityResourceLimit {
		return nil, errCapabilityResultInvalid
	}
	taskID, err := normalizeIdentifier(wire.TaskID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	billingOrderID, err := normalizeIdentifier(wire.BillingOrderID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	requestID, err := normalizeIdentifier(wire.ClientRequestID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	resources := make([]MediaGeneratedResourceResult, 0, len(wire.Resources))
	seen := make(map[string]struct{}, len(wire.Resources))
	for _, resource := range wire.Resources {
		resourceID, normalizeErr := normalizeResourceIdentifier(resource.ResourceID)
		if normalizeErr != nil || resource.Kind != wire.MediaKind || resource.URL != "/api/resources/"+resourceID+"/file" {
			return nil, errCapabilityResultInvalid
		}
		if _, exists := seen[resourceID]; exists {
			return nil, errCapabilityResultInvalid
		}
		seen[resourceID] = struct{}{}
		resources = append(resources, MediaGeneratedResourceResult{ResourceID: resourceID, Kind: resource.Kind, URL: resource.URL})
	}
	return MediaGenerateResult{
		TaskID: taskID, BillingOrderID: billingOrderID, MediaKind: wire.MediaKind,
		ClientRequestID: requestID, Resources: resources,
	}, nil
}

func decodeVisionAnalyzeResult(payload json.RawMessage) (CapabilityResult, error) {
	var wire struct {
		TaskID            string       `json:"taskId"`
		BillingOrderID    string       `json:"billingOrderId"`
		ModelRecordID     string       `json:"modelRecordId"`
		ModelKey          string       `json:"modelKey"`
		ClientRequestID   string       `json:"clientRequestId"`
		SourceResourceIDs []string     `json:"sourceResourceIds"`
		Detail            VisionDetail `json:"detail"`
		Analysis          string       `json:"analysis"`
		Usage             *struct {
			InputTokens  *int64 `json:"inputTokens"`
			CachedTokens *int64 `json:"cachedTokens"`
			OutputTokens *int64 `json:"outputTokens"`
		} `json:"usage"`
	}
	if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error {
		return decoder.Decode(&wire)
	}, agentToolResultLimit) != nil || !wire.Detail.Valid() || wire.SourceResourceIDs == nil || wire.Usage == nil ||
		wire.Usage.InputTokens == nil || wire.Usage.CachedTokens == nil || wire.Usage.OutputTokens == nil {
		return nil, errCapabilityResultInvalid
	}
	taskID, err := normalizeIdentifier(wire.TaskID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	billingOrderID, err := normalizeIdentifier(wire.BillingOrderID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	modelRecordID, err := normalizeIdentifier(wire.ModelRecordID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	modelKey, err := normalizeText(wire.ModelKey, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	requestID, err := normalizeIdentifier(wire.ClientRequestID, capabilityIdentifierLimit)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	resourceIDs, err := normalizeResourceIdentifiers(wire.SourceResourceIDs, visionResourceLimit, false)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	analysis, err := normalizeText(wire.Analysis, capabilityInstructionsLimit)
	if err != nil {
		return nil, errCapabilityResultInvalid
	}
	usage := VisionTokenUsage{
		InputTokens: *wire.Usage.InputTokens, CachedTokens: *wire.Usage.CachedTokens, OutputTokens: *wire.Usage.OutputTokens,
	}
	if usage.InputTokens < 0 || usage.CachedTokens < 0 || usage.OutputTokens < 0 || usage.CachedTokens > usage.InputTokens {
		return nil, errCapabilityResultInvalid
	}
	return VisionAnalyzeResult{
		TaskID: taskID, BillingOrderID: billingOrderID, ModelRecordID: modelRecordID, ModelKey: modelKey,
		ClientRequestID: requestID, SourceResourceIDs: resourceIDs, Detail: wire.Detail, Analysis: analysis, Usage: usage,
	}, nil
}

func decodeSkillsLoadResult(payload json.RawMessage) (CapabilityResult, error) {
	var wire SkillsLoadResult
	if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error {
		return decoder.Decode(&wire)
	}, capabilityPayloadLimit) != nil || wire.Version < 1 || !validSHA256(wire.Checksum) {
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
