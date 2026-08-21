package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestKuaiziSeedreamCreatePayloadDistinguishesLiteAndPro(t *testing.T) {
	watermark := false
	tests := []struct {
		name       string
		model      string
		maxImages  int
		sequential bool
	}{
		{name: "lite", model: "seedream5.0lite", maxImages: 14, sequential: true},
		{name: "pro", model: "seedream5.0pro", maxImages: 10, sequential: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			references := make([]providerMedia, test.maxImages)
			for index := range references {
				references[index] = providerMedia{URL: "https://assets.example.com/reference-" + string(rune('a'+index)) + ".png"}
			}
			payload, err := kuaiziSeedreamCreatePayload(canvasGenerationInput{
				Prompt:          "生成电影感橙子广告",
				Config:          providerConfig{Model: test.model, Size: "2048x2048", Count: "1"},
				ReferenceImages: references,
				Watermark:       taskWatermarkRuntime{Parameter: &watermark},
			})
			if err != nil {
				t.Fatal(err)
			}
			if payload["model"] != test.model || payload["prompt"] != "生成电影感橙子广告" || payload["size"] != "2048x2048" || payload["output_format"] != "jpeg" || payload["watermark"] != false {
				t.Fatalf("payload = %#v", payload)
			}
			images, ok := payload["image"].([]string)
			if !ok || len(images) != test.maxImages || images[0] != "https://assets.example.com/reference-a.png" {
				t.Fatalf("image = %#v", payload["image"])
			}
			sequential, exists := payload["sequential_image_generation"]
			if exists != test.sequential || (exists && sequential != "disabled") {
				t.Fatalf("sequential = %#v, exists=%v", sequential, exists)
			}
		})
	}
}

func TestKuaiziSeedreamCreatePayloadRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name  string
		input canvasGenerationInput
		want  string
	}{
		{name: "unknown model", input: canvasGenerationInput{Prompt: "x", Config: providerConfig{Model: "seedream-unknown", Size: "2048x2048"}}, want: "未登记"},
		{name: "blank prompt", input: canvasGenerationInput{Config: providerConfig{Model: "seedream5.0lite", Size: "2048x2048"}}, want: "提示词不能为空"},
		{name: "batch", input: canvasGenerationInput{Prompt: "x", Config: providerConfig{Model: "seedream5.0lite", Size: "2048x2048", Count: "2"}}, want: "每个任务只支持生成 1 张"},
		{name: "mask", input: canvasGenerationInput{Prompt: "x", Config: providerConfig{Model: "seedream5.0lite", Size: "2048x2048"}, Mask: &providerMedia{DataURL: "data:image/png;base64,YQ=="}}, want: "蒙版"},
		{name: "video reference", input: canvasGenerationInput{Prompt: "x", Config: providerConfig{Model: "seedream5.0lite", Size: "2048x2048"}, ReferenceVideos: []providerMedia{{URL: "https://assets.example.com/a.mp4"}}}, want: "只支持图片参考素材"},
		{name: "private reference", input: canvasGenerationInput{Prompt: "x", Config: providerConfig{Model: "seedream5.0lite", Size: "2048x2048"}, ReferenceImages: []providerMedia{{DataURL: "data:image/png;base64,YQ=="}}}, want: "公网 URL"},
		{name: "lite reference overflow", input: seedreamInputWithReferenceCount("seedream5.0lite", 15), want: "最多支持 14 张参考图"},
		{name: "pro reference overflow", input: seedreamInputWithReferenceCount("seedream5.0pro", 11), want: "最多支持 10 张参考图"},
		{name: "pixel minimum", input: canvasGenerationInput{Prompt: "x", Config: providerConfig{Model: "seedream5.0lite", Size: "1024x1024"}}, want: "3686400–10404496"},
		{name: "pixel maximum", input: canvasGenerationInput{Prompt: "x", Config: providerConfig{Model: "seedream5.0lite", Size: "4096x4096"}}, want: "3686400–10404496"},
		{name: "ratio maximum", input: canvasGenerationInput{Prompt: "x", Config: providerConfig{Model: "seedream5.0lite", Size: "8208x512"}}, want: "宽高比"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := kuaiziSeedreamCreatePayload(test.input); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestKuaiziSeedreamTaskSubmitsDocumentedPayloadAndDownloadsResult(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	var createBody map[string]any
	var statusBody map[string]any
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/ai-open-platform-api/") && request.Header.Get("ApiKey") != "seedream-key" {
			t.Errorf("ApiKey = %q", request.Header.Get("ApiKey"))
		}
		switch request.URL.Path {
		case kuaiziSeedreamCreatePath:
			if err := json.NewDecoder(request.Body).Decode(&createBody); err != nil {
				t.Fatal(err)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"code":0,"message":"","data":{"task_id":"kz-cgt-seedream-1"},"trace_id":"trace-create-1"}`))
		case kuaiziSeedreamStatusPath:
			if err := json.NewDecoder(request.Body).Decode(&statusBody); err != nil {
				t.Fatal(err)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"code":0,"message":"","data":{"task_id":"kz-cgt-seedream-1","status":"succeeded","image_urls":["` + server.URL + `/result.jpg"]},"trace_id":"trace-poll-1"}`))
		case "/result.jpg":
			writer.Header().Set("Content-Type", "image/jpeg")
			_, _ = writer.Write([]byte("jpeg-bytes"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	result, err := runKuaiziSeedreamTaskWithPollInterval(context.Background(), canvasGenerationInput{
		Prompt: "生成电影海报",
		Config: providerConfig{BaseURL: server.URL, APIKey: "seedream-key", Model: "seedream5.0lite", Size: "2048x2048", Count: "1"},
	}, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if createBody["model"] != "seedream5.0lite" || createBody["sequential_image_generation"] != "disabled" {
		t.Fatalf("create body = %#v", createBody)
	}
	if statusBody["task_id"] != "kz-cgt-seedream-1" {
		t.Fatalf("status body = %#v", statusBody)
	}
	images, ok := result["images"].([]map[string]string)
	if !ok || len(images) != 1 || images[0]["taskId"] != "kz-cgt-seedream-1" || !strings.HasPrefix(images[0]["dataUrl"], "data:image/jpeg;base64,") {
		t.Fatalf("result = %#v", result)
	}
}

func TestKuaiziSeedreamTaskPreservesFailedTaskEvidence(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == kuaiziSeedreamCreatePath {
			_, _ = writer.Write([]byte(`{"code":0,"message":"","data":{"task_id":"kz-cgt-seedream-failed"},"trace_id":"trace-create"}`))
			return
		}
		_, _ = writer.Write([]byte(`{"code":0,"message":"","data":{"task_id":"kz-cgt-seedream-failed","status":"failed","error":"Text Risk Not Pass"},"trace_id":"trace-failed-42"}`))
	}))
	defer server.Close()

	_, err := runKuaiziSeedreamTaskWithPollInterval(context.Background(), canvasGenerationInput{
		Prompt: "x", Config: providerConfig{BaseURL: server.URL, APIKey: "key", Model: "seedream5.0pro", Size: "2048x2048"},
	}, time.Millisecond)
	for _, evidence := range []string{"kz-cgt-seedream-failed", "Text Risk Not Pass", "trace-failed-42"} {
		if err == nil || !strings.Contains(err.Error(), evidence) {
			t.Fatalf("error = %v, want evidence %q", err, evidence)
		}
	}
}

