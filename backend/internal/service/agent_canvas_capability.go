package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

type agentCanvasApplyOpsCapabilityExecutor struct {
	service *Service
}

type agentCanvasOperationTranslation struct {
	patch               CanvasMutationPatch
	evidence            agentruntime.CanvasApplyOpsEvidence
	appliedOperationIDs []string
}

type agentCanvasEntitySet struct {
	order []string
	items map[string]json.RawMessage
}

type agentCanvasStoredDocument struct {
	Nodes       []json.RawMessage `json:"nodes"`
	Connections []json.RawMessage `json:"connections"`
}

func (executor agentCanvasApplyOpsCapabilityExecutor) Execute(ctx context.Context, scope agentruntime.Scope, call agentruntime.ToolCallDecision) (agentruntime.ToolExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return agentruntime.ToolExecutionResult{}, err
	}
	if executor.service == nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_unavailable", "canvas.apply_ops executor is unavailable")
	}
	if err := scope.Validate(); err != nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_scope_invalid", err.Error())
	}
	decoded, err := agentruntime.DecodeCapabilityArguments(call.ToolName, call.Arguments)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_arguments_invalid", "canvas.apply_ops arguments are invalid")
	}
	arguments, ok := decoded.(agentruntime.CanvasApplyOpsArguments)
	if !ok || call.ToolName != agentruntime.ToolCanvasApplyOps || call.ActionVersion != 1 {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_arguments_invalid", "canvas.apply_ops call identity is invalid")
	}
	if arguments.CanvasID != scope.CanvasID {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_scope_conflict", "canvas.apply_ops canvas scope is stale")
	}
	record, proposalHash, err := executor.service.authorizedCanvasApplyOpsRecord(scope, call)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, err
	}
	if record.Status == agentruntime.ToolCallSucceeded {
		result, decodeErr := agentruntime.DecodeCapabilityResult(agentruntime.ToolCanvasApplyOps, json.RawMessage(record.OutputJSON))
		if decodeErr != nil {
			return agentruntime.ToolExecutionResult{}, failAgentCapability("canvas_receipt_invalid", "canvas.apply_ops stored receipt is invalid")
		}
		return agentruntime.NewToolExecutionResult(agentruntime.ToolCanvasApplyOps, result)
	}
	canvas, _, err := executor.service.canvasAccess(scope.ActorUserID, scope.CanvasID)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "capability_ownership_forbidden", Err: err}
	}
	if canvas.ProjectID != scope.DomainProjectID {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("capability_scope_conflict", "canvas.apply_ops project scope is stale")
	}
	if canvas.Revision != arguments.BaseRevision {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("canvas_revision_conflict", "canvas.apply_ops base revision is stale")
	}
	translation, err := translateAgentCanvasOperations(canvas.PayloadJSON, arguments.Operations)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, err
	}
	expectedRevision := arguments.BaseRevision + 1
	result, err := agentruntime.NewToolExecutionResult(agentruntime.ToolCanvasApplyOps, agentruntime.CanvasApplyOpsResult{
		CanvasID: arguments.CanvasID, BaseRevision: arguments.BaseRevision, CommittedRevision: expectedRevision,
		ClientMutationID: arguments.ClientMutationID, ProposalHash: proposalHash,
		AppliedOperationIDs: translation.appliedOperationIDs, Evidence: translation.evidence,
	})
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "capability_result_invalid", Err: err}
	}
	mutation, err := executor.service.commitCanvasMutation(&model.User{ID: scope.ActorUserID}, scope.CanvasID, CanvasMutationRequest{
		BaseRevision: arguments.BaseRevision, ClientMutationID: arguments.ClientMutationID, Patch: translation.patch,
	}, &agentCanvasMutationCompletion{
		Scope: scope, ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion,
		ProposalHash: proposalHash, ToolReceiptJSON: string(result.Output),
	})
	if err != nil {
		var conflict *CanvasMutationConflictError
		switch {
		case errors.As(err, &conflict):
			return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: conflict.Code, Err: err}
		default:
			return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "canvas_commit_failed", Err: err}
		}
	}
	authoritative, err := executor.service.repo.CanvasProject(scope.CanvasID)
	if err != nil {
		return agentruntime.ToolExecutionResult{}, &agentCapabilityExecutionError{Code: "canvas_commit_unreadable", Err: err}
	}
	if mutation.ActorUserID != scope.ActorUserID || mutation.ClientMutationID != arguments.ClientMutationID ||
		mutation.Revision != expectedRevision || authoritative.Revision < mutation.Revision {
		return agentruntime.ToolExecutionResult{}, failAgentCapability("canvas_commit_conflict", "canvas.apply_ops committed revision could not be verified")
	}
	return result, nil
}

