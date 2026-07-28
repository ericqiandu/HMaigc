package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

const (
	siteSettingKey      = "site"
	siteLogoMaxBytes    = 2 << 20
	siteAgreementMaxLen = 50_000
	siteRecordNoMaxLen  = 100
	siteRecordURLMaxLen = 500
)

var ErrSiteLogoNotConfigured = errors.New("站点 Logo 尚未配置")

type SiteSettingRequest struct {
	SiteName                         string `json:"siteName"`
	FooterCopyright                  string `json:"footerCopyright"`
	ICPRegistrationNumber            string `json:"icpRegistrationNumber"`
	ICPRegistrationURL               string `json:"icpRegistrationUrl"`
	PublicSecurityRegistrationNumber string `json:"publicSecurityRegistrationNumber"`
	PublicSecurityRegistrationURL    string `json:"publicSecurityRegistrationUrl"`
	UserAgreement                    string `json:"userAgreement"`
	PrivacyPolicy                    string `json:"privacyPolicy"`
}

type PublicSiteSetting struct {
	SiteName                         string `json:"siteName"`
	LogoURL                          string `json:"logoUrl"`
	FooterCopyright                  string `json:"footerCopyright"`
	ICPRegistrationNumber            string `json:"icpRegistrationNumber"`
	ICPRegistrationURL               string `json:"icpRegistrationUrl"`
	PublicSecurityRegistrationNumber string `json:"publicSecurityRegistrationNumber"`
	PublicSecurityRegistrationURL    string `json:"publicSecurityRegistrationUrl"`
	UserAgreement                    string `json:"userAgreement"`
	PrivacyPolicy                    string `json:"privacyPolicy"`
	UpdatedBy                        string `json:"updatedBy"`
	CreatedAt                        string `json:"createdAt"`
	UpdatedAt                        string `json:"updatedAt"`
}

type siteSettingValue struct {
	SiteName                         string `json:"siteName"`
	LogoFile                         string `json:"logoFile"`
	LogoMimeType                     string `json:"logoMimeType"`
	FooterCopyright                  string `json:"footerCopyright"`
	ICPRegistrationNumber            string `json:"icpRegistrationNumber"`
	ICPRegistrationURL               string `json:"icpRegistrationUrl"`
	PublicSecurityRegistrationNumber string `json:"publicSecurityRegistrationNumber"`
	PublicSecurityRegistrationURL    string `json:"publicSecurityRegistrationUrl"`
	UserAgreement                    string `json:"userAgreement"`
	PrivacyPolicy                    string `json:"privacyPolicy"`
}

func (s *Service) PublicSiteSetting() (*PublicSiteSetting, error) {
	setting, value, err := s.readSiteSetting()
	if err != nil {
		return nil, err
	}
	result := publicSiteSetting(setting, value)
	return &result, nil
}

func (s *Service) AdminSiteSetting(actor *model.User) (*PublicSiteSetting, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	return s.PublicSiteSetting()
}

