package database

import (
	"strings"
	"testing"
	"time"

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
		"agent_character_identity_versions",
		"agent_shot_binding_revisions",
	} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("missing append-only production runtime table %s", table)
		}
	}
}

func TestEnsureAgentProductionRuntimeSchemaCreatesScopedIdentityIndexesIdempotently(t *testing.T) {
	db := openProductionRuntimeSchemaSQLite(t)
	if err := EnsureAgentProductionRuntimeSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := EnsureAgentProductionRuntimeSchema(db); err != nil {
		t.Fatalf("second migration failed: %v", err)
	}
	for _, name := range []string{
		"idx_agent_identity_scope_character_version",
		"idx_agent_shot_binding_scope_shot_character_revision",
	} {
		found := false
		for _, specification := range agentProductionRuntimeIntegrityIndexes {
			if specification.name == name {
				found = true
				if !strings.Contains(specification.columns, "tenant_kind,tenant_id,domain_project_id,canvas_id") {
					t.Fatalf("index %s does not preserve tenant/project/canvas scope: %s", name, specification.columns)
				}
				exists, err := verifyAgentRuntimeIntegrityIndex(db, specification)
				if err != nil || !exists {
					t.Fatalf("verify index %s: exists=%t err=%v", name, exists, err)
				}
			}
		}
		if !found {
			t.Fatalf("missing index specification %s", name)
		}
	}
}

func TestCharacterIdentityVersionIsImmutableAndUniqueWithinScope(t *testing.T) {
	db := openProductionRuntimeSchemaSQLite(t)
	if err := EnsureAgentProductionRuntimeSchema(db); err != nil {
		t.Fatal(err)
	}
	first := productionIdentityVersionFixture("identity-1")
	if err := db.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	duplicate := first
	duplicate.ID = "identity-duplicate"
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate scoped identity version was accepted")
	}
	if err := db.Model(&model.AgentCharacterIdentityVersion{}).
		Where("id = ?", first.ID).Update("resource_id", "resource-rewritten").Error; err == nil {
		t.Fatal("immutable identity version was updated")
	}
	var stored model.AgentCharacterIdentityVersion
	if err := db.First(&stored, "id = ?", first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ResourceID != first.ResourceID {
		t.Fatalf("identity resource = %q, want %q", stored.ResourceID, first.ResourceID)
	}
	if err := db.Delete(&model.AgentCharacterIdentityVersion{}, "id = ?", first.ID).Error; err == nil {
		t.Fatal("immutable identity version was deleted")
	}
}

func TestShotBindingRevisionIsImmutableAndUniqueWithinScope(t *testing.T) {
	db := openProductionRuntimeSchemaSQLite(t)
	if err := EnsureAgentProductionRuntimeSchema(db); err != nil {
		t.Fatal(err)
	}
	first := model.AgentShotBindingRevision{
		ID: "shot-binding-1", TenantKind: "personal", TenantID: "user-1", ActorUserID: "user-1",
		DomainProjectID: "project-1", CanvasID: "canvas-1", ThreadID: "thread-1", RunID: "run-1",
		ShotKey: "shot-1", CharacterKey: "character-1", Revision: 1, ShotArtifactRevisionID: "shot-r1",
		IdentityVersionID: "identity-1", ResourceID: "resource-1",
		DependencyHash: strings.Repeat("b", 64), LifecycleStatus: "current", CreatedAt: time.Now().UTC(),
	}
	if err := db.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	duplicate := first
	duplicate.ID = "shot-binding-duplicate"
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate scoped shot binding revision was accepted")
	}
	if err := db.Model(&model.AgentShotBindingRevision{}).
		Where("id = ?", first.ID).Update("resource_id", "resource-rewritten").Error; err == nil {
		t.Fatal("immutable shot binding revision was updated")
	}
	if err := db.Delete(&model.AgentShotBindingRevision{}, "id = ?", first.ID).Error; err == nil {
		t.Fatal("immutable shot binding revision was deleted")
	}
}

func productionIdentityVersionFixture(id string) model.AgentCharacterIdentityVersion {
	return model.AgentCharacterIdentityVersion{
		ID: id, TenantKind: "personal", TenantID: "user-1", ActorUserID: "user-1",
		DomainProjectID: "project-1", CanvasID: "canvas-1", ThreadID: "thread-1", RunID: "run-1",
		CharacterKey: "character-1", Version: 1, CharacterBibleRevisionID: "character-bible-r1",
		ResourceID: "resource-1", DependencyHash: strings.Repeat("a", 64), LifecycleStatus: "current",
		CreatedAt: time.Now().UTC(),
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
