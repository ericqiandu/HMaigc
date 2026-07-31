package opscontroller

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"infinite-canvas/backend/internal/opsprotocol"
)

type ReleaseState struct {
	CurrentVersion  string
	PreviousVersion string
	RollbackBackup  string
	UpdatedAt       time.Time
}

func ReadReleaseState(path string) (ReleaseState, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ReleaseState{}, nil
		}
		return ReleaseState{}, err
	}
	defer file.Close()
	values := make(map[string]string)
	scanner := bufio.NewScanner(io.LimitReader(file, 64<<10))
	for scanner.Scan() {
		line := scanner.Text()
		key, value, found := strings.Cut(line, "=")
		if found {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return ReleaseState{}, err
	}
	state := ReleaseState{
		CurrentVersion: values["CURRENT_VERSION"], PreviousVersion: values["PREVIOUS_VERSION"],
		RollbackBackup: values["ROLLBACK_BACKUP"],
	}
	if value := values["UPDATED_AT"]; value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return ReleaseState{}, fmt.Errorf("发布状态更新时间无效: %w", err)
		}
		state.UpdatedAt = parsed
	}
	if state.CurrentVersion != "" {
		if err := opsprotocol.ValidateReleaseVersion(state.CurrentVersion); err != nil {
			return ReleaseState{}, fmt.Errorf("当前发布版本状态无效: %w", err)
		}
	}
	if state.PreviousVersion != "" {
		if err := opsprotocol.ValidateReleaseVersion(state.PreviousVersion); err != nil {
			return ReleaseState{}, fmt.Errorf("上一发布版本状态无效: %w", err)
		}
	}
	return state, nil
}

func ScanBackups(root string, limit int) ([]opsprotocol.Backup, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []opsprotocol.Backup{}, nil
		}
		return nil, err
	}
	backups := make([]opsprotocol.Backup, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasSuffix(entry.Name(), ".tmp") {
			continue
		}
		backup, err := inspectBackup(filepath.Join(root, entry.Name()))
		if err != nil {
			backup = opsprotocol.Backup{
				Name: entry.Name(), Path: filepath.Join(root, entry.Name()),
				ChecksumStatus: "invalid", ValidationError: err.Error(),
			}
		}
		backups = append(backups, backup)
	}
	sort.Slice(backups, func(left int, right int) bool {
		return backups[left].Name > backups[right].Name
	})
	if len(backups) > limit {
		backups = backups[:limit]
	}
	return backups, nil
}

func inspectRollbackReadiness(root string, state ReleaseState) (bool, string) {
	if state.PreviousVersion == "" || state.RollbackBackup == "" {
		return false, "没有升级前恢复点"
	}
	inside, err := pathInsideRoot(root, state.RollbackBackup)
	if err != nil {
		return false, "恢复点路径校验失败: " + err.Error()
	}
	if !inside {
		return false, "恢复点不在受控备份目录内"
	}
	backup, err := inspectBackup(state.RollbackBackup)
	if err != nil {
		return false, "恢复点完整性校验失败: " + err.Error()
	}
	if backup.Version != state.PreviousVersion {
		return false, "恢复点版本与上一发布版本不一致"
	}
	return true, "恢复点已通过完整性校验"
}

func pathInsideRoot(root string, candidate string) (bool, error) {
	rootPath, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	candidatePath, err := filepath.Abs(candidate)
	if err != nil {
		return false, err
	}
	relative, err := filepath.Rel(rootPath, candidatePath)
	if err != nil {
		return false, err
	}
	if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false, nil
	}
	info, err := os.Lstat(candidatePath)
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, errors.New("恢复点不是受控目录")
	}
	return true, nil
}

func inspectBackup(path string) (opsprotocol.Backup, error) {
	manifest, err := readKeyValueFile(filepath.Join(path, "manifest.env"), 64<<10)
	if err != nil {
		return opsprotocol.Backup{}, fmt.Errorf("读取备份清单失败: %w", err)
	}
	version := manifest["VERSION"]
	if err := opsprotocol.ValidateReleaseVersion(version); err != nil {
		return opsprotocol.Backup{}, fmt.Errorf("备份版本无效: %w", err)
	}
	createdAt, err := time.Parse(time.RFC3339, manifest["CREATED_AT"])
	if err != nil {
		return opsprotocol.Backup{}, fmt.Errorf("备份创建时间无效: %w", err)
	}
	checksums, err := readChecksumFile(filepath.Join(path, "SHA256SUMS"))
	if err != nil {
		return opsprotocol.Backup{}, err
	}
	required := []string{"postgres.dump", "backend-data.tgz", "manifest.env"}
	var sizeBytes int64
	for _, name := range required {
		expected, exists := checksums[name]
		if !exists {
			return opsprotocol.Backup{}, fmt.Errorf("校验清单缺少 %s", name)
		}
		filePath := filepath.Join(path, name)
		actual, size, err := fileSHA256(filePath)
		if err != nil {
			return opsprotocol.Backup{}, err
		}
		if actual != expected {
			return opsprotocol.Backup{}, fmt.Errorf("%s 校验和不一致", name)
		}
		sizeBytes += size
	}
	return opsprotocol.Backup{
		Name: filepath.Base(path), Path: path, Version: version, CreatedAt: createdAt,
		SizeBytes: sizeBytes, ChecksumStatus: "verified",
	}, nil
}

func readKeyValueFile(path string, limit int64) (map[string]string, error) {
	file, err := openRegularFile(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	values := make(map[string]string)
	scanner := bufio.NewScanner(io.LimitReader(file, limit))
	for scanner.Scan() {
		key, value, found := strings.Cut(scanner.Text(), "=")
		if found {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return values, scanner.Err()
}

func readChecksumFile(path string) (map[string]string, error) {
	file, err := openRegularFile(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	result := make(map[string]string)
	scanner := bufio.NewScanner(io.LimitReader(file, 64<<10))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || len(fields[0]) != sha256.Size*2 {
			return nil, errors.New("备份校验清单格式无效")
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			return nil, errors.New("备份校验值格式无效")
		}
		name := strings.TrimPrefix(fields[1], "*")
		if filepath.Base(name) != name {
			return nil, errors.New("备份校验文件名无效")
		}
		result[name] = fields[0]
	}
	return result, scanner.Err()
}

func openRegularFile(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("文件不是受控普通文件: %s", filepath.Base(path))
	}
	return os.Open(path)
}

func fileSHA256(path string) (string, int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", 0, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() <= 0 {
		return "", 0, fmt.Errorf("备份文件无效: %s", filepath.Base(path))
	}
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), info.Size(), nil
}
