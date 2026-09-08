package service

import (
	"strings"
	"testing"
)

func TestCheckpointDecodeDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, input, reason string }{
		{"unknown field", `{"private-secret":"credential"}`, "unknown_field"},
		{"type", `{"stepNumber":"private-secret"}`, "type_mismatch"},
		{"syntax", `{"stepNumber":!}`, "syntax"},
		{"empty", ``, "empty_or_truncated"},
		{"trailing", `{} {"private-secret":1}`, "trailing_data"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := decodeAgentRuntimeState(test.input)
			if err == nil || !strings.Contains(err.Error(), "reason="+test.reason) {
				t.Fatalf("expected reason %s, got %v", test.reason, err)
			}
			if strings.Contains(err.Error(), "private-secret") || strings.Contains(err.Error(), "credential") {
				t.Fatal("checkpoint content leaked in diagnostic")
			}
		})
	}
}

func TestCheckpointDecodePreservesValidState(t *testing.T) {
	state, err := decodeAgentRuntimeState(`{"stepNumber":3}`)
	if err != nil || state.StepNumber != 3 {
		t.Fatalf("valid state lost: %v, %v", state.StepNumber, err)
	}
}
