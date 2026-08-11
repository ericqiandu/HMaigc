package service

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/testsupport"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProviderSecretRoundTripUsesCredentialVersionIDAAD(t *testing.T) {
	directory := t.TempDir()
	cipher := NewProviderSecretCipher(directory)
	svc, _ := openProviderSecretSQLite(t, directory)

	encrypted, err := svc.EncryptProviderSecret("account-a", "credential-a", "credential-version-7", "kuaizi-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encrypted, "enc:provider:v2:") || strings.Contains(encrypted, "kuaizi-secret") {
		t.Fatalf("provider ciphertext envelope = %q", encrypted)
	}
	decrypted, err := cipher.Decrypt("account-a", "credential-a", "credential-version-7", encrypted)
	if err != nil || decrypted != "kuaizi-secret" {
		t.Fatalf("provider secret round trip = %q, err=%v", decrypted, err)
	}
	key, err := os.ReadFile(filepath.Join(directory, ".settings-key"))
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != 32 {
		t.Fatalf("settings key length = %d, want 32", len(key))
	}
	info, err := os.Stat(filepath.Join(directory, ".settings-key"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("settings key mode = %o, want owner-only", info.Mode().Perm())
	}
}

func TestProviderSecretRejectsRebindingTamperingAndWrongKey(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, ".settings-key"), []byte(strings.Repeat("k", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	cipher := NewProviderSecretCipher(directory)
	encrypted, err := cipher.Encrypt("account-a", "credential-a", "credential-version-3", "secret")
	if err != nil {
		t.Fatal(err)
	}

	for name, binding := range map[string]struct {
		accountID    string
		credentialID string
		versionID    string
	}{
		"account":            {"account-b", "credential-a", "credential-version-3"},
		"credential":         {"account-a", "credential-b", "credential-version-3"},
		"credential version": {"account-a", "credential-a", "credential-version-4"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := cipher.Decrypt(binding.accountID, binding.credentialID, binding.versionID, encrypted); !errors.Is(err, ErrProviderSecretAuthentication) {
				t.Fatalf("rebound decrypt error = %v", err)
			}
		})
	}

	payload, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(encrypted, providerSecretPrefix))
	if err != nil {
		t.Fatal(err)
	}
	payload[len(payload)-1] ^= 0x01
	tampered := providerSecretPrefix + base64.RawStdEncoding.EncodeToString(payload)
	if _, err := cipher.Decrypt("account-a", "credential-a", "credential-version-3", tampered); !errors.Is(err, ErrProviderSecretAuthentication) {
		t.Fatalf("tampered decrypt error = %v", err)
	}

	wrongDirectory := t.TempDir()
	if err := os.WriteFile(filepath.Join(wrongDirectory, ".settings-key"), []byte(strings.Repeat("x", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewProviderSecretCipher(wrongDirectory).Decrypt("account-a", "credential-a", "credential-version-3", encrypted); !errors.Is(err, ErrProviderSecretAuthentication) {
		t.Fatalf("wrong-key decrypt error = %v", err)
	}
}

func TestProviderSecretRejectsCredentialVersionIDRebinding(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, ".settings-key"), []byte(strings.Repeat("k", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	cipher := NewProviderSecretCipher(directory)
	encrypted, err := cipher.Encrypt("account-a", "credential-a", "credential-version-a", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encrypted, "enc:provider:v2:") {
		t.Fatalf("ciphertext version = %q", encrypted)
	}
	if _, err := cipher.Decrypt("account-a", "credential-a", "credential-version-b", encrypted); !errors.Is(err, ErrProviderSecretAuthentication) {
		t.Fatalf("credential-version rebound decrypt error = %v", err)
	}
}

func TestProviderSecretFailsClosedForMalformedCiphertextAndKey(t *testing.T) {
	for name, ciphertext := range map[string]string{
		"invalid base64": "enc:provider:v2:!",
		"short payload":  "enc:provider:v2:YQ",
		"wrong version":  "enc:provider:v1:YQ",
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, ".settings-key"), []byte(strings.Repeat("k", 32)), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := NewProviderSecretCipher(directory).Decrypt("account", "credential", "credential-version", ciphertext); err == nil {
				t.Fatal("malformed provider ciphertext was accepted")
			}
		})
	}

	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, ".settings-key"), []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewProviderSecretCipher(directory).Encrypt("account", "credential", "credential-version", "secret"); !errors.Is(err, ErrProviderSecretKeyLength) {
		t.Fatalf("short key encrypt error = %v", err)
	}
}

