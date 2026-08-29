package agentruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

const modelDecisionLimit = 128 * 1024
const finalMessageLimit = 32 * 1024

type DecisionKind string

const (
	DecisionFinal                DecisionKind = "final"
	DecisionToolCall             DecisionKind = "tool_call"
	DecisionClarificationRequest DecisionKind = "clarification_request"
)

type ToolName string

const (
	ToolSkillLoad        ToolName = "skill.load"
	ToolProductionPlan   ToolName = "production.plan"
	ToolProductionRender ToolName = "production.render"
	ToolCanvasCommit     ToolName = "canvas.commit"
	ToolMediaAssemble    ToolName = "media.assemble"
)

func (name ToolName) Valid() bool {
	return name.ValidForToolSchema(CurrentToolSchemaVersion)
}

func (name ToolName) Known() bool {
	return name.ValidForToolSchema(CurrentToolSchemaVersion) || name.ValidForToolSchema(LegacyToolSchemaVersion) || name.ValidForToolSchema(NextToolSchemaVersion)
}

func (name ToolName) ValidForToolSchema(toolSchemaVersion int) bool {
	switch toolSchemaVersion {
	case LegacyToolSchemaVersion:
		switch name {
		case ToolSkillLoad, ToolProductionPlan, ToolProductionRender, ToolCanvasCommit:
			return true
		default:
			return false
		}
	case CurrentToolSchemaVersion:
		switch name {
		case ToolSkillLoad, ToolSpecialistDelegate, ToolVisionAnalyze, ToolMediaGenerate, ToolCanvasProject:
			return true
		default:
			return false
		}
	case NextToolSchemaVersion:
		switch name {
		case ToolSkillLoad, ToolSpecialistDelegate, ToolVisionAnalyze, ToolMediaGenerate, ToolCanvasProject, ToolMediaAssemble:
			return true
		default:
			return false
		}
	default:
		return false
	}
}

