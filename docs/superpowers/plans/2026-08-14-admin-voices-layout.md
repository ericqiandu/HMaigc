# Admin Voices Layout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 `/admin/voices` 攄入后台统一数据页布局，并以真实 MiniMax Speech 渠道和音色事实展示概览、筛选、目录及可执行空状态。

**Architecture:** 保留 `VoicesPage` 现有请求、筛选、分页和 Drawer 状态，仅把顶层 JSX 接入 `AdminDataLayout`、`AdminMetricBand`、`AdminFilterSection` 与 `AdminContentSection`。指标由当前已成功读取的 `channels` 和 `voices` 纯派生；样式继续归属 `admin-feature-workspace.css`，不新增页面专用设计系统。

**Tech Stack:** React 19、TypeScript strict、Ant Design、React Router、Bun test、Happy DOM、PostCSS、现有后台语义 CSS Token。

## Global Constraints

- 只调整页面信息架构、共享表面、空状态入口和响应式表现。
- 不修改 API、DTO、后端、数据库、权限、积分价格、供应商协议或音色业务流程。
- 指标只使用已成功读取的真实渠道和当前音色目录；失败不得投影为零值。
- 所有 JSX/TSX 标签保留描述性 `className`，禁止新增 `any` 或 `as any`。
- 生产职责不超过 3 项，生产文件不超过 4 个，净新增生产代码不超过 260 行；超过预算 50% 停止实施。
- 不使用子代理：本计划各任务共享同一页面状态、测试夹具和 CSS owner，按仓库政策使用主代理内联执行。

---

## File Structure

- Create: `web/test/admin-voices-layout.test.tsx` — 真实组件结构、指标事实、错误与空渠道操作的回归契约。
- Modify: `web/src/pages/admin/voices/voices-page.tsx` — 共享数据页编排、真实指标和空状态入口。
- Modify: `web/src/pages/admin/admin-feature-workspace.css` — 音色内容区接缝、结果元数据和 1279/639 响应式规则。

### Task 1: 锁定音色数据页事实与结构

**Files:**
- Create: `web/test/admin-voices-layout.test.tsx`
- Modify: `web/src/pages/admin/voices/voices-page.tsx`

**Interfaces:**
- Consumes: `AdminDataLayout`、`AdminMetricBand`、`AdminMetric`、`AdminFilterSection`、`AdminContentSection`；现有 `listAdminChannels()` 和 `listAdminChannelVoices(channelId)` 响应。
- Produces: 固定区块顺序 `.admin-metric-band → .admin-filter-section → .admin-content-section`；四项指标和 `/admin/models` 空状态入口。

- [ ] **Step 1: 创建真实组件夹具和失败断言**

在 `web/test/admin-voices-layout.test.tsx` 使用 `mock.module` 注入两个 MiniMax Speech 渠道和三条音色：一条已发布 active、一条停用 active、一条已发布 failed。测试必须通过真实 `VoicesPage` 渲染验证：

```tsx
expect(Array.from(document.querySelector(".admin-data-layout")?.children ?? [], (node) => node.className)).toEqual([
    "admin-metric-band",
    "admin-filter-section",
    "admin-content-section",
]);
expect(document.querySelector(".admin-metric-band-list")?.textContent).toContain("MiniMax Speech 渠道2");
expect(document.querySelector(".admin-metric-band-list")?.textContent).toContain("当前渠道音色3");
expect(document.querySelector(".admin-metric-band-list")?.textContent).toContain("已发布音色2");
expect(document.querySelector(".admin-metric-band-list")?.textContent).toContain("需处理音色1");
expect(document.querySelector('[role="region"][aria-label="音色筛选"]')).not.toBeNull();
expect(document.querySelector(".admin-content-section-title")?.textContent).toBe("音色目录");
```

另加两个状态测试：渠道请求失败时 `.admin-metric-band` 不存在且错误可重试；渠道列表为空时表格空状态包含可访问名称“前往模型中心”的 `/admin/models` 链接。

- [ ] **Step 2: 运行定向测试并确认 RED**

Run:

```powershell
cd web
bun test test/admin-voices-layout.test.tsx
```

