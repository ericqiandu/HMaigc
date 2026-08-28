package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
)

var (
	ErrAgentRuntimeToolArgumentsInvalid = errors.New("agent runtime tool arguments are invalid")
	ErrSpecialistToolNotAllowed         = errors.New("specialist tool is not allowed by frozen skills")
)

type SpecialistDelegateArguments struct {
	SpecialistKey        agentruntime.SpecialistKey         `json:"specialistKey"`
	Objective            string                             `json:"objective"`
	InputRevisions       []agentruntime.ArtifactRevisionRef `json:"inputRevisions"`
	SkillDirs            []string                           `json:"skillDirs"`
	ToolAllowlist        []agentruntime.AgentToolName       `json:"toolAllowlist"`
	ExpectedOutputSchema string                             `json:"expectedOutputSchema"`
	ExpectedDelivery     agentruntime.ExpectedDelivery      `json:"expectedDelivery"`
}

type VisionAnalyzeArguments struct {
	InputRevisions       []agentruntime.ArtifactRevisionRef `json:"inputRevisions"`
	ResourceIDs          []string                           `json:"resourceIds"`
	ExpectedOutputSchema string                             `json:"expectedOutputSchema"`
	ExpectedDelivery     agentruntime.ExpectedDelivery      `json:"expectedDelivery"`
}

type MediaGenerateArguments struct {
	InputRevisions       []agentruntime.ArtifactRevisionRef    `json:"inputRevisions"`
	GenerationModel      agentruntime.GenerationModelSelection `json:"generationModel"`
	Capability           string                                `json:"capability"`
	Parameters           json.RawMessage                       `json:"parameters"`
	OutputArtifactKey    string                                `json:"outputArtifactKey"`
	ExpectedOutputSchema string                                `json:"expectedOutputSchema"`
	ExpectedDelivery     agentruntime.ExpectedDelivery         `json:"expectedDelivery"`
}

type CanvasProjectArguments struct {
	ArtifactRevisions []agentruntime.ArtifactRevisionRef `json:"artifactRevisions"`
	BaseRevision      int64                              `json:"baseRevision"`
	ExpectedDelivery  agentruntime.ExpectedDelivery      `json:"expectedDelivery"`
}

func decodeVisionAnalyzeArguments(payload []byte) (VisionAnalyzeArguments, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var arguments VisionAnalyzeArguments
	if err := decoder.Decode(&arguments); err != nil {
		return VisionAnalyzeArguments{}, ErrAgentRuntimeToolArgumentsInvalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return VisionAnalyzeArguments{}, ErrAgentRuntimeToolArgumentsInvalid
	}
	if len(arguments.InputRevisions) != 1 || len(arguments.ResourceIDs) != 1 ||
		arguments.ExpectedOutputSchema != agentruntime.ArtifactSchemaVisualEvidenceV1 ||
		validateAgentRuntimeRevisionRefs(arguments.InputRevisions) != nil || arguments.ExpectedDelivery.Validate() != nil {
		return VisionAnalyzeArguments{}, ErrAgentRuntimeToolArgumentsInvalid
	}
	if strings.TrimSpace(arguments.ResourceIDs[0]) != arguments.ResourceIDs[0] || arguments.ResourceIDs[0] == "" ||
		len(arguments.ResourceIDs[0]) > 80 ||
		(arguments.ExpectedDelivery.Kind != agentruntime.DeliveryGeneratedAsset && arguments.ExpectedDelivery.Kind != agentruntime.DeliveryMixed) {
		return VisionAnalyzeArguments{}, ErrAgentRuntimeToolArgumentsInvalid
	}
	return arguments, nil
}

