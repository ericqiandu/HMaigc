package database

import (
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEnsureAgentRuntimeIntegritySchemaCreatesExactIndexes(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := EnsureAgentRuntimeIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"idx_agent_runs_thread_client_request": `CREATE UNIQUE INDEX idx_agent_runs_thread_client_request ON agent_runs(thread_id, client_request_id)`,
		"idx_agent_run_events_run_sequence":    `CREATE UNIQUE INDEX idx_agent_run_events_run_sequence ON agent_run_events(run_id, sequence)`,
		"idx_agent_checkpoints_run_sequence":   `CREATE UNIQUE INDEX idx_agent_checkpoints_run_sequence ON agent_checkpoints(run_id, sequence)`,
		"idx_agent_tool_calls_action":          `CREATE UNIQUE INDEX idx_agent_tool_calls_action ON agent_tool_calls(run_id, tool_call_id, action_version)`,
		"idx_agent_threads_scope":              `CREATE INDEX idx_agent_threads_scope ON agent_threads(tenant_kind, tenant_id, canvas_id, updated_at)`,
	}
	for name, expected := range want {
		var actual string
		if err := db.Raw("SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?", name).Scan(&actual).Error; err != nil {
			t.Fatal(err)
		}
		if compactSQL(actual) != compactSQL(expected) {
			t.Fatalf("index %s SQL = %q, want %q", name, actual, expected)
		}
	}
}

func TestEnsureAgentRuntimeIntegritySchemaRejectsWrongNamedIndex(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	row := model.AgentRunEvent{
		ID: "event-1", RunID: "run-1", Sequence: 1,
		Kind: agentruntime.EventRunCreated, PayloadJSON: `{}`, CreatedAt: now,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	const wrong = `CREATE UNIQUE INDEX idx_agent_run_events_run_sequence ON agent_run_events(run_id, kind)`
	if err := db.Exec(wrong).Error; err != nil {
		t.Fatal(err)
	}
	err := EnsureAgentRuntimeIntegritySchema(db)
	if err == nil || !strings.Contains(err.Error(), "idx_agent_run_events_run_sequence") {
		t.Fatalf("wrong index error = %v", err)
	}
	var stored model.AgentRunEvent
	if err := db.First(&stored, "id = ?", row.ID).Error; err != nil {
		t.Fatalf("existing event changed: %v", err)
	}
	var definition string
	if err := db.Raw("SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?", "idx_agent_run_events_run_sequence").Scan(&definition).Error; err != nil {
		t.Fatal(err)
	}
	if compactSQL(definition) != compactSQL(wrong) {
		t.Fatalf("wrong index was silently rewritten: %q", definition)
	}
}

func TestEnsureAgentRuntimeIntegritySchemaRejectsLegacyDuplicateFacts(t *testing.T) {
	db := openAgentRuntimeSchemaSQLite(t)
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	rows := []model.AgentRunEvent{
		{ID: "event-a", RunID: "run-duplicate", Sequence: 1, Kind: agentruntime.EventRunCreated, PayloadJSON: `{}`, CreatedAt: now},
		{ID: "event-b", RunID: "run-duplicate", Sequence: 1, Kind: agentruntime.EventRunStatusChanged, PayloadJSON: `{}`, CreatedAt: now},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	err := EnsureAgentRuntimeIntegritySchema(db)
	if err == nil || !strings.Contains(err.Error(), "run-duplicate") {
		t.Fatalf("duplicate event error = %v", err)
	}
	var count int64
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ?", "run-duplicate").Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("duplicate historical rows changed: count=%d err=%v", count, err)
	}
}

func TestMigrateSchemaAddsAgentRuntimeWithoutChangingCanvasFacts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/agent-additive.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.User{}, &model.CanvasProject{}, &model.CanvasChange{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	user := model.User{ID: "user-existing", Username: "existing", DisplayName: "Existing", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}
	canvas := model.CanvasProject{ID: "canvas-existing", UserID: user.ID, ProjectID: "project-existing", Title: "Existing Canvas", PayloadJSON: `{"nodes":[]}`, Revision: 7, CreatedAt: now, UpdatedAt: now}
	change := model.CanvasChange{ID: "change-existing", CanvasID: canvas.ID, Revision: 7, ActorUserID: user.ID, ClientMutationID: "mutation-existing", PayloadJSON: `{"type":"replace"}`, CreatedAt: now}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&canvas).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&change).Error; err != nil {
		t.Fatal(err)
	}

	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []any{&model.AgentThread{}, &model.AgentRun{}, &model.AgentRunEvent{}, &model.AgentCheckpoint{}, &model.AgentToolCall{}} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("missing additive agent runtime table for %T", table)
		}
	}
	for _, column := range []string{"state_version"} {
		if !db.Migrator().HasColumn(&model.AgentRun{}, column) {
			t.Fatalf("missing additive agent_runs column %s", column)
		}
	}
	for _, column := range []string{"risk_level", "required_access", "approval_required", "approval_decision", "approval_by_user_id", "approval_decided_at", "idempotency_key"} {
		if !db.Migrator().HasColumn(&model.AgentToolCall{}, column) {
			t.Fatalf("missing additive agent_tool_calls column %s", column)
		}
	}
	var storedCanvas model.CanvasProject
	if err := db.First(&storedCanvas, "id = ?", canvas.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedCanvas.Revision != canvas.Revision || storedCanvas.PayloadJSON != canvas.PayloadJSON || storedCanvas.Title != canvas.Title {
		t.Fatalf("canvas changed during additive migration: %#v", storedCanvas)
	}
	var storedChange model.CanvasChange
	if err := db.First(&storedChange, "id = ?", change.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedChange.Revision != change.Revision || storedChange.PayloadJSON != change.PayloadJSON || storedChange.ClientMutationID != change.ClientMutationID {
		t.Fatalf("canvas change altered during additive migration: %#v", storedChange)
	}
	var indexCount int64
	if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", "idx_agent_run_events_run_sequence").Scan(&indexCount).Error; err != nil {
		t.Fatal(err)
	}
	if indexCount != 1 {
		t.Fatal("MigrateSchema did not install agent runtime integrity indexes")
	}
}

func openAgentRuntimeSchemaSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return db
}
