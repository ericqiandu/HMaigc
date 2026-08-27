# Homepage Critical Request Consolidation Implementation Plan

> **Execution:** Use `superpowers:test-driven-development` inline. This task shares the existing Vite manifest and bundle-budget contract, so it stays in the current workspace and is not delegated.

**Goal:** Reduce cold homepage request fanout without changing visuals, business APIs, auth behavior, Agent behavior, or user-media object storage.

**Architecture:** Keep the current same-origin delivery and intent-first route prefetch architecture. Add one narrowly scoped Rolldown group for the Ant Design `App`/`ConfigProvider` subtree that the root synchronously imports, while leaving route-only Ant Design modules split. Tighten the manifest gate to preserve the measured improvement.

**Tech Stack:** Vite 8/Rolldown, React 19, Ant Design 6, Bun test.

**Spec:** `docs/superpowers/specs/2026-08-24-mainland-web-performance-design.md`; bounded extension approved in chat on 2026-08-27.

## Constraints

- Production budget: 2 files, less than 60 net new production lines.
- No OSS/CDN changes, no API/database changes, no new dependency, no visual or interaction change.
- The group may include only the synchronously required root UI-provider dependency subtree. It must not merge all `antd` route modules into one eager chunk.
- Existing raw/gzip budgets remain unchanged. Improvement is accepted only when entry requests are at most 24 and homepage static requests are at most 30.

### Task 1: Define the critical UI group under RED

**Files:**
- Create: `web/test/vite-code-splitting.test.ts`
- Modify: `web/vite.config.ts`

- [x] Add a failing test that requires an exported `app-ui-core` group, matches `antd/es/app` and `antd/es/config-provider`, rejects route-only modules such as `antd/es/table`, and recursively includes only that dependency subtree.
- [x] Run the focused test and record RED because the group does not exist.
- [x] Implement the minimal group with lower priority than React/icons.
- [x] Run the focused test and record GREEN.

### Task 2: Measure and lock the production topology

**Files:**
- Modify: `web/scripts/check-bundle-budget.mjs`

- [x] Build the production Web app and inspect the real manifest closure.
- [x] If request counts do not meet 24/30 or gzip exceeds existing budgets, revert the grouping design and report the evidence instead of loosening gates.
- [x] Tighten entry/home request budgets around the achieved topology with bounded headroom, never above 24/30.
- [x] Run the build again so the final artifact proves both request and byte budgets.

### Task 3: Review and verification

- [x] Run the focused splitting and bundle-domain tests.
- [x] Run Web tests, TypeScript production build, and `git diff --check`.
- [x] Compare before/after manifest counts and gzip bytes.
- [x] Perform one explicit review against the user request, this plan, actual diff, failure behavior, media/deployment boundaries, and test evidence.
- [x] Do not commit, push, tag, release, or deploy without a separate current authorization.
