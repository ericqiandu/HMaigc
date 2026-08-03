package service

import (
	"strings"
	"testing"
)

func TestMiniMaxH3UsesV2RootAndUnwrapsTaskResponse(t *testing.T) {
	config := providerConfig{BaseURL: "https://api.minimaxi.com/v1"}
	if got := apiURL(config.BaseURL, "/v2/query/video_generation/task-1"); got != "https://api.minimaxi.com/v2/query/video_generation/task-1" {
		t.Fatalf("request URL = %q", got)
	}
	response := map[string]interface{}{"task": map[string]interface{}{
		"status":  "succeeded",
		"content": map[string]interface{}{"url": "https://example.com/video.mp4"},
	}}
	state := miniMaxH3TaskState(response)
	if stringField(state, "status") != "succeeded" || miniMaxH3ResultURL(state) != "https://example.com/video.mp4" {
		t.Fatalf("unexpected task state: %#v", state)
	}
}

func TestMiniMaxH3FailureMessagePreservesProviderReason(t *testing.T) {
	state := map[string]interface{}{"status": "Failed", "message": "provider rejected request"}
	if got := miniMaxH3FailureMessage(state); got != "provider rejected request" {
		t.Fatalf("failure reason = %q", got)
	}
}

func TestMiniMaxH3TextVideoBody(t *testing.T) {
	body, err := miniMaxH3VideoBody(miniMaxH3TestInput("text"))
	if err != nil {
		t.Fatalf("build text video body: %v", err)
	}
	if body["model"] != miniMaxH3Model || body["resolution"] != "2K" || body["ratio"] != "16:9" || body["duration"] != 8 {
		t.Fatalf("unexpected body: %#v", body)
	}
	content, ok := body["content"].([]map[string]interface{})
	if !ok || len(content) != 1 || content[0]["type"] != "text" {
		t.Fatalf("unexpected content: %#v", body["content"])
	}
}

func TestMiniMaxH3FirstLastFrameRoles(t *testing.T) {
	input := miniMaxH3TestInput("first_last_frame")
	input.Metadata["videoStartFrameNodeId"] = "start"
	input.Metadata["videoEndFrameNodeId"] = "end"
	input.ReferenceImages = []providerMedia{
		{ID: "end", Name: "end.png", URL: "https://assets.example.com/end.png"},
		{ID: "start", Name: "start.png", URL: "https://assets.example.com/start.png"},
	}
	body, err := miniMaxH3VideoBody(input)
	if err != nil {
		t.Fatalf("build frame body: %v", err)
	}
	if body["ratio"] != "adaptive" {
		t.Fatalf("frame mode must use adaptive ratio: %#v", body["ratio"])
	}
	content := body["content"].([]map[string]interface{})
	if content[1]["role"] != "last_frame" || content[2]["role"] != "first_frame" {
		t.Fatalf("frame roles must follow node ids: %#v", content)
	}
}

func TestMiniMaxH3OmniReferenceRejectsAudioOnly(t *testing.T) {
	input := miniMaxH3TestInput("omni_reference")
	input.ReferenceAudios = []providerMedia{{ID: "audio", URL: "https://assets.example.com/audio.mp3"}}
	_, err := miniMaxH3VideoBody(input)
	if err == nil || !strings.Contains(err.Error(), "不能只提供音频") {
		t.Fatalf("expected audio-only error, got %v", err)
	}
}

func TestMiniMaxH3RejectsUnsupportedParameters(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*canvasGenerationInput)
	}{
		{name: "model", mutate: func(input *canvasGenerationInput) { input.Config.Model = "MiniMax-H2" }},
		{name: "duration", mutate: func(input *canvasGenerationInput) { input.Config.VideoSeconds = "16" }},
		{name: "resolution", mutate: func(input *canvasGenerationInput) { input.Config.VQuality = "1080p" }},
		{name: "ratio", mutate: func(input *canvasGenerationInput) { input.Config.Size = "2:1" }},
		{name: "adaptive text", mutate: func(input *canvasGenerationInput) { input.Config.Size = "adaptive" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := miniMaxH3TestInput("text")
			test.mutate(&input)
			if _, err := miniMaxH3VideoBody(input); err == nil {
				t.Fatal("expected explicit validation error")
			}
		})
	}
}

func miniMaxH3TestInput(mode string) canvasGenerationInput {
	return canvasGenerationInput{
		Prompt: "一位演员走进晨雾中的城市",
		Config: providerConfig{
			Model: miniMaxH3Model, VideoSeconds: "8", VQuality: "2k", Size: "16:9", VideoWatermark: "true",
		},
		Metadata: map[string]interface{}{"videoGenerationMode": mode},
	}
}
