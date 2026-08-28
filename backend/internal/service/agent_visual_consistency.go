package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

const (
	mediaCandidateArtifactKind       = "media_candidate"
	visualConsistencyReviewKind      = "visual_consistency_review"
	mediaCandidateSelectionKind      = "media_candidate_selection"
	maximumVisualCandidates          = 32
	maximumConsistencyDescriptionLen = 8 * 1024
)

var (
	ErrVisualCandidateReviewInvalid     = errors.New("visual candidate review is invalid")
	ErrVisualCandidateSelectionInvalid  = errors.New("visual candidate selection is invalid")
	ErrVisualCandidateSelectionRequired = errors.New("visual candidate selection is required")
)

type VisualConsistencyDimension string

const (
	VisualConsistencyIdentity        VisualConsistencyDimension = "identity"
	VisualConsistencyClothing        VisualConsistencyDimension = "clothing"
	VisualConsistencyScene           VisualConsistencyDimension = "scene"
	VisualConsistencyTimeSpace       VisualConsistencyDimension = "time_space"
	VisualConsistencyComposition     VisualConsistencyDimension = "composition"
	VisualConsistencyScreenDirection VisualConsistencyDimension = "screen_direction"
	VisualConsistencyFrameContinuity VisualConsistencyDimension = "frame_continuity"
)

func allVisualConsistencyDimensions() []VisualConsistencyDimension {
	return []VisualConsistencyDimension{
		VisualConsistencyIdentity,
		VisualConsistencyClothing,
		VisualConsistencyScene,
		VisualConsistencyTimeSpace,
		VisualConsistencyComposition,
		VisualConsistencyScreenDirection,
		VisualConsistencyFrameContinuity,
	}
}

type VisualConsistencyOutcome string

const (
	VisualConsistencyMatched   VisualConsistencyOutcome = "matched"
	VisualConsistencyDeviation VisualConsistencyOutcome = "deviation"
	VisualConsistencyUncertain VisualConsistencyOutcome = "uncertain"
)

type GeneratedMediaCandidate struct {
	ArtifactID              string
	ArtifactKey             string
	MediaKind               string
	ResourceID              string
	SourceTaskID            string
	ProviderRequestIdentity string
	UpstreamRevisions       []agentruntime.ArtifactRevisionRef
	SkillVersions           []agentruntime.SkillSelection
}

type VisualConsistencyFinding struct {
	Dimension             VisualConsistencyDimension         `json:"dimension"`
	Outcome               VisualConsistencyOutcome           `json:"outcome"`
	Description           string                             `json:"description"`
	EvidenceRevisions     []agentruntime.ArtifactRevisionRef `json:"evidenceRevisions"`
	ConfidenceBasisPoints int                                `json:"confidenceBasisPoints"`
}

type VisualCandidateAssessment struct {
	CandidateArtifactID    string                           `json:"candidateArtifactId"`
	VisualEvidenceRevision agentruntime.ArtifactRevisionRef `json:"visualEvidenceRevision"`
	Findings               []VisualConsistencyFinding       `json:"findings"`
}

type VisualCandidateReviewCommand struct {
	ReviewArtifactID            string
	ReviewArtifactKey           string
	ReviewModelRecordID         string
	ReviewRequestIdentity       string
	ReviewRunID                 string
	ConfirmedReferenceRevisions []agentruntime.ArtifactRevisionRef
	Candidates                  []GeneratedMediaCandidate
	Assessments                 []VisualCandidateAssessment
	RankedCandidateArtifactIDs  []string
	Uncertainties               []string
	Conflicts                   []string
	RetrySuggestions            []string
	SkillVersions               []agentruntime.SkillSelection
}

type VisualCandidateReviewResult struct {
	ReviewRevision             model.AgentArtifactRevision
	CandidateRevisions         []model.AgentArtifactRevision
	RankedCandidateRevisionIDs []string
}

