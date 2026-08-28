package agentruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

const ArtifactSchemaCharacterVisualBibleV1 = "character_visual_bible.v1"

type CharacterVisualFact struct {
	FactKey      string                `json:"factKey"`
	Description  string                `json:"description"`
	EvidenceRefs []ArtifactRevisionRef `json:"evidenceRefs"`
}

type CharacterDynamicFeature struct {
	FeatureKey   string                `json:"featureKey"`
	ScopeKey     string                `json:"scopeKey"`
	Description  string                `json:"description"`
	EvidenceRefs []ArtifactRevisionRef `json:"evidenceRefs"`
}

type CharacterVisualProfile struct {
	CharacterKey       string                    `json:"characterKey"`
	CanonicalName      string                    `json:"canonicalName"`
	Aliases            []string                  `json:"aliases"`
	StaticFeatures     []CharacterVisualFact     `json:"staticFeatures"`
	DynamicFeatures    []CharacterDynamicFeature `json:"dynamicFeatures"`
	ReferenceRevisions []ArtifactRevisionRef     `json:"referenceRevisions"`
	VoiceRoleKey       string                    `json:"voiceRoleKey,omitempty"`
	VoiceAssetRevision *ArtifactRevisionRef      `json:"voiceAssetRevision,omitempty"`
	Unknowns           []VisualEvidenceIssue     `json:"unknowns"`
	Conflicts          []VisualEvidenceIssue     `json:"conflicts"`
}

type CharacterVisualBible struct {
	ScriptRevision          ArtifactRevisionRef      `json:"scriptRevision"`
	AssetBindingRevision    ArtifactRevisionRef      `json:"assetBindingRevision"`
	VisualEvidenceRevisions []ArtifactRevisionRef    `json:"visualEvidenceRevisions"`
	ReferenceAssetRevisions []ArtifactRevisionRef    `json:"referenceAssetRevisions"`
	Characters              []CharacterVisualProfile `json:"characters"`
}

func DecodeCharacterVisualBible(payload []byte) (CharacterVisualBible, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var bible CharacterVisualBible
	if err := decoder.Decode(&bible); err != nil {
		return CharacterVisualBible{}, ErrArtifactPayloadInvalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return CharacterVisualBible{}, ErrArtifactPayloadInvalid
	}
	if err := ValidateCharacterVisualBible(bible); err != nil {
		return CharacterVisualBible{}, err
	}
	return bible, nil
}

func ValidateCharacterVisualBible(bible CharacterVisualBible) error {
	inputs := characterVisualBibleInputs(bible)
	if bible.ScriptRevision.Validate() != nil || bible.AssetBindingRevision.Validate() != nil ||
		len(bible.VisualEvidenceRevisions) == 0 || len(bible.ReferenceAssetRevisions) == 0 ||
		len(bible.Characters) == 0 || validateArtifactRevisionRefs(inputs) != nil {
		return ErrArtifactPayloadInvalid
	}
	allowedEvidence := artifactRevisionRefSet(bible.VisualEvidenceRevisions)
	allowedReferences := artifactRevisionRefSet(bible.ReferenceAssetRevisions)
	characterKeys := make(map[string]struct{}, len(bible.Characters))
	for _, character := range bible.Characters {
		if !validArtifactText(character.CharacterKey, 120) || !validArtifactText(character.CanonicalName, 240) ||
			character.Aliases == nil || character.StaticFeatures == nil || len(character.StaticFeatures) == 0 ||
			character.DynamicFeatures == nil || len(character.ReferenceRevisions) == 0 || character.Unknowns == nil ||
			character.Conflicts == nil || !validOptionalArtifactText(character.VoiceRoleKey, 120) ||
			!validVisualTextList(character.Aliases, 240) || !artifactRevisionRefsBelongTo(character.ReferenceRevisions, allowedReferences) {
			return ErrArtifactPayloadInvalid
		}
		if _, exists := characterKeys[character.CharacterKey]; exists {
			return ErrArtifactPayloadInvalid
		}
		characterKeys[character.CharacterKey] = struct{}{}
		knownKeys := map[string]struct{}{character.CharacterKey: {}}
		for _, fact := range character.StaticFeatures {
			if !validArtifactText(fact.FactKey, 120) || !validArtifactText(fact.Description, 4*1024) ||
				len(fact.EvidenceRefs) == 0 || !artifactRevisionRefsBelongTo(fact.EvidenceRefs, allowedEvidence) {
				return ErrArtifactPayloadInvalid
			}
			if _, exists := knownKeys[fact.FactKey]; exists {
				return ErrArtifactPayloadInvalid
			}
			knownKeys[fact.FactKey] = struct{}{}
		}
		for _, feature := range character.DynamicFeatures {
			if !validArtifactText(feature.FeatureKey, 120) || !validArtifactText(feature.ScopeKey, 120) ||
				!validArtifactText(feature.Description, 4*1024) || len(feature.EvidenceRefs) == 0 ||
				!artifactRevisionRefsBelongTo(feature.EvidenceRefs, allowedEvidence) {
				return ErrArtifactPayloadInvalid
			}
			if _, exists := knownKeys[feature.FeatureKey]; exists {
				return ErrArtifactPayloadInvalid
			}
			knownKeys[feature.FeatureKey] = struct{}{}
		}
		if character.VoiceAssetRevision != nil {
			if _, exists := allowedReferences[*character.VoiceAssetRevision]; !exists {
				return ErrArtifactPayloadInvalid
			}
		}
		if validateVisualEvidenceIssues(character.Unknowns, knownKeys) != nil ||
			validateVisualEvidenceIssues(character.Conflicts, knownKeys) != nil {
			return ErrArtifactPayloadInvalid
		}
	}
	return nil
}

func characterVisualBibleInputs(bible CharacterVisualBible) []ArtifactRevisionRef {
	inputs := []ArtifactRevisionRef{bible.ScriptRevision, bible.AssetBindingRevision}
	inputs = append(inputs, bible.VisualEvidenceRevisions...)
	return append(inputs, bible.ReferenceAssetRevisions...)
}
