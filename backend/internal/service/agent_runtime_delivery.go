package service

import (
	"encoding/json"
	"errors"
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
			case model.AgentProductionArtifactStoryboardImage:
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
	return evidence, nil
}
