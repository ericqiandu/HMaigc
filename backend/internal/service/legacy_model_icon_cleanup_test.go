package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveLegacyChannelModelIconsOnlyRemovesRetiredDirectory(t *testing.T) {
	dataDir := t.TempDir()
	legacyDir := filepath.Join(dataDir, "model-icons")
	siblingDir := filepath.Join(dataDir, "assets")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(siblingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "old.png"), []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (&Service{dataDir: dataDir}).RemoveLegacyChannelModelIcons(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacyDir); !os.IsNotExist(err) {
		t.Fatalf("legacy directory still exists or stat failed: %v", err)
	}
	if _, err := os.Stat(siblingDir); err != nil {
		t.Fatalf("unrelated data directory was affected: %v", err)
	}
}
