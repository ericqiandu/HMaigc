package agentruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const maxArtifactPayloadBytes = 1024 * 1024

const (
	ArtifactSchemaScriptBundleV1            = "script_bundle.v1"
	ArtifactSchemaAssetBindingV1            = "asset_binding.v1"
	ArtifactSchemaMediaCandidateV1          = "media_candidate.v1"
	ArtifactSchemaVisualConsistencyReviewV1 = "visual_consistency_review.v1"
	ArtifactSchemaMediaCandidateSelectionV1 = "media_candidate_selection.v1"
	ArtifactSchemaVideoPlanV1               = "video_plan.v1"
	ArtifactSchemaAudioPlanV1               = "audio_plan.v1"
	ArtifactSchemaAssemblyPlanV1            = "assembly_plan.v1"
	ArtifactReviewContentType               = "artifact_review"
	StageReviewContentType                  = "stage_review_resolution"
	AssetPublicationContentType             = "asset_publication"
	AssetPublicationFailedType              = "asset_publication_failed"
)

var (
	ErrArtifactDraftInvalid    = errors.New("artifact draft is invalid")
	ErrArtifactRevisionInvalid = errors.New("artifact revision reference is invalid")
	ErrArtifactPayloadInvalid  = errors.New("artifact payload is invalid")
)

type ArtifactRevisionRef struct {
	ArtifactID string `json:"artifactId"`
	RevisionID string `json:"revisionId"`
}

type ArtifactDraft struct {
	ArtifactKey          string                `json:"artifactKey"`
	Kind                 string                `json:"kind"`
	SchemaVersion        int                   `json:"schemaVersion"`
	Payload              json.RawMessage       `json:"payload"`
	ResourceID           string                `json:"resourceId,omitempty"`
	UpstreamRevisions    []ArtifactRevisionRef `json:"upstreamRevisions"`
	ModelRequestIdentity string                `json:"modelRequestIdentity,omitempty"`
	SkillVersions        []SkillSelection      `json:"skillVersions"`
}

type CharacterNeed struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type SceneNeed struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type PropNeed struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type VoiceRoleNeed struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type ScriptBundle struct {
	Title      string          `json:"title"`
	Logline    string          `json:"logline"`
	Script     string          `json:"script"`
	Characters []CharacterNeed `json:"characters"`
	Scenes     []SceneNeed     `json:"scenes"`
	Props      []PropNeed      `json:"props"`
	VoiceRoles []VoiceRoleNeed `json:"voiceRoles"`
}

type AssetRequirementKind string

const (
	AssetRequirementCharacter AssetRequirementKind = "character"
	AssetRequirementScene     AssetRequirementKind = "scene"
	AssetRequirementProp      AssetRequirementKind = "prop"
	AssetRequirementVoiceRole AssetRequirementKind = "voice_role"
)

func (kind AssetRequirementKind) Valid() bool {
	switch kind {
	case AssetRequirementCharacter, AssetRequirementScene, AssetRequirementProp, AssetRequirementVoiceRole:
		return true
	default:
		return false
	}
}

type AssetBindingState string

const (
	AssetBindingMatched        AssetBindingState = "matched"
	AssetBindingMissing        AssetBindingState = "missing"
	AssetBindingConflict       AssetBindingState = "conflict"
	AssetBindingChoiceRequired AssetBindingState = "choice_required"
)

func (state AssetBindingState) Valid() bool {
	switch state {
	case AssetBindingMatched, AssetBindingMissing, AssetBindingConflict, AssetBindingChoiceRequired:
		return true
	default:
		return false
	}
}

type AssetBindingEntry struct {
	RequirementKey       string               `json:"requirementKey"`
	RequirementKind      AssetRequirementKind `json:"requirementKind"`
	State                AssetBindingState    `json:"state"`
	ResourceID           string               `json:"resourceId,omitempty"`
	CandidateResourceIDs []string             `json:"candidateResourceIds"`
}

type AssetBindingSet struct {
	BindingKey     string              `json:"bindingKey"`
	ScriptRevision ArtifactRevisionRef `json:"scriptRevision"`
	Confirmed      bool                `json:"confirmed"`
	Entries        []AssetBindingEntry `json:"entries"`
}

