# Membership Storefront Close Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 `/membership` 增加始终可发现的页面级关闭按钮，并让按钮与 `Escape` 共用“有历史返回、无历史回首页”的安全导航。

**Architecture:** 新建一个无浏览器副作用的导航领域模块，将历史状态解析和键盘退出判定集中为纯函数；`MembershipPage` 只负责把纯函数结果交给 React Router。按钮直接挂在页面根节点，付款弹窗打开时不渲染，样式复用全局语义 Token。

**Tech Stack:** React 19、TypeScript strict、React Router、Lucide React、Bun Test、Vite、CSS Design Tokens

## Global Constraints

- 页面按钮和 `Escape` 必须复用同一退出函数。
- 历史索引大于 0 时返回上一页；缺失、非法或等于 0 时进入 `/`。
- 付款弹窗打开时不渲染页面级关闭按钮。
- 关闭按钮使用 Lucide `X`，可访问名称固定为“关闭会员商城”。
- 交互热区固定为 44×44px，并提供可见 `:focus-visible` 状态。
- 不改变会员数据、套餐选择、订单创建和付款弹窗状态机。
- 不新增依赖，不使用 `any`，不写死新的颜色值。

---

### Task 1: 会员商城安全退出入口

**Files:**
- Create: `web/src/pages/membership/membership-storefront-navigation.ts`
- Create: `web/test/membership-storefront-navigation.test.ts`
- Modify: `web/src/pages/membership/index.tsx`
- Modify: `web/src/pages/membership/membership-payment-dialog.tsx`
- Modify: `web/src/pages/membership/membership-storefront.css`
- Modify: `web/test/membership-payment-dialog.test.ts`
- Modify: `web/test/membership-storefront-visual-contract.test.ts`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Produces: `membershipStorefrontExitIntent(historyState: unknown): "back" | "home"`
- Produces: `shouldExitMembershipStorefront(key: string, paymentDialogOpen: boolean): boolean`
- Consumes: React Router `navigate(-1)` 与 `navigate("/")`

- [ ] **Step 1: 写导航与页面结构失败测试**

```ts
expect(membershipStorefrontExitIntent({ idx: 2 })).toBe("back");
expect(membershipStorefrontExitIntent({ idx: 0 })).toBe("home");
expect(membershipStorefrontExitIntent(null)).toBe("home");
expect(shouldExitMembershipStorefront("Escape", false)).toBe(true);
expect(shouldExitMembershipStorefront("Escape", true)).toBe(false);
expect(page).toContain('aria-label="关闭会员商城"');
expect(page).toContain("membership-storefront-close");
expect(storefront).toContain("width: 44px");
expect(storefront).toContain("height: 44px");
```

- [ ] **Step 2: 运行聚焦测试并确认真实 RED**

Run:

```powershell
cd web
bun test test/membership-storefront-navigation.test.ts test/membership-payment-dialog.test.ts test/membership-storefront-visual-contract.test.ts
```

Expected: FAIL，明确指出导航模块、关闭按钮或 44px 样式尚不存在；不得因依赖或测试环境失败冒充 RED。

- [ ] **Step 3: 实现纯导航契约**

```ts
export type MembershipStorefrontExitIntent = "back" | "home";

export function membershipStorefrontExitIntent(historyState: unknown): MembershipStorefrontExitIntent {
    if (typeof historyState !== "object" || historyState === null || !("idx" in historyState)) return "home";
    const historyIndex = historyState.idx;
    return typeof historyIndex === "number" && Number.isFinite(historyIndex) && historyIndex > 0 ? "back" : "home";
}

export function shouldExitMembershipStorefront(key: string, paymentDialogOpen: boolean): boolean {
    return key === "Escape" && !paymentDialogOpen;
}
```

- [ ] **Step 4: 接入页面按钮和统一退出行为**

```tsx
const exitMembershipStorefront = useCallback(() => {
    if (membershipStorefrontExitIntent(window.history.state) === "back") {
        navigate(-1);
        return;
    }
    navigate("/");
}, [navigate]);

{!dialogOpen ? (
    <button aria-label="关闭会员商城" className="membership-storefront-close" onClick={exitMembershipStorefront} type="button">
        <X aria-hidden="true" className="membership-storefront-close-icon" />
    </button>
) : null}
```

同时将现有 `shouldNavigateFromMembershipPage` 从付款弹窗模块硬切换为 `shouldExitMembershipStorefront`，删除旧导出与旧双轨命名。

- [ ] **Step 5: 实现 Token 化视觉与响应式**

```css
.membership-storefront-close {
    position: fixed;
    top: 16px;
    right: 16px;
    z-index: 100;
    display: inline-flex;
    width: 44px;
    height: 44px;
    align-items: center;
    justify-content: center;
    border: 0;
    border-radius: 8px;
    color: var(--text-primary);
    background: var(--bg-tertiary);
    cursor: pointer;
}
```

悬停、按下与焦点仅使用现有语义 Token；图标保持 20px，不增加阴影或第二层边框。

- [ ] **Step 6: 运行聚焦测试并确认 GREEN**

Run:

```powershell
cd web
bun test test/membership-storefront-navigation.test.ts test/membership-payment-dialog.test.ts test/membership-storefront-visual-contract.test.ts
```

Expected: PASS，导航边界、付款弹窗隔离、按钮语义和尺寸全部通过。

- [ ] **Step 7: 运行完整前端门禁**

Run:

```powershell
cd web
bun test
bun run build
```

Expected: 全量测试、TypeScript、Vite 生产构建与 bundle budgets 全部通过。

- [ ] **Step 8: 本地预览验收**

Run:

```powershell
docker compose -p hmaigc-local build web
docker compose -p hmaigc-local up -d --no-deps web
curl.exe -fsS http://127.0.0.1:3000/membership -o NUL
docker compose -p hmaigc-local ps
```

Expected: Web 容器 healthy、`/membership` 返回 200；页面右上角按钮为 44×44px，Tab 可聚焦，付款弹窗打开时页面按钮不出现。

- [ ] **Step 9: 同步变更记录并完成最终审查**

在 `CHANGELOG.md` 的 Unreleased 会员体验下记录“会员商城增加可见关闭入口，有历史返回来路、无历史回首页”；随后执行：

```powershell
git diff --check
git status --short
git diff -- web/src/pages/membership web/test CHANGELOG.md
```

Expected: 无空白错误、无构建产物、无支付/数据库/无关文件变更。

- [ ] **Step 10: 提交独立版本**

```powershell
git add -- web/src/pages/membership/membership-storefront-navigation.ts web/src/pages/membership/index.tsx web/src/pages/membership/membership-payment-dialog.tsx web/src/pages/membership/membership-storefront.css web/test/membership-storefront-navigation.test.ts web/test/membership-payment-dialog.test.ts web/test/membership-storefront-visual-contract.test.ts CHANGELOG.md
git commit -m "feat(web): 会员商城 - 增加页面关闭入口"
```

Expected: 单一提交仅包含关闭入口实现、测试与变更记录；不 push。