Expected: FAIL，原因是当前页面不存在 `.admin-data-layout`、指标带、内容区标题和空渠道操作。

- [ ] **Step 3: 接入共享布局并纯派生指标**

在 `voices-page.tsx` 增加共享布局导入；新增 `loadedVoiceChannelId`，在每次开始读取目标渠道前清空，在该请求成功写入 `voices` 后写入目标渠道 ID，无渠道时置为空字符串。用它阻止新渠道尚未成功读取时展示旧渠道指标：

```tsx
const [loadedVoiceChannelId, setLoadedVoiceChannelId] = useState("");
const voiceFactsReady = !channelId || loadedVoiceChannelId === channelId;
const publishedVoiceCount = voices.filter((voice) => voice.enabled).length;
const attentionVoiceCount = voices.filter((voice) =>
    voice.providerStatus === "failed" || voice.providerStatus === "uncertain" || voice.providerStatus === "missing",
).length;
```

渠道读取失败时，`AdminPageFrame` 内容只渲染 `AdminContentError`。否则顶层使用 `AdminDataLayout`，并且仅在 `voiceFactsReady` 时渲染以下指标带：

```tsx
<AdminMetricBand title="音色目录概览" description="查看 MiniMax Speech 渠道与当前音色发布状态。">
    <AdminMetric label="MiniMax Speech 渠道" value={`${channels.length} 个`} detail="当前可维护的音频渠道" />
    <AdminMetric label="当前渠道音色" value={`${voices.length} 个`} detail={selectedChannel?.name || "尚未选择渠道"} />
    <AdminMetric label="已发布音色" value={`${publishedVoiceCount} 个`} detail="用户端可见目录状态" />
    <AdminMetric label="需处理音色" value={`${attentionVoiceCount} 个`} detail="失败、待确认或供应商缺失" />
</AdminMetricBand>
```

把现有 `ListToolbar` 节点原样移动进 `<AdminFilterSection label="音色筛选">`。把现有错误、骨架、`TableSurface` 和 `Table` 节点原样移动进 `<AdminContentSection title="音色目录">`，并把当前渠道名称放进 `actions={<span className="admin-voice-content-meta">…</span>}`。禁止复制现有请求或筛选状态。

渠道首次读取失败时在 `AdminDataLayout` 外层内容位置只渲染 `AdminContentError`，不渲染指标。空渠道的 `AdminTableEmpty` 增加：

```tsx
action={<Button className="admin-voice-model-center-link" href="/admin/models">前往模型中心</Button>}
```

- [ ] **Step 4: 运行定向测试并确认 GREEN**

Run:

```powershell
cd web
bun test test/admin-voices-layout.test.tsx test/admin-data-layout.test.tsx test/admin-light-theme-contrast.test.ts
```

Expected: PASS；区块顺序、指标、错误、空渠道入口和共享主题契约均通过。

- [ ] **Step 5: 提交结构与事实改造**

```powershell
git add -- web/src/pages/admin/voices/voices-page.tsx web/test/admin-voices-layout.test.tsx
git diff --cached --check
git commit -m "refactor(admin): 统一音色管理数据布局"
```

### Task 2: 收敛音色页响应式和内容表面

**Files:**
- Modify: `web/src/pages/admin/admin-feature-workspace.css`
- Modify: `web/test/admin-voices-layout.test.tsx`

**Interfaces:**
- Consumes: Task 1 生成的 `.admin-voice-content-meta`、`.admin-voice-table-surface` 和 `.admin-voice-model-center-link`，以及共享 `.admin-filter-section` / `.admin-content-section`。
- Produces: 桌面紧凑栅格、内容区单层表面、移动端单列筛选和 44px 操作热区。

- [ ] **Step 1: 先增加 CSS 契约断言**

在 `admin-voices-layout.test.tsx` 读取 `admin-feature-workspace.css`，断言：

```tsx
expect(styles).toContain(".admin-workspace .admin-content-section-body > .admin-voice-table-surface");
expect(styles).toContain("margin: -12px -16px 0");
expect(styles).toContain(".admin-voice-content-meta");
expect(styles).toContain("@media (max-width: 1279px)");
expect(styles).toContain("@media (max-width: 639px)");
expect(styles).toContain("grid-template-columns: minmax(0, 1fr)");
expect(styles).toContain("min-height: 44px");
```

