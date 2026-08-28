package agentruntime

import (
	"errors"
	"strings"
)

type DeliveryKind string

const (
	DeliveryAnswer         DeliveryKind = "answer"
	DeliveryCanvasChange   DeliveryKind = "canvas_change"
	DeliveryGeneratedAsset DeliveryKind = "generated_asset"
	DeliveryMixed          DeliveryKind = "mixed"
)

type ArtifactKind string

const (
	ArtifactImage          ArtifactKind = "image"
	ArtifactVideo          ArtifactKind = "video"
	ArtifactAudio          ArtifactKind = "audio"
	ArtifactText           ArtifactKind = "text"
	ArtifactCanvasRevision ArtifactKind = "canvas_revision"
)

func (kind ArtifactKind) Valid() bool {
	switch kind {
	case ArtifactImage, ArtifactVideo, ArtifactAudio, ArtifactText, ArtifactCanvasRevision:
		return true
	default:
		return false
	}
}

type DeliveryFact string

const (
	DeliveryFactFinalMessage     DeliveryFact = "final_message"
	DeliveryFactCanvasRevision   DeliveryFact = "canvas_revision"
	DeliveryFactArtifact         DeliveryFact = "artifact"
	DeliveryFactArtifactRevision DeliveryFact = "artifact_revision"
	DeliveryFactResource         DeliveryFact = "resource"
	DeliveryFactPublication      DeliveryFact = "publication"
)

type DeliveryCriterion struct {
	Fact     DeliveryFact `json:"fact"`
	Artifact ArtifactKind `json:"artifact,omitempty"`
}

type ExpectedDelivery struct {
	Kind               DeliveryKind        `json:"kind"`
	RequiredArtifacts  []ArtifactKind      `json:"requiredArtifacts,omitempty"`
	TargetCanvasID     string              `json:"targetCanvasId,omitempty"`
	CompletionCriteria []DeliveryCriterion `json:"completionCriteria"`
}

func (expected ExpectedDelivery) Equal(other ExpectedDelivery) bool {
	if expected.Kind != other.Kind || expected.TargetCanvasID != other.TargetCanvasID ||
		len(expected.RequiredArtifacts) != len(other.RequiredArtifacts) ||
		len(expected.CompletionCriteria) != len(other.CompletionCriteria) {
		return false
	}
	for index := range expected.RequiredArtifacts {
		if expected.RequiredArtifacts[index] != other.RequiredArtifacts[index] {
			return false
		}
	}
	for index := range expected.CompletionCriteria {
		if expected.CompletionCriteria[index] != other.CompletionCriteria[index] {
			return false
		}
	}
	return true
}

func (expected ExpectedDelivery) Validate() error {
	if expected.Kind != DeliveryAnswer && expected.Kind != DeliveryCanvasChange && expected.Kind != DeliveryGeneratedAsset && expected.Kind != DeliveryMixed {
		return errors.New("expected delivery kind is invalid")
	}
	if len(expected.CompletionCriteria) == 0 || len(expected.CompletionCriteria) > 16 {
		return errors.New("expected delivery criteria are invalid")
	}
	if (expected.Kind == DeliveryCanvasChange || expected.Kind == DeliveryMixed) && strings.TrimSpace(expected.TargetCanvasID) == "" {
		return errors.New("expected delivery canvas is required")
	}
	required := make(map[ArtifactKind]struct{}, len(expected.RequiredArtifacts))
	for _, artifact := range expected.RequiredArtifacts {
		if !artifact.Valid() {
			return errors.New("expected delivery artifact is invalid")
		}
		if _, duplicate := required[artifact]; duplicate {
			return errors.New("expected delivery artifact is duplicated")
		}
		required[artifact] = struct{}{}
	}
	criteriaArtifacts := make(map[ArtifactKind]struct{})
	hasFinalMessage := false
	hasCanvasRevision := false
	for _, criterion := range expected.CompletionCriteria {
		switch criterion.Fact {
		case DeliveryFactFinalMessage:
			hasFinalMessage = true
			if criterion.Artifact != "" {
				return errors.New("expected delivery criterion artifact is unexpected")
			}
		case DeliveryFactCanvasRevision:
			hasCanvasRevision = true
			if criterion.Artifact != "" {
				return errors.New("expected delivery criterion artifact is unexpected")
			}
		case DeliveryFactArtifact, DeliveryFactArtifactRevision, DeliveryFactResource, DeliveryFactPublication:
			if !criterion.Artifact.Valid() {
				return errors.New("expected delivery criterion artifact is required")
			}
			criteriaArtifacts[criterion.Artifact] = struct{}{}
		default:
			return errors.New("expected delivery criterion fact is invalid")
		}
	}
	for artifact := range required {
		if artifact == ArtifactText && hasFinalMessage {
			continue
		}
		if artifact == ArtifactCanvasRevision && hasCanvasRevision {
			continue
		}
		if _, found := criteriaArtifacts[artifact]; !found {
			return errors.New("required artifact lacks a completion criterion")
		}
	}
	switch expected.Kind {
	case DeliveryAnswer:
		if !hasFinalMessage || hasCanvasRevision || len(criteriaArtifacts) > 0 || strings.TrimSpace(expected.TargetCanvasID) != "" {
			return errors.New("answer delivery facts are inconsistent")
		}
	case DeliveryCanvasChange:
		if !hasCanvasRevision || len(criteriaArtifacts) > 0 {
			return errors.New("canvas delivery facts are inconsistent")
		}
	case DeliveryGeneratedAsset:
		if len(required) == 0 || len(criteriaArtifacts) == 0 || hasCanvasRevision || strings.TrimSpace(expected.TargetCanvasID) != "" {
			return errors.New("generated asset delivery facts are inconsistent")
		}
	case DeliveryMixed:
		if !hasCanvasRevision || (!hasFinalMessage && len(criteriaArtifacts) == 0) {
			return errors.New("mixed delivery facts are incomplete")
		}
	}
	return nil
}

