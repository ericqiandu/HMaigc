# Agent Token Billing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. This billing/provider/reconciliation path is tightly coupled and must not use subagent-driven development. Steps use checkbox syntax for tracking.

**Goal:** Replace fixed-per-request Agent billing with atomic pre-reservation and automatic settlement from Kuaizi task billing records.

**Architecture:** Reuse BillingOrder, credit accounts, ledgers, ApiCallLog, the system proxy, and existing versioned Kuaizi credentials. Add one token_usage billing mode, freeze endpoint/credential versions on every order, and use /user/billing/list by exact upstream task ID as the cost authority. A bounded worker reconciles pending orders and never repeats the model request.

**Tech Stack:** Go, Gin, GORM, SQLite/PostgreSQL, existing Kuaizi outbound client and AES-GCM credential versions.

## Global Constraints

- Kuaizi 1 point = CNY 1 = 100 subunits; local CNY 1 = 100 credits. At multiplier 1.0, upstream amount=1 subunit equals 1 local credit.
- First release supports only managed Kuaizi deepseek-v4-flash and deepseek-v4-pro Chat Completions.
- Historical fixed_request orders are immutable.
- Missing or ambiguous upstream billing facts never fall back to one credit or an estimated final charge.
- Every real model request owns one order; idempotent replay cannot reserve or settle twice.
- Budget: three production responsibilities, ten production files, about 800 net production lines. The two registry/publication files are existing owners required to register deepseek-v4-flash and enforce its Agent eligibility; no parallel adapter is introduced.
- This plan delivers the backend commercial core. Aggregated per-turn fee UI is a separate follow-up.

## File Map

- backend/internal/model/models.go: token snapshot and reconciliation facts on BillingOrder.
- backend/internal/service/channel_models.go: token_usage publication gate for managed text models.
- backend/internal/service/finance.go: reservation construction and one reconciliation service API.
- backend/internal/repository/finance.go: atomic reserve, difference settlement, refund, and reconciliation claiming.
- backend/internal/service/kuaizi_client.go: strict billing-list client.
- backend/internal/service/provider_credentials.go: frozen endpoint/credential identities and exact-version resolution.
- backend/internal/service/provider_registry.go: register the already-supported Kuaizi DeepSeek Flash model identity.
- backend/internal/service/agent_model_setting.go: fail closed unless managed DeepSeek Agent models have complete token pricing.
- backend/internal/handler/auth.go: persist upstream task ID before final billing.
- backend/internal/service/service.go: bounded reconciliation worker.
- apps/hono-api/README.md: current Agent billing architecture.

---

### Task 1: Token billing contract and publication gate

**Files:**
- Modify: backend/internal/model/models.go
- Modify: backend/internal/service/channel_models.go
- Modify: backend/internal/service/provider_registry.go
- Modify: backend/internal/service/agent_model_setting.go
- Test: backend/internal/service/channel_models_test.go
- Test: backend/internal/service/token_billing_test.go

**Interfaces:**

```go
type TokenPricingSnapshot struct {
    InputPerMillionMicros  int64 `json:"inputPerMillionMicros"`
    CachedPerMillionMicros int64 `json:"cachedPerMillionMicros"`
    OutputPerMillionMicros int64 `json:"outputPerMillionMicros"`
    MaxOutputTokens        int64 `json:"maxOutputTokens"`
}
type TokenUsageFact struct {
    InputTokens int64
    CachedTokens int64
    OutputTokens int64
}
func tokenChargeMicrocredits(pricing TokenPricingSnapshot, usage TokenUsageFact, multiplierBPS int64) (int64, error)
```

- [ ] **Step 1: Write failing contract tests**

Add TestKuaiziTextModelAllowsTokenUsageBilling, TestTokenUsageBillingRejectsNonTextAndMissingPrice, and TestTokenChargeMicrocreditsUsesIntegerCeiling. Flash prices with 20,000 uncached input and 5,000 output must equal 3,000,000 microcredits. Reject cached>input, negatives, zero prices, and overflow.

- [ ] **Step 2: Run RED**

