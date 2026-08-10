# Membership Checkout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver a production-grade personal/team membership checkout that matches the approved LibTV-inspired information hierarchy while preserving HMaigc design tokens, immutable order facts, single-payable-transaction correctness, auditable payment outcomes, and responsive accessibility.

**Architecture:** Keep membership selection editable only before order creation. Freeze the selected plan into `MembershipOrder`, expose a minimal typed checkout projection from that snapshot, and treat `/pay/:token` as a read-only order fact plus an explicit one-time QR payment action. PostgreSQL uniqueness is the final concurrency arbiter; verified webhook facts are committed before fulfillment; the Web route consumes a discriminated checkout DTO and renders personal, team, and credit-topup branches without duplicating business truth.

**Tech Stack:** Go 1.25, Gin, GORM, PostgreSQL 17, SQLite, Redis 7, React 19, TypeScript, Bun, CSS semantic tokens, Nginx, Chromium, Docker Compose.

## Global Constraints

- Use the approved design in `docs/superpowers/specs/2026-08-09-membership-checkout-design.md` as the product contract.
- No compatibility branch, fake payment provider, fake QR, silent fallback, fabricated original price, or implicit auto-renew wording.
- Checkout responses must not expose `userId`, `teamId`, mutable `teamName`, raw plan snapshot JSON, token hash, internal transaction IDs, merchant order numbers, provider error details, or raw webhook payloads.
- A membership order is immutable after creation. Checkout presentation is projected from its frozen snapshot, never from the current plan table.
- `Idempotency-Key` is required for membership order creation and is scoped by user. Same key/same normalized request returns the original order; same key/different request returns conflict.
- Exactly one checkout token and one `created|pending|review_required` payable transaction may exist per order. Claim and fulfillment lock/revalidate the same order before changing that slot; database unique constraints, not process locks, decide races.
- Existing hash-only checkout sessions remain readable through their bearer token but cannot be silently rotated or reconstructed. Recovery without encrypted token material fails explicitly.
- Once a verified payment fact has committed in its independent transaction, it survives subsequent fulfillment rejection, fulfillment INSERT/COMMIT failure, late arrival, cancellation, and duplicate-payment review. Failure to persist that fact itself is an explicit hard failure and no fulfillment starts.
- Production provider URLs and checkout URLs are HTTPS-only. Development HTTP is restricted to loopback.
- Every checkout response, including errors, is `private, no-store`; bearer paths, code URLs, hashes, and raw provider errors are absent from application, GORM, inner Nginx, and edge Nginx logs.
- All new frontend elements use existing semantic tokens. One visible rounded-border layer per independent block, 44px mobile controls, keyboard focus visibility, no 390px overflow, and equivalent light/dark hierarchy are mandatory.
- Database changes are additive. Before production migration, create the existing full PostgreSQL/backend-data restore point. Any historical invariant conflict causes non-zero migration failure; do not delete or auto-correct financial facts.
- New string columns that must scan into Go strings use `NOT NULL DEFAULT ''`; controlled legacy `NULL -> ''` normalization is tested against real pre-change SQLite and PostgreSQL fixtures. Nullable timestamps remain pointers.
- Paid `month|year` plans require `priceCents > 0` at admin update, startup integrity validation, and order creation. Existing invalid paid-plan configuration fails startup/migration review; it is never auto-repriced or sent to a QR provider.

---

### Task 1: Add payment integrity schema and membership-order idempotency

