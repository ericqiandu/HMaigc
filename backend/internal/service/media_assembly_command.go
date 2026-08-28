package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

var (
	ErrMediaAssemblyCommandInvalid         = errors.New("media assembly command is invalid")
	ErrMediaAssemblyPlanVersionUnsupported = errors.New("media assembly plan version is not executable")
)

type MediaAssemblyInput struct {
	Evidence  agentruntime.DeliveryArtifact
	Resource  model.Resource
	InputPath string
}

type MediaAssemblyCommand struct {
	Executable        string
	Arguments         []string
	PlanDigest        string
	OutputArtifactKey string
}

// BuildMediaAssemblyCommand translates one validated assembly_plan.v2 into an
// argv-only FFmpeg invocation. Callers must execute Executable with Arguments
// directly; this contract deliberately never creates a shell command string.
func BuildMediaAssemblyCommand(
	draft agentruntime.ArtifactDraft,
	inputs []MediaAssemblyInput,
	outputPath string,
) (MediaAssemblyCommand, error) {
	if draft.SchemaID() != agentruntime.ArtifactSchemaAssemblyPlanV2 {
		return MediaAssemblyCommand{}, ErrMediaAssemblyPlanVersionUnsupported
	}
	if err := agentruntime.ValidateArtifactDraft(draft); err != nil {
		return MediaAssemblyCommand{}, fmt.Errorf("%w: artifact: %v", ErrMediaAssemblyCommandInvalid, err)
	}
	plan, err := agentruntime.DecodeAssemblyPlanV2(draft.Payload)
	if err != nil {
		return MediaAssemblyCommand{}, fmt.Errorf("%w: plan: %v", ErrMediaAssemblyCommandInvalid, err)
	}
	if err := validateMediaAssemblyInputs(plan, inputs, outputPath); err != nil {
		return MediaAssemblyCommand{}, err
	}
	digest, err := digestAssemblyPlanV2(plan)
	if err != nil {
		return MediaAssemblyCommand{}, fmt.Errorf("%w: canonical plan: %v", ErrMediaAssemblyCommandInvalid, err)
	}

	arguments := []string{"-hide_banner", "-nostdin", "-n"}
	for _, input := range inputs {
		arguments = append(arguments, "-i", input.InputPath)
	}
	filter := buildAssemblyFilterGraph(plan)
	arguments = append(arguments, "-filter_complex", filter, "-map", "[vout]")
	if plan.AudioMode == agentruntime.MediaAudioNone {
		arguments = append(arguments, "-an")
	} else {
		arguments = append(arguments, "-map", "[aout]", "-c:a", "aac", "-shortest")
	}
	arguments = append(arguments,
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-r", strconv.Itoa(*plan.Output.FrameRate),
		"-metadata", "assembly_plan_digest="+digest,
		"-f", "mp4",
		"-movflags", "+faststart",
		outputPath,
	)
	return MediaAssemblyCommand{
		Executable:        "ffmpeg",
		Arguments:         arguments,
		PlanDigest:        digest,
		OutputArtifactKey: plan.Output.ArtifactKey,
	}, nil
}

