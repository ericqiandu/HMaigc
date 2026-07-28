package service

import (
	"errors"
	"fmt"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
)

const channelModelIconMaxBytes = 1 << 20

var ErrChannelModelIconNotConfigured = errors.New("模型图标尚未配置")

func (s *Service) UpdateAdminChannelModelIcon(actor *model.User, channelID string, modelID string, header *multipart.FileHeader) (*model.ChannelModel, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if header == nil {
		return nil, BadAuthRequest("请选择要上传的模型图标")
	}
	if header.Size <= 0 || header.Size > channelModelIconMaxBytes {
		return nil, BadAuthRequest("模型图标文件大小必须在 1MB 以内")
	}
	item, err := s.repo.ChannelModelByID(channelID, modelID)
	if err != nil {
		return nil, err
	}
	content, mimeType, extension, err := readManagedImage(header, channelModelIconMaxBytes, "模型图标")
	if err != nil {
		return nil, err
	}
	iconDir := s.channelModelIconDir()
	if err := os.MkdirAll(iconDir, 0o750); err != nil {
		return nil, fmt.Errorf("创建模型图标目录失败: %w", err)
	}
	fileName := item.ID + "-" + managedImageFileName(content, extension)
	finalPath := filepath.Join(iconDir, fileName)
	if _, err := os.Stat(finalPath); errors.Is(err, os.ErrNotExist) {
		if err := writeFileAtomically(iconDir, ".model-icon-*", finalPath, content); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, fmt.Errorf("检查模型图标文件失败: %w", err)
	}
	previousFile := item.IconFile
	item.IconFile = fileName
	item.IconMimeType = mimeType
	audit, err := newAdminAuditEvent(actor, "channel_model.icon.update", "channel_model", item.ID, "更新模型展示图标", map[string]any{
		"channelId": item.ChannelID, "modelKey": item.ModelKey, "iconFile": fileName,
	})
	if err != nil {
		return nil, err
	}
	if err := s.repo.SaveChannelModelPresentation(item, audit); err != nil {
		if previousFile != fileName {
			_ = os.Remove(finalPath)
		}
		return nil, err
	}
	if previousFile != "" && previousFile != fileName {
		_ = os.Remove(filepath.Join(iconDir, filepath.Base(previousFile)))
	}
	saved, err := s.repo.ChannelModelByID(channelID, modelID)
	if err != nil {
		return nil, err
	}
	return normalizeChannelModelForOutput(saved), nil
}

func (s *Service) RemoveAdminChannelModelIcon(actor *model.User, channelID string, modelID string) (*model.ChannelModel, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	item, err := s.repo.ChannelModelByID(channelID, modelID)
	if err != nil {
		return nil, err
	}
	previousFile := item.IconFile
	item.IconFile = ""
	item.IconMimeType = ""
	audit, err := newAdminAuditEvent(actor, "channel_model.icon.remove", "channel_model", item.ID, "移除模型自定义图标", map[string]any{
		"channelId": item.ChannelID, "modelKey": item.ModelKey,
	})
	if err != nil {
		return nil, err
	}
	if err := s.repo.SaveChannelModelPresentation(item, audit); err != nil {
		return nil, err
	}
	if previousFile != "" {
		_ = os.Remove(filepath.Join(s.channelModelIconDir(), filepath.Base(previousFile)))
	}
	saved, err := s.repo.ChannelModelByID(channelID, modelID)
	if err != nil {
		return nil, err
	}
	return normalizeChannelModelForOutput(saved), nil
}

func (s *Service) ChannelModelIconFile(actor *model.User, modelID string) (string, string, time.Time, error) {
	if actor == nil {
		return "", "", time.Time{}, Unauthorized("请先登录")
	}
	item, err := s.repo.ChannelModelByPublicID(modelID)
	if err != nil {
		return "", "", time.Time{}, err
	}
	if !item.Enabled && actor.Role != model.UserRoleAdmin {
		return "", "", time.Time{}, Forbidden("模型当前未启用")
	}
	if item.IconFile == "" || item.IconMimeType == "" {
		return "", "", time.Time{}, ErrChannelModelIconNotConfigured
	}
	fileName := filepath.Base(item.IconFile)
	if fileName != item.IconFile || !strings.HasPrefix(fileName, item.ID+"-") {
		return "", "", time.Time{}, errors.New("模型图标文件配置无效")
	}
	switch item.IconMimeType {
	case "image/png", "image/jpeg", "image/webp":
	default:
		return "", "", time.Time{}, errors.New("模型图标 MIME 类型配置无效")
	}
	filePath := filepath.Join(s.channelModelIconDir(), fileName)
	info, err := os.Stat(filePath)
	if err != nil {
		return "", "", time.Time{}, err
	}
	if !info.Mode().IsRegular() {
		return "", "", time.Time{}, errors.New("模型图标文件无效")
	}
	return filePath, item.IconMimeType, info.ModTime(), nil
}

func normalizeChannelModelForOutput(item *model.ChannelModel) *model.ChannelModel {
	if item.PriceTiers == nil {
		item.PriceTiers = make([]model.ChannelModelPriceTier, 0)
	}
	item.IconURL = channelModelIconURL(*item)
	return item
}

func channelModelIconURL(item model.ChannelModel) string {
	if item.IconFile == "" {
		return ""
	}
	version := item.UpdatedAt.UnixNano()
	return fmt.Sprintf("/api/model-icons/%s?v=%d", item.ID, version)
}

func (s *Service) channelModelIconDir() string {
	return filepath.Join(s.dataDir, "model-icons")
}