**Files:**
- Modify: `backend/internal/model/membership.go`
- Modify: `backend/internal/model/payment.go`
- Add: `backend/internal/model/actor.go`
- Modify: `backend/internal/database/schema.go`
- Modify: `backend/internal/database/schema_test.go`
- Modify: `backend/cmd/migrate-sqlite-postgres/main.go`
- Modify: `backend/cmd/migrate-sqlite-postgres/migration_coverage_test.go`
- Modify: `backend/internal/repository/membership.go`
- Modify: `backend/internal/handler/membership.go`
- Modify: `backend/internal/service/membership.go`
- Modify: `backend/internal/service/membership_test.go`
- Modify: `backend/internal/service/commercial_membership_test.go`
- Modify: `backend/internal/service/payment_webhook_test.go`
- Modify: `backend/internal/service/referral_test.go`
- Modify: `backend/internal/service/membership_catalog.go`
- Modify: `backend/cmd/server/main.go`
- Add: `backend/internal/repository/payment_postgres_test.go`
- Add: `deploy/tests/docker-compose.payment-integration.yml`
- Add: `scripts/tests/run-payment-integration.sh`
- Modify: `.github/workflows/publish-images.yml`
- Modify: `web/src/services/api/membership.ts`
- Modify: `web/src/pages/membership/index.tsx`
- Add: `web/test/membership-order-idempotency.test.ts`

**Interfaces:**
- `MembershipOrder.IdempotencyKey` and `RequestHash` are write-only `NOT NULL DEFAULT ''` fields (`json:"-"`). Empty legacy keys remain empty.
- `PaymentCheckoutSession.TokenCipher` and new webhook fact/failure string fields are `NOT NULL DEFAULT ''`; historical processed webhook facts are deterministically backfilled from their transaction or the migration fails.
- `PaymentTransactionReviewRequired` occupies the payable slot. `PaymentTransaction.FailureCode` is a stable non-secret code. `PaymentWebhookEvent` adds merchant order, provider trade number, amount, currency, paid time, and stable failure code; raw payload is never stored.
- `CreateMembershipOrder(user, request, idempotencyKey)` requires a 1..120 byte key.
- `MembershipOrderByIdempotencyKey(userID, key)` and atomic `CreateMembershipOrder(order)` return the winning row.
- `MigrateBaseSchema(db)` adds tables/columns/defaults only. `EnsurePaymentIntegritySchema(db)` verifies and creates post-data integrity indexes only after conflict scans pass. Runtime `MigrateSchema(db)` calls both in order; SQLite-to-PostgreSQL calls base schema, copies data, then calls integrity schema.
- `model.SystemActorID` is the only system-actor literal.

- [x] **Step 1: Write failing model, migration, service, and PostgreSQL concurrency tests**

Cover missing/oversize idempotency key, same key/same request replay, same key/different plan/team/seats conflict, cross-user key reuse, replay after plan update/archive, paid month/year plan price `0` or negative at admin update/startup/order creation, concurrent inserts producing one row, real old-schema rows containing NULL, exact index predicates, wrong existing index name/predicate, and migration rejection of duplicate payable rows or duplicate non-empty provider trade numbers. A migration rejection must identify order type/order ID and merchant facts without deleting data.

Update every Go caller to pass an explicit stable test key; do not hide the requirement behind a variadic/default wrapper. Add a Web contract test proving the key is bound to a canonical `{planId, teamId, seats}` fingerprint: identical request retries reuse it, any plan/team/seat change rotates it, it is sent as `Idempotency-Key`, and successful new-team creation stores the resolved team ID before the order request so a later retry does not create another order fingerprint.

- [x] **Step 2: Run focused tests and verify RED**

```powershell
cd backend
go test ./internal/database -run 'Test.*PaymentIntegrity' -count=1
go test ./internal/service -run 'Test.*MembershipOrderIdempotency' -count=1
cd ..
bash scripts/tests/run-payment-integration.sh --run 'TestPostgres.*MembershipOrderIdempotency'
```

Expected: non-zero exits for missing schema/repository/service contracts, not environment setup errors. The runner supplies a real PostgreSQL DSN and required mode, so a skipped integration package is also a test failure.

- [x] **Step 3: Implement the additive schema and atomic membership-order claim**

Use deterministic JSON over normalized `{planId, teamId, seats}` plus SHA-256. Do not hash the mutable current plan snapshot. Insert with `ON CONFLICT ... DO NOTHING`, then read and compare the winner. Add `Idempotency-Key` to CORS allow-headers. Implement:

