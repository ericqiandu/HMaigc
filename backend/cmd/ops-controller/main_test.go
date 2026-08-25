package main

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateControllerConfigurationIsReadOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	production := filepath.Join(root, "production.env")
	control := filepath.Join(root, "control.env")
	writeControllerFixture(t, production, "HMAIGC_VERSION=v1.0.58\n"+
		"HMAIGC_BACKEND_IMAGE=example.invalid/backend@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"+
		"HMAIGC_WEB_IMAGE=example.invalid/web@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n"+
		"BACKUP_HELPER_IMAGE=example.invalid/backup@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd\n"+
		"HMAIGC_OPS_STATE_VOLUME=hmaigc-ops-state\nCANVAS_ENVIRONMENT=production\n")
	writeControllerFixture(t, control, "HMAIGC_OPS_IMAGE=example.invalid/ops@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc\n"+
		"HMAIGC_OPS_VERSION=v1.0.58\nHMAIGC_OPS_PROTOCOL_VERSION=1\n"+
		"HMAIGC_OPS_STATE_VOLUME=hmaigc-ops-state\nCANVAS_ENVIRONMENT=production\n")
	before := fixtureDigest(t, production, control)

	if err := validateControllerConfiguration(production, control); err != nil {
		t.Fatal(err)
	}
	after := fixtureDigest(t, production, control)
	if before != after {
		t.Fatal("validation mutated canonical configuration")
	}
	if _, err := os.Stat(filepath.Join(root, "controller.db")); !os.IsNotExist(err) {
		t.Fatalf("validation created controller state: %v", err)
	}
}

func TestParseSocketModeRejectsWorldWritableControllerSocket(t *testing.T) {
	if _, err := parseSocketMode("0666"); err == nil {
		t.Fatal("controller socket must never be writable by users outside the configured backend group")
	}
	for _, value := range []string{"0600", "0660"} {
		if _, err := parseSocketMode(value); err != nil {
			t.Fatalf("expected %s to remain valid: %v", value, err)
		}
	}
}

func writeControllerFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fixtureDigest(t *testing.T, paths ...string) [32]byte {
	t.Helper()
	hash := sha256.New()
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = hash.Write(data)
	}
	var result [32]byte
	copy(result[:], hash.Sum(nil))
	return result
}
