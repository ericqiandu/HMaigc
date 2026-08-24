# Agent 文生视频生产契约修复 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让支持文生视频的动态模型在没有分镜首帧时直接创建真实视频 Task，并保证审批前冻结的输入、报价、执行和恢复事实完全一致。

**Architecture:** 在 Provider 能力目录中增加显式 `supportsTextToVideo` 事实；`production.render` 冻结阶段根据同镜分镜 Resource 与该能力选择并冻结 `videoInputMode + videoInputResourceId`。执行阶段只消费冻结事实：`storyboard` 必须绑定精确就绪图片，`text_to_video` 必须保持空 Resource；不支持文生视频且缺少 Resource 时在出现付费审批前明确失败，缺失显式模式的历史载荷也明确失败。

**Tech Stack:** Go、GORM、SQLite、现有 Agent Runtime/Task/Billing/Artifact Ledger、Go testing

**Spec:** `docs/superpowers/specs/2026-08-23-agent-native-streaming-chat-design.md`

## Global Constraints

- 自动模式仍必须逐项确认每个付费媒体 Artifact。
- 不生成前置图片，不得被本地代码解释为允许 Agent 自动增加图片任务。
- 失败必须保留稳定错误码，不得自动切换模型或静默改变已批准输入。
- Task、BillingOrder、Resource、Artifact Ledger、Canvas revision/CAS 和 delivery verifier 继续走现有单一路径。
- 不修改或校正本地历史积分数据；历史 `uncertain` 账单只报告事实并交由运营核对。
- 工作区现有用户改动必须保留；只修改并暂存本计划相关 hunk。

---

### Task 1: 发布文生视频能力事实

**Files:**
- Modify: `backend/internal/service/provider_registry.go`
- Modify: `backend/internal/service/admin.go`
- Test: `backend/internal/service/agent_runtime_text_to_video_test.go`

**Interfaces:**
- Produces: `ProviderModelSpec.SupportsTextToVideo bool`
- Produces: `PublicProviderCapabilities.SupportsTextToVideo bool`
- Consumes: 现有 Seedance/Kling Provider 真实的零参考图请求能力

- [x] **Step 1: Write the failing provider capability test**

```go
if !seedance.Models[0].SupportsTextToVideo {
    t.Fatal("Seedance text-to-video capability is not published")
}
```

- [x] **Step 2: Run the focused test and verify RED**

Run: `go test ./internal/service -run 'TestVideoProvidersPublishTextToVideoCapability' -count=1`

Expected: FAIL because the capability fields do not exist.

- [x] **Step 3: Add the minimal explicit capability and public projection**

```go
type ProviderModelSpec struct {
    SupportsTextToVideo bool `json:"supportsTextToVideo"`
}

type PublicProviderCapabilities struct {
    SupportsTextToVideo bool `json:"supportsTextToVideo"`
}
```

Set the fact to `true` only for currently implemented Seedance and Kling video adapters, and copy it through `publicProviderModelCapabilities`.

- [x] **Step 4: Run focused tests and verify GREEN**

Run: `go test ./internal/service -run 'TestVideoProvidersPublishTextToVideoCapability' -count=1`

Expected: PASS.

### Task 2: Freeze and execute the exact video input mode

**Files:**
- Modify: `backend/internal/agentruntime/production.go`
- Modify: `backend/internal/service/agent_runtime_render.go`
- Modify: `backend/internal/service/agent_runtime_render_task.go`
- Test: `backend/internal/service/agent_runtime_production_test.go`

**Interfaces:**
- Produces: `ProductionRenderArguments.VideoInputMode + VideoInputResourceID`
- Consumes: `PublicProviderCapabilities.SupportsTextToVideo`
- Preserves: existing `FrozenRenderQuote`, task identity and billing fingerprint

- [x] **Step 1: Write failing execution tests**

Cover both observable contracts:

`TestProductionRenderBuildsTextToVideoTaskWithoutStoryboardResource` 断言 `text_to_video + 空 Resource ID` 生成零参考图任务，并拒绝缺少显式 mode 的历史冻结 JSON；`TestProductionStoryboardResourceAcceptsCommittedReadyArtifact` 断言 `storyboard + 精确 Resource ID` 只生成一项对应参考图输入。

Also cover freeze behavior: unsupported text-to-video + missing storyboard returns `production_prerequisite_missing` before approval, while supported text-to-video freezes an empty `videoInputResourceId`.

- [x] **Step 2: Run the focused tests and verify RED**

