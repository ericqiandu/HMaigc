package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

var (
	errAgentCanvasProjectionRevisionNotApproved = errors.New("agent canvas projection revision is not approved")
	errAgentCanvasProjectionRevisionStale       = errors.New("agent canvas projection revision is stale")
	errAgentCanvasProjectionLineageInvalid      = errors.New("agent canvas projection lineage is invalid")
)

type agentCanvasProjectionResult struct {
	CanvasID          string                         `json:"canvasId"`
	BaseRevision      int64                          `json:"baseRevision"`
	CommittedRevision int64                          `json:"committedRevision"`
	ClientMutationID  string                         `json:"clientMutationId"`
	Bindings          []agentCanvasProjectionBinding `json:"bindings"`
}

func decodeAgentCanvasProjectionResult(payload []byte) (agentCanvasProjectionResult, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var result agentCanvasProjectionResult
	if err := decoder.Decode(&result); err != nil {
		return agentCanvasProjectionResult{}, errAgentCanvasProjectionInvalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) ||
		strings.TrimSpace(result.CanvasID) != result.CanvasID || result.CanvasID == "" ||
		result.BaseRevision < 0 || result.CommittedRevision != result.BaseRevision+1 ||
		strings.TrimSpace(result.ClientMutationID) != result.ClientMutationID || result.ClientMutationID == "" ||
		len(result.Bindings) == 0 || len(result.Bindings) > 64 {
		return agentCanvasProjectionResult{}, errAgentCanvasProjectionInvalid
	}
	seenArtifacts := make(map[string]struct{}, len(result.Bindings))
	seenNodes := make(map[string]struct{}, len(result.Bindings))
	seenProjections := make(map[string]struct{}, len(result.Bindings))
	for _, binding := range result.Bindings {
		if !validStoredText(binding.ArtifactID, 80) || !validStoredText(binding.RevisionID, 80) ||
			!validStoredText(binding.NodeID, 120) || !validStoredText(binding.ProjectionID, 120) {
			return agentCanvasProjectionResult{}, errAgentCanvasProjectionInvalid
		}
		if _, duplicate := seenArtifacts[binding.ArtifactID]; duplicate {
			return agentCanvasProjectionResult{}, errAgentCanvasProjectionInvalid
		}
		if _, duplicate := seenNodes[binding.NodeID]; duplicate {
			return agentCanvasProjectionResult{}, errAgentCanvasProjectionInvalid
		}
		if _, duplicate := seenProjections[binding.ProjectionID]; duplicate {
			return agentCanvasProjectionResult{}, errAgentCanvasProjectionInvalid
		}
		seenArtifacts[binding.ArtifactID] = struct{}{}
		seenNodes[binding.NodeID] = struct{}{}
		seenProjections[binding.ProjectionID] = struct{}{}
	}
	return result, nil
}

