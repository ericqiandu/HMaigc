package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"

	"github.com/redis/go-redis/v9"
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

func openPaymentIntegrationPostgres(t *testing.T) *gorm.DB {
	t.Helper()
	required := strings.TrimSpace(os.Getenv("CANVAS_REQUIRE_INTEGRATION_TESTS")) == "1"
	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		if required {
			t.Fatal("CANVAS_REQUIRE_INTEGRATION_TESTS=1 but DATABASE_URL is empty")
		}
		t.Skip("DATABASE_URL is not configured for PostgreSQL integration tests")
	}
	parsed := requireLoopbackTestURL(t, dsn, "PostgreSQL")
	databaseName := strings.TrimPrefix(parsed.Path, "/")
	if !strings.Contains(strings.ToLower(databaseName), "test") && !strings.Contains(strings.ToLower(databaseName), "integration") {
		t.Fatalf("PostgreSQL integration database name %q must contain test or integration", databaseName)
	}
	redisURL := strings.TrimSpace(os.Getenv("REDIS_URL"))
	if redisURL == "" {
		t.Fatal("payment integration tests require REDIS_URL")
	}
	requireLoopbackTestURL(t, redisURL, "Redis")
	redisOptions, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("parse Redis URL: %v", err)
	}
	redisClient := redis.NewClient(redisOptions)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping Redis: %v", err)
	}
	t.Cleanup(func() { _ = redisClient.Close() })

	admin, err := database.Open(database.Config{Driver: "postgres", DSN: dsn})
	if err != nil {
		t.Fatalf("open PostgreSQL admin connection: %v", err)
	}
	schema := "payment_test_" + randomPaymentIntegrationSuffix(t)
	if err := admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	isolatedURL := *parsed
	query := isolatedURL.Query()
	query.Set("search_path", schema)
	isolatedURL.RawQuery = query.Encode()
	db, err := database.Open(database.Config{Driver: "postgres", DSN: isolatedURL.String()})
	if err != nil {
		t.Fatalf("open isolated PostgreSQL schema: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, sqlErr := db.DB(); sqlErr == nil {
			_ = sqlDB.Close()
		}
		if dropErr := admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`).Error; dropErr != nil {
			t.Errorf("drop isolated schema %s: %v", schema, dropErr)
		}
		if sqlDB, sqlErr := admin.DB(); sqlErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func requireLoopbackTestURL(t *testing.T, raw string, label string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		t.Fatalf("parse %s URL: %v", label, err)
	}
	host := parsed.Hostname()
	if !strings.EqualFold(host, "localhost") {
		address := net.ParseIP(host)
		if address == nil || !address.IsLoopback() {
			t.Fatalf("%s integration URL must use a loopback host, got %q", label, host)
		}
	}
	return parsed
}

func randomPaymentIntegrationSuffix(t *testing.T) string {
	t.Helper()
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(value[:])
}
