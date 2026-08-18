package database

import (
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTeamIntegritySchemaCreatesExactPartialUniqueIndex(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := EnsureTeamIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	var definition string
	if err := db.Raw("SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?", "idx_team_owner_creation").Scan(&definition).Error; err != nil {
		t.Fatal(err)
	}
	want := `CREATE UNIQUE INDEX idx_team_owner_creation ON teams(owner_user_id, creation_idempotency_key) WHERE creation_idempotency_key <> ''`
	if compactSchemaSQL(definition) != compactSchemaSQL(want) {
		t.Fatalf("team creation index = %q, want %q", definition, want)
	}
	definition = ""
	if err := db.Raw("SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?", "idx_team_pending_invitation_email").Scan(&definition).Error; err != nil {
		t.Fatal(err)
	}
	want = `CREATE UNIQUE INDEX idx_team_pending_invitation_email ON team_invitations(team_id, lower(email)) WHERE status = 'pending'`
	if compactSchemaSQL(definition) != compactSchemaSQL(want) {
		t.Fatalf("team invitation index = %q, want %q", definition, want)
	}
}

func TestTeamIntegritySchemaRejectsWrongExistingIndexDefinition(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX idx_team_owner_creation ON teams(owner_user_id)`).Error; err != nil {
		t.Fatal(err)
	}
	err = EnsureTeamIntegritySchema(db)
	if err == nil || !strings.Contains(err.Error(), "idx_team_owner_creation") {
		t.Fatalf("wrong team index error = %v", err)
	}
}