type storedVisualCandidateAssessment struct {
	CandidateRevision      agentruntime.ArtifactRevisionRef `json:"candidateRevision"`
	VisualEvidenceRevision agentruntime.ArtifactRevisionRef `json:"visualEvidenceRevision"`
	Findings               []VisualConsistencyFinding       `json:"findings"`
}

type visualConsistencyReviewPayload struct {
	ReviewRunID                 string                             `json:"reviewRunId"`
	ReviewModelRecordID         string                             `json:"reviewModelRecordId"`
	ReviewRequestIdentity       string                             `json:"reviewRequestIdentity"`
	CandidateRevisions          []agentruntime.ArtifactRevisionRef `json:"candidateRevisions"`
	ConfirmedReferenceRevisions []agentruntime.ArtifactRevisionRef `json:"confirmedReferenceRevisions"`
	Assessments                 []storedVisualCandidateAssessment  `json:"assessments"`
	RankedCandidateRevisionIDs  []string                           `json:"rankedCandidateRevisionIds"`
	Uncertainties               []string                           `json:"uncertainties"`
	Conflicts                   []string                           `json:"conflicts"`
	RetrySuggestions            []string                           `json:"retrySuggestions"`
}

type mediaCandidateSelectionPayload = agentruntime.MediaCandidateSelection

func (s *Service) ReviewVisualCandidates(
	ctx context.Context,
	scope agentruntime.Scope,
	command VisualCandidateReviewCommand,
) (VisualCandidateReviewResult, error) {
	if ctx == nil || scope.Validate() != nil || !validCandidateReviewIdentity(command) {
		return VisualCandidateReviewResult{}, ErrVisualCandidateReviewInvalid
	}
	select {
	case <-ctx.Done():
		return VisualCandidateReviewResult{}, ctx.Err()
	default:
	}

	candidateRevisions := make([]model.AgentArtifactRevision, 0, len(command.Candidates))
	candidateByArtifactID := make(map[string]model.AgentArtifactRevision, len(command.Candidates))
	for _, candidate := range command.Candidates {
		draft, err := mediaCandidateDraft(candidate)
		if err != nil {
			return VisualCandidateReviewResult{}, err
		}
		revision, err := s.repo.AppendMediaCandidateRevision(scope, candidate.ArtifactID, draft)
		if err != nil {
			return VisualCandidateReviewResult{}, err
		}
		candidateRevisions = append(candidateRevisions, *revision)
		candidateByArtifactID[candidate.ArtifactID] = *revision
	}

	payload, upstream, rankedRevisionIDs, err := s.validateVisualCandidateReview(scope, command, candidateByArtifactID)
	if err != nil {
		return VisualCandidateReviewResult{}, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return VisualCandidateReviewResult{}, err
	}
	review, err := s.repo.AppendArtifactRevisionOnce(scope, command.ReviewArtifactID, agentruntime.ArtifactDraft{
		ArtifactKey: command.ReviewArtifactKey, Kind: visualConsistencyReviewKind, SchemaVersion: 1,
		Payload: encoded, UpstreamRevisions: upstream, ModelRequestIdentity: command.ReviewRequestIdentity,
		SkillVersions: command.SkillVersions,
	})
	if err != nil {
		return VisualCandidateReviewResult{}, err
	}
	return VisualCandidateReviewResult{
		ReviewRevision: *review, CandidateRevisions: candidateRevisions,
		RankedCandidateRevisionIDs: rankedRevisionIDs,
	}, nil
}

