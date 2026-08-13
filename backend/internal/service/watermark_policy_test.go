package service

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPublishWatermarkPolicyRequiresAdminSafeRichTextAndStrictHTTPSURL(t *testing.T) {
	svc, _, admin, user := openWatermarkPolicyServiceFixture(t)
	valid := PublishWatermarkPolicyRequest{ManagementRuleRichText: "<p>AI 生成内容水印管理规则</p>", WatermarkPolicyURL: "https://example.com/watermark-policy"}
	if _, err := svc.PublishWatermarkPolicy(user, valid); authStatus(err) != http.StatusForbidden {
		t.Fatalf("non-admin publish error = %v", err)
	}
	invalid := []PublishWatermarkPolicyRequest{
		{WatermarkPolicyURL: valid.WatermarkPolicyURL},
		{ManagementRuleRichText: valid.ManagementRuleRichText},
		{ManagementRuleRichText: "<p>   </p>", WatermarkPolicyURL: valid.WatermarkPolicyURL},
		{ManagementRuleRichText: "<script>alert(1)</script>", WatermarkPolicyURL: valid.WatermarkPolicyURL},
		{ManagementRuleRichText: valid.ManagementRuleRichText, WatermarkPolicyURL: "http://example.com/policy"},
		{ManagementRuleRichText: valid.ManagementRuleRichText, WatermarkPolicyURL: "https://user:pass@example.com/policy"},
		{ManagementRuleRichText: valid.ManagementRuleRichText, WatermarkPolicyURL: "https://example.com/policy#fragment"},
		{ManagementRuleRichText: valid.ManagementRuleRichText, WatermarkPolicyURL: "https://example.com/\npolicy"},
	}
	for index, request := range invalid {
		if _, err := svc.PublishWatermarkPolicy(admin, request); authStatus(err) != http.StatusBadRequest {
			t.Fatalf("invalid request %d error = %v", index, err)
		}
	}
}

