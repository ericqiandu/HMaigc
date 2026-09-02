package opsstate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"infinite-canvas/backend/internal/opsprotocol"
)

const maximumFactSize = 4 << 20

var (
	ErrInvalidOperationID      = errors.New("invalid operation id")
	ErrImmutableFactExists     = errors.New("immutable fact already exists")
	ErrCorruptFact             = errors.New("corrupt operation fact")
	ErrInvalidCommandSignature = errors.New("invalid command signature")
	ErrCommandExpired          = errors.New("command expired")
	ErrCommandMismatch         = errors.New("command does not match operation")
)

type durableFact interface {
	opsprotocol.OperationRequestFile |
		opsprotocol.SignedCommandFile |
		opsprotocol.RunnerLease |
		opsprotocol.RunnerHeartbeat |
		opsprotocol.OperationCheckpoint |
		opsprotocol.OperationResult |
		opsprotocol.OperationEvent
}

func marshalFact[T durableFact](value T) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal operation fact: %w", err)
	}
	return append(encoded, '\n'), nil
}

func writeBytesAtomic(path string, mode fs.FileMode, encoded []byte) error {
	directory := filepath.Dir(path)
	if err := ensurePrivateDirectory(directory); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".hmaigc-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary operation fact: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set temporary operation fact permissions: %w", err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary operation fact: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary operation fact: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary operation fact: %w", err)
	}
	if err := replaceFile(temporaryPath, path); err != nil {
		return fmt.Errorf("replace operation fact: %w", err)
	}
	if err := syncDirectory(directory); err != nil {
		return fmt.Errorf("sync operation fact directory: %w", err)
	}
	return nil
}

func createBytesImmutable(path string, mode fs.FileMode, encoded []byte, identicalIsSuccess bool) error {
	directory := filepath.Dir(path)
	if err := ensurePrivateDirectory(directory); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".hmaigc-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary immutable operation fact: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set immutable operation fact permissions: %w", err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write immutable operation fact: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync immutable operation fact: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close immutable operation fact: %w", err)
	}
	if err := os.Link(temporaryPath, path); errors.Is(err, fs.ErrExist) {
		if identicalIsSuccess {
			existing, readErr := readBytes(path)
			if readErr != nil {
				return readErr
			}
			if bytes.Equal(existing, encoded) {
				return nil
			}
		}
		return ErrImmutableFactExists
	} else if err != nil {
		return fmt.Errorf("publish immutable operation fact: %w", err)
	}
	if err := syncDirectory(directory); err != nil {
		return fmt.Errorf("sync immutable operation fact directory: %w", err)
	}
	return nil
}

func readStrictJSON[T durableFact](path string) (T, error) {
	var destination T
	encoded, err := readBytes(path)
	if err != nil {
		return destination, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&destination); err != nil {
		return destination, fmt.Errorf("%w: decode %s: %v", ErrCorruptFact, filepath.Base(path), err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return destination, fmt.Errorf("%w: decode %s: %v", ErrCorruptFact, filepath.Base(path), err)
	}
	return destination, nil
}

func readBytes(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s is not a regular file", ErrCorruptFact, filepath.Base(path))
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	encoded, err := io.ReadAll(io.LimitReader(file, maximumFactSize+1))
	if err != nil {
		return nil, fmt.Errorf("read operation fact: %w", err)
	}
	if len(encoded) > maximumFactSize {
		return nil, fmt.Errorf("%w: %s exceeds %d bytes", ErrCorruptFact, filepath.Base(path), maximumFactSize)
	}
	return encoded, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing json.RawMessage
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values")
	}
	return err
}

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create operation fact directory: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect operation fact directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("operation fact directory is not a real directory: %s", path)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("set operation fact directory permissions: %w", err)
	}
	return nil
}

// Journal 根目录同时承载业务后端只读访问的控制 socket 和共享密钥；
// 这里只验证目录边界，不能覆盖控制器为共享组设置的权限。
func ensureJournalRootDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create operations state root: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect operations state root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("operations state root is not a real directory: %s", path)
	}
	if info.Mode().Perm() == 0o750 {
		return nil
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("set operations state root permissions: %w", err)
	}
	return nil
}
