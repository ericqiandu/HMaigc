# HMaigc 管理后台 Pro 化第一阶段 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在不改业务接口、数据库和创作端主题的前提下，将管理后台硬切为默认浅色、可独立切换深色、符合 Ant Design Pro 信息架构的统一外壳，并把数据概览做成首个真实样板页。

**Architecture:** 新增唯一的后台主题作用域，由 `AdminThemeProvider` 同时拥有浏览器持久化、后台语义变量、Ant Design `ConfigProvider` 和 Portal 容器；`AdminShell` 与 `AdminPageFrame` 继续作为唯一公共结构。数据概览保留现有请求、筛选、刷新、导出和统计口径，只调整标题操作区、内容顺序、状态反馈与主题化图表。

**Tech Stack:** React 19、TypeScript 7、React Router 8、Ant Design 6、Recharts 3、Bun Test、Happy DOM、Vite 8、Docker Compose。

## Global Constraints

- 继续使用现有 React、Vite、React Router 与 Ant Design；不引入 Umi、ProLayout、MUI、Mantis、Headless UI 或第二套组件系统。
- 后台主题仅允许 `light | dark`，首次默认 `light`，使用独立浏览器持久化键，禁止修改 `infinite-canvas:theme_store`。
- 后台公共结构仅由 `AdminShell`、`AdminPageFrame` 与 `AdminThemeProvider` 提供，不保留旧外壳平行分支。
- 桌面侧栏展开 236px、折叠 64px；顶部栏 56px；页面最大宽 1304px，桌面左右内边距 32px；小于 1280px 使用 320px 抽屉。
- 数据概览顺序固定为：标题与页面操作、运行与商业指标、筛选工具栏、使用趋势、标签页与明细表。
- 不修改 API、数据库、权限、审计、计费、任务状态、统计口径、创作首页、项目、素材、技能与画布。
- 页面不得新增裸色值；后台原始颜色只允许集中定义在后台主题语义映射中，业务组件消费 CSS 变量。
- 所有 JSX/TSX 标签必须带描述性 `className`；禁止 `any` 和 `as any`。
- 第一阶段生产职责保持 3 项；预计 7 个生产文件、2 个测试文件，净新增或替换 500–800 行；超过 12 个生产文件、1200 行或涉及接口/数据库即停止并重新拆分。
- 这些任务共享 `AdminShell`、主题状态与同一组后台 CSS，按仓库约束必须使用主代理顺序执行；不得启用多代理实现/审查循环。
- 日常 RED/GREEN 只跑定向测试；全量 Web、构建、Docker 和三视口双主题浏览器矩阵只在最终稳定里程碑执行一次。

## File Map

### Production files

- Create `web/src/pages/admin/admin-theme.tsx`: 后台主题类型、独立持久化、Ant Design 主题映射、Portal 容器与 `useAdminTheme`。
- Modify `web/src/pages/admin/index.tsx`: 把 `AdminProvider` 和 `AdminShell` 放入唯一后台主题根。
- Modify `web/src/pages/admin/components/admin-shell.tsx`: 顶栏主题按钮、移动主题按钮、页面操作 Portal 插槽及 Pro 化结构。
- Modify `web/src/pages/admin/components/analytics-panel.tsx`: 页面操作上移、指标/筛选顺序、显式加载失败状态与主题化图表。
- Modify `web/src/pages/admin/admin-workspace.css`: 后台明暗语义 Token、外壳、页头、页面标题和公共表面。
- Modify `web/src/pages/admin/admin-art-layout.css`: 数据概览指标、筛选、趋势、表格的样板布局。
- Modify `web/src/pages/admin/admin-responsive.css`: 1024/390 响应式、44px 热区、表格局部横向滚动。

### Test files

- Create `web/test/admin-theme-scope.test.tsx`: 独立主题持久化、首次浅色、创作主题不受影响、Portal 容器和切换行为。
- Create `web/test/admin-pro-layout.test.tsx`: 页面框架操作插槽、数据概览真实 DOM 顺序、刷新和错误/加载状态、关键 CSS 契约。

---

### Task 1: 建立后台独立主题作用域

