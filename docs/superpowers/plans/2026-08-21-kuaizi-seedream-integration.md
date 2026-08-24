# Kuaizi Seedream 5.0 Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `seedream5.0lite` and `seedream5.0pro` to the Kuaizi managed model catalog with correct async generation, dynamic dimensions, traceable failures, and existing commercial task/billing controls.

**Architecture:** Introduce a dedicated Kuaizi Seedream adapter and route managed image tasks by provider family. Extend the provider capability DTO with a generic resolution-to-pixel-budget map so Seedream dimensions are derived without hardcoding model names in the web client.

**Tech Stack:** Go 1.x, Gin service layer, GORM repository contracts, React 19, TypeScript, Vitest, Vite.

**Spec:** `docs/superpowers/specs/2026-08-21-kuaizi-seedream-integration.md`

## Global Constraints

- No frontend model allowlist or model-name semantic branch.
- No `any` additions in TypeScript.
- Every upstream task returns exactly one image; canvas batch fan-out remains the sole multi-image owner.
- No fallback model, fallback endpoint, synthetic result, or swallowed upstream error.
- Published models remain unavailable until explicit pricing is configured.
- Existing Kuaizi credential freezing, watermark snapshot, billing, resource persistence, and audit boundaries remain authoritative.

---

### Task 1: Dynamic Seedream capability and sizing contract

**Files:**
- Modify: `backend/internal/service/provider_registry.go`
- Modify: `backend/internal/service/provider_registry_test.go`
- Modify: `backend/internal/service/admin.go`
- Modify: `web/src/stores/use-config-store.ts`
- Modify: `web/src/lib/image-model-capabilities.ts`
- Modify: `web/test/canvas-image-generation-settings.test.ts`

**Interfaces:**
- Produces: `ProviderModelSpec.ResolutionPixels map[string]int64`
- Produces: `PublicProviderCapabilities.ResolutionPixels map[string]int64`
- Produces: `ProviderModelCapabilities.resolutionPixels: Record<string, number>`
- Produces: `buildImageDimensions(ratio, resolution, resolutionPixels?) string`

- [ ] **Step 1: Write failing Go registry tests**

Add table assertions proving that both Seedream models exist under family `seedream`, publish image capability, controlled watermark, 2K/3K target-pixel facts, one output per task, and model-specific reference limits.

- [ ] **Step 2: Run the focused Go tests and verify RED**

Run: `go test ./internal/service -run 'TestProviderRegistryPublishesSeedream' -count=1`

Expected: FAIL because the Seedream family and `ResolutionPixels` contract do not exist.

- [ ] **Step 3: Implement registry and public DTO support**

Add the generic map to registry/public DTOs, deep-clone it, and register Lite/Pro with the exact capabilities in the spec. Preserve empty non-nil capability collections where the API currently promises arrays.

- [ ] **Step 4: Run the focused Go tests and verify GREEN**

Run: `go test ./internal/service -run 'TestProviderRegistryPublishesSeedream|TestProviderRegistryJSONEmitsCapabilityCollectionsAsArrays' -count=1`

Expected: PASS.

- [ ] **Step 5: Write failing web dimension tests**

Add literal expectations for Seedream pixel-budget dimensions, including `1:1 / 2K -> 2048x2048`, `16:9 / 3K -> 4096x2304`, and preservation of the existing GPT Image 2 `16:9 / 2K -> 2048x1152` behavior without a pixel map.

- [ ] **Step 6: Run the focused web test and verify RED**

Run: `pnpm --dir web vitest run test/canvas-image-generation-settings.test.ts`

Expected: FAIL because the function has no pixel-budget input and the config type lacks `resolutionPixels`.

- [ ] **Step 7: Implement generic pixel-budget sizing**

Derive aligned dimensions from `sqrt(targetPixels * ratio)` and `sqrt(targetPixels / ratio)`. Pass the selected model's dynamic capability map from normalization and resolution matching. Keep the existing longest-edge algorithm when the map is empty.

- [ ] **Step 8: Run affected web tests and verify GREEN**

Run: `pnpm --dir web vitest run test/canvas-image-generation-settings.test.ts`

Expected: PASS.

### Task 2: Seedream asynchronous provider adapter

**Files:**
- Create: `backend/internal/service/provider_kuaizi_seedream.go`
- Create: `backend/internal/service/provider_kuaizi_seedream_test.go`

