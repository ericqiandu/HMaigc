package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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
	siteSettingKey                 = "site"
	siteLogoMaxBytes               = 2 << 20
	siteMarketingImageMaxBytes     = 8 << 20
	siteAgreementMaxLen            = 50_000
	siteLegalHTMLMaxLen            = 256 << 10
	siteRecordNoMaxLen             = 100
	siteRecordURLMaxLen            = 500
	siteHomeHeroSloganMaxLen       = 40
	siteBannerLabelMaxLen          = 20
	siteBannerTextMaxLen           = 200
	siteBannerActionMaxLen         = 20
	siteMarketingTitleMaxLen       = 80
	siteMarketingDescriptionMaxLen = 200
	marketingPopupFrequencyOnce    = "once"
	marketingPopupFrequencyDaily   = "daily"
	marketingPopupFrequencySession = "session"
	homeBannerFrequencyAlways      = "always"
	homeBannerFrequencyOnce        = "once"
	homeBannerFrequencyDaily       = "daily"
	homeBannerFrequencySession     = "session"
)

var ErrSiteLogoNotConfigured = errors.New("站点 Logo 尚未配置")
var ErrSiteMarketingImageNotConfigured = errors.New("营销弹窗图片尚未配置")

type SiteSettingRequest struct {
	SiteName                         string `json:"siteName"`
	HomeHeroSlogan                   string `json:"homeHeroSlogan"`
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
	HomeBannerFrequency              string `json:"homeBannerFrequency"`
	MarketingPopupEnabled            bool   `json:"marketingPopupEnabled"`
	MarketingPopupTitle              string `json:"marketingPopupTitle"`
	MarketingPopupDescription        string `json:"marketingPopupDescription"`
	MarketingPopupActionLabel        string `json:"marketingPopupActionLabel"`
	MarketingPopupActionURL          string `json:"marketingPopupActionUrl"`
	MarketingPopupFrequency          string `json:"marketingPopupFrequency"`
}

type LegalContentSettingRequest struct {
	UserAgreement       string `json:"userAgreement"`
	PrivacyPolicy       string `json:"privacyPolicy"`
	MembershipAgreement string `json:"membershipAgreement"`
}

type PublicSiteSetting struct {
	SiteName                         string `json:"siteName"`
	HomeHeroSlogan                   string `json:"homeHeroSlogan"`
	LogoURL                          string `json:"logoUrl"`
	FooterCopyright                  string `json:"footerCopyright"`
	ICPRegistrationNumber            string `json:"icpRegistrationNumber"`
	ICPRegistrationURL               string `json:"icpRegistrationUrl"`
	PublicSecurityRegistrationNumber string `json:"publicSecurityRegistrationNumber"`
	PublicSecurityRegistrationURL    string `json:"publicSecurityRegistrationUrl"`
	UserAgreement                    string `json:"userAgreement"`
	PrivacyPolicy                    string `json:"privacyPolicy"`
	MembershipAgreement              string `json:"membershipAgreement"`
	HomeBannerEnabled                bool   `json:"homeBannerEnabled"`
	HomeBannerLabel                  string `json:"homeBannerLabel"`
	HomeBannerText                   string `json:"homeBannerText"`
	HomeBannerPrimaryActionLabel     string `json:"homeBannerPrimaryActionLabel"`
	HomeBannerPrimaryActionURL       string `json:"homeBannerPrimaryActionUrl"`
	HomeBannerSecondaryActionLabel   string `json:"homeBannerSecondaryActionLabel"`
	HomeBannerSecondaryActionURL     string `json:"homeBannerSecondaryActionUrl"`
	HomeBannerFrequency              string `json:"homeBannerFrequency"`
	MarketingPopupEnabled            bool   `json:"marketingPopupEnabled"`
	MarketingPopupImageURL           string `json:"marketingPopupImageUrl"`
	MarketingPopupTitle              string `json:"marketingPopupTitle"`
	MarketingPopupDescription        string `json:"marketingPopupDescription"`
	MarketingPopupActionLabel        string `json:"marketingPopupActionLabel"`
	MarketingPopupActionURL          string `json:"marketingPopupActionUrl"`
	MarketingPopupFrequency          string `json:"marketingPopupFrequency"`
	UpdatedBy                        string `json:"updatedBy"`
	CreatedAt                        string `json:"createdAt"`
	UpdatedAt                        string `json:"updatedAt"`
}

