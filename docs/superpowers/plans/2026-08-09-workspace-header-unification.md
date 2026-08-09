# 工作区页头与首页统一实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让普通工作区与首页复用同一套站点品牌和账户操作组件，同时保持画布编辑器及独立商城/后台布局不变。

**Architecture:** 将首页现有品牌区与账户区提升为 `components/layout` 下的共享组件；首页页头和工作区页头只负责壳层布局。工作区继续独立挂载浮动导航，画布编辑器继续使用现有 `CanvasTopBar`，不引入兼容分支或第二套账户菜单。

**Tech Stack:** React 19、TypeScript 7、React Router 8、Ant Design 6、Tailwind CSS、Bun Test、Vite 8。

## Global Constraints

- `/canvas/:id` 画布编辑器及 `web/src/pages/canvas/canvas-project-top-bar.tsx`、`web/src/styles/canvas-chrome.css` 不得修改。
- `/membership`、`/credit-store`、认证页和 `/admin/*` 保留独立信息架构。
- 不修改积分、会员、公告、邀请、账户、主题或退出登录的后端接口与数据契约。
- 共享样式只消费 `web/src/styles/design-tokens.css` 的语义 Token；不得新增页面私有颜色、阴影或层级原始值。
- TypeScript 禁止 `any` 与 `as any`；所有 JSX 标签保留描述性 `className`。
- 桌面品牌区和账户胶囊为 40px；移动端主要操作热区不低于 44px。
- 必须先观察测试失败，再写最小实现；失败状态不得用默认数据或静默兜底掩盖。
- 最终提交不得包含 `web/dist/`、本地数据、密钥或其他可再生输出。

---

## 文件结构

- Create: `web/src/components/layout/site-brand-link.tsx` — 读取站点设置并渲染唯一品牌链接 DOM。
- Create: `web/src/components/layout/site-brand-link.css` — 统一 Logo、站点名称、主题和移动端尺寸。
- Create: `web/src/components/layout/site-account-actions.tsx` — 承接首页账户区的余额、会员、通知、邀请、头像与菜单逻辑。
- Create: `web/src/components/layout/site-account-actions.css` — 承接账户区的共享外观和移动端 44px 契约。
- Create: `web/test/site-header-unification.test.ts` — 锁定共享 owner、路由范围、Logo/会员契约和画布排除边界。
- Modify: `web/src/pages/home/updream/updream-header.tsx` — 使用共享品牌和账户组件。
- Modify: `web/src/pages/home/updream/updream-sticky-header.css` — 仅保留首页壳层和首页场景对比度规则。
- Modify: `web/src/pages/home/updream/updream-home.css` — 删除已经迁入共享账户样式的规则。
- Modify: `web/src/components/layout/workspace-top-bar.tsx` — 使用共享品牌和账户组件。
- Modify: `web/src/components/layout/workspace-top-bar.css` — 只保留工作区页头壳层布局。
- Modify: `web/test/logout-confirmation-contract.test.ts` — 将退出登录入口真源切到共享账户组件。
- Delete: `web/src/pages/home/updream/updream-account-actions.tsx` — 删除首页私有账户实现。
- Delete: `web/src/components/layout/workspace-sidebar-footer.tsx` — 删除工作区重复账户实现。
- Modify: `CHANGELOG.md` — 在“未发布”记录页头统一事实。

---

### Task 1: 建立共享品牌入口

**Files:**
- Create: `web/test/site-header-unification.test.ts`
- Create: `web/src/components/layout/site-brand-link.tsx`
- Create: `web/src/components/layout/site-brand-link.css`
- Modify: `web/src/pages/home/updream/updream-header.tsx`
- Modify: `web/src/pages/home/updream/updream-sticky-header.css`
- Modify: `web/src/components/layout/workspace-top-bar.tsx`
- Modify: `web/src/components/layout/workspace-top-bar.css`

**Interfaces:**
- Consumes: `useSiteSettings(): { settings: SiteSettings }` 与 `siteLogoURL(settings): string`。
- Produces: `SiteBrandLink(): JSX.Element`，无业务页面参数，始终链接到 `/`。

- [ ] **Step 1: 写品牌 owner 的失败测试**