```sql
CREATE UNIQUE INDEX idx_membership_order_user_idempotency
ON membership_orders(user_id, idempotency_key)
WHERE idempotency_key <> '';

CREATE UNIQUE INDEX idx_payment_transactions_payable_order
ON payment_transactions(order_type, order_id)
WHERE status IN ('created', 'pending', 'review_required');

CREATE UNIQUE INDEX idx_payment_transactions_provider_trade
ON payment_transactions(provider, provider_trade_no)
WHERE provider_trade_no <> '';
```

Use `NOT NULL DEFAULT ''` for the new scan-safe strings and test a real pre-change schema upgrade. For the SQLite-to-PostgreSQL command, run only base schema before copy and integrity enforcement after copied rows exist. Verify existing index SQL/predicate instead of trusting `IF NOT EXISTS`. Never synthesize historical idempotency keys or pick a winner among duplicate payable/payment-trade facts.

Hard-cut the Web membership order call in the same task: a small pure request-fingerprint helper owns `{fingerprint, key}` and `createMembershipOrder(input, idempotencyKey)` sends the standard header. Same-fingerprint retries reuse the key; changing any normalized purchase input rotates it. This keeps the Task 1 commit deployable without a temporary header-optional backend path.

- [x] **Step 4: Run focused SQLite and real PostgreSQL tests**

```powershell
bash scripts/tests/run-payment-integration.sh --run 'Test.*(PaymentIntegrity|MembershipOrderIdempotency)'
cd web
bun test test/membership-order-idempotency.test.ts
bun run build
```

The script creates a unique `hmaigc-payment-integration-<run-suffix>` PostgreSQL 17/Redis 7 project, waits for health, exports both the DSN and `CANVAS_REQUIRE_INTEGRATION_TESTS=1`, executes the requested Go packages, and removes only that run's containers/network/volumes in a trap. The Go harness rejects non-loopback/non-test DSNs and gives every package/test a random isolated schema with cleanup, so parallel packages never migrate or truncate each other's data. CI invokes the same script; no integration test may skip in required mode.

- [x] **Step 5: Review and commit Task 1**

Confirm only additive financial schema, handler/service/repository changes, tests, and required CI wiring are staged. Actual commits:

```text
feat(payment): enforce membership order idempotency
fix(payment): validate prices before catalog writes
```

---

### Task 2: Make checkout tokens recoverable and expose immutable typed checkout facts

**Files:**
- Modify: `backend/internal/model/payment.go`
- Modify: `backend/internal/repository/payment.go`
- Modify: `backend/internal/service/payment.go`
- Add: `backend/internal/service/payment_checkout_view.go`
- Add: `backend/internal/service/payment_checkout_view_test.go`
- Modify: `backend/internal/service/commercial_membership_test.go`
- Modify: `backend/internal/service/membership.go`
- Modify: `backend/internal/handler/payment.go`
- Modify: `web/src/services/api/payment.ts`
- Add: `web/src/pages/payment/payment-checkout-domain.ts`
- Modify: `web/src/pages/payment/payment-checkout-page.tsx`
- Add: `web/test/payment-checkout-domain.test.ts`

**Interfaces:**
- `PaymentCheckoutSession.TokenCipher` stores an `enc:v1:` AES-GCM envelope with AAD binding version/order type/order ID/user ID; plaintext token is never stored.
- `CreateOrGetPaymentCheckoutSession(candidate)` never overwrites an existing session.
- `PaymentCheckoutView` separates `orderStatus` and `checkoutStatus`, and includes `serverNow`, `expiresAt`, `providers`, optional `activeTransaction`, and exactly one discriminated `membershipSummary` or `creditTopupSummary` selected by `orderType`.
- `MembershipCheckoutSummary` includes only frozen audience, code/name/tier/cycle, seats, actual/original price, credits per period, and total credits per period; `CreditTopupCheckoutSummary` remains a separate minimal type.

- [x] **Step 1: Write failing checkout-session and snapshot-projection tests**

Cover personal/team arithmetic, original price `0`, original price equal/below actual, corrupted/missing snapshot, ID/audience/currency/price mismatch, multiplication overflow, credit-topup preservation, no internal JSON fields, sequential/concurrent checkout creation returning the same URL/expiry, database absence of plaintext token, invalid cipher, hash mismatch, and missing key.