Run: `go test ./internal/service -run 'TestProductionRender.*(TextToVideo|FrozenStoryboard|Prerequisite)' -count=1`

Expected: FAIL because video execution unconditionally loads the sibling storyboard Resource.

- [x] **Step 3: Add the minimal frozen input contract**

```go
type ProductionRenderArguments struct {
    VideoInputMode       ProductionVideoInputMode `json:"videoInputMode,omitempty"`
    VideoInputResourceID string `json:"videoInputResourceId,omitempty"`
}
```

During freeze, resolve a ready same-shot storyboard Resource once. Freeze its ID if present. If absent, allow an empty ID only when `supportsTextToVideo=true`; otherwise return `production_prerequisite_missing` before quote/approval.

During execution, use only the frozen mode/ID pair. `text_to_video` requires an empty ID; `storyboard` requires a non-empty ID that still resolves to a ready image in the same user/team scope. Missing mode, invalid combinations, changed assets and non-query database errors fail explicitly without falling back to text-to-video.

- [x] **Step 4: Run focused tests and verify GREEN**

Run: `go test ./internal/service -run 'TestProductionRender|TestProductionArtifact|TestAgentRuntimeProduction' -count=1`

Expected: PASS.

### Task 3: Synchronize runtime instructions and architecture documentation

**Files:**
- Modify: `backend/internal/service/agent_runtime.go`
- Modify: `backend/internal/service/agent_runtime_test.go`
- Modify: `README.md`
- Modify: `docs/content/docs/backend/backend-database.mdx`
- Modify: `docs/content/docs/pending-test.mdx`

**Interfaces:**
- Consumes: `callableModels[].providerCapabilities.supportsTextToVideo`
- Produces: one unambiguous Agent rule for choosing text-to-video versus a frozen storyboard input

- [x] **Step 1: Write the failing prompt contract assertion**

```go
if !strings.Contains(systemPrompt, "supportsTextToVideo=true") {
    t.Fatal("runtime prompt does not describe text-to-video")
}
```

- [x] **Step 2: Run the focused test and verify RED**

Run: `go test ./internal/service -run 'TestAgentRuntimeSystemPrompt' -count=1`

Expected: FAIL because the prompt currently says every video continues from a storyboard Resource.

- [x] **Step 3: Update the single runtime contract and docs**

Document these exact facts: the server—not the model—selects and freezes `videoInputResourceId`; empty input is legal only for a published text-to-video model; media approval precedes Task/Billing creation; no model fallback occurs; image/video Artifact generation remains separately approved.

- [x] **Step 4: Run focused tests and documentation checks**

Run: `go test ./internal/service -run 'TestAgentRuntimeSystemPrompt|TestProductionRender' -count=1`

Expected: PASS.

### Task 4: Final review and verification

**Files:**
- Review only: every task-related diff and test file

**Interfaces:**
- Verifies: provider catalog -> frozen approval JSON -> Task typed input -> provider adapter -> Artifact/Resource -> canvas commit

- [x] **Step 1: Run backend focused and full gates**

Run:

```text
go test ./internal/agentruntime ./internal/service -count=1
go test ./... -count=1
go test -race ./internal/agentruntime ./internal/service -count=1
go build ./...
```

- [x] **Step 2: Run workspace integrity gates**

Run: `git diff --check`

If code files changed and Graphify is installed, run: `graphify update .`; otherwise record the unavailable command without claiming it ran.

- [x] **Step 3: Perform explicit review**

Check original request, this plan, actual diff, API/JSON contract, database impact, permissions, billing idempotency, error codes, docs, tests, and preservation of unrelated worktree changes. Apply one concentrated repair pass, then one targeted re-review.

- [x] **Step 4: Rebuild local containers and rerun the real automatic-mode flow**

Use the existing local data directory and the in-app browser. Approve exactly one 5-second video quote. Verify a real provider request ID, succeeded media Task, ready video Resource, canvas revision increment and video node URL. If available credits remain insufficient or historical `uncertain` orders block reservation, report that external billing-state blocker and do not mutate credits.

Result: backend image rebuilt against the existing `.local/data` and became healthy; the signed-in canvas reloaded at revision 64, dynamic Seedance video models were visible, and the browser console contained no errors. The wallet exposed about 9.1 available credits and the previous real run remains terminal `insufficient_credits`; because another Agent decision chain plus the 5-credit video reservation cannot reach a successful terminal state, no second billed request was submitted and no credit or historical `uncertain` order was mutated.