type siteSettingValue struct {
	SiteName                         string `json:"siteName"`
	HomeHeroSlogan                   string `json:"homeHeroSlogan"`
	LogoFile                         string `json:"logoFile"`
	LogoMimeType                     string `json:"logoMimeType"`
	FooterCopyright                  string `json:"footerCopyright"`
	ICPRegistrationNumber            string `json:"icpRegistrationNumber"`
	ICPRegistrationURL               string `json:"icpRegistrationUrl"`
	PublicSecurityRegistrationNumber string `json:"publicSecurityRegistrationNumber"`
	PublicSecurityRegistrationURL    string `json:"publicSecurityRegistrationUrl"`
	UserAgreement                    string `json:"userAgreement"`
	PrivacyPolicy                    string `json:"privacyPolicy"`
	MembershipAgreement              string `json:"membershipAgreement"`
	HomeBannerEnabled                bool   `json:"homeBannerEnabled"`
	HomeBannerLabel                  string `json:"homeBannerLabel"`
	HomeBannerText                   string `json:"homeBannerText"`
	HomeBannerPrimaryActionLabel     string `json:"homeBannerPrimaryActionLabel"`
	HomeBannerPrimaryActionURL       string `json:"homeBannerPrimaryActionUrl"`
	HomeBannerSecondaryActionLabel   string `json:"homeBannerSecondaryActionLabel"`
	HomeBannerSecondaryActionURL     string `json:"homeBannerSecondaryActionUrl"`
	HomeBannerFrequency              string `json:"homeBannerFrequency"`
	MarketingPopupEnabled            bool   `json:"marketingPopupEnabled"`
	MarketingPopupImageFile          string `json:"marketingPopupImageFile"`
	MarketingPopupImageMimeType      string `json:"marketingPopupImageMimeType"`
	MarketingPopupTitle              string `json:"marketingPopupTitle"`
	MarketingPopupDescription        string `json:"marketingPopupDescription"`
	MarketingPopupActionLabel        string `json:"marketingPopupActionLabel"`
	MarketingPopupActionURL          string `json:"marketingPopupActionUrl"`
	MarketingPopupFrequency          string `json:"marketingPopupFrequency"`
}

func (s *Service) PublicSiteSetting() (*PublicSiteSetting, error) {
	setting, value, err := s.readSiteSetting()
	if err != nil {
		return nil, err
	}
	result := publicSiteSetting(setting, value)
	return &result, nil
}

