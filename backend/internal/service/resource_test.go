package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSignedOSSObjectURLUsesExpiringQuerySignature(t *testing.T) {
	expiresAt := time.Unix(1800000000, 0)
	value, err := signedOSSObjectURL(ossSettingValue{
		Endpoint: "https://oss-cn-test.aliyuncs.com", Bucket: "private-bucket",
		AccessKeyID: "access-id", AccessKeySecret: "secret-value",
	}, "users/u-1/image/test image.png", expiresAt)
	if err != nil {
		t.Fatalf("signedOSSObjectURL() error = %v", err)
	}
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if parsed.Host != "private-bucket.oss-cn-test.aliyuncs.com" || query.Get("OSSAccessKeyId") != "access-id" || query.Get("Expires") != "1800000000" || query.Get("Signature") == "" {
		t.Fatalf("signed URL = %q", value)
	}
	if strings.Contains(value, "secret-value") {
		t.Fatalf("signed URL leaked access key secret: %q", value)
	}
}

func TestDirectResourceURLChecksOwnershipAndSignsOSSResource(t *testing.T) {
	svc := newResourceTestService(t)
	settingJSON, _ := json.Marshal(ossSettingValue{
		Enabled: true, Provider: "aliyun", Endpoint: "https://oss-cn-test.aliyuncs.com", Bucket: "private-bucket",
		AccessKeyID: "access-id", AccessKeySecret: "secret-value",
	})
	if err := svc.repo.SaveSystemSetting(&model.SystemSetting{Key: ossSettingKey, ValueJSON: string(settingJSON)}); err != nil {
		t.Fatal(err)
	}
	resource := model.Resource{
		ID: "resource-direct", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady,
		Provider: "aliyun", Endpoint: "https://oss-cn-test.aliyuncs.com", Bucket: "private-bucket",
		ObjectKey: "users/user-1/image/direct.png", MimeType: "image/png",
	}
	if err := svc.repo.CreateResource(&resource); err != nil {
		t.Fatal(err)
	}
	value, err := svc.DirectResourceURL("user-1", resource.ID)
	if err != nil || !strings.Contains(value, "Signature=") {
		t.Fatalf("DirectResourceURL() = %q, %v", value, err)
	}
	if _, err := svc.DirectResourceURL("other-user", resource.ID); err == nil {
		t.Fatal("DirectResourceURL() allowed another user's resource")
	}
}

