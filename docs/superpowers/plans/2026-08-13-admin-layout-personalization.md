# 后台布局个性化实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task in the current primary session. Do not use `superpowers:subagent-driven-development`: the settings source, theme provider, shell and CSS share one state boundary and must be changed as one linear delivery chain.

**Goal:** 在保留现有后台路由、权限与导航体系的前提下，增加 Ant Design Pro `SettingDrawer` 同类的后台界面设置，支持后台明暗主题、菜单主题、品牌色、侧栏宽度、内容宽度与固定顶栏，并在当前浏览器持久化。

**Architecture:** 新建单一 `AdminLayoutSettingsProvider` 作为后台外观唯一事实源；`AdminThemeProvider` 只负责将该事实投影为 Ant Design token、后台根节点数据属性和弹层容器；`AdminLayoutSettingsDrawer` 只编辑设置；`AdminShell` 只将设置映射到侧栏、顶栏和内容布局。删除旧 `ADMIN_THEME_STORAGE_KEY` 独立状态，不保留兼容分支。

**Tech Stack:** React 19、TypeScript 7 strict、Ant Design 6、React Router 8、Bun Test、Happy DOM、Vite、现有 Docker Compose 本地镜像流程。

## Global Constraints

- [ ] 先阅读并遵守根目录 `AGENTS.md`、`Design.md`、`web/DESIGN.md`；设计真源为 `docs/superpowers/specs/2026-08-13-admin-layout-personalization-design.md`。
- [ ] 不引入 `@ant-design/pro-components` 或其他新依赖，不替换现有路由、权限、菜单数据与页面业务结构。
- [ ] 生产职责最多 3 个：统一设置状态、设置抽屉、后台外壳消费。
- [ ] 生产文件最多 8 个，净新增生产代码目标 400–600 行；达到 800 行或出现超过 500 行的新生产文件时停止并重新评估。
- [ ] 禁止 `any`、`as any`、旧主题兼容键、双轨状态、静默保存失败或伪造云端同步。
- [ ] 不修改后端、数据库、支付、模型、部署控制器和创作端主题。
- [ ] focused RED/GREEN 只跑受影响测试；全 Web、构建、Docker 和真实浏览器只在稳定里程碑与最终收口执行一次。
- [ ] 每个 JSX/TSX 标签保留描述性 `className`；设置抽屉遵守单层圆角边框与移动端 44px 触控高度。
- [ ] 保留用户现有未提交改动，不暂存 `.superpowers/` 与已删除的 `docs/superpowers/specs/2026-08-13-canvas-media-node-polish-design.md`。

---

## Task 1：建立统一后台布局设置事实源

**Files:**

- Create: `web/src/pages/admin/admin-layout-settings.tsx`
- Modify: `web/src/pages/admin/index.tsx`
- Modify: `web/src/pages/admin/admin-theme.tsx`
- Create: `web/test/admin-layout-settings.test.tsx`
- Modify: `web/test/admin-theme-scope.test.tsx`

### 1.1 先写设置解析、持久化和上下文失败测试

- [ ] 新建 `web/test/admin-layout-settings.test.tsx`，覆盖默认值、完整合法值、任意非法字段整对象回退、写入失败显式返回错误、更新失败仍保留内存预览、恢复默认值。
- [ ] 继续在 `web/test/admin-theme-scope.test.tsx` 断言创作端主题键不变，并删除对旧 `ADMIN_THEME_STORAGE_KEY` 的依赖。

核心测试形状：

