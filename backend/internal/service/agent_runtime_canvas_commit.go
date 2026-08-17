package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

type agentProductionCanvasCommitArguments struct {
	PlanKey      string   `json:"planKey"`
	PlanVersion  int      `json:"planVersion"`
	BaseRevision int64    `json:"baseRevision"`
	ArtifactIDs  []string `json:"artifactIds"`
}

type agentProductionCanvasCommitResult struct {
	CanvasID          string                    `json:"canvasId"`
	PlanKey           string                    `json:"planKey"`
	PlanVersion       int                       `json:"planVersion"`
	BaseRevision      int64                     `json:"baseRevision"`
	CommittedRevision int64                     `json:"committedRevision"`
	ClientMutationID  string                    `json:"clientMutationId"`
	Bindings          []productionCanvasBinding `json:"bindings"`
}

func decodeAgentProductionCanvasCommitArguments(raw json.RawMessage) (agentProductionCanvasCommitArguments, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var arguments agentProductionCanvasCommitArguments
	if err := decoder.Decode(&arguments); err != nil {
		return agentProductionCanvasCommitArguments{}, errors.New("production canvas commit arguments are invalid")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return agentProductionCanvasCommitArguments{}, errors.New("production canvas commit arguments are invalid")
	}
	arguments.PlanKey = strings.TrimSpace(arguments.PlanKey)
	if arguments.PlanKey == "" || len(arguments.PlanKey) > 120 || arguments.PlanVersion < 1 ||
		arguments.BaseRevision < 0 || len(arguments.ArtifactIDs) == 0 || len(arguments.ArtifactIDs) > 129 {
		return agentProductionCanvasCommitArguments{}, errors.New("production canvas commit arguments are invalid")
	}
	seen := make(map[string]struct{}, len(arguments.ArtifactIDs))
	for index := range arguments.ArtifactIDs {
		arguments.ArtifactIDs[index] = strings.TrimSpace(arguments.ArtifactIDs[index])
		if arguments.ArtifactIDs[index] == "" || len(arguments.ArtifactIDs[index]) > 80 {
			return agentProductionCanvasCommitArguments{}, errors.New("production canvas commit artifact identity is invalid")
		}
		if _, exists := seen[arguments.ArtifactIDs[index]]; exists {
			return agentProductionCanvasCommitArguments{}, errors.New("production canvas commit artifacts are duplicated")
		}
		seen[arguments.ArtifactIDs[index]] = struct{}{}
	}
	slices.Sort(arguments.ArtifactIDs)
	return arguments, nil
}

