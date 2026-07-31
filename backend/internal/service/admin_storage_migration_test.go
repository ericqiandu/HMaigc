package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestStorageMigrationUploadsVerifiesAndKeepsLocalSource(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	var objectMu sync.Mutex
	objects := make(map[string][]byte)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodPut:
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Error(err)
				writer.WriteHeader(http.StatusInternalServerError)
				return
			}
			sum := md5.Sum(body)
			etag := hex.EncodeToString(sum[:])
			objectMu.Lock()
			objects[request.URL.Path] = body
			objectMu.Unlock()
			writer.Header().Set("ETag", `"`+etag+`"`)
			writer.WriteHeader(http.StatusOK)
		case http.MethodHead:
			objectMu.Lock()
			body, exists := objects[request.URL.Path]
			objectMu.Unlock()
			if !exists {
				writer.WriteHeader(http.StatusNotFound)
				return
			}
			sum := md5.Sum(body)
			writer.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
			writer.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
			writer.WriteHeader(http.StatusOK)
		default:
			writer.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

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

	svc := newStorageMigrationTestService(t)
	admin := &model.User{ID: "admin-1", Username: "admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	if err := svc.repo.Create(admin); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateOSSSetting(admin, OSSSettingRequest{
		Enabled: true, Provider: "aliyun", Endpoint: server.URL, Bucket: "migration-bucket",
		AccessKeyID: "access-id", AccessKeySecret: "access-secret", PathPrefix: "hmaigc/prod",
	}); err != nil {
		t.Fatal(err)
	}

	payload := []byte("production-media")
	sourceObjectKey := "users/user-1/image/source.png"
	sourcePath := filepath.Join(svc.dataDir, "resources", filepath.FromSlash(sourceObjectKey))
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, payload, 0o640); err != nil {
		t.Fatal(err)
	}
	resource := &model.Resource{
		ID: "resource-1", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady,
		Provider: "local", ObjectKey: sourceObjectKey, MimeType: "image/png", Size: int64(len(payload)),
		CreatedAt: time.Now().Add(-time.Minute), UpdatedAt: time.Now().Add(-time.Minute),
	}
	if err := svc.repo.CreateResource(resource); err != nil {
		t.Fatal(err)
	}

	job, err := svc.StartStorageMigration(admin, StartStorageMigrationRequest{Confirmation: "MIGRATE LOCAL TO OSS"})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != model.StorageMigrationQueued || job.TotalItems != 1 || job.TotalBytes != int64(len(payload)) {
		t.Fatalf("queued job = %#v", job)
	}

	svc.processNextStorageMigration()

	completed, err := svc.repo.StorageMigrationJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != model.StorageMigrationSucceeded || completed.CommittedItems != 1 || completed.FailedItems != 0 {
		t.Fatalf("completed job = %#v", completed)
	}
	migrated, err := svc.repo.Resource(resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	if migrated.Provider != "aliyun" || migrated.Bucket != "migration-bucket" || migrated.ObjectKey != "hmaigc/prod/"+sourceObjectKey || migrated.ETag == "" {
		t.Fatalf("migrated resource = %#v", migrated)
	}
	if _, err := os.Stat(sourcePath); err != nil {
		t.Fatalf("local source was removed: %v", err)
	}
	objectMu.Lock()
	uploaded := append([]byte(nil), objects["/"+migrated.ObjectKey]...)
	objectMu.Unlock()
	if string(uploaded) != string(payload) {
		t.Fatalf("uploaded payload = %q", uploaded)
	}
	items, err := svc.repo.StorageMigrationItems(job.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Status != model.StorageMigrationItemCommitted || len(items[0].SourceSHA256) != 64 {
		t.Fatalf("migration items = %#v", items)
	}
}

func TestStorageMigrationFailsItemWhenLocalFileIsMissing(t *testing.T) {
	svc := newStorageMigrationTestService(t)
	now := time.Now()
	job := &model.StorageMigrationJob{
		ID: "job-missing", Status: model.StorageMigrationQueued, RequestedBy: "admin-1",
		TargetProvider: "aliyun", TargetEndpoint: "https://oss-cn-test.aliyuncs.com",
		TargetBucket: "bucket", TargetPrefix: "hmaigc/prod", TotalItems: 1, TotalBytes: 10,
		CreatedAt: now, UpdatedAt: now,
	}
	item := model.StorageMigrationItem{
		ID: "item-missing", JobID: job.ID, ResourceID: "resource-missing", Status: model.StorageMigrationItemPending,
		SourceObjectKey: "users/user-1/image/missing.png", TargetObjectKey: "hmaigc/prod/users/user-1/image/missing.png",
		Size: 10, CreatedAt: now, UpdatedAt: now,
	}
	if err := svc.repo.CreateStorageMigrationJob(job); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.AppendStorageMigrationItems([]model.StorageMigrationItem{item}); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CreateResource(&model.Resource{
		ID: item.ResourceID, UserID: "user-1", Status: model.ResourceStatusReady, Provider: "local",
		ObjectKey: item.SourceObjectKey, Size: item.Size,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.MarkStorageMigrationItemRunning(item.ID, now); err != nil {
		t.Fatal(err)
	}
	err := svc.migrateLocalResourceItem(job, &item, ossSettingValue{})
	if err == nil || !strings.Contains(err.Error(), "打开本地资源失败") {
		t.Fatalf("migrateLocalResourceItem() error = %v", err)
	}
}

func TestStorageMigrationJobCreationIsSerialized(t *testing.T) {
	svc := newStorageMigrationTestService(t)
	now := time.Now()
	first := &model.StorageMigrationJob{
		ID: "job-first", Status: model.StorageMigrationPreparing, RequestedBy: "admin-1",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := svc.repo.CreateStorageMigrationJobIfIdle(first); err != nil {
		t.Fatal(err)
	}
	second := &model.StorageMigrationJob{
		ID: "job-second", Status: model.StorageMigrationPreparing, RequestedBy: "admin-1",
		CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second),
	}
	if err := svc.repo.CreateStorageMigrationJobIfIdle(second); !errors.Is(err, repository.ErrStorageMigrationAlreadyActive) {
		t.Fatalf("CreateStorageMigrationJobIfIdle() error = %v", err)
	}
}

func TestStorageMigrationResumeFailsInterruptedSnapshot(t *testing.T) {
	svc := newStorageMigrationTestService(t)
	now := time.Now()
	job := &model.StorageMigrationJob{
		ID: "job-preparing", Status: model.StorageMigrationPreparing, RequestedBy: "admin-1",
		CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute),
	}
	if err := svc.repo.CreateStorageMigrationJob(job); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.ResumeStorageMigrations(now); err != nil {
		t.Fatal(err)
	}
	resumed, err := svc.repo.StorageMigrationJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != model.StorageMigrationFailed || resumed.CompletedAt == nil || !strings.Contains(resumed.Error, "快照") {
		t.Fatalf("resumed job = %#v", resumed)
	}
}

func TestStorageMigrationFatalFailureLeavesCurrentItemRetryable(t *testing.T) {
	svc := newStorageMigrationTestService(t)
	now := time.Now()
	job := &model.StorageMigrationJob{
		ID: "job-running", Status: model.StorageMigrationRunning, RequestedBy: "admin-1",
		TotalItems: 1, TotalBytes: 10, CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute),
	}
	item := model.StorageMigrationItem{
		ID: "item-running", JobID: job.ID, ResourceID: "resource-running",
		Status: model.StorageMigrationItemRunning, Size: 10, CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute),
	}
	if err := svc.repo.CreateStorageMigrationJob(job); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.AppendStorageMigrationItems([]model.StorageMigrationItem{item}); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.FailRunningStorageMigration(job.ID, "target configuration changed", now); err != nil {
		t.Fatal(err)
	}
	items, err := svc.repo.StorageMigrationItems(job.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Status != model.StorageMigrationItemPending {
		t.Fatalf("items after fatal failure = %#v", items)
	}
	if err := svc.repo.RetryFailedStorageMigration(job.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	retried, err := svc.repo.StorageMigrationJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retried.Status != model.StorageMigrationQueued {
		t.Fatalf("retried job = %#v", retried)
	}
}

func newStorageMigrationTestService(t *testing.T) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.User{},
		&model.SystemSetting{},
		&model.Resource{},
		&model.StorageMigrationJob{},
		&model.StorageMigrationItem{},
		&model.AdminAuditEvent{},
	); err != nil {
		t.Fatal(err)
	}
	return New(repository.New(db), t.TempDir())
}