func decodeMediaGenerateArguments(payload []byte) (MediaGenerateArguments, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var arguments MediaGenerateArguments
	if err := decoder.Decode(&arguments); err != nil {
		return MediaGenerateArguments{}, ErrAgentRuntimeToolArgumentsInvalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return MediaGenerateArguments{}, ErrAgentRuntimeToolArgumentsInvalid
	}
	if arguments.InputRevisions == nil {
		arguments.InputRevisions = []agentruntime.ArtifactRevisionRef{}
	}
	parameters := bytes.TrimSpace(arguments.Parameters)
	if strings.TrimSpace(arguments.GenerationModel.ChannelID) != arguments.GenerationModel.ChannelID || arguments.GenerationModel.ChannelID == "" ||
		strings.TrimSpace(arguments.GenerationModel.Model) != arguments.GenerationModel.Model || arguments.GenerationModel.Model == "" ||
		(arguments.Capability != "image" && arguments.Capability != "video" && arguments.Capability != "audio") ||
		!validAgentRuntimeContractName(arguments.OutputArtifactKey) || !validAgentRuntimeContractName(arguments.ExpectedOutputSchema) ||
		len(parameters) < 2 || parameters[0] != '{' || !json.Valid(parameters) ||
		validateAgentRuntimeRevisionRefs(arguments.InputRevisions) != nil || arguments.ExpectedDelivery.Validate() != nil ||
		(arguments.ExpectedDelivery.Kind != agentruntime.DeliveryGeneratedAsset && arguments.ExpectedDelivery.Kind != agentruntime.DeliveryMixed) {
		return MediaGenerateArguments{}, ErrAgentRuntimeToolArgumentsInvalid
	}
	arguments.Parameters = append(json.RawMessage(nil), parameters...)
	return arguments, nil
}

func decodeCanvasProjectArguments(payload []byte) (CanvasProjectArguments, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var arguments CanvasProjectArguments
	if err := decoder.Decode(&arguments); err != nil {
		return CanvasProjectArguments{}, ErrAgentRuntimeToolArgumentsInvalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return CanvasProjectArguments{}, ErrAgentRuntimeToolArgumentsInvalid
	}
	if len(arguments.ArtifactRevisions) == 0 || len(arguments.ArtifactRevisions) > 64 || arguments.BaseRevision < 0 ||
		validateAgentRuntimeRevisionRefs(arguments.ArtifactRevisions) != nil || arguments.ExpectedDelivery.Validate() != nil ||
		(arguments.ExpectedDelivery.Kind != agentruntime.DeliveryCanvasChange && arguments.ExpectedDelivery.Kind != agentruntime.DeliveryMixed) {
		return CanvasProjectArguments{}, ErrAgentRuntimeToolArgumentsInvalid
	}
	return arguments, nil
}