```ts
import { describe, expect, test } from "bun:test";

const homeHeader = await Bun.file(new URL("../src/pages/home/updream/updream-header.tsx", import.meta.url)).text();
const workspaceHeader = await Bun.file(new URL("../src/components/layout/workspace-top-bar.tsx", import.meta.url)).text();
const sharedBrand = Bun.file(new URL("../src/components/layout/site-brand-link.tsx", import.meta.url));
const sharedBrandStyles = Bun.file(new URL("../src/components/layout/site-brand-link.css", import.meta.url));

describe("site header unification", () => {
    test("home and workspace share one brand owner", async () => {
        expect(await sharedBrand.exists()).toBe(true);
        expect(homeHeader).toContain("<SiteBrandLink />");
        expect(workspaceHeader).toContain("<SiteBrandLink />");
        expect(homeHeader).not.toContain("siteLogoURL(");
        expect(workspaceHeader).not.toContain("siteLogoURL(");
    });

    test("shared brand owns the homepage dimensions", async () => {
        const styles = await sharedBrandStyles.text();
        expect(styles).toContain("height: var(--space-7)");
        expect(styles).toContain("width: 22px");
        expect(styles).toContain("width: 26px");
        expect(styles).toContain("min-height: 44px");
    });
});
```

- [ ] **Step 2: 运行测试并确认缺少共享组件而失败**

Run: `cd web && bun test test/site-header-unification.test.ts`

Expected: FAIL；`site-brand-link.tsx` 与 `site-brand-link.css` 不存在，两个页头也尚未渲染 `<SiteBrandLink />`。

- [ ] **Step 3: 实现最小共享品牌组件**

```tsx
import { Link } from "react-router";

import { siteLogoURL, useSiteSettings } from "@/components/site/site-settings-provider";
import "@/components/layout/site-brand-link.css";

export function SiteBrandLink() {
    const { settings } = useSiteSettings();

    return (
        <Link className="site-brand-link" data-logo-source={settings.logoUrl ? "custom" : "default"} to="/" aria-label={`${settings.siteName} 首页`}>
            <span className="site-brand-mark" aria-hidden="true">
                <img className="site-brand-image" src={siteLogoURL(settings)} alt="" />
            </span>
            <span className="site-brand-name">{settings.siteName}</span>
        </Link>
    );
}
```

`site-brand-link.css` 使用首页现有 40px 点击区、32px 标记容器、22px 默认 Logo、26px 自定义 Logo、20px 站点名称；在 `max-width: 639px` 设置 `min-height: 44px`，在 `max-width: 420px` 隐藏 `.site-brand-name`。颜色、圆角、焦点、hover 和过渡全部使用现有语义 Token。

- [ ] **Step 4: 两个页头接入共享品牌并删除重复品牌 CSS**

在 `updream-header.tsx` 与 `workspace-top-bar.tsx` 中导入并渲染 `<SiteBrandLink />`，删除两处 `useSiteSettings`、`siteLogoURL` 和重复品牌 DOM。将 `updream-sticky-header.css` 中首页专属文字对比规则改为 `.updream-header .site-brand-name`，其余品牌尺寸规则删除；将 `workspace-top-bar.css` 中 `.workspace-top-bar-brand*` 规则删除，只保留 `.workspace-top-bar .site-brand-link { pointer-events: auto; }` 这一壳层职责。

- [ ] **Step 5: 运行品牌测试和类型检查**

Run: `cd web && bun test test/site-header-unification.test.ts && bunx tsc --noEmit`

Expected: PASS；无 TypeScript 错误。

- [ ] **Step 6: 提交品牌原语**

```bash
git add web/test/site-header-unification.test.ts web/src/components/layout/site-brand-link.tsx web/src/components/layout/site-brand-link.css web/src/pages/home/updream/updream-header.tsx web/src/pages/home/updream/updream-sticky-header.css web/src/components/layout/workspace-top-bar.tsx web/src/components/layout/workspace-top-bar.css
git commit -m "refactor(web): 页头 - 统一站点品牌入口"
```

---

### Task 2: 提升首页账户区为共享组件