**Files:**
- Create: `web/src/pages/admin/admin-theme.tsx`
- Modify: `web/src/pages/admin/index.tsx:1-32`
- Modify: `web/src/pages/admin/admin-workspace.css:1-72`
- Test: `web/test/admin-theme-scope.test.tsx`

**Interfaces:**
- Consumes: `getAntThemeConfig(dark: boolean): ThemeConfig`、现有根设计 Token、`AdminProvider`、`AdminShell`。
- Produces: `AdminThemeName`、`ADMIN_THEME_STORAGE_KEY`、`readAdminTheme(storage)`、`AdminThemeProvider`、`useAdminTheme()`；hook 同时返回 `getPortalContainer(): HTMLElement`。

- [ ] **Step 1: 写独立主题的失败测试**

```tsx
import "./setup-happy-dom";
import { afterEach, beforeAll, describe, expect, test } from "bun:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";

let RootProvider: typeof import("../src/pages/admin/admin-theme").AdminThemeProvider;
let useAdminTheme: typeof import("../src/pages/admin/admin-theme").useAdminTheme;
let adminStorageKey: string;
let root: Root | null = null;

beforeAll(async () => {
    const module = await import("../src/pages/admin/admin-theme");
    RootProvider = module.AdminThemeProvider;
    useAdminTheme = module.useAdminTheme;
    adminStorageKey = module.ADMIN_THEME_STORAGE_KEY;
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
    localStorage.clear();
});

function Probe() {
    const { theme, setTheme } = useAdminTheme();
    return createElement(
        "button",
        { className: "admin-theme-probe", onClick: () => setTheme(theme === "light" ? "dark" : "light") },
        theme,
    );
}

describe("admin theme scope", () => {
    test("defaults to light and never mutates the creation theme preference", async () => {
        localStorage.setItem("infinite-canvas:theme_store", JSON.stringify({ state: { theme: "dark" } }));
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);
        await act(async () => root?.render(createElement(RootProvider, null, createElement(Probe))));

        const button = document.querySelector<HTMLButtonElement>(".admin-theme-probe");
        expect(button?.textContent).toBe("light");
        expect(document.querySelector("[data-admin-theme='light']")).not.toBeNull();
        await act(async () => button?.click());
        expect(localStorage.getItem(adminStorageKey)).toBe("dark");
        expect(localStorage.getItem("infinite-canvas:theme_store")).toContain('"dark"');
    });
});
```

- [ ] **Step 2: 运行测试并确认 RED**

Run:

```powershell
Set-Location web
bun test test/admin-theme-scope.test.tsx
```

Expected: FAIL，因为 `admin-theme.tsx`、`AdminThemeProvider` 与后台持久化键尚不存在。

- [ ] **Step 3: 实现后台主题类型、持久化与 Ant Design 作用域**

在 `web/src/pages/admin/admin-theme.tsx` 建立以下明确接口：

```tsx
import { App, ConfigProvider, type ThemeConfig } from "antd";
import zhCN from "antd/locale/zh_CN";
import { createContext, useContext, useMemo, useRef, useState, type ReactNode } from "react";

import { getAntThemeConfig } from "@/lib/app-theme";

export type AdminThemeName = "light" | "dark";
export const ADMIN_THEME_STORAGE_KEY = "hmaigc:admin-theme";

type AdminThemeContextValue = {
    theme: AdminThemeName;
    setTheme: (theme: AdminThemeName) => void;
    getPortalContainer: () => HTMLElement;
};

export function readAdminTheme(storage: Pick<Storage, "getItem">): AdminThemeName {
    const stored = storage.getItem(ADMIN_THEME_STORAGE_KEY);
    return stored === "dark" || stored === "light" ? stored : "light";
}

function adminAntTheme(theme: AdminThemeName): ThemeConfig {
    const dark = theme === "dark";
    const base = getAntThemeConfig(dark);
    return {
        ...base,
        cssVar: { key: `hmaigc-admin-${theme}` },
        token: {
            ...base.token,
            colorPrimary: dark ? "#7ccbff" : "#2979c9",
            colorInfo: dark ? "#7ccbff" : "#2979c9",
            colorLink: dark ? "#7ccbff" : "#2979c9",
            colorBgLayout: dark ? "#0b0f14" : "#f5f7fa",
            colorBgContainer: dark ? "#1b212b" : "#ffffff",
            borderRadiusLG: 8,
        },
    };
}
```