func TestNormalizeSingleByteRange(t *testing.T) {
	tests := map[string]string{
		"bytes=0-1023":       "bytes=0-1023",
		"bytes=1024-":        "bytes=1024-",
		"bytes=-2048":        "bytes=-2048",
		"bytes=0-1,10-20":    "",
		"items=0-10":         "",
		"bytes=invalid-1024": "",
	}
	for input, expected := range tests {
		if actual := normalizeSingleByteRange(input); actual != expected {
			t.Fatalf("normalizeSingleByteRange(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestHydratePublicUpstreamResourceUsesSignedOSSURL(t *testing.T) {
	svc := newResourceTestService(t)
	settingJSON, _ := json.Marshal(ossSettingValue{
		Enabled: true, Provider: "aliyun", Endpoint: "https://oss-cn-test.aliyuncs.com", Bucket: "private-bucket",
		AccessKeyID: "access-id", AccessKeySecret: "secret-value",
	})
	if err := svc.repo.SaveSystemSetting(&model.SystemSetting{Key: ossSettingKey, ValueJSON: string(settingJSON)}); err != nil {
		t.Fatal(err)
	}
	resource := model.Resource{
		ID: "resource-1", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady,
		Provider: "aliyun", Endpoint: "https://oss-cn-test.aliyuncs.com", Bucket: "private-bucket",
		ObjectKey: "users/user-1/image/reference.png", MimeType: "image/png", DurationMs: 12_345,
	}
	if err := svc.repo.CreateResource(&resource); err != nil {
		t.Fatal(err)
	}
	media := providerMedia{StorageKey: "resource:resource-1", DataURL: "data:image/png;base64,old"}
	if err := svc.hydrateProviderMedia("user-1", &media, true); err != nil {
		t.Fatalf("hydrateProviderMedia() error = %v", err)
	}
	if !strings.HasPrefix(media.URL, "https://private-bucket.oss-cn-test.aliyuncs.com/") || media.DataURL != "" || !strings.Contains(media.URL, "Signature=") {
		t.Fatalf("media = %#v", media)
	}
	if media.DurationMs != resource.DurationMs {
		t.Fatalf("media duration = %d, want authoritative %d", media.DurationMs, resource.DurationMs)
	}
	if err := svc.hydrateProviderMedia("other-user", &providerMedia{StorageKey: "resource:resource-1"}, true); err == nil {
		t.Fatal("hydrateProviderMedia() allowed another user's resource")
	}
}

func TestHydratePublicUpstreamResourceRejectsUnownedDirectURL(t *testing.T) {
	svc := newResourceTestService(t)
	err := svc.hydrateProviderMedia("user-1", &providerMedia{URL: "https://example.com/unowned.mp4", DurationMs: 5_000}, true)
	if err == nil || !strings.Contains(err.Error(), "本人已上传") {
		t.Fatalf("hydrateProviderMedia() error = %v", err)
	}
}

func TestHydratePublicUpstreamResourceRejectsLocalStorage(t *testing.T) {
	svc := newResourceTestService(t)
	resource := model.Resource{ID: "resource-local", UserID: "user-1", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "local.png"}
	if err := svc.repo.CreateResource(&resource); err != nil {
		t.Fatal(err)
	}
	err := svc.hydrateProviderMedia("user-1", &providerMedia{StorageKey: "resource:resource-local"}, true)
	if err == nil || !strings.Contains(err.Error(), "启用 OSS") {
		t.Fatalf("hydrateProviderMedia() error = %v", err)
	}
}

func TestActiveResourceOSSSettingIgnoresLegacyUserVersion(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	svc, db := newResourceTestServiceWithDB(t)
	systemJSON, _ := json.Marshal(ossSettingValue{Enabled: true, Provider: "aliyun", Endpoint: server.URL, Bucket: "system", AccessKeyID: "system-id", AccessKeySecret: "system-secret"})
	if err := svc.repo.SaveSystemSetting(&model.SystemSetting{Key: ossSettingKey, ValueJSON: string(systemJSON)}); err != nil {
		t.Fatal(err)
	}
	legacyJSON, _ := json.Marshal(ossSettingValue{Enabled: true, Provider: "aliyun", Endpoint: server.URL, Bucket: "user", AccessKeyID: "user-id", AccessKeySecret: "user-secret"})
	if err := db.Create(&model.UserOSSSetting{ID: "legacy-setting", UserID: "user-1", Enabled: true, ValueJSON: string(legacyJSON)}).Error; err != nil {
		t.Fatal(err)
	}
	setting, settingID, useOSS, err := svc.activeResourceOSSSetting()
	if err != nil {
		t.Fatal(err)
	}
	if !useOSS || settingID != "" || setting.Bucket != "system" {
		t.Fatalf("activeResourceOSSSetting() = %#v, %q, %v", setting, settingID, useOSS)
	}
}

func TestHistoricalUserOSSSettingRemainsReadableByResourceVersion(t *testing.T) {
	svc, db := newResourceTestServiceWithDB(t)
	storedSecret, err := svc.encryptSettingSecret("old-secret")
	if err != nil {
		t.Fatal(err)
	}
	valueJSON, _ := json.Marshal(ossSettingValue{Enabled: true, Provider: "aliyun", Endpoint: "https://oss-cn-test.aliyuncs.com", Bucket: "old", AccessKeyID: "old-id", AccessKeySecret: storedSecret})
	if err := db.Create(&model.UserOSSSetting{ID: "legacy-setting", UserID: "user-1", Enabled: true, ValueJSON: string(valueJSON)}).Error; err != nil {
		t.Fatal(err)
	}
	_, oldValue, err := svc.readUserOSSSettingByID("user-1", "legacy-setting")
	if err != nil {
		t.Fatal(err)
	}
	if oldValue.Bucket != "old" || oldValue.AccessKeySecret != "old-secret" {
		t.Fatalf("historical setting = %#v", oldValue)
	}
}

func newResourceTestService(t *testing.T) *Service {
	t.Helper()
	service, _ := newResourceTestServiceWithDB(t)
	return service
}

func newResourceTestServiceWithDB(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.SystemSetting{}, &model.UserOSSSetting{}, &model.UserDailyUploadUsage{}, &model.Resource{}, &model.SessionFile{}); err != nil {
		t.Fatal(err)
	}
	return &Service{repo: repository.New(db), dataDir: t.TempDir()}, db
}

func TestLegacyMediaMigrationSkipsInvalidDataURL(t *testing.T) {
	svc := &Service{}
	input := map[string]interface{}{
		"history": []interface{}{
			map[string]interface{}{"content": "data:video/mp4;base64,broken"},
		},
	}

	result, err := svc.persistLegacyGeneratedMediaResult("user-1", input)
	if err != nil {
		t.Fatalf("persistLegacyGeneratedMediaResult() error = %v", err)
	}
	history := result["history"].([]interface{})
	content := history[0].(map[string]interface{})["content"]
	if content != "data:video/mp4;base64,broken" {
		t.Fatalf("invalid legacy content changed to %v", content)
	}
}

func TestGeneratedMediaRejectsInvalidDataURL(t *testing.T) {
	svc := &Service{}
	_, err := svc.persistGeneratedMediaResult("user-1", map[string]interface{}{
		"content": "data:video/mp4;base64,broken",
	})
	if err == nil {
		t.Fatal("persistGeneratedMediaResult() error = nil, want invalid data URL error")
	}
}

func TestPersistGeneratedMediaPreservesProviderSuccessBeyondStoredFileQuota(t *testing.T) {
	svc := newResourceTestService(t)
	if err := svc.repo.Create(&model.Resource{
		ID:     "existing",
		UserID: "user-1",
		Status: model.ResourceStatusReady,
		Size:   gigabytes(defaultRuntimePolicy().Resource.StoredFileGB) - 1,
	}); err != nil {
		t.Fatal(err)
	}

	result, err := svc.persistGeneratedMediaResult("user-1", map[string]interface{}{
		"image": map[string]interface{}{"dataUrl": "data:image/png;base64,YQ=="},
	})
	if err != nil {
		t.Fatalf("persistGeneratedMediaResult() error = %v", err)
	}
	image, _ := result["image"].(map[string]interface{})
	if !strings.HasPrefix(fmt.Sprint(image["storageKey"]), "resource:") {
		t.Fatalf("generated image was not persisted: %#v", image)
	}
}

func TestAuthoritativeMediaDurationUsesServerProbe(t *testing.T) {
	svc := &Service{mediaDurationProbe: func(_ context.Context, body io.Reader) (int64, error) {
		data, err := io.ReadAll(body)
		if err != nil {
			return 0, err
		}
		if string(data) != "server-owned-media" {
			t.Fatalf("probe body = %q", data)
		}
		return 61_234, nil
	}}
	body := bytes.NewReader([]byte("server-owned-media"))
	durationMs, err := svc.authoritativeMediaDuration("video", body)
	if err != nil {
		t.Fatal(err)
	}
	if durationMs != 61_234 {
		t.Fatalf("duration = %d", durationMs)
	}
	remaining, _ := io.ReadAll(body)
	if string(remaining) != "server-owned-media" {
		t.Fatalf("body was not rewound: %q", remaining)
	}
}

func TestParseMediaDurationMilliseconds(t *testing.T) {
	durationMs, err := parseMediaDurationMilliseconds("15.001\n")
	if err != nil || durationMs != 15_001 {
		t.Fatalf("parseMediaDurationMilliseconds() = %d, %v", durationMs, err)
	}
	if _, err := parseMediaDurationMilliseconds("unknown"); err == nil {
		t.Fatal("invalid ffprobe duration was accepted")
	}
}
