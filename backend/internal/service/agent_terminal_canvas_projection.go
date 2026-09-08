package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

const agentTerminalCanvasCommitAttempts = 3
const agentMediaSettlementPendingCode = "agent_media_settlement_pending"

type agentTerminalCanvasFailure struct {
	code    string
	details string
}

type agentTerminalMediaCanvasDisposition struct {
	targetNodeID string
	failure      agentTerminalCanvasFailure
}

type agentTerminalCanvasProjection struct {
	scope agentruntime.Scope
	state agentruntime.RuntimeState
}

type agentTerminalCanvasProjectionResult struct {
	revision int64
	changed  bool
}

func (s *Service) projectTerminalAgentRunCanvas(scope agentruntime.Scope, state agentruntime.RuntimeState) (agentTerminalCanvasProjectionResult, error) {
	if !agentRuntimeRunTerminal(state.Status) {
		return agentTerminalCanvasProjectionResult{}, nil
	}
	return s.projectTerminalAgentRunsCanvas([]agentTerminalCanvasProjection{{scope: scope, state: state}})
}

func (s *Service) projectTerminalAgentRunsCanvas(projections []agentTerminalCanvasProjection) (agentTerminalCanvasProjectionResult, error) {
	indexed, ordered, err := indexAgentTerminalCanvasProjections(projections)
	if err != nil || len(indexed) == 0 {
		return agentTerminalCanvasProjectionResult{}, err
	}
	anchor := ordered[0].scope
	for attempt := 0; attempt < agentTerminalCanvasCommitAttempts; attempt++ {
		canvas, _, err := s.canvasAccess(anchor.ActorUserID, anchor.CanvasID)
		if err != nil {
			return agentTerminalCanvasProjectionResult{}, err
		}
		updatedNodes, err := s.terminalAgentCanvasNodes(canvas.PayloadJSON, indexed)
		if err != nil {
			return agentTerminalCanvasProjectionResult{}, err
		}
		if len(updatedNodes) == 0 {
			return agentTerminalCanvasProjectionResult{revision: canvas.Revision}, nil
		}
		committed, err := s.CommitCanvasMutation(&model.User{ID: anchor.ActorUserID}, anchor.CanvasID, CanvasMutationRequest{
			BaseRevision:     canvas.Revision,
			ClientMutationID: terminalAgentCanvasMutationID(ordered),
			Patch:            CanvasMutationPatch{UpsertNodes: updatedNodes},
		})
		if err == nil {
			return agentTerminalCanvasProjectionResult{revision: committed.Revision, changed: true}, nil
		}
		var conflict *CanvasMutationConflictError
		if !errors.As(err, &conflict) || conflict.Code != "canvas_revision_conflict" {
			return agentTerminalCanvasProjectionResult{}, err
		}
	}
	return agentTerminalCanvasProjectionResult{}, errors.New("Agent 终态投影期间画布持续发生版本冲突")
}

func indexAgentTerminalCanvasProjections(
	projections []agentTerminalCanvasProjection,
) (map[string]agentTerminalCanvasProjection, []agentTerminalCanvasProjection, error) {
	indexed := make(map[string]agentTerminalCanvasProjection, len(projections))
	ordered := make([]agentTerminalCanvasProjection, 0, len(projections))
	var anchor *agentruntime.Scope
	for _, projection := range projections {
		if !agentRuntimeRunTerminal(projection.state.Status) {
			continue
		}
		if err := projection.scope.Validate(); err != nil {
			return nil, nil, err
		}
		if projection.state.StateVersion < 1 {
			return nil, nil, errors.New("Agent 终态投影缺少状态版本")
		}
		if anchor == nil {
			copied := projection.scope
			anchor = &copied
		} else if projection.scope.ActorUserID != anchor.ActorUserID || projection.scope.CanvasID != anchor.CanvasID ||
			projection.scope.TenantKind != anchor.TenantKind || projection.scope.TenantID != anchor.TenantID ||
			projection.scope.DomainProjectID != anchor.DomainProjectID {
			return nil, nil, errors.New("Agent 终态投影作用域不一致")
		}
		if existing, exists := indexed[projection.scope.RunID]; exists {
			if existing.scope.ThreadID != projection.scope.ThreadID || existing.state.StateVersion != projection.state.StateVersion || existing.state.Status != projection.state.Status {
				return nil, nil, errors.New("Agent 终态投影运行事实冲突")
			}
			continue
		}
		indexed[projection.scope.RunID] = projection
		ordered = append(ordered, projection)
	}
	sort.Slice(ordered, func(left int, right int) bool {
		return ordered[left].scope.RunID < ordered[right].scope.RunID
	})
	return indexed, ordered, nil
}