func mediaCandidateDraft(candidate GeneratedMediaCandidate) (agentruntime.ArtifactDraft, error) {
	if !validStoredText(candidate.ArtifactID, 80) || !validStoredText(candidate.ArtifactKey, 120) ||
		!validStoredText(candidate.ResourceID, 80) || !validStoredText(candidate.SourceTaskID, 80) ||
		!validStoredText(candidate.ProviderRequestIdentity, 180) ||
		(candidate.MediaKind != "image" && candidate.MediaKind != "video" && candidate.MediaKind != "audio") ||
		candidate.UpstreamRevisions == nil || candidate.SkillVersions == nil {
		return agentruntime.ArtifactDraft{}, ErrVisualCandidateReviewInvalid
	}
	payload, err := json.Marshal(agentruntime.MediaCandidateContent{
		CandidateKey: candidate.ArtifactKey, MediaKind: agentruntime.ArtifactKind(candidate.MediaKind),
		ProviderRequestIdentity: candidate.ProviderRequestIdentity, ResourceID: candidate.ResourceID,
		SourceTaskID: candidate.SourceTaskID,
	})
	if err != nil {
		return agentruntime.ArtifactDraft{}, err
	}
	return agentruntime.ArtifactDraft{
		ArtifactKey: candidate.ArtifactKey, Kind: mediaCandidateArtifactKind, SchemaVersion: 1,
		Payload: payload, ResourceID: candidate.ResourceID, UpstreamRevisions: candidate.UpstreamRevisions,
		ModelRequestIdentity: candidate.ProviderRequestIdentity, SkillVersions: candidate.SkillVersions,
	}, nil
}

func validCandidateReviewIdentity(command VisualCandidateReviewCommand) bool {
	return validStoredText(command.ReviewArtifactID, 80) && validStoredText(command.ReviewArtifactKey, 120) &&
		validStoredText(command.ReviewModelRecordID, 120) && validStoredText(command.ReviewRequestIdentity, 180) &&
		validStoredText(command.ReviewRunID, 80) && len(command.Candidates) > 0 &&
		len(command.Candidates) <= maximumVisualCandidates && command.Assessments != nil &&
		command.RankedCandidateArtifactIDs != nil && command.ConfirmedReferenceRevisions != nil &&
		len(command.ConfirmedReferenceRevisions) > 0 && command.Uncertainties != nil && command.Conflicts != nil &&
		command.RetrySuggestions != nil && command.SkillVersions != nil
}

