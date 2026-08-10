package repository

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/testsupport"

	"gorm.io/gorm"
)

func TestPostgresMembershipOrderIdempotencyConcurrentInsertReturnsWinner(t *testing.T) {
	db := openPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatalf("migrate base schema: %v", err)
	}
	if err := database.EnsurePaymentIntegritySchema(db); err != nil {
		t.Fatalf("ensure payment integrity schema: %v", err)
	}
	if err := database.EnsurePaymentIntegritySchema(db); err != nil {
		t.Fatalf("verify existing PostgreSQL payment integrity indexes: %v", err)
	}
	repo := New(db)
	const workers = 24
	start := make(chan struct{})
	results := make(chan *model.MembershipOrder, workers)
	errors := make(chan error, workers)
	var wait sync.WaitGroup
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			<-start
			now := time.Now().UTC()
			candidate := &model.MembershipOrder{
				ID: fmt.Sprintf("concurrent-order-%02d", worker), OrderNumber: fmt.Sprintf("M-CONCURRENT-%02d", worker),
				UserID: "concurrent-user", PlanID: "concurrent-plan", Seats: 1,
				UnitPriceCents: 100, TotalPriceCents: 100, Currency: "CNY", Status: model.MembershipOrderPending,
				PlanSnapshotJSON: `{"id":"concurrent-plan"}`, IdempotencyKey: "concurrent-request", RequestHash: strings.Repeat("a", 64),
				CreatedAt: now, UpdatedAt: now,
			}
			winner, err := repo.CreateMembershipOrder(candidate)
			if err != nil {
				errors <- err
				return
			}
			results <- winner
		}(index)
	}
	close(start)
	wait.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Fatalf("concurrent membership order claim: %v", err)
	}
	winnerID := ""
	resultCount := 0
	for winner := range results {
		resultCount++
		if winner == nil || winner.ID == "" {
			t.Fatalf("empty winning order: %#v", winner)
		}
		if winnerID == "" {
			winnerID = winner.ID
		}
		if winner.ID != winnerID {
			t.Fatalf("concurrent callers observed different winners: first=%s current=%s", winnerID, winner.ID)
		}
	}
	if resultCount != workers {
		t.Fatalf("winner result count = %d, want %d", resultCount, workers)
	}
	var count int64
	if err := db.Model(&model.MembershipOrder{}).Where("user_id = ? AND idempotency_key = ?", "concurrent-user", "concurrent-request").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("winning membership order count = %d, err=%v", count, err)
	}
}

func TestPostgresPaymentPayableClaimSerializesSameAndCrossProvider(t *testing.T) {
	db := openPostgresPaymentIntegritySchema(t)
	if err := database.EnsurePaymentIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	order, session := createPostgresPayableMembershipFixture(t, db, "claim-race", now)
	repo := New(db)

	type claimResult struct {
		provider model.PaymentProvider
		winner   *model.PaymentTransaction
		err      error
	}
	const workers = 24
	start := make(chan struct{})
	results := make(chan claimResult, workers)
	var wait sync.WaitGroup
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			provider := model.PaymentProviderWechat
			if worker%2 == 1 {
				provider = model.PaymentProviderAlipay
			}
			candidate := &model.PaymentTransaction{
				ID: fmt.Sprintf("claim-race-transaction-%02d", worker), OrderType: model.PaymentOrderMembership,
				OrderID: order.ID, UserID: order.UserID, Provider: provider,
				MerchantOrderNo: fmt.Sprintf("MCLAIMRACE%02d", worker), AmountCents: order.TotalPriceCents,
				Currency: order.Currency, Status: model.PaymentTransactionCreated, ExpiresAt: &session.ExpiresAt,
				CreatedAt: now, UpdatedAt: now,
			}
			<-start
			winner, _, err := repo.ClaimPayablePaymentTransaction(candidate)
			results <- claimResult{provider: provider, winner: winner, err: err}
		}(index)
	}
	close(start)
	wait.Wait()
	close(results)

	winningProvider := model.PaymentProvider("")
	winningID := ""
	for result := range results {
		if result.err == nil {
			if result.winner == nil {
				t.Fatal("successful payable claim returned no transaction")
			}
			if winningProvider == "" {
				winningProvider, winningID = result.winner.Provider, result.winner.ID
			}
			if result.provider != winningProvider || result.winner.ID != winningID {
				t.Fatalf("same-provider replay winner = %#v, first provider/id = %s/%s", result.winner, winningProvider, winningID)
			}
			continue
		}
		if !errors.Is(result.err, ErrPaymentChannelLocked) {
			t.Fatalf("cross-provider payable claim error = %v, want ErrPaymentChannelLocked", result.err)
		}
	}
	if winningProvider == "" {
		t.Fatal("no payable transaction won the race")
	}
	assertPostgresPaymentTransactionCount(t, db, "order_id = ?", order.ID, 1)
}

func TestPostgresPaymentPayableClaimRacingCancellationNeverLosesThePayableFact(t *testing.T) {
	for attempt := 0; attempt < 8; attempt++ {
		db := openPostgresPaymentIntegritySchema(t)
		if err := database.EnsurePaymentIntegritySchema(db); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		order, session := createPostgresPayableMembershipFixture(t, db, fmt.Sprintf("cancel-race-%02d", attempt), now)
		repo := New(db)
		candidate := &model.PaymentTransaction{
			ID: fmt.Sprintf("cancel-race-transaction-%02d", attempt), OrderType: model.PaymentOrderMembership,
			OrderID: order.ID, UserID: order.UserID, Provider: model.PaymentProviderWechat,
			MerchantOrderNo: fmt.Sprintf("MCANCELRACE%02d", attempt), AmountCents: order.TotalPriceCents,
			Currency: order.Currency, Status: model.PaymentTransactionCreated, ExpiresAt: &session.ExpiresAt,
			CreatedAt: now, UpdatedAt: now,
		}
		start := make(chan struct{})
		claimResult := make(chan error, 1)
		cancelResult := make(chan error, 1)
		go func() {
			<-start
			_, _, err := repo.ClaimPayablePaymentTransaction(candidate)
			claimResult <- err
		}()
		go func() {
			<-start
			cancelResult <- repo.CloseMembershipOrder(order.ID, order.UserID, order.UserID, "cancel race", now, nil)
		}()
		close(start)
		claimErr, cancelErr := <-claimResult, <-cancelResult
		if claimErr == nil {
			if !errors.Is(cancelErr, ErrPaymentReconciliationRequired) {
				t.Fatalf("attempt %d cancellation error = %v, want reconciliation required", attempt, cancelErr)
			}
		} else {
			if cancelErr != nil || !errors.Is(claimErr, ErrPaymentOrderNotPayable) {
				t.Fatalf("attempt %d race results claim=%v cancel=%v", attempt, claimErr, cancelErr)
			}
		}
		var persisted model.MembershipOrder
		if err := db.First(&persisted, "id = ?", order.ID).Error; err != nil {
			t.Fatal(err)
		}
		var payableCount int64
		if err := db.Model(&model.PaymentTransaction{}).Where("order_id = ? AND status IN ?", order.ID, []model.PaymentTransactionStatus{
			model.PaymentTransactionCreated, model.PaymentTransactionPending, model.PaymentTransactionReviewRequired,
		}).Count(&payableCount).Error; err != nil {
			t.Fatal(err)
		}
		if persisted.Status == model.MembershipOrderCancelled && payableCount != 0 {
			t.Fatalf("attempt %d cancelled order retained %d payable transactions", attempt, payableCount)
		}
	}
}

