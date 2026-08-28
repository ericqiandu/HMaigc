package agentruntime

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
)

type DecisionStreamObserver struct {
	raw               string
	emitted           string
	toolSchemaVersion int
}

func NewDecisionStreamObserver() *DecisionStreamObserver {
	return NewDecisionStreamObserverForToolSchema(CurrentToolSchemaVersion)
}

func NewDecisionStreamObserverForToolSchema(toolSchemaVersion int) *DecisionStreamObserver {
	return &DecisionStreamObserver{toolSchemaVersion: toolSchemaVersion}
}

func AgentMessageItemID(runID string, stepNumber int) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("agent-message\x00%s\x00%d", runID, stepNumber))))
}

func (observer *DecisionStreamObserver) Push(delta string) (string, error) {
	if len(observer.raw)+len(delta) > modelDecisionLimit {
		return "", errors.New("agent model decision size is invalid")
	}
	observer.raw += delta
	kind, kindFound := directJSONStringField(observer.raw, 0, 1, "kind")
	if !kindFound || kind != string(DecisionFinal) {
		return "", nil
	}
	finalStart, finalFound := directJSONObjectField(observer.raw, 0, 1, "final")
	if !finalFound {
		return "", nil
	}
	message, messageFound := directJSONStringField(observer.raw, finalStart, 2, "message")
	if !messageFound || len(message) <= len(observer.emitted) {
		return "", nil
	}
	if message[:len(observer.emitted)] != observer.emitted {
		return "", errors.New("agent final message stream is not monotonic")
	}
	visible := message[len(observer.emitted):]
	observer.emitted = message
	return visible, nil
}

func (observer *DecisionStreamObserver) Finish() (ModelDecision, error) {
	return ParseModelDecisionForToolSchema([]byte(observer.raw), observer.toolSchemaVersion)
}

func directJSONObjectField(raw string, start int, wantedDepth int, key string) (int, bool) {
	return scanDirectField(raw, start, wantedDepth, key, func(index int) (int, bool) {
		if index < len(raw) && raw[index] == '{' {
			return index + 1, true
		}
		return 0, false
	})
}

func directJSONStringField(raw string, start int, wantedDepth int, key string) (string, bool) {
	valueStart, found := scanDirectField(raw, start, wantedDepth, key, func(index int) (int, bool) {
		if index < len(raw) && raw[index] == '"' {
			return index + 1, true
		}
		return 0, false
	})
	if !found {
		return "", false
	}
	end := valueStart
	escaped := false
	for end < len(raw) {
		character := raw[end]
		if character == '"' && !escaped {
			break
		}
		if character == '\\' && !escaped {
			escaped = true
		} else {
			escaped = false
		}
		end++
	}
	decoded, err := strconv.Unquote("\"" + raw[valueStart:end] + "\"")
	if err != nil {
		return "", false
	}
	return decoded, true
}

func scanDirectField[T any](raw string, start int, wantedDepth int, key string, accept func(int) (T, bool)) (T, bool) {
	var zero T
	depth := wantedDepth
	if start == 0 {
		depth = 0
	}
	for index := start; index < len(raw); {
		switch raw[index] {
		case '{', '[':
			depth++
			index++
		case '}', ']':
			depth--
			index++
		case '"':
			value, next, complete := scanJSONStringToken(raw, index)
			if !complete {
				return zero, false
			}
			index = next
			if depth != wantedDepth || value != key {
				continue
			}
			for index < len(raw) && isJSONWhitespace(raw[index]) {
				index++
			}
			if index >= len(raw) || raw[index] != ':' {
				continue
			}
			index++
			for index < len(raw) && isJSONWhitespace(raw[index]) {
				index++
			}
			return accept(index)
		default:
			index++
		}
	}
	return zero, false
}

func scanJSONStringToken(raw string, start int) (string, int, bool) {
	escaped := false
	for index := start + 1; index < len(raw); index++ {
		if raw[index] == '"' && !escaped {
			decoded, err := strconv.Unquote(raw[start : index+1])
			return decoded, index + 1, err == nil
		}
		if raw[index] == '\\' && !escaped {
			escaped = true
		} else {
			escaped = false
		}
	}
	return "", len(raw), false
}

func isJSONWhitespace(value byte) bool {
	return value == ' ' || value == '\n' || value == '\r' || value == '\t'
}