Add failing Web domain tests for the discriminated DTO, server-offset countdown, terminal-state monotonicity, active QR restoration, and credit-topup compatibility. The same task hard-cuts `payment-checkout-page.tsx` from legacy `status/amountCents` to `orderStatus/checkoutStatus` and the typed summary; no dual-field compatibility branch is allowed.

- [x] **Step 2: Run focused tests and verify RED**

```powershell
cd backend
go test ./internal/service -run 'Test.*(CheckoutView|PaymentCheckoutSession)' -count=1
cd ../web
bun test test/payment-checkout-domain.test.ts
```

- [x] **Step 3: Implement the shared snapshot validator and typed view**

Extract one snapshot parser/validator used by checkout projection and membership fulfillment. Preserve original price exactly; derive UI discount only when original exceeds actual. Validate all `int64` products before multiplication. Keep valid expired/consumed bearer GET readable, while write APIs require active checkout and pending order.

- [x] **Step 4: Implement recoverable single checkout creation**

Generate token/hash/cipher once, atomically insert-or-read by order, decrypt and hash-verify the winning row, and return the same public URL to all callers. Existing active hash-only rows must not be rotated; direct bearer read remains possible, while owner-side URL recovery fails explicitly.

Implement the minimal functional Web hard cut in this task: consume the new DTO, preserve personal/team/credit-topup rendering and polling, restore any `activeTransaction`, and compile without legacy response fields. The full reference-inspired component split and visual refinement remains Task 5.

- [x] **Step 5: Run focused and package tests**

```powershell
cd backend
go test ./internal/service ./internal/repository ./internal/handler -run 'Test.*(Checkout|MembershipSnapshot)' -count=1
go test ./... -count=1
cd ../web
bun test test/payment-checkout-domain.test.ts
bun run build
```

- [x] **Step 6: Review and commit Task 2**

Actual commits:

```text
feat(payment): expose immutable checkout facts
fix(payment): harden checkout fact recovery
```

---

### Task 3: Enforce one payable transaction and persist verified webhook outcomes

**Files:**
- Modify: `backend/internal/model/payment.go`
- Modify: `backend/internal/repository/payment.go`
- Modify: `backend/internal/repository/credit_store.go`
- Modify: `backend/internal/repository/membership.go`
- Modify: `backend/internal/service/membership.go`
- Modify: `backend/internal/service/payment.go`
- Modify: `backend/internal/service/payment_connector.go`
- Modify: `backend/internal/service/payment_webhook.go`
- Modify: `backend/internal/handler/payment.go`
- Add: `backend/internal/handler/payment_reconciliation_test.go`
- Modify: `backend/internal/service/payment_webhook_test.go`
- Modify: `backend/internal/service/commercial_membership_test.go`
- Modify: `backend/internal/service/membership_test.go`
- Modify: `backend/internal/service/credit_store_test.go`
- Modify: `backend/internal/repository/payment_postgres_test.go`

**Interfaces:**
- `ClaimPayablePaymentTransaction(candidate)` locks and revalidates the membership/credit order, checkout, existing transactions, and verified-success webhook facts before claiming by `(order_type, order_id)` across all providers.
- `PaymentCheckoutTransactionView` contains only provider/status/codeUrl/expiresAt.
- `RecordPaymentWebhookEvent(event)` commits the verified fact independently before fulfillment.
- `MarkPaymentWebhookOutcome(eventID, status, failureCode, failureReason, now)` cannot downgrade `processed`.
- Fulfillment accepts `created`, `pending`, and query-confirmed `review_required` transactions and always uses `model.SystemActorID`.
- `AdminReconcilePaymentTransaction(actor, id)` is a strong audited action: it queries the real provider by persisted merchant order, fulfills confirmed payment, or closes only after the provider confirms unpaid/not-found and its close operation succeeds; ambiguity stays `review_required` and keeps the payable slot occupied.
- Membership fulfillment locks the entitlement subject in a fixed order (personal user row or team row) and computes the next subscription start/end inside the same transaction. Webhook, provider-query reconciliation, and any admin-confirmed path use this single fulfillment implementation.

