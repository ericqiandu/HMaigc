# 会员管理页面统一排版 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把会员管理页统一为“会员运营概览 → 模块导航 → 当前模块内容”的后台数据页结构，同时完整保留六个会员运营模块的业务行为。

**Architecture:** `MembershipAdminPage` 继续拥有模块选择和按需加载状态；现有套餐、商城、订单、发票、交易和回调组件继续拥有各自业务内容。顶层只复用 `AdminDataLayout`、`AdminMetricBand` 与 `AdminContentSection`，CSS 负责移除会员页私有概览和嵌套表面，不引入新状态层或兼容路径。

**Tech Stack:** React 19、TypeScript strict、Ant Design、Bun Test、Happy DOM、Vite、Docker Compose。

## Global Constraints

- 不修改 API、DTO、后端、数据库、计费、支付、权限或模块按需加载契约。
- 页面顺序固定为“概览 → 模块导航 → 当前模块内容”。
- 首次失败不渲染伪造数据；刷新失败保留上次成功事实和原地重试。
- 每个独立区块最多一层可见圆角边界，不新增渐变、重阴影或私有卡片体系。
- 390–1279px 的模块选择和主要操作热区不低于 44px，页面级横向溢出必须为零。
- 生产职责不超过 3 项，生产文件不超过 5 个，净新增生产代码不超过约 350 行。
- 不得暂存或修改既有删除项 `docs/superpowers/specs/2026-08-13-canvas-media-node-polish-design.md` 与未跟踪 `.superpowers/`。
- 最终只重建无状态 Web 容器；主项目 `.local/data` 与后端容器必须保持不变。

---

### Task 1: 用真实页面回归锁定统一结构

**Files:**
- Create: `web/test/admin-membership-layout.test.tsx`
- Read: `web/test/admin-referrals-layout.test.tsx`
- Read: `web/src/pages/admin/components/admin-data-layout.tsx`

**Interfaces:**
- Consumes: `MembershipAdminPage` 的现有公开渲染行为、`AdminReferralProgramData` 测试所使用的 Happy DOM + data router 模式。
- Produces: 对 `.admin-data-layout`、`.admin-metric-band`、`.admin-content-section`、六项 tab、移动模块选择和失败状态的 DOM 契约。

- [ ] **Step 1: 写真实页面结构失败测试**

在 `web/test/admin-membership-layout.test.tsx` 中使用 `setup-happy-dom`、`createMemoryRouter`、`ConfigProvider` 和 `App` 渲染真实 `MembershipAdminPage`。Mock 会员与支付读取接口，返回至少一个套餐，并断言：

```tsx
const layout = document.querySelector(".admin-data-layout");
expect(Array.from(layout?.children ?? [], (node) => node.className)).toEqual(["admin-metric-band", "admin-content-section"]);
expect(layout?.querySelector(".admin-metric-band .admin-data-section-title")?.textContent).toBe("会员运营概览");
expect(layout?.querySelector(".admin-content-section-title")?.textContent).toBe("会员业务模块");
expect(Array.from(layout?.querySelectorAll('[role="tab"]') ?? [], (tab) => tab.textContent)).toEqual([
    "套餐与权益",
    "商城展示",
    "会员订单",
    "开票处理",
    "支付交易",
    "回调审计",
]);
expect(layout?.querySelector(".admin-membership-table")?.textContent).toContain("标准版");
```

- [ ] **Step 2: 写首次失败真实性测试**

让 `listAdminMembershipPlans` 抛出 `new Error("会员套餐服务暂时不可用")`，断言：

```tsx
expect(document.querySelector(".admin-content-error")?.textContent).toContain("套餐配置加载失败");
expect(document.querySelector(".admin-content-error")?.textContent).toContain("会员套餐服务暂时不可用");
expect(document.querySelector(".admin-membership-table")).toBeNull();
expect(document.querySelector(".admin-metric-value")?.textContent).not.toBe("0 个套餐");
```