```bash
cd backend
go test ./internal/service -run '^(TestKuaiziTextModelAllowsTokenUsageBilling|TestTokenUsageBillingRejectsNonTextAndMissingPrice|TestTokenChargeMicrocreditsUsesIntegerCeiling)$' -count=1
```

Expected: FAIL because token_usage and the calculator do not exist.

- [ ] **Step 3: Add explicit BillingOrder facts**

Add ReservedAmountMicrocredits, TokenPricingSnapshotJSON, EstimatedInputTokens, MaxOutputTokens, InputTokens, CachedTokens, OutputTokens, ProviderBillingOrderID, ProviderBillingAmount, ProviderBillingStatus, ProviderEndpointVersionID, ProviderCredentialVersionID, ReconcileAttempts, NextReconcileAt, ReconcileLeaseOwner, ReconcileLeaseToken, and ReconcileLeaseExpiresAt. Use checked integer multiplication/addition and ceiling division; never float64.

- [ ] **Step 4: Enforce publication**

Allow billingMode=="token_usage" only for managed text models. Require CNY ModelPricing with positive input/output prices, non-negative cached price, and positive expected output tokens used as the enforced commercial maxOutputTokens. Reject flat/resolution pricing and fixed unit price for the same model.

- [ ] **Step 5: Run GREEN and commit**

```bash
cd backend
go test ./internal/service -run '^(TestKuaiziTextModelAllowsTokenUsageBilling|TestTokenUsageBillingRejectsNonTextAndMissingPrice|TestTokenChargeMicrocreditsUsesIntegerCeiling)$' -count=1
git add internal/model/models.go internal/service/channel_models.go internal/service/provider_registry.go internal/service/agent_model_setting.go internal/service/channel_models_test.go internal/service/token_billing_test.go
git commit -m "feat(billing): 增加 Agent Token 计费契约"
```

---

### Task 2: Atomic reservation and actual-cost settlement

**Files:**
- Modify: backend/internal/repository/finance.go
- Modify: backend/internal/service/finance.go
- Test: backend/internal/repository/token_billing_test.go
- Test: backend/internal/service/token_billing_test.go

**Interfaces:**

```go
type TokenBillingReservation struct {
    EstimatedInputTokens int64
    MaxOutputTokens int64
    Pricing TokenPricingSnapshot
    EndpointVersionID string
    CredentialVersionID string
}
func (s *Service) ReserveProxyTokenBilling(userID, channelID, modelKey, scene, idempotencyKey string, reservation TokenBillingReservation) (*model.BillingOrder, error)
func (r *Repository) SettleTokenBilling(orderID, providerOrderID string, providerAmountSubunits int64, usage TokenUsageFact, now time.Time) error
func (r *Repository) MarkTokenBillingForReconciliation(orderID, providerTaskID, reason string, next time.Time) error
```

- [ ] **Step 1: Write transaction RED tests**

Add TestReserveProxyTokenBillingAtomicallyFreezesMaximumCost, TestSettleTokenBillingConsumesActualAndReturnsDifference, TestSettleTokenBillingIsIdempotent, TestSettleTokenBillingRollsBackAccountLedgerAndOrderTogether, and TestConcurrentTokenReservationsCannotOverdrawAccount.

Reserve 30 credits and settle amount=6. Assert 24 credits returned, reserved decreases by 30, exactly one consume ledger records -6, and one release ledger records +24 available. A failing order-update trigger must roll back every money fact.

- [ ] **Step 2: Run RED**

```bash
cd backend
go test ./internal/repository ./internal/service -run '^(TestReserveProxyTokenBilling|TestSettleTokenBilling|TestConcurrentTokenReservations)' -count=1
```

- [ ] **Step 3: Implement one transaction owner**

Reserve uses the existing account lock. Settlement lock order is BillingOrder FOR UPDATE then CreditAccount FOR UPDATE, followed by conditional order update, consume ledger, and difference-release ledger in one transaction. Require status reserved/running/uncertain and matching provider task identity. If actual amount exceeds the reserved maximum, move to metering_uncertain; never create debt or silently cap it.

- [ ] **Step 4: Add durable reconciliation claiming**

