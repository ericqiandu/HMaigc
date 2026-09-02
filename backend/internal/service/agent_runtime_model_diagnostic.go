package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

type agentRuntimeDecisionDiagnostic struct {
	ValidationError      string                               `json:"validationError"`
	StrictDecodeProblem  string                               `json:"strictDecodeProblem,omitempty"`
	DecisionKind         string                               `json:"decisionKind,omitempty"`
	ToolName             string                               `json:"toolName,omitempty"`
	DecisionKeys         []string                             `json:"decisionKeys,omitempty"`
	ToolCallKeys         []string                             `json:"toolCallKeys,omitempty"`
	ExpectedDeliveryKeys []string                             `json:"expectedDeliveryKeys,omitempty"`
	DeliveryKind         string                               `json:"deliveryKind,omitempty"`
	RequiredArtifacts    []string                             `json:"requiredArtifacts,omitempty"`
	CriterionFacts       []string                             `json:"criterionFacts,omitempty"`
	ArgumentKeys         []string                             `json:"argumentKeys,omitempty"`
	ParameterKeys        []string                             `json:"parameterKeys,omitempty"`
	Media                *agentRuntimeMediaDecisionDiagnostic `json:"media,omitempty"`
}

type agentRuntimeMediaDecisionDiagnostic struct {
	MediaKind                  string `json:"mediaKind,omitempty"`
	ModelRecordIDPresent       bool   `json:"modelRecordIdPresent"`
	ModelRecordIDNonEmpty      bool   `json:"modelRecordIdNonEmpty"`
	ModelKeyPresent            bool   `json:"modelKeyPresent"`
	ModelKeyNonEmpty           bool   `json:"modelKeyNonEmpty"`
	SourceResourceIDsPresent   bool   `json:"sourceResourceIdsPresent"`
	SourceResourceCount        int    `json:"sourceResourceCount"`
	TargetCanvasNodeIDPresent  bool   `json:"targetCanvasNodeIdPresent"`
	TargetCanvasNodeIDNonEmpty bool   `json:"targetCanvasNodeIdNonEmpty"`
	ClientRequestIDPresent     bool   `json:"clientRequestIdPresent"`
	ClientRequestIDNonEmpty    bool   `json:"clientRequestIdNonEmpty"`
	ParametersPresent          bool   `json:"parametersPresent"`
	ParametersObject           bool   `json:"parametersObject"`
}

func buildAgentRuntimeDecisionDiagnostic(payload []byte, validationErr error) agentRuntimeDecisionDiagnostic {
	diagnostic := agentRuntimeDecisionDiagnostic{
		ValidationError:     validationErr.Error(),
		StrictDecodeProblem: strictAgentRuntimeDecisionDecodeProblem(payload),
	}
	var decision map[string]json.RawMessage
	if json.Unmarshal(payload, &decision) != nil {
		return diagnostic
	}
	diagnostic.DecisionKeys = sortedStructuralKeys(decision, nil)
	diagnostic.DecisionKind, _ = rawJSONString(decision["kind"])

	var toolCall map[string]json.RawMessage
	if json.Unmarshal(decision["toolCall"], &toolCall) != nil {
		return diagnostic
	}
	diagnostic.ToolCallKeys = sortedStructuralKeys(toolCall, nil)
	diagnostic.ToolName, _ = rawJSONString(toolCall["toolName"])
	if expectedDelivery, ok := rawJSONObject(toolCall["expectedDelivery"]); ok {
		diagnostic.ExpectedDeliveryKeys = sortedStructuralKeys(expectedDelivery, nil)
		diagnostic.DeliveryKind = rawStructuralString(expectedDelivery["kind"])
		diagnostic.RequiredArtifacts = rawStructuralStringArray(expectedDelivery["requiredArtifacts"])
		diagnostic.CriterionFacts = rawDeliveryCriterionFacts(expectedDelivery["completionCriteria"])
	}

	var arguments map[string]json.RawMessage
	if json.Unmarshal(toolCall["arguments"], &arguments) != nil {
		return diagnostic
	}
	diagnostic.ArgumentKeys = sortedStructuralKeys(arguments, nil)

	parameters, parametersObject := rawJSONObject(arguments["parameters"])
	diagnostic.ParameterKeys = sortedStructuralKeys(parameters, map[string]struct{}{
		"prompt":       {},
		"text":         {},
		"message":      {},
		"instructions": {},
	})

	if diagnostic.ToolName != "media.generate" {
		return diagnostic
	}
	media := &agentRuntimeMediaDecisionDiagnostic{}
	media.MediaKind, _ = rawJSONString(arguments["mediaKind"])
	media.ModelRecordIDPresent, media.ModelRecordIDNonEmpty = rawStringPresence(arguments, "modelRecordId")
	media.ModelKeyPresent, media.ModelKeyNonEmpty = rawStringPresence(arguments, "modelKey")
	media.SourceResourceIDsPresent, media.SourceResourceCount = rawArrayPresence(arguments, "sourceResourceIds")
	media.TargetCanvasNodeIDPresent, media.TargetCanvasNodeIDNonEmpty = rawStringPresence(arguments, "targetCanvasNodeId")
	media.ClientRequestIDPresent, media.ClientRequestIDNonEmpty = rawStringPresence(arguments, "clientRequestId")
	_, media.ParametersPresent = arguments["parameters"]
	media.ParametersObject = parametersObject
	diagnostic.Media = media
	return diagnostic
}

