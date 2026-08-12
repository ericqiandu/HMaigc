package service

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestKuaiziSeedance25RequestMatchesPublishedContract(t *testing.T) {
	request, err := newKuaiziSeedance25Request(canvasGenerationInput{
		Prompt: "电影感追逐镜头",
		Config: providerConfig{
			Model:              "kuaizi-seedance-2.5",
			VQuality:           "720p",
			Size:               "adaptive",
			VideoSeconds:       "30",
			VideoGenerateAudio: "true",
			VideoWatermark:     "false",
		},
		ReferenceImages: []providerMedia{
			{ID: "first", URL: "asset://first-frame"},
			{ID: "reference", URL: "asset://reference-image"},
		},
		ReferenceVideos: []providerMedia{{URL: "asset://reference-video"}},
		ReferenceAudios: []providerMedia{{URL: "asset://reference-audio"}},
		Metadata:        map[string]interface{}{"videoStartFrameNodeId": "first"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if request.Mode != "seedance2.5" || request.Resolution != "720p" || request.Ratio != "adaptive" || request.Duration != 30 {
		t.Fatalf("request settings = %#v", request)
	}
	if !request.GenerateAudio || request.Watermark || !request.ReturnLastFrame {
		t.Fatalf("request flags = %#v", request)
	}
	if len(request.Images) != 2 || request.Images[0].Role != "first_frame" || request.Images[1].Role != "reference_image" {
		t.Fatalf("images = %#v", request.Images)
	}
	if len(request.Videos) != 1 || request.Videos[0].Role != "reference_video" {
		t.Fatalf("videos = %#v", request.Videos)
	}
	if len(request.Audios) != 1 || request.Audios[0].Role != "reference_audio" {
		t.Fatalf("audios = %#v", request.Audios)
	}
}

func TestKuaiziSeedance25RequestRejectsUnsupportedStructure(t *testing.T) {
	base := canvasGenerationInput{
		Prompt: "test",
		Config: providerConfig{Model: "kuaizi-seedance-2.5", VQuality: "720p", Size: "adaptive", VideoSeconds: "5"},
	}
	tests := []struct {
		name   string
		mutate func(*canvasGenerationInput)
		want   string
	}{
		{name: "duration below", mutate: func(input *canvasGenerationInput) { input.Config.VideoSeconds = "3" }, want: "4–30"},
		{name: "duration above", mutate: func(input *canvasGenerationInput) { input.Config.VideoSeconds = "31" }, want: "4–30"},
		{name: "resolution", mutate: func(input *canvasGenerationInput) { input.Config.VQuality = "1080p" }, want: "480p 或 720p"},
		{name: "images", mutate: func(input *canvasGenerationInput) { input.ReferenceImages = make([]providerMedia, 31) }, want: "30 张"},
		{name: "videos", mutate: func(input *canvasGenerationInput) { input.ReferenceVideos = make([]providerMedia, 11) }, want: "10 个"},
		{name: "audios", mutate: func(input *canvasGenerationInput) { input.ReferenceAudios = make([]providerMedia, 11) }, want: "10 段"},
		{name: "audio only without prompt", mutate: func(input *canvasGenerationInput) {
			input.Prompt = ""
			input.ReferenceAudios = []providerMedia{{URL: "asset://audio"}}
		}, want: "提示词"},
		{name: "frame ratio", mutate: func(input *canvasGenerationInput) {
			input.Config.Size = "16:9"
			input.ReferenceImages = []providerMedia{{ID: "first", URL: "asset://first"}}
			input.Metadata = map[string]interface{}{"videoStartFrameNodeId": "first"}
		}, want: "adaptive"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := base
			test.mutate(&input)
			_, err := newKuaiziSeedance25Request(input)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestKuaiziSeedance25ResponseParsingIsClosedAndComplete(t *testing.T) {
	created, err := parseKuaiziSeedance25Create([]byte(`{"code":0,"data":{"task_id":"task-1"},"trace_id":"trace-create"}`))
	if err != nil || created.TaskID != "task-1" || created.TraceID != "trace-create" {
		t.Fatalf("created = %#v, error = %v", created, err)
	}

	for _, status := range []string{"submitted", "pending", "running"} {
		state, err := parseKuaiziSeedance25Status([]byte(`{"code":0,"data":{"task_id":"task-1","status":"` + status + `"}}`))
		if err != nil || state.Status != status || state.Terminal {
			t.Fatalf("status %s = %#v, error = %v", status, state, err)
		}
	}

	succeeded, err := parseKuaiziSeedance25Status([]byte(`{"code":0,"data":{"task_id":"task-1","status":"succeeded","video_url":"https://cdn.example.com/result.mp4","last_frame_url":"https://cdn.example.com/last.png","duration":5,"usage":{"total_tokens":108900}},"trace_id":"trace-status"}`))
	if err != nil || !succeeded.Terminal || succeeded.VideoURL == "" || succeeded.TotalTokens != "108900" || succeeded.TraceID != "trace-status" {
		t.Fatalf("succeeded = %#v, error = %v", succeeded, err)
	}

	failed, err := parseKuaiziSeedance25Status([]byte(`{"code":0,"data":{"task_id":"task-1","status":"failed","error":"provider rejected"}}`))
	if err != nil || !failed.Terminal || failed.Error != "provider rejected" {
		t.Fatalf("failed = %#v, error = %v", failed, err)
	}

	invalid := []string{
		`{"code":0,"data":{}}`,
		`{"code":0,"data":{"task_id":"task-1","status":"mystery"}}`,
		`{"code":0,"data":{"task_id":"task-1","status":"succeeded","video_url":"","duration":5,"usage":{"total_tokens":1}}}`,
		`{"code":0,"data":{"task_id":"task-1","status":"succeeded","video_url":"https://cdn.example.com/result.mp4","duration":5,"usage":{}}}`,
		`{"code":1001,"message":"bad request","data":{}}`,
	}
	for _, raw := range invalid {
		if _, err := parseKuaiziSeedance25Status([]byte(raw)); err == nil {
			t.Fatalf("parse status accepted %s", raw)
		}
	}
}

func TestKuaiziSeedance25RequestJSONDoesNotContainRuntimeSecrets(t *testing.T) {
	request, err := newKuaiziSeedance25Request(canvasGenerationInput{
		Prompt: "test",
		Config: providerConfig{
			Model:        "kuaizi-seedance-2.5",
			VQuality:     "480p",
			Size:         "16:9",
			VideoSeconds: "5",
			BaseURL:      "https://secret.example.com",
			APIKey:       "secret-key",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "secret.example.com") || strings.Contains(string(payload), "secret-key") {
		t.Fatalf("request leaked runtime secret: %s", payload)
	}
}
