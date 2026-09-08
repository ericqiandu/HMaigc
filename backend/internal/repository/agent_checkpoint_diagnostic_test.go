package repository

import (
	"strings"
	"testing"
)

func TestCheckpointDiagnosticKeepsConsistencyGate(t *testing.T) {
	_, err := decodeAgentCheckpointState(`{"stepNumber":2}`, 0, 0, 3, 0, "")
	if err == nil || !strings.Contains(err.Error(), "inconsistent") {
		t.Fatalf("mismatched checkpoint must fail: %v", err)
	}
	_, err = decodeAgentCheckpointState(`{"stepNumber":"secret"}`, 0, 0, 0, 0, "")
	if err == nil || !strings.Contains(err.Error(), "reason=type_mismatch") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("expected redacted diagnostic: %v", err)
	}
}