type DeliveryArtifact struct {
	Kind          ArtifactKind `json:"kind"`
	ArtifactID    string       `json:"artifactId,omitempty"`
	RevisionID    string       `json:"revisionId,omitempty"`
	ResourceID    string       `json:"resourceId,omitempty"`
	URL           string       `json:"url,omitempty"`
	ResourceReady bool         `json:"resourceReady,omitempty"`
	Approved      bool         `json:"approved,omitempty"`
	PublicationID string       `json:"publicationId,omitempty"`
}

type DeliveryEvidence struct {
	FinalMessage   string             `json:"finalMessage,omitempty"`
	CanvasID       string             `json:"canvasId,omitempty"`
	CanvasRevision int64              `json:"canvasRevision,omitempty"`
	Artifacts      []DeliveryArtifact `json:"artifacts,omitempty"`
}

type VerificationStatus string

const (
	VerificationSatisfied  VerificationStatus = "satisfied"
	VerificationRepairable VerificationStatus = "repairable"
	VerificationFailed     VerificationStatus = "failed"
)

type DeliveryVerification struct {
	Status          VerificationStatus  `json:"status"`
	Rationale       string              `json:"rationale"`
	MissingCriteria []DeliveryCriterion `json:"missingCriteria,omitempty"`
}

func VerifyDelivery(expected ExpectedDelivery, evidence DeliveryEvidence) DeliveryVerification {
	if err := expected.Validate(); err != nil {
		return DeliveryVerification{Status: VerificationFailed, Rationale: err.Error()}
	}
	artifacts := make(map[ArtifactKind][]DeliveryArtifact, len(evidence.Artifacts))
	for _, artifact := range evidence.Artifacts {
		if !validDeliveryArtifact(artifact) {
			return DeliveryVerification{Status: VerificationFailed, Rationale: "delivery evidence artifact is invalid"}
		}
		artifacts[artifact.Kind] = append(artifacts[artifact.Kind], artifact)
	}
	missing := make([]DeliveryCriterion, 0)
	for _, criterion := range expected.CompletionCriteria {
		satisfied := false
		switch criterion.Fact {
		case DeliveryFactFinalMessage:
			satisfied = strings.TrimSpace(evidence.FinalMessage) != ""
		case DeliveryFactCanvasRevision:
			satisfied = evidence.CanvasRevision > 0 && evidence.CanvasID == expected.TargetCanvasID
		case DeliveryFactArtifact:
			satisfied = deliveryArtifactMatches(artifacts[criterion.Artifact], func(artifact DeliveryArtifact) bool {
				return artifact.URL != ""
			})
		case DeliveryFactArtifactRevision:
			satisfied = deliveryArtifactMatches(artifacts[criterion.Artifact], func(artifact DeliveryArtifact) bool {
				return artifact.ArtifactID != "" && artifact.RevisionID != "" && artifact.Approved
			})
		case DeliveryFactResource:
			satisfied = deliveryArtifactMatches(artifacts[criterion.Artifact], func(artifact DeliveryArtifact) bool {
				return artifact.ArtifactID != "" && artifact.RevisionID != "" && artifact.Approved &&
					artifact.ResourceID != "" && artifact.ResourceReady && artifact.URL != ""
			})
		case DeliveryFactPublication:
			satisfied = deliveryArtifactMatches(artifacts[criterion.Artifact], func(artifact DeliveryArtifact) bool {
				return artifact.ArtifactID != "" && artifact.RevisionID != "" && artifact.Approved &&
					artifact.ResourceID != "" && artifact.ResourceReady && artifact.URL != "" && artifact.PublicationID != ""
			})
		}
		if !satisfied {
			missing = append(missing, criterion)
		}
	}
	if len(missing) > 0 {
		return DeliveryVerification{Status: VerificationRepairable, Rationale: "delivery evidence is incomplete", MissingCriteria: missing}
	}
	return DeliveryVerification{Status: VerificationSatisfied, Rationale: "delivery evidence satisfies every criterion"}
}

func validDeliveryArtifact(artifact DeliveryArtifact) bool {
	if !artifact.Kind.Valid() || !validOptionalDeliveryIdentity(artifact.ArtifactID) ||
		!validOptionalDeliveryIdentity(artifact.RevisionID) || !validOptionalDeliveryIdentity(artifact.ResourceID) ||
		!validOptionalDeliveryIdentity(artifact.PublicationID) || strings.TrimSpace(artifact.URL) != artifact.URL ||
		len(artifact.URL) > 4*1024 || (artifact.ArtifactID == "") != (artifact.RevisionID == "") {
		return false
	}
	hasExactRevision := artifact.ArtifactID != "" && artifact.RevisionID != ""
	if artifact.URL == "" && !hasExactRevision {
		return false
	}
	if artifact.Approved && !hasExactRevision {
		return false
	}
	if artifact.ResourceReady && (!hasExactRevision || artifact.ResourceID == "" || artifact.URL == "") {
		return false
	}
	return artifact.PublicationID == "" || (artifact.Approved && artifact.ResourceReady)
}

func validOptionalDeliveryIdentity(value string) bool {
	return strings.TrimSpace(value) == value && len(value) <= 120
}

func deliveryArtifactMatches(artifacts []DeliveryArtifact, predicate func(DeliveryArtifact) bool) bool {
	for _, artifact := range artifacts {
		if predicate(artifact) {
			return true
		}
	}
	return false
}
