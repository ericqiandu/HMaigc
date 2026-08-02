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
	siteSettingKey         = "site"
	siteLogoMaxBytes       = 2 << 20
	siteAgreementMaxLen    = 50_000
	siteLegalHTMLMaxLen    = 256 << 10
	siteRecordNoMaxLen     = 100
	siteRecordURLMaxLen    = 500
	siteBannerLabelMaxLen  = 20
	siteBannerTextMaxLen   = 200
	siteBannerActionMaxLen = 20
)

var ErrSiteLogoNotConfigured = errors.New("站点 Logo 尚未配置")

type SiteSettingRequest struct {
	SiteName                         string `json:"siteName"`
	FooterCopyright                  string `json:"footerCopyright"`
	ICPRegistrationNumber            string `json:"icpRegistrationNumber"`
	ICPRegistrationURL               string `json:"icpRegistrationUrl"`
	PublicSecurityRegistrationNumber string `json:"publicSecurityRegistrationNumber"`
	PublicSecurityRegistrationURL    string `json:"publicSecurityRegistrationUrl"`
	HomeBannerEnabled                bool   `json:"homeBannerEnabled"`
	HomeBannerLabel                  string `json:"homeBannerLabel"`
	HomeBannerText                   string `json:"homeBannerText"`
	HomeBannerPrimaryActionLabel     string `json:"homeBannerPrimaryActionLabel"`
	HomeBannerPrimaryActionURL       string `json:"homeBannerPrimaryActionUrl"`
	HomeBannerSecondaryActionLabel   string `json:"homeBannerSecondaryActionLabel"`
	HomeBannerSecondaryActionURL     string `json:"homeBannerSecondaryActionUrl"`
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
	HomeBannerEnabled                bool   `json:"homeBannerEnabled"`
	HomeBannerLabel                  string `json:"homeBannerLabel"`
	HomeBannerText                   string `json:"homeBannerText"`
	HomeBannerPrimaryActionLabel     string `json:"homeBannerPrimaryActionLabel"`
	HomeBannerPrimaryActionURL       string `json:"homeBannerPrimaryActionUrl"`
	HomeBannerSecondaryActionLabel   string `json:"homeBannerSecondaryActionLabel"`
	HomeBannerSecondaryActionURL     string `json:"homeBannerSecondaryActionUrl"`
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
	HomeBannerEnabled                bool   `json:"homeBannerEnabled"`
	HomeBannerLabel                  string `json:"homeBannerLabel"`
	HomeBannerText                   string `json:"homeBannerText"`
	HomeBannerPrimaryActionLabel     string `json:"homeBannerPrimaryActionLabel"`
	HomeBannerPrimaryActionURL       string `json:"homeBannerPrimaryActionUrl"`
	HomeBannerSecondaryActionLabel   string `json:"homeBannerSecondaryActionLabel"`
	HomeBannerSecondaryActionURL     string `json:"homeBannerSecondaryActionUrl"`
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
		HomeBannerEnabled:                req.HomeBannerEnabled,
		HomeBannerLabel:                  strings.TrimSpace(req.HomeBannerLabel),
		HomeBannerText:                   strings.TrimSpace(req.HomeBannerText),
		HomeBannerPrimaryActionLabel:     strings.TrimSpace(req.HomeBannerPrimaryActionLabel),
		HomeBannerPrimaryActionURL:       strings.TrimSpace(req.HomeBannerPrimaryActionURL),
		HomeBannerSecondaryActionLabel:   strings.TrimSpace(req.HomeBannerSecondaryActionLabel),
		HomeBannerSecondaryActionURL:     strings.TrimSpace(req.HomeBannerSecondaryActionURL),
	}
	if err := validateSiteSetting(next); err != nil {
		return nil, BadAuthRequest(err.Error())
	}
	setting, err := s.saveSiteSetting(actor, currentSetting, next)
	if err != nil {
		return nil, err
	}
	result := publicSiteSetting(setting, next)
	if err := s.appendAdminAudit(actor, "site_setting.update", "system_setting", siteSettingKey, "更新站点品牌、首页横幅与备案", result); err != nil {
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
	value := defaultSiteSetting()
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
		SiteName:          "HMaigc",
		FooterCopyright:   fmt.Sprintf("© %d HMaigc. 保留所有权利。", time.Now().Year()),
		HomeBannerEnabled: true,
		HomeBannerLabel:   "招募中",
		HomeBannerText:    "招增长伙伴：懂冷启动、内容增长或海外增长，欢迎加入 HMaigc。",
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
	if len([]rune(value.HomeBannerLabel)) > siteBannerLabelMaxLen {
		return fmt.Errorf("首页横幅状态标签不能超过 %d 个字符", siteBannerLabelMaxLen)
	}
	if len([]rune(value.HomeBannerText)) > siteBannerTextMaxLen {
		return fmt.Errorf("首页横幅文案不能超过 %d 个字符", siteBannerTextMaxLen)
	}
	if value.HomeBannerEnabled && value.HomeBannerText == "" {
		return errors.New("启用首页横幅时必须填写展示文案")
	}
	if err := validateSiteBannerAction("首页横幅主按钮", value.HomeBannerPrimaryActionLabel, value.HomeBannerPrimaryActionURL); err != nil {
		return err
	}
	if err := validateSiteBannerAction("首页横幅次按钮", value.HomeBannerSecondaryActionLabel, value.HomeBannerSecondaryActionURL); err != nil {
		return err
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
	"a":          {},
	"blockquote": {},
	"br":         {},
	"code":       {},
	"em":         {},
	"h1":         {},
	"h2":         {},
	"h3":         {},
	"hr":         {},
	"img":        {},
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
			if err := validateLegalRichTextAttributes(label, token); err != nil {
				return err
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

func validateLegalRichTextAttributes(label string, token html.Token) error {
	allowedAttributes := map[string]struct{}{}
	switch token.Data {
	case "a":
		allowedAttributes = map[string]struct{}{"href": {}, "rel": {}, "target": {}}
	case "img":
		allowedAttributes = map[string]struct{}{"alt": {}, "src": {}, "title": {}}
	case "p", "h1", "h2", "h3":
		allowedAttributes = map[string]struct{}{"style": {}}
	}
	seen := make(map[string]struct{}, len(token.Attr))
	for _, attribute := range token.Attr {
		if _, duplicate := seen[attribute.Key]; duplicate {
			return fmt.Errorf("%s的 HTML 标签 <%s> 包含重复属性 %s", label, token.Data, attribute.Key)
		}
		seen[attribute.Key] = struct{}{}
		if _, allowed := allowedAttributes[attribute.Key]; !allowed || attribute.Namespace != "" {
			return fmt.Errorf("%s的 HTML 标签 <%s> 包含不支持的属性 %s", label, token.Data, attribute.Key)
		}
		if err := validateLegalRichTextAttributeValue(label, token.Data, attribute.Key, attribute.Val); err != nil {
			return err
		}
	}
	if token.Data == "a" {
		if _, exists := seen["href"]; !exists {
			return fmt.Errorf("%s的链接缺少地址", label)
		}
	}
	if token.Data == "img" {
		if _, exists := seen["src"]; !exists {
			return fmt.Errorf("%s的图片缺少地址", label)
		}
	}
	return nil
}

func validateLegalRichTextAttributeValue(label string, tag string, key string, value string) error {
	switch key {
	case "href":
		parsed, err := url.Parse(value)
		if err != nil || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https" && parsed.Scheme != "mailto" && parsed.Scheme != "tel") {
			return fmt.Errorf("%s包含不安全的链接地址", label)
		}
	case "src":
		parsed, err := url.Parse(value)
		if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("%s包含不安全的图片地址", label)
		}
	case "target":
		if value != "_blank" {
			return fmt.Errorf("%s包含不支持的链接打开方式", label)
		}
	case "rel":
		if value != "noopener noreferrer" && value != "noopener noreferrer nofollow" {
			return fmt.Errorf("%s包含不支持的链接安全属性", label)
		}
	case "style":
		if tag != "p" && tag != "h1" && tag != "h2" && tag != "h3" {
			return fmt.Errorf("%s包含不支持的对齐位置", label)
		}
		if value != "text-align: left" && value != "text-align: center" && value != "text-align: right" && value != "text-align: justify" {
			return fmt.Errorf("%s包含不支持的文本对齐样式", label)
		}
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

func validateSiteBannerAction(label string, actionLabel string, rawURL string) error {
	if len([]rune(actionLabel)) > siteBannerActionMaxLen {
		return fmt.Errorf("%s名称不能超过 %d 个字符", label, siteBannerActionMaxLen)
	}
	if (actionLabel == "") != (rawURL == "") {
		return fmt.Errorf("%s名称与跳转链接必须同时填写", label)
	}
	return validateSiteRecordURL(label+"跳转链接", rawURL)
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
		HomeBannerEnabled:                value.HomeBannerEnabled,
		HomeBannerLabel:                  value.HomeBannerLabel,
		HomeBannerText:                   value.HomeBannerText,
		HomeBannerPrimaryActionLabel:     value.HomeBannerPrimaryActionLabel,
		HomeBannerPrimaryActionURL:       value.HomeBannerPrimaryActionURL,
		HomeBannerSecondaryActionLabel:   value.HomeBannerSecondaryActionLabel,
		HomeBannerSecondaryActionURL:     value.HomeBannerSecondaryActionURL,
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