func (s *Service) UpdateSiteSetting(actor *model.User, req SiteSettingRequest) (*PublicSiteSetting, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	s.siteSettingMu.Lock()
	defer s.siteSettingMu.Unlock()
	currentSetting, current, err := s.readSiteSetting()
	if err != nil {
		return nil, err
	}
	next := siteSettingValue{
		SiteName:                         strings.TrimSpace(req.SiteName),
		LogoFile:                         current.LogoFile,
		LogoMimeType:                     current.LogoMimeType,
		FooterCopyright:                  strings.TrimSpace(req.FooterCopyright),
		ICPRegistrationNumber:            strings.TrimSpace(req.ICPRegistrationNumber),
		ICPRegistrationURL:               strings.TrimSpace(req.ICPRegistrationURL),
		PublicSecurityRegistrationNumber: strings.TrimSpace(req.PublicSecurityRegistrationNumber),
		PublicSecurityRegistrationURL:    strings.TrimSpace(req.PublicSecurityRegistrationURL),
		UserAgreement:                    strings.TrimSpace(req.UserAgreement),
		PrivacyPolicy:                    strings.TrimSpace(req.PrivacyPolicy),
	}
	if err := validateSiteSetting(next); err != nil {
		return nil, BadAuthRequest(err.Error())
	}
	setting, err := s.saveSiteSetting(actor, currentSetting, next)
	if err != nil {
		return nil, err
	}
	result := publicSiteSetting(setting, next)
	if err := s.appendAdminAudit(actor, "site_setting.update", "system_setting", siteSettingKey, "更新站点基础信息、备案与法律内容", result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Service) UpdateSiteLogo(actor *model.User, header *multipart.FileHeader) (*PublicSiteSetting, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	s.siteSettingMu.Lock()
	defer s.siteSettingMu.Unlock()
	if header == nil {
		return nil, BadAuthRequest("请选择要上传的 Logo")
	}
	if header.Size <= 0 || header.Size > siteLogoMaxBytes {
		return nil, BadAuthRequest("Logo 文件大小必须在 2MB 以内")
	}
	content, mimeType, extension, err := readSiteLogo(header)
	if err != nil {
		return nil, err
	}
	currentSetting, current, err := s.readSiteSetting()
	if err != nil {
		return nil, err
	}
	logoDir := s.siteLogoDir()
	if err := os.MkdirAll(logoDir, 0o750); err != nil {
		return nil, fmt.Errorf("创建站点 Logo 目录失败: %w", err)
	}
	digest := sha256.Sum256(content)
	fileName := hex.EncodeToString(digest[:]) + extension
	finalPath := filepath.Join(logoDir, fileName)
	if _, err := os.Stat(finalPath); errors.Is(err, os.ErrNotExist) {
		if err := writeSiteLogoAtomically(logoDir, finalPath, content); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, fmt.Errorf("检查站点 Logo 文件失败: %w", err)
	}
	next := current
	next.LogoFile = fileName
	next.LogoMimeType = mimeType
	setting, err := s.saveSiteSetting(actor, currentSetting, next)
	if err != nil {
		if current.LogoFile != fileName {
			_ = os.Remove(finalPath)
		}
		return nil, err
	}
	if current.LogoFile != "" && current.LogoFile != fileName {
		_ = os.Remove(filepath.Join(logoDir, filepath.Base(current.LogoFile)))
	}
	result := publicSiteSetting(setting, next)
	if err := s.appendAdminAudit(actor, "site_setting.logo.update", "system_setting", siteSettingKey, "更新站点 Logo", result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Service) RemoveSiteLogo(actor *model.User) (*PublicSiteSetting, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	s.siteSettingMu.Lock()
	defer s.siteSettingMu.Unlock()
	currentSetting, current, err := s.readSiteSetting()
	if err != nil {
		return nil, err
	}
	previousFile := current.LogoFile
	next := current
	next.LogoFile = ""
	next.LogoMimeType = ""
	setting, err := s.saveSiteSetting(actor, currentSetting, next)
	if err != nil {
		return nil, err
	}
	if previousFile != "" {
		_ = os.Remove(filepath.Join(s.siteLogoDir(), filepath.Base(previousFile)))
	}
	result := publicSiteSetting(setting, next)
	if err := s.appendAdminAudit(actor, "site_setting.logo.remove", "system_setting", siteSettingKey, "移除站点自定义 Logo", result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Service) SiteLogoFile() (string, string, time.Time, error) {
	setting, value, err := s.readSiteSetting()
	if err != nil {
		return "", "", time.Time{}, err
	}
	if setting == nil || value.LogoFile == "" {
		return "", "", time.Time{}, ErrSiteLogoNotConfigured
	}
	fileName := filepath.Base(value.LogoFile)
	if fileName != value.LogoFile {
		return "", "", time.Time{}, errors.New("站点 Logo 文件配置无效")
	}
	filePath := filepath.Join(s.siteLogoDir(), fileName)
	info, err := os.Stat(filePath)
	if err != nil {
		return "", "", time.Time{}, err
	}
	if !info.Mode().IsRegular() {
		return "", "", time.Time{}, errors.New("站点 Logo 文件无效")
	}
	return filePath, value.LogoMimeType, info.ModTime(), nil
}

func (s *Service) readSiteSetting() (*model.SystemSetting, siteSettingValue, error) {
	setting, err := s.repo.SystemSetting(siteSettingKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, defaultSiteSetting(), nil
	}
	if err != nil {
		return nil, siteSettingValue{}, err
	}
	var value siteSettingValue
	if err := json.Unmarshal([]byte(setting.ValueJSON), &value); err != nil {
		return nil, siteSettingValue{}, fmt.Errorf("站点配置数据损坏: %w", err)
	}
	if err := validateSiteSetting(value); err != nil {
		return nil, siteSettingValue{}, fmt.Errorf("站点配置无效: %w", err)
	}
	return setting, value, nil
}

func (s *Service) saveSiteSetting(actor *model.User, current *model.SystemSetting, value siteSettingValue) (*model.SystemSetting, error) {
	if err := validateSiteSetting(value); err != nil {
		return nil, BadAuthRequest(err.Error())
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	setting := &model.SystemSetting{Key: siteSettingKey, ValueJSON: string(encoded), UpdatedBy: actor.ID}
	if current != nil {
		setting.CreatedAt = current.CreatedAt
	}
	if err := s.repo.SaveSystemSetting(setting); err != nil {
		return nil, err
	}
	return setting, nil
}

func defaultSiteSetting() siteSettingValue {
	return siteSettingValue{
		SiteName:        "HMaigc",
		FooterCopyright: fmt.Sprintf("© %d HMaigc. 保留所有权利。", time.Now().Year()),
	}
}

func validateSiteSetting(value siteSettingValue) error {
	nameLength := len([]rune(strings.TrimSpace(value.SiteName)))
	if nameLength < 1 || nameLength > 40 {
		return errors.New("站点名称必须是 1-40 个字符")
	}
	if len([]rune(value.FooterCopyright)) > 200 {
		return errors.New("底部版权不能超过 200 个字符")
	}
	if len([]rune(value.ICPRegistrationNumber)) > siteRecordNoMaxLen {
		return fmt.Errorf("ICP备案号不能超过 %d 个字符", siteRecordNoMaxLen)
	}
	if value.ICPRegistrationURL != "" && value.ICPRegistrationNumber == "" {
		return errors.New("填写ICP备案链接时必须同时填写ICP备案号")
	}
	if err := validateSiteRecordURL("ICP备案链接", value.ICPRegistrationURL); err != nil {
		return err
	}
	if len([]rune(value.PublicSecurityRegistrationNumber)) > siteRecordNoMaxLen {
		return fmt.Errorf("公安备案号不能超过 %d 个字符", siteRecordNoMaxLen)
	}
	if value.PublicSecurityRegistrationURL != "" && value.PublicSecurityRegistrationNumber == "" {
		return errors.New("填写公安备案链接时必须同时填写公安备案号")
	}
	if err := validateSiteRecordURL("公安备案链接", value.PublicSecurityRegistrationURL); err != nil {
		return err
	}
	if len([]rune(value.UserAgreement)) > siteAgreementMaxLen {
		return fmt.Errorf("用户协议不能超过 %d 个字符", siteAgreementMaxLen)
	}
	if len([]rune(value.PrivacyPolicy)) > siteAgreementMaxLen {
		return fmt.Errorf("隐私政策不能超过 %d 个字符", siteAgreementMaxLen)
	}
	if value.LogoFile != "" && filepath.Base(value.LogoFile) != value.LogoFile {
		return errors.New("站点 Logo 文件配置无效")
	}
	if value.LogoFile == "" && value.LogoMimeType != "" {
		return errors.New("站点 Logo 文件与类型不一致")
	}
	return nil
}

func validateSiteRecordURL(label string, rawURL string) error {
	if rawURL == "" {
		return nil
	}
	if len([]rune(rawURL)) > siteRecordURLMaxLen {
		return fmt.Errorf("%s不能超过 %d 个字符", label, siteRecordURLMaxLen)
	}
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return fmt.Errorf("%s必须是有效的 HTTP 或 HTTPS 地址", label)
	}
	return nil
}

func publicSiteSetting(setting *model.SystemSetting, value siteSettingValue) PublicSiteSetting {
	result := PublicSiteSetting{
		SiteName:                         value.SiteName,
		FooterCopyright:                  value.FooterCopyright,
		ICPRegistrationNumber:            value.ICPRegistrationNumber,
		ICPRegistrationURL:               value.ICPRegistrationURL,
		PublicSecurityRegistrationNumber: value.PublicSecurityRegistrationNumber,
		PublicSecurityRegistrationURL:    value.PublicSecurityRegistrationURL,
		UserAgreement:                    value.UserAgreement,
		PrivacyPolicy:                    value.PrivacyPolicy,
	}
	if setting != nil {
		result.UpdatedBy = setting.UpdatedBy
		result.CreatedAt = setting.CreatedAt.UTC().Format(time.RFC3339Nano)
		result.UpdatedAt = setting.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	if value.LogoFile != "" {
		version := "configured"
		if setting != nil && !setting.UpdatedAt.IsZero() {
			version = fmt.Sprintf("%d", setting.UpdatedAt.UnixNano())
		}
		result.LogoURL = "/api/public/site/logo?v=" + version
	}
	return result
}

func readSiteLogo(header *multipart.FileHeader) ([]byte, string, string, error) {
	file, err := header.Open()
	if err != nil {
		return nil, "", "", err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, siteLogoMaxBytes+1))
	if err != nil {
		return nil, "", "", fmt.Errorf("读取 Logo 文件失败: %w", err)
	}
	if len(content) == 0 || len(content) > siteLogoMaxBytes {
		return nil, "", "", BadAuthRequest("Logo 文件大小必须在 2MB 以内")
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
		return nil, "", "", BadAuthRequest("Logo 仅支持 PNG、JPG 或 WebP 格式")
	}
}

func writeSiteLogoAtomically(directory string, finalPath string, content []byte) error {
	tempFile, err := os.CreateTemp(directory, ".site-logo-*")
	if err != nil {
		return fmt.Errorf("创建 Logo 临时文件失败: %w", err)
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
		return fmt.Errorf("设置 Logo 文件权限失败: %w", err)
	}
	if _, err := tempFile.Write(content); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("写入 Logo 文件失败: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("同步 Logo 文件失败: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("关闭 Logo 文件失败: %w", err)
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return fmt.Errorf("保存 Logo 文件失败: %w", err)
	}
	cleanup = false
	return nil
}

func (s *Service) siteLogoDir() string {
	return filepath.Join(s.dataDir, "site-assets")
}