Provider 必须：

1. 只从 `ADMIN_THEME_STORAGE_KEY` 读取初始值。
2. 写入原始字符串 `light | dark`，不读取或修改全局 Zustand 主题。
3. 以 `data-admin-theme` 标记后台根。
4. 将 `ConfigProvider`、`App` 与实际后台 DOM 放在同一 Provider 内。
5. 使用根 `ref` 实现稳定的 `getPortalContainer(): HTMLElement`，并同时传给 `ConfigProvider.getPopupContainer` 与 Context；根不可用时才返回 `document.body`。
6. `AdminShell` 的 Ant Design `Drawer` 必须显式设置 `getContainer={getPortalContainer}`；不能假设 `ConfigProvider.getPopupContainer` 会接管 Drawer。

- [ ] **Step 4: 将后台入口硬切到唯一主题根**

把 `web/src/pages/admin/index.tsx` 的管理员成功分支改为：

```tsx
return (
    <AdminThemeProvider>
        <AdminProvider>
            <AdminShell />
        </AdminProvider>
    </AdminThemeProvider>
);
```

拒绝访问页面保持现有全局主题，因为它不属于已进入后台后的工作区。

- [ ] **Step 5: 定义后台明暗语义 Token**

在 `admin-workspace.css` 顶部集中加入两个明确映射：

```css
.admin-theme-root[data-admin-theme="light"],
.admin-theme-root[data-admin-theme="light"] .admin-workspace {
    --workspace-ui-page: #f5f7fa;
    --workspace-ui-surface: #ffffff;
    --workspace-ui-surface-subtle: #fafafa;
    --workspace-ui-control: #ffffff;
    --workspace-ui-control-hover: rgb(31 35 41 / 5%);
    --workspace-ui-text: #1f2329;
    --workspace-ui-text-secondary: #646a73;
    --workspace-ui-text-tertiary: #8f959e;
    --workspace-ui-text-disabled: rgb(31 35 41 / 35%);
    --workspace-ui-divider: #e8eaed;
    --workspace-ui-accent: #2979c9;
    --admin-chart-primary: #2979c9;
    --admin-chart-secondary: #0f766e;
    --admin-chart-warning: #b65c00;
    --background: #f5f7fa;
    --foreground: #1f2329;
    --card: #ffffff;
    --card-foreground: #1f2329;
    --popover: #ffffff;
    --popover-foreground: #1f2329;
    --primary: #2979c9;
    --primary-foreground: #ffffff;
    --muted: #fafafa;
    --muted-foreground: #8f959e;
    --border: #e8eaed;
    --input: #ffffff;
    color-scheme: light;
}

.admin-theme-root[data-admin-theme="dark"],
.admin-theme-root[data-admin-theme="dark"] .admin-workspace {
    --workspace-ui-page: #0b0f14;
    --workspace-ui-surface: #1b212b;
    --workspace-ui-surface-subtle: #111827;
    --workspace-ui-control: #1b212b;
    --workspace-ui-control-hover: rgb(255 255 255 / 5%);
    --workspace-ui-text: rgb(255 255 255 / 96%);
    --workspace-ui-text-secondary: rgb(255 255 255 / 72%);
    --workspace-ui-text-tertiary: rgb(255 255 255 / 48%);
    --workspace-ui-text-disabled: rgb(255 255 255 / 38%);
    --workspace-ui-divider: rgb(255 255 255 / 8%);
    --workspace-ui-accent: #7ccbff;
    --admin-chart-primary: #7ccbff;
    --admin-chart-secondary: #5eead4;
    --admin-chart-warning: #ff9f0a;
    --background: #0b0f14;
    --foreground: rgb(255 255 255 / 96%);
    --card: #1b212b;
    --card-foreground: rgb(255 255 255 / 96%);
    --popover: #1b212b;
    --popover-foreground: rgb(255 255 255 / 96%);
    --primary: #7ccbff;
    --primary-foreground: #06111a;
    --muted: #111827;
    --muted-foreground: rgb(255 255 255 / 48%);
    --border: rgb(255 255 255 / 8%);
    --input: #1b212b;
    color-scheme: dark;
}
```