func (s *Service) validateVisualCandidateReview(
	scope agentruntime.Scope,
	command VisualCandidateReviewCommand,
	candidates map[string]model.AgentArtifactRevision,
) (visualConsistencyReviewPayload, []agentruntime.ArtifactRevisionRef, []string, error) {
	if len(candidates) != len(command.Candidates) || len(command.Assessments) != len(candidates) ||
		len(command.RankedCandidateArtifactIDs) != len(candidates) ||
		!validUniqueStoredTextList(command.Uncertainties, maximumConsistencyDescriptionLen) ||
		!validUniqueStoredTextList(command.Conflicts, maximumConsistencyDescriptionLen) ||
		!validUniqueStoredTextList(command.RetrySuggestions, maximumConsistencyDescriptionLen) {
		return visualConsistencyReviewPayload{}, nil, nil, ErrVisualCandidateReviewInvalid
	}

	confirmed := make(map[agentruntime.ArtifactRevisionRef]struct{}, len(command.ConfirmedReferenceRevisions))
	upstream := make([]agentruntime.ArtifactRevisionRef, 0, len(candidates)*2+len(command.ConfirmedReferenceRevisions))
	for _, reference := range command.ConfirmedReferenceRevisions {
		if _, exists := confirmed[reference]; exists || s.requireExactVisualEvidence(scope, reference) != nil {
			return visualConsistencyReviewPayload{}, nil, nil, ErrVisualCandidateReviewInvalid
		}
		confirmed[reference] = struct{}{}
		upstream = append(upstream, reference)
	}

	candidateRefs := make([]agentruntime.ArtifactRevisionRef, 0, len(command.Candidates))
	for _, candidate := range command.Candidates {
		revision, exists := candidates[candidate.ArtifactID]
		if !exists || revision.ResourceID != candidate.ResourceID || revision.ModelRequestIdentity != candidate.ProviderRequestIdentity {
			return visualConsistencyReviewPayload{}, nil, nil, ErrVisualCandidateReviewInvalid
		}
		resource, err := s.productionResourceForScope(scope, revision.ResourceID)
		if err != nil || !reviewCandidateResourceReady(resource, candidate.MediaKind) {
			return visualConsistencyReviewPayload{}, nil, nil, errors.Join(ErrVisualCandidateReviewInvalid, err)
		}
		ref := agentruntime.ArtifactRevisionRef{ArtifactID: revision.ArtifactID, RevisionID: revision.ID}
		candidateRefs = append(candidateRefs, ref)
		upstream = append(upstream, ref)
	}

	assessments := make([]storedVisualCandidateAssessment, 0, len(command.Assessments))
	assessed := make(map[string]struct{}, len(command.Assessments))
	for _, assessment := range command.Assessments {
		candidate, exists := candidates[assessment.CandidateArtifactID]
		if !exists {
			return visualConsistencyReviewPayload{}, nil, nil, ErrVisualCandidateReviewInvalid
		}
		if _, duplicate := assessed[assessment.CandidateArtifactID]; duplicate {
			return visualConsistencyReviewPayload{}, nil, nil, ErrVisualCandidateReviewInvalid
		}
		candidateRef := agentruntime.ArtifactRevisionRef{ArtifactID: candidate.ArtifactID, RevisionID: candidate.ID}
		if err := s.validateCandidateAssessment(scope, candidateRef, assessment, confirmed); err != nil {
			return visualConsistencyReviewPayload{}, nil, nil, err
		}
		assessed[assessment.CandidateArtifactID] = struct{}{}
		upstream = append(upstream, assessment.VisualEvidenceRevision)
		assessments = append(assessments, storedVisualCandidateAssessment{
			CandidateRevision: candidateRef, VisualEvidenceRevision: assessment.VisualEvidenceRevision,
			Findings: assessment.Findings,
		})
	}

	rankedRevisionIDs := make([]string, 0, len(command.RankedCandidateArtifactIDs))
	ranked := make(map[string]struct{}, len(command.RankedCandidateArtifactIDs))
	for _, artifactID := range command.RankedCandidateArtifactIDs {
		candidate, exists := candidates[artifactID]
		if !exists {
			return visualConsistencyReviewPayload{}, nil, nil, ErrVisualCandidateReviewInvalid
		}
		if _, duplicate := ranked[artifactID]; duplicate {
			return visualConsistencyReviewPayload{}, nil, nil, ErrVisualCandidateReviewInvalid
		}
		ranked[artifactID] = struct{}{}
		rankedRevisionIDs = append(rankedRevisionIDs, candidate.ID)
	}
	upstream = uniqueRevisionRefs(upstream)
	return visualConsistencyReviewPayload{
		ReviewRunID: command.ReviewRunID, ReviewModelRecordID: command.ReviewModelRecordID,
		ReviewRequestIdentity: command.ReviewRequestIdentity, CandidateRevisions: candidateRefs,
		ConfirmedReferenceRevisions: command.ConfirmedReferenceRevisions, Assessments: assessments,
		RankedCandidateRevisionIDs: rankedRevisionIDs, Uncertainties: command.Uncertainties,
		Conflicts: command.Conflicts, RetrySuggestions: command.RetrySuggestions,
	}, upstream, rankedRevisionIDs, nil
}

func (s *Service) validateCandidateAssessment(
	scope agentruntime.Scope,
	candidate agentruntime.ArtifactRevisionRef,
	assessment VisualCandidateAssessment,
	confirmed map[agentruntime.ArtifactRevisionRef]struct{},
) error {
	if len(assessment.Findings) != len(allVisualConsistencyDimensions()) ||
		s.requireExactReviewRevision(scope, assessment.VisualEvidenceRevision, "visual_evidence") != nil {
		return ErrVisualCandidateReviewInvalid
	}
	evidenceRevision, err := s.repo.ArtifactRevisionForArtifactInScope(
		scope, assessment.VisualEvidenceRevision.ArtifactID, assessment.VisualEvidenceRevision.RevisionID,
	)
	if err != nil {
		return ErrVisualCandidateReviewInvalid
	}
	evidence, err := agentruntime.DecodeVisualEvidence([]byte(evidenceRevision.PayloadJSON))
	if err != nil || evidence.SourceRevision != candidate {
		return ErrVisualCandidateReviewInvalid
	}
	if !validConsistencyFindings(assessment.Findings, assessment.VisualEvidenceRevision, confirmed) {
		return ErrVisualCandidateReviewInvalid
	}
	return nil
}

