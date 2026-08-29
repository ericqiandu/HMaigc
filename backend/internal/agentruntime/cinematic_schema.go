package agentruntime

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

const (
	ArtifactSchemaStoryboardPlanV1       = "storyboard_plan.v1"
	ArtifactSchemaCameraTreeV1           = "camera_tree.v1"
	ArtifactSchemaFirstMotionLastFrameV1 = "first_motion_last_frame.v1"
	maxCinematicShotDurationMS           = 30 * 60 * 1000
	maxStoryboardDurationMS              = 12 * 60 * 60 * 1000
)

var (
	ErrCameraTreeCycle         = errors.New("camera tree contains a cycle")
	ErrCameraTreeSelfReference = errors.New("camera tree contains a self reference")
	ErrFrameBoundaryInvalid    = errors.New("first or last frame boundary is invalid")
	ErrShotRevisionInvalid     = errors.New("cinematic shot revision is invalid")
)

type DialogueLine struct {
	CharacterKey string `json:"characterKey"`
	Text         string `json:"text"`
}

type SoundCue struct {
	CueKey      string `json:"cueKey"`
	Description string `json:"description"`
}

type FrameState struct {
	State                string                `json:"state"`
	Static               bool                  `json:"static"`
	EvidenceRevisions    []ArtifactRevisionRef `json:"evidenceRevisions,omitempty"`
	VisibleCharacterKeys []string              `json:"visibleCharacterKeys,omitempty"`
}

type FirstMotionLastFrame struct {
	FirstFrame           FrameState            `json:"firstFrame"`
	Motion               string                `json:"motion"`
	LastFrame            FrameState            `json:"lastFrame"`
	InputRevisions       []ArtifactRevisionRef `json:"inputRevisions,omitempty"`
	ContinuityConditions []string              `json:"continuityConditions,omitempty"`
}

type CinematicShot struct {
	ShotKey              string                `json:"shotKey"`
	NarrativePurpose     string                `json:"narrativePurpose"`
	ShotSize             string                `json:"shotSize"`
	CameraPosition       string                `json:"cameraPosition"`
	Angle                string                `json:"angle"`
	Composition          string                `json:"composition"`
	ScreenDirection      string                `json:"screenDirection"`
	CameraMotion         string                `json:"cameraMotion"`
	OnScreenAction       string                `json:"onScreenAction"`
	Dialogue             []DialogueLine        `json:"dialogue"`
	Sound                []SoundCue            `json:"sound"`
	DurationMS           int                   `json:"durationMs"`
	Transition           string                `json:"transition"`
	VisibleCharacterKeys []string              `json:"visibleCharacterKeys"`
	InputRevisions       []ArtifactRevisionRef `json:"inputRevisions"`
	FramePlan            FirstMotionLastFrame  `json:"framePlan"`
}

// CinematicShotRevision freezes one shot payload together with the exact
// upstream Artifact revisions that produced it. Revision numbers only append
// for the stable ShotKey; DependencyHash is derived from those exact refs.
type CinematicShotRevision struct {
	Revision       int64         `json:"revision"`
	Shot           CinematicShot `json:"shot"`
	DependencyHash string        `json:"dependencyHash"`
}

type StoryboardPlan struct {
	ScriptRevision          ArtifactRevisionRef   `json:"scriptRevision"`
	AssetBindingRevision    ArtifactRevisionRef   `json:"assetBindingRevision"`
	CharacterBibleRevision  ArtifactRevisionRef   `json:"characterBibleRevision"`
	VisualEvidenceRevisions []ArtifactRevisionRef `json:"visualEvidenceRevisions"`
	TargetDurationMS        int                   `json:"targetDurationMs"`
	Shots                   []CinematicShot       `json:"shots"`
}

type CameraNode struct {
	CameraKey       string   `json:"cameraKey"`
	ParentCameraKey string   `json:"parentCameraKey,omitempty"`
	Independent     bool     `json:"independent"`
	ShotKeys        []string `json:"shotKeys"`
	SubjectKeys     []string `json:"subjectKeys"`
	ShotSize        string   `json:"shotSize"`
	Angle           string   `json:"angle"`
	ScreenDirection string   `json:"screenDirection"`
	Purpose         string   `json:"purpose"`
}

