package service

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestValidateSyncedPayloadAllowsDataURLMentionInErrorMessage(t *testing.T) {
	raw, err := json.Marshal(map[string]interface{}{
		"nodes": []interface{}{
			map[string]interface{}{
				"metadata": map[string]interface{}{
					"errorDetails": "Expected a base64 image such as data:image/png;base64,aW1n, but received application/octet-stream",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSyncedPayload(raw, "画布"); err != nil {
		t.Fatalf("validateSyncedPayload() error = %v", err)
	}
}

func TestValidateSyncedPayloadRejectsNestedInlineMedia(t *testing.T) {
	for _, content := range []string{
		"data:image/png;base64,aW1n",
		"  DATA:VIDEO/mp4;base64,dmlkZW8=",
		"data:audio/mpeg;base64,YXVkaW8=",
	} {
		raw, err := json.Marshal(map[string]interface{}{
			"nodes": []interface{}{
				map[string]interface{}{"metadata": map[string]interface{}{"content": content}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := validateSyncedPayload(raw, "画布"); err == nil {
			t.Fatalf("validateSyncedPayload(%q) error = nil", content)
		}
	}
}

func TestDeleteUserCanvasProjectRollsBackShareWhenProjectDeleteFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.CanvasProject{}, &model.CanvasShare{}, &model.CanvasChange{}, &model.CanvasCollaborator{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	project := &model.CanvasProject{ID: "canvas-delete-rollback", UserID: "user-1", Title: "待删除画布", PayloadJSON: `{"id":"canvas-delete-rollback","title":"待删除画布"}`, CreatedAt: now, UpdatedAt: now}
	share := &model.CanvasShare{ID: "share-delete-rollback", UserID: "user-1", ProjectID: project.ID, TokenHash: "share-delete-rollback-hash", Enabled: true, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(project).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(share).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_canvas_delete BEFORE DELETE ON canvas_projects BEGIN SELECT RAISE(ABORT, 'forced canvas delete failure'); END`).Error; err != nil {
		t.Fatal(err)
	}

	svc := New(repository.New(db), t.TempDir())
	if err := svc.DeleteUserCanvasProject("user-1", project.ID); err == nil {
		t.Fatal("DeleteUserCanvasProject() error = nil, want forced transaction failure")
	}
	if err := db.First(&model.CanvasProject{}, "id = ?", project.ID).Error; err != nil {
		t.Fatalf("canvas project must remain after rollback: %v", err)
	}
	if err := db.First(&model.CanvasShare{}, "id = ?", share.ID).Error; err != nil {
		t.Fatalf("canvas share must remain after rollback: %v", err)
	}
}

func TestDeleteUserAssetDeletesExclusiveOSSObject(t *testing.T) {
	deleteRequests := make(chan string, 1)
	server := useAssetDeletionOSSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodDelete {
			t.Fatalf("OSS request method = %s, want DELETE", request.Method)
		}
		deleteRequests <- request.URL.Path
		writer.WriteHeader(http.StatusNoContent)
	}))

	svc := newAssetDeletionTestService(t)
	saveAssetDeletionFixture(t, svc, server.URL, "asset-1", "resource-exclusive")

	if err := svc.DeleteUserAsset("user-1", "asset-1"); err != nil {
		t.Fatalf("DeleteUserAsset() error = %v", err)
	}
	select {
	case path := <-deleteRequests:
		if path != "/users/user-1/image/exclusive.png" {
			t.Fatalf("OSS DELETE path = %q", path)
		}
	default:
		t.Fatal("DeleteUserAsset() did not delete the OSS object")
	}
	if _, err := svc.repo.AssetForUser("user-1", "asset-1"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("deleted asset lookup error = %v", err)
	}
	resource, err := svc.repo.Resource("resource-exclusive")
	if err != nil {
		t.Fatal(err)
	}
	if resource.Status != model.ResourceStatusDeleted {
		t.Fatalf("resource status = %s, want deleted", resource.Status)
	}
}

func TestDeleteUserAssetOSSFailureKeepsAssetAndResource(t *testing.T) {
	server := useAssetDeletionOSSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = writer.Write([]byte("temporary failure"))
	}))
	svc := newAssetDeletionTestService(t)
	saveAssetDeletionFixture(t, svc, server.URL, "asset-failure", "resource-failure")

	err := svc.DeleteUserAsset("user-1", "asset-failure")
	if err == nil || !strings.Contains(err.Error(), "OSS 对象删除失败") {
		t.Fatalf("DeleteUserAsset() error = %v", err)
	}
	if _, err := svc.repo.AssetForUser("user-1", "asset-failure"); err != nil {
		t.Fatalf("asset must remain after OSS failure: %v", err)
	}
	resource, err := svc.repo.Resource("resource-failure")
	if err != nil {
		t.Fatal(err)
	}
	if resource.Status != model.ResourceStatusReady {
		t.Fatalf("resource status = %s, want ready", resource.Status)
	}
}

func TestDeleteUserAssetAcceptsAlreadyMissingOSSObject(t *testing.T) {
	server := useAssetDeletionOSSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
	}))
	svc := newAssetDeletionTestService(t)
	saveAssetDeletionFixture(t, svc, server.URL, "asset-missing-object", "resource-missing-object")

	if err := svc.DeleteUserAsset("user-1", "asset-missing-object"); err != nil {
		t.Fatalf("DeleteUserAsset() error = %v", err)
	}
	resource, err := svc.repo.Resource("resource-missing-object")
	if err != nil {
		t.Fatal(err)
	}
	if resource.Status != model.ResourceStatusDeleted {
		t.Fatalf("resource status = %s, want deleted", resource.Status)
	}
}

func TestDeleteUserAssetRetainsResourceReferencedByCanvas(t *testing.T) {
	deleteRequests := 0
	server := useAssetDeletionOSSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		deleteRequests++
		writer.WriteHeader(http.StatusNoContent)
	}))
	svc := newAssetDeletionTestService(t)
	saveAssetDeletionFixture(t, svc, server.URL, "asset-shared", "resource-shared")
	canvasPayload, err := json.Marshal(struct {
		Nodes []assetDeletionCanvasNode `json:"nodes"`
	}{
		Nodes: []assetDeletionCanvasNode{{
			Metadata: assetDeletionResourceData{StorageKey: "resource:resource-shared"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.Save(&model.CanvasProject{
		ID: "canvas-1", UserID: "user-1", PayloadJSON: string(canvasPayload),
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	if err := svc.DeleteUserAsset("user-1", "asset-shared"); err != nil {
		t.Fatalf("DeleteUserAsset() error = %v", err)
	}
	if deleteRequests != 0 {
		t.Fatalf("OSS DELETE requests = %d, want 0 for a referenced resource", deleteRequests)
	}
	resource, err := svc.repo.Resource("resource-shared")
	if err != nil {
		t.Fatal(err)
	}
	if resource.Status != model.ResourceStatusReady {
		t.Fatalf("referenced resource status = %s, want ready", resource.Status)
	}
}

func TestDeleteUserAssetsSharingResourceDeletesObjectAfterLastAsset(t *testing.T) {
	deleteRequests := 0
	server := useAssetDeletionOSSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		deleteRequests++
		writer.WriteHeader(http.StatusNoContent)
	}))
	svc := newAssetDeletionTestService(t)
	saveAssetDeletionFixture(t, svc, server.URL, "asset-shared-first", "resource-shared-assets")
	saveAssetDeletionRecord(t, svc, "asset-shared-second", "resource-shared-assets", time.Now())

	if err := svc.DeleteUserAsset("user-1", "asset-shared-first"); err != nil {
		t.Fatalf("DeleteUserAsset(first) error = %v", err)
	}
	if deleteRequests != 0 {
		t.Fatalf("OSS DELETE requests after first asset = %d, want 0", deleteRequests)
	}
	if err := svc.DeleteUserAsset("user-1", "asset-shared-second"); err != nil {
		t.Fatalf("DeleteUserAsset(second) error = %v", err)
	}
	if deleteRequests != 1 {
		t.Fatalf("OSS DELETE requests after last asset = %d, want 1", deleteRequests)
	}
	resource, err := svc.repo.Resource("resource-shared-assets")
	if err != nil {
		t.Fatal(err)
	}
	if resource.Status != model.ResourceStatusDeleted {
		t.Fatalf("resource status = %s, want deleted", resource.Status)
	}
}

func TestDeleteUserAssetRejectsResourceInActiveMigration(t *testing.T) {
	deleteRequests := 0
	server := useAssetDeletionOSSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		deleteRequests++
		writer.WriteHeader(http.StatusNoContent)
	}))
	svc := newAssetDeletionTestService(t)
	saveAssetDeletionFixture(t, svc, server.URL, "asset-migrating", "resource-migrating")
	if err := svc.repo.Save(&model.StorageMigrationItem{
		ID: "migration-item-1", JobID: "migration-job-1", ResourceID: "resource-migrating",
		Status: model.StorageMigrationItemPending, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	err := svc.DeleteUserAsset("user-1", "asset-migrating")
	if err == nil || !strings.Contains(err.Error(), "正在迁移") {
		t.Fatalf("DeleteUserAsset() error = %v", err)
	}
	if deleteRequests != 0 {
		t.Fatalf("OSS DELETE requests = %d, want 0 during migration", deleteRequests)
	}
	if _, err := svc.repo.AssetForUser("user-1", "asset-migrating"); err != nil {
		t.Fatalf("asset must remain during migration: %v", err)
	}
}

func TestDeleteUserAssetDeletesExclusiveLocalFile(t *testing.T) {
	svc := newAssetDeletionTestService(t)
	now := time.Now()
	objectKey := "users/user-1/image/local.png"
	filePath := filepath.Join(svc.dataDir, "resources", filepath.FromSlash(objectKey))
	if err := os.MkdirAll(filepath.Dir(filePath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte("img"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CreateResource(&model.Resource{
		ID: "resource-local-delete", UserID: "user-1", Kind: "image",
		Status: model.ResourceStatusReady, Provider: "local", ObjectKey: objectKey,
		MimeType: "image/png", Size: 3, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	saveAssetDeletionRecord(t, svc, "asset-local-delete", "resource-local-delete", now)

	if err := svc.DeleteUserAsset("user-1", "asset-local-delete"); err != nil {
		t.Fatalf("DeleteUserAsset() error = %v", err)
	}
	if _, err := os.Stat(filePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("local resource file still exists or stat failed: %v", err)
	}
}

func newAssetDeletionTestService(t *testing.T) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.SystemSetting{},
		&model.UserOSSSetting{},
		&model.Resource{},
		&model.Asset{},
		&model.AssetVersion{},
		&model.AssetRepresentation{},
		&model.CharacterVoiceBinding{},
		&model.ProjectAssetLink{},
		&model.Shot{},
		&model.ShotAssetReference{},
		&model.VoiceProfile{},
		&model.StorageMigrationItem{},
		&model.CanvasProject{},
		&model.Session{},
	); err != nil {
		t.Fatal(err)
	}
	return New(repository.New(db), t.TempDir())
}

type assetDeletionResourceData struct {
	StorageKey string `json:"storageKey"`
}

type assetDeletionPayload struct {
	ID    string                    `json:"id"`
	Kind  string                    `json:"kind"`
	Title string                    `json:"title"`
	Data  assetDeletionResourceData `json:"data"`
}

type assetDeletionCanvasNode struct {
	Metadata assetDeletionResourceData `json:"metadata"`
}

func useAssetDeletionOSSServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	serverAddress := server.Listener.Addr().String()
	originalTransport := outboundTransport
	outboundTransport = &http.Transport{
		DialContext: func(ctx context.Context, network string, _ string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: time.Second}
			return dialer.DialContext(ctx, network, serverAddress)
		},
	}
	t.Cleanup(func() {
		outboundTransport.CloseIdleConnections()
		outboundTransport = originalTransport
	})
	return server
}

func saveAssetDeletionFixture(t *testing.T, svc *Service, endpoint string, assetID string, resourceID string) {
	t.Helper()
	settingJSON, err := json.Marshal(ossSettingValue{
		Enabled: true, Provider: "aliyun", Endpoint: endpoint, Bucket: "media",
		AccessKeyID: "access-id", AccessKeySecret: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.SaveSystemSetting(&model.SystemSetting{Key: ossSettingKey, ValueJSON: string(settingJSON)}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := svc.repo.CreateResource(&model.Resource{
		ID: resourceID, UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady,
		Provider: "aliyun", Endpoint: endpoint, Bucket: "media",
		ObjectKey: "users/user-1/image/exclusive.png", MimeType: "image/png", Size: 3,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	saveAssetDeletionRecord(t, svc, assetID, resourceID, now)
}

func saveAssetDeletionRecord(t *testing.T, svc *Service, assetID string, resourceID string, now time.Time) {
	t.Helper()
	payload, err := json.Marshal(assetDeletionPayload{
		ID: assetID, Kind: "image", Title: "待删除素材",
		Data: assetDeletionResourceData{StorageKey: "resource:" + resourceID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.UpsertAsset(&model.Asset{
		ID: assetID, UserID: "user-1", Kind: "image", Title: "待删除素材",
		PayloadJSON: string(payload), CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
}
