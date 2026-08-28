package service

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

func TestAssemblyCommandBuildsDeterministicShellSafeArguments(t *testing.T) {
	draft, inputs := assemblyCommandFixture(t)
	inputs[0].InputPath = `C:\media\clip 1;$(touch hacked).mp4`
	inputs[2].InputPath = `/tmp/dialogue'quote.wav`
	outputPath := `/tmp/final output.mp4`

	command, err := BuildMediaAssemblyCommand(draft, inputs, outputPath)
	if err != nil {
		t.Fatalf("BuildMediaAssemblyCommand() error = %v", err)
	}
	if command.Executable != "ffmpeg" {
		t.Fatalf("executable = %q, want ffmpeg", command.Executable)
	}
	if len(command.PlanDigest) != 64 {
		t.Fatalf("plan digest = %q, want sha256 hex", command.PlanDigest)
	}
	if command.OutputArtifactKey != "final-film" {
		t.Fatalf("output artifact key = %q, want final-film", command.OutputArtifactKey)
	}
	if !containsArgument(command.Arguments, "-n") || containsArgument(command.Arguments, "-y") {
		t.Fatalf("command must refuse to overwrite an existing output: %#v", command.Arguments)
	}
	for _, input := range inputs {
		if !argumentFollows(command.Arguments, "-i", input.InputPath) {
			t.Fatalf("input path %q was not preserved as one argv entry: %#v", input.InputPath, command.Arguments)
		}
	}
	if command.Arguments[len(command.Arguments)-1] != outputPath {
		t.Fatalf("output argv = %q, want %q", command.Arguments[len(command.Arguments)-1], outputPath)
	}
	joined := strings.Join(command.Arguments, "\n")
	for _, expected := range []string{
		"trim=start=0.000:end=5.000",
		"settb=AVTB",
		"setsar=1",
		"xfade=transition=fade:duration=0.500:offset=4.500",
		"atrim=start=0.100:end=8.100",
		"aresample=48000",
		"aformat=sample_fmts=fltp:channel_layouts=stereo",
		"adelay=0:all=1",
		"volume=-1.500dB",
		"amix=inputs=1:normalize=0:dropout_transition=0",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("command args do not contain %q: %#v", expected, command.Arguments)
		}
	}

	compactPayload, err := json.Marshal(mustDecodeAssemblyPlanV2(t, draft.Payload))
	if err != nil {
		t.Fatal(err)
	}
	compactDraft := draft
	compactDraft.Payload = compactPayload
	second, err := BuildMediaAssemblyCommand(compactDraft, inputs, outputPath)
	if err != nil {
		t.Fatalf("second BuildMediaAssemblyCommand() error = %v", err)
	}
	if command.PlanDigest != second.PlanDigest || !reflect.DeepEqual(command, second) {
		t.Fatalf("canonical command changed with JSON whitespace:\nfirst=%#v\nsecond=%#v", command, second)
	}
}

