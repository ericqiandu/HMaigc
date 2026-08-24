package main

import (
	"database/sql"
	"encoding/json"
	"io"
	"os"

	"infinite-canvas/backend/internal/database"
)

const agentRuntimeRetirementAuditDatabaseError = "agent_runtime_retirement_audit_database_error"

type agentRuntimeRetirementAuditFailure struct {
	ErrorCode string `json:"errorCode"`
	Stage     string `json:"stage"`
}

func main() {
	os.Exit(run(os.Stdout, os.Stderr))
}

func run(stdout io.Writer, stderr io.Writer) int {
	db, err := database.Open(database.Config{
		Driver:  os.Getenv("CANVAS_DATABASE_DRIVER"),
		DSN:     os.Getenv("DATABASE_URL"),
		DataDir: os.Getenv("CANVAS_BACKEND_DATA_DIR"),
	})
	if err != nil {
		return writeAuditFailure(stderr, "open")
	}
	if err := database.ConfigurePool(db); err != nil {
		closeAuditDatabase(db)
		return writeAuditFailure(stderr, "configure_pool")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return writeAuditFailure(stderr, "database_handle")
	}
	audit, auditErr := database.AuditAgentRuntimeUpgrade(db)
	closeErr := sqlDB.Close()
	if auditErr != nil {
		return writeAuditFailure(stderr, "audit")
	}
	if closeErr != nil {
		return writeAuditFailure(stderr, "close")
	}
	if err := json.NewEncoder(stdout).Encode(audit); err != nil {
		return writeAuditFailure(stderr, "encode")
	}
	if len(audit.Blockers) != 0 {
		return 3
	}
	return 0
}

func closeAuditDatabase(db interface{ DB() (*sql.DB, error) }) {
	sqlDB, err := db.DB()
	if err == nil {
		_ = sqlDB.Close()
	}
}

func writeAuditFailure(stderr io.Writer, stage string) int {
	_ = json.NewEncoder(stderr).Encode(agentRuntimeRetirementAuditFailure{
		ErrorCode: agentRuntimeRetirementAuditDatabaseError,
		Stage:     stage,
	})
	return 2
}
