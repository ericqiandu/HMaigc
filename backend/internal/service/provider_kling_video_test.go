package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestParseKlingCredentialsRequiresStrictJSON(t *testing.T) {
	credentials, err := parseKlingCredentials(`{"accessKey":"ak-test","secretKey":"sk-test"}`)
	if err != nil {
		t.Fatalf("parse credentials: %v", err)
	}
	if credentials.AccessKey != "ak-test" || credentials.SecretKey != "sk-test" {
		t.Fatalf("unexpected credentials: %#v", credentials)
	}
	for _, raw := range []string{
		`ak-test:sk-test`,
		`{"accessKey":"ak-test"}`,
		`{"accessKey":"ak-test","secretKey":"sk-test","unknown":true}`,
		`{"accessKey":"ak-test","secretKey":"sk-test"} {}`,
	} {
		if _, err := parseKlingCredentials(raw); err == nil {
			t.Fatalf("expected credentials %q to fail", raw)
		}
	}
}

func TestKlingJWTUsesHS256AndExpectedClaims(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	token, err := klingJWT(klingCredentials{AccessKey: "ak-test", SecretKey: "sk-test"}, now)
	if err != nil {
		t.Fatalf("create jwt: %v", err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt parts = %d", len(parts))
	}
	mac := hmac.New(sha256.New, []byte("sk-test"))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if parts[2] != base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) {
		t.Fatal("jwt signature mismatch")
	}
	claimsData, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(claimsData, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	if claims["iss"] != "ak-test" || int64(claims["exp"].(float64)) != now.Add(30*time.Minute).Unix() || int64(claims["nbf"].(float64)) != now.Add(-5*time.Second).Unix() {
		t.Fatalf("unexpected claims: %#v", claims)
	}
}

func TestKlingTextVideoRequest(t *testing.T) {
	path, body, err := klingVideoRequest(klingTestInput("text"))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if path != "/v1/videos/text2video" || body["model_name"] != "kling-v2-6" || body["mode"] != "pro" || body["duration"] != "10" || body["aspect_ratio"] != "16:9" {
		t.Fatalf("unexpected request: path=%s body=%#v", path, body)
	}
}

func TestKlingFirstLastFrameRequest(t *testing.T) {
	input := klingTestInput("first_last_frame")
	input.ReferenceImages = []providerMedia{
		{ID: "first", URL: "https://assets.example.com/first.png"},
		{ID: "last", URL: "https://assets.example.com/last.png"},
	}
	path, body, err := klingVideoRequest(input)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if path != "/v1/videos/image2video" || body["image"] != input.ReferenceImages[0].URL || body["image_tail"] != input.ReferenceImages[1].URL {
		t.Fatalf("unexpected request: path=%s body=%#v", path, body)
	}
}

func TestKlingRejectsUnsupportedParametersAndModes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*canvasGenerationInput)
	}{
		{name: "duration", mutate: func(input *canvasGenerationInput) { input.Config.VideoSeconds = "6" }},
		{name: "resolution", mutate: func(input *canvasGenerationInput) { input.Config.VQuality = "2k" }},
		{name: "ratio", mutate: func(input *canvasGenerationInput) { input.Config.Size = "4:3" }},
		{name: "audio", mutate: func(input *canvasGenerationInput) { input.Config.VideoGenerateAudio = "true" }},
		{name: "watermark", mutate: func(input *canvasGenerationInput) { input.Config.VideoWatermark = "true" }},
		{name: "omni reference", mutate: func(input *canvasGenerationInput) { input.Metadata["videoGenerationMode"] = "omni_reference" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := klingTestInput("text")
			test.mutate(&input)
			if _, _, err := klingVideoRequest(input); err == nil {
				t.Fatal("expected explicit validation error")
			}
		})
	}
}

func TestKlingVideoResultURL(t *testing.T) {
	state := map[string]interface{}{"task_result": map[string]interface{}{"videos": []interface{}{
		map[string]interface{}{"id": "video-1", "url": "https://assets.example.com/result.mp4"},
	}}}
	if got := klingVideoResultURL(state); got != "https://assets.example.com/result.mp4" {
		t.Fatalf("result URL = %q", got)
	}
}

func klingTestInput(mode string) canvasGenerationInput {
	return canvasGenerationInput{
		Prompt: "一位演员走进晨雾中的城市",
		Config: providerConfig{
			Model: "kling-v2-6", VideoSeconds: "10", VQuality: "1080p", Size: "16:9", VideoGenerateAudio: "false", VideoWatermark: "false",
		},
		Metadata: map[string]interface{}{"videoGenerationMode": mode},
	}
}