- [ ] **Step 2: 运行测试并确认 RED**

Run:

```powershell
cd web
bun test test/admin-voices-layout.test.tsx
```

Expected: FAIL，原因是内容区接缝、元数据和移动端空状态操作尚无样式 owner。

- [ ] **Step 3: 实现最小响应式样式**

在 `admin-feature-workspace.css` 的既有音色块内：

```css
.admin-voice-content-meta {
    color: var(--workspace-ui-text-tertiary);
    font-size: 12px;
    line-height: 18px;
}

.admin-workspace .admin-content-section-body > .admin-voice-table-surface {
    margin: -12px -16px 0;
    overflow: hidden;
    border-radius: 0;
}

@media (max-width: 639px) {
    .admin-voice-list-toolbar .workspace-list-toolbar-fields {
        grid-template-columns: minmax(0, 1fr);
    }

    .admin-voice-model-center-link {
        min-height: 44px;
    }
}
```

保留现有 1279px 两列、639px 单列和页面级操作等宽规则；删除被共享布局取代的 `.admin-voice-toolbar` / 私有表面冗余规则，不新增圆角套圆角。

- [ ] **Step 4: 运行定向测试并格式化**

Run:

```powershell
cd web
bunx prettier --write src/pages/admin/voices/voices-page.tsx src/pages/admin/admin-feature-workspace.css test/admin-voices-layout.test.tsx
bun test test/admin-voices-layout.test.tsx test/admin-data-layout.test.tsx test/admin-light-theme-contrast.test.ts test/admin-theme-scope.test.tsx
```

Expected: PASS，且 Prettier 不再产生差异。

- [ ] **Step 5: 提交响应式收敛**

```powershell
git add -- web/src/pages/admin/admin-feature-workspace.css web/test/admin-voices-layout.test.tsx
git diff --cached --check
git commit -m "fix(admin): 收敛音色管理响应式布局"
```

### Task 3: 生产构建与真实页面验收

**Files:**
- Verify only: `web/src/pages/admin/voices/voices-page.tsx`
- Verify only: `web/src/pages/admin/admin-feature-workspace.css`
- Verify only: `web/test/admin-voices-layout.test.tsx`

**Interfaces:**
- Consumes: Task 1–2 的最终页面和测试契约。
- Produces: 可复现的测试、构建、容器与浏览器验收证据。

- [ ] **Step 1: 执行最终 Web 门禁**

Run:

```powershell
cd web
bun test
bun run build
bunx prettier --check src/pages/admin/voices/voices-page.tsx src/pages/admin/admin-feature-workspace.css test/admin-voices-layout.test.tsx
```

Expected: 全量测试、TypeScript、Vite 构建和三项 bundle budget 全部 PASS。

- [ ] **Step 2: 重建无状态 Web 镜像**

Run:

```powershell
./scripts/local-compose.ps1 build web
./scripts/local-compose.ps1 up -d --no-deps --force-recreate --wait web
curl.exe -fsS http://127.0.0.1:3000/api/health
```

Expected: `hmaigc-local-web-1` 与既有 backend 均 healthy，`/api/health` 返回 `code: 0`；不创建、删除或迁移数据库。

- [ ] **Step 3: 验收真实浏览器矩阵**

使用内置浏览器检查 `/admin/voices`：

- 1440×900、1024×768、390×844。
- 日间与夜间主题。
- 区块顺序、四项真实指标、筛选布局、表格局部滚动、空渠道入口。
- 打开“新增目录音色”和“克隆音色”抽屉，确认标题、表单、关闭、脏状态和 44px 移动热区保持可用。
- 页面 `scrollWidth <= clientWidth`，只有表格内容区允许横向滚动。

- [ ] **Step 4: 最终自审与提交状态确认**

Run:

```powershell
git diff --check
git status --short
git log -3 --oneline
```

对照原始需求、设计规格、计划、最终 diff、API 边界和测试证据逐项核验。只报告并保留原有无关工作区项，不把 `docs/superpowers/specs/2026-08-13-canvas-media-node-polish-design.md` 的删除或 `.superpowers/` 纳入实现提交。
