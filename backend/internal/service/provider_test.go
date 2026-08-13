package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const testReferenceImageDataURL = "data:image/png;base64,aGVsbG8="

func TestRunAIOpenPlatformVolcengineVideoTaskUsesCompatibleContract(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case aiOpenPlatformVolcengineCreatePath:
			if request.Header.Get("Authorization") != "Bearer test-key" {
				t.Fatalf("create Authorization = %q", request.Header.Get("Authorization"))
			}
			if request.Method != http.MethodPost {
				t.Fatalf("create method = %s", request.Method)
			}
			var payload map[string]interface{}
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatalf("decode create payload: %v", err)
			}
			if payload["model"] != "doubao-seedance-2-0-fast-260128" ||
				payload["resolution"] != "720p" ||
				payload["ratio"] != "16:9" ||
				payload["duration"] != float64(6) ||
				payload["return_last_frame"] != true {
				t.Fatalf("create payload = %#v", payload)
			}
			if payload["watermark"] != false {
				t.Fatalf("frozen watermark = %#v, want false", payload["watermark"])
			}
			content, ok := payload["content"].([]interface{})
			if !ok || len(content) != 1 {
				t.Fatalf("content = %#v", payload["content"])
			}
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"id":"kz-cgt-test","status":"queued"}`))
		case aiOpenPlatformVolcenginePollPath + "kz-cgt-test":
			if request.Header.Get("Authorization") != "Bearer test-key" {
				t.Fatalf("poll Authorization = %q", request.Header.Get("Authorization"))
			}
			if request.Method != http.MethodGet {
				t.Fatalf("poll method = %s", request.Method)
			}
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"id":"kz-cgt-test","status":"succeeded","content":{"video_url":"` + server.URL + `/temporary.mp4","kz_video_url":"` + server.URL + `/persistent.mp4","last_frame_url":"` + server.URL + `/last-frame.png"}}`))
		case "/persistent.mp4":
			response.Header().Set("Content-Type", "video/mp4")
			_, _ = response.Write([]byte("video-bytes"))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	result, err := runAIOpenPlatformVolcengineVideoTask(context.Background(), canvasGenerationInput{
		Prompt:    "海面缓慢起伏",
		Watermark: taskWatermarkRuntime{Capability: model.WatermarkCapabilityControlled, Directive: model.WatermarkDirectiveWithoutWatermark, Parameter: boolPointer(false)},
		Config: providerConfig{
			BaseURL:            server.URL,
			APIKey:             "test-key",
			InterfaceType:      string(model.ChannelInterfaceAIOpenVideoVolcengine),
			Model:              "doubao-seedance-2-0-fast-260128",
			Size:               "16:9",
			VQuality:           "720",
			VideoSeconds:       "6",
			VideoGenerateAudio: "true",
		},
	})
	if err != nil {
		t.Fatalf("runAIOpenPlatformVolcengineVideoTask() error = %v", err)
	}
	video, ok := result["video"].(map[string]interface{})
	if !ok {
		t.Fatalf("result = %#v", result)
	}
	if video["taskId"] != "kz-cgt-test" ||
		video["sourceUrl"] != server.URL+"/persistent.mp4" ||
		video["lastFrameUrl"] != server.URL+"/last-frame.png" {
		t.Fatalf("video = %#v", video)
	}
}