type ModelDecision struct {
	Kind          DecisionKind           `json:"kind"`
	Final         *FinalDecision         `json:"final,omitempty"`
	ToolCall      *ToolCallDecision      `json:"toolCall,omitempty"`
	Clarification *ClarificationDecision `json:"clarification,omitempty"`
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

type ClarificationQuestionType string

const (
	ClarificationSingleChoice ClarificationQuestionType = "single_choice"
	ClarificationMultiChoice  ClarificationQuestionType = "multi_choice"
	ClarificationFreeText     ClarificationQuestionType = "free_text"
)

type ClarificationOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type ClarificationQuestion struct {
	ID                string                    `json:"id"`
	Prompt            string                    `json:"prompt"`
	Type              ClarificationQuestionType `json:"type"`
	Options           []ClarificationOption     `json:"options,omitempty"`
	AllowCustomAnswer bool                      `json:"allowCustomAnswer,omitempty"`
}

type ClarificationDecision struct {
	RequestID        string                  `json:"requestId"`
	Questions        []ClarificationQuestion `json:"questions"`
	ExpectedDelivery ExpectedDelivery        `json:"expectedDelivery"`
}

func ParseModelDecision(payload []byte) (ModelDecision, error) {
	return ParseModelDecisionForToolSchema(payload, CurrentToolSchemaVersion)
}

func ParseModelDecisionForToolSchema(payload []byte, toolSchemaVersion int) (ModelDecision, error) {
	if toolSchemaVersion != CurrentToolSchemaVersion && toolSchemaVersion != ProductionToolSchemaVersion && toolSchemaVersion != NextToolSchemaVersion {
		return ModelDecision{}, errors.New("agent tool schema version is invalid")
	}
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
	if err := decision.ValidateForToolSchema(toolSchemaVersion); err != nil {
		return ModelDecision{}, err
	}
	return decision, nil
}

func (decision ModelDecision) Validate() error {
	return decision.ValidateForToolSchema(CurrentToolSchemaVersion)
}

func (decision ModelDecision) ValidateForToolSchema(toolSchemaVersion int) error {
	if toolSchemaVersion != CurrentToolSchemaVersion && toolSchemaVersion != ProductionToolSchemaVersion && toolSchemaVersion != NextToolSchemaVersion {
		return errors.New("agent tool schema version is invalid")
	}
	switch decision.Kind {
	case DecisionFinal:
		if decision.Final == nil || decision.ToolCall != nil || decision.Clarification != nil {
			return errors.New("agent final decision payload is invalid")
		}
		decision.Final.Message = strings.TrimSpace(decision.Final.Message)
		if decision.Final.Message == "" || len(decision.Final.Message) > finalMessageLimit {
			return errors.New("agent final message is invalid")
		}
		return decision.Final.ExpectedDelivery.Validate()
	case DecisionToolCall:
		if decision.ToolCall == nil || decision.Final != nil || decision.Clarification != nil {
			return errors.New("agent tool decision payload is invalid")
		}
		call := decision.ToolCall
		call.ToolCallID = strings.TrimSpace(call.ToolCallID)
		if call.ToolCallID == "" || len(call.ToolCallID) > 120 || !call.ToolName.ValidForToolSchema(toolSchemaVersion) || call.ActionVersion < 1 {
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
	case DecisionClarificationRequest:
		if decision.Clarification == nil || decision.Final != nil || decision.ToolCall != nil {
			return errors.New("agent clarification decision payload is invalid")
		}
		return decision.Clarification.Validate()
	default:
		return errors.New("agent model decision kind is invalid")
	}
}

func (decision *ClarificationDecision) Validate() error {
	decision.RequestID = strings.TrimSpace(decision.RequestID)
	if !boundedDecisionText(decision.RequestID, 120) || len(decision.Questions) < 1 || len(decision.Questions) > 3 {
		return errors.New("agent clarification identity is invalid")
	}
	if err := decision.ExpectedDelivery.Validate(); err != nil {
		return err
	}
	questionIDs := make(map[string]struct{}, len(decision.Questions))
	for index := range decision.Questions {
		question := &decision.Questions[index]
		question.ID = strings.TrimSpace(question.ID)
		question.Prompt = strings.TrimSpace(question.Prompt)
		if !boundedDecisionText(question.ID, 120) || !boundedDecisionText(question.Prompt, 240) {
			return errors.New("agent clarification question is invalid")
		}
		if _, exists := questionIDs[question.ID]; exists {
			return errors.New("agent clarification question identity is duplicated")
		}
		questionIDs[question.ID] = struct{}{}
		if err := question.validateOptions(); err != nil {
			return err
		}
	}
	return nil
}

func (question *ClarificationQuestion) validateOptions() error {
	switch question.Type {
	case ClarificationSingleChoice, ClarificationMultiChoice:
		if len(question.Options) < 2 || len(question.Options) > 6 {
			return errors.New("agent clarification choice options are invalid")
		}
	case ClarificationFreeText:
		if len(question.Options) != 0 || question.AllowCustomAnswer {
			return errors.New("agent clarification free text options are invalid")
		}
		return nil
	default:
		return errors.New("agent clarification question type is invalid")
	}
	optionIDs := make(map[string]struct{}, len(question.Options))
	for index := range question.Options {
		option := &question.Options[index]
		option.ID = strings.TrimSpace(option.ID)
		option.Label = strings.TrimSpace(option.Label)
		if !boundedDecisionText(option.ID, 120) || !boundedDecisionText(option.Label, 80) {
			return errors.New("agent clarification option is invalid")
		}
		if _, exists := optionIDs[option.ID]; exists {
			return errors.New("agent clarification option identity is duplicated")
		}
		optionIDs[option.ID] = struct{}{}
	}
	return nil
}

func boundedDecisionText(value string, limit int) bool {
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= limit
}
