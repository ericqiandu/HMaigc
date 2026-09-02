package agentruntime_test

import (
	"encoding/json"
	"errors"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
)

func TestVideoPlanRequiresAudioModeToMatchEverySegment(t *testing.T) {
	upstream := []agentruntime.ArtifactRevisionRef{{ArtifactID: "storyboard", RevisionID: "storyboard-r1"}}
	valid := agentruntime.ArtifactDraft{
		ArtifactKey: "video-plan", Kind: "video_plan", SchemaVersion: 1,
		Payload: json.RawMessage(`{
			"planKey":"video-plan","inputRevisions":[{"artifactId":"storyboard","revisionId":"storyboard-r1"}],
			"audioMode":"native","segments":[{"segmentKey":"shot-1","inputRevisions":[{"artifactId":"storyboard","revisionId":"storyboard-r1"}],"outputArtifactKey":"video-shot-1","generateAudio":true}]
		}`),
		UpstreamRevisions: upstream,
	}
	if err := agentruntime.ValidateArtifactDraft(valid); err != nil {
		t.Fatalf("valid native video plan rejected: %v", err)
	}

	invalid := valid
	invalid.Payload = json.RawMessage(`{
		"planKey":"video-plan","inputRevisions":[{"artifactId":"storyboard","revisionId":"storyboard-r1"}],
		"audioMode":"native","segments":[{"segmentKey":"shot-1","inputRevisions":[{"artifactId":"storyboard","revisionId":"storyboard-r1"}],"outputArtifactKey":"video-shot-1","generateAudio":false}]
	}`)
	if err := agentruntime.ValidateArtifactDraft(invalid); !errors.Is(err, agentruntime.ErrArtifactPayloadInvalid) {
		t.Fatalf("native video plan contradiction error = %v", err)
	}
}

func TestAudioPlanRequiresExactVoiceLineTimelineAndUpstream(t *testing.T) {
	upstream := []agentruntime.ArtifactRevisionRef{{ArtifactID: "script", RevisionID: "script-r2"}}
	valid := agentruntime.ArtifactDraft{
		ArtifactKey: "audio-plan", Kind: "audio_plan", SchemaVersion: 1,
		Payload: json.RawMessage(`{
			"planKey":"audio-plan","inputRevisions":[{"artifactId":"script","revisionId":"script-r2"}],
			"clips":[{"clipKey":"line-1","voiceBindingKey":"hero-voice","lineKey":"script-line-1","dialogue":"别回头。","startMs":0,"durationMs":1200,"outputArtifactKey":"audio-line-1"}]
		}`),
		UpstreamRevisions: upstream,
	}
	if err := agentruntime.ValidateArtifactDraft(valid); err != nil {
		t.Fatalf("valid audio plan rejected: %v", err)
	}

	invalid := valid
	invalid.UpstreamRevisions = []agentruntime.ArtifactRevisionRef{{ArtifactID: "script", RevisionID: "script-r1"}}
	if err := agentruntime.ValidateArtifactDraft(invalid); !errors.Is(err, agentruntime.ErrArtifactPayloadInvalid) {
		t.Fatalf("audio plan upstream drift error = %v", err)
	}
}

func TestAssemblyPlanSeparatesNativeAndIndependentAudio(t *testing.T) {
	video := agentruntime.ArtifactRevisionRef{ArtifactID: "video", RevisionID: "video-r1"}
	audio := agentruntime.ArtifactRevisionRef{ArtifactID: "audio", RevisionID: "audio-r1"}
	independent := agentruntime.ArtifactDraft{
		ArtifactKey: "assembly", Kind: "assembly_plan", SchemaVersion: 1,
		Payload: json.RawMessage(`{
			"planKey":"assembly","audioMode":"independent",
			"videoRevisions":[{"artifactId":"video","revisionId":"video-r1"}],
			"audioRevisions":[{"artifactId":"audio","revisionId":"audio-r1"}],"outputArtifactKey":"final-film"
		}`),
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{video, audio},
	}
	if err := agentruntime.ValidateArtifactDraft(independent); err != nil {
		t.Fatalf("valid independent-audio assembly rejected: %v", err)
	}

	nativeWithSeparateAudio := independent
	nativeWithSeparateAudio.Payload = json.RawMessage(`{
		"planKey":"assembly","audioMode":"native",
		"videoRevisions":[{"artifactId":"video","revisionId":"video-r1"}],
		"audioRevisions":[{"artifactId":"audio","revisionId":"audio-r1"}],"outputArtifactKey":"final-film"
	}`)
	if err := agentruntime.ValidateArtifactDraft(nativeWithSeparateAudio); !errors.Is(err, agentruntime.ErrArtifactPayloadInvalid) {
		t.Fatalf("native assembly accepted separate audio: %v", err)
	}
}