func TestPostgresPaymentPayableClaimRacingLifecycleExpiryNeverCancelsTheWinner(t *testing.T) {
	for attempt := 0; attempt < 8; attempt++ {
		db := openPostgresPaymentIntegritySchema(t)
		if err := database.EnsurePaymentIntegritySchema(db); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		order, session := createPostgresPayableMembershipFixture(t, db, fmt.Sprintf("expiry-race-%02d", attempt), now)
		if err := db.Model(&model.MembershipOrder{}).Where("id = ?", order.ID).Update("created_at", now.Add(-25*time.Hour)).Error; err != nil {
			t.Fatal(err)
		}
		repo := New(db)
		candidate := &model.PaymentTransaction{
			ID: fmt.Sprintf("expiry-race-transaction-%02d", attempt), OrderType: model.PaymentOrderMembership,
			OrderID: order.ID, UserID: order.UserID, Provider: model.PaymentProviderWechat,
			MerchantOrderNo: fmt.Sprintf("MEXPIRYRACE%02d", attempt), AmountCents: order.TotalPriceCents,
			Currency: order.Currency, Status: model.PaymentTransactionCreated, ExpiresAt: &session.ExpiresAt,
			CreatedAt: now, UpdatedAt: now,
		}
		start := make(chan struct{})
		claimResult := make(chan error, 1)
		expiryResult := make(chan error, 1)
		go func() {
			<-start
			_, _, err := repo.ClaimPayablePaymentTransaction(candidate)
			claimResult <- err
		}()
		go func() {
			<-start
			expiryResult <- repo.ReconcileMembershipLifecycle(now, now.Add(-24*time.Hour))
		}()
		close(start)
		claimErr, expiryErr := <-claimResult, <-expiryResult
		if expiryErr != nil {
			t.Fatalf("attempt %d lifecycle expiry: %v", attempt, expiryErr)
		}
		if claimErr != nil && !errors.Is(claimErr, ErrPaymentOrderNotPayable) {
			t.Fatalf("attempt %d claim error = %v", attempt, claimErr)
		}
		var persisted model.MembershipOrder
		if err := db.First(&persisted, "id = ?", order.ID).Error; err != nil {
			t.Fatal(err)
		}
		var payableCount int64
		if err := db.Model(&model.PaymentTransaction{}).Where("order_id = ? AND status IN ?", order.ID, []model.PaymentTransactionStatus{
			model.PaymentTransactionCreated, model.PaymentTransactionPending, model.PaymentTransactionReviewRequired,
		}).Count(&payableCount).Error; err != nil {
			t.Fatal(err)
		}
		if persisted.Status == model.MembershipOrderCancelled && payableCount != 0 {
			t.Fatalf("attempt %d lifecycle cancelled order with %d payable facts", attempt, payableCount)
		}
	}
}