func TestTaskWatermarkRuntimeRejectsInconsistentSnapshots(t *testing.T) {
	value := true
	withoutWatermark := false
	tests := []model.Task{
		{WatermarkCapability: model.WatermarkCapabilityControlled, WatermarkDirective: model.WatermarkDirectiveWithWatermark},
		{WatermarkCapability: model.WatermarkCapabilityUnsupported, WatermarkDirective: model.WatermarkDirectiveProviderDefault, WatermarkParameterApplied: true, WatermarkParameterValue: &value},
		{WatermarkCapability: model.WatermarkCapabilityNotApplicable, WatermarkDirective: model.WatermarkDirectiveWithoutWatermark},
		{WatermarkCapability: model.WatermarkCapabilityControlled, WatermarkDirective: model.WatermarkDirectiveWithoutWatermark, WatermarkParameterApplied: true, WatermarkParameterValue: &withoutWatermark},
		{WatermarkCapability: model.WatermarkCapabilityControlled, WatermarkDirective: model.WatermarkDirectiveWithWatermark, WatermarkParameterApplied: true, WatermarkParameterValue: &value, WatermarkPolicyPublicationID: "unexpected", WatermarkPolicyVersion: 1},
		{WatermarkCapability: model.WatermarkCapabilityUnsupported, WatermarkDirective: model.WatermarkDirectiveProviderDefault, WatermarkPolicyPublicationID: "unexpected", WatermarkPolicyVersion: 1},
	}
	for index, task := range tests {
		if _, err := taskWatermarkRuntimeFromTask(task); err == nil {
			t.Fatalf("inconsistent snapshot %d was accepted: %#v", index, task)
		}
	}
}

func TestControlledProviderSendsFrozenWatermarkValue(t *testing.T) {
	for _, value := range []bool{true, false} {
		input := canvasGenerationInput{Watermark: taskWatermarkRuntime{Capability: model.WatermarkCapabilityControlled, Parameter: boolPointer(value)}}
		body := map[string]interface{}{}
		insertFrozenWatermark(body, "watermark", input.Watermark)
		if body["watermark"] != value {
			t.Fatalf("watermark = %#v, want %v", body["watermark"], value)
		}
	}
}

func boolPointer(value bool) *bool { return &value }

func TestRunAIOpenPlatformVolcengineVideoTaskRejectsInvalidParameters(t *testing.T) {
	tests := []struct {
		name       string
		model      string
		ratio      string
		resolution string
		duration   string
	}{
		{
			name:       "fast rejects 4k",
			model:      "doubao-seedance-2-0-fast-260128",
			ratio:      "16:9",
			resolution: "4k",
			duration:   "6",
		},
		{
			name:       "rejects unsupported ratio",
			model:      "doubao-seedance-2-0-260128",
			ratio:      "2:1",
			resolution: "1080p",
			duration:   "6",
		},
		{
			name:       "rejects duration outside contract",
			model:      "doubao-seedance-2-0-260128",
			ratio:      "16:9",
			resolution: "1080p",
			duration:   "16",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := runAIOpenPlatformVolcengineVideoTask(context.Background(), canvasGenerationInput{
				Prompt: "海面缓慢起伏",
				Config: providerConfig{
					BaseURL:       "https://video.example.com",
					APIKey:        "test-key",
					InterfaceType: string(model.ChannelInterfaceAIOpenVideoVolcengine),
					Model:         test.model,
					Size:          test.ratio,
					VQuality:      test.resolution,
					VideoSeconds:  test.duration,
				},
			})
			if err == nil {
				t.Fatal("runAIOpenPlatformVolcengineVideoTask() error = nil")
			}
		})
	}
}

