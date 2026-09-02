package service

import (
	"encoding/json"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

func TestDirectTokenResponseUsageFinalizesTaskBillingAndOutboxAtomically(t *testing.T) {
	_, db := newMembershipTestService(t)
	if err := db.AutoMigrate(&model.Result{}, &model.TaskOutbox{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	pricing := TokenPricingSnapshot{InputPerMillionMicros: 1_000_000, CachedPerMillionMicros: 500_000, OutputPerMillionMicros: 2_000_000, MaxOutputTokens: 1_000}
	pricingJSON, err := json.Marshal(pricing)
	if err != nil {
		t.Fatal(err)
	}
	account := model.CreditAccount{UserID: "direct-final-user", AvailableMicrocredits: 97_000_000, ReservedMicrocredits: 3_000_000, CreatedAt: now, UpdatedAt: now}
	order := model.BillingOrder{
		ID: "direct-final-order", UserID: account.UserID, TaskID: "direct-final-task", IdempotencyKey: "direct-final-order",
		ChannelID: "direct-channel", ChannelModelID: "vision-record", Model: "vision", Capability: "vision", Scene: "agent",
		BillingMode: "token_usage", MultiplierBasisPoints: 10_000, AmountMicrocredits: 3_000_000, ReservedAmountMicrocredits: 3_000_000,
		TokenPricingSnapshotJSON: string(pricingJSON), Status: model.BillingStatusRunning, CreatedAt: now, UpdatedAt: now,
	}
	leaseExpiry := now.Add(time.Minute)
	task := model.Task{
		ID: order.TaskID, UserID: account.UserID, Type: "agent_vision_analysis", Status: model.TaskStatusRunning, BillingOrderID: order.ID,
		ProviderRequestID: "resp-direct-final", LeaseOwner: "direct-worker", LeaseExpiresAt: &leaseExpiry, LeaseGeneration: 1,
		LeaseToken: "4123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	usage := TokenUsageFact{InputTokens: 1_000, CachedTokens: 200, OutputTokens: 100, Available: true}
	settlement, err := responseUsageSettlementFromOrder(order, task.ProviderRequestID, usage, now)
	if err != nil {
		t.Fatal(err)
	}
	completed := task
	completed.Status = model.TaskStatusSucceeded
	completed.ResultJSON = `{"analysis":"done"}`
	completed.CompletedAt = &now
	outbox := repository.TaskOutboxDraft{IdempotencyKey: "direct-final-outbox", EventType: model.TaskOutboxAgentRunWakeup, PayloadJSON: `{}`, AvailableAt: now}
	if err := repository.New(db).FinalizeSucceededTaskAndBilling(repository.SucceededTaskFinalization{
		Task: &completed, BillingAction: repository.CompletedTaskBillingSettleFromUsage, ResponseUsageSettlement: &settlement, Outbox: &outbox,
	}); err != nil {
		t.Fatal(err)
	}
	var storedTask model.Task
	var storedOrder model.BillingOrder
	var storedOutbox model.TaskOutbox
	if err := db.First(&storedTask, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedOutbox, "idempotency_key = ?", outbox.IdempotencyKey).Error; err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != model.TaskStatusSucceeded || storedOrder.Status != model.BillingStatusSettled || storedOrder.ProviderRequestID != task.ProviderRequestID || storedOutbox.Status != model.TaskOutboxPending {
		t.Fatalf("direct finalization task=%#v order=%#v outbox=%#v", storedTask, storedOrder, storedOutbox)
	}
}

func TestDirectTokenResponseUsageRollsBackTaskBillingAndLedgerWhenOutboxFails(t *testing.T) {
	_, db := newMembershipTestService(t)
	if err := db.AutoMigrate(&model.Result{}, &model.TaskOutbox{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_direct_response_outbox BEFORE INSERT ON task_outboxes BEGIN SELECT RAISE(ABORT, 'outbox unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	pricing := TokenPricingSnapshot{InputPerMillionMicros: 1_000_000, CachedPerMillionMicros: 500_000, OutputPerMillionMicros: 2_000_000, MaxOutputTokens: 1_000}
	pricingJSON, err := json.Marshal(pricing)
	if err != nil {
		t.Fatal(err)
	}
	account := model.CreditAccount{UserID: "direct-rollback-user", AvailableMicrocredits: 97_000_000, ReservedMicrocredits: 3_000_000, CreatedAt: now, UpdatedAt: now}
	order := model.BillingOrder{
		ID: "direct-rollback-order", UserID: account.UserID, TaskID: "direct-rollback-task", IdempotencyKey: "direct-rollback-order",
		ChannelID: "direct-channel", ChannelModelID: "vision-record", Model: "vision", Capability: "vision", Scene: "agent",
		BillingMode: "token_usage", MultiplierBasisPoints: 10_000, AmountMicrocredits: 3_000_000, ReservedAmountMicrocredits: 3_000_000,
		TokenPricingSnapshotJSON: string(pricingJSON), Status: model.BillingStatusRunning, CreatedAt: now, UpdatedAt: now,
	}
	leaseExpiry := now.Add(time.Minute)
	task := model.Task{
		ID: order.TaskID, UserID: account.UserID, Type: "agent_vision_analysis", Status: model.TaskStatusRunning, BillingOrderID: order.ID,
		ProviderRequestID: "resp-direct-rollback", LeaseOwner: "direct-worker", LeaseExpiresAt: &leaseExpiry, LeaseGeneration: 1,
		LeaseToken: "5123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	settlement, err := responseUsageSettlementFromOrder(order, task.ProviderRequestID, TokenUsageFact{InputTokens: 1_000, CachedTokens: 200, OutputTokens: 100, Available: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	completed := task
	completed.Status = model.TaskStatusSucceeded
	completed.ResultJSON = `{"analysis":"done"}`
	completed.CompletedAt = &now
	outbox := repository.TaskOutboxDraft{IdempotencyKey: "direct-rollback-outbox", EventType: model.TaskOutboxAgentRunWakeup, PayloadJSON: `{}`, AvailableAt: now}
	err = repository.New(db).FinalizeSucceededTaskAndBilling(repository.SucceededTaskFinalization{
		Task: &completed, BillingAction: repository.CompletedTaskBillingSettleFromUsage, ResponseUsageSettlement: &settlement, Outbox: &outbox,
	})
	if err == nil {
		t.Fatal("direct response usage finalization accepted failed outbox insert")
	}
	var storedTask model.Task
	var storedOrder model.BillingOrder
	var storedAccount model.CreditAccount
	var ledgerCount int64
	if err := db.First(&storedTask, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedAccount, "user_id = ?", account.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ?", order.ID).Count(&ledgerCount).Error; err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != model.TaskStatusRunning || storedOrder.Status != model.BillingStatusRunning ||
		storedAccount.AvailableMicrocredits != account.AvailableMicrocredits || storedAccount.ReservedMicrocredits != account.ReservedMicrocredits || ledgerCount != 0 {
		t.Fatalf("direct rollback facts task=%#v order=%#v account=%#v ledger=%d", storedTask, storedOrder, storedAccount, ledgerCount)
	}
}

func TestResponseUsageSettlementRejectsManagedOrIncompleteFacts(t *testing.T) {
	pricing := TokenPricingSnapshot{InputPerMillionMicros: 1, OutputPerMillionMicros: 1, MaxOutputTokens: 1}
	pricingJSON, err := json.Marshal(pricing)
	if err != nil {
		t.Fatal(err)
	}
	order := model.BillingOrder{BillingMode: "token_usage", TokenPricingSnapshotJSON: string(pricingJSON), MultiplierBasisPoints: 10_000, ProviderEndpointVersionID: "managed-endpoint", ProviderCredentialVersionID: "managed-key"}
	usage := TokenUsageFact{InputTokens: 1, OutputTokens: 1, Available: true}
	if _, err := responseUsageSettlementFromOrder(order, "resp-managed", usage, time.Now().UTC()); err == nil {
		t.Fatal("managed order accepted direct response settlement")
	}
	order.ProviderEndpointVersionID = ""
	order.ProviderCredentialVersionID = ""
	if _, err := responseUsageSettlementFromOrder(order, "resp-direct", TokenUsageFact{}, time.Now().UTC()); err == nil {
		t.Fatal("missing usage accepted direct response settlement")
	}
}