func (s *Service) terminalAgentCanvasNodes(
	payload string,
	projections map[string]agentTerminalCanvasProjection,
) ([]json.RawMessage, error) {
	trimmedPayload := bytes.TrimSpace([]byte(payload))
	if len(trimmedPayload) == 0 || trimmedPayload[0] != '{' {
		return nil, errors.New("Agent 终态投影读取到无效画布事实")
	}
	var document agentCanvasStoredDocument
	if err := json.Unmarshal(trimmedPayload, &document); err != nil {
		return nil, errors.New("Agent 终态投影读取到无效画布事实")
	}
	updated := make([]json.RawMessage, 0)
	type resolvedProjection struct {
		failure          agentTerminalCanvasFailure
		mediaDisposition *agentTerminalMediaCanvasDisposition
	}
	resolved := make(map[string]resolvedProjection, len(projections))
	for _, raw := range document.Nodes {
		var node struct {
			ID       string `json:"id"`
			Metadata struct {
				Status              string `json:"status"`
				AgentRunID          string `json:"agentRunId"`
				GenerationErrorCode string `json:"generationErrorCode"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal(raw, &node); err != nil || node.ID == "" {
			return nil, errors.New("Agent 终态投影读取到无效节点事实")
		}
		projection, exists := projections[node.Metadata.AgentRunID]
		if !exists {
			continue
		}
		isLoading := node.Metadata.Status == "loading"
		isPendingSettlement := node.Metadata.Status == "error" && node.Metadata.GenerationErrorCode == agentMediaSettlementPendingCode
		if !isLoading && !isPendingSettlement {
			continue
		}
		facts, ok := resolved[node.Metadata.AgentRunID]
		if !ok {
			failure, err := terminalAgentCanvasFailure(projection.state)
			if err != nil {
				return nil, err
			}
			mediaDisposition, err := s.terminalAgentMediaCanvasDisposition(projection.scope, projection.state)
			if err != nil {
				return nil, err
			}
			facts = resolvedProjection{failure: failure, mediaDisposition: mediaDisposition}
			resolved[node.Metadata.AgentRunID] = facts
		}
		failure := facts.failure
		if facts.mediaDisposition != nil && node.ID == facts.mediaDisposition.targetNodeID {
			failure = facts.mediaDisposition.failure
		} else if isPendingSettlement {
			continue
		}
		patch, err := json.Marshal(struct {
			Metadata map[string]json.RawMessage `json:"metadata"`
		}{Metadata: map[string]json.RawMessage{
			"status":              mustAgentTerminalJSON("error"),
			"generationErrorCode": mustAgentTerminalJSON(failure.code),
			"errorDetails":        mustAgentTerminalJSON(failure.details),
			"taskStage":           json.RawMessage("null"),
			"taskProgress":        json.RawMessage("null"),
		}})
		if err != nil {
			return nil, err
		}
		projected, err := applyAgentCanvasNodePatch(raw, node.ID, patch)
		if err != nil {
			return nil, err
		}
		currentCanonical, currentErr := canonicalAgentJSON(raw)
		projectedCanonical, projectedErr := canonicalAgentJSON(projected)
		if currentErr != nil || projectedErr != nil {
			return nil, errors.Join(errors.New("Agent 终态节点无法规范化"), currentErr, projectedErr)
		}
		if !bytes.Equal(currentCanonical, projectedCanonical) {
			updated = append(updated, projected)
		}
	}
	return updated, nil
}

func (s *Service) terminalAgentMediaCanvasDisposition(
	scope agentruntime.Scope,
	state agentruntime.RuntimeState,
) (*agentTerminalMediaCanvasDisposition, error) {
	if state.Status != agentruntime.RunCancelled || !state.PendingToolStarted || state.PendingToolCall == nil || state.PendingToolCall.ToolName != agentruntime.ToolMediaGenerate {
		if state.LastToolResult == nil || state.LastToolResult.Succeeded {
			return nil, nil
		}
		if state.LastToolResult.ErrorCode == "" {
			return nil, errors.New("Agent 终态媒体失败缺少错误码")
		}
		record, err := s.repo.AgentToolCallForScope(scope, state.LastToolResult.ToolCallID, state.LastToolResult.ActionVersion)
		if err != nil {
			return nil, fmt.Errorf("读取 Agent 终态工具事实: %w", err)
		}
		if record.ToolName != string(agentruntime.ToolMediaGenerate) {
			return nil, nil
		}
		if record.Status != agentruntime.ToolCallFailed || record.ErrorCode != state.LastToolResult.ErrorCode {
			return nil, errors.New("Agent 终态媒体失败与持久化工具事实冲突")
		}
		return terminalAgentMediaDispositionFromArguments(
			agentruntime.ToolMediaGenerate,
			json.RawMessage(record.InputJSON),
			agentTerminalCanvasFailure{
				code:    state.LastToolResult.ErrorCode,
				details: "Agent 媒体生成未完成，此节点可重新生成。",
			},
		)
	}
	return terminalAgentMediaDispositionFromArguments(
		state.PendingToolCall.ToolName,
		state.PendingToolCall.Arguments,
		agentTerminalCanvasFailure{
			code:    agentMediaSettlementPendingCode,
			details: "Agent 已停止，已提交的生成任务仍在处理或核对费用；完成后会自动回填，请勿重复提交。",
		},
	)
}

func terminalAgentMediaDispositionFromArguments(
	toolName agentruntime.ToolName,
	argumentsJSON json.RawMessage,
	failure agentTerminalCanvasFailure,
) (*agentTerminalMediaCanvasDisposition, error) {
	decoded, err := agentruntime.DecodeCapabilityArguments(toolName, argumentsJSON)
	if err != nil {
		return nil, fmt.Errorf("Agent 终态媒体参数无效: %w", err)
	}
	arguments, ok := decoded.(agentruntime.MediaGenerateArguments)
	if !ok || arguments.TargetCanvasNodeID == "" {
		return nil, errors.New("Agent 终态媒体目标节点事实缺失")
	}
	return &agentTerminalMediaCanvasDisposition{targetNodeID: arguments.TargetCanvasNodeID, failure: failure}, nil
}

func terminalAgentCanvasFailure(state agentruntime.RuntimeState) (agentTerminalCanvasFailure, error) {
	switch state.Status {
	case agentruntime.RunCancelled:
		return agentTerminalCanvasFailure{code: "agent_run_cancelled", details: "Agent 已停止，此节点未完成。"}, nil
	case agentruntime.RunFailed:
		if state.FailureCode == "" {
			return agentTerminalCanvasFailure{}, errors.New("Agent 失败终态缺少错误码")
		}
		return agentTerminalCanvasFailure{code: state.FailureCode, details: "Agent 运行失败，此节点未完成。"}, nil
	case agentruntime.RunSucceeded:
		return agentTerminalCanvasFailure{code: "agent_delivery_incomplete", details: "Agent 已结束，但此节点没有生成结果。"}, nil
	default:
		return agentTerminalCanvasFailure{}, errors.New("Agent 终态投影收到非终态运行事实")
	}
}

func terminalAgentCanvasMutationID(projections []agentTerminalCanvasProjection) string {
	input := bytes.NewBufferString("agent-terminal-canvas")
	for _, projection := range projections {
		_, _ = fmt.Fprintf(input, "\x00%s\x00%d", projection.scope.RunID, projection.state.StateVersion)
	}
	digest := sha256.Sum256(input.Bytes())
	return "agent-terminal-" + hex.EncodeToString(digest[:16])
}

func mustAgentTerminalJSON(value string) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}