func validateMediaAssemblyInputs(plan agentruntime.AssemblyPlanV2, inputs []MediaAssemblyInput, outputPath string) error {
	if strings.TrimSpace(outputPath) == "" || strings.ContainsRune(outputPath, '\x00') || !strings.EqualFold(filepath.Ext(outputPath), ".mp4") {
		return fmt.Errorf("%w: output path must name an mp4 file", ErrMediaAssemblyCommandInvalid)
	}
	expectedCount := len(plan.Clips) + len(plan.AudioTracks)
	if len(inputs) != expectedCount {
		return fmt.Errorf("%w: got %d inputs, want %d", ErrMediaAssemblyCommandInvalid, len(inputs), expectedCount)
	}
	paths := make(map[string]struct{}, len(inputs))
	for index, input := range inputs {
		var expectedRevision agentruntime.ArtifactRevisionRef
		expectedArtifactKind := agentruntime.ArtifactVideo
		expectedResourceKind := "video"
		expectedMIMEPrefix := "video/"
		var requiredDurationMS int64
		if index < len(plan.Clips) {
			clip := plan.Clips[index]
			expectedRevision = clip.SourceRevision
			requiredDurationMS = *clip.TrimEndMS
		} else {
			track := plan.AudioTracks[index-len(plan.Clips)]
			expectedRevision = track.SourceRevision
			expectedArtifactKind = agentruntime.ArtifactAudio
			expectedResourceKind = "audio"
			expectedMIMEPrefix = "audio/"
			requiredDurationMS = *track.TrimEndMS
		}
		evidence := input.Evidence
		if evidence.ArtifactID != expectedRevision.ArtifactID || evidence.RevisionID != expectedRevision.RevisionID {
			return fmt.Errorf("%w: input %d revision does not match the ordered plan", ErrMediaAssemblyCommandInvalid, index)
		}
		if evidence.Kind != expectedArtifactKind {
			return fmt.Errorf("%w: input %d approved artifact kind does not match the plan", ErrMediaAssemblyCommandInvalid, index)
		}
		if !evidence.Approved {
			return fmt.Errorf("%w: input %d revision is not approved", ErrMediaAssemblyCommandInvalid, index)
		}
		if !evidence.ResourceReady {
			return fmt.Errorf("%w: input %d approval does not reference a ready resource", ErrMediaAssemblyCommandInvalid, index)
		}
		if strings.TrimSpace(input.InputPath) == "" || strings.ContainsRune(input.InputPath, '\x00') || input.InputPath == outputPath {
			return fmt.Errorf("%w: input %d path is invalid", ErrMediaAssemblyCommandInvalid, index)
		}
		if _, duplicate := paths[input.InputPath]; duplicate {
			return fmt.Errorf("%w: input path %q is duplicated", ErrMediaAssemblyCommandInvalid, input.InputPath)
		}
		paths[input.InputPath] = struct{}{}
		resource := input.Resource
		if !validExactAssemblyMetadata(evidence.ResourceID) || evidence.ResourceID != resource.ID {
			return fmt.Errorf("%w: input %d approved revision resource does not match the resolved resource", ErrMediaAssemblyCommandInvalid, index)
		}
		if err := validateMediaAssemblyResource(resource, expectedResourceKind, expectedMIMEPrefix, requiredDurationMS); err != nil {
			return fmt.Errorf("%w: input %d resource: %v", ErrMediaAssemblyCommandInvalid, index, err)
		}
	}
	return nil
}

func validateMediaAssemblyResource(resource model.Resource, expectedKind string, expectedMIMEPrefix string, requiredDurationMS int64) error {
	if !validExactAssemblyMetadata(resource.ID) {
		return errors.New("id is missing or not exact")
	}
	if resource.Status != model.ResourceStatusReady {
		return errors.New("status is not ready")
	}
	if resource.Kind != expectedKind {
		return fmt.Errorf("kind %q does not match %q", resource.Kind, expectedKind)
	}
	if !validExactAssemblyMetadata(resource.MimeType) || !strings.HasPrefix(strings.ToLower(resource.MimeType), expectedMIMEPrefix) {
		return fmt.Errorf("mime type %q does not match %q", resource.MimeType, expectedMIMEPrefix)
	}
	if !validExactAssemblyMetadata(resource.Provider) {
		return errors.New("provider is missing or not exact")
	}
	if !validExactAssemblyMetadata(resource.ObjectKey) {
		return errors.New("object key is missing or not exact")
	}
	if resource.Size <= 0 {
		return errors.New("size must be positive")
	}
	if resource.DurationMs < requiredDurationMS {
		return fmt.Errorf("duration %dms does not cover required trim end %dms", resource.DurationMs, requiredDurationMS)
	}
	if !validExactAssemblyMetadata(resource.ETag) {
		return errors.New("etag is missing or not exact")
	}
	return nil
}

func validExactAssemblyMetadata(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && !strings.ContainsRune(value, '\x00')
}

