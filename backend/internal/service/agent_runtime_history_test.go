package service

import (
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
)

func TestAgentThreadHistoryTurnViewNormalizesTimesToUTC(t *testing.T) {
	shanghai := time.FixedZone("Asia/Shanghai", 8*60*60)
	createdAt := time.Date(2026, time.September, 7, 8, 9, 10, 123000000, shanghai)
	updatedAt := createdAt.Add(time.Minute)
	completedAt := updatedAt.Add(time.Minute)

	view, err := agentThreadHistoryTurnView(model.AgentRun{
		ID: "run-utc", ThreadID: "thread-utc", CreatedAt: createdAt, UpdatedAt: updatedAt, CompletedAt: &completedAt,
	}, []model.AgentTimelineItem{{
		ID: "item-utc", RunID: "run-utc", ContentJSON: `{}`, StartedAt: createdAt,
		CreatedAt: createdAt, UpdatedAt: updatedAt, CompletedAt: &completedAt,
	}})
	if err != nil {
		t.Fatalf("build history turn view: %v", err)
	}

	assertUTCTime(t, "run.createdAt", view.Run.CreatedAt)
	assertUTCTime(t, "run.updatedAt", view.Run.UpdatedAt)
	assertUTCTimePointer(t, "run.completedAt", view.Run.CompletedAt)
	assertUTCTime(t, "item.startedAt", view.Items[0].StartedAt)
	assertUTCTime(t, "item.createdAt", view.Items[0].CreatedAt)
	assertUTCTime(t, "item.updatedAt", view.Items[0].UpdatedAt)
	assertUTCTimePointer(t, "item.completedAt", view.Items[0].CompletedAt)
}

func assertUTCTime(t *testing.T, label string, value time.Time) {
	t.Helper()
	if value.Location() != time.UTC {
		t.Fatalf("%s location = %s, want UTC", label, value.Location())
	}
}

func assertUTCTimePointer(t *testing.T, label string, value *time.Time) {
	t.Helper()
	if value == nil {
		t.Fatalf("%s is nil", label)
	}
	assertUTCTime(t, label, *value)
}