不要给 `document.documentElement` 添加/删除 `dark`；后台明暗只在 `.admin-theme-root` 内生效。必须同时覆盖 `.admin-workspace`，因为 `workspace-ui.css` 会在该元素上重新声明语义变量；必须覆盖常用 Tailwind 颜色别名，避免创作端根 `.dark` 穿透后台。

- [ ] **Step 6: 运行定向测试并确认 GREEN**

Run:

```powershell
Set-Location web
bun test test/admin-theme-scope.test.tsx test/admin-mobile-navigation.test.ts
```

Expected: PASS；首次后台为浅色、切换持久化、创作主题键不变、现有移动 Drawer 根契约不回退。

- [ ] **Step 7: 提交主题里程碑**

```powershell
git add web/src/pages/admin/admin-theme.tsx web/src/pages/admin/index.tsx web/src/pages/admin/admin-workspace.css web/test/admin-theme-scope.test.tsx
git diff --cached --check
git commit -m "feat(admin): 建立独立后台主题作用域"
```

---

### Task 2: Pro 化公共外壳与页面标题操作区

**Files:**
- Modify: `web/src/pages/admin/components/admin-shell.tsx:1-183`
- Modify: `web/src/pages/admin/admin-workspace.css:231-321`
- Modify: `web/src/pages/admin/admin-responsive.css:1-152`
- Test: `web/test/admin-pro-layout.test.tsx`

**Interfaces:**
- Consumes: `useAdminTheme(): { theme; setTheme }`、`AdminNavigation`、现有路由定位函数。
- Produces: `AdminPageActions({ children })` Portal 插槽、统一桌面/移动主题切换、固定 56px 顶栏与 1304px 页面框架。

- [ ] **Step 1: 写外壳和页面操作插槽的失败测试**

在 `web/test/admin-pro-layout.test.tsx` 建立 Happy DOM 测试并先覆盖纯页面框架：

```tsx
import "./setup-happy-dom";
import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { MemoryRouter } from "react-router";

import { AdminPageActions, AdminPageFrame } from "../src/pages/admin/components/admin-shell";

let root: Root | null = null;
afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

describe("admin pro layout", () => {
    test("renders descendant actions in the page header slot", async () => {
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);
        await act(async () =>
            root?.render(
                createElement(
                    MemoryRouter,
                    null,
                    createElement(
                        AdminPageFrame,
                        { title: "数据概览", description: "活跃、调用与成本趋势" },
                        createElement(AdminPageActions, null, createElement("button", { className: "analytics-refresh" }, "刷新")),
                    ),
                ),
            ),
        );

        const actions = document.querySelector(".admin-page-actions");
        expect(actions?.querySelector(".analytics-refresh")?.textContent).toBe("刷新");
        expect(document.querySelector(".admin-page-content .analytics-refresh")).toBeNull();
    });
});
```

- [ ] **Step 2: 运行测试并确认 RED**

Run:

```powershell
Set-Location web
bun test test/admin-pro-layout.test.tsx
```

Expected: FAIL，因为 `AdminPageActions` 尚不存在，且 `AdminPageFrame` 没有 Portal 目标。

- [ ] **Step 3: 实现页面操作 Portal 插槽**

在 `admin-shell.tsx` 增加局部 Context 和 Portal：

```tsx
const AdminPageActionTargetContext = createContext<HTMLElement | null>(null);

export function AdminPageActions({ children }: { children: ReactNode }) {
    const target = useContext(AdminPageActionTargetContext);
    return target ? createPortal(children, target) : null;
}
```

`AdminPageFrame` 必须始终渲染一个操作容器并保存 ref：

```tsx
const [actionTarget, setActionTarget] = useState<HTMLDivElement | null>(null);

return (
    <AdminPageActionTargetContext.Provider value={actionTarget}>
        <main id="admin-main-content" className="admin-page thin-scrollbar h-full overflow-y-auto" tabIndex={-1}>
            <div className="admin-page-frame mx-auto w-full">
                <header className="admin-page-header flex flex-wrap justify-between">
                    <div className="admin-page-heading min-w-0">...</div>
                    <div ref={setActionTarget} className="admin-page-actions flex shrink-0 items-center">
                        {actions}
                    </div>
                </header>
                ...
            </div>
        </main>
    </AdminPageActionTargetContext.Provider>
);
```