func TestProviderSecretDecryptDoesNotCreateMissingSettingsKey(t *testing.T) {
	directory := t.TempDir()
	cipher := NewProviderSecretCipher(directory)
	if _, err := cipher.Decrypt("account", "credential", "credential-version", "enc:provider:v2:YQ"); !errors.Is(err, ErrProviderSecretKeyMissing) {
		t.Fatalf("missing key decrypt error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, ".settings-key")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("decrypt created or changed missing key: %v", err)
	}
}

func TestProviderSecretEncryptCannotCreateKeyWithoutDatabaseAuthorization(t *testing.T) {
	directory := t.TempDir()
	if _, err := NewProviderSecretCipher(directory).Encrypt("account", "credential", "credential-version", "secret"); !errors.Is(err, ErrProviderSecretKeyCreationNotAuthorized) {
		t.Fatalf("untrusted first encrypt error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, ".settings-key")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("untrusted encrypt created settings key: %v", err)
	}
}

func TestProviderSecretRuntimeRejectsMissingKeyWithoutCreatingReplacement(t *testing.T) {
	directory := t.TempDir()
	svc, repo := openProviderSecretSQLite(t, directory)
	now := time.Now().UTC()
	ciphertext, err := svc.EncryptProviderSecret("account", "credential", "version-1", "secret")
	if err != nil {
		t.Fatal(err)
	}
	createStoredProviderCredentialSecret(t, repo, "account", "credential", "version-1", 1, ciphertext, now)
	keyPath := filepath.Join(directory, ".settings-key")
	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}

	if err := svc.ValidateProviderSecretRuntime(); !errors.Is(err, ErrProviderSecretKeyMissing) {
		t.Fatalf("provider runtime missing-key error = %v", err)
	}
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	t.Setenv("CANVAS_CORS_ORIGINS", "http://localhost:3000")
	if err := svc.ValidateStartupRuntime(); !errors.Is(err, ErrProviderSecretKeyMissing) {
		t.Fatalf("startup runtime missing-key error = %v", err)
	}
	if _, err := svc.EncryptProviderSecret("account", "credential", "version-2", "new-secret"); !errors.Is(err, ErrProviderSecretKeyMissing) {
		t.Fatalf("provider encrypt after key loss error = %v", err)
	}
	if _, err := os.Stat(keyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("provider runtime created replacement key: %v", err)
	}
}

func TestProviderSecretRuntimeWithoutStoredCipherDoesNotCreateKey(t *testing.T) {
	directory := t.TempDir()
	svc, _ := openProviderSecretSQLite(t, directory)
	if err := svc.ValidateProviderSecretRuntime(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, ".settings-key")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty provider runtime created key: %v", err)
	}
}

