package service

import (
	"context"
	"errors"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
)

func TestAgentRuntimeStateObservationFailurePolicyRequiresConsecutiveFailures(t *testing.T) {
	policy := newAgentRuntimeStateObservationFailurePolicy(3)
	transient := errors.New("transient database error")

	if policy.Record(transient) {
		t.Fatal("one transient observation failure cancelled the provider")
	}
	if policy.Record(nil) {
		t.Fatal("a successful observation cancelled the provider")
	}
	if policy.Record(transient) || policy.Record(transient) {
		t.Fatal("fewer than three consecutive failures cancelled the provider")
	}
	if !policy.Record(transient) {
		t.Fatal("three consecutive observation failures did not cancel the provider")
	}
	report := policy.Report()
	if report.TotalFailures != 4 || report.ConsecutiveFailures != 3 || !report.ThresholdExceeded {
		t.Fatalf("observation report = %#v", report)
	}
}

func TestLoadAgentRuntimeCheckpointWithRetryReturnsRecoveredFacts(t *testing.T) {
	attempts := 0
	want := agentruntime.RuntimeState{StateVersion: 3, StepNumber: 1, MaxSteps: 4, Status: agentruntime.RunRunning}
	state, report, err := loadAgentRuntimeCheckpointWithRetry(context.Background(), 3, 0, func() (agentruntime.RuntimeState, error) {
		attempts++
		if attempts < 3 {
			return agentruntime.RuntimeState{}, errors.New("transient database error")
		}
		return want, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if state.StateVersion != want.StateVersion || attempts != 3 || report.TotalFailures != 2 || report.ConsecutiveFailures != 0 || report.ThresholdExceeded {
		t.Fatalf("recovered checkpoint: state=%#v attempts=%d report=%#v", state, attempts, report)
	}
}
