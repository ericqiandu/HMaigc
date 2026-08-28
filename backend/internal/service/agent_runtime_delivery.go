package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

func (s *Service) agentRuntimeDeliveryEvidence(scope agentruntime.Scope, finalMessage string) (agentruntime.DeliveryEvidence, error) {
	calls, err := s.repo.AgentToolCallsForScope(scope)
	if err != nil {
		return agentruntime.DeliveryEvidence{}, err
	}
	evidence := agentruntime.DeliveryEvidence{FinalMessage: strings.TrimSpace(finalMessage)}
	seenArtifacts := make(map[string]bool)
	approvedRevisionIDs := map[string]struct{}{}
	publicationByRevisionID := map[string]string{}
	mediaFactsLoaded := false
	loadMediaFacts := func() error {
		if mediaFactsLoaded {
			return nil
		}
		approved, err := s.repo.ApprovedArtifactRevisionIDsForScope(scope)
		if err != nil {
			return err
		}
		publications, err := s.repo.SucceededAgentAssetPublicationsForScope(scope)
		if err != nil {
			return err
		}
		publicationByRevisionID, err = successfulPublicationIDsByRevision(publications)
		if err != nil {
			return err
		}
		approvedRevisionIDs = approved
		mediaFactsLoaded = true
		return nil
	}
	var latestCanvasCommit model.AgentToolCall
	hasCanvasCommit := false
	for _, call := range calls {
		if call.Status != agentruntime.ToolCallSucceeded {
			continue
		}
		switch agentruntime.ToolName(call.ToolName) {
		case agentruntime.ToolCanvasCommit:
			var result agentProductionCanvasCommitResult
			if err := json.Unmarshal([]byte(call.OutputJSON), &result); err != nil || result.CanvasID != scope.CanvasID || result.CommittedRevision < 1 || strings.TrimSpace(result.ClientMutationID) == "" {
				return agentruntime.DeliveryEvidence{}, errors.New("agent canvas delivery evidence is invalid")
			}
			if result.CommittedRevision >= evidence.CanvasRevision {
				evidence.CanvasID = result.CanvasID
				evidence.CanvasRevision = result.CommittedRevision
				latestCanvasCommit = call
				hasCanvasCommit = true
			}
		case agentruntime.ToolCanvasProject:
			result, err := validateAgentCanvasProjectionDelivery(scope, call)
			if err != nil {
				return agentruntime.DeliveryEvidence{}, err
			}
			if result.CommittedRevision >= evidence.CanvasRevision {
				evidence.CanvasID = result.CanvasID
				evidence.CanvasRevision = result.CommittedRevision
			}
		case agentruntime.ToolProductionRender:
			var result agentProductionRenderResult
			if err := json.Unmarshal([]byte(call.OutputJSON), &result); err != nil || result.ArtifactID == "" || result.ArtifactStatus != model.AgentProductionArtifactSucceeded || result.ResourceID == "" {
				return agentruntime.DeliveryEvidence{}, errors.New("agent generation delivery evidence is invalid")
			}
			resource, err := s.productionResourceForScope(scope, result.ResourceID)
			if err != nil || resource.Status != model.ResourceStatusReady {
				return agentruntime.DeliveryEvidence{}, errors.New("agent generation resource evidence is invalid")
			}
			kind := agentruntime.ArtifactKind("")
			switch result.ArtifactKind {
			case model.AgentProductionArtifactReferenceImage, model.AgentProductionArtifactStoryboardImage:
				kind = agentruntime.ArtifactImage
			case model.AgentProductionArtifactVideoClip:
				kind = agentruntime.ArtifactVideo
			default:
				return agentruntime.DeliveryEvidence{}, errors.New("agent generation artifact kind is invalid")
			}
			artifact := agentruntime.DeliveryArtifact{Kind: kind, URL: "/api/resources/" + resource.ID + "/file"}
			key := string(artifact.Kind) + "\x00" + artifact.URL
			if !seenArtifacts[key] {
				evidence.Artifacts = append(evidence.Artifacts, artifact)
				seenArtifacts[key] = true
			}
		case agentruntime.ToolMediaGenerate:
			arguments, err := decodeFrozenAgentMediaGenerationArguments(json.RawMessage(call.InputJSON))
			if err != nil {
				return agentruntime.DeliveryEvidence{}, errors.New("agent media delivery input is invalid")
			}
			result, err := agentruntime.DecodeMediaGenerationToolResult([]byte(call.OutputJSON))
			if err != nil {
				return agentruntime.DeliveryEvidence{}, errors.New("agent media delivery evidence is invalid")
			}
			frozenAudioMode, err := agentMediaAudioModeForArguments(arguments)
			if err != nil || result.AudioMode != frozenAudioMode {
				return agentruntime.DeliveryEvidence{}, errors.New("agent media audio delivery evidence is invalid")
			}
			if err := loadMediaFacts(); err != nil {
				return agentruntime.DeliveryEvidence{}, err
			}
			candidateSet := make(map[agentruntime.ArtifactRevisionRef]struct{}, len(result.Candidates))
			for _, reference := range result.Candidates {
				if reference.Validate() != nil {
					return agentruntime.DeliveryEvidence{}, errors.New("agent media candidate reference is invalid")
				}
				if _, duplicate := candidateSet[reference]; duplicate {
					return agentruntime.DeliveryEvidence{}, errors.New("agent media candidate reference is duplicated")
				}
				candidateSet[reference] = struct{}{}
				revision, err := s.repo.ArtifactRevisionForArtifactInScope(scope, reference.ArtifactID, reference.RevisionID)
				if err != nil {
					return agentruntime.DeliveryEvidence{}, err
				}
				resource, err := s.productionResourceForScope(scope, revision.ResourceID)
				if err != nil {
					return agentruntime.DeliveryEvidence{}, err
				}
				_, approved := approvedRevisionIDs[revision.ID]
				artifact, err := mediaCandidateDeliveryArtifact(*revision, *resource, approved, publicationByRevisionID[revision.ID])
				if err != nil {
					return agentruntime.DeliveryEvidence{}, err
				}
				key := artifact.ArtifactID + "\x00" + artifact.RevisionID
				if !seenArtifacts[key] {
					evidence.Artifacts = append(evidence.Artifacts, artifact)
					seenArtifacts[key] = true
				}
			}
		}
	}
	if hasCanvasCommit {
		arguments, err := decodeAgentProductionCanvasCommitArguments(json.RawMessage(latestCanvasCommit.InputJSON))
		if err != nil {
			return agentruntime.DeliveryEvidence{}, errors.New("agent committed plan delivery input is invalid")
		}
		_, artifacts, resources, _, err := s.productionCanvasCommitFacts(scope, arguments)
		if err != nil {
			return agentruntime.DeliveryEvidence{}, err
		}
		committedArtifacts, err := committedPlanDeliveryArtifacts(scope.CanvasID, artifacts, resources)
		if err != nil {
			return agentruntime.DeliveryEvidence{}, err
		}
		for _, artifact := range committedArtifacts {
			key := string(artifact.Kind) + "\x00" + artifact.URL
			if !seenArtifacts[key] {
				evidence.Artifacts = append(evidence.Artifacts, artifact)
				seenArtifacts[key] = true
			}
		}
	}
	return evidence, nil
}

