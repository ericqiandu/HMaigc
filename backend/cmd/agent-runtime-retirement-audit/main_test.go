package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
)

func TestRunReportsReadOnlyRetirementAuditExitCodes(t *testing.T) {
	tests := []struct {
		name          string
		seedBlocker   bool
		wantExitCode  int
		wantCandidate int
		wantBlockers  int
	}{
		{name: "no candidates", wantExitCode: 0},
		{name: "business blockers", seedBlocker: true, wantExitCode: 3, wantCandidate: 1, wantBlockers: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dsn := filepath.Join(t.TempDir(), "audit.db") + "?_foreign_keys=on"
			db, err := database.Open(database.Config{Driver: "sqlite", DSN: dsn, DataDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			if err := database.MigrateBaseSchema(db); err != nil {
				t.Fatal(err)
			}
			if test.seedBlocker {
				now := time.Date(2026, time.August, 24, 20, 0, 0, 0, time.UTC)
				thread := model.AgentThread{
					ID: "thread-command-audit", TenantKind: agentruntime.TenantPersonal, TenantID: "user-command-audit",
					CreatedByUserID: "user-command-audit", DomainProjectID: "project-command-audit", CanvasID: "canvas-command-audit",
					Status: agentruntime.ThreadActive, CreatedAt: now, UpdatedAt: now,
				}
				if err := db.Create(&thread).Error; err != nil {
					t.Fatal(err)
				}
				run := model.AgentRun{
					ID: "run-command-audit", ThreadID: thread.ID, ActorUserID: thread.CreatedByUserID,
					ClientRequestID: "request-command-audit", Status: agentruntime.RunWaitingInput,
					ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
					RuntimeVersion:    agentruntime.CurrentRuntimeVersion + 1, PolicyVersion: agentruntime.CurrentPolicyVersion,
					CreatedAt: now, UpdatedAt: now,
				}
				if err := db.Create(&run).Error; err != nil {
					t.Fatal(err)
				}
			}
			closeCommandTestDatabase(t, db)

			t.Setenv("CANVAS_DATABASE_DRIVER", "sqlite")
			t.Setenv("DATABASE_URL", dsn)
			t.Setenv("CANVAS_BACKEND_DATA_DIR", t.TempDir())
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			if got := run(&stdout, &stderr); got != test.wantExitCode {
				t.Fatalf("exit code = %d, want %d; stderr=%s", got, test.wantExitCode, stderr.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("unexpected stderr = %s", stderr.String())
			}
			var audit database.AgentRuntimeUpgradeAudit
			if err := json.Unmarshal(stdout.Bytes(), &audit); err != nil {
				t.Fatalf("decode stdout %q: %v", stdout.String(), err)
			}
			if audit.CandidateRuns != test.wantCandidate || len(audit.Blockers) != test.wantBlockers {
				t.Fatalf("audit = %#v", audit)
			}
		})
	}
}

func TestRunDoesNotCreateSchema(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "empty.db") + "?_foreign_keys=on"
	t.Setenv("CANVAS_DATABASE_DRIVER", "sqlite")
	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("CANVAS_BACKEND_DATA_DIR", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if got := run(&stdout, &stderr); got != 2 {
		t.Fatalf("exit code = %d, want 2", got)
	}
	if stdout.Len() != 0 {
		t.Fatalf("unexpected stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), `"errorCode":"agent_runtime_retirement_audit_database_error"`) {
		t.Fatalf("stderr = %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), `"stage":"audit"`) {
		t.Fatalf("stderr does not identify the safe failure stage: %s", stderr.String())
	}
	db, err := database.Open(database.Config{Driver: "sqlite", DSN: dsn, DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer closeCommandTestDatabase(t, db)
	if db.Migrator().HasTable(&model.AgentRun{}) {
		t.Fatal("read-only audit command created agent_runs table")
	}
}

func TestRunRedactsDatabaseConfigurationErrors(t *testing.T) {
	t.Setenv("CANVAS_DATABASE_DRIVER", "unsupported-secret-driver")
	t.Setenv("DATABASE_URL", "postgresql://secret-user:secret-password@example.invalid/secret-db")
	t.Setenv("CANVAS_BACKEND_DATA_DIR", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if got := run(&stdout, &stderr); got != 2 {
		t.Fatalf("exit code = %d, want 2", got)
	}
	for _, forbidden := range []string{"secret-user", "secret-password", "secret-db", "unsupported-secret-driver"} {
		if strings.Contains(stderr.String(), forbidden) {
			t.Fatalf("stderr leaked %q: %s", forbidden, stderr.String())
		}
	}
	if !strings.Contains(stderr.String(), `"stage":"open"`) {
		t.Fatalf("stderr does not identify the safe failure stage: %s", stderr.String())
	}
}

func TestRunReportsNonRetirableActiveRun(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "active.db") + "?_foreign_keys=on"
	db, err := database.Open(database.Config{Driver: "sqlite", DSN: dsn, DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 24, 20, 30, 0, 0, time.UTC)
	active := model.AgentRun{
		ID: "run-command-running", ThreadID: "thread-command-running", ActorUserID: "user-command-running",
		ClientRequestID: "request-command-running", Status: agentruntime.RunRunning,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion - 1, RuntimeVersion: 1, PolicyVersion: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&active).Error; err != nil {
		t.Fatal(err)
	}
	closeCommandTestDatabase(t, db)

	t.Setenv("CANVAS_DATABASE_DRIVER", "sqlite")
	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("CANVAS_BACKEND_DATA_DIR", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if got := run(&stdout, &stderr); got != 3 {
		t.Fatalf("exit code = %d, want 3; stderr=%s", got, stderr.String())
	}
	var audit database.AgentRuntimeUpgradeAudit
	if err := json.Unmarshal(stdout.Bytes(), &audit); err != nil {
		t.Fatal(err)
	}
	if audit.CandidateRuns != 1 || len(audit.Blockers) != 1 || audit.Blockers[0].RunID != active.ID ||
		audit.Blockers[0].Category != "non_retirable_active_status" {
		t.Fatalf("audit = %#v", audit)
	}
}

func closeCommandTestDatabase(t *testing.T, db interface{ DB() (*sql.DB, error) }) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
}
