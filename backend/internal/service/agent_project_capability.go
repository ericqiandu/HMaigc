package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

type agentProjectApplyOpsCapabilityExecutor struct {
	service *Service
}

func (s *Service) agentProjectApplyOpsWouldChange(scope agentruntime.Scope, arguments agentruntime.ProjectApplyOpsArguments) (bool, error) {
	project, err := s.repo.ProjectReadableForUser(scope.ActorUserID, arguments.DomainProjectID)
	if err != nil {
		return false, err
	}
	if project.ID != scope.DomainProjectID || project.Revision != arguments.BaseRevision {
		return true, nil
	}
	currentShots, err := s.repo.ProjectShots(project.ID)
	if err != nil {
		return false, err
	}
	shotsByUnitID := make(map[string][]model.Shot)
	for _, shot := range currentShots {
		shotsByUnitID[shot.UnitID] = append(shotsByUnitID[shot.UnitID], shot)
	}
	for _, operation := range arguments.Operations {
		switch operation.Type {
		case agentruntime.ProjectOperationCreateCharacterCandidates:
			return true, nil
		case agentruntime.ProjectOperationReplaceUnitShots:
			stored := shotsByUnitID[operation.UnitID]
			if len(stored) != len(operation.Shots) {
				return true, nil
			}
			for index, requested := range operation.Shots {
				current := stored[index]
				if current.Position != index || current.Title != requested.Title || current.Description != requested.Description || current.DurationMs != requested.DurationMS {
					return true, nil
				}
			}
		default:
			return true, nil
		}
	}
	return false, nil
}

func (executor agentProjectApplyOpsCapabilityExecutor) Execute(ctx context.Context, scope agentruntime.Scope, call agentruntime.ToolCallDecision) (agentruntime.ToolExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return agentruntime.ToolExecutionResult{}, err
	}
	if executor.service == nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_unavailable", "project.apply_ops executor is unavailable")
	}
	if err := scope.Validate(); err != nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_scope_invalid", err.Error())
	}
	decoded, err := agentruntime.DecodeCapabilityArguments(call.ToolName, call.Arguments)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_arguments_invalid", "project.apply_ops arguments are invalid")
	}
	arguments, ok := decoded.(agentruntime.ProjectApplyOpsArguments)
	if !ok || call.ToolName != agentruntime.ToolProjectApplyOps || call.ActionVersion != 1 {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_arguments_invalid", "project.apply_ops call identity is invalid")
	}
	if arguments.DomainProjectID != scope.DomainProjectID {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_scope_conflict", "project.apply_ops project scope is stale")
	}
	record, proposalHash, err := executor.service.authorizedProjectApplyOpsRecord(scope, call)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, err
	}
	operationIDs, expectedCandidateCount, expectedShotCount := summarizeProjectOperations(arguments.Operations)
	candidates, replacements, candidateIDs, shotIDs, err := translateAgentProjectOperations(scope.DomainProjectID, arguments.ClientMutationID, arguments.Operations)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, err
	}
	if record.Status == agentruntime.ToolCallSucceeded {
		decodedResult, decodeErr := agentruntime.DecodeCapabilityResult(agentruntime.ToolProjectApplyOps, json.RawMessage(record.OutputJSON))
		if decodeErr != nil {
			return agentruntime.ToolExecutionResult{}, failAgentCapability("project_receipt_invalid", "project.apply_ops stored receipt is invalid")
		}
		result, valid := decodedResult.(agentruntime.ProjectApplyOpsResult)
		if !valid || result.DomainProjectID != arguments.DomainProjectID || result.BaseRevision != arguments.BaseRevision ||
			result.ClientMutationID != arguments.ClientMutationID || result.ProposalHash != proposalHash ||
			!sameStringSequence(result.AppliedOperationIDs, operationIDs) || len(result.CandidateIDs) != expectedCandidateCount || len(result.ShotIDs) != expectedShotCount ||
			!sameStringSequence(result.CandidateIDs, candidateIDs) || !sameStringSequence(result.ShotIDs, shotIDs) {
			return agentruntime.ToolExecutionResult{}, failAgentCapability("project_receipt_invalid", "project.apply_ops stored receipt conflicts with the approved mutation")
		}
		return agentruntime.NewToolExecutionResult(agentruntime.ToolProjectApplyOps, result)
	}
	project, err := executor.service.repo.ProjectEditableForUser(scope.ActorUserID, scope.DomainProjectID, time.Now().UTC())
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "capability_ownership_forbidden", Err: err}
	}
	if project.Revision != arguments.BaseRevision {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("project_revision_conflict", "project.apply_ops base revision is stale")
	}
	result, err := agentruntime.NewToolExecutionResult(agentruntime.ToolProjectApplyOps, agentruntime.ProjectApplyOpsResult{
		DomainProjectID: scope.DomainProjectID, BaseRevision: arguments.BaseRevision, CommittedRevision: arguments.BaseRevision + 1,
		ClientMutationID: arguments.ClientMutationID, ProposalHash: proposalHash, AppliedOperationIDs: operationIDs,
		CandidateIDs: candidateIDs, ShotIDs: shotIDs,
	})
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "capability_result_invalid", Err: err}
	}
	err = executor.service.repo.CommitAgentProjectMutation(repository.AgentProjectMutationInput{
		Scope: scope, ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion, ProposalHash: proposalHash,
		BaseRevision: arguments.BaseRevision, ToolReceiptJSON: string(result.Output), Candidates: candidates,
		ShotReplacements: replacements, Now: time.Now().UTC(),
	})
	if err != nil {
		if errors.Is(err, repository.ErrProjectShotsInUse) {
			return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "project_shots_in_use", Err: err}
		}
		if errors.Is(err, repository.ErrProjectRevisionConflict) {
			return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "project_revision_conflict", Err: err}
		}
		if errors.Is(err, repository.ErrProjectUnitScopeConflict) {
			return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "project_unit_scope_conflict", Err: err}
		}
		if errors.Is(err, repository.ErrAgentRuntimeStepConflict) {
			return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "project_commit_conflict", Err: err}
		}
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "project_commit_failed", Err: err}
	}
	authoritative, err := executor.service.repo.ProjectReadableForUser(scope.ActorUserID, scope.DomainProjectID)
	if err != nil || authoritative.Revision < arguments.BaseRevision+1 {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "project_commit_unreadable", Err: err}
	}
	return result, nil
}

