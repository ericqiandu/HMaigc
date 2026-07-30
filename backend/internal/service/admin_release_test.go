package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"infinite-canvas/backend/internal/buildinfo"
	"infinite-canvas/backend/internal/model"
)

func TestAdminReleaseNotesRequiresAdministrator(t *testing.T) {
	changelogPath := filepath.Join(t.TempDir(), "CHANGELOG.md")
	if err := os.WriteFile(changelogPath, []byte("# private release notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := &Service{}

	_, err := svc.AdminReleaseNotes(&model.User{ID: "user-1", Role: model.UserRoleUser}, changelogPath)
	var authErr *AuthError
	if !errors.As(err, &authErr) || authErr.Status != 403 {
		t.Fatal("ordinary user unexpectedly received release notes")
	}
}

func TestAdminReleaseNotesReturnsConfiguredFile(t *testing.T) {
	changelogPath := filepath.Join(t.TempDir(), "CHANGELOG.md")
	const content = "# HMaigc private release notes"
	if err := os.WriteFile(changelogPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := &Service{}

	result, err := svc.AdminReleaseNotes(&model.User{ID: "admin-1", Role: model.UserRoleAdmin}, changelogPath)
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != buildinfo.Version {
		t.Fatalf("expected version %q, got %q", buildinfo.Version, result.Version)
	}
	if result.Changelog != content {
		t.Fatalf("expected changelog %q, got %q", content, result.Changelog)
	}
}

func TestAdminReleaseNotesFailsWhenFileIsMissing(t *testing.T) {
	svc := &Service{}
	if _, err := svc.AdminReleaseNotes(&model.User{ID: "admin-1", Role: model.UserRoleAdmin}, filepath.Join(t.TempDir(), "missing.md")); err == nil {
		t.Fatal("missing changelog unexpectedly succeeded")
	}
}
