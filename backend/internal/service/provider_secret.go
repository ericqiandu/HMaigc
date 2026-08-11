package service

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const providerSecretPrefix = "enc:provider:v1:"

var (
	ErrProviderSecretAuthentication = errors.New("provider secret authentication failed")
	ErrProviderSecretKeyMissing     = errors.New("provider secret key file is missing")
	ErrProviderSecretKeyLength      = errors.New("provider secret key length is invalid")
)

type ProviderSecretCipher struct {
	dataDir string
}

func NewProviderSecretCipher(dataDir string) *ProviderSecretCipher {
	return &ProviderSecretCipher{dataDir: dataDir}
}

func (c *ProviderSecretCipher) Encrypt(accountID string, credentialID string, version int64, plaintext string) (string, error) {
	if strings.TrimSpace(accountID) == "" || strings.TrimSpace(credentialID) == "" || version <= 0 {
		return "", errors.New("provider secret AAD identity is invalid")
	}
	if plaintext == "" {
		return "", errors.New("provider secret plaintext is empty")
	}
	key, err := c.encryptionKey(true)
	if err != nil {
		return "", err
	}
	gcm, err := providerGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, nonce, []byte(plaintext), providerSecretAAD(accountID, credentialID, version))
	payload := append(nonce, sealed...)
	return providerSecretPrefix + base64.RawStdEncoding.EncodeToString(payload), nil
}

func (c *ProviderSecretCipher) Decrypt(accountID string, credentialID string, version int64, ciphertext string) (string, error) {
	if strings.TrimSpace(accountID) == "" || strings.TrimSpace(credentialID) == "" || version <= 0 {
		return "", errors.New("provider secret AAD identity is invalid")
	}
	if !strings.HasPrefix(ciphertext, providerSecretPrefix) {
		return "", errors.New("provider secret ciphertext version is invalid")
	}
	// 已有密文时密钥文件是不可替代的持久化根；缺失必须失败，禁止生成新 key 伪装恢复。
	key, err := c.encryptionKey(false)
	if err != nil {
		return "", err
	}
	payload, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(ciphertext, providerSecretPrefix))
	if err != nil {
		return "", errors.New("provider secret ciphertext format is invalid")
	}
	gcm, err := providerGCM(key)
	if err != nil {
		return "", err
	}
	if len(payload) < gcm.NonceSize()+gcm.Overhead() {
		return "", errors.New("provider secret ciphertext length is invalid")
	}
	plaintext, err := gcm.Open(nil, payload[:gcm.NonceSize()], payload[gcm.NonceSize():], providerSecretAAD(accountID, credentialID, version))
	if err != nil {
		return "", ErrProviderSecretAuthentication
	}
	return string(plaintext), nil
}

func providerGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func providerSecretAAD(accountID string, credentialID string, version int64) []byte {
	var buffer bytes.Buffer
	writeProviderAADString(&buffer, "provider:v1")
	writeProviderAADString(&buffer, accountID)
	writeProviderAADString(&buffer, credentialID)
	_ = binary.Write(&buffer, binary.BigEndian, version)
	return buffer.Bytes()
}

func writeProviderAADString(buffer *bytes.Buffer, value string) {
	_ = binary.Write(buffer, binary.BigEndian, uint32(len(value)))
	_, _ = buffer.WriteString(value)
}

func (c *ProviderSecretCipher) encryptionKey(create bool) ([]byte, error) {
	path := filepath.Join(c.dataDir, ".settings-key")
	key, err := os.ReadFile(path)
	if err == nil {
		if len(key) != 32 {
			return nil, ErrProviderSecretKeyLength
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read provider secret key: %w", err)
	}
	if !create {
		return nil, ErrProviderSecretKeyMissing
	}
	if err := os.MkdirAll(c.dataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create provider secret key directory: %w", err)
	}
	key = make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		stored, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("read concurrently created provider secret key: %w", readErr)
		}
		if len(stored) != 32 {
			return nil, ErrProviderSecretKeyLength
		}
		return stored, nil
	}
	if err != nil {
		return nil, fmt.Errorf("create provider secret key: %w", err)
	}
	written, writeErr := file.Write(key)
	closeErr := file.Close()
	if writeErr != nil || written != len(key) || closeErr != nil {
		_ = os.Remove(path)
		if writeErr != nil {
			return nil, fmt.Errorf("write provider secret key: %w", writeErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close provider secret key: %w", closeErr)
		}
		return nil, io.ErrShortWrite
	}
	return key, nil
}
