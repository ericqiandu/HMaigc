package agentruntime_test

import (
	"encoding/json"
	"errors"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
)

func TestCurrentToolSchemaRejectsHistoricalMediaAssemblyDecision(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"kind":"tool_call","toolCall":{"toolCallId":"assemble-final","toolName":"media.assemble","actionVersion":1,"arguments":{"planRevision":{"artifactId":"plan-artifact","revisionId":"plan-r2"},"expectedDelivery":{"kind":"mixed","requiredArtifacts":["video","canvas_revision"],"targetCanvasId":"canvas-1","completionCriteria":[{"fact":"task_backed_resource","artifact":"video"},{"fact":"canvas_revision"}]}},"expectedDelivery":{"kind":"mixed","requiredArtifacts":["video","canvas_revision"],"targetCanvasId":"canvas-1","completionCriteria":[{"fact":"task_backed_resource","artifact":"video"},{"fact":"canvas_revision"}]}}}`)
	if _, err := agentruntime.ParseModelDecisionForToolSchema(payload, agentruntime.CurrentToolSchemaVersion); err == nil {
		t.Fatal("current tool schema accepted retired media.assemble")
	}
}

func TestMediaAssemblyTimelineContentRejectsSyntheticProgressAndURLs(t *testing.T) {
	t.Parallel()

	content := agentruntime.MediaAssemblyTimelineContent{
		ContentType: agentruntime.MediaAssemblyContentType,
		ToolCallID:  "assemble-final", ActionVersion: 1,
		TaskID: "assembly-task", TaskStatus: agentruntime.MediaAssemblyTaskRunning,
		Stage: "拼接视频片段", ClipCount: 2, AudioMode: agentruntime.MediaAudioNative,
		Output:       agentruntime.AssemblyOutputV2{ArtifactKey: "final-video", Container: "mp4", VideoCodec: "h264", AudioCodec: "aac", Width: intPtr(1920), Height: intPtr(1080), FrameRate: intPtr(24)},
		PlanRevision: agentruntime.ArtifactRevisionRef{ArtifactID: "plan-artifact", RevisionID: "plan-r2"},
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := agentruntime.DecodeMediaAssemblyTimelineContent(encoded)
	if err != nil || decoded.TaskStatus != agentruntime.MediaAssemblyTaskRunning || decoded.Stage != "拼接视频片段" {
		t.Fatalf("decoded content = %#v, err=%v", decoded, err)
	}

	invalid := []string{
		string(encoded[:len(encoded)-1]) + `,"progress":50}`,
		string(encoded[:len(encoded)-1]) + `,"url":"https://example.invalid/final.mp4"}`,
		string(encoded[:len(encoded)-1]) + `,"reasoning":"hidden"}`,
	}
	for _, payload := range invalid {
		if _, err := agentruntime.DecodeMediaAssemblyTimelineContent([]byte(payload)); !errors.Is(err, agentruntime.ErrArtifactPayloadInvalid) {
			t.Fatalf("invalid timeline payload err = %v", err)
		}
	}
}

func intPtr(value int) *int { return &value }
