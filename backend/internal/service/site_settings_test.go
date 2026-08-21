package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const expectedDefaultHomeHeroSlogan = "让算力更有想象力！"

func newSiteSettingTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.SystemSetting{}, &model.AdminAuditEvent{}); err != nil {
		t.Fatal(err)
	}
	return New(repository.New(db), t.TempDir()), db
}

func TestSiteSettingDefaultsAndAdminUpdate(t *testing.T) {
	svc, db := newSiteSettingTestService(t)
	legalDocumentsConfigured, err := svc.LegalDocumentsConfigured()
	if err != nil {
		t.Fatal(err)
	}
	if legalDocumentsConfigured {
		t.Fatal("default legal documents should not be treated as published")
	}
	defaults, err := svc.PublicSiteSetting()
	if err != nil {
		t.Fatal(err)
	}
	if defaults.SiteName != "HMaigc" || defaults.HomeHeroSlogan != expectedDefaultHomeHeroSlogan || defaults.FooterCopyright == "" || defaults.LogoURL != "" {
		t.Fatalf("unexpected defaults: %#v", defaults)
	}
	if !defaults.HomeBannerEnabled || defaults.HomeBannerText == "" {
		t.Fatalf("unexpected home banner defaults: %#v", defaults)
	}
	if defaults.HomeBannerFrequency != "always" {
		t.Fatalf("unexpected home banner frequency: %#v", defaults)
	}
	if defaults.MarketingPopupEnabled || defaults.MarketingPopupFrequency != "once" {
		t.Fatalf("unexpected marketing popup defaults: %#v", defaults)
	}

	admin := &model.User{ID: "site-admin", Role: model.UserRoleAdmin}
	updated, err := svc.UpdateSiteSetting(admin, SiteSettingRequest{
		SiteName:                         "弘梦 AIGC",
		HomeHeroSlogan:                   "让创作更有想象力！",
		FooterCopyright:                  "© 弘梦科技",
		ICPRegistrationNumber:            "蜀ICP备2026000000号-1",
		ICPRegistrationURL:               "https://beian.miit.gov.cn/",
		PublicSecurityRegistrationNumber: "川公网安备51000000000000号",
		PublicSecurityRegistrationURL:    "http://www.beian.gov.cn/portal/registerSystemInfo?recordcode=51000000000000",
		HomeBannerEnabled:                true,
		HomeBannerLabel:                  "活动中",
		HomeBannerText:                   "商业合作伙伴招募计划",
		HomeBannerPrimaryActionLabel:     "查看详情",
		HomeBannerPrimaryActionURL:       "https://hmaigc.ai/partner",
		HomeBannerFrequency:              "daily",
		MarketingPopupFrequency:          "once",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.SiteName != "弘梦 AIGC" || updated.HomeHeroSlogan != "让创作更有想象力！" || updated.ICPRegistrationNumber == "" || updated.PublicSecurityRegistrationNumber == "" || updated.HomeBannerText != "商业合作伙伴招募计划" {
		t.Fatalf("unexpected updated setting: %#v", updated)
	}
	updated, err = svc.UpdateLegalContentSetting(admin, LegalContentSettingRequest{
		UserAgreement:       "<p>第一条 用户权利与义务</p>",
		PrivacyPolicy:       "<p>第一条 信息处理规则</p>",
		MembershipAgreement: "<p>第一条 会员购买与服务规则</p>",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.UserAgreement == "" || updated.PrivacyPolicy == "" || updated.MembershipAgreement == "" {
		t.Fatalf("unexpected legal setting: %#v", updated)
	}
	legalDocumentsConfigured, err = svc.LegalDocumentsConfigured()
	if err != nil {
		t.Fatal(err)
	}
	if !legalDocumentsConfigured {
		t.Fatal("saved legal documents should be treated as published")
	}
	legalAgreement := updated.UserAgreement
	legalPrivacy := updated.PrivacyPolicy
	legalMembership := updated.MembershipAgreement
	updated, err = svc.UpdateSiteSetting(admin, SiteSettingRequest{SiteName: "弘梦 AIGC 2", HomeHeroSlogan: "让创作更有想象力！", FooterCopyright: "© 弘梦科技", HomeBannerEnabled: true, HomeBannerText: "新版运营公告", HomeBannerFrequency: "session", MarketingPopupFrequency: "once"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.UserAgreement != legalAgreement || updated.PrivacyPolicy != legalPrivacy || updated.MembershipAgreement != legalMembership {
		t.Fatalf("brand update overwrote legal content: %#v", updated)
	}
	reloaded, err := svc.PublicSiteSetting()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.SiteName != updated.SiteName || reloaded.FooterCopyright != updated.FooterCopyright || reloaded.HomeBannerFrequency != "session" {
		t.Fatalf("reloaded setting = %#v, want %#v", reloaded, updated)
	}
	var auditCount int64
	if err := db.Model(&model.AdminAuditEvent{}).Where("action = ?", "site_setting.update").Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if auditCount != 2 {
		t.Fatalf("audit count = %d, want 2", auditCount)
	}
	var legalAuditCount int64
	if err := db.Model(&model.AdminAuditEvent{}).Where("action = ?", "site_setting.legal.update").Count(&legalAuditCount).Error; err != nil {
		t.Fatal(err)
	}
	if legalAuditCount != 1 {
		t.Fatalf("legal audit count = %d, want 1", legalAuditCount)
	}
	var legalAudit model.AdminAuditEvent
	if err := db.Where("action = ?", "site_setting.legal.update").First(&legalAudit).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(legalAudit.MetadataJSON, "用户权利与义务") || strings.Contains(legalAudit.MetadataJSON, "信息处理规则") || strings.Contains(legalAudit.MetadataJSON, "会员购买与服务规则") {
		t.Fatalf("legal audit leaked agreement body: %s", legalAudit.MetadataJSON)
	}
	for _, marker := range []string{`"userAgreementPublished":true`, `"privacyPolicyPublished":true`, `"membershipAgreementPublished":true`} {
		if !strings.Contains(legalAudit.MetadataJSON, marker) {
			t.Fatalf("legal audit metadata %s missing %s", legalAudit.MetadataJSON, marker)
		}
	}
}

func TestSiteSettingHomeHeroSloganContract(t *testing.T) {
	svc, _ := newSiteSettingTestService(t)
	admin := &model.User{ID: "site-admin", Role: model.UserRoleAdmin}

	defaults, err := svc.PublicSiteSetting()
	if err != nil {
		t.Fatal(err)
	}
	defaultPayload := siteSettingPayload(t, defaults)
	if got := defaultPayload.HomeHeroSlogan; got != expectedDefaultHomeHeroSlogan {
		t.Fatalf("default homeHeroSlogan = %#v, want %q", got, expectedDefaultHomeHeroSlogan)
	}

	request := siteSettingRequestFromPayload(t, map[string]string{
		"siteName":                "HMaigc",
		"homeHeroSlogan":          "  创意从每一次生成开始  ",
		"homeBannerFrequency":     "always",
		"marketingPopupFrequency": "once",
	})
	updated, err := svc.UpdateSiteSetting(admin, request)
	if err != nil {
		t.Fatal(err)
	}
	updatedPayload := siteSettingPayload(t, updated)
	if got := updatedPayload.HomeHeroSlogan; got != "创意从每一次生成开始" {
		t.Fatalf("saved homeHeroSlogan = %#v, want trimmed value", got)
	}
	maximumSlogan := strings.Repeat("算", 40)
	maximumRequest := siteSettingRequestFromPayload(t, map[string]string{
		"siteName":                "HMaigc",
		"homeHeroSlogan":          maximumSlogan,
		"homeBannerFrequency":     "always",
		"marketingPopupFrequency": "once",
	})
	maximumUpdated, err := svc.UpdateSiteSetting(admin, maximumRequest)
	if err != nil {
		t.Fatalf("40-character homeHeroSlogan should be accepted: %v", err)
	}
	if got := siteSettingPayload(t, maximumUpdated).HomeHeroSlogan; got != maximumSlogan {
		t.Fatalf("maximum homeHeroSlogan = %q, want %q", got, maximumSlogan)
	}

	for name, slogan := range map[string]string{
		"empty":    "   ",
		"too_long": strings.Repeat("算", 41),
	} {
		t.Run(name, func(t *testing.T) {
			invalid := siteSettingRequestFromPayload(t, map[string]string{
				"siteName":                "HMaigc",
				"homeHeroSlogan":          slogan,
				"homeBannerFrequency":     "always",
				"marketingPopupFrequency": "once",
			})
			if _, err := svc.UpdateSiteSetting(admin, invalid); err == nil {
				t.Fatalf("homeHeroSlogan %q should be rejected", slogan)
			}
		})
	}
}

func siteSettingRequestFromPayload(t *testing.T, payload map[string]string) SiteSettingRequest {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var request SiteSettingRequest
	if err := json.Unmarshal(encoded, &request); err != nil {
		t.Fatal(err)
	}
	return request
}

func siteSettingPayload(t *testing.T, value *PublicSiteSetting) struct {
	HomeHeroSlogan string `json:"homeHeroSlogan"`
} {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		HomeHeroSlogan string `json:"homeHeroSlogan"`
	}
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestSiteSettingMembershipAgreementValidationIsAtomicAndDoesNotGateRegistration(t *testing.T) {
	svc, _ := newSiteSettingTestService(t)
	admin := &model.User{ID: "site-admin", Role: model.UserRoleAdmin}
	valid := LegalContentSettingRequest{
		UserAgreement:       "<p>用户协议已发布</p>",
		PrivacyPolicy:       "<p>隐私政策已发布</p>",
		MembershipAgreement: "<p>会员服务协议已发布</p>",
	}
	if _, err := svc.UpdateLegalContentSetting(admin, valid); err != nil {
		t.Fatal(err)
	}

	invalid := valid
	invalid.UserAgreement = "<p>不应被部分保存的用户协议</p>"
	invalid.MembershipAgreement = "<script>alert(1)</script>"
	if _, err := svc.UpdateLegalContentSetting(admin, invalid); err == nil {
		t.Fatal("unsafe membership agreement should be rejected")
	}
	reloaded, err := svc.PublicSiteSetting()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.UserAgreement != valid.UserAgreement || reloaded.PrivacyPolicy != valid.PrivacyPolicy || reloaded.MembershipAgreement != valid.MembershipAgreement {
		t.Fatalf("invalid legal update partially overwrote stored content: %#v", reloaded)
	}

	withoutMembership := valid
	withoutMembership.MembershipAgreement = ""
	if _, err := svc.UpdateLegalContentSetting(admin, withoutMembership); err != nil {
		t.Fatalf("empty membership agreement should remain a valid publication state: %v", err)
	}
	configured, err := svc.LegalDocumentsConfigured()
	if err != nil {
		t.Fatal(err)
	}
	if !configured {
		t.Fatal("membership agreement publication must not gate registration consent")
	}
}

func TestSiteSettingRejectsUnauthorizedAndInvalidUpdates(t *testing.T) {
	svc, _ := newSiteSettingTestService(t)
	user := &model.User{ID: "site-user", Role: model.UserRoleUser}
	_, err := svc.UpdateSiteSetting(user, SiteSettingRequest{SiteName: "无权修改"})
	requireAuthStatus(t, err, http.StatusForbidden)

	admin := &model.User{ID: "site-admin", Role: model.UserRoleAdmin}
	if _, err := svc.UpdateSiteSetting(admin, SiteSettingRequest{SiteName: ""}); err == nil {
		t.Fatal("empty site name should be rejected")
	}
	oversizedAgreement := string(bytes.Repeat([]byte("字"), siteAgreementMaxLen+1))
	if _, err := svc.UpdateLegalContentSetting(admin, LegalContentSettingRequest{UserAgreement: oversizedAgreement}); err == nil {
		t.Fatal("oversized agreement should be rejected")
	}
	maximumRichText := "<p>" + strings.Repeat("协", siteAgreementMaxLen) + "</p>"
	if _, err := svc.UpdateLegalContentSetting(admin, LegalContentSettingRequest{UserAgreement: maximumRichText}); err != nil {
		t.Fatalf("maximum visible rich text should be accepted: %v", err)
	}
	if _, err := svc.UpdateLegalContentSetting(admin, LegalContentSettingRequest{UserAgreement: "<script>alert(1)</script>"}); err == nil {
		t.Fatal("script tag should be rejected")
	}
	if _, err := svc.UpdateLegalContentSetting(admin, LegalContentSettingRequest{UserAgreement: `<p class="external">协议</p>`}); err == nil {
		t.Fatal("HTML attributes should be rejected")
	}
	legalRichContent := `<h2 style="text-align: center">协议标题</h2><p><a target="_blank" rel="noopener noreferrer" href="https://hmaigc.ai/help">帮助</a></p><img src="https://assets.hmaigc.ai/legal/example.png" alt="协议示例">`
	if _, err := svc.UpdateLegalContentSetting(admin, LegalContentSettingRequest{UserAgreement: legalRichContent}); err != nil {
		t.Fatalf("safe legal links, alignment and images should be accepted: %v", err)
	}
	browserSerializedAlignment := `<h2 style="text-align: center;">协议标题</h2>`
	if _, err := svc.UpdateLegalContentSetting(admin, LegalContentSettingRequest{UserAgreement: browserSerializedAlignment}); err != nil {
		t.Fatalf("browser-serialized text alignment should be accepted: %v", err)
	}
	unsupportedLegalStyle := `<h2 style="text-align: center; color: red">协议标题</h2>`
	if _, err := svc.UpdateLegalContentSetting(admin, LegalContentSettingRequest{UserAgreement: unsupportedLegalStyle}); err == nil {
		t.Fatal("additional legal text styles should be rejected")
	}
	unsafeLegalContent := `<p><a href="javascript:alert(1)">危险链接</a></p>`
	if _, err := svc.UpdateLegalContentSetting(admin, LegalContentSettingRequest{UserAgreement: unsafeLegalContent}); err == nil {
		t.Fatal("unsafe legal link should be rejected")
	}
	unsafeLegalImage := `<img src="data:image/png;base64,AAAA">`
	if _, err := svc.UpdateLegalContentSetting(admin, LegalContentSettingRequest{UserAgreement: unsafeLegalImage}); err == nil {
		t.Fatal("inline legal image should be rejected")
	}
	if _, err := svc.UpdateSiteSetting(admin, SiteSettingRequest{SiteName: "弘梦", HomeHeroSlogan: expectedDefaultHomeHeroSlogan, ICPRegistrationURL: "javascript:alert(1)", HomeBannerFrequency: "always", MarketingPopupFrequency: "once"}); err == nil {
		t.Fatal("unsafe registration URL should be rejected")
	}
	if _, err := svc.UpdateSiteSetting(admin, SiteSettingRequest{SiteName: "弘梦", HomeHeroSlogan: expectedDefaultHomeHeroSlogan, PublicSecurityRegistrationURL: "https://user@example.com/path", HomeBannerFrequency: "always", MarketingPopupFrequency: "once"}); err == nil {
		t.Fatal("registration URL with credentials should be rejected")
	}
	if _, err := svc.UpdateSiteSetting(admin, SiteSettingRequest{SiteName: "弘梦", HomeHeroSlogan: expectedDefaultHomeHeroSlogan, ICPRegistrationURL: "https://beian.miit.gov.cn/", HomeBannerFrequency: "always", MarketingPopupFrequency: "once"}); err == nil {
		t.Fatal("registration URL without registration number should be rejected")
	}
	if _, err := svc.UpdateSiteSetting(admin, SiteSettingRequest{SiteName: "弘梦", HomeHeroSlogan: expectedDefaultHomeHeroSlogan, HomeBannerEnabled: true, HomeBannerFrequency: "always", MarketingPopupFrequency: "once"}); err == nil {
		t.Fatal("enabled home banner without text should be rejected")
	}
	if _, err := svc.UpdateSiteSetting(admin, SiteSettingRequest{SiteName: "弘梦", HomeHeroSlogan: expectedDefaultHomeHeroSlogan, HomeBannerText: "公告", HomeBannerPrimaryActionLabel: "查看", HomeBannerFrequency: "always", MarketingPopupFrequency: "once"}); err == nil {
		t.Fatal("home banner action label without URL should be rejected")
	}
	if _, err := svc.UpdateSiteSetting(admin, SiteSettingRequest{SiteName: "弘梦", HomeHeroSlogan: expectedDefaultHomeHeroSlogan, HomeBannerText: "公告", HomeBannerPrimaryActionLabel: "查看", HomeBannerPrimaryActionURL: "javascript:alert(1)", HomeBannerFrequency: "always", MarketingPopupFrequency: "once"}); err == nil {
		t.Fatal("unsafe home banner action URL should be rejected")
	}
	if _, err := svc.UpdateSiteSetting(admin, SiteSettingRequest{SiteName: "弘梦", HomeHeroSlogan: expectedDefaultHomeHeroSlogan, HomeBannerFrequency: "always", MarketingPopupEnabled: true, MarketingPopupTitle: "新品上线", MarketingPopupFrequency: "once"}); err == nil {
		t.Fatal("enabled marketing popup without image should be rejected")
	}
	if _, err := svc.UpdateSiteSetting(admin, SiteSettingRequest{SiteName: "弘梦", HomeHeroSlogan: expectedDefaultHomeHeroSlogan, HomeBannerFrequency: "always", MarketingPopupFrequency: "always"}); err == nil {
		t.Fatal("invalid marketing popup frequency should be rejected")
	}
	if _, err := svc.UpdateSiteSetting(admin, SiteSettingRequest{SiteName: "弘梦", HomeHeroSlogan: expectedDefaultHomeHeroSlogan, HomeBannerFrequency: "hourly", MarketingPopupFrequency: "once"}); err == nil {
		t.Fatal("invalid home banner frequency should be rejected")
	}
}

func TestSiteLogoUploadAndRemoval(t *testing.T) {
	svc, _ := newSiteSettingTestService(t)
	admin := &model.User{ID: "site-admin", Role: model.UserRoleAdmin}
	header := pngFileHeader(t)
	updated, err := svc.UpdateSiteLogo(admin, header)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LogoURL == "" {
		t.Fatal("uploaded logo URL should not be empty")
	}
	filePath, mimeType, _, err := svc.SiteLogoFile()
	if err != nil {
		t.Fatal(err)
	}
	if filePath == "" || mimeType != "image/png" {
		t.Fatalf("logo file = %q, mime = %q", filePath, mimeType)
	}

	removed, err := svc.RemoveSiteLogo(admin)
	if err != nil {
		t.Fatal(err)
	}
	if removed.LogoURL != "" {
		t.Fatalf("removed logo URL = %q, want empty", removed.LogoURL)
	}
	if _, _, _, err := svc.SiteLogoFile(); !errors.Is(err, ErrSiteLogoNotConfigured) {
		t.Fatalf("logo after removal error = %v", err)
	}
}

func TestMarketingPopupImageAndConfiguration(t *testing.T) {
	svc, _ := newSiteSettingTestService(t)
	admin := &model.User{ID: "site-admin", Role: model.UserRoleAdmin}
	withImage, err := svc.UpdateMarketingPopupImage(admin, pngFileHeader(t))
	if err != nil {
		t.Fatal(err)
	}
	if withImage.MarketingPopupImageURL == "" {
		t.Fatal("uploaded marketing image URL should not be empty")
	}
	filePath, mimeType, _, err := svc.MarketingPopupImageFile()
	if err != nil {
		t.Fatal(err)
	}
	if filePath == "" || mimeType != "image/png" {
		t.Fatalf("marketing image file = %q, mime = %q", filePath, mimeType)
	}
	configured, err := svc.UpdateSiteSetting(admin, SiteSettingRequest{
		SiteName:                  "HMaigc",
		HomeHeroSlogan:            expectedDefaultHomeHeroSlogan,
		MarketingPopupEnabled:     true,
		MarketingPopupTitle:       "Seedance 2.5 预售上线",
		MarketingPopupDescription: "预售加赠生成次数",
		MarketingPopupActionLabel: "立即参与",
		MarketingPopupActionURL:   "https://hmaigc.ai/membership",
		HomeBannerFrequency:       "always",
		MarketingPopupFrequency:   "daily",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !configured.MarketingPopupEnabled || configured.MarketingPopupFrequency != "daily" || configured.MarketingPopupImageURL == "" {
		t.Fatalf("unexpected marketing popup setting: %#v", configured)
	}
	if _, err := svc.RemoveMarketingPopupImage(admin); err == nil {
		t.Fatal("enabled marketing popup image removal should be rejected")
	}
	configured, err = svc.UpdateSiteSetting(admin, SiteSettingRequest{SiteName: "HMaigc", HomeHeroSlogan: expectedDefaultHomeHeroSlogan, HomeBannerFrequency: "always", MarketingPopupFrequency: "once"})
	if err != nil {
		t.Fatal(err)
	}
	removed, err := svc.RemoveMarketingPopupImage(admin)
	if err != nil {
		t.Fatal(err)
	}
	if removed.MarketingPopupImageURL != "" {
		t.Fatalf("removed marketing image URL = %q, want empty", removed.MarketingPopupImageURL)
	}
	if _, _, _, err := svc.MarketingPopupImageFile(); !errors.Is(err, ErrSiteMarketingImageNotConfigured) {
		t.Fatalf("marketing image after removal error = %v", err)
	}
}

func TestSiteSettingDoesNotHideCorruptedStoredData(t *testing.T) {
	svc, db := newSiteSettingTestService(t)
	if err := db.Create(&model.SystemSetting{Key: siteSettingKey, ValueJSON: "not-json"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublicSiteSetting(); err == nil {
		t.Fatal("corrupted setting should fail explicitly")
	}
}

func TestSiteSettingAppliesNewFieldDefaultsToStoredConfiguration(t *testing.T) {
	svc, db := newSiteSettingTestService(t)
	stored := `{"siteName":"弘梦","footerCopyright":"© 弘梦科技"}`
	if err := db.Create(&model.SystemSetting{Key: siteSettingKey, ValueJSON: stored}).Error; err != nil {
		t.Fatal(err)
	}
	setting, err := svc.PublicSiteSetting()
	if err != nil {
		t.Fatal(err)
	}
	if setting.SiteName != "弘梦" || setting.HomeHeroSlogan != expectedDefaultHomeHeroSlogan || !setting.HomeBannerEnabled || setting.HomeBannerText == "" || setting.HomeBannerFrequency != "always" || setting.MarketingPopupFrequency != "once" {
		t.Fatalf("stored setting did not receive current schema defaults: %#v", setting)
	}
	if setting.MembershipAgreement != "" {
		t.Fatalf("legacy site setting membership agreement = %q, want empty", setting.MembershipAgreement)
	}
}

func pngFileHeader(t *testing.T) *multipart.FileHeader {
	t.Helper()
	pngData, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "logo.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(pngData); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, "/", &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if err := request.ParseMultipartForm(int64(len(body.Bytes()))); err != nil {
		t.Fatal(err)
	}
	files := request.MultipartForm.File["file"]
	if len(files) != 1 {
		t.Fatalf("multipart file count = %d, want 1", len(files))
	}
	return files[0]
}
