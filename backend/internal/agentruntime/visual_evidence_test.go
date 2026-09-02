package agentruntime

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestDecodeVisualEvidenceRequiresStableReferencedKeys(t *testing.T) {
	source := ArtifactRevisionRef{ArtifactID: "source-image", RevisionID: "source-image-r1"}
	evidence := VisualEvidence{
		SourceRevision: source,
		Characters: []VisualCharacter{{
			Key: "character-linxia", Name: "林夏", Clothing: "米白风衣", Hair: "黑色长发",
			StableFeatures: []string{"左眼下方泪痣"},
		}},
		IdentityEvidence: []VisualIdentityEvidence{{
			CharacterKey: "character-linxia", Observations: []string{"人物位于画面左侧"},
		}},
		Scene: VisualSceneEvidence{Key: "scene-rainy-street", Description: "雨夜街道"},
		Props: []VisualPropEvidence{{Key: "prop-umbrella", Name: "黑伞", Description: "人物右手持伞"}},
		SpatialRelations: []VisualSpatialRelation{{
			SubjectKey: "character-linxia", Relation: "holds", ObjectKey: "prop-umbrella",
		}},
		Shot: VisualShotEvidence{
			ShotSize: "中景", Angle: "平视", Composition: "人物偏左",
			ScreenDirection: "面向画面右侧", Gaze: "看向街道尽头",
			FirstFrameCondition: "人物静止持伞", LastFrameCondition: "人物仍位于左侧",
		},
		ActionState: "人物站立等待",
		OCRText:     []string{},
		Uncertainties: []VisualEvidenceIssue{{
			Code: "face-partially-occluded", Description: "伞沿遮挡部分面部", RelatedKeys: []string{"character-linxia"},
		}},
		Conflicts:             []VisualEvidenceIssue{},
		ConfidenceBasisPoints: 8750,
		VisionModelRecordID:   "vision-model-record-1",
		RequestIdentity:       "provider-request-1",
	}
	payload, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := DecodeVisualEvidence(payload)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.SourceRevision != source || decoded.Characters[0].Key != "character-linxia" || decoded.ConfidenceBasisPoints != 8750 {
		t.Fatalf("decoded evidence = %#v", decoded)
	}

	evidence.SpatialRelations[0].ObjectKey = "missing-prop"
	payload, err = json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeVisualEvidence(payload); !errors.Is(err, ErrArtifactPayloadInvalid) {
		t.Fatalf("dangling visual key error = %v, want %v", err, ErrArtifactPayloadInvalid)
	}
}

func TestVisualEvidenceArtifactRequiresExactSourceRevision(t *testing.T) {
	source := ArtifactRevisionRef{ArtifactID: "source-image", RevisionID: "source-image-r1"}
	payload := json.RawMessage(`{
		"sourceRevision":{"artifactId":"source-image","revisionId":"source-image-r1"},
		"characters":[],"identityEvidence":[],
		"scene":{"key":"scene-1","description":"空旷室内"},
		"props":[],"spatialRelations":[],
		"shot":{"shotSize":"全景","angle":"平视","composition":"中心构图","screenDirection":"无","gaze":"无","firstFrameCondition":"空镜","lastFrameCondition":"空镜"},
		"actionState":"无人物动作","ocrText":[],"uncertainties":[],"conflicts":[],
		"confidenceBasisPoints":9300,"visionModelRecordId":"vision-model-record-1","requestIdentity":"provider-request-1"
	}`)
	draft := ArtifactDraft{
		ArtifactKey: "visual-evidence-source-image", Kind: "visual_evidence", SchemaVersion: 1,
		Payload: payload, UpstreamRevisions: []ArtifactRevisionRef{source}, ModelRequestIdentity: "provider-request-1",
	}
	if err := ValidateArtifactDraft(draft); err != nil {
		t.Fatal(err)
	}

	draft.UpstreamRevisions[0].RevisionID = "source-image-r2"
	if err := ValidateArtifactDraft(draft); !errors.Is(err, ErrArtifactPayloadInvalid) {
		t.Fatalf("mismatched source revision error = %v, want %v", err, ErrArtifactPayloadInvalid)
	}
}