**Interfaces:**
- Consumes: `kuaiziProviderModelSpec(modelKey) (ProviderModelSpec, bool)`
- Consumes: `insertFrozenWatermark(body, field, runtime)`
- Produces: `runKuaiziSeedreamTask(ctx context.Context, input canvasGenerationInput) (map[string]interface{}, error)`
- Produces: `runKuaiziSeedreamTaskWithPollInterval(ctx, input, interval)` for deterministic polling tests

- [ ] **Step 1: Write failing payload contract tests**

Cover Lite's explicit `sequential_image_generation=disabled`, Pro's omission of that field, public reference URLs, frozen watermark, JPEG output, count-one enforcement, model-specific reference limits, and documented pixel boundaries.

- [ ] **Step 2: Run the focused adapter tests and verify RED**

Run: `go test ./internal/service -run 'TestKuaiziSeedream' -count=1`

Expected: FAIL because the adapter functions do not exist.

- [ ] **Step 3: Implement request validation and create payload**

Implement exact model selection, structural validation, public reference URL extraction, dimensions validation, watermark insertion, and the Lite/Pro sequential-field distinction. Do not add prompt optimization or interactive-coordinate editing because the current shared task contract exposes neither setting.

- [ ] **Step 4: Run payload tests and verify GREEN**

Run: `go test ./internal/service -run 'TestKuaiziSeedreamCreatePayload' -count=1`

Expected: PASS.

- [ ] **Step 5: Write failing async lifecycle tests**

Use an HTTP test server to exercise create, running, succeeded, failed, non-zero business code, trace propagation, task-ID mismatch, multiple image URLs, invalid URL, and non-image download behavior.

- [ ] **Step 6: Run lifecycle tests and verify RED**

Run: `go test ./internal/service -run 'TestKuaiziSeedreamTask' -count=1`

Expected: FAIL because polling and response decoding are not implemented.

- [ ] **Step 7: Implement async create, polling, trace-aware errors, and image download**

Reuse the established Kuaizi base-URL validator, request context markers, polling deadline, retryable HTTP status policy, binary download validator, and data-URL result contract. Enforce exactly one output URL.

- [ ] **Step 8: Run all adapter tests and verify GREEN**

Run: `go test ./internal/service -run 'TestKuaiziSeedream' -count=1`

Expected: PASS.

### Task 3: Family-aware Kuaizi dispatch and final verification

**Files:**
- Modify: `backend/internal/service/provider_kuaizi_compatible_worker.go`
- Create or modify: `backend/internal/service/provider_kuaizi_compatible_worker_test.go`

**Interfaces:**
- Consumes: `kuaiziProviderFamilyForModel(modelKey)`
- Consumes: `runKuaiziSeedreamTask(ctx, input)`
- Produces: a single family-aware managed-provider dispatch path

- [ ] **Step 1: Write failing family dispatch tests**

Prove that `gpt-image2` keeps its current adapter, `seedream` selects the new adapter, video/text families remain unchanged, and an unknown image family fails explicitly.

- [ ] **Step 2: Run the worker tests and verify RED**

Run: `go test ./internal/service -run 'TestProcessKuaiziCompatibleTaskDispatch' -count=1`

Expected: FAIL because image dispatch is still capability-only and always invokes GPT Image 2.

- [ ] **Step 3: Implement family-aware dispatch**

Resolve the registered family and switch image adapters by family. Keep capability validation and emit an explicit error for an unimplemented family.

- [ ] **Step 4: Run worker and adapter tests and verify GREEN**

Run: `go test ./internal/service -run 'TestProcessKuaiziCompatibleTaskDispatch|TestKuaiziSeedream|TestKuaiziGPTImage2' -count=1`

Expected: PASS.

- [ ] **Step 5: Run stable milestone gates**

Run:

```powershell
go test ./internal/service ./internal/repository ./internal/database
pnpm --dir web vitest run test/canvas-image-generation-settings.test.ts
pnpm --dir web build
```

Expected: all commands PASS without warnings attributable to this change.

- [ ] **Step 6: Review scope and contracts**

Compare the approved spec, actual diff, provider DTOs, registry publication, watermark behavior, failure evidence, and tests. Confirm that unrelated `web/src/router.tsx` and `web/src/pages/prototype/` changes are not staged.

- [ ] **Step 7: Commit the coherent implementation**

Stage only the Seedream spec, plan, implementation, and tests, then commit with:

```text
feat(models): integrate Kuaizi Seedream 5.0
```
