package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestKuaiziSeedance25ClientCreatesAndQueriesWithPublishedHTTPContract(t *testing.T) {
	t.Helper()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		if request.Method != http.MethodPost || request.Header.Get("ApiKey") != "family-key" || request.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request = method:%s headers:%v", request.Method, request.Header)
		}
		payload, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
			return
		}
		switch request.URL.Path {
		case kuaiziSeedance25CreatePath:
			if !strings.Contains(string(payload), `"mode":"seedance2.5"`) || strings.Contains(string(payload), "family-key") {
				t.Errorf("create payload = %s", payload)
			}
			_, _ = response.Write([]byte(`{"code":0,"data":{"task_id":"upstream-task"},"trace_id":"create-trace"}`))
		case kuaiziSeedance25StatusPath:
			if string(payload) != `{"task_id":"upstream-task"}` {
				t.Errorf("status payload = %s", payload)
			}
			_, _ = response.Write([]byte(`{"code":0,"data":{"task_id":"upstream-task","status":"running"},"trace_id":"poll-trace"}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	client := NewKuaiziSeedance25Client(server.Client())
	created, err := client.Create(context.Background(), server.URL, "family-key", kuaiziSeedance25Request{Mode: "seedance2.5", Resolution: "720p", Ratio: "16:9", Duration: 5})
	if err != nil || created.TaskID != "upstream-task" || created.TraceID != "create-trace" {
		t.Fatalf("created = %#v, error = %v", created, err)
	}
	state, err := client.Status(context.Background(), server.URL, "family-key", created.TaskID)
	if err != nil || state.Status != "running" || state.TraceID != "poll-trace" {
		t.Fatalf("state = %#v, error = %v", state, err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d", requests)
	}
}

func TestKuaiziSeedance25ClientFailsClosedWithoutLeakingSecrets(t *testing.T) {
	const secret = "sentinel-family-key"
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "http failure", status: http.StatusBadGateway, body: `upstream echoed ` + secret},
		{name: "oversized success", status: http.StatusOK, body: strings.Repeat("x", kuaiziSeedance25ResponseLimit+1)},
		{name: "invalid success", status: http.StatusOK, body: `{"code":0,"data":null} ` + secret},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(test.status)
				_, _ = response.Write([]byte(test.body))
			}))
			defer server.Close()
			client := NewKuaiziSeedance25Client(server.Client())
			_, err := client.Status(context.Background(), server.URL, secret, "task")
			if err == nil {
				t.Fatal("invalid upstream response was accepted")
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), test.body) {
				t.Fatalf("error leaked upstream data: %v", err)
			}
		})
	}
}

func TestKuaiziSeedance25ClientRejectsSecretReflectedIdentifiers(t *testing.T) {
	const secret = "sentinel-family-key"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"code":0,"data":{"task_id":"sentinel-family-key"},"trace_id":"sentinel-family-key"}`))
	}))
	defer server.Close()

	client := NewKuaiziSeedance25Client(server.Client())
	_, err := client.Create(context.Background(), server.URL, secret, kuaiziSeedance25Request{Mode: "seedance2.5"})
	if err == nil {
		t.Fatal("secret-reflecting task id was accepted")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked secret: %v", err)
	}
}
