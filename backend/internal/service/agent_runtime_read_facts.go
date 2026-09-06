package service

import (
	"encoding/json"
	"errors"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

type agentRuntimeObservedToolFact struct {
	ToolCallID    string                `json:"toolCallId"`
	ToolName      agentruntime.ToolName `json:"toolName"`
	ActionVersion int                   `json:"actionVersion"`
	Arguments     json.RawMessage       `json:"arguments"`
	Output        json.RawMessage       `json:"output"`
}

// agentRuntimeObservedReadFacts reconstructs the successful read facts from the
// current mutation epoch. The latest successful write or paid capability starts
// a new epoch because all earlier read results may be stale after that action.
func agentRuntimeObservedReadFacts(
	calls []model.AgentToolCall,
	lastResult *agentruntime.ToolResult,
) ([]agentRuntimeObservedToolFact, error) {
	epochStart := 0
	for index, call := range calls {
		if call.Status != agentruntime.ToolCallSucceeded {
			continue
		}
		toolName := agentruntime.ToolName(strings.TrimSpace(call.ToolName))
		policy, known := agentruntime.ToolPolicyFor(toolName)
		if !known || policy.RiskLevel != call.RiskLevel {
			return nil, errors.New("agent observed read facts are invalid")
		}
		if call.RiskLevel != agentruntime.ToolRiskRead {
			epochStart = index + 1
		}
	}

	result := make([]agentRuntimeObservedToolFact, 0)
	indexByIdentity := make(map[string]int)
	for _, call := range calls[epochStart:] {
		if call.Status != agentruntime.ToolCallSucceeded || call.RiskLevel != agentruntime.ToolRiskRead {
			continue
		}
		toolName := agentruntime.ToolName(strings.TrimSpace(call.ToolName))
		policy, known := agentruntime.ToolPolicyFor(toolName)
		if !known || policy.RiskLevel != agentruntime.ToolRiskRead || strings.TrimSpace(call.ToolCallID) == "" || call.ActionVersion < 1 {
			return nil, errors.New("agent observed read facts are invalid")
		}
		if lastResult != nil && lastResult.Succeeded && lastResult.ToolCallID == call.ToolCallID && lastResult.ActionVersion == call.ActionVersion {
			continue
		}
		arguments, err := canonicalAgentJSON([]byte(call.InputJSON))
		if err != nil {
			return nil, errors.New("agent observed read facts are invalid")
		}
		output, err := canonicalAgentJSON([]byte(call.OutputJSON))
		if err != nil {
			return nil, errors.New("agent observed read facts are invalid")
		}
		fact := agentRuntimeObservedToolFact{
			ToolCallID: call.ToolCallID, ToolName: toolName, ActionVersion: call.ActionVersion,
			Arguments: json.RawMessage(arguments), Output: json.RawMessage(output),
		}
		identity := string(toolName) + "\x00" + string(arguments)
		if existing, found := indexByIdentity[identity]; found {
			result[existing] = fact
			continue
		}
		indexByIdentity[identity] = len(result)
		result = append(result, fact)
	}
	return result, nil
}

func validateAgentRuntimeObservedReadFacts(facts []agentRuntimeObservedToolFact) error {
	seen := make(map[string]struct{}, len(facts))
	for _, fact := range facts {
		policy, known := agentruntime.ToolPolicyFor(fact.ToolName)
		if !known || policy.RiskLevel != agentruntime.ToolRiskRead || strings.TrimSpace(fact.ToolCallID) == "" || fact.ActionVersion < 1 {
			return errors.New("agent observed read facts are invalid")
		}
		arguments, err := canonicalAgentJSON(fact.Arguments)
		if err != nil {
			return errors.New("agent observed read facts are invalid")
		}
		output, err := canonicalAgentJSON(fact.Output)
		if err != nil || string(output) != string(fact.Output) || string(arguments) != string(fact.Arguments) {
			return errors.New("agent observed read facts are invalid")
		}
		identity := string(fact.ToolName) + "\x00" + string(arguments)
		if _, duplicate := seen[identity]; duplicate {
			return errors.New("agent observed read facts are invalid")
		}
		seen[identity] = struct{}{}
	}
	return nil
}