**Files:**
- Create: `web/src/components/layout/site-account-actions.tsx`
- Create: `web/src/components/layout/site-account-actions.css`
- Modify: `web/src/pages/home/updream/updream-header.tsx`
- Modify: `web/src/pages/home/updream/updream-home.css`
- Modify: `web/test/site-header-unification.test.ts`
- Modify: `web/test/logout-confirmation-contract.test.ts`
- Delete: `web/src/pages/home/updream/updream-account-actions.tsx`

**Interfaces:**
- Consumes: `useUserStore`、`useWalletBalance`、`useMembershipAction`、`useThemeStore`、`useConfirmLogout`、`SystemAnnouncementCenter` 与现有 `ReferralRewardCenter`。
- Produces: `SiteAccountActions(): JSX.Element`；内部唯一 `MembershipIcon({ className }: { className: string }): JSX.Element`。

- [ ] **Step 1: 扩展测试，先要求共享账户 owner**

```ts
const sharedAccount = Bun.file(new URL("../src/components/layout/site-account-actions.tsx", import.meta.url));
const sharedAccountStyles = Bun.file(new URL("../src/components/layout/site-account-actions.css", import.meta.url));
const homeAccount = Bun.file(new URL("../src/pages/home/updream/updream-account-actions.tsx", import.meta.url));

test("homepage account actions have a shared owner", async () => {
    expect(await sharedAccount.exists()).toBe(true);
    expect(homeHeader).toContain("<SiteAccountActions />");
    expect(await homeAccount.exists()).toBe(false);
    const source = await sharedAccount.text();
    expect(source).toContain("useMembershipAction");
    expect(source).toContain("site-membership-icon");
    expect(source).not.toContain("Gem");
});

test("shared account actions keep desktop density and mobile touch targets", async () => {
    const styles = await sharedAccountStyles.text();
    expect(styles).toContain("height: var(--space-7)");
    expect(styles).toContain("@media (max-width: 520px)");
    expect(styles).toContain("min-height: 44px");
    expect(styles).not.toContain("#172033");
    expect(styles).not.toContain("#ffffff");
});
```

- [ ] **Step 2: 运行测试并确认共享账户文件缺失而失败**

Run: `cd web && bun test test/site-header-unification.test.ts test/logout-confirmation-contract.test.ts`

Expected: FAIL；共享账户文件不存在，旧首页文件仍存在。

- [ ] **Step 3: 移动并重命名账户实现**

先把 `updream-account-actions.tsx` 原文件完整移动为 `components/layout/site-account-actions.tsx`，再执行以下确定性重命名：`UpdreamAccountActions` → `SiteAccountActions`、`UpdreamUserAvatar` → `SiteUserAvatar`、`.updream-account-*` → `.site-account-*`、`.updream-membership-*` → `.site-membership-*`。原 `MembershipIcon` 的完整 SVG path 树逐字保留，只把根 class 改为 `site-membership-icon`。`ReferralRewardCenter` 改用 `@/pages/home/updream/referral-reward-center` 绝对导入，不移动其业务实现。hydration、余额格式化、会员文案、邀请事件、公告、主题与退出登录代码不做行为修改。

- [ ] **Step 4: 提取共享账户样式并修正移动端热区**

从 `updream-home.css` 移出账户、登录按钮、余额图标和 `max-width: 520px` 账户规则到 `site-account-actions.css`。迁移时把旧 `#172033`、`#ffffff`、半透明同色值和余额蓝色全部映射为 `--text-primary`、`--bg-primary`、`--bg-hover`、`--brand-primary` 及 `color-mix`，不得把首页硬编码颜色带入共享样式。桌面保留 40px 高度；移动端将通知、会员、头像、登录和升级入口统一为至少 44px，不沿用旧 36px 缩小值。共享组件自行导入该 CSS，保证直接访问 `/projects` 时不依赖首页路由先加载样式。

- [ ] **Step 5: 首页改用共享账户组件并更新退出登录契约**

`updream-header.tsx` 改为：

```tsx
import { SiteAccountActions } from "@/components/layout/site-account-actions";

export function UpdreamHeader() {
    return (
        <header className="updream-header flex h-[72px] items-center justify-between px-5 sm:px-8">
            <SiteBrandLink />
            <SiteAccountActions />
        </header>
    );
}
```