func TestPostgresPaymentPayableLifecycleRechecksAfterWaitingOnUncommittedClaim(t *testing.T) {
	db := openPostgresPaymentIntegritySchema(t)
	if err := database.EnsurePaymentIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	order, session := createPostgresPayableMembershipFixture(t, db, "life-snap", now)
	if err := db.Model(&model.MembershipOrder{}).Where("id = ?", order.ID).Update("created_at", now.Add(-25*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	releaseInsert := installPostgresInsertBarrier(t, db, "payment_transactions", "lifecycle_claim")
	repo := New(db)
	candidate := &model.PaymentTransaction{
		ID: "lifecycle-post-snapshot-transaction", OrderType: model.PaymentOrderMembership,
		OrderID: order.ID, UserID: order.UserID, Provider: model.PaymentProviderWechat,
		MerchantOrderNo: "MLIFECYCLEPOSTSNAPSHOT", AmountCents: order.TotalPriceCents,
		Currency: order.Currency, Status: model.PaymentTransactionCreated, ExpiresAt: &session.ExpiresAt,
		CreatedAt: now, UpdatedAt: now,
	}
	claimResult := make(chan error, 1)
	go func() {
		_, _, err := repo.ClaimPayablePaymentTransaction(candidate)
		claimResult <- err
	}()
	waitForPostgresBlockedQuery(t, db, `INSERT INTO "payment_transactions"`)

	lifecycleResult := make(chan error, 1)
	go func() {
		lifecycleResult <- repo.ReconcileMembershipLifecycle(now, now.Add(-24*time.Hour))
	}()
	// The lifecycle statement has already taken its MVCC snapshot and is now waiting on the order lock.
	waitForPostgresBlockedQuery(t, db, `FROM "membership_orders"`)
	releaseInsert()

	if err := <-claimResult; err != nil {
		t.Fatalf("payable claim: %v", err)
	}
	if err := <-lifecycleResult; err != nil {
		t.Fatalf("membership lifecycle: %v", err)
	}
	var stored model.MembershipOrder
	if err := db.First(&stored, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.MembershipOrderPending {
		t.Fatalf("lifecycle cancelled order after waiting on committed claim: %s", stored.Status)
	}
	assertPostgresPaymentTransactionCount(t, db, "order_id = ?", order.ID, 1)
}

func TestPostgresPaymentVerifiedFactSerializesBeforeNewPayableClaim(t *testing.T) {
	db := openPostgresPaymentIntegritySchema(t)
	if err := database.EnsurePaymentIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	order, session := createPostgresPayableMembershipFixture(t, db, "fact-claim", now)
	oldTransaction := &model.PaymentTransaction{
		ID: "fact-claim-old-transaction", OrderType: model.PaymentOrderMembership,
		OrderID: order.ID, UserID: order.UserID, Provider: model.PaymentProviderWechat,
		MerchantOrderNo: "MFACTCLAIMOLD", AmountCents: order.TotalPriceCents, Currency: order.Currency,
		Status: model.PaymentTransactionFailed, FailureCode: "provider_rejected", ExpiresAt: &session.ExpiresAt,
		CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute),
	}
	if err := db.Create(oldTransaction).Error; err != nil {
		t.Fatal(err)
	}
	event := &model.PaymentWebhookEvent{
		ID: "fact-claim-event", Provider: oldTransaction.Provider, ProviderEventID: "fact-claim-provider-event",
		TransactionID: oldTransaction.ID, MerchantOrderNo: oldTransaction.MerchantOrderNo, ProviderTradeNo: "fact-claim-trade",
		AmountCents: oldTransaction.AmountCents, Currency: oldTransaction.Currency, PaidAt: &now,
		PayloadDigest: strings.Repeat("e", 64), Status: model.PaymentWebhookReviewRequired,
		FailureCode: "late_payment", ReceivedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	releaseInsert := installPostgresInsertBarrier(t, db, "payment_webhook_events", "fact_record")
	repo := New(db)
	factResult := make(chan error, 1)
	go func() {
		_, _, err := repo.RecordPaymentWebhookEvent(event)
		factResult <- err
	}()
	waitForPostgresBlockedQuery(t, db, `INSERT INTO "payment_webhook_events"`)

	newTransaction := &model.PaymentTransaction{
		ID: "fact-claim-new-transaction", OrderType: model.PaymentOrderMembership,
		OrderID: order.ID, UserID: order.UserID, Provider: model.PaymentProviderAlipay,
		MerchantOrderNo: "MFACTCLAIMNEW", AmountCents: order.TotalPriceCents, Currency: order.Currency,
		Status: model.PaymentTransactionCreated, ExpiresAt: &session.ExpiresAt, CreatedAt: now, UpdatedAt: now,
	}
	claimResult := make(chan error, 1)
	go func() {
		_, _, err := repo.ClaimPayablePaymentTransaction(newTransaction)
		claimResult <- err
	}()
	claimErr, claimCompleted := waitForPostgresBlockedQueryOrResult(t, db, `FROM "membership_orders"`, claimResult)
	releaseInsert()
	if err := <-factResult; err != nil {
		t.Fatalf("record verified fact: %v", err)
	}
	if !claimCompleted {
		claimErr = <-claimResult
	}
	if !errors.Is(claimErr, ErrPaymentVerifiedFactExists) {
		t.Fatalf("claim racing verified fact error = %v, want ErrPaymentVerifiedFactExists", claimErr)
	}
	assertPostgresPaymentTransactionCount(t, db, "order_id = ?", order.ID, 1)
}

func TestPostgresPaymentWebhookVerifiedReviewFactBlocksCancellationAndLifecycleCloseButAllowsSafeCheckoutExpiry(t *testing.T) {
	t.Run("membership cancellation", func(t *testing.T) {
		db := openPostgresPaymentIntegritySchema(t)
		if err := database.EnsurePaymentIntegritySchema(db); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		repo, order, session, _, _ := createPostgresPaymentReviewFactFixture(t, db, "m-cancel", model.PaymentTransactionFailed, now)
		err := repo.CloseMembershipOrder(order.ID, order.UserID, order.UserID, "cancel with verified fact", now, nil)
		if !errors.Is(err, ErrPaymentReconciliationRequired) {
			t.Fatalf("membership cancellation error = %v, want reconciliation required", err)
		}
		assertPostgresOrderAndCheckoutState(t, db, order.ID, model.MembershipOrderPending, session.ID, model.PaymentCheckoutActive)
	})

	t.Run("membership lifecycle expiry", func(t *testing.T) {
		db := openPostgresPaymentIntegritySchema(t)
		if err := database.EnsurePaymentIntegritySchema(db); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		repo, order, session, _, _ := createPostgresPaymentReviewFactFixture(t, db, "m-expiry", model.PaymentTransactionFailed, now)
		if err := db.Model(&model.MembershipOrder{}).Where("id = ?", order.ID).Update("created_at", now.Add(-25*time.Hour)).Error; err != nil {
			t.Fatal(err)
		}
		if err := repo.ReconcileMembershipLifecycle(now, now.Add(-24*time.Hour)); err != nil {
			t.Fatal(err)
		}
		assertPostgresOrderAndCheckoutState(t, db, order.ID, model.MembershipOrderPending, session.ID, model.PaymentCheckoutActive)
	})

	t.Run("direct checkout expiry", func(t *testing.T) {
		db := openPostgresPaymentIntegritySchema(t)
		if err := database.EnsurePaymentIntegritySchema(db); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		repo, order, session, _, _ := createPostgresPaymentReviewFactFixture(t, db, "direct-expiry", model.PaymentTransactionFailed, now)
		if err := db.Model(&model.PaymentCheckoutSession{}).Where("id = ?", session.ID).Update("expires_at", now.Add(-time.Minute)).Error; err != nil {
			t.Fatal(err)
		}
		expired, err := repo.ExpirePaymentCheckoutSession(session.ID, now)
		if !expired || err != nil {
			t.Fatalf("direct checkout expiry = expired:%v err:%v, want safe expiry with durable review fact", expired, err)
		}
		assertPostgresOrderAndCheckoutState(t, db, order.ID, model.MembershipOrderPending, session.ID, model.PaymentCheckoutExpired)
	})

	t.Run("credit cancellation", func(t *testing.T) {
		db := openPostgresPaymentIntegritySchema(t)
		if err := database.EnsurePaymentIntegritySchema(db); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		user := &model.User{ID: "credit-review-user", Username: "credit-review-user", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
		order := &model.CreditTopupOrder{
			ID: "credit-review-order", OrderNumber: "C-CREDIT-REVIEW", UserID: user.ID, ProductID: "credit-review-product",
			BaseMicrocredits: 100, TotalMicrocredits: 100, TotalPriceCents: 100, Currency: "CNY",
			Status: model.CreditTopupOrderPending, ProductSnapshotJSON: `{}`, IdempotencyKey: "credit-review-key",
			RequestHash: strings.Repeat("c", 64), CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(order).Error; err != nil {
			t.Fatal(err)
		}
		session := &model.PaymentCheckoutSession{
			ID: "credit-review-session", OrderType: model.PaymentOrderCreditTopup, OrderID: order.ID, UserID: user.ID,
			TokenHash: strings.Repeat("d", 64), TokenCipher: "enc:v1:test", Status: model.PaymentCheckoutActive,
			ExpiresAt: now.Add(15 * time.Minute), CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(session).Error; err != nil {
			t.Fatal(err)
		}
		transaction := &model.PaymentTransaction{
			ID: "credit-review-transaction", OrderType: model.PaymentOrderCreditTopup, OrderID: order.ID, UserID: user.ID,
			Provider: model.PaymentProviderWechat, MerchantOrderNo: "CCREDITREVIEW", AmountCents: order.TotalPriceCents,
			Currency: order.Currency, Status: model.PaymentTransactionFailed, FailureCode: "provider_rejected",
			ExpiresAt: &session.ExpiresAt, CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(transaction).Error; err != nil {
			t.Fatal(err)
		}
		event := paymentReviewEvent(transaction, "credit-review-event", "credit-review-trade", now)
		repo := New(db)
		if _, created, err := repo.RecordPaymentWebhookEvent(event); err != nil || !created {
			t.Fatalf("record credit review fact = created:%v err:%v", created, err)
		}
		if err := repo.CancelCreditTopupOrder(user.ID, order.ID, now); !errors.Is(err, ErrPaymentReconciliationRequired) {
			t.Fatalf("credit cancellation error = %v, want reconciliation required", err)
		}
		var storedOrder model.CreditTopupOrder
		if err := db.First(&storedOrder, "id = ?", order.ID).Error; err != nil {
			t.Fatal(err)
		}
		if storedOrder.Status != model.CreditTopupOrderPending {
			t.Fatalf("credit order status = %s, want pending", storedOrder.Status)
		}
		var storedSession model.PaymentCheckoutSession
		if err := db.First(&storedSession, "id = ?", session.ID).Error; err != nil {
			t.Fatal(err)
		}
		if storedSession.Status != model.PaymentCheckoutActive {
			t.Fatalf("credit checkout status = %s, want active", storedSession.Status)
		}
	})

	t.Run("provider-confirmed local close", func(t *testing.T) {
		db := openPostgresPaymentIntegritySchema(t)
		if err := database.EnsurePaymentIntegritySchema(db); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		repo, order, session, transaction, _ := createPostgresPaymentReviewFactFixture(t, db, "local-close", model.PaymentTransactionPending, now)
		err := repo.ClosePaymentTransactionAfterProviderConfirmation(transaction.ID, now, nil)
		if !errors.Is(err, ErrPaymentVerifiedFactExists) {
			t.Fatalf("local close error = %v, want verified fact conflict", err)
		}
		assertPostgresOrderAndCheckoutState(t, db, order.ID, model.MembershipOrderPending, session.ID, model.PaymentCheckoutActive)
		var stored model.PaymentTransaction
		if err := db.First(&stored, "id = ?", transaction.ID).Error; err != nil {
			t.Fatal(err)
		}
		if stored.Status != model.PaymentTransactionPending {
			t.Fatalf("transaction status = %s, want pending", stored.Status)
		}
	})
}

func TestPostgresPaymentPayableCheckoutExpiryMovesTransactionToReview(t *testing.T) {
	for _, initialStatus := range []model.PaymentTransactionStatus{
		model.PaymentTransactionCreated,
		model.PaymentTransactionPending,
	} {
		t.Run(string(initialStatus), func(t *testing.T) {
			db := openPostgresPaymentIntegritySchema(t)
			if err := database.EnsurePaymentIntegritySchema(db); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC().Truncate(time.Microsecond)
			suffix := "pxc"
			if initialStatus == model.PaymentTransactionPending {
				suffix = "pxp"
			}
			order, session := createPostgresPayableMembershipFixture(t, db, suffix, now)
			expiredAt := now.Add(-time.Minute)
			if err := db.Model(&model.PaymentCheckoutSession{}).Where("id = ?", session.ID).
				Select("expires_at", "updated_at").Updates(&model.PaymentCheckoutSession{ExpiresAt: expiredAt, UpdatedAt: expiredAt}).Error; err != nil {
				t.Fatal(err)
			}
			transaction := &model.PaymentTransaction{
				ID: "payable-expiry-" + suffix, OrderType: model.PaymentOrderMembership,
				OrderID: order.ID, UserID: order.UserID, Provider: model.PaymentProviderWechat,
				MerchantOrderNo: "MPAYABLEEXPIRY" + strings.ToUpper(suffix),
				AmountCents:     order.TotalPriceCents, Currency: order.Currency, Status: initialStatus,
				ExpiresAt: &expiredAt, CreatedAt: expiredAt.Add(-time.Minute), UpdatedAt: expiredAt.Add(-time.Minute),
			}
			if initialStatus == model.PaymentTransactionPending {
				transaction.CodeURL = "weixin://wxpay/postgres-expired"
			}
			if err := db.Create(transaction).Error; err != nil {
				t.Fatal(err)
			}

			expired, err := New(db).ExpirePaymentCheckoutSession(session.ID, now)
			if err != nil || !expired {
				t.Fatalf("expire payable checkout = expired:%v err:%v", expired, err)
			}
			if err := db.First(transaction, "id = ?", transaction.ID).Error; err != nil {
				t.Fatal(err)
			}
			if transaction.Status != model.PaymentTransactionReviewRequired || transaction.FailureCode != "checkout_expired_requires_reconciliation" {
				t.Fatalf("expired payable transaction = status:%s code:%q", transaction.Status, transaction.FailureCode)
			}
			assertPostgresOrderAndCheckoutState(t, db, order.ID, model.MembershipOrderPending, session.ID, model.PaymentCheckoutExpired)
		})
	}
}

func TestPostgresPaymentPayableClaimRejectsAlreadyPaidTransaction(t *testing.T) {
	db := openPostgresPaymentIntegritySchema(t)
	if err := database.EnsurePaymentIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	order, session := createPostgresPayableMembershipFixture(t, db, "already-paid", now)
	if err := db.Create(&model.PaymentTransaction{
		ID: "already-paid-transaction", OrderType: model.PaymentOrderMembership, OrderID: order.ID, UserID: order.UserID,
		Provider: model.PaymentProviderWechat, MerchantOrderNo: "MALREADYPAID01", ProviderTradeNo: "already-paid-trade",
		AmountCents: order.TotalPriceCents, Currency: order.Currency, Status: model.PaymentTransactionPaid,
		ExpiresAt: &session.ExpiresAt, PaidAt: &now, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	candidate := &model.PaymentTransaction{
		ID: "already-paid-second", OrderType: model.PaymentOrderMembership, OrderID: order.ID, UserID: order.UserID,
		Provider: model.PaymentProviderAlipay, MerchantOrderNo: "MALREADYPAID02", AmountCents: order.TotalPriceCents,
		Currency: order.Currency, Status: model.PaymentTransactionCreated, ExpiresAt: &session.ExpiresAt, CreatedAt: now, UpdatedAt: now,
	}
	if _, _, err := New(db).ClaimPayablePaymentTransaction(candidate); !errors.Is(err, ErrPaymentOrderNotPayable) {
		t.Fatalf("claim after paid transaction error = %v", err)
	}
	assertPostgresPaymentTransactionCount(t, db, "order_id = ?", order.ID, 1)
}

func TestPostgresPaymentPayableClaimRejectsVerifiedReviewFact(t *testing.T) {
	db := openPostgresPaymentIntegritySchema(t)
	if err := database.EnsurePaymentIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	order, session := createPostgresPayableMembershipFixture(t, db, "verified-review", now)
	repo := New(db)
	first := &model.PaymentTransaction{
		ID: "verified-review-first", OrderType: model.PaymentOrderMembership, OrderID: order.ID, UserID: order.UserID,
		Provider: model.PaymentProviderWechat, MerchantOrderNo: "MVERIFIEDREVIEW01", AmountCents: order.TotalPriceCents,
		Currency: order.Currency, Status: model.PaymentTransactionCreated, ExpiresAt: &session.ExpiresAt, CreatedAt: now, UpdatedAt: now,
	}
	if _, claimed, err := repo.ClaimPayablePaymentTransaction(first); err != nil || !claimed {
		t.Fatalf("first claim = claimed:%v err:%v", claimed, err)
	}
	if err := repo.UpdatePaymentTransactionCreation(first.ID, model.PaymentTransactionFailed, "", "provider_rejected", "deterministic rejection", now); err != nil {
		t.Fatal(err)
	}
	event := &model.PaymentWebhookEvent{
		ID: "verified-review-event", Provider: first.Provider, ProviderEventID: "verified-review-provider-event",
		TransactionID: first.ID, MerchantOrderNo: first.MerchantOrderNo, ProviderTradeNo: "verified-review-trade",
		AmountCents: first.AmountCents, Currency: first.Currency, PaidAt: &now, PayloadDigest: strings.Repeat("b", 64),
		Status: model.PaymentWebhookReviewRequired, FailureCode: "late_payment", ReceivedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if _, created, err := repo.RecordPaymentWebhookEvent(event); err != nil || !created {
		t.Fatalf("record verified review fact = created:%v err:%v", created, err)
	}
	second := *first
	second.ID = "verified-review-second"
	second.Provider = model.PaymentProviderAlipay
	second.MerchantOrderNo = "MVERIFIEDREVIEW02"
	second.Status = model.PaymentTransactionCreated
	if _, _, err := repo.ClaimPayablePaymentTransaction(&second); !errors.Is(err, ErrPaymentVerifiedFactExists) {
		t.Fatalf("claim after verified review fact error = %v", err)
	}
	assertPostgresPaymentTransactionCount(t, db, "order_id = ?", order.ID, 1)
}

func TestPostgresPaymentFulfillmentSerializesMembershipSubjectAndGrantsBothOrders(t *testing.T) {
	for _, subject := range []string{"personal", "team"} {
		t.Run(subject, func(t *testing.T) {
			db := openPostgresPaymentIntegritySchema(t)
			if err := database.EnsurePaymentIntegritySchema(db); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC().Truncate(time.Microsecond)
			user := &model.User{ID: subject + "-subject-user", Username: subject + "-subject-user", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}
			if err := db.Create(user).Error; err != nil {
				t.Fatal(err)
			}
			teamID := ""
			orderUserIDs := []string{user.ID, user.ID}
			if subject == "team" {
				teamID = "team-subject"
				if err := db.Create(&model.Team{ID: teamID, OwnerUserID: user.ID, Name: "Team", Status: model.TeamStatusActive, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
					t.Fatal(err)
				}
				secondBuyer := &model.User{ID: "team-second-buyer", Username: "team-second-buyer", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}
				if err := db.Create(secondBuyer).Error; err != nil {
					t.Fatal(err)
				}
				orderUserIDs[1] = secondBuyer.ID
			}
			repo := New(db)
			fulfillments := make([]PaymentFulfillment, 0, 2)
			for index := 0; index < 2; index++ {
				orderID := fmt.Sprintf("%s-fulfillment-order-%d", subject, index)
				order := &model.MembershipOrder{
					ID: orderID, OrderNumber: fmt.Sprintf("M-%s-%d", strings.ToUpper(subject), index), UserID: orderUserIDs[index],
					IdempotencyKey: fmt.Sprintf("%s-fulfillment-key-%d", subject, index), RequestHash: strings.Repeat(fmt.Sprint(index+1), 64),
					TeamID: teamID, PlanID: subject + "-plan", Seats: 1, UnitPriceCents: 1990, TotalPriceCents: 1990,
					Currency: "CNY", Status: model.MembershipOrderPending, PlanSnapshotJSON: `{"id":"membership-plan"}`,
					CreatedAt: now, UpdatedAt: now,
				}
				if err := db.Create(order).Error; err != nil {
					t.Fatal(err)
				}
				expiresAt := now.Add(15 * time.Minute)
				if err := db.Create(&model.PaymentCheckoutSession{
					ID: fmt.Sprintf("%s-fulfillment-checkout-%d", subject, index), OrderType: model.PaymentOrderMembership,
					OrderID: orderID, UserID: orderUserIDs[index], TokenHash: fmt.Sprintf("%064d", index+10), TokenCipher: "enc:v1:test",
					Status: model.PaymentCheckoutActive, ExpiresAt: expiresAt, CreatedAt: now, UpdatedAt: now,
				}).Error; err != nil {
					t.Fatal(err)
				}
				transaction := &model.PaymentTransaction{
					ID: fmt.Sprintf("%s-fulfillment-transaction-%d", subject, index), OrderType: model.PaymentOrderMembership,
					OrderID: orderID, UserID: orderUserIDs[index], Provider: model.PaymentProviderWechat,
					MerchantOrderNo: fmt.Sprintf("M%sFULFILL%d", strings.ToUpper(subject), index), AmountCents: 1990, Currency: "CNY",
					Status: model.PaymentTransactionPending, ExpiresAt: &expiresAt, CreatedAt: now, UpdatedAt: now,
				}
				if err := db.Create(transaction).Error; err != nil {
					t.Fatal(err)
				}
				providerTradeNo := fmt.Sprintf("%s-provider-trade-%d", subject, index)
				event := &model.PaymentWebhookEvent{
					ID: fmt.Sprintf("%s-fulfillment-event-%d", subject, index), Provider: transaction.Provider,
					ProviderEventID: fmt.Sprintf("%s-provider-event-%d", subject, index), TransactionID: transaction.ID,
					MerchantOrderNo: transaction.MerchantOrderNo, ProviderTradeNo: providerTradeNo, AmountCents: 1990, Currency: "CNY",
					PaidAt: &now, PayloadDigest: fmt.Sprintf("%064d", index+20), Status: model.PaymentWebhookReviewRequired,
					ReceivedAt: now, CreatedAt: now, UpdatedAt: now,
				}
				if _, created, err := repo.RecordPaymentWebhookEvent(event); err != nil || !created {
					t.Fatalf("record event %d = created:%v err:%v", index, created, err)
				}
				reference := "membership-order:" + orderID
				fulfillments = append(fulfillments, PaymentFulfillment{
					Event: event, TransactionID: transaction.ID, ProviderTradeNo: providerTradeNo, PaidAt: now,
					PaymentConfirmed: true,
					Activation: MembershipActivation{
						BillingCycle: model.MembershipBillingCycleMonth,
						Subscription: &model.MembershipSubscription{
							ID: fmt.Sprintf("%s-subscription-%d", subject, index), UserID: orderUserIDs[index], TeamID: teamID,
							PlanID: order.PlanID, OrderID: order.ID, Status: model.MembershipSubscriptionActive, Seats: 1,
							PlanSnapshotJSON: order.PlanSnapshotJSON, CreatedAt: now, UpdatedAt: now,
						},
						MembershipLedger: &model.CreditLedgerEntry{
							ID: fmt.Sprintf("%s-ledger-%d", subject, index), UserID: orderUserIDs[index], Type: model.CreditLedgerMembership,
							AmountMicrocredits: 300, AvailableDeltaMicrocredits: 300, ActorUserID: model.SystemActorID,
							Scene: "membership", Note: "membership grant", ReferenceKey: &reference, CreatedAt: now,
						},
					},
				})
			}

			start := make(chan struct{})
			errorsChannel := make(chan error, len(fulfillments))
			var wait sync.WaitGroup
			for index := range fulfillments {
				wait.Add(1)
				go func(input PaymentFulfillment) {
					defer wait.Done()
					<-start
					_, err := repo.FulfillPaymentTransaction(input)
					errorsChannel <- err
				}(fulfillments[index])
			}
			close(start)
			wait.Wait()
			close(errorsChannel)
			for err := range errorsChannel {
				if err != nil {
					t.Fatal(err)
				}
			}
			var subscriptions []model.MembershipSubscription
			query := db.Model(&model.MembershipSubscription{})
			if teamID == "" {
				query = query.Where("user_id = ? AND team_id = ''", user.ID)
			} else {
				query = query.Where("team_id = ?", teamID)
			}
			if err := query.Find(&subscriptions).Error; err != nil {
				t.Fatal(err)
			}
			if len(subscriptions) != 2 {
				t.Fatalf("subscription count = %d, want 2", len(subscriptions))
			}
			sort.Slice(subscriptions, func(left int, right int) bool {
				return subscriptions[left].StartsAt.Before(subscriptions[right].StartsAt)
			})
			if subscriptions[0].EndsAt == nil || !subscriptions[1].StartsAt.Equal(*subscriptions[0].EndsAt) ||
				subscriptions[1].EndsAt == nil || !subscriptions[0].StartsAt.Before(*subscriptions[0].EndsAt) {
				t.Fatalf("subscription intervals are not consecutive: %#v", subscriptions)
			}
			if teamID == "" {
				var account model.CreditAccount
				if err := db.First(&account, "user_id = ?", user.ID).Error; err != nil {
					t.Fatal(err)
				}
				if account.AvailableMicrocredits != 600 {
					t.Fatalf("personal grants = %d, want 600", account.AvailableMicrocredits)
				}
			} else {
				var account model.TeamCreditAccount
				if err := db.First(&account, "team_id = ?", teamID).Error; err != nil {
					t.Fatal(err)
				}
				if account.AvailableMicrocredits != 600 {
					t.Fatalf("team grants = %d, want 600", account.AvailableMicrocredits)
				}
			}
			var systemLedgerCount int64
			if teamID == "" {
				if err := db.Model(&model.CreditLedgerEntry{}).Where("actor_user_id = ?", model.SystemActorID).Count(&systemLedgerCount).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if err := db.Model(&model.TeamCreditLedgerEntry{}).Where("actor_user_id = ?", model.SystemActorID).Count(&systemLedgerCount).Error; err != nil {
					t.Fatal(err)
				}
			}
			if systemLedgerCount != 2 {
				t.Fatalf("system-actor ledger count = %d, want 2", systemLedgerCount)
			}
		})
	}
}

func TestPostgresPaymentWebhookFulfillmentFailuresRetainIndependentVerifiedFact(t *testing.T) {
	for _, failure := range []string{"subscription insert", "ledger insert", "commit"} {
		t.Run(failure, func(t *testing.T) {
			db := openPostgresPaymentIntegritySchema(t)
			if err := database.EnsurePaymentIntegritySchema(db); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC().Truncate(time.Microsecond)
			repo, fulfillment := createPostgresPaymentFulfillmentFixture(t, db, failure, now)
			functionName := "reject_payment_fulfillment_" + strings.ReplaceAll(failure, " ", "_")
			if err := db.Exec(fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected %s failure'; END; $$`, functionName, failure)).Error; err != nil {
				t.Fatal(err)
			}
			triggerTable := "membership_subscriptions"
			if failure == "ledger insert" {
				triggerTable = "credit_ledger_entries"
			}
			triggerSQL := fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION %s()`, functionName, triggerTable, functionName)
			if failure == "commit" {
				triggerSQL = fmt.Sprintf(`CREATE CONSTRAINT TRIGGER %s AFTER INSERT ON membership_subscriptions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION %s()`, functionName, functionName)
			}
			if err := db.Exec(triggerSQL).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := repo.FulfillPaymentTransaction(fulfillment); err == nil {
				t.Fatalf("injected %s failure unexpectedly fulfilled", failure)
			}
			if err := repo.MarkPaymentWebhookOutcome(fulfillment.Event.ID, model.PaymentWebhookReviewRequired, "fulfillment_db_failure", "retryable fulfillment failure", now); err != nil {
				t.Fatal(err)
			}
			var event model.PaymentWebhookEvent
			if err := db.First(&event, "id = ?", fulfillment.Event.ID).Error; err != nil {
				t.Fatal(err)
			}
			if event.Status != model.PaymentWebhookReviewRequired || event.FailureCode != "fulfillment_db_failure" {
				t.Fatalf("independent event after %s failure = %#v", failure, event)
			}
			var order model.MembershipOrder
			if err := db.First(&order, "id = ?", fulfillment.Activation.Subscription.OrderID).Error; err != nil {
				t.Fatal(err)
			}
			if order.Status != model.MembershipOrderPending {
				t.Fatalf("order status after %s failure = %s, want pending", failure, order.Status)
			}
		})
	}
}

func TestPostgresPaymentWebhookProviderTradeCannotCrossOrders(t *testing.T) {
	db := openPostgresPaymentIntegritySchema(t)
	if err := database.EnsurePaymentIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	firstOrder, firstSession := createPostgresPayableMembershipFixture(t, db, "trade-one", now)
	secondOrder, secondSession := createPostgresPayableMembershipFixture(t, db, "trade-two", now)
	firstTransaction := &model.PaymentTransaction{
		ID: "provider-trade-transaction-first", OrderType: model.PaymentOrderMembership, OrderID: firstOrder.ID,
		UserID: firstOrder.UserID, Provider: model.PaymentProviderWechat, MerchantOrderNo: "MPROVIDERTRADE01",
		AmountCents: firstOrder.TotalPriceCents, Currency: firstOrder.Currency, Status: model.PaymentTransactionFailed,
		FailureCode: "provider_rejected", ExpiresAt: &firstSession.ExpiresAt, CreatedAt: now, UpdatedAt: now,
	}
	secondTransaction := &model.PaymentTransaction{
		ID: "provider-trade-transaction-second", OrderType: model.PaymentOrderMembership, OrderID: secondOrder.ID,
		UserID: secondOrder.UserID, Provider: model.PaymentProviderWechat, MerchantOrderNo: "MPROVIDERTRADE02",
		AmountCents: secondOrder.TotalPriceCents, Currency: secondOrder.Currency, Status: model.PaymentTransactionFailed,
		FailureCode: "provider_rejected", ExpiresAt: &secondSession.ExpiresAt, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create([]*model.PaymentTransaction{firstTransaction, secondTransaction}).Error; err != nil {
		t.Fatal(err)
	}
	repo := New(db)
	first := &model.PaymentWebhookEvent{
		ID: "provider-trade-first", Provider: model.PaymentProviderWechat, ProviderEventID: "provider-trade-event-first",
		TransactionID: firstTransaction.ID, MerchantOrderNo: firstTransaction.MerchantOrderNo, ProviderTradeNo: "shared-provider-trade",
		AmountCents: 100, Currency: "CNY", PaidAt: &now, PayloadDigest: strings.Repeat("1", 64), Status: model.PaymentWebhookReceived,
		ReceivedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if _, created, err := repo.RecordPaymentWebhookEvent(first); err != nil || !created {
		t.Fatalf("record first provider trade = created:%v err:%v", created, err)
	}
	second := *first
	second.ID = "provider-trade-second"
	second.ProviderEventID = "provider-trade-event-second"
	second.TransactionID = secondTransaction.ID
	second.MerchantOrderNo = secondTransaction.MerchantOrderNo
	second.PayloadDigest = strings.Repeat("2", 64)
	stored, created, err := repo.RecordPaymentWebhookEvent(&second)
	if err != nil || !created {
		t.Fatalf("durable cross-order provider trade fact = created:%v err:%v", created, err)
	}
	if stored.Status != model.PaymentWebhookReviewRequired || stored.FailureCode != "provider_trade_conflict" {
		t.Fatalf("cross-order provider trade fact = %#v", stored)
	}
	var count int64
	if err := db.Model(&model.PaymentWebhookEvent{}).Where("provider_trade_no = ?", first.ProviderTradeNo).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("provider trade event count = %d, err=%v", count, err)
	}
}

func TestPostgresPaymentIntegrityUpgradesLegacyNullRowsAndVerifiesIndexes(t *testing.T) {
	db := openPaymentIntegrationPostgres(t)
	legacyDDL := []string{
		`CREATE TABLE membership_orders (id text PRIMARY KEY, order_number text, user_id text, idempotency_key text NULL, request_hash text NULL, team_id text, plan_id text, seats integer, unit_price_cents bigint, total_price_cents bigint, currency text, status text, payment_provider text, provider_trade_no text, plan_snapshot_json text, resolved_by text, resolution_note text, paid_at timestamptz, created_at timestamptz, updated_at timestamptz)`,
		`CREATE TABLE payment_checkout_sessions (id text PRIMARY KEY, order_type text, order_id text, user_id text, token_hash text, token_cipher text NULL, status text, expires_at timestamptz, created_at timestamptz, updated_at timestamptz)`,
		`CREATE TABLE payment_transactions (id text PRIMARY KEY, order_type text, order_id text, user_id text, provider text, merchant_order_no text, provider_trade_no text NULL, amount_cents bigint, currency text, status text, code_url text, failure_code text NULL, failure_reason text NULL, expires_at timestamptz, paid_at timestamptz, closed_at timestamptz, created_at timestamptz, updated_at timestamptz)`,
		`CREATE TABLE payment_webhook_events (id text PRIMARY KEY, provider text, provider_event_id text, transaction_id text, merchant_order_no text NULL, provider_trade_no text NULL, currency text NULL, failure_code text NULL, payload_digest text, status text, failure_reason text NULL, received_at timestamptz, processed_at timestamptz, created_at timestamptz, updated_at timestamptz)`,
	}
	for _, statement := range legacyDDL {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := db.Exec(`INSERT INTO membership_orders (id, order_number, user_id, plan_id, seats, unit_price_cents, total_price_cents, currency, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "legacy-order", "M-LEGACY", "legacy-user", "legacy-plan", 1, 100, 100, "CNY", model.MembershipOrderPaid, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO payment_checkout_sessions (id, order_type, order_id, user_id, token_hash, status, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, "legacy-session", model.PaymentOrderMembership, "legacy-order", "legacy-user", "legacy-token-hash", model.PaymentCheckoutConsumed, now, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO payment_transactions (id, order_type, order_id, user_id, provider, merchant_order_no, provider_trade_no, amount_cents, currency, status, failure_reason, paid_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?)`, "legacy-transaction", model.PaymentOrderMembership, "legacy-order", "legacy-user", model.PaymentProviderWechat, "legacy-merchant", "legacy-trade", 100, "CNY", model.PaymentTransactionPaid, now, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO payment_webhook_events (id, provider, provider_event_id, transaction_id, payload_digest, status, failure_reason, received_at, processed_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?)`, "legacy-event", model.PaymentProviderWechat, "legacy-provider-event", "legacy-transaction", "legacy-digest", model.PaymentWebhookProcessed, now, now, now, now).Error; err != nil {
		t.Fatal(err)
	}

	if err := database.MigrateSchema(db); err != nil {
		t.Fatalf("upgrade legacy PostgreSQL schema: %v", err)
	}
	if err := database.EnsurePaymentIntegritySchema(db); err != nil {
		t.Fatalf("verify PostgreSQL payment integrity indexes: %v", err)
	}
	for table, columns := range map[string][]string{
		"membership_orders":         {"idempotency_key", "request_hash"},
		"payment_checkout_sessions": {"token_cipher"},
		"payment_transactions":      {"failure_code"},
		"payment_webhook_events":    {"merchant_order_no", "provider_trade_no", "currency", "failure_code"},
	} {
		for _, column := range columns {
			assertPostgresNotNullEmptyDefault(t, db, table, column)
		}
	}
	var event model.PaymentWebhookEvent
	if err := db.First(&event, "id = ?", "legacy-event").Error; err != nil {
		t.Fatal(err)
	}
	if event.MerchantOrderNo != "legacy-merchant" || event.ProviderTradeNo != "legacy-trade" || event.AmountCents != 100 || event.Currency != "CNY" || event.PaidAt == nil || !event.PaidAt.Equal(now) {
		t.Fatalf("legacy webhook facts = %#v", event)
	}
}

func TestPostgresPaymentIntegrityRejectsWrongNamedIndexPredicate(t *testing.T) {
	db := openPostgresPaymentIntegritySchema(t)
	if err := db.Exec(`CREATE UNIQUE INDEX idx_payment_transactions_payable_order ON payment_transactions(order_type, order_id) WHERE status IN ('created', 'pending')`).Error; err != nil {
		t.Fatal(err)
	}
	err := database.EnsurePaymentIntegritySchema(db)
	if err == nil || !strings.Contains(err.Error(), "idx_payment_transactions_payable_order") {
		t.Fatalf("wrong PostgreSQL predicate error = %v", err)
	}
}

func TestPostgresPaymentIntegrityRejectsDuplicatePayableFactsWithoutDeletion(t *testing.T) {
	db := openPostgresPaymentIntegritySchema(t)
	now := time.Now().UTC()
	transactions := []model.PaymentTransaction{
		{ID: "postgres-payable-a", OrderType: model.PaymentOrderMembership, OrderID: "postgres-payable-order", Provider: model.PaymentProviderWechat, MerchantOrderNo: "postgres-merchant-a", AmountCents: 100, Currency: "CNY", Status: model.PaymentTransactionCreated, CreatedAt: now, UpdatedAt: now},
		{ID: "postgres-payable-b", OrderType: model.PaymentOrderMembership, OrderID: "postgres-payable-order", Provider: model.PaymentProviderAlipay, MerchantOrderNo: "postgres-merchant-b", AmountCents: 100, Currency: "CNY", Status: model.PaymentTransactionReviewRequired, CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&transactions).Error; err != nil {
		t.Fatal(err)
	}
	err := database.EnsurePaymentIntegritySchema(db)
	if err == nil || !strings.Contains(err.Error(), "membership/postgres-payable-order") || !strings.Contains(err.Error(), "postgres-merchant-a") || !strings.Contains(err.Error(), "postgres-merchant-b") {
		t.Fatalf("duplicate PostgreSQL payable facts error = %v", err)
	}
	assertPostgresPaymentTransactionCount(t, db, "order_id = ?", "postgres-payable-order", 2)
}

func TestPostgresPaymentIntegrityRejectsDuplicateProviderTradeFactsWithoutDeletion(t *testing.T) {
	db := openPostgresPaymentIntegritySchema(t)
	now := time.Now().UTC()
	transactions := []model.PaymentTransaction{
		{ID: "postgres-trade-a", OrderType: model.PaymentOrderMembership, OrderID: "postgres-order-a", Provider: model.PaymentProviderWechat, MerchantOrderNo: "postgres-merchant-a", ProviderTradeNo: "postgres-duplicate-trade", AmountCents: 100, Currency: "CNY", Status: model.PaymentTransactionPaid, CreatedAt: now, UpdatedAt: now},
		{ID: "postgres-trade-b", OrderType: model.PaymentOrderCreditTopup, OrderID: "postgres-order-b", Provider: model.PaymentProviderWechat, MerchantOrderNo: "postgres-merchant-b", ProviderTradeNo: "postgres-duplicate-trade", AmountCents: 200, Currency: "CNY", Status: model.PaymentTransactionPaid, CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&transactions).Error; err != nil {
		t.Fatal(err)
	}
	err := database.EnsurePaymentIntegritySchema(db)
	if err == nil || !strings.Contains(err.Error(), "membership/postgres-order-a") || !strings.Contains(err.Error(), "credit_topup/postgres-order-b") || !strings.Contains(err.Error(), "postgres-merchant-a") || !strings.Contains(err.Error(), "postgres-merchant-b") {
		t.Fatalf("duplicate PostgreSQL provider trade facts error = %v", err)
	}
	assertPostgresPaymentTransactionCount(t, db, "provider_trade_no = ?", "postgres-duplicate-trade", 2)
}

func openPostgresPaymentIntegritySchema(t *testing.T) *gorm.DB {
	t.Helper()
	db := openPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatalf("migrate PostgreSQL base schema: %v", err)
	}
	return db
}

func assertPostgresNotNullEmptyDefault(t *testing.T, db *gorm.DB, table string, column string) {
	t.Helper()
	var nullable string
	var defaultSQL string
	if err := db.Raw(`SELECT is_nullable, COALESCE(column_default, '') FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`, table, column).Row().Scan(&nullable, &defaultSQL); err != nil {
		t.Fatal(err)
	}
	if nullable != "NO" || !strings.Contains(defaultSQL, "''") {
		t.Fatalf("%s.%s constraint = nullable:%s default:%q, want NOT NULL DEFAULT ''", table, column, nullable, defaultSQL)
	}
}

func assertPostgresPaymentTransactionCount(t *testing.T, db *gorm.DB, predicate string, value string, expected int64) {
	t.Helper()
	var count int64
	if err := db.Model(&model.PaymentTransaction{}).Where(predicate, value).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != expected {
		t.Fatalf("payment transaction count = %d, want %d", count, expected)
	}
}

func createPostgresPayableMembershipFixture(t *testing.T, db *gorm.DB, suffix string, now time.Time) (*model.MembershipOrder, *model.PaymentCheckoutSession) {
	t.Helper()
	user := &model.User{
		ID: "payable-user-" + suffix, Username: "payable-user-" + suffix,
		Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	order := &model.MembershipOrder{
		ID: "payable-order-" + suffix, OrderNumber: "M-" + strings.ToUpper(suffix), UserID: user.ID,
		IdempotencyKey: "payable-key-" + suffix, RequestHash: strings.Repeat("a", 64), PlanID: "payable-plan-" + suffix,
		Seats: 1, UnitPriceCents: 1990, TotalPriceCents: 1990, Currency: "CNY", Status: model.MembershipOrderPending,
		PlanSnapshotJSON: `{"id":"payable-plan"}`, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(order).Error; err != nil {
		t.Fatal(err)
	}
	session := &model.PaymentCheckoutSession{
		ID: "payable-session-" + suffix, OrderType: model.PaymentOrderMembership, OrderID: order.ID, UserID: user.ID,
		TokenHash: fmt.Sprintf("%064s", suffix), TokenCipher: "enc:v1:test", Status: model.PaymentCheckoutActive,
		ExpiresAt: now.Add(15 * time.Minute), CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(session).Error; err != nil {
		t.Fatal(err)
	}
	return order, session
}

func createPostgresPaymentReviewFactFixture(t *testing.T, db *gorm.DB, suffix string, status model.PaymentTransactionStatus, now time.Time) (*Repository, *model.MembershipOrder, *model.PaymentCheckoutSession, *model.PaymentTransaction, *model.PaymentWebhookEvent) {
	t.Helper()
	order, session := createPostgresPayableMembershipFixture(t, db, suffix, now)
	transaction := &model.PaymentTransaction{
		ID: "review-transaction-" + suffix, OrderType: model.PaymentOrderMembership, OrderID: order.ID, UserID: order.UserID,
		Provider: model.PaymentProviderWechat, MerchantOrderNo: "MREVIEW" + strings.ToUpper(strings.ReplaceAll(suffix, "-", "")),
		AmountCents: order.TotalPriceCents, Currency: order.Currency, Status: status, ExpiresAt: &session.ExpiresAt,
		CreatedAt: now, UpdatedAt: now,
	}
	if status == model.PaymentTransactionFailed {
		transaction.FailureCode = "provider_rejected"
	}
	if err := db.Create(transaction).Error; err != nil {
		t.Fatal(err)
	}
	event := paymentReviewEvent(transaction, "review-event-"+suffix, "review-trade-"+suffix, now)
	repo := New(db)
	if _, created, err := repo.RecordPaymentWebhookEvent(event); err != nil || !created {
		t.Fatalf("record review fact = created:%v err:%v", created, err)
	}
	return repo, order, session, transaction, event
}

func paymentReviewEvent(transaction *model.PaymentTransaction, eventID string, providerTradeNo string, now time.Time) *model.PaymentWebhookEvent {
	return &model.PaymentWebhookEvent{
		ID: eventID, Provider: transaction.Provider, ProviderEventID: eventID + "-provider",
		TransactionID: transaction.ID, MerchantOrderNo: transaction.MerchantOrderNo, ProviderTradeNo: providerTradeNo,
		AmountCents: transaction.AmountCents, Currency: transaction.Currency, PaidAt: &now,
		PayloadDigest: strings.Repeat("9", 64), Status: model.PaymentWebhookReviewRequired,
		FailureCode: "verified_payment_review", ReceivedAt: now, CreatedAt: now, UpdatedAt: now,
	}
}

func assertPostgresOrderAndCheckoutState(t *testing.T, db *gorm.DB, orderID string, orderStatus model.MembershipOrderStatus, sessionID string, checkoutStatus model.PaymentCheckoutStatus) {
	t.Helper()
	var order model.MembershipOrder
	if err := db.First(&order, "id = ?", orderID).Error; err != nil {
		t.Fatal(err)
	}
	if order.Status != orderStatus {
		t.Fatalf("membership order status = %s, want %s", order.Status, orderStatus)
	}
	var session model.PaymentCheckoutSession
	if err := db.First(&session, "id = ?", sessionID).Error; err != nil {
		t.Fatal(err)
	}
	if session.Status != checkoutStatus {
		t.Fatalf("checkout status = %s, want %s", session.Status, checkoutStatus)
	}
}

func createPostgresPaymentFulfillmentFixture(t *testing.T, db *gorm.DB, suffix string, now time.Time) (*Repository, PaymentFulfillment) {
	t.Helper()
	fixtureSuffix := "commit"
	if suffix == "subscription insert" {
		fixtureSuffix = "insert"
	}
	order, session := createPostgresPayableMembershipFixture(t, db, "fault-"+fixtureSuffix, now)
	repo := New(db)
	transaction := &model.PaymentTransaction{
		ID: "fault-transaction-" + fixtureSuffix, OrderType: model.PaymentOrderMembership,
		OrderID: order.ID, UserID: order.UserID, Provider: model.PaymentProviderWechat,
		MerchantOrderNo: "MFAULT" + strings.ToUpper(strings.ReplaceAll(suffix, " ", "")), AmountCents: order.TotalPriceCents,
		Currency: order.Currency, Status: model.PaymentTransactionPending, ExpiresAt: &session.ExpiresAt, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(transaction).Error; err != nil {
		t.Fatal(err)
	}
	providerTradeNo := "fault-provider-trade-" + strings.ReplaceAll(suffix, " ", "-")
	event := &model.PaymentWebhookEvent{
		ID: "fault-event-" + fixtureSuffix, Provider: transaction.Provider,
		ProviderEventID: "fault-provider-event-" + strings.ReplaceAll(suffix, " ", "-"), TransactionID: transaction.ID,
		MerchantOrderNo: transaction.MerchantOrderNo, ProviderTradeNo: providerTradeNo, AmountCents: transaction.AmountCents,
		Currency: transaction.Currency, PaidAt: &now, PayloadDigest: strings.Repeat("f", 64), Status: model.PaymentWebhookReviewRequired,
		ReceivedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if _, created, err := repo.RecordPaymentWebhookEvent(event); err != nil || !created {
		t.Fatalf("record fault event = created:%v err:%v", created, err)
	}
	reference := "membership-order:" + order.ID
	return repo, PaymentFulfillment{
		Event: event, TransactionID: transaction.ID, ProviderTradeNo: providerTradeNo, PaidAt: now, PaymentConfirmed: true,
		Activation: MembershipActivation{
			BillingCycle: model.MembershipBillingCycleMonth,
			Subscription: &model.MembershipSubscription{
				ID: "fault-subscription-" + fixtureSuffix, UserID: order.UserID, PlanID: order.PlanID,
				OrderID: order.ID, Status: model.MembershipSubscriptionActive, Seats: order.Seats,
				PlanSnapshotJSON: order.PlanSnapshotJSON, CreatedAt: now, UpdatedAt: now,
			},
			MembershipLedger: &model.CreditLedgerEntry{
				ID: "fault-ledger-" + fixtureSuffix, UserID: order.UserID, Type: model.CreditLedgerMembership,
				AmountMicrocredits: 300, AvailableDeltaMicrocredits: 300, ActorUserID: model.SystemActorID,
				Scene: "membership", Note: "membership grant", ReferenceKey: &reference, CreatedAt: now,
			},
		},
	}
}

func openPaymentIntegrationPostgres(t *testing.T) *gorm.DB {
	t.Helper()
	return testsupport.OpenPaymentIntegrationPostgres(t)
}