func TestRunAIOpenPlatformVolcengineVideoTaskRejectsUnknownStatus(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case aiOpenPlatformVolcengineCreatePath:
			_, _ = response.Write([]byte(`{"id":"kz-cgt-unknown","status":"queued"}`))
		case aiOpenPlatformVolcenginePollPath + "kz-cgt-unknown":
			_, _ = response.Write([]byte(`{"id":"kz-cgt-unknown","status":"unexpected"}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	_, err := runAIOpenPlatformVolcengineVideoTask(context.Background(), canvasGenerationInput{
		Prompt: "海面缓慢起伏",
		Config: providerConfig{
			BaseURL:       server.URL,
			APIKey:        "test-key",
			InterfaceType: string(model.ChannelInterfaceAIOpenVideoVolcengine),
			Model:         "doubao-seedance-2-0-fast-260128",
			Size:          "16:9",
			VQuality:      "720p",
			VideoSeconds:  "6",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "未知状态") {
		t.Fatalf("runAIOpenPlatformVolcengineVideoTask() error = %v", err)
	}
}

func TestWriteMediaPartSanitizesFilenameAndSetsMimeType(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writeMediaPart(writer, "image", providerMedia{ID: "image-1", Name: "提示词\n带换行.png", Type: "image/png", DataURL: testReferenceImageDataURL}); err != nil {
		t.Fatalf("writeMediaPart() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("multipart.Writer.Close() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://example.test", bytes.NewReader(body.Bytes()))
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if err := request.ParseMultipartForm(1 << 20); err != nil {
		t.Fatalf("ParseMultipartForm() error = %v", err)
	}
	files := request.MultipartForm.File["image"]
	if len(files) != 1 {
		t.Fatalf("image files = %d, want 1", len(files))
	}
	file := files[0]
	if file.Filename != "reference-image-1.png" || strings.ContainsAny(file.Filename, "\r\n") {
		t.Fatalf("filename = %q", file.Filename)
	}
	if got := file.Header.Get("Content-Type"); got != "image/png" {
		t.Fatalf("part Content-Type = %q, want image/png", got)
	}
	opened, err := file.Open()
	if err != nil {
		t.Fatalf("file.Open() error = %v", err)
	}
	defer opened.Close()
	data, err := io.ReadAll(opened)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("file data = %q, want hello", data)
	}
}

func TestProviderHTTPErrorWarnsAboutUncertain524Billing(t *testing.T) {
	message := (providerHTTPError{StatusCode: 524, Status: "524 A Timeout Occurred"}).Error()
	if !strings.Contains(message, "可能仍在服务端执行并产生费用") || !strings.Contains(message, "请勿立即重试") {
		t.Fatalf("providerHTTPError.Error() = %q", message)
	}
}

func TestDoBinaryRejectsOversizedProviderResponse(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.FormatInt(maxProviderResponseBytes+1, 10))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	_, _, err := getExternalBinary(context.Background(), server.URL)
	if err == nil || !strings.Contains(err.Error(), "超过 64MB") {
		t.Fatalf("getExternalBinary() error = %v", err)
	}
}

func TestTextResponseInputIncludesReferenceImages(t *testing.T) {
	input := canvasGenerationInput{
		Prompt: "describe this image",
		Config: providerConfig{SystemPrompt: "answer in Chinese"},
		ReferenceImages: []providerMedia{
			{ID: "image-1", Name: "image.png", Type: "image/png", DataURL: testReferenceImageDataURL},
		},
	}

	value, err := textResponseInput(input)
	if err != nil {
		t.Fatalf("textResponseInput() error = %v", err)
	}
	messages, ok := value.([]map[string]interface{})
	if !ok {
		t.Fatalf("textResponseInput() = %T, want []map[string]interface{}", value)
	}
	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(messages))
	}
	if messages[0]["role"] != "system" || messages[0]["content"] != "answer in Chinese" {
		t.Fatalf("system message = %#v", messages[0])
	}
	content, ok := messages[1]["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("user content = %T, want []map[string]interface{}", messages[1]["content"])
	}
	if len(content) != 2 {
		t.Fatalf("len(content) = %d, want 2", len(content))
	}
	if content[0]["type"] != "input_text" || content[0]["text"] != "describe this image" {
		t.Fatalf("text content = %#v", content[0])
	}
	if content[1]["type"] != "input_image" || content[1]["image_url"] != testReferenceImageDataURL {
		t.Fatalf("image content = %#v", content[1])
	}
}

func TestTextChatContentIncludesReferenceImages(t *testing.T) {
	input := canvasGenerationInput{
		Prompt: "describe this image",
		ReferenceImages: []providerMedia{
			{ID: "image-1", Name: "image.png", Type: "image/png", DataURL: testReferenceImageDataURL},
		},
	}

	value, err := textChatContent(input)
	if err != nil {
		t.Fatalf("textChatContent() error = %v", err)
	}
	content, ok := value.([]map[string]interface{})
	if !ok {
		t.Fatalf("textChatContent() = %T, want []map[string]interface{}", value)
	}
	if len(content) != 2 {
		t.Fatalf("len(content) = %d, want 2", len(content))
	}
	if content[0]["type"] != "text" || content[0]["text"] != "describe this image" {
		t.Fatalf("text content = %#v", content[0])
	}
	imageURL, ok := content[1]["image_url"].(map[string]interface{})
	if !ok {
		t.Fatalf("image_url = %T, want map[string]interface{}", content[1]["image_url"])
	}
	if content[1]["type"] != "image_url" || imageURL["url"] != testReferenceImageDataURL {
		t.Fatalf("image content = %#v", content[1])
	}
}

func TestTextReferenceImageRejectsInternalAssetURL(t *testing.T) {
	_, err := textResponseInput(canvasGenerationInput{
		Prompt: "describe this image",
		ReferenceImages: []providerMedia{
			{ID: "image-1", Name: "image.png", URL: "asset://local-image"},
		},
	})
	if err == nil {
		t.Fatal("textResponseInput() error = nil, want error")
	}
}

func TestSeedanceVideosBodyUsesVideosEndpointFields(t *testing.T) {
	body, err := seedanceVideosBody(canvasGenerationInput{
		Prompt: "make it move",
		Config: providerConfig{
			Model:              "seedance-2.0-mini-480p",
			Size:               "9:16",
			VideoSeconds:       "8",
			VideoGenerateAudio: "true",
		},
		ReferenceImages: []providerMedia{
			{ID: "image-1", DataURL: testReferenceImageDataURL},
			{ID: "image-2", DataURL: "data:image/png;base64,d29ybGQ="},
		},
		ReferenceVideos: []providerMedia{{ID: "video-1", URL: "https://example.com/ref.mp4"}},
		ReferenceAudios: []providerMedia{{ID: "audio-1", DataURL: "data:audio/mpeg;base64,AAAA"}},
	})
	if err != nil {
		t.Fatalf("seedanceVideosBody() error = %v", err)
	}
	if body["model"] != "seedance-2.0-mini-480p" {
		t.Fatalf("model = %#v", body["model"])
	}
	if body["aspect_ratio"] != "9:16" || body["duration"] != 8 {
		t.Fatalf("size fields = %#v %#v", body["aspect_ratio"], body["duration"])
	}
	if body["generate_audio"] != true {
		t.Fatalf("generate_audio = %#v, want true", body["generate_audio"])
	}
	if body["image_url"] != testReferenceImageDataURL {
		t.Fatalf("image_url = %#v", body["image_url"])
	}
	referenceImages, ok := body["reference_image_urls"].([]string)
	if !ok || len(referenceImages) != 1 || referenceImages[0] != "data:image/png;base64,d29ybGQ=" {
		t.Fatalf("reference_image_urls = %#v", body["reference_image_urls"])
	}
	referenceVideos, ok := body["reference_videos"].([]string)
	if !ok || len(referenceVideos) != 1 || referenceVideos[0] != "https://example.com/ref.mp4" {
		t.Fatalf("reference_videos = %#v", body["reference_videos"])
	}
	referenceAudios, ok := body["reference_audios"].([]string)
	if !ok || len(referenceAudios) != 1 || referenceAudios[0] != "data:audio/mpeg;base64,AAAA" {
		t.Fatalf("reference_audios = %#v", body["reference_audios"])
	}
	if body["content"] != nil || body["ratio"] != nil {
		t.Fatalf("unexpected agent-plan fields in body: %#v", body)
	}
}

func TestSeedanceVideosBodyHonorsGenerateAudio(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		want  bool
	}{
		{name: "default enabled", want: true},
		{name: "explicit enabled", value: "true", want: true},
		{name: "explicit disabled", value: "false", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, err := seedanceVideosBody(canvasGenerationInput{
				Prompt: "make it move",
				Config: providerConfig{
					Model:              "seedance-2.0-mini-480p",
					VideoGenerateAudio: test.value,
				},
			})
			if err != nil {
				t.Fatalf("seedanceVideosBody() error = %v", err)
			}
			if body["generate_audio"] != test.want {
				t.Fatalf("generate_audio = %#v, want %v", body["generate_audio"], test.want)
			}
		})
	}
}

func TestSeedanceVideosBodyUsesOrderedFrameImageURLsWhenConfigured(t *testing.T) {
	body, err := seedanceVideosBody(canvasGenerationInput{
		Prompt: "make it move",
		Config: providerConfig{Model: "seedance-2.0-mini-480p"},
		ReferenceImages: []providerMedia{
			{ID: "character", DataURL: "data:image/png;base64,Y2hhcmFjdGVy"},
			{ID: "end-frame", DataURL: "data:image/png;base64,d29ybGQ="},
			{ID: "front-frame", DataURL: testReferenceImageDataURL},
		},
		Metadata: map[string]interface{}{"videoStartFrameNodeId": "front-frame", "videoEndFrameNodeId": "end-frame"},
	})
	if err != nil {
		t.Fatalf("seedanceVideosBody() error = %v", err)
	}
	imageURLs, ok := body["image_urls"].([]string)
	if !ok || len(imageURLs) != 3 {
		t.Fatalf("image_urls = %#v", body["image_urls"])
	}
	want := []string{testReferenceImageDataURL, "data:image/png;base64,d29ybGQ=", "data:image/png;base64,Y2hhcmFjdGVy"}
	for index := range want {
		if imageURLs[index] != want[index] {
			t.Fatalf("image_urls = %#v, want %#v", imageURLs, want)
		}
	}
	if body["image_url"] != nil || body["reference_image_urls"] != nil {
		t.Fatalf("unexpected legacy image fields in body: %#v", body)
	}
	if prompt := body["prompt"]; prompt != "make it move" {
		t.Fatalf("prompt = %#v", body["prompt"])
	}
}

func TestRunVideoTaskUsesNewAPIForAnyVideoModel(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	paths := make([]string, 0, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/videos":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("parse create body: %v", err)
			}
			if r.FormValue("model") != "custom-video-v1" || r.FormValue("prompt") != "make it move" {
				t.Errorf("create form = %#v", r.MultipartForm.Value)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"video-1","status":"queued"}`))
		case "GET /v1/videos/video-1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"video-1","status":"completed"}`))
		case "GET /v1/videos/video-1/content":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("video"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, err := runVideoTask(context.Background(), canvasGenerationInput{
		Prompt: "make it move",
		Config: providerConfig{BaseURL: server.URL + "/v1", APIKey: "test-key", Model: "custom-video-v1"},
	})
	if err != nil {
		t.Fatalf("runVideoTask() error = %v", err)
	}
	video, ok := result["video"].(map[string]interface{})
	if !ok || video["dataUrl"] != "data:video/mp4;base64,dmlkZW8=" {
		t.Fatalf("video = %#v", result["video"])
	}
	want := "POST /v1/videos,GET /v1/videos/video-1,GET /v1/videos/video-1/content"
	if got := strings.Join(paths, ","); got != want {
		t.Fatalf("paths = %q, want %q", got, want)
	}
}

func TestRunVideoTaskUsesNestedURLBeforeResultURL(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	paths := make([]string, 0, 3)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/videos":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"task_id":"video-1","status":"queued"}}`))
		case "GET /v1/videos/video-1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":"success","data":{"task_id":"video-1","status":"SUCCESS","result_url":"` + server.URL + `/v1/videos/video-1/content","data":{"status":"completed","url":"` + server.URL + `/files/video.mp4"}}}`))
		case "GET /files/video.mp4":
			if authorization := r.Header.Get("Authorization"); authorization != "" {
				t.Errorf("file Authorization = %q, want empty", authorization)
			}
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("video"))
		case "GET /v1/videos/video-1/content":
			http.Error(w, "forbidden", http.StatusForbidden)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, err := runVideoTask(context.Background(), canvasGenerationInput{
		Prompt: "make it move",
		Config: providerConfig{BaseURL: server.URL, APIKey: "test-key", Model: "grok-imagine-video-1.5-1080p", VideoSeconds: "15"},
	})
	if err != nil {
		t.Fatalf("runVideoTask() error = %v", err)
	}
	video, ok := result["video"].(map[string]interface{})
	if !ok || video["dataUrl"] != "data:video/mp4;base64,dmlkZW8=" {
		t.Fatalf("video = %#v", result["video"])
	}
	want := "POST /v1/videos,GET /v1/videos/video-1,GET /files/video.mp4"
	if got := strings.Join(paths, ","); got != want {
		t.Fatalf("paths = %q, want %q", got, want)
	}
}

func TestRunVideoTaskUsesJSONForGrokVideo(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/videos":
			if contentType := r.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
				t.Errorf("Content-Type = %q, want application/json", contentType)
			}
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if body["model"] != "grok-video" || body["prompt"] != "make it move" {
				t.Errorf("request body = %#v", body)
			}
			if body["image"] != testReferenceImageDataURL {
				t.Errorf("image = %#v", body["image"])
			}
			images, ok := body["images"].([]interface{})
			if !ok || len(images) != 1 || images[0] != testReferenceImageDataURL {
				t.Errorf("images = %#v", body["images"])
			}
			_, _ = w.Write([]byte(`{"id":"video-1","status":"queued"}`))
		case "GET /v1/videos/video-1":
			_, _ = w.Write([]byte(`{"id":"video-1","status":"completed"}`))
		case "GET /v1/videos/video-1/content":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("video"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, err := runVideoTask(context.Background(), canvasGenerationInput{
		Prompt:          "make it move",
		Config:          providerConfig{BaseURL: server.URL + "/v1", APIKey: "test-key", Model: "grok-video", VideoSeconds: "10"},
		ReferenceImages: []providerMedia{{ID: "image-1", DataURL: testReferenceImageDataURL}},
		Metadata:        map[string]interface{}{"videoEditOperation": "image_to_video"},
	})
	if err != nil {
		t.Fatalf("runVideoTask() error = %v", err)
	}
	video, ok := result["video"].(map[string]interface{})
	if !ok || video["dataUrl"] != "data:video/mp4;base64,dmlkZW8=" {
		t.Fatalf("video = %#v", result["video"])
	}
}

func TestRunVideoTaskUsesXAIVideoGenerationEndpoint(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	paths := make([]string, 0, 3)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/videos/generations":
			if contentType := r.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
				t.Errorf("Content-Type = %q, want application/json", contentType)
			}
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if body["model"] != "grok-imagine-video-1.5" || body["prompt"] != "make it move" {
				t.Errorf("request body = %#v", body)
			}
			if body["duration"] != float64(10) || body["aspect_ratio"] != "1:1" || body["resolution"] != "720p" {
				t.Errorf("xAI settings = %#v", body)
			}
			for _, legacyField := range []string{"seconds", "size", "images"} {
				if _, exists := body[legacyField]; exists {
					t.Errorf("request body includes legacy field %q: %#v", legacyField, body)
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"request_id":"video-1"}`))
		case "GET /v1/videos/video-1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"done","video":{"url":"` + server.URL + `/files/video.mp4"}}`))
		case "GET /files/video.mp4":
			if authorization := r.Header.Get("Authorization"); authorization != "" {
				t.Errorf("file Authorization = %q, want empty", authorization)
			}
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("video"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, err := runVideoTask(context.Background(), canvasGenerationInput{
		Prompt: "make it move",
		Config: providerConfig{
			BaseURL:       server.URL + "/v1",
			APIKey:        "test-key",
			Model:         "grok-imagine-video-1.5",
			InterfaceType: "xai-video",
			VideoSeconds:  "10",
			Size:          "1:1",
			VQuality:      "720",
		},
	})
	if err != nil {
		t.Fatalf("runVideoTask() error = %v", err)
	}
	video, ok := result["video"].(map[string]interface{})
	if !ok || video["dataUrl"] != "data:video/mp4;base64,dmlkZW8=" {
		t.Fatalf("video = %#v", result["video"])
	}
	want := "POST /v1/videos/generations,GET /v1/videos/video-1,GET /files/video.mp4"
	if got := strings.Join(paths, ","); got != want {
		t.Fatalf("paths = %q, want %q", got, want)
	}
}

