# 后台用户管理页面排版实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把用户管理页接入共享数据页布局，在保持全部用户管理业务契约的前提下统一筛选、批量操作和列表排版。

**Architecture:** `UsersPanel` 继续持有请求、URL、选择、列偏好和用户操作状态，只把现有视图组合进 `AdminDataLayout`、`AdminFilterSection` 与 `AdminContentSection`。共享 CSS 只补用户页需要的标题操作位和响应式规则，不改变 API 或领域逻辑。

**Tech Stack:** React 19、TypeScript strict、Ant Design 6、现有后台数据页组件、Bun Test、Vite。

## Global Constraints

- 不改变后台路由、接口、权限、URL 查询状态、列偏好键或用户操作行为。
- 不新增用户指标带，不把筛选结果总数伪装为运营指标。
- 不引入新依赖、兼容层、默认数据或静默回退。
- 生产职责不超过 3 项，修改不超过 4 个生产文件，净新增生产代码不超过 250 行。
- 最终必须通过全量测试、生产构建、镜像重建和 1440×900、1024×768、390×844 浏览器验收。

---

### Task 1: 用真实页面测试固定用户页结构

**Files:**
- Create: `web/test/admin-users-layout.test.tsx`
- Modify: `web/src/pages/admin/users/users-panel.tsx`

**Interfaces:**
- Consumes: `AdminDataLayout`、`AdminFilterSection`、`AdminContentSection`。
- Preserves: `UsersPanel({ onUserChanged })`、`listAdminUsers` 请求参数、URL 状态、列偏好和用户操作回调。

- [ ] **Step 1: 编写失败的结构测试**

使用 Happy DOM、`MemoryRouter`、`App` 和真实 `UsersPanel`，模拟一个成功用户列表响应并断言：

```ts
expect(Array.from(document.querySelector(".admin-data-layout")?.children ?? [], (node) => node.className)).toEqual([
  "admin-filter-section",
  "admin-content-section",
]);
expect(document.querySelector('[role="region"][aria-label="用户筛选"]')).not.toBeNull();
expect(document.querySelector(".admin-content-section-title")?.textContent).toBe("用户列表");
expect(document.querySelector(".admin-data-section-actions")?.textContent).toContain("共 1 位用户");
expect(document.querySelector(".admin-data-section-actions")?.textContent).toContain("列设置");
```

同时断言筛选标签、表格、分页、加载失败重试和筛选空状态仍由现有真实组件渲染。

- [ ] **Step 2: 运行定向 RED**

Run: `bun test test/admin-users-layout.test.tsx`

Expected: FAIL，因为 `UsersPanel` 尚未渲染共享数据页结构。

- [ ] **Step 3: 最小接入共享组件**

在 `users-panel.tsx` 中：

- 用 `AdminDataLayout` 包裹主内容。
- 用 `AdminFilterSection label="用户筛选"` 包裹现有 `ListToolbar`。
- 将结果总数与列设置提取为 `AdminContentSection` 的 `actions`。
- 将 `AdminBatchBar`、加载/错误状态、Table 与 Pagination 放入同一“用户列表”内容区。
- 保留两个 Drawer 在数据布局外，保持其生命周期与回调不变。

- [ ] **Step 4: 运行定向 GREEN**

Run: `bun test test/admin-users-layout.test.tsx test/admin-data-layout.test.tsx`

Expected: PASS，且结构顺序与既有状态断言全部通过。

- [ ] **Step 5: 提交结构改造**

```bash
git add web/src/pages/admin/users/users-panel.tsx web/test/admin-users-layout.test.tsx
git commit -m "refactor(admin): 统一用户管理页面结构"
```

---

### Task 2: 清理用户页专用样式并完成响应式排版

**Files:**
- Modify: `web/src/pages/admin/admin-workspace.css`
- Modify: `web/src/pages/admin/admin-responsive.css`
- Modify: `web/test/admin-users-layout.test.tsx`

**Interfaces:**
- Consumes: `.admin-users-content-actions`、`.admin-users-list-toolbar`、`.admin-users-selection-region`。
- Produces: 桌面紧凑操作位、平板筛选换行、手机单列筛选和表格局部滚动。

- [ ] **Step 1: 编写失败的 CSS 契约测试**

断言用户页操作位使用共享标题栏，旧的“工具栏内尾随结果区”选择器不再存在；响应式 CSS 在 1279px 与 639px 下分别提供换行与单列规则。

- [ ] **Step 2: 运行定向 RED**

Run: `bun test test/admin-users-layout.test.tsx`

Expected: FAIL，因为旧工具栏尾随布局仍存在。

- [ ] **Step 3: 最小实现样式硬切**

- 删除只服务旧 DOM 结构的用户工具栏尾随和相邻表格选择器。
- 标题操作位在桌面横排，在手机允许换行且按钮热区不低于 44px。
- 筛选控件继续复用 `workspace-list-toolbar`，手机保持单列。
- 不通过页面级 `overflow-x: hidden` 掩盖布局问题。

- [ ] **Step 4: 运行定向 GREEN 与静态检查**

```bash
bun test test/admin-users-layout.test.tsx test/admin-data-layout.test.tsx
rg -n "admin-users-toolbar-trailing" web/src/pages/admin web/test
```

Expected: tests PASS；退役选择器没有生产引用。

- [ ] **Step 5: 提交响应式样式**

```bash
git add web/src/pages/admin/admin-workspace.css web/src/pages/admin/admin-responsive.css web/test/admin-users-layout.test.tsx
git commit -m "style(admin): 统一用户管理响应式排版"
```

---

### Task 3: 最终验收与镜像重建

**Files:**
- Verify only; 仅在发现当前 diff 缺陷且仍在预算内时修改。

- [ ] **Step 1: 运行全量 Web 门禁**

```bash
cd web
bun test
bun run build
```

- [ ] **Step 2: 核对范围与禁止项**

确认无 `any`、TODO、额外依赖、后端或数据库改动；实际生产文件和净新增行数未越过预算。

- [ ] **Step 3: 安全重建本地镜像**

从工作树运行 `scripts/local-compose.ps1 up -d --build --wait`，确认 `/data` 精确绑定主仓 `E:/新版短剧制作/open-ai-canvas/.local/data`，backend/web Healthy，`/api/health` 和 `/admin/users` 返回 200。

- [ ] **Step 4: 真实浏览器三档验收**

在 1440×900、1024×768、390×844 验证筛选、结果操作、表格和分页顺序；执行一次搜索清除、角色筛选、列设置打开/关闭、用户详情打开/关闭，并确认页面无横向溢出。

- [ ] **Step 5: 最终显式审查**

对照原始请求、设计规格、实施计划、实际 diff、测试、浏览器和数据挂载事实；只修复当前 diff 缺陷，不扩展新范围，不创建空提交。
