package agentruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

const modelDecisionLimit = 128 * 1024
const finalMessageLimit = 32 * 1024

type DecisionKind string

const (
	DecisionFinal    DecisionKind = "final"
	DecisionToolCall DecisionKind = "tool_call"
)

type ToolName string

const (
	ToolSkillLoad        ToolName = "skill.load"
	ToolProductionPlan   ToolName = "production.plan"
	ToolProductionRender ToolName = "production.render"
	ToolCanvasCommit     ToolName = "canvas.commit"
)

func (name ToolName) Valid() bool {
	switch name {
	case ToolSkillLoad, ToolProductionPlan, ToolProductionRender, ToolCanvasCommit:
		return true
	default:
		return false
	}
}

type ModelDecision struct {
	Kind     DecisionKind      `json:"kind"`
	Final    *FinalDecision    `json:"final,omitempty"`
	ToolCall *ToolCallDecision `json:"toolCall,omitempty"`
}

type FinalDecision struct {
	Message          string           `json:"message"`
	ExpectedDelivery ExpectedDelivery `json:"expectedDelivery"`
}

type ToolCallDecision struct {
	ToolCallID       string           `json:"toolCallId"`
	ToolName         ToolName         `json:"toolName"`
	ActionVersion    int              `json:"actionVersion"`
	Arguments        json.RawMessage  `json:"arguments"`
	ExpectedDelivery ExpectedDelivery `json:"expectedDelivery"`
}

func ParseModelDecision(payload []byte) (ModelDecision, error) {
	if len(payload) == 0 || len(payload) > modelDecisionLimit {
		return ModelDecision{}, errors.New("agent model decision size is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var decision ModelDecision
	if err := decoder.Decode(&decision); err != nil {
		return ModelDecision{}, errors.New("agent model decision json is invalid")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ModelDecision{}, errors.New("agent model decision must contain one json document")
	}
	if err := decision.Validate(); err != nil {
		return ModelDecision{}, err
	}
	if decision.ToolCall != nil && !decision.ToolCall.ToolName.Valid() {
		return ModelDecision{}, errors.New("agent tool call identity is invalid")
	}
	return decision, nil
}

func (decision ModelDecision) Validate() error {
	switch decision.Kind {
	case DecisionFinal:
		if decision.Final == nil || decision.ToolCall != nil {
			return errors.New("agent final decision payload is invalid")
		}
		decision.Final.Message = strings.TrimSpace(decision.Final.Message)
		if decision.Final.Message == "" || len(decision.Final.Message) > finalMessageLimit {
			return errors.New("agent final message is invalid")
		}
		return decision.Final.ExpectedDelivery.Validate()
	case DecisionToolCall:
		if decision.ToolCall == nil || decision.Final != nil {
			return errors.New("agent tool decision payload is invalid")
		}
		call := decision.ToolCall
		call.ToolCallID = strings.TrimSpace(call.ToolCallID)
		if call.ToolCallID == "" || len(call.ToolCallID) > 120 || !call.ToolName.Valid() || call.ActionVersion < 1 {
			return errors.New("agent tool call identity is invalid")
		}
		if err := call.ExpectedDelivery.Validate(); err != nil {
			return err
		}
		arguments := bytes.TrimSpace(call.Arguments)
		if len(arguments) == 0 || bytes.Equal(arguments, []byte("null")) || arguments[0] != '{' || !json.Valid(arguments) {
			return errors.New("agent tool call arguments are invalid")
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, arguments); err != nil {
			return errors.New("agent tool call arguments are invalid")
		}
		call.Arguments = append(call.Arguments[:0], compact.Bytes()...)
		return nil
	default:
		return errors.New("agent model decision kind is invalid")
	}
}
