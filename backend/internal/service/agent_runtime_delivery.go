package service

import (
	"encoding/json"
	"errors"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
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
		case agentruntime.ToolCanvasApplyOps:
			var result agentCanvasApplyOpsResult
			if err := json.Unmarshal([]byte(call.OutputJSON), &result); err != nil || result.CanvasID != scope.CanvasID || result.CommittedRevision < 1 || strings.TrimSpace(result.ClientMutationID) == "" {
				return agentruntime.DeliveryEvidence{}, errors.New("agent canvas delivery evidence is invalid")
			}
			if result.CommittedRevision >= evidence.CanvasRevision {
				evidence.CanvasID = result.CanvasID
				evidence.CanvasRevision = result.CommittedRevision
			}
		case agentruntime.ToolGenerationWait:
			var result agentGenerationWaitResult
			if err := json.Unmarshal([]byte(call.OutputJSON), &result); err != nil || result.TaskID == "" || result.Status != "succeeded" || len(result.Artifacts) == 0 {
				return agentruntime.DeliveryEvidence{}, errors.New("agent generation delivery evidence is invalid")
			}
			for _, artifact := range result.Artifacts {
				if !artifact.Kind.Valid() || artifact.Kind == agentruntime.ArtifactCanvasRevision || strings.TrimSpace(artifact.URL) == "" {
					return agentruntime.DeliveryEvidence{}, errors.New("agent generation artifact evidence is invalid")
				}
				key := string(artifact.Kind) + "\x00" + artifact.URL
				if !seenArtifacts[key] {
					evidence.Artifacts = append(evidence.Artifacts, artifact)
					seenArtifacts[key] = true
				}
			}
		}
	}
	return evidence, nil
}