- [x] **Step 1: Write failing PostgreSQL race and fault tests**

Cover concurrent same-provider and cross-provider claims racing with fulfillment and cancellation/expiry, exactly one payable row, no new transaction after paid or verified review-required fact, cancellation/expiry refusing to blind-close a possibly payable transaction, two different paid orders for the same personal user and same team producing consecutive non-overlapping subscriptions with both exact credit grants, same-provider replay, active channel lock, refresh returning the same QR, provider timeout entering `review_required`, provider query paid/unpaid/unknown outcomes, provider close failure, strong audit rejected/failed attempts, created-state callback success, duplicate event/same digest exactly once, same non-empty provider trade number across distinct event IDs/orders, event ID/different digest conflict, late/closed/second payment review, unknown merchant and amount/currency mismatch rejection, plus injected ledger/subscription INSERT and COMMIT failure leaving an independently committed event.

- [x] **Step 2: Run focused tests and verify RED**

```powershell
bash scripts/tests/run-payment-integration.sh --run 'Test(Postgres|Payment).*(Payable|Webhook|Fulfillment|Reconciliation)'
```

- [x] **Step 3: Implement atomic payable claim and response recovery**

In one transaction, lock the concrete order row and checkout row, verify pending/active status and absence of paid or verified-success review facts, then persist the unique merchant order and `created` row. Fulfillment takes the same order lock, then locks the personal user or team entitlement row before reading the latest subscription end and computing/inserting the next interval. No subscription dates are precomputed outside that transaction. The partial unique index decides residual races. Same-provider retries return the existing transaction. Cross-provider attempts return a stable channel-locked conflict. Deterministic provider rejection releases the slot; an uncertain result becomes `review_required` and continues to occupy it. Membership/credit cancellation and lifecycle expiry must no longer blindly close `created|pending|review_required`: they either find no transaction, obtain provider-confirmed close through the reconciliation path, or explicitly reject/defer the cancellation while keeping the payable fact intact.

- [x] **Step 4: Move webhook facts outside the fulfillment transaction**

Persist provider/event ID, digest, merchant order, provider trade number, amount, currency, paid time, and stable failure code without raw payload. Mark the event review-required before automated fulfillment. On success, atomically update transaction/order/entitlement/ledger/checkout/event. On rejection or fulfillment failure, independently retain `review_required` or `rejected` and emit a structured secret-free alert. Signature/fact-commit failure returns provider failure; durable permanent rejection/late/duplicate/review facts return provider success; retryable fulfillment database failure returns provider failure and a same-ID/same-digest notification re-enters fulfillment. Digest conflict returns failure without overwriting the first fact.

Add provider order-query/close implementations for WeChat and Alipay and a strong-audited admin reconciliation endpoint. A timeout or lost response never auto-retries into a second merchant order. Query-confirmed payment reuses the webhook fulfillment pipeline with a deterministic normalized fact digest; confirmed unpaid/not-found is closed at the provider before local close; unknown remains review-required.

- [x] **Step 5: Run focused and full backend correctness gates**

```powershell
bash scripts/tests/run-payment-integration.sh --run 'Test(Postgres|Payment).*(Payable|Webhook|Fulfillment|Reconciliation)'
cd backend
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
```

- [x] **Step 6: Review and commit Task 3**

Actual commits:

```text
fix(payment): serialize payable transactions and webhook facts
fix(payment): close reconciliation and locking races
fix(payment): 会员收银台 - 消除对账死锁并隔离冲突二维码
```

---

### Task 4: Close checkout cache, URL, and logging boundaries

