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
		{ID: "first", URL: "https://cdn.example.com/first.png", StorageKey: "resource:first", Role: "first_frame"},
		{ID: "last", URL: "https://cdn.example.com/last.png", StorageKey: "resource:last", Role: "last_frame"},
		{ID: "reference", URL: "https://cdn.example.com/reference.png", StorageKey: "resource:reference", Role: "reference_image"},
	}
	input.ReferenceVideos = []providerMedia{{URL: "https://cdn.example.com/reference.mp4", StorageKey: "resource:video", Role: "reference_video"}}
	input.ReferenceAudios = []providerMedia{{URL: "https://cdn.example.com/reference.mp3", StorageKey: "resource:audio", Role: "reference_audio"}}

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
		{name: "arbitrary public https URL", mutate: func(input *canvasGenerationInput) {
			input.ReferenceImages = []providerMedia{{URL: "https://cdn.example.com/reference.png", Role: "reference_image"}}
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
		LastFrameURL: "https://cdn.example.com/last.png", Duration: 12, TotalTokens: "12345678901234567890", TokensPresent: true,
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
	ciphertext, err := cipher.encrypt("account", "credential", "credential-v1", "frozen-key-v1", true, ErrProviderSecretKeyMissing)
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
		ciphertext, err := client.cipher.Encrypt(runtime.Account.ID, runtime.Credential.ID, runtime.CredentialVersion.ID, secret)
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

func TestSeedance25ClientSanitizesAllProviderControlledErrors(t *testing.T) {
	const secret = "sentinel-provider-key"
	const prompt = "sentinel-user-prompt alice@example.test"
	tests := []struct {
		name string
		body string
	}{
		{name: "failed data error", body: `{"code":0,"message":"","data":{"task_id":"provider-task","status":"failed","error":"rejected sentinel-provider-key sentinel-user-prompt alice@example.test"},"trace_id":"trace-safe"}`},
		{name: "unknown status", body: `{"code":0,"message":"","data":{"task_id":"provider-task","status":"mystery-sentinel-provider-key-sentinel-user-prompt alice@example.test"},"trace_id":"trace-safe"}`},
		{name: "envelope message and trace", body: `{"code":40001,"message":"sentinel-provider-key sentinel-user-prompt alice@example.test","data":null,"trace_id":"trace-sentinel-provider-key-sentinel-user-prompt"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(writer, test.body)
			}))
			defer server.Close()
			client, runtime := seedance25TestClient(t, server)
			ciphertext, err := client.cipher.Encrypt(runtime.Account.ID, runtime.Credential.ID, runtime.CredentialVersion.ID, secret)
			if err != nil {
				t.Fatal(err)
			}
			runtime.CredentialVersion.KeyCipher = ciphertext
			runtime.Task.Prompt = prompt
			polled, err := client.Status(context.Background(), runtime, "provider-task")
			observed := ""
			if err != nil {
				observed = err.Error()
			} else if polled.State.Terminal && !polled.State.Succeeded {
				observed = polled.State.FailureReason
			} else {
				t.Fatalf("Status() = %#v, %v; want controlled failure", polled, err)
			}
			for _, sentinel := range []string{secret, "sentinel-user-prompt", "alice@example.test"} {
				if strings.Contains(observed, sentinel) {
					t.Fatalf("Status() leaked %q: %s", sentinel, observed)
				}
			}
			var providerErr *KuaiziSeedance25Error
			if errors.As(err, &providerErr) && len([]rune(providerErr.Message)) > 240 {
				t.Fatalf("sanitized provider message length = %d", len([]rune(providerErr.Message)))
			}
		})
	}
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

func TestSeedance25CreateHTTPFailureUsesExplicitSideEffectClassification(t *testing.T) {
	for _, statusCode := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusGatewayTimeout} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(statusCode)
				_, _ = io.WriteString(writer, `{"code":50000,"message":"gateway failed","data":null,"trace_id":"trace-http"}`)
			}))
			defer server.Close()
			client, runtime := seedance25TestClient(t, server)
			_, err := client.Create(context.Background(), runtime, seedance25TestInput())
			var uncertain *KuaiziSeedance25CreateUncertainError
			if !errors.As(err, &uncertain) {
				t.Fatalf("Create() error = %T %v, want typed create uncertainty", err, err)
			}
		})
	}
	for _, statusCode := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(statusCode)
				body := `<html>request rejected</html>`
				if statusCode == http.StatusUnprocessableEntity {
					body = strings.Repeat("x", 4<<20+1)
				}
				_, _ = io.WriteString(writer, body)
			}))
			defer server.Close()
			client, runtime := seedance25TestClient(t, server)
			_, err := client.Create(context.Background(), runtime, seedance25TestInput())
			var uncertain *KuaiziSeedance25CreateUncertainError
			if err == nil || errors.As(err, &uncertain) {
				t.Fatalf("Create() error = %T %v, want definitive rejection", err, err)
			}
		})
	}
}

func TestSeedance25CreateHTTP200RequiresCompleteSuccessToAvoidUncertainty(t *testing.T) {
	for _, body := range []string{
		`{"code":40001,"message":"rejected","data":null,"trace_id":"trace-business"}`,
		`{"code":0,"message":"","data":null,"trace_id":"trace-missing-data"}`,
	} {
		server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, body)
		}))
		client, runtime := seedance25TestClient(t, server)
		_, err := client.Create(context.Background(), runtime, seedance25TestInput())
		server.Close()
		var uncertain *KuaiziSeedance25CreateUncertainError
		if !errors.As(err, &uncertain) {
			t.Fatalf("Create() body=%s error = %T %v, want uncertainty", body, err, err)
		}
	}
}

func TestSeedance25CreateHTTP5xxIsUncertainBeforeEnvelopeParsing(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{name: "500 html", statusCode: http.StatusInternalServerError, body: `<html>upstream failed</html>`},
		{name: "502 empty", statusCode: http.StatusBadGateway},
		{name: "524 oversized", statusCode: 524, body: strings.Repeat("x", 4<<20+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.statusCode)
				_, _ = io.WriteString(writer, test.body)
			}))
			defer server.Close()
			client, runtime := seedance25TestClient(t, server)
			_, err := client.Create(context.Background(), runtime, seedance25TestInput())
			var uncertain *KuaiziSeedance25CreateUncertainError
			if !errors.As(err, &uncertain) {
				t.Fatalf("Create() error = %T %v, want HTTP-first uncertainty", err, err)
			}
		})
	}
}

func TestSeedance25ClientRejectsSensitiveOrMalformedSuccessfulFields(t *testing.T) {
	const secret = "sentinel-success-key"
	const prompt = "sentinel success prompt alice@example.test"
	tests := []struct {
		name  string
		stage string
		data  string
	}{
		{name: "create id reflects key", stage: "create", data: `{"task_id":"sentinel-success-key"}`},
		{name: "create id reflects prompt", stage: "create", data: `{"task_id":"sentinel success prompt alice@example.test"}`},
		{name: "create id invalid characters", stage: "create", data: `{"task_id":"provider/task?id=unsafe"}`},
		{name: "video url reflects key", stage: "status", data: `{"task_id":"provider-task","status":"succeeded","video_url":"https://cdn.example/result.mp4?token=sentinel-success-key","last_frame_url":"https://cdn.example/last.png","duration":8,"total_tokens":"42"}`},
		{name: "last frame url reflects encoded prompt", stage: "status", data: `{"task_id":"provider-task","status":"succeeded","video_url":"https://cdn.example/result.mp4","last_frame_url":"https://cdn.example/last.png?note=sentinel%20success%20prompt%20alice%40example.test","duration":8,"total_tokens":"42"}`},
		{name: "last frame url reflects double encoded prompt", stage: "status", data: `{"task_id":"provider-task","status":"succeeded","video_url":"https://cdn.example/result.mp4","last_frame_url":"https://cdn.example/last.png?note=sentinel%2520success%2520prompt%2520alice%2540example.test","duration":8,"total_tokens":"42"}`},
		{name: "last frame url reflects four-times encoded prompt", stage: "status", data: `{"task_id":"provider-task","status":"succeeded","video_url":"https://cdn.example/result.mp4","last_frame_url":"https://cdn.example/last.png?note=sentinel%25252520success%25252520prompt%25252520alice%25252540example.test","duration":8,"total_tokens":"42"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(writer, `{"code":0,"message":"","data":`+test.data+`,"trace_id":"trace-safe"}`)
			}))
			defer server.Close()
			client, runtime := seedance25TestClient(t, server)
			ciphertext, err := client.cipher.Encrypt(runtime.Account.ID, runtime.Credential.ID, runtime.CredentialVersion.ID, secret)
			if err != nil {
				t.Fatal(err)
			}
			runtime.CredentialVersion.KeyCipher = ciphertext
			runtime.Task.Prompt = prompt
			if test.stage == "create" {
				_, err = client.Create(context.Background(), runtime, seedance25TestInput())
				var uncertain *KuaiziSeedance25CreateUncertainError
				if !errors.As(err, &uncertain) {
					t.Fatalf("Create() error = %T %v, want uncertainty", err, err)
				}
			} else {
				_, err = client.Status(context.Background(), runtime, "provider-task")
				if err == nil {
					t.Fatal("Status() accepted sensitive successful fields")
				}
			}
			if err != nil && (strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "alice@example.test")) {
				t.Fatalf("error leaked successful field sentinel: %v", err)
			}
		})
	}
}

