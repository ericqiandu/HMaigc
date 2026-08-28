package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

func (s *Service) coordinatePendingAgentSpecialistDelegate(
	scope agentruntime.Scope,
	state agentruntime.RuntimeState,
	call *agentruntime.ToolCallDecision,
) (*AgentRuntimeProgress, error) {
	arguments, skills, err := freezeSpecialistDelegateArguments(state.Configuration, state.LoadedSkillDirs, call.Arguments)
	if err != nil || !arguments.ExpectedDelivery.Equal(call.ExpectedDelivery) {
		return s.resolvePendingAgentToolFailureWithOutput(scope, state, call, "specialist_delegate_invalid", map[string]string{
			"reason": errorMessageOr(err, "delegate delivery conflicts with the frozen tool call"),
		})
	}
	if !state.PendingToolStarted {
		started, beginErr := agentruntime.BeginToolExecution(state, agentruntime.ToolExecution{
			ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion,
		})
		if beginErr != nil {
			return nil, beginErr
		}
		progress, commitErr := s.commitAgentRuntimeState(scope, state, started)
		if commitErr != nil {
			return nil, commitErr
		}
		state = progress.State
		if !samePendingStartedTool(state, call) {
			return s.agentRuntimeProgressForCurrentState(scope, state)
		}
	}

	snapshot, err := s.ensureSpecialistProductionGraph(scope, arguments)
	if err != nil {
		failureCode := "specialist_delegate_graph_invalid"
		if errors.Is(err, repository.ErrProductionGraphVersionConflict) {
			failureCode = "specialist_delegate_graph_version_conflict"
		}
		return s.resolvePendingAgentToolFailureWithOutput(scope, state, call, failureCode, map[string]string{"reason": err.Error()})
	}
	stage, stageDraft, err := delegatedProductionStage(*snapshot, arguments.StageKey)
	if err != nil {
		return s.resolvePendingAgentToolFailureWithOutput(scope, state, call, "specialist_delegate_stage_invalid", map[string]string{"reason": err.Error()})
	}
	if err := requireDelegatedStageDependencies(*snapshot, stageDraft); err != nil {
		return s.resolvePendingAgentToolFailureWithOutput(scope, state, call, "specialist_delegate_dependency_incomplete", map[string]string{"reason": err.Error()})
	}
	if stage.Status == agentruntime.StageApproved || stage.Status == agentruntime.StageCompleted {
		return s.resolveApprovedSpecialistDelegate(scope, state, call, *snapshot, stage)
	}
	if stage.Status != agentruntime.StagePlanned && stage.Status != agentruntime.StageAwaitingReview {
		return s.resolvePendingAgentToolFailureWithOutput(scope, state, call, "specialist_delegate_stage_conflict", map[string]string{
			"reason": "the delegated production stage is not executable or awaiting review",
		})
	}
	parentRun, err := s.repo.AgentRunForScope(scope)
	if err != nil {
		return nil, err
	}
	request := agentruntime.SpecialistRequest{
		SpecialistRunID:      specialistDelegateRunID(scope, call.ToolCallID, call.ActionVersion, stage.ID),
		StageID:              stage.ID,
		SpecialistKey:        arguments.SpecialistKey,
		SpecialistVersion:    1,
		ParentModelRecordID:  parentRun.ModelRecordID,
		ParentModelKey:       parentRun.ModelKey,
		Objective:            arguments.Objective,
		InputRevisions:       arguments.InputRevisions,
		LoadedSkills:         skills,
		ToolAllowlist:        arguments.ToolAllowlist,
		ExpectedOutputSchema: arguments.ExpectedOutputSchema,
		ExpectedDelivery:     arguments.ExpectedDelivery,
	}
	if _, err := s.RunSpecialist(context.Background(), scope, *parentRun, request); err != nil {
		return s.resolvePendingAgentToolFailureWithOutput(scope, state, call, "specialist_delegate_failed", map[string]string{"reason": err.Error()})
	}
	latest, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	if !samePendingStartedTool(latest, call) {
		return s.agentRuntimeProgressForCurrentState(scope, latest)
	}
	return s.agentRuntimeProgressForCurrentState(scope, latest)
}

