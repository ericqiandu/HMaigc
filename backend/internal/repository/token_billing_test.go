package repository

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestSettleTokenBillingConsumesActualAndReturnsDifference(t *testing.T) {
	repo, db := openTokenBillingRepository(t)
	order := reserveTokenBillingFixture(t, repo, db, "settle-difference")

	err := repo.SettleTokenBilling(order.ID, "provider-order-1", 6, TokenUsageFact{InputTokens: 20_000, CachedTokens: 1_000, OutputTokens: 5_000}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}

	assertTokenBillingSettlement(t, db, order.ID, 94_000_000, 0, 6_000_000, 24_000_000)
	var stored model.BillingOrder
	if err := db.First(&stored, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.BillingStatusSettled || stored.AmountMicrocredits != 6_000_000 || stored.ReservedAmountMicrocredits != 30_000_000 || stored.ProviderBillingOrderID != "provider-order-1" || stored.ProviderBillingAmount != 6 || stored.InputTokens != 20_000 || stored.CachedTokens != 1_000 || stored.OutputTokens != 5_000 {
		t.Fatalf("settled order = %#v", stored)
	}
}

func TestSettleTokenBillingIsIdempotent(t *testing.T) {
	repo, db := openTokenBillingRepository(t)
	order := reserveTokenBillingFixture(t, repo, db, "settle-idempotent")
	usage := TokenUsageFact{InputTokens: 20_000, OutputTokens: 5_000}

	if err := repo.SettleTokenBilling(order.ID, "provider-order-2", 6, usage, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := repo.SettleTokenBilling(order.ID, "provider-order-2", 6, usage, time.Now().UTC().Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	assertTokenBillingSettlement(t, db, order.ID, 94_000_000, 0, 6_000_000, 24_000_000)
}

func TestSettleTokenBillingRollsBackAccountLedgerAndOrderTogether(t *testing.T) {
	repo, db := openTokenBillingRepository(t)
	order := reserveTokenBillingFixture(t, repo, db, "settle-rollback")
	if err := db.Exec(`CREATE TRIGGER reject_token_order_update BEFORE UPDATE ON billing_orders WHEN OLD.id = 'settle-rollback' BEGIN SELECT RAISE(ABORT, 'reject token order'); END`).Error; err != nil {
		t.Fatal(err)
	}

	if err := repo.SettleTokenBilling(order.ID, "provider-order-3", 6, TokenUsageFact{InputTokens: 1}, time.Now().UTC()); err == nil {
		t.Fatal("settlement succeeded despite rejected order update")
	}

	var account model.CreditAccount
	if err := db.First(&account, "user_id = ?", order.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if account.AvailableMicrocredits != 70_000_000 || account.ReservedMicrocredits != 30_000_000 {
		t.Fatalf("account changed after rollback = %#v", account)
	}
	var stored model.BillingOrder
	if err := db.First(&stored, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.BillingStatusReserved || stored.AmountMicrocredits != 30_000_000 {
		t.Fatalf("order changed after rollback = %#v", stored)
	}
	var finalLedgerCount int64
	if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ? AND type IN ?", order.ID, []model.CreditLedgerType{model.CreditLedgerConsume, model.CreditLedgerRelease}).Count(&finalLedgerCount).Error; err != nil {
		t.Fatal(err)
	}
	if finalLedgerCount != 0 {
		t.Fatalf("final ledger count after rollback = %d", finalLedgerCount)
	}
}

func TestSettleTokenBillingKeepsFundsFrozenWhenActualExceedsReservation(t *testing.T) {
	repo, db := openTokenBillingRepository(t)
	order := reserveTokenBillingFixture(t, repo, db, "settle-over-reservation")

	if err := repo.SettleTokenBilling(order.ID, "provider-order-over", 31, TokenUsageFact{InputTokens: 1}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	var account model.CreditAccount
	if err := db.First(&account, "user_id = ?", order.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if account.AvailableMicrocredits != 70_000_000 || account.ReservedMicrocredits != 30_000_000 {
		t.Fatalf("over-reservation account = %#v", account)
	}
	var stored model.BillingOrder
	if err := db.First(&stored, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.BillingStatusUncertain || stored.ProviderBillingStatus != "amount_exceeds_reservation" || stored.ProviderBillingAmount != 31 {
		t.Fatalf("over-reservation order = %#v", stored)
	}
}

func TestClaimTokenBillingReconciliationsLeasesDueOrders(t *testing.T) {
	repo, db := openTokenBillingRepository(t)
	now := time.Now().UTC()
	due := model.BillingOrder{
		ID: "reconcile-due", UserID: "token-user", IdempotencyKey: "reconcile-due", BillingMode: "token_usage",
		Status: model.BillingStatusRunning, AmountMicrocredits: 30_000_000, ReservedAmountMicrocredits: 30_000_000,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&due).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkTokenBillingForReconciliation(due.ID, "provider-task-due", "账单暂未生成", now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}

	claimed, err := repo.ClaimTokenBillingReconciliations("worker-a", now, time.Minute, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 || claimed[0].ID != due.ID || claimed[0].ReconcileLeaseOwner != "worker-a" || claimed[0].ReconcileLeaseToken == "" || claimed[0].ReconcileAttempts != 1 {
		t.Fatalf("claimed orders = %#v", claimed)
	}
	if second, err := repo.ClaimTokenBillingReconciliations("worker-b", now, time.Minute, 10); err != nil || len(second) != 0 {
		t.Fatalf("second claim = %#v, %v", second, err)
	}
	if err := repo.RescheduleTokenBillingReconciliation(due.ID, "worker-a", "wrong-token", "still pending", now.Add(time.Minute)); err == nil {
		t.Fatal("stale reconciliation token rescheduled order")
	}
	if err := repo.RescheduleTokenBillingReconciliation(due.ID, "worker-a", claimed[0].ReconcileLeaseToken, "still pending", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if immediate, err := repo.ClaimTokenBillingReconciliations("worker-b", now, time.Minute, 10); err != nil || len(immediate) != 0 {
		t.Fatalf("future order reclaimed early = %#v, %v", immediate, err)
	}
}

func TestConcurrentTokenReservationsCannotOverdrawAccount(t *testing.T) {
	repo, db := openTokenBillingRepository(t)
	now := time.Now().UTC()
	account := model.CreditAccount{UserID: "concurrent-token-user", AvailableMicrocredits: 100_000_000, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	orders := []*model.BillingOrder{
		{ID: "concurrent-token-a", UserID: account.UserID, IdempotencyKey: "concurrent-token-a", BillingMode: "token_usage", AmountMicrocredits: 60_000_000, ReservedAmountMicrocredits: 60_000_000, Status: model.BillingStatusReserved, CreatedAt: now, UpdatedAt: now},
		{ID: "concurrent-token-b", UserID: account.UserID, IdempotencyKey: "concurrent-token-b", BillingMode: "token_usage", AmountMicrocredits: 60_000_000, ReservedAmountMicrocredits: 60_000_000, Status: model.BillingStatusReserved, CreatedAt: now, UpdatedAt: now},
	}
	start := make(chan struct{})
	errorsByOrder := make([]error, len(orders))
	var wait sync.WaitGroup
	for index := range orders {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			errorsByOrder[index] = repo.ReserveBillingOrder(orders[index])
		}(index)
	}
	close(start)
	wait.Wait()

	succeeded := 0
	insufficient := 0
	for _, err := range errorsByOrder {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrInsufficientCredits):
			insufficient++
		default:
			t.Fatalf("unexpected reservation error: %v", err)
		}
	}
	if succeeded != 1 || insufficient != 1 {
		t.Fatalf("reservation results: succeeded=%d insufficient=%d errors=%v", succeeded, insufficient, errorsByOrder)
	}
	if err := db.First(&account, "user_id = ?", account.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if account.AvailableMicrocredits != 40_000_000 || account.ReservedMicrocredits != 60_000_000 {
		t.Fatalf("account after concurrent reservations = %#v", account)
	}
}

func TestSettleTeamTokenBillingReturnsDifferenceToTeamAccount(t *testing.T) {
	repo, db := openTokenBillingRepository(t)
	now := time.Now().UTC()
	team := model.Team{ID: "token-team", OwnerUserID: "token-team-user", Name: "Token Team", Status: model.TeamStatusActive, CreatedAt: now, UpdatedAt: now}
	member := model.TeamMember{ID: "token-team-member", TeamID: team.ID, UserID: team.OwnerUserID, Role: model.TeamMemberRoleOwner, Status: model.TeamMemberStatusActive, CreatedAt: now, UpdatedAt: now}
	account := model.TeamCreditAccount{TeamID: team.ID, AvailableMicrocredits: 100_000_000, CreatedAt: now, UpdatedAt: now}
	for _, row := range []any{&team, &member, &account} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	order := &model.BillingOrder{
		ID: "team-token-order", UserID: member.UserID, TeamID: team.ID, IdempotencyKey: "team-token-order",
		BillingMode: "token_usage", AmountMicrocredits: 30_000_000, ReservedAmountMicrocredits: 30_000_000,
		Status: model.BillingStatusReserved, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.ReserveBillingOrder(order); err != nil {
		t.Fatal(err)
	}
	if err := repo.SettleTokenBilling(order.ID, "provider-team-order", 6, TokenUsageFact{InputTokens: 1}, now); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&account, "team_id = ?", team.ID).Error; err != nil {
		t.Fatal(err)
	}
	if account.AvailableMicrocredits != 94_000_000 || account.ReservedMicrocredits != 0 {
		t.Fatalf("team account = %#v", account)
	}
	var finalLedgerCount int64
	if err := db.Model(&model.TeamCreditLedgerEntry{}).Where("billing_order_id = ? AND type IN ?", order.ID, []model.CreditLedgerType{model.CreditLedgerConsume, model.CreditLedgerRelease}).Count(&finalLedgerCount).Error; err != nil {
		t.Fatal(err)
	}
	if finalLedgerCount != 2 {
		t.Fatalf("team final ledger count = %d", finalLedgerCount)
	}
}

func openTokenBillingRepository(t *testing.T) (*Repository, *gorm.DB) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "token-billing.db") + "?_busy_timeout=5000&_journal_mode=WAL"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return New(db), db
}

func reserveTokenBillingFixture(t *testing.T, repo *Repository, db *gorm.DB, orderID string) *model.BillingOrder {
	t.Helper()
	account := model.CreditAccount{UserID: "token-user", AvailableMicrocredits: 100_000_000}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	order := &model.BillingOrder{
		ID: orderID, UserID: account.UserID, IdempotencyKey: orderID, ChannelID: "token-channel", ChannelModelID: "token-model",
		Model: "deepseek-v4-flash", Capability: "text", Scene: "agent", BillingMode: "token_usage", Status: model.BillingStatusReserved,
		AmountMicrocredits: 30_000_000, ReservedAmountMicrocredits: 30_000_000, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := repo.ReserveBillingOrder(order); err != nil {
		t.Fatal(err)
	}
	return order
}

func assertTokenBillingSettlement(t *testing.T, db *gorm.DB, orderID string, wantAvailable int64, wantReserved int64, wantConsumed int64, wantReleased int64) {
	t.Helper()
	var account model.CreditAccount
	if err := db.First(&account, "user_id = ?", "token-user").Error; err != nil {
		t.Fatal(err)
	}
	if account.AvailableMicrocredits != wantAvailable || account.ReservedMicrocredits != wantReserved {
		t.Fatalf("account = %#v, want available=%d reserved=%d", account, wantAvailable, wantReserved)
	}
	var ledgers []model.CreditLedgerEntry
	if err := db.Where("billing_order_id = ? AND type IN ?", orderID, []model.CreditLedgerType{model.CreditLedgerConsume, model.CreditLedgerRelease}).Order("type asc").Find(&ledgers).Error; err != nil {
		t.Fatal(err)
	}
	if len(ledgers) != 2 {
		t.Fatalf("final ledgers = %#v", ledgers)
	}
	amounts := map[model.CreditLedgerType]int64{}
	for _, ledger := range ledgers {
		amounts[ledger.Type] += ledger.AmountMicrocredits
	}
	if amounts[model.CreditLedgerConsume] != -wantConsumed || amounts[model.CreditLedgerRelease] != wantReleased {
		t.Fatalf("final ledger amounts = %#v", amounts)
	}
}