func TestCharacterVisualBibleRequiresExactEvidenceRefsAndUniqueCharacters(t *testing.T) {
	script := ArtifactRevisionRef{ArtifactID: "script", RevisionID: "script-r1"}
	binding := ArtifactRevisionRef{ArtifactID: "bindings", RevisionID: "bindings-r2"}
	evidence := ArtifactRevisionRef{ArtifactID: "hero-evidence", RevisionID: "hero-evidence-r1"}
	reference := ArtifactRevisionRef{ArtifactID: "hero-reference", RevisionID: "hero-reference-r3"}
	bible := CharacterVisualBible{
		ScriptRevision:          script,
		AssetBindingRevision:    binding,
		VisualEvidenceRevisions: []ArtifactRevisionRef{evidence},
		ReferenceAssetRevisions: []ArtifactRevisionRef{reference},
		Characters: []CharacterVisualProfile{{
			CharacterKey: "hero", CanonicalName: "林夏", Aliases: []string{"记者"},
			StaticFeatures: []CharacterVisualFact{{
				FactKey: "hero-face", Description: "左眼下方有泪痣", EvidenceRefs: []ArtifactRevisionRef{evidence},
			}},
			DynamicFeatures: []CharacterDynamicFeature{{
				FeatureKey: "hero-scene-coat", ScopeKey: "scene-station", Description: "米白色风衣",
				EvidenceRefs: []ArtifactRevisionRef{evidence},
			}},
			ReferenceRevisions: []ArtifactRevisionRef{reference}, Unknowns: []VisualEvidenceIssue{}, Conflicts: []VisualEvidenceIssue{},
		}},
	}
	if err := ValidateCharacterVisualBible(bible); err != nil {
		t.Fatalf("valid character visual bible rejected: %v", err)
	}

	duplicate := bible
	duplicate.Characters = append(append([]CharacterVisualProfile(nil), bible.Characters...), bible.Characters[0])
	if err := ValidateCharacterVisualBible(duplicate); !errors.Is(err, ErrArtifactPayloadInvalid) {
		t.Fatalf("duplicate character error = %v, want %v", err, ErrArtifactPayloadInvalid)
	}

	unknownEvidence := bible
	unknownEvidence.Characters = append([]CharacterVisualProfile(nil), bible.Characters...)
	unknownEvidence.Characters[0].StaticFeatures = append([]CharacterVisualFact(nil), bible.Characters[0].StaticFeatures...)
	unknownEvidence.Characters[0].StaticFeatures[0].EvidenceRefs = []ArtifactRevisionRef{{ArtifactID: "unknown", RevisionID: "unknown-r1"}}
	if err := ValidateCharacterVisualBible(unknownEvidence); !errors.Is(err, ErrArtifactPayloadInvalid) {
		t.Fatalf("unknown evidence error = %v, want %v", err, ErrArtifactPayloadInvalid)
	}

	wrongEvidenceKind := bible
	wrongEvidenceKind.Characters = append([]CharacterVisualProfile(nil), bible.Characters...)
	wrongEvidenceKind.Characters[0].StaticFeatures = append([]CharacterVisualFact(nil), bible.Characters[0].StaticFeatures...)
	wrongEvidenceKind.Characters[0].StaticFeatures[0].EvidenceRefs = []ArtifactRevisionRef{reference}
	if err := ValidateCharacterVisualBible(wrongEvidenceKind); !errors.Is(err, ErrArtifactPayloadInvalid) {
		t.Fatalf("reference asset used as evidence error = %v, want %v", err, ErrArtifactPayloadInvalid)
	}

	wrongReferenceKind := bible
	wrongReferenceKind.Characters = append([]CharacterVisualProfile(nil), bible.Characters...)
	wrongReferenceKind.Characters[0].ReferenceRevisions = []ArtifactRevisionRef{evidence}
	if err := ValidateCharacterVisualBible(wrongReferenceKind); !errors.Is(err, ErrArtifactPayloadInvalid) {
		t.Fatalf("visual evidence used as reference asset error = %v, want %v", err, ErrArtifactPayloadInvalid)
	}
}