type ArtifactReviewContent struct {
	ContentType    string `json:"contentType"`
	StageID        string `json:"stageId"`
	StageVersion   int64  `json:"stageVersion"`
	ArtifactID     string `json:"artifactId"`
	RevisionID     string `json:"revisionId"`
	ArtifactSchema string `json:"artifactSchema"`
	Summary        string `json:"summary"`
}

type StageReviewResolutionContent struct {
	ContentType            string                  `json:"contentType"`
	StageID                string                  `json:"stageId"`
	StageVersion           int64                   `json:"stageVersion"`
	RevisionID             string                  `json:"revisionId"`
	Decision               StageReviewDecision     `json:"decision"`
	ClientRequestID        string                  `json:"clientRequestId"`
	PublicationIntent      *AssetPublicationIntent `json:"publicationIntent,omitempty"`
	ResultStageVersion     int64                   `json:"resultStageVersion"`
	ResultStatus           ProductionStageStatus   `json:"resultStatus"`
	ResultReviewRevisionID string                  `json:"resultReviewRevisionId,omitempty"`
	ResultUpdatedAt        time.Time               `json:"resultUpdatedAt"`
}

type MediaCandidateSelection struct {
	StageID                   string              `json:"stageId"`
	ReviewRevision            ArtifactRevisionRef `json:"reviewRevision"`
	SelectedCandidateRevision ArtifactRevisionRef `json:"selectedCandidateRevision"`
	ApprovedByUserID          string              `json:"approvedByUserId"`
	ClientRequestID           string              `json:"clientRequestId"`
}

func (selection MediaCandidateSelection) Validate() error {
	if !validArtifactText(selection.StageID, 80) || selection.ReviewRevision.Validate() != nil ||
		selection.SelectedCandidateRevision.Validate() != nil || selection.ReviewRevision == selection.SelectedCandidateRevision ||
		!validArtifactText(selection.ApprovedByUserID, 80) || !validArtifactText(selection.ClientRequestID, 120) {
		return ErrArtifactPayloadInvalid
	}
	return nil
}

