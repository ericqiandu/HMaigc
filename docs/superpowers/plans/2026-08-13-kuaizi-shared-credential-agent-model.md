# Kuaizi Shared Credential And Agent Model Implementation Plan

> **For agentic workers:** Execute inline in the current worktree. Do not use subagent-driven development because credential migration, task freezing, model publication, and Agent selection share one transaction and DTO boundary.

**Goal:** Hard-cut Kuaizi to one account-level Base URL/API Key and configure one administrator-selected Agent model for all users.

**Architecture:** Reuse the existing provider credential/version tables with one reserved account credential family; preserve disabled legacy credentials only for already-frozen tasks. Store the selected Agent channel-model ID in `SystemSetting`, validate it against the live catalog on write and read, and expose only the validated selection in the authenticated system-channel response.

**Tech Stack:** Go, GORM, PostgreSQL/SQLite, Gin, React, TypeScript, Ant Design, Bun.

## Global Constraints

- Hard cutover: no family-level credential write/verify route and no implicit Agent model fallback.
- Migration decrypts and compares every active legacy Key before any mutation; one mismatch rolls back and blocks startup.
- Existing frozen tasks retain legacy credential/version IDs; every new task binds the account credential.
- No database column or table addition; use existing provider and system-setting facts.
- Two commits: shared credential cutover, then Agent default model and hidden user selector.

---

### Task 1: Account credential migration and runtime cutover

**Files:**
- Create: `backend/internal/service/provider_account_credential.go`
- Create: `backend/internal/service/provider_account_credential_test.go`
- Modify: `backend/internal/repository/provider_account.go`
- Modify: `backend/internal/service/provider_credentials.go`
- Modify: `backend/internal/service/provider_credentials_view.go`
- Modify: `backend/internal/service/provider_model_publication.go`
- Modify: `backend/internal/service/provider.go`
- Modify: `backend/internal/handler/provider_accounts.go`
- Modify: `backend/cmd/server/main.go`
- Modify: `web/src/services/api/provider-accounts.ts`
- Modify: `web/src/pages/admin/providers/kuaizi-provider-page.tsx`

**Interfaces:**
- Produces `MigrateKuaiziAccountCredential() error`, singular credential routes, and one credential DTO.
- Preserves `FrozenProviderRuntime(task)` for disabled legacy credential versions.

- [ ] Write focused tests that create equal and unequal legacy active credentials and assert transactional migration, rollback, model rebinding, legacy disablement, and frozen legacy reads.
- [ ] Run `go test ./internal/service ./internal/repository -run 'KuaiziAccountCredential|FrozenProviderRuntime' -count=1` and confirm the new contract fails because the migration and singular API do not exist.
- [ ] Implement the reserved account credential, transactional migration, singular save/verify APIs, runtime lookup by bound credential ID, and singular admin DTO/UI.
- [ ] Re-run the focused Go tests and the provider Web tests until they pass.
- [ ] Run the isolated PostgreSQL provider integration gate, review the diff, and commit `refactor(provider): 统一筷子账号凭据`.

### Task 2: Administrator-selected default Agent model

**Files:**
- Create: `backend/internal/service/agent_model_setting.go`
- Create: `backend/internal/service/agent_model_setting_test.go`
- Modify: `backend/internal/handler/finance.go`
- Modify: `backend/internal/service/admin.go`
- Modify: `web/src/services/api/wallet.ts`
- Modify: `web/src/pages/admin/channels-page.tsx`
- Modify: `web/src/components/canvas/canvas-assistant-panel.tsx`
- Modify: `web/src/components/canvas/canvas-agent-composer-controls.tsx`
- Modify: `web/src/components/canvas/canvas-agent-selection-summary.tsx`
- Test: `web/test/canvas-agent-text-model.test.tsx`

**Interfaces:**
- Produces admin GET/PUT setting endpoints and `agentDefaultModel` in authenticated model catalog facts.
- Canvas consumes only `agentDefaultModel`; missing/invalid selection blocks sending with explicit copy.

- [ ] Write Go tests for administrator authorization, eligible model validation, audit persistence, and read-time invalidation after disable/unprice/delete.
- [ ] Write Web tests proving the admin selector saves only eligible text models and the canvas renders no user model selector or fallback.
- [ ] Run focused Go/Web tests and confirm failures are caused by missing setting and remaining selector UI.
- [ ] Implement the setting service/routes/admin UI, catalog projection, canvas strict consumption, and remove the user selection state/menu/summary.
- [ ] Re-run focused tests, then run full Go tests, full Web tests, Web build, Prettier, and diff checks.
- [ ] Perform one requirements/diff/security review and commit `feat(agent): 增加全站默认模型配置`.

### Task 3: Local data migration and browser acceptance

**Files:**
- No production files expected.

- [ ] Back up the canonical local data directory metadata and verify the compose bind before rebuilding.
- [ ] Rebuild the local backend/web through `scripts/local-compose.ps1`; confirm startup migration completes and both services are healthy.
- [ ] In the administrator UI, verify one shared Kuaizi Key, select DeepSeek as the default Agent model, and save.
- [ ] In the canvas, verify the selector is absent, DeepSeek is used, and missing/disabled configuration fails explicitly without choosing another model.
- [ ] Confirm wallet and audit facts, clean transient test containers, and report the two commits without pushing.