```tsx
import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";

import {
    ADMIN_LAYOUT_SETTINGS_STORAGE_KEY,
    AdminLayoutSettingsProvider,
    DEFAULT_ADMIN_LAYOUT_SETTINGS,
    parseAdminLayoutSettings,
    persistAdminLayoutSettings,
    useAdminLayoutSettings,
} from "../src/pages/admin/admin-layout-settings";

test("rejects a partially invalid stored object instead of mixing defaults with corrupt facts", () => {
    const stored = JSON.stringify({
        ...DEFAULT_ADMIN_LAYOUT_SETTINGS,
        siderWidth: 999,
    });

    expect(parseAdminLayoutSettings(stored)).toEqual(DEFAULT_ADMIN_LAYOUT_SETTINGS);
});

test("reports persistence failure without rolling back the in-memory preview", async () => {
    const failingStorage = {
        getItem: () => null,
        setItem: () => {
            throw new Error("quota exceeded");
        },
    };

    // Render provider with injected storage, update theme to dark, then assert:
    // settings.theme === "dark" and persistenceError contains "无法保存后台界面设置".
});

test("does not modify the creation theme preference", () => {
    const creationTheme = JSON.stringify({ state: { theme: "dark" } });
    localStorage.setItem("infinite-canvas:theme_store", creationTheme);
    persistAdminLayoutSettings(localStorage, DEFAULT_ADMIN_LAYOUT_SETTINGS);
    expect(localStorage.getItem("infinite-canvas:theme_store")).toBe(creationTheme);
});
```

测试用 Provider 允许注入最小存储契约，禁止在测试中替换全局 `localStorage`：

```ts
type AdminLayoutStorage = Pick<Storage, "getItem" | "setItem" | "removeItem">;
```

Provider 的测试注入只暴露存储边界，不允许注入预制设置或绕过解析：

```tsx
export function AdminLayoutSettingsProvider({
    children,
    storage = window.localStorage,
}: {
    children: ReactNode;
    storage?: AdminLayoutStorage;
}) {
    // Production and tests execute the same read / validate / persist path.
}
```

### 1.2 运行 RED，确认失败来自缺失统一设置模块

- [ ] 在 `web` 目录运行：

```powershell
bun test test/admin-layout-settings.test.tsx test/admin-theme-scope.test.tsx
```

Expected: FAIL，明确提示 `admin-layout-settings` 模块或其导出不存在；不得接受测试环境、路径或语法错误作为 RED。

### 1.3 最小实现严格设置契约

- [ ] 在 `admin-layout-settings.tsx` 中实现闭集类型、默认值、解析、持久化与 Context。

```tsx
export type AdminThemeName = "light" | "dark";
export type AdminMenuTheme = "follow" | "light" | "dark";
export type AdminSiderWidth = 208 | 236 | 272;
export type AdminContentWidth = "fixed" | "fluid";

export type AdminLayoutSettings = {
    theme: AdminThemeName;
    menuTheme: AdminMenuTheme;
    colorPrimary: string;
    siderWidth: AdminSiderWidth;
    contentWidth: AdminContentWidth;
    fixedHeader: boolean;
};

export const ADMIN_LAYOUT_SETTINGS_STORAGE_KEY = "hmaigc:admin-layout-settings:v1";

export const DEFAULT_ADMIN_LAYOUT_SETTINGS: Readonly<AdminLayoutSettings> = Object.freeze({
    theme: "light",
    menuTheme: "follow",
    colorPrimary: "#2979C9",
    siderWidth: 236,
    contentWidth: "fluid",
    fixedHeader: true,
});
```

- [ ] `parseAdminLayoutSettings(raw)` 先 JSON parse，再逐字段闭集校验；任一字段缺失或非法时返回完整默认对象，禁止字段级拼装旧事实。
- [ ] `readAdminLayoutSettings(storage)` 捕获 `getItem` 异常并返回完整默认对象；读取失败不得阻断后台挂载。
- [ ] `isAdminBrandColor(value)` 只接受完整 `#RRGGBB`，通过后统一大写。
- [ ] `updateSettings(patch)` 先更新 React 内存状态，再尝试写入一个版本化键；写失败写入 `persistenceError`，不回滚预览、不显示成功。
- [ ] `resetSettings()` 一次恢复全部默认值并走同一持久化路径。

Provider 公共接口：