这保留既有 `actions` prop，并允许懒加载的真实页面把操作提升到统一标题区，不需要上移业务状态或复制 API 调用。

- [ ] **Step 4: 在桌面与移动顶栏加入明确主题切换**

使用 `useAdminTheme` 和原生按钮；同时从 hook 读取 `getPortalContainer` 并传给 Drawer：

```tsx
const { theme, setTheme } = useAdminTheme();
const nextTheme = theme === "light" ? "dark" : "light";
<button
    type="button"
    className="admin-theme-toggle app-workspace-icon-button"
    aria-label={`切换为${nextTheme === "dark" ? "深色" : "浅色"}后台`}
    aria-pressed={theme === "dark"}
    onClick={() => setTheme(nextTheme)}
>
    {theme === "dark" ? <Sun ... /> : <Moon ... />}
</button>
<Drawer getContainer={getPortalContainer} ... />
```

桌面右侧顺序固定为主题切换、返回创作台；移动右侧顺序固定为主题切换、打开导航。禁止复用创作端的 `AnimatedThemeToggler`，因为它拥有另一份状态。

- [ ] **Step 5: 收敛公共外壳 CSS**

在 `admin-workspace.css` 与 `admin-responsive.css` 明确以下契约：

```css
.admin-theme-root,
.admin-workspace {
    width: 100%;
    min-width: 0;
    height: 100%;
    min-height: 0;
}

.admin-desktop-header,
.admin-mobile-header {
    height: 56px;
    min-height: 56px;
}

.admin-page-frame {
    width: 100%;
    max-width: 1304px;
    margin-inline: auto;
    padding: 24px 32px 56px;
}

.admin-page-title {
    font-size: 22px;
    font-weight: 600;
    line-height: 30px;
}

@media (max-width: 1279px) {
    .admin-page-frame { padding-inline: 24px; }
    .admin-theme-toggle,
    .admin-mobile-menu-button { width: 44px; min-height: 44px; }
}

@media (max-width: 639px) {
    .admin-page-frame { padding: 20px 16px 40px; }
    .admin-page-header { align-items: stretch; flex-direction: column; }
    .admin-page-actions { width: 100%; flex-wrap: wrap; }
}
```

保留 236/64px 侧栏和 320px Drawer，删除与新值冲突的重复规则，不新增第二套 `.pro-*` 平行样式。

- [ ] **Step 6: 运行定向测试并确认 GREEN**

Run:

```powershell
Set-Location web
bun test test/admin-theme-scope.test.tsx test/admin-pro-layout.test.tsx test/admin-mobile-navigation.test.ts test/admin-model-center.test.tsx
```

Expected: PASS；操作 Portal、独立主题切换、现有导航/模型中心路由均保持可用。

- [ ] **Step 7: 提交公共外壳里程碑**

```powershell
git add web/src/pages/admin/components/admin-shell.tsx web/src/pages/admin/admin-workspace.css web/src/pages/admin/admin-responsive.css web/test/admin-pro-layout.test.tsx
git diff --cached --check
git commit -m "feat(admin): 统一 Pro 化后台外壳"
```

---

### Task 3: 将数据概览改造成真实样板页

**Files:**
- Modify: `web/src/pages/admin/components/analytics-panel.tsx:1-388`
- Modify: `web/src/pages/admin/admin-art-layout.css:1-276`
- Modify: `web/src/pages/admin/admin-responsive.css:1-152`
- Test: `web/test/admin-pro-layout.test.tsx`

**Interfaces:**
- Consumes: `AdminPageActions`、`AdminContentSkeleton`、`AdminContentError`、`AdminTableEmpty`、既有 `getAdminAnalytics`、`exportAdminAnalytics`、`listAdminUsers`。
- Produces: 页面级刷新/导出操作、显式初次加载/初次失败/刷新失败/空数据状态、固定内容顺序和主题化 Recharts 颜色。

- [ ] **Step 1: 扩展数据概览真实 DOM 失败测试**

使用 Bun `mock.module` 在动态导入前替换 API，并用完整但最小的 `AdminAnalytics` fixture：

