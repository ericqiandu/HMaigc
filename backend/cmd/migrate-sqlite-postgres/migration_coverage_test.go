package main

import (
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMigrationListCoversApplicationSchema(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyMigrationCoverage(db); err != nil {
		t.Fatal(err)
	}
}

func TestPaymentIntegrityMigrationCopiesBeforeRejectingConflictingFacts(t *testing.T) {
	source, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	target, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateBaseSchema(source); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	transactions := []model.PaymentTransaction{
		{ID: "migration-conflict-a", OrderType: model.PaymentOrderMembership, OrderID: "migration-order", Provider: model.PaymentProviderWechat, MerchantOrderNo: "migration-merchant-a", AmountCents: 100, Currency: "CNY", Status: model.PaymentTransactionCreated, CreatedAt: now, UpdatedAt: now},
		{ID: "migration-conflict-b", OrderType: model.PaymentOrderMembership, OrderID: "migration-order", Provider: model.PaymentProviderAlipay, MerchantOrderNo: "migration-merchant-b", AmountCents: 100, Currency: "CNY", Status: model.PaymentTransactionReviewRequired, CreatedAt: now, UpdatedAt: now},
	}
	if err := source.Create(&transactions).Error; err != nil {
		t.Fatal(err)
	}
	err = migrateApplicationTables(source, target, true)
	if err == nil || !strings.Contains(err.Error(), "membership/migration-order") || !strings.Contains(err.Error(), "migration-merchant-a") || !strings.Contains(err.Error(), "migration-merchant-b") {
		t.Fatalf("migration conflict error = %v", err)
	}
	var count int64
	if err := target.Model(&model.PaymentTransaction{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("copied conflict facts count = %d, err=%v", count, err)
	}
}

func TestPaymentIntegrityMigrationBackfillsProcessedWebhookFactsAndReruns(t *testing.T) {
	source, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	target, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateBaseSchema(source); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	transaction := model.PaymentTransaction{
		ID: "migration-paid-transaction", OrderType: model.PaymentOrderMembership, OrderID: "migration-paid-order",
		Provider: model.PaymentProviderWechat, MerchantOrderNo: "migration-paid-merchant", ProviderTradeNo: "migration-paid-trade",
		AmountCents: 300, Currency: "CNY", Status: model.PaymentTransactionPaid, PaidAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	event := model.PaymentWebhookEvent{
		ID: "migration-processed-event", Provider: model.PaymentProviderWechat, ProviderEventID: "migration-provider-event",
		TransactionID: transaction.ID, PayloadDigest: "migration-digest", Status: model.PaymentWebhookProcessed,
		ReceivedAt: now, ProcessedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	if err := source.Create(&transaction).Error; err != nil {
		t.Fatal(err)
	}
	if err := source.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateApplicationTables(source, target, true); err != nil {
		t.Fatalf("first migration: %v", err)
	}
	if err := migrateApplicationTables(source, target, false); err != nil {
		t.Fatalf("idempotent migration verification: %v", err)
	}
	var migrated model.PaymentWebhookEvent
	if err := target.First(&migrated, "id = ?", event.ID).Error; err != nil {
		t.Fatal(err)
	}
	if migrated.MerchantOrderNo != transaction.MerchantOrderNo || migrated.ProviderTradeNo != transaction.ProviderTradeNo || migrated.AmountCents != transaction.AmountCents || migrated.Currency != transaction.Currency || migrated.PaidAt == nil || !migrated.PaidAt.Equal(now) {
		t.Fatalf("migrated webhook facts = %#v, want transaction facts", migrated)
	}
}
