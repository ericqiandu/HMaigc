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
	DeliveryFactFinalMessage   DeliveryFact = "final_message"
	DeliveryFactCanvasRevision DeliveryFact = "canvas_revision"
	DeliveryFactArtifact       DeliveryFact = "artifact"
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
		case DeliveryFactArtifact:
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
	Kind ArtifactKind `json:"kind"`
	URL  string       `json:"url"`
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
	artifacts := make(map[ArtifactKind]bool, len(evidence.Artifacts))
	for _, artifact := range evidence.Artifacts {
		if !artifact.Kind.Valid() || strings.TrimSpace(artifact.URL) == "" {
			return DeliveryVerification{Status: VerificationFailed, Rationale: "delivery evidence artifact is invalid"}
		}
		artifacts[artifact.Kind] = true
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
			satisfied = artifacts[criterion.Artifact]
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