func DecodeMediaCandidateSelection(payload []byte) (MediaCandidateSelection, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var selection MediaCandidateSelection
	if err := decoder.Decode(&selection); err != nil {
		return MediaCandidateSelection{}, ErrArtifactPayloadInvalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || selection.Validate() != nil {
		return MediaCandidateSelection{}, ErrArtifactPayloadInvalid
	}
	return selection, nil
}

// AssetPublicationIntent is persisted with the approval fact. It makes one
// user confirmation authorize both the exact revision and its exact library
// destination, so publication never requires a second confirmation.
type AssetPublicationIntent struct {
	PublicationPurpose string `json:"publicationPurpose"`
	TargetCategory     string `json:"targetCategory"`
	TargetBindingKey   string `json:"targetBindingKey"`
}

type AssetPublicationContent struct {
	ContentType        string `json:"contentType"`
	PublicationID      string `json:"publicationId"`
	ArtifactRevisionID string `json:"artifactRevisionId"`
	ResourceID         string `json:"resourceId"`
	AssetID            string `json:"assetId"`
	AssetVersionID     string `json:"assetVersionId"`
	ProjectAssetLinkID string `json:"projectAssetLinkId"`
	RepresentationID   string `json:"representationId"`
	PublicationPurpose string `json:"publicationPurpose"`
	TargetCategory     string `json:"targetCategory"`
	TargetBindingKey   string `json:"targetBindingKey"`
}

type AssetPublicationFailureContent struct {
	ContentType        string `json:"contentType"`
	PublicationID      string `json:"publicationId"`
	ArtifactRevisionID string `json:"artifactRevisionId"`
	ErrorCode          string `json:"errorCode"`
}

func (draft ArtifactDraft) SchemaID() string {
	return fmt.Sprintf("%s.v%d", draft.Kind, draft.SchemaVersion)
}

func (content ArtifactReviewContent) Validate() error {
	if content.ContentType != ArtifactReviewContentType || !validArtifactText(content.StageID, 80) || content.StageVersion < 1 ||
		!validArtifactText(content.ArtifactID, 80) || !validArtifactText(content.RevisionID, 80) ||
		!validArtifactText(content.ArtifactSchema, 120) || !validArtifactText(content.Summary, 16*1024) {
		return ErrArtifactPayloadInvalid
	}
	return nil
}

func DecodeArtifactReviewContent(payload []byte) (ArtifactReviewContent, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var content ArtifactReviewContent
	if err := decoder.Decode(&content); err != nil {
		return ArtifactReviewContent{}, ErrArtifactPayloadInvalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ArtifactReviewContent{}, ErrArtifactPayloadInvalid
	}
	if err := content.Validate(); err != nil {
		return ArtifactReviewContent{}, err
	}
	return content, nil
}

func (content StageReviewResolutionContent) Validate() error {
	if content.ContentType != StageReviewContentType || !validArtifactText(content.StageID, 80) ||
		content.StageVersion < 1 || !validArtifactText(content.RevisionID, 80) || !content.Decision.Valid() ||
		!validArtifactText(content.ClientRequestID, 120) || content.ResultStageVersion != content.StageVersion+1 ||
		!content.ResultStatus.Valid() || !stageReviewDecisionMatchesStatus(content.Decision, content.ResultStatus) ||
		!stageReviewDecisionMatchesRevision(content.Decision, content.RevisionID, content.ResultReviewRevisionID) ||
		content.ResultUpdatedAt.IsZero() || !validPublicationIntentForDecision(content.PublicationIntent, content.Decision) {
		return ErrArtifactPayloadInvalid
	}
	return nil
}

func (intent AssetPublicationIntent) Validate() error {
	if !validArtifactText(intent.PublicationPurpose, 120) || !validArtifactText(intent.TargetBindingKey, 120) ||
		!validAssetPublicationCategory(intent.TargetCategory) {
		return ErrArtifactPayloadInvalid
	}
	return nil
}

func validPublicationIntentForDecision(intent *AssetPublicationIntent, decision StageReviewDecision) bool {
	if intent == nil {
		return true
	}
	return decision == StageReviewApprove && intent.Validate() == nil
}

func validAssetPublicationCategory(category string) bool {
	switch category {
	case "character", "environment", "wardrobe", "prop", "weapon", "style", "other":
		return true
	default:
		return false
	}
}

func (content AssetPublicationContent) Validate() error {
	if content.ContentType != AssetPublicationContentType || !validArtifactText(content.PublicationID, 80) ||
		!validArtifactText(content.ArtifactRevisionID, 80) || !validArtifactText(content.ResourceID, 80) ||
		!validArtifactText(content.AssetID, 80) || !validArtifactText(content.AssetVersionID, 80) ||
		!validArtifactText(content.ProjectAssetLinkID, 80) || !validArtifactText(content.RepresentationID, 80) ||
		!validArtifactText(content.PublicationPurpose, 120) || !validAssetPublicationCategory(content.TargetCategory) ||
		!validArtifactText(content.TargetBindingKey, 120) {
		return ErrArtifactPayloadInvalid
	}
	return nil
}

func (content AssetPublicationFailureContent) Validate() error {
	if content.ContentType != AssetPublicationFailedType || !validArtifactText(content.PublicationID, 80) ||
		!validArtifactText(content.ArtifactRevisionID, 80) || !validArtifactText(content.ErrorCode, 80) {
		return ErrArtifactPayloadInvalid
	}
	return nil
}

func DecodeAssetPublicationContent(payload []byte) (AssetPublicationContent, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var content AssetPublicationContent
	if err := decoder.Decode(&content); err != nil {
		return AssetPublicationContent{}, ErrArtifactPayloadInvalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || content.Validate() != nil {
		return AssetPublicationContent{}, ErrArtifactPayloadInvalid
	}
	return content, nil
}

func DecodeAssetPublicationFailureContent(payload []byte) (AssetPublicationFailureContent, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var content AssetPublicationFailureContent
	if err := decoder.Decode(&content); err != nil {
		return AssetPublicationFailureContent{}, ErrArtifactPayloadInvalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || content.Validate() != nil {
		return AssetPublicationFailureContent{}, ErrArtifactPayloadInvalid
	}
	return content, nil
}

func stageReviewDecisionMatchesRevision(decision StageReviewDecision, revisionID string, resultRevisionID string) bool {
	if decision == StageReviewRequestRevision {
		return resultRevisionID == ""
	}
	return resultRevisionID == revisionID
}

func stageReviewDecisionMatchesStatus(decision StageReviewDecision, status ProductionStageStatus) bool {
	switch decision {
	case StageReviewApprove:
		return status == StageApproved
	case StageReviewRequestRevision:
		return status == StageRunning
	case StageReviewStop:
		return status == StageStopped
	default:
		return false
	}
}

func DecodeStageReviewResolutionContent(payload []byte) (StageReviewResolutionContent, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var content StageReviewResolutionContent
	if err := decoder.Decode(&content); err != nil {
		return StageReviewResolutionContent{}, ErrArtifactPayloadInvalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return StageReviewResolutionContent{}, ErrArtifactPayloadInvalid
	}
	if err := content.Validate(); err != nil {
		return StageReviewResolutionContent{}, err
	}
	return content, nil
}

func (reference ArtifactRevisionRef) Validate() error {
	if strings.TrimSpace(reference.ArtifactID) != reference.ArtifactID || reference.ArtifactID == "" || len(reference.ArtifactID) > 120 ||
		strings.TrimSpace(reference.RevisionID) != reference.RevisionID || reference.RevisionID == "" || len(reference.RevisionID) > 120 {
		return ErrArtifactRevisionInvalid
	}
	return nil
}

func ValidateArtifactDraft(draft ArtifactDraft) error {
	if strings.TrimSpace(draft.ArtifactKey) != draft.ArtifactKey || draft.ArtifactKey == "" || len(draft.ArtifactKey) > 120 ||
		strings.TrimSpace(draft.Kind) != draft.Kind || draft.Kind == "" || len(draft.Kind) > 120 || draft.SchemaVersion < 1 {
		return ErrArtifactDraftInvalid
	}
	payload := bytes.TrimSpace(draft.Payload)
	if len(payload) == 0 || len(payload) > maxArtifactPayloadBytes || !json.Valid(payload) || payload[0] != '{' {
		return ErrArtifactPayloadInvalid
	}
	if strings.TrimSpace(draft.ResourceID) != draft.ResourceID || len(draft.ResourceID) > 120 ||
		strings.TrimSpace(draft.ModelRequestIdentity) != draft.ModelRequestIdentity || len(draft.ModelRequestIdentity) > 180 {
		return ErrArtifactDraftInvalid
	}
	if err := validateArtifactRevisionRefs(draft.UpstreamRevisions); err != nil {
		return err
	}
	if err := validateKnownArtifactPayload(draft, payload); err != nil {
		return err
	}
	if len(draft.SkillVersions) > 0 {
		if err := ValidateRunConfiguration(RunConfiguration{ExecutionMode: ExecutionGuided, Skills: draft.SkillVersions}); err != nil {
			return fmt.Errorf("%w: skill versions: %v", ErrArtifactDraftInvalid, err)
		}
	}
	return nil
}

func validateKnownArtifactPayload(draft ArtifactDraft, payload []byte) error {
	switch draft.SchemaID() {
	case ArtifactSchemaScriptBundleV1:
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		var bundle ScriptBundle
		if err := decoder.Decode(&bundle); err != nil {
			return ErrArtifactPayloadInvalid
		}
		var trailing json.RawMessage
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return ErrArtifactPayloadInvalid
		}
		return validateScriptBundle(bundle)
	case ArtifactSchemaAssetBindingV1:
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		var bindings AssetBindingSet
		if err := decoder.Decode(&bindings); err != nil {
			return ErrArtifactPayloadInvalid
		}
		var trailing json.RawMessage
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return ErrArtifactPayloadInvalid
		}
		if err := validateAssetBindingSet(bindings); err != nil {
			return err
		}
		if len(draft.UpstreamRevisions) != 1 || draft.UpstreamRevisions[0] != bindings.ScriptRevision {
			return ErrArtifactPayloadInvalid
		}
	case ArtifactSchemaVisualEvidenceV1:
		evidence, err := DecodeVisualEvidence(payload)
		if err != nil {
			return err
		}
		if len(draft.UpstreamRevisions) != 1 || draft.UpstreamRevisions[0] != evidence.SourceRevision ||
			draft.ModelRequestIdentity != evidence.RequestIdentity {
			return ErrArtifactPayloadInvalid
		}
	case ArtifactSchemaCharacterVisualBibleV1:
		bible, err := DecodeCharacterVisualBible(payload)
		if err != nil {
			return err
		}
		if !artifactRevisionRefsEqual(draft.UpstreamRevisions, characterVisualBibleInputs(bible)) {
			return ErrArtifactPayloadInvalid
		}
	case ArtifactSchemaStoryboardPlanV1:
		plan, err := DecodeStoryboardPlan(payload)
		if err != nil {
			return err
		}
		if !artifactRevisionRefsEqual(draft.UpstreamRevisions, storyboardPlanInputs(plan)) {
			return ErrArtifactPayloadInvalid
		}
	case ArtifactSchemaCameraTreeV1:
		tree, err := DecodeCameraTree(payload)
		if err != nil {
			return err
		}
		if !artifactRevisionRefsEqual(draft.UpstreamRevisions, cameraTreeInputs(tree)) {
			return ErrArtifactPayloadInvalid
		}
	case ArtifactSchemaFirstMotionLastFrameV1:
		framePlan, err := DecodeFirstMotionLastFrame(payload)
		if err != nil {
			return err
		}
		if !artifactRevisionRefsEqual(draft.UpstreamRevisions, framePlan.InputRevisions) {
			return ErrArtifactPayloadInvalid
		}
	case ArtifactSchemaVideoPlanV1:
		plan, err := DecodeVideoPlan(payload)
		if err != nil {
			return err
		}
		if !artifactRevisionRefsEqual(draft.UpstreamRevisions, plan.InputRevisions) {
			return ErrArtifactPayloadInvalid
		}
	case ArtifactSchemaAudioPlanV1:
		plan, err := DecodeAudioPlan(payload)
		if err != nil {
			return err
		}
		if !artifactRevisionRefsEqual(draft.UpstreamRevisions, plan.InputRevisions) {
			return ErrArtifactPayloadInvalid
		}
	case ArtifactSchemaAssemblyPlanV1:
		plan, err := DecodeAssemblyPlan(payload)
		if err != nil {
			return err
		}
		if !artifactRevisionRefsEqual(draft.UpstreamRevisions, assemblyPlanInputs(plan)) {
			return ErrArtifactPayloadInvalid
		}
	case ArtifactSchemaMediaCandidateSelectionV1:
		selection, err := DecodeMediaCandidateSelection(payload)
		if err != nil {
			return err
		}
		want := []ArtifactRevisionRef{selection.ReviewRevision, selection.SelectedCandidateRevision}
		if !artifactRevisionRefsStartWith(draft.UpstreamRevisions, want) {
			return ErrArtifactPayloadInvalid
		}
	}
	return nil
}

func artifactRevisionRefsStartWith(references []ArtifactRevisionRef, prefix []ArtifactRevisionRef) bool {
	if len(references) < len(prefix) {
		return false
	}
	for index := range prefix {
		if references[index] != prefix[index] {
			return false
		}
	}
	return true
}

func artifactRevisionRefsEqual(left []ArtifactRevisionRef, right []ArtifactRevisionRef) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func artifactRevisionRefSet(references []ArtifactRevisionRef) map[ArtifactRevisionRef]struct{} {
	result := make(map[ArtifactRevisionRef]struct{}, len(references))
	for _, reference := range references {
		result[reference] = struct{}{}
	}
	return result
}

func artifactRevisionRefsBelongTo(references []ArtifactRevisionRef, allowed map[ArtifactRevisionRef]struct{}) bool {
	if err := validateArtifactRevisionRefs(references); err != nil {
		return false
	}
	for _, reference := range references {
		if _, exists := allowed[reference]; !exists {
			return false
		}
	}
	return true
}

func validateScriptBundle(bundle ScriptBundle) error {
	if !validArtifactText(bundle.Title, 240) || !validArtifactText(bundle.Logline, 2*1024) ||
		!validArtifactText(bundle.Script, maxArtifactPayloadBytes) || len(bundle.Characters) == 0 ||
		bundle.Scenes == nil || bundle.Props == nil || bundle.VoiceRoles == nil {
		return ErrArtifactPayloadInvalid
	}
	keys := make(map[string]struct{}, len(bundle.Characters)+len(bundle.Scenes)+len(bundle.Props)+len(bundle.VoiceRoles))
	for _, item := range bundle.Characters {
		if err := validateNarrativeNeed(item.Key, item.Name, item.Description, keys); err != nil {
			return err
		}
	}
	for _, item := range bundle.Scenes {
		if err := validateNarrativeNeed(item.Key, item.Name, item.Description, keys); err != nil {
			return err
		}
	}
	for _, item := range bundle.Props {
		if err := validateNarrativeNeed(item.Key, item.Name, item.Description, keys); err != nil {
			return err
		}
	}
	for _, item := range bundle.VoiceRoles {
		if err := validateNarrativeNeed(item.Key, item.Name, item.Description, keys); err != nil {
			return err
		}
	}
	return nil
}

func validateNarrativeNeed(key string, name string, description string, keys map[string]struct{}) error {
	if !validArtifactText(key, 120) || !validArtifactText(name, 240) || !validArtifactText(description, 4*1024) {
		return ErrArtifactPayloadInvalid
	}
	if _, exists := keys[key]; exists {
		return ErrArtifactPayloadInvalid
	}
	keys[key] = struct{}{}
	return nil
}

func validateAssetBindingSet(bindings AssetBindingSet) error {
	if !validArtifactText(bindings.BindingKey, 120) || bindings.ScriptRevision.Validate() != nil || len(bindings.Entries) == 0 {
		return ErrArtifactPayloadInvalid
	}
	requirements := make(map[string]struct{}, len(bindings.Entries))
	allMatched := true
	for _, entry := range bindings.Entries {
		if !validArtifactText(entry.RequirementKey, 120) || !entry.RequirementKind.Valid() || !entry.State.Valid() ||
			strings.TrimSpace(entry.ResourceID) != entry.ResourceID || len(entry.ResourceID) > 120 || entry.CandidateResourceIDs == nil {
			return ErrArtifactPayloadInvalid
		}
		if _, exists := requirements[entry.RequirementKey]; exists {
			return ErrArtifactPayloadInvalid
		}
		requirements[entry.RequirementKey] = struct{}{}
		if !validResourceCandidates(entry.CandidateResourceIDs) {
			return ErrArtifactPayloadInvalid
		}
		switch entry.State {
		case AssetBindingMatched:
			if entry.ResourceID == "" || len(entry.CandidateResourceIDs) != 0 {
				return ErrArtifactPayloadInvalid
			}
		case AssetBindingMissing:
			allMatched = false
			if entry.ResourceID != "" || len(entry.CandidateResourceIDs) != 0 {
				return ErrArtifactPayloadInvalid
			}
		case AssetBindingConflict:
			allMatched = false
			if entry.ResourceID != "" || len(entry.CandidateResourceIDs) < 2 {
				return ErrArtifactPayloadInvalid
			}
		case AssetBindingChoiceRequired:
			allMatched = false
			if entry.ResourceID != "" || len(entry.CandidateResourceIDs) < 1 {
				return ErrArtifactPayloadInvalid
			}
		}
	}
	if bindings.Confirmed && !allMatched {
		return ErrArtifactPayloadInvalid
	}
	return nil
}

func validResourceCandidates(resourceIDs []string) bool {
	seen := make(map[string]struct{}, len(resourceIDs))
	for _, resourceID := range resourceIDs {
		if !validArtifactText(resourceID, 120) {
			return false
		}
		if _, exists := seen[resourceID]; exists {
			return false
		}
		seen[resourceID] = struct{}{}
	}
	return true
}

func validArtifactText(value string, maximum int) bool {
	return strings.TrimSpace(value) == value && value != "" && len(value) <= maximum
}

func validateArtifactRevisionRefs(references []ArtifactRevisionRef) error {
	seenArtifacts := make(map[string]struct{}, len(references))
	for _, reference := range references {
		if err := reference.Validate(); err != nil {
			return err
		}
		if _, duplicated := seenArtifacts[reference.ArtifactID]; duplicated {
			return fmt.Errorf("%w: artifact %q is duplicated", ErrArtifactRevisionInvalid, reference.ArtifactID)
		}
		seenArtifacts[reference.ArtifactID] = struct{}{}
	}
	return nil
}
