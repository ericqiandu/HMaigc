package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAIOpenPlatformVideoBodyUsesNativeMultimodalFields(t *testing.T) {
	request, err := aiOpenPlatformVideoBody(canvasGenerationInput{
		Prompt: "电影感追逐镜头",
		Config: providerConfig{
			Model:                          "doubao-seedance-2-0-fast-260128",
			Size:                           "16:9",
			VQuality:                       "720p",
			VideoSeconds:                   "5",
			VideoGenerateAudio:             "true",
			VideoWatermark:                 "false",
			VideoSuperResolutionEnabled:    "true",
			VideoSuperResolutionResolution: "4k",
			VideoSuperResolutionScene:      "short_series",
			VideoSuperResolutionVersion:    "professional",
			VideoSuperResolutionFPS:        "60",
		},
		ReferenceImages: []providerMedia{{URL: "https://cdn.example.com/first.png"}},
		ReferenceVideos: []providerMedia{{URL: "asset://video-reference"}},
		ReferenceAudios: []providerMedia{{URL: "https://cdn.example.com/audio.mp3"}},
		Metadata:        map[string]interface{}{"videoStartFrameNodeId": ""},
	})
	if err != nil {
		t.Fatalf("aiOpenPlatformVideoBody() error = %v", err)
	}
	if request.Mode != "fast" || request.Resolution != "720p" || request.Ratio != "16:9" ||
		request.Duration != 5 || !request.GenerateAudio || request.Watermark || !request.ReturnLastFrame {
		t.Fatalf("request settings = %#v", request)
	}
	if len(request.Images) != 1 || len(request.Videos) != 1 || len(request.Audios) != 1 {
		t.Fatalf("media lists = images:%#v videos:%#v audios:%#v", request.Images, request.Videos, request.Audios)
	}
	if request.Videos[0].Role != "reference_video" || request.Audios[0].Role != "reference_audio" {
		t.Fatalf("media roles = videos:%#v audios:%#v", request.Videos, request.Audios)
	}
	if request.SuperResolutionConfig == nil ||
		request.SuperResolutionConfig.Resolution != "4k" ||
		request.SuperResolutionConfig.Scene != "short_series" ||
		request.SuperResolutionConfig.ToolVersion != "professional" ||
		request.SuperResolutionConfig.FPS != 60 {
		t.Fatalf("super resolution = %#v", request.SuperResolutionConfig)
	}
}