type CameraRelation struct {
	RelationKey   string                `json:"relationKey"`
	FromCameraKey string                `json:"fromCameraKey"`
	ToCameraKey   string                `json:"toCameraKey"`
	Relation      string                `json:"relation"`
	EvidenceRefs  []ArtifactRevisionRef `json:"evidenceRefs"`
}

type CameraCoverageGap struct {
	GapKey            string   `json:"gapKey"`
	Description       string   `json:"description"`
	RelatedCameraKeys []string `json:"relatedCameraKeys"`
}

type CameraTree struct {
	StoryboardRevision      ArtifactRevisionRef   `json:"storyboardRevision"`
	VisualEvidenceRevisions []ArtifactRevisionRef `json:"visualEvidenceRevisions"`
	ShotKeys                []string              `json:"shotKeys"`
	Cameras                 []CameraNode          `json:"cameras"`
	Relations               []CameraRelation      `json:"relations"`
	MissingViews            []CameraCoverageGap   `json:"missingViews"`
}

func DecodeStoryboardPlan(payload []byte) (StoryboardPlan, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var plan StoryboardPlan
	if err := decoder.Decode(&plan); err != nil {
		return StoryboardPlan{}, ErrArtifactPayloadInvalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return StoryboardPlan{}, ErrArtifactPayloadInvalid
	}
	if err := ValidateStoryboardPlan(plan); err != nil {
		return StoryboardPlan{}, err
	}
	return plan, nil
}

func ValidateStoryboardPlan(plan StoryboardPlan) error {
	inputs := storyboardPlanInputs(plan)
	if plan.ScriptRevision.Validate() != nil || plan.AssetBindingRevision.Validate() != nil ||
		plan.CharacterBibleRevision.Validate() != nil || plan.VisualEvidenceRevisions == nil ||
		plan.TargetDurationMS <= 0 || plan.TargetDurationMS > maxStoryboardDurationMS || len(plan.Shots) == 0 ||
		validateArtifactRevisionRefs(inputs) != nil {
		return ErrArtifactPayloadInvalid
	}
	allowedInputs := artifactRevisionRefSet(inputs)
	shotKeys := make(map[string]struct{}, len(plan.Shots))
	totalDurationMS := 0
	for _, shot := range plan.Shots {
		if err := ValidateCinematicShot(shot); err != nil {
			return err
		}
		if _, exists := shotKeys[shot.ShotKey]; exists || !artifactRevisionRefsBelongTo(shot.InputRevisions, allowedInputs) {
			return ErrArtifactPayloadInvalid
		}
		shotKeys[shot.ShotKey] = struct{}{}
		if totalDurationMS > maxStoryboardDurationMS-shot.DurationMS {
			return ErrArtifactPayloadInvalid
		}
		totalDurationMS += shot.DurationMS
	}
	if totalDurationMS != plan.TargetDurationMS {
		return ErrArtifactPayloadInvalid
	}
	return nil
}

func ValidateCinematicShot(shot CinematicShot) error {
	for _, value := range []string{
		shot.ShotKey, shot.NarrativePurpose, shot.ShotSize, shot.CameraPosition, shot.Angle,
		shot.Composition, shot.ScreenDirection, shot.CameraMotion, shot.OnScreenAction, shot.Transition,
	} {
		if !validArtifactText(value, 4*1024) {
			return ErrArtifactPayloadInvalid
		}
	}
	if shot.DurationMS <= 0 || shot.DurationMS > maxCinematicShotDurationMS || shot.Dialogue == nil || shot.Sound == nil ||
		shot.VisibleCharacterKeys == nil || len(shot.InputRevisions) == 0 {
		return ErrArtifactPayloadInvalid
	}
	if err := validateArtifactRevisionRefs(shot.InputRevisions); err != nil {
		return err
	}
	visibleCharacters := make(map[string]struct{}, len(shot.VisibleCharacterKeys))
	for _, characterKey := range shot.VisibleCharacterKeys {
		if !validArtifactText(characterKey, 120) {
			return ErrArtifactPayloadInvalid
		}
		if _, exists := visibleCharacters[characterKey]; exists {
			return ErrArtifactPayloadInvalid
		}
		visibleCharacters[characterKey] = struct{}{}
	}
	for _, line := range shot.Dialogue {
		if !validArtifactText(line.CharacterKey, 120) || !validArtifactText(line.Text, 8*1024) {
			return ErrArtifactPayloadInvalid
		}
		if _, exists := visibleCharacters[line.CharacterKey]; !exists {
			return ErrArtifactPayloadInvalid
		}
	}
	soundKeys := make(map[string]struct{}, len(shot.Sound))
	for _, cue := range shot.Sound {
		if !validArtifactText(cue.CueKey, 120) || !validArtifactText(cue.Description, 4*1024) {
			return ErrArtifactPayloadInvalid
		}
		if _, exists := soundKeys[cue.CueKey]; exists {
			return ErrArtifactPayloadInvalid
		}
		soundKeys[cue.CueKey] = struct{}{}
	}
	if err := ValidateFirstMotionLastFrame(shot.FramePlan); err != nil {
		return err
	}
	shotInputs := artifactRevisionRefSet(shot.InputRevisions)
	if !artifactRevisionRefsBelongTo(shot.FramePlan.InputRevisions, shotInputs) {
		return ErrArtifactPayloadInvalid
	}
	for _, characterKeys := range [][]string{
		shot.FramePlan.FirstFrame.VisibleCharacterKeys,
		shot.FramePlan.LastFrame.VisibleCharacterKeys,
	} {
		for _, characterKey := range characterKeys {
			if _, exists := visibleCharacters[characterKey]; !exists {
				return ErrArtifactPayloadInvalid
			}
		}
	}
	return nil
}