```tsx
import { mock } from "bun:test";

const analytics = {
    from: "2026-07-15",
    to: "2026-08-13",
    kpi: {
        activeUsers: 12,
        dau: 3,
        wau: 8,
        mau: 12,
        generationTasks: 26,
        upstreamRequests: 24,
        successRate: 95.8,
        p95DurationMs: 1240,
        currentQueuedTasks: 2,
        estimatedCostMicros: 1200000,
        costAvailable: true,
        currency: "CNY",
        settledRevenueMicrocredits: 8000000,
        settledBaseCostMicrocredits: 3000000,
        grossProfitMicrocredits: 5000000,
        settledBillingOrders: 9,
        pendingAmountMicrocredits: 100000,
        pendingBillingOrders: 1,
        reviewAmountMicrocredits: 0,
        reviewBillingOrders: 0,
    },
    trend: [{ day: "2026-08-13", tasks: 2, requests: 2, activeUsers: 2, requestSuccessRate: 100 }],
    models: [],
    users: [],
    failures: [],
} satisfies import("../src/services/api/auth").AdminAnalytics;

let requestCount = 0;
mock.module("@/services/api/auth", () => ({
    getAdminAnalytics: async () => {
        requestCount += 1;
        return analytics;
    },
    exportAdminAnalytics: async () => new Blob(["ok"]),
    listAdminUsers: async () => ({ users: [], total: 0 }),
}));
```

动态导入 `AnalyticsPanel` 后渲染在 `MemoryRouter + ConfigProvider + App + AdminPageFrame` 内，等待请求完成，再断言：

```tsx
const layout = document.querySelector(".admin-page-content .admin-analytics-layout");
const children = [...(layout?.children ?? [])];
expect(children.indexOf(layout?.querySelector(".admin-analytics-overview-grid") as Element)).toBe(0);
expect(children.indexOf(layout?.querySelector(".admin-analytics-toolbar") as Element)).toBe(1);
expect(children.indexOf(layout?.querySelector(".admin-analytics-trend") as Element)).toBe(2);
expect(children.indexOf(layout?.querySelector(".admin-analytics-tabs") as Element)).toBe(3);
expect(document.querySelector(".admin-page-actions .admin-analytics-refresh-button")).not.toBeNull();
expect(document.querySelector(".admin-page-actions .admin-export-button")).not.toBeNull();
```

另加三个状态用例：

1. deferred 初次请求未完成时出现 `.admin-content-skeleton`；拒绝后出现 `.admin-content-error` 和“重试”，不允许仅把指标显示为 `--` 后静默结束。
2. 首次成功但 `models/users/failures` 都为空时，当前标签页使用 `AdminTableEmpty` 明确显示“暂无模型统计”。
3. 已有数据后点击刷新并失败时保留“活跃用户 12”等已有指标，同时在布局顶部显示真实错误，不把旧事实清空成 `--`。

- [ ] **Step 2: 运行数据概览测试并确认 RED**

Run:

```powershell
Set-Location web
bun test test/admin-pro-layout.test.tsx
```

Expected: FAIL；当前筛选工具栏先于指标，刷新/导出仍在内容区，初次失败只有全局 message。

- [ ] **Step 3: 把页面操作提升到标题区且保留原行为**

用 `AdminPageActions` 包裹现有两个按钮：

```tsx
<AdminPageActions>
    <Button
        className="admin-analytics-refresh-button"
        icon={<RefreshCw className="admin-analytics-refresh-icon size-4" />}
        loading={loading}
        onClick={() => void reload()}
    >
        刷新
    </Button>
    <AdminExportButton
        exportFile={() => exportAdminAnalytics(filters)}
        fileName={() => `usage-${filters.from}-${filters.to}.csv`}
        label="导出 CSV"
    />
</AdminPageActions>
```

从 `ListToolbar.trailing` 删除这两个按钮；筛选字段和 `filters` 对象保持原样。

- [ ] **Step 4: 实现显式加载与错误事实**

增加 `loadError: string | null`，在每次 `reload` 前清空，失败时保存真实 `Error.message`。渲染规则固定为：

```tsx
if (loading && !data) {
    return <AdminContentSkeleton rows={12} label="正在读取数据概览" />;
}

if (loadError && !data) {
    return <AdminContentError title="数据概览读取失败" description={loadError} onRetry={() => void reload()} />;
}
```

