package agentruntime

import (
	"errors"
	"fmt"
	"strings"
)

const (
	maxProductionPlanShots      = 64
	maxProductionPlanReferences = 16
)

type ImageRenderConfig struct {
	Size                  string `json:"size"`
	Quality               string `json:"quality,omitempty"`
	Count                 int    `json:"count"`
	TransparentBackground bool   `json:"transparentBackground,omitempty"`
}

type VideoRenderConfig struct {
	DurationSeconds int    `json:"durationSeconds"`
	Quality         string `json:"quality"`
	GenerateAudio   bool   `json:"generateAudio"`
}

type FrozenRenderQuote struct {
	AmountMicrocredits        int64  `json:"amountMicrocredits"`
	PerTaskAmountMicrocredits int64  `json:"perTaskAmountMicrocredits"`
	PriceVersion              int64  `json:"priceVersion"`
	BillingMode               string `json:"billingMode"`
	PricingResolution         string `json:"pricingResolution,omitempty"`
	PricingInputVariant       string `json:"pricingInputVariant,omitempty"`
	Quantity                  int64  `json:"quantity"`
	QuoteFingerprint          string `json:"quoteFingerprint"`
}

// ProductionRenderArguments are trusted server-frozen approval facts. Model
// output is decoded through a separate request type and cannot supply Quote or
// Attempt.
type ProductionRenderArguments struct {
	PlanKey         string                   `json:"planKey"`
	PlanVersion     int                      `json:"planVersion"`
	ArtifactID      string                   `json:"artifactId"`
	Attempt         int                      `json:"attempt"`
	GenerationModel GenerationModelSelection `json:"generationModel"`
	ImageConfig     *ImageRenderConfig       `json:"imageConfig,omitempty"`
	VideoConfig     *VideoRenderConfig       `json:"videoConfig,omitempty"`
	FrozenRenderQuote
}

type ProductionPlanDraft struct {
	Title            string                `json:"title"`
	TargetDurationMS int                   `json:"targetDurationMs"`
	Script           string                `json:"script"`
	References       []ReferenceAssetDraft `json:"references,omitempty"`
	Shots            []ShotPlanDraft       `json:"shots"`
}

// ReferenceAssetDraft describes a non-timeline visual anchor. It is rendered
// before dependent storyboard images and never contributes to film duration.
type ReferenceAssetDraft struct {
	ReferenceKey string `json:"referenceKey"`
	Role         string `json:"role"`
	Title        string `json:"title"`
	ImagePrompt  string `json:"imagePrompt"`
}

type ShotPlanDraft struct {
	ShotKey       string   `json:"shotKey"`
	Order         int      `json:"order"`
	DurationMS    int      `json:"durationMs"`
	ScriptText    string   `json:"scriptText"`
	ImagePrompt   string   `json:"imagePrompt"`
	VideoPrompt   string   `json:"videoPrompt"`
	ReferenceKeys []string `json:"referenceKeys,omitempty"`
	Dependencies  []string `json:"dependencies"`
}

func (draft ProductionPlanDraft) Validate() error {
	if strings.TrimSpace(draft.Title) == "" || len(draft.Title) > 240 {
		return errors.New("production plan title is invalid")
	}
	if draft.TargetDurationMS <= 0 {
		return errors.New("production plan target duration is invalid")
	}
	if strings.TrimSpace(draft.Script) == "" {
		return errors.New("production plan script is required")
	}
	if len(draft.Shots) == 0 || len(draft.Shots) > maxProductionPlanShots {
		return errors.New("production plan shot count is invalid")
	}
	if len(draft.References) > maxProductionPlanReferences {
		return errors.New("production plan reference count is invalid")
	}
	referenceKeys := make(map[string]struct{}, len(draft.References))
	for index, reference := range draft.References {
		referenceKey := strings.TrimSpace(reference.ReferenceKey)
		if referenceKey == "" || len(referenceKey) > 120 {
			return fmt.Errorf("production plan reference %d key is invalid", index+1)
		}
		if _, exists := referenceKeys[referenceKey]; exists {
			return fmt.Errorf("production plan reference key %s is duplicated", referenceKey)
		}
		if strings.TrimSpace(reference.Role) == "" || len(strings.TrimSpace(reference.Role)) > 80 ||
			strings.TrimSpace(reference.Title) == "" || len(strings.TrimSpace(reference.Title)) > 240 ||
			strings.TrimSpace(reference.ImagePrompt) == "" {
			return fmt.Errorf("production plan reference %s content is incomplete", referenceKey)
		}
		referenceKeys[referenceKey] = struct{}{}
	}

	shotKeys := make(map[string]struct{}, len(draft.Shots))
	shotOrders := make(map[string]int, len(draft.Shots))
	totalDurationMS := 0
	for index, shot := range draft.Shots {
		shotKey := strings.TrimSpace(shot.ShotKey)
		if shotKey == "" || len(shotKey) > 120 {
			return fmt.Errorf("production plan shot %d key is invalid", index+1)
		}
		if _, exists := shotKeys[shotKey]; exists {
			return fmt.Errorf("production plan shot key %s is duplicated", shotKey)
		}
		shotKeys[shotKey] = struct{}{}
		shotOrders[shotKey] = shot.Order
		if shot.Order != index+1 {
			return fmt.Errorf("production plan shot %s order is not contiguous", shotKey)
		}
		if shot.DurationMS <= 0 {
			return fmt.Errorf("production plan shot %s duration is invalid", shotKey)
		}
		if strings.TrimSpace(shot.ScriptText) == "" || strings.TrimSpace(shot.ImagePrompt) == "" || strings.TrimSpace(shot.VideoPrompt) == "" {
			return fmt.Errorf("production plan shot %s content is incomplete", shotKey)
		}
		seenReferences := make(map[string]struct{}, len(shot.ReferenceKeys))
		for _, referenceKey := range shot.ReferenceKeys {
			referenceKey = strings.TrimSpace(referenceKey)
			if _, exists := referenceKeys[referenceKey]; !exists {
				return fmt.Errorf("production plan shot %s reference %s is missing", shotKey, referenceKey)
			}
			if _, exists := seenReferences[referenceKey]; exists {
				return fmt.Errorf("production plan shot %s reference %s is duplicated", shotKey, referenceKey)
			}
			seenReferences[referenceKey] = struct{}{}
		}
		totalDurationMS += shot.DurationMS
	}
	for _, shot := range draft.Shots {
		seenDependencies := make(map[string]struct{}, len(shot.Dependencies))
		for _, dependency := range shot.Dependencies {
			dependency = strings.TrimSpace(dependency)
			if dependency == "" || dependency == shot.ShotKey {
				return fmt.Errorf("production plan shot %s dependency is invalid", shot.ShotKey)
			}
			if _, exists := shotKeys[dependency]; !exists {
				return fmt.Errorf("production plan shot %s dependency %s is missing", shot.ShotKey, dependency)
			}
			if shotOrders[dependency] >= shot.Order {
				return fmt.Errorf("production plan shot %s dependency %s is not earlier", shot.ShotKey, dependency)
			}
			if _, exists := seenDependencies[dependency]; exists {
				return fmt.Errorf("production plan shot %s dependency %s is duplicated", shot.ShotKey, dependency)
			}
			seenDependencies[dependency] = struct{}{}
		}
	}
	if totalDurationMS != draft.TargetDurationMS {
		return errors.New("production plan shot durations do not match target duration")
	}
	return nil
}
