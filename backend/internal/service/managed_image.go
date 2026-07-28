package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
)

func readManagedImage(header *multipart.FileHeader, maxBytes int64, label string) ([]byte, string, string, error) {
	file, err := header.Open()
	if err != nil {
		return nil, "", "", err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, "", "", fmt.Errorf("读取%s文件失败: %w", label, err)
	}
	if len(content) == 0 || int64(len(content)) > maxBytes {
		return nil, "", "", BadAuthRequest(fmt.Sprintf("%s文件大小必须在 %s 以内", label, formatByteLimit(maxBytes)))
	}
	mimeType := strings.TrimSpace(strings.Split(http.DetectContentType(content), ";")[0])
	switch mimeType {
	case "image/png":
		return content, mimeType, ".png", nil
	case "image/jpeg":
		return content, mimeType, ".jpg", nil
	case "image/webp":
		return content, mimeType, ".webp", nil
	default:
		return nil, "", "", BadAuthRequest(label + "仅支持 PNG、JPG 或 WebP 格式")
	}
}

func managedImageFileName(content []byte, extension string) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:]) + extension
}

func writeFileAtomically(directory string, pattern string, finalPath string, content []byte) error {
	tempFile, err := os.CreateTemp(directory, pattern)
	if err != nil {
		return fmt.Errorf("创建图片临时文件失败: %w", err)
	}
	tempPath := tempFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	if err := tempFile.Chmod(0o640); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("设置图片文件权限失败: %w", err)
	}
	if _, err := tempFile.Write(content); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("写入图片文件失败: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("同步图片文件失败: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("关闭图片文件失败: %w", err)
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return fmt.Errorf("保存图片文件失败: %w", err)
	}
	cleanup = false
	return nil
}

func formatByteLimit(value int64) string {
	if value%(1<<20) == 0 {
		return fmt.Sprintf("%dMB", value/(1<<20))
	}
	return fmt.Sprintf("%dKB", value/(1<<10))
}