```ts
type AdminLayoutSettingsContextValue = {
    settings: AdminLayoutSettings;
    persistenceError: string | null;
    updateSettings: (patch: Partial<AdminLayoutSettings>) => boolean;
    resetSettings: () => boolean;
};
```

返回值只表示本次是否成功写入浏览器存储；内存预览无论返回值为何都必须更新。

### 1.4 硬切 `AdminThemeProvider` 为统一事实源消费者

- [ ] 在 `web/src/pages/admin/index.tsx` 中固定 Provider 顺序：

```tsx
<AdminLayoutSettingsProvider>
    <AdminThemeProvider>
        <AdminProvider>
            <AdminShell />
        </AdminProvider>
    </AdminThemeProvider>
</AdminLayoutSettingsProvider>
```

- [ ] 从 `admin-theme.tsx` 删除 `ADMIN_THEME_STORAGE_KEY`、`readAdminTheme`、本地 `useState` 和 `setTheme`。
- [ ] `AdminThemeProvider` 通过 `useAdminLayoutSettings()` 读取 `settings.theme`、`settings.colorPrimary` 和解析后的菜单主题。
- [ ] `AdminThemeContext` 只保留 `getPortalContainer`，防止重新形成第二状态源。
- [ ] `adminAntTheme(theme, colorPrimary)` 将统一品牌色写入 `colorPrimary`、`colorInfo`、`colorLink`，不得按深色主题替换为另一条硬编码品牌色。
- [ ] 根节点输出真实设置事实：

```tsx
<div
    ref={setRootElement}
    className="admin-theme-root"
    data-admin-theme={settings.theme}
    data-admin-menu-theme={resolveAdminMenuTheme(settings)}
    style={{ "--admin-color-primary": settings.colorPrimary } as AdminThemeRootStyle}
>
```

`AdminThemeRootStyle` 使用 `CSSProperties` 交叉类型声明自定义 CSS 变量，禁止 `any`。

### 1.5 运行 GREEN 并提交职责一

- [ ] 运行：

```powershell
bun test test/admin-layout-settings.test.tsx test/admin-theme-scope.test.tsx
```

Expected: PASS；同时断言旧键字符串 `hmaigc:admin-theme` 不再出现在生产代码。

- [ ] 检查：

```powershell
rg -n "ADMIN_THEME_STORAGE_KEY|hmaigc:admin-theme|as any|:\s*any\b" src/pages/admin test/admin-layout-settings.test.tsx test/admin-theme-scope.test.tsx
```

Expected: 无旧后台主题键与 `any` 命中。

- [ ] 暂存仅本任务文件并提交：

```powershell
git add web/src/pages/admin/admin-layout-settings.tsx web/src/pages/admin/index.tsx web/src/pages/admin/admin-theme.tsx web/test/admin-layout-settings.test.tsx web/test/admin-theme-scope.test.tsx
git diff --cached --check
git commit -m "refactor(admin): 统一后台布局设置状态"
```

---

## Task 2：实现 Pro 风格设置抽屉与单一入口

**Files:**

- Create: `web/src/pages/admin/components/admin-layout-settings-drawer.tsx`
- Create: `web/src/pages/admin/admin-layout-settings.css`
- Modify: `web/src/pages/admin/components/admin-shell.tsx`
- Create: `web/test/admin-layout-settings-drawer.test.tsx`

### 2.1 先写真实 DOM 交互 RED

- [ ] 使用 Happy DOM + React `createRoot` 渲染统一 Provider、`AdminThemeProvider` 与设置抽屉，不通过源码字符串代替交互事实。
- [ ] 覆盖以下行为：
  - 桌面与移动入口均使用“界面设置”语义，实际打开同一个右侧抽屉。
  - 点击深色主题、强制浅色菜单、272px 侧栏、固定内容宽度、关闭固定顶栏，Context 即时变化并写入单一存储键。
  - 自定义色 `#123ABC` 生效；`123ABC`、短色值和语义色外输入保持草稿并显示错误，不修改生效颜色。
  - localStorage 写失败时抽屉显示“当前预览已生效，但无法保存到此浏览器”，不得出现“已保存”。
  - “恢复默认设置”一次恢复六个字段。
  - 抽屉关闭后焦点回到触发按钮；键盘可操作，390px 入口与主要控件不小于 44px。