```go
func (r *Repository) ClaimTokenBillingReconciliations(owner string, now time.Time, lease time.Duration, limit int) ([]model.BillingOrder, error)
func (r *Repository) RescheduleTokenBillingReconciliation(orderID, owner, reason string, next time.Time) error
```

Claims select uncertain token orders with next_reconcile_at<=now. PostgreSQL uses row locking/skip-locked; attempts are bounded. These methods never invoke a model.

- [ ] **Step 5: Run GREEN and commit**

```bash
cd backend
go test ./internal/repository ./internal/service -run 'TokenBilling|ConcurrentTokenReservations' -count=1
git add internal/repository/finance.go internal/repository/token_billing_test.go internal/service/finance.go internal/service/token_billing_test.go
git commit -m "feat(billing): 原子结算 Agent Token 费用"
```

---

### Task 3: Strict Kuaizi task-billing lookup

**Files:**
- Modify: backend/internal/service/kuaizi_client.go
- Modify: backend/internal/service/kuaizi_client_test.go

**Interfaces:**

```go
type KuaiziBillingFact struct {
    OrderID string
    Amount int64
    Status string
    TaskID string
    TaskStatus string
    TaskDuration int64
    TotalTokens int64
    CreatedAt time.Time
    TraceID string
}
func (c *KuaiziClient) BillingByTaskID(ctx context.Context, baseURL, apiKey, taskID string) (KuaiziBillingFact, error)
```

- [ ] **Step 1: Write protocol RED tests**

Assert POST /ai-open-platform-api/v1/user/billing/list, ApiKey authentication, and exact body:

```json
{"task_id":"kz-cgt-task","page":1,"page_size":20}
```

Cover succeeded, pending, failed, zero rows, duplicate exact rows, mismatched task ID, oversized body, HTTP 401/403/500, non-zero code, invalid/overflow amount, invalid status/time, trailing JSON, and key/trace reflection. Errors must not include API key, prompt, or upstream description.

- [ ] **Step 2: Run RED**

```bash
cd backend
go test ./internal/service -run '^TestKuaiziBilling' -count=1
```

- [ ] **Step 3: Implement strict parsing**

Reuse Kuaizi HTTP/DNS/dial SSRF controls, HTTP-before-business-code classification, UseNumber, 64 KiB limit, and second decode requiring EOF. Parse integers through big.Int before checked int64 conversion. Zero exact rows returns typed billing_not_found; multiple exact rows returns billing_ambiguous. Accept only documented statuses.

- [ ] **Step 4: Run GREEN and commit**

```bash
cd backend
go test ./internal/service -run '^(TestKuaiziBilling|TestKuaiziBalance)' -count=1
git add internal/service/kuaizi_client.go internal/service/kuaizi_client_test.go
git commit -m "feat(provider): 查询筷子任务扣费账单"
```

---

### Task 4: Frozen-credential proxy reconciliation

**Files:**
- Modify: backend/internal/service/provider_credentials.go
- Modify: backend/internal/handler/auth.go
- Modify: backend/internal/service/service.go
- Modify: backend/internal/service/finance.go
- Modify: apps/hono-api/README.md
- Test: backend/internal/handler/auth_test.go
- Test: backend/internal/service/provider_credentials_test.go
- Test: backend/internal/service/token_billing_test.go

**Interfaces:** The single flow is proxy request -> frozen reservation -> provider task ID -> Kuaizi bill -> atomic settlement or durable reconciliation.

- [ ] **Step 1: Write integration RED tests**

Add TestKuaiziAgentProxySettlesFromUpstreamBillingAmount, TestKuaiziAgentProxyMissingUsageStillSettlesByTaskID, TestKuaiziAgentProxyPendingBillIsReconciledWithoutSecondModelCall, TestKuaiziAgentProxyAmbiguousBillRemainsMeteringUncertain, TestKuaiziTokenReconciliationUsesFrozenCredentialAfterRotation, and TestKuaiziAgentProxyDuplicateResponseSettlesOnce.

The pending test serves one chat response with id=kz-cgt-task, one pending and one succeeded amount=6 bill, then runs reconciliation. Assert one chat call, two bill lookups, one consume ledger, and the exact reserve difference returned.

- [ ] **Step 2: Run RED**