func digestAssemblyPlanV2(plan agentruntime.AssemblyPlanV2) (string, error) {
	canonical, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

func buildAssemblyFilterGraph(plan agentruntime.AssemblyPlanV2) string {
	filters := make([]string, 0, len(plan.Clips)*3+len(plan.AudioTracks)+2)
	videoDurations := make([]int64, len(plan.Clips))
	for index, clip := range plan.Clips {
		videoDurations[index] = *clip.TrimEndMS - *clip.TrimStartMS
		filters = append(filters, fmt.Sprintf(
			"[%d:v]trim=start=%s:end=%s,settb=AVTB,setpts=PTS-STARTPTS,scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1,fps=%d,format=yuv420p[v%d]",
			index, assemblySeconds(*clip.TrimStartMS), assemblySeconds(*clip.TrimEndMS),
			*plan.Output.Width, *plan.Output.Height, *plan.Output.Width, *plan.Output.Height, *plan.Output.FrameRate, index,
		))
	}
	currentVideo := "[v0]"
	currentDurationMS := videoDurations[0]
	for index := 1; index < len(plan.Clips); index++ {
		transition := plan.Clips[index-1].TransitionToNext
		nextLabel := fmt.Sprintf("[vjoin%d]", index)
		if transition.Kind == agentruntime.AssemblyTransitionCut {
			filters = append(filters, fmt.Sprintf("%s[v%d]concat=n=2:v=1:a=0%s", currentVideo, index, nextLabel))
			currentDurationMS += videoDurations[index]
		} else {
			offsetMS := currentDurationMS - *transition.DurationMS
			filters = append(filters, fmt.Sprintf(
				"%s[v%d]xfade=transition=fade:duration=%s:offset=%s%s",
				currentVideo, index, assemblySeconds(*transition.DurationMS), assemblySeconds(offsetMS), nextLabel,
			))
			currentDurationMS += videoDurations[index] - *transition.DurationMS
		}
		currentVideo = nextLabel
	}
	filters = append(filters, currentVideo+"null[vout]")

	switch plan.AudioMode {
	case agentruntime.MediaAudioNative:
		filters = append(filters, buildNativeAssemblyAudio(plan)...)
	case agentruntime.MediaAudioIndependent:
		filters = append(filters, buildIndependentAssemblyAudio(plan)...)
	}
	return strings.Join(filters, ";")
}

func buildNativeAssemblyAudio(plan agentruntime.AssemblyPlanV2) []string {
	filters := make([]string, 0, len(plan.Clips)*2+1)
	for index, clip := range plan.Clips {
		filters = append(filters, fmt.Sprintf(
			"[%d:a]atrim=start=%s:end=%s,asetpts=PTS-STARTPTS,aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo,volume=%sdB[a%d]",
			index, assemblySeconds(*clip.TrimStartMS), assemblySeconds(*clip.TrimEndMS), assemblyGain(*clip.NativeAudioGainMilliDB), index,
		))
	}
	currentAudio := "[a0]"
	for index := 1; index < len(plan.Clips); index++ {
		transition := plan.Clips[index-1].TransitionToNext
		nextLabel := fmt.Sprintf("[ajoin%d]", index)
		if transition.Kind == agentruntime.AssemblyTransitionCut {
			filters = append(filters, fmt.Sprintf("%s[a%d]concat=n=2:v=0:a=1%s", currentAudio, index, nextLabel))
		} else {
			filters = append(filters, fmt.Sprintf(
				"%s[a%d]acrossfade=d=%s:c1=tri:c2=tri%s",
				currentAudio, index, assemblySeconds(*transition.DurationMS), nextLabel,
			))
		}
		currentAudio = nextLabel
	}
	return append(filters, currentAudio+"anull[aout]")
}

func buildIndependentAssemblyAudio(plan agentruntime.AssemblyPlanV2) []string {
	filters := make([]string, 0, len(plan.AudioTracks)+1)
	labels := make([]string, 0, len(plan.AudioTracks))
	for index, track := range plan.AudioTracks {
		inputIndex := len(plan.Clips) + index
		label := fmt.Sprintf("[track%d]", index)
		filters = append(filters, fmt.Sprintf(
			"[%d:a]atrim=start=%s:end=%s,asetpts=PTS-STARTPTS,aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo,volume=%sdB,adelay=%d:all=1%s",
			inputIndex, assemblySeconds(*track.TrimStartMS), assemblySeconds(*track.TrimEndMS),
			assemblyGain(*track.GainMilliDB), *track.StartMS, label,
		))
		labels = append(labels, label)
	}
	filters = append(filters, strings.Join(labels, "")+fmt.Sprintf("amix=inputs=%d:normalize=0:dropout_transition=0[aout]", len(labels)))
	return filters
}

func assemblySeconds(milliseconds int64) string {
	return fmt.Sprintf("%.3f", float64(milliseconds)/1000)
}

func assemblyGain(milliDB int) string {
	return fmt.Sprintf("%.3f", float64(milliDB)/1000)
}
