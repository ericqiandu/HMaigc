package agentruntime_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
)

func TestValidateArtifactDraftRequiresExactUpstreamRevisionRefs(t *testing.T) {
	err := agentruntime.ValidateArtifactDraft(agentruntime.ArtifactDraft{
		ArtifactKey:   "script-1",
		Kind:          "brief",
		SchemaVersion: 1,
		Payload:       json.RawMessage(`{"title":"片名"}`),
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{
			{ArtifactID: "artifact-1"},
		},
	})
	if !errors.Is(err, agentruntime.ErrArtifactRevisionInvalid) {
		t.Fatalf("incomplete upstream revision error = %v, want %v", err, agentruntime.ErrArtifactRevisionInvalid)
	}
}

func TestValidateArtifactDraftAcceptsImmutableEnvelope(t *testing.T) {
	err := agentruntime.ValidateArtifactDraft(agentruntime.ArtifactDraft{
		ArtifactKey:          "storyboard-1",
		Kind:                 "storyboard_plan.v1",
		SchemaVersion:        1,
		Payload:              json.RawMessage(`{"shots":[{"shotKey":"shot-1"}]}`),
		ResourceID:           "resource-1",
		ModelRequestIdentity: "provider-request-1",
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{
			{ArtifactID: "artifact-script", RevisionID: "revision-script-2"},
		},
	})
	if err != nil {
		t.Fatalf("valid artifact draft rejected: %v", err)
	}
}

func TestValidateArtifactDraftRejectsMalformedPayload(t *testing.T) {
	err := agentruntime.ValidateArtifactDraft(agentruntime.ArtifactDraft{
		ArtifactKey:   "script-1",
		Kind:          "script_bundle.v1",
		SchemaVersion: 1,
		Payload:       json.RawMessage(`{"title":`),
	})
	if !errors.Is(err, agentruntime.ErrArtifactPayloadInvalid) {
		t.Fatalf("malformed payload error = %v, want %v", err, agentruntime.ErrArtifactPayloadInvalid)
	}
}

func TestAssemblyPlanArtifactSchemaHardCutsV1FromV2(t *testing.T) {
	video := agentruntime.ArtifactRevisionRef{ArtifactID: "video-1", RevisionID: "video-1-r1"}
	draft := agentruntime.ArtifactDraft{
		ArtifactKey: "assembly", Kind: "assembly_plan", SchemaVersion: 1,
		Payload:           json.RawMessage(`{"planKey":"assembly","audioMode":"none","videoRevisions":[{"artifactId":"video-1","revisionId":"video-1-r1"}],"audioRevisions":[],"outputArtifactKey":"final"}`),
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{video},
	}
	if err := agentruntime.ValidateArtifactDraft(draft); err != nil {
		t.Fatalf("read-only assembly_plan.v1 rejected: %v", err)
	}

	draft.SchemaVersion = 2
	if err := agentruntime.ValidateArtifactDraft(draft); !errors.Is(err, agentruntime.ErrArtifactPayloadInvalid) {
		t.Fatalf("legacy payload accepted as assembly_plan.v2: %v", err)
	}
}

func TestAssemblyPlanV2ArtifactKeyMustMatchPlanKey(t *testing.T) {
	video := agentruntime.ArtifactRevisionRef{ArtifactID: "video-1", RevisionID: "video-1-r1"}
	err := agentruntime.ValidateArtifactDraft(agentruntime.ArtifactDraft{
		ArtifactKey: "assembly-envelope", Kind: "assembly_plan", SchemaVersion: 2,
		Payload:           json.RawMessage(`{"planKey":"different-plan","audioMode":"none","clips":[{"clipKey":"clip-1","sourceRevision":{"artifactId":"video-1","revisionId":"video-1-r1"},"trimStartMs":0,"trimEndMs":5000,"nativeAudioGainMilliDb":null,"transitionToNext":{"kind":"cut","durationMs":0}}],"audioTracks":[],"output":{"artifactKey":"final","container":"mp4","videoCodec":"h264","audioCodec":"none","width":1280,"height":720,"frameRate":25}}`),
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{video},
	})
	if !errors.Is(err, agentruntime.ErrArtifactPayloadInvalid) {
		t.Fatalf("mismatched assembly plan key error = %v, want %v", err, agentruntime.ErrArtifactPayloadInvalid)
	}
}

