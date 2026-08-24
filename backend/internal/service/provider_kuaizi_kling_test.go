package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestKuaiziKlingCreatePayloadMapsDocumentedInputs(t *testing.T) {
	payload, err := kuaiziKlingCreatePayload(canvasGenerationInput{
		Prompt: "保持人物外观，完成一段电影镜头",
		Config: providerConfig{
			Model:              kuaiziKlingModel,
			Size:               "16:9",
			VideoSeconds:       "10",
			VQuality:           "pro",
			VideoGenerateAudio: "false",
		},
		ReferenceImages: []providerMedia{
			{ID: "first", URL: "https://assets.example.com/first.png"},
			{ID: "last", URL: "https://assets.example.com/last.png"},
		},
		Metadata: map[string]interface{}{
			"videoGenerationMode":   "image_reference",
			"videoStartFrameNodeId": "first",
			"videoEndFrameNodeId":   "last",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if payload["model"] != "kling-v3-omni" || payload["prompt"] != "保持人物外观，完成一段电影镜头" || payload["aspect_ratio"] != "16:9" || payload["duration"] != 10 || payload["generate_audio"] != false || payload["kling_mode"] != "pro" {
		t.Fatalf("payload = %#v", payload)
	}
	images, ok := payload["images"].([]map[string]string)
	if !ok || len(images) != 2 {
		t.Fatalf("images = %#v", payload["images"])
	}
	if images[0]["role"] != "first_frame" || images[1]["role"] != "end_frame" {
		t.Fatalf("image roles = %#v", images)
	}
}

func TestKuaiziKlingCreatePayloadMapsReferenceVideoMode(t *testing.T) {
	payload, err := kuaiziKlingCreatePayload(canvasGenerationInput{
		Prompt:          "沿用参考视频中的运动特征",
		Config:          providerConfig{Model: kuaiziKlingModel, Size: "9:16", VideoSeconds: "8", VQuality: "std", VideoGenerateAudio: "false"},
		ReferenceVideos: []providerMedia{{URL: "https://assets.example.com/reference.mp4", DurationMs: 8_000}},
		Metadata:        map[string]interface{}{"videoGenerationMode": "omni_reference"},
	})
	if err != nil {
		t.Fatal(err)
	}
	videos, ok := payload["videos"].([]map[string]string)
	if !ok || len(videos) != 1 || videos[0]["url"] != "https://assets.example.com/reference.mp4" || videos[0]["refer_type"] != "feature" {
		t.Fatalf("videos = %#v", payload["videos"])
	}
}

func TestKuaiziKlingCreatePayloadRejectsDocumentedBoundaryViolations(t *testing.T) {
	base := func() canvasGenerationInput {
		return canvasGenerationInput{Prompt: "生成视频", Config: providerConfig{Model: kuaiziKlingModel, Size: "16:9", VideoSeconds: "5", VQuality: "std", VideoGenerateAudio: "false"}}
	}
	tests := []struct {
		name   string
		mutate func(*canvasGenerationInput)
		want   string
	}{
		{name: "unknown model", mutate: func(input *canvasGenerationInput) { input.Config.Model = "kling-v2" }, want: "仅支持模型 kling-v3-omni"},
		{name: "empty content", mutate: func(input *canvasGenerationInput) { input.Prompt = "" }, want: "至少提供一项"},
		{name: "duration below minimum", mutate: func(input *canvasGenerationInput) { input.Config.VideoSeconds = "2" }, want: "3–15"},
		{name: "invalid ratio", mutate: func(input *canvasGenerationInput) { input.Config.Size = "4:3" }, want: "不支持画面比例"},
		{name: "unsupported mode", mutate: func(input *canvasGenerationInput) { input.Config.VQuality = "1080p" }, want: "仅支持 std、pro 或 4k"},
		{name: "4k with generated audio", mutate: func(input *canvasGenerationInput) {
			input.Config.VQuality = "4k"
			input.Config.VideoGenerateAudio = "true"
		}, want: "4k 不支持同步音频"},
		{name: "audio with video", mutate: func(input *canvasGenerationInput) {
			input.Config.VideoGenerateAudio = "true"
			input.ReferenceVideos = []providerMedia{{URL: "https://assets.example.com/reference.mp4", DurationMs: 5_000}}
		}, want: "参考视频时不能生成同步音频"},
		{name: "4k with video", mutate: func(input *canvasGenerationInput) {
			input.Config.VQuality = "4k"
			input.ReferenceVideos = []providerMedia{{URL: "https://assets.example.com/reference.mp4", DurationMs: 5_000}}
		}, want: "参考视频时不支持 4k"},
		{name: "video duration above maximum", mutate: func(input *canvasGenerationInput) {
			input.Config.VideoSeconds = "11"
			input.ReferenceVideos = []providerMedia{{URL: "https://assets.example.com/reference.mp4", DurationMs: 5_000}}
		}, want: "参考视频任务时长必须为 3–10 秒"},
		{name: "end frame without first frame", mutate: func(input *canvasGenerationInput) {
			input.Prompt = ""
			input.ReferenceImages = []providerMedia{{ID: "last", URL: "https://assets.example.com/last.png"}}
			input.Metadata = map[string]interface{}{"videoEndFrameNodeId": "last"}
		}, want: "尾帧必须同时提供首帧"},
		{name: "too many images", mutate: func(input *canvasGenerationInput) {
			input.ReferenceImages = make([]providerMedia, 8)
			for index := range input.ReferenceImages {
				input.ReferenceImages[index] = providerMedia{URL: "https://assets.example.com/reference.png"}
			}
		}, want: "最多支持 7 张图片"},
		{name: "private image", mutate: func(input *canvasGenerationInput) {
			input.ReferenceImages = []providerMedia{{DataURL: "data:image/png;base64,YQ=="}}
		}, want: "公网可访问 URL"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := base()
			test.mutate(&input)
			if _, err := kuaiziKlingCreatePayload(input); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestKuaiziKlingTaskUsesApiKeyPOSTPollingAndPreservesEvidence(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	var createBody map[string]interface{}
	var statusBody map[string]interface{}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost && request.URL.Path != "/result.mp4" {
			t.Errorf("method = %s", request.Method)
		}
		if strings.HasPrefix(request.URL.Path, "/ai-open-platform-api/") && request.Header.Get("ApiKey") != "kling-key" {
			t.Errorf("ApiKey = %q", request.Header.Get("ApiKey"))
		}
		switch request.URL.Path {
		case kuaiziKlingCreatePath:
			if err := json.NewDecoder(request.Body).Decode(&createBody); err != nil {
				t.Fatal(err)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"code":0,"message":"","data":{"task_id":"kz-cgt-kling-1"},"trace_id":"trace-create"}`))
		case kuaiziKlingStatusPath:
			if err := json.NewDecoder(request.Body).Decode(&statusBody); err != nil {
				t.Fatal(err)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"code":0,"message":"","data":{"task_id":"kz-cgt-kling-1","status":"succeeded","video_url":"` + server.URL + `/result.mp4","duration":5},"trace_id":"trace-status"}`))
		case "/result.mp4":
			writer.Header().Set("Content-Type", "video/mp4")
			_, _ = writer.Write([]byte("video-bytes"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	result, err := runKuaiziKlingTaskWithPollInterval(context.Background(), canvasGenerationInput{
		Prompt: "生成广告视频",
		Config: providerConfig{BaseURL: server.URL, APIKey: "kling-key", Model: kuaiziKlingModel, Size: "16:9", VideoSeconds: "5", VQuality: "std", VideoGenerateAudio: "false"},
	}, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if createBody["model"] != "kling-v3-omni" || statusBody["task_id"] != "kz-cgt-kling-1" {
		t.Fatalf("create=%#v status=%#v", createBody, statusBody)
	}
	video, ok := result["video"].(map[string]interface{})
	if !ok || video["taskId"] != "kz-cgt-kling-1" || video["sourceUrl"] != server.URL+"/result.mp4" || video["traceId"] != "trace-status" || video["durationMs"] != int64(5_000) || !strings.HasPrefix(video["dataUrl"].(string), "data:video/mp4;base64,") {
		t.Fatalf("video = %#v", result["video"])
	}
}

func TestKuaiziKlingTaskPreservesFailedTaskEvidence(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case kuaiziKlingCreatePath:
			_, _ = writer.Write([]byte(`{"code":0,"message":"","data":{"task_id":"kz-cgt-kling-failed"},"trace_id":"trace-create"}`))
		case kuaiziKlingStatusPath:
			_, _ = writer.Write([]byte(`{"code":0,"message":"","data":{"task_id":"kz-cgt-kling-failed","status":"failed","error":"内容生成失败"},"trace_id":"trace-failed"}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	_, err := runKuaiziKlingTaskWithPollInterval(context.Background(), canvasGenerationInput{
		Prompt: "生成广告视频",
		Config: providerConfig{BaseURL: server.URL, APIKey: "kling-key", Model: kuaiziKlingModel, Size: "16:9", VideoSeconds: "5", VQuality: "std", VideoGenerateAudio: "false"},
	}, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "task_id=kz-cgt-kling-failed") || !strings.Contains(err.Error(), "trace_id=trace-failed") || !strings.Contains(err.Error(), "内容生成失败") {
		t.Fatalf("error = %v", err)
	}
}
