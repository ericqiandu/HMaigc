# 后台页面排版优化实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 建立后台数据页统一排版组件，并把数据概览改造成“指标 → 筛选 → 趋势 → 明细”的商业运营级样板。

**Architecture:** 新增无业务状态的共享排版组件，由数据概览继续持有请求、筛选、图表、Tabs 与 Table 状态。共享 CSS 统一纵向节奏、连续指标带、内容分区和三档响应式规则，不引入新依赖、不改变后端契约。

**Tech Stack:** React 19、TypeScript strict、Ant Design 6、Recharts、现有 workspace 语义 Token、Bun Test、Vite。

## Global Constraints

- 不改变后台路由、接口、权限、URL 筛选状态或业务行为。
- 不引入 ProComponents、Headless UI 或其他新布局依赖。
- 生产职责不超过 3 项，生产文件不超过 8 个，净新增生产代码目标不超过 500 行。
- 单个新增生产文件不超过 300 行，禁止 `any` 和旧行为兼容层。
- 数据页顺序固定为“概览指标 → 条件筛选 → 数据内容”。
- 加载、失败、空数据和成功内容保持互斥真实状态。
- 表格横向溢出只发生在表格自身内容区。
- 最终必须执行全量测试、生产构建、本地镜像重建和 1440×900、1024×768、390×844 浏览器验收。
- 不改后端、数据库、DTO、计费或运营数据。

## 文件结构

- Create: `web/src/pages/admin/components/admin-data-layout.tsx` — 共享数据页排版语义组件，不拥有业务状态。
- Modify: `web/src/pages/admin/components/analytics-panel.tsx` — 接入共享组件，保留现有请求与交互逻辑。
- Modify: `web/src/pages/admin/admin-workspace.css` — 桌面共享排版、指标带和内容区样式。
- Modify: `web/src/pages/admin/admin-responsive.css` — 390px、1024px 响应式规则。
- Test: `web/test/admin-data-layout.test.tsx` — 共享组件语义与可访问性。
- Test: `web/test/admin-pro-layout.test.tsx` — 数据概览顺序、筛选和响应式契约。

---

### Task 1: 建立共享数据页排版语义

**Files:**
- Create: `web/src/pages/admin/components/admin-data-layout.tsx`
- Test: `web/test/admin-data-layout.test.tsx`

**Interfaces:**
- Produces: `AdminDataLayout({ children })`
- Produces: `AdminMetricBand({ title, description, queue, children })`
- Produces: `AdminMetric({ label, value, detail })`
- Produces: `AdminFilterSection({ label, children })`
- Produces: `AdminContentSection({ title, description, actions, children })`
- All components accept React nodes only and own no request, URL or selection state.

- [ ] **Step 1: Write the failing semantic tests**

Add tests that render one complete data layout and assert:

```tsx
render(
  <AdminDataLayout>
    <AdminMetricBand title="运行概览" description="用户活跃度、任务量与服务性能" queue={<span>队列 0</span>}>
      <AdminMetric label="活跃用户" value="1" detail="DAU 0" />
    </AdminMetricBand>
    <AdminFilterSection label="统计筛选"><input aria-label="模型" /></AdminFilterSection>
    <AdminContentSection title="使用趋势" description="真实请求趋势"><div>图表</div></AdminContentSection>
  </AdminDataLayout>,
);

expect(screen.getByRole("region", { name: "运行概览" })).toBeTruthy();
expect(screen.getByRole("region", { name: "统计筛选" })).toBeTruthy();
expect(screen.getByRole("heading", { name: "使用趋势", level: 2 })).toBeTruthy();
```

Also assert the metric uses `dt/dd`, optional actions render once, and every JSX element has a descriptive className.

- [ ] **Step 2: Run focused RED**

Run: `bun test test/admin-data-layout.test.tsx`

Expected: FAIL because `admin-data-layout.tsx` does not exist.

- [ ] **Step 3: Implement the minimal semantic components**

Use focused function components with exact exported prop types:

