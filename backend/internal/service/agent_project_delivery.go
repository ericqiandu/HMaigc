package service

import (
	"encoding/json"
	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

// 回执是历史提交事实，不能单独证明产物仍存在；仅同运行中更新的章节替换可取代旧回执。
func currentAgentProjectEntities(scope agentruntime.Scope, calls []model.AgentToolCall, facts repository.AgentProjectDeliverySnapshot) (bool, error) {
	type unitDelivery struct {
		revision int64
		shots    []model.Shot
	}
	units := make(map[string]unitDelivery)
	expectedCandidates := make(map[string]model.ProjectAssetCandidate)
	for _, call := range calls {
		if call.Status != agentruntime.ToolCallSucceeded || call.ToolName != string(agentruntime.ToolProjectApplyOps) {
			continue
		}
		receipt, err := currentProjectApplyOpsDelivery(scope, call)
		if err != nil {
			return false, err
		}
		decoded, err := agentruntime.DecodeCapabilityArguments(agentruntime.ToolProjectApplyOps, json.RawMessage(call.InputJSON))
		if err != nil {
			return false, err
		}
		args := decoded.(agentruntime.ProjectApplyOpsArguments)
		candidates, replacements, _, _, err := translateAgentProjectOperations(scope.DomainProjectID, args.ClientMutationID, args.Operations)
		if err != nil {
			return false, err
		}
		for _, candidate := range candidates {
			expectedCandidates[candidate.ID] = candidate
		}
		for _, replacement := range replacements {
			if previous, exists := units[replacement.UnitID]; !exists || receipt.CommittedRevision > previous.revision {
				units[replacement.UnitID] = unitDelivery{revision: receipt.CommittedRevision, shots: replacement.Shots}
			}
		}
	}
	shotsByUnit := make(map[string][]model.Shot)
	for _, shot := range facts.Shots {
		shotsByUnit[shot.UnitID] = append(shotsByUnit[shot.UnitID], shot)
	}
	for unitID, expected := range units {
		actual := shotsByUnit[unitID]
		if len(actual) != len(expected.shots) {
			return false, nil
		}
		for i, shot := range expected.shots {
			current := actual[i]
			if current.ID != shot.ID || current.ProjectID != shot.ProjectID || current.UnitID != shot.UnitID ||
				current.Position != shot.Position || current.Title != shot.Title || current.Description != shot.Description || current.DurationMs != shot.DurationMs {
				return false, nil
			}
		}
	}
	actualCandidates := make(map[string]model.ProjectAssetCandidate)
	for _, candidate := range facts.Candidates {
		actualCandidates[candidate.ID] = candidate
	}
	for id, expected := range expectedCandidates {
		actual, exists := actualCandidates[id]
		if !exists || actual.ProjectID != expected.ProjectID || actual.UnitID != expected.UnitID || actual.Name != expected.Name ||
			actual.Category != expected.Category || !equalAgentToolArguments(actual.DetailsJSON, json.RawMessage(expected.DetailsJSON)) {
			return false, nil
		}
	}
	return true, nil
}