func NewCinematicShotRevision(shot CinematicShot, revision int64) (CinematicShotRevision, error) {
	shot = cloneCinematicShot(shot)
	hash, err := CanonicalDependencyHash(shot.InputRevisions)
	if err != nil {
		return CinematicShotRevision{}, fmt.Errorf("%w: %v", ErrShotRevisionInvalid, err)
	}
	result := CinematicShotRevision{Revision: revision, Shot: shot, DependencyHash: hash}
	if err := ValidateCinematicShotRevision(result); err != nil {
		return CinematicShotRevision{}, err
	}
	return result, nil
}

func ValidateCinematicShotRevision(revision CinematicShotRevision) error {
	if revision.Revision < 1 || ValidateCinematicShot(revision.Shot) != nil || !validDependencyHash(revision.DependencyHash) {
		return ErrShotRevisionInvalid
	}
	want, err := CanonicalDependencyHash(revision.Shot.InputRevisions)
	if err != nil || revision.DependencyHash != want {
		return ErrShotRevisionInvalid
	}
	return nil
}

// CanonicalDependencyHash is order-independent while remaining sensitive to
// exact Artifact and revision identities. Duplicate Artifact dependencies are
// rejected instead of being silently normalized.
func CanonicalDependencyHash(references []ArtifactRevisionRef) (string, error) {
	if err := validateArtifactRevisionRefs(references); err != nil {
		return "", err
	}
	canonical := append([]ArtifactRevisionRef(nil), references...)
	sort.Slice(canonical, func(left, right int) bool {
		if canonical[left].ArtifactID == canonical[right].ArtifactID {
			return canonical[left].RevisionID < canonical[right].RevisionID
		}
		return canonical[left].ArtifactID < canonical[right].ArtifactID
	})
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("encode dependency references: %w", err)
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func cloneCinematicShot(shot CinematicShot) CinematicShot {
	shot.Dialogue = append([]DialogueLine(nil), shot.Dialogue...)
	shot.Sound = append([]SoundCue(nil), shot.Sound...)
	shot.VisibleCharacterKeys = append([]string(nil), shot.VisibleCharacterKeys...)
	shot.InputRevisions = append([]ArtifactRevisionRef(nil), shot.InputRevisions...)
	shot.FramePlan.FirstFrame.EvidenceRevisions = append([]ArtifactRevisionRef(nil), shot.FramePlan.FirstFrame.EvidenceRevisions...)
	shot.FramePlan.FirstFrame.VisibleCharacterKeys = append([]string(nil), shot.FramePlan.FirstFrame.VisibleCharacterKeys...)
	shot.FramePlan.LastFrame.EvidenceRevisions = append([]ArtifactRevisionRef(nil), shot.FramePlan.LastFrame.EvidenceRevisions...)
	shot.FramePlan.LastFrame.VisibleCharacterKeys = append([]string(nil), shot.FramePlan.LastFrame.VisibleCharacterKeys...)
	shot.FramePlan.InputRevisions = append([]ArtifactRevisionRef(nil), shot.FramePlan.InputRevisions...)
	shot.FramePlan.ContinuityConditions = append([]string(nil), shot.FramePlan.ContinuityConditions...)
	return shot
}

func DecodeFirstMotionLastFrame(payload []byte) (FirstMotionLastFrame, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var value FirstMotionLastFrame
	if err := decoder.Decode(&value); err != nil {
		return FirstMotionLastFrame{}, ErrArtifactPayloadInvalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return FirstMotionLastFrame{}, ErrArtifactPayloadInvalid
	}
	if err := ValidateFirstMotionLastFrame(value); err != nil {
		return FirstMotionLastFrame{}, err
	}
	return value, nil
}

func ValidateFirstMotionLastFrame(value FirstMotionLastFrame) error {
	if !value.FirstFrame.Static || !value.LastFrame.Static || !validArtifactText(value.FirstFrame.State, 8*1024) ||
		!validArtifactText(value.LastFrame.State, 8*1024) {
		return ErrFrameBoundaryInvalid
	}
	if !validArtifactText(value.Motion, 8*1024) || !validVisualTextList(value.FirstFrame.VisibleCharacterKeys, 120) ||
		!validVisualTextList(value.LastFrame.VisibleCharacterKeys, 120) || !validVisualTextList(value.ContinuityConditions, 4*1024) {
		return ErrArtifactPayloadInvalid
	}
	for _, references := range [][]ArtifactRevisionRef{
		value.FirstFrame.EvidenceRevisions, value.LastFrame.EvidenceRevisions, value.InputRevisions,
	} {
		if err := validateArtifactRevisionRefs(references); err != nil {
			return err
		}
	}
	allowedInputs := artifactRevisionRefSet(value.InputRevisions)
	if !artifactRevisionRefsBelongTo(value.FirstFrame.EvidenceRevisions, allowedInputs) ||
		!artifactRevisionRefsBelongTo(value.LastFrame.EvidenceRevisions, allowedInputs) {
		return ErrArtifactPayloadInvalid
	}
	return nil
}

func DecodeCameraTree(payload []byte) (CameraTree, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var tree CameraTree
	if err := decoder.Decode(&tree); err != nil {
		return CameraTree{}, ErrArtifactPayloadInvalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return CameraTree{}, ErrArtifactPayloadInvalid
	}
	if err := ValidateCameraTree(tree); err != nil {
		return CameraTree{}, err
	}
	return tree, nil
}

func ValidateCameraTree(tree CameraTree) error {
	inputs := cameraTreeInputs(tree)
	if tree.StoryboardRevision.Validate() != nil || tree.VisualEvidenceRevisions == nil || len(tree.ShotKeys) == 0 ||
		len(tree.Cameras) == 0 || tree.Relations == nil || tree.MissingViews == nil || validateArtifactRevisionRefs(inputs) != nil {
		return ErrArtifactPayloadInvalid
	}
	knownShots := make(map[string]struct{}, len(tree.ShotKeys))
	for _, shotKey := range tree.ShotKeys {
		if !validArtifactText(shotKey, 120) {
			return ErrArtifactPayloadInvalid
		}
		if _, exists := knownShots[shotKey]; exists {
			return ErrArtifactPayloadInvalid
		}
		knownShots[shotKey] = struct{}{}
	}
	cameras := make(map[string]CameraNode, len(tree.Cameras))
	for _, camera := range tree.Cameras {
		if !validArtifactText(camera.CameraKey, 120) || !validOptionalArtifactText(camera.ParentCameraKey, 120) ||
			len(camera.ShotKeys) == 0 || len(camera.SubjectKeys) == 0 || !validVisualTextList(camera.SubjectKeys, 120) ||
			!validArtifactText(camera.ShotSize, 240) || !validArtifactText(camera.Angle, 240) ||
			!validArtifactText(camera.ScreenDirection, 4*1024) || !validArtifactText(camera.Purpose, 4*1024) {
			return ErrArtifactPayloadInvalid
		}
		if camera.ParentCameraKey == camera.CameraKey {
			return ErrCameraTreeSelfReference
		}
		if _, exists := cameras[camera.CameraKey]; exists {
			return ErrArtifactPayloadInvalid
		}
		cameras[camera.CameraKey] = camera
	}
	assignedShots := make(map[string]struct{}, len(tree.ShotKeys))
	rootCount := 0
	for _, camera := range tree.Cameras {
		if camera.ParentCameraKey == "" {
			rootCount++
		} else {
			if camera.Independent {
				return ErrArtifactPayloadInvalid
			}
			if _, exists := cameras[camera.ParentCameraKey]; !exists {
				return ErrArtifactPayloadInvalid
			}
		}
		for _, shotKey := range camera.ShotKeys {
			if _, exists := knownShots[shotKey]; !exists {
				return ErrArtifactPayloadInvalid
			}
			if _, exists := assignedShots[shotKey]; exists {
				return ErrArtifactPayloadInvalid
			}
			assignedShots[shotKey] = struct{}{}
		}
	}
	if rootCount == 0 || len(assignedShots) != len(knownShots) {
		if cameraTreeContainsCycle(tree.Cameras) {
			return ErrCameraTreeCycle
		}
		return ErrArtifactPayloadInvalid
	}
	if rootCount > 1 {
		for _, camera := range tree.Cameras {
			if camera.ParentCameraKey == "" && !camera.Independent {
				return ErrArtifactPayloadInvalid
			}
		}
	}
	if cameraTreeContainsCycle(tree.Cameras) {
		return ErrCameraTreeCycle
	}
	allowedEvidence := artifactRevisionRefSet(tree.VisualEvidenceRevisions)
	relationKeys := make(map[string]struct{}, len(tree.Relations))
	for _, relation := range tree.Relations {
		if !validArtifactText(relation.RelationKey, 120) || !validArtifactText(relation.Relation, 240) ||
			relation.FromCameraKey == relation.ToCameraKey || !artifactRevisionRefsBelongTo(relation.EvidenceRefs, allowedEvidence) {
			return ErrArtifactPayloadInvalid
		}
		if _, exists := cameras[relation.FromCameraKey]; !exists {
			return ErrArtifactPayloadInvalid
		}
		if _, exists := cameras[relation.ToCameraKey]; !exists {
			return ErrArtifactPayloadInvalid
		}
		if _, exists := relationKeys[relation.RelationKey]; exists {
			return ErrArtifactPayloadInvalid
		}
		relationKeys[relation.RelationKey] = struct{}{}
	}
	gapKeys := make(map[string]struct{}, len(tree.MissingViews))
	for _, gap := range tree.MissingViews {
		if !validArtifactText(gap.GapKey, 120) || !validArtifactText(gap.Description, 4*1024) || gap.RelatedCameraKeys == nil {
			return ErrArtifactPayloadInvalid
		}
		if _, exists := gapKeys[gap.GapKey]; exists {
			return ErrArtifactPayloadInvalid
		}
		gapKeys[gap.GapKey] = struct{}{}
		for _, cameraKey := range gap.RelatedCameraKeys {
			if _, exists := cameras[cameraKey]; !exists {
				return ErrArtifactPayloadInvalid
			}
		}
	}
	return nil
}

func cameraTreeContainsCycle(cameras []CameraNode) bool {
	parents := make(map[string]string, len(cameras))
	for _, camera := range cameras {
		parents[camera.CameraKey] = camera.ParentCameraKey
	}
	const (
		visiting = iota + 1
		visited
	)
	states := make(map[string]int, len(cameras))
	var visit func(string) bool
	visit = func(cameraKey string) bool {
		switch states[cameraKey] {
		case visiting:
			return true
		case visited:
			return false
		}
		states[cameraKey] = visiting
		if parent := parents[cameraKey]; parent != "" && visit(parent) {
			return true
		}
		states[cameraKey] = visited
		return false
	}
	for cameraKey := range parents {
		if visit(cameraKey) {
			return true
		}
	}
	return false
}

func storyboardPlanInputs(plan StoryboardPlan) []ArtifactRevisionRef {
	inputs := []ArtifactRevisionRef{plan.ScriptRevision, plan.AssetBindingRevision, plan.CharacterBibleRevision}
	return append(inputs, plan.VisualEvidenceRevisions...)
}

func cameraTreeInputs(tree CameraTree) []ArtifactRevisionRef {
	inputs := []ArtifactRevisionRef{tree.StoryboardRevision}
	return append(inputs, tree.VisualEvidenceRevisions...)
}

func validOptionalArtifactText(value string, maximum int) bool {
	return value == "" || validArtifactText(value, maximum)
}
