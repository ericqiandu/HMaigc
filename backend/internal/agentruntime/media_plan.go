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
	maximumAssemblyPixels   = 8192 * 4320
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

type AssemblyTransitionKind string

const (
	AssemblyTransitionCut       AssemblyTransitionKind = "cut"
	AssemblyTransitionCrossfade AssemblyTransitionKind = "crossfade"
)

type AssemblyTransitionV2 struct {
	Kind       AssemblyTransitionKind `json:"kind"`
	DurationMS *int64                 `json:"durationMs"`
}

type AssemblyClipV2 struct {
	ClipKey                string               `json:"clipKey"`
	SourceRevision         ArtifactRevisionRef  `json:"sourceRevision"`
	TrimStartMS            *int64               `json:"trimStartMs"`
	TrimEndMS              *int64               `json:"trimEndMs"`
	NativeAudioGainMilliDB *int                 `json:"nativeAudioGainMilliDb"`
	TransitionToNext       AssemblyTransitionV2 `json:"transitionToNext"`
}

type AssemblyAudioTrackV2 struct {
	TrackKey       string              `json:"trackKey"`
	SourceRevision ArtifactRevisionRef `json:"sourceRevision"`
	StartMS        *int64              `json:"startMs"`
	TrimStartMS    *int64              `json:"trimStartMs"`
	TrimEndMS      *int64              `json:"trimEndMs"`
	GainMilliDB    *int                `json:"gainMilliDb"`
}

type AssemblyOutputV2 struct {
	ArtifactKey string `json:"artifactKey"`
	Container   string `json:"container"`
	VideoCodec  string `json:"videoCodec"`
	AudioCodec  string `json:"audioCodec"`
	Width       *int   `json:"width"`
	Height      *int   `json:"height"`
	FrameRate   *int   `json:"frameRate"`
}