type specialistDelegateToolOutput struct {
	GraphVersion     int64                              `json:"graphVersion"`
	StageID          string                             `json:"stageId"`
	StageKey         string                             `json:"stageKey"`
	StageVersion     int64                              `json:"stageVersion"`
	StageStatus      agentruntime.ProductionStageStatus `json:"stageStatus"`
	ReviewRevisionID string                             `json:"reviewRevisionId"`
	SpecialistRunID  string                             `json:"specialistRunId"`
}

func (s *Service) activeSpecialistDelegateForStage(
	scope agentruntime.Scope,
	state agentruntime.RuntimeState,
	stageID string,
) (*agentruntime.ToolCallDecision, repository.ProductionRuntimeSnapshot, model.AgentProductionStage, error) {
	if state.Status != agentruntime.RunWaitingTool || !state.PendingToolStarted || state.PendingToolCall == nil ||
		state.PendingToolCall.ToolName != agentruntime.ToolSpecialistDelegate {
		return nil, repository.ProductionRuntimeSnapshot{}, model.AgentProductionStage{}, agentruntime.ErrSpecialistModelInheritance
	}
	call := state.PendingToolCall
	arguments, _, err := freezeSpecialistDelegateArguments(state.Configuration, state.LoadedSkillDirs, call.Arguments)
	if err != nil || !arguments.ExpectedDelivery.Equal(call.ExpectedDelivery) {
		return nil, repository.ProductionRuntimeSnapshot{}, model.AgentProductionStage{}, agentruntime.ErrSpecialistModelInheritance
	}
	snapshot, err := s.repo.ProductionRuntimeSnapshotForScope(scope)
	if err != nil {
		return nil, repository.ProductionRuntimeSnapshot{}, model.AgentProductionStage{}, err
	}
	if snapshot.Graph == nil || snapshot.Draft == nil || !reflect.DeepEqual(*snapshot.Draft, arguments.ProductionGraph) ||
		(snapshot.Graph.Version != arguments.ExpectedGraphVersion && snapshot.Graph.Version != arguments.ExpectedGraphVersion+1) {
		return nil, repository.ProductionRuntimeSnapshot{}, model.AgentProductionStage{}, agentruntime.ErrSpecialistModelInheritance
	}
	stage, _, err := delegatedProductionStage(*snapshot, arguments.StageKey)
	if err != nil || stage.ID != strings.TrimSpace(stageID) {
		return nil, repository.ProductionRuntimeSnapshot{}, model.AgentProductionStage{}, agentruntime.ErrSpecialistModelInheritance
	}
	return call, *snapshot, stage, nil
}

func (s *Service) resolveApprovedSpecialistDelegate(
	scope agentruntime.Scope,
	state agentruntime.RuntimeState,
	call *agentruntime.ToolCallDecision,
	snapshot repository.ProductionRuntimeSnapshot,
	stage model.AgentProductionStage,
) (*AgentRuntimeProgress, error) {
	if !samePendingStartedTool(state, call) || (stage.Status != agentruntime.StageApproved && stage.Status != agentruntime.StageCompleted) ||
		stage.ReviewRevisionID == "" || snapshot.Graph == nil {
		return nil, repository.ErrProductionStageReviewConflict
	}
	revision, err := s.repo.ArtifactRevisionInScope(scope, stage.ReviewRevisionID)
	if err != nil || revision.CreatedBySpecialistID == "" {
		return nil, errors.Join(repository.ErrProductionStageReviewConflict, err)
	}
	specialistRun, err := s.repo.AgentSpecialistRunForScope(scope, revision.CreatedBySpecialistID)
	if err != nil || specialistRun.StageID != stage.ID || specialistRun.Status != model.AgentSpecialistRunSucceeded {
		return nil, errors.Join(repository.ErrAgentSpecialistRunConflict, err)
	}
	output, err := json.Marshal(specialistDelegateToolOutput{
		GraphVersion: snapshot.Graph.Version, StageID: stage.ID, StageKey: stage.StageKey,
		StageVersion: stage.Version, StageStatus: stage.Status, ReviewRevisionID: stage.ReviewRevisionID,
		SpecialistRunID: specialistRun.ID,
	})
	if err != nil {
		return nil, err
	}
	resolved, err := agentruntime.ResolveTool(state, agentruntime.ToolResolution{
		ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion, Succeeded: true, Output: output,
	})
	if err != nil {
		return nil, err
	}
	progress, err := s.commitAgentRuntimeState(scope, state, resolved)
	if err != nil {
		return nil, err
	}
	recorded, err := s.repo.AgentToolCallForScope(scope, call.ToolCallID, call.ActionVersion)
	if err != nil {
		return nil, err
	}
	if recorded.Status != agentruntime.ToolCallSucceeded {
		return nil, errors.New("specialist delegate completion facts conflict")
	}
	return progress, nil
}

