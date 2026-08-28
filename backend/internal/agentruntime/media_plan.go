package agentruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

const (
	maximumMediaPlanEntries = 256
	maximumAudioTimelineMS  = 12 * 60 * 60 * 1000
)

type MediaAudioMode string

const (
	MediaAudioNone        MediaAudioMode = "none"
	MediaAudioNative      MediaAudioMode = "native"
	MediaAudioIndependent MediaAudioMode = "independent"
)

func (mode MediaAudioMode) Valid() bool {
	return mode == MediaAudioNone || mode == MediaAudioNative || mode == MediaAudioIndependent
}

type VideoPlanSegment struct {
	SegmentKey        string                `json:"segmentKey"`
	InputRevisions    []ArtifactRevisionRef `json:"inputRevisions"`
	OutputArtifactKey string                `json:"outputArtifactKey"`
	GenerateAudio     bool                  `json:"generateAudio"`
}

type VideoPlan struct {
	PlanKey        string                `json:"planKey"`
	InputRevisions []ArtifactRevisionRef `json:"inputRevisions"`
	AudioMode      MediaAudioMode        `json:"audioMode"`
	Segments       []VideoPlanSegment    `json:"segments"`
}

type AudioPlanClip struct {
	ClipKey           string `json:"clipKey"`
	VoiceBindingKey   string `json:"voiceBindingKey"`
	LineKey           string `json:"lineKey"`
	Dialogue          string `json:"dialogue"`
	StartMS           int    `json:"startMs"`
	DurationMS        int    `json:"durationMs"`
	OutputArtifactKey string `json:"outputArtifactKey"`
}

type AudioPlan struct {
	PlanKey        string                `json:"planKey"`
	InputRevisions []ArtifactRevisionRef `json:"inputRevisions"`
	Clips          []AudioPlanClip       `json:"clips"`
}

type AssemblyPlan struct {
	PlanKey           string                `json:"planKey"`
	AudioMode         MediaAudioMode        `json:"audioMode"`
	VideoRevisions    []ArtifactRevisionRef `json:"videoRevisions"`
	AudioRevisions    []ArtifactRevisionRef `json:"audioRevisions"`
	OutputArtifactKey string                `json:"outputArtifactKey"`
}

func DecodeVideoPlan(payload []byte) (VideoPlan, error) {
	var plan VideoPlan
	if err := decodeStrictMediaPlan(payload, &plan); err != nil || ValidateVideoPlan(plan) != nil {
		return VideoPlan{}, ErrArtifactPayloadInvalid
	}
	return plan, nil
}

func ValidateVideoPlan(plan VideoPlan) error {
	if !validArtifactText(plan.PlanKey, 120) || !plan.AudioMode.Valid() || len(plan.InputRevisions) == 0 ||
		len(plan.Segments) == 0 || len(plan.Segments) > maximumMediaPlanEntries || validateArtifactRevisionRefs(plan.InputRevisions) != nil {
		return ErrArtifactPayloadInvalid
	}
	allowed := artifactRevisionRefSet(plan.InputRevisions)
	used := make(map[ArtifactRevisionRef]struct{}, len(plan.InputRevisions))
	segmentKeys := make(map[string]struct{}, len(plan.Segments))
	outputKeys := make(map[string]struct{}, len(plan.Segments))
	for _, segment := range plan.Segments {
		if !validArtifactText(segment.SegmentKey, 120) || !validArtifactText(segment.OutputArtifactKey, 120) ||
			len(segment.InputRevisions) == 0 || !artifactRevisionRefsBelongTo(segment.InputRevisions, allowed) ||
			(plan.AudioMode == MediaAudioNative) != segment.GenerateAudio {
			return ErrArtifactPayloadInvalid
		}
		if _, duplicate := segmentKeys[segment.SegmentKey]; duplicate {
			return ErrArtifactPayloadInvalid
		}
		if _, duplicate := outputKeys[segment.OutputArtifactKey]; duplicate {
			return ErrArtifactPayloadInvalid
		}
		segmentKeys[segment.SegmentKey] = struct{}{}
		outputKeys[segment.OutputArtifactKey] = struct{}{}
		for _, reference := range segment.InputRevisions {
			used[reference] = struct{}{}
		}
	}
	if len(used) != len(allowed) {
		return ErrArtifactPayloadInvalid
	}
	return nil
}

