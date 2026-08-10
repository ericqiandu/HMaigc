# Membership Checkout Left Facts Unification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make order creation, creation failure, checkout recovery, active QR, and terminal payment states render the same compact left-side membership facts structure while only the right payment state changes.

**Architecture:** Introduce one typed membership order facts display model with two explicit builders: current plan input before order creation and frozen checkout summary after creation. Render both through one `MembershipOrderFacts` component inside the existing `PaymentCheckoutShell`; remove the 880px setup shell and its parallel left-side markup rather than styling the duplicate path.

**Tech Stack:** React, TypeScript, Ant Design, Bun tests, PostCSS source contracts, production Docker/Nginx Chromium gate.

## Global Constraints

- Every membership dialog state uses a `766px` shell with `425px / 341px` desktop columns.
- Order creation and error states may replace only the right surface; left DOM hierarchy remains `heading -> product -> details -> renewal note`.
- Before order creation, financial facts come only from the selected `MembershipPlan` and validated seats.
- After order creation, financial facts come only from the frozen `PaymentCheckout` membership summary.
- Never fabricate discounts: original-price and discount rows render only when original total is greater than actual total.
- Do not copy LibLib names, agreement text, renewal promises, prices, or credits.
- Keep the truthful one-time-purchase copy: `本次为一次性购买，到期不自动续费。`
- Team inputs remain editable only before order creation and move to the right action surface; writing states disable them.
- Mobile uses one column within `calc(100vw - 32px)` and natural vertical scrolling without hidden overflow.
- No backend, database, payment state-machine, QR, agreement-publication, or fulfillment changes.
- No `any`, compatibility export, parallel setup summary, or silent fallback.

---

### Task 1: Create the single membership order facts model and presentation owner

**Files:**

- Create: `web/src/pages/payment/membership-order-facts-domain.ts`
- Create: `web/src/pages/payment/membership-order-facts.tsx`
- Create: `web/src/pages/payment/credit-topup-order-facts.tsx`
- Modify: `web/src/pages/payment/payment-checkout-experience.tsx`
- Delete: `web/src/pages/payment/membership-checkout-summary.tsx`
- Modify: `web/test/payment-checkout-visual-contract.test.ts`

**Interfaces:**

- Consumes: `MembershipPlan`, `PaymentCheckout`, existing `checkoutSummary`, `publicPlanName`, `billingCycleLabel`, `planTotalCredits`, and `planTotalPriceCents`.
- Produces:

```ts
export type MembershipOrderFactsModel = {
    audience: "personal" | "team";
    billingCycle: "month" | "year";
    creditsPerPeriod: number;
    currency: string;
    orderNumber: string;
    originalTotalPriceCents: number;
    originalUnitPriceCents: number;
    seats: number;
    title: string;
    totalCredits: number;
    totalPriceCents: number;
    unitPriceCents: number;
};

export function membershipOrderFactsFromPlan(plan: MembershipPlan, seats: number): MembershipOrderFactsModel;
export function membershipOrderFactsFromCheckout(checkout: PaymentCheckout): MembershipOrderFactsModel;
export function MembershipOrderFacts(props: { facts: MembershipOrderFactsModel }): ReactElement;
export function MembershipOrderFactsSkeleton(): ReactElement;
export function CreditTopupOrderFacts(props: { checkout: PaymentCheckout }): ReactElement;
```

- `membershipOrderFactsFromCheckout` throws an explicit error when the checkout is not a membership order; credit top-up continues using its existing top-up summary path.

- [ ] **Step 1: Write failing model and presentation tests**

Extend `web/test/payment-checkout-visual-contract.test.ts` with real plan and checkout fixtures. Assert:

```ts
const preview = membershipOrderFactsFromPlan(personalPlan, 1);
const frozen = membershipOrderFactsFromCheckout(personalCheckout);

expect(preview).toEqual({
    audience: "personal",
    billingCycle: "year",
    creditsPerPeriod: personalPlan.creditsPerPeriod,
    currency: "CNY",
    orderNumber: "",
    originalTotalPriceCents: personalPlan.originalPriceCents,
    originalUnitPriceCents: personalPlan.originalPriceCents,
    seats: 1,
    title: "标准版",
    totalCredits: personalPlan.creditsPerPeriod,
    totalPriceCents: personalPlan.priceCents,
    unitPriceCents: personalPlan.priceCents,
});
expect(frozen.orderNumber).toBe("M202608100001");
```

Render both models through `MembershipOrderFacts` and assert both contain the same structural owners:

```ts
for (const markup of [previewMarkup, frozenMarkup]) {
    expect(markup).toContain("membership-order-facts");
    expect(markup).toContain("membership-order-facts-heading");
    expect(markup).toContain("membership-order-facts-product");
    expect(markup).toContain("membership-order-facts-details");
    expect(markup).toContain("商品信息");
    expect(markup).toContain("订单明细");
    expect(markup).toContain("本次为一次性购买，到期不自动续费。");
}
```

Add assertions that discounted facts show unit original price, total original price and discount, while `originalTotalPriceCents <= totalPriceCents` hides all discount markup. Assert annual personal facts show the actual monthly equivalent derived from `unitPriceCents / 12`, and monthly facts do not invent an annual equivalent.

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```bash
cd web
bun test test/payment-checkout-visual-contract.test.ts
```

Expected: FAIL because `membership-order-facts-domain.ts`, `membershipOrderFactsFromPlan`, and `MembershipOrderFacts` do not exist.

- [ ] **Step 3: Implement the pure display model builders**

Create `membership-order-facts-domain.ts` with checked integer multiplication for seats. Normalize seats with the existing plan boundaries before computing totals:

```ts
const normalizedSeats = plan.audience === "team" ? Math.min(plan.maxSeats, Math.max(plan.minSeats, seats)) : 1;
```

Map plan preview facts only from current plan fields. Map frozen facts only from `checkoutSummary(checkout)`. Do not reuse current plan data after checkout creation.

- [ ] **Step 4: Implement one left-side presentation component**

Create `membership-order-facts.tsx` with this semantic order:

```tsx
<section className="membership-order-facts">
    <header className="membership-order-facts-heading">
        <h1 className="membership-order-facts-title">...</h1>
        {facts.orderNumber ? <p className="membership-order-facts-order-number">订单 {facts.orderNumber}</p> : null}
    </header>
    <section aria-label="商品信息" className="membership-order-facts-product-section">...</section>
    <section aria-labelledby="membership-order-facts-detail-title" className="membership-order-facts-details">...</section>
    <p className="membership-order-facts-renewal-note">本次为一次性购买，到期不自动续费。</p>
</section>
```

Use the existing currency-safe `Intl.NumberFormat` and microcredit conversion behavior. The title is `开通创作会员` or `开通团队会员`, followed by the actual public plan title, `按月购买` or `按年购买`, and total credits. The product card shows cycle, actual unit price, optional original unit price, and annual monthly equivalent. The detail table shows credits, team seats when applicable, truthful renewal information, optional original/discount rows, and total payable.

`MembershipOrderFactsSkeleton` retains the same heading, product, details, and renewal owner elements with `aria-busy="true"`; it is used only when an order is being recovered without a selected plan and contains no fabricated financial values.

- [ ] **Step 5: Hard-cut the frozen checkout path to the new owner**

In `payment-checkout-experience.tsx`, membership checkout maps to `membershipOrderFactsFromCheckout(checkout)` and renders `MembershipOrderFacts`. Move the current credit top-up rows into `CreditTopupOrderFacts`, which rejects non-top-up checkouts and keeps the existing frozen amount/credit behavior. Delete `membership-checkout-summary.tsx`; do not leave a re-export or compatibility wrapper.

- [ ] **Step 6: Run focused and full Web verification**

Run:

```bash
cd web
bun test test/payment-checkout-visual-contract.test.ts
bun test
bun run build
```

Expected: all commands exit `0`; frozen personal/team/top-up checkout tests remain green.

- [ ] **Step 7: Review and commit Task 1**

Run `git diff --check`, confirm no `any`, raw feature colors, LibLib copy, or backend changes, then:

```bash
git add -- web/src/pages/payment/membership-order-facts-domain.ts web/src/pages/payment/membership-order-facts.tsx web/src/pages/payment/credit-topup-order-facts.tsx web/src/pages/payment/payment-checkout-experience.tsx web/src/pages/payment/membership-checkout-summary.tsx web/test/payment-checkout-visual-contract.test.ts
git diff --cached --check
git commit -m "refactor(web): 会员收银台 - 统一左侧订单事实组件"
```

---

### Task 2: Hard-cut creation, loading, and failure states to the same 766px shell

**Files:**

- Modify: `web/src/pages/membership/membership-payment-dialog.tsx`
- Modify: `web/src/pages/membership/membership-payment-setup.tsx`
- Modify: `web/src/pages/membership/membership-order.css`
- Modify: `web/src/pages/payment/payment-checkout.css`
- Modify: `web/test/membership-payment-dialog.test.ts`
- Modify: `web/test/payment-checkout-visual-contract.test.ts`
- Modify: `web/scripts/membership-checkout-browser-assertions.mjs`
- Modify: `web/scripts/verify-membership-checkout-browser.mjs`
- Modify: `web/scripts/membership-checkout-browser-fixture.mjs`

