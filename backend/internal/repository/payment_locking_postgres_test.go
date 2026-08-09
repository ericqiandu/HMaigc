package repository

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func TestPostgresPaymentWebhookVerifiedFactSerializesBeforeCheckoutExpiry(t *testing.T) {
	db := openPostgresPaymentIntegritySchema(t)
	if err := database.EnsurePaymentIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	order, session := createPostgresPayableMembershipFixture(t, db, "direct-expiry-race", now)
	if err := db.Model(&model.PaymentCheckoutSession{}).Where("id = ?", session.ID).Update("expires_at", now.Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	transaction := &model.PaymentTransaction{
		ID: "direct-expiry-race-transaction", OrderType: model.PaymentOrderMembership, OrderID: order.ID, UserID: order.UserID,
		Provider: model.PaymentProviderWechat, MerchantOrderNo: "MDIRECTEXPIRYRACE", AmountCents: order.TotalPriceCents,
		Currency: order.Currency, Status: model.PaymentTransactionFailed, FailureCode: "provider_rejected",
		ExpiresAt: &session.ExpiresAt, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(transaction).Error; err != nil {
		t.Fatal(err)
	}
	event := paymentReviewEvent(transaction, "direct-expiry-race-event", "direct-expiry-race-trade", now)
	releaseInsert := installPostgresInsertBarrier(t, db, "payment_webhook_events", "direct_expiry_fact")
	repo := New(db)
	factResult := make(chan error, 1)
	go func() {
		_, _, err := repo.RecordPaymentWebhookEvent(event)
		factResult <- err
	}()
	waitForPostgresBlockedQuery(t, db, `INSERT INTO "payment_webhook_events"`)

	expiryResult := make(chan error, 1)
	go func() {
		expired, err := repo.ExpirePaymentCheckoutSession(session.ID, now)
		if err == nil && expired {
			err = errors.New("checkout expired before concurrent verified fact committed")
		}
		expiryResult <- err
	}()
	expiryErr, expiryCompleted := waitForPostgresBlockedAnyQueryOrResult(t, db, []string{`FROM "membership_orders"`, `UPDATE "payment_checkout_sessions"`}, expiryResult)
	if expiryCompleted {
		releaseInsert()
		<-factResult
		t.Fatalf("checkout expiry did not wait for verified fact order lock: %v", expiryErr)
	}
	releaseInsert()
	if err := <-factResult; err != nil {
		t.Fatalf("record verified fact: %v", err)
	}
	expiryErr = <-expiryResult
	if !errors.Is(expiryErr, ErrPaymentVerifiedFactExists) {
		t.Fatalf("checkout expiry racing verified fact error = %v, want ErrPaymentVerifiedFactExists", expiryErr)
	}
	assertPostgresOrderAndCheckoutState(t, db, order.ID, model.MembershipOrderPending, session.ID, model.PaymentCheckoutActive)
}

func TestPostgresPaymentFulfillmentAndLifecycleUseOrderBeforeSubscriptionLockOrder(t *testing.T) {
	db := openPostgresPaymentIntegritySchema(t)
	if err := database.EnsurePaymentIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	paidAt := time.Now().UTC().Truncate(time.Microsecond)
	lifecycleNow := paidAt.Add(2 * time.Minute)
	repo, fulfillment := createPostgresPaymentFulfillmentFixture(t, db, "lifecycle lock order", paidAt)
	orderID := fulfillment.Activation.Subscription.OrderID
	userID := fulfillment.Activation.Subscription.UserID
	if err := db.Model(&model.MembershipOrder{}).Where("id = ?", orderID).Update("created_at", lifecycleNow.Add(-25*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	existingEndsAt := lifecycleNow.Add(-time.Minute)
	if err := db.Create(&model.MembershipSubscription{
		ID: "lifecycle-existing-subscription", UserID: userID, PlanID: "existing-plan", OrderID: "existing-order",
		Status: model.MembershipSubscriptionActive, Seats: 1, StartsAt: paidAt.Add(-30 * 24 * time.Hour), EndsAt: &existingEndsAt,
		PlanSnapshotJSON: `{"id":"existing-plan"}`, CreatedAt: paidAt.Add(-30 * 24 * time.Hour), UpdatedAt: paidAt,
	}).Error; err != nil {
		t.Fatal(err)
	}

	userLock := db.Begin()
	if userLock.Error != nil {
		t.Fatal(userLock.Error)
	}
	if err := userLock.Exec(`SELECT id FROM users WHERE id = ? FOR UPDATE`, userID).Error; err != nil {
		_ = userLock.Rollback().Error
		t.Fatal(err)
	}
	released := false
	t.Cleanup(func() {
		if !released {
			_ = userLock.Rollback().Error
		}
	})

	fulfillmentResult := make(chan error, 1)
	go func() {
		_, err := repo.FulfillPaymentTransaction(fulfillment)
		fulfillmentResult <- err
	}()
	waitForPostgresBlockedQuery(t, db, `FROM "users"`)
	lifecycleResult := make(chan error, 1)
	go func() {
		lifecycleResult <- repo.ReconcileMembershipLifecycle(lifecycleNow, lifecycleNow.Add(-24*time.Hour))
	}()
	waitForPostgresBlockedQuery(t, db, `FROM "membership_orders"`)
	if err := userLock.Commit().Error; err != nil {
		t.Fatal(err)
	}
	released = true

	fulfillmentErr := <-fulfillmentResult
	lifecycleErr := <-lifecycleResult
	if fulfillmentErr != nil || lifecycleErr != nil {
		t.Fatalf("shared lock order results = fulfillment:%v lifecycle:%v", fulfillmentErr, lifecycleErr)
	}
	var order model.MembershipOrder
	if err := db.First(&order, "id = ?", orderID).Error; err != nil {
		t.Fatal(err)
	}
	if order.Status != model.MembershipOrderPaid {
		t.Fatalf("fulfilled order status = %s, want paid", order.Status)
	}
}

func TestPostgresPaymentWebhookProviderTradeConflictSerializesBeforeDurableFact(t *testing.T) {
	db := openPostgresPaymentIntegritySchema(t)
	if err := database.EnsurePaymentIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	firstOrder, firstSession := createPostgresPayableMembershipFixture(t, db, "trade-race-one", now)
	secondOrder, secondSession := createPostgresPayableMembershipFixture(t, db, "trade-race-two", now)
	firstTransaction := &model.PaymentTransaction{
		ID: "trade-race-tx-first", OrderType: model.PaymentOrderMembership, OrderID: firstOrder.ID,
		UserID: firstOrder.UserID, Provider: model.PaymentProviderWechat, MerchantOrderNo: "MPROVIDERTRADERACE01",
		AmountCents: firstOrder.TotalPriceCents, Currency: firstOrder.Currency, Status: model.PaymentTransactionFailed,
		FailureCode: "provider_rejected", ExpiresAt: &firstSession.ExpiresAt, CreatedAt: now, UpdatedAt: now,
	}
	secondTransaction := &model.PaymentTransaction{
		ID: "trade-race-tx-second", OrderType: model.PaymentOrderMembership, OrderID: secondOrder.ID,
		UserID: secondOrder.UserID, Provider: model.PaymentProviderWechat, MerchantOrderNo: "MPROVIDERTRADERACE02",
		AmountCents: secondOrder.TotalPriceCents, Currency: secondOrder.Currency, Status: model.PaymentTransactionFailed,
		FailureCode: "provider_rejected", ExpiresAt: &secondSession.ExpiresAt, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create([]*model.PaymentTransaction{firstTransaction, secondTransaction}).Error; err != nil {
		t.Fatal(err)
	}
	const providerTradeNo = "shared-provider-trade-race"
	first := paymentReviewEvent(firstTransaction, "provider-trade-race-first", providerTradeNo, now)
	first.Status = model.PaymentWebhookReceived
	first.FailureCode = ""
	second := paymentReviewEvent(secondTransaction, "provider-trade-race-second", providerTradeNo, now)
	second.Status = model.PaymentWebhookReceived
	second.FailureCode = ""

	releaseInsert := installPostgresConditionalInsertBarrier(t, db, "payment_webhook_events", "provider_trade_race", first.ID)
	repo := New(db)
	firstResult := make(chan error, 1)
	go func() {
		_, _, err := repo.RecordPaymentWebhookEvent(first)
		firstResult <- err
	}()
	waitForPostgresBlockedQuery(t, db, `INSERT INTO "payment_webhook_events"`)

	secondResult := make(chan error, 1)
	go func() {
		_, _, err := repo.RecordPaymentWebhookEvent(second)
		secondResult <- err
	}()
	secondErr, secondCompleted := waitForPostgresBlockedQueryOrResult(t, db, "pg_advisory_xact_lock", secondResult)
	if secondCompleted {
		releaseInsert()
		<-firstResult
		t.Fatalf("second provider-trade fact bypassed the uncommitted first fact: %v", secondErr)
	}
	releaseInsert()
	if err := <-firstResult; err != nil {
		t.Fatalf("record first provider-trade fact: %v", err)
	}
	if err := <-secondResult; err != nil {
		t.Fatalf("record second provider-trade fact: %v", err)
	}
	var stored []model.PaymentWebhookEvent
	if err := db.Where("provider = ? AND provider_trade_no = ?", model.PaymentProviderWechat, providerTradeNo).Order("created_at asc, id asc").Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 || stored[0].Status != model.PaymentWebhookReceived || stored[1].Status != model.PaymentWebhookReviewRequired || stored[1].FailureCode != "provider_trade_conflict" {
		t.Fatalf("serialized provider-trade facts = %#v", stored)
	}
}

func installPostgresInsertBarrier(t *testing.T, db *gorm.DB, table string, suffix string) func() {
	t.Helper()
	barrierTable := "payment_test_barrier_" + suffix
	functionName := "payment_test_block_" + suffix
	triggerName := "payment_test_trigger_" + suffix
	if err := db.Exec(fmt.Sprintf(`CREATE TABLE %s (id integer PRIMARY KEY)`, barrierTable)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(fmt.Sprintf(`INSERT INTO %s (id) VALUES (1)`, barrierTable)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM 1 FROM %s WHERE id = 1 FOR UPDATE; RETURN NEW; END; $$`, functionName, barrierTable)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION %s()`, triggerName, table, functionName)).Error; err != nil {
		t.Fatal(err)
	}
	holder := db.Begin()
	if holder.Error != nil {
		t.Fatal(holder.Error)
	}
	if err := holder.Exec(fmt.Sprintf(`SELECT id FROM %s WHERE id = 1 FOR UPDATE`, barrierTable)).Error; err != nil {
		_ = holder.Rollback().Error
		t.Fatal(err)
	}
	released := false
	t.Cleanup(func() {
		if !released {
			_ = holder.Rollback().Error
		}
	})
	return func() {
		if released {
			return
		}
		released = true
		if err := holder.Commit().Error; err != nil {
			t.Fatalf("release PostgreSQL insert barrier: %v", err)
		}
	}
}

func installPostgresConditionalInsertBarrier(t *testing.T, db *gorm.DB, table string, suffix string, rowID string) func() {
	t.Helper()
	barrierTable := "payment_test_barrier_" + suffix
	functionName := "payment_test_block_" + suffix
	triggerName := "payment_test_trigger_" + suffix
	if err := db.Exec(fmt.Sprintf(`CREATE TABLE %s (id integer PRIMARY KEY)`, barrierTable)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(fmt.Sprintf(`INSERT INTO %s (id) VALUES (1)`, barrierTable)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.id = %s THEN PERFORM 1 FROM %s WHERE id = 1 FOR UPDATE; END IF; RETURN NEW; END; $$`, functionName, quotePostgresLiteral(rowID), barrierTable)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION %s()`, triggerName, table, functionName)).Error; err != nil {
		t.Fatal(err)
	}
	holder := db.Begin()
	if holder.Error != nil {
		t.Fatal(holder.Error)
	}
	if err := holder.Exec(fmt.Sprintf(`SELECT id FROM %s WHERE id = 1 FOR UPDATE`, barrierTable)).Error; err != nil {
		_ = holder.Rollback().Error
		t.Fatal(err)
	}
	released := false
	t.Cleanup(func() {
		if !released {
			_ = holder.Rollback().Error
		}
	})
	return func() {
		if released {
			return
		}
		released = true
		if err := holder.Commit().Error; err != nil {
			t.Fatalf("release PostgreSQL conditional insert barrier: %v", err)
		}
	}
}

func quotePostgresLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func waitForPostgresBlockedQuery(t *testing.T, db *gorm.DB, queryFragment string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var count int64
		if err := db.Raw(`SELECT count(*) FROM pg_stat_activity WHERE pid <> pg_backend_pid() AND state = 'active' AND wait_event_type = 'Lock' AND query LIKE ?`, "%"+queryFragment+"%").Scan(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for PostgreSQL blocked query containing %q", queryFragment)
}

func waitForPostgresBlockedQueryOrResult(t *testing.T, db *gorm.DB, queryFragment string, result <-chan error) (error, bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-result:
			return err, true
		default:
		}
		var count int64
		if err := db.Raw(`SELECT count(*) FROM pg_stat_activity WHERE pid <> pg_backend_pid() AND state = 'active' AND wait_event_type = 'Lock' AND query LIKE ?`, "%"+queryFragment+"%").Scan(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count > 0 {
			return nil, false
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for PostgreSQL blocked query containing %q or operation result", queryFragment)
	return nil, false
}

func waitForPostgresBlockedAnyQueryOrResult(t *testing.T, db *gorm.DB, queryFragments []string, result <-chan error) (error, bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-result:
			return err, true
		default:
		}
		for _, fragment := range queryFragments {
			var count int64
			if err := db.Raw(`SELECT count(*) FROM pg_stat_activity WHERE pid <> pg_backend_pid() AND state = 'active' AND wait_event_type = 'Lock' AND query LIKE ?`, "%"+fragment+"%").Scan(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count > 0 {
				return nil, false
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for PostgreSQL blocked query containing one of %q or operation result", queryFragments)
	return nil, false
}