已有数据后的刷新失败在布局顶部显示紧凑 `AdminContentError`，不得清空旧数据，也不得伪造成功。保留 `message.error` 只会造成重复错误反馈，应删除这一处 message 调用和对应 `App.useApp()`。

三个表格都显式设置空状态，不依赖 Ant Design 默认英文或图形占位：

```tsx
locale={{ emptyText: <AdminTableEmpty compact title="暂无模型统计" description="当前筛选范围内没有模型调用记录。" /> }}
```

用户活动与异常定位分别使用“暂无用户活动”和“暂无异常记录”；筛选条件是否存在只影响说明文案，不制造假数据。

- [ ] **Step 5: 硬切内容顺序并主题化图表**

JSX 顺序必须是：

```tsx
<div className="admin-analytics-layout">
    <div className="admin-analytics-overview-grid">...</div>
    <div className="admin-analytics-toolbar">...</div>
    <section className="admin-analytics-trend">...</section>
    <Tabs className="admin-analytics-tabs" ... />
</div>
```

把 Recharts 裸色替换为后台主题变量：

```tsx
<CartesianGrid stroke="var(--workspace-ui-divider)" vertical={false} />
<Area ... stroke="var(--admin-chart-primary)" fill="var(--admin-chart-primary)" />
<Area ... stroke="var(--admin-chart-secondary)" fill="var(--admin-chart-secondary)" />
<Line ... stroke="var(--admin-chart-warning)" />
```

坐标文字、Tooltip、Legend 继续由 Ant Design/Recharts 语义色控制；不得新增按 `theme ===` 分支的平行图表 JSX。

- [ ] **Step 6: 收敛数据概览样板 CSS**

在 `admin-art-layout.css` 保留单层表面：

```css
.admin-workspace .admin-analytics-layout {
    display: grid;
    gap: 24px;
}

.admin-workspace .admin-analytics-overview-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    overflow: hidden;
    border-radius: 8px;
    background: var(--workspace-ui-surface);
}

.admin-workspace .admin-analytics-metric-section + .admin-analytics-metric-section {
    border-left: 1px solid var(--workspace-ui-divider);
}

.admin-workspace .admin-analytics-toolbar,
.admin-workspace .admin-analytics-trend,
.admin-workspace .admin-analytics-tabs {
    min-width: 0;
    border-radius: 8px;
    background: var(--workspace-ui-surface);
}
```

移动端把 overview 改成单列，并将第二 section 的左边界改为顶部分隔；表格横向滚动只发生在 `.ant-table-content/.ant-table-body`。

- [ ] **Step 7: 运行定向测试与类型构建前检查**

Run:

```powershell
Set-Location web
bun test test/admin-pro-layout.test.tsx test/admin-theme-scope.test.tsx test/admin-mobile-navigation.test.ts test/admin-model-center.test.tsx
bunx prettier --check src/pages/admin/admin-theme.tsx src/pages/admin/index.tsx src/pages/admin/components/admin-shell.tsx src/pages/admin/components/analytics-panel.tsx src/pages/admin/admin-workspace.css src/pages/admin/admin-art-layout.css src/pages/admin/admin-responsive.css test/admin-theme-scope.test.tsx test/admin-pro-layout.test.tsx
```

Expected: 所有定向测试和格式检查 PASS。

- [ ] **Step 8: 提交数据概览样板里程碑**

```powershell
git add web/src/pages/admin/components/analytics-panel.tsx web/src/pages/admin/admin-art-layout.css web/src/pages/admin/admin-responsive.css web/test/admin-pro-layout.test.tsx
git diff --cached --check
git commit -m "feat(admin): Pro 化数据概览样板"
```

---

### Task 4: 最终商业级验收、镜像重建与集中修复

**Files:**
- Verify only first; modify only the Task 1–3 files when a current-diff defect is proven.
- Do not add permanent ad-hoc browser scripts, screenshots, build artifacts or local data to Git.

**Interfaces:**
- Consumes: Task 1–3 的唯一后台主题根、公共外壳、数据概览样板。
- Produces: 可重复的自动化证据、Docker 健康证据、三视口双主题真实浏览器证据、一次集中 review 结论。