func DecodeAudioPlan(payload []byte) (AudioPlan, error) {
	var plan AudioPlan
	if err := decodeStrictMediaPlan(payload, &plan); err != nil || ValidateAudioPlan(plan) != nil {
		return AudioPlan{}, ErrArtifactPayloadInvalid
	}
	return plan, nil
}

func ValidateAudioPlan(plan AudioPlan) error {
	if !validArtifactText(plan.PlanKey, 120) || len(plan.InputRevisions) == 0 || validateArtifactRevisionRefs(plan.InputRevisions) != nil ||
		len(plan.Clips) == 0 || len(plan.Clips) > maximumMediaPlanEntries {
		return ErrArtifactPayloadInvalid
	}
	clipKeys := make(map[string]struct{}, len(plan.Clips))
	lineKeys := make(map[string]struct{}, len(plan.Clips))
	outputKeys := make(map[string]struct{}, len(plan.Clips))
	previousEndMS := 0
	for index, clip := range plan.Clips {
		if !validArtifactText(clip.ClipKey, 120) || !validArtifactText(clip.VoiceBindingKey, 120) ||
			!validArtifactText(clip.LineKey, 120) || !validArtifactText(clip.Dialogue, 8*1024) ||
			!validArtifactText(clip.OutputArtifactKey, 120) || clip.StartMS < 0 || clip.DurationMS <= 0 ||
			clip.StartMS > maximumAudioTimelineMS-clip.DurationMS || (index > 0 && clip.StartMS < previousEndMS) {
			return ErrArtifactPayloadInvalid
		}
		if _, duplicate := clipKeys[clip.ClipKey]; duplicate {
			return ErrArtifactPayloadInvalid
		}
		if _, duplicate := lineKeys[clip.LineKey]; duplicate {
			return ErrArtifactPayloadInvalid
		}
		if _, duplicate := outputKeys[clip.OutputArtifactKey]; duplicate {
			return ErrArtifactPayloadInvalid
		}
		clipKeys[clip.ClipKey] = struct{}{}
		lineKeys[clip.LineKey] = struct{}{}
		outputKeys[clip.OutputArtifactKey] = struct{}{}
		previousEndMS = clip.StartMS + clip.DurationMS
	}
	return nil
}

func DecodeAssemblyPlan(payload []byte) (AssemblyPlan, error) {
	var plan AssemblyPlan
	if err := decodeStrictMediaPlan(payload, &plan); err != nil || ValidateAssemblyPlan(plan) != nil {
		return AssemblyPlan{}, ErrArtifactPayloadInvalid
	}
	return plan, nil
}

func ValidateAssemblyPlan(plan AssemblyPlan) error {
	if !validArtifactText(plan.PlanKey, 120) || !plan.AudioMode.Valid() || !validArtifactText(plan.OutputArtifactKey, 120) ||
		len(plan.VideoRevisions) == 0 || len(plan.VideoRevisions) > maximumMediaPlanEntries ||
		validateArtifactRevisionRefs(plan.VideoRevisions) != nil || validateArtifactRevisionRefs(plan.AudioRevisions) != nil {
		return ErrArtifactPayloadInvalid
	}
	if (plan.AudioMode == MediaAudioIndependent) != (len(plan.AudioRevisions) > 0) {
		return ErrArtifactPayloadInvalid
	}
	return validateArtifactRevisionRefs(assemblyPlanInputs(plan))
}

func assemblyPlanInputs(plan AssemblyPlan) []ArtifactRevisionRef {
	inputs := make([]ArtifactRevisionRef, 0, len(plan.VideoRevisions)+len(plan.AudioRevisions))
	inputs = append(inputs, plan.VideoRevisions...)
	return append(inputs, plan.AudioRevisions...)
}

func decodeStrictMediaPlan[T VideoPlan | AudioPlan | AssemblyPlan](payload []byte, target *T) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ErrArtifactPayloadInvalid
	}
	return nil
}
