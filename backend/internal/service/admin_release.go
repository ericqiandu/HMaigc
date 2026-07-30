package service

import (
	"fmt"
	"io"
	"os"
	"strings"

	"infinite-canvas/backend/internal/buildinfo"
	"infinite-canvas/backend/internal/model"
)

const maxReleaseNotesBytes = 1 << 20

type AdminReleaseNotes struct {
	Version   string `json:"version"`
	Changelog string `json:"changelog"`
}

func (s *Service) AdminReleaseNotes(actor *model.User, changelogPath string) (*AdminReleaseNotes, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	path := strings.TrimSpace(changelogPath)
	if path == "" {
		return nil, fmt.Errorf("更新日志文件路径未配置")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开更新日志失败: %w", err)
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, maxReleaseNotesBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取更新日志失败: %w", err)
	}
	if len(content) > maxReleaseNotesBytes {
		return nil, fmt.Errorf("更新日志超过 %d 字节限制", maxReleaseNotesBytes)
	}
	return &AdminReleaseNotes{
		Version:   buildinfo.Version,
		Changelog: string(content),
	}, nil
}