func TestAIOpenPlatformVideoBodyRejectsInvalidSuperResolutionTransition(t *testing.T) {
	_, err := aiOpenPlatformVideoBody(canvasGenerationInput{
		Prompt: "test",
		Config: providerConfig{
			Model:                          "doubao-seedance-2-0-260128",
			Size:                           "16:9",
			VQuality:                       "1080p",
			VideoSeconds:                   "5",
			VideoSuperResolutionEnabled:    "true",
			VideoSuperResolutionResolution: "720p",
			VideoSuperResolutionScene:      "aigc",
			VideoSuperResolutionVersion:    "standard",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "不支持从 1080p 超分到 720p") {
		t.Fatalf("error = %v", err)
	}
}

func TestAIOpenPlatformVideoLegacyAdapterRejectsSeedance25(t *testing.T) {
	_, err := aiOpenPlatformVideoModelMode("kuaizi-seedance-2.5")
	if err == nil {
		t.Fatal("legacy AI Open Platform adapter accepted Seedance 2.5")
	}
}

func TestAIOpenPlatformVideoResolutionMatchesSeedanceModelCapabilities(t *testing.T) {
	tests := []struct {
		name       string
		mode       string
		resolution string
		want       string
		wantError  bool
	}{
		{name: "fast 480p", mode: "fast", resolution: "480p", want: "480p"},
		{name: "fast 720p", mode: "fast", resolution: "720p", want: "720p"},
		{name: "fast rejects 1080p", mode: "fast", resolution: "1080p", wantError: true},
		{name: "fast rejects 4k", mode: "fast", resolution: "4k", wantError: true},
		{name: "mini 480p", mode: "mini", resolution: "480p", want: "480p"},
		{name: "mini 720p", mode: "mini", resolution: "720p", want: "720p"},
		{name: "mini rejects 1080p", mode: "mini", resolution: "1080p", wantError: true},
		{name: "mini rejects 4k", mode: "mini", resolution: "4k", wantError: true},
		{name: "pro 480p", mode: "pro", resolution: "480p", want: "480p"},
		{name: "pro 720p", mode: "pro", resolution: "720p", want: "720p"},
		{name: "pro 1080p", mode: "pro", resolution: "1080p", want: "1080p"},
		{name: "pro 4k", mode: "pro", resolution: "4K", want: "4k"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := aiOpenPlatformVideoResolution(test.resolution, test.mode)
			if test.wantError {
				if err == nil {
					t.Fatalf("aiOpenPlatformVideoResolution(%q, %q) = %q, want error", test.resolution, test.mode, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("aiOpenPlatformVideoResolution(%q, %q) error = %v", test.resolution, test.mode, err)
			}
			if got != test.want {
				t.Fatalf("aiOpenPlatformVideoResolution(%q, %q) = %q, want %q", test.resolution, test.mode, got, test.want)
			}
		})
	}
}

func TestAIOpenPlatformVideoBodyRejectsAudioOnlyWithoutPrompt(t *testing.T) {
	_, err := aiOpenPlatformVideoBody(canvasGenerationInput{
		Config: providerConfig{
			Model:        "doubao-seedance-2-0-fast-260128",
			Size:         "16:9",
			VQuality:     "720p",
			VideoSeconds: "5",
		},
		ReferenceAudios: []providerMedia{{URL: "https://cdn.example.com/audio.mp3"}},
	})
	if err == nil || !strings.Contains(err.Error(), "至少需要提示词、参考图片或参考视频") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunAIOpenPlatformVideoTaskUsesNativeTaskProtocol(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	var received aiOpenPlatformVideoCreateRequest
	var statusRequest aiOpenPlatformVideoStatusRequest
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/result.mp4" && request.Header.Get("ApiKey") != "test-key" {
			t.Fatalf("ApiKey = %q", request.Header.Get("ApiKey"))
		}
		switch request.URL.Path {
		case aiOpenPlatformVideoCreatePath:
			if request.Method != http.MethodPost {
				t.Fatalf("create method = %s", request.Method)
			}
			if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
				t.Fatalf("decode create request: %v", err)
			}
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"code":0,"message":"","data":{"task_id":"video-task-1"},"trace_id":"trace-create"}`))
		case aiOpenPlatformVideoStatusPath:
			if request.Method != http.MethodPost {
				t.Fatalf("status method = %s", request.Method)
			}
			if err := json.NewDecoder(request.Body).Decode(&statusRequest); err != nil {
				t.Fatalf("decode status request: %v", err)
			}
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"code":0,"message":"","data":{"task_id":"video-task-1","status":"succeeded","video_url":"` + server.URL + `/result.mp4","last_frame_url":"https://cdn.example.com/last-frame.png"},"trace_id":"trace-status"}`))
		case "/result.mp4":
			response.Header().Set("Content-Type", "video/mp4")
			_, _ = response.Write([]byte("test-video"))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	result, err := runAIOpenPlatformVideoTask(context.Background(), canvasGenerationInput{
		Prompt: "生成短片",
		Config: providerConfig{
			BaseURL:      server.URL,
			APIKey:       "test-key",
			Model:        "doubao-seedance-2-0-260128",
			Size:         "9:16",
			VQuality:     "1080p",
			VideoSeconds: "5",
		},
	})
	if err != nil {
		t.Fatalf("runAIOpenPlatformVideoTask() error = %v", err)
	}
	if received.Mode != "pro" || received.Ratio != "9:16" || statusRequest.TaskID != "video-task-1" {
		t.Fatalf("create/status request = %#v / %#v", received, statusRequest)
	}
	video, ok := result["video"].(map[string]interface{})
	if !ok {
		t.Fatalf("video result = %#v", result["video"])
	}
	if video["taskId"] != "video-task-1" || video["sourceUrl"] != server.URL+"/result.mp4" {
		t.Fatalf("video metadata = %#v", video)
	}
	if video["lastFrameUrl"] != "https://cdn.example.com/last-frame.png" {
		t.Fatalf("lastFrameUrl = %#v", video["lastFrameUrl"])
	}
}

func TestAIOpenPlatformVideoRequestReportsNativeEnvelopeError(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"code":40001,"message":"resolution is invalid","data":null,"trace_id":"trace-error"}`))
	}))
	defer server.Close()

	var response aiOpenPlatformVideoCreated
	err := requestAIOpenPlatformVideoJSON(
		context.Background(),
		providerConfig{BaseURL: server.URL, APIKey: "test-key"},
		aiOpenPlatformVideoCreatePath,
		aiOpenPlatformVideoCreateRequest{},
		&response,
	)
	if err == nil ||
		!strings.Contains(err.Error(), "40001") ||
		!strings.Contains(err.Error(), "trace-error") ||
		!strings.Contains(err.Error(), "resolution is invalid") {
		t.Fatalf("error = %v", err)
	}
}
