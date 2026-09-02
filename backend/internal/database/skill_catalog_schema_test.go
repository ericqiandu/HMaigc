package database

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/skillcatalog"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMigrateSchemaPublishesFirstPartySkillVersions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"skills", "skill_versions"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("missing first-party skill table %s", table)
		}
	}
	type publishedSkill struct {
		Dir                    string
		Status                 string
		CurrentVersionID       string
		Version                int
		Checksum               string
		Instructions           string
		CapabilityManifestJSON string
	}
	var skills []publishedSkill
	if err := db.Table("skills").
		Select("skills.dir, skills.status, skills.current_version_id, skill_versions.version, skill_versions.checksum, skill_versions.instructions, skill_versions.capability_manifest_json").
		Joins("JOIN skill_versions ON skill_versions.id = skills.current_version_id").
		Order("skills.dir ASC").
		Scan(&skills).Error; err != nil {
		t.Fatal(err)
	}
	if len(skills) < 3 {
		t.Fatalf("published first-party skills = %d, want at least 3", len(skills))
	}
	builtins, err := skillcatalog.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	expectedVersions := make(map[string]int, len(builtins))
	for _, builtin := range builtins {
		expectedVersions[builtin.Dir] = builtin.Version
	}
	for _, skill := range skills {
		var manifest agentruntime.SkillCapabilityManifest
		manifestError := json.Unmarshal([]byte(skill.CapabilityManifestJSON), &manifest)
		if skill.Status != "published" || skill.CurrentVersionID == "" || skill.Version != expectedVersions[skill.Dir] || len(skill.Checksum) != 64 || skill.Instructions == "" ||
			manifestError != nil || agentruntime.ValidateSkillCapabilityManifest(manifest) != nil {
			t.Fatalf("invalid published skill facts: %#v", skill)
		}
	}
}

func TestMigrateSchemaPublishesGovernedViMaxDerivedSkillVersions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}

	const pinnedRevision = "05a48943878312d88fe5a016c12a9654940ecc43"
	pinnedSourceURL := "https://github.com/HKUDS/ViMax/tree/" + pinnedRevision
	expectedDirs := []string{
		"camera-tree-continuity",
		"character-visual-bible",
		"first-motion-last-frame",
		"storyboard-cinematic-language",
		"visual-consistency-review",
		"visual-evidence-analysis",
	}
	expectedVersions := map[string]int{
		"camera-tree-continuity":        2,
		"character-visual-bible":        2,
		"first-motion-last-frame":       2,
		"storyboard-cinematic-language": 1,
		"visual-consistency-review":     2,
		"visual-evidence-analysis":      2,
	}
	type publishedSkill struct {
		Dir                    string
		Status                 string
		Version                int
		Checksum               string
		CapabilityManifestJSON string
		SourceKind             string
		SourceURL              string
		SourceRevision         string
		SourceLicense          string
		PublishedAt            *time.Time
	}
	var skills []publishedSkill
	if err := db.Table("skills").
		Select("skills.dir, skills.status, skills.source_kind, skills.source_url, skills.source_revision, skills.source_license, skill_versions.version, skill_versions.checksum, skill_versions.capability_manifest_json, skill_versions.published_at").
		Joins("JOIN skill_versions ON skill_versions.id = skills.current_version_id").
		Where("skills.dir IN ?", expectedDirs).
		Order("skills.dir ASC").
		Scan(&skills).Error; err != nil {
		t.Fatal(err)
	}
	if len(skills) != len(expectedDirs) {
		t.Fatalf("published ViMax-derived skills = %d, want %d: %#v", len(skills), len(expectedDirs), skills)
	}
	for index, skill := range skills {
		var manifest agentruntime.SkillCapabilityManifest
		manifestError := json.Unmarshal([]byte(skill.CapabilityManifestJSON), &manifest)
		if skill.Dir != expectedDirs[index] || skill.Status != string(model.SkillStatusPublished) || skill.Version != expectedVersions[skill.Dir] ||
			len(skill.Checksum) != 64 || skill.SourceKind != "adapted" || skill.SourceURL != pinnedSourceURL ||
			skill.SourceRevision != pinnedRevision || skill.SourceLicense != "MIT" || skill.PublishedAt == nil ||
			manifestError != nil || agentruntime.ValidateSkillCapabilityManifest(manifest) != nil {
			t.Fatalf("invalid published ViMax-derived skill facts: %#v", skill)
		}
	}
}