func TestWatermarkPreferenceStatusTracksImmutablePublication(t *testing.T) {
	svc, db, admin, user := openWatermarkPolicyServiceFixture(t)
	initial, err := svc.WatermarkPreference(user)
	if err != nil {
		t.Fatal(err)
	}
	if initial.Status != WatermarkPreferencePolicyUnavailable || initial.RemoveWatermark || initial.CanEnable || initial.CurrentPolicy != nil {
		t.Fatalf("initial preference = %#v", initial)
	}

	v1, err := svc.PublishWatermarkPolicy(admin, PublishWatermarkPolicyRequest{ManagementRuleRichText: "<p>规则一</p>", WatermarkPolicyURL: "https://example.com/v1"})
	if err != nil {
		t.Fatal(err)
	}
	active, err := svc.UpdateWatermarkPreference(user, UpdateWatermarkPreferenceRequest{RemoveWatermark: true, PublicationID: v1.ID})
	if err != nil {
		t.Fatal(err)
	}
	if active.Status != WatermarkPreferenceActive || !active.RemoveWatermark || !active.CanEnable || active.CurrentPolicy == nil || active.CurrentPolicy.ID != v1.ID {
		t.Fatalf("active preference = %#v", active)
	}
	if _, err := svc.UpdateWatermarkPreference(user, UpdateWatermarkPreferenceRequest{RemoveWatermark: true, PublicationID: v1.ID}); err != nil {
		t.Fatal(err)
	}
	var consentCount int64
	if err := db.Model(&model.UserPolicyConsent{}).Count(&consentCount).Error; err != nil || consentCount != 1 {
		t.Fatalf("consent count = %d, err=%v", consentCount, err)
	}
	var eventCount int64
	if err := db.Model(&model.UserWatermarkPreferenceEvent{}).Count(&eventCount).Error; err != nil || eventCount != 2 {
		t.Fatalf("event count = %d, err=%v", eventCount, err)
	}

	v2, err := svc.PublishWatermarkPolicy(admin, PublishWatermarkPolicyRequest{ManagementRuleRichText: "<p>规则二</p>", WatermarkPolicyURL: "https://example.com/v2"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := svc.WatermarkPreference(user)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != WatermarkPreferencePolicyUpdated || updated.RemoveWatermark || !updated.CanEnable || updated.CurrentPolicy == nil || updated.CurrentPolicy.ID != v2.ID {
		t.Fatalf("updated preference = %#v", updated)
	}
	_, err = svc.UpdateWatermarkPreference(user, UpdateWatermarkPreferenceRequest{RemoveWatermark: true, PublicationID: v1.ID})
	if authStatus(err) != http.StatusConflict {
		t.Fatalf("stale preference error = %v", err)
	}
	var failedEvent model.UserWatermarkPreferenceEvent
	if err := db.Where("user_id = ? AND result_status = ?", user.ID, "version_conflict").First(&failedEvent).Error; err != nil {
		t.Fatal(err)
	}
	if failedEvent.RemoveWatermark != true || failedEvent.PolicyPublicationID != v1.ID {
		t.Fatalf("failed preference event = %#v", failedEvent)
	}
	disabled, err := svc.UpdateWatermarkPreference(user, UpdateWatermarkPreferenceRequest{RemoveWatermark: false, PublicationID: v1.ID})
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Status != WatermarkPreferenceDisabled || disabled.RemoveWatermark {
		t.Fatalf("disabled preference = %#v", disabled)
	}
}

func TestUnavailableWatermarkPolicyRejectionIsAuditedWithoutChangingPreference(t *testing.T) {
	svc, db, _, user := openWatermarkPolicyServiceFixture(t)
	_, err := svc.UpdateWatermarkPreference(user, UpdateWatermarkPreferenceRequest{RemoveWatermark: true, PublicationID: "missing-publication"})
	if authStatus(err) != http.StatusBadRequest {
		t.Fatalf("unavailable policy error = %v", err)
	}
	var failedEvent model.UserWatermarkPreferenceEvent
	if err := db.Where("user_id = ? AND result_status = ?", user.ID, "policy_unavailable").First(&failedEvent).Error; err != nil {
		t.Fatal(err)
	}
	var preferenceCount int64
	if err := db.Model(&model.UserWatermarkPreference{}).Where("user_id = ?", user.ID).Count(&preferenceCount).Error; err != nil {
		t.Fatal(err)
	}
	if preferenceCount != 0 {
		t.Fatalf("unavailable policy changed preference rows = %d", preferenceCount)
	}
}

func TestWatermarkPolicyPublicationAuditOmitsRichText(t *testing.T) {
	svc, db, admin, _ := openWatermarkPolicyServiceFixture(t)
	const sentinel = "sensitive-rule-sentinel"
	publication, err := svc.PublishWatermarkPolicy(admin, PublishWatermarkPolicyRequest{ManagementRuleRichText: "<p>" + sentinel + "</p>", WatermarkPolicyURL: "https://example.com/policy"})
	if err != nil {
		t.Fatal(err)
	}
	var audit model.AdminAuditEvent
	if err := db.First(&audit, "action = ?", "watermark_policy.publish").Error; err != nil {
		t.Fatal(err)
	}
	if audit.TargetID != publication.ID || watermarkTestContains(audit.MetadataJSON, sentinel) || watermarkTestContains(audit.Summary, sentinel) {
		t.Fatalf("unsafe publication audit = %#v", audit)
	}
}

func TestStaleWatermarkPreferenceWritesSafeStructuredFailureLog(t *testing.T) {
	svc, _, admin, user := openWatermarkPolicyServiceFixture(t)
	v1, err := svc.PublishWatermarkPolicy(admin, PublishWatermarkPolicyRequest{ManagementRuleRichText: "<p>规则一</p>", WatermarkPolicyURL: "https://example.com/v1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublishWatermarkPolicy(admin, PublishWatermarkPolicyRequest{ManagementRuleRichText: "<p>sensitive-policy-body</p>", WatermarkPolicyURL: "https://example.com/v2"}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	originalWriter := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(originalWriter) })

	_, err = svc.UpdateWatermarkPreference(user, UpdateWatermarkPreferenceRequest{RemoveWatermark: true, PublicationID: v1.ID})
	if authStatus(err) != http.StatusConflict {
		t.Fatalf("stale preference error = %v", err)
	}
	logged := output.String()
	for _, required := range []string{"event=watermark_preference_update_failed", "user_id=" + user.ID, "reason=version_conflict"} {
		if !strings.Contains(logged, required) {
			t.Fatalf("failure log missing %q: %s", required, logged)
		}
	}
	for _, forbidden := range []string{"sensitive-policy-body", "<p>", "https://example.com/v2"} {
		if strings.Contains(logged, forbidden) {
			t.Fatalf("failure log leaked %q: %s", forbidden, logged)
		}
	}
}

func openWatermarkPolicyServiceFixture(t *testing.T) (*Service, *gorm.DB, *model.User, *model.User) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "watermark-policy.db")+"?_busy_timeout=5000"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	admin := &model.User{ID: "watermark-admin", Username: "watermark-admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}
	user := &model.User{ID: "watermark-user", Username: "watermark-user", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&[]*model.User{admin, user}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	return New(repository.New(db), t.TempDir()), db, admin, user
}

func authStatus(err error) int {
	var authErr *AuthError
	if errors.As(err, &authErr) {
		return authErr.Status
	}
	return 0
}

func watermarkTestContains(value string, needle string) bool {
	return len(needle) > 0 && strings.Contains(value, needle)
}
