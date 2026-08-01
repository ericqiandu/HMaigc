package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"infinite-canvas/backend/internal/model"

	"golang.org/x/net/html"
	"gorm.io/gorm"
)

const (
	siteSettingKey      = "site"
	siteLogoMaxBytes    = 2 << 20
	siteAgreementMaxLen = 50_000
	siteLegalHTMLMaxLen = 256 << 10
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
}

type LegalContentSettingRequest struct {
	UserAgreement string `json:"userAgreement"`
	PrivacyPolicy string `json:"privacyPolicy"`
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
		UserAgreement:                    current.UserAgreement,
		PrivacyPolicy:                    current.PrivacyPolicy,
	}
	if err := validateSiteSetting(next); err != nil {
		return nil, BadAuthRequest(err.Error())
	}
	setting, err := s.saveSiteSetting(actor, currentSetting, next)
	if err != nil {
		return nil, err
	}
	result := publicSiteSetting(setting, next)
	if err := s.appendAdminAudit(actor, "site_setting.update", "system_setting", siteSettingKey, "更新站点基础信息与备案", result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Service) UpdateLegalContentSetting(actor *model.User, req LegalContentSettingRequest) (*PublicSiteSetting, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	s.siteSettingMu.Lock()
	defer s.siteSettingMu.Unlock()
	currentSetting, current, err := s.readSiteSetting()
	if err != nil {
		return nil, err
	}
	next := current
	next.UserAgreement = strings.TrimSpace(req.UserAgreement)
	next.PrivacyPolicy = strings.TrimSpace(req.PrivacyPolicy)
	if err := validateSiteSetting(next); err != nil {
		return nil, BadAuthRequest(err.Error())
	}
	setting, err := s.saveSiteSetting(actor, currentSetting, next)
	if err != nil {
		return nil, err
	}
	result := publicSiteSetting(setting, next)
	if err := s.appendAdminAudit(actor, "site_setting.legal.update", "system_setting", siteSettingKey, "更新用户协议与隐私政策", result); err != nil {
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
	content, mimeType, extension, err := readManagedImage(header, siteLogoMaxBytes, "Logo")
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
	fileName := managedImageFileName(content, extension)
	finalPath := filepath.Join(logoDir, fileName)
	if _, err := os.Stat(finalPath); errors.Is(err, os.ErrNotExist) {
		if err := writeFileAtomically(logoDir, ".site-logo-*", finalPath, content); err != nil {
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
	if err := validateLegalRichText("用户协议", value.UserAgreement); err != nil {
		return err
	}
	if err := validateLegalRichText("隐私政策", value.PrivacyPolicy); err != nil {
		return err
	}
	if value.LogoFile != "" && filepath.Base(value.LogoFile) != value.LogoFile {
		return errors.New("站点 Logo 文件配置无效")
	}
	if value.LogoFile == "" && value.LogoMimeType != "" {
		return errors.New("站点 Logo 文件与类型不一致")
	}
	return nil
}

var legalRichTextTags = map[string]struct{}{
	"blockquote": {},
	"br":         {},
	"code":       {},
	"em":         {},
	"h1":         {},
	"h2":         {},
	"h3":         {},
	"hr":         {},
	"li":         {},
	"ol":         {},
	"p":          {},
	"pre":        {},
	"s":          {},
	"strong":     {},
	"ul":         {},
}

func validateLegalRichText(label string, value string) error {
	if value == "" {
		return nil
	}
	if len(value) > siteLegalHTMLMaxLen {
		return fmt.Errorf("%s内容不能超过 %d KiB", label, siteLegalHTMLMaxLen>>10)
	}
	tokenizer := html.NewTokenizer(strings.NewReader(value))
	visibleCharacters := 0
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			if errors.Is(tokenizer.Err(), io.EOF) {
				if visibleCharacters > siteAgreementMaxLen {
					return fmt.Errorf("%s不能超过 %d 个字符", label, siteAgreementMaxLen)
				}
				return nil
			}
			return fmt.Errorf("%s内容格式无效: %w", label, tokenizer.Err())
		case html.TextToken:
			visibleCharacters += utf8.RuneCount(tokenizer.Text())
			if visibleCharacters > siteAgreementMaxLen {
				return fmt.Errorf("%s不能超过 %d 个字符", label, siteAgreementMaxLen)
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			if _, allowed := legalRichTextTags[token.Data]; !allowed {
				return fmt.Errorf("%s包含不支持的 HTML 标签 <%s>", label, token.Data)
			}
			if len(token.Attr) > 0 {
				return fmt.Errorf("%s的 HTML 标签 <%s> 不能包含属性", label, token.Data)
			}
		case html.EndTagToken:
			token := tokenizer.Token()
			if _, allowed := legalRichTextTags[token.Data]; !allowed {
				return fmt.Errorf("%s包含不支持的 HTML 标签 </%s>", label, token.Data)
			}
		case html.CommentToken, html.DoctypeToken:
			return fmt.Errorf("%s包含不支持的 HTML 内容", label)
		}
	}
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

func (s *Service) siteLogoDir() string {
	return filepath.Join(s.dataDir, "site-assets")
}
