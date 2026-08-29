package repository

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

func TestAppendAgentMediaAssemblyTimelineRejectsConflictingLateCancelledOutput(t *testing.T) {
	repo, db := openAdminAgentRunRepositorySQLite(t)
	now := time.Date(2026, time.August, 29, 10, 0, 0, 0, time.UTC)
	const runID = "run-late-assembly-identity"
	const userID = "user-late-assembly-identity"
	createAdminAgentRunUser(t, db, userID, "late-assembly@example.com", "迟到装配用户")
	createAdminAgentRunRecord(t, db, adminAgentRunFixture{
		runID: runID, userID: userID, projectID: "project-late-assembly",
		canvasID: "canvas-late-assembly", status: agentruntime.RunCancelled, updatedAt: now,
	})
	scope := agentruntime.Scope{
		TenantKind: agentruntime.TenantPersonal, TenantID: userID,
		ThreadID: "thread-" + runID, RunID: runID, ActorUserID: userID,
		DomainProjectID: "project-late-assembly", CanvasID: "canvas-late-assembly",
		Access: agentruntime.AccessGrant{Level: agentruntime.AccessManager, SubscriptionActive: true},
	}
	width, height, frameRate := 1920, 1080, 24
	cancelled := agentruntime.MediaAssemblyTimelineContent{
		ContentType: agentruntime.MediaAssemblyContentType, ToolCallID: "assemble-final", ActionVersion: 1,
		TaskID: "assembly-original", TaskStatus: agentruntime.MediaAssemblyTaskCancelled,
		Stage: "Agent 任务已终止", ClipCount: 2, AudioMode: agentruntime.MediaAudioNone,
		Output: agentruntime.AssemblyOutputV2{
			ArtifactKey: "final-video", Container: "mp4", VideoCodec: "h264", AudioCodec: "none",
			Width: &width, Height: &height, FrameRate: &frameRate,
		},
		PlanRevision: agentruntime.ArtifactRevisionRef{ArtifactID: "assembly-plan", RevisionID: "assembly-plan-r2"},
		ErrorCode:    "media_assembly_cancelled",
	}
	payload, err := json.Marshal(cancelled)
	if err != nil {
		t.Fatal(err)
	}
	itemID := agentFactID("timeline", runID, "tool-call", cancelled.ToolCallID+":1")
	completedAt := now
	if err := db.Create(&model.AgentTimelineItem{
		ID: itemID, TenantKind: scope.TenantKind, TenantID: scope.TenantID,
		ThreadID: scope.ThreadID, RunID: runID, Kind: model.AgentTimelineItemToolCall,
		Status: model.AgentTimelineItemInterrupted, Ordinal: 1, SourceEventSequence: 3,
		ContentJSON: string(payload), StartedAt: now, CompletedAt: &completedAt, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	late := cancelled
	late.Final = &agentruntime.MediaAssemblyFinal{
		ArtifactRevision: agentruntime.ArtifactRevisionRef{ArtifactID: "final-video", RevisionID: "final-video-r1"},
		ResourceID:       "assembled-original", Adopted: false,
	}
	if _, err := repo.AppendAgentMediaAssemblyTimeline(scope, late); err != nil {
		t.Fatalf("exact late output = %v", err)
	}

	conflicting := late
	conflicting.TaskID = "assembly-conflicting"
	if _, err := repo.AppendAgentMediaAssemblyTimeline(scope, conflicting); !errors.Is(err, ErrAgentTimelineConflict) {
		t.Fatalf("conflicting late output error = %v, want %v", err, ErrAgentTimelineConflict)
	}
	var stored model.AgentTimelineItem
	if err := db.First(&stored, "id = ?", itemID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ContentJSON != mustJSON(t, late) || stored.SourceEventSequence != 4 {
		t.Fatalf("conflicting late output changed timeline facts: %#v", stored)
	}
}

func mustJSON(t *testing.T, value agentruntime.MediaAssemblyTimelineContent) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
