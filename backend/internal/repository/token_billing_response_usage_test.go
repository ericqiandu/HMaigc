package repository

import (
	"errors"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
)

func TestTokenBillingResponseUsageSettlesPersonalAndReleasesDifference(t *testing.T) {
	repo, db := openTokenBillingRepository(t)
	order := reserveTokenBillingFixture(t, repo, db, "response-personal")
	fact := responseUsageSettlementFact("resp-personal", 6_000_000, TokenUsageFact{InputTokens: 20_000, CachedTokens: 1_000, OutputTokens: 5_000})
	if err := repo.SettleTokenBillingFromResponseUsage(order.ID, fact); err != nil {
		t.Fatal(err)
	}
	assertTokenBillingSettlement(t, db, order.ID, 94_000_000, 0, 6_000_000, 24_000_000)
	var stored model.BillingOrder
	if err := db.First(&stored, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ProviderRequestID != fact.ProviderRequestID || stored.ProviderBillingUnit != "microcredit" || stored.ProviderBillingAmount != fact.AmountMicrocredits || stored.ProviderBillingStatus != "succeeded" || stored.TokenUsageStatus != "reported" {
		t.Fatalf("direct response settlement = %#v", stored)
	}
}

func TestTokenBillingResponseUsageSettlesTeam(t *testing.T) {
	repo, db := openTokenBillingRepository(t)
	now := time.Now().UTC()
	team := model.Team{ID: "response-team", OwnerUserID: "team-owner", Name: "Team", CreatedAt: now, UpdatedAt: now}
	member := model.TeamMember{ID: "response-team-owner", TeamID: team.ID, UserID: team.OwnerUserID, Role: model.TeamMemberRoleOwner, Status: model.TeamMemberStatusActive, CreatedAt: now, UpdatedAt: now}
	account := model.TeamCreditAccount{TeamID: team.ID, AvailableMicrocredits: 100_000_000, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&team).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&member).Error; err != nil {
		t.Fatal(err)
	}
	order := &model.BillingOrder{
		ID: "response-team-order", UserID: team.OwnerUserID, TeamID: team.ID, IdempotencyKey: "response-team-order",
		ChannelID: "direct-channel", ChannelModelID: "vision-model", Model: "vision", Capability: "vision", Scene: "agent",
		BillingMode: "token_usage", AmountMicrocredits: 30_000_000, ReservedAmountMicrocredits: 30_000_000,
		Status: model.BillingStatusReserved, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.ReserveBillingOrder(order); err != nil {
		t.Fatal(err)
	}
	fact := responseUsageSettlementFact("resp-team", 6_000_000, TokenUsageFact{InputTokens: 20_000, OutputTokens: 5_000})
	if err := repo.SettleTokenBillingFromResponseUsage(order.ID, fact); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&account, "team_id = ?", team.ID).Error; err != nil {
		t.Fatal(err)
	}
	if account.AvailableMicrocredits != 94_000_000 || account.ReservedMicrocredits != 0 {
		t.Fatalf("team account = %#v", account)
	}
}