func TestSeedance25ResponseRequiresPresentCodeAndCompleteSuccessUsage(t *testing.T) {
	tests := []struct {
		name  string
		stage string
		body  string
	}{
		{name: "create missing code", stage: "create", body: `{"data":{"task_id":"provider-task"},"trace_id":"trace"}`},
		{name: "create wrong code type", stage: "create", body: `{"code":"0","data":{"task_id":"provider-task"},"trace_id":"trace"}`},
		{name: "status missing code", stage: "status", body: `{"data":{"task_id":"provider-task","status":"pending"},"trace_id":"trace"}`},
		{name: "success missing tokens", stage: "status", body: `{"code":0,"data":{"task_id":"provider-task","status":"succeeded","video_url":"https://cdn.example.com/result.mp4","last_frame_url":"https://cdn.example.com/last.png","duration":8},"trace_id":"trace"}`},
		{name: "success null tokens", stage: "status", body: `{"code":0,"data":{"task_id":"provider-task","status":"succeeded","video_url":"https://cdn.example.com/result.mp4","last_frame_url":"https://cdn.example.com/last.png","duration":8,"total_tokens":null},"trace_id":"trace"}`},
		{name: "success negative tokens", stage: "status", body: `{"code":0,"data":{"task_id":"provider-task","status":"succeeded","video_url":"https://cdn.example.com/result.mp4","last_frame_url":"https://cdn.example.com/last.png","duration":8,"total_tokens":-1},"trace_id":"trace"}`},
		{name: "success oversized tokens", stage: "status", body: `{"code":0,"data":{"task_id":"provider-task","status":"succeeded","video_url":"https://cdn.example.com/result.mp4","last_frame_url":"https://cdn.example.com/last.png","duration":8,"total_tokens":"` + strings.Repeat("9", 81) + `"},"trace_id":"trace"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(writer, test.body)
			}))
			defer server.Close()
			client, runtime := seedance25TestClient(t, server)
			var err error
			if test.stage == "create" {
				_, err = client.Create(context.Background(), runtime, seedance25TestInput())
				var uncertain *KuaiziSeedance25CreateUncertainError
				if !errors.As(err, &uncertain) {
					t.Fatalf("Create() error = %T %v, want uncertainty", err, err)
				}
			} else {
				_, err = client.Status(context.Background(), runtime, "provider-task")
				if err == nil {
					t.Fatal("Status() error = nil")
				}
			}
		})
	}
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
		items[index] = providerMedia{URL: "https://cdn.example.com/reference-" + kind + ".bin", StorageKey: "resource:" + kind + "-fixture"}
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
	ciphertext, err := cipher.encrypt("account", "credential", "credential-v1", "frozen-key-v1", true, ErrProviderSecretKeyMissing)
	if err != nil {
		t.Fatal(err)
	}
	return NewKuaiziSeedance25Client(server.Client(), cipher), seedance25TestRuntime(server.URL, ciphertext)
}