func TestAssemblyPlanV2AcceptsExplicitOrderedAssemblyContract(t *testing.T) {
	video1 := agentruntime.ArtifactRevisionRef{ArtifactID: "video-1", RevisionID: "video-1-r2"}
	video2 := agentruntime.ArtifactRevisionRef{ArtifactID: "video-2", RevisionID: "video-2-r1"}
	audio := agentruntime.ArtifactRevisionRef{ArtifactID: "audio-1", RevisionID: "audio-1-r3"}
	draft := agentruntime.ArtifactDraft{
		ArtifactKey: "assembly-final", Kind: "assembly_plan", SchemaVersion: 2,
		Payload: json.RawMessage(`{
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
		}`),
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{video1, video2, audio},
	}
	if err := agentruntime.ValidateArtifactDraft(draft); err != nil {
		t.Fatalf("valid assembly_plan.v2 rejected: %v", err)
	}
}

func TestAssemblyPlanV2RejectsIncompleteContradictoryAndStaleContracts(t *testing.T) {
	video1 := agentruntime.ArtifactRevisionRef{ArtifactID: "video-1", RevisionID: "video-1-r2"}
	video2 := agentruntime.ArtifactRevisionRef{ArtifactID: "video-2", RevisionID: "video-2-r1"}
	audio := agentruntime.ArtifactRevisionRef{ArtifactID: "audio-1", RevisionID: "audio-1-r3"}
	tests := []struct {
		name     string
		payload  string
		upstream []agentruntime.ArtifactRevisionRef
	}{
		{
			name:     "missing explicit trim start",
			payload:  `{"planKey":"assembly","audioMode":"none","clips":[{"clipKey":"clip-1","sourceRevision":{"artifactId":"video-1","revisionId":"video-1-r2"},"trimEndMs":5000,"nativeAudioGainMilliDb":null,"transitionToNext":{"kind":"cut","durationMs":0}}],"audioTracks":[],"output":{"artifactKey":"final","container":"mp4","videoCodec":"h264","audioCodec":"none","width":1280,"height":720,"frameRate":25}}`,
			upstream: []agentruntime.ArtifactRevisionRef{video1},
		},
		{
			name:     "missing explicit native audio absence",
			payload:  `{"planKey":"assembly","audioMode":"none","clips":[{"clipKey":"clip-1","sourceRevision":{"artifactId":"video-1","revisionId":"video-1-r2"},"trimStartMs":0,"trimEndMs":5000,"transitionToNext":{"kind":"cut","durationMs":0}}],"audioTracks":[],"output":{"artifactKey":"final","container":"mp4","videoCodec":"h264","audioCodec":"none","width":1280,"height":720,"frameRate":25}}`,
			upstream: []agentruntime.ArtifactRevisionRef{video1},
		},
		{
			name:     "negative trim start",
			payload:  `{"planKey":"assembly","audioMode":"none","clips":[{"clipKey":"clip-1","sourceRevision":{"artifactId":"video-1","revisionId":"video-1-r2"},"trimStartMs":-1,"trimEndMs":5000,"nativeAudioGainMilliDb":null,"transitionToNext":{"kind":"cut","durationMs":0}}],"audioTracks":[],"output":{"artifactKey":"final","container":"mp4","videoCodec":"h264","audioCodec":"none","width":1280,"height":720,"frameRate":25}}`,
			upstream: []agentruntime.ArtifactRevisionRef{video1},
		},
		{
			name:     "crossfade consumes the next clip",
			payload:  `{"planKey":"assembly","audioMode":"none","clips":[{"clipKey":"clip-1","sourceRevision":{"artifactId":"video-1","revisionId":"video-1-r2"},"trimStartMs":0,"trimEndMs":5000,"nativeAudioGainMilliDb":null,"transitionToNext":{"kind":"crossfade","durationMs":1000}},{"clipKey":"clip-2","sourceRevision":{"artifactId":"video-2","revisionId":"video-2-r1"},"trimStartMs":0,"trimEndMs":1000,"nativeAudioGainMilliDb":null,"transitionToNext":{"kind":"cut","durationMs":0}}],"audioTracks":[],"output":{"artifactKey":"final","container":"mp4","videoCodec":"h264","audioCodec":"none","width":1280,"height":720,"frameRate":25}}`,
			upstream: []agentruntime.ArtifactRevisionRef{video1, video2},
		},
		{
			name:     "native audio with independent track",
			payload:  `{"planKey":"assembly","audioMode":"native","clips":[{"clipKey":"clip-1","sourceRevision":{"artifactId":"video-1","revisionId":"video-1-r2"},"trimStartMs":0,"trimEndMs":5000,"nativeAudioGainMilliDb":0,"transitionToNext":{"kind":"cut","durationMs":0}}],"audioTracks":[{"trackKey":"dialogue","sourceRevision":{"artifactId":"audio-1","revisionId":"audio-1-r3"},"startMs":0,"trimStartMs":0,"trimEndMs":5000,"gainMilliDb":0}],"output":{"artifactKey":"final","container":"mp4","videoCodec":"h264","audioCodec":"aac","width":1280,"height":720,"frameRate":25}}`,
			upstream: []agentruntime.ArtifactRevisionRef{video1, audio},
		},
		{
			name:     "independent audio without tracks",
			payload:  `{"planKey":"assembly","audioMode":"independent","clips":[{"clipKey":"clip-1","sourceRevision":{"artifactId":"video-1","revisionId":"video-1-r2"},"trimStartMs":0,"trimEndMs":5000,"nativeAudioGainMilliDb":null,"transitionToNext":{"kind":"cut","durationMs":0}}],"audioTracks":[],"output":{"artifactKey":"final","container":"mp4","videoCodec":"h264","audioCodec":"aac","width":1280,"height":720,"frameRate":25}}`,
			upstream: []agentruntime.ArtifactRevisionRef{video1},
		},
		{
			name:     "independent audio missing explicit gain",
			payload:  `{"planKey":"assembly","audioMode":"independent","clips":[{"clipKey":"clip-1","sourceRevision":{"artifactId":"video-1","revisionId":"video-1-r2"},"trimStartMs":0,"trimEndMs":5000,"nativeAudioGainMilliDb":null,"transitionToNext":{"kind":"cut","durationMs":0}}],"audioTracks":[{"trackKey":"dialogue","sourceRevision":{"artifactId":"audio-1","revisionId":"audio-1-r3"},"startMs":0,"trimStartMs":0,"trimEndMs":5000}],"output":{"artifactKey":"final","container":"mp4","videoCodec":"h264","audioCodec":"aac","width":1280,"height":720,"frameRate":25}}`,
			upstream: []agentruntime.ArtifactRevisionRef{video1, audio},
		},
		{
			name:     "audio track extends beyond maximum timeline",
			payload:  `{"planKey":"assembly","audioMode":"independent","clips":[{"clipKey":"clip-1","sourceRevision":{"artifactId":"video-1","revisionId":"video-1-r2"},"trimStartMs":0,"trimEndMs":5000,"nativeAudioGainMilliDb":null,"transitionToNext":{"kind":"cut","durationMs":0}}],"audioTracks":[{"trackKey":"dialogue","sourceRevision":{"artifactId":"audio-1","revisionId":"audio-1-r3"},"startMs":43199999,"trimStartMs":0,"trimEndMs":2000,"gainMilliDb":0}],"output":{"artifactKey":"final","container":"mp4","videoCodec":"h264","audioCodec":"aac","width":1280,"height":720,"frameRate":25}}`,
			upstream: []agentruntime.ArtifactRevisionRef{video1, audio},
		},
		{
			name:     "crossfade next clip missing explicit trim",
			payload:  `{"planKey":"assembly","audioMode":"none","clips":[{"clipKey":"clip-1","sourceRevision":{"artifactId":"video-1","revisionId":"video-1-r2"},"trimStartMs":0,"trimEndMs":5000,"nativeAudioGainMilliDb":null,"transitionToNext":{"kind":"crossfade","durationMs":500}},{"clipKey":"clip-2","sourceRevision":{"artifactId":"video-2","revisionId":"video-2-r1"},"nativeAudioGainMilliDb":null,"transitionToNext":{"kind":"cut","durationMs":0}}],"audioTracks":[],"output":{"artifactKey":"final","container":"mp4","videoCodec":"h264","audioCodec":"none","width":1280,"height":720,"frameRate":25}}`,
			upstream: []agentruntime.ArtifactRevisionRef{video1, video2},
		},
		{
			name:     "unsupported container and codec",
			payload:  `{"planKey":"assembly","audioMode":"none","clips":[{"clipKey":"clip-1","sourceRevision":{"artifactId":"video-1","revisionId":"video-1-r2"},"trimStartMs":0,"trimEndMs":5000,"nativeAudioGainMilliDb":null,"transitionToNext":{"kind":"cut","durationMs":0}}],"audioTracks":[],"output":{"artifactKey":"final","container":"mkv","videoCodec":"copy","audioCodec":"none","width":1280,"height":720,"frameRate":25}}`,
			upstream: []agentruntime.ArtifactRevisionRef{video1},
		},
		{
			name:     "output resolution exceeds execution limit",
			payload:  `{"planKey":"assembly","audioMode":"none","clips":[{"clipKey":"clip-1","sourceRevision":{"artifactId":"video-1","revisionId":"video-1-r2"},"trimStartMs":0,"trimEndMs":5000,"nativeAudioGainMilliDb":null,"transitionToNext":{"kind":"cut","durationMs":0}}],"audioTracks":[],"output":{"artifactKey":"final","container":"mp4","videoCodec":"h264","audioCodec":"none","width":16384,"height":16384,"frameRate":25}}`,
			upstream: []agentruntime.ArtifactRevisionRef{video1},
		},
		{
			name:     "output pixel count exceeds execution limit",
			payload:  `{"planKey":"assembly","audioMode":"none","clips":[{"clipKey":"clip-1","sourceRevision":{"artifactId":"video-1","revisionId":"video-1-r2"},"trimStartMs":0,"trimEndMs":5000,"nativeAudioGainMilliDb":null,"transitionToNext":{"kind":"cut","durationMs":0}}],"audioTracks":[],"output":{"artifactKey":"final","container":"mp4","videoCodec":"h264","audioCodec":"none","width":8192,"height":8192,"frameRate":25}}`,
			upstream: []agentruntime.ArtifactRevisionRef{video1},
		},
		{
			name:     "combined video exceeds maximum timeline",
			payload:  `{"planKey":"assembly","audioMode":"none","clips":[{"clipKey":"clip-1","sourceRevision":{"artifactId":"video-1","revisionId":"video-1-r2"},"trimStartMs":0,"trimEndMs":30000000,"nativeAudioGainMilliDb":null,"transitionToNext":{"kind":"cut","durationMs":0}},{"clipKey":"clip-2","sourceRevision":{"artifactId":"video-2","revisionId":"video-2-r1"},"trimStartMs":0,"trimEndMs":30000000,"nativeAudioGainMilliDb":null,"transitionToNext":{"kind":"cut","durationMs":0}}],"audioTracks":[],"output":{"artifactKey":"final","container":"mp4","videoCodec":"h264","audioCodec":"none","width":1280,"height":720,"frameRate":25}}`,
			upstream: []agentruntime.ArtifactRevisionRef{video1, video2},
		},
		{
			name:     "unknown json field",
			payload:  `{"planKey":"assembly","audioMode":"none","clips":[{"clipKey":"clip-1","sourceRevision":{"artifactId":"video-1","revisionId":"video-1-r2"},"trimStartMs":0,"trimEndMs":5000,"nativeAudioGainMilliDb":null,"transitionToNext":{"kind":"cut","durationMs":0},"unknown":true}],"audioTracks":[],"output":{"artifactKey":"final","container":"mp4","videoCodec":"h264","audioCodec":"none","width":1280,"height":720,"frameRate":25}}`,
			upstream: []agentruntime.ArtifactRevisionRef{video1},
		},
		{
			name:     "stale upstream revision",
			payload:  `{"planKey":"assembly","audioMode":"independent","clips":[{"clipKey":"clip-1","sourceRevision":{"artifactId":"video-1","revisionId":"video-1-r2"},"trimStartMs":0,"trimEndMs":5000,"nativeAudioGainMilliDb":null,"transitionToNext":{"kind":"crossfade","durationMs":500}},{"clipKey":"clip-2","sourceRevision":{"artifactId":"video-2","revisionId":"video-2-r1"},"trimStartMs":250,"trimEndMs":4250,"nativeAudioGainMilliDb":null,"transitionToNext":{"kind":"cut","durationMs":0}}],"audioTracks":[{"trackKey":"dialogue","sourceRevision":{"artifactId":"audio-1","revisionId":"audio-1-r3"},"startMs":0,"trimStartMs":100,"trimEndMs":8100,"gainMilliDb":-1500}],"output":{"artifactKey":"final","container":"mp4","videoCodec":"h264","audioCodec":"aac","width":1920,"height":1080,"frameRate":25}}`,
			upstream: []agentruntime.ArtifactRevisionRef{{ArtifactID: "video-1", RevisionID: "video-1-r1"}, video2, audio},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := agentruntime.ValidateArtifactDraft(agentruntime.ArtifactDraft{
				ArtifactKey: "assembly", Kind: "assembly_plan", SchemaVersion: 2,
				Payload: json.RawMessage(test.payload), UpstreamRevisions: test.upstream,
			})
			if !errors.Is(err, agentruntime.ErrArtifactPayloadInvalid) {
				t.Fatalf("assembly_plan.v2 error = %v, want %v", err, agentruntime.ErrArtifactPayloadInvalid)
			}
		})
	}
}