**Files:**
- Modify: `backend/internal/handler/payment.go`
- Add: `backend/internal/handler/payment_test.go`
- Modify: `backend/internal/service/payment.go`
- Add: `backend/internal/service/payment_setting_security_test.go`
- Modify: `backend/cmd/server/main.go`
- Modify: `backend/cmd/server/main_test.go`
- Modify: `backend/internal/database/database.go`
- Modify: `backend/internal/database/database_test.go`
- Modify: `nginx.conf`
- Modify: `deploy/nginx/hmaigc.conf.example`
- Add: `scripts/tests/verify-payment-checkout-nginx.test.sh`
- Modify: `docker-compose.yml`
- Modify: `docker-compose.production.yml`
- Modify: `deploy/docker-compose.ops.yml`
- Modify: `.env.example`
- Modify: `.env.production.example`
- Modify: `PRODUCTION.md`
- Modify: `deploy/README.md`

**Interfaces:**
- `setPaymentCapabilityHeaders()` applies `Cache-Control: private, no-store`, `Pragma: no-cache`, and `Referrer-Policy: no-referrer` before success or failure.
- `ValidatePaymentRuntime()` runs before readiness/server startup.
- `CANVAS_ENVIRONMENT=development|production` controls URL rules; missing or unknown values fail startup, production requires HTTPS, and development permits HTTP only for loopback.
- Access logs use redacted route templates and never include bearer tokens, checkout URLs, QR code URLs, token hashes, raw provider errors, or Referer.

- [x] **Step 1: Write failing handler, runtime, logger, and Nginx tests**

Use sentinels for bearer token, token hash, QR code URL, and provider error. Assert headers on 2xx/4xx/5xx, production rejection of HTTP, development acceptance of loopback HTTP only, server startup rejection of saved insecure config, Gin path redaction, parameterized GORM logging, and absence of sentinels from inner/edge Nginx logs.

- [x] **Step 2: Run focused tests and verify RED**

```powershell
cd backend
go test ./internal/handler ./internal/service ./internal/database ./cmd/server -run 'Test.*(PaymentHeaders|PaymentRuntime|SensitivePath|Parameterized)' -count=1
cd ..
bash scripts/tests/verify-payment-checkout-nginx.test.sh
```

- [x] **Step 3: Implement headers, environment validation, and startup gate**

Apply the headers before handler branching. Missing/unknown environment fails. Permit development HTTP only for loopback; production requires HTTPS and valid origins. Validate persisted provider/base URLs before opening the listener or readiness.

- [x] **Step 4: Implement application and proxy log redaction**

Replace canvas-only redaction with a sensitive route redactor. Remove raw `ErrorMessage` from request logs. Enable GORM parameterized query logging. Use fixed placeholders or disabled access logs for `/pay/` and `/api/payments/checkout/`; remove Referer from sensitive log formats.

- [x] **Step 5: Run backend, Nginx, and Compose gates**

```powershell
cd backend
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
cd ..
bash scripts/tests/verify-payment-checkout-nginx.test.sh
$env:HMAIGC_IMAGE_REGISTRY='ghcr.io/example'
$env:HMAIGC_VERSION='v0.0.0-ci'
$env:HMAIGC_OPS_VERSION='v0.0.0-ci'
$env:POSTGRES_PASSWORD='ci-only-password'
$env:CANVAS_CORS_ORIGINS='https://ci.example.invalid'
$env:CANVAS_ENVIRONMENT='production'
docker compose config --quiet
docker compose -f docker-compose.production.yml config --quiet
docker compose -f deploy/docker-compose.ops.yml config --quiet
```

- [x] **Step 6: Review and commit Task 4**

Actual commit:

```text
fix(payment): protect checkout capabilities and logs
```

---

### Task 5: Build the responsive personal/team checkout interface

**Files:**
- Add: `web/src/pages/payment/payment-checkout-shell.tsx`
- Add: `web/src/pages/payment/membership-checkout-summary.tsx`
- Add: `web/src/pages/payment/payment-qr-panel.tsx`
- Modify: `web/src/pages/payment/payment-checkout-page.tsx`
- Modify: `web/src/pages/payment/payment-checkout.css`
- Modify: `web/src/pages/membership/membership-purchase-modal.tsx`
- Modify: `web/src/pages/membership/membership-order.css`
- Add: `web/test/payment-checkout-visual-contract.test.ts`

