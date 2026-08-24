# CDN Compression Release v1.0.52 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Correct the immutable CDN delivery gate so it enforces compression only for assets eligible for Alibaba Cloud CDN compression, then publish the same verified application content as `v1.0.52` without moving or overwriting failed tag `v1.0.51`.

**Architecture:** Keep CDN behavior authoritative: local build byte size determines whether an entry asset is compression-eligible, while HTTP protocol, MIME, immutable cache and exact CORS checks remain mandatory for every entry asset. Treat `v1.0.51` as an immutable failed release fact; advance release metadata and create a new tag only after the fix passes focused, full and remote gates.

**Tech Stack:** Bash, GitHub Actions, Vite build artifacts, Alibaba Cloud CDN/OSS, Git/GitHub CLI.

**Spec:** `scripts/verify-static-release-assets.sh`, `.github/workflows/publish-images.yml`, `VERSION`, and the failed tag workflow run `32677391302`.

## Global Constraints

- Do not delete, move or overwrite remote tag `v1.0.51` or its `hmaigc/web/releases/v1.0.51` objects.
- Require `br` or `gzip` only for JavaScript/CSS whose local byte size is within the CDN compression eligibility interval of 1 KiB through 10 MiB inclusive.
- Continue rejecting HTTP/1.1, redirects, invalid MIME, mutable cache headers, incorrect CORS origin/methods and missing eligible compression.
- Preserve `web/src/router.tsx` and `web/src/pages/prototype/` byte-for-byte and never stage them.
- No model, Agent runtime, billing, database or API contract changes.

---

### Task 1: Reproduce the CDN compression boundary

**Files:**
- Modify: `scripts/tests/verify-static-release-assets.test.sh`
- Test: `scripts/tests/verify-static-release-assets.test.sh`

**Interfaces:**
- Consumes: the existing fake `curl` response contract and local files under `$TEST_ROOT/dist/assets`.
- Produces: a regression case where a sub-1-KiB CSS response with no `Content-Encoding` is valid, while a 1-KiB JavaScript response with no encoding remains invalid.

- [ ] **Step 1: Make the JavaScript fixture compression-eligible**

Generate `assets/app.js` at exactly 1024 bytes and keep `assets/app.css` below 1024 bytes.

- [ ] **Step 2: Add a small-asset identity response**

Teach fake `curl` to emit an empty `content-encoding` only for the CSS URL when `FAKE_SMALL_ASSET_IDENTITY=true`, then run the verifier expecting success.

- [ ] **Step 3: Run the regression test to verify RED**

Run: `bash scripts/tests/verify-static-release-assets.test.sh`

Expected: FAIL with `入口资源缺少 Brotli/gzip 压缩` for `app.css`, proving the current verifier incorrectly rejects an ineligible small asset.

### Task 2: Enforce compression eligibility by local byte size

**Files:**
- Modify: `scripts/verify-static-release-assets.sh`
- Test: `scripts/tests/verify-static-release-assets.test.sh`

**Interfaces:**
- Consumes: `asset_path` already resolved against `DIST_DIR` and the response headers captured by `curl`.
- Produces: `verify_delivery_headers <asset-url> <header-file> <http-version> <asset-size>` with deterministic byte-boundary enforcement.

- [ ] **Step 1: Add explicit compression bounds**

Define byte constants `1024` and `10485760` for the inclusive CDN compression interval.

- [ ] **Step 2: Pass the real local asset size into header validation**

Read the file size with `wc -c < "$DIST_DIR/$asset_path"` after the existing file-presence check and pass it as the fourth validator argument.

- [ ] **Step 3: Preserve strict behavior for eligible assets**

Accept `br` or `gzip` for every size. Accept empty/identity only outside the inclusive interval; inside it, retain the current explicit failure message.

- [ ] **Step 4: Run focused GREEN verification**

Run: `bash scripts/tests/verify-static-release-assets.test.sh`

Expected: PASS, including the small identity success and eligible JavaScript identity rejection.

### Task 3: Advance immutable release metadata

**Files:**
- Modify: `VERSION`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: failed immutable `v1.0.51` tag and the corrected delivery gate.
- Produces: application version `v1.0.52` and a release-note entry describing the CDN boundary correction without claiming `v1.0.51` succeeded.

- [ ] **Step 1: Change the application version**

Replace `v1.0.51` with `v1.0.52` in `VERSION`.

- [ ] **Step 2: Add the v1.0.52 changelog section**

Under `未发布`, add `## v1.0.52 - 2026-08-24` with a delivery note that sub-1-KiB/over-10-MiB entry assets may use identity while eligible assets must use Brotli/gzip, and that all other CDN security/cache gates remain unchanged.

### Task 4: Review, integrate and publish

**Files:**
- Review only: all files changed by Tasks 1–3.

**Interfaces:**
- Consumes: focused GREEN result, repository test/build commands and GitHub release workflow.
- Produces: one focused fix commit, a green PR, immutable annotated tag `v1.0.52`, verified CDN assets, versioned container images and GitHub Release notes.

- [ ] **Step 1: Run release-focused checks**

Run: `bash scripts/tests/verify-static-release-assets.test.sh`, `bash scripts/extract-release-notes.sh v1.0.52`, and `git diff --check`.

- [ ] **Step 2: Run full local gates**

Run: `go test ./...` from `backend`, then `bun test` and `bun run build` from `web`.

- [ ] **Step 3: Perform explicit review**

Check requirements, actual diff, CDN size boundaries, failure semantics, immutable versioning, secrets, docs/tests, user-file hashes and staged scope. Apply at most one concentrated repair and one targeted re-review.

- [ ] **Step 4: Commit and open a focused PR**

Stage only this plan and Tasks 1–3 files. Commit as `fix(release): align CDN compression eligibility`, push the existing release branch and open a PR to `main`.

- [ ] **Step 5: Merge only after remote gates succeed**

Verify the PR is `CLEAN / MERGEABLE` and all required checks are successful, then merge without deleting the local branch or user files.

- [ ] **Step 6: Publish the new immutable version**

Create annotated tag `v1.0.52` on the merged `main` commit, verify `VERSION` and CHANGELOG from the tagged object, push only that tag, and wait for CDN asset verification, all three versioned images and GitHub Release notes to succeed.

## Self-Review

- Spec coverage: reproduces the exact production failure, preserves every non-compression delivery gate, records immutable version advancement and includes end-to-end release verification.
- Placeholder scan: no deferred steps or unspecified implementation decisions remain.
- Interface consistency: the fourth `asset-size` argument is produced and consumed within the same script; tests exercise both sides of the 1-KiB boundary.