func rawStructuralString(raw json.RawMessage) string {
	value, ok := rawJSONString(raw)
	if !ok || !validDiagnosticFieldName(value) {
		return ""
	}
	return value
}

func rawStructuralStringArray(raw json.RawMessage) []string {
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if item := rawStructuralString(value); item != "" {
			result = append(result, item)
		} else {
			result = append(result, "invalid")
		}
	}
	return result
}

func rawDeliveryCriterionFacts(raw json.RawMessage) []string {
	var criteria []map[string]json.RawMessage
	if json.Unmarshal(raw, &criteria) != nil {
		return nil
	}
	result := make([]string, 0, len(criteria))
	for _, criterion := range criteria {
		fact := rawStructuralString(criterion["fact"])
		if fact == "" {
			fact = "invalid"
		}
		if artifact := rawStructuralString(criterion["artifact"]); artifact != "" {
			fact += ":" + artifact
		}
		result = append(result, fact)
	}
	return result
}

func strictAgentRuntimeDecisionDecodeProblem(payload []byte) string {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var decision agentruntime.ModelDecision
	if err := decoder.Decode(&decision); err != nil {
		const unknownFieldPrefix = "json: unknown field \""
		message := err.Error()
		if strings.HasPrefix(message, unknownFieldPrefix) && strings.HasSuffix(message, "\"") {
			field := strings.TrimSuffix(strings.TrimPrefix(message, unknownFieldPrefix), "\"")
			if validDiagnosticFieldName(field) {
				return "unknown_field:" + field
			}
			return "unknown_field"
		}
		var syntaxError *json.SyntaxError
		if errors.As(err, &syntaxError) {
			return "syntax_error"
		}
		var typeError *json.UnmarshalTypeError
		if errors.As(err, &typeError) {
			if validDiagnosticFieldName(typeError.Field) {
				return "type_error:" + typeError.Field
			}
			return "type_error"
		}
		return "strict_decode_error"
	}
	return ""
}

func validDiagnosticFieldName(value string) bool {
	if value == "" || len(value) > 120 {
		return false
	}
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || character == '_' || character == '-' || character == '.' {
			continue
		}
		return false
	}
	return true
}

func (s *Service) persistAgentRuntimeDecisionDiagnostic(task model.Task, payload []byte, validationErr error) error {
	diagnostic := buildAgentRuntimeDecisionDiagnostic(payload, validationErr)
	encoded, err := json.Marshal(diagnostic)
	if err != nil {
		return err
	}
	return s.log(task.UserID, task.ID, "error", "Agent 决策协议校验失败", string(encoded))
}

func rawJSONString(raw json.RawMessage) (string, bool) {
	var value string
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return strings.TrimSpace(value), true
}

func rawJSONObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	var value map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil || value == nil {
		return nil, false
	}
	return value, true
}

func rawStringPresence(values map[string]json.RawMessage, key string) (bool, bool) {
	raw, present := values[key]
	if !present {
		return false, false
	}
	value, valid := rawJSONString(raw)
	return true, valid && value != ""
}

func rawArrayPresence(values map[string]json.RawMessage, key string) (bool, int) {
	raw, present := values[key]
	if !present {
		return false, 0
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return true, 0
	}
	return true, len(items)
}

func sortedStructuralKeys(values map[string]json.RawMessage, excluded map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if _, skip := excluded[key]; skip {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
