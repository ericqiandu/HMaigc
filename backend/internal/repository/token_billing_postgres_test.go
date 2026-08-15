package repository

import (
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/testsupport"

	"gorm.io/gorm"
)

func TestPostgresConcurrentTokenReservationsCannotOverdrawAccount(t *testing.T) {
	db := openPostgresTokenBillingSchema(t)
	repo := New(db)
	account := model.CreditAccount{UserID: "pg-token-user", AvailableMicrocredits: 40_000_000, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	orders := []*model.BillingOrder{
		postgresTokenOrder("pg-reserve-a", account.UserID),
		postgresTokenOrder("pg-reserve-b", account.UserID),
	}
	start := make(chan struct{})
	results := make([]error, len(orders))
	var wait sync.WaitGroup
	for index := range orders {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			results[index] = repo.ReserveBillingOrder(orders[index])
		}(index)
	}
	close(start)
	wait.Wait()
	winners := 0
	for _, err := range results {
		if err == nil {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("reservation results = %#v", results)
	}
	if err := db.First(&account, "user_id = ?", account.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if account.AvailableMicrocredits != 10_000_000 || account.ReservedMicrocredits != 30_000_000 {
		t.Fatalf("account = %#v", account)
	}
}

func TestPostgresTokenSettlementReturnsDifferenceExactlyOnce(t *testing.T) {
	db := openPostgresTokenBillingSchema(t)
	repo := New(db)
	order := postgresTokenOrder("pg-settle", "token-user")
	account := model.CreditAccount{UserID: order.UserID, AvailableMicrocredits: 100_000_000, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.ReserveBillingOrder(order); err != nil {
		t.Fatal(err)
	}
	usage := TokenUsageFact{InputTokens: 20, CachedTokens: 2, OutputTokens: 5}
	if err := repo.SettleTokenBilling(order.ID, "pg-provider-order", 6, "succeeded", 4, usage, "reported", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repo.SettleTokenBilling(order.ID, "pg-provider-order", 6, "succeeded", 4, usage, "reported", time.Now()); err != nil {
		t.Fatal(err)
	}
	assertTokenBillingSettlement(t, db, order.ID, 94_000_000, 0, 6_000_000, 24_000_000)
}

func openPostgresTokenBillingSchema(t *testing.T) *gorm.DB {
	t.Helper()
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func postgresTokenOrder(id string, userID string) *model.BillingOrder {
	now := time.Now().UTC()
	return &model.BillingOrder{
		ID: id, UserID: userID, IdempotencyKey: id, ChannelID: "pg-token-channel", ChannelModelID: "pg-token-model",
		Model: "deepseek-v4-flash", Capability: "text", Scene: "agent", BillingMode: "token_usage",
		Status: model.BillingStatusReserved, AmountMicrocredits: 30_000_000, ReservedAmountMicrocredits: 30_000_000,
		CreatedAt: now, UpdatedAt: now,
	}
}
