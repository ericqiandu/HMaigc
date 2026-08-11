package service

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProviderSecretRoundTripUsesVersionedAAD(t *testing.T) {
	directory := t.TempDir()
	cipher := NewProviderSecretCipher(directory)

	encrypted, err := cipher.Encrypt("account-a", "credential-a", 7, "kuaizi-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encrypted, "enc:provider:v1:") || strings.Contains(encrypted, "kuaizi-secret") {
		t.Fatalf("provider ciphertext envelope = %q", encrypted)
	}
	decrypted, err := cipher.Decrypt("account-a", "credential-a", 7, encrypted)
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
	cipher := NewProviderSecretCipher(directory)
	encrypted, err := cipher.Encrypt("account-a", "credential-a", 3, "secret")
	if err != nil {
		t.Fatal(err)
	}

	for name, binding := range map[string]struct {
		accountID    string
		credentialID string
		version      int64
	}{
		"account":    {"account-b", "credential-a", 3},
		"credential": {"account-a", "credential-b", 3},
		"version":    {"account-a", "credential-a", 4},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := cipher.Decrypt(binding.accountID, binding.credentialID, binding.version, encrypted); !errors.Is(err, ErrProviderSecretAuthentication) {
				t.Fatalf("rebound decrypt error = %v", err)
			}
		})
	}

	tamperedSuffix := "A"
	if strings.HasSuffix(encrypted, tamperedSuffix) {
		tamperedSuffix = "B"
	}
	tampered := encrypted[:len(encrypted)-1] + tamperedSuffix
	if _, err := cipher.Decrypt("account-a", "credential-a", 3, tampered); !errors.Is(err, ErrProviderSecretAuthentication) {
		t.Fatalf("tampered decrypt error = %v", err)
	}

	wrongDirectory := t.TempDir()
	if err := os.WriteFile(filepath.Join(wrongDirectory, ".settings-key"), []byte(strings.Repeat("x", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewProviderSecretCipher(wrongDirectory).Decrypt("account-a", "credential-a", 3, encrypted); !errors.Is(err, ErrProviderSecretAuthentication) {
		t.Fatalf("wrong-key decrypt error = %v", err)
	}
}

func TestProviderSecretFailsClosedForMalformedCiphertextAndKey(t *testing.T) {
	for name, ciphertext := range map[string]string{
		"invalid base64": "enc:provider:v1:!",
		"short payload":  "enc:provider:v1:YQ",
		"wrong version":  "enc:provider:v2:YQ",
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, ".settings-key"), []byte(strings.Repeat("k", 32)), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := NewProviderSecretCipher(directory).Decrypt("account", "credential", 1, ciphertext); err == nil {
				t.Fatal("malformed provider ciphertext was accepted")
			}
		})
	}

	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, ".settings-key"), []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewProviderSecretCipher(directory).Encrypt("account", "credential", 1, "secret"); !errors.Is(err, ErrProviderSecretKeyLength) {
		t.Fatalf("short key encrypt error = %v", err)
	}
}

func TestProviderSecretDecryptDoesNotCreateMissingSettingsKey(t *testing.T) {
	directory := t.TempDir()
	cipher := NewProviderSecretCipher(directory)
	if _, err := cipher.Decrypt("account", "credential", 1, "enc:provider:v1:YQ"); !errors.Is(err, ErrProviderSecretKeyMissing) {
		t.Fatalf("missing key decrypt error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, ".settings-key")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("decrypt created or changed missing key: %v", err)
	}
}
