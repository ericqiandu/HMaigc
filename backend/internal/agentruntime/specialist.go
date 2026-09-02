package agentruntime

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	maxSpecialistObjectiveBytes = 16 * 1024
	maxSpecialistSkills         = 8
	maxSpecialistTools          = 8
	maxCapabilitySchemas        = 16
)

var (
	ErrSpecialistRequestInvalid       = errors.New("specialist request is invalid")
	ErrSpecialistModelInheritance     = errors.New("specialist model inheritance failed")
	ErrSkillCapabilityManifestInvalid = errors.New("skill capability manifest is invalid")
	ErrSkillCapabilityMismatch        = errors.New("skill capability manifest does not authorize specialist request")
)

// SkillCapabilityManifest is a published, immutable fact. It declares where a
// Skill may be loaded and which deterministic capabilities it may authorize;
// runtime code never infers these facts from the Skill instructions.
type SkillCapabilityManifest struct {
	Specialists     []SpecialistKey `json:"specialists"`
	Tools           []AgentToolName `json:"tools"`
	ArtifactSchemas []string        `json:"artifactSchemas"`
}

// SpecialistDefinition freezes identity and contract version only. The main
// Agent remains responsible for semantic routing and stage ordering.
type SpecialistDefinition struct {
	Key     SpecialistKey `json:"key"`
	Version int           `json:"version"`
}

var specialistRegistry = []SpecialistDefinition{
	{Key: SpecialistAsset, Version: 1},
	{Key: SpecialistAudio, Version: 1},
	{Key: SpecialistNarrative, Version: 1},
	{Key: SpecialistStoryboard, Version: 1},
	{Key: SpecialistVideoAssembly, Version: 1},
	{Key: SpecialistVisual, Version: 1},
}

func SpecialistDefinitions() []SpecialistDefinition {
	definitions := make([]SpecialistDefinition, len(specialistRegistry))
	copy(definitions, specialistRegistry)
	return definitions
}

func SpecialistDefinitionFor(key SpecialistKey, version int) (SpecialistDefinition, bool) {
	index := sort.Search(len(specialistRegistry), func(index int) bool {
		return specialistRegistry[index].Key >= key
	})
	if index >= len(specialistRegistry) || specialistRegistry[index].Key != key || specialistRegistry[index].Version != version {
		return SpecialistDefinition{}, false
	}
	return specialistRegistry[index], true
}

type SpecialistRequest struct {
	SpecialistRunID       string                `json:"specialistRunId"`
	ParentSpecialistRunID string                `json:"parentSpecialistRunId,omitempty"`
	StageID               string                `json:"stageId"`
	SpecialistKey         SpecialistKey         `json:"specialistKey"`
	SpecialistVersion     int                   `json:"specialistVersion"`
	ParentModelRecordID   string                `json:"parentModelRecordId"`
	ParentModelKey        string                `json:"parentModelKey"`
	Objective             string                `json:"objective"`
	InputRevisions        []ArtifactRevisionRef `json:"inputRevisions"`
	LoadedSkills          []SkillSelection      `json:"loadedSkills"`
	ToolAllowlist         []AgentToolName       `json:"toolAllowlist"`
	ExpectedOutputSchema  string                `json:"expectedOutputSchema"`
	ExpectedDelivery      ExpectedDelivery      `json:"expectedDelivery"`
}

type RequiredAction struct {
	ActionType string `json:"actionType"`
	TargetKey  string `json:"targetKey,omitempty"`
	Rationale  string `json:"rationale"`
}

type SpecialistResult struct {
	Summary     string           `json:"summary"`
	Artifacts   []ArtifactDraft  `json:"artifacts"`
	Delivery    DeliveryEvidence `json:"delivery"`
	NextActions []RequiredAction `json:"nextActions"`
}

func ValidateSkillCapabilityManifest(manifest SkillCapabilityManifest) error {
	if len(manifest.Specialists) == 0 || len(manifest.Specialists) > len(specialistRegistry) ||
		len(manifest.Tools) > maxSpecialistTools || len(manifest.ArtifactSchemas) == 0 || len(manifest.ArtifactSchemas) > maxCapabilitySchemas {
		return ErrSkillCapabilityManifestInvalid
	}
	previousSpecialist := SpecialistKey("")
	for _, specialist := range manifest.Specialists {
		if !specialist.Valid() || (previousSpecialist != "" && specialist <= previousSpecialist) {
			return ErrSkillCapabilityManifestInvalid
		}
		previousSpecialist = specialist
	}
	previousTool := AgentToolName("")
	for _, tool := range manifest.Tools {
		if !validSkillCapabilityToolName(tool) || (previousTool != "" && tool <= previousTool) {
			return ErrSkillCapabilityManifestInvalid
		}
		previousTool = tool
	}
	previousSchema := ""
	for _, schema := range manifest.ArtifactSchemas {
		if !validArtifactSchemaName(schema) || (previousSchema != "" && schema <= previousSchema) {
			return ErrSkillCapabilityManifestInvalid
		}
		previousSchema = schema
	}
	return nil
}