`logout-confirmation-contract.test.ts` 将首页入口文件改为 `site-account-actions.tsx`，继续断言 `useConfirmLogout` 与 `confirmLogout()`，不改变确认弹窗业务契约。删除旧 `updream-account-actions.tsx`。

- [ ] **Step 6: 运行账户测试与类型检查**

Run: `cd web && bun test test/site-header-unification.test.ts test/logout-confirmation-contract.test.ts && bunx tsc --noEmit`

Expected: PASS；旧首页账户文件不存在，退出登录测试仍通过。

- [ ] **Step 7: 提交共享账户组件**

```bash
git add web/src/components/layout/site-account-actions.tsx web/src/components/layout/site-account-actions.css web/src/pages/home/updream/updream-header.tsx web/src/pages/home/updream/updream-home.css web/test/site-header-unification.test.ts web/test/logout-confirmation-contract.test.ts
git add -u web/src/pages/home/updream/updream-account-actions.tsx
git commit -m "refactor(web): 页头 - 提升首页账户区为共享组件"
```

---

### Task 3: 工作区复用首页账户区并删除双轨实现

**Files:**
- Modify: `web/src/components/layout/workspace-top-bar.tsx`
- Modify: `web/src/components/layout/workspace-top-bar.css`
- Modify: `web/test/site-header-unification.test.ts`
- Modify: `web/test/logout-confirmation-contract.test.ts`
- Delete: `web/src/components/layout/workspace-sidebar-footer.tsx`

**Interfaces:**
- Consumes: `SiteAccountActions(): JSX.Element` 与 `SiteBrandLink(): JSX.Element`。
- Produces: `WorkspaceTopBar(): JSX.Element`，仅负责 64px 工作区壳层与左右布局。

- [ ] **Step 1: 写工作区硬切换失败测试**

```ts
const workspaceAccount = Bun.file(new URL("../src/components/layout/workspace-sidebar-footer.tsx", import.meta.url));
const canvasTopBar = await Bun.file(new URL("../src/pages/canvas/canvas-project-top-bar.tsx", import.meta.url)).text();
const workspaceShell = await Bun.file(new URL("../src/components/layout/app-top-nav.tsx", import.meta.url)).text();

test("workspace reuses the homepage account actions without a parallel menu", async () => {
    expect(workspaceHeader).toContain("<SiteAccountActions />");
    expect(workspaceHeader).not.toContain("WorkspaceSidebarFooter");
    expect(await workspaceAccount.exists()).toBe(false);
});

test("canvas editor and independent surfaces stay outside the workspace header", () => {
    expect(workspaceShell).toContain('pathname === "/membership"');
    expect(workspaceShell).toContain('pathname === "/credit-store"');
    expect(workspaceShell).toContain('pathname.startsWith("/admin")');
    expect(workspaceShell).toContain('/^\\/canvas\\/[^/]+/.test(pathname)');
    expect(workspaceShell).not.toContain('pathname === "/canvas"');
    expect(canvasTopBar).not.toContain("SiteAccountActions");
    expect(canvasTopBar).not.toContain("SiteBrandLink");
});
```

- [ ] **Step 2: 运行测试并确认工作区仍使用旧菜单而失败**

Run: `cd web && bun test test/site-header-unification.test.ts test/logout-confirmation-contract.test.ts`

Expected: FAIL；`WorkspaceSidebarFooter` 仍被导入且文件仍存在。

- [ ] **Step 3: 工作区接入共享账户并删除旧实现**

`workspace-top-bar.tsx` 的账户区改为：

```tsx
<div className="workspace-top-bar-account">
    <SiteAccountActions />
</div>
```

删除 `workspace-sidebar-footer.tsx`；`logout-confirmation-contract.test.ts` 的入口列表只保留共享 `site-account-actions.tsx`，证明退出登录只有一个正式 owner。

- [ ] **Step 4: 将工作区 CSS 收敛为壳层布局**

删除 `.workspace-top-bar-account-pill`、`.workspace-top-bar-balance*`、`.workspace-top-bar-membership*`、`.workspace-account-trigger*` 及对应暗色/响应式规则。保留：

