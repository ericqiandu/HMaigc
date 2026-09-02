package service

import "testing"

func TestAgentMediaGenerationOperationWakesOwningRun(t *testing.T) {
	t.Parallel()
	want := "runtime-run"
	operation := agentMediaGenerationOperationForRun(want)
	if got, ok := agentMediaGenerationRunID(operation); !ok || got != want {
		t.Fatalf("operation parser = %q/%v, want %q/true", got, ok, want)
	}
	for _, invalid := range []string{"", "media_generation:", "other:" + want} {
		if got, ok := agentMediaGenerationRunID(invalid); ok || got != "" {
			t.Fatalf("invalid operation %q parsed as %q/%v", invalid, got, ok)
		}
	}
}
