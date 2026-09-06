package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

const agentMediaResultCanvasCommitAttempts = 3

func (s *Service) validateAgentMediaTargetCanvasNode(scope agentruntime.Scope, nodeID string, mediaKind agentruntime.MediaKind) error {
	canvas, _, err := s.canvasAccess(scope.ActorUserID, scope.CanvasID)
	if err != nil {
		var accessError *AuthError
		if errors.As(err, &accessError) {
			return errors.Join(errAgentMediaTargetInvalid, err)
		}
		return err
	}
	if !agentCanvasProjectBelongsToScope(*canvas, scope) {
		return errAgentMediaTargetInvalid
	}
	var document agentCanvasStoredDocument
	if json.Unmarshal([]byte(canvas.PayloadJSON), &document) != nil || document.Nodes == nil {
		return errors.New("media.generate target canvas facts are invalid")
	}
	nodes, err := newAgentCanvasEntitySet(document.Nodes, "node")
	if err != nil {
		return err
	}
	current, exists := nodes.items[nodeID]
	if !exists {
		return errAgentMediaTargetInvalid
	}
	var identity struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(current, &identity) != nil {
		return errors.New("media.generate target canvas node facts are invalid")
	}
	if identity.Type != string(mediaKind) {
		return errAgentMediaTargetInvalid
	}
	return nil
}

func (s *Service) persistAgentMediaResultOnCanvas(
	scope agentruntime.Scope,
	call agentruntime.ToolCallDecision,
	arguments agentruntime.MediaGenerateArguments,
	task *model.Task,
	receipt agentruntime.MediaGenerateResult,
) error {
	if task == nil || task.ID != receipt.TaskID || task.UserID != scope.ActorUserID || task.ProjectID != scope.CanvasID ||
		task.Operation != agentMediaGenerationOperationForRun(scope.RunID) || task.Status != model.TaskStatusSucceeded ||
		task.BillingOrderID == "" || task.BillingOrderID != receipt.BillingOrderID || task.Capability != string(arguments.MediaKind) ||
		task.CompletedAt == nil || task.CreatedAt.IsZero() || task.UpdatedAt.IsZero() ||
		receipt.MediaKind != arguments.MediaKind || receipt.ClientRequestID != arguments.ClientRequestID || len(receipt.Resources) < 1 {
		return errors.New("media.generate successful task facts conflict with the target canvas binding")
	}
	prompt := strings.TrimSpace(task.Prompt)
	if prompt == "" {
		return errors.New("media.generate authoritative prompt is empty")
	}
	primary := receipt.Resources[0]
	if primary.Kind != arguments.MediaKind || primary.ResourceID == "" || primary.URL != "/api/resources/"+primary.ResourceID+"/file" {
		return errors.New("media.generate primary resource facts are invalid")
	}
	resource, err := s.agentResourceForScope(scope, primary.ResourceID)
	if err != nil || resource.Status != model.ResourceStatusReady || resource.Kind != string(arguments.MediaKind) {
		return errors.Join(errors.New("media.generate primary resource is unavailable in the approved tenant scope"), err)
	}
	clientMutationID := agentMediaResultCanvasMutationID(scope, call)
	for attempt := 0; attempt < agentMediaResultCanvasCommitAttempts; attempt++ {
		canvas, _, err := s.canvasAccess(scope.ActorUserID, scope.CanvasID)
		if err != nil {
			return err
		}
		updatedNode, changed, err := agentMediaResultBoundNode(
			canvas.PayloadJSON,
			arguments.TargetCanvasNodeID,
			arguments.MediaKind,
			prompt,
			*task,
			primary,
			*resource,
			call,
			arguments,
			scope.RunID,
		)
		if err != nil {
			return err
		}
		if !changed {
			return nil
		}
		_, err = s.CommitCanvasMutation(&model.User{ID: scope.ActorUserID}, scope.CanvasID, CanvasMutationRequest{
			BaseRevision:     canvas.Revision,
			ClientMutationID: clientMutationID,
			Patch:            CanvasMutationPatch{UpsertNodes: []json.RawMessage{updatedNode}},
		})
		if err == nil {
			return nil
		}
		var conflict *CanvasMutationConflictError
		if !errors.As(err, &conflict) || conflict.Code != "canvas_revision_conflict" {
			return err
		}
	}
	return errors.New("media.generate target canvas changed while persisting the successful result")
}

