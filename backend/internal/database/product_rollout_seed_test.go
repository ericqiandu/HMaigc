package database

import (
	"encoding/json"
	"testing"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMigrateSchemaPublishesDisabledProductRolloutConfiguration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}

	var stored model.SystemSetting
	if err := db.Where("key = ?", "product_rollouts").First(&stored).Error; err != nil {
		t.Fatalf("fresh deployment is missing required product rollout configuration: %v", err)
	}
	if stored.UpdatedBy != "system-migration" {
		t.Fatalf("rollout configuration updater = %q, want system-migration", stored.UpdatedBy)
	}
	var rules map[string]struct {
		Percentage     int      `json:"percentage"`
		AdminAllowlist []string `json:"adminAllowlist"`
	}
	if err := json.Unmarshal([]byte(stored.ValueJSON), &rules); err != nil {
		t.Fatalf("decode seeded rollout configuration: %v", err)
	}
	expectedKeys := []string{"canvasToolRegistryV2", "canvasMediaToolsV2", "canvasProductionNodesV2", "creativeWorkspaceV2", "skillLibraryV2"}
	if len(rules) != len(expectedKeys) {
		t.Fatalf("seeded rollout rule count = %d, want %d", len(rules), len(expectedKeys))
	}
	for _, key := range expectedKeys {
		rule, ok := rules[key]
		if !ok || rule.Percentage != 0 || len(rule.AdminAllowlist) != 0 {
			t.Fatalf("seeded rollout rule %s = %#v, present=%t; want disabled and empty allowlist", key, rule, ok)
		}
	}

	const configured = `{"canvasToolRegistryV2":{"percentage":100,"adminAllowlist":[]},"canvasMediaToolsV2":{"percentage":0,"adminAllowlist":[]},"canvasProductionNodesV2":{"percentage":0,"adminAllowlist":[]},"creativeWorkspaceV2":{"percentage":0,"adminAllowlist":[]},"skillLibraryV2":{"percentage":0,"adminAllowlist":[]}}`
	update := struct {
		ValueJSON string `gorm:"column:value_json"`
		UpdatedBy string `gorm:"column:updated_by"`
	}{ValueJSON: configured, UpdatedBy: "admin-1"}
	if err := db.Model(&model.SystemSetting{}).Where("key = ?", "product_rollouts").Select("value_json", "updated_by").Updates(update).Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("key = ?", "product_rollouts").First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ValueJSON != configured || stored.UpdatedBy != "admin-1" {
		t.Fatalf("schema migration overwrote operator configuration: %#v", stored)
	}
}
