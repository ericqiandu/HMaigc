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
