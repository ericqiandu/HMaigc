package repository

import (
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func TestFreezeProviderTaskRuntimeUsesActiveVersions(t *testing.T) {
	db := openProviderRepositorySQLite(t)
	now := time.Now().UTC()
	seedProviderRuntime(t, db, now, "healthy")
	if err := db.Create(&model.CreditAccount{UserID: "user", AvailableMicrocredits: 100}).Error; err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: "task", UserID: "user", Capability: "video", Status: model.TaskStatusQueued}
	order := &model.BillingOrder{ID: "order", UserID: "user", TaskID: task.ID, ChannelModelID: "channel-model", AmountMicrocredits: 10, Status: model.BillingStatusReserved}

	if err := New(db).CreateTaskWithCreditReservation(task, order, ActiveTaskPolicy{Unlimited: true}); err != nil {
		t.Fatal(err)
	}
	var stored model.Task
	if err := db.First(&stored, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ProviderAccountID != "account" || stored.ProviderEndpointVersionID != "endpoint-v1" || stored.ProviderCredentialVersionID != "key-v1" {
		t.Fatalf("frozen runtime = %#v", stored)
	}

	if err := db.Model(&model.ProviderEndpointVersion{}).Where("id = ?", "endpoint-v1").Update("status", "retired").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ProviderCredentialVersion{}).Where("id = ?", "key-v1").Update("status", "retired").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&stored, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ProviderEndpointVersionID != "endpoint-v1" || stored.ProviderCredentialVersionID != "key-v1" {
		t.Fatalf("frozen runtime changed after rotation: %#v", stored)
	}
}

func TestFreezeProviderTaskRuntimeRejectsUnhealthyCredential(t *testing.T) {
	db := openProviderRepositorySQLite(t)
	seedProviderRuntime(t, db, time.Now().UTC(), "unavailable")
	if err := db.Create(&model.CreditAccount{UserID: "user", AvailableMicrocredits: 100}).Error; err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: "task", UserID: "user", Capability: "video", Status: model.TaskStatusQueued}
	order := &model.BillingOrder{ID: "order", UserID: "user", TaskID: task.ID, ChannelModelID: "channel-model", AmountMicrocredits: 10, Status: model.BillingStatusReserved}

	if err := New(db).CreateTaskWithCreditReservation(task, order, ActiveTaskPolicy{Unlimited: true}); err == nil {
		t.Fatal("unhealthy credential was accepted")
	}
	if task.ProviderAccountID != "" || task.ProviderEndpointVersionID != "" || task.ProviderCredentialVersionID != "" {
		t.Fatalf("failed freeze mutated task: %#v", task)
	}
	var taskCount, orderCount int64
	if err := db.Model(&model.Task{}).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	var account model.CreditAccount
	if err := db.First(&account, "user_id = ?", "user").Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 || orderCount != 0 || account.AvailableMicrocredits != 100 || account.ReservedMicrocredits != 0 {
		t.Fatalf("failed freeze left persistent facts: tasks=%d orders=%d account=%#v", taskCount, orderCount, account)
	}
}

func seedProviderRuntime(t *testing.T, db *gorm.DB, now time.Time, health string) {
	t.Helper()
	rows := []any{
		&model.ProviderAccount{ID: "account", ProviderKind: "kuaizi", Name: "筷子科技", Enabled: true, CreatedAt: now, UpdatedAt: now},
		&model.ProviderEndpointVersion{ID: "endpoint-v1", ProviderAccountID: "account", BaseURL: "https://example.com", Status: "active", Version: 1, CreatedAt: now},
		&model.ProviderCredential{ID: "credential", ProviderAccountID: "account", Family: "seedance", HealthStatus: health, Enabled: true, CreatedAt: now, UpdatedAt: now},
		&model.ProviderCredentialVersion{ID: "key-v1", ProviderCredentialID: "credential", KeyCipher: "cipher", KeyFingerprint: "fingerprint", Status: "active", Version: 1, CreatedAt: now},
		&model.ChannelModel{ID: "channel-model", ChannelID: "channel", ProviderCredentialID: "credential", ModelKey: "kuaizi-seedance-2.5", Capability: "video", Enabled: true, CreatedAt: now, UpdatedAt: now},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
}