关键交互测试：

```tsx
test("keeps an invalid custom color as a field error without changing the active brand color", async () => {
    await renderSettingsDrawer({ open: true });
    const input = screenElement<HTMLInputElement>(".admin-layout-color-input");

    await act(async () => {
        input.value = "#12";
        input.dispatchEvent(new InputEvent("input", { bubbles: true }));
    });

    expect(document.querySelector(".admin-layout-color-error")?.textContent).toContain("#RRGGBB");
    expect(document.querySelector(".admin-layout-settings-probe")?.getAttribute("data-color-primary")).toBe("#2979C9");
});
```

### 2.2 运行 RED

- [ ] 运行：

```powershell
bun test test/admin-layout-settings-drawer.test.tsx
```

Expected: FAIL，明确缺少设置抽屉或入口行为。

### 2.3 最小实现设置抽屉

- [ ] 新建 `AdminLayoutSettingsDrawer`，只接收：

```ts
type AdminLayoutSettingsDrawerProps = {
    open: boolean;
    onClose: () => void;
};
```

- [ ] 使用现有 Ant Design `Drawer`、`Segmented`、`Switch`、`Input`、`Button` 与 `Alert`，不增加依赖。
- [ ] 抽屉宽度桌面 360px，移动端 `min(360px, calc(100vw - 24px))`；挂载到 `useAdminTheme().getPortalContainer()`。
- [ ] 四组设置按设计真源排列，预设色仅为蓝 `#2979C9`、青 `#08979C`、紫 `#722ED1`。
- [ ] 自定义色使用受控草稿；只有完整合法值才调用 `updateSettings`。
- [ ] `persistenceError` 使用明确 `Alert` 展示，不伪造同步/保存成功。
- [ ] 恢复按钮直接调用 `resetSettings()`；返回 `true` 时提示“已恢复并保存后台默认界面设置”，返回 `false` 时提示“已恢复当前预览，但无法保存到此浏览器”，禁止失败时显示保存成功。

### 2.4 把入口集中到 `AdminShell`

- [ ] `AdminShell` 持有唯一 `settingsOpen` 状态，桌面顶栏和移动顶栏只调用同一个 `openSettings`。
- [ ] 删除旧 Moon/Sun 快捷主题按钮与 `useAdminTheme` 的主题写操作，替换为 `Settings2` 图标按钮：

```tsx
<Tooltip title="界面设置" placement="bottom">
    <button
        type="button"
        className="admin-layout-settings-trigger app-workspace-icon-button"
        aria-label="打开后台界面设置"
        aria-expanded={settingsOpen}
        onClick={() => setSettingsOpen(true)}
    >
        <Settings2 className="admin-layout-settings-trigger-icon size-4" />
    </button>
</Tooltip>
```

- [ ] `MobileAdminNavigation` 接收 `onOpenSettings`，不复制抽屉实例或第二套状态。
- [ ] 在 Shell 根部只渲染一个 `AdminLayoutSettingsDrawer`。

### 2.5 样式与 GREEN

- [ ] `admin-layout-settings.css` 只负责抽屉与设置控件；不承载 shell 布局。
- [ ] 使用留白、对齐和背景层级，最多一层可见圆角边框；主要选择按钮可见焦点清晰。
- [ ] `prefers-reduced-motion` 下关闭非必要动画。
- [ ] 运行：

```powershell
bun test test/admin-layout-settings-drawer.test.tsx test/admin-layout-settings.test.tsx test/admin-theme-scope.test.tsx
```

Expected: PASS。

