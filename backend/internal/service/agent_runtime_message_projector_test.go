package service

import (
	"errors"
	"testing"
	"time"
)

func TestAgentVisibleMessageProjectorBatchesRapidDeltasAndFlushesRemainder(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	type projection struct {
		delta   string
		message string
		started bool
	}
	projections := make([]projection, 0, 3)
	projector := newAgentVisibleMessageProjector(200*time.Millisecond, func(delta string, message string, started bool) error {
		projections = append(projections, projection{delta: delta, message: message, started: started})
		return nil
	})

	if err := projector.Push("第", now); err != nil {
		t.Fatal(err)
	}
	if err := projector.Push("一", now.Add(20*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := projector.Push("段", now.Add(40*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := projector.Push("。", now.Add(250*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := projector.Push("完成", now.Add(270*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := projector.Flush(now.Add(280 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}

	if len(projections) != 3 {
		t.Fatalf("rapid stream created %d durable projections, want 3", len(projections))
	}
	wantDeltas := []string{"第", "一段。", "完成"}
	wantMessages := []string{"第", "第一段。", "第一段。完成"}
	for index := range projections {
		if projections[index].delta != wantDeltas[index] || projections[index].message != wantMessages[index] || projections[index].started != (index == 0) {
			t.Fatalf("projection[%d] = %#v", index, projections[index])
		}
	}
}

func TestCombineAgentRuntimeRequestErrorsPreservesBothFailures(t *testing.T) {
	providerErr := errors.New("provider stream failed")
	projectionErr := errors.New("visible projection failed")

	combined := combineAgentRuntimeRequestErrors(providerErr, projectionErr)
	if !errors.Is(combined, providerErr) || !errors.Is(combined, projectionErr) {
		t.Fatalf("combined error = %v, want both provider and projection failures", combined)
	}
	if combineAgentRuntimeRequestErrors(nil, projectionErr) != projectionErr {
		t.Fatal("single projection failure should remain unchanged")
	}
}

func TestAgentVisibleMessageProjectorDoesNotAdvanceAfterFailedWrite(t *testing.T) {
	now := time.Now()
	writeErr := errors.New("projection write rejected")
	attempts := 0
	projector := newAgentVisibleMessageProjector(time.Second, func(delta, message string, started bool) error {
		attempts++
		if delta != "first" || message != "first" || !started {
			t.Fatalf("failed write altered projection identity: %q %q %v", delta, message, started)
		}
		if attempts == 1 {
			return writeErr
		}
		return nil
	})
	if err := projector.Push("first", now); !errors.Is(err, writeErr) {
		t.Fatalf("error lost: %v", err)
	}
	if projector.PersistedMessage() != "" {
		t.Fatal("failed write presented as persisted")
	}
	if err := projector.Flush(now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if projector.PersistedMessage() != "first" || attempts != 2 {
		t.Fatal("retry lost or duplicated pending text")
	}
}
