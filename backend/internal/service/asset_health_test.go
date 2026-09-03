package service

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
)

func TestUserAssetHealthClassifiesManagedAssetsFromStorageFacts(t *testing.T) {
	svc := newAssetDeletionTestService(t)
	now := time.Now().UTC()

	saveHealthResource(t, svc, model.Resource{
		ID: "resource-ready", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady,
		Provider: "local", ObjectKey: "users/user-1/image/ready.png", MimeType: "image/png",
		CreatedAt: now, UpdatedAt: now,
	})
	readyPath := filepath.Join(svc.dataDir, "resources", filepath.FromSlash("users/user-1/image/ready.png"))
	if err := os.MkdirAll(filepath.Dir(readyPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(readyPath, []byte("image"), 0o640); err != nil {
		t.Fatal(err)
	}
	saveAssetDeletionRecord(t, svc, "asset-ready", "resource-ready", now)

	saveHealthResource(t, svc, model.Resource{
		ID: "resource-file-missing", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady,
		Provider: "local", ObjectKey: "users/user-1/image/missing.png", MimeType: "image/png",
		CreatedAt: now, UpdatedAt: now,
	})
	saveAssetDeletionRecord(t, svc, "asset-file-missing", "resource-file-missing", now)
	saveAssetDeletionRecord(t, svc, "asset-record-missing", "resource-record-missing", now)
	saveHealthResource(t, svc, model.Resource{
		ID: "resource-foreign", UserID: "user-2", Kind: "image", Status: model.ResourceStatusReady,
		Provider: "local", ObjectKey: "users/user-2/image/foreign.png", MimeType: "image/png",
		CreatedAt: now, UpdatedAt: now,
	})
	saveAssetDeletionRecord(t, svc, "asset-foreign", "resource-foreign", now)
	saveHealthAsset(t, svc, model.Asset{
		ID: "asset-unmanaged", UserID: "user-1", Kind: "image", Title: "外部图片",
		PayloadJSON: `{"id":"asset-unmanaged","kind":"image","title":"外部图片","data":{"dataUrl":"https://cdn.example.com/image.png"}}`,
		CreatedAt:   now, UpdatedAt: now,
	})
	saveHealthAsset(t, svc, model.Asset{
		ID: "asset-text", UserID: "user-1", Kind: "text", Title: "文案",
		PayloadJSON: `{"id":"asset-text","kind":"text","title":"文案","data":{"content":"旁白"}}`,
		CreatedAt:   now, UpdatedAt: now,
	})

	health, err := svc.UserAssetHealth(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	byID := assetHealthByID(health)
	assertAssetHealth(t, byID, "asset-ready", AssetHealthAvailable)
	assertAssetHealth(t, byID, "asset-file-missing", AssetHealthMissing)
	assertAssetHealth(t, byID, "asset-record-missing", AssetHealthMissing)
	assertAssetHealth(t, byID, "asset-foreign", AssetHealthMissing)
	assertAssetHealth(t, byID, "asset-unmanaged", AssetHealthUnverified)
	assertAssetHealth(t, byID, "asset-text", AssetHealthNotApplicable)
}

func TestUserAssetHealthDistinguishesMissingOSSObjectsFromCheckFailures(t *testing.T) {
	server := useAssetDeletionOSSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodHead {
			t.Fatalf("method = %s", request.Method)
		}
		switch filepath.Base(request.URL.Path) {
		case "missing.png":
			writer.WriteHeader(http.StatusNotFound)
		case "error.png":
			writer.WriteHeader(http.StatusServiceUnavailable)
		default:
			writer.WriteHeader(http.StatusOK)
		}
	}))
	svc := newAssetDeletionTestService(t)
	saveHealthOSSSetting(t, svc, server.URL)
	now := time.Now().UTC()
	for _, fixture := range []struct {
		assetID    string
		resourceID string
		objectKey  string
	}{
		{assetID: "asset-oss-ready", resourceID: "resource-oss-ready", objectKey: "users/user-1/image/ready.png"},
		{assetID: "asset-oss-missing", resourceID: "resource-oss-missing", objectKey: "users/user-1/image/missing.png"},
		{assetID: "asset-oss-error", resourceID: "resource-oss-error", objectKey: "users/user-1/image/error.png"},
	} {
		saveHealthResource(t, svc, model.Resource{
			ID: fixture.resourceID, UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady,
			Provider: "aliyun", Endpoint: server.URL, Bucket: "media", ObjectKey: fixture.objectKey,
			MimeType: "image/png", CreatedAt: now, UpdatedAt: now,
		})
		saveAssetDeletionRecord(t, svc, fixture.assetID, fixture.resourceID, now)
	}

	health, err := svc.UserAssetHealth(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	byID := assetHealthByID(health)
	assertAssetHealth(t, byID, "asset-oss-ready", AssetHealthAvailable)
	assertAssetHealth(t, byID, "asset-oss-missing", AssetHealthMissing)
	assertAssetHealth(t, byID, "asset-oss-error", AssetHealthUnverified)
	if byID["asset-oss-error"].Reason == "" {
		t.Fatal("OSS check failure must retain a user-visible reason")
	}
}

func saveHealthResource(t *testing.T, svc *Service, resource model.Resource) {
	t.Helper()
	if err := svc.repo.CreateResource(&resource); err != nil {
		t.Fatal(err)
	}
}

func saveHealthAsset(t *testing.T, svc *Service, asset model.Asset) {
	t.Helper()
	if err := svc.repo.UpsertAsset(&asset); err != nil {
		t.Fatal(err)
	}
}

func saveHealthOSSSetting(t *testing.T, svc *Service, endpoint string) {
	t.Helper()
	value, err := json.Marshal(ossSettingValue{
		Enabled: true, Provider: "aliyun", Endpoint: endpoint, Bucket: "media",
		AccessKeyID: "access-id", AccessKeySecret: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.SaveSystemSetting(&model.SystemSetting{Key: ossSettingKey, ValueJSON: string(value)}); err != nil {
		t.Fatal(err)
	}
}

func assetHealthByID(items []UserAssetHealth) map[string]UserAssetHealth {
	result := make(map[string]UserAssetHealth, len(items))
	for _, item := range items {
		result[item.ID] = item
	}
	return result
}

func assertAssetHealth(t *testing.T, items map[string]UserAssetHealth, id string, want AssetHealthStatus) {
	t.Helper()
	item, exists := items[id]
	if !exists {
		t.Fatalf("missing health result for %s", id)
	}
	if item.Status != want {
		t.Fatalf("health[%s].Status = %q, want %q (reason=%q)", id, item.Status, want, item.Reason)
	}
}
