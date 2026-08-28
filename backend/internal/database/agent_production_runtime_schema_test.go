package database

import (
	"testing"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEnsureAgentProductionRuntimeSchemaCreatesAppendOnlyTables(t *testing.T) {
	db := openProductionRuntimeSchemaSQLite(t)
	if err := EnsureAgentProductionRuntimeSchema(db); err != nil {
		t.Fatalf("migrate production runtime schema: %v", err)
	}
	for _, table := range []string{
		"agent_production_graph_versions",
		"agent_production_stages",
		"agent_specialist_runs",
		"agent_artifacts",
		"agent_artifact_revisions",
		"agent_asset_binding_revisions",
		"agent_asset_publications",
	} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("missing append-only production runtime table %s", table)
		}
	}
}

func TestAgentArtifactRevisionIdentityIsUnique(t *testing.T) {
	db := openProductionRuntimeSchemaSQLite(t)
	if err := EnsureAgentProductionRuntimeSchema(db); err != nil {
		t.Fatal(err)
	}
	first := model.AgentArtifactRevision{
		ID: "revision-1", ArtifactID: "artifact-1", Revision: 1,
		PayloadJSON: `{}`, UpstreamRevisionsJSON: `[]`, SkillVersionsJSON: `[]`, SchemaVersion: 1,
	}
	duplicate := first
	duplicate.ID = "revision-2"
	if err := db.Create(&first).Error; err != nil {
		t.Fatalf("create first artifact revision: %v", err)
	}
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate artifact revision number was accepted")
	}
}

func TestAgentAssetPublicationIdentityIsUniquePerScopeAndPurpose(t *testing.T) {
	db := openProductionRuntimeSchemaSQLite(t)
	if err := EnsureAgentProductionRuntimeSchema(db); err != nil {
		t.Fatal(err)
	}
	first := model.AgentAssetPublication{
		ID: "publication-1", TenantKind: "personal", TenantID: "user-1", DomainProjectID: "project-1",
		ArtifactRevisionID: "revision-1", PublicationPurpose: "character_reference",
	}
	duplicate := first
	duplicate.ID = "publication-2"
	if err := db.Create(&first).Error; err != nil {
		t.Fatalf("create first asset publication: %v", err)
	}
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate scoped publication purpose was accepted")
	}
}

func TestEnsureAgentProductionRuntimeSchemaRejectsWrongNamedIndex(t *testing.T) {
	db := openProductionRuntimeSchemaSQLite(t)
	if err := db.AutoMigrate(&model.AgentProductionGraphVersion{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX idx_agent_graph_scope_key_version ON agent_production_graph_versions (tenant_kind, tenant_id, graph_key, version)`).Error; err != nil {
		t.Fatal(err)
	}

	if err := EnsureAgentProductionRuntimeSchema(db); err == nil {
		t.Fatal("migration trusted a wrong index that reused the required name")
	}
}

func openProductionRuntimeSchemaSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/agent-production-runtime.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}
