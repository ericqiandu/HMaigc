# Membership Checkout Reference Layout Implementation Plan

> **For agentic workers:** Execute task-by-task with strict RED → GREEN TDD. Do not add compatibility branches, fake payment data, LibLib branding, or payment-blocking agreement consent.

**Goal:** Rebuild the membership checkout to the approved compact reference dimensions and add an independently managed HMaigc membership agreement link beneath the real payment QR code.

**Architecture:** Extend the existing site legal-content aggregate with `membershipAgreement`, reuse the existing public legal page and admin legal editor, and keep payment behavior in the existing `PaymentCheckoutExperience`. Agreement publication is presentation-only and never enters order, checkout, transaction, or fulfillment decisions.

**Tech Stack:** Go, GORM, Gin, React, TypeScript, Ant Design QRCode, Bun tests, PostCSS, Playwright/Chromium, Docker/Nginx.

**Approved design:** `docs/superpowers/specs/2026-08-10-membership-payment-dialog-design.md`

## Global constraints

- Desktop shell: `766px`; target height about `472px`, with natural growth for real content.
- Desktop columns: `425px` order surface and `341px` payment surface.
- QR SVG: `112px`; complete white quiet-zone surface: `128px`.
- Tablet keeps the `425:341` ratio within `calc(100vw - 48px)`; mobile becomes one column within `calc(100vw - 32px)`.
- Only membership checkout shows `开通即代表同意《HMaigc会员服务协议》`; credit top-up does not.
- Published or unpublished agreement must never block order, checkout, transaction, QR, callback, or fulfillment.
- Public empty state is `会员服务协议尚未发布`.
- No checkbox, e-signature, agreement snapshot, automatic-renewal wording, or LibLib legal content.
- Preserve invariant QR black/white tokens and four-module quiet zone.
- Keep `LegalDocumentsConfigured()` scoped to registration's user agreement and privacy policy.
- No database migration: legal content remains in the existing site-setting JSON aggregate.
- No push, tag, image publication, or cloud upgrade in this implementation.

---

## Task 1: Extend the backend legal-content contract

**Files:**

- Modify: `backend/internal/service/site_settings.go`
- Modify: `backend/internal/service/site_settings_test.go`
- Modify: `docs/content/docs/backend/backend-database.mdx`

### Step 1: Write failing backend tests

Add focused tests proving:

1. `UpdateLegalContentSetting` accepts, sanitizes, persists, audits, and publicly returns `membershipAgreement`.
2. Invalid or oversized membership HTML fails explicitly without partially overwriting legal content.
3. `UpdateSiteSetting` preserves all three legal documents.
4. Legacy JSON without `membershipAgreement` reads as empty.
5. `LegalDocumentsConfigured()` remains true when the registration documents exist and membership agreement is empty.
6. Empty membership agreement is a valid saved/public state, not a registration or payment error.

Run:

```bash
cd backend
go test ./internal/service -run 'Test.*(Legal|SiteSetting).*MembershipAgreement' -count=1
```

Expected RED: missing fields and persistence behavior.

### Step 2: Implement one legal aggregate

Add `MembershipAgreement string` with JSON name `membershipAgreement` to:

- `LegalContentSettingRequest`
- `PublicSiteSetting`
- `siteSettingValue`

Update the existing paths to preserve, trim, sanitize, validate, persist, and publicly map it. Include only field-presence metadata in the existing legal audit; never log HTML. Leave `LegalDocumentsConfigured()` behavior unchanged.

Do not create a second setting key or payment-owned agreement store.

### Step 3: Document persistence

Update `docs/content/docs/backend/backend-database.mdx`: the existing `site` JSON has optional `membershipAgreement`; missing legacy values read empty; payment availability is independent of publication.

### Step 4: Verify

```bash
cd backend
go test ./internal/service -run 'Test.*(Legal|SiteSetting)' -count=1
go test ./... -count=1
go vet ./...
go build ./...
```

### Step 5: Commit

```bash
git add -- backend/internal/service/site_settings.go backend/internal/service/site_settings_test.go docs/content/docs/backend/backend-database.mdx
git diff --cached --check
git commit -m "feat(legal): 会员协议 - 增加独立后台发布内容"
```

---

## Task 2: Add admin editing and the public agreement page

**Files:**

- Modify: `web/src/services/api/site-settings.ts`
- Modify: `web/src/pages/admin/settings/legal-draft.ts`
- Modify: `web/src/pages/admin/settings/legal-settings-page.tsx`
- Modify: `web/src/pages/legal/legal-document-page.tsx`
- Modify: `web/src/router.tsx`
- Create: `web/src/constants/legal-documents.ts`
- Modify: `web/test/legal-draft.test.ts`
- Create: `web/test/membership-agreement-contract.test.tsx`

### Step 1: Write failing tests

Cover:

1. API types require `membershipAgreement`.
2. Draft creation, dirty comparison, reset, and save payload retain all three documents.
3. Admin shows three tabs and factual `0/3` through `3/3` publication status.
4. Public route is exactly `/legal/membership-agreement`.
5. Published HTML renders under `HMaigc会员服务协议`.
6. Empty content renders `会员服务协议尚未发布`.
7. Load failure remains an explicit failure, not an unpublished state.

Run:

```bash
cd web
bun test test/legal-draft.test.ts test/membership-agreement-contract.test.tsx
```

### Step 2: Introduce one typed route contract

Create `web/src/constants/legal-documents.ts`:

```ts
export const legalDocumentRoutes = {
    userAgreement: "/legal/user-agreement",
    privacyPolicy: "/legal/privacy-policy",
    membershipAgreement: "/legal/membership-agreement",
} as const;

export type LegalDocumentKind = keyof typeof legalDocumentRoutes;
```

Use it in the router, public page, admin editor, and payment link. Do not duplicate the membership URL.

### Step 3: Extend API, draft, and admin editor

- Add `membershipAgreement` to `SiteSettings` and the legal-update request.
- Extend pure draft functions to all three documents.
- Add a `会员服务协议` editor tab using existing rich text, dirty-state protection, preview, save, and audit-backed API.
- Count published only when trimmed saved content is non-empty.

### Step 4: Extend public rendering

- Register `/legal/membership-agreement`.
- Reuse `LegalDocumentPage`; select field/title/empty message by typed kind.
- Preserve sanitization, loading, retry, and explicit error semantics.

### Step 5: Verify

```bash
cd web
bun test test/legal-draft.test.ts test/membership-agreement-contract.test.tsx
bun test
bun run build
```

### Step 6: Commit

```bash
git add -- web/src/services/api/site-settings.ts web/src/pages/admin/settings/legal-draft.ts web/src/pages/admin/settings/legal-settings-page.tsx web/src/pages/legal/legal-document-page.tsx web/src/router.tsx web/src/constants/legal-documents.ts web/test/legal-draft.test.ts web/test/membership-agreement-contract.test.tsx
git diff --cached --check
git commit -m "feat(web): 会员协议 - 增加后台编辑与公开页面"
```

---

## Task 3: Rebuild checkout to the compact reference geometry

**Files:**

- Modify: `web/src/pages/payment/membership-checkout-summary.tsx`
- Modify: `web/src/pages/payment/payment-qr-panel.tsx`
- Modify: `web/src/pages/payment/payment-checkout.css`
- Modify: `web/src/pages/membership/membership-payment-dialog.tsx`
- Modify: `web/test/payment-checkout-visual-contract.test.ts`
- Modify: `web/test/membership-payment-dialog.test.ts`

### Step 1: Write failing presentation tests

Assert:

1. Dialog is `766px`, not `880px`.
2. Desktop columns are exactly `425px 341px` at full size.
3. Tablet keeps the `425:341` ratio without horizontal overflow.
4. Mobile is one column within `calc(100vw - 32px)` with natural vertical scroll.
5. QR uses `size={112}`, `marginSize={4}`, SVG, invariant black/white tokens, and a `128px` surface.
6. Active membership QR contains the agreement sentence and a link with `target="_blank"` and `rel="noopener noreferrer"`.
7. Credit top-up does not contain that link.
8. Payment UI never fetches agreement content or checks publication.
9. Summary uses frozen facts and does not invent price, discount, renewal, or consent.
10. Active QR remains provider-locked.

Run:

```bash
cd web
bun test test/payment-checkout-visual-contract.test.ts test/membership-payment-dialog.test.ts
```

Expected RED: old `880px`, `1fr 320px`, `184px` QR, and missing link.

### Step 2: Implement desktop geometry

- Set dialog width to `766`.
- Page shell: `width: min(calc(100% - 48px), 766px)`; dialog shell fills its modal body.
- Full desktop grid: `minmax(0, 425px) minmax(0, 341px)`.
- Keep one outer visible boundary/radius; internal surfaces use backgrounds and straight dividers.
- Use existing spacing/typography tokens.
- Do not fix height to `472px`; permit real content growth.

### Step 3: Implement tablet and mobile

- Tablet cap: `calc(100vw - 48px)` with proportional `425fr 341fr` columns.
- Below `768px`: one column, `calc(100vw - 32px)`, payment below order, natural vertical scroll.
- Do not hide horizontal overflow to fake success.
- Mobile close/provider/retry/team controls remain at least `44px`.

### Step 4: Resize the real QR

- Ant `QRCode`: `size={112}`, `marginSize={4}`, `type="svg"`.
- Total white surface: exactly `128px`.
- Keep only `--qr-foreground` and `--qr-background`.
- Never transform or replace provider `codeUrl`.

### Step 5: Add agreement link

In the active transaction branch:

- render `开通即代表同意` + `《HMaigc会员服务协议》`;
- use `legalDocumentRoutes.membershipAgreement`;
- open safely in a new tab;
- render only for `checkout.orderType === "membership"`;
- never change payable behavior based on document content.

Reading order: QR → countdown → agreement.

### Step 6: Verify

```bash
cd web
bun test test/payment-checkout-visual-contract.test.ts test/membership-payment-dialog.test.ts test/membership-agreement-contract.test.tsx
bun test
bun run build
```

### Step 7: Commit

```bash
git add -- web/src/pages/payment/membership-checkout-summary.tsx web/src/pages/payment/payment-qr-panel.tsx web/src/pages/payment/payment-checkout.css web/src/pages/membership/membership-payment-dialog.tsx web/test/payment-checkout-visual-contract.test.ts web/test/membership-payment-dialog.test.ts
git diff --cached --check
git commit -m "feat(web): 会员收银台 - 复刻紧凑双栏付款布局"
```

---

## Task 4: Enforce production-browser and release-note gates

**Files:**

- Modify: `web/scripts/membership-checkout-browser-assertions.mjs`
- Modify: `web/scripts/verify-membership-checkout-browser.mjs`
- Modify: `web/scripts/membership-checkout-browser-fixture.mjs`
- Modify: `CHANGELOG.md`

### Step 1: Add browser RED assertions

Extend the real Chromium matrix:

- desktop shell `766px` after animations settle;
- columns `425px` and `341px`;
- QR surface `128px`, SVG `112px`;
- tablet bounds at `768×1024`;
- mobile one-column bounds at `390×844`;
- no descendant outside shell and no hidden horizontal clipping;
- QR black/white in light/dark themes;
- agreement link text, href, target, rel, focus, and contrast;
- published route renders fixture HTML;
- unpublished route shows the empty state while QR remains visible;
- top-up omits the agreement;
- personal/team payment remain inside the same dialog.

Use fixture-only controls to switch published/empty state. Do not add production test branches.

### Step 2: Record RED and update fixture matrix

Run the existing production-browser command before production CSS changes and record geometry/link failures:

```bash
cd web
bun run verify:membership-checkout-browser
```

Add `membershipAgreement` to fixture site settings and published/unpublished cases. Update the expected case count from the reviewed enumerated case list, preserving all existing payment cases.

### Step 3: Run final gates

```bash
cd backend
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...

cd ../web
bun install --frozen-lockfile
bun test
bun run build
node --check scripts/membership-checkout-browser-assertions.mjs
node --check scripts/verify-membership-checkout-browser.mjs
```

Also run the existing Nginx checkout security sentinel, production Docker/Nginx Chromium matrix, PostgreSQL 17/Redis 7 payment integration gate, scoped formatter, and `git diff --check`. Verify temporary containers, networks, volumes, and images are removed.

The exact repository commands for the two integration gates are:

```bash
bash scripts/tests/verify-payment-checkout-nginx.test.sh
bash scripts/tests/run-payment-integration.sh --all
```

### Step 4: Update changelog

Under Unreleased document:

- compact `766px` checkout;
- `112px` QR in a `128px` white surface;
- independent HMaigc membership agreement admin/public entry;
- unpublished agreement does not block payment.

Do not change `VERSION`, tag, publish images, or claim cloud deployment.

### Step 5: Final review and commit

Review the approved spec against actual diff, Go/TS contracts, agreement/payment independence, measurements, QR scan safety, absence of LibLib content, and test evidence.

Stage the exact browser fixture plus:

```bash
git add -- web/scripts/membership-checkout-browser-assertions.mjs web/scripts/verify-membership-checkout-browser.mjs web/scripts/membership-checkout-browser-fixture.mjs CHANGELOG.md
git diff --cached --check
git commit -m "test(web): 会员收银台 - 锁定参考尺寸与协议回归"
```

The existing image workflow already runs `bun run verify:membership-checkout-browser`, so no workflow edit is required. Never use a broad directory add.

---

## Completion checklist

- [ ] Backend stores, sanitizes, audits, and returns `membershipAgreement`.
- [ ] Registration consent still depends only on user agreement and privacy policy.
- [ ] Admin edits and previews the independent membership agreement.
- [ ] Public route renders published or truthful unpublished state.
- [ ] Payment never reads agreement publication state.
- [ ] Membership QR shows the link; top-up does not.
- [ ] Desktop measures `766 / 425 / 341 / 128 / 112` after animations settle.
- [ ] Tablet/mobile are in bounds without hidden overflow.
- [ ] QR, provider lock, countdown, polling, and terminal states remain correct.
- [ ] Backend, Web, Nginx, PostgreSQL/Redis, and production Chromium gates pass.
- [ ] No secrets, artifacts, unrelated files, or LibLib content enter the diff.
- [ ] Commits are independently reversible and worktree is clean.