**Interfaces:**
- The Task 2 API/domain hard cut is the only data contract; this task only decomposes presentation components and visual behavior.
- Polling is monotonic: a late pending response cannot replace a paid terminal state. A transient poll error preserves the loaded summary/QR and shows an explicit retry state.
- Existing active transaction restores the same provider and QR; provider selection locks while it is payable.

- [x] **Step 1: Write failing pure-domain and source-contract tests**

Cover personal/team/credit-topup component composition, original-price visibility, expired/consumed/paid/cancelled presentation, a non-promissory `refunded` review state, transient failure preservation, active QR restoration, semantic-token-only styling, and required component separation.

- [x] **Step 2: Run focused tests and verify RED**

```powershell
cd web
bun test test/payment-checkout-visual-contract.test.ts
```

- [x] **Step 3: Implement the typed API and pure presentation domain**

Reuse the typed Task 2 presentation domain. Format one-time payment truthfully and do not calculate a pre-payment entitlement end date.

- [x] **Step 4: Implement the LibTV-inspired HMaigc shell**

Desktop: reference-like left order summary and fixed-width right QR panel. Team summary includes frozen seat count and total credits. Mobile: ordered vertical flow with intact QR and no horizontal scroll. Retain real provider choice when more than one provider is configured. Use semantic tokens, restrained cyan only for selection/payable total/status, and one visible rounded-border layer per block.

- [x] **Step 5: Run focused and full Web gates**

```powershell
cd web
bun test
bun run build
```

- [x] **Step 6: Review and commit Task 5**

Actual commit:

```text
feat(web): 会员收银台 - 重构个人与团队响应式付款界面
```

---

### Task 6: Add production Nginx Chromium checkout regression gates

**Files:**
- Add: `web/scripts/verify-membership-checkout-browser.mjs`
- Add: `web/scripts/membership-checkout-browser-fixture.mjs`
- Add: `web/scripts/membership-checkout-browser-assertions.mjs`
- Modify: `nginx.conf`
- Modify: `scripts/tests/verify-payment-checkout-nginx.test.sh`
- Modify: `web/package.json`
- Modify: `web/bun.lock`
- Modify: `.github/workflows/publish-images.yml`
- Modify: `web/src/pages/payment/payment-checkout.css` only when a real Chromium assertion fails
- Modify: `web/test/payment-checkout-visual-contract.test.ts` only when replacing a source-only false-green with the real browser contract

**Interfaces:**
- The gate serves the production Web build through the real Nginx image and a deterministic local API fixture containing no secrets.
- `puppeteer-core` is a direct pinned development dependency; the gate uses an explicitly provided Chrome/Chromium executable and never silently skips.
- It covers personal, team, credit-topup, existing QR restore, paid, expired, cancelled, transient poll failure, and provider failure.
- It fails when Chromium is unavailable, required variables are absent, or any scenario is skipped.

- [x] **Step 1: Write the browser gate and first run it against the pre-change artifact**

Assert at 1440×900, 768×1024, and 390×844: correct summary facts, QR image size and accessibility name, provider selection/lock, 44px controls, visible keyboard focus, zero overflow, zero console/page errors, zero unexpected CSP violations, readable light/dark contrast, terminal states, and no internal identifiers or auto-renew claims.

- [x] **Step 2: Verify the gate RED or catches a deliberate fixture mutation**

Run the script once with a fixture mutation that changes team total, removes QR alt text, or forces mobile overflow; capture a non-zero exit. Restore the fixture immediately.

- [x] **Step 3: Make only the minimal UI/CSS adjustments required for GREEN**

Do not introduce a browser-only production branch or test hook. Any fixture transport is external to the production bundle.

- [x] **Step 4: Run production artifact and Chromium gates**

```powershell
cd web
bun run build
$env:HMAIGC_CHROMIUM_EXECUTABLE='C:\path\to\chrome.exe'
bun run verify:membership-checkout-browser
```

- [x] **Step 5: Add the non-skippable CI command and commit Task 6**

Place the Chromium gate in `verify` before image publication. Actual commit:

