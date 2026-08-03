package database

import "testing"

func TestOpenSQLiteUsesPortableJournalMode(t *testing.T) {
	t.Parallel()

	db, err := Open(Config{Driver: "sqlite", DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close sqlite database: %v", err)
		}
	})

	var journalMode string
	if err := db.Raw("PRAGMA journal_mode").Scan(&journalMode).Error; err != nil {
		t.Fatalf("read journal mode: %v", err)
	}
	if journalMode != "delete" {
		t.Fatalf("journal mode = %q, want delete", journalMode)
	}

	var foreignKeys int
	if err := db.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil {
		t.Fatalf("read foreign key mode: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}
}