func (s *Service) authorizedCanvasApplyOpsRecord(scope agentruntime.Scope, call agentruntime.ToolCallDecision) (*model.AgentToolCall, string, error) {
	record, err := s.repo.AgentToolCallForScope(scope, call.ToolCallID, call.ActionVersion)
	if err != nil {
		return nil, "", &agentCapabilityExecutionError{Code: "canvas_proposal_missing", Err: err}
	}
	if record.ToolName != string(agentruntime.ToolCanvasApplyOps) || record.ToolName != string(call.ToolName) ||
		!equalAgentToolArguments(record.InputJSON, call.Arguments) || record.ApprovalProposalHash == "" {
		return nil, "", failAgentCapability("canvas_proposal_conflict", "canvas.apply_ops frozen proposal conflicts with the requested call")
	}
	if record.Status != agentruntime.ToolCallPending && record.Status != agentruntime.ToolCallRunning && record.Status != agentruntime.ToolCallSucceeded {
		return nil, "", failAgentCapability("canvas_proposal_conflict", "canvas.apply_ops proposal is not executable")
	}
	if record.ApprovalDecision != agentruntime.ToolApprovalApproved || record.ApprovalByUserID != scope.ActorUserID || record.ApprovalDecidedAt == nil {
		return nil, "", failAgentCapability("canvas_proposal_not_approved", "canvas.apply_ops proposal has not been approved by the current actor")
	}
	if err := validateStoredApprovalProposal(scope, record, record.ApprovalProposalHash, time.Now().UTC(), false); err != nil {
		return nil, "", &agentCapabilityExecutionError{Code: "canvas_proposal_invalid", Err: err}
	}
	return record, record.ApprovalProposalHash, nil
}

