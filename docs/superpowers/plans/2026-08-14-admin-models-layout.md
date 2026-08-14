# 模型中心「渠道与模型」页面统一排版实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. This page, its shared CSS, and its tests have overlapping ownership, so the work must stay sequential in the primary task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在不改变渠道、密钥、模型目录和调用契约的前提下，把 `/admin/models` 统一为“接入概览 → 渠道筛选 → 渠道目录”的后台数据页布局。

**Architecture:** 保留 `ChannelsPage` 对 URL、请求、编辑抽屉和模型管理的现有所有权，只把顶层 JSX 接入 `AdminDataLayout`、`AdminMetricBand`、`AdminFilterSection` 与 `AdminContentSection`。页面专属 CSS 仅负责渠道字段密度和响应式映射；加载、错误、空数据与保留旧数据刷新继续由原状态机决定。

**Tech Stack:** React 19、TypeScript strict、Ant Design、React Router、Bun Test、共享后台数据布局组件。

## Global Constraints

- 本轮只修改 `/admin/models` 的“渠道与模型”页面，不改筷子账号、价格与 Agent 或音色管理。
- 不修改 API、DTO、后端、数据库、权限、计费、模型发布规则或上游调用链。
- 密钥保持 write-only，测试和错误文案不得出现真实 Key。
- 首次读取失败不得显示零指标或空表；保留旧数据刷新失败必须继续展示旧目录和真实错误。
- 生产职责不超过 3 项、生产文件不超过 6 个、净新增生产代码不超过约 400 行。
- 390px 移动端主要操作不低于 44px，宽表只允许内容区内部滚动。

---

### Task 1: 用真实页面状态锁定共享数据布局契约

**Files:**
- Create: `web/test/admin-channels-layout.test.tsx`
- Modify: `web/src/pages/admin/channels/channels-page.tsx`

**Interfaces:**
- Consumes: `AdminDataLayout`、`AdminMetricBand`、`AdminMetric`、`AdminFilterSection`、`AdminContentSection`；`listAdminChannels()` 返回 `{ channels, total }`。
- Produces: `ChannelsPage` 的稳定 DOM 顺序 `.admin-data-layout > .admin-metric-band + .admin-filter-section + .admin-content-section`。

- [ ] **Step 1: 写成功目录与首次失败的组件测试**

使用完整 `ModelChannel` fixture 注入 `listAdminChannels`，渲染真实 `ChannelsPage`，断言以下字面事实：

```tsx
expect(Array.from(document.querySelector(".admin-data-layout")?.children ?? [], (node) => node.className)).toEqual([
    "admin-metric-band",
    "admin-filter-section",
    "admin-content-section",
]);
expect(document.querySelector(".admin-metric-band-list")?.textContent).toContain("渠道目录1 个渠道");
expect(document.querySelector(".admin-content-section-title")?.textContent).toBe("渠道目录");
expect(document.querySelector(".admin-channel-table")?.textContent).toContain("筷子科技");
```

第二个用例让 `listAdminChannels` 抛出 `渠道服务暂时不可用`，断言 `.admin-content-error` 显示原始原因，并且 `.admin-metric-band` 与 `.admin-channel-table` 均不存在。

- [ ] **Step 2: 运行测试并确认 RED**

Run: `cd web && bun test test/admin-channels-layout.test.tsx`

Expected: FAIL，成功用例缺少共享布局顺序，失败用例仍渲染私有概览或表格结构。

- [ ] **Step 3: 最小接入共享数据布局**

在 `channels-page.tsx` 中导入共享组件，并把现有成功分支重排为：

```tsx
<AdminDataLayout>
    <AdminMetricBand title="渠道接入概览" description="集中查看渠道、模型和配置完整性。">
        <AdminMetric label="渠道目录" value={`${total} 个渠道`} detail="当前筛选下的服务端结果" />
        <AdminMetric label="已启用渠道" value={`${enabledChannelCount} 个`} detail="可进入系统模型集合" />
        <AdminMetric label="已登记模型" value={`${visibleModelCount} 个`} detail="当前页渠道内模型总数" />
        <AdminMetric label="待完善配置" value={`${incompleteChannelCount} 个`} detail="缺少密钥或模型目录" />
    </AdminMetricBand>
    <AdminFilterSection label="渠道筛选条件">{existingToolbar}</AdminFilterSection>
    <AdminContentSection title="渠道目录" description="维护渠道连接、并发、启停和渠道内模型。" actions={existingResultCount}>
        {existingRefreshErrorAndTable}
    </AdminContentSection>
</AdminDataLayout>
```

首次加载失败时直接渲染 `AdminContentError`，不得构造上述指标；已有数据刷新失败时把错误置于 `AdminContentSection` 内并保留表格。

- [ ] **Step 4: 运行聚焦测试并确认 GREEN**

Run: `cd web && bun test test/admin-channels-layout.test.tsx`