func TestProviderSecretFirstWriteRejectsOrphanStoredCipherWithoutCreatingKey(t *testing.T) {
	directory := t.TempDir()
	svc, repo := openProviderSecretSQLite(t, directory)
	if err := repo.CreateProviderCredentialVersion(&model.ProviderCredentialVersion{
		ID: "orphan-version", ProviderCredentialID: "missing-credential", Version: 1,
		Status: "pending", KeyCipher: "enc:provider:v2:orphan", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EncryptProviderSecret("new-account", "new-credential", "new-version", "new-secret"); !errors.Is(err, ErrProviderSecretKeyMissing) {
		t.Fatalf("first write with orphan stored cipher error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, ".settings-key")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan stored cipher allowed replacement key creation: %v", err)
	}
}

func TestProviderSecretRuntimeRejectsWrongOrMalformedKey(t *testing.T) {
	for name, replacement := range map[string]struct {
		replacement string
		expected    error
	}{
		"wrong key":     {strings.Repeat("x", 32), ErrProviderSecretAuthentication},
		"malformed key": {"short", ErrProviderSecretKeyLength},
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			svc, repo := openProviderSecretSQLite(t, directory)
			now := time.Now().UTC()
			ciphertext, err := svc.EncryptProviderSecret("account", "credential", "version", "secret")
			if err != nil {
				t.Fatal(err)
			}
			createStoredProviderCredentialSecret(t, repo, "account", "credential", "version", 1, ciphertext, now)
			if err := os.WriteFile(filepath.Join(directory, ".settings-key"), []byte(replacement.replacement), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := svc.ValidateProviderSecretRuntime(); !errors.Is(err, replacement.expected) {
				t.Fatalf("provider runtime key error = %v, want %v", err, replacement.expected)
			}
		})
	}
}

func TestProviderSecretRuntimeValidatesEveryStoredCipherWithDatabaseAAD(t *testing.T) {
	directory := t.TempDir()
	svc, repo := openProviderSecretSQLite(t, directory)
	now := time.Now().UTC()
	first, err := svc.EncryptProviderSecret("account", "credential", "version-1", "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.EncryptProviderSecret("account", "credential", "version-2", "second")
	if err != nil {
		t.Fatal(err)
	}
	createStoredProviderCredentialSecret(t, repo, "account", "credential", "version-1", 1, first, now)
	if err := repo.CreateProviderCredentialVersion(&model.ProviderCredentialVersion{
		ID: "version-2", ProviderCredentialID: "credential", Version: 2, Status: "pending",
		KeyCipher: second, KeyFingerprint: "fingerprint-2", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ValidateProviderSecretRuntime(); err != nil {
		t.Fatalf("valid provider runtime rejected: %v", err)
	}
	if err := repo.Save(&model.ProviderCredentialVersion{
		ID: "version-2", ProviderCredentialID: "credential", Version: 2, Status: "pending",
		KeyCipher: first, KeyFingerprint: "fingerprint-2", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ValidateProviderSecretRuntime(); !errors.Is(err, ErrProviderSecretAuthentication) {
		t.Fatalf("AAD-rebound stored cipher error = %v", err)
	}
}

func TestPostgresProviderCredentialSecretRuntimeRejectsMissingKey(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureProviderIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	repo := repository.New(db)
	svc := New(repo, directory)
	now := time.Now().UTC().Truncate(time.Microsecond)
	ciphertext, err := svc.EncryptProviderSecret("pg-account", "pg-credential", "pg-version", "pg-secret")
	if err != nil {
		t.Fatal(err)
	}
	createStoredProviderCredentialSecret(t, repo, "pg-account", "pg-credential", "pg-version", 1, ciphertext, now)
	if err := os.Remove(filepath.Join(directory, ".settings-key")); err != nil {
		t.Fatal(err)
	}
	if err := svc.ValidateProviderSecretRuntime(); !errors.Is(err, ErrProviderSecretKeyMissing) {
		t.Fatalf("PostgreSQL provider runtime missing-key error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, ".settings-key")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("PostgreSQL provider runtime created replacement key: %v", err)
	}
}

func openProviderSecretSQLite(t *testing.T, directory string) (*Service, *repository.Repository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureProviderIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	return New(repo, directory), repo
}

func createStoredProviderCredentialSecret(t *testing.T, repo *repository.Repository, accountID string, credentialID string, versionID string, version int64, ciphertext string, now time.Time) {
	t.Helper()
	if err := repo.CreateProviderAccount(&model.ProviderAccount{ID: accountID, ProviderKind: "kuaizi", Name: "Kuaizi", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateProviderCredential(&model.ProviderCredential{ID: credentialID, ProviderAccountID: accountID, Family: "seedance", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateProviderCredentialVersion(&model.ProviderCredentialVersion{
		ID: versionID, ProviderCredentialID: credentialID, Version: version, Status: "pending",
		KeyCipher: ciphertext, KeyFingerprint: "fingerprint-" + versionID, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
}