func translateAgentCanvasOperations(payload string, operations []agentruntime.CanvasOperation) (agentCanvasOperationTranslation, error) {
	var document agentCanvasStoredDocument
	if err := json.Unmarshal([]byte(payload), &document); err != nil || document.Nodes == nil || document.Connections == nil {
		return agentCanvasOperationTranslation{}, failAgentCapability("canvas_facts_invalid", "canvas.apply_ops stored canvas facts are invalid")
	}
	nodes, err := newAgentCanvasEntitySet(document.Nodes, "node")
	if err != nil {
		return agentCanvasOperationTranslation{}, err
	}
	connections, err := newAgentCanvasEntitySet(document.Connections, "connection")
	if err != nil {
		return agentCanvasOperationTranslation{}, err
	}
	translation := agentCanvasOperationTranslation{
		evidence: agentruntime.CanvasApplyOpsEvidence{
			AddedNodeIDs: []string{}, UpdatedNodeIDs: []string{}, DeletedNodeIDs: []string{},
			UpsertedConnectionIDs: []string{}, DeletedConnectionIDs: []string{}, SelectedNodeIDs: []string{},
		},
		appliedOperationIDs: make([]string, 0, len(operations)),
	}
	touchedNodes := make(map[string]struct{}, len(operations))
	touchedConnections := make(map[string]struct{}, len(operations))
	deletedNodes := make(map[string]struct{})
	deletedConnections := make(map[string]struct{})
	for _, operation := range operations {
		translation.appliedOperationIDs = append(translation.appliedOperationIDs, operation.OperationID)
		switch operation.Type {
		case agentruntime.CanvasOperationAddNode:
			if operation.Node == nil {
				return agentCanvasOperationTranslation{}, failAgentCapability("canvas_operation_invalid", "canvas.apply_ops add_node is missing node facts")
			}
			if _, exists := nodes.items[operation.Node.ID]; exists {
				return agentCanvasOperationTranslation{}, failAgentCapability("canvas_node_conflict", "canvas.apply_ops cannot add an existing node")
			}
			if err := claimAgentCanvasIdentity(touchedNodes, operation.Node.ID, "node"); err != nil {
				return agentCanvasOperationTranslation{}, err
			}
			raw, marshalErr := json.Marshal(operation.Node)
			if marshalErr != nil {
				return agentCanvasOperationTranslation{}, &agentCapabilityExecutionError{Code: "canvas_operation_invalid", Err: marshalErr}
			}
			nodes.put(operation.Node.ID, raw)
			translation.patch.UpsertNodes = append(translation.patch.UpsertNodes, raw)
			translation.evidence.AddedNodeIDs = append(translation.evidence.AddedNodeIDs, operation.Node.ID)
		case agentruntime.CanvasOperationUpdateNode:
			current, exists := nodes.items[operation.NodeID]
			if !exists {
				return agentCanvasOperationTranslation{}, failAgentCapability("canvas_node_missing", "canvas.apply_ops cannot update a missing node")
			}
			if err := claimAgentCanvasIdentity(touchedNodes, operation.NodeID, "node"); err != nil {
				return agentCanvasOperationTranslation{}, err
			}
			updated, updateErr := applyAgentCanvasNodePatch(current, operation.NodeID, operation.Patch)
			if updateErr != nil {
				return agentCanvasOperationTranslation{}, updateErr
			}
			nodes.put(operation.NodeID, updated)
			translation.patch.UpsertNodes = append(translation.patch.UpsertNodes, updated)
			translation.evidence.UpdatedNodeIDs = append(translation.evidence.UpdatedNodeIDs, operation.NodeID)
		case agentruntime.CanvasOperationDeleteNode:
			if _, exists := nodes.items[operation.NodeID]; !exists {
				return agentCanvasOperationTranslation{}, failAgentCapability("canvas_node_missing", "canvas.apply_ops cannot delete a missing node")
			}
			if err := claimAgentCanvasIdentity(touchedNodes, operation.NodeID, "node"); err != nil {
				return agentCanvasOperationTranslation{}, err
			}
			nodes.remove(operation.NodeID)
			deletedNodes[operation.NodeID] = struct{}{}
			translation.patch.DeleteNodeIDs = append(translation.patch.DeleteNodeIDs, operation.NodeID)
			translation.evidence.DeletedNodeIDs = append(translation.evidence.DeletedNodeIDs, operation.NodeID)
		case agentruntime.CanvasOperationConnectNodes:
			if operation.Connection == nil {
				return agentCanvasOperationTranslation{}, failAgentCapability("canvas_operation_invalid", "canvas.apply_ops connect_nodes is missing connection facts")
			}
			connection := operation.Connection
			if _, exists := connections.items[connection.ID]; exists {
				return agentCanvasOperationTranslation{}, failAgentCapability("canvas_connection_conflict", "canvas.apply_ops cannot add an existing connection")
			}
			if err := claimAgentCanvasIdentity(touchedConnections, connection.ID, "connection"); err != nil {
				return agentCanvasOperationTranslation{}, err
			}
			if err := validateAgentCanvasConnectionHandles(nodes, *connection); err != nil {
				return agentCanvasOperationTranslation{}, err
			}
			raw, marshalErr := json.Marshal(connection)
			if marshalErr != nil {
				return agentCanvasOperationTranslation{}, &agentCapabilityExecutionError{Code: "canvas_operation_invalid", Err: marshalErr}
			}
			connections.put(connection.ID, raw)
			translation.patch.UpsertConnections = append(translation.patch.UpsertConnections, raw)
			translation.evidence.UpsertedConnectionIDs = append(translation.evidence.UpsertedConnectionIDs, connection.ID)
		case agentruntime.CanvasOperationDeleteConnections:
			for _, connectionID := range operation.ConnectionIDs {
				if _, exists := connections.items[connectionID]; !exists {
					return agentCanvasOperationTranslation{}, failAgentCapability("canvas_connection_missing", "canvas.apply_ops cannot delete a missing connection")
				}
				if err := claimAgentCanvasIdentity(touchedConnections, connectionID, "connection"); err != nil {
					return agentCanvasOperationTranslation{}, err
				}
				connections.remove(connectionID)
				deletedConnections[connectionID] = struct{}{}
				translation.patch.DeleteConnectionIDs = append(translation.patch.DeleteConnectionIDs, connectionID)
				translation.evidence.DeletedConnectionIDs = append(translation.evidence.DeletedConnectionIDs, connectionID)
			}
		case agentruntime.CanvasOperationSetViewport:
			if operation.Viewport == nil || translation.evidence.ViewportApplied {
				return agentCanvasOperationTranslation{}, failAgentCapability("canvas_operation_conflict", "canvas.apply_ops viewport operation is duplicated or incomplete")
			}
			translation.patch.Document = &CanvasDocumentPatch{Viewport: &CanvasViewportPatch{X: operation.Viewport.X, Y: operation.Viewport.Y, K: operation.Viewport.Zoom}}
			translation.evidence.ViewportApplied = true
		case agentruntime.CanvasOperationSelectNodes:
			if len(translation.evidence.SelectedNodeIDs) > 0 {
				return agentCanvasOperationTranslation{}, failAgentCapability("canvas_operation_conflict", "canvas.apply_ops selection operation is duplicated")
			}
			for _, nodeID := range operation.NodeIDs {
				if _, exists := nodes.items[nodeID]; !exists {
					return agentCanvasOperationTranslation{}, failAgentCapability("canvas_selection_stale", "canvas.apply_ops selected node no longer exists")
				}
			}
			translation.evidence.SelectedNodeIDs = append([]string{}, operation.NodeIDs...)
		default:
			return agentCanvasOperationTranslation{}, failAgentCapability("canvas_operation_invalid", "canvas.apply_ops operation type is unsupported")
		}
	}
	for _, connectionID := range append([]string{}, connections.order...) {
		raw, exists := connections.items[connectionID]
		if !exists {
			continue
		}
		var identity agentCanvasConnectionIdentity
		if json.Unmarshal(raw, &identity) != nil {
			return agentCanvasOperationTranslation{}, failAgentCapability("canvas_facts_invalid", "canvas.apply_ops stored connection facts are invalid")
		}
		_, fromDeleted := deletedNodes[identity.FromNodeID]
		_, toDeleted := deletedNodes[identity.ToNodeID]
		if !fromDeleted && !toDeleted {
			if err := validateAgentCanvasConnectionHandles(nodes, agentruntime.CanvasConnectionInput{
				ID: identity.ID, FromNodeID: identity.FromNodeID, ToNodeID: identity.ToNodeID,
				FromHandleID: identity.FromHandleID, ToHandleID: identity.ToHandleID,
			}); err != nil {
				return agentCanvasOperationTranslation{}, err
			}
			continue
		}
		if _, addedByProposal := touchedConnections[connectionID]; addedByProposal {
			return agentCanvasOperationTranslation{}, failAgentCapability("canvas_operation_conflict", "canvas.apply_ops cannot add a connection whose endpoint is deleted in the same proposal")
		}
		connections.remove(connectionID)
		if _, alreadyDeleted := deletedConnections[connectionID]; !alreadyDeleted {
			translation.patch.DeleteConnectionIDs = append(translation.patch.DeleteConnectionIDs, connectionID)
			translation.evidence.DeletedConnectionIDs = append(translation.evidence.DeletedConnectionIDs, connectionID)
			deletedConnections[connectionID] = struct{}{}
		}
	}
	for _, nodeID := range translation.evidence.SelectedNodeIDs {
		if _, exists := nodes.items[nodeID]; !exists {
			return agentCanvasOperationTranslation{}, failAgentCapability("canvas_selection_stale", "canvas.apply_ops selected node does not exist in the final canvas state")
		}
	}
	if len(translation.patch.UpsertNodes)+len(translation.patch.DeleteNodeIDs)+len(translation.patch.UpsertConnections)+len(translation.patch.DeleteConnectionIDs) == 0 && translation.patch.Document == nil {
		return agentCanvasOperationTranslation{}, failAgentCapability("canvas_operation_not_durable", "canvas.apply_ops requires at least one durable canvas operation")
	}
	return translation, nil
}

