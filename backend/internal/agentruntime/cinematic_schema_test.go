package agentruntime

import (
	"errors"
	"testing"
)

func TestShotRevisionDependencyHashIsCanonical(t *testing.T) {
	dependencies := []ArtifactRevisionRef{
		{ArtifactID: "character-artifact", RevisionID: "character-r2"},
		{ArtifactID: "script-artifact", RevisionID: "script-r4"},
	}
	shot := cinematicShotRevisionFixture(dependencies)

	first, err := NewCinematicShotRevision(shot, 3)
	if err != nil {
		t.Fatal(err)
	}
	shot.InputRevisions[0], shot.InputRevisions[1] = shot.InputRevisions[1], shot.InputRevisions[0]
	second, err := NewCinematicShotRevision(shot, 3)
	if err != nil {
		t.Fatal(err)
	}
	if first.DependencyHash != second.DependencyHash {
		t.Fatalf("dependency hashes differ by input order: %q != %q", first.DependencyHash, second.DependencyHash)
	}
	if first.Revision != 3 || first.Shot.ShotKey != "shot-courtyard-1" {
		t.Fatalf("shot revision = %#v", first)
	}
	if err := ValidateCinematicShotRevision(first); err != nil {
		t.Fatalf("valid shot revision rejected: %v", err)
	}

	first.DependencyHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := ValidateCinematicShotRevision(first); !errors.Is(err, ErrShotRevisionInvalid) {
		t.Fatalf("tampered dependency hash error = %v, want %v", err, ErrShotRevisionInvalid)
	}
}

func TestShotRevisionDependencyHashRejectsDuplicateExactRevision(t *testing.T) {
	dependency := ArtifactRevisionRef{ArtifactID: "script-artifact", RevisionID: "script-r4"}
	shot := cinematicShotRevisionFixture([]ArtifactRevisionRef{dependency, dependency})

	if _, err := NewCinematicShotRevision(shot, 1); !errors.Is(err, ErrShotRevisionInvalid) {
		t.Fatalf("duplicate dependency error = %v, want %v", err, ErrShotRevisionInvalid)
	}
}

func cinematicShotRevisionFixture(dependencies []ArtifactRevisionRef) CinematicShot {
	return CinematicShot{
		ShotKey: "shot-courtyard-1", NarrativePurpose: "交代人物决定离开", ShotSize: "中景",
		CameraPosition: "庭院门内", Angle: "平视", Composition: "人物位于画面左三分之一",
		ScreenDirection: "人物由左向右", CameraMotion: "缓慢前推", OnScreenAction: "人物收起雨伞并转身",
		Dialogue: []DialogueLine{{CharacterKey: "character-xiaoming", Text: "我们走吧。"}},
		Sound:    []SoundCue{{CueKey: "rain", Description: "持续雨声"}}, DurationMS: 5000, Transition: "cut",
		VisibleCharacterKeys: []string{"character-xiaoming"}, InputRevisions: dependencies,
		FramePlan: FirstMotionLastFrame{
			FirstFrame:     FrameState{State: "人物持伞站在门内", Static: true, EvidenceRevisions: dependencies, VisibleCharacterKeys: []string{"character-xiaoming"}},
			Motion:         "人物收伞、转身并迈出一步",
			LastFrame:      FrameState{State: "人物面向院外停住", Static: true, EvidenceRevisions: dependencies, VisibleCharacterKeys: []string{"character-xiaoming"}},
			InputRevisions: dependencies, ContinuityConditions: []string{"服装与伞保持一致"},
		},
	}
}