func TestXAIVideoBodyUsesOfficialImageShapeAndNormalizesSettings(t *testing.T) {
	body, err := grokVideoBody(canvasGenerationInput{
		Prompt: "make it move",
		Config: providerConfig{
			Model:         "grok-imagine-video-1.5",
			InterfaceType: "xai-video",
			VideoSeconds:  "20",
			Size:          "1024x1792",
			VQuality:      "1080",
		},
		ReferenceImages: []providerMedia{{ID: "image-1", DataURL: testReferenceImageDataURL}},
		Metadata:        map[string]interface{}{"videoEditOperation": "image_to_video"},
	})
	if err != nil {
		t.Fatalf("grokVideoBody() error = %v", err)
	}
	if body["duration"] != 15 || body["aspect_ratio"] != "9:16" || body["resolution"] != "1080p" {
		t.Fatalf("xAI settings = %#v", body)
	}
	image, ok := body["image"].(map[string]interface{})
	if !ok || image["url"] != testReferenceImageDataURL {
		t.Fatalf("image = %#v", body["image"])
	}
	for _, legacyField := range []string{"seconds", "size", "images"} {
		if _, exists := body[legacyField]; exists {
			t.Fatalf("body includes legacy field %q: %#v", legacyField, body)
		}
	}
}

func TestXAIVideoBodyRejectsMultipleStartImages(t *testing.T) {
	_, err := grokVideoBody(canvasGenerationInput{
		Config: providerConfig{Model: "grok-imagine-video-1.5", InterfaceType: "xai-video"},
		ReferenceImages: []providerMedia{
			{ID: "image-1", DataURL: testReferenceImageDataURL},
			{ID: "image-2", DataURL: testReferenceImageDataURL},
		},
		Metadata: map[string]interface{}{"videoEditOperation": "image_to_video"},
	})
	if err == nil || !strings.Contains(err.Error(), "只支持 1 张起始图") {
		t.Fatalf("grokVideoBody() error = %v", err)
	}
}