// LegalDocumentsConfigured reports whether both public legal documents have
// been explicitly published by an administrator. Empty documents are not a
// valid basis for accepting consent during registration.
func (s *Service) LegalDocumentsConfigured() (bool, error) {
	_, value, err := s.readSiteSetting()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(value.UserAgreement) != "" && strings.TrimSpace(value.PrivacyPolicy) != "", nil
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
		HomeHeroSlogan:                   strings.TrimSpace(req.HomeHeroSlogan),
		LogoFile:                         current.LogoFile,
		LogoMimeType:                     current.LogoMimeType,
		FooterCopyright:                  strings.TrimSpace(req.FooterCopyright),
		ICPRegistrationNumber:            strings.TrimSpace(req.ICPRegistrationNumber),
		ICPRegistrationURL:               strings.TrimSpace(req.ICPRegistrationURL),
		PublicSecurityRegistrationNumber: strings.TrimSpace(req.PublicSecurityRegistrationNumber),
		PublicSecurityRegistrationURL:    strings.TrimSpace(req.PublicSecurityRegistrationURL),
		UserAgreement:                    current.UserAgreement,
		PrivacyPolicy:                    current.PrivacyPolicy,
		MembershipAgreement:              current.MembershipAgreement,
		HomeBannerEnabled:                req.HomeBannerEnabled,
		HomeBannerLabel:                  strings.TrimSpace(req.HomeBannerLabel),
		HomeBannerText:                   strings.TrimSpace(req.HomeBannerText),
		HomeBannerPrimaryActionLabel:     strings.TrimSpace(req.HomeBannerPrimaryActionLabel),
		HomeBannerPrimaryActionURL:       strings.TrimSpace(req.HomeBannerPrimaryActionURL),
		HomeBannerSecondaryActionLabel:   strings.TrimSpace(req.HomeBannerSecondaryActionLabel),
		HomeBannerSecondaryActionURL:     strings.TrimSpace(req.HomeBannerSecondaryActionURL),
		HomeBannerFrequency:              strings.TrimSpace(req.HomeBannerFrequency),
		MarketingPopupEnabled:            req.MarketingPopupEnabled,
		MarketingPopupImageFile:          current.MarketingPopupImageFile,
		MarketingPopupImageMimeType:      current.MarketingPopupImageMimeType,
		MarketingPopupTitle:              strings.TrimSpace(req.MarketingPopupTitle),
		MarketingPopupDescription:        strings.TrimSpace(req.MarketingPopupDescription),
		MarketingPopupActionLabel:        strings.TrimSpace(req.MarketingPopupActionLabel),
		MarketingPopupActionURL:          strings.TrimSpace(req.MarketingPopupActionURL),
		MarketingPopupFrequency:          strings.TrimSpace(req.MarketingPopupFrequency),
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

func (s *Service) UpdateMarketingPopupImage(actor *model.User, header *multipart.FileHeader) (*PublicSiteSetting, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	s.siteSettingMu.Lock()
	defer s.siteSettingMu.Unlock()
	if header == nil {
		return nil, BadAuthRequest("请选择要上传的营销图片")
	}
	if header.Size <= 0 || header.Size > siteMarketingImageMaxBytes {
		return nil, BadAuthRequest("营销图片大小必须在 8MB 以内")
	}
	content, mimeType, extension, err := readManagedImage(header, siteMarketingImageMaxBytes, "营销图片")
	if err != nil {
		return nil, err
	}
	currentSetting, current, err := s.readSiteSetting()
	if err != nil {
		return nil, err
	}
	assetDir := s.siteLogoDir()
	if err := os.MkdirAll(assetDir, 0o750); err != nil {
		return nil, fmt.Errorf("创建站点素材目录失败: %w", err)
	}
	fileName := "marketing-" + managedImageFileName(content, extension)
	finalPath := filepath.Join(assetDir, fileName)
	createdFile := false
	if _, err := os.Stat(finalPath); errors.Is(err, os.ErrNotExist) {
		if err := writeFileAtomically(assetDir, ".site-marketing-*", finalPath, content); err != nil {
			return nil, err
		}
		createdFile = true
	} else if err != nil {
		return nil, fmt.Errorf("检查营销图片文件失败: %w", err)
	}
	next := current
	next.MarketingPopupImageFile = fileName
	next.MarketingPopupImageMimeType = mimeType
	setting, err := s.saveSiteSetting(actor, currentSetting, next)
	if err != nil {
		if createdFile {
			if cleanupErr := removeManagedSiteAsset(finalPath); cleanupErr != nil {
				return nil, errors.Join(err, fmt.Errorf("保存营销图片配置失败且清理新文件失败: %w", cleanupErr))
			}
		}
		return nil, err
	}
	result := publicSiteSetting(setting, next)
	if err := s.appendAdminAudit(actor, "site_setting.marketing_image.update", "system_setting", siteSettingKey, "更新登录后营销弹窗图片", result); err != nil {
		return nil, err
	}
	if current.MarketingPopupImageFile != "" && current.MarketingPopupImageFile != fileName {
		oldPath := filepath.Join(assetDir, filepath.Base(current.MarketingPopupImageFile))
		if err := removeManagedSiteAsset(oldPath); err != nil {
			return nil, s.reportSiteAssetCleanupFailure(actor, "site_setting.marketing_image.cleanup_failed", oldPath, err)
		}
	}
	return &result, nil
}

func (s *Service) RemoveMarketingPopupImage(actor *model.User) (*PublicSiteSetting, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	s.siteSettingMu.Lock()
	defer s.siteSettingMu.Unlock()
	currentSetting, current, err := s.readSiteSetting()
	if err != nil {
		return nil, err
	}
	previousFile := current.MarketingPopupImageFile
	next := current
	next.MarketingPopupImageFile = ""
	next.MarketingPopupImageMimeType = ""
	setting, err := s.saveSiteSetting(actor, currentSetting, next)
	if err != nil {
		return nil, err
	}
	result := publicSiteSetting(setting, next)
	if err := s.appendAdminAudit(actor, "site_setting.marketing_image.remove", "system_setting", siteSettingKey, "移除登录后营销弹窗图片", result); err != nil {
		return nil, err
	}
	if previousFile != "" {
		previousPath := filepath.Join(s.siteLogoDir(), filepath.Base(previousFile))
		if err := removeManagedSiteAsset(previousPath); err != nil {
			return nil, s.reportSiteAssetCleanupFailure(actor, "site_setting.marketing_image.cleanup_failed", previousPath, err)
		}
	}
	return &result, nil
}

type siteAssetCleanupFailure struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

func removeManagedSiteAsset(path string) error {
	err := os.Remove(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Service) reportSiteAssetCleanupFailure(actor *model.User, action string, path string, cleanupErr error) error {
	log.Printf("site asset cleanup failed action=%s path=%s error=%v", action, path, cleanupErr)
	auditErr := s.appendAdminAudit(actor, action, "system_setting", siteSettingKey, "站点营销素材清理失败", siteAssetCleanupFailure{
		Path:  path,
		Error: cleanupErr.Error(),
	})
	if auditErr != nil {
		return errors.Join(fmt.Errorf("营销素材已更新，但旧文件清理失败: %w", cleanupErr), fmt.Errorf("记录清理失败审计日志失败: %w", auditErr))
	}
	return fmt.Errorf("营销素材已更新，但旧文件清理失败: %w", cleanupErr)
}

func (s *Service) MarketingPopupImageFile() (string, string, time.Time, error) {
	setting, value, err := s.readSiteSetting()
	if err != nil {
		return "", "", time.Time{}, err
	}
	if setting == nil || value.MarketingPopupImageFile == "" {
		return "", "", time.Time{}, ErrSiteMarketingImageNotConfigured
	}
	fileName := filepath.Base(value.MarketingPopupImageFile)
	if fileName != value.MarketingPopupImageFile {
		return "", "", time.Time{}, errors.New("营销弹窗图片配置无效")
	}
	filePath := filepath.Join(s.siteLogoDir(), fileName)
	info, err := os.Stat(filePath)
	if err != nil {
		return "", "", time.Time{}, err
	}
	if !info.Mode().IsRegular() {
		return "", "", time.Time{}, errors.New("营销弹窗图片文件无效")
	}
	return filePath, value.MarketingPopupImageMimeType, info.ModTime(), nil
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
	next.MembershipAgreement = strings.TrimSpace(req.MembershipAgreement)
	if err := validateSiteSetting(next); err != nil {
		return nil, BadAuthRequest(err.Error())
	}
	setting, err := s.saveSiteSetting(actor, currentSetting, next)
	if err != nil {
		return nil, err
	}
	result := publicSiteSetting(setting, next)
	audit := map[string]bool{
		"userAgreementPublished":       next.UserAgreement != "",
		"privacyPolicyPublished":       next.PrivacyPolicy != "",
		"membershipAgreementPublished": next.MembershipAgreement != "",
	}
	if err := s.appendAdminAudit(actor, "site_setting.legal.update", "system_setting", siteSettingKey, "更新用户协议、隐私政策与会员服务协议", audit); err != nil {
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
		SiteName:                "HMaigc",
		HomeHeroSlogan:          "让算力更有想象力！",
		FooterCopyright:         fmt.Sprintf("© %d HMaigc. 保留所有权利。", time.Now().Year()),
		HomeBannerEnabled:       true,
		HomeBannerLabel:         "招募中",
		HomeBannerText:          "招增长伙伴：懂冷启动、内容增长或海外增长，欢迎加入 HMaigc。",
		HomeBannerFrequency:     homeBannerFrequencyAlways,
		MarketingPopupFrequency: marketingPopupFrequencyOnce,
	}
}

func validateSiteSetting(value siteSettingValue) error {
	nameLength := len([]rune(strings.TrimSpace(value.SiteName)))
	if nameLength < 1 || nameLength > 40 {
		return errors.New("站点名称必须是 1-40 个字符")
	}
	homeHeroSloganLength := len([]rune(strings.TrimSpace(value.HomeHeroSlogan)))
	if homeHeroSloganLength < 1 || homeHeroSloganLength > siteHomeHeroSloganMaxLen {
		return fmt.Errorf("首页主口号必须是 1-%d 个字符", siteHomeHeroSloganMaxLen)
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
	if value.HomeBannerFrequency != homeBannerFrequencyAlways && value.HomeBannerFrequency != homeBannerFrequencyOnce && value.HomeBannerFrequency != homeBannerFrequencyDaily && value.HomeBannerFrequency != homeBannerFrequencySession {
		return errors.New("首页横幅展示频率无效")
	}
	if len([]rune(value.MarketingPopupTitle)) > siteMarketingTitleMaxLen {
		return fmt.Errorf("营销弹窗标题不能超过 %d 个字符", siteMarketingTitleMaxLen)
	}
	if len([]rune(value.MarketingPopupDescription)) > siteMarketingDescriptionMaxLen {
		return fmt.Errorf("营销弹窗说明不能超过 %d 个字符", siteMarketingDescriptionMaxLen)
	}
	if value.MarketingPopupFrequency != marketingPopupFrequencyOnce && value.MarketingPopupFrequency != marketingPopupFrequencyDaily && value.MarketingPopupFrequency != marketingPopupFrequencySession {
		return errors.New("营销弹窗展示频率无效")
	}
	if value.MarketingPopupEnabled {
		if value.MarketingPopupImageFile == "" {
			return errors.New("启用营销弹窗前必须上传展示图片")
		}
		if value.MarketingPopupTitle == "" {
			return errors.New("启用营销弹窗前必须填写标题")
		}
	}
	if err := validateSiteBannerAction("营销弹窗按钮", value.MarketingPopupActionLabel, value.MarketingPopupActionURL); err != nil {
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
	if err := validateLegalRichText("会员服务协议", value.MembershipAgreement); err != nil {
		return err
	}
	if value.LogoFile != "" && filepath.Base(value.LogoFile) != value.LogoFile {
		return errors.New("站点 Logo 文件配置无效")
	}
	if value.LogoFile == "" && value.LogoMimeType != "" {
		return errors.New("站点 Logo 文件与类型不一致")
	}
	if value.MarketingPopupImageFile != "" && filepath.Base(value.MarketingPopupImageFile) != value.MarketingPopupImageFile {
		return errors.New("营销弹窗图片文件配置无效")
	}
	if value.MarketingPopupImageFile == "" && value.MarketingPopupImageMimeType != "" {
		return errors.New("营销弹窗图片文件与类型不一致")
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
		if !isSupportedLegalTextAlignmentStyle(value) {
			return fmt.Errorf("%s包含不支持的文本对齐样式", label)
		}
	}
	return nil
}

func isSupportedLegalTextAlignmentStyle(value string) bool {
	declaration := strings.TrimSpace(value)
	declaration = strings.TrimSpace(strings.TrimSuffix(declaration, ";"))
	if declaration == "" || strings.Contains(declaration, ";") {
		return false
	}
	property, alignment, found := strings.Cut(declaration, ":")
	if !found || strings.TrimSpace(property) != "text-align" {
		return false
	}
	switch strings.TrimSpace(alignment) {
	case "left", "center", "right", "justify":
		return true
	default:
		return false
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
		HomeHeroSlogan:                   value.HomeHeroSlogan,
		FooterCopyright:                  value.FooterCopyright,
		ICPRegistrationNumber:            value.ICPRegistrationNumber,
		ICPRegistrationURL:               value.ICPRegistrationURL,
		PublicSecurityRegistrationNumber: value.PublicSecurityRegistrationNumber,
		PublicSecurityRegistrationURL:    value.PublicSecurityRegistrationURL,
		UserAgreement:                    value.UserAgreement,
		PrivacyPolicy:                    value.PrivacyPolicy,
		MembershipAgreement:              value.MembershipAgreement,
		HomeBannerEnabled:                value.HomeBannerEnabled,
		HomeBannerLabel:                  value.HomeBannerLabel,
		HomeBannerText:                   value.HomeBannerText,
		HomeBannerPrimaryActionLabel:     value.HomeBannerPrimaryActionLabel,
		HomeBannerPrimaryActionURL:       value.HomeBannerPrimaryActionURL,
		HomeBannerSecondaryActionLabel:   value.HomeBannerSecondaryActionLabel,
		HomeBannerSecondaryActionURL:     value.HomeBannerSecondaryActionURL,
		HomeBannerFrequency:              value.HomeBannerFrequency,
		MarketingPopupEnabled:            value.MarketingPopupEnabled,
		MarketingPopupTitle:              value.MarketingPopupTitle,
		MarketingPopupDescription:        value.MarketingPopupDescription,
		MarketingPopupActionLabel:        value.MarketingPopupActionLabel,
		MarketingPopupActionURL:          value.MarketingPopupActionURL,
		MarketingPopupFrequency:          value.MarketingPopupFrequency,
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
	if value.MarketingPopupImageFile != "" {
		version := "configured"
		if setting != nil && !setting.UpdatedAt.IsZero() {
			version = fmt.Sprintf("%d", setting.UpdatedAt.UnixNano())
		}
		result.MarketingPopupImageURL = "/api/public/site/marketing-image?v=" + version
	}
	return result
}

func (s *Service) siteLogoDir() string {
	return filepath.Join(s.dataDir, "site-assets")
}