func TestMigrateSchemaPublishesNewSkillVersionWithoutMutatingHistory(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Skill{}, &model.SkillVersion{}); err != nil {
		t.Fatal(err)
	}
	publishedAt := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	legacySkill := model.Skill{
		ID: "skill-short-drama-director", Dir: "short-drama-director", Name: "短剧总导演",
		Description: "legacy description", Visibility: "public", Status: model.SkillStatusPublished,
		CurrentVersionID: "skill-short-drama-director-v1", SourceKind: "original", SourceLicense: "HMaigc-Proprietary",
	}
	legacyVersion := model.SkillVersion{
		ID: "skill-short-drama-director-v1", SkillID: legacySkill.ID, Version: 1,
		Instructions: "legacy v1 instructions", Checksum: "legacy-v1-checksum", Changelog: "legacy v1",
		CreatedBy: "system", PublishedAt: &publishedAt, CreatedAt: publishedAt,
	}
	if err := db.Create(&legacySkill).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&legacyVersion).Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	var preserved model.SkillVersion
	if err := db.Where("id = ?", legacyVersion.ID).First(&preserved).Error; err != nil {
		t.Fatal(err)
	}
	if preserved.Instructions != legacyVersion.Instructions || preserved.Checksum != legacyVersion.Checksum || preserved.Version != 1 {
		t.Fatalf("published v1 history changed: %#v", preserved)
	}
	var current model.Skill
	if err := db.Where("id = ?", legacySkill.ID).First(&current).Error; err != nil {
		t.Fatal(err)
	}
	builtins, err := skillcatalog.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	expectedCurrentVersionID := ""
	for _, builtin := range builtins {
		if builtin.Dir == "short-drama-director" {
			expectedCurrentVersionID = "skill-short-drama-director-v" + strconv.Itoa(builtin.Version)
			break
		}
	}
	if current.CurrentVersionID != expectedCurrentVersionID {
		t.Fatalf("current version = %s, want %s", current.CurrentVersionID, expectedCurrentVersionID)
	}
	var versionCount int64
	if err := db.Model(&model.SkillVersion{}).Where("skill_id = ?", legacySkill.ID).Count(&versionCount).Error; err != nil {
		t.Fatal(err)
	}
	if versionCount != 2 {
		t.Fatalf("skill version count = %d, want 2", versionCount)
	}
}

func TestMigrateSchemaRejectsChangedPublishedSkillCapabilityManifest(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	currentVersionID := firstPublishedSkillCurrentVersionID(t, db)
	if err := db.Table("skill_versions").Where("id = ?", currentVersionID).
		Update("capability_manifest_json", `{"specialists":["audio"],"tools":[],"artifactSchemas":["tampered.v1"]}`).Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err == nil {
		t.Fatal("changed published skill capability manifest must stop deployment")
	}
}

func TestMigrateSchemaRejectsChangedPublishedSkillVersion(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Table("skill_versions").Where("id = ?", firstPublishedSkillCurrentVersionID(t, db)).Update("checksum", "tampered").Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err == nil {
		t.Fatal("changed published skill version must stop deployment")
	}
}

func TestMigrateSchemaKeepsPublishedSkillCatalogTimestampsStable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	type catalogFact struct {
		UpdatedAt time.Time
		Versions  int64
	}
	var before catalogFact
	if err := db.Table("skills").Select("updated_at").Order("dir ASC").Limit(1).Scan(&before).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("skill_versions").Count(&before.Versions).Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	var after catalogFact
	if err := db.Table("skills").Select("updated_at").Order("dir ASC").Limit(1).Scan(&after).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("skill_versions").Count(&after.Versions).Error; err != nil {
		t.Fatal(err)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) || after.Versions != before.Versions {
		t.Fatalf("idempotent migration changed catalog facts: before=%#v after=%#v", before, after)
	}
}

func TestMigrateSchemaRejectsMissingPublishedSkillTimestamp(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Table("skill_versions").Where("id = ?", firstPublishedSkillCurrentVersionID(t, db)).Update("published_at", nil).Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err == nil {
		t.Fatal("published skill version without publication timestamp must stop deployment")
	}
}

func firstPublishedSkillCurrentVersionID(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var versionID string
	if err := db.Table("skills").Where("status = ?", model.SkillStatusPublished).Order("dir ASC").Limit(1).Pluck("current_version_id", &versionID).Error; err != nil {
		t.Fatal(err)
	}
	if versionID == "" {
		t.Fatal("missing current published skill version")
	}
	return versionID
}