func newAgentCanvasEntitySet(rawEntities []json.RawMessage, label string) (*agentCanvasEntitySet, error) {
	set := &agentCanvasEntitySet{order: make([]string, 0, len(rawEntities)), items: make(map[string]json.RawMessage, len(rawEntities))}
	for _, raw := range rawEntities {
		id, err := rawEntityID(raw)
		if err != nil {
			return nil, failAgentCapability("canvas_facts_invalid", fmt.Sprintf("canvas.apply_ops stored %s identity is invalid", label))
		}
		if _, duplicate := set.items[id]; duplicate {
			return nil, failAgentCapability("canvas_facts_invalid", fmt.Sprintf("canvas.apply_ops stored %s identity is duplicated", label))
		}
		set.order = append(set.order, id)
		set.items[id] = append(json.RawMessage(nil), raw...)
	}
	return set, nil
}

func (set *agentCanvasEntitySet) put(id string, raw json.RawMessage) {
	if _, exists := set.items[id]; !exists {
		set.order = append(set.order, id)
	}
	set.items[id] = append(json.RawMessage(nil), raw...)
}

func (set *agentCanvasEntitySet) remove(id string) {
	delete(set.items, id)
}

func claimAgentCanvasIdentity(seen map[string]struct{}, id string, label string) error {
	if _, exists := seen[id]; exists {
		return failAgentCapability("canvas_operation_conflict", fmt.Sprintf("canvas.apply_ops %s is changed more than once", label))
	}
	seen[id] = struct{}{}
	return nil
}

