package opscontroller

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRollbackReadinessRequiresVerifiedBackupInsideRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	backupPath := filepath.Join(root, "20260731-000000--v1.0.10")
	createVerifiedBackup(t, backupPath, "v1.0.10")
	state := ReleaseState{PreviousVersion: "v1.0.10", RollbackBackup: backupPath}

	ready, status := inspectRollbackReadiness(root, state)
	if !ready {
		t.Fatalf("expected verified rollback point, got %s", status)
	}

	if err := os.WriteFile(filepath.Join(backupPath, "postgres.dump"), []byte("corrupted"), 0o600); err != nil {
		t.Fatal(err)
	}
	ready, status = inspectRollbackReadiness(root, state)
	if ready || status == "" {
		t.Fatalf("expected corrupted rollback point to be rejected: ready=%v status=%q", ready, status)
	}

	outside := filepath.Join(t.TempDir(), "outside")
	createVerifiedBackup(t, outside, "v1.0.10")
	ready, status = inspectRollbackReadiness(root, ReleaseState{PreviousVersion: "v1.0.10", RollbackBackup: outside})
	if ready || status != "恢复点不在受控备份目录内" {
		t.Fatalf("expected outside rollback point rejection, got ready=%v status=%q", ready, status)
	}
}

func createVerifiedBackup(t *testing.T, path string, version string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"postgres.dump":    []byte("postgres"),
		"backend-data.tgz": []byte("backend"),
		"manifest.env":     []byte("VERSION=" + version + "\nCREATED_AT=" + time.Now().UTC().Format(time.RFC3339) + "\n"),
	}
	checksums := ""
	for _, name := range []string{"postgres.dump", "backend-data.tgz", "manifest.env"} {
		content := files[name]
		if err := os.WriteFile(filepath.Join(path, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(content)
		checksums += hex.EncodeToString(hash[:]) + "  " + name + "\n"
	}
	if err := os.WriteFile(filepath.Join(path, "SHA256SUMS"), []byte(checksums), 0o600); err != nil {
		t.Fatal(err)
	}
}