```bash
cd backend
go test ./internal/handler ./internal/service -run '^TestKuaiziAgentProxy|^TestKuaiziTokenReconciliation' -count=1
```

- [ ] **Step 3: Freeze runtime identities**

Extend SystemProxyRuntime with ProviderEndpointVersionID and ProviderCredentialVersionID. Persist only IDs, never plaintext base URL/key. Add exact-version reconciliation resolution using existing AES-GCM decryption.

- [ ] **Step 4: Reorder proxy handling**

Add the structural helper `prepareTokenBilledProxyRequest(path string, body []byte, maxOutputTokens int64) ([]byte, int64, error)`. It validates JSON, computes a conservative input upper bound from complete UTF-8 body bytes plus a fixed documented protocol margin, and injects/enforces the same max output limit in the forwarded request. It performs no semantic parsing.

Enrich and persist ApiCallLog before billing finalization so ProviderRequestID is durable. For token_usage call one service reconciliation method. A succeeded bill settles immediately. Missing/pending bills schedule durable reconciliation and still return the successful model response without claiming a final charge. Explicit provider failure refunds only after a failed/no-charge billing fact; otherwise it remains uncertain. Never fall back to fixed_request.

- [ ] **Step 5: Add bounded reconciliation worker**

The existing worker claims due token orders, resolves frozen Kuaizi runtime, calls BillingByTaskID, and uses the same atomic settle/refund methods. Use bounded exponential intervals and a fixed attempt maximum. Exhaustion records metering_uncertain_requires_review and preserves frozen credits.

- [ ] **Step 6: Update required architecture documentation**

Update apps/hono-api/README.md “AI 对话架构（当前）”: each agents-cli model request is independently reserved and reconciled through backend system proxy; tools are free; generated media has separate orders; missing provider billing facts remain explicit.

- [ ] **Step 7: Run stable milestone gates and commit**

```bash
cd backend
go test ./internal/handler ./internal/service ./internal/repository -count=1
go test -race ./internal/handler ./internal/service ./internal/repository -run 'TokenBilling|KuaiziAgentProxy|KuaiziBilling' -count=1
go vet ./...
go build ./...
git add internal/service/provider_credentials.go internal/handler/auth.go internal/service/service.go internal/service/finance.go internal/handler/auth_test.go internal/service/provider_credentials_test.go internal/service/token_billing_test.go ../apps/hono-api/README.md
git commit -m "feat(agent): 按筷子真实账单结算 Token 费用"
```

---

### Task 5: Final commercial verification

**Files:** Test-only unless verification finds a defect introduced by Tasks 1-4 inside the announced file map.

- [ ] **Step 1: Run fresh full backend gates once**

```bash
cd backend
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
```

- [ ] **Step 2: Run real PostgreSQL money/concurrency gates once**

Require exact tests:

```text
TestPostgresConcurrentTokenReservationsCannotOverdrawAccount
TestPostgresTokenSettlementReturnsDifferenceExactlyOnce
TestPostgresKuaiziTokenReconciliationUsesFrozenCredential
```

The runner must reject zero matches and clean its isolated containers, network, and volumes.

- [ ] **Step 3: Run one local no-cost provider simulation**

Use httptest only: chat returns task ID, billing returns amount=6, and wallet shows reserve -> consume 6 -> release difference. Do not call a real Kuaizi model/key.

- [ ] **Step 4: Self-review spec, diff, and budget**

Verify one request=one order, no fixed fallback, upstream amount authority, no plaintext secret, frozen credential rotation safety, atomic/idempotent money, historical fixed orders unchanged, production files<=10, and net production additions about<=800. Any >50% budget overrun or new transaction owner triggers redesign.

- [ ] **Step 5: One independent review and one consolidated fix wave**

Review only Critical/Important defects introduced here. Run one scoped re-review. A new cross-module Important finding at scoped re-review triggers the architecture breaker.

- [ ] **Step 6: Commit a fix wave only if needed**

After checking `git diff --name-only`, stage only verified paths from the File Map and their listed tests; never stage unrelated working-tree changes. Commit with `fix(billing): 收口 Agent Token 计费一致性`.

Record actual files/LOC and gates. Explicitly defer aggregated per-turn fee UI to the next plan.