func applyAgentCanvasNodePatch(current json.RawMessage, expectedNodeID string, patch json.RawMessage) (json.RawMessage, error) {
	var currentObject map[string]json.RawMessage
	var patchObject map[string]json.RawMessage
	if json.Unmarshal(current, &currentObject) != nil || json.Unmarshal(patch, &patchObject) != nil {
		return nil, failAgentCapability("canvas_node_invalid", "canvas.apply_ops node patch is invalid")
	}
	allowed := map[string]struct{}{"type": {}, "title": {}, "position": {}, "width": {}, "height": {}, "metadata": {}}
	for key, value := range patchObject {
		if _, ok := allowed[key]; !ok || len(bytes.TrimSpace(value)) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, failAgentCapability("canvas_node_patch_forbidden", "canvas.apply_ops node patch contains a forbidden field")
		}
		currentObject[key] = append(json.RawMessage(nil), value...)
	}
	encoded, err := json.Marshal(currentObject)
	if err != nil {
		return nil, &agentCapabilityExecutionError{Code: "canvas_node_invalid", Err: err}
	}
	if err := validateAgentCanvasNode(encoded, expectedNodeID); err != nil {
		return nil, err
	}
	return encoded, nil
}

func validateAgentCanvasNode(raw json.RawMessage, expectedNodeID string) error {
	var node struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Title    string `json:"title"`
		Position *struct {
			X *float64 `json:"x"`
			Y *float64 `json:"y"`
		} `json:"position"`
		Width    *float64        `json:"width"`
		Height   *float64        `json:"height"`
		Metadata json.RawMessage `json:"metadata"`
	}
	if json.Unmarshal(raw, &node) != nil || node.ID != expectedNodeID || strings.TrimSpace(node.Type) == "" || strings.TrimSpace(node.Title) == "" ||
		node.Position == nil || node.Position.X == nil || node.Position.Y == nil || node.Width == nil || node.Height == nil ||
		!validCanvasViewportPatch(CanvasViewportPatch{X: *node.Position.X, Y: *node.Position.Y, K: 1}) || *node.Width < 1 || *node.Width > 20_000 || *node.Height < 1 || *node.Height > 20_000 ||
		len(node.Metadata) == 0 || !json.Valid(node.Metadata) || bytes.TrimSpace(node.Metadata)[0] != '{' {
		return failAgentCapability("canvas_node_invalid", "canvas.apply_ops node facts are invalid")
	}
	return nil
}

type agentCanvasConnectionIdentity struct {
	ID           string `json:"id"`
	FromNodeID   string `json:"fromNodeId"`
	ToNodeID     string `json:"toNodeId"`
	FromHandleID string `json:"fromHandleId"`
	ToHandleID   string `json:"toHandleId"`
}

func validateAgentCanvasConnectionHandles(nodes *agentCanvasEntitySet, connection agentruntime.CanvasConnectionInput) error {
	fromNode, fromExists := nodes.items[connection.FromNodeID]
	toNode, toExists := nodes.items[connection.ToNodeID]
	if !fromExists || !toExists {
		return failAgentCapability("canvas_connection_endpoint_missing", "canvas.apply_ops connection endpoint no longer exists")
	}
	if err := validateAgentCanvasHandle(fromNode, connection.FromHandleID); err != nil {
		return err
	}
	if err := validateAgentCanvasHandle(toNode, connection.ToHandleID); err != nil {
		return err
	}
	return nil
}

func validateAgentCanvasHandle(node json.RawMessage, handleID string) error {
	handleID = strings.TrimSpace(handleID)
	if handleID == "" {
		return nil
	}
	var facts struct {
		Type     string `json:"type"`
		Metadata struct {
			Storyboard struct {
				Rows []struct {
					ID string `json:"id"`
				} `json:"rows"`
			} `json:"storyboard"`
		} `json:"metadata"`
	}
	if json.Unmarshal(node, &facts) != nil || facts.Type != "script" {
		return failAgentCapability("canvas_handle_invalid", "canvas.apply_ops handle does not belong to the endpoint node")
	}
	if handleID == "storyboard:context" {
		return nil
	}
	if !strings.HasPrefix(handleID, "row:") || len(handleID) == len("row:") {
		return failAgentCapability("canvas_handle_invalid", "canvas.apply_ops handle is unknown")
	}
	rowID := strings.TrimPrefix(handleID, "row:")
	for _, row := range facts.Metadata.Storyboard.Rows {
		if row.ID == rowID {
			return nil
		}
	}
	return failAgentCapability("canvas_handle_invalid", "canvas.apply_ops storyboard row handle is stale")
}

var _ AgentCapabilityExecutor = agentCanvasApplyOpsCapabilityExecutor{}