func validConsistencyFindings(
	findings []VisualConsistencyFinding,
	visualEvidenceRevision agentruntime.ArtifactRevisionRef,
	confirmed map[agentruntime.ArtifactRevisionRef]struct{},
) bool {
	if len(findings) != len(allVisualConsistencyDimensions()) || visualEvidenceRevision.Validate() != nil || len(confirmed) == 0 {
		return false
	}
	seen := make(map[VisualConsistencyDimension]struct{}, len(findings))
	for _, finding := range findings {
		if !validConsistencyDimension(finding.Dimension) || !validConsistencyOutcome(finding.Outcome) ||
			!validStoredText(finding.Description, maximumConsistencyDescriptionLen) ||
			finding.ConfidenceBasisPoints < 0 || finding.ConfidenceBasisPoints > 10_000 || finding.EvidenceRevisions == nil {
			return false
		}
		if _, duplicate := seen[finding.Dimension]; duplicate {
			return false
		}
		seen[finding.Dimension] = struct{}{}
		hasCandidateEvidence := false
		hasConfirmedReference := false
		seenEvidence := make(map[agentruntime.ArtifactRevisionRef]struct{}, len(finding.EvidenceRevisions))
		for _, reference := range finding.EvidenceRevisions {
			if reference.Validate() != nil {
				return false
			}
			if _, duplicate := seenEvidence[reference]; duplicate {
				return false
			}
			seenEvidence[reference] = struct{}{}
			if reference == visualEvidenceRevision {
				hasCandidateEvidence = true
				continue
			}
			if _, exists := confirmed[reference]; exists {
				hasConfirmedReference = true
				continue
			}
			return false
		}
		if !hasCandidateEvidence || !hasConfirmedReference {
			return false
		}
	}
	return len(seen) == len(allVisualConsistencyDimensions())
}

func (s *Service) requireExactReviewRevision(
	scope agentruntime.Scope,
	reference agentruntime.ArtifactRevisionRef,
	requiredKind string,
) error {
	if reference.Validate() != nil {
		return ErrVisualCandidateReviewInvalid
	}
	revision, err := s.repo.ArtifactRevisionForArtifactInScope(scope, reference.ArtifactID, reference.RevisionID)
	if err != nil || revision.LifecycleStatus == model.AgentArtifactRevisionStale ||
		(requiredKind != "" && revision.Kind != requiredKind) {
		return ErrVisualCandidateReviewInvalid
	}
	head, err := s.repo.ArtifactHeadRevisionForScope(scope, reference.ArtifactID)
	if err != nil || head.ID != revision.ID {
		return ErrVisualCandidateReviewInvalid
	}
	return nil
}

func (s *Service) requireExactVisualEvidence(scope agentruntime.Scope, reference agentruntime.ArtifactRevisionRef) error {
	if s.requireExactReviewRevision(scope, reference, "visual_evidence") != nil {
		return ErrVisualCandidateReviewInvalid
	}
	revision, err := s.repo.ArtifactRevisionForArtifactInScope(scope, reference.ArtifactID, reference.RevisionID)
	if err != nil {
		return ErrVisualCandidateReviewInvalid
	}
	if _, err := agentruntime.DecodeVisualEvidence([]byte(revision.PayloadJSON)); err != nil {
		return ErrVisualCandidateReviewInvalid
	}
	return nil
}

func (s *Service) decodeVisualConsistencyReview(
	scope agentruntime.Scope,
	revision model.AgentArtifactRevision,
) (visualConsistencyReviewPayload, error) {
	if revision.Kind != visualConsistencyReviewKind || revision.SchemaVersion != 1 || revision.ResourceID != "" {
		return visualConsistencyReviewPayload{}, ErrVisualCandidateSelectionInvalid
	}
	var payload visualConsistencyReviewPayload
	if err := decodeStrictStoredJSONDocument(revision.PayloadJSON, func(decoder *json.Decoder) error {
		return decoder.Decode(&payload)
	}); err != nil || !validStoredVisualConsistencyReview(revision, payload) ||
		s.validateStoredVisualReviewReferences(scope, payload) != nil {
		return visualConsistencyReviewPayload{}, ErrVisualCandidateSelectionInvalid
	}
	return payload, nil
}