```tsx
export function AdminDataLayout({ children }: { children: ReactNode }) {
  return <div className="admin-data-layout">{children}</div>;
}

export function AdminMetricBand({ title, description, queue, children }: AdminMetricBandProps) {
  const headingId = useId();
  return (
    <section className="admin-metric-band" aria-labelledby={headingId}>
      <header className="admin-data-section-header">
        <div className="admin-data-section-copy">
          <h2 id={headingId} className="admin-data-section-title">{title}</h2>
          <p className="admin-data-section-description">{description}</p>
        </div>
        {queue ? <div className="admin-metric-band-queue">{queue}</div> : null}
      </header>
      <dl className="admin-metric-band-list">{children}</dl>
    </section>
  );
}

export function AdminMetric({ label, value, detail }: AdminMetricProps) {
  return (
    <div className="admin-metric-item">
      <dt className="admin-metric-label">{label}</dt>
      <dd className="admin-metric-value">{value}</dd>
      {detail ? <dd className="admin-metric-detail">{detail}</dd> : null}
    </div>
  );
}

export function AdminFilterSection({ label, children }: AdminFilterSectionProps) {
  return <section className="admin-filter-section" aria-label={label}>{children}</section>;
}

export function AdminContentSection({ title, description, actions, children }: AdminContentSectionProps) {
  const headingId = useId();
  return (
    <section className="admin-content-section" aria-labelledby={headingId}>
      <header className="admin-data-section-header">
        <div className="admin-data-section-copy">
          <h2 id={headingId} className="admin-data-section-title">{title}</h2>
          {description ? <p className="admin-data-section-description">{description}</p> : null}
        </div>
        {actions ? <div className="admin-data-section-actions">{actions}</div> : null}
      </header>
      <div className="admin-content-section-body">{children}</div>
    </section>
  );
}
```

The implementation must use `section`, `header`, `h2`, `dl`, `dt`, `dd` and `aria-labelledby` rather than generic nested cards.

- [ ] **Step 4: Run focused GREEN**

Run: `bun test test/admin-data-layout.test.tsx`

Expected: PASS.

- [ ] **Step 5: Commit the isolated component**

```bash
git add web/src/pages/admin/components/admin-data-layout.tsx web/test/admin-data-layout.test.tsx
git commit -m "feat(admin): 增加后台数据页排版组件"
```

---

### Task 2: 将数据概览接入统一排版

**Files:**
- Modify: `web/src/pages/admin/components/analytics-panel.tsx`
- Modify: `web/test/admin-pro-layout.test.tsx`

**Interfaces:**
- Consumes: Task 1 的五个共享排版组件。
- Preserves: `AnalyticsPanelProps`、`adminApi.analyticsOverview`、URL 查询参数、刷新、CSV 导出、图表、Tabs 和 Table 契约。

- [ ] **Step 1: Write the failing structure test**

Extend `admin-pro-layout.test.tsx` to assert the rendered data overview contains, in DOM order:

```ts
const text = container.textContent ?? "";
expect(text.indexOf("运行概览")).toBeLessThan(text.indexOf("商业指标"));
expect(text.indexOf("商业指标")).toBeLessThan(text.indexOf("时间范围"));
expect(text.indexOf("时间范围")).toBeLessThan(text.indexOf("使用趋势"));
expect(text.indexOf("使用趋势")).toBeLessThan(text.indexOf("模型分析"));
```

Assert there are exactly two metric bands, one named filter region and two content sections. Retain the existing checks for page actions, filters, loading error and preserved refresh data.

- [ ] **Step 2: Run focused RED**

Run: `bun test test/admin-pro-layout.test.tsx`

Expected: FAIL because the analytics page still renders bespoke metric and section markup.

- [ ] **Step 3: Replace bespoke markup with shared components**

In `analytics-panel.tsx`:

- Wrap successful content with `AdminDataLayout`.
- Render existing running and commercial data through two `AdminMetricBand` instances.
- Keep the existing queue fact in the running band header.
- Wrap existing `ListToolbar` with `AdminFilterSection label="统计筛选"`.
- Wrap the Recharts block in `AdminContentSection title="使用趋势"`.
- Wrap Tabs/Table in `AdminContentSection title="分析明细"`.
- Remove duplicate local section heading and metric markup only after every consumer is moved.
- Do not modify API calls, callbacks, memoized columns, CSV serialization or filter values.

- [ ] **Step 4: Run focused GREEN**

Run: `bun test test/admin-data-layout.test.tsx test/admin-pro-layout.test.tsx`

Expected: PASS with all earlier analytics behavior tests still green.

- [ ] **Step 5: Commit the data overview cutover**

```bash
git add web/src/pages/admin/components/analytics-panel.tsx web/test/admin-pro-layout.test.tsx
git commit -m "refactor(admin): 统一数据概览页面结构"
```

---

### Task 3: 统一桌面与响应式排版样式

**Files:**
- Modify: `web/src/pages/admin/admin-workspace.css`
- Modify: `web/src/pages/admin/admin-responsive.css`
- Modify: `web/test/admin-pro-layout.test.tsx`

**Interfaces:**
- Consumes: `.admin-data-layout`、`.admin-metric-band`、`.admin-filter-section`、`.admin-content-section` 语义类。
- Produces: 1440px 横向指标、1024px 自适应指标/筛选、390px 单列筛选与局部表格滚动的确定性样式。

- [ ] **Step 1: Write the failing CSS contract assertions**

Assert the CSS contains:

```ts
expect(css).toContain(".admin-data-layout");
expect(css).toContain(".admin-metric-band-list");
expect(css).toContain(".admin-filter-section");
expect(css).toContain(".admin-content-section");
expect(responsiveCss).toContain("grid-template-columns: 1fr");
```

Parse the relevant rules or use stable exact snippets to confirm:

- Data layout uses one consistent section gap.
- Metric band is one continuous surface with separators.
- Filter section has no nested decorative card border.
- At `max-width: 1279px`, metric and filter grids wrap without page overflow.
- At `max-width: 639px`, filter fields become one column and controls keep 44px minimum height.

- [ ] **Step 2: Run focused RED**

Run: `bun test test/admin-pro-layout.test.tsx`

Expected: FAIL because the new class rules do not exist.

- [ ] **Step 3: Implement semantic desktop styles**

In `admin-workspace.css`:

- Use `24px` desktop section gap and existing workspace surface/divider tokens.
- Give each metric band one surface only; metrics use `border-left` separators except the first.
- Use tabular numbers for values; retain existing semantic success/warning/error text.
- Keep the filter section structurally flat and reuse existing `workspace-list-toolbar` controls.
- Give content sections a shared header rhythm, with charts and tables directly below it.
- Delete obsolete analytics-only selectors after confirming no production references remain.

- [ ] **Step 4: Implement responsive styles**

In `admin-responsive.css`:

- At `max-width: 1279px`, change metric list and filter fields to responsive wrapping.
- At `max-width: 639px`, use one-column filter fields, 16px page padding and 44px control height.
- Preserve table overflow within `.ant-table-content` / `.ant-table-body`.
- Do not add page-level horizontal clipping as a substitute for correct sizing.

- [ ] **Step 5: Run focused GREEN and static checks**

Run:

```bash
bun test test/admin-data-layout.test.tsx test/admin-pro-layout.test.tsx
rg -n "admin-analytics-metric-section|admin-analytics-section-heading" web/src/pages/admin web/test
```

Expected: tests PASS; retired selector search returns no production usages.

- [ ] **Step 6: Commit the visual system**

```bash
git add web/src/pages/admin/admin-workspace.css web/src/pages/admin/admin-responsive.css web/test/admin-pro-layout.test.tsx
git commit -m "style(admin): 统一数据页排版与响应式密度"
```

---

### Task 4: 商业级验收、镜像和最终审查

**Files:**
- Verify only; modify production files only if a current-diff defect is found within budget.

**Interfaces:**
- Validates the complete design against the committed diff and running local environment.

- [ ] **Step 1: Run focused and full Web gates**

Run:

```bash
cd web
bun test test/admin-data-layout.test.tsx test/admin-pro-layout.test.tsx
bun test
bun run build
```

Expected: all tests and bundle budgets PASS.

- [ ] **Step 2: Check scope and code quality**

Run:

```bash
git diff --check 86dd390..HEAD
git diff --stat 86dd390..HEAD
rg -n "as any|: any\\b|TODO|FIXME|@ant-design/pro-components" \
  web/src/pages/admin/components/admin-data-layout.tsx \
  web/src/pages/admin/components/analytics-panel.tsx \
  web/src/pages/admin/admin-workspace.css \
  web/src/pages/admin/admin-responsive.css
```

Expected: no forbidden matches; production file count <= 8; production net additions <= 750 lines (budget plus 50% fuse).

- [ ] **Step 3: Rebuild the local images safely**

Before rebuild, snapshot file paths, byte sizes and UTC modification times under the canonical `E:/新版短剧制作/open-ai-canvas/.local/data` path. Then run from the main repository root:

```powershell
pwsh -File .\scripts\local-compose.ps1 up -d --build --wait
```

Verify backend/web containers are healthy, `/api/health` and `/admin` return HTTP 200, and mounts still bind the exact canonical `.local/data` path. Never create a worktree-local data directory.

- [ ] **Step 4: Run real-browser responsive acceptance**

Use the authenticated localhost admin tab and verify at:

- 1440×900: two metric bands precede filters; chart and details align to the same content width.
- 1024×768: mobile top bar is active; metrics and filters wrap without overlap.
- 390×844: filter controls form one column; page has no horizontal overflow; table scroll stays local.

Exercise refresh, one filter change, tab switching and settings drawer open/close. Confirm no console errors.

- [ ] **Step 5: Perform one final review**

Review the original request, `2026-08-14-admin-page-layout-design.md`, this plan, actual diff, tests, browser facts and data mount facts. Classify any finding as current-diff defect, existing debt or new scope. Fix only current-diff defects within budget; do not start iterative redesign.

- [ ] **Step 6: Commit any final in-budget correction**

If no correction is needed, do not create an empty commit. If correction is needed:

```bash
git add <only-current-diff-files>
git diff --cached --check
git commit -m "fix(admin): 修正后台数据页排版验收问题"
```