func TestNewAPIVideoPromptKeepsTextOnlyPromptUnchanged(t *testing.T) {
	input := canvasGenerationInput{
		Prompt: "make it move",
	}
	if prompt := newAPIVideoPromptText(input); prompt != "make it move" {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestVideoProviderPromptsKeepReferencePromptUnchanged(t *testing.T) {
	input := canvasGenerationInput{
		Prompt:          "镜头缓慢前推，人物走向门口",
		ReferenceImages: []providerMedia{{ID: "image-1", DataURL: testReferenceImageDataURL}},
		Metadata:        map[string]interface{}{"videoEditOperation": "image_to_video"},
	}
	for name, prompt := range map[string]string{
		"newapi":           newAPIVideoPromptText(input),
		"seedance-content": seedancePromptText(input),
		"seedance-videos":  seedanceVideosPromptText(input),
	} {
		if prompt != input.Prompt {
			t.Fatalf("%s prompt = %q", name, prompt)
		}
	}
}

func TestNewAPIVideoOmitsImagesForTextToVideoOperation(t *testing.T) {
	input := canvasGenerationInput{
		Prompt: "make it move with the described character",
		ReferenceImages: []providerMedia{
			{ID: "image-1", DataURL: testReferenceImageDataURL},
		},
		Metadata: map[string]interface{}{"videoEditOperation": "text_to_video"},
	}
	if shouldSendNewAPIVideoImages(input) {
		t.Fatal("shouldSendNewAPIVideoImages() = true, want false")
	}
	if prompt := newAPIVideoPromptText(input); strings.Contains(prompt, "@image1") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestSeedanceVideosBodyRequiresImageForVideoOrAudioReferences(t *testing.T) {
	_, err := seedanceVideosBody(canvasGenerationInput{
		Prompt:          "make it move",
		Config:          providerConfig{Model: "seedance-2.0-mini-480p"},
		ReferenceVideos: []providerMedia{{ID: "video-1", URL: "https://example.com/ref.mp4"}},
	})
	if err == nil {
		t.Fatal("seedanceVideosBody() error = nil, want error")
	}
}

func TestArkPlanConfigStaysSeparateFromSeedanceVideosEndpoint(t *testing.T) {
	config := providerConfig{BaseURL: "https://ark.cn-beijing.volces.com/api/plan/v3", Model: "seedance-2.0-pro"}
	if !isArkPlanVideoConfig(config) {
		t.Fatal("isArkPlanVideoConfig() = false, want true")
	}
	if !isSeedanceVideoConfig(config) {
		t.Fatal("isSeedanceVideoConfig() = false, want true")
	}
}

func TestValidateGenerationInterfaceRejectsMismatchedType(t *testing.T) {
	if err := validateGenerationInterface("video", "chat-completion"); err == nil {
		t.Fatal("validateGenerationInterface() error = nil")
	}
	if err := validateGenerationInterface("video", "newapi-channel-1"); err == nil {
		t.Fatal("validateGenerationInterface() accepted removed newapi-channel-1")
	}
	if err := validateGenerationInterface("video", "newapi-channel-2"); err == nil {
		t.Fatal("validateGenerationInterface() accepted removed newapi-channel-2")
	}
	if err := validateGenerationInterface("video", "xai-video"); err != nil {
		t.Fatalf("validateGenerationInterface() error = %v", err)
	}
}

func TestProcessTaskValidatesInterfaceBeforeHydratingMedia(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ModelChannel{}); err != nil {
		t.Fatal(err)
	}
	channel := model.ModelChannel{
		ID:            "channel-1",
		Scope:         model.ChannelScopeSystem,
		Enabled:       true,
		Name:          "system channel",
		BaseURL:       server.URL + "/v1",
		APIKey:        "system-key",
		APIFormat:     "openai",
		InterfaceType: model.ChannelInterfaceChatCompletion,
		ModelsJSON:    `["text-model"]`,
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	input := canvasGenerationInput{
		Mode:            "video",
		Prompt:          "make it move",
		Config:          providerConfig{ChannelID: channel.ID, Model: "text-model"},
		ReferenceImages: []providerMedia{{StorageKey: "resource:missing"}},
	}
	raw, _ := json.Marshal(input)
	_, err = (&Service{repo: repository.New(db)}).processCanvasGenerationTask(context.Background(), model.Task{
		UserID: "user-1", Type: "video_generate", Prompt: "make it move", InputJSON: string(raw),
		WatermarkCapability: model.WatermarkCapabilityUnsupported, WatermarkDirective: model.WatermarkDirectiveProviderDefault,
	})
	if err == nil || !strings.Contains(err.Error(), "不支持video生成") {
		t.Fatalf("processCanvasGenerationTask() error = %v", err)
	}
}
