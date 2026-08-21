package database

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMigrateSchemaBackfillsFrozenSkillChecksumsInEventsAndCheckpoints(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	payload := `{"stateVersion":1,"stepNumber":0,"maxSteps":8,"status":"queued","userMessage":"使用导演技能","configuration":{"generationModels":{},"skills":[{"dir":"legacy-director","name":"旧导演","description":"","instructions":"冻结的历史导演指令","version":1}],"attachments":[],"executionMode":"guided"},"clarificationHistory":[]}`
	now := time.Now().UTC()
	if err := db.Create(&model.AgentCheckpoint{ID: "legacy-checkpoint", RunID: "legacy-run", Sequence: 1, StateVersion: 1, StateJSON: payload, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AgentRunEvent{ID: "legacy-event", RunID: "legacy-run", Sequence: 1, Kind: agentruntime.EventRunCreated, PayloadJSON: payload, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	expectedDigest := sha256.Sum256([]byte("冻结的历史导演指令"))
	expectedChecksum := hex.EncodeToString(expectedDigest[:])
	for table, column := range map[string]string{"agent_checkpoints": "state_json", "agent_run_events": "payload_json"} {
		var migrated string
		if err := db.Table(table).Where("id = ?", map[string]string{"agent_checkpoints": "legacy-checkpoint", "agent_run_events": "legacy-event"}[table]).Pluck(column, &migrated).Error; err != nil {
			t.Fatal(err)
		}
		var state agentruntime.RuntimeState
		if err := json.Unmarshal([]byte(migrated), &state); err != nil {
			t.Fatal(err)
		}
		if len(state.Configuration.Skills) != 1 || state.Configuration.Skills[0].Checksum != expectedChecksum || state.UserMessage != "使用导演技能" {
			t.Fatalf("invalid migrated %s fact: %#v", table, state)
		}
	}
}

func TestAddFrozenSkillChecksumsRejectsConflictingExistingChecksum(t *testing.T) {
	payload := `{"configuration":{"skills":[{"instructions":"冻结指令","checksum":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}}`
	if _, _, err := addFrozenSkillChecksums(payload); err == nil {
		t.Fatal("conflicting historical skill checksum was accepted")
	}
}

func TestMigrateAgentRuntimeSkillChecksumsSkipsCompletedVersion(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := migrateAgentRuntimeSkillChecksums(db); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	conflicting := `{"configuration":{"skills":[{"instructions":"新写入事实","checksum":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}}`
	if err := db.Create(&model.AgentRunEvent{
		ID: "post-migration-event", RunID: "post-migration-run", Sequence: 1,
		Kind: agentruntime.EventRunCreated, PayloadJSON: conflicting, CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	if err := migrateAgentRuntimeSkillChecksums(db); err != nil {
		t.Fatalf("completed migration scanned historical facts again: %v", err)
	}
	var completed int64
	if err := db.Raw("SELECT COUNT(*) FROM data_migrations WHERE id = ? AND completed_at IS NOT NULL", agentRuntimeSkillChecksumMigrationID).Scan(&completed).Error; err != nil {
		t.Fatal(err)
	}
	if completed != 1 {
		t.Fatalf("completed migration records = %d, want 1", completed)
	}
}

func TestMigrateAgentRuntimeSkillChecksumsRecordsCompletionOnlyAfterSuccess(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	conflicting := `{"configuration":{"skills":[{"instructions":"冻结指令","checksum":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}}`
	if err := db.Create(&model.AgentCheckpoint{
		ID: "retry-checkpoint", RunID: "retry-run", Sequence: 1,
		StateVersion: 1, StateJSON: conflicting, CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	if err := migrateAgentRuntimeSkillChecksums(db); err == nil {
		t.Fatal("conflicting historical fact completed the migration")
	}
	var completed int64
	if err := db.Raw("SELECT COUNT(*) FROM data_migrations WHERE id = ? AND completed_at IS NOT NULL", agentRuntimeSkillChecksumMigrationID).Scan(&completed).Error; err != nil {
		t.Fatal(err)
	}
	if completed != 0 {
		t.Fatalf("failed migration records = %d, want 0", completed)
	}

	valid := `{"configuration":{"skills":[{"instructions":"冻结指令"}]}}`
	if err := db.Model(&model.AgentCheckpoint{}).Where("id = ?", "retry-checkpoint").Update("state_json", valid).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateAgentRuntimeSkillChecksums(db); err != nil {
		t.Fatalf("retry after correcting historical fact: %v", err)
	}
	if err := db.Raw("SELECT COUNT(*) FROM data_migrations WHERE id = ? AND completed_at IS NOT NULL", agentRuntimeSkillChecksumMigrationID).Scan(&completed).Error; err != nil {
		t.Fatal(err)
	}
	if completed != 1 {
		t.Fatalf("successful retry records = %d, want 1", completed)
	}
}
