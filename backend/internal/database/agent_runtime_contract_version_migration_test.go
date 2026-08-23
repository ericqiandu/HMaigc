package database

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMigrateLegacyAgentContractVersionsLabelsTerminalRunWithoutRewritingFacts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	state := legacyAgentRuntimeStateV1{
		StateVersion: 4, StepNumber: 2, MaxSteps: 8, Status: agentruntime.RunSucceeded,
		FinalMessage: "旧运行结果", UserMessage: "旧运行请求",
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	completedAt := now.Add(time.Minute)
	run := model.AgentRun{
		ID: "legacy-terminal-run", ThreadID: "legacy-thread", ActorUserID: "legacy-user",
		ClientRequestID: "legacy-request", Status: state.Status, LastEventSequence: 7,
		StateVersion: state.StateVersion, StepNumber: state.StepNumber, MaxSteps: state.MaxSteps,
		ModelRecordID: "legacy-model-record", ModelKey: "legacy-model", ToolSchemaVersion: 1,
		RuntimeVersion: 0, PolicyVersion: 0, CreatedAt: now, UpdatedAt: completedAt, CompletedAt: &completedAt,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	checkpoint := model.AgentCheckpoint{
		ID: "legacy-terminal-checkpoint", RunID: run.ID, Sequence: run.LastEventSequence,
		StateVersion: state.StateVersion, StateJSON: string(stateJSON), CreatedAt: completedAt,
	}
	if err := db.Create(&checkpoint).Error; err != nil {
		t.Fatal(err)
	}
	event := model.AgentRunEvent{
		ID: "legacy-terminal-event", RunID: run.ID, Sequence: run.LastEventSequence,
		Kind: agentruntime.EventRunCompleted, PayloadJSON: string(stateJSON), CreatedAt: completedAt,
	}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}

	if err := migrateLegacyAgentContractVersions(db); err != nil {
		t.Fatal(err)
	}
	if err := migrateLegacyAgentContractVersions(db); err != nil {
		t.Fatalf("idempotent migration: %v", err)
	}

	var stored model.AgentRun
	if err := db.First(&stored, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.RuntimeVersion != 1 || stored.PolicyVersion != 1 {
		t.Fatalf("contract versions = %d/%d, want 1/1", stored.RuntimeVersion, stored.PolicyVersion)
	}
	if stored.Status != run.Status || stored.StateVersion != run.StateVersion || stored.StepNumber != run.StepNumber ||
		stored.LastEventSequence != run.LastEventSequence || stored.ModelRecordID != run.ModelRecordID || stored.ModelKey != run.ModelKey ||
		stored.CompletedAt == nil || !stored.CompletedAt.Equal(completedAt) {
		t.Fatalf("legacy run facts changed: %#v", stored)
	}
	var storedCheckpoint model.AgentCheckpoint
	if err := db.First(&storedCheckpoint, "id = ?", checkpoint.ID).Error; err != nil {
		t.Fatal(err)
	}
	var storedEvent model.AgentRunEvent
	if err := db.First(&storedEvent, "id = ?", event.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedCheckpoint.StateJSON != checkpoint.StateJSON || storedEvent.PayloadJSON != event.PayloadJSON {
		t.Fatal("migration rewrote immutable Agent event or checkpoint facts")
	}
	var completed int64
	if err := db.Raw("SELECT COUNT(*) FROM data_migrations WHERE id = ? AND completed_at IS NOT NULL", agentContractVersionMigrationID).Scan(&completed).Error; err != nil {
		t.Fatal(err)
	}
	if completed != 1 {
		t.Fatalf("completed migration records = %d, want 1", completed)
	}
}

func TestMigrateLegacyAgentContractVersionsRejectsAmbiguousZeroVersionWithoutMutation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	completedAt := now.Add(time.Minute)
	run := model.AgentRun{
		ID: "ambiguous-version-run", ThreadID: "legacy-thread", ActorUserID: "legacy-user",
		ClientRequestID: "ambiguous-request", Status: agentruntime.RunFailed, LastEventSequence: 3,
		StateVersion: 2, StepNumber: 1, MaxSteps: 8, ModelRecordID: "legacy-model-record",
		ModelKey: "legacy-model", ToolSchemaVersion: 1, RuntimeVersion: 0, PolicyVersion: 1,
		CreatedAt: now, UpdatedAt: completedAt, CompletedAt: &completedAt,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}

	err = migrateLegacyAgentContractVersions(db)
	if err == nil || !strings.Contains(err.Error(), run.ID) || !strings.Contains(err.Error(), "runtime_version=0") || !strings.Contains(err.Error(), "policy_version=1") {
		t.Fatalf("ambiguous version error = %v", err)
	}
	var stored model.AgentRun
	if err := db.First(&stored, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.RuntimeVersion != 0 || stored.PolicyVersion != 1 {
		t.Fatalf("ambiguous versions changed to %d/%d", stored.RuntimeVersion, stored.PolicyVersion)
	}
	var completed int64
	if err := db.Raw("SELECT COUNT(*) FROM data_migrations WHERE id = ? AND completed_at IS NOT NULL", agentContractVersionMigrationID).Scan(&completed).Error; err != nil {
		t.Fatal(err)
	}
	if completed != 0 {
		t.Fatalf("failed migration records = %d, want 0", completed)
	}
}

func TestMigrateLegacyAgentContractVersionsRejectsTerminalRunWithoutMatchingCheckpoint(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	completedAt := now.Add(time.Minute)
	run := model.AgentRun{
		ID: "missing-checkpoint-run", ThreadID: "legacy-thread", ActorUserID: "legacy-user",
		ClientRequestID: "missing-checkpoint-request", Status: agentruntime.RunFailed, LastEventSequence: 3,
		StateVersion: 2, StepNumber: 1, MaxSteps: 8, ModelRecordID: "legacy-model-record",
		ModelKey: "legacy-model", ToolSchemaVersion: 1, RuntimeVersion: 0, PolicyVersion: 0,
		CreatedAt: now, UpdatedAt: completedAt, CompletedAt: &completedAt,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}

	err = migrateLegacyAgentContractVersions(db)
	if err == nil || !strings.Contains(err.Error(), run.ID) || !strings.Contains(err.Error(), "checkpoint") {
		t.Fatalf("missing checkpoint error = %v", err)
	}
	var stored model.AgentRun
	if err := db.First(&stored, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.RuntimeVersion != 0 || stored.PolicyVersion != 0 {
		t.Fatalf("run without checkpoint changed to %d/%d", stored.RuntimeVersion, stored.PolicyVersion)
	}
}

func TestMigrateLegacyAgentContractVersionsRejectsNewUnversionedRunAfterCompletion(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := migrateLegacyAgentContractVersions(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	completedAt := now.Add(time.Minute)
	run := model.AgentRun{
		ID: "post-migration-unversioned-run", ThreadID: "legacy-thread", ActorUserID: "legacy-user",
		ClientRequestID: "post-migration-request", Status: agentruntime.RunFailed, LastEventSequence: 3,
		StateVersion: 2, StepNumber: 1, MaxSteps: 8, ModelRecordID: "legacy-model-record",
		ModelKey: "legacy-model", ToolSchemaVersion: 1, RuntimeVersion: 0, PolicyVersion: 0,
		CreatedAt: now, UpdatedAt: completedAt, CompletedAt: &completedAt,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}

	err = migrateLegacyAgentContractVersions(db)
	if err == nil || !strings.Contains(err.Error(), run.ID) || !strings.Contains(err.Error(), "迁移完成后") {
		t.Fatalf("post-migration unversioned run error = %v", err)
	}
}

func TestMigrateSchemaRunsLegacyAgentContractVersionMigration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	state := legacyAgentRuntimeStateV1{
		StateVersion: 3, StepNumber: 2, MaxSteps: 8, Status: agentruntime.RunFailed,
		FailureCode: "legacy_failure", UserMessage: "旧运行请求",
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	completedAt := now.Add(time.Minute)
	run := model.AgentRun{
		ID: "schema-legacy-terminal-run", ThreadID: "schema-legacy-thread", ActorUserID: "legacy-user",
		ClientRequestID: "schema-legacy-request", Status: state.Status, LastEventSequence: 5,
		StateVersion: state.StateVersion, StepNumber: state.StepNumber, MaxSteps: state.MaxSteps,
		ModelRecordID: "legacy-model-record", ModelKey: "legacy-model", ToolSchemaVersion: 1,
		RuntimeVersion: 0, PolicyVersion: 0, CreatedAt: now, UpdatedAt: completedAt, CompletedAt: &completedAt,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AgentCheckpoint{
		ID: "schema-legacy-terminal-checkpoint", RunID: run.ID, Sequence: run.LastEventSequence,
		StateVersion: state.StateVersion, StateJSON: string(stateJSON), CreatedAt: completedAt,
	}).Error; err != nil {
		t.Fatal(err)
	}

	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	var stored model.AgentRun
	if err := db.First(&stored, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.RuntimeVersion != 1 || stored.PolicyVersion != 1 {
		t.Fatalf("MigrateSchema contract versions = %d/%d, want 1/1", stored.RuntimeVersion, stored.PolicyVersion)
	}
}