- [ ] **Step 1: 跑全量 Web 测试**

Run:

```powershell
Set-Location web
bun test
```

Expected: 全量 PASS；不允许跳过失败测试或通过修改无关断言掩盖回归。

- [ ] **Step 2: 跑生产构建与包体预算**

Run:

```powershell
Set-Location web
bun run build
```

Expected: TypeScript、Vite build 和 `check:bundle` 全部 PASS。

- [ ] **Step 3: 安全重建本地镜像**

从 worktree 根运行仓库提供的安全脚本；它会解析 Git common dir 并只绑定主项目 `.local/data`：

```powershell
.\scripts\local-compose.ps1 up -d --build --wait
.\scripts\local-compose.ps1 ps
```

Expected: `backend` 与 `web` 均健康/运行，`http://127.0.0.1:3000/api/health` 返回成功。禁止直接运行未显式绑定 `CANVAS_DATA_PATH` 的 `docker compose up`，禁止创建第二份数据库。

- [ ] **Step 4: 执行真实浏览器矩阵**

使用已登录的本地后台 `/admin`，逐个验证：

| 视口 | 主题 | 必查事实 |
|---|---|---|
| 1440×900 | 浅色、深色 | 236/64 侧栏、56 顶栏、1240 有效内容、标题操作、指标→筛选→趋势→表格 |
| 1024×768 | 浅色、深色 | 无桌面侧栏、56 顶栏、320 Drawer、无页面横向溢出 |
| 390×844 | 浅色、深色 | 标题/操作换行、44px 热区、指标单列、表格局部横滚 |

每种主题至少打开一次移动 Drawer 和一个 Ant Design 下拉/Tooltip，确认 Portal 主题与后台一致。额外确认：切换后台主题并刷新后保持；返回创作台后创作端原主题不变。

- [ ] **Step 5: 执行一次显式 review 并集中修复**

对照：

1. 用户批准的设计规格。
2. 本计划 Global Constraints。
3. `git diff` 的实际生产文件与测试文件。
4. 主题、Portal、导航、数据概览行为和响应式浏览器证据。

只修当前 diff 缺陷。若发现需要 API/数据库/其他后台页面重排，记录为后续阶段，不扩大本阶段；若生产文件超过 12 个或净改动超过 1200 行，停止并报告架构熔断。

- [ ] **Step 6: 修复后只重跑受影响定向门禁，再做一次最终确认**

至少执行：

```powershell
Set-Location web
bun test test/admin-theme-scope.test.tsx test/admin-pro-layout.test.tsx test/admin-mobile-navigation.test.ts test/admin-model-center.test.tsx
bun test
bun run build
```

若集中修复没有影响 Dockerfile/Compose，不重复构建后端；重建 Web 镜像并复查 `/admin` 即可：

```powershell
Set-Location ..
.\scripts\local-compose.ps1 build web
.\scripts\local-compose.ps1 up -d --no-deps --force-recreate web
.\scripts\local-compose.ps1 ps
```

- [ ] **Step 7: 提交最终验收修复（仅在确有改动时）**

```powershell
git add web/src/pages/admin/admin-theme.tsx web/src/pages/admin/index.tsx web/src/pages/admin/components/admin-shell.tsx web/src/pages/admin/components/analytics-panel.tsx web/src/pages/admin/admin-workspace.css web/src/pages/admin/admin-art-layout.css web/src/pages/admin/admin-responsive.css web/test/admin-theme-scope.test.tsx web/test/admin-pro-layout.test.tsx
git diff --cached --check
git diff --cached --name-status
git commit -m "fix(admin): 收口 Pro 化后台验收问题"
```

暂存前必须确认没有包含用户原有的 `docs/superpowers/specs/2026-08-13-canvas-media-node-polish-design.md` 删除记录、`.superpowers/`、截图、构建产物、数据目录或任何密钥。

## Deferred Follow-up

- 第二阶段逐页迁移模型、定价、用户、支付、日志、运维和设置页面的内容区；每个领域单独设计与验收。
- 创作首页、项目、素材、技能和画布只共享设计标准，不在本计划中改造。
- Headless UI 是否用于创作端复合交互，另立规格评估，不进入后台依赖。
