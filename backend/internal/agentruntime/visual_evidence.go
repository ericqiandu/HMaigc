package agentruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

const ArtifactSchemaVisualEvidenceV1 = "visual_evidence.v1"

type VisualCharacter struct {
	Key            string   `json:"key"`
	Name           string   `json:"name"`
	Clothing       string   `json:"clothing"`
	Hair           string   `json:"hair"`
	StableFeatures []string `json:"stableFeatures"`
}

type VisualIdentityEvidence struct {
	CharacterKey string   `json:"characterKey"`
	Observations []string `json:"observations"`
}

type VisualSceneEvidence struct {
	Key         string `json:"key"`
	Description string `json:"description"`
}

type VisualPropEvidence struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type VisualSpatialRelation struct {
	SubjectKey string `json:"subjectKey"`
	Relation   string `json:"relation"`
	ObjectKey  string `json:"objectKey"`
}

type VisualShotEvidence struct {
	ShotSize            string `json:"shotSize"`
	Angle               string `json:"angle"`
	Composition         string `json:"composition"`
	ScreenDirection     string `json:"screenDirection"`
	Gaze                string `json:"gaze"`
	FirstFrameCondition string `json:"firstFrameCondition"`
	LastFrameCondition  string `json:"lastFrameCondition"`
}

type VisualEvidenceIssue struct {
	Code        string   `json:"code"`
	Description string   `json:"description"`
	RelatedKeys []string `json:"relatedKeys"`
}

type VisualEvidence struct {
	SourceRevision        ArtifactRevisionRef      `json:"sourceRevision"`
	Characters            []VisualCharacter        `json:"characters"`
	IdentityEvidence      []VisualIdentityEvidence `json:"identityEvidence"`
	Scene                 VisualSceneEvidence      `json:"scene"`
	Props                 []VisualPropEvidence     `json:"props"`
	SpatialRelations      []VisualSpatialRelation  `json:"spatialRelations"`
	Shot                  VisualShotEvidence       `json:"shot"`
	ActionState           string                   `json:"actionState"`
	OCRText               []string                 `json:"ocrText"`
	Uncertainties         []VisualEvidenceIssue    `json:"uncertainties"`
	Conflicts             []VisualEvidenceIssue    `json:"conflicts"`
	ConfidenceBasisPoints int                      `json:"confidenceBasisPoints"`
	VisionModelRecordID   string                   `json:"visionModelRecordId"`
	RequestIdentity       string                   `json:"requestIdentity"`
}

func DecodeVisualEvidence(payload []byte) (VisualEvidence, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var evidence VisualEvidence
	if err := decoder.Decode(&evidence); err != nil {
		return VisualEvidence{}, ErrArtifactPayloadInvalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return VisualEvidence{}, ErrArtifactPayloadInvalid
	}
	if err := evidence.Validate(); err != nil {
		return VisualEvidence{}, err
	}
	return evidence, nil
}