**Interfaces:**

- Consumes: `membershipOrderFactsFromPlan(plan, seats)`, `MembershipOrderFacts`, existing team selection callbacks, write-state flags, and `PaymentCheckoutExperience`.
- Produces: one `MembershipPaymentDialog` whose width is always `766`; setup root uses `payment-checkout-shell is-dialog membership-payment-setup`, and left surface always renders `MembershipOrderFacts`.

- [ ] **Step 1: Write failing all-state parity tests**

Update `membership-payment-dialog.test.ts` to render personal creation, personal failure, team confirmation, and frozen checkout loading. Assert the first three real setup markups contain:

```ts
expect(markup).toContain("payment-checkout-shell is-dialog membership-payment-setup");
expect(markup).toContain("payment-checkout-order-surface");
expect(markup).toContain("payment-checkout-payment-surface");
expect(markup).toContain("membership-order-facts");
expect(markup).toContain("membership-order-facts-product");
expect(markup).toContain("membership-order-facts-details");
```

Source-contract assertions:

```ts
expect(dialogSource).toContain("width={766}");
expect(dialogSource).not.toContain("checkoutToken ? 766 : 880");
expect(setupSource).not.toContain("membership-payment-dialog-heading");
expect(setupSource).not.toContain("membership-payment-preview");
expect(setupSource).not.toContain("membership-payment-product");
```

Assert creation failure still renders the raw server error and retry action on the right, while the left retains actual plan title, billing cycle, credits, original price, discount, and payable total. Assert a non-discount plan does not render original or discount rows.

Extend the production browser fixture with explicit dialog states:

- `membership-personal-creating-dialog`
- `membership-personal-order-failure-dialog`
- `membership-personal-checkout-failure-dialog`
- `membership-personal-active-qr-dialog`

For each state collect shell width, left/right widths, the four structural class owners, and descendant bounds. Assert desktop `766 / 425 / 341`, tablet 425:341 ratio, mobile single-column bounds, and identical left structural owners.

- [ ] **Step 2: Run the focused test and verify RED**

Run the component tests:

```bash
cd web
bun test test/membership-payment-dialog.test.ts test/payment-checkout-visual-contract.test.ts
```

Expected: FAIL because the setup path still uses the 880px parallel structure and old preview classes.

Build the current production tree, then run the new browser assertions before changing production components:

```powershell
bun run build
$env:HMAIGC_CHROMIUM_EXECUTABLE='C:\Program Files\Google\Chrome\Application\chrome.exe'
bun run verify:membership-checkout-browser
```

Expected: FAIL on the creating or failure dialog because it is 880px or lacks `membership-order-facts` owners.

- [ ] **Step 3: Render the same shell and left facts in setup states**

Change `MembershipPaymentDialog` to `width={766}` for every state. In `MembershipPaymentSetup`, build facts once:

```ts
const facts = plan ? membershipOrderFactsFromPlan(plan, appliedSeats) : null;
```

Render:

```tsx
<div className="payment-checkout-shell is-dialog membership-payment-setup">
    <div className="payment-checkout-order-surface">
        {facts ? <MembershipOrderFacts facts={facts} /> : <MembershipOrderFactsSkeleton />}
    </div>
    <aside aria-label="创建付款码" className="payment-checkout-payment-surface membership-payment-setup-action">
        {/* team configuration, progress, or explicit error */}
    </aside>
</div>
```

Keep team selection and seat inputs in the right surface before confirmation. Disable every input and close action while `submitting || openingCheckout`. Error and retry remain right-side facts. Do not duplicate any price or credit markup in `MembershipPaymentSetup`.

- [ ] **Step 4: Remove the obsolete setup CSS and apply reference geometry**

Delete old 880px setup-only owners:

- `.membership-payment-dialog-heading`
- `.membership-payment-dialog-eyebrow`
- `.membership-payment-dialog-title`
- `.membership-payment-product*`
- `.membership-payment-preview*`
- the `560px 320px` setup grid

Make the modal width `min(766px, calc(100vw - 48px))` in all desktop/tablet states and `min(766px, calc(100vw - 32px))` below 768px. Reuse the checkout shell's 425/341 grid and mobile one-column rule. Keep one outer rounded boundary; internal left and right surfaces stay straight.

- [ ] **Step 5: Run focused GREEN and full Web regression**

Run:

```bash
cd web
bun test test/membership-payment-dialog.test.ts test/payment-checkout-visual-contract.test.ts
bun test
bun run build
$env:HMAIGC_CHROMIUM_EXECUTABLE='C:\Program Files\Google\Chrome\Application\chrome.exe'
bun run verify:membership-checkout-browser
```

Expected: focused and full suites exit `0`; bundle budgets pass.

- [ ] **Step 6: Review and commit Task 2**

Review the rendered markup against the reference structure and ensure only the right state differs. Then:

```bash
git add -- web/src/pages/membership/membership-payment-dialog.tsx web/src/pages/membership/membership-payment-setup.tsx web/src/pages/membership/membership-order.css web/src/pages/payment/payment-checkout.css web/test/membership-payment-dialog.test.ts web/test/payment-checkout-visual-contract.test.ts web/scripts/membership-checkout-browser-assertions.mjs web/scripts/verify-membership-checkout-browser.mjs web/scripts/membership-checkout-browser-fixture.mjs
git diff --cached --check
git commit -m "fix(web): 会员收银台 - 统一创建与失败状态排版"
```

---

### Task 3: Run production gates, document the correction, and refresh local preview

**Files:**

- Modify: `CHANGELOG.md`

**Interfaces:**

- Consumes: the production dialog and browser matrix completed in Task 2.
- Produces: final release evidence, factual changelog entry, and a rebuilt local preview.

- [ ] **Step 1: Run mutation evidence**

Run the existing `team-total` mutation and require a non-zero exit:

```powershell
$env:HMAIGC_CHECKOUT_GATE_MUTATION='team-total'
bun run verify:membership-checkout-browser
Remove-Item Env:HMAIGC_CHECKOUT_GATE_MUTATION
```

The command must fail on a team-order fact assertion. A zero exit is a test defect.

- [ ] **Step 2: Run final production gates**

Run:

```bash
cd web
bun install --frozen-lockfile
bun test
bun run build
node --check scripts/membership-checkout-browser-assertions.mjs
node --check scripts/verify-membership-checkout-browser.mjs
node --check scripts/membership-checkout-browser-fixture.mjs

cd ..
bash scripts/tests/verify-payment-checkout-nginx.test.sh
git diff --check
```

No backend or database files change, so the already green payment integration contract remains unchanged; still run `go test ./... -count=1` from `backend` as the cross-contract smoke gate.

- [ ] **Step 3: Update the Unreleased changelog**

Add one factual bullet under `会员与支付`:

```md
- 会员付款弹窗在订单创建、创建失败、付款码恢复和支付终态中统一使用同一套左侧商品与订单明细结构，右侧仅切换配置、错误或付款状态，避免创建失败时退回旧版宽布局。
```

- [ ] **Step 4: Rebuild the local preview and verify health**

Run:

```powershell
docker compose -f docker-compose.yml up -d --build
Invoke-WebRequest -UseBasicParsing http://127.0.0.1:3000/api/health
Invoke-WebRequest -UseBasicParsing http://127.0.0.1:3000/membership
```

Require both containers healthy and both HTTP requests return `200`. Preserve `.local/data`; do not remove or recreate the local database directory.

- [ ] **Step 5: Final review and commit Task 3**

Review the approved spec against the actual diff, all four dialog states, desktop/tablet/mobile screenshots, raw server errors, financial truth, responsive bounds, and absence of LibLib content. Stage exact files:

```bash
git add -- CHANGELOG.md
git diff --cached --check
git commit -m "test(web): 会员收银台 - 锁定全状态左侧排版"
```

---

## Completion Checklist

- [ ] Creating, failure, active QR, and terminal states share one left component and structural signature.
- [ ] Every membership dialog state measures `766 / 425 / 341` at full desktop width.
- [ ] Product card contains actual cycle, unit price, optional original price, and annual monthly equivalent.
- [ ] Detail list contains actual credits, truthful renewal information, optional discount, and payable total.
- [ ] Team preview uses validated seats and keeps its editable inputs only on the right before order creation.
- [ ] Frozen checkout never reads the current plan after order creation.
- [ ] No-discount plans hide original and discount rows.
- [ ] Right-side progress, raw error, retry, provider, QR, and terminal states remain explicit.
- [ ] Mobile and tablet remain in bounds without hidden overflow.
- [ ] Focused, full Web, build, Go smoke, Nginx, production Chromium, and mutation gates pass.
- [ ] Local preview runs the new images with preserved data.
- [ ] No compatibility wrapper, fake financial fact, secret, generated artifact, or unrelated file enters commits.