func (s *Service) ensureSpecialistProductionGraph(
	scope agentruntime.Scope,
	arguments SpecialistDelegateArguments,
) (*repository.ProductionRuntimeSnapshot, error) {
	snapshot, err := s.repo.ProductionRuntimeSnapshotForScope(scope)
	if err != nil {
		return nil, err
	}
	if snapshot.Graph == nil {
		if arguments.ExpectedGraphVersion != 0 {
			return nil, repository.ErrProductionGraphVersionConflict
		}
		if _, err := s.repo.AppendProductionGraphVersion(scope, 0, arguments.ProductionGraph); err != nil {
			return nil, err
		}
		return s.repo.ProductionRuntimeSnapshotForScope(scope)
	}
	if snapshot.Draft == nil || !reflect.DeepEqual(*snapshot.Draft, arguments.ProductionGraph) ||
		(snapshot.Graph.Version != arguments.ExpectedGraphVersion && snapshot.Graph.Version != arguments.ExpectedGraphVersion+1) {
		return nil, repository.ErrProductionGraphVersionConflict
	}
	return snapshot, nil
}

func delegatedProductionStage(
	snapshot repository.ProductionRuntimeSnapshot,
	stageKey string,
) (model.AgentProductionStage, agentruntime.ProductionStageDraft, error) {
	if snapshot.Draft == nil || len(snapshot.Draft.Stages) != len(snapshot.Stages) {
		return model.AgentProductionStage{}, agentruntime.ProductionStageDraft{}, repository.ErrProductionRuntimeSnapshotInvalid
	}
	for index := range snapshot.Draft.Stages {
		if snapshot.Draft.Stages[index].StageKey == stageKey && snapshot.Stages[index].StageKey == stageKey {
			return snapshot.Stages[index], snapshot.Draft.Stages[index], nil
		}
	}
	return model.AgentProductionStage{}, agentruntime.ProductionStageDraft{}, repository.ErrProductionStageConflict
}

func requireDelegatedStageDependencies(
	snapshot repository.ProductionRuntimeSnapshot,
	stage agentruntime.ProductionStageDraft,
) error {
	statusByKey := make(map[string]agentruntime.ProductionStageStatus, len(snapshot.Stages))
	for _, stored := range snapshot.Stages {
		statusByKey[stored.StageKey] = stored.Status
	}
	for _, dependency := range stage.DependsOnStageKeys {
		status, found := statusByKey[dependency]
		if !found || (status != agentruntime.StageApproved && status != agentruntime.StageCompleted) {
			return fmt.Errorf("production stage dependency %q is not approved", dependency)
		}
	}
	return nil
}

func specialistDelegateRunID(scope agentruntime.Scope, toolCallID string, actionVersion int, stageID string) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		"specialist-delegate", scope.RunID, toolCallID, strconv.Itoa(actionVersion), stageID,
	}, "\x00")))
	return fmt.Sprintf("%x", digest[:])
}

func errorMessageOr(err error, fallback string) string {
	if err != nil {
		return err.Error()
	}
	return fallback
}