func (evidence VisualEvidence) Validate() error {
	if evidence.SourceRevision.Validate() != nil || evidence.Characters == nil || evidence.IdentityEvidence == nil ||
		evidence.Props == nil || evidence.SpatialRelations == nil || evidence.OCRText == nil ||
		evidence.Uncertainties == nil || evidence.Conflicts == nil ||
		!validArtifactText(evidence.Scene.Key, 120) || !validArtifactText(evidence.Scene.Description, 8*1024) ||
		!validArtifactText(evidence.ActionState, 8*1024) || evidence.ConfidenceBasisPoints < 0 ||
		evidence.ConfidenceBasisPoints > 10_000 || !validArtifactText(evidence.VisionModelRecordID, 120) ||
		!validArtifactText(evidence.RequestIdentity, 180) || validateVisualShot(evidence.Shot) != nil ||
		!validVisualTextList(evidence.OCRText, 4*1024) {
		return ErrArtifactPayloadInvalid
	}

	stableKeys := map[string]struct{}{evidence.Scene.Key: {}}
	characterKeys := make(map[string]struct{}, len(evidence.Characters))
	for _, character := range evidence.Characters {
		if !validArtifactText(character.Key, 120) || !validArtifactText(character.Name, 240) ||
			!validArtifactText(character.Clothing, 4*1024) || !validArtifactText(character.Hair, 4*1024) ||
			character.StableFeatures == nil || !validVisualTextList(character.StableFeatures, 4*1024) {
			return ErrArtifactPayloadInvalid
		}
		if _, exists := stableKeys[character.Key]; exists {
			return ErrArtifactPayloadInvalid
		}
		stableKeys[character.Key] = struct{}{}
		characterKeys[character.Key] = struct{}{}
	}
	for _, prop := range evidence.Props {
		if !validArtifactText(prop.Key, 120) || !validArtifactText(prop.Name, 240) || !validArtifactText(prop.Description, 4*1024) {
			return ErrArtifactPayloadInvalid
		}
		if _, exists := stableKeys[prop.Key]; exists {
			return ErrArtifactPayloadInvalid
		}
		stableKeys[prop.Key] = struct{}{}
	}

	identityKeys := make(map[string]struct{}, len(evidence.IdentityEvidence))
	for _, identity := range evidence.IdentityEvidence {
		if _, exists := characterKeys[identity.CharacterKey]; !exists || identity.Observations == nil ||
			len(identity.Observations) == 0 || !validVisualTextList(identity.Observations, 4*1024) {
			return ErrArtifactPayloadInvalid
		}
		if _, exists := identityKeys[identity.CharacterKey]; exists {
			return ErrArtifactPayloadInvalid
		}
		identityKeys[identity.CharacterKey] = struct{}{}
	}
	for _, relation := range evidence.SpatialRelations {
		if _, exists := stableKeys[relation.SubjectKey]; !exists {
			return ErrArtifactPayloadInvalid
		}
		if _, exists := stableKeys[relation.ObjectKey]; !exists || !validArtifactText(relation.Relation, 240) {
			return ErrArtifactPayloadInvalid
		}
	}
	if validateVisualEvidenceIssues(evidence.Uncertainties, stableKeys) != nil ||
		validateVisualEvidenceIssues(evidence.Conflicts, stableKeys) != nil {
		return ErrArtifactPayloadInvalid
	}
	return nil
}

func validateVisualShot(shot VisualShotEvidence) error {
	for _, value := range []string{
		shot.ShotSize, shot.Angle, shot.Composition, shot.ScreenDirection,
		shot.Gaze, shot.FirstFrameCondition, shot.LastFrameCondition,
	} {
		if !validArtifactText(value, 4*1024) {
			return ErrArtifactPayloadInvalid
		}
	}
	return nil
}

func validVisualTextList(values []string, maximumLength int) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validArtifactText(value, maximumLength) {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func validateVisualEvidenceIssues(issues []VisualEvidenceIssue, stableKeys map[string]struct{}) error {
	issueCodes := make(map[string]struct{}, len(issues))
	for _, issue := range issues {
		if !validArtifactText(issue.Code, 120) || !validArtifactText(issue.Description, 4*1024) ||
			issue.RelatedKeys == nil {
			return ErrArtifactPayloadInvalid
		}
		if _, exists := issueCodes[issue.Code]; exists {
			return ErrArtifactPayloadInvalid
		}
		issueCodes[issue.Code] = struct{}{}
		relatedKeys := make(map[string]struct{}, len(issue.RelatedKeys))
		for _, key := range issue.RelatedKeys {
			if _, exists := stableKeys[key]; !exists {
				return ErrArtifactPayloadInvalid
			}
			if _, exists := relatedKeys[key]; exists {
				return ErrArtifactPayloadInvalid
			}
			relatedKeys[key] = struct{}{}
		}
	}
	return nil
}