func TestTokenBillingResponseUsageChargesAboveReservationWhenBalanceAllows(t *testing.T) {
	repo, db := openTokenBillingRepository(t)
	now := time.Now().UTC()
	account := model.CreditAccount{UserID: "response-over-user", AvailableMicrocredits: 100_000_000, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	order := &model.BillingOrder{
		ID: "response-over-order", UserID: account.UserID, IdempotencyKey: "response-over-order", ChannelID: "direct-channel",
		ChannelModelID: "vision-model", Model: "vision", Capability: "vision", Scene: "agent", BillingMode: "token_usage",
		AmountMicrocredits: 5_000_000, ReservedAmountMicrocredits: 5_000_000, Status: model.BillingStatusReserved, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.ReserveBillingOrder(order); err != nil {
		t.Fatal(err)
	}
	fact := responseUsageSettlementFact("resp-over", 6_000_000, TokenUsageFact{InputTokens: 20_000, OutputTokens: 5_000})
	if err := repo.SettleTokenBillingFromResponseUsage(order.ID, fact); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&account, "user_id = ?", account.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if account.AvailableMicrocredits != 94_000_000 || account.ReservedMicrocredits != 0 {
		t.Fatalf("over-reservation account = %#v", account)
	}
	var consume model.CreditLedgerEntry
	if err := db.Where("billing_order_id = ? AND type = ?", order.ID, model.CreditLedgerConsume).First(&consume).Error; err != nil {
		t.Fatal(err)
	}
	if consume.AvailableDeltaMicrocredits != -1_000_000 || consume.ReservedDeltaMicrocredits != -5_000_000 {
		t.Fatalf("over-reservation ledger = %#v", consume)
	}
}

func TestTokenBillingResponseUsageReplayIsExactAndConcurrent(t *testing.T) {
	repo, db := openTokenBillingRepository(t)
	order := reserveTokenBillingFixture(t, repo, db, "response-replay")
	fact := responseUsageSettlementFact("resp-replay", 6_000_000, TokenUsageFact{InputTokens: 20_000, OutputTokens: 5_000})
	start := make(chan struct{})
	errorsByWorker := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			errorsByWorker <- repo.SettleTokenBillingFromResponseUsage(order.ID, fact)
		}()
	}
	close(start)
	workers.Wait()
	close(errorsByWorker)
	for err := range errorsByWorker {
		if err != nil {
			t.Fatalf("concurrent replay error = %v", err)
		}
	}
	if err := repo.SettleTokenBillingFromResponseUsage(order.ID, fact); err != nil {
		t.Fatalf("exact replay error = %v", err)
	}
	conflict := fact
	conflict.Usage.OutputTokens++
	if err := repo.SettleTokenBillingFromResponseUsage(order.ID, conflict); !errors.Is(err, ErrBillingStateConflict) {
		t.Fatalf("conflicting replay error = %v", err)
	}
	var consumeCount int64
	if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ? AND type = ?", order.ID, model.CreditLedgerConsume).Count(&consumeCount).Error; err != nil {
		t.Fatal(err)
	}
	if consumeCount != 1 {
		t.Fatalf("consume ledger count = %d", consumeCount)
	}
}

func TestTokenBillingResponseUsageRollsBackWhenLedgerFails(t *testing.T) {
	repo, db := openTokenBillingRepository(t)
	order := reserveTokenBillingFixture(t, repo, db, "response-rollback")
	if err := db.Exec(`CREATE TRIGGER reject_response_usage_ledger BEFORE INSERT ON credit_ledger_entries WHEN NEW.billing_order_id = 'response-rollback' BEGIN SELECT RAISE(ABORT, 'ledger unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	fact := responseUsageSettlementFact("resp-rollback", 6_000_000, TokenUsageFact{InputTokens: 20_000, OutputTokens: 5_000})
	if err := repo.SettleTokenBillingFromResponseUsage(order.ID, fact); err == nil {
		t.Fatal("ledger failure was accepted")
	}
	var storedOrder model.BillingOrder
	var account model.CreditAccount
	if err := db.First(&storedOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&account, "user_id = ?", order.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if storedOrder.Status != model.BillingStatusReserved || account.AvailableMicrocredits != 70_000_000 || account.ReservedMicrocredits != 30_000_000 {
		t.Fatalf("rollback facts order=%#v account=%#v", storedOrder, account)
	}
}

func TestDirectTokenUncertainOrderIsNotClaimedForManagedReconciliation(t *testing.T) {
	repo, db := openTokenBillingRepository(t)
	now := time.Now().UTC()
	order := reserveTokenBillingFixture(t, repo, db, "direct-uncertain")
	if err := db.Model(&model.BillingOrder{}).Where("id = ?", order.ID).
		Select("status", "provider_request_id", "next_reconcile_at").
		Updates(model.BillingOrder{Status: model.BillingStatusUncertain, ProviderRequestID: "resp-uncertain", NextReconcileAt: &now}).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimKuaiziBillingReconciliations("worker", now.Add(time.Second), time.Minute, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 0 {
		t.Fatalf("direct uncertain order entered managed reconciliation = %#v", claimed)
	}
}

func TestTokenBillingResponseUsageRejectsZeroSettlementAmount(t *testing.T) {
	repo, _ := openTokenBillingRepository(t)
	fact := responseUsageSettlementFact("resp-zero", 0, TokenUsageFact{InputTokens: 1, OutputTokens: 1})
	if err := repo.SettleTokenBillingFromResponseUsage("missing-order", fact); err == nil || err.Error() != "response usage settlement facts are invalid" {
		t.Fatalf("zero amount response settlement error = %v", err)
	}
}

func responseUsageSettlementFact(providerRequestID string, amount int64, usage TokenUsageFact) ResponseUsageSettlementFact {
	return ResponseUsageSettlementFact{
		ProviderRequestID: providerRequestID, Usage: usage, UsageStatus: "reported", AmountMicrocredits: amount, SettledAt: time.Now().UTC(),
	}
}
