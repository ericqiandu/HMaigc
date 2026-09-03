package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

const agentMediaPromptCanvasCommitAttempts = 3

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

func (s *Service) persistAgentMediaPromptOnCanvas(
	scope agentruntime.Scope,
	call agentruntime.ToolCallDecision,
	arguments agentruntime.MediaGenerateArguments,
	prompt string,
) error {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return errors.New("media.generate authoritative prompt is empty")
	}
	clientMutationID := agentMediaPromptCanvasMutationID(scope, call)
	for attempt := 0; attempt < agentMediaPromptCanvasCommitAttempts; attempt++ {
		canvas, _, err := s.canvasAccess(scope.ActorUserID, scope.CanvasID)
		if err != nil {
			return err
		}
		updatedNode, changed, err := agentMediaPromptBoundNode(canvas.PayloadJSON, arguments.TargetCanvasNodeID, arguments.MediaKind, prompt)
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
	return errors.New("media.generate target canvas changed while persisting the authoritative prompt")
}

func agentMediaPromptBoundNode(payload string, nodeID string, mediaKind agentruntime.MediaKind, prompt string) (json.RawMessage, bool, error) {
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
		Type     string `json:"type"`
		Metadata struct {
			Prompt          string  `json:"prompt"`
			ComposerContent *string `json:"composerContent"`
		} `json:"metadata"`
	}
	if json.Unmarshal(current, &facts) != nil || facts.Type != string(mediaKind) {
		return nil, false, errors.New("media.generate target canvas node kind conflicts with the generated media")
	}
	if facts.Metadata.Prompt == prompt && facts.Metadata.ComposerContent != nil && *facts.Metadata.ComposerContent == prompt {
		return nil, false, nil
	}
	patch, err := json.Marshal(map[string]map[string]string{
		"metadata": {"prompt": prompt, "composerContent": prompt},
	})
	if err != nil {
		return nil, false, err
	}
	updated, err := applyAgentCanvasNodePatch(current, nodeID, patch)
	return updated, err == nil, err
}

func agentMediaPromptCanvasMutationID(scope agentruntime.Scope, call agentruntime.ToolCallDecision) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("agent-media-prompt\x00%s\x00%s\x00%d", scope.RunID, call.ToolCallID, call.ActionVersion)))
	return "agent-media-prompt-" + hex.EncodeToString(digest[:16])
}