Expected: PASS，2 个测试全部通过且没有未处理 Promise。

---

### Task 2: 收敛渠道页面视觉密度和响应式行为

**Files:**
- Modify: `web/src/pages/admin/admin-domain-workspace.css`
- Modify: `web/src/pages/admin/admin-responsive.css`
- Modify: `web/test/admin-channels-layout.test.tsx`

**Interfaces:**
- Consumes: Task 1 的 `.admin-channel-content`、`.admin-channel-list-toolbar`、`.admin-channel-table`、共享数据布局 class。
- Produces: 桌面、平板与移动端稳定的渠道筛选和表格局部滚动契约。

- [ ] **Step 1: 先增加响应式契约测试**

读取真实 CSS，断言渠道内容可收缩、共享内容区移除嵌套边界、移动端筛选和主要操作达到 44px：

```ts
expect(styles).toContain(".admin-workspace .admin-channel-content");
expect(styles).toContain("min-width: 0");
expect(styles).toContain(".admin-content-section-body > .admin-channel-table");
expect(responsiveStyles).toContain(".admin-workspace .admin-channel-list-toolbar");
expect(responsiveStyles).toContain("min-height: 44px");
```

- [ ] **Step 2: 运行测试并确认 RED**

Run: `cd web && bun test test/admin-channels-layout.test.tsx`

Expected: FAIL，缺少渠道页面新的共享内容和移动触控规则。

- [ ] **Step 3: 实现最小页面样式**

在 `admin-domain-workspace.css` 中只增加渠道页面映射：根容器和 Grid 子级允许收缩、共享内容区中的表格表面不重复圆角边框、渠道身份和 Base URL 保持单行截断、结果数量使用三级文本。

在 `admin-responsive.css` 的既有 `<1280px` 与 `<640px` 区域加入：筛选工具栏单列、搜索与 Select 宽度 `100%`、页面新增按钮和筛选控件最小高度 `44px`，不对页面使用 `overflow-x: hidden`。

- [ ] **Step 4: 运行聚焦测试并确认 GREEN**

Run: `cd web && bun test test/admin-channels-layout.test.tsx test/admin-data-layout.test.tsx test/admin-theme-scope.test.tsx`

Expected: PASS，渠道页面和共享布局测试全部通过。

---

### Task 3: 完整验收、镜像更新与提交

**Files:**
- Verify: `web/src/pages/admin/channels/channels-page.tsx`
- Verify: `web/src/pages/admin/admin-domain-workspace.css`
- Verify: `web/src/pages/admin/admin-responsive.css`
- Verify: `web/test/admin-channels-layout.test.tsx`

**Interfaces:**
- Consumes: Task 1–2 的页面结构与样式。
- Produces: 可提交、可本地预览且不触碰后端数据的 Web 版本。

- [ ] **Step 1: 运行受影响测试与生产构建**

Run:

```powershell
cd web
bun test test/admin-channels-layout.test.tsx test/admin-data-layout.test.tsx test/admin-theme-scope.test.tsx
bun run build
bunx prettier --check src/pages/admin/channels/channels-page.tsx src/pages/admin/admin-domain-workspace.css src/pages/admin/admin-responsive.css test/admin-channels-layout.test.tsx
```

Expected: 所有命令 exit 0，三项包体预算通过。

- [ ] **Step 2: 只重建无状态 Web 容器**

Run:

```powershell
& .\scripts\local-compose.ps1 build web
& .\scripts\local-compose.ps1 up -d --no-deps --wait web
```

Expected: `hmaigc-local-web-1` healthy；backend 容器未重建，`.local/data` 未变化。

- [ ] **Step 3: 真实浏览器验收**

在 `/admin/models` 验证 1440×900、1024×768、390×844 的日间与夜间主题：区块顺序正确、无页面横向溢出、移动控件 44px、宽表仅内容区滚动。依次执行搜索、接口筛选、状态筛选、刷新、打开新增抽屉、编辑已有渠道、关闭未保存抽屉、打开渠道模型管理并返回；不得保存或删除真实配置。

- [ ] **Step 4: 最终全量门禁和显式 review**

Run:

```powershell
cd web
bun test --reporter=dot
bun run build
git diff --check
```

逐项对照规格、计划、实际 diff、状态语义、密钥边界、响应式和镜像健康；任何缺口在同一集中修复轮完成后只重跑受影响聚焦门禁，再做一次完整确认。

- [ ] **Step 5: 精确提交实施文件**

```powershell
git add -- web/src/pages/admin/channels/channels-page.tsx web/src/pages/admin/admin-domain-workspace.css web/src/pages/admin/admin-responsive.css web/test/admin-channels-layout.test.tsx
git diff --cached --check
git commit -m "refactor(admin): 统一模型渠道页面排版"
```

不得暂存既有的文档删除、`.superpowers/` 或其他无关工作区改动；不执行 push、tag、merge 或 rebase。