func (s *Service) coordinatePendingAgentCanvasProjection(
	scope agentruntime.Scope,
	state agentruntime.RuntimeState,
	call *agentruntime.ToolCallDecision,
	record *model.AgentToolCall,
) (*AgentRuntimeProgress, error) {
	arguments, err := decodeCanvasProjectArguments(call.Arguments)
	if err != nil || !arguments.ExpectedDelivery.Equal(call.ExpectedDelivery) ||
		arguments.ExpectedDelivery.TargetCanvasID != scope.CanvasID {
		return s.resolvePendingAgentToolFailureWithOutput(scope, state, call, "canvas_projection_invalid", map[string]string{
			"reason": errAgentCanvasProjectionInvalid.Error(),
		})
	}
	revisions, resources, mediaArguments, err := s.agentCanvasProjectionFacts(scope, arguments)
	if err != nil {
		failureCode, _ := agentCanvasProjectionFailureDetails(err)
		switch {
		case errors.Is(err, errAgentCanvasProjectionRevisionNotApproved):
			failureCode = "canvas_projection_revision_not_approved"
		case errors.Is(err, errAgentCanvasProjectionRevisionStale):
			failureCode = "canvas_projection_revision_stale"
		case errors.Is(err, errAgentCanvasProjectionLineageInvalid):
			failureCode = "canvas_projection_lineage_invalid"
		}
		return s.resolvePendingAgentToolFailureWithOutput(scope, state, call, failureCode, map[string]string{"reason": err.Error()})
	}
	patch, bindings, err := buildAgentCanvasProjectionPatch(scope.CanvasID, revisions, resources, mediaArguments)
	if err != nil {
		failureCode, _ := agentCanvasProjectionFailureDetails(err)
		return s.resolvePendingAgentToolFailureWithOutput(scope, state, call, failureCode, map[string]string{"reason": err.Error()})
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
			return s.agentCanvasProjectionCompletedToolProgress(scope, state, call)
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
		failureCode := agentCanvasMutationFailureCode(err)
		if failureCode == "" {
			return nil, err
		}
		output := map[string]string{"reason": err.Error()}
		if failureCode == "canvas_revision_conflict" {
			canvas, _, accessErr := s.canvasAccess(scope.ActorUserID, scope.CanvasID)
			if accessErr != nil {
				return nil, accessErr
			}
			output["currentRevision"] = strconv.FormatInt(canvas.Revision, 10)
		}
		return s.resolvePendingAgentToolFailureWithOutput(scope, state, call, failureCode, output)
	}
	output, err := json.Marshal(agentCanvasProjectionResult{
		CanvasID: mutation.CanvasID, BaseRevision: arguments.BaseRevision, CommittedRevision: mutation.Revision,
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
		return s.agentCanvasProjectionCompletedToolProgress(scope, latest, call)
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
	return s.agentCanvasProjectionCompletedToolProgress(scope, progress.State, call)
}

func (s *Service) agentCanvasProjectionFacts(
	scope agentruntime.Scope,
	arguments CanvasProjectArguments,
) ([]model.AgentArtifactRevision, map[string]model.Resource, map[agentruntime.ArtifactRevisionRef]agentMediaGenerationArguments, error) {
	approved, err := s.repo.ApprovedArtifactRevisionIDsForScope(scope)
	if err != nil {
		return nil, nil, nil, err
	}
	calls, err := s.repo.AgentToolCallsForScope(scope)
	if err != nil {
		return nil, nil, nil, err
	}
	mediaByRevision := make(map[agentruntime.ArtifactRevisionRef]agentMediaGenerationArguments)
	for _, storedCall := range calls {
		if storedCall.Status != agentruntime.ToolCallSucceeded || storedCall.ToolName != string(agentruntime.ToolMediaGenerate) {
			continue
		}
		frozen, decodeErr := decodeFrozenAgentMediaGenerationArguments(json.RawMessage(storedCall.InputJSON))
		if decodeErr != nil {
			return nil, nil, nil, errors.Join(errAgentCanvasProjectionLineageInvalid, decodeErr)
		}
		result, decodeErr := agentruntime.DecodeMediaGenerationToolResult([]byte(storedCall.OutputJSON))
		if decodeErr != nil {
			return nil, nil, nil, errors.Join(errAgentCanvasProjectionLineageInvalid, decodeErr)
		}
		for _, reference := range result.Candidates {
			if _, duplicate := mediaByRevision[reference]; duplicate {
				return nil, nil, nil, errAgentCanvasProjectionLineageInvalid
			}
			mediaByRevision[reference] = frozen
		}
	}
	revisions := make([]model.AgentArtifactRevision, 0, len(arguments.ArtifactRevisions))
	resources := make(map[string]model.Resource)
	selectedMedia := make(map[agentruntime.ArtifactRevisionRef]agentMediaGenerationArguments)
	for _, reference := range arguments.ArtifactRevisions {
		if _, ok := approved[reference.RevisionID]; !ok {
			return nil, nil, nil, errAgentCanvasProjectionRevisionNotApproved
		}
		revision, loadErr := s.repo.ArtifactRevisionForArtifactInScope(scope, reference.ArtifactID, reference.RevisionID)
		if loadErr != nil {
			return nil, nil, nil, errors.Join(errAgentCanvasProjectionInvalid, loadErr)
		}
		head, loadErr := s.repo.ArtifactHeadRevisionForScope(scope, reference.ArtifactID)
		if loadErr != nil || head.ID != revision.ID || revision.LifecycleStatus == model.AgentArtifactRevisionStale {
			return nil, nil, nil, errors.Join(errAgentCanvasProjectionRevisionStale, loadErr)
		}
		if revision.ResourceID != "" {
			resource, resourceErr := s.productionResourceForScope(scope, revision.ResourceID)
			if resourceErr != nil {
				return nil, nil, nil, resourceErr
			}
			resources[resource.ID] = *resource
		}
		if revision.Kind == "media_candidate" {
			media, exists := mediaByRevision[reference]
			if !exists {
				return nil, nil, nil, errAgentCanvasProjectionLineageInvalid
			}
			selectedMedia[reference] = media
		}
		revisions = append(revisions, *revision)
	}
	return revisions, resources, selectedMedia, nil
}

func (s *Service) agentCanvasProjectionCompletedToolProgress(
	scope agentruntime.Scope,
	state agentruntime.RuntimeState,
	call *agentruntime.ToolCallDecision,
) (*AgentRuntimeProgress, error) {
	record, err := s.repo.AgentToolCallForScope(scope, call.ToolCallID, call.ActionVersion)
	if err != nil {
		return nil, err
	}
	if record.Status != agentruntime.ToolCallSucceeded && record.Status != agentruntime.ToolCallFailed {
		return nil, errors.New("agent canvas projection completion facts conflict")
	}
	return s.agentRuntimeProgressForCurrentState(scope, state)
}