func TestScriptBundleRequiresCompleteScriptAndCharacterCatalog(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{
			name:    "empty script",
			payload: `{"title":"雨夜","logline":"雨夜重逢","script":"","characters":[{"key":"hero","name":"林夏","description":"女记者"}],"scenes":[],"props":[],"voiceRoles":[]}`,
		},
		{
			name:    "empty character catalog",
			payload: `{"title":"雨夜","logline":"雨夜重逢","script":"第一场","characters":[],"scenes":[],"props":[],"voiceRoles":[]}`,
		},
		{
			name:    "missing explicit catalogs",
			payload: `{"title":"雨夜","logline":"雨夜重逢","script":"第一场","characters":[{"key":"hero","name":"林夏","description":"女记者"}]}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := agentruntime.ValidateArtifactDraft(agentruntime.ArtifactDraft{
				ArtifactKey: "script", Kind: "script_bundle", SchemaVersion: 1,
				Payload: json.RawMessage(test.payload), UpstreamRevisions: []agentruntime.ArtifactRevisionRef{},
			})
			if !errors.Is(err, agentruntime.ErrArtifactPayloadInvalid) {
				t.Fatalf("script bundle error = %v, want %v", err, agentruntime.ErrArtifactPayloadInvalid)
			}
		})
	}
}

func TestScriptBundleAcceptsCompleteStrictPayload(t *testing.T) {
	err := agentruntime.ValidateArtifactDraft(agentruntime.ArtifactDraft{
		ArtifactKey: "script", Kind: "script_bundle", SchemaVersion: 1,
		Payload: json.RawMessage(`{
			"title":"雨夜",
			"logline":"女记者在暴雨夜追查失踪案",
			"script":"第一场：林夏进入废弃车站。",
			"characters":[{"key":"hero","name":"林夏","description":"冷静敏锐的女记者"}],
			"scenes":[{"key":"station","name":"废弃车站","description":"暴雨中的旧站台"}],
			"props":[{"key":"recorder","name":"录音笔","description":"带有关键录音"}],
			"voiceRoles":[{"key":"hero_voice","name":"林夏对白","description":"克制、清晰的女声"}]
		}`),
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{},
	})
	if err != nil {
		t.Fatalf("complete script bundle rejected: %v", err)
	}
}

func TestAssetBindingRequiresExactStateAndConfirmedMatches(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{
			name:    "confirmed but unresolved",
			payload: `{"bindingKey":"cast","scriptRevision":{"artifactId":"script","revisionId":"script-r1"},"confirmed":true,"entries":[{"requirementKey":"hero","requirementKind":"character","state":"missing","candidateResourceIds":[]}]}`,
		},
		{
			name:    "matched without resource",
			payload: `{"bindingKey":"cast","scriptRevision":{"artifactId":"script","revisionId":"script-r1"},"confirmed":false,"entries":[{"requirementKey":"hero","requirementKind":"character","state":"matched","candidateResourceIds":[]}]}`,
		},
		{
			name:    "duplicate requirement key",
			payload: `{"bindingKey":"cast","scriptRevision":{"artifactId":"script","revisionId":"script-r1"},"confirmed":false,"entries":[{"requirementKey":"hero","requirementKind":"character","state":"missing","candidateResourceIds":[]},{"requirementKey":"hero","requirementKind":"character","state":"missing","candidateResourceIds":[]}]}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := agentruntime.ValidateArtifactDraft(agentruntime.ArtifactDraft{
				ArtifactKey: "bindings", Kind: "asset_binding", SchemaVersion: 1,
				Payload: json.RawMessage(test.payload), UpstreamRevisions: []agentruntime.ArtifactRevisionRef{{ArtifactID: "script", RevisionID: "script-r1"}},
			})
			if !errors.Is(err, agentruntime.ErrArtifactPayloadInvalid) {
				t.Fatalf("asset binding error = %v, want %v", err, agentruntime.ErrArtifactPayloadInvalid)
			}
		})
	}
}

