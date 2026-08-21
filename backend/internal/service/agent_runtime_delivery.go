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
		}
	}
	if hasCanvasCommit {
		arguments, err := decodeAgentProductionCanvasCommitArguments(json.RawMessage(latestCanvasCommit.InputJSON))
		if err != nil {
			return agentruntime.DeliveryEvidence{}, errors.New("agent committed plan delivery input is invalid")
		}
		_, artifacts, resources, err := s.productionCanvasCommitFacts(scope, arguments)
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