func TestAssemblyCommandRejectsV1AndNonExactInputs(t *testing.T) {
	draft, inputs := assemblyCommandFixture(t)
	v1 := agentruntime.ArtifactDraft{
		ArtifactKey: "legacy", Kind: "assembly_plan", SchemaVersion: 1,
		Payload:           json.RawMessage(`{"planKey":"legacy","audioMode":"none","videoRevisions":[{"artifactId":"video-1","revisionId":"video-1-r2"}],"audioRevisions":[],"outputArtifactKey":"final"}`),
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{{ArtifactID: "video-1", RevisionID: "video-1-r2"}},
	}
	if _, err := BuildMediaAssemblyCommand(v1, inputs[:1], "/tmp/out.mp4"); !errors.Is(err, ErrMediaAssemblyPlanVersionUnsupported) {
		t.Fatalf("v1 execution error = %v, want %v", err, ErrMediaAssemblyPlanVersionUnsupported)
	}

	tests := []struct {
		name       string
		mutate     func([]MediaAssemblyInput) []MediaAssemblyInput
		outputPath string
	}{
		{
			name: "stale source revision",
			mutate: func(values []MediaAssemblyInput) []MediaAssemblyInput {
				values[0].Evidence.RevisionID = "video-1-r1"
				return values
			},
			outputPath: "/tmp/out.mp4",
		},
		{
			name: "wrong ordered source",
			mutate: func(values []MediaAssemblyInput) []MediaAssemblyInput {
				values[0], values[1] = values[1], values[0]
				return values
			},
			outputPath: "/tmp/out.mp4",
		},
		{
			name: "source revision is not approved",
			mutate: func(values []MediaAssemblyInput) []MediaAssemblyInput {
				values[0].Evidence.Approved = false
				return values
			},
			outputPath: "/tmp/out.mp4",
		},
		{
			name: "approval resource is not ready",
			mutate: func(values []MediaAssemblyInput) []MediaAssemblyInput {
				values[0].Evidence.ResourceReady = false
				return values
			},
			outputPath: "/tmp/out.mp4",
		},
		{
			name: "resource is not ready",
			mutate: func(values []MediaAssemblyInput) []MediaAssemblyInput {
				values[0].Resource.Status = model.ResourceStatusPending
				return values
			},
			outputPath: "/tmp/out.mp4",
		},
		{
			name: "approved revision resource is substituted",
			mutate: func(values []MediaAssemblyInput) []MediaAssemblyInput {
				values[0].Resource.ID = "resource-video-substitute"
				return values
			},
			outputPath: "/tmp/out.mp4",
		},
		{
			name: "resource media kind does not match plan",
			mutate: func(values []MediaAssemblyInput) []MediaAssemblyInput {
				values[2].Resource.Kind = "video"
				values[2].Resource.MimeType = "video/mp4"
				return values
			},
			outputPath: "/tmp/out.mp4",
		},
		{
			name: "resource duration does not cover trim",
			mutate: func(values []MediaAssemblyInput) []MediaAssemblyInput {
				values[1].Resource.DurationMs = 4000
				return values
			},
			outputPath: "/tmp/out.mp4",
		},
		{
			name: "resource content identity is missing",
			mutate: func(values []MediaAssemblyInput) []MediaAssemblyInput {
				values[0].Resource.ETag = ""
				return values
			},
			outputPath: "/tmp/out.mp4",
		},
		{
			name: "resource metadata is not exact",
			mutate: func(values []MediaAssemblyInput) []MediaAssemblyInput {
				values[0].Resource.ETag = " etag-resource-video-1"
				return values
			},
			outputPath: "/tmp/out.mp4",
		},
		{
			name: "resolved input path is missing",
			mutate: func(values []MediaAssemblyInput) []MediaAssemblyInput {
				values[0].InputPath = ""
				return values
			},
			outputPath: "/tmp/out.mp4",
		},
		{
			name:       "output path is missing",
			mutate:     func(values []MediaAssemblyInput) []MediaAssemblyInput { return values },
			outputPath: "",
		},
		{
			name: "input is missing",
			mutate: func(values []MediaAssemblyInput) []MediaAssemblyInput {
				return values[:len(values)-1]
			},
			outputPath: "/tmp/out.mp4",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutated := append([]MediaAssemblyInput(nil), inputs...)
			_, err := BuildMediaAssemblyCommand(draft, test.mutate(mutated), test.outputPath)
			if !errors.Is(err, ErrMediaAssemblyCommandInvalid) {
				t.Fatalf("BuildMediaAssemblyCommand() error = %v, want %v", err, ErrMediaAssemblyCommandInvalid)
			}
		})
	}
}

func TestAssemblyCommandBuildsExplicitNoneAndNativeAudioModes(t *testing.T) {
	video := agentruntime.ArtifactRevisionRef{ArtifactID: "video-1", RevisionID: "video-1-r1"}
	input := MediaAssemblyInput{
		Evidence:  approvedAssemblyEvidence(video, agentruntime.ArtifactVideo, "resource-video-1"),
		Resource:  assemblyResource("resource-video-1", "video", "video/mp4", 5000),
		InputPath: "/tmp/video.mp4",
	}
	tests := []struct {
		name        string
		artifactKey string
		payload     string
		want        []string
		unwanted    []string
	}{
		{
			name:        "no audio",
			artifactKey: "none",
			payload:     `{"planKey":"none","audioMode":"none","clips":[{"clipKey":"clip-1","sourceRevision":{"artifactId":"video-1","revisionId":"video-1-r1"},"trimStartMs":0,"trimEndMs":5000,"nativeAudioGainMilliDb":null,"transitionToNext":{"kind":"cut","durationMs":0}}],"audioTracks":[],"output":{"artifactKey":"final-none","container":"mp4","videoCodec":"h264","audioCodec":"none","width":1280,"height":720,"frameRate":24}}`,
			want:        []string{"-an"},
			unwanted:    []string{"[aout]", "-c:a"},
		},
		{
			name:        "native audio",
			artifactKey: "native",
			payload:     `{"planKey":"native","audioMode":"native","clips":[{"clipKey":"clip-1","sourceRevision":{"artifactId":"video-1","revisionId":"video-1-r1"},"trimStartMs":0,"trimEndMs":5000,"nativeAudioGainMilliDb":0,"transitionToNext":{"kind":"cut","durationMs":0}}],"audioTracks":[],"output":{"artifactKey":"final-native","container":"mp4","videoCodec":"h264","audioCodec":"aac","width":1280,"height":720,"frameRate":24}}`,
			want:        []string{"[0:a]atrim=start=0.000:end=5.000", "volume=0.000dB", "[aout]", "-c:a", "aac"},
			unwanted:    []string{"-an"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			draft := agentruntime.ArtifactDraft{
				ArtifactKey: test.artifactKey, Kind: "assembly_plan", SchemaVersion: 2,
				Payload: json.RawMessage(test.payload), UpstreamRevisions: []agentruntime.ArtifactRevisionRef{video},
			}
			command, err := BuildMediaAssemblyCommand(draft, []MediaAssemblyInput{input}, "/tmp/out.mp4")
			if err != nil {
				t.Fatalf("BuildMediaAssemblyCommand() error = %v", err)
			}
			joined := strings.Join(command.Arguments, "\n")
			for _, expected := range test.want {
				if !strings.Contains(joined, expected) {
					t.Fatalf("command args do not contain %q: %#v", expected, command.Arguments)
				}
			}
			for _, unexpected := range test.unwanted {
				if strings.Contains(joined, unexpected) {
					t.Fatalf("command args unexpectedly contain %q: %#v", unexpected, command.Arguments)
				}
			}
		})
	}
}