func agentMediaResultBoundNode(
	payload string,
	nodeID string,
	mediaKind agentruntime.MediaKind,
	prompt string,
	task model.Task,
	primary agentruntime.MediaGeneratedResourceResult,
	resource model.Resource,
	call agentruntime.ToolCallDecision,
	arguments agentruntime.MediaGenerateArguments,
	runID string,
) (json.RawMessage, bool, error) {
	var document agentCanvasStoredDocument
	if json.Unmarshal([]byte(payload), &document) != nil || document.Nodes == nil {
		return nil, false, errors.New("media.generate target canvas facts are invalid")
	}
	nodes, err := newAgentCanvasEntitySet(document.Nodes, "node")
	if err != nil {
		return nil, false, err
	}
	current, exists := nodes.items[nodeID]
	if !exists {
		return nil, false, errors.New("media.generate target canvas node is missing")
	}
	var facts struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(current, &facts) != nil || facts.Type != string(mediaKind) {
		return nil, false, errors.New("media.generate target canvas node kind conflicts with the generated media")
	}
	metadata := map[string]json.RawMessage{
		"errorDetails":            json.RawMessage("null"),
		"generationErrorCode":     json.RawMessage("null"),
		"failedPromptFingerprint": json.RawMessage("null"),
	}
	values := map[string]string{
		"prompt": prompt, "composerContent": prompt,
		"content": primary.URL, "storageKey": "resource:" + primary.ResourceID, "status": "success",
		"taskId": task.ID, "taskStatus": string(model.TaskStatusSucceeded), "taskStage": task.Stage,
		"taskCreatedAt": task.CreatedAt.UTC().Format(time.RFC3339Nano), "taskUpdatedAt": task.UpdatedAt.UTC().Format(time.RFC3339Nano),
		"mimeType": resource.MimeType,
	}
	for key, value := range values {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, false, err
		}
		metadata[key] = encoded
	}
	for key, value := range map[string]int64{"taskProgress": 100, "bytes": resource.Size} {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, false, err
		}
		metadata[key] = encoded
	}
	evidence, err := json.Marshal(struct {
		RunID           string `json:"runId"`
		ToolCallID      string `json:"toolCallId"`
		ActionVersion   int    `json:"actionVersion"`
		TaskID          string `json:"taskId"`
		BillingOrderID  string `json:"billingOrderId"`
		ResourceID      string `json:"resourceId"`
		ClientRequestID string `json:"clientRequestId"`
		ModelRecordID   string `json:"modelRecordId"`
		ModelKey        string `json:"modelKey"`
	}{
		RunID: runID, ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion,
		TaskID: task.ID, BillingOrderID: task.BillingOrderID, ResourceID: primary.ResourceID,
		ClientRequestID: arguments.ClientRequestID, ModelRecordID: arguments.ModelRecordID, ModelKey: arguments.ModelKey,
	})
	if err != nil {
		return nil, false, err
	}
	metadata["generationEvidence"] = evidence
	if resource.Width > 0 {
		metadata["naturalWidth"], err = json.Marshal(resource.Width)
		if err != nil {
			return nil, false, err
		}
	}
	if resource.Height > 0 {
		metadata["naturalHeight"], err = json.Marshal(resource.Height)
		if err != nil {
			return nil, false, err
		}
	}
	if resource.DurationMs > 0 {
		metadata["durationMs"], err = json.Marshal(resource.DurationMs)
		if err != nil {
			return nil, false, err
		}
	}
	patch, err := json.Marshal(struct {
		Metadata map[string]json.RawMessage `json:"metadata"`
	}{Metadata: metadata})
	if err != nil {
		return nil, false, err
	}
	updated, err := applyAgentCanvasNodePatch(current, nodeID, patch)
	if err != nil {
		return nil, false, err
	}
	currentCanonical, currentErr := canonicalAgentJSON(current)
	updatedCanonical, updatedErr := canonicalAgentJSON(updated)
	if currentErr != nil || updatedErr != nil {
		return nil, false, errors.Join(errors.New("media.generate target canvas node facts are invalid"), currentErr, updatedErr)
	}
	if bytes.Equal(currentCanonical, updatedCanonical) {
		return nil, false, nil
	}
	return updated, true, nil
}

func agentMediaResultCanvasMutationID(scope agentruntime.Scope, call agentruntime.ToolCallDecision) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("agent-media-result\x00%s\x00%s\x00%d", scope.RunID, call.ToolCallID, call.ActionVersion)))
	return "agent-media-result-" + hex.EncodeToString(digest[:16])
}
