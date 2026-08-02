package service

import (
	"testing"

	"infinite-canvas/backend/internal/model"
)

func TestBuildAgentStoryboardResultAutomaticStartsTextToVideoWithDeliverySpec(t *testing.T) {
	plan := agentStoryboardPlan{
		Title:      "雨夜追车",
		StyleGuide: "电影感霓虹夜景",
		Shots: []agentStoryboardShot{{
			Title:       "追逐",
			Duration:    8,
			VideoPrompt: "少女追逐无人列车",
		}},
	}
	_, ops, err := buildAgentStoryboardResult(
		model.Task{ID: "task-1", Prompt: "测试"},
		plan,
		nil,
		"automatic",
		agentDeliverySpec{AspectRatio: "16:9", Resolution: "720", DurationSeconds: 6},
	)
	if err != nil {
		t.Fatalf("build result: %v", err)
	}

	shotID := "agent-task-1-shot-1"
	shot := findCanvasOp(t, ops, "add_node", shotID)
	metadata, ok := shot["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("shot metadata has unexpected type: %#v", shot["metadata"])
	}
	for key, expected := range map[string]any{
		"videoEditOperation": "text_to_video",
		"size":               "16:9",
		"vquality":           "720",
		"seconds":            "6",
	} {
		if metadata[key] != expected {
			t.Fatalf("metadata[%s] = %#v, want %#v", key, metadata[key], expected)
		}
	}

	run := findCanvasOp(t, ops, "run_generation", "")
	if run["nodeId"] != shotID || run["mode"] != "video" {
		t.Fatalf("unexpected generation op: %#v", run)
	}
}

func TestBuildAgentStoryboardResultGuidedDoesNotStartGeneration(t *testing.T) {
	plan := agentStoryboardPlan{Title: "方案", Shots: []agentStoryboardShot{{Title: "镜头", Duration: 6, VideoPrompt: "提示词"}}}
	_, ops, err := buildAgentStoryboardResult(model.Task{ID: "task-2"}, plan, nil, "guided", agentDeliverySpec{})
	if err != nil {
		t.Fatalf("build result: %v", err)
	}
	for _, op := range ops {
		if op["type"] == "run_generation" {
			t.Fatalf("guided mode must not start generation: %#v", op)
		}
	}
}

func findCanvasOp(t *testing.T, ops []map[string]any, opType string, id string) map[string]any {
	t.Helper()
	for _, op := range ops {
		if op["type"] == opType && (id == "" || op["id"] == id) {
			return op
		}
	}
	t.Fatalf("missing %s op for %s", opType, id)
	return nil
}