func summarizeProjectOperations(operations []agentruntime.ProjectOperation) ([]string, int, int) {
	operationIDs := make([]string, 0, len(operations))
	candidateCount := 0
	shotCount := 0
	for _, operation := range operations {
		operationIDs = append(operationIDs, operation.OperationID)
		candidateCount += len(operation.Candidates)
		shotCount += len(operation.Shots)
	}
	return operationIDs, candidateCount, shotCount
}

func sameStringSequence(left []string, right []string) bool {
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

func (s *Service) authorizedProjectApplyOpsRecord(scope agentruntime.Scope, call agentruntime.ToolCallDecision) (*model.AgentToolCall, string, error) {
	record, err := s.repo.AgentToolCallForScope(scope, call.ToolCallID, call.ActionVersion)
	if err != nil {
		return nil, "", &agentCapabilityExecutionError{Code: "project_proposal_missing", Err: err}
	}
	if record.ToolName != string(agentruntime.ToolProjectApplyOps) || record.ToolName != string(call.ToolName) ||
		!equalAgentToolArguments(record.InputJSON, call.Arguments) || record.ApprovalProposalHash == "" {
		return nil, "", failAgentCapability("project_proposal_conflict", "project.apply_ops frozen proposal conflicts with the requested call")
	}
	if record.Status != agentruntime.ToolCallPending && record.Status != agentruntime.ToolCallRunning && record.Status != agentruntime.ToolCallSucceeded {
		return nil, "", failAgentCapability("project_proposal_conflict", "project.apply_ops proposal is not executable")
	}
	if record.ApprovalDecision != agentruntime.ToolApprovalApproved || record.ApprovalByUserID != scope.ActorUserID || record.ApprovalDecidedAt == nil {
		return nil, "", failAgentCapability("project_proposal_not_approved", "project.apply_ops proposal has not been approved by the current actor")
	}
	if err := validateStoredApprovalProposal(scope, record, record.ApprovalProposalHash, time.Now().UTC(), false); err != nil {
		return nil, "", &agentCapabilityExecutionError{Code: "project_proposal_invalid", Err: err}
	}
	return record, record.ApprovalProposalHash, nil
}

func translateAgentProjectOperations(projectID string, clientMutationID string, operations []agentruntime.ProjectOperation) ([]model.ProjectAssetCandidate, []repository.AgentProjectShotReplacement, []string, []string, error) {
	now := time.Now().UTC()
	candidates := make([]model.ProjectAssetCandidate, 0)
	replacements := make([]repository.AgentProjectShotReplacement, 0)
	candidateIDs := make([]string, 0)
	shotIDs := make([]string, 0)
	for _, operation := range operations {
		switch operation.Type {
		case agentruntime.ProjectOperationCreateCharacterCandidates:
			for position, input := range operation.Candidates {
				details, err := json.Marshal(input)
				if err != nil {
					return nil, nil, nil, nil, failAgentCapability("project_operation_invalid", "character candidate details are not serializable")
				}
				id := stableAgentProjectEntityID(projectID, clientMutationID, operation.OperationID, "candidate", position)
				candidateIDs = append(candidateIDs, id)
				candidates = append(candidates, model.ProjectAssetCandidate{
					ID: id, ProjectID: projectID, UnitID: operation.UnitID, Name: input.Name,
					Category: model.AssetCategoryCharacter, Status: "pending_confirmation", DetailsJSON: string(details),
					CreatedAt: now, UpdatedAt: now,
				})
			}
		case agentruntime.ProjectOperationReplaceUnitShots:
			shots := make([]model.Shot, 0, len(operation.Shots))
			for position, input := range operation.Shots {
				id := stableAgentProjectEntityID(projectID, clientMutationID, operation.OperationID, "shot", position)
				shotIDs = append(shotIDs, id)
				shots = append(shots, model.Shot{
					ID: id, ProjectID: projectID, UnitID: operation.UnitID, Title: input.Title, Description: input.Description,
					Position: position, DurationMs: input.DurationMS, Status: "draft", CreatedAt: now, UpdatedAt: now,
				})
			}
			replacements = append(replacements, repository.AgentProjectShotReplacement{UnitID: operation.UnitID, Shots: shots})
		default:
			return nil, nil, nil, nil, failAgentCapability("project_operation_invalid", "project operation type is unsupported")
		}
	}
	return candidates, replacements, candidateIDs, shotIDs, nil
}

func stableAgentProjectEntityID(projectID string, clientMutationID string, operationID string, entityKind string, position int) string {
	digest := sha256.Sum256([]byte(projectID + "\x00" + clientMutationID + "\x00" + operationID + "\x00" + entityKind + "\x00" + strconv.Itoa(position)))
	return hex.EncodeToString(digest[:16])
}