func TestCinematicArtifactSchemasRequireExactUpstreamRevisions(t *testing.T) {
	script := agentruntime.ArtifactRevisionRef{ArtifactID: "script", RevisionID: "script-r1"}
	binding := agentruntime.ArtifactRevisionRef{ArtifactID: "bindings", RevisionID: "bindings-r1"}
	evidence := agentruntime.ArtifactRevisionRef{ArtifactID: "evidence", RevisionID: "evidence-r1"}
	reference := agentruntime.ArtifactRevisionRef{ArtifactID: "reference", RevisionID: "reference-r1"}
	bible := agentruntime.CharacterVisualBible{
		ScriptRevision: script, AssetBindingRevision: binding,
		VisualEvidenceRevisions: []agentruntime.ArtifactRevisionRef{evidence},
		ReferenceAssetRevisions: []agentruntime.ArtifactRevisionRef{reference},
		Characters: []agentruntime.CharacterVisualProfile{{
			CharacterKey: "hero", CanonicalName: "林夏", Aliases: []string{},
			StaticFeatures: []agentruntime.CharacterVisualFact{{
				FactKey: "face", Description: "左眼下方有泪痣", EvidenceRefs: []agentruntime.ArtifactRevisionRef{evidence},
			}},
			DynamicFeatures:    []agentruntime.CharacterDynamicFeature{},
			ReferenceRevisions: []agentruntime.ArtifactRevisionRef{reference},
			Unknowns:           []agentruntime.VisualEvidenceIssue{}, Conflicts: []agentruntime.VisualEvidenceIssue{},
		}},
	}
	payload, err := json.Marshal(bible)
	if err != nil {
		t.Fatal(err)
	}
	draft := agentruntime.ArtifactDraft{
		ArtifactKey: "character-bible", Kind: "character_visual_bible", SchemaVersion: 1, Payload: payload,
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{script, binding, evidence, reference},
	}
	if err := agentruntime.ValidateArtifactDraft(draft); err != nil {
		t.Fatalf("valid character bible draft rejected: %v", err)
	}

	draft.UpstreamRevisions[2].RevisionID = "evidence-r2"
	if err := agentruntime.ValidateArtifactDraft(draft); !errors.Is(err, agentruntime.ErrArtifactPayloadInvalid) {
		t.Fatalf("mismatched character bible upstream error = %v, want %v", err, agentruntime.ErrArtifactPayloadInvalid)
	}
}

func TestStoryboardPlanRejectsUnknownShotInputRevision(t *testing.T) {
	script := agentruntime.ArtifactRevisionRef{ArtifactID: "script", RevisionID: "script-r1"}
	binding := agentruntime.ArtifactRevisionRef{ArtifactID: "bindings", RevisionID: "bindings-r1"}
	bible := agentruntime.ArtifactRevisionRef{ArtifactID: "character-bible", RevisionID: "character-bible-r1"}
	unknown := agentruntime.ArtifactRevisionRef{ArtifactID: "unknown", RevisionID: "unknown-r1"}
	plan := agentruntime.StoryboardPlan{
		ScriptRevision: script, AssetBindingRevision: binding, CharacterBibleRevision: bible,
		VisualEvidenceRevisions: []agentruntime.ArtifactRevisionRef{}, TargetDurationMS: 3000,
		Shots: []agentruntime.CinematicShot{{
			ShotKey: "shot-1", NarrativePurpose: "建立空间", ShotSize: "全景", CameraPosition: "入口",
			Angle: "平视", Composition: "中心构图", ScreenDirection: "人物面向右侧", CameraMotion: "固定",
			OnScreenAction: "人物进入", Dialogue: []agentruntime.DialogueLine{}, Sound: []agentruntime.SoundCue{},
			DurationMS: 3000, Transition: "直接切入", VisibleCharacterKeys: []string{"hero"},
			InputRevisions: []agentruntime.ArtifactRevisionRef{unknown},
			FramePlan: agentruntime.FirstMotionLastFrame{
				FirstFrame: agentruntime.FrameState{State: "空入口", Static: true}, Motion: "人物走入",
				LastFrame: agentruntime.FrameState{State: "人物停下", Static: true},
			},
		}},
	}
	payload, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	draft := agentruntime.ArtifactDraft{
		ArtifactKey: "storyboard", Kind: "storyboard_plan", SchemaVersion: 1, Payload: payload,
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{script, binding, bible},
	}
	if err := agentruntime.ValidateArtifactDraft(draft); !errors.Is(err, agentruntime.ErrArtifactPayloadInvalid) {
		t.Fatalf("unknown shot input error = %v, want %v", err, agentruntime.ErrArtifactPayloadInvalid)
	}

	plan.Shots[0].InputRevisions = []agentruntime.ArtifactRevisionRef{binding}
	payload, err = json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	draft.Payload = payload
	if err := agentruntime.ValidateArtifactDraft(draft); err != nil {
		t.Fatalf("valid storyboard plan rejected: %v", err)
	}
}