func validStoredVisualConsistencyReview(revision model.AgentArtifactRevision, payload visualConsistencyReviewPayload) bool {
	if !validStoredText(payload.ReviewRunID, 80) || !validStoredText(payload.ReviewModelRecordID, 120) ||
		!validStoredText(payload.ReviewRequestIdentity, 180) || payload.ReviewRequestIdentity != revision.ModelRequestIdentity ||
		len(payload.CandidateRevisions) == 0 || len(payload.CandidateRevisions) > maximumVisualCandidates ||
		len(payload.Assessments) != len(payload.CandidateRevisions) ||
		len(payload.RankedCandidateRevisionIDs) != len(payload.CandidateRevisions) ||
		len(payload.ConfirmedReferenceRevisions) == 0 ||
		!validUniqueStoredTextList(payload.Uncertainties, maximumConsistencyDescriptionLen) ||
		!validUniqueStoredTextList(payload.Conflicts, maximumConsistencyDescriptionLen) ||
		!validUniqueStoredTextList(payload.RetrySuggestions, maximumConsistencyDescriptionLen) {
		return false
	}

	candidates, candidateIDs, ok := uniqueRevisionRefSets(payload.CandidateRevisions)
	if !ok {
		return false
	}
	confirmed, _, ok := uniqueRevisionRefSets(payload.ConfirmedReferenceRevisions)
	if !ok {
		return false
	}
	assessed := make(map[agentruntime.ArtifactRevisionRef]struct{}, len(payload.Assessments))
	evidence := make(map[agentruntime.ArtifactRevisionRef]struct{}, len(payload.Assessments))
	for _, assessment := range payload.Assessments {
		if _, exists := candidates[assessment.CandidateRevision]; !exists {
			return false
		}
		if _, duplicate := assessed[assessment.CandidateRevision]; duplicate {
			return false
		}
		if assessment.VisualEvidenceRevision.Validate() != nil {
			return false
		}
		if _, duplicate := evidence[assessment.VisualEvidenceRevision]; duplicate {
			return false
		}
		if !validConsistencyFindings(assessment.Findings, assessment.VisualEvidenceRevision, confirmed) {
			return false
		}
		assessed[assessment.CandidateRevision] = struct{}{}
		evidence[assessment.VisualEvidenceRevision] = struct{}{}
	}
	ranked := make(map[string]struct{}, len(payload.RankedCandidateRevisionIDs))
	for _, revisionID := range payload.RankedCandidateRevisionIDs {
		if _, exists := candidateIDs[revisionID]; !exists || !validStoredText(revisionID, 120) {
			return false
		}
		if _, duplicate := ranked[revisionID]; duplicate {
			return false
		}
		ranked[revisionID] = struct{}{}
	}

	var storedUpstream []agentruntime.ArtifactRevisionRef
	if err := decodeStrictStoredJSONDocument(revision.UpstreamRevisionsJSON, func(decoder *json.Decoder) error {
		return decoder.Decode(&storedUpstream)
	}); err != nil {
		return false
	}
	expectedUpstream := append([]agentruntime.ArtifactRevisionRef{}, payload.ConfirmedReferenceRevisions...)
	expectedUpstream = append(expectedUpstream, payload.CandidateRevisions...)
	for _, assessment := range payload.Assessments {
		expectedUpstream = append(expectedUpstream, assessment.VisualEvidenceRevision)
	}
	return revisionRefsEqual(storedUpstream, uniqueRevisionRefs(expectedUpstream))
}