func validateAgentCanvasProjectionDelivery(
	scope agentruntime.Scope,
	call model.AgentToolCall,
) (agentCanvasProjectionResult, error) {
	arguments, err := decodeCanvasProjectArguments(json.RawMessage(call.InputJSON))
	if err != nil || arguments.ExpectedDelivery.Kind != agentruntime.DeliveryCanvasChange ||
		arguments.ExpectedDelivery.TargetCanvasID != scope.CanvasID {
		return agentCanvasProjectionResult{}, errors.New("agent canvas projection delivery input is invalid")
	}
	result, err := decodeAgentCanvasProjectionResult([]byte(call.OutputJSON))
	if err != nil || result.CanvasID != scope.CanvasID || result.BaseRevision != arguments.BaseRevision ||
		result.ClientMutationID != agentCanvasMutationID(call.IdempotencyKey) ||
		len(result.Bindings) != len(arguments.ArtifactRevisions) {
		return agentCanvasProjectionResult{}, errors.New("agent canvas projection delivery evidence is invalid")
	}
	revisions := make(map[string]string, len(arguments.ArtifactRevisions))
	for _, reference := range arguments.ArtifactRevisions {
		if reference.Validate() != nil {
			return agentCanvasProjectionResult{}, errors.New("agent canvas projection delivery revision is invalid")
		}
		if _, duplicate := revisions[reference.ArtifactID]; duplicate {
			return agentCanvasProjectionResult{}, errors.New("agent canvas projection delivery revision is duplicated")
		}
		revisions[reference.ArtifactID] = reference.RevisionID
	}
	for _, binding := range result.Bindings {
		if revisions[binding.ArtifactID] != binding.RevisionID ||
			binding.NodeID != agentCanvasProjectionNodeID(scope.CanvasID, binding.ArtifactID) ||
			binding.ProjectionID != productionCanvasStableID("artifact-projection", scope.CanvasID, binding.ArtifactID) {
			return agentCanvasProjectionResult{}, errors.New("agent canvas projection delivery binding is invalid")
		}
	}
	return result, nil
}