```text
test(web): 会员收银台 - 增加生产 Nginx Chromium 回归门禁
```

---

### Task 7: Synchronize operational documentation and run the final commercial review

**Files:**
- Modify: `.env.production.example`
- Modify: `docs/content/docs/backend/backend-database.mdx`
- Modify: `docs/content/docs/pending-test.mdx`
- Modify: `PRODUCTION.md`
- Modify: `deploy/README.md`
- Modify: `CHANGELOG.md`
- Modify: `docs/superpowers/specs/2026-08-09-membership-checkout-design.md`
- Modify: `docs/superpowers/plans/2026-08-09-membership-checkout.md`

**Interfaces:**
- Documents state the real one-time-payment provider behavior, idempotency requirements, encrypted checkout recovery dependency on `.settings-key`, partial unique indexes, webhook review workflow, backup/rollback sequence, no-store/log contract, and exact validation commands.
- The plan checklist and final evidence must match the actual diff and commands; no unrun command may be reported as passing.
- Pre-documentation corrections are recorded by `ci(payment): 支付集成测试 - 强制运行完整 PostgreSQL 对账矩阵` and `fix(payment): 支付对账 - 补齐待复核交易运营入口`; the final review must include both rather than treating them as documentation-only work.

- [x] **Step 1: Update the operating and database contracts**

Document pre-migration conflict scans and backup, restore of PostgreSQL plus backend data together, how to triage `review_required`, why legacy hash-only checkout tokens cannot be reconstructed, and how production URL/environment validation fails startup.

- [x] **Step 2: Run the complete required backend environment**

Use the scoped integration runner, which starts isolated PostgreSQL 17 and Redis 7 and exports `CANVAS_REQUIRE_INTEGRATION_TESTS=1` plus the test DSN:

```powershell
bash scripts/tests/run-payment-integration.sh --all
```

- [x] **Step 3: Run the complete Web and infrastructure gates**

`PRODUCTION.md` 第 1 节是唯一完整发布门禁清单；以下只列出 Task 7 当前阶段需要补跑的环境化检查子集。未重复列出的 frozen install、Go 全量/race/vet/build 与 SPA route 检查仍必须按该唯一清单执行，不能把本段当成较低标准或完整替代。

```powershell
cd web
bun test
bun run build
$env:HMAIGC_CHROMIUM_EXECUTABLE='C:\path\to\chrome.exe'
bun run verify:membership-checkout-browser
cd ..
bash scripts/tests/verify-payment-checkout-nginx.test.sh
$env:HMAIGC_IMAGE_REGISTRY='ghcr.io/example'
$env:HMAIGC_VERSION='v0.0.0-ci'
$env:HMAIGC_OPS_VERSION='v0.0.0-ci'
$env:POSTGRES_PASSWORD='ci-only-password'
$env:CANVAS_CORS_ORIGINS='https://ci.example.invalid'
$env:CANVAS_ENVIRONMENT='production'
docker compose config --quiet
docker compose -f docker-compose.production.yml config --quiet
docker compose -f deploy/docker-compose.ops.yml config --quiet
docker run --rm -v "${PWD}:/repo" -w /repo rhysd/actionlint:1.7.7
git diff --check
```

- [x] **Step 4: Perform the explicit final review**

Compare the original request, approved design, this plan, the complete diff, backend/frontend DTOs, schema and rollback impact, security/logging behavior, personal/team/credit-topup UI, all test evidence, and documentation. Any missing item returns to its owning task before completion.

Final independent standards, specification/visual, and documentation reviews found no remaining Critical, Important, or Minor issues after the expired-checkout recovery and binding-order fixes.

The final review corrections are recorded by `fix(payment): 收银台到期 - 封闭旧链接与待对账事实` (`673afb0`).

- [x] **Step 5: Commit only the synchronized documentation/evidence**

All listed documentation files are already tracked. Stage only the reviewed paths with normal `git add -- <paths>`; do not use `-f` and do not add ignored `.superpowers/sdd` reports or generated artifacts.

Commit:

```text
docs(payment): 会员收银台 - 同步运维契约与验收证据
```