func freezeSpecialistDelegateArguments(
	configuration agentruntime.RunConfiguration,
	loadedSkillDirs []string,
	payload []byte,
) (SpecialistDelegateArguments, []agentruntime.SkillSelection, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var arguments SpecialistDelegateArguments
	if err := decoder.Decode(&arguments); err != nil {
		return SpecialistDelegateArguments{}, nil, ErrAgentRuntimeToolArgumentsInvalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return SpecialistDelegateArguments{}, nil, ErrAgentRuntimeToolArgumentsInvalid
	}
	if arguments.InputRevisions == nil {
		arguments.InputRevisions = []agentruntime.ArtifactRevisionRef{}
	}
	if arguments.ToolAllowlist == nil {
		arguments.ToolAllowlist = []agentruntime.AgentToolName{}
	}
	if len(arguments.SkillDirs) == 0 || len(arguments.SkillDirs) > 8 ||
		strings.TrimSpace(arguments.Objective) != arguments.Objective || arguments.Objective == "" ||
		strings.TrimSpace(arguments.ExpectedOutputSchema) != arguments.ExpectedOutputSchema || arguments.ExpectedOutputSchema == "" {
		return SpecialistDelegateArguments{}, nil, ErrAgentRuntimeToolArgumentsInvalid
	}
	if !sort.StringsAreSorted(arguments.SkillDirs) || !strictUniqueStrings(arguments.SkillDirs) {
		return SpecialistDelegateArguments{}, nil, ErrAgentRuntimeToolArgumentsInvalid
	}
	loaded := make(map[string]struct{}, len(loadedSkillDirs))
	for _, dir := range loadedSkillDirs {
		if strings.TrimSpace(dir) != dir || dir == "" {
			return SpecialistDelegateArguments{}, nil, ErrAgentRuntimeToolArgumentsInvalid
		}
		loaded[dir] = struct{}{}
	}
	selectedByDir := make(map[string]agentruntime.SkillSelection, len(configuration.Skills))
	for _, skill := range configuration.Skills {
		selectedByDir[skill.Dir] = skill
	}
	frozenSkills := make([]agentruntime.SkillSelection, 0, len(arguments.SkillDirs))
	declaredTools := make(map[agentruntime.AgentToolName]struct{})
	for _, dir := range arguments.SkillDirs {
		skill, selected := selectedByDir[dir]
		_, isLoaded := loaded[dir]
		if !selected || !isLoaded {
			return SpecialistDelegateArguments{}, nil, ErrAgentRuntimeToolArgumentsInvalid
		}
		frozenSkills = append(frozenSkills, cloneAgentRuntimeSkillSelection(skill))
		for _, tool := range skill.CapabilityManifest.Tools {
			declaredTools[tool] = struct{}{}
		}
	}
	for _, tool := range arguments.ToolAllowlist {
		if _, allowed := declaredTools[tool]; !allowed {
			return SpecialistDelegateArguments{}, nil, ErrSpecialistToolNotAllowed
		}
	}
	definition, found := agentruntime.SpecialistDefinitionFor(arguments.SpecialistKey, 1)
	if !found {
		return SpecialistDelegateArguments{}, nil, ErrAgentRuntimeToolArgumentsInvalid
	}
	request := agentruntime.SpecialistRequest{
		SpecialistRunID:      "delegate-validation",
		StageID:              "delegate-stage-validation",
		SpecialistKey:        definition.Key,
		SpecialistVersion:    definition.Version,
		ParentModelRecordID:  "delegate-parent-model-record",
		ParentModelKey:       "delegate-parent-model",
		Objective:            arguments.Objective,
		InputRevisions:       arguments.InputRevisions,
		LoadedSkills:         frozenSkills,
		ToolAllowlist:        arguments.ToolAllowlist,
		ExpectedOutputSchema: arguments.ExpectedOutputSchema,
		ExpectedDelivery:     arguments.ExpectedDelivery,
	}
	if err := agentruntime.ValidateSpecialistRequest(request, request.ParentModelRecordID, request.ParentModelKey); err != nil {
		if errors.Is(err, agentruntime.ErrSkillCapabilityMismatch) {
			return SpecialistDelegateArguments{}, nil, ErrSpecialistToolNotAllowed
		}
		return SpecialistDelegateArguments{}, nil, errors.Join(ErrAgentRuntimeToolArgumentsInvalid, err)
	}
	return arguments, frozenSkills, nil
}

func strictUniqueStrings(values []string) bool {
	previous := ""
	for _, value := range values {
		if strings.TrimSpace(value) != value || value == "" || value == previous {
			return false
		}
		previous = value
	}
	return true
}

func validateAgentRuntimeRevisionRefs(references []agentruntime.ArtifactRevisionRef) error {
	seen := make(map[string]struct{}, len(references))
	for _, reference := range references {
		if err := reference.Validate(); err != nil {
			return err
		}
		identity := reference.ArtifactID + "\x00" + reference.RevisionID
		if _, duplicate := seen[identity]; duplicate {
			return ErrAgentRuntimeToolArgumentsInvalid
		}
		seen[identity] = struct{}{}
	}
	return nil
}

func validAgentRuntimeContractName(value string) bool {
	if value == "" || len(value) > 120 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '_' || character == '-' || character == '.' {
			continue
		}
		return false
	}
	return true
}

func cloneAgentRuntimeSkillSelection(skill agentruntime.SkillSelection) agentruntime.SkillSelection {
	cloned := skill
	cloned.CapabilityManifest.Specialists = append([]agentruntime.SpecialistKey(nil), skill.CapabilityManifest.Specialists...)
	cloned.CapabilityManifest.Tools = append([]agentruntime.AgentToolName(nil), skill.CapabilityManifest.Tools...)
	cloned.CapabilityManifest.ArtifactSchemas = append([]string(nil), skill.CapabilityManifest.ArtifactSchemas...)
	return cloned
}
