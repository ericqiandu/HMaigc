package service

import (
	"bytes"
	"encoding/base64"
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
	defaults, err := svc.PublicSiteSetting()
	if err != nil {
		t.Fatal(err)
	}
	if defaults.SiteName != "HMaigc" || defaults.FooterCopyright == "" || defaults.LogoURL != "" {
		t.Fatalf("unexpected defaults: %#v", defaults)
	}

	admin := &model.User{ID: "site-admin", Role: model.UserRoleAdmin}
	updated, err := svc.UpdateSiteSetting(admin, SiteSettingRequest{
		SiteName:                         "弘梦 AIGC",
		FooterCopyright:                  "© 弘梦科技",
		ICPRegistrationNumber:            "蜀ICP备2026000000号-1",
		ICPRegistrationURL:               "https://beian.miit.gov.cn/",
		PublicSecurityRegistrationNumber: "川公网安备51000000000000号",
		PublicSecurityRegistrationURL:    "http://www.beian.gov.cn/portal/registerSystemInfo?recordcode=51000000000000",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.SiteName != "弘梦 AIGC" || updated.ICPRegistrationNumber == "" || updated.PublicSecurityRegistrationNumber == "" {
		t.Fatalf("unexpected updated setting: %#v", updated)
	}
	updated, err = svc.UpdateLegalContentSetting(admin, LegalContentSettingRequest{UserAgreement: "<p>第一条 用户权利与义务</p>", PrivacyPolicy: "<p>第一条 信息处理规则</p>"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.UserAgreement == "" || updated.PrivacyPolicy == "" {
		t.Fatalf("unexpected legal setting: %#v", updated)
	}
	legalAgreement := updated.UserAgreement
	legalPrivacy := updated.PrivacyPolicy
	updated, err = svc.UpdateSiteSetting(admin, SiteSettingRequest{SiteName: "弘梦 AIGC 2", FooterCopyright: "© 弘梦科技"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.UserAgreement != legalAgreement || updated.PrivacyPolicy != legalPrivacy {
		t.Fatalf("brand update overwrote legal content: %#v", updated)
	}
	reloaded, err := svc.PublicSiteSetting()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.SiteName != updated.SiteName || reloaded.FooterCopyright != updated.FooterCopyright {
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
	if _, err := svc.UpdateSiteSetting(admin, SiteSettingRequest{SiteName: "弘梦", ICPRegistrationURL: "javascript:alert(1)"}); err == nil {
		t.Fatal("unsafe registration URL should be rejected")
	}
	if _, err := svc.UpdateSiteSetting(admin, SiteSettingRequest{SiteName: "弘梦", PublicSecurityRegistrationURL: "https://user@example.com/path"}); err == nil {
		t.Fatal("registration URL with credentials should be rejected")
	}
	if _, err := svc.UpdateSiteSetting(admin, SiteSettingRequest{SiteName: "弘梦", ICPRegistrationURL: "https://beian.miit.gov.cn/"}); err == nil {
		t.Fatal("registration URL without registration number should be rejected")
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

func TestSiteSettingDoesNotHideCorruptedStoredData(t *testing.T) {
	svc, db := newSiteSettingTestService(t)
	if err := db.Create(&model.SystemSetting{Key: siteSettingKey, ValueJSON: "not-json"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublicSiteSetting(); err == nil {
		t.Fatal("corrupted setting should fail explicitly")
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
