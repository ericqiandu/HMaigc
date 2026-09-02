package database

import (
	"fmt"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

var agentProductionRuntimeIntegrityIndexes = []agentRuntimeIntegrityIndex{
	{
		name: "idx_agent_graph_scope_key_version", table: "agent_production_graph_versions",
		columns: "tenant_kind,tenant_id,domain_project_id,canvas_id,thread_id,run_id,graph_key,version", unique: true,
		createSQL: `CREATE UNIQUE INDEX idx_agent_graph_scope_key_version ON agent_production_graph_versions (tenant_kind, tenant_id, domain_project_id, canvas_id, thread_id, run_id, graph_key, version)`,
	},
	{
		name: "idx_agent_artifact_scope_key", table: "agent_artifacts",
		columns: "tenant_kind,tenant_id,domain_project_id,canvas_id,thread_id,run_id,artifact_key", unique: true,
		createSQL: `CREATE UNIQUE INDEX idx_agent_artifact_scope_key ON agent_artifacts (tenant_kind, tenant_id, domain_project_id, canvas_id, thread_id, run_id, artifact_key)`,
	},
	{
		name: "idx_agent_binding_scope_key_revision", table: "agent_asset_binding_revisions",
		columns: "tenant_kind,tenant_id,domain_project_id,canvas_id,thread_id,run_id,binding_key,revision", unique: true,
		createSQL: `CREATE UNIQUE INDEX idx_agent_binding_scope_key_revision ON agent_asset_binding_revisions (tenant_kind, tenant_id, domain_project_id, canvas_id, thread_id, run_id, binding_key, revision)`,
	},
	{
		name: "idx_agent_asset_publication_identity", table: "agent_asset_publications",
		columns: "tenant_kind,tenant_id,domain_project_id,artifact_revision_id,publication_purpose", unique: true,
		createSQL: `CREATE UNIQUE INDEX idx_agent_asset_publication_identity ON agent_asset_publications (tenant_kind, tenant_id, domain_project_id, artifact_revision_id, publication_purpose)`,
	},
	{
		name: "idx_agent_identity_scope_character_version", table: "agent_character_identity_versions",
		columns: "tenant_kind,tenant_id,domain_project_id,canvas_id,thread_id,run_id,character_key,version", unique: true,
		createSQL: `CREATE UNIQUE INDEX idx_agent_identity_scope_character_version ON agent_character_identity_versions (tenant_kind, tenant_id, domain_project_id, canvas_id, thread_id, run_id, character_key, version)`,
	},
	{
		name: "idx_agent_shot_binding_scope_shot_character_revision", table: "agent_shot_binding_revisions",
		columns: "tenant_kind,tenant_id,domain_project_id,canvas_id,thread_id,run_id,shot_key,character_key,revision", unique: true,
		createSQL: `CREATE UNIQUE INDEX idx_agent_shot_binding_scope_shot_character_revision ON agent_shot_binding_revisions (tenant_kind, tenant_id, domain_project_id, canvas_id, thread_id, run_id, shot_key, character_key, revision)`,
	},
}

// EnsureAgentProductionRuntimeSchema installs only additive production-runtime
// tables and indexes. It never rewrites or backfills the legacy plan/artifact
// tables. The current v2 runtime keeps its existing ownership until the final
// hard cutover; only then do those legacy tables become read-only history.
func EnsureAgentProductionRuntimeSchema(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.AutoMigrate(
			&model.AgentRun{},
			&model.AgentProductionGraphVersion{},
			&model.AgentProductionStage{},
			&model.AgentSpecialistRun{},
			&model.AgentArtifact{},
			&model.AgentArtifactRevision{},
			&model.AgentAssetBindingRevision{},
			&model.AgentCharacterIdentityVersion{},
			&model.AgentShotBindingRevision{},
			&model.AgentAssetPublication{},
		); err != nil {
			return err
		}
		for _, specification := range agentProductionRuntimeIntegrityIndexes {
			exists, err := verifyAgentRuntimeIntegrityIndex(tx, specification)
			if err != nil {
				return err
			}
			if exists {
				continue
			}
			if err := tx.Exec(specification.createSQL).Error; err != nil {
				return fmt.Errorf("create agent production runtime integrity index %s: %w", specification.name, err)
			}
		}
		if err := ensureAgentProductionRuntimeImmutability(tx); err != nil {
			return err
		}
		return nil
	})
}

func ensureAgentProductionRuntimeImmutability(db *gorm.DB) error {
	tables := []string{"agent_character_identity_versions", "agent_shot_binding_revisions"}
	if db.Dialector.Name() == "postgres" {
		if err := db.Exec(`
			CREATE OR REPLACE FUNCTION reject_agent_production_immutable_mutation()
			RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN
				RAISE EXCEPTION 'agent production evidence is immutable';
			END;
			$$`).Error; err != nil {
			return fmt.Errorf("create agent production immutability function: %w", err)
		}
		for _, table := range tables {
			trigger := "trg_" + table + "_immutable"
			statement := fmt.Sprintf(`
				DO $$
				BEGIN
					IF NOT EXISTS (
						SELECT 1 FROM pg_trigger
						WHERE tgname = '%s' AND tgrelid = '%s'::regclass AND NOT tgisinternal
					) THEN
						CREATE TRIGGER %s BEFORE UPDATE OR DELETE ON %s
						FOR EACH ROW EXECUTE FUNCTION reject_agent_production_immutable_mutation();
					END IF;
				END;
				$$`, trigger, table, trigger, table)
			if err := db.Exec(statement).Error; err != nil {
				return fmt.Errorf("create agent production immutability trigger %s: %w", trigger, err)
			}
		}
		return nil
	}
	for _, table := range tables {
		for _, operation := range []string{"UPDATE", "DELETE"} {
			trigger := fmt.Sprintf("trg_%s_immutable_%s", table, operation)
			statement := fmt.Sprintf(
				`CREATE TRIGGER IF NOT EXISTS %s BEFORE %s ON %s BEGIN SELECT RAISE(ABORT, 'agent production evidence is immutable'); END`,
				trigger, operation, table,
			)
			if err := db.Exec(statement).Error; err != nil {
				return fmt.Errorf("create agent production immutability trigger %s: %w", trigger, err)
			}
		}
	}
	return nil
}