func TestCinematicShotRequiresBoundedDurationAndDeclaredInputRefs(t *testing.T) {
	binding := ArtifactRevisionRef{ArtifactID: "bindings", RevisionID: "bindings-r1"}
	unknown := ArtifactRevisionRef{ArtifactID: "unknown", RevisionID: "unknown-r1"}
	shot := CinematicShot{
		ShotKey: "shot-1", NarrativePurpose: "建立人物与车站空间", ShotSize: "全景", CameraPosition: "站台入口",
		Angle: "平视", Composition: "人物位于画面左侧", ScreenDirection: "面向画面右侧", CameraMotion: "固定机位",
		OnScreenAction: "林夏进入站台", Dialogue: []DialogueLine{{CharacterKey: "hero", Text: "有人吗？"}},
		Sound: []SoundCue{{CueKey: "rain", Description: "持续雨声"}}, DurationMS: 3200, Transition: "直接切入",
		VisibleCharacterKeys: []string{"hero"}, InputRevisions: []ArtifactRevisionRef{binding},
		FramePlan: FirstMotionLastFrame{
			FirstFrame: FrameState{State: "空站台入口", Static: true}, Motion: "林夏从左侧走入",
			LastFrame: FrameState{State: "林夏停在站台中央", Static: true},
		},
	}
	if err := ValidateCinematicShot(shot); err != nil {
		t.Fatalf("valid cinematic shot rejected: %v", err)
	}

	invalidDuration := shot
	invalidDuration.DurationMS = 0
	if err := ValidateCinematicShot(invalidDuration); !errors.Is(err, ErrArtifactPayloadInvalid) {
		t.Fatalf("duration error = %v, want %v", err, ErrArtifactPayloadInvalid)
	}

	duplicateInput := shot
	duplicateInput.InputRevisions = []ArtifactRevisionRef{binding, binding}
	if err := ValidateCinematicShot(duplicateInput); !errors.Is(err, ErrArtifactRevisionInvalid) {
		t.Fatalf("duplicate input error = %v, want %v", err, ErrArtifactRevisionInvalid)
	}

	undeclaredFrameEvidence := shot
	undeclaredFrameEvidence.FramePlan.FirstFrame.EvidenceRevisions = []ArtifactRevisionRef{unknown}
	if err := ValidateCinematicShot(undeclaredFrameEvidence); !errors.Is(err, ErrArtifactPayloadInvalid) {
		t.Fatalf("undeclared frame evidence error = %v, want %v", err, ErrArtifactPayloadInvalid)
	}

	unknownFrameCharacter := shot
	unknownFrameCharacter.FramePlan.LastFrame.VisibleCharacterKeys = []string{"unknown-character"}
	if err := ValidateCinematicShot(unknownFrameCharacter); !errors.Is(err, ErrArtifactPayloadInvalid) {
		t.Fatalf("unknown frame character error = %v, want %v", err, ErrArtifactPayloadInvalid)
	}
}

func TestCameraTreeRejectsSelfReferenceAndCycle(t *testing.T) {
	selfReference := CameraTree{
		StoryboardRevision: ArtifactRevisionRef{ArtifactID: "storyboard", RevisionID: "storyboard-r1"},
		ShotKeys:           []string{"shot-wide"},
		Cameras: []CameraNode{{
			CameraKey: "wide", ParentCameraKey: "wide", ShotKeys: []string{"shot-wide"}, SubjectKeys: []string{"hero"},
			ShotSize: "全景", Angle: "平视", ScreenDirection: "面向右侧", Purpose: "建立空间",
		}},
		VisualEvidenceRevisions: []ArtifactRevisionRef{}, Relations: []CameraRelation{}, MissingViews: []CameraCoverageGap{},
	}
	if err := ValidateCameraTree(selfReference); !errors.Is(err, ErrCameraTreeSelfReference) {
		t.Fatalf("self reference error = %v, want %v", err, ErrCameraTreeSelfReference)
	}

	cycle := CameraTree{
		StoryboardRevision: ArtifactRevisionRef{ArtifactID: "storyboard", RevisionID: "storyboard-r1"},
		ShotKeys:           []string{"shot-wide", "shot-close"},
		Cameras: []CameraNode{
			{CameraKey: "wide", ParentCameraKey: "close", ShotKeys: []string{"shot-wide"}, SubjectKeys: []string{"hero"}, ShotSize: "全景", Angle: "平视", ScreenDirection: "面向右侧", Purpose: "建立空间"},
			{CameraKey: "close", ParentCameraKey: "wide", ShotKeys: []string{"shot-close"}, SubjectKeys: []string{"hero"}, ShotSize: "近景", Angle: "平视", ScreenDirection: "面向右侧", Purpose: "强调反应"},
		},
		VisualEvidenceRevisions: []ArtifactRevisionRef{}, Relations: []CameraRelation{}, MissingViews: []CameraCoverageGap{},
	}
	if err := ValidateCameraTree(cycle); !errors.Is(err, ErrCameraTreeCycle) {
		t.Fatalf("cycle error = %v, want %v", err, ErrCameraTreeCycle)
	}
}