- [ ] **Step 3: 写响应式 CSS 契约测试**

读取 `admin-domain-workspace.css` 与 `admin-responsive.css`，断言共享表面、移动触控和旧私有概览删除：

```tsx
expect(domainStyles).toContain(".admin-workspace .admin-membership-content .admin-metric-band-list");
expect(domainStyles).toContain(".admin-workspace .admin-content-section-body > .admin-membership-tabs");
expect(domainStyles).not.toContain(".admin-workspace .admin-membership-context {");
expect(responsiveStyles).toContain(".admin-membership-mobile-section");
expect(responsiveStyles).toContain("min-height: 44px");
```

- [ ] **Step 4: 运行 focused 测试并确认 RED**

Run:

```powershell
cd web
bun test test/admin-membership-layout.test.tsx test/admin-data-layout.test.tsx
```

Expected: 新测试因真实页面尚未渲染共享 `AdminDataLayout` / `AdminContentSection` 或仍保留 `.admin-membership-context` 而失败；既有共享布局测试继续通过。

---

### Task 2: 硬切到共享会员数据布局

**Files:**
- Modify: `web/src/pages/admin/membership/membership-page.tsx`
- Modify: `web/src/pages/admin/admin-domain-workspace.css`
- Modify: `web/src/pages/admin/admin-responsive.css`
- Test: `web/test/admin-membership-layout.test.tsx`

**Interfaces:**
- Consumes: `AdminDataLayout({ children })`、`AdminMetricBand({ title, description, queue, children })`、`AdminMetric({ label, value, detail? })`、`AdminContentSection({ title, description?, actions?, children })`。
- Produces: `MembershipAdminPage` 的固定两层结构：共享运营概览和包含模块导航/当前内容的共享业务区。

- [ ] **Step 1: 替换私有会员概览**

在 `membership-page.tsx` 导入共享数据布局组件，用以下结构替换 `.admin-membership-context`：

```tsx
<AdminDataLayout>
    <AdminMetricBand
        title="会员运营概览"
        description="套餐目录与待处理订单均来自当前服务端事实"
        queue={<span className="admin-membership-operating-note">支付到账只通过验签回调或渠道查单履约；异常交易请在支付交易中发起对账。</span>}
    >
        <AdminMetric label="套餐目录" value={plansState.loaded && !plansState.error ? `${plansState.data.length} 个套餐` : "等待读取"} />
        <AdminMetric label="待核款订单" value={ordersState.loaded && !ordersState.error ? `${pendingOrderCount} 笔待处理` : "进入订单后读取"} />
    </AdminMetricBand>
</AdminDataLayout>
```

紧接 `AdminMetricBand`，在同一个 `AdminDataLayout` 内创建 `AdminContentSection`，标题固定为“会员业务模块”，说明固定为“按业务职责切换配置与运营记录，数据仅在进入对应模块后读取。”；把当前 `.admin-membership-mobile-section` 的 `Select` 和 `.admin-membership-tabs` 的 `Tabs` 两个完整 JSX 节点原样移动到该 section 的 children 内。保持 `activeSection`、六项 `Tabs.items`、加载分支、错误分支、表格、表单和所有事件处理函数不变。

- [ ] **Step 2: 删除私有概览样式并映射共享表面**

在 `admin-domain-workspace.css`：

- 删除 `.admin-membership-context*` 的私有网格、边界、背景和排版规则。
- 让 `.admin-membership-content .admin-metric-band-list` 在桌面使用两列。
- 为 `.admin-membership-operating-note` 使用语义次级文字，不使用独立卡片。
- 让 `.admin-content-section-body > .admin-membership-tabs` 通过负边距与共享 section 边界对齐。
- 将会员 tabs 内容中的 `.app-table-surface` 顶层圆角和外框移交给共享 `AdminContentSection`，表格自身横向滚动保留。

在 `admin-responsive.css`：