func TestKuaiziSeedreamTaskRejectsBusinessFailureAndMultipleOutputs(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	t.Run("business failure carries trace", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"code":400,"message":"invalid size","data":{},"trace_id":"trace-business-7"}`))
		}))
		defer server.Close()
		_, err := runKuaiziSeedreamTask(context.Background(), canvasGenerationInput{Prompt: "x", Config: providerConfig{BaseURL: server.URL, APIKey: "key", Model: "seedream5.0lite", Size: "2048x2048"}})
		var uncertain *KuaiziCompatibleCreateError
		if errors.As(err, &uncertain) || err == nil || !strings.Contains(err.Error(), "invalid size") || !strings.Contains(err.Error(), "trace-business-7") {
			t.Fatalf("error = %T %v", err, err)
		}
	})

	t.Run("http failure carries gateway evidence", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"code":400,"message":"invalid request","data":{},"trace_id":"trace-http-8"}`))
		}))
		defer server.Close()
		_, err := runKuaiziSeedreamTask(context.Background(), canvasGenerationInput{Prompt: "x", Config: providerConfig{BaseURL: server.URL, APIKey: "key", Model: "seedream5.0lite", Size: "2048x2048"}})
		var uncertain *KuaiziCompatibleCreateError
		if errors.As(err, &uncertain) || err == nil || !strings.Contains(err.Error(), "invalid request") || !strings.Contains(err.Error(), "trace-http-8") {
			t.Fatalf("error = %T %v", err, err)
		}
	})

	t.Run("multiple outputs violate one task contract", func(t *testing.T) {
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			if request.URL.Path == kuaiziSeedreamCreatePath {
				_, _ = writer.Write([]byte(`{"code":0,"message":"","data":{"task_id":"kz-cgt-seedream-many"},"trace_id":"trace-create"}`))
				return
			}
			_, _ = writer.Write([]byte(`{"code":0,"message":"","data":{"task_id":"kz-cgt-seedream-many","status":"succeeded","image_urls":["` + server.URL + `/a.jpg","` + server.URL + `/b.jpg"]},"trace_id":"trace-many"}`))
		}))
		defer server.Close()
		_, err := runKuaiziSeedreamTaskWithPollInterval(context.Background(), canvasGenerationInput{Prompt: "x", Config: providerConfig{BaseURL: server.URL, APIKey: "key", Model: "seedream5.0lite", Size: "2048x2048"}}, time.Millisecond)
		if err == nil || !strings.Contains(err.Error(), "必须返回且只返回 1 张图片") || !strings.Contains(err.Error(), "trace-many") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestKuaiziSeedreamTaskRejectsInvalidTerminalEvidence(t *testing.T) {
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	tests := []struct {
		name   string
		status func(string) string
		want   []string
	}{
		{
			name: "mismatched task id",
			status: func(string) string {
				return `{"code":0,"message":"","data":{"task_id":"other-task","status":"succeeded","image_urls":["https://assets.example.com/result.jpg"]},"trace_id":"trace-mismatch"}`
			},
			want: []string{"不匹配的任务 ID", "trace-mismatch"},
		},
		{
			name: "invalid result url",
			status: func(string) string {
				return `{"code":0,"message":"","data":{"task_id":"kz-cgt-seedream-terminal","status":"succeeded","image_urls":["ftp://assets.example.com/result.jpg"]},"trace_id":"trace-url"}`
			},
			want: []string{"图片地址必须使用 HTTPS", "trace-url"},
		},
		{
			name: "credential in result url",
			status: func(string) string {
				return `{"code":0,"message":"","data":{"task_id":"kz-cgt-seedream-terminal","status":"succeeded","image_urls":["https://assets.example.com/result.jpg?token=key"]},"trace_id":"trace-secret"}`
			},
			want: []string{"图片地址包含供应商凭据", "trace-secret"},
		},
		{
			name: "non image result",
			status: func(serverURL string) string {
				return `{"code":0,"message":"","data":{"task_id":"kz-cgt-seedream-terminal","status":"succeeded","image_urls":["` + serverURL + `/result.txt"]},"trace_id":"trace-mime"}`
			},
			want: []string{"返回的内容不是图片", "trace-mime"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case kuaiziSeedreamCreatePath:
					writer.Header().Set("Content-Type", "application/json")
					_, _ = writer.Write([]byte(`{"code":0,"message":"","data":{"task_id":"kz-cgt-seedream-terminal"},"trace_id":"trace-create"}`))
				case kuaiziSeedreamStatusPath:
					writer.Header().Set("Content-Type", "application/json")
					_, _ = writer.Write([]byte(test.status(server.URL)))
				case "/result.txt":
					writer.Header().Set("Content-Type", "text/plain")
					_, _ = writer.Write([]byte("not-an-image"))
				default:
					http.NotFound(writer, request)
				}
			}))
			defer server.Close()

			_, err := runKuaiziSeedreamTaskWithPollInterval(context.Background(), canvasGenerationInput{
				Prompt: "x", Config: providerConfig{BaseURL: server.URL, APIKey: "key", Model: "seedream5.0lite", Size: "2048x2048"},
			}, time.Millisecond)
			for _, evidence := range test.want {
				if err == nil || !strings.Contains(err.Error(), evidence) {
					t.Fatalf("error = %v, want evidence %q", err, evidence)
				}
			}
		})
	}
}

func seedreamInputWithReferenceCount(modelKey string, count int) canvasGenerationInput {
	references := make([]providerMedia, count)
	for index := range references {
		references[index] = providerMedia{URL: "https://assets.example.com/reference-" + string(rune('a'+index)) + ".png"}
	}
	return canvasGenerationInput{Prompt: "x", Config: providerConfig{Model: modelKey, Size: "2048x2048"}, ReferenceImages: references}
}