func assemblyCommandFixture(t *testing.T) (agentruntime.ArtifactDraft, []MediaAssemblyInput) {
	t.Helper()
	video1 := agentruntime.ArtifactRevisionRef{ArtifactID: "video-1", RevisionID: "video-1-r2"}
	video2 := agentruntime.ArtifactRevisionRef{ArtifactID: "video-2", RevisionID: "video-2-r1"}
	audio := agentruntime.ArtifactRevisionRef{ArtifactID: "audio-1", RevisionID: "audio-1-r3"}
	payload := json.RawMessage(`{
		"planKey":"assembly-final",
		"audioMode":"independent",
		"clips":[
			{"clipKey":"clip-1","sourceRevision":{"artifactId":"video-1","revisionId":"video-1-r2"},"trimStartMs":0,"trimEndMs":5000,"nativeAudioGainMilliDb":null,"transitionToNext":{"kind":"crossfade","durationMs":500}},
			{"clipKey":"clip-2","sourceRevision":{"artifactId":"video-2","revisionId":"video-2-r1"},"trimStartMs":250,"trimEndMs":4250,"nativeAudioGainMilliDb":null,"transitionToNext":{"kind":"cut","durationMs":0}}
		],
		"audioTracks":[
			{"trackKey":"dialogue","sourceRevision":{"artifactId":"audio-1","revisionId":"audio-1-r3"},"startMs":0,"trimStartMs":100,"trimEndMs":8100,"gainMilliDb":-1500}
		],
		"output":{"artifactKey":"final-film","container":"mp4","videoCodec":"h264","audioCodec":"aac","width":1920,"height":1080,"frameRate":25}
	}`)
	draft := agentruntime.ArtifactDraft{
		ArtifactKey: "assembly-final", Kind: "assembly_plan", SchemaVersion: 2, Payload: payload,
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{video1, video2, audio},
	}
	inputs := []MediaAssemblyInput{
		{Evidence: approvedAssemblyEvidence(video1, agentruntime.ArtifactVideo, "resource-video-1"), Resource: assemblyResource("resource-video-1", "video", "video/mp4", 5000), InputPath: "/tmp/video-1.mp4"},
		{Evidence: approvedAssemblyEvidence(video2, agentruntime.ArtifactVideo, "resource-video-2"), Resource: assemblyResource("resource-video-2", "video", "video/mp4", 4500), InputPath: "/tmp/video-2.mp4"},
		{Evidence: approvedAssemblyEvidence(audio, agentruntime.ArtifactAudio, "resource-audio-1"), Resource: assemblyResource("resource-audio-1", "audio", "audio/wav", 9000), InputPath: "/tmp/dialogue.wav"},
	}
	return draft, inputs
}

func approvedAssemblyEvidence(revision agentruntime.ArtifactRevisionRef, kind agentruntime.ArtifactKind, resourceID string) agentruntime.DeliveryArtifact {
	return agentruntime.DeliveryArtifact{
		Kind: kind, ArtifactID: revision.ArtifactID, RevisionID: revision.RevisionID,
		ResourceID: resourceID, ResourceReady: true, Approved: true,
	}
}

func assemblyResource(id string, kind string, mimeType string, durationMS int64) model.Resource {
	return model.Resource{
		ID: id, Kind: kind, Status: model.ResourceStatusReady, Provider: "oss",
		ObjectKey: "production/" + id, MimeType: mimeType, Size: 1024,
		DurationMs: durationMS, ETag: "etag-" + id,
	}
}

func mustDecodeAssemblyPlanV2(t *testing.T, payload []byte) agentruntime.AssemblyPlanV2 {
	t.Helper()
	plan, err := agentruntime.DecodeAssemblyPlanV2(payload)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func argumentFollows(arguments []string, flag string, value string) bool {
	for index := 0; index+1 < len(arguments); index++ {
		if arguments[index] == flag && arguments[index+1] == value {
			return true
		}
	}
	return false
}

func containsArgument(arguments []string, value string) bool {
	for _, argument := range arguments {
		if argument == value {
			return true
		}
	}
	return false
}