- 继续在 `max-width: 767px` 隐藏桌面 tab nav、展示 `.admin-membership-mobile-section`。
- 确认移动下拉最小高度 44px，内容根节点 `min-width: 0`，不产生页面级横向滚动。

- [ ] **Step 3: 运行 focused 测试并确认 GREEN**

Run:

```powershell
cd web
bun test test/admin-membership-layout.test.tsx test/admin-data-layout.test.tsx
```

Expected: PASS，且新测试明确覆盖结构顺序、首次错误、六模块导航和响应式 CSS 契约。

- [ ] **Step 4: 格式化受影响文件**

Run:

```powershell
cd web
bunx prettier --write src/pages/admin/membership/membership-page.tsx src/pages/admin/admin-domain-workspace.css src/pages/admin/admin-responsive.css test/admin-membership-layout.test.tsx
```

Expected: 命令退出码 0；没有格式化无关文件。

---

### Task 3: 集中审查、最终门禁与本地交付

**Files:**
- Review: `web/src/pages/admin/membership/membership-page.tsx`
- Review: `web/src/pages/admin/admin-domain-workspace.css`
- Review: `web/src/pages/admin/admin-responsive.css`
- Review: `web/test/admin-membership-layout.test.tsx`

**Interfaces:**
- Consumes: Task 2 的稳定页面结构与现有会员 API/状态函数。
- Produces: 一个职责单一、可独立回滚的会员管理排版提交与可查看的本地 Web 镜像。

- [ ] **Step 1: 执行唯一一次集中自审**

逐项核对：

- `git diff` 不包含 API、DTO、后端、数据库或业务事件函数变化。
- 六项 module key、加载函数、写操作、脏状态和对账函数完整保留。
- 生产改动不超过 5 个文件和约 350 行净新增；超过 50% 立即停止并重新设计。
- 首次失败不出现表格或零值假指标；刷新失败仍保留上次成功数据。
- 没有 `any`、硬编码候选、兼容分支或新增隐式回退。

- [ ] **Step 2: 执行最终 Web 门禁**

Run:

```powershell
cd web
bun test
bun run build
```

Expected: 全量测试 0 失败；TypeScript、Vite build 和三项 bundle budget 全部通过。

- [ ] **Step 3: 精确暂存并提交**

Run:

```powershell
git add -- web/src/pages/admin/membership/membership-page.tsx web/src/pages/admin/admin-domain-workspace.css web/src/pages/admin/admin-responsive.css web/test/admin-membership-layout.test.tsx
git diff --cached --check
git commit -m "refactor(admin): 统一会员管理页面排版"
```

Expected: 提交只包含本计划的生产文件和测试；现有文档删除、`.superpowers/` 与构建产物不进入提交。

- [ ] **Step 4: 仅重建本地 Web 容器**

在工作树根执行，并显式绑定主项目数据路径：

```powershell
$env:CANVAS_DATA_PATH='E:\新版短剧制作\open-ai-canvas\.local\data'
$backendIdBefore = docker inspect hmaigc-local-backend-1 --format '{{.Id}}'
docker compose -p hmaigc-local build web
docker compose -p hmaigc-local up -d --no-deps --wait web
$backendIdAfter = docker inspect hmaigc-local-backend-1 --format '{{.Id}}'
```

Expected: Web/Backend 均 healthy；`$backendIdBefore -eq $backendIdAfter`；`/api/health` 和 `/admin/membership` 返回 200。

- [ ] **Step 5: 真实浏览器回归**

在已登录本地管理后台检查 1280px 与 390px：

- DOM 顺序为概览、模块导航、当前模块内容。
- 逐项切换六个模块，确认模块标题和真实内容更新，且未触发写请求。
- 首屏、表格和移动端均无页面级横向溢出。
- 移动模块选择器和主要操作保持至少 44px 热区。
- 浏览器控制台无本次改动引入的新错误。

Expected: 全部通过；临时 viewport 恢复默认，本地会员管理页保留供用户查看。