func ValidateSpecialistRequest(request SpecialistRequest, parentModelRecordID string, parentModelKey string) error {
	if strings.TrimSpace(parentModelRecordID) == "" || strings.TrimSpace(parentModelKey) == "" ||
		request.ParentModelRecordID != parentModelRecordID || request.ParentModelKey != parentModelKey {
		return ErrSpecialistModelInheritance
	}
	if strings.TrimSpace(request.SpecialistRunID) != request.SpecialistRunID || request.SpecialistRunID == "" || len(request.SpecialistRunID) > 80 ||
		strings.TrimSpace(request.ParentSpecialistRunID) != request.ParentSpecialistRunID || len(request.ParentSpecialistRunID) > 80 ||
		strings.TrimSpace(request.StageID) != request.StageID || request.StageID == "" || len(request.StageID) > 80 ||
		strings.TrimSpace(request.Objective) != request.Objective || request.Objective == "" || len(request.Objective) > maxSpecialistObjectiveBytes ||
		strings.TrimSpace(request.ExpectedOutputSchema) != request.ExpectedOutputSchema || !validArtifactSchemaName(request.ExpectedOutputSchema) {
		return ErrSpecialistRequestInvalid
	}
	if _, found := SpecialistDefinitionFor(request.SpecialistKey, request.SpecialistVersion); !found {
		return ErrSpecialistRequestInvalid
	}
	if err := validateArtifactRevisionRefs(request.InputRevisions); err != nil {
		return fmt.Errorf("%w: input revisions: %v", ErrSpecialistRequestInvalid, err)
	}
	if len(request.LoadedSkills) == 0 || len(request.LoadedSkills) > maxSpecialistSkills {
		return ErrSpecialistRequestInvalid
	}
	if err := ValidateRunConfiguration(RunConfiguration{ExecutionMode: ExecutionGuided, Skills: request.LoadedSkills}); err != nil {
		return fmt.Errorf("%w: loaded skills: %v", ErrSpecialistRequestInvalid, err)
	}
	if err := ValidateSkillSelectionsForSpecialist(request.LoadedSkills, request.SpecialistKey); err != nil {
		return err
	}
	declaredTools := make(map[AgentToolName]struct{})
	declaresExpectedSchema := false
	for _, skill := range request.LoadedSkills {
		for _, tool := range skill.CapabilityManifest.Tools {
			declaredTools[tool] = struct{}{}
		}
		if containsString(skill.CapabilityManifest.ArtifactSchemas, request.ExpectedOutputSchema) {
			declaresExpectedSchema = true
		}
	}
	if !declaresExpectedSchema || len(request.ToolAllowlist) > maxSpecialistTools {
		return ErrSkillCapabilityMismatch
	}
	previousTool := AgentToolName("")
	for _, tool := range request.ToolAllowlist {
		if !validSkillCapabilityToolName(tool) || (previousTool != "" && tool <= previousTool) {
			return ErrSpecialistRequestInvalid
		}
		if _, declared := declaredTools[tool]; !declared {
			return ErrSkillCapabilityMismatch
		}
		previousTool = tool
	}
	if err := request.ExpectedDelivery.Validate(); err != nil {
		return fmt.Errorf("%w: expected delivery: %v", ErrSpecialistRequestInvalid, err)
	}
	return nil
}

func ValidateSkillSelectionsForSpecialist(skills []SkillSelection, specialist SpecialistKey) error {
	if !specialist.Valid() || len(skills) == 0 || len(skills) > maxSpecialistSkills {
		return ErrSkillCapabilityMismatch
	}
	for _, skill := range skills {
		if err := ValidateSkillCapabilityManifest(skill.CapabilityManifest); err != nil || !manifestContainsSpecialist(skill.CapabilityManifest, specialist) {
			return ErrSkillCapabilityMismatch
		}
	}
	return nil
}

// validSkillCapabilityToolName keeps immutable historical manifests readable
// while allowing newly published manifests to authorize only current atomic
// capabilities. Active runtime policy still decides which schema can execute.
func validSkillCapabilityToolName(tool AgentToolName) bool {
	switch tool {
	case ToolSpecialistDelegate, ToolVisionAnalyze, ToolMediaGenerate, ToolMediaAssemble, ToolCanvasProject,
		ToolCanvasRead, ToolCanvasApplyOps, ToolAssetsRead, ToolAssetsPublish, ToolSkillsLoad:
		return true
	default:
		return false
	}
}

func validArtifactSchemaName(value string) bool {
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

func manifestContainsSpecialist(manifest SkillCapabilityManifest, specialist SpecialistKey) bool {
	index := sort.Search(len(manifest.Specialists), func(index int) bool { return manifest.Specialists[index] >= specialist })
	return index < len(manifest.Specialists) && manifest.Specialists[index] == specialist
}

func containsString(values []string, value string) bool {
	index := sort.SearchStrings(values, value)
	return index < len(values) && values[index] == value
}
