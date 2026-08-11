package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
)

func TestSeedance25CreatePayloadMatchesKuaiziContract(t *testing.T) {
	input := seedance25TestInput()
	input.Config.VideoGenerateAudio = "false"
	input.Config.VideoWatermark = "true"
	input.ReferenceImages = []providerMedia{
		{ID: "first", URL: "https://cdn.example.com/first.png", Role: "first_frame"},
		{ID: "last", URL: "https://cdn.example.com/last.png", Role: "last_frame"},
		{ID: "reference", URL: "https://cdn.example.com/reference.png", Role: "reference_image"},
	}
	input.ReferenceVideos = []providerMedia{{URL: "https://cdn.example.com/reference.mp4", Role: "reference_video"}}
	input.ReferenceAudios = []providerMedia{{URL: "https://cdn.example.com/reference.mp3", Role: "reference_audio"}}

	body, err := kuaiziSeedance25Body(input)
	if err != nil {
		t.Fatalf("kuaiziSeedance25Body() error = %v", err)
	}
	if body.Mode != "seedance2.5" || body.Resolution != "720p" || body.Ratio != "16:9" || body.Duration != 4 {
		t.Fatalf("identity/settings = %#v", body)
	}
	if body.GenerateAudio || !body.Watermark || !body.ReturnLastFrame {
		t.Fatalf("flags = %#v", body)
	}
	if len(body.Images) != 3 || body.Images[0].Role != "first_frame" || body.Images[1].Role != "last_frame" || body.Images[2].Role != "reference_image" {
		t.Fatalf("images = %#v", body.Images)
	}
	if len(body.Videos) != 1 || body.Videos[0].Role != "reference_video" || len(body.Audios) != 1 || body.Audios[0].Role != "reference_audio" {
		t.Fatalf("video/audio = %#v / %#v", body.Videos, body.Audios)
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"mode":"seedance2.5"`, `"generate_audio":false`, `"watermark":true`, `"return_last_frame":true`, `"images":`, `"videos":`, `"audios":`} {
		if !strings.Contains(string(encoded), field) {
			t.Fatalf("payload %s missing %s", encoded, field)
		}
	}
}

func TestSeedance25CreatePayloadAcceptsPublishedDimensions(t *testing.T) {
	for _, duration := range []string{"4", "30", "-1"} {
		for _, resolution := range []string{"480p", "720p"} {
			for _, ratio := range []string{"16:9", "4:3", "1:1", "3:4", "9:16", "21:9", "adaptive"} {
				input := seedance25TestInput()
				input.Config.VideoSeconds = duration
				input.Config.VQuality = resolution
				input.Config.Size = ratio
				body, err := kuaiziSeedance25Body(input)
				if err != nil {
					t.Fatalf("duration=%s resolution=%s ratio=%s: %v", duration, resolution, ratio, err)
				}
				if body.Resolution != resolution || body.Ratio != ratio {
					t.Fatalf("duration=%s resolution=%s ratio=%s body=%#v", duration, resolution, ratio, body)
				}
			}
		}
	}
}

func TestSeedance25CreatePayloadEnforcesStructuralBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*canvasGenerationInput)
		wantErr bool
	}{
		{name: "30 images", mutate: func(input *canvasGenerationInput) { input.ReferenceImages = repeatedSeedance25Media(30, "image") }},
		{name: "31 images", mutate: func(input *canvasGenerationInput) { input.ReferenceImages = repeatedSeedance25Media(31, "image") }, wantErr: true},
		{name: "10 videos", mutate: func(input *canvasGenerationInput) { input.ReferenceVideos = repeatedSeedance25Media(10, "video") }},
		{name: "11 videos", mutate: func(input *canvasGenerationInput) { input.ReferenceVideos = repeatedSeedance25Media(11, "video") }, wantErr: true},
		{name: "10 audios", mutate: func(input *canvasGenerationInput) { input.ReferenceAudios = repeatedSeedance25Media(10, "audio") }},
		{name: "11 audios", mutate: func(input *canvasGenerationInput) { input.ReferenceAudios = repeatedSeedance25Media(11, "audio") }, wantErr: true},
		{name: "3 seconds", mutate: func(input *canvasGenerationInput) { input.Config.VideoSeconds = "3" }, wantErr: true},
		{name: "4 seconds", mutate: func(input *canvasGenerationInput) { input.Config.VideoSeconds = "4" }},
		{name: "30 seconds", mutate: func(input *canvasGenerationInput) { input.Config.VideoSeconds = "30" }},
		{name: "31 seconds", mutate: func(input *canvasGenerationInput) { input.Config.VideoSeconds = "31" }, wantErr: true},
		{name: "1080p", mutate: func(input *canvasGenerationInput) { input.Config.VQuality = "1080p" }, wantErr: true},
		{name: "4K", mutate: func(input *canvasGenerationInput) { input.Config.VQuality = "4K" }, wantErr: true},
		{name: "empty URL", mutate: func(input *canvasGenerationInput) { input.ReferenceImages = []providerMedia{{Role: "reference_image"}} }, wantErr: true},
		{name: "http URL", mutate: func(input *canvasGenerationInput) {
			input.ReferenceImages = []providerMedia{{URL: "http://cdn.example.com/reference.png", Role: "reference_image"}}
		}, wantErr: true},
		{name: "private URL", mutate: func(input *canvasGenerationInput) {
			input.ReferenceImages = []providerMedia{{URL: "https://127.0.0.1/reference.png", Role: "reference_image"}}
		}, wantErr: true},
		{name: "unknown image role", mutate: func(input *canvasGenerationInput) {
			input.ReferenceImages = []providerMedia{{URL: "https://cdn.example.com/reference.png", Role: "thumbnail"}}
		}, wantErr: true},
		{name: "unknown video role", mutate: func(input *canvasGenerationInput) {
			input.ReferenceVideos = []providerMedia{{URL: "https://cdn.example.com/reference.mp4", Role: "first_frame"}}
		}, wantErr: true},
		{name: "unknown audio role", mutate: func(input *canvasGenerationInput) {
			input.ReferenceAudios = []providerMedia{{URL: "https://cdn.example.com/reference.mp3", Role: "voiceover"}}
		}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := seedance25TestInput()
			test.mutate(&input)
			_, err := kuaiziSeedance25Body(input)
			if test.wantErr && err == nil {
				t.Fatal("kuaiziSeedance25Body() error = nil")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("kuaiziSeedance25Body() error = %v", err)
			}
		})
	}
}

func TestSeedance25StatusClassificationIsClosedOverKnownStates(t *testing.T) {
	for _, status := range []string{"submitted", "pending", "running"} {
		state, err := classifyKuaiziSeedance25Status(kuaiziSeedance25Status{TaskID: "provider-task", Status: status})
		if err != nil || state.Terminal {
			t.Fatalf("status %s = %#v, %v", status, state, err)
		}
	}
	succeeded, err := classifyKuaiziSeedance25Status(kuaiziSeedance25Status{
		TaskID: "provider-task", Status: "succeeded", VideoURL: "https://cdn.example.com/result.mp4",
		LastFrameURL: "https://cdn.example.com/last.png", Duration: 12, TotalTokens: "12345678901234567890",
	})
	if err != nil || !succeeded.Terminal || !succeeded.Succeeded || succeeded.AssetSourceURL == "" || succeeded.LastFrameURL == "" || succeeded.ActualDurationSeconds != 12 || succeeded.TotalTokens != "12345678901234567890" {
		t.Fatalf("succeeded = %#v, %v", succeeded, err)
	}
	failed, err := classifyKuaiziSeedance25Status(kuaiziSeedance25Status{TaskID: "provider-task", Status: "failed", Error: "provider rejected"})
	if err != nil || !failed.Terminal || failed.Succeeded || failed.FailureReason != "provider rejected" {
		t.Fatalf("failed = %#v, %v", failed, err)
	}
	if _, err := classifyKuaiziSeedance25Status(kuaiziSeedance25Status{TaskID: "provider-task", Status: "mystery"}); err == nil {
		t.Fatal("unknown status was accepted")
	}
	if _, err := classifyKuaiziSeedance25Status(kuaiziSeedance25Status{Status: "running"}); err == nil {
		t.Fatal("missing task ID was accepted")
	}
	if _, err := classifyKuaiziSeedance25Status(kuaiziSeedance25Status{TaskID: "provider-task", Status: "succeeded", VideoURL: "http://cdn.example.com/result.mp4"}); err == nil {
		t.Fatal("unsafe result URL was accepted")
	}
	if _, err := classifyKuaiziSeedance25Status(kuaiziSeedance25Status{TaskID: "provider-task", Status: "succeeded", VideoURL: "https://cdn.example.com/result.mp4", LastFrameURL: ""}); err == nil {
		t.Fatal("missing last frame URL was accepted")
	}
}

func TestSeedance25ClientUsesFrozenSecretAndPreservesTraceFacts(t *testing.T) {
	var receivedKey string
	var receivedStatusRequest kuaiziSeedance25StatusRequest
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		receivedKey = request.Header.Get("ApiKey")
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case kuaiziSeedance25CreatePath:
			_, _ = io.WriteString(writer, `{"code":0,"message":"","data":{"task_id":"provider-task-1"},"trace_id":"trace-create"}`)
		case kuaiziSeedance25StatusPath:
			if err := json.NewDecoder(request.Body).Decode(&receivedStatusRequest); err != nil {
				t.Errorf("decode status request: %v", err)
			}
			_, _ = io.WriteString(writer, `{"code":0,"message":"","data":{"task_id":"provider-task-1","status":"succeeded","video_url":"https://cdn.example.com/result.mp4","last_frame_url":"https://cdn.example.com/last.png","duration":7,"total_tokens":"9007199254740993"},"trace_id":"trace-poll"}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	cipher := NewProviderSecretCipher(t.TempDir())
	ciphertext, err := cipher.encrypt("account", "credential", 1, "frozen-key-v1", true, ErrProviderSecretKeyMissing)
	if err != nil {
		t.Fatal(err)
	}
	runtime := seedance25TestRuntime(server.URL, ciphertext)
	client := NewKuaiziSeedance25Client(server.Client(), cipher)
	created, err := client.Create(context.Background(), runtime, seedance25TestInput())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.TaskID != "provider-task-1" || created.TraceID != "trace-create" || receivedKey != "frozen-key-v1" {
		t.Fatalf("created/key = %#v / %q", created, receivedKey)
	}
	polled, err := client.Status(context.Background(), runtime, created.TaskID)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if polled.TraceID != "trace-poll" || polled.State.TotalTokens != "9007199254740993" || receivedStatusRequest.TaskID != created.TaskID {
		t.Fatalf("polled/request = %#v / %#v", polled, receivedStatusRequest)
	}
}

func TestSeedance25ClientClassifiesBusinessFailureAndCreateUncertainty(t *testing.T) {
	t.Run("business code", func(t *testing.T) {
		const secret = "sentinel-business-secret"
		server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"code":40001,"message":"invalid `+secret+` resolution","data":null,"trace_id":"trace-error"}`)
		}))
		defer server.Close()
		client, runtime := seedance25TestClient(t, server)
		ciphertext, err := client.cipher.Encrypt(runtime.Account.ID, runtime.Credential.ID, runtime.CredentialVersion.Version, secret)
		if err != nil {
			t.Fatal(err)
		}
		runtime.CredentialVersion.KeyCipher = ciphertext
		_, err = client.Create(context.Background(), runtime, seedance25TestInput())
		var providerErr *KuaiziSeedance25Error
		if !errors.As(err, &providerErr) || providerErr.Kind != "business" || providerErr.Code != "40001" || providerErr.TraceID != "trace-error" {
			t.Fatalf("error = %#v", err)
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("business error leaked secret: %v", err)
		}
	})

	t.Run("request written then response timeout", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			time.Sleep(100 * time.Millisecond)
		}))
		defer server.Close()
		client, runtime := seedance25TestClient(t, server)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		_, err := client.Create(ctx, runtime, seedance25TestInput())
		var uncertain *KuaiziSeedance25CreateUncertainError
		if !errors.As(err, &uncertain) {
			t.Fatalf("error = %T %v, want create uncertainty", err, err)
		}
	})
}

func TestSeedance25ClientRejectsMissingTaskIDAndPollingDeadline(t *testing.T) {
	t.Run("missing task id", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"code":0,"message":"","data":{},"trace_id":"trace-create"}`)
		}))
		defer server.Close()
		client, runtime := seedance25TestClient(t, server)
		_, err := client.Create(context.Background(), runtime, seedance25TestInput())
		var uncertain *KuaiziSeedance25CreateUncertainError
		if !errors.As(err, &uncertain) {
			t.Fatalf("missing task id error = %T %v, want create uncertainty", err, err)
		}
	})

	t.Run("poll timeout", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"code":0,"message":"","data":{"task_id":"provider-task","status":"pending"},"trace_id":"trace-poll"}`)
		}))
		defer server.Close()
		client, runtime := seedance25TestClient(t, server)
		ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
		defer cancel()
		if _, err := client.PollUntilTerminal(ctx, runtime, "provider-task", time.Millisecond, nil); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("PollUntilTerminal() error = %v", err)
		}
	})
}

func seedance25TestInput() canvasGenerationInput {
	return canvasGenerationInput{
		Mode:   "video",
		Prompt: "电影感追逐镜头",
		Config: providerConfig{Model: "kuaizi-seedance-2.5", Size: "16:9", VQuality: "720p", VideoSeconds: "4", VideoGenerateAudio: "true"},
	}
}

func repeatedSeedance25Media(count int, kind string) []providerMedia {
	items := make([]providerMedia, count)
	for index := range items {
		items[index] = providerMedia{URL: "https://cdn.example.com/reference-" + kind + ".bin"}
		switch kind {
		case "image":
			items[index].Role = "reference_image"
		case "video":
			items[index].Role = "reference_video"
		case "audio":
			items[index].Role = "reference_audio"
		}
	}
	return items
}

func seedance25TestRuntime(baseURL string, ciphertext string) *ProviderTaskRuntime {
	return &ProviderTaskRuntime{
		Account:           model.ProviderAccount{ID: "account", ProviderKind: "kuaizi", Enabled: true},
		EndpointVersion:   model.ProviderEndpointVersion{ID: "endpoint-v1", ProviderAccountID: "account", BaseURL: baseURL, Version: 1},
		Credential:        model.ProviderCredential{ID: "credential", ProviderAccountID: "account", Family: "seedance", Enabled: true, ConcurrencyLimit: 1},
		CredentialVersion: model.ProviderCredentialVersion{ID: "credential-v1", ProviderCredentialID: "credential", Version: 1, KeyCipher: ciphertext},
		ChannelModel:      model.ChannelModel{ID: "channel-model", ProviderCredentialID: "credential", ModelKey: "kuaizi-seedance-2.5", Capability: "video", Enabled: true},
		ProviderFact:      model.ProviderTaskFact{TaskID: "task", ProviderAccountID: "account", ProviderEndpointVersionID: "endpoint-v1", ProviderCredentialID: "credential", ProviderCredentialVersionID: "credential-v1", ChannelModelID: "channel-model"},
	}
}

func seedance25TestClient(t *testing.T, server *httptest.Server) (*KuaiziSeedance25Client, *ProviderTaskRuntime) {
	t.Helper()
	cipher := NewProviderSecretCipher(t.TempDir())
	ciphertext, err := cipher.encrypt("account", "credential", 1, "frozen-key-v1", true, ErrProviderSecretKeyMissing)
	if err != nil {
		t.Fatal(err)
	}
	return NewKuaiziSeedance25Client(server.Client(), cipher), seedance25TestRuntime(server.URL, ciphertext)
}