- [ ] 暂存仅本任务文件并提交：

```powershell
git add web/src/pages/admin/components/admin-layout-settings-drawer.tsx web/src/pages/admin/admin-layout-settings.css web/src/pages/admin/components/admin-shell.tsx web/test/admin-layout-settings-drawer.test.tsx
git diff --cached --check
git commit -m "feat(admin): 增加后台界面设置抽屉"
```

---

## Task 3：让后台外壳消费宽度、菜单主题与固定顶栏

**Files:**

- Modify: `web/src/pages/admin/components/admin-shell.tsx`
- Modify: `web/src/pages/admin/admin-workspace.css`
- Modify: `web/src/pages/admin/admin-responsive.css`
- Modify: `web/test/admin-pro-layout.test.tsx`
- Modify: `web/test/admin-layout-settings-drawer.test.tsx`

### 3.1 先写布局投影 RED

- [ ] 在真实 DOM 测试中渲染 `AdminShell` 的最小路由树，断言：
  - 展开侧栏的 `style.width` 依次为 `208px`、`236px`、`272px`。
  - 折叠态始终为 `64px`，不受展开宽度影响。
  - 根节点写出 `data-admin-content-width="fixed|fluid"` 和 `data-admin-fixed-header="true|false"`。
  - `menuTheme=follow` 分别解析为当前明暗主题；强制 light/dark 不随页面主题改变。
  - `fixed` 页面框架最大宽度 1120px，`fluid` 最大宽度 1304px。
  - `fixedHeader=false` 时桌面 header 与页面共享滚动容器；移动 header 仍 sticky 可访问。

CSS 合同测试只补充浏览器难稳定计算的断点事实，不替代 DOM 交互：

```tsx
test("defines the agreed fixed and fluid content widths without widening the page root", async () => {
    const styles = await Bun.file(new URL("../src/pages/admin/admin-workspace.css", import.meta.url)).text();
    expect(styles).toContain('[data-admin-content-width="fixed"] .admin-page-frame');
    expect(styles).toContain("max-width: 1120px");
    expect(styles).toContain('[data-admin-content-width="fluid"] .admin-page-frame');
    expect(styles).toContain("max-width: 1304px");
});
```

### 3.2 运行 RED

- [ ] 运行：

```powershell
bun test test/admin-pro-layout.test.tsx test/admin-layout-settings-drawer.test.tsx
```

Expected: FAIL，明确来自 shell 尚未映射设置或缺失 CSS 合同。

### 3.3 映射设置到 Shell，禁止页面自己读存储

- [ ] `AdminShell` 只通过 `useAdminLayoutSettings()` 读取设置。
- [ ] 为自定义 CSS 变量声明精确类型：

```ts
type AdminWorkspaceStyle = CSSProperties & {
    "--admin-sider-width": string;
};
```

- [ ] Shell 根节点写出：

```tsx
<div
    className="app-user-workspace admin-workspace workspace-ui-scope ..."
    data-admin-content-width={settings.contentWidth}
    data-admin-fixed-header={settings.fixedHeader ? "true" : "false"}
    style={{ "--admin-sider-width": `${settings.siderWidth}px` } as AdminWorkspaceStyle}
>
```

- [ ] 展开侧栏宽度使用 `var(--admin-sider-width)`；折叠仍使用现有 64px 类，不创建第二个折叠宽度设置。
- [ ] `AdminPageFrame`、业务页面和导航组件不得直接读取 localStorage。

### 3.4 统一菜单主题和品牌色 CSS 变量

- [ ] 在 `admin-workspace.css` 中将全局品牌变量改为统一事实：

```css
--workspace-ui-accent: var(--admin-color-primary);
--primary: var(--admin-color-primary);
--admin-chart-primary: var(--admin-color-primary);
```

