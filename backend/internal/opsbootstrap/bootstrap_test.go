package opsbootstrap

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"infinite-canvas/backend/internal/opsprotocol"
	"infinite-canvas/backend/internal/opsstate"

	_ "github.com/mattn/go-sqlite3"
)

func TestBootstrapRefusesActiveHistoricalOperation(t *testing.T) {
	t.Parallel()
	fixture := newBootstrapFixture(t)
	fixture.insertLegacyOperation(t, opsprotocol.OperationRunning)

	err := Bootstrap(fixture.input())
	if !errors.Is(err, ErrActiveLegacyOperation) {
		t.Fatalf("got %v", err)
	}
	assertFileAbsent(t, filepath.Join(fixture.stateRoot, "config", "control.env"))
}

func TestBootstrapImportsTerminalHistoryWithoutChangingIDs(t *testing.T) {
	t.Parallel()
	fixture := newBootstrapFixture(t)
	legacy := fixture.insertLegacyOperation(t, opsprotocol.OperationFailed)

	if err := Bootstrap(fixture.input()); err != nil {
		t.Fatal(err)
	}
	journal, err := opsstate.NewJournal(fixture.stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	result, err := journal.ReadResult(legacy.id)
	if err != nil {
		t.Fatal(err)
	}
	if result.OperationID != legacy.id || !result.CompletedAt.Equal(legacy.completedAt) {
		t.Fatalf("%+v", result)
	}
	if _, err := os.Stat(filepath.Join(fixture.stateRoot, "bootstrap", "legacy-controller.db")); err != nil {
		t.Fatalf("verified legacy backup missing: %v", err)
	}
}

func TestBootstrapIsIdempotentForSameSourceAndRejectsDifferentFacts(t *testing.T) {
	t.Parallel()
	fixture := newBootstrapFixture(t)
	fixture.insertLegacyOperation(t, opsprotocol.OperationSucceeded)
	input := fixture.input()
	if err := Bootstrap(input); err != nil {
		t.Fatal(err)
	}
	if err := Bootstrap(input); err != nil {
		t.Fatalf("same bootstrap input must be idempotent: %v", err)
	}
	input.ControllerVersion = "v1.0.59"
	if err := Bootstrap(input); !errors.Is(err, ErrBootstrapSourceMismatch) {
		t.Fatalf("got %v", err)
	}
}

func TestBootstrapCapturesCommittedWALFactsInConsistentSQLiteSnapshot(t *testing.T) {
	fixture := newBootstrapFixture(t)
	if _, err := fixture.db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec("PRAGMA wal_autocheckpoint=0"); err != nil {
		t.Fatal(err)
	}
	legacy := fixture.insertLegacyOperation(t, opsprotocol.OperationSucceeded)

	if err := Bootstrap(fixture.input()); err != nil {
		t.Fatal(err)
	}
	backup, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(filepath.Join(fixture.stateRoot, "bootstrap", "legacy-controller.db"))+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	var count int
	if err := backup.QueryRow("SELECT COUNT(*) FROM operation_records WHERE id = ?", legacy.id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("consistent bootstrap snapshot lost committed WAL operation %s", legacy.id)
	}
}

func TestBootstrapResumesSameSourceAfterInterruptedAttempt(t *testing.T) {
	fixture := newBootstrapFixture(t)
	fixture.insertLegacyOperation(t, opsprotocol.OperationSucceeded)
	input := fixture.input()
	snapshotPath, cleanup, err := createConsistentSQLiteSnapshot(input.SourceDatabase)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	databaseHash, err := fileHash(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	environmentHash, err := fileHash(input.SourceEnvironment)
	if err != nil {
		t.Fatal(err)
	}
	marker := bootstrapMarker{
		SourceDatabaseSHA256: databaseHash, SourceEnvironmentSHA256: environmentHash,
		ControllerImage: input.ControllerImage, ControllerVersion: input.ControllerVersion,
		ProtocolVersion: input.ProtocolVersion, StateVolume: input.StateVolume,
	}
	encoded, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	bootstrapDirectory := filepath.Join(input.StateRoot, "bootstrap")
	if err := os.MkdirAll(bootstrapDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(filepath.Join(bootstrapDirectory, "in-progress.json"), append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifiedCopy(snapshotPath, filepath.Join(bootstrapDirectory, "legacy-controller.db"), databaseHash); err != nil {
		t.Fatal(err)
	}

	if err := Bootstrap(input); err != nil {
		t.Fatalf("same-source interrupted bootstrap should resume after verifying durable facts: %v", err)
	}
	if err := ValidateStateReadOnly(input.StateRoot); err != nil {
		t.Fatal(err)
	}
}

type bootstrapFixture struct {
	t         *testing.T
	root      string
	stateRoot string
	sourceDB  string
	sourceEnv string
	db        *sql.DB
}

type legacyFact struct {
	id          string
	completedAt time.Time
}

func newBootstrapFixture(t *testing.T) *bootstrapFixture {
	t.Helper()
	root := t.TempDir()
	databasePath := filepath.Join(root, "controller.db")
	database, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	_, err = database.Exec(`CREATE TABLE operation_records (
		id TEXT PRIMARY KEY, action TEXT, target_version TEXT, current_version_at_start TEXT,
		result_version TEXT, status TEXT, phase TEXT, error TEXT, exit_code INTEGER,
		actor_user_id TEXT, actor_display_name TEXT, idempotency_key TEXT, request_hash TEXT,
		created_at DATETIME, started_at DATETIME, completed_at DATETIME, updated_at DATETIME
	)`)
	if err != nil {
		t.Fatal(err)
	}
	sourceEnv := filepath.Join(root, "legacy.env")
	contents := "HMAIGC_IMAGE_REGISTRY=example.invalid\nHMAIGC_VERSION=v1.0.57\n" +
		"HMAIGC_BACKEND_IMAGE=example.invalid/hmaigc-backend@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n" +
		"HMAIGC_WEB_IMAGE=example.invalid/hmaigc-web@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n" +
		"BACKUP_HELPER_IMAGE=alpine@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd\n" +
		"HMAIGC_OPS_COMPOSE_PROJECT_NAME=hmaigc-ops\nHMAIGC_OPS_STATE_VOLUME=hmaigc-ops-state\n" +
		"HMAIGC_BACKEND_GID=101\nCANVAS_ENVIRONMENT=production\nPOSTGRES_PASSWORD=fixture-secret\n"
	if err := os.WriteFile(sourceEnv, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return &bootstrapFixture{t: t, root: root, stateRoot: filepath.Join(root, "state"), sourceDB: databasePath, sourceEnv: sourceEnv, db: database}
}

func (f *bootstrapFixture) input() Input {
	return Input{
		SourceDatabase: f.sourceDB, SourceEnvironment: f.sourceEnv, StateRoot: f.stateRoot,
		ControllerImage:   "example.invalid/hmaigc-ops-controller@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		ControllerVersion: "v1.0.58", ProtocolVersion: "1", StateVolume: "hmaigc-ops-state",
	}
}

func (f *bootstrapFixture) insertLegacyOperation(t *testing.T, status opsprotocol.OperationStatus) legacyFact {
	t.Helper()
	now := time.Date(2026, 8, 25, 1, 2, 3, 0, time.UTC)
	id := "legacy-" + string(status)
	var completedAt *time.Time
	if opsprotocol.IsTerminalStatus(status) {
		completedAt = &now
	}
	_, err := f.db.Exec(`INSERT INTO operation_records
		(id, action, target_version, current_version_at_start, result_version, status, phase, error,
		exit_code, actor_user_id, actor_display_name, idempotency_key, request_hash,
		created_at, started_at, completed_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, string(opsprotocol.ActionUpgrade), "v1.0.57", "v1.0.56", "v1.0.57", string(status),
		"legacy", "legacy failure", 1, "admin-id", "admin", "legacy-idempotency-0001-"+string(status), "", now, now, completedAt, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	return legacyFact{id: id, completedAt: now}
}

func assertFileAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected %s to be absent, got %v", path, err)
	}
}