func TestCameraTreeMultipleRootsMustBeExplicitlyIndependent(t *testing.T) {
	tree := CameraTree{
		StoryboardRevision: ArtifactRevisionRef{ArtifactID: "storyboard", RevisionID: "storyboard-r1"},
		ShotKeys:           []string{"shot-wide", "shot-insert"},
		Cameras: []CameraNode{
			{CameraKey: "wide", ShotKeys: []string{"shot-wide"}, SubjectKeys: []string{"hero"}, ShotSize: "全景", Angle: "平视", ScreenDirection: "面向右侧", Purpose: "建立空间", Independent: true},
			{CameraKey: "insert", ShotKeys: []string{"shot-insert"}, SubjectKeys: []string{"recorder"}, ShotSize: "特写", Angle: "俯视", ScreenDirection: "保持右侧", Purpose: "交代线索"},
		},
		VisualEvidenceRevisions: []ArtifactRevisionRef{}, Relations: []CameraRelation{}, MissingViews: []CameraCoverageGap{},
	}
	if err := ValidateCameraTree(tree); !errors.Is(err, ErrArtifactPayloadInvalid) {
		t.Fatalf("implicit second root error = %v, want %v", err, ErrArtifactPayloadInvalid)
	}

	tree.Cameras[1].Independent = true
	if err := ValidateCameraTree(tree); err != nil {
		t.Fatalf("explicit independent roots rejected: %v", err)
	}

	tree.Cameras[1].ShotKeys = []string{"missing-shot"}
	if err := ValidateCameraTree(tree); !errors.Is(err, ErrArtifactPayloadInvalid) {
		t.Fatalf("missing shot reference error = %v, want %v", err, ErrArtifactPayloadInvalid)
	}
}

func TestCameraTreeRelationEvidenceMustUseDeclaredVisualEvidence(t *testing.T) {
	storyboard := ArtifactRevisionRef{ArtifactID: "storyboard", RevisionID: "storyboard-r1"}
	evidence := ArtifactRevisionRef{ArtifactID: "evidence", RevisionID: "evidence-r1"}
	tree := CameraTree{
		StoryboardRevision: storyboard, VisualEvidenceRevisions: []ArtifactRevisionRef{evidence},
		ShotKeys: []string{"shot-wide", "shot-close"},
		Cameras: []CameraNode{
			{CameraKey: "wide", Independent: true, ShotKeys: []string{"shot-wide"}, SubjectKeys: []string{"hero"}, ShotSize: "全景", Angle: "平视", ScreenDirection: "面向右侧", Purpose: "建立空间"},
			{CameraKey: "close", ParentCameraKey: "wide", ShotKeys: []string{"shot-close"}, SubjectKeys: []string{"hero"}, ShotSize: "近景", Angle: "平视", ScreenDirection: "面向右侧", Purpose: "强调反应"},
		},
		Relations: []CameraRelation{{
			RelationKey: "coverage", FromCameraKey: "wide", ToCameraKey: "close", Relation: "包含",
			EvidenceRefs: []ArtifactRevisionRef{storyboard},
		}},
		MissingViews: []CameraCoverageGap{},
	}
	if err := ValidateCameraTree(tree); !errors.Is(err, ErrArtifactPayloadInvalid) {
		t.Fatalf("storyboard used as relation evidence error = %v, want %v", err, ErrArtifactPayloadInvalid)
	}

	tree.Relations[0].EvidenceRefs = []ArtifactRevisionRef{evidence}
	if err := ValidateCameraTree(tree); err != nil {
		t.Fatalf("declared visual relation evidence rejected: %v", err)
	}
}

func TestFirstMotionLastFrameRequiresStaticBoundaryStates(t *testing.T) {
	value := FirstMotionLastFrame{
		FirstFrame: FrameState{State: "人物位于走廊入口"},
		Motion:     "人物跑到门口",
		LastFrame:  FrameState{State: "人物站在门口", Static: true},
	}
	if err := ValidateFirstMotionLastFrame(value); !errors.Is(err, ErrFrameBoundaryInvalid) {
		t.Fatalf("dynamic first frame error = %v, want %v", err, ErrFrameBoundaryInvalid)
	}

	value.FirstFrame.Static = true
	if err := ValidateFirstMotionLastFrame(value); err != nil {
		t.Fatalf("explicit static boundaries rejected: %v", err)
	}

	value.FirstFrame.EvidenceRevisions = []ArtifactRevisionRef{{ArtifactID: "evidence", RevisionID: "evidence-r1"}}
	if err := ValidateFirstMotionLastFrame(value); !errors.Is(err, ErrArtifactPayloadInvalid) {
		t.Fatalf("undeclared standalone evidence error = %v, want %v", err, ErrArtifactPayloadInvalid)
	}
}