```css
.workspace-top-bar {
    position: fixed;
    inset: 0 0 auto;
    z-index: var(--layer-header);
    height: var(--space-9);
    pointer-events: none;
}

.workspace-top-bar-inner {
    display: flex;
    width: 100%;
    height: 100%;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-5);
    padding: var(--space-3) var(--space-5);
}

.workspace-top-bar .site-brand-link,
.workspace-top-bar-account {
    pointer-events: auto;
}
```

移动端只调整工作区壳层的高度、间距与左右 padding；共享品牌和账户内部尺寸由各自 CSS 负责。

- [ ] **Step 5: 运行定向测试、全量 source tests 与生产构建**

Run: `cd web && bun test test/site-header-unification.test.ts test/logout-confirmation-contract.test.ts && bun test && bun run build`

Expected: 定向测试 PASS；全部 Bun 测试 PASS；TypeScript、Vite 与 bundle budget PASS。

- [ ] **Step 6: 提交工作区硬切换**

```bash
git add web/src/components/layout/workspace-top-bar.tsx web/src/components/layout/workspace-top-bar.css web/test/site-header-unification.test.ts web/test/logout-confirmation-contract.test.ts
git add -u web/src/components/layout/workspace-sidebar-footer.tsx
git commit -m "fix(web): 工作区页头 - 复用首页账户操作"
```

---

### Task 4: 真实页面回归、文档与交付复核

**Files:**
- Modify: `CHANGELOG.md`
- Force-add: `docs/superpowers/specs/2026-08-09-workspace-header-unification-design.md`
- Force-add: `docs/superpowers/plans/2026-08-09-workspace-header-unification.md`

**Interfaces:**
- Consumes: 已通过测试与构建的共享页头实现。
- Produces: 可审计的未发布说明、视觉回归证据与干净提交范围。

- [ ] **Step 1: 启动本地 Web 并逐路由检查桌面版**

Run: `cd web && bun run dev`

在 1440px、亮色和暗色下检查 `/`、`/projects`、项目详情、`/tasks`、`/assets`、`/skills`、`/teams`、`/wallet`、`/settings`、`/canvas`：Logo 容器、Logo 图片、站点名称、积分、金色会员图标、会员文案、头像和菜单尺寸一致；页面无横向溢出，账户菜单功能可达。

- [ ] **Step 2: 检查移动版与排除路由**

在 390×844 下检查 `/projects`、`/tasks`、`/assets`、`/wallet`、`/settings`：主要热区不小于 44px，隐藏文案后仍有 `aria-label`，页头无重叠和横向滚动。打开 `/canvas/:id`，确认画布顶部栏 DOM 与外观未改变；打开 `/membership`、`/credit-store` 与 `/admin`，确认仍使用各自布局。

- [ ] **Step 3: 更新未发布日志**

在 `CHANGELOG.md` 的“未发布”下增加：

```markdown
### 工作区体验

- 统一首页与项目、任务、素材、技能、团队、积分、设置及画布列表的站点 Logo、会员入口和账户操作，消除多套页头造成的尺寸与图标漂移。
```

- [ ] **Step 4: 运行最终验证**

Run: `cd web && bun test && bun run build`

Run: `git diff --check && git status --short`

Expected: 测试与构建 PASS；`git diff --check` 无错误；状态中只有本任务源码、测试、设计/计划文档和 `CHANGELOG.md`。

- [ ] **Step 5: 对照设计做最终自审**

逐项确认：普通工作区均使用共享品牌与账户组件；画布编辑器文件无 diff；独立商城/认证/后台路由无视觉结构改动；不存在 `Gem` 会员入口、第二套账户菜单或旧 `workspace-sidebar-footer.tsx`；无接口、数据库或业务数据变化。

- [ ] **Step 6: 提交文档与未发布记录**

```bash
git add CHANGELOG.md
git add -f docs/superpowers/specs/2026-08-09-workspace-header-unification-design.md docs/superpowers/plans/2026-08-09-workspace-header-unification.md
git commit -m "docs(web): 工作区页头 - 记录统一设计与验收"
```
