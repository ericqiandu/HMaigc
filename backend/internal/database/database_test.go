package database

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestOpenSQLiteUsesPortableJournalMode(t *testing.T) {
	t.Parallel()

	db, err := Open(Config{Driver: "sqlite", DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close sqlite database: %v", err)
		}
	})

	var journalMode string
	if err := db.Raw("PRAGMA journal_mode").Scan(&journalMode).Error; err != nil {
		t.Fatalf("read journal mode: %v", err)
	}
	if journalMode != "delete" {
		t.Fatalf("journal mode = %q, want delete", journalMode)
	}

	var foreignKeys int
	if err := db.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil {
		t.Fatalf("read foreign key mode: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}
}

func TestParameterizedLoggingDropsSensitiveQueryValues(t *testing.T) {
	t.Parallel()

	db, err := Open(Config{Driver: "sqlite", DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close sqlite database: %v", err)
		}
	})

	filter, ok := db.Logger.(gorm.ParamsFilter)
	if !ok {
		t.Fatal("configured GORM logger does not expose parameter filtering")
	}
	const tokenHashSentinel = "TASK4_TOKEN_HASH_SENTINEL_DATABASE_LOG"
	query, params := filter.ParamsFilter(
		context.Background(),
		"SELECT * FROM payment_checkout_sessions WHERE token_hash = ?",
		tokenHashSentinel,
	)
	if query != "SELECT * FROM payment_checkout_sessions WHERE token_hash = ?" {
		t.Fatalf("parameterized query changed = %q", query)
	}
	if strings.Contains(fmt.Sprint(params), tokenHashSentinel) || len(params) != 0 {
		t.Fatalf("GORM log parameters retained sensitive token hash: %#v", params)
	}
}

func TestParameterizedLoggingUsesPlaceholdersInRealGORMTrace(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: newParameterizedDatabaseLogger(&output).LogMode(logger.Info),
	})
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	if _, ok := db.Logger.(gorm.ParamsFilter); !ok {
		t.Fatalf("configured real GORM logger does not expose parameter filtering: %T", db.Logger)
	}
	if err := db.Exec("CREATE TABLE payment_checkout_sessions (token_hash TEXT)").Error; err != nil {
		t.Fatalf("create checkout table: %v", err)
	}
	const tokenHashSentinel = "TASK4_TOKEN_HASH_SENTINEL_GORM_TRACE"
	var rows []struct {
		TokenHash string
	}
	if err := db.Raw(
		"SELECT token_hash FROM payment_checkout_sessions WHERE token_hash = ?",
		tokenHashSentinel,
	).Scan(&rows).Error; err != nil {
		t.Fatalf("execute parameterized trace query: %v", err)
	}
	logged := output.String()
	if strings.Contains(logged, tokenHashSentinel) {
		t.Fatalf("real GORM trace interpolated token hash: %s", logged)
	}
	if !strings.Contains(logged, "token_hash = ?") {
		t.Fatalf("real GORM trace omitted parameterized SQL: %s", logged)
	}
}

func TestParameterizedLoggingSanitizesRawDriverErrors(t *testing.T) {
	t.Parallel()

	const rawDriverErrorSentinel = "TASK4_PROVIDER_ERROR_SENTINEL_GORM_DRIVER"
	var output bytes.Buffer
	databaseLogger := newParameterizedDatabaseLogger(&output).LogMode(logger.Info)
	databaseLogger.Trace(
		context.Background(),
		time.Now().Add(-time.Millisecond),
		func() (string, int64) {
			return "UPDATE payment_transactions SET code_url = ? WHERE token_hash = ?", 0
		},
		errors.New(rawDriverErrorSentinel),
	)
	logged := output.String()
	if strings.Contains(logged, rawDriverErrorSentinel) {
		t.Fatalf("GORM trace leaked raw driver error: %s", logged)
	}
	if !strings.Contains(logged, "database operation failed") {
		t.Fatalf("GORM trace omitted stable error classification: %s", logged)
	}
}

func TestParameterizedLoggingIgnoresRecordNotFound(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: newParameterizedDatabaseLogger(&output),
	})
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	if err := db.Exec("CREATE TABLE task4_records (id TEXT PRIMARY KEY)").Error; err != nil {
		t.Fatalf("create record-not-found table: %v", err)
	}
	output.Reset()
	const missingRecordSentinel = "TASK4_RECORD_NOT_FOUND_SENTINEL_GORM"
	var record struct {
		ID string
	}
	err = db.Table("task4_records").Where("id = ?", missingRecordSentinel).First(&record).Error
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("record lookup error = %v, want gorm.ErrRecordNotFound", err)
	}
	if logged := output.String(); logged != "" {
		t.Fatalf("record-not-found lookup was logged: %s", logged)
	}
}