func TestCameraTreeArtifactRejectsUnknownFieldsAndMismatchedUpstream(t *testing.T) {
	storyboard := agentruntime.ArtifactRevisionRef{ArtifactID: "storyboard", RevisionID: "storyboard-r1"}
	tree := agentruntime.CameraTree{
		StoryboardRevision: storyboard, VisualEvidenceRevisions: []agentruntime.ArtifactRevisionRef{},
		ShotKeys: []string{"shot-1"},
		Cameras: []agentruntime.CameraNode{{
			CameraKey: "wide", ShotKeys: []string{"shot-1"}, SubjectKeys: []string{"hero"},
			ShotSize: "全景", Angle: "平视", ScreenDirection: "面向右侧", Purpose: "建立空间",
		}},
		Relations: []agentruntime.CameraRelation{}, MissingViews: []agentruntime.CameraCoverageGap{},
	}
	payload, err := json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	draft := agentruntime.ArtifactDraft{
		ArtifactKey: "camera-tree", Kind: "camera_tree", SchemaVersion: 1, Payload: payload,
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{storyboard},
	}
	if err := agentruntime.ValidateArtifactDraft(draft); err != nil {
		t.Fatalf("valid camera tree rejected: %v", err)
	}

	draft.UpstreamRevisions[0].RevisionID = "storyboard-r2"
	if err := agentruntime.ValidateArtifactDraft(draft); !errors.Is(err, agentruntime.ErrArtifactPayloadInvalid) {
		t.Fatalf("mismatched camera tree upstream error = %v, want %v", err, agentruntime.ErrArtifactPayloadInvalid)
	}

	draft.Payload = json.RawMessage(`{"storyboardRevision":{"artifactId":"storyboard","revisionId":"storyboard-r1"},"visualEvidenceRevisions":[],"shotKeys":["shot-1"],"cameras":[],"relations":[],"missingViews":[],"unexpected":true}`)
	if err := agentruntime.ValidateArtifactDraft(draft); !errors.Is(err, agentruntime.ErrArtifactPayloadInvalid) {
		t.Fatalf("unknown camera tree field error = %v, want %v", err, agentruntime.ErrArtifactPayloadInvalid)
	}
}

