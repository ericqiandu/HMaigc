package service

import (
	"encoding/json"
	"testing"

	"infinite-canvas/backend/internal/model"
)

func TestProductRolloutsRequireExplicitValidConfiguration(t *testing.T) {
	svc, db := newSiteSettingTestService(t)
	user := &model.User{ID: "user-1", Role: model.UserRoleUser}
	if _, err := svc.ProductRollouts(user); err == nil {
		t.Fatal("missing rollout configuration must fail explicitly")
	}
	if err := db.Create(&model.SystemSetting{Key: productRolloutSettingKey, ValueJSON: `{"canvasToolRegistryV2":{"percentage":101,"adminAllowlist":[]}}`}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ProductRollouts(user); err == nil {
		t.Fatal("invalid rollout percentage must fail explicitly")
	}
}

func TestProductRolloutsUseAllowlistAndStablePercentageBuckets(t *testing.T) {
	svc, _ := newSiteSettingTestService(t)
	admin := &model.User{ID: "admin-1", Role: model.UserRoleAdmin}
	config := ProductRolloutSetting{
		CanvasToolRegistryV2:    ProductRolloutRule{Percentage: 0, AdminAllowlist: []string{"user-allowed"}},
		CanvasMediaToolsV2:      ProductRolloutRule{Percentage: 100},
		CanvasProductionNodesV2: ProductRolloutRule{Percentage: 0},
		CreativeWorkspaceV2:     ProductRolloutRule{Percentage: 0},
		SkillLibraryV2:          ProductRolloutRule{Percentage: 0},
	}
	if _, err := svc.UpdateProductRollouts(admin, config); err != nil {
		t.Fatal(err)
	}
	allowed, err := svc.ProductRollouts(&model.User{ID: "user-allowed", Role: model.UserRoleUser})
	if err != nil {
		t.Fatal(err)
	}
	if !allowed.CanvasToolRegistryV2 || !allowed.CanvasMediaToolsV2 {
		t.Fatalf("allowlist and 100 percent rules must enable flags: %#v", allowed)
	}
	blocked, err := svc.ProductRollouts(&model.User{ID: "user-blocked", Role: model.UserRoleUser})
	if err != nil {
		t.Fatal(err)
	}
	if blocked.CanvasToolRegistryV2 || !blocked.CanvasMediaToolsV2 {
		t.Fatalf("zero percent rule must stay disabled outside allowlist: %#v", blocked)
	}
	repeated, err := svc.ProductRollouts(&model.User{ID: "user-blocked", Role: model.UserRoleUser})
	if err != nil || *repeated != *blocked {
		t.Fatalf("rollout decision must be stable: %#v, %v", repeated, err)
	}
}

func TestUpdateProductRolloutsRequiresAdminAndStoresCompleteContract(t *testing.T) {
	svc, db := newSiteSettingTestService(t)
	config := ProductRolloutSetting{
		CanvasToolRegistryV2:    ProductRolloutRule{Percentage: 25, AdminAllowlist: []string{" user-1 ", "user-1", "user-2"}},
		CanvasMediaToolsV2:      ProductRolloutRule{Percentage: 0},
		CanvasProductionNodesV2: ProductRolloutRule{Percentage: 0},
		CreativeWorkspaceV2:     ProductRolloutRule{Percentage: 0},
		SkillLibraryV2:          ProductRolloutRule{Percentage: 0},
	}
	if _, err := svc.UpdateProductRollouts(&model.User{ID: "user", Role: model.UserRoleUser}, config); err == nil {
		t.Fatal("non-admin update must fail")
	}
	stored, err := svc.UpdateProductRollouts(&model.User{ID: "admin", Role: model.UserRoleAdmin}, config)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.CanvasToolRegistryV2.AdminAllowlist) != 2 || stored.CanvasToolRegistryV2.AdminAllowlist[0] != "user-1" {
		t.Fatalf("allowlist must be trimmed and deduplicated: %#v", stored.CanvasToolRegistryV2.AdminAllowlist)
	}
	var setting model.SystemSetting
	if err := db.Where("key = ?", productRolloutSettingKey).First(&setting).Error; err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal([]byte(setting.ValueJSON), &decoded); err != nil || len(decoded) != 5 {
		t.Fatalf("stored rollout contract must contain exactly five flags: %s, %v", setting.ValueJSON, err)
	}
}

func TestParseProductRolloutSettingJSONRejectsMissingAndUnknownFields(t *testing.T) {
	valid := `{"canvasToolRegistryV2":{"percentage":0,"adminAllowlist":[]},"canvasMediaToolsV2":{"percentage":0,"adminAllowlist":[]},"canvasProductionNodesV2":{"percentage":0,"adminAllowlist":[]},"creativeWorkspaceV2":{"percentage":0,"adminAllowlist":[]},"skillLibraryV2":{"percentage":0,"adminAllowlist":[]}}`
	if _, err := ParseProductRolloutSettingJSON([]byte(valid)); err != nil {
		t.Fatalf("valid complete rollout contract rejected: %v", err)
	}
	if _, err := ParseProductRolloutSettingJSON([]byte(`{"canvasToolRegistryV2":{"percentage":0,"adminAllowlist":[]}}`)); err == nil {
		t.Fatal("missing flags must fail")
	}
	if _, err := ParseProductRolloutSettingJSON([]byte(`{"canvasToolRegistryV2":{"percentage":0,"adminAllowlist":[],"fallback":true},"canvasMediaToolsV2":{"percentage":0,"adminAllowlist":[]},"canvasProductionNodesV2":{"percentage":0,"adminAllowlist":[]},"creativeWorkspaceV2":{"percentage":0,"adminAllowlist":[]},"skillLibraryV2":{"percentage":0,"adminAllowlist":[]}}`)); err == nil {
		t.Fatal("unknown rule fields must fail")
	}
}