type AssemblyPlanV2 struct {
	PlanKey     string                 `json:"planKey"`
	AudioMode   MediaAudioMode         `json:"audioMode"`
	Clips       []AssemblyClipV2       `json:"clips"`
	AudioTracks []AssemblyAudioTrackV2 `json:"audioTracks"`
	Output      AssemblyOutputV2       `json:"output"`
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

func DecodeAssemblyPlanV2(payload []byte) (AssemblyPlanV2, error) {
	var plan AssemblyPlanV2
	if err := validateAssemblyPlanV2RequiredFields(payload); err != nil ||
		decodeStrictMediaPlan(payload, &plan) != nil || ValidateAssemblyPlanV2(plan) != nil {
		return AssemblyPlanV2{}, ErrArtifactPayloadInvalid
	}
	return plan, nil
}

func ValidateAssemblyPlanV2(plan AssemblyPlanV2) error {
	if !validArtifactText(plan.PlanKey, 120) || !plan.AudioMode.Valid() || len(plan.Clips) == 0 ||
		len(plan.Clips) > maximumMediaPlanEntries || plan.AudioTracks == nil || len(plan.AudioTracks) > maximumMediaPlanEntries ||
		!validAssemblyOutputV2(plan.Output, plan.AudioMode) {
		return ErrArtifactPayloadInvalid
	}

	clipKeys := make(map[string]struct{}, len(plan.Clips))
	for index, clip := range plan.Clips {
		if !validArtifactText(clip.ClipKey, 120) || clip.SourceRevision.Validate() != nil ||
			!validAssemblyInterval(clip.TrimStartMS, clip.TrimEndMS) || !validAssemblyGain(clip.NativeAudioGainMilliDB) ||
			(plan.AudioMode == MediaAudioNative) != (clip.NativeAudioGainMilliDB != nil) {
			return ErrArtifactPayloadInvalid
		}
		if _, duplicate := clipKeys[clip.ClipKey]; duplicate {
			return ErrArtifactPayloadInvalid
		}
		clipKeys[clip.ClipKey] = struct{}{}

		if clip.TransitionToNext.DurationMS == nil {
			return ErrArtifactPayloadInvalid
		}
		transitionMS := *clip.TransitionToNext.DurationMS
		switch clip.TransitionToNext.Kind {
		case AssemblyTransitionCut:
			if transitionMS != 0 {
				return ErrArtifactPayloadInvalid
			}
		case AssemblyTransitionCrossfade:
			if index == len(plan.Clips)-1 || transitionMS <= 0 ||
				!validAssemblyInterval(plan.Clips[index+1].TrimStartMS, plan.Clips[index+1].TrimEndMS) ||
				transitionMS >= *clip.TrimEndMS-*clip.TrimStartMS ||
				transitionMS >= *plan.Clips[index+1].TrimEndMS-*plan.Clips[index+1].TrimStartMS {
				return ErrArtifactPayloadInvalid
			}
		default:
			return ErrArtifactPayloadInvalid
		}
	}
	totalVideoMS := int64(0)
	for index, clip := range plan.Clips {
		contributionMS := *clip.TrimEndMS - *clip.TrimStartMS
		if index > 0 {
			contributionMS -= *plan.Clips[index-1].TransitionToNext.DurationMS
		}
		if totalVideoMS > maximumAudioTimelineMS-contributionMS {
			return ErrArtifactPayloadInvalid
		}
		totalVideoMS += contributionMS
	}

	trackKeys := make(map[string]struct{}, len(plan.AudioTracks))
	for _, track := range plan.AudioTracks {
		if !validArtifactText(track.TrackKey, 120) || track.SourceRevision.Validate() != nil ||
			track.StartMS == nil || *track.StartMS < 0 || *track.StartMS > maximumAudioTimelineMS ||
			!validAssemblyInterval(track.TrimStartMS, track.TrimEndMS) || track.GainMilliDB == nil || !validAssemblyGain(track.GainMilliDB) {
			return ErrArtifactPayloadInvalid
		}
		durationMS := *track.TrimEndMS - *track.TrimStartMS
		if *track.StartMS > maximumAudioTimelineMS-durationMS {
			return ErrArtifactPayloadInvalid
		}
		if _, duplicate := trackKeys[track.TrackKey]; duplicate {
			return ErrArtifactPayloadInvalid
		}
		trackKeys[track.TrackKey] = struct{}{}
	}
	if (plan.AudioMode == MediaAudioIndependent) != (len(plan.AudioTracks) > 0) {
		return ErrArtifactPayloadInvalid
	}
	return validateArtifactRevisionRefs(assemblyPlanV2Inputs(plan))
}

func validateAssemblyPlanV2RequiredFields(payload []byte) error {
	var root map[string]json.RawMessage
	if json.Unmarshal(payload, &root) != nil || !hasJSONFields(root, "planKey", "audioMode", "clips", "audioTracks", "output") {
		return ErrArtifactPayloadInvalid
	}
	var clips []map[string]json.RawMessage
	if json.Unmarshal(root["clips"], &clips) != nil {
		return ErrArtifactPayloadInvalid
	}
	for _, clip := range clips {
		if !hasJSONFields(clip, "clipKey", "sourceRevision", "trimStartMs", "trimEndMs", "nativeAudioGainMilliDb", "transitionToNext") {
			return ErrArtifactPayloadInvalid
		}
		var transition map[string]json.RawMessage
		if json.Unmarshal(clip["transitionToNext"], &transition) != nil || !hasJSONFields(transition, "kind", "durationMs") {
			return ErrArtifactPayloadInvalid
		}
	}
	var tracks []map[string]json.RawMessage
	if json.Unmarshal(root["audioTracks"], &tracks) != nil {
		return ErrArtifactPayloadInvalid
	}
	for _, track := range tracks {
		if !hasJSONFields(track, "trackKey", "sourceRevision", "startMs", "trimStartMs", "trimEndMs", "gainMilliDb") {
			return ErrArtifactPayloadInvalid
		}
	}
	var output map[string]json.RawMessage
	if json.Unmarshal(root["output"], &output) != nil ||
		!hasJSONFields(output, "artifactKey", "container", "videoCodec", "audioCodec", "width", "height", "frameRate") {
		return ErrArtifactPayloadInvalid
	}
	return nil
}

func hasJSONFields(value map[string]json.RawMessage, fields ...string) bool {
	for _, field := range fields {
		if _, exists := value[field]; !exists {
			return false
		}
	}
	return true
}

func assemblyPlanV2Inputs(plan AssemblyPlanV2) []ArtifactRevisionRef {
	inputs := make([]ArtifactRevisionRef, 0, len(plan.Clips)+len(plan.AudioTracks))
	for _, clip := range plan.Clips {
		inputs = append(inputs, clip.SourceRevision)
	}
	for _, track := range plan.AudioTracks {
		inputs = append(inputs, track.SourceRevision)
	}
	return inputs
}

func validAssemblyInterval(start *int64, end *int64) bool {
	return start != nil && end != nil && *start >= 0 && *end > *start && *end <= maximumAudioTimelineMS
}

func validAssemblyGain(gain *int) bool {
	return gain == nil || (*gain >= -96000 && *gain <= 24000)
}

func validAssemblyOutputV2(output AssemblyOutputV2, audioMode MediaAudioMode) bool {
	if !validArtifactText(output.ArtifactKey, 120) || output.Container != "mp4" || output.VideoCodec != "h264" ||
		output.Width == nil || output.Height == nil || output.FrameRate == nil ||
		*output.Width <= 0 || *output.Height <= 0 || *output.Width > 8192 || *output.Height > 8192 ||
		int64(*output.Width)*int64(*output.Height) > maximumAssemblyPixels ||
		*output.Width%2 != 0 || *output.Height%2 != 0 ||
		*output.FrameRate <= 0 || *output.FrameRate > 240 {
		return false
	}
	if audioMode == MediaAudioNone {
		return output.AudioCodec == "none"
	}
	return output.AudioCodec == "aac"
}

func decodeStrictMediaPlan[T VideoPlan | AudioPlan | AssemblyPlan | AssemblyPlanV2](payload []byte, target *T) error {
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