func TestFirstMotionLastFrameArtifactRequiresExactInputRevisions(t *testing.T) {
	storyboard := agentruntime.ArtifactRevisionRef{ArtifactID: "storyboard", RevisionID: "storyboard-r1"}
	value := agentruntime.FirstMotionLastFrame{
		FirstFrame: agentruntime.FrameState{State: "人物位于入口", Static: true}, Motion: "人物走向门口",
		LastFrame:      agentruntime.FrameState{State: "人物停在门口", Static: true},
		InputRevisions: []agentruntime.ArtifactRevisionRef{storyboard},
	}
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	draft := agentruntime.ArtifactDraft{
		ArtifactKey: "shot-1-frame-plan", Kind: "first_motion_last_frame", SchemaVersion: 1, Payload: payload,
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{storyboard},
	}
	if err := agentruntime.ValidateArtifactDraft(draft); err != nil {
		t.Fatalf("valid frame plan rejected: %v", err)
	}

	draft.UpstreamRevisions = []agentruntime.ArtifactRevisionRef{}
	if err := agentruntime.ValidateArtifactDraft(draft); !errors.Is(err, agentruntime.ErrArtifactPayloadInvalid) {
		t.Fatalf("missing frame-plan upstream error = %v, want %v", err, agentruntime.ErrArtifactPayloadInvalid)
	}
}

func TestStageReviewResolutionContentRequiresDecisionStatusMatch(t *testing.T) {
	base := agentruntime.StageReviewResolutionContent{
		ContentType: agentruntime.StageReviewContentType, StageID: "stage-script", StageVersion: 3,
		RevisionID: "revision-script-1", Decision: agentruntime.StageReviewApprove, ClientRequestID: "review-1",
		ResultStageVersion: 4, ResultStatus: agentruntime.StageApproved,
		ResultReviewRevisionID: "revision-script-1", ResultUpdatedAt: time.Now().UTC(),
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid stage review resolution rejected: %v", err)
	}
	for _, testCase := range []struct {
		name     string
		decision agentruntime.StageReviewDecision
		status   agentruntime.ProductionStageStatus
	}{
		{name: "approve running", decision: agentruntime.StageReviewApprove, status: agentruntime.StageRunning},
		{name: "revision approved", decision: agentruntime.StageReviewRequestRevision, status: agentruntime.StageApproved},
		{name: "stop approved", decision: agentruntime.StageReviewStop, status: agentruntime.StageApproved},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			content := base
			content.Decision = testCase.decision
			content.ResultStatus = testCase.status
			if testCase.decision == agentruntime.StageReviewRequestRevision {
				content.ResultReviewRevisionID = ""
			}
			if err := content.Validate(); err == nil {
				t.Fatal("mismatched stage review resolution was accepted")
			}
		})
	}
}

func TestStageReviewPublicationIntentRequiresApprovalAndRoundTripsStrictly(t *testing.T) {
	intent := &agentruntime.AssetPublicationIntent{
		PublicationPurpose: "character-library",
		TargetCategory:     "character",
		TargetBindingKey:   "hero",
	}
	base := agentruntime.StageReviewResolutionContent{
		ContentType: agentruntime.StageReviewContentType, StageID: "stage-character", StageVersion: 2,
		RevisionID: "revision-character-1", Decision: agentruntime.StageReviewApprove, ClientRequestID: "review-character-1",
		PublicationIntent: intent, ResultStageVersion: 3, ResultStatus: agentruntime.StageApproved,
		ResultReviewRevisionID: "revision-character-1", ResultUpdatedAt: time.Now().UTC(),
	}
	encoded, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := agentruntime.DecodeStageReviewResolutionContent(encoded)
	if err != nil {
		t.Fatalf("publication intent round trip failed: %v", err)
	}
	if decoded.PublicationIntent == nil || *decoded.PublicationIntent != *intent {
		t.Fatalf("publication intent = %#v, want %#v", decoded.PublicationIntent, intent)
	}

	invalidDecision := base
	invalidDecision.Decision = agentruntime.StageReviewRequestRevision
	invalidDecision.ResultStatus = agentruntime.StageRunning
	invalidDecision.ResultReviewRevisionID = ""
	if err := invalidDecision.Validate(); err == nil {
		t.Fatal("revision request accepted a publication intent")
	}

	invalidCategory := base
	invalidCategory.PublicationIntent = &agentruntime.AssetPublicationIntent{
		PublicationPurpose: "character-library", TargetCategory: "unknown", TargetBindingKey: "hero",
	}
	if err := invalidCategory.Validate(); err == nil {
		t.Fatal("unknown publication category was accepted")
	}
}