func (s *Service) coordinatePendingAgentProductionCanvasCommit(
	scope agentruntime.Scope,
	state agentruntime.RuntimeState,
	call *agentruntime.ToolCallDecision,
	record *model.AgentToolCall,
) (*AgentRuntimeProgress, error) {
	arguments, err := decodeAgentProductionCanvasCommitArguments(call.Arguments)
	if err != nil {
		return s.resolvePendingAgentToolFailureWithOutput(scope, state, call, "production_canvas_invalid", map[string]string{"reason": err.Error()})
	}
	plan, artifacts, resources, err := s.productionCanvasCommitFacts(scope, arguments)
	if err != nil {
		return s.resolvePendingAgentToolFailureWithOutput(scope, state, call, "production_canvas_invalid", map[string]string{"reason": err.Error()})
	}
	patch, bindings, err := buildProductionCanvasPatch(*plan, artifacts, resources)
	if err != nil {
		return s.resolvePendingAgentToolFailureWithOutput(scope, state, call, "production_canvas_invalid", map[string]string{"reason": err.Error()})
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
			return s.productionCanvasCommitCompletedToolProgress(scope, state, call)
		}
		record, _, err = s.frozenAgentToolCall(scope, call, agentruntime.ToolCallRunning, state.Configuration.ExecutionMode)
		if err != nil {
			return nil, err
		}
	}
	mutation, err := s.CommitCanvasMutation(&model.User{ID: scope.ActorUserID}, scope.CanvasID, CanvasMutationRequest{
		BaseRevision: arguments.BaseRevision, ClientMutationID: agentCanvasMutationID(record.IdempotencyKey), Patch: patch,
	})
	if err != nil {
		if failureCode := agentCanvasMutationFailureCode(err); failureCode != "" {
			if failureCode == "canvas_revision_conflict" {
				canvas, _, accessErr := s.canvasAccess(scope.ActorUserID, scope.CanvasID)
				if accessErr != nil {
					return nil, accessErr
				}
				return s.resolvePendingAgentToolFailureWithOutput(scope, state, call, failureCode, map[string]string{
					"currentRevision": strconv.FormatInt(canvas.Revision, 10),
				})
			}
			return s.resolvePendingAgentToolFailure(scope, state, call, failureCode)
		}
		return nil, err
	}
	artifactByID := make(map[string]model.AgentProductionArtifact, len(artifacts))
	for _, artifact := range artifacts {
		artifactByID[artifact.ID] = artifact
	}
	for _, binding := range bindings {
		artifact := artifactByID[binding.ArtifactID]
		if _, err := s.repo.CommitAgentProductionArtifactCanvasNode(scope, repository.ArtifactCanvasCommit{
			ArtifactID: artifact.ID, ExpectedStatus: artifact.Status, ExpectedAttempt: artifact.Attempt,
			CanvasNodeID: binding.NodeID, Now: time.Now().UTC(),
		}); err != nil {
			if errors.Is(err, repository.ErrAgentProductionArtifactConflict) {
				return s.resolvePendingAgentToolFailure(scope, state, call, "production_artifact_conflict")
			}
			return nil, err
		}
	}
	output, err := json.Marshal(agentProductionCanvasCommitResult{
		CanvasID: mutation.CanvasID, PlanKey: plan.PlanKey, PlanVersion: plan.Version,
		BaseRevision: arguments.BaseRevision, CommittedRevision: mutation.Revision,
		ClientMutationID: mutation.ClientMutationID, Bindings: bindings,
	})
	if err != nil {
		return nil, err
	}
	latest, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	if !samePendingStartedTool(latest, call) {
		return s.productionCanvasCommitCompletedToolProgress(scope, latest, call)
	}
	resolved, err := agentruntime.ResolveTool(latest, agentruntime.ToolResolution{
		ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion, Succeeded: true, Output: output,
	})
	if err != nil {
		return nil, err
	}
	progress, err := s.commitAgentRuntimeState(scope, latest, resolved)
	if err != nil {
		return nil, err
	}
	return s.productionCanvasCommitCompletedToolProgress(scope, progress.State, call)
}

func (s *Service) productionCanvasCommitFacts(scope agentruntime.Scope, arguments agentProductionCanvasCommitArguments) (*model.AgentProductionPlanVersion, []model.AgentProductionArtifact, map[string]model.Resource, error) {
	plan, err := s.repo.AgentProductionPlanVersionForScope(scope, arguments.PlanKey, arguments.PlanVersion)
	if err != nil {
		return nil, nil, nil, err
	}
	if plan.Status != model.AgentProductionPlanActive {
		return nil, nil, nil, errors.New("production canvas plan is not active")
	}
	artifacts, err := s.repo.AgentProductionArtifactsForVersion(scope, arguments.PlanKey, arguments.PlanVersion)
	if err != nil {
		return nil, nil, nil, err
	}
	actualIDs := make([]string, 0, len(artifacts))
	resources := make(map[string]model.Resource)
	for _, artifact := range artifacts {
		actualIDs = append(actualIDs, artifact.ID)
		if artifact.ResourceID == "" {
			continue
		}
		resource, loadErr := s.productionResourceForScope(scope, artifact.ResourceID)
		if loadErr != nil {
			return nil, nil, nil, loadErr
		}
		resources[resource.ID] = *resource
	}
	slices.Sort(actualIDs)
	if !slices.Equal(actualIDs, arguments.ArtifactIDs) {
		return nil, nil, nil, errors.New("production canvas artifacts do not match the plan")
	}
	return plan, artifacts, resources, nil
}

func (s *Service) productionCanvasCommitCompletedToolProgress(scope agentruntime.Scope, state agentruntime.RuntimeState, call *agentruntime.ToolCallDecision) (*AgentRuntimeProgress, error) {
	record, err := s.repo.AgentToolCallForScope(scope, call.ToolCallID, call.ActionVersion)
	if err != nil {
		return nil, err
	}
	if record.Status != agentruntime.ToolCallSucceeded && record.Status != agentruntime.ToolCallFailed {
		return nil, errors.New("production canvas commit completion facts conflict")
	}
	return s.agentRuntimeProgressForCurrentState(scope, state)
}
