package testsupport

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// OpenPaymentIntegrationPostgres opens an isolated schema in the required loopback PG17/Redis harness.
func OpenPaymentIntegrationPostgres(t testing.TB) *gorm.DB {
	t.Helper()
	required := strings.TrimSpace(os.Getenv("CANVAS_REQUIRE_INTEGRATION_TESTS")) == "1"
	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		if required {
			t.Fatal("CANVAS_REQUIRE_INTEGRATION_TESTS=1 but DATABASE_URL is empty")
		}
		t.Skip("DATABASE_URL is not configured for PostgreSQL integration tests")
	}
	parsed := requireLoopbackTestURL(t, dsn, "PostgreSQL")
	databaseName := strings.TrimPrefix(parsed.Path, "/")
	if !strings.Contains(strings.ToLower(databaseName), "test") && !strings.Contains(strings.ToLower(databaseName), "integration") {
		t.Fatalf("PostgreSQL integration database name %q must contain test or integration", databaseName)
	}
	redisURL := strings.TrimSpace(os.Getenv("REDIS_URL"))
	if redisURL == "" {
		t.Fatal("payment integration tests require REDIS_URL")
	}
	requireLoopbackTestURL(t, redisURL, "Redis")
	redisOptions, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("parse Redis URL: %v", err)
	}
	redisClient := redis.NewClient(redisOptions)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping Redis: %v", err)
	}
	t.Cleanup(func() { _ = redisClient.Close() })

	admin, err := database.Open(database.Config{Driver: "postgres", DSN: dsn})
	if err != nil {
		t.Fatalf("open PostgreSQL admin connection: %v", err)
	}
	schema := "payment_test_" + randomPaymentIntegrationSuffix(t)
	if err := admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	isolatedURL := *parsed
	query := isolatedURL.Query()
	query.Set("search_path", schema)
	isolatedURL.RawQuery = query.Encode()
	db, err := database.Open(database.Config{Driver: "postgres", DSN: isolatedURL.String()})
	if err != nil {
		t.Fatalf("open isolated PostgreSQL schema: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, sqlErr := db.DB(); sqlErr == nil {
			_ = sqlDB.Close()
		}
		if dropErr := admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`).Error; dropErr != nil {
			t.Errorf("drop isolated schema %s: %v", schema, dropErr)
		}
		if sqlDB, sqlErr := admin.DB(); sqlErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func requireLoopbackTestURL(t testing.TB, raw string, label string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		t.Fatalf("parse %s URL: %v", label, err)
	}
	host := parsed.Hostname()
	if !strings.EqualFold(host, "localhost") {
		address := net.ParseIP(host)
		if address == nil || !address.IsLoopback() {
			t.Fatalf("%s integration URL must use a loopback host, got %q", label, host)
		}
	}
	return parsed
}

func randomPaymentIntegrationSuffix(t testing.TB) string {
	t.Helper()
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(value[:])
}
