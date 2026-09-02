package opsconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateFilesAcceptsCanonicalDigestPinnedConfiguration(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	production := filepath.Join(root, "production.env")
	control := filepath.Join(root, "control.env")
	writeConfigFixture(t, production, canonicalProductionFixture)
	writeConfigFixture(t, control, canonicalControlFixture)

	if err := ValidateFiles(production, control); err != nil {
		t.Fatal(err)
	}
}

func TestValidateFilesRejectsTaggedControllerImage(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	production := filepath.Join(root, "production.env")
	control := filepath.Join(root, "control.env")
	writeConfigFixture(t, production, canonicalProductionFixture)
	writeConfigFixture(t, control, "HMAIGC_OPS_IMAGE=example.invalid/hmaigc-ops-controller:v1.0.58\n"+
		"HMAIGC_OPS_VERSION=v1.0.58\nHMAIGC_OPS_PROTOCOL_VERSION=1\n"+
		"HMAIGC_OPS_STATE_VOLUME=hmaigc-ops-state\nCANVAS_ENVIRONMENT=production\n")

	if err := ValidateFiles(production, control); err == nil {
		t.Fatal("expected tagged controller image to be rejected")
	}
}

func TestValidateFilesRejectsMissingBackupHelperDigest(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	production := filepath.Join(root, "production.env")
	control := filepath.Join(root, "control.env")
	writeConfigFixture(t, production, "HMAIGC_VERSION=v1.0.58\n"+
		"HMAIGC_BACKEND_IMAGE=example.invalid/hmaigc-backend@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"+
		"HMAIGC_WEB_IMAGE=example.invalid/hmaigc-web@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n"+
		"HMAIGC_OPS_STATE_VOLUME=hmaigc-ops-state\nCANVAS_ENVIRONMENT=production\n")
	writeConfigFixture(t, control, canonicalControlFixture)

	if err := ValidateFiles(production, control); err == nil {
		t.Fatal("expected missing backup helper digest to be rejected")
	}
}

func writeConfigFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

const canonicalProductionFixture = "HMAIGC_VERSION=v1.0.58\n" +
	"HMAIGC_BACKEND_IMAGE=example.invalid/hmaigc-backend@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n" +
	"HMAIGC_WEB_IMAGE=example.invalid/hmaigc-web@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n" +
	"BACKUP_HELPER_IMAGE=example.invalid/backup-helper@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd\n" +
	"HMAIGC_OPS_STATE_VOLUME=hmaigc-ops-state\nCANVAS_ENVIRONMENT=production\n"

const canonicalControlFixture = "HMAIGC_OPS_IMAGE=example.invalid/hmaigc-ops-controller@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc\n" +
	"HMAIGC_OPS_VERSION=v1.0.58\nHMAIGC_OPS_PROTOCOL_VERSION=1\n" +
	"HMAIGC_OPS_STATE_VOLUME=hmaigc-ops-state\nCANVAS_ENVIRONMENT=production\n"
