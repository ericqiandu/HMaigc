package service

import (
	"encoding/json"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
)

func TestMembershipStorefrontReturnsBackendManagedPresentationAndPlans(t *testing.T) {
	svc, _ := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}

	storefront, err := svc.MembershipStorefront()
	if err != nil {
		t.Fatal(err)
	}
	if !storefront.Presentation.Promotion.Enabled || storefront.Presentation.Promotion.EndsAt.IsZero() {
		t.Fatalf("unexpected promotion: %#v", storefront.Presentation.Promotion)
	}
	if len(storefront.Plans) == 0 || len(storefront.Presentation.GenerationColumns) == 0 || len(storefront.Presentation.GenerationSections) == 0 || len(storefront.Presentation.FAQs) == 0 {
		t.Fatalf("storefront is incomplete: %#v", storefront)
	}
	if storefront.ServerNow.IsZero() {
		t.Fatal("serverNow must be populated")
	}
}

func TestUpdateMembershipStorefrontPersistsConfigurationAndAuditAtomically(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	admin := &model.User{ID: "admin-storefront", Username: "admin-storefront", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	if err := db.Create(admin).Error; err != nil {
		t.Fatal(err)
	}
	value := defaultMembershipStorefrontSetting()
	value.Promotion.Title = "  新会员活动  "
	value.Promotion.EndsAt = time.Now().Add(48 * time.Hour)
	value.FAQs[0].Question = "  新问题  "

	updated, err := svc.UpdateMembershipStorefront(admin, value)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Presentation.Promotion.Title != "新会员活动" || updated.Presentation.FAQs[0].Question != "新问题" || updated.UpdatedAt == nil {
		t.Fatalf("unexpected updated storefront: %#v", updated)
	}

	var setting model.SystemSetting
	if err := db.First(&setting, "key = ?", membershipStorefrontSettingKey).Error; err != nil {
		t.Fatal(err)
	}
	var stored MembershipStorefrontSetting
	if err := json.Unmarshal([]byte(setting.ValueJSON), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Promotion.Title != "新会员活动" || setting.UpdatedBy != admin.ID {
		t.Fatalf("unexpected stored setting: %#v %#v", setting, stored)
	}

	var audit model.AdminAuditEvent
	if err := db.First(&audit, "action = ?", "membership_storefront.update").Error; err != nil {
		t.Fatal(err)
	}
	if audit.ActorUserID != admin.ID || audit.TargetID != membershipStorefrontSettingKey {
		t.Fatalf("unexpected audit: %#v", audit)
	}
}

func TestUpdateMembershipStorefrontRejectsInvalidMatrixAndNonAdmin(t *testing.T) {
	svc, _ := newMembershipTestService(t)
	user := &model.User{ID: "storefront-user", Role: model.UserRoleUser, Status: model.UserStatusActive}
	value := defaultMembershipStorefrontSetting()
	if _, err := svc.UpdateMembershipStorefront(user, value); err == nil {
		t.Fatal("non-admin update unexpectedly succeeded")
	}

	admin := &model.User{ID: "storefront-admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	value.GenerationSections[0].Rows[0].Values = []string{"1"}
	if _, err := svc.UpdateMembershipStorefront(admin, value); err == nil {
		t.Fatal("invalid generation matrix unexpectedly succeeded")
	}
}

func TestMembershipStorefrontRejectsMissingPlanHighlightCoverage(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	value := defaultMembershipStorefrontSetting()
	value.PlanHighlights = value.PlanHighlights[:1]
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SystemSetting{Key: membershipStorefrontSettingKey, ValueJSON: string(encoded)}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MembershipStorefront(); err == nil {
		t.Fatal("storefront with missing plan highlight unexpectedly succeeded")
	}
}
