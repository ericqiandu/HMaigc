package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
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
)

type publishedStaticObject struct {
	body         []byte
	cacheControl string
	contentType  string
}

func TestPublishStaticAssetsUploadsVersionedFilesAndManifest(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	var objectMu sync.Mutex
	objects := make(map[string]publishedStaticObject)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodPut:
			if request.Header.Get("x-oss-forbid-overwrite") != "true" {
				t.Error("static asset PUT must forbid overwrite")
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Error(err)
				writer.WriteHeader(http.StatusInternalServerError)
				return
			}
			sum := md5.Sum(body)
			etag := hex.EncodeToString(sum[:])
			objectMu.Lock()
			objects[request.URL.Path] = publishedStaticObject{
				body:         body,
				cacheControl: request.Header.Get("Cache-Control"),
				contentType:  request.Header.Get("Content-Type"),
			}
			objectMu.Unlock()
			writer.Header().Set("ETag", `"`+etag+`"`)
			writer.WriteHeader(http.StatusOK)
		case http.MethodHead:
			objectMu.Lock()
			object, exists := objects[request.URL.Path]
			objectMu.Unlock()
			if !exists {
				writer.WriteHeader(http.StatusNotFound)
				return
			}
			sum := md5.Sum(object.body)
			writer.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
			writer.Header().Set("Content-Length", fmt.Sprintf("%d", len(object.body)))
			writer.WriteHeader(http.StatusOK)
		case http.MethodGet:
			objectMu.Lock()
			object, exists := objects[request.URL.Path]
			objectMu.Unlock()
			if !exists {
				writer.WriteHeader(http.StatusNotFound)
				return
			}
			writer.Header().Set("Content-Type", object.contentType)
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write(object.body)
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

	buildDirectory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(buildDirectory, "assets"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(buildDirectory, "index.html"), []byte("server-owned"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(buildDirectory, "assets", "app.js"), []byte("console.log('release')"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(buildDirectory, "logo.svg"), []byte("<svg/>"), 0o640); err != nil {
		t.Fatal(err)
	}

	summary, err := PublishStaticAssets(buildDirectory, StaticAssetPublishConfig{
		Endpoint:        server.URL,
		Bucket:          "static-bucket",
		AccessKeyID:     "access-id",
		AccessKeySecret: "access-secret",
		PathPrefix:      "hmaigc/web",
		Release:         "v1.0.13",
		SourceCommit:    strings.Repeat("a", 40),
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Files != 2 || summary.ObjectPrefix != "hmaigc/web/releases/v1.0.13" {
		t.Fatalf("summary = %#v", summary)
	}

	objectMu.Lock()
	if _, exists := objects["/hmaigc/web/releases/v1.0.13/index.html"]; exists {
		objectMu.Unlock()
		t.Fatal("index.html must remain server-owned and must not be uploaded")
	}
	for _, objectPath := range []string{
		"/hmaigc/web/releases/v1.0.13/assets/app.js",
		"/hmaigc/web/releases/v1.0.13/logo.svg",
	} {
		object, exists := objects[objectPath]
		if !exists {
			objectMu.Unlock()
			t.Fatalf("missing uploaded object %s", objectPath)
		}
		if object.cacheControl != staticAssetCacheControl {
			objectMu.Unlock()
			t.Fatalf("%s cache-control = %q", objectPath, object.cacheControl)
		}
	}
	manifest, exists := objects["/hmaigc/web/releases/v1.0.13/manifest.json"]
	if !exists {
		objectMu.Unlock()
		t.Fatal("manifest must be committed last")
	}
	if !strings.Contains(string(manifest.body), `"assets/app.js"`) || !strings.Contains(string(manifest.body), `"sha256"`) {
		objectMu.Unlock()
		t.Fatalf("manifest = %s", manifest.body)
	}
	objectCount := len(objects)
	objectMu.Unlock()
	repeated, err := PublishStaticAssets(buildDirectory, StaticAssetPublishConfig{
		Endpoint:        server.URL,
		Bucket:          "static-bucket",
		AccessKeyID:     "access-id",
		AccessKeySecret: "access-secret",
		PathPrefix:      "hmaigc/web",
		Release:         "v1.0.13",
		SourceCommit:    strings.Repeat("a", 40),
	})
	if err != nil {
		t.Fatal(err)
	}
	if repeated.Files != summary.Files || repeated.Bytes != summary.Bytes {
		t.Fatalf("repeated summary = %#v", repeated)
	}
	objectMu.Lock()
	defer objectMu.Unlock()
	if len(objects) != objectCount {
		t.Fatal("idempotent publication must not upload objects again")
	}
}

func TestPublishStaticAssetsRejectsMutableReleaseName(t *testing.T) {
	_, err := PublishStaticAssets(t.TempDir(), StaticAssetPublishConfig{Release: "latest"})
	if err == nil || !strings.Contains(err.Error(), "不可变") {
		t.Fatalf("PublishStaticAssets() error = %v", err)
	}
}