- [ ] 给 `.admin-sidebar` 与 `.admin-mobile-navigation-drawer` 增加 light/dark 菜单变量覆盖，选择器以 `.admin-theme-root[data-admin-menu-theme="..."]` 为根；禁止为每个导航项复制颜色分支。
- [ ] light 菜单始终使用浅色表面和深色文字，dark 菜单始终使用深色表面和浅色文字，active/icon 继续消费 `--workspace-ui-accent`。

### 3.5 实现内容宽度与固定顶栏

- [ ] 添加内容模式：

```css
.admin-workspace[data-admin-content-width="fixed"] .admin-page-frame {
    max-width: 1120px;
}

.admin-workspace[data-admin-content-width="fluid"] .admin-page-frame {
    max-width: 1304px;
}
```

- [ ] `fixedHeader=true` 保持现有 header + 独立 `.admin-page` 滚动。
- [ ] `fixedHeader=false` 改为 `.admin-workspace-main` 统一纵向滚动，`.admin-page` 取消自身高度/滚动；不得出现双滚动条。
- [ ] 1279px 以下不应用自定义侧栏宽度；移动 header 无论该设置为何都保持 `position: sticky; top: 0`。
- [ ] 639px 以下 `.admin-page-frame` 保持 16px 水平内边距；页面根 `overflow-x: hidden`，宽表格只在自身容器滚动。

### 3.6 GREEN、格式与职责提交

- [ ] 运行 focused tests：

```powershell
bun test test/admin-pro-layout.test.tsx test/admin-layout-settings-drawer.test.tsx test/admin-layout-settings.test.tsx test/admin-theme-scope.test.tsx
```

Expected: PASS。

- [ ] 格式与静态检查：

```powershell
bunx prettier --write src/pages/admin/admin-layout-settings.tsx src/pages/admin/admin-theme.tsx src/pages/admin/components/admin-layout-settings-drawer.tsx src/pages/admin/components/admin-shell.tsx src/pages/admin/admin-layout-settings.css src/pages/admin/admin-workspace.css src/pages/admin/admin-responsive.css test/admin-layout-settings.test.tsx test/admin-layout-settings-drawer.test.tsx test/admin-theme-scope.test.tsx test/admin-pro-layout.test.tsx
rg -n "as any|:\s*any\b|hmaigc:admin-theme|@ant-design/pro-components" src/pages/admin test/admin-*.test.tsx
```

Expected: Prettier 完成；禁止项无命中。

- [ ] 提交：

```powershell
git add web/src/pages/admin/components/admin-shell.tsx web/src/pages/admin/admin-workspace.css web/src/pages/admin/admin-responsive.css web/test/admin-pro-layout.test.tsx web/test/admin-layout-settings-drawer.test.tsx
git diff --cached --check
git commit -m "feat(admin): 应用后台个性化布局"
```

---

## Task 4：一次性商业级验收、镜像重建与最终审查

**Files:**

- Verify only; production changes are allowed only for defects directly caused by Tasks 1–3 and within the original budget.

### 4.1 稳定里程碑全量门禁

- [ ] 在 `web` 目录串行运行：

```powershell
bun test
bun run build
```

Expected: 全量测试 0 failure；TypeScript、Vite production build 与 bundle budget 全部通过。

- [ ] 检查生产范围和体积：

```powershell
git diff --stat 6653bb2..HEAD
git diff --check 6653bb2..HEAD
git diff --name-only 6653bb2..HEAD
rg -n "as any|:\s*any\b|TODO|FIXME|hmaigc:admin-theme" web/src/pages/admin web/test/admin-*.test.tsx
```

Expected: 本功能生产文件不超过 8 个、无 `any`、无旧主题键、无占位实现；测试和设计文件不计入生产文件预算。

### 4.2 安全重建本地镜像

- [ ] 从 worktree 根运行仓库安全脚本，复用主项目 `.local/data`，禁止直接执行裸 `docker compose`：

```powershell
pwsh -File .\scripts\local-compose.ps1 up -d --build --wait
```

Expected: 脚本打印 canonical data directory，backend/web healthy；不得创建 worktree 内第二份数据库。