func (s *Service) validateStoredVisualReviewReferences(
	scope agentruntime.Scope,
	payload visualConsistencyReviewPayload,
) error {
	for _, reference := range payload.ConfirmedReferenceRevisions {
		if s.requireExactVisualEvidence(scope, reference) != nil {
			return ErrVisualCandidateSelectionInvalid
		}
	}
	for _, reference := range payload.CandidateRevisions {
		if s.requireExactReviewRevision(scope, reference, mediaCandidateArtifactKind) != nil {
			return ErrVisualCandidateSelectionInvalid
		}
		candidate, err := s.repo.ArtifactRevisionForArtifactInScope(scope, reference.ArtifactID, reference.RevisionID)
		if err != nil || !validStoredMediaCandidate(*candidate) {
			return ErrVisualCandidateSelectionInvalid
		}
	}
	for _, assessment := range payload.Assessments {
		if s.requireExactVisualEvidence(scope, assessment.VisualEvidenceRevision) != nil {
			return ErrVisualCandidateSelectionInvalid
		}
		evidenceRevision, err := s.repo.ArtifactRevisionForArtifactInScope(
			scope, assessment.VisualEvidenceRevision.ArtifactID, assessment.VisualEvidenceRevision.RevisionID,
		)
		if err != nil {
			return ErrVisualCandidateSelectionInvalid
		}
		evidence, err := agentruntime.DecodeVisualEvidence([]byte(evidenceRevision.PayloadJSON))
		if err != nil || evidence.SourceRevision != assessment.CandidateRevision {
			return ErrVisualCandidateSelectionInvalid
		}
	}
	return nil
}

func validStoredMediaCandidate(revision model.AgentArtifactRevision) bool {
	if revision.Kind != mediaCandidateArtifactKind || revision.SchemaVersion != 1 ||
		!validStoredText(revision.ArtifactKey, 120) || !validStoredText(revision.ResourceID, 80) ||
		!validStoredText(revision.ModelRequestIdentity, 180) {
		return false
	}
	candidate, err := agentruntime.DecodeMediaCandidateContent([]byte(revision.PayloadJSON))
	if err != nil {
		return false
	}
	return candidate.CandidateKey == revision.ArtifactKey &&
		validStoredText(candidate.SourceTaskID, 80) && candidate.ResourceID == revision.ResourceID &&
		candidate.ProviderRequestIdentity == revision.ModelRequestIdentity
}

func uniqueRevisionRefSets(
	references []agentruntime.ArtifactRevisionRef,
) (map[agentruntime.ArtifactRevisionRef]struct{}, map[string]struct{}, bool) {
	byReference := make(map[agentruntime.ArtifactRevisionRef]struct{}, len(references))
	byRevisionID := make(map[string]struct{}, len(references))
	artifactIDs := make(map[string]struct{}, len(references))
	for _, reference := range references {
		if reference.Validate() != nil {
			return nil, nil, false
		}
		if _, duplicate := byReference[reference]; duplicate {
			return nil, nil, false
		}
		if _, duplicate := byRevisionID[reference.RevisionID]; duplicate {
			return nil, nil, false
		}
		if _, duplicate := artifactIDs[reference.ArtifactID]; duplicate {
			return nil, nil, false
		}
		byReference[reference] = struct{}{}
		byRevisionID[reference.RevisionID] = struct{}{}
		artifactIDs[reference.ArtifactID] = struct{}{}
	}
	return byReference, byRevisionID, true
}

func revisionRefsEqual(left []agentruntime.ArtifactRevisionRef, right []agentruntime.ArtifactRevisionRef) bool {
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

func candidateSelectionArtifactID(
	scope agentruntime.Scope,
	stageID string,
	reviewRevisionID string,
	selectedRevisionID string,
	clientRequestID string,
) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		"media-candidate-selection", string(scope.TenantKind), scope.TenantID, scope.ActorUserID,
		scope.DomainProjectID, scope.CanvasID, scope.ThreadID, scope.RunID, stageID,
		reviewRevisionID, selectedRevisionID, clientRequestID,
	}, "\x00")))
	return "candidate-selection-" + hex.EncodeToString(digest[:16])
}