func successfulPublicationIDsByRevision(publications []model.AgentAssetPublication) (map[string]string, error) {
	byRevisionID := make(map[string]string, len(publications))
	for _, publication := range publications {
		if publication.Status != model.AgentAssetPublicationSucceeded ||
			!validStoredText(publication.ID, 120) || !validStoredText(publication.ArtifactRevisionID, 120) {
			return nil, errors.New("agent publication delivery facts are invalid")
		}
		if byRevisionID[publication.ArtifactRevisionID] == "" {
			byRevisionID[publication.ArtifactRevisionID] = publication.ID
		}
	}
	return byRevisionID, nil
}

func mediaCandidateDeliveryArtifact(
	revision model.AgentArtifactRevision,
	resource model.Resource,
	approved bool,
	publicationID string,
) (agentruntime.DeliveryArtifact, error) {
	if !validStoredMediaCandidate(revision) || revision.ResourceID != resource.ID {
		return agentruntime.DeliveryArtifact{}, errors.New("agent media candidate delivery facts are invalid")
	}
	candidate, err := agentruntime.DecodeMediaCandidateContent([]byte(revision.PayloadJSON))
	if err != nil || resource.Kind != string(candidate.MediaKind) {
		return agentruntime.DeliveryArtifact{}, errors.New("agent media candidate resource facts are invalid")
	}
	if strings.TrimSpace(publicationID) != publicationID || len(publicationID) > 120 ||
		(publicationID != "" && (!approved || !agentMediaResourceReady(&resource))) {
		return agentruntime.DeliveryArtifact{}, errors.New("agent media candidate publication facts are invalid")
	}
	kind := agentruntime.ArtifactKind(candidate.MediaKind)
	artifact := agentruntime.DeliveryArtifact{
		Kind: kind, ArtifactID: revision.ArtifactID, RevisionID: revision.ID,
		ResourceID: resource.ID, ResourceReady: agentMediaResourceReady(&resource),
		Approved: approved, PublicationID: publicationID,
	}
	if artifact.ResourceReady {
		artifact.URL = "/api/resources/" + resource.ID + "/file"
	}
	return artifact, nil
}

func committedPlanDeliveryArtifacts(
	canvasID string,
	artifacts []model.AgentProductionArtifact,
	resources map[string]model.Resource,
) ([]agentruntime.DeliveryArtifact, error) {
	canvasID = strings.TrimSpace(canvasID)
	if canvasID == "" {
		return nil, errors.New("agent committed plan canvas identity is invalid")
	}
	evidence := make([]agentruntime.DeliveryArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifact.Status != model.AgentProductionArtifactCommitted || strings.TrimSpace(artifact.CanvasNodeID) == "" {
			return nil, errors.New("agent committed plan artifact evidence is incomplete")
		}
		switch artifact.Kind {
		case model.AgentProductionArtifactScript:
			evidence = append(evidence, agentruntime.DeliveryArtifact{
				Kind: agentruntime.ArtifactText,
				URL:  fmt.Sprintf("canvas://%s/nodes/%s", canvasID, artifact.CanvasNodeID),
			})
		case model.AgentProductionArtifactReferenceImage, model.AgentProductionArtifactStoryboardImage:
			resource, exists := resources[artifact.ResourceID]
			if !exists || resource.Status != model.ResourceStatusReady {
				return nil, errors.New("agent committed image resource evidence is incomplete")
			}
			evidence = append(evidence, agentruntime.DeliveryArtifact{Kind: agentruntime.ArtifactImage, URL: "/api/resources/" + resource.ID + "/file"})
		case model.AgentProductionArtifactVideoClip:
			resource, exists := resources[artifact.ResourceID]
			if !exists || resource.Status != model.ResourceStatusReady {
				return nil, errors.New("agent committed video resource evidence is incomplete")
			}
			evidence = append(evidence, agentruntime.DeliveryArtifact{Kind: agentruntime.ArtifactVideo, URL: "/api/resources/" + resource.ID + "/file"})
		default:
			return nil, errors.New("agent committed plan artifact kind is invalid")
		}
	}
	return evidence, nil
}