- [ ] 验证：

```powershell
Invoke-WebRequest -UseBasicParsing http://127.0.0.1:3000/api/health
Invoke-WebRequest -UseBasicParsing http://127.0.0.1:3000/admin
```

Expected: 两个请求均为 HTTP 200；本地现有账号和业务数据仍可使用。

### 4.3 真实浏览器矩阵

- [ ] 使用真实浏览器按 1440×900、1024×768、390×844 三个视口验收：
  - 设置入口可见且只打开一个右侧抽屉。
  - light/dark 页面主题与 follow/light/dark 菜单主题组合正确。
  - 蓝/青/紫预设色与合法自定义色即时影响 active 菜单、按钮和 Ant 弹层。
  - 208/236/272px 展开宽度可见；折叠始终 64px；1024/390 使用移动导航，不受桌面宽度影响。
  - fixed/fluid 内容宽度差异可见；fixedHeader 开关无双滚动条。
  - 刷新后设置保留；恢复默认值恢复六项事实。
  - 非法颜色显示字段错误；模拟存储失败显示明确错误且预览仍生效。
  - 下拉、Drawer、Tooltip 都归属 `.admin-theme-root`；创作端主题不变。
  - `document.documentElement.scrollWidth <= document.documentElement.clientWidth`；控制台无错误。

### 4.4 一次显式最终 review

- [ ] 对照原始需求、设计、计划、实际 diff 逐项核对：
  - 是否只有一个后台外观事实源。
  - 是否没有顶部/混合布局、云端同步和任意主题编辑器等范围扩张。
  - 是否未改路由、权限、业务数据、后端和部署协议。
  - 是否处理损坏设置、非法颜色、存储失败、刷新持久化、移动断点与可访问性。
  - 是否没有新增不必要依赖、重复 CSS 分支或大文件职责混杂。
- [ ] 若发现当前 diff 缺陷，只允许一次集中修复并重跑受影响 focused tests，最后再跑一次 `bun test` 与 `bun run build`；若出现跨模块 Important/Critical 或预算超 50%，停止补丁循环并回到架构决策。

### 4.5 最终提交检查

- [ ] 检查工作区，确保不混入用户原有删除项和 `.superpowers/`：

```powershell
git status --short
git diff --cached --stat
git diff --cached --check
```

- [ ] 若最终集中修复产生未提交的同一意图变更，单独提交：

```powershell
git add -- web/src/pages/admin/admin-layout-settings.tsx web/src/pages/admin/index.tsx web/src/pages/admin/admin-theme.tsx web/src/pages/admin/components/admin-layout-settings-drawer.tsx web/src/pages/admin/admin-layout-settings.css web/src/pages/admin/components/admin-shell.tsx web/src/pages/admin/admin-workspace.css web/src/pages/admin/admin-responsive.css web/test/admin-layout-settings.test.tsx web/test/admin-layout-settings-drawer.test.tsx web/test/admin-theme-scope.test.tsx web/test/admin-pro-layout.test.tsx
git diff --cached --check
git commit -m "fix(admin): 收口后台界面设置验收问题"
```

Expected: 分支包含三个职责提交，必要时外加一个最终集中修复提交；不 push、不建标签、不改分支。

## Done Definition

- [ ] 六项后台界面设置均由单一 Provider 管理并持久化到一个版本化键。
- [ ] 设置抽屉在桌面和移动端可用，错误事实明确，键盘和触控可达。
- [ ] 菜单主题、品牌色、侧栏宽度、内容宽度和固定顶栏真实改变后台布局。
- [ ] 创作端主题、管理员权限、路由、后端、数据库和云端升级协议未改变。
- [ ] focused tests、全量 Web、production build、bundle budget、镜像健康和三视口浏览器验收全部通过。
- [ ] 实际生产改动不超过预算，无 `any`、无旧主题双轨、无未解释脚本或测试污染。