func reviewCandidateResourceReady(resource *model.Resource, mediaKind string) bool {
	if resource == nil || resource.Status != model.ResourceStatusReady || resource.Kind != mediaKind ||
		strings.TrimSpace(resource.Provider) == "" || resource.Provider == "local" ||
		strings.TrimSpace(resource.Endpoint) == "" || strings.TrimSpace(resource.Bucket) == "" ||
		strings.TrimSpace(resource.ObjectKey) == "" {
		return false
	}
	prefix := mediaKind + "/"
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(resource.MimeType)), prefix)
}

func validConsistencyDimension(dimension VisualConsistencyDimension) bool {
	for _, allowed := range allVisualConsistencyDimensions() {
		if dimension == allowed {
			return true
		}
	}
	return false
}

func validConsistencyOutcome(outcome VisualConsistencyOutcome) bool {
	switch outcome {
	case VisualConsistencyMatched, VisualConsistencyDeviation, VisualConsistencyUncertain:
		return true
	default:
		return false
	}
}

func validStoredText(value string, maximum int) bool {
	return value != "" && strings.TrimSpace(value) == value && len(value) <= maximum
}

func validUniqueStoredTextList(values []string, maximum int) bool {
	if values == nil {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validStoredText(value, maximum) {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func uniqueRevisionRefs(references []agentruntime.ArtifactRevisionRef) []agentruntime.ArtifactRevisionRef {
	unique := make([]agentruntime.ArtifactRevisionRef, 0, len(references))
	seen := make(map[agentruntime.ArtifactRevisionRef]struct{}, len(references))
	for _, reference := range references {
		if _, exists := seen[reference]; exists {
			continue
		}
		seen[reference] = struct{}{}
		unique = append(unique, reference)
	}
	return unique
}

func findReviewCandidate(
	payload visualConsistencyReviewPayload,
	selectedRevisionID string,
) (agentruntime.ArtifactRevisionRef, bool) {
	for _, reference := range payload.CandidateRevisions {
		if reference.RevisionID == selectedRevisionID {
			return reference, true
		}
	}
	return agentruntime.ArtifactRevisionRef{}, false
}

func mapNotFoundToCandidateSelection(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrVisualCandidateSelectionInvalid
	}
	return err
}

func candidateSelectionDraft(
	scope agentruntime.Scope,
	stageID string,
	review model.AgentArtifactRevision,
	selected model.AgentArtifactRevision,
	clientRequestID string,
) (string, agentruntime.ArtifactDraft, error) {
	reviewRef := agentruntime.ArtifactRevisionRef{ArtifactID: review.ArtifactID, RevisionID: review.ID}
	selectedRef := agentruntime.ArtifactRevisionRef{ArtifactID: selected.ArtifactID, RevisionID: selected.ID}
	var reviewUpstream []agentruntime.ArtifactRevisionRef
	if err := decodeStrictStoredJSONDocument(review.UpstreamRevisionsJSON, func(decoder *json.Decoder) error {
		return decoder.Decode(&reviewUpstream)
	}); err != nil {
		return "", agentruntime.ArtifactDraft{}, ErrVisualCandidateSelectionInvalid
	}
	payload, err := json.Marshal(mediaCandidateSelectionPayload{
		StageID: stageID, ReviewRevision: reviewRef, SelectedCandidateRevision: selectedRef,
		ApprovedByUserID: scope.ActorUserID, ClientRequestID: clientRequestID,
	})
	if err != nil {
		return "", agentruntime.ArtifactDraft{}, err
	}
	artifactID := candidateSelectionArtifactID(scope, stageID, review.ID, selected.ID, clientRequestID)
	selectionUpstream := uniqueRevisionRefs(append([]agentruntime.ArtifactRevisionRef{reviewRef, selectedRef}, reviewUpstream...))
	return artifactID, agentruntime.ArtifactDraft{
		ArtifactKey: artifactID, Kind: mediaCandidateSelectionKind, SchemaVersion: 1,
		Payload: payload, UpstreamRevisions: selectionUpstream,
		SkillVersions: []agentruntime.SkillSelection{},
	}, nil
}
